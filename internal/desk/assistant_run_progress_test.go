package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/relay"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

func progressValue(text string, task *string) contract.RunEventMessageProgress {
	return contract.RunEventMessageProgress{Type: "message_progress", Text: text, TaskID: task}
}
func TestAssistantRunProgressWireVector(t *testing.T) {
	raw, err := os.ReadFile("../../web/src/assistant/serverRuns/fixtures/progress.json")
	assistantRunMust(t, err)
	var p assistantRunProgress
	assistantRunMust(t, json.Unmarshal(raw, &p))
	if p.ID != "arun_"+strings.Repeat("1", 32) || p.Sequence != 1 || p.Text != "Whole 한글🙂\nprefix" {
		t.Fatal("hand-authored vector changed")
	}
	if !validAssistantRunProgress(progressValue(p.Text, p.TaskID)) {
		t.Fatal("canonical prefix refused")
	}
	w := httptest.NewRecorder()
	assistantRunMust(t, assistantRunSSE(w, http.NewResponseController(w), "progress", "", p))
	want := "event: progress\ndata: " + strings.TrimSpace(string(raw)) + "\n\n"
	if w.Body.String() != want {
		t.Fatalf("Go/browser wire mismatch: %s", w.Body.String())
	}
	if _, err := assistantRunEventBytes(contract.RunEvent{MessageProgress: &contract.RunEventMessageProgress{Type: "message_progress", Text: p.Text}}); err == nil {
		t.Fatal("live text entered semantic store")
	}
	for _, p := range []contract.RunEventMessageProgress{progressValue(strings.Repeat("x", assistantRunProgressBytes+1), nil), progressValue(string([]byte{0xff}), nil), progressValue("\x00", nil), {Type: "message", Text: "x"}, {Type: "message_progress", Text: "x", AdditionalMembers: map[string]json.RawMessage{"authority": json.RawMessage(`true`)}}, progressValue("x", new(string))} {
		if validAssistantRunProgress(p) {
			t.Fatal("malformed canonical progress accepted")
		}
	}
}
func TestAssistantRunProgressPreviewVectors(t *testing.T) {
	child := "child"
	good := []assistantRunPreview{{Text: "Incomplete parent"}, {Text: "Incomplete child", TaskID: &child}}
	assistantRunMust(t, validateAssistantRunPreview(good))
	for _, raw := range []string{`null`, `[]`, `[{}]`, `[{"text":""}]`, `[{"text":"x","taskId":null}]`, `[{"text":"x","authority":true}]`} {
		if readAssistantRunPreview([]byte(raw)) == nil {
			t.Fatal("invalid private preview wire accepted", raw)
		}
	}
	for _, rows := range [][]assistantRunPreview{{{}}, {{Text: "x"}, {Text: "y"}}, {{Text: strings.Repeat("x", assistantRunProgressBytes+1)}}, {{Text: "x", TaskID: new(string)}}} {
		if validateAssistantRunPreview(rows) == nil {
			t.Fatal("invalid preview accepted", rows)
		}
	}
	// Exact private member spelling, unknown members and missing text cannot
	// become permissions or be silently defaulted by Go's permissive decoder.
	for _, raw := range []string{`{"runs":[{"displayPreview":[{"Text":"x"}]}]}`, `{"runs":[{"displayPreview":[{"text":"x","authority":true}]}]}`} {
		if assistantRunJSONMembers([]byte(raw)) == nil {
			t.Fatal("private preview member guard missing")
		}
	}
}
func TestAssistantRunProgressBoundedAuthenticatedLifecycle(t *testing.T) {
	s, token, e := assistantExecutionFixture(t, "https://example.invalid", "work")
	before := assistantExecutionRecord(t, e)
	stale := *e
	if err := stale.Progress(context.Background(), progressValue("Stale execution", nil)); err == nil {
		t.Fatal("superseded execution emitted prefix")
	}
	wake, release, err := e.service.subscribe(e.record.ID)
	assistantRunMust(t, err)
	defer release()
	for i := 0; i < 100; i++ {
		assistantRunMust(t, e.Progress(context.Background(), progressValue(fmt.Sprintf("Prefix %d 한글", i), nil)))
	}
	if after := assistantExecutionRecord(t, e); after.Revision != before.Revision || len(after.Events) != len(before.Events) || len(after.DisplayPreview) != 0 {
		t.Fatal("each prefix persisted")
	}
	if len(wake) != 1 {
		t.Fatal("unbounded wake queue")
	}
	request := httptest.NewRequest("GET", "/", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	p, err := e.service.readProgress(request, assistantExecutionRecord(t, e))
	assistantRunMust(t, err)
	if len(p) != 1 || p[0].Text != "Prefix 99 한글" || p[0].Sequence != 100 {
		t.Fatal("prefix was appended or lost", p)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			task := fmt.Sprintf("child-%d", i)
			if err := e.Progress(context.Background(), progressValue("Child", &task)); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	p, err = e.service.readProgress(request, assistantExecutionRecord(t, e))
	assistantRunMust(t, err)
	if len(p) != 9 {
		t.Fatal("concurrent child prefixes overwrote parent")
	}
	cp := *before.Checkpoint
	assistantRunMust(t, e.Checkpoint(context.Background(), cp))
	saved := assistantExecutionRecord(t, e)
	if len(saved.DisplayPreview) != 9 || !reflect.DeepEqual(cp, *saved.Checkpoint) {
		t.Fatal("display custody changed canonical checkpoint")
	}
	// Reopening a service does not reconstruct a live execution from display data.
	other := &assistantRunService{server: s, active: map[string]*assistantRunExecution{}}
	empty, err := other.readProgress(request, saved)
	assistantRunMust(t, err)
	if len(empty) != 0 {
		t.Fatal("preview became live authority after restart")
	}
	assistantRunMust(t, e.Event(context.Background(), "final", contract.RunEvent{Message: &contract.RunEventMessage{Type: "message", Text: "Final"}}))
	saved = assistantExecutionRecord(t, e)
	for _, row := range saved.DisplayPreview {
		if row.TaskID == nil {
			t.Fatal("completed parent prose duplicated")
		}
	}
	// A foreign bearer cannot observe live state, even with a known record.
	foreign := request.Clone(context.Background())
	foreign.Header = foreign.Header.Clone()
	foreign.Header.Set("Authorization", "Bearer "+policyTestSession(t, s))
	if _, err = e.service.readProgress(foreign, saved); err == nil {
		t.Fatal("foreign session observed prefix")
	}
	s.sessions.revoke(token)
	if err = e.Progress(context.Background(), progressValue("Secret after logout", nil)); err == nil {
		t.Fatal("logout emission accepted")
	}
	if _, err = e.service.readProgress(request, saved); err == nil {
		t.Fatal("logout read accepted")
	}
}

// This response writer observes the real registered SSE handler without a
// browser or an extra network listener. Each Write is a complete SSE frame.
type assistantProgressWriter struct {
	header http.Header
	frames chan string
	ctx    context.Context
}

func (w *assistantProgressWriter) Header() http.Header { return w.header }
func (w *assistantProgressWriter) WriteHeader(int)     {}
func (w *assistantProgressWriter) Write(p []byte) (int, error) {
	select {
	case w.frames <- string(p):
		return len(p), nil
	case <-w.ctx.Done():
		return 0, w.ctx.Err()
	}
}
func (w *assistantProgressWriter) Flush()                           {}
func (w *assistantProgressWriter) SetWriteDeadline(time.Time) error { return nil }
func assistantProgressStream(t *testing.T, s *Server, token, id string) (<-chan string, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	w := &assistantProgressWriter{header: http.Header{}, frames: make(chan string, 256), ctx: ctx}
	r := httptest.NewRequest("GET", "/api/assistant-runs/"+id+"/events", nil).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer "+token)
	done := make(chan struct{})
	go func() { defer close(done); s.ServeHTTP(w, r) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("SSE reader did not exit")
		}
	})
	return w.frames, cancel
}
func assistantProgressFrame(t *testing.T, frames <-chan string, kind string) string {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case frame := <-frames:
			if strings.HasPrefix(frame, "event: "+kind+"\n") {
				return frame
			}
		case <-timer.C:
			t.Fatal("missing SSE " + kind)
			return ""
		}
	}
}

