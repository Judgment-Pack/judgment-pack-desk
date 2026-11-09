package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

// Handwritten presentation, never recorded from the projector. Omitted tokens
// and an explicit output zero must survive without acquiring an input zero.
const retainedReport1 = `{"version":1,"checkpointVersion":2,"id":"report-run","phase":"work","status":"stopped","unit":"provider-managed","used":0,"limit":2,"grants":[{"from":1,"to":2,"at":"2026-10-28T00:00:00Z"}],"reason":"cancelled","explanation":"Stopped by user","prompts":[{"request":41,"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},{"request":43,"sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}],"sources":[{"source":"runtime","kind":"runtime","calls":1,"refused":0,"proposed":0,"bytes":0,"milliseconds":0}],"sourceIdentities":[{"source":"runtime","identityDigest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","withheld":false,"serviceAsked":true}],"skills":[],"critic":{"mode":"auto","ran":false,"reason":"no-proposal"},"delegation":{"parent":{"model":"a-model","modelRequests":3,"tokens":{"inputTokens":5000,"outputTokens":400}},"batches":[{"batch":1,"children":[{"task":"11111111-1111-4111-8111-111111111111","batch":1,"profile":"review","model":"a-cheaper-model","state":"complete","modelRequests":3,"toolCalls":2,"tokens":{"inputTokens":900,"outputTokens":120},"firstByteMs":700,"durationMs":8200},{"task":"22222222-2222-4222-8222-222222222222","batch":1,"profile":"research","model":"a-cheaper-model","state":"complete","modelRequests":5,"toolCalls":4,"firstByteMs":900,"durationMs":11000,"partial":true}],"total":{"modelRequests":8,"toolCalls":6,"tokens":{"inputTokens":900,"outputTokens":120},"durationMs":11500}}]},"usage":{"rows":[{"source":"agent","connection":"subscription","role":"assistant","model":"reported-model","requests":0,"invocations":1,"milliseconds":0,"bytes":0,"tokens":{"outputTokens":0}}]},"childUsage":[{"id":"child-one","usage":{"rows":[]}}],"failover":[{"role":"assistant","connection":"second","consumed":1,"history":[{"from":"first","to":"second","reason":"timeout"}]}],"completions":[],"plan":[],"planState":{"revision":"sha256:4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945","sequence":0,"nextId":1,"discarded":0,"history":[]}}`

const retainedReportAPI = `{"version":1,"checkpointVersion":2,"id":"api-report","phase":"work","status":"complete","unit":"model-requests","used":1,"limit":1,"requestCap":1,"grants":[],"explanation":"","plan":[],"completions":[{"request":1,"phase":"work","result":{"finishReason":"stop","outputTokens":0}}]}`

func retainedReport2() string {
	raw := strings.Replace(retainedReport1, `"version":1,"checkpointVersion":2`, `"version":2,"checkpointVersion":3`, 1)
	return strings.TrimSuffix(raw, "}") + `,"route":{"version":1,"route":"agent","skills":[],"tools":[],"operations":["read"],"planning":"withheld","decidedBy":"host","reason":"retained","bindingDigest":"sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd","catalogueDigest":"sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee","widened":false,"refusals":[]},"feedback":[{"sourceDigest":"sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff","record":{"version":1,"id":"feedback-one","runId":"report-run","sequence":1,"previous":null,"target":{"kind":"run"},"rating":"good","actor":"user","recordedAt":"2026-10-28T00:00:00.000Z"}}],"repeatTools":{"executions":[{"execution":"report-run","remindAt":2,"stopAt":3,"count":0,"reminded":false,"reminderDelivered":false,"stopped":false}],"policy":{"repeatTools.mode":{"layer":"run","value":"on"},"repeatTools.remindAt":{"layer":"run","value":2},"repeatTools.stopAt":{"layer":"run","value":3}}},"usageTotals":{"modelRequests":{"known":0,"complete":true},"providerInvocations":{"known":1,"complete":true},"inputTokens":{"known":0,"complete":false},"outputTokens":{"known":0,"complete":true}}}`
}
func reportConversation(report string) []byte {
	return []byte(`{"version":1,"chats":[{"checkpoint":{"state":{"responses":[{"work":{"report":{"requests":0,"unit":"provider-managed","canonical":` + report + `}}}]}}}]}`)
}

