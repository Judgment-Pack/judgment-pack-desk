package desk

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var privateWorkspaceTestSessions = struct {
	sync.Mutex
	byServer map[*httptest.Server]string
}{byServer: map[*httptest.Server]string{}}

// Private-store fixtures use the same launch/session exchange as the browser.
// Reuse the actor so an action grant stays scoped to one signed-in session.
func privateWorkspaceTestSession(t *testing.T, server *httptest.Server) string {
	t.Helper()
	privateWorkspaceTestSessions.Lock()
	defer privateWorkspaceTestSessions.Unlock()
	if token := privateWorkspaceTestSessions.byServer[server]; token != "" {
		return token
	}
	token := beginSession(t, server)
	privateWorkspaceTestSessions.byServer[server] = token
	t.Cleanup(func() {
		privateWorkspaceTestSessions.Lock()
		defer privateWorkspaceTestSessions.Unlock()
		delete(privateWorkspaceTestSessions.byServer, server)
	})
	return token
}

func TestPrivateBriefAndTestStoresKeepCustodyAfterSourceDeletion(t *testing.T) {
	s, token := policyTestServer(t)
	p := policyCreate(t, s, token)
	source := policyAuthoringRead(t, s, token, p, "revisionId="+p.Document.Candidate.RevisionID)
	policyWorkspaceSave(t, s, token, "/api/draft-packs", policyWorkspaceBody(true, source))
	privateText := "Private Policy ambiguity retained in the recovered test rationale."
	suite := map[string]any{"version": 1, "suites": map[string]any{"private-pack": map[string]any{"packText": `{"title":"Private candidate"}`, "unknowns": []string{privateText}, "cases": []any{map[string]any{"id": "case-one", "rationale": privateText, "row": map[string]any{"facts": map[string]any{}}}}}}}
	policyWorkspaceSave(t, s, token, "/api/pack-tests", suite)
	snapshot := policyWorkspaceRead(t, s, token, "/api/briefs?subject=pack-draft:interpretation-one").Snapshot
	command := BriefCommand{Action: "begin", Subject: "pack-draft:interpretation-one", Snapshot: snapshot, Model: "fixture"}
	for _, path := range []string{"/api/briefs", "/api/briefs?subject=pack:ordinary"} {
		w := policyWorkspaceCall(s, testToken, "POST", path, "", command)
		if w.Code != 401 {
			t.Fatal("launch credential admitted a private command body", path, w.Code, w.Body.String())
		}
	}
	started := policyWorkspaceCall(s, token, "POST", "/api/briefs", "", command)
	var reply conversationReply
	var store BriefStore
	if started.Code != 200 || json.Unmarshal(started.Body.Bytes(), &reply) != nil || json.Unmarshal(reply.Content, &store) != nil {
		t.Fatal("authenticated brief begin", started.Code, started.Body.String())
	}
	command.Token = store.Subjects[command.Subject].Pending.Token
	for _, path := range []string{"/api/briefs", "/api/briefs?subject=pack:ordinary", "/api/pack-tests"} {
		if w := policyCall(s, testToken, "GET", path, nil); w.Code != 401 {
			t.Fatal("launch credential read pending private material", path, w.Code, w.Body.String())
		}
	}
	command.Action = "complete"
	command.Text = BriefText{Context: privateText, Findings: "Private findings.", Uncertainty: "Retained uncertainty.", NextAction: "Review privately."}
	if w := policyWorkspaceCall(s, token, "POST", "/api/briefs?subject=pack:ordinary", "", command); w.Code != 200 {
		t.Fatal("authenticated command uses its body subject", w.Code, w.Body.String())
	}
	policyWorkspaceSave(t, s, token, "/api/draft-packs", map[string]any{"version": 1, "drafts": []any{}})
	policyWorkspaceSave(t, s, token, "/api/conversations", map[string]any{"version": 1, "chats": []any{}})
	if err := os.Remove(filepath.Join(s.projectDir, policyDirectory, "ledger.json")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/briefs", "/api/briefs?subject=pack:ordinary", "/api/pack-tests"} {
		read := policyWorkspaceRead(t, s, token, path)
		if !strings.Contains(string(read.Content), privateText) {
			t.Fatal("source deletion erased retained private material", path)
		}
		if w := policyCall(s, testToken, "GET", path, nil); w.Code != 401 {
			t.Fatal("source deletion declassified private store", path, w.Code, w.Body.String())
		}
	}
	before := policyWorkspaceRead(t, s, token, "/api/pack-tests")
	if w := policyWorkspaceCall(s, testToken, "PUT", "/api/pack-tests", before.SHA256, suite); w.Code != 401 {
		t.Fatal("launch credential wrote retained tests", w.Code, w.Body.String())
	}
	if after := policyWorkspaceRead(t, s, token, "/api/pack-tests"); after.SHA256 != before.SHA256 {
		t.Fatal("refused write changed tests")
	}
	backup := policyCall(s, token, "GET", "/api/storage/backup", nil)
	if backup.Code != 200 {
		t.Fatal("authorized export without current source", backup.Code, backup.Body.String())
	}
	if w := policyCall(s, testToken, "GET", "/api/storage/backup", nil); w.Code != 401 {
		t.Fatal("launch credential exported retained briefs/tests", w.Code, w.Body.String())
	}
	status := policyCall(s, token, "GET", "/api/storage", nil)
	var location storageStatus
	if json.Unmarshal(status.Body.Bytes(), &location) != nil {
		t.Fatal("invalid storage status")
	}
	move := moveChatData{Path: filepath.Join(t.TempDir(), "moved"), Revision: location.Revision}
	if w := policyCall(s, testToken, "POST", "/api/storage/move", move); w.Code != 401 {
		t.Fatal("launch credential moved retained private stores", w.Code, w.Body.String())
	}
	if w := policyCall(s, token, "POST", "/api/storage/move", move); w.Code != 200 {
		t.Fatal("authorized private move", w.Code, w.Body.String())
	}
	config := s.cfg
	config.DeskConfigDir = t.TempDir()
	restored, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restored.Close() })
	restoredToken := policyTestSession(t, restored)
	target := filepath.Join(t.TempDir(), "restored")
	if w := policyRestoreCall(t, restored, testToken, target, backup.Body.Bytes()); w.Code != 401 {
		t.Fatal("launch credential restored imported private stores", w.Code, w.Body.String())
	}
	if w := policyRestoreCall(t, restored, restoredToken, target, backup.Body.Bytes()); w.Code != 200 {
		t.Fatal("authorized restore unnecessarily required Policy ledger", w.Code, w.Body.String())
	}
	for _, path := range []string{"/api/briefs", "/api/pack-tests"} {
		if !strings.Contains(string(policyWorkspaceRead(t, restored, restoredToken, path).Content), privateText) {
			t.Fatal("restore lost retained private content", path)
		}
		if w := policyCall(restored, testToken, "GET", path, nil); w.Code != 401 {
			t.Fatal("restored private store leaked", path, w.Code)
		}
	}
	s.sessions.revoke(token)
	for _, path := range []string{"/api/briefs", "/api/briefs?subject=pack:ordinary", "/api/pack-tests", "/api/storage/backup"} {
		if w := policyCall(s, token, "GET", path, nil); w.Code != 401 {
			t.Fatal("revoked session accessed retained private data", path, w.Code)
		}
	}
}

