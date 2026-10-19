package desk

// Policy recovery is a closed private-data archive, independent of the personal
// chat ZIP. The ledger fixes the cut; immutable objects precede its publication.
import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
	"time"
)

const policyArchiveDataLimit = 512 << 20
const policyArchiveLimit = policyArchiveDataLimit + 2<<20
const policyArchiveEntryLimit = 8192
const policyArchiveFormat = "jpack-desk-policy-recovery"

type policyArchiveCounts struct {
	Adoptions          int   `json:"adoptions"`
	AdoptionReviews    int   `json:"adoptionReviews"`
	Coverages          int   `json:"coverages"`
	CoverageCandidates int   `json:"coverageCandidates"`
	CoverageReviews    int   `json:"coverageReviews"`
	Policies           int   `json:"policies"`
	Revisions          int   `json:"revisions"`
	Reviews            int   `json:"reviews"`
	Publications       int   `json:"publications"`
	Operations         int   `json:"operations"`
	SourceObservations int   `json:"sourceObservations"`
	ExternalVersions   int   `json:"externalVersions"`
	Blobs              int   `json:"blobs"`
	BlobBytes          int64 `json:"blobBytes"`
}
type policyArchiveManifest struct {
	Format              string                `json:"format"`
	Version             int                   `json:"version"`
	PolicySchemaVersion int                   `json:"policySchemaVersion"`
	CapturedAt          string                `json:"capturedAt"`
	Scope               string                `json:"scope"`
	Ledger              retainedArtifactRef   `json:"ledger"`
	Blobs               []retainedArtifactRef `json:"blobs"`
}
type policyArchiveCut struct {
	Ledger policyLedger
	Raw    []byte
	Refs   []retainedArtifactRef
	Counts policyArchiveCounts
}

func policyArchiveInvalid() error {
	return policyRefusal(400, "archive-integrity", "The Policy archive is incomplete or inconsistent. No project was restored.")
}
func policyArchiveCapacity() error {
	return policyRefusal(413, "archive-capacity", "The complete Policy archive exceeds the supported recovery limits.")
}

