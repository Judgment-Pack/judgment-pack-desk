package desk

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
)

type assistantDialectCheckIntent struct {
	HostOperationID string `json:"hostOperationId"`
	Document        []byte `json:"document"`
	Pack            []byte `json:"pack"`
}
type assistantDialectCheckResult struct {
	Report      []byte `json:"report"`
	Unavailable bool   `json:"unavailable"`
}
type assistantDialectPublicationIntent struct {
	HostOperationID string                    `json:"hostOperationId"`
	Request         dialectPublicationRequest `json:"request"`
	Read            assistantDialectRead      `json:"read"`
	CreatedAt       string                    `json:"createdAt"`
}
type assistantDialectPublicationResult struct {
	Status         int                       `json:"status"`
	Answer         *dialectPublicationAnswer `json:"answer,omitempty"`
	MappingCheckID string                    `json:"mappingCheckId,omitempty"`
}
type assistantDialectUnknown struct{}

func (*assistantDialectUnknown) Error() string {
	return "The Dialect check persistence outcome is unknown."
}
func assistantDialectFailure(err error) runcontext.ToolResult {
	var unknown *assistantDialectUnknown
	if errors.As(err, &unknown) {
		return assistantHostFailure(failure.Authored("storage", "The Dialect check outcome is unknown. Inspect retained evidence; no automatic check, model or persistence retry is allowed.", "storage"))
	}
	var authored *failure.Error
	if !errors.As(err, &authored) {
		err = failure.Authored("storage", "The Dialect proposal could not complete. Inspect current source and retained checks before continuing.", "desk")
	}
	return assistantHostFailure(err)
}

