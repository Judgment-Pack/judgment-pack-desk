package desk

// Source-only feasibility probe. This does not enroll a production Desk source
// or alter E2. It runs real released delegate/Eino/native adapters against the
// original private RunStore and handwritten offline provider peers. The small
// scope wrapper below is test-only and must not be shipped as source admission.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	nativeadapter "github.com/Judgment-Pack/judgment-pack-agent/go/adapter/agent"
	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter/delegate"
	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter/eino"
	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter/turn"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/gate"
	"github.com/Judgment-Pack/judgment-pack-agent/go/relay"
)

const assistantChildProbeOwn = "fixture_child_read"

var assistantChildProbeSchema = json.RawMessage(`{"type":"object","properties":{"probeQuery":{"type":"string"}},"required":["probeQuery"],"additionalProperties":false}`)
var assistantChildProbeArguments = []json.RawMessage{json.RawMessage(`{"probeQuery":"same first read"}`), json.RawMessage(`{"probeQuery":"same second read"}`)}

type assistantChildProbeCapabilityKey struct{}
type assistantChildProbeCapability struct {
	owner   *assistantRunExecution
	root    *runcontext.RunBinding
	store   *assistantCanonicalRunStore
	binding *runcontext.RunBinding
	scope   runcontext.DurableScope
	epoch   uint64
	mu      sync.Mutex
	active  bool
}
type assistantChildProbeAssociation struct {
	Token    string                  `json:"token"`
	Physical string                  `json:"physical"`
	Call     string                  `json:"call"`
	Dispatch string                  `json:"dispatch"`
	Scope    runcontext.DurableScope `json:"scope"`
	Source   string                  `json:"source"`
	Wire     string                  `json:"wire"`
	Args     json.RawMessage         `json:"args"`
	Result   json.RawMessage         `json:"result"`
}

// Only scalar identity/count diagnostics are retained; this is never a checkpoint
// repair or a substitute persistence port. The first failing original callback wins.
type assistantChildProbeCheckpointFailure struct {
	used                       int64
	root, request, phase, unit bool
	err                        error
}

type assistantChildProbeState struct {
	t               *testing.T
	e               *assistantRunExecution
	mu              sync.Mutex
	associations    map[string]assistantChildProbeAssociation
	byScope         map[string]int
	active          map[string]int
	peak            map[string]int
	peers           map[string]int
	gates           map[*gate.Calls]*gate.Gate
	childCaps       map[string]*assistantChildProbeCapability
	source          *runcontext.ToolSource
	sourceCanonical string
	sourceDigest    string
	wire            string
	api             bool
	first           chan struct{}
	firstOnce       sync.Once
	failedCP        *assistantChildProbeCheckpointFailure
}

func (c *assistantChildProbeCapability) check(ctx context.Context) error {
	c.mu.Lock()
	active := c.active
	c.mu.Unlock()
	if !active || ctx.Value(assistantChildProbeCapabilityKey{}) != c || c.binding == nil || c.binding.Writer != c.root.Writer || c.binding.Writer.Store != c.store || c.binding.Scope != c.scope {
		return errAssistantRunScope
	}
	c.owner.canonicalMu.Lock()
	current := c.owner.generation == c.epoch && c.owner.canonicalBinding == c.root && c.owner.canonicalStore == c.store && !c.owner.stopRequested && c.owner.lifecycle == "running"
	c.owner.canonicalMu.Unlock()
	generation, ok := ctx.Value(assistantInvocationGenerationKey{}).(assistantInvocationGeneration)
	if !current || !ok || generation.owner != c.owner || generation.number != c.epoch {
		return errAssistantRunScope
	}
	return ctx.Err()
}

