package desk

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

func assistantSourceTestView(t *testing.T, s *Server, token string) assistantSourceCatalogView {
	t.Helper()
	response := policyCall(s, token, "GET", "/api/assistant-source-approvals/chat-one", nil)
	if response.Code != 200 {
		t.Fatalf("review %d: %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("review was cacheable")
	}
	var view assistantSourceCatalogView
	assistantRunMust(t, json.Unmarshal(response.Body.Bytes(), &view))
	if view.Version != 1 || len(view.Sources) != 4 {
		t.Fatal("incomplete Desk catalog", view)
	}
	return view
}
func assistantSourceTestRow(t *testing.T, view assistantSourceCatalogView, id string) assistantSourceReviewView {
	t.Helper()
	for _, row := range view.Sources {
		if row.Snapshot.AdapterID == id {
			return row
		}
	}
	t.Fatal("missing source", id)
	return assistantSourceReviewView{}
}
func assistantSourceTestApprove(t *testing.T, s *Server, token string, row assistantSourceReviewView) assistantSourceReviewView {
	t.Helper()
	response := policyCall(s, token, "POST", "/api/assistant-source-approvals/chat-one/approve", assistantSourceApproveRequest{ReviewID: row.ReviewID, ReviewRevision: row.ReviewRevision})
	if response.Code != 200 {
		t.Fatalf("approve %d: %s", response.Code, response.Body.String())
	}
	var view assistantSourceCatalogView
	assistantRunMust(t, json.Unmarshal(response.Body.Bytes(), &view))
	result := assistantSourceTestRow(t, view, row.Snapshot.AdapterID)
	if result.Status != "approved" || result.Approval == nil {
		t.Fatal("approval not persisted", result)
	}
	return result
}
func TestAssistantSourceApprovalLifecycle(t *testing.T) {
	s, token, _, executor := assistantProtocolFixture(t, nil)
	initial := assistantSourceTestView(t, s, token)
	workflow := assistantSourceTestRow(t, initial, "desk-research-workflow")
	if workflow.Status != "review-required" || workflow.ReviewID == "" {
		t.Fatal("concrete Desk workflow absent", workflow)
	}
	for _, id := range []string{"desk-search", "desk-link", "desk-website"} {
		row := assistantSourceTestRow(t, initial, id)
		if row.Status != "setup-required" || row.ReviewID != "" || row.Approval != nil {
			t.Fatal("unobserved source became authority", row)
		}
	}
	again := assistantSourceTestRow(t, assistantSourceTestView(t, s, token), "desk-research-workflow")
	if again.ReviewID != workflow.ReviewID {
		t.Fatal("unchanged read minted another review")
	}
	approved := assistantSourceTestApprove(t, s, token, workflow)
	current := assistantSourceTestRow(t, assistantSourceTestView(t, s, token), "desk-research-workflow")
	if current.Status != "approved" || !reflect.DeepEqual(current.Approval, approved.Approval) {
		t.Fatal("lost persisted approval")
	}
	// Delivery retry is never silently reapproved after a possibly lost response.
	retry := policyCall(s, token, "POST", "/api/assistant-source-approvals/chat-one/approve", assistantSourceApproveRequest{workflow.ReviewID, workflow.ReviewRevision})
	if retry.Code != 409 {
		t.Fatal("consumed review replayed", retry.Code)
	}
	stale := policyCall(s, token, "POST", "/api/assistant-source-approvals/chat-one/revoke", assistantSourceRevokeRequest{approved.Approval.ID, 2})
	if stale.Code != 400 {
		t.Fatal("stale revocation accepted", stale.Code)
	}
	revoke := policyCall(s, token, "POST", "/api/assistant-source-approvals/chat-one/revoke", assistantSourceRevokeRequest{approved.Approval.ID, 1})
	if revoke.Code != 200 {
		t.Fatal("revoke", revoke.Code, revoke.Body.String())
	}
	var receipt struct {
		Status   string                           `json:"status"`
		Approval assistantSourceApprovalReference `json:"approval"`
	}
	assistantRunMust(t, json.Unmarshal(revoke.Body.Bytes(), &receipt))
	if receipt.Status != "revoked" || receipt.Approval.Revision != 2 || receipt.Approval.RevokedAt == "" {
		t.Fatal("revocation receipt", receipt)
	}
	reviewed := assistantSourceTestRow(t, assistantSourceTestView(t, s, token), "desk-research-workflow")
	if reviewed.Status != "revoked" || reviewed.ReviewID == workflow.ReviewID || reviewed.ReviewID == "" {
		t.Fatal("revocation reactivated or lost new explicit review", reviewed)
	}
	if executor.calls.Load() != 0 {
		t.Fatal("review invoked model execution")
	}
	if s.localGateway != nil {
		t.Fatal("review started managed Gateway")
	}
	// Tokens are never part of the durable record.
	data, err := s.openChatData()
	assistantRunMust(t, err)
	defer data.root.Close()
	project, err := assistantRunNamespace(data.root, s.conversationIdentity())
	assistantRunMust(t, err)
	raw, err := readPrivateData(data.root, assistantSourceLedgerName(project), assistantRunRecordLimit)
	assistantRunMust(t, err)
	if strings.Contains(string(raw), token) {
		t.Fatal("raw bearer retained")
	}
}
func TestAssistantSourceApprovalScopeAndRestart(t *testing.T) {
	for _, change := range []string{"foreign-session", "principal", "policy", "restart", "foreign-chat", "foreign-project", "signout"} {
		t.Run(change, func(t *testing.T) {
			s, token, _, _ := assistantProtocolFixture(t, nil)
			row := assistantSourceTestRow(t, assistantSourceTestView(t, s, token), "desk-research-workflow")
			approved := assistantSourceTestApprove(t, s, token, row)
			current := token
			switch change {
			case "foreign-session":
				current = policyTestSession(t, s)
			case "principal":
				current, _ = s.sessions.createExpiring("another principal", nil, "", "")
			case "policy":
				s.signIn.mu.Lock()
				s.signIn.generation++
				s.signIn.mu.Unlock()
			case "restart":
				assistantRunMust(t, s.Close())
				var err error
				s, err = New(s.cfg)
				assistantRunMust(t, err)
				t.Cleanup(func() { _ = s.Close() })
				current = policyTestSession(t, s)
			case "foreign-chat":
				response := policyCall(s, token, "POST", "/api/assistant-source-approvals/chat-other/approve", assistantSourceApproveRequest{row.ReviewID, 1})
				if response.Code < 400 {
					t.Fatal("foreign chat admitted")
				}
				return
			case "foreign-project":
				other, otherToken, _, _ := assistantProtocolFixture(t, nil)
				response := policyCall(other, otherToken, "POST", "/api/assistant-source-approvals/chat-one/approve", assistantSourceApproveRequest{row.ReviewID, 1})
				if response.Code < 400 {
					t.Fatal("foreign project admitted")
				}
				return
			case "signout":
				s.sessions.revoke(token)
			}
			response := policyCall(s, current, "POST", "/api/assistant-source-approvals/chat-one/revoke", assistantSourceRevokeRequest{approved.Approval.ID, 1})
			if response.Code < 400 {
				t.Fatal("foreign authorization revoked approval", response.Code)
			}
			if change != "signout" {
				view := assistantSourceTestView(t, s, current)
				if assistantSourceTestRow(t, view, "desk-research-workflow").Status == "approved" {
					t.Fatal("approval crossed scope")
				}
			}
		})
	}
}
func TestAssistantSourceApprovalClosedRequests(t *testing.T) {
	s, token, _, _ := assistantProtocolFixture(t, nil)
	id := strings.Repeat("a", 32)
	requests := []string{
		`null`, `{}`, `{"reviewId":"` + id + `"}`, `{"reviewId":"` + id + `","reviewRevision":null}`,
		`{"reviewId":"` + id + `","reviewRevision":1,"source":"custom"}`,
		`{"ReviewId":"` + id + `","reviewRevision":1}`,
		`{"reviewId":"` + id + `","reviewRevision":1,"reviewRevision":1}`,
		`{"reviewId":"` + id + `","reviewRevision":1.0}`,
		`{"reviewId":"` + id + `","reviewRevision":-1}`,
		`{"reviewId":"` + id + `","reviewRevision":9007199254740992}`,
		`{"reviewId":"` + id + `","reviewRevision":1} {}`,
		strings.Repeat(" ", 4097),
	}
	for _, raw := range requests {
		r := httptest.NewRequest(http.MethodPost, "/api/assistant-source-approvals/chat-one/approve", strings.NewReader(raw))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("malformed selector %q status %d", raw, w.Code)
		}
	}
	for _, path := range []string{"/api/assistant-source-approvals/chat-one?source=custom", "/api/assistant-source-approvals/chat-one/approve?review=1"} {
		response := policyCall(s, token, "POST", path, map[string]any{})
		if response.Code < 400 {
			t.Fatal("query accepted", path)
		}
	}
	if response := policyCall(s, "", "GET", "/api/assistant-source-approvals/chat-one", nil); response.Code < 400 {
		t.Fatal("anonymous review")
	}
}
func TestAssistantSourceApprovalChangedConfiguration(t *testing.T) {
	s, token, _, _ := assistantProtocolFixture(t, nil)
	old := assistantSourceTestRow(t, assistantSourceTestView(t, s, token), "desk-research-workflow")
	writeDeskConfig(t, s, researchDeskFile("https://example.invalid"))
	response := policyCall(s, token, "POST", "/api/assistant-source-approvals/chat-one/approve", assistantSourceApproveRequest{old.ReviewID, 1})
	if response.Code != 409 {
		t.Fatal("stale config approved", response.Code, response.Body.String())
	}
	fresh := assistantSourceTestRow(t, assistantSourceTestView(t, s, token), "desk-research-workflow")
	if fresh.Snapshot.ConfigurationDigest == old.Snapshot.ConfigurationDigest || fresh.ReviewID == old.ReviewID {
		t.Fatal("changed config recaptured old review")
	}
	assistantSourceTestApprove(t, s, token, fresh)
	writeDeskConfig(t, s, `{"deskConfigVersion":1}`)
	current := assistantSourceTestRow(t, assistantSourceTestView(t, s, token), "desk-research-workflow")
	if current.Status == "approved" {
		t.Fatal("changed configuration remained approved")
	}
}
func TestAssistantSourceCatalogActualMetadataAndNoExecution(t *testing.T) {
	owners := assistantSourceOwners()
	catalog := assistantSourceBaseCatalog()
	if len(owners) != 4 || len(catalog) != 4 {
		t.Fatal("adapter catalog clipped")
	}
	names := []string{"search_sources", "read_link", "explore_website", "request_dialect_research"}
	effects := []string{"read", "read", "read", "workflow"}
	for i, owner := range owners {
		actual := owner.declaration
		row := catalog[i]
		declaration := row.CompleteDeclarations[0]
		if actual.Name != names[i] || actual.Effect != effects[i] || actual.Source != nil || actual.Execute != nil || declaration.Name != actual.Name || declaration.Description != actual.Description || !reflect.DeepEqual(declaration.InputSchema, actual.InputSchema) || declaration.Delegation != actual.Delegation {
			t.Fatal("metadata/execution extraction changed", i)
		}
		if row.Kind != "custom" || row.Trust != "first-party" || len(row.CompleteDeclarations) != 1 {
			t.Fatal("false inventory", row)
		}
		digest := runcontext.ToolListDigest([]runcontext.ListedTool{{Name: actual.Name, Description: actual.Description, InputSchema: actual.InputSchema, Taxonomy: owner.taxonomy}})
		if row.ToolListDigest != digest {
			t.Fatal("digest did not bind complete actual declaration")
		}
		changed := *owner.taxonomy
		changed.Operation = "configure"
		if runcontext.ToolListDigest([]runcontext.ListedTool{{Name: actual.Name, Description: actual.Description, InputSchema: actual.InputSchema, Taxonomy: &changed}}) == digest {
			t.Fatal("taxonomy missing from digest")
		}
	}
}
func TestAssistantSourceApprovalClosedStorageAndReadback(t *testing.T) {
	s, token, _, _ := assistantProtocolFixture(t, nil)
	assistantSourceTestView(t, s, token)
	store, err := s.openChatData()
	assistantRunMust(t, err)
	defer store.root.Close()
	project, err := assistantRunNamespace(store.root, s.conversationIdentity())
	assistantRunMust(t, err)
	raw, err := readPrivateData(store.root, assistantSourceLedgerName(project), assistantRunRecordLimit)
	assistantRunMust(t, err)
	ledger, err := decodeAssistantSourceLedger(raw, project)
	assistantRunMust(t, err)
	for _, mutate := range []func(map[string]any){
		func(v map[string]any) { v["unknown"] = true },
		func(v map[string]any) { delete(v, "approvals") },
		func(v map[string]any) { v["reviews"] = nil },
		func(v map[string]any) { v["version"] = 2 },
		func(v map[string]any) { v["project"] = strings.Repeat("a", 64) },
		func(v map[string]any) {
			v["reviews"].([]any)[0].(map[string]any)["scope"].(map[string]any)["sessionScope"] = "bearer"
		},
		func(v map[string]any) {
			v["reviews"].([]any)[0].(map[string]any)["snapshot"].(map[string]any)["kind"] = "mcp"
		},
		func(v map[string]any) {
			v["reviews"].([]any)[0].(map[string]any)["snapshot"].(map[string]any)["toolListDigest"] = "sha256:" + strings.Repeat("a", 64)
		},
	} {
		var value map[string]any
		assistantRunMust(t, json.Unmarshal(raw, &value))
		mutate(value)
		if _, err := decodeAssistantSourceLedger(assistantChatJSON(value), project); err == nil {
			t.Fatal("malformed private authority accepted")
		}
	}
	duplicate := strings.Replace(string(raw), `"version":1`, `"version":1,"version":1`, 1)
	if _, err := decodeAssistantSourceLedger([]byte(duplicate), project); err == nil {
		t.Fatal("duplicate custody accepted")
	}
	if _, err := decodeAssistantSourceLedger(append(raw, []byte(strings.Repeat(" ", assistantRunRecordLimit))...), project); err == nil {
		t.Fatal("oversized custody accepted")
	}
	// A writer reporting success without preserving the exact bytes is uncertain.
	err = writeAssistantSourceLedger(t.Context(), store, ledger, func(root *os.Root, name string, data []byte) error {
		return writePrivateData(root, name, []byte(`{"version":1}`))
	})
	if err == nil {
		t.Fatal("false-success writer returned authority")
	}
	err = writeAssistantSourceLedger(t.Context(), store, ledger, func(*os.Root, string, []byte) error { return errors.New("uncertain fixture write") })
	if err == nil {
		t.Fatal("uncertain writer returned authority")
	}
	assistantRunMust(t, writePrivateData(store.root, assistantSourceLedgerName(project), raw))
	// Existing record count bound refuses; it never drops declarations or history.
	ledger.Reviews = make([]assistantSourceReview, assistantRunCountLimit+1)
	if validateAssistantSourceLedger(ledger, project) == nil {
		t.Fatal("review count bound ignored")
	}
}
func TestAssistantSourceReviewNoNetworkAndAuthorityChange(t *testing.T) {
	s, token, _, executor := assistantProtocolFixture(t, nil)
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		t.Error("source review called HTTP acquisition")
	}))
	defer upstream.Close()
	writeDeskConfig(t, s, researchDeskFile(upstream.URL))
	assistantSourceTestView(t, s, token)
	if calls != 0 || executor.calls.Load() != 0 {
		t.Fatal("review performed source/model work")
	}
	// A locally changed authority/public key invalidates held local observation.
	request := assistantProtocolRequest(token)
	scope, source, err := s.assistantSourceScopeCurrent(request, "chat-one")
	assistantRunMust(t, err)
	catalog, err := s.assistantSourceCatalog(t.Context(), source, scope.Project)
	assistantRunMust(t, err)
	changed := strings.Replace(researchDeskFile(upstream.URL), "gateway:test", "gateway:changed", 1)
	writeDeskConfig(t, s, changed)
	if err = s.assistantSourceCatalogLocalCurrent(source, catalog); err == nil {
		t.Fatal("changed authority accepted")
	}
	if calls != 0 {
		t.Fatal("currentness sent source call")
	}
	// Review cannot infer search consent from the configured preference.
	assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "jpack-search.json"), []byte(`{"version":1,"connection":"demo","mode":"auto"}`), 0600))
	view := assistantSourceTestView(t, s, token)
	if assistantSourceTestRow(t, view, "desk-search").Approval != nil {
		t.Fatal("configuration synthesized source approval")
	}
}

