package desk

// Private experimental authority state. Nothing in this file activates a route,
// launches a process, or changes the capabilities advertised by Desk.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/jsonscalar"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const policyExecutionProfile = "local-admin-manual-pack/1"
const policyExecutionLifecycle = "desk-managed-effective/1"
const policyExecutionReleaseProfile = "judgment-pack-runner/canonical/1"
const policyExecutionPayloadProfile = "judgment-pack-runner/execution-payload/1"
const policyExecutionResourceLimit = 256
const policyExecutionIntentLimit = 4096
const policyExecutionEpochLimit = 256

var policyExecutionID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var policyExecutionDigest = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

type policyExecutionIdentity struct {
	InstallationID   string `json:"installationId"`
	MachineWorkspace string `json:"machineWorkspace"`
	RunnerWorkspace  string `json:"runnerWorkspace"`
	ExecutorID       string `json:"executorId"`
}
type policyExecutionResource struct {
	JobID          string `json:"jobId"`
	JobRevision    string `json:"jobRevision"`
	ReleaseID      string `json:"releaseId"`
	ReleaseProfile string `json:"releaseProfile"`
	ReleaseDigest  string `json:"releaseDigest"`
}
type policyExecutionRegistration struct {
	Revision string `json:"revision"`
	Digest   string `json:"digest"`
}
type policyExecutionSigning struct {
	Mode      string `json:"mode"`
	PublicKey string `json:"publicKey,omitempty"`
}
type policyExecutionPayload struct {
	Profile string `json:"profile"`
	Digest  string `json:"digest"`
	Bytes   string `json:"bytes"`
}
type policyExecutionAdoption struct {
	ID          string                `json:"id"`
	CandidateID string                `json:"candidateId"`
	ContentRef  retainedArtifactRef   `json:"contentRef"`
	Content     policyAdoptionContent `json:"content"`
	Review      policyAdoptionReview  `json:"review"`
}
type policyExecutionRegistrationContent struct {
	ResourceID       string                  `json:"resourceId"`
	Resource         policyExecutionResource `json:"resource"`
	Release          retainedArtifactRef     `json:"release"`
	Pack             retainedArtifactRef     `json:"pack"`
	PackID           string                  `json:"packId"`
	PackVersion      string                  `json:"packVersion"`
	RuntimeDigest    string                  `json:"runtimeDigest"`
	Adoption         policyExecutionAdoption `json:"adoption"`
	LifecycleProfile string                  `json:"lifecycleProfile"`
	CallerDomain     string                  `json:"callerDomain"`
	At               string                  `json:"at"`
}
type policyExecutionRegisteredResource struct {
	Registration policyExecutionRegistration        `json:"registration"`
	Content      policyExecutionRegistrationContent `json:"content"`
}
type policyExecutionRegistrationReceipt struct {
	RequestDigest string `json:"requestDigest"`
	ResourceID    string `json:"resourceId"`
}
type policyExecutionIntent struct {
	ID             string                      `json:"id"`
	ResourceID     string                      `json:"resourceId"`
	CallerDomain   string                      `json:"callerDomain"`
	IdempotencyKey string                      `json:"idempotencyKey"`
	Resource       policyExecutionResource     `json:"resource"`
	Registration   policyExecutionRegistration `json:"registration"`
	Request        retainedArtifactRef         `json:"request"`
	Facts          retainedArtifactRef         `json:"facts"`
	Evidence       *retainedArtifactRef        `json:"evidence,omitempty"`
	At             string                      `json:"at"`
}
type policyExecutionEpoch struct {
	Epoch         string                 `json:"epoch"`
	SessionID     string                 `json:"sessionId"`
	Challenge     string                 `json:"challenge"`
	Signing       policyExecutionSigning `json:"signing"`
	RequestDigest string                 `json:"requestDigest"`
	At            string                 `json:"at"`
}
type policyExecutionReserveRequest struct {
	IntentID         string                      `json:"intentId"`
	Resource         policyExecutionResource     `json:"resource"`
	Registration     policyExecutionRegistration `json:"registration"`
	Payload          policyExecutionPayload      `json:"payload"`
	RawRequestDigest string                      `json:"rawRequestDigest"`
}
type policyExecutionStartRequest struct {
	IntentID         string                      `json:"intentId"`
	Resource         policyExecutionResource     `json:"resource"`
	Registration     policyExecutionRegistration `json:"registration"`
	Payload          policyExecutionPayload      `json:"payload"`
	OperationID      string                      `json:"operationId"`
	AttemptID        string                      `json:"attemptId"`
	Epoch            string                      `json:"epoch"`
	RawRequestDigest string                      `json:"rawRequestDigest"`
}

