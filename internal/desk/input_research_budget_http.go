package desk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"
)

type inputResearchBudgetReview struct {
	RunID         string                     `json:"runId"`
	Revision      string                     `json:"revision"`
	RecipeVersion int                        `json:"recipeVersion"`
	RecordCount   int                        `json:"recordCount"`
	SourceCount   int                        `json:"sourceCount"`
	Request       inputResearchBudgetRequest `json:"request"`
}

// Called under the common cross-process research admission lock. Approval and
// its queued state are a single durable write. The browser cannot supply limits.
func (s *Server) handleResearchBudget(w http.ResponseWriter, r *http.Request, root, runRoot *os.Root, run inputResearchRun) {
	fail := func(err error) { writeJSONCoded(w, 409, CodeBadRequest, err.Error()) }
	review := strings.HasSuffix(r.URL.Path, "/budget-review")
	var request struct {
		RequestID string `json:"requestId"`
		Decision  string `json:"decision"`
	}
	if review {
		var empty struct{}
		if inputRequest(r, &empty) != nil {
			writeJSONCoded(w, 400, CodeBadRequest, "No extra budget review arguments are accepted.")
			return
		}
	} else if inputRequest(r, &request) != nil || (request.Decision != "approve" && request.Decision != "decline") || request.RequestID == "" {
		writeJSONCoded(w, 400, CodeBadRequest, "Choose a decision for the exact saved budget request.")
		return
	}
	actor := inputReviewActor{Kind: "local-access"}
	if held, ok := s.sessionOf(r); ok {
		actor = inputReviewActor{Kind: "session", Subject: held.subject, Issuer: held.issuer, Name: held.name}
	}
	if !review && request.Decision == "decline" {
		updated, err := s.researchJournal(runRoot, run.ID).Update(r.Context(), func(current *inputResearchRun) error {
			return decideResearchBudget(current, request.RequestID, "decline", actor)
		})
		if err != nil {
			fail(err)
			return
		}
		writeJSON(w, 200, updated)
		return
	}
	if err := s.researchInputAvailable(r.Context(), root, run.PreparationID, run.ID); err != nil {
		fail(err)
		return
	}
	// An older finished run must not be resumed over a newer confirmed run.
	if err := s.researchBudgetIsLatest(r.Context(), root, run); err != nil {
		fail(err)
		return
	}
	var response inputResearchBudgetReview
	updated, err := s.researchJournal(runRoot, run.ID).Update(r.Context(), func(current *inputResearchRun) error {
		if review {
			if current.State != "paused" && current.State != "completed-with-gaps" && current.State != "partial" {
				return errors.New("This research is not waiting for a budget increase.")
			}
		} else if current.State != "paused" || pendingResearchBudget(*current) == nil || pendingResearchBudget(*current).ID != request.RequestID {
			return errors.New("This budget request was already resolved or changed. Reload research.")
		}
		p, err := s.preparationRepository().Read(r.Context())
		if err != nil {
			return err
		}
		defer p.Close()
		saved, err := p.Get(current.PreparationID)
		if err != nil {
			return err
		}
		if saved.SHA256 != r.Header.Get("If-Match") {
			return errors.New("Dialects changed after this review. Reload and review the increase again.")
		}
		if saved.Document.RecipeVersion != current.RecipeVersion || saved.Document.Recipe.Research == nil || saved.Document.Recipe.Intent != current.Intent {
			return errors.New("The recipe changed. Keep the saved results and start new research using the current recipe.")
		}
		before, _ := json.Marshal(current.Recipe)
		after, _ := json.Marshal(saved.Document.Recipe.Research)
		if string(before) != string(after) {
			return errors.New("The research setup changed. Review a new research run.")
		}
		companies, err := selectedResearchCompanies(p, saved.Document, current.Recipe)
		if err != nil {
			return err
		}
		a, _ := json.Marshal(companies)
		b, _ := json.Marshal(current.Companies)
		if len(companies) != len(current.Companies) || len(companies) > 0 && string(a) != string(b) {
			return errors.New("The selected company records changed. Start new research with a reviewed selection.")
		}
		if !review {
			if err = s.validateResearchSearchConnection(r.Context(), current.Recipe); err != nil {
				return err
			}
			if err = s.validateResearchConnection(r.Context(), current.Recipe.AI); err != nil {
				return err
			}
			gateway, err := s.configuredResearch()
			if err != nil {
				return err
			}
			if gateway.authority != current.Gateway.Authority || gateway.signerPublic != current.Gateway.Public {
				return errors.New("The source Gateway changed. Start new research.")
			}
			if err = s.verifyResearchContinuation(r.Context(), runRoot, *current); err != nil {
				return err
			}
			return decideResearchBudget(current, request.RequestID, "approve", actor)
		}
		if !pauseResearchForBudget(current) {
			return errors.New("No additional allowance is available for this run. Keep the saved results and review the recipe.")
		}
		response = inputResearchBudgetReview{RunID: current.ID, Revision: saved.SHA256, RecipeVersion: current.RecipeVersion, RecordCount: len(current.Records), SourceCount: len(current.Sources), Request: *pendingResearchBudget(*current)}
		return nil
	})
	if err != nil {
		fail(err)
		return
	}
	if review {
		writeJSON(w, 200, response)
	} else {
		writeJSON(w, 200, updated)
	}
}
func (s *Server) researchBudgetIsLatest(ctx context.Context, root *os.Root, run inputResearchRun) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !inputResearchID.MatchString(entry.Name()) || entry.Name() == run.ID {
			continue
		}
		rr, err := openInputResearch(root, entry.Name(), false)
		if err != nil {
			return err
		}
		other, err := s.researchJournal(rr, entry.Name()).Read(ctx)
		rr.Close()
		if err != nil {
			return err
		}
		started, startErr := time.Parse(time.RFC3339Nano, run.CreatedAt)
		otherStarted, otherErr := time.Parse(time.RFC3339Nano, other.CreatedAt)
		if startErr != nil || otherErr != nil {
			return errors.New("Research dates could not be checked. Reload research before continuing.")
		}
		if other.PreparationID == run.PreparationID && (otherStarted.After(started) || otherStarted.Equal(started) && other.ID > run.ID) && (len(other.Attempts) > 0 || other.State == "queued" || other.State == "running" || other.State == "paused") {
			return errors.New("Newer research exists for these inputs. Review its results before continuing.")
		}
	}
	return nil
}
