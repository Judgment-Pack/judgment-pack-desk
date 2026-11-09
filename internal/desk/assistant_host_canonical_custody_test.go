package desk

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

// Handwritten boundary vectors. These are not execution evidence or generated
// snapshots; actual producer/evidence tests exercise the real RunStore.
func assistantCanonicalCustodyVector() (assistantRunRecord, assistantHostIntent, assistantRunOperation) {
	request := `{"z":{"big":9007199254740993,"decimal":1.20,"flag":false,"literal":"50","missing":null},"a":"<>&"}`
	event := json.RawMessage(`{"a":"<>&","z":{"big":9007199254740992,"decimal":1.2,"flag":false,"literal":"50","missing":null}}`)
	fingerprint := `{"kind":"host","request":{"a":"<>&","z":{"big":9007199254740992,"decimal":1.2,"flag":false,"literal":"50","missing":null}},"scope":{"execution":"execution-one","role":"author"},"source":"host","tool":"graph_validate"}`
	call := "call-one"
	in := assistantHostIntent{CallID: call, Root: true, Arguments: []byte(request), DurableDispatchID: "dispatch-one"}
	d := contract.CheckpointDurableDispatch{ID: in.DurableDispatchID, Scope: contract.CheckpointDurableScope{Execution: "execution-one", Role: "author"}, Kind: "host", Source: "host", Tool: "graph_validate", Read: true, Request: request, RequestDigest: "sha256:" + digestOf([]byte(request)), Fingerprint: "sha256:" + digestOf([]byte(fingerprint)), State: "claimed"}
	run := assistantRunRecord{CanonicalRequired: true, Canonical: &assistantCanonicalJournal{Active: true}, Checkpoint: &contract.Checkpoint{ID: "execution-one", Durable: &contract.CheckpointDurableBook{Dispatches: []contract.CheckpointDurableDispatch{d}}}, Events: []assistantRunEvent{{ID: "event-one", Event: contract.RunEvent{ToolCall: &contract.ToolCall{Type: "tool_call", CallID: &call, Name: "graph_validate", Args: event}}}}}
	result := []byte(`{"structuredContent":{"number":1.20,"big":9007199254740993,"missing":null,"flag":false,"literal":"50"},"isError":false,"content":[{"text":"Exact 1.20 and 9007199254740993","type":"text"}]}`)
	op := assistantRunOperation{ID: "physical-one", Name: "graph_validate", Kind: "host", State: "completed", Intent: assistantChatJSON(in), Result: assistantChatJSON(assistantHostResult{Result: result})}
	return run, in, op
}

