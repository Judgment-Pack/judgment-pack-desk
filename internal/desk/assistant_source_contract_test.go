package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	native "github.com/Judgment-Pack/judgment-pack-agent/go/adapter/agent"
	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter/eino"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/gate"
	"github.com/Judgment-Pack/judgment-pack-agent/go/relay"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
)

const assistantSourceSentinel = "UNVERIFIED_E0_PAYLOAD_DO_NOT_ACCEPT_9f2"
const assistantSourceWire = "fixture-source__read_unverified"

type assistantSourceContextLink struct{ held *runcontext.SessionContext }

func (l *assistantSourceContextLink) Attach(c *runcontext.SessionContext) { l.held = c }

// This is explicit, handwritten fixture authority for one complete source list.
// It is neither a production approval mechanism nor a cryptographic receipt.
func assistantSourceContractTool(t *testing.T, mode string, executions, verifications, identities, digests *int) runcontext.HostTool {
	t.Helper()
	identity := contract.ToolOfferIdentity{Kind: "connection", Label: "Handwritten receipt rejection fixture"}
	source := &runcontext.ToolSource{Kind: "custom", ID: "fixture-source", Trust: "connected", Version: "fixture-1", Identity: &identity}
	tool := runcontext.HostTool{Name: "read_unverified", Description: "Return the handwritten unverified receipt fixture.", InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`), Source: source, Taxonomy: &runcontext.ToolTaxonomy{Operation: "read", Effect: "read-external"}, EffectClass: "read", Approval: "none", Delegation: "read"}
	source.ToolListDigest = runcontext.ToolListDigest([]runcontext.ListedTool{{Name: tool.Name, Description: tool.Description, InputSchema: tool.InputSchema, Taxonomy: tool.Taxonomy}})
	source.CurrentIdentity = func(context.Context) (contract.ToolOfferIdentity, error) {
		*identities++
		if mode == "identity-drift" {
			return contract.ToolOfferIdentity{Kind: identity.Kind, Label: "Changed fixture identity"}, nil
		}
		return identity, nil
	}
	source.CurrentDigest = func(context.Context) (string, error) {
		*digests++
		return "sha256:" + strings.Repeat("0", 64), nil
	}
	source.VerifyReceipt = func(ctx context.Context, evidence runcontext.ReceiptEvidence) (bool, error) {
		*verifications++
		t.Logf("receipt callback tool=%s receipt=%s source=%s args=%s result=%s", evidence.Tool, evidence.Receipt, evidence.Source, evidence.Args, evidence.Result)
		if evidence.Tool != tool.Name || evidence.Receipt != "deliberately-unverified" || !strings.Contains(evidence.Result, assistantSourceSentinel) {
			t.Error("receipt callback lost exact fixture association")
			return false, nil
		}
		if mode == "expired-callback" {
			<-ctx.Done()
			return true, nil // A late affirmative answer still cannot validate it.
		}
		return false, nil
	}
	tool.Execute = func(context.Context, json.RawMessage) (runcontext.ToolResult, error) {
		*executions++
		result := runcontext.ResultOf([]runcontext.Content{runcontext.TextContent(assistantSourceSentinel)}, nil)
		if mode != "missing-receipt" && mode != "error-without-receipt" {
			result.Receipt = "deliberately-unverified"
		}
		if mode == "error-without-receipt" {
			yes := true
			result.IsError = &yes
		}
		return result, nil
	}
	return tool
}

var assistantSourceContextID = regexp.MustCompile(`"contextId"\s*:\s*"([^"\r\n]+)"`)

// The model peer learns the reference from the actual list_context answer.
// It does not obtain an ID from the fixture's ledger inspection.
func assistantSourceReference(raw []byte) string {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	var find func(any) string
	find = func(v any) string {
		switch x := v.(type) {
		case string:
			if m := assistantSourceContextID.FindStringSubmatch(x); len(m) == 2 {
				return m[1]
			}
		case []any:
			for _, item := range x {
				if id := find(item); id != "" {
					return id
				}
			}
		case map[string]any:
			if id, ok := x["contextId"].(string); ok {
				return id
			}
			for _, item := range x {
				if id := find(item); id != "" {
					return id
				}
			}
		}
		return ""
	}
	return find(value)
}

func assistantSourceEvidence(t *testing.T, name string, value []byte) {
	t.Helper()
	if dir := os.Getenv("JPS_SOURCE_CONTRACT_EVIDENCE"); dir != "" {
		assistantRunMust(t, os.MkdirAll(dir, 0700))
		assistantRunMust(t, os.WriteFile(filepath.Join(dir, strings.ReplaceAll(t.Name(), "/", "-")+"-"+name), value, 0600))
	}
}

