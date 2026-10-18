package desk

// Private, manually reviewed source authority. Gateway observations remain raw
// evidence; neither acquisition nor a fallback can manufacture local approval.
import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

type policySourceAuthorityStream struct {
	Version int                          `json:"version"`
	Events  []policySourceAuthorityEvent `json:"events"`
}
type policySourceOwnership struct {
	Mode      string       `json:"mode"`
	Inherited bool         `json:"inherited"`
	EventID   string       `json:"eventId,omitempty"`
	At        string       `json:"at,omitempty"`
	Actor     *policyActor `json:"actor,omitempty"`
}
type policySourceStatusConfiguration struct {
	ID             string                  `json:"id"`
	BindingID      string                  `json:"bindingId"`
	Source         policyConnectedIdentity `json:"source"`
	Capability     string                  `json:"capability"`
	FallbackStatus string                  `json:"fallbackStatus"`
	At             string                  `json:"at"`
	Actor          policyActor             `json:"actor"`
}
type policySourceStatusPreview struct {
	Eligible        bool                     `json:"eligible"`
	Reason          string                   `json:"reason,omitempty"`
	Status          string                   `json:"status,omitempty"`
	Provenance      string                   `json:"provenance,omitempty"`
	ConfigurationID string                   `json:"configurationId,omitempty"`
	ObservationID   string                   `json:"observationId,omitempty"`
	BindingID       string                   `json:"bindingId,omitempty"`
	SourceRevision  string                   `json:"sourceRevision,omitempty"`
	Source          *policyConnectedIdentity `json:"source,omitempty"`
	Content         *policySourceContent     `json:"content,omitempty"`
	RawStatus       string                   `json:"rawStatus,omitempty"`
	Evidence        *retainedArtifactRef     `json:"evidence,omitempty"`
}
type policyExternalVersion struct {
	ID              string                  `json:"id"`
	EventID         string                  `json:"eventId"`
	ConfigurationID string                  `json:"configurationId"`
	Observation     policySourceObservation `json:"observation"`
	Status          string                  `json:"status"`
	Provenance      string                  `json:"provenance"`
	RawStatus       string                  `json:"rawStatus,omitempty"`
	Evidence        *retainedArtifactRef    `json:"evidence,omitempty"`
	At              string                  `json:"at"`
	Actor           policyActor             `json:"actor"`
}
type policySourceImportedDraft struct {
	ConfigurationID     string                  `json:"configurationId"`
	Observation         policySourceObservation `json:"observation"`
	CandidateRevisionID string                  `json:"candidateRevisionId"`
	CandidateDigest     string                  `json:"candidateDigest"`
	WorkflowDigest      string                  `json:"workflowDigest"`
}
type policySourceAuthorityEvent struct {
	ID              string                           `json:"id"`
	Sequence        int                              `json:"sequence"`
	Kind            string                           `json:"kind"`
	At              string                           `json:"at"`
	Actor           policyActor                      `json:"actor"`
	Rationale       string                           `json:"rationale"`
	PolicySequence  int                              `json:"policySequence"`
	Configuration   *policySourceStatusConfiguration `json:"configuration,omitempty"`
	ManagementMode  string                           `json:"managementMode,omitempty"`
	ExternalVersion *policyExternalVersion           `json:"externalVersion,omitempty"`
	ImportedDraft   *policySourceImportedDraft       `json:"importedDraft,omitempty"`
}
type policySourceAuthoritySummary struct {
	Revision             string                 `json:"revision"`
	Ownership            policySourceOwnership  `json:"ownership"`
	ExternalVersionCount int                    `json:"externalVersionCount"`
	LastApplied          *policyExternalVersion `json:"lastApplied,omitempty"`
}
type policySourceStatusAvailability struct {
	State       string `json:"state"`
	Usable      bool   `json:"usable"`
	Gap         bool   `json:"gap"`
	ErrorCode   string `json:"errorCode,omitempty"`
	LastSuccess string `json:"lastSuccess,omitempty"`
}
type policySourceStatusCapability struct {
	Kind             string `json:"kind"`
	MappingSupported bool   `json:"mappingSupported"`
}
type policySourceStatusAction struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
}
type policySourceStatusReply struct {
	Recovery            *policyRecoveryView                 `json:"recovery,omitempty"`
	Revision            string                              `json:"revision"`
	PolicyRevision      string                              `json:"policyRevision"`
	SourceRevision      string                              `json:"sourceRevision"`
	CandidateDigest     string                              `json:"candidateDigest"`
	CandidateRevisionID string                              `json:"candidateRevisionId"`
	Ownership           policySourceOwnership               `json:"ownership"`
	Binding             *policySourceBinding                `json:"binding,omitempty"`
	Capability          policySourceStatusCapability        `json:"capability"`
	Availability        policySourceStatusAvailability      `json:"availability"`
	CurrentConfig       *policySourceStatusConfiguration    `json:"currentConfig,omitempty"`
	CurrentPreview      policySourceStatusPreview           `json:"currentPreview"`
	LastApplied         *policyExternalVersion              `json:"lastApplied,omitempty"`
	Configurations      []policySourceStatusConfiguration   `json:"configurations"`
	ExternalVersions    []policyExternalVersion             `json:"externalVersions"`
	Events              []policySourceAuthorityEvent        `json:"events"`
	Actions             map[string]policySourceStatusAction `json:"actions"`
}
type policySourceStatusDraft struct {
	Title           string `json:"title"`
	Narrative       string `json:"narrative"`
	ArtifactVersion string `json:"artifactVersion"`
}
type policySourceStatusRequest struct {
	OperationID           string                   `json:"operationId"`
	BaseRevision          string                   `json:"baseRevision"`
	BaseSourceRevision    string                   `json:"baseSourceRevision"`
	BaseAuthorityRevision string                   `json:"baseAuthorityRevision"`
	Rationale             string                   `json:"rationale"`
	Action                string                   `json:"action"`
	BindingID             string                   `json:"bindingId,omitempty"`
	FallbackStatus        string                   `json:"fallbackStatus,omitempty"`
	ManagementMode        string                   `json:"managementMode,omitempty"`
	ObservationID         string                   `json:"observationId,omitempty"`
	ConfigurationID       string                   `json:"configurationId,omitempty"`
	Draft                 *policySourceStatusDraft `json:"draft,omitempty"`
}

