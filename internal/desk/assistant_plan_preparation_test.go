package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

// Hand-authored retained removal, rather than a recording of update_plan. The
// source still carries the original person's accepted Plan and real resolver.
func assistantPreparationPending(t *testing.T, cp contract.Checkpoint) contract.Checkpoint {
	t.Helper()
	value := runcontext.MustParseJSON(string(assistantChatJSON(cp)))
	plan, state := value.Get("plan"), value.Get("planState")
	if plan.Len() != 2 || state.Get("sequence").Num() != 0 {
		t.Fatal("fixture needs original two-step acceptance")
	}
	candidate := runcontext.Array(plan.Items()[0].Clone())
	candidateRevision := "sha256:" + runcontext.ContextDigest(runcontext.CanonicalJSON(candidate))
	next := state.Clone()
	next.Set("revision", runcontext.String(candidateRevision))
	next.Set("sequence", runcontext.Number(1))
	next.Set("history", runcontext.Array(runcontext.ObjectOf("sequence", runcontext.Number(1), "previous", state.Get("revision").Clone(), "revision", runcontext.String(candidateRevision), "explanation", runcontext.String("Remove reviewed second step"), "diff", runcontext.Array(runcontext.ObjectOf("id", plan.Items()[1].Get("id").Clone(), "kind", runcontext.String("removed"), "before", plan.Items()[1].Clone(), "from", runcontext.Number(1))))))
	// The published validator reconstructs revisePlan plus appendPlanRecord;
	// the detached candidate carries its own departure journal and counter.
	departures := runcontext.Array(runcontext.ObjectOf("kind", runcontext.String("removed"), "step", plan.Items()[1].Get("id").Clone(), "index", runcontext.Number(1)))
	next.Set("departures", runcontext.Array(runcontext.ObjectOf("revision", runcontext.String(candidateRevision), "explanation", runcontext.String("Remove reviewed second step"), "changes", departures.Clone())))
	next.Set("departuresDiscarded", runcontext.Number(0))
	state.Set("pending", runcontext.ObjectOf("purpose", runcontext.String("plan-drift"), "callId", runcontext.String("exact-plan-decision"), "revision", state.Get("revision").Clone(), "candidate", runcontext.ObjectOf("plan", candidate, "planState", next), "explanation", runcontext.String("Remove reviewed second step"), "departures", departures))
	value.Set("status", runcontext.String("paused"))
	value.Set("digest", runcontext.String(runcontext.RecordDigest(value)))
	_, err := runcontext.ReadExecution(value)
	if err != nil {
		t.Fatalf("handwritten pending removal failed published ReadExecution: %v", err)
	}
	assistantRunMust(t, json.Unmarshal(value.JSON(), &cp))
	return cp
}

