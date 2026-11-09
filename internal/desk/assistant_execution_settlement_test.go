package desk

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
)

// The actual Manager launches copied test-owned bytes. No installed shell,
// interpreter, provider or model is used. The closed binary name plus native
// command arguments identify this private fixture before test flag parsing.
func init() {
	if filepath.Base(os.Args[0]) != "desk-native-settlement-peer" || len(os.Args) < 2 || os.Args[1] != "app-server" {
		return
	}
	send := func(v any) { raw, _ := json.Marshal(v); fmt.Println(string(raw)) }
	var tool string
	scan := bufio.NewScanner(os.Stdin)
	scan.Buffer(make([]byte, 4096), 2<<20)
	for scan.Scan() {
		var q struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scan.Bytes(), &q) != nil {
			os.Exit(81)
		}
		reply := func(v any) { send(map[string]any{"id": q.ID, "result": v}) }
		switch q.Method {
		case "initialize":
			reply(map[string]any{"userAgent": "jps_desk/0.157.1 (Linux)"})
		case "initialized":
		case "account/read":
			reply(map[string]any{"requiresOpenaiAuth": true, "account": map[string]string{"type": "chatgpt", "planType": "plus"}})
		case "model/list":
			var p struct {
				Hidden bool `json:"includeHidden"`
			}
			_ = json.Unmarshal(q.Params, &p)
			if p.Hidden {
				raw, err := os.ReadFile(filepath.Join(os.Getenv("CODEX_HOME"), "model-catalog.json"))
				if err != nil {
					os.Exit(82)
				}
				var catalog struct {
					Models []struct {
						Slug string `json:"slug"`
					} `json:"models"`
				}
				if json.Unmarshal(raw, &catalog) != nil {
					os.Exit(83)
				}
				rows := []map[string]any{}
				for _, m := range catalog.Models {
					rows = append(rows, map[string]any{"model": m.Slug})
				}
				reply(map[string]any{"data": rows})
			} else {
				reply(map[string]any{"data": []any{map[string]any{"model": "shared-model", "displayName": "Fixture", "defaultReasoningEffort": "medium", "supportedReasoningEfforts": []any{map[string]string{"reasoningEffort": "medium"}}}}})
			}
		case "thread/start":
			var p struct {
				Model string `json:"model"`
				Tools []struct {
					Name string `json:"name"`
				} `json:"dynamicTools"`
			}
			if json.Unmarshal(q.Params, &p) != nil || len(p.Tools) != 1 || p.Tools[0].Name != "desk_fixture_act" {
				os.Exit(84)
			}
			tool = p.Tools[0].Name
			cwd, _ := os.Getwd()
			reply(map[string]any{"thread": map[string]string{"id": "thread"}, "model": p.Model, "modelProvider": "openai", "approvalPolicy": "never", "cwd": cwd})
		case "turn/start":
			reply(map[string]any{"turn": map[string]string{"id": "turn"}})
			send(map[string]any{"id": "fixture-rpc", "method": "item/tool/call", "params": map[string]any{"threadId": "thread", "turnId": "turn", "callId": "fixture-call", "tool": tool, "arguments": map[string]any{"value": 7}}})
		case "":
			send(map[string]any{"method": "item/completed", "params": map[string]any{"threadId": "thread", "turnId": "turn", "item": map[string]string{"id": "message", "type": "agentMessage", "phase": "final_answer", "text": "Fixture response."}}})
			send(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "thread", "turn": map[string]string{"id": "turn", "status": "completed"}}})
		default:
			os.Exit(85)
		}
	}
	os.Exit(0)
}

type assistantNativeSettlementExecutor struct{ *assistantProtocolExecutor }

func (*assistantNativeSettlementExecutor) assistantCanonicalRuns() {}

type assistantNativeSettlementFixture struct {
	server     *Server
	token      string
	execution  *assistantRunExecution
	invocation context.Context
	binding    *runcontext.RunBinding
	agent      *adapter.Agent
	manager    *codexbridge.Manager
}

