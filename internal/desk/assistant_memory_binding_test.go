package desk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

// These cases use the authenticated persisted execution, actual API adapter or
// native Manager, question pause, current binding admission and person answer.
// They do not construct replacement checkpoints or replace the prompt host.
func TestAssistantMemoryActualBindings(t *testing.T) {
	for _, native := range []bool{false, true} {
		for _, action := range []string{"forget", "clear", "edit", "disable", "dependency", "expiry"} {
			t.Run(action+map[bool]string{false: "-api", true: "-native"}[native], func(t *testing.T) {
				entry := enrichedMemoryFixture()
				entry.Key = "review"
				entry.Text = "Review the original evidence concisely."
				// Independent fixture digest, not a captured production result.
				entry.Canonical.Digest = "sha256:262dafaf220ccca0ce0a1f2f4d33b8ea7a824d70bcf95600bbe9af97c90c2bbe"
				prepare := func(s *Server, _ string, start assistantRunStart) assistantRunStart {
					assistantFixtureReviewOff(s)
					if action == "dependency" {
						assistantRunMust(t, os.WriteFile(filepath.Join(s.projectDir, "notes.txt"), []byte("first"), 0600))
						entry.Dependencies = []memoryDependency{{Path: "notes.txt", SHA256: digestOf([]byte("first"))}}
					}
					seedCanonicalMemory(t, s, entry)
					return start
				}
				var requests atomic.Int32
				var u *upstream
				var trace string
				var s *Server
				var token string
				var e *assistantRunExecution
				proceed := make(chan struct{})
				execute := func(ctx context.Context, e *assistantRunExecution) error {
					<-proceed
					x := assistantExecutionOpen(t, e.service.server, e)
					defer x.Close()
					assistantContinuationOfferQuestion(t, x)
					x.Run.Critic = "off"
					err := x.Adapter.Run(ctx, x.Run)
					for _, event := range assistantExecutionRecord(t, e).Events {
						if event.Event.Error != nil {
							t.Logf("adapter refusal: %+v", event.Event.Error)
						}
					}
					return err
				}
				if native {
					s, token, e, _, trace = assistantCodexSourceFixtureExecute(t, "continuation-question", "work", testAIAlpha, `{"profileVersion":1,"codex":{"inherit":true},"projectInstructions":"HOST-INSTRUCTIONS-ONCE","context":{"version":1,"projectBrief":"execution-brief.md"}}`, prepare, execute)
				} else {
					u = newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
						n := requests.Add(1)
						body := string(u.arrivals()[int(n)-1].body)
						if n == 1 && (!strings.Contains(body, entry.Text) || !strings.Contains(body, entry.Canonical.ID)) {
							t.Error("typed recall missing on actual API request", body)
						}
						if n > 1 && strings.Contains(body, entry.Text) {
							t.Error("deleted text reconstructed on API resume", body)
						}
						w.Header().Set("Content-Type", "text/event-stream")
						if n == 1 {
							args, _ := json.Marshal(`{"title":"Scope","question":"Which scope?","selection":"single","options":[{"id":"one","label":"One"},{"id":"two","label":"Two"}]}`)
							_, _ = fmt.Fprintf(w, "data: {\"id\":\"fixture\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"ask-one\",\"type\":\"function\",\"function\":{\"name\":\"ask_user\",\"arguments\":%s}}]},\"finish_reason\":null}]}\n\ndata: {\"id\":\"fixture\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n", args)
						} else {
							_, _ = io.WriteString(w, "data: {\"id\":\"done\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Answer retained.\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"done\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
						}
					})
					s, token, e = assistantExecutionFixtureSource(t, u.server.URL, "work", execute, prepare)
				}
				close(proceed)
				paused := assistantProtocolWait(t, s, token, e.record.ID, "awaiting-input")
				if paused.Checkpoint.Memories == nil || len(paused.Checkpoint.Memories.Recalled) != 1 || paused.Checkpoint.Memories.Recalled[0].ID != entry.Canonical.ID {
					t.Fatal("per-record recall not retained", paused.Checkpoint)
				}
				switch action {
				case "forget":
					changeMemory(t, s, memoryChange{Action: "forget", ID: entry.ID})
				case "clear":
					changeMemory(t, s, memoryChange{Action: "clear"})
				case "edit":
					changeMemory(t, s, memoryChange{Action: "edit", ID: entry.ID, Text: "Review current evidence carefully."})
				case "disable":
					changeMemory(t, s, memoryChange{Action: "settings", Settings: &memorySettings{Use: false}})
				case "dependency":
					assistantRunMust(t, os.WriteFile(filepath.Join(s.projectDir, "notes.txt"), []byte("second"), 0600))
				case "expiry":
					expiry := "2020-01-01T00:00:00Z"
					entry.Canonical.ExpiresAt = &expiry
					seedCanonicalMemory(t, s, entry)
				}
				assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+paused.ID+"/actions", assistantContinuationActualAnswer(paused)), 200)
				complete := assistantProtocolWait(t, s, token, e.record.ID, "complete")
				want := "revoked"
				if action == "expiry" {
					want = "expired"
				}
				var retained *contract.ProvenanceMemoryRecall
				for i := range complete.Checkpoint.Memories.Recalled {
					row := &complete.Checkpoint.Memories.Recalled[i]
					if row.ID == entry.Canonical.ID {
						retained = row
					}
				}
				if retained == nil || retained.State != want || retained.Digest != entry.Canonical.Digest {
					t.Fatal("current-binding resume lost lifecycle", complete.Checkpoint.Memories)
				}
				memoryReload(t, *complete.Checkpoint)
				if native {
					generations := 0
					for _, row := range assistantCodexLog(t, trace) {
						if string(row["type"]) != `"generation"` {
							continue
						}
						var request struct{ BaseInstructions string }
						assistantRunMust(t, json.Unmarshal(row["request"], &request))
						if generations == 0 && (!strings.Contains(request.BaseInstructions, entry.Text) || !strings.Contains(request.BaseInstructions, entry.Canonical.ID)) {
							t.Fatal("typed native recall missing", request)
						}
						if generations > 0 && strings.Contains(request.BaseInstructions, entry.Text) {
							t.Fatal("deleted native recall reconstructed", request)
						}
						generations++
					}
					if generations != 2 || complete.Checkpoint.Unit != "provider-managed" || complete.Checkpoint.Used != 0 || complete.Checkpoint.Limit != 1 || complete.Checkpoint.RequestCap != nil {
						t.Fatal("native accounting changed", generations, complete.Checkpoint)
					}
				} else if requests.Load() != 2 {
					t.Fatal("API continuation repeated requests", requests.Load())
				}
			})
		}
	}
}
