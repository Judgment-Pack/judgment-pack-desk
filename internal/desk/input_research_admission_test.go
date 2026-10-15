package desk

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestResearchAdmissionRetainsWaitingAndOneModelRequest(t *testing.T) {
	for _, outcome := range []string{"completed", "lost-response", "wait-timeout"} {
		t.Run(outcome, func(t *testing.T) {
			root, run := researchRunFixture(t)
			ops := recoveryOperations()
			calls := 0
			ops.model = func(_ context.Context, _ inputResearchAI, _ string, availability func(bool) error) (json.RawMessage, error) {
				calls++
				if err := availability(true); err != nil {
					t.Fatal(err)
				}
				saved, err := readInputResearch(root)
				if err != nil {
					t.Fatal(err)
				}
				current := researchSummary(saved).CurrentStep
				if current == nil || current.State != "waiting" {
					t.Fatal("no retained waiting status")
				}
				prompt, _ := researchPrompt(saved)
				var usage struct {
					Used map[string]int `json:"requestsUsed"`
				}
				_ = json.Unmarshal([]byte(prompt), &usage)
				if usage.Used["model"] != 0 {
					t.Fatal("waiting consumed a request")
				}
				recovered := saved
				recovered.Attempts = append([]inputResearchAttempt(nil), saved.Attempts...)
				recoverInputResearch(&recovered)
				if recovered.State != "interrupted" || recovered.Attempts[0].State != "not-started" {
					t.Fatal("process recovery invented an uncertain request")
				}
				if outcome == "wait-timeout" {
					return nil, context.DeadlineExceeded
				}
				if err := availability(false); err != nil {
					t.Fatal(err)
				}
				saved, _ = readInputResearch(root)
				if saved.Attempts[0].State != "started" {
					t.Fatal("dispatch not journaled")
				}
				if outcome == "lost-response" {
					return nil, errors.New("response lost after admission")
				}
				return recoveryAction("finish", map[string]any{}), nil
			}
			executeInputResearch(context.Background(), fileResearchStore(root), run, ops)
			saved, err := readInputResearch(root)
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 || len(saved.Attempts) != 1 {
				t.Fatal("model operation replayed")
			}
			want := "completed"
			if outcome == "lost-response" {
				want = "uncertain"
			}
			if outcome == "wait-timeout" {
				want = "not-started"
			}
			if saved.Attempts[0].State != want {
				t.Fatalf("got %s, want %s", saved.Attempts[0].State, want)
			}
			if outcome == "wait-timeout" && !strings.Contains(saved.Problem, "No AI request was sent.") {
				t.Fatal("missing no-dispatch explanation")
			}
			if saved.PendingRetry != nil {
				t.Fatal("model wait entered source retry policy")
			}
		})
	}
}

func TestResearchStopWhileWaitingNeverDispatches(t *testing.T) {
	root, run := researchRunFixture(t)
	ops := recoveryOperations()
	waiting, done := make(chan struct{}), make(chan struct{})
	ops.model = func(ctx context.Context, _ inputResearchAI, _ string, availability func(bool) error) (json.RawMessage, error) {
		if err := availability(true); err != nil {
			return nil, err
		}
		close(waiting)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	go func() {
		defer close(done)
		executeInputResearch(context.Background(), fileResearchStore(root), run, ops)
	}()
	select {
	case <-waiting:
	case <-time.After(3 * time.Second):
		t.Fatal("no waiting operation")
	}
	if _, err := changeInputResearch(root, func(r *inputResearchRun) error { r.StopRequested = true; return nil }); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop did not interrupt waiting")
	}
	saved, err := readInputResearch(root)
	if err != nil {
		t.Fatal(err)
	}
	if saved.State != "stopped" || len(saved.Attempts) != 1 || saved.Attempts[0].State != "not-started" {
		t.Fatal("stopped wait was marked dispatched")
	}
	if researchSummary(saved).CurrentStep != nil {
		t.Fatal("stopped wait remains active")
	}
}
