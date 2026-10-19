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

// The harness places its execution state in a user text: a second part of the
// person's message on the first request, and a user message of its own after
// the history (trailing-user) on later ones; under system-tail the newest user
// turn holds only tool results. Each body below follows the shape the contract
// vector budget/state-placement.json gives for that wire, and recall reads the
// person's own text in each.
func TestMemoryRecallQueryDropsTheExecutionStateOnEachWire(t *testing.T) {
	const person = "THE PERSON'S MESSAGE\nRecall the outreach plan for the northern region."
	state := executionStateHeader + "\nRequest 2 of 40. Plan: none."
	toolResult := `{"content":[{"type":"text","text":"schema text the model read"}]}`
	cases := map[string]string{
		"openai-compatible first request": `{"messages":[{"role":"system","content":"host"},{"role":"user","content":[{"type":"text","text":` + jsonText(person) + `},{"type":"text","text":` + jsonText(state) + `}]}]}`,
		"openai-compatible trailing-user": `{"messages":[{"role":"system","content":"host"},{"role":"user","content":[{"type":"text","text":` + jsonText(person) + `}]},{"role":"assistant","content":null,"tool_calls":[{"id":"call-1","type":"function","function":{"name":"get_schema","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call-1","content":` + jsonText(toolResult) + `},{"role":"user","content":` + jsonText(state) + `}]}`,
		"openai-compatible system-tail":   `{"messages":[{"role":"system","content":` + jsonText("host\n"+state) + `},{"role":"user","content":[{"type":"text","text":` + jsonText(person) + `}]},{"role":"assistant","content":null,"tool_calls":[{"id":"call-1","type":"function","function":{"name":"get_schema","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call-1","content":` + jsonText(toolResult) + `}]}`,
		"anthropic first request":         `{"system":"host","messages":[{"role":"user","content":[{"type":"text","text":` + jsonText(person) + `},{"type":"text","text":` + jsonText(state) + `}]}]}`,
		"anthropic trailing-user":         `{"system":"host","messages":[{"role":"user","content":[{"type":"text","text":` + jsonText(person) + `}]},{"role":"assistant","content":[{"type":"tool_use","id":"call-1","name":"get_schema","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-1","content":` + jsonText(toolResult) + `},{"type":"text","text":` + jsonText(state) + `}]}]}`,
		"anthropic system-tail":           `{"system":[{"type":"text","text":"host"},{"type":"text","text":` + jsonText(state) + `}],"messages":[{"role":"user","content":[{"type":"text","text":` + jsonText(person) + `}]},{"role":"assistant","content":[{"type":"tool_use","id":"call-1","name":"get_schema","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-1","content":` + jsonText(toolResult) + `}]}]}`,
		"gemini first request":            `{"systemInstruction":{"parts":[{"text":"host"}]},"contents":[{"role":"user","parts":[{"text":` + jsonText(person) + `},{"text":` + jsonText(state) + `}]}]}`,
		"gemini trailing-user":            `{"systemInstruction":{"parts":[{"text":"host"}]},"contents":[{"role":"user","parts":[{"text":` + jsonText(person) + `}]},{"role":"model","parts":[{"functionCall":{"name":"get_schema","args":{}}}]},{"role":"user","parts":[{"functionResponse":{"name":"get_schema","response":{"content":"schema text the model read"}}}]},{"role":"user","parts":[{"text":` + jsonText(state) + `}]}]}`,
		"gemini system-tail":              `{"systemInstruction":{"parts":[{"text":"host"},{"text":` + jsonText(state) + `}]},"contents":[{"role":"user","parts":[{"text":` + jsonText(person) + `}]},{"role":"model","parts":[{"functionCall":{"name":"get_schema","args":{}}}]},{"role":"user","parts":[{"functionResponse":{"name":"get_schema","response":{"content":"schema text the model read"}}}]}]}`,
	}
	// The harness's own user texts, as the package's loop.ts sends them (written
	// here by hand from @judgment-pack/harness v0.1.0): a synthesis request under
	// trailing-user ends the history with FINISH_RESPONSE_INSTRUCTIONS, and a
	// reformat request, under either placement, with the answer it could not
	// read and the reformat request; the state is a second part of that last
	// user message. Recall reads the person's message, not these.
	finish := "The previous model response ended without a complete answer. Complete the current response using only the supplied request and completed results. Tools are unavailable. Do not restart discovery, repeat actions, or claim an unsaved change or an unperformed check succeeded. Keep the answer concise. If a complete supported proposal is not possible, explain the verified findings and the exact remaining work. Return a complete answer, not a continuation of a partial JSON block."
	reformat := "The previous answer carried a proposal that could not be read. Return the same proposal, unchanged in content, as exactly one fenced ```json block whose root object has a single \"proposal\" member shaped as your instructions describe. Valid JSON only: no comments, no trailing commas, no second proposal block. Tools are unavailable. The problem: the fenced proposal block is not valid JSON."
	unread := "Here is the pack.\n```json\n{\"proposal\": {\"document\": \n```"
	for _, placement := range []string{"trailing-user", "system-tail"} {
		system := `"host"`
		anthropicSystem, geminiSystem := `"host"`, `{"parts":[{"text":"host"}]}`
		if placement == "system-tail" {
			// The request before carried the state in the system text; this one ends with a user message, so the state is its second part.
			system, anthropicSystem, geminiSystem = `"host"`, `[{"type":"text","text":"host"}]`, `{"parts":[{"text":"host"}]}`
		}
		cases["openai-compatible reformat "+placement] = `{"messages":[{"role":"system","content":` + system + `},{"role":"user","content":[{"type":"text","text":` + jsonText(person) + `}]},{"role":"assistant","content":null,"tool_calls":[{"id":"call-1","type":"function","function":{"name":"get_schema","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call-1","content":` + jsonText(toolResult) + `},{"role":"assistant","content":` + jsonText(unread) + `},{"role":"user","content":[{"type":"text","text":` + jsonText(reformat) + `},{"type":"text","text":` + jsonText(state) + `}]}]}`
		cases["anthropic reformat "+placement] = `{"system":` + anthropicSystem + `,"messages":[{"role":"user","content":[{"type":"text","text":` + jsonText(person) + `}]},{"role":"assistant","content":[{"type":"tool_use","id":"call-1","name":"get_schema","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-1","content":` + jsonText(toolResult) + `}]},{"role":"assistant","content":[{"type":"text","text":` + jsonText(unread) + `}]},{"role":"user","content":[{"type":"text","text":` + jsonText(reformat) + `},{"type":"text","text":` + jsonText(state) + `}]}]}`
		cases["gemini reformat "+placement] = `{"systemInstruction":` + geminiSystem + `,"contents":[{"role":"user","parts":[{"text":` + jsonText(person) + `}]},{"role":"model","parts":[{"functionCall":{"name":"get_schema","args":{}}}]},{"role":"user","parts":[{"functionResponse":{"name":"get_schema","response":{"content":"schema text the model read"}}}]},{"role":"model","parts":[{"text":` + jsonText(unread) + `}]},{"role":"user","parts":[{"text":` + jsonText(reformat) + `},{"text":` + jsonText(state) + `}]}]}`
	}
	cases["openai-compatible synthesis trailing-user"] = `{"messages":[{"role":"system","content":"host"},{"role":"user","content":[{"type":"text","text":` + jsonText(person) + `}]},{"role":"assistant","content":null,"tool_calls":[{"id":"call-1","type":"function","function":{"name":"get_schema","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call-1","content":` + jsonText(toolResult) + `},{"role":"user","content":[{"type":"text","text":` + jsonText(finish) + `},{"type":"text","text":` + jsonText(state) + `}]}]}`
	// The Anthropic provider joins the tool result and the finishing request into one user turn.
	cases["anthropic synthesis trailing-user"] = `{"system":"host","messages":[{"role":"user","content":[{"type":"text","text":` + jsonText(person) + `}]},{"role":"assistant","content":[{"type":"tool_use","id":"call-1","name":"get_schema","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-1","content":` + jsonText(toolResult) + `},{"type":"text","text":` + jsonText(finish) + `},{"type":"text","text":` + jsonText(state) + `}]}]}`
	cases["gemini synthesis trailing-user"] = `{"systemInstruction":{"parts":[{"text":"host"}]},"contents":[{"role":"user","parts":[{"text":` + jsonText(person) + `}]},{"role":"model","parts":[{"functionCall":{"name":"get_schema","args":{}}}]},{"role":"user","parts":[{"functionResponse":{"name":"get_schema","response":{"content":"schema text the model read"}}}]},{"role":"user","parts":[{"text":` + jsonText(finish) + `},{"text":` + jsonText(state) + `}]}]}`
	want := memoryQueryTail("\n" + person)
	for name, raw := range cases {
		var body map[string]any
		if err := json.Unmarshal([]byte(raw), &body); err != nil {
			t.Fatal(name, err)
		}
		got := memoryRequestQuery(body)
		if got != want {
			t.Fatalf("%s: recall query %q, want %q", name, got, want)
		}
		if strings.Contains(got, "EXECUTION STATE") || strings.Contains(got, "schema text the model read") {
			t.Fatalf("%s: recall query reads the state or a tool result: %q", name, got)
		}
		if strings.Contains(got, "The previous model response ended") || strings.Contains(got, "could not be read") || strings.Contains(got, "Here is the pack") {
			t.Fatalf("%s: recall query reads the harness's own request: %q", name, got)
		}
	}
	// Where no user text carries the person's-message marker (a pane run), the
	// newest remaining user text is read, as before.
	var pane map[string]any
	if err := json.Unmarshal([]byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"Draft a pack for the outreach plan."}]},{"role":"assistant","content":"partial"},{"role":"user","content":[{"type":"text","text":"Finish it."},{"type":"text","text":`+jsonText(state)+`}]}]}`), &pane); err != nil {
		t.Fatal(err)
	}
	if got := memoryRequestQuery(pane); got != memoryQueryTail("\nFinish it.") {
		t.Fatalf("pane fallback: %q", got)
	}
	// And a turn that holds only tool results is still skipped there.
	if err := json.Unmarshal([]byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"Draft a pack for the outreach plan."}]},{"role":"assistant","content":[{"type":"tool_use","id":"call-1","name":"get_schema","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-1","content":"x"}]}]}`), &pane); err != nil {
		t.Fatal(err)
	}
	if got := memoryRequestQuery(pane); got != memoryQueryTail("\nDraft a pack for the outreach plan.") {
		t.Fatalf("pane fallback past a tool-only turn: %q", got)
	}

	// The codex wire: the engine appends the current state (the chat's draft
	// snapshot) and its guidance after the person's message in one prompt, and
	// the page sends the person's message beside it. The query reads that alone.
	snapshot := "CHAT DRAFT SNAPSHOT (may predate the saved workspace; use the current workspace or current object reads for edits, and preserve its identity)\n{\"id\":\"northern-outreach\",\"rules\":[{\"id\":\"private-threshold\"}]}"
	guidance := "EXECUTION GUIDANCE\n{\"requests\":{\"used\":2,\"limit\":40},\"retainedOperations\":[]}"
	message := "Recall the outreach plan for the northern region."
	codex := codexbridge.RunRequest{Prompt: "Conversation instructions\n\nTRANSCRIPT SO FAR\n\n" + personMessageMarker + message + "\n\n" + snapshot + "\n\n" + guidance, PersonMessage: message}
	got := agentRecallQuery(codex)
	if got != memoryQueryTail(personMessageMarker+message) {
		t.Fatalf("codex recall query %q", got)
	}
	for _, leak := range []string{"CHAT DRAFT SNAPSHOT", "private-threshold", "EXECUTION GUIDANCE", "retainedOperations"} {
		if strings.Contains(got, leak) {
			t.Fatalf("codex recall query reads %q: %q", leak, got)
		}
	}
	// A request without the person's message (server-owned research) is read from its prompt, as before.
	if got := agentRecallQuery(codexbridge.RunRequest{Prompt: "outreach"}); got != memoryQueryTail("outreach") {
		t.Fatalf("prompt fallback: %q", got)
	}
	// A body with the state alone, or with tool results alone, recalls nothing.
	for name, raw := range map[string]string{
		"state only":        `{"messages":[{"role":"user","content":` + jsonText(state) + `}]}`,
		"tool results only": `{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-1","content":"x"}]}]}`,
	} {
		var body map[string]any
		if err := json.Unmarshal([]byte(raw), &body); err != nil {
			t.Fatal(name, err)
		}
		if got := memoryRequestQuery(body); got != "" {
			t.Fatalf("%s: recall query %q, want none", name, got)
		}
	}
}

func jsonText(text string) string {
	raw, _ := json.Marshal(text)
	return string(raw)
}
