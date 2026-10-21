package desk

// A composer permission is a reviewed run waiting for a chat request, not a
// browser preference. Consuming it and queueing that exact run is one write.
import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

var researchChatID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,160}$`)

type inputResearchApproval struct {
	RequestKey   string           `json:"requestKey,omitempty"`
	ChatID       string           `json:"chatId"`
	Name         string           `json:"name"`
	SessionScope string           `json:"sessionScope"`
	Reviewer     inputReviewActor `json:"reviewer"`
	ApprovedAt   string           `json:"approvedAt"`
	ExpiresAt    string           `json:"expiresAt"`
	RevokedAt    string           `json:"revokedAt,omitempty"`
	UsedAt       string           `json:"usedAt,omitempty"`
	TurnID       string           `json:"turnId,omitempty"`
}
type inputResearchApprovalView struct {
	ChatID        string              `json:"chatId"`
	TurnID        string              `json:"turnId,omitempty"`
	Reviewer      inputReviewActor    `json:"reviewer"`
	RunID         string              `json:"runId"`
	PreparationID string              `json:"preparationId"`
	Name          string              `json:"name"`
	RecipeVersion int                 `json:"recipeVersion"`
	Limits        inputResearchLimits `json:"limits"`
	State         string              `json:"state"`
	ApprovedAt    string              `json:"approvedAt"`
	ExpiresAt     string              `json:"expiresAt"`
	UsedAt        string              `json:"usedAt,omitempty"`
	RevokedAt     string              `json:"revokedAt,omitempty"`
}

func validateResearchApproval(run inputResearchRun) error {
	a := run.Approval
	if a == nil {
		return nil
	}
	bad := errors.New("The saved research permission is invalid.")
	start, e := time.Parse(time.RFC3339Nano, a.ApprovedAt)
	end, e2 := time.Parse(time.RFC3339Nano, a.ExpiresAt)
	if e != nil || e2 != nil || !end.After(start) || end.Sub(start) > time.Hour || !researchChatID.MatchString(a.ChatID) || len(a.SessionScope) != 64 || run.ContinuationOf != "" {
		return bad
	}
	if a.RequestKey != "" && (!assistantRunDigest(a.RequestKey) || a.UsedAt == "") {
		return bad
	}
	if (a.UsedAt == "") != (a.TurnID == "") || a.UsedAt != "" && a.RevokedAt != "" {
		return bad
	}
	if a.UsedAt != "" {
		used, e := time.Parse(time.RFC3339Nano, a.UsedAt)
		if e != nil || used.Before(start) || !used.Before(end) || !researchChatID.MatchString(a.TurnID) || run.State == "review" {
			return bad
		}
	} else if run.State != "review" && run.State != "stopped" {
		return bad
	}
	if a.RevokedAt != "" {
		if _, e := time.Parse(time.RFC3339Nano, a.RevokedAt); e != nil {
			return bad
		}
	}
	return nil
}
func (s *Server) researchApprovalScope(r *http.Request) string {
	// A domain-separated MAC is not a session credential. Reload retains it;
	// sign-out, another window's session and a server restart cannot reuse it.
	return s.sessions.handle("input-research-approval\n" + bearerOf(r))
}
func researchApprovalState(run inputResearchRun, now time.Time) string {
	a := run.Approval
	if a == nil {
		return "manual"
	}
	if a.UsedAt != "" {
		return "used"
	}
	if a.RevokedAt != "" || run.StopRequested {
		return "revoked"
	}
	until, err := time.Parse(time.RFC3339Nano, a.ExpiresAt)
	if err != nil || !now.Before(until) {
		return "expired"
	}
	return "ready"
}
func researchApprovalView(run inputResearchRun, state string) inputResearchApprovalView {
	a := run.Approval
	return inputResearchApprovalView{ChatID: a.ChatID, TurnID: a.TurnID, Reviewer: a.Reviewer, RunID: run.ID, PreparationID: run.PreparationID, Name: a.Name, RecipeVersion: run.RecipeVersion, Limits: run.Recipe.Limits, State: state, ApprovedAt: a.ApprovedAt, ExpiresAt: a.ExpiresAt, UsedAt: a.UsedAt, RevokedAt: a.RevokedAt}
}
func (s *Server) researchApprovals(ctx context.Context, root *os.Root, chat, scope string) ([]inputResearchRun, error) {
	dir, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	runs := []inputResearchRun{}
	for _, entry := range entries {
		if !inputResearchID.MatchString(entry.Name()) {
			continue
		}
		rr, err := openInputResearch(root, entry.Name(), false)
		if err != nil {
			return nil, err
		}
		run, err := s.researchJournal(rr, entry.Name()).Read(ctx)
		rr.Close()
		if err != nil {
			return nil, err
		}
		if run.Approval != nil && run.Approval.ChatID == chat && run.Approval.SessionScope == scope {
			runs = append(runs, run)
		}
	}
	sort.Slice(runs, func(i, j int) bool {
		a, _ := time.Parse(time.RFC3339Nano, runs[i].Approval.ApprovedAt)
		b, _ := time.Parse(time.RFC3339Nano, runs[j].Approval.ApprovedAt)
		return a.After(b)
	})
	return runs, nil
}

// Shared by the page's Start research button, authorizing Auto, and chat's
// admitted start. Each path validates the same frozen recipe and connections.
func (s *Server) validateResearchAdmission(r *http.Request, run inputResearchRun) (string, error) {
	p, err := s.preparationRepository().Read(r.Context())
	if err != nil {
		return "", err
	}
	defer p.Close()
	return s.validateResearchAdmissionView(r, run, p)
}

func (s *Server) validateResearchAdmissionView(r *http.Request, run inputResearchRun, p inputPreparationReader) (string, error) {
	saved, err := p.Get(run.PreparationID)
	if err != nil {
		return "", err
	}
	if saved.SHA256 != run.Revision {
		return "", errors.New("Dialects changed after the research review. Review the current recipe before starting.")
	}
	// Company references can point at another input, whose version is not in the
	// preparation digest. Recheck those snapshots immediately before admission.
	companies, err := selectedResearchCompanies(p, saved.Document, run.Recipe)
	if err != nil {
		return "", err
	}
	if run.ContinuationOf == "" && !run.Reprocess {
		a, _ := json.Marshal(companies)
		b, _ := json.Marshal(run.Companies)
		if len(companies) != len(run.Companies) || len(companies) > 0 && string(a) != string(b) {
			return "", errors.New("Selected companies changed. Review research again.")
		}
	}
	if err = validateResearchSearchForPhase(r.Context(), run, inputResearchOperations{validateSearch: s.validateResearchSearchConnection}); err != nil {
		return "", err
	}
	if err = s.validateResearchConnection(r.Context(), run.Recipe.AI); err != nil {
		return "", err
	}

	if run.Execution != nil {
		execution, e := s.researchExecution(r.Context(), &run.Recipe)
		if e != nil {
			return "", e
		}
		if execution != *run.Execution {
			return "", errors.New("AI execution capabilities changed. Review research again.")
		}
	}
	gateway, err := s.configuredResearch()
	if err != nil {
		return "", err
	}
	if gateway.authority != run.Gateway.Authority || gateway.signerPublic != run.Gateway.Public {
		return "", errors.New("The source Gateway changed. Review research again.")
	}
	if err := r.Context().Err(); err != nil {
		return "", err
	}
	return saved.Document.Name, nil
}

// The research handler holds the common admission lock before calling this.
func (s *Server) handleAuthorizeResearch(w http.ResponseWriter, r *http.Request, root, rr *os.Root, run inputResearchRun) {
	if run.Reprocess {
		writeJSONCoded(w, 409, CodeBadRequest, "Confirm the selected records in the reprocessing review.")
		return
	}
	var request struct {
		ChatID  string `json:"chatId"`
		Minutes int    `json:"minutes"`
	}
	if inputRequest(r, &request) != nil || !researchChatID.MatchString(request.ChatID) || (request.Minutes != 15 && request.Minutes != 60) {
		writeJSONCoded(w, 400, CodeBadRequest, "Choose this chat and an allowance lasting 15 minutes or one hour.")
		return
	}
	fail := func(err error) { writeJSONCoded(w, 409, CodeBadRequest, err.Error()) }
	scope := s.researchApprovalScope(r)
	grants, err := s.researchApprovals(r.Context(), root, request.ChatID, scope)
	if err != nil {
		fail(err)
		return
	}
	for _, grant := range grants {
		if researchApprovalState(grant, time.Now()) == "ready" {
			fail(errors.New("This chat already has an unused research allowance. Switch to Ask me before reviewing another."))
			return
		}
	}
	if err = s.researchInputAvailable(r.Context(), root, run.PreparationID, run.ID); err != nil {
		fail(err)
		return
	}
	_, err = s.researchJournal(rr, run.ID).Update(r.Context(), func(current *inputResearchRun) error {
		if current.State != "review" || current.StopRequested || current.Approval != nil || current.ContinuationOf != "" {
			return errors.New("Review a new saved recipe before allowing automatic research.")
		}
		if r.Header.Get("If-Match") != current.Revision {
			return errors.New("The reviewed inputs changed. Reload the allowance review.")
		}
		name, err := s.validateResearchAdmission(r, *current)
		if err != nil {
			return err
		}
		if err = s.researchBudgetIsLatest(r.Context(), root, *current); err != nil {
			return err
		}
		actor := inputReviewActor{Kind: "local-access"}
		if held, ok := s.sessionOf(r); ok {
			actor = inputReviewActor{Kind: "session", Subject: held.subject, Issuer: held.issuer, Name: held.name}
		}
		now := time.Now().UTC()
		current.Approval = &inputResearchApproval{ChatID: request.ChatID, Name: name, SessionScope: scope, Reviewer: actor, ApprovedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Duration(request.Minutes) * time.Minute).Format(time.RFC3339Nano)}
		return nil
	})
	if err != nil {
		fail(err)
		return
	}
	writeJSON(w, 200, map[string]string{"mode": "auto"})
}

func (s *Server) handleResearchApprovals(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) || s.refuseUnusableStore(w) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	chat := r.PathValue("chat")
	if !researchChatID.MatchString(chat) || r.URL.RawQuery != "" {
		writeJSONCoded(w, 400, CodeBadRequest, "Invalid conversation.")
		return
	}
	fail := func(err error) { writeJSONCoded(w, 409, CodeBadRequest, err.Error()) }
	var revoke struct {
		RunID string `json:"runId"`
	}
	var request struct {
		PreparationID string `json:"preparationId"`
		Revision      string `json:"revision"`
		TurnID        string `json:"turnId"`
	}
	isRevoke := strings.HasSuffix(r.URL.Path, "/revoke")
	if r.Method != "GET" {
		if isRevoke {
			if inputRequest(r, &revoke) != nil || !inputResearchID.MatchString(revoke.RunID) {
				writeJSONCoded(w, 400, CodeBadRequest, "Choose the allowance to revoke.")
				return
			}
		} else if inputRequest(r, &request) != nil || !inputPreparationID.MatchString(request.PreparationID) || len(request.Revision) != 64 || !researchChatID.MatchString(request.TurnID) {
			writeJSONCoded(w, 400, CodeBadRequest, "Request research for a saved input and this conversation turn.")
			return
		}
	}

	if r.Method != "GET" && !isRevoke {
		receipt, err := s.requestResearch(r, inputResearchRequest{ChatID: chat, PreparationID: request.PreparationID, Revision: request.Revision, TurnID: request.TurnID}, nil)
		if err != nil {
			fail(err)
			return
		}
		if receipt.Status == "started" {
			writeJSON(w, 200, map[string]any{"status": receipt.Status, "runId": receipt.RunID, "state": receipt.State})
		} else {
			writeJSON(w, 200, map[string]string{"status": "review-required"})
		}
		return
	}

	root, err := s.inputResearchRoot(false)
	if errors.Is(err, os.ErrNotExist) {
		if r.Method == "GET" {
			writeJSON(w, 200, map[string]string{"mode": "manual"})
			return
		}
		if isRevoke {
			fail(errors.New("This research allowance is unavailable in this session."))
			return
		}
		writeJSON(w, 200, map[string]string{"status": "review-required"})
		return
	}
	if err != nil {
		fail(err)
		return
	}
	defer root.Close()
	if r.Method != "GET" {
		lock, err := root.Open(".")
		if err != nil {
			fail(err)
			return
		}
		defer lock.Close()
		if err = lockPrivateData(lock, true); err != nil {
			fail(errors.New("Another research change is being saved. Try again after it finishes."))
			return
		}
	}
	grants, err := s.researchApprovals(r.Context(), root, chat, s.researchApprovalScope(r))
	if err != nil {
		fail(err)
		return
	}
	if r.Method == "GET" {
		reply := map[string]any{"mode": "manual"}
		if len(grants) > 0 {
			grant := grants[0]
			state := researchApprovalState(grant, time.Now())
			if state == "ready" {
				saved, err := s.preparationRepository().Get(r.Context(), grant.PreparationID)
				if err != nil || saved.SHA256 != grant.Revision {
					state = "stale"
				} else if s.researchBudgetIsLatest(r.Context(), root, grant) != nil {
					state = "stale"
				} else {
					reply["mode"] = "auto"
				}
			}
			reply["grant"] = researchApprovalView(grant, state)
		}
		writeJSON(w, 200, reply)
		return
	}
	if isRevoke {
		for _, grant := range grants {
			if grant.ID == revoke.RunID {
				rr, err := openInputResearch(root, grant.ID, false)
				if err != nil {
					fail(err)
					return
				}
				defer rr.Close()
				_, err = s.researchJournal(rr, grant.ID).Update(r.Context(), func(current *inputResearchRun) error {
					if current.Approval.UsedAt != "" {
						return errors.New("This allowance was already used. Use Stop on the research run if needed.")
					}
					current.Approval.RevokedAt = time.Now().UTC().Format(time.RFC3339Nano)
					return nil
				})
				if err != nil {
					fail(err)
					return
				}
				writeJSON(w, 200, map[string]string{"mode": "manual"})
				return
			}
		}
		fail(errors.New("This research allowance is unavailable in this session."))
		return
	}
}

func researchApprovalEvidence(run inputResearchRun) *inputResearchApprovalView {
	if run.Approval == nil {
		return nil
	}
	view := researchApprovalView(run, researchApprovalState(run, time.Now()))
	return &view
}