func policyArchiveClosure(ctx context.Context, ledger policyLedger, artifacts retainedArtifactRepository) (policyArchiveCut, error) {
	cut := policyArchiveCut{Ledger: ledger, Refs: []retainedArtifactRef{}}
	if ledger.SchemaVersion == policyExecutionLedgerSchemaVersion || ledger.Execution != nil {
		return cut, policyExecutionArchiveUnsupported()
	}
	if validatePolicyLedger(ledger) != nil {
		return cut, policyArchiveInvalid()
	}
	cut.Raw, _ = json.Marshal(ledger)
	if len(cut.Raw) > policyLedgerLimit {
		return cut, policyArchiveCapacity()
	}
	refs := map[string]retainedArtifactRef{}
	totalBytes := int64(len(cut.Raw))
	add := func(ref retainedArtifactRef) error {
		if !ref.valid() {
			return policyArchiveInvalid()
		}
		if prior, ok := refs[ref.SHA256]; ok {
			if prior != ref {
				return policyArchiveInvalid()
			}
			return nil
		}
		totalBytes += int64(ref.Bytes)
		if totalBytes > policyArchiveDataLimit || len(refs)+3 > policyArchiveEntryLimit {
			return policyArchiveCapacity()
		}
		refs[ref.SHA256] = ref
		return nil
	}
	addObservation := func(o policySourceObservation) error {
		if err := add(o.Original); err != nil {
			return err
		}
		return add(o.Document)
	}
	service := policyService{artifacts: artifacts}
	for _, record := range ledger.Policies {
		cut.Counts.Policies++
		cut.Counts.Revisions += len(record.Revisions)
		cut.Counts.Reviews += len(record.Reviews)
		cut.Counts.Publications += len(record.Publications)
		for _, revision := range record.Revisions {
			if err := add(revision.Content); err != nil {
				return cut, err
			}
			if revision.Source != nil {
				if err := add(revision.Source.Content); err != nil {
					return cut, err
				}
			}
			if _, err := service.candidate(ctx, revision); err != nil {
				return cut, policyArchiveInvalid()
			}
		}
	}
	var coverageErr error
	cut.Counts.Coverages, cut.Counts.CoverageCandidates, cut.Counts.CoverageReviews, coverageErr = coverageArchiveClosure(ctx, ledger, artifacts, add)
	if coverageErr != nil {
		return cut, coverageErr
	}
	var adoptionErr error
	cut.Counts.Adoptions, cut.Counts.AdoptionReviews, adoptionErr = adoptionArchiveClosure(ctx, ledger, artifacts, add)
	if adoptionErr != nil {
		return cut, adoptionErr
	}
	for _, source := range ledger.Sources {
		cut.Counts.SourceObservations += len(source.Observations)
		for _, observation := range source.Observations {
			if err := addObservation(observation); err != nil {
				return cut, err
			}
			if err := validateArchivedPolicyObservation(ctx, artifacts, observation); err != nil {
				return cut, err
			}
		}
	}
	for _, stream := range ledger.SourceAuthorities {
		for _, event := range stream.Events {
			if event.ExternalVersion != nil {
				cut.Counts.ExternalVersions++
				if err := addObservation(event.ExternalVersion.Observation); err != nil {
					return cut, err
				}
				if event.ExternalVersion.Evidence != nil {
					if err := add(*event.ExternalVersion.Evidence); err != nil {
						return cut, err
					}
				}
			}
			if event.ImportedDraft != nil {
				if err := addObservation(event.ImportedDraft.Observation); err != nil {
					return cut, err
				}
			}
		}
	}
	for _, operation := range ledger.Operations {
		cut.Counts.Operations++
		if err := add(operation.Result); err != nil {
			return cut, err
		}
		if err := validateArchivedPolicyOperation(ctx, ledger, artifacts, operation); err != nil {
			return cut, err
		}
	}
	for _, ref := range refs {
		cut.Refs = append(cut.Refs, ref)
		cut.Counts.BlobBytes += int64(ref.Bytes)
	}
	cut.Counts.Blobs = len(cut.Refs)
	if len(cut.Refs)+2 > policyArchiveEntryLimit || cut.Counts.BlobBytes+int64(len(cut.Raw)) > policyArchiveDataLimit {
		return cut, policyArchiveCapacity()
	}
	sort.Slice(cut.Refs, func(i, j int) bool { return cut.Refs[i].SHA256 < cut.Refs[j].SHA256 })
	for _, ref := range cut.Refs {
		if _, err := artifacts.Get(ctx, ref); err != nil {
			return cut, policyArchiveInvalid()
		}
	}
	return cut, ctx.Err()
}

