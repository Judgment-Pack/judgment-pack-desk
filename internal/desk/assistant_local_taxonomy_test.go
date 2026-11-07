package desk

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

// These expectations are authored independently of the constructor declarations.
// Retained review output grants no resource application or publication authority.
func TestAssistantLocalConstructorTaxonomies(t *testing.T) {
	e := &assistantRunExecution{source: assistantRunSource{Original: []byte(`{"pack":{"id":"saved-pack","path":"packs/current.json"}}`)}}
	registryTools := func(r *assistantHostRegistry, err error) []runcontext.HostTool {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return r.tools
	}
	policy := registryTools(e.assistantPolicyTools(nil))
	recipe := registryTools(e.assistantDialectRecipeTools())
	mapping := registryTools(e.assistantDialectTools())
	records := registryTools(e.assistantDialectRecordTools())
	tests, err := e.assistantTestTools(nil)
	if err != nil {
		t.Fatal(err)
	}
	saved := e.assistantSavedPackTools(nil)
	type expectation struct {
		operation, effect, legacy, delegation string
		reference, immutable                  bool
	}
	groups := []struct {
		name  string
		tools []runcontext.HostTool
		want  map[string]expectation
	}{
		{"policy", policy, map[string]expectation{
			"read_policy":                         {"read", "read", "read", "read", true, false},
			"read_policy_comparison":              {"read", "read", "read", "read", true, false},
			"read_policy_coverage":                {"read", "read", "read", "read", true, false},
			"propose_policy_update":               {"propose", "propose", "proposal", "", true, false},
			"propose_policy_pack_interpretation":  {"propose", "propose", "proposal", "", true, false},
			"propose_policy_graph_interpretation": {"propose", "propose", "proposal", "", true, false},
		}},
		{"recipe", recipe, map[string]expectation{
			"get_dialect_recipe_instructions": {"read", "read", "read", "read", true, true},
			"list_dialects":                   {"discover", "read", "read", "read", false, false},
			"read_dialect_recipe":             {"read", "read", "read", "read", false, false},
			"propose_dialect_recipe":          {"propose", "propose", "proposal", "", false, false},
		}},
		{"mapping", mapping, map[string]expectation{
			"read_dialect":                {"read", "read", "read", "read", true, false},
			"read_dialect_publication":    {"read", "read", "read", "read", true, false},
			"propose_dialect":             {"propose", "propose", "proposal", "", false, false},
			"propose_dialect_publication": {"propose", "propose", "proposal", "", false, false},
		}},
		{"records", records, map[string]expectation{
			"read_dialect_records":   {"read", "read", "read", "read", true, false},
			"read_dialect_activity":  {"read", "read", "read", "read", true, false},
			"propose_dialect_action": {"propose", "propose", "proposal", "", false, false},
		}},
		{"tests", tests, map[string]expectation{
			"read_pack_tests":    {"read", "read", "read", "read", true, false},
			"propose_test_cases": {"propose", "propose", "proposal", "", true, false},
		}},
		{"saved-pack", saved, map[string]expectation{
			"propose_saved_pack": {"propose", "propose", "proposal", "", false, false},
		}},
	}
	for _, group := range groups {
		t.Run(group.name, func(t *testing.T) {
			if len(group.tools) != len(group.want) {
				t.Fatalf("got %d registrations, want %d", len(group.tools), len(group.want))
			}
			seen := map[string]bool{}
			for _, tool := range group.tools {
				t.Run(tool.Name, func(t *testing.T) {
					want, ok := group.want[tool.Name]
					if !ok || seen[tool.Name] {
						t.Fatal("unexpected or duplicate registration")
					}
					seen[tool.Name] = true
					taxonomy := &runcontext.ToolTaxonomy{Operation: want.operation, Effect: want.effect}
					if !reflect.DeepEqual(tool.Taxonomy, taxonomy) {
						t.Fatalf("owner taxonomy = %#v, want %#v", tool.Taxonomy, taxonomy)
					}
					if problem := runcontext.HostTaxonomyProblem(tool); problem != "" {
						t.Fatal(problem)
					}
					declared, err := json.Marshal(tool.Declaration().Taxonomy)
					if err != nil {
						t.Fatal(err)
					}
					expected, _ := json.Marshal(taxonomy)
					if string(declared) != string(expected) {
						t.Fatalf("published taxonomy = %s, want %s", declared, expected)
					}
					if tool.Effect != want.legacy || tool.Delegation != want.delegation {
						t.Fatal("legacy execution or delegation changed")
					}
					var contextPolicy *runcontext.ToolContextPolicy
					if want.reference || want.immutable {
						contextPolicy = &runcontext.ToolContextPolicy{Reference: want.reference, Immutable: want.immutable}
					}
					if !reflect.DeepEqual(tool.ContextPolicy, contextPolicy) {
						t.Fatal("context freshness/retention policy changed")
					}
					if tool.Source != nil || tool.SourceTool != "" || tool.EffectClass != "" || tool.Approval != "" || tool.Approve != nil || tool.TaxonomyLimit != nil {
						t.Fatal("local declaration introduced source or approval authority")
					}
				})
			}
		})
	}
	unlinked := &assistantRunExecution{source: assistantRunSource{Original: []byte(`{}`)}}
	if len(unlinked.assistantSavedPackTools(nil)) != 0 {
		t.Fatal("saved-pack constructor offered an unlinked resource")
	}
}

// Exercise the actual local producer without assembling the full registry or
// invoking a model/Runtime. The retained independent draft is review material.
func TestAssistantLocalRecipeProposalRetainsReviewOnly(t *testing.T) {
	s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
	registry, err := e.assistantDialectRecipeTools()
	assistantRunMust(t, err)
	tool := assistantGraphFind(t, registry.tools, "propose_dialect_recipe")
	call := "local-recipe-review"
	raw := json.RawMessage(assistantRecipeCandidate)
	assistantRunMust(t, e.writer.boundEvent(t.Context(), contract.RunEvent{ToolCall: &contract.ToolCall{Type: "tool_call", CallID: &call, Name: tool.Name, Args: raw}}))
	ctx := context.WithValue(runcontext.WithCallID(t.Context(), call), assistantHostRootKey{}, true)
	result, err := tool.Execute(ctx, raw)
	assistantRunMust(t, err)
	if result.Err() {
		t.Fatal(result.Value().Text())
	}
	record := assistantRecipeObserve(t, s, e)
	retained, err := s.draftRepository().Get(t.Context(), record.DomainOutputs[0].Recipe.Retained.ID)
	assistantRunMust(t, err)
	if retained.Document.SavedInputID != "" {
		t.Fatal("review proposal applied a preparation")
	}
	if tool.Taxonomy.Operation != "propose" || tool.Taxonomy.Effect != "propose" || tool.Delegation != "" || tool.Source != nil || tool.Approve != nil {
		t.Fatal("review retention gained execution authority")
	}
}
