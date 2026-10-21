package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"

	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
)

// Private recipe-read eligibility contains no credentials, values or source
// bodies. It is immutable custody, never a new permission or a human review.
type assistantDialectActionBasis struct {
	Recipe  assistantDialectRecipeRead    `json:"recipe"`
	Records []assistantDialectRecordBasis `json:"records"`
}
type assistantDialectRecordBasis struct {
	ID             string `json:"id"`
	Class          string `json:"class"`
	Researched     bool   `json:"researched"`
	ReviewDecision string `json:"reviewDecision,omitempty"`
	ReviewVersion  int    `json:"reviewVersion,omitempty"`
}

func assistantDialectRecipeView(saved inputPreparationReply) assistantDialectRecipeRead {
	d := saved.Document
	v := assistantDialectRecipeRead{ID: d.ID, Name: d.Name, Version: d.RecipeVersion, Revision: saved.SHA256, Intent: d.Recipe.Intent, Template: d.Recipe.Template}
	if r := d.Recipe.Research; r != nil {
		v.Research = &assistantDialectResearchRead{r.Fields, r.Criteria, r.Limits, r.Execution}
	}
	if q := d.Recipe.Qualification; q != nil {
		v.Qualification = &assistantDialectQualificationRead{q.Kind, q.Key, q.Digest, q.Mapping}
	}
	return v
}
func assistantDialectRecipeBasis(saved inputPreparationReply) assistantDialectActionBasis {
	b := assistantDialectActionBasis{Recipe: assistantDialectRecipeView(saved), Records: []assistantDialectRecordBasis{}}
	for _, r := range saved.Document.Records {
		row := assistantDialectRecordBasis{ID: r.ID, Class: r.Class, Researched: r.Research != nil}
		if r.Review != nil {
			row.ReviewDecision = r.Review.Decision
			row.ReviewVersion = r.Review.RecipeVersion
		}
		b.Records = append(b.Records, row)
	}
	return b
}
func (e *assistantRunExecution) readAssistantDialectActionRecipe(ctx context.Context, id string) (assistantHostObservation, error) {
	if err := e.Authorize(ctx); err != nil {
		return assistantHostObservation{}, err
	}
	saved, err := e.service.server.preparationRepository().Get(ctx, id)
	if err != nil {
		return assistantHostObservation{}, err
	}
	basis := assistantDialectRecipeBasis(saved)
	view := assistantChatJSON(basis.Recipe)
	read := assistantDialectRead{ID: id, Name: saved.Document.Name, Revision: saved.SHA256, RecipeVersion: saved.Document.RecipeVersion, View: view, Recipe: &basis}
	if err = e.Authorize(ctx); err != nil {
		return assistantHostObservation{}, err
	}
	result, err := assistantHostJSON(json.RawMessage(view))
	return assistantHostObservation{Result: result, DialectRead: &read}, err
}
func validAssistantDialectActionRead(read assistantDialectRead) bool {
	b := read.Recipe
	if b == nil || read.PackKey != "" || read.PackPath != "" || read.Declaration != "" || read.Pack != nil || read.Variant != "" || read.BasePublication != "" || !inputPreparationID.MatchString(read.ID) || !assistantRunDigest(read.Revision) || !assistantDialectRecipeText(read.Name, 160) || read.RecipeVersion < 1 || b.Records == nil || len(b.Records) > 1000 {
		return false
	}
	v := b.Recipe
	if v.ID != read.ID || v.Name != read.Name || v.Revision != read.Revision || v.Version != read.RecipeVersion || !bytes.Equal(assistantChatJSON(v), read.View) {
		return false
	}
	seen := map[string]bool{}
	for _, r := range b.Records {
		if r.ID == "" || seen[r.ID] || !contains([]string{"asserted", "generated"}, r.Class) || r.ReviewDecision != "" && !contains([]string{"accepted", "needs-information", "rejected"}, r.ReviewDecision) || (r.ReviewDecision == "") != (r.ReviewVersion == 0) {
			return false
		}
		seen[r.ID] = true
	}
	return true
}
func assistantDialectActionArguments(a assistantDialectRecordArguments) assistantDialectArguments {
	return assistantDialectArguments{ID: a.ID, Revision: a.Revision}
}
func assistantDialectActionFrom(meta assistantDialectActionMetadata, read assistantDialectRead, a assistantDialectRecordArguments) *assistantDialectAction {
	out := newAssistantDialectAction(meta, read, a.Kind)
	out.Patch = a.Patch
	out.Section = a.Section
	out.RecordIDs = a.RecordIDs
	out.RunID = a.RunID
	return out
}
func validateAssistantDialectActionBasis(b assistantDialectActionBasis, a assistantDialectRecordArguments) error {
	bad := failure.ToolInput("Read the current recipe, select existing records of one source class, and review generated records for the current recipe before proposing this action. Nothing was changed or started.")
	if (a.Kind == "research" || a.Kind == "reprocess") && b.Recipe.Research == nil {
		return bad
	}
	if a.Kind == "reprocess" && len(a.RecordIDs) > b.Recipe.Research.Limits.Records {
		return bad
	}
	if a.Kind == "evaluate" && b.Recipe.Qualification == nil {
		return bad
	}
	generated, supplied := false, false
	for _, id := range a.RecordIDs {
		found := false
		for _, row := range b.Records {
			if row.ID != id {
				continue
			}
			found = true
			if a.Kind == "reprocess" && !row.Researched {
				return bad
			}
			if row.Class == "generated" {
				generated = true
				if a.Kind == "evaluate" && (row.ReviewDecision != "accepted" || row.ReviewVersion != b.Recipe.Version) {
					return bad
				}
			} else {
				supplied = true
			}
		}
		if !found {
			return bad
		}
	}
	if a.Kind == "evaluate" && generated && supplied {
		return bad
	}
	return nil
}
func (s *Server) assistantDialectActiveResearch(ctx context.Context, id, run string) (*assistantDialectResearchSummary, error) {
	items, err := s.readInputResearchSummaries(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, item := range assistantDialectResearchSummaries(items) {
		if item.ID == run && contains([]string{"queued", "running"}, item.State) && !item.StopRequested {
			return &item, nil
		}
	}
	return nil, failure.ToolInput("Read current activity and select the exact active research run. No research was stopped.")
}
func (e *assistantRunExecution) executeAssistantDialectAction(ctx context.Context, a assistantDialectRecordArguments) (assistantHostObservation, error) {
	empty := assistantHostObservation{}
	meta, ok := ctx.Value(assistantDialectPreparedKey{}).(*assistantDialectActionMetadata)
	if !ok || meta == nil {
		return empty, errAssistantRunScope
	}
	record, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
	if err != nil {
		return empty, err
	}
	op := assistantDialectOperation(record, meta.ReadOperationID)
	var receipt assistantHostResult
	if op == nil || readAssistantHostReceipt(op.Result, &receipt) != nil || receipt.DialectRead == nil || !validAssistantDialectActionRead(*receipt.DialectRead) {
		return empty, errAssistantRunScope
	}
	read := *receipt.DialectRead
	if err = validateAssistantDialectActionBasis(*read.Recipe, a); err != nil {
		return empty, err
	}
	if err = e.recheckAssistantDialectAction(ctx, read); err != nil {
		return empty, err
	}
	out := &assistantDialectActionOutput{Read: read, Action: assistantDialectActionFrom(*meta, read, a), Ready: true}
	if a.Kind == "stop-research" {
		out.ActiveResearch, err = e.service.server.assistantDialectActiveResearch(ctx, a.ID, a.RunID)
		if err != nil {
			return empty, err
		}
	}
	if err = e.recheckAssistantDialectAction(ctx, read); err != nil {
		return empty, err
	}
	if err = e.Authorize(ctx); err != nil {
		return empty, err
	}
	result, err := assistantDialectActionResult(out)
	return assistantHostObservation{Result: result, Output: &assistantDomainOutput{Kind: "dialect-action", DialectAction: out}}, err
}
func (e *assistantRunExecution) recheckAssistantDialectAction(ctx context.Context, read assistantDialectRead) error {
	if err := e.Authorize(ctx); err != nil {
		return err
	}
	saved, err := e.service.server.preparationRepository().Get(ctx, read.ID)
	if err != nil {
		return err
	}
	if saved.SHA256 != read.Revision || !reflect.DeepEqual(assistantDialectRecipeBasis(saved), *read.Recipe) {
		return errAssistantRunConflict
	}
	return e.Authorize(ctx)
}
func validateAssistantDialectReviewOutput(run assistantRunRecord, out assistantDomainOutput, receipts map[string]assistantHostResult, producer assistantRunOperation, in assistantHostIntent) error {
	p := out.DialectAction
	a, err := readAssistantDialectRecordArguments(producer.Name, in.Arguments)
	if err != nil || producer.State != "completed" || !validAssistantDialectActionRead(p.Read) || p.CheckID != "" || p.MappingCheckID != "" || p.Published != nil || p.Preview != nil || !p.Ready || !reflect.DeepEqual(p.Action, assistantDialectActionFrom(*in.DialectAction, p.Read, a)) || validateAssistantDialectActionBasis(*p.Read.Recipe, a) != nil {
		return errAssistantRunInvalid
	}
	if a.Kind == "stop-research" {
		v := p.ActiveResearch
		if v == nil || v.ID != a.RunID || !contains([]string{"queued", "running"}, v.State) || v.StopRequested {
			return errAssistantRunInvalid
		}
	} else if p.ActiveResearch != nil {
		return errAssistantRunInvalid
	}
	r, ok := receipts[producer.ID]
	expected, err := assistantDialectActionResult(p)
	if !ok || r.DialectRead != nil || r.Tests != nil || r.Read != nil || r.PolicySource != nil || err != nil || !bytes.Equal(expected.Value().JSON(), r.Result) {
		return errAssistantRunInvalid
	}
	return nil
}