func TestAssistantSourceObservedSearchRevisionAndCompleteReadCatalog(t *testing.T) {
	s, token, _, executor := assistantProtocolFixture(t, nil)
	bundle, status := assistantSearchBundle(t)
	revision := strings.Repeat("a", 64)
	writeStatus := func(revision string) {
		t.Helper()
		assistantRunMust(t, os.WriteFile(status, assistantChatJSON(map[string]any{"version": 1, "connections": []any{map[string]any{"id": "demo", "revision": revision, "provider": "tavily"}}}), 0600))
	}
	writeStatus(revision)
	done := make(chan struct{})
	build, err := launchedGatewayBuild(bundle)
	assistantRunMust(t, err)
	s.localGateway = &localGateway{bundle: bundle, done: done, pin: &localGatewayPin{build: &build, URL: "http://127.0.0.1:43210", Authority: "gateway:test", Signer: localSigner{Algorithm: "ed25519", Public: testSignerPublic}, documentProcessing: true}}
	t.Cleanup(func() { close(done) })
	writeDeskConfig(t, s, `{"deskConfigVersion":1}`)
	assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "jpack-search.json"), []byte(`{"version":1,"connection":"demo","mode":"auto"}`), 0600))
	// Test setup explicitly starts only its authored status fixture. The production
	// review path must reuse it and never start a companion or Gateway itself.
	companion := s.connectionCompanion("web-search", true)
	_, err = companion.call(t.Context(), bundle, platformPath(s.assistant.dir, "gateway-connections"), "status", json.RawMessage(`{}`), "web-search", false)
	assistantRunMust(t, err)
	view := assistantSourceTestView(t, s, token)
	search := assistantSourceTestRow(t, view, "desk-search")
	if search.Status != "review-required" || search.Snapshot.Configuration.Revision != revision {
		t.Fatal("actual selected connection not observed", search)
	}
	if assistantSourceTestRow(t, view, "desk-link").Status != "review-required" {
		t.Fatal("document adapter lost supported domain")
	}
	if assistantSourceTestRow(t, view, "desk-website").Status != "setup-required" {
		t.Fatal("bundle manifest invented website capability")
	}
	writeStatus(strings.Repeat("b", 64))
	response := policyCall(s, token, "POST", "/api/assistant-source-approvals/chat-one/approve", assistantSourceApproveRequest{search.ReviewID, 1})
	if response.Code != 409 {
		t.Fatal("changed observed revision approved", response.Code, response.Body.String())
	}
	fresh := assistantSourceTestRow(t, assistantSourceTestView(t, s, token), "desk-search")
	approved := assistantSourceTestApprove(t, s, token, fresh)
	if approved.Snapshot.Configuration.Revision != strings.Repeat("b", 64) {
		t.Fatal("new explicit review did not bind actual revision")
	}
	if executor.calls.Load() != 0 {
		t.Fatal("review started model")
	}
}
func TestAssistantSourceExplicitExternalReadAdapters(t *testing.T) {
	s, token, _, _ := assistantProtocolFixture(t, nil)
	var config map[string]any
	assistantRunMust(t, json.Unmarshal([]byte(researchDeskFile("https://example.invalid")), &config))
	research := config["research"].(map[string]any)
	research["documents"] = map[string]any{"source": "documents"}
	research["sources"].(map[string]any)["web"] = map[string]any{"source": "web", "discovery": "web-discovery"}
	writeDeskConfig(t, s, string(assistantChatJSON(config)))
	view := assistantSourceTestView(t, s, token)
	for _, id := range []string{"desk-link", "desk-website"} {
		row := assistantSourceTestRow(t, view, id)
		if row.Status != "review-required" {
			t.Fatal("explicit external adapter unavailable", id, row)
		}
		assistantSourceTestApprove(t, s, token, row)
	}
}

