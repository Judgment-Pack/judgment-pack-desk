package desk

// Prerequisite only: published adapters and the original private Desk RunStore.
// This does not exercise the production lifetime registry, HTTP start, actual
// provider transport, delegated coordinator or critic workflow.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	native "github.com/Judgment-Pack/judgment-pack-agent/go/adapter/agent"
	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter/eino"
	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter/wire"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/gate"
	"github.com/Judgment-Pack/judgment-pack-agent/go/relay"
)

func TestAssistantChatContextOriginalStoreContract(t *testing.T) {
	for _, engine := range []string{"api", "native"} {
		if !t.Run(engine, func(t *testing.T) { assistantChatContextContract(t, engine) }) {
			return // Preserve the first failed prerequisite; do not continue probes.
		}
	}
}

type assistantChatContextProbe struct {
	store     assistantRunStore
	base      assistantRunBinding
	lease     *assistantRunLease
	engine    string
	immutable atomic.Int32
	mutable   atomic.Int32
	writes    atomic.Int32
	revision  string
	sequence  int
}

type assistantChatContextTurn struct {
	p          *assistantChatContextProbe
	chat       *runcontext.ChatContext
	runtime    *runcontext.RuntimeIdentity
	g          *gate.Gate
	writer     *assistantRunWriter
	port       *assistantCanonicalRunStore
	record     assistantRunRecord
	input      *runcontext.Input
	tools      []runcontext.HostTool
	turnNumber int
	eventsMu   sync.Mutex
	events     []json.RawMessage
}

