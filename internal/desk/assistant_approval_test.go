package desk

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type actionFixture struct {
	s                             *Server
	ts                            *httptest.Server
	chat, state, candidate, check map[string]any
	cases                         []any
	before, after                 string
}

func newActionFixture(t *testing.T) *actionFixture {
	t.Helper()
	s, ts, _ := assistantServer(t)
	f := &actionFixture{s: s, ts: ts, before: `{"id":"urn:example","version":"0.1.0"}`, after: `{"id":"urn:example","version":"0.1.1"}`}
	if e := os.WriteFile(filepath.Join(s.projectDir, "pack.json"), []byte(f.before), 0600); e != nil {
		t.Fatal(e)
	}
	f.cases = []any{map[string]any{"id": "case-one", "row": map[string]any{"id": "case-one", "facts": map[string]any{"zero": 0, "no": false, "decimal": "24", "null": nil}, "expectedDisposition": map[string]any{"kind": "outcome", "outcomeId": "yes"}}}}
	cases, _ := json.Marshal(f.cases)
	f.check = map[string]any{"valid": true, "documentDigest": digestOf([]byte(f.after)), "suiteKey": string(cases), "totalCases": 1, "report": map[string]any{"status": "passed", "summary": map[string]any{"total": 1, "passed": 1, "mismatched": 0}, "packs": []any{map[string]any{"rows": []any{map[string]any{"id": "case-one", "status": "passed"}}, "coverage": []any{}}}}}
	f.candidate = map[string]any{"text": f.after, "digest": digestOf([]byte(f.after)), "packReview": map[string]any{"basis": map[string]any{"packId": "example", "path": "pack.json", "text": f.before}, "check": f.check}}
	f.state = map[string]any{"status": "complete", "candidates": []any{f.candidate}, "unknowns": []any{}, "questions": []any{}}
	f.chat = map[string]any{"id": "chat-one", "interaction": "work", "pack": map[string]any{"id": "example", "path": "pack.json"}, "checkpoint": map[string]any{"state": f.state, "sources": []any{}}}
	f.save(t)
	return f
}
func (f *actionFixture) save(t *testing.T) {
	t.Helper()
	store, e := f.s.openChatData()
	if e != nil {
		t.Fatal(e)
	}
	defer store.root.Close()
	documents := map[string]any{f.s.conversationName(): map[string]any{"version": 1, "chats": []any{f.chat}}, strings.Replace(f.s.conversationName(), "conversations-", "pack-tests-", 1): map[string]any{"version": 1, "suites": map[string]any{"example": map[string]any{"cases": f.cases}}}}
	for name, doc := range documents {
		data, _ := json.Marshal(doc)
		if e := writePrivateData(store.root, name, data); e != nil {
			t.Fatal(e)
		}
	}
}
func (f *actionFixture) grant(t *testing.T) map[string]any {
	t.Helper()
	code, body := sendJSON(t, f.ts, http.MethodPost, "/api/assistant-approvals/chat-one/grant", map[string]any{"action": "pack-update", "revision": digestOf([]byte(f.before))})
	if code != 200 {
		t.Fatal(code, body)
	}
	return body["grant"].(map[string]any)
}
func (f *actionFixture) request(grant map[string]any) map[string]any {
	return map[string]any{"action": "pack-update", "id": grant["id"], "revision": digestOf([]byte(f.before)), "content": f.after}
}
func TestAssistantApprovalExplicitSingleUse(t *testing.T) {
	f := newActionFixture(t)
	code, _ := sendJSON(t, f.ts, http.MethodPost, "/api/assistant-approvals/chat-one/commit", f.request(map[string]any{}))
	if code != 409 {
		t.Fatal("saved without allowance", code)
	}
	grant := f.grant(t)
	code, status := sendJSON(t, f.ts, http.MethodGet, "/api/assistant-approvals/chat-one", nil)
	if code != 200 || status["mode"] != "auto" {
		t.Fatal(code, status)
	}
	// Concurrent commits race at the same host transaction; only one may write.
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, _ := sendJSON(t, f.ts, http.MethodPost, "/api/assistant-approvals/chat-one/commit", f.request(grant))
			codes <- code
		}()
	}
	wg.Wait()
	close(codes)
	counts := map[int]int{}
	for c := range codes {
		counts[c]++
	}
	if counts[200] != 1 || counts[409] != 1 {
		t.Fatal(counts)
	}
	data, _ := os.ReadFile(filepath.Join(f.s.projectDir, "pack.json"))
	if string(data) != f.after {
		t.Fatal(string(data))
	}
	_, status = sendJSON(t, f.ts, http.MethodGet, "/api/assistant-approvals/chat-one", nil)
	if status["mode"] != "manual" || status["grant"].(map[string]any)["state"] != "used" {
		t.Fatal(status)
	}
}
func TestAssistantApprovalFailsClosed(t *testing.T) {
	cases := []struct {
		name   string
		change func(*actionFixture, map[string]any)
	}{
		{"plan", func(f *actionFixture, _ map[string]any) { f.chat["interaction"] = "plan" }},
		{"running", func(f *actionFixture, _ map[string]any) { f.state["status"] = "running" }},
		{"questions", func(f *actionFixture, _ map[string]any) { f.state["questions"] = []any{map[string]any{"id": "policy"}} }},
		{"unknowns", func(f *actionFixture, _ map[string]any) { f.state["unknowns"] = []any{"policy needs review"} }},
		{"invalid", func(f *actionFixture, _ map[string]any) { f.check["valid"] = false }},
		{"digest", func(f *actionFixture, _ map[string]any) { f.check["documentDigest"] = "wrong" }},
		{"target", func(f *actionFixture, _ map[string]any) {
			f.chat["pack"] = map[string]any{"id": "other", "path": "pack.json"}
		}},
		{"tests moved", func(f *actionFixture, _ map[string]any) {
			f.cases[0].(map[string]any)["row"].(map[string]any)["facts"] = map[string]any{"zero": "0"}
		}},
		{"no assertions", func(f *actionFixture, _ map[string]any) { f.check["totalCases"] = 2 }},
		{"mismatch", func(f *actionFixture, _ map[string]any) { f.check["report"].(map[string]any)["status"] = "mismatch" }},
		{"different action", func(_ *actionFixture, r map[string]any) { r["action"] = "research" }},
		{"different content", func(_ *actionFixture, r map[string]any) { r["content"] = `{"version":"9"}` }},
		{"different id", func(_ *actionFixture, r map[string]any) { r["id"] = "invented" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newActionFixture(t)
			grant := f.grant(t)
			request := f.request(grant)
			tc.change(f, request)
			f.save(t)
			code, body := sendJSON(t, f.ts, http.MethodPost, "/api/assistant-approvals/chat-one/commit", request)
			if code != 409 {
				t.Fatal(code, body)
			}
			data, _ := os.ReadFile(filepath.Join(f.s.projectDir, "pack.json"))
			if string(data) != f.before {
				t.Fatal("refused action changed file")
			}
		})
	}
}
func TestAssistantApprovalRevocationExpiryAndFreshness(t *testing.T) {
	for _, kind := range []string{"revoke", "expiry", "plan", "file", "tests", "session"} {
		t.Run(kind, func(t *testing.T) {
			f := newActionFixture(t)
			g := f.grant(t)
			switch kind {
			case "revoke":
				code, _ := sendJSON(t, f.ts, http.MethodPost, "/api/assistant-approvals/chat-one/revoke", map[string]any{"id": g["id"]})
				if code != 200 {
					t.Fatal(code)
				}
			case "expiry":
				f.s.writes.Lock()
				for k, g := range f.s.actionGrants {
					g.ExpiresAt = time.Now().Add(-time.Second)
					f.s.actionGrants[k] = g
				}
				f.s.writes.Unlock()
			case "plan":
				f.chat["interaction"] = "plan"
				f.save(t)
			case "file":
				if e := os.WriteFile(filepath.Join(f.s.projectDir, "pack.json"), []byte(`{"version":"0.1.2"}`), 0600); e != nil {
					t.Fatal(e)
				}
			case "tests":
				f.cases = append(f.cases, map[string]any{"id": "new"})
				f.save(t)
			case "session":
				f.s.writes.Lock()
				for k, g := range f.s.actionGrants {
					delete(f.s.actionGrants, k)
					f.s.actionGrants["another-session\nchat-one"] = g
				}
				f.s.writes.Unlock()
			}
			code, body := sendJSON(t, f.ts, http.MethodGet, "/api/assistant-approvals/chat-one", nil)
			if code != 200 || body["mode"] != "manual" {
				t.Fatal(code, body)
			}
			code, body = sendJSON(t, f.ts, http.MethodPost, "/api/assistant-approvals/chat-one/commit", f.request(g))
			if code != 409 {
				t.Fatal(code, body)
			}
		})
	}
}
