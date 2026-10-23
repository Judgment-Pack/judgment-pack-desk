package desk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/gate"
)

func assistantContinuationOfferQuestion(t *testing.T, x *assistantExecution) {
	t.Helper()
	interaction := "question"
	offer, err := x.gate.Open(context.Background(), []contract.ToolOfferHostTool{{Name: "ask_user", Interaction: &interaction}})
	assistantRunMust(t, err)
	x.gate.Enter(gate.LoopPhase(offer))
	x.Run.HostTools = []runcontext.HostTool{{Name: "ask_user", Description: "Ask a scope question.", Interaction: interaction, InputSchema: json.RawMessage(`{"type":"object"}`)}}
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
			s, token, e := assistantExecutionFixtureExecute(t, u.server.URL, "work", func(ctx context.Context, e *assistantRunExecution) error {
				<-proceed
				x := assistantExecutionOpen(t, e.service.server, e)
				defer x.Close()
				if kind == "answer" {
					assistantContinuationOfferQuestion(t, x)
				} else if len(e.record.Actions) == 0 {
					cp := *e.record.Checkpoint
					cp.Used = cp.Limit - 1
					assistantRunMust(t, e.Checkpoint(ctx, cp))
					x.Run.Context.Execution = &cp
				}
				return x.Adapter.Run(ctx, x.Run)
			})
			close(proceed)
			state := "awaiting-input"
			if kind != "answer" {
				state = "paused-budget"
			}
			paused := assistantProtocolWait(t, s, token, e.record.ID, state)
			before := assistantExecutionRecord(t, e)
			a := assistantContinuationActualAnswer(paused)
			if kind != "answer" {
				a = assistantContinuationAction(paused, kind)
			}
			assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+paused.ID+"/actions", a), 200)
			complete := assistantProtocolWait(t, s, token, e.record.ID, "complete")
			if requests.Load() != 2 {
				t.Fatal("continuation repeated provider work", requests.Load())
			}
			for _, prior := range before.Operations {
				found := false
				for _, op := range complete.Operations {
					if op.ID == prior.ID {
						found = op.State == prior.State && string(op.Intent) == string(prior.Intent) && string(op.Result) == string(prior.Result)
					}
				}
				if !found {
					t.Fatal("completed receipt changed/lost", prior.ID)
				}
			}
			if complete.Checkpoint.Used != paused.Checkpoint.Used+1 {
				t.Fatal("request accounting reset")
			}
			assistantRunClientJSON(t, "actual-continuation-"+kind+".json", complete)
			if kind == "answer" {
				latest := assistantExecutionRecord(t, e)
				projected, err := compileAssistantChatProjection(e.source.Original, latest, "2026-10-23T00:00:00Z")
				assistantRunMust(t, err)
				assistantRunMust(t, validateConversations([]byte(`{"version":1,"chats":[`+string(projected)+`]}`)))
				assistantRunClientOutput(t, "actual-continuation-projected-chat.json", projected)
			}
		})
	}
}
func TestAssistantRunContinuationActualSubscription(t *testing.T) {
	proceed := make(chan struct{})
	s, token, e, _, trace := assistantCodexSourceFixtureExecute(t, "continuation-question", "work", testAIAlpha, `{"profileVersion":1,"codex":{"inherit":true},"projectInstructions":"HOST-INSTRUCTIONS-ONCE","context":{"version":1,"projectBrief":"execution-brief.md"}}`, nil, func(ctx context.Context, e *assistantRunExecution) error {
		<-proceed
		x := assistantExecutionOpen(t, e.service.server, e)
		defer x.Close()
		assistantContinuationOfferQuestion(t, x)
		return x.Adapter.Run(ctx, x.Run)
	})
	close(proceed)
	paused := assistantProtocolWait(t, s, token, e.record.ID, "awaiting-input")
	assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+paused.ID+"/actions", assistantContinuationActualAnswer(paused)), 200)
	complete := assistantProtocolWait(t, s, token, e.record.ID, "complete")
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