// This callback checks only current session/project authority. It never enters
// the private journal or preparation repository while an owning store is locked.
func (e *assistantRunExecution) authorizeAssistantDialect(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := e.ctx.Err(); err != nil {
		return err
	}
	return e.service.server.authorizeAssistantRun(e.request.WithContext(ctx), e.record)
}
func (e *assistantRunExecution) checkAssistantDialect(ctx context.Context, host string, document []byte, pack string) ([]byte, dialectAuthoringReport, string, error) {
	var report dialectAuthoringReport
	op, err := e.boundIntent(ctx, "runtime", "dialect/check", func(string) ([]byte, error) {
		return json.Marshal(assistantDialectCheckIntent{host, document, []byte(pack)})
	})
	if err != nil {
		return nil, report, "", err
	}
	if err = e.recheckAssistantCritic(ctx); err != nil {
		return nil, report, op.ID, err
	}
	if err = e.Dispatch(ctx, op.ID); err != nil {
		return nil, report, op.ID, err
	}
	raw, report, checkErr := e.service.server.checkDialectAuthoring(ctx, document, pack)
	if checkErr == nil && !validAssistantDialectReport(raw) {
		checkErr = errDialectChecksUnavailable
	}
	observed, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	state := "completed"
	if checkErr != nil {
		state = "refused"
	}
	if raw == nil {
		raw = []byte{}
	}
	receipt, _ := json.Marshal(assistantDialectCheckResult{raw, checkErr != nil})
	if err = e.Result(observed, op.ID, state, receipt); err != nil {
		return raw, report, op.ID, err
	}
	return raw, report, op.ID, checkErr
}
func (e *assistantRunExecution) executeAssistantDialectProposal(ctx context.Context, name string, a assistantDialectArguments) (assistantHostObservation, error) {
	var empty assistantHostObservation
	meta, ok := ctx.Value(assistantDialectPreparedKey{}).(*assistantDialectActionMetadata)
	if !ok || meta == nil {
		return empty, errAssistantRunScope
	}
	record, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
	if err != nil {
		return empty, err
	}
	// Only the persisted metadata selects the earlier actual root read. Never
	// recapture a replacement basis after check/preview waits.
	var held assistantDialectRead
	found := false
	for _, op := range record.Operations {
		if op.ID == meta.ReadOperationID {
			var result assistantHostResult
			if readAssistantHostReceipt(op.Result, &result) == nil && result.DialectRead != nil {
				held = *result.DialectRead
				found = true
			}
		}
	}
	if !found {
		return empty, errAssistantRunScope
	}
	hostID := ""
	for _, op := range record.Operations {
		var retained assistantHostIntent
		if op.Kind == "host" && op.Name == name && readAssistantHostReceipt(op.Intent, &retained) == nil && retained.DialectAction != nil && retained.DialectAction.ID == meta.ID {
			hostID = op.ID
		}
	}
	if hostID == "" {
		return empty, errAssistantRunScope
	}
	if name == "propose_dialect_publication" {
		return e.checkAssistantDialectPublication(ctx, hostID, a, held, *meta)
	}
	if err = e.recheckAssistantDialect(ctx, held); err != nil {
		return empty, err
	}
	saved, err := e.service.server.preparationRepository().Get(ctx, a.ID)
	if err != nil {
		return empty, err
	}
	if saved.SHA256 != held.Revision {
		return empty, errAssistantRunScope
	}
	qualification, err := assistantDialectQualification(saved.Document, a, held)
	if err != nil {
		return empty, failure.ToolInput("Use current columns, nonoverlapping fact paths, explicit conversions and known evidence requirements.")
	}
	document, err := assistantDialectMappingDocument(qualification.Mapping)
	if err != nil {
		return empty, err
	}
	raw, report, checkID, err := e.checkAssistantDialect(ctx, hostID, document, string(held.Pack))
	if err != nil {
		return empty, err
	}
	action := newAssistantDialectAction(*meta, held, "update-dialect")
	mapping := a.mapping()
	action.Dialect = &mapping
	action.DialectCheck = &assistantDialectCheckpoint{string(document), raw}
	preview := &assistantDialectPreview{Status: "not-started", Issues: []assistantDialectPreviewIssue{}}
	if report.accepted(true) {
		preview.Status = "complete"
		preview.RecordCount = len(saved.Document.Records)
		doc := saved.Document
		doc.Recipe.Qualification = qualification
		for _, record := range doc.Records {
			row, err := prepareInputRecord(doc, record)
			if err != nil {
				preview.Status = "unavailable"
				row.Issues = append(row.Issues, inputConversionIssue{"record", "The retained record could not be previewed with this mapping."})
			}
			if len(row.Issues) > 0 {
				preview.RecordsWithIssues++
				if len(preview.Issues) < 5 {
					issues := row.Issues
					if len(issues) > 10 {
						issues = issues[:10]
					}
					preview.Issues = append(preview.Issues, assistantDialectPreviewIssue{record.ID, issues})
				}
			}
		}
	}
	if err = e.recheckAssistantDialect(ctx, held); err != nil {
		return empty, err
	}
	out := &assistantDialectActionOutput{Read: held, CheckID: checkID, Action: action, Preview: preview, Ready: report.accepted(true) && preview.Status == "complete"}
	if err = e.recheckAssistantDialect(ctx, held); err != nil {
		return empty, err
	}
	result, err := assistantDialectActionResult(out)
	if err != nil {
		return empty, err
	}
	return assistantHostObservation{Result: result, Output: &assistantDomainOutput{Kind: "dialect-action", DialectAction: out}}, nil
}
func (e *assistantRunExecution) checkAssistantDialectPublication(ctx context.Context, host string, a assistantDialectArguments, held assistantDialectRead, meta assistantDialectActionMetadata) (assistantHostObservation, error) {
	var empty assistantHostObservation
	request := dialectPublicationRequest{Action: "check", Revision: a.Revision, Variant: a.Variant, Version: a.Version, Impact: a.Impact}
	op, err := e.boundIntent(ctx, "runtime", "dialect-publication/check", func(string) ([]byte, error) {
		return json.Marshal(assistantDialectPublicationIntent{host, request, held, meta.CreatedAt})
	})
	if err != nil {
		return empty, err
	}
	if err = e.Dispatch(ctx, op.ID); err != nil {
		return empty, err
	}
	mappingCheckID := ""
	admission := &dialectPublicationCheckAdmission{At: meta.CreatedAt, BasePublication: held.BasePublication, Authorize: e.authorizeAssistantDialect, Recheck: func(ctx context.Context) error { return e.recheckAssistantDialect(ctx, held) }, Check: func(ctx context.Context, document []byte, pack string) (dialectAuthoringReport, error) {
		_, report, id, err := e.checkAssistantDialect(ctx, host, document, pack)
		mappingCheckID = id
		if err == nil && !report.accepted(true) {
			err = failure.ToolInput("Runtime did not accept the exact publication mapping.")
		}
		return report, err
	}}
	status, body := e.service.server.publishCheckedDialectUsing(ctx, a.ID, request, nil, admission)
	receipt := assistantDialectPublicationResult{Status: status, MappingCheckID: mappingCheckID}
	if answer, ok := body.(dialectPublicationAnswer); ok && status == 200 {
		receipt.Answer = &answer
	}
	observed, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	state := "completed"
	if receipt.Answer == nil {
		state = "refused"
	}
	encoded, _ := json.Marshal(receipt)
	if err = e.Result(observed, op.ID, state, encoded); err != nil {
		return empty, &assistantDialectUnknown{}
	}
	if receipt.Answer == nil {
		if status >= 500 || ctx.Err() != nil {
			return empty, &assistantDialectUnknown{}
		}
		return empty, failure.ToolInput("The publication check refused the current source, version, impact or base. Inspect the retained check and read current context before proposing again.")
	}
	out := &assistantDialectActionOutput{Read: held, CheckID: op.ID, MappingCheckID: mappingCheckID}
	if receipt.Answer.Saved {
		out.Published = receipt.Answer
	} else {
		if err = e.recheckAssistantDialect(ctx, held); err != nil {
			return empty, err
		}
		action := newAssistantDialectAction(meta, held, "publish-dialect")
		action.Publication = &assistantPublicationProposal{Variant: a.Variant, Version: a.Version, Impact: a.Impact, Answer: *receipt.Answer}
		out.Action = action
		out.Ready = true
		if err = e.service.server.checkAssistantDialectPublicationEvidence(ctx, *out); err != nil {
			return empty, &assistantDialectUnknown{}
		}
		if err = e.recheckAssistantDialect(ctx, held); err != nil {
			return empty, err
		}
	}
	result, err := assistantDialectActionResult(out)
	if err != nil {
		return empty, err
	}
	return assistantHostObservation{Result: result, Output: &assistantDomainOutput{Kind: "dialect-action", DialectAction: out}}, nil
}
func assistantDialectActionResult(out *assistantDialectActionOutput) (runcontext.ToolResult, error) {
	if out.Published != nil {
		p := out.Published
		return assistantHostJSON(map[string]any{"status": "already-published", "version": p.Publication.Documents[0].Version, "digest": p.Digest, "message": "The existing immutable publication receipt was observed. No new version or job change was made."})
	}
	if out.Action == nil {
		return runcontext.ToolResult{}, errAssistantRunInvalid
	}
	status := "review-required"
	if !out.Ready {
		status = "check-refused"
	}
	value := map[string]any{"status": status, "actionId": out.Action.ID, "message": "Review this exact proposal and its checks in chat. No recipe, records, publication, allowance or job pin was changed."}
	if out.Action.Dialect != nil {
		value["preview"] = out.Preview
		fields, err := assistantTestObject(out.Action.DialectCheck.Report)
		if err != nil {
			return runcontext.ToolResult{}, err
		}
		if fields["compatibility"] != nil {
			value["coverage"] = json.RawMessage(fields["compatibility"])
		}
		value["mappingDigest"] = json.RawMessage(fields["digest"])
	} else {
		value["digest"] = out.Action.Publication.Answer.Digest
		value["version"] = out.Action.Publication.Version
	}
	result, err := assistantHostJSON(value)
	if err != nil {
		return result, err
	}
	return result.WithIsError(!out.Ready), nil
}

