package desk

// Compatibility is a reviewed claim, separate from validation and test success.
// Both manually edited files and saved chat proposals enter this publication gate.
import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
)

type projectArtifactReview struct {
	Key          string               `json:"key"`
	Path         string               `json:"path"`
	ArtifactID   string               `json:"artifactId"`
	Version      string               `json:"version"`
	BaseVersion  string               `json:"baseVersion,omitempty"`
	BaseArtifact *retainedArtifactRef `json:"baseArtifact,omitempty"`
	Required     bool                 `json:"required"`
	Review       *artifactReviewReply `json:"review,omitempty"`
}

func projectReviewRevision(snap *reviewSnapshot) string {
	raw, _ := json.Marshal([]any{snap.set, artifactReference(snap.lock), snap.hasLock})
	return digestOf(raw)
}
func projectReviewTarget(key string) string { return "urn:judgmentpack:project-review:" + key }
func projectReviewBase(snap *reviewSnapshot) string {
	if snap.hasLock {
		return digestOf(snap.lock)
	}
	return ""
}

func (s *Server) projectReviewRows(ctx context.Context, snap *reviewSnapshot, ledger publicationLedger) ([]projectArtifactReview, error) {
	candidate, err := publicationFromSnapshot(snap)
	if err != nil {
		return nil, err
	}
	rows := []projectArtifactReview{}
	dir, err := holdDeskFolder(s.root, s.root.Name())
	if err != nil {
		return nil, err
	}
	defer dir.file.Close()
	comparisons := map[string]string{}
	higher := func(base, candidate string) (bool, error) {
		if base == candidate {
			return false, nil
		}
		key := base + "/" + candidate
		if value, ok := comparisons[key]; ok {
			return value == "greater", nil
		}
		if len(comparisons) >= 256 {
			return false, errors.New("Too many artifact versions to compare in one review.")
		}
		result, e := s.artifactVersionComparison(ctx, dir, base, candidate)
		if e != nil {
			return false, e
		}
		comparisons[key] = result.Comparison
		return result.Comparison == "greater", nil
	}
	var lock lockDocument
	if snap.hasLock {
		_, lock, err = lockedSet(snap.lock)
		if err != nil {
			return nil, err
		}
	}
	for _, doc := range candidate.Documents {
		row := projectArtifactReview{Key: doc.Kind + ":" + doc.Alias, Path: doc.Path, ArtifactID: doc.ID, Version: doc.Version, Required: true}
		locked := lock.Packs[doc.Alias]
		if doc.Kind == "graph" {
			locked = lock.Graphs[doc.Alias]
		}
		if locked.Digest == "sha256:"+doc.Content.SHA256 {
			row.Required = false
			rows = append(rows, row)
			continue
		}
		// Prefer the binding being replaced, including a legacy reviewed lock.
		if locked.Digest != "" && row.Required {
			var old []byte
			if data, ok := snap.bytesOf(locked.Digest); ok {
				old = data
			} else if name, ok := copyName(locked.Digest); ok {
				copies, e := s.reviewedCopies(false)
				if e == nil {
					old, e = readPrivateData(copies, name, reviewTextLimit)
					copies.Close()
				}
				if e != nil {
					return nil, e
				}
			}
			if sha256Digest(old) != locked.Digest {
				return nil, errRetainedArtifactChanged
			}
			var head struct{ ID, Version string }
			if json.Unmarshal(old, &head) != nil {
				return nil, errRetainedArtifactChanged
			}
			if head.ID == doc.ID {
				row.BaseVersion = head.Version
				ref := artifactReference(old)
				row.BaseArtifact = &ref
			}
		}
		known := false
		for _, publication := range ledger.Items {
			for _, prior := range publication.Documents {
				if prior.Kind == doc.Kind && prior.ID == doc.ID && prior.Content == doc.Content {
					known = true
				}
			}
		}
		// Adopting an already published immutable release compares its replaced
		// binding. A newly published version advances the highest identity floor.
		if !known {
			for _, publication := range ledger.Items {
				for _, prior := range publication.Documents {
					if prior.Kind != doc.Kind || prior.ID != doc.ID {
						continue
					}
					newer := row.BaseVersion == ""
					if !newer {
						var e error
						newer, e = higher(row.BaseVersion, prior.Version)
						if e != nil {
							return nil, e
						}
					}
					if newer {
						row.BaseVersion = prior.Version
						ref := prior.Content
						row.BaseArtifact = &ref
					}
				}
			}
		} else if row.BaseVersion == "" {
			row.Required = false
		}

		if row.Required {
			retained, e := s.latestArtifactReview(ctx, projectReviewTarget(row.Key))
			if e != nil {
				return nil, e
			}
			if retained != nil && retained.Review.StorageRevision == projectReviewRevision(snap) && retained.Review.Candidate == doc.Content && retained.Review.BasePublication == projectReviewBase(snap) && retained.Review.ProposedVersion == doc.Version {
				row.Review = retained
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (s *Server) handleProjectArtifactReview(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var request struct {
		Token   string                    `json:"token"`
		Impacts map[string]artifactImpact `json:"impacts"`
	}
	raw, err := readBounded(r.Body, artifactReviewLimit)
	if err != nil || decodeDataJSON(raw, &request) != nil || len(request.Token) != 64 || len(request.Impacts) > reviewEntryLimit {
		writeJSONCoded(w, 400, CodeBadRequest, "Review the artifact versions and compatibility impact.")
		return
	}
	fail := func(err error) { writeJSONCoded(w, 409, CodeStale, err.Error()) }
	dir, refusal := s.reviewRuntime()
	if refusal != "" {
		fail(errors.New(refusal))
		return
	}
	unlock, failure := s.lockProjectFor(r.Context(), "artifact review", "reviewed")
	if failure != nil {
		writeJSONCoded(w, failure.status, failure.code, failure.message)
		return
	}
	defer unlock()
	snap, err := s.readSnapshot()
	if err != nil {
		fail(err)
		return
	}
	if !hmac.Equal([]byte(request.Token), []byte(s.reviewToken(snap))) {
		fail(errors.New("The project changed. Review the current files again."))
		return
	}
	p, err := s.beginPublication(r.Context(), snap)
	if err != nil {
		fail(err)
		return
	}
	defer p.close()
	if err = s.checkPublicationVersions(r.Context(), dir, p); err != nil {
		fail(err)
		return
	}
	rows, err := s.projectReviewRows(r.Context(), snap, p.ledger)
	if err != nil {
		fail(err)
		return
	}
	count := 0
	for _, row := range rows {
		if !row.Required {
			continue
		}
		count++
		if err = s.checkArtifactImpact(r.Context(), dir, row.BaseVersion, row.Version, request.Impacts[row.Key]); err != nil {
			fail(err)
			return
		}
	}
	if count != len(request.Impacts) {
		fail(errors.New("Review exactly the artifacts whose content changed."))
		return
	}
	if err = s.retainProjectArtifactReviews(r.Context(), snap, p, rows, request.Impacts); err != nil {
		fail(err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": rows})
}

func (s *Server) admitProjectArtifactReviews(ctx context.Context, dir heldDir, snap *reviewSnapshot, p *publicationTransaction, refs map[string]retainedArtifactRef) error {
	rows, err := s.projectReviewRows(ctx, snap, p.ledger)
	if err != nil {
		return err
	}
	manifest, _ := json.Marshal(snap.set)
	accepted := []retainedArtifactRef{}
	for _, row := range rows {
		if !row.Required {
			continue
		}
		ref, ok := refs[row.Key]
		if !ok {
			return errors.New("Review each changed artifact’s compatibility before locking.")
		}
		retained, e := s.artifactReview(ctx, ref)
		if e != nil {
			return e
		}
		review := retained.Review
		var doc publishedDocument
		for _, d := range p.candidate.Documents {
			if d.Kind+":"+d.Alias == row.Key {
				doc = d
				break
			}
		}
		if review.Target != projectReviewTarget(row.Key) || review.StorageRevision != projectReviewRevision(snap) || review.BasePublication != projectReviewBase(snap) || review.Candidate != doc.Content || review.ProposedVersion != doc.Version || !sameEvaluationValue(review.Dependencies, projectReviewDependencies(artifactReference(manifest), *p.candidate.Config, row.BaseArtifact)) {
			return errors.New("The artifact or its reviewed base changed. Review compatibility again.")
		}
		if e = s.checkArtifactImpact(ctx, dir, row.BaseVersion, row.Version, review.Impact); e != nil {
			return e
		}
		accepted = append(accepted, ref)
	}
	if len(accepted) != len(refs) {
		return errors.New("The compatibility review does not match the changed artifacts.")
	}
	p.candidate.Reviews = accepted
	p.candidate.ID = publicationID(p.candidate)
	if len(accepted) == 0 {
		for _, prior := range p.ledger.Items {
			if samePublicationContent(prior, p.candidate) {
				p.candidate = prior
				break
			}
		}
	}
	return nil
}

// The project lock and private history are separate durable writes. Retain the
// accepted review references before invoking Runtime, so a lost ledger receipt
// can recover them from an already committed exact public lock.
func (p *publicationTransaction) stageReview() error {
	raw, err := json.Marshal(p.candidate)
	if err != nil {
		return err
	}
	if len(raw) > publicationLedgerLimit {
		return errors.New("Publication review exceeds its storage limit.")
	}
	return writePrivateData(p.root, "pending.json", raw)
}
func readPendingPublication(root *os.Root) (*artifactPublication, error) {
	raw, err := readPrivateData(root, "pending.json", publicationLedgerLimit)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p artifactPublication
	if decodeDataJSON(raw, &p) != nil || p.ID != publicationID(p) || p.Dialect != nil || p.Config == nil || !p.Config.valid() {
		return nil, errors.New("Pending publication review is unreadable.")
	}
	for _, ref := range p.Reviews {
		if !ref.valid() {
			return nil, errRetainedArtifactChanged
		}
	}
	return &p, nil
}

func projectReviewDependencies(manifest, config retainedArtifactRef, base *retainedArtifactRef) []retainedArtifactRef {
	refs := []retainedArtifactRef{manifest, config}
	if base != nil {
		refs = append(refs, *base)
	}
	return refs
}

func (s *Server) retainProjectArtifactReviews(ctx context.Context, snap *reviewSnapshot, p *publicationTransaction, rows []projectArtifactReview, impacts map[string]artifactImpact) error {
	manifest, _ := json.Marshal(snap.set)
	manifestRef, err := s.artifactRepository().Put(ctx, manifest)
	if err != nil {
		return err
	}
	for i := range rows {
		row := &rows[i]
		if !row.Required {
			continue
		}
		var doc publishedDocument
		for _, d := range p.candidate.Documents {
			if d.Kind+":"+d.Alias == row.Key {
				doc = d
				break
			}
		}
		preview, _ := json.Marshal(projectArtifactReview{Key: row.Key, Path: row.Path, ArtifactID: row.ArtifactID, Version: row.Version, BaseVersion: row.BaseVersion, BaseArtifact: row.BaseArtifact, Required: true})
		review := artifactReview{Version: 1, Target: projectReviewTarget(row.Key), BasePublication: projectReviewBase(snap), StorageRevision: projectReviewRevision(snap), Candidate: doc.Content, ProposedVersion: doc.Version, Impact: impacts[row.Key], Dependencies: projectReviewDependencies(manifestRef, *p.candidate.Config, row.BaseArtifact), Preview: preview}
		kept, e := s.retainArtifactReview(ctx, review)
		if e != nil {
			return e
		}
		row.Review = &kept
	}
	return nil
}

// Configuration upgrades cannot replace a published artifact. This preflight is
// read-only: failures must preserve the existing upgrade's all-files rollback.
// First identities are covered by the upgrade's explicit exact-file confirmation;
// its committed public lock and reviewed copies remain the durable first release.
// Normal publication subsequently imports that exact lock into the shared ledger.
func (s *Server) checkUpgradePublication(ctx context.Context, dir heldDir, original, snap *reviewSnapshot) error {
	ledger, err := s.readDialectPublicationLedger()
	if err != nil {
		return err
	}
	candidate, err := publicationFromSnapshot(snap)
	if err != nil {
		return err
	}
	p := &publicationTransaction{ledger: ledger, candidate: candidate}
	if original.hasLock {
		locked, _, e := lockedSet(original.lock)
		if e != nil {
			return e
		}
		// A committed lock can precede its history receipt. Do not change its
		// closure until ordinary publication reconciles the exact accepted review.
		root, _, e := openOwnFolder(s.root, strings.Split(publicationDirectory, "/"), false, "artifact versions")
		if e == nil {
			pending, readErr := readPendingPublication(root)
			root.Close()
			if readErr != nil {
				return readErr
			}
			if pending != nil && publicationMatchesSet(*pending, locked) {
				committed := false
				for _, item := range ledger.Items {
					if item.ID == pending.ID {
						committed = true
					}
				}
				if !committed {
					return errors.New("Reconcile the previous publication with Review and lock before upgrading project settings.")
				}
			}
		} else if !errors.Is(e, os.ErrNotExist) {
			return e
		}
		// Only artifact bytes are needed for version admission; the configuration
		// is intentionally changing in this independently reviewed operation.
		prior := &reviewSnapshot{config: original.config}
		for _, entry := range locked.Entries {
			raw, ok := original.bytesOf(entry.Digest)
			if !ok {
				name, valid := copyName(entry.Digest)
				if !valid {
					return errRetainedArtifactChanged
				}
				copies, e := s.reviewedCopies(false)
				if e != nil {
					return e
				}
				raw, e = readPrivateData(copies, name, reviewTextLimit)
				copies.Close()
				if e != nil || sha256Digest(raw) != entry.Digest {
					return errRetainedArtifactChanged
				}
			}
			prior.docs = append(prior.docs, snapshotDocument{entry: entry, data: raw})
		}
		base, e := publicationFromSnapshot(prior)
		if e != nil {
			return e
		}
		// The current lock may be the only identity index for a legacy release.
		// Do not retire it through a configuration upgrade unless the ordinary
		// publication ledger already retains it. Otherwise later reuse could
		// incorrectly be admitted as a first identity.
		for _, prior := range base.Documents {
			retained := false
			for _, doc := range candidate.Documents {
				if doc.Kind == prior.Kind && doc.ID == prior.ID && doc.Content == prior.Content {
					retained = true
				}
			}
			for _, publication := range ledger.Items {
				for _, doc := range publication.Documents {
					if doc.Kind == prior.Kind && doc.ID == prior.ID && doc.Content == prior.Content {
						retained = true
					}
				}
			}
			if !retained {
				return errors.New("Preserve the previous artifact release with Review and lock before upgrading project settings.")
			}
		}
		p.base = &base
		p.ledger.Items = append(append([]artifactPublication{}, ledger.Items...), base)
	}
	if err = admitPublication(p.ledger, candidate); err != nil {
		return err
	}
	if err = s.checkPublicationVersions(ctx, dir, p); err != nil {
		return err
	}
	rows, err := s.projectReviewRows(ctx, snap, p.ledger)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.Required && row.BaseVersion != "" {
			return errors.New("Publish changed packs and graphs with Review and lock before upgrading project settings.")
		}
	}
	return nil
}

// Identity-only comparison against the Runtime lock; no current editor bytes
// substitute for a historical configuration or accepted review association.
func publicationMatchesSet(p artifactPublication, set reviewSet) bool {
	if p.Config == nil || "sha256:"+p.Config.SHA256 != set.Config || len(p.Documents) != len(set.Entries) {
		return false
	}
	for _, entry := range set.Entries {
		found := false
		for _, doc := range p.Documents {
			if doc.Kind == entry.Kind && doc.Alias == entry.ID && doc.Path == entry.Path && "sha256:"+doc.Content.SHA256 == entry.Digest {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (s *Server) pendingProjectPublication(snap *reviewSnapshot) (*artifactPublication, error) {
	if !snap.hasLock {
		return nil, nil
	}
	locked, _, err := lockedSet(snap.lock)
	if err != nil {
		return nil, err
	}
	if !locked.equal(snap.set) {
		return nil, nil
	}
	root, _, err := openOwnFolder(s.root, strings.Split(publicationDirectory, "/"), false, "artifact versions")
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	pending, err := readPendingPublication(root)
	if err != nil {
		return nil, err
	}
	if pending != nil && publicationMatchesSet(*pending, locked) {
		return pending, nil
	}
	return nil, nil
}
