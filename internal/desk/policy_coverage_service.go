package desk

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

type policyCoverageResolvedPack struct {
	Pack        policyCoveragePack
	Publication artifactPublication
	Raw         []byte
}
type policyCoverageResolveRequest struct {
	PolicyPublicationID string                     `json:"policyPublicationId"`
	Pack                policyCoveragePackSelector `json:"pack"`
}

func (v *policyCoverageResolveRequest) UnmarshalJSON(raw []byte) error {
	type plain policyCoverageResolveRequest
	var p plain
	if coverageClosedJSON(raw, &p, "policyPublicationId", "pack") != nil || !policyEventIDForm.MatchString(p.PolicyPublicationID) || !strings.HasPrefix(p.PolicyPublicationID, "publication_") {
		return errors.New("Choose exact Policy and Pack publications.")
	}
	*v = policyCoverageResolveRequest(p)
	return nil
}

type policyCoverageBasis struct {
	PolicyRevision string                  `json:"policyRevision"`
	Source         policyAuthoringSource   `json:"source"`
	Pack           policyCoveragePack      `json:"pack"`
	Inventory      policyCoverageInventory `json:"inventory"`
	Recovery       *policyRecoveryView     `json:"recovery,omitempty"`
}
type policyCoverageMaterial struct {
	Content        policyCoverageContent
	Pack           []byte
	Original       []byte
	OriginalSource *policySource
}

