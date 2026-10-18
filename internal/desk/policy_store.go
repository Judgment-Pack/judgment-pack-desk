package desk

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strings"
	"time"
)

const policyDirectory = ".desk-private/policies"
const policyLedgerLimit = 16 << 20
const policyCountLimit = 256
const policyHistoryLimit = 512
const policyStorageSchemaVersion = 2
const policyEventLimit = 3 * policyHistoryLimit
const policyOperationLimit = 4096
const policyNarrativeLimit = 128 << 10
const policyArtifactVersionLimit = 256

// encoding/json may escape each accepted character to six bytes. The retained
// private envelope must allow every narrative accepted by the request boundary.
const policyCandidateByteLimit = 6*(policyNarrativeLimit+500+policyArtifactVersionLimit) + 1024

var policyIDForm = regexp.MustCompile(`^policy_[a-f0-9]{48}$`)
var policyEventIDForm = regexp.MustCompile(`^(revision|review|publication)_[a-f0-9]{48}$`)
var policyOperationIDForm = regexp.MustCompile(`^[a-zA-Z0-9_-]{16,128}$`)

type policyStoredRecord struct {
	SchemaVersion   int                 `json:"schemaVersion"`
	ID              string              `json:"id"`
	CreatedAt       string              `json:"createdAt"`
	UpdatedAt       string              `json:"updatedAt"`
	HeadRevisionID  string              `json:"headRevisionId"`
	CurrentReviewID string              `json:"currentReviewId,omitempty"`
	Revisions       []policyRevision    `json:"revisions"`
	Reviews         []policyReview      `json:"reviews"`
	Publications    []policyPublication `json:"publications"`
}

type policyStoredOperation struct {
	OperationID   string              `json:"operationId"`
	Kind          string              `json:"kind"`
	PolicyID      string              `json:"policyId"`
	RequestDigest string              `json:"requestDigest"`
	Result        retainedArtifactRef `json:"result"`
}

type policyLedger struct {
	SchemaVersion int                              `json:"schemaVersion"`
	Policies      map[string]policyStoredRecord    `json:"policies"`
	Operations    map[string]policyStoredOperation `json:"operations"`
}

// A domain transaction owns metadata and operation receipts together. Immutable
// blobs are retained first; the one atomic metadata write makes them reachable.
// Local locks are not a distributed authorization or team-storage adapter.
type policyRepository interface {
	View(context.Context, func(policyLedger) error) error
	Update(context.Context, func(*policyLedger) (bool, error)) error
}

type filePolicyRepository struct{ open func(bool) (*os.Root, error) }

func (s *Server) openPolicies(create bool) (*os.Root, error) {
	root, made, err := openOwnFolder(s.root, strings.Split(policyDirectory, "/"), create, "policies")
	if err == nil && made {
		if err = writePrivateData(root, ".gitignore", []byte("*\n")); err != nil {
			root.Close()
		}
	}
	return root, err
}

func emptyPolicyLedger() policyLedger {
	return policyLedger{SchemaVersion: policyStorageSchemaVersion, Policies: map[string]policyStoredRecord{}, Operations: map[string]policyStoredOperation{}}
}

