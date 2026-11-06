package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
)

// The test executable itself is the only launched peer. It does not discover a
// CLI, read credentials or use a provider. Fixtures are handwritten below.
func TestMain(m *testing.M) {
	if len(os.Args) == 4 && os.Args[1] == "app-server" && os.Args[2] == "--stdio" && os.Args[3] == "--strict-config" {
		os.Exit(finiteNativePeer())
	}
	os.Exit(m.Run())
}

func finiteNativePeer() int {
	if os.WriteFile(filepath.Join(os.Getenv("TMPDIR"), "owned-peer.pid"), []byte(strconv.Itoa(os.Getpid())), 0600) != nil {
		return 21
	}
	write := func(value any) { _ = json.NewEncoder(os.Stdout).Encode(value) }
	note := func(method string, params any) { write(map[string]any{"method": method, "params": params}) }
	scan := bufio.NewScanner(os.Stdin)
	scan.Buffer(make([]byte, 4096), 1<<20)
	for count := 0; scan.Scan(); count++ {
		if count >= 16 {
			return 22
		}
		var frame struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scan.Bytes(), &frame) != nil {
			return 23
		}
		reply := func(value any) { write(map[string]any{"id": frame.ID, "result": value}) }
		switch frame.Method {
		case "initialize":
			reply(map[string]string{"userAgent": "jps_desk/0.157.1 (Linux)"})
		case "initialized":
		case "account/read":
			reply(map[string]any{"requiresOpenaiAuth": true, "account": map[string]string{"type": "chatgpt", "planType": "plus"}})
		case "model/list":
			var params struct {
				Hidden bool `json:"includeHidden"`
			}
			if json.Unmarshal(frame.Params, &params) != nil {
				return 24
			}
			models := []string{"gpt-6-sol"}
			if params.Hidden {
				models = []string{"gpt-6-astra", "gpt-6-sol", "gpt-6-luna", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "gpt-5.5"}
			}
			rows := []any{}
			for _, model := range models {
				rows = append(rows, map[string]any{"model": model, "displayName": model, "defaultReasoningEffort": "medium", "supportedReasoningEfforts": []any{map[string]string{"reasoningEffort": "medium"}}})
			}
			reply(map[string]any{"data": rows})
		case "thread/start":
			cwd, _ := os.Getwd()
			reply(map[string]any{"thread": map[string]string{"id": "peer-thread"}, "model": "gpt-6-sol", "modelProvider": "openai", "approvalPolicy": "never", "cwd": cwd, "reasoningEffort": "medium"})
		case "turn/start":
			reply(map[string]any{"turn": map[string]string{"id": "peer-turn"}})
			if strings.Contains(string(frame.Params), `"stall"`) {
				continue
			}
			note("item/completed", map[string]any{"threadId": "peer-thread", "turnId": "peer-turn", "item": map[string]string{"id": "peer-message", "type": "agentMessage", "phase": "final_answer", "text": "Supplied fixture answer."}})
			breakdown := map[string]int{"inputTokens": 2, "cachedInputTokens": 0, "outputTokens": 3, "reasoningOutputTokens": 1, "totalTokens": 5}
			note("thread/tokenUsage/updated", map[string]any{"threadId": "peer-thread", "turnId": "peer-turn", "tokenUsage": map[string]any{"last": breakdown, "total": breakdown}})
			note("turn/completed", map[string]any{"threadId": "peer-thread", "turn": map[string]string{"id": "peer-turn", "status": "completed"}})
		default:
			return 25
		}
	}
	return 0
}

func testOptions() options {
	return options{Model: "gpt-6-sol", Effort: "medium", Case1: "fixed-first", Case2: "fixed-second", SetupTimeout: time.Second}
}

