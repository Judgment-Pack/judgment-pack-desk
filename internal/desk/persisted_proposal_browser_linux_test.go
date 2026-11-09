package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// This complete source-authored minimal pack is the existing UI fixture's
// policy with stable literals. The only proposed change is its version.
const persistedProposalOriginal = `{"specVersion":"0.2.0-draft","id":"https://example.invalid/judgment-packs/minimal","version":"0.1.0","title":"A minimal pack","decision":{"intent":"Decide the one thing this pack decides.","question":"Does this request proceed?"},"outcomes":[{"id":"proceed","label":"Proceed"},{"id":"hold","label":"Hold"}],"rules":[{"id":"always","description":"Proceed.","when":{"op":"literal","value":true},"outcome":"proceed","onUnknown":"escalate"}]}` + "\n"

func TestPersistedProposalOfflineBrowser(t *testing.T) {
	if os.Getenv("DESK_PERSISTED_PROPOSAL_REPLAY") != "1" {
		t.Skip("explicit finite offline proposal journey")
	}
	out, browser := os.Getenv("DESK_PERSISTED_PROPOSAL_OUTPUT"), os.Getenv("DESK_REPLAY_BROWSER_BIN")
	if !filepath.IsAbs(out) || !filepath.IsAbs(browser) {
		t.Fatal("absolute owned output and browser required")
	}
	assistantRunMust(t, os.Mkdir(out, 0700))
	driver, _ := filepath.Abs("../../scripts/release-quality/persisted-proposal-browser.mjs")
	reader, _ := filepath.Abs("../../scripts/release-quality/persisted-proposal-report.mjs")
	peer, _ := filepath.Abs("../../scripts/release-quality/persisted-proposal-peer.py")
	launcher, _ := filepath.Abs("persisted_proposal_browser_linux_test.go")
	assets, _ := filepath.Abs("../../web/dist")
	exe, err := os.Executable()
	assistantRunMust(t, err)
	identities := map[string]string{"applicationExecutable": replayHash(t, exe), "launcher": replayHash(t, launcher), "driver": replayHash(t, driver), "reader": replayHash(t, reader), "peer": replayHash(t, peer), "browser": replayHash(t, browser), "assets": replayTree(t, assets, nil)}
	var sourceRows [][2]string
	for _, path := range []string{"internal", "web/src"} {
		base, _ := filepath.Abs("../../" + path)
		replayTree(t, base, &sourceRows)
	}
	encoded, _ := json.Marshal(sourceRows)
	assistantRunMust(t, os.WriteFile(filepath.Join(out, "source-inventory.json"), encoded, 0600))
	identities["sourceInventory"] = replayDigest(encoded)
	candidate := strings.Replace(persistedProposalOriginal, `"version":"0.1.0"`, `"version":"0.1.1"`, 1)
	started := time.Now()
	for _, mode := range []string{"api-save", "native-apply-save", "native-dirty-refusal"} {
		if time.Since(started) >= 180*time.Second {
			t.Fatal("aggregate deadline")
		}
		if !t.Run(mode, func(t *testing.T) {
			root := filepath.Join(out, mode)
			assistantRunMust(t, os.Mkdir(root, 0700))
			project, config := filepath.Join(root, "project"), filepath.Join(root, "config")
			assistantRunMust(t, os.Mkdir(project, 0700))
			assistantRunMust(t, os.Mkdir(config, 0700))
			trace := filepath.Join(root, "fixture-lifecycle.jsonl")
			assistantRunMust(t, os.WriteFile(trace, nil, 0600))
			proposalPath := filepath.Join(root, "candidate.fixture.json")
			assistantRunMust(t, os.WriteFile(proposalPath, []byte(candidate), 0600))
			binary := func(role string) string {
				p := filepath.Join(root, role)
				body := fmt.Sprintf("#!/bin/sh\nexec /usr/bin/python3 %q %q %q %q %q \"$@\"\n", peer, role, project, trace, proposalPath)
				assistantRunMust(t, os.WriteFile(p, []byte(body), 0700))
				return p
			}
			var apiCalls atomic.Int32
			apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
				if err != nil {
					t.Error("finite API read")
					w.WriteHeader(400)
					return
				}
				var req struct {
					Model string `json:"model"`
					Tools []struct {
						Function struct {
							Name string `json:"name"`
						} `json:"function"`
					} `json:"tools"`
				}
				if json.Unmarshal(raw, &req) != nil || req.Model != "gpt-6-sol" {
					t.Error("finite API model request refused")
					w.WriteHeader(400)
					return
				}
				n := apiCalls.Add(1)
				if n == 1 {
					found := 0
					for _, tool := range req.Tools {
						if tool.Function.Name == assistantSavedPackTool {
							found++
						}
					}
					if found != 1 {
						t.Error("saved proposal tool not offered")
						w.WriteHeader(400)
						return
					}
					assistantPolicySSE(w, assistantSavedPackTool, map[string]string{"document": candidate}, "")
				} else if n == 2 {
					assistantPolicySSE(w, "", nil, "Review the checked proposal.")
				} else {
					t.Error("finite API extra invocation")
					w.WriteHeader(400)
				}
			}))
			t.Cleanup(apiServer.Close)
			token, err := NewToken()
			assistantRunMust(t, err)
			s, server := startDesk(t, Config{ProjectDir: project, DeskConfigDir: config, Static: os.DirFS(assets), JpackBin: binary("runtime"), CodexBin: binary("native"), Token: token})
			t.Cleanup(func() { _ = s.Close(); server.Close() })
			model := "gpt-6-sol"
			c := aiConnection{ID: testAIAlpha, Name: "Offline proposal fixture", Enabled: true, Assistant: AssistantSlotView{Engine: "vercel", Thinking: "off", Endpoint: &AssistantEndpointView{URL: apiServer.URL, Kind: "openai-compatible", Models: []string{model}, Model: &model, Tools: []string{"get_schema"}}}}
			if mode != "api-save" {
				c.Assistant = AssistantSlotView{Engine: "codex", Thinking: "off", Agent: &AssistantAgentConfig{Provider: "openai", AuthMethod: "subscription", Model: &model, Models: []string{model}, Tools: []string{"get_schema"}, Effort: "medium", ChildModel: model, CriticModel: model}}
			}
			registry, err := encodeAIRegistry(aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{c}})
			assistantRunMust(t, err)
			_, err = decodeAIRegistry(registry)
			assistantRunMust(t, err)
			assistantRunMust(t, s.assistant.writeConfigNamed(aiConnectionsFile, registry, nil))
			if mode == "api-save" {
				origin, ok := endpointOrigin(apiServer.URL)
				if !ok {
					t.Fatal("finite API endpoint origin invalid")
				}
				assistantRunMust(t, s.assistant.storeKeyNamed(connectionKeyName(&c), storedKey{present: true, key: "offline-fixture-key", origin: origin, kind: "openai-compatible"}))
			}
			profile := `{"profileVersion":1,"api":{"inherit":true},"codex":{"inherit":true},"critic":"off"}`
			_, err = decodeAssistantProfile([]byte(profile))
			assistantRunMust(t, err)
			writeProjectFile(t, project, assistantProfilePath, profile)
			writeProjectFile(t, project, "jpack.json", `{"configVersion":"5","packs":{"one":{"path":"one.json","description":"A minimal pack"}}}`)
			writeProjectFile(t, project, "one.json", persistedProposalOriginal)
			store, err := s.openChatData()
			assistantRunMust(t, err)
			storeName, err := resolveConversationName(store.root, s.conversationIdentity())
			assistantRunMust(t, err)
			suitePath := filepath.Join(store.root.Name(), strings.Replace(storeName, "conversations-", "pack-tests-", 1))
			assistantRunMust(t, store.root.Close())
			reportPath := filepath.Join(root, "browser-result.json")
			assistantRunMust(t, os.WriteFile(reportPath, []byte("{}\n"), 0600))
			controlPath := filepath.Join(root, "control.json")
			replayWrite(t, controlPath, map[string]any{"kind": "offline-persisted-proposal-control", "caseId": mode, "origin": server.URL, "setupCode": token, "browserBin": browser, "reportPath": reportPath, "suitePath": suitePath, "original": persistedProposalOriginal, "candidate": candidate, "prompt": "Propose the linked saved pack with version 0.1.1 only. Keep all saved test expectations and policy unchanged. Use propose_saved_pack once, then finish."})
			deadline := started.Add(180 * time.Second)
			if d := time.Now().Add(75 * time.Second); d.Before(deadline) {
				deadline = d
			}
			ctx, cancel := context.WithDeadline(t.Context(), deadline)
			cmd := exec.CommandContext(ctx, "node", driver, controlPath)
			var output replayBoundedOutput
			cmd.Stdout = &output
			cmd.Stderr = &output
			stop := context.AfterFunc(ctx, func() { _ = s.Close() })
			browserErr := cmd.Run()
			stop()
			cancel()
			closeErr := s.Close()
			server.Close()
			assistantRunMust(t, os.WriteFile(filepath.Join(root, "driver-status.log"), output.Bytes(), 0600))
			raw, readErr := replayReadPrivate(reportPath, 65536)
			var browserReport map[string]any
			if readErr == nil {
				readErr = json.Unmarshal(raw, &browserReport)
			}
			disk, diskErr := os.ReadFile(filepath.Join(project, "one.json"))
			traceSummary, traceErr := replayTrace(trace)
			mounted := map[string]any{"kind": "offline-persisted-proposal-mounted", "provenance": "offline-test", "qualification": "provisional", "caseId": mode, "identities": identities, "browser": browserReport, "apiCalls": apiCalls.Load(), "trace": traceSummary, "disk": string(disk), "diskSha256": replayDigest(disk), "elapsedMs": time.Since(started).Milliseconds(), "complete": browserErr == nil && !output.overflow && closeErr == nil && readErr == nil && diskErr == nil && traceErr == nil}
			replayWrite(t, filepath.Join(root, "mounted-report.json"), mounted)
			if browserErr != nil || output.overflow || closeErr != nil || readErr != nil || diskErr != nil || traceErr != nil || browserReport["state"] != "complete" {
				t.Fatal("offline persisted proposal incomplete; closed evidence retained")
			}
			want := candidate
			if mode == "native-dirty-refusal" {
				want = persistedProposalOriginal
			}
			if !bytes.Equal(disk, []byte(want)) {
				t.Fatal("disk did not match authored expected bytes")
			}
			if mode == "api-save" && apiCalls.Load() != 2 || mode != "api-save" && apiCalls.Load() != 0 {
				t.Fatal("unexpected model invocation count")
			}
			t.Log("normal persisted proposal journey complete; exact disk and closed evidence retained")
		}) {
			break
		}
	}
}

