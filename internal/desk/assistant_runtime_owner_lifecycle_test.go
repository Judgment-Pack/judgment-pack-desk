package desk

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

type assistantP2Executor struct{ *assistantProtocolExecutor }

func (*assistantP2Executor) assistantCanonicalRuns() {}

type assistantP2Fixture struct {
	server            *Server
	token             string
	start             assistantRunStart
	execution         *assistantRunExecution
	original          *runcontext.RunBinding
	invocation        context.Context
	view              assistantRunView
	cleanups, resumed atomic.Int32
	proceed           chan struct{}
	chat              *runcontext.ChatContext
	runtime           *runcontext.RuntimeIdentity
	heldRuntime       *assistantRuntime
}

// This deterministic executor exercises the actual service and public original
// Store/ID writer route. Settlement is explicit fixture evidence; it does not
// qualify production provider callbacks or Runtime process/Gate retention.
func assistantP2Park(t *testing.T, runtime bool, proof assistantInvocationSettlement, omitRuntime ...bool) *assistantP2Fixture {
	t.Helper()
	f := &assistantP2Fixture{proceed: make(chan struct{}), chat: runcontext.NewChatContext(""), runtime: &runcontext.RuntimeIdentity{Identity: "fixture-runtime", Version: "1.0.0"}}
	opened := make(chan error, 1)
	s, token, start, base := assistantProtocolFixture(t, func(ctx context.Context, e *assistantRunExecution) (result error) {
		defer func() { opened <- result }()
		input := &runcontext.Input{Chat: f.chat, Runtime: f.runtime}
		if err := e.bindCanonicalInput(ctx, input); err != nil {
			return err
		}
		f.execution = e
		f.original, f.invocation = input.Persistence, context.WithoutCancel(ctx)
		if runtime && (len(omitRuntime) == 0 || !omitRuntime[0]) {
			var err error
			f.heldRuntime, err = e.assistantRuntime(ctx)
			if err != nil {
				return err
			}
		}
		cp := assistantProtocolAsked(*input.Persistence.Writer.Checkpoint())
		cp.Status = "paused"
		if runtime {
			pin := "sha256:" + strings.Repeat("a", 64)
			cp.Approval.Source = contract.ToolOfferSourceRecord{Kind: "runtime", ID: "runtime", Trust: "first-party"}
			cp.Version = 3
			name := "write_file"
			cp.Approval.Source.Tool = &name
			cp.Approval.Source.ToolListDigest = &pin
			cp.Approval.Source.Identity = &contract.ToolOfferIdentity{Kind: "connection", Label: "Fixture principal"}
			cp.Approval.Source.IdentityDigest = &pin
			cp.Approval.Runtime = &contract.CheckpointRuntimeApprovalBinding{AdmissionDigest: pin, Owner: contract.CheckpointRuntimeExecutionOwner{ArtifactDigest: pin, ProcessID: "fixture-launch", ProjectID: "fixture-project", ProjectLabel: "Fixture project", SessionID: "fixture-session", PrincipalDigest: pin, PrincipalLabel: "Fixture principal", ScopeDigest: pin, ScopeLabel: "Fixture scope"}}
			if f.heldRuntime != nil {
				cp.Approval.Runtime.Owner = f.heldRuntime.owner.identity
			}
		}
		if err := input.Persistence.Writer.Save(ctx, *input.Persistence, cp); err != nil {
			return err
		}
		err := e.settleRuntimeInvocation(ctx, proof, func(ctx context.Context, next *runcontext.Input) error {
			f.resumed.Add(1)
			if next.Persistence == f.original || next.Persistence.Writer == f.original.Writer || next.Persistence.Writer.Store != f.original.Writer.Store || next.Persistence.Writer.ID != f.original.Writer.ID || next.Approval == nil || !next.Approval.Approved || next.Approval.CallID != "call-one" || next.Chat != f.chat || next.Runtime != f.runtime {
				return errors.New("continuation changed original authority or logical Chat/Runtime")
			}
			select {
			case <-f.proceed:
			case <-ctx.Done():
				return ctx.Err()
			}
			cp := *next.Persistence.Writer.Checkpoint()
			cp.Status = "running"
			cp.Approval.State = "approved"
			at := "2026-10-28T23:00:00Z"
			cp.Approval.DecidedAt = &at
			if err := next.Persistence.Writer.Save(ctx, *next.Persistence, cp); err != nil {
				return err
			}
			cp.Status = "complete"
			cp.Approval.State = "answered"
			if err := json.Unmarshal([]byte(`{"content":[{"type":"text","text":"exact fixture result"}]}`), &cp.Approval.Result); err != nil {
				return err
			}
			return next.Persistence.Writer.Save(ctx, *next.Persistence, cp)
		}, func() { f.cleanups.Add(1) })
		return err
	})
	f.server, f.token, f.start = s, token, start
	s.assistantRunExecutor = &assistantP2Executor{base}
	if runtime && (len(omitRuntime) == 0 || !omitRuntime[0]) {
		assistantP2ConfigureRuntime(t, s)
	}
	view := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
	select {
	case err := <-opened:
		assistantRunMust(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("executor did not acknowledge fixture pause")
	}
	// An explicit service notification signals parked; owner.done stays open.
	h := s.assistantRunService()
	wake, unsubscribe, err := h.subscribe(view.ID)
	assistantRunMust(t, err)
	defer unsubscribe()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		f.execution.canonicalMu.Lock()
		state := f.execution.lifecycle
		f.execution.canonicalMu.Unlock()
		if state == "parked" || state == "closed" {
			break
		}
		select {
		case <-wake:
		case <-deadline.C:
			t.Fatal("owner did not settle", state)
		}
	}
	f.view = assistantProtocolView(t, policyCall(s, token, "GET", "/api/assistant-runs/"+view.ID, nil), 200)
	t.Cleanup(func() { h.close(); f.chat.Dispose() })
	return f
}

