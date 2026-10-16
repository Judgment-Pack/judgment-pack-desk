package desk

// Publication is the reviewed project lock, not a generic file save. This ledger
// binds immutable artifact identities to retained bytes and keeps the complete
// dependency set that was reviewed, including graphs' referenced packs.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const publicationDirectory = ".desk-private/artifact-publications"
const publicationLedgerLimit = 8 << 20

type publishedDocument struct {
	Kind    string              `json:"kind"`
	Alias   string              `json:"alias"`
	ID      string              `json:"id"`
	Version string              `json:"version"`
	Path    string              `json:"path"`
	Content retainedArtifactRef `json:"content"`
}
type artifactPublication struct {
	ID        string                     `json:"id"`
	At        string                     `json:"at"`
	Imported  bool                       `json:"imported,omitempty"`
	Config    *retainedArtifactRef       `json:"config,omitempty"`
	Dialect   *dialectPublicationContext `json:"dialect,omitempty"`
	Documents []publishedDocument        `json:"documents"`
	Review    *retainedArtifactRef       `json:"review,omitempty"`
}
type publicationLedger struct {
	Version  int                   `json:"version"`
	Items    []artifactPublication `json:"items"`
	Receipts map[string]string     `json:"receipts,omitempty"`
}
type publicationTransaction struct {
	root      *os.Root
	lock      *os.File
	ledger    publicationLedger
	candidate artifactPublication
	base      *artifactPublication
}

func (p *publicationTransaction) close() {
	if p.lock != nil {
		p.lock.Close()
	}
	p.root.Close()
}
func publicationID(p artifactPublication) string {
	p.ID = ""
	p.At = ""
	p.Imported = false
	raw, _ := json.Marshal(p)
	return digestOf(raw)
}
func readPublicationLedger(root *os.Root) (publicationLedger, error) {
	raw, err := readPrivateData(root, "history.json", publicationLedgerLimit)
	if errors.Is(err, os.ErrNotExist) {
		return publicationLedger{Version: 1, Items: []artifactPublication{}}, nil
	}
	var ledger publicationLedger
	if err != nil {
		return ledger, err
	}
	if decodeDataJSON(raw, &ledger) != nil || ledger.Version != 1 || len(ledger.Items) > 4096 {
		return ledger, errors.New("Artifact version history is unreadable.")
	}
	ids := map[string]bool{}
	for _, p := range ledger.Items {
		if p.Review != nil && !p.Review.valid() || p.ID != publicationID(p) || ids[p.ID] || (p.Dialect == nil && (p.Config == nil || !p.Config.valid()) || p.Dialect != nil && !p.Dialect.valid(p)) || p.At == "" {
			return ledger, errors.New("Artifact version history is inconsistent.")
		}
		ids[p.ID] = true
		for _, d := range p.Documents {
			if d.ID == "" || d.Version == "" || (d.Kind != "pack" && d.Kind != "graph" && d.Kind != "dialect") || !d.Content.valid() {
				return ledger, errors.New("Artifact version history is inconsistent.")
			}
		}
	}
	if len(ledger.Receipts) > 4096 {
		return ledger, errors.New("Artifact publication receipts reached their storage limit.")
	}
	for request, id := range ledger.Receipts {
		if !retainedArtifactDigest.MatchString(request) || !ids[id] {
			return ledger, errors.New("Artifact publication receipt is inconsistent.")
		}
	}
	if err = admitPublication(ledger, artifactPublication{}); err != nil {
		return ledger, err
	}
	return ledger, nil
}
func admitPublication(ledger publicationLedger, candidate artifactPublication) error {
	type identity struct{ kind, id, version string }
	identities := map[identity]retainedArtifactRef{}
	for _, p := range append(append([]artifactPublication{}, ledger.Items...), candidate) {
		for _, d := range p.Documents {
			key := identity{d.Kind, d.ID, d.Version}
			if prior, ok := identities[key]; ok && prior != d.Content {
				return fmt.Errorf("The %s %s version %s already identifies different reviewed content. Choose a new artifact version before locking.", d.Kind, d.ID, d.Version)
			}
			identities[key] = d.Content
		}
	}
	return nil
}
func publicationFromSnapshot(snap *reviewSnapshot) (artifactPublication, error) {
	config := artifactReference(snap.config)
	p := artifactPublication{At: time.Now().UTC().Format(time.RFC3339Nano), Config: &config, Documents: []publishedDocument{}}
	for _, d := range snap.docs {
		var head struct {
			ID      string `json:"id"`
			Version string `json:"version"`
		}
		if json.Unmarshal(d.data, &head) != nil || head.ID == "" || head.Version == "" {
			return p, errors.New("Every reviewed pack and graph must have an artifact identity and version.")
		}
		p.Documents = append(p.Documents, publishedDocument{Kind: d.entry.Kind, Alias: d.entry.ID, ID: head.ID, Version: head.Version, Path: d.entry.Path, Content: artifactReference(d.data)})
	}
	p.ID = publicationID(p)
	return p, nil
}
func (s *Server) retainPublicationSnapshot(ctx context.Context, snap *reviewSnapshot) error {
	if _, err := s.artifactRepository().Put(ctx, snap.config); err != nil {
		return err
	}
	for _, d := range snap.docs {
		if _, err := s.artifactRepository().Put(ctx, d.data); err != nil {
			return err
		}
	}
	return nil
}

