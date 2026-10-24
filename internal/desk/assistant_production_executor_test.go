package desk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
)

// New installs the actual production executor. This fixture supplies only a
// saved connection, current person/source, published Runtime and native peer;
// it never substitutes an executor or constructs a checkpoint.
func assistantProductionFixture(t *testing.T, endpoint, phase, nativeMode string) (*Server, string, assistantRunStart, string) {
	t.Helper()
	runtime := os.Getenv("DESK_DIALECT_MAPPING_RUNTIME")
	if runtime == "" {
		t.Skip("published Runtime fixture required")
	}
	s, token := policyTestServer(t)
	if _, ok := s.assistantRunExecutor.(*assistantProductionExecutor); !ok {
		t.Fatal("normal Server lacks the closed production executor")
	}
	s.cfg.JpackBin = runtime
	writeProjectFile(t, s.projectDir, "jpack.json", `{"configVersion":"5","packs":{"one":{"path":"one.pack.json"}}}`)
	writeProjectFile(t, s.projectDir, "one.pack.json", reviewPack)
	writeProjectFile(t, s.projectDir, assistantProfilePath, `{"profileVersion":1,"api":{"inherit":true},"codex":{"inherit":true},"critic":"off","projectInstructions":"PRODUCTION-HOST-INSTRUCTIONS"}`)
	c := aiTestConnection(testAIAlpha, "Production fixture", endpoint)
	c.Assistant.Endpoint.Tools = []string{"get_schema", "validate", "experimental_evaluate"}
	trace := ""
	if nativeMode != "" {
		model := "shared-model"
		c.Assistant.Engine, c.Assistant.Endpoint = "codex", nil
		c.Assistant.Agent = &AssistantAgentConfig{Model: &model, Provider: "openai", AuthMethod: "subscription", Models: []string{model, "child-model"}, Tools: []string{"get_schema", "validate", "experimental_evaluate"}, Effort: "medium"}
		dir := t.TempDir()
		binary := filepath.Join(dir, "native-peer")
		trace = filepath.Join(dir, "trace.jsonl")
		peer := filepath.Join(dir, "peer.py")
		assistantRunMust(t, os.WriteFile(peer, []byte(assistantExecutionCodexPeer), 0600))
		data, err := s.openChatData()
		assistantRunMust(t, err)
		ledger := filepath.Join(data.root.Name(), s.assistantRuns().name)
		assistantRunMust(t, data.root.Close())
		wrapper := fmt.Sprintf("#!/bin/sh\nexec python3 '%s' '%s' '%s' '%s'\n", peer, nativeMode, trace, ledger)
		assistantRunMust(t, os.WriteFile(binary, []byte(wrapper), 0700))
		manager, err := codexbridge.NewManager(codexbridge.Options{Binary: binary, ProfileDir: filepath.Join(dir, "account"), ProjectDir: s.projectDir})
		assistantRunMust(t, err)
		t.Cleanup(manager.Close)
		s.codex = manager
		s.aiAccounts.values = map[string]providerAccountManager{testAIAlpha: manager}
	} else {
		origin, _ := endpointOrigin(endpoint)
		assistantRunMust(t, s.assistant.storeKeyNamed(connectionKeyName(&c), storedKey{present: true, key: "production-fixture", origin: origin, kind: c.Assistant.Endpoint.Kind}))
	}
	raw, err := encodeAIRegistry(aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{c}})
	assistantRunMust(t, err)
	assistantRunMust(t, s.assistant.writeConfigNamed(aiConnectionsFile, raw, nil))
	chat := map[string]any{"id": "chat-one", "title": "Production chat", "pinned": false, "archived": false, "composer": "", "model": "shared-model", "aiConnection": testAIAlpha, "mode": "draft", "view": "chat", "interaction": phase, "adversarialReview": false, "updatedAt": "2026-10-24T00:00:00Z", "serverRequest": map[string]string{"requestId": "message-one", "admissionId": "admission-one"}, "checkpoint": map[string]any{"sources": []any{}, "state": map[string]any{"phase": "conversation", "status": "stopped", "turns": []any{map[string]any{"id": "message-one", "role": "user", "kind": "message", "text": "Review exact input", "at": "2026-10-24T00:00:00Z"}}, "candidates": []any{}, "cases": []any{}, "events": []any{}, "unknowns": []any{}}}}
	if nativeMode != "" {
		chat["reasoning"] = map[string]any{"model": "shared-model", "effort": nil}
	}
	w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", "absent", map[string]any{"version": 1, "chats": []any{chat}})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	saved := policyWorkspaceRead(t, s, token, "/api/conversations")
	_, rows, err := assistantChatRows(saved.Content)
	assistantRunMust(t, err)
	return s, token, assistantRunStart{Purpose: "chat", OwnerID: "chat-one", RequestID: "message-one", AdmissionID: "admission-one", Revision: digestOf(rows[0])}, trace
}