func TestConversationCanonicalRunReportRawReader(t *testing.T) {
	for _, raw := range []string{retainedReport1, retainedReport2(), retainedReportAPI} {
		data := reportConversation(raw)
		before := bytes.Clone(data)
		assistantRunMust(t, validateConversations(data))
		report, err := runcontext.ParseRunReport([]byte(raw))
		assistantRunMust(t, err)
		if report.Has("usage") && report.Get("usage").Get("rows").Items()[0].Get("tokens").Has("inputTokens") {
			t.Fatal("invented token count")
		}
		if !bytes.Equal(data, before) {
			t.Fatal("validation changed original history")
		}
	}
	// These literals round into apparently safe numbers under ordinary decoding.
	for _, raw := range []string{
		strings.Replace(retainedReport1, `"used":0`, `"used":9007199254740991.1`, 1),
		strings.Replace(retainedReport1, `"used":0`, `"used":0,"used":0`, 1),
		strings.Replace(retainedReport1, `"version":1`, `"version":7`, 1),
		strings.Replace(retainedReport1, `"used":0`, `"used":0,"approval":{}`, 1),
		strings.Replace(retainedReportAPI, `"finishReason":"stop"`, `"finishReason":"invented"`, 1),
		`{"version":1,"explanation":"` + strings.Repeat("x", runcontext.MaxRunReportBytes) + `"}`,
	} {
		data := reportConversation(raw)
		before := bytes.Clone(data)
		if err := validateConversations(data); err == nil {
			t.Fatal("accepted malformed retained report", raw[:60])
		}
		if !bytes.Equal(data, before) {
			t.Fatal("refusal changed original history")
		}
	}
	assistantRunMust(t, validateConversations([]byte(`{"version":1,"chats":[{"checkpoint":{"state":{"responses":[{"work":{"report":{"requests":3,"unit":"provider-managed"}}}]}}}]}`)))
}

func TestAssistantCanonicalRunReportTerminalSource(t *testing.T) {
	for _, version := range []int{1, 3} {
		cp := contract.Checkpoint{}
		raw := `{"version":1,"id":"execution-1","requestKey":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","phase":"work","status":"complete","unit":"provider-managed","used":0,"limit":1,"grants":[],"plan":[],"explanation":"","operations":[],"context":{"scope":"scope-1","entries":[]},"sources":[],"usage":{"rows":[]}}`
		if version == 3 {
			raw = strings.Replace(raw, `"version":1`, `"version":3`, 1)
		}
		assistantRunMust(t, json.Unmarshal([]byte(raw), &cp))
		source, _ := json.Marshal(cp)
		got, err := assistantCanonicalRunReport(&cp)
		assistantRunMust(t, err)
		held, err := runcontext.ParseRunReport(got)
		assistantRunMust(t, err)
		if !held.Has("sources") || !held.Has("usage") || held.Has("context") || held.Has("requestKey") {
			t.Fatal("report dropped facts or retained authority")
		}
		after, _ := json.Marshal(cp)
		if !bytes.Equal(source, after) {
			t.Fatal("projection changed checkpoint")
		}
		for _, status := range []string{"running", "paused"} {
			cp.Status = status
			report, err := assistantCanonicalRunReport(&cp)
			assistantRunMust(t, err)
			if report != nil {
				t.Fatal("manufactured terminal report")
			}
		}
	}
}

