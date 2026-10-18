package desk

// Adoption is Desk-private reviewed dependency history. It carries no execution
// grant and is not registration with Runner or a stable Policy standard.
import (
	"encoding/json"
	"errors"
	"regexp"
)

const policyAdoptionCountLimit = 256
const policyAdoptionSetLimit = 16
const policyAdoptionContentLimit = 2 << 20

var policyAdoptionIDForm = regexp.MustCompile(`^adoption_[a-f0-9]{48}$`)
var policyAdoptionCandidateIDForm = regexp.MustCompile(`^adoption_candidate_[a-f0-9]{48}$`)
var policyAdoptionReviewIDForm = regexp.MustCompile(`^adoption_review_[a-f0-9]{48}$`)

type policyAdoptionSelection struct {
	PolicyID    string `json:"policyId"`
	CoverageID  string `json:"coverageId"`
	Revision    string `json:"revision"`
	CandidateID string `json:"candidateId"`
	ReviewID    string `json:"reviewId"`
}
type policyAdoptionDependency struct {
	Selection           policyAdoptionSelection `json:"selection"`
	CandidateDigest     string                  `json:"candidateDigest"`
	CoverageContent     retainedArtifactRef     `json:"coverageContent"`
	CoverageReview      policyCoverageReview    `json:"coverageReview"`
	CoverageRecovery    *policyCoverageRecovery `json:"coverageRecovery,omitempty"`
	PolicyRevision      string                  `json:"policyRevision"`
	LifecycleRevision   string                  `json:"lifecycleRevision"`
	SourceRevision      string                  `json:"sourceRevision"`
	AuthorityRevision   string                  `json:"authorityRevision"`
	PolicyPublicationID string                  `json:"policyPublicationId"`
	Source              policyAuthoringSource   `json:"source"`
	Scope               policyCoverageScope     `json:"scope"`
	Statement           policyCoverageStatement `json:"statement"`
}
type policyAdoptionBasis struct {
	Pack         policyCoveragePackSelector `json:"pack"`
	Publication  artifactPublication        `json:"publication"`
	Dependencies []policyAdoptionDependency `json:"dependencies"`
}
type policyAdoptionMaterial struct {
	Basis      policyAdoptionBasis
	Materials  []policyCoverageMaterial
	PortableID string
}
type policyAdoptionPrepared struct {
	Document  []byte
	Materials [][]byte
	Report    json.RawMessage
}
type policyAdoptionCheck struct {
	Availability string          `json:"availability"`
	Reason       string          `json:"reason,omitempty"`
	Report       json.RawMessage `json:"report,omitempty"`
	prepared     *policyAdoptionPrepared
}
type policyAdoptionContent struct {
	SchemaVersion int                   `json:"schemaVersion"`
	PortableID    string                `json:"portableId"`
	FormatVersion string                `json:"formatVersion"`
	BasisDigest   string                `json:"basisDigest"`
	Basis         policyAdoptionBasis   `json:"basis"`
	Document      retainedArtifactRef   `json:"document"`
	Materials     []retainedArtifactRef `json:"materials"`
	Check         retainedArtifactRef   `json:"check"`
}
type policyAdoptionCandidate struct {
	ID            string                     `json:"id"`
	Digest        string                     `json:"digest"`
	Content       retainedArtifactRef        `json:"content"`
	PortableID    string                     `json:"portableId"`
	FormatVersion string                     `json:"formatVersion"`
	BasisDigest   string                     `json:"basisDigest"`
	Pack          policyCoveragePackSelector `json:"pack"`
	Dependencies  []policyAdoptionDependency `json:"dependencies"`
	Document      retainedArtifactRef        `json:"document"`
	Materials     []retainedArtifactRef      `json:"materials"`
	Check         retainedArtifactRef        `json:"check"`
}
type policyAdoptionReview struct {
	ID              string               `json:"id"`
	Sequence        int                  `json:"sequence"`
	CandidateID     string               `json:"candidateId"`
	CandidateDigest string               `json:"candidateDigest"`
	BasisDigest     string               `json:"basisDigest"`
	Decision        string               `json:"decision"`
	Comment         string               `json:"comment,omitempty"`
	At              string               `json:"at"`
	Actor           policyActor          `json:"actor"`
	Check           *retainedArtifactRef `json:"check,omitempty"`
}
type policyAdoptionRecord struct {
	SchemaVersion   int                     `json:"schemaVersion"`
	ID              string                  `json:"id"`
	CreatedAt       string                  `json:"createdAt"`
	UpdatedAt       string                  `json:"updatedAt"`
	Actor           policyActor             `json:"actor"`
	CandidateID     string                  `json:"candidateId"`
	Content         retainedArtifactRef     `json:"content"`
	CurrentReviewID string                  `json:"currentReviewId,omitempty"`
	Reviews         []policyAdoptionReview  `json:"reviews"`
	Recovery        *policyCoverageRecovery `json:"recovery,omitempty"`
}
type policyAdoptionDocument struct {
	SchemaVersion   int                     `json:"schemaVersion"`
	ID              string                  `json:"id"`
	CreatedAt       string                  `json:"createdAt"`
	UpdatedAt       string                  `json:"updatedAt"`
	Candidate       policyAdoptionCandidate `json:"candidate"`
	ReviewState     string                  `json:"reviewState"`
	CurrentReviewID string                  `json:"currentReviewId,omitempty"`
	Reviews         []policyAdoptionReview  `json:"reviews"`
}
type policyAdoptionState struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}
type policyAdoptionReply struct {
	RecoveryBoundary *policyCoverageRecovery `json:"recoveryBoundary,omitempty"`
	Revision         string                  `json:"revision"`
	Document         policyAdoptionDocument  `json:"document"`
	Readiness        policyAdoptionState     `json:"readiness"`
	Eligibility      policyAdoptionState     `json:"eligibility"`
	Recovery         *policyRecoveryView     `json:"recovery,omitempty"`
}
type policyAdoptionSummary struct {
	ID              string                     `json:"id"`
	Revision        string                     `json:"revision"`
	CandidateDigest string                     `json:"candidateDigest"`
	Pack            policyCoveragePackSelector `json:"pack"`
	Dependencies    []policyAdoptionSelection  `json:"dependencies"`
	ReviewState     string                     `json:"reviewState"`
	UpdatedAt       string                     `json:"updatedAt"`
}
type policyAdoptionRequest struct {
	Action          string                      `json:"action"`
	OperationID     string                      `json:"operationId"`
	Pack            *policyCoveragePackSelector `json:"pack,omitempty"`
	Coverages       []policyAdoptionSelection   `json:"coverages,omitempty"`
	AdoptionID      string                      `json:"adoptionId,omitempty"`
	BaseRevision    string                      `json:"baseRevision,omitempty"`
	CandidateDigest string                      `json:"candidateDigest,omitempty"`
	BasisDigest     string                      `json:"basisDigest,omitempty"`
	Decision        string                      `json:"decision,omitempty"`
	Comment         string                      `json:"comment,omitempty"`
}

