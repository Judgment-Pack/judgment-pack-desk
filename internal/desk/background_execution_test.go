package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter/eino"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
	"github.com/Judgment-Pack/judgment-pack-agent/go/gate"
	"github.com/Judgment-Pack/judgment-pack-agent/go/relay"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/aifailure"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
)

func backgroundResearchFixtureContext(t *testing.T, choice backgroundModelChoice) context.Context {
	task := backgroundFixture(choice)
	return backgroundFixtureContext(t, &task)
}
func backgroundFixtureContext(t *testing.T, task *backgroundModelRequest) context.Context {
	return backgroundFixtureContextWithParent(t, context.Background(), task)
}
func backgroundFixtureContextWithParent(t *testing.T, parent context.Context, task *backgroundModelRequest) context.Context {
	t.Helper()
	root, run := researchRunFixture(t)
	a, _, err := beginResearchAttempt(&run, "model", json.RawMessage(`{"step":0}`))
	if err != nil {
		t.Fatal(err)
	}
	if task != nil {
		run.Recipe.AI = task.Choice
	}
	if err = writeInputResearch(root, run); err != nil {
		t.Fatal(err)
	}
	ctx := bindBackgroundAttempt(parent, fileResearchStore(root), run, a)
	if task != nil {
		task.Owner = backgroundAttemptOwner(ctx)
	}
	return ctx
}

func TestBackgroundCanonicalOneRequestRetainsExactTypesAndCheckpoint(t *testing.T) {
	const answer = `{"fields":{"absent":{},"zero":0,"false":false,"null":null,"numberString":"12","decimal":"1.00"}}`
	for _, kind := range []string{"openai-compatible", "anthropic", "gemini"} {
		t.Run(kind, func(t *testing.T) {
			var calls atomic.Int32
			s, _, _, task := backgroundAPIFixture(t, kind, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var body map[string]json.RawMessage
				_ = json.NewDecoder(r.Body).Decode(&body)
				raw, _ := json.Marshal(body)
				if strings.Contains(string(raw), `"tools":[{`) {
					t.Error("final request leaked tools")
				}
				switch kind {
				case "openai-compatible":
					_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": answer}}}})
				case "anthropic":
					_ = json.NewEncoder(w).Encode(map[string]any{"stop_reason": "end_turn", "content": []any{map[string]any{"type": "text", "text": answer}}})
				case "gemini":
					_ = json.NewEncoder(w).Encode(map[string]any{"candidates": []any{map[string]any{"finishReason": "STOP", "content": map[string]any{"parts": []any{map[string]any{"text": answer}}}}}})
				}
			})
			ctx := backgroundFixtureContext(t, &task)
			got, err := s.runBackgroundModel(ctx, task, nil)
			if err != nil || string(got) != answer || calls.Load() != 1 {
				t.Fatalf("got %s %v calls %d", got, err, calls.Load())
			}
			a := ctx.Value(backgroundAttemptKey{}).(*backgroundAttempt)
			run, err := a.store.read()
			if err != nil {
				t.Fatal(err)
			}
			held := run.Attempts[0].Background
			if held == nil || !held.Dispatched || held.Checkpoint == nil || held.Checkpoint.Limit != 1 || held.Checkpoint.Used != 1 || held.Checkpoint.RequestCap == nil || *held.Checkpoint.RequestCap != 1 || string(held.Response) != answer || len(held.Wire) == 0 {
				t.Fatalf("incomplete receipt: %+v", held)
			}
			if _, err = s.runBackgroundModel(ctx, task, nil); err == nil || calls.Load() != 1 {
				t.Fatal("used operation replayed")
			}

			invalid := *held
			badCheckpoint := *held.Checkpoint
			badCheckpoint.Status = "unsupported-status"
			invalid.Checkpoint = &badCheckpoint
			encoded, _ := json.Marshal(invalid)
			var decoded backgroundModelReceipt
			if json.Unmarshal(encoded, &decoded) == nil {
				t.Fatal("canonical checkpoint reader bypassed")
			}

			for _, mutate := range []func(*backgroundModelReceipt){
				func(r *backgroundModelReceipt) { r.Response = json.RawMessage(`{"fields":{"changed":true}}`) },
				func(r *backgroundModelReceipt) { r.Wire = json.RawMessage(`{"changed":true}`) },
			} {
				if _, err = a.store.change(func(run *inputResearchRun) error { mutate(run.Attempts[0].Background); return nil }); err == nil {
					t.Fatal("original journal replaced completed receipt bytes")
				}
			}
			if _, err = a.store.change(func(run *inputResearchRun) error { run.Attempts[0].Background = nil; return nil }); err == nil {
				t.Fatal("original journal cleared one-use receipt")
			}
			full, _ := json.Marshal(run)
			run.Intent = strings.Repeat("x", inputResearchRecordLimit-len(full)+16)
			if _, err = encodeResearchJournal(run, run.ID); err == nil {
				t.Fatal("complete receipt bytes escaped journal cap")
			}
			run.Attempts[0].Background = nil
			if _, err = encodeResearchJournal(run, run.ID); err != nil {
				t.Fatal("journal cap positive control", err)
			}
		})
	}
}