func validateArchivedPolicyOperation(ctx context.Context, ledger policyLedger, artifacts retainedArtifactRepository, operation policyStoredOperation) error {
	raw, err := artifacts.Get(ctx, operation.Result)
	if err != nil {
		return policyArchiveInvalid()
	}
	var r policyMutationReply
	if rejectDuplicateMembers(raw) != nil || decodeDataJSON(raw, &r) != nil || r.Delivery != "committed" || r.Receipt.OperationID != operation.OperationID || r.Receipt.Kind != operation.Kind || r.Receipt.PolicyID != operation.PolicyID || r.Receipt.RequestDigest != operation.RequestDigest || !validPolicyActor(r.Receipt.Actor) || !validPolicyTime(r.Receipt.At) || !retainedArtifactDigest.MatchString(r.Receipt.Revision) || !validPolicyMutationIdentity(r) || !validPolicyLifecycleResult(r) || !validPolicySourceAuthorityResult(r) || !validPolicyCoverageResult(r) || !validPolicyAdoptionResult(r) {
		return policyArchiveInvalid()
	}
	if err := validateArchivedCoverageOperation(ctx, ledger, artifacts, r); err != nil {
		return err
	}
	if err := validateArchivedAdoptionOperation(ctx, ledger, artifacts, r); err != nil {
		return err
	}
	record := ledger.Policies[operation.PolicyID]
	validRecovery := func(view *policyRecoveryView) bool {
		if view == nil {
			return true
		}
		for _, snapshot := range ledger.Recoveries {
			if snapshot.SnapshotID == view.SnapshotID {
				return view.RestoredAt == snapshot.At && view.CurrentnessUnknown
			}
		}
		return false
	}
	if !validRecovery(r.Policy.Recovery) || r.Lifecycle != nil && !validRecovery(r.Lifecycle.Recovery) || r.SourceStatus != nil && !validRecovery(r.SourceStatus.Recovery) || r.SourceSync != nil && !validRecovery(r.SourceSync.Recovery) {
		return policyArchiveInvalid()
	}
	knownRevision := func(revision policyRevision) bool {
		for _, stored := range record.Revisions {
			if samePolicyStatusValue(stored, revision) {
				return true
			}
		}
		return false
	}
	if r.OutcomeKind == "" {
		doc := r.Policy.Document
		if doc.SchemaVersion != 1 || len(doc.Revisions) == 0 || len(doc.Reviews) > len(record.Reviews) || len(doc.Publications) > len(record.Publications) {
			return policyArchiveInvalid()
		}
		var head policyRevision
		for _, revision := range doc.Revisions {
			if !knownRevision(revision) {
				return policyArchiveInvalid()
			}
			if revision.ID == doc.Candidate.RevisionID {
				head = revision
			}
		}
		candidate, err := (policyService{artifacts: artifacts}).candidate(ctx, head)
		if err != nil || !samePolicyStatusValue(candidate, doc.Candidate) || candidate.Digest != r.Receipt.CandidateDigest {
			return policyArchiveInvalid()
		}
		for i, review := range doc.Reviews {
			if !samePolicyStatusValue(review, record.Reviews[i]) {
				return policyArchiveInvalid()
			}
		}
		for i, publication := range doc.Publications {
			if !samePolicyStatusValue(publication, record.Publications[i]) {
				return policyArchiveInvalid()
			}
		}
	}
	if r.Lifecycle != nil {
		stream := ledger.Lifecycles[operation.PolicyID]
		if stream == nil || len(r.Lifecycle.Events) > len(stream.Events) {
			return policyArchiveInvalid()
		}
		for i, e := range r.Lifecycle.Events {
			if !samePolicyStatusValue(e, stream.Events[i]) {
				return policyArchiveInvalid()
			}
		}
	}
	if r.SourceStatus != nil {
		stream := ledger.SourceAuthorities[operation.PolicyID]
		if stream == nil || len(r.SourceStatus.Events) > len(stream.Events) {
			return policyArchiveInvalid()
		}
		for i, e := range r.SourceStatus.Events {
			if !samePolicyStatusValue(e, stream.Events[i]) {
				return policyArchiveInvalid()
			}
		}
	}
	if r.SourceSync != nil {
		one := emptyPolicyLedger()
		one.Policies[record.ID] = record
		one.Sources = map[string]*policyConnectedSource{record.ID: r.SourceSync.State}
		one.Recoveries = ledger.Recoveries
		if r.SourceSync.State == nil || validatePolicySources(one) != nil || (r.SourceSync.Recovery == nil && r.SourceSync.Revision != policySourceRevision(r.SourceSync.State)) {
			return policyArchiveInvalid()
		}
		if boundary := r.SourceSync.State.Recovery; boundary != nil {
			if r.SourceSync.Recovery == nil || r.SourceSync.Recovery.SnapshotID != boundary.SnapshotID || r.SourceSync.Recovery.SourceConsentRequired != (boundary.ConsentOperationID == "") || boundary.ConsentOperationID != "" && boundary.ConsentOperationID != r.SourceSync.State.Grant.OperationID {
				return policyArchiveInvalid()
			}
		}
	}
	return nil
}

