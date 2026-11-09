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
	"sync/atomic"
	"testing"
	"time"
)

// This opt-in journey mounts the real server and built UI. Only the external
// provider and Runtime are fixtures; no chat, run, proposal or receipt is seeded.
func TestMemoryReviewOfflineBrowser(t *testing.T) {
	memoryReviewOfflineBrowser(t, "accept")
}

// Each independent case has one normal owner sign-in, at most two starts,
// four local provider calls, two settings POSTs and four decision POSTs. The browser
// deadline is 120 seconds for Clear's two-response journey and 90 seconds otherwise.
func TestMemoryManagementOfflineBrowser(t *testing.T) {
	for _, scenario := range []string{"edit-forget", "decline", "clear", "stale", "lost-ack"} {
		if !t.Run(scenario, func(t *testing.T) { memoryReviewOfflineBrowser(t, scenario) }) {
			return
		}
	}
}

func memoryReviewOfflineBrowser(t *testing.T, scenario string) {
	if os.Getenv("DESK_MEMORY_REVIEW_REPLAY") != "1" {
		t.Skip("explicit finite offline memory review journey")
	}
	out, browser := os.Getenv("DESK_MEMORY_REVIEW_OUTPUT"), os.Getenv("DESK_REPLAY_BROWSER_BIN")
	if !filepath.IsAbs(out) || !filepath.IsAbs(browser) {
		t.Fatal("absolute owned output and browser required")
	}
	if scenario != "accept" {
		out = filepath.Join(out, scenario)
	}
	assistantRunMust(t, os.Mkdir(out, 0700))
	project, config := filepath.Join(out, "project"), filepath.Join(out, "config")
	assistantRunMust(t, os.Mkdir(project, 0700))
	assistantRunMust(t, os.Mkdir(config, 0700))
	driver, _ := filepath.Abs("../../scripts/release-quality/memory-review-browser.mjs")
	peer, _ := filepath.Abs("../../scripts/release-quality/persisted-proposal-peer.py")
	assets, _ := filepath.Abs("../../web/dist")
	launcher, _ := filepath.Abs("memory_review_browser_linux_test.go")
	exe, err := os.Executable()
	assistantRunMust(t, err)
	identities := map[string]string{"applicationExecutable": replayHash(t, exe), "launcher": replayHash(t, launcher), "driver": replayHash(t, driver), "peer": replayHash(t, peer), "browser": replayHash(t, browser), "assets": replayTree(t, assets, nil)}
	for _, name := range []string{"replay-report.mjs", "persisted-proposal-report.mjs", "offline-owner-browser-qualification.mjs"} {
		path, _ := filepath.Abs("../../scripts/release-quality/" + name)
		identities[name] = replayHash(t, path)
	}
	var sourceRows [][2]string
	for _, name := range []string{"internal", "web/src"} {
		base, _ := filepath.Abs("../../" + name)
		replayTree(t, base, &sourceRows)
	}
	encoded, err := json.Marshal(sourceRows)
	assistantRunMust(t, err)
	assistantRunMust(t, os.WriteFile(filepath.Join(out, "source-inventory.json"), encoded, 0600))
	identities["sourceInventory"] = replayDigest(encoded)
	trace, fixture := filepath.Join(out, "fixture-lifecycle.jsonl"), filepath.Join(out, "pack.fixture.json")
	assistantRunMust(t, os.WriteFile(trace, nil, 0600))
	assistantRunMust(t, os.WriteFile(fixture, []byte(persistedProposalOriginal), 0600))
	runtime := filepath.Join(out, "runtime")
	assistantRunMust(t, os.WriteFile(runtime, []byte(fmt.Sprintf("#!/bin/sh\nexec /usr/bin/python3 %q runtime %q %q %q \"$@\"\n", peer, project, trace, fixture)), 0700))
	var calls atomic.Int32
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
			t.Error("finite memory provider request refused")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		call := calls.Add(1)
		maxCalls := int32(2)
		if scenario == "clear" {
			maxCalls = 4
		}
		if call > maxCalls {
			t.Error("finite memory provider received an extra invocation")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		switch call % 2 {
		case 1:
			offered := 0
			for _, tool := range request.Tools {
				if tool.Function.Name == "remember" {
					offered++
				}
			}
			if offered != 1 {
				t.Error("canonical memory proposal tool not offered exactly once")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			assistantPolicySSE(w, "remember", map[string]string{"kind": "preference", "text": "Keep outreach concise."}, "")
		case 0:
			assistantPolicySSE(w, "", nil, "The preference is ready for your review.")
		default:
			t.Error("finite memory provider received an extra invocation")
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer upstream.Close()
	token, err := NewToken()
	assistantRunMust(t, err)
	s, server := startDesk(t, Config{ProjectDir: project, DeskConfigDir: config, Static: os.DirFS(assets), JpackBin: runtime, RunnerBin: "off", CodexBin: "off", Token: token})
	t.Cleanup(func() { _ = s.Close(); server.Close() })
	model := "gpt-6-sol"
	connection := aiConnection{ID: testAIAlpha, Name: "Offline memory fixture", Enabled: true, Assistant: AssistantSlotView{Engine: "vercel", Thinking: "off", Endpoint: &AssistantEndpointView{URL: upstream.URL, Kind: "openai-compatible", Models: []string{model}, Model: &model, Tools: []string{"get_schema"}}}}
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
	reportPath, controlPath := filepath.Join(out, "browser-result.json"), filepath.Join(out, "control.json")
	replayWrite(t, reportPath, map[string]any{})
	replayWrite(t, controlPath, map[string]any{"kind": "offline-memory-review-control", "scenario": scenario, "origin": server.URL, "setupCode": token, "browserBin": browser, "reportPath": reportPath})
	started := time.Now()
	launcherTimeout := 105 * time.Second
	if scenario == "clear" {
		launcherTimeout = 135 * time.Second
	}
	ctx, cancel := context.WithTimeout(t.Context(), launcherTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", driver, controlPath)
	var output replayBoundedOutput
	cmd.Stdout, cmd.Stderr = &output, &output
	stop := context.AfterFunc(ctx, func() { _ = s.Close() })
	browserErr := cmd.Run()
	stop()
	cancel()
	var stored memoryReply
	var durableProposals []map[string]any
	diskErr := s.withMemory(func(root *os.Root, reply *memoryReply) error {
		stored = *reply
		ledger, err := s.memoryProposalRuns(root, reply.Name)
		if err != nil {
			return err
		}
		for _, run := range ledger.Runs {
			for _, proposal := range run.Checkpoint.Memories.Proposed {
				durableProposals = append(durableProposals, map[string]any{"runId": run.ID, "runRevision": run.Revision, "id": proposal.ID, "digest": proposal.Digest, "text": proposal.Text})
			}
		}
		return nil
	})
	closeErr := s.Close()
	server.Close()
	assistantRunMust(t, os.WriteFile(filepath.Join(out, "driver-status.log"), output.Bytes(), 0600))
	raw, readErr := replayReadPrivate(reportPath, 65536)
	var report struct {
		State        string `json:"state"`
		MemorySHA256 string `json:"memorySha256"`
		RunID        string `json:"runId"`
		Proposal     struct {
			ID          string `json:"id"`
			Digest      string `json:"digest"`
			RunRevision uint64 `json:"runRevision"`
		} `json:"proposal"`
	}
	if readErr == nil {
		readErr = json.Unmarshal(raw, &report)
	}
	lifecycle, traceErr := replayTrace(trace)
	expectedEntries, expectedCalls := 1, int32(2)
	if scenario == "edit-forget" || scenario == "decline" || scenario == "clear" {
		expectedEntries = 0
	}
	if scenario == "clear" {
		expectedCalls = 4
	}
	complete := browserErr == nil && !output.overflow && readErr == nil && diskErr == nil && closeErr == nil && traceErr == nil && report.State == "complete" && calls.Load() == expectedCalls && stored.SHA256 == report.MemorySHA256 && len(stored.Content.Entries) == expectedEntries && len(stored.Content.Reviews) == 1
	if complete {
		matched := 0
		for _, proposal := range durableProposals {
			if proposal["runId"] == report.RunID && proposal["runRevision"] == report.Proposal.RunRevision && proposal["id"] == report.Proposal.ID && proposal["digest"] == report.Proposal.Digest && proposal["text"] == "Keep outreach concise." {
				matched++
			}
		}
		complete = matched == 1
	}
	if complete {
		receipt := stored.Content.Reviews[0]
		decision := "accept"
		if scenario == "edit-forget" || scenario == "clear" || scenario == "stale" {
			decision = "edit"
		}
		if scenario == "decline" {
			decision = "decline"
		}
		complete = receipt.Decision == decision && !stored.Content.Settings.Use && stored.Content.Settings.Suggest && lifecycle["refusals"] == 0 && lifecycle["allPidsAbsent"] == true
		if complete && expectedEntries == 1 {
			entry := stored.Content.Entries[0]
			text := "Keep outreach concise."
			if scenario == "stale" {
				text = "Keep outreach evidence explicit."
			}
			complete = entry.Text == text && entry.State == "active" && entry.Canonical != nil && receipt.MemoryID == entry.Canonical.ID && receipt.MemoryDigest == entry.Canonical.Digest
		}
	}

	replayWrite(t, filepath.Join(out, "mounted-report.json"), map[string]any{"kind": "offline-memory-review-mounted", "provenance": "offline-test", "scenario": scenario, "complete": complete, "identities": identities, "apiCalls": calls.Load(), "durableProposals": durableProposals, "memorySha256": stored.SHA256, "entries": len(stored.Content.Entries), "receipts": len(stored.Content.Reviews), "trace": lifecycle, "elapsedMs": time.Since(started).Milliseconds()})
	if !complete {
		t.Fatal("offline memory review incomplete; private evidence retained")
	}
	t.Log("normal sign-in, canonical proposal, explicit review and reload completed; no replay")
}
