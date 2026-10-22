package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/gate"
	"github.com/Judgment-Pack/judgment-pack-agent/go/relay"
)

// Hold the real authenticated service execution open while a test drives its
// private capabilities. No model/Runtime implementation is replaced here.
func assistantExecutionFixture(t *testing.T, upstreamURL, phase string, families ...string) (*Server, string, *assistantRunExecution) {
	t.Helper()
	return assistantExecutionFixtureExecute(t, upstreamURL, phase, nil, families...)
}
func assistantExecutionFixtureExecute(t *testing.T, upstreamURL, phase string, execute func(context.Context, *assistantRunExecution) error, families ...string) (*Server, string, *assistantRunExecution) {
	return assistantExecutionFixtureSource(t, upstreamURL, phase, execute, nil, families...)
}
func assistantExecutionFixtureSource(t *testing.T, upstreamURL, phase string, execute func(context.Context, *assistantRunExecution) error, prepare func(*Server, string, assistantRunStart) assistantRunStart, families ...string) (*Server, string, *assistantRunExecution) {
	t.Helper()
	entered := make(chan *assistantRunExecution, 1)
	s, token, start, _ := assistantProtocolFixture(t, func(ctx context.Context, e *assistantRunExecution) error {
		entered <- e
		if execute != nil {
			return execute(ctx, e)
		}
		<-ctx.Done()
		return ctx.Err()
	})
	connection := aiTestConnection(testAIAlpha, "Execution fixture", upstreamURL)
	connection.Assistant.Endpoint.Tools = []string{"validate", "experimental_evaluate", "get_schema"}
	if len(families) > 0 {
		connection.Assistant.Endpoint.Kind = families[0]
	}
	raw, err := encodeAIRegistry(aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{connection}})
	assistantRunMust(t, err)
	assistantRunMust(t, s.assistant.writeConfigNamed(aiConnectionsFile, raw, nil))
	assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, assistantProfilePath), []byte(`{"profileVersion":1,"api":{"inherit":true},"projectInstructions":"HOST-INSTRUCTIONS-ONCE","context":{"version":1,"projectBrief":"execution-brief.md"}}`), 0600))
	assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "execution-brief.md"), []byte("HOST-BRIEF-ONCE"), 0600))
	if phase == "plan" {
		saved := policyWorkspaceRead(t, s, token, "/api/conversations")
		var doc conversationDocument
		assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
		doc.Chats[0] = bytes.Replace(doc.Chats[0], []byte(`"work"`), []byte(`"plan"`), 1)
		if w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, doc); w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		saved = policyWorkspaceRead(t, s, token, "/api/conversations")
		assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
		start.Revision = digestOf(doc.Chats[0])
	}
	if prepare != nil {
		start = prepare(s, token, start)
	}
	origin, _ := endpointOrigin(upstreamURL)
	assistantRunMust(t, s.assistant.storeKeyNamed(connectionKeyName(&connection), storedKey{present: true, key: "host-fixture-key", origin: origin, kind: connection.Assistant.Endpoint.Kind}))
	view := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
	select {
	case e := <-entered:
		if e.record.ID != view.ID {
			t.Fatal("wrong execution")
		}
		t.Cleanup(func() {
			e.cancel()
			select {
			case <-e.done:
			case <-time.After(3 * time.Second):
				t.Error("execution did not drain")
			}
		})
		return s, token, e
	case <-time.After(3 * time.Second):
		t.Fatal("execution did not start")
	}
	return nil, "", nil
}
func assistantExecutionRecord(t *testing.T, e *assistantRunExecution) assistantRunRecord {
	t.Helper()
	r, err := e.writer.lease.store.Read(context.Background(), e.record.Binding, e.record.ID)
	assistantRunMust(t, err)
	return r
}
func assistantExecutionRequest() relay.Request {
	return relay.Request{Headers: map[string]string{"Content-Type": "application/json", "Authorization": "Bearer adapter-secret", "X-Assistant-Connection": "foreign"}, Body: []byte(`{"model":"shared-model","messages":[{"role":"user","content":"EXACT-WIRE"}]}`)}
}
func assistantExecutionRead(t *testing.T, call relay.Call, ctx context.Context) []byte {
	t.Helper()
	response, err := call(ctx, "chat/completions", assistantExecutionRequest())
	assistantRunMust(t, err)
	body, err := io.ReadAll(response.Body)
	assistantRunMust(t, err)
	assistantRunMust(t, response.Body.Close())
	if response.StatusCode != 200 {
		t.Fatalf("relay status %d: %s", response.StatusCode, body)
	}
	return body
}
func TestAssistantExecutionModelActualRelayReceipts(t *testing.T) {
	observed := make(chan assistantRunRecord, 1)
	var e *assistantRunExecution
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		record, err := e.writer.lease.store.Read(r.Context(), e.record.Binding, e.record.ID)
		if err != nil {
			t.Error(err)
		}
		observed <- record
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "{ \"exact\":true }\n")
	})
	_, _, e = assistantExecutionFixture(t, u.server.URL, "work")
	call, err := e.assistantModel()
	assistantRunMust(t, err)
	if got := assistantExecutionRead(t, call, context.Background()); string(got) != "{ \"exact\":true }\n" {
		t.Fatal(string(got))
	}
	before := <-observed
	if len(before.Operations) != 1 || before.Operations[0].State != "dispatched" || before.Operations[0].Result != nil {
		t.Fatalf("outbound preceded durable dispatch: %+v", before.Operations)
	}
	var intent struct {
		Suffix  string
		Request relay.Request
	}
	assistantRunMust(t, json.Unmarshal(before.Operations[0].Intent, &intent))
	if intent.Suffix != "chat/completions" || !bytes.Equal(intent.Request.Body, assistantExecutionRequest().Body) {
		t.Fatal("intent changed exact request")
	}
	after := assistantExecutionRecord(t, e)
	var result struct {
		Status int
		Body   []byte
	}
	assistantRunMust(t, json.Unmarshal(after.Operations[0].Result, &result))
	if after.Operations[0].State != "completed" || result.Status != 200 || string(result.Body) != "{ \"exact\":true }\n" {
		t.Fatal("exact result missing")
	}
	arrival := u.only(t)
	if !bytes.Equal(arrival.body, assistantExecutionRequest().Body) || arrival.header.Get("Authorization") != "Bearer host-fixture-key" || arrival.header.Get(aiConnectionHeader) != "" {
		t.Fatal("adapter credential/header escaped or prompt reinjected")
	}
	bad := assistantExecutionRequest()
	bad.Body = []byte(`{"model":"foreign","messages":[]}`)
	if _, err = call(context.Background(), "chat/completions", bad); err == nil {
		t.Fatal("foreign model admitted")
	}
	if err = e.Dispatch(context.Background(), after.Operations[0].ID); err == nil {
		t.Fatal("completed operation replayed")
	}
	if len(u.arrivals()) != 1 {
		t.Fatal("refusal reached upstream")
	}
}

