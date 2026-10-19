package desk

import (
	"context"
	"encoding/json"
	"strings"
)

// Schema 2 keeps its existing transaction path. Once execution is enabled,
// lifecycle observations share its durable host-time high water: supersession
// cannot be undone by moving the clock back. A denied mutation retains only
// time, never partially appended events or rewritten operation receipts.
func (s policyService) lifecycleAuthorityUpdate(ctx context.Context, id string, actor policyActor, q policyLifecycleRequest, use func(*policyLedger, string) (bool, error)) error {
	var refusal error
	err := s.repository.Update(ctx, func(l *policyLedger) (bool, error) {
		if l.Execution == nil {
			return use(l, "")
		}
		if err := s.authorized(ctx); err != nil {
			return false, err
		}
		if !policyIDForm.MatchString(id) || !validPolicyActor(actor) || len(q.Rationale) > 8192 || q.Action == "withdraw" && (strings.TrimSpace(q.Rationale) == "" || q.EffectiveAt != "" || q.ExpiresAt != "" || q.SupersedesPublicationID != "") || q.Action == "activate" && (q.EffectiveAt != "" && !validPolicyLifecycleTime(q.EffectiveAt) || q.ExpiresAt != "" && !validPolicyLifecycleTime(q.ExpiresAt)) {
			return false, executionFailure("invalid-request")
		}
		now := s.lifecycleNow() // computed while the same authority locks are held
		if !validPolicyLifecycleTime(now) || now < l.Execution.LastTime {
			return false, executionFailure("clock-unavailable")
		}
		advanced := now > l.Execution.LastTime
		before, _ := json.Marshal(l)
		work, err := decodePolicyLedger(before)
		if err != nil {
			return false, executionFailure("authority-unavailable")
		}
		work.authorityRevision = l.authorityRevision
		work.Execution.LastTime = now
		changed, denial := use(&work, now)
		if auth := s.authorized(ctx); auth != nil {
			return false, auth
		}
		if denial != nil {
			l.Execution.LastTime = now
			refusal = denial
			return advanced, nil
		}
		*l = work
		return changed || advanced, nil
	})
	if err != nil {
		return err
	}
	return refusal
}