func assistantPreparationFixture(t *testing.T) (*Server, string, *assistantRunExecution, *atomic.Int32) {
	t.Helper()
	calls := &atomic.Int32{}
	u := newUpstream(t, func(http.ResponseWriter, *http.Request) { calls.Add(1) })
	s, token, e := assistantExecutionFixtureSource(t, u.server.URL, "work", nil, func(s *Server, token string, start assistantRunStart) assistantRunStart {
		start = assistantPlanPrepareSource(t)(s, token, start)
		original := s.assistantRunExecutor.(*assistantProtocolExecutor)
		s.assistantRunExecutor = &assistantPlanBindingExecutor{original}
		start, _ = assistantBindingHistorical(t, s, token, start)
		return start
	})
	// Direct published writer calls carry the service-admitted generation.
	ctx := e.generationContext(t.Context(), e.generation)
	input := &runcontext.Input{Execution: e.record.Checkpoint}
	assistantRunMust(t, e.bindCanonicalInput(ctx, input))
	_, err := runcontext.InvokeDurable(ctx, input.Persistence, "host", "fixture", "inspect", []byte(`{"exact":1.20}`), true, func([]byte) (runcontext.ToolResult, error) {
		return runcontext.ReadToolResult(runcontext.MustParseJSON(`{"content":[{"type":"text","text":"retained 1.20 and 1e2"}],"structuredContent":{"number":1.20,"exponent":1e2}}`)), nil
	}, nil, nil)
	assistantRunMust(t, err)
	cp := assistantPreparationPending(t, *input.Persistence.Writer.Checkpoint())
	// Seed independently authored raw result bytes through the original store.
	// Only this fixture commit may write them; decision commits use the original
	// port directly so the assertion cannot repair a lossy implementation.
	store := input.Persistence.Writer.Store
	input.Persistence.Writer.Store = &assistantPreparationRawStore{RunStore: store}
	assistantRunMust(t, input.Persistence.Writer.Save(ctx, *input.Persistence, cp))
	input.Persistence.Writer.Store = store
	e.decision = &contract.CheckpointApprovalDecision{CallID: "exact-plan-decision", Approved: true}
	e.planDecision = assistantRunPlanReference(assistantExecutionRecord(t, e))
	// Runtime identity is fixture-owned. Its very first frame will fail; a model
	// endpoint remains available solely to count any accidental generation.
	assistantExecutionRuntimeFixture(t, s, e)
	peer := `#!/usr/bin/env python3
import json,sys
for line in sys.stdin:
 with open('plan-runtime-frame.json','w') as f: f.write(line)
 sys.exit(72)
`
	assistantRunMust(t, os.WriteFile(s.cfg.JpackBin, []byte(peer), 0700))
	return s, token, e, calls
}

func TestAssistantPlanPreparationBeforeRuntime(t *testing.T) {
	for _, approved := range []bool{true, false} {
		t.Run(map[bool]string{true: "accept", false: "reject"}[approved], func(t *testing.T) {
			s, _, e, calls := assistantPreparationFixture(t)
			ctx := e.generationContext(t.Context(), e.generation)
			e.decision.Approved = approved
			original := e.canonicalBinding
			before := assistantExecutionRecord(t, e)
			originalAccepted := assistantCheckpointAcceptedPlan(before.Checkpoint).JSON()
			originalDurable := assistantPreparationRawDurable(t, before.Canonical.Head.Checkpoint)
			if !bytes.Contains(originalDurable, []byte(assistantPreparationRawResult)) {
				t.Fatal("handwritten original numeric lexemes missing before decision")
			}
			x, err := e.openAssistantExecution(ctx, assistantExecutionPrompt{Name: "served"})
			if x != nil {
				x.Close()
				t.Fatal("failing Runtime unexpectedly opened")
			}
			if err == nil {
				t.Fatal("Runtime failure missing")
			}
			if _, err := os.Stat(filepath.Join(s.cfg.ProjectDir, "plan-runtime-frame.json")); err != nil {
				t.Fatal("Runtime was not attempted after acknowledgement", err)
			}
			after := assistantExecutionRecord(t, e)
			if !bytes.Equal(originalDurable, assistantPreparationRawDurable(t, after.Canonical.Head.Checkpoint)) {
				t.Fatal("decision changed retained completed results")
			}
			state := runcontext.MustParseJSON(string(assistantChatJSON(after.Checkpoint))).Get("planState")
			decisions := state.Get("decisions")
			if state.Get("pending") != nil || decisions.Len() != 1 || runcontext.CanonicalJSON(decisions.Items()[0].Get("approved")) != map[bool]string{true: "true", false: "false"}[approved] || !bytes.Equal(originalAccepted, assistantCheckpointAcceptedPlan(after.Checkpoint).JSON()) || original != e.canonicalBinding || calls.Load() != 0 {
				t.Fatal("decision not retained exactly before Runtime failure", err)
			}
			pending := runcontext.MustParseJSON(string(assistantChatJSON(before.Checkpoint))).Get("planState").Get("pending")
			row := decisions.Items()[0]
			if row.Get("revision").Str() != pending.Get("revision").Str() || row.Get("candidateRevision").Str() != pending.Get("candidate").Get("planState").Get("revision").Str() {
				t.Fatal("decided wrong transition")
			}
			count := 0
			for _, event := range after.Canonical.Events {
				if bytes.Contains(event.Bytes, []byte(`"plan_drift_decided"`)) {
					count++
				}
			}
			if count != 1 {
				t.Fatal("duplicate or missing durable Plan event", count)
			}
			// Later adapter preparation sees the exact returned checkpoint and consumed
			// decision; the original shared writer cannot append a second decision.
			input := &runcontext.Input{Execution: original.Writer.Checkpoint(), Persistence: original, Plan: runcontext.MustParseJSON(string(e.source.Plan.Snapshot)), PlanResolver: e.source.PlanResolver}
			prepared, err := e.prepareAssistantPlanDecision(ctx, input)
			assistantRunMust(t, err)
			if prepared != input || prepared.Approval != nil {
				t.Fatal("decision reintroduced")
			}
			prior := assistantExecutionRecord(t, e)
			inner := e.canonicalAdapter(adapter.Func(func(_ context.Context, run *adapter.Run) error {
				if run.Context != prepared || run.Context.Persistence != original {
					t.Fatal("adapter replaced prepared input")
				}
				return nil
			}), nil)
			assistantRunMust(t, inner.Run(ctx, &adapter.Run{Context: prepared}))
			again := assistantExecutionRecord(t, e)
			if !reflect.DeepEqual(prior.Canonical, again.Canonical) {
				t.Fatal("adapter preparation duplicated decision")
			}
		})
	}
}