func (f filePolicyRepository) View(ctx context.Context, use func(policyLedger) error) error {
	return f.transaction(ctx, false, func(ledger *policyLedger) (bool, error) { return false, use(*ledger) })
}
func (f filePolicyRepository) Update(ctx context.Context, use func(*policyLedger) (bool, error)) error {
	return f.transaction(ctx, true, use)
}
func (f filePolicyRepository) transaction(ctx context.Context, write bool, use func(*policyLedger) (bool, error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := f.open(write)
	if errors.Is(err, os.ErrNotExist) && !write {
		ledger := emptyPolicyLedger()
		_, err = use(&ledger)
		return err
	}
	if err != nil {
		return err
	}
	defer root.Close()
	lock, err := root.Open(".")
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = waitPrivateDataLock(ctx, lock, write); err != nil {
		return policyRefusal(503, "storage-busy", "Policy storage is busy. Retry the same operation shortly.")
	}
	ledger := emptyPolicyLedger()
	raw, err := readPrivateData(root, "ledger.json", policyLedgerLimit)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		if rejectDuplicateMembers(raw) != nil || decodeDataJSON(raw, &ledger) != nil || validatePolicyLedger(ledger) != nil {
			return errors.New("policy storage integrity failed")
		}
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	changed, err := use(&ledger)
	if err != nil && write {
		return policyBeforeCommitFailure(err)
	}
	if err != nil || !changed || !write {
		return err
	}
	if err = validatePolicyLedger(ledger); err != nil {
		return policyBeforeCommitFailure(err)
	}
	raw, err = json.Marshal(ledger)
	if err != nil {
		return policyBeforeCommitFailure(err)
	}
	if len(raw) > policyLedgerLimit {
		return policyRefusal(409, "storage-capacity", "Policy storage has reached its supported capacity.")
	}
	if err = ctx.Err(); err != nil {
		return policyBeforeCommitFailure(err)
	}
	return writePrivateData(root, "ledger.json", raw)
}

func policyRecordRevision(record policyStoredRecord) string {
	raw, _ := json.Marshal(record)
	return digestOf(raw)
}
func policyWorkflowDigest(workflow policyWorkflow) string {
	raw, _ := json.Marshal(workflow)
	return digestOf(raw)
}
func policyHead(record policyStoredRecord) policyRevision {
	for _, revision := range record.Revisions {
		if revision.ID == record.HeadRevisionID {
			return revision
		}
	}
	return policyRevision{}
}
func policyCurrentReview(record policyStoredRecord) *policyReview {
	head := policyHead(record)
	for _, review := range record.Reviews {
		if review.ID == record.CurrentReviewID && review.CandidateRevisionID == head.ID && review.CandidateDigest == head.Digest && review.WorkflowDigest == head.WorkflowDigest {
			return &review
		}
	}
	return nil
}
func policyReviewState(record policyStoredRecord) string {
	if review := policyCurrentReview(record); review != nil {
		if review.Decision == "approve" {
			return "approved"
		}
		return "changes-requested"
	}
	return "unreviewed"
}
func validPolicyTime(value string) bool {
	_, err := time.Parse("2006-01-02T15:04:05Z", value)
	return err == nil
}
func validPolicyActor(actor policyActor) bool {
	return actor.Subject != "" && len(actor.Subject) <= 1024 && (actor.Kind == "local" && actor.Issuer == "" || actor.Kind == "oidc" && actor.Issuer != "" && len(actor.Issuer) <= 4096)
}
func validatePolicyLedger(ledger policyLedger) error {
	bad := errors.New("policy storage integrity failed")
	if ledger.SchemaVersion != policyStorageSchemaVersion || ledger.Policies == nil || ledger.Operations == nil || len(ledger.Policies) > policyCountLimit || len(ledger.Operations) > policyOperationLimit {
		return bad
	}
	for id, record := range ledger.Policies {
		if record.SchemaVersion != policyStorageSchemaVersion || record.ID != id || !policyIDForm.MatchString(id) || !validPolicyTime(record.CreatedAt) || !validPolicyTime(record.UpdatedAt) || len(record.Revisions) == 0 || len(record.Revisions) > policyHistoryLimit || len(record.Reviews) > policyHistoryLimit || len(record.Publications) > policyHistoryLimit || record.Reviews == nil || record.Publications == nil {
			return bad
		}
		revisions := map[string]policyRevision{}
		for _, revision := range record.Revisions {
			if !policyEventIDForm.MatchString(revision.ID) || !strings.HasPrefix(revision.ID, "revision_") || revisions[revision.ID].ID != "" || !revision.Content.valid() || revision.Content.Bytes > policyCandidateByteLimit || revision.Digest != revision.Content.SHA256 || len(revision.Title) > 500 || len(revision.ArtifactVersion) > policyArtifactVersionLimit || revision.WorkflowDigest != policyWorkflowDigest(revision.Workflow) || !validPolicyTime(revision.At) || !validPolicyActor(revision.Actor) {
				return bad
			}
			revisions[revision.ID] = revision
		}
		if revisions[record.HeadRevisionID].ID == "" || record.CreatedAt != record.Revisions[0].At {
			return bad
		}
		reviews := map[string]policyReview{}
		for _, review := range record.Reviews {
			revision := revisions[review.CandidateRevisionID]
			if !policyEventIDForm.MatchString(review.ID) || !strings.HasPrefix(review.ID, "review_") || reviews[review.ID].ID != "" || revision.ID == "" || review.CandidateDigest != revision.Digest || review.WorkflowDigest != revision.WorkflowDigest || !contains([]string{"approve", "request-changes"}, review.Decision) || review.Decision == "approve" && revision.Workflow.IndependentReviewRequired || len(review.Comment) > 8192 || !validPolicyTime(review.At) || !validPolicyActor(review.Actor) {
				return bad
			}
			reviews[review.ID] = review
		}
		if record.CurrentReviewID != "" && policyCurrentReview(record) == nil {
			return bad
		}
		versions, publications := map[string]bool{}, map[string]bool{}
		for _, publication := range record.Publications {
			revision, review := revisions[publication.CandidateRevisionID], reviews[publication.ReviewID]
			if !policyEventIDForm.MatchString(publication.ID) || !strings.HasPrefix(publication.ID, "publication_") || publications[publication.ID] || revision.ID == "" || publication.CandidateDigest != revision.Digest || publication.WorkflowDigest != revision.WorkflowDigest || publication.Content != revision.Content || publication.ArtifactVersion == "" || publication.ArtifactVersion != revision.ArtifactVersion || versions[publication.ArtifactVersion] || review.ID == "" || review.Decision != "approve" || review.CandidateRevisionID != revision.ID || !validPolicyTime(publication.At) || !validPolicyActor(publication.Actor) {
				return bad
			}
			publications[publication.ID], versions[publication.ArtifactVersion] = true, true
		}
		if validatePolicyEventOrder(record) != nil {
			return bad
		}
	}
	for key, operation := range ledger.Operations {
		if key != operation.OperationID || !policyOperationIDForm.MatchString(key) || !contains([]string{"create", "save-draft", "review", "publish"}, operation.Kind) || ledger.Policies[operation.PolicyID].ID == "" || !retainedArtifactDigest.MatchString(operation.RequestDigest) || !operation.Result.valid() {
			return bad
		}
	}
	return nil
}

// Event sequence is committed under the same metadata lock as the history and
// receipt. Timestamps are display/audit data; they cannot establish commit order.
func policyEventCount(record policyStoredRecord) int {
	return len(record.Revisions) + len(record.Reviews) + len(record.Publications)
}
func validatePolicyEventOrder(record policyStoredRecord) error {
	bad := errors.New("policy event order is inconsistent")
	total := policyEventCount(record)
	if total < 1 || total > policyEventLimit {
		return bad
	}
	type event struct{ kind, id, revisionID, reviewID, at string }
	events := make([]event, total+1)
	add := func(sequence int, item event, last *int) bool {
		if sequence <= *last || sequence > total || events[sequence].kind != "" {
			return false
		}
		events[sequence] = item
		*last = sequence
		return true
	}
	last := 0
	for _, revision := range record.Revisions {
		if !add(revision.Sequence, event{kind: "revision", id: revision.ID, at: revision.At}, &last) {
			return bad
		}
	}
	last = 0
	for _, review := range record.Reviews {
		if !add(review.Sequence, event{kind: "review", id: review.ID, revisionID: review.CandidateRevisionID, at: review.At}, &last) {
			return bad
		}
	}
	last = 0
	for _, publication := range record.Publications {
		if !add(publication.Sequence, event{kind: "publication", id: publication.ID, revisionID: publication.CandidateRevisionID, reviewID: publication.ReviewID, at: publication.At}, &last) {
			return bad
		}
	}
	currentRevision, currentReview := "", ""
	for sequence := 1; sequence <= total; sequence++ {
		item := events[sequence]
		switch item.kind {
		case "revision":
			currentRevision, currentReview = item.id, ""
		case "review":
			if item.revisionID != currentRevision {
				return bad
			}
			currentReview = item.id
		case "publication":
			if item.revisionID != currentRevision || item.reviewID != currentReview {
				return bad
			}
		default:
			return bad // Missing sequence: history must cover exactly 1..N.
		}
	}
	if record.HeadRevisionID != currentRevision || record.CurrentReviewID != currentReview || record.UpdatedAt != events[total].at {
		return bad
	}
	return nil
}
