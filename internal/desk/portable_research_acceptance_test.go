package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// Wire replies for the three API families the shared executor supports.
func portableFamilyReply(t *testing.T, family string, w http.ResponseWriter, answer string) {
	t.Helper()
	switch family {
	case "anthropic":
		_ = json.NewEncoder(w).Encode(map[string]any{"stop_reason": "end_turn", "content": []any{map[string]any{"type": "text", "text": answer}}})
	case "gemini":
		_ = json.NewEncoder(w).Encode(map[string]any{"candidates": []any{map[string]any{"finishReason": "STOP", "content": map[string]any{"parts": []any{map[string]any{"text": answer}}}}}})
	default:
		portableTestReply(w, answer)
	}
}

// API-family conformance for fresh portable research: every family reaches the
// provider once through the Gateway adapter, and the Runner replays the real
// receipt offline without spending another request.
func TestPortableResearchAcrossAPIFamiliesWithRealReceipts(t *testing.T) {
	for _, family := range []string{"openai-compatible", "anthropic", "gemini"} {
		t.Run(family, func(t *testing.T) {
			var calls atomic.Int32
			s, _, run := portableResearchFixtureFor(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				wantPath := map[string]string{"openai-compatible": "/chat/completions", "anthropic": "/v1/messages", "gemini": "/v1beta/models/shared-model:generateContent"}[family]
				if r.URL.Path != wantPath {
					t.Errorf("wrong %s route %s", family, r.URL.Path)
				}
				portableFamilyReply(t, family, w, portableProcurementPreset.Reply)
			}, portableProcurementPreset, family)
			store := portableResearchStoreFixture(t, run, true)
			if _, err := store.change(func(r *inputResearchRun) error { r.State = "running"; return nil }); err != nil {
				t.Fatal(err)
			}
			s.executePortableResearch(t.Context(), store, run)
			held, err := store.read()
			if err != nil || held.State != "completed" || calls.Load() != 1 || len(held.Attempts) != 1 {
				t.Fatalf("not completed: %s %s calls=%d %v", held.State, held.Problem, calls.Load(), err)
			}
			input, err := portableResearchInputWithResponse(held, held.Portable.Response)
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				result, err := s.inputRunner(t.Context(), "inputs/preview", input, "")
				if err != nil || !bytes.Contains(result, []byte(portableProcurementPreset.Want)) || !bytes.Contains(result, []byte(`"generatedInfluence":true`)) {
					t.Fatalf("replay failed: %s %v", result, err)
				}
			}
			if calls.Load() != 1 {
				t.Fatal("replay spent a request")
			}
		})
	}
}

// A connection edited after review is a different binding: the worker refuses
// before the model is asked and keeps the frozen review as it was.
func TestPortableResearchStaleConnectionBindingRefusedBeforeAcquisition(t *testing.T) {
	var calls atomic.Int32
	s, ts, run := portableResearchFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		portableTestReply(w, portableProcurementPreset.Reply)
	})
	registry, err := s.readAIRegistry()
	if err != nil {
		t.Fatal(err)
	}
	registry.Connections[0].Assistant.Endpoint.Models = []string{"shared-model", "another-model"}
	next := saveTestAIRegistry(t, s, ts, registry.aiRegistry)
	if next.Connections[0].Revision == run.Portable.Frozen.Choice.Revision {
		t.Fatal("fixture did not change the connection revision")
	}
	store := portableResearchStoreFixture(t, run, false)
	if _, err = store.change(func(r *inputResearchRun) error { r.State = "running"; return nil }); err != nil {
		t.Fatal(err)
	}
	s.executePortableResearch(t.Context(), store, run)
	held, err := store.read()
	if err != nil || held.State != "blocked" || held.Problem == "" || calls.Load() != 0 || len(held.Attempts) != 0 || len(held.Portable.Response) != 0 {
		t.Fatalf("stale binding was not refused: state=%s problem=%q calls=%d attempts=%d %v", held.State, held.Problem, calls.Load(), len(held.Attempts), err)
	}
	if held.Portable.Frozen.Choice.Revision != run.Portable.Frozen.Choice.Revision {
		t.Fatal("frozen connection revision was rewritten")
	}
}

// A worker cancelled or a Stop saved while the provider call is in flight ends
// the run without a result; the completed acquisition before it survives, the
// in-flight one is uncertain, and resuming never replays it.
func TestPortableResearchInterruptedCallKeepsCompletedAndNeverReplays(t *testing.T) {
	for _, stop := range []bool{false, true} {
		t.Run(map[bool]string{false: "worker cancelled", true: "stop saved"}[stop], func(t *testing.T) {
			var calls atomic.Int32
			started, provider := make(chan struct{}), make(chan struct{})
			s, _, run := portableResearchFixture(t, func(w http.ResponseWriter, r *http.Request) {
				// Read the request so the server notices a client disconnect.
				_, _ = io.Copy(io.Discard, r.Body)
				switch calls.Add(1) {
				case 1:
					// Wrong type: a bounded correction, so one acquisition completes first.
					portableTestReply(w, `{"kind":"answer","fields":{"eligible":{"status":"found","value":"false","cites":[0]}}}`)
				case 2:
					close(started)
					select {
					case <-r.Context().Done():
						close(provider)
					case <-time.After(20 * time.Second):
					}
				default:
					portableTestReply(w, portableProcurementPreset.Reply)
				}
			})
			store := portableResearchStoreFixture(t, run, false)
			if _, err := store.change(func(r *inputResearchRun) error { r.State = "running"; return nil }); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan struct{})
			go func() { defer close(done); s.executePortableResearch(ctx, store, run) }()
			select {
			case <-started:
			case <-time.After(30 * time.Second):
				t.Fatal("second request never started")
			}
			if stop {
				if _, err := store.change(func(r *inputResearchRun) error { r.StopRequested = true; return nil }); err != nil {
					t.Fatal(err)
				}
			} else {
				cancel()
			}
			select {
			case <-done:
			case <-time.After(30 * time.Second):
				t.Fatal("worker ignored cancellation")
			}
			select {
			case <-provider:
			case <-time.After(10 * time.Second):
				t.Fatal("cancellation did not reach the provider request")
			}
			want := map[bool]string{false: "interrupted", true: "stopped"}[stop]
			held, err := store.read()
			if err != nil || held.State != want || len(held.Portable.Response) != 0 || len(held.Attempts) != 2 || held.Attempts[0].State != "completed" {
				t.Fatalf("wrong interrupted state %s %q attempts=%+v %v", held.State, held.Problem, held.Attempts, err)
			}
			held, err = store.change(func(r *inputResearchRun) error { recoverInputResearch(r); return nil })
			if err != nil || held.Attempts[1].State != "uncertain" || held.Attempts[0].State != "completed" {
				t.Fatalf("recovery lost the call states: %+v %v", held.Attempts, err)
			}
			responses, err := portableResearchResponses(held)
			if err != nil || len(responses) != 1 {
				t.Fatal("completed acquisition did not survive interruption", err)
			}
			// Confirming again cannot queue the run: the uncertain request is not repeated.
			if _, err = queuePortableResearch(t.Context(), store.journal, run.Revision); err == nil {
				t.Fatalf("interrupted research was queued again: %v", err)
			}
			after, err := store.read()
			if err != nil || calls.Load() != 2 || after.State != want || len(after.Portable.Response) != 0 || len(after.Attempts) != 2 || after.Attempts[1].State != "uncertain" {
				t.Fatalf("interrupted run changed: state=%s calls=%d attempts=%+v %v", after.State, calls.Load(), after.Attempts, err)
			}
		})
	}
}
