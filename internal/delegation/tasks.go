// Package delegation schedules bounded independent work. Callers own durable
// storage, permissions, budget admission and result publication. No provider or
// workflow-specific types belong here.
package delegation

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type Policy struct {
	Approach string `json:"approach"`
	// Zero selects the supported connection ceiling, not an unlimited pool.
	Concurrency int `json:"concurrency"`
}

func (p Policy) Validate() error {
	if (p.Approach != "standard" && p.Approach != "delegated") || p.Concurrency < 0 || p.Concurrency > 2 {
		return errors.New("Choose Standard or Delegated, with Auto, one or two concurrent AI tasks.")
	}
	return nil
}

type Execution struct {
	Approach        string `json:"approach"`
	Requested       int    `json:"requestedConcurrency"`
	Concurrency     int    `json:"concurrency"`
	ConnectionLimit int    `json:"connectionLimit"`
}

func Resolve(p *Policy, connectionLimit int) Execution {
	e := Execution{Approach: "standard", Concurrency: 1, ConnectionLimit: max(1, min(2, connectionLimit))}
	if p == nil {
		return e
	}
	e.Approach = p.Approach
	e.Requested = p.Concurrency
	if p.Approach == "delegated" {
		e.Concurrency = e.ConnectionLimit
		if p.Concurrency > 0 {
			e.Concurrency = min(e.Concurrency, p.Concurrency)
		}
	}
	return e
}

type Task struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Objective  string `json:"objective"`
	State      string `json:"state"`
	CreatedAt  string `json:"createdAt"`
	StartedAt  string `json:"startedAt,omitempty"`
	FinishedAt string `json:"finishedAt,omitempty"`
	Problem    string `json:"problem,omitempty"`
}

func (t Task) Validate() error {
	if t.ID == "" || len(t.ID) > 128 || t.Title == "" || len(t.Title) > 512 || t.Objective == "" || len(t.Objective) > 2000 || len(t.Problem) > 4000 {
		return errors.New("Invalid delegated task.")
	}
	switch t.State {
	case "queued", "running", "complete", "failed", "interrupted":
	default:
		return errors.New("Invalid delegated task state.")
	}
	for _, v := range []string{t.CreatedAt, t.StartedAt, t.FinishedAt} {
		if v != "" {
			if _, err := time.Parse(time.RFC3339Nano, v); err != nil {
				return errors.New("Invalid delegated task time.")
			}
		}
	}
	return nil
}

// Fatal prevents further dispatch, but joins in-flight work so durable writes
// cannot arrive after the owning workflow has finalized.
type Fatal struct{ Err error }

func (e *Fatal) Error() string { return e.Err.Error() }
func (e *Fatal) Unwrap() error { return e.Err }

type Hooks struct {
	// Transition commits before execution and after each terminal outcome.
	Transition func(context.Context, Task) error
	Execute    func(context.Context, Task) error
}

func Run(ctx context.Context, tasks []Task, concurrency int, h Hooks) error {
	if concurrency < 1 || concurrency > 2 || len(tasks) > 100 || h.Transition == nil || h.Execute == nil {
		return errors.New("Invalid delegated task batch.")
	}
	seen := map[string]bool{}
	for _, t := range tasks {
		if err := t.Validate(); err != nil {
			return err
		}
		if t.State != "queued" || seen[t.ID] {
			return errors.New("Only distinct queued tasks may be dispatched.")
		}
		seen[t.ID] = true
	}
	batch, cancel := context.WithCancel(ctx)
	defer cancel()
	var mu sync.Mutex
	next := 0
	var fatal error
	fail := func(err error) {
		mu.Lock()
		if fatal == nil {
			fatal = err
		}
		mu.Unlock()
		cancel()
	}
	var wg sync.WaitGroup
	for i := 0; i < min(concurrency, len(tasks)); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				mu.Lock()
				if next >= len(tasks) || batch.Err() != nil {
					mu.Unlock()
					return
				}
				t := tasks[next]
				next++
				mu.Unlock()
				t.State = "running"
				t.StartedAt = time.Now().UTC().Format(time.RFC3339Nano)
				if err := h.Transition(batch, t); err != nil {
					fail(err)
					return
				}
				err := h.Execute(batch, t)
				t.State = "complete"
				if err != nil {
					t.State = "failed"
					t.Problem = err.Error()
					if len(t.Problem) > 4000 {
						t.Problem = "The task failed; see its saved operations for details."
					}
				}
				if batch.Err() != nil {
					t.State = "interrupted"
					if t.Problem == "" {
						t.Problem = "Stopped before the task completed."
					}
				}
				t.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
				// Cancellation does not cancel the final local checkpoint.
				if e := h.Transition(context.WithoutCancel(ctx), t); e != nil {
					fail(e)
					return
				}
				var terminal *Fatal
				if errors.As(err, &terminal) {
					fail(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	for ; next < len(tasks); next++ {
		t := tasks[next]
		t.State = "interrupted"
		t.Problem = "Stopped before the task started."
		t.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if e := h.Transition(context.WithoutCancel(ctx), t); e != nil && fatal == nil {
			fatal = e
		}
	}
	if fatal != nil {
		return fmt.Errorf("Delegated work stopped: %w", fatal)
	}
	return ctx.Err()
}
