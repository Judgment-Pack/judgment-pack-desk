package desk

import (
	"encoding/json"
	"errors"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/aifailure"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/delegation"
	"os"
	"regexp"
	"strings"
	"time"
)

const inputResearchDirectory = ".desk-private/input-research"
const inputResearchRecordLimit = 16 << 20

var inputResearchID = regexp.MustCompile(`^research_[a-f0-9]{32}$`)

type inputResearchAttempt struct {
	Background      *backgroundModelReceipt         `json:"background,omitempty"`
	RetainedProof   *portableResearchProofReference `json:"retainedProof,omitempty"`
	Failure         *aifailure.Failure              `json:"failure,omitempty"`
	AppliedSequence int                             `json:"appliedSequence,omitempty"`
	TaskID          string                          `json:"taskId,omitempty"`
	RecordResults   []researchRecordOutcome         `json:"recordResults,omitempty"`

	ReservedBytes      int             `json:"reservedBytes,omitempty"`
	ActionProblem      string          `json:"actionProblem,omitempty"`
	ID                 string          `json:"id"`
	Kind               string          `json:"kind"`
	Digest             string          `json:"digest"`
	State              string          `json:"state"`
	StartedAt          string          `json:"startedAt"`
	FinishedAt         string          `json:"finishedAt,omitempty"`
	Result             json.RawMessage `json:"result,omitempty"`
	Problem            string          `json:"problem,omitempty"`
	ProblemCode        string          `json:"problemCode,omitempty"`
	Recovery           string          `json:"recovery,omitempty"`
	URL                string          `json:"url,omitempty"`
	Query              string          `json:"query,omitempty"`
	Number             int             `json:"number,omitempty"`
	RetryOf            string          `json:"retryOf,omitempty"`
	RetryAt            string          `json:"retryAt,omitempty"`
	RetriedBy          string          `json:"retriedBy,omitempty"`
	RetryNote          string          `json:"retryNote,omitempty"`
	Continued          bool            `json:"continued,omitempty"`
	AcquisitionRefusal bool            `json:"acquisitionRefusal,omitempty"`
}
type inputResearchRun struct {
	Portable            *portableResearchJournal    `json:"portable,omitempty"`
	RelationshipVersion int                         `json:"relationshipVersion,omitempty"`
	Execution           *delegation.Execution       `json:"execution,omitempty"`
	Tasks               []researchTask              `json:"tasks,omitempty"`
	PendingRetries      []inputResearchPendingRetry `json:"pendingRetries,omitempty"`
	// AppliedAttempt is transient: validation must associate out-of-order results by ID.
	AppliedAttempt         string                   `json:"-"`
	SourceOriginIDs        map[string]string        `json:"sourceOriginIds,omitempty"`
	ContractVersion        int                      `json:"contractVersion,omitempty"`
	Reprocess              bool                     `json:"reprocess,omitempty"`
	Selection              []researchRecordSnapshot `json:"selection,omitempty"`
	Existing               []researchRecordSnapshot `json:"existing,omitempty"`
	SourceRuns             map[string]string        `json:"sourceRuns,omitempty"`
	Outcomes               []researchRecordOutcome  `json:"outcomes,omitempty"`
	PublicationResumeState string                   `json:"publicationResumeState,omitempty"`
	PublicationPending     bool                     `json:"publicationPending,omitempty"`

	Candidates      []inputResearchCandidate     `json:"candidates,omitempty"`
	Approval        *inputResearchApproval       `json:"approval,omitempty"`
	BudgetRequests  []inputResearchBudgetRequest `json:"budgetRequests,omitempty"`
	EntityIDs       map[string]string            `json:"entityIds,omitempty"`
	IdentityVersion int                          `json:"identityVersion,omitempty"`
	RowIDs          map[string]string            `json:"rowIds,omitempty"`

	Phase            string `json:"phase,omitempty"`
	PhaseReason      string `json:"phaseReason,omitempty"`
	ContinuationOf   string `json:"continuationOf,omitempty"`
	InheritedRecords int    `json:"inheritedRecords,omitempty"`

	Version       int                    `json:"version"`
	Gateway       inputResearchPin       `json:"gateway"`
	Step          int                    `json:"step"`
	ID            string                 `json:"id"`
	PreparationID string                 `json:"preparationId"`
	Revision      string                 `json:"revision"`
	RecipeVersion int                    `json:"recipeVersion"`
	Intent        string                 `json:"intent"`
	Recipe        inputResearchRecipe    `json:"recipe"`
	CreatedAt     string                 `json:"createdAt"`
	UpdatedAt     string                 `json:"updatedAt"`
	State         string                 `json:"state"`
	Problem       string                 `json:"problem,omitempty"`
	Seeds         []string               `json:"seeds"`
	Companies     []inputResearchEntity  `json:"companies"`
	Records       []inputResearchEntity  `json:"records"`
	Sources       []inputResearchSource  `json:"sources"`
	Attempts      []inputResearchAttempt `json:"attempts"`
	// A byte cap complements request counts. Token/credit costs are unavailable
	// unless the actual provider reports them; count is never a currency estimate.
	RetrievedBytes int                        `json:"retrievedBytes"`
	StopRequested  bool                       `json:"stopRequested"`
	PendingRetry   *inputResearchPendingRetry `json:"pendingRetry,omitempty"`
}

