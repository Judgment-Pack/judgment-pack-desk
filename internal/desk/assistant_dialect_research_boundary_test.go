package desk

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

type assistantResearchTestOwner struct {
	journal     researchJournal
	before      func(context.Context, *inputResearchRun) error
	after       func()
	acknowledge bool
	afterChange func(context.Context, *inputResearchRun) error
	calls       atomic.Int32
	queued      atomic.Int32
}

func (o *assistantResearchTestOwner) Run(string) researchJournal { return o }
func (o *assistantResearchTestOwner) Read(ctx context.Context) (inputResearchRun, error) {
	return o.journal.Read(ctx)
}
func (o *assistantResearchTestOwner) Create(ctx context.Context, r inputResearchRun) error {
	return o.journal.Create(ctx, r)
}
func (o *assistantResearchTestOwner) Update(ctx context.Context, change func(*inputResearchRun) error) (inputResearchRun, error) {
	o.calls.Add(1)
	result, err := o.journal.Update(ctx, func(r *inputResearchRun) error {
		if o.before != nil {
			if err := o.before(ctx, r); err != nil {
				return err
			}
		}
		err := change(r)
		if err == nil && r.State == "queued" {
			o.queued.Add(1)
		}
		if err == nil && o.afterChange != nil {
			return o.afterChange(ctx, r)
		}
		return err
	})
	if err == nil && o.after != nil {
		o.after()
		if !o.acknowledge {
			return inputResearchRun{}, errors.New("fixture acknowledgement lost")
		}
	}
	return result, err
}
func assistantResearchDirect(t *testing.T, e *assistantRunExecution, x *assistantExecution, ctx context.Context, call, raw string) runcontext.ToolResult {
	t.Helper()
	assistantPolicyEvent(t, e, call, assistantResearchTool, []byte(raw))
	ctx = context.WithValue(runcontext.WithCallID(ctx, call), assistantHostRootKey{}, true)
	result, err := assistantGraphFind(t, x.Run.HostTools, assistantResearchTool).Execute(ctx, []byte(raw))
	assistantRunMust(t, err)
	return result
}
func TestAssistantDialectResearchUnknownRecovery(t *testing.T) {
	var requests atomic.Int32
	testHookResearchRequest = func() { requests.Add(1) }
	t.Cleanup(func() { testHookResearchRequest = nil })
	for _, mode := range []string{"before-write", "lost-ack", "cancel-after-queue"} {
		t.Run(mode, func(t *testing.T) {
			s, token, e := assistantRecipeBrowserFixture(t, "http://127.0.0.1:1")
			saved, rr, run := assistantResearchSeed(t, s, token, e, true)
			x := assistantExecutionOpen(t, s, e)
			assistantRecordRead(t, e, x, saved)
			owner := &assistantResearchTestOwner{journal: s.researchJournal(rr, run.ID)}
			if mode == "before-write" {
				owner.before = func(context.Context, *inputResearchRun) error { return errors.New("fixture refused before commit") }
			} else {
				owner.after = func() {
					if mode == "cancel-after-queue" {
						e.cancel()
					}
				}
			}
			s.researchJournals = owner
			result := assistantResearchDirect(t, e, x, t.Context(), "unknown-request", assistantResearchArgs(saved))
			if !result.Err() {
				t.Fatal("unknown acknowledgement reported success")
			}
			if mode == "before-write" {
				got, err := owner.Read(t.Context())
				assistantRunMust(t, err)
				if got.State != "review" || got.Approval.UsedAt != "" || len(assistantExecutionRecord(t, e).DomainOutputs) != 0 {
					t.Fatal("pre-write refusal consumed allowance")
				}
				return
			}
			if !strings.Contains(result.Value().Text(), "outcome is unknown") {
				t.Fatal("unknown typed failure missing", result.Value().Text())
			}
			select {
			case <-e.done:
			case <-time.After(5 * time.Second):
				t.Fatal("unknown workflow did not stop chat")
			}
			used := assistantResearchUsed(t, s, rr, run)
			before := assistantExecutionRecord(t, e)
			requestCount := requests.Load()
			assistantRunMust(t, assistantRecipeImport(t, e, before))
			if len(before.DomainOutputs) != 0 || !assistantResearchUnobserved(before) {
				t.Fatal("unknown effect was fabricated as observed")
			}
			// Generic raw history/discovery/Stop cannot reconcile an owner receipt.
			for _, path := range []string{"/api/assistant-runs/" + before.ID, "/api/assistant-runs/" + before.ID + "/events", "/api/assistant-runs/owners/" + before.Binding.Owner.ID + "/requests/" + before.Binding.RequestID} {
				w := policyCall(s, token, "GET", path, nil)
				if w.Code != 200 {
					t.Fatal("raw historical read unavailable", path, w.Code, w.Body.String())
				}
			}
			if w := policyCall(s, token, "POST", "/api/assistant-runs/"+before.ID+"/actions", assistantRunAction{ID: "stopped-inspection", Kind: "cancel", Revision: before.Revision}); w.Code != 409 {
				t.Fatal("terminal Stop changed retained work", w.Code)
			}
			afterRaw := assistantExecutionRecord(t, e)
			if len(afterRaw.DomainOutputs) != 0 || owner.calls.Load() != 1 {
				t.Fatal("raw read reconciled/repeated workflow")
			}
			if err := s.prepareAssistantChatProjection(e.request.Clone(t.Context()), before.ID); err == nil {
				t.Fatal("unknown admission projected without explicit owner reconciliation")
			}
			// A changed current recipe must not invalidate the actual historical admission.
			doc := saved.Document
			doc.Name = "Changed after admitted research"
			_, err := s.preparationRepository().CompareAndSwap(t.Context(), doc, saved.SHA256)
			assistantRunMust(t, err)
			// Reconstruct only the explicit model-free terminal capability: no executor,
			// adapter, writer, request owner invocation or context continuation is present.
			reconstructed := &assistantRunExecution{service: &assistantRunService{server: s}, request: e.request.Clone(t.Context()), record: before}
			assistantRunMust(t, reconstructed.ProjectChat(t.Context()))
			assistantRunMust(t, reconstructed.ProjectChat(t.Context()))
			after := assistantExecutionRecord(t, e)
			assistantRunMust(t, assistantRecipeImport(t, e, after))
			if len(after.DomainOutputs) != 1 || !after.DomainOutputs[0].DialectAction.Research.Recovered || owner.calls.Load() != 1 || len(before.Operations) != len(after.Operations) || requests.Load() != requestCount {
				t.Fatal("reconciliation did not observe exactly one retained action")
			}
			if after.DomainOutputs[0].DialectAction.Action.Applied.At != used.Approval.UsedAt {
				t.Fatal("recovery invented receipt timestamp")
			}
			final := assistantResearchUsed(t, s, rr, run)
			if !bytesEqualResearch(used, final) {
				t.Fatal("chat cancellation/recovery changed independent worker")
			}
			// Stop belongs to the independent research owner and remains available.
			owner.after = nil
			w := assistantResearchHTTP(t, s, token, "/api/input-preparations/"+run.PreparationID+"/research/"+run.ID+"/stop", "", `{}`)
			if w.Code != 200 {
				t.Fatal("independent Stop unavailable", w.Code, w.Body.String())
			}
			stopped, err := owner.Read(t.Context())
			assistantRunMust(t, err)
			if !stopped.StopRequested {
				t.Fatal("independent worker stop not retained")
			}
		})
	}
}
func TestAssistantDialectResearchQueuedAuthority(t *testing.T) {
	for _, mode := range []string{"cancel", "revoke", "profile", "recipe", "expired", "critic"} {
		t.Run(mode, func(t *testing.T) {
			var s *Server
			var token string
			var e *assistantRunExecution
			if mode == "critic" {
				s, token, e, _ = assistantTestHostFixture(t, "http://127.0.0.1:1", "work", false, false, true)
			} else {
				s, token, e = assistantRecipeBrowserFixture(t, "http://127.0.0.1:1")
			}
			saved, rr, run := assistantResearchSeed(t, s, token, e, true)
			var x *assistantExecution
			if mode == "critic" {
				x = assistantTestHostOpen(t, s, e)
			} else {
				x = assistantExecutionOpen(t, s, e)
			}
			assistantRecordRead(t, e, x, saved)
			entered := make(chan struct{})
			release := make(chan struct{})
			owner := &assistantResearchTestOwner{journal: s.researchJournal(rr, run.ID), before: func(ctx context.Context, current *inputResearchRun) error {
				close(entered)
				select {
				case <-release:
				case <-ctx.Done():
					return ctx.Err()
				}
				if mode == "expired" {
					current.Approval.ApprovedAt = time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339Nano)
					current.Approval.ExpiresAt = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
				}
				return nil
			}}
			s.researchJournals = owner
			raw := assistantResearchArgs(saved)
			call := "queued-request"
			assistantPolicyEvent(t, e, call, assistantResearchTool, []byte(raw))
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			ctx = context.WithValue(runcontext.WithCallID(ctx, call), assistantHostRootKey{}, true)
			done := make(chan runcontext.ToolResult, 1)
			go func() {
				result, _ := assistantGraphFind(t, x.Run.HostTools, assistantResearchTool).Execute(ctx, []byte(raw))
				done <- result
			}()
			select {
			case <-entered:
			case result := <-done:
				t.Fatal("request refused before queue", result.Value().Text())
			case <-time.After(5 * time.Second):
				t.Fatal("owner queue not reached")
			}
			switch mode {
			case "cancel":
				cancel()
			case "revoke":
				s.sessions.revoke(token)
			case "critic":
				assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "one.json"), []byte(strings.Replace(assistantTestPack, `"id":"one"`, `"id":"one","description":"Changed during admission"`, 1)), 0600))
			case "profile":
				assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, assistantProfilePath), []byte(`{"profileVersion":1,"api":{"inherit":true},"projectInstructions":"changed"}`), 0600))
			case "recipe":
				doc := saved.Document
				doc.Name = "Changed during queue"
				_, err := s.preparationRepository().CompareAndSwap(t.Context(), doc, saved.SHA256)
				assistantRunMust(t, err)
			}
			close(release)
			select {
			case result := <-done:
				if !result.Err() {
					t.Fatal("queued invalid authority admitted", mode)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("queued authority did not drain")
			}
			got, err := owner.Read(t.Context())
			assistantRunMust(t, err)
			if got.State != "review" || got.Approval.UsedAt != "" || len(got.Attempts) != 0 || len(assistantExecutionRecord(t, e).DomainOutputs) != 0 {
				t.Fatal("queued invalid authority consumed allowance", mode)
			}
		})
	}
}
func TestAssistantDialectResearchPlanChildAndIntent(t *testing.T) {
	for _, mode := range []string{"plan", "child", "no-read", "stale-read", "foreign-read"} {
		t.Run(mode, func(t *testing.T) {
			phase := "work"
			if mode == "plan" {
				phase = "plan"
			}
			s, token, e := assistantExecutionFixture(t, "http://127.0.0.1:1", phase)
			saved, rr, run := assistantResearchSeed(t, s, token, e, false)
			x := assistantExecutionOpen(t, s, e)
			if mode != "no-read" {
				assistantRecordRead(t, e, x, saved)
			}
			if mode == "stale-read" {
				saved.SHA256 = strings.Repeat("b", 64)
			}
			if mode == "foreign-read" {
				saved.Document.ID = "prep_99999999999999999999999999999999"
			}
			registry, err := e.assistantDialectResearchTools()
			assistantRunMust(t, err)
			if len(registry.tools) != 1 || registry.tools[0].Effect != "workflow" || registry.tools[0].Source != nil || registry.tools[0].Approve != nil || registry.tools[0].Delegation != "" {
				t.Fatal("incorrect workflow declaration")
			}
			for _, tool := range x.Run.HostTools {
				if mode == "plan" && tool.Name == assistantResearchTool {
					t.Fatal("Plan offered workflow")
				}
			}
			raw := []byte(assistantResearchArgs(saved))
			ctx := runcontext.WithCallID(t.Context(), "direct-request")
			if mode != "child" {
				assistantPolicyEvent(t, e, "direct-request", assistantResearchTool, raw)
				ctx = context.WithValue(ctx, assistantHostRootKey{}, true)
			}
			result, err := registry.tools[0].Execute(ctx, raw)
			assistantRunMust(t, err)
			if !result.Err() || len(assistantExecutionRecord(t, e).DomainOutputs) != 0 {
				t.Fatal("unproven root workflow accepted", mode)
			}
			got, err := s.researchJournal(rr, run.ID).Read(t.Context())
			assistantRunMust(t, err)
			if got.State != "review" || got.Approval != nil {
				t.Fatal("refused workflow changed owner")
			}
		})
	}
}
func TestAssistantDialectResearchReceiptImport(t *testing.T) {
	s, token, e := assistantRecipeBrowserFixture(t, "http://127.0.0.1:1")
	saved, rr, run := assistantResearchSeed(t, s, token, e, true)
	x := assistantExecutionOpen(t, s, e)
	assistantRecordRead(t, e, x, saved)
	result := assistantRecipeInvoke(t, e, x, "request", assistantResearchTool, assistantResearchArgs(saved))
	if result.Err() {
		t.Fatal(result.Value().Text())
	}
	record := assistantMappingObserve(t, s, e, 1)
	for _, mode := range []string{"applied-run", "applied-at", "missing-owner-op", "request-turn", "request-scope", "request-key", "receipt-null", "receipt-run", "receipt-key-null", "receipt-key-null-pending", "host-result", "missing-output", "recovered-alias", "wrong-host", "foreign-output"} {
		t.Run(mode, func(t *testing.T) {
			mutated := assistantVariantClone(t, record)
			out := mutated.DomainOutputs[0].DialectAction
			switch mode {
			case "missing-output":
				mutated.DomainOutputs = nil
			case "host-result":
				for i := range mutated.Operations {
					op := &mutated.Operations[i]
					if op.Kind == "host" && op.Name == assistantResearchTool {
						r, _ := assistantHostJSON(map[string]string{"status": "started", "runId": "forged"})
						op.Result = assistantChatJSON(assistantHostResult{Result: r.Value().JSON()})
					}
				}
			case "applied-run":
				out.Action.Applied.RunID = "research_99999999999999999999999999999999"
			case "applied-at":
				out.Action.Applied.At = "2026-10-20T01:00:00Z"
			case "missing-owner-op":
				out.Research.OperationID = "missing"
			case "recovered-alias":
				out.Research.Recovered = true
			case "foreign-output":
				out.Read.Recipe = nil
			default:
				for i := range mutated.Operations {
					op := &mutated.Operations[i]
					if op.Name != assistantResearchOperation {
						continue
					}
					var in assistantResearchIntent
					assistantRunMust(t, readAssistantHostReceipt(op.Intent, &in))
					var decision assistantResearchDecision
					assistantRunMust(t, readAssistantHostReceipt(op.Result, &decision))
					switch mode {
					case "request-turn":
						in.ResearchRequest.TurnID = "foreign-turn"
					case "request-scope":
						in.ResearchRequest.ChatID = "foreign-chat"
					case "request-key":
						in.ResearchRequest.RequestKey = strings.Repeat("a", 64)
					case "wrong-host":
						in.HostOperationID = record.Operations[0].ID
					case "receipt-run":
						decision.Receipt.RunID = "research_99999999999999999999999999999999"
					case "receipt-key-null", "receipt-key-null-pending":
						decision.Receipt.RequestKey = ""
						out.Research.Receipt.RequestKey = ""
						if mode == "receipt-key-null-pending" {
							// This exact supported prefix occurs when owner observation is
							// durable before the host result/output acknowledgement.
							mutated.DomainOutputs = nil
							for j := range mutated.Operations {
								host := &mutated.Operations[j]
								if host.Kind == "host" && host.Name == assistantResearchTool {
									host.State = "dispatched"
									host.Result = nil
								}
							}
							events := mutated.Events[:0]
							for _, event := range mutated.Events {
								if event.Event.ToolResult != nil && event.Event.ToolResult.Name == assistantResearchTool {
									continue
								}
								events = append(events, event)
							}
							mutated.Events = events
							op.Result = assistantChatJSON(decision)
							assistantRunMust(t, assistantRecipeImport(t, e, mutated))
						}
						op.Result = []byte(strings.Replace(string(assistantChatJSON(decision)), `"usedAt":`, `"requestKey":null,"usedAt":`, 1))
						continue
					case "receipt-null":
						op.Result = []byte(`{"receipt":null,"refused":false}`)
						continue
					}
					op.Intent = assistantChatJSON(in)
					op.Result = assistantChatJSON(decision)
				}
			}
			if assistantRecipeImport(t, e, mutated) == nil {
				t.Fatal("malformed admission import accepted", mode)
			}
		})
	}
	used := assistantResearchUsed(t, s, rr, run)
	_, err := s.researchJournal(rr, run.ID).Update(t.Context(), func(r *inputResearchRun) error { r.Approval.RequestKey = strings.Repeat("e", 64); return nil })
	if err == nil {
		t.Fatal("used owner provenance rewritten")
	}
	after, err := s.researchJournal(rr, run.ID).Read(t.Context())
	assistantRunMust(t, err)
	if !reflect.DeepEqual(used, after) {
		t.Fatal("refused receipt mutation changed journal")
	}
}