func assistantNativeSettlementSetup(t *testing.T) *assistantNativeSettlementFixture {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the actual native subscription Manager is supported on Linux")
	}
	type entered struct {
		execution *assistantRunExecution
		ctx       context.Context
	}
	active := make(chan entered, 1)
	s, token, start, base := assistantProtocolFixture(t, func(ctx context.Context, e *assistantRunExecution) error {
		active <- entered{e, ctx}
		<-ctx.Done()
		return ctx.Err()
	})
	off := false
	base.review = &off
	s.assistantRunExecutor = &assistantNativeSettlementExecutor{base}
	connection := aiTestConnection(testAIAlpha, "Native settlement fixture", "https://example.invalid")
	model := "shared-model"
	connection.Assistant.Engine = "codex"
	connection.Assistant.Endpoint = nil
	connection.Assistant.Agent = &AssistantAgentConfig{Model: &model, Provider: "openai", AuthMethod: "subscription", Models: []string{model}, Tools: []string{}, Effort: "medium", ChildModel: model, CriticModel: model}
	raw, err := encodeAIRegistry(aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{connection}})
	assistantRunMust(t, err)
	assistantRunMust(t, s.assistant.writeConfigNamed(aiConnectionsFile, raw, nil))
	fixture := t.TempDir()
	binary := filepath.Join(fixture, "desk-native-settlement-peer")
	exe, err := os.Executable()
	assistantRunMust(t, err)
	raw, err = os.ReadFile(exe)
	assistantRunMust(t, err)
	assistantRunMust(t, os.WriteFile(binary, raw, 0700))
	manager, err := codexbridge.NewManager(codexbridge.Options{Binary: binary, ProfileDir: filepath.Join(fixture, "account"), ProjectDir: s.projectDir})
	assistantRunMust(t, err)
	s.codex = manager
	s.aiAccounts.values = map[string]providerAccountManager{testAIAlpha: manager}
	assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
	var run entered
	select {
	case run = <-active:
	case <-time.After(5 * time.Second):
		t.Fatal("service owner did not enter")
	}
	e := run.execution
	_, err = e.prepareAssistantInput(run.ctx)
	assistantRunMust(t, err)
	agent, err := e.assistantAgent()
	assistantRunMust(t, err)
	f := &assistantNativeSettlementFixture{s, token, e, run.ctx, e.canonicalBinding, agent, manager}
	t.Cleanup(func() {
		e.cancel()
		select {
		case <-e.done:
		case <-time.After(6 * time.Second):
			t.Error("owner did not retire")
		}
		manager.Close()
	})
	return f
}

type assistantNativeSettlementRun struct {
	done                chan error
	entered             chan struct{}
	released            chan struct{}
	returned            chan struct{}
	resume              chan struct{}
	cancel              context.CancelFunc
	stop                *assistantAgentStop
	releaseProof        func() bool
	callbackOperation   string
	calls, releaseCalls atomic.Int32
	once                sync.Once
}

func (r *assistantNativeSettlementRun) allowReturn() { r.once.Do(func() { close(r.resume) }) }

