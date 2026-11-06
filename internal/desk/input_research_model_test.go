package desk

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestInputResearchUsesReviewedNamedConnectionAndCannotPickAnotherModel(t *testing.T) {
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer research-test-key" {
			t.Error("wrong credential")
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "shared-model" || body["stream"] != true {
			t.Error("wrong model request")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": `{"tool":"finish","arguments":{}}`}}}})
	}))
	defer provider.Close()
	s, ts, _ := assistantServer(t)
	registry := saveTestAIRegistry(t, s, ts, aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{aiTestConnection(testAIAlpha, "Research", provider.URL)}})
	connection := registry.Connections[0]
	code, _ := aiTestRequest(t, ts, "PUT", "/api/assistant/key", map[string]string{"key": "research-test-key"}, map[string]string{aiConnectionHeader: connection.ID, aiRevisionHeader: connection.Revision})
	if code != 200 {
		t.Fatal(code)
	}
	choice := inputResearchAI{Connection: connection.ID, Revision: connection.Revision, Model: "shared-model"}
	raw, e := s.inputResearchModel(backgroundResearchFixtureContext(t, choice), choice, "Research provided URLs.", nil)
	if e != nil {
		t.Fatal(e)
	}
	action, e := decodeResearchAction(raw)
	if e != nil || action.Tool != "finish" || calls.Load() != 1 {
		t.Fatal("action lost", e)
	}
	choice.Model = "unapproved-model"
	if _, e = s.inputResearchModel(backgroundResearchFixtureContext(t, choice), choice, "test", nil); e == nil || calls.Load() != 1 {
		t.Fatal("unapproved model sent")
	}
	choice.Model = "shared-model"
	choice.Revision = "old"
	if _, e = s.inputResearchModel(backgroundResearchFixtureContext(t, choice), choice, "test", nil); e == nil || calls.Load() != 1 {
		t.Fatal("stale connection sent")
	}
}
