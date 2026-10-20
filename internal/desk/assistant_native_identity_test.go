package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

// Provider IDs repeat across invocations; the actual bridge still must hand
// each callback a distinct canonical ID, including exact escaped JSON data.
func TestAssistantGraphActualNativeArgumentCustody(t *testing.T) {
	candidate := strings.Replace(assistantGraphCandidate, `"literal":"50"`, `"literal":"中文 <record> & \u2028 \u2029 >"`, 1)
	e, original := assistantGraphActualNativeIdentity(t, candidate)
	var producer assistantRunOperation
	var in assistantHostIntent
	for _, op := range original.Operations {
		if op.Kind == "host" && op.Name == "propose_graph" {
			producer = op
			assistantRunMust(t, readAssistantHostReceipt(op.Intent, &in))
		}
	}
	if producer.ID == "" || !bytes.Contains(in.Arguments, []byte("<record>")) {
		t.Fatal("producer did not retain the original argument bytes")
	}
	ctx := context.WithValue(runcontext.WithCallID(t.Context(), in.CallID), assistantHostRootKey{}, true)
	ctx = context.WithValue(ctx, assistantHostCallbackKey{}, in.CallbackID)
	ran := false
	replay, err := e.invokeHost(ctx, producer.Name, in.Arguments, func(context.Context, bool) (runcontext.ToolResult, *assistantGraphRead, *graphDraft, error) {
		ran = true
		return runcontext.ToolResult{}, nil, nil, nil
	})
	assistantRunMust(t, err)
	if ran || replay.Err() || len(assistantExecutionRecord(t, e).Operations) != len(original.Operations) {
		t.Fatal("exact completed delivery reran its producer")
	}
	for _, mode := range []string{"callback", "invocation", "invocation-earlier", "invocation-later", "call", "arguments", "tool", "child-root", "event-arguments", "event-duplicate"} {
		t.Run(mode, func(t *testing.T) {
			raw, _ := json.Marshal(original)
			var record assistantRunRecord
			assistantRunMust(t, json.Unmarshal(raw, &record))
			for i := range record.Operations {
				op := &record.Operations[i]
				if mode == "invocation" && op.ID == in.CallbackID {
					var body map[string]json.RawMessage
					assistantRunMust(t, json.Unmarshal(op.Intent, &body))
					body["invocation"] = json.RawMessage(`"foreign-invocation"`)
					op.Intent, _ = json.Marshal(body)
				}
				if op.ID != producer.ID && mode != "child-root" {
					continue
				}
				if op.Kind != "host" {
					continue
				}
				var intent assistantHostIntent
				assistantRunMust(t, readAssistantHostReceipt(op.Intent, &intent))
				switch mode {
				case "callback":
					intent.CallbackID = "foreign-callback"
				case "call":
					intent.CallID = "foreign-call"
				case "arguments":
					intent.Arguments = bytes.Replace(intent.Arguments, []byte("<record>"), []byte("<different>"), 1)
				case "tool":
					op.Name = "read_decision"
				case "child-root":
					if intent.Root {
						continue
					}
					intent.Root = true
				}
				op.Intent, _ = json.Marshal(intent)
			}
			if mode == "invocation-earlier" || mode == "invocation-later" {
				callback, invocation := in.CallbackID, ""
				for _, op := range record.Operations {
					if op.Kind == "model" {
						invocation = op.ID
						if mode == "invocation-earlier" {
							break
						}
					}
				}
				if mode == "invocation-later" {
					for _, op := range record.Operations {
						if op.Kind != "host" || op.Name != "read_decision" {
							continue
						}
						var read assistantHostIntent
						assistantRunMust(t, readAssistantHostReceipt(op.Intent, &read))
						if read.Root {
							callback = read.CallbackID
							break
						}
					}
				}
				for i := range record.Operations {
					op := &record.Operations[i]
					if op.ID != callback {
						continue
					}
					var body map[string]json.RawMessage
					assistantRunMust(t, json.Unmarshal(op.Intent, &body))
					body["invocation"], _ = json.Marshal(invocation)
					op.Intent, _ = json.Marshal(body)
				}
			}
			if mode == "event-arguments" || mode == "event-duplicate" {
				for _, held := range record.Events {
					c := held.Event.ToolCall
					if c == nil || c.CallID == nil || *c.CallID != in.CallID {
						continue
					}
					if mode == "event-duplicate" {
						held.ID = "duplicated-root-event"
						record.Events = append(record.Events, held)
					} else {
						c.Args = bytes.Replace(c.Args, []byte(`\u003crecord\u003e`), []byte(`\u003cdifferent\u003e`), 1)
					}
					break
				}
			}
			raw, _ = json.Marshal(assistantRunLedger{Version: 1, Project: record.Binding.Project, Runs: []assistantRunRecord{record}})
			if _, err := decodeAssistantRunStorage(e.writer.lease.store.name, raw); err == nil {
				t.Fatal("mismatched native custody survived import")
			}
		})
	}
	assistantRunMust(t, e.writer.Stop(t.Context()))
	stopped := assistantExecutionRecord(t, e)
	if len(stopped.DomainOutputs) != 1 || stopped.DomainOutputs[0].Draft.Content != candidate {
		t.Fatal("Stop lost the completed output")
	}
	recovered := assistantRunLedger{Version: 1, Project: stopped.Binding.Project, Runs: []assistantRunRecord{stopped}}
	interruptAssistantRuns(&recovered, true)
	raw, _ := json.Marshal(recovered)
	if _, err := decodeAssistantRunStorage(e.writer.lease.store.name, raw); err != nil {
		t.Fatal("historical completed output refused", err)
	}
}