// The existing P1 peer supplies the process and Linux cwd trampoline from
// copied test-owned bytes; no installed executable or shell is launched.
func assistantP2ConfigureRuntime(t *testing.T, s *Server) {
	t.Helper()
	exe, err := os.Executable()
	assistantRunMust(t, err)
	raw, err := os.ReadFile(exe)
	assistantRunMust(t, err)
	binaries := t.TempDir()
	bin := filepath.Join(binaries, "jpack")
	assistantP1Executable(t, bin, raw)
	assistantP1Executable(t, filepath.Join(binaries, "sh"), raw)
	t.Setenv("PATH", binaries)
	t.Setenv("JPS_RUNTIME_OWNER_P1_PEER", "1")
	t.Setenv(runtimeConfigEnv, "")
	t.Setenv(runtimeSigningKeyEnv, "")
	t.Setenv(runtimeSigningPublicKeyEnv, "")
	s.cfg.JpackBin = bin
	assistantRunMust(t, os.MkdirAll(filepath.Join(s.projectDir, "audit"), 0700))
	assistantRunMust(t, os.MkdirAll(filepath.Join(s.projectDir, "resources"), 0700))
	assistantRunMust(t, os.WriteFile(filepath.Join(s.projectDir, "resources", "fixture.json"), []byte(`{"fixture":true}`), 0600))
	assistantRunMust(t, os.WriteFile(filepath.Join(s.projectDir, "jpack.json"), []byte(`{"configVersion":"3","audit":{"dir":"audit"},"packs":{"fixture":{"path":"resources/fixture.json"}}}`), 0600))
}

func assistantP2Proof() assistantInvocationSettlement {
	return assistantInvocationSettlement{PauseAcknowledged: true, ReceiptsAcknowledged: true, CallbacksJoined: true, ProviderReleased: true, NoPendingWork: true}
}
func (f *assistantP2Fixture) approval() assistantRunAction {
	approved := true
	return assistantRunAction{ID: "fixture-decision", Kind: "approval", Revision: f.view.Revision, CallID: "call-one", ArgsDigest: f.view.ApprovalReference.ArgsDigest, ShownDigest: &f.view.ApprovalReference.ArgsDigest, Approved: &approved}
}

