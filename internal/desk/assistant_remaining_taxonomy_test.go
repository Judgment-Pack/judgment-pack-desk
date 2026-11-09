package desk

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/gate"
)

func TestAssistantRemainingLocalTaxonomy(t *testing.T) {
	e := &assistantRunExecution{}
	graph, err := e.assistantGraphTools(nil)
	assistantRunMust(t, err)
	expected := map[string]runcontext.ToolTaxonomy{
		"list_decisions":                   {Operation: "discover", Effect: "read"},
		"read_decision":                    {Operation: "read", Effect: "read"},
		"get_graph_authoring_instructions": {Operation: "read", Effect: "read"},
		"graph_validate":                   {Operation: "check", Effect: "read", CheckKind: "validate"},
		"graph_rehearse":                   {Operation: "check", Effect: "read", CheckKind: "rehearse"},
		"graph_explain":                    {Operation: "check", Effect: "read", CheckKind: "analyze"},
		"propose_graph":                    {Operation: "propose", Effect: "propose"},
	}
	for name, want := range expected {
		tool := assistantGraphFind(t, graph.tools, name)
		if !reflect.DeepEqual(tool.Taxonomy, &want) {
			t.Fatal("local graph taxonomy changed", name, tool.Taxonomy)
		}
		if problem := runcontext.HostTaxonomyProblem(tool); problem != "" {
			t.Fatal(name, problem)
		}
	}
	e.source.Start.Purpose = "author-pack"
	pack, err := e.assistantAuthoringTools()
	assistantRunMust(t, err)
	// Pack authoring declares only propose_pack, which the Agent reads itself
	// as the run's proposal (Agent v0.13.2 tool-offer contract), with its
	// owner taxonomy; no other standalone pack tool is offered.
	if len(pack.tools) != 1 || pack.tools[0].Name != "propose_pack" || !reflect.DeepEqual(pack.tools[0].Taxonomy, &runcontext.ToolTaxonomy{Operation: "propose", Effect: "propose"}) {
		t.Fatal("standalone pack offer changed", len(pack.tools))
	}
	if problem := runcontext.HostTaxonomyProblem(pack.tools[0]); problem != "" {
		t.Fatal(problem)
	}
	e.source.Start.Purpose = "author-graph"
	author, err := e.assistantAuthoringTools()
	assistantRunMust(t, err)
	if len(author.tools) != 2 {
		t.Fatal("standalone graph checks changed")
	}
	for _, tool := range author.tools {
		want := expected[tool.Name]
		if !reflect.DeepEqual(tool.Taxonomy, &want) {
			t.Fatal("standalone check taxonomy differs", tool.Name)
		}
	}
	e.record.Binding.Phase = "plan"
	plan := e.assistantPlanTools()
	if len(plan) != 1 || plan[0].Effect != "plan" || !reflect.DeepEqual(plan[0].Taxonomy, &runcontext.ToolTaxonomy{Operation: "propose", Effect: "propose"}) {
		t.Fatal("Plan taxonomy or legacy phase contract changed")
	}
	if problem := runcontext.HostTaxonomyProblem(plan[0]); problem != "" {
		t.Fatal(problem)
	}
	offer := contract.ToolOffer{HostTools: []contract.ToolOfferHostTool{plan[0].Declaration(), assistantGraphFind(t, graph.tools, "propose_graph").Declaration()}}
	phase := gate.PlanPhase(offer)
	if !contains(phase.Host, assistantPlanTool) || contains(phase.Host, "propose_graph") {
		t.Fatal("Plan domain proposal filtering widened")
	}
}

// These expectations are handwritten owner contracts, independent of the
// production catalog whose declarations they check.
func assistantExternalOwnerTaxonomies() map[string]runcontext.ToolTaxonomy {
	return map[string]runcontext.ToolTaxonomy{
		assistantResearchTool: {Operation: "act", Effect: "act"},
		assistantSearchTool:   {Operation: "discover", Effect: "read-external"},
		assistantLinkTool:     {Operation: "read", Effect: "read-external"},
		assistantWebsiteTool:  {Operation: "discover", Effect: "read-external"},
	}
}
func assistantExternalOwnerIDs() map[string]string {
	return map[string]string{assistantResearchTool: "desk-research-workflow", assistantSearchTool: "desk-search", assistantLinkTool: "desk-link", assistantWebsiteTool: "desk-website"}
}
func TestAssistantExternalOwnerTaxonomy(t *testing.T) {
	for _, raw := range []runcontext.HostTool{assistantResearchDeclaration(), assistantSearchDeclaration(), assistantLinkDeclaration(), assistantWebsiteDeclaration()} {
		t.Run(raw.Name, func(t *testing.T) {
			// A raw factory product is not an executable external-source offer.
			// The actual approval wrapper owns taxonomy and provenance together.
			if raw.Taxonomy != nil || raw.Source != nil {
				t.Fatal("raw factory manufactured source enrollment")
			}
			raw.Execute = func(context.Context, json.RawMessage) (runcontext.ToolResult, error) {
				return runcontext.ToolResult{}, nil
			}
			approval := assistantExternalApprovalVector(raw.Name)
			tool, err := assistantExternalSourceTool(raw, approval, func(context.Context) error { return nil }, func(context.Context, runcontext.ReceiptEvidence) (bool, error) { return true, nil }, func(runcontext.ApprovalRequest) string { return "refuse" })
			assistantRunMust(t, err)
			want := assistantExternalOwnerTaxonomies()[raw.Name]
			if tool.Taxonomy == nil || *tool.Taxonomy != want || tool.Source == nil || tool.Source.ID != assistantExternalOwnerIDs()[raw.Name] || tool.Effect != "" || tool.Delegation != raw.Delegation {
				t.Fatal("external owner taxonomy or legacy delegation differs", raw.Name)
			}
			if problem := runcontext.HostTaxonomyProblem(tool); problem != "" {
				t.Fatal(problem)
			}
			plan := gate.PlanPhase(contract.ToolOffer{HostTools: []contract.ToolOfferHostTool{tool.Declaration()}})
			if contains(plan.Host, raw.Name) != (raw.Name != assistantResearchTool) {
				t.Fatal("source Plan boundary widened", raw.Name)
			}
		})
	}
}
