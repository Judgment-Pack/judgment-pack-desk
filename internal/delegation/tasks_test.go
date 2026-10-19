package delegation

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunBoundsConcurrencyAndKeepsSiblings(t *testing.T) {
	tasks := []Task{{ID: "a", Title: "A", Objective: "A", State: "queued"}, {ID: "b", Title: "B", Objective: "B", State: "queued"}, {ID: "c", Title: "C", Objective: "C", State: "queued"}}
	var mu sync.Mutex
	states := map[string]string{}
	var active, peak atomic.Int32
	err := Run(context.Background(), tasks, 2, Hooks{Transition: func(_ context.Context, t Task) error { mu.Lock(); states[t.ID] = t.State; mu.Unlock(); return nil }, Execute: func(_ context.Context, t Task) error {
		mu.Lock()
		state := states[t.ID]
		mu.Unlock()
		if state != "running" {
			return &Fatal{Err: errors.New("executed before checkpoint")}
		}
		n := active.Add(1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		defer active.Add(-1)
		time.Sleep(20 * time.Millisecond)
		if t.ID == "b" {
			return errors.New("task failed")
		}
		return nil
	}})
	if err != nil || peak.Load() != 2 || states["a"] != "complete" || states["b"] != "failed" || states["c"] != "complete" {
		t.Fatalf("%v %v %d", err, states, peak.Load())
	}
}
func TestRunCheckpointFailureAndCancellation(t *testing.T) {
	task := Task{ID: "a", Title: "A", Objective: "A", State: "queued"}
	calls := 0
	err := Run(context.Background(), []Task{task}, 1, Hooks{Transition: func(context.Context, Task) error { return errors.New("disk unavailable") }, Execute: func(context.Context, Task) error { calls++; return nil }})
	if err == nil || calls != 0 {
		t.Fatal("dispatched without saved intent")
	}
	ctx, cancel := context.WithCancel(context.Background())
	states := map[string]string{}
	err = Run(ctx, []Task{task, {ID: "b", Title: "B", Objective: "B", State: "queued"}}, 1, Hooks{Transition: func(_ context.Context, t Task) error { states[t.ID] = t.State; return nil }, Execute: func(context.Context, Task) error { cancel(); return ctx.Err() }})
	if !errors.Is(err, context.Canceled) || states["a"] != "interrupted" || states["b"] != "interrupted" {
		t.Fatalf("%v %v", err, states)
	}
}
func TestResolveExecution(t *testing.T) {
	for _, c := range []struct {
		policy      *Policy
		limit, want int
	}{{nil, 2, 1}, {&Policy{"delegated", 0}, 2, 2}, {&Policy{"delegated", 0}, 1, 1}, {&Policy{"delegated", 2}, 1, 1}, {&Policy{"delegated", 1}, 2, 1}} {
		if got := Resolve(c.policy, c.limit); got.Concurrency != c.want {
			t.Fatalf("%+v", got)
		}
	}
	if (Policy{"delegated", 3}).Validate() == nil {
		t.Fatal("unbounded concurrency")
	}
}
func TestCapacitySharesBackgroundBudgetAndReservesInteractiveSpace(t *testing.T) {
	var pool Capacity
	ctx := context.Background()
	a, e := pool.Acquire(ctx, "connection")
	if e != nil {
		t.Fatal(e)
	}
	defer a()
	b, e := pool.Acquire(ctx, "connection")
	if e != nil {
		t.Fatal(e)
	}
	defer b()
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, e = pool.Acquire(canceled, "connection"); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	c, ok := pool.Try("connection")
	if !ok {
		t.Fatal("background work consumed interactive capacity")
	}
	defer c()
	d, ok := pool.Try("connection")
	if !ok {
		t.Fatal("missing fourth slot")
	}
	defer d()
	if release, ok := pool.Try("connection"); ok {
		release()
		t.Fatal("connection-wide cap exceeded")
	}
	other, e := pool.Acquire(ctx, "other")
	if e != nil {
		t.Fatal(e)
	}
	other()
}
func TestCapacityThrottleReducesAdmissionWithoutReplaying(t *testing.T) {
	var pool Capacity
	pool.Throttle("connection")
	a, ok := pool.Try("connection")
	if !ok {
		t.Fatal("first slot")
	}
	defer a()
	if b, ok := pool.Try("connection"); ok {
		b()
		t.Fatal("throttled capacity exceeded")
	}
}