func TestBackgroundCanonicalDefaultGuardAndMalformedCompletion(t *testing.T) {
	privatePath := strings.Join([]string{"", "home", "private", "source"}, "/")
	for _, answer := range []string{`{"fields":{"text":"` + privatePath + `"}}`, `not JSON`, strings.Repeat("x", backgroundModelResponseBytes+1)} {
		t.Run(answer[:8], func(t *testing.T) {
			var calls atomic.Int32
			s, _, _, task := backgroundAPIFixture(t, "openai-compatible", func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": answer}}}})
			})
			ctx := backgroundFixtureContext(t, &task)
			got, err := s.runBackgroundModel(ctx, task, nil)
			if calls.Load() != 1 {
				t.Fatal("extra operation")
			}
			if len(answer) > backgroundModelResponseBytes {
				if err == nil {
					t.Fatal("oversize accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(got), privatePath) {
				t.Fatal("default output guard disabled")
			}
			if answer == "not JSON" && string(got) != `"not JSON"` {
				t.Fatalf("malformed completed text lost: %s", got)
			}
		})
	}
}

type backgroundLostAckJournal struct {
	researchJournal
	fail func(inputResearchRun) bool
}

func (j backgroundLostAckJournal) Update(ctx context.Context, f func(*inputResearchRun) error) (inputResearchRun, error) {
	run, err := j.researchJournal.Update(ctx, f)
	if err == nil && j.fail(run) {
		return inputResearchRun{}, errors.New("acknowledgement lost")
	}
	return run, err
}
func TestBackgroundCanonicalLostAcknowledgementAndRecoveryNeverReplay(t *testing.T) {
	for _, afterDispatch := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-dispatch", true: "completed-response"}[afterDispatch], func(t *testing.T) {
			var calls atomic.Int32
			s, _, _, task := backgroundAPIFixture(t, "openai-compatible", func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"{\"fields\":{}}"}}]}`))
			})
			ctx := backgroundFixtureContext(t, &task)
			a := ctx.Value(backgroundAttemptKey{}).(*backgroundAttempt)
			original := a.store.journal
			a.store.journal = backgroundLostAckJournal{original, func(run inputResearchRun) bool {
				held := run.Attempts[0].Background
				return held != nil && (afterDispatch && len(held.Wire) > 0 || !afterDispatch && held.Checkpoint != nil)
			}}
			if _, err := s.runBackgroundModel(ctx, task, nil); err == nil {
				t.Fatal("unacknowledged save accepted")
			}
			if calls.Load() != map[bool]int32{false: 0, true: 1}[afterDispatch] {
				t.Fatal("dispatch crossed failed checkpoint")
			}
			a.store.journal = original
			run, err := a.store.change(func(run *inputResearchRun) error { recoverInputResearch(run); return nil })
			if err != nil {
				t.Fatal(err)
			}
			reconstructed := bindBackgroundAttempt(context.Background(), a.store, run, run.Attempts[0])
			if _, err = s.runBackgroundModel(reconstructed, task, nil); err == nil || calls.Load() > 1 {
				t.Fatal("reconstruction replayed uncertain operation")
			}
			if run.Attempts[0].Background == nil || run.Attempts[0].Background.Checkpoint == nil {
				t.Fatal("checkpoint lost on recovery")
			}
		})
	}
}

func TestBackgroundReceiptStrictMembersAndBounds(t *testing.T) {
	valid := backgroundModelReceipt{Version: 1, Owner: "workflow/attempt", Digest: strings.Repeat("a", 64), Project: strings.Repeat("b", 64), Profile: strings.Repeat("c", 64), Choice: backgroundModelChoice{Connection: "named", Revision: "revision", Model: "model"}}
	raw, _ := json.Marshal(valid)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	for _, name := range []string{"version", "owner", "digest", "project", "profile", "choice", "dispatched"} {
		t.Run(name, func(t *testing.T) {
			copy := map[string]json.RawMessage{}
			for k, v := range fields {
				copy[k] = v
			}
			delete(copy, name)
			mutant, _ := json.Marshal(copy)
			var got backgroundModelReceipt
			if json.Unmarshal(mutant, &got) == nil {
				t.Fatal("missing required member accepted")
			}
		})
	}
	for _, change := range []func(*backgroundModelReceipt){func(r *backgroundModelReceipt) {
		r.Response = json.RawMessage(`"` + strings.Repeat("x", backgroundModelResponseBytes) + `"`)
	}, func(r *backgroundModelReceipt) { r.Checkpoint = &contract.Checkpoint{} }} {
		mutant := valid
		change(&mutant)
		raw, _ = json.Marshal(mutant)
		var got backgroundModelReceipt
		if json.Unmarshal(raw, &got) == nil {
			t.Fatal("invalid receipt accepted")
		}
	}
	if err := json.Unmarshal(append(raw[:len(raw)-1], []byte(`,"unknown":true}`)...), &valid); err == nil {
		t.Fatal("unknown member accepted")
	}
	oversized := valid
	oversized.Choice.Model = strings.Repeat("x", backgroundModelReceiptBytes)
	encoded, _ := json.Marshal(oversized)
	var decoded backgroundModelReceipt
	if json.Unmarshal(encoded, &decoded) == nil {
		t.Fatal("complete receipt escaped encoded bound")
	}
}

// The actual private Manager exercises discovery, account admission, generation,
// aliases and callback custody; the fixture supplies every launched executable.
func TestBackgroundCanonicalActualNativeManager(t *testing.T) {
	s, _, _ := agentServer(t, &agentStub{})
	fixture := t.TempDir()
	binary := filepath.Join(fixture, "peer")
	peer := filepath.Join(fixture, "peer.py")
	code := strings.ReplaceAll(backgroundNativePeer, "RESPONSE", `{"fields":{"zero":0,"eligible":false,"amount":"1.00"}}`)
	if err := os.WriteFile(peer, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexec python3 '"+peer+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	manager, err := codexbridge.NewManager(codexbridge.Options{Binary: binary, ProfileDir: filepath.Join(fixture, "account"), ProjectDir: s.cfg.ProjectDir})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	s.codex = manager
	registry, err := s.readAIRegistry()
	if err != nil {
		t.Fatal(err)
	}
	var choice backgroundModelChoice
	for _, c := range registry.Connections {
		if c.ID == "legacy-codex" {
			choice = backgroundModelChoice{Connection: c.ID, Revision: c.Revision, Model: "model", Effort: "medium"}
		}
	}
	task := backgroundFixture(choice)
	ctx := backgroundFixtureContext(t, &task)
	result, err := s.runBackgroundModel(ctx, task, nil)
	if err != nil || string(result) != `{"fields":{"zero":0,"eligible":false,"amount":"1.00"}}` {
		t.Fatalf("native result %s %v", result, err)
	}
	a := ctx.Value(backgroundAttemptKey{}).(*backgroundAttempt)
	run, err := a.store.read()
	if err != nil {
		t.Fatal(err)
	}
	held := run.Attempts[0].Background
	if !held.Dispatched || held.Checkpoint.Unit != "provider-managed" || held.Checkpoint.RequestCap != nil || held.Checkpoint.Used != 0 || held.Checkpoint.Limit != 1 || !bytes.Contains(held.Wire, []byte(`"arguments":{"fields":{"zero":0,"eligible":false,"amount":"1.00"}}`)) || !bytes.Contains(held.Wire, []byte(`"answer":`)) {
		t.Fatal("native custody or accounting lost")
	}
}

const backgroundNativePeer = `import json,sys,os
def write(v): print(json.dumps(v,separators=(',',':')),flush=True)
def note(method,p): write({'method':method,'params':p})
request={}
for line in sys.stdin:
 q=json.loads(line);method=q.get('method','');p=q.get('params',{})
 def reply(v):write({'id':q['id'],'result':v})
 if method=='initialize':reply({'userAgent':'jps_desk/0.157.1 (Linux)'})
 elif method=='initialized':pass
 elif method=='account/read':reply({'requiresOpenaiAuth':True,'account':{'type':'chatgpt','planType':'plus'}})
 elif method=='model/list':
  if p.get('includeHidden'):
   with open(os.path.join(os.environ['CODEX_HOME'],'model-catalog.json')) as f:catalog=json.load(f)
   reply({'data':[{'model':m['slug']} for m in catalog['models']]})
  else:reply({'data':[{'model':'model','displayName':'model','defaultReasoningEffort':'medium','supportedReasoningEfforts':[{'reasoningEffort':'medium'}]}]})
 elif method=='thread/start':
  request=p
  if p['approvalPolicy']!='never' or p['allowProviderModelFallback']!=False or p['permissions']!='jps' or p['environments']!=[]:sys.exit(72)
  reply({'thread':{'id':'thread'},'model':p['model'],'modelProvider':'openai','approvalPolicy':'never','cwd':os.getcwd()})
 elif method=='turn/start':
  reply({'turn':{'id':'turn'}})
  assert len(request['dynamicTools'])==1
  tool=next(t['name'] for t in request['dynamicTools'] if t['description'].startswith('desk_answer:'))
  write({'id':'rpc','method':'item/tool/call','params':{'threadId':'thread','turnId':'turn','callId':'call','tool':tool,'arguments':json.loads('RESPONSE')}})
 elif not method:
  note('item/completed',{'threadId':'thread','turnId':'turn','item':{'id':'message','type':'agentMessage','phase':'final_answer','text':'Done.'}})
  note('turn/completed',{'threadId':'thread','turn':{'id':'turn','status':'completed'}})
 else:sys.exit(74)