func (v *policyAdoptionSelection) UnmarshalJSON(raw []byte) error {
	type plain policyAdoptionSelection
	var p plain
	if coverageClosedJSON(raw, &p, "policyId", "coverageId", "revision", "candidateId", "reviewId") != nil || !validAdoptionSelection(policyAdoptionSelection(p)) {
		return errors.New("Choose exact approved coverage snapshots.")
	}
	*v = policyAdoptionSelection(p)
	return nil
}
func validAdoptionSelection(p policyAdoptionSelection) bool {
	return policyIDForm.MatchString(p.PolicyID) && policyCoverageIDForm.MatchString(p.CoverageID) && retainedArtifactDigest.MatchString(p.Revision) && policyCoverageCandidateIDForm.MatchString(p.CandidateID) && policyCoverageReviewIDForm.MatchString(p.ReviewID)
}
func (v *policyAdoptionRequest) UnmarshalJSON(raw []byte) error {
	type plain policyAdoptionRequest
	var p plain
	if coverageClosedJSON(raw, &p, "action", "operationId", "pack", "coverages", "adoptionId", "baseRevision", "candidateDigest", "basisDigest", "decision", "comment") != nil {
		return errors.New("Invalid adoption command.")
	}
	allowed := []string{"action", "operationId"}
	if p.Action == "prepare" {
		allowed = append(allowed, "pack", "coverages")
	} else if p.Action == "review" {
		allowed = append(allowed, "adoptionId", "baseRevision", "candidateDigest", "basisDigest", "decision", "comment")
	} else {
		return errors.New("Invalid adoption action.")
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	for k := range fields {
		if !contains(allowed, k) {
			return errors.New("Unrelated adoption action field.")
		}
	}
	*v = policyAdoptionRequest(p)
	return validateAdoptionRequest(*v)
}
func validateAdoptionRequest(q policyAdoptionRequest) error {
	bad := policyRefusal(400, CodeBadRequest, "Choose an exact Pack and complete approved coverage set, or review the exact saved adoption.")
	if !policyOperationIDForm.MatchString(q.OperationID) {
		return bad
	}
	if q.Action == "prepare" {
		if q.Pack == nil || !validCoveragePackSelector(*q.Pack) || len(q.Coverages) < 1 || len(q.Coverages) > policyAdoptionSetLimit || q.AdoptionID != "" || q.BaseRevision != "" || q.CandidateDigest != "" || q.BasisDigest != "" || q.Decision != "" || q.Comment != "" {
			return bad
		}
		seen := map[string]bool{}
		for _, v := range q.Coverages {
			if !validAdoptionSelection(v) || seen[v.PolicyID] {
				return bad
			}
			seen[v.PolicyID] = true
		}
		return nil
	}
	if q.Action != "review" || !policyAdoptionIDForm.MatchString(q.AdoptionID) || !retainedArtifactDigest.MatchString(q.BaseRevision) || !retainedArtifactDigest.MatchString(q.CandidateDigest) || !retainedArtifactDigest.MatchString(q.BasisDigest) || !contains([]string{"approve", "request-changes"}, q.Decision) || len(q.Comment) > 8192 || q.Pack != nil || len(q.Coverages) != 0 {
		return bad
	}
	return nil
}
func adoptionRevision(r policyAdoptionRecord) string { raw, _ := json.Marshal(r); return digestOf(raw) }
func adoptionBasisDigest(b policyAdoptionBasis) string {
	raw, _ := json.Marshal(b)
	return digestOf(raw)
}
func adoptionReviewState(r policyAdoptionRecord) (string, string) {
	for i := len(r.Reviews) - 1; i >= 0; i-- {
		v := r.Reviews[i]
		if r.Recovery != nil && v.Sequence <= r.Recovery.Sequence {
			continue
		}
		if v.Decision == "approve" {
			return "reviewed", v.ID
		}
		return "changes-requested", v.ID
	}
	return "unreviewed", ""
}
func (v *policyAdoptionRecord) UnmarshalJSON(raw []byte) error {
	type plain policyAdoptionRecord
	var p plain
	if err := coverageDecode(raw, &p); err != nil {
		return err
	}
	*v = policyAdoptionRecord(p)
	return nil
}
func (v *policyAdoptionContent) UnmarshalJSON(raw []byte) error {
	type plain policyAdoptionContent
	var p plain
	if err := coverageDecode(raw, &p); err != nil {
		return err
	}
	*v = policyAdoptionContent(p)
	return nil
}
func (v *policyAdoptionReply) UnmarshalJSON(raw []byte) error {
	type plain policyAdoptionReply
	var p plain
	if err := coverageDecode(raw, &p); err != nil {
		return err
	}
	*v = policyAdoptionReply(p)
	return nil
}