// This fixture drives production assistantAgent through the real Manager and
// its actual callback journal. The pause is handwritten through the supported
// original package writer. It is a bounded journal seam proof, not actual full
// application contextual-factory/Gate/Runtime enrollment qualification.
func (f *assistantNativeSettlementFixture) run(t *testing.T, mode string) *assistantNativeSettlementRun {
	t.Helper()
	r := &assistantNativeSettlementRun{done: make(chan error, 1), entered: make(chan struct{}), released: make(chan struct{}), returned: make(chan struct{}), resume: make(chan struct{})}
	r.stop = &assistantAgentStop{parent: f.invocation}
	provider, cancel := context.WithCancel(context.WithValue(f.invocation, assistantAgentStopKey{}, r.stop))
	r.cancel = cancel
	release, finish := adapter.ProviderReleaseAttempt()
	r.releaseProof = finish
	acknowledge := release.Acknowledge
	release.Acknowledge = func(id *adapter.ProviderReleaseInvocation) bool {
		r.releaseCalls.Add(1)
		ok := false
		if mode != "release-withheld" {
			ok = acknowledge(id)
		}
		close(r.released)
		return ok
	}
	callbacks := adapter.AgentCallbacks{ProviderRelease: release, Event: func(context.Context, adapter.AgentEvent) error { return nil }, Tool: func(callCtx context.Context, call adapter.AgentToolCall) (adapter.AgentToolAnswer, error) {
		defer close(r.returned)
		r.calls.Add(1)
		r.callbackOperation, _ = callCtx.Value(assistantHostCallbackKey{}).(string)
		if mode != "ordinary-cancel" {
			cp := assistantProtocolAsked(*f.binding.Writer.Checkpoint())
			cp.Status = "paused"
			cp.Approval.CallID = call.ID
			cp.Approval.Name = "write_fixture"
			cp.Approval.Tool = "write_fixture"
			cp.Approval.Args = map[string]json.RawMessage{"value": json.RawMessage(`7`)}
			if err := f.binding.Writer.Save(callCtx, *f.binding, cp); err != nil {
				return adapter.AgentToolAnswer{}, err
			}
			r.stop.observe(contract.RunEvent{ApprovalRequested: &contract.RunEventApprovalRequested{CallID: call.ID}})
		}
		close(r.entered)
		cancel()
		<-r.released // actual Manager process cleanup, separately from callback join
		<-r.resume
		return adapter.AgentToolAnswer{Text: "The exact call awaits an explicit decision."}, context.Canceled
	}}
	request := adapter.AgentRequest{Model: "shared-model", Prompt: "Exact bounded native settlement fixture.", Instructions: "Fixture data only.", Effort: "medium", Phase: "author", Tools: []adapter.AgentTool{{Name: "desk_fixture_act", Description: "write_fixture: exact journal fixture", InputSchema: json.RawMessage(`{"type":"object","properties":{"value":{"type":"integer"}},"additionalProperties":false}`)}}}
	// Agent v0.13.2 runs a native provider call inside its route's durable
	// claim (InvokeNativeRoute: a read "agent" dispatch for the request's
	// model and exact request), acknowledged before the host is entered; an
	// approval stop is qualified against that open claim. Make the call
	// inside the same claim, as the native adapter does.
	intent, err := json.Marshal(request)
	assistantRunMust(t, err)
	go func() {
		_, err := runcontext.InvokeDurable(provider, f.binding, "agent", "fixture-native-provider", request.Model, intent, true, func([]byte) (struct{}, error) {
			return struct{}{}, f.agent.Run(provider, request, callbacks)
		}, func(struct{}) bool { return true }, nil)
		r.done <- err
	}()
	t.Cleanup(func() {
		cancel()
		r.allowReturn()
		select {
		case <-r.returned:
		case <-time.After(6 * time.Second):
			t.Error("callback did not return")
		}
	})
	select {
	case <-r.entered:
	case err := <-r.done:
		t.Fatal("Manager returned before callback", err)
	case <-time.After(5 * time.Second):
		t.Fatal("actual Manager did not call fixture")
	}
	select {
	case <-r.released:
	case <-time.After(5 * time.Second):
		t.Fatal("actual Manager cleanup did not acknowledge release")
	}
	return r
}
func assistantNativeSettlementOutcome(t *testing.T, e *assistantRunExecution, kind string) (assistantRunOperation, assistantAgentOutcome) {
	t.Helper()
	run, err := e.writer.lease.store.Read(t.Context(), e.record.Binding, e.record.ID)
	assistantRunMust(t, err)
	for _, op := range run.Operations {
		if op.Kind == kind {
			var outcome assistantAgentOutcome
			if len(op.Result) > 0 {
				assistantRunMust(t, json.Unmarshal(op.Result, &outcome))
			}
			return op, outcome
		}
	}
	t.Fatal("missing observed operation", kind)
	return assistantRunOperation{}, assistantAgentOutcome{}
}

