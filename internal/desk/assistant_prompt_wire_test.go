package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-agent/go/relay"
)

// Deleting the configured brief after the Host reads it distinguishes adapter
// assembly from a hidden second relay read. It does not alter the profile/source
// identity that admission rechecks. Memory uses the real private Host store.
type assistantPromptOnceHost struct {
	*assistantExecutionPromptObserver
	briefPath string
}

func (h *assistantPromptOnceHost) Brief(ctx context.Context) (string, error) {
	text, err := h.assistantExecutionPromptObserver.Brief(ctx)
	if err == nil {
		err = os.Remove(h.briefPath)
	}
	return text, err
}

func TestAssistantPromptCanonicalAdapterOwnsEveryWire(t *testing.T) {
	for _, family := range []string{"openai-compatible", "anthropic", "gemini", "native"} {
		t.Run(family, func(t *testing.T) {
			var s *Server
			var e *assistantRunExecution
			var trace string
			var upstream *upstream
			if family == "native" {
				s, _, e, _, trace = assistantCodexSourceFixture(t, "tool", "work", testAIAlpha, `{"profileVersion":1,"codex":{"inherit":true},"projectInstructions":"HOST-INSTRUCTIONS-ONCE","context":{"version":1,"projectBrief":"execution-brief.md"}}`, func(s *Server, _ string, start assistantRunStart) assistantRunStart {
					assistantFixtureReviewOff(s)
					return start
				})
			} else {
				upstream = newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					response := `{"id":"fixture","object":"chat.completion","model":"shared-model","choices":[{"index":0,"message":{"role":"assistant","content":"Fixture answer."},"finish_reason":"stop"}]}`
					if family == "anthropic" {
						response = `{"id":"fixture","type":"message","role":"assistant","model":"shared-model","content":[{"type":"text","text":"Fixture answer."}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`
					} else if family == "gemini" {
						response = `{"candidates":[{"content":{"role":"model","parts":[{"text":"Fixture answer."}]},"finishReason":"STOP"}]}`
					}
					_, _ = io.WriteString(w, response)
				})
				s, _, e = assistantExecutionFixtureSource(t, upstream.server.URL, "work", nil, func(s *Server, _ string, start assistantRunStart) assistantRunStart {
					assistantFixtureReviewOff(s)
					return start
				}, family)
			}
			changeMemory(t, s, memoryChange{Action: "settings", Settings: &memorySettings{Use: true}})
			changeMemory(t, s, memoryChange{Action: "save", Key: "exact-input", Text: "HOST-MEMORY-ONCE: Review exact input carefully."})
			x := assistantExecutionOpen(t, s, e)
			observer := &assistantExecutionPromptObserver{Host: x.Run.Host}
			x.Run.Host = &assistantPromptOnceHost{assistantExecutionPromptObserver: observer, briefPath: filepath.Join(s.projectDir, "execution-brief.md")}
			x.Run.Critic = "off"
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			assistantRunMust(t, x.Adapter.Run(ctx, x.Run))
			if observer.brief != 1 || observer.instructions != 1 || observer.memory != 1 || observer.query != "Review exact input" {
				raw, _ := json.Marshal(assistantExecutionRecord(t, e).Events)
				t.Fatalf("adapter Host reads/query changed: %+v record=%s", observer, raw)
			}
			record := assistantExecutionRecord(t, e)
			if record.Checkpoint.Status != "complete" || len(record.Checkpoint.Prompts) != 1 {
				t.Fatal("actual adapter failed to complete with prompt provenance", record.Checkpoint.Status)
			}
			wire := ""
			if family == "native" {
				for _, row := range assistantCodexLog(t, trace) {
					if string(row["type"]) != `"generation"` {
						continue
					}
					var request struct{ BaseInstructions string }
					assistantRunMust(t, json.Unmarshal(row["request"], &request))
					wire = request.BaseInstructions
				}
			} else {
				arrival := upstream.only(t)
				wire = string(arrival.body)
				matched := false
				for _, op := range record.Operations {
					if op.Kind != "model" {
						continue
					}
					var intent struct{ Request relay.Request }
					assistantRunMust(t, json.Unmarshal(op.Intent, &intent))
					if !bytes.Equal(intent.Request.Body, arrival.body) {
						t.Fatal("relay changed exact adapter request bytes")
					}
					matched = true
				}
				if !matched {
					t.Fatal("actual relay dispatch receipt missing")
				}
			}
			previous := -1
			for _, marker := range []string{"HOST-BRIEF-ONCE", "HOST-INSTRUCTIONS-ONCE", "HOST-MEMORY-ONCE"} {
				at := strings.Index(wire, marker)
				if at <= previous || strings.Count(wire, marker) != 1 {
					t.Fatalf("adapter layer omitted, reordered or appended: %s", marker)
				}
				previous = at
			}
			if strings.Contains(wire, "Project background (not approval") || strings.Contains(wire, "Recalled project memory (historical") {
				t.Fatal("relay added legacy prompt framing")
			}
		})
	}
}
