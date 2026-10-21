package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

func TestAssistantGraphStaleReadCustody(t *testing.T) {
	for _, mode := range []string{"file", "deleted-file", "path", "description", "deleted-declaration", "child-only", "reconstructed-narrative"} {
		t.Run(mode, func(t *testing.T) {
			s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
			x := assistantGraphSetup(t, s, e)
			if mode != "reconstructed-narrative" && mode != "child-only" {
				if assistantGraphInvoke(t, e, x, "root-read", "read_decision", map[string]string{"type": "graph", "id": "flow"}).Err() {
					t.Fatal("read failed")
				}
			}
			switch mode {
			case "file":
				assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "flow.graph.json"), []byte(assistantGraphOriginal+" "), 0600))
			case "deleted-file":
				assistantRunMust(t, os.Remove(filepath.Join(s.cfg.ProjectDir, "flow.graph.json")))
			case "path":
				assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "other.graph.json"), []byte(assistantGraphOriginal), 0600))
				assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "jpack.json"), []byte(strings.Replace(assistantGraphConfig, "flow.graph.json", "other.graph.json", 1)), 0600))
			case "description":
				assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "jpack.json"), []byte(strings.Replace(assistantGraphConfig, "A graph", "Changed graph", 1)), 0600))
			case "deleted-declaration":
				assistantRunMust(t, os.Remove(filepath.Join(s.cfg.ProjectDir, "flow.graph.json")))
				assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "jpack.json"), []byte(`{"configVersion":"5","packs":{"one":{"path":"one.json"}},"graphs":{}}`), 0600))
			case "child-only":
				args := json.RawMessage(`{"type":"graph","id":"flow"}`)
				result, err := assistantGraphFind(t, x.Run.HostTools, "read_decision").Execute(runcontext.WithCallID(t.Context(), "child-read"), args)
				assistantRunMust(t, err)
				if result.Err() {
					t.Fatal("child read failed", result.Value().Text())
				}
			case "reconstructed-narrative":
				cp := *assistantExecutionRecord(t, e).Checkpoint
				cp.AdditionalMembers = map[string]json.RawMessage{"graphRead": json.RawMessage(`{"id":"flow","path":"flow.graph.json","sha256":"` + digestOf([]byte(assistantGraphOriginal)) + `"}`)}
				assistantRunMust(t, e.Checkpoint(t.Context(), cp))
			}
			registry, err := e.assistantGraphTools(x.runtime)
			assistantRunMust(t, err)
			x.Run.HostTools = registry.tools
			result := assistantGraphInvoke(t, e, x, "stale-proposal", "propose_graph", map[string]string{"id": "flow", "content": assistantGraphCandidate})
			if !result.Err() || len(assistantExecutionRecord(t, e).DomainOutputs) != 0 {
				t.Fatal("stale or foreign read authorized proposal", result.Value().Text())
			}
		})
	}
}
func TestAssistantGraphMalformedScopeAndRegistry(t *testing.T) {
	for _, args := range []string{`[]`, `{"id":"flow","content":"{}","path":"foreign"}`, `{"id":"flow","id":"other","content":"{}"}`, `{"ID":"flow","content":"{}"}`, `{"id":"flow","content":null}`, `{"id":"flow","content":"{}","Description":"alias"}`, `{"id":"../outside","content":"{}"}`} {
		if _, err := readAssistantGraphArguments("propose_graph", json.RawMessage(args)); err == nil {
			t.Fatal("malformed arguments accepted", args)
		}
	}
	if _, err := readAssistantGraphArguments("graph_validate", json.RawMessage(`{"content":"`+strings.Repeat("x", maxFileBytes+1)+`"}`)); err == nil {
		t.Fatal("excessive content accepted")
	}
	valid := runcontext.HostTool{Name: "read_fixture", Effect: "read", InputSchema: json.RawMessage(`{"type":"object"}`), Execute: func(context.Context, json.RawMessage) (runcontext.ToolResult, error) {
		return runcontext.ToolResult{}, nil
	}}
	for _, tools := range [][]runcontext.HostTool{{valid, valid}, {{Name: "missing", Effect: "read", InputSchema: valid.InputSchema}}, {{Name: "wrong", Effect: "act", InputSchema: valid.InputSchema, Execute: valid.Execute}}} {
		if _, err := newAssistantHostRegistry(tools); err == nil {
			t.Fatal("invalid registry accepted")
		}
	}
	for _, mode := range []string{"no-call", "foreign-call", "foreign-owner", "foreign-project", "uncertain"} {
		t.Run(mode, func(t *testing.T) {
			s, token, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
			x := assistantGraphSetup(t, s, e)
			args := json.RawMessage(`{}`)
			id := "scope-call"
			ctx := context.WithValue(runcontext.WithCallID(t.Context(), id), assistantHostRootKey{}, true)
			if mode != "no-call" {
				name := "list_decisions"
				if mode == "foreign-call" {
					name = "read_decision"
				}
				assistantRunMust(t, e.writer.boundEvent(t.Context(), contract.RunEvent{ToolCall: &contract.ToolCall{Type: "tool_call", CallID: &id, Name: name, Args: args}}))
			}
			if mode == "foreign-owner" {
				saved := policyWorkspaceRead(t, s, token, "/api/conversations")
				var doc conversationDocument
				assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
				doc.Chats[0] = bytes.Replace(doc.Chats[0], []byte("Review exact input"), []byte("Changed exact input"), 1)
				w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, doc)
				if w.Code != 200 {
					t.Fatal(w.Body.String())
				}
			}
			if mode == "foreign-project" {
				s.projectDir = filepath.Join(t.TempDir(), "other-project")
			}
			if mode == "uncertain" {
				in, _ := json.Marshal(assistantHostIntent{CallID: id, Root: true, Arguments: args})
				op := assistantRunOperation{ID: "graph-host-" + digestOf(in)[:32], CallID: "graph-host-" + digestOf(in)[:32], Kind: "host", Name: "list_decisions", Intent: in}
				assistantRunMust(t, e.writer.Intent(t.Context(), op))
				assistantRunMust(t, e.writer.Dispatch(t.Context(), op.ID))
			}
			result, err := assistantGraphFind(t, x.Run.HostTools, "list_decisions").Execute(ctx, args)
			assistantRunMust(t, err)
			if !result.Err() {
				t.Fatal("unbound or uncertain invocation executed")
			}
		})
	}
}
func TestAssistantGraphPersistenceBeforeSuccess(t *testing.T) {
	var e *assistantRunExecution
	var faultMu sync.Mutex
	var storeName string
	reads := 0
	fault := errors.New("fixture producer persistence unavailable")
	faultWhileActive := false
	// Install before admission starts. The monitor and failure cleanup also read
	// private files, so arm only this ledger after the tool call is persisted.
	testHookPrivateRead = func(name string) error {
		faultMu.Lock()
		defer faultMu.Unlock()
		if name == storeName {
			reads++
			if reads == 6 {
				faultWhileActive = e.ctx.Err() == nil
				return fault
			}
		}
		return nil
	}
	// Registered first: the fixture's later cleanup cancels and joins the
	// service (including its monitor and final ledger read) before this reset.
	t.Cleanup(func() {
		if e != nil {
			select {
			case <-e.done:
			default:
				t.Error("execution still reads private storage; fault hook retained")
				return
			}
		}
		testHookPrivateRead = nil
	})
	s, _, execution := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
	e = execution
	x := assistantGraphSetup(t, s, e)
	name := "propose_graph"
	id := "new-graph"
	args, _ := json.Marshal(map[string]string{"id": "new-flow", "content": assistantGraphCandidate})
	assistantRunMust(t, e.writer.boundEvent(t.Context(), contract.RunEvent{ToolCall: &contract.ToolCall{Type: "tool_call", CallID: &id, Name: name, Args: args}}))
	faultMu.Lock()
	storeName = e.writer.lease.store.name
	faultMu.Unlock()
	ctx := context.WithValue(runcontext.WithCallID(t.Context(), id), assistantHostRootKey{}, true)
	result, err := assistantGraphFind(t, x.Run.HostTools, name).Execute(ctx, args)
	assistantRunMust(t, err)
	faultMu.Lock()
	observedReads, activeFailure := reads, faultWhileActive
	faultMu.Unlock()
	if !result.Err() || !errors.Is(e.writer.Err(), fault) || !activeFailure {
		t.Fatal("failed producer persistence exposed success or fault missed the active call", observedReads, result.Value().Text())
	}
}

