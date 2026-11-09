package desk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
)

// Agent343 is exercised through the actual output/record guards and RunWriter,
// not an injected failure. The URI is public synthetic fixture data.
func TestAssistantRetainedProductionAPIGuardedResultUnknown(t *testing.T) {
	var requests atomic.Int32
	u := newUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			assistantPolicySSE(w, "experimental_evaluate", json.RawMessage(`{"rehearsal":false}`), "")
			return
		}
		assistantPolicySSE(w, "", nil, "Unexpected second model request.")
	})
	opened := make(chan *assistantExecution, 1)
	var start assistantRunStart
	s, token, e := assistantExecutionFixtureSource(t, u.server.URL, "work", func(ctx context.Context, e *assistantRunExecution) error {
		x, err := e.openAssistantExecution(ctx, assistantExecutionPrompt{Name: "served"})
		if err != nil {
			return err
		}
		opened <- x
		defer x.Close()
		return x.execute(ctx, e.completeAssistantConversation)
	}, func(s *Server, _ string, in assistantRunStart) assistantRunStart {
		s.assistantRunExecutor = &assistantCanonicalProtocolExecutor{s.assistantRunExecutor.(*assistantProtocolExecutor)}
		assistantP2ConfigureRuntime(t, s)
		t.Setenv("JPS_RUNTIME_OWNER_P1_RESULT", "guarded-public-uri")
		start = in
		return in
	})
	var held *assistantExecution
	select {
	case held = <-opened:
	case <-e.done:
		t.Fatal("guarded API journey failed before opening")
	case <-time.After(5 * time.Second):
		t.Fatal("guarded API journey did not open")
	}
	paused := assistantRetainedWaitParked(t, s, token, e)
	if paused.Checkpoint.Approval == nil || paused.Checkpoint.Approval.Runtime == nil || len(assistantGuardedFixtureActs(t, assistantExecutionRecord(t, e))) != 0 {
		t.Fatal("Runtime act was not retained before explicit approval")
	}
	approved := true
	decision := assistantRunAction{ID: "guarded-explicit-decision", Kind: "approval", Revision: paused.Revision, CallID: paused.Checkpoint.Approval.CallID, ArgsDigest: paused.ApprovalReference.ArgsDigest, ShownDigest: &paused.ApprovalReference.ArgsDigest, Approved: &approved}
	assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+paused.ID+"/actions", decision), 200)
	select {
	case <-e.done:
	case <-time.After(15 * time.Second):
		t.Fatal("guarded-result execution owner did not stop")
	}
	h := s.assistantRunService()
	h.mu.Lock()
	active := h.active[e.record.ID] != nil
	h.mu.Unlock()
	if active || e.runtimeContinuationAlive(held.runtime) || requests.Load() != 1 || len(u.arrivals()) != 1 {
		t.Fatal("guarded result left a live owner or dispatched another model request")
	}
	before := assistantExecutionRecord(t, e)
	acts := assistantGuardedFixtureActs(t, before)
	if len(acts) != 1 || acts[0].State != "completed" || len(acts[0].Result) == 0 {
		t.Fatal("explicit approval did not execute and acknowledge exactly one physical act")
	}
	var receipt struct {
		Result *contract.ToolResultMCPToolResult `json:"result"`
	}
	assistantRunMust(t, json.Unmarshal(acts[0].Result, &receipt))
	if receipt.Result == nil || len(receipt.Result.Content) != 1 || receipt.Result.Content[0].Option1 == nil || receipt.Result.Content[0].Option1.Text == nil || *receipt.Result.Content[0].Option1.Text != "Evaluation result for https://example.invalid/tmp/public-policy" {
		t.Fatal("physical fixture result was replaced or did not contain the guard trigger")
	}
	if before.Checkpoint.Approval == nil || before.Checkpoint.Approval.State != "approved" || before.Checkpoint.Approval.Result != nil || assistantUnkeptApproval(before) == nil {
		t.Fatal("guard did not leave the exact approved claim without a canonical result")
	}
	// Save checks the original fatal error before closed-dispatch handling. Pass
	// its own acknowledged checkpoint unchanged: this must refuse before any
	// write. No artificial error, replacement writer or checkpoint is supplied.
	e.canonicalMu.Lock()
	binding := e.canonicalBinding
	e.canonicalMu.Unlock()
	err := binding.Writer.Save(t.Context(), *binding, *binding.Writer.Checkpoint())
	var unknown *runcontext.StorageOutcomeUnknown
	if !errors.As(err, &unknown) {
		t.Fatalf("actual guarded writer did not retain StorageOutcomeUnknown: %T", err)
	}
	f := failure.Normalize(err, "storage", time.Time{})
	if f.Code != "storage" || f.Source != "storage" || f.Outcome != "unknown" {
		t.Fatal("guarded writer lost structured unknown storage failure")
	}
	// The same evidence while a writer is live is not a stopped/unknown public
	// outcome. This is a display predicate check, not a fabricated live owner.
	live := assistantUnkeptApprovalPublic(before, true)
	if live.State != before.State || !reflect.DeepEqual(live.ApprovalResults, assistantApprovalResults(before)) {
		t.Fatal("in-flight pre-Commit evidence was classified as a stopped owner")
	}
	view := assistantProtocolView(t, policyCall(s, token, "GET", "/api/assistant-runs/"+before.ID, nil), 200)
	assistantGuardedUnknownView(t, view)
	assistantRunClientJSON(t, "actual-guarded-result-343.json", view)
	// Exact matching uses the existing admitted request/chat revision and all
	// scope bindings. A genuine new request is not a blanket per-chat refusal.
	source := e.source
	if !assistantGuardedApprovalRetry(source, before) {
		t.Fatal("same original admitted request was not recognized as an unsafe retry")
	}
	source.Binding.RequestID = "genuinely-new-user-request"
	source.Binding.RequestDigest = digestOf([]byte("A different user request."))
	if assistantGuardedApprovalRetry(source, before) {
		t.Fatal("guarded result blocked a genuinely new request")
	}
	// Duplicate delivery is idempotent; a new decision/resume cannot reacquire
	// the lost canonical owner. A fresh admission id cannot retry this request.
	assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+before.ID+"/actions", decision), 200)
	stale := decision
	stale.ID = "guarded-new-decision"
	stale.Revision = view.Revision
	assistantGuardedHTTPRefusal(t, policyCall(s, token, "POST", "/api/assistant-runs/"+before.ID+"/actions", stale), http.StatusServiceUnavailable, "This assistant purpose is not available on the server yet.")
	assistantGuardedHTTPRefusal(t, policyCall(s, token, "POST", "/api/assistant-runs/"+before.ID+"/actions", map[string]any{"id": "guarded-retry", "revision": view.Revision, "kind": "retry"}), http.StatusBadRequest, "Invalid assistant run request.")
	retry := start
	retry.AdmissionID = "guarded-fresh-admission"
	assistantGuardedHTTPRefusal(t, policyCall(s, token, "POST", "/api/assistant-runs", retry), http.StatusServiceUnavailable, "This assistant purpose is not available on the server yet.")
	for i := 0; i < 2; i++ {
		reloaded := assistantProtocolView(t, policyCall(s, token, "GET", "/api/assistant-runs/"+before.ID, nil), 200)
		assistantGuardedUnknownView(t, reloaded)
		if !sameAssistantRunCheckpoint(reloaded.Checkpoint, before.Checkpoint) || !reflect.DeepEqual(reloaded.Operations, before.Operations) {
			t.Fatal("reload changed original checkpoint or physical receipts")
		}
	}
	discovery := policyCall(s, token, "GET", "/api/assistant-runs/owners/"+url.PathEscape(before.Binding.Owner.ID)+"/requests/"+url.PathEscape(before.Binding.RequestID), nil)
	if discovery.Code != http.StatusOK {
		t.Fatalf("guarded reload discovery status=%d want=%d", discovery.Code, http.StatusOK)
	}
	var discovered struct {
		Runs []assistantRunView `json:"runs"`
	}
	assistantRunMust(t, json.Unmarshal(discovery.Body.Bytes(), &discovered))
	if len(discovered.Runs) != 1 {
		t.Fatal("reload discovery lost or duplicated guarded run")
	}
	assistantGuardedUnknownView(t, discovered.Runs[0])
	after := assistantExecutionRecord(t, e)
	if requests.Load() != 1 || len(assistantGuardedFixtureActs(t, after)) != 1 || !reflect.DeepEqual(before.Canonical, after.Canonical) || !sameAssistantRunCheckpoint(before.Checkpoint, after.Checkpoint) || !reflect.DeepEqual(before.Operations, after.Operations) || len(after.Actions) != 1 {
		t.Fatal("guarded-result read/duplicate/retry path replayed or changed canonical evidence")
	}
}

