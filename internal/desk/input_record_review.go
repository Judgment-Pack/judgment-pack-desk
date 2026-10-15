package desk

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

// These are local human review decisions, not signed acquisition evidence or
// pack decisions. Accepting a record never changes its generated classification.
type inputRecordReview struct {
	RecordID      string           `json:"recordId"`
	RecordDigest  string           `json:"recordDigest"`
	RecipeVersion int              `json:"recipeVersion"`
	Decision      string           `json:"decision"`
	Note          string           `json:"note"`
	At            string           `json:"at"`
	Reviewer      inputReviewActor `json:"reviewer"`
}
type inputReviewActor struct {
	Kind    string  `json:"kind"`
	Subject string  `json:"subject,omitempty"`
	Issuer  *string `json:"issuer,omitempty"`
	Name    string  `json:"name,omitempty"`
}
type inputRecordReviewEvent struct {
	Review inputRecordReview   `json:"review"`
	Record preparedInputRecord `json:"record"`
}

func inputRecordDigest(record preparedInputRecord) string {
	record.Review = nil
	raw, _ := json.Marshal(record)
	return digestOf(raw)
}

// Builds a review on the caller's private snapshot. The caller must commit it
// with CompareAndSwap against the revision checked by If-Match.
func (s *Server) reviewInputRecord(r *http.Request, doc *inputPreparation, now string) (bool, error) {
	var request struct {
		RecordID  *string   `json:"recordId"`
		RecordIDs *[]string `json:"recordIds"`
		Decision  string    `json:"decision"`
		Note      string    `json:"note"`
	}
	if inputRequest(r, &request) != nil || utf8.RuneCountInString(request.Note) > 2000 {
		return false, errors.New("Choose a review decision and a note of at most 2,000 characters.")
	}
	switch request.Decision {
	case "accepted", "needs-information", "rejected":
	default:
		return false, errors.New("Choose Ready for evaluation, Needs more information or Exclude.")
	}
	// Both the single-record and bulk dialogs commit one revision-checked
	// snapshot through the repository. Resolve the entire selection before
	// changing any record; there is no partial bulk approval.
	if (request.RecordID == nil) == (request.RecordIDs == nil) {
		return false, errors.New("Choose one record or a selection of records to review.")
	}
	var ids []string
	if request.RecordID != nil {
		ids = []string{*request.RecordID}
	} else {
		ids = *request.RecordIDs
	}
	if len(ids) == 0 || len(ids) > inputRecordLimit {
		return false, errors.New("Choose between 1 and 1,000 records to review.")
	}
	byID := make(map[string]int, len(doc.Records))
	for i, record := range doc.Records {
		byID[record.ID] = i
	}
	indices := make([]int, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			return false, errors.New("Choose each research record only once.")
		}
		seen[id] = true
		index, ok := byID[id]
		if !ok {
			return false, errors.New("The selected research record is no longer available. Reload the inputs.")
		}
		record := doc.Records[index]
		if record.Class != "generated" || record.Research == nil {
			return false, errors.New("Choose AI-researched records to review. No records were changed.")
		}
		indices = append(indices, index)
	}
	note := strings.TrimSpace(request.Note)
	actor := inputReviewActor{Kind: "local-access"}
	if held, ok := s.sessionOf(r); ok {
		actor = inputReviewActor{Kind: "session", Subject: held.subject, Issuer: held.issuer, Name: held.name}
	}
	actorJSON, _ := json.Marshal(actor)
	changed := false
	for _, index := range indices {
		record := &doc.Records[index]
		digest := inputRecordDigest(*record)
		// Re-saving the same choice by the same identity does not duplicate history.
		if prior := record.Review; prior != nil && prior.RecordDigest == digest && prior.RecipeVersion == doc.RecipeVersion && prior.Decision == request.Decision && prior.Note == note {
			priorActor, _ := json.Marshal(prior.Reviewer)
			if string(actorJSON) == string(priorActor) {
				continue
			}
		}
		snapshot := *record
		snapshot.Review = nil
		review := inputRecordReview{RecordID: record.ID, RecordDigest: digest, RecipeVersion: doc.RecipeVersion, Decision: request.Decision, Note: note, At: now, Reviewer: actor}
		doc.Reviews = append(doc.Reviews, inputRecordReviewEvent{Review: review, Record: snapshot})
		record.Review = &review
		changed = true
	}
	return changed, nil
}

func validateInputRecordReviews(doc inputPreparation) error {
	valid := func(review inputRecordReview, record preparedInputRecord) bool {
		_, err := time.Parse(time.RFC3339Nano, review.At)
		return err == nil && review.RecordID == record.ID && review.RecordDigest == inputRecordDigest(record) && review.RecipeVersion > 0 && review.RecipeVersion <= doc.RecipeVersion && (review.Decision == "accepted" || review.Decision == "needs-information" || review.Decision == "rejected")
	}
	for _, event := range doc.Reviews {
		if event.Record.Review != nil || !valid(event.Review, event.Record) {
			return errors.New("invalid retained input review")
		}
	}
	for _, record := range doc.Records {
		if record.Review != nil && !valid(*record.Review, record) {
			return errors.New("invalid current input review")
		}
	}
	return nil
}
