package desk

import (
	"encoding/json"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/aifailure"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/delegation"
	"net/url"
	"strings"
)

// Activity is a bounded display projection, never the provider payload or an
// audit verification verdict. Polling it does not ship prompts or page bodies.
type inputResearchStep struct {
	Explanation   string                  `json:"explanation,omitempty"`
	Failure       *aifailure.Failure      `json:"failure,omitempty"`
	TaskID        string                  `json:"taskId,omitempty"`
	RecordResults []researchRecordOutcome `json:"recordResults,omitempty"`
	Recovered     bool                    `json:"recovered,omitempty"`

	ActionProblem string `json:"actionProblem,omitempty"`
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	State         string `json:"state"`
	StartedAt     string `json:"startedAt"`
	FinishedAt    string `json:"finishedAt,omitempty"`
	URL           string `json:"url,omitempty"`
	Query         string `json:"query,omitempty"`
	Problem       string `json:"problem,omitempty"`
	ProblemCode   string `json:"problemCode,omitempty"`
	Recovery      string `json:"recovery,omitempty"`
	Result        string `json:"result,omitempty"`
	Digest        string `json:"digest,omitempty"`
	Number        int    `json:"number,omitempty"`
	MaxAttempts   int    `json:"maxAttempts,omitempty"`
	RetryOf       string `json:"retryOf,omitempty"`
	RetryAt       string `json:"retryAt,omitempty"`
	RetriedBy     string `json:"retriedBy,omitempty"`
	RetryNote     string `json:"retryNote,omitempty"`
	Continued     bool   `json:"continued,omitempty"`
}
type inputResearchSummary struct {
	Explanation        string                 `json:"explanation,omitempty"`
	Execution          *delegation.Execution  `json:"execution,omitempty"`
	Tasks              []researchTaskView     `json:"tasks,omitempty"`
	Reprocess          bool                   `json:"reprocess,omitempty"`
	SelectionCount     int                    `json:"selectionCount,omitempty"`
	Outcomes           *researchOutcomeCounts `json:"outcomes,omitempty"`
	PublicationPending bool                   `json:"publicationPending,omitempty"`

	ActivePageReads int                         `json:"activePageReads,omitempty"`
	BudgetRequest   *inputResearchBudgetRequest `json:"budgetRequest,omitempty"`
	CanIncrease     bool                        `json:"canIncrease,omitempty"`
	ApprovedLimits  inputResearchLimits         `json:"approvedLimits"`
	Phase           string                      `json:"phase,omitempty"`
	CanContinue     bool                        `json:"canContinue,omitempty"`
	SourceCount     int                         `json:"sourceCount"`

	ID                 string                     `json:"id"`
	State              string                     `json:"state"`
	CreatedAt          string                     `json:"createdAt"`
	UpdatedAt          string                     `json:"updatedAt"`
	RecipeVersion      int                        `json:"recipeVersion"`
	RecordCount        int                        `json:"recordCount"`
	Problem            string                     `json:"problem,omitempty"`
	StopRequested      bool                       `json:"stopRequested"`
	StepCount          int                        `json:"stepCount"`
	CurrentStep        *inputResearchStep         `json:"currentStep,omitempty"`
	Recovery           string                     `json:"recovery,omitempty"`
	UnavailableSources int                        `json:"unavailableSources,omitempty"`
	PendingRetry       *inputResearchPendingRetry `json:"pendingRetry,omitempty"`
}
type inputResearchActivity struct {
	Approval *inputResearchApprovalView `json:"approval,omitempty"`
	inputResearchSummary
	Steps          []inputResearchStep          `json:"steps"`
	BudgetRequests []inputResearchBudgetRequest `json:"budgetRequests,omitempty"`
}