func TestAssistantExecutionModelQueuedAuthority(t *testing.T) {
	for _, mode := range []string{"source", "connection", "profile", "session", "policy", "persistence"} {
		t.Run(mode, func(t *testing.T) {
			u := newUpstream(t, nil)
			s, token, e := assistantExecutionFixture(t, u.server.URL, "work")
			call, err := e.assistantModel()
			assistantRunMust(t, err)
			for range maxRelayInFlight {
				s.relaySlots <- struct{}{}
			}
			released := false
			release := func() {
				if !released {
					released = true
					for range maxRelayInFlight {
						<-s.relaySlots
					}
				}
			}
			defer release()
			done := make(chan error, 1)
			go func() {
				response, err := call(context.Background(), "chat/completions", assistantExecutionRequest())
				if response != nil {
					_, _ = io.Copy(io.Discard, response.Body)
					_ = response.Body.Close()
				}
				done <- err
			}()
			deadline := time.Now().Add(time.Second)
			for len(s.relayQueue) == 0 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if len(s.relayQueue) == 0 {
				t.Fatal("actual relay queue was bypassed")
			}
			switch mode {
			case "source":
				saved := policyWorkspaceRead(t, s, token, "/api/conversations")
				changed := bytes.ReplaceAll(saved.Content, []byte("Review exact input"), []byte("Changed exact input"))
				var doc conversationDocument
				assistantRunMust(t, json.Unmarshal(changed, &doc))
				if w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, doc); w.Code != 200 {
					t.Fatal(w.Body.String())
				}
			case "connection":
				c := aiTestConnection(testAIAlpha, "Changed revision", u.server.URL)
				raw, _ := encodeAIRegistry(aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{c}})
				assistantRunMust(t, s.assistant.writeConfigNamed(aiConnectionsFile, raw, nil))
			case "profile":
				assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, assistantProfilePath), []byte(`{"profileVersion":1,"api":{"inherit":true},"projectInstructions":"changed"}`), 0600))
			case "session":
				s.sessions.revoke(token)
			case "policy":
				s.signIn.mu.Lock()
				s.signIn.cancelEpoch()
				s.signIn.epoch, s.signIn.cancelEpoch = context.WithCancel(context.Background())
				s.signIn.generation++
				s.signIn.mu.Unlock()
			case "persistence":
				// Replace the retained ledger with a directory: actual persistence
				// must fail before any transport request can leave the queue.
				store, openErr := s.openChatData()
				assistantRunMust(t, openErr)
				ledger := e.writer.lease.store.name
				assistantRunMust(t, store.root.Rename(ledger, ledger+".fixture-held"))
				assistantRunMust(t, store.root.Mkdir(ledger, 0700))
				assistantRunMust(t, store.root.Close())
			}
			release()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("queued call did not end")
			}
			if len(u.arrivals()) != 0 {
				t.Fatal("queued changed authority crossed actual outbound boundary")
			}
			if mode == "persistence" && e.writer.Err() == nil {
				t.Fatal("actual persistence failure was not latched")
			}
		})
	}
}

