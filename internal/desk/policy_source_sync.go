package desk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Host-control DTOs from Gateway's google-drive descriptor. They are unsigned
// snapshots; snapshot-only never conveys provider approval or withdrawal.
type policyConnectedIdentity struct {
	Provider   string `json:"provider"`
	AccountID  string `json:"accountId"`
	ResourceID string `json:"resourceId"`
}
type policySourceLifecycle struct {
	Kind string `json:"kind"`
}
type policySourceBinding struct {
	Version     int                     `json:"version"`
	OperationID string                  `json:"operationId"`
	BindingID   string                  `json:"bindingId"`
	Source      policyConnectedIdentity `json:"source"`
	CreatedAt   string                  `json:"createdAt"`
	State       string                  `json:"state"`
	Coverage    string                  `json:"coverage"`
	Lifecycle   policySourceLifecycle   `json:"lifecycle"`
}
type policySourceContent struct {
	BytesDigest string `json:"bytesDigest"`
	ByteLength  int    `json:"byteLength"`
	MediaType   string `json:"mediaType"`
}
type policySourceExtraction struct {
	Content             policySourceContent `json:"content"`
	Profile             string              `json:"profile"`
	Processor           string              `json:"processor"`
	ProcessorVersion    string              `json:"processorVersion"`
	ConfigurationDigest string              `json:"configurationDigest"`
}
type policyGatewayObservation struct {
	Version        int                     `json:"version"`
	BindingID      string                  `json:"bindingId"`
	ObservationID  string                  `json:"observationId"`
	Source         policyConnectedIdentity `json:"source"`
	SourceRevision string                  `json:"sourceRevision"`
	ObservedAt     string                  `json:"observedAt"`
	Availability   string                  `json:"availability"`
	Coverage       string                  `json:"coverage"`
	Lifecycle      policySourceLifecycle   `json:"lifecycle"`
	Content        policySourceContent     `json:"content"`
	Extraction     policySourceExtraction  `json:"extraction"`
	Document       json.RawMessage         `json:"document"`
}
type policySourceObservation struct {
	ID             string                  `json:"id"`
	BindingID      string                  `json:"bindingId"`
	Source         policyConnectedIdentity `json:"source"`
	SourceRevision string                  `json:"sourceRevision"`
	ObservedAt     string                  `json:"observedAt"`
	RetainedAt     string                  `json:"retainedAt"`
	Availability   string                  `json:"availability"`
	Coverage       string                  `json:"coverage"`
	Lifecycle      policySourceLifecycle   `json:"lifecycle"`
	Content        policySourceContent     `json:"content"`
	Extraction     policySourceExtraction  `json:"extraction"`
	Original       retainedArtifactRef     `json:"original"`
	Document       retainedArtifactRef     `json:"document"`
	SemanticDigest string                  `json:"semanticDigest"`
	Name           string                  `json:"name"`
}
type policySourceResolution struct {
	ObservationID       string      `json:"observationId"`
	CandidateRevisionID string      `json:"candidateRevisionId"`
	CandidateDigest     string      `json:"candidateDigest"`
	WorkflowDigest      string      `json:"workflowDigest"`
	At                  string      `json:"at"`
	Actor               policyActor `json:"actor"`
	Comment             string      `json:"comment"`
}
type policySourceAction struct {
	Kind  string      `json:"kind"`
	At    string      `json:"at"`
	Actor policyActor `json:"actor"`
}
type policySourceGrant struct {
	Authority        string      `json:"authority"`
	Actor            policyActor `json:"actor"`
	At               string      `json:"at"`
	OperationID      string      `json:"operationId"`
	ResourceID       string      `json:"resourceId"`
	SelectionContext string      `json:"selectionContext"`
}
type policySourcePoll struct {
	ID         string `json:"id"`
	Owner      string `json:"owner"`
	Deadline   string `json:"deadline"`
	Generation int    `json:"generation"`
}
type policyConnectedSource struct {
	Recovery          *policyRecoverySource     `json:"recovery,omitempty"`
	Version           int                       `json:"version"`
	Generation        int                       `json:"generation"`
	State             string                    `json:"state"`
	FrequencyMinutes  int                       `json:"frequencyMinutes"`
	Grant             policySourceGrant         `json:"grant"`
	Binding           *policySourceBinding      `json:"binding,omitempty"`
	CleanupOperations []string                  `json:"cleanupOperations,omitempty"`
	Cleanup           []string                  `json:"cleanup"`
	Poll              *policySourcePoll         `json:"poll,omitempty"`
	LastAttempt       string                    `json:"lastAttempt,omitempty"`
	LastSuccess       string                    `json:"lastSuccess,omitempty"`
	NextCheck         string                    `json:"nextCheck,omitempty"`
	ErrorCode         string                    `json:"errorCode,omitempty"`
	Gap               bool                      `json:"gap"`
	Observations      []policySourceObservation `json:"observations"`
	Resolutions       []policySourceResolution  `json:"resolutions"`
	Actions           []policySourceAction      `json:"actions"`
}
type policySourceSyncReply struct {
	Recovery  *policyRecoveryView    `json:"recovery,omitempty"`
	Revision  string                 `json:"revision"`
	Supported bool                   `json:"supported"`
	Pending   bool                   `json:"pending"`
	State     *policyConnectedSource `json:"state,omitempty"`
}
type policySourceSyncRequest struct {
	OperationID        string `json:"operationId"`
	BaseRevision       string `json:"baseRevision"`
	BaseSourceRevision string `json:"baseSourceRevision"`
	Action             string `json:"action"`
	ResourceID         string `json:"resourceId,omitempty"`
	SelectionContext   string `json:"selectionContext,omitempty"`
	Consent            string `json:"consent,omitempty"`
	FrequencyMinutes   int    `json:"frequencyMinutes,omitempty"`
	ObservationID      string `json:"observationId,omitempty"`
	CandidateDigest    string `json:"candidateDigest,omitempty"`
	WorkflowDigest     string `json:"workflowDigest,omitempty"`
	Comment            string `json:"comment,omitempty"`
}