// The actual Manager launches this test-owned peer. It requests the source,
// learns a reference through list_context, then requests all three readers.
// Like the Manager's own peer fixtures, it lists Desk's supplied model catalog
// when asked for hidden models, which the Manager checks before a run.
const assistantSourceNativePeer = `#!/usr/bin/env python3
import json,os,re,sys
request={}; step=0; ref='unavailable-source-result'
def write(v): print(json.dumps(v,separators=(',',':')),flush=True)
def note(method,p): write({'method':method,'params':p})
def offered(name): return next(t['name'] for t in request['dynamicTools'] if t['description'].startswith(name+':'))
def call():
 global step
 names=['fixture-source__read_unverified','list_context','read_context','read_window','search']
 if step==5:
  note('item/completed',{'threadId':'thread','turnId':'turn','item':{'id':'answer','type':'agentMessage','phase':'final_answer','text':'Fixture inspection complete.'}})
  note('turn/completed',{'threadId':'thread','turn':{'id':'turn','status':'completed'}})
  return
 args={} if step<2 else ({'pattern':'UNVERIFIED_E0','limit':5} if step==4 else {'id':ref})
 write({'id':'rpc-'+str(step),'method':'item/tool/call','params':{'threadId':'thread','turnId':'turn','callId':'call-'+str(step),'tool':offered(names[step]),'arguments':args}})
 step+=1
for line in sys.stdin:
 q=json.loads(line); method=q.get('method',''); p=q.get('params',{})
 def reply(v): write({'id':q['id'],'result':v})
 if method=='initialize': reply({'userAgent':'jps_desk/0.157.1 (Linux)'})
 elif method=='initialized': pass
 elif method=='account/read': reply({'requiresOpenaiAuth':True,'account':{'type':'chatgpt','planType':'plus'}})
 elif method=='model/list' and p.get('includeHidden'):
  with open(os.path.join(os.environ['CODEX_HOME'],'model-catalog.json')) as f: catalog=json.load(f)
  reply({'data':[{'model':m['slug']} for m in catalog['models']]})
 elif method=='model/list': reply({'data':[{'model':'fixture','displayName':'Fixture','defaultReasoningEffort':'medium','supportedReasoningEfforts':[{'reasoningEffort':'medium'}]}]})
 elif method=='thread/start': request=p; reply({'thread':{'id':'thread'},'model':p['model'],'modelProvider':'openai','approvalPolicy':'never','cwd':p['cwd']})
 elif method=='turn/start': reply({'turn':{'id':'turn'}}); call()
 elif not method:
  if step==2:
   text=json.dumps(q)
   def scan(v):
    if isinstance(v,str):
     m=re.search(r'"contextId"\s*:\s*"([^"\r\n]+)"',v)
     if m: return m.group(1)
    if isinstance(v,dict):
     for a in v.values():
      found=scan(a)
      if found: return found
    if isinstance(v,list):
     for a in v:
      found=scan(a)
      if found: return found
   ref=scan(q) or ref
  call()
 else: sys.exit(74)
`

func assistantSourceContractNative(t *testing.T, observed *[][]byte) *adapter.Agent {
	t.Helper()
	dir := t.TempDir()
	binary := filepath.Join(dir, "source-peer")
	assistantRunMust(t, os.WriteFile(binary, []byte(assistantSourceNativePeer), 0700))
	// A private Codex profile is never inside the project it serves, nor the
	// project inside it: the project is a directory of its own.
	m, err := codexbridge.NewManager(codexbridge.Options{Binary: binary, ProfileDir: filepath.Join(dir, "account"), ProjectDir: t.TempDir()})
	assistantRunMust(t, err)
	t.Cleanup(m.Close)
	return &adapter.Agent{Model: "fixture", Effort: "medium", Run: func(ctx context.Context, request adapter.AgentRequest, callbacks adapter.AgentCallbacks) error {
		raw, err := json.Marshal(request)
		if err != nil {
			return err
		}
		*observed = append(*observed, raw)
		bridge := codexbridge.RunRequest{Model: request.Model, Prompt: request.Prompt, Instructions: request.Instructions, Effort: request.Effort, Phase: request.Phase}
		for _, tool := range request.Tools {
			bridge.Tools = append(bridge.Tools, codexbridge.Tool{Name: tool.Name, Description: tool.Description, InputSchema: tool.InputSchema})
		}
		if release := callbacks.ProviderRelease; release != nil {
			bridge.ProviderReleased = func() { release.Acknowledge(release.Invocation) }
		}
		return m.Run(ctx, codexbridge.Owner{ID: "source-fixture-owner", Session: ctx, Policy: ctx}, bridge, func(event codexbridge.RunEvent) error {
			return callbacks.Event(ctx, adapter.AgentEvent{Type: event.Type, ID: event.ID, Text: event.Text, Phase: event.Phase, Attempt: event.Attempt})
		}, func(ctx context.Context, call codexbridge.ToolCall) (codexbridge.ToolAnswer, error) {
			answer, err := callbacks.Tool(ctx, adapter.AgentToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments})
			*observed = append(*observed, []byte(answer.Text))
			return codexbridge.ToolAnswer{Text: answer.Text, IsError: answer.IsError}, err
		})
	}}
}