func TestAssistantExecutionModelInterruptedBody(t *testing.T) {
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = io.WriteString(w, "partial")
	})
	_, _, e := assistantExecutionFixture(t, u.server.URL, "work")
	call, err := e.assistantModel()
	assistantRunMust(t, err)
	response, err := call(context.Background(), "chat/completions", assistantExecutionRequest())
	assistantRunMust(t, err)
	_, err = io.ReadAll(response.Body)
	if err == nil {
		t.Fatal("incomplete provider body became a known receipt")
	}
	_ = response.Body.Close()
	if e.ctx.Err() == nil {
		t.Fatal("uncertain model dispatch left admission open")
	}
	if _, err = call(context.Background(), "chat/completions", assistantExecutionRequest()); err == nil {
		t.Fatal("uncertain dispatch retried")
	}
	<-e.done
	record := assistantExecutionRecord(t, e)
	if len(record.Operations) != 1 || record.Operations[0].State != "uncertain" || record.Operations[0].Result != nil {
		t.Fatalf("lost response was completed: %+v", record.Operations)
	}
	if len(u.arrivals()) != 1 {
		t.Fatal("provider request repeated")
	}
}

// A hand-authored MCP peer runs as the actual rooted child. It reads the
// retained ledger before answering, so moving durability after stdin is caught.
const assistantExecutionPeer = `#!/usr/bin/env python3
import json, os, sys, time
with open('execution-fixture.json') as f: config=json.load(f)
for line in sys.stdin:
 q=json.loads(line)
 with open(config['ledger']) as f: ledger=json.load(f)
 ops=[op for run in ledger['runs'] for op in (run.get('operations') or []) if op['id']==q.get('id')]
 if 'id' in q and (len(ops)!=1 or ops[0]['state']!='dispatched'): sys.exit(31)
 with open('execution-fixture.jsonl','a') as f: f.write(json.dumps(q)+'\n')
 if 'id' not in q: continue
 method=q['method']; p=q.get('params',{}); name=p.get('name','')
 if method=='initialize': result={'protocolVersion':'2025-06-18','capabilities':{},'serverInfo':{'name':'fixture','version':'1'}}
 elif method=='tools/list': result={'tools':[{'name':n,'description':n,'inputSchema':{'type':'object','properties':{}}} for n in ['validate','get_schema','experimental_evaluate','experimental_test_cases','unsafe_extra']]}
 elif method=='prompts/list': result={'prompts':[{'name':'served','arguments':[]},{'name':'requires-input','arguments':[{'name':'input','required':True}]}]}
 elif method=='prompts/get': result={'messages':[{'role':'user','content':{'type':'text','text':'SERVED-RUNTIME-PROMPT'}}]}
 else:
  mode=p.get('arguments',{}).get('fixture','')
  if mode=='hang': time.sleep(30)
  if mode=='oversize': print('x'*(32*1024*1024+1),flush=True);continue
  result={'content':[{'type':'text','text':json.dumps({'cwd':os.path.realpath('.'),'name':name,'exact':'child-result'})}]}
 answer={'jsonrpc':'2.0','id':q['id'],'result':result}
 if p.get('arguments',{}).get('fixture')=='wrong-id': answer['id']='unpaired'
 print(json.dumps(answer,separators=(',',':')),flush=True)
`

