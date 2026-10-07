package desk

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
)

func TestAssistantProfileSharedFixtures(t *testing.T) {
	data, err := os.ReadFile("../../web/src/config/fixtures/assistant-profiles.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name     string
		Value    json.RawMessage
		Accepted bool
	}
	if err = json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		t.Run(f.Name, func(t *testing.T) {
			_, err := decodeAssistantProfile(f.Value)
			if (err == nil) != f.Accepted {
				t.Fatalf("acceptance mismatch: %v", err)
			}
		})
	}
}
func TestAssistantProfileDeskIsolationAndRestoringInheritance(t *testing.T) {
	s, server, _ := assistantServer(t)
	a := createTestDesk(t, server, "Alpha")
	b := createTestDesk(t, server, "Beta")
	alpha, beta := s.desks[a.ID], s.desks[b.ID]
	machine := []byte(`{"deskConfigVersion":1,"assistant":{"engine":"codex","agent":{"provider":"openai","authMethod":"subscription","model":"machine","models":["machine"],"tools":[]}}}`)
	if err := os.WriteFile(s.deskConfigPath(), machine, 0600); err != nil {
		t.Fatal(err)
	}
	profilePath := filepath.Join(alpha.projectDir, assistantProfilePath)
	if err := os.WriteFile(profilePath, []byte(`{"profileVersion":1,"codex":{"inherit":false,"models":["alpha-model"],"model":"alpha-model"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if !alpha.agentModelAllowed("alpha-model") || alpha.agentModelAllowed("machine") || !beta.agentModelAllowed("machine") || beta.agentModelAllowed("alpha-model") {
		t.Fatal("model grants crossed desk boundaries")
	}
	// Exercise named desk routing with a model absent from the shared defaults.
	token, err := s.sessions.create("local-user", nil)
	if err != nil {
		t.Fatal(err)
	}
	alpha.codex = &agentStub{run: func(_ context.Context, _ codexbridge.Owner, r codexbridge.RunRequest, _ func(codexbridge.RunEvent) error, _ codexbridge.ToolHandler) error {
		if r.Model != "alpha-model" {
			t.Error("wrong model")
		}
		return nil
	}}
	ws := agentDeskSocket(t, server, token, "?desk="+a.ID)
	writeAgent(t, ws, map[string]any{"type": "start", "request": map[string]any{"model": "alpha-model", "prompt": "test", "instructions": "test", "tools": []any{}}})
	if event := readAgent(t, ws); event["type"] != "started" {
		t.Fatal(event)
	}
	if event := readAgent(t, ws); event["type"] != "end" || event["error"] != "" {
		t.Fatal(event)
	}
	if err := os.WriteFile(profilePath, []byte(inheritedAssistantProfile), 0600); err != nil {
		t.Fatal(err)
	}
	if !alpha.agentModelAllowed("machine") || alpha.agentModelAllowed("alpha-model") {
		t.Fatal("inheritance did not restore shared defaults")
	}
	saved, _ := os.ReadFile(s.deskConfigPath())
	if string(saved) != string(machine) {
		t.Fatal("shared settings changed")
	}
}
func TestAssistantProfileInvalidFileDoesNotFallBack(t *testing.T) {
	s, _, _ := agentServer(t, &agentStub{})
	path := filepath.Join(s.projectDir, assistantProfilePath)
	for _, data := range []string{`invalid`, `{"profileVersion":1,"codex":{"inherit":false,"models":[],"model":null}}`, string([]byte{0xff})} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if s.agentModelAllowed("model") {
			t.Fatal("invalid or disabled profile fell back to shared grants")
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if !s.agentModelAllowed("model") {
		t.Fatal("absent profile should inherit")
	}
}
func TestAssistantProfileAPISelectors(t *testing.T) {
	for _, tc := range []struct {
		kind, suffix, body string
		allowed            bool
	}{
		{"gemini", "v1beta/models/allowed:streamGenerateContent", "{}", true},
		{"gemini", "v1beta/models/other:streamGenerateContent", "{}", false},
		{"gemini", "v1beta/models/allowed:countTokens", "{}", true},
		{"openai-compatible", "chat/completions", `{"model":"allowed","messages":[]}`, true},
		{"anthropic", "v1/messages", `{"model":"other","messages":[]}`, false},
		{"openai-compatible", "responses", `{"model":42}`, false},
	} {
		if profileAPIModelAllowed(tc.kind, tc.suffix, []byte(tc.body), []string{"allowed"}) != tc.allowed {
			t.Fatal(tc)
		}
	}
}

func TestAssistantProfileAPIRelayStopsUnapprovedModel(t *testing.T) {
	upstream := newUpstream(t, nil)
	s, server, _ := relayDesk(t, "openai-compatible", upstream)
	path := filepath.Join(s.projectDir, assistantProfilePath)
	if err := os.WriteFile(path, []byte(`{"profileVersion":1,"api":{"inherit":false,"models":["local"],"model":"local"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, model := range []string{"a-model", "local"} {
		request := relayRequest(t, server, http.MethodPost, "chat/completions", strings.NewReader(`{"model":"`+model+`","messages":[]}`))
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		want := http.StatusForbidden
		if model == "local" {
			want = http.StatusOK
		}
		if response.StatusCode != want {
			t.Fatalf("%s: got %d", model, response.StatusCode)
		}
	}
	if len(upstream.arrivals()) != 1 || !strings.Contains(string(upstream.arrivals()[0].body), `"local"`) {
		t.Fatal("unapproved model reached upstream")
	}
}

func TestAssistantProfileSymlinkIsNotAnInheritanceRequest(t *testing.T) {
	s, _, _ := agentServer(t, &agentStub{})
	if err := os.Symlink("missing-profile.json", filepath.Join(s.projectDir, assistantProfilePath)); err != nil {
		t.Fatal(err)
	}
	if s.agentModelAllowed("model") {
		t.Fatal("dangling profile link inherited shared grants")
	}
}
