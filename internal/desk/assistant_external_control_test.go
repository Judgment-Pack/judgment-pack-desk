package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
)

func assistantExternalControlVector(t *testing.T) (assistantRunRecord, assistantHostIntent, assistantRunOperation, assistantHostResult) {
	t.Helper()
	binding := assistantExternalBindingVector(assistantSearchTool)
	in := assistantHostIntent{CallID: "source-call", Arguments: []byte(`{"query":"public policy"}`), SourceBinding: &binding}
	op := assistantRunOperation{ID: "host-one", CallID: "host-one", Kind: "host", Name: assistantSearchTool, State: "refused", Intent: assistantChatJSON(in)}
	run := assistantRunRecord{Binding: assistantRunBinding{RequestID: "request-one"}}
	control, err := assistantExternalControlFor(run, op, in, &assistantExternalNotStarted{kind: "research-choice", reason: "choice-required", query: "public policy"})
	assistantRunMust(t, err)
	cause := assistantExternalControlFailure(*control)
	result := assistantHostResult{Result: assistantHostFailure(cause).Value().JSON(), ExternalControl: control, ExternalFailure: assistantExternalFailureBytes(cause)}
	op.Result = assistantChatJSON(result)
	run.Operations = []assistantRunOperation{op}
	return run, in, op, result
}

func TestAssistantExternalControlClosedPhysicalCustody(t *testing.T) {
	run, in, op, result := assistantExternalControlVector(t)
	assistantRunMust(t, validateAssistantExternalResult(run, op, in, result))
	if rows := assistantExternalCalls(run); len(rows) != 1 || rows[0].Control == nil || rows[0].OwnTool != assistantSearchTool || rows[0].Outcome != "not-started" {
		t.Fatal("lost authenticated finite display", rows)
	}
	if err, found := assistantExternalReplay(result); !found || err == nil {
		t.Fatal("retained refusal became a successful replay")
	}
	for _, mode := range []string{"control-host", "control-binding", "control-request", "control-kind", "control-reason", "control-query", "control-provider", "source-missing", "state-completed", "failure-missing", "failure-unknown", "result-success", "result-prose", "acquire-dispatched", "acquire-completed", "seal-dispatched", "storage-intent", "storage-receipt", "closed-dispatch"} {
		t.Run(mode, func(t *testing.T) {
			r, intent, physical, receipt := assistantExternalControlVector(t)
			switch mode {
			case "control-host":
				receipt.ExternalControl.HostOperationID = "foreign"
			case "control-binding":
				receipt.ExternalControl.SourceBindingDigest = strings.Repeat("c", 64)
			case "control-request":
				receipt.ExternalControl.RequestID = "foreign"
			case "control-kind":
				receipt.ExternalControl.Kind = "unknown"
			case "control-reason":
				receipt.ExternalControl.Reason = "arbitrary prose https://secret.test"
			case "control-query":
				receipt.ExternalControl.Query = "foreign"
			case "control-provider":
				receipt.ExternalControl.Provider = "https://secret.test"
			case "source-missing":
				intent.SourceBinding = nil
			case "state-completed":
				physical.State = "completed"
			case "failure-missing":
				receipt.ExternalFailure = nil
			case "failure-unknown":
				receipt.ExternalFailure = assistantExternalFailureBytes(failure.Err("permission", "desk", "unknown"))
			case "result-success":
				receipt.Result = runcontext.ResultOf([]runcontext.Content{runcontext.TextContent("success")}, nil).Value().JSON()
			case "result-prose":
				receipt.Result = assistantHostFailure(failure.Err("configuration", "desk", "not-started")).Value().JSON()
			default:
				c := assistantSearchCustody{HostOperationID: physical.ID, Acquire: "reserved", Seal: "not-started"}
				switch mode {
				case "acquire-dispatched":
					c.Acquire = "dispatched"
				case "acquire-completed":
					c.Acquire = "completed"
				case "seal-dispatched":
					c.Seal = "dispatched"
				case "storage-intent":
					c.Stage.Intent = &attachmentReceipt{ID: "packet", SHA256: strings.Repeat("a", 64)}
				case "storage-receipt":
					c.Final.Receipt = &attachmentReceipt{ID: "packet", SHA256: strings.Repeat("a", 64)}
				case "closed-dispatch":
					c.DispatchClosed = true
				}
				r.Searches = []assistantSearchCustody{c}
			}
			if validateAssistantExternalResult(r, physical, intent, receipt) == nil {
				t.Fatal("forged control accepted", mode)
			}
		})
	}
	raw := assistantChatJSON(result)
	for _, bad := range [][]byte{
		bytes.Replace(raw, []byte(`"version":1`), []byte(`"Version":1`), 1),
		bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":1,"extra":true`), 1),
		bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1),
		bytes.Replace(raw, assistantChatJSON(result.ExternalControl), []byte("null"), 1),
	} {
		var decoded assistantHostResult
		if readAssistantHostReceipt(bad, &decoded) == nil {
			t.Fatal("open or aliased physical control decoded")
		}
	}
}