func TestAssistantRuntimeLifecycleServiceApproval(t *testing.T) {
	for _, runtime := range []bool{false, true} {
		name := "host"
		if runtime {
			name = "runtime"
		}
		if !t.Run(name, func(t *testing.T) {
			f := assistantP2Park(t, runtime, assistantP2Proof())
			e, h := f.execution, f.server.assistantRunService()
			if f.view.State != "awaiting-input" || f.view.Checkpoint.Status != "paused" {
				t.Fatal("approval misclassified", f.view.State)
			}
			h.mu.Lock()
			lease := h.lease
			h.releaseIdle()
			retained := h.active[f.view.ID] == e && h.lease == lease
			h.mu.Unlock()
			if !retained || e.ctx.Err() != nil {
				t.Fatal("parked owner or lease lost")
			}
			if runtime && (e.retainedRuntime != f.heldRuntime || e.runtimeOwner.Load() != f.heldRuntime || f.heldRuntime.ctx.Err() != nil) {
				t.Fatal("exact Runtime process lost at park")
			}
			select {
			case <-e.invocationDone:
			default:
				t.Fatal("invocation did not join")
			}
			select {
			case <-e.done:
				t.Fatal("owner completed while parked")
			default:
			}
			action := f.approval()
			assistantProtocolView(t, policyCall(f.server, f.token, "POST", "/api/assistant-runs/"+f.view.ID+"/actions", action), 200)
			assistantProtocolView(t, policyCall(f.server, f.token, "POST", "/api/assistant-runs/"+f.view.ID+"/actions", action), 200)
			// Retired writer uses an uncanceled context, exercising the generation fence.
			before, err := e.canonicalStore.read(t.Context())
			assistantRunMust(t, err)
			if err := e.publishCanonicalHostEvent(f.invocation, contract.RunEvent{Message: &contract.RunEventMessage{Type: "message", Text: "late"}}); err == nil {
				t.Fatal("retired generation host Event succeeded")
			}
			cp := *f.original.Writer.Checkpoint()
			if err := f.original.Writer.Save(f.invocation, *f.original, cp); err == nil {
				t.Fatal("retired generation Save succeeded")
			}
			after, err := e.canonicalStore.read(t.Context())
			assistantRunMust(t, err)
			if after.Canonical.Head.Revision != before.Canonical.Head.Revision {
				t.Fatal("late generation changed durable head")
			}
			close(f.proceed)
			assistantProtocolWait(t, f.server, f.token, f.view.ID, "complete")
			if f.resumed.Load() != 1 || f.cleanups.Load() != 1 {
				t.Fatal("duplicate continuation or cleanup", f.resumed.Load(), f.cleanups.Load())
			}
		}) {
			return
		}
	}
}

