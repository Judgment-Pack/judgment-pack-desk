package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
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

// The canonical background adapter frames the domain JSON with host instructions
// and the response schema. Decode that explicit envelope, never search arbitrary
// instruction text for a JSON object.
func portableCanonicalPrompt(text string) (json.RawMessage, error) {
	const schema = `{"type":"object","properties":{"kind":{"type":"string","enum":["answer","search","page","read_context"]},"arguments":{"type":"object"},"fields":{"type":"object"}},"required":["kind"],"additionalProperties":false}`
	prefix := portableResearchInstructions + "\n\nReturn exactly one JSON object satisfying this host response schema, without markdown:\n" + schema + "\n\n"
	body, ok := strings.CutPrefix(text, prefix)
	if !ok || !json.Valid([]byte(body)) {
		return nil, errors.New("invalid canonical portable prompt envelope")
	}
	var members map[string]json.RawMessage
	if json.Unmarshal([]byte(body), &members) != nil || len(members["request"]) == 0 {
		return nil, errors.New("missing reviewed portable request")
	}
	return json.RawMessage(body), nil
}

func TestPortableCanonicalPromptEnvelope(t *testing.T) {
	const schema = `{"type":"object","properties":{"kind":{"type":"string","enum":["answer","search","page","read_context"]},"arguments":{"type":"object"},"fields":{"type":"object"}},"required":["kind"],"additionalProperties":false}`
	prefix := portableResearchInstructions + "\n\nReturn exactly one JSON object satisfying this host response schema, without markdown:\n" + schema + "\n\n"
	body := `{"request":{"question":"A \"quoted\" request"},"nextResponseIndex":0}`
	raw, err := portableCanonicalPrompt(prefix + body)
	if err != nil || string(raw) != body {
		t.Fatal("canonical prompt changed", err)
	}
	for _, invalid := range []string{body, "untrusted\n\n" + body, prefix + `{}`, prefix + body + `{}`, strings.Replace(prefix, "read_context", "write", 1) + body, prefix + `{"request":`} {
		if _, err := portableCanonicalPrompt(invalid); err == nil {
			t.Errorf("accepted malformed envelope: %q", invalid)
		}
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
				wantPath := map[string]string{"openai-compatible": "/chat/completions", "anthropic": "/v1/messages", "gemini": "/v1beta/models/shared-model:streamGenerateContent"}[family]
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

// Context coverage through the portable worker: a large retained acquisition is
// shortened to the shared fitting budget while the reviewed request and the
// source index stay whole, and a read_context action is answered from the
// retained text without another acquisition.
func TestPortableResearchContextIsFittedAndReadFromRetainedSource(t *testing.T) {
	const head = "RETAINED-HEAD-MARKER"
	var calls atomic.Int32
	var prompts [][]byte
	var mu sync.Mutex
	s, _, run := portableResearchFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role    string
				Content json.RawMessage
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Messages) < 2 || body.Messages[1].Role != "user" {
			t.Errorf("unreadable provider request: decode=%v messages=%d", err, len(body.Messages))
			return
		}
		var parts []struct{ Type, Text string }
		if json.Unmarshal(body.Messages[1].Content, &parts) != nil || len(parts) < 1 || parts[0].Type != "text" {
			t.Error("canonical user text part missing")
			return
		}
		prompt, err := portableCanonicalPrompt(parts[0].Text)
		if err != nil {
			t.Error(err)
			return
		}
		mu.Lock()
		prompts = append(prompts, prompt)
		mu.Unlock()
		if calls.Add(1) == 1 {
			var sent struct {
				Responses []struct {
					SourceID string `json:"sourceId"`
					SHA256   string `json:"sha256"`
				} `json:"retainedResponses"`
			}
			if json.Unmarshal(prompt, &sent) != nil || len(sent.Responses) != 1 {
				t.Error("retained source index missing from the prompt")
				return
			}
			action, _ := json.Marshal(map[string]any{"kind": "read_context", "arguments": map[string]any{"sourceId": sent.Responses[0].SourceID, "sha256": sent.Responses[0].SHA256, "offset": 0}})
			portableTestReply(w, string(action))
			return
		}
		// Not a verifiable answer: the run stops on its bounded corrections.
		portableTestReply(w, `{"kind":"answer","fields":{}}`)
	})
	retained := portableRetainedResponse{Request: json.RawMessage(`{"tool":"page"}`), Result: json.RawMessage(`"` + head + strings.Repeat("x", 400<<10) + `"`), Receipt: json.RawMessage(`{}`), Salts: json.RawMessage(`{}`)}
	retained.Profile.ID, retained.Profile.Digest = "retained", "sha256:"+strings.Repeat("a", 64)
	raw, _ := portableResearchJSON(retained)
	run.Portable.Frozen.Limits.Pages = 1
	run.Revision = portableResearchRevision(run.Portable.Frozen)
	store := portableResearchStoreFixture(t, run, true)
	if _, err := store.change(func(r *inputResearchRun) error {
		r.State = "running"
		attempt, _, err := beginResearchAttempt(r, "page", json.RawMessage(`{"tool":"page"}`))
		if err != nil {
			return err
		}
		return finishResearchAttempt(r, attempt.ID, raw, "")
	}); err != nil {
		t.Fatal(err)
	}
	s.executePortableResearch(t.Context(), store, run)
	if calls.Load() < 2 {
		t.Fatalf("read_context was not followed by another model turn: %d", calls.Load())
	}
	policy, _, err := s.aiContext(run.Portable.Frozen.Choice.Model)
	if err != nil {
		t.Fatal(err)
	}
	budget := policy.InputBytes() - len(portableResearchInstructions) - policy.BriefBytes - policy.MemoryBytes - 4608
	var first, second struct {
		Request  json.RawMessage `json:"request"`
		Note     string          `json:"contextNote"`
		Profiles json.RawMessage `json:"profiles"`
		Sources  []struct{ Text string }
		Section  *struct {
			Text       string `json:"text"`
			TotalBytes int    `json:"totalBytes"`
		} `json:"retainedSection"`
	}
	if json.Unmarshal(prompts[0], &first) != nil || json.Unmarshal(prompts[1], &second) != nil {
		t.Fatal("prompts are not the reviewed JSON body")
	}
	if len(prompts[0]) > budget || first.Note == "" || len(prompts[0]) > 400<<10 {
		t.Fatalf("prompt was not fitted: %d bytes, budget %d, note %q", len(prompts[0]), budget, first.Note)
	}
	if !bytes.Equal(first.Request, run.Portable.Frozen.Plan.Arguments) || !bytes.Contains(first.Request, []byte("eligible")) || len(first.Profiles) == 0 {
		t.Fatal("fitting dropped the reviewed request or profiles")
	}
	if second.Section == nil || !strings.HasPrefix(second.Section.Text, `"`+head) || second.Section.TotalBytes < 400<<10 {
		t.Fatalf("read_context was not answered from the retained source: %+v", second.Section)
	}
	held, err := store.read()
	if err != nil || len(held.Attempts) < 3 || held.Attempts[0].Kind != "page" {
		t.Fatalf("unexpected journal %+v %v", held.Attempts, err)
	}
	for _, a := range held.Attempts[1:] {
		if a.Kind != "model" {
			t.Fatalf("read_context caused a %s acquisition", a.Kind)
		}
	}
}
