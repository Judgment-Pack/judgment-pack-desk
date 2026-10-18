package desk

// These are private Desk application records, not a portable Policy format or
// a claim of JPS Policy conformance. Executable authority stays with packs/graphs.
type policyActor struct {
	Kind    string `json:"kind"`
	Subject string `json:"subject"`
	Issuer  string `json:"issuer,omitempty"`
}

type policyWorkflow struct {
	IndependentReviewRequired bool `json:"independentReviewRequired"`
}

type policyCandidateContent struct {
	SchemaVersion   int    `json:"schemaVersion"`
	Title           string `json:"title"`
	Narrative       string `json:"narrative"`
	ArtifactVersion string `json:"artifactVersion"`
}

type policyCandidate struct {
	Title           string              `json:"title"`
	Narrative       string              `json:"narrative"`
	ArtifactVersion string              `json:"artifactVersion"`
	Digest          string              `json:"digest"`
	RevisionID      string              `json:"revisionId"`
	Content         retainedArtifactRef `json:"content"`
}

type policyRevision struct {
	ID              string              `json:"id"`
	Sequence        int                 `json:"sequence"`
	Digest          string              `json:"digest"`
	Title           string              `json:"title"`
	ArtifactVersion string              `json:"artifactVersion"`
	Content         retainedArtifactRef `json:"content"`
	Workflow        policyWorkflow      `json:"workflow"`
	WorkflowDigest  string              `json:"workflowDigest"`
	At              string              `json:"at"`
	Actor           policyActor         `json:"actor"`
}

type policyReview struct {
	ID                  string      `json:"id"`
	Sequence            int         `json:"sequence"`
	CandidateRevisionID string      `json:"candidateRevisionId"`
	CandidateDigest     string      `json:"candidateDigest"`
	WorkflowDigest      string      `json:"workflowDigest"`
	Decision            string      `json:"decision"`
	Comment             string      `json:"comment,omitempty"`
	At                  string      `json:"at"`
	Actor               policyActor `json:"actor"`
}

type policyPublication struct {
	ID                  string              `json:"id"`
	Sequence            int                 `json:"sequence"`
	CandidateRevisionID string              `json:"candidateRevisionId"`
	CandidateDigest     string              `json:"candidateDigest"`
	WorkflowDigest      string              `json:"workflowDigest"`
	Content             retainedArtifactRef `json:"content"`
	ArtifactVersion     string              `json:"artifactVersion"`
	ReviewID            string              `json:"reviewId"`
	At                  string              `json:"at"`
	Actor               policyActor         `json:"actor"`
}

type policyDocument struct {
	SchemaVersion   int                 `json:"schemaVersion"`
	ID              string              `json:"id"`
	ManagementMode  string              `json:"managementMode"`
	CreatedAt       string              `json:"createdAt"`
	UpdatedAt       string              `json:"updatedAt"`
	Candidate       policyCandidate     `json:"candidate"`
	Workflow        policyWorkflow      `json:"workflow"`
	WorkflowDigest  string              `json:"workflowDigest"`
	ReviewState     string              `json:"reviewState"`
	CurrentReviewID string              `json:"currentReviewId,omitempty"`
	Revisions       []policyRevision    `json:"revisions"`
	Reviews         []policyReview      `json:"reviews"`
	Publications    []policyPublication `json:"publications"`
}

type policyReply struct {
	Revision string         `json:"revision"`
	Document policyDocument `json:"document"`
}

type policySummary struct {
	ID                        string             `json:"id"`
	Title                     string             `json:"title"`
	ArtifactVersion           string             `json:"artifactVersion"`
	UpdatedAt                 string             `json:"updatedAt"`
	ReviewState               string             `json:"reviewState"`
	CandidateDigest           string             `json:"candidateDigest"`
	CurrentCandidatePublished bool               `json:"currentCandidatePublished"`
	PublicationCount          int                `json:"publicationCount"`
	LatestPublication         *policyPublication `json:"latestPublication,omitempty"`
}

type policyEditRequest struct {
	OperationID     string          `json:"operationId"`
	BaseRevision    string          `json:"baseRevision,omitempty"`
	Title           string          `json:"title"`
	Narrative       string          `json:"narrative"`
	ArtifactVersion string          `json:"artifactVersion"`
	Workflow        *policyWorkflow `json:"workflow,omitempty"`
}

type policyReviewRequest struct {
	OperationID     string `json:"operationId"`
	BaseRevision    string `json:"baseRevision"`
	CandidateDigest string `json:"candidateDigest"`
	WorkflowDigest  string `json:"workflowDigest"`
	Decision        string `json:"decision"`
	Comment         string `json:"comment,omitempty"`
}

type policyPublishRequest struct {
	OperationID     string `json:"operationId"`
	BaseRevision    string `json:"baseRevision"`
	CandidateDigest string `json:"candidateDigest"`
	WorkflowDigest  string `json:"workflowDigest"`
	ReviewID        string `json:"reviewId"`
}

type policyOperationReceipt struct {
	OperationID     string      `json:"operationId"`
	Kind            string      `json:"kind"`
	PolicyID        string      `json:"policyId"`
	RequestDigest   string      `json:"requestDigest"`
	At              string      `json:"at"`
	Actor           policyActor `json:"actor"`
	Revision        string      `json:"revision"`
	CandidateDigest string      `json:"candidateDigest"`
	ReviewID        string      `json:"reviewId,omitempty"`
	PublicationID   string      `json:"publicationId,omitempty"`
}

type policyMutationReply struct {
	Delivery string                 `json:"delivery"`
	Receipt  policyOperationReceipt `json:"receipt"`
	Policy   policyReply            `json:"policy"`
}
