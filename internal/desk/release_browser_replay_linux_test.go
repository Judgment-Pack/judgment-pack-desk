package desk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
)

type replayPublicCase struct {
	ID, Prompt, CorpusSHA256 string
}

// The executable owns the normal Server lifecycle. Only the external browser
// drives model admission; all native/Runtime executables are supplied fixtures.
func TestReleaseReplayOfflineBrowser(t *testing.T) {
	if os.Getenv("DESK_OFFLINE_BROWSER_REPLAY") != "1" {
		t.Skip("explicit offline mounted release probe only")
	}
	out := os.Getenv("DESK_OFFLINE_BROWSER_OUTPUT")
	public := os.Getenv("DESK_REPLAY_PUBLIC_ROOT")
	browser := os.Getenv("DESK_REPLAY_BROWSER_BIN")
	for _, p := range []string{out, public, browser} {
		if !filepath.IsAbs(p) {
			t.Fatal("explicit absolute owned output, public corpus and browser paths required")
		}
	}
	if err := os.Mkdir(out, 0700); err != nil {
		t.Fatal("fresh output directory required")
	}
	driver, _ := filepath.Abs("../../scripts/release-quality/browser-replay.mjs")
	reader, _ := filepath.Abs("../../scripts/release-quality/replay-report.mjs")
	peer, _ := filepath.Abs("../../scripts/release-quality/offline-peer.py")
	launcher, _ := filepath.Abs("release_browser_replay_linux_test.go")
	executable, err := os.Executable()
	assistantRunMust(t, err)
	slicePath := filepath.Join(public, "quality/release-slice.json")
	const sliceSHA = "7728e5f53f9539c0343a86d6b4aa9351b1c8aad662acf35cd495dca36c91fe24"
	if replayHash(t, slicePath) != sliceSHA {
		t.Fatal("exact pinned public slice required")
	}
	var slice struct {
		Kind  string
		Cases []replayPublicCase
	}
	raw, err := os.ReadFile(slicePath)
	assistantRunMust(t, err)
	assistantRunMust(t, json.Unmarshal(raw, &slice))
	if slice.Kind != "release-quality-slice" || len(slice.Cases) != 2 || slice.Cases[0].ID != "model-policy-refusal" || slice.Cases[1].ID != "model-recorded-tool-result" {
		t.Fatal("exact public cases required")
	}
	assets, _ := filepath.Abs("../../web/dist")
	identities := map[string]string{"launcher": replayHash(t, launcher), "driver": replayHash(t, driver), "reader": replayHash(t, reader), "peer": replayHash(t, peer), "applicationExecutable": replayHash(t, executable), "browserExecutable": replayHash(t, browser), "assets": replayTree(t, assets, nil)}
	var sourceRows [][2]string
	for _, path := range []string{"internal", "web/src"} {
		base, _ := filepath.Abs("../../" + path)
		replayTree(t, base, &sourceRows)
	}
	encoded, _ := json.Marshal(sourceRows)
	identities["sourceInventory"] = replayDigest(encoded)
	if len(encoded) > 4<<20 {
		t.Fatal("source inventory bound")
	}
	assistantRunMust(t, os.WriteFile(filepath.Join(out, "provisional-source-inventory.json"), append(encoded, '\n'), 0600))
	started := time.Now()
	for i, fixed := range slice.Cases {
		// Never advance to another case after an incomplete run/cleanup.
		if time.Since(started) >= 180*time.Second {
			t.Fatal("offline aggregate deadline")
		}
		if !t.Run(fixed.ID, func(t *testing.T) {
			root := filepath.Join(out, fixed.ID)
			assistantRunMust(t, os.Mkdir(root, 0700))
			for _, name := range []string{"project", "config", "evidence"} {
				assistantRunMust(t, os.Mkdir(filepath.Join(root, name), 0700))
			}
			project, config, evidence := filepath.Join(root, "project"), filepath.Join(root, "config"), filepath.Join(root, "evidence")
			trace := filepath.Join(evidence, "fixture-lifecycle.jsonl")
			assistantRunMust(t, os.WriteFile(trace, nil, 0600))
			corpus := replayTree(t, filepath.Join(public, "corpus", fixed.ID), nil)
			if corpus != fixed.CorpusSHA256 {
				t.Fatal("pinned public corpus differs")
			}
			mode := "policy"
			if i == 1 {
				mode = "schema"
			}
			binary := func(role string) string {
				p := filepath.Join(root, role)
				body := fmt.Sprintf("#!/bin/sh\nexec /usr/bin/python3 %q %q %q --trace %q \"$@\"\n", peer, role, mode, trace)
				assistantRunMust(t, os.WriteFile(p, []byte(body), 0700))
				return p
			}
			token, err := NewToken()
			assistantRunMust(t, err)
			sink := codexbridge.NewAccountingSink()
			s, server := startDesk(t, Config{ProjectDir: project, DeskConfigDir: config, Static: os.DirFS(assets), JpackBin: binary("runtime"), CodexBin: binary("native"), CodexAccounting: sink, Token: token})
			t.Cleanup(func() { _ = s.Close(); server.Close() })
			model := "gpt-6-sol"
			c := aiConnection{ID: testAIAlpha, Name: "Offline fixture", Enabled: true, Assistant: AssistantSlotView{Engine: "codex", Thinking: "off", Agent: &AssistantAgentConfig{Provider: "openai", AuthMethod: "subscription", Model: &model, Models: []string{model}, Tools: []string{"get_schema"}, Effort: "medium", ChildModel: model, CriticModel: model}}}
			registry, err := encodeAIRegistry(aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{c}})
			assistantRunMust(t, err)
			assistantRunMust(t, s.assistant.writeConfigNamed(aiConnectionsFile, registry, nil))
			writeProjectFile(t, s.projectDir, assistantProfilePath, `{"profileVersion":1,"api":{"inherit":true},"codex":{"inherit":true},"critic":"off"}`)
			browserPath := filepath.Join(evidence, "browser-result.json")
			replayWrite(t, browserPath, map[string]any{"kind": "offline-mounted-browser-result", "provenance": "offline-test", "caseId": fixed.ID, "state": "incomplete", "reason": "pending", "runId": nil, "chatId": nil, "answers": nil, "tools": nil, "operations": nil, "counts": nil, "settings": nil, "elapsedMs": 0})
			controlPath := filepath.Join(root, "control.json")
			replayWrite(t, controlPath, map[string]any{"kind": "offline-mounted-replay-control", "provenance": "offline-test", "caseId": fixed.ID, "prompt": fixed.Prompt, "model": model, "effort": "medium", "caseDeadlineMs": 60000, "aggregateDeadlineMs": 180000, "origin": server.URL, "setupCode": token, "browserBin": browser, "reportPath": browserPath})
			want := i
			report := map[string]any{"kind": "offline-mounted-case-report", "provenance": "offline-test", "qualification": "provisional", "state": "incomplete", "reason": "pending", "input": map[string]any{"caseId": fixed.ID, "prompt": fixed.Prompt, "corpusSha256": corpus, "sliceSha256": sliceSHA}, "bounds": map[string]any{"caseMs": 60000, "aggregateMs": 180000, "maxNativeTurns": 1, "maxCallbacks": want}, "identities": identities, "browser": nil, "accounting": nil, "dropped": nil, "trace": nil}
			reportPath := filepath.Join(evidence, "report.json")
			replayWrite(t, reportPath, report)
			deadline := started.Add(180 * time.Second)
			if bound := time.Now().Add(75 * time.Second); bound.Before(deadline) {
				deadline = bound
			}
			ctx, cancel := context.WithDeadline(t.Context(), deadline)
			cmd := exec.CommandContext(ctx, "node", driver, controlPath)
			var output replayBoundedOutput
			cmd.Stdout, cmd.Stderr = &output, &output
			// Deadline cancellation reaches the normal Server owner even if the
			// browser cannot use its UI Stop. No exact-child/pattern kill here.
			stopOwner := context.AfterFunc(ctx, func() { _ = s.Close() })
			browserErr := cmd.Run()
			stopOwner()
			cancel()
			closeErr := s.Close()
			server.Close()
			assistantRunMust(t, os.WriteFile(filepath.Join(evidence, "driver-status.log"), output.Bytes(), 0600))
			var browserReport map[string]json.RawMessage
			data, readErr := replayReadPrivate(browserPath, 65536)
			if readErr == nil && uniqueJSON(data) && json.Unmarshal(data, &browserReport) == nil {
				report["browser"] = browserReport
			}
			accounting := []map[string]any{}
			accountingOK := true
			drain := true
			for drain {
				select {
				case a := <-sink.Reports():
					accountingOK = accountingOK && a.Invocation == 1 && a.Admitted && a.OwnedClientReaped && a.ApplicationSuccess && a.TerminalObserved && a.TerminalStatus == "completed" && !a.Incomplete && !a.FixedModelIneligible && a.RerouteCount == 0 && a.RequestedModel == model && a.AcknowledgedModel == model && a.RequestedEffort.Value == "medium" && a.AcknowledgedEffort.Value == "medium"
					accounting = append(accounting, map[string]any{"invocation": a.Invocation, "requestedModel": a.RequestedModel, "requestedEffort": a.RequestedEffort, "acknowledgedModel": a.AcknowledgedModel, "acknowledgedEffort": a.AcknowledgedEffort, "admitted": a.Admitted, "reaped": a.OwnedClientReaped, "success": a.ApplicationSuccess, "terminalObserved": a.TerminalObserved, "terminalStatus": a.TerminalStatus, "usagePresent": a.UsagePresent, "usageValid": a.UsageSchemaValid, "usage": a.Usage, "finality": a.TerminalUsageFinality, "incomplete": a.Incomplete, "fixedModelIneligible": a.FixedModelIneligible, "rerouteCount": a.RerouteCount})
				default:
					drain = false
				}
			}
			// An absent report remains a missing array member, never zero usage.
			report["accounting"], report["dropped"] = accounting, sink.Dropped()
			traceSummary, traceErr := replayTrace(trace)
			report["trace"] = traceSummary
			report["reason"] = "browser-incomplete"
			if browserErr == nil && !output.overflow {
				report["state"], report["reason"] = "complete", "complete"
			}
			if closeErr != nil {
				report["state"], report["reason"] = "incomplete", "shutdown-incomplete"
			} else if traceErr != nil {
				report["state"], report["reason"] = "incomplete", "trace-incomplete"
			} else if len(accounting) != 1 || sink.Dropped() != 0 || !accountingOK {
				report["state"], report["reason"] = "incomplete", "accounting-incomplete"
			}
			replayWrite(t, reportPath, report)
			validation := exec.Command("node", reader, "validate", reportPath)
			validation.Stdout, validation.Stderr = io.Discard, io.Discard
			if err := validation.Run(); err != nil {
				t.Fatal("closed offline report refused; private evidence retained")
			}
			if report["state"] != "complete" {
				t.Fatalf("offline mounted case incomplete: %s", report["reason"])
			}
			t.Log("offline normal mounted browser complete; private report retained")
		}) {
			break
		}
	}
}