type assistantResearchDelayRead struct {
	inputPreparationRead
	until time.Time
}

func (v assistantResearchDelayRead) Get(id string) (inputPreparationReply, error) {
	wait := time.NewTimer(time.Until(v.until) + time.Millisecond)
	defer wait.Stop()
	<-wait.C
	return v.inputPreparationRead.Get(id)
}

type assistantResearchDelayPreparation struct {
	inputPreparationRepository
	until time.Time
}

func (p assistantResearchDelayPreparation) Read(ctx context.Context) (inputPreparationRead, error) {
	v, err := p.inputPreparationRepository.Read(ctx)
	if err != nil {
		return nil, err
	}
	return assistantResearchDelayRead{v, p.until}, nil
}
func (p assistantResearchDelayPreparation) TryRead(ctx context.Context) (inputPreparationRead, error) {
	v, err := p.inputPreparationRepository.TryRead(ctx)
	if err != nil {
		return nil, err
	}
	return assistantResearchDelayRead{v, p.until}, nil
}
func TestAssistantDialectResearchExpiryAfterValidation(t *testing.T) {
	s, token, e := assistantRecipeBrowserFixture(t, "http://127.0.0.1:1")
	saved, rr, run := assistantResearchSeed(t, s, token, e, true)
	x := assistantExecutionOpen(t, s, e)
	assistantRecordRead(t, e, x, saved)
	until := time.Now().Add(150 * time.Millisecond)
	s.inputPreparations = assistantResearchDelayPreparation{s.preparationRepository(), until}
	owner := &assistantResearchTestOwner{journal: s.researchJournal(rr, run.ID), before: func(_ context.Context, r *inputResearchRun) error {
		r.Approval.ExpiresAt = until.UTC().Format(time.RFC3339Nano)
		return nil
	}}
	s.researchJournals = owner
	result := assistantResearchDirect(t, e, x, t.Context(), "expiring-request", assistantResearchArgs(saved))
	if !result.Err() || owner.queued.Load() != 0 {
		t.Fatal("expiry after validation attempted durable queued transition")
	}
	got, err := owner.Read(t.Context())
	assistantRunMust(t, err)
	if got.State != "review" || got.Approval.UsedAt != "" {
		t.Fatal("expired queued grant consumed")
	}
}
func TestAssistantDialectResearchPreparationCommitGuard(t *testing.T) {
	for _, injected := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "injected"}[injected], func(t *testing.T) {
			s, token, e := assistantRecipeBrowserFixture(t, "http://127.0.0.1:1")
			saved, rr, run := assistantResearchSeed(t, s, token, e, true)
			if injected {
				useMemoryPreparationFixture(t, s)
			}
			x := assistantExecutionOpen(t, s, e)
			assistantRecordRead(t, e, x, saved)
			entered := make(chan struct{})
			release := make(chan struct{})
			owner := &assistantResearchTestOwner{journal: s.researchJournal(rr, run.ID), afterChange: func(ctx context.Context, r *inputResearchRun) error {
				close(entered)
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}}
			s.researchJournals = owner
			call := "commit-request"
			raw := assistantResearchArgs(saved)
			assistantPolicyEvent(t, e, call, assistantResearchTool, []byte(raw))
			ctx := context.WithValue(runcontext.WithCallID(t.Context(), call), assistantHostRootKey{}, true)
			tool := assistantGraphFind(t, x.Run.HostTools, assistantResearchTool)
			done := make(chan runcontext.ToolResult, 1)
			go func() { r, _ := tool.Execute(ctx, []byte(raw)); done <- r }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("validated commit not reached")
			}
			doc := saved.Document
			doc.Name = "Concurrent replacement"
			bounded, cancel := context.WithTimeout(t.Context(), 75*time.Millisecond)
			_, err := s.preparationRepository().CompareAndSwap(bounded, doc, saved.SHA256)
			cancel()
			close(release)
			result := <-done
			if err == nil {
				t.Fatal("preparation replacement landed between validation and durable queue commit")
			}
			if result.Err() {
				t.Fatal("guarded original request refused", result.Value().Text())
			}
			assistantResearchUsed(t, s, rr, run)
			current, err := s.preparationRepository().Get(t.Context(), doc.ID)
			assistantRunMust(t, err)
			if current.SHA256 != saved.SHA256 {
				t.Fatal("timed-out replacement mutated source")
			}
			_, err = s.preparationRepository().CompareAndSwap(t.Context(), doc, saved.SHA256)
			assistantRunMust(t, err)
		})
	}
}

