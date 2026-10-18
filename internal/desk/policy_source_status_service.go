package desk

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

func samePolicyStatusValue(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
func validatePolicySourceAuthorities(ledger policyLedger) error {
	bad := errors.New("policy source authority integrity failed")
	if len(ledger.SourceAuthorities) > len(ledger.Policies) {
		return bad
	}
	for id, stream := range ledger.SourceAuthorities {
		record, ok := ledger.Policies[id]
		if !ok || stream == nil || stream.Version != 1 || stream.Events == nil || len(stream.Events) == 0 || len(stream.Events) > policyHistoryLimit {
			return bad
		}
		observations := map[string]policySourceObservation{}
		if source := ledger.Sources[id]; source != nil {
			for _, o := range source.Observations {
				observations[o.ID] = o
			}
		}
		configs := map[string]policySourceStatusConfiguration{}
		ids := map[string]bool{}
		applications := map[string]bool{}
		previousSequence := 0
		mode := "desk-managed"
		for i, e := range stream.Events {
			if !policyAuthorityIDForm.MatchString(e.ID) || !strings.HasPrefix(e.ID, "authority_") || ids[e.ID] || e.Sequence != i+1 || !validPolicyLifecycleTime(e.At) || !validPolicyActor(e.Actor) || strings.TrimSpace(e.Rationale) == "" || len(e.Rationale) > 8192 || e.PolicySequence < previousSequence || e.PolicySequence > policyEventCount(record) {
				return bad
			}
			ids[e.ID] = true
			previousSequence = e.PolicySequence
			fields := 0
			if e.Configuration != nil {
				fields++
			}
			if e.ManagementMode != "" {
				fields++
			}
			if e.ExternalVersion != nil {
				fields++
			}
			if e.ImportedDraft != nil {
				fields++
			}
			if fields != 1 {
				return bad
			}
			switch e.Kind {
			case "configure":
				c := e.Configuration
				if c == nil || !policyAuthorityIDForm.MatchString(c.ID) || !strings.HasPrefix(c.ID, "configuration_") || ids[c.ID] || !retainedArtifactDigest.MatchString(c.BindingID) || !validPolicyConnectedIdentity(c.Source) || c.Capability != "not-exposed" || !contains([]string{"draft", "published"}, c.FallbackStatus) || c.At != e.At || c.Actor != e.Actor {
					return bad
				}
				ids[c.ID] = true
				configs[c.ID] = *c
			case "set-ownership":
				if !contains([]string{"desk-managed", "externally-managed"}, e.ManagementMode) {
					return bad
				}
				mode = e.ManagementMode
			case "apply-external":
				v := e.ExternalVersion
				if v == nil || mode != "externally-managed" || !policyAuthorityIDForm.MatchString(v.ID) || !strings.HasPrefix(v.ID, "external_") || ids[v.ID] || v.EventID != e.ID || v.At != e.At || v.Actor != e.Actor {
					return bad
				}
				c, ok := configs[v.ConfigurationID]
				o, found := observations[v.Observation.ID]
				if !ok || !found || !samePolicyStatusValue(o, v.Observation) || c.BindingID != o.BindingID || c.Source != o.Source || c.FallbackStatus != "published" || v.Status != "published" || v.Provenance != "admin-configured" || v.RawStatus != "" || v.Evidence != nil {
					return bad
				}
				application := v.ConfigurationID + "\x00" + o.ID + "\x00" + v.Status + "\x00" + v.Provenance
				if applications[application] {
					return bad
				}
				applications[application] = true
				ids[v.ID] = true
			case "import-draft":
				d := e.ImportedDraft
				if d == nil {
					return bad
				}
				c, ok := configs[d.ConfigurationID]
				o, found := observations[d.Observation.ID]
				if !ok || !found || !samePolicyStatusValue(o, d.Observation) || c.BindingID != o.BindingID || c.Source != o.Source || !contains([]string{"draft", "published"}, c.FallbackStatus) {
					return bad
				}
				found = false
				for _, r := range record.Revisions {
					if r.ID == d.CandidateRevisionID && r.Digest == d.CandidateDigest && r.WorkflowDigest == d.WorkflowDigest && r.Sequence == e.PolicySequence && r.At == e.At && r.Actor == e.Actor && r.Source == nil {
						found = true
					}
				}
				if !found {
					return bad
				}
				mode = "desk-managed"
			default:
				return bad
			}
		}
	}
	return nil
}

func (s policyService) sourceStatus(ctx context.Context, id, authority string) (policySourceStatusReply, error) {
	var result policySourceStatusReply
	err := s.repository.View(ctx, func(ledger policyLedger) error {
		if err := s.authorized(ctx); err != nil {
			return err
		}
		record, ok := ledger.Policies[id]
		if !ok {
			return policyRefusal(404, CodeNotFound, "Policy not found.")
		}
		result = policySourceStatusView(record, ledger.Sources[id], ledger.SourceAuthorities[id], authority)
		return nil
	})
	return result, err
}
func (s policyService) sourceStatusCommand(ctx context.Context, id string, actor policyActor, q policySourceStatusRequest, authority string) (policyMutationReply, error) {
	if !policyOperationIDForm.MatchString(q.OperationID) || !contains(policySourceAuthorityKinds(), "authority-"+q.Action) || strings.TrimSpace(q.Rationale) == "" || len(q.Rationale) > 8192 {
		return policyMutationReply{}, policyRefusal(400, CodeBadRequest, "Review a valid source authority action and rationale.")
	}
	raw, _ := json.Marshal(struct {
		Policy string
		Actor  policyActor
		Body   policySourceStatusRequest
	}{id, actor, q})
	command := policyCommand{Kind: "authority-" + q.Action, PolicyID: id, OperationID: q.OperationID, RequestDigest: digestOf(raw), Actor: actor}
	var completed *policyMutationReply
	if err := s.repository.View(ctx, func(ledger policyLedger) error {
		if err := s.authorized(ctx); err != nil {
			return err
		}
		var err error
		completed, err = s.recovered(ctx, ledger, command)
		return err
	}); err != nil {
		return policyMutationReply{}, err
	}
	if completed != nil {
		return *completed, nil
	}
	if q.Action == "import-draft" {
		if q.Draft == nil || len(q.Draft.Title) > 500 || len(q.Draft.Narrative) > policyNarrativeLimit || len(q.Draft.ArtifactVersion) > policyArtifactVersionLimit {
			return policyMutationReply{}, policyRefusal(400, CodeBadRequest, "Review bounded draft text and version before importing.")
		}
		if strings.TrimSpace(q.Draft.ArtifactVersion) != "" {
			if s.validateVersion == nil {
				return policyMutationReply{}, policyRefusal(503, "runtime-unavailable", "Runtime version checks are unavailable.")
			}
			if err := s.validateVersion(ctx, q.Draft.ArtifactVersion, q.Draft.ArtifactVersion); err != nil {
				return policyMutationReply{}, policyBeforeCommitFailure(err)
			}
		}
	}
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
		source, stream := ledger.Sources[id], ledger.SourceAuthorities[id]
		if q.BaseRevision != policyEffectiveRevision(record, stream) || q.BaseSourceRevision != policySourceRevision(source) || q.BaseAuthorityRevision != policyAuthorityRevision(stream) {
			return false, policyRefusal(409, "authority-conflict", "The policy, source or authority configuration changed. Review the current basis.")
		}
		if len(ledger.Operations) >= policyOperationLimit || stream != nil && len(stream.Events) >= policyHistoryLimit {
			return false, policyRefusal(409, "storage-capacity", "Source authority history has reached its supported capacity.")
		}
		view := policySourceStatusView(record, source, stream, authority)
		now := s.lifecycleNow()
		eventID, err := newPolicyEventID("authority")
		if err != nil {
			return false, err
		}
		event := policySourceAuthorityEvent{ID: eventID, Kind: q.Action, At: now, Actor: actor, Rationale: q.Rationale, PolicySequence: policyEventCount(record)}
		receipt := policyOperationReceipt{OperationID: q.OperationID, Kind: command.Kind, PolicyID: id, RequestDigest: command.RequestDigest, At: now, Actor: actor}
		switch q.Action {
		case "set-ownership":
			if !contains([]string{"desk-managed", "externally-managed"}, q.ManagementMode) {
				return false, policyRefusal(400, CodeBadRequest, "Choose Desk-managed or externally managed ownership.")
			}
			if q.ManagementMode == view.Ownership.Mode && !view.Ownership.Inherited {
				return false, policyRefusal(409, "authority-conflict", "This ownership is already recorded.")
			}
			event.ManagementMode = q.ManagementMode
		case "configure":
			if !view.Availability.Usable {
				return false, policyRefusal(409, "source-status-unavailable", "A current usable source observation is required to configure source status.")
			}
			if view.Capability.Kind != "not-exposed" {
				return false, policyRefusal(409, "unsupported-lifecycle-mapping", "This Gateway lifecycle capability has no supported authority adapter.")
			}
			if q.BindingID != source.Binding.BindingID {
				return false, policyRefusal(409, "authority-conflict", "Review the exact current source binding.")
			}
			fallback := q.FallbackStatus
			if fallback == "" {
				fallback = "draft"
			}
			if !contains([]string{"draft", "published"}, fallback) {
				return false, policyRefusal(400, CodeBadRequest, "Choose Draft or an explicit administrator-configured Published fallback.")
			}
			configID, err := newPolicyEventID("configuration")
			if err != nil {
				return false, err
			}
			event.Configuration = &policySourceStatusConfiguration{ID: configID, BindingID: q.BindingID, Source: source.Binding.Source, Capability: "not-exposed", FallbackStatus: fallback, At: now, Actor: actor}
			receipt.ConfigurationID = configID
		case "apply-external", "import-draft":
			p := view.CurrentPreview
			if !p.Eligible {
				return false, policyRefusal(409, "source-status-unavailable", "The current source status basis is unavailable or needs configuration review.")
			}
			if q.ObservationID != p.ObservationID || q.ConfigurationID != p.ConfigurationID {
				return false, policyRefusal(409, "authority-conflict", "Review the exact current observation and configuration.")
			}
			observation := source.Observations[len(source.Observations)-1]
			// Exact retained source material must exist before it can be applied/imported.
			if _, err := s.artifacts.Get(ctx, observation.Original); err != nil {
				return false, policyBeforeCommitFailure(err)
			}
			if _, err := s.artifacts.Get(ctx, observation.Document); err != nil {
				return false, policyBeforeCommitFailure(err)
			}
			receipt.ConfigurationID, receipt.ObservationID = q.ConfigurationID, q.ObservationID
			if q.Action == "apply-external" {
				action := view.Actions["applyExternal"]
				if !action.Allowed {
					code := action.Reason
					if code == "external-ownership-required" {
						code = "external-ownership"
					}
					return false, policyRefusal(409, code, "Review external ownership and an eligible unapplied non-Draft status.")
				}
				versionID, err := newPolicyEventID("external")
				if err != nil {
					return false, err
				}
				event.ExternalVersion = &policyExternalVersion{ID: versionID, EventID: event.ID, ConfigurationID: q.ConfigurationID, Observation: observation, Status: p.Status, Provenance: p.Provenance, At: now, Actor: actor}
				receipt.ExternalVersionID = versionID
			} else {
				if !view.Actions["importDraft"].Allowed {
					return false, policyRefusal(409, "draft-status-required", "Review a usable source status before explicitly importing a new Desk draft.")
				}
				if len(record.Revisions) >= policyHistoryLimit {
					return false, policyRefusal(409, "storage-capacity", "This policy has reached its supported draft history capacity.")
				}
				head := policyHead(record)
				raw, _ := json.Marshal(policyCandidateContent{SchemaVersion: 1, Title: q.Draft.Title, Narrative: q.Draft.Narrative, ArtifactVersion: q.Draft.ArtifactVersion})
				content, err := s.artifacts.Put(ctx, raw)
				if err != nil {
					return false, err
				}
				revisionID, err := newPolicyEventID("revision")
				if err != nil {
					return false, err
				}
				revision := policyRevision{ID: revisionID, Sequence: policyEventCount(record) + 1, Title: q.Draft.Title, ArtifactVersion: q.Draft.ArtifactVersion, Digest: content.SHA256, Content: content, Workflow: head.Workflow, WorkflowDigest: head.WorkflowDigest, At: now, Actor: actor}
				record.Revisions = append(record.Revisions, revision)
				record.HeadRevisionID, record.CurrentReviewID, record.UpdatedAt = revision.ID, "", now
				ledger.Policies[id] = record
				event.PolicySequence = revision.Sequence
				event.ImportedDraft = &policySourceImportedDraft{ConfigurationID: q.ConfigurationID, Observation: observation, CandidateRevisionID: revision.ID, CandidateDigest: revision.Digest, WorkflowDigest: revision.WorkflowDigest}
				// Import is also the explicit review of this source update for the new draft.
				if len(source.Resolutions) >= policyHistoryLimit {
					return false, policyRefusal(409, "storage-capacity", "Source resolution history has reached its supported capacity.")
				}
				source.Resolutions = append(source.Resolutions, policySourceResolution{ObservationID: observation.ID, CandidateRevisionID: revision.ID, CandidateDigest: revision.Digest, WorkflowDigest: revision.WorkflowDigest, At: now, Actor: actor, Comment: q.Rationale})
			}
		}
		if stream == nil {
			stream = &policySourceAuthorityStream{Version: 1, Events: []policySourceAuthorityEvent{}}
		}
		event.Sequence = len(stream.Events) + 1
		stream.Events = append(stream.Events, event)
		if ledger.SourceAuthorities == nil {
			ledger.SourceAuthorities = map[string]*policySourceAuthorityStream{}
		}
		ledger.SourceAuthorities[id] = stream
		current := policySourceStatusView(record, source, stream, authority)
		receipt.Revision, receipt.CandidateDigest, receipt.AuthorityRevision, receipt.AuthorityEventID = current.PolicyRevision, current.CandidateDigest, current.Revision, event.ID
		result = policyMutationReply{OutcomeKind: "source-authority", Delivery: "committed", PolicyID: id, PolicyRevision: current.PolicyRevision, SourceStatus: &current, Receipt: receipt}
		encoded, _ := json.Marshal(result)
		retained, err := s.artifacts.Put(ctx, encoded)
		if err != nil {
			return false, err
		}
		ledger.Operations[q.OperationID] = policyStoredOperation{OperationID: q.OperationID, Kind: command.Kind, PolicyID: id, RequestDigest: command.RequestDigest, Result: retained}
		return true, s.authorized(ctx)
	})
	return result, err
}