`

func TestBackgroundCanonicalFinishingClosedAndCheckpointResume(t *testing.T) {
	for _, kind := range []string{"openai-compatible", "anthropic", "gemini"} {
		for _, truncated := range []bool{false, true} {
			t.Run(kind+map[bool]string{false: "-empty", true: "-truncated"}[truncated], func(t *testing.T) {
				var physical atomic.Int32
				s, _, _, task := backgroundAPIFixture(t, kind, func(w http.ResponseWriter, r *http.Request) {
					physical.Add(1)
					reason, text := "stop", ""
					if truncated {
						reason, text = "length", `{"fields":{}}`
					}
					switch kind {
					case "openai-compatible":
						_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": reason, "message": map[string]any{"content": text}}}})
					case "anthropic":
						if truncated {
							reason = "max_tokens"
						} else {
							reason = "end_turn"
						}
						_ = json.NewEncoder(w).Encode(map[string]any{"stop_reason": reason, "content": []any{map[string]any{"type": "text", "text": text}}})
					case "gemini":
						if truncated {
							reason = "MAX_TOKENS"
						} else {
							reason = "STOP"
						}
						_ = json.NewEncoder(w).Encode(map[string]any{"candidates": []any{map[string]any{"finishReason": reason, "content": map[string]any{"parts": []any{map[string]any{"text": text}}}}}})
					}
				})
				ctx := backgroundFixtureContext(t, &task)
				if raw, err := s.runBackgroundModel(ctx, task, nil); err == nil || len(raw) != 0 || physical.Load() != 1 {
					t.Fatalf("unfinished response %s %v calls=%d", raw, err, physical.Load())
				}
				a := ctx.Value(backgroundAttemptKey{}).(*backgroundAttempt)
				held, err := a.store.read()
				if err != nil {
					t.Fatal(err)
				}
				cp := *held.Attempts[0].Background.Checkpoint
				raw, _ := json.Marshal(cp)
				value, err := runcontext.ParseJSON(raw)
				if err != nil {
					t.Fatal(err)
				}
				parsed, err := runcontext.ReadExecution(value)
				if err != nil || parsed.RequestCap == nil || *parsed.RequestCap != 1 || parsed.Used != 1 || parsed.Limit != 1 {
					t.Fatalf("checkpoint lost cap: %+v %v", parsed, err)
				}
				cp.Status = "stopped"
				cap := 1
				capabilityCalls := 0
				g := gate.New(gate.Options{})
				g.SetOffer(contract.ToolOffer{Tools: []contract.ToolOfferRuntimeTool{}, HostTools: []contract.ToolOfferHostTool{}})
				run := &adapter.Run{Wire: kind, ModelName: task.Choice.Model, Prompt: task.Prompt, AllowConversation: true, Critic: "off", Gate: g.Calls(), Context: &runcontext.Input{RequestCap: &cap, CurrentRequest: &runcontext.CurrentRequest{Message: task.Owner, Input: task.Prompt}, Execution: &cp}, Emit: func(contract.RunEvent) {}, Model: func(context.Context, string, relay.Request) (*http.Response, error) {
					capabilityCalls++
					return nil, errors.New("must not dispatch")
				}}
				if err := eino.New().Run(context.Background(), run); err != nil {
					t.Fatal(err)
				}
				if capabilityCalls != 0 {
					t.Fatal("resumed one-request operation called capability")
				}
				reconstructed := bindBackgroundAttempt(context.Background(), a.store, held, held.Attempts[0])
				if _, err := s.runBackgroundModel(reconstructed, task, nil); err == nil || physical.Load() != 1 {
					t.Fatal("reconstructed receipt replayed")
				}
			})
		}
	}
}

func TestBackgroundCanonicalCurrentCustodyAtWaitAndRetention(t *testing.T) {
	for _, when := range []string{"queued", "response"} {
		for _, change := range []string{"choice", "profile", "credential", "brief", "epoch", "stop"} {
			t.Run(when+"-"+change, func(t *testing.T) {
				var physical atomic.Int32
				var mutate func()
				s, ts, registry, task := backgroundAPIFixture(t, "openai-compatible", func(w http.ResponseWriter, r *http.Request) {
					physical.Add(1)
					if when == "response" {
						mutate()
					}
					_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"{\"fields\":{}}"}}]}`)
				})
				if err := os.WriteFile(filepath.Join(s.projectDir, assistantProfilePath), []byte(`{"profileVersion":2,"inherit":true,"context":{"version":1,"projectBrief":"context.md"}}`), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(s.projectDir, "context.md"), []byte("Reviewed source"), 0600); err != nil {
					t.Fatal(err)
				}
				ctx := backgroundFixtureContext(t, &task)
				a := ctx.Value(backgroundAttemptKey{}).(*backgroundAttempt)
				mutate = func() {
					var err error
					switch change {
					case "choice":
						_, err = a.store.change(func(r *inputResearchRun) error { r.Recipe.AI.Model = "different-model"; return nil })
					case "stop":
						_, err = a.store.change(func(r *inputResearchRun) error { r.StopRequested = true; return nil })
					case "profile":
						err = os.WriteFile(filepath.Join(s.projectDir, assistantProfilePath), []byte(`{"profileVersion":2,"inherit":true,"projectInstructions":"Changed"}`), 0600)
					case "brief":
						err = os.WriteFile(filepath.Join(s.projectDir, "context.md"), []byte("Changed source"), 0600)
					case "credential":
						c := registry.Connections[0]
						code, raw := aiTestRequest(t, ts, "PUT", "/api/assistant/key", map[string]string{"key": "changed-fixture-key"}, map[string]string{aiConnectionHeader: c.ID, aiRevisionHeader: c.Revision})
						if code != 200 {
							t.Errorf("credential: %d %s", code, raw)
						}
					case "epoch":
						s.signIn.mu.Lock()
						s.signIn.cancelEpoch()
						s.signIn.mu.Unlock()
					}
					if err != nil {
						t.Error(err)
					}
				}
				_, err := s.runBackgroundModel(ctx, task, func(waiting bool) error {
					if waiting && when == "queued" {
						mutate()
					}
					return nil
				})
				want := int32(0)
				if when == "response" {
					want = 1
				}
				if err == nil || physical.Load() != want {
					t.Fatalf("changed custody accepted: %v calls=%d", err, physical.Load())
				}
				held, readErr := a.store.read()
				if readErr != nil {
					t.Fatal(readErr)
				}
				if len(held.Attempts[0].Background.Response) > 0 || len(held.Attempts[0].Background.Wire) > 0 {
					t.Fatal("stale response retained as accepted")
				}
			})
		}
	}
}

