package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

// Drive the original MCP bytes through Desk's real construction and both
// adapters. Bootstrap reads are allowed; malformed identity must stop before
// the model, native generation or an acting tool is dispatched.
func TestAssistantRuntimeIdentityActualAdapterRefusal(t *testing.T) {
	var descriptor string
	for _, row := range assistantRuntimeIdentityTestVectors(t).MalformedVersions {
		if row.ID == "version-lone-high-structured" {
			descriptor = row.Result
		}
	}
	if descriptor == "" {
		t.Fatal("missing shared malformed Runtime descriptor")
	}
	for _, engine := range []string{"api", "native"} {
		t.Run(engine, func(t *testing.T) {
			upstream := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				t.Error("malformed Runtime identity reached the model provider")
				w.WriteHeader(http.StatusInternalServerError)
			})
			var s *Server
			var e *assistantRunExecution
			trace := ""
			if engine == "native" {
				s, _, e, _, trace = assistantCodexFixture(t, "read", "work")
			} else {
				s, _, e = assistantExecutionFixture(t, upstream.server.URL, "work")
			}
			assistantExecutionRuntimeFixture(t, s, e)
			quoted, err := json.Marshal(descriptor)
			assistantRunMust(t, err)
			const boundary = " elif method=='prompts/get':"
			if strings.Count(assistantExecutionPeer, boundary) != 1 {
				t.Fatal("Runtime fixture dispatch boundary changed")
			}
			peer := strings.Replace(assistantExecutionPeer, boundary, " elif method=='tools/call' and name=='describe_runtime': result=json.loads("+string(quoted)+")\n"+boundary, 1)
			assistantRunMust(t, os.WriteFile(s.cfg.JpackBin, []byte(peer), 0700))
			x, err := e.openAssistantExecution(t.Context(), assistantExecutionPrompt{Name: "served"})
			assistantRunMust(t, err)
			defer x.Close()
			if x.Run.Context.Runtime == nil || x.Run.Context.Runtime.Version == "" {
				t.Fatal("malformed Runtime version became unavailable before admission")
			}
			before := assistantExecutionRecord(t, e)
			// Observe the actual adapter callback as well as the persisted sink. A
			// rejected pre-boundary event must remain visible in failure diagnostics.
			emit := x.Run.Emit
			type refusalSummary struct {
				Code, Source, Outcome string
				DetailMatch           bool
			}
			var emittedFailures []refusalSummary
			x.Run.Emit = func(event contract.RunEvent) {
				if event.Error != nil && event.Error.Failure != nil && len(emittedFailures) < 8 {
					f := event.Error.Failure
					emittedFailures = append(emittedFailures, refusalSummary{f.Code, f.Source, f.Outcome, f.Detail != nil && strings.Contains(*f.Detail, "Runtime identity refused")})
				}
				emit(event)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			assistantRunMust(t, x.Adapter.Run(ctx, x.Run))
			after := assistantExecutionRecord(t, e)
			// Published Authored configuration failures retain outcome unknown;
			// the assertions below separately prove that no dispatch started.
			refused := false
			for _, event := range after.Events {
				if event.Event.Error != nil && event.Event.Error.Failure != nil {
					f := event.Event.Error.Failure
					if f.Code == "configuration" && f.Source == "context" && f.Outcome == "unknown" && f.Detail != nil && strings.Contains(*f.Detail, "Runtime identity refused") {
						refused = true
					}
				}
				if event.Event.ToolCall != nil || event.Event.ApprovalRequested != nil {
					t.Fatal("malformed Runtime identity reached tool dispatch or approval")
				}
			}
			if !refused || len(upstream.arrivals()) != 0 || after.Checkpoint.Used != before.Checkpoint.Used {
				t.Logf("Runtime identity refusal evidence engine=%s refused=%t arrivals=%d beforeUsed=%d afterUsed=%d events=%d operations=%d emittedFailures=%d", engine, refused, len(upstream.arrivals()), before.Checkpoint.Used, after.Checkpoint.Used, len(after.Events), len(after.Operations), len(emittedFailures))
				for i, f := range emittedFailures {
					t.Logf("Runtime identity emitted failure index=%d code=%.80s source=%.80s outcome=%.80s detailMatch=%t", i, f.Code, f.Source, f.Outcome, f.DetailMatch)
				}
				for i, event := range after.Events {
					if i == 8 {
						break
					}
					if event.Event.Error != nil && event.Event.Error.Failure != nil {
						f := event.Event.Error.Failure
						t.Logf("Runtime identity persisted failure index=%d code=%.80s source=%.80s outcome=%.80s detailMatch=%t", i, f.Code, f.Source, f.Outcome, f.Detail != nil && strings.Contains(*f.Detail, "Runtime identity refused"))
					}
				}
				t.Fatal("actual adapter did not deliver refusal before model use")
			}
			for _, op := range after.Operations {
				if op.Kind == "model" {
					t.Fatal("malformed Runtime identity admitted a model operation")
				}
			}
			if trace != "" {
				log, err := os.ReadFile(trace)
				if err != nil && !errors.Is(err, os.ErrNotExist) {
					t.Fatal(err)
				}
				if bytes.Contains(log, []byte(`"type": "generation"`)) {
					t.Fatal("malformed Runtime identity started native generation")
				}
			}
		})
	}
}