func (q policyExecutionStartRequest) reserve() policyExecutionReserveRequest {
	return policyExecutionReserveRequest{q.IntentID, q.Resource, q.Registration, q.Payload, q.RawRequestDigest}
}

type policyExecutionReserveResult struct {
	Delivery      string `json:"delivery"`
	OperationID   string `json:"operationId"`
	AttemptID     string `json:"attemptId"`
	Epoch         string `json:"epoch"`
	RequestDigest string `json:"requestDigest"`
}
type policyExecutionStartResult struct {
	Delivery          string `json:"delivery"`
	OperationID       string `json:"operationId"`
	AttemptID         string `json:"attemptId"`
	Epoch             string `json:"epoch"`
	RequestDigest     string `json:"requestDigest"`
	Decision          string `json:"decision"`
	Reason            string `json:"reason"`
	At                string `json:"at"`
	AuthorityRevision string `json:"authorityRevision"`
}
type policyExecutionAttempt struct {
	Reserve   policyExecutionReserveRequest `json:"reserve"`
	Reserved  policyExecutionReserveResult  `json:"reserved"`
	SessionID string                        `json:"sessionId"`
	State     string                        `json:"state"`
	Reason    string                        `json:"reason"`
	At        string                        `json:"at"`
	Start     *policyExecutionStartResult   `json:"start,omitempty"`
	Hold      *policyExecutionHold          `json:"hold,omitempty"`
}
type policyExecutionHold struct {
	Reason string `json:"reason"`
	At     string `json:"at"`
}
type policyExecutionState struct {
	Version                int                                           `json:"version"`
	Profile                string                                        `json:"profile"`
	Identity               policyExecutionIdentity                       `json:"identity"`
	LastTime               string                                        `json:"lastTime"`
	CurrentEpoch           string                                        `json:"currentEpoch"`
	Resources              map[string]policyExecutionRegisteredResource  `json:"resources"`
	RegistrationOperations map[string]policyExecutionRegistrationReceipt `json:"registrationOperations"`
	Intents                map[string]policyExecutionIntent              `json:"intents"`
	Epochs                 map[string]policyExecutionEpoch               `json:"epochs"`
	Attempts               map[string]policyExecutionAttempt             `json:"attempts"`
}

