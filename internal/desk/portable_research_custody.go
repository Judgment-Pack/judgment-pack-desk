package desk

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// Only oversized complete acquisitions use a descriptor. Inline history keeps
// its original representation. The manifest and its objects belong to the
// existing research proof/artifact lifecycle, never a second sidecar store.
type portableResearchProofReference struct {
	Profile       string              `json:"profile"`
	ProfileDigest string              `json:"profileDigest"`
	Version       int                 `json:"version"`
	Manifest      retainedArtifactRef `json:"manifest"`
	Bytes         int                 `json:"bytes"`
}

var errPortableResearchStorageLimit = errors.New("The portable research evidence storage limit leaves no room for another acquisition. Retained acquisitions were kept; no further request was made.")

var errPortableResearchAcquisitionSize = errors.New("The complete acquisition exceeds portable research's inline result limit. Its exact response was retained for audit; research stopped without another request.")
var errPortableResearchProofUnavailable = errors.New("The retained portable acquisition proof is unavailable or does not match this operation. No request was replayed.")

func (ref *portableResearchProofReference) valid() bool {
	return ref != nil && ref.Profile != "" && len(ref.Profile) <= 256 && strings.HasPrefix(ref.ProfileDigest, "sha256:") && retainedArtifactDigest.MatchString(strings.TrimPrefix(ref.ProfileDigest, "sha256:")) && ref.Version == 1 && ref.Manifest.valid() && ref.Manifest.Bytes > 0 && ref.Manifest.Bytes <= 8<<10 && ref.Bytes >= ref.Manifest.Bytes && ref.Bytes <= inputResearchSourceProofLimit
}

func (store researchRunStore) loadPortableProof(ctx context.Context, run inputResearchRun, attempt inputResearchAttempt) (researchProofSnapshot, error) {
	bad := func() (researchProofSnapshot, error) {
		return researchProofSnapshot{}, errPortableResearchProofUnavailable
	}
	ref := attempt.RetainedProof
	if !ref.valid() || store.loadProof == nil || run.Portable == nil || attempt.ID != "attempt_"+attempt.Digest {
		return bad()
	}
	saved, err := store.loadProof(ctx, attempt.ID)
	if err != nil || verifyRetainedArtifact(ref.Manifest, saved.Raw) != nil || saved.Bytes != ref.Bytes || saved.Proof.Session != run.ID+"-"+attempt.ID || saved.Proof.Pin != run.Gateway || len(saved.Proof.Response) > inputResearchSourceResponseLimit || len(saved.Proof.Arguments) > backgroundModelResponseBytes || len(saved.Proof.Seal) != 0 {
		return bad()
	}
	capability := attempt.Kind
	if capability == "model" {
		capability = "answer"
	}
	assignment, err := portableResearchAssignments(run.Portable.Frozen.Input, run.Portable.Frozen.Plan.Name)
	if err != nil || assignment[capability].Profile != ref.Profile || assignment[capability].Digest != ref.ProfileDigest {
		return bad()
	}
	metadata, declaredSize, err := storedProofMetadata(saved.Raw)
	if err != nil || metadata == nil || metadata.Seal != nil || declaredSize != saved.Bytes || metadata.Session != saved.Proof.Session || metadata.Source != saved.Proof.Source || metadata.Pin != saved.Proof.Pin || verifyRetainedArtifact(metadata.Arguments, saved.Proof.Arguments) != nil || verifyRetainedArtifact(metadata.Response, saved.Proof.Response) != nil {
		return bad()
	}
	profile, err := portableResearchProfileFor(run, capability)
	if err != nil || saved.Proof.Source != profile.Source {
		return bad()
	}
	return saved, nil
}

func (store researchRunStore) validatePortableProofs(ctx context.Context, run inputResearchRun) error {
	for _, a := range run.Attempts {
		if a.RetainedProof != nil {
			if _, err := store.loadPortableProof(ctx, run, a); err != nil {
				return err
			}
		}
	}
	return nil
}

// Preserve the Gateway envelope before any compaction/re-encoding. Oversized
// accepted pages and receipt-refused pages share custody, but keep their own
// refusal reason. The terminal size decision closes the existing worker loop.
func retainPortableAcquisitionWithCustody(ctx context.Context, store researchRunStore, run inputResearchRun, attempt inputResearchAttempt, capability string, profile portableProfileDetails, request, raw json.RawMessage) (json.RawMessage, error) {
	retained, admissionErr := retainAdmittedPortableAcquisition(run, capability, profile, request, raw)
	if len(raw) <= 2<<20 && len(retained) <= 2<<20 {
		return retained, admissionErr
	}
	var refusal *researchAcquisitionRefused
	if admissionErr != nil && !errors.As(admissionErr, &refusal) {
		return nil, admissionErr
	}
	if len(raw) > inputResearchSourceResponseLimit || len(request) > backgroundModelResponseBytes || store.saveProof == nil || store.loadProof == nil {
		return nil, errPortableResearchProofUnavailable
	}
	proof := inputResearchProof{Session: run.ID + "-" + attempt.ID, Source: profile.Source, Pin: run.Gateway, Arguments: request, Response: raw}
	priorBytes := run.RetrievedBytes + researchReservedBytes(run) - attempt.ReservedBytes + portableResearchProofReservedBytes(run) - inputResearchSourceProofLimit
	size, err := store.saveProof(ctx, attempt.ID, proof, priorBytes)
	if err != nil {
		return nil, errPortableResearchProofUnavailable
	}
	saved, err := store.loadProof(ctx, attempt.ID)
	if err != nil || !sameResearchAcquisition(saved.Proof, proof) || size != saved.Bytes {
		return nil, errPortableResearchProofUnavailable
	}
	assignments, err := portableResearchAssignments(run.Portable.Frozen.Input, run.Portable.Frozen.Plan.Name)
	if err != nil {
		return nil, errPortableResearchProofUnavailable
	}
	assignment := assignments[capability]
	ref := &portableResearchProofReference{Profile: assignment.Profile, ProfileDigest: assignment.Digest, Version: 1, Manifest: artifactReference(saved.Raw), Bytes: size}
	held := attempt
	held.RetainedProof = ref
	if _, err = store.loadPortableProof(ctx, run, held); err != nil {
		return nil, err
	}
	if refusal == nil {
		refusal = &researchAcquisitionRefused{code: "source-response-too-large", cause: errPortableResearchAcquisitionSize}
	}
	refusal.proof = ref
	return nil, refusal
}

func portableResearchProofReservedBytes(run inputResearchRun) int {
	reserved := 0
	if run.Portable != nil {
		for _, a := range run.Attempts {
			if a.ReservedBytes > 0 {
				reserved += inputResearchSourceProofLimit
			}
		}
	}
	return reserved
}