func TestAssistantGraphQueuedGuidanceFreshness(t *testing.T) {
	for _, mode := range []string{"pack", "declaration"} {
		t.Run(mode, func(t *testing.T) {
			s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
			x := assistantGraphSetup(t, s, e)
			x.runtime.serial <- struct{}{}
			call := "guidance"
			args := json.RawMessage(`{"relationship":"one then next","packs":["one"]}`)
			assistantRunMust(t, e.writer.boundEvent(t.Context(), contract.RunEvent{ToolCall: &contract.ToolCall{Type: "tool_call", CallID: &call, Name: "get_graph_authoring_instructions", Args: args}}))
			before := len(assistantExecutionRuntimeLog(t, s))
			prepared := make(chan struct{}, 1)
			testHookAssistantGraphGuidance = func() { prepared <- struct{}{} }
			defer func() { testHookAssistantGraphGuidance = nil }()
			done := make(chan runcontext.ToolResult, 1)
			ctx := context.WithValue(runcontext.WithCallID(t.Context(), call), assistantHostRootKey{}, true)
			go func() {
				result, _ := assistantGraphFind(t, x.Run.HostTools, "get_graph_authoring_instructions").Execute(ctx, args)
				done <- result
			}()
			select {
			case <-prepared:
			case <-time.After(3 * time.Second):
				<-x.runtime.serial
				t.Fatal("guidance did not reach serialized Runtime wait")
			}

			if mode == "pack" {
				assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "one.json"), []byte(`{"changed":true}`), 0600))
			} else {
				assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "jpack.json"), []byte(strings.Replace(assistantGraphConfig, "A pack", "Changed pack", 1)), 0600))
			}
			<-x.runtime.serial
			select {
			case result := <-done:
				if !result.Err() {
					t.Fatal("stale guidance dispatched")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("guidance did not refuse")
			}
			if len(assistantExecutionRuntimeLog(t, s)) != before {
				t.Fatal("stale guidance physically reached Runtime")
			}
		})
	}
}
func TestAssistantGraphActualNativeChildIdentity(t *testing.T) {
	assistantGraphActualNativeIdentity(t, assistantGraphCandidate)
}

