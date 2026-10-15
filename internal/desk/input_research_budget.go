package desk

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Every grant is for one exact run and a monotonic increase in request counts.
// The saved Recipe, record target, connection, retries and evidence policy stay
// immutable. Chat/Research preferences never grant additional allowance.
type inputResearchBudgetRequest struct {
	ID         string              `json:"id"`
	State      string              `json:"state"`
	CreatedAt  string              `json:"createdAt"`
	ResolvedAt string              `json:"resolvedAt,omitempty"`
	Reviewer   *inputReviewActor   `json:"reviewer,omitempty"`
	Reason     string              `json:"reason"`
	Current    inputResearchLimits `json:"current"`
	Proposed   inputResearchLimits `json:"proposed"`
	Used       inputResearchLimits `json:"used"`
}

func researchApprovedLimits(run inputResearchRun) inputResearchLimits {
	limits := run.Recipe.Limits
	for _, request := range run.BudgetRequests {
		if request.State == "approved" {
			limits = request.Proposed
		}
	}
	return limits
}
func researchBudgetUsage(run inputResearchRun) inputResearchLimits {
	used := inputResearchLimits{Records: len(run.Records)}
	for _, a := range run.Attempts {
		if a.State == "waiting" || a.State == "not-started" {
			continue
		}
		switch a.Kind {
		case "model":
			used.ModelCalls++
		case "search":
			used.Searches++
		case "page":
			used.Pages++
		}
	}
	return used
}
func pendingResearchBudget(run inputResearchRun) *inputResearchBudgetRequest {
	if n := len(run.BudgetRequests); n > 0 && run.BudgetRequests[n-1].State == "pending" {
		v := run.BudgetRequests[n-1]
		return &v
	}
	return nil
}
func validateResearchBudgets(run inputResearchRun) error {
	bad := errors.New("The saved research budget approvals are invalid.")
	if len(run.BudgetRequests) > 16 {
		return bad
	}
	current := run.Recipe.Limits
	seen := map[string]bool{}
	for i, request := range run.BudgetRequests {
		proposed := request.Proposed
		if !strings.HasPrefix(request.ID, "budget_") || len(request.ID) != 71 || seen[request.ID] || request.Current != current || request.Used.Records < 0 || request.Used.Searches < 0 || request.Used.Pages < 0 || request.Used.ModelCalls < 0 || request.Used.Records > current.Records || request.Used.Searches > current.Searches || request.Used.Pages > current.Pages || request.Used.ModelCalls > current.ModelCalls {
			return bad
		}
		seen[request.ID] = true
		if proposed.Records != current.Records || proposed.Searches < current.Searches || proposed.Pages < current.Pages || proposed.ModelCalls < current.ModelCalls || proposed == current {
			return bad
		}
		recipe := run.Recipe
		recipe.Limits = proposed
		if validateInputResearch(&recipe) != nil || (run.ContinuationOf != "" || run.Reprocess) {
			return bad
		}
		if _, err := time.Parse(time.RFC3339Nano, request.CreatedAt); err != nil {
			return bad
		}
		switch request.State {
		case "pending":
			if i != len(run.BudgetRequests)-1 || run.State != "paused" || request.ResolvedAt != "" || request.Reviewer != nil {
				return bad
			}
		case "approved", "declined":
			if _, err := time.Parse(time.RFC3339Nano, request.ResolvedAt); err != nil || request.Reviewer == nil {
				return bad
			}
			if request.State == "approved" {
				current = proposed
			} else if i != len(run.BudgetRequests)-1 {
				return bad
			}
		default:
			return bad
		}
	}
	if run.State == "paused" && pendingResearchBudget(run) == nil {
		return bad
	}
	return nil
}

// A conservative, bounded host recommendation. It is an offer, never a grant.
// Only a genuine budget stop can create it; provider/auth/policy failures cannot.
func researchBudgetProposal(run inputResearchRun) *inputResearchBudgetRequest {
	if request := pendingResearchBudget(run); request != nil {
		return request
	}
	if run.StopRequested || (run.ContinuationOf != "" || run.Reprocess) || len(run.BudgetRequests) >= 16 || len(run.Records) >= run.Recipe.Limits.Records {
		return nil
	}
	if !contains([]string{"running", "completed-with-gaps", "partial"}, run.State) {
		return nil
	}
	for _, r := range run.BudgetRequests {
		if r.State == "declined" {
			return nil
		}
	}
	current, used := researchApprovedLimits(run), researchBudgetUsage(run)
	if run.Phase != "finalize" && used.ModelCalls < current.ModelCalls {
		return nil
	}
	if len(run.Attempts) > 0 {
		last := run.Attempts[len(run.Attempts)-1]
		if last.ActionProblem != "" || last.State != "completed" && !(last.Continued && researchContinuableAttempt(last)) {
			return nil
		}
	}
	proposed := current
	remaining := current.Records - len(run.Records)
	// A run always needs room for a fresh AI action and its save checkpoint.
	if current.ModelCalls-used.ModelCalls <= 2 {
		proposed.ModelCalls = min(60, current.ModelCalls+min(20, max(6, remaining*3+2)))
	}
	if proposed.ModelCalls-used.ModelCalls <= 2 {
		return nil
	}
	if current.Pages-used.Pages < min(remaining, 3) {
		proposed.Pages = min(100, current.Pages+max(1, min(10, remaining)-(current.Pages-used.Pages)))
	}
	if run.Recipe.Search != nil && current.Searches-used.Searches == 0 {
		proposed.Searches = min(30, current.Searches+min(5, remaining))
	}
	if proposed == current {
		return nil
	}
	reason := fmt.Sprintf("%d of %d records are saved. The current request limits leave too little room for further discovery and saving. Review the additional allowance to continue; more records are not guaranteed.", len(run.Records), current.Records)
	raw, _ := json.Marshal([]any{run.ID, run.Step, len(run.Attempts), current, proposed, used})
	return &inputResearchBudgetRequest{ID: "budget_" + digestOf(raw), State: "pending", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Reason: reason, Current: current, Proposed: proposed, Used: used}
}
func pauseResearchForBudget(run *inputResearchRun) bool {
	request := researchBudgetProposal(*run)
	if request == nil {
		return false
	}
	if pendingResearchBudget(*run) == nil {
		run.BudgetRequests = append(run.BudgetRequests, *request)
	}
	run.State = "paused"
	run.Problem = strings.TrimSpace(run.PhaseReason + " Research paused at its approved request limits. Review an increase to continue, or keep the saved results.")
	return true
}
func decideResearchBudget(run *inputResearchRun, id, decision string, actor inputReviewActor) error {
	request := pendingResearchBudget(*run)
	if run.State != "paused" || run.StopRequested || request == nil || request.ID != id {
		return errors.New("This budget request was already resolved or changed. Reload research.")
	}
	if decision != "approve" && decision != "decline" {
		return errors.New("Choose whether to approve this increase or keep the saved results.")
	}
	held := &run.BudgetRequests[len(run.BudgetRequests)-1]
	held.ResolvedAt = time.Now().UTC().Format(time.RFC3339Nano)
	held.Reviewer = &actor
	if decision == "decline" {
		held.State = "declined"
		run.State = "completed-with-gaps"
		run.Problem = researchKeptResults(*run)
	} else {
		held.State = "approved"
		run.State = "queued"
		run.Phase = ""
		run.PhaseReason = ""
		run.Problem = ""
	}
	return nil
}

func researchKeptResults(run inputResearchRun) string {
	return fmt.Sprintf("Research ended. Saved records: %d. Retained sources: %d. No additional requests were authorized.", len(run.Records), len(run.Sources))
}
