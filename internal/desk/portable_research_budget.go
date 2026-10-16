package desk

import (
	"encoding/json"
	"time"
)

// Host-authored offer within the same fixed contract. It grants nothing until
// decideResearchBudget persists the exact reviewed increase.
func portableResearchBudgetProposal(run inputResearchRun) *inputResearchBudgetRequest {
	if pending := pendingResearchBudget(run); pending != nil {
		return pending
	}
	if run.StopRequested || run.Portable == nil || len(run.Portable.Response) > 0 || len(run.BudgetRequests) >= 16 || !contains([]string{"running", "partial"}, run.State) || run.Problem != errResearchRequestLimit.Error() {
		return nil
	}
	for _, request := range run.BudgetRequests {
		if request.State == "declined" {
			return nil
		}
	}
	for _, attempt := range run.Attempts {
		if attempt.State != "completed" && attempt.State != "not-started" {
			return nil
		}
	}
	current, used := researchApprovedLimits(run), researchBudgetUsage(run)
	proposed := current
	if used.ModelCalls >= current.ModelCalls {
		proposed.ModelCalls = min(30, current.ModelCalls+6)
	}
	var contract portableResearchRequest
	if json.Unmarshal(run.Portable.Frozen.Plan.Arguments, &contract) != nil {
		return nil
	}
	if contains(contract.Capabilities, "search") && used.Searches >= current.Searches {
		proposed.Searches = min(15, current.Searches+3)
	}
	if contains(contract.Capabilities, "page") && used.Pages >= current.Pages {
		proposed.Pages = min(15, current.Pages+3)
	}
	if proposed == current || validatePortableResearchLimits(proposed) != nil {
		return nil
	}
	raw, _ := json.Marshal([]any{run.ID, len(run.Attempts), current, proposed, used})
	return &inputResearchBudgetRequest{ID: "budget_" + digestOf(raw), State: "pending", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Reason: "The reviewed portable research request limit was reached. Keep the retained acquisitions or approve this bounded increase for the same contract.", Current: current, Proposed: proposed, Used: used}
}