func TestAssistantDialectResearchKnownAdmissionCancellation(t *testing.T) {
	s, token, e := assistantRecipeBrowserFixture(t, "http://127.0.0.1:1")
	saved, rr, run := assistantResearchSeed(t, s, token, e, true)
	x := assistantExecutionOpen(t, s, e)
	assistantRecordRead(t, e, x, saved)
	owner := &assistantResearchTestOwner{journal: s.researchJournal(rr, run.ID), after: e.cancel, acknowledge: true}
	s.researchJournals = owner
	result := assistantResearchDirect(t, e, x, t.Context(), "known-cancel", assistantResearchArgs(saved))
	if result.Err() {
		t.Fatal("known admission lost after cancellation", result.Value().Text())
	}
	select {
	case <-e.done:
	case <-time.After(5 * time.Second):
		t.Fatal("chat cancellation did not finish")
	}
	used := assistantResearchUsed(t, s, rr, run)
	record := assistantMappingObserve(t, s, e, 1)
	if record.DomainOutputs[0].DialectAction.Action.Applied.At != used.Approval.UsedAt || record.DomainOutputs[0].DialectAction.Research.Recovered {
		t.Fatal("known cancellation replaced exact admission observation")
	}
	reconstructed := &assistantRunExecution{service: &assistantRunService{server: s}, request: e.request.Clone(t.Context()), record: record}
	assistantRunMust(t, reconstructed.ProjectChat(t.Context()))
	if owner.calls.Load() != 1 {
		t.Fatal("known admission projection invoked owner mutation")
	}
}
func TestAssistantDialectResearchProjectionOwnerProof(t *testing.T) {
	for _, mode := range []string{"missing", "different", "review-stale"} {
		t.Run(mode, func(t *testing.T) {
			s, token, e := assistantRecipeBrowserFixture(t, "http://127.0.0.1:1")
			saved, rr, run := assistantResearchSeed(t, s, token, e, mode != "review-stale")
			x := assistantExecutionOpen(t, s, e)
			assistantRecordRead(t, e, x, saved)
			result := assistantRecipeInvoke(t, e, x, "request", assistantResearchTool, assistantResearchArgs(saved))
			if result.Err() {
				t.Fatal(result.Value().Text())
			}
			record := assistantMappingObserve(t, s, e, 1)
			cp := *record.Checkpoint
			cp.Status = "complete"
			assistantRunMust(t, e.Checkpoint(t.Context(), cp))
			assistantRunMust(t, e.writer.State(t.Context(), "complete"))
			if mode == "review-stale" {
				doc := saved.Document
				doc.Name = "Fresh review required"
				_, err := s.preparationRepository().CompareAndSwap(t.Context(), doc, saved.SHA256)
				assistantRunMust(t, err)
			} else {
				original := s.researchJournal(rr, run.ID)
				s.researchJournals = assistantResearchProofOwner{original, mode}
			}
			if err := e.ProjectChat(t.Context()); err == nil {
				t.Fatal("projection inferred admission/review from stale or absent owner evidence", mode)
			}
			if w := policyCall(s, token, "GET", "/api/assistant-runs/"+record.ID, nil); w.Code != 200 {
				t.Fatal("domain proof gated raw historical read")
			}
		})
	}
}

