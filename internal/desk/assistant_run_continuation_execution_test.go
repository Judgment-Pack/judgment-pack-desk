package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

func assistantContinuationOfferQuestion(t *testing.T, x *assistantExecution) {
	t.Helper()
	x.Run.HostTools = append(x.Run.HostTools, runcontext.HostTool{Name: "ask_user", Description: "Ask a scope question.", Taxonomy: &runcontext.ToolTaxonomy{Operation: "ask", Effect: "pure"}, Interaction: "question", InputSchema: json.RawMessage(`{"type":"object"}`)})
}
func assistantContinuationActualAnswer(view assistantRunView) assistantRunAction {
	a := assistantContinuationAction(view, "answer")
	for _, event := range view.Events {
		if q := event.Event.UserQuestion; q != nil {
			a.QuestionID = q.ID
			a.QuestionDigest = assistantRunQuestionDigest(q.Question)
		}
	}
	return a
}
func TestAssistantRunContinuationActualEino(t *testing.T) {
	for _, kind := range []string{"answer", "extend-budget"} {
		t.Run(kind, func(t *testing.T) {
			var requests atomic.Int32
			var u *upstream
			u = newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				n := requests.Add(1)
				raw := u.arrivals()[int(n)-1].body
				w.Header().Set("Content-Type", "text/event-stream")
				if n == 1 {
					name, args := "ask_user", `{"title":"Scope","question":"Which scope?","selection":"single","options":[{"id":"one","label":"One"},{"id":"two","label":"Two"}]}`
					if kind != "answer" {
						name, args = "validate", `{}`
					}
					encoded, _ := json.Marshal(args)
					_, _ = fmt.Fprintf(w, "data: {\"id\":\"fixture\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"ask-one\",\"type\":\"function\",\"function\":{\"name\":%q,\"arguments\":%s}}]},\"finish_reason\":null}]}\n\ndata: {\"id\":\"fixture\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n", name, encoded)
				} else {
					if kind == "answer" {
						var body any
						assistantRunMust(t, json.Unmarshal(raw, &body))
						decoded, _ := json.Marshal(body)
						if !strings.Contains(string(decoded), "PERSON'S RETAINED QUESTION ANSWER") || !strings.Contains(string(decoded), "questionDigest") || !strings.Contains(string(decoded), "Review exact input") {
							t.Error("actual API adapter lost question answer/original request", string(raw))
						}
						// Parse the exact history JSON from the actual provider messages rather than
						// accepting a label or an unconsumed Context.Documents value.
						var wire struct {
							Messages []struct {
								Content any `json:"content"`
							} `json:"messages"`
						}
						assistantRunMust(t, json.Unmarshal(raw, &wire))
						found := false
						for _, m := range wire.Messages {
							texts := []string{}
							switch content := m.Content.(type) {
							case string:
								texts = append(texts, content)
							case []any:
								for _, part := range content {
									if part, ok := part.(map[string]any); ok && part["type"] == "text" {
										if text, ok := part["text"].(string); ok {
											texts = append(texts, text)
										}
									}
								}
							}
							for _, text := range texts {
								marker := "PERSON'S RETAINED QUESTION ANSWER (user history; preserve the original task and constraints; no tool approval or compute grant)\n"
								if pos := strings.Index(text, marker); pos >= 0 {
									var history struct {
										Answer assistantRunAnswer `json:"answer"`
									}
									dec := json.NewDecoder(strings.NewReader(text[pos+len(marker):]))
									assistantRunMust(t, dec.Decode(&history))
									found = history.Answer.Text == "Exact answer <&> 한글\nnext line" && len(history.Answer.Selected) == 1 && history.Answer.Selected[0] == "two"
								}
							}
						}
						if !found {
							t.Error("exact answer did not reach actual API request")
						}
					}
					_, _ = io.WriteString(w, "data: {\"id\":\"done\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Answer retained.\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"done\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
				}
			})
			proceed := make(chan struct{})
			s, token, e := assistantExecutionFixtureSource(t, u.server.URL, "work", func(ctx context.Context, e *assistantRunExecution) error {
				<-proceed
				x := assistantExecutionOpen(t, e.service.server, e)
				defer x.Close()
				if kind == "answer" && (!assistantCanonicalActive(e.record) || e.canonicalStore == nil || e.canonicalStore.prepared != nil || e.canonicalStore.writer != e.writer || e.canonicalInput.Persistence != e.canonicalBinding) {
					t.Fatal("admission-to-launch prepared identity changed")
				}
				if kind == "answer" {
					assistantContinuationOfferQuestion(t, x)
				} else if len(e.record.Actions) == 0 {
					// Seed a hand-authored near-exhausted checkpoint before the adapter
					// binds persistence; the remaining request and explicit grant are real.
					cp := *e.record.Checkpoint
					cp.Used = cp.Limit - 1
					assistantRunMust(t, e.Checkpoint(ctx, cp))
					x.Run.Context.Execution = &cp
				}
				if kind == "extend-budget" {
					cp := x.Run.Context.Execution
					t.Logf("budget fixture before adapter: canonical=%t persistence=%t runStore=%t status=%s used=%d limit=%d prompts=%d requests=%d", assistantCanonicalActive(e.record), x.Run.Context.Persistence != nil, x.Run.Context.RunStore != nil, cp.Status, cp.Used, cp.Limit, len(cp.Prompts), requests.Load())
				}
				runErr := x.Adapter.Run(ctx, x.Run)
				if kind == "extend-budget" {
					// Capture the adapter's first failure before the host finalizes the
					// response as stopped. Keep the actual adapter and allowance intact.
					latest, readErr := e.writer.lease.store.Read(context.WithoutCancel(ctx), e.record.Binding, e.record.ID)
					if readErr != nil || latest.Checkpoint == nil {
						t.Logf("budget fixture adapter returned: error=%v read_error=%v checkpoint_present=%t requests=%d writer_error=%v", runErr, readErr, latest.Checkpoint != nil, requests.Load(), e.writer.Err())
					} else {
						cp := latest.Checkpoint
						reason := ""
						if cp.Reason != nil {
							reason = *cp.Reason
						}
						t.Logf("budget fixture adapter returned: error=%v state=%s status=%s reason=%q used=%d limit=%d prompts=%d requests=%d writer_error=%v", runErr, latest.State, cp.Status, reason, cp.Used, cp.Limit, len(cp.Prompts), requests.Load(), e.writer.Err())
						for _, row := range latest.Events {
							if row.Event.Error != nil {
								raw, err := json.Marshal(row.Event)
								t.Logf("budget fixture adapter error event: %s marshal_error=%v", raw, err)
							}
						}
					}
				}
				return runErr
			}, func(s *Server, _ string, start assistantRunStart) assistantRunStart {
				if kind == "answer" {
					// Both producer and successor must use the published durable
					// preparation path before the identity assertion can qualify it.
					original := s.assistantRunExecutor.(*assistantProtocolExecutor)
					s.assistantRunExecutor = &assistantCanonicalProtocolExecutor{assistantProtocolExecutor: original}
				}
				return start
			})
			close(proceed)
			state := "awaiting-input"
			if kind != "answer" {
				state = "paused-budget"
			}
			paused := assistantProtocolWait(t, s, token, e.record.ID, state)
			before := assistantExecutionRecord(t, e)
			// A browser's unrelated document CAS retains the pinned source, so the
			// next real adapter dispatch consumes the same request and operation receipts.
			saved := policyWorkspaceRead(t, s, token, "/api/conversations")
			var document conversationDocument
			assistantRunMust(t, json.Unmarshal(saved.Content, &document))
			original := append([]byte{}, document.Chats[0]...)
			document.Chats = append(document.Chats, json.RawMessage(`{"id":"unrelated","title":"Unsent draft"}`))
			changed := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, document)
			if changed.Code != 200 {
				t.Fatal("unrelated CAS", changed.Code, changed.Body.String())
			}
			current := policyWorkspaceRead(t, s, token, "/api/conversations")
			assistantRunMust(t, json.Unmarshal(current.Content, &document))
			if !bytes.Equal(original, document.Chats[0]) {
				t.Fatal("unrelated save rewrote pinned chat")
			}
			a := assistantContinuationActualAnswer(paused)
			if kind != "answer" {
				a = assistantContinuationAction(paused, kind)
			}
			admitted := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+paused.ID+"/actions", a), 200)
			if kind == "answer" {
				assistantRunClientJSON(t, "actual-answer-parent.json", admitted)
			}
			next := assistantContinuationSuccessor(t, s, token, admitted)
			complete := assistantProtocolWait(t, s, token, next.ID, "complete")
			if requests.Load() != 2 {
				t.Fatal("continuation repeated provider work", requests.Load())
			}
			for _, prior := range before.Operations {
				found := false
				for _, op := range assistantProtocolView(t, policyCall(s, token, "GET", "/api/assistant-runs/"+paused.ID, nil), 200).Operations {
					if op.ID == prior.ID {
						found = op.State == prior.State && string(op.Intent) == string(prior.Intent) && string(op.Result) == string(prior.Result)
					}
				}
				if !found {
					t.Fatal("completed receipt changed/lost", prior.ID)
				}
			}
			if kind == "answer" {
				parent := canonicalActivationRecord(t, s, paused.ID)
				if complete.ID == paused.ID || complete.Checkpoint.ID == paused.Checkpoint.ID || complete.Checkpoint.Used != 1 || complete.Checkpoint.Limit != 20 || len(complete.Checkpoint.Grants) != 0 || !reflect.DeepEqual(parent.Canonical, before.Canonical) || !reflect.DeepEqual(parent.Checkpoint, before.Checkpoint) {
					t.Fatal("answer successor inherited allowance or mutated complete parent")
				}
			} else if complete.Checkpoint.Used != paused.Checkpoint.Used+1 {
				t.Fatal("budget continuation reset allowance")
			}
			assistantRunClientJSON(t, "actual-continuation-"+kind+".json", complete)
			if kind == "answer" {
				latest := canonicalActivationRecord(t, s, complete.ID)
				projected, err := compileAssistantChatProjection(e.source.Original, latest, "2026-10-23T00:00:00Z")
				assistantRunMust(t, err)
				assistantRunMust(t, validateConversations([]byte(`{"version":1,"chats":[`+string(projected)+`]}`)))
				assistantRunClientOutput(t, "actual-continuation-projected-chat.json", projected)
			}
		})
	}
}