type assistantProgressExecutor struct {
	*assistantProtocolExecutor
	policy runcontext.PolicyLayer
}

func (x *assistantProgressExecutor) Prepare(ctx context.Context, source assistantRunSource) (contract.Checkpoint, error) {
	return assistantFixtureCheckpoint(ctx, source, x.review, x.policy)
}
func assistantProgressAdmission(s *Server, mode string) {
	assistantFixtureReviewOff(s)
	var base *assistantProtocolExecutor
	switch x := s.assistantRunExecutor.(type) {
	case *assistantProtocolExecutor:
		base = x
	case *assistantCodexExecutor:
		base = x.assistantProtocolExecutor
	}
	var policy runcontext.PolicyLayer
	if mode == "off" {
		policy = runcontext.PolicyLayer{"outputGuard": "off"}
	}
	s.assistantRunExecutor = &assistantProgressExecutor{assistantProtocolExecutor: base, policy: policy}
}
func assistantProgressSummary(t *testing.T, e *assistantRunExecution) string {
	t.Helper()
	r := assistantExecutionRecord(t, e)
	ops := []string{}
	issues := []string{}
	for _, op := range r.Operations {
		ops = append(ops, op.Kind+":"+op.State)
	}
	for _, event := range r.Events {
		if event.Event.Error != nil {
			raw, _ := json.Marshal(event.Event.Error)
			if len(raw) > 1024 {
				raw = raw[:1024]
			}
			issues = append(issues, string(raw))
		}
	}
	e.service.mu.Lock()
	sequence := e.progress.sequence
	e.service.mu.Unlock()
	raw, _ := json.Marshal(map[string]any{"state": r.State, "checkpoint": r.Checkpoint.Status, "operations": ops, "errors": issues, "progressSequence": sequence})
	return string(raw)
}
func TestAssistantRunProgressActualAPIAndNativeSSE(t *testing.T) {
	for _, engine := range []string{"api", "native"} {
		for _, mode := range []string{"off", "redact"} {
			t.Run(engine+"/"+mode, func(t *testing.T) {
				var s *Server
				var token string
				var e *assistantRunExecution
				var release func()
				if engine == "api" {
					gate := make(chan struct{})
					var once sync.Once
					release = func() { once.Do(func() { close(gate) }) }
					defer release()
					u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
						t.Log("actual upstream stream started")
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = io.WriteString(w, "data: {\"id\":\"fixture\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Fixture answer.\"},\"finish_reason\":null}]}\n\n")
						http.NewResponseController(w).Flush()
						select {
						case <-gate:
						case <-r.Context().Done():
							return
						}
						_, _ = io.WriteString(w, "data: {\"id\":\"fixture\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
					})
					s, token, e = assistantExecutionFixtureSource(t, u.server.URL, "work", nil, func(s *Server, _ string, start assistantRunStart) assistantRunStart {
						assistantProgressAdmission(s, mode)
						return start
					})
				} else {
					var trace string
					s, token, e, _, trace = assistantCodexSourceFixture(t, "progress", "work", testAIAlpha, `{"profileVersion":1,"codex":{"inherit":true},"projectInstructions":"HOST-INSTRUCTIONS-ONCE","context":{"version":1,"projectBrief":"execution-brief.md"}}`, func(s *Server, _ string, start assistantRunStart) assistantRunStart {
						assistantProgressAdmission(s, mode)
						return start
					})
					release = func() { assistantRunMust(t, os.WriteFile(trace+".release", []byte("release"), 0600)) }
					defer release()
				}
				x := assistantExecutionOpen(t, s, e)
				off := false
				x.Run.Review = &off
				if mode == "off" {
					x.Run.Policy = runcontext.PolicyLayer{"outputGuard": "off"}
				}
				if engine == "api" {
					model := x.Run.Model
					x.Run.Model = func(ctx context.Context, suffix string, request relay.Request) (*http.Response, error) {
						answer, err := model(ctx, suffix, request)
						if answer != nil {
							t.Logf("actual relay status=%d contentType=%q", answer.StatusCode, answer.Header.Get("Content-Type"))
						}
						return answer, err
					}
				}
				emit := x.Run.Emit
				x.Run.Emit = func(event contract.RunEvent) {
					emit(event)
					if event.MessageProgress != nil {
						t.Logf("canonical progress bytes=%d %s", len(event.MessageProgress.Text), assistantProgressSummary(t, e))
					}
				}
				frames, cancel := assistantProgressStream(t, s, token, e.record.ID)
				defer cancel()
				wire := assistantProgressFrame(t, frames, "snapshot")
				if mode == "redact" {
					release()
				}
				done := make(chan error, 1)
				go func() { done <- x.Adapter.Run(e.ctx, x.Run) }()
				if mode == "off" {
					var frame string
					var sequence uint64
					timer := time.NewTimer(5 * time.Second)
					defer timer.Stop()
					for frame == "" {
						select {
						case held := <-frames:
							wire += held
							if strings.HasPrefix(held, "event: progress\n") {
								payload := []byte(strings.TrimSpace(strings.TrimPrefix(held, "event: progress\ndata: ")))
								var fields map[string]json.RawMessage
								var p assistantRunProgress
								decoder := json.NewDecoder(bytes.NewReader(payload))
								decoder.DisallowUnknownFields()
								if strings.Contains(held, "\nid:") || !uniqueJSON(payload) || json.Unmarshal(payload, &fields) != nil || decoder.Decode(&p) != nil {
									t.Fatal("noncanonical progressive frame", held)
								}
								for _, key := range []string{"id", "owner", "requestId", "epoch", "sequence", "cursor", "text"} {
									if len(fields[key]) == 0 || string(fields[key]) == "null" {
										t.Fatal("missing progressive member", key, held)
									}
								}
								record := assistantExecutionRecord(t, e)
								_, _, cursorOK := assistantRunParseCursor(p.Cursor, record)
								e.service.mu.Lock()
								epoch := e.progress.epoch
								e.service.mu.Unlock()
								if p.ID != record.ID || p.Owner != record.Binding.Owner || p.RequestID != record.Binding.RequestID || p.Epoch != epoch || p.Sequence <= sequence || !cursorOK || fields["taskId"] != nil || !validAssistantRunProgress(progressValue(p.Text, nil)) {
									t.Fatal("invalid progressive identity, sequence or prefix", held)
								}
								sequence = p.Sequence
								// Initial clears are valid whole prefixes. Retain them in wire and
								// await prose under the same deadline, without dropping invalid frames.
								if p.Text == "" {
									continue
								}
								if !strings.HasPrefix(p.Text, "Fixture") {
									t.Fatal("unexpected nonempty progressive prefix", held)
								}
								frame = held
							}
						case err := <-done:
							t.Fatalf("adapter ended before live progress: %v %s", err, assistantProgressSummary(t, e))
						case <-timer.C:
							t.Fatalf("missing SSE progress: %s", assistantProgressSummary(t, e))
						}
					}
					for _, event := range assistantExecutionRecord(t, e).Events {
						if event.Event.Message != nil {
							t.Fatal("prefix arrived after completed message")
						}
					}
					reconnect, disconnect := assistantProgressStream(t, s, token, e.record.ID)
					assistantProgressFrame(t, reconnect, "snapshot")
					assistantProgressFrame(t, reconnect, "progress")
					disconnect()
					if e.ctx.Err() != nil {
						t.Fatal("reader disconnection canceled execution")
					}
					release()
				}
				select {
				case err := <-done:
					assistantRunMust(t, err)
				case <-time.After(5 * time.Second):
					t.Fatalf("actual adapter did not complete: %s", assistantProgressSummary(t, e))
				}
				// Receive the completed canonical checkpoint through the actual SSE handler,
				// retaining those observations only as ephemeral cross-reader test inputs.
				timer := time.NewTimer(5 * time.Second)
				defer timer.Stop()
				complete := false
				for !complete {
					select {
					case frame := <-frames:
						wire += frame
						if mode == "redact" && strings.HasPrefix(frame, "event: progress\n") {
							t.Fatal("guarded prose leaked as a prefix")
						}
						if strings.HasPrefix(frame, "event: snapshot\n") {
							at := strings.Index(frame, "data: ")
							var view assistantRunView
							if at >= 0 && json.Unmarshal([]byte(strings.TrimSpace(frame[at+6:])), &view) == nil && view.Checkpoint != nil && view.Checkpoint.Status == "complete" {
								complete = true
							}
						}
					case <-timer.C:
						t.Fatalf("completed checkpoint did not reach SSE: %s", assistantProgressSummary(t, e))
					}
				}
				assistantRunClientOutput(t, "progress-"+engine+"-"+mode+".sse", []byte(wire))
				record := assistantExecutionRecord(t, e)
				found := false
				for _, event := range record.Events {
					if event.Event.Message != nil {
						found = true
					}
				}
				if !found || len(record.DisplayPreview) != 0 {
					t.Fatalf("terminal message/preview reconciliation failed: %s", assistantProgressSummary(t, e))
				}
				cp, err := json.Marshal(record.Checkpoint)
				assistantRunMust(t, err)
				if bytes.Contains(cp, []byte(`"epoch"`)) || bytes.Contains(cp, []byte(`"displayPreview"`)) {
					t.Fatal("live fields contaminated canonical checkpoint")
				}
			})
		}
	}
}
