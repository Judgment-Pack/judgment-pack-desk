package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func adoptionAcceptedFixture(_ context.Context, m policyAdoptionMaterial) policyAdoptionCheck {
	policies := []any{}
	materials := [][]byte{}
	seen := map[string]bool{}
	add := func(raw []byte) {
		if !seen[digestOf(raw)] {
			seen[digestOf(raw)] = true
			materials = append(materials, raw)
		}
	}
	for _, v := range m.Materials {
		p := adoptionRuntimePolicy(v)
		raw, _ := json.Marshal(p)
		add(raw)
		add(v.Pack)
		add([]byte(v.Content.Source.Narrative))
		if v.Content.Statement.Status == "implemented" {
			add([]byte(v.Content.Statement.Basis))
		}
		if v.OriginalSource != nil {
			add(v.Original)
		}
		policies = append(policies, map[string]any{"policy": map[string]any{"kind": "policy", "id": p["id"], "version": v.Content.Source.ArtifactVersion, "content": coverageRuntimeContent(raw, "application/json")}, "scope": map[string]any{"kind": "whole-policy"}, "coverage": []any{coverageRuntimeStatement(v.Content.Statement)}})
	}
	pack := m.Materials[0].Content.Pack.Document
	doc, _ := json.Marshal(map[string]any{"formatVersion": policyAdoptionFormat, "kind": "governing-adoption", "id": m.PortableID, "version": "1.0.0", "consumer": map[string]any{"kind": "pack", "id": pack.ID, "version": pack.Version, "content": coverageRuntimeContent(m.Materials[0].Pack, "application/json")}, "governingPolicies": policies})
	r := coverageRuntimeFixtureReport("check")
	r.FormatVersion = policyAdoptionFormat
	r.CandidateCapabilities = json.RawMessage(adoptionRuntimeCapabilities)
	r.Profile = adoptionRuntimeContract.Profile
	r.ProfileVersion = policyAdoptionFormat
	r.SpecRevision = policyAdoptionSpecRevision
	r.Specification = adoptionRuntimeContract.Specification
	r.Maturity = "proposed-draft"
	r.Capabilities.Maturity = r.Maturity
	r.Capabilities.ProfileVersions = []string{policyAdoptionFormat}
	r.Capabilities.CheckScope = policyAdoptionCheckScope
	r.Capabilities.ValidationScope = adoptionRuntimeContract.ValidationScope
	r.Capabilities.MaxDepth = 32
	r.Capabilities.MaxCoreNumberBytes = 128
	r.Capabilities.MaxCoreExponentMagnitude = 308
	r.Capabilities.MaxArtifactVersionBytes = 256
	r.CandidateResult = &policyRuntimeCandidateResult{Status: "valid", FormatVersion: policyAdoptionFormat, Maturity: "proposed-draft"}
	r.Status = "references-valid"
	r.ValidationScope = policyAdoptionCheckScope
	r.Kind = "governing-adoption"
	r.ArtifactID = m.PortableID
	r.ArtifactVersion = "1.0.0"
	r.BytesDigest = "sha256:" + digestOf(doc)
	r.CanonicalDigest = r.BytesDigest
	report, _ := json.Marshal(r)
	return policyAdoptionCheck{Availability: "available", Report: report, prepared: &policyAdoptionPrepared{Document: doc, Materials: materials, Report: report}}
}
func adoptionApprovedCoverage(t *testing.T, service policyService, actor policyActor, p policyReply, pack policyCoveragePackSelector) policyAdoptionSelection {
	t.Helper()
	service.checkCoverage = coverageAcceptedFixture
	saved, err := service.coverageCommand(context.Background(), p.Document.ID, actor, coverageSaveBody(t, p, pack))
	if err != nil {
		t.Fatal(err)
	}
	approved, err := service.coverageCommand(context.Background(), p.Document.ID, actor, coverageReviewBody(t, *saved.Coverage))
	if err != nil {
		t.Fatal(err)
	}
	c := approved.Coverage
	return policyAdoptionSelection{PolicyID: p.Document.ID, CoverageID: c.Document.ID, Revision: c.Revision, CandidateID: c.Document.Candidate.ID, ReviewID: c.Document.CurrentReviewID}
}
func adoptionFixture(t *testing.T) (*Server, string, policyService, policyActor, policyAdoptionRequest) {
	t.Helper()
	s, token, p, pack := coverageFixture(t)
	service, actor := coverageServiceFixture(s, token)
	service.checkAdoption = adoptionAcceptedFixture
	selection := adoptionApprovedCoverage(t, service, actor, p, pack)
	return s, token, service, actor, policyAdoptionRequest{Action: "prepare", OperationID: policyOp(t), Pack: &pack, Coverages: []policyAdoptionSelection{selection}}
}
func adoptionReviewRequest(t *testing.T, r policyMutationReply) policyAdoptionRequest {
	t.Helper()
	a := r.Adoption
	return policyAdoptionRequest{Action: "review", OperationID: policyOp(t), AdoptionID: a.Document.ID, BaseRevision: a.Revision, CandidateDigest: a.Document.Candidate.Digest, BasisDigest: a.Document.Candidate.BasisDigest, Decision: "approve"}
}
func adoptionRequireFailure(t *testing.T, err error, code string) {
	t.Helper()
	var f *policyFailure
	if !errors.As(err, &f) || f.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}
