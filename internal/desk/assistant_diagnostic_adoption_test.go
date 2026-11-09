package desk

import "testing"

// Preparation has no Runtime handle or provider runner. The real served tool
// declaration belongs to Execute, after Gate.Open has observed Runtime.
func TestAssistantDiagnosticPrepareWithoutServedTools(t *testing.T) {
	source := assistantRunSource{
		Start: assistantRunStart{Purpose: assistantDiagnosticPurpose},
		Binding: assistantRunBinding{
			Owner:        assistantRunOwner{Kind: assistantDiagnosticPurpose},
			ConnectionID: testAIAlpha, Model: "shared-model", Phase: "work",
		},
		Profile: assistantProfile{Version: 1},
	}
	cp, err := (&assistantDiagnosticExecutor{}).Prepare(t.Context(), source)
	assistantRunMust(t, err)
	if cp.Unit != "provider-managed" || cp.Used != 0 || cp.Limit != 1 || cp.RequestCap != nil {
		t.Fatalf("preparation changed native accounting: %+v", cp)
	}
	if cp.Durable != nil {
		t.Fatal("preparation invented durable execution evidence")
	}
}
