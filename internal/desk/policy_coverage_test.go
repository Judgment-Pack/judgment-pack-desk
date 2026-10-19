package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func coverageFixture(t *testing.T) (*Server, string, policyReply, policyCoveragePackSelector) {
	t.Helper()
	s, token := policyTestServer(t)
	p := policyCreate(t, s, token)
	p = policyApprove(t, s, token, p)
	published := policyMutation(t, policyCall(s, token, "POST", "/api/policies/"+p.Document.ID+"/publish", policyPublishBody(t, p)))
	return s, token, published.Policy, coveragePublishFixture(t, s, []byte(reviewPack))
}
func coveragePublishFixture(t *testing.T, s *Server, raw []byte) policyCoveragePackSelector {
	t.Helper()
	var head struct {
		ID      string `json:"id"`
		Version string `json:"version"`
	}
	if json.Unmarshal(raw, &head) != nil {
		t.Fatal("invalid fixture Pack")
	}
	content, err := s.artifactRepository().Put(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	config, err := s.artifactRepository().Put(context.Background(), []byte(`{"fixture":"coverage"}`))
	if err != nil {
		t.Fatal(err)
	}
	p := artifactPublication{At: time.Now().UTC().Format(time.RFC3339Nano), Config: &config, Documents: []publishedDocument{{Kind: "pack", Alias: "synthetic", ID: head.ID, Version: head.Version, Path: "packs/coverage.json", Content: content}}}
	p.ID = publicationID(p)
	tx, err := s.openPublicationTransaction(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.close()
	tx.candidate = p
	if err = tx.commit(); err != nil {
		t.Fatal(err)
	}
	return coveragePackSelector(policyCoveragePack{PublicationID: p.ID, Document: p.Documents[0]})
}
func coverageSaveBody(t *testing.T, p policyReply, pack policyCoveragePackSelector) policyCoverageRequest {
	t.Helper()
	return policyCoverageRequest{Action: "save", OperationID: policyOp(t), PolicyPublicationID: p.Document.Publications[0].ID, Pack: &pack, Scope: &policyCoverageScope{Kind: "whole-narrative"}, Statement: &policyCoverageStatement{Status: "implemented", ConsumerElements: []policyCoverageElement{{Kind: "rule", ID: "literal-rule"}}, Basis: "Reviewed the whole retained published narrative against the exact Pack rule."}}
}
func coverageReplyOf(t *testing.T, s *Server, token, policyID, coverageID string) policyCoverageReply {
	t.Helper()
	w := policyCall(s, token, "GET", "/api/policies/"+policyID+"/coverage/"+coverageID, nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var r policyCoverageReply
	if json.Unmarshal(w.Body.Bytes(), &r) != nil {
		t.Fatal(w.Body.String())
	}
	return r
}
func coverageServiceFixture(s *Server, token string) (policyService, policyActor) {
	r := httptest.NewRequest("POST", "/api/policies", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	held, _ := s.sessionOf(r)
	actor := policyActorOf(held)
	return s.localPolicyService(r, actor), actor
}
func coverageReviewBody(t *testing.T, r policyCoverageReply) policyCoverageRequest {
	t.Helper()
	return policyCoverageRequest{Action: "review", OperationID: policyOp(t), CoverageID: r.Document.ID, BaseRevision: r.Revision, CandidateDigest: r.Document.Candidate.Digest, PolicyRevision: r.PolicyRevision, Decision: "approve"}
}
func coverageRuntimeFixtureReport(operation string) policyCoverageRuntimeReport {
	r := policyCoverageRuntimeReport{OutputVersion: "2", Command: "experimental policy " + operation, ContractVersion: "1", Specification: "https://github.com/Judgment-Pack/judgment-pack-spec/blob/" + policyCoverageSpecRevision + "/rfcs/0023-policy-companion.md", SpecRevision: policyCoverageSpecRevision, Experimental: true, Maturity: "non-normative", Profile: policyCoverageProfile, ProfileVersion: policyCoverageProfileVersion, Diagnostics: []policyCoverageRuntimeDiagnostic{}}
	r.Tool.Name = "jpack"
	r.Tool.Version = "controlled-test-fixture"
	r.Capabilities = policyCoverageRuntimeCapabilities{Kinds: []string{"policy", "governing-adoption"}, ProfileVersions: []string{policyCoverageProfileVersion}, CanonicalProfiles: []string{"jps-canon"}, ConsumerKinds: []string{"pack"}, ConsumerElementKinds: []string{"rule", "exception", "applicability", "fallback"}, Operations: []string{"contract", "canonicalize", "check"}, ValidationScope: "closed-prototype-document-and-local-references", CheckScope: policyCoverageCheckScope, MaxDocumentBytes: 262144, MaxMaterialBytes: 1048576, MaxOriginalBytes: 4194304, MaxTotalMaterialBytes: 16777216, MaxMaterials: 16, MaxMaterialsWireBytes: 24 << 20, Experimental: true, Maturity: "non-normative"}
	return r
}

// A controlled service dependency tests storage/admission races, not Runtime
// conformance. The separate real-binary test exercises actual canonical/check output.
func coverageAcceptedFixture(_ context.Context, m policyCoverageMaterial) policyCoverageCheckReply {
	policy, _ := json.Marshal(coverageRuntimePolicy(m))
	doc, _ := json.Marshal(map[string]any{"prototype": map[string]any{"profile": policyCoverageProfile, "proposalVersion": policyCoverageProfileVersion, "status": "non-normative"}, "kind": "governing-adoption", "id": "urn:desk:controlled-coverage", "version": "1.0.0", "consumer": map[string]any{"kind": "pack", "id": m.Content.Pack.Document.ID, "version": m.Content.Pack.Document.Version, "content": coverageRuntimeContent(m.Pack, "application/json")}, "governingPolicies": []any{map[string]any{"policy": map[string]any{"kind": "policy", "id": "urn:desk:policy:" + m.Content.Source.PolicyID, "version": m.Content.Source.ArtifactVersion, "content": coverageRuntimeContent(policy, "application/json")}, "scope": map[string]any{"kind": "whole-policy"}, "coverage": []any{coverageRuntimeStatement(m.Content.Statement)}}}})
	report := coverageRuntimeFixtureReport("check")
	report.Status = "references-valid"
	report.Kind = "governing-adoption"
	report.ArtifactID = "urn:desk:controlled-coverage"
	report.ArtifactVersion = "1.0.0"
	report.ValidationScope = policyCoverageCheckScope
	report.BytesDigest = "sha256:" + digestOf(doc)
	report.CanonicalDigest = report.BytesDigest
	raw, _ := json.Marshal(report)
	materials := [][]byte{m.Pack, []byte(m.Content.Source.Narrative), policy}
	if m.Content.Statement.Status == "implemented" {
		materials = append(materials, []byte(m.Content.Statement.Basis))
	}
	if m.OriginalSource != nil {
		materials = append(materials, m.Original)
	}
	return policyCoverageCheckReply{Availability: "available", Report: raw, prepared: &policyCoveragePreparedCheck{Document: doc, Materials: materials}}
}
func TestPolicyCoverageOfflineDraftExactBasisAndRecovery(t *testing.T) {
	s, token, p, pack := coverageFixture(t)
	path := "/api/policies/" + p.Document.ID + "/coverage"
	q := coverageSaveBody(t, p, pack)
	saved := policyMutation(t, policyCall(s, token, "POST", path, q))
	c := saved.Coverage
	if saved.OutcomeKind != "coverage" || c == nil || c.Document.ReviewState != "unreviewed" || saved.Receipt.CoverageRevision != c.Revision || saved.Receipt.CoverageCandidateID != c.Document.Candidate.ID || saved.Receipt.CandidateDigest != c.Document.Candidate.Digest {
		t.Fatal(saved)
	}
	if c.Document.Candidate.Source.CandidateDigest == digestOf([]byte(c.Document.Candidate.Source.Narrative)) || c.Document.Candidate.Source.Content != p.Document.Publications[0].Content || c.Inventory.Rules[0] != "literal-rule" {
		t.Fatal("identity domains or inventory changed", c)
	}
	if after := policyRead(t, s, token, p.Document.ID); after.Revision != p.Revision || after.Document.CurrentReviewID != p.Document.CurrentReviewID {
		t.Fatal("coverage modified Policy approval")
	}
	// Later mutable files and Policy heads cannot substitute a selected publication.
	if err := os.MkdirAll(filepath.Join(s.projectDir, "packs"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.projectDir, "packs/coverage.json"), []byte(`{"id":"different"}`), 0600); err != nil {
		t.Fatal(err)
	}
	edit := policyEditBody(t, p)
	edit.Narrative = "New unrelated draft."
	policyMutation(t, policyCall(s, token, "PUT", "/api/policies/"+p.Document.ID, edit))
	reloaded := coverageReplyOf(t, s, token, p.Document.ID, c.Document.ID)
	if reloaded.Document.Candidate.Source.Narrative != p.Document.Candidate.Narrative || reloaded.Document.Candidate.Pack.Document.Content != pack.Content {
		t.Fatal("selected basis advanced")
	}
	s.cfg.JpackBin = filepath.Join(t.TempDir(), "absent-runtime")
	again := policyMutation(t, policyCall(s, token, "POST", path, q))
	if again.Delivery != "recovered" || again.Coverage.Revision != c.Revision {
		t.Fatal("completed request rechecked Runtime or current state")
	}
	got := policyMutation(t, policyCall(s, token, "GET", "/api/policy-operations/"+q.OperationID, nil))
	if got.Delivery != "recovered" {
		t.Fatal(got)
	}
	changed := q
	changed.Statement = &policyCoverageStatement{Status: "manual", Handoff: "Reviewed manual process"}
	policyError(t, policyCall(s, token, "POST", path, changed), 409, "idempotency-conflict")
	var opts struct {
		PolicyPublications []policyCoveragePublicationChoice `json:"policyPublications"`
		PackPublications   []policyCoveragePackChoice        `json:"packPublications"`
	}
	w := policyCall(s, token, "GET", "/api/policies/"+p.Document.ID+"/coverage-options", nil)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &opts) != nil || len(opts.PolicyPublications) != 1 || len(opts.PackPublications) != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = policyCall(s, token, "POST", path+"/resolve", policyCoverageResolveRequest{PolicyPublicationID: q.PolicyPublicationID, Pack: pack})
	var resolved policyCoverageBasis
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &resolved) != nil || resolved.Source.Narrative != p.Document.Candidate.Narrative || resolved.Inventory.Rules[0] != "literal-rule" {
		t.Fatal(w.Code, w.Body.String())
	}
	w = policyCall(s, token, "POST", path+"/"+c.Document.ID+"/check", policyCoverageCheckRequest{BaseRevision: reloaded.Revision, CandidateDigest: reloaded.Document.Candidate.Digest, PolicyRevision: reloaded.PolicyRevision})
	var check policyCoverageCheckReply
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &check) != nil || check.Availability != "unavailable" {
		t.Fatal(w.Code, w.Body.String())
	}
	historical := policyCall(s, token, "GET", path+"/"+c.Document.ID+"/candidates/"+c.Document.Candidate.ID, nil)
	if historical.Code != 200 {
		t.Fatal(historical.Code, historical.Body.String())
	}
}
func TestPolicyCoverageReviewCASHistoryAndPolicyContext(t *testing.T) {
	s, token, p, pack := coverageFixture(t)
	service, actor := coverageServiceFixture(s, token)
	service.checkCoverage = coverageAcceptedFixture
	q := coverageSaveBody(t, p, pack)
	saved, err := service.coverageCommand(context.Background(), p.Document.ID, actor, q)
	if err != nil {
		t.Fatal(err)
	}
	review := coverageReviewBody(t, *saved.Coverage)
	approved, err := service.coverageCommand(context.Background(), p.Document.ID, actor, review)
	if err != nil {
		t.Fatal(err)
	}
	if approved.Coverage.Document.ReviewState != "reviewed" || approved.Coverage.Document.Reviews[0].Check == nil {
		t.Fatal(approved)
	}
	// Completed approval is historical and works during a later Runtime outage.
	service.checkCoverage = nil
	recovered, err := service.coverageCommand(context.Background(), p.Document.ID, actor, review)
	if err != nil || recovered.Delivery != "recovered" {
		t.Fatal(recovered, err)
	}
	oldDigest := approved.Coverage.Document.Candidate.Digest
	oldID := approved.Coverage.Document.Candidate.ID
	update := q
	update.OperationID = policyOp(t)
	update.CoverageID = approved.Coverage.Document.ID
	update.BaseRevision = approved.Coverage.Revision
	update.Statement = &policyCoverageStatement{Status: "manual", Handoff: "Use the named retained manual review process."}
	changed, err := service.coverageCommand(context.Background(), p.Document.ID, actor, update)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Coverage.Document.ReviewState != "unreviewed" || len(changed.Coverage.Document.Candidates) != 2 || len(changed.Coverage.Document.Reviews) != 1 {
		t.Fatal(changed)
	}
	historical, err := service.coverageGet(context.Background(), p.Document.ID, update.CoverageID, oldID)
	if err != nil || historical.Document.Candidate.Digest != oldDigest || historical.Document.ReviewState != "reviewed" {
		t.Fatal(historical, err)
	}
	stale := coverageReviewBody(t, *changed.Coverage)
	edit := policyEditBody(t, p)
	edit.Title = "New mutable Policy context"
	policyMutation(t, policyCall(s, token, "PUT", "/api/policies/"+p.Document.ID, edit))
	_, err = service.coverageCommand(context.Background(), p.Document.ID, actor, stale)
	var failure *policyFailure
	if !errors.As(err, &failure) || failure.Code != "revision-conflict" || failure.CurrentPolicyRevision == "" {
		t.Fatal(err)
	}
	current := coverageReplyOf(t, s, token, p.Document.ID, update.CoverageID)
	request := coverageReviewBody(t, current)
	request.Decision = "request-changes"
	requested, err := service.coverageCommand(context.Background(), p.Document.ID, actor, request)
	if err != nil || requested.Coverage.Document.ReviewState != "changes-requested" {
		t.Fatal(requested, err)
	}
}
func TestPolicyCoverageStrictInputAuthorizationAndUnavailableApproval(t *testing.T) {
	s, token, p, pack := coverageFixture(t)
	path := "/api/policies/" + p.Document.ID + "/coverage"
	q := coverageSaveBody(t, p, pack)
	saved := policyMutation(t, policyCall(s, token, "POST", path, q))
	review := coverageReviewBody(t, *saved.Coverage)
	for _, route := range []string{path, path + "-options", path + "/" + saved.Coverage.Document.ID, path + "/" + saved.Coverage.Document.ID + "/candidates/" + saved.Coverage.Document.Candidate.ID} {
		policyError(t, policyCall(s, "", "GET", route, nil), 401, CodeUnauthorized)
	}
	for _, mutation := range []func(map[string]any){func(m map[string]any) { m["actor"] = map[string]any{"kind": "local", "subject": "forged"} }, func(m map[string]any) {
		m["PolicyPublicationId"] = m["policyPublicationId"]
		delete(m, "policyPublicationId")
	}, func(m map[string]any) { m["coverageId"] = nil }, func(m map[string]any) { m["comment"] = "unrelated save field" }, func(m map[string]any) { m["scope"] = map[string]any{"kind": "headings"} }, func(m map[string]any) {
		m["statement"] = map[string]any{"status": "manual", "handoff": "A process", "basis": "unrelated"}
	}, func(m map[string]any) {
		m["statement"] = map[string]any{"status": "implemented", "basis": "reviewed", "consumerElements": []any{map[string]any{"kind": "fallback", "id": ""}}}
	}} {
		raw, _ := json.Marshal(q)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		body["operationId"] = policyOp(t)
		mutation(body)
		policyError(t, policyCall(s, token, "POST", path, body), 400, CodeBadRequest)
	}
	policyError(t, policyCall(s, token, "POST", path, review), 503, "runtime-unavailable")
	// Request changes does not manufacture Runtime success.
	review.OperationID = policyOp(t)
	review.Decision = "request-changes"
	result := policyMutation(t, policyCall(s, token, "POST", path, review))
	if result.Coverage.Document.Reviews[0].Check != nil {
		t.Fatal("request changes claimed check")
	}
}
func TestPolicyCoverageRuntimeDoesNotLockAndRechecksAuthorization(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		t.Run(map[bool]string{false: "candidate-race", true: "revoked-session"}[revoke], func(t *testing.T) {
			s, token, p, pack := coverageFixture(t)
			service, actor := coverageServiceFixture(s, token)
			saved, err := service.coverageCommand(context.Background(), p.Document.ID, actor, coverageSaveBody(t, p, pack))
			if err != nil {
				t.Fatal(err)
			}
			entered, release := make(chan struct{}), make(chan struct{})
			service.checkCoverage = func(ctx context.Context, m policyCoverageMaterial) policyCoverageCheckReply {
				close(entered)
				<-release
				return coverageAcceptedFixture(ctx, m)
			}
			result := make(chan error, 1)
			go func() {
				_, err := service.coverageCommand(context.Background(), p.Document.ID, actor, coverageReviewBody(t, *saved.Coverage))
				result <- err
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("check not entered")
			}
			if revoke {
				s.sessions.revoke(token)
			} else {
				update := coverageSaveBody(t, p, pack)
				update.CoverageID = saved.Coverage.Document.ID
				update.BaseRevision = saved.Coverage.Revision
				update.Statement = &policyCoverageStatement{Status: "out-of-scope", Rationale: "Explicitly reviewed limited implementation scope."}
				if _, err := service.coverageCommand(context.Background(), p.Document.ID, actor, update); err != nil {
					t.Fatal("Runtime held storage lock", err)
				}
			}
			close(release)
			err = <-result
			var failure *policyFailure
			if !errors.As(err, &failure) || revoke && failure.Status != 401 || !revoke && failure.Code != "revision-conflict" {
				t.Fatal(err)
			}
		})
	}
}
func TestPolicyCoverageArchiveAndRestoreRetainsBytesButFencesReviews(t *testing.T) {
	s, token, p, pack := coverageFixture(t)
	service, actor := coverageServiceFixture(s, token)
	service.checkCoverage = coverageAcceptedFixture
	saved, err := service.coverageCommand(context.Background(), p.Document.ID, actor, coverageSaveBody(t, p, pack))
	if err != nil {
		t.Fatal(err)
	}
	approved, err := service.coverageCommand(context.Background(), p.Document.ID, actor, coverageReviewBody(t, *saved.Coverage))
	if err != nil {
		t.Fatal(err)
	}
	orphan, err := s.artifactRepository().Put(context.Background(), []byte("Unrelated retained artifact must remain outside the coverage archive."))
	if err != nil {
		t.Fatal(err)
	}
	cut := archiveTestCut(t, s)
	if cut.Counts.Coverages != 1 || cut.Counts.CoverageCandidates != 1 || cut.Counts.CoverageReviews != 1 {
		t.Fatal(cut.Counts)
	}
	found := false
	for _, ref := range cut.Refs {
		if ref == orphan {
			t.Fatal("archive included unrelated retained artifact")
		}
		if ref == pack.Content {
			found = true
		}
	}
	if !found {
		t.Fatal("Pack bytes missing from closed archive")
	}
	if !s.assistant.usable() {
		t.Fatal("fixture custody", s.assistant.problem)
	}
	archive := recoveryArchiveBytes(t, s, token)
	destination := filepath.Join(t.TempDir(), "restored")
	recoveryRestore(t, s, token, archive, recoveryBody(t, recoveryInspect(t, s, token, archive, destination)))
	fresh, newToken := recoveryOpen(t, s, destination)
	history := coverageReplyOf(t, fresh, newToken, p.Document.ID, approved.Coverage.Document.ID)
	if history.Recovery == nil || !history.Recovery.FreshReviewRequired || history.Document.ReviewState != "unreviewed" || history.Availability.PackPublication != "missing" || len(history.Document.Reviews) != 1 || history.Document.Candidate.Source.Narrative != p.Document.Candidate.Narrative {
		t.Fatal(history)
	}
	original, err := fresh.artifactRepository().Get(context.Background(), pack.Content)
	if err != nil || !bytes.Equal(original, []byte(reviewPack)) {
		t.Fatal("restored exact Pack unavailable", err)
	}
	freshService, newActor := coverageServiceFixture(fresh, newToken)
	freshService.checkCoverage = coverageAcceptedFixture
	_, err = freshService.coverageCommand(context.Background(), p.Document.ID, newActor, coverageReviewBody(t, history))
	var failure *policyFailure
	if !errors.As(err, &failure) || failure.Code != "publication-unavailable" {
		t.Fatal(err)
	}
	recovered := policyMutation(t, policyCall(fresh, newToken, "GET", "/api/policy-operations/"+approved.Receipt.OperationID, nil))
	if recovered.Delivery != "recovered" || recovered.Coverage.Document.ReviewState != "reviewed" {
		t.Fatal("historical receipt lost", recovered)
	}
	// Subsequent archive includes historical receipts unchanged and another restore barrier.
	_ = recoveryArchiveBytes(t, fresh, newToken)
	root, err := fresh.openRetainedArtifacts(false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.WriteFile(pack.Content.SHA256, []byte("corrupt retained Pack"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := policyArchiveClosure(context.Background(), cut.Ledger, fresh.artifactRepository()); err == nil {
		t.Fatal("archive accepted corrupt selected Pack bytes")
	}
}

func TestPolicyCoveragePrecommitAndUncertainDeliveryRecovery(t *testing.T) {
	s, token, p, pack := coverageFixture(t)
	path := "/api/policies/" + p.Document.ID + "/coverage"
	saved := policyMutation(t, policyCall(s, token, "POST", path, coverageSaveBody(t, p, pack)))
	for _, failAt := range []int{1, 2, 3} {
		base := s.artifactRepository()
		s.retainedArtifacts = &policyFailingArtifacts{base: base, failAt: failAt}
		q := coverageSaveBody(t, p, pack)
		q.CoverageID = saved.Coverage.Document.ID
		q.BaseRevision = saved.Coverage.Revision
		q.Statement = &policyCoverageStatement{Status: "manual", Handoff: "A retained manual responsibility and process."}
		failure := policyError(t, policyCall(s, token, "POST", path, q), 503, "storage-unavailable")
		if failure["outcome"] != "not-committed" {
			t.Fatal("precommit retention uncertainty", failure)
		}
		s.retainedArtifacts = base
		current := coverageReplyOf(t, s, token, p.Document.ID, q.CoverageID)
		if current.Revision != saved.Coverage.Revision {
			t.Fatal("failed retention exposed candidate")
		}
	}
	q := coverageSaveBody(t, p, pack)
	q.CoverageID = saved.Coverage.Document.ID
	q.BaseRevision = saved.Coverage.Revision
	q.Statement = &policyCoverageStatement{Status: "out-of-scope", Rationale: "Explicit reviewed scope boundary."}
	reads := 0
	testHookPrivateRead = func(name string) error {
		if name == "ledger.json" {
			reads++
			if reads == 4 {
				return errors.New("controlled ledger read-back failure")
			}
		}
		return nil
	}
	t.Cleanup(func() { testHookPrivateRead = nil })
	failure := policyError(t, policyCall(s, token, "POST", path, q), 503, "storage-unavailable")
	if failure["outcome"] != "unknown" {
		t.Fatal("uncertain ledger commit reported not committed", failure)
	}
	testHookPrivateRead = nil
	recovered := policyMutation(t, policyCall(s, token, "GET", "/api/policy-operations/"+q.OperationID, nil))
	if recovered.Delivery != "recovered" || recovered.Coverage.Document.Candidate.Statement.Status != "out-of-scope" {
		t.Fatal(recovered)
	}
	replayed := policyMutation(t, policyCall(s, token, "POST", path, q))
	if replayed.Delivery != "recovered" || replayed.Coverage.Revision != recovered.Coverage.Revision || len(replayed.Coverage.Document.Candidates) != 2 {
		t.Fatal("uncertain operation replayed", replayed)
	}
	s.sessions.revoke(token)
	policyError(t, policyCall(s, token, "GET", "/api/policy-operations/"+q.OperationID, nil), 401, CodeUnauthorized)
}

func TestPolicyCoverageConcurrentSaveAndWithdrawnHistoricalReview(t *testing.T) {
	for _, sameOperation := range []bool{false, true} {
		t.Run(map[bool]string{false: "distinct-operation", true: "same-operation"}[sameOperation], func(t *testing.T) {
			s, token, p, pack := coverageFixture(t)
			service, actor := coverageServiceFixture(s, token)
			saved, err := service.coverageCommand(context.Background(), p.Document.ID, actor, coverageSaveBody(t, p, pack))
			if err != nil {
				t.Fatal(err)
			}
			q := coverageSaveBody(t, p, pack)
			q.CoverageID = saved.Coverage.Document.ID
			q.BaseRevision = saved.Coverage.Revision
			q.Statement = &policyCoverageStatement{Status: "manual", Handoff: "Responsible process reviewed explicitly."}
			other := q
			if !sameOperation {
				other.OperationID = policyOp(t)
			}
			type outcome struct {
				reply policyMutationReply
				err   error
			}
			done := make(chan outcome, 2)
			for _, request := range []policyCoverageRequest{q, other} {
				go func(request policyCoverageRequest) {
					r, err := service.coverageCommand(context.Background(), p.Document.ID, actor, request)
					done <- outcome{r, err}
				}(request)
			}
			committed, recovered, conflicts := 0, 0, 0
			for range 2 {
				result := <-done
				var failure *policyFailure
				if result.err == nil {
					if result.reply.Delivery == "committed" {
						committed++
					} else {
						recovered++
					}
				} else if errors.As(result.err, &failure) && failure.Code == "revision-conflict" {
					conflicts++
				} else {
					t.Fatal(result.err)
				}
			}
			if committed != 1 || sameOperation && recovered != 1 || !sameOperation && conflicts != 1 {
				t.Fatal(committed, recovered, conflicts)
			}
			current := coverageReplyOf(t, s, token, p.Document.ID, q.CoverageID)
			if len(current.Document.Candidates) != 2 {
				t.Fatal("concurrent save duplicated candidate")
			}
			// Lifecycle history is separate from this local provenance review.
			service.checkCoverage = coverageAcceptedFixture
			withdraw := lifecycleBody(t, service, p, "withdraw")
			withdraw.Rationale = "Withdrawn synthetic publication remains inspectable historical provenance."
			if _, err := service.lifecycleCommand(context.Background(), p.Document.ID, actor, withdraw); err != nil {
				t.Fatal(err)
			}
			approved, err := service.coverageCommand(context.Background(), p.Document.ID, actor, coverageReviewBody(t, current))
			if err != nil || approved.Coverage.Document.ReviewState != "reviewed" {
				t.Fatal("historical coverage treated as execution admission", err)
			}
		})
	}
}

func TestPolicyCoverageSharedMaterialArchiveAndRestore(t *testing.T) {
	for _, realRuntime := range []bool{false, true} {
		name := "controlled"
		if realRuntime {
			name = "real-runtime"
		}
		t.Run(name, func(t *testing.T) {
			bin := os.Getenv("JPACK_POLICY_TEST_BIN")
			if realRuntime && bin == "" {
				t.Skip("set JPACK_POLICY_TEST_BIN to exercise shared material with experimental Policy prototype 0.0.2 tools")
			}
			for _, status := range []string{"manual", "implemented"} {
				t.Run(status, func(t *testing.T) {
					s, token := policyTestServer(t)
					edit := policyEditRequest{OperationID: policyOp(t), Title: "Exact Pack bytes as published narrative", Narrative: reviewPack, ArtifactVersion: "1.0.0"}
					if status == "implemented" {
						edit.Import = &policyImport{Name: "original.txt", MediaType: "text/plain", Data: []byte(reviewPack)}
					}
					p := policyMutation(t, policyCall(s, token, "POST", "/api/policies", edit)).Policy
					p = policyApprove(t, s, token, p)
					p = policyMutation(t, policyCall(s, token, "POST", "/api/policies/"+p.Document.ID+"/publish", policyPublishBody(t, p))).Policy
					pack := coveragePublishFixture(t, s, []byte(reviewPack))
					if realRuntime {
						s.cfg.JpackBin = bin
					}
					service, actor := coverageServiceFixture(s, token)
					if !realRuntime {
						service.checkCoverage = coverageAcceptedFixture
					}
					q := coverageSaveBody(t, p, pack)
					if status == "manual" {
						q.Statement = &policyCoverageStatement{Status: "manual", Handoff: "An administrator handles the exact whole published narrative."}
					} else {
						q.Statement.Basis = reviewPack
					}
					saved, err := service.coverageCommand(context.Background(), p.Document.ID, actor, q)
					if err != nil {
						t.Fatal(err)
					}
					approved, err := service.coverageCommand(context.Background(), p.Document.ID, actor, coverageReviewBody(t, *saved.Coverage))
					if err != nil {
						t.Fatal(err)
					}
					if approved.Coverage.Document.ReviewState != "reviewed" {
						t.Fatal("shared-material review was not committed")
					}
					checkRef := approved.Coverage.Document.Reviews[0].Check
					raw, err := s.artifactRepository().Get(context.Background(), *checkRef)
					if err != nil {
						t.Fatal(err)
					}
					var checked policyCoverageRetainedCheck
					if decodeDataJSON(raw, &checked) != nil || len(checked.Materials) != 2 {
						t.Fatalf("expected two unique materials for shared roles: %s", raw)
					}
					cut := archiveTestCut(t, s)
					if cut.Counts.Coverages != 1 || cut.Counts.CoverageReviews != 1 {
						t.Fatal(cut.Counts)
					}
					archive := recoveryArchiveBytes(t, s, token)
					destination := filepath.Join(t.TempDir(), "restored-shared-material")
					recoveryRestore(t, s, token, archive, recoveryBody(t, recoveryInspect(t, s, token, archive, destination)))
					fresh, newToken := recoveryOpen(t, s, destination)
					restored := coverageReplyOf(t, fresh, newToken, p.Document.ID, approved.Coverage.Document.ID)
					if restored.Document.Candidate.Source.Narrative != reviewPack || restored.Document.Candidate.Pack.Document.Content != pack.Content || restored.Recovery == nil || !restored.Recovery.FreshReviewRequired || restored.Document.ReviewState != "unreviewed" || len(restored.Document.Reviews) != 1 || restored.Availability.PackPublication != "missing" {
						t.Fatal("shared bytes or historical review boundary changed", restored)
					}
					retained, err := fresh.artifactRepository().Get(context.Background(), pack.Content)
					if err != nil || !bytes.Equal(retained, []byte(reviewPack)) {
						t.Fatal("shared retained Pack/narrative missing", err)
					}
					// A second complete capture verifies restored receipts and shared roles too.
					_ = recoveryArchiveBytes(t, fresh, newToken)
					// Removing the shared blob from the check manifest must still refuse even
					// though the same bytes are retained elsewhere in the project.
					bad := checked
					bad.Materials = nil
					for _, ref := range checked.Materials {
						if ref != pack.Content {
							bad.Materials = append(bad.Materials, ref)
						}
					}
					encoded, _ := json.Marshal(bad)
					badRef, err := s.artifactRepository().Put(context.Background(), encoded)
					if err != nil {
						t.Fatal(err)
					}
					review := approved.Coverage.Document.Reviews[0]
					review.Check = &badRef
					var material policyCoverageMaterial
					err = service.repository.View(context.Background(), func(l policyLedger) error {
						var err error
						material, err = service.coverageMaterial(context.Background(), l.Policies[p.Document.ID], coverageHead(l.Coverages[approved.Coverage.Document.ID]))
						return err
					})
					if err != nil {
						t.Fatal(err)
					}
					if _, err := archivedCoverageCheck(context.Background(), s.artifactRepository(), review, material); err == nil {
						t.Fatal("archive accepted a missing required shared material")
					}
				})
			}
		})
	}
}
