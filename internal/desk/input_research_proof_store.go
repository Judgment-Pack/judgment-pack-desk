package desk

// Research proof manifests remain part of the local operation journal. Payloads
// use the workspace artifact repository. A manifest is published only after every
// referenced object is durable; a failed write is never retried here. Orphaned
// objects are retained for recovery, not deleted based on an uncertain outcome.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
)

type storedResearchProof struct {
	StorageVersion int                  `json:"storageVersion"`
	Session        string               `json:"session"`
	Source         string               `json:"source"`
	Pin            inputResearchPin     `json:"pin"`
	Arguments      retainedArtifactRef  `json:"arguments"`
	Response       retainedArtifactRef  `json:"response"`
	Seal           *retainedArtifactRef `json:"seal,omitempty"`
}
type researchProofSnapshot struct {
	Raw   []byte
	Proof inputResearchProof
	Bytes int
}

var errResearchProofConflict = errors.New("retained research proof cannot be replaced")
var errResearchProofStorageLimit = errors.New("Research reached its saved-source storage limit. Use fewer or smaller documents in a new run. No automatic retry was made.")

func researchProofName(id string) (string, error) {
	if !researchProofID.MatchString(id) {
		return "", errors.New("invalid retained source identity")
	}
	return id + ".proof.json", nil
}
func storedProofMetadata(raw []byte) (*storedResearchProof, int, error) {
	var members map[string]json.RawMessage
	if json.Unmarshal(raw, &members) != nil {
		return nil, 0, errors.New("invalid retained source proof")
	}
	if _, present := members["storageVersion"]; !present {
		return nil, len(raw), nil
	}
	var m storedResearchProof
	if len(raw) > 8<<10 || decodeDataJSON(raw, &m) != nil || m.StorageVersion != 1 || !m.Arguments.valid() || !m.Response.valid() || m.Seal != nil && !m.Seal.valid() {
		return nil, 0, errors.New("invalid retained source manifest")
	}
	size := len(raw) + m.Arguments.Bytes + m.Response.Bytes
	if m.Seal != nil {
		size += m.Seal.Bytes
	}
	if size > inputResearchSourceProofLimit {
		return nil, 0, errors.New("retained source proof exceeds its storage limit")
	}
	return &m, size, nil
}
func resolveResearchProof(ctx context.Context, artifacts retainedArtifactRepository, raw []byte) (researchProofSnapshot, error) {
	snap := researchProofSnapshot{Raw: raw}
	m, size, err := storedProofMetadata(raw)
	if err != nil {
		return snap, err
	}
	snap.Bytes = size
	if m == nil {
		err = decodeDataJSON(raw, &snap.Proof)
		return snap, err // legacy proofs stay byte-for-byte unchanged
	}
	p := inputResearchProof{Session: m.Session, Source: m.Source, Pin: m.Pin}
	for _, item := range []struct {
		ref  *retainedArtifactRef
		dest *json.RawMessage
	}{
		{&m.Arguments, &p.Arguments}, {&m.Response, &p.Response}, {m.Seal, &p.Seal},
	} {
		if item.ref == nil {
			continue
		}
		b, e := artifacts.Get(ctx, *item.ref)
		if e != nil {
			return snap, e
		}
		// Check at the consumer boundary too; a future adapter cannot silently
		// substitute a decoded/re-encoded object for the retained acquisition.
		if e = verifyRetainedArtifact(*item.ref, b); e != nil {
			return snap, e
		}
		if !json.Valid(b) {
			return snap, errors.New("retained source artifact is not JSON")
		}
		*item.dest = b
	}
	snap.Proof = p
	return snap, nil
}
func (s *Server) loadResearchProof(ctx context.Context, root *os.Root, id string) (researchProofSnapshot, error) {
	name, err := researchProofName(id)
	if err != nil {
		return researchProofSnapshot{}, err
	}
	raw, err := readPrivateData(root, name, inputResearchSourceProofLimit)
	if err != nil {
		return researchProofSnapshot{}, err
	}
	return resolveResearchProof(ctx, s.artifactRepository(), raw)
}

