package desk

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	native "github.com/Judgment-Pack/judgment-pack-agent/go/adapter/agent"
	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter/delegate"
	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter/eino"
	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter/wire"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
	"github.com/Judgment-Pack/judgment-pack-agent/go/gate"
	"github.com/Judgment-Pack/judgment-pack-agent/go/relay"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
)

// This prerequisite retains the original Desk store, writer and lease. It does not
// certify the production service's owner lifetime, artifact custody or OS scope.
// Every protocol reply below is handwritten; no installed provider or Runtime is used.
func TestAssistantRuntimeOwnerSameStoreContinuation(t *testing.T) {
	for _, engine := range []string{"native", "api"} {
		if !t.Run(engine, func(t *testing.T) { assistantRuntimeOwnerContinuation(t, engine, "positive") }) {
			return // A failed prerequisite stops before another engine or any negative probes.
		}
	}
}

type assistantRuntimeOwnerPeer struct {
	mu         sync.Mutex
	tools      []contract.ToolOfferRuntimeTool
	calls      []json.RawMessage
	lists      int
	beforeCall func(context.Context) error
}

func (p *assistantRuntimeOwnerPeer) call(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var q struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(raw, &q); err != nil {
		return nil, err
	}
	if len(q.ID) == 0 {
		return nil, nil
	}
	var result any
	switch q.Method {
	case "initialize":
		result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "owner-prerequisite-peer", "version": "fixture"}}
	case "tools/list":
		p.lists++
		result = map[string]any{"tools": p.tools}
	case "tools/call":
		if p.beforeCall != nil {
			if err := p.beforeCall(ctx); err != nil {
				return nil, err
			}
		}
		p.calls = append(p.calls, append(json.RawMessage(nil), q.Params...))
		result = map[string]any{"content": []map[string]string{{"type": "text", "text": "evaluated exact draft"}}, "structuredContent": map[string]any{"status": "ok", "auditAppended": false}}
	default:
		return nil, fmt.Errorf("unexpected MCP method %q", q.Method)
	}
	return json.Marshal(map[string]any{"jsonrpc": "2.0", "id": q.ID, "result": result})
}

func TestAssistantRuntimeOwnerSameStoreStopBarriers(t *testing.T) {
	for _, barrier := range []string{"parked", "prepared", "installed"} {
		if !t.Run(barrier, func(t *testing.T) { assistantRuntimeOwnerContinuation(t, "native", barrier) }) {
			return
		}
	}
}

// This successor preserves the earlier failed R1 source/evidence separately.
// Read-only preparation is not executor admission.
func TestAssistantRuntimeOwnerClosedLeaseSuccessor(t *testing.T) {
	assistantRuntimeOwnerContinuation(t, "native", "lease")
}

func TestAssistantRuntimeOwnerSameStoreAuthorityBarriers(t *testing.T) {
	for _, scenario := range []string{"authority", "lease", "revocation", "owner", "list", "phase", "source", "decision", "late-callback", "release-withheld"} {
		if !t.Run(scenario, func(t *testing.T) { assistantRuntimeOwnerContinuation(t, "native", scenario) }) {
			return
		}
	}
}