// Execute the actual producer and invokeObservedHost against fixture-owned
// dependencies through real E1 approval, original canonical Writer, source
// policy and SourceGate. This remains direct-host evidence; E6 owns API/native
// source enrollment and upstream final-scope/child qualification.
func TestAssistantExternalControlActualSearchEarlyExits(t *testing.T) {
	for _, v := range []struct {
		name, choice, kind string
		setup              bool
	}{
		{"ask", "ask", "research-choice", false},
		{"provided", "provided", "provided-only", false},
		{"setup", "research", "setup-required", true},
	} {
		t.Run(v.name, func(t *testing.T) {
			f := newAssistantSearchFixture(t, "api", v.choice, "work", "http://127.0.0.1:1")
			invoke, replay := assistantExternalEvidenceControlScope(t, f, func(ctx context.Context, _ bool) (assistantHostObservation, error) {
				// Source/list approval is current at physical admission. This
				// authored fixture loses setup only on actual producer entry.
				if v.setup {
					assistantRunMust(t, os.WriteFile(f.statusPath, assistantChatJSON(map[string]any{"version": 1, "connections": []any{}}), 0600))
				}
				return f.e.executeAssistantSearch(ctx, "public policy")
			})
			result, err := invoke("source-control", json.RawMessage(`{"query":"public policy"}`))
			var typed *failure.Error
			if !errors.As(err, &typed) || typed.Failure.Outcome != "not-started" || result.Receipt != "" {
				t.Fatal("control swallowed its real normalized failure", err, result.Receipt)
			}
			record := assistantExecutionRecord(t, f.e)
			if f.acquisitions.Load() != 0 || f.seals.Load() != 0 || f.calls.Load() != 0 || len(record.Searches) != 0 || len(record.DomainOutputs) != 0 {
				t.Fatal("early exit performed source work")
			}
			var receipt assistantHostResult
			found := false
			for _, op := range record.Operations {
				if op.Kind != "host" || op.Name != assistantSearchTool {
					continue
				}
				assistantRunMust(t, readAssistantHostReceipt(op.Result, &receipt))
				if receipt.ExternalControl == nil || receipt.ExternalControl.Kind != v.kind || op.State != "refused" {
					t.Fatal("physical control missing", op)
				}
				found = true
			}
			if !found {
				t.Fatal("actual physical control was not retained")
			}
			for _, event := range record.Events {
				if event.Event.ToolResult != nil {
					t.Fatal("direct failure forged a canonical result")
				}
			}
			// Same operation reread remains a failure and does not re-enter acquisition.
			if v.setup {
				// Restore the exact observed owner, never create another
				// approval, before observing the retained physical refusal.
				assistantRunMust(t, os.WriteFile(f.statusPath, assistantChatJSON(map[string]any{"version": 1, "connections": []any{map[string]any{"id": "demo", "revision": strings.Repeat("a", 64), "provider": "tavily"}}}), 0600))
			}
			_, err = replay("source-control", json.RawMessage(`{"query":"public policy"}`))
			if !errors.As(err, &typed) || typed.Failure.Outcome != "not-started" || f.calls.Load() != 0 {
				t.Fatal("refusal replay became success or source work", err)
			}
			reloaded, readErr := f.e.writer.lease.store.Read(t.Context(), record.Binding, record.ID)
			assistantRunMust(t, readErr)
			if len(assistantRunPublic(reloaded).ExternalCalls) != 1 {
				t.Fatal("reload lost authenticated control")
			}
		})
	}
}

