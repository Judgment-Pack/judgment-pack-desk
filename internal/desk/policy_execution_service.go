package desk

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/jsonscalar"
	"strconv"
	"time"
	"unicode/utf8"
)

// These injected capabilities are host-only. resolveRelease must authenticate
// the owning Runner, require its exact job revision and absence of all triggers,
// and return the existing canonical typed Release preimage. It runs outside the
// Policy lock. The adoption resolver reads only this locked ledger and immutable
// retained content; it cannot accept a caller-selected dependency subset.
type policyExecutionResolvedRelease struct {
	Workspace string
	Resource  policyExecutionResource
	Bytes     []byte
}
type policyExecutionService struct {
	repository      filePolicyRepository
	artifacts       retainedArtifactRepository
	identity        policyExecutionIdentity
	authorize       func() bool // trusted current local administrator, never launch token/OIDC
	clock           func() time.Time
	resolveRelease  func(context.Context, string) (policyExecutionResolvedRelease, error)
	resolveAdoption func(context.Context, policyLedger, string, string) (policyExecutionAdoption, error)
}
type policyExecutionRegisterRequest struct {
	OperationID string `json:"operationId"`
	JobID       string `json:"jobId"`
	AdoptionID  string `json:"adoptionId"`
	ReviewID    string `json:"reviewId"`
}
type policyExecutionRegisteredResult struct {
	Delivery     string                      `json:"delivery"`
	Resource     policyExecutionResource     `json:"resource"`
	Registration policyExecutionRegistration `json:"registration"`
}

// The channel adapter creates this only after external ownership acquisition.
// A JSON body cannot supply or replace ownership verification or scope.
type policyExecutionSession struct {
	identity  policyExecutionIdentity
	id        string
	challenge string
	owned     func() error
}
type policyExecutionActivateRequest struct {
	Challenge string                 `json:"challenge"`
	Signing   policyExecutionSigning `json:"signing"`
}
type policyExecutionActivateResult struct {
	Delivery      string `json:"delivery"`
	Epoch         string `json:"epoch"`
	RequestDigest string `json:"requestDigest"`
}
type policyExecutionInspectResult struct {
	Delivery     string                      `json:"delivery"`
	Registration policyExecutionRegistration `json:"registration"`
	Eligible     bool                        `json:"eligible"`
	Reason       string                      `json:"reason"`
	At           string                      `json:"at"`
}
type policyExecutionHistoryResult struct {
	Delivery      string `json:"delivery"`
	IntentID      string `json:"intentId"`
	OperationID   string `json:"operationId"`
	AttemptID     string `json:"attemptId"`
	Epoch         string `json:"epoch"`
	State         string `json:"state"`
	RequestDigest string `json:"requestDigest"`
	Reason        string `json:"reason"`
	At            string `json:"at"`
}

func (s policyExecutionService) authorized(ctx context.Context) error {
	if ctx.Err() != nil || s.authorize == nil || !s.authorize() {
		return executionFailure("unauthorized")
	}
	if s.repository.anchor == nil || s.artifacts == nil || !s.identity.valid() || s.repository.anchor.identity.InstallationID != s.identity.InstallationID || s.repository.anchor.identity.WorkspaceID != s.identity.MachineWorkspace {
		return executionFailure("authority-unavailable")
	}
	return nil
}
func (s policyExecutionService) now() (string, error) {
	if s.clock == nil {
		return "", executionFailure("clock-unavailable")
	}
	t := s.clock().UTC()
	if t.Year() < 1970 || t.Year() > 9999 {
		return "", executionFailure("clock-unavailable")
	}
	return t.Format("2006-01-02T15:04:05Z"), nil
}
func (s policyExecutionService) scoped(l policyLedger) error {
	if l.SchemaVersion != 3 || l.Execution == nil || l.Execution.Identity != s.identity {
		return executionFailure("authority-unavailable")
	}
	return nil
}