func researchSummary(run inputResearchRun) inputResearchSummary {
	summary := inputResearchSummary{ID: run.ID, State: run.State, CreatedAt: run.CreatedAt, UpdatedAt: run.UpdatedAt, RecipeVersion: run.RecipeVersion, RecordCount: len(run.Records), Problem: run.Problem, StopRequested: run.StopRequested, StepCount: len(run.Attempts)}
	// Recover the model's last assessment from the immutable journal. Budget
	// transitions used to overwrite Problem, hiding why no records were saved.
	// This is display-only: it cannot create evidence, records or permissions.
	if !contains([]string{"running", "queued", "review"}, run.State) {
		for i := len(run.Attempts) - 1; i >= 0; i-- {
			a := run.Attempts[i]
			if a.Kind == "model" && a.TaskID == "" {
				summary.Explanation = researchAttemptExplanation(a)
				break
			}
		}
	}
	if summary.Problem == "Research finished with the saved results. The additional allowance was declined." {
		summary.Problem = researchKeptResults(run)
	}
	summary.Execution = run.Execution
	summary.Tasks = researchTaskViews(run)
	summary.Reprocess = run.Reprocess
	summary.SelectionCount = len(run.Selection)
	summary.PublicationPending = run.PublicationPending
	if run.ContractVersion >= 2 {
		counts := countResearchOutcomes(run)
		summary.Outcomes = &counts
	}
	summary.BudgetRequest = pendingResearchBudget(run)
	summary.CanIncrease = researchBudgetProposal(run) != nil
	summary.ApprovedLimits = researchApprovedLimits(run)
	summary.Phase = run.Phase
	summary.CanContinue = researchCanContinue(run)
	summary.SourceCount = len(run.Sources)
	summary.UnavailableSources = len(researchSourceFailures(run))
	if run.State == "running" && !run.StopRequested {
		summary.PendingRetry = run.PendingRetry
	}
	if len(run.Attempts) > 0 {
		step := researchStep(run, len(run.Attempts)-1)
		if step.Problem != "" && step.Problem == run.Problem {
			summary.Recovery = step.Recovery
		}
	}
	if run.State == "running" {
		for i := range run.Attempts {
			step := researchStep(run, i)
			if step.State == "started" || step.State == "waiting" {
				if summary.CurrentStep == nil {
					summary.CurrentStep = &step
				}
				if step.Kind == "page" && step.State == "started" {
					summary.ActivePageReads++
				}
			}
		}
	}
	return summary
}
func researchActivity(run inputResearchRun) inputResearchActivity {
	activity := inputResearchActivity{Approval: researchApprovalEvidence(run), inputResearchSummary: researchSummary(run), Steps: []inputResearchStep{}, BudgetRequests: run.BudgetRequests}
	for i := range run.Attempts {
		activity.Steps = append(activity.Steps, researchStep(run, i))
	}
	return activity
}
func researchRequestCoordinates(kind string, raw json.RawMessage) (string, string) {
	var v struct {
		URL   string `json:"url"`
		Query string `json:"query"`
	}
	if kind == "model" || json.Unmarshal(raw, &v) != nil {
		return "", ""
	}
	if kind == "page" {
		normalized, e := inputResearchURL(v.URL)
		if e == nil {
			return normalized, ""
		}
	}
	if kind == "search" && len(v.Query) <= 2000 {
		return "", v.Query
	}
	return "", ""
}
func researchStep(run inputResearchRun, index int) inputResearchStep {
	a := run.Attempts[index]
	step := inputResearchStep{Failure: a.Failure, TaskID: a.TaskID, ID: a.ID, Kind: a.Kind, State: a.State, StartedAt: a.StartedAt, FinishedAt: a.FinishedAt, URL: a.URL, Query: a.Query, Problem: a.Problem, ProblemCode: a.ProblemCode, Recovery: a.Recovery, Digest: a.Digest}
	step.Explanation = researchAttemptExplanation(a)
	step.ActionProblem = a.ActionProblem
	step.RecordResults = a.RecordResults
	failed, recovered := 0, 0
	for _, result := range a.RecordResults {
		if result.State == "needs-correction" {
			failed++
			if result.RecoveredBy != "" {
				recovered++
			}
		}
	}
	step.Recovered = failed > 0 && failed == recovered
	step.Continued = a.Continued
	step.Number, step.RetryOf, step.RetryAt, step.RetriedBy, step.RetryNote = researchAttemptNumber(a), a.RetryOf, a.RetryAt, a.RetriedBy, a.RetryNote
	if run.Recipe.Retries != nil && run.Recipe.Retries.Mode == "temporary" && a.Kind == "search" {
		step.MaxAttempts = run.Recipe.Retries.MaxRetries + 1
	}
	// Old journals retained a source request's hash, not its coordinates. Recover
	// those only from an immediately preceding action whose exact args hash agrees.
	if a.Kind != "model" && step.URL == "" && step.Query == "" && index > 0 {
		previous := run.Attempts[index-1]
		if previous.Kind == "model" && previous.State == "completed" {
			action, e := decodeResearchAction(previous.Result)
			if e == nil {
				kind, args, e := researchSourceArguments(run, action)
				if e == nil && kind == a.Kind && digestOf(append([]byte(kind+"\n"), args...)) == a.Digest {
					step.URL, step.Query = researchRequestCoordinates(kind, args)
				}
			}
		}
	}
	if a.State == "completed" {
		if a.Kind == "model" {
			if action, e := decodeResearchAction(a.Result); e == nil {
				step.Result = action.Tool
			}
		} else {
			var source inputResearchSource
			if json.Unmarshal(a.Result, &source) == nil {
				if step.URL == "" {
					step.URL = source.URL
				}
				if step.Query == "" {
					step.Query = source.Query
				}
				step.Result = "source-saved"
			}
		}
	}
	// Legacy refusals lack the policy marker. This is only a settings review
	// hint for a known Google starting URL, never permission to retry a request.
	if step.Recovery == "" && a.Problem == researchSourceFailure(400, []byte(`{"error":"web-redirect-origin"}`)).Error() && googleResearchRedirect(step.URL) {
		step.Recovery = "google-grounding"
	}
	return step
}
func googleResearchRedirect(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Scheme == "https" && u.Host == "vertexaisearch.cloud.google.com" && u.User == nil && strings.HasPrefix(u.Path, "/grounding-api-redirect/")
}
func researchRedirectRecovery(message string) string {
	value := strings.TrimPrefix(strings.TrimSpace(message), "source failed: ")
	if len(value) > 200 || !strings.HasPrefix(value, "web-redirect-origin ") {
		return ""
	}
	var envelope struct {
		Redirect struct {
			Policy    string `json:"policy"`
			Permitted *bool  `json:"permitted"`
			From      string `json:"from"`
			To        string `json:"to"`
		} `json:"redirect"`
	}
	if json.Unmarshal([]byte(strings.TrimPrefix(value, "web-redirect-origin ")), &envelope) != nil {
		return ""
	}
	v := envelope.Redirect
	target, e := url.Parse(v.To)
	if v.Policy != "google-grounding" || v.Permitted == nil || *v.Permitted || v.From != "https://vertexaisearch.cloud.google.com" || e != nil || target.Scheme != "https" || target.Host == "" || target.User != nil || target.Port() != "" || v.To != "https://"+target.Host {
		return ""
	}
	return "google-grounding"
}

// Only a completed, accepted finish action owns an assessment. Do not project
// raw provider failures, rejected arguments or a child task as the run result.
func researchAttemptExplanation(a inputResearchAttempt) string {
	if a.Kind != "model" || a.State != "completed" || a.ActionProblem != "" {
		return ""
	}
	action, err := decodeResearchAction(a.Result)
	if err != nil || action.Tool != "finish" {
		return ""
	}
	var args struct {
		Reason string `json:"reason,omitempty"`
	}
	if decodeDataJSON(action.Arguments, &args) != nil || len(args.Reason) > 2000 {
		return ""
	}
	return strings.TrimSpace(args.Reason)
}
