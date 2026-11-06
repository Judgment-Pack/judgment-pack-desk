package codexbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestAccountingPeer(t *testing.T) {
	if os.Getenv("JPS_ACCOUNTING_PEER") != "1" {
		return
	}
	mode := os.Getenv("JPS_ACCOUNTING_MODE")
	trace, err := os.OpenFile(filepath.Join(os.Getenv("TMPDIR"), "accounting-requests"), os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(5)
	}
	defer trace.Close()
	send := func(v any) { _ = json.NewEncoder(os.Stdout).Encode(v) }
	note := func(method, params string) { send(map[string]any{"method": method, "params": json.RawMessage(params)}) }
	finish := func() {
		note("item/completed", `{"threadId":"thread","turnId":"turn","item":{"id":"message","type":"agentMessage","phase":"final_answer","text":"Answer"}}`)
		status := "completed"
		if mode == "failed" || mode == "interrupted" {
			status = mode
		}
		note("turn/completed", `{"threadId":"thread","turn":{"id":"turn","status":"`+status+`","error":"ERROR_SENTINEL"}}`)
		note("thread/tokenUsage/updated", strings.Replace(accountingSecond, `"totalTokens":17`, `"totalTokens":99`, 1))
	}
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		var f frame
		if json.Unmarshal(scan.Bytes(), &f) != nil {
			os.Exit(6)
		}
		_, _ = trace.Write(append(scan.Bytes(), '\n'))
		reply := func(v any) { send(map[string]any{"id": f.ID, "result": v}) }
		switch f.Method {
		case "initialize":
			reply(map[string]string{"userAgent": "jps_desk/0.157.1 (Linux)"})
		case "initialized":
		case "account/read":
			reply(map[string]any{"requiresOpenaiAuth": true, "account": map[string]string{"type": "chatgpt", "planType": "plus", "email": "AUTH_SENTINEL", "accessToken": "AUTH_SENTINEL"}})
		case "model/list":
			var args struct {
				Hidden bool `json:"includeHidden"`
			}
			_ = json.Unmarshal(f.Params, &args)
			if args.Hidden {
				reply(map[string]any{"data": catalogRows()})
			} else {
				reply(map[string]any{"data": []any{map[string]any{"model": "model", "displayName": "Model", "defaultReasoningEffort": "medium", "supportedReasoningEfforts": []any{map[string]string{"reasoningEffort": "medium"}}}}})
			}
		case "thread/start":
			if mode == "bad-response" {
				reply("PRIVATE_SENTINEL")
				continue
			}
			cwd, _ := os.Getwd()
			result := map[string]any{"thread": map[string]string{"id": "thread"}, "model": "model", "modelProvider": "openai", "approvalPolicy": "never", "cwd": cwd}
			if mode != "effort-missing" {
				result["reasoningEffort"] = nil
			}
			if mode == "normal" {
				result["reasoningEffort"] = "medium"
			}
			if mode == "bad-start" {
				result["model"] = "other"
			}
			reply(result)
		case "turn/start":
			reply(map[string]any{"turn": map[string]string{"id": "turn"}})
			if mode != "cancel-before" {
				note("thread/tokenUsage/updated", accountingFirst)
				note("thread/tokenUsage/updated", accountingSecond)
			}
			if mode == "malformed" {
				note("thread/tokenUsage/updated", strings.Replace(accountingFirst, `"inputTokens":2`, `"inputTokens":-1`, 1))
			}
			if mode == "reroute" {
				note("model/rerouted", accountingRerouteFixture)
			}
			if mode == "retry" {
				note("error", `{"threadId":"thread","turnId":"turn","willRetry":true,"error":{"message":"ERROR_SENTINEL"}}`)
			}
			note("item/started", `{"threadId":"thread","turnId":"turn","item":{"id":"reason","type":"reasoning","text":"REASON_SENTINEL"}}`)
			send(map[string]any{"id": "native-rpc", "method": "item/tool/call", "params": map[string]any{"threadId": "thread", "turnId": "turn", "callId": "call", "tool": "desk_runtime_0", "arguments": map[string]bool{"rehearsal": false}}})
		case "":
			finish()
		default:
			os.Exit(7)
		}
	}
	os.Exit(0)
}
func accountingManager(t *testing.T, mode string, sink *AccountingSink) (*Manager, **client) {
	t.Helper()
	m := runManager(t, "normal")
	m.accounting = sink
	var process *client
	m.launch = func(ctx context.Context, native *exec.Cmd) (*client, error) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestAccountingPeer$")
		cmd.Env = append(native.Env, "JPS_ACCOUNTING_PEER=1", "JPS_ACCOUNTING_MODE="+mode)
		cmd.Dir = native.Dir
		c, err := startClient(ctx, cmd)
		process = c
		return c, err
	}
	return m, &process
}
func TestAccountingRunPreservesRequestsEventsToolsAndRelease(t *testing.T) {
	type result struct {
		trace  string
		events []RunEvent
		calls  []string
		err    string
		order  []string
	}
	for _, mode := range []string{"normal", "failed", "interrupted", "reroute", "malformed", "retry", "effort-missing", "bad-start", "bad-response", "cancel-before", "cancel-after"} {
		t.Run(mode, func(t *testing.T) {
			var results [2]result
			for pass := 0; pass < 2; pass++ {
				var sink *AccountingSink
				if pass == 1 {
					sink = NewAccountingSink()
				}
				m, process := accountingManager(t, mode, sink)
				owner, a, b := ownerFixture()
				defer a()
				defer b()
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				request := runRequest()
				request.Effort = "medium"
				request.ProviderReleased = func() {
					results[pass].order = append(results[pass].order, "release")
					if *process == nil || (*process).cmd.ProcessState == nil {
						t.Error("release before cmd.Wait")
					} else {
						select {
						case <-(*process).exited:
						default:
							t.Error("release before reap")
						}
						if err := syscall.Kill((*process).cmd.Process.Pid, 0); !errors.Is(err, syscall.ESRCH) {
							t.Errorf("exact child still present: %v", err)
						}
					}
					if sink != nil && len(sink.reports) != 0 {
						t.Error("report published before ProviderReleased")
					}
				}
				err := m.Run(ctx, owner, request, func(event RunEvent) error {
					event.ID = ""
					results[pass].events = append(results[pass].events, event)
					return nil
				}, func(_ context.Context, call ToolCall) (ToolAnswer, error) {
					results[pass].calls = append(results[pass].calls, call.Name+":"+string(call.Arguments))
					results[pass].order = append(results[pass].order, "tool")
					if strings.HasPrefix(mode, "cancel-") {
						cancel()
						return ToolAnswer{}, nil
					}
					return ToolAnswer{Text: "Verified"}, nil
				})
				results[pass].order = append(results[pass].order, "returned")
				if err != nil {
					results[pass].err = err.Error()
				}
				trace, readErr := os.ReadFile(filepath.Join(m.profile.temp, "accounting-requests"))
				if readErr != nil {
					t.Fatal(readErr)
				}
				results[pass].trace = strings.ReplaceAll(string(trace), m.profile.work, "<work>")
				if sink == nil {
					continue
				}
				select {
				case r := <-sink.Reports():
					if !r.Admitted || !r.OwnedClientReaped || r.TerminalUsageFinality != "unproven" || r.ApplicationSuccess != (err == nil) {
						t.Fatalf("lifecycle facts: %+v", r)
					}
					if mode == "cancel-before" || mode == "bad-start" || mode == "bad-response" {
						if r.UsagePresent {
							t.Fatal("invented missing usage")
						}
					} else if !r.UsagePresent || r.Usage.Total.Total.Value != 17 {
						t.Fatal("lost latest observed partial usage")
					}
					if mode == "normal" && (!r.TerminalObserved || r.Incomplete || r.UsageUpdates != 2) {
						t.Fatalf("successful usage evidence: %+v", r)
					}
					if mode == "failed" || mode == "interrupted" {
						if !r.TerminalObserved || r.TerminalStatus != mode || !r.Incomplete {
							t.Fatal("failed/interrupted terminal lost")
						}
					}
					if mode == "malformed" && !r.Malformed {
						t.Fatal("malformed flag lost")
					}
					if mode == "reroute" && !r.FixedModelIneligible {
						t.Fatal("reroute eligibility lost")
					}
					if mode == "effort-missing" && r.AcknowledgedEffort.State != "absent" {
						t.Fatal("missing effort invented")
					}
					wire, _ := json.Marshal(r)
					if strings.Contains(string(wire), "SENTINEL") {
						t.Fatal("private data escaped")
					}
				default:
					t.Fatal("missing report after return")
				}
			}
			if !reflect.DeepEqual(results[0], results[1]) {
				t.Fatalf("observation changed behavior: off=%+v on=%+v", results[0], results[1])
			}
		})
	}
}
func TestAccountingBusyAndInvalidNeverClaimReap(t *testing.T) {
	sink := NewAccountingSink()
	m, _ := accountingManager(t, "normal", sink)
	owner, a, b := ownerFixture()
	defer a()
	defer b()
	execute := func(context.Context, ToolCall) (ToolAnswer, error) {
		t.Error("unexpected tool")
		return ToolAnswer{}, nil
	}
	emit := func(RunEvent) error { return nil }
	if err := m.Run(context.Background(), Owner{}, runRequest(), emit, execute); !errors.Is(err, ErrRun) {
		t.Fatal(err)
	}
	m.mu.Lock()
	err := m.Run(context.Background(), owner, runRequest(), emit, execute)
	m.mu.Unlock()
	if !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		r := <-sink.Reports()
		if r.Admitted || r.OwnedClientReaped || !r.Incomplete {
			t.Fatal("false release proof")
		}
	}
}
func TestAccountingFullSinkDoesNotChangeRunOrDelayReap(t *testing.T) {
	sink := NewAccountingSink()
	for i := 0; i < 16; i++ {
		sink.tryPublish(AccountingReport{})
	}
	m, process := accountingManager(t, "normal", sink)
	owner, a, b := ownerFixture()
	defer a()
	defer b()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := m.Run(ctx, owner, runRequest(), func(RunEvent) error { return nil }, func(context.Context, ToolCall) (ToolAnswer, error) { return ToolAnswer{Text: "done"}, nil }); err != nil {
		t.Fatal(err)
	}
	if sink.Dropped() != 1 || (*process).cmd.ProcessState == nil || m.client != nil || m.active != nil {
		t.Fatal("full sink changed cleanup")
	}
}

