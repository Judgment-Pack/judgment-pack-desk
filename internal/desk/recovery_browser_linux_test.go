package desk

import (
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

// Only the external peers are fixtures. Admission, execution, persisted records,
// sign-in, reconnect and cancellation all use the normal Desk application.
func TestRecoveryOfflineBrowser(t *testing.T) {
	if os.Getenv("DESK_RECOVERY_REPLAY") != "1" {
		t.Skip("explicit finite offline recovery browser batch")
	}
	out, browser := os.Getenv("DESK_RECOVERY_OUTPUT"), os.Getenv("DESK_REPLAY_BROWSER_BIN")
	if !filepath.IsAbs(out) || !filepath.IsAbs(browser) {
		t.Fatal("absolute owned output and browser required")
	}
	assistantRunMust(t, os.Mkdir(out, 0700))
	driver, _ := filepath.Abs("../../scripts/release-quality/recovery-browser.mjs")
	peer, _ := filepath.Abs("../../scripts/release-quality/recovery-browser-peer.py")
	assets, _ := filepath.Abs("../../web/dist")
	launcher, _ := filepath.Abs("recovery_browser_linux_test.go")
	exe, err := os.Executable()
	assistantRunMust(t, err)
	identities := map[string]string{"applicationExecutable": replayHash(t, exe), "driver": replayHash(t, driver), "peer": replayHash(t, peer), "launcher": replayHash(t, launcher), "browser": replayHash(t, browser), "assets": replayTree(t, assets, nil)}
	for _, name := range []string{"replay-report.mjs", "offline-owner-browser-qualification.mjs"} {
		path, _ := filepath.Abs("../../scripts/release-quality/" + name)
		identities[name] = replayHash(t, path)
	}
	var rows [][2]string
	for _, name := range []string{"internal", "web/src"} {
		path, _ := filepath.Abs("../../" + name)
		replayTree(t, path, &rows)
	}
	raw, err := json.Marshal(rows)
	assistantRunMust(t, err)
	assistantRunMust(t, os.WriteFile(filepath.Join(out, "source-inventory.json"), raw, 0600))
	identities["sourceInventory"] = replayDigest(raw)
	for _, mode := range []string{"reload-stop", "typed-error"} {
		if !t.Run(mode, func(t *testing.T) {
			root := filepath.Join(out, mode)
			assistantRunMust(t, os.Mkdir(root, 0700))
			project, config := filepath.Join(root, "project"), filepath.Join(root, "config")
			assistantRunMust(t, os.Mkdir(project, 0700))
			assistantRunMust(t, os.Mkdir(config, 0700))
			trace, runtime := filepath.Join(root, "fixture-lifecycle.jsonl"), filepath.Join(root, "runtime")
			assistantRunMust(t, os.WriteFile(trace, nil, 0600))
			assistantRunMust(t, os.WriteFile(runtime, []byte(fmt.Sprintf("#!/bin/sh\nexec /usr/bin/python3 %q %q %q \"$@\"\n", peer, project, trace)), 0700))
			var calls, cancelled, starts, actions atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, err := io.ReadAll(io.LimitReader(r.Body, (4<<20)+1))
				var request struct {
					Model string `json:"model"`
					Tools []struct {
						Function struct {
							Name string `json:"name"`
						} `json:"function"`
					} `json:"tools"`
				}
				if err != nil || len(raw) > 4<<20 || json.Unmarshal(raw, &request) != nil || request.Model != "gpt-6-sol" {
					t.Error("finite recovery provider request refused")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				switch calls.Add(1) {
				case 1:
					found := 0
					for _, tool := range request.Tools {
						if tool.Function.Name == "get_schema" {
							found++
						}
					}
					if found != 1 {
						t.Error("read-only schema tool not offered exactly once")
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					assistantPolicySSE(w, "get_schema", map[string]any{}, "")
				case 2:
					if mode == "typed-error" {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusUnauthorized)
						_, _ = io.WriteString(w, `{"error":{"message":"fixture-provider-prose-must-not-render","type":"authentication_error","code":"invalid_api_key"}}`)
						return
					}
					select {
					case <-r.Context().Done():
						cancelled.Add(1)
					case <-time.After(100 * time.Second):
						t.Error("held provider exceeded finite case deadline")
					}
				default:
					t.Error("unexpected provider retry or replay")
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			t.Cleanup(upstream.Close)
			token, err := NewToken()
			assistantRunMust(t, err)
			s, server := startDeskWithHandler(t, Config{ProjectDir: project, DeskConfigDir: config, Static: os.DirFS(assets), JpackBin: runtime, RunnerBin: "off", CodexBin: "off", Token: token}, func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == "POST" && r.URL.Path == "/api/assistant-runs" {
						starts.Add(1)
					}
					if r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/api/assistant-runs/") && strings.HasSuffix(r.URL.Path, "/actions") {
						actions.Add(1)
					}
					next.ServeHTTP(w, r)
				})
			})
			t.Cleanup(func() { _ = s.Close(); server.Close() })
			model := "gpt-6-sol"
			connection := aiConnection{ID: testAIAlpha, Name: "Offline recovery fixture", Enabled: true, Assistant: AssistantSlotView{Engine: "vercel", Thinking: "off", Endpoint: &AssistantEndpointView{URL: upstream.URL, Kind: "openai-compatible", Models: []string{model}, Model: &model, Tools: []string{"get_schema"}}}}
			registry, err := encodeAIRegistry(aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{connection}})
			assistantRunMust(t, err)
			_, err = decodeAIRegistry(registry)
			assistantRunMust(t, err)
			assistantRunMust(t, s.assistant.writeConfigNamed(aiConnectionsFile, registry, nil))
			origin, ok := endpointOrigin(upstream.URL)
			if !ok {
				t.Fatal("finite provider origin invalid")
			}
			assistantRunMust(t, s.assistant.storeKeyNamed(connectionKeyName(&connection), storedKey{present: true, key: "offline-fixture-key", origin: origin, kind: "openai-compatible"}))
			writeProjectFile(t, project, assistantProfilePath, `{"profileVersion":1,"api":{"inherit":true},"codex":{"inherit":true},"critic":"off"}`)
			writeProjectFile(t, project, "jpack.json", `{"configVersion":"5","packs":{"one":{"path":"one.json","description":"A minimal pack"}}}`)
			writeProjectFile(t, project, "one.json", persistedProposalOriginal)
			reportPath, controlPath := filepath.Join(root, "browser-result.json"), filepath.Join(root, "control.json")
			replayWrite(t, reportPath, map[string]any{})
			replayWrite(t, controlPath, map[string]any{"kind": "offline-recovery-control", "caseId": mode, "origin": server.URL, "setupCode": token, "browserBin": browser, "reportPath": reportPath})
			started := time.Now()
			ctx, cancel := context.WithTimeout(t.Context(), 105*time.Second)
			cmd := exec.CommandContext(ctx, "node", driver, controlPath)
			var output replayBoundedOutput
			cmd.Stdout, cmd.Stderr = &output, &output
			stop := context.AfterFunc(ctx, func() { _ = s.Close() })
			browserErr := cmd.Run()
			cancelledBeforeCleanup := cancelled.Load()
			stop()
			cancel()
			closeErr := s.Close()
			server.Close()
			upstream.Close()
			assistantRunMust(t, os.WriteFile(filepath.Join(root, "driver-status.log"), output.Bytes(), 0600))
			data, readErr := replayReadPrivate(reportPath, 65536)
			var report struct {
				State string `json:"state"`
			}
			if readErr == nil {
				readErr = json.Unmarshal(data, &report)
			}
			lifecycle, traceErr := replayTrace(trace)
			wantActions, wantCancelled := int32(0), int32(0)
			if mode == "reload-stop" {
				wantActions, wantCancelled = 1, 1
			}
			complete := browserErr == nil && !output.overflow && closeErr == nil && readErr == nil && traceErr == nil && report.State == "complete" && calls.Load() == 2 && starts.Load() == 1 && actions.Load() == wantActions && cancelled.Load() == wantCancelled
			if complete {
				complete = lifecycle["schemaCalls"] == 1 && lifecycle["refusals"] == 0 && lifecycle["allPidsAbsent"] == true
			}
			replayWrite(t, filepath.Join(root, "mounted-report.json"), map[string]any{"kind": "offline-recovery-mounted", "provenance": "offline-test", "caseId": mode, "complete": complete, "identities": identities, "apiCalls": calls.Load(), "starts": starts.Load(), "actions": actions.Load(), "providerCancelled": cancelled.Load(), "providerCancelledBeforeCleanup": cancelledBeforeCleanup, "trace": lifecycle, "elapsedMs": time.Since(started).Milliseconds()})
			if !complete {
				t.Fatal("offline recovery incomplete; private evidence retained")
			}
			t.Log("actual application recovery and unchanged completed operation evidence passed")
		}) {
			break
		}
	}
}
