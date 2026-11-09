package desk

import (
	"encoding/json"
	"testing"
)

func assistantVariantReceipts(t *testing.T, run assistantRunRecord) map[string]assistantHostResult {
	t.Helper()
	receipts := map[string]assistantHostResult{}
	for _, op := range run.Operations {
		if op.Kind == "host" && op.Result != nil {
			var result assistantHostResult
			assistantRunMust(t, readAssistantHostReceipt(op.Result, &result))
			receipts[op.ID] = result
		}
	}
	return receipts
}
func assistantVariantClone(t *testing.T, run assistantRunRecord) assistantRunRecord {
	t.Helper()
	raw, err := json.Marshal(run)
	assistantRunMust(t, err)
	var held assistantRunRecord
	assistantRunMust(t, json.Unmarshal(raw, &held))
	return held
}
func TestAssistantDomainHostVariantBoundaries(t *testing.T) {
	// Actual shared producers give both validators valid, independently retained
	// receipts. Negative witnesses change only the foreign optional field.
	s, token, e, _ := assistantTestHostFixture(t, "http://127.0.0.1:1", "work", false, false)
	x := assistantTestHostOpen(t, s, e)
	for _, tool := range []string{"read_pack_tests", "propose_test_cases", "get_dialect_recipe_instructions", "list_dialects", "read_dialect_recipe", "propose_dialect_recipe"} {
		_ = assistantGraphFind(t, x.Run.HostTools, tool)
	}
	if r := assistantTestHostInvoke(t, e, x, "test-read", "read_pack_tests", map[string]any{}); r.Err() {
		t.Fatal(r.Value().Text())
	}
	if r := assistantTestHostInvoke(t, e, x, "test-proposal", "propose_test_cases", assistantTestHostProposalArgs()); r.Err() {
		t.Fatal(r.Value().Text())
	}
	if r := assistantRecipeInvoke(t, e, x, "recipe-proposal", "propose_dialect_recipe", assistantRecipeCandidate); r.Err() {
		t.Fatal(r.Value().Text())
	}
	record := assistantExecutionRecord(t, e)
	assistantRunMust(t, validateAssistantDomainOutputs(record))
	assistantRunMust(t, assistantRecipeImport(t, e, record))
	if len(record.DomainOutputs) != 2 {
		t.Fatal("combined producers absent", len(record.DomainOutputs))
	}
	var recipe, test assistantDomainOutput
	var recipeIntent assistantHostIntent
	var testRead assistantTestRead
	for _, out := range record.DomainOutputs {
		if out.Kind == "dialect-recipe" {
			recipe = out
		} else if out.Kind == "test-proposal" {
			test = out
		}
	}
	receipts := assistantVariantReceipts(t, record)
	for _, op := range record.Operations {
		if op.ID == recipe.OperationID {
			assistantRunMust(t, readAssistantHostReceipt(op.Intent, &recipeIntent))
		}
		if held := receipts[op.ID].Tests; held != nil {
			testRead = *held
		}
	}
	assistantRunMust(t, validateAssistantDialectRecipeOutput(record, recipe, receipts))
	assistantRunMust(t, validateAssistantTestOutput(record, test, receipts))
	for _, original := range []assistantDomainOutput{recipe, test} {
		t.Run(original.Kind+"-with-dialect-action", func(t *testing.T) {
			changed := original
			changed.DialectAction = &assistantDialectActionOutput{}
			var err error
			if original.Kind == "dialect-recipe" {
				err = validateAssistantDialectRecipeOutput(record, changed, receipts)
			} else {
				err = validateAssistantTestOutput(record, changed, receipts)
			}
			if err == nil {
				t.Fatal("foreign Dialect action accepted")
			}
		})
	}
	t.Run("recipe-output-with-tests", func(t *testing.T) {
		changed := recipe
		changed.TestProposal = test.TestProposal
		if validateAssistantDialectRecipeOutput(record, changed, receipts) == nil {
			t.Fatal("recipe accepted test output")
		}
	})
	t.Run("tests-output-with-recipe", func(t *testing.T) {
		changed := test
		changed.Recipe = recipe.Recipe
		if validateAssistantTestOutput(record, changed, receipts) == nil {
			t.Fatal("test accepted recipe output")
		}
	})
	for _, original := range []assistantDomainOutput{recipe, test} {
		t.Run(original.Kind+"-receipt-with-dialect", func(t *testing.T) {
			copy := assistantVariantReceipts(t, record)
			held := copy[original.OperationID]
			held.DialectRead = &assistantDialectRead{}
			copy[original.OperationID] = held
			var err error
			if original.Kind == "dialect-recipe" {
				err = validateAssistantDialectRecipeOutput(record, original, copy)
			} else {
				err = validateAssistantTestOutput(record, original, copy)
			}
			if err == nil {
				t.Fatal("foreign Dialect custody accepted")
			}
		})
	}
	t.Run("recipe-receipt-with-tests", func(t *testing.T) {
		held := receipts[recipe.OperationID]
		held.Tests = &testRead
		copy := assistantVariantReceipts(t, record)
		copy[recipe.OperationID] = held
		if validateAssistantDialectRecipeOutput(record, recipe, copy) == nil {
			t.Fatal("recipe accepted test-read custody")
		}
	})
	t.Run("tests-intent-with-recipe", func(t *testing.T) {
		changed := assistantVariantClone(t, record)
		for i := range changed.Operations {
			op := &changed.Operations[i]
			if op.ID == test.OperationID {
				var in assistantHostIntent
				assistantRunMust(t, readAssistantHostReceipt(op.Intent, &in))
				in.RecipeProposal = recipeIntent.RecipeProposal
				op.Intent, _ = json.Marshal(in)
			}
		}
		if validateAssistantTestOutput(changed, test, receipts) == nil {
			t.Fatal("test producer accepted recipe metadata")
		}
		if validateAssistantDomainOutputs(changed) == nil {
			t.Fatal("mixed test intent imported")
		}
	})
	for _, mode := range []string{"recipe", "policy", "dialect"} {
		t.Run("test-read-with-"+mode, func(t *testing.T) {
			for _, op := range record.Operations {
				if op.Name != "read_pack_tests" || op.Kind != "host" {
					continue
				}
				var in assistantHostIntent
				assistantRunMust(t, readAssistantHostReceipt(op.Intent, &in))
				if mode == "recipe" {
					in.RecipeProposal = recipeIntent.RecipeProposal
				} else if mode == "policy" {
					in.Policy = &assistantPolicyHostBinding{}
				} else {
					in.DialectAction = &assistantDialectActionMetadata{}
				}
				if validateAssistantTestReadReceipt(op, in, receipts[op.ID]) == nil {
					t.Fatal("test read accepted foreign intent metadata")
				}
			}
		})
	}
	t.Run("recipe-read-with-policy", func(t *testing.T) {
		if validateAssistantDialectRecipeIntent(record, assistantRunOperation{Name: "list_dialects"}, assistantHostIntent{Policy: &assistantPolicyHostBinding{}}) == nil {
			t.Fatal("recipe read accepted Policy metadata")
		}
	})
	for _, name := range []string{"list_decisions", "list_dialects", "unknown_historical_tool"} {
		t.Run("foreign-test-receipt-"+name, func(t *testing.T) {
			raw, _ := json.Marshal(assistantHostResult{Tests: &testRead, Result: []byte(`{"content":[]}`)})
			held := assistantRunRecord{Operations: []assistantRunOperation{{Kind: "host", Name: name, Result: raw}}}
			if validateAssistantDomainOutputs(held) == nil {
				t.Fatal("unrelated host accepted test metadata")
			}
		})
	}
	// An existing Graph and a linked Policy each retain their original variant
	// constraints when the shared struct gains the two new optional outputs.
	gs, _, ge := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
	gx := assistantGraphSetup(t, gs, ge)
	if r := assistantGraphInvoke(t, ge, gx, "graph", "propose_graph", map[string]string{"id": "new-flow", "content": assistantGraphCandidate}); r.Err() {
		t.Fatal(r.Value().Text())
	}
	graph := assistantExecutionRecord(t, ge)
	assistantRunMust(t, validateAssistantDomainOutputs(graph))
	for _, mode := range []string{"recipe", "test", "dialect"} {
		changed := assistantVariantClone(t, graph)
		if mode == "recipe" {
			changed.DomainOutputs[0].Recipe = recipe.Recipe
		} else if mode == "test" {
			changed.DomainOutputs[0].TestProposal = test.TestProposal
		} else {
			changed.DomainOutputs[0].DialectAction = &assistantDialectActionOutput{}
		}
		if validateAssistantDomainOutputs(changed) == nil {
			t.Fatal("Graph accepted foreign variant", mode)
		}
	}
	ps, _, pe, p, _ := assistantPolicyFixture(t, "http://127.0.0.1:1", "work", false, nil)
	px := assistantPolicyOpen(t, ps, pe)
	if r := assistantPolicyInvoke(t, pe, px, "policy", "propose_policy_update", map[string]string{"revision": p.Revision, "title": "Candidate", "narrative": assistantPolicyNarrative, "artifactVersion": "1.1.0"}); r.Err() {
		t.Fatal(r.Value().Text())
	}
	policy := assistantExecutionRecord(t, pe)
	pr := assistantVariantReceipts(t, policy)
	pout := policy.DomainOutputs[0]
	assistantRunMust(t, validateAssistantPolicyOutput(policy, pout, pr))
	for _, mode := range []string{"recipe-output", "test-output", "test-receipt", "recipe-intent", "dialect-output", "dialect-receipt", "dialect-intent"} {
		changed := assistantVariantClone(t, policy)
		out := pout
		copy := assistantVariantReceipts(t, policy)
		switch mode {
		case "dialect-output":
			out.DialectAction = &assistantDialectActionOutput{}
		case "dialect-receipt":
			held := copy[out.OperationID]
			held.DialectRead = &assistantDialectRead{}
			copy[out.OperationID] = held
		case "dialect-intent":
			for i := range changed.Operations {
				op := &changed.Operations[i]
				if op.ID == out.OperationID {
					var in assistantHostIntent
					assistantRunMust(t, readAssistantHostReceipt(op.Intent, &in))
					in.DialectAction = &assistantDialectActionMetadata{}
					op.Intent, _ = json.Marshal(in)
				}
			}
		case "recipe-output":
			out.Recipe = recipe.Recipe
		case "test-output":
			out.TestProposal = test.TestProposal
		case "test-receipt":
			held := copy[out.OperationID]
			held.Tests = &testRead
			copy[out.OperationID] = held
		case "recipe-intent":
			for i := range changed.Operations {
				op := &changed.Operations[i]
				if op.ID == out.OperationID {
					var in assistantHostIntent
					assistantRunMust(t, readAssistantHostReceipt(op.Intent, &in))
					in.RecipeProposal = recipeIntent.RecipeProposal
					op.Intent, _ = json.Marshal(in)
				}
			}
		}
		if validateAssistantPolicyOutput(changed, out, copy) == nil {
			t.Fatal("Policy accepted foreign variant", mode)
		}
	}
	// Suite materialization and recipe creation have already completed. Terminal
	// write/readback/ack reuses both receipts and does no model/Runtime work.
	runtimeBefore := len(assistantExecutionRuntimeLog(t, s))
	suiteBefore := policyWorkspaceRead(t, s, token, "/api/pack-tests")
	opsBefore := len(record.Operations)
	cp := *record.Checkpoint
	cp.Status = "complete"
	assistantRunMust(t, e.Checkpoint(t.Context(), cp))
	assistantRunMust(t, e.writer.State(t.Context(), "complete"))
	assistantRunMust(t, e.ProjectChat(t.Context()))
	assistantRunMust(t, e.ProjectChat(t.Context()))
	saved := policyWorkspaceRead(t, s, token, "/api/conversations")
	var doc struct {
		Chats []struct {
			InputDrafts []assistantDialectRecipeProposal `json:"inputDrafts"`
			Checkpoint  json.RawMessage                  `json:"checkpoint"`
		} `json:"chats"`
	}
	assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
	if len(doc.Chats) != 1 || len(doc.Chats[0].InputDrafts) != 1 || doc.Chats[0].InputDrafts[0].DraftID != recipe.Recipe.Proposal.DraftID {
		t.Fatal("combined projection lost exact recipe card")
	}
	if len(assistantExecutionRecord(t, e).Operations) != opsBefore || len(assistantExecutionRuntimeLog(t, s)) != runtimeBefore || policyWorkspaceRead(t, s, token, "/api/pack-tests").SHA256 != suiteBefore.SHA256 {
		t.Fatal("terminal reconciliation repeated work or suite append")
	}
	held, err := s.draftRepository().Get(t.Context(), recipe.Recipe.Retained.ID)
	assistantRunMust(t, err)
	if held.SHA256 != recipe.Recipe.Retained.SHA256 {
		t.Fatal("combined projection changed independent draft")
	}
}