func successfulReport(sequence uint64) codexbridge.AccountingReport {
	return codexbridge.AccountingReport{SchemaVersion: 1, Invocation: sequence, RequestedModel: "gpt-6-sol",
		RequestedEffort: codexbridge.AccountingEffort{State: "value", Value: "medium"}, SelectedEffort: "medium",
		AcknowledgedModel: "gpt-6-sol", AcknowledgedProvider: "openai", AcknowledgedEffort: codexbridge.AccountingEffort{State: "null"},
		ThreadID: fmt.Sprintf("thread-%d", sequence), TurnID: fmt.Sprintf("turn-%d", sequence), Admitted: true,
		OwnedClientReaped: true, ApplicationSuccess: true, ExitReason: "success", UsagePresent: true, UsageSchemaValid: true,
		TerminalObserved: true, TerminalStatus: "completed", TerminalUsageFinality: "unproven"}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func newTestOwner(t *testing.T, closeFn func() error) (*owner, chan codexbridge.AccountingReport) {
	t.Helper()
	reports := make(chan codexbridge.AccountingReport, 16)
	h := newOwner(testOptions(), reports, func() uint64 { return 0 }, closeFn)
	t.Cleanup(func() { h.close("test_cleanup"); h.finish() })
	return h, reports
}

func completeFixture(t *testing.T, h *owner, reports chan codexbridge.AccountingReport, number int) {
	t.Helper()
	chat := fmt.Sprintf("chat-%d", number)
	must(t, h.begin(command{Action: "begin", Case: number, ChatID: chat}))
	reports <- successfulReport(uint64(number))
	must(t, h.drain())
	must(t, h.complete(command{Action: "complete", Case: number, ChatID: chat, ThreadID: fmt.Sprintf("thread-%d", number), TurnID: fmt.Sprintf("turn-%d", number)}))
}

func TestDefaultAndClosedControlDoNoWork(t *testing.T) {
	var output bytes.Buffer
	must(t, run(nil, &output))
	if !strings.Contains(output.String(), "usage:") {
		t.Fatal("default did not give inert usage")
	}
	for _, args := range [][]string{{"live"}, {"serve"}, {"serve", "--model", "gpt-6-sol"}, {"serve", "--unknown"}} {
		if err := run(args, io.Discard); !errors.Is(err, errControl) {
			t.Fatalf("open command: %v %v", args, err)
		}
	}
	for _, raw := range []string{
		`{"action":"begin","action":"complete","case":1}`,
		`{"action":"begin","authorization":"approved"}`,
		`{"action":"begin"} {}`, `null`, `[]`, `{"action":"begin","case":1.5}`,
		strings.Repeat(" ", maxControlBytes+1),
	} {
		if _, err := decodeCommand([]byte(raw)); !errors.Is(err, errControl) {
			t.Fatalf("accepted control %q", raw)
		}
	}
	if _, err := decodeCommand([]byte(`{"action":"begin","case":1,"chatId":"fresh"}`)); err != nil {
		t.Fatal(err)
	}
}

func TestSequentialCasesPreserveReportsAndFinalShutdown(t *testing.T) {
	closes := 0
	h, reports := newTestOwner(t, func() error { closes++; return nil })
	completeFixture(t, h, reports, 1)
	if closes != 0 || h.isClosing() || !h.cases[0].Completed || h.cases[1].Accounting != nil {
		t.Fatal("case1 closed the shared owner or contaminated case2")
	}
	completeFixture(t, h, reports, 2)
	h.close("completed")
	evidence := h.finish()
	if closes != 1 || !evidence.NormalOwnerShutdown || !evidence.DeadlineCallbacksJoined || evidence.Cases[0].Accounting.Invocation != 1 || evidence.Cases[1].Accounting.Invocation != 2 || evidence.Cases[0].ChatID == evidence.Cases[1].ChatID || evidence.MonetaryCost != nil {
		t.Fatalf("bad lifecycle %+v", evidence)
	}
	if h.begin(command{Action: "begin", Case: 1, ChatID: "late"}) == nil {
		t.Fatal("accepted after closure")
	}
}

func TestAccountingAnomaliesAndMissingReportRefuse(t *testing.T) {
	for _, kind := range []string{"sequence", "duplicate", "reroute", "missing", "unreaped", "wrong-model", "thread"} {
		t.Run(kind, func(t *testing.T) {
			h, reports := newTestOwner(t, func() error { return nil })
			must(t, h.begin(command{Action: "begin", Case: 1, ChatID: "chat-1"}))
			r := successfulReport(1)
			switch kind {
			case "sequence":
				r.Invocation = 2
			case "reroute":
				r.RerouteObserved = true
			case "unreaped":
				r.OwnedClientReaped = false
			case "wrong-model":
				r.AcknowledgedModel = "other"
			}
			if kind != "missing" {
				reports <- r
			}
			err := h.drain()
			if kind == "duplicate" {
				must(t, err)
				reports <- r
				err = h.drain()
			}
			if kind == "missing" || kind == "thread" {
				must(t, err)
				err = h.complete(command{Action: "complete", Case: 1, ChatID: "chat-1", ThreadID: "wrong", TurnID: "turn-1"})
			}
			if err == nil {
				t.Fatal("anomaly accepted")
			}
			h.close("accounting_anomaly")
			if h.begin(command{Action: "begin", Case: 2, ChatID: "chat-2"}) == nil {
				t.Fatal("advanced on anomaly")
			}
		})
	}
}

func TestDeadlineClosesWithStalledDriverAndAwaitsCallback(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	h, _ := newTestOwner(t, func() error { close(entered); <-release; return nil })
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	h.caseDuration = 20 * time.Millisecond
	must(t, h.begin(command{Action: "begin", Case: 1, ChatID: "first"}))
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("deadline did not own shutdown")
	}
	finished := make(chan lifecycleEvidence, 1)
	go func() { finished <- h.finish() }()
	select {
	case <-finished:
		t.Fatal("claimed cleanup before actual Close returned")
	default:
	}
	if h.begin(command{Action: "begin", Case: 2, ChatID: "late"}) == nil {
		t.Fatal("deadline allowed case2")
	}
	close(release)
	select {
	case evidence := <-finished:
		if evidence.Reason != "case_deadline" || !evidence.NormalOwnerShutdown || !evidence.DeadlineCallbacksJoined || evidence.Cases[1].ChatID != "" {
			t.Fatalf("bad deadline evidence %+v", evidence)
		}
	case <-time.After(time.Second):
		t.Fatal("callback closure not joined")
	}
}