func assistantRuntimeOwnerContinuation(t *testing.T, engine, scenario string, textCase ...assistantApprovalTextCase) {
	t.Helper()
	var reviewed *assistantApprovalTextCase
	if len(textCase) > 0 {
		reviewed = &textCase[0]
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	experimental := true
	description, siblingDescription := "Evaluate the exact unsaved draft", "Inspect the unselected sibling"
	peer := &assistantRuntimeOwnerPeer{tools: []contract.ToolOfferRuntimeTool{
		{Name: "experimental_evaluate", Description: &description, InputSchema: json.RawMessage(`{"type":"object","properties":{"value":{"type":"integer"},"rehearsal":{"type":"boolean"}}}`), Meta: contract.ToolTaxonomy{Operation: "evaluate", Effect: "act", Experimental: &experimental}},
		{Name: "get_schema", Description: &siblingDescription, InputSchema: json.RawMessage(`{"type":"object"}`), Meta: contract.ToolTaxonomy{Operation: "read", Effect: "read"}},
	}}
	pin := "sha256:" + strings.Repeat("a", 64)
	owner := gate.RuntimeExecutionOwner{ArtifactDigest: pin, ProcessID: "test-owned-launch", ProjectID: "test-project", ProjectLabel: "Fixture project", SessionID: "test-session", PrincipalDigest: pin, PrincipalLabel: "Fixture editor", ScopeDigest: pin, ScopeLabel: "Exact draft"}
	// Kept-text rows use the supported non-Runtime SourceGate contract.
	// Runtime canonicalization and its v3 approval inventory remain unchanged.
	var sourceCalls []json.RawMessage
	var sourceTools []runcontext.HostTool
	if reviewed != nil {
		digest := "sha256:" + digestOf([]byte(`[{"description":"Write the exact reviewed note.","inputSchema":{"type":"object"},"name":"write_note","taxonomy":{"effect":"act","operation":"act"}}]`))
		sourceTools = []runcontext.HostTool{{Name: "write_note", Description: "Write the exact reviewed note.", InputSchema: json.RawMessage(`{"type":"object"}`), Taxonomy: &runcontext.ToolTaxonomy{Operation: "act", Effect: "act"}, EffectClass: "act", Approval: "required", Source: &runcontext.ToolSource{Kind: "custom", ID: "notes", Trust: "person", Version: "1", ToolListDigest: digest, CurrentDigest: func(context.Context) (string, error) { return digest, nil }}, Approve: func(runcontext.ApprovalRequest) string { return "ask" }, Execute: func(callCtx context.Context, raw json.RawMessage) (runcontext.ToolResult, error) {
			if peer.beforeCall != nil {
				if err := peer.beforeCall(callCtx); err != nil {
					return runcontext.ToolResult{}, err
				}
			}
			sourceCalls = append(sourceCalls, append(json.RawMessage(nil), raw...))
			return runcontext.ResultOf([]runcontext.Content{runcontext.TextContent("exact reviewed note written")}, nil), nil
		}}}
	}
	g := gate.New(gate.Options{Granted: []string{"experimental_evaluate"}, Runtime: peer.call, RuntimeActions: &gate.RuntimeActionPort{Owner: owner, Current: func(context.Context) (gate.RuntimeExecutionOwner, error) { return owner, nil }}})
	// Match production: the actual contextual session enrolls the final source
	// declarations, including their gate-retained framing marker.
	_, err := g.Open(ctx, nil)
	assistantRunMust(t, err)
	if len(g.Offer().Tools) != 1 || g.Offer().Tools[0].Name != "experimental_evaluate" || peer.lists != 1 {
		t.Fatal("initial full catalogue selection changed")
	}
	calls := g.Calls()

	// Use the existing real Desk store fixture, then admit a checkpoint for the
	// actual engine rather than importing or reconstructing an acknowledged run.
	store, binding, _ := assistantRunFixture(t)
	initialInput := &runcontext.Input{CurrentRequest: &runcontext.CurrentRequest{Message: "Review", Input: "exact original"}}
	session := runcontext.Session{Prompt: "Review the exact saved input", Context: initialInput, RuntimeCalls: calls, HostTools: sourceTools, AllowConversation: true, Interactive: true, Critic: "off"}
	for _, tool := range g.Offer().Tools {
		session.Tools = append(session.Tools, runcontext.RuntimeTool{Name: tool.Name, Meta: &runcontext.ToolTaxonomy{Operation: tool.Meta.Operation, Effect: tool.Meta.Effect, Experimental: tool.Meta.Experimental}, Offer: &tool})
	}
	// Match the actual adapters' host-authored policy during preparation. A
	// durable continuation must retain this policy before its first request.
	policyRun := &adapter.Run{ModelName: "fixture", Wire: "openai-compatible", Context: initialInput}
	named := runcontext.RunModels{Critic: "fixture", Child: "fixture"}
	if engine == "native" {
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
	opened, err := runcontext.ContextualSession(ctx, session)
	assistantRunMust(t, err)
	cp, err := opened.Context.Snapshot("")
	opened.Context.Close()
	assistantRunMust(t, err)
	binding.Phase, binding.Unit, binding.ExecutionKey = cp.Phase, cp.Unit, cp.RequestKey
	lease, err := store.Acquire(ctx)
	assistantRunMust(t, err)
	defer lease.Close()
	record, created, err := lease.Admit(ctx, binding, "runtime-owner-prerequisite", digestOf([]byte("exact original")), func() error { return nil })
	assistantRunMust(t, err)
	if !created {
		t.Fatal("original admission missing")
	}
	deskWriter, err := lease.Writer(ctx, binding, record.ID)
	assistantRunMust(t, err)
	assistantRunMust(t, deskWriter.initializeCanonical(ctx, cp))
	record, err = store.Read(ctx, binding, record.ID)
	assistantRunMust(t, err)
	execution := &assistantRunExecution{ctx: ctx, record: record, writer: deskWriter}
	authorityLive := true
	authorize := func(context.Context) error {
		if !authorityLive {
			return errors.New("fixture authority revoked")
		}
		return nil
	}
	input := &runcontext.Input{Authorize: authorize, CurrentRequest: &runcontext.CurrentRequest{Message: "Review", Input: "exact original"}}
	assistantRunMust(t, execution.bindCanonicalInput(ctx, input))
	original := input.Persistence
	if original == nil || original.Writer == nil {
		t.Fatal("original canonical binding missing")
	}
	originalWriter := original.Writer
	originalStore, ok := originalWriter.Store.(*assistantCanonicalRunStore)
	if !ok || originalStore.writer != deskWriter || deskWriter.lease != lease {
		t.Fatal("original store authority missing")
	}
	defer func() {
		latest, readErr := store.Read(context.WithoutCancel(ctx), binding, record.ID)
		t.Logf("final evidence engine=%s read=%v head=%s originalCheckpoint=%s physicalCalls=%d enrollments-retained Gate=%p Calls=%p DeskWriter=%p Store=%p lease=%p", engine, readErr, mustAssistantRuntimeOwnerJSON(latest.Canonical), mustAssistantRuntimeOwnerJSON(originalWriter.Checkpoint()), len(peer.calls), g, calls, deskWriter, originalStore, lease)
	}()
	peer.beforeCall = func(callCtx context.Context) error {
		current, err := store.Read(callCtx, binding, record.ID)
		if err != nil {
			return err
		}
		execution.canonicalMu.Lock()
		installed, stopped := execution.canonicalBinding, execution.stopRequested
		execution.canonicalMu.Unlock()
		if ctx.Err() != nil || stopped || installed == nil || installed.Writer == originalWriter || installed.Writer.Store != originalStore || current.Checkpoint == nil || current.Checkpoint.Approval == nil || current.Checkpoint.Approval.State != "approved" {
			return errors.New("physical dispatch preceded approved acknowledgement or lost retained authority")
		}
		t.Logf("physical dispatch acknowledged head=%s checkpoint=%s currentWriter=%p", current.Canonical.Head.Revision, mustAssistantRuntimeOwnerJSON(current.Checkpoint), installed.Writer)
		return nil
	}
	var events []contract.RunEvent
	var enrollments, releases, callbacks, callbacksJoined, releaseCapabilities atomic.Int32
	var invocations atomic.Int32
	var mu sync.Mutex
	var callbackErr, managerErr error
	var lateCallback func() error
	settled := make(chan struct{}, 1)
	var manager *codexbridge.Manager
	if engine == "native" {
		dir := t.TempDir()
		binary := filepath.Join(dir, "native-peer")
		quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
		argumentEnv := ""
		if reviewed != nil {
			argumentEnv = "export JPS_RUNTIME_OWNER_ARGS=" + quote(reviewed.Request) + "\nexport JPS_RUNTIME_OWNER_SOURCE=1\n"
		}
		wrapper := "#!/bin/sh\n" + argumentEnv + "export JPS_RUNTIME_OWNER_NATIVE_PEER=1\nexport JPS_RUNTIME_OWNER_TRACE=" + quote(filepath.Join(dir, "trace.jsonl")) + "\nexec " + quote(os.Args[0]) + " -test.run='^TestAssistantRuntimeOwnerNativePeer$'\n"
		assistantRunMust(t, os.WriteFile(binary, []byte(wrapper), 0700))
		manager, err = codexbridge.NewManager(codexbridge.Options{Binary: binary, ProfileDir: filepath.Join(dir, "account"), ProjectDir: store.server.projectDir})
		assistantRunMust(t, err)
		defer manager.Close()
	}
	makeRun := func(current *runcontext.Input, resume bool) (*adapter.Run, adapter.Adapter) {
		run := &adapter.Run{Prompt: "Review the exact saved input", Wire: "openai-compatible", ModelName: "fixture", Thinking: "off", Critic: "off", Gate: calls, Tools: g.Offer().Tools, HostTools: sourceTools, AllowConversation: true, Interactive: true, Context: current, Emit: func(e contract.RunEvent) { events = append(events, e) }}
		run.EnrollContext = func(_ context.Context, offer adapter.ContextOffer) error {
			enrollments.Add(1)
			return g.EnrollContext(offer.ID, offer.HostTools)
		}
		if engine == "native" {
			run.Agent = &adapter.Agent{Model: "fixture", Effort: "medium", Run: func(invocation context.Context, request adapter.AgentRequest, cb adapter.AgentCallbacks) error {
				invocations.Add(1)
				if cb.ProviderRelease != nil && cb.ProviderRelease.Invocation != nil && cb.ProviderRelease.Acknowledge != nil {
					releaseCapabilities.Add(1)
				}
				// This test-owned host integrates the actual Manager and package
				// with the original store/Gate; it is not assistantAgent coverage.
				var callbackMu sync.Mutex
				var callbacksHeld sync.WaitGroup
				callbackClosing := false
				var returnedToolErr error
				providerReleased := make(chan struct{})
				bridge := codexbridge.RunRequest{Model: request.Model, Prompt: request.Prompt, Instructions: request.Instructions, Effort: request.Effort, Phase: request.Phase}
				for _, tool := range request.Tools {
					bridge.Tools = append(bridge.Tools, codexbridge.Tool{Name: tool.Name, Description: tool.Description, InputSchema: tool.InputSchema})
				}
				bridge.ProviderReleased = func() {
					releases.Add(1)
					if scenario != "release-withheld" && cb.ProviderRelease != nil && !cb.ProviderRelease.Acknowledge(cb.ProviderRelease.Invocation) {
						t.Error("actual Manager release rejected")
					}
					close(providerReleased)
				}
				err := manager.Run(invocation, codexbridge.Owner{ID: "owner-prerequisite", Session: ctx, Policy: ctx}, bridge, func(e codexbridge.RunEvent) error {
					return cb.Event(invocation, adapter.AgentEvent{Type: e.Type, ID: e.ID, Text: e.Text, Phase: e.Phase, Attempt: e.Attempt})
				}, func(callCtx context.Context, call codexbridge.ToolCall) (codexbridge.ToolAnswer, error) {
					callbackMu.Lock()
					if callbackClosing {
						callbackMu.Unlock()
						return codexbridge.ToolAnswer{}, errAssistantRunDispatch
					}
					callbacksHeld.Add(1)
					callbackMu.Unlock()
					defer callbacksHeld.Done()
					callbacks.Add(1)
					if lateCallback == nil {
						lateCallback = func() error {
							_, err := cb.Tool(invocation, adapter.AgentToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments})
							return err
						}
					}
					defer callbacksJoined.Add(1)
					answer, err := cb.Tool(callCtx, adapter.AgentToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments})
					callbackMu.Lock()
					if returnedToolErr == nil {
						returnedToolErr = err
					}
					callbackMu.Unlock()
					mu.Lock()
					callbackErr = err
					mu.Unlock()
					settled <- struct{}{}
					return codexbridge.ToolAnswer{Text: answer.Text, IsError: answer.IsError}, err
				})
				mu.Lock()
				managerErr = err
				mu.Unlock()
				// Close callback admission before joining: Manager can return on
				// its attempt cancellation while its Tool goroutine is unwinding.
				callbackMu.Lock()
				callbackClosing = true
				callbackMu.Unlock()
				joined := make(chan struct{})
				go func() { callbacksHeld.Wait(); close(joined) }()
				bounded, finish := context.WithTimeout(ctx, 5*time.Second)
				defer finish()
				select {
				case <-joined:
				case <-bounded.Done():
					return bounded.Err()
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
				callbackMu.Lock()
				toolErr := returnedToolErr
				callbackMu.Unlock()
				if toolErr != nil && (err == nil || err == codexbridge.ErrRun || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
					select {
					case <-providerReleased:
						if ctx.Err() != nil {
							return ctx.Err()
						}
						// Preserve the exact callback error; never recognize a pause
						// by text or author a checkpoint in the test host.
						return toolErr
					case <-bounded.Done():
						return bounded.Err()
					}
				}
				return err
			}}
			implementation := execution.canonicalAdapter(native.New(), g)
			if scenario == "release-withheld" {
				// Production's conversational coordinator supplies the live release
				// obligation. A bare ordinary native invocation has none.
				run.Children = func(context.Context, contract.DelegatedTaskProfile, func(contract.RunEvent)) (*gate.Calls, []contract.ToolOfferRuntimeTool, error) {
					return nil, nil, errors.New("release fixture must not dispatch a child")
				}
				implementation = delegate.With(implementation, delegate.Options{})
			}
			return run, implementation
		}
		requests := 0
		run.Model = func(context.Context, string, relay.Request) (*http.Response, error) {
			requests++
			if requests != 1 {
				return nil, errors.New("unexpected API follow-up while approval held")
			}
			if resume {
				return assistantRuntimeOwnerResponse(false), nil
			}
			if reviewed != nil {
				return assistantRuntimeOwnerResponse(true, reviewed.Request, "notes__write_note"), nil
			}
			return assistantRuntimeOwnerResponse(true), nil
		}
		return run, execution.canonicalAdapter(eino.New(), g)
	}
	run, implementation := makeRun(input, false)
	err = implementation.Run(ctx, run)
	if engine == "native" {
		select {
		case <-settled:
		case <-ctx.Done():
			t.Fatal("actual native callback did not settle", ctx.Err())
		}
	}
	mu.Lock()
	t.Logf("pause evidence engine=%s adapter=%T:%v callback=%T:%v Manager=%T:%v release=%d callbacks=%d Writer.Failed=%t binding=%p writer=%p Gate=%p Calls=%p", engine, err, err, callbackErr, callbackErr, managerErr, managerErr, releases.Load(), callbacks.Load(), originalWriter.Failed(), original, originalWriter, g, calls)
	mu.Unlock()
	assistantRunMust(t, err)
	paused, err := store.Read(ctx, binding, record.ID)
	assistantRunMust(t, err)
	canonicalActivationCheck(t, paused)
	if scenario == "release-withheld" {
		_, prepareErr := runcontext.PrepareDurableInput(ctx, &runcontext.Input{RunStore: &runcontext.DurableRunInput{Store: originalStore, ID: originalWriter.ID, Resume: true}})
		var unknown *runcontext.ProviderReleaseUnknown
		if !errors.As(prepareErr, &unknown) || len(peer.calls) != 0 || releases.Load() != 1 || releaseCapabilities.Load() != 1 || callbacksJoined.Load() != 1 || enrollments.Load() != 1 {
			t.Fatal("unacknowledged release admitted continuation", prepareErr)
		}
		t.Logf("release withheld capability=%d actualRelease=%d joined=%d prepare=%T:%v physicalCalls=0", releaseCapabilities.Load(), releases.Load(), callbacksJoined.Load(), prepareErr, prepareErr)
		return
	}
	var asked *contract.RunEventApprovalRequested
	for _, e := range events {
		if e.ApprovalRequested != nil {
			if asked != nil {
				t.Fatal("duplicate approval request")
			}
			asked = e.ApprovalRequested
		}
		if e.ToolCall != nil {
			t.Fatal("Runtime tool event preceded approval")
		}
	}
	if asked == nil {
		t.Fatal("actual approval request missing")
	}
	if reviewed == nil && (asked.Runtime == nil || asked.Request != nil) {
		t.Fatal("Runtime approval left its supported v3 inventory")
	}
	if reviewed != nil && (asked.Runtime != nil || asked.Source.Kind != "custom" || asked.Source.ID != "notes" || asked.Request == nil) {
		t.Fatal("kept-text approval did not come from the custom source")
	}
	args, _ := json.Marshal(asked.Args)
	if reviewed == nil && !reflect.DeepEqual(assistantRuntimeOwnerJSON(t, args), assistantRuntimeOwnerJSON(t, []byte(`{"value":7,"rehearsal":true}`))) {
		t.Fatal("Agent did not rewrite rehearsal", string(args))
	}
	expectedVersion := int64(3)
	if reviewed != nil {
		expectedVersion = 5
	}
	if paused.Checkpoint.Version != expectedVersion || paused.Checkpoint.Status != "paused" || paused.Checkpoint.Approval == nil || paused.Checkpoint.Approval.State != "asked" || (reviewed == nil && paused.Checkpoint.Approval.Runtime == nil || reviewed != nil && (paused.Checkpoint.Approval.Runtime != nil || paused.Checkpoint.Approval.Source.Kind != "custom" || paused.Checkpoint.Approval.Request == nil)) || len(peer.calls) != 0 || len(sourceCalls) != 0 {
		t.Fatalf("pause not acknowledged before physical dispatch: %+v calls=%d", paused.Checkpoint, len(peer.calls))
	}
	t.Logf("acknowledged pause cp=%d status=%s approval=%s run=%s call=%s Runtime=%s physicalCalls=%d enrollments=%d", paused.Checkpoint.Version, paused.Checkpoint.Status, paused.Checkpoint.Approval.State, paused.Checkpoint.ID, asked.CallID, string(mustAssistantRuntimeOwnerJSON(asked.Runtime)), len(peer.calls), enrollments.Load())
	if engine == "native" && (releases.Load() != 1 || callbacks.Load() != 1) {
		t.Fatal("actual Manager pause did not release and settle exactly once")
	}
	// The supported callback-error route leaves the original writer live after
	// acknowledging the exact paused head; no cancellation compatibility is used.
	if ctx.Err() != nil || deskWriter.Err() != nil || originalWriter.Failed() || paused.Checkpoint.Durable == nil || paused.Checkpoint.Durable.PendingHandoff != nil || !sameAssistantRunCheckpoint(originalWriter.Checkpoint(), paused.Checkpoint) {
		t.Fatal("pause settlement or final canonical acknowledgement is uncertain", ctx.Err(), deskWriter.Err())
	}
	if engine == "native" && callbacksJoined.Load() != 1 {
		t.Fatal("native callback remains unjoined")
	}
	// Agent v0.13.2 records a nonnil waiting callback outcome as unknown even
	// after the package acknowledges the pause. Only this read agent receipt
	// may remain unknown; an uncertain host/Runtime effect never qualifies.
	mu.Lock()
	pauseErr := callbackErr
	mu.Unlock()
	pauseFailure := failure.Normalize(pauseErr, "tool", time.Time{})
	var storageUnknown *runcontext.StorageOutcomeUnknown
	var releaseUnknown *runcontext.ProviderReleaseUnknown
	knownCallbackPause := engine == "native" && pauseErr != nil && !failure.IsCancelled(pauseErr) && !errors.As(pauseErr, &storageUnknown) && !errors.As(pauseErr, &releaseUnknown) && pauseFailure.Code == "unknown" && pauseFailure.Source == "tool" && pauseFailure.Outcome == "unknown" && paused.Checkpoint.Status == "paused" && paused.Checkpoint.Approval != nil && paused.Checkpoint.Approval.State == "asked" && !originalWriter.Failed() && releases.Load() == 1 && callbacks.Load() == 1 && callbacksJoined.Load() == 1 && invocations.Load() == 1 && len(peer.calls) == 0 && len(sourceCalls) == 0
	unknownPauses := 0
	for _, dispatch := range paused.Checkpoint.Durable.Dispatches {
		if dispatch.State == "unknown" {
			unknownPauses++
		}
		if unknownPauses > 1 || dispatch.State == "claimed" || dispatch.State == "unknown" && (!knownCallbackPause || dispatch.Kind != "agent" || !dispatch.Read || dispatch.Failure == nil || !reflect.DeepEqual(*dispatch.Failure, pauseFailure)) || dispatch.Failure != nil && dispatch.Failure.Detail != nil && strings.HasPrefix(*dispatch.Failure.Detail, "ProviderReleaseUnknown:") {
			t.Fatal("unsettled durable provider/callback dispatch", dispatch)
		}
	}
	t.Logf("settled pause revision=%s cursor=%s commit=%s commits=%d checkpoint=%s", paused.Canonical.Head.Revision, paused.Canonical.Head.Cursor, paused.Canonical.Head.CommitID, len(paused.Canonical.Commits), mustAssistantRuntimeOwnerJSON(paused.Checkpoint))
	assistantRunMust(t, originalWriter.UseAcknowledgedRoot(*original))
	assistantRunMust(t, originalWriter.CloseDispatch(*original))
	stopAtBarrier := func(expected *runcontext.RunBinding, discard *runcontext.RunBinding) {
		t.Helper()
		entered, stopped := make(chan struct{}), make(chan error, 1)
		go func() {
			close(entered)
			execution.closeCanonicalDispatch()
			stopped <- execution.finalizeCanonicalStop()
		}()
		<-entered
		assistantRunMust(t, <-stopped)
		if discard != nil {
			assistantRunMust(t, discard.Writer.CloseDispatch(*discard))
		}
		current, err := store.Read(ctx, binding, record.ID)
		assistantRunMust(t, err)
		canonicalActivationCheck(t, current)
		if current.Checkpoint.Status != "stopped" || execution.canonicalBinding != expected || !expected.Writer.Failed() || !execution.stopRequested || len(peer.calls) != 0 || enrollments.Load() != 1 || releases.Load() != 1 || invocations.Load() != 1 {
			t.Fatal("Stop barrier lost current-writer custody or dispatched a successor", scenario)
		}
		t.Logf("Stop barrier=%s finalizer=%p original=%p discarded=%p head=%s physicalCalls=%d enrollments=%d", scenario, expected.Writer, originalWriter, discard, current.Canonical.Head.Revision, len(peer.calls), enrollments.Load())
	}
	if scenario == "parked" {
		stopAtBarrier(original, nil)
		return
	}
	shown := runcontext.ApprovalShownDigest(*paused.Checkpoint.Approval)
	if reviewed != nil {
		if runcontext.ApprovalShownArguments(*paused.Checkpoint.Approval) != reviewed.Request || runcontext.ApprovalEventArguments(*asked) != reviewed.Request || shown != reviewed.Digest {
			t.Fatal("stored card/event differs from the handwritten request")
		}
		// A pre-upgrade selector/missing render proof refuses before any write.
		yes := true
		old := assistantRunAction{ArgsDigest: digestOf(args), Approved: &yes}
		if assistantRunApprovalMatches(paused.Checkpoint.Approval, old) || len(peer.calls) != 0 || len(sourceCalls) != 0 {
			t.Fatal("old approval reference admitted an effect")
		}
		old.ArgsDigest = shown
		if assistantRunApprovalMatches(paused.Checkpoint.Approval, old) {
			t.Fatal("server reference substituted for rendering proof")
		}
		old.ShownDigest = &shown
		if !assistantRunApprovalMatches(paused.Checkpoint.Approval, old) {
			t.Fatal("refreshed card cannot be approved")
		}
	}
	note := "Explicit fixture owner decision"
	fresh := &runcontext.Input{Authorize: authorize, CurrentRequest: &runcontext.CurrentRequest{Message: "Review", Input: "exact original"}, RunStore: &runcontext.DurableRunInput{Store: originalStore, ID: originalWriter.ID, Resume: true}, Approval: &contract.CheckpointApprovalDecision{CallID: asked.CallID, Approved: true, Note: &note, ShownDigest: &shown}}
	if scenario == "lease" {
		assistantRunMust(t, lease.Close())
	}
	if scenario == "revocation" {
		cancel()
	}
	if scenario == "phase" {
		fresh.Phase = "plan"
	}
	if scenario == "source" {
		fresh.CurrentRequest.Input = "changed source"
	}
	if scenario == "decision" {
		fresh.Approval.CallID = "different-call"
	}
	resumed, err := runcontext.PrepareDurableInput(ctx, fresh)
	if scenario == "lease" {
		assistantRunMust(t, err)
		if resumed == nil || resumed.Persistence == nil || resumed.Persistence.Writer.Store != originalStore || resumed.Persistence.Writer.ID != originalWriter.ID || execution.canonicalBinding != original || len(peer.calls) != 0 || enrollments.Load() != 1 {
			t.Fatal("read-only successor changed original custody")
		}
		// Commit receives exact acknowledged bytes, with no phase/checkpoint/source
		// rewrite. Its actual production lease fence must refuse before any write.
		_, commitErr := originalStore.Commit(ctx, runcontext.RunCommit{RunID: originalStore.id, ExpectedRevision: paused.Canonical.Head.Revision, CommitID: "closed-lease-successor", Checkpoint: paused.Canonical.Head.Checkpoint, Events: [][]byte{}})
		authority, holdErr := holdAssistantRunPhysicalAuthority(execution)
		assistantRunMust(t, holdErr)
		physicallyWritten := false
		physicalErr := authority.write(ctx, func() error { physicallyWritten = true; return nil })
		dispatchErr := deskWriter.Dispatch(ctx, "no-physical-call")
		unchanged, readErr := store.Read(ctx, binding, record.ID)
		assistantRunMust(t, readErr)
		if !errors.Is(commitErr, errAssistantRunScope) || !errors.Is(dispatchErr, errAssistantRunScope) || !errors.Is(physicalErr, errAssistantRunScope) || physicallyWritten || !reflect.DeepEqual(unchanged.Canonical, paused.Canonical) || execution.canonicalBinding != original || len(peer.calls) != 0 || enrollments.Load() != 1 {
			t.Fatal("closed lease crossed actual Commit/Dispatch/physical admission", commitErr, dispatchErr, physicalErr)
		}
		t.Logf("read-only Prepare succeeded; actual Commit=%v writer.Dispatch=%v final-physical-admission=%v unchanged head=%s physicalCalls=0", commitErr, dispatchErr, physicalErr, unchanged.Canonical.Head.Revision)
		return
	}
	if scenario == "revocation" {
		if err == nil || len(peer.calls) != 0 || execution.canonicalBinding != original || enrollments.Load() != 1 {
			t.Fatal("lost lease/session prepared successor", scenario, err)
		}
		t.Logf("barrier=%s prepare=%T:%v physicalCalls=0", scenario, err, err)
		return
	}
	assistantRunMust(t, err)
	if resumed == nil || resumed.Persistence == nil || resumed.Persistence.Writer == originalWriter || resumed.Persistence.Writer.Store != originalStore || resumed.Persistence.Writer.ID != originalWriter.ID || !sameAssistantRunCheckpoint(resumed.Execution, paused.Checkpoint) {
		t.Fatal("published same-store preparation did not preserve exact canonical authority")
	}
	prepared := resumed.Persistence
	if scenario == "prepared" {
		stopAtBarrier(original, prepared)
		return
	}
	// This fixture has one synchronous owner; installation still shares Stop's
	// actual binding mutex and rechecks the retained source head and lease.
	execution.canonicalMu.Lock()
	latest, installErr := store.Read(ctx, binding, record.ID)
	if installErr == nil && (execution.stopRequested || execution.canonicalBinding != original || ctx.Err() != nil || latest.Canonical.Head.Revision != paused.Canonical.Head.Revision || deskWriter.lease != lease || originalStore.writer != deskWriter) {
		installErr = errors.New("owner changed before current binding installation")
	}
	if installErr == nil {
		execution.canonicalBinding = prepared
	}
	execution.canonicalMu.Unlock()
	assistantRunMust(t, installErr)
	if scenario == "installed" {
		stopAtBarrier(prepared, nil)
		return
	}
	if scenario == "late-callback" {
		before, err := store.Read(ctx, binding, record.ID)
		assistantRunMust(t, err)
		if lateCallback == nil {
			t.Fatal("actual callback was not retained")
		}
		lateErr := lateCallback()
		after, err := store.Read(ctx, binding, record.ID)
		assistantRunMust(t, err)
		if lateErr == nil || !reflect.DeepEqual(before.Canonical, after.Canonical) || len(peer.calls) != 0 || enrollments.Load() != 1 || execution.canonicalBinding != prepared {
			t.Fatal("late old-generation callback affected successor", lateErr)
		}
		t.Logf("actual late callback refused=%T:%v headUnchanged=%s physicalCalls=0", lateErr, lateErr, after.Canonical.Head.Revision)
		return
	}
	if scenario == "authority" {
		authorityLive = false
	}
	if scenario == "owner" {
		owner.ProcessID = "changed-owner"
	}
	if scenario == "list" {
		changed := "Changed unselected sibling"
		peer.tools[1].Description = &changed
	}
	resumeEventStart := len(events)
	beforeRuntime, _ := json.Marshal(paused.Checkpoint.Approval.Runtime)
	run, implementation = makeRun(resumed, true)
	resumeErr := implementation.Run(ctx, run)
	mu.Lock()
	t.Logf("resume evidence engine=%s adapter=%T:%v callback=%T:%v Manager=%T:%v release=%d callbacks=%d joined=%d invocations=%d newBinding=%p newWriter=%p originalClosed=%t", engine, resumeErr, resumeErr, callbackErr, callbackErr, managerErr, managerErr, releases.Load(), callbacks.Load(), callbacksJoined.Load(), invocations.Load(), prepared, prepared.Writer, originalWriter.Failed())
	mu.Unlock()
	if scenario == "authority" || scenario == "owner" || scenario == "list" || scenario == "phase" || scenario == "source" || scenario == "decision" {
		refused := resumeErr != nil
		for _, event := range events[resumeEventStart:] {
			refused = refused || event.Error != nil || event.ToolResult != nil && event.ToolResult.IsError
		}
		if !refused || len(peer.calls) != 0 || execution.canonicalBinding != prepared || originalStore.writer != deskWriter || deskWriter.lease != lease {
			t.Fatal("changed authority/source did not refuse before physical dispatch", scenario, resumeErr)
		}
		t.Logf("authority barrier=%s refused=%t physicalCalls=0", scenario, refused)
		return
	}
	assistantRunMust(t, resumeErr)
	if resumed.Persistence != prepared || execution.canonicalBinding != prepared || execution.writer != deskWriter || originalStore.writer != deskWriter || deskWriter.lease != lease || run.Gate != calls || enrollments.Load() != 2 || !originalWriter.Failed() {
		t.Fatal("retained original authority or fresh enrollment lost")
	}
	if engine == "native" && (releases.Load() != 2 || callbacks.Load() != 1 || callbacksJoined.Load() != 1 || invocations.Load() != 2) {
		t.Fatal("actual Manager continuation did not release exactly once per invocation")
	}
	done, err := store.Read(ctx, binding, record.ID)
	assistantRunMust(t, err)
	canonicalActivationCheck(t, done)
	// Actual Go/native continuation also feeds the person-visible projection.
	// This assertion does not claim that the native model received that result.
	if engine == "native" {
		state := runcontext.MustParseJSON(string(assistantChatJSON(done.Checkpoint))).Get("planState")
		if state.Has("taskSchedule") || state.Has("taskEnvelope") {
			t.Fatal("actual native answered approval acquired package task scheduling")
		}
	}
	display := assistantApprovalResults(done)
	expectedDisplay := assistantApprovalResultViewOf(done.Checkpoint)
	if expectedDisplay == nil || len(display) != 1 || !reflect.DeepEqual(display[0], *expectedDisplay) || !display[0].Result.Available || len(display[0].Result.Text) == 0 {
		t.Fatal("actual continuation lost original person-visible approval result text")
	}
	afterRuntime, _ := json.Marshal(done.Checkpoint.Approval.Runtime)
	if done.Checkpoint.ID != paused.Checkpoint.ID || !reflect.DeepEqual(beforeRuntime, afterRuntime) || done.Checkpoint.Approval.State != "answered" || (reviewed == nil && (len(peer.calls) != 1 || len(sourceCalls) != 0) || reviewed != nil && (len(peer.calls) != 0 || len(sourceCalls) != 1)) {
		t.Fatal("exact original approval continuation lost")
	}
	if reviewed == nil && !reflect.DeepEqual(assistantRuntimeOwnerJSON(t, peer.calls[0]), assistantRuntimeOwnerJSON(t, []byte(`{"name":"experimental_evaluate","arguments":{"value":7,"rehearsal":true}}`))) {
		t.Fatal("physical approved arguments changed", string(peer.calls[0]))
	}
	if reviewed != nil {
		if string(sourceCalls[0]) != reviewed.Request || runcontext.ApprovalShownArguments(*done.Checkpoint.Approval) != reviewed.Request {
			t.Fatal("displayed, approved and physically executed source text differ", string(sourceCalls[0]))
		}
	}
	head, err := runcontext.ParseJSON(done.Canonical.Head.Checkpoint)
	assistantRunMust(t, err)
	wantedKind := "runtime"
	if reviewed != nil {
		wantedKind = "host"
	}
	effectClaims := 0
	for _, d := range head.Get("durable").Get("dispatches").Items() {
		if d.Get("kind").Str() == wantedKind {
			effectClaims++
			if reviewed != nil && d.Get("request").Str() != reviewed.Request {
				t.Fatal("source dispatch row lost exact request")
			}
			if d.Get("read").Truth() || d.Get("state").Str() != "result" {
				t.Fatal("acting dispatch claim lost result/read=false")
			}
		}
	}
	if effectClaims != 1 {
		t.Fatal("original store did not retain exactly one effect claim", effectClaims)
	}
}

func assistantRuntimeOwnerJSON(t *testing.T, raw []byte) any {
	t.Helper()
	var value any
	assistantRunMust(t, json.Unmarshal(raw, &value))
	return value
}
func mustAssistantRuntimeOwnerJSON(value any) []byte {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return raw
}
func assistantRuntimeOwnerResponse(tool bool, argumentText ...string) *http.Response {
	delta := map[string]any{"content": "Evaluation completed."}
	finish := "stop"
	if tool {
		args := `{"value":7,"rehearsal":false}`
		if len(argumentText) > 0 {
			args = argumentText[0]
		}
		name := "experimental_evaluate"
		if len(argumentText) > 1 {
			name = argumentText[1]
		}
		delta = map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "runtime-call", "type": "function", "function": map[string]any{"name": name, "arguments": args}}}}
		finish = "tool_calls"
	}
	raw := mustAssistantRuntimeOwnerJSON(map[string]any{"id": "fixture", "object": "chat.completion.chunk", "model": "fixture", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}})
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + string(raw) + "\n\ndata: [DONE]\n\n"))}
}