func assistantExecutionRuntimeFixture(t *testing.T, s *Server, e *assistantRunExecution) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "execution-runtime")
	assistantRunMust(t, os.WriteFile(bin, []byte(assistantExecutionPeer), 0700))
	s.cfg.JpackBin = bin
	store, err := s.openChatData()
	assistantRunMust(t, err)
	ledgerPath := filepath.Join(store.root.Name(), e.writer.lease.store.name)
	assistantRunMust(t, store.root.Close())
	config, _ := json.Marshal(map[string]string{"ledger": ledgerPath})
	assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "execution-fixture.json"), config, 0600))
}
func assistantExecutionOpen(t *testing.T, s *Server, e *assistantRunExecution) *assistantExecution {
	t.Helper()
	assistantExecutionRuntimeFixture(t, s, e)
	x, err := e.openAssistantExecution(context.Background(), assistantExecutionPrompt{Name: "served"})
	assistantRunMust(t, err)
	t.Cleanup(x.Close)
	return x
}
func assistantExecutionRuntimeLog(t *testing.T, s *Server) []map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(s.cfg.ProjectDir, "execution-fixture.jsonl"))
	assistantRunMust(t, err)
	var rows []map[string]json.RawMessage
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		var row map[string]json.RawMessage
		assistantRunMust(t, json.Unmarshal(line, &row))
		rows = append(rows, row)
	}
	return rows
}
func TestAssistantExecutionRuntimeRootIDsAndReceipts(t *testing.T) {
	s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
	x := assistantExecutionOpen(t, s, e)
	if x.Run.Prompt != "SERVED-RUNTIME-PROMPT\n\nTRANSCRIPT SO FAR\n\n\nTHE PERSON'S MESSAGE\nReview exact input" {
		t.Fatal(x.Run.Prompt)
	}
	for _, tool := range x.Run.Tools {
		if tool.Name == "describe_runtime" || tool.Name == "unsafe_extra" {
			t.Fatal("host metadata or ungranted tool exposed")
		}
	}
	for range 2 {
		answer, err := x.runtime.Call(context.Background(), json.RawMessage(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"validate","arguments":{}}}`))
		assistantRunMust(t, err)
		if !strings.Contains(string(answer), `"id":7`) || !strings.Contains(string(answer), "child-result") {
			t.Fatal(string(answer))
		}
	}
	beforeInvalid := len(assistantExecutionRuntimeLog(t, s))
	for _, id := range []string{`null`, `{}`, `[]`} {
		frame := json.RawMessage(`{"jsonrpc":"2.0","id":` + id + `,"method":"tools/list","params":{}}`)
		if _, err := x.runtime.Call(context.Background(), frame); err == nil {
			t.Fatal("invalid caller ID admitted", id)
		}
	}
	if len(assistantExecutionRuntimeLog(t, s)) != beforeInvalid {
		t.Fatal("invalid caller ID reached child")
	}
	rows := assistantExecutionRuntimeLog(t, s)
	ids := map[string]bool{}
	for _, row := range rows {
		if id := string(row["id"]); id != "" {
			if ids[id] {
				t.Fatal("independent caller IDs aliased", id)
			}
			ids[id] = true
		}
	}
	record := assistantExecutionRecord(t, e)
	for _, op := range record.Operations {
		if op.Kind == "runtime" && (op.State != "completed" || op.Result == nil) {
			t.Fatal("physical Runtime frame not retained", op.ID, op.State)
		}
	}
	// Root custody is measured in the real child's observed cwd.
	last := record.Operations[len(record.Operations)-1]
	if !bytes.Contains(last.Result, []byte(s.cfg.ProjectDir)) {
		t.Fatalf("child escaped project root: %s", last.Result)
	}
	x.Close()
	if x.runtime.cmd.ProcessState == nil || !x.runtime.cmd.ProcessState.Exited() && x.runtime.cmd.ProcessState.Sys() == nil {
		t.Fatal("child not reaped")
	}
	s.mu.Lock()
	_, registered := s.conns[x.runtime.conn]
	s.mu.Unlock()
	if registered {
		t.Fatal("closed Runtime retained registration")
	}
}
func TestAssistantExecutionRuntimeUncertainStops(t *testing.T) {
	for _, mode := range []string{"hang", "wrong-id", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
			assistantExecutionRuntimeFixture(t, s, e)
			r, err := e.assistantRuntime(context.Background())
			assistantRunMust(t, err)
			defer r.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			frame, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "same-caller-id", "method": "tools/call", "params": map[string]any{"name": "validate", "arguments": map[string]string{"fixture": mode}}})
			go func() { _, err := r.Call(ctx, frame); done <- err }()
			if mode == "hang" {
				deadline := time.Now().Add(time.Second)
				for time.Now().Before(deadline) {
					if _, err := os.Stat(filepath.Join(s.cfg.ProjectDir, "execution-fixture.jsonl")); err == nil {
						break
					}
					time.Sleep(time.Millisecond)
				}
				cancel()
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("uncertain Runtime response accepted")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("Runtime did not stop")
			}
			r.Close()
			if r.cmd.ProcessState == nil {
				t.Fatal("uncertain child not reaped")
			}
			if e.ctx.Err() == nil {
				t.Fatal("uncertain Runtime dispatch left run open")
			}
			if _, err = r.Call(context.Background(), frame); err == nil {
				t.Fatal("late call reused transport")
			}
			<-e.done
			record := assistantExecutionRecord(t, e)
			if len(record.Operations) != 1 || record.Operations[0].State != "uncertain" || record.Operations[0].Result != nil {
				t.Fatal("uncertainty was made retryable", record.Operations)
			}
		})
	}
}
func TestAssistantExecutionRuntimePromptAndProjectRefusal(t *testing.T) {
	s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
	assistantExecutionRuntimeFixture(t, s, e)
	r, err := e.assistantRuntime(context.Background())
	assistantRunMust(t, err)
	defer r.Close()
	if _, err = r.Prompt(context.Background(), assistantExecutionPrompt{Name: "requires-input"}); err == nil {
		t.Fatal("omitted served prompt argument admitted")
	}
	if _, err = r.Prompt(context.Background(), assistantExecutionPrompt{Name: "invented"}); err == nil {
		t.Fatal("unserved prompt admitted")
	}
	before := len(assistantExecutionRuntimeLog(t, s))
	s.location.moving.Store(true)
	_, err = r.Call(context.Background(), json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	s.location.moving.Store(false)
	if err == nil {
		t.Fatal("project move fence bypassed")
	}
	if len(assistantExecutionRuntimeLog(t, s)) != before {
		t.Fatal("project-refused request reached child")
	}
}
func TestAssistantExecutionPlanAndChildIntersection(t *testing.T) {
	for _, phase := range []string{"plan", "work"} {
		t.Run(phase, func(t *testing.T) {
			s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", phase)
			x := assistantExecutionOpen(t, s, e)
			if x.Run.Gate.AdmitHost("delegate_tasks") != nil {
				t.Fatal("canonical delegation interaction missing from phase")
			}
			child, tools, err := x.Run.Children(context.Background(), contract.DelegatedTaskProfile{ID: "fixture", RuntimeTools: []string{"validate", "experimental_test_cases", "unsafe_extra"}}, func(contract.RunEvent) {})
			assistantRunMust(t, err)
			for _, tool := range tools {
				if tool.Name == "unsafe_extra" || phase == "plan" && tool.Name == "experimental_test_cases" {
					t.Fatal("child widened parent/grants", tool.Name)
				}
			}
			before := len(assistantExecutionRuntimeLog(t, s))
			result, err := child.CallTool(context.Background(), "experimental_evaluate", json.RawMessage(`{}`))
			assistantRunMust(t, err)
			if !result.IsError {
				t.Fatal("child called outside profile")
			}
			if phase == "plan" {
				result, err = child.CallTool(context.Background(), "experimental_test_cases", json.RawMessage(`{"pack":"{}","matrix":"{}"}`))
				assistantRunMust(t, err)
				if !result.IsError {
					t.Fatal("Plan child lifted test restriction")
				}
				x.Run.Gate.BeginCritic()
				x.Run.Gate.EndCritic()
				result, err = x.Run.Gate.CallTool(context.Background(), "experimental_evaluate", json.RawMessage(`{}`))
				assistantRunMust(t, err)
				if !result.IsError {
					t.Fatal("adapter widened Plan")
				}
				if len(assistantExecutionRuntimeLog(t, s)) != before {
					t.Fatal("refused phase calls reached Runtime")
				}
			} else {
				// The current Desk ceiling does not grant this test tool. The
				// published child phase still charges refused tests to the shared turn.
				for _, calls := range []*gate.Calls{child, child, child, child, x.Run.Gate} {
					result, err = calls.CallTool(context.Background(), "experimental_test_cases", json.RawMessage(`{"pack":"{}","matrix":"{}"}`))
					assistantRunMust(t, err)
					if !result.IsError {
						t.Fatal("ungranted test tool reached Runtime")
					}
				}
				if x.gate.TestCalls() != 3 || len(assistantExecutionRuntimeLog(t, s)) != before {
					t.Fatal("refused child tests lost shared count or reached Runtime")
				}
			}
		})
	}
}
func TestAssistantExecutionEinoServedPromptAndCheckpoint(t *testing.T) {
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"id\":\"fixture\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Fixture answer.\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"fixture\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	})
	s, _, e := assistantExecutionFixture(t, u.server.URL, "work")
	x := assistantExecutionOpen(t, s, e)
	observer := &assistantExecutionPromptObserver{Host: x.Run.Host}
	x.Run.Host = observer
	off := false
	x.Run.Review = &off
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	assistantRunMust(t, x.Adapter.Run(ctx, x.Run))
	record := assistantExecutionRecord(t, e)
	if e.ctx.Err() != nil {
		t.Fatalf("composition canceled: writer=%v events=%+v", e.writer.Err(), record.Events)
	}
	if record.Checkpoint.ID != e.record.Checkpoint.ID || record.Checkpoint.Status != "complete" || record.Checkpoint.Used != 1 {
		t.Fatalf("canonical checkpoint lost: %+v events=%+v", record.Checkpoint, record.Events)
	}
	arrival := u.only(t)
	for _, part := range []string{"SERVED-RUNTIME-PROMPT", "HOST-INSTRUCTIONS-ONCE", "HOST-BRIEF-ONCE"} {
		if strings.Count(string(arrival.body), part) != 1 {
			t.Fatalf("prompt part count for %q: %s", part, arrival.body)
		}
	}
	if observer.brief != 1 || observer.instructions != 1 || observer.memory != 0 || observer.query != "" {
		t.Fatalf("host parts were not requested once with exact current query: %+v", observer)
	}
	if len(record.Checkpoint.Prompts) != 1 {
		t.Fatal("canonical prompt digest missing")
	}
	if !strings.Contains(string(arrival.body), "Review exact input") {
		t.Fatal("current request omitted")
	}
	found := false
	for _, event := range record.Events {
		if event.Event.Message != nil && event.Event.Message.Text == "Fixture answer." {
			found = true
		}
	}
	if !found {
		t.Fatalf("actual Eino answer missing: %+v", record.Events)
	}
}

func TestAssistantExecutionModelFamilies(t *testing.T) {
	for _, row := range []struct{ family, suffix, header, credential, body string }{
		{"openai-compatible", "chat/completions", "Authorization", "Bearer host-fixture-key", `{"model":"shared-model","messages":[{"role":"user","content":"EXACT-FAMILY"}]}`},
		{"anthropic", "v1/messages", "X-Api-Key", "host-fixture-key", `{"model":"shared-model","max_tokens":64,"messages":[{"role":"user","content":"EXACT-FAMILY"}]}`},
		{"gemini", "v1beta/models/shared-model:streamGenerateContent?alt=sse", "X-Goog-Api-Key", "host-fixture-key", `{"contents":[{"role":"user","parts":[{"text":"EXACT-FAMILY"}]}]}`},
	} {
		t.Run(row.family, func(t *testing.T) {
			u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"fixture":"exact-family-response"}`)
			})
			_, _, e := assistantExecutionFixture(t, u.server.URL, "work", row.family)
			call, err := e.assistantModel()
			assistantRunMust(t, err)
			request := relay.Request{Headers: map[string]string{"Content-Type": "application/json", "Anthropic-Version": "2023-06-01"}, Body: []byte(row.body)}
			response, err := call(context.Background(), row.suffix, request)
			assistantRunMust(t, err)
			body, err := io.ReadAll(response.Body)
			assistantRunMust(t, err)
			assistantRunMust(t, response.Body.Close())
			if response.StatusCode != 200 || string(body) != `{"fixture":"exact-family-response"}` {
				t.Fatalf("%s relay completion %d: %s", row.family, response.StatusCode, body)
			}
			arrival := u.only(t)
			path, query, _ := strings.Cut(row.suffix, "?")
			if arrival.path != "/"+path || arrival.rawQuery != query || !bytes.Equal(arrival.body, []byte(row.body)) || arrival.header.Get(row.header) != row.credential {
				t.Fatalf("wrong actual %s wire: %+v", row.family, arrival)
			}
			record := assistantExecutionRecord(t, e)
			if len(record.Operations) != 1 || record.Operations[0].State != "completed" {
				t.Fatal("transport completion receipt missing")
			}
		})
	}
}

