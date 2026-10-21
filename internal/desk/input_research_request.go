package desk

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"
)

// The request is host-authored. RequestKey records provenance, not permission:
// an earlier manual or other-call receipt for the same turn remains authoritative.
type inputResearchRequest struct {
	ChatID        string `json:"chatId"`
	PreparationID string `json:"preparationId"`
	Revision      string `json:"revision"`
	TurnID        string `json:"turnId"`
	RequestKey    string `json:"requestKey,omitempty"`
}
type inputResearchRequestReceipt struct {
	Status     string `json:"status"`
	RunID      string `json:"runId,omitempty"`
	UsedAt     string `json:"usedAt,omitempty"`
	RequestKey string `json:"requestKey,omitempty"`
	State      string `json:"state,omitempty"`
}

// Unknown means Update was entered and its acknowledgement is unavailable. It
// never means no run was queued. Recovery may only inspect the used receipt.
type inputResearchRequestUnknown struct{ cause error }

func (e *inputResearchRequestUnknown) Error() string {
	return "The research request outcome is unknown."
}
func (e *inputResearchRequestUnknown) Unwrap() error { return e.cause }
func validResearchRequest(q inputResearchRequest) bool {
	return researchChatID.MatchString(q.ChatID) && researchChatID.MatchString(q.TurnID) && inputPreparationID.MatchString(q.PreparationID) && assistantRunDigest(q.Revision) && (q.RequestKey == "" || assistantRunDigest(q.RequestKey))
}
func researchRequestReceipt(run inputResearchRun) inputResearchRequestReceipt {
	return inputResearchRequestReceipt{Status: "started", RunID: run.ID, UsedAt: run.Approval.UsedAt, RequestKey: run.Approval.RequestKey, State: run.State}
}
func matchingResearchRequest(run inputResearchRun, q inputResearchRequest) bool {
	return run.PreparationID == q.PreparationID && run.Revision == q.Revision && run.Approval != nil && run.Approval.ChatID == q.ChatID && run.Approval.TurnID == q.TurnID && run.Approval.UsedAt != ""
}

// inspectResearchRequest is observation only. It does not recover a worker,
// acquire an admission lease, validate a fresh recipe, or consume an allowance.
func (s *Server) inspectResearchRequest(r *http.Request, q inputResearchRequest) (inputResearchRequestReceipt, error) {
	if !validResearchRequest(q) {
		return inputResearchRequestReceipt{}, errAssistantRunInvalid
	}
	root, err := s.inputResearchRoot(false)
	if err != nil {
		return inputResearchRequestReceipt{}, err
	}
	defer root.Close()
	grants, err := s.researchApprovals(r.Context(), root, q.ChatID, s.researchApprovalScope(r))
	if err != nil {
		return inputResearchRequestReceipt{}, err
	}
	for _, grant := range grants {
		if matchingResearchRequest(grant, q) {
			return researchRequestReceipt(grant), nil
		}
	}
	return inputResearchRequestReceipt{}, os.ErrNotExist
}

// Notification only; fixtures count owner entries without changing authority,
// request data, receipts or the owning journal.
var testHookResearchRequest func()