func validateArchivedPolicyObservation(ctx context.Context, artifacts retainedArtifactRepository, o policySourceObservation) error {
	document, err := artifacts.Get(ctx, o.Document)
	if err != nil {
		return policyArchiveInvalid()
	}
	original, err := artifacts.Get(ctx, o.Original)
	if err != nil {
		return policyArchiveInvalid()
	}
	wire := policyGatewayObservation{Version: 1, BindingID: o.BindingID, ObservationID: o.ID, Source: o.Source, SourceRevision: o.SourceRevision, ObservedAt: o.ObservedAt, Availability: o.Availability, Coverage: o.Coverage, Lifecycle: o.Lifecycle, Content: o.Content, Extraction: o.Extraction, Document: document}
	raw, _ := json.Marshal(wire)
	// The existing strict v1 verifier is reused with a pure frozen-profile
	// canonicalizer. This cannot dispatch Runtime/Gateway/provider work.
	verifier := &Server{policySourceCanon: gatewayCanonicalJSON}
	prepared, err := verifier.verifyPolicyObservation(ctx, raw, policySourceBinding{BindingID: o.BindingID, Source: o.Source})
	if err != nil || !bytes.Equal(document, prepared.Document) || !bytes.Equal(original, prepared.Original) || prepared.Metadata.SemanticDigest != o.SemanticDigest || prepared.Metadata.Name != o.Name {
		return policyArchiveInvalid()
	}
	return nil
}

func writePolicyArchive(ctx context.Context, out *os.File, cut policyArchiveCut, artifacts retainedArtifactRepository) error {
	decoded, err := decodePolicyLedger(cut.Raw)
	if err != nil {
		return policyArchiveInvalid()
	}
	if decoded.SchemaVersion == policyExecutionLedgerSchemaVersion || cut.Ledger.Execution != nil {
		return policyExecutionArchiveUnsupported()
	}
	manifest := policyArchiveManifest{Format: policyArchiveFormat, Version: 1, PolicySchemaVersion: 2, CapturedAt: time.Now().UTC().Format("2006-01-02T15:04:05Z"), Scope: "project-policies", Ledger: artifactReference(cut.Raw), Blobs: cut.Refs}
	raw, _ := json.Marshal(manifest)
	if len(raw) > maxBackupManifest {
		return policyArchiveCapacity()
	}
	z := zip.NewWriter(out)
	write := func(name string, data []byte) error {
		w, err := z.Create(name)
		if err == nil {
			_, err = w.Write(data)
		}
		return err
	}
	if err := write("manifest.json", raw); err != nil {
		return err
	}
	if err := write("policy-ledger.json", cut.Raw); err != nil {
		return err
	}
	for _, ref := range cut.Refs {
		data, err := artifacts.Get(ctx, ref)
		if err != nil {
			return err
		}
		if err = write("blobs/"+ref.SHA256, data); err != nil {
			return err
		}
	}
	if err := z.Close(); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	stat, err := out.Stat()
	if err != nil {
		return err
	}
	if stat.Size() > policyArchiveLimit {
		return policyArchiveCapacity()
	}
	return nil
}

type policyArchiveReader struct{ files map[string]*zip.File }