type assistantExecutionPromptObserver struct {
	runcontext.Host
	brief, instructions, memory int
	query                       string
}

func (h *assistantExecutionPromptObserver) Brief(ctx context.Context) (string, error) {
	h.brief++
	return h.Host.Brief(ctx)
}
func (h *assistantExecutionPromptObserver) ProjectInstructions(ctx context.Context) (string, error) {
	h.instructions++
	return h.Host.ProjectInstructions(ctx)
}
func (h *assistantExecutionPromptObserver) MemoryFor(ctx context.Context, q string) (string, error) {
	h.memory++
	h.query = q
	return h.Host.MemoryFor(ctx, q)
}

func TestAssistantExecutionForeignCheckpointRefused(t *testing.T) {
	s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
	x := assistantExecutionOpen(t, s, e)
	cp := *e.record.Checkpoint
	cp.ID = "11111111-1111-4111-8111-111111111111"
	x.Run.Emit(contract.RunEvent{ExecutionCheckpoint: &contract.RunEventExecutionCheckpoint{Type: "execution_checkpoint", Checkpoint: cp}})
	if e.ctx.Err() == nil {
		t.Fatal("foreign child checkpoint did not close admission")
	}
	record := assistantExecutionRecord(t, e)
	if record.Checkpoint.ID != e.record.Checkpoint.ID {
		t.Fatal("child replaced root checkpoint")
	}
}