func TestAssistantHostArgumentWireEncoding(t *testing.T) {
	original := []byte("{\n  \"text\":\"<>&\u2028\u2029中文\", \"facts\":{\"number\":1.20,\"literal\":\"50\",\"missing\":null,\"flag\":false,\"zero\":0,\"Key\":1,\"key\":2}\n}")
	retained := []byte(`{"text":"\u003c\u003e\u0026\u2028\u2029中文","facts":{"number":1.20,"literal":"50","missing":null,"flag":false,"zero":0,"Key":1,"key":2}}`)
	if !assistantHostArgumentsMatch(retained, original) {
		t.Fatal("journal encoding changed argument custody")
	}
	for _, changed := range [][]byte{
		bytes.Replace(retained, []byte("1.20"), []byte("1.2"), 1),
		bytes.Replace(retained, []byte(`"50"`), []byte(`50`), 1),
		bytes.Replace(retained, []byte(`"missing":null,`), nil, 1),
		bytes.Replace(retained, []byte(`"flag":false`), []byte(`"flag":0`), 1),
		bytes.Replace(retained, []byte(`"zero":0`), []byte(`"zero":"0"`), 1),
		bytes.Replace(retained, []byte(`"Key":1,"key":2`), []byte(`"key":2,"Key":1`), 1),
		bytes.Replace(retained, []byte(`"key":2`), []byte(`"key":2,"key":2`), 1),
		[]byte(`{`),
	} {
		if assistantHostArgumentsMatch(changed, original) {
			t.Fatal("changed facts matched", string(changed))
		}
	}
	if assistantHostArgumentsMatch([]byte(`{`), []byte(`{`)) {
		t.Fatal("invalid JSON matched itself")
	}
}

func TestAssistantParentHostArgumentWireEncoding(t *testing.T) {
	id := "canonical-call"
	in := assistantHostIntent{CallID: id, Root: true, Arguments: []byte(" { \"text\": \"<>&\", \"number\": 1.20 } ")}
	run := assistantRunRecord{Events: []assistantRunEvent{{ID: "event", Event: contract.RunEvent{ToolCall: &contract.ToolCall{Type: "tool_call", CallID: &id, Name: "graph_validate", Args: json.RawMessage(`{"text":"\u003c\u003e\u0026","number":1.20}`)}}}}}
	if !assistantParentHostCall(run, "graph_validate", in) {
		t.Fatal("canonical API event failed the journal encoding boundary")
	}
	in.Arguments = bytes.Replace(in.Arguments, []byte("1.20"), []byte("1.2"), 1)
	if assistantParentHostCall(run, "graph_validate", in) {
		t.Fatal("changed exact numeric spelling borrowed event custody")
	}
}