func (s *Server) checkAssistantDialectPublicationEvidence(ctx context.Context, out assistantDialectActionOutput) error {
	if out.Published != nil {
		answer := out.Published
		ledger, err := s.readDialectPublicationLedger()
		if err != nil {
			return err
		}
		for _, p := range ledger.Items {
			if p.ID == answer.Publication.ID {
				actual, err := s.publicationAnswer(ctx, p)
				if err != nil {
					return err
				}
				if sameDraftValue(actual, *answer) {
					return nil
				}
			}
		}
		return errAssistantRunConflict
	}
	if out.Action == nil || out.Action.Publication == nil {
		return nil
	}
	proposal := out.Action.Publication
	answer := proposal.Answer
	if answer.Review == nil {
		return errAssistantRunInvalid
	}
	retained, err := s.artifactReview(ctx, *answer.Review)
	if err != nil {
		return err
	}
	review := retained.Review
	pub := answer.Publication
	if pub.Dialect == nil || review.Target != dialectArtifactID(out.Read.ID, proposal.Variant) || review.StorageRevision != out.Read.Revision || review.BasePublication != out.Read.BasePublication || review.ProposedVersion != proposal.Version || review.Impact != proposal.Impact || review.Candidate != artifactReference([]byte(answer.Document)) || !sameDraftValue(review.Dependencies, []retainedArtifactRef{pub.Dialect.Recipe, pub.Dialect.Pack}) {
		return errAssistantRunConflict
	}
	var preview dialectPublicationAnswer
	if json.Unmarshal(review.Preview, &preview) != nil {
		return errAssistantRunInvalid
	}
	preview.Review = answer.Review
	if !sameDraftValue(preview, answer) {
		return errAssistantRunConflict
	}
	for _, ref := range append([]retainedArtifactRef{review.Candidate}, review.Dependencies...) {
		if _, err = s.artifactRepository().Get(ctx, ref); err != nil {
			return err
		}
	}
	return nil
}
