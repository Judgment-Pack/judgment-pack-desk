package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter/delegate"
	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter/turn"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/gate"
	"github.com/Judgment-Pack/judgment-pack-agent/go/relay"
)

// Real production admission, source/phase checks, private journal and published
// Runtime. These handwritten provider replies are inputs, never recorded runs.
func TestAssistantCanonicalActivationProductionAPI(t *testing.T) {
	for _, family := range []string{"openai-compatible", "anthropic", "gemini"} {
		t.Run(family, func(t *testing.T) {
			var requests atomic.Int32
			u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				reply := `{"id":"fixture","object":"chat.completion","model":"shared-model","choices":[{"index":0,"message":{"role":"assistant","content":"Canonical answer."},"finish_reason":"stop"}]}`
				if family == "anthropic" {
					reply = `{"id":"fixture","type":"message","role":"assistant","model":"shared-model","content":[{"type":"text","text":"Canonical answer."}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`
				}
				if family == "gemini" {
					reply = `{"candidates":[{"content":{"role":"model","parts":[{"text":"Canonical answer."}]},"finishReason":"STOP"}]}`
				}
				_, _ = fmt.Fprint(w, reply)
			})
			s, token, start, _ := assistantProductionFixture(t, u.server.URL, "work", "")
			registry, err := s.readAIRegistry()
			assistantRunMust(t, err)
			c := &registry.Connections[0]
			c.Assistant.Endpoint.Kind = family
			raw, err := encodeAIRegistry(registry.aiRegistry)
			assistantRunMust(t, err)
			assistantRunMust(t, s.assistant.writeConfigNamed(aiConnectionsFile, raw, nil))
			origin, _ := endpointOrigin(u.server.URL)
			assistantRunMust(t, s.assistant.storeKeyNamed(connectionKeyName(c), storedKey{present: true, key: "production-fixture", origin: origin, kind: family}))
			initial := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
			// Strict published guarding scans retained prepared requests per commit.
			// This bounded fixture allowance leaves production request limits unchanged.
			view := assistantProtocolWaitWithin(t, s, token, initial.ID, "complete", 60*time.Second)
			record := canonicalActivationRecord(t, s, view.ID)
			canonicalActivationCheck(t, record)
			if requests.Load() != 1 || record.Checkpoint.Durable == nil || record.Checkpoint.Digest == nil || record.ChatProjection == nil || !record.ChatProjection.Acknowledged {
				t.Fatal("actual canonical production path missing", requests.Load())
			}
			models := 0
			for _, op := range record.Operations {
				if op.Kind == "model" {
					models++
					if op.State != "completed" || len(op.Result) == 0 {
						t.Fatal("physical model acknowledgement missing")
					}
				}
			}
			if models != 1 || len(record.Canonical.Commits) < 3 {
				t.Fatal("canonical/domain journals missing")
			}
			before, _ := json.Marshal(record.Canonical)
			assistantProtocolView(t, policyCall(s, token, "GET", "/api/assistant-runs/"+view.ID, nil), 200)
			if w := policyCall(s, token, "GET", "/api/assistant-runs/"+view.ID+"/events", nil); w.Code != 200 {
				t.Fatal(w.Code)
			}
			assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 200)
			after, _ := json.Marshal(canonicalActivationRecord(t, s, view.ID).Canonical)
			if !bytes.Equal(before, after) || requests.Load() != 1 {
				t.Fatal("read/admission replay changed canonical bytes or dispatched")
			}
		})
	}
}
func canonicalActivationRecord(t *testing.T, s *Server, id string) assistantRunRecord {
	t.Helper()
	for _, record := range assistantAuthoringLedger(t, s).Runs {
		if record.ID == id {
			return record
		}
	}
	t.Fatal("run missing")
	return assistantRunRecord{}
}
func canonicalActivationCheck(t *testing.T, run assistantRunRecord) {
	t.Helper()
	if !assistantCanonicalActive(run) || !validAssistantCanonicalJournal(run.Canonical) {
		t.Fatal("canonical profile missing/invalid")
	}
	assistantRunMust(t, validateAssistantCanonicalProjection(run))
	value, err := runcontext.ParseJSON(run.Canonical.Head.Checkpoint)
	assistantRunMust(t, err)
	cp, err := runcontext.ReadExecution(value)
	assistantRunMust(t, err)
	if !sameAssistantRunCheckpoint(cp, run.Checkpoint) {
		t.Fatal("public/canonical checkpoint diverged")
	}
	seen := map[string]bool{}
	for _, event := range run.Events {
		if seen[event.ID] {
			t.Fatal("public delivery duplicated")
		}
		seen[event.ID] = true
	}
}
func canonicalActivationAdmitted(t *testing.T) (assistantRunStore, assistantRunBinding, *assistantRunLease, *assistantRunWriter, assistantRunRecord) {
	t.Helper()
	store, binding, cp := assistantRunFixture(t)
	lease, err := store.Acquire(t.Context())
	assistantRunMust(t, err)
	t.Cleanup(func() { _ = lease.Close() })
	record, created, err := lease.Admit(t.Context(), binding, "canonical-admission", digestOf([]byte("canonical body")), func() error { return nil })
	assistantRunMust(t, err)
	if !created {
		t.Fatal("not created")
	}
	writer, err := lease.Writer(t.Context(), binding, record.ID)
	assistantRunMust(t, err)
	assistantRunMust(t, writer.initializeCanonical(t.Context(), cp))
	record, err = store.Read(t.Context(), binding, record.ID)
	assistantRunMust(t, err)
	return store, binding, lease, writer, record
}
func TestAssistantCanonicalActivationRecoveryAndImportExact(t *testing.T) {
	for _, mode := range []string{"stop", "reacquire", "import"} {
		t.Run(mode, func(t *testing.T) {
			store, binding, lease, writer, record := canonicalActivationAdmitted(t)
			e := &assistantRunExecution{record: record, writer: writer}
			input := &runcontext.Input{Execution: record.Checkpoint}
			assistantRunMust(t, e.bindCanonicalInput(t.Context(), input))
			input, err := runcontext.PrepareDurableInput(t.Context(), input)
			assistantRunMust(t, err)
			assistantRunMust(t, input.Persistence.Writer.Save(t.Context(), *input.Persistence, *record.Checkpoint))
			op := assistantRunOp("observed-before-crash")
			assistantRunMust(t, writer.Intent(t.Context(), op))
			assistantRunMust(t, writer.Dispatch(t.Context(), op.ID))
			before, err := store.Read(t.Context(), binding, record.ID)
			assistantRunMust(t, err)
			canonicalActivationCheck(t, before)
			exact, _ := json.Marshal(before.Canonical)
			if mode == "stop" {
				assistantRunMust(t, writer.Stop(t.Context()))
			}
			if mode == "reacquire" {
				assistantRunMust(t, lease.Close())
				recovered, err := store.Acquire(t.Context())
				assistantRunMust(t, err)
				defer recovered.Close()
			}
			if mode == "import" {
				raw := assistantRunRaw(t, store)
				historical, err := historicalAssistantRunStorage(store.name, raw)
				assistantRunMust(t, err)
				ledger, err := decodeAssistantRunStorage(store.name, historical)
				assistantRunMust(t, err)
				after := ledger.Runs[0]
				saved, _ := json.Marshal(after.Canonical)
				if !after.Historical || !bytes.Equal(exact, saved) || !sameAssistantRunCheckpoint(before.Checkpoint, after.Checkpoint) || after.Operations[0].State != "uncertain" {
					t.Fatal("import changed acknowledged bytes/custody")
				}
				return
			}
			after, err := store.Read(t.Context(), binding, record.ID)
			assistantRunMust(t, err)
			saved, _ := json.Marshal(after.Canonical)
			if !bytes.Equal(exact, saved) || !sameAssistantRunCheckpoint(before.Checkpoint, after.Checkpoint) || after.Operations[0].State != "uncertain" {
				t.Fatal("recovery rewrote canonical bytes or replayed")
			}
			if err := writer.Dispatch(t.Context(), op.ID); err == nil {
				t.Fatal("uncertain operation replayed")
			}
		})
	}
}