func TestAssistantExecutionLegacyContextMarkerRetains(t *testing.T) {
	u := newUpstream(t, nil)
	s, token, e := assistantExecutionFixture(t, u.server.URL, "work")
	request := httptest.NewRequest("POST", relayPrefix+"chat/completions", bytes.NewReader(assistantExecutionRequest().Body))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set(aiConnectionHeader, e.record.Binding.ConnectionID)
	request.Header.Set(aiRevisionHeader, e.record.Binding.ConnectionRevision)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Assistant-Assembled", "true")
	response := httptest.NewRecorder()
	s.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	arrival := u.only(t)
	if strings.Count(string(arrival.body), "HOST-BRIEF-ONCE") != 1 {
		t.Fatalf("browser suppressed legacy context: %s", arrival.body)
	}
}

// Rebuild every capability with a freshly read writer after the canonical
// explicit budget-extension checkpoint, as the next owner invocation does.
func TestAssistantExecutionReconstructedIdentities(t *testing.T) {
	u := newUpstream(t, nil)
	s, _, e := assistantExecutionFixture(t, u.server.URL, "work")
	first := assistantExecutionOpen(t, s, e)
	message := contract.RunEvent{Message: &contract.RunEventMessage{Type: "message", Text: "retained message"}}
	first.Run.Emit(message)
	assistantExecutionRead(t, first.Run.Model, context.Background())
	first.Close()
	before := assistantExecutionRecord(t, e)
	paused := *before.Checkpoint
	paused.Status = "paused"
	assistantRunMust(t, e.Checkpoint(context.Background(), paused))
	extended, err := runcontext.ExtendExecution(paused, false, time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC))
	assistantRunMust(t, err)
	assistantRunMust(t, e.Checkpoint(context.Background(), *extended))
	continued := *e
	continued.record = assistantExecutionRecord(t, e)
	continued.writer, err = e.writer.lease.Writer(context.Background(), e.record.Binding, e.record.ID)
	assistantRunMust(t, err)
	second, err := continued.openAssistantExecution(context.Background(), assistantExecutionPrompt{Name: "served"})
	assistantRunMust(t, err)
	defer second.Close()
	second.Run.Emit(message)
	child, _, err := second.Run.Children(context.Background(), contract.DelegatedTaskProfile{ID: "fixture", RuntimeTools: []string{"validate"}}, func(contract.RunEvent) {})
	assistantRunMust(t, err)
	freshModel, err := continued.assistantModel()
	assistantRunMust(t, err)
	// Independent child/parent gates and rebuilt model closures share the one
	// durable writer; allocation and append must stay one atomic transaction.
	errors := make(chan error, 4)
	var work sync.WaitGroup
	for _, calls := range []*gate.Calls{child, second.Run.Gate} {
		work.Add(1)
		go func(calls *gate.Calls) {
			defer work.Done()
			_, err := calls.CallTool(context.Background(), "validate", json.RawMessage(`{}`))
			errors <- err
		}(calls)
	}
	for _, call := range []relay.Call{second.Run.Model, freshModel} {
		work.Add(1)
		go func(call relay.Call) {
			defer work.Done()
			response, err := call(context.Background(), "chat/completions", assistantExecutionRequest())
			if err == nil {
				_, err = io.ReadAll(response.Body)
				closeErr := response.Body.Close()
				if err == nil {
					err = closeErr
				}
				if response.StatusCode != 200 {
					err = fmt.Errorf("reconstructed model status %d", response.StatusCode)
				}
			}
			errors <- err
		}(call)
	}
	work.Wait()
	close(errors)
	for err := range errors {
		assistantRunMust(t, err)
	}
	after := assistantExecutionRecord(t, &continued)
	if len(u.arrivals()) != 3 {
		t.Fatal("completed requests replayed or fresh requests lost", len(u.arrivals()))
	}
	if !reflect.DeepEqual(before.Operations, after.Operations[:len(before.Operations)]) || !reflect.DeepEqual(before.Events, after.Events[:len(before.Events)]) {
		t.Fatal("continuation changed retained receipts")
	}
	if len(after.Events) != len(before.Events)+1 {
		t.Fatal("same semantic event incorrectly deduplicated across factories")
	}
	identities := map[string]bool{}
	for _, op := range after.Operations {
		if identities[op.ID] {
			t.Fatal("operation identity reused", op.ID)
		}
		identities[op.ID] = true
		if op.State != "completed" {
			t.Fatal("fresh operation not completed", op.ID, op.State)
		}
	}
	for _, event := range after.Events {
		if identities[event.ID] {
			t.Fatal("event identity reused", event.ID)
		}
		identities[event.ID] = true
	}
	oldModel := before.Operations[len(before.Operations)-1]
	// Explicit IDs retain their original deduplication/replay contract.
	assistantRunMust(t, continued.writer.Intent(context.Background(), oldModelIntent(oldModel)))
	if err = continued.writer.Dispatch(context.Background(), oldModel.ID); err == nil {
		t.Fatal("old completed operation became replayable")
	}
	if !reflect.DeepEqual(oldModel, assistantExecutionRecord(t, &continued).Operations[len(before.Operations)-1]) {
		t.Fatal("old receipt changed after replay refusal")
	}
}
func oldModelIntent(op assistantRunOperation) assistantRunOperation {
	op.State = ""
	op.Result = nil
	return op
}

