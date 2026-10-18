package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
)

// Validate historical candidate bindings offline. Imported historical review
// assertions are never made current: restore installs an inactive boundary.
func archivedAdoptionReport(ctx context.Context, a retainedArtifactRepository, ref retainedArtifactRef, c policyAdoptionContent, doc []byte) error {
	raw, err := a.Get(ctx, ref)
	if err != nil {
		return policyArchiveInvalid()
	}
	var report policyCoverageRuntimeReport
	if coverageDecode(raw, &report) != nil || !validPolicyRuntimeReport(report, "check", adoptionRuntimeContract) || report.Status != "references-valid" || report.ValidationScope != policyAdoptionCheckScope || len(report.Diagnostics) != 0 || report.DiagnosticsTruncated || report.Kind != "governing-adoption" || report.ArtifactID != c.PortableID || report.ArtifactVersion != "1.0.0" || report.BytesDigest != "sha256:"+digestOf(doc) || report.CanonicalDigest != report.BytesDigest || report.Document != "" && report.Document != string(doc) {
		return policyArchiveInvalid()
	}
	return nil
}
func archivedAdoptionContent(ctx context.Context, l policyLedger, a retainedArtifactRepository, r policyAdoptionRecord, c policyAdoptionContent, add func(retainedArtifactRef) error) error {
	doc, err := a.Get(ctx, c.Document)
	if err != nil {
		return policyArchiveInvalid()
	}
	if err = archivedAdoptionReport(ctx, a, c.Check, c, doc); err != nil {
		return err
	}
	var adoption struct {
		GoverningPolicies []struct {
			Policy struct {
				Kind    string              `json:"kind"`
				ID      string              `json:"id"`
				Version string              `json:"version"`
				Content policySourceContent `json:"content"`
			} `json:"policy"`
		} `json:"governingPolicies"`
	}
	if rejectDuplicateMembers(doc) != nil || json.Unmarshal(doc, &adoption) != nil || len(adoption.GoverningPolicies) != len(c.Basis.Dependencies) {
		return policyArchiveInvalid()
	}
	refs := []retainedArtifactRef{r.Content, c.Document, c.Check, c.Basis.Pack.Content}
	supplied := map[string][]byte{}
	for _, ref := range c.Materials {
		data, e := a.Get(ctx, ref)
		if _, exists := supplied[ref.SHA256]; e != nil || exists || !ref.valid() {
			return policyArchiveInvalid()
		}
		supplied[ref.SHA256] = data
		refs = append(refs, ref)
	}
	expected := map[string]bool{}
	require := func(raw []byte) bool {
		retained, ok := supplied[digestOf(raw)]
		if !ok || !bytes.Equal(retained, raw) {
			return false
		}
		expected[digestOf(raw)] = true
		return true
	}
	governing := []any{}
	service := policyService{artifacts: a}
	for i, d := range c.Basis.Dependencies {
		coverage := l.Coverages[d.Selection.CoverageID]
		if coverage.PolicyID != d.Selection.PolicyID {
			return policyArchiveInvalid()
		}
		var metadata policyCoverageCandidateMetadata
		for _, v := range coverage.Candidates {
			if v.ID == d.Selection.CandidateID {
				metadata = v
			}
		}
		if metadata.ID == "" || metadata.Digest != d.CandidateDigest || metadata.Content != d.CoverageContent {
			return policyArchiveInvalid()
		}
		found := false
		for _, v := range coverage.Reviews {
			if v.ID == d.Selection.ReviewID && samePolicyStatusValue(v, d.CoverageReview) {
				found = true
			}
		}
		if !found {
			return policyArchiveInvalid()
		}
		// Verify the selected coverage revision was an actual historical prefix. A
		// later archive cannot invent a revision that never included this approval.
		prefix := coverage
		prefix.Recovery = d.CoverageRecovery
		prefix.Candidates = []policyCoverageCandidateMetadata{}
		prefix.Reviews = []policyCoverageReview{}
		for _, v := range coverage.Candidates {
			if v.Sequence <= d.CoverageReview.Sequence {
				prefix.Candidates = append(prefix.Candidates, v)
			}
		}
		for _, v := range coverage.Reviews {
			if v.Sequence <= d.CoverageReview.Sequence {
				prefix.Reviews = append(prefix.Reviews, v)
			}
		}
		prefix.HeadCandidateID = metadata.ID
		prefix.CurrentReviewID = d.Selection.ReviewID
		prefix.UpdatedAt = d.CoverageReview.At
		if recovery := d.CoverageRecovery; recovery != nil {
			known := false
			for _, snapshot := range l.Recoveries {
				if snapshot.SnapshotID == recovery.SnapshotID && snapshot.At == recovery.RestoredAt {
					known = true
				}
			}
			if !known || recovery.Sequence >= d.CoverageReview.Sequence {
				return policyArchiveInvalid()
			}
		}
		if coverageRevision(prefix) != d.Selection.Revision {
			return policyArchiveInvalid()
		}

		material, e := service.coverageMaterial(ctx, l.Policies[d.Selection.PolicyID], metadata)
		if e != nil || !samePolicyStatusValue(material.Content.Source, d.Source) || !samePolicyStatusValue(material.Content.Scope, d.Scope) || !samePolicyStatusValue(material.Content.Statement, d.Statement) || coveragePackSelector(material.Content.Pack) != c.Basis.Pack || !samePolicyStatusValue(material.Content.Publication, c.Basis.Publication) {
			return policyArchiveInvalid()
		}
		if _, e = archivedCoverageCheck(ctx, a, d.CoverageReview, material); e != nil {
			return e
		}
		if !require(material.Pack) || !require([]byte(d.Source.Narrative)) || d.Statement.Status == "implemented" && !require([]byte(d.Statement.Basis)) || material.OriginalSource != nil && !require(material.Original) {
			return policyArchiveInvalid()
		}
		policy := adoptionRuntimePolicy(material)
		policyRaw, _ := json.Marshal(policy)
		// The adoption document selects the exact application/json Policy role.
		// Other required roles may contain different bytes with the same JSON meaning.
		reference := adoption.GoverningPolicies[i].Policy
		canonical, present := supplied[strings.TrimPrefix(reference.Content.BytesDigest, "sha256:")]
		if !present || reference.Kind != "policy" || reference.ID != policy["id"] || reference.Version != d.Source.ArtifactVersion || reference.Content.MediaType != "application/json" || reference.Content.ByteLength != len(canonical) || reference.Content.BytesDigest != "sha256:"+digestOf(canonical) || !sameDialectJSON(policyRaw, canonical) || !require(canonical) {
			return policyArchiveInvalid()
		}

		governing = append(governing, map[string]any{"policy": map[string]any{"kind": "policy", "id": policy["id"], "version": d.Source.ArtifactVersion, "content": coverageRuntimeContent(canonical, "application/json")}, "scope": map[string]any{"kind": "whole-policy"}, "coverage": []any{coverageRuntimeStatement(d.Statement)}})
		refs = append(refs, d.CoverageContent, *d.CoverageReview.Check)
	}
	if len(expected) != len(supplied) {
		return policyArchiveInvalid()
	}
	pack, err := a.Get(ctx, c.Basis.Pack.Content)
	if err != nil || !require(pack) {
		return policyArchiveInvalid()
	}
	expectedDoc, _ := json.Marshal(map[string]any{"formatVersion": policyAdoptionFormat, "kind": "governing-adoption", "id": c.PortableID, "version": "1.0.0", "consumer": map[string]any{"kind": "pack", "id": c.Basis.Pack.ArtifactID, "version": c.Basis.Pack.Version, "content": coverageRuntimeContent(pack, "application/json")}, "governingPolicies": governing})
	if !sameDialectJSON(expectedDoc, doc) {
		return policyArchiveInvalid()
	}
	for _, review := range r.Reviews {
		if review.BasisDigest != c.BasisDigest {
			return policyArchiveInvalid()
		}
		if review.Check != nil {
			if err := archivedAdoptionReport(ctx, a, *review.Check, c, doc); err != nil {
				return err
			}
			refs = append(refs, *review.Check)
		}
	}
	for _, ref := range refs {
		if err := add(ref); err != nil {
			return err
		}
	}
	return nil
}
func adoptionArchiveClosure(ctx context.Context, l policyLedger, a retainedArtifactRepository, add func(retainedArtifactRef) error) (int, int, error) {
	reviews := 0
	service := policyService{artifacts: a}
	for _, r := range l.Adoptions {
		c, err := service.adoptionContent(ctx, r)
		if err != nil {
			return 0, 0, policyArchiveInvalid()
		}
		if err = archivedAdoptionContent(ctx, l, a, r, c, add); err != nil {
			return 0, 0, err
		}
		reviews += len(r.Reviews)
	}
	return len(l.Adoptions), reviews, nil
}
func validateArchivedAdoptionOperation(ctx context.Context, l policyLedger, a retainedArtifactRepository, result policyMutationReply) error {
	if result.Adoption == nil {
		return nil
	}
	reply := result.Adoption
	r := l.Adoptions[reply.Document.ID]
	if r.ID == "" || len(reply.Document.Reviews) > len(r.Reviews) || reply.Document.CreatedAt != r.CreatedAt {
		return policyArchiveInvalid()
	}
	c, err := (policyService{artifacts: a}).adoptionContent(ctx, r)
	if err != nil || !samePolicyStatusValue(adoptionCandidate(r, c), reply.Document.Candidate) || c.Basis.Dependencies[0].Selection.PolicyID != result.Receipt.PolicyID {
		return policyArchiveInvalid()
	}
	for i, v := range reply.Document.Reviews {
		if !samePolicyStatusValue(v, r.Reviews[i]) {
			return policyArchiveInvalid()
		}
	}
	if reply.Recovery != nil {
		found := false
		for _, snapshot := range l.Recoveries {
			if snapshot.SnapshotID == reply.Recovery.SnapshotID && snapshot.At == reply.Recovery.RestoredAt && reply.Recovery.CurrentnessUnknown && reply.Recovery.FreshReviewRequired {
				found = true
			}
		}
		if !found {
			return policyArchiveInvalid()
		}
	}
	// Reconstruct the exact historical metadata prefix. Current lifecycle state
	// may differ, but the retained revision/review assertions must be provable.
	prefix := r
	prefix.Reviews = reply.Document.Reviews
	prefix.Recovery = reply.RecoveryBoundary
	prefix.UpdatedAt = prefix.CreatedAt
	prefix.CurrentReviewID = ""
	if len(prefix.Reviews) > 0 {
		last := prefix.Reviews[len(prefix.Reviews)-1]
		prefix.CurrentReviewID = last.ID
		prefix.UpdatedAt = last.At
	}
	if (reply.Recovery == nil) != (prefix.Recovery == nil) {
		return policyArchiveInvalid()
	}
	if prefix.Recovery != nil {
		if prefix.Recovery.SnapshotID != reply.Recovery.SnapshotID || prefix.Recovery.RestoredAt != reply.Recovery.RestoredAt || prefix.Recovery.Sequence < 0 || prefix.Recovery.Sequence > len(prefix.Reviews) {
			return policyArchiveInvalid()
		}
	}
	state, review := adoptionReviewState(prefix)
	if adoptionRevision(prefix) != reply.Revision || state != reply.Document.ReviewState || review != reply.Document.CurrentReviewID || prefix.UpdatedAt != reply.Document.UpdatedAt {
		return policyArchiveInvalid()
	}
	if !contains([]string{"ready", "stale", "unavailable"}, reply.Readiness.Status) || !contains([]string{"effective", "not-yet-effective", "ineligible", "unavailable"}, reply.Eligibility.Status) {
		return policyArchiveInvalid()
	}
	return nil
}