func TestAssistantExternalControlInputAndUnknownStayErrors(t *testing.T) {
	binding := assistantExternalBindingVector(assistantSearchTool)
	ctx := context.WithValue(t.Context(), assistantHostSourceKey{}, binding)
	for _, raw := range []string{`{}`, `{"query":""}`, string(assistantChatJSON(map[string]string{"query": strings.Repeat("x", 2001)}))} {
		_, parseErr := readAssistantSearchQuery([]byte(raw))
		_, err := assistantExternalInputFailure(ctx, assistantSearchTool, parseErr)
		var typed *failure.Error
		if parseErr == nil || !errors.As(err, &typed) || typed.Failure.Outcome != "not-started" {
			t.Fatal("invalid sourced input returned nil", err)
		}
	}
	cause := failure.Err("storage", "tool", "not-started")
	observed, err := assistantExternalObservationFailure(ctx, assistantSearchTool, assistantHostFailure(cause), cause)
	var typed *failure.Error
	if !errors.As(err, &typed) || typed.Failure.Outcome != "unknown" || observed.ExternalNotStarted != nil {
		t.Fatal("uncertain failure claimed no acquisition", err)
	}
	// A legacy unsourced path retains its established ToolResult,nil behavior.
	_, err = assistantExternalInputFailure(t.Context(), assistantSearchTool, cause)
	if err != nil {
		t.Fatal("legacy error behavior changed")
	}
}

func TestAssistantExternalControlSharedProtocolVectors(t *testing.T) {
	raw, err := os.ReadFile("testdata/assistant-external-controls.json")
	assistantRunMust(t, err)
	var vectors struct {
		Valid []struct {
			Name      string          `json:"name"`
			Tool      string          `json:"tool"`
			Arguments json.RawMessage `json:"arguments"`
			Control   json.RawMessage `json:"control"`
		} `json:"valid"`
		InvalidPatches []map[string]json.RawMessage `json:"invalidPatches"`
		InvalidJSON    []string                     `json:"invalidJSON"`
	}
	assistantRunMust(t, json.Unmarshal(raw, &vectors))
	read := func(raw []byte, tool string, args []byte) bool {
		var c assistantExternalControl
		return validUTF8(raw) && uniqueJSON(raw) && readAssistantHostReceipt(raw, &c) == nil && assistantExternalControlShape(c, tool, args)
	}
	for _, v := range vectors.Valid {
		if !read(v.Control, v.Tool, v.Arguments) {
			t.Fatal("shared valid control refused", v.Name)
		}
		var fields map[string]json.RawMessage
		assistantRunMust(t, json.Unmarshal(v.Control, &fields))
		for field := range fields {
			if field == "query" || field == "provider" {
				continue
			}
			copied := map[string]json.RawMessage{}
			for key, val := range fields {
				if key != field {
					copied[key] = val
				}
			}
			if read(assistantChatJSON(copied), v.Tool, v.Arguments) {
				t.Fatal("required shared control member omitted", v.Name, field)
			}
		}
	}
	seed := vectors.Valid[0]
	for i, patch := range vectors.InvalidPatches {
		var fields map[string]json.RawMessage
		assistantRunMust(t, json.Unmarshal(seed.Control, &fields))
		for key, value := range patch {
			fields[key] = value
		}
		if read(assistantChatJSON(fields), seed.Tool, seed.Arguments) {
			t.Fatal("shared invalid control accepted", i)
		}
	}
	for _, bad := range vectors.InvalidJSON {
		if read([]byte(bad), seed.Tool, seed.Arguments) {
			t.Fatal("shared invalid JSON accepted", bad)
		}
	}
}