func TestAssistantRuntimeLifecycleStopBarriers(t *testing.T) {
	for _, barrier := range []string{"parked", "prepared", "installed", "closed-lease", "source-revoked"} {
		if !t.Run(barrier, func(t *testing.T) {
			f := assistantP2Park(t, false, assistantP2Proof())
			e, h := f.execution, f.server.assistantRunService()
			expected := f.original
			if barrier != "parked" {
				record, err := e.canonicalStore.read(t.Context())
				assistantRunMust(t, err)
				h.mu.Lock()
				reservation, err := e.reserveRuntimeContinuation(record)
				h.mu.Unlock()
				assistantRunMust(t, err)
				next, err := runcontext.PrepareDurableInput(t.Context(), &runcontext.Input{Chat: f.chat, RunStore: &runcontext.DurableRunInput{Store: reservation.store, ID: reservation.store.id, Resume: true}, Approval: f.approval().decisionP2()})
				assistantRunMust(t, err)
				if barrier == "closed-lease" {
					assistantRunMust(t, e.writer.lease.Close())
				}
				if barrier == "source-revoked" {
					saved := policyWorkspaceRead(t, f.server, f.token, "/api/conversations")
					response := policyWorkspaceCall(f.server, f.token, "PUT", "/api/conversations", saved.SHA256, map[string]any{"version": 1, "chats": []any{}})
					if response.Code != 200 {
						t.Fatal("source revocation failed", response.Code)
					}
				}
				if barrier == "installed" {
					assistantRunMust(t, e.installRuntimeContinuation(t.Context(), reservation, next))
					expected = next.Persistence
				}
				if barrier == "closed-lease" || barrier == "source-revoked" {
					if err := e.installRuntimeContinuation(t.Context(), reservation, next); err == nil {
						t.Fatal("invalid authority installed")
					}
					if e.canonicalBinding != f.original || f.resumed.Load() != 0 {
						t.Fatal("failed install enrolled")
					}
					e.cancel()
					return
				}
				defer next.Persistence.Writer.CloseDispatch(*next.Persistence)
			}
			latest := assistantProtocolView(t, policyCall(f.server, f.token, "GET", "/api/assistant-runs/"+f.view.ID, nil), 200)
			assistantProtocolView(t, policyCall(f.server, f.token, "POST", "/api/assistant-runs/"+f.view.ID+"/actions", assistantRunAction{ID: "stop-fixture", Kind: "cancel", Revision: latest.Revision}), 200)
			if e.canonicalBinding != expected || f.resumed.Load() != 0 || f.cleanups.Load() != 1 {
				t.Fatal("Stop selected wrong binding or enrolled continuation")
			}
			record, err := e.writer.lease.store.Read(t.Context(), e.record.Binding, e.record.ID)
			assistantRunMust(t, err)
			if record.State != "stopped" || record.Checkpoint.Status != "stopped" {
				t.Fatal("Stop did not finalize canonical head", record.State)
			}
		}) {
			return
		}
	}
}
func (a assistantRunAction) decisionP2() *contract.CheckpointApprovalDecision {
	return &contract.CheckpointApprovalDecision{CallID: a.CallID, Approved: *a.Approved, Note: a.Note, ShownDigest: a.ShownDigest}
}

func TestAssistantRuntimeLifecycleSettlementRefusal(t *testing.T) {
	for _, missing := range []string{"receipt", "callback", "provider", "pending"} {
		if !t.Run(missing, func(t *testing.T) {
			proof := assistantP2Proof()
			switch missing {
			case "receipt":
				proof.ReceiptsAcknowledged = false
			case "callback":
				proof.CallbacksJoined = false
			case "provider":
				proof.ProviderReleased = false
			case "pending":
				proof.NoPendingWork = false
			}
			f := assistantP2Park(t, false, proof)
			assistantProtocolWait(t, f.server, f.token, f.view.ID, "awaiting-input")
			if w := policyCall(f.server, f.token, "POST", "/api/assistant-runs/"+f.view.ID+"/actions", f.approval()); w.Code != 503 {
				t.Fatal("unqualified settlement resumed", w.Code, w.Body.String())
			}
			if f.resumed.Load() != 0 || f.cleanups.Load() != 1 {
				t.Fatal("uncertain pause enrolled or leaked owner")
			}
		}) {
			return
		}
	}
}

