package desk

import (
	"context"
	"encoding/json"
	"testing"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

// Published Agent v0.6.0 tool-offer hostTool.effect uses "proposal".
// Sourced tool effectClass has a separate vocabulary and is not changed here.
func TestAssistantHostCanonicalEffects(t *testing.T) {
	for _, policy := range []bool{false, true} {
		t.Run(map[bool]string{false: "conversation", true: "policy"}[policy], func(t *testing.T) {
			var e *assistantRunExecution
			var s *Server
			if policy {
				s, _, e, _, _ = assistantPolicyFixture(t, "http://127.0.0.1:1", "work", false, nil)
			} else {
				s, _, e = assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
			}
			x := assistantExecutionOpen(t, s, e)
			expected := map[string]string{"list_decisions": "read", "read_decision": "read", "get_graph_authoring_instructions": "read", "graph_validate": "read", "graph_explain": "read", "graph_rehearse": "read", "propose_graph": "proposal", "read_pack_tests": "read", "propose_test_cases": "proposal", "get_dialect_recipe_instructions": "read", "list_dialects": "read", "read_dialect_recipe": "read", "propose_dialect_recipe": "proposal", "read_dialect": "read", "propose_dialect": "proposal", "read_dialect_publication": "read", "propose_dialect_publication": "proposal", "read_dialect_records": "read", "read_dialect_activity": "read", "propose_dialect_action": "proposal"}
			if policy {
				expected = map[string]string{"read_policy": "read", "propose_policy_update": "proposal", "read_policy_comparison": "read", "read_policy_coverage": "read", "propose_policy_pack_interpretation": "proposal", "propose_policy_graph_interpretation": "proposal"}
			}
			registry, err := newAssistantHostRegistry(x.Run.HostTools)
			assistantRunMust(t, err)
			if len(registry.tools) != len(expected) {
				t.Fatal("combined actual registry incomplete", len(registry.tools), len(expected))
			}
			for _, declaration := range registry.declarations() {
				raw := assistantChatJSON(declaration)
				var fields map[string]json.RawMessage
				assistantRunMust(t, json.Unmarshal(raw, &fields))
				name, err := assistantTestString(fields["name"])
				assistantRunMust(t, err)
				effect, err := assistantTestString(fields["effect"])
				assistantRunMust(t, err)
				want, ok := expected[name]
				if !ok || effect != want || !contains([]string{"read", "proposal"}, effect) {
					t.Fatal("noncanonical emitted host effect", name, effect, want)
				}
				delete(expected, name)
			}
			if len(expected) != 0 {
				t.Fatal("canonical declaration missing", expected)
			}
		})
	}
	for _, effect := range []string{"read", "proposal", "propose", "workflow", "memory", "plan", ""} {
		tool := runcontext.HostTool{Name: "fixture-effect", Effect: effect, InputSchema: []byte(`{"type":"object","properties":{},"required":[],"additionalProperties":false}`), Execute: func(context.Context, json.RawMessage) (runcontext.ToolResult, error) {
			t.Fatal("registry executed a capability")
			return runcontext.ToolResult{}, nil
		}}
		_, err := newAssistantHostRegistry([]runcontext.HostTool{tool})
		valid := effect == "read" || effect == "proposal"
		if (err == nil) != valid {
			t.Fatal("unsupported effect alias/capability accepted or published effect refused", effect, err)
		}
	}
}
