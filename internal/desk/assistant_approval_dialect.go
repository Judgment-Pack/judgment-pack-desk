package desk

import (
	"context"
	"encoding/json"
	"errors"
)

// Stored proposals are candidates, never authority. The retained host review and
// current recipe/ledger must independently agree before the shared dispatcher can
// grant or apply anything. No file path is synthesized for an immutable artifact.
type assistantPublicationAction struct {
	Version     int                           `json:"version"`
	ID          string                        `json:"id"`
	Kind        string                        `json:"kind"`
	InputID     string                        `json:"inputId"`
	Revision    string                        `json:"revision"`
	Applied     json.RawMessage               `json:"applied"`
	Publication *assistantPublicationProposal `json:"publication"`
}
type assistantPublicationProposal struct {
	Variant string                   `json:"variant"`
	Version string                   `json:"version"`
	Impact  artifactImpact           `json:"impact"`
	Answer  dialectPublicationAnswer `json:"answer"`
}

func (a assistantPublicationAction) request() dialectPublicationRequest {
	p := a.Publication
	return dialectPublicationRequest{Action: "publish", Revision: a.Revision, Variant: p.Variant, Version: p.Version, Impact: p.Impact, Reviewed: true, CheckDigest: p.Answer.Digest, BasePublication: &p.Answer.BasePublication, Review: p.Answer.Review}
}
func resolveDialectPublicationApproval(ctx context.Context, s *Server, chat assistantApprovalChat, _ json.RawMessage, target string) (assistantActionResource, error) {
	refuse := func() (assistantActionResource, error) {
		return assistantActionResource{}, errors.New("Check and review the current Dialect publication before saving.")
	}
	var action *assistantPublicationAction
	for i := range chat.InputActions {
		a := &chat.InputActions[i]
		if a.ID == target {
			if action != nil {
				return refuse()
			}
			action = a
		}
	}
	if target == "" || action == nil || action.Version != 1 || action.Kind != "publish-dialect" || action.Publication == nil || len(action.Applied) > 0 && string(action.Applied) != "null" || !inputPreparationID.MatchString(action.InputID) || !retainedArtifactDigest.MatchString(action.Revision) {
		return refuse()
	}
	p := action.Publication
	if p.Answer.Saved || p.Answer.Review == nil || !p.Impact.valid() || p.Impact.Classification == "unknown" || (p.Variant != "asserted" && p.Variant != "generated") {
		return refuse()
	}
	request := action.request()
	retained, err := s.artifactReview(ctx, *p.Answer.Review)
	if err != nil {
		return assistantActionResource{}, err
	}
	review := retained.Review
	path := dialectArtifactID(action.InputID, p.Variant)
	if review.Target != path || review.StorageRevision != action.Revision || review.ProposedVersion != p.Version || review.BasePublication != p.Answer.BasePublication || review.Impact != p.Impact || review.Candidate != artifactReference([]byte(p.Answer.Document)) || p.Answer.Digest != "sha256:"+review.Candidate.SHA256 {
		return refuse()
	}
	var preview dialectPublicationAnswer
	if json.Unmarshal(review.Preview, &preview) != nil {
		return refuse()
	}
	preview.Review = p.Answer.Review
	if !sameDraftValue(preview, p.Answer) || preview.Publication.Dialect == nil || preview.Publication.Dialect.Request != dialectPublicationRequestID(action.InputID, request) {
		return refuse()
	}
	// Nonblocking acquisition avoids reversing the project/private-data lock order.
	// Keep this read view until publication finishes, preventing a recipe TOCTOU.
	view, err := s.preparationRepository().TryRead(ctx)
	if err != nil {
		return assistantActionResource{}, err
	}
	accepted := false
	defer func() {
		if !accepted {
			view.Close()
		}
	}()
	saved, err := view.Get(action.InputID)
	if err != nil {
		return assistantActionResource{}, err
	}
	if saved.SHA256 != action.Revision || saved.Document.Recipe.Qualification == nil {
		return refuse()
	}
	dependencies := []retainedArtifactRef{artifactReference(dialectRecipeBytes(saved.Document)), artifactReference([]byte(saved.Document.Recipe.Qualification.Pack))}
	if !sameDraftValue(dependencies, review.Dependencies) {
		return refuse()
	}
	ledger, err := s.readDialectPublicationLedger()
	if err != nil {
		return assistantActionResource{}, err
	}
	if latestDialectPublication(ledger, action.InputID, p.Variant) != review.BasePublication {
		return refuse()
	}
	for _, ref := range append([]retainedArtifactRef{review.Candidate}, review.Dependencies...) {
		if _, err = s.artifactRepository().Get(ctx, ref); err != nil {
			return assistantActionResource{}, err
		}
	}
	encoded, _ := json.Marshal(request)
	accepted = true
	return assistantActionResource{Path: path, Name: saved.Document.Name, Revision: saved.SHA256, Checks: digestOf(encoded), Close: func() { view.Close() }, Verify: func(content string) error {
		var supplied dialectPublicationRequest
		if decodeDataJSON([]byte(content), &supplied) != nil || !sameDraftValue(request, supplied) {
			return errors.New("The publication proposal changed. Review it again.")
		}
		var state struct {
			Status    string            `json:"status"`
			Unknowns  []json.RawMessage `json:"unknowns"`
			Questions []struct {
				Resolution json.RawMessage `json:"resolution"`
			} `json:"questions"`
		}
		if json.Unmarshal(chat.Checkpoint.State, &state) != nil || (state.Status != "complete" && state.Status != "ready") || len(state.Unknowns) > 0 {
			return errors.New("Finish the response and review open questions before publishing.")
		}
		for _, q := range state.Questions {
			if len(q.Resolution) == 0 || string(q.Resolution) == "null" {
				return errors.New("Answer the open question before publishing.")
			}
		}
		return nil
	}, Commit: func(_ string) (int, any) {
		status, body := s.publishCheckedDialect(ctx, action.InputID, request, view)
		if status != 200 {
			return status, body
		}
		answer, ok := body.(dialectPublicationAnswer)
		if !ok || !answer.Saved || answer.Digest != p.Answer.Digest {
			return 503, codedBody(CodeBadRequest, "The publication could not be verified. Check its saved result before retrying.")
		}
		verified, err := s.publicationAnswer(ctx, answer.Publication)
		if err != nil {
			return 503, codedBody(CodeBadRequest, err.Error())
		}
		return 200, verified
	}}, nil
}
