package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

func assistantNativeCompatibilitySource(t *testing.T) func(*Server, string, assistantRunStart) assistantRunStart {
	return func(s *Server, token string, start assistantRunStart) assistantRunStart {
		start = assistantPlanPrepareSource(t)(s, token, start)
		original := s.assistantRunExecutor.(*assistantCodexExecutor).assistantProtocolExecutor
		s.assistantRunExecutor = &assistantPlanBindingExecutor{original}
		start, _ = assistantBindingHistorical(t, s, token, start)
		return start
	}
}

func TestAssistantNativePlanAfterApprovalRefusesBeforePreparation(t *testing.T) {
	s, _, e, _, trace := assistantCodexSourceFixture(t, "normal", "work", testAIAlpha, `{"profileVersion":1,"codex":{"inherit":true}}`, assistantNativeCompatibilitySource(t))
	input := &runcontext.Input{Execution: e.record.Checkpoint}
	assistantRunMust(t, e.bindCanonicalInput(t.Context(), input))
	cp := assistantPreparationPending(t, *input.Persistence.Writer.Checkpoint())
	approval := runcontext.MustParseJSON(`{"callId":"completed-act","name":"notes__write_note","tool":"write_note","source":{"kind":"custom","id":"notes","trust":"person"},"args":{},"state":"answered","result":{"content":[{"type":"text","text":" {\n  \"n\":1.20,\"wide\":9007199254740993\n}\n"}]}}`)
	value := runcontext.MustParseJSON(string(assistantChatJSON(cp)))
	value.Set("approval", approval)
	value.Set("digest", runcontext.String(runcontext.RecordDigest(value)))
	_, err := runcontext.ReadExecution(value)
	assistantRunMust(t, err)
	assistantRunMust(t, json.Unmarshal(value.JSON(), &cp))
	// Use the admitted binding's generation-scoped checkpoint port. A direct
	// untagged store commit must continue to fail its ownership check.
	assistantRunMust(t, input.Checkpoint(t.Context(), cp))
	before := assistantExecutionRecord(t, e)
	e.decision = &contract.CheckpointApprovalDecision{CallID: "exact-plan-decision", Approved: true}
	e.planDecision = assistantRunPlanReference(before)
	// Stale caller data cannot suppress the approval retained by the current head.
	stale := cp
	stale.Approval = nil
	input.Execution = &stale
	for _, check := range []func() error{
		func() error { return e.guardAssistantNativePlanContinuation(t.Context(), input, true) },
		func() error {
			x, err := e.openAssistantExecution(t.Context(), assistantExecutionPrompt{Name: "served"})
			if x != nil {
				x.Close()
				t.Fatal("unsupported native continuation opened")
			}
			return err
		},
	} {
		err := check()
		if err == nil || !strings.Contains(err.Error(), "NativePlanAfterApprovalUnsupported") {
			t.Fatal("missing named pre-dispatch refusal", err)
		}
		var typed *failure.Error
		if !errors.As(err, &typed) || typed.Failure.Code != "configuration" || typed.Failure.Source != "context" || typed.Failure.Outcome != "not-started" {
			t.Fatal("native refusal lost shared typed failure", err)
		}
		after := assistantExecutionRecord(t, e)
		if !reflect.DeepEqual(before.Canonical, after.Canonical) || !reflect.DeepEqual(before.Operations, after.Operations) || !reflect.DeepEqual(assistantApprovalResults(before), assistantApprovalResults(after)) {
			t.Fatal("refusal consumed Plan or changed retained approved result")
		}
	}
	if len(assistantCodexLog(t, trace)) != 0 {
		t.Fatal("refused Plan dispatched native provider")
	}
	if _, err := os.Stat(filepath.Join(s.cfg.ProjectDir, "execution-fixture.jsonl")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("refused Plan started Runtime", err)
	}
}