// Explicit initialization is deliberately unexported and has no production
// caller in Phase A. Phase B must durably fence old executors BEFORE calling it.
func (s policyExecutionService) initialize(ctx context.Context) error {
	if err := s.authorized(ctx); err != nil {
		return err
	}
	return s.repository.Update(ctx, func(l *policyLedger) (bool, error) {
		if err := s.authorized(ctx); err != nil {
			return false, err
		}
		if l.Execution != nil {
			return false, s.scoped(*l)
		}
		if l.SchemaVersion != 2 {
			return false, executionFailure("authority-unavailable")
		}
		now, err := s.now()
		if err != nil {
			return false, err
		}
		if err = initializePolicyExecutionLedger(l, s.identity, now); err != nil {
			return false, err
		}
		return true, s.authorized(ctx)
	})
}
func initializePolicyExecutionLedger(l *policyLedger, identity policyExecutionIdentity, now string) error {
	if l.SchemaVersion != 2 || l.Execution != nil || !identity.valid() || !executionTime(now) {
		return executionFailure("authority-unavailable")
	}
	l.SchemaVersion = 3
	l.Execution = &policyExecutionState{Version: 1, Profile: policyExecutionProfile, Identity: identity, LastTime: now, Resources: map[string]policyExecutionRegisteredResource{}, RegistrationOperations: map[string]policyExecutionRegistrationReceipt{}, Intents: map[string]policyExecutionIntent{}, Epochs: map[string]policyExecutionEpoch{}, Attempts: map[string]policyExecutionAttempt{}}
	return nil
}

// A denial is a committed result, not a transaction error. In particular, time
// observed by an authenticated command survives refusal. Repository failure
// overrides any decision and is uncertain; this closure is never replayed.
func (s policyExecutionService) update(ctx context.Context, use func(*policyLedger, string) (bool, error)) error {
	if err := s.authorized(ctx); err != nil {
		return err
	}
	var refusal error
	err := s.repository.Update(ctx, func(l *policyLedger) (bool, error) {
		if err := s.authorized(ctx); err != nil {
			return false, err
		}
		if err := s.scoped(*l); err != nil {
			return false, err
		}
		now, err := s.now()
		if err != nil {
			refusal = err
			return false, nil
		}
		if now < l.Execution.LastTime {
			refusal = executionFailure("clock-unavailable")
			return false, nil
		}
		advanced := now > l.Execution.LastTime
		l.Execution.LastTime = now
		changed, denial := use(l, now)
		if err := s.authorized(ctx); err != nil {
			return false, err
		}
		refusal = denial
		return advanced || changed, nil
	})
	if err != nil {
		return err
	}
	return refusal
}

// Derive the entire closure from retained, independently approved adoption
// content, not mutable adoption-time freshness or a client-supplied list.
func resolvePolicyExecutionAdoption(a retainedArtifactRepository) func(context.Context, policyLedger, string, string) (policyExecutionAdoption, error) {
	return func(ctx context.Context, l policyLedger, id, reviewID string) (policyExecutionAdoption, error) {
		r, ok := l.Adoptions[id]
		if !ok || r.Recovery != nil {
			return policyExecutionAdoption{}, executionFailure("eligibility-blocked")
		}
		c, err := (policyService{artifacts: a}).adoptionContent(ctx, r)
		if err != nil {
			return policyExecutionAdoption{}, err
		}
		if err = archivedAdoptionContent(ctx, l, a, r, c, func(retainedArtifactRef) error { return nil }); err != nil {
			return policyExecutionAdoption{}, err
		}
		var approved policyAdoptionReview
		for _, v := range r.Reviews {
			if v.ID == reviewID && v.Decision == "approve" {
				approved = v
			}
		}
		if approved.ID == "" || approved.Check == nil {
			return policyExecutionAdoption{}, executionFailure("eligibility-blocked")
		}
		return policyExecutionAdoption{r.ID, r.CandidateID, r.Content, c, approved}, nil
	}
}

type policyExecutionReleaseMaterial struct {
	pack                               []byte
	packID, packVersion, runtimeDigest string
}

