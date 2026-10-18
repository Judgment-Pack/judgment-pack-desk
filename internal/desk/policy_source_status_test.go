package desk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func authorityFixture(t *testing.T) *policySourceFixture {
	t.Helper()
	f := newPolicySourceFixture(t)
	f.bind(t)
	f.poll(t)
	return f
}
func (f *policySourceFixture) status(t *testing.T) policySourceStatusReply {
	t.Helper()
	w := policyCall(f.s, f.token, "GET", "/api/policies/"+f.policy.Document.ID+"/source-status", nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var r policySourceStatusReply
	if json.Unmarshal(w.Body.Bytes(), &r) != nil {
		t.Fatal("invalid status")
	}
	return r
}
func (f *policySourceFixture) statusBody(t *testing.T, action string) policySourceStatusRequest {
	t.Helper()
	v := f.status(t)
	return policySourceStatusRequest{OperationID: policyOp(t), BaseRevision: v.PolicyRevision, BaseSourceRevision: v.SourceRevision, BaseAuthorityRevision: v.Revision, Action: action, Rationale: "Explicitly reviewed administrator decision.", BindingID: func() string {
		if action == "configure" && v.Binding != nil {
			return v.Binding.BindingID
		}
		return ""
	}(), ObservationID: func() string {
		if action == "apply-external" || action == "import-draft" {
			return v.CurrentPreview.ObservationID
		}
		return ""
	}(), ConfigurationID: func() string {
		if action == "apply-external" || action == "import-draft" {
			return v.CurrentPreview.ConfigurationID
		}
		return ""
	}()}
}
func (f *policySourceFixture) statusDo(t *testing.T, q policySourceStatusRequest) policyMutationReply {
	t.Helper()
	return policyMutation(t, policyCall(f.s, f.token, "POST", "/api/policies/"+f.policy.Document.ID+"/source-status", q))
}
func (f *policySourceFixture) configureStatus(t *testing.T, status string) policyMutationReply {
	q := f.statusBody(t, "configure")
	q.FallbackStatus = status
	return f.statusDo(t, q)
}
func (f *policySourceFixture) ownStatus(t *testing.T, mode string) policyMutationReply {
	q := f.statusBody(t, "set-ownership")
	q.ManagementMode = mode
	return f.statusDo(t, q)
}
func statusService(f *policySourceFixture) policyService {
	return policyService{repository: filePolicyRepository{open: f.s.openPolicies}, artifacts: f.s.artifactRepository(), authorize: func() bool { return true }, validateVersion: func(context.Context, string, string) error { return nil }}
}

func TestPolicySourceStatusDraftRequiresExplicitImportPreservesWorkingDraft(t *testing.T) {
	f := authorityFixture(t)
	f.policy = policyApprove(t, f.s, f.token, f.policy)
	before := f.policy
	view := f.status(t)
	if !view.Ownership.Inherited || view.Ownership.Mode != "desk-managed" || view.CurrentPreview.Status != "" || view.LastApplied != nil {
		t.Fatal("invented initial authority", view)
	}
	configured := f.configureStatus(t, "draft")
	if configured.SourceStatus.CurrentPreview.Status != "draft" || configured.SourceStatus.CurrentPreview.Provenance != "admin-configured" {
		t.Fatal(configured)
	}
	unchanged := policyRead(t, f.s, f.token, f.policy.Document.ID)
	if !samePolicyStatusValue(before, unchanged) {
		// The optional sourceAuthority projection is new; all retained Policy content remains exact.
		if !samePolicyStatusValue(before.Document, unchanged.Document) || before.Revision != unchanged.Revision {
			t.Fatal("configuration overwrote saved draft", unchanged)
		}
	}
	f.ownStatus(t, "externally-managed")
	q := f.statusBody(t, "apply-external")
	policyError(t, policyCall(f.s, f.token, "POST", "/api/policies/"+f.policy.Document.ID+"/source-status", q), 409, "import-draft-required")
	q = f.statusBody(t, "import-draft")
	q.Draft = &policySourceStatusDraft{Title: before.Document.Candidate.Title, Narrative: before.Document.Candidate.Narrative, ArtifactVersion: before.Document.Candidate.ArtifactVersion}
	imported := f.statusDo(t, q)
	current := policyRead(t, f.s, f.token, f.policy.Document.ID)
	if current.Document.ManagementMode != "desk-managed" || current.Document.ReviewState != "unreviewed" || current.Document.CurrentReviewID != "" || current.Document.Candidate.RevisionID == before.Document.Candidate.RevisionID || current.Document.Candidate.Digest != before.Document.Candidate.Digest || len(current.Document.Revisions) != len(before.Document.Revisions)+1 || !samePolicyStatusValue(current.Document.Reviews, before.Document.Reviews) {
		t.Fatal("import transferred approval or changed identical content", current)
	}
	if imported.SourceStatus.Events[len(imported.SourceStatus.Events)-1].ImportedDraft.Observation.ID != q.ObservationID || imported.Receipt.CandidateDigest != current.Document.Candidate.Digest || f.sync(t).Pending {
		t.Fatal("import lost exact provenance/resolution", imported)
	}
}

func TestPolicySourceStatusPublishedExternalVersionHistoryAndNoFabricatedApproval(t *testing.T) {
	f := authorityFixture(t)
	original := f.policy
	f.configureStatus(t, "published")
	f.ownStatus(t, "externally-managed")
	q := f.statusBody(t, "apply-external")
	applied := f.statusDo(t, q)
	v := applied.SourceStatus.LastApplied
	if v == nil || v.Status != "published" || v.Provenance != "admin-configured" || v.RawStatus != "" || v.Evidence != nil || v.Observation.ID != q.ObservationID || len(applied.SourceStatus.ExternalVersions) != 1 {
		t.Fatal(applied)
	}
	current := policyRead(t, f.s, f.token, f.policy.Document.ID)
	if len(current.Document.Reviews) != 0 || len(current.Document.Publications) != 0 || !samePolicyStatusValue(current.Document.Candidate, original.Document.Candidate) {
		t.Fatal("external fallback invented local approval", current)
	}
	replay := f.statusDo(t, q)
	if replay.Delivery != "recovered" || !samePolicyStatusValue(replay.Receipt, applied.Receipt) {
		t.Fatal("duplicate changed outcome", replay)
	}
	duplicate := f.statusBody(t, "apply-external")
	policyError(t, policyCall(f.s, f.token, "POST", "/api/policies/"+f.policy.Document.ID+"/source-status", duplicate), 409, "already-applied")
	f.configureStatus(t, "draft")
	after := f.status(t)
	if after.CurrentPreview.Status != "draft" || !samePolicyStatusValue(after.LastApplied, v) || after.LastApplied.ConfigurationID == after.CurrentConfig.ID {
		t.Fatal("new config reinterpreted history", after)
	}
	w := policyCall(f.s, f.token, "GET", "/api/policies/"+f.policy.Document.ID+"/source-status/versions/"+v.ID, nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var historical policyExternalVersionReply
	_ = json.Unmarshal(w.Body.Bytes(), &historical)
	if !samePolicyStatusValue(historical.Version, *v) || historical.Configuration.FallbackStatus != "published" || digestOf(historical.Document) != v.Observation.Document.SHA256 {
		t.Fatal("historical bytes/basis changed", historical)
	}
	// A Published source is also explicitly importable as a new, unreviewed Desk draft.
	f.configureStatus(t, "published")
	importQ := f.statusBody(t, "import-draft")
	importQ.Draft = &policySourceStatusDraft{Title: "Reviewed imported source", Narrative: "Administrator-selected draft text", ArtifactVersion: "1.1.0"}
	f.statusDo(t, importQ)
	current = policyRead(t, f.s, f.token, f.policy.Document.ID)
	if current.Document.ManagementMode != "desk-managed" || current.Document.ReviewState != "unreviewed" || len(current.Document.Publications) != 0 {
		t.Fatal("Published source forced local publication", current)
	}
}

func TestPolicySourceStatusSourceFailuresNeverApplyFallbackOrEraseLastReviewed(t *testing.T) {
	f := authorityFixture(t)
	f.configureStatus(t, "published")
	f.ownStatus(t, "externally-managed")
	f.statusDo(t, f.statusBody(t, "apply-external"))
	baseline := f.status(t)
	for _, state := range []string{"paused", "revoked", "authorization-ended", "gap", "error", "binding", "no-observation"} {
		t.Run(state, func(t *testing.T) {
			repo := filePolicyRepository{open: f.s.openPolicies}
			var original policyConnectedSource
			err := repo.Update(context.Background(), func(l *policyLedger) (bool, error) {
				raw, _ := json.Marshal(l.Sources[f.policy.Document.ID])
				_ = json.Unmarshal(raw, &original)
				s := l.Sources[f.policy.Document.ID]
				switch state {
				case "gap":
					s.Gap = true
				case "error":
					s.ErrorCode = "source-unavailable"
				case "no-observation":
					s.Observations = []policySourceObservation{}
				case "binding":
					s.State = "binding"
				default:
					s.State = state
				}
				return true, nil
			})
			// Applied evidence remains retained, so removing its observation must fail integrity.
			if state == "no-observation" {
				if err == nil {
					t.Fatal("removed applied observation")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			view := f.status(t)
			if view.CurrentPreview.Eligible || view.CurrentPreview.Status != "" || !samePolicyStatusValue(view.LastApplied, baseline.LastApplied) {
				t.Fatal("failure activated fallback or erased history", view)
			}
			q := f.statusBody(t, "apply-external")
			q.ObservationID, q.ConfigurationID = baseline.CurrentPreview.ObservationID, baseline.CurrentPreview.ConfigurationID
			policyError(t, policyCall(f.s, f.token, "POST", "/api/policies/"+f.policy.Document.ID+"/source-status", q), 409, "source-status-unavailable")
			if err := repo.Update(context.Background(), func(l *policyLedger) (bool, error) { l.Sources[f.policy.Document.ID] = &original; return true, nil }); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPolicySourceStatusOwnershipRoundtripMasksOldReviewsAndFencesEveryLiveCAS(t *testing.T) {
	f := newPolicySourceFixture(t)
	f.policy = policyApprove(t, f.s, f.token, f.policy)
	f.policy = policyMutation(t, policyCall(f.s, f.token, "POST", "/api/policies/"+f.policy.Document.ID+"/publish", policyPublishBody(t, f.policy))).Policy
	f.bind(t)
	f.poll(t)
	f.keep(t)
	before := f.policy
	sourceQ := policySourceSyncRequest{OperationID: policyOp(t), Action: "check-now", BaseRevision: before.Revision, BaseSourceRevision: f.sync(t).Revision}
	lifecycle, err := statusService(f).lifecycle(context.Background(), f.policy.Document.ID)
	if err != nil {
		t.Fatal(err)
	}
	activate := policyLifecycleRequest{OperationID: policyOp(t), Action: "activate", BaseRevision: before.Revision, BaseLifecycleRevision: lifecycle.Revision, PublicationID: before.Document.Publications[0].ID}
	f.ownStatus(t, "externally-managed")
	external := policyRead(t, f.s, f.token, f.policy.Document.ID)
	if external.Document.ManagementMode != "externally-managed" || external.Document.ReviewState != "unreviewed" || external.Document.CurrentReviewID != "" {
		t.Fatal(external)
	}
	policyError(t, policyCall(f.s, f.token, "POST", "/api/policies/"+f.policy.Document.ID+"/review", policyReviewBody(t, external, "approve")), 409, "external-ownership")
	f.ownStatus(t, "desk-managed")
	after := policyRead(t, f.s, f.token, f.policy.Document.ID)
	if after.Revision == before.Revision || after.Document.ReviewState != "unreviewed" || after.Document.CurrentReviewID != "" || !samePolicyStatusValue(after.Document.Candidate, before.Document.Candidate) || !samePolicyStatusValue(after.Document.Reviews, before.Document.Reviews) || !samePolicyStatusValue(after.Document.Publications, before.Document.Publications) {
		t.Fatal("roundtrip revived review or rewrote history", after)
	}
	policyError(t, policyCall(f.s, f.token, "PUT", "/api/policies/"+f.policy.Document.ID, policyEditBody(t, before)), 409, "revision-conflict")
	policyError(t, policyCall(f.s, f.token, "POST", "/api/policies/"+f.policy.Document.ID+"/source-sync", sourceQ), 409, "source-conflict")
	policyError(t, policyCall(f.s, f.token, "POST", "/api/policies/"+f.policy.Document.ID+"/lifecycle", activate), 409, "lifecycle-conflict")
	activate.BaseRevision = after.Revision
	policyError(t, policyCall(f.s, f.token, "POST", "/api/policies/"+f.policy.Document.ID+"/lifecycle", activate), 409, "ownership-review-required")
	policyError(t, policyCall(f.s, f.token, "POST", "/api/policies/"+f.policy.Document.ID+"/publish", policyPublishBody(t, after)), 409, "review-incomplete")
	lifecycle, _ = statusService(f).lifecycle(context.Background(), f.policy.Document.ID)
	if lifecycle.PolicyRevision != after.Revision {
		t.Fatal("lifecycle exposed raw record revision")
	}
	list, err := statusService(f).list(context.Background())
	if err != nil || list[0].CurrentCandidatePublished || list[0].PublicationCount != 1 || list[0].ReviewState != "unreviewed" {
		t.Fatal(list, err)
	}
	f.ownStatus(t, "externally-managed")
	external = policyRead(t, f.s, f.token, f.policy.Document.ID)
	withdraw := activate
	withdraw.OperationID = policyOp(t)
	withdraw.Action = "withdraw"
	withdraw.BaseRevision = external.Revision
	withdraw.Rationale = "Restrict the historical local release."
	result := policyMutation(t, policyCall(f.s, f.token, "POST", "/api/policies/"+f.policy.Document.ID+"/lifecycle", withdraw))
	if result.Lifecycle.Releases[0].Status != "withdrawn" || result.Receipt.Revision != external.Revision {
		t.Fatal("historical withdrawal denied", result)
	}
}

func TestPolicySourceStatusExplicitInheritedDeskChoicePreservesEligibleReview(t *testing.T) {
	f := newPolicySourceFixture(t)
	f.policy = policyApprove(t, f.s, f.token, f.policy)
	before := f.policy
	f.ownStatus(t, "desk-managed")
	after := policyRead(t, f.s, f.token, f.policy.Document.ID)
	if after.Revision == before.Revision || after.Document.ReviewState != "approved" || after.Document.CurrentReviewID != before.Document.CurrentReviewID || after.SourceAuthority.Ownership.Inherited {
		t.Fatal(after)
	}
}

func TestPolicySourceStatusImportReplaySkipsUnavailableRuntime(t *testing.T) {
	f := authorityFixture(t)
	f.configureStatus(t, "draft")
	service := statusService(f)
	calls := 0
	service.validateVersion = func(context.Context, string, string) error { calls++; return nil }
	authority, _ := f.s.policySourceAuthorityBasis()
	q := f.statusBody(t, "import-draft")
	q.Draft = &policySourceStatusDraft{Title: "Explicit new draft", Narrative: "Retained source used as reviewed starting material", ArtifactVersion: "1.0.0"}
	actor := f.policy.Document.Revisions[0].Actor
	first, err := service.sourceStatusCommand(context.Background(), f.policy.Document.ID, actor, q, authority)
	if err != nil {
		t.Fatal(err)
	}
	service.validateVersion = func(context.Context, string, string) error { calls++; return errors.New("Runtime now unavailable") }
	again, err := service.sourceStatusCommand(context.Background(), f.policy.Document.ID, actor, q, authority)
	if err != nil || again.Delivery != "recovered" || calls != 1 || !samePolicyStatusValue(first.Receipt, again.Receipt) {
		t.Fatal("replay called Runtime or mutated", again, err, calls)
	}
}

func TestPolicySourceStatusCurrentAuthorityFenceRejectsBeforeWorkerObservesRotation(t *testing.T) {
	for _, transition := range []string{"rotate", "disconnect"} {
		t.Run(transition, func(t *testing.T) {
			f := authorityFixture(t)
			f.configureStatus(t, "published")
			f.ownStatus(t, "externally-managed")
			q := f.statusBody(t, "apply-external")
			path := "/api/policies/" + f.policy.Document.ID + "/source-status"
			f.s.signIn.policySourceMu.Lock()
			started := make(chan struct{})
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() { close(started); done <- policyCall(f.s, f.token, "POST", path, q) }()
			<-started
			if err := f.s.assistant.rotatePolicySourceAuthority(); err != nil {
				t.Fatal(err)
			}
			if transition == "disconnect" {
				f.s.endPolicySourceConnection(context.Background())
			}
			f.s.signIn.policySourceMu.Unlock()
			w := <-done
			code := "source-status-unavailable"
			if transition == "disconnect" {
				code = "authority-conflict"
			}
			policyError(t, w, 409, code)
			view := f.status(t)
			if view.LastApplied != nil || view.CurrentPreview.Eligible || view.Availability.Usable {
				t.Fatal("stale active grant authorized fallback", view)
			}
			if transition == "rotate" && f.sync(t).State.State != "active" {
				t.Fatal("test relied on worker changing active state")
			}
		})
	}
}

func TestPolicySourceStatusCASRaceAndLostReplyRecovery(t *testing.T) {
	f := authorityFixture(t)
	service := statusService(f)
	authority, _ := f.s.policySourceAuthorityBasis()
	actor := f.policy.Document.Revisions[0].Actor
	first := f.statusBody(t, "set-ownership")
	first.ManagementMode = "externally-managed"
	second := first
	second.OperationID = policyOp(t)
	second.ManagementMode = "desk-managed"
	start := make(chan struct{})
	results := make(chan error, 2)
	var group sync.WaitGroup
	for _, q := range []policySourceStatusRequest{first, second} {
		group.Add(1)
		go func(q policySourceStatusRequest) {
			defer group.Done()
			<-start
			_, err := service.sourceStatusCommand(context.Background(), f.policy.Document.ID, actor, q, authority)
			results <- err
		}(q)
	}
	close(start)
	group.Wait()
	close(results)
	committed := 0
	for err := range results {
		if err == nil {
			committed++
		} else {
			lifecycleRefused(t, err, "authority-conflict")
		}
	}
	if committed != 1 || len(f.status(t).Events) != 1 {
		t.Fatal("ownership CAS did not serialize", committed)
	}
	q := f.statusBody(t, "configure")
	q.FallbackStatus = "published"
	reads := 0
	testHookPrivateRead = func(name string) error {
		if name == "ledger.json" {
			reads++
			if reads == 3 {
				return errors.New("lost committed read-back")
			}
		}
		return nil
	}
	t.Cleanup(func() { testHookPrivateRead = nil })
	failure := policyError(t, policyCall(f.s, f.token, "POST", "/api/policies/"+f.policy.Document.ID+"/source-status", q), 503, "storage-unavailable")
	testHookPrivateRead = nil
	if failure["outcome"] != "unknown" {
		t.Fatal(failure)
	}
	recovered := policyMutation(t, policyCall(f.s, f.token, "GET", "/api/policy-operations/"+q.OperationID, nil))
	again := f.statusDo(t, q)
	if recovered.Delivery != "recovered" || again.Delivery != "recovered" || !samePolicyStatusValue(recovered.Receipt, again.Receipt) || len(f.status(t).Events) != 2 {
		t.Fatal("lost reply repeated event", again)
	}
}

func TestPolicySourceStatusRetentionIntegrityClosedShapeAndRestart(t *testing.T) {
	f := authorityFixture(t)
	q := f.statusBody(t, "configure")
	q.FallbackStatus = "published"
	f.s.retainedArtifacts = &policyFailingArtifacts{base: f.s.artifactRepository(), failAt: 1}
	policyError(t, policyCall(f.s, f.token, "POST", "/api/policies/"+f.policy.Document.ID+"/source-status", q), 503, "storage-unavailable")
	if len(f.status(t).Events) != 0 {
		t.Fatal("failed retention exposed authority")
	}
	f.s.retainedArtifacts = nil
	first := f.statusDo(t, q)
	f.ownStatus(t, "externally-managed")
	applied := f.statusDo(t, f.statusBody(t, "apply-external"))
	if !validPolicySourceAuthorityResult(applied) {
		t.Fatal("valid outcome rejected")
	}
	var raw []byte
	_ = statusService(f).repository.View(context.Background(), func(l policyLedger) error { raw, _ = json.Marshal(l); return nil })
	for _, change := range []func(*policySourceAuthorityStream){func(s *policySourceAuthorityStream) { s.Events[0].Configuration.FallbackStatus = "effective" }, func(s *policySourceAuthorityStream) { s.Events[2].ExternalVersion.Provenance = "source-provided" }, func(s *policySourceAuthorityStream) {
		s.Events[2].ExternalVersion.Observation.SourceRevision = "forged"
	}, func(s *policySourceAuthorityStream) { s.Events[1].PolicySequence = 999999 }} {
		var ledger policyLedger
		_ = json.Unmarshal(raw, &ledger)
		change(ledger.SourceAuthorities[f.policy.Document.ID])
		if validatePolicyLedger(ledger) == nil {
			t.Fatal("corrupt authority accepted")
		}
	}
	for _, change := range []func(*policyMutationReply){func(r *policyMutationReply) { r.Receipt.AuthorityEventID = "authority_" + strings.Repeat("a", 48) }, func(r *policyMutationReply) { r.Receipt.ConfigurationID = "wrong" }, func(r *policyMutationReply) { r.Receipt.ObservationID = "wrong" }, func(r *policyMutationReply) { r.SourceStatus.LastApplied.Provenance = "source-provided" }, func(r *policyMutationReply) { r.OutcomeKind = "lifecycle" }} {
		encoded, _ := json.Marshal(applied)
		var r policyMutationReply
		_ = json.Unmarshal(encoded, &r)
		change(&r)
		if validPolicyMutationIdentity(r) && validPolicySourceAuthorityResult(r) {
			t.Fatal("corrupt outcome accepted")
		}
	}
	path := "/api/policies/" + f.policy.Document.ID + "/source-status"
	for _, field := range []map[string]any{{"mapping": map[string]string{"approved": "published"}}, {"fallbackStatus": nil}, {"fallbackStatus": ""}, {"actor": map[string]string{"kind": "local", "subject": "forged"}}} {
		body := map[string]any{}
		encoded, _ := json.Marshal(f.statusBody(t, "configure"))
		_ = json.Unmarshal(encoded, &body)
		for k, v := range field {
			body[k] = v
		}
		policyError(t, policyCall(f.s, f.token, "POST", path, body), 400, CodeBadRequest)
	}
	before := f.status(t)
	config := f.s.cfg
	if err := f.s.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = again.Close() })
	again.policySourceCancel()
	<-again.policySourceDone
	again.policySourceCancel = nil
	f.s = again
	f.token = policyTestSession(t, again)
	after := f.status(t)
	if !samePolicyStatusValue(before.Events, after.Events) || !samePolicyStatusValue(before.LastApplied, after.LastApplied) {
		t.Fatal("restart changed retained authority")
	}
	recovered := policyMutation(t, policyCall(f.s, f.token, "GET", "/api/policy-operations/"+first.Receipt.OperationID, nil))
	if !samePolicyStatusValue(recovered.Receipt, first.Receipt) {
		t.Fatal("restart changed receipt")
	}
	// Cold status/history/recovery never needs an unrelated mutable draft blob.
	current := policyRead(t, f.s, f.token, f.policy.Document.ID)
	if err := os.Remove(filepath.Join(f.s.projectDir, retainedArtifactDirectory, current.Document.Candidate.Content.SHA256)); err != nil {
		t.Fatal(err)
	}
	if !f.status(t).Availability.Usable {
		t.Fatal("unrelated missing draft blocked authority view")
	}
}

func TestPolicySourceStatusEvaluatorProviderVocabularyAndSameBytesNewStatus(t *testing.T) {
	evidence := artifactReference([]byte("authenticated lifecycle fixture"))
	for _, fixture := range []struct {
		raw     string
		mapping map[string]string
	}{{"Accepted", map[string]string{"Accepted": "published", "Editing": "draft"}}, {"LIVE", map[string]string{"LIVE": "published", "WORK": "draft"}}} {
		status, basis, reason := evaluatePolicySourceStatus(policyStatusEvidence{Capability: "exposed", RawStatus: fixture.raw, Evidence: &evidence}, "draft", fixture.mapping)
		if status != "published" || basis != "source-provided" || reason != "" {
			t.Fatal(status, basis, reason)
		}
		status, basis, reason = evaluatePolicySourceStatus(policyStatusEvidence{Capability: "exposed", RawStatus: "UNKNOWN", Evidence: &evidence}, "published", fixture.mapping)
		if status != "" || basis != "" || reason != "unknown-source-status" {
			t.Fatal("unknown capable state used fallback")
		}
	}
	if status, _, reason := evaluatePolicySourceStatus(policyStatusEvidence{Capability: "exposed", RawStatus: "Accepted"}, "published", map[string]string{"Accepted": "published"}); status != "" || reason != "lifecycle-evidence-unavailable" {
		t.Fatal("missing evidence activated fallback")
	}
	if status, basis, _ := evaluatePolicySourceStatus(policyStatusEvidence{Capability: "not-exposed"}, "draft", nil); status != "draft" || basis != "admin-configured" {
		t.Fatal(status, basis)
	}
	// A capable adapter's identity projection must include lifecycle independently
	// of source bytes; current v1 Drive wire still refuses that unsupported shape.
	f := authorityFixture(t)
	var o policyGatewayObservation
	_ = json.Unmarshal(f.observation, &o)
	o.Lifecycle.Kind = "exposed"
	raw, _ := json.Marshal(o)
	if _, err := f.s.verifyPolicyObservation(context.Background(), raw, f.binding); err == nil {
		t.Fatal("production Drive verifier accepted future lifecycle fixture")
	}
}

func TestPolicySourceStatusNewObservationOrConfigRequiresNewReviewedDecision(t *testing.T) {
	f := authorityFixture(t)
	f.configureStatus(t, "published")
	f.ownStatus(t, "externally-managed")
	first := f.statusDo(t, f.statusBody(t, "apply-external"))
	stale := f.statusBody(t, "apply-external")
	f.observation = policyObservationFixture(t, f.s, f.binding, "Changed extraction with identical original bytes", "2026-10-18T06:40:15Z", 123)
	f.poll(t)
	pending := f.status(t)
	if !samePolicyStatusValue(first.SourceStatus.LastApplied, pending.LastApplied) || pending.CurrentPreview.ObservationID == pending.LastApplied.Observation.ID || pending.CurrentPreview.Content.BytesDigest != pending.LastApplied.Observation.Content.BytesDigest {
		t.Fatal("source update silently applied or lost same-byte identity", pending)
	}
	policyError(t, policyCall(f.s, f.token, "POST", "/api/policies/"+f.policy.Document.ID+"/source-status", stale), 409, "authority-conflict")
	f.statusDo(t, f.statusBody(t, "apply-external"))
	before := f.status(t)
	f.configureStatus(t, "published")
	configured := f.status(t)
	if configured.PolicyRevision != before.PolicyRevision || !samePolicyStatusValue(configured.LastApplied, before.LastApplied) {
		t.Fatal("configuration silently changed status/content", configured)
	}
	f.statusDo(t, f.statusBody(t, "apply-external"))
	if len(f.status(t).ExternalVersions) != 3 {
		t.Fatal("different reviewed configuration did not retain a new decision")
	}
}

func TestPolicySourceStatusHTTPGuardsAndAssistantOwnershipScope(t *testing.T) {
	f := newPolicySourceFixture(t)
	path := "/api/policies/" + f.policy.Document.ID + "/source-status"
	q := f.statusBody(t, "set-ownership")
	q.ManagementMode = "externally-managed"
	for _, method := range []string{"GET", "POST"} {
		policyError(t, policyCall(f.s, "", method, path, q), 401, CodeUnauthorized)
		policyError(t, policyCall(f.s, testToken, method, path, q), 401, CodeUnauthorized)
	}
	request := httptest.NewRequest("POST", path, strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer "+f.token)
	request.Header.Set("Origin", "https://foreign.example")
	response := httptest.NewRecorder()
	f.s.ServeHTTP(response, request)
	if response.Code != 403 {
		t.Fatal("foreign Origin admitted")
	}
	assistant := newPolicyAssistantFixture(t)
	grant := assistant.allow(t)
	source := &policySourceFixture{s: assistant.s, token: assistant.token, policy: assistant.policy}
	source.ownStatus(t, "externally-managed")
	source.ownStatus(t, "desk-managed")
	w := policyCall(assistant.s, assistant.token, "POST", "/api/assistant-approvals/chat-one/commit", assistant.body(grant))
	if w.Code != 409 {
		t.Fatal("old assistant proposal crossed ownership epoch", w.Code, w.Body.String())
	}
	body := assistant.body("")
	body["action"] = "policy-source-authority"
	w = policyCall(assistant.s, assistant.token, "POST", "/api/assistant-approvals/chat-one/grant", body)
	if w.Code != 409 {
		t.Fatal("unregistered authority mutation gained AI adapter", w.Code)
	}
	f.s.sessions.revoke(f.token)
	policyError(t, policyCall(f.s, f.token, "POST", path, q), 401, CodeUnauthorized)
}
