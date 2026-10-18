package desk

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

type policyFailure struct {
	Status                         int
	Code, Message, CurrentRevision string
	CurrentPolicyRevision          string
	Uncertain                      bool
}

func (f *policyFailure) Error() string { return f.Message }
func policyRefusal(status int, code, message string) *policyFailure {
	return &policyFailure{Status: status, Code: code, Message: message}
}

func policyUncertainStorage() *policyFailure {
	failure := policyRefusal(503, "storage-unavailable", "Policy storage could not confirm this operation. Keep its operation ID and check recovery before starting another write.")
	failure.Uncertain = true
	return failure
}

func policyBeforeCommitFailure(err error) error {
	var known *policyFailure
	if errors.As(err, &known) {
		return err
	}
	return policyRefusal(503, "storage-unavailable", "Policy content could not be retained. No policy change was committed. Check storage before retrying.")
}

type policyService struct {
	checkAdoption   func(context.Context, policyAdoptionMaterial) policyAdoptionCheck
	coveragePack    func(context.Context, policyCoveragePackSelector) (policyCoverageResolvedPack, error)
	checkCoverage   func(context.Context, policyCoverageMaterial) policyCoverageCheckReply
	repository      policyRepository
	history         policyHistoryReader
	artifacts       retainedArtifactRepository
	authorize       func() bool
	validateVersion func(context.Context, string, string) error
	clock           func() time.Time
}

type policyCommand struct {
	Kind, PolicyID, OperationID, RequestDigest string
	Actor                                      policyActor
	Edit                                       *policyEditRequest
	Review                                     *policyReviewRequest
	Publish                                    *policyPublishRequest
}

func (s policyService) authorized(ctx context.Context) error {
	if ctx.Err() != nil || s.authorize == nil || !s.authorize() {
		return policyRefusal(401, CodeUnauthorized, "Sign in again before accessing policies.")
	}
	return nil
}

func (s policyService) list(ctx context.Context) ([]policySummary, error) {
	items := []policySummary{}
	err := s.repository.View(ctx, func(ledger policyLedger) error {
		if err := s.authorized(ctx); err != nil {
			return err
		}
		for _, record := range ledger.Policies {
			head := policyHead(record)
			stream := ledger.SourceAuthorities[record.ID]
			row := policySummary{Recovery: policyRecoveryProjection(record, stream), ID: record.ID, Title: head.Title, ArtifactVersion: head.ArtifactVersion, UpdatedAt: record.UpdatedAt, ReviewState: policyAuthorityReviewState(record, stream), PublicationCount: len(record.Publications), CandidateDigest: head.Digest}
			owner, _ := policyAuthorityOwnership(stream)
			if stream != nil {
				row.ManagementMode = owner.Mode
				row.SourceAuthority = policyAuthoritySummary(stream)
			}
			row.CurrentCandidatePublished = policyAuthorityCurrentCandidatePublished(record, stream)
			if len(record.Publications) > 0 {
				publication := record.Publications[len(record.Publications)-1]
				row.LatestPublication = &publication
			}
			items = append(items, row)
		}
		return nil
	})
	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt == items[j].UpdatedAt {
			return items[i].ID < items[j].ID
		}
		return items[i].UpdatedAt > items[j].UpdatedAt
	})
	return items, err
}

func (s policyService) get(ctx context.Context, id string) (policyReply, error) {
	var record policyStoredRecord
	var stream *policySourceAuthorityStream
	err := s.repository.View(ctx, func(ledger policyLedger) error {
		if err := s.authorized(ctx); err != nil {
			return err
		}
		var ok bool
		record, ok = ledger.Policies[id]
		stream = ledger.SourceAuthorities[id]
		if !ok {
			return policyRefusal(404, CodeNotFound, "Policy not found.")
		}
		return nil
	})
	if err != nil {
		return policyReply{}, err
	}
	return s.reply(ctx, record, stream)
}