// Exercise the released delegation coordinator with a real admitted filesystem
// store and handwritten session steps; no provider or memory store substitutes.
func TestAssistantCanonicalActivationDecoratorAndChildren(t *testing.T) {
	store, binding, _, writer, record := canonicalActivationAdmitted(t)
	e := &assistantRunExecution{record: record, writer: writer}
	g := gate.New(gate.Options{Granted: []string{gate.TestTool}, Runtime: func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return nil, errors.New("handwritten runtime refusal")
	}})
	g.SetOffer(contract.ToolOffer{Tools: []contract.ToolOfferRuntimeTool{{Name: gate.TestTool}}, HostTools: []contract.ToolOfferHostTool{turn.DelegationTool(turn.TaskProfiles)}})
	g.Enter(gate.LoopPhase(contract.ToolOffer{Tools: []contract.ToolOfferRuntimeTool{{Name: gate.TestTool}}}))
	input := &runcontext.Input{Execution: record.Checkpoint, CurrentRequest: &runcontext.CurrentRequest{Message: "Review", Input: "exact original"}}
	assistantRunMust(t, e.bindCanonicalInput(t.Context(), input))
	var rootCalls, childCalls int
	inner := adapter.Func(func(ctx context.Context, run *adapter.Run) error {
		models := runcontext.RunModels{Critic: "fixture", Child: "fixture"}
		opened, err := runcontext.ContextualSession(ctx, runcontext.Session{Prompt: "Review the exact saved input", Model: &runcontext.ModelSession{Family: "gemini", Model: "fixture"}, Context: run.Context, HostTools: run.HostTools, PolicyLegacyModels: &models})
		if err != nil {
			return err
		}
		defer opened.Context.Close()
		_, _ = run.Gate.CallTool(ctx, gate.TestTool, json.RawMessage(`{}`))
		state, err := opened.Context.Snapshot("")
		if err != nil {
			return err
		}
		p := run.Context.Persistence
		if err = p.Writer.Save(ctx, *p, state); err != nil {
			return err
		}
		current, err := store.Read(ctx, binding, record.ID)
		if err != nil {
			return err
		}
		if current.Checkpoint.TestCalls == nil || *current.Checkpoint.TestCalls != int64(g.TestCalls()) || current.Checkpoint.Digest == nil {
			t.Error("live first/child count or digest lost")
		}
		if run.Context.Objective == "" {
			rootCalls++
			for _, tool := range opened.Session.HostTools {
				if tool.Name == turn.DelegateTool {
					_, err = tool.Execute(ctx, json.RawMessage(`{"tasks":[{"title":"Review","objective":"Check supplied facts.","profile":"review"}]}`))
					if err != nil {
						return err
					}
				}
			}
		} else {
			childCalls++
		}
		state, err = opened.Context.Snapshot("complete")
		if err != nil {
			return err
		}
		if err = p.Writer.Save(ctx, *p, state); err != nil {
			return err
		}
		run.Emit(contract.RunEvent{Message: &contract.RunEventMessage{Type: "message", Text: "Child facts retained."}})
		return nil
	})
	run := &adapter.Run{ModelName: "fixture", Wire: "gemini", Prompt: "Review", Critic: "off", AllowConversation: true, Interactive: true, Context: input, Gate: g.Calls(), Model: func(context.Context, string, relay.Request) (*http.Response, error) {
		return nil, errors.New("no model dispatched")
	}, Emit: func(contract.RunEvent) {}, Models: func() (runcontext.RunModels, error) {
		return runcontext.RunModels{Critic: "fixture", Child: "fixture"}, nil
	}}
	run.Children = func(ctx context.Context, profile contract.DelegatedTaskProfile, emit func(contract.RunEvent)) (*gate.Calls, []contract.ToolOfferRuntimeTool, error) {
		child := gate.New(gate.Options{Granted: []string{gate.TestTool}, Turn: g, Runtime: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			return nil, errors.New("handwritten child refusal")
		}, Emit: emit})
		child.SetOffer(contract.ToolOffer{Tools: []contract.ToolOfferRuntimeTool{{Name: gate.TestTool}}})
		child.Enter(gate.LoopPhase(contract.ToolOffer{Tools: []contract.ToolOfferRuntimeTool{{Name: gate.TestTool}}}))
		return child.Calls(), nil, nil
	}
	assistantRunMust(t, delegate.With(e.canonicalAdapter(inner, g), delegate.Options{}).Run(t.Context(), run))
	after, err := store.Read(t.Context(), binding, record.ID)
	assistantRunMust(t, err)
	canonicalActivationCheck(t, after)
	if rootCalls != 1 || childCalls != 1 || g.TestCalls() != 2 || after.Checkpoint.TestCalls == nil || *after.Checkpoint.TestCalls != 2 || after.Checkpoint.Delegation == nil || len(after.Checkpoint.Delegation.Batches) != 1 || after.Checkpoint.Durable == nil || len(after.Checkpoint.Durable.Children) != 1 {
		t.Fatalf("decorator composition lost root/child/counter/figures: root=%d child=%d count=%d events=%v", rootCalls, childCalls, g.TestCalls(), after.Events)
	}
}

