package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
)

func TestAIWireGuardNeverReadsPromptSourcesOrChangesInput(t *testing.T) {
	s := promptFixture(t, "")
	promptProfile(t, s, map[string]any{"profileVersion": 1, "context": map[string]any{"version": 1, "projectBrief": "missing-brief.md"}})
	// The guard must not need any prompt source, even an unreadable memory store.
	store, err := s.openChatData()
	assistantRunMust(t, err)
	defer store.root.Close()
	assistantRunMust(t, writePrivateData(store.root, s.memoryName(), []byte("not a memory document")))
	for _, row := range []struct{ kind, suffix, body string }{
		{"openai-compatible", "chat/completions", " {\"model\":\"fixture\", \"messages\":[{\"role\":\"user\",\"content\":\"THE PERSON'S MESSAGE\\noutreach\"}]}\n"},
		{"anthropic", "v1/messages", `{"model":"fixture","max_tokens":1024,"system":"ADAPTER SYSTEM","messages":[{"role":"user","content":"outreach"}]}`},
		{"gemini", "v1beta/models/fixture:generateContent", `{"systemInstruction":{"parts":[{"text":"ADAPTER SYSTEM"}]},"contents":[{"role":"user","parts":[{"text":"outreach"}]}]}`},
		{"openai-compatible", "chat/completions", "opaque probe bytes"},
	} {
		t.Run(row.kind+row.suffix+row.body[:1], func(t *testing.T) {
			raw := []byte(row.body)
			before := bytes.Clone(raw)
			if err := s.checkModelWireContext(row.kind, row.suffix, raw); err != nil || !bytes.Equal(raw, before) {
				t.Fatalf("wire guard read prompt sources or changed input: %v", err)
			}
		})
	}
	request := codexbridge.RunRequest{Model: "fixture", Prompt: "outreach", Instructions: "ADAPTER INSTRUCTIONS"}
	before := request
	if err := s.checkAgentWireContext(request); err != nil || !reflect.DeepEqual(request, before) {
		t.Fatal("native wire guard read prompt sources or changed input", err)
	}
	if _, err := os.Stat(filepath.Join(s.projectDir, "missing-brief.md")); !os.IsNotExist(err) {
		t.Fatal("guard created brief source", err)
	}
}

func TestAIWireGuardBudgetsEveryWireWithoutClipping(t *testing.T) {
	s := promptFixture(t, "")
	promptProfile(t, s, map[string]any{"profileVersion": 1, "context": map[string]any{"version": 1, "models": map[string]any{"small": map[string]int{"contextWindowTokens": 16384, "outputTokens": 4096}}}})
	for _, row := range []struct{ kind, suffix, output string }{
		{"openai-compatible", "chat/completions", `"max_completion_tokens":9000`},
		{"anthropic", "v1/messages", `"max_tokens":9000`},
		{"gemini", "v1beta/models/small:generateContent", `"generationConfig":{"maxOutputTokens":9000}`},
	} {
		t.Run(row.kind, func(t *testing.T) {
			// Gemini resolves the model from the URL; another body model must not
			// choose a larger budget. Every wire counts schemas and UTF-8 bytes.
			model := "small"
			if row.kind == "gemini" {
				model = "foreign-body-model"
			}
			for _, payload := range []string{row.output, `"tools":` + jsonText(strings.Repeat("é", 20000))} {
				raw := []byte(`{"model":"` + model + `",` + payload + `}`)
				before := bytes.Clone(raw)
				if err := s.checkModelWireContext(row.kind, row.suffix, raw); err == nil || !bytes.Equal(raw, before) {
					t.Fatal("wire budget bypassed or refused input clipped", payload[:min(len(payload), 30)])
				}
			}
		})
	}
	for _, request := range []codexbridge.RunRequest{
		{Model: "small", Prompt: strings.Repeat("é", 20000)},
		{Model: "small", Instructions: strings.Repeat("é", 20000)},
		{Model: "small", Tools: []codexbridge.Tool{{Name: "fixture", Description: strings.Repeat("é", 20000), InputSchema: json.RawMessage(`{"type":"object"}`)}}},
	} {
		if err := s.checkAgentWireContext(request); err == nil {
			t.Fatal("native wire budget ignored prompt, instructions or schemas")
		}
	}
	// Configured larger contexts keep their output reserve rather than inheriting
	// the default model's smaller limit.
	promptProfile(t, s, map[string]any{"profileVersion": 1, "context": map[string]any{"version": 1, "models": map[string]any{"large": map[string]int{"contextWindowTokens": 128000, "outputTokens": 40000}}}})
	for _, row := range []struct{ kind, suffix, body string }{
		{"openai-compatible", "chat/completions", `{"model":"large","max_completion_tokens":40000}`},
		{"anthropic", "v1/messages", `{"model":"large","max_tokens":40000}`},
		{"gemini", "v1beta/models/large:generateContent", `{"generationConfig":{"maxOutputTokens":40000}}`},
	} {
		if err := s.checkModelWireContext(row.kind, row.suffix, []byte(row.body)); err != nil {
			t.Fatal("configured large model refused", row.kind, err)
		}
	}

}

