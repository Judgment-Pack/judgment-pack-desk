package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

const assistantRecordRun = "research_11111111111111111111111111111111"

func assistantRecordSeed(t *testing.T, s *Server) inputPreparationReply {
	t.Helper()
	saved := assistantMappingSeed(t, s)
	doc := saved.Document
	r := researchRecipeFixture()
	r.Target = "records"
	r.AI.Connection = testAIAlpha
	r.AI.Model = "fixture-private-model"
	doc.Recipe.Research = &r
	doc.Recipe.Intent = "Original portable recipe"
	doc.RecipeVersion = 2
	generated := preparedInputRecord{ID: "row_" + strings.Repeat("2", 64), Name: "Generated record", Class: "generated", Status: "ready", Values: map[string]string{"value": "00123", "flag": "false"}, SourceID: "research-source", Review: &inputRecordReview{RecordID: "row_" + strings.Repeat("2", 64), RecipeVersion: 2, Decision: "accepted", Note: "Human review"}, Research: &inputResearchRecord{RunID: assistantRecordRun, RecipeVersion: 1, Entity: inputResearchEntity{ID: "entity-one", Kind: "record", Name: "Original generated name", URL: "https://example.invalid/original", Fields: map[string]inputResearchValue{"flag": {Status: "supported", Value: json.RawMessage(`false`), Sources: []inputResearchCitation{{SourceID: "source-one", Quote: strings.Repeat("q", 201)}}}, "zero": {Status: "supported", Value: json.RawMessage(`0`), Sources: []inputResearchCitation{}}, "empty": {Status: "supported", Value: json.RawMessage(`""`), Sources: []inputResearchCitation{}}}}}}
	generated.Review.RecordDigest = inputRecordDigest(generated)
	generated.Review.At = "2026-10-20T00:00:00Z"
	doc.Records = append(doc.Records, generated)
	doc.Records[0].Values["long"] = strings.Repeat("😀", 251)
	saved, err := s.preparationRepository().CompareAndSwap(t.Context(), doc, saved.SHA256)
	assistantRunMust(t, err)
	root, err := s.inputResearchRoot(true)
	assistantRunMust(t, err)
	defer root.Close()
	rr, err := openInputResearch(root, assistantRecordRun, true)
	assistantRunMust(t, err)
	defer rr.Close()
	run := inputResearchRun{Version: 1, ID: assistantRecordRun, PreparationID: doc.ID, Revision: saved.SHA256, RecipeVersion: 2, Recipe: r, State: "running", CreatedAt: "2026-10-20T00:00:00Z", UpdatedAt: "2026-10-20T00:00:00Z", Records: []inputResearchEntity{}, Sources: []inputResearchSource{}, Attempts: []inputResearchAttempt{}, Seeds: []string{}, Companies: []inputResearchEntity{}}
	assistantRunMust(t, s.researchJournal(rr, assistantRecordRun).Create(t.Context(), run))
	// A separate worker owns this active journal. Keep its real execution lease
	// so the server's normal recovery scanner must leave it alone; no provider runs.
	worker, err := rr.OpenFile("worker.lock", os.O_CREATE|os.O_RDWR, 0600)
	assistantRunMust(t, err)
	assistantRunMust(t, waitPrivateDataLock(t.Context(), worker, true))
	t.Cleanup(func() { worker.Close() })

	er, err := s.inputEvaluationRoot(true)
	assistantRunMust(t, err)
	defer er.Close()
	evaluation := inputEvaluation{Version: 1, ID: "eval_" + strings.Repeat("3", 32), PreparationID: doc.ID, Revision: saved.SHA256, RecipeVersion: 2, CreatedAt: "2026-10-20T00:00:00Z", ConfirmedAt: "2026-10-20T00:00:01Z", State: "complete", Rows: []inputEvaluationRow{{RecordID: doc.Records[0].ID, State: "complete"}}}
	br, err := openInputEvaluation(er, evaluation.ID, true)
	assistantRunMust(t, err)
	defer br.Close()
	_, err = s.evaluationJournal(br, evaluation.ID).CompareAndSwap(t.Context(), evaluation, "")
	assistantRunMust(t, err)
	return saved
}
func assistantRecordArgs(saved inputPreparationReply, kind string) string {
	a := map[string]any{"id": saved.Document.ID, "revision": saved.SHA256, "kind": kind}
	switch kind {
	case "update-recipe":
		a["patch"] = json.RawMessage(`{"intent":"Review exact <record>& 中文","research":{"fields":[{"name":"flag","description":"Boolean, not supplied text","type":"boolean"}],"criteria":[],"execution":{"approach":"standard","concurrency":0}}}`)
	case "remove-recipe":
		a["section"] = "qualification"
	case "reprocess":
		a["recordIds"] = []string{saved.Document.Records[1].ID}
	case "evaluate":
		a["recordIds"] = []string{saved.Document.Records[1].ID}
	case "stop-research":
		a["runId"] = assistantRecordRun
	}
	return string(assistantChatJSON(a))
}
func TestAssistantDialectRecordActualAPIProducer(t *testing.T) {
	var step atomic.Int32
	var saved inputPreparationReply
	names := []string{"read_dialect_recipe", "read_dialect_records", "read_dialect_activity", "propose_dialect_action", "propose_dialect_action", "propose_dialect_action", "propose_dialect_action", "propose_dialect_action", "propose_dialect_action"}
	kinds := []string{"update-recipe", "remove-recipe", "research", "reprocess", "stop-research", "evaluate"}
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		n := int(step.Add(1))
		finish := "tool_calls"
		var delta map[string]any
		if n <= len(names) {
			args := string(assistantChatJSON(map[string]string{"id": saved.Document.ID}))
			if n > 3 {
				args = assistantRecordArgs(saved, kinds[n-4])
			}
			delta = map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "same-provider-call", "type": "function", "function": map[string]any{"name": names[n-1], "arguments": args}}}}
		} else {
			delta = map[string]any{"role": "assistant", "content": "Review only. Nothing was changed, started, stopped or evaluated."}
			finish = "stop"
		}
		raw, _ := json.Marshal(map[string]any{"id": "records-fixture", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}}})
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: "+string(raw)+"\n\ndata: {\"id\":\"records-fixture\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\""+finish+"\"}]}\n\ndata: [DONE]\n\n")
	})
	s, token, e := assistantRecipeBrowserFixture(t, u.server.URL)
	saved = assistantRecordSeed(t, s)
	x := assistantExecutionOpen(t, s, e)
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	assistantRunMust(t, x.Adapter.Run(ctx, x.Run))
	if step.Load() != 10 {
		t.Fatal("actual nine-tool read/action loop did not complete", step.Load())
	}
	for _, op := range assistantExecutionRecord(t, e).Operations {
		if op.Kind == "host" && op.State != "completed" {
			t.Errorf("host refused %s: %s", op.Name, op.Result)
		}
	}
	record := assistantMappingObserve(t, s, e, 6)
	after, err := s.preparationRepository().Get(t.Context(), saved.Document.ID)
	assistantRunMust(t, err)
	if after.SHA256 != saved.SHA256 {
		t.Fatal("review-only actions changed preparation")
	}
	research, err := s.readInputResearchSummaries(t.Context(), saved.Document.ID)
	assistantRunMust(t, err)
	if len(research) != 1 || research[0].State != "running" || research[0].StopRequested {
		t.Fatal("action changed research")
	}
	evaluations, err := s.readInputEvaluationSummaries(t.Context(), saved.Document.ID)
	assistantRunMust(t, err)
	if len(evaluations) != 1 || evaluations[0].State != "complete" {
		t.Fatal("action changed evaluations")
	}
	assistantRunMust(t, e.writer.State(t.Context(), "complete"))
	assistantRunMust(t, e.ProjectChat(t.Context()))
	assistantRunMust(t, e.ProjectChat(t.Context()))
	if step.Load() != 10 {
		t.Fatal("projection replay ran model")
	}
	for key, raw := range map[string][]byte{"DESK_DIALECT_RECORD_READER_CHAT": policyWorkspaceRead(t, s, token, "/api/conversations").Content, "DESK_DIALECT_RECORD_READER_SOURCE": assistantChatJSON(saved)} {
		if p := os.Getenv(key); p != "" {
			assistantRunMust(t, os.WriteFile(p, raw, 0600))
		}
	}
	for _, op := range record.Operations {
		if op.Kind == "host" && contains([]string{"read_dialect_records", "read_dialect_activity"}, op.Name) {
			var receipt assistantHostResult
			assistantRunMust(t, readAssistantHostReceipt(op.Result, &receipt))
			v, err := runcontext.ParseJSON(receipt.Result)
			assistantRunMust(t, err)
			result := runcontext.ReadToolResult(v)
			if result.Err() {
				t.Fatal(result.Value().Text())
			}
			key := "DESK_DIALECT_RECORD_READER_RECORDS"
			if op.Name == "read_dialect_activity" {
				key = "DESK_DIALECT_RECORD_READER_ACTIVITY"
			}
			if p := os.Getenv(key); p != "" {
				assistantRunMust(t, os.WriteFile(p, []byte(result.Content[0].Text()), 0600))
			}
		}
	}
	// Repeated output preserves exact existing actions, applied state and
	// unrelated raw cards; a same-ID null is not an omitted optional member.
	actions := []json.RawMessage{}
	for _, out := range record.DomainOutputs {
		actions = append(actions, assistantChatJSON(out.DialectAction.Action))
	}
	first, _ := assistantTestObject(actions[0])
	first["applied"] = []byte(`{"at":"2026-10-20T00:00:00Z"}`)
	actions[0] = assistantChatEncodeObject(first)
	actions = append(actions, []byte(`{"id":"unrelated-card","opaque":{"Flag":false,"flag":0}}`))
	fields := map[string]json.RawMessage{"inputActions": assistantChatEncodeArray(actions)}
	original := bytes.Clone(fields["inputActions"])
	assistantRunMust(t, appendAssistantDialectActions(fields, record.DomainOutputs))
	if !bytes.Equal(original, fields["inputActions"]) {
		t.Fatal("exact replay changed applied or unrelated raw actions")
	}
	for _, raw := range []json.RawMessage{[]byte(`null`), []byte(`{"intent":"Review exact <record>& 中文","research":{"fields":[{"name":"flag","description":"Boolean, not supplied text","type":"boolean"}],"criteria":[],"execution":{"approach":"standard","concurrency":0},"target":null}}`)} {
		bad := append([]json.RawMessage(nil), actions...)
		changed, _ := assistantTestObject(bad[0])
		changed["patch"] = raw
		bad[0] = assistantChatEncodeObject(changed)
		fields["inputActions"] = assistantChatEncodeArray(bad)
		if appendAssistantDialectActions(fields, record.DomainOutputs) == nil {
			t.Fatal("same-ID replay accepted an explicit null as omitted")
		}
	}
	for _, mode := range []string{"candidate", "basis", "read", "mixed", "missing-output", "null-record", "active-run"} {
		t.Run(mode, func(t *testing.T) {
			bad := assistantVariantClone(t, record)
			p := bad.DomainOutputs[0].DialectAction
			switch mode {
			case "candidate":
				p.Action.Patch.Intent = new(string)
			case "basis":
				p.Read.Recipe.Records[1].ReviewVersion = 1
			case "read":
				p.Read.Recipe = nil
			case "mixed":
				p.Action.Publication = &assistantPublicationProposal{}
			case "missing-output":
				bad.DomainOutputs = nil
			case "null-record":
				p.Action.RecordIDs = []string{""}
			case "active-run":
				bad.DomainOutputs[4].DialectAction.ActiveResearch.State = "complete"
			}
			if assistantRecipeImport(t, e, bad) == nil {
				t.Fatal("import accepted changed finite custody", mode)
			}
		})
	}
}
func TestAssistantDialectRecordActualNativeParentChild(t *testing.T) {
	s, _, e, _, trace := assistantCodexFixture(t, "delegate", "work")
	saved := assistantRecordSeed(t, s)
	id := string(assistantChatJSON(map[string]string{"id": saved.Document.ID}))
	peer := strings.Replace(assistantExecutionCodexPeer, "if mode=='delegate' and index==1:\n   tool(offered('delegate_tasks'),{'tasks':[{'title':'Inspect schema','objective':'Inspect the schema only.','profile':'review'}]})", `if mode=='delegate' and index==1: tool(offered('read_dialect_recipe'),json.loads(`+fmt.Sprintf("%q", id)+`),call='same-provider-call')
  elif mode=='delegate' and index==2: tool(offered('read_dialect_records'),json.loads(`+fmt.Sprintf("%q", id)+`),call='same-provider-call')
  elif mode=='delegate' and index==3: tool(offered('propose_dialect_action'),json.loads(`+fmt.Sprintf("%q", assistantRecordArgs(saved, "update-recipe"))+`),call='same-provider-call')`, 1)
	peer = strings.Replace(peer, "if mode=='duplicate': tool(offered('get_schema'),{},rpc='rpc-two')", `if mode=='delegate' and index==1: tool(offered('delegate_tasks'),{'tasks':[{'title':'Read records','objective':'Read the explicit records only.','profile':'review'}]},call='delegate',rpc='rpc-delegate')
  elif mode=='duplicate': tool(offered('get_schema'),{},rpc='rpc-two')`, 1)
	assistantRunMust(t, os.WriteFile(filepath.Join(filepath.Dir(trace), "peer.py"), []byte(peer), 0600))
	x := assistantExecutionOpen(t, s, e)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	assistantRunMust(t, x.Adapter.Run(ctx, x.Run))
	record := assistantMappingObserve(t, s, e, 1)
	roots, children := 0, 0
	for _, op := range record.Operations {
		if op.Kind == "host" {
			var in assistantHostIntent
			assistantRunMust(t, readAssistantHostReceipt(op.Intent, &in))
			if in.Root {
				roots++
			} else {
				children++
			}
		}
	}
	if roots != 2 || children != 1 {
		t.Fatal("native root/child/root custody lost", roots, children)
	}
}
func TestAssistantDialectRecordFiniteArguments(t *testing.T) {
	saved := inputPreparationReply{Document: repositoryDocument(1), SHA256: strings.Repeat("a", 64)}
	saved.Document.Records = append(saved.Document.Records, preparedInputRecord{ID: "row_" + strings.Repeat("2", 64)})
	good := assistantRecordArgs(saved, "update-recipe")
	for _, change := range [][2]string{{`"kind":"update-recipe"`, `"kind":"evaluate"`}, {`"criteria":[]`, `"criteria":null`}, {`"concurrency":0`, `"concurrency":null`}, {`"type":"boolean"`, `"type":"made-up"`}, {`"research":{`, `"research":{"ai":{},`}, {`"patch":{`, `"patch":{"unknown":false,`}, {`"intent":"Review exact \u003crecord\u003e\u0026 中文"`, `"intent":null`}, {`"id":`, `"ID":`}} {
		bad := strings.Replace(good, change[0], change[1], 1)
		if bad == good {
			t.Fatal("mutation fixture did not match", change)
		}
		if _, err := readAssistantDialectRecordArguments("propose_dialect_action", []byte(bad)); err == nil {
			t.Fatal("malformed action accepted", bad)
		}
	}
	if _, err := readAssistantDialectRecordArguments("propose_dialect_action", []byte(good)); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"research", "remove-recipe"} {
		bad := strings.Replace(good, `"kind":"update-recipe"`, `"kind":"`+kind+`"`, 1)
		if kind == "remove-recipe" {
			bad = strings.Replace(bad, `"patch":`, `"section":"research","patch":`, 1)
		}
		if _, err := readAssistantDialectRecordArguments("propose_dialect_action", []byte(bad)); err == nil {
			t.Fatal("mixed action members accepted", bad)
		}
	}

	for _, raw := range []string{`{"id":"` + saved.Document.ID + `","offset":1001}`, `{"id":"` + saved.Document.ID + `","limit":0}`, `{"id":"` + saved.Document.ID + `","limit":null}`, `{"id":"` + saved.Document.ID + `","offset":"0"}`} {
		if _, err := readAssistantDialectRecordArguments("read_dialect_records", []byte(raw)); err == nil {
			t.Fatal("unbounded read accepted", raw)
		}
	}
}