func TestBackgroundCanonicalFailureBoundaryAndUnknownDelivery(t *testing.T) {
	for _, native := range []bool{false, true} {
		held := aifailure.Authored("permission", "model", "not-started", "Host refusal.")
		held.Status = 403
		held.RequestID = "req_fixture"
		got := backgroundCanonicalFailure(held, native)
		var canonical *failure.Error
		if !errors.As(got, &canonical) || canonical.Failure.Code != "permission" || canonical.Failure.Outcome != "not-started" || canonical.Failure.Status == nil || *canonical.Failure.Status != 403 || canonical.Failure.RequestID == nil || *canonical.Failure.RequestID != "req_fixture" {
			t.Fatalf("typed metadata lost: %+v", got)
		}
	}
	var physical atomic.Int32
	s, _, _, task := backgroundAPIFixture(t, "openai-compatible", func(w http.ResponseWriter, r *http.Request) {
		physical.Add(1)
		h, ok := w.(http.Hijacker)
		if !ok {
			t.Error("hijack missing")
			return
		}
		c, _, err := h.Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = c.Close()
	})
	ctx := backgroundFixtureContext(t, &task)
	if _, err := s.runBackgroundModel(ctx, task, nil); err == nil {
		t.Fatal("unknown delivery succeeded")
	}
	a := ctx.Value(backgroundAttemptKey{}).(*backgroundAttempt)
	held, err := a.store.read()
	if err != nil {
		t.Fatal(err)
	}
	if !held.Attempts[0].Background.Dispatched || held.Attempts[0].Background.Failure == nil || held.Attempts[0].Background.Failure.Outcome != "unknown" {
		t.Fatal("unknown outcome lost")
	}
	reconstructed := bindBackgroundAttempt(context.Background(), a.store, held, held.Attempts[0])
	if _, err := s.runBackgroundModel(reconstructed, task, nil); err == nil || physical.Load() != 1 {
		t.Fatal("uncertain delivery replayed")
	}
}