func assistantGraphActualNativeIdentity(t *testing.T, candidate string) (*assistantRunExecution, assistantRunRecord) {
	t.Helper()
	s, _, e, _, trace := assistantCodexFixture(t, "delegate", "work")
	peerPath := filepath.Join(filepath.Dir(trace), "peer.py")
	peer := assistantExecutionCodexPeer
	peer = strings.Replace(peer, "if mode=='delegate' and index==1:\n   tool(offered('delegate_tasks'),{'tasks':[{'title':'Inspect schema','objective':'Inspect the schema only.','profile':'review'}]})", `if mode=='delegate' and index==1:
   tool(offered('read_decision'),{'type':'graph','id':'flow'},call='same-provider-call')
  elif mode=='delegate' and index==2: tool(offered('read_decision'),{'type':'graph','id':'flow'},call='same-provider-call')
  elif mode=='delegate' and index==3: tool(offered('propose_graph'),{'id':'flow','content':`+fmt.Sprintf("%q", candidate)+`},call='same-provider-call')`, 1)
	peer = strings.Replace(peer, "if mode=='duplicate': tool(offered('get_schema'),{},rpc='rpc-two')", `if mode=='delegate' and index==1: tool(offered('delegate_tasks'),{'tasks':[{'title':'Read graph','objective':'Inspect the declared graph only.','profile':'review'}]},call='delegate',rpc='rpc-delegate')
  elif mode=='duplicate': tool(offered('get_schema'),{},rpc='rpc-two')`, 1)
	assistantRunMust(t, os.WriteFile(peerPath, []byte(peer), 0600))
	x := assistantGraphSetup(t, s, e)
	off := false
	x.Run.Review = &off
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	assistantRunMust(t, x.Adapter.Run(ctx, x.Run))
	record := assistantExecutionRecord(t, e)
	if len(record.DomainOutputs) != 1 {
		raw, _ := json.Marshal(record)
		t.Fatalf("native producer absent: %s writer=%v", raw, e.writer.Err())
	}
	callbacks := map[string]bool{}
	roots := map[bool]int{}
	reads := 0
	for _, op := range record.Operations {
		if op.Kind != "host" {
			continue
		}
		var in assistantHostIntent
		assistantRunMust(t, json.Unmarshal(op.Intent, &in))
		if in.CallbackID == "" || callbacks[in.CallbackID] {
			t.Fatal("native callback identity collapsed")
		}
		callbacks[in.CallbackID] = true
		roots[in.Root]++
		if op.Name == "read_decision" {
			var result assistantHostResult
			assistantRunMust(t, json.Unmarshal(op.Result, &result))
			if !in.Root && result.Read != nil {
				t.Fatal("child receipt gained root read custody")
			}
			reads++
		}
	}
	if reads != 2 || roots[true] != 2 || roots[false] != 1 {
		t.Fatalf("parent child parent did not retain distinct scopes: reads=%d roots=%v", reads, roots)
	}
	ids := map[string]bool{}
	for _, op := range record.Operations {
		if op.Kind != "host" {
			continue
		}
		var in assistantHostIntent
		_ = json.Unmarshal(op.Intent, &in)
		if ids[in.CallID] {
			t.Fatal("bridge reused canonical identity across provider invocations")
		}
		ids[in.CallID] = true
		if op.Name == "propose_graph" && record.DomainOutputs[0].CallID != in.CallID {
			t.Fatal("canonical call association lost")
		}
	}

	// Reconstructed imported output cannot turn a child receipt into a basis.
	raw, _ := json.Marshal(assistantRunLedger{Version: 1, Project: record.Binding.Project, Runs: []assistantRunRecord{record}})
	if _, err := decodeAssistantRunStorage(e.writer.lease.store.name, raw); err != nil {
		t.Fatal("native receipt reconstruction refused", err)
	}
	if !bytes.Equal([]byte(record.DomainOutputs[0].Draft.Content), []byte(candidate)) {
		t.Fatal("native candidate bytes changed")
	}
	return e, record
}