func TestAssistantRuntimeLifecycleRuntimeCustody(t *testing.T) {
	for _, scenario := range []string{"missing", "exit-parked", "exit-prepared", "phase"} {
		if !t.Run(scenario, func(t *testing.T) {
			f := assistantP2Park(t, true, assistantP2Proof(), scenario == "missing")
			e, h := f.execution, f.server.assistantRunService()
			if scenario == "missing" {
				assistantProtocolWait(t, f.server, f.token, f.view.ID, "awaiting-input")
				if w := policyCall(f.server, f.token, "POST", "/api/assistant-runs/"+f.view.ID+"/actions", f.approval()); w.Code != 503 {
					t.Fatal("missing Runtime owner resumed", w.Code)
				}
				return
			}
			if scenario == "phase" {
				record, err := e.canonicalStore.read(t.Context())
				assistantRunMust(t, err)
				record.Binding.Phase = "plan"
				h.mu.Lock()
				_, err = e.reserveRuntimeContinuation(record)
				h.mu.Unlock()
				if err == nil {
					t.Fatal("Plan reserved acting continuation")
				}
				return
			}
			var reservation *assistantRuntimeReservation
			var next *runcontext.Input
			if scenario == "exit-prepared" {
				record, err := e.canonicalStore.read(t.Context())
				assistantRunMust(t, err)
				h.mu.Lock()
				reservation, err = e.reserveRuntimeContinuation(record)
				h.mu.Unlock()
				assistantRunMust(t, err)
				next, err = runcontext.PrepareDurableInput(t.Context(), &runcontext.Input{Chat: f.chat, Runtime: f.runtime, RunStore: &runcontext.DurableRunInput{Store: reservation.store, ID: reservation.store.id, Resume: true}, Approval: f.approval().decisionP2()})
				assistantRunMust(t, err)
			}
			// Kill only the exact process launched by this fixture; the monitor must
			// notice even after the transport clears its atomic publication.
			assistantRunMust(t, f.heldRuntime.cmd.Process.Kill())
			select {
			case <-f.heldRuntime.waitDone:
			case <-time.After(3 * time.Second):
				t.Fatal("Runtime did not exit")
			}
			if next != nil {
				if err := e.installRuntimeContinuation(t.Context(), reservation, next); err == nil {
					t.Fatal("exited Runtime installed")
				}
				_ = next.Persistence.Writer.CloseDispatch(*next.Persistence)
			}
			select {
			case <-e.done:
			case <-time.After(6 * time.Second):
				t.Fatal("parked process exit did not retire owner")
			}
			if e.retainedRuntime != f.heldRuntime || e.canonicalBinding != f.original || f.resumed.Load() != 0 || f.cleanups.Load() != 1 {
				t.Fatal("lost retained pointer or enrolled after exit")
			}
		}) {
			return
		}
	}
}

func TestAssistantRuntimeLifecycleParkedCleanup(t *testing.T) {
	for _, mode := range []string{"abandon", "session", "policy", "shutdown"} {
		if !t.Run(mode, func(t *testing.T) {
			f := assistantP2Park(t, false, assistantP2Proof())
			e, h := f.execution, f.server.assistantRunService()
			originalStore, originalWriter, originalLease := e.canonicalStore, e.writer, e.writer.lease
			switch mode {
			case "abandon":
				result := assistantProtocolView(t, policyCall(f.server, f.token, "POST", "/api/assistant-runs/"+f.view.ID+"/actions", assistantRunAction{ID: "abandon-fixture", Kind: "abandon", Revision: f.view.Revision}), 200)
				if result.State != "stopped" {
					t.Fatal("abandon did not settle terminal receipt", result.State)
				}
			case "session":
				f.server.sessions.revoke(f.token)
			case "policy":
				f.server.signIn.mu.Lock()
				f.server.signIn.cancelEpoch()
				f.server.signIn.epoch, f.server.signIn.cancelEpoch = context.WithCancel(context.Background())
				f.server.signIn.generation++
				f.server.signIn.mu.Unlock()
			case "shutdown":
				h.close()
			}
			select {
			case <-e.done:
			case <-time.After(6 * time.Second):
				t.Fatal("parked owner did not retire", mode)
			}
			if e.canonicalStore != originalStore || e.writer != originalWriter || e.writer.lease != originalLease || f.cleanups.Load() != 1 || f.resumed.Load() != 0 {
				t.Fatal("cleanup replaced authority or repeated", mode)
			}
			h.mu.Lock()
			idle := len(h.active) == 0 && h.lease == nil
			h.mu.Unlock()
			if !idle {
				t.Fatal("parked cleanup retained slot or lease", mode)
			}
			record, err := originalLease.store.Read(t.Context(), e.record.Binding, e.record.ID)
			assistantRunMust(t, err)
			if record.State != "stopped" || record.Checkpoint.Status != "stopped" {
				t.Fatal("cleanup lost canonical stop", mode, record.State)
			}
			if mode == "abandon" && (len(record.Actions) != 1 || record.Actions[0].Kind != "abandon" || record.Actions[0].AuthorizingSession == "" || record.Actions[0].AuthorizingPolicy == "") {
				t.Fatal("abandon receipt lost authorizing identity")
			}
		}) {
			return
		}
	}
}