func (s policyService) candidate(ctx context.Context, revision policyRevision) (policyCandidate, error) {
	raw, err := s.artifacts.Get(ctx, revision.Content)
	if err != nil {
		return policyCandidate{}, err
	}
	var content policyCandidateContent
	if decodeDataJSON(raw, &content) != nil || content.SchemaVersion != 1 || !samePolicySource(content.Source, revision.Source) || !validPolicySource(content.Source) || content.Title != revision.Title || content.ArtifactVersion != revision.ArtifactVersion || revision.Digest != digestOf(raw) || len(content.Narrative) > policyNarrativeLimit {
		return policyCandidate{}, errors.New("retained policy candidate is inconsistent")
	}
	if content.Source != nil {
		if _, err = s.artifacts.Get(ctx, content.Source.Content); err != nil {
			return policyCandidate{}, err
		}
	}
	return policyCandidate{Source: content.Source, Title: content.Title, Narrative: content.Narrative, ArtifactVersion: content.ArtifactVersion, Digest: revision.Digest, RevisionID: revision.ID, Content: revision.Content}, nil
}
func (s policyService) reply(ctx context.Context, record policyStoredRecord, stream *policySourceAuthorityStream) (policyReply, error) {
	head := policyHead(record)
	candidate, err := s.candidate(ctx, head)
	if err != nil {
		return policyReply{}, err
	}
	if err = s.authorized(ctx); err != nil {
		return policyReply{}, err
	}
	owner, _ := policyAuthorityOwnership(stream)
	currentReviewID := ""
	if review := policyAuthorityCurrentReview(record, stream); review != nil {
		currentReviewID = review.ID
	}
	published := policyAuthorityCurrentCandidatePublished(record, stream)
	return policyReply{Recovery: policyRecoveryProjection(record, stream), CurrentCandidatePublished: &published, SourceAuthority: policyAuthoritySummary(stream), Revision: policyEffectiveRevision(record, stream), Document: policyDocument{SchemaVersion: 1, ID: record.ID, ManagementMode: owner.Mode, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt, Candidate: candidate, Workflow: head.Workflow, WorkflowDigest: head.WorkflowDigest, ReviewState: policyAuthorityReviewState(record, stream), CurrentReviewID: currentReviewID, Revisions: record.Revisions, Reviews: record.Reviews, Publications: record.Publications}}, nil
}

func (s policyService) revision(ctx context.Context, id, revisionID string) (any, error) {
	var revision policyRevision
	var candidate policyCandidate
	err := s.readHistory(ctx, func(ledger policyLedger) error {
		for _, item := range ledger.Policies[id].Revisions {
			if item.ID == revisionID {
				revision = item
				var err error
				candidate, err = s.candidate(ctx, revision)
				return err
			}
		}
		return policyRefusal(404, CodeNotFound, "Policy revision not found.")
	})
	if err != nil {
		return nil, err
	}
	return struct {
		Candidate      policyCandidate `json:"candidate"`
		Workflow       policyWorkflow  `json:"workflow"`
		WorkflowDigest string          `json:"workflowDigest"`
		At             string          `json:"at"`
		Actor          policyActor     `json:"actor"`
		Sequence       int             `json:"sequence"`
	}{candidate, revision.Workflow, revision.WorkflowDigest, revision.At, revision.Actor, revision.Sequence}, nil
}

func (s policyService) recovered(ctx context.Context, ledger policyLedger, command policyCommand) (*policyMutationReply, error) {
	operation, ok := ledger.Operations[command.OperationID]
	if !ok {
		return nil, nil
	}
	if operation.Kind != command.Kind || operation.RequestDigest != command.RequestDigest || command.Kind != "create" && operation.PolicyID != command.PolicyID {
		return nil, policyRefusal(409, "idempotency-conflict", "This operation ID was already used for a different request. Keep the original request or start a new reviewed operation.")
	}
	raw, err := s.artifacts.Get(ctx, operation.Result)
	if err != nil {
		return nil, policyUncertainStorage()
	}
	var result policyMutationReply
	if decodeDataJSON(raw, &result) != nil || result.Delivery != "committed" || result.Receipt.OperationID != command.OperationID || result.Receipt.Kind != command.Kind || result.Receipt.RequestDigest != command.RequestDigest || result.Receipt.PolicyID != operation.PolicyID || result.Receipt.Actor != command.Actor || !validPolicyMutationIdentity(result) || !validPolicyLifecycleResult(result) || !validPolicySourceAuthorityResult(result) || !validPolicyCoverageResult(result) || !validPolicyAdoptionResult(result) {
		return nil, policyUncertainStorage()
	}
	if err = s.authorized(ctx); err != nil {
		return nil, err
	}
	result.Delivery = "recovered"
	return &result, nil
}