var policyAuthorityIDForm = regexp.MustCompile(`^(authority|configuration|external)_[a-f0-9]{48}$`)

func policySourceAuthorityKinds() []string {
	return []string{"authority-configure", "authority-set-ownership", "authority-apply-external", "authority-import-draft"}
}
func policyAuthorityRevision(stream *policySourceAuthorityStream) string {
	raw, _ := json.Marshal(stream)
	return digestOf(raw)
}
func policyAuthorityOwnership(stream *policySourceAuthorityStream) (policySourceOwnership, int) {
	owner := policySourceOwnership{Mode: "desk-managed", Inherited: true}
	boundary := 0
	if stream != nil {
		for _, event := range stream.Events {
			mode := event.ManagementMode
			if event.Kind == "import-draft" {
				mode = "desk-managed"
			}
			if mode != "" {
				if mode != owner.Mode {
					boundary = event.PolicySequence
				}
				actor := event.Actor
				owner = policySourceOwnership{Mode: mode, EventID: event.ID, At: event.At, Actor: &actor}
			}
		}
	}
	return owner, boundary
}
func policyEffectiveRevision(record policyStoredRecord, stream *policySourceAuthorityStream) string {
	owner, _ := policyAuthorityOwnership(stream)
	if owner.EventID == "" {
		return policyRecordRevision(record)
	}
	return digestOf([]byte(policyRecordRevision(record) + "\x00" + owner.EventID))
}
func policyAuthoritySummary(stream *policySourceAuthorityStream) *policySourceAuthoritySummary {
	if stream == nil {
		return nil
	}
	owner, _ := policyAuthorityOwnership(stream)
	result := &policySourceAuthoritySummary{Revision: policyAuthorityRevision(stream), Ownership: owner}
	for _, e := range stream.Events {
		if e.ExternalVersion != nil {
			result.ExternalVersionCount++
			v := *e.ExternalVersion
			result.LastApplied = &v
		}
	}
	return result
}
func policyAuthorityCurrentCandidatePublished(record policyStoredRecord, stream *policySourceAuthorityStream) bool {
	owner, boundary := policyAuthorityOwnership(stream)
	boundary = policyRecoveryReviewBoundary(record, boundary)
	if owner.Mode != "desk-managed" {
		return false
	}
	head := policyHead(record)
	for _, publication := range record.Publications {
		if publication.Sequence > boundary && publication.CandidateRevisionID == head.ID && publication.CandidateDigest == head.Digest && publication.WorkflowDigest == head.WorkflowDigest {
			return true
		}
	}
	return false
}
func policyAuthorityCurrentReview(record policyStoredRecord, stream *policySourceAuthorityStream) *policyReview {
	owner, boundary := policyAuthorityOwnership(stream)
	boundary = policyRecoveryReviewBoundary(record, boundary)
	review := policyCurrentReview(record)
	if owner.Mode != "desk-managed" || review != nil && review.Sequence <= boundary {
		return nil
	}
	return review
}
func policyAuthorityReviewState(record policyStoredRecord, stream *policySourceAuthorityStream) string {
	if r := policyAuthorityCurrentReview(record, stream); r != nil {
		if r.Decision == "approve" {
			return "approved"
		}
		return "changes-requested"
	}
	return "unreviewed"
}
func policyAuthorityAdmission(record policyStoredRecord, stream *policySourceAuthorityStream, kind string) error {
	owner, _ := policyAuthorityOwnership(stream)
	if contains([]string{"review", "publish", "activate"}, kind) && owner.Mode != "desk-managed" {
		return policyRefusal(409, "external-ownership", "Import a reviewed Desk draft before reviewing, publishing or activating a local release.")
	}
	return nil
}

