package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

func assistantRecordRead(t *testing.T, e *assistantRunExecution, x *assistantExecution, saved inputPreparationReply) {
	t.Helper()
	r := assistantRecipeInvoke(t, e, x, "recipe-read", "read_dialect_recipe", string(assistantChatJSON(map[string]string{"id": saved.Document.ID})))
	if r.Err() {
		t.Fatal(r.Value().Text())
	}
}
func TestAssistantDialectRecordActionGuards(t *testing.T) {
	for _, mode := range []string{"no-read", "child-read", "stale", "no-research", "no-qualification", "missing-record", "supplied-reprocess", "reprocess-limit", "mixed-evaluation", "unreviewed", "stale-review", "inactive-run", "plan", "child-proposal"} {
		t.Run(mode, func(t *testing.T) {
			phase := "work"
			if mode == "plan" {
				phase = "plan"
			}
			s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", phase)
			saved := assistantRecordSeed(t, s)
			doc := saved.Document
			kind := "evaluate"
			switch mode {
			case "no-research":
				doc.Recipe.Research = nil
				kind = "research"
			case "no-qualification":
				doc.Recipe.Qualification = nil
			case "unreviewed":
				doc.Records[1].Review = nil
			case "stale-review":
				doc.Records[1].Review.RecipeVersion = 1
			case "reprocess-limit":
				doc.Recipe.Research.Limits.Records = 1
				kind = "reprocess"
			case "supplied-reprocess":
				kind = "reprocess"
			case "inactive-run":
				kind = "stop-research"
			}
			if !reflect.DeepEqual(doc, saved.Document) || contains([]string{"unreviewed", "stale-review", "reprocess-limit"}, mode) {
				var err error
				saved, err = s.preparationRepository().CompareAndSwap(t.Context(), doc, saved.SHA256)
				assistantRunMust(t, err)
			}
			x := assistantExecutionOpen(t, s, e)
			if mode == "child-read" {
				tool := assistantGraphFind(t, x.Run.HostTools, "read_dialect_recipe")
				r, err := tool.Execute(runcontext.WithCallID(t.Context(), "child-read"), assistantChatJSON(map[string]string{"id": saved.Document.ID}))
				assistantRunMust(t, err)
				if r.Err() {
					t.Fatal(r.Value().Text())
				}
			} else if mode != "no-read" {
				assistantRecordRead(t, e, x, saved)
			}
			if mode == "stale" {
				d := saved.Document
				d.Name = "Changed after explicit read"
				_, err := s.preparationRepository().CompareAndSwap(t.Context(), d, saved.SHA256)
				assistantRunMust(t, err)
			}
			if mode == "inactive-run" {
				root, err := s.inputResearchRoot(false)
				assistantRunMust(t, err)
				defer root.Close()
				rr, err := openInputResearch(root, assistantRecordRun, false)
				assistantRunMust(t, err)
				defer rr.Close()
				_, err = s.researchJournal(rr, assistantRecordRun).Update(t.Context(), func(run *inputResearchRun) error { run.State = "complete"; return nil })
				assistantRunMust(t, err)
			}
			raw := assistantRecordArgs(saved, kind)
			if mode == "missing-record" {
				raw = strings.Replace(raw, saved.Document.Records[1].ID, "row_"+strings.Repeat("9", 64), 1)
			}
			if mode == "supplied-reprocess" {
				raw = strings.Replace(raw, saved.Document.Records[1].ID, saved.Document.Records[0].ID, 1)
			}
			if mode == "mixed-evaluation" || mode == "reprocess-limit" {
				var a map[string]any
				assistantRunMust(t, json.Unmarshal([]byte(raw), &a))
				a["recordIds"] = []string{saved.Document.Records[0].ID, saved.Document.Records[1].ID}
				raw = string(assistantChatJSON(a))
			}
			var r runcontext.ToolResult
			if mode == "child-proposal" || mode == "plan" || mode == "no-read" || mode == "child-read" {
				tools, err := e.assistantDialectRecordTools()
				assistantRunMust(t, err)
				ctx := runcontext.WithCallID(t.Context(), "direct-proposal")
				if mode != "child-proposal" {
					if mode != "plan" {
						assistantPolicyEvent(t, e, "direct-proposal", "propose_dialect_action", []byte(raw))
					}
					ctx = context.WithValue(ctx, assistantHostRootKey{}, true)
				}
				r, err = assistantGraphFind(t, tools.tools, "propose_dialect_action").Execute(ctx, []byte(raw))
				assistantRunMust(t, err)
			} else {
				r = assistantRecipeInvoke(t, e, x, "proposal", "propose_dialect_action", raw)
			}
			if !r.Err() || len(assistantExecutionRecord(t, e).DomainOutputs) != 0 {
				t.Fatal("action guard accepted invalid basis", mode, r.Value().Text())
			}
		})
	}
}