// Capture executes as the actual inner of e.canonicalAdapter. The released
// delegate has already installed ChildPersistence and canonicalAdapter has
// checked its original Writer. No scope is read from provider args/history.
func (p *assistantChildProbeState) capture(inner adapter.Adapter) adapter.Adapter {
	return adapter.Func(func(ctx context.Context, r *adapter.Run) error {
		p.e.canonicalMu.Lock()
		root, store, epoch := p.e.canonicalBinding, p.e.canonicalStore, p.e.generation
		p.e.canonicalMu.Unlock()
		binding := r.Context.Persistence
		if binding == nil || root == nil || store == nil || binding.Writer != root.Writer || binding.Writer.Store != store || store.writer != p.e.writer || store.owner != p.e || store.generation != epoch || !store.accept {
			return errAssistantRunScope
		}
		capability := &assistantChildProbeCapability{owner: p.e, root: root, store: store, binding: binding, scope: binding.Scope, epoch: epoch, active: true}
		defer func() { capability.mu.Lock(); capability.active = false; capability.mu.Unlock() }()
		if capability.scope.Task != "" {
			if capability.scope.Execution != capability.scope.Task || capability.scope.Role != "author" || binding.Root {
				return errAssistantRunScope
			}
			p.mu.Lock()
			p.childCaps[capability.scope.Execution] = capability
			p.mu.Unlock()
		}
		held := *r
		held.HostTools = append([]runcontext.HostTool{}, r.HostTools...)
		for i := range held.HostTools {
			tool := &held.HostTools[i]
			if tool.Source == nil {
				continue
			}
			// Every child owns its closure and source copy; sharing a mutable
			// Source.VerifyReceipt pointer across scopes would race/rebind it.
			source := *tool.Source
			source.VerifyReceipt = func(callCtx context.Context, evidence runcontext.ReceiptEvidence) (bool, error) {
				return p.verify(context.WithValue(callCtx, assistantChildProbeCapabilityKey{}, capability), capability, evidence)
			}
			tool.Source = &source
			execute := tool.Execute
			tool.Execute = func(callCtx context.Context, args json.RawMessage) (runcontext.ToolResult, error) {
				return execute(context.WithValue(callCtx, assistantChildProbeCapabilityKey{}, capability), args)
			}
		}
		p.mu.Lock()
		g := p.gates[r.Gate]
		p.mu.Unlock()
		if g == nil {
			return errAssistantRunScope
		}
		held.EnrollContext = func(_ context.Context, offer adapter.ContextOffer) error {
			return g.EnrollContext(offer.ID, offer.HostTools)
		}
		return inner.Run(ctx, &held)
	})
}

func (p *assistantChildProbeState) claim(ctx context.Context, c *assistantChildProbeCapability, args json.RawMessage) (contract.CheckpointDurableDispatch, error) {
	var found contract.CheckpointDurableDispatch
	if err := c.check(ctx); err != nil {
		return found, err
	}
	run, err := p.e.writer.lease.store.Read(ctx, p.e.record.Binding, p.e.record.ID)
	if err != nil {
		return found, err
	}
	for _, d := range run.Checkpoint.Durable.Dispatches {
		task := ""
		if d.Scope.Task != nil {
			task = *d.Scope.Task
		}
		if d.State != "claimed" || d.Scope.Execution != c.scope.Execution || d.Scope.Role != c.scope.Role || task != c.scope.Task || d.Kind != "host" || d.Source != p.sourceDigest || d.Tool != p.wire || !d.Read || !assistantHostArgumentsMatch([]byte(d.Request), args) {
			continue
		}
		if found.ID != "" || d.ID == "" || d.RequestDigest != "sha256:"+digestOf([]byte(d.Request)) {
			return found, errAssistantRunScope
		}
		scopeRaw, _ := json.Marshal(d.Scope)
		fingerprint := runcontext.RecordDigest(runcontext.ObjectOf("kind", runcontext.String(d.Kind), "scope", runcontext.MustParseJSON(string(scopeRaw)), "source", runcontext.String(d.Source), "tool", runcontext.String(d.Tool), "request", runcontext.MustParseJSON(d.Request)))
		if d.Fingerprint != fingerprint {
			return found, errAssistantRunScope
		}
		found = d
	}
	if found.ID == "" {
		return found, errAssistantRunScope
	}
	return found, nil
}