// Receipt lookup is historical read-only recovery. Absence does not prove an
// in-flight operation will never commit, and reading a receipt repeats no write.
func (s policyService) operation(ctx context.Context, id string) (policyMutationReply, error) {
	var result policyMutationReply
	err := s.readHistory(ctx, func(ledger policyLedger) error {
		operation, ok := ledger.Operations[id]
		if !ok {
			return policyRefusal(404, CodeNotFound, "No completed policy operation receipt is available. The original request may still be in progress.")
		}
		if err := validateArchivedPolicyOperation(ctx, ledger, s.artifacts, operation); err != nil {
			return policyRefusal(409, "storage-integrity", "The retained Policy operation is incomplete or inconsistent.")
		}
		raw, err := s.artifacts.Get(ctx, operation.Result)
		if err != nil {
			return policyRefusal(503, "storage-unavailable", "The retained Policy operation is unavailable.")
		}
		if decodeDataJSON(raw, &result) != nil || result.Delivery != "committed" || result.Receipt.OperationID != id || result.Receipt.Kind != operation.Kind || result.Receipt.PolicyID != operation.PolicyID || result.Receipt.RequestDigest != operation.RequestDigest || !validPolicyMutationIdentity(result) || !validPolicyLifecycleResult(result) || !validPolicySourceAuthorityResult(result) || !validPolicyCoverageResult(result) || !validPolicyAdoptionResult(result) {
			return policyRefusal(409, "storage-integrity", "The retained Policy operation is inconsistent.")
		}
		result.Delivery = "recovered"
		return nil
	})
	if err != nil {
		return policyMutationReply{}, err
	}
	return result, nil
}

func policyCommandRecord(ledger policyLedger, command policyCommand) (policyStoredRecord, error) {
	if command.Kind == "create" {
		if command.Edit.BaseRevision != "" {
			return policyStoredRecord{}, policyRefusal(400, CodeBadRequest, "A new policy cannot specify an existing revision.")
		}
		return policyStoredRecord{}, nil
	}
	record, ok := ledger.Policies[command.PolicyID]
	if !ok {
		return record, policyRefusal(404, CodeNotFound, "Policy not found.")
	}
	var base string
	switch command.Kind {
	case "save-draft":
		base = command.Edit.BaseRevision
	case "review":
		base = command.Review.BaseRevision
	case "publish":
		base = command.Publish.BaseRevision
	}
	if base != policyEffectiveRevision(record, ledger.SourceAuthorities[record.ID]) {
		failure := policyRefusal(409, "revision-conflict", "This policy changed. Keep your edits and reload the saved revision before continuing.")
		failure.CurrentRevision = policyEffectiveRevision(record, ledger.SourceAuthorities[record.ID])
		return record, failure
	}
	return record, nil
}

func policyValidateCommand(command policyCommand, record policyStoredRecord, authority *policySourceAuthorityStream) error {
	if command.Edit != nil {
		if command.Edit.Import != nil {
			if command.Kind != "create" {
				return policyRefusal(400, CodeBadRequest, "Original source import is supported only when creating a policy draft.")
			}
			if err := validatePolicyImport(*command.Edit.Import); err != nil {
				return err
			}
		}
		if len(command.Edit.Title) > 500 || len(command.Edit.Narrative) > policyNarrativeLimit || len(command.Edit.ArtifactVersion) > policyArtifactVersionLimit {
			return policyRefusal(400, CodeBadRequest, "The policy title, narrative or version exceeds its supported size.")
		}
		return nil
	}
	head := policyHead(record)
	var candidateDigest, workflowDigest string
	if command.Review != nil {
		candidateDigest, workflowDigest = command.Review.CandidateDigest, command.Review.WorkflowDigest
		if !contains([]string{"approve", "request-changes"}, command.Review.Decision) || len(command.Review.Comment) > 8192 {
			return policyRefusal(400, CodeBadRequest, "Provide an approval or request for changes with a bounded comment.")
		}
	} else {
		candidateDigest, workflowDigest = command.Publish.CandidateDigest, command.Publish.WorkflowDigest
	}
	if candidateDigest != head.Digest || workflowDigest != head.WorkflowDigest {
		return policyRefusal(409, "revision-conflict", "Review the current policy candidate and workflow before continuing.")
	}
	if command.Kind == "publish" || command.Review.Decision == "approve" {
		if head.Workflow.IndependentReviewRequired {
			return policyRefusal(422, "unsupported-workflow", "Independent review requires another authorized reviewer. This local installation supports one administrator and cannot satisfy that workflow.")
		}
		if strings.TrimSpace(head.Title) == "" || strings.TrimSpace(head.ArtifactVersion) == "" {
			return policyRefusal(422, CodeBadRequest, "Provide a title and artifact version before approval or publication.")
		}
	}
	if command.Kind == "publish" {
		review := policyAuthorityCurrentReview(record, authority)
		if review == nil || review.Decision != "approve" || review.ID != command.Publish.ReviewID {
			return policyRefusal(409, "review-incomplete", "Approve this exact current policy candidate before publishing it.")
		}
		for _, publication := range record.Publications {
			if publication.ArtifactVersion == head.ArtifactVersion {
				return policyRefusal(409, "version-conflict", "This policy version is already published. Choose a new version and review the changed candidate.")
			}
		}
	}
	return nil
}