func TestAssistantExecutionCanceledResponseClose(t *testing.T) {
	for _, mode := range []string{"unread", "partial", "retained-eof"} {
		t.Run(mode, func(t *testing.T) {
			u := newUpstream(t, nil)
			s, _, e := assistantExecutionFixture(t, u.server.URL, "work")
			x := assistantExecutionOpen(t, s, e)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			response, err := x.Run.Model(ctx, "chat/completions", assistantExecutionRequest())
			assistantRunMust(t, err)
			if mode == "partial" {
				one := make([]byte, 1)
				_, err = io.ReadFull(response.Body, one)
				assistantRunMust(t, err)
			}
			if mode == "retained-eof" {
				_, err = io.ReadAll(response.Body)
				assistantRunMust(t, err)
			}
			cancel()
			_ = response.Body.Close()
			if mode == "retained-eof" {
				if e.ctx.Err() != nil {
					t.Fatal("normal retained EOF Close canceled healthy run")
				}
				assistantExecutionRead(t, x.Run.Model, context.Background())
				_, err = x.Run.Gate.CallTool(context.Background(), "validate", json.RawMessage(`{}`))
				assistantRunMust(t, err)
				if len(u.arrivals()) != 2 {
					t.Fatal("healthy receipt closed future dispatch")
				}
				return
			}
			if e.ctx.Err() == nil {
				t.Fatal("canceled unobserved response left root admission open")
			}
			if _, err = x.Run.Model(context.Background(), "chat/completions", assistantExecutionRequest()); err == nil {
				t.Fatal("unknown model dispatch was followed by another request")
			}
			if _, err = x.Run.Gate.CallTool(context.Background(), "validate", json.RawMessage(`{}`)); err == nil {
				t.Fatal("unknown model dispatch was followed by Runtime I/O")
			}
			<-e.done
			record := assistantExecutionRecord(t, e)
			var unknown int
			for _, op := range record.Operations {
				if op.Kind == "model" {
					unknown++
					if op.State != "uncertain" || op.Result != nil {
						t.Fatal("unobserved model outcome became known", op.State)
					}
				}
			}
			if unknown != 1 || len(u.arrivals()) != 1 {
				t.Fatal("unknown request lost or replayed")
			}
		})
	}
}

