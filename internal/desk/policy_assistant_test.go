package desk

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

type policyAssistantFixture struct {
	*actionFixture
	token    string
	policy   policyReply
	proposal assistantPolicyProposal
}

func newPolicyAssistantFixture(t *testing.T) *policyAssistantFixture {
	t.Helper()
	s, token := policyTestServer(t)
	p := policyCreate(t, s, token)
	before := assistantPolicyFields{p.Document.Candidate.Title, p.Document.Candidate.Narrative, p.Document.Candidate.ArtifactVersion}
	proposal := assistantPolicyProposal{Version: 1, ID: "policy-proposal", PolicyID: p.Document.ID, Revision: p.Revision, Before: before, Candidate: assistantPolicyFields{before.Title, "Updated draft with exact retained missing values.", "1.1.0"}, OperationID: policyOp(t)}
	proposal.Check = &struct {
		Scope  string `json:"scope"`
		Status string `json:"status"`
	}{"draft-shape-and-revision", "passed"}
	state := map[string]any{"status": "complete", "unknowns": []any{}, "questions": []any{}}
	f := &policyAssistantFixture{actionFixture: &actionFixture{s: s, state: state, chat: map[string]any{"id": "chat-one", "interaction": "work", "policy": map[string]string{"id": p.Document.ID}, "checkpoint": map[string]any{"state": state}}, cases: []any{}}, token: token, policy: p, proposal: proposal}
	f.persist(t)
	return f
}
func (f *policyAssistantFixture) persist(t *testing.T) {
	f.chat["policyProposals"] = []assistantPolicyProposal{f.proposal}
	f.save(t)
}
func (f *policyAssistantFixture) body(grant string) map[string]any {
	p := f.proposal
	content, _ := json.Marshal(policyEditRequest{OperationID: p.OperationID, BaseRevision: p.Revision, Title: p.Candidate.Title, Narrative: p.Candidate.Narrative, ArtifactVersion: p.Candidate.ArtifactVersion})
	return map[string]any{"action": "policy-update", "target": p.ID, "id": grant, "revision": p.Revision, "reviewed": true, "content": string(content)}
}
func (f *policyAssistantFixture) allow(t *testing.T) string {
	t.Helper()
	w := policyCall(f.s, f.token, "POST", "/api/assistant-approvals/chat-one/grant", f.body(""))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var body struct {
		Grant assistantActionGrant `json:"grant"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Grant.ID
}
func TestPolicyAssistantManualAutomaticAndReceiptRecovery(t *testing.T) {
	for _, mode := range []string{"apply", "commit"} {
		t.Run(mode, func(t *testing.T) {
			f := newPolicyAssistantFixture(t)
			grant := ""
			if mode == "commit" {
				grant = f.allow(t)
			}
			result := policyMutation(t, policyCall(f.s, f.token, "POST", "/api/assistant-approvals/chat-one/"+mode, f.body(grant)))
			if result.Policy.Document.Candidate.Narrative != f.proposal.Candidate.Narrative || len(result.Policy.Document.Reviews) != 0 || len(result.Policy.Document.Publications) != 0 {
				t.Fatal("draft update changed governance", result)
			}
			// Recovery is the immutable completed result even after later edits.
			edit := policyEditBody(t, result.Policy)
			edit.Narrative = "Later editor revision"
			policyMutation(t, policyCall(f.s, f.token, "PUT", "/api/policies/"+f.policy.Document.ID, edit))
			recovered := policyMutation(t, policyCall(f.s, f.token, "GET", "/api/policy-operations/"+f.proposal.OperationID, nil))
			if recovered.Policy.Revision != result.Policy.Revision || recovered.Policy.Document.Candidate.Narrative != f.proposal.Candidate.Narrative {
				t.Fatal("recovery substituted current draft", recovered)
			}
			w := policyCall(f.s, f.token, "POST", "/api/assistant-approvals/chat-one/"+mode, f.body(grant))
			if w.Code != 409 {
				t.Fatal("proposal replayed", w.Code, w.Body.String())
			}
			// Deleting the conversation leaves the independent Policy intact.
			f.chat = map[string]any{"id": "unrelated"}
			f.save(t)
			if saved := policyRead(t, f.s, f.token, f.policy.Document.ID); saved.Document.Candidate.Narrative != "Later editor revision" {
				t.Fatal("chat deletion affected Policy")
			}
		})
	}
}
func TestPolicyAssistantFailsClosedOnScopeDrift(t *testing.T) {
	tests := []struct {
		name   string
		change func(*policyAssistantFixture, map[string]any)
	}{
		{"plan", func(f *policyAssistantFixture, _ map[string]any) { f.chat["interaction"] = "plan" }},
		{"running", func(f *policyAssistantFixture, _ map[string]any) { f.state["status"] = "running" }},
		{"unknown", func(f *policyAssistantFixture, _ map[string]any) { f.state["unknowns"] = []string{"Unresolved scope"} }},
		{"question", func(f *policyAssistantFixture, _ map[string]any) {
			f.state["questions"] = []any{map[string]string{"id": "question"}}
		}},
		{"target", func(_ *policyAssistantFixture, r map[string]any) { r["target"] = "other" }},
		{"resource", func(f *policyAssistantFixture, _ map[string]any) {
			f.chat["policy"] = map[string]string{"id": "policy_" + strings.Repeat("a", 48)}
		}},
		{"candidate", func(f *policyAssistantFixture, _ map[string]any) { f.proposal.Candidate.Narrative = "Unreviewed body" }},
		{"body", func(_ *policyAssistantFixture, r map[string]any) {
			r["content"] = `{"actor":{"kind":"local"},"narrative":"spoof"}`
		}},
		{"action", func(_ *policyAssistantFixture, r map[string]any) { r["action"] = "policy-publish" }},
		{"missing check", func(f *policyAssistantFixture, _ map[string]any) { f.proposal.Check = nil }},
		{"revision", func(f *policyAssistantFixture, _ map[string]any) {
			edit := policyEditBody(t, f.policy)
			edit.Narrative = "Concurrent change"
			policyMutation(t, policyCall(f.s, f.token, "PUT", "/api/policies/"+f.policy.Document.ID, edit))
		}},
		{"session", func(f *policyAssistantFixture, _ map[string]any) { f.token = policyTestSession(t, f.s) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newPolicyAssistantFixture(t)
			grant := f.allow(t)
			body := f.body(grant)
			tc.change(f, body)
			f.persist(t)
			w := policyCall(f.s, f.token, "POST", "/api/assistant-approvals/chat-one/commit", body)
			if w.Code != 409 {
				t.Fatal(w.Code, w.Body.String())
			}
			w = policyCall(f.s, f.token, "GET", "/api/policy-operations/"+f.proposal.OperationID, nil)
			if w.Code != 404 {
				t.Fatal("refused write has receipt", w.Code, w.Body.String())
			}
		})
	}
}
func TestPolicyAssistantManualRequiresExplicitReviewAndCurrentSession(t *testing.T) {
	f := newPolicyAssistantFixture(t)
	for _, kind := range []string{"unreviewed", "launch token", "missing grant", "plan grant"} {
		t.Run(kind, func(t *testing.T) {
			body := f.body("")
			token, route := f.token, "apply"
			switch kind {
			case "unreviewed":
				body["reviewed"] = false
			case "launch token":
				token = testToken
			case "missing grant":
				route = "commit"
			case "plan grant":
				f.chat["interaction"] = "plan"
				f.persist(t)
				route = "grant"
			}
			w := policyCall(f.s, token, "POST", "/api/assistant-approvals/chat-one/"+route, body)
			if w.Code != 409 {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
	f.chat["interaction"] = "work"
	f.persist(t)
	w := policyCall(f.s, testToken, "GET", "/api/conversations", nil)
	if w.Code != 401 {
		t.Fatal("launch credential read Policy transcript", w.Code, w.Body.String())
	}
	w = policyCall(f.s, f.token, "GET", "/api/conversations", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), f.proposal.Candidate.Narrative) {
		t.Fatal("Policy conversation did not survive read", w.Code, w.Body.String())
	}
}

func TestPolicyAssistantHistoryAuthorizationDoesNotDependOnCurrentContent(t *testing.T) {
	f := newPolicyAssistantFixture(t)
	// An inaccessible current object is availability, not a loss of this
	// administrator's access to retained historical conversation material.
	f.chat["policy"] = map[string]string{"id": "policy_" + strings.Repeat("a", 48)}
	f.persist(t)
	for _, path := range []string{"/api/conversations", "/api/storage/backup"} {
		w := policyCall(f.s, f.token, "GET", path, nil)
		if w.Code != 200 {
			t.Fatal("current content gated retained history", path, w.Code, w.Body.String())
		}
		w = policyCall(f.s, testToken, "GET", path, nil)
		if w.Code != 401 {
			t.Fatal("launch credential retrieved Policy history", path, w.Code, w.Body.String())
		}
	}
	f.s.sessions.revoke(f.token)
	for _, path := range []string{"/api/conversations", "/api/storage/backup"} {
		w := policyCall(f.s, f.token, "GET", path, nil)
		if w.Code != 401 {
			t.Fatal("expired session read history", path, w.Code)
		}
	}
}

func TestPolicyAssistantIncomingConversationRequiresRealSessionBeforeWriting(t *testing.T) {
	f := newPolicyAssistantFixture(t)
	// Start with ordinary history so checking only the old snapshot cannot
	// satisfy admission for newly introduced Policy-linked material.
	f.chat = map[string]any{"id": "ordinary"}
	f.save(t)
	before := policyCall(f.s, testToken, "GET", "/api/conversations", nil)
	var current conversationReply
	if before.Code != 200 || json.Unmarshal(before.Body.Bytes(), &current) != nil {
		t.Fatal(before.Code, before.Body.String())
	}
	raw, _ := json.Marshal(map[string]any{"version": 1, "chats": []any{map[string]any{"id": "policy-chat", "policy": map[string]string{"id": f.policy.Document.ID}}}})
	r := httptest.NewRequest("PUT", "/api/conversations", strings.NewReader(string(raw)))
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("If-Match", current.SHA256)
	w := httptest.NewRecorder()
	f.s.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("launch credential wrote Policy context", w.Code, w.Body.String())
	}
	after := policyCall(f.s, testToken, "GET", "/api/conversations", nil)
	if before.Body.String() != after.Body.String() {
		t.Fatal("refused incoming history was written")
	}
}

func TestPolicyAssistantFreshHistoryAndBackupBeforeAnyChatSnapshot(t *testing.T) {
	for _, savedPolicy := range []bool{false, true} {
		name := "brand-new project"
		if savedPolicy {
			name = "saved Policy without a chat"
		}
		t.Run(name, func(t *testing.T) {
			// Match the standalone CLI's default path selection rather than the
			// existing helpers' explicit DeskConfigDir/history fixture override.
			configHome, dataHome := t.TempDir(), t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", configHome)
			t.Setenv("XDG_DATA_HOME", dataHome)
			s, err := New(Config{ProjectDir: t.TempDir(), Token: testToken, Port: 8791, JpackBin: jpackBinary(), Logger: log.New(io.Discard, "", 0)})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = s.Close() })
			if s.defaultChatDataPath() != filepath.Join(dataHome, deskDirName, "history") {
				t.Fatal("fixture bypassed default history selection", s.defaultChatDataPath())
			}
			token := policyTestSession(t, s)
			if savedPolicy {
				policyCreate(t, s, token)
			}
			// Backing up first also exercises a store with no conversation file,
			// before GET has had an opportunity to establish any private binding.
			backup := policyCall(s, token, "GET", "/api/storage/backup", nil)
			if backup.Code != 200 {
				t.Fatal("fresh backup", backup.Code, backup.Body.String())
			}
			archive, err := zip.NewReader(bytes.NewReader(backup.Body.Bytes()), int64(backup.Body.Len()))
			if err != nil {
				t.Fatal("fresh backup was not a complete archive", err)
			}
			for _, file := range archive.File {
				if conversationFileName.MatchString(file.Name) {
					t.Fatal("backup fabricated a conversation snapshot", file.Name)
				}
			}
			for _, credential := range []string{testToken, token} {
				w := policyCall(s, credential, "GET", "/api/conversations", nil)
				var reply conversationReply
				if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &reply) != nil {
					t.Fatal("fresh history", w.Code, w.Body.String())
				}
				if reply.Project != s.projectDir || reply.SHA256 != "absent" || string(reply.Content) != `{"version":1,"chats":[]}` {
					t.Fatal("fresh history lost its canonical empty document", reply)
				}
			}
		})
	}
}