func replayWrite(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	assistantRunMust(t, err)
	if len(data) > 65536 {
		t.Fatal("closed replay evidence bound")
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0600)
	assistantRunMust(t, err)
	_, err = f.Write(append(data, '\n'))
	assistantRunMust(t, err)
	assistantRunMust(t, f.Close())
}

func replayReadPrivate(path string, limit int64) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > limit {
		return nil, fmt.Errorf("private evidence unavailable")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return nil, fmt.Errorf("private evidence ownership unavailable")
	}
	return io.ReadAll(io.LimitReader(f, limit+1))
}

func replayDigest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func replayHash(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	assistantRunMust(t, err)
	defer f.Close()
	h := sha256.New()
	_, err = io.Copy(h, f)
	assistantRunMust(t, err)
	return hex.EncodeToString(h.Sum(nil))
}
func replayTree(t *testing.T, root string, inventory *[][2]string) string {
	t.Helper()
	rows := [][2]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("identity symlink refused")
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rows = append(rows, [2]string{filepath.ToSlash(rel), replayHash(t, path)})
		return nil
	})
	assistantRunMust(t, err)
	if inventory != nil {
		for _, row := range rows {
			*inventory = append(*inventory, [2]string{filepath.Base(root) + "/" + row[0], row[1]})
		}
	}
	data, _ := json.Marshal(rows)
	return replayDigest(data)
}