type assistantRecordQueuedRepository struct {
	inputPreparationRepository
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (r *assistantRecordQueuedRepository) Get(ctx context.Context, id string) (inputPreparationReply, error) {
	r.calls.Add(1)
	select {
	case r.entered <- struct{}{}:
	default:
	}
	select {
	case <-r.release:
	case <-ctx.Done():
		return inputPreparationReply{}, ctx.Err()
	}
	return r.inputPreparationRepository.Get(ctx, id)
}
func TestAssistantDialectRecordQueuedAuthority(t *testing.T) {
	for _, name := range []string{"read_dialect_records", "read_dialect_activity", "propose_dialect_action"} {
		for _, mode := range []string{"revoked", "canceled", "source"} {
			t.Run(name+"/"+mode, func(t *testing.T) {
				s, token, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
				saved := assistantRecordSeed(t, s)
				x := assistantExecutionOpen(t, s, e)
				if name == "propose_dialect_action" {
					assistantRecordRead(t, e, x, saved)
				}
				base := s.preparationRepository()
				queue := &assistantRecordQueuedRepository{inputPreparationRepository: base, entered: make(chan struct{}, 1), release: make(chan struct{})}
				s.inputPreparations = queue
				raw := assistantChatJSON(map[string]string{"id": saved.Document.ID})
				if name == "propose_dialect_action" {
					raw = []byte(assistantRecordArgs(saved, "research"))
				}
				call := "queued-call"
				assistantPolicyEvent(t, e, call, name, raw)
				ctx, cancel := context.WithCancel(context.WithValue(runcontext.WithCallID(t.Context(), call), assistantHostRootKey{}, true))
				defer cancel()
				type answer struct {
					result runcontext.ToolResult
					err    error
				}
				done := make(chan answer, 1)
				go func() {
					r, err := assistantGraphFind(t, x.Run.HostTools, name).Execute(ctx, raw)
					done <- answer{r, err}
				}()
				assistantMappingAwait(t, queue.entered)
				switch mode {
				case "revoked":
					s.sessions.revoke(token)
				case "canceled":
					cancel()
				case "source":
					doc := saved.Document
					doc.Name = "Fresh queued read"
					_, err := base.CompareAndSwap(t.Context(), doc, saved.SHA256)
					assistantRunMust(t, err)
				}
				close(queue.release)
				select {
				case a := <-done:
					assistantRunMust(t, a.err)
					wantRefusal := mode != "source" || name == "propose_dialect_action"
					if a.result.Err() != wantRefusal {
						t.Fatal("queued authority/source outcome wrong", a.result.Value().Text())
					}
					if !wantRefusal && !strings.Contains(a.result.Content[0].Text(), "revision") {
						t.Fatal("fresh queued source not read")
					}
				case <-time.After(5 * time.Second):
					t.Fatal("queued read did not drain")
				}
				if len(assistantExecutionRecord(t, e).DomainOutputs) != 0 {
					t.Fatal("queued invalid basis retained action")
				}
			})
		}
	}
}
func TestAssistantDialectRecordProjectionCurrentAndHistory(t *testing.T) {
	for _, mode := range []string{"source", "run", "history"} {
		t.Run(mode, func(t *testing.T) {
			s, token, e := assistantRecipeBrowserFixture(t, "http://127.0.0.1:1")
			saved := assistantRecordSeed(t, s)
			x := assistantExecutionOpen(t, s, e)
			assistantRecordRead(t, e, x, saved)
			kind := "research"
			if mode == "run" {
				kind = "stop-research"
			}
			r := assistantRecipeInvoke(t, e, x, "proposal", "propose_dialect_action", assistantRecordArgs(saved, kind))
			if r.Err() {
				t.Fatal(r.Value().Text())
			}
			record := assistantMappingObserve(t, s, e, 1)
			cp := *record.Checkpoint
			cp.Status = "complete"
			assistantRunMust(t, e.Checkpoint(t.Context(), cp))
			assistantRunMust(t, e.writer.State(t.Context(), "complete"))
			assistantRunMust(t, s.prepareAssistantChatProjection(e.request.Clone(t.Context()), record.ID))
			if mode == "history" {
				assistantRunMust(t, e.ProjectChat(t.Context()))
			}
			if mode == "run" {
				root, err := s.inputResearchRoot(false)
				assistantRunMust(t, err)
				defer root.Close()
				rr, err := openInputResearch(root, assistantRecordRun, false)
				assistantRunMust(t, err)
				defer rr.Close()
				_, err = s.researchJournal(rr, assistantRecordRun).Update(t.Context(), func(run *inputResearchRun) error { run.StopRequested = true; return nil })
				assistantRunMust(t, err)
			} else {
				doc := saved.Document
				doc.Name = "Changed after prepared card"
				_, err := s.preparationRepository().CompareAndSwap(t.Context(), doc, saved.SHA256)
				assistantRunMust(t, err)
			}
			err := s.writeAssistantChatProjection(e.request.Clone(t.Context()), record.ID)
			if (err == nil) != (mode == "history") {
				t.Fatal("current versus historical projection currency wrong", err)
			}
			if w := policyCall(s, token, "GET", "/api/assistant-runs/"+record.ID, nil); w.Code != 200 {
				t.Fatal("feature freshness blocked raw historical GET")
			}
			assistantRunMust(t, assistantRecipeImport(t, e, assistantExecutionRecord(t, e)))
		})
	}
}
func TestAssistantDialectRecordBoundsAndTypedValues(t *testing.T) {
	s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
	saved := assistantRecordSeed(t, s)
	x := assistantExecutionOpen(t, s, e)
	raw := `{"id":"` + saved.Document.ID + `","offset":0,"limit":1}`
	r := assistantRecipeInvoke(t, e, x, "bounded", "read_dialect_records", raw)
	if r.Err() {
		t.Fatal(r.Value().Text())
	}
	var view struct {
		Items      []map[string]json.RawMessage `json:"items"`
		NextOffset int                          `json:"nextOffset"`
		Total      int                          `json:"total"`
	}
	assistantRunMust(t, json.Unmarshal([]byte(r.Content[0].Text()), &view))
	if len(view.Items) != 1 || view.NextOffset != 1 || view.Total != 2 || !bytes.Contains(view.Items[0]["values"], []byte(`"truncated":true`)) {
		t.Fatal("record bounds/truncation lost")
	}
	r = assistantRecipeInvoke(t, e, x, "typed", "read_dialect_records", `{"id":"`+saved.Document.ID+`","offset":1}`)
	if r.Err() {
		t.Fatal(r.Value().Text())
	}
	text := r.Content[0].Text()
	for _, needle := range []string{`"text":"00123"`, `"value":false`, `"value":0`, `"value":""`, `"current":true`, `"truncated":true`, `"runId":"` + assistantRecordRun + `"`} {
		if !strings.Contains(text, needle) {
			t.Fatal("typed original, provenance or review lost", needle, text)
		}
	}
	if strings.Contains(text, "fixture-private-model") {
		t.Fatal("records leaked recipe binding")
	}
}
func TestAssistantDialectRecordUnavailableHistory(t *testing.T) {
	s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
	saved := assistantRecordSeed(t, s)
	x := assistantExecutionOpen(t, s, e)
	s.researchJournals = newMemoryResearchJournals()
	r := assistantRecipeInvoke(t, e, x, "unavailable", "read_dialect_activity", `{"id":"`+saved.Document.ID+`"}`)
	if !r.Err() {
		t.Fatal("unsupported journal discovery became empty history")
	}
}

func TestAssistantDialectRecordDependencyLimits(t *testing.T) {
	b := assistantDialectActionBasis{Recipe: assistantDialectRecipeRead{Version: 2, Research: &assistantDialectResearchRead{Limits: inputResearchLimits{Records: 1}}, Qualification: &assistantDialectQualificationRead{}}, Records: []assistantDialectRecordBasis{{ID: "a", Class: "generated", Researched: true, ReviewDecision: "accepted", ReviewVersion: 2}, {ID: "b", Class: "generated", Researched: true, ReviewDecision: "accepted", ReviewVersion: 2}}}
	for _, a := range []assistantDialectRecordArguments{{Kind: "reprocess", RecordIDs: []string{"a"}}, {Kind: "evaluate", RecordIDs: []string{"a", "b"}}} {
		if err := validateAssistantDialectActionBasis(b, a); err != nil {
			t.Fatal("valid dependency refused", err)
		}
	}
	if validateAssistantDialectActionBasis(b, assistantDialectRecordArguments{Kind: "reprocess", RecordIDs: []string{"a", "b"}}) == nil {
		t.Fatal("saved reprocess record limit ignored")
	}
}
func TestAssistantDialectRecordUnknownAndReplay(t *testing.T) {
	s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
	saved := assistantRecordSeed(t, s)
	x := assistantExecutionOpen(t, s, e)
	assistantRecordRead(t, e, x, saved)
	name, call := "propose_dialect_action", "unknown-delivery"
	raw := []byte(assistantRecordArgs(saved, "research"))
	assistantPolicyEvent(t, e, call, name, raw)
	run := assistantExecutionRecord(t, e)
	in := assistantHostIntent{CallID: call, Root: true, Arguments: raw}
	assistantRunMust(t, prepareAssistantDialectAction(run, name, &in, 0))
	intent, _ := json.Marshal(in)
	id := assistantDialectOperationID(in)
	assistantRunMust(t, e.writer.Intent(t.Context(), assistantRunOperation{ID: id, CallID: id, Kind: "host", Name: name, Intent: intent}))
	assistantRunMust(t, e.Dispatch(t.Context(), id))
	ctx := context.WithValue(runcontext.WithCallID(t.Context(), call), assistantHostRootKey{}, true)
	result, err := assistantGraphFind(t, x.Run.HostTools, name).Execute(ctx, raw)
	assistantRunMust(t, err)
	if !result.Err() {
		t.Fatal("uncertain action redelivery produced a new card")
	}
	retained := assistantExecutionRecord(t, e)
	if len(retained.DomainOutputs) != 0 || len(retained.Operations) != len(run.Operations)+1 {
		t.Fatal("uncertain action redelivery ran work")
	}
	assistantRunMust(t, assistantRecipeImport(t, e, retained))
}
func TestAssistantDialectRecordRawReadCustody(t *testing.T) {
	s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
	saved := assistantRecordSeed(t, s)
	x := assistantExecutionOpen(t, s, e)
	assistantRecordRead(t, e, x, saved)
	record := assistantExecutionRecord(t, e)
	for _, op := range record.Operations {
		if op.Kind != "host" || op.Name != "read_dialect_recipe" {
			continue
		}
		var in assistantHostIntent
		var receipt assistantHostResult
		assistantRunMust(t, readAssistantHostReceipt(op.Intent, &in))
		assistantRunMust(t, readAssistantHostReceipt(op.Result, &receipt))
		assistantRunMust(t, validateAssistantDialectReadReceipt(op, in, receipt))
		op.Result = bytes.Replace(op.Result, []byte(`"researched":false`), []byte(`"researched":null`), 1)
		var collapsed assistantHostResult
		assistantRunMust(t, json.Unmarshal(op.Result, &collapsed))
		if validateAssistantDialectReadReceipt(op, in, collapsed) == nil {
			t.Fatal("raw null eligibility survived typed decoding")
		}
		return
	}
	t.Fatal("recipe read receipt missing")
}

func TestAssistantDialectRecordFieldBounds(t *testing.T) {
	row := preparedInputRecord{Values: map[string]string{}, Research: &inputResearchRecord{Entity: inputResearchEntity{Fields: map[string]inputResearchValue{}}}}
	for i := 0; i < 31; i++ {
		key := fmt.Sprintf("field%02d", i)
		row.Values[key] = strings.Repeat("v", 501)
		row.Research.Entity.Fields[key] = inputResearchValue{Status: "supported", Note: strings.Repeat("n", 201), Value: json.RawMessage(`false`), Sources: []inputResearchCitation{{SourceID: "a", Quote: strings.Repeat("q", 201)}, {SourceID: "b", Quote: "second"}, {SourceID: "c", Quote: "omitted"}}}
	}
	raw := assistantChatJSON(assistantDialectRecordView(row, 1))
	var item struct {
		Values map[string]struct {
			Text      string `json:"text"`
			Truncated bool   `json:"truncated"`
		} `json:"values"`
		ValuesTruncated bool `json:"valuesTruncated"`
		Research        struct {
			Fields map[string]struct {
				Note          string `json:"note"`
				NoteTruncated bool   `json:"noteTruncated"`
				Sources       []struct {
					Quote     string `json:"quote"`
					Truncated bool   `json:"truncated"`
				} `json:"sources"`
				SourcesTruncated bool `json:"sourcesTruncated"`
			} `json:"fields"`
			FieldsTruncated bool `json:"fieldsTruncated"`
		} `json:"research"`
	}
	assistantRunMust(t, json.Unmarshal(raw, &item))
	if len(item.Values) != 30 || !item.ValuesTruncated || len(item.Research.Fields) != 30 || !item.Research.FieldsTruncated {
		t.Fatal("field enumeration bounds missing")
	}
	for _, v := range item.Values {
		if len(v.Text) != 500 || !v.Truncated {
			t.Fatal("original string truncation missing")
		}
	}
	for _, v := range item.Research.Fields {
		if len(v.Note) != 200 || !v.NoteTruncated || len(v.Sources) != 2 || !v.SourcesTruncated || len(v.Sources[0].Quote) != 200 || !v.Sources[0].Truncated {
			t.Fatal("source/note limits missing")
		}
	}
}

type assistantRecordSummaryOwner struct {
	researchJournalProvider
	items   []inputResearchSummary
	entered chan struct{}
	release chan struct{}
}

func (r assistantRecordSummaryOwner) Summaries(ctx context.Context, id string) ([]inputResearchSummary, error) {
	select {
	case r.entered <- struct{}{}:
	default:
	}
	select {
	case <-r.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return append([]inputResearchSummary{}, r.items...), nil
}
func TestAssistantDialectRecordQueuedHistory(t *testing.T) {
	for _, mode := range []string{"parity", "revoked", "source"} {
		t.Run(mode, func(t *testing.T) {
			s, token, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
			saved := assistantRecordSeed(t, s)
			x := assistantExecutionOpen(t, s, e)
			args := assistantDialectRecordArguments{ID: saved.Document.ID}
			baseline, err := e.readAssistantDialectActivity(t.Context(), args)
			assistantRunMust(t, err)
			items, err := s.readInputResearchSummaries(t.Context(), saved.Document.ID)
			assistantRunMust(t, err)
			entered, release := make(chan struct{}, 1), make(chan struct{})
			s.researchJournals = assistantRecordSummaryOwner{newMemoryResearchJournals(), items, entered, release}
			call, name := "history-queued", "read_dialect_activity"
			raw := assistantChatJSON(map[string]string{"id": saved.Document.ID})
			assistantPolicyEvent(t, e, call, name, raw)
			ctx := context.WithValue(runcontext.WithCallID(t.Context(), call), assistantHostRootKey{}, true)
			type answer struct {
				result runcontext.ToolResult
				err    error
			}
			done := make(chan answer, 1)
			go func() {
				r, err := assistantGraphFind(t, x.Run.HostTools, name).Execute(ctx, raw)
				done <- answer{r, err}
			}()
			assistantMappingAwait(t, entered)
			expected := saved.SHA256
			switch mode {
			case "revoked":
				s.sessions.revoke(token)
			case "source":
				doc := saved.Document
				doc.Name = "New after history queue"
				current, err := s.preparationRepository().CompareAndSwap(t.Context(), doc, saved.SHA256)
				assistantRunMust(t, err)
				expected = current.SHA256
			}
			close(release)
			select {
			case a := <-done:
				assistantRunMust(t, a.err)
				if a.result.Err() != (mode == "revoked") {
					t.Fatal("history queue authority mismatch", a.result.Value().Text())
				}
				if mode == "parity" && !bytes.Equal(a.result.Value().JSON(), baseline.Value().JSON()) {
					t.Fatal("injected/default history owner parity lost")
				}
				if mode == "source" && !strings.Contains(a.result.Content[0].Text(), expected) {
					t.Fatal("activity returned preparation from before queued history")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("history queue did not drain")
			}
		})
	}
}

func TestAssistantDialectRecordLegacyRecipeObservation(t *testing.T) {
	s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
	saved := assistantRecordSeed(t, s)
	x := assistantExecutionOpen(t, s, e)
	assistantRecordRead(t, e, x, saved)
	record := assistantExecutionRecord(t, e)
	// Previously completed recipe observations retain their original tool text.
	// Absence of new private eligibility custody cannot authorize a new action.
	for i, op := range record.Operations {
		if op.Kind == "host" && op.Name == "read_dialect_recipe" {
			var receipt assistantHostResult
			assistantRunMust(t, readAssistantHostReceipt(op.Result, &receipt))
			receipt.DialectRead = nil
			record.Operations[i].Result = assistantChatJSON(receipt)
		}
	}
	assistantRunMust(t, assistantRecipeImport(t, e, record))
	in := assistantHostIntent{Root: true, CallID: "new-action", Arguments: []byte(assistantRecordArgs(saved, "research"))}
	if prepareAssistantDialectAction(record, "propose_dialect_action", &in, 0) == nil {
		t.Fatal("legacy observation restored new action authority")
	}
}
