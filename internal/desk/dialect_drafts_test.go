package desk

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func draftCall(s *Server, method, path string, body any, revision string) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
	r.Header.Set("Authorization", "Bearer "+testToken)
	if revision != "" {
		r.Header.Set("If-Match", revision)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func draftResult(t *testing.T, w *httptest.ResponseRecorder) dialectDraftReply {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var reply dialectDraftReply
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	return reply
}
func TestDialectDraftLifecycle(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) {
			s, _ := draftRepositoryFixture(t, backend)
			testDialectDraftLifecycle(t, s)
		})
	}
}
func testDialectDraftLifecycle(t *testing.T, s *Server) {
	request := dialectDraftRequest{Key: "manual-one", Name: "", Recipe: inputRecipe{Intent: "Find records", Research: &inputResearchRecipe{Version: 1, Target: "records", Fields: []inputResearchField{{Name: "", Description: "", Type: "number"}}, Criteria: []inputResearchCriterion{}, Limits: inputResearchLimits{Records: 5, Pages: 5, ModelCalls: 10}}}}
	first := draftResult(t, draftCall(s, "POST", "/api/dialect-drafts", request, ""))
	if first.Document.ID == "" || first.Document.SavedInputID != "" {
		t.Fatal(first)
	}
	again := draftResult(t, draftCall(s, "POST", "/api/dialect-drafts", request, ""))
	if again.SHA256 != first.SHA256 {
		t.Fatal("creation was not idempotent")
	}
	if w := draftCall(s, "POST", "/api/dialect-drafts/"+first.Document.ID+"/publish", map[string]any{}, first.SHA256); w.Code != 422 {
		t.Fatal(w.Code, w.Body.String())
	}
	body := map[string]any{"name": "Typed fields", "recipe": map[string]any{"intent": "Keep numbers and missing values", "research": map[string]any{"version": 1, "target": "records", "fields": []any{map[string]any{"name": "amount", "description": "A numeric amount", "type": "number"}}, "criteria": []any{}, "ai": map[string]any{"connection": "legacy-api", "revision": "revision", "model": "test-model"}, "limits": map[string]any{"records": 5, "searches": 0, "pages": 5, "modelCalls": 10}}}}
	if w := draftCall(s, "PUT", "/api/dialect-drafts/"+first.Document.ID, body, "wrong"); w.Code != 409 {
		t.Fatal("missing stale guard", w.Code)
	}
	edited := draftResult(t, draftCall(s, "PUT", "/api/dialect-drafts/"+first.Document.ID, body, first.SHA256))
	reload := draftResult(t, draftCall(s, "GET", "/api/dialect-drafts/"+first.Document.ID, nil, ""))
	if reload.Document.Recipe.Research.Fields[0].Type != "number" || reload.Document.Name != "Typed fields" {
		t.Fatal(reload)
	}
	// Reopening a proposal never overwrites the saved editor buffer.
	again = draftResult(t, draftCall(s, "POST", "/api/dialect-drafts", request, ""))
	if again.SHA256 != edited.SHA256 {
		t.Fatal("reopen overwrote edits")
	}
	published := draftResult(t, draftCall(s, "POST", "/api/dialect-drafts/"+first.Document.ID+"/publish", map[string]any{}, edited.SHA256))
	if published.Document.SavedInputID == "" {
		t.Fatal("not published")
	}
	retry := draftResult(t, draftCall(s, "POST", "/api/dialect-drafts/"+first.Document.ID+"/publish", map[string]any{}, edited.SHA256))
	if retry.Document.SavedInputID != published.Document.SavedInputID {
		t.Fatal("publish duplicated")
	}
	if w := draftCall(s, "PUT", "/api/dialect-drafts/"+first.Document.ID, body, published.SHA256); w.Code != 409 {
		t.Fatal("published draft remained editable")
	}
	w := draftCall(s, "GET", "/api/input-preparations/"+published.Document.SavedInputID, nil, "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var prep inputPreparationReply
	json.Unmarshal(w.Body.Bytes(), &prep)
	if len(prep.Document.Records) != 0 || len(prep.Document.Activity) != 1 || prep.Document.Recipe.Research.Fields[0].Type != "number" {
		t.Fatal("promotion changed facts or started work")
	}
	w = draftCall(s, "GET", "/api/input-preparations/"+published.Document.SavedInputID+"/research", nil, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatal("draft started research", w.Body.String())
	}
}
func TestDialectDraftSurvivesChatDeletionAndMigration(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) {
			s, _ := draftRepositoryFixture(t, backend)
			testDialectDraftSurvivesChatDeletionAndMigration(t, s)
		})
	}
}
func testDialectDraftSurvivesChatDeletionAndMigration(t *testing.T, s *Server) {
	current := draftCall(s, "GET", "/api/conversations", nil, "")
	var chats conversationReply
	json.Unmarshal(current.Body.Bytes(), &chats)
	proposal := map[string]any{"version": 1, "draftId": "proposal-one", "name": "Retained proposal", "intent": "Find records", "target": "records", "fields": []any{map[string]any{"name": "active", "description": "Explicit true or false", "type": "boolean"}}, "criteria": []any{}, "limits": map[string]any{"records": 5, "searches": 2, "pages": 5, "modelCalls": 10}}
	doc := map[string]any{"version": 1, "chats": []any{map[string]any{"id": "chat-one", "inputDrafts": []any{proposal}}}}
	saved := draftCall(s, "PUT", "/api/conversations", doc, chats.SHA256)
	if saved.Code != 200 {
		t.Fatal(saved.Code, saved.Body.String())
	}
	json.Unmarshal(saved.Body.Bytes(), &chats)
	removed := draftCall(s, "PUT", "/api/conversations", map[string]any{"version": 1, "chats": []any{}}, chats.SHA256)
	if removed.Code != 200 {
		t.Fatal(removed.Body.String())
	}
	id := dialectDraftIdentity("", &dialectDraftOrigin{ChatID: "chat-one", DraftID: "proposal-one"})
	retained := draftResult(t, draftCall(s, "GET", "/api/dialect-drafts/"+id, nil, ""))
	if retained.Document.Name != "Retained proposal" || retained.Document.Recipe.Research.Fields[0].Type != "boolean" || retained.Document.Recipe.Research.AI.Connection != "" {
		t.Fatal(retained)
	}
	discarded := draftResult(t, draftCall(s, "DELETE", "/api/dialect-drafts/"+id, nil, retained.SHA256))
	if !discarded.Document.Discarded {
		t.Fatal("not discarded")
	}
	// Restored historical chat snapshots cannot resurrect a discarded draft.
	raw, _ := json.Marshal(doc)
	if err := s.retainConversationDialectDrafts(t.Context(), raw); err != nil {
		t.Fatal(err)
	}
	again := draftResult(t, draftCall(s, "GET", "/api/dialect-drafts/"+id, nil, ""))
	if !again.Document.Discarded {
		t.Fatal("resurrected")
	}
	list := draftCall(s, "GET", "/api/dialect-drafts", nil, "")
	if !strings.Contains(list.Body.String(), `"items":[]`) {
		t.Fatal(list.Body.String())
	}
}
func TestDialectDraftRecoversInterruptedPromotion(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) {
			s, _ := draftRepositoryFixture(t, backend)
			testDialectDraftRecoversInterruptedPromotion(t, s)
		})
	}
}
func testDialectDraftRecoversInterruptedPromotion(t *testing.T, s *Server) {
	saved := draftResult(t, draftCall(s, "POST", "/api/dialect-drafts", dialectDraftRequest{Key: "interrupted", Name: "Table", Recipe: inputRecipe{Intent: "Import rows"}}, ""))
	id, err := s.publishDialectDraft(t.Context(), saved.Document)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the preparation commit succeeding but the receipt write failing.
	w := draftCall(s, "PUT", "/api/dialect-drafts/"+saved.Document.ID, map[string]any{"name": "Wrong overwrite", "recipe": inputRecipe{}}, saved.SHA256)
	if w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	recovered := draftResult(t, draftCall(s, "POST", "/api/dialect-drafts/"+saved.Document.ID+"/publish", map[string]any{}, saved.SHA256))
	if recovered.Document.SavedInputID != id {
		t.Fatal("recovery duplicated preparation")
	}
}
func TestDialectDraftAdmission(t *testing.T) {
	s, _ := newTestServer(t, false)
	for _, path := range []string{"/api/dialect-drafts/invalid", "/api/dialect-drafts?unexpected=1"} {
		if w := draftCall(s, "GET", path, nil, ""); w.Code != 400 {
			t.Fatal(path, w.Code)
		}
	}
	r := httptest.NewRequest("POST", "/api/dialect-drafts", strings.NewReader(`{"key":"one","recipe":{"intent":""}}`))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code == 200 {
		t.Fatal("unauthenticated write")
	}
	if w := draftCall(s, "POST", "/api/dialect-drafts", map[string]any{"key": "one", "name": "Test", "recipe": map[string]any{"intent": "", "execute": true}}, ""); w.Code != 400 {
		t.Fatal("unknown operation accepted", w.Code)
	}
}