func (p *assistantChildProbeState) execute(ctx context.Context, args json.RawMessage) (runcontext.ToolResult, error) {
	c, ok := ctx.Value(assistantChildProbeCapabilityKey{}).(*assistantChildProbeCapability)
	if !ok || c.scope.Task == "" {
		return runcontext.ToolResult{}, errAssistantRunScope
	}
	d, err := p.claim(ctx, c, args)
	if err != nil {
		return runcontext.ToolResult{}, err
	}
	p.mu.Lock()
	p.active[c.scope.Execution]++
	if p.active[c.scope.Execution] > p.peak[c.scope.Execution] {
		p.peak[c.scope.Execution] = p.active[c.scope.Execution]
	}
	p.byScope[c.scope.Execution]++
	first := p.byScope[c.scope.Execution] == 1
	if p.api && first && len(p.byScope) == 2 {
		p.firstOnce.Do(func() { close(p.first) })
	}
	p.mu.Unlock()
	defer func() { p.mu.Lock(); p.active[c.scope.Execution]--; p.mu.Unlock() }()
	if p.api && first {
		select {
		case <-p.first:
		case <-ctx.Done():
			return runcontext.ToolResult{}, ctx.Err()
		case <-time.After(20 * time.Second):
			return runcontext.ToolResult{}, fmt.Errorf("offline child scopes did not overlap")
		}
	}
	result := runcontext.ResultOf([]runcontext.Content{runcontext.TextContent("Handwritten immutable source result.")}, nil)
	token := "child-proof-" + rand.Text()
	a := assistantChildProbeAssociation{Token: token, Physical: "probe-host-" + rand.Text(), Call: runcontext.CallIDOf(ctx), Dispatch: d.ID, Scope: c.scope, Source: p.sourceDigest, Wire: p.wire, Args: bytes.Clone(args), Result: bytes.Clone(result.JSON())}
	// This is the original admitted physical journal, not a replacement store.
	// Fixture-only DTO: no production SourceBinding/approval is activated.
	raw, _ := json.Marshal(a)
	op := assistantRunOperation{ID: a.Physical, CallID: a.Physical, Kind: "host", Name: assistantChildProbeOwn, Intent: raw}
	if err = p.e.writer.Intent(ctx, op); err != nil {
		return runcontext.ToolResult{}, err
	}
	if err = p.e.writer.Dispatch(ctx, op.ID); err != nil {
		return runcontext.ToolResult{}, err
	}
	if err = p.e.writer.Result(ctx, op.ID, "completed", a.Result); err != nil {
		return runcontext.ToolResult{}, err
	}
	p.mu.Lock()
	p.associations[token] = a
	p.mu.Unlock()
	result.Receipt = token
	return result, nil
}

func (p *assistantChildProbeState) associationMatches(a assistantChildProbeAssociation, c *assistantChildProbeCapability, evidence runcontext.ReceiptEvidence, run assistantRunRecord) bool {
	if a.Token != evidence.Receipt || a.Scope != c.scope || a.Source != p.sourceDigest || a.Wire != p.wire || evidence.Source != p.sourceCanonical || evidence.Tool != assistantChildProbeOwn || evidence.Args != runcontext.CanonicalJSON(runcontext.MustParseJSON(string(a.Args))) || evidence.Result != runcontext.CanonicalJSON(runcontext.MustParseJSON(string(a.Result))) {
		return false
	}
	physical := 0
	for _, op := range run.Operations {
		if op.ID == a.Physical {
			physical++
			if op.Kind != "host" || op.Name != assistantChildProbeOwn || op.State != "completed" || !bytes.Equal(op.Intent, assistantChatJSON(a)) || !bytes.Equal(op.Result, a.Result) {
				return false
			}
		}
	}
	if physical != 1 {
		return false
	}
	count := 0
	for _, d := range run.Checkpoint.Durable.Dispatches {
		if d.ID != a.Dispatch {
			continue
		}
		count++
		task := ""
		if d.Scope.Task != nil {
			task = *d.Scope.Task
		}
		if d.State != "result" || d.Scope.Execution != a.Scope.Execution || d.Scope.Role != a.Scope.Role || task != a.Scope.Task || d.Kind != "host" || d.Source != a.Source || d.Tool != a.Wire || !assistantHostArgumentsMatch([]byte(d.Request), a.Args) || !assistantCanonicalHostResultWireEqual(a.Result, d.Result) || bytes.Contains(d.Result, []byte(a.Token)) {
			return false
		}
	}
	return count == 1
}