func assistantChatContextContract(t *testing.T, engine string) {
	store, binding, _ := assistantRunFixture(t)
	lease, err := store.Acquire(t.Context())
	assistantRunMust(t, err)
	defer lease.Close()
	p := &assistantChatContextProbe{store: store, base: binding, lease: lease, engine: engine, revision: "workspace-v1"}
	chat := runcontext.NewChatContext("")
	defer chat.Dispose()
	identity := &runcontext.RuntimeIdentity{Identity: "immutable-runtime-artifact-a", Version: "fixture-1"}
	first := p.admit(t, chat, identity)
	first.execute(t, false, false)
	firstHead := first.saved(t)
	if firstHead.Checkpoint.Status != "complete" || p.immutable.Load() != 1 || p.mutable.Load() != 1 || len(chat.Entries()) == 0 {
		t.Fatalf("first independently admitted turn did not retain its reads: status=%q immutable=%d mutable=%d chatEntries=%d checkpointScope=%q chatScope=%q", firstHead.Checkpoint.Status, p.immutable.Load(), p.mutable.Load(), len(chat.Entries()), firstHead.Checkpoint.Context.Scope, chat.Scope())
	}
	entry := chat.Entries()[0].ID
	p.revision = "workspace-v2"
	second := p.admit(t, chat, identity)
	if second.record.ID == first.record.ID || second.port == first.port || second.writer == first.writer || second.record.Binding.Owner.ID != first.record.Binding.Owner.ID {
		t.Fatal("second turn did not have separate admission and original store authority for the same owner")
	}
	second.execute(t, false, false)
	if p.immutable.Load() != 1 || p.mutable.Load() != 2 {
		t.Fatalf("cross-turn immutable reuse / mutable freshness: immutable=%d mutable=%d", p.immutable.Load(), p.mutable.Load())
	}
	second.assertVisible(t, entry, true)
	if !bytes.Contains(second.saved(t).Canonical.Head.Checkpoint, []byte("workspace-v2")) {
		t.Fatal("fresh mutable observation absent from acknowledged second checkpoint")
	}
	// Pause a third admitted turn after completed observations, then resume using
	// the exact original Desk store object, run id, Desk writer and live lease.
	paused := p.admit(t, chat, identity)
	paused.execute(t, true, false)
	before := paused.saved(t)
	if before.Checkpoint.Approval == nil || before.Checkpoint.Approval.State != "asked" || p.writes.Load() != 0 {
		t.Fatal("approval pause did not precede the physical operation")
	}
	completed := []contract.CheckpointOperation{}
	for _, op := range before.Checkpoint.Operations {
		if op.Status == "complete" {
			completed = append(completed, op)
		}
	}
	if len(completed) < 2 {
		t.Fatal("completed observations were not retained before pause")
	}
	oldBinding := paused.input.Persistence
	paused.input, err = runcontext.PrepareDurableInput(t.Context(), &runcontext.Input{
		Chat: chat, Runtime: identity, Connection: binding.ConnectionID,
		CurrentRequest: &runcontext.CurrentRequest{Message: paused.message(), Input: paused.message()},
		RunStore:       &runcontext.DurableRunInput{Store: paused.port, ID: oldBinding.Writer.ID, Resume: true},
		Approval:       &contract.CheckpointApprovalDecision{CallID: before.Checkpoint.Approval.CallID, Approved: true},
	})
	assistantRunMust(t, err)
	if paused.input.Persistence == oldBinding || paused.input.Persistence.Writer.Store != paused.port || paused.port.writer != paused.writer || paused.writer.lease != lease {
		t.Fatal("fresh public preparation replaced original durable authority")
	}
	readsBefore := p.mutable.Load()
	paused.execute(t, false, true)
	after := paused.saved(t)
	if after.Checkpoint.Status != "complete" || p.writes.Load() != 1 || p.mutable.Load() != readsBefore || len(after.Operations) != 1 || after.Operations[0].State != "completed" || string(after.Operations[0].Result) != `{"missing":null,"fact":false}` {
		t.Fatal("resume replayed observations, lost exact physical receipt or failed to complete")
	}
	for _, wanted := range completed {
		found := false
		for _, got := range after.Checkpoint.Operations {
			if got.ID == wanted.ID {
				found = reflect.DeepEqual(wanted, got)
			}
		}
		if !found {
			t.Fatal("resume changed completed operation identity, arguments or result", wanted.ID)
		}
	}
	other := runcontext.NewChatContext("")
	defer other.Dispose()
	isolated := p.admit(t, other, identity)
	isolated.assertVisible(t, entry, false)
	isolated.execute(t, false, false)
	if p.immutable.Load() != 2 {
		t.Fatal("another chat reused the first chat's immutable observation")
	}
	changed := p.admit(t, chat, &runcontext.RuntimeIdentity{Identity: "immutable-runtime-artifact-b", Version: "fixture-1"})
	changed.execute(t, false, false)
	if p.immutable.Load() != 3 {
		t.Fatal("changed Runtime identity reused stale immutable observation")
	}
	// These public scope controls qualify the narrowing primitive, not actual
	// delegated or critic workflow integration.
	for _, visible := range []bool{false, true} {
		refs := map[string]bool{}
		if visible {
			refs[entry] = true
		}
		child, err := chat.ChildScope(refs).OpenTurn()
		assistantRunMust(t, err)
		if (child.Use(entry) != nil) != visible {
			t.Fatal("child read outside explicit references")
		}
		child.Release()
	}
	critic, err := runcontext.ContextualSession(t.Context(), runcontext.Session{Prompt: "Explicit review", Model: &runcontext.ModelSession{Family: "openai-compatible", Model: "fixture", Thinking: "off"}, Critic: "off", Context: &runcontext.Input{Chat: chat, CurrentRequest: &runcontext.CurrentRequest{Message: "Review", Input: "Only explicit review facts"}}})
	assistantRunMust(t, err)
	if _, err = critic.Context.Ledger.Read(runcontext.ObjectOf("id", runcontext.String(entry))); err != nil {
		t.Fatal("author retrieval prerequisite missing", err)
	}
	critic.Context.BeginIsolatedPhase()
	if critic.Context.Ledger.Scope() == chat.Scope() {
		t.Fatal("critic kept author's retrieval scope")
	}
	if _, err = critic.Context.Ledger.Read(runcontext.ObjectOf("id", runcontext.String(entry))); err == nil {
		t.Fatal("critic reached author's retained entry")
	}
	critic.Context.Close()
	chat.Dispose()
	if _, err := chat.OpenTurn(); err == nil || len(chat.Entries()) != 0 {
		t.Fatal("disposed chat retained or admitted context")
	}
	t.Logf("prerequisite %s: two separate admitted turns; original Desk store continuation; immutable calls=%d mutable calls=%d physical writes=%d; no production lifetime or complete workflow claim", engine, p.immutable.Load(), p.mutable.Load(), p.writes.Load())
}

