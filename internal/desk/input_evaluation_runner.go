package desk

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

var evaluationJobID = regexp.MustCompile(`^job_[a-f0-9]{32}$`)

// Runner creates one job per frozen release, and returns it again only when the
// name and initial trigger configuration match. That is the creation identity;
// the run submission key and event delivery identity have different scopes.
// Keep this call outside journal transactions. Retain its acknowledgement before
// submitting any rows, and never adopt a response for a different reviewed intent.
func (s *Server) ensureEvaluationJob(ctx context.Context, checkpoint *evaluationCheckpoint, b *inputEvaluation) error {
	if checkpoint.failure != nil {
		return checkpoint.failure
	}
	if b.JobID != "" {
		return nil
	}
	request := map[string]any{"name": b.Name, "releaseId": b.ReleaseID, "reviewed": true}
	if b.Subscription != nil {
		request["trigger"] = subscriptionTrigger()
	}
	raw, err := s.inputRunner(ctx, "jobs", request, "")
	if err != nil {
		return err
	}
	var job struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		ReleaseID string `json:"releaseId"`
		TriggerID string `json:"initialTriggerId"`
	}
	if json.Unmarshal(raw, &job) != nil || !evaluationJobID.MatchString(job.ID) || job.ReleaseID != b.ReleaseID || job.Name != strings.TrimSpace(b.Name) {
		return errors.New("The job response does not match the reviewed evaluation.")
	}
	if b.Subscription != nil {
		if !triggerIDPattern.MatchString(job.TriggerID) {
			return errors.New("The subscription job response has no valid trigger.")
		}
		b.Subscription.TriggerID = job.TriggerID
	} else if job.TriggerID != "" {
		return errors.New("The job response has an unexpected trigger.")
	}
	b.JobID = job.ID
	return checkpoint.retain(*b)
}
