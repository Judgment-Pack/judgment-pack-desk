package desk

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
)

// This opt-in feasibility probe uses New and the public saved-chat HTTP routes.
// It is offline evidence, not a browser replay or a live comparison.
func TestReleaseReplayOfflineSavedChat(t *testing.T) {
	if os.Getenv("DESK_OFFLINE_RELEASE_REPLAY") != "1" {
		t.Skip("explicit offline release probe only")
	}
	peer, err := filepath.Abs("../../scripts/release-quality/offline-peer.py")
	assistantRunMust(t, err)
	for _, tc := range []struct{ id, mode, prompt string }{
		{"model-policy-refusal", "policy", "Explain briefly why the host must keep the declared policy boundary in force. Answer in one sentence without calling tools."},
		{"model-recorded-tool-result", "schema", "Read the schema with get_schema, then state what the recorded schema says in one sentence. Do not call any other tool."},
	} {
		t.Run(tc.id, func(t *testing.T) {
			root := t.TempDir()
			project, config := filepath.Join(root, "project"), filepath.Join(root, "config")
			assistantRunMust(t, os.Mkdir(project, 0700))
			assistantRunMust(t, os.Mkdir(config, 0700))
			makePeer := func(role string) string {
				bin := filepath.Join(root, role)
				// Fixed fixture arguments, never an installed native CLI.
				body := fmt.Sprintf("#!/bin/sh\nexec /usr/bin/python3 %q %q %q \"$@\"\n", peer, role, tc.mode)
				assistantRunMust(t, os.WriteFile(bin, []byte(body), 0700))
				return bin
			}
			sink := codexbridge.NewAccountingSink()
			s, err := New(Config{ProjectDir: project, DeskConfigDir: config, JpackBin: makePeer("runtime"), CodexBin: makePeer("native"), CodexAccounting: sink, Token: testToken, Port: 8791})
			assistantRunMust(t, err)
			t.Cleanup(func() { _ = s.Close() })
			token := policyTestSession(t, s)
			model := "gpt-6-sol"
			connection := aiConnection{ID: testAIAlpha, Name: "Offline fixture", Enabled: true, Assistant: AssistantSlotView{Engine: "codex", Thinking: "off", Agent: &AssistantAgentConfig{Provider: "openai", AuthMethod: "subscription", Model: &model, Models: []string{model}, Tools: []string{"get_schema"}, Effort: "medium", ChildModel: model, CriticModel: model}}}
			raw, err := encodeAIRegistry(aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{connection}})
			assistantRunMust(t, err)
			assistantRunMust(t, s.assistant.writeConfigNamed(aiConnectionsFile, raw, nil))
			writeProjectFile(t, s.projectDir, assistantProfilePath, `{"profileVersion":1,"api":{"inherit":true},"codex":{"inherit":true},"critic":"off"}`)
			chat := map[string]any{"id": "offline-chat", "title": "Offline replay", "pinned": false, "archived": false, "composer": "", "model": model, "aiConnection": testAIAlpha, "mode": "draft", "view": "chat", "interaction": "work", "adversarialReview": false, "reasoning": map[string]string{"model": model, "effort": "medium"}, "updatedAt": "2026-10-24T00:00:00Z", "serverRequest": map[string]string{"requestId": "offline-request", "admissionId": "offline-admission"}, "checkpoint": map[string]any{"sources": []any{}, "state": map[string]any{"phase": "conversation", "status": "stopped", "turns": []any{map[string]any{"id": "offline-request", "role": "user", "kind": "message", "text": tc.prompt, "at": "2026-10-24T00:00:00Z"}}, "candidates": []any{}, "cases": []any{}, "events": []any{}, "unknowns": []any{}}}}
			w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", "absent", map[string]any{"version": 1, "chats": []any{chat}})
			if w.Code != 200 {
				t.Fatalf("save: %d %s", w.Code, w.Body.String())
			}
			saved := policyWorkspaceRead(t, s, token, "/api/conversations")
			_, rows, err := assistantChatRows(saved.Content)
			assistantRunMust(t, err)
			start := assistantRunStart{Purpose: "chat", OwnerID: "offline-chat", RequestID: "offline-request", AdmissionID: "offline-admission", Revision: digestOf(rows[0])}
			initial := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
			end := time.Now().Add(60 * time.Second)
			var run assistantRunView
			for time.Now().Before(end) {
				run = assistantProtocolView(t, policyCall(s, token, "GET", "/api/assistant-runs/"+initial.ID, nil), 200)
				if assistantRunTerminal(run.State) {
					break
				}
				time.Sleep(250 * time.Millisecond)
			}
			if run.State != "complete" {
				checkpoint := "missing"
				if run.Checkpoint != nil {
					checkpoint = run.Checkpoint.Status
				}
				t.Fatalf("offline normal saved-chat incomplete: state=%s revision=%d checkpoint=%s operations=%d", run.State, run.Revision, checkpoint, len(run.Operations))
			}
			reads, models := 0, 0
			for _, op := range run.Operations {
				if op.Kind == "model" {
					models++
				}
				if op.Kind == "runtime" && strings.Contains(string(op.Intent), `"get_schema"`) {
					reads++
				}
			}
			want := 0
			if tc.mode == "schema" {
				want = 1
			}
			if reads != want || models != 1 {
				t.Fatalf("unexpected counts models=%d schema=%d", models, reads)
			}
			select {
			case report := <-sink.Reports():
				if report.Incomplete || !report.ApplicationSuccess || !report.OwnedClientReaped || report.Invocation != 1 {
					t.Fatalf("offline accounting incomplete: %+v", report)
				}
			default:
				t.Fatal("missing offline accounting")
			}
			if sink.Dropped() != 0 {
				t.Fatal("dropped offline accounting")
			}
		})
	}
}