func policySourceRevision(source *policyConnectedSource) string {
	if source == nil {
		return digestOf([]byte("null"))
	}
	copy := *source
	copy.Poll, copy.LastAttempt, copy.LastSuccess, copy.NextCheck, copy.ErrorCode, copy.Gap = nil, "", "", "", "", false
	raw, _ := json.Marshal(copy)
	return digestOf(raw)
}
func policySourcePending(record policyStoredRecord, source *policyConnectedSource) bool {
	if source == nil {
		return false
	}
	if policyRecoverySourceHeld(source) && len(source.Observations) == 0 {
		return false
	}
	if len(source.Observations) == 0 {
		return source.State != "revoked" && source.State != "authorization-ended"
	}
	latest, head := source.Observations[len(source.Observations)-1], policyHead(record)
	if !policyRecoverySourceHeld(source) && (source.Binding == nil || latest.BindingID != source.Binding.BindingID || source.Binding.OperationID != source.Grant.OperationID) && source.State != "revoked" && source.State != "authorization-ended" {
		return true
	}
	for i := len(source.Resolutions) - 1; i >= 0; i-- {
		r := source.Resolutions[i]
		if r.ObservationID == latest.ID && r.CandidateRevisionID == head.ID && r.CandidateDigest == head.Digest && r.WorkflowDigest == head.WorkflowDigest {
			return false
		}
	}
	return true
}
func policySyncReply(record policyStoredRecord, source *policyConnectedSource, supported bool) policySourceSyncReply {
	view := source
	if policyRecoverySourceHeld(source) {
		copy := *source
		copy.State, copy.ErrorCode, copy.Gap, copy.Poll, copy.NextCheck = "authorization-ended", "restored-source-consent-required", true, nil, ""
		view = &copy
	}
	return policySourceSyncReply{Recovery: policyRecoveryProjection(record, nil), Revision: policySourceRevision(source), Supported: supported, Pending: policySourcePending(record, source), State: view}
}
func policySourceFrequency(n int) bool { return n == 5 || n == 15 || n == 60 }
func appendPolicySourceCleanup(ids []string, id string) []string {
	if !contains(ids, id) {
		return append(ids, id)
	}
	return ids
}
func policySourceMutationKind(action string) string { return "source-" + action }
func policySourceKinds() []string {
	return []string{"source-bind", "source-pause", "source-resume", "source-revoke", "source-check-now", "source-keep-current"}
}