func (p *assistantChildProbeState) verify(ctx context.Context, c *assistantChildProbeCapability, evidence runcontext.ReceiptEvidence) (bool, error) {
	if err := c.check(ctx); err != nil {
		return false, err
	}
	p.mu.Lock()
	a, ok := p.associations[evidence.Receipt]
	p.mu.Unlock()
	if !ok {
		return false, nil
	}
	run, err := p.e.writer.lease.store.Read(ctx, p.e.record.Binding, p.e.record.ID)
	if err != nil {
		return false, err
	}
	if !p.associationMatches(a, c, evidence, run) {
		return false, nil
	}
	// Mutations update one association fact at a time while leaving every other
	// exact fact present, so an equal-argument sibling cannot be substituted.
	for _, mode := range []string{"scope", "source", "args", "claim", "epoch"} {
		changed := a
		switch mode {
		case "scope":
			changed.Scope.Execution = "foreign-child"
			changed.Scope.Task = "foreign-child"
		case "source":
			changed.Source = "sha256:" + strings.Repeat("f", 64)
		case "args":
			changed.Args = json.RawMessage(`{"probeQuery":"foreign"}`)
		case "claim":
			changed.Dispatch = "foreign-claim"
		case "epoch":
			foreign := &assistantChildProbeCapability{owner: c.owner, root: c.root, store: c.store, binding: c.binding, scope: c.scope, epoch: c.epoch + 1, active: true}
			if foreign.check(context.WithValue(ctx, assistantChildProbeCapabilityKey{}, foreign)) == nil {
				p.t.Error("foreign epoch capability accepted")
			}
			continue
		}
		if p.associationMatches(changed, c, evidence, run) {
			p.t.Errorf("association mutation accepted: %s", mode)
		}
	}
	return true, ctx.Err()
}

// Diagnostics run only after the actual coordinator returns on a failed probe.
// Never print error prose or retained content. The private Agent cause traversal
// is read-only, source-version-specific diagnosis; it supplies no run authority.
func assistantChildProbeErrorShape(value reflect.Value) string {
	labels := map[string]string{
		errAssistantRunInvalid.Error():   "assistant-run-invalid",
		errAssistantRunScope.Error():     "assistant-run-scope",
		errAssistantRunConflict.Error():  "assistant-run-conflict",
		errAssistantRunDispatch.Error():  "assistant-run-dispatch",
		errAssistantRunRecovery.Error():  "assistant-run-recovery",
		context.Canceled.Error():         "context-canceled",
		context.DeadlineExceeded.Error(): "context-deadline",
	}
	parts := []string{}
	for depth := 0; depth < 8 && value.IsValid(); depth++ {
		if value.Kind() == reflect.Interface {
			if value.IsNil() {
				break
			}
			value = value.Elem()
		}
		parts = append(parts, value.Type().String())
		if value.Kind() == reflect.Pointer {
			if value.IsNil() {
				break
			}
			value = value.Elem()
		}
		if value.Kind() != reflect.Struct {
			break
		}
		if message := value.FieldByName("s"); message.IsValid() && message.Kind() == reflect.String {
			if label := labels[message.String()]; label != "" {
				parts = append(parts, label)
			}
		}
		next := value.FieldByName("cause")
		if !next.IsValid() {
			next = value.FieldByName("err")
		}
		value = next
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ">")
}