func (p *assistantChatContextProbe) admit(t *testing.T, chat *runcontext.ChatContext, identity *runcontext.RuntimeIdentity) *assistantChatContextTurn {
	t.Helper()
	p.sequence++
	x := &assistantChatContextTurn{p: p, chat: chat, runtime: identity, turnNumber: p.sequence}
	description := "Read the immutable schema"
	// An offer does not grant Runtime transport authority. Grant only the read
	// exercised here, independently of the served tool declaration below.
	x.g = gate.New(gate.Options{Granted: []string{"get_schema"}, Emit: x.collectEvent, Runtime: func(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
		var q struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.Unmarshal(raw, &q); err != nil {
			return nil, err
		}
		if q.Method != "tools/call" {
			return nil, fmt.Errorf("unexpected fixture Runtime request %s", q.Method)
		}
		p.immutable.Add(1)
		return json.Marshal(map[string]any{"jsonrpc": "2.0", "id": q.ID, "result": map[string]any{"content": []map[string]string{{"type": "text", "text": "immutable-schema-v1"}}}})
	}})
	x.g.SetOffer(contract.ToolOffer{Tools: []contract.ToolOfferRuntimeTool{{Name: "get_schema", Description: &description, InputSchema: json.RawMessage(`{"type":"object","properties":{}}`), Meta: contract.ToolTaxonomy{Operation: "read", Effect: "read"}}}})
	x.tools = []runcontext.HostTool{{Name: "read_workspace", Description: "Read the current mutable workspace", Effect: "read", Taxonomy: &runcontext.ToolTaxonomy{Operation: "read", Effect: "read"}, InputSchema: json.RawMessage(`{"type":"object","properties":{}}`), Execute: func(context.Context, json.RawMessage) (runcontext.ToolResult, error) {
		p.mutable.Add(1)
		return runcontext.ResultOf([]runcontext.Content{runcontext.TextContent(p.revision)}, nil), nil
	}}}
	// This fixed literal pins the hand-authored source declaration, independent
	// of ToolListDigest, the implementation under test.
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(`[{"description":"Finish one exact note","inputSchema":{"properties":{},"type":"object"},"name":"finish_note","taxonomy":{"effect":"act","operation":"act"}}]`)))
	x.tools = append(x.tools, runcontext.HostTool{Name: "finish_note", Description: "Finish one exact note", Taxonomy: &runcontext.ToolTaxonomy{Operation: "act", Effect: "act"}, EffectClass: "act", Approval: "required", InputSchema: json.RawMessage(`{"type":"object","properties":{}}`), Source: &runcontext.ToolSource{Kind: "custom", ID: "notes", Trust: "person", Version: "1", ToolListDigest: digest, CurrentDigest: func(context.Context) (string, error) { return digest, nil }}, Execute: func(ctx context.Context, _ json.RawMessage) (runcontext.ToolResult, error) {
		p.writes.Add(1)
		op := assistantRunOperation{ID: "physical-note", CallID: "physical-note-call", Kind: "host", Name: "finish_note", Intent: []byte(`{}`)}
		if err := x.writer.Intent(ctx, op); err != nil {
			return runcontext.ToolResult{}, err
		}
		if err := x.writer.Dispatch(ctx, op.ID); err != nil {
			return runcontext.ToolResult{}, err
		}
		if err := x.writer.Result(ctx, op.ID, "completed", []byte(`{"missing":null,"fact":false}`)); err != nil {
			return runcontext.ToolResult{}, err
		}
		return runcontext.Answer(runcontext.MustParseJSON(`{"missing":null,"fact":false}`)), nil
	}})
	x.input = &runcontext.Input{Chat: chat, Runtime: identity, Connection: p.base.ConnectionID, CurrentRequest: &runcontext.CurrentRequest{Message: x.message(), Input: x.message()}}
	session := runcontext.Session{Prompt: "Inspect exact current facts", Context: x.input, RuntimeCalls: x.g.Calls(), HostTools: x.tools, AllowConversation: true, Interactive: true, Critic: "off"}
	for _, tool := range x.g.Offer().Tools {
		session.Tools = append(session.Tools, runcontext.RuntimeTool{Name: tool.Name, Meta: &runcontext.ToolTaxonomy{Operation: tool.Meta.Operation, Effect: tool.Meta.Effect}, Offer: &tool})
	}
	// Preparation and execution must resolve the same host-authored policy.
	// Both published adapters supply the named models and actual connection
	// families; omitting them here changes the saved policy before any reads.
	policyRun := &adapter.Run{ModelName: "fixture", Wire: "openai-compatible", Context: x.input}
	named := runcontext.RunModels{Critic: "fixture", Child: "fixture"}
	if p.engine == "native" {
		policyRun.Agent = &adapter.Agent{Model: "fixture", Effort: "medium"}
		session.Agent = &runcontext.AgentSession{Model: policyRun.Agent.Model, Effort: policyRun.Agent.Effort}
	} else {
		capabilities, err := wire.CapabilitiesFor(wire.EndpointKind(policyRun.Wire), policyRun.ModelName, nil, nil)
		assistantRunMust(t, err)
		session.Model = &runcontext.ModelSession{Family: policyRun.Wire, Model: policyRun.ModelName, Thinking: "off", ToolSchemas: string(capabilities.ToolSchemas)}
		if capabilities.ContextWindowTokens != nil && capabilities.OutputTokens != nil {
			session.Model.Budget = &runcontext.ModelBudget{ContextWindowTokens: *capabilities.ContextWindowTokens, OutputTokens: *capabilities.OutputTokens}
		}
	}
	session.PolicyLegacyModels = &named
	session.PolicyFamilies = adapter.PolicyFamilies(policyRun, named)
	session.PolicyConnections = append(adapter.PolicyConnectionIDs(policyRun), named.Critic, named.Child)
	opened, err := runcontext.ContextualSession(t.Context(), session)
	assistantRunMust(t, err)
	cp, err := opened.Context.Snapshot("")
	opened.Context.Close()
	assistantRunMust(t, err)
	if cp.Context.Scope != chat.Scope() {
		t.Fatal("initial checkpoint omitted the original chat scope")
	}
	binding := p.base
	binding.RequestID = x.message()
	binding.RequestDigest = digestOf([]byte(x.message()))
	binding.Owner.Revision = digestOf([]byte(x.message()))
	binding.ExecutionKey = cp.RequestKey
	binding.Unit = cp.Unit
	binding.Phase = cp.Phase
	x.record, _, err = p.lease.Admit(t.Context(), binding, x.message(), digestOf([]byte(x.message())), func() error { return nil })
	assistantRunMust(t, err)
	x.writer, err = p.lease.Writer(t.Context(), binding, x.record.ID)
	assistantRunMust(t, err)
	assistantRunMust(t, x.writer.initializeCanonical(t.Context(), cp))
	x.port, err = x.writer.canonicalStore(t.Context(), cp.ID)
	assistantRunMust(t, err)
	x.input.RunStore = &runcontext.DurableRunInput{Store: x.port, ID: cp.ID, Resume: true}
	x.input, err = runcontext.PrepareDurableInput(t.Context(), x.input)
	assistantRunMust(t, err)
	t.Logf("admitted engine=%s turn=%d initialScope=%q restoredScope=%q chatScope=%q granted=%v runtimeOffer=%v", p.engine, x.turnNumber, cp.Context.Scope, x.input.Execution.Context.Scope, chat.Scope(), x.g.AllowList(), x.g.Offer().Tools)
	t.Cleanup(func() {
		if t.Failed() {
			x.eventsMu.Lock()
			defer x.eventsMu.Unlock()
			for i, event := range x.events {
				t.Logf("synthetic event engine=%s turn=%d index=%d: %s", p.engine, x.turnNumber, i, event)
			}
		}
	})
	return x
}