func TestAssistantNativeSettlementActualManager(t *testing.T) {
	f := assistantNativeSettlementSetup(t)
	r := f.run(t, "approval")
	select {
	case err := <-r.done:
		t.Fatal("process release alone joined callback", err)
	default:
	}
	if f.execution.ctx.Err() != nil {
		t.Fatal("provider cancellation closed owner before callback joined")
	}
	r.allowReturn()
	select {
	case err := <-r.done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("native cancellation changed", err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("native callback did not join")
	}
	if !r.releaseProof() || r.releaseCalls.Load() != 1 || r.calls.Load() != 1 || !r.stop.acknowledged() {
		t.Fatal("release/callback/receipt proof missing")
	}
	for _, kind := range []string{"callback", "model"} {
		op, outcome := assistantNativeSettlementOutcome(t, f.execution, kind)
		if op.State != "completed" || outcome.Stop != "approval" || outcome.Failure != nil {
			t.Fatal("known stop receipt lost", kind, op.State, outcome)
		}
	}
	if f.execution.ctx.Err() != nil {
		t.Fatal("acknowledged approval cancelled original owner")
	}
	f.execution.canonicalMu.Lock()
	state := f.execution.lifecycle
	proof := f.execution.settlement
	f.execution.canonicalMu.Unlock()
	if state != "running" || proof != nil {
		t.Fatal("P3a activated parked continuation", state)
	}
}

func TestAssistantNativeSettlementUnknownReleaseAndCancellation(t *testing.T) {
	for _, mode := range []string{"release-withheld", "ordinary-cancel"} {
		if !t.Run(mode, func(t *testing.T) {
			f := assistantNativeSettlementSetup(t)
			r := f.run(t, mode)
			r.allowReturn()
			select {
			case err := <-r.done:
				if err == nil {
					t.Fatal("cancellation became success")
				}
			case <-time.After(6 * time.Second):
				t.Fatal("native invocation did not finish")
			}
			op, outcome := assistantNativeSettlementOutcome(t, f.execution, "model")
			if op.State != "completed" || outcome.Stop != "" || outcome.Failure == nil || outcome.Failure.Outcome != "unknown" {
				t.Fatal("unknown native outcome was relabelled", op.State, outcome)
			}
			if mode == "release-withheld" && r.releaseProof() {
				t.Fatal("withheld acknowledgement became released")
			}
			if f.execution.ctx.Err() == nil {
				t.Fatal("unknown invocation retained live owner")
			}
		}) {
			return
		}
	}
}

func TestAssistantNativeSettlementJoinTimeoutAndRevocation(t *testing.T) {
	for _, mode := range []string{"timeout", "session"} {
		if !t.Run(mode, func(t *testing.T) {
			f := assistantNativeSettlementSetup(t)
			r := f.run(t, mode)
			if mode == "session" {
				f.server.sessions.revoke(f.token)
			}
			select {
			case err := <-r.done:
				if err == nil {
					t.Fatal("unjoined callback became success")
				}
			case <-time.After(7 * time.Second):
				t.Fatal("callback join exceeded owner bound")
			}
			if f.execution.ctx.Err() == nil {
				t.Fatal("join failure left owner live")
			}
			r.allowReturn()
			select {
			case <-r.returned:
			case <-time.After(6 * time.Second):
				t.Fatal("late callback did not return")
			}
			// Result observations retain their original cancellation behavior. Wait for
			// this exact late callback's bounded receipt before fixture teardown.
			deadline := time.Now().Add(6 * time.Second)
			for {
				record, err := f.execution.writer.lease.store.Read(t.Context(), f.execution.record.Binding, f.execution.record.ID)
				assistantRunMust(t, err)
				retained := false
				for _, op := range record.Operations {
					if op.ID == r.callbackOperation && len(op.Result) > 0 {
						var answer assistantAgentOutcome
						assistantRunMust(t, json.Unmarshal(op.Result, &answer))
						if answer.Stop != "" || answer.Failure == nil || answer.Failure.Outcome != "unknown" {
							t.Fatal("late receipt became a known approval", answer)
						}
						retained = true
					}
				}
				if retained || f.execution.writer.Err() != nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("late observed receipt did not settle")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if r.stop.acknowledged() {
				t.Fatal("late callback acknowledged a safe pause")
			}
			model, outcome := assistantNativeSettlementOutcome(t, f.execution, "model")
			if model.State == "completed" && (outcome.Stop != "" || outcome.Failure == nil) {
				t.Fatal("unjoined model outcome became successful", model.State, outcome)
			}
		}) {
			return
		}
	}
}

func TestAssistantNativeSettlementExactScopeAndProof(t *testing.T) {
	f := assistantNativeSettlementSetup(t)
	for _, mode := range []string{"generation", "binding", "receipt-error", "unreturned-callback", "duplicate-release", "late-callback"} {
		if !t.Run(mode, func(t *testing.T) {
			scope := newAssistantNativeSettlement(f.invocation, f.execution)
			switch mode {
			case "generation":
				scope.generation++
				if scope.fixedScope(f.invocation) == nil {
					t.Fatal("stale generation accepted")
				}
			case "binding":
				copy := *scope.binding
				scope.binding = &copy
				if scope.fixedScope(f.invocation) == nil {
					t.Fatal("different binding accepted")
				}
			case "receipt-error", "unreturned-callback", "duplicate-release":
				token, err := scope.begin("fixture-call")
				assistantRunMust(t, err)
				assistantRunMust(t, scope.admit(token, assistantRunOperation{ID: "fixture-operation", Kind: "callback"}))
				if mode != "unreturned-callback" {
					scope.receipt(token, true, nil)
				}
				if mode == "receipt-error" {
					scope.receipt(token, true, errors.New("fixture unknown receipt acknowledgement"))
				}
				scope.end(token)
				scope.release(true)
				if mode == "duplicate-release" {
					scope.release(true)
				}
				assistantRunMust(t, scope.closeAndJoin(f.execution.ctx))
				if scope.approvalSettled() {
					t.Fatal("incomplete or invalid evidence qualified approval", mode)
				}
			case "late-callback":
				assistantRunMust(t, scope.closeAndJoin(f.execution.ctx))
				if _, err := scope.begin("late"); err == nil {
					t.Fatal("late callback admitted")
				}
			}
		}) {
			return
		}
	}
}

// Callbacks that have all joined have joined even when the owner was canceled
// meanwhile, as a refused callback cancels it: the join is never reported as
// timed out at random.
func TestAssistantNativeSettlementJoinedBeforeOwnerCancel(t *testing.T) {
	owner, cancel := context.WithCancel(t.Context())
	cancel()
	for i := 0; i < 200; i++ {
		s := &assistantNativeSettlement{joined: make(chan struct{}), callbacks: map[*assistantNativeCallback]struct{}{}}
		if err := s.closeAndJoin(owner); err != nil {
			t.Fatal("joined callbacks reported as not joined", i, err)
		}
		if s.aborted || !s.joinedClosed {
			t.Fatal("joined settlement marked aborted")
		}
	}
	// A callback that has not joined still bounds the wait by the owner.
	s := &assistantNativeSettlement{joined: make(chan struct{}), callbacks: map[*assistantNativeCallback]struct{}{{}: {}}}
	if err := s.closeAndJoin(owner); err == nil || !s.aborted {
		t.Fatal("an unjoined callback did not abort under a canceled owner", err)
	}
}
