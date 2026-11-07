package desk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

// These vectors are handwritten, shared with the real browser reader. Actual
// producer observations below are ephemeral inputs, never golden expectations.
func TestAssistantRunClientSharedVectors(t *testing.T) {
	for _, name := range []string{"pending", "terminal"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "assistant", "serverRuns", "fixtures", name+".json"))
			assistantRunMust(t, err)
			if !uniqueJSON(raw) {
				t.Fatal("duplicate vector JSON")
			}
			var view assistantRunView
			assistantRunMust(t, json.Unmarshal(raw, &view))
			if view.ID != "arun_"+strings.Repeat("1", 32) || view.Owner.Kind != "chat" || view.Owner.ID != "chat-one" || view.Owner.Revision != strings.Repeat("a", 64) || view.RequestID != "message-one" || view.Cursor != fmt.Sprintf("%d:%d", view.Revision, len(view.Events)) {
				t.Fatal("vector binding/cursor")
			}
			for _, event := range view.Events {
				_, err := assistantRunEventBytes(event.Event)
				assistantRunMust(t, err)
			}
			if name == "pending" {
				if view.State != "admitted" || view.Revision != 1 || view.Events != nil || view.Operations != nil {
					t.Fatal("nil pending vectors")
				}
			} else {
				if view.State != "complete" || view.Revision != 3 || len(view.Events) != 2 || view.Events[0].ID != "event-one" || view.Events[0].Event.Message == nil || view.Events[0].Event.Message.Text != "Ready 한글" || view.Events[1].Event.End == nil || len(view.Operations) != 1 || string(view.Operations[0].Intent) != `{"false":false}` || string(view.Operations[0].Result) != `{"null":null}` {
					t.Fatal("terminal vectors")
				}
			}
		})
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "assistant", "serverRuns", "fixtures", "checkpoint.json"))
	assistantRunMust(t, err)
	value, err := runcontext.ParseJSON(raw)
	assistantRunMust(t, err)
	cp, err := runcontext.ReadExecution(value)
	assistantRunMust(t, err)
	if cp == nil || cp.RequestKey != strings.Repeat("b", 64) || cp.Used != 1 || cp.Limit != 20 {
		t.Fatal("canonical checkpoint vector")
	}
	for _, raw := range []string{`{"type":"unknown"}`, `{}`, `{"type":"message_progress"}`, `{"type":"execution_checkpoint"}`, `{"type":"budget_paused"}`} {
		var event contract.RunEvent
		if err := json.Unmarshal([]byte(raw), &event); err == nil {
			if _, err = assistantRunEventBytes(event); err == nil {
				t.Fatal("invalid durable event", raw)
			}
		}
	}
}
func assistantRunClientOutput(t *testing.T, name string, raw []byte) {
	t.Helper()
	if dir := os.Getenv("DESK_RUN_CLIENT_OUTPUTS"); dir != "" {
		assistantRunMust(t, os.WriteFile(filepath.Join(dir, name), raw, 0600))
	}
}
func assistantRunClientJSON(t *testing.T, name string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	assistantRunMust(t, err)
	assistantRunClientOutput(t, name, raw)
}
func TestAssistantRunClientActualProducer(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	s, token, start, _ := assistantProtocolFixture(t, func(ctx context.Context, e *assistantRunExecution) error {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
		if err := e.Event(ctx, "event-one", contract.RunEvent{Message: &contract.RunEventMessage{Type: "message", Text: "Ready 한글"}}); err != nil {
			return err
		}
		if err := e.Event(ctx, "event-end", contract.RunEvent{End: &contract.RunEventEnd{Type: "end"}}); err != nil {
			return err
		}
		return assistantProtocolComplete(ctx, e)
	})
	admitted := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
	<-started
	pending := assistantProtocolView(t, policyCall(s, token, "GET", "/api/assistant-runs/"+admitted.ID, nil), 200)
	assistantRunClientJSON(t, "pending.json", pending)
	close(release)
	complete := assistantProtocolWait(t, s, token, admitted.ID, "complete")
	if len(complete.Events) != 2 || complete.Checkpoint == nil {
		t.Fatal("actual completed journal missing")
	}
	assistantRunClientJSON(t, "terminal.json", complete)
	discovery := policyCall(s, token, "GET", "/api/assistant-runs/owners/chat-one/requests/message-one", nil)
	if discovery.Code != 200 {
		t.Fatal("discovery", discovery.Code)
	}
	assistantRunClientOutput(t, "discovery.json", discovery.Body.Bytes())
	// A refresh may only know the current saved journal revision. Authorized
	// title edits retain the exact user request and must not hide its older run.
	saved := policyWorkspaceRead(t, s, token, "/api/conversations")
	var document conversationDocument
	assistantRunMust(t, json.Unmarshal(saved.Content, &document))
	var chat map[string]json.RawMessage
	assistantRunMust(t, json.Unmarshal(document.Chats[0], &chat))
	chat["title"] = json.RawMessage(`"Later authorized title"`)
	encodedChat, err := json.Marshal(chat)
	document.Chats[0] = encodedChat
	assistantRunMust(t, err)
	if update := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, document); update.Code != 200 {
		t.Fatal("authorized journal edit", update.Code, update.Body.String())
	}
	current := policyWorkspaceRead(t, s, token, "/api/conversations")
	assistantRunMust(t, json.Unmarshal(current.Content, &document))
	currentRevision := digestOf(document.Chats[0])
	if currentRevision == complete.Owner.Revision {
		t.Fatal("fixture did not change journal revision")
	}
	assistantRunClientJSON(t, "current-binding.json", struct {
		Owner     assistantRunOwner `json:"owner"`
		RequestID string            `json:"requestId"`
	}{assistantRunOwner{Kind: "chat", ID: "chat-one", Revision: currentRevision}, "message-one"})
	discovery = policyCall(s, token, "GET", "/api/assistant-runs/owners/chat-one/requests/message-one", nil)
	var retained struct {
		Runs []assistantRunView `json:"runs"`
	}
	assistantRunMust(t, json.Unmarshal(discovery.Body.Bytes(), &retained))
	if discovery.Code != 200 || len(retained.Runs) != 1 || retained.Runs[0].Owner.Revision != complete.Owner.Revision {
		t.Fatal("journal edit hid original binding")
	}
	assistantRunClientOutput(t, "changed-journal-discovery.json", discovery.Body.Bytes())
	historical := policyCall(s, policyTestSession(t, s), "GET", "/api/assistant-runs/owners/chat-one/requests/message-one", nil)
	assistantRunMust(t, json.Unmarshal(historical.Body.Bytes(), &retained))
	if historical.Code != 200 || len(retained.Runs) != 1 || !retained.Runs[0].Historical {
		t.Fatal("new session inherited live ownership")
	}
	assistantRunClientOutput(t, "historical-discovery.json", historical.Body.Bytes())
	wrongRequest := policyCall(s, token, "GET", "/api/assistant-runs/owners/chat-one/requests/other-request", nil)
	assistantRunMust(t, json.Unmarshal(wrongRequest.Body.Bytes(), &retained))
	if wrongRequest.Code != 200 || len(retained.Runs) != 0 {
		t.Fatal("wrong request inherited run")
	}
	assistantRunClientOutput(t, "wrong-request-discovery.json", wrongRequest.Body.Bytes())
	if wrongOwner := policyCall(s, token, "GET", "/api/assistant-runs/owners/other-chat/requests/message-one", nil); wrongOwner.Code != 404 {
		t.Fatal("wrong owner discovery", wrongOwner.Code)
	}
	request := httptest.NewRequest("GET", "/api/assistant-runs/"+complete.ID+"/events", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Last-Event-ID", pending.Cursor)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, request)
	if w.Code != 200 || strings.Count(w.Body.String(), "event: event\n") != 2 || !strings.Contains(w.Body.String(), "event: snapshot\nid: "+complete.Cursor) {
		t.Fatal("actual shared cursor suffix", w.Code, w.Body.String())
	}
	assistantRunClientOutput(t, "events.sse", w.Body.Bytes())
	// The latest cursor still earns an authoritative snapshot on reconnect.
	request = httptest.NewRequest("GET", "/api/assistant-runs/"+complete.ID+"/events", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Last-Event-ID", complete.Cursor)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, request)
	if w.Code != 200 || strings.Count(w.Body.String(), "event: snapshot\n") != 1 {
		t.Fatal("latest cursor reconciliation")
	}
	assistantRunClientOutput(t, "reconnect.sse", w.Body.Bytes())
	stale := policyCall(s, token, "POST", "/api/assistant-runs/"+complete.ID+"/actions", assistantRunAction{ID: "stale-action", Kind: "cancel", Revision: 1})
	if stale.Code != http.StatusConflict {
		t.Fatal("stale action admitted", stale.Code)
	}
	// Handwritten records enumerate the current public event inventory. Required
	// payload members and each declared version are explicit; these are never
	// captured from a producer or manufactured by the decoder being checked.
	records := map[string]string{
		"approval_requested":      `{"type":"approval_requested","version":1,"callId":"call-one","purpose":"tool-call","name":"fixture_write","tool":"fixture_write","source":{"kind":"custom","id":"fixture","trust":"person","version":"1"},"args":{"text":"Fixture text"}}`,
		"steered":                 `{"type":"steered","version":1,"bytes":0,"atRequest":1}`,
		"compacted":               `{"type":"compacted","version":1,"turns":1,"bytes":0,"digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","mode":"index"}`,
		"skill_loaded":            `{"type":"skill_loaded","version":1,"skill":{"id":"skill-one","name":"fixture-skill","source":{"kind":"custom","id":"fixture","trust":"person","version":"1"},"digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"bytes":0}`,
		"skill_unloaded":          `{"type":"skill_unloaded","version":1,"id":"skill-one"}`,
		"retry":                   `{"type":"retry","version":1,"attempt":1}`,
		"response_recovery":       `{"type":"response_recovery","version":1}`,
		"delegation_requested":    `{"type":"delegation_requested","version":1,"request":{"tasks":[]}}`,
		"delegated_task":          `{"type":"delegated_task","version":1,"task":{"id":"11111111-1111-4111-8111-111111111111","parentRunId":"22222222-2222-4222-8222-222222222222","title":"Fixture task","objective":"Read the explicit fixture","profile":"read","state":"complete","createdAt":"2026-11-04T00:00:00Z","modelRequests":0,"toolCalls":0,"attempts":1,"steps":[]}}`,
		"user_question":           `{"type":"user_question","version":1,"id":"question-one","question":{"title":"Choose","question":"Which fixture?","selection":"single","options":[{"id":"one","label":"One","description":"The explicit fixture"}],"recommendedOptionId":null,"recommendation":null}}`,
		"reasoning":               `{"type":"reasoning","version":1,"text":"Fixture reasoning","done":true}`,
		"message":                 `{"type":"message","version":1,"text":"Fixture message"}`,
		"tool_call":               `{"type":"tool_call","version":1,"name":"fixture_read","args":{}}`,
		"tool_result":             `{"type":"tool_result","version":1,"name":"fixture_read","isError":false,"text":"Fixture result"}`,
		"guardrail":               `{"type":"guardrail","version":1,"tool":"fixture_read","action":"refused","detail":"Fixture refusal"}`,
		"thinking_unavailable":    `{"type":"thinking_unavailable","version":1,"detail":"Fixture unavailable"}`,
		"critic_decided":          `{"type":"critic_decided","version":1,"mode":"off","ran":false,"reason":"off"}`,
		"critique":                `{"type":"critique","version":1,"refuted":false,"checks":[],"text":"Fixture critique"}`,
		"proposal":                `{"type":"proposal","version":1,"document":{},"unknowns":[],"digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`,
		"error":                   `{"type":"error","version":1,"message":"Fixture error"}`,
		"end":                     `{"type":"end","version":1}`,
		"policy_resolved":         `{"type":"policy_resolved","version":1,"members":{}}`,
		"connection_switched":     `{"type":"connection_switched","version":1,"role":"author","from":"first","to":"second","reason":"timeout"}`,
		"usage":                   `{"type":"usage","version":1,"usage":{"rows":[]}}`,
		"recorded":                `{"type":"recorded","version":1,"id":"record-one","bytes":0}`,
		"memory_proposed":         `{"type":"memory_proposed","version":1,"memory":{"id":"memory-one","kind":"fact","text":"Fixture fact","source":{"run":"run-one","proposedBy":"host"},"scope":"project","digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","createdAt":"2026-11-04T00:00:00Z","state":"proposed"}}`,
		"memory_recalled":         `{"type":"memory_recalled","version":1,"ids":[],"digests":[],"bytes":0}`,
		"edit_applied":            `{"type":"edit_applied","version":1,"document":"fixture-document","line":1,"bytes":0,"valid":true}`,
		"edit_refused":            `{"type":"edit_refused","version":1,"reason":"Fixture refusal"}`,
		"export_written":          `{"type":"export_written","version":1,"records":0,"bytes":0,"digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`,
		"output_guarded":          `{"type":"output_guarded","version":1,"where":"message","pattern":"fixture","action":"redact"}`,
		"plan_revised":            `{"type":"plan_revised","version":2,"execution":"execution-one","sequence":1,"previous":"previous-one","revision":"revision-one","explanation":"Fixture revision","diff":[]}`,
		"plan_departed":           `{"type":"plan_departed","version":2,"execution":"execution-one","revision":"revision-one","acceptedRevision":"accepted-one","explanation":"Fixture departure","departures":[]}`,
		"plan_completion_refused": `{"type":"plan_completion_refused","version":2,"execution":"execution-one","revision":"revision-one","reason":"missing-evidence"}`,
		"plan_drift_decided":      `{"type":"plan_drift_decided","version":2,"execution":"execution-one","callId":"call-one","revision":"revision-one","candidateRevision":"candidate-one","approved":false}`,
		"route_decided":           `{"type":"route_decided","version":2,"route":{"route":"agent","skills":[],"tools":"all","operations":"all","planning":"withheld","version":1,"decidedBy":"host","reason":"Fixture route","bindingDigest":"binding-one","catalogueDigest":"catalogue-one","widened":false,"refusals":[]}}`,
		"route_widened":           `{"type":"route_widened","version":2,"route":{"route":"agent","skills":[],"tools":"all","operations":"all","planning":"withheld","version":1,"decidedBy":"host","reason":"Fixture route","bindingDigest":"binding-one","catalogueDigest":"catalogue-one","widened":true,"refusals":[]}}`,
		"repeat_detected":         `{"type":"repeat_detected","version":2,"execution":"execution-one","dispatchId":"dispatch-one","keyDigest":"repeat-one","count":2,"reminder":true,"stopped":false}`,
	}
	union := reflect.TypeOf(contract.RunEvent{})
	seen := map[int]bool{}
	types := []string{}
	for expected, raw := range records {
		var event contract.RunEvent
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			t.Fatalf("handwritten event %s refused: %T: %v", expected, err, err)
		}
		encoded, err := assistantRunEventBytes(event)
		assistantRunMust(t, err)
		var discriminator struct {
			Type string `json:"type"`
		}
		assistantRunMust(t, json.Unmarshal(encoded, &discriminator))
		if discriminator.Type != expected {
			t.Fatalf("event discriminator %s, want %s", discriminator.Type, expected)
		}
		for i := 0; i < union.NumField(); i++ {
			if !reflect.ValueOf(event).Field(i).IsNil() {
				if seen[i] {
					t.Fatalf("duplicate union leg for %s", expected)
				}
				seen[i] = true
			}
		}
		types = append(types, discriminator.Type)
	}
	sort.Strings(types)
	if len(types) != 38 || len(seen) != union.NumField()-3 {
		t.Fatal("durable discriminator inventory", types, "union legs", len(seen), union.NumField())
	}
	assistantRunClientJSON(t, "event-types.json", types)
}