// Called only while holding the cross-process project lock. Lock ordering is
// project -> publication ledger -> retained artifacts, for every writer.
func (s *Server) beginPublication(ctx context.Context, snap *reviewSnapshot) (*publicationTransaction, error) {
	p, err := s.openPublicationTransaction(ctx)
	if err != nil {
		return nil, err
	}
	failed := true
	defer func() {
		if failed {
			p.close()
		}
	}()
	p.candidate, err = publicationFromSnapshot(snap)
	if err != nil {
		return nil, err
	}
	// Bootstrap a previous reviewed lock from exact bytes only. Never substitute
	// current edited files for the old digest or manufacture historical approval.
	if snap.hasLock {
		prior, _, parseErr := lockedSet(snap.lock)
		if parseErr == nil {
			old := &reviewSnapshot{set: prior}
			read := func(digest string) ([]byte, error) {
				if b, ok := snap.bytesOf(digest); ok {
					return b, nil
				}
				name, ok := copyName(digest)
				if !ok {
					return nil, errors.New("Invalid reviewed content identity.")
				}
				copies, e := s.reviewedCopies(false)
				if e != nil {
					return nil, e
				}
				defer copies.Close()
				b, e := readPrivateData(copies, name, reviewTextLimit)
				if e == nil && sha256Digest(b) != digest {
					e = errRetainedArtifactChanged
				}
				return b, e
			}
			old.config, err = read(prior.Config)
			if err != nil {
				return nil, fmt.Errorf("The previous reviewed configuration could not be retained: %w", err)
			}
			for _, entry := range prior.Entries {
				b, e := read(entry.Digest)
				if e != nil {
					return nil, fmt.Errorf("The previous reviewed %s could not be retained: %w", entry.Kind, e)
				}
				old.docs = append(old.docs, snapshotDocument{entry: entry, data: b})
			}
			saved, e := publicationFromSnapshot(old)
			if e != nil {
				return nil, e
			}
			saved.Imported = true
			p.base = &saved
			if e = s.retainPublicationSnapshot(ctx, old); e != nil {
				return nil, e
			}
			known := false
			for _, entry := range p.ledger.Items {
				if entry.ID == saved.ID {
					known = true
					break
				}
			}
			if !known {
				if e = admitPublication(p.ledger, saved); e != nil {
					return nil, e
				}
				p.ledger.Items = append(p.ledger.Items, saved)
				// The existing public lock is already a durable publication. Commit
				// its recovery before attempting to replace it with another lock.
				if e = writePublicationLedger(p.root, p.ledger); e != nil {
					return nil, e
				}
			}
		}
	}
	if err = admitPublication(p.ledger, p.candidate); err != nil {
		return nil, err
	}
	// Keep bytes before changing the public lock; a storage failure cannot lose
	// the previous dependency closure or expose a partially retained candidate.
	if err = s.retainPublicationSnapshot(ctx, snap); err != nil {
		return nil, err
	}
	// These verified digest-addressed copies make the public lock recoverable
	// even if the process stops before its history receipt is committed.
	if err = s.storeReviewedCopies(snap); err != nil {
		return nil, err
	}
	proposed := p.withCandidate()
	if _, err = encodePublicationLedger(proposed); err != nil {
		return nil, err
	}
	failed = false
	return p, nil
}