type replayBoundedOutput struct {
	bytes.Buffer
	overflow bool
}

func (w *replayBoundedOutput) Write(data []byte) (int, error) {
	n := len(data)
	left := 8192 - w.Len()
	if n > left {
		w.overflow = true
		data = data[:left]
	}
	_, _ = w.Buffer.Write(data)
	return n, nil
}

func replayTrace(path string) (map[string]any, error) {
	data, err := replayReadPrivate(path, 65536)
	if err != nil {
		return nil, err
	}
	summary := map[string]any{"runtimeStarts": 0, "nativeStarts": 0, "nativeTurns": 0, "callbacks": 0, "schemaCalls": 0, "metadataCalls": 0, "refusals": 0, "allPidsAbsent": true}
	pids := map[int]bool{}
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		var row struct {
			Kind string `json:"kind"`
			Role string `json:"role"`
			PID  int    `json:"pid"`
			Tag  string `json:"tag"`
		}
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.DisallowUnknownFields()
		if !uniqueJSON(line) || decoder.Decode(&row) != nil || row.PID <= 0 || (row.Role != "native" && row.Role != "runtime") {
			return summary, fmt.Errorf("invalid fixture lifecycle")
		}
		key := ""
		switch row.Kind {
		case "start":
			pids[row.PID] = true
			key = row.Role + "Starts"
		case "native-turn":
			key = "nativeTurns"
		case "callback":
			key = "callbacks"
		case "schema":
			key = "schemaCalls"
		case "metadata":
			key = "metadataCalls"
		case "refused":
			key = "refusals"
		default:
			return summary, fmt.Errorf("unknown fixture lifecycle")
		}
		summary[key] = summary[key].(int) + 1
	}
	for pid := range pids {
		if syscall.Kill(pid, 0) != syscall.ESRCH {
			summary["allPidsAbsent"] = false
			return summary, fmt.Errorf("fixture owned PID still present")
		}
	}
	return summary, nil
}