func TestAssistantPlanPreparationRefusesBeforeRuntime(t *testing.T) {
	for _, fault := range []string{"missing-expectation", "stale-expectation", "no-plan", "no-resolver", "no-decision", "wrong-decision", "missing-pending", "revoked", "stale-head", "lost-ack"} {
		t.Run(fault, func(t *testing.T) {
			s, token, e, calls := assistantPreparationFixture(t)
			ctx := e.generationContext(t.Context(), e.generation)
			switch fault {
			case "missing-expectation":
				e.planDecision = nil
			case "stale-expectation":
				e.planDecision.CandidateRevision = e.planDecision.PlanRevision
			case "no-plan":
				e.source.Plan = nil
			case "no-resolver":
				e.source.PlanResolver = nil
			case "no-decision":
				e.decision = nil
			case "wrong-decision":
				e.decision.CallID = "different"
			case "missing-pending":
				cp := *e.canonicalBinding.Writer.Checkpoint()
				v := runcontext.MustParseJSON(string(assistantChatJSON(cp)))
				state := v.Get("planState")
				clean := runcontext.Object()
				for _, key := range state.Keys() {
					if key != "pending" {
						clean.Set(key, state.Get(key))
					}
				}
				v.Set("planState", clean)
				v.Set("status", runcontext.String("running"))
				v.Set("digest", runcontext.String(runcontext.RecordDigest(v)))
				assistantRunMust(t, json.Unmarshal(v.JSON(), &cp))
				assistantRunMust(t, e.canonicalBinding.Writer.Save(ctx, *e.canonicalBinding, cp))
			case "revoked":
				s.sessions.revoke(token)
			case "stale-head":
				port, err := e.writer.canonicalStore(ctx, e.record.Canonical.Head.RunID)
				assistantRunMust(t, err)
				head, err := port.Load(ctx, port.id)
				assistantRunMust(t, err)
				_, err = port.Commit(ctx, runcontext.RunCommit{RunID: port.id, ExpectedRevision: head.Revision, CommitID: "competing-plan-head", Checkpoint: head.Checkpoint, Events: [][]byte{}})
				assistantRunMust(t, err)
			case "lost-ack":
				e.canonicalBinding.Writer.Store = &assistantPreparationUnknownStore{RunStore: e.canonicalBinding.Writer.Store}
			}
			before := assistantExecutionRecord(t, e)
			x, err := e.openAssistantExecution(ctx, assistantExecutionPrompt{Name: "served"})
			if x != nil {
				x.Close()
				t.Fatal("refused input opened Runtime")
			}
			if err == nil {
				t.Fatal("missing refusal")
			}
			if _, err := os.Stat(filepath.Join(s.cfg.ProjectDir, "plan-runtime-frame.json")); !os.IsNotExist(err) {
				t.Fatal("refusal attempted Runtime", err)
			}
			after := assistantExecutionRecord(t, e)
			if calls.Load() != 0 {
				t.Fatal("refusal generated model work")
			}
			if fault != "lost-ack" && !reflect.DeepEqual(before.Canonical, after.Canonical) {
				t.Fatal("refusal changed retained canonical result")
			}
			if fault == "lost-ack" {
				v := runcontext.MustParseJSON(string(assistantChatJSON(after.Checkpoint)))
				if v.Get("planState").Get("decisions").Len() != 1 {
					t.Fatal("lost acknowledgement not retained")
				}
				// The writer latches uncertainty: no automatic decision replay or retry.
				input := &runcontext.Input{Execution: e.canonicalBinding.Writer.Checkpoint(), Persistence: e.canonicalBinding, Approval: e.decision, Plan: runcontext.MustParseJSON(string(e.source.Plan.Snapshot)), PlanResolver: e.source.PlanResolver}
				if _, err := e.prepareAssistantPlanDecision(ctx, input); err == nil {
					t.Fatal("lost acknowledgement replayed")
				}
				again := assistantExecutionRecord(t, e)
				if !reflect.DeepEqual(after.Canonical, again.Canonical) {
					t.Fatal("lost acknowledgement duplicated commit")
				}
			}
		})
	}
}