func (a policyArchiveReader) Put(context.Context, []byte) (retainedArtifactRef, error) {
	return retainedArtifactRef{}, errors.New("archive is read-only")
}
func (a policyArchiveReader) Get(ctx context.Context, ref retainedArtifactRef) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file := a.files["blobs/"+ref.SHA256]
	if file == nil || !ref.valid() {
		return nil, policyArchiveInvalid()
	}
	raw, err := readBackupFile(file, ref.Bytes)
	if err != nil || verifyRetainedArtifact(ref, raw) != nil {
		return nil, policyArchiveInvalid()
	}
	return raw, nil
}
func readPolicyArchive(ctx context.Context, file *os.File) (policyArchiveCut, policyArchiveManifest, policyArchiveReader, error) {
	var cut policyArchiveCut
	var manifest policyArchiveManifest
	reader := policyArchiveReader{files: map[string]*zip.File{}}
	fail := func(err error) (policyArchiveCut, policyArchiveManifest, policyArchiveReader, error) {
		return cut, manifest, reader, err
	}
	stat, err := file.Stat()
	if err != nil {
		return fail(err)
	}
	if stat.Size() > policyArchiveLimit {
		return fail(policyArchiveCapacity())
	}
	if err = checkArchiveDirectory(file, stat.Size(), policyArchiveEntryLimit); err != nil {
		return fail(policyArchiveInvalid())
	}
	z, err := zip.NewReader(file, stat.Size())
	if err != nil {
		return fail(policyArchiveInvalid())
	}
	for _, entry := range z.File {
		name := entry.Name
		if reader.files[name] != nil || entry.Mode()&os.ModeType != 0 || (name != "manifest.json" && name != "policy-ledger.json" && (!strings.HasPrefix(name, "blobs/") || !retainedArtifactDigest.MatchString(strings.TrimPrefix(name, "blobs/")))) {
			return fail(policyArchiveInvalid())
		}
		reader.files[name] = entry
	}
	if reader.files["manifest.json"] == nil || reader.files["policy-ledger.json"] == nil {
		return fail(policyArchiveInvalid())
	}
	raw, err := readBackupFile(reader.files["manifest.json"], maxBackupManifest)
	if err != nil || rejectDuplicateMembers(raw) != nil || decodeDataJSON(raw, &manifest) != nil {
		return fail(policyArchiveInvalid())
	}
	if manifest.Format != policyArchiveFormat || manifest.Version != 1 || manifest.PolicySchemaVersion != 2 || manifest.Scope != "project-policies" {
		return fail(policyRefusal(400, "archive-unsupported", "This Policy archive format or private schema is not supported."))
	}
	if !validPolicyTime(manifest.CapturedAt) || manifest.Blobs == nil || len(manifest.Blobs)+2 != len(reader.files) || manifest.Ledger.Bytes > policyLedgerLimit || !manifest.Ledger.valid() {
		return fail(policyArchiveInvalid())
	}
	seen := map[string]bool{}
	declared := map[string]retainedArtifactRef{}
	total := int64(manifest.Ledger.Bytes)
	for _, ref := range manifest.Blobs {
		if !ref.valid() || seen[ref.SHA256] || reader.files["blobs/"+ref.SHA256] == nil {
			return fail(policyArchiveInvalid())
		}
		seen[ref.SHA256] = true
		declared[ref.SHA256] = ref
		total += int64(ref.Bytes)
	}
	if total > policyArchiveDataLimit {
		return fail(policyArchiveCapacity())
	}
	raw, err = readBackupFile(reader.files["policy-ledger.json"], policyLedgerLimit)
	if err != nil || verifyRetainedArtifact(manifest.Ledger, raw) != nil {
		return fail(policyArchiveInvalid())
	}
	cut.Ledger, err = decodePolicyLedger(raw)
	if err != nil {
		return fail(policyArchiveInvalid())
	}
	cut, err = policyArchiveClosure(ctx, cut.Ledger, reader)
	if err != nil {
		return fail(err)
	}
	if len(cut.Refs) != len(manifest.Blobs) {
		return fail(policyArchiveInvalid())
	}
	for _, ref := range cut.Refs {
		if declared[ref.SHA256] != ref {
			return fail(policyArchiveInvalid())
		}
	}
	// Keep the exact uploaded ledger bytes; no authority is inferred from them.
	cut.Raw = raw
	return cut, manifest, reader, nil
}