func TestAssistantCanonicalActivationStorageUncertaintyClosesDispatch(t *testing.T) {
	for _, mode := range []string{"claim-before", "claim-after", "result-before", "result-after"} {
		t.Run(mode, func(t *testing.T) {
			store, binding, _, writer, record := canonicalActivationAdmitted(t)
			e := &assistantRunExecution{record: record, writer: writer}
			input := &runcontext.Input{Execution: record.Checkpoint}
			assistantRunMust(t, e.bindCanonicalInput(t.Context(), input))
			input, err := runcontext.PrepareDurableInput(t.Context(), input)
			assistantRunMust(t, err)
			stage, reads, calls := "claim", 0, 0
			testHookPrivateRead = func(name string) error {
				if name != store.name || !strings.HasPrefix(mode, stage+"-") {
					return nil
				}
				reads++
				if strings.HasSuffix(mode, "-before") && reads == 1 || strings.HasSuffix(mode, "-after") && reads == 2 {
					return errors.New("handwritten canonical acknowledgement fault")
				}
				return nil
			}
			defer func() { testHookPrivateRead = nil }()
			invoke := func() (json.RawMessage, error) {
				return runcontext.InvokeDurable(t.Context(), input.Persistence, "host", "fixture-source", "fixture-write", []byte(`{"missing":null,"enabled":false}`), false, func([]byte) (json.RawMessage, error) {
					calls++
					stage = "result"
					reads = 0
					return json.RawMessage(`{"observed":true,"missing":null}`), nil
				}, func(json.RawMessage) bool { return true }, nil)
			}
			_, err = invoke()
			if err == nil || !input.Persistence.Writer.Failed() {
				t.Fatal("storage error did not latch dependent dispatch")
			}
			expected := 0
			if strings.HasPrefix(mode, "result-") {
				expected = 1
			}
			if calls != expected {
				t.Fatal("claim/result boundary crossed", calls, expected)
			}
			testHookPrivateRead = nil
			port, err := writer.canonicalStore(t.Context(), record.Canonical.Head.RunID)
			assistantRunMust(t, err)
			_, err = port.Load(t.Context(), port.id)
			assistantRunMust(t, err)
			_, err = invoke()
			if err == nil || calls != expected {
				t.Fatal("reading a published outcome reopened failed dispatch")
			}
			observed, err := store.Read(t.Context(), binding, record.ID)
			assistantRunMust(t, err)
			canonicalActivationCheck(t, observed)
		})
	}
}