func TestAssistantNativeHostArgumentWireEncoding(t *testing.T) {
	in := assistantHostIntent{CallID: "native-call", CallbackID: "callback", Root: true, Arguments: []byte(" { \"text\": \"<>&\", \"number\": 1.20 } ")}
	run := assistantRunRecord{Operations: []assistantRunOperation{
		{ID: "invocation", Kind: "model", Intent: []byte(`{"model":"shared-model","prompt":"request","instructions":"rules","phase":"author","tools":[{"name":"offered-host","description":"graph_validate: Validate a graph","inputSchema":{"type":"object"}}]}`)},
		{ID: "callback", Kind: "callback", Name: "offered-host", Intent: []byte(`{"invocation":"invocation","call":{"id":"native-call","name":"offered-host","arguments":{"text":"\u003c\u003e\u0026","number":1.20}}}`)},
	}}
	if !assistantNativeHostCall(run, "graph_validate", in) {
		t.Fatal("exact native argument journal encoding refused")
	}
	in.Arguments = bytes.Replace(in.Arguments, []byte("1.20"), []byte("1.2"), 1)
	if assistantNativeHostCall(run, "graph_validate", in) {
		t.Fatal("changed exact native number spelling borrowed callback custody")
	}
}

// Native author, child and critic invocations all use the same journal order.
// A later invocation (including a model operation from another wire) cannot
// change custody of an earlier callback, or lend its offer to a later one.
func TestAssistantNativeInvocationAssociation(t *testing.T) {
	model := assistantRunOperation{ID: "author", Kind: "model", Intent: []byte(`{"model":"shared-model","phase":"author","tools":[{"name":"offered-host","description":"graph_validate: Validate","inputSchema":{"type":"object"}}]}`)}
	first := assistantRunOperation{ID: "first", Kind: "callback", Name: "offered-host", Intent: []byte(`{"invocation":"author","call":{"id":"first-call","name":"offered-host","arguments":{}}}`)}
	critic := assistantRunOperation{ID: "critic", Kind: "model", Intent: []byte(`{"model":"critic-model","phase":"critic","tools":[{"name":"offered-host","description":"graph_validate: Validate","inputSchema":{"type":"object"}}]}`)}
	second := assistantRunOperation{ID: "second", Kind: "callback", Name: "offered-host", Intent: []byte(`{"invocation":"critic","call":{"id":"second-call","name":"offered-host","arguments":{}}}`)}
	api := assistantRunOperation{ID: "later-api", Kind: "model", Intent: []byte(`{"suffix":"chat/completions","request":{"body":"e30="}}`)}
	original := assistantRunRecord{Operations: []assistantRunOperation{model, first, critic, second, api}}
	for _, which := range []struct{ callback, call string }{{"first", "first-call"}, {"second", "second-call"}} {
		in := assistantHostIntent{CallbackID: which.callback, CallID: which.call, Arguments: []byte(`{}`)}
		if !assistantNativeHostCall(original, "graph_validate", in) {
			t.Fatal("later model changed earlier callback custody", which.callback)
		}
	}
	for _, which := range []struct{ callback, call, invocation string }{{"first", "first-call", "critic"}, {"second", "second-call", "author"}, {"first", "first-call", "later-api"}, {"second", "second-call", ""}} {
		raw, _ := json.Marshal(original)
		var changed assistantRunRecord
		assistantRunMust(t, json.Unmarshal(raw, &changed))
		for i := range changed.Operations {
			op := &changed.Operations[i]
			if op.ID != which.callback {
				continue
			}
			var body map[string]json.RawMessage
			assistantRunMust(t, json.Unmarshal(op.Intent, &body))
			body["invocation"], _ = json.Marshal(which.invocation)
			op.Intent, _ = json.Marshal(body)
		}
		if assistantNativeHostCall(changed, "graph_validate", assistantHostIntent{CallbackID: which.callback, CallID: which.call, Arguments: []byte(`{}`)}) {
			t.Fatal("cross-invocation callback acquired offer", which)
		}
	}
}