func TestAssistantProductionClosedFactory(t *testing.T) {
	factory := &assistantProductionExecutor{}
	for _, purpose := range []string{"", "unknown", "author-function", strings.Repeat("x", 10000)} {
		if _, err := assistantProductionPurpose(purpose); err == nil {
			t.Fatal("unknown production purpose selected an executor", purpose)
		}
		if _, err := factory.Prepare(t.Context(), assistantRunSource{Start: assistantRunStart{Purpose: purpose}}); err == nil {
			t.Fatal("unknown production purpose admitted", purpose)
		}
		if err := factory.Execute(context.Background(), &assistantRunExecution{source: assistantRunSource{Start: assistantRunStart{Purpose: purpose}}}); err == nil {
			t.Fatal("unknown production purpose executed", purpose)
		}
	}
	for _, purpose := range []string{"chat", "brief", "author-pack", "describe-pack", "author-graph"} {
		executor, err := assistantProductionPurpose(purpose)
		if err != nil {
			t.Fatal("closed owner missing", purpose, err)
		}
		want := "*desk.assistantAuthoringExecutor"
		if purpose == "chat" {
			want = "*desk.assistantCreationExecutor"
		}
		if purpose == "brief" {
			want = "*desk.assistantBriefExecutor"
		}
		if fmt.Sprintf("%T", executor) != want {
			t.Fatal("wrong closed owner", purpose, executor)
		}
	}
}

// Preparation is independently tested without provider/Runtime capabilities;
// actual production activation is proved by TestAssistantProductionActualChat.
func TestAssistantProductionPreparationSettings(t *testing.T) {
	s, _, e := assistantExecutionFixture(t, "https://example.invalid", "work")
	before := len(assistantAuthoringLedger(t, s).Runs)
	on, off := true, false
	for _, tc := range []struct {
		name, critic, project, want string
		review                      *bool
	}{
		{"legacy-on", "", "off", "on", &on}, {"legacy-off", "", "on", "off", &off},
		{"canonical-precedence", "off", "on", "off", &on}, {"project", "", "on", "on", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := &assistantExecutionInput{Request: runcontext.CurrentRequest{Message: "Prepare only", Input: "Prepare only"}, Critic: tc.critic, CriticProject: tc.project, Review: tc.review}
			cp, err := prepareAssistantContextSession(t.Context(), e.source, input, "chat", "")
			assistantRunMust(t, err)
			if string(cp.Policy["critic.default"].Value) != fmt.Sprintf("%q", tc.want) {
				t.Fatal("prepared critic lost saved controls", cp.Policy["critic.default"])
			}
			if len(assistantAuthoringLedger(t, s).Runs) != before {
				t.Fatal("preparation dispatched work")
			}
		})
	}
}

func TestAssistantProductionActualChat(t *testing.T) {
	for _, native := range []bool{false, true} {
		for _, phase := range []string{"work", "plan"} {
			t.Run(fmt.Sprintf("%s-%t", phase, native), func(t *testing.T) {
				var requests atomic.Int32
				upstream := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
					n := requests.Add(1)
					if n == 1 {
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = fmt.Fprint(w, "data: {\"id\":\"schema\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"schema-call\",\"type\":\"function\",\"function\":{\"name\":\"get_schema\",\"arguments\":\"{}\"}}]},\"finish_reason\":null}]}\n\ndata: {\"id\":\"schema\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
					} else {
						assistantBriefSSE(w, "Production fixture answer.", "stop")
					}
				})
				mode := ""
				if native {
					mode = "read"
				}
				s, token, start, trace := assistantProductionFixture(t, upstream.server.URL, phase, mode)
				initial := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
				run := assistantProtocolWait(t, s, token, initial.ID, "complete")
				if run.Checkpoint == nil || run.Checkpoint.Phase != phase || run.ChatProjection == nil || !run.ChatProjection.Acknowledged {
					t.Fatal("production run lost phase or durable projection", run)
				}
				if string(run.Checkpoint.Policy["budget.memoryProposalsPerRun"].Value) != "0" {
					t.Fatal("production Suggest snapshot not admitted")
				}
				reads := 0
				for _, op := range run.Operations {
					if op.Kind == "runtime" && strings.Contains(string(op.Intent), "get_schema") {
						reads++
					}
				}
				if reads != 1 {
					t.Fatal("actual production Runtime read missing", reads)
				}
				if native {
					if run.Checkpoint.Used != 0 || run.Checkpoint.Limit != 1 || run.Checkpoint.RequestCap != nil {
						t.Fatal("native accounting changed", run.Checkpoint)
					}
					raw, err := os.ReadFile(trace)
					assistantRunMust(t, err)
					if strings.Count(string(raw), `"type": "generation"`) != 1 || !strings.Contains(string(raw), "PRODUCTION-HOST-INSTRUCTIONS") {
						t.Fatal("native production binding or exact generation missing", string(raw))
					}
				} else if requests.Load() != 2 {
					t.Fatal("production API generation count changed", requests.Load())
				}
				before := len(assistantAuthoringLedger(t, s).Runs)
				again := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 200)
				if again.ID != run.ID || len(assistantAuthoringLedger(t, s).Runs) != before {
					t.Fatal("admission replay dispatched again")
				}
				stream := policyCall(s, token, "GET", "/api/assistant-runs/"+run.ID+"/events", nil)
				if stream.Code != 200 {
					t.Fatal("production event read", stream.Code)
				}
				if out := os.Getenv("JPS_CREATION_OUTPUTS"); out != "" {
					chat := policyWorkspaceRead(t, s, token, "/api/conversations")
					raw, _ := json.Marshal(map[string]any{"run": run, "chat": json.RawMessage(chat.Content)})
					assistantRunMust(t, os.WriteFile(filepath.Join(out, fmt.Sprintf("production-chat-%s-%t.json", phase, native)), raw, 0600))
					assistantRunMust(t, os.WriteFile(filepath.Join(out, fmt.Sprintf("production-chat-%s-%t.sse", phase, native)), stream.Body.Bytes(), 0600))
				}
			})
		}
	}
}
