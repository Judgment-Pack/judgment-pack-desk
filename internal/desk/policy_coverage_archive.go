package desk

import (
	"bytes"
	"context"
	"encoding/json"
)

// Offline recovery checks private identity bindings and historical Runtime
// output. It never repeats Runtime validation or promotes archived authority.
func archivedCoverageCheck(ctx context.Context, artifacts retainedArtifactRepository, review policyCoverageReview, material policyCoverageMaterial) ([]retainedArtifactRef, error) {
	if review.Check == nil {
		return nil, nil
	}
	raw, err := artifacts.Get(ctx, *review.Check)
	if err != nil {
		return nil, policyArchiveInvalid()
	}
	var retained policyCoverageRetainedCheck
	if rejectDuplicateMembers(raw) != nil || decodeDataJSON(raw, &retained) != nil || retained.SchemaVersion != 1 || !coverageCheckAccepted(retained.Check) || retained.Check.CandidateDigest != review.CandidateDigest || retained.Check.PolicyRevision != review.PolicyRevision || !retainedArtifactDigest.MatchString(retained.Check.CoverageRevision) || !retained.Document.valid() || len(retained.Materials) < 3 || len(retained.Materials) > 16 {
		return nil, policyArchiveInvalid()
	}
	var report policyCoverageRuntimeReport
	if decodeDataJSON(retained.Check.Report, &report) != nil || !validCoverageRuntimeReport(report, "check") || report.ValidationScope != policyCoverageCheckScope || len(report.Diagnostics) != 0 {
		return nil, policyArchiveInvalid()
	}
	doc, err := artifacts.Get(ctx, retained.Document)
	if err != nil || report.Document != "" && !bytes.Equal([]byte(report.Document), doc) || report.BytesDigest != "sha256:"+digestOf(doc) || report.CanonicalDigest != report.BytesDigest {
		return nil, policyArchiveInvalid()
	}
	refs := []retainedArtifactRef{*review.Check, retained.Document}
	supplied := map[string][]byte{}
	for _, ref := range retained.Materials {
		data, err := artifacts.Get(ctx, ref)
		if err != nil || supplied[ref.SHA256] != nil {
			return nil, policyArchiveInvalid()
		}
		supplied[ref.SHA256] = data
		refs = append(refs, ref)
	}
	required := [][]byte{material.Pack, []byte(material.Content.Source.Narrative)}
	if material.Content.Statement.Status == "implemented" {
		required = append(required, []byte(material.Content.Statement.Basis))
	}
	if material.OriginalSource != nil {
		required = append(required, material.Original)
	}
	for _, data := range required {
		if !bytes.Equal(supplied[digestOf(data)], data) {
			return nil, policyArchiveInvalid()
		}
	}
	var parsed struct {
		Kind     string `json:"kind"`
		Consumer struct {
			ID      string              `json:"id"`
			Version string              `json:"version"`
			Content policySourceContent `json:"content"`
		} `json:"consumer"`
		GoverningPolicies []struct {
			Policy struct {
				ID      string              `json:"id"`
				Version string              `json:"version"`
				Content policySourceContent `json:"content"`
			} `json:"policy"`
			Scope struct {
				Kind string `json:"kind"`
			} `json:"scope"`
			Coverage []json.RawMessage `json:"coverage"`
		} `json:"governingPolicies"`
	}
	if rejectDuplicateMembers(doc) != nil || json.Unmarshal(doc, &parsed) != nil || parsed.Kind != "governing-adoption" || parsed.Consumer.ID != material.Content.Pack.Document.ID || parsed.Consumer.Version != material.Content.Pack.Document.Version || parsed.Consumer.Content.BytesDigest != "sha256:"+digestOf(material.Pack) || parsed.Consumer.Content.ByteLength != len(material.Pack) || parsed.Consumer.Content.MediaType != "application/json" || len(parsed.GoverningPolicies) != 1 {
		return nil, policyArchiveInvalid()
	}
	governing := parsed.GoverningPolicies[0]
	if governing.Policy.ID != "urn:desk:policy:"+material.Content.Source.PolicyID || governing.Policy.Version != material.Content.Source.ArtifactVersion || governing.Scope.Kind != "whole-policy" || len(governing.Coverage) != 1 {
		return nil, policyArchiveInvalid()
	}
	expectedRow, _ := json.Marshal(coverageRuntimeStatement(material.Content.Statement))
	if !sameDialectJSON(expectedRow, governing.Coverage[0]) {
		return nil, policyArchiveInvalid()
	}
	var policyRaw []byte
	for key, data := range supplied {
		if "sha256:"+key == governing.Policy.Content.BytesDigest {
			policyRaw = data
			break
		}
	}
	expectedPolicy, _ := json.Marshal(coverageRuntimePolicy(material))
	if len(policyRaw) != governing.Policy.Content.ByteLength || governing.Policy.Content.MediaType != "application/json" || !sameDialectJSON(expectedPolicy, policyRaw) {
		return nil, policyArchiveInvalid()
	}
	return refs, nil
}
func validateArchivedCoverageOperation(ctx context.Context, l policyLedger, artifacts retainedArtifactRepository, r policyMutationReply) error {
	if r.Coverage == nil {
		return nil
	}
	reply := r.Coverage
	record := l.Coverages[reply.Document.ID]
	doc := reply.Document
	if record.ID == "" || record.PolicyID != r.Receipt.PolicyID || len(doc.Candidates) == 0 || len(doc.Candidates) > len(record.Candidates) || len(doc.Reviews) > len(record.Reviews) || doc.CreatedAt != record.CreatedAt || doc.Candidate.ID != doc.Candidates[len(doc.Candidates)-1].ID {
		return policyArchiveInvalid()
	}
	for i, c := range doc.Candidates {
		if !samePolicyStatusValue(c, record.Candidates[i]) {
			return policyArchiveInvalid()
		}
	}
	for i, review := range doc.Reviews {
		if !samePolicyStatusValue(review, record.Reviews[i]) {
			return policyArchiveInvalid()
		}
	}
	metadata := doc.Candidates[len(doc.Candidates)-1]
	service := policyService{artifacts: artifacts}
	material, err := service.coverageMaterial(ctx, l.Policies[record.PolicyID], metadata)
	if err != nil {
		return policyArchiveInvalid()
	}
	expected := policyCoverageCandidate{ID: metadata.ID, Digest: metadata.Digest, Content: metadata.Content, Source: material.Content.Source, Pack: material.Content.Pack, Scope: material.Content.Scope, Statement: material.Content.Statement}
	if !samePolicyStatusValue(expected, doc.Candidate) {
		return policyArchiveInvalid()
	}
	inventory, err := coverageInventory(material.Pack)
	if err != nil || !samePolicyStatusValue(inventory, reply.Inventory) {
		return policyArchiveInvalid()
	}
	if !contains([]string{"present", "missing", "unavailable"}, reply.Availability.PackPublication) {
		return policyArchiveInvalid()
	}
	if recovery := reply.Recovery; recovery != nil {
		found := false
		for _, snapshot := range l.Recoveries {
			if snapshot.SnapshotID == recovery.SnapshotID && snapshot.At == recovery.RestoredAt && recovery.CurrentnessUnknown {
				found = true
			}
		}
		if !found {
			return policyArchiveInvalid()
		}
	}
	return nil
}
func coverageArchiveClosure(ctx context.Context, l policyLedger, artifacts retainedArtifactRepository, add func(retainedArtifactRef) error) (int, int, int, error) {
	candidates, reviews := 0, 0
	service := policyService{artifacts: artifacts}
	for _, record := range l.Coverages {
		candidates += len(record.Candidates)
		reviews += len(record.Reviews)
		materials := map[string]policyCoverageMaterial{}
		for _, c := range record.Candidates {
			material, err := service.coverageMaterial(ctx, l.Policies[record.PolicyID], c)
			if err != nil {
				return 0, 0, 0, policyArchiveInvalid()
			}
			materials[c.ID] = material
			for _, ref := range []retainedArtifactRef{c.Content, material.Content.Pack.Document.Content} {
				if err := add(ref); err != nil {
					return 0, 0, 0, err
				}
			}
		}
		for _, review := range record.Reviews {
			refs, err := archivedCoverageCheck(ctx, artifacts, review, materials[review.CandidateID])
			if err != nil {
				return 0, 0, 0, err
			}
			for _, ref := range refs {
				if err := add(ref); err != nil {
					return 0, 0, 0, err
				}
			}
		}
	}
	return len(l.Coverages), candidates, reviews, nil
}