func (x *assistantChatContextTurn) message() string {
	return fmt.Sprintf("context-turn-%d", x.turnNumber)
}

func (x *assistantChatContextTurn) saved(t *testing.T) assistantRunRecord {
	t.Helper()
	r, err := x.p.store.Read(t.Context(), x.record.Binding, x.record.ID)
	assistantRunMust(t, err)
	assistantRunMust(t, validateAssistantCanonicalProjection(r))
	if x.port.writer != x.writer || x.writer.lease != x.p.lease || r.Canonical.Head.RunID != x.input.Persistence.Writer.ID {
		t.Fatal("original store acknowledgement identity changed")
	}
	return r
}

func (x *assistantChatContextTurn) assertVisible(t *testing.T, id string, want bool) {
	t.Helper()
	turn, err := x.chat.OpenTurn()
	assistantRunMust(t, err)
	defer turn.Release()
	if (turn.Use(id) != nil) != want {
		t.Fatal("chat scope retrieval isolation failed", want)
	}
}

func (x *assistantChatContextTurn) execute(t *testing.T, pause, resume bool) {
	t.Helper()
	run := &adapter.Run{Prompt: "Inspect exact current facts", Wire: "openai-compatible", ModelName: "fixture", Thinking: "off", Critic: "off", Gate: x.g.Calls(), Tools: x.g.Offer().Tools, HostTools: x.tools, AllowConversation: true, Interactive: true, Context: x.input}
	// The original RunStore owns the canonical journal; this diagnostic sink
	// must not append emitted events to the Desk writer a second time.
	run.Emit = x.collectEvent
	run.EnrollContext = func(_ context.Context, offer adapter.ContextOffer) error {
		return x.g.EnrollContext(offer.ID, offer.HostTools)
	}
	names := []string{"get_schema", "read_workspace"}
	if resume {
		names = nil
	} else if pause {
		names = append(names, "notes__finish_note")
	}
	var implementation adapter.Adapter
	if x.p.engine == "native" {
		implementation = native.New()
		run.Agent = &adapter.Agent{Model: "fixture", Effort: "medium", Run: func(ctx context.Context, request adapter.AgentRequest, cb adapter.AgentCallbacks) error {
			defer func() {
				if cb.ProviderRelease != nil && !cb.ProviderRelease.Acknowledge(cb.ProviderRelease.Invocation) {
					t.Error("scripted native provider release was not accepted")
				}
			}()
			offered := make([]string, 0, len(request.Tools))
			for _, tool := range request.Tools {
				offered = append(offered, tool.Name+"="+tool.Description)
			}
			t.Logf("scripted native turn=%d offered=%v requested=%v", x.turnNumber, offered, names)
			for i, name := range names {
				wireName := name
				for _, tool := range request.Tools {
					if strings.HasPrefix(tool.Description, name+":") {
						wireName = tool.Name
					}
				}
				answer, err := cb.Tool(ctx, adapter.AgentToolCall{ID: fmt.Sprintf("call-%d", i), Name: wireName, Arguments: json.RawMessage(`{}`)})
				if err != nil {
					return err
				}
				if answer.IsError {
					return fmt.Errorf("fixture tool %s refused: %s", name, answer.Text)
				}
			}
			return cb.Event(ctx, adapter.AgentEvent{Type: "message", Text: "Done.", Phase: "final"})
		}}
	} else {
		implementation = eino.New()
		requests := 0
		run.Model = func(_ context.Context, _ string, request relay.Request) (*http.Response, error) {
			requests++
			if requests > 2 {
				return nil, fmt.Errorf("unexpected third scripted model request")
			}
			if requests == 1 && len(names) > 0 {
				var body struct {
					Tools []struct {
						Function struct{ Name, Description string } `json:"function"`
					} `json:"tools"`
				}
				if err := json.Unmarshal(request.Body, &body); err != nil {
					return nil, err
				}
				offered := make([]string, 0, len(body.Tools))
				for _, tool := range body.Tools {
					offered = append(offered, tool.Function.Name)
				}
				t.Logf("scripted API turn=%d request=%d offered=%v requested=%v", x.turnNumber, requests, offered, names)
				calls := []any{}
				for i, name := range names {
					wireName := name
					for _, tool := range body.Tools {
						if strings.HasPrefix(tool.Function.Description, name+":") {
							wireName = tool.Function.Name
						}
					}
					calls = append(calls, map[string]any{"index": i, "id": fmt.Sprintf("call-%d", i), "type": "function", "function": map[string]any{"name": wireName, "arguments": "{}"}})
				}
				return assistantChatContextSSE(map[string]any{"tool_calls": calls}, "tool_calls"), nil
			}
			return assistantChatContextSSE(map[string]any{"content": "Done."}, "stop"), nil
		}
	}
	assistantRunMust(t, implementation.Run(t.Context(), run))
	cp := x.input.Persistence.Writer.Checkpoint()
	// Like the owning service, settle the host envelope after the canonical
	// checkpoint is acknowledged. A complete checkpoint alone retains the owner.
	state := "complete"
	if pause {
		if cp.Status != "paused" || cp.Approval == nil || cp.Approval.State != "asked" {
			t.Fatalf("expected acknowledged approval pause before host settlement: status=%q approval=%v", cp.Status, cp.Approval)
		}
		state = "awaiting-input"
	} else if cp.Status != "complete" {
		t.Fatalf("expected acknowledged completion before host settlement: status=%q", cp.Status)
	}
	assistantRunMust(t, x.writer.State(t.Context(), state))
	t.Logf("executed engine=%s turn=%d pause=%t resume=%t status=%q immutable=%d mutable=%d writes=%d chatEntries=%d checkpointScope=%q chatScope=%q operations=%v", x.p.engine, x.turnNumber, pause, resume, cp.Status, x.p.immutable.Load(), x.p.mutable.Load(), x.p.writes.Load(), len(x.chat.Entries()), cp.Context.Scope, x.chat.Scope(), cp.Operations)
}

