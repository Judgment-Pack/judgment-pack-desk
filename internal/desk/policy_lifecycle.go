package desk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Separate optional private-v2 stream: never rewrite candidates, publications,
// old event sequence numbers or retained operation outcomes to add lifecycle.
type policyLifecycleStream struct {
	Version int                    `json:"version"`
	Events  []policyLifecycleEvent `json:"events"`
}
type policyLifecycleEvent struct {
	ID                      string      `json:"id"`
	Sequence                int         `json:"sequence"`
	Action                  string      `json:"action"`
	PublicationID           string      `json:"publicationId"`
	CandidateDigest         string      `json:"candidateDigest"`
	At                      string      `json:"at"`
	Actor                   policyActor `json:"actor"`
	Provenance              string      `json:"provenance"`
	EffectiveAt             string      `json:"effectiveAt,omitempty"`
	ExpiresAt               string      `json:"expiresAt,omitempty"`
	SupersedesPublicationID string      `json:"supersedesPublicationId,omitempty"`
	Rationale               string      `json:"rationale,omitempty"`
}
type policyReleaseLifecycle struct {
	ActivationAllowed *bool  `json:"activationAllowed,omitempty"`
	PublicationID     string `json:"publicationId"`
	Status            string `json:"status"`
	EffectiveAt       string `json:"effectiveAt,omitempty"`
	ExpiresAt         string `json:"expiresAt,omitempty"`
	SupersededBy      string `json:"supersededBy,omitempty"`
	ActivationID      string `json:"activationId,omitempty"`
	WithdrawalID      string `json:"withdrawalId,omitempty"`
}
type policyLifecycleReply struct {
	Recovery       *policyRecoveryView      `json:"recovery,omitempty"`
	Revision       string                   `json:"revision"`
	PolicyRevision string                   `json:"policyRevision"`
	ObservedAt     string                   `json:"observedAt"`
	Releases       []policyReleaseLifecycle `json:"releases"`
	Events         []policyLifecycleEvent   `json:"events"`
}
type policyLifecycleRequest struct {
	OperationID             string `json:"operationId"`
	Action                  string `json:"action"`
	BaseRevision            string `json:"baseRevision"`
	BaseLifecycleRevision   string `json:"baseLifecycleRevision"`
	PublicationID           string `json:"publicationId"`
	EffectiveAt             string `json:"effectiveAt,omitempty"`
	ExpiresAt               string `json:"expiresAt,omitempty"`
	SupersedesPublicationID string `json:"supersedesPublicationId,omitempty"`
	Rationale               string `json:"rationale,omitempty"`
}