type assistantCanonicalProtocolExecutor struct {
	*assistantProtocolExecutor
}

func (*assistantCanonicalProtocolExecutor) assistantCanonicalRuns() {}
func TestAssistantCanonicalActivationActualContinuations(t *testing.T) {
	for _, kind := range []string{"answer", "extend-budget"} {
		t.Run(kind, func(t *testing.T) {
			var requests atomic.Int32
			u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				n := requests.Add(1)
				if n == 1 {
					name, args := "ask_user", `{"title":"Scope","question":"Which scope?","selection":"single","options":[{"id":"one","label":"One"},{"id":"two","label":"Two"}]}`
					if kind != "answer" {
						name, args = "validate", `{}`
					}
					assistantPolicySSE(w, name, json.RawMessage(args), "")
				} else {
					assistantBriefSSE(w, "Exact continuation retained.", "stop")
				}
			})
			proceed := make(chan struct{})
			s, token, e := assistantExecutionFixtureSource(t, u.server.URL, "work", func(ctx context.Context, e *assistantRunExecution) error {
				<-proceed
				assistantExecutionRuntimeFixture(t, e.service.server, e)
				x, err := e.openAssistantExecution(ctx, assistantExecutionPrompt{Name: "served"})
				if err != nil {
					t.Logf("continuation fixture open failed: %v", err)
					return fmt.Errorf("continuation fixture opening: %w", err)
				}
				defer x.Close()
				if kind == "answer" {
					assistantContinuationOfferQuestion(t, x)
				}
				if kind != "answer" && len(e.record.Actions) == 0 {
					// Establish required critic/source custody before this handwritten
					// near-ceiling input is restored through the real canonical CAS.
					cp := *x.Run.Context.Execution
					cp.Used = cp.Limit - 1
					if cp.Digest != nil {
						raw, _ := json.Marshal(cp)
						value, err := runcontext.ParseJSON(raw)
						if err != nil {
							return err
						}
						digest := runcontext.RecordDigest(value)
						cp.Digest = &digest
					}
					if err := x.Run.Context.Checkpoint(ctx, cp); err != nil {
						return err
					}
					x.Run.Context.Execution = x.Run.Context.Persistence.Writer.Checkpoint()
				}

				return x.Adapter.Run(ctx, x.Run)
			}, func(s *Server, _ string, start assistantRunStart) assistantRunStart {
				original := s.assistantRunExecutor.(*assistantProtocolExecutor)
				s.assistantRunExecutor = &assistantCanonicalProtocolExecutor{assistantProtocolExecutor: original}
				return start
			})
			close(proceed)
			state := "awaiting-input"
			if kind != "answer" {
				state = "paused-budget"
			}
			paused := assistantProtocolWaitWithin(t, s, token, e.record.ID, state, 60*time.Second)
			before := canonicalActivationRecord(t, s, e.record.ID)
			canonicalActivationCheck(t, before)
			a := assistantContinuationActualAnswer(paused)
			if kind != "answer" {
				a = assistantContinuationAction(paused, kind)
			}
			admitted := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+paused.ID+"/actions", a), 200)
			next := assistantContinuationSuccessor(t, s, token, admitted)
			complete := assistantProtocolWaitWithin(t, s, token, next.ID, "complete", 60*time.Second)
			after := canonicalActivationRecord(t, s, next.ID)
			canonicalActivationCheck(t, after)
			parent := canonicalActivationRecord(t, s, paused.ID)
			if requests.Load() != 2 || len(parent.Actions) != 1 {
				t.Fatal("explicit action missing or replayed")
			}
			if kind == "answer" {
				if next.ID == paused.ID || after.Checkpoint.ID == before.Checkpoint.ID || after.Checkpoint.Used != 1 || after.Checkpoint.Limit != 20 || len(after.Checkpoint.Grants) != 0 || !reflect.DeepEqual(before.Canonical, parent.Canonical) || !reflect.DeepEqual(before.Checkpoint, parent.Checkpoint) || !reflect.DeepEqual(before.Operations, parent.Operations) || !reflect.DeepEqual(before.Events, parent.Events) {
					t.Fatal("successor reset or mutated parent journal")
				}
			} else {
				if admitted.Checkpoint.Status != "running" || after.Checkpoint.ID != before.Checkpoint.ID || !reflect.DeepEqual(after.Canonical.Commits[:len(before.Canonical.Commits)], before.Canonical.Commits) {
					t.Fatal("budget reset canonical journal")
				}
				found := false
				for _, commit := range after.Canonical.Commits {
					if commit.ID == "host-action-"+a.ID {
						found = commit.ExpectedRevision == before.Canonical.Head.Revision
					}
				}
				if !found {
					t.Fatal("budget host action canonical receipt missing")
				}
			}
			exact, _ := json.Marshal(after.Canonical)
			assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+paused.ID+"/actions", a), 200)
			again, _ := json.Marshal(canonicalActivationRecord(t, s, next.ID).Canonical)
			if !bytes.Equal(exact, again) || requests.Load() != 2 || kind == "extend-budget" && complete.Checkpoint.Used != paused.Checkpoint.Used+1 {
				t.Fatal("action redelivery changed checkpoint or replayed")
			}
		})
	}
}

