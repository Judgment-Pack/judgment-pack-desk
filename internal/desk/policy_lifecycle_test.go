package desk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func lifecycleFixture(t *testing.T) (*Server, string, policyReply, policyService) {
	t.Helper()
	s, token := policyTestServer(t)
	approved := policyApprove(t, s, token, policyCreate(t, s, token))
	published := policyMutation(t, policyCall(s, token, "POST", "/api/policies/"+approved.Document.ID+"/publish", policyPublishBody(t, approved))).Policy
	service := policyService{repository: filePolicyRepository{open: s.openPolicies}, history: s.policyHistoryReader(), artifacts: s.artifactRepository(), authorize: func() bool { return true }, clock: func() time.Time { return time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC) }}
	return s, token, published, service
}
func lifecycleBody(t *testing.T, service policyService, policy policyReply, action string) policyLifecycleRequest {
	t.Helper()
	view, err := service.lifecycle(context.Background(), policy.Document.ID)
	if err != nil {
		t.Fatal(err)
	}
	return policyLifecycleRequest{OperationID: policyOp(t), Action: action, BaseRevision: policy.Revision, BaseLifecycleRevision: view.Revision, PublicationID: policy.Document.Publications[len(policy.Document.Publications)-1].ID}
}
func lifecycleDo(t *testing.T, service policyService, policy policyReply, q policyLifecycleRequest) policyMutationReply {
	t.Helper()
	result, err := service.lifecycleCommand(context.Background(), policy.Document.ID, policy.Document.Publications[0].Actor, q)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func lifecycleRefused(t *testing.T, err error, code string) {
	t.Helper()
	var failure *policyFailure
	if !errors.As(err, &failure) || failure.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

func TestPolicyLifecycleSchedulingPreservesLegacyAndReceiptHistory(t *testing.T) {
	s, token, policy, service := lifecycleFixture(t)
	ctx, id := context.Background(), policy.Document.ID
	before, err := service.lifecycle(ctx, id)
	if err != nil || len(before.Events) != 0 || before.Releases[0].Status != "published" || before.Releases[0].EffectiveAt != "" {
		t.Fatal("fabricated legacy activation", before, err)
	}
	q := lifecycleBody(t, service, policy, "activate")
	q.EffectiveAt, q.ExpiresAt = "2030-01-01T13:00:00Z", "2030-01-01T14:00:00Z"
	activated := lifecycleDo(t, service, policy, q)
	if activated.Lifecycle.Releases[0].Status != "published" || activated.PolicyRevision != policy.Revision || activated.Lifecycle.Events[0].Provenance != "admin-configured" {
		t.Fatal(activated)
	}
	for _, check := range []struct{ at, status string }{{"2030-01-01T13:00:00Z", "effective"}, {"2030-01-01T14:00:00Z", "expired"}} {
		instant, _ := time.Parse(time.RFC3339, check.at)
		service.clock = func() time.Time { return instant }
		view, err := service.lifecycle(ctx, id)
		if err != nil || view.Releases[0].Status != check.status || view.Revision != activated.Lifecycle.Revision || len(view.Events) != 1 {
			t.Fatal("clock view mutated history", view, err)
		}
	}
	withdraw := lifecycleBody(t, service, policy, "withdraw")
	withdraw.Rationale = "Retain this release as withdrawn history."
	withdrawn := lifecycleDo(t, service, policy, withdraw)
	if withdrawn.Lifecycle.Releases[0].Status != "withdrawn" || len(withdrawn.Lifecycle.Events) != 2 {
		t.Fatal(withdrawn)
	}
	recovered := lifecycleDo(t, service, policy, q)
	if recovered.Delivery != "recovered" || recovered.Lifecycle.Releases[0].Status != "published" || recovered.Lifecycle.ObservedAt != activated.Lifecycle.ObservedAt {
		t.Fatal("recovery recomputed a historical outcome", recovered)
	}
	current, _ := service.lifecycle(ctx, id)
	if current.Releases[0].Status != "withdrawn" || len(current.Events) != 2 {
		t.Fatal(current)
	}
	again := lifecycleBody(t, service, policy, "activate")
	_, err = service.lifecycleCommand(ctx, id, policy.Document.Publications[0].Actor, again)
	lifecycleRefused(t, err, "lifecycle-conflict")
	retained := policyRead(t, s, token, id)
	old, _ := json.Marshal(policy)
	unchanged, _ := json.Marshal(retained)
	if string(old) != string(unchanged) {
		t.Fatal("lifecycle rewrote policy history or candidate bytes")
	}
	// Publication receipt predates lifecycle. It remains readable unchanged.
	var publicationOperation string
	_ = service.repository.View(ctx, func(ledger policyLedger) error {
		for key, operation := range ledger.Operations {
			if operation.Kind == "publish" {
				publicationOperation = key
			}
		}
		return nil
	})
	historical, err := service.operation(ctx, publicationOperation)
	if err != nil || historical.Lifecycle != nil || historical.Policy.Revision != policy.Revision {
		t.Fatal("legacy receipt was reinterpreted", historical, err)
	}
}

func TestPolicyLifecycleStrictDatesAndSupersessionReferences(t *testing.T) {
	_, _, policy, service := lifecycleFixture(t)
	ctx, actor := context.Background(), policy.Document.Publications[0].Actor
	for _, effective := range []string{"2030-01-01T11:59:59Z", "2030-01-01T12:00:00.0Z", "2030-01-01T12:00:00+00:00", "2030-02-30T12:00:00Z", "0000-01-01T12:00:00Z", "2030-01-01T12:00:00Z ", strings.Repeat("0", 4096)} {
		q := lifecycleBody(t, service, policy, "activate")
		q.EffectiveAt = effective
		_, err := service.lifecycleCommand(ctx, policy.Document.ID, actor, q)
		lifecycleRefused(t, err, "invalid-lifecycle-time")
	}
	for _, expiry := range []string{"2030-01-01T12:00:00Z", "2029-12-31T12:00:00Z", "2030-01-01T12:00:01.1Z"} {
		q := lifecycleBody(t, service, policy, "activate")
		q.ExpiresAt = expiry
		_, err := service.lifecycleCommand(ctx, policy.Document.ID, actor, q)
		lifecycleRefused(t, err, "invalid-lifecycle-time")
	}
	for _, target := range []string{policy.Document.Publications[0].ID, "publication_" + strings.Repeat("f", 48)} {
		q := lifecycleBody(t, service, policy, "activate")
		q.SupersedesPublicationID = target
		_, err := service.lifecycleCommand(ctx, policy.Document.ID, actor, q)
		lifecycleRefused(t, err, CodeBadRequest)
	}
	view, _ := service.lifecycle(ctx, policy.Document.ID)
	if len(view.Events) != 0 {
		t.Fatal("refusal changed history")
	}
}

func TestPolicyLifecycleSupersessionDoesNotWithdrawOrReactivate(t *testing.T) {
	for _, scheduled := range []bool{false, true} {
		t.Run(map[bool]string{false: "effective successor", true: "canceled future successor"}[scheduled], func(t *testing.T) {
			s, token, first, service := lifecycleFixture(t)
			firstActivation := lifecycleBody(t, service, first, "activate")
			lifecycleDo(t, service, first, firstActivation)
			edit := policyEditBody(t, first)
			edit.ArtifactVersion = "1.1.0"
			next := policyMutation(t, policyCall(s, token, "PUT", "/api/policies/"+first.Document.ID, edit)).Policy
			next = policyApprove(t, s, token, next)
			next = policyMutation(t, policyCall(s, token, "POST", "/api/policies/"+next.Document.ID+"/publish", policyPublishBody(t, next))).Policy
			q := lifecycleBody(t, service, next, "activate")
			q.SupersedesPublicationID = first.Document.Publications[0].ID
			if scheduled {
				q.EffectiveAt = "2030-01-01T13:00:00Z"
			}
			result := lifecycleDo(t, service, next, q)
			oldStatus, successorStatus := "superseded", "effective"
			if scheduled {
				oldStatus, successorStatus = "effective", "published"
			}
			if result.Lifecycle.Releases[0].Status != oldStatus || result.Lifecycle.Releases[0].WithdrawalID != "" || result.Lifecycle.Releases[1].Status != successorStatus {
				t.Fatal(result)
			}
			withdraw := lifecycleBody(t, service, next, "withdraw")
			withdraw.Rationale = "Retire only the successor."
			result = lifecycleDo(t, service, next, withdraw)
			service.clock = func() time.Time { return time.Date(2030, 1, 1, 14, 0, 0, 0, time.UTC) }
			view, err := service.lifecycle(context.Background(), next.Document.ID)
			if err != nil || view.Releases[0].Status != oldStatus || view.Releases[1].Status != "withdrawn" {
				t.Fatal("withdrawal reactivated older release", result)
			}
		})
	}
}

func TestPolicyLifecyclePolicyCASAndCrossPolicyScope(t *testing.T) {
	s, token, policy, service := lifecycleFixture(t)
	actor, ctx := policy.Document.Publications[0].Actor, context.Background()
	other := policyApprove(t, s, token, policyCreate(t, s, token))
	other = policyMutation(t, policyCall(s, token, "POST", "/api/policies/"+other.Document.ID+"/publish", policyPublishBody(t, other))).Policy
	q := lifecycleBody(t, service, policy, "activate")
	q.SupersedesPublicationID = other.Document.Publications[0].ID
	_, err := service.lifecycleCommand(ctx, policy.Document.ID, actor, q)
	lifecycleRefused(t, err, CodeBadRequest)
	q.SupersedesPublicationID = ""
	q.PublicationID = other.Document.Publications[0].ID
	_, err = service.lifecycleCommand(ctx, policy.Document.ID, actor, q)
	lifecycleRefused(t, err, CodeNotFound)
	q = lifecycleBody(t, service, policy, "activate")
	edit := policyEditBody(t, policy)
	edit.Narrative = "Changed draft while lifecycle confirmation was open"
	policyMutation(t, policyCall(s, token, "PUT", "/api/policies/"+policy.Document.ID, edit))
	_, err = service.lifecycleCommand(ctx, policy.Document.ID, actor, q)
	lifecycleRefused(t, err, "lifecycle-conflict")
	view, _ := service.lifecycle(ctx, policy.Document.ID)
	if len(view.Events) != 0 {
		t.Fatal("stale/scope refusal changed lifecycle", view)
	}
}

func TestPolicyLifecycleCASAndOperationIdentityRace(t *testing.T) {
	_, _, policy, service := lifecycleFixture(t)
	q := lifecycleBody(t, service, policy, "activate")
	other := q
	other.OperationID = policyOp(t)
	var group sync.WaitGroup
	errorsFound := make(chan error, 2)
	for _, request := range []policyLifecycleRequest{q, other} {
		group.Add(1)
		go func(request policyLifecycleRequest) {
			defer group.Done()
			_, err := service.lifecycleCommand(context.Background(), policy.Document.ID, policy.Document.Publications[0].Actor, request)
			errorsFound <- err
		}(request)
	}
	group.Wait()
	close(errorsFound)
	succeeded, refused := 0, 0
	for err := range errorsFound {
		if err == nil {
			succeeded++
		} else {
			lifecycleRefused(t, err, "lifecycle-conflict")
			refused++
		}
	}
	if succeeded != 1 || refused != 1 {
		t.Fatal(succeeded, refused)
	}
	view, _ := service.lifecycle(context.Background(), policy.Document.ID)
	if len(view.Events) != 1 {
		t.Fatal(view)
	}
	// Recover whichever request committed, then prove a changed body is refused.
	for _, request := range []policyLifecycleRequest{q, other} {
		if _, err := service.operation(context.Background(), request.OperationID); err == nil {
			request.ExpiresAt = "2030-01-01T13:00:00Z"
			_, err = service.lifecycleCommand(context.Background(), policy.Document.ID, policy.Document.Publications[0].Actor, request)
			lifecycleRefused(t, err, "idempotency-conflict")
		}
	}
}

func TestPolicyLifecycleHTTPGuardAndUncertainRecovery(t *testing.T) {
	s, token, policy, service := lifecycleFixture(t)
	path := "/api/policies/" + policy.Document.ID + "/lifecycle"
	q := lifecycleBody(t, service, policy, "activate")
	policyError(t, policyCall(s, "", "GET", path, nil), 401, CodeUnauthorized)
	policyError(t, policyCall(s, testToken, "POST", path, q), 401, CodeUnauthorized)
	req := httptest.NewRequest("POST", path, strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Origin", "https://foreign.example")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal("foreign Origin admitted", w.Code)
	}
	policyError(t, policyCall(s, token, "POST", path, map[string]any{"operationId": policyOp(t), "actor": policyActor{Kind: "local", Subject: "forged"}}), 400, CodeBadRequest)
	reads := 0
	testHookPrivateRead = func(name string) error {
		if name == "ledger.json" {
			reads++
			if reads == 2 {
				return errors.New("lost read-back")
			}
		}
		return nil
	}
	t.Cleanup(func() { testHookPrivateRead = nil })
	failure := policyError(t, policyCall(s, token, "POST", path, q), 503, "storage-unavailable")
	if failure["outcome"] != "unknown" {
		t.Fatal("uncertain outcome reported committed/refused", failure)
	}
	testHookPrivateRead = nil
	recovered := policyMutation(t, policyCall(s, token, "GET", "/api/policy-operations/"+q.OperationID, nil))
	if recovered.Delivery != "recovered" || recovered.Lifecycle == nil || len(recovered.Lifecycle.Events) != 1 {
		t.Fatal(recovered)
	}
	again := policyMutation(t, policyCall(s, token, "POST", path, q))
	if again.Delivery != "recovered" || len(again.Lifecycle.Events) != 1 {
		t.Fatal("operation replayed", again)
	}
}

func TestPolicyLifecycleRetentionFailureAndLedgerIntegrity(t *testing.T) {
	s, _, policy, service := lifecycleFixture(t)
	q := lifecycleBody(t, service, policy, "activate")
	service.artifacts = &policyFailingArtifacts{base: s.artifactRepository(), failAt: 1}
	_, err := service.lifecycleCommand(context.Background(), policy.Document.ID, policy.Document.Publications[0].Actor, q)
	lifecycleRefused(t, err, "storage-unavailable")
	view, _ := service.lifecycle(context.Background(), policy.Document.ID)
	if len(view.Events) != 0 {
		t.Fatal("failed receipt retention exposed lifecycle", view)
	}
	service.artifacts = s.artifactRepository()
	lifecycleDo(t, service, policy, q)
	var raw []byte
	_ = service.repository.View(context.Background(), func(ledger policyLedger) error { raw, _ = json.Marshal(ledger); return nil })
	for _, corrupt := range []func(*policyLifecycleEvent){func(e *policyLifecycleEvent) { e.Sequence = 2 }, func(e *policyLifecycleEvent) { e.Provenance = "source-provided" }, func(e *policyLifecycleEvent) { e.PublicationID = "publication_" + strings.Repeat("f", 48) }, func(e *policyLifecycleEvent) { e.ExpiresAt = e.EffectiveAt }, func(e *policyLifecycleEvent) { e.SupersedesPublicationID = e.PublicationID }} {
		var ledger policyLedger
		_ = json.Unmarshal(raw, &ledger)
		corrupt(&ledger.Lifecycles[policy.Document.ID].Events[0])
		if validatePolicyLedger(ledger) == nil {
			t.Fatal("corrupt lifecycle accepted")
		}
	}
}

type policyUnavailableLifecycleArtifacts struct {
	base    retainedArtifactRepository
	missing map[string]bool
}

func (f *policyUnavailableLifecycleArtifacts) Get(ctx context.Context, ref retainedArtifactRef) ([]byte, error) {
	if f.missing[ref.SHA256] {
		return nil, errors.New("fixture missing/corrupt candidate bytes")
	}
	return f.base.Get(ctx, ref)
}
func (f *policyUnavailableLifecycleArtifacts) Put(ctx context.Context, raw []byte) (retainedArtifactRef, error) {
	return f.base.Put(ctx, raw)
}

func TestPolicyLifecycleOnlyOutcomeBindsOldPublicationAndAllowsUnavailableDraft(t *testing.T) {
	s, token, original, service := lifecycleFixture(t)
	edit := policyEditBody(t, original)
	edit.Narrative = "New mutable candidate, unrelated to the old publication"
	edit.ArtifactVersion = "1.1.0"
	current := policyMutation(t, policyCall(s, token, "PUT", "/api/policies/"+original.Document.ID, edit)).Policy
	if current.Document.Candidate.Digest == original.Document.Candidate.Digest {
		t.Fatal("fixture did not change head")
	}
	artifacts := &policyUnavailableLifecycleArtifacts{base: s.artifactRepository(), missing: map[string]bool{current.Document.Candidate.Digest: true}}
	s.retainedArtifacts = artifacts
	service.artifacts = artifacts
	activate := lifecycleBody(t, service, current, "activate")
	// Activation can inspect its exact published target even with an unavailable
	// unrelated head. Its response contains no reconstructed Policy candidate.
	result := policyMutation(t, policyCall(s, token, "POST", "/api/policies/"+current.Document.ID+"/lifecycle", activate))
	if result.OutcomeKind != "lifecycle" || result.PolicyID != current.Document.ID || result.PolicyRevision != current.Revision || result.Receipt.CandidateDigest != original.Document.Candidate.Digest {
		t.Fatal("wrong candidate in lifecycle receipt", result)
	}
	artifacts.missing[original.Document.Candidate.Digest] = true
	if read := policyCall(s, token, "GET", "/api/policies/"+current.Document.ID, nil); read.Code != 503 {
		t.Fatal("missing head fabricated as readable", read.Code)
	}
	withdraw := lifecycleBody(t, service, current, "withdraw")
	withdraw.Rationale = "Withdraw despite unavailable retained content"
	w := policyCall(s, token, "POST", "/api/policies/"+current.Document.ID+"/lifecycle", withdraw)
	result = policyMutation(t, w)
	if strings.Contains(w.Body.String(), `"policy":`) || result.Receipt.CandidateDigest != original.Document.Candidate.Digest || result.Lifecycle.Events[len(result.Lifecycle.Events)-1].CandidateDigest != original.Document.Candidate.Digest {
		t.Fatal("withdrawal depended on/fabricated draft", w.Body.String())
	}
	recovered := policyMutation(t, policyCall(s, token, "GET", "/api/policy-operations/"+withdraw.OperationID, nil))
	if recovered.Lifecycle.Releases[0].Status != "withdrawn" || recovered.Receipt.CandidateDigest != original.Document.Candidate.Digest {
		t.Fatal(recovered)
	}
	corrupt := recovered
	corrupt.Receipt.CandidateDigest = current.Document.Candidate.Digest
	if validPolicyLifecycleResult(corrupt) {
		t.Fatal("recovery accepted a substituted candidate")
	}
}

func TestPolicyLifecycleHTTPRejectsPresentEmptyNullAndCaseVariantDates(t *testing.T) {
	s, token, policy, service := lifecycleFixture(t)
	q := lifecycleBody(t, service, policy, "activate")
	raw, _ := json.Marshal(q)
	for _, field := range []string{"effectiveAt", "expiresAt", "EffectiveAt"} {
		for _, value := range []any{nil, ""} {
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			body[field] = value
			policyError(t, policyCall(s, token, "POST", "/api/policies/"+policy.Document.ID+"/lifecycle", body), 400, CodeBadRequest)
		}
	}
	view, _ := service.lifecycle(context.Background(), policy.Document.ID)
	if len(view.Events) != 0 {
		t.Fatal("malformed explicit date activated now", view)
	}
	result := policyMutation(t, policyCall(s, token, "POST", "/api/policies/"+policy.Document.ID+"/lifecycle", q))
	if !validPolicyLifecycleTime(result.Lifecycle.Events[0].EffectiveAt) {
		t.Fatal("omitted date did not use server UTC time")
	}
}