func validatePolicySources(ledger policyLedger) error {
	bad := errors.New("policy source storage integrity failed")
	if len(ledger.Sources) > policyCountLimit {
		return bad
	}
	for id, source := range ledger.Sources {
		if ledger.Policies[id].ID == "" || source == nil || source.Version != 1 || source.Generation < 1 || !contains([]string{"binding", "active", "paused", "revoked", "authorization-ended"}, source.State) || !policySourceFrequency(source.FrequencyMinutes) || !retainedArtifactDigest.MatchString(source.Grant.Authority) || !retainedArtifactDigest.MatchString(source.Grant.OperationID) || source.Grant.ResourceID == "" || len(source.Grant.ResourceID) > 4096 || !retainedArtifactDigest.MatchString(source.Grant.SelectionContext) || !validPolicyActor(source.Grant.Actor) || !validPolicyTime(source.Grant.At) || source.Observations == nil || source.Resolutions == nil || source.Actions == nil || source.Cleanup == nil || len(source.Observations) > policyHistoryLimit || len(source.Resolutions) > policyHistoryLimit || len(source.Actions) > policyHistoryLimit || len(source.Cleanup) > policyHistoryLimit || len(source.CleanupOperations) > policyHistoryLimit {
			return bad
		}
		for _, stamp := range []string{source.LastAttempt, source.LastSuccess, source.NextCheck} {
			if stamp != "" && !validPolicyTime(stamp) {
				return bad
			}
		}
		if source.Binding != nil && !validPolicySourceBinding(*source.Binding) {
			return bad
		}
		if source.Binding != nil && (source.Binding.OperationID != source.Grant.OperationID || source.Binding.Source.ResourceID != source.Grant.ResourceID) {
			return bad
		}
		if source.State == "active" && (source.Binding == nil || source.Binding.State != "active") {
			return bad
		}
		for _, binding := range append(append([]string{}, source.Cleanup...), source.CleanupOperations...) {
			if !retainedArtifactDigest.MatchString(binding) {
				return bad
			}
		}
		if source.Poll != nil && (!retainedArtifactDigest.MatchString(source.Poll.ID) || !retainedArtifactDigest.MatchString(source.Poll.Owner) || !validPolicyTime(source.Poll.Deadline) || source.Poll.Generation != source.Generation) {
			return bad
		}
		observations := map[string]bool{}
		for _, item := range source.Observations {
			if !retainedArtifactDigest.MatchString(item.ID) || observations[item.ID] || !retainedArtifactDigest.MatchString(item.BindingID) || !validPolicyConnectedIdentity(item.Source) || item.SourceRevision == "" || len(item.SourceRevision) > 4096 || !validPolicyTime(item.ObservedAt) || !validPolicyTime(item.RetainedAt) || item.Availability != "present" || item.Coverage != "snapshot-only" || item.Lifecycle.Kind != "not-exposed" || !item.Original.valid() || item.Original.Bytes > 4<<20 || !item.Document.valid() || item.Document.Bytes > 16<<20 || item.Content.BytesDigest != "sha256:"+item.Original.SHA256 || item.Content.ByteLength != item.Original.Bytes || item.Extraction.Content.BytesDigest != "sha256:"+item.Document.SHA256 || item.Extraction.Content.ByteLength != item.Document.Bytes || item.Extraction.Content.MediaType != "application/vnd.jpack.attachment+json" || item.Extraction.Profile != "attachment-record-v1/canon" || !retainedArtifactDigest.MatchString(item.SemanticDigest) || !validPolicyPrefixedDigest(item.Extraction.ConfigurationDigest) || item.Name == "" || len(item.Name) > 255 {
				return bad
			}
			observations[item.ID] = true
		}
		for _, item := range source.Resolutions {
			if !observations[item.ObservationID] || !validPolicyActor(item.Actor) || !validPolicyTime(item.At) || len(item.Comment) > 8192 {
				return bad
			}
			found := false
			for _, rev := range ledger.Policies[id].Revisions {
				if item.CandidateRevisionID == rev.ID && item.CandidateDigest == rev.Digest && item.WorkflowDigest == rev.WorkflowDigest {
					found = true
				}
			}
			if !found {
				return bad
			}
		}
		for _, item := range source.Actions {
			if !contains(policySourceKinds(), policySourceMutationKind(item.Kind)) || !validPolicyTime(item.At) || !validPolicyActor(item.Actor) {
				return bad
			}
		}
	}
	return nil
}
func validPolicyConnectedIdentity(s policyConnectedIdentity) bool {
	return s.Provider == "google-drive" && s.AccountID != "" && len(s.AccountID) <= 4096 && s.ResourceID != "" && len(s.ResourceID) <= 4096
}
func validPolicyPrefixedDigest(s string) bool {
	return len(s) == 71 && s[:7] == "sha256:" && retainedArtifactDigest.MatchString(s[7:])
}
func validPolicySourceBinding(b policySourceBinding) bool {
	return b.Version == 1 && retainedArtifactDigest.MatchString(b.OperationID) && retainedArtifactDigest.MatchString(b.BindingID) && validPolicyConnectedIdentity(b.Source) && validPolicyTime(b.CreatedAt) && contains([]string{"active", "revoked"}, b.State) && b.Coverage == "snapshot-only" && b.Lifecycle.Kind == "not-exposed"
}

