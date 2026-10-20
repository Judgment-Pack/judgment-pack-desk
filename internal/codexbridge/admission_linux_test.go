package codexbridge

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestResearchRunWaitsForAdmissionWithoutReplaying(t *testing.T) {
	for _, busy := range []string{"status-lock", "active-run"} {
		t.Run(busy, func(t *testing.T) {
			m := runManager(t, "normal")
			owner, a, b := ownerFixture()
			defer a()
			defer b()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var release func()
			if busy == "status-lock" {
				m.mu.Lock()
				release = m.mu.Unlock
			} else {
				entered, unblock, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
				go func() {
					done <- m.Run(ctx, owner, runRequest(), func(RunEvent) error { return nil }, func(context.Context, ToolCall) (ToolAnswer, error) {
						close(entered)
						<-unblock
						return ToolAnswer{Text: "done"}, nil
					})
				}()
				select {
				case <-entered:
				case <-ctx.Done():
					t.Fatal("first run never started")
				}
				release = func() {
					close(unblock)
					if err := <-done; err != nil {
						t.Fatal(err)
					}
				}
			}
			waiting, done := make(chan struct{}), make(chan error, 1)
			var admitted, calls atomic.Int32
			request := runRequest()
			request.WaitForAvailability = true
			go func() {
				done <- m.Run(ctx, owner, request, func(e RunEvent) error {
					if e.Type == "waiting" {
						close(waiting)
					}
					if e.Type == "admitted" {
						admitted.Add(1)
					}
					return nil
				}, func(context.Context, ToolCall) (ToolAnswer, error) {
					calls.Add(1)
					return ToolAnswer{Text: "done"}, nil
				})
			}()
			select {
			case <-waiting:
			case <-ctx.Done():
				release()
				t.Fatal("no waiting event")
			}
			if calls.Load() != 0 || admitted.Load() != 0 {
				release()
				t.Fatal("dispatched while busy")
			}
			release()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("waiting run did not proceed")
			}
			if calls.Load() != 1 || admitted.Load() != 1 {
				t.Fatalf("unexpected dispatch count %d/%d", calls.Load(), admitted.Load())
			}
		})
	}
}

func TestResearchAdmissionCancellationAndJournalFailureSendNothing(t *testing.T) {
	for _, reason := range []string{"cancel", "session", "policy", "deadline", "journal"} {
		t.Run(reason, func(t *testing.T) {
			m := runManager(t, "normal")
			owner, a, b := ownerFixture()
			defer a()
			defer b()
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			request := runRequest()
			request.WaitForAvailability = true
			waiting, done := make(chan struct{}), make(chan error, 1)
			var calls atomic.Int32
			m.mu.Lock()
			go func() {
				done <- m.Run(ctx, owner, request, func(e RunEvent) error {
					if e.Type == "waiting" {
						close(waiting)
					}
					if e.Type == "admitted" && reason == "journal" {
						return errors.New("journal unavailable")
					}
					return nil
				}, func(context.Context, ToolCall) (ToolAnswer, error) { calls.Add(1); return ToolAnswer{}, nil })
			}()
			<-waiting
			switch reason {
			case "cancel":
				cancel()
			case "session":
				a()
			case "policy":
				b()
			case "journal":
				m.mu.Unlock()
			}
			select {
			case err := <-done:
				if err == nil {
					t.Error("canceled request succeeded")
				}
			case <-time.After(2 * time.Second):
				t.Error("wait ignored cancellation")
			}
			if reason != "journal" {
				m.mu.Unlock()
			}
			if calls.Load() != 0 {
				t.Fatal("request dispatched")
			}
			m.mu.Lock()
			active := m.active
			m.mu.Unlock()
			if active != nil {
				t.Fatal("owner leaked")
			}
		})
	}
}

func TestRunHostFencesAtPhysicalAdmissionAndGeneration(t *testing.T) {
	for _, boundary := range []string{"admission", "generation"} {
		t.Run(boundary, func(t *testing.T) {
			m := runManager(t, "normal")
			owner, a, b := ownerFixture()
			defer a()
			defer b()
			var admitted, generation, calls atomic.Int32
			refused := errors.New("host authority changed")
			request := runRequest()
			request.AuthorizeAdmission = func(ctx context.Context) error {
				admitted.Add(1)
				if ctx.Err() != nil {
					t.Error("admission got an expired context")
				}
				if boundary == "admission" {
					return refused
				}
				return nil
			}
			request.BeforeGeneration = func(context.Context) error { generation.Add(1); return refused }
			err := m.Run(context.Background(), owner, request, func(RunEvent) error { calls.Add(1); return nil }, func(context.Context, ToolCall) (ToolAnswer, error) { calls.Add(1); return ToolAnswer{}, nil })
			if !errors.Is(err, refused) || admitted.Load() != 1 || calls.Load() != 0 || boundary == "admission" && generation.Load() != 0 || boundary == "generation" && generation.Load() != 1 {
				t.Fatalf("host fence bypassed: %v %d/%d/%d", err, admitted.Load(), generation.Load(), calls.Load())
			}
			// Each refusal releases the same lock/process needed by a normal run.
			if err = m.Run(context.Background(), owner, runRequest(), func(RunEvent) error { return nil }, func(context.Context, ToolCall) (ToolAnswer, error) { return ToolAnswer{Text: "done"}, nil }); err != nil {
				t.Fatal("host refusal retained account", err)
			}
		})
	}
}