func TestAssistantGraphOutputImportAssociations(t *testing.T) {
	s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
	x := assistantGraphSetup(t, s, e)
	if assistantGraphInvoke(t, e, x, "read", "read_decision", map[string]string{"id": "flow", "type": "graph"}).Err() {
		t.Fatal("read failed")
	}
	if assistantGraphInvoke(t, e, x, "propose", "propose_graph", map[string]string{"id": "flow", "content": assistantGraphCandidate}).Err() {
		t.Fatal("producer failed")
	}
	original := assistantExecutionRecord(t, e)
	for _, mode := range []string{"run", "request", "call", "operation", "content", "base", "output-removed", "duplicate", "child-receipt", "missing-producer", "result-status"} {
		t.Run(mode, func(t *testing.T) {
			raw, _ := json.Marshal(original)
			var record assistantRunRecord
			assistantRunMust(t, json.Unmarshal(raw, &record))
			switch mode {
			case "run":
				record.DomainOutputs[0].RunID = "arun_00000000000000000000000000000000"
			case "request":
				record.DomainOutputs[0].RequestKey = "foreign-request"
			case "call":
				record.DomainOutputs[0].CallID = "foreign-call"
			case "operation":
				record.DomainOutputs[0].OperationID = "foreign-operation"
			case "content":
				record.DomainOutputs[0].Draft.Content = assistantGraphOriginal
			case "base":
				record.DomainOutputs[0].Draft.BaseSHA256 = digestOf([]byte("foreign"))
			case "output-removed":
				record.DomainOutputs = nil
			case "duplicate":
				record.DomainOutputs = append(record.DomainOutputs, record.DomainOutputs[0])
			case "child-receipt":
				for i := range record.Operations {
					op := &record.Operations[i]
					if op.Kind == "host" && op.Name == "read_decision" {
						var in assistantHostIntent
						assistantRunMust(t, json.Unmarshal(op.Intent, &in))
						in.Root = false
						op.Intent, _ = json.Marshal(in)
					}
				}
			case "result-status":
				for i := range record.Operations {
					op := &record.Operations[i]
					if op.Name == "propose_graph" && op.Kind == "host" {
						var result assistantHostResult
						assistantRunMust(t, json.Unmarshal(op.Result, &result))
						result.Result = bytes.Replace(result.Result, []byte(`\"status\":\"draft\"`), []byte(`\"status\":\"saved\"`), 1)
						op.Result, _ = json.Marshal(result)
					}
				}
			case "missing-producer":
				for i, op := range record.Operations {
					if op.Name == "propose_graph" && op.Kind == "host" {
						record.Operations = append(record.Operations[:i], record.Operations[i+1:]...)
						break
					}
				}
			}
			ledger := assistantRunLedger{Version: 1, Project: record.Binding.Project, Runs: []assistantRunRecord{record}}
			raw, _ = json.Marshal(ledger)
			if _, err := decodeAssistantRunStorage(e.writer.lease.store.name, raw); err == nil {
				t.Fatal("fabricated output accepted", mode)
			}
		})
	}
	// Empty-domain runs retain the exact pre-domain projection digest formula.
	older := original
	older.DomainOutputs = nil
	raw, _ := json.Marshal(struct {
		State      string
		Checkpoint any
		Events     []assistantRunEvent
	}{older.State, assistantChatProjectionCheckpoint(older), older.Events})
	if assistantChatEventDigest(older) != digestOf(raw) {
		t.Fatal("legacy prepared projection digest changed")
	}
}