// Diagnostic witness for the released root-snapshot handoff interaction. It
// observes fixed metadata only, never provider bodies or private request data.
func TestAssistantCanonicalActivationNativeHandoffObservation(t *testing.T) {
	s, token, start, _ := assistantProductionFixture(t, "https://example.invalid", "work", "delegate")
	data, err := s.openChatData()
	assistantRunMust(t, err)
	path := filepath.Join(data.root.Name(), s.assistantRuns().name)
	assistantRunMust(t, data.root.Close())
	var mu sync.Mutex
	last := ""
	ledgerName := s.assistantRuns().name
	testHookPrivateRead = func(name string) error {
		if name != ledgerName {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		var ledger assistantRunLedger
		if json.Unmarshal(raw, &ledger) != nil {
			return nil
		}
		mu.Lock()
		defer mu.Unlock()
		for _, run := range ledger.Runs {
			if run.Canonical == nil || run.Checkpoint == nil || run.Canonical.Head.Revision == last {
				continue
			}
			last = run.Canonical.Head.Revision
			handoff := "absent"
			if run.Checkpoint.Handoff != nil {
				handoff = run.Checkpoint.Handoff.State
			}
			t.Logf("canonical commit=%d outer=%d status=%s handoff=%s", len(run.Canonical.Commits), run.Revision, run.Checkpoint.Status, handoff)
			if run.Checkpoint.Durable != nil {
				for _, row := range run.Checkpoint.Durable.Dispatches {
					task := ""
					if row.Scope.Task != nil {
						task = *row.Scope.Task
					}
					t.Logf("logical kind=%s tool=%s state=%s execution=%s role=%s task=%s", row.Kind, row.Tool, row.State, row.Scope.Execution, row.Scope.Role, task)
				}
			}
			for _, op := range run.Operations {
				t.Logf("physical id=%s kind=%s name=%s state=%s", op.ID, op.Kind, op.Name, op.State)
			}
		}
		return nil
	}
	defer func() { testHookPrivateRead = nil }()
	initial := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
	h := s.assistantRunService()
	h.mu.Lock()
	e := h.active[initial.ID]
	h.mu.Unlock()
	if e != nil {
		select {
		case <-e.done:
		case <-time.After(60 * time.Second):
			t.Fatal("bounded witness did not settle")
		}
	}
	record := canonicalActivationRecord(t, s, initial.ID)
	t.Logf("final outer=%s canonical=%s writer_error=%v", record.State, record.Checkpoint.Status, e.writer.Err())
	if !errors.Is(e.writer.Err(), errAssistantRunDispatch) {
		t.Fatal("expected exact retained dispatch refusal for witness")
	}
}

func TestAssistantCanonicalActivationProfileAndProjectionRefusals(t *testing.T) {
	store, _, _, _, record := canonicalActivationAdmitted(t)
	for _, mode := range []string{"required", "journal", "active", "head", "projection"} {
		t.Run(mode, func(t *testing.T) {
			raw, err := json.Marshal(record)
			assistantRunMust(t, err)
			var copy assistantRunRecord
			assistantRunMust(t, json.Unmarshal(raw, &copy))
			switch mode {
			case "required":
				copy.CanonicalRequired = false
			case "journal":
				copy.Canonical = nil
			case "active":
				copy.Canonical.Active = false
			case "head":
				copy.Canonical.Head.Checkpoint = nil
			case "projection":
				copy.Checkpoint.Status = "complete"
			}
			raw, err = json.Marshal(assistantRunLedger{Version: 1, Project: store.project, Runs: []assistantRunRecord{copy}})
			assistantRunMust(t, err)
			if _, err = decodeAssistantRunStorage(store.name, raw); err == nil {
				t.Fatal("missing/changed canonical profile or projection accepted")
			}
		})
	}
}
func TestAssistantCanonicalActivationCreationHostTransitions(t *testing.T) {
	var calls atomic.Int32
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		assistantBriefSSE(w, assistantAuthoringProposalFixture(json.RawMessage(assistantCreationCasesDocument)), "stop")
	})
	s, token := assistantCreationActualFixture(t, u.server.URL)
	recovered := assistantCreationActualRunWithin(t, s, token, assistantCreationAction{Kind: "recover", MessageID: "assistant-one", MessageDigest: digestOf([]byte(assistantAuthoringProposalFixture(json.RawMessage(reviewPack))))}, 1, 60*time.Second)
	first := canonicalActivationRecord(t, s, recovered.ID)
	canonicalActivationCheck(t, first)
	if calls.Load() != 0 || first.Checkpoint.Status != "complete" || first.Creation == nil {
		t.Fatal("model-free creation did not retain exact completion")
	}
	assistantCreationRetainDraft(t, s, token)
	reviewed := assistantCreationActualRunWithin(t, s, token, assistantCreationAction{Kind: "establish-cases", CandidateDigest: recovered.Creation.CandidateDigest}, 2, 60*time.Second)
	after := canonicalActivationRecord(t, s, reviewed.ID)
	canonicalActivationCheck(t, after)
	if calls.Load() != 1 || after.Checkpoint.Durable == nil || after.Checkpoint.Status != "complete" || after.ChatProjection == nil || !after.ChatProjection.Acknowledged {
		t.Fatal("review/model-free finalization lost canonical state")
	}
	messages := 0
	for _, event := range after.Events {
		if event.Event.Message != nil && event.Event.Message.Text == "Creation review retained. Checks do not authorize Save." {
			messages++
		}
	}
	if messages != 1 {
		t.Fatal("host notice reused stale binding or duplicated", messages)
	}
}