func assistantGuardedFixtureActs(t *testing.T, run assistantRunRecord) []assistantRunOperation {
	t.Helper()
	var acts []assistantRunOperation
	for _, op := range run.Operations {
		if op.Kind != "runtime" || op.Name != "tools/call" {
			continue
		}
		var call struct {
			Params struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			} `json:"params"`
		}
		assistantRunMust(t, json.Unmarshal(op.Intent, &call))
		if call.Params.Name == "experimental_evaluate" {
			if string(call.Params.Arguments) != `{"rehearsal":true}` {
				t.Fatal("physical guarded action changed shown reviewed arguments")
			}
			acts = append(acts, op)
		}
	}
	return acts
}

func assistantGuardedUnknownView(t *testing.T, view assistantRunView) {
	t.Helper()
	if view.State != "interrupted" || view.Checkpoint == nil || view.Checkpoint.Approval == nil || view.Checkpoint.Approval.State != "approved" || view.Checkpoint.Approval.Result != nil || view.ApprovalReference != nil || len(view.ApprovalResults) != 1 {
		t.Fatal("person-visible view did not distinguish unknown stopped owner from unchanged canonical head")
	}
	result := view.ApprovalResults[0]
	if !result.IsError || result.Result.Available || len(result.Result.Text) != 0 || len(result.Result.OtherContent) != 0 || result.CallID != view.Checkpoint.Approval.CallID {
		t.Fatal("person-visible view fabricated retained result or reported success")
	}
	raw, err := json.Marshal(result)
	assistantRunMust(t, err)
	if strings.Contains(string(raw), "https://example.invalid/tmp/public-policy") {
		t.Fatal("unretained result was fabricated in the result display")
	}
}

// Require the named refusal contract; an arbitrary server failure is not
// evidence that the original action could not be dispatched again.
func assistantGuardedHTTPRefusal(t *testing.T, response *httptest.ResponseRecorder, status int, message string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("guarded action refusal status=%d want=%d", response.Code, status)
	}
	var body map[string]string
	assistantRunMust(t, json.Unmarshal(response.Body.Bytes(), &body))
	if !reflect.DeepEqual(body, map[string]string{"code": CodeBadRequest, "error": message}) {
		t.Fatal("guarded action did not return its exact structured refusal")
	}
}