type assistantResearchProofOwner struct {
	researchJournal
	mode string
}

func (p assistantResearchProofOwner) Run(string) researchJournal { return p }
func (p assistantResearchProofOwner) Read(ctx context.Context) (inputResearchRun, error) {
	r, err := p.researchJournal.Read(ctx)
	if err != nil {
		return r, err
	}
	if p.mode == "missing" {
		r.Approval = nil
	} else {
		used, _ := time.Parse(time.RFC3339Nano, r.Approval.UsedAt)
		a := *r.Approval
		a.UsedAt = used.Add(time.Millisecond).Format(time.RFC3339Nano)
		r.Approval = &a
	}
	return r, nil
}

func TestAssistantDialectResearchCanonicalWorkflow(t *testing.T) {
	s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
	_ = s
	registry, err := e.assistantDialectResearchTools()
	assistantRunMust(t, err)
	base := registry.tools[0]
	for _, field := range []string{"effect", "name", "source", "source-tool", "effect-class", "approval", "delegation", "context"} {
		t.Run(field, func(t *testing.T) {
			tool := base
			switch field {
			case "effect":
				tool.Effect = "act"
			case "name":
				tool.Name = "unimplemented_workflow"
			case "source":
				tool.Source = &runcontext.ToolSource{}
			case "source-tool":
				tool.SourceTool = "external"
			case "effect-class":
				tool.EffectClass = "act"
			case "approval":
				tool.Approval = "required"
			case "delegation":
				tool.Delegation = "read"
			case "context":
				tool.ContextPolicy = &runcontext.ToolContextPolicy{Immutable: true}
			}
			if _, err := newAssistantHostRegistry([]runcontext.HostTool{tool}); err == nil {
				t.Fatal("unimplemented/sourced/delegated workflow declaration accepted", field)
			}
		})
	}
}