func TestAssistantSourceApprovalStaleCompleteList(t *testing.T) {
	s, token, _, _ := assistantProtocolFixture(t, nil)
	row := assistantSourceTestRow(t, assistantSourceTestView(t, s, token), "desk-research-workflow")
	store, err := s.openChatData()
	assistantRunMust(t, err)
	defer store.root.Close()
	project, err := assistantRunNamespace(store.root, s.conversationIdentity())
	assistantRunMust(t, err)
	ledger, err := readAssistantSourceLedger(store.root, project)
	assistantRunMust(t, err)
	for i := range ledger.Reviews {
		review := &ledger.Reviews[i]
		if review.ID != row.ReviewID {
			continue
		}
		snapshot := &review.Snapshot
		declaration := &snapshot.CompleteDeclarations[0]
		declaration.Description = "An earlier complete owner declaration."
		taxonomy := &runcontext.ToolTaxonomy{Operation: declaration.Taxonomy.Operation, Effect: declaration.Taxonomy.Effect}
		snapshot.ToolListDigest = runcontext.ToolListDigest([]runcontext.ListedTool{{Name: declaration.Name, Description: declaration.Description, InputSchema: declaration.InputSchema, Taxonomy: taxonomy}})
		snapshot.ConfigurationDigest = assistantSourceConfigurationDigest(*snapshot, project)
		snapshot.Identity.Label = "Desk research workflow " + snapshot.ConfigurationDigest
		snapshot.IdentityDigest = runcontext.IdentityDigest(*snapshot.Identity)
	}
	assistantRunMust(t, writeAssistantSourceLedger(t.Context(), store, ledger, writePrivateData))
	response := policyCall(s, token, "POST", "/api/assistant-source-approvals/chat-one/approve", assistantSourceApproveRequest{row.ReviewID, 1})
	if response.Code != 409 {
		t.Fatal("stale complete list approved", response.Code, response.Body.String())
	}
}
