package desk

import (
	"context"
	"encoding/json"
	"sort"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

func assistantDialectShort(s string, max int) string {
	n := 0
	for i, r := range s {
		width := 1
		if r > 0xffff {
			width = 2
		}
		if n+width > max {
			return s[:i]
		}
		n += width
	}
	return s
}
func assistantDialectKeys[V any](m map[string]V) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	if len(out) > 30 {
		out = out[:30]
	}
	return out
}
func assistantDialectRecordView(row preparedInputRecord, version int) map[string]any {
	values := map[string]any{}
	for _, k := range assistantDialectKeys(row.Values) {
		v := row.Values[k]
		values[k] = map[string]any{"text": assistantDialectShort(v, 500), "truncated": runcontext.U16Length(v) > 500}
	}
	item := map[string]any{"id": row.ID, "name": row.Name, "class": row.Class, "status": row.Status, "sourceId": row.SourceID, "row": row.Row, "values": values, "valuesTruncated": len(row.Values) > 30}
	if v := row.Review; v != nil {
		item["review"] = map[string]any{"decision": v.Decision, "note": v.Note, "recipeVersion": v.RecipeVersion, "current": v.RecipeVersion == version}
	}
	if r := row.Research; r != nil {
		fields := map[string]any{}
		for _, k := range assistantDialectKeys(r.Entity.Fields) {
			f := r.Entity.Fields[k]
			sources := []any{}
			for i, s := range f.Sources {
				if i >= 2 {
					break
				}
				sources = append(sources, map[string]any{"id": s.SourceID, "quote": assistantDialectShort(s.Quote, 200), "truncated": runcontext.U16Length(s.Quote) > 200})
			}
			field := map[string]any{"status": f.Status, "sources": sources, "sourcesTruncated": len(f.Sources) > 2}
			if f.Note != "" {
				field["note"] = assistantDialectShort(f.Note, 200)
				field["noteTruncated"] = runcontext.U16Length(f.Note) > 200
			}
			// Preserve typed research separately from the original display strings.
			// An oversized typed value is explicitly unavailable, never string-coerced.
			if f.Value != nil {
				if runcontext.U16Length(string(f.Value)) <= 500 {
					field["value"] = json.RawMessage(f.Value)
				} else {
					field["valueTruncated"] = true
				}
			}
			fields[k] = field
		}
		var website any
		if r.Entity.Website != "" {
			website = r.Entity.Website
		}
		item["research"] = map[string]any{"runId": r.RunID, "recipeVersion": r.RecipeVersion, "entityId": r.Entity.ID, "evidenceUrl": r.Entity.URL, "website": website, "fields": fields, "fieldsTruncated": len(r.Entity.Fields) > 30}
	}
	return item
}
func (e *assistantRunExecution) readAssistantDialectRecords(ctx context.Context, a assistantDialectRecordArguments) (runcontext.ToolResult, error) {
	if err := e.Authorize(ctx); err != nil {
		return runcontext.ToolResult{}, err
	}
	saved, err := e.service.server.preparationRepository().Get(ctx, a.ID)
	if err != nil {
		return runcontext.ToolResult{}, err
	}
	offset, limit := 0, 5
	if a.Offset != nil {
		offset = *a.Offset
	}
	if a.Limit != nil {
		limit = *a.Limit
	}
	items := []any{}
	size := 0
	doc := saved.Document
	for i := offset; i < len(doc.Records) && i < offset+limit; i++ {
		item := assistantDialectRecordView(doc.Records[i], doc.RecipeVersion)
		raw, err := json.Marshal(item)
		if err != nil {
			return runcontext.ToolResult{}, err
		}
		n := runcontext.U16Length(string(raw))
		if len(items) > 0 && size+n > 48000 {
			break
		}
		items = append(items, item)
		size += n
	}
	var next any
	if offset+len(items) < len(doc.Records) {
		next = offset + len(items)
	}
	if err = e.Authorize(ctx); err != nil {
		return runcontext.ToolResult{}, err
	}
	return assistantHostJSON(map[string]any{"id": a.ID, "revision": saved.SHA256, "recipeVersion": doc.RecipeVersion, "total": len(doc.Records), "items": items, "nextOffset": next, "guidance": "Values and quotations are shortened, not independently verified. Typed research stays separate from supplied strings. Review full sources and mark every generated record Ready for evaluation for the current recipe; this read saves no review."})
}

type assistantDialectResearchSummary struct {
	ID            string `json:"id"`
	State         string `json:"state"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
	RecipeVersion int    `json:"recipeVersion"`
	RecordCount   int    `json:"recordCount"`
	StopRequested bool   `json:"stopRequested"`
	Problem       string `json:"problem,omitempty"`
}

func assistantDialectResearchSummaries(items []inputResearchSummary) []assistantDialectResearchSummary {
	out := []assistantDialectResearchSummary{}
	for _, v := range items {
		out = append(out, assistantDialectResearchSummary{v.ID, v.State, v.CreatedAt, v.UpdatedAt, v.RecipeVersion, v.RecordCount, v.StopRequested, assistantDialectShort(v.Problem, 500)})
	}
	return out
}
func (e *assistantRunExecution) readAssistantDialectActivity(ctx context.Context, a assistantDialectRecordArguments) (runcontext.ToolResult, error) {
	if err := e.Authorize(ctx); err != nil {
		return runcontext.ToolResult{}, err
	}
	s := e.service.server
	research, err := s.readInputResearchSummaries(ctx, a.ID)
	if err != nil {
		return runcontext.ToolResult{}, err
	}
	if err = e.Authorize(ctx); err != nil {
		return runcontext.ToolResult{}, err
	}
	evaluations, err := s.readInputEvaluationSummaries(ctx, a.ID)
	if err != nil {
		return runcontext.ToolResult{}, err
	}
	if err = e.Authorize(ctx); err != nil {
		return runcontext.ToolResult{}, err
	}
	saved, err := s.preparationRepository().Get(ctx, a.ID)
	if err != nil {
		return runcontext.ToolResult{}, err
	}
	if len(research) > 10 {
		research = research[:10]
	}
	if len(evaluations) > 5 {
		evaluations = evaluations[:5]
	}
	for i := range evaluations {
		evaluations[i].Problem = assistantDialectShort(evaluations[i].Problem, 500)
	}
	events := saved.Document.Activity
	if len(events) > 20 {
		events = events[len(events)-20:]
	}
	if events == nil {
		events = []inputActivity{}
	}
	reviews := []any{}
	retained := saved.Document.Reviews
	if len(retained) > 20 {
		retained = retained[len(retained)-20:]
	}
	for _, r := range retained {
		reviews = append(reviews, map[string]any{"recordId": r.Review.RecordID, "decision": r.Review.Decision, "at": r.Review.At, "recipeVersion": r.Review.RecipeVersion})
	}
	if err = e.Authorize(ctx); err != nil {
		return runcontext.ToolResult{}, err
	}
	return assistantHostJSON(map[string]any{"id": a.ID, "revision": saved.SHA256, "events": events, "reviews": reviews, "research": assistantDialectResearchSummaries(research), "evaluations": evaluations, "guidance": "This is local activity, not a complete or signed audit trail. Running research is a separately admitted Desk worker from this chat. Reading never recovers, starts, stops or retries it."})
}
