package desk

// Coverage is private reviewed provenance for exact retained publications. It
// neither registers governing adoption nor grants execution/publication authority.
import (
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"unicode/utf8"
)

const policyCoverageCountLimit = 256
const policyCoverageTextLimit = 4096
const policyCoverageCandidateLimit = 2 << 20

var policyCoverageIDForm = regexp.MustCompile(`^coverage_[a-f0-9]{48}$`)
var policyCoverageCandidateIDForm = regexp.MustCompile(`^coverage_candidate_[a-f0-9]{48}$`)
var policyCoverageReviewIDForm = regexp.MustCompile(`^coverage_review_[a-f0-9]{48}$`)

type policyCoverageElement struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`
}
type policyCoverageStatement struct {
	Status           string                  `json:"status"`
	ConsumerElements []policyCoverageElement `json:"consumerElements,omitempty"`
	Basis            string                  `json:"basis,omitempty"`
	Handoff          string                  `json:"handoff,omitempty"`
	Explanation      string                  `json:"explanation,omitempty"`
	Rationale        string                  `json:"rationale,omitempty"`
}
type policyCoverageScope struct {
	Kind string `json:"kind"`
}
type policyCoveragePackSelector struct {
	PublicationID string              `json:"publicationId"`
	ArtifactID    string              `json:"artifactId"`
	Version       string              `json:"version"`
	Content       retainedArtifactRef `json:"content"`
}
type policyCoverageInventory struct {
	Rules         []string `json:"rules"`
	Exceptions    []string `json:"exceptions"`
	Applicability bool     `json:"applicability"`
	Fallback      bool     `json:"fallback"`
}
type policyCoveragePackChoice struct {
	PublicationID string              `json:"publicationId"`
	ArtifactID    string              `json:"artifactId"`
	Version       string              `json:"version"`
	Content       retainedArtifactRef `json:"content"`
	Alias         string              `json:"alias,omitempty"`
}

type policyCoveragePublicationChoice struct {
	PublicationID   string              `json:"publicationId"`
	RevisionID      string              `json:"revisionId"`
	CandidateDigest string              `json:"candidateDigest"`
	ArtifactVersion string              `json:"artifactVersion"`
	Title           string              `json:"title"`
	Content         retainedArtifactRef `json:"content"`
	At              string              `json:"at"`
}
type policyCoveragePack struct {
	PublicationID string            `json:"publicationId"`
	Document      publishedDocument `json:"document"`
}
type policyCoverageContent struct {
	SchemaVersion int                     `json:"schemaVersion"`
	Source        policyAuthoringSource   `json:"source"`
	Pack          policyCoveragePack      `json:"pack"`
	Publication   artifactPublication     `json:"publication"`
	Scope         policyCoverageScope     `json:"scope"`
	Statement     policyCoverageStatement `json:"statement"`
}
type policyCoverageCandidate struct {
	ID        string                  `json:"id"`
	Digest    string                  `json:"digest"`
	Content   retainedArtifactRef     `json:"content"`
	Source    policyAuthoringSource   `json:"source"`
	Pack      policyCoveragePack      `json:"pack"`
	Scope     policyCoverageScope     `json:"scope"`
	Statement policyCoverageStatement `json:"statement"`
}
type policyCoverageCandidateMetadata struct {
	Status              string                     `json:"status"`
	ID                  string                     `json:"id"`
	Sequence            int                        `json:"sequence"`
	Digest              string                     `json:"digest"`
	Content             retainedArtifactRef        `json:"content"`
	PolicyPublicationID string                     `json:"policyPublicationId"`
	Pack                policyCoveragePackSelector `json:"pack"`
	At                  string                     `json:"at"`
	Actor               policyActor                `json:"actor"`
}
type policyCoverageReview struct {
	ID              string               `json:"id"`
	Sequence        int                  `json:"sequence"`
	CandidateID     string               `json:"candidateId"`
	CandidateDigest string               `json:"candidateDigest"`
	PolicyRevision  string               `json:"policyRevision"`
	Decision        string               `json:"decision"`
	Comment         string               `json:"comment,omitempty"`
	At              string               `json:"at"`
	Actor           policyActor          `json:"actor"`
	Check           *retainedArtifactRef `json:"check,omitempty"`
}
type policyCoverageRecovery struct {
	SnapshotID string `json:"snapshotId"`
	RestoredAt string `json:"restoredAt"`
	Sequence   int    `json:"sequence"`
}
type policyCoverageRecord struct {
	SchemaVersion   int                               `json:"schemaVersion"`
	ID              string                            `json:"id"`
	PolicyID        string                            `json:"policyId"`
	CreatedAt       string                            `json:"createdAt"`
	UpdatedAt       string                            `json:"updatedAt"`
	HeadCandidateID string                            `json:"headCandidateId"`
	CurrentReviewID string                            `json:"currentReviewId,omitempty"`
	Candidates      []policyCoverageCandidateMetadata `json:"candidates"`
	Reviews         []policyCoverageReview            `json:"reviews"`
	Recovery        *policyCoverageRecovery           `json:"recovery,omitempty"`
}
type policyCoverageDocument struct {
	SchemaVersion   int                               `json:"schemaVersion"`
	ID              string                            `json:"id"`
	PolicyID        string                            `json:"policyId"`
	CreatedAt       string                            `json:"createdAt"`
	UpdatedAt       string                            `json:"updatedAt"`
	Candidate       policyCoverageCandidate           `json:"candidate"`
	ReviewState     string                            `json:"reviewState"`
	CurrentReviewID string                            `json:"currentReviewId,omitempty"`
	Candidates      []policyCoverageCandidateMetadata `json:"candidates"`
	Reviews         []policyCoverageReview            `json:"reviews"`
}
type policyCoverageAvailability struct {
	PackPublication string `json:"packPublication"`
	Reason          string `json:"reason,omitempty"`
}
type policyCoverageReply struct {
	Revision       string                     `json:"revision"`
	PolicyRevision string                     `json:"policyRevision"`
	Document       policyCoverageDocument     `json:"document"`
	Recovery       *policyRecoveryView        `json:"recovery,omitempty"`
	Inventory      policyCoverageInventory    `json:"inventory"`
	Availability   policyCoverageAvailability `json:"availability"`
}
type policyCoverageSummary struct {
	ID                  string                     `json:"id"`
	PolicyID            string                     `json:"policyId"`
	Revision            string                     `json:"revision"`
	CandidateID         string                     `json:"candidateId"`
	CandidateDigest     string                     `json:"candidateDigest"`
	PolicyPublicationID string                     `json:"policyPublicationId"`
	PolicyVersion       string                     `json:"policyVersion"`
	Pack                policyCoveragePackSelector `json:"pack"`
	Status              string                     `json:"status"`
	ReviewState         string                     `json:"reviewState"`
	UpdatedAt           string                     `json:"updatedAt"`
	Recovery            *policyRecoveryView        `json:"recovery,omitempty"`
}
type policyCoverageRequest struct {
	Action              string                      `json:"action"`
	OperationID         string                      `json:"operationId"`
	CoverageID          string                      `json:"coverageId,omitempty"`
	BaseRevision        string                      `json:"baseRevision,omitempty"`
	PolicyPublicationID string                      `json:"policyPublicationId,omitempty"`
	Pack                *policyCoveragePackSelector `json:"pack,omitempty"`
	Scope               *policyCoverageScope        `json:"scope,omitempty"`
	Statement           *policyCoverageStatement    `json:"statement,omitempty"`
	CandidateDigest     string                      `json:"candidateDigest,omitempty"`
	PolicyRevision      string                      `json:"policyRevision,omitempty"`
	Decision            string                      `json:"decision,omitempty"`
	Comment             string                      `json:"comment,omitempty"`
}
type policyCoverageCheckRequest struct {
	BaseRevision    string `json:"baseRevision"`
	CandidateDigest string `json:"candidateDigest"`
	PolicyRevision  string `json:"policyRevision"`
}
type policyCoveragePreparedCheck struct {
	Document  []byte
	Materials [][]byte
}
type policyCoverageRetainedCheck struct {
	SchemaVersion int                      `json:"schemaVersion"`
	Check         policyCoverageCheckReply `json:"check"`
	Document      retainedArtifactRef      `json:"document"`
	Materials     []retainedArtifactRef    `json:"materials"`
}
type policyCoverageCheckReply struct {
	prepared         *policyCoveragePreparedCheck
	CoverageRevision string          `json:"coverageRevision"`
	CandidateDigest  string          `json:"candidateDigest"`
	PolicyRevision   string          `json:"policyRevision"`
	Availability     string          `json:"availability"`
	Report           json.RawMessage `json:"report,omitempty"`
	Reason           string          `json:"reason,omitempty"`
}

// encoding/json accepts case aliases and null scalars; private contracts do not.
func coverageClosedJSON(raw []byte, target any, allowed ...string) error {
	if rejectDuplicateMembers(raw) != nil || decodeDataJSON(raw, target) != nil {
		return errors.New("unsupported coverage fields")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return errors.New("coverage fields must be an object")
	}
	for key, value := range fields {
		if !contains(allowed, key) || string(value) == "null" {
			return errors.New("unsupported coverage field")
		}
	}
	return nil
}
func (v *policyCoverageElement) UnmarshalJSON(raw []byte) error {
	type plain policyCoverageElement
	var p plain
	if coverageClosedJSON(raw, &p, "kind", "id") != nil {
		return errors.New("invalid coverage element")
	}
	if (p.Kind == "rule" || p.Kind == "exception") && (p.ID == "" || len(p.ID) > 4096) || (p.Kind == "applicability" || p.Kind == "fallback") && p.ID != "" || !contains([]string{"rule", "exception", "applicability", "fallback"}, p.Kind) {
		return errors.New("invalid typed coverage element")
	}
	var f map[string]json.RawMessage
	_ = json.Unmarshal(raw, &f)
	if (p.Kind == "applicability" || p.Kind == "fallback") && f["id"] != nil {
		return errors.New("singleton coverage elements have no ID")
	}
	*v = policyCoverageElement(p)
	return nil
}
func (v *policyCoverageStatement) UnmarshalJSON(raw []byte) error {
	type plain policyCoverageStatement
	var p plain
	if coverageClosedJSON(raw, &p, "status", "consumerElements", "basis", "handoff", "explanation", "rationale") != nil {
		return errors.New("invalid coverage declaration")
	}
	allowed := []string{"status"}
	switch p.Status {
	case "implemented":
		allowed = append(allowed, "consumerElements", "basis")
	case "manual":
		allowed = append(allowed, "handoff")
	case "awaiting-interpretation":
		allowed = append(allowed, "explanation")
	case "out-of-scope":
		allowed = append(allowed, "rationale")
	default:
		return errors.New("unsupported coverage declaration")
	}
	var f map[string]json.RawMessage
	_ = json.Unmarshal(raw, &f)
	for key := range f {
		if !contains(allowed, key) {
			return errors.New("unrelated coverage declaration field")
		}
	}
	*v = policyCoverageStatement(p)
	return v.validate()
}
func (v policyCoverageStatement) validate() error {
	bad := errors.New("Provide the required bounded coverage declaration fields.")
	text := ""
	switch v.Status {
	case "implemented":
		text = v.Basis
		if len(v.ConsumerElements) < 1 || len(v.ConsumerElements) > 64 {
			return bad
		}
	case "manual":
		text = v.Handoff
	case "awaiting-interpretation":
		text = v.Explanation
	case "out-of-scope":
		text = v.Rationale
	default:
		return bad
	}
	if strings.TrimSpace(text) == "" || len(text) > policyCoverageTextLimit || !utf8.ValidString(text) {
		return bad
	}
	seen := map[policyCoverageElement]bool{}
	for _, el := range v.ConsumerElements {
		raw, _ := json.Marshal(el)
		var checked policyCoverageElement
		if json.Unmarshal(raw, &checked) != nil || seen[el] {
			return bad
		}
		seen[el] = true
	}
	if v.Status != "implemented" && (len(v.ConsumerElements) > 0 || v.Basis != "") || v.Status != "manual" && v.Handoff != "" || v.Status != "awaiting-interpretation" && v.Explanation != "" || v.Status != "out-of-scope" && v.Rationale != "" {
		return bad
	}
	return nil
}
func (v *policyCoverageScope) UnmarshalJSON(raw []byte) error {
	type plain policyCoverageScope
	var p plain
	if coverageClosedJSON(raw, &p, "kind") != nil || p.Kind != "whole-narrative" {
		return errors.New("Choose whole published narrative scope.")
	}
	*v = policyCoverageScope(p)
	return nil
}
func (v *policyCoveragePackSelector) UnmarshalJSON(raw []byte) error {
	type plain policyCoveragePackSelector
	var p plain
	if coverageClosedJSON(raw, &p, "publicationId", "artifactId", "version", "content") != nil {
		return errors.New("invalid exact Pack selector")
	}
	var f map[string]json.RawMessage
	_ = json.Unmarshal(raw, &f)
	var ref retainedArtifactRef
	if coverageClosedJSON(f["content"], &ref, "sha256", "bytes") != nil || len(f) != 4 || !retainedArtifactDigest.MatchString(p.PublicationID) || p.ArtifactID == "" || len(p.ArtifactID) > 4096 || p.Version == "" || len(p.Version) > policyArtifactVersionLimit || !p.Content.valid() || p.Content.Bytes < 1 {
		return errors.New("Choose an exact retained Pack publication.")
	}
	*v = policyCoveragePackSelector(p)
	return nil
}
func (v *policyCoverageRequest) UnmarshalJSON(raw []byte) error {
	type plain policyCoverageRequest
	var p plain
	if coverageClosedJSON(raw, &p, "action", "operationId", "coverageId", "baseRevision", "policyPublicationId", "pack", "scope", "statement", "candidateDigest", "policyRevision", "decision", "comment") != nil {
		return errors.New("invalid coverage command")
	}
	allowed := []string{"action", "operationId", "coverageId", "baseRevision"}
	switch p.Action {
	case "save":
		allowed = append(allowed, "policyPublicationId", "pack", "scope", "statement")
	case "review":
		allowed = append(allowed, "candidateDigest", "policyRevision", "decision", "comment")
	default:
		return errors.New("invalid coverage action")
	}
	var f map[string]json.RawMessage
	_ = json.Unmarshal(raw, &f)
	for key := range f {
		if !contains(allowed, key) {
			return errors.New("unrelated coverage action field")
		}
	}
	*v = policyCoverageRequest(p)
	return nil
}
func (v *policyCoverageCheckRequest) UnmarshalJSON(raw []byte) error {
	type plain policyCoverageCheckRequest
	var p plain
	if coverageClosedJSON(raw, &p, "baseRevision", "candidateDigest", "policyRevision") != nil {
		return errors.New("invalid exact coverage check")
	}
	*v = policyCoverageCheckRequest(p)
	return nil
}
func coveragePackSelector(p policyCoveragePack) policyCoveragePackSelector {
	return policyCoveragePackSelector{PublicationID: p.PublicationID, ArtifactID: p.Document.ID, Version: p.Document.Version, Content: p.Document.Content}
}
func coverageRevision(record policyCoverageRecord) string {
	raw, _ := json.Marshal(record)
	return digestOf(raw)
}
func coverageHead(record policyCoverageRecord) policyCoverageCandidateMetadata {
	for _, c := range record.Candidates {
		if c.ID == record.HeadCandidateID {
			return c
		}
	}
	return policyCoverageCandidateMetadata{}
}
func coverageReviewState(record policyCoverageRecord, candidateID string) (string, string) {
	for i := len(record.Reviews) - 1; i >= 0; i-- {
		r := record.Reviews[i]
		if r.CandidateID == candidateID && (record.Recovery == nil || r.Sequence > record.Recovery.Sequence) {
			if r.Decision == "approve" {
				return "reviewed", r.ID
			}
			return "changes-requested", r.ID
		}
	}
	return "unreviewed", ""
}
func coverageRecoveryView(record policyCoverageRecord, candidateID string) *policyRecoveryView {
	if record.Recovery == nil {
		return nil
	}
	state, _ := coverageReviewState(record, candidateID)
	return &policyRecoveryView{SnapshotID: record.Recovery.SnapshotID, RestoredAt: record.Recovery.RestoredAt, CurrentnessUnknown: true, FreshReviewRequired: state != "reviewed"}
}

// Check exact JSON member names from the host-owned DTO, including nested
// retained envelopes. This is transport validation, not a prototype schema or
// canonicalizer. Opaque Runtime JSON remains the Runtime boundary's concern.
func coverageExactMembers(raw []byte, kind reflect.Type) error {
	for kind.Kind() == reflect.Pointer {
		kind = kind.Elem()
	}
	if kind == reflect.TypeFor[json.RawMessage]() || kind.Kind() == reflect.Slice && kind.Elem().Kind() == reflect.Uint8 || string(raw) == "null" {
		return nil
	}
	switch kind.Kind() {
	case reflect.Struct:
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || fields == nil {
			return errors.New("coverage object expected")
		}
		known := map[string]reflect.Type{}
		for i := 0; i < kind.NumField(); i++ {
			field := kind.Field(i)
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if name == "-" || field.PkgPath != "" {
				continue
			}
			if name == "" {
				name = field.Name
			}
			known[name] = field.Type
		}
		for key, value := range fields {
			field, ok := known[key]
			if !ok {
				return errors.New("use exact coverage field names")
			}
			if err := coverageExactMembers(value, field); err != nil {
				return err
			}
		}
	case reflect.Slice:
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil {
			return errors.New("coverage array expected")
		}
		for _, item := range items {
			if err := coverageExactMembers(item, kind.Elem()); err != nil {
				return err
			}
		}
	case reflect.Map:
		var items map[string]json.RawMessage
		if json.Unmarshal(raw, &items) != nil {
			return errors.New("coverage map expected")
		}
		for _, item := range items {
			if err := coverageExactMembers(item, kind.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}
func coverageDecode(raw []byte, target any) error {
	if rejectDuplicateMembers(raw) != nil {
		return errors.New("duplicate coverage member")
	}
	if err := decodeDataJSON(raw, target); err != nil {
		return err
	}
	return coverageExactMembers(raw, reflect.TypeOf(target))
}
func (v *policyCoverageContent) UnmarshalJSON(raw []byte) error {
	type plain policyCoverageContent
	var p plain
	if err := coverageDecode(raw, &p); err != nil {
		return err
	}
	*v = policyCoverageContent(p)
	return nil
}
func (v *policyCoverageRecord) UnmarshalJSON(raw []byte) error {
	type plain policyCoverageRecord
	var p plain
	if err := coverageDecode(raw, &p); err != nil {
		return err
	}
	*v = policyCoverageRecord(p)
	return nil
}
func (v *policyCoverageReply) UnmarshalJSON(raw []byte) error {
	type plain policyCoverageReply
	var p plain
	if err := coverageDecode(raw, &p); err != nil {
		return err
	}
	*v = policyCoverageReply(p)
	return nil
}
