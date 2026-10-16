package desk

import (
	"errors"
	"time"
)

func validateSubscriptionUpgradeJournal(b inputEvaluation) error {
	bad := errors.New("Invalid retained subscription upgrade.")
	r := b.SubscriptionUpgrade
	if r == nil {
		if b.Upgrade != nil && (b.Upgrade.Phase != "" || b.Upgrade.Problem != "" || len(b.Upgrade.RetryAt) != 0) {
			return bad
		}
	} else {
		if !inputEvaluationID.MatchString(r.SourceID) || r.SourceID == b.ID || r.SourceDigest == "" || !referenceReleaseID.MatchString(r.SourceReleaseID) || !evaluationJobID.MatchString(r.JobID) || !referenceReleaseID.MatchString(r.BaseReleaseID) || r.BaseRevision < 1 || r.TriggerID == "" || !contains([]string{"new", "changed"}, r.Mode) || !contains([]string{"asserted", "generated"}, r.SourceClass) || r.PolicyDigest == "" || len(r.Baseline) > inputRecordLimit {
			return bad
		}
		if _, err := time.Parse(time.RFC3339Nano, r.ReviewedAt); err != nil {
			return bad
		}
		for id, digest := range r.Baseline {
			if id == "" || digest == "" {
				return bad
			}
		}
		u := b.Upgrade
		if u != nil {
			if len(u.RetryAt) > 128 {
				return bad
			}
			for _, at := range u.RetryAt {
				if _, err := time.Parse(time.RFC3339Nano, at); err != nil {
					return bad
				}
			}
		}
		if u != nil {
			if u.JobID != r.JobID || u.BaseRevision != r.BaseRevision || u.BaseReleaseID != r.BaseReleaseID || !contains([]string{"prepared", "submitted", "refused"}, u.Phase) || (u.Phase == "refused") != (u.Problem != "") || len(u.Result) > 0 && u.Phase != "submitted" {
				return bad
			}
		}
		if b.Subscription != nil || b.ConfirmedAt != "" || b.JobID != "" {
			sub := b.Subscription
			if sub == nil || b.ConfirmedAt == "" || b.JobID != r.JobID || u == nil || len(u.Result) == 0 || !sub.Initialized || sub.IncludeExisting || sub.TriggerID != r.TriggerID || sub.Mode != r.Mode || sub.SourceClass != r.SourceClass || sub.PolicyDigest != r.PolicyDigest || !sameEvaluationValue(sub.Baseline, r.Baseline) {
				return bad
			}
		}
	}
	if len(b.Successors) > 128 {
		return bad
	}
	seen := map[string]bool{}
	for i, next := range b.Successors {
		if !inputEvaluationID.MatchString(next.CandidateID) || next.CandidateID == b.ID || seen[next.CandidateID] || !contains([]string{"pending", "complete", "refused"}, next.State) || i < len(b.Successors)-1 && next.State != "refused" {
			return bad
		}
		seen[next.CandidateID] = true
		if b.Subscription == nil || next.State != "refused" && (b.Subscription.Enabled || b.Subscription.StatePending) {
			return bad
		}
	}
	// A refused attempt releases the source for ordinary pause/resume controls.
	// Only the last active reservation constrains the current delivery state.
	return nil
}
func validSubscriptionUpgradePhase(a, b *inputUpgradeReview) bool {
	if len(b.RetryAt) < len(a.RetryAt) || len(b.RetryAt) > len(a.RetryAt)+1 {
		return false
	}
	for i := range a.RetryAt {
		if a.RetryAt[i] != b.RetryAt[i] {
			return false
		}
	}
	if len(b.RetryAt) > len(a.RetryAt) && (a.Phase != "submitted" || b.Phase != "submitted" || len(a.Result) > 0) {
		return false
	}
	if a.Phase == b.Phase {
		return a.Problem == b.Problem
	}
	if len(a.Result) > 0 {
		return false
	}
	return a.Phase == "prepared" && (b.Phase == "submitted" || b.Phase == "refused") || a.Phase == "submitted" && b.Phase == "refused"
}
func validateSubscriptionSuccessors(before, next inputEvaluation) error {
	bad := errors.New("Retained subscription handoffs cannot be replaced.")
	if len(next.Successors) < len(before.Successors) || len(next.Successors) > len(before.Successors)+1 {
		return bad
	}
	for i, old := range before.Successors {
		n := next.Successors[i]
		if n.CandidateID != old.CandidateID || old.State != n.State && (i != len(before.Successors)-1 || old.State != "pending" || !contains([]string{"complete", "refused"}, n.State)) {
			return bad
		}
	}
	if len(next.Successors) > len(before.Successors) {
		if subscriptionReserved(before) || subscriptionSettled(before) != nil || next.Successors[len(next.Successors)-1].State != "pending" {
			return bad
		}
	}
	if subscriptionReserved(next) {
		if next.Subscription == nil || next.Subscription.Enabled || next.Subscription.StatePending || !sameEvaluationValue(before.Rows, next.Rows) {
			return bad
		}
	}
	return nil
}