func TestAssistantNativeReleasedPlanWithChildrenRefusesBeforeDispatch(t *testing.T) {
	s, _, e, _, trace := assistantCodexSourceFixture(t, "normal", "work", testAIAlpha, `{"profileVersion":1,"codex":{"inherit":true}}`, assistantNativeCompatibilitySource(t))
	peer := strings.Replace(assistantExecutionCodexPeer, "  else: tool(offered('get_schema'),{})", `  else:
   cp=runs[-1]['checkpoint']
   tool(offered('update_plan'),{'explanation':'Remove reviewed second step','plan':[{'id':cp['plan'][0]['id'],'step':cp['plan'][0]['step'],'status':'pending'}]})`, 1)
	if peer == assistantExecutionCodexPeer {
		t.Fatal("native Plan peer anchor changed")
	}
	assistantRunMust(t, os.WriteFile(filepath.Join(filepath.Dir(trace), "peer.py"), []byte(peer), 0600))
	x := assistantExecutionOpen(t, s, e)
	if x.Run.Children == nil || x.Run.Agent == nil {
		t.Fatal("actual production native/delegation composition absent")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	assistantRunMust(t, x.Adapter.Run(ctx, x.Run))
	x.Close()
	before := assistantExecutionRecord(t, e)
	if before.Checkpoint.PlanState == nil || before.Checkpoint.PlanState.Pending == nil {
		failures := []string{}
		for _, held := range before.Events {
			if event := held.Event.Error; event != nil && event.Failure != nil {
				failures = append(failures, event.Failure.Code+"/"+event.Failure.Source+"/"+event.Failure.Outcome)
				if len(failures) > 4 {
					failures = failures[1:]
				}
			}
		}
		t.Fatalf("actual native update_plan did not pause: status=%s plan=%d events=%d operations=%d nativeTraceRecords=%d failures=%v writer=%v", before.Checkpoint.Status, len(before.Checkpoint.Plan), len(before.Events), len(before.Operations), len(assistantCodexLog(t, trace)), failures, e.writer.Err())
	}
	released := false
	for _, d := range before.Checkpoint.Durable.Dispatches {
		if d.Kind == "agent" && d.PlanPause != nil {
			released = true
		}
	}
	if !released {
		t.Fatal("actual released native Plan evidence missing")
	}
	planCallbacks, planModels := 0, 0
	for _, op := range before.Operations {
		if op.Kind != "callback" && op.Kind != "model" {
			continue
		}
		var outcome assistantAgentOutcome
		assistantRunMust(t, json.Unmarshal(op.Result, &outcome))
		if op.State != "completed" || outcome.Stop != "plan" || outcome.Failure != nil {
			t.Fatal("released Plan lacks exact acknowledged physical stop receipt", op.ID)
		}
		if op.Kind == "callback" {
			planCallbacks++
		} else {
			planModels++
		}
	}
	if planCallbacks != 1 || planModels != 1 || e.ctx.Err() != nil {
		t.Fatal("Plan pause lost its live owner or original joined receipt", planCallbacks, planModels, e.ctx.Err())
	}
	traceBefore, err := os.ReadFile(trace)
	assistantRunMust(t, err)
	e.decision = &contract.CheckpointApprovalDecision{CallID: before.Checkpoint.PlanState.Pending.CallID, Approved: true}
	e.planDecision = assistantRunPlanReference(before)
	opened, err := e.openAssistantExecution(t.Context(), assistantExecutionPrompt{Name: "served"})
	if opened != nil {
		opened.Close()
		t.Fatal("unsupported native Plan/delegation opened")
	}
	if err == nil || !strings.Contains(err.Error(), "NativePlanDelegationUnsupported") {
		t.Fatal("missing named native Plan/delegation refusal", err)
	}
	after := assistantExecutionRecord(t, e)
	traceAfter, err := os.ReadFile(trace)
	assistantRunMust(t, err)
	if !reflect.DeepEqual(before.Canonical, after.Canonical) || !reflect.DeepEqual(before.Operations, after.Operations) || !bytes.Equal(traceBefore, traceAfter) {
		t.Fatal("refused Plan/delegation consumed state or dispatched model/tool/effect")
	}
}

// Production never opts into TaskSteps. Exercise its real native update_plan
// journey; retrofitting an envelope into an admitted head is not that journey.
func TestAssistantProductionTaskStepsDisabledActualNativePlan(t *testing.T) {
	s, _, e, manager, trace := assistantCodexSourceFixture(t, "normal", "work", testAIAlpha, `{"profileVersion":1,"codex":{"inherit":true}}`, assistantNativeCompatibilitySource(t))
	peer := strings.Replace(assistantExecutionCodexPeer, "  else: tool(offered('get_schema'),{})", `  else:
   cp=runs[-1]['checkpoint']
   if offered('delegate_plan_steps')!='unoffered': sys.exit(76)
   tool(offered('update_plan'),{'explanation':'Inspect the reviewed input','plan':[{'id':row['id'],'step':row['step'],'status':'in_progress' if i==0 else 'pending'} for i,row in enumerate(cp['plan'])]})`, 1)
	if peer == assistantExecutionCodexPeer {
		t.Fatal("native Plan peer anchor changed")
	}
	assistantRunMust(t, os.WriteFile(filepath.Join(filepath.Dir(trace), "peer.py"), []byte(peer), 0600))
	x := assistantExecutionOpen(t, s, e)
	defer x.Close()
	originalRun, originalAgent, originalBinding := x.Run, x.Run.Agent, x.Run.Context.Persistence
	if x.Run.Context.TaskSteps || x.Run.Context.TaskStepEnvelope != nil || originalAgent == nil || x.Run.Children == nil || manager == nil || s.codex != manager {
		t.Fatal("actual factory native/Children construction or disabled settings changed")
	}
	before := assistantExecutionRecord(t, e)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	assistantRunMust(t, x.Adapter.Run(ctx, x.Run))
	if x.Run != originalRun || x.Run.Agent != originalAgent || x.Run.Context.Persistence != originalBinding || x.Run.Context.TaskSteps || x.Run.Context.TaskStepEnvelope != nil {
		t.Fatal("actual native route or disabled settings changed")
	}
	after := assistantExecutionRecord(t, e)
	canonicalActivationCheck(t, after)
	if after.Checkpoint == nil || after.Checkpoint.Status != "complete" || len(after.Checkpoint.Plan) != 2 || after.Checkpoint.Plan[0].Status != "in_progress" || after.Checkpoint.Plan[1].Status != "pending" || !bytes.Equal(assistantCheckpointAcceptedPlan(before.Checkpoint).JSON(), assistantCheckpointAcceptedPlan(after.Checkpoint).JSON()) {
		t.Fatal("actual native update_plan did not retain reviewed scope and working progress")
	}
	value := runcontext.MustParseJSON(string(assistantChatJSON(after.Checkpoint)))
	state := value.Get("planState")
	for _, name := range []string{"taskEnvelope", "taskSchedule", "taskCoordinator"} {
		if state.Has(name) {
			t.Fatal("disabled production journey created task scheduling state", name)
		}
	}
	if after.Checkpoint.Durable == nil || len(after.Checkpoint.Durable.Children) != 0 {
		t.Fatal("disabled journey created child task dispatch")
	}
	updates, native := 0, 0
	for _, dispatch := range after.Checkpoint.Durable.Dispatches {
		if dispatch.Scope.Task != nil || dispatch.Scope.Execution != after.Checkpoint.ID || dispatch.Tool == "delegate_plan_steps" {
			t.Fatal("disabled journey dispatched a task", dispatch.ID)
		}
		if dispatch.Tool == "update_plan" && dispatch.State == "result" {
			updates++
		}
		if dispatch.Kind == "agent" {
			native++
		}
	}
	generations := 0
	for _, row := range assistantCodexLog(t, trace) {
		if string(row["type"]) != `"generation"` {
			continue
		}
		generations++
		var request struct {
			Tools []struct {
				Description string `json:"description"`
			} `json:"dynamicTools"`
		}
		assistantRunMust(t, json.Unmarshal(row["request"], &request))
		for _, tool := range request.Tools {
			if strings.HasPrefix(tool.Description, "delegate_plan_steps:") {
				t.Fatal("disabled factory offered task-only delegation to actual native provider")
			}
		}
	}
	if updates != 1 || native != 1 || generations != 1 {
		t.Fatal("actual disabled journey lacks exact native/Plan witnesses", updates, native, generations)
	}
}
