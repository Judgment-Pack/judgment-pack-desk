package desk

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
)

func assistantContextFixtureBinding(t *testing.T, s *Server, token string) assistantRunBinding {
	t.Helper()
	r, _ := http.NewRequest("POST", "/api/assistant-runs", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	b, _, err := s.assistantRunIdentity(r)
	assistantRunMust(t, err)
	b.Project = s.assistantRuns().project
	return b
}
func assistantContextAssertIdle(t *testing.T, s *Server) {
	t.Helper()
	h := s.assistantRunService()
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.active) != 0 || len(h.contextOwners) != 0 {
		t.Fatal("completed production owner did not positively settle")
	}
	c := h.contexts.registry
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.reserved != 0 || len(c.runs) != 0 {
		t.Fatal("completed production context ownership leaked")
	}
}

// Real production construction for a supported closed authoring workflow. No
// executor, checkpoint, dispatch, settlement or registry is substituted. The
// Runtime and native programs are the existing handwritten offline peers.
// This does not qualify the separately blocked full saved-chat registry.
func TestAssistantContextActualAuthoringTwoTurns(t *testing.T) {
	for _, engine := range []string{"api", "native"} {
		t.Run(engine, func(t *testing.T) {
			var requests atomic.Int32
			u := newUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
				switch (requests.Add(1) - 1) % 3 {
				case 0:
					assistantPolicySSE(w, "get_schema", map[string]any{}, "")
				case 1:
					assistantPolicySSE(w, "validate", map[string]any{}, "")
				default:
					assistantBriefSSE(w, assistantAuthoringProposalFixture(json.RawMessage(reviewPack)), "stop")
				}
			})
			s, token := assistantAuthoringFixture(t, u.server.URL)
			// Extend only this test-owned peer. The descriptor is the actual
			// describe_runtime response consumed by production construction;
			// neither the cache identity nor a Chat is injected into execution.
			peer, err := os.ReadFile(s.cfg.JpackBin)
			assistantRunMust(t, err)
			// Operation identities belong to one run. This fixture retains both
			// explicit turns, so completed runs cannot participate in the peer's
			// exact-one-dispatched-operation check for the current Runtime.
			receiptAnchor := "for run in ledger['runs'] for op"
			if strings.Count(string(peer), receiptAnchor) != 1 {
				t.Fatal("authoring Runtime peer receipt anchor changed")
			}
			peer = []byte(strings.Replace(string(peer), receiptAnchor, "for run in ledger['runs'] if run['state']=='running' for op", 1))
			anchor := " elif method=='prompts/list':"
			if strings.Count(string(peer), anchor) != 1 {
				t.Fatal("authoring Runtime peer prompt anchor changed")
			}
			peer = []byte(strings.Replace(string(peer), anchor, ` elif method=='tools/call' and name=='describe_runtime':
  descriptor={'tool':{'name':'Runtime','version':'0.39.0'},'schema':'authoring-context-fixture'}
  result={'content':[{'type':'text','text':json.dumps(descriptor)}],'structuredContent':descriptor}
 elif method=='tools/call' and name=='get_schema': result={'content':[{'type':'text','text':'immutable-schema'}]}
 elif method=='tools/call' and name=='validate':
  with open('context-workspace.txt') as f: revision=f.read()
  result={'content':[{'type':'text','text':revision}]}
`+anchor, 1))
			assistantRunMust(t, os.WriteFile(s.cfg.JpackBin, peer, 0700))
			trace := ""
			if engine == "native" {
				registry, err := s.readAIRegistry()
				assistantRunMust(t, err)
				model := "shared-model"
				registry.Connections[0].Assistant.Engine = "codex"
				registry.Connections[0].Assistant.Endpoint = nil
				registry.Connections[0].Assistant.Agent = &AssistantAgentConfig{Provider: "openai", AuthMethod: "subscription", Model: &model, Models: []string{model}, Effort: "medium", Tools: []string{"get_schema", "validate"}}
				raw, err := encodeAIRegistry(registry.aiRegistry)
				assistantRunMust(t, err)
				assistantRunMust(t, s.assistant.writeConfigNamed(aiConnectionsFile, raw, nil))
				writeProjectFile(t, s.projectDir, assistantProfilePath, `{"profileVersion":1,"codex":{"inherit":true},"critic":"off"}`)
				dir := t.TempDir()
				binary := filepath.Join(dir, "native-peer")
				trace = filepath.Join(dir, "trace.jsonl")
				peer := strings.Replace(assistantExecutionCodexPeer, "text='Fixture answer.'", "text="+fmt.Sprintf("%q", assistantAuthoringProposalFixture(json.RawMessage(reviewPack))), 1)
				// Each actual native invocation asks for both reads. The cache
				// must suppress the second turn's physical get_schema call.
				anchor := "  if mode=='duplicate': tool(offered('get_schema'),{},rpc='rpc-two')"
				if strings.Count(peer, anchor) != 1 {
					t.Fatal("native peer callback anchor changed")
				}
				peer = strings.Replace(peer, anchor, "  if mode=='context' and q.get('id')=='rpc': tool(offered('validate'),{},call='mutable-call',rpc='mutable-rpc')\n  elif mode=='context': finish()\n"+strings.Replace(anchor, "  if ", "  elif ", 1), 1)
				// Record only closed protocol milestones, never requests or answers.
				// A failing run can then distinguish bridge preflight from generation.
				anchor = " if method=='initialize':"
				if strings.Count(peer, anchor) != 1 {
					t.Fatal("native peer initialization anchor changed")
				}
				peer = strings.Replace(peer, anchor, " if method in ['initialize','account/read','model/list','thread/start','turn/start']: log({'type':'context-stage','stage':method})\n"+anchor, 1)
				t.Cleanup(func() {
					if !t.Failed() {
						return
					}
					counts := map[string]int{}
					for _, row := range assistantCodexLog(t, trace) {
						if string(row["type"]) != `"context-stage"` {
							continue
						}
						var stage string
						assistantRunMust(t, json.Unmarshal(row["stage"], &stage))
						counts[stage]++
					}
					t.Logf("native peer milestones: initialize=%d account_read=%d model_list=%d thread_start=%d turn_start=%d", counts["initialize"], counts["account/read"], counts["model/list"], counts["thread/start"], counts["turn/start"])
				})
				assistantRunMust(t, os.WriteFile(filepath.Join(dir, "peer.py"), []byte(peer), 0600))
				data, err := s.openChatData()
				assistantRunMust(t, err)
				ledger := filepath.Join(data.root.Name(), s.assistantRuns().name)
				assistantRunMust(t, data.root.Close())
				wrapper := fmt.Sprintf("#!/bin/sh\nexec python3 '%s' context '%s' '%s'\n", filepath.Join(dir, "peer.py"), trace, ledger)
				assistantRunMust(t, os.WriteFile(binary, []byte(wrapper), 0700))
				manager, err := codexbridge.NewManager(codexbridge.Options{Binary: binary, ProfileDir: filepath.Join(dir, "account"), ProjectDir: s.projectDir})
				assistantRunMust(t, err)
				t.Cleanup(manager.Close)
				s.codex = manager
				s.aiAccounts.values = map[string]providerAccountManager{testAIAlpha: manager}
			}
			var chat *runcontext.ChatContext
			var selector *assistantRunContextSelector
			var firstRun string
			for turn := 1; turn <= 2; turn++ {
				revision := fmt.Sprintf("workspace-v%d", turn)
				writeProjectFile(t, s.projectDir, "context-workspace.txt", revision)
				a := assistantAuthoringRequestFixture(fmt.Sprintf("context-turn-%d", turn), "describe-pack")
				if engine == "native" {
					a.Effort = "medium"
				}
				captured := assistantAuthoringCaptureFixture(t, s, token, a)
				original, _ := json.Marshal(captured.Start)
				start := assistantContextStartFixture(t, s, token, *captured.Start)
				if selector == nil {
					selector = start.Context
				} else if *selector != *start.Context {
					t.Fatal("same mounted pane changed selector")
				}
				held := assistantContextTestRetain(t, s.assistantRunService().contexts.registry, assistantContextFixtureBinding(t, s, token), *start.Context, start.Purpose, start.OwnerID)
				if chat == nil {
					chat = held.Chat()
				} else if held.Chat() != chat {
					t.Fatal("second actual turn replaced Chat")
				}
				held.Release()
				view := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
				if view.Checkpoint == nil {
					t.Fatal("actual admission omitted prepared checkpoint")
				}
				if view.Checkpoint.Context.Scope != chat.Scope() {
					t.Fatalf("actual Prepare changed scope: prepared=%q retained=%q disposed=%v", view.Checkpoint.Context.Scope, chat.Scope(), chat.IsDisposed())
				}
				t.Logf("actual authoring settlement: turn=%d", turn)
				// Every canonical event rewrites the one retained per-project ledger.
				// The second turn's run commits behind the first turn's complete
				// journal (about 1.8 MB here), so it settles more slowly than one run.
				complete := assistantProtocolWaitWithin(t, s, token, view.ID, "complete", 60*time.Second)
				if complete.Checkpoint == nil {
					t.Fatal("actual execution omitted completed checkpoint")
				}
				if complete.Checkpoint.Context.Scope != chat.Scope() {
					t.Fatalf("actual Execute changed scope: prepared=%q complete=%q retained=%q disposed=%v entries=%d requests=%d", view.Checkpoint.Context.Scope, complete.Checkpoint.Context.Scope, chat.Scope(), chat.IsDisposed(), len(chat.Entries()), requests.Load())
				}
				if chat.IsDisposed() {
					t.Fatal("actual execution disposed mounted authoring Chat")
				}
				reads := map[string]int{}
				for _, row := range assistantCodexLog(t, filepath.Join(s.projectDir, "execution-fixture.jsonl")) {
					if string(row["method"]) != `"tools/call"` {
						continue
					}
					var params struct{ Name string }
					assistantRunMust(t, json.Unmarshal(row["params"], &params))
					reads[params.Name]++
				}
				if reads["describe_runtime"] != turn || reads["get_schema"] != 1 || reads["validate"] != turn {
					t.Fatalf("actual cross-turn physical reads: turn=%d descriptor=%d immutable=%d mutable=%d", turn, reads["describe_runtime"], reads["get_schema"], reads["validate"])
				}
				seen := map[string]bool{}
				for _, op := range complete.Checkpoint.Operations {
					if op.Status != "complete" || op.Result == nil {
						continue
					}
					if op.Result.IsError != nil && *op.Result.IsError || len(op.Result.Content) != 1 {
						continue
					}
					part := op.Result.Content[0].Option1
					if part == nil || part.Type == nil || *part.Type != "text" || part.Text == nil {
						continue
					}
					if op.Name == "get_schema" && *part.Text == "immutable-schema" || op.Name == "validate" && *part.Text == revision {
						seen[op.Name] = true
					}
				}
				if !seen["get_schema"] || !seen["validate"] {
					t.Fatalf("actual checkpoint omitted exact read results: turn=%d immutable=%v freshMutable=%v", turn, seen["get_schema"], seen["validate"])
				}
				assistantContextAssertIdle(t, s)
				if engine == "native" && turn == 1 {
					assistantContextMeasureSettledLedgerDecode(t, s)
				}
				after, _ := json.Marshal(captured.Start)
				if string(after) != string(original) || captured.Start.Context != nil {
					t.Fatal("start mutated immutable capture reference")
				}
				if turn == 1 {
					firstRun = view.ID
				} else if firstRun == view.ID {
					t.Fatal("two explicit captures shared run identity")
				}
			}
			if engine == "api" && requests.Load() != 6 {
				t.Fatal("two explicit turns changed provider request count", requests.Load())
			}
			if engine == "native" {
				generations := 0
				for _, row := range assistantCodexLog(t, trace) {
					if string(row["type"]) == `"generation"` {
						generations++
					}
				}
				if generations != 2 {
					t.Fatal("native generation count", generations)
				}
			}
			if w := policyCall(s, token, "POST", "/api/assistant-contexts/documents/release", assistantContextDocumentRelease{selector.Document}); w.Code != 200 {
				t.Fatal(w.Code)
			}
			if !chat.IsDisposed() {
				t.Fatal("settled authoring pane did not release")
			}
		})
	}
}