func TestAggregateDeadlineBetweenCasesPreventsAdmission(t *testing.T) {
	h, reports := newTestOwner(t, func() error { return nil })
	h.aggregateDuration = 30 * time.Millisecond
	completeFixture(t, h, reports, 1)
	select {
	case <-h.closed:
	case <-time.After(time.Second):
		t.Fatal("aggregate deadline did not close")
	}
	if h.begin(command{Action: "begin", Case: 2, ChatID: "second"}) == nil {
		t.Fatal("admitted after aggregate expiry")
	}
	if h.finish().Reason != "aggregate_deadline" {
		t.Fatal("lost aggregate cause")
	}
}

func TestSetupDeadlinePreventsFirstCase(t *testing.T) {
	o := testOptions()
	o.SetupTimeout = 10 * time.Millisecond
	h := newOwner(o, make(chan codexbridge.AccountingReport), func() uint64 { return 0 }, func() error { return nil })
	defer func() { h.close("test_cleanup"); h.finish() }()
	select {
	case <-h.closed:
	case <-time.After(time.Second):
		t.Fatal("setup deadline did not close")
	}
	if h.begin(command{Action: "begin", Case: 1, ChatID: "late"}) == nil || h.finish().Reason != "setup_deadline" {
		t.Fatal("setup expiry admitted inference")
	}
}