func policyExecutionArchiveUnsupported() error {
	return policyRefusal(409, "archive-unsupported", "Execution authority cannot be archived or restored by the schema 2 recovery format.")
}
func executionFailure(code string) error {
	return policyRefusal(409, code, "Manual execution authority refused this operation ("+code+").")
}
func executionPositive(s string) bool {
	n, e := strconv.ParseUint(s, 10, 64)
	return e == nil && n > 0 && strconv.FormatUint(n, 10) == s
}
func executionTime(s string) bool {
	if len(s) != 20 {
		return false
	}
	t, err := time.Parse("2006-01-02T15:04:05Z", s)
	return err == nil && t.Year() >= 1970 && t.Format("2006-01-02T15:04:05Z") == s
}
func executionWorkspace(s string) bool {
	if len(s) < 1 || len(s) > 256 {
		return false
	}
	for _, c := range []byte(s) {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}
func (v policyExecutionIdentity) valid() bool {
	return retainedArtifactDigest.MatchString(v.InstallationID) && retainedArtifactDigest.MatchString(v.MachineWorkspace) && retainedArtifactDigest.MatchString(v.ExecutorID) && executionWorkspace(v.RunnerWorkspace)
}
func (v policyExecutionResource) valid() bool {
	return policyExecutionID.MatchString(v.JobID) && policyExecutionID.MatchString(v.ReleaseID) && executionPositive(v.JobRevision) && v.ReleaseProfile == policyExecutionReleaseProfile && policyExecutionDigest.MatchString(v.ReleaseDigest)
}
func (v policyExecutionRegistration) valid() bool {
	return v.Revision == "1" && policyExecutionDigest.MatchString(v.Digest)
}
func (v policyExecutionSigning) valid() bool {
	return v.Mode == "off" && v.PublicKey == "" || v.Mode == "ed25519" && retainedArtifactDigest.MatchString(v.PublicKey)
}
func (v *policyExecutionSigning) UnmarshalJSON(raw []byte) error {
	type plain policyExecutionSigning
	var value plain
	if executionDecode(raw, &value) != nil || !policyExecutionSigning(value).valid() {
		return errors.New("execution signing")
	}
	var members map[string]json.RawMessage
	_ = json.Unmarshal(raw, &members)
	_, present := members["publicKey"]
	if value.Mode == "off" && present || value.Mode == "ed25519" && !present {
		return errors.New("execution signing members")
	}
	*v = policyExecutionSigning(value)
	return nil
}
func executionHash(v any) string { raw, _ := json.Marshal(v); return "sha256:" + digestOf(raw) }
func executionRequestDigest(method string, body any) string {
	raw, _ := json.Marshal(struct {
		Method string `json:"method"`
		Body   any    `json:"body"`
	}{method, body})
	canonical, err := gatewayCanonicalJSON(context.Background(), raw)
	if err != nil {
		return ""
	}
	return "sha256:" + digestOf(canonical)
}
func executionResourceID(i policyExecutionIdentity, job string) string {
	return digestOf([]byte(i.InstallationID + "\x00" + i.MachineWorkspace + "\x00" + i.RunnerWorkspace + "\x00" + job))
}
func executionCaller(i policyExecutionIdentity) string { return "local-admin:" + i.InstallationID }
func executionIntentID(i policyExecutionIdentity, job, key string) string {
	return "intent_" + digestOf([]byte(executionResourceID(i, job)+"\x00"+executionCaller(i)+"\x00"+key))
}

// Strict member/required/null checking applies to new execution envelopes only;
// existing schema 2 optional historical fields retain their compatibility.
func executionShape(raw []byte, typ reflect.Type) error {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return errors.New("execution null")
	}
	if typ == reflect.TypeFor[json.RawMessage]() {
		return nil
	}
	switch typ.Kind() {
	case reflect.Struct:
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || fields == nil {
			return errors.New("execution object")
		}
		known := map[string]reflect.StructField{}
		for n := 0; n < typ.NumField(); n++ {
			f := typ.Field(n)
			if f.PkgPath != "" {
				continue
			}
			tag := strings.Split(f.Tag.Get("json"), ",")
			if tag[0] == "-" {
				continue
			}
			name := tag[0]
			if name == "" {
				name = f.Name
			}
			known[name] = f
			if !strings.Contains(f.Tag.Get("json"), ",omitempty") && fields[name] == nil {
				return errors.New("execution missing member")
			}
		}
		for name, value := range fields {
			f, ok := known[name]
			if !ok {
				return errors.New("execution unknown member")
			}
			if err := executionShape(value, f.Type); err != nil {
				return err
			}
		}
	case reflect.Map:
		var values map[string]json.RawMessage
		if json.Unmarshal(raw, &values) != nil || values == nil {
			return errors.New("execution map")
		}
		for _, v := range values {
			if err := executionShape(v, typ.Elem()); err != nil {
				return err
			}
		}
	case reflect.Slice:
		var values []json.RawMessage
		if json.Unmarshal(raw, &values) != nil || values == nil {
			return errors.New("execution array")
		}
		for _, v := range values {
			if err := executionShape(v, typ.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}
func executionDecode(raw []byte, out any) error {
	if !utf8.Valid(raw) || jsonscalar.ValidateEscapes(raw) != nil || rejectDuplicateMembers(raw) != nil || decodeDataJSON(raw, out) != nil {
		return errors.New("execution JSON")
	}
	return executionShape(raw, reflect.TypeOf(out))
}
func decodePolicyLedger(raw []byte) (policyLedger, error) {
	var l policyLedger
	if len(raw) > policyLedgerLimit || !utf8.Valid(raw) || rejectDuplicateMembers(raw) != nil || decodeDataJSON(raw, &l) != nil {
		return l, errors.New("policy storage integrity failed")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return l, errors.New("policy storage integrity failed")
	}
	// Exact top-level members also prevent case-insensitive execution aliases.
	if coverageExactMembers(raw, reflect.TypeFor[policyLedger]()) != nil {
		return l, errors.New("policy storage integrity failed")
	}
	if execution, ok := fields["execution"]; ok {
		if l.SchemaVersion != 3 || jsonscalar.ValidateEscapes(execution) != nil || executionShape(execution, reflect.TypeFor[policyExecutionState]()) != nil {
			return l, errors.New("policy execution integrity failed")
		}
	}
	if validatePolicyLedger(l) != nil {
		return l, errors.New("policy storage integrity failed")
	}
	l.authorityRevision = digestOf(raw)
	return l, nil
}

func validatePolicyExecution(l policyLedger) error {
	bad := errors.New("policy execution integrity failed")
	e := l.Execution
	if l.SchemaVersion == 2 {
		if e != nil {
			return bad
		}
		return nil
	}
	if l.SchemaVersion != 3 || e == nil || e.Version != 1 || e.Profile != policyExecutionProfile || !e.Identity.valid() || !executionTime(e.LastTime) || e.Resources == nil || e.RegistrationOperations == nil || e.Intents == nil || e.Epochs == nil || e.Attempts == nil || len(e.Resources) > policyExecutionResourceLimit || len(e.RegistrationOperations) > policyOperationLimit || len(e.Intents) > policyExecutionIntentLimit || len(e.Epochs) > policyExecutionEpochLimit || len(e.Attempts) > policyExecutionIntentLimit {
		return bad
	}
	if e.CurrentEpoch != "" {
		if !executionPositive(e.CurrentEpoch) || e.Epochs[e.CurrentEpoch].Epoch != e.CurrentEpoch {
			return bad
		}
	}
	for id, r := range e.Resources {
		c := r.Content
		a := c.Adoption
		if id != executionResourceID(e.Identity, c.Resource.JobID) || c.ResourceID != id || !c.Resource.valid() || !r.Registration.valid() || r.Registration.Digest != executionHash(c) || !c.Release.valid() || c.Release.Bytes > 8<<20 || c.Resource.ReleaseDigest != "sha256:"+c.Release.SHA256 || !c.Pack.valid() || c.Pack != a.Content.Basis.Pack.Content || c.PackID != a.Content.Basis.Pack.ArtifactID || c.PackVersion != a.Content.Basis.Pack.Version || !policyExecutionDigest.MatchString(c.RuntimeDigest) || c.LifecycleProfile != policyExecutionLifecycle || c.CallerDomain != executionCaller(e.Identity) || !executionTime(c.At) || c.At > e.LastTime || !policyAdoptionIDForm.MatchString(a.ID) || !policyAdoptionCandidateIDForm.MatchString(a.CandidateID) || !a.ContentRef.valid() || a.Review.Decision != "approve" || a.Review.CandidateID != a.CandidateID || a.Review.CandidateDigest != a.ContentRef.SHA256 || a.Review.BasisDigest != a.Content.BasisDigest || a.Review.Check == nil || !a.Review.Check.valid() || len(a.Content.Basis.Dependencies) < 1 || len(a.Content.Basis.Dependencies) > policyAdoptionSetLimit {
			return bad
		}
		for _, d := range a.Content.Basis.Dependencies {
			if !validAdoptionSelection(d.Selection) || d.Source.PolicyID != d.Selection.PolicyID || d.Source.PublicationID != d.PolicyPublicationID {
				return bad
			}
		}
		if !executionAdoptionSnapshot(l, a) {
			return bad
		}
	}
	for id, r := range e.RegistrationOperations {
		if !policyOperationIDForm.MatchString(id) || !policyExecutionDigest.MatchString(r.RequestDigest) || e.Resources[r.ResourceID].Content.ResourceID != r.ResourceID {
			return bad
		}
	}
	for id, i := range e.Intents {
		r, ok := e.Resources[i.ResourceID]
		if !ok || id != i.ID || id != executionIntentID(e.Identity, i.Resource.JobID, i.IdempotencyKey) || !executionKey(i.IdempotencyKey) || i.CallerDomain != executionCaller(e.Identity) || i.Resource != r.Content.Resource || i.Registration != r.Registration || !i.Request.valid() || !i.Facts.valid() || i.Request.Bytes > 2<<20 || i.Facts.Bytes > 2<<20 || i.Evidence != nil && (!i.Evidence.valid() || i.Evidence.Bytes > 2<<20) || !executionTime(i.At) || i.At > e.LastTime {
			return bad
		}
	}
	sessions := map[string]bool{}
	for id, v := range e.Epochs {
		if id != v.Epoch || !executionPositive(id) || !retainedArtifactDigest.MatchString(v.SessionID) || sessions[v.SessionID] || !retainedArtifactDigest.MatchString(v.Challenge) || !v.Signing.valid() || !executionTime(v.At) || v.At > e.LastTime || v.RequestDigest != executionRequestDigest("activate", policyExecutionActivateRequest{v.Challenge, v.Signing}) {
			return bad
		}
		sessions[v.SessionID] = true
	}
	if len(e.Epochs) > 0 && e.CurrentEpoch != strconv.Itoa(len(e.Epochs)) {
		return bad
	}
	for n := 1; n <= len(e.Epochs); n++ {
		if e.Epochs[strconv.Itoa(n)].Epoch == "" {
			return bad
		}
	}
	ops, attempts := map[string]bool{}, map[string]bool{}
	for id, a := range e.Attempts {
		i, ok := e.Intents[id]
		if a.Reserve.RawRequestDigest != "sha256:"+i.Request.SHA256 {
			return bad
		}
		ep := e.Epochs[a.Reserved.Epoch]
		if !executionPositive(a.Reserved.Epoch) || ep.Epoch != a.Reserved.Epoch || !retainedArtifactDigest.MatchString(a.SessionID) || !executionPayloadMatches(a.Reserve.Payload, e.Resources[i.ResourceID], i, ep) {
			return bad
		}
		if !ok || a.Reserve.IntentID != id || a.Reserve.Resource != i.Resource || a.Reserve.Registration != i.Registration || !policyExecutionID.MatchString(a.Reserved.OperationID) || !policyExecutionID.MatchString(a.Reserved.AttemptID) || ops[a.Reserved.OperationID] || attempts[a.Reserved.AttemptID] || a.Reserved.Delivery != "committed" || a.Reserved.RequestDigest != executionRequestDigest("reserve", a.Reserve) || ep.SessionID != a.SessionID || !executionTime(a.At) || a.At > e.LastTime || !executionPayloadShape(a.Reserve.Payload) || !contains([]string{"reserved", "start-committed", "blocked"}, a.State) {
			return bad
		}
		ops[a.Reserved.OperationID] = true
		attempts[a.Reserved.AttemptID] = true
		if a.Hold != nil && (a.State != "reserved" || a.Start != nil || a.Hold.Reason != "operation-held" || !executionTime(a.Hold.At) || a.Hold.At < a.At || a.Hold.At > e.LastTime) {
			return bad
		}
		if a.Start == nil {
			if a.State == "reserved" && a.Reason != "reserved" || a.State == "blocked" && !executionBlockedReason(a.Reason) || a.State == "start-committed" {
				return bad
			}
			continue
		}
		s := a.Start
		q := policyExecutionStartRequest{a.Reserve.IntentID, a.Reserve.Resource, a.Reserve.Registration, a.Reserve.Payload, a.Reserved.OperationID, a.Reserved.AttemptID, a.Reserved.Epoch, a.Reserve.RawRequestDigest}
		if s.Delivery != "committed" || s.OperationID != a.Reserved.OperationID || s.AttemptID != a.Reserved.AttemptID || s.Epoch != a.Reserved.Epoch || s.RequestDigest != executionRequestDigest("commitStart", q) || s.Decision != a.State || s.Reason != a.Reason || s.At != a.At || !retainedArtifactDigest.MatchString(s.AuthorityRevision) || s.Decision == "start-committed" && s.Reason != "start-committed" || s.Decision == "blocked" && !executionBlockedReason(s.Reason) || s.Decision == "reserved" {
			return bad
		}
	}
	return nil
}
func executionKey(s string) bool {
	if len(s) < 1 || len(s) > 128 {
		return false
	}
	for _, c := range []byte(s) {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}
func executionBlockedReason(s string) bool {
	return contains([]string{"resource-changed", "unsupported-profile", "withdrawn", "not-effective", "expired", "superseded", "adoption-changes-requested", "dependency-unavailable", "authority-unavailable", "clock-unavailable", "operation-held"}, s)
}
func executionAdoptionSnapshot(l policyLedger, a policyExecutionAdoption) bool {
	r, ok := l.Adoptions[a.ID]
	if !ok || r.CandidateID != a.CandidateID || r.Content != a.ContentRef {
		return false
	}
	found := false
	for _, v := range r.Reviews {
		if samePolicyStatusValue(v, a.Review) {
			found = true
		}
	}
	if !found {
		return false
	}
	c := a.Content
	if c.SchemaVersion != 1 || c.FormatVersion != policyAdoptionFormat || c.PortableID != "urn:desk:adoption-candidate:"+a.CandidateID || c.BasisDigest != adoptionBasisDigest(c.Basis) || !validCoveragePackSelector(c.Basis.Pack) || !c.Document.valid() || !c.Check.valid() || c.Materials == nil || len(c.Materials) > 16 {
		return false
	}
	seen := map[string]bool{}
	for _, d := range c.Basis.Dependencies {
		if seen[d.Selection.PolicyID] || !validAdoptionSelection(d.Selection) || d.CandidateDigest != d.CoverageContent.SHA256 || !d.CoverageContent.valid() || d.CoverageReview.ID != d.Selection.ReviewID || d.CoverageReview.CandidateID != d.Selection.CandidateID || d.CoverageReview.CandidateDigest != d.CandidateDigest || d.CoverageReview.Decision != "approve" || d.CoverageReview.Check == nil || d.CoverageReview.PolicyRevision != d.PolicyRevision || d.Statement.validate() != nil || d.Scope.Kind != "whole-narrative" || d.Statement.Status == "awaiting-interpretation" {
			return false
		}
		seen[d.Selection.PolicyID] = true
	}
	return true
}