func (s policyService) sourceSync(ctx context.Context, id string, supported bool) (policySourceSyncReply, error) {
	var result policySourceSyncReply
	err := s.repository.View(ctx, func(ledger policyLedger) error {
		if err := s.authorized(ctx); err != nil {
			return err
		}
		record, ok := ledger.Policies[id]
		if !ok {
			return policyRefusal(404, CodeNotFound, "Policy not found.")
		}
		result = policySyncReply(record, ledger.Sources[id], supported)
		result.Recovery = policyRecoveryProjection(record, ledger.SourceAuthorities[id])
		return nil
	})
	return result, err
}

func (s policyService) sourceCommand(ctx context.Context, id string, actor policyActor, q policySourceSyncRequest, authority string, supported bool) (policyMutationReply, error) {
	if !policyOperationIDForm.MatchString(q.OperationID) || !contains(policySourceKinds(), policySourceMutationKind(q.Action)) {
		return policyMutationReply{}, policyRefusal(400, CodeBadRequest, "Invalid source synchronization action.")
	}
	raw, _ := json.Marshal(struct {
		Policy string
		Actor  policyActor
		Body   policySourceSyncRequest
	}{id, actor, q})
	command := policyCommand{Kind: policySourceMutationKind(q.Action), PolicyID: id, OperationID: q.OperationID, RequestDigest: digestOf(raw), Actor: actor}
	var result policyMutationReply
	err := s.repository.Update(ctx, func(ledger *policyLedger) (bool, error) {
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
		source := ledger.Sources[id]
		if policyEffectiveRevision(record, ledger.SourceAuthorities[id]) != q.BaseRevision || policySourceRevision(source) != q.BaseSourceRevision {
			return false, policyRefusal(409, "source-conflict", "The policy or source changed. Review the latest source state before continuing.")
		}
		if len(ledger.Operations) >= policyOperationLimit || source != nil && len(source.Actions) >= policyHistoryLimit {
			return false, policyRefusal(409, "storage-capacity", "Policy source history has reached its supported capacity.")
		}
		held := policyRecoverySourceHeld(source)
		if held && q.Action != "bind" && q.Action != "keep-current" {
			return false, policyRefusal(409, "restored-source-consent-required", "Choose this source again with current administrator consent before provider actions. Retained observations remain historical.")
		}
		now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
		if q.Action == "bind" {
			if !supported {
				return false, policyRefusal(409, "source-unsupported", "The installed Gateway does not support automatic policy source checks.")
			}
			if !retainedArtifactDigest.MatchString(authority) || q.Consent != "recurring-read-after-sign-out" || !retainedArtifactDigest.MatchString(q.SelectionContext) || q.ResourceID == "" || len(q.ResourceID) > 4096 || !policySourceFrequency(q.FrequencyMinutes) {
				return false, policyRefusal(400, CodeBadRequest, "Review recurring source access and choose a supported check frequency.")
			}
			if source != nil && !held && source.State != "revoked" && source.State != "authorization-ended" {
				return false, policyRefusal(409, "source-conflict", "Revoke the current source before connecting another.")
			}
			if source == nil {
				source = &policyConnectedSource{Version: 1, Cleanup: []string{}, Observations: []policySourceObservation{}, Resolutions: []policySourceResolution{}, Actions: []policySourceAction{}}
			}
			if held {
				source.CleanupOperations = nil
				source.Cleanup = []string{}
				source.Poll = nil
			}
			if !held && source.Grant.OperationID != "" {
				source.CleanupOperations = appendPolicySourceCleanup(source.CleanupOperations, source.Grant.OperationID)
			}
			if !held && source.Binding != nil {
				source.Cleanup = appendPolicySourceCleanup(source.Cleanup, source.Binding.BindingID)
			}
			source.Grant = policySourceGrant{Authority: authority, Actor: actor, At: now, OperationID: digestOf([]byte(id + "\x00" + q.OperationID)), ResourceID: q.ResourceID, SelectionContext: q.SelectionContext}
			if held {
				source.Recovery.ConsentOperationID = source.Grant.OperationID
				record.Recovery.SourceConsentRequired = false
				ledger.Policies[id] = record
			}
			source.State, source.FrequencyMinutes, source.Binding, source.LastSuccess = "binding", q.FrequencyMinutes, nil, ""
		} else {
			if source == nil {
				return false, policyRefusal(409, "source-conflict", "Connect a policy source first.")
			}
			switch q.Action {
			case "pause":
				if source.State != "active" && source.State != "binding" {
					return false, policyRefusal(409, "source-conflict", "This source is not running.")
				}
				source.State = "paused"
			case "resume":
				if source.State != "paused" || source.Grant.Authority != authority || !supported {
					return false, policyRefusal(409, "source-authorization-ended", "Reconnect this source with current administrator consent.")
				}
				source.State = "active"
				if source.Binding == nil {
					source.State = "binding"
				}
			case "revoke":
				source.State = "revoked"
				source.CleanupOperations = appendPolicySourceCleanup(source.CleanupOperations, source.Grant.OperationID)
				if source.Binding != nil {
					source.Cleanup = appendPolicySourceCleanup(source.Cleanup, source.Binding.BindingID)
				}
			case "check-now":
				if source.State != "active" && source.State != "binding" {
					return false, policyRefusal(409, "source-conflict", "Resume this source before checking it.")
				}
			case "keep-current":
				if len(source.Observations) == 0 || (!held && (source.Binding == nil || source.Observations[len(source.Observations)-1].BindingID != source.Binding.BindingID) && source.State != "revoked" && source.State != "authorization-ended") || source.Observations[len(source.Observations)-1].ID != q.ObservationID || len(source.Resolutions) >= policyHistoryLimit || len(q.Comment) > 8192 {
					return false, policyRefusal(409, "source-conflict", "Review the latest retained source update before keeping the current candidate.")
				}
				head := policyHead(record)
				if head.Digest != q.CandidateDigest || head.WorkflowDigest != q.WorkflowDigest {
					return false, policyRefusal(409, "source-conflict", "Review the current candidate and workflow.")
				}
				source.Resolutions = append(source.Resolutions, policySourceResolution{ObservationID: q.ObservationID, CandidateRevisionID: head.ID, CandidateDigest: head.Digest, WorkflowDigest: head.WorkflowDigest, At: now, Actor: actor, Comment: q.Comment})
			}
		}
		source.Generation++
		source.Poll = nil
		source.NextCheck = now
		source.ErrorCode = ""
		source.Actions = append(source.Actions, policySourceAction{Kind: q.Action, At: now, Actor: actor})
		if ledger.Sources == nil {
			ledger.Sources = map[string]*policyConnectedSource{}
		}
		ledger.Sources[id] = source
		reply, err := s.reply(ctx, record, ledger.SourceAuthorities[id])
		if err != nil {
			return false, err
		}
		sync := policySyncReply(record, source, supported)
		sync.Recovery = policyRecoveryProjection(record, ledger.SourceAuthorities[id])
		result = policyMutationReply{Delivery: "committed", Policy: reply, SourceSync: &sync, Receipt: policyOperationReceipt{OperationID: q.OperationID, Kind: command.Kind, PolicyID: id, RequestDigest: command.RequestDigest, At: now, Actor: actor, Revision: reply.Revision, CandidateDigest: reply.Document.Candidate.Digest}}
		raw, _ := json.Marshal(result)
		ref, err := s.artifacts.Put(ctx, raw)
		if err != nil {
			return false, err
		}
		ledger.Operations[q.OperationID] = policyStoredOperation{OperationID: q.OperationID, Kind: command.Kind, PolicyID: id, RequestDigest: command.RequestDigest, Result: ref}
		return true, s.authorized(ctx)
	})
	if err != nil {
		return policyMutationReply{}, err
	}
	return result, nil
}

func policySourceFailure(code string) error { return fmt.Errorf("policy source: %s", code) }