// Hold the transition after the real callback is stopped, then let a deadline
// or owner shutdown win. Stopping a pending callback must not admit late work.
func TestTransitionRefusesExpiryOrClosureWhileCallbackStops(t *testing.T) {
	for _, transition := range []string{"first-case", "completion"} {
		for _, interruption := range []string{"deadline", "owner-close", "aggregate-deadline"} {
			if transition == "first-case" && interruption == "aggregate-deadline" {
				continue
			}
			t.Run(transition+"/"+interruption, func(t *testing.T) {
				o := testOptions()
				if transition == "first-case" && interruption == "deadline" {
					o.SetupTimeout = 150 * time.Millisecond
				}
				reports := make(chan codexbridge.AccountingReport, 1)
				h := newOwner(o, reports, func() uint64 { return 0 }, func() error { return nil })
				t.Cleanup(func() { h.close("test_cleanup"); h.finish() })
				watch := h.setup
				operation := func() error {
					return h.begin(command{Action: "begin", Case: 1, ChatID: "first"})
				}
				if transition == "completion" {
					if interruption == "deadline" {
						h.caseDuration = 150 * time.Millisecond
					}
					if interruption == "aggregate-deadline" {
						h.aggregateDuration = 150 * time.Millisecond
					}
					must(t, h.begin(command{Action: "begin", Case: 1, ChatID: "first"}))
					reports <- successfulReport(1)
					must(t, h.drain())
					watch = h.current
					operation = func() error {
						return h.complete(command{Action: "complete", Case: 1, ChatID: "first", ThreadID: "thread-1", TurnID: "turn-1"})
					}
				}
				stopped, release := make(chan bool, 1), make(chan struct{})
				var releaseOnce sync.Once
				releaseStop := func() { releaseOnce.Do(func() { close(release) }) }
				defer releaseStop()
				stop := watch.stop
				watch.stop = func() bool {
					result := stop()
					stopped <- result
					<-release
					return result
				}
				result := make(chan error, 1)
				go func() { result <- operation() }()
				select {
				case prevented := <-stopped:
					if !prevented {
						t.Fatal("callback ran before the intended interleaving")
					}
				case <-time.After(time.Second):
					t.Fatal("transition did not reach callback stop")
				}
				if interruption == "owner-close" {
					h.close("test_shutdown")
				} else {
					deadline := watch.ctx
					if interruption == "aggregate-deadline" {
						deadline = h.aggregate.ctx
					}
					select {
					case <-deadline.Done():
					case <-time.After(time.Second):
						t.Fatal("deadline did not expire during transition")
					}
				}
				releaseStop()
				select {
				case err := <-result:
					if !errors.Is(err, errControl) {
						t.Fatalf("late transition accepted: %v", err)
					}
				case <-time.After(time.Second):
					t.Fatal("transition deadlocked with owner shutdown")
				}
				if h.cases[0].Completed || (transition == "first-case" && h.active != 0) {
					t.Fatal("refused transition changed accepted case state")
				}
				h.close("test_refusal")
				if !h.finish().DeadlineCallbacksJoined {
					t.Fatal("shutdown did not join callbacks")
				}
			})
		}
	}
}