func TestDialectDraftRecoversLegacyChatSave(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) {
			s, _ := draftRepositoryFixture(t, backend)
			testDialectDraftRecoversLegacyChatSave(t, s)
		})
	}
}
func testDialectDraftRecoversLegacyChatSave(t *testing.T, s *Server) {
	origin := &dialectDraftOrigin{ChatID: "old-chat", DraftID: "old-proposal"}
	w := draftCall(s, "POST", "/api/input-preparations", map[string]any{"name": "Existing dialect", "recipe": inputRecipe{Intent: "Import records"}, "chatOrigin": origin}, "")
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var prep inputPreparationReply
	json.Unmarshal(w.Body.Bytes(), &prep)
	retained := draftResult(t, draftCall(s, "POST", "/api/dialect-drafts", dialectDraftRequest{Origin: origin, Name: "Old proposal", Recipe: inputRecipe{Intent: "Original"}}, ""))
	if retained.Document.SavedInputID != prep.Document.ID {
		t.Fatal("legacy completed save was lost")
	}
}

func TestDialectDraftMigratesExistingConversationOnRead(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) {
			s, _ := draftRepositoryFixture(t, backend)
			testDialectDraftMigratesExistingConversationOnRead(t, s)
		})
	}
}
func testDialectDraftMigratesExistingConversationOnRead(t *testing.T, s *Server) {
	// Seed an old checkpoint without using the new conversation writer.
	store, err := s.openChatData()
	if err != nil {
		t.Fatal(err)
	}
	name, err := resolveConversationName(store.root, s.conversationIdentity())
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"version":1,"chats":[{"id":"legacy-chat","inputDrafts":[{"version":1,"draftId":"legacy-proposal","name":"Old draft","intent":"Find records","target":"records","fields":[{"name":"name","description":"Published name","type":"string"}],"criteria":[],"limits":{"records":5,"searches":2,"pages":5,"modelCalls":10}}]}]}`)
	if err = writePrivateData(store.root, name, raw); err != nil {
		t.Fatal(err)
	}
	store.root.Close()
	w := draftCall(s, "GET", "/api/conversations", nil, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	id := dialectDraftIdentity("", &dialectDraftOrigin{ChatID: "legacy-chat", DraftID: "legacy-proposal"})
	retained := draftResult(t, draftCall(s, "GET", "/api/dialect-drafts/"+id, nil, ""))
	if retained.Document.Name != "Old draft" {
		t.Fatal(retained)
	}
	var current conversationReply
	json.Unmarshal(w.Body.Bytes(), &current)
	if w = draftCall(s, "PUT", "/api/conversations", map[string]any{"version": 1, "chats": []any{}}, current.SHA256); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	retained = draftResult(t, draftCall(s, "GET", "/api/dialect-drafts/"+id, nil, ""))
	if retained.Document.Name != "Old draft" {
		t.Fatal("chat deletion removed migrated draft")
	}
}

func TestDialectDraftAndLegacySaveShareOneOperation(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) {
			s, _ := draftRepositoryFixture(t, backend)
			testDialectDraftAndLegacySaveShareOneOperation(t, s)
		})
	}
}
func testDialectDraftAndLegacySaveShareOneOperation(t *testing.T, s *Server) {
	origin := &dialectDraftOrigin{ChatID: "same-chat", DraftID: "same-proposal"}
	request := dialectDraftRequest{Origin: origin, Name: "One dialect", Recipe: inputRecipe{Intent: "Import records"}}
	draft := draftResult(t, draftCall(s, "POST", "/api/dialect-drafts", request, ""))
	saved := draftResult(t, draftCall(s, "POST", "/api/dialect-drafts/"+draft.Document.ID+"/publish", map[string]any{}, draft.SHA256))
	legacy := draftCall(s, "POST", "/api/input-preparations", map[string]any{"name": request.Name, "recipe": request.Recipe, "chatOrigin": origin}, "")
	if legacy.Code != 200 {
		t.Fatal(legacy.Code, legacy.Body.String())
	}
	var prep inputPreparationReply
	json.Unmarshal(legacy.Body.Bytes(), &prep)
	if prep.Document.ID != saved.Document.SavedInputID {
		t.Fatal("older client created a duplicate")
	}
}