type assistantPreparationUnknownStore struct{ runcontext.RunStore }

func (s *assistantPreparationUnknownStore) Commit(ctx context.Context, commit runcontext.RunCommit) (runcontext.RunReceipt, error) {
	_, err := s.RunStore.Commit(ctx, commit)
	if err != nil {
		return runcontext.RunReceipt{}, err
	}
	return runcontext.RunReceipt{}, errors.New("fixture lost decision acknowledgement")
}

// This result is authored independently of execution output. The ordinary writer
// canonicalizes numbers on Save; the original store accepts raw Checkpoint bytes.
const assistantPreparationRawResult = `{"content":[{"type":"text","text":"retained 1.20 and 1e2"}],"structuredContent":{"number":1.20,"exponent":1e2}}`

type assistantPreparationRawStore struct{ runcontext.RunStore }

func (s *assistantPreparationRawStore) Commit(ctx context.Context, commit runcontext.RunCommit) (runcontext.RunReceipt, error) {
	var checkpoint map[string]json.RawMessage
	if err := json.Unmarshal(commit.Checkpoint, &checkpoint); err != nil {
		return runcontext.RunReceipt{}, err
	}
	var durable map[string]json.RawMessage
	if err := json.Unmarshal(checkpoint["durable"], &durable); err != nil {
		return runcontext.RunReceipt{}, err
	}
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(durable["dispatches"], &rows); err != nil {
		return runcontext.RunReceipt{}, err
	}
	for _, row := range rows {
		if string(row["source"]) == `"fixture"` && string(row["tool"]) == `"inspect"` && string(row["state"]) == `"result"` {
			row["result"] = json.RawMessage(assistantPreparationRawResult)
		}
	}
	var err error
	if durable["dispatches"], err = json.Marshal(rows); err != nil {
		return runcontext.RunReceipt{}, err
	}
	if checkpoint["durable"], err = json.Marshal(durable); err != nil {
		return runcontext.RunReceipt{}, err
	}
	if commit.Checkpoint, err = json.Marshal(checkpoint); err != nil {
		return runcontext.RunReceipt{}, err
	}
	return s.RunStore.Commit(ctx, commit)
}

func assistantPreparationRawDurable(t *testing.T, checkpoint []byte) []byte {
	t.Helper()
	var fields map[string]json.RawMessage
	assistantRunMust(t, json.Unmarshal(checkpoint, &fields))
	return fields["durable"]
}