func validateExecutionRelease(v policyExecutionResolvedRelease, identity policyExecutionIdentity) (policyExecutionReleaseMaterial, error) {
	var result policyExecutionReleaseMaterial
	bad := executionFailure("unsupported-profile")
	if v.Workspace != identity.RunnerWorkspace || !v.Resource.valid() || len(v.Bytes) == 0 || len(v.Bytes) > 8<<20 || !utf8.Valid(v.Bytes) || jsonscalar.ValidateEscapes(v.Bytes) != nil || !json.Valid(v.Bytes) || rejectDuplicateMembers(v.Bytes) != nil || v.Resource.ReleaseDigest != "sha256:"+digestOf(v.Bytes) {
		return result, bad
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(v.Bytes, &fields) != nil {
		return result, bad
	}
	required := []string{"schemaVersion", "id", "pack", "packDigest", "packId", "packVersion", "title", "runtimeDigest", "createdAt", "config", "lock", "validation", "sample", "preview", "tests"}
	for _, name := range required {
		if fields[name] == nil || bytes.Equal(bytes.TrimSpace(fields[name]), []byte("null")) {
			return result, bad
		}
		if name != "validation" && name != "sample" && name != "preview" {
			var value string
			if json.Unmarshal(fields[name], &value) != nil {
				return result, bad
			}
		}
	}
	for name := range fields {
		if !contains(append(required, "testEvidence"), name) {
			return result, bad
		}
	}
	get := func(name string) string { var value string; _ = json.Unmarshal(fields[name], &value); return value }
	result = policyExecutionReleaseMaterial{[]byte(get("pack")), get("packId"), get("packVersion"), get("runtimeDigest")}
	var sample map[string]json.RawMessage
	if get("schemaVersion") != "1" || get("id") != v.Resource.ReleaseID || len(result.pack) == 0 || len(result.pack) > 1<<20 || !utf8.Valid(result.pack) || !json.Valid(result.pack) || get("packDigest") != "sha256:"+digestOf(result.pack) || result.packID == "" || len(result.packID) > 500 || result.packVersion == "" || len(result.packVersion) > 256 || !policyExecutionDigest.MatchString(result.runtimeDigest) || json.Unmarshal(fields["sample"], &sample) != nil || sample == nil {
		return result, bad
	}
	for name := range sample {
		if name != "facts" && name != "evidence" {
			return result, bad
		}
	}
	return result, nil
}

func (s policyExecutionService) register(ctx context.Context, q policyExecutionRegisterRequest) (policyExecutionRegisteredResult, error) {
	var result policyExecutionRegisteredResult
	if !policyOperationIDForm.MatchString(q.OperationID) || !policyExecutionID.MatchString(q.JobID) || !policyAdoptionIDForm.MatchString(q.AdoptionID) || !policyAdoptionReviewIDForm.MatchString(q.ReviewID) {
		return result, executionFailure("invalid-request")
	}
	if err := s.authorized(ctx); err != nil {
		return result, err
	}
	digest := executionHash(struct {
		Caller  string
		Request policyExecutionRegisterRequest
	}{executionCaller(s.identity), q})
	// Recover the original exact registration even if Runner later changes or
	// is unavailable. This observation never replaces immutable resource state.
	existing := false
	if err := s.repository.View(ctx, func(l policyLedger) error {
		if err := s.authorized(ctx); err != nil {
			return err
		}
		if err := s.scoped(l); err != nil {
			return err
		}
		_, existing = l.Execution.RegistrationOperations[q.OperationID]
		return nil
	}); err != nil {
		return result, err
	}
	if existing {
		err := s.update(ctx, func(l *policyLedger, _ string) (bool, error) {
			receipt, ok := l.Execution.RegistrationOperations[q.OperationID]
			if !ok {
				return false, executionFailure("authority-unavailable")
			}
			if receipt.RequestDigest != digest {
				return false, executionFailure("binding-conflict")
			}
			r := l.Execution.Resources[receipt.ResourceID]
			result = policyExecutionRegisteredResult{"recovered", r.Content.Resource, r.Registration}
			return false, nil
		})
		return result, err
	}
	if s.resolveRelease == nil || s.resolveAdoption == nil {
		return result, executionFailure("authority-unavailable")
	}
	release, err := s.resolveRelease(ctx, q.JobID)
	if err != nil {
		return result, err
	}
	material, err := validateExecutionRelease(release, s.identity)
	if err != nil {
		return result, err
	}
	if release.Resource.JobID != q.JobID {
		return result, executionFailure("resource-changed")
	}
	ref, err := s.artifacts.Put(ctx, release.Bytes)
	if err != nil {
		return result, policyUncertainStorage()
	}
	if ref != artifactReference(release.Bytes) {
		return result, policyUncertainStorage()
	}
	err = s.update(ctx, func(l *policyLedger, now string) (bool, error) {
		e := l.Execution
		if old, ok := e.RegistrationOperations[q.OperationID]; ok {
			if old.RequestDigest != digest {
				return false, executionFailure("binding-conflict")
			}
			r := e.Resources[old.ResourceID]
			result = policyExecutionRegisteredResult{"recovered", r.Content.Resource, r.Registration}
			return false, nil
		}
		id := executionResourceID(s.identity, q.JobID)
		if _, ok := e.Resources[id]; ok {
			return false, executionFailure("resource-changed")
		}
		if len(e.Resources) >= policyExecutionResourceLimit || len(e.RegistrationOperations) >= policyOperationLimit {
			return false, executionFailure("capacity")
		}
		adoption, err := s.resolveAdoption(ctx, *l, q.AdoptionID, q.ReviewID)
		if err != nil {
			return false, executionFailure("authority-unavailable")
		}
		if !s.retainedAdoption(ctx, *l, adoption) {
			return false, executionFailure("binding-conflict")
		}
		if adoption.ID != q.AdoptionID || adoption.Review.ID != q.ReviewID || adoption.Content.Basis.Pack.Content != artifactReference(material.pack) || adoption.Content.Basis.Pack.ArtifactID != material.packID || adoption.Content.Basis.Pack.Version != material.packVersion {
			return false, executionFailure("binding-conflict")
		}
		content := policyExecutionRegistrationContent{id, release.Resource, ref, artifactReference(material.pack), material.packID, material.packVersion, material.runtimeDigest, adoption, policyExecutionLifecycle, executionCaller(s.identity), now}
		r := policyExecutionRegisteredResource{policyExecutionRegistration{"1", executionHash(content)}, content}
		if reason := s.eligible(ctx, *l, r, now); reason != "eligible" {
			return false, executionFailure("eligibility-blocked")
		}
		e.Resources[id] = r
		e.RegistrationOperations[q.OperationID] = policyExecutionRegistrationReceipt{digest, id}
		result = policyExecutionRegisteredResult{"committed", r.Content.Resource, r.Registration}
		return true, nil
	})
	return result, err
}

func (s policyExecutionService) eligible(ctx context.Context, l policyLedger, r policyExecutionRegisteredResource, now string) string {
	c := r.Content
	if !s.retainedAdoption(ctx, l, c.Adoption) {
		return "dependency-unavailable"
	}
	if raw, err := s.artifacts.Get(ctx, c.Release); err != nil || verifyRetainedArtifact(c.Release, raw) != nil {
		return "dependency-unavailable"
	}
	a := l.Adoptions[c.Adoption.ID]
	if a.Recovery != nil {
		return "authority-unavailable"
	}
	for _, review := range a.Reviews {
		if review.Sequence > c.Adoption.Review.Sequence && review.Decision == "request-changes" {
			return "adoption-changes-requested"
		}
	}
	for _, dependency := range c.Adoption.Content.Basis.Dependencies {
		p, ok := l.Policies[dependency.Selection.PolicyID]
		if !ok {
			return "dependency-unavailable"
		}
		owner, boundary := policyAuthorityOwnership(l.SourceAuthorities[p.ID])
		boundary = policyRecoveryReviewBoundary(p, boundary)
		if owner.Mode != "desk-managed" {
			return "authority-unavailable"
		}
		found := false
		for _, publication := range p.Publications {
			if publication.ID == dependency.PolicyPublicationID {
				if publication.Sequence <= boundary {
					return "authority-unavailable"
				}
				found = true
			}
		}
		if !found {
			return "dependency-unavailable"
		}
		effective := false
		for _, view := range policyLifecycleView(p, l.Lifecycles[p.ID], now, l.SourceAuthorities[p.ID]).Releases {
			if view.PublicationID == dependency.PolicyPublicationID {
				switch view.Status {
				case "effective":
					effective = true
				case "withdrawn", "expired", "superseded":
					return view.Status
				default:
					return "not-effective"
				}
			}
		}
		if !effective {
			return "not-effective"
		}
	}
	return "eligible"
}
func (s policyExecutionService) retainedAdoption(ctx context.Context, l policyLedger, a policyExecutionAdoption) bool {
	if !executionAdoptionSnapshot(l, a) {
		return false
	}
	raw, err := s.artifacts.Get(ctx, a.ContentRef)
	if err != nil || verifyRetainedArtifact(a.ContentRef, raw) != nil {
		return false
	}
	var content policyAdoptionContent
	if coverageDecode(raw, &content) != nil || !samePolicyStatusValue(content, a.Content) {
		return false
	}
	return archivedAdoptionContent(ctx, l, s.artifacts, l.Adoptions[a.ID], content, func(retainedArtifactRef) error { return nil }) == nil
}

func (s policyExecutionService) prepareIntent(ctx context.Context, resource policyExecutionResource, registration policyExecutionRegistration, key string, raw []byte) (policyExecutionIntent, string, error) {
	var result policyExecutionIntent
	delivery := ""
	if !resource.valid() || !registration.valid() || !executionKey(key) || len(raw) == 0 || len(raw) > 2<<20 || !utf8.Valid(raw) || !json.Valid(raw) || jsonscalar.ValidateEscapes(raw) != nil || rejectDuplicateMembers(raw) != nil {
		return result, delivery, executionFailure("invalid-request")
	}
	var body map[string]json.RawMessage
	if json.Unmarshal(raw, &body) != nil || body == nil || body["facts"] == nil {
		return result, delivery, executionFailure("invalid-request")
	}
	for name := range body {
		if name != "facts" && name != "evidence" {
			return result, delivery, executionFailure("unsupported-profile")
		}
	}
	if err := s.authorized(ctx); err != nil {
		return result, delivery, err
	}
	put := func(value []byte) (retainedArtifactRef, error) {
		ref, err := s.artifacts.Put(ctx, value)
		if err != nil || ref != artifactReference(value) {
			return ref, policyUncertainStorage()
		}
		return ref, nil
	}
	request, err := put(raw)
	if err != nil {
		return result, delivery, err
	}
	facts, err := put(body["facts"])
	if err != nil {
		return result, delivery, err
	}
	var evidence *retainedArtifactRef
	if value, ok := body["evidence"]; ok {
		ref, err := put(value)
		if err != nil {
			return result, delivery, err
		}
		evidence = &ref
	}
	err = s.update(ctx, func(l *policyLedger, now string) (bool, error) {
		e := l.Execution
		id := executionIntentID(s.identity, resource.JobID, key)
		rid := executionResourceID(s.identity, resource.JobID)
		candidate := policyExecutionIntent{id, rid, executionCaller(s.identity), key, resource, registration, request, facts, evidence, now}
		if old, ok := e.Intents[id]; ok {
			candidate.At = old.At
			if !samePolicyStatusValue(old, candidate) {
				return false, executionFailure("binding-conflict")
			}
			result = old
			delivery = "recovered"
			return false, nil
		}
		r, ok := e.Resources[rid]
		if !ok {
			return false, executionFailure("resource-unregistered")
		}
		if r.Content.Resource != resource || r.Registration != registration {
			return false, executionFailure("resource-changed")
		}
		if len(e.Intents) >= policyExecutionIntentLimit {
			return false, executionFailure("capacity")
		}
		if reason := s.eligible(ctx, *l, r, now); reason != "eligible" {
			return false, executionFailure("eligibility-blocked")
		}
		e.Intents[id] = candidate
		result = candidate
		delivery = "committed"
		return true, nil
	})
	return result, delivery, err
}

func (s policyExecutionService) session(c policyExecutionSession) error {
	if c.identity != s.identity || !retainedArtifactDigest.MatchString(c.id) || !retainedArtifactDigest.MatchString(c.challenge) || c.owned == nil {
		return executionFailure("unauthorized")
	}
	if c.owned() != nil {
		return executionFailure("authority-unavailable")
	}
	return nil
}
func (s policyExecutionService) active(e *policyExecutionState, c policyExecutionSession) (policyExecutionEpoch, error) {
	if err := s.session(c); err != nil {
		return policyExecutionEpoch{}, err
	}
	epoch := e.Epochs[e.CurrentEpoch]
	if epoch.SessionID != c.id || epoch.Challenge != c.challenge {
		return epoch, executionFailure("stale-epoch")
	}
	return epoch, nil
}
func (s policyExecutionService) activate(ctx context.Context, c policyExecutionSession, q policyExecutionActivateRequest) (policyExecutionActivateResult, error) {
	var result policyExecutionActivateResult
	if err := s.session(c); err != nil {
		return result, err
	}
	if q.Challenge != c.challenge || !q.Signing.valid() {
		return result, executionFailure("invalid-request")
	}
	digest := executionRequestDigest("activate", q)
	err := s.update(ctx, func(l *policyLedger, now string) (bool, error) {
		if err := s.session(c); err != nil {
			return false, err
		}
		e := l.Execution
		for _, epoch := range e.Epochs {
			if epoch.SessionID == c.id {
				if epoch.Epoch != e.CurrentEpoch {
					return false, executionFailure("stale-epoch")
				}
				if epoch.RequestDigest != digest || epoch.Challenge != c.challenge {
					return false, executionFailure("binding-conflict")
				}
				result = policyExecutionActivateResult{"recovered", epoch.Epoch, digest}
				return false, nil
			}
		}
		if len(e.Epochs) >= policyExecutionEpochLimit {
			return false, executionFailure("capacity")
		}
		epoch := strconv.Itoa(len(e.Epochs) + 1)
		e.Epochs[epoch] = policyExecutionEpoch{epoch, c.id, c.challenge, q.Signing, digest, now}
		e.CurrentEpoch = epoch
		result = policyExecutionActivateResult{"committed", epoch, digest}
		return true, nil
	})
	if err != nil {
		return policyExecutionActivateResult{}, err
	}
	return result, nil
}

type policyExecutionExactBytes struct {
	Encoding string `json:"encoding"`
	Digest   string `json:"digest"`
	Bytes    int    `json:"bytes"`
}
type policyExecutionEvidence struct {
	Presence string                     `json:"presence"`
	Value    *policyExecutionExactBytes `json:"value,omitempty"`
}
type policyExecutionDescriptor struct {
	Profile       string                    `json:"profile"`
	ReleaseDigest string                    `json:"releaseDigest"`
	PackDigest    string                    `json:"packDigest"`
	RuntimeDigest string                    `json:"runtimeDigest"`
	Facts         policyExecutionExactBytes `json:"facts"`
	Evidence      policyExecutionEvidence   `json:"evidence"`
	Signing       policyExecutionSigning    `json:"signing"`
}

func executionPayloadShape(p policyExecutionPayload) bool {
	if p.Profile != policyExecutionPayloadProfile || len(p.Bytes) > 5464 || !policyExecutionDigest.MatchString(p.Digest) {
		return false
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(p.Bytes)
	if err != nil || len(raw) == 0 || len(raw) > 4096 || base64.StdEncoding.EncodeToString(raw) != p.Bytes || p.Digest != "sha256:"+digestOf(raw) {
		return false
	}
	var d policyExecutionDescriptor
	if executionDecode(raw, &d) != nil || d.Profile != p.Profile || !d.Signing.valid() {
		return false
	}
	canon, err := gatewayCanonicalJSON(context.Background(), raw)
	return err == nil && bytes.Equal(canon, raw)
}
func (s policyExecutionService) payload(ctx context.Context, p policyExecutionPayload, r policyExecutionRegisteredResource, i policyExecutionIntent, epoch policyExecutionEpoch) bool {
	if !executionPayloadMatches(p, r, i, epoch) {
		return false
	}
	for _, v := range []retainedArtifactRef{i.Request, i.Facts} {
		data, err := s.artifacts.Get(ctx, v)
		if err != nil || verifyRetainedArtifact(v, data) != nil {
			return false
		}
	}
	if i.Evidence != nil {
		data, err := s.artifacts.Get(ctx, *i.Evidence)
		if err != nil || verifyRetainedArtifact(*i.Evidence, data) != nil {
			return false
		}
	}
	return true
}
func executionPayloadMatches(p policyExecutionPayload, r policyExecutionRegisteredResource, i policyExecutionIntent, epoch policyExecutionEpoch) bool {
	if !executionPayloadShape(p) {
		return false
	}
	raw, _ := base64.StdEncoding.DecodeString(p.Bytes)
	var d policyExecutionDescriptor
	_ = json.Unmarshal(raw, &d)
	ref := func(v retainedArtifactRef) policyExecutionExactBytes {
		return policyExecutionExactBytes{"exact-bytes", "sha256:" + v.SHA256, v.Bytes}
	}
	want := policyExecutionDescriptor{policyExecutionPayloadProfile, r.Content.Resource.ReleaseDigest, "sha256:" + r.Content.Pack.SHA256, r.Content.RuntimeDigest, ref(i.Facts), policyExecutionEvidence{Presence: "omitted"}, epoch.Signing}
	if i.Evidence != nil {
		value := ref(*i.Evidence)
		want.Evidence = policyExecutionEvidence{"supplied", &value}
	}
	if !samePolicyStatusValue(d, want) {
		return false
	}
	return true
}

func (s policyExecutionService) reserve(ctx context.Context, c policyExecutionSession, q policyExecutionReserveRequest) (policyExecutionReserveResult, error) {
	var result policyExecutionReserveResult
	if !policyExecutionID.MatchString(q.IntentID) || !q.Resource.valid() || !q.Registration.valid() || !executionPayloadShape(q.Payload) || !policyExecutionDigest.MatchString(q.RawRequestDigest) {
		return result, executionFailure("invalid-request")
	}
	if err := s.session(c); err != nil {
		return result, err
	}
	err := s.update(ctx, func(l *policyLedger, now string) (bool, error) {
		e := l.Execution
		epoch, err := s.active(e, c)
		if err != nil {
			return false, err
		}
		if old, ok := e.Attempts[q.IntentID]; ok {
			if old.SessionID != c.id || old.Reserved.Epoch != epoch.Epoch {
				return false, executionFailure("operation-held")
			}
			if old.Reserved.RequestDigest != executionRequestDigest("reserve", q) {
				return false, executionFailure("binding-conflict")
			}
			if old.State == "blocked" {
				return false, executionFailure("operation-held")
			}
			// Recovery is observational. Permanently seal an uncommitted reserve
			// so it cannot be continued by a later commitStart call.
			changed := false
			if old.State == "reserved" && old.Hold == nil {
				old.Hold = &policyExecutionHold{"operation-held", now}
				e.Attempts[q.IntentID] = old
				changed = true
			}
			result = old.Reserved
			result.Delivery = "recovered"
			return changed, nil
		}
		i, ok := e.Intents[q.IntentID]
		if !ok {
			return false, executionFailure("intent-unavailable")
		}
		r, ok := e.Resources[i.ResourceID]
		if !ok {
			return false, executionFailure("resource-unregistered")
		}
		if i.Resource != q.Resource || i.Registration != q.Registration || r.Content.Resource != q.Resource || r.Registration != q.Registration || q.RawRequestDigest != "sha256:"+i.Request.SHA256 {
			return false, executionFailure("binding-conflict")
		}
		if !s.payload(ctx, q.Payload, r, i, epoch) {
			return false, executionFailure("binding-conflict")
		}
		if len(e.Attempts) >= policyExecutionIntentLimit {
			return false, executionFailure("capacity")
		}
		op, err := NewToken()
		if err != nil {
			return false, executionFailure("authority-unavailable")
		}
		attempt, err := NewToken()
		if err != nil {
			return false, executionFailure("authority-unavailable")
		}
		result = policyExecutionReserveResult{"committed", "operation_" + op, "attempt_" + attempt, epoch.Epoch, executionRequestDigest("reserve", q)}
		a := policyExecutionAttempt{Reserve: q, Reserved: result, SessionID: c.id, State: "reserved", Reason: "reserved", At: now}
		if reason := s.eligible(ctx, *l, r, now); reason != "eligible" {
			a.State = "blocked"
			a.Reason = reason
			e.Attempts[q.IntentID] = a
			return true, executionFailure("eligibility-blocked")
		}
		e.Attempts[q.IntentID] = a
		return true, nil
	})
	if err != nil {
		return policyExecutionReserveResult{}, err
	}
	return result, nil
}

func (s policyExecutionService) commitStart(ctx context.Context, c policyExecutionSession, q policyExecutionStartRequest) (policyExecutionStartResult, error) {
	var result policyExecutionStartResult
	if !policyExecutionID.MatchString(q.IntentID) || !q.Resource.valid() || !q.Registration.valid() || !executionPayloadShape(q.Payload) || !policyExecutionID.MatchString(q.OperationID) || !policyExecutionID.MatchString(q.AttemptID) || !executionPositive(q.Epoch) || !policyExecutionDigest.MatchString(q.RawRequestDigest) {
		return result, executionFailure("invalid-request")
	}
	if err := s.session(c); err != nil {
		return result, err
	}
	err := s.update(ctx, func(l *policyLedger, now string) (bool, error) {
		e := l.Execution
		epoch, err := s.active(e, c)
		if err != nil {
			return false, err
		}
		if epoch.Epoch != q.Epoch {
			return false, executionFailure("stale-epoch")
		}
		a, ok := e.Attempts[q.IntentID]
		if !ok {
			return false, executionFailure("operation-held")
		}
		if a.SessionID != c.id || a.Reserved.Epoch != q.Epoch || a.Reserved.OperationID != q.OperationID || a.Reserved.AttemptID != q.AttemptID || !samePolicyStatusValue(a.Reserve, q.reserve()) {
			return false, executionFailure("binding-conflict")
		}
		digest := executionRequestDigest("commitStart", q)
		if a.Start != nil {
			if a.Start.RequestDigest != digest {
				return false, executionFailure("binding-conflict")
			}
			result = *a.Start
			result.Delivery = "recovered"
			return false, nil
		}
		if a.State != "reserved" || a.Hold != nil {
			return false, executionFailure("operation-held")
		}
		i := e.Intents[q.IntentID]
		r := e.Resources[i.ResourceID]
		reason := "eligible"
		if !s.payload(ctx, q.Payload, r, i, epoch) {
			reason = "dependency-unavailable"
		} else {
			reason = s.eligible(ctx, *l, r, now)
		}
		decision := "start-committed"
		if reason == "eligible" {
			reason = "start-committed"
		} else {
			decision = "blocked"
		}
		if !retainedArtifactDigest.MatchString(l.authorityRevision) {
			return false, executionFailure("authority-unavailable")
		}
		result = policyExecutionStartResult{"committed", q.OperationID, q.AttemptID, q.Epoch, digest, decision, reason, now, l.authorityRevision}
		a.State = decision
		a.Reason = reason
		a.At = now
		a.Start = &result
		e.Attempts[q.IntentID] = a
		return true, nil
	})
	if err != nil {
		return policyExecutionStartResult{}, err
	}
	return result, nil
}

func (s policyExecutionService) inspect(ctx context.Context, c policyExecutionSession, resource policyExecutionResource) (policyExecutionInspectResult, error) {
	var result policyExecutionInspectResult
	if !resource.valid() {
		return result, executionFailure("invalid-request")
	}
	if err := s.authorized(ctx); err != nil {
		return result, err
	}
	err := s.repository.View(ctx, func(l policyLedger) error {
		if err := s.authorized(ctx); err != nil {
			return err
		}
		if err := s.scoped(l); err != nil {
			return err
		}
		if _, err := s.active(l.Execution, c); err != nil {
			return err
		}
		now, err := s.now()
		if err != nil {
			return err
		}
		if now < l.Execution.LastTime {
			return executionFailure("clock-unavailable")
		}
		r, ok := l.Execution.Resources[executionResourceID(s.identity, resource.JobID)]
		if !ok {
			return executionFailure("resource-unregistered")
		}
		if r.Content.Resource != resource {
			return executionFailure("resource-changed")
		}
		reason := s.eligible(ctx, l, r, now)
		result = policyExecutionInspectResult{"observed", r.Registration, reason == "eligible", reason, now}
		return s.authorized(ctx)
	})
	return result, err
}
func (s policyExecutionService) history(ctx context.Context, c policyExecutionSession, intentID string) (policyExecutionHistoryResult, error) {
	var result policyExecutionHistoryResult
	if !policyExecutionID.MatchString(intentID) {
		return result, executionFailure("invalid-request")
	}
	if err := s.authorized(ctx); err != nil {
		return result, err
	}
	err := s.repository.View(ctx, func(l policyLedger) error {
		if err := s.authorized(ctx); err != nil {
			return err
		}
		if err := s.scoped(l); err != nil {
			return err
		}
		if _, err := s.active(l.Execution, c); err != nil {
			return err
		}
		a, ok := l.Execution.Attempts[intentID]
		if !ok {
			return executionFailure("history-unavailable")
		}
		digest := a.Reserved.RequestDigest
		if a.Start != nil {
			digest = a.Start.RequestDigest
		}
		result = policyExecutionHistoryResult{"observed", intentID, a.Reserved.OperationID, a.Reserved.AttemptID, a.Reserved.Epoch, a.State, digest, a.Reason, a.At}
		if a.Hold != nil {
			result.State = "blocked"
			result.Reason = a.Hold.Reason
			result.At = a.Hold.At
		}
		return s.authorized(ctx)
	})
	return result, err
}

// Keep unexpected resolver/storage failures out of wire refusal text.
func policyExecutionFailureCode(err error) string {
	var f *policyFailure
	if errors.As(err, &f) && contains([]string{"authority-unavailable", "invalid-request", "unauthorized", "stale-epoch", "resource-unregistered", "resource-changed", "unsupported-profile", "intent-unavailable", "binding-conflict", "operation-held", "eligibility-blocked", "capacity", "clock-unavailable", "history-unavailable"}, f.Code) {
		return f.Code
	}
	return "authority-unavailable"
}
