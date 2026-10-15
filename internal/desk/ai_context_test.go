package desk

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/aicontext"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
)

func TestAIContextProjectBriefFreshAndScoped(t *testing.T) {
	s, _, _ := assistantServer(t)
	profile := []byte(`{"profileVersion":2,"inherit":true,"context":{"version":1,"projectBrief":"assistant-context.md","models":{"small":{"contextWindowTokens":16384,"outputTokens":4096}}}}`)
	if err := os.WriteFile(filepath.Join(s.projectDir, assistantProfilePath), profile, 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.projectDir, "assistant-context.md")
	if err := os.WriteFile(path, []byte("Current project brief"), 0600); err != nil {
		t.Fatal(err)
	}
	req := codexbridge.RunRequest{Model: "small", Prompt: "current request", Instructions: "host policy"}
	if err := s.prepareAgentContext(&req); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(req.Instructions, "Current project brief") || !strings.Contains(req.Instructions, "not approval") {
		t.Fatal(req.Instructions)
	}
	if err := os.WriteFile(path, []byte("New brief revision"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"openai-compatible", "anthropic", "gemini"} {
		raw := []byte(`{"model":"small","messages":[{"role":"user","content":"task"}],"system":"host"}`)
		if kind == "gemini" {
			raw = []byte(`{"contents":[{"role":"user","parts":[{"text":"task"}]}]}`)
		}
		got, err := s.prepareModelContext(kind, "v1beta/models/small:generateContent", raw)
		if err != nil {
			t.Fatal(kind, err)
		}
		if !strings.Contains(string(got), "New brief revision") || strings.Contains(string(got), "Current project brief") {
			t.Fatal(kind, string(got))
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.aiContext("small"); err == nil {
		t.Fatal("deleted brief silently reused")
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "external"), path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.aiContext("small"); err == nil {
		t.Fatal("symlinked brief accepted")
	}
}
func TestAIContextWireGuardIncludesToolsAndOutputReserve(t *testing.T) {
	s, _, _ := assistantServer(t)
	raw := []byte(`{"model":"fixture","messages":[{"role":"user","content":"unchanged"}]}`)
	got, err := s.prepareModelContext("openai-compatible", "chat/completions", raw)
	if err != nil || string(got) != string(raw) {
		t.Fatalf("changed accepted request %s: %v", got, err)
	}
	body, _ := json.Marshal(map[string]any{"model": "fixture", "messages": []any{}, "tools": strings.Repeat("x", aicontext.Defaults().InputBytes())})
	if _, err = s.prepareModelContext("openai-compatible", "chat/completions", body); err == nil {
		t.Fatal("unbounded schemas bypassed guard")
	}
	if _, err = s.prepareModelContext("anthropic", "v1/messages", []byte(`{"model":"fixture","messages":[],"max_tokens":40000}`)); err == nil {
		t.Fatal("output reserve ignored")
	}
	req := codexbridge.RunRequest{Model: "fixture", Prompt: strings.Repeat("é", 100000)}
	if err = s.prepareAgentContext(&req); err == nil {
		t.Fatal("UTF-8 prompt bypassed guard")
	}
}
func TestResearchContextRetrievesExactRetainedPagesWithoutSourceAcquisition(t *testing.T) {
	text := strings.Repeat("Canadian company 😀 ", 5000)
	run := inputResearchRun{Sources: []inputResearchSource{{ID: "allowed", Kind: "page", Text: text}, {ID: "sibling", Kind: "page", Text: "other task"}}}
	scoped := run
	scoped.Sources = run.Sources[:1]
	offset := 0
	assembled := ""
	for {
		args, _ := json.Marshal(map[string]any{"sourceId": "allowed", "sha256": digestOf([]byte(text)), "offset": offset})
		got, err := researchContextRead(scoped, args)
		if err != nil {
			t.Fatal(err)
		}
		part := got["text"].(string)
		if !utf8.ValidString(part) || len(part) > aicontext.Defaults().ReadBytes {
			t.Fatal("invalid page")
		}
		assembled += part
		if got["nextOffset"] == nil {
			break
		}
		offset = got["nextOffset"].(int)
	}
	if assembled != text {
		t.Fatal("source changed during paging")
	}
	for _, args := range []string{`{"sourceId":"sibling","sha256":"wrong","offset":0}`, `{"sourceId":"allowed","sha256":"stale","offset":0}`} {
		if _, err := researchContextRead(scoped, []byte(args)); err == nil {
			t.Fatal("out-of-scope/stale source accepted")
		}
	}
	raw, _ := json.Marshal(map[string]any{"tool": "read_context", "arguments": map[string]any{"sourceId": "allowed", "sha256": digestOf([]byte(text)), "offset": 6000}})
	run.Attempts = []inputResearchAttempt{{TaskID: "task", Kind: "model", State: "completed", Result: raw}}
	if researchContextSelection(run, "") != nil {
		t.Fatal("parent inherited child read")
	}
	// A successful local read is represented in the next prompt, with no search/page operation.
	raw, _ = json.Marshal(map[string]any{"tool": "read_context", "arguments": map[string]any{"sourceId": "allowed", "sha256": digestOf([]byte(text)), "offset": 0}})
	run.Attempts[0].Result = raw
	if researchContextSelection(run, "task") == nil {
		t.Fatal("requested context omitted")
	}
}
func TestResearchContextCompactionPreservesAuthorityAndOriginals(t *testing.T) {
	body := map[string]any{"approvedLimits": map[string]any{"records": 10}, "retainedSources": []any{map[string]any{"id": "source", "text": strings.Repeat("quotation 😀 ", 20000)}}, "retainedSourceIndex": []any{map[string]any{"sourceId": "source", "sha256": "exact"}}, "recordResults": []any{map[string]any{"status": "saved", "id": "record-1"}}}
	raw, _ := json.Marshal(body)
	p := aicontext.Defaults()
	prompt, err := fitResearchContext(string(raw), p)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompt) >= len(raw) || !strings.Contains(prompt, "record-1") || !strings.Contains(prompt, `"records":10`) || !strings.Contains(prompt, "read_context") {
		t.Fatal("lost required context")
	}
	unchanged, _ := json.Marshal(body)
	if string(raw) != string(unchanged) {
		t.Fatal("changed original journal")
	}
}