func TestPolicyAdoptionDurableCompleteSetSeparateReviewAndRecovery(t *testing.T) {
	s, token, service, actor, q := adoptionFixture(t)
	second := policyCreate(t, s, token)
	second = policyApprove(t, s, token, second)
	second = policyMutation(t, policyCall(s, token, "POST", "/api/policies/"+second.Document.ID+"/publish", policyPublishBody(t, second))).Policy
	q.Coverages = append(q.Coverages, adoptionApprovedCoverage(t, service, actor, second, *q.Pack))
	prepared, err := service.adoptionCommand(context.Background(), actor, q)
	if err != nil {
		t.Fatal(err)
	}
	a := prepared.Adoption
	if a.Document.ReviewState != "unreviewed" || len(a.Document.Candidate.Dependencies) != 2 || a.Eligibility.Status != "not-yet-effective" || a.Readiness.Status != "ready" {
		t.Fatal(a)
	}
	if !validPolicyMutationIdentity(prepared) || !validPolicyAdoptionResult(prepared) {
		t.Fatal("invalid adoption operation identity")
	}
	approved, err := service.adoptionCommand(context.Background(), actor, adoptionReviewRequest(t, prepared))
	if err != nil {
		t.Fatal(err)
	}
	if approved.Adoption.Document.ReviewState != "reviewed" || approved.Adoption.Eligibility.Status != "not-yet-effective" {
		t.Fatal(approved)
	}
	service.checkAdoption = func(context.Context, policyAdoptionMaterial) policyAdoptionCheck {
		t.Fatal("recovered operation reran Runtime")
		return policyAdoptionCheck{}
	}
	recovered, err := service.adoptionCommand(context.Background(), actor, q)
	if err != nil || recovered.Delivery != "recovered" || recovered.Adoption.Revision != a.Revision {
		t.Fatal(recovered, err)
	}
	read, err := service.operation(context.Background(), q.OperationID)
	if err != nil || read.Delivery != "recovered" {
		t.Fatal(read, err)
	}
	changed := q
	changed.Coverages = changed.Coverages[:1]
	_, err = service.adoptionCommand(context.Background(), actor, changed)
	adoptionRequireFailure(t, err, "idempotency-conflict")
	service.authorize = func() bool { return false }
	_, err = service.adoptionCommand(context.Background(), actor, q)
	adoptionRequireFailure(t, err, CodeUnauthorized)
	_, err = service.operation(context.Background(), q.OperationID)
	adoptionRequireFailure(t, err, CodeUnauthorized)
	service.authorize = func() bool { return true }
	cut := archiveTestCut(t, s)
	if cut.Counts.Adoptions != 1 || cut.Counts.AdoptionReviews != 1 {
		t.Fatal(cut.Counts)
	}
}
func TestPolicyAdoptionRefusesSubstitutionsIncompleteAndStaleBasis(t *testing.T) {
	_, _, service, actor, q := adoptionFixture(t)
	tests := []struct {
		name  string
		alter func(*policyAdoptionRequest)
		code  string
	}{
		{"empty", func(q *policyAdoptionRequest) { q.Coverages = nil }, CodeBadRequest},
		{"duplicate", func(q *policyAdoptionRequest) { q.Coverages = append(q.Coverages, q.Coverages[0]) }, CodeBadRequest},
		{"candidate", func(q *policyAdoptionRequest) {
			q.Coverages[0].CandidateID = "adoption_candidate_" + strings.Repeat("a", 48)
		}, CodeBadRequest},
		{"review", func(q *policyAdoptionRequest) { q.Coverages[0].ReviewID = "coverage_review_" + strings.Repeat("a", 48) }, "revision-conflict"},
		{"source", func(q *policyAdoptionRequest) { q.Coverages[0].PolicyID = "policy_" + strings.Repeat("a", 48) }, CodeNotFound},
		{"pack", func(q *policyAdoptionRequest) { p := *q.Pack; p.Content.Bytes++; q.Pack = &p }, CodeNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			copy := q
			copy.OperationID = policyOp(t)
			copy.Coverages = append([]policyAdoptionSelection{}, q.Coverages...)
			tt.alter(&copy)
			_, err := service.adoptionCommand(context.Background(), actor, copy)
			adoptionRequireFailure(t, err, tt.code)
		})
	}
	prepared, err := service.adoptionCommand(context.Background(), actor, q)
	if err != nil {
		t.Fatal(err)
	}
	review := adoptionReviewRequest(t, prepared)
	original := service.checkAdoption
	service.checkAdoption = func(ctx context.Context, m policyAdoptionMaterial) policyAdoptionCheck {
		err := service.repository.Update(ctx, func(l *policyLedger) (bool, error) {
			p := l.Policies[q.Coverages[0].PolicyID]
			publication := p.Publications[0]
			l.Lifecycles = map[string]*policyLifecycleStream{p.ID: {Version: 1, Events: []policyLifecycleEvent{{ID: "lifecycle_" + strings.Repeat("a", 48), Sequence: 1, Action: "withdraw", PublicationID: publication.ID, CandidateDigest: publication.CandidateDigest, At: service.lifecycleNow(), Actor: actor, Provenance: "admin-configured", Rationale: "withdraw during check"}}}}
			return true, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return original(ctx, m)
	}
	_, err = service.adoptionCommand(context.Background(), actor, review)
	adoptionRequireFailure(t, err, "dependency-ineligible")
	got, err := service.adoptionGet(context.Background(), prepared.Adoption.Document.ID)
	if err != nil || got.Document.ReviewState != "unreviewed" || got.Readiness.Status != "stale" || got.Eligibility.Status != "ineligible" {
		t.Fatal(got, err)
	}
}
func TestPolicyAdoptionArchiveClosedAndRestoreInactive(t *testing.T) {
	s, _, service, actor, q := adoptionFixture(t)
	prepared, err := service.adoptionCommand(context.Background(), actor, q)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := service.adoptionCommand(context.Background(), actor, adoptionReviewRequest(t, prepared))
	if err != nil {
		t.Fatal(err)
	}
	cut := archiveTestCut(t, s)
	snapshot := policyRecoverySnapshot{SnapshotID: strings.Repeat("a", 64), ArchiveDigest: strings.Repeat("b", 64), LedgerDigest: digestOf(cut.Raw), At: service.lifecycleNow(), Actor: actor}
	err = service.repository.Update(context.Background(), func(l *policyLedger) (bool, error) { return true, restorePolicyRecoveryBarrier(l, snapshot) })
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.adoptionGet(context.Background(), prepared.Adoption.Document.ID)
	if err != nil || got.Document.ReviewState != "unreviewed" || got.Recovery == nil || !got.Recovery.FreshReviewRequired || got.Eligibility.Status != "ineligible" {
		t.Fatal(got, err)
	}
	_, err = service.adoptionCommand(context.Background(), actor, adoptionReviewRequest(t, policyMutationReply{Adoption: &got}))
	if err == nil {
		t.Fatal("restored approval acquired authority")
	}
	_ = archiveTestCut(t, s)
	restrictive := adoptionReviewRequest(t, policyMutationReply{Adoption: &got})
	restrictive.Decision = "request-changes"
	if _, err := service.adoptionCommand(context.Background(), actor, restrictive); err != nil {
		t.Fatal("restored restrictive review", err)
	}
	second := snapshot
	second.SnapshotID = strings.Repeat("c", 64)
	if err := service.repository.Update(context.Background(), func(l *policyLedger) (bool, error) { return true, restorePolicyRecoveryBarrier(l, second) }); err != nil {
		t.Fatal(err)
	}
	_ = archiveTestCut(t, s)
	for _, ref := range approved.Adoption.Document.Candidate.Materials {
		missing := &policyUnavailableLifecycleArtifacts{base: service.artifacts, missing: map[string]bool{ref.SHA256: true}}
		if _, err := policyArchiveClosure(context.Background(), cut.Ledger, missing); err == nil {
			t.Fatal("missing adoption material accepted")
		}
	}
	for _, field := range []string{"revision", "reviewState", "currentReviewId"} {
		operation := cut.Ledger.Operations[approved.Receipt.OperationID]
		raw, e := service.artifacts.Get(context.Background(), operation.Result)
		if e != nil {
			t.Fatal(e)
		}
		var outcome policyMutationReply
		if json.Unmarshal(raw, &outcome) != nil {
			t.Fatal("bad fixture operation")
		}
		switch field {
		case "revision":
			outcome.Adoption.Revision = strings.Repeat("e", 64)
			outcome.Receipt.AdoptionRevision = outcome.Adoption.Revision
		case "reviewState":
			outcome.Adoption.Document.ReviewState = "unreviewed"
		case "currentReviewId":
			outcome.Adoption.Document.CurrentReviewID = "adoption_review_" + strings.Repeat("e", 48)
		}
		raw, _ = json.Marshal(outcome)
		operation.Result, e = service.artifacts.Put(context.Background(), raw)
		if e != nil {
			t.Fatal(e)
		}
		if err := validateArchivedPolicyOperation(context.Background(), cut.Ledger, service.artifacts, operation); err == nil {
			t.Fatalf("forged %s archive receipt accepted", field)
		}
	}
	// Removing a retained policy material while forging a new private envelope
	// must still fail closure validation, even though both digest refs are valid.
	l := cut.Ledger
	r := l.Adoptions[approved.Adoption.Document.ID]
	content, err := service.adoptionContent(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	content.Materials = content.Materials[:1]
	raw, _ := json.Marshal(content)
	r.Content, err = service.artifacts.Put(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	l.Adoptions[r.ID] = r
	if _, err := policyArchiveClosure(context.Background(), l, service.artifacts); err == nil {
		t.Fatal("incomplete adoption archive accepted")
	}
}
func TestPolicyAdoptionRealRuntimeRawNumbersAndMaterialCaps(t *testing.T) {
	bin := os.Getenv("JPACK_POLICY_TEST_BIN")
	if bin == "" {
		t.Skip("set JPACK_POLICY_TEST_BIN to candidate-capable Runtime")
	}
	s, token, p, _ := coverageFixture(t)
	s.cfg.JpackBin = bin
	raw := []byte(strings.Replace(strings.TrimSpace(strings.Replace(reviewPack, `"version":"0.1.0"`, `"version":"0.2.0"`, 1)), `}]}`, `}],"extensions":{"example.test":{"rawNumber":9007199254740993,"rawDecimal":1.2300,"rawExponent":1e+308}}}`, 1))
	pack := coveragePublishFixture(t, s, raw)
	service, actor := coverageServiceFixture(s, token)
	selection := adoptionApprovedCoverage(t, service, actor, p, pack)
	service.checkAdoption = func(ctx context.Context, m policyAdoptionMaterial) policyAdoptionCheck {
		c := s.checkPolicyAdoption(ctx, m)
		if !adoptionCheckAccepted(c) {
			t.Logf("check: %+v %s", c, c.Report)
		}
		return c
	}
	q := policyAdoptionRequest{Action: "prepare", OperationID: policyOp(t), Pack: &pack, Coverages: []policyAdoptionSelection{selection}}
	prepared, err := service.adoptionCommand(context.Background(), actor, q)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ref := range prepared.Adoption.Document.Candidate.Materials {
		data, e := service.artifacts.Get(context.Background(), ref)
		if e != nil {
			t.Fatal(e)
		}
		if bytes.Equal(data, raw) {
			found = true
		}
	}
	if !found {
		t.Fatal("raw Core numeric bytes were reserialized")
	}
	_, err = service.adoptionCommand(context.Background(), actor, adoptionReviewRequest(t, prepared))
	if err != nil {
		t.Fatal(err)
	}
	_ = archiveTestCut(t, s)
	var material policyAdoptionMaterial
	err = service.repository.View(context.Background(), func(l policyLedger) error {
		var e error
		resolved, e := service.coveragePack(context.Background(), pack)
		if e != nil {
			return e
		}
		material, e = service.adoptionResolve(context.Background(), l, resolved, q.Coverages)
		material.PortableID = prepared.Adoption.Document.Candidate.PortableID
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	original := material.Materials[0]
	material.Materials = nil
	for i := 0; i < 16; i++ {
		v := original
		v.Content.Source.PolicyID = "policy_" + fmt.Sprintf("%048x", i)
		material.Materials = append(material.Materials, v)
	}
	if got := s.checkPolicyAdoption(context.Background(), material); got.Availability != "unsupported" || got.prepared != nil {
		t.Fatal("16 policies exceeded16 material registrations but were retained", got)
	}
	material.Materials = []policyCoverageMaterial{original}
	material.Materials[0].Pack = bytes.Repeat([]byte(" "), (1<<20)+1)
	if got := s.checkPolicyAdoption(context.Background(), material); got.Availability != "unsupported" {
		t.Fatal(got)
	}
	s.cfg.JpackBin = filepath.Join(t.TempDir(), "absent-runtime")
	if got := s.checkPolicyAdoption(context.Background(), material); got.Availability != "unavailable" {
		t.Fatal(got)
	}
}
func TestPolicyAdoptionRuntimeStrictCandidateResponse(t *testing.T) {
	r := coverageRuntimeFixtureReport("check")
	if validPolicyRuntimeReport(r, "check", adoptionRuntimeContract) {
		t.Fatal("prototype accepted as candidate")
	}
	for _, field := range []string{"formatVersion", "candidateCapabilities", "candidateResult"} {
		raw, _ := json.Marshal(r)
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields)
		fields[field] = json.RawMessage("null")
		if field == "formatVersion" {
			fields[field] = json.RawMessage(`""`)
		}
		raw, _ = json.Marshal(fields)
		var decoded policyCoverageRuntimeReport
		if json.Unmarshal(raw, &decoded) == nil {
			t.Fatal("prototype response strictness weakened", field)
		}
	}
	_, _, service, actor, q := adoptionFixture(t)
	original := service.checkAdoption
	for _, scenario := range []string{"authority", "source", "format", "unknown", "missing-disclaimer", "scope"} {
		t.Run(scenario, func(t *testing.T) {
			service.checkAdoption = func(ctx context.Context, m policyAdoptionMaterial) policyAdoptionCheck {
				c := original(ctx, m)
				var fields map[string]any
				_ = json.Unmarshal(c.Report, &fields)
				switch scenario {
				case "authority":
					fields["capabilities"].(map[string]any)["executionAuthority"] = true
				case "source":
					fields["specRevision"] = strings.Repeat("0", 40)
				case "format":
					fields["formatVersion"] = "2.0.0"
				case "unknown":
					fields["permission"] = true
				case "missing-disclaimer":
					delete(fields["capabilities"].(map[string]any), "approvalVerification")
				case "scope":
					fields["validationScope"] = "full-conformance"
				}
				c.Report, _ = json.Marshal(fields)
				return c
			}
			copy := q
			copy.OperationID = policyOp(t)
			_, err := service.adoptionCommand(context.Background(), actor, copy)
			if err == nil {
				t.Fatal("invalid Runtime authority accepted")
			}
		})
	}
}

func TestPolicyAdoptionPrecommitUncertainWriteAndCurrentAuthorization(t *testing.T) {
	s, _, service, actor, q := adoptionFixture(t)
	base := service.artifacts
	for _, failAt := range []int{1, 2, 3} {
		service.artifacts = &policyFailingArtifacts{base: base, failAt: failAt}
		q.OperationID = policyOp(t)
		_, err := service.adoptionCommand(context.Background(), actor, q)
		w := httptest.NewRecorder()
		policyHTTPFailure(w, err)
		var failure map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &failure)
		if w.Code != 503 || failure["outcome"] != "not-committed" {
			t.Fatal(w.Code, failure, err)
		}
	}
	service.artifacts = base
	reads := 0
	testHookPrivateRead = func(name string) error {
		if name == "ledger.json" {
			reads++
			if reads == 4 {
				return errors.New("controlled adoption read-back failure")
			}
		}
		return nil
	}
	t.Cleanup(func() { testHookPrivateRead = nil })
	q.OperationID = policyOp(t)
	_, err := service.adoptionCommand(context.Background(), actor, q)
	w := httptest.NewRecorder()
	policyHTTPFailure(w, err)
	var failure map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &failure)
	if w.Code != 503 || failure["outcome"] != "unknown" {
		t.Fatal(w.Code, failure, err, reads)
	}
	testHookPrivateRead = nil
	recovered, err := service.operation(context.Background(), q.OperationID)
	if err != nil || recovered.Delivery != "recovered" || recovered.Adoption.Document.ReviewState != "unreviewed" {
		t.Fatal(recovered, err)
	}
	service.checkAdoption = func(context.Context, policyAdoptionMaterial) policyAdoptionCheck {
		t.Fatal("uncertain write was automatically replayed")
		return policyAdoptionCheck{}
	}
	again, err := service.adoptionCommand(context.Background(), actor, q)
	if err != nil || again.Delivery != "recovered" || again.Adoption.Document.ID != recovered.Adoption.Document.ID {
		t.Fatal(again, err)
	}
	permitted := true
	service.authorize = func() bool { return permitted }
	service.checkAdoption = func(ctx context.Context, m policyAdoptionMaterial) policyAdoptionCheck {
		permitted = false
		return adoptionAcceptedFixture(ctx, m)
	}
	q.OperationID = policyOp(t)
	_, err = service.adoptionCommand(context.Background(), actor, q)
	adoptionRequireFailure(t, err, CodeUnauthorized)
	permitted = true
	if _, err = service.operation(context.Background(), q.OperationID); err == nil {
		t.Fatal("authorization lost during Runtime nevertheless committed")
	}
	_ = s
}

func TestPolicyAdoptionRechecksClockPublicationAndCoverage(t *testing.T) {
	for _, scenario := range []string{"expiry", "publication", "coverage"} {
		t.Run(scenario, func(t *testing.T) {
			_, _, service, actor, q := adoptionFixture(t)
			now := time.Now().UTC().Truncate(time.Second)
			service.clock = func() time.Time { return now }
			if scenario == "expiry" {
				err := service.repository.Update(context.Background(), func(l *policyLedger) (bool, error) {
					p := l.Policies[q.Coverages[0].PolicyID]
					pub := p.Publications[0]
					l.Lifecycles = map[string]*policyLifecycleStream{p.ID: {Version: 1, Events: []policyLifecycleEvent{{ID: "lifecycle_" + strings.Repeat("b", 48), Sequence: 1, Action: "activate", PublicationID: pub.ID, CandidateDigest: pub.CandidateDigest, At: service.lifecycleNow(), EffectiveAt: service.lifecycleNow(), ExpiresAt: now.Add(time.Minute).Format("2006-01-02T15:04:05Z"), Actor: actor, Provenance: "admin-configured"}}}}
					return true, nil
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			prepared, err := service.adoptionCommand(context.Background(), actor, q)
			if err != nil {
				t.Fatal(err)
			}
			original := service.checkAdoption
			packRead := service.coveragePack
			changed := false
			service.coveragePack = func(ctx context.Context, p policyCoveragePackSelector) (policyCoverageResolvedPack, error) {
				resolved, e := packRead(ctx, p)
				if changed && scenario == "publication" {
					resolved.Raw = append(append([]byte{}, resolved.Raw...), byte(' '))
				}
				return resolved, e
			}
			service.checkAdoption = func(ctx context.Context, m policyAdoptionMaterial) policyAdoptionCheck {
				checked := original(ctx, m)
				switch scenario {
				case "expiry":
					now = now.Add(2 * time.Minute)
				case "publication":
					changed = true
				case "coverage":
					selection := q.Coverages[0]
					current, err := service.coverageGet(ctx, selection.PolicyID, selection.CoverageID, "")
					if err != nil {
						t.Fatal(err)
					}
					review := coverageReviewBody(t, current)
					review.Decision = "request-changes"
					if _, err := service.coverageCommand(ctx, selection.PolicyID, actor, review); err != nil {
						t.Fatal(err)
					}
				}
				return checked
			}
			review := adoptionReviewRequest(t, prepared)
			_, err = service.adoptionCommand(context.Background(), actor, review)
			code := "revision-conflict"
			if scenario == "expiry" {
				code = "dependency-ineligible"
			}
			adoptionRequireFailure(t, err, code)
			if _, err = service.operation(context.Background(), review.OperationID); err == nil {
				t.Fatal("changed basis committed approval")
			}
		})
	}
}
func TestPolicyAdoptionOptionsUseExactSavedBytesAndClosedHTTP(t *testing.T) {
	s, token, service, actor, q := adoptionFixture(t)
	if err := os.MkdirAll(filepath.Join(s.projectDir, "packs"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.projectDir, runtimeConfigName), []byte(`{"packs":{"chosen":{"path":"packs/chosen.json"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.projectDir, "packs/chosen.json"), []byte(reviewPack), 0600); err != nil {
		t.Fatal(err)
	}
	policyError(t, policyCall(s, "", "GET", "/api/policy-adoptions", nil), 401, CodeUnauthorized)
	policyError(t, policyCall(s, testToken, "GET", "/api/policy-adoptions", nil), 401, CodeUnauthorized)
	policyError(t, policyCall(s, token, "GET", "/api/policy-adoptions?other=true", nil), 400, CodeBadRequest)
	policyError(t, policyCall(s, token, "GET", "/api/policy-adoptions/options?pack=chosen&pack=chosen", nil), 400, CodeBadRequest)
	r := httptest.NewRequest("POST", "/api/policy-adoptions", strings.NewReader(`{"action":"prepare","action":"review","operationId":"duplicate_operation"}`))
	r.Header.Set("Authorization", "Bearer "+token)
	duplicate := httptest.NewRecorder()
	s.ServeHTTP(duplicate, r)
	if duplicate.Code != 400 {
		t.Fatal("duplicate JSON field accepted", duplicate.Body.String())
	}
	options := policyCall(s, token, "GET", "/api/policy-adoptions/options?pack=chosen", nil)
	var selected struct {
		PackPublications []policyCoveragePackChoice `json:"packPublications"`
		Coverages        []policyAdoptionChoice     `json:"coverages"`
	}
	if options.Header().Get("Cache-Control") != "no-store" || options.Code != 200 || json.Unmarshal(options.Body.Bytes(), &selected) != nil || len(selected.PackPublications) != 1 || len(selected.Coverages) != 1 {
		t.Fatal(options.Code, options.Body.String())
	}
	if err := os.WriteFile(filepath.Join(s.projectDir, "packs/chosen.json"), append([]byte(reviewPack), ' '), 0600); err != nil {
		t.Fatal(err)
	}
	options = policyCall(s, token, "GET", "/api/policy-adoptions/options?pack=chosen", nil)
	if options.Code != 200 || json.Unmarshal(options.Body.Bytes(), &selected) != nil || len(selected.PackPublications) != 0 || len(selected.Coverages) != 0 {
		t.Fatal("changed saved bytes substituted publication", options.Code, options.Body.String())
	}
	// Exact retained publication remains selectable by its own immutable identity.
	prepared, err := service.adoptionCommand(context.Background(), actor, q)
	if err != nil {
		t.Fatal(err)
	}
	history := policyCall(s, token, "GET", "/api/policy-adoptions/"+prepared.Adoption.Document.ID+"/history", nil)
	if history.Code != 200 {
		t.Fatal(history.Code, history.Body.String())
	}
	for _, bad := range []any{map[string]any{"action": "prepare", "operationId": policyOp(t), "pack": q.Pack, "coverages": q.Coverages, "approved": true}, map[string]any{"Action": "prepare", "operationId": policyOp(t), "pack": q.Pack, "coverages": q.Coverages}} {
		policyError(t, policyCall(s, token, "POST", "/api/policy-adoptions", bad), 400, CodeBadRequest)
	}
	s.sessions.revoke(token)
	policyError(t, policyCall(s, token, "GET", "/api/policy-adoptions", nil), 401, CodeUnauthorized)
	policyError(t, policyCall(s, token, "GET", "/api/policy-adoptions/"+prepared.Adoption.Document.ID+"/history", nil), 401, CodeUnauthorized)
}

func TestPolicyAdoptionRealRuntimeArchiveResolvesExactPolicyRole(t *testing.T) {
	bin := os.Getenv("JPACK_POLICY_TEST_BIN")
	if bin == "" {
		t.Skip("set JPACK_POLICY_TEST_BIN to candidate-capable Runtime")
	}
	s, token, p, pack := coverageFixture(t)
	s.cfg.JpackBin = bin
	service, actor, saved, material := coverageMaterialFixture(t, s, token, p, pack)
	// This basis is valid text/plain evidence. Its JSON meaning matches the
	// generated Policy, whose document does not depend on the statement basis.
	basis, err := json.MarshalIndent(adoptionRuntimePolicy(material), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	edit := coverageSaveBody(t, p, pack)
	edit.CoverageID = saved.Document.ID
	edit.BaseRevision = saved.Revision
	edit.Statement.Basis = string(basis)
	changed, err := service.coverageCommand(context.Background(), p.Document.ID, actor, edit)
	if err != nil {
		t.Fatal(err)
	}
	coverage, err := service.coverageCommand(context.Background(), p.Document.ID, actor, coverageReviewBody(t, *changed.Coverage))
	if err != nil {
		t.Fatal(err)
	}
	c := coverage.Coverage
	q := policyAdoptionRequest{Action: "prepare", OperationID: policyOp(t), Pack: &pack, Coverages: []policyAdoptionSelection{{PolicyID: p.Document.ID, CoverageID: c.Document.ID, Revision: c.Revision, CandidateID: c.Document.Candidate.ID, ReviewID: c.Document.CurrentReviewID}}}
	prepared, err := service.adoptionCommand(context.Background(), actor, q)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := service.adoptionCommand(context.Background(), actor, adoptionReviewRequest(t, prepared))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := service.artifacts.Get(context.Background(), approved.Adoption.Document.Candidate.Document)
	if err != nil {
		t.Fatal(err)
	}
	var adoption struct {
		GoverningPolicies []struct {
			Policy struct {
				Content policySourceContent `json:"content"`
			} `json:"policy"`
		} `json:"governingPolicies"`
	}
	if json.Unmarshal(doc, &adoption) != nil || len(adoption.GoverningPolicies) != 1 {
		t.Fatal("invalid retained adoption document")
	}
	policyRef := adoption.GoverningPolicies[0].Policy.Content
	canonical, err := service.artifacts.Get(context.Background(), retainedArtifactRef{SHA256: strings.TrimPrefix(policyRef.BytesDigest, "sha256:"), Bytes: policyRef.ByteLength})
	if err != nil || bytes.Equal(canonical, basis) || !sameDialectJSON(canonical, basis) || policyRef.MediaType != "application/json" {
		t.Fatal("fixture lacks byte-distinct semantically equal Policy and basis roles", err)
	}
	cut := archiveTestCut(t, s)
	if cut.Counts.Adoptions != 1 || cut.Counts.AdoptionReviews != 1 {
		t.Fatal(cut.Counts)
	}
	for _, data := range [][]byte{basis, canonical} {
		found := false
		for _, ref := range cut.Refs {
			if ref.SHA256 == digestOf(data) {
				found = true
			}
		}
		if !found {
			t.Fatal("archive lost an exact required material role")
		}
	}
}