func TestBackgroundCanonicalWorkflowChoiceAndImmutableReceipt(t *testing.T) {
	choice := backgroundModelChoice{Connection: "named", Revision: "revision", Model: "model"}
	a := backgroundAttempt{revision: "reviewed", gateway: inputResearchPin{Authority: "authority", Public: "public"}}
	run := inputResearchRun{Revision: a.revision, Gateway: a.gateway, Recipe: inputResearchRecipe{AI: choice}}
	if !a.matches(run, choice) {
		t.Fatal("legacy positive control refused")
	}
	run.Recipe.AI.Model = "changed"
	if a.matches(run, choice) {
		t.Fatal("changed recipe choice admitted")
	}
	run.Portable = &portableResearchJournal{Frozen: portableResearchFrozen{Choice: choice}}
	if !a.matches(run, choice) {
		t.Fatal("portable positive control refused")
	}
	run.Portable.Frozen.Choice.Effort = "high"
	if a.matches(run, choice) {
		t.Fatal("changed portable choice admitted")
	}
	r := &backgroundModelReceipt{Version: 1, Owner: "run/attempt", Digest: strings.Repeat("a", 64), Project: strings.Repeat("b", 64), Profile: strings.Repeat("c", 64), Choice: choice, Dispatched: true, Response: json.RawMessage(`{"fields":{}}`), Wire: json.RawMessage(`{"complete":true}`)}
	old := inputResearchAttempt{Kind: "model", State: "started", Background: r}
	for name, mutate := range map[string]func(*backgroundModelReceipt){
		"identity": func(r *backgroundModelReceipt) { r.Owner = "other" }, "dispatch": func(r *backgroundModelReceipt) { r.Dispatched = false }, "result": func(r *backgroundModelReceipt) { r.Response = json.RawMessage(`{}`) }, "wire": func(r *backgroundModelReceipt) { r.Wire = nil }, "choice": func(r *backgroundModelReceipt) { r.Choice.Model = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			copy := *r
			mutate(&copy)
			next := old
			next.Background = &copy
			if backgroundReceiptTransition(old, next) {
				t.Fatal("receipt changed")
			}
		})
	}
	if !backgroundReceiptTransition(old, old) {
		t.Fatal("unchanged receipt refused")
	}
}