func TestAssistantExternalControlCanonicalUnknownIsNotSuccess(t *testing.T) {
	run, in, op := assistantExternalCustodyVector()
	run.Binding.RequestID = "request-one"
	op.State = "refused"
	control, err := assistantExternalControlFor(run, op, in, &assistantExternalNotStarted{kind: "setup-required", reason: "configuration-required"})
	assistantRunMust(t, err)
	cause := assistantExternalControlFailure(*control)
	result := assistantHostResult{Result: assistantHostFailure(cause).Value().JSON(), ExternalControl: control, ExternalFailure: assistantExternalFailureBytes(cause)}
	op.Result = assistantChatJSON(result)
	d := &run.Checkpoint.Durable.Dispatches[0]
	d.State = "unknown"
	held := failure.Normalize(cause, "tool", time.Time{})
	d.Failure = &held
	assistantRunMust(t, validateAssistantExternalResult(run, op, in, result))
	if !assistantCanonicalHostResultMatches(run, op, in) || assistantCanonicalHostSucceeded(run, op, in) || len(run.Checkpoint.Durable.Observations) != 0 {
		t.Fatal("physical control created successful canonical custody")
	}
	d.State = "result"
	d.Result = assistantHostFailure(cause).Value().JSON()
	if validateAssistantExternalResult(run, op, in, result) == nil {
		t.Fatal("canonical result was substituted for durable error shape")
	}
	d.State = "unknown"
	d.Result = nil
	d.Failure = nil
	if validateAssistantExternalResult(run, op, in, result) == nil {
		t.Fatal("unknown failure metadata lost its physical association")
	}
}

func TestAssistantExternalControlNilErrorNeverExemptsReceipt(t *testing.T) {
	for _, mode := range []string{"plain-result", "is-error-result", "caught-error"} {
		t.Run(mode, func(t *testing.T) {
			f := newAssistantSearchFixture(t, "api", "ask", "work", "http://127.0.0.1:1")
			invoke, _ := assistantExternalEvidenceControlScope(t, f, func(context.Context, bool) (assistantHostObservation, error) {
				tool := runcontext.ResultOf([]runcontext.Content{runcontext.TextContent("Finite fixture result")}, nil).WithIsError(mode == "is-error-result")
				if mode == "caught-error" {
					return assistantHostObservation{}, errors.New("raw failure https://secret.invalid/?token=private")
				}
				return assistantHostObservation{Result: tool}, nil
			})
			result, err := invoke("no-proof", json.RawMessage(`{"query":"public policy"}`))
			var typed *failure.Error
			if !errors.As(err, &typed) || typed.Failure.Outcome != "unknown" || result.Receipt != "" {
				t.Fatal("unverified nil-error outcome escaped", err)
			}
			record := assistantExecutionRecord(t, f.e)
			for _, op := range record.Operations {
				if op.Kind != "host" {
					continue
				}
				var retained assistantHostResult
				assistantRunMust(t, readAssistantHostReceipt(op.Result, &retained))
				if retained.ExternalControl != nil || bytes.Contains(op.Result, []byte("private")) {
					t.Fatal("unknown was relabeled or raw failure escaped")
				}
			}
			calls := assistantRunPublic(record).ExternalCalls
			if len(calls) != 1 || calls[0].Outcome != "unknown" || calls[0].Control != nil || f.calls.Load() != 0 {
				t.Fatal("absence of acquisition was inferred as a control", calls)
			}
		})
	}
}