func (p *assistantChildProbeState) storageDiagnostics(ctx context.Context, adapterErr error) {
	p.t.Helper()
	shape := func(err error) string { return assistantChildProbeErrorShape(reflect.ValueOf(err)) }
	p.e.canonicalMu.Lock()
	binding, store := p.e.canonicalBinding, p.e.canonicalStore
	owned := store != nil && store.owner == p.e && store.generation == p.e.generation && store.accept
	p.e.canonicalMu.Unlock()
	p.e.writer.lease.mu.Lock()
	leaseOpen, recoveryErr := p.e.writer.lease.file != nil, p.e.writer.lease.recoveryErr
	p.e.writer.lease.mu.Unlock()
	p.t.Logf("storage diagnosis: adapter=%s physical-writer=%s lease-open=%t recovery=%s private-store=%s canonical-owned=%t", shape(adapterErr), shape(p.e.writer.Err()), leaseOpen, shape(recoveryErr), shape(p.e.writer.lease.store.problem), owned)
	if binding != nil && binding.Writer != nil {
		// The coordinator has joined its invocations; no private field is read
		// while an adapter is running. Do not call Interface or use unsafe.
		fatal := reflect.ValueOf(binding.Writer).Elem().FieldByName("fatal")
		p.t.Logf("storage diagnosis: canonical-writer=%s original-store=%t", assistantChildProbeErrorShape(fatal), binding.Writer.Store == store)
	}
	p.mu.Lock()
	rootPeers, childPeers := 0, 0
	for scope, calls := range p.peers {
		if binding != nil && scope == binding.Scope.Execution {
			rootPeers += calls
		} else {
			childPeers += calls
		}
	}
	captured, scopes, physical := len(p.childCaps), len(p.byScope), len(p.associations)
	p.mu.Unlock()
	p.t.Logf("storage diagnosis: root-provider-calls=%d child-provider-calls=%d captured-children=%d physical-scopes=%d physical-results=%d", rootPeers, childPeers, captured, scopes, physical)
	bounded, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	saved, err := p.e.writer.lease.store.Read(bounded, p.e.record.Binding, p.e.record.ID)
	if err != nil {
		p.t.Logf("storage diagnosis: read=%s", shape(err))
		return
	}
	p.e.writer.mu.Lock()
	physicalRevision := p.e.writer.revision
	p.e.writer.mu.Unlock()
	p.t.Logf("storage diagnosis: physical-revision-equals-saved=%t", physicalRevision == saved.Revision)
	p.mu.Lock()
	failedCP := p.failedCP
	p.mu.Unlock()
	if failedCP != nil {
		savedUsed := int64(-1)
		if saved.Checkpoint != nil {
			savedUsed = saved.Checkpoint.Used
		}
		p.t.Logf("storage diagnosis: first-checkpoint-callback=%s attempted-used=%d saved-used=%d root-id=%t request=%t phase=%t unit=%t", shape(failedCP.err), failedCP.used, savedUsed, failedCP.root, failedCP.request, failedCP.phase, failedCP.unit)
	} else {
		p.t.Log("storage diagnosis: no-failed-checkpoint-callback")
	}
	states := map[string]int{}
	if saved.Checkpoint != nil && saved.Checkpoint.Durable != nil {
		for _, dispatch := range saved.Checkpoint.Durable.Dispatches {
			states[dispatch.Kind+"/"+dispatch.State]++
		}
	}
	commits, events := 0, 0
	if saved.Canonical != nil {
		commits, events = len(saved.Canonical.Commits), len(saved.Canonical.Events)
	}
	p.t.Logf("storage diagnosis: saved-projection=%s saved-commits=%d saved-events=%d saved-dispatches=%v", shape(validateAssistantCanonicalProjection(saved)), commits, events, states)
	if binding != nil && binding.Writer != nil {
		if cp := binding.Writer.Checkpoint(); cp != nil {
			_, err = readAssistantRunCheckpoint(*cp, saved.Binding)
			p.t.Logf("storage diagnosis: acknowledged-checkpoint=%s equals-saved=%t", shape(err), sameAssistantRunCheckpoint(cp, saved.Checkpoint))
		}
	}
}

func assistantChildProbeReply(calls []struct {
	name string
	args json.RawMessage
}) *http.Response {
	delta := map[string]any{"content": "Offline source facts retained."}
	finish := "stop"
	if len(calls) > 0 {
		rows := []any{}
		for i, call := range calls {
			rows = append(rows, map[string]any{"index": i, "id": fmt.Sprintf("probe-call-%d", i), "type": "function", "function": map[string]any{"name": call.name, "arguments": string(call.args)}})
		}
		delta = map[string]any{"tool_calls": rows}
		finish = "tool_calls"
	}
	chunk := map[string]any{"id": "offline-probe", "object": "chat.completion.chunk", "model": "fixture", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}, "usage": map[string]int{"prompt_tokens": 2, "completion_tokens": 1}}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + string(assistantChatJSON(chunk)) + "\n\ndata: [DONE]\n\n"))}
}