func TestBackgroundCanonicalNativeRequiresValidatedResponseTool(t *testing.T) {
	stub := &agentStub{}
	s, _, _ := agentServer(t, stub)
	registry, err := s.readAIRegistry()
	if err != nil {
		t.Fatal(err)
	}
	var choice backgroundModelChoice
	for _, c := range registry.Connections {
		if c.ID == "legacy-codex" {
			choice = backgroundModelChoice{Connection: c.ID, Revision: c.Revision, Model: "model"}
		}
	}
	task := backgroundFixture(choice)
	stub.run = func(ctx context.Context, owner codexbridge.Owner, req codexbridge.RunRequest, emit func(codexbridge.RunEvent) error, tool codexbridge.ToolHandler) error {
		if err := req.AuthorizeAdmission(ctx); err != nil {
			return err
		}
		if err := req.BeforeGeneration(ctx); err != nil {
			return err
		}
		return emit(codexbridge.RunEvent{Type: "message", ID: "final", Phase: "final_answer", Text: `{"fields":{}}`})
	}
	if raw, err := s.runBackgroundModel(backgroundFixtureContext(t, &task), task, nil); err == nil || len(raw) > 0 {
		t.Fatal("native prose bypassed host response tool", string(raw), err)
	}
}

func TestBackgroundCanonicalRetainsExactWireBytes(t *testing.T) {
	const wire = "{ \"choices\": [{\"finish_reason\":\"stop\", \"message\":{\"content\":\"{\\\"fields\\\":{\\\"n\\\":\\\"1.00\\\"}}\"}}], \"usage\":{\"prompt_tokens\":1,\"completion_tokens\":2}, \"rawDecimal\":1.00 }\n"
	s, _, _, task := backgroundAPIFixture(t, "openai-compatible", func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, wire) })
	ctx := backgroundFixtureContext(t, &task)
	if raw, err := s.runBackgroundModel(ctx, task, nil); err != nil || string(raw) != `{"fields":{"n":"1.00"}}` {
		t.Fatal(string(raw), err)
	}
	a := ctx.Value(backgroundAttemptKey{}).(*backgroundAttempt)
	run, err := a.store.read()
	if err != nil {
		t.Fatal(err)
	}
	var held struct {
		Status int    `json:"status"`
		Body   []byte `json:"body"`
	}
	if err = json.Unmarshal(run.Attempts[0].Background.Wire, &held); err != nil || held.Status != 200 || string(held.Body) != wire {
		t.Fatal("wire result changed", err)
	}
}

