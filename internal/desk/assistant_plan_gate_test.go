package desk

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/gate"
)

// A real Desk declaration set admits the package actions. The contextual action
// then closes only the active step using real canonical host-read evidence.
func TestAssistantPlanGateActualContextualEvidence(t *testing.T) {
	g := gate.New(gate.Options{})
	g.SetOffer(contract.ToolOffer{})
	_, _, _, writer, record := canonicalActivationAdmitted(t)
	e := &assistantRunExecution{record: record, writer: writer}
	input := &runcontext.Input{Execution: record.Checkpoint, CurrentRequest: &runcontext.CurrentRequest{Message: "Review", Input: "exact original"}}
	assistantRunMust(t, e.bindCanonicalInput(t.Context(), input))
	file := filepath.Join(t.TempDir(), "observed.txt")
	assistantRunMust(t, os.WriteFile(file, []byte("Observed exact bytes"), 0600))
	opened, err := runcontext.ContextualSession(t.Context(), runcontext.Session{Prompt: "Review the exact saved input", AllowConversation: true, Interactive: true, Model: &runcontext.ModelSession{Family: "gemini", Model: "fixture"}, Context: input, HostTools: []runcontext.HostTool{{Name: "inspect", Taxonomy: &runcontext.ToolTaxonomy{Operation: "read", Effect: "read"}, Effect: "read", Description: "Read exact supplied file", InputSchema: json.RawMessage(`{"type":"object"}`), Execute: func(context.Context, json.RawMessage) (runcontext.ToolResult, error) {
		raw, err := os.ReadFile(file)
		return runcontext.ResultOf([]runcontext.Content{runcontext.TextContent(string(raw))}, nil), err
	}}}})
	assistantRunMust(t, err)
	defer opened.Context.Close()
	bindings := &assistantGateBindings{}
	bindings.add(g)
	run := &adapter.Run{Gate: g.Calls(), Context: input}
	assistantRunMust(t, bindings.bind(run))
	declarations := []contract.ToolOfferHostTool{}
	for _, host := range opened.Session.HostTools {
		declarations = append(declarations, host.Declaration())
	}
	assistantRunMust(t, adapter.EnrollContext(t.Context(), run, "actual-plan-evidence", declarations))
	tool := func(name string) runcontext.HostTool { return assistantGraphFind(t, opened.Session.HostTools, name) }
	for _, name := range []string{"update_plan", "complete_plan_step", "request_budget_increase"} {
		actual := tool(name).Declaration()
		found := false
		for _, declaration := range g.Offer().HostTools {
			if declaration.Name == name {
				found = reflect.DeepEqual(declaration, actual)
			}
		}
		if !found {
			t.Fatal("gate declaration differs from actual contextual action", name)
		}
		if refusal := g.Calls().AdmitHost(name); refusal != nil {
			t.Fatal("package action gate-refused", name, refusal)
		}
	}
	_, err = tool("update_plan").Execute(t.Context(), json.RawMessage(`{"explanation":"Inspect supplied file","plan":[{"step":"Inspect exact bytes","status":"in_progress"},{"step":"Report","status":"pending"}]}`))
	assistantRunMust(t, err)
	_, err = tool("inspect").Execute(t.Context(), json.RawMessage(`{}`))
	assistantRunMust(t, err)
	c := opened.Context.Execution
	book, scope := c.PlanProof()
	operation := ""
	for _, d := range book.Dispatches {
		if d.Tool == "inspect" && d.State == "result" && d.Scope == scope && d.Failure == nil {
			result, err := runcontext.ParseJSON(d.Result)
			assistantRunMust(t, err)
			if result.Get("isError").Truth() {
				t.Fatal("host read unsuccessful")
			}
			operation = d.ID
		}
	}
	if operation == "" || len(book.Observations) == 0 {
		t.Fatal("real acknowledged evidence absent")
	}
	args := runcontext.ObjectOf("id", c.State().Plan.Items()[0].Get("id"), "expectedRevision", c.State().PlanState.Get("revision"), "outcome", runcontext.String("completed"), "evidence", runcontext.ObjectOf("operations", runcontext.Array(runcontext.String(operation)), "artifacts", runcontext.Array()))
	if refusal := g.Calls().AdmitHost("complete_plan_step"); refusal != nil {
		t.Fatal(refusal)
	}
	_, err = tool("complete_plan_step").Execute(t.Context(), args.JSON())
	assistantRunMust(t, err)
	latest := opened.Context.Persistence("author").Writer.Checkpoint()
	raw, err := json.Marshal(latest)
	assistantRunMust(t, err)
	v := runcontext.MustParseJSON(string(raw))
	first, second := v.Get("plan").Items()[0], v.Get("plan").Items()[1]
	if first.Get("status").Str() != "completed" || second.Get("status").Str() != "pending" || first.Get("evidence").Get("operations").Items()[0].Str() != operation || len(latest.Durable.Observations) == 0 || v.Get("planState").Get("refusals").Len() != 0 {
		t.Fatal("exact contextual completion failed")
	}
}