func validatePolicyCoverages(ledger policyLedger) error {
	bad := errors.New("Policy coverage storage is inconsistent")
	if len(ledger.Coverages) > policyCoverageCountLimit {
		return bad
	}
	snapshots := map[string]policyRecoverySnapshot{}
	for _, r := range ledger.Recoveries {
		snapshots[r.SnapshotID] = r
	}
	for key, record := range ledger.Coverages {
		if record.SchemaVersion != 1 || key != record.ID || !policyCoverageIDForm.MatchString(key) || ledger.Policies[record.PolicyID].ID == "" || !validPolicyTime(record.CreatedAt) || !validPolicyTime(record.UpdatedAt) || len(record.Candidates) < 1 || len(record.Candidates) > policyHistoryLimit || record.Reviews == nil || len(record.Reviews) > policyHistoryLimit {
			return bad
		}
		total := len(record.Candidates) + len(record.Reviews)
		events := make([]string, total+1)
		candidates := map[string]policyCoverageCandidateMetadata{}
		reviews := map[string]bool{}
		last := 0
		for _, c := range record.Candidates {
			if !policyCoverageCandidateIDForm.MatchString(c.ID) || candidates[c.ID].ID != "" || c.Sequence <= last || c.Sequence > total || !c.Content.valid() || c.Content.Bytes < 1 || c.Content.Bytes > policyCoverageCandidateLimit || c.Digest != c.Content.SHA256 || !validPolicyActor(c.Actor) || !validPolicyTime(c.At) || !validCoveragePackSelector(c.Pack) || !contains([]string{"implemented", "manual", "awaiting-interpretation", "out-of-scope"}, c.Status) {
				return bad
			}
			found := false
			for _, p := range ledger.Policies[record.PolicyID].Publications {
				if p.ID == c.PolicyPublicationID {
					found = true
				}
			}
			if !found {
				return bad
			}
			events[c.Sequence] = c.ID
			candidates[c.ID] = c
			last = c.Sequence
		}
		last = 0
		for _, r := range record.Reviews {
			c := candidates[r.CandidateID]
			if !policyCoverageReviewIDForm.MatchString(r.ID) || reviews[r.ID] || c.ID == "" || r.Sequence <= last || r.Sequence > total || events[r.Sequence] != "" || r.CandidateDigest != c.Digest || !retainedArtifactDigest.MatchString(r.PolicyRevision) || !contains([]string{"approve", "request-changes"}, r.Decision) || !validPolicyActor(r.Actor) || !validPolicyTime(r.At) || len(r.Comment) > 8192 || (r.Decision == "approve") != (r.Check != nil) || r.Check != nil && (!r.Check.valid() || r.Check.Bytes > policyCoverageCheckByteLimit) {
				return bad
			}
			events[r.Sequence] = r.ID
			reviews[r.ID] = true
			last = r.Sequence
		}
		current, review := "", ""
		at := ""
		for sequence := 1; sequence <= total; sequence++ {
			id := events[sequence]
			if c := candidates[id]; c.ID != "" {
				current, review, at = id, "", c.At
				continue
			}
			found := false
			for _, r := range record.Reviews {
				if r.ID == id {
					if r.CandidateID != current {
						return bad
					}
					review, at = r.ID, r.At
					found = true
					break
				}
			}
			if !found {
				return bad
			}
		}
		if current != record.HeadCandidateID || review != record.CurrentReviewID || at != record.UpdatedAt || record.CreatedAt != record.Candidates[0].At {
			return bad
		}
		if r := record.Recovery; r != nil {
			snap := snapshots[r.SnapshotID]
			if snap.SnapshotID == "" || snap.At != r.RestoredAt || r.Sequence < 1 || r.Sequence > total {
				return bad
			}
		}
	}
	return nil
}
func validCoveragePackSelector(p policyCoveragePackSelector) bool {
	raw, _ := json.Marshal(p)
	var checked policyCoveragePackSelector
	return json.Unmarshal(raw, &checked) == nil
}
func coveragePublication(record policyStoredRecord, id string) (policyPublication, policyRevision, error) {
	for _, p := range record.Publications {
		if p.ID == id {
			for _, r := range record.Revisions {
				if r.ID == p.CandidateRevisionID && r.Digest == p.CandidateDigest && r.Content == p.Content && r.ArtifactVersion == p.ArtifactVersion {
					return p, r, nil
				}
			}
		}
	}
	return policyPublication{}, policyRevision{}, policyRefusal(404, CodeNotFound, "The selected exact Policy publication is unavailable.")
}
func coverageSourceMatches(ctx context.Context, s policyService, record policyStoredRecord, source policyAuthoringSource) error {
	p, r, err := coveragePublication(record, source.PublicationID)
	if err != nil {
		return err
	}
	c, err := s.candidate(ctx, r)
	if err != nil {
		return err
	}
	if source.Version != 1 || source.PolicyID != record.ID || source.RevisionID != r.ID || source.CandidateDigest != p.CandidateDigest || source.Content != p.Content || source.Title != c.Title || source.ArtifactVersion != c.ArtifactVersion || source.Narrative != c.Narrative {
		return errors.New("Retained coverage Policy source is inconsistent")
	}
	return nil
}
func coverageInventory(raw []byte) (policyCoverageInventory, error) {
	result := policyCoverageInventory{Rules: []string{}, Exceptions: []string{}}
	if rejectDuplicateMembers(raw) != nil {
		return result, errors.New("The retained Pack contains duplicate members.")
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal(raw, &doc) != nil || doc == nil {
		return result, errors.New("The retained Pack inventory is unavailable.")
	}
	list := func(key string) ([]string, error) {
		out := []string{}
		var rows []struct {
			ID string `json:"id"`
		}
		if doc[key] != nil && json.Unmarshal(doc[key], &rows) != nil {
			return nil, errors.New("The retained Pack inventory is unavailable.")
		}
		if len(rows) > 4096 {
			return nil, errors.New("The retained Pack inventory exceeds the display limit.")
		}
		for _, row := range rows {
			if row.ID == "" || len(row.ID) > 4096 {
				return nil, errors.New("The retained Pack inventory is unavailable.")
			}
			out = append(out, row.ID)
		}
		return out, nil
	}
	var err error
	result.Rules, err = list("rules")
	if err != nil {
		return result, err
	}
	result.Exceptions, err = list("exceptions")
	if err != nil {
		return result, err
	}
	_, result.Applicability = doc["applicability"]
	_, result.Fallback = doc["fallbackOutcome"]
	return result, nil
}
func validCoverageWitness(c policyCoverageContent) bool {
	if c.SchemaVersion != 1 || c.Scope.Kind != "whole-narrative" || c.Statement.validate() != nil || c.Publication.ID != c.Pack.PublicationID || !retainedArtifactDigest.MatchString(c.Publication.ID) || c.Publication.ID != publicationID(c.Publication) || c.Publication.At == "" || c.Pack.Document.Kind != "pack" || !validCoveragePackSelector(coveragePackSelector(c.Pack)) {
		return false
	}
	found := false
	for _, d := range c.Publication.Documents {
		if d == c.Pack.Document {
			found = true
		}
	}
	return found
}
func (s policyService) coverageMaterial(ctx context.Context, record policyStoredRecord, metadata policyCoverageCandidateMetadata) (policyCoverageMaterial, error) {
	var material policyCoverageMaterial
	raw, err := s.artifacts.Get(ctx, metadata.Content)
	if err != nil {
		return material, err
	}
	if len(raw) > policyCoverageCandidateLimit || rejectDuplicateMembers(raw) != nil || decodeDataJSON(raw, &material.Content) != nil || !validCoverageWitness(material.Content) || metadata.Digest != digestOf(raw) || metadata.PolicyPublicationID != material.Content.Source.PublicationID || metadata.Pack != coveragePackSelector(material.Content.Pack) || metadata.Status != material.Content.Statement.Status {
		return material, errors.New("Retained coverage candidate is inconsistent")
	}
	if err = coverageSourceMatches(ctx, s, record, material.Content.Source); err != nil {
		return material, err
	}
	material.Pack, err = s.artifacts.Get(ctx, material.Content.Pack.Document.Content)
	if err != nil {
		return material, err
	}
	var head struct {
		ID      string `json:"id"`
		Version string `json:"version"`
	}
	if rejectDuplicateMembers(material.Pack) != nil || json.Unmarshal(material.Pack, &head) != nil || head.ID != material.Content.Pack.Document.ID || head.Version != material.Content.Pack.Document.Version {
		return material, errors.New("Retained coverage Pack identity is inconsistent")
	}
	_, revision, err := coveragePublication(record, material.Content.Source.PublicationID)
	if err != nil {
		return material, err
	}
	candidate, err := s.candidate(ctx, revision)
	if err != nil {
		return material, err
	}
	material.OriginalSource = candidate.Source
	if candidate.Source != nil {
		material.Original, err = s.artifacts.Get(ctx, candidate.Source.Content)
	}
	return material, err
}
func (s policyService) coverageAvailability(ctx context.Context, p policyCoveragePackSelector) policyCoverageAvailability {
	if s.coveragePack == nil {
		return policyCoverageAvailability{PackPublication: "unavailable", Reason: "The original Pack publication cannot be checked."}
	}
	_, err := s.coveragePack(ctx, p)
	if err == nil {
		return policyCoverageAvailability{PackPublication: "present"}
	}
	var failure *policyFailure
	if errors.As(err, &failure) && failure.Status == 404 {
		return policyCoverageAvailability{PackPublication: "missing", Reason: "The original exact Pack publication is missing. Retained history remains inspectable; restore its publication before a new approval."}
	}
	return policyCoverageAvailability{PackPublication: "unavailable", Reason: "The original Pack publication is unavailable. Keep this review draft and check publication storage."}
}
func (s policyService) coverageReply(ctx context.Context, policy policyStoredRecord, authority *policySourceAuthorityStream, record policyCoverageRecord, candidateID string, availability policyCoverageAvailability) (policyCoverageReply, error) {
	if candidateID == "" {
		candidateID = record.HeadCandidateID
	}
	var metadata policyCoverageCandidateMetadata
	for _, c := range record.Candidates {
		if c.ID == candidateID {
			metadata = c
			break
		}
	}
	if metadata.ID == "" {
		return policyCoverageReply{}, policyRefusal(404, CodeNotFound, "Coverage candidate not found.")
	}
	material, err := s.coverageMaterial(ctx, policy, metadata)
	if err != nil {
		return policyCoverageReply{}, err
	}
	inventory, err := coverageInventory(material.Pack)
	if err != nil {
		return policyCoverageReply{}, err
	}
	state, review := coverageReviewState(record, candidateID)
	c := material.Content
	reply := policyCoverageReply{Revision: coverageRevision(record), PolicyRevision: policyEffectiveRevision(policy, authority), Document: policyCoverageDocument{SchemaVersion: 1, ID: record.ID, PolicyID: record.PolicyID, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt, Candidate: policyCoverageCandidate{ID: metadata.ID, Digest: metadata.Digest, Content: metadata.Content, Source: c.Source, Pack: c.Pack, Scope: c.Scope, Statement: c.Statement}, ReviewState: state, CurrentReviewID: review, Candidates: record.Candidates, Reviews: record.Reviews}, Recovery: coverageRecoveryView(record, candidateID), Inventory: inventory, Availability: availability}
	return reply, s.authorized(ctx)
}
func (s policyService) coverageGet(ctx context.Context, id, coverageID, candidateID string) (policyCoverageReply, error) {
	var policy policyStoredRecord
	var authority *policySourceAuthorityStream
	var record policyCoverageRecord
	err := s.repository.View(ctx, func(l policyLedger) error {
		if err := s.authorized(ctx); err != nil {
			return err
		}
		policy = l.Policies[id]
		record = l.Coverages[coverageID]
		authority = l.SourceAuthorities[id]
		if policy.ID == "" || record.PolicyID != id {
			return policyRefusal(404, CodeNotFound, "Coverage review not found.")
		}
		return nil
	})
	if err != nil {
		return policyCoverageReply{}, err
	}
	metadata := coverageHead(record)
	if candidateID != "" {
		for _, c := range record.Candidates {
			if c.ID == candidateID {
				metadata = c
				break
			}
		}
	}
	availability := s.coverageAvailability(ctx, metadata.Pack)
	return s.coverageReply(ctx, policy, authority, record, candidateID, availability)
}
func (s policyService) coverageList(ctx context.Context, id string) (any, error) {
	answer := struct {
		Items    []policyCoverageSummary `json:"items"`
		Recovery *policyRecoveryView     `json:"recovery,omitempty"`
	}{Items: []policyCoverageSummary{}}
	var policy policyStoredRecord
	var records []policyCoverageRecord
	err := s.repository.View(ctx, func(l policyLedger) error {
		if err := s.authorized(ctx); err != nil {
			return err
		}
		policy = l.Policies[id]
		if policy.ID == "" {
			return policyRefusal(404, CodeNotFound, "Policy not found.")
		}
		answer.Recovery = policyRecoveryProjection(policy, l.SourceAuthorities[id])
		for _, r := range l.Coverages {
			if r.PolicyID == id {
				records = append(records, r)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, r := range records {
		h := coverageHead(r)
		publication, _, err := coveragePublication(policy, h.PolicyPublicationID)
		if err != nil {
			return nil, err
		}
		state, _ := coverageReviewState(r, h.ID)
		answer.Items = append(answer.Items, policyCoverageSummary{ID: r.ID, PolicyID: id, Revision: coverageRevision(r), CandidateID: h.ID, CandidateDigest: h.Digest, PolicyPublicationID: h.PolicyPublicationID, PolicyVersion: publication.ArtifactVersion, Pack: h.Pack, Status: h.Status, ReviewState: state, UpdatedAt: r.UpdatedAt, Recovery: coverageRecoveryView(r, h.ID)})
	}
	sort.Slice(answer.Items, func(i, j int) bool {
		if answer.Items[i].UpdatedAt == answer.Items[j].UpdatedAt {
			return answer.Items[i].ID < answer.Items[j].ID
		}
		return answer.Items[i].UpdatedAt > answer.Items[j].UpdatedAt
	})
	return answer, s.authorized(ctx)
}
func (s policyService) coverageResolve(ctx context.Context, id string, q policyCoverageResolveRequest) (policyCoverageBasis, error) {
	var basis policyCoverageBasis
	source, err := s.authoringSource(ctx, id, "", q.PolicyPublicationID)
	if err != nil {
		return basis, err
	}
	if s.coveragePack == nil {
		return basis, policyRefusal(503, "storage-unavailable", "Pack publications are unavailable.")
	}
	pack, err := s.coveragePack(ctx, q.Pack)
	if err != nil {
		return basis, err
	}
	inventory, err := coverageInventory(pack.Raw)
	if err != nil {
		return basis, policyRefusal(422, "unsupported-pack", err.Error())
	}
	err = s.repository.View(ctx, func(l policyLedger) error {
		if err := s.authorized(ctx); err != nil {
			return err
		}
		record := l.Policies[id]
		if err := coverageSourceMatches(ctx, s, record, source); err != nil {
			return err
		}
		basis = policyCoverageBasis{PolicyRevision: policyEffectiveRevision(record, l.SourceAuthorities[id]), Source: source, Pack: pack.Pack, Inventory: inventory, Recovery: policyRecoveryProjection(record, l.SourceAuthorities[id])}
		return nil
	})
	return basis, err
}
func coverageValidateRequest(q policyCoverageRequest) error {
	if !policyOperationIDForm.MatchString(q.OperationID) || q.CoverageID != "" && !policyCoverageIDForm.MatchString(q.CoverageID) {
		return policyRefusal(400, CodeBadRequest, "Provide a supported coverage action and unique operation ID.")
	}
	if q.Action == "save" {
		if (q.CoverageID == "") != (q.BaseRevision == "") || q.CoverageID != "" && !retainedArtifactDigest.MatchString(q.BaseRevision) || !policyEventIDForm.MatchString(q.PolicyPublicationID) || !strings.HasPrefix(q.PolicyPublicationID, "publication_") || q.Pack == nil || !validCoveragePackSelector(*q.Pack) || q.Scope == nil || q.Scope.Kind != "whole-narrative" || q.Statement == nil || q.Statement.validate() != nil {
			return policyRefusal(400, CodeBadRequest, "Choose exact publications, whole narrative scope and a supported coverage declaration.")
		}
		return nil
	}
	if q.Action != "review" || q.CoverageID == "" || !retainedArtifactDigest.MatchString(q.BaseRevision) || !retainedArtifactDigest.MatchString(q.CandidateDigest) || !retainedArtifactDigest.MatchString(q.PolicyRevision) || !contains([]string{"approve", "request-changes"}, q.Decision) || len(q.Comment) > 8192 {
		return policyRefusal(400, CodeBadRequest, "Review the exact saved coverage candidate and current Policy context.")
	}
	return nil
}
func coverageCommandRecord(l policyLedger, id string, q policyCoverageRequest) (policyStoredRecord, policyCoverageRecord, error) {
	p := l.Policies[id]
	r := l.Coverages[q.CoverageID]
	if p.ID == "" {
		return p, r, policyRefusal(404, CodeNotFound, "Policy not found.")
	}
	if q.CoverageID != "" {
		if r.ID == "" || r.PolicyID != id {
			return p, r, policyRefusal(404, CodeNotFound, "Coverage review not found.")
		}
		if coverageRevision(r) != q.BaseRevision {
			f := policyRefusal(409, "revision-conflict", "This coverage record changed. Keep your declaration and reload its saved revision.")
			f.CurrentRevision = coverageRevision(r)
			return p, r, f
		}
	}
	if q.Action == "review" {
		if coverageHead(r).Digest != q.CandidateDigest {
			return p, r, policyRefusal(409, "revision-conflict", "Review the exact current saved coverage candidate.")
		}
		current := policyEffectiveRevision(p, l.SourceAuthorities[id])
		if current != q.PolicyRevision {
			f := policyRefusal(409, "revision-conflict", "The Policy context changed. Refresh that context while keeping the selected publication and declaration.")
			f.CurrentPolicyRevision = current
			return p, r, f
		}
	}
	return p, r, nil
}
func (s policyService) coverageCommand(ctx context.Context, id string, actor policyActor, q policyCoverageRequest) (policyMutationReply, error) {
	empty := policyMutationReply{}
	if err := s.authorized(ctx); err != nil {
		return empty, err
	}
	if err := coverageValidateRequest(q); err != nil {
		return empty, err
	}
	raw, _ := json.Marshal(struct {
		Kind, PolicyID string
		Actor          policyActor
		Request        policyCoverageRequest
	}{"coverage-" + q.Action, id, actor, q})
	command := policyCommand{Kind: "coverage-" + q.Action, PolicyID: id, OperationID: q.OperationID, RequestDigest: digestOf(raw), Actor: actor}
	var recovered *policyMutationReply
	var inspected policyCoverageRecord
	var policy policyStoredRecord
	err := s.repository.View(ctx, func(l policyLedger) error {
		if err := s.authorized(ctx); err != nil {
			return err
		}
		var err error
		recovered, err = s.recovered(ctx, l, command)
		if err != nil || recovered != nil {
			return err
		}
		policy, inspected, err = coverageCommandRecord(l, id, q)
		return err
	})
	if err != nil {
		return empty, err
	}
	if recovered != nil {
		return *recovered, nil
	}
	var material policyCoverageMaterial
	availability := policyCoverageAvailability{PackPublication: "present"}
	var checked *policyCoverageCheckReply
	if q.Action == "save" {
		source, err := s.authoringSource(ctx, id, "", q.PolicyPublicationID)
		if err != nil {
			return empty, err
		}
		if s.coveragePack == nil {
			return empty, policyRefusal(503, "storage-unavailable", "Pack publication storage is unavailable.")
		}
		pack, err := s.coveragePack(ctx, *q.Pack)
		if err != nil {
			return empty, err
		}
		if _, err = coverageInventory(pack.Raw); err != nil {
			return empty, policyRefusal(422, "unsupported-pack", err.Error())
		}
		material = policyCoverageMaterial{Content: policyCoverageContent{SchemaVersion: 1, Source: source, Pack: pack.Pack, Publication: pack.Publication, Scope: *q.Scope, Statement: *q.Statement}, Pack: pack.Raw}
	} else {
		material, err = s.coverageMaterial(ctx, policy, coverageHead(inspected))
		if err != nil {
			return empty, policyBeforeCommitFailure(err)
		}
		availability = s.coverageAvailability(ctx, coveragePackSelector(material.Content.Pack))
		if q.Decision == "approve" {
			if availability.PackPublication != "present" {
				return empty, policyRefusal(409, "publication-unavailable", availability.Reason)
			}
			check := s.coverageRunCheck(ctx, material)
			checked = &check
			if err := s.authorized(ctx); err != nil {
				return empty, err
			}
			if !coverageCheckAccepted(check) {
				code := "coverage-check-failed"
				status := 422
				if check.Availability == "unavailable" {
					code = "runtime-unavailable"
					status = 503
				}
				if check.Availability == "unsupported" {
					code = "unsupported-contract"
				}
				return empty, policyRefusal(status, code, "Runtime could not confirm these exact coverage references. Keep the draft and review its explicit check findings.")
			}
		}
	}
	var result policyMutationReply
	err = s.repository.Update(ctx, func(l *policyLedger) (bool, error) {
		if err := s.authorized(ctx); err != nil {
			return false, err
		}
		recovered, err := s.recovered(ctx, *l, command)
		if err != nil {
			return false, err
		}
		if recovered != nil {
			result = *recovered
			return false, nil
		}
		if len(l.Operations) >= policyOperationLimit {
			return false, policyRefusal(409, "storage-capacity", "Policy operation history has reached capacity.")
		}
		p, r, err := coverageCommandRecord(*l, id, q)
		if err != nil {
			return false, err
		}
		if err = coverageSourceMatches(ctx, s, p, material.Content.Source); err != nil {
			return false, err
		}
		if _, err = s.artifacts.Put(ctx, material.Pack); err != nil {
			return false, err
		}
		now := time.Now()
		if s.clock != nil {
			now = s.clock()
		}
		at := now.UTC().Format("2006-01-02T15:04:05Z")
		if r.ID == "" {
			if len(l.Coverages) >= policyCoverageCountLimit {
				return false, policyRefusal(409, "storage-capacity", "Coverage records have reached capacity.")
			}
			key, err := newPolicyEventID("coverage")
			if err != nil {
				return false, err
			}
			r = policyCoverageRecord{SchemaVersion: 1, ID: key, PolicyID: id, CreatedAt: at, UpdatedAt: at, Candidates: []policyCoverageCandidateMetadata{}, Reviews: []policyCoverageReview{}}
		}
		sequence := len(r.Candidates) + len(r.Reviews) + 1
		reviewID := ""
		if q.Action == "save" {
			raw, _ := json.Marshal(material.Content)
			if len(raw) > policyCoverageCandidateLimit {
				return false, policyRefusal(413, "storage-capacity", "This coverage candidate exceeds its supported retained size.")
			}
			content, err := s.artifacts.Put(ctx, raw)
			if err != nil {
				return false, err
			}
			if coverageHead(r).Digest != content.SHA256 {
				if len(r.Candidates) >= policyHistoryLimit {
					return false, policyRefusal(409, "storage-capacity", "Coverage candidate history has reached capacity.")
				}
				key, err := newPolicyEventID("coverage_candidate")
				if err != nil {
					return false, err
				}
				r.Candidates = append(r.Candidates, policyCoverageCandidateMetadata{Status: q.Statement.Status, ID: key, Sequence: sequence, Digest: content.SHA256, Content: content, PolicyPublicationID: q.PolicyPublicationID, Pack: *q.Pack, At: at, Actor: actor})
				r.HeadCandidateID, r.CurrentReviewID, r.UpdatedAt = key, "", at
			}
		} else {
			if len(r.Reviews) >= policyHistoryLimit {
				return false, policyRefusal(409, "storage-capacity", "Coverage review history has reached capacity.")
			}
			key, err := newPolicyEventID("coverage_review")
			if err != nil {
				return false, err
			}
			head := coverageHead(r)
			review := policyCoverageReview{ID: key, Sequence: sequence, CandidateID: head.ID, CandidateDigest: head.Digest, PolicyRevision: q.PolicyRevision, Decision: q.Decision, Comment: q.Comment, At: at, Actor: actor}
			if checked != nil {
				checked.CoverageRevision = q.BaseRevision
				checked.CandidateDigest = head.Digest
				checked.PolicyRevision = q.PolicyRevision
				if checked.prepared == nil {
					return false, errors.New("Runtime coverage check materials are unavailable")
				}
				retainedCheck := policyCoverageRetainedCheck{SchemaVersion: 1, Check: *checked, Materials: []retainedArtifactRef{}}
				var retainErr error
				retainedCheck.Document, retainErr = s.artifacts.Put(ctx, checked.prepared.Document)
				if retainErr != nil {
					return false, retainErr
				}
				retainedDigests := map[string]bool{}
				for _, material := range checked.prepared.Materials {
					if retainedDigests[digestOf(material)] {
						continue
					}
					retainedDigests[digestOf(material)] = true
					ref, err := s.artifacts.Put(ctx, material)
					if err != nil {
						return false, err
					}
					retainedCheck.Materials = append(retainedCheck.Materials, ref)
				}
				checkRaw, _ := json.Marshal(retainedCheck)
				ref, err := s.artifacts.Put(ctx, checkRaw)
				if err != nil {
					return false, err
				}
				review.Check = &ref
			}
			r.Reviews = append(r.Reviews, review)
			r.CurrentReviewID, r.UpdatedAt, reviewID = key, at, key
		}
		if l.Coverages == nil {
			l.Coverages = map[string]policyCoverageRecord{}
		}
		l.Coverages[r.ID] = r
		reply, err := s.coverageReply(ctx, p, l.SourceAuthorities[id], r, "", availability)
		if err != nil {
			return false, err
		}
		head := coverageHead(r)
		receipt := policyOperationReceipt{OperationID: q.OperationID, Kind: command.Kind, PolicyID: id, RequestDigest: command.RequestDigest, At: at, Actor: actor, Revision: reply.PolicyRevision, CandidateDigest: head.Digest, ReviewID: reviewID, CoverageID: r.ID, CoverageRevision: reply.Revision, CoverageCandidateID: head.ID}
		result = policyMutationReply{Delivery: "committed", OutcomeKind: "coverage", PolicyID: id, PolicyRevision: reply.PolicyRevision, Coverage: &reply, Receipt: receipt}
		encoded, _ := json.Marshal(result)
		ref, err := s.artifacts.Put(ctx, encoded)
		if err != nil {
			return false, err
		}
		l.Operations[q.OperationID] = policyStoredOperation{OperationID: q.OperationID, Kind: command.Kind, PolicyID: id, RequestDigest: command.RequestDigest, Result: ref}
		return true, s.authorized(ctx)
	})
	return result, err
}
func (s policyService) coverageRunCheck(ctx context.Context, material policyCoverageMaterial) policyCoverageCheckReply {
	if s.checkCoverage == nil {
		return policyCoverageCheckReply{Availability: "unavailable", Reason: "This Runtime does not provide the experimental Policy coverage checks."}
	}
	return s.checkCoverage(ctx, material)
}
func coverageCheckAccepted(reply policyCoverageCheckReply) bool {
	var report policyCoverageRuntimeReport
	return reply.Availability == "available" && json.Unmarshal(reply.Report, &report) == nil && report.Status == "references-valid"
}
func (s policyService) coverageCheck(ctx context.Context, id, coverageID string, q policyCoverageCheckRequest) (policyCoverageCheckReply, error) {
	var policy policyStoredRecord
	var record policyCoverageRecord
	err := s.repository.View(ctx, func(l policyLedger) error {
		if err := s.authorized(ctx); err != nil {
			return err
		}
		request := policyCoverageRequest{Action: "review", CoverageID: coverageID, BaseRevision: q.BaseRevision, CandidateDigest: q.CandidateDigest, PolicyRevision: q.PolicyRevision}
		var err error
		policy, record, err = coverageCommandRecord(l, id, request)
		return err
	})
	if err != nil {
		return policyCoverageCheckReply{}, err
	}
	material, err := s.coverageMaterial(ctx, policy, coverageHead(record))
	if err != nil {
		return policyCoverageCheckReply{}, err
	}
	reply := s.coverageRunCheck(ctx, material)
	reply.CoverageRevision = q.BaseRevision
	reply.CandidateDigest = q.CandidateDigest
	reply.PolicyRevision = q.PolicyRevision
	return reply, s.authorized(ctx)
}
func validPolicyCoverageResult(r policyMutationReply) bool {
	if !strings.HasPrefix(r.Receipt.Kind, "coverage-") {
		return r.Coverage == nil && r.Receipt.CoverageID == "" && r.Receipt.CoverageRevision == "" && r.Receipt.CoverageCandidateID == ""
	}
	c := r.Coverage
	if c == nil || r.OutcomeKind != "coverage" || !contains([]string{"coverage-save", "coverage-review"}, r.Receipt.Kind) || r.Lifecycle != nil || r.SourceStatus != nil || r.SourceSync != nil || c.Document.SchemaVersion != 1 || c.Document.PolicyID != r.Receipt.PolicyID || c.Document.ID != r.Receipt.CoverageID || c.PolicyRevision != r.Receipt.Revision || c.Revision != r.Receipt.CoverageRevision || c.Document.Candidate.ID != r.Receipt.CoverageCandidateID || c.Document.Candidate.Digest != r.Receipt.CandidateDigest || c.Document.Candidate.Content.SHA256 != r.Receipt.CandidateDigest || !policyCoverageIDForm.MatchString(c.Document.ID) || !policyCoverageCandidateIDForm.MatchString(c.Document.Candidate.ID) || !retainedArtifactDigest.MatchString(c.Revision) || !contains([]string{"unreviewed", "reviewed", "changes-requested"}, c.Document.ReviewState) {
		return false
	}
	if r.Receipt.Kind == "coverage-review" {
		if len(c.Document.Reviews) == 0 {
			return false
		}
		review := c.Document.Reviews[len(c.Document.Reviews)-1]
		return review.ID == r.Receipt.ReviewID && review.CandidateID == r.Receipt.CoverageCandidateID && review.Actor == r.Receipt.Actor && review.At == r.Receipt.At
	}
	return r.Receipt.ReviewID == ""
}
