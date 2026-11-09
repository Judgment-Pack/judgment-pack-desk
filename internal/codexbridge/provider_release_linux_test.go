package codexbridge

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunProviderReleaseOwnsAdmissionAndReapedProcess(t *testing.T) {
	for _, mode := range []string{"normal", "hang", "bad-start", "failed", "models-duplicate", "signed-out", "admission", "generation"} {
		t.Run(mode, func(t *testing.T) {
			m := runManager(t, mode)
			owner, a, b := ownerFixture()
			defer a()
			defer b()
			if mode == "signed-out" {
				if err := os.Remove(filepath.Join(m.profile.codex, "account.fixture")); err != nil {
					t.Fatal(err)
				}
			}
			// Reuse an idle discovery client where possible, so early refusal must
			// release that exact retained client as well as newly launched processes.
			var process *client
			launch := m.launch
			m.launch = func(ctx context.Context, cmd *exec.Cmd) (*client, error) {
				c, err := launch(ctx, cmd)
				process = c
				return c, err
			}
			if mode == "admission" {
				if _, err := m.Models(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			var released atomic.Int32
			request := runRequest()
			request.ProviderReleased = func() {
				released.Add(1)
				if process != nil {
					select {
					case <-process.exited:
					default:
						t.Error("acknowledged before reap")
					}
					if process.cmd.ProcessState == nil {
						t.Error("acknowledged before cmd.Wait")
					}
				}
				if !m.mu.TryLock() {
					t.Error("acknowledged with admission lock retained")
					return
				}
				defer m.mu.Unlock()
				if m.active != nil || m.client != nil {
					t.Error("acknowledged retained invocation")
				}
			}
			refused := errors.New("host fence")
			if mode == "admission" {
				request.AuthorizeAdmission = func(context.Context) error { return refused }
			}
			if mode == "generation" {
				request.BeforeGeneration = func(context.Context) error { return refused }
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if mode == "hang" {
				ctx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
				defer cancel()
			}
			err := m.Run(ctx, owner, request, func(RunEvent) error {
				if released.Load() != 0 {
					t.Error("released during events")
				}
				return nil
			}, func(context.Context, ToolCall) (ToolAnswer, error) {
				if released.Load() != 0 {
					t.Error("released during tool")
				}
				return ToolAnswer{Text: "done"}, nil
			})
			if mode == "normal" && err != nil || mode != "normal" && err == nil {
				t.Fatalf("unexpected outcome %v", err)
			}
			if released.Load() != 1 {
				t.Fatalf("release count %d", released.Load())
			}
		})
	}
}

func TestRunProviderReleaseWaitsForExactReapAndBusyDoesNotReleaseOwner(t *testing.T) {
	m := runManager(t, "normal")
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
		// startClient has completed initialize, so the scanner's deferred close
		// already retained actual. Extend the owned reap barrier deterministically.
		c.exited = make(chan struct{})
		go func() { <-actual; close(reaped); <-allow; close(c.exited) }()
		return c, nil
	}
	var releases atomic.Int32
	request := runRequest()
	request.ProviderReleased = func() { releases.Add(1) }
	done := make(chan error, 1)
	go func() {
		done <- m.Run(context.Background(), owner, request, func(RunEvent) error { return nil }, func(context.Context, ToolCall) (ToolAnswer, error) { return ToolAnswer{Text: "done"}, nil })
	}()
	select {
	case <-reaped:
	case <-time.After(3 * time.Second):
		t.Fatal("process not reaped")
	}
	if releases.Load() != 0 {
		t.Fatal("release preceded reap barrier")
	}
	busy := runRequest()
	busy.ProviderReleased = func() { t.Error("busy invocation acknowledged another owner") }
	if err := m.Run(context.Background(), owner, busy, func(RunEvent) error { return nil }, func(context.Context, ToolCall) (ToolAnswer, error) {
		t.Error("busy tool dispatched")
		return ToolAnswer{}, nil
	}); !errors.Is(err, ErrBusy) {
		t.Fatalf("busy %v", err)
	}
	select {
	case err := <-done:
		t.Fatalf("run returned before release %v", err)
	default:
	}
	close(allow)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("release did not finish")
	}
	if releases.Load() != 1 {
		t.Fatalf("release count %d", releases.Load())
	}
}