// A completion notice joins the host's own completing commit. Only a plain
// message with the complete status is accepted, before any source or ledger
// work, and it is stored as the root writer stores a handed event.
func TestAssistantCanonicalHostNoticeShape(t *testing.T) {
	store, binding, _, writer, record := canonicalActivationAdmitted(t)
	e := &assistantRunExecution{record: record, writer: writer}
	message := contract.RunEvent{Message: &contract.RunEventMessage{Type: "message", Text: "Creation review retained."}}
	end := contract.RunEvent{End: &contract.RunEventEnd{Type: "end"}}
	mixed := message
	mixed.End = end.End
	for name, call := range map[string]func() error{
		"running": func() error { return e.canonicalHostStatus(t.Context(), "running", message) },
		"end":     func() error { return e.canonicalHostStatus(t.Context(), "complete", end) },
		"mixed":   func() error { return e.canonicalHostStatus(t.Context(), "complete", mixed) },
		"empty":   func() error { return e.canonicalHostStatus(t.Context(), "complete", contract.RunEvent{}) },
	} {
		before, err := store.Read(t.Context(), binding, record.ID)
		assistantRunMust(t, err)
		if err := call(); !errors.Is(err, errAssistantRunInvalid) {
			t.Fatal(name, "notice accepted or refused for another reason", err)
		}
		after, err := store.Read(t.Context(), binding, record.ID)
		assistantRunMust(t, err)
		if after.Revision != before.Revision || !reflect.DeepEqual(after.Canonical, before.Canonical) {
			t.Fatal(name, "refused notice changed the run")
		}
	}
	encoded, err := assistantCanonicalHostEvent(record.Canonical.Head.RunID, message)
	assistantRunMust(t, err)
	value, err := runcontext.ParseJSON(encoded)
	assistantRunMust(t, err)
	envelope, err := runcontext.ReadStoredRunEvent(encoded)
	assistantRunMust(t, err)
	stamped, _ := json.Marshal(contract.StampEvent(message))
	held, _ := json.Marshal(envelope.Event)
	if runcontext.CanonicalJSON(value) != string(encoded) || envelope.Scope != (runcontext.DurableScope{Execution: record.Canonical.Head.RunID, Role: "author"}) || !bytes.Equal(stamped, held) {
		t.Fatal("host notice is not stored as the root writer stores an event", string(encoded))
	}
}