// Required production qualification, deliberately kept separate from the first
// bounded ownership gate until the released graph241/factory243 dependency is
// satisfied. This uses the full normal saved-chat host registry in both engines.
func TestAssistantContextFullProductionTwoTurns(t *testing.T) {
	for _, engine := range []string{"api", "native"} {
		t.Run(engine, func(t *testing.T) {
			u := newUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
				assistantPolicySSE(w, "", nil, "Two-turn context fixture.")
			})
			mode := ""
			if engine == "native" {
				mode = "read"
			}
			s, token, start, _ := assistantProductionFixture(t, u.server.URL, "work", mode)
			held := assistantContextTestRetain(t, s.assistantRunService().contexts.registry, assistantContextFixtureBinding(t, s, token), *start.Context, "chat", start.OwnerID)
			chat := held.Chat()
			held.Release()
			for turn := 1; turn <= 2; turn++ {
				if turn == 2 {
					saved := policyWorkspaceRead(t, s, token, "/api/conversations")
					fields, rows, err := assistantChatRows(saved.Content)
					assistantRunMust(t, err)
					row, err := assistantTestObject(rows[0])
					assistantRunMust(t, err)
					cp, err := assistantTestObject(row["checkpoint"])
					assistantRunMust(t, err)
					state, err := assistantTestObject(cp["state"])
					assistantRunMust(t, err)
					turns, err := assistantChatArray(state["turns"])
					assistantRunMust(t, err)
					turns = append(turns, assistantChatJSON(map[string]any{"id": "message-two", "role": "user", "kind": "message", "text": "Continue exact review", "at": "2026-10-24T00:01:00Z"}))
					state["turns"] = assistantChatEncodeArray(turns)
					cp["state"] = assistantChatEncodeObject(state)
					row["checkpoint"] = assistantChatEncodeObject(cp)
					row["serverRequest"] = assistantChatJSON(map[string]string{"requestId": "message-two", "admissionId": "admission-two"})
					rows[0] = assistantChatEncodeObject(row)
					fields["chats"] = assistantChatEncodeArray(rows)
					if w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, json.RawMessage(assistantChatEncodeObject(fields))); w.Code != 200 {
						t.Fatal(w.Code, w.Body.String())
					}
					start.RequestID = "message-two"
					start.AdmissionID = "admission-two"
					start.Revision = digestOf(rows[0])
				}
				view := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
				complete := assistantProtocolWait(t, s, token, view.ID, "complete")
				if complete.Checkpoint == nil || complete.Checkpoint.Context.Scope != chat.Scope() || chat.IsDisposed() {
					t.Fatal("normal production turn changed Chat scope")
				}
				assistantContextAssertIdle(t, s)
			}
			if w := policyCall(s, token, "POST", "/api/assistant-contexts/documents/release", assistantContextDocumentRelease{start.Context.Document}); w.Code != 200 {
				t.Fatal(w.Code)
			}
			if !chat.IsDisposed() {
				t.Fatal("completed production Chat not disposed after release")
			}
		})
	}
}