// A future adapter must supply normalized evidence only after its own strict
// supported wire verification. This pure evaluator does not accept HTTP claims.
type policyStatusEvidence struct {
	Capability, RawStatus string
	Evidence              *retainedArtifactRef
}

func evaluatePolicySourceStatus(e policyStatusEvidence, fallback string, mapping map[string]string) (string, string, string) {
	switch e.Capability {
	case "not-exposed":
		if contains([]string{"draft", "published"}, fallback) {
			return fallback, "admin-configured", ""
		}
		return "", "", "configuration-required"
	case "exposed":
		if e.RawStatus == "" || e.Evidence == nil || !e.Evidence.valid() {
			return "", "", "lifecycle-evidence-unavailable"
		}
		status, ok := mapping[e.RawStatus]
		if !ok || !contains([]string{"draft", "published", "effective", "superseded", "expired", "withdrawn"}, status) {
			return "", "", "unknown-source-status"
		}
		return status, "source-provided", ""
	default:
		return "", "", "lifecycle-capability-unavailable"
	}
}
func policyStatusAvailability(source *policyConnectedSource, authority string) policySourceStatusAvailability {
	a := policySourceStatusAvailability{State: "unbound"}
	if policyRecoverySourceHeld(source) {
		return policySourceStatusAvailability{State: "restored-source-consent-required", Gap: true, ErrorCode: "restored-source-consent-required"}
	}
	if source == nil {
		return a
	}
	a.Gap, a.ErrorCode, a.LastSuccess = source.Gap, source.ErrorCode, source.LastSuccess
	if source.State != "active" {
		a.State = source.State
		return a
	}
	if authority == "" || source.Grant.Authority != authority {
		a.State = "authorization-ended"
		return a
	}
	if source.Binding == nil {
		a.State = "binding"
		return a
	}
	if source.ErrorCode != "" {
		a.State = "unavailable"
		return a
	}
	if source.Gap {
		a.State = "gap"
		return a
	}
	if len(source.Observations) == 0 {
		a.State = "no-observation"
		return a
	}
	o := source.Observations[len(source.Observations)-1]
	if o.BindingID != source.Binding.BindingID || o.Source != source.Binding.Source {
		a.State = "binding-changed"
		return a
	}
	a.State, a.Usable = "present", true
	return a
}
func policySourceStatusView(record policyStoredRecord, source *policyConnectedSource, stream *policySourceAuthorityStream, authority string) policySourceStatusReply {
	owner, _ := policyAuthorityOwnership(stream)
	head := policyHead(record)
	r := policySourceStatusReply{Revision: policyAuthorityRevision(stream), PolicyRevision: policyEffectiveRevision(record, stream), SourceRevision: policySourceRevision(source), CandidateDigest: head.Digest, CandidateRevisionID: head.ID, Ownership: owner, Capability: policySourceStatusCapability{Kind: "unavailable"}, Availability: policyStatusAvailability(source, authority), Configurations: []policySourceStatusConfiguration{}, ExternalVersions: []policyExternalVersion{}, Events: []policySourceAuthorityEvent{}, Actions: map[string]policySourceStatusAction{}}
	r.Recovery = policyRecoveryProjection(record, stream)
	if source != nil && source.Binding != nil {
		r.Binding = source.Binding
		r.Capability.Kind = source.Binding.Lifecycle.Kind
	}
	if stream != nil {
		r.Events = stream.Events
		for _, e := range stream.Events {
			if e.Configuration != nil {
				r.Configurations = append(r.Configurations, *e.Configuration)
				c := *e.Configuration
				r.CurrentConfig = &c
			}
			if e.ExternalVersion != nil {
				r.ExternalVersions = append(r.ExternalVersions, *e.ExternalVersion)
				v := *e.ExternalVersion
				r.LastApplied = &v
			}
		}
	}
	p := policySourceStatusPreview{Reason: "configuration-required"}
	if r.CurrentConfig != nil {
		p.ConfigurationID = r.CurrentConfig.ID
	}
	if !r.Availability.Usable {
		p.Reason = "source-" + r.Availability.State
	} else if r.CurrentConfig != nil {
		o := source.Observations[len(source.Observations)-1]
		p.ObservationID, p.BindingID, p.SourceRevision = o.ID, o.BindingID, o.SourceRevision
		identity, content := o.Source, o.Content
		p.Source, p.Content = &identity, &content
		if r.CurrentConfig.BindingID != o.BindingID || r.CurrentConfig.Source != o.Source {
			p.Reason = "configuration-binding-changed"
		} else {
			p.Status, p.Provenance, p.Reason = evaluatePolicySourceStatus(policyStatusEvidence{Capability: o.Lifecycle.Kind}, r.CurrentConfig.FallbackStatus, nil)
			p.Eligible = p.Reason == ""
		}
	}
	r.CurrentPreview = p
	r.Actions["setOwnership"] = policySourceStatusAction{Allowed: true}
	r.Actions["configure"] = policySourceStatusAction{Allowed: r.Availability.Usable && r.Capability.Kind == "not-exposed"}
	if !r.Actions["configure"].Allowed {
		r.Actions["configure"] = policySourceStatusAction{Reason: "source-" + r.Availability.State}
	}
	apply := policySourceStatusAction{Allowed: p.Eligible && p.Status != "draft" && owner.Mode == "externally-managed", Reason: p.Reason}
	if p.Eligible && p.Status == "draft" {
		apply.Reason = "import-draft-required"
	} else if p.Eligible && owner.Mode != "externally-managed" {
		apply.Reason = "external-ownership-required"
	}
	if apply.Allowed {
		for _, v := range r.ExternalVersions {
			if v.Observation.ID == p.ObservationID && v.ConfigurationID == p.ConfigurationID && v.Status == p.Status && v.Provenance == p.Provenance {
				apply = policySourceStatusAction{Reason: "already-applied"}
				break
			}
		}
	}
	r.Actions["applyExternal"] = apply
	r.Actions["importDraft"] = policySourceStatusAction{Allowed: p.Eligible, Reason: p.Reason}
	return r
}