func jsonText(text string) string {
	raw, _ := json.Marshal(text)
	return string(raw)
}

// Exercise each retained legacy caller as well as the guard itself. Missing
// configured prompt sources cannot affect byte-identical adapter input.
func TestAIWireGuardLegacyDispatchOwnsNoPromptLayers(t *testing.T) {
	for _, row := range []struct{ kind, suffix, body string }{
		{"openai-compatible", "chat/completions", ` {"model":"small","messages":[{"role":"user","content":"outreach"}]} `},
		{"anthropic", "v1/messages", `{"model":"small","max_tokens":1024,"messages":[{"role":"user","content":"outreach"}]}`},
		{"gemini", "v1beta/models/small:generateContent", `{"contents":[{"role":"user","parts":[{"text":"outreach"}]}]}`},
	} {
		t.Run(row.kind, func(t *testing.T) {
			u := newUpstream(t, nil)
			s, ts, _ := relayDesk(t, row.kind, u)
			promptProfile(t, s, map[string]any{"profileVersion": 1, "context": map[string]any{"version": 1, "projectBrief": "missing.md", "models": map[string]any{"small": map[string]int{"contextWindowTokens": 16384, "outputTokens": 4096}}}})
			response, body := relayDo(t, ts, http.MethodPost, row.suffix, strings.NewReader(row.body), nil)
			if response.StatusCode != 200 || !bytes.Equal(u.only(t).body, []byte(row.body)) {
				t.Fatal("legacy relay changed input or read missing brief", response.StatusCode, body)
			}
			oversized := strings.TrimSuffix(strings.TrimSpace(row.body), "}") + `,"tools":` + jsonText(strings.Repeat("é", 20000)) + `}`
			response, body = relayDo(t, ts, http.MethodPost, row.suffix, strings.NewReader(oversized), nil)
			if response.StatusCode != 413 || len(u.arrivals()) != 1 {
				t.Fatal("legacy relay bypassed wire guard or sent clipped input", response.StatusCode, body)
			}
		})
	}
	t.Run("native", func(t *testing.T) {
		var calls atomic.Int32
		stub := &agentStub{run: func(_ context.Context, _ codexbridge.Owner, request codexbridge.RunRequest, _ func(codexbridge.RunEvent) error, _ codexbridge.ToolHandler) error {
			calls.Add(1)
			if request.Prompt != "outreach" || request.Instructions != "ADAPTER-INSTRUCTIONS" {
				t.Error("legacy native transport appended prompt layers")
			}
			return nil
		}}
		s, ts, token := agentServer(t, stub)
		promptProfile(t, s, map[string]any{"profileVersion": 1, "context": map[string]any{"version": 1, "projectBrief": "missing.md", "models": map[string]any{"model": map[string]int{"contextWindowTokens": 16384, "outputTokens": 4096}}}})
		for _, prompt := range []string{"outreach", strings.Repeat("é", 20000)} {
			ws := agentSocket(t, ts, token)
			writeAgent(t, ws, map[string]any{"type": "start", "request": map[string]any{"model": "model", "prompt": prompt, "instructions": "ADAPTER-INSTRUCTIONS", "tools": []any{}}})
			if event := readAgent(t, ws); event["type"] != "started" {
				t.Fatal("native fixture did not start", event)
			}
			if event := readAgent(t, ws); event["type"] != "end" || (prompt == "outreach" && event["error"] != "") || (prompt != "outreach" && event["error"] != "context-limit") {
				t.Fatal("native wire guard did not preserve/refuse exact input", event)
			}
		}
		if calls.Load() != 1 {
			t.Fatal("native guard dispatched oversized input", calls.Load())
		}
	})
}