type assistantCanonicalCodexContinuationExecutor struct{ *assistantCodexExecutor }

func (*assistantCanonicalCodexContinuationExecutor) assistantCanonicalRuns() {}

func TestAssistantRunContinuationActualSubscription(t *testing.T) {
	proceed := make(chan struct{})
	s, token, e, _, trace := assistantCodexSourceFixtureExecute(t, "continuation-question", "work", testAIAlpha, `{"profileVersion":1,"codex":{"inherit":true},"projectInstructions":"HOST-INSTRUCTIONS-ONCE","context":{"version":1,"projectBrief":"execution-brief.md"}}`, func(s *Server, _ string, start assistantRunStart) assistantRunStart {
		original := s.assistantRunExecutor.(*assistantCodexExecutor)
		s.assistantRunExecutor = &assistantCanonicalCodexContinuationExecutor{assistantCodexExecutor: original}
		return start
	}, func(ctx context.Context, e *assistantRunExecution) error {
		<-proceed
		x := assistantExecutionOpen(t, e.service.server, e)
		defer x.Close()
		if !assistantCanonicalActive(e.record) || e.canonicalStore == nil || e.canonicalStore.prepared != nil || e.canonicalStore.writer != e.writer || e.canonicalInput.Persistence != e.canonicalBinding {
			t.Fatal("admission-to-launch prepared identity changed")
		}
		assistantContinuationOfferQuestion(t, x)
		if len(e.record.AnswerHistory) == 0 {
			originalEmit := x.Run.Emit
			var errorEvents atomic.Int32
			x.Run.Emit = func(event contract.RunEvent) {
				if event.Error != nil && errorEvents.Add(1) <= 8 {
					if f := event.Error.Failure; f != nil {
						t.Logf("native question parent emitted failure code=%.80s source=%.80s outcome=%.80s", f.Code, f.Source, f.Outcome)
					} else {
						t.Log("native question parent emitted error without typed failure")
					}
				}
				originalEmit(event)
			}
		}
		runErr := x.Adapter.Run(ctx, x.Run)
		if len(e.record.AnswerHistory) == 0 {
			// Capture the original parent outcome before Close/finalization. Keep
			// payloads and provider prose out of the bounded structural evidence.
			t.Logf("native question parent adapter=%T scope=%t dispatch=%t invalid=%t canceled=%t deadline=%t executionCanceled=%t canonicalWriterFailed=%t", runErr, errors.Is(runErr, errAssistantRunScope), errors.Is(runErr, errAssistantRunDispatch), errors.Is(runErr, errAssistantRunInvalid), errors.Is(runErr, context.Canceled), errors.Is(runErr, context.DeadlineExceeded), e.ctx.Err() != nil, x.Run.Context.Persistence.Writer.Failed())
			// No API request counter exists on this provider-managed fixture.
			assistantRetainedProductionSnapshot(t, e, x, -1)
			if cp := x.Run.Context.Persistence.Writer.Checkpoint(); cp != nil && cp.Durable != nil {
				count := 0
				for _, dispatch := range cp.Durable.Dispatches {
					if dispatch.Kind != "agent" {
						continue
					}
					count++
					if count > 8 {
						break
					}
					if f := dispatch.Failure; f != nil {
						t.Logf("native question parent durable agent state=%.80s failure code=%.80s source=%.80s outcome=%.80s", dispatch.State, f.Code, f.Source, f.Outcome)
					} else {
						t.Logf("native question parent durable agent state=%.80s typedFailure=false", dispatch.State)
					}
				}
			}
		}
		return runErr
	})
	close(proceed)
	paused := assistantProtocolWait(t, s, token, e.record.ID, "awaiting-input")
	parent := canonicalActivationRecord(t, s, paused.ID)
	admitted := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+paused.ID+"/actions", assistantContinuationActualAnswer(paused)), 200)
	assistantRunClientJSON(t, "actual-subscription-answer-parent.json", admitted)
	next := assistantContinuationSuccessor(t, s, token, admitted)
	complete := assistantProtocolWait(t, s, token, next.ID, "complete")
	after := canonicalActivationRecord(t, s, paused.ID)
	if complete.ID == paused.ID || complete.Checkpoint.ID == paused.Checkpoint.ID || !reflect.DeepEqual(parent.Canonical, after.Canonical) || !reflect.DeepEqual(parent.Checkpoint, after.Checkpoint) || len(complete.AnswerHistory) != 1 {
		t.Fatal("native successor mutated complete head or lost history")
	}
	if complete.Checkpoint.Used != 0 || complete.Checkpoint.Limit != 1 || len(complete.Checkpoint.Grants) != 0 {
		t.Fatal("invented provider-managed allowance")
	}
	count := 0
	for _, row := range assistantCodexLog(t, trace) {
		if string(row["type"]) == `"generation"` {
			count++
		}
	}
	if count != 2 {
		t.Fatal("native question re-executed", count)
	}
	assistantRunClientJSON(t, "actual-continuation-subscription.json", complete)
}