func waitFile(t *testing.T, path string) []byte {
	t.Helper()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(2 * time.Second)
	defer timeout.Stop()
	for {
		raw, err := os.ReadFile(path)
		if err == nil {
			return raw
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		select {
		case <-ticker.C:
		case <-timeout.C:
			t.Fatalf("missing private file %s", filepath.Base(path))
		}
	}
}

func pathFixture(t *testing.T) options {
	t.Helper()
	base := t.TempDir()
	o := testOptions()
	o.ControlDir, o.ProjectDir, o.ConfigDir, o.AssetsDir = filepath.Join(base, "control"), filepath.Join(base, "project"), filepath.Join(base, "config"), filepath.Join(base, "assets")
	for _, path := range []string{o.ControlDir, o.ProjectDir, o.ConfigDir, o.AssetsDir} {
		must(t, os.Mkdir(path, 0700))
	}
	must(t, os.WriteFile(filepath.Join(o.AssetsDir, "index.html"), []byte("<!doctype html><title>Supplied assets</title>"), 0600))
	bin, err := filepath.Abs(os.Args[0])
	must(t, err)
	o.CodexBin, o.JpackBin = bin, bin
	return o
}

func TestPrivatePathsAndBoundedEvidence(t *testing.T) {
	o := pathFixture(t)
	root, err := preparePaths(o)
	must(t, err)
	defer root.Close()
	if writePrivate(root, "oversized.json", strings.Repeat("x", 65537)) == nil {
		t.Fatal("truncated oversized evidence")
	}
	must(t, os.WriteFile(filepath.Join(o.ControlDir, "input.json"), []byte(`{}`), 0644))
	if _, err := readPrivate(root, "input.json"); err == nil {
		t.Fatal("accepted public control")
	}
	must(t, os.Chmod(filepath.Join(o.ControlDir, "input.json"), 0600))
	must(t, os.Symlink("input.json", filepath.Join(o.ControlDir, "link.json")))
	if _, err := readPrivate(root, "link.json"); err == nil {
		t.Fatal("accepted symlink control")
	}
	if _, err := preparePaths(o); err == nil {
		t.Fatal("adopted nonempty control directory")
	}
	bad := pathFixture(t)
	bad.ConfigDir = bad.ProjectDir
	if _, err := preparePaths(bad); err == nil {
		t.Fatal("accepted shared private roots")
	}
	linked := pathFixture(t)
	must(t, os.Symlink(linked.ConfigDir, filepath.Join(linked.AssetsDir, "outside")))
	if _, err := preparePaths(linked); err == nil {
		t.Fatal("accepted asset symlink")
	}
}

func TestRealHTTPServerKeepsNormalAuthAndClosesOnAbort(t *testing.T) {
	o := pathFixture(t)
	root, err := preparePaths(o)
	must(t, err)
	defer root.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- serve(ctx, o, root) }()
	var descriptor struct {
		BaseURL    string `json:"baseUrl"`
		SetupToken string `json:"setupToken"`
	}
	must(t, json.Unmarshal(waitFile(t, filepath.Join(o.ControlDir, "owner.json")), &descriptor))
	if descriptor.SetupToken == "" || !strings.HasPrefix(descriptor.BaseURL, "http://127.0.0.1:") {
		t.Fatal("missing private launch descriptor")
	}
	client := &http.Client{Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get(descriptor.BaseURL + "/api/conversations")
	must(t, err)
	response.Body.Close()
	if response.StatusCode == http.StatusOK {
		t.Fatal("owner bypassed normal browser authentication")
	}
	info, err := os.Stat(filepath.Join(o.ControlDir, "owner.json"))
	must(t, err)
	if info.Mode().Perm() != 0600 {
		t.Fatal("launch secret not private")
	}
	must(t, writePrivate(root, "abort.json", command{Action: "abort"}))
	select {
	case err := <-result:
		if !errors.Is(err, errControl) {
			t.Fatalf("abort outcome: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("real owner did not close")
	}
	var evidence lifecycleEvidence
	must(t, json.Unmarshal(waitFile(t, filepath.Join(o.ControlDir, "lifecycle.json")), &evidence))
	if evidence.Reason != "driver_aborted" || !evidence.NormalOwnerShutdown || evidence.Cases[0].Accounting != nil {
		t.Fatalf("bad real owner evidence %+v", evidence)
	}
	if response, err := client.Get(descriptor.BaseURL); err == nil {
		response.Body.Close()
		t.Fatal("real HTTP listener remained open")
	}
}

func TestSuppliedPeerUsesActualManagerReapAndSequence(t *testing.T) {
	base := t.TempDir()
	config, project := filepath.Join(base, "config"), filepath.Join(base, "project")
	must(t, os.Mkdir(config, 0700))
	must(t, os.Mkdir(project, 0700))
	bin, err := filepath.Abs(os.Args[0])
	must(t, err)
	sink := codexbridge.NewAccountingSink()
	manager, err := codexbridge.NewManager(codexbridge.Options{Binary: bin, ProfileDir: filepath.Join(config, "native"), ProjectDir: project, Accounting: sink})
	must(t, err)
	defer manager.Close()
	h := newOwner(testOptions(), sink.Reports(), sink.Dropped, func() error { manager.Close(); return nil })
	defer func() { h.close("test_cleanup"); h.finish() }()
	for number := 1; number <= 2; number++ {
		chat := fmt.Sprintf("chat-%d", number)
		must(t, h.begin(command{Action: "begin", Case: number, ChatID: chat}))
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err = manager.Run(ctx, codexbridge.Owner{ID: "supplied-session", Session: ctx, Policy: ctx}, codexbridge.RunRequest{Model: "gpt-6-sol", Effort: "medium", Prompt: "supplied fixed input"}, func(codexbridge.RunEvent) error { return nil }, func(context.Context, codexbridge.ToolCall) (codexbridge.ToolAnswer, error) {
			return codexbridge.ToolAnswer{}, errors.New("unexpected supplied-peer callback")
		})
		cancel()
		must(t, err)
		must(t, h.drain())
		r := h.cases[number-1].Accounting
		if r == nil || r.Invocation != uint64(number) || !r.OwnedClientReaped || r.Incomplete {
			t.Fatalf("actual manager report %+v", r)
		}
		raw, err := os.ReadFile(filepath.Join(config, "native", "tmp", "owned-peer.pid"))
		must(t, err)
		pid, err := strconv.Atoi(string(raw))
		must(t, err)
		if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
			t.Fatalf("exact supplied child not reaped: %v", err)
		}
		must(t, h.complete(command{Action: "complete", Case: number, ChatID: chat, ThreadID: r.ThreadID, TurnID: r.TurnID}))
	}
	h.close("completed")
	if evidence := h.finish(); !evidence.NormalOwnerShutdown || evidence.Dropped != 0 {
		t.Fatalf("bad owner completion %+v", evidence)
	}
}

func TestDeadlineClosesActualManagerAndReapsStalledSuppliedPeer(t *testing.T) {
	base := t.TempDir()
	config, project := filepath.Join(base, "config"), filepath.Join(base, "project")
	must(t, os.Mkdir(config, 0700))
	must(t, os.Mkdir(project, 0700))
	bin, err := filepath.Abs(os.Args[0])
	must(t, err)
	sink := codexbridge.NewAccountingSink()
	manager, err := codexbridge.NewManager(codexbridge.Options{Binary: bin, ProfileDir: filepath.Join(config, "native"), ProjectDir: project, Accounting: sink})
	must(t, err)
	defer manager.Close()
	h := newOwner(testOptions(), sink.Reports(), sink.Dropped, func() error { manager.Close(); return nil })
	defer func() { h.close("test_cleanup"); h.finish() }()
	h.caseDuration = 300 * time.Millisecond
	must(t, h.begin(command{Action: "begin", Case: 1, ChatID: "stalled"}))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- manager.Run(ctx, codexbridge.Owner{ID: "supplied-session", Session: ctx, Policy: ctx}, codexbridge.RunRequest{Model: "gpt-6-sol", Effort: "medium", Prompt: "stall"}, func(codexbridge.RunEvent) error { return nil }, func(context.Context, codexbridge.ToolCall) (codexbridge.ToolAnswer, error) {
			return codexbridge.ToolAnswer{}, errControl
		})
	}()
	raw := waitFile(t, filepath.Join(config, "native", "tmp", "owned-peer.pid"))
	pid, err := strconv.Atoi(string(raw))
	must(t, err)
	select {
	case <-h.closed:
	case <-time.After(time.Second):
		t.Fatal("deadline did not close actual Manager")
	}
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("stalled peer incorrectly succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("actual Manager Run did not return after close")
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("stalled exact child remained: %v", err)
	}
	evidence := h.finish()
	if evidence.Reason != "case_deadline" || !evidence.NormalOwnerShutdown || evidence.Cases[0].Accounting == nil || !evidence.Cases[0].Accounting.OwnedClientReaped || !evidence.Cases[0].Accounting.Incomplete || evidence.Cases[1].ChatID != "" {
		t.Fatalf("bad stalled lifecycle %+v", evidence)
	}
}