// A test-binary native peer, reached only through the actual Manager. Its
// catalog comes from the test-owned Manager profile, never the installed CLI.
func TestAssistantRuntimeOwnerNativePeer(t *testing.T) {
	if os.Getenv("JPS_RUNTIME_OWNER_NATIVE_PEER") != "1" {
		return
	}
	trace := os.Getenv("JPS_RUNTIME_OWNER_TRACE")
	write := func(value any) { raw, _ := json.Marshal(value); fmt.Println(string(raw)) }
	log := func(method string) {
		f, err := os.OpenFile(trace, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			os.Exit(81)
		}
		_, _ = fmt.Fprintf(f, "%s\n", method)
		_ = f.Close()
	}
	var request struct {
		Model        string                               `json:"model"`
		DynamicTools []struct{ Name, Description string } `json:"dynamicTools"`
	}
	scan := bufio.NewScanner(os.Stdin)
	scan.Buffer(make([]byte, 4096), 2<<20)
	for scan.Scan() {
		var q struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scan.Bytes(), &q) != nil {
			os.Exit(82)
		}
		reply := func(result any) { write(map[string]any{"id": q.ID, "result": result}) }
		switch q.Method {
		case "initialize":
			reply(map[string]any{"userAgent": "jps_desk/0.157.1 (Linux)"})
		case "initialized":
		case "account/read":
			reply(map[string]any{"requiresOpenaiAuth": true, "account": map[string]any{"type": "chatgpt", "planType": "plus"}})
		case "model/list":
			var params struct {
				Hidden bool `json:"includeHidden"`
			}
			_ = json.Unmarshal(q.Params, &params)
			if params.Hidden {
				raw, err := os.ReadFile(filepath.Join(os.Getenv("CODEX_HOME"), "model-catalog.json"))
				if err != nil {
					os.Exit(83)
				}
				var catalog struct {
					Models []struct {
						Slug string `json:"slug"`
					} `json:"models"`
				}
				if json.Unmarshal(raw, &catalog) != nil {
					os.Exit(84)
				}
				rows := []map[string]any{}
				for _, model := range catalog.Models {
					rows = append(rows, map[string]any{"model": model.Slug})
				}
				reply(map[string]any{"data": rows})
			} else {
				reply(map[string]any{"data": []any{map[string]any{"model": "fixture", "displayName": "fixture", "defaultReasoningEffort": "medium", "supportedReasoningEfforts": []any{map[string]string{"reasoningEffort": "medium"}}}}})
			}
		case "thread/start":
			if json.Unmarshal(q.Params, &request) != nil {
				os.Exit(85)
			}
			cwd, _ := os.Getwd()
			reply(map[string]any{"thread": map[string]string{"id": "thread"}, "model": request.Model, "modelProvider": "openai", "approvalPolicy": "never", "cwd": cwd})
		case "turn/start":
			prior, _ := os.ReadFile(trace)
			log("generation")
			reply(map[string]any{"turn": map[string]string{"id": "turn"}})
			if strings.Contains(string(prior), "generation") {
				write(map[string]any{"method": "item/completed", "params": map[string]any{"threadId": "thread", "turnId": "turn", "item": map[string]string{"id": "final", "type": "agentMessage", "phase": "final_answer", "text": "Evaluation completed."}}})
				write(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "thread", "turn": map[string]string{"id": "turn", "status": "completed"}}})
			} else {
				name := ""
				for _, tool := range request.DynamicTools {
					if os.Getenv("JPS_RUNTIME_OWNER_SOURCE") == "1" && strings.Contains(tool.Description, "Write the exact reviewed note.") || os.Getenv("JPS_RUNTIME_OWNER_SOURCE") == "" && strings.HasPrefix(tool.Description, "experimental_evaluate: ") {
						name = tool.Name
					}
				}
				if name == "" {
					os.Exit(86)
				}
				arguments := json.RawMessage(`{"value":7,"rehearsal":false}`)
				if text := os.Getenv("JPS_RUNTIME_OWNER_ARGS"); text != "" {
					arguments = json.RawMessage(text)
				}
				write(map[string]any{"id": "runtime-rpc", "method": "item/tool/call", "params": map[string]any{"threadId": "thread", "turnId": "turn", "callId": "runtime-call", "tool": name, "arguments": arguments}})
			}
		case "":
			log("answer")
		default:
			os.Exit(87)
		}
	}
	os.Exit(0)
}
