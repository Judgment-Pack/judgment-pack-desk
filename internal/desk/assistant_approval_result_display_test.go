package desk

import (
	"bytes"
	"context"
	"encoding/json"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAssistantApprovalResultDisplayRetainsEarlierCanonicalText(t *testing.T) {
	raw, err := os.ReadFile("testdata/assistant-approval-result-display.json")
	assistantRunMust(t, err)
	var expected []assistantApprovalResultView
	assistantRunMust(t, json.Unmarshal(raw, &expected))
	_, _, base := assistantRunFixture(t)
	run := assistantRunRecord{Canonical: &assistantCanonicalJournal{Head: assistantCanonicalHead{RunID: "display-run"}}}
	for index, want := range expected {
		cp := assistantProtocolAsked(base)
		cp.ID, cp.Status = "display-run", "complete"
		cp.Approval.CallID, cp.Approval.Name, cp.Approval.Tool, cp.Approval.State = want.CallID, want.Name, "write_note", "answered"
		if want.Result.Available {
			result := map[string]any{"content": []any{}, "isError": want.IsError}
			content := []any{}
			for _, text := range want.Result.Text {
				content = append(content, map[string]string{"type": "text", "text": text})
			}
			for _, kind := range want.Result.OtherContent {
				if kind == "structuredContent" {
					result[kind] = json.RawMessage(`{"number":1.20}`)
				} else {
					content = append(content, map[string]string{"kind": kind, "mediaType": "image/png", "reference": "fixture:image"})
				}
			}
			result["content"] = content
			assistantRunMust(t, json.Unmarshal(assistantChatJSON(result), &cp.Approval.Result))
		}
		cp = assistantRecordHeader(t, cp)
		encoded := assistantChatJSON(runcontext.StoredRunEnvelope{Version: 1, Scope: runcontext.DurableScope{Execution: "display-run", Role: "author"}, Event: contract.RunEvent{ExecutionCheckpoint: &contract.RunEventExecutionCheckpoint{Type: "execution_checkpoint", Checkpoint: cp}}})
		var decoded runcontext.StoredRunEnvelope
		assistantRunMust(t, json.Unmarshal(encoded, &decoded))
		if decoded.Event.ExecutionCheckpoint == nil || !reflect.DeepEqual(decoded.Event.ExecutionCheckpoint.Checkpoint, cp) {
			t.Fatal("retained display fixture lost its canonical checkpoint")
		}
		// Duplicates are different journal events for the same canonical call.
		run.Canonical.Events = append(run.Canonical.Events, assistantCanonicalEvent{Cursor: string(rune('a' + index)), Bytes: encoded}, assistantCanonicalEvent{Cursor: string(rune('d' + index)), Bytes: encoded})
		run.Checkpoint = &cp
	}
	// A later asked approval must not erase prior answered results.
	asked := assistantProtocolAsked(base)
	asked.ID, asked.Approval.CallID = "display-run", "new-ask"
	asked = assistantRecordHeader(t, asked)
	run.Checkpoint = &asked
	before := assistantChatJSON(run)
	view := assistantRunPublic(run)
	if !reflect.DeepEqual(view.ApprovalResults, expected) {
		t.Fatalf("canonical result display changed original text or call identity: %#v", view.ApprovalResults)
	}
	var restored assistantRunView
	assistantRunMust(t, json.Unmarshal(assistantChatJSON(view), &restored))
	if !reflect.DeepEqual(restored.ApprovalResults, expected) || string(before) != string(assistantChatJSON(run)) {
		t.Fatal("read/reload changed original journal or result text")
	}
	for _, row := range view.ApprovalResults {
		if row.CallID == "new-ask" {
			t.Fatal("pending approval leaked into completed results")
		}
	}
}

func TestAssistantApprovalResultSavedDisplayExcludedFromLaterNativeContext(t *testing.T) {
	const exactResult = " {\n \"number\":1.20,\"wide\":9007199254740993,\"zero\":-0\n}\n"
	// Save the response through the real bounded projection/write/readback path.
	// The answered checkpoint is handwritten fixture state, not a claim of a live
	// approval effect. Actual approval effects are covered by the694 route cases.
	f := newChatProjectionFixture(t)
	original := bytes.Replace(f.read(t), []byte(`"id":"message-one"`), []byte(`"id":"prior-message"`), 1)
	f.save(t, original)
	run := f.admit(t, "chat-one", "prior-message", "display-context-first", "complete", projectionMessage("The retained response finished."))
	_, err := f.store.transaction(t.Context(), func(ledger *assistantRunLedger) (bool, error) {
		retained, err := assistantChatProjectionRun(ledger, run.ID)
		if err != nil {
			return false, err
		}
		cp := assistantProtocolAsked(*retained.Checkpoint)
		cp.Status = "complete"
		cp.Approval.State = "answered"
		result := map[string]any{"content": []any{map[string]string{"type": "text", "text": exactResult}}}
		if err = json.Unmarshal(assistantChatJSON(result), &cp.Approval.Result); err != nil {
			return false, err
		}
		retained.Checkpoint = &cp
		retained.Revision++
		return true, nil
	})
	assistantRunMust(t, err)
	assistantRunMust(t, f.s.reconcileAssistantChatProjection(f.request(), run.ID))
	savedProjection := f.read(t)
	_, rows, err := assistantChatRows(savedProjection)
	assistantRunMust(t, err)
	chat, err := assistantChatObject(rows[0])
	assistantRunMust(t, err)
	checkpoint, err := assistantChatObject(chat["checkpoint"])
	assistantRunMust(t, err)
	state, err := assistantChatObject(checkpoint["state"])
	assistantRunMust(t, err)
	responses, err := assistantChatArray(state["responses"])
	assistantRunMust(t, err)
	if len(responses) != 1 {
		t.Fatal("actual saved response projection missing")
	}
	response, err := assistantChatObject(responses[0])
	assistantRunMust(t, err)
	work, err := assistantChatObject(response["work"])
	assistantRunMust(t, err)
	displayItems := bytes.Clone(work["items"])
	projected := runcontext.MustParseJSON(string(displayItems))
	if projected.Len() != 1 || projected.Items()[0].Get("approvalResult").Get("text").Items()[0].Str() != exactResult {
		t.Fatal("saved projection changed original display text")
	}
	// Preserve an ordinary historical field containing the same text, plus the
	// person's own identical quote. Field-path exclusion must not redact either.
	ordinary := "Historical critique retains matching text: " + exactResult
	work["critique"] = assistantChatJSON(ordinary)
	response["work"] = assistantChatEncodeObject(work)
	responses[0] = assistantChatEncodeObject(response)
	state["responses"] = assistantChatEncodeArray(responses)
	state["approvalResult"] = assistantChatJSON("Outside the new display path")
	turns, err := assistantChatArray(state["turns"])
	assistantRunMust(t, err)
	userText := "The person repeats exactly: " + exactResult
	turns = append(turns, assistantChatJSON(map[string]any{"id": "message-one", "role": "user", "kind": "message", "text": userText, "statement": userText, "input": userText, "at": "2026-11-04T12:00:00Z"}))
	state["turns"] = assistantChatEncodeArray(turns)
	checkpoint["state"] = assistantChatEncodeObject(state)
	chat["checkpoint"] = assistantChatEncodeObject(checkpoint)
	chat["critic"] = assistantChatJSON("off")
	supplied := assistantChatEncodeObject(chat)
	// Load the actually saved projected response into a new native execution,
	// preserving the existing fixture's private Manager, factory and shared route.
	s, token, old, _, trace := assistantCodexFixture(t, "normal", "work")
	e := assistantChatResave(t, s, token, old, string(supplied), 201)
	peer := strings.Replace(assistantExecutionCodexPeer, "  else: tool(offered('get_schema'),{})", `  else:
   entries=runs[-1]['checkpoint']['context']['entries']
   ids=[entry['id'] for entry in entries if ':saved-chat-state:' in entry['id']]
   if len(ids)!=1: sys.exit(76)
   tool(offered('read_context'),{'id':ids[0],'path':'/responses'},call='display-context-read',rpc='display-context-rpc')`, 1)
	peer = strings.Replace(peer, "  if mode=='duplicate': tool(offered('get_schema'),{},rpc='rpc-two')\n  else: finish()", `  if q.get('id')!='display-context-rpc' or q.get('result',{}).get('success')!=True: sys.exit(77)
  log({'type':'display-context-answer','answer':q['result']})
  finish()`, 1)
	if !strings.Contains(peer, "display-context-answer") || !strings.Contains(peer, "':saved-chat-state:'") {
		t.Fatal("native context peer anchors changed")
	}
	assistantRunMust(t, os.WriteFile(filepath.Join(filepath.Dir(trace), "peer.py"), []byte(peer), 0600))
	before := policyWorkspaceRead(t, s, token, "/api/conversations")
	var reloaded conversationDocument
	assistantRunMust(t, json.Unmarshal(before.Content, &reloaded))
	restored := runcontext.MustParseJSON(string(reloaded.Chats[0])).Get("checkpoint").Get("state").Get("responses").Items()[0].Get("work").Get("items")
	if !bytes.Equal(displayItems, restored.JSON()) {
		t.Fatal("reload altered saved display bytes")
	}
	x := assistantExecutionOpen(t, s, e)
	var observations *runcontext.Value
	for _, doc := range x.Run.Context.Documents {
		if doc.ID == "saved-chat-state" {
			observations = doc.Value
		}
	}
	if observations == nil {
		t.Fatal("actual factory omitted historical context")
	}
	assertHistorical := func(value *runcontext.Value) {
		if !value.IsArray() || value.Len() != 1 {
			t.Fatal("model context lost ordinary response history")
		}
		work := value.Items()[0].Get("work")
		if !work.IsObject() || !work.Get("critique").IsString() || work.Get("critique").Str() != ordinary {
			t.Fatal("narrow exclusion changed matching historical prose")
		}
		items := work.Get("items")
		if !items.IsArray() || items.Len() != 1 {
			t.Fatal("model context lost existing work items")
		}
		item := items.Items()[0]
		if !item.IsObject() || item.Has("approvalResult") || !item.Get("id").IsString() || !item.Get("name").IsString() || !item.Get("status").IsString() || item.Get("id").Str() != projected.Items()[0].Get("id").Str() || item.Get("name").Str() != projected.Items()[0].Get("name").Str() || item.Get("status").Str() != projected.Items()[0].Get("status").Str() {
			t.Fatal("model context retained display result or lost existing work fields")
		}
	}
	assertHistorical(observations.Get("responses"))
	if observations.Get("approvalResult").Str() != "Outside the new display path" || x.Run.Context.CurrentRequest.Message != userText || x.Run.Context.CurrentRequest.Input != userText {
		t.Fatal("exclusion changed outside-path data or the person's matching quote")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	assistantRunMust(t, x.Adapter.Run(ctx, x.Run))
	generations, answers := 0, 0
	for _, record := range assistantCodexLog(t, trace) {
		switch string(record["type"]) {
		case `"generation"`:
			generations++
			var prompt string
			assistantRunMust(t, json.Unmarshal(record["prompt"], &prompt))
			if !strings.Contains(prompt, userText) {
				t.Fatal("actual native request lost the person's matching text")
			}
		case `"display-context-answer"`:
			answers++
			var answer struct {
				ContentItems []struct {
					Text string `json:"text"`
				} `json:"contentItems"`
			}
			assistantRunMust(t, json.Unmarshal(record["answer"], &answer))
			if len(answer.ContentItems) != 1 {
				t.Fatal("actual native context answer missing")
			}
			text := answer.ContentItems[0].Text
			at := strings.IndexByte(text, '{')
			if at < 0 {
				t.Fatal("native context read lacked JSON result")
			}
			var body json.RawMessage
			assistantRunMust(t, json.NewDecoder(strings.NewReader(text[at:])).Decode(&body))
			wrapped := runcontext.MustParseJSON(string(body))
			read := wrapped.Get("structuredContent")
			// SessionContext.Result removes the text part that duplicates structuredContent.
			if !wrapped.IsObject() || !read.IsObject() || wrapped.Has("content") {
				t.Fatal("native context read lacked its deduplicated structured result")
			}
			if !read.Get("path").IsString() || read.Get("path").Str() != "/responses" || read.Get("value") == nil {
				t.Fatal("native context read returned sections instead of exact requested history")
			}
			assertHistorical(read.Get("value"))
		}
	}
	if generations != 1 || answers != 1 {
		t.Fatal("actual native request/context path was not exercised", generations, answers)
	}
	after := policyWorkspaceRead(t, s, token, "/api/conversations")
	if before.SHA256 != after.SHA256 || !bytes.Equal(before.Content, after.Content) || !bytes.Equal(savedProjection, f.read(t)) {
		t.Fatal("model assembly mutated either saved original/display document")
	}
}