func validPolicySourceAuthorityResult(result policyMutationReply) bool {
	if !strings.HasPrefix(result.Receipt.Kind, "authority-") {
		return result.SourceStatus == nil && result.Receipt.AuthorityRevision == "" && result.Receipt.AuthorityEventID == "" && result.Receipt.ConfigurationID == "" && result.Receipt.ObservationID == "" && result.Receipt.ExternalVersionID == ""
	}
	r := result.SourceStatus
	if r == nil || !retainedArtifactDigest.MatchString(r.CandidateDigest) || !policyEventIDForm.MatchString(r.CandidateRevisionID) || !strings.HasPrefix(r.CandidateRevisionID, "revision_") || result.Receipt.LifecycleEventID != "" || result.Receipt.LifecycleRevision != "" || result.Receipt.ReviewID != "" || result.Receipt.PublicationID != "" || result.OutcomeKind != "source-authority" || r.Revision != result.Receipt.AuthorityRevision || r.PolicyRevision != result.Receipt.Revision || r.CandidateDigest != result.Receipt.CandidateDigest || len(r.Events) == 0 || r.Revision != policyAuthorityRevision(&policySourceAuthorityStream{Version: 1, Events: r.Events}) || result.Lifecycle != nil || result.SourceSync != nil {
		return false
	}
	event := r.Events[len(r.Events)-1]
	if event.ID != result.Receipt.AuthorityEventID || "authority-"+event.Kind != result.Receipt.Kind || event.At != result.Receipt.At || event.Actor != result.Receipt.Actor {
		return false
	}
	summary := policyAuthoritySummary(&policySourceAuthorityStream{Version: 1, Events: r.Events})
	if !samePolicyStatusValue(summary.Ownership, r.Ownership) || summary.ExternalVersionCount != len(r.ExternalVersions) || !samePolicyStatusValue(summary.LastApplied, r.LastApplied) {
		return false
	}
	configurations := []policySourceStatusConfiguration{}
	versions := []policyExternalVersion{}
	configs := map[string]policySourceStatusConfiguration{}
	ids := map[string]bool{}
	mode := "desk-managed"
	for i, e := range r.Events {
		if !policyAuthorityIDForm.MatchString(e.ID) || !strings.HasPrefix(e.ID, "authority_") || ids[e.ID] || e.Sequence != i+1 || !validPolicyLifecycleTime(e.At) || !validPolicyActor(e.Actor) || strings.TrimSpace(e.Rationale) == "" || len(e.Rationale) > 8192 {
			return false
		}
		ids[e.ID] = true
		fields := 0
		if e.Configuration != nil {
			fields++
		}
		if e.ManagementMode != "" {
			fields++
		}
		if e.ExternalVersion != nil {
			fields++
		}
		if e.ImportedDraft != nil {
			fields++
		}
		if fields != 1 {
			return false
		}
		switch e.Kind {
		case "configure":
			c := e.Configuration
			if c == nil || !policyAuthorityIDForm.MatchString(c.ID) || !strings.HasPrefix(c.ID, "configuration_") || ids[c.ID] || !retainedArtifactDigest.MatchString(c.BindingID) || !validPolicyConnectedIdentity(c.Source) || c.Capability != "not-exposed" || !contains([]string{"draft", "published"}, c.FallbackStatus) || c.At != e.At || c.Actor != e.Actor {
				return false
			}
			ids[c.ID] = true
			configs[c.ID] = *c
		case "set-ownership":
			if !contains([]string{"desk-managed", "externally-managed"}, e.ManagementMode) {
				return false
			}
			mode = e.ManagementMode
		case "apply-external":
			v := e.ExternalVersion
			if v == nil || mode != "externally-managed" || !policyAuthorityIDForm.MatchString(v.ID) || !strings.HasPrefix(v.ID, "external_") || ids[v.ID] || v.EventID != e.ID || v.At != e.At || v.Actor != e.Actor || v.Status != "published" || v.Provenance != "admin-configured" || v.RawStatus != "" || v.Evidence != nil || !validPolicyAuthorityObservation(v.Observation) {
				return false
			}
			c, ok := configs[v.ConfigurationID]
			if !ok || c.FallbackStatus != "published" || c.BindingID != v.Observation.BindingID || c.Source != v.Observation.Source {
				return false
			}
			ids[v.ID] = true
		case "import-draft":
			d := e.ImportedDraft
			if d == nil || !validPolicyAuthorityObservation(d.Observation) || !policyEventIDForm.MatchString(d.CandidateRevisionID) || !strings.HasPrefix(d.CandidateRevisionID, "revision_") || !retainedArtifactDigest.MatchString(d.CandidateDigest) || !retainedArtifactDigest.MatchString(d.WorkflowDigest) {
				return false
			}
			c, ok := configs[d.ConfigurationID]
			if !ok || c.BindingID != d.Observation.BindingID || c.Source != d.Observation.Source {
				return false
			}
			mode = "desk-managed"
		default:
			return false
		}
		if e.Configuration != nil {
			configurations = append(configurations, *e.Configuration)
		}
		if e.ExternalVersion != nil {
			versions = append(versions, *e.ExternalVersion)
		}
	}
	var current *policySourceStatusConfiguration
	if len(configurations) > 0 {
		c := configurations[len(configurations)-1]
		current = &c
	}
	if !samePolicyStatusValue(current, r.CurrentConfig) {
		return false
	}
	if !samePolicyStatusValue(configurations, r.Configurations) || !samePolicyStatusValue(versions, r.ExternalVersions) {
		return false
	}
	config, observation, external := "", "", ""
	switch event.Kind {
	case "configure":
		if event.Configuration == nil {
			return false
		}
		config = event.Configuration.ID
	case "set-ownership":
		if !contains([]string{"desk-managed", "externally-managed"}, event.ManagementMode) {
			return false
		}
	case "apply-external":
		if event.ExternalVersion == nil || event.ExternalVersion.EventID != event.ID || event.ExternalVersion.Actor != event.Actor || event.ExternalVersion.At != event.At {
			return false
		}
		config, observation, external = event.ExternalVersion.ConfigurationID, event.ExternalVersion.Observation.ID, event.ExternalVersion.ID
	case "import-draft":
		if event.ImportedDraft == nil || event.ImportedDraft.CandidateDigest != r.CandidateDigest || event.ImportedDraft.CandidateRevisionID != r.CandidateRevisionID {
			return false
		}
		config, observation = event.ImportedDraft.ConfigurationID, event.ImportedDraft.Observation.ID
	default:
		return false
	}
	return config == result.Receipt.ConfigurationID && observation == result.Receipt.ObservationID && external == result.Receipt.ExternalVersionID
}

func validPolicyAuthorityObservation(o policySourceObservation) bool {
	return retainedArtifactDigest.MatchString(o.ID) && retainedArtifactDigest.MatchString(o.BindingID) && validPolicyConnectedIdentity(o.Source) && o.SourceRevision != "" && len(o.SourceRevision) <= 4096 && validPolicyTime(o.ObservedAt) && validPolicyTime(o.RetainedAt) && o.Availability == "present" && o.Coverage == "snapshot-only" && o.Lifecycle.Kind == "not-exposed" && o.Original.valid() && o.Document.valid() && o.Content.BytesDigest == "sha256:"+o.Original.SHA256 && o.Content.ByteLength == o.Original.Bytes && o.Extraction.Content.BytesDigest == "sha256:"+o.Document.SHA256 && o.Extraction.Content.ByteLength == o.Document.Bytes && o.Extraction.Content.MediaType == "application/vnd.jpack.attachment+json" && o.Extraction.Profile == "attachment-record-v1/canon" && retainedArtifactDigest.MatchString(o.SemanticDigest) && validPolicyPrefixedDigest(o.Extraction.ConfigurationDigest)
}