func TestAccountingPublicationWaitsForOwnedReap(t *testing.T) {
	sink := NewAccountingSink()
	m, _ := accountingManager(t, "normal", sink)
	owner, a, b := ownerFixture()
	defer a()
	defer b()
	reaped, allow := make(chan struct{}), make(chan struct{})
	launch := m.launch
	m.launch = func(ctx context.Context, cmd *exec.Cmd) (*client, error) {
		c, err := launch(ctx, cmd)
		if err != nil {
			return c, err
		}
		actual := c.exited
		c.exited = make(chan struct{})
		go func() { <-actual; close(reaped); <-allow; close(c.exited) }()
		return c, nil
	}
	done := make(chan error, 1)
	go func() {
		done <- m.Run(context.Background(), owner, runRequest(), func(RunEvent) error { return nil }, func(context.Context, ToolCall) (ToolAnswer, error) { return ToolAnswer{Text: "done"}, nil })
	}()
	select {
	case <-reaped:
	case <-time.After(3 * time.Second):
		t.Fatal("fake process did not exit")
	}
	select {
	case <-sink.Reports():
		t.Fatal("report preceded exact reap barrier")
	default:
	}
	busy := runRequest()
	busy.ProviderReleased = func() { t.Error("busy invocation released owner") }
	if err := m.Run(context.Background(), owner, busy, func(RunEvent) error { return nil }, func(context.Context, ToolCall) (ToolAnswer, error) { t.Error("busy tool"); return ToolAnswer{}, nil }); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	r := <-sink.Reports()
	if r.Invocation != 2 || r.Admitted || r.OwnedClientReaped {
		t.Fatal("busy publication claimed another owner's reap")
	}
	close(allow)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("run did not finish")
	}
	r = <-sink.Reports()
	if r.Invocation != 1 || !r.OwnedClientReaped {
		t.Fatal("owning report lost reap correlation")
	}
}