func TestBackgroundCanonicalNativeFailurePhase(t *testing.T) {
	for _, generated := range []bool{false, true} {
		t.Run(map[bool]string{false: "not-started", true: "unknown"}[generated], func(t *testing.T) {
			stub := &agentStub{}
			s, _, _ := agentServer(t, stub)
			registry, err := s.readAIRegistry()
			if err != nil {
				t.Fatal(err)
			}
			var choice backgroundModelChoice
			for _, c := range registry.Connections {
				if c.ID == "legacy-codex" {
					choice = backgroundModelChoice{Connection: c.ID, Revision: c.Revision, Model: "model"}
				}
			}
			task := backgroundFixture(choice)
			ctx := backgroundFixtureContext(t, &task)
			calls := 0
			stub.run = func(ctx context.Context, _ codexbridge.Owner, req codexbridge.RunRequest, _ func(codexbridge.RunEvent) error, _ codexbridge.ToolHandler) error {
				calls++
				if generated {
					if err := req.AuthorizeAdmission(ctx); err != nil {
						return err
					}
					if err := req.BeforeGeneration(ctx); err != nil {
						return err
					}
				}
				return codexbridge.ErrBusy
			}
			_, err = s.runBackgroundModel(ctx, task, nil)
			var held *aifailure.Failure
			outcome := map[bool]string{false: "not-started", true: "unknown"}[generated]
			if !errors.As(err, &held) || held.Code != "busy" || held.Outcome != outcome || calls != 1 {
				t.Fatalf("native failure %+v calls%d", held, calls)
			}
			a := ctx.Value(backgroundAttemptKey{}).(*backgroundAttempt)
			run, err := a.store.read()
			if err != nil {
				t.Fatal(err)
			}
			r := run.Attempts[0].Background
			if r.Dispatched != generated || (len(r.Wire) > 0) != generated || r.Failure == nil || r.Failure.Outcome != outcome {
				t.Fatalf("phase receipt %+v", r)
			}
		})
	}
}