func (q *policySourceStatusRequest) UnmarshalJSON(raw []byte) error {
	type request policySourceStatusRequest
	var value request
	var members map[string]json.RawMessage
	if rejectDuplicateMembers(raw) != nil || decodeDataJSON(raw, &value) != nil || json.Unmarshal(raw, &members) != nil || members == nil {
		return errors.New("invalid source authority request")
	}
	allowed := []string{"operationId", "baseRevision", "baseSourceRevision", "baseAuthorityRevision", "rationale", "action"}
	switch value.Action {
	case "configure":
		allowed = append(allowed, "bindingId", "fallbackStatus")
	case "set-ownership":
		allowed = append(allowed, "managementMode")
	case "apply-external":
		allowed = append(allowed, "observationId", "configurationId")
	case "import-draft":
		allowed = append(allowed, "observationId", "configurationId", "draft")
	default:
		return errors.New("invalid source authority action")
	}
	if raw, ok := members["fallbackStatus"]; ok {
		var fallback string
		if json.Unmarshal(raw, &fallback) != nil || !contains([]string{"draft", "published"}, fallback) {
			return errors.New("invalid fallback status")
		}
	}
	for key, member := range members {
		if !contains(allowed, key) || string(member) == "null" {
			return errors.New("invalid source authority field")
		}
	}
	if strings.TrimSpace(value.Rationale) == "" || len(value.Rationale) > 8192 {
		return errors.New("review a bounded rationale")
	}
	*q = policySourceStatusRequest(value)
	return nil
}