func TestAssistantDialectResearchUsedTurn(t *testing.T) {
	s, token, e := assistantRecipeBrowserFixture(t, "http://127.0.0.1:1")
	_, rr, run := assistantResearchSeed(t, s, token, e, true)
	q := inputResearchRequest{ChatID: e.record.Binding.Owner.ID, PreparationID: run.PreparationID, Revision: run.Revision, TurnID: e.record.Binding.RequestID, RequestKey: strings.Repeat("a", 64)}
	first, err := s.requestResearch(e.request.WithContext(t.Context()), q, nil)
	assistantRunMust(t, err)
	if first.Status != "started" {
		t.Fatal("first reviewed request refused")
	}
	q.TurnID = "another-turn"
	other, err := s.requestResearch(e.request.WithContext(t.Context()), q, nil)
	assistantRunMust(t, err)
	if other.Status != "review-required" {
		t.Fatal("used receipt reassigned to another turn")
	}
	used := assistantResearchUsed(t, s, rr, run)
	if used.Approval.TurnID != e.record.Binding.RequestID {
		t.Fatal("original owner turn changed")
	}
}

func TestAssistantDialectResearchStaleReviewCompletion(t *testing.T) {
	s, token, e := assistantRecipeBrowserFixture(t, "http://127.0.0.1:1")
	saved, rr, run := assistantResearchSeed(t, s, token, e, false)
	x := assistantExecutionOpen(t, s, e)
	assistantRecordRead(t, e, x, saved)
	doc := saved.Document
	doc.Name = "Changed before unadmitted card completion"
	_, err := s.preparationRepository().CompareAndSwap(t.Context(), doc, saved.SHA256)
	assistantRunMust(t, err)
	result := assistantRecipeInvoke(t, e, x, "stale-review-request", assistantResearchTool, assistantResearchArgs(saved))
	record := assistantExecutionRecord(t, e)
	if !result.Err() || len(record.DomainOutputs) != 0 || assistantResearchUnobserved(record) {
		t.Fatal("stale unadmitted review card retained or classified as uncertain admission")
	}
	assistantRunMust(t, assistantRecipeImport(t, e, record))
	got, err := s.researchJournal(rr, run.ID).Read(t.Context())
	assistantRunMust(t, err)
	if got.State != "review" || got.Approval != nil {
		t.Fatal("stale review completion changed research")
	}
}