func TestAssistantExecutionEinoBudgetPause(t *testing.T) {
	var requests atomic.Int32
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		n := requests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"budget\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"budget-%d\",\"type\":\"function\",\"function\":{\"name\":\"validate\",\"arguments\":\"{\\\"step\\\":%d}\"}}]},\"finish_reason\":null}]}\n\ndata: {\"id\":\"budget\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n", n, n)
	})
	proceed := make(chan struct{})
	var x *assistantExecution
	s, token, e := assistantExecutionFixtureExecute(t, u.server.URL, "work", func(ctx context.Context, e *assistantRunExecution) error {
		select {
		case <-proceed:
			return x.Adapter.Run(ctx, x.Run)
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	x = assistantExecutionOpen(t, s, e)
	// Restore a hand-authored near-exhausted canonical checkpoint. Keep the
	// published allowance and grant chain intact; two actual requests remain.
	cp := *e.record.Checkpoint
	cp.Used = cp.Limit - 2
	assistantRunMust(t, e.Checkpoint(context.Background(), cp))
	x.Run.Context.Execution = &cp
	child, _, err := x.Run.Children(context.Background(), contract.DelegatedTaskProfile{ID: "fixture"}, func(contract.RunEvent) {})
	assistantRunMust(t, err)
	refused, err := child.CallTool(context.Background(), "experimental_test_cases", json.RawMessage(`{}`))
	assistantRunMust(t, err)
	if !refused.IsError || x.gate.TestCalls() != 1 {
		t.Fatal("shared test-count fixture was not established")
	}
	off := false
	x.Run.Review = &off
	close(proceed)
	assistantProtocolWait(t, s, token, e.record.ID, "paused-budget")
	record := assistantExecutionRecord(t, e)
	if e.writer.Err() != nil {
		t.Fatal("allowance exhaustion became persistence failure", e.writer.Err())
	}
	if record.Checkpoint.Status != "paused" || record.Checkpoint.Used != int64(runcontext.MaxTurns) || record.Checkpoint.Limit != int64(runcontext.MaxTurns) || record.Checkpoint.TestCalls == nil || *record.Checkpoint.TestCalls != 1 {
		t.Fatalf("canonical pause lost allowance/count: %+v", record.Checkpoint)
	}
	if requests.Load() != 2 || len(record.Checkpoint.Prompts) != 2 {
		t.Fatal("exhaustion sent another provider request", requests.Load())
	}
	for _, event := range record.Events {
		if event.Event.BudgetPaused != nil {
			t.Fatal("budget checkpoint leaked into semantic event sink")
		}
	}
}

func TestAssistantExecutionBudgetPauseRootIdentity(t *testing.T) {
	s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
	x := assistantExecutionOpen(t, s, e)
	cp := *e.record.Checkpoint
	cp.Status = "paused"
	cp.ID = "11111111-1111-4111-8111-111111111111"
	x.Run.Emit(contract.RunEvent{BudgetPaused: &contract.RunEventBudgetPaused{Type: "budget_paused", Checkpoint: cp}})
	if e.ctx.Err() == nil {
		t.Fatal("foreign budget checkpoint left dispatch open")
	}
	if assistantExecutionRecord(t, e).Checkpoint.ID != e.record.Checkpoint.ID {
		t.Fatal("foreign pause replaced root")
	}
}