func newPolicyEventID(kind string) (string, error) {
	token, err := NewToken()
	return kind + "_" + token, err
}

func (s policyService) execute(ctx context.Context, command policyCommand) (policyMutationReply, error) {
	if err := s.authorized(ctx); err != nil {
		return policyMutationReply{}, err
	}
	if !policyOperationIDForm.MatchString(command.OperationID) {
		return policyMutationReply{}, policyRefusal(400, CodeBadRequest, "Provide a unique operation ID of 16–128 letters, digits, hyphens or underscores.")
	}
	var recovered *policyMutationReply
	var inspected policyStoredRecord
	err := s.repository.View(ctx, func(ledger policyLedger) error {
		if err := s.authorized(ctx); err != nil {
			return err
		}
		var err error
		recovered, err = s.recovered(ctx, ledger, command)
		if err != nil || recovered != nil {
			return err
		}
		inspected, err = policyCommandRecord(ledger, command)
		if err != nil {
			return err
		}
		if err := policyAuthorityAdmission(inspected, ledger.SourceAuthorities[inspected.ID], command.Kind); err != nil {
			return err
		}
		return policyValidateCommand(command, inspected, ledger.SourceAuthorities[inspected.ID])
	})
	if err != nil {
		return policyMutationReply{}, err
	}
	if recovered != nil {
		return *recovered, nil
	}
	// Runtime is a read-only external process. Never hold the private storage
	// write lock while calling it. The transaction rechecks the exact base and
	// candidate afterward; this result is not a reusable authorization grant.
	if command.Kind == "publish" || command.Kind == "review" && command.Review.Decision == "approve" {
		candidate, err := s.candidate(ctx, policyHead(inspected))
		if err != nil {
			return policyMutationReply{}, policyBeforeCommitFailure(err)
		}
		if strings.TrimSpace(candidate.Narrative) == "" {
			return policyMutationReply{}, policyRefusal(422, CodeBadRequest, "Provide policy content before approval or publication.")
		}
		if s.validateVersion == nil {
			return policyMutationReply{}, policyRefusal(503, "runtime-unavailable", "Runtime version checks are unavailable. Keep this policy draft and retry later.")
		}
		base := candidate.ArtifactVersion
		if command.Kind == "publish" && len(inspected.Publications) > 0 {
			base = inspected.Publications[len(inspected.Publications)-1].ArtifactVersion
		}
		if err = s.validateVersion(ctx, base, candidate.ArtifactVersion); err != nil {
			return policyMutationReply{}, policyBeforeCommitFailure(err)
		}
	}
	var result policyMutationReply
	err = s.repository.Update(ctx, func(ledger *policyLedger) (bool, error) {
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
		if len(ledger.Operations) >= policyOperationLimit {
			return false, policyRefusal(409, "storage-capacity", "Policy operation history has reached its supported capacity.")
		}
		record, err := policyCommandRecord(*ledger, command)
		if err != nil {
			return false, err
		}
		if err = policyAuthorityAdmission(record, ledger.SourceAuthorities[record.ID], command.Kind); err != nil {
			return false, err
		}
		if err = policyValidateCommand(command, record, ledger.SourceAuthorities[record.ID]); err != nil {
			return false, err
		}
		if command.Kind == "publish" && policySourcePending(record, ledger.Sources[record.ID]) {
			return false, policyRefusal(409, "source-update-unresolved", "Review the latest connected source update before publishing this candidate.")
		}
		instant := time.Now()
		if s.clock != nil {
			instant = s.clock()
		}
		now := instant.UTC().Format("2006-01-02T15:04:05Z")
		receipt := policyOperationReceipt{OperationID: command.OperationID, Kind: command.Kind, RequestDigest: command.RequestDigest, At: now, Actor: command.Actor}
		if command.Kind == "create" {
			if len(ledger.Policies) >= policyCountLimit {
				return false, policyRefusal(409, "storage-capacity", "Policy storage has reached its supported capacity.")
			}
			id, err := newPolicyEventID("policy")
			if err != nil {
				return false, err
			}
			record = policyStoredRecord{SchemaVersion: policyStorageSchemaVersion, ID: id, CreatedAt: now, UpdatedAt: now, Revisions: []policyRevision{}, Reviews: []policyReview{}, Publications: []policyPublication{}}
		}
		sequence := policyEventCount(record) + 1
		if command.Edit != nil {
			workflow := policyHead(record).Workflow
			if command.Edit.Workflow != nil {
				workflow = *command.Edit.Workflow
			}
			source := policyHead(record).Source
			if command.Edit.Import != nil {
				imported := command.Edit.Import
				retained, err := s.artifacts.Put(ctx, imported.Data)
				if err != nil {
					return false, err
				}
				source = &policySource{Name: imported.Name, MediaType: imported.MediaType, Origin: "user-upload", Content: retained}
			}
			raw, _ := json.Marshal(policyCandidateContent{Source: source, SchemaVersion: 1, Title: command.Edit.Title, Narrative: command.Edit.Narrative, ArtifactVersion: command.Edit.ArtifactVersion})
			content, err := s.artifacts.Put(ctx, raw)
			if err != nil {
				return false, err
			}
			head := policyHead(record)
			if head.Digest != content.SHA256 || head.WorkflowDigest != policyWorkflowDigest(workflow) {
				if len(record.Revisions) >= policyHistoryLimit {
					return false, policyRefusal(409, "storage-capacity", "This policy has reached its supported revision capacity.")
				}
				id, err := newPolicyEventID("revision")
				if err != nil {
					return false, err
				}
				revision := policyRevision{Source: source, ID: id, Sequence: sequence, Digest: content.SHA256, Title: command.Edit.Title, ArtifactVersion: command.Edit.ArtifactVersion, Content: content, Workflow: workflow, WorkflowDigest: policyWorkflowDigest(workflow), At: now, Actor: command.Actor}
				record.Revisions = append(record.Revisions, revision)
				record.HeadRevisionID, record.CurrentReviewID, record.UpdatedAt = id, "", now
			}
		} else if command.Review != nil {
			if len(record.Reviews) >= policyHistoryLimit {
				return false, policyRefusal(409, "storage-capacity", "This policy has reached its supported review capacity.")
			}
			id, err := newPolicyEventID("review")
			if err != nil {
				return false, err
			}
			head := policyHead(record)
			record.Reviews = append(record.Reviews, policyReview{ID: id, Sequence: sequence, CandidateRevisionID: head.ID, CandidateDigest: head.Digest, WorkflowDigest: head.WorkflowDigest, Decision: command.Review.Decision, Comment: command.Review.Comment, At: now, Actor: command.Actor})
			record.CurrentReviewID, record.UpdatedAt, receipt.ReviewID = id, now, id
		} else {
			if len(record.Publications) >= policyHistoryLimit {
				return false, policyRefusal(409, "storage-capacity", "This policy has reached its supported publication capacity.")
			}
			id, err := newPolicyEventID("publication")
			if err != nil {
				return false, err
			}
			head := policyHead(record)
			record.Publications = append(record.Publications, policyPublication{ID: id, Sequence: sequence, CandidateRevisionID: head.ID, CandidateDigest: head.Digest, WorkflowDigest: head.WorkflowDigest, Content: head.Content, ArtifactVersion: head.ArtifactVersion, ReviewID: command.Publish.ReviewID, At: now, Actor: command.Actor})
			record.UpdatedAt, receipt.PublicationID = now, id
		}
		ledger.Policies[record.ID] = record
		reply, err := s.reply(ctx, record, ledger.SourceAuthorities[record.ID])
		if err != nil {
			return false, err
		}
		receipt.PolicyID, receipt.Revision, receipt.CandidateDigest = record.ID, reply.Revision, reply.Document.Candidate.Digest
		result = policyMutationReply{Delivery: "committed", Receipt: receipt, Policy: reply}
		raw, _ := json.Marshal(result)
		retained, err := s.artifacts.Put(ctx, raw)
		if err != nil {
			return false, err
		}
		ledger.Operations[command.OperationID] = policyStoredOperation{OperationID: command.OperationID, Kind: command.Kind, PolicyID: record.ID, RequestDigest: command.RequestDigest, Result: retained}
		if err = s.authorized(ctx); err != nil {
			return false, err
		}
		return true, nil
	})
	if err != nil {
		return policyMutationReply{}, err
	}
	return result, nil
}