func TestBackgroundCanonicalReservesWholeReceiptBeforeDispatch(t *testing.T) {
	for _, portable := range []bool{false, true} {
		_, run := researchRunFixture(t)
		if portable {
			run.Portable = &portableResearchJournal{}
			run.Version = 2
		}
		a := inputResearchAttempt{Kind: "model"}
		if err := reserveResearchSource(&run, &a); err != nil {
			t.Fatal(err)
		}
		if a.ReservedBytes < portableResearchAttemptReserve+backgroundModelReceiptReserve {
			t.Fatal("receipt not reserved")
		}
		run.Intent = strings.Repeat("x", inputResearchRecordLimit-backgroundModelReceiptReserve)
		if err := reserveResearchSource(&run, &inputResearchAttempt{Kind: "model"}); err == nil {
			t.Fatal("near-capacity model admitted")
		}
	}
	var calls atomic.Int32
	const body = `{"choices":[{"finish_reason":"stop","message":{"content":"{\"fields\":{}}"}}]}`
	wirePayload := ((2 << 20) - len(`{"status":200,"body":""}`)) / 4 * 3
	wire := body + strings.Repeat(" ", wirePayload-len(body))
	s, _, _, task := backgroundAPIFixture(t, "openai-compatible", func(w http.ResponseWriter, r *http.Request) { calls.Add(1); _, _ = io.WriteString(w, wire) })
	ctx := backgroundFixtureContext(t, &task)
	if _, err := s.runBackgroundModel(ctx, task, nil); err != nil {
		t.Fatal("bounded complete receipt not retained", err)
	}
	a := ctx.Value(backgroundAttemptKey{}).(*backgroundAttempt)
	run, err := a.store.read()
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || len(run.Attempts[0].Background.Wire) < (2<<20)-4 {
		t.Fatal("full bounded wire missing")
	}
	// The owning workflow refuses admission before invoking the model callback.
	root, near := researchRunFixture(t)
	near.Intent = strings.Repeat("x", inputResearchRecordLimit-backgroundModelReceiptReserve)
	if err := writeInputResearch(root, near); err != nil {
		t.Fatal(err)
	}
	if _, err := researchAttempt(context.Background(), fileResearchStore(root), "model", json.RawMessage(`{}`), func(context.Context, inputResearchRun, inputResearchAttempt) (json.RawMessage, error) {
		calls.Add(1)
		return nil, nil
	}); err == nil || calls.Load() != 1 {
		t.Fatal("near-capacity admission dispatched model")
	}
}

func TestBackgroundCanonicalRefusesMissingCompletionAndUnrequestedTools(t *testing.T) {
	for _, tc := range []struct{ name, kind, wire string }{
		{"omitted", "openai-compatible", `{"choices":[{"message":{"content":"{\"fields\":{}}"}}]}`},
		{"openai-tool", "openai-compatible", `{"choices":[{"finish_reason":"stop","message":{"content":"{\"fields\":{}}","tool_calls":[{"id":"one","type":"function","function":{"name":"write_pack","arguments":"{}"}}]}}]}`},
		{"anthropic-tool", "anthropic", `{"stop_reason":"end_turn","content":[{"type":"text","text":"{\"fields\":{}}"},{"type":"tool_use","id":"one","name":"write_pack","input":{}}]}`},
		{"gemini-tool", "gemini", `{"candidates":[{"finishReason":"STOP","content":{"parts":[{"text":"{\"fields\":{}}"},{"functionCall":{"name":"write_pack","args":{}}}]}}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			s, _, _, task := backgroundAPIFixture(t, tc.kind, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); _, _ = io.WriteString(w, tc.wire) })
			ctx := backgroundFixtureContext(t, &task)
			raw, err := s.runBackgroundModel(ctx, task, nil)
			var held *aifailure.Failure
			if len(raw) > 0 || !errors.As(err, &held) || held.Code != "invalid-response" || calls.Load() != 1 {
				t.Fatalf("unexpected response %s %+v calls%d", raw, held, calls.Load())
			}
			a := ctx.Value(backgroundAttemptKey{}).(*backgroundAttempt)
			run, err := a.store.read()
			if err != nil {
				t.Fatal(err)
			}
			if run.Attempts[0].Background.Checkpoint.Used != 1 || len(run.Attempts[0].Background.Response) > 0 {
				t.Fatal("unexpected action accepted or extra request claimed")
			}
		})
	}
}
