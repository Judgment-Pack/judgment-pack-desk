package desk

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"time"
)

var sourceReviewsFileName = regexp.MustCompile(`^source-reviews-[a-f0-9]{64}\.json$`)
var refreshID = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
var refreshDigest = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

func validateSourceReviews(data []byte) error {
	bad := errors.New("source reviews must contain valid snapshot references, version 1, and at most 2048 reviews")
	if !validUTF8(data) || !json.Valid(data) {
		return bad
	}
	type reference struct {
		ID           string `json:"id"`
		Digest       string `json:"digest"`
		Pages        []int  `json:"pages"`
		AllowPartial *bool  `json:"allowPartial"`
	}
	var doc struct {
		Version int `json:"version"`
		Reviews []struct {
			ID         string    `json:"id"`
			Name       string    `json:"name"`
			Before     reference `json:"before"`
			After      reference `json:"after"`
			CheckedAt  string    `json:"checkedAt"`
			ReviewedAt string    `json:"reviewedAt"`
		} `json:"reviews"`
	}
	if json.Unmarshal(data, &doc) != nil || doc.Version != 1 || doc.Reviews == nil || len(doc.Reviews) > 2048 {
		return bad
	}
	ids := map[string]bool{}
	for _, r := range doc.Reviews {
		if !refreshID.MatchString(r.ID) || ids[r.ID] || len(r.Name) == 0 || len(r.Name) > 2048 || r.Before.ID == r.After.ID {
			return bad
		}
		ids[r.ID] = true
		if _, err := time.Parse(time.RFC3339Nano, r.CheckedAt); err != nil {
			return bad
		}
		if r.ReviewedAt != "" {
			if _, err := time.Parse(time.RFC3339Nano, r.ReviewedAt); err != nil {
				return bad
			}
		}
		for _, ref := range []reference{r.Before, r.After} {
			if !refreshID.MatchString(ref.ID) || !refreshDigest.MatchString(ref.Digest) || ref.AllowPartial == nil || ref.Pages == nil || len(ref.Pages) > 500 {
				return bad
			}
			pages := map[int]bool{}
			for _, p := range ref.Pages {
				if p < 1 || pages[p] {
					return bad
				}
				pages[p] = true
			}
		}
	}
	return nil
}
func (s *Server) handleSourceReviews(w http.ResponseWriter, r *http.Request) {
	s.handleWorkspaceRecord(w, r, false)
}