func (s *Server) inputResearchRoot(create bool) (*os.Root, error) {
	root, made, e := openOwnFolder(s.root, strings.Split(inputResearchDirectory, "/"), create, "input research")
	if e == nil && made {
		e = writePrivateData(root, ".gitignore", []byte("*\n"))
		if e != nil {
			root.Close()
		}
	}
	return root, e
}
func openInputResearch(root *os.Root, id string, create bool) (*os.Root, error) {
	if !inputResearchID.MatchString(id) {
		return nil, errors.New("Invalid research ID.")
	}
	r, _, e := openOwnFolder(root, []string{id}, create, "input research run")
	return r, e
}

// beginResearchAttempt must be committed before invoking a provider. A matching
// completed call is reusable; a started call has an uncertain outcome and is
// never automatically replayed. This survives process loss, not just a reload.
func beginResearchAttempt(run *inputResearchRun, kind string, args json.RawMessage) (inputResearchAttempt, bool, error) {
	if run.StopRequested || run.State != "running" {
		return inputResearchAttempt{}, false, errors.New("Research is no longer running.")
	}
	if kind != "model" && kind != "search" && kind != "page" {
		return inputResearchAttempt{}, false, errors.New("Unsupported research operation.")
	}
	key := digestOf(append([]byte(kind+"\n"), args...))
	// Prefer the final completed attempt in a retry group. A failed/in-flight
	// group is never resumed by another invocation, even if policy allows retries.
	var previous *inputResearchAttempt
	for i := range run.Attempts {
		if run.Attempts[i].Digest == key && run.Attempts[i].Kind == kind {
			previous = &run.Attempts[i]
		}
	}
	if previous != nil {
		if previous.State == "completed" {
			return *previous, true, nil
		}
		if previous.Continued && researchContinuableAttempt(*previous) {
			return *previous, false, &researchSourceGap{attemptID: previous.ID, cause: errors.New("This source was already unavailable. Its request was not repeated.")}
		}
		return *previous, false, errors.New("An earlier request has an uncertain or failed outcome. Review it before starting another attempt.")
	}
	if err := researchRequestBudget(run, kind); err != nil {
		return inputResearchAttempt{}, false, err
	}
	attempt := inputResearchAttempt{ID: "attempt_" + key, Kind: kind, Digest: key, State: "started", Number: 1, StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	// Retain only source coordinates, never model prompts or connection secrets.
	attempt.URL, attempt.Query = researchRequestCoordinates(kind, args)
	if err := reserveResearchSource(run, &attempt); err != nil {
		return inputResearchAttempt{}, false, err
	}
	run.Attempts = append(run.Attempts, attempt)
	return attempt, false, nil
}
func finishResearchAttempt(run *inputResearchRun, id string, result json.RawMessage, problem string) error {
	for i := range run.Attempts {
		a := &run.Attempts[i]
		if a.ID != id {
			continue
		}
		if a.State != "started" && a.State != "waiting" {
			return errors.New("Research attempt was already finalized.")
		}
		if a.State == "waiting" && problem == "" {
			return errors.New("An AI request cannot complete before admission.")
		}
		if len(result) > 2<<20 {
			return errors.New("Research response exceeds its byte limit.")
		}
		a.ReservedBytes = 0
		a.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if problem != "" {
			if a.State == "waiting" {
				a.State = "not-started"
			} else {
				a.State = "uncertain"
			}
			a.Problem = problem
		} else {
			a.State = "completed"
			a.Result = append(json.RawMessage(nil), result...)
		}
		return nil
	}
	return errors.New("Research attempt not found.")
}

// refuseResearchAttempt finishes a started source attempt whose acquisition
// completed and which its caller then refused (researchAcquisitionRefused):
// the state is "refused", the code and problem name why, and the retained
// response is kept for audit. Only "completed" attempts are sources, so a
// refused one never reaches the model context or an answer.
func refuseResearchAttempt(run *inputResearchRun, id string, result json.RawMessage, code, problem string) error {
	for i := range run.Attempts {
		a := &run.Attempts[i]
		if a.ID != id {
			continue
		}
		external := run.Portable != nil && a.RetainedProof != nil
		if a.State != "started" || !external && (a.Kind == "model" || len(result) == 0) || code == "" || problem == "" {
			return errors.New("Research attempt cannot be refused.")
		}
		if len(result) > 2<<20 {
			return errors.New("Research response exceeds its byte limit.")
		}
		a.ReservedBytes = 0
		a.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
		a.State = "refused"
		a.Result = append(json.RawMessage(nil), result...)
		a.Problem, a.ProblemCode = problem, code
		return nil
	}
	return errors.New("Research attempt not found.")
}
func recoverInputResearch(run *inputResearchRun) {
	if run.State != "running" {
		return
	}
	run.State = "interrupted"
	for i := range run.Tasks {
		t := &run.Tasks[i]
		if t.State == "running" || t.State == "queued" {
			t.State = "interrupted"
			t.Problem = "The process stopped. Saved sources and findings were kept; the task was not replayed."
			t.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
		}
	}
	for i := range run.Candidates {
		c := &run.Candidates[i]
		if c.State != "reading" {
			continue
		}
		c.State = "uncertain"
		// Completion can be durable just before queue/source publication.
		// Recover its verified metadata locally, never reacquire the URL.
		for _, a := range run.Attempts {
			if a.Kind != "page" || a.State != "completed" || a.URL != c.URL {
				continue
			}
			var source inputResearchSource
			if json.Unmarshal(a.Result, &source) == nil && source.ID == a.ID && source.Kind == "page" && source.Verification == "verified" {
				if retainResearchSource(run, source) == nil {
					c.State = "ready"
					c.SourceID = source.ID
				}
			}
		}
	}
	cancelResearchRetry(run, "The process stopped before the scheduled retry. No further request was made.")
	run.Problem = "Research stopped before completion. Saved records are available; outstanding requests will not be repeated automatically."
	for i := range run.Attempts {
		run.Attempts[i].ReservedBytes = 0
		if run.Attempts[i].State == "waiting" {
			run.Attempts[i].State = "not-started"
			run.Attempts[i].Problem = "The process stopped while waiting for the AI connection. No AI request was sent."
			run.Attempts[i].FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
		}
		if run.Attempts[i].State == "started" {
			run.Attempts[i].State = "uncertain"
			run.Attempts[i].Problem = "The process stopped before the response was retained."
		}
	}
}

// Waiting is known to precede native admission. Commit Started before allowing
// the bridge to use the account, so process loss can never authorize replay.
func researchModelAvailability(run *inputResearchRun, id string, waiting bool) error {
	if len(run.Attempts) == 0 {
		return errors.New("Research attempt not found.")
	}
	a, err := researchAttemptByID(run, id)
	if err != nil {
		return err
	}
	if a.ID != id || a.Kind != "model" || (a.State != "started" && a.State != "waiting") {
		return errors.New("Research attempt is no longer active.")
	}
	if waiting {
		a.State = "waiting"
	} else {
		a.State = "started"
	}
	return nil
}