func TestAssistantExternalChildScopeProbe(t *testing.T) {
	for _, kind := range []string{"api", "native"} {
		t.Run(kind, func(t *testing.T) {
			store, binding, cp := assistantRunFixture(t)
			// Author the exact initial API/native execution; do not edit a retained
			// checkpoint or change its unit to make another adapter accept it.
			// Declare the same run policy and named models before the initial
			// snapshot that the actual adapter will resolve on continuation.
			concurrency := 2
			policy := runcontext.PolicyLayer{"concurrency": map[string]any{"children": 2, "requests": 2}}
			named := runcontext.RunModels{Critic: "fixture", Child: "fixture", ConcurrentRequests: &concurrency}
			session := runcontext.Session{Prompt: "Review the exact saved input", Policy: policy, Critic: "off", PolicyLegacyModels: &named, Context: &runcontext.Input{CurrentRequest: &runcontext.CurrentRequest{Message: "Review", Input: "exact original"}}}
			if kind == "native" {
				session.Agent = &runcontext.AgentSession{Model: "fixture"}
			} else {
				session.Model = &runcontext.ModelSession{Family: "openai-compatible", Model: "fixture"}
			}
			opened, err := runcontext.ContextualSession(t.Context(), session)
			assistantRunMust(t, err)
			cp, err = opened.Context.Snapshot("")
			assistantRunMust(t, err)
			opened.Context.Close()
			binding.Unit, binding.ExecutionKey, binding.Phase = cp.Unit, cp.RequestKey, cp.Phase
			lease, err := store.Acquire(t.Context())
			assistantRunMust(t, err)
			defer lease.Close()
			record, created, err := lease.Admit(t.Context(), binding, "child-scope-probe", digestOf([]byte("handwritten child probe")), func() error { return nil })
			assistantRunMust(t, err)
			if !created {
				t.Fatal("probe not admitted")
			}
			writer, err := lease.Writer(t.Context(), binding, record.ID)
			assistantRunMust(t, err)
			assistantRunMust(t, writer.initializeCanonical(t.Context(), cp))
			record, err = store.Read(t.Context(), binding, record.ID)
			assistantRunMust(t, err)
			e := &assistantRunExecution{record: record, writer: writer, ctx: t.Context(), generation: 1, lifecycle: "running"}
			ctx, cancel := context.WithTimeout(e.generationContext(t.Context(), 1), 60*time.Second)
			defer cancel()
			input := &runcontext.Input{Execution: record.Checkpoint, CurrentRequest: &runcontext.CurrentRequest{Message: "Review", Input: "exact original"}}
			assistantRunMust(t, e.bindCanonicalInput(ctx, input))
			p := &assistantChildProbeState{t: t, e: e, associations: map[string]assistantChildProbeAssociation{}, byScope: map[string]int{}, active: map[string]int{}, peak: map[string]int{}, peers: map[string]int{}, gates: map[*gate.Calls]*gate.Gate{}, childCaps: map[string]*assistantChildProbeCapability{}, api: kind == "api", first: make(chan struct{})}
			// Observe only the scalar candidate and unchanged return from the
			// original root callback. Keep the admitted Writer and Store installed.
			originalCheckpoint := input.Checkpoint
			input.Checkpoint = func(callCtx context.Context, state contract.Checkpoint) error {
				err := originalCheckpoint(callCtx, state)
				if err != nil {
					p.mu.Lock()
					if p.failedCP == nil {
						p.failedCP = &assistantChildProbeCheckpointFailure{used: state.Used, root: state.ID == p.e.canonicalRunID(), request: state.RequestKey == binding.ExecutionKey, phase: state.Phase == binding.Phase, unit: state.Unit == binding.Unit, err: err}
					}
					p.mu.Unlock()
				}
				return err
			}
			taxonomy := &runcontext.ToolTaxonomy{Operation: "read", Effect: "read-external"}
			source := &runcontext.ToolSource{Kind: "custom", ID: "fixture-child", Trust: "first-party", Version: "1", Identity: &contract.ToolOfferIdentity{Kind: "connection", Label: "Offline handwritten source owner"}}
			source.ToolListDigest = runcontext.ToolListDigest([]runcontext.ListedTool{{Name: assistantChildProbeOwn, Description: "Read one immutable offline fixture.", InputSchema: assistantChildProbeSchema, Taxonomy: taxonomy}})
			source.CurrentIdentity = func(context.Context) (contract.ToolOfferIdentity, error) { return *source.Identity, nil }
			source.VerifyReceipt = func(context.Context, runcontext.ReceiptEvidence) (bool, error) {
				return false, fmt.Errorf("unscoped verifier must never run")
			}
			p.source = source
			sourceRaw, _ := json.Marshal(source.Record(assistantChildProbeOwn))
			sourceValue := runcontext.MustParseJSON(string(sourceRaw))
			p.sourceCanonical = runcontext.CanonicalJSON(sourceValue)
			p.sourceDigest = runcontext.RecordDigest(sourceValue)
			p.wire = runcontext.WireName(source.ID, assistantChildProbeOwn)
			tool := runcontext.HostTool{Name: assistantChildProbeOwn, Description: "Read one immutable offline fixture.", InputSchema: assistantChildProbeSchema, Taxonomy: taxonomy, EffectClass: "read", Delegation: "read", Source: source, Execute: p.execute}
			g := gate.New(gate.Options{})
			// Source wire names and framing belong to the final contextual offer.
			// Enroll that real offer through capture, as the production host does.
			g.SetOffer(contract.ToolOffer{})
			g.Enter(gate.LoopPhase(contract.ToolOffer{}))
			calls := g.Calls()
			p.gates[calls] = g
			run := &adapter.Run{ModelName: "fixture", Wire: "openai-compatible", Prompt: "Review", Critic: "off", AllowConversation: true, Interactive: true, Context: input, Gate: calls, HostTools: []runcontext.HostTool{tool}, Policy: policy, Models: func() (runcontext.RunModels, error) {
				return named, nil
			}, Emit: func(event contract.RunEvent) {
				if event.Error != nil {
					t.Errorf("offline %s adapter failure: %s", kind, assistantChatJSON(event.Error))
				}
			}}
			run.Children = func(_ context.Context, profile contract.DelegatedTaskProfile, emit func(contract.RunEvent)) (*gate.Calls, []contract.ToolOfferRuntimeTool, error) {
				child := gate.New(gate.Options{Emit: emit, Turn: g})
				parentOffer := g.Offer()
				phase := gate.ChildPhase(profile, parentOffer, g.Phase())
				offer := phase.Offer(parentOffer)
				offer.HostTools = nil // The actual child context enrolls its final declarations.
				child.SetOffer(offer)
				child.Enter(phase)
				calls := child.Calls()
				p.mu.Lock()
				p.gates[calls] = child
				p.mu.Unlock()
				return calls, offer.Tools, nil
			}
			delegation := json.RawMessage(`{"tasks":[{"title":"First scope","objective":"Read immutable evidence in first scope.","profile":"review"},{"title":"Second scope","objective":"Read immutable evidence in second scope.","profile":"review"}]}`)
			var engine adapter.Adapter
			if kind == "api" {
				run.Model = func(callCtx context.Context, _ string, _ relay.Request) (*http.Response, error) {
					scope, ok := runcontext.DurableDispatchScope(callCtx)
					if !ok {
						return nil, fmt.Errorf("API provider scope absent")
					}
					p.mu.Lock()
					p.peers[scope.Execution]++
					n := p.peers[scope.Execution]
					p.mu.Unlock()
					if n > 1 {
						return assistantChildProbeReply(nil), nil
					}
					if scope.Task == "" {
						return assistantChildProbeReply([]struct {
							name string
							args json.RawMessage
						}{{turn.DelegateTool, delegation}}), nil
					}
					return assistantChildProbeReply([]struct {
						name string
						args json.RawMessage
					}{{p.wire, assistantChildProbeArguments[0]}, {p.wire, assistantChildProbeArguments[1]}}), nil
				}
				engine = eino.New()
			} else {
				run.Agent = &adapter.Agent{Model: "fixture", Run: func(callCtx context.Context, request adapter.AgentRequest, cb adapter.AgentCallbacks) error {
					defer func() {
						if cb.ProviderRelease != nil && cb.ProviderRelease.Acknowledge != nil {
							cb.ProviderRelease.Acknowledge(cb.ProviderRelease.Invocation)
						}
					}()
					scope, ok := runcontext.DurableDispatchScope(callCtx)
					if !ok {
						return fmt.Errorf("native provider scope absent")
					}
					p.mu.Lock()
					p.peers[scope.Execution]++
					n := p.peers[scope.Execution]
					p.mu.Unlock()
					find := func(schema json.RawMessage) string {
						found := ""
						for _, offered := range request.Tools {
							if assistantCanonicalHostResultWireEqual(offered.InputSchema, schema) {
								if found != "" {
									return ""
								}
								found = offered.Name
							}
						}
						return found
					}
					if scope.Task == "" && n == 1 {
						name := find(turn.DelegationTool(turn.TaskProfiles).InputSchema)
						if name == "" {
							return fmt.Errorf("exact native delegation schema absent")
						}
						_, err := cb.Tool(callCtx, adapter.AgentToolCall{ID: "delegate-probe", Name: name, Arguments: delegation})
						return err
					}
					if scope.Task != "" {
						name := find(assistantChildProbeSchema)
						if name == "" {
							return fmt.Errorf("exact native source schema absent")
						}
						for i, args := range assistantChildProbeArguments {
							answer, err := cb.Tool(callCtx, adapter.AgentToolCall{ID: fmt.Sprintf("native-read-%d", i), Name: name, Arguments: args})
							if err != nil || answer.IsError {
								return fmt.Errorf("native scoped read: %v %s", err, answer.Text)
							}
						}
					}
					return cb.Event(callCtx, adapter.AgentEvent{Type: "message", ID: "final-probe", Phase: "final", Text: "Offline immutable source facts retained."})
				}}
				engine = nativeadapter.New()
			}
			err = delegate.With(e.canonicalAdapter(p.capture(engine), g), delegate.Options{}).Run(ctx, run)
			if err != nil || t.Failed() {
				p.storageDiagnostics(ctx, err)
			}
			assistantRunMust(t, err)
			if ctx.Err() != nil {
				t.Fatal("bounded offline probe expired", ctx.Err())
			}
			after, err := store.Read(ctx, binding, record.ID)
			assistantRunMust(t, err)
			canonicalActivationCheck(t, after)
			p.mu.Lock()
			associations := make([]assistantChildProbeAssociation, 0, len(p.associations))
			for _, a := range p.associations {
				associations = append(associations, a)
			}
			scopes := len(p.byScope)
			for scope, n := range p.byScope {
				if n != 2 || p.peak[scope] != 1 {
					t.Errorf("per-scope call serialization lost: calls=%d peak=%d", n, p.peak[scope])
				}
			}
			p.mu.Unlock()
			if scopes != 2 || len(associations) != 4 {
				t.Fatalf("actual scoped producers missing: scopes=%d physical=%d", scopes, len(associations))
			}
			claims := map[string]bool{}
			for _, a := range associations {
				if claims[a.Dispatch] {
					t.Fatal("two physical operations bound same canonical dispatch")
				}
				claims[a.Dispatch] = true
				accepted := 0
				for _, item := range after.Canonical.Events {
					envelope, err := runcontext.ReadStoredRunEvent(item.Bytes)
					assistantRunMust(t, err)
					result := envelope.Event.ToolResult
					if result != nil && result.Source != nil && result.Source.Receipt != nil && *result.Source.Receipt == a.Token {
						accepted++
						if envelope.Scope != a.Scope || result.CallID == nil || *result.CallID != a.Call || result.Name != a.Wire || result.IsError || result.Text != "Handwritten immutable source result." || len(result.Structured) != 0 {
							t.Fatal("receipt accepted in foreign scope/call or changed projection")
						}
						closedSource := *result.Source
						closedSource.Receipt = nil
						sourceRaw, _ := json.Marshal(closedSource)
						if runcontext.CanonicalJSON(runcontext.MustParseJSON(string(sourceRaw))) != p.sourceCanonical {
							t.Fatal("accepted receipt source changed")
						}
					}
				}
				if accepted != 1 {
					t.Fatal("missing exact final accepted source receipt", accepted)
				}
			}
			// Cross-scope equal-argument substitutions use real sibling claims and
			// receipt tokens from this run, rather than guessed nonexistent ids.
			for _, a := range associations {
				p.mu.Lock()
				capability := p.childCaps[a.Scope.Execution]
				p.mu.Unlock()
				evidence := runcontext.ReceiptEvidence{Source: p.sourceCanonical, Tool: assistantChildProbeOwn, Args: runcontext.CanonicalJSON(runcontext.MustParseJSON(string(a.Args))), Result: runcontext.CanonicalJSON(runcontext.MustParseJSON(string(a.Result))), Receipt: a.Token}
				for _, sibling := range associations {
					if sibling.Scope == a.Scope || !bytes.Equal(sibling.Args, a.Args) {
						continue
					}
					changed := a
					changed.Dispatch = sibling.Dispatch
					if p.associationMatches(changed, capability, evidence, after) {
						t.Fatal("real sibling canonical claim substituted")
					}
					changed = sibling
					evidence.Receipt = sibling.Token
					if p.associationMatches(changed, capability, evidence, after) {
						t.Fatal("real sibling receipt substituted into another child verifier")
					}
				}
			}
			// Exercise the actual canonical-store epoch fence without mutating its
			// current head or poisoning the original Agent writer.
			head, err := e.canonicalStore.Load(ctx, e.canonicalStore.id)
			assistantRunMust(t, err)
			_, err = e.canonicalStore.Commit(e.generationContext(t.Context(), 2), runcontext.RunCommit{RunID: e.canonicalStore.id, ExpectedRevision: head.Revision, CommitID: "foreign-probe-epoch", Checkpoint: head.Checkpoint, Events: [][]byte{}})
			if err == nil {
				t.Fatal("foreign epoch canonical commit accepted")
			}
			final, err := store.Read(ctx, binding, record.ID)
			assistantRunMust(t, err)
			if !reflect.DeepEqual(after.Canonical, final.Canonical) || !reflect.DeepEqual(after.Operations, final.Operations) {
				t.Fatal("rejected epoch rewrote canonical journal")
			}
		})
	}
}
