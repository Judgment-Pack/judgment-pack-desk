package desk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

func portableResearchReply(run inputResearchRun) any {
	return map[string]any{"id": run.ID, "revision": run.Revision, "state": run.State, "createdAt": run.CreatedAt, "choice": run.Portable.Frozen.Choice, "limits": researchApprovedLimits(run), "plan": run.Portable.Frozen.Plan, "inputText": string(run.Portable.Frozen.Input), "responseText": string(run.Portable.Response), "activity": portableResearchActivity(run)}
}
func portableResearchActivity(run inputResearchRun) inputResearchActivity {
	activity := researchActivity(run)
	responses, _ := portableResearchResponses(run)
	activity.SourceCount = len(responses)
	activity.RecordCount = 0
	if len(run.Portable.Response) > 0 {
		activity.RecordCount = 1
	}
	for i, attempt := range run.Attempts {
		if attempt.State != "completed" {
			continue
		}
		if attempt.Kind != "model" {
			activity.Steps[i].Result = "source-saved"
			continue
		}
		var retained portableRetainedResponse
		if json.Unmarshal(attempt.Result, &retained) == nil {
			if action, err := decodePortableResearchAction(retained.Result); err == nil {
				activity.Steps[i].Result = "portable-" + action.Kind
			}
		}
	}
	activity.ApprovedLimits = researchApprovedLimits(run)
	return activity
}
func (s *Server) portableResearchChatWork(chat string) error {
	if chat == "" {
		return nil
	}
	if !researchChatID.MatchString(chat) {
		return errors.New("Invalid research conversation.")
	}
	saved, _, err := s.approvalChat(chat)
	if err != nil {
		return err
	}
	if saved.Interaction == "plan" {
		return errors.New("Switch to Work before starting research or changing its installation bindings.")
	}
	return nil
}
func (s *Server) validatePortableResearchAdmission(ctx context.Context, run inputResearchRun) error {
	frozen := run.Portable.Frozen
	if err := s.portableResearchChatWork(frozen.ChatID); err != nil {
		return err
	}
	checked, err := s.reviewPortableResearch(ctx, frozen.Input, frozen.Choice, frozen.Limits, frozen.ChatID, frozen.SessionScope)
	if err != nil {
		return err
	}
	if checked.Revision != run.Revision || checked.Gateway != run.Gateway {
		return errors.New("The research contract or installation binding changed. Review it again.")
	}
	return nil
}
func (s *Server) handlePortableResearch(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) || s.refuseUnusableStore(w) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	fail := func(err error) { writeJSONCoded(w, 409, CodeBadRequest, err.Error()) }
	if r.URL.RawQuery != "" {
		writeJSONCoded(w, 400, CodeBadRequest, "Invalid portable research request.")
		return
	}
	if strings.HasSuffix(r.URL.Path, "/profiles") {
		var request struct {
			Choice   backgroundModelChoice `json:"choice"`
			ChatID   string                `json:"chatId,omitempty"`
			Reviewed bool                  `json:"reviewed"`
		}
		if inputRequest(r, &request) != nil || !request.Reviewed {
			writeJSONCoded(w, 400, CodeBadRequest, "Review the named model connection before registering its profile.")
			return
		}
		if err := s.portableResearchChatWork(request.ChatID); err != nil {
			fail(err)
			return
		}
		raw, err := s.registerModelSourceProfile(r.Context(), request.Choice)
		if err != nil {
			fail(err)
			return
		}
		writeJSON(w, 200, raw)
		return
	}
	id := r.PathValue("research")
	if id != "" && !inputResearchID.MatchString(id) {
		writeJSONCoded(w, 400, CodeBadRequest, "Invalid research ID.")
		return
	}
	root, err := s.inputResearchRoot(r.Method != "GET")
	if err != nil {
		fail(errors.New("Research storage is unavailable."))
		return
	}
	defer root.Close()
	if id == "" {
		var request struct {
			Input  json.RawMessage       `json:"input"`
			Choice backgroundModelChoice `json:"choice"`
			Limits inputResearchLimits   `json:"limits"`
			ChatID string                `json:"chatId,omitempty"`
		}
		if inputRequest(r, &request) != nil {
			writeJSONCoded(w, 400, CodeBadRequest, "Invalid portable research review.")
			return
		}
		// A review can be prepared in Plan. Confirmation and automatic dispatch must
		// check the persisted phase again; neither review nor import calls a model.
		if request.ChatID != "" {
			if !researchChatID.MatchString(request.ChatID) {
				fail(errors.New("Invalid research conversation."))
				return
			}
			if _, _, err := s.approvalChat(request.ChatID); err != nil {
				fail(err)
				return
			}
		}
		run, err := s.reviewPortableResearch(r.Context(), request.Input, request.Choice, request.Limits, request.ChatID, s.researchApprovalScope(r))
		if err != nil {
			fail(err)
			return
		}
		rr, err := openInputResearch(root, run.ID, true)
		if err != nil {
			fail(err)
			return
		}
		defer rr.Close()
		if err := s.researchJournal(rr, run.ID).Create(r.Context(), run); err != nil {
			fail(err)
			return
		}
		writeJSON(w, 201, portableResearchReply(run))
		return
	}
	rr, err := openInputResearch(root, id, false)
	if err != nil {
		writeJSONCoded(w, 404, CodeNotFound, "Research run not found.")
		return
	}
	defer rr.Close()
	journal := s.researchJournal(rr, id)
	run, err := journal.Read(r.Context())
	if err == nil {
		err = s.researchRunStore(rr, id).validatePortableProofs(r.Context(), run)
	}
	if err != nil || run.Portable == nil {
		writeJSONCoded(w, 404, CodeNotFound, "Research run not found.")
		return
	}
	if r.Method == "GET" {
		writeJSON(w, 200, portableResearchReply(run))
		return
	}
	if strings.HasSuffix(r.URL.Path, "/budget-decision") {
		var request struct {
			RequestID string `json:"requestId"`
			Decision  string `json:"decision"`
		}
		if inputRequest(r, &request) != nil || r.Header.Get("If-Match") != run.Revision {
			fail(errors.New("Review the exact saved request increase."))
			return
		}
		if request.Decision == "approve" {
			if err := s.validatePortableResearchAdmission(r.Context(), run); err != nil {
				fail(err)
				return
			}
		}
		actor := portableResearchReviewer(s, r)
		run, err = journal.Update(r.Context(), func(current *inputResearchRun) error {
			return decideResearchBudget(current, request.RequestID, request.Decision, actor)
		})
		if err != nil {
			fail(err)
			return
		}
		writeJSON(w, 200, portableResearchReply(run))
		return
	}
	var empty struct{}
	if inputRequest(r, &empty) != nil {
		writeJSONCoded(w, 400, CodeBadRequest, "No extra research arguments are accepted.")
		return
	}
	if strings.HasSuffix(r.URL.Path, "/stop") {
		run, err = journal.Update(r.Context(), func(run *inputResearchRun) error {
			if pending := pendingResearchBudget(*run); pending != nil {
				if err := decideResearchBudget(run, pending.ID, "decline", portableResearchReviewer(s, r)); err != nil {
					return err
				}
				run.State = "stopped"
			}
			run.StopRequested = true
			if run.State == "review" || run.State == "queued" {
				run.State = "stopped"
			}
			return nil
		})
	} else if strings.HasSuffix(r.URL.Path, "/confirm") {
		if r.Header.Get("If-Match") != run.Revision || s.researchApprovalScope(r) != run.Portable.Frozen.SessionScope {
			fail(errors.New("Review this research in the current signed-in window before starting it."))
			return
		}
		if err = s.validatePortableResearchAdmission(r.Context(), run); err != nil {
			fail(err)
			return
		}
		run, err = queuePortableResearch(r.Context(), journal, run.Revision)
	} else {
		writeJSONCoded(w, 400, CodeBadRequest, "Unknown research action.")
		return
	}
	if err != nil {
		fail(err)
		return
	}
	writeJSON(w, 200, portableResearchReply(run))
}
func queuePortableResearch(ctx context.Context, journal researchJournal, revision string) (inputResearchRun, error) {
	return journal.Update(ctx, func(run *inputResearchRun) error {
		if run.Portable == nil || run.Revision != revision || run.StopRequested {
			return errors.New("This research review is no longer available.")
		}
		if run.State == "queued" || run.State == "running" || run.State == "completed" {
			return nil
		}
		if run.State != "review" {
			return errors.New("This research has already started. Retained requests cannot be replayed.")
		}
		run.State = "queued"
		return nil
	})
}