func TestAssistantCanonicalHostCustodyVectors(t *testing.T) {
	run, in, op := assistantCanonicalCustodyVector()
	if id, err := assistantCanonicalHostClaim(run, op.Name, in); err != nil || id != in.DurableDispatchID {
		t.Fatal("exact live claim refused", id, err)
	}
	if !assistantParentHostCall(run, op.Name, in) {
		t.Fatal("canonical event representation lost exact request custody")
	}
	if assistantHostArgumentsMatch(run.Events[0].Event.ToolCall.Args, in.Arguments) {
		t.Fatal("fixture failed to reorder/normalize event")
	}
	for _, mode := range []string{"missing-id", "wrong-id", "duplicate-id", "ambiguous-claim", "request-order", "request-number", "request-large-number", "request-string", "request-duplicate", "intent-duplicate", "digest", "fingerprint", "scope-execution", "scope-role", "scope-task", "source", "tool", "read", "event-call", "event-name", "event-value", "event-duplicate", "duplicate-call"} {
		t.Run(mode, func(t *testing.T) {
			r, in, op := assistantCanonicalCustodyVector()
			d := &r.Checkpoint.Durable.Dispatches[0]
			switch mode {
			case "missing-id":
				in.DurableDispatchID = ""
			case "wrong-id":
				in.DurableDispatchID = "different-dispatch"
			case "duplicate-id":
				r.Checkpoint.Durable.Dispatches = append(r.Checkpoint.Durable.Dispatches, *d)
			case "ambiguous-claim":
				copy := *d
				copy.ID = "another-real-claim"
				r.Checkpoint.Durable.Dispatches = append(r.Checkpoint.Durable.Dispatches, copy)
			case "request-order":
				in.Arguments = []byte(`{"a":"<>&","z":{"big":9007199254740993,"decimal":1.20,"flag":false,"literal":"50","missing":null}}`)
			case "request-number":
				in.Arguments = bytes.Replace(in.Arguments, []byte("1.20"), []byte("1.2"), 1)
			case "request-large-number":
				in.Arguments = bytes.Replace(in.Arguments, []byte("9007199254740993"), []byte("9007199254740992"), 1)
			case "request-string":
				in.Arguments = bytes.Replace(in.Arguments, []byte(`"50"`), []byte(`50`), 1)
			case "request-duplicate":
				d.Request = strings.Replace(d.Request, `"a":"<>&"`, `"a":"<>&","a":"<>&"`, 1)
			case "intent-duplicate":
				in.Arguments = bytes.Replace(in.Arguments, []byte(`"a":"<>&"`), []byte(`"a":"<>&","a":"<>&"`), 1)
			case "digest":
				d.RequestDigest = "sha256:" + strings.Repeat("f", 64)
			case "fingerprint":
				d.Fingerprint = "sha256:" + strings.Repeat("f", 64)
			case "scope-execution":
				d.Scope.Execution = "other-execution"
			case "scope-role":
				d.Scope.Role = "critic"
			case "scope-task":
				task := "child"
				d.Scope.Task = &task
			case "source":
				d.Source = "other-source"
			case "tool":
				d.Tool = "graph_explain"
			case "read":
				d.Read = false
			case "event-call":
				call := "other-call"
				r.Events[0].Event.ToolCall.CallID = &call
			case "event-name":
				r.Events[0].Event.ToolCall.Name = "graph_explain"
			case "event-value":
				r.Events[0].Event.ToolCall.Args = bytes.Replace(r.Events[0].Event.ToolCall.Args, []byte(`false`), []byte(`true`), 1)
			case "event-duplicate":
				r.Events[0].Event.ToolCall.Args = bytes.Replace(r.Events[0].Event.ToolCall.Args, []byte(`"a":"<>&"`), []byte(`"a":"<>&","a":"<>&"`), 1)
			case "duplicate-call":
				r.Events = append(r.Events, r.Events[0])
			}
			if mode == "ambiguous-claim" {
				if _, err := assistantCanonicalHostClaim(r, op.Name, in); err == nil {
					t.Fatal("ambiguous live claim accepted")
				}
				return
			}
			if assistantParentHostCall(r, op.Name, in) {
				t.Fatal("altered root custody accepted", mode)
			}
		})
	}
	for _, mode := range []string{"claimed", "unknown", "result", "refused-result", "result-number", "result-large-number", "result-string", "result-null", "result-duplicate"} {
		t.Run(mode, func(t *testing.T) {
			r, in, op := assistantCanonicalCustodyVector()
			d := &r.Checkpoint.Durable.Dispatches[0]
			d.State = mode
			if mode == "refused-result" {
				d.State = "result"
				d.Result = json.RawMessage(`{"isError":true,"content":[{"type":"text","text":"Retained refusal"}]}`)
				op.State = "refused"
				op.Result = assistantChatJSON(assistantHostResult{Result: []byte(`{"content":[{"text":"Retained refusal","type":"text"}],"isError":true}`)})
			}
			if strings.HasPrefix(mode, "result") {
				d.State = "result"
				d.Result = json.RawMessage(`{"content":[{"text":"Exact 1.20 and 9007199254740993","type":"text"}],"isError":false,"structuredContent":{"big":9007199254740993,"flag":false,"literal":"50","missing":null,"number":1.20}}`)
				switch mode {
				case "result-number":
					d.Result = bytes.Replace(d.Result, []byte(`"number":1.20`), []byte(`"number":1.2`), 1)
				case "result-large-number":
					d.Result = bytes.Replace(d.Result, []byte(`"big":9007199254740993`), []byte(`"big":9007199254740992`), 1)
				case "result-string":
					d.Result = bytes.Replace(d.Result, []byte(`"literal":"50"`), []byte(`"literal":50`), 1)
				case "result-null":
					d.Result = bytes.Replace(d.Result, []byte(`"missing":null,`), nil, 1)
				case "result-duplicate":
					d.Result = bytes.Replace(d.Result, []byte(`"isError":false`), []byte(`"isError":false,"isError":false`), 1)
				}
			}
			want := mode == "claimed" || mode == "unknown" || mode == "result" || mode == "refused-result"
			if assistantCanonicalHostResultMatches(r, op, in) != want {
				t.Fatal("physical/canonical result distinction changed", mode)
			}
			if assistantCanonicalHostSucceeded(r, op, in) {
				t.Fatal("unobserved or uncertain result became successful custody")
			}
		})
	}
	for _, raw := range []string{`{"callId":"call","root":true,"arguments":"e30=","durableDispatchId":"one","durableDispatchId":"two"}`, `{"callId":"call","root":true,"arguments":"e30=","DurableDispatchId":"one"}`, `{"callId":"call","root":true,"arguments":"e30=","durableDispatchId":7}`} {
		var in assistantHostIntent
		if readAssistantHostReceipt([]byte(raw), &in) == nil {
			t.Fatal("malformed private dispatch member accepted", raw)
		}
	}
	// ResultOf's structured content has Agent Value semantics before the Desk
	// receipt exists. Supported normalization therefore happens at construction,
	// while textual JSON stays exact. The comparator never repeats float parsing.
	structured := runcontext.MustParseJSON(`{"nested":{"decimal":1.20,"negativeZero":-0,"big":9007199254740993,"literal":"9007199254740993"}}`)
	result := runcontext.ResultOf([]runcontext.Content{runcontext.TextContent(`{"decimal":1.20,"negativeZero":-0,"big":9007199254740993}`)}, structured)
	physical := result.Value().JSON()
	if !bytes.Contains(physical, []byte(`"big":9007199254740992`)) || !bytes.Contains(physical, []byte(`"decimal":1.2`)) || !bytes.Contains(physical, []byte(`"negativeZero":0`)) {
		t.Fatal("ToolResult construction normalization changed", string(physical))
	}
	canonical := []byte(runcontext.CanonicalJSON(result.Value()))
	if !assistantCanonicalHostResultWireEqual(physical, canonical) {
		t.Fatal("supported ToolResult normalization refused")
	}
	for _, altered := range [][]byte{bytes.Replace(canonical, []byte(`"big":9007199254740992`), []byte(`"big":9007199254740993`), 1), bytes.Replace(canonical, []byte(`"decimal":1.2`), []byte(`"decimal":1.20`), 1), bytes.Replace(canonical, []byte(`"negativeZero":0`), []byte(`"negativeZero":-0`), 1), bytes.Replace(canonical, []byte(`1.20,`), []byte(`1.2,`), 1)} {
		if bytes.Equal(altered, canonical) {
			continue
		}
		if assistantCanonicalHostResultWireEqual(physical, altered) {
			t.Fatal("numeric result distinction lost", string(altered))
		}
	}
}