// Keep synthetic transport and gate observations for the first failed prerequisite.
// Checkpoint payloads are summarized to avoid duplicating their retained contents.
func (x *assistantChatContextTurn) collectEvent(event contract.RunEvent) {
	var value any = event
	if checkpoint := event.ExecutionCheckpoint; checkpoint != nil {
		value = map[string]any{"type": "execution_checkpoint", "status": checkpoint.Checkpoint.Status, "scope": checkpoint.Checkpoint.Context.Scope, "operations": checkpoint.Checkpoint.Operations}
	} else if event.Error == nil && event.ToolCall == nil && event.ToolResult == nil && event.Guardrail == nil && event.ApprovalRequested == nil && event.BudgetPaused == nil && event.Message == nil && event.End == nil && event.RepeatDetected == nil {
		raw, err := json.Marshal(event)
		if err != nil {
			value = map[string]string{"eventEncodingError": err.Error()}
		} else {
			var kind struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(raw, &kind); err != nil {
				value = map[string]string{"eventEncodingError": err.Error()}
			} else {
				value = kind
			}
		}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		raw, _ = json.Marshal(map[string]string{"eventEncodingError": err.Error()})
	}
	x.eventsMu.Lock()
	x.events = append(x.events, raw)
	x.eventsMu.Unlock()
}

func assistantChatContextSSE(delta map[string]any, finish string) *http.Response {
	chunk, _ := json.Marshal(map[string]any{"id": "fixture", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}})
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + string(chunk) + "\n\ndata: [DONE]\n\n"))}
}