// The generic chat action dispatcher owns Ask me/Auto, session, phase, expiry,
// exact revision and grant consumption. This adapter owns only the reviewed run.
func resolvePortableResearchApproval(ctx context.Context, s *Server, chat assistantApprovalChat, _ json.RawMessage, target string) (assistantActionResource, error) {
	var empty assistantActionResource
	if !inputResearchID.MatchString(target) {
		return empty, errors.New("Choose an exact portable research review.")
	}
	root, err := s.inputResearchRoot(false)
	if err != nil {
		return empty, err
	}
	defer root.Close()
	rr, err := openInputResearch(root, target, false)
	if err != nil {
		return empty, err
	}
	closeRoot := func() { rr.Close() }
	journal := s.researchJournal(rr, target)
	run, err := journal.Read(ctx)
	if err != nil || run.Portable == nil || run.Portable.Frozen.ChatID != chat.ID || run.State != "review" || run.StopRequested {
		closeRoot()
		return empty, errors.New("This chat has no matching pending research review.")
	}
	if err = s.validatePortableResearchAdmission(ctx, run); err != nil {
		closeRoot()
		return empty, err
	}
	return assistantActionResource{Path: inputResearchDirectory + "/" + target, Name: run.Portable.Frozen.Plan.Name, Revision: run.Revision, Checks: run.Revision, Close: closeRoot, Verify: func(content string) error {
		var value struct{}
		if decodeDataJSON([]byte(content), &value) != nil {
			return errors.New("No unreviewed research arguments are accepted.")
		}
		return nil
	}, Commit: func(string) (int, any) {
		accepted, err := queuePortableResearch(ctx, journal, run.Revision)
		if err != nil {
			return 409, codedBody(CodeBadRequest, err.Error())
		}
		return 200, portableResearchReply(accepted)
	}}, nil
}

func portableResearchReviewer(s *Server, r *http.Request) inputReviewActor {
	if held, ok := s.sessionOf(r); ok {
		return inputReviewActor{Kind: "session", Subject: held.subject, Issuer: held.issuer, Name: held.name}
	}
	return inputReviewActor{Kind: "local-access"}
}
