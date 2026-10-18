package desk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

type assistantPolicyRequestKey struct{}
type assistantPolicyFields struct {
	Title           string `json:"title"`
	Narrative       string `json:"narrative"`
	ArtifactVersion string `json:"artifactVersion"`
}
type assistantPolicyProposal struct {
	Version     int                   `json:"version"`
	ID          string                `json:"id"`
	PolicyID    string                `json:"policyId"`
	Revision    string                `json:"revision"`
	Before      assistantPolicyFields `json:"before"`
	Candidate   assistantPolicyFields `json:"candidate"`
	OperationID string                `json:"operationId"`
	Check       *struct {
		Scope  string `json:"scope"`
		Status string `json:"status"`
	} `json:"check"`
	Save json.RawMessage `json:"save"`
}

// The dispatcher supplies the actual signed-in request. A restored checkpoint,
// model-provided actor or successful check cannot establish policy authority.
func resolvePolicyApproval(ctx context.Context, s *Server, chat assistantApprovalChat, _ json.RawMessage, target string) (assistantActionResource, error) {
	refuse := func() (assistantActionResource, error) {
		return assistantActionResource{}, errors.New("Review the exact current Policy draft proposal before saving.")
	}
	r, ok := ctx.Value(assistantPolicyRequestKey{}).(*http.Request)
	if !ok || chat.Policy == nil || !policyIDForm.MatchString(chat.Policy.ID) || target == "" || chat.Interaction == "plan" {
		return refuse()
	}
	held, ok := s.sessionOf(r)
	if !ok || !validPolicyActor(policyActorOf(held)) {
		return refuse()
	}
	actor := policyActorOf(held)
	service := s.localPolicyService(r, actor)
	var proposal *assistantPolicyProposal
	for i := range chat.PolicyProposals {
		p := &chat.PolicyProposals[i]
		if p.ID == target {
			if proposal != nil {
				return refuse()
			}
			proposal = p
		}
	}
	if proposal == nil || proposal.Version != 1 || proposal.PolicyID != chat.Policy.ID || !retainedArtifactDigest.MatchString(proposal.Revision) || !policyOperationIDForm.MatchString(proposal.OperationID) || proposal.Check == nil || proposal.Check.Scope != "draft-shape-and-revision" || proposal.Check.Status != "passed" {
		return refuse()
	}
	if len(proposal.Save) > 0 && string(proposal.Save) != "null" {
		var saved struct {
			State string `json:"state"`
		}
		if json.Unmarshal(proposal.Save, &saved) != nil || saved.State != "pending" {
			return refuse()
		}
	}
	p := *proposal
	current, err := service.get(ctx, p.PolicyID)
	if err != nil {
		return assistantActionResource{}, err
	}
	if current.Revision != p.Revision || p.Before != (assistantPolicyFields{Title: current.Document.Candidate.Title, Narrative: current.Document.Candidate.Narrative, ArtifactVersion: current.Document.Candidate.ArtifactVersion}) {
		return refuse()
	}
	request := policyEditRequest{OperationID: p.OperationID, BaseRevision: p.Revision, Title: p.Candidate.Title, Narrative: p.Candidate.Narrative, ArtifactVersion: p.Candidate.ArtifactVersion}
	command := policyCommand{Kind: "save-draft", PolicyID: p.PolicyID, OperationID: p.OperationID, Actor: actor, Edit: &request}
	if err = policyValidateCommand(command, policyStoredRecord{}); err != nil {
		return assistantActionResource{}, err
	}
	raw, _ := json.Marshal(struct {
		Kind, PolicyID string
		Actor          policyActor
		Request        any
	}{command.Kind, command.PolicyID, actor, &request})
	command.RequestDigest = digestOf(raw)
	encoded, _ := json.Marshal(request)
	return assistantActionResource{Path: "policy:" + p.PolicyID, Name: current.Document.Candidate.Title, Revision: p.Revision, Checks: digestOf(encoded), Verify: func(content string) error {
		var supplied policyEditRequest
		if decodeDataJSON([]byte(content), &supplied) != nil || !sameDraftValue(supplied, request) {
			return errors.New("The Policy proposal body changed. Review the exact draft update again.")
		}
		var state struct {
			Status    string            `json:"status"`
			Unknowns  []json.RawMessage `json:"unknowns"`
			Questions []struct {
				Resolution json.RawMessage `json:"resolution"`
			} `json:"questions"`
		}
		if json.Unmarshal(chat.Checkpoint.State, &state) != nil || (state.Status != "complete" && state.Status != "ready") || len(state.Unknowns) > 0 {
			return errors.New("Finish the response and resolve open questions before saving this Policy draft.")
		}
		for _, question := range state.Questions {
			if len(question.Resolution) == 0 || string(question.Resolution) == "null" {
				return errors.New("Resolve open questions before saving this Policy draft.")
			}
		}
		return service.authorized(ctx)
	}, Commit: func(content string) (int, any) {
		// Service CAS, actor checks and durable receipt remain the only writer.
		result, err := service.execute(ctx, command)
		if err == nil {
			err = service.authorized(ctx)
			if err != nil {
				var failure *policyFailure
				if errors.As(err, &failure) {
					failure.Uncertain = true
				}
			}
		}
		if err != nil {
			var failure *policyFailure
			if !errors.As(err, &failure) {
				failure = policyUncertainStorage()
			}
			body := codedBody(failure.Code, failure.Message)
			body["outcome"] = "not-committed"
			if failure.Uncertain {
				body["outcome"] = "unknown"
			}
			return failure.Status, body
		}
		return 200, result
	}}, nil
}

// Policy-linked transcripts contain policy excerpts. Require current Policy
// access before returning private history, including after session changes.
func (s *Server) authorizePolicyConversations(r *http.Request, raw json.RawMessage) error {
	// This deployment has one authenticated administrator. Content availability
	// is independent of access: a missing current source must not erase history.
	// Future resource ACLs extend this shared service authorization boundary.
	return s.authorizePolicyWorkspace(r, raw, false)
}