// Handwritten source-effect vectors supplement the actual research producer
// gate. Retained states remain readable without turning them into live claims.
func TestAssistantCanonicalSourceEffectCustody(t *testing.T) {
	for _, name := range []string{assistantSearchTool, assistantLinkTool, assistantWebsiteTool, assistantResearchTool} {
		t.Run(name, func(t *testing.T) {
			for _, state := range []string{"claimed", "result", "unknown"} {
				t.Run(state, func(t *testing.T) {
					run, in, op := assistantCanonicalCustodyVector()
					binding := assistantExternalBindingVector(name)
					in.SourceBinding, op.Name = &binding, name
					d := &run.Checkpoint.Durable.Dispatches[0]
					d.Source, d.Tool, d.State = binding.SourceDigest, binding.WireName, state
					// The three retrieval owners are reads. The reviewed workflow
					// is an act, even though its answer is framed as source data.
					d.Read = name != assistantResearchTool
					request := runcontext.MustParseJSON(d.Request)
					scope := runcontext.MustParseJSON(`{"execution":"execution-one","role":"author"}`)
					d.Fingerprint = runcontext.RecordDigest(runcontext.ObjectOf("kind", runcontext.String("host"), "scope", scope, "source", runcontext.String(d.Source), "tool", runcontext.String(d.Tool), "request", request))
					call := run.Events[0].Event.ToolCall
					call.Name = binding.WireName
					var source contract.ToolOfferSourceRecord
					assistantRunMust(t, json.Unmarshal(binding.SourceRecord, &source))
					call.Source = &source
					op.Intent = assistantChatJSON(in)
					before := assistantChatJSON(run)
					if assistantCanonicalRetainedHost(run, name, in) == nil || !assistantParentHostCall(run, name, in) {
						t.Fatal("exact sourced effect custody refused")
					}
					id, err := assistantCanonicalHostClaim(run, name, in)
					if state == "claimed" && (err != nil || id != in.DurableDispatchID) || state != "claimed" && err == nil {
						t.Fatal("retained state changed live claim admission", id, err)
					}
					if !bytes.Equal(before, assistantChatJSON(run)) {
						t.Fatal("custody read rewrote historical records")
					}
					for _, mutation := range []string{"read-bit", "child", "role", "task"} {
						t.Run(mutation, func(t *testing.T) {
							r, intent := run, in
							checkpoint, durable, dispatch := *run.Checkpoint, *run.Checkpoint.Durable, *d
							checkpoint.Durable = &durable
							durable.Dispatches = []contract.CheckpointDurableDispatch{dispatch}
							r.Checkpoint = &checkpoint
							switch mutation {
							case "read-bit":
								durable.Dispatches[0].Read = !dispatch.Read
							case "child":
								intent.Root = false
							case "role":
								durable.Dispatches[0].Scope.Role = "critic"
							case "task":
								task := "child"
								durable.Dispatches[0].Scope.Task = &task
							}
							if assistantCanonicalRetainedHost(r, name, intent) != nil || assistantParentHostCall(r, name, intent) {
								t.Fatal("altered source effect/root custody accepted", mutation)
							}
						})
					}
				})
			}
		})
	}
}