func TestPrivateWorkspaceRejectsLaunchBeforeEmptyAndIndirectReads(t *testing.T) {
	s, token := policyTestServer(t)
	preparation := "prep_" + strings.Repeat("a", 32)
	evaluation := "eval_" + strings.Repeat("b", 32)
	for _, route := range []struct{ method, path string }{
		{"GET", "/api/briefs"}, {"GET", "/api/pack-tests"},
		{"POST", "/api/briefs"}, {"PUT", "/api/pack-tests"},
		{"POST", "/api/assistant-approvals/chat-one/grant"},
		{"GET", "/api/input-preparations/" + preparation + "/evaluations"},
		{"POST", "/api/input-preparations/" + preparation + "/evaluations"},
		{"GET", "/api/input-preparations/" + preparation + "/evaluations/" + evaluation},
		{"POST", "/api/input-preparations/" + preparation + "/evaluations/" + evaluation + "/subscription-upgrade"},
	} {
		if w := policyCall(s, testToken, route.method, route.path, nil); w.Code != 401 {
			t.Fatal("launch credential reached private indirect read/admission", route, w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/api/briefs", "/api/pack-tests"} {
		if got := policyWorkspaceRead(t, s, token, path); got.SHA256 != "absent" {
			t.Fatal("ordinary authenticated empty store changed", path)
		}
	}
}

func TestPolicyInterpretationProposalBackendParity(t *testing.T) {
	policyID := "policy_" + strings.Repeat("a", 48)
	revisionID := "revision_" + strings.Repeat("b", 48)
	base := map[string]any{"version": 1, "id": "proposal-one", "policyId": policyID, "revisionId": revisionID, "createdAt": "2026-10-18T12:00:00.123Z"}
	for _, test := range []struct {
		field string
		value any
		valid bool
	}{{"id", "bad/slash", false}, {"id", strings.Repeat("a", 81), false}, {"id", strings.Repeat("a", 80), true}, {"openedChatId", "bad/slash", false}, {"openedChatId", "", false}, {"openedChatId", strings.Repeat("a", 81), false}, {"openedChatId", strings.Repeat("a", 80), true}, {"publicationId", nil, false}, {"afterTurnId", nil, false}, {"openedChatId", nil, false}, {"OpenedChatId", "hidden-alias", false}, {"CreatedAt", "2026-10-18T12:00:00Z", false}, {"createdAt", "2026-02-31T12:00:00Z", false}, {"createdAt", "not-a-date", false}, {"createdAt", "2026-10-18T1:00:00Z", false}, {"createdAt", "2026-10-18T12:00:00,123Z", false}, {"createdAt", "2026-10-18T12:00:00.1234567890Z", false}, {"createdAt", "2026-10-18T12:00:00+24:00", false}, {"createdAt", "2026-10-18T12:00:00.123456789+23:59", true}, {"afterTurnId", strings.Repeat("é", 128), true}, {"afterTurnId", strings.Repeat("é", 129), false}} {
		proposal := map[string]any{}
		for key, value := range base {
			proposal[key] = value
		}
		proposal[test.field] = test.value
		body, _ := json.Marshal(map[string]any{"version": 1, "chats": []any{map[string]any{"id": "policy-chat", "policy": map[string]string{"id": policyID}, "policyInterpretations": []any{proposal}}}})
		err := validateConversations(body)
		if (err == nil) != test.valid {
			t.Fatal("proposal schema parity", test.field, test.value, err)
		}
	}
}