// Runtime owns version grammar and exact arithmetic. Publication supplies the
// compatibility decision; this only refuses a backwards or unchanged identity.
func (s *Server) checkPublicationVersions(ctx context.Context, dir heldDir, p *publicationTransaction) error {
	checked := map[string]string{}
	compare := func(base, candidate string) (string, error) {
		key := base + "/" + candidate
		if answer, ok := checked[key]; ok {
			return answer, nil
		}
		if len(checked) >= 256 {
			return "", errors.New("Too many distinct artifact version comparisons in one publication.")
		}
		raw, err := runRuntime(ctx, s.cfg.JpackBin, dir, "artifact", "version", "--base", base, "--candidate", candidate, "--format", "json")
		var reply struct{ OutputVersion, Command, Status, Base, Candidate, Comparison string }
		if err != nil || json.Unmarshal(raw, &reply) != nil || reply.OutputVersion != "2" || reply.Command != "artifact version" || reply.Status != "available" || reply.Base != base || reply.Candidate != candidate {
			return "", errors.New("Runtime could not verify the artifact version change. Use a Runtime with artifact version tooling before publishing.")
		}
		checked[key] = reply.Comparison
		return reply.Comparison, nil
	}
	for _, candidate := range p.candidate.Documents {
		// Validate every first publication too: packs lock does not validate this
		// grammar. A malformed first version must never poison later comparisons.
		if comparison, err := compare(candidate.Version, candidate.Version); err != nil {
			return err
		} else if comparison != "equal" {
			return errors.New("Runtime returned an inconsistent artifact version comparison.")
		}
		known := false
		var replaced []publishedDocument
		if p.base != nil {
			for _, base := range p.base.Documents {
				if (base.Path == candidate.Path || base.Alias == candidate.Alias) && base.Kind == candidate.Kind && base.ID == candidate.ID && base.Content != candidate.Content {
					replaced = append(replaced, base)
				}
			}
		}
		for _, publication := range p.ledger.Items {
			for _, saved := range publication.Documents {
				if saved.Kind == candidate.Kind && saved.ID == candidate.ID && saved.Content == candidate.Content {
					known = true
				}
			}
		}
		// Keeping an existing alias or adding a reference to an existing immutable
		// release is not an edit. Replacing an existing alias or path must advance
		// its version, including when that alias is redirected to another file.
		if known && len(replaced) == 0 {
			continue
		}
		// Adopting an existing immutable release only advances the replaced
		// binding(s). A genuinely new release must advance the published floor.
		bases := replaced
		if !known {
			bases = nil
			for _, publication := range p.ledger.Items {
				for _, saved := range publication.Documents {
					if saved.Kind == candidate.Kind && saved.ID == candidate.ID {
						bases = append(bases, saved)
					}
				}
			}
		}
		for _, saved := range bases {
			comparison, err := compare(saved.Version, candidate.Version)
			if err != nil {
				return err
			}
			if comparison != "greater" {
				return fmt.Errorf("The %s %s needs a version greater than %s. Restoring earlier content also requires a new version.", candidate.Kind, candidate.ID, saved.Version)
			}
		}
	}
	return nil
}
func (p *publicationTransaction) withCandidate() publicationLedger {
	ledger := publicationLedger{Version: 1, Items: append([]artifactPublication{}, p.ledger.Items...), Receipts: p.ledger.Receipts}
	for _, v := range ledger.Items {
		if v.ID == p.candidate.ID {
			return ledger
		}
	}
	ledger.Items = append(ledger.Items, p.candidate)
	return ledger
}
func encodePublicationLedger(ledger publicationLedger) ([]byte, error) {
	if len(ledger.Items) > 4096 {
		return nil, errors.New("Artifact version history reached its storage limit.")
	}
	raw, err := json.Marshal(ledger)
	if err != nil {
		return nil, err
	}
	if len(raw) > publicationLedgerLimit {
		return nil, errors.New("Artifact version history reached its storage limit.")
	}
	return raw, nil
}
func writePublicationLedger(root *os.Root, ledger publicationLedger) error {
	raw, err := encodePublicationLedger(ledger)
	if err != nil {
		return err
	}
	if previous, e := readPrivateData(root, "history.json", publicationLedgerLimit); e == nil && string(previous) == string(raw) {
		return nil
	}
	return writePrivateData(root, "history.json", raw)
}
func (p *publicationTransaction) commit() error {
	return writePublicationLedger(p.root, p.withCandidate())
}
func (s *Server) handleArtifactVersions(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	kind, id := r.URL.Query().Get("kind"), r.URL.Query().Get("id")
	before := 0
	if value := r.URL.Query().Get("before"); value != "" {
		n, e := strconv.Atoi(value)
		if e != nil || n < 1 {
			writeJSONCoded(w, 400, CodeBadRequest, "Invalid history cursor.")
			return
		}
		before = n
	}
	if (kind != "pack" && kind != "graph" && kind != "dialect") || id == "" || len(id) > 4096 {
		writeJSONCoded(w, 400, CodeBadRequest, "Choose an artifact identity.")
		return
	}
	type item struct {
		Publication artifactPublication `json:"publication"`
		Document    publishedDocument   `json:"document"`
		Raw         string              `json:"raw"`
	}
	answer := struct {
		Items []item `json:"items"`
		Next  int    `json:"next,omitempty"`
	}{Items: []item{}}
	root, _, err := openOwnFolder(s.root, strings.Split(publicationDirectory, "/"), false, "artifact versions")
	if errors.Is(err, os.ErrNotExist) {
		writeJSON(w, 200, answer)
		return
	}
	if err != nil {
		writeJSONCoded(w, 503, CodeInternal, "Artifact version history is unavailable.")
		return
	}
	defer root.Close()
	ledger, err := readPublicationLedger(root)
	if err != nil {
		writeJSONCoded(w, 503, CodeInternal, "Artifact version history is unavailable.")
		return
	}
	type row struct {
		sequence, index int
		document        publishedDocument
	}
	var rows []row
	sequence := 0
	for i, publication := range ledger.Items {
		seen := map[string]bool{}
		for _, d := range publication.Documents {
			sequence++
			if d.Kind != kind || d.ID != id {
				continue
			}
			key := d.Version + "/" + d.Content.SHA256
			if seen[key] {
				continue
			}
			seen[key] = true
			rows = append(rows, row{sequence, i, d})
		}
	}
	budget := 0
	for i := len(rows) - 1; i >= 0; i-- {
		entry := rows[i]
		if before > 0 && entry.sequence >= before {
			continue
		}
		if len(answer.Items) >= 25 || budget+entry.document.Content.Bytes > reviewReadingLimit {
			answer.Next = entry.sequence + 1
			writeJSON(w, 200, answer)
			return
		}
		raw, e := s.artifactRepository().Get(r.Context(), entry.document.Content)
		if e != nil {
			writeJSONCoded(w, 503, CodeInternal, "Retained artifact content could not be verified.")
			return
		}
		budget += len(raw)
		answer.Items = append(answer.Items, item{ledger.Items[entry.index], entry.document, string(raw)})
	}
	writeJSON(w, 200, answer)
}

// Lock ordering for all writers: project/preparation -> ledger -> retained artifacts.
func (s *Server) openPublicationTransaction(ctx context.Context) (*publicationTransaction, error) {
	root, made, err := openOwnFolder(s.root, strings.Split(publicationDirectory, "/"), true, "artifact versions")
	if err != nil {
		return nil, err
	}
	if made {
		if err = writePrivateData(root, ".gitignore", []byte("*\n")); err != nil {
			root.Close()
			return nil, err
		}
	}
	p := &publicationTransaction{root: root}
	failed := true
	defer func() {
		if failed {
			p.close()
		}
	}()
	p.lock, err = root.Open(".")
	if err != nil {
		return nil, err
	}
	if err = waitPrivateDataLock(ctx, p.lock, true); err != nil {
		return nil, err
	}
	p.ledger, err = readPublicationLedger(root)
	if err != nil {
		return nil, err
	}
	failed = false
	return p, nil
}