func (q *policyLifecycleRequest) UnmarshalJSON(raw []byte) error {
	// Keep omission (server-selected now) distinct from a malformed explicit date.
	// An alias retains the closed decoder without recursively invoking this method.
	type fields policyLifecycleRequest
	var value fields
	if err := decodeDataJSON(raw, &value); err != nil {
		return err
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(raw, &members); err != nil {
		return err
	}
	for key, member := range members {
		if !contains([]string{"operationId", "action", "baseRevision", "baseLifecycleRevision", "publicationId", "effectiveAt", "expiresAt", "supersedesPublicationId", "rationale"}, key) || string(member) == "null" {
			return errors.New("unsupported lifecycle field")
		}
	}
	for _, name := range []string{"effectiveAt", "expiresAt"} {
		if _, present := members[name]; present {
			var value string
			if json.Unmarshal(members[name], &value) != nil || !validPolicyLifecycleTime(value) {
				return errors.New("invalid explicit lifecycle time")
			}
		}
	}
	*q = policyLifecycleRequest(value)
	return nil
}

var policyLifecycleIDForm = regexp.MustCompile(`^lifecycle_[a-f0-9]{48}$`)

func policyLifecycleKinds() []string { return []string{"lifecycle-activate", "lifecycle-withdraw"} }
func policyLifecycleRevision(stream *policyLifecycleStream) string {
	raw, _ := json.Marshal(stream)
	return digestOf(raw)
}
func (s policyService) lifecycleNow() string {
	now := time.Now()
	if s.clock != nil {
		now = s.clock()
	}
	return now.UTC().Format("2006-01-02T15:04:05Z")
}
func validPolicyLifecycleTime(value string) bool {
	if len(value) != 20 {
		return false
	}
	parsed, err := time.Parse("2006-01-02T15:04:05Z", value)
	return err == nil && parsed.Year() >= 1 && parsed.Format("2006-01-02T15:04:05Z") == value
}
func policyLifecyclePublications(record policyStoredRecord) map[string]policyPublication {
	items := map[string]policyPublication{}
	for _, item := range record.Publications {
		items[item.ID] = item
	}
	return items
}
func validatePolicyLifecycles(ledger policyLedger) error {
	bad := errors.New("policy lifecycle integrity failed")
	if len(ledger.Lifecycles) > len(ledger.Policies) {
		return bad
	}
	for id, stream := range ledger.Lifecycles {
		record, ok := ledger.Policies[id]
		if !ok || stream == nil || stream.Version != 1 || stream.Events == nil || len(stream.Events) == 0 || len(stream.Events) > policyHistoryLimit {
			return bad
		}
		publications := policyLifecyclePublications(record)
		seen, activated, withdrawn := map[string]bool{}, map[string]bool{}, map[string]bool{}
		for i, event := range stream.Events {
			publication, ok := publications[event.PublicationID]
			if !ok || !policyLifecycleIDForm.MatchString(event.ID) || seen[event.ID] || event.Sequence != i+1 || event.CandidateDigest != publication.CandidateDigest || !validPolicyLifecycleTime(event.At) || !validPolicyActor(event.Actor) || event.Provenance != "admin-configured" || len(event.Rationale) > 8192 {
				return bad
			}
			seen[event.ID] = true
			switch event.Action {
			case "activate":
				if activated[event.PublicationID] || withdrawn[event.PublicationID] || !validPolicyLifecycleTime(event.EffectiveAt) || event.EffectiveAt < event.At || event.ExpiresAt != "" && (!validPolicyLifecycleTime(event.ExpiresAt) || event.ExpiresAt <= event.EffectiveAt) {
					return bad
				}
				if event.SupersedesPublicationID != "" {
					older, ok := publications[event.SupersedesPublicationID]
					if !ok || older.Sequence >= publication.Sequence {
						return bad
					}
				}
				activated[event.PublicationID] = true
			case "withdraw":
				if withdrawn[event.PublicationID] || strings.TrimSpace(event.Rationale) == "" || event.EffectiveAt != "" || event.ExpiresAt != "" || event.SupersedesPublicationID != "" {
					return bad
				}
				withdrawn[event.PublicationID] = true
			default:
				return bad
			}
		}
	}
	return nil
}
func policyLifecycleView(record policyStoredRecord, stream *policyLifecycleStream, now string, authority *policySourceAuthorityStream) policyLifecycleReply {
	result := policyLifecycleReply{Recovery: policyRecoveryProjection(record, authority), Revision: policyLifecycleRevision(stream), PolicyRevision: policyEffectiveRevision(record, authority), ObservedAt: now, Releases: []policyReleaseLifecycle{}, Events: []policyLifecycleEvent{}}
	activations, withdrawals := map[string]policyLifecycleEvent{}, map[string]policyLifecycleEvent{}
	if stream != nil {
		result.Events = append(result.Events, stream.Events...)
		for _, event := range stream.Events {
			if event.Action == "activate" {
				activations[event.PublicationID] = event
			} else {
				withdrawals[event.PublicationID] = event
			}
		}
	}
	for _, publication := range record.Publications {
		release := policyReleaseLifecycle{PublicationID: publication.ID, Status: "published"}
		owner, boundary := policyAuthorityOwnership(authority)
		boundary = policyRecoveryReviewBoundary(record, boundary)
		_, activated := activations[publication.ID]
		_, withdrawn := withdrawals[publication.ID]
		allowed := owner.Mode == "desk-managed" && publication.Sequence > boundary && !activated && !withdrawn
		release.ActivationAllowed = &allowed
		if event, ok := activations[publication.ID]; ok {
			release.ActivationID, release.EffectiveAt, release.ExpiresAt = event.ID, event.EffectiveAt, event.ExpiresAt
			if now >= event.EffectiveAt {
				release.Status = "effective"
			}
			if event.ExpiresAt != "" && now >= event.ExpiresAt {
				release.Status = "expired"
			}
		}
		// An explicitly superseding activation takes effect at its scheduled time.
		// Withdrawal before that time cancels it; later withdrawal never reactivates
		// the older release. Neither supersession nor this view authorizes execution.
		for _, event := range result.Events {
			withdrawal, withdrawn := withdrawals[event.PublicationID]
			if event.Action == "activate" && event.SupersedesPublicationID == publication.ID && now >= event.EffectiveAt && (!withdrawn || withdrawal.At >= event.EffectiveAt) {
				release.SupersededBy = event.PublicationID
				if release.Status != "expired" {
					release.Status = "superseded"
				}
			}
		}
		if event, ok := withdrawals[publication.ID]; ok {
			release.Status, release.WithdrawalID = "withdrawn", event.ID
		}
		result.Releases = append(result.Releases, release)
	}
	return result
}
func (s policyService) lifecycle(ctx context.Context, id string) (policyLifecycleReply, error) {
	var result policyLifecycleReply
	err := s.repository.View(ctx, func(ledger policyLedger) error {
		if err := s.authorized(ctx); err != nil {
			return err
		}
		record, ok := ledger.Policies[id]
		if !ok {
			return policyRefusal(404, CodeNotFound, "Policy not found.")
		}
		result = policyLifecycleView(record, ledger.Lifecycles[id], s.lifecycleNow(), ledger.SourceAuthorities[id])
		return s.authorized(ctx)
	})
	return result, err
}
func validPolicyMutationIdentity(result policyMutationReply) bool {
	if strings.HasPrefix(result.Receipt.Kind, "adoption-") {
		return result.OutcomeKind == "adoption" && result.PolicyID == result.Receipt.PolicyID && result.PolicyRevision == result.Receipt.Revision && result.Policy.Document.ID == "" && result.Policy.Revision == "" && validPolicyAdoptionResult(result)
	}
	if strings.HasPrefix(result.Receipt.Kind, "coverage-") {
		return result.OutcomeKind == "coverage" && result.PolicyID == result.Receipt.PolicyID && result.PolicyRevision == result.Receipt.Revision && result.Policy.Document.ID == "" && result.Policy.Revision == "" && validPolicyCoverageResult(result)
	}
	if strings.HasPrefix(result.Receipt.Kind, "lifecycle-") || strings.HasPrefix(result.Receipt.Kind, "authority-") {
		return (result.OutcomeKind == "lifecycle" && strings.HasPrefix(result.Receipt.Kind, "lifecycle-") || result.OutcomeKind == "source-authority" && strings.HasPrefix(result.Receipt.Kind, "authority-")) && result.PolicyID == result.Receipt.PolicyID && result.PolicyRevision == result.Receipt.Revision && result.Policy.Document.ID == "" && result.Policy.Revision == ""
	}
	return result.OutcomeKind == "" && result.PolicyID == "" && result.PolicyRevision == "" && result.Policy.Document.ID == result.Receipt.PolicyID && result.Policy.Revision == result.Receipt.Revision
}
func validPolicyLifecycleResult(result policyMutationReply) bool {
	if !strings.HasPrefix(result.Receipt.Kind, "lifecycle-") {
		return result.Lifecycle == nil
	}
	if result.Lifecycle == nil || result.Lifecycle.Revision != result.Receipt.LifecycleRevision || result.Lifecycle.PolicyRevision != result.Receipt.Revision || len(result.Lifecycle.Events) == 0 {
		return false
	}
	event := result.Lifecycle.Events[len(result.Lifecycle.Events)-1]
	return event.ID == result.Receipt.LifecycleEventID && event.PublicationID == result.Receipt.PublicationID && event.CandidateDigest == result.Receipt.CandidateDigest && event.Actor == result.Receipt.Actor && event.At == result.Receipt.At && "lifecycle-"+event.Action == result.Receipt.Kind
}
func (s policyService) lifecycleCommand(ctx context.Context, id string, actor policyActor, q policyLifecycleRequest) (policyMutationReply, error) {
	if !policyOperationIDForm.MatchString(q.OperationID) || !contains(policyLifecycleKinds(), "lifecycle-"+q.Action) {
		return policyMutationReply{}, policyRefusal(400, CodeBadRequest, "Invalid policy lifecycle action.")
	}
	raw, _ := json.Marshal(struct {
		Policy string
		Actor  policyActor
		Body   policyLifecycleRequest
	}{id, actor, q})
	command := policyCommand{Kind: "lifecycle-" + q.Action, PolicyID: id, OperationID: q.OperationID, RequestDigest: digestOf(raw), Actor: actor}
	var result policyMutationReply
	err := s.lifecycleAuthorityUpdate(ctx, id, actor, q, func(ledger *policyLedger, now string) (bool, error) {
		if err := s.authorized(ctx); err != nil {
			return false, err
		}
		recovered, err := s.recovered(ctx, *ledger, command)
		if err != nil {
			return false, err
		}
		if recovered != nil {
			result = *recovered
			return false, nil
		}
		record, ok := ledger.Policies[id]
		if !ok {
			return false, policyRefusal(404, CodeNotFound, "Policy not found.")
		}
		stream := ledger.Lifecycles[id]
		if q.BaseRevision != policyEffectiveRevision(record, ledger.SourceAuthorities[id]) || q.BaseLifecycleRevision != policyLifecycleRevision(stream) {
			return false, policyRefusal(409, "lifecycle-conflict", "The policy or lifecycle changed. Review its current state before continuing.")
		}
		if len(ledger.Operations) >= policyOperationLimit || stream != nil && len(stream.Events) >= policyHistoryLimit {
			return false, policyRefusal(409, "storage-capacity", "Policy lifecycle history has reached its supported capacity.")
		}
		publications := policyLifecyclePublications(record)
		publication, ok := publications[q.PublicationID]
		if !ok {
			return false, policyRefusal(404, CodeNotFound, "Policy publication not found.")
		}
		if stream != nil {
			for _, event := range stream.Events {
				if event.PublicationID == publication.ID && (event.Action == "withdraw" || event.Action == q.Action) {
					return false, policyRefusal(409, "lifecycle-conflict", "This release already has that lifecycle decision or was withdrawn.")
				}
			}
		}
		if now == "" {
			now = s.lifecycleNow()
		}
		event := policyLifecycleEvent{Action: q.Action, PublicationID: publication.ID, CandidateDigest: publication.CandidateDigest, At: now, Actor: actor, Provenance: "admin-configured", Rationale: q.Rationale}
		if len(q.Rationale) > 8192 || !validPolicyLifecycleTime(now) {
			return false, policyRefusal(400, CodeBadRequest, "Invalid policy lifecycle details.")
		}
		if q.Action == "activate" {
			if err := policyAuthorityAdmission(record, ledger.SourceAuthorities[id], "activate"); err != nil {
				return false, err
			}
			_, boundary := policyAuthorityOwnership(ledger.SourceAuthorities[id])
			boundary = policyRecoveryReviewBoundary(record, boundary)
			if publication.Sequence <= boundary {
				if record.Recovery != nil && publication.Sequence <= record.Recovery.PolicySequence {
					return false, policyRefusal(409, "restored-review-required", "Publish a newly reviewed candidate after restoring this snapshot before activating a release. Historical publications remain available.")
				}
				return false, policyRefusal(409, "ownership-review-required", "Publish under current Desk ownership before newly activating this release.")
			}
			event.EffectiveAt = q.EffectiveAt
			if event.EffectiveAt == "" {
				event.EffectiveAt = now
			}
			event.ExpiresAt, event.SupersedesPublicationID = q.ExpiresAt, q.SupersedesPublicationID
			if !validPolicyLifecycleTime(event.EffectiveAt) || event.EffectiveAt < now || event.ExpiresAt != "" && (!validPolicyLifecycleTime(event.ExpiresAt) || event.ExpiresAt <= event.EffectiveAt) {
				return false, policyRefusal(400, "invalid-lifecycle-time", "Use UTC whole-second dates with expiry after effectiveness; activation cannot be backdated.")
			}
			if event.SupersedesPublicationID != "" {
				older, ok := publications[event.SupersedesPublicationID]
				if !ok || older.Sequence >= publication.Sequence {
					return false, policyRefusal(400, CodeBadRequest, "Choose an older publication of this policy to supersede.")
				}
			}
		} else if strings.TrimSpace(q.Rationale) == "" || q.EffectiveAt != "" || q.ExpiresAt != "" || q.SupersedesPublicationID != "" {
			return false, policyRefusal(400, CodeBadRequest, "Withdrawal requires a rationale and cannot set an activation schedule.")
		}
		// Activation verifies the exact target, never an unrelated mutable draft.
		// Withdrawal needs current authority and the validated release ledger;
		// unavailable narrative bytes must not prevent recording that restriction.
		if q.Action == "activate" {
			for _, revision := range record.Revisions {
				if revision.ID == publication.CandidateRevisionID {
					if _, err := s.candidate(ctx, revision); err != nil {
						return false, policyBeforeCommitFailure(err)
					}
					break
				}
			}
		}
		event.ID, err = newPolicyEventID("lifecycle")
		if err != nil {
			return false, err
		}
		if stream == nil {
			stream = &policyLifecycleStream{Version: 1, Events: []policyLifecycleEvent{}}
		}
		event.Sequence = len(stream.Events) + 1
		stream.Events = append(stream.Events, event)
		if ledger.Lifecycles == nil {
			ledger.Lifecycles = map[string]*policyLifecycleStream{}
		}
		ledger.Lifecycles[id] = stream
		lifecycle := policyLifecycleView(record, stream, now, ledger.SourceAuthorities[id])
		result = policyMutationReply{Delivery: "committed", OutcomeKind: "lifecycle", PolicyID: id, PolicyRevision: lifecycle.PolicyRevision, Lifecycle: &lifecycle, Receipt: policyOperationReceipt{OperationID: q.OperationID, Kind: command.Kind, PolicyID: id, RequestDigest: command.RequestDigest, At: now, Actor: actor, Revision: lifecycle.PolicyRevision, CandidateDigest: event.CandidateDigest, PublicationID: publication.ID, LifecycleEventID: event.ID, LifecycleRevision: lifecycle.Revision}}
		raw, _ := json.Marshal(result)
		ref, err := s.artifacts.Put(ctx, raw)
		if err != nil {
			return false, policyBeforeCommitFailure(err)
		}
		ledger.Operations[q.OperationID] = policyStoredOperation{OperationID: q.OperationID, Kind: command.Kind, PolicyID: id, RequestDigest: command.RequestDigest, Result: ref}
		return true, s.authorized(ctx)
	})
	return result, err
}
func (s *Server) handlePolicyLifecycle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.guard(w, r) {
		return
	}
	held, ok := s.sessionOf(r)
	if !ok {
		policyHTTPFailure(w, policyRefusal(401, CodeUnauthorized, "Sign in before accessing policy lifecycle."))
		return
	}
	actor, id := policyActorOf(held), r.PathValue("policy")
	if !validPolicyActor(actor) || !policyIDForm.MatchString(id) || r.URL.RawQuery != "" {
		policyHTTPFailure(w, policyRefusal(400, CodeBadRequest, "Invalid policy lifecycle request."))
		return
	}
	service := s.localPolicyService(r, actor)
	if r.Method == http.MethodGet {
		result, err := service.lifecycle(r.Context(), id)
		if err != nil {
			policyHTTPFailure(w, err)
			return
		}
		if err = service.authorized(r.Context()); err != nil {
			policyHTTPFailure(w, err)
			return
		}
		writeJSON(w, 200, result)
		return
	}
	var q policyLifecycleRequest
	if inputRequest(r, &q) != nil {
		policyHTTPFailure(w, policyRefusal(400, CodeBadRequest, "Provide only supported policy lifecycle fields."))
		return
	}
	result, err := service.lifecycleCommand(r.Context(), id, actor, q)
	if err != nil {
		policyHTTPFailure(w, err)
		return
	}
	if err = service.authorized(r.Context()); err != nil {
		failure := policyRefusal(401, CodeUnauthorized, "Sign in again before recovering this lifecycle operation.")
		failure.Uncertain = true
		policyHTTPFailure(w, failure)
		return
	}
	writeJSON(w, 200, result)
}