// Size is used inside the attempt journal transaction, including failed
// extractions. It does not reacquire that transaction or contact a provider.
func retainedResearchProofSize(root *os.Root, id string) (int, error) {
	name, err := researchProofName(id)
	if err != nil {
		return 0, err
	}
	raw, err := readPrivateData(root, name, inputResearchSourceProofLimit)
	if err != nil {
		return 0, err
	}
	_, size, err := storedProofMetadata(raw)
	return size, err
}
func sameResearchAcquisition(a, b inputResearchProof) bool {
	return a.Session == b.Session && a.Source == b.Source && a.Pin == b.Pin && bytes.Equal(a.Arguments, b.Arguments) && bytes.Equal(a.Response, b.Response)
}
func (s *Server) saveResearchProof(ctx context.Context, root *os.Root, id string, proof inputResearchProof, retainedBytes int) (int, error) {
	name, err := researchProofName(id)
	if err != nil {
		return 0, err
	}
	m := storedResearchProof{StorageVersion: 1, Session: proof.Session, Source: proof.Source, Pin: proof.Pin, Arguments: artifactReference(proof.Arguments), Response: artifactReference(proof.Response)}
	if len(proof.Seal) > 0 {
		ref := artifactReference(proof.Seal)
		m.Seal = &ref
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return 0, err
	}
	_, size, err := storedProofMetadata(raw)
	if err != nil {
		return 0, err
	}
	if retainedBytes < 0 || retainedBytes > inputResearchRetainedByteLimit-size-inputResearchSourceMetadataReserve {
		return 0, errResearchProofStorageLimit
	}
	for _, b := range []json.RawMessage{proof.Arguments, proof.Response, proof.Seal} {
		if b != nil && !json.Valid(b) {
			return 0, errors.New("invalid research acquisition JSON")
		}
	}
	if len(proof.Arguments) == 0 || len(proof.Response) == 0 {
		return 0, errors.New("incomplete research acquisition")
	}
	lock, err := root.Open(".")
	if err != nil {
		return 0, err
	}
	defer lock.Close()
	if err = waitPrivateDataLock(ctx, lock, true); err != nil {
		return 0, err
	}
	artifacts := s.artifactRepository()
	prior, err := readPrivateData(root, name, inputResearchSourceProofLimit)
	if err == nil {
		saved, e := resolveResearchProof(ctx, artifacts, prior)
		if e != nil {
			return 0, e
		}
		if !sameResearchAcquisition(saved.Proof, proof) || len(saved.Proof.Seal) > 0 && !bytes.Equal(saved.Proof.Seal, proof.Seal) {
			return 0, errResearchProofConflict
		}
		if bytes.Equal(saved.Proof.Seal, proof.Seal) {
			return saved.Bytes, nil
		}
		// The only transition is adding the seal to the unchanged acquisition.
	} else if !errors.Is(err, os.ErrNotExist) {
		return 0, err
	}
	for _, item := range []struct {
		ref *retainedArtifactRef
		raw []byte
	}{
		{&m.Response, proof.Response}, {&m.Arguments, proof.Arguments}, {m.Seal, proof.Seal},
	} {
		if item.ref == nil {
			continue
		}
		ref, e := artifacts.Put(ctx, item.raw)
		if e != nil {
			return 0, e
		}
		if ref != *item.ref {
			return 0, errRetainedArtifactChanged
		}
	}
	if err = ctx.Err(); err != nil {
		return 0, err
	}
	// Verify object readability before publishing a manifest. The manifest
	// primitive then verifies its own exact bytes after the durable replacement.
	if _, err = resolveResearchProof(ctx, artifacts, raw); err != nil {
		return 0, err
	}
	if err = ctx.Err(); err != nil {
		return 0, err
	}
	if err = writePrivateData(root, name, raw); err != nil {
		return 0, err
	}
	return size, nil
}

func (s *Server) copyResearchProof(ctx context.Context, destination *os.Root, id string, saved researchProofSnapshot) error {
	if _, err := resolveResearchProof(ctx, s.artifactRepository(), saved.Raw); err != nil {
		return err
	}
	name, err := researchProofName(id)
	if err != nil {
		return err
	}
	lock, err := destination.Open(".")
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = waitPrivateDataLock(ctx, lock, true); err != nil {
		return err
	}
	prior, err := readPrivateData(destination, name, inputResearchSourceProofLimit)
	if err == nil {
		if !bytes.Equal(prior, saved.Raw) {
			return errResearchProofConflict
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return writePrivateData(destination, name, saved.Raw)
}