// Independently handwritten expectations connect the actual saved-suite projection
// to the finite peer; no application output is used to author this matrix.
func TestPersistedProposalMatrixFinitePeer(t *testing.T) {
	suite := []byte(`{"revision":1,"cases":[{"id":"existing","name":"Authored original","revision":1,"row":{"id":"existing","facts":{"n":1},"expectedDisposition":"proceed"},"sources":[],"sourceMappings":{},"origin":"manual","rationale":"Original requirement: proceed without changing policy."}],"runs":[],"recovered":[]}`)
	projected, ids, err := assistantSavedPackMatrix(suite)
	assistantRunMust(t, err)
	expected := `{"matrixVersion":"3","cases":[{"id":"existing","facts":{"n":1},"expectedDisposition":"proceed","focus":"Original requirement: proceed without changing policy.","origin":"manual"}]}`
	var got, want any
	assistantRunMust(t, json.Unmarshal(projected, &got))
	assistantRunMust(t, json.Unmarshal([]byte(expected), &want))
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if !bytes.Equal(gotJSON, wantJSON) || len(ids) != 1 || ids[0] != "existing" {
		t.Fatal("saved suite projection differs from authored matrix")
	}
	root := t.TempDir()
	trace, candidatePath := filepath.Join(root, "trace"), filepath.Join(root, "candidate.json")
	candidate := strings.Replace(persistedProposalOriginal, `"version":"0.1.0"`, `"version":"0.1.1"`, 1)
	assistantRunMust(t, os.WriteFile(trace, nil, 0600))
	assistantRunMust(t, os.WriteFile(candidatePath, []byte(candidate), 0600))
	frame, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "experimental_test_cases", "arguments": map[string]string{"pack": candidate, "matrix": string(projected)}}})
	peer, _ := filepath.Abs("../../scripts/release-quality/persisted-proposal-peer.py")
	cmd := exec.Command("/usr/bin/python3", peer, "runtime", root, trace, candidatePath, "mcp")
	cmd.Stdin = bytes.NewReader(append(frame, '\n'))
	output, err := cmd.Output()
	assistantRunMust(t, err)
	var envelope struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   json.RawMessage `json:"error"`
	}
	assistantRunMust(t, json.Unmarshal(output, &envelope))
	if envelope.JSONRPC != "2.0" || string(envelope.ID) != "1" || len(envelope.Result) == 0 || (len(envelope.Error) != 0 && string(envelope.Error) != "null") {
		t.Fatal("finite peer returned an invalid JSON-RPC result envelope")
	}
	payload, err := assistantSavedPackPayload(envelope.Result)
	assistantRunMust(t, err)
	if !validAssistantSavedPackReport(assistantChatEncodeObject(payload), []string{"existing"}) {
		t.Fatal("finite peer report does not satisfy saved-suite contract")
	}
}