func TestAssistantSourceContractFailedReceiptRetention(t *testing.T) {
	for _, engine := range []string{"api", "native"} {
		for _, mode := range []string{"rejected-receipt", "missing-receipt", "error-without-receipt", "expired-callback", "identity-drift", "custom-digest"} {
			if !t.Run(engine+"/"+mode, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
				defer cancel()
				f := assistantCanonicalStoreFixture(t, "source-contract-"+engine+"-"+mode)
				defer func() { assistantSourceEvidence(t, "raw-journal.json", assistantRunRaw(t, f.store)) }()
				executions, verifications, identities, digests := 0, 0, 0, 0
				tool := assistantSourceContractTool(t, mode, &executions, &verifications, &identities, &digests)
				chat := runcontext.NewChatContext("source-contract-chat")
				link := &assistantSourceContextLink{}
				input := &runcontext.Input{Chat: chat, Link: link, CurrentRequest: &runcontext.CurrentRequest{Message: "Inspect the supplied source fixture", Input: "Inspect the supplied source fixture"}, RunStore: &runcontext.DurableRunInput{Store: f.port, ID: f.port.id}}
				g := gate.New(gate.Options{})
				g.SetOffer(contract.ToolOffer{})
				bindings := &assistantGateBindings{}
				bindings.add(g)
				var events []contract.RunEvent
				var requests [][]byte
				run := &adapter.Run{Prompt: "Inspect the supplied source fixture", ModelName: "fixture", Wire: "openai-compatible", Thinking: "off", Critic: "off", AllowConversation: true, Interactive: true, Context: input, Gate: g.Calls(), HostTools: []runcontext.HostTool{tool}, Emit: func(event contract.RunEvent) { events = append(events, event) }}
				var executor adapter.Adapter = eino.New()
				requestCount, reference := 0, "unavailable-source-result"
				if engine == "native" {
					run.Agent = assistantSourceContractNative(t, &requests)
					executor = native.New()
				} else {
					run.Model = func(_ context.Context, _ string, request relay.Request) (*http.Response, error) {
						requests = append(requests, bytes.Clone(request.Body))
						requestCount++
						if requestCount == 3 {
							if id := assistantSourceReference(request.Body); id != "" {
								reference = id
							}
						}
						w := httptest.NewRecorder()
						names := []string{assistantSourceWire, "list_context", "read_context", "read_window", "search"}
						if requestCount <= len(names) {
							args := json.RawMessage(`{}`)
							if requestCount == 3 || requestCount == 4 {
								args, _ = json.Marshal(map[string]string{"id": reference})
							}
							if requestCount == 5 {
								args = json.RawMessage(`{"pattern":"UNVERIFIED_E0","limit":5}`)
							}
							delta, _ := json.Marshal(map[string]any{"id": "fixture", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprintf("source-call-%d", requestCount), "type": "function", "function": map[string]any{"name": names[requestCount-1], "arguments": string(args)}}}}, "finish_reason": nil}}})
							w.Header().Set("Content-Type", "text/event-stream")
							_, _ = fmt.Fprintf(w, "data: %s\n\ndata: {\"id\":\"fixture\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n", delta)
						} else {
							assistantBriefSSE(w, "Fixture inspection complete.", "stop")
						}
						return w.Result(), nil
					}
				}
				err := bindings.adapter(executor).Run(ctx, run)
				observed, _ := json.MarshalIndent(map[string]any{"adapterError": fmt.Sprint(err), "executions": executions, "verifications": verifications, "identityObservations": identities, "digestObservations": digests, "requests": requests, "events": events}, "", "  ")
				assistantSourceEvidence(t, "adapter-observation.json", observed)
				t.Logf("adapter=%s case=%s err=%v executions=%d receipts=%d identity=%d digest=%d requests=%d", engine, mode, err, executions, verifications, identities, digests, len(requests))
				assistantRunMust(t, err)
				if link.held == nil {
					t.Fatal("actual adapter did not open SessionContext")
				}
				head, err := f.port.Load(ctx, f.port.id)
				assistantRunMust(t, err)
				if head == nil {
					t.Fatal("actual Desk canonical store has no head")
				}
				assistantSourceEvidence(t, "canonical-checkpoint.json", head.Checkpoint)
				value, err := runcontext.ParseJSON(head.Checkpoint)
				assistantRunMust(t, err)
				cp, err := runcontext.ReadExecution(value)
				assistantRunMust(t, err)
				innerDurableSuccess, sourceFailure, readerExposure := false, false, false
				if cp.Durable != nil {
					for _, row := range cp.Durable.Dispatches {
						if row.Tool == assistantSourceWire {
							t.Logf("inner durable tool=%s source=%s state=%s result=%s", row.Tool, row.Source, row.State, row.Result)
							innerDurableSuccess = row.State == "result" && bytes.Contains(row.Result, []byte(assistantSourceSentinel))
						}
					}
				}
				for _, event := range events {
					if result := event.ToolResult; result != nil {
						t.Logf("model tool result name=%s error=%v text=%s structured=%s source=%v", result.Name, result.IsError, result.Text, result.Structured, result.Source)
						if result.Name == assistantSourceWire {
							sourceFailure = result.IsError && strings.Contains(string(result.Structured), `"tool-failed"`) && strings.Contains(string(result.Structured), `"unknown"`)
						}
						if result.Name == "read_context" || result.Name == "read_window" || result.Name == "search" {
							readerExposure = readerExposure || !result.IsError && (strings.Contains(result.Text, assistantSourceSentinel) || bytes.Contains(result.Structured, []byte(assistantSourceSentinel)))
						}
					}
				}
				retained := cp.Context
				retainedBytes, _ := json.Marshal(retained)
				assistantSourceEvidence(t, "retained-context.json", retainedBytes)
				chatBytes, _ := json.Marshal(chat.Entries())
				assistantSourceEvidence(t, "chat-entries.json", chatBytes)
				// Reload the exact stored checkpoint into a fresh chat with the same
				// scope through the original canonical store. The run is complete,
				// and since Agent v0.13.2 (#328) a complete run-store head is not
				// resumed: the reload is refused by name (RunComplete) before
				// anything of the run is bound, so no reloaded context can carry
				// the withheld result to a model. A reload that opens is checked
				// for exposure as before.
				reloadSession := runcontext.Session{Prompt: run.Prompt, Model: &runcontext.ModelSession{Family: "openai-compatible", Model: "fixture"}, HostTools: []runcontext.HostTool{tool}, Critic: "off", Context: &runcontext.Input{
					Execution: cp,
					Chat:      runcontext.NewChatContext(retained.Scope),
					RunStore:  &runcontext.DurableRunInput{Store: f.port, ID: f.port.id, Resume: true},
				}}
				if engine == "native" {
					reloadSession.Model, reloadSession.Agent = nil, &runcontext.AgentSession{Model: "fixture", Effort: "medium"}
				}
				var reloadSnapshot []byte
				reloaded, err := runcontext.ContextualSession(ctx, reloadSession)
				var complete *runcontext.RunComplete
				if cp.Status == "complete" {
					if !errors.As(err, &complete) {
						t.Fatal("a reload of the complete run was not refused as RunComplete", err)
					}
				} else {
					assistantRunMust(t, err)
					defer reloaded.Context.Close()
					reloadSnapshot, _ = json.Marshal(reloaded.Context.Ledger.Snapshot())
				}
				assistantSourceEvidence(t, "reloaded-context.json", reloadSnapshot)
				nextRequestExposure := engine == "api" && len(requests) > 1 && bytes.Contains(requests[1], []byte(assistantSourceSentinel))
				summary, _ := json.Marshal(map[string]any{"innerDurableSuccess": innerDurableSuccess, "sourceFailure": sourceFailure, "nextRequestBeforeReadersExposure": nextRequestExposure, "readerExposure": readerExposure, "retainedExposure": bytes.Contains(retainedBytes, []byte(assistantSourceSentinel)), "chatExposure": bytes.Contains(chatBytes, []byte(assistantSourceSentinel)), "reloadExposure": bytes.Contains(reloadSnapshot, []byte(assistantSourceSentinel)), "referenceLearnedByPeer": reference})
				assistantSourceEvidence(t, "exposure-summary.json", summary)
				t.Logf("receipt withholding evidence %s", summary)
				if mode == "identity-drift" {
					if executions != 0 || verifications != 0 || identities == 0 {
						t.Fatal("changed current identity did not refuse before executor")
					}
				} else if executions != 1 || !sourceFailure {
					t.Fatal("fixture did not establish the exact failed-receipt outcome")
				}
				if mode == "custom-digest" && digests != 0 {
					t.Fatal("custom CurrentDigest unexpectedly became an automatic freshness contract")
				}
				// An inner durable success alone is an ordering observation. The
				// prerequisite fails only on retained or actual model-visible exposure.
				if nextRequestExposure || readerExposure || bytes.Contains(retainedBytes, []byte(assistantSourceSentinel)) || bytes.Contains(reloadSnapshot, []byte(assistantSourceSentinel)) {
					t.Fatal("failed external receipt exposed the unverified result as usable retained/model context; receipt-before-retention or an explicit quarantine API is required")
				}
			}) {
				return // The first fixture/product gate stops all remaining cases.
			}
		}
	}
}
