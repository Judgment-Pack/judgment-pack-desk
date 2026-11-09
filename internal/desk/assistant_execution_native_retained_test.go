package desk

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

// Exercise the registered subscription source, authenticated decision endpoint,
// actual Manager release/join and original Runtime owner. The handwritten result
// is fixture-specific: this does not qualify persistence of redacted real output.
func TestAssistantRetainedProductionNativeRuntimeApproval(t *testing.T) {
	for _, item := range []struct {
		name      string
		decisions []bool
	}{
		{"approve", []bool{true}},
		{"decline", []bool{false}},
		{"two-approvals", []bool{true, true}},
	} {
		t.Run(item.name, func(t *testing.T) {
			var phases *assistantNativeTerminalPhases
			if item.name == "two-approvals" {
				phases = &assistantNativeTerminalPhases{started: time.Now()}
				defer phases.log(t)
			}
			mode := "runtime-approval"
			if len(item.decisions) == 2 {
				mode = "runtime-approval-twice"
			}
			proceed := make(chan struct{})
			opened := make(chan *assistantExecution, 1)
			var completed atomic.Int32
			s, token, e, _, trace := assistantCodexSourceFixtureExecute(t, mode, "work", testAIAlpha, `{"profileVersion":1,"codex":{"inherit":true},"projectInstructions":"HOST-INSTRUCTIONS-ONCE","context":{"version":1,"projectBrief":"execution-brief.md"}}`, func(s *Server, _ string, start assistantRunStart) assistantRunStart {
				s.assistantRunExecutor = &assistantCanonicalCodexContinuationExecutor{s.assistantRunExecutor.(*assistantCodexExecutor)}
				if phases != nil {
					s.testAssistantRunPhase = func(_ *assistantRunExecution, phase string) {
						phases.mark(phase)
					}
				}
				return start
			}, func(ctx context.Context, e *assistantRunExecution) error {
				<-proceed
				// Use the existing native Runtime fixture. Select stable content
				// only here; ownership tests retain the peer's cwd assertions.
				assistantExecutionRuntimeFixture(t, e.service.server, e)
				path := filepath.Join(e.service.server.cfg.ProjectDir, "execution-fixture.json")
				raw, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				var config map[string]string
				if err = json.Unmarshal(raw, &config); err != nil {
					return err
				}
				config["retainedResult"] = "handwritten"
				raw, err = json.Marshal(config)
				if err != nil {
					return err
				}
				if err = os.WriteFile(path, raw, 0600); err != nil {
					return err
				}
				x, err := e.openAssistantExecution(ctx, assistantExecutionPrompt{Name: "served"})
				if err != nil {
					return err
				}
				opened <- x
				defer x.Close()
				return x.execute(ctx, func(ctx context.Context) error {
					phases.mark("completion-callback-entry")
					defer phases.mark("completion-callback-return")
					completed.Add(1)
					return e.completeAssistantConversation(ctx)
				})
			})
			close(proceed)
			var held *assistantExecution
			select {
			case held = <-opened:
			case <-e.done:
				t.Fatal("native execution failed before opening")
			case <-time.After(5 * time.Second):
				t.Fatal("native execution did not open")
			}
			defer func() {
				if t.Failed() {
					assistantRetainedProductionSnapshot(t, e, held, int32(len(assistantNativeRetainedGenerations(t, trace))))
				}
			}()
			var first assistantRunRecord
			var priorRows []contract.CheckpointDurableDispatch
			var callIDs []string
			originalBinding, originalStore := held.invocation.binding, held.invocation.store
			methodCalls := map[string]int(nil)
			for index, approved := range item.decisions {
				paused := assistantRetainedWaitParked(t, s, token, e)
				record := assistantExecutionRecord(t, e)
				if index == 0 {
					first = record
					methodCalls = assistantRetainedMethodCalls(t, record)
				}
				if paused.Checkpoint.Approval == nil || paused.Checkpoint.Approval.Runtime == nil || completed.Load() != 0 || !e.runtimeContinuationAlive(held.runtime) {
					t.Fatal("native Runtime approval lost its original retained owner or call")
				}
				e.canonicalMu.Lock()
				proof, binding, generation := e.settlement, e.canonicalBinding, e.generation
				sameAuthority := e.retainedRuntime == held.runtime && e.canonicalStore == originalStore && binding.Writer.Store == originalBinding.Writer.Store && binding.Writer.ID == originalBinding.Writer.ID && generation == held.invocation.generation+uint64(index)
				e.canonicalMu.Unlock()
				if !sameAuthority || binding.Writer.Failed() || !sameAssistantRunCheckpoint(binding.Writer.Checkpoint(), record.Checkpoint) || !proof.safe(nil) || !e.settledRuntimeCheckpoint(record) || len(proof.nativeClaims) != index+1 {
					t.Fatal("native pause lacks exact current writer, live release/join or original claim evidence")
				}
				priorActs := 0
				for _, prior := range item.decisions[:index] {
					if prior {
						priorActs++
					}
				}
				if len(assistantNativeRetainedActs(t, record)) != priorActs {
					t.Fatal("Runtime effect ran before its explicit native approval")
				}
				unknown := 0
				for _, row := range record.Checkpoint.Durable.Dispatches {
					if row.State == "claimed" {
						t.Fatal("parked native invocation still has claimed work")
					}
					if row.State == "unknown" {
						unknown++
						if row.Kind != "agent" || !row.Read || !reflect.DeepEqual(row.Failure, &contract.Failure{Version: 1, Code: "unknown", Source: "tool", Outcome: "unknown"}) {
							t.Fatal("native approval borrowed an unknown effect")
						}
					}
				}
				if unknown != index+1 {
					t.Fatal("native pause changed or lost a prior exact read-agent receipt")
				}
				for _, prior := range priorRows {
					found := false
					for _, row := range record.Checkpoint.Durable.Dispatches {
						if row.ID == prior.ID {
							found = reflect.DeepEqual(row, prior)
						}
					}
					if !found {
						t.Fatal("second approval changed a prior acknowledged row")
					}
				}
				priorRows = append([]contract.CheckpointDurableDispatch(nil), record.Checkpoint.Durable.Dispatches...)
				args, err := json.Marshal(paused.Checkpoint.Approval.Args)
				assistantRunMust(t, err)
				if string(args) != fmt.Sprintf(`{"fixtureStep":%d,"rehearsal":true}`, index+1) {
					t.Fatal("shown Runtime arguments differ from the prepared reviewed bytes")
				}
				callbacks := 0
				var observedIDs, invocations []string
				seenIDs := map[string]bool{}
				for _, op := range record.Operations {
					if op.Kind != "callback" {
						continue
					}
					var call struct {
						Invocation string `json:"invocation"`
						Call       struct {
							ID        string          `json:"id"`
							Arguments json.RawMessage `json:"arguments"`
						} `json:"call"`
					}
					assistantRunMust(t, json.Unmarshal(op.Intent, &call))
					callbacks++
					var receipt assistantAgentOutcome
					assistantRunMust(t, json.Unmarshal(op.Result, &receipt))
					if op.State != "completed" || call.Call.ID == "" || seenIDs[call.Call.ID] || receipt.Stop != "approval" || receipt.Failure != nil || receipt.Answer == nil {
						t.Fatal("native pause lacked a unique opaque call and exact completed callback receipt")
					}
					seenIDs[call.Call.ID] = true
					observedIDs = append(observedIDs, call.Call.ID)
					invocations = append(invocations, call.Invocation)
					var args map[string]any
					assistantRunMust(t, json.Unmarshal(call.Call.Arguments, &args))
					if !reflect.DeepEqual(args, map[string]any{"rehearsal": false, "fixtureStep": float64(callbacks)}) {
						t.Fatal("native model's original false rehearsal arguments were rewritten before Gate preparation")
					}
				}
				if callbacks != index+1 {
					t.Fatal("native callback count changed across explicit decisions")
				}
				for i, id := range callIDs {
					if observedIDs[i] != id {
						t.Fatal("prior native opaque call identity changed")
					}
				}
				callIDs = observedIDs
				if paused.Checkpoint.Approval.CallID != callIDs[index] {
					t.Fatal("Runtime ask was not bound to the exact original physical callback")
				}
				generations := assistantNativeRetainedGenerations(t, trace)
				if len(generations) != index+1 {
					t.Fatal("native pause executed an extra model generation")
				}
				for i, generation := range generations {
					var operation string
					assistantRunMust(t, json.Unmarshal(generation["operation"], &operation))
					if operation != invocations[i] {
						t.Fatal("native callback was not bound to its exact actual model invocation")
					}
				}
				if index > 0 {
					assistantNativeRetainedEvidence(t, generations[index], index, callIDs[index-1], item.decisions[index-1], record, paused.ApprovalResults, held.methodText)
				}
				decision := assistantRunAction{ID: fmt.Sprintf("native-explicit-decision-%d", index+1), Kind: "approval", Revision: paused.Revision, CallID: paused.Checkpoint.Approval.CallID, ArgsDigest: paused.ApprovalReference.ArgsDigest, ShownDigest: &paused.ApprovalReference.ArgsDigest, Approved: &approved}
				if index == len(item.decisions)-1 {
					phases.mark("final-approval-request-entry")
				}
				assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+paused.ID+"/actions", decision), 200)
				if index == len(item.decisions)-1 {
					phases.mark("final-approval-response")
				}
			}
			phases.mark("terminal-wait-start")
			// Each decision's continuation re-reads and rewrites the canonical
			// ledger, so the last one takes most of the old 15 s on an idle
			// machine. Wait as the other actual canonical continuations do.
			finalView := assistantProtocolWaitWithin(t, s, token, e.record.ID, "complete", 60*time.Second)
			phases.mark("terminal-wait-return")
			after := assistantExecutionRecord(t, e)
			e.canonicalMu.Lock()
			finalAuthority := e.canonicalStore == originalStore && e.canonicalBinding != originalBinding && e.canonicalBinding.Writer.Store == originalBinding.Writer.Store && e.canonicalBinding.Writer.ID == originalBinding.Writer.ID && e.generation == held.invocation.generation+uint64(len(item.decisions))
			e.canonicalMu.Unlock()
			if !finalAuthority || e.runtimeContinuationAlive(held.runtime) || held.gate.Calls() != held.Run.Gate {
				t.Fatal("native continuation replaced owner/Gate authority or failed terminal Runtime cleanup")
			}
			generations := assistantNativeRetainedGenerations(t, trace)
			if len(generations) != len(item.decisions)+1 || completed.Load() != 1 || len(after.Actions) != len(item.decisions) || after.Checkpoint.ID != first.Checkpoint.ID || !reflect.DeepEqual(methodCalls, assistantRetainedMethodCalls(t, after)) || len(after.Canonical.Commits) < len(first.Canonical.Commits) || !reflect.DeepEqual(first.Canonical.Commits, after.Canonical.Commits[:len(first.Canonical.Commits)]) {
				t.Fatal("native continuation replayed work, reset the journal, or lost completion/method evidence")
			}
			assistantNativeRetainedEvidence(t, generations[len(generations)-1], len(item.decisions), callIDs[len(callIDs)-1], item.decisions[len(item.decisions)-1], after, finalView.ApprovalResults, held.methodText)
			if len(finalView.ApprovalResults) != len(item.decisions) || !reflect.DeepEqual(finalView.ApprovalResults, assistantApprovalResults(after)) {
				t.Fatal("native decisions lost original person-visible results after a later approval")
			}
			acts := 0
			for _, op := range after.Operations {
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
				if call.Params.Name != "experimental_evaluate" {
					continue
				}
				acts++
				var receipt struct {
					Result *contract.ToolResultMCPToolResult `json:"result"`
				}
				assistantRunMust(t, json.Unmarshal(op.Result, &receipt))
				if op.State != "completed" || string(call.Params.Arguments) != fmt.Sprintf(`{"fixtureStep":%d,"rehearsal":true}`, acts) || assistantNativeRetainedText(receipt.Result) != "Exact native fixture evaluation result." {
					t.Fatal("physical native Runtime effect changed shown arguments or exact handwritten result")
				}
			}
			want := 0
			for _, approved := range item.decisions {
				if approved {
					want++
				}
			}
			if acts != want {
				t.Fatalf("physical native Runtime act count %d, want %d", acts, want)
			}
			for _, row := range after.Checkpoint.Durable.Dispatches {
				if row.State == "claimed" || row.State == "unknown" && row.Kind != "agent" {
					t.Fatal("completed native journey retained unfinished effect work")
				}
			}
		})
	}
}

