package desk

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func changeMemory(t *testing.T, s *Server, c memoryChange) memoryReply {
	t.Helper()
	var out memoryReply
	e := s.withMemory(func(root *os.Root, r *memoryReply) error {
		if e := s.applyMemory(root, r, c); e != nil {
			return e
		}
		b, _ := json.Marshal(r.Content)
		if e := writePrivateData(root, s.memoryName(), b); e != nil {
			return e
		}
		r.SHA256 = digestOf(b)
		out = *r
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	return out
}
func seedMemoryMessage(t *testing.T, s *Server, text string) *memorySource {
	t.Helper()
	source := &memorySource{ChatID: "chat-one", TurnID: "turn-one"}
	doc := map[string]any{"version": 1, "chats": []any{map[string]any{"id": source.ChatID, "checkpoint": map[string]any{"state": map[string]any{"turns": []any{map[string]any{"id": source.TurnID, "role": "user", "statement": text, "at": time.Now().UTC().Format(time.RFC3339Nano)}}}, "sources": []any{}}}}}
	data, _ := json.Marshal(doc)
	store, e := s.openChatData()
	if e != nil {
		t.Fatal(e)
	}
	defer store.root.Close()
	if e = writePrivateData(store.root, s.conversationName(), data); e != nil {
		t.Fatal(e)
	}
	return source
}
func TestProjectMemoryRecallSettingsAndScope(t *testing.T) {
	s, _, _ := assistantServer(t)
	r := changeMemory(t, s, memoryChange{Action: "save", Key: "outreach-format", Text: "Outreach explanations should label missing evidence."})
	if got, e := s.memoryBackground("outreach"); e != nil || got != "" {
		t.Fatal("recall enabled by save", e, got)
	}
	changeMemory(t, s, memoryChange{Action: "settings", Settings: &memorySettings{Use: true}})
	host, err := s.assistantPromptHost(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	got, err := host.MemoryFor(t.Context(), "outreach")
	if err != nil || !strings.Contains(fmt.Sprint(got), r.Content.Entries[0].Text) {
		t.Fatal("canonical explicit-query recall", got, err)
	}
	if got, e := s.memoryBackground("unrelated astronomy"); e != nil || got != "" {
		t.Fatal("unrelated memory injected", e, got)
	}
	other, _, _ := assistantServer(t)
	if got, e := other.memoryBackground("outreach"); e != nil || got != "" {
		t.Fatal("cross-project recall", e, got)
	}
}
func TestProjectMemorySuggestionsApprovalAndForget(t *testing.T) {
	s, _, _ := assistantServer(t)
	source := seedMemoryMessage(t, s, "I prefer concise outreach explanations.")
	c := memoryChange{Action: "suggest", Key: "outreach-style", Text: "Concise outreach explanations.", Source: source}
	checkDenied := func() {
		t.Helper()
		if e := s.withMemory(func(root *os.Root, r *memoryReply) error { return s.applyMemory(root, r, c) }); e == nil {
			t.Fatal("unexpected suggestion accepted")
		}
	}
	checkDenied()
	changeMemory(t, s, memoryChange{Action: "settings", Settings: &memorySettings{Use: true, Suggest: true}})
	proposed := changeMemory(t, s, c)
	if proposed.Content.Entries[0].State != "pending" {
		t.Fatal(proposed)
	}
	if got, _ := s.memoryBackground("outreach"); got != "" {
		t.Fatal("unreviewed memory used")
	}
	id := proposed.Content.Entries[0].ID
	changeMemory(t, s, memoryChange{Action: "approve", ID: id})
	if got, _ := s.memoryBackground("outreach"); got == "" {
		t.Fatal("approved memory not recalled")
	}
	changeMemory(t, s, memoryChange{Action: "forget", ID: id})
	checkDenied()
	c.Key = "paraphrased-topic"
	c.Text = "Short outreach answers."
	checkDenied()
	store, e := s.openChatData()
	if e != nil {
		t.Fatal(e)
	}
	defer store.root.Close()
	bytes, e := readPrivateData(store.root, s.memoryName(), memoryLimit)
	if e != nil || strings.Contains(string(bytes), "Concise outreach explanations.") {
		t.Fatal("forgotten text still stored", e, string(bytes))
	}
}
func TestProjectMemoryExplicitRememberClearAndStaleSource(t *testing.T) {
	s, _, _ := assistantServer(t)
	source := seedMemoryMessage(t, s, "Remember that outreach explanations should be concise.")
	r := changeMemory(t, s, memoryChange{Action: "suggest", Key: "outreach-style", Text: "outreach explanations should be concise.", Source: source})
	if r.Content.Entries[0].State != "active" {
		t.Fatal(r)
	}
	path := filepath.Join(s.projectDir, "policy.md")
	os.WriteFile(path, []byte("version one"), 0600)
	r = changeMemory(t, s, memoryChange{Action: "save", Key: "policy-pointer", Text: "Consult policy.md for outreach rules.", Dependencies: []memoryDependency{{Path: "policy.md", SHA256: digestOf([]byte("version one"))}}})
	changeMemory(t, s, memoryChange{Action: "settings", Settings: &memorySettings{Use: true, Suggest: true}})
	os.WriteFile(path, []byte("version two"), 0600)
	got, _ := s.memoryBackground("policy outreach")
	if strings.Contains(got, "Consult policy.md") {
		t.Fatal("stale dependency recalled")
	}
	changeMemory(t, s, memoryChange{Action: "clear"})
	c := memoryChange{Action: "suggest", Key: "another-key", Text: "A different paraphrase", Source: source}
	if e := s.withMemory(func(root *os.Root, r *memoryReply) error { return s.applyMemory(root, r, c) }); e == nil {
		t.Fatal("old source resurrected after clear")
	}
	c = memoryChange{Action: "save", Key: "credential", Text: "api_key: super-secret"}
	if e := s.withMemory(func(root *os.Root, r *memoryReply) error { return s.applyMemory(root, r, c) }); e == nil {
		t.Fatal("credential accepted")
	}
}
func TestProjectMemoryHTTPGuardsAndCAS(t *testing.T) {
	s, ts, _ := assistantServer(t)
	call := func(method, path, digest, body string, auth bool) *http.Response {
		t.Helper()
		r, _ := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
		if auth {
			bearer(r)
		}
		r.Header.Set("If-Match", digest)
		resp, e := ts.Client().Do(r)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { resp.Body.Close() })
		return resp
	}
	if r := call("GET", "/api/ai-memory", "", "", false); r.StatusCode == 200 {
		t.Fatal("unauthenticated read")
	}
	if r := call("POST", "/api/ai-memory", "absent", `{"action":"save","key":"outreach","text":"Use evidence."}`, true); r.StatusCode != 200 {
		t.Fatal(r.Status)
	}
	if r := call("POST", "/api/ai-memory", "absent", `{"action":"clear"}`, true); r.StatusCode != 409 {
		t.Fatal("stale destructive write", r.Status)
	}
	if r := call("POST", "/api/ai-memory/suggest", "", `{"action":"save","key":"bypass","text":"bypass"}`, true); r.StatusCode == 200 {
		t.Fatal("AI endpoint accepted direct save")
	}
	info, e := os.Stat(filepath.Join(s.defaultChatDataPath(), s.memoryName()))
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal(e, info)
	}
	// The actual handler refuses a symlink in private memory storage.
	os.Remove(filepath.Join(s.defaultChatDataPath(), s.memoryName()))
	os.Symlink(filepath.Join(t.TempDir(), "outside"), filepath.Join(s.defaultChatDataPath(), s.memoryName()))
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/ai-memory", nil)
	bearer(req)
	s.handleAIMemory(recorder, req)
	if recorder.Code == 200 {
		t.Fatal("symlink accepted")
	}
}

func TestProjectMemoryStorageLifecycle(t *testing.T) {
	t.Run("backup restore", func(t *testing.T) {
		original, ots, _ := assistantServer(t)
		changeMemory(t, original, memoryChange{Action: "settings", Settings: &memorySettings{Use: true, Suggest: true}})
		changeMemory(t, original, memoryChange{Action: "save", Key: "outreach-style", Text: "Concise outreach notes."})
		backup := backupBytes(t, ots)
		cfg := original.cfg
		cfg.DeskConfigDir = t.TempDir()
		restored, rts := startDesk(t, cfg)
		defer restored.Close()
		defer rts.Close()
		empty := storageGet(t, rts)
		if code, body := restoreBackup(t, rts, moveChatData{Path: filepath.Join(t.TempDir(), "restored"), Revision: empty.Revision}, backup); code != 200 {
			t.Fatal(code, body)
		}
		if recalled, e := restored.memoryBackground("outreach"); e != nil || !strings.Contains(recalled, "Concise outreach notes.") {
			t.Fatal(e, recalled)
		}
	})
	t.Run("project move with memory but no chat", func(t *testing.T) {
		s, ts, _ := assistantServer(t)
		changeMemory(t, s, memoryChange{Action: "settings", Settings: &memorySettings{Use: true}})
		changeMemory(t, s, memoryChange{Action: "save", Key: "outreach-style", Text: "Use short outreach notes."})
		old := s.memoryName()
		destination := filepath.Join(t.TempDir(), "moved")
		request := movePreview(t, ts, destination)
		if code, data := moveRequest(t, ts, "move", request); code != 200 {
			t.Fatal(code, string(data))
		}
		again, _, _ := startedAt(t, s, destination)
		if recalled, e := again.memoryBackground("outreach"); e != nil || !strings.Contains(recalled, "Use short outreach notes.") {
			t.Fatal(e, recalled)
		}
		if e := again.withMemory(func(_ *os.Root, r *memoryReply) error {
			if r.Name != old {
				t.Fatal("lost original namespace", r.Name, old)
			}
			return nil
		}); e != nil {
			t.Fatal(e)
		}
	})
}