// Exercise each real application producer with handwritten provider replies.
// The production fixture installs the real executor; no terminal checkpoint is
// injected at projection or read time.
func TestAssistantCanonicalRunReportActualProducers(t *testing.T) {
	for _, native := range []bool{false, true} {
		t.Run(fmt.Sprintf("native-%t", native), func(t *testing.T) {
			var requests atomic.Int32
			fixtureReady, admitted := false, false
			var execution *assistantRunExecution
			defer func() {
				if !t.Failed() {
					return
				}
				t.Logf("report producer evidence native=%t fixtureReady=%t admitted=%t requests=%d executionCaptured=%t", native, fixtureReady, admitted, requests.Load(), execution != nil)
				if execution != nil {
					assistantRetainedProductionSnapshot(t, execution, nil, requests.Load())
				}
			}()
			upstream := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				assistantBriefSSE(w, "Report producer answer.", "stop")
			})
			mode := ""
			if native {
				mode = "read"
			}
			s, token, start, _ := assistantProductionFixture(t, upstream.server.URL, "work", mode)
			fixtureReady = true
			production, ok := s.assistantRunExecutor.(*assistantProductionExecutor)
			if !ok {
				t.Fatal("report fixture lost the production executor")
			}
			// The exact factory instance still owns Prepare and Execute. Embedding
			// also preserves its canonical and retained-chat capability markers.
			s.assistantRunExecutor = &assistantReportObservedProductionExecutor{assistantProductionExecutor: production, t: t, requests: &requests}
			first := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
			admitted = true
			h := s.assistantRunService()
			h.mu.Lock()
			execution = h.active[first.ID]
			h.mu.Unlock()
			view := assistantProtocolWaitWithin(t, s, token, first.ID, "complete", 60*time.Second)
			record := canonicalActivationRecord(t, s, view.ID)
			if record.Checkpoint == nil || record.Checkpoint.Status != "complete" || record.ChatProjection == nil || !record.ChatProjection.Acknowledged {
				t.Fatal("actual terminal producer or acknowledged saved history missing")
			}
			if native && (record.Checkpoint.Unit != "provider-managed" || record.Checkpoint.Used != 0) {
				t.Fatal("native accounting changed")
			}
			saved := policyWorkspaceRead(t, s, token, "/api/conversations")
			assistantRunMust(t, validateConversations(saved.Content))
			var doc struct {
				Chats []struct {
					Checkpoint struct {
						State struct {
							Responses []struct {
								Work struct {
									Report struct {
										Canonical json.RawMessage `json:"canonical"`
									} `json:"report"`
								} `json:"work"`
							} `json:"responses"`
						} `json:"state"`
					} `json:"checkpoint"`
				} `json:"chats"`
			}
			assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
			responses := doc.Chats[0].Checkpoint.State.Responses
			if len(responses) == 0 {
				t.Fatal("actual response missing")
			}
			retained := responses[len(responses)-1].Work.Report.Canonical
			got, err := runcontext.ParseRunReport(retained)
			assistantRunMust(t, err)
			expected, err := runcontext.ProjectRunReport(*record.Checkpoint)
			assistantRunMust(t, err)
			gotText, err := runcontext.CanonicalRunReport(got)
			assistantRunMust(t, err)
			expectedText, err := runcontext.CanonicalRunReport(expected)
			assistantRunMust(t, err)
			if gotText != expectedText {
				t.Fatal("saved history lost original producer facts")
			}
			if got.Has("durable") || got.Has("operations") || got.Has("requestKey") {
				t.Fatal("saved report contains execution authority")
			}
		})
	}
}

// The same handwritten checkpoint 5/6 records feed both application readers.
// A terminal projection is presentation only; neither kept text nor execution
// authority may leak into the conversation's saved report.
func TestAssistantCanonicalRunReportKeptTextReaders(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "assistant", "serverRuns", "fixtures", "approval-records.json"))
	assistantRunMust(t, err)
	var rows []struct {
		Checkpoint contract.Checkpoint `json:"checkpoint"`
	}
	assistantRunMust(t, json.Unmarshal(raw, &rows))
	if len(rows) != 2 {
		t.Fatal("missing checkpoint 5/6 reader cases")
	}
	for _, row := range rows {
		cp := row.Checkpoint
		cp.Status = "stopped"
		cp.Approval.State = "answered"
		at := "2026-11-04T00:00:00Z"
		cp.Approval.DecidedAt = &at
		var result contract.ToolResultMCPToolResult
		assistantRunMust(t, json.Unmarshal([]byte(`{"content":[{"type":"text","text":"recorded result"}]}`), &result))
		cp.Approval.Result = &result
		before, err := json.Marshal(cp)
		assistantRunMust(t, err)
		projected, err := assistantCanonicalRunReport(&cp)
		assistantRunMust(t, err)
		report, err := runcontext.ParseRunReport(projected)
		assistantRunMust(t, err)
		if report.Get("version").Num() != 3 || report.Get("checkpointVersion").Num() != float64(cp.Version) || report.Has("principal") != (cp.Version == 6) {
			t.Fatal("report version or principal inventory changed")
		}
		for _, name := range []string{"approval", "request", "operations", "durable", "context", "requestKey"} {
			if report.Has(name) {
				t.Fatalf("report retained execution authority %s", name)
			}
		}
		assistantRunMust(t, validateConversations(reportConversation(string(projected))))
		after, err := json.Marshal(cp)
		assistantRunMust(t, err)
		if !bytes.Equal(before, after) {
			t.Fatal("report changed the retained checkpoint")
		}
	}
}

// Observation only: preserve the actual factory's behavior and returned error.
type assistantReportObservedProductionExecutor struct {
	*assistantProductionExecutor
	t        *testing.T
	requests *atomic.Int32
}

func (x *assistantReportObservedProductionExecutor) Execute(ctx context.Context, e *assistantRunExecution) error {
	err := x.assistantProductionExecutor.Execute(ctx, e)
	if err != nil {
		x.t.Logf("report actual production error=%T scope=%t dispatch=%t invalid=%t storageTransition=%t canceled=%t deadline=%t", err, errors.Is(err, errAssistantRunScope), errors.Is(err, errAssistantRunDispatch), errors.Is(err, errAssistantRunInvalid), errors.Is(err, errStorageOwnerTransitionRequired), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded))
		assistantRetainedProductionSnapshot(x.t, e, nil, x.requests.Load())
	}
	return err
}