func assistantNativeRetainedActs(t *testing.T, run assistantRunRecord) []assistantRunOperation {
	t.Helper()
	var acts []assistantRunOperation
	for _, op := range run.Operations {
		if op.Kind != "runtime" || op.Name != "tools/call" {
			continue
		}
		var call struct {
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		assistantRunMust(t, json.Unmarshal(op.Intent, &call))
		if call.Params.Name == "experimental_evaluate" {
			acts = append(acts, op)
		}
	}
	return acts
}

func assistantNativeRetainedGenerations(t *testing.T, trace string) []map[string]json.RawMessage {
	t.Helper()
	var generations []map[string]json.RawMessage
	for _, row := range assistantCodexLog(t, trace) {
		if string(row["type"]) == `"generation"` {
			generations = append(generations, row)
		}
	}
	return generations
}

func assistantNativeRetainedText(result *contract.ToolResultMCPToolResult) string {
	if result == nil || len(result.Content) != 1 || result.Content[0].Option1 == nil || result.Content[0].Option1.Text == nil {
		return ""
	}
	return *result.Content[0].Option1.Text
}

// Person-visible/canonical/physical equality is required. Exact native prompt
// delivery is observed separately under the accepted Agent309 limitation.
func assistantNativeRetainedEvidence(t *testing.T, generation map[string]json.RawMessage, step int, callID string, approved bool, record assistantRunRecord, display []assistantApprovalResultView, method string) {
	t.Helper()
	var prompt string
	assistantRunMust(t, json.Unmarshal(generation["prompt"], &prompt))
	if method == "" || !strings.Contains(prompt, method) || !strings.Contains(prompt, "Review exact input") {
		t.Fatal("native successor lost immutable method or fresh request context")
	}
	// A second ask replaces the current approval. Its predecessor's exact
	// answered checkpoint/result remains in the original canonical journal.
	var expected *contract.Checkpoint
	if record.Checkpoint.Approval != nil && record.Checkpoint.Approval.CallID == callID {
		expected = record.Checkpoint
	}
	for _, stored := range record.Canonical.Events {
		var envelope runcontext.StoredRunEnvelope
		assistantRunMust(t, json.Unmarshal(stored.Bytes, &envelope))
		if checkpoint := envelope.Event.ExecutionCheckpoint; checkpoint != nil && checkpoint.Checkpoint.Approval != nil {
			approval := checkpoint.Checkpoint.Approval
			if approval.CallID == callID && approval.Result != nil {
				expected = &checkpoint.Checkpoint
			}
		}
	}
	if expected == nil || expected.Approval == nil || expected.Approval.Result == nil {
		t.Fatal("native decision lost its exact canonical approval result")
	}
	result := expected.Approval.Result
	wantedDisplay := assistantApprovalResultViewOf(expected)
	matches := 0
	for _, shown := range display {
		if shown.CallID != callID {
			continue
		}
		matches++
		if wantedDisplay == nil || !reflect.DeepEqual(shown, *wantedDisplay) || !shown.Result.Available || len(shown.Result.Text) == 0 {
			t.Fatal("native person-visible result differs from original canonical result text")
		}
	}
	if matches != 1 || approved && assistantNativeRetainedText(result) != "Exact native fixture evaluation result." {
		t.Fatal("native decision lost exact available person-visible result text")
	}
	physical := 0
	for _, op := range assistantNativeRetainedActs(t, record) {
		var call struct {
			Params struct {
				Arguments struct {
					Step int `json:"fixtureStep"`
				} `json:"arguments"`
			} `json:"params"`
		}
		assistantRunMust(t, json.Unmarshal(op.Intent, &call))
		if call.Params.Arguments.Step != step {
			continue
		}
		physical++
		var receipt struct {
			Result *contract.ToolResultMCPToolResult `json:"result"`
		}
		assistantRunMust(t, json.Unmarshal(op.Result, &receipt))
		if op.State != "completed" || receipt.Result == nil || !reflect.DeepEqual(receipt.Result.Content, result.Content) {
			t.Fatal("native physical/canonical approval result content equality failed")
		}
	}
	wantPhysical := 0
	if approved {
		wantPhysical = 1
	}
	if physical != wantPhysical {
		t.Fatal("native explicit decision changed physical effect count")
	}

	var delivered struct {
		CallID   string                            `json:"callId"`
		Approved bool                              `json:"approved"`
		Result   *contract.ToolResultMCPToolResult `json:"result"`
	}
	found, exact := false, false
	for _, body := range strings.Split(prompt, runcontext.ApprovalContinuation)[1:] {
		framed := strings.TrimPrefix(body, "\n")
		opening := strings.IndexByte(framed, '\n')
		if opening >= 0 && json.NewDecoder(strings.NewReader(framed[opening+1:])).Decode(&delivered) == nil && delivered.CallID == callID {
			found = true
			exact = delivered.Approved == approved && reflect.DeepEqual(delivered.Result, result)
			if exact {
				break
			}
		}
	}
	// Neither delivery nor omission is a fixture requirement: record actual
	// bounded evidence without inventing a stronger native package contract.
	t.Logf("native continuation approvalResultBlockPresent=%t exactApprovalResultBlockPresent=%t", found, exact)
}

// These are hand-authored validator vectors, not manufactured canonical heads or
// receipts. The authenticated journey above supplies the live proof separately.
func TestAssistantNativeApprovalClaimRows(t *testing.T) {
	e, other := &assistantRunExecution{}, &assistantRunExecution{}
	binding := &runcontext.RunBinding{}
	claim := contract.CheckpointDurableDispatch{ID: "native-1", Scope: contract.CheckpointDurableScope{Execution: "run", Role: "author"}, Kind: "agent", Source: "provider", Tool: "model", Fingerprint: "fingerprint", Request: `{"prompt":"original"}`, RequestDigest: "request-digest", State: "claimed", Read: true}
	second := claim
	second.ID = "native-2"
	second.Request = `{"prompt":"successor"}`
	second.RequestDigest = "successor-digest"
	second.Fingerprint = "successor-fingerprint"
	var claims []string
	for _, row := range []contract.CheckpointDurableDispatch{claim, second} {
		raw, err := json.Marshal(row)
		assistantRunMust(t, err)
		claims = append(claims, string(raw))
	}
	rows := []contract.CheckpointDurableDispatch{claim, second}
	for i := range rows {
		rows[i].State = "unknown"
		rows[i].Failure = &contract.Failure{Version: 1, Code: "unknown", Source: "tool", Outcome: "unknown"}
	}
	proof := assistantInvocationSettlement{nativeOwner: e, nativeBinding: binding, nativeGeneration: 2, nativeClaims: claims}
	if !proof.nativeApprovalRows(e, binding, 2, rows) {
		t.Fatal("two exact original native claims were refused")
	}
	for _, item := range []struct {
		name   string
		change func(*contract.CheckpointDurableDispatch)
	}{
		{"unmatched-id", func(r *contract.CheckpointDurableDispatch) { r.ID = "foreign" }},
		{"changed-prior-request", func(r *contract.CheckpointDurableDispatch) { r.Request = `{"prompt":"changed"}` }},
		{"changed-prior-scope", func(r *contract.CheckpointDurableDispatch) { r.Scope.Role = "critic" }},
		{"changed-source", func(r *contract.CheckpointDurableDispatch) { r.Source = "foreign" }},
		{"changed-fingerprint", func(r *contract.CheckpointDurableDispatch) { r.Fingerprint = "foreign" }},
		{"changed-request-digest", func(r *contract.CheckpointDurableDispatch) { r.RequestDigest = "foreign" }},
		{"read-false", func(r *contract.CheckpointDurableDispatch) { r.Read = false }},
		{"unknown-runtime-effect", func(r *contract.CheckpointDurableDispatch) { r.Kind = "runtime"; r.Read = false }},
		{"unknown-host-effect", func(r *contract.CheckpointDurableDispatch) { r.Kind = "host"; r.Read = false }},
		{"result-present", func(r *contract.CheckpointDurableDispatch) { r.Result = json.RawMessage(`{}`) }},
		{"repeat-result-present", func(r *contract.CheckpointDurableDispatch) { v := "{}"; r.RepeatResultJSON = &v }},
		{"artifact-present", func(r *contract.CheckpointDurableDispatch) {
			r.Artifacts = []contract.CheckpointArtifactObservation{{}}
		}},
		{"detail-present", func(r *contract.CheckpointDurableDispatch) { v := "bounded detail"; r.Failure.Detail = &v }},
		{"different-failure", func(r *contract.CheckpointDurableDispatch) { r.Failure.Code = "permission" }},
		{"claimed-work", func(r *contract.CheckpointDurableDispatch) { r.State = "claimed"; r.Failure = nil }},
		{"changed-prior-state", func(r *contract.CheckpointDurableDispatch) { r.State = "completed"; r.Failure = nil }},
	} {
		t.Run(item.name, func(t *testing.T) {
			raw, err := json.Marshal(rows)
			assistantRunMust(t, err)
			var changed []contract.CheckpointDurableDispatch
			assistantRunMust(t, json.Unmarshal(raw, &changed))
			item.change(&changed[0])
			if proof.nativeApprovalRows(e, binding, 2, changed) {
				t.Fatal("changed or uncertain row borrowed native pause evidence")
			}
		})
	}
	if proof.nativeApprovalRows(other, binding, 2, rows) || proof.nativeApprovalRows(e, &runcontext.RunBinding{}, 2, rows) || proof.nativeApprovalRows(e, binding, 3, rows) {
		t.Fatal("native pause proof crossed owner, writer or generation")
	}
	if proof.nativeApprovalRows(e, binding, 2, rows[1:]) || proof.nativeApprovalRows(e, binding, 2, append(append([]contract.CheckpointDurableDispatch(nil), rows...), rows[0])) {
		t.Fatal("missing or duplicated claim was accepted")
	}
}

func TestAssistantNativeApprovalClaimTransfer(t *testing.T) {
	for _, mode := range []string{"same-run", "foreign-owner", "skipped-generation", "foreign-store", "foreign-writer-store", "foreign-run", "missing-old-binding", "missing-next-binding", "missing-old-writer", "missing-next-writer", "missing-old-store", "missing-next-store", "empty-run"} {
		t.Run(mode, func(t *testing.T) {
			e := &assistantRunExecution{}
			store := &assistantCanonicalRunStore{}
			old := &assistantExecutionInvocation{owner: e, generation: 1, store: store, binding: &runcontext.RunBinding{Writer: &runcontext.RunWriter{Store: store, ID: "run"}}, started: true, returned: true, closed: true, acceptedNativeClaims: []string{"original-immutable-claim"}}
			next := &assistantExecutionInvocation{owner: e, generation: 2, store: store, binding: &runcontext.RunBinding{Writer: &runcontext.RunWriter{Store: store, ID: "run"}}}
			switch mode {
			case "foreign-owner":
				next.owner = &assistantRunExecution{}
			case "skipped-generation":
				next.generation = 3
			case "foreign-store":
				next.store = &assistantCanonicalRunStore{}
			case "foreign-writer-store":
				next.binding.Writer.Store = &assistantCanonicalRunStore{}
			case "foreign-run":
				next.binding.Writer.ID = "foreign"
			case "missing-old-binding":
				old.binding = nil
			case "missing-next-binding":
				next.binding = nil
			case "missing-old-writer":
				old.binding.Writer = nil
			case "missing-next-writer":
				next.binding.Writer = nil
			case "missing-old-store":
				old.store = nil
			case "missing-next-store":
				next.store = nil
			case "empty-run":
				old.binding.Writer.ID, next.binding.Writer.ID = "", ""
			}
			relay := &assistantGateNoticeRelay{retired: old}
			err := relay.install(next)
			if mode != "same-run" {
				if err == nil || next.priorNativeClaims != nil {
					t.Fatal("prior native evidence crossed its original owner/run/generation")
				}
				return
			}
			assistantRunMust(t, err)
			if !reflect.DeepEqual(next.priorNativeClaims, []string{"original-immutable-claim"}) || old.acceptedNativeClaims != nil || old.priorNativeClaims != nil {
				t.Fatal("same-run handoff lost evidence or retained a redundant history")
			}
		})
	}
}

// Volatile fixed-name timings for the exact two-approvals fixture only. No
// callback logs, reads state, waits on work or starts a goroutine. The snapshot
// remains diagnostic even if the unchanged terminal wait has already failed.
type assistantNativeTerminalPhases struct {
	mu      sync.Mutex
	started time.Time
	count   int
	events  [64]struct {
		phase   string
		elapsed time.Duration
	}
}

func (p *assistantNativeTerminalPhases) mark(phase string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.count == len(p.events) {
		return
	}
	p.events[p.count].phase = phase
	p.events[p.count].elapsed = time.Since(p.started)
	p.count++
}

func (p *assistantNativeTerminalPhases) log(t *testing.T) {
	p.mu.Lock()
	events, count := p.events, p.count
	p.mu.Unlock()
	for _, event := range events[:count] {
		t.Logf("native terminal phase=%s elapsed_ns=%d", event.phase, event.elapsed.Nanoseconds())
	}
}