type assistantResearchObservedPreparation struct {
	inputPreparationRepository
	entered chan struct{}
}

func (p assistantResearchObservedPreparation) Read(ctx context.Context) (inputPreparationRead, error) {
	select {
	case p.entered <- struct{}{}:
	default:
	}
	return p.inputPreparationRepository.Read(ctx)
}
func (p assistantResearchObservedPreparation) TryRead(ctx context.Context) (inputPreparationRead, error) {
	select {
	case p.entered <- struct{}{}:
	default:
	}
	return p.inputPreparationRepository.TryRead(ctx)
}
func TestAssistantDialectResearchReadViewBusy(t *testing.T) {
	for _, injected := range []bool{false, true} {
		t.Run(map[bool]string{false: "file-writer", true: "injected-projection-read"}[injected], func(t *testing.T) {
			s, token, e := assistantRecipeBrowserFixture(t, "http://127.0.0.1:1")
			saved, rr, run := assistantResearchSeed(t, s, token, e, true)
			if injected {
				useMemoryPreparationFixture(t, s)
			}
			x := assistantExecutionOpen(t, s, e)
			assistantRecordRead(t, e, x, saved)
			repo := s.preparationRepository()
			var view inputPreparationRead
			var err error
			if injected {
				view, err = repo.Read(t.Context())
			} else {
				view, err = repo.(fileInputPreparationRepository).openTransaction(t.Context(), true, true)
			}
			assistantRunMust(t, err)
			defer view.Close()
			entered := make(chan struct{}, 1)
			s.inputPreparations = assistantResearchObservedPreparation{repo, entered}
			call := "busy-request"
			raw := []byte(assistantResearchArgs(saved))
			assistantPolicyEvent(t, e, call, assistantResearchTool, raw)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			ctx = context.WithValue(runcontext.WithCallID(ctx, call), assistantHostRootKey{}, true)
			done := make(chan runcontext.ToolResult, 1)
			tool := assistantGraphFind(t, x.Run.HostTools, assistantResearchTool)
			go func() { result, _ := tool.Execute(ctx, raw); done <- result }()
			select {
			case <-entered:
			case result := <-done:
				t.Fatal("owner view boundary not reached", result.Value().Text())
			case <-time.After(5 * time.Second):
				t.Fatal("owner did not reach contended view")
			}
			// The queued projection/user edit keeps its view. Admission must release
			// private rather than wait on it, so unrelated history/Stop remains usable.
			probe, finish := context.WithTimeout(t.Context(), 500*time.Millisecond)
			private, probeErr := s.privateDataLockContext(probe, true)
			finish()
			if private != nil {
				private.Close()
			}
			cancel()
			var result runcontext.ToolResult
			select {
			case result = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("busy refusal did not quiesce")
			}
			if probeErr != nil {
				t.Fatal("request waited on preparation while holding the private lock", probeErr)
			}
			if !result.Err() {
				t.Fatal("busy preparation was treated as admitted")
			}
			got, err := s.researchJournal(rr, run.ID).Read(t.Context())
			assistantRunMust(t, err)
			record := assistantExecutionRecord(t, e)
			if got.State != "review" || got.Approval.UsedAt != "" || len(record.DomainOutputs) != 0 || assistantResearchUnobserved(record) {
				t.Fatal("known busy refusal attempted admission or retained uncertainty")
			}
		})
	}
}