// Lock order is conversation/private -> research admission -> run -> preparation.
// Authority is per invocation and runs under the private lock: it must never
// reacquire that lock or the assistant ledger. Nil preserves manual admission.
func (s *Server) requestResearch(r *http.Request, q inputResearchRequest, authority func(context.Context) error) (inputResearchRequestReceipt, error) {
	if testHookResearchRequest != nil {
		testHookResearchRequest()
	}
	empty := inputResearchRequestReceipt{}
	if !validResearchRequest(q) {
		return empty, errAssistantRunInvalid
	}
	lock, err := s.privateDataLockContext(r.Context(), true)
	if err != nil {
		return empty, err
	}
	defer lock.Close()
	if err = s.checkChatExecutionLocked(q.ChatID); err != nil {
		return empty, err
	}
	root, err := s.inputResearchRoot(false)
	if errors.Is(err, os.ErrNotExist) {
		return inputResearchRequestReceipt{Status: "review-required"}, nil
	}
	if err != nil {
		return empty, err
	}
	defer root.Close()
	admission, err := root.Open(".")
	if err != nil {
		return empty, err
	}
	defer admission.Close()
	if err = lockPrivateData(admission, true); err != nil {
		return empty, errors.New("Another research change is being saved. Try again after it finishes.")
	}
	grants, err := s.researchApprovals(r.Context(), root, q.ChatID, s.researchApprovalScope(r))
	if err != nil {
		return empty, err
	}
	// Same-turn idempotency precedes all fresh source, budget and grant checks.
	for _, grant := range grants {
		if matchingResearchRequest(grant, q) {
			return researchRequestReceipt(grant), nil
		}
	}
	authorize := func() error {
		if _, ok := s.sessionOf(r); !ok {
			return errAssistantRunScope
		}
		if err := s.authorizePrivateWorkspace(r); err != nil {
			return err
		}
		if err := r.Context().Err(); err != nil {
			return err
		}
		if err := s.checkChatExecutionLocked(q.ChatID); err != nil {
			return err
		}
		if authority != nil {
			return authority(r.Context())
		}
		return nil
	}
	if err = authorize(); err != nil {
		return empty, err
	}
	for _, grant := range grants {
		if researchApprovalState(grant, time.Now()) != "ready" || grant.PreparationID != q.PreparationID || grant.Revision != q.Revision {
			continue
		}
		if err = s.researchInputAvailable(r.Context(), root, grant.PreparationID, grant.ID); err != nil {
			return empty, err
		}
		if err = s.researchBudgetIsLatest(r.Context(), root, grant); err != nil {
			return empty, err
		}
		rr, err := openInputResearch(root, grant.ID, false)
		if err != nil {
			return empty, err
		}
		defer rr.Close()
		// Acquire under the run lock, but keep this owning read view through the
		// journal commit/readback; no replacement can slip past validation.
		var preparation inputPreparationRead
		defer func() {
			if preparation != nil {
				preparation.Close()
			}
		}()
		attempted := false
		updated, err := s.researchJournal(rr, grant.ID).Update(r.Context(), func(current *inputResearchRun) error {
			if current.Approval == nil || current.Approval.ChatID != q.ChatID || current.Approval.SessionScope != s.researchApprovalScope(r) || current.PreparationID != q.PreparationID || current.Revision != q.Revision || current.State != "review" || researchApprovalState(*current, time.Now()) != "ready" {
				return errors.New("This research allowance is no longer available.")
			}
			var err error
			// Do not wait for a preparation view while holding private/run.
			// Projection acquires its owning view before the private ledger.
			preparation, err = s.preparationRepository().TryRead(r.Context())
			if err != nil {
				return err
			}
			if _, err := s.validateResearchAdmissionView(r, *current, preparation); err != nil {
				return err
			}
			if err := s.researchInputAvailable(r.Context(), root, current.PreparationID, current.ID); err != nil {
				return err
			}
			if err := s.researchBudgetIsLatest(r.Context(), root, *current); err != nil {
				return err
			}
			if err := authorize(); err != nil {
				return err
			}
			now := time.Now().UTC()
			if researchApprovalState(*current, now) != "ready" {
				return errors.New("The research allowance expired. Review a new allowance.")
			}
			current.Approval.UsedAt = now.Format(time.RFC3339Nano)
			current.Approval.TurnID = q.TurnID
			current.Approval.RequestKey = q.RequestKey
			current.State = "queued"
			attempted = true
			return nil
		})
		if err != nil {
			if attempted {
				return empty, &inputResearchRequestUnknown{err}
			}
			return empty, err
		}
		return researchRequestReceipt(updated), nil
	}
	return inputResearchRequestReceipt{Status: "review-required"}, nil
}