func TestAssistantGraphRootMetadataBinding(t *testing.T) {
	s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
	x := assistantGraphSetup(t, s, e)
	request := *x.Run.Context.CurrentRequest
	for _, mode := range []string{"copied-root", "child", "request", "execution"} {
		t.Run(mode, func(t *testing.T) {
			run := *x.Run
			input := *x.Run.Context
			run.Context = &input
			switch mode {
			case "child":
				run.Context = &runcontext.Input{}
			case "request":
				copy := request
				copy.Input = "foreign"
				input.CurrentRequest = &copy
			case "execution":
				copy := *input.Execution
				copy.RequestKey = "foreign"
				input.Execution = &copy
			}
			observed := false
			wrapped := assistantHostAdapter(adapter.Func(func(ctx context.Context, _ *adapter.Run) error {
				observed, _ = ctx.Value(assistantHostRootKey{}).(bool)
				return nil
			}), e.record.Binding, request)
			assistantRunMust(t, wrapped.Run(t.Context(), &run))
			if observed != (mode == "copied-root") {
				t.Fatal("immutable adapter scope mismatch", mode, observed)
			}
		})
	}
}

func TestAssistantGraphNativeForeignCallback(t *testing.T) {
	s, _, e, _, _ := assistantCodexFixture(t, "callback-wait", "work")
	x := assistantGraphSetup(t, s, e)
	assistantRunMust(t, e.Authorize(t.Context()))
	if assistantExecutionRecord(t, e).Checkpoint.Status != "running" {
		t.Fatal("fixture must test live dispatch, not a terminal checkpoint refusal")
	}

	foreign := "foreign-native-call"
	args := json.RawMessage(`{}`)
	assistantRunMust(t, e.writer.boundEvent(t.Context(), contract.RunEvent{ToolCall: &contract.ToolCall{Type: "tool_call", CallID: &foreign, Name: "list_decisions", Args: args}}))
	callctx := context.WithValue(runcontext.WithCallID(t.Context(), foreign), assistantHostRootKey{}, true)
	callctx = context.WithValue(callctx, assistantHostCallbackKey{}, "foreign-callback")
	result, err := assistantGraphFind(t, x.Run.HostTools, "list_decisions").Execute(callctx, args)
	assistantRunMust(t, err)
	if !result.Err() {
		t.Fatal("foreign native callback acquired host scope")
	}

}

func TestAssistantGraphReceiptMembers(t *testing.T) {
	intent := assistantHostIntent{CallID: "call", Root: true, Arguments: []byte(`{}`)}
	raw, _ := json.Marshal(intent)
	var parsed assistantHostIntent
	assistantRunMust(t, readAssistantHostReceipt(raw, &parsed))
	for _, changed := range [][]byte{
		bytes.Replace(raw, []byte(`"arguments"`), []byte(`"Arguments"`), 1),
		bytes.Replace(raw, []byte(`"root":true`), []byte(`"root":true,"authority":"restored"`), 1),
		bytes.Replace(raw, []byte(`"root":true`), []byte(`"root":null`), 1),
		bytes.Replace(raw, []byte(`"root":true,`), nil, 1),
	} {
		if readAssistantHostReceipt(changed, &parsed) == nil {
			t.Fatal("noncanonical host custody members accepted", string(changed))
		}
	}
	receipt := assistantHostResult{Result: []byte(`{"content":[]}`), Read: &assistantGraphRead{ID: "flow", Path: "flow.graph.json", SHA256: digestOf([]byte("graph")), Declaration: digestOf([]byte("declaration"))}}
	raw, _ = json.Marshal(receipt)
	var result assistantHostResult
	assistantRunMust(t, readAssistantHostReceipt(raw, &result))
	for _, changed := range [][]byte{bytes.Replace(raw, []byte(`"result"`), []byte(`"Result"`), 1), bytes.Replace(raw, []byte(`"id":"flow"`), []byte(`"ID":"flow"`), 1)} {
		if readAssistantHostReceipt(changed, &result) == nil {
			t.Fatal("aliased host result/custody accepted", string(changed))
		}
	}
}
