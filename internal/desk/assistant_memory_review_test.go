package desk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

func memoryReviewRead(t *testing.T, s *Server, token string) memoryReply {
	t.Helper()
	w := policyCall(s, token, "GET", "/api/ai-memory", nil)
	var out memoryReply
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatalf("memory read: %d %s", w.Code, w.Body.String())
	}
	return out
}
func memoryReviewPost(t *testing.T, s *Server, token, revision string, change memoryReviewChange) memoryReply {
	t.Helper()
	w := policyWorkspaceCall(s, token, "POST", "/api/ai-memory", revision, memoryChange{Action: "review", Review: &change})
	var out memoryReply
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatalf("memory review: %d %s", w.Code, w.Body.String())
	}
	return out
}
func memoryReviewAction(p memoryProposalReview, decision string) memoryReviewChange {
	return memoryReviewChange{ID: randomStartupID(), RunID: p.RunID, RunRevision: p.RunRevision, ProposalID: p.Memory.ID, ProposalDigest: p.Memory.Digest, Decision: decision}
}

func memoryProposalFixture(t *testing.T, native bool, phase string, suggest bool) (*Server, string, assistantRunView) {
	t.Helper()
	proceed := make(chan struct{})
	prepare := func(s *Server, _ string, start assistantRunStart) assistantRunStart {
		assistantFixtureReviewOff(s)
		changeMemory(t, s, memoryChange{Action: "settings", Settings: &memorySettings{Suggest: suggest, Use: false}})
		return start
	}
	execute := func(ctx context.Context, e *assistantRunExecution) error {
		<-proceed
		x := assistantExecutionOpen(t, e.service.server, e)
		defer x.Close()
		x.Run.Critic = "off"
		return x.Adapter.Run(ctx, x.Run)
	}
	var s *Server
	var token string
	var execution *assistantRunExecution
	if native {
		s, token, execution, _, _ = assistantCodexSourceFixtureExecute(t, "memory", phase, testAIAlpha, `{"profileVersion":1,"codex":{"inherit":true}}`, prepare, execute)
	} else {
		var count atomic.Int32
		u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			if count.Add(1) == 1 {
				args, _ := json.Marshal(`{"kind":"preference","text":"Keep outreach concise."}`)
				_, _ = fmt.Fprintf(w, "data: {\"id\":\"fixture\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"remember-one\",\"type\":\"function\",\"function\":{\"name\":\"remember\",\"arguments\":%s}}]},\"finish_reason\":null}]}\n\ndata: {\"id\":\"fixture\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n", args)
			} else {
				_, _ = io.WriteString(w, "data: {\"id\":\"done\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Proposal recorded only.\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"done\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			}
		})
		s, token, execution = assistantExecutionFixtureSource(t, u.server.URL, phase, execute, prepare)
	}
	close(proceed)
	return s, token, assistantProtocolWait(t, s, token, execution.record.ID, "complete")
}

func TestAssistantMemoryProposalActual(t *testing.T) {
	for _, native := range []bool{false, true} {
		for _, phase := range []string{"work", "plan"} {
			for _, suggest := range []bool{false, true} {
				name := fmt.Sprintf("%s-%t-%t", phase, native, suggest)
				t.Run(name, func(t *testing.T) {
					s, token, run := memoryProposalFixture(t, native, phase, suggest)
					before := memoryReviewRead(t, s, token)
					if len(before.Content.Entries) != 0 {
						t.Fatal("proposal automatically stored or accepted")
					}
					if !suggest {
						if len(before.Proposals) != 0 || len(run.Checkpoint.Memories.Proposed) != 0 {
							t.Fatal("Suggest off permitted a proposal")
						}
						if member := run.Checkpoint.Policy["budget.memoryProposalsPerRun"]; string(member.Value) != "0" {
							t.Fatal("nested released zero proposal budget not admitted", run.Checkpoint.Policy)
						}
						return
					}
					if len(before.Proposals) != 1 || !before.Proposals[0].Reviewable || before.Content.Settings.Use {
						t.Fatal("proposal/recall controls coupled", before)
					}
					proposal := before.Proposals[0]
					if proposal.Memory.ID != run.Checkpoint.Memories.Proposed[0].ID || proposal.Memory.Digest != run.Checkpoint.Memories.Proposed[0].Digest || proposal.Memory.Source.Run != run.Checkpoint.ID || proposal.TurnID != run.RequestID {
						t.Fatal("canonical proposal binding changed", proposal)
					}
					// GET and Stop/read never accept; no proposal is copied into a second store.
					readAgain := memoryReviewRead(t, s, token)
					if readAgain.SHA256 != before.SHA256 || len(readAgain.Content.Entries) != 0 {
						t.Fatal("observational GET wrote proposal")
					}
					change := memoryReviewAction(proposal, "accept")
					if phase == "plan" {
						change.Decision = "edit"
						text := "Keep outreach evidence explicit."
						change.Text = &text
					}
					after := memoryReviewPost(t, s, token, before.SHA256, change)
					if len(after.Content.Entries) != 1 || len(after.Content.Reviews) != 1 || len(after.Proposals) != 0 {
						t.Fatal("human review result missing", after)
					}
					entry := after.Content.Entries[0]
					if entry.Source.ChatID != run.Owner.ID || entry.Source.TurnID != run.RequestID || entry.Canonical == nil || entry.Key != memoryTopic(proposal.Memory, entry.Text) {
						t.Fatal("model supplied management attribution", entry)
					}
					if phase == "work" && (entry.Canonical.ID != proposal.Memory.ID || entry.Canonical.Digest != proposal.Memory.Digest) {
						t.Fatal("unchanged acceptance rewrote canonical identity")
					}
					if phase == "plan" && (entry.Canonical.ID == proposal.Memory.ID || entry.Canonical.Source.ProposedBy != "person" || after.Content.Reviews[0].ProposalID != proposal.Memory.ID) {
						t.Fatal("edited proposal reused old identity")
					}
					// A known completed delivery reconciles, including after Clear; it never replays.
					replay := memoryReviewPost(t, s, token, before.SHA256, change)
					if replay.SHA256 != after.SHA256 {
						t.Fatal("known receipt replay wrote again")
					}
					other := change
					other.Decision = "decline"
					other.Text = nil
					if w := policyWorkspaceCall(s, token, "POST", "/api/ai-memory", before.SHA256, memoryChange{Action: "review", Review: &other}); w.Code == 200 {
						t.Fatal("completed receipt reconciled a different intent")
					}
					other = memoryReviewAction(proposal, "accept")
					if w := policyWorkspaceCall(s, token, "POST", "/api/ai-memory", after.SHA256, memoryChange{Action: "review", Review: &other}); w.Code == 200 {
						t.Fatal("another delivery accepted the same proposal again")
					}
					host, err := s.assistantPromptHost(t.Context())
					assistantRunMust(t, err)
					answer, err := host.MemoryFor(t.Context(), "outreach")
					assistantRunMust(t, err)
					if len(answer.([]contract.Memory)) != 0 {
						t.Fatal("Use off recalled accepted memory")
					}
					changeMemory(t, s, memoryChange{Action: "settings", Settings: &memorySettings{Suggest: true, Use: true}})
					host, err = s.assistantPromptHost(t.Context())
					assistantRunMust(t, err)
					answer, err = host.MemoryFor(t.Context(), "outreach")
					assistantRunMust(t, err)
					rows := answer.([]contract.Memory)
					if len(rows) != 1 || rows[0].ID != entry.Canonical.ID || rows[0].Digest != entry.Canonical.Digest {
						t.Fatal("accepted typed recall changed", answer)
					}
					if output := os.Getenv("JPS_MEMORY_OUTPUTS"); output != "" {
						raw, _ := json.Marshal(struct {
							Run           assistantRunView `json:"run"`
							Before, After memoryReply
						}{run, before, after})
						assistantRunMust(t, os.WriteFile(filepath.Join(output, name+".json"), raw, 0600))
					}
					changeMemory(t, s, memoryChange{Action: "clear"})
					reconciled := memoryReviewPost(t, s, token, before.SHA256, change)
					if len(reconciled.Content.Entries) != 0 || len(reconciled.Content.Reviews) != 1 {
						t.Fatal("receipt reconstructed cleared content")
					}
				})
			}
		}
	}
}

func TestAssistantMemoryReviewRefusals(t *testing.T) {
	s, token, _ := memoryProposalFixture(t, false, "work", true)
	before := memoryReviewRead(t, s, token)
	proposal := before.Proposals[0]
	for _, kind := range []string{"store-revision", "run-revision", "proposal-digest", "proposal-id", "sensitive", "session", "unknown-decision"} {
		t.Run(kind, func(t *testing.T) {
			change := memoryReviewAction(proposal, "accept")
			revision, session := before.SHA256, token
			switch kind {
			case "store-revision":
				revision = strings.Repeat("0", 64)
			case "run-revision":
				change.RunRevision++
			case "proposal-digest":
				change.ProposalDigest = "sha256:" + strings.Repeat("0", 64)
			case "proposal-id":
				change.ProposalID = "different-proposal"
			case "sensitive":
				change.Decision = "edit"
				secret := "api_key: private-fixture"
				change.Text = &secret
			case "session":
				session = policyTestSession(t, s)
			case "unknown-decision":
				change.Decision = "automatic"
			}
			w := policyWorkspaceCall(s, session, "POST", "/api/ai-memory", revision, memoryChange{Action: "review", Review: &change})
			if w.Code == 200 {
				t.Fatal("unowned/stale/invalid review accepted", kind)
			}
			var failure map[string]any
			if json.Unmarshal(w.Body.Bytes(), &failure) != nil || failure["error"] == nil {
				t.Fatal("malformed refusal response", w.Body.String())
			}
			if current := memoryReviewRead(t, s, token); current.SHA256 != before.SHA256 || len(current.Content.Reviews) != 0 || len(current.Content.Entries) != 0 {
				t.Fatal("refused review mutated store")
			}
		})
	}
	for _, body := range []any{
		map[string]any{"action": "review", "review": map[string]any{}},
		map[string]any{"action": "review", "review": nil},
		map[string]any{"action": "review", "source": map[string]any{"chatId": "different", "turnId": "different"}, "review": memoryReviewAction(proposal, "accept")},
		map[string]any{"Action": "review", "review": memoryReviewAction(proposal, "accept")},
	} {
		if w := policyWorkspaceCall(s, token, "POST", "/api/ai-memory", before.SHA256, body); w.Code == 200 {
			t.Fatal("malformed or caller-derived provenance accepted")
		}
	}
	// Current saved latest-user attribution is rechecked, rather than trusting
	// the run's old visible source-chat link after a new person turn is saved.
	saved := policyWorkspaceRead(t, s, token, "/api/conversations")
	var document conversationDocument
	assistantRunMust(t, json.Unmarshal(saved.Content, &document))
	var chat map[string]any
	assistantRunMust(t, json.Unmarshal(document.Chats[0], &chat))
	state := chat["checkpoint"].(map[string]any)["state"].(map[string]any)
	state["turns"] = append(state["turns"].([]any), map[string]any{"id": "new-user", "role": "user", "kind": "message", "text": "Current request", "at": "2026-10-24T10:00:00Z"})
	document.Chats[0], _ = json.Marshal(chat)
	if w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, document); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := policyWorkspaceCall(s, token, "POST", "/api/ai-memory", before.SHA256, memoryChange{Action: "review", Review: ptrMemoryReview(memoryReviewAction(proposal, "accept"))}); w.Code == 200 {
		t.Fatal("old proposal accepted against a newer user turn")
	}
}

func ptrMemoryReview(change memoryReviewChange) *memoryReviewChange { return &change }

func TestAssistantMemoryReviewDeclineAndBarrier(t *testing.T) {
	for _, operation := range []string{"decline", "clear-before", "forgotten-source", "suggest-off", "concurrent"} {
		t.Run(operation, func(t *testing.T) {
			s, token, _ := memoryProposalFixture(t, false, "plan", true)
			before := memoryReviewRead(t, s, token)
			proposal := before.Proposals[0]
			change := memoryReviewAction(proposal, "accept")
			switch operation {
			case "decline":
				change.Decision = "decline"
			case "clear-before":
				changeMemory(t, s, memoryChange{Action: "clear"})
			case "forgotten-source":
				accepted := memoryReviewPost(t, s, token, before.SHA256, change)
				changeMemory(t, s, memoryChange{Action: "forget", ID: accepted.Content.Entries[0].ID})
				fresh := memoryReviewAction(proposal, "accept")
				current := memoryReviewRead(t, s, token)
				if w := policyWorkspaceCall(s, token, "POST", "/api/ai-memory", current.SHA256, memoryChange{Action: "review", Review: &fresh}); w.Code == 200 {
					t.Fatal("forgotten proposal recreated with another delivery ID")
				}
				replayed := memoryReviewPost(t, s, token, before.SHA256, change)
				if len(replayed.Content.Entries) != 0 {
					t.Fatal("known receipt reconstructed forgotten text")
				}
				return
			case "suggest-off":
				changeMemory(t, s, memoryChange{Action: "settings", Settings: &memorySettings{Suggest: false, Use: false}})
			case "concurrent":
				results := make(chan int, 2)
				for range 2 {
					go func() {
						request := memoryReviewAction(proposal, "accept")
						results <- policyWorkspaceCall(s, token, "POST", "/api/ai-memory", before.SHA256, memoryChange{Action: "review", Review: &request}).Code
					}()
				}
				first, second := <-results, <-results
				if !((first == 200 && second == 409) || (first == 409 && second == 200)) {
					t.Fatal("two concurrent acceptance effects", first, second)
				}
				current := memoryReviewRead(t, s, token)
				if len(current.Content.Entries) != 1 || len(current.Content.Reviews) != 1 {
					t.Fatal("acceptance CAS lost atomic receipt")
				}
				return
			}
			current := memoryReviewRead(t, s, token)
			if operation == "clear-before" {
				if w := policyWorkspaceCall(s, token, "POST", "/api/ai-memory", current.SHA256, memoryChange{Action: "review", Review: &change}); w.Code == 200 {
					t.Fatal("Clear generation bypassed")
				}
				return
			}
			after := memoryReviewPost(t, s, token, current.SHA256, change)
			if operation == "decline" && (len(after.Content.Entries) != 0 || len(after.Content.Reviews) != 1 || len(after.Proposals) != 0) {
				t.Fatal("decline stored text or remained pending", after)
			}
			if operation == "suggest-off" && len(after.Content.Entries) != 1 {
				t.Fatal("Suggest off blocked explicit person review")
			}
		})
	}
}

func TestAssistantMemoryReviewUncertainReadback(t *testing.T) {
	s, token, _ := memoryProposalFixture(t, false, "work", true)
	before := memoryReviewRead(t, s, token)
	change := memoryReviewAction(before.Proposals[0], "accept")
	reads := 0
	testHookPrivateRead = func(name string) error {
		if name == s.memoryName() {
			reads++
			if reads == 2 {
				return fmt.Errorf("fixture readback unavailable")
			}
		}
		return nil
	}
	w := policyWorkspaceCall(s, token, "POST", "/api/ai-memory", before.SHA256, memoryChange{Action: "review", Review: &change})
	testHookPrivateRead = nil
	t.Cleanup(func() { testHookPrivateRead = nil })
	if w.Code == 200 {
		t.Fatal("uncertain readback claimed successful acceptance")
	}
	// The only effect and receipt were one atomic file replacement. A read can
	// identify that completed intent; no GET or retry constructs another entry.
	current := memoryReviewRead(t, s, token)
	if len(current.Content.Entries) != 1 || len(current.Content.Reviews) != 1 || current.Content.Reviews[0].ID != change.ID {
		t.Fatal("atomic completed intent/result lost")
	}
	reconciled := memoryReviewPost(t, s, token, before.SHA256, change)
	if reconciled.SHA256 != current.SHA256 {
		t.Fatal("known completed intent replayed")
	}
	changeMemory(t, s, memoryChange{Action: "forget", ID: current.Content.Entries[0].ID})
	reconciled = memoryReviewPost(t, s, token, before.SHA256, change)
	if len(reconciled.Content.Entries) != 0 {
		t.Fatal("uncertain delivery recovery recreated forgotten text")
	}
}

func TestAssistantMemoryReviewSharedStorageVectors(t *testing.T) {
	var vector struct {
		Document json.RawMessage `json:"document"`
		Required []string        `json:"requiredReceiptMembers"`
		Bad      []struct {
			Member string `json:"member"`
			Value  any    `json:"value"`
		} `json:"badReceiptMembers"`
		BadLinks []map[string]any `json:"badResultLinks"`
	}
	raw, err := os.ReadFile("testdata/assistant-memory-review-v1.json")
	assistantRunMust(t, err)
	assistantRunMust(t, json.Unmarshal(raw, &vector))
	name := "ai-memory-" + strings.Repeat("a", 64) + ".json"
	read := func(raw []byte, valid bool) {
		t.Helper()
		if (validateMemoryStorage(raw) == nil) != valid || (validateStorageFile(name, raw) == nil) != valid {
			t.Fatalf("shared private/backup vector valid=%v: %s", valid, raw)
		}
	}
	read(vector.Document, true)
	decode := func() map[string]any {
		var doc map[string]any
		assistantRunMust(t, json.Unmarshal(vector.Document, &doc))
		return doc
	}
	for _, field := range vector.Required {
		doc := decode()
		delete(doc["reviews"].([]any)[0].(map[string]any), field)
		raw, _ := json.Marshal(doc)
		read(raw, false)
	}
	for _, bad := range vector.Bad {
		doc := decode()
		doc["reviews"].([]any)[0].(map[string]any)[bad.Member] = bad.Value
		raw, _ := json.Marshal(doc)
		read(raw, false)
	}
	for _, bad := range vector.BadLinks {
		doc := decode()
		receipt := doc["reviews"].([]any)[0].(map[string]any)
		for key, value := range bad {
			receipt[key] = value
		}
		raw, _ := json.Marshal(doc)
		read(raw, false)
	}
	for _, decision := range []string{"accept", "edit"} {
		doc := decode()
		receipt := doc["reviews"].([]any)[0].(map[string]any)
		receipt["decision"] = decision
		receipt["entryId"] = "private-one"
		receipt["memoryId"] = "proposal-one"
		receipt["memoryDigest"] = "sha256:" + strings.Repeat("6", 64)
		if decision == "edit" {
			receipt["memoryId"] = "person-memory-one"
		}
		raw, _ := json.Marshal(doc)
		read(raw, true)
	}
	for _, shape := range []any{nil, map[string]any{}, []any{nil}} {
		doc := decode()
		doc["reviews"] = shape
		raw, _ := json.Marshal(doc)
		read(raw, false)
	}
	for _, name := range []string{"Reviews", "REVIEWS"} {
		doc := decode()
		doc[name] = doc["reviews"]
		delete(doc, "reviews")
		raw, _ := json.Marshal(doc)
		read(raw, false)
	}
}

func TestAssistantMemorySuggestCurrentAuthority(t *testing.T) {
	for _, paused := range []bool{false, true} {
		t.Run(fmt.Sprint(paused), func(t *testing.T) {
			arrived, release, proceed := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var requests atomic.Int32
			u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				if requests.Add(1) != 1 {
					t.Error("changed Suggest dispatched another model request")
				}
				close(arrived)
				if !paused {
					<-release
				}
				name, args := "remember", `{"kind":"preference","text":"Keep outreach concise."}`
				if paused {
					name = "ask_user"
					args = `{"title":"Scope","question":"Which scope?","selection":"single","options":[{"id":"one","label":"One"},{"id":"two","label":"Two"}]}`
				}
				encoded, _ := json.Marshal(args)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprintf(w, "data: {\"id\":\"fixture\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"control-one\",\"type\":\"function\",\"function\":{\"name\":%q,\"arguments\":%s}}]},\"finish_reason\":null}]}\n\ndata: {\"id\":\"fixture\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n", name, encoded)
			})
			s, token, e := assistantExecutionFixtureSource(t, u.server.URL, "work", func(ctx context.Context, e *assistantRunExecution) error {
				<-proceed
				x := assistantExecutionOpen(t, e.service.server, e)
				defer x.Close()
				x.Run.Critic = "off"
				if paused {
					assistantContinuationOfferQuestion(t, x)
				}
				return x.Adapter.Run(ctx, x.Run)
			}, func(s *Server, _ string, start assistantRunStart) assistantRunStart {
				assistantFixtureReviewOff(s)
				changeMemory(t, s, memoryChange{Action: "settings", Settings: &memorySettings{Suggest: true}})
				return start
			})
			close(proceed)
			<-arrived
			var original assistantRunView
			if paused {
				original = assistantProtocolWait(t, s, token, e.record.ID, "awaiting-input")
			}
			changeMemory(t, s, memoryChange{Action: "settings", Settings: &memorySettings{Suggest: false}})
			if paused {
				w := policyCall(s, token, "POST", "/api/assistant-runs/"+original.ID+"/actions", assistantContinuationActualAnswer(original))
				if w.Code == 200 {
					t.Fatal("changed Suggest silently changed an admitted continuation budget")
				}
				current := assistantProtocolView(t, policyCall(s, token, "GET", "/api/assistant-runs/"+original.ID, nil), 200)
				if current.Checkpoint.Limit != original.Checkpoint.Limit || current.Checkpoint.Used != original.Checkpoint.Used {
					t.Fatal("refused continuation granted/recounted requests")
				}
			} else {
				close(release)
				current := assistantProtocolWait(t, s, token, e.record.ID, "stopped")
				if len(current.Checkpoint.Memories.Proposed) != 0 {
					t.Fatal("current Suggest off admitted remember")
				}
			}
			if requests.Load() != 1 || len(memoryReviewRead(t, s, token).Content.Entries) != 0 {
				t.Fatal("setting change accepted or replayed work")
			}
		})
	}
}

func TestAssistantMemoryReviewResumeAndStop(t *testing.T) {
	for _, stop := range []bool{false, true} {
		t.Run(fmt.Sprint(stop), func(t *testing.T) {
			proceed := make(chan struct{})
			var requests atomic.Int32
			var u *upstream
			u = newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				n := requests.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				name, args := "remember", `{"kind":"preference","text":"Review exact evidence concisely."}`
				if n == 2 {
					name = "ask_user"
					args = `{"title":"Scope","question":"Which scope?","selection":"single","options":[{"id":"one","label":"One"},{"id":"two","label":"Two"}]}`
				}
				if n < 3 {
					encoded, _ := json.Marshal(args)
					_, _ = fmt.Fprintf(w, "data: {\"id\":\"fixture\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":%q,\"type\":\"function\",\"function\":{\"name\":%q,\"arguments\":%s}}]},\"finish_reason\":null}]}\n\ndata: {\"id\":\"fixture\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n", fmt.Sprint("call-", n), name, encoded)
				} else {
					if !strings.Contains(string(u.arrivals()[int(n)-1].body), "Review exact evidence concisely.") {
						t.Error("accepted typed memory was omitted on actual resume")
					}
					_, _ = io.WriteString(w, "data: {\"id\":\"done\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Done.\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"done\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
				}
			})
			s, token, e := assistantExecutionFixtureSource(t, u.server.URL, "work", func(ctx context.Context, e *assistantRunExecution) error {
				<-proceed
				x := assistantExecutionOpen(t, e.service.server, e)
				defer x.Close()
				x.Run.Critic = "off"
				assistantContinuationOfferQuestion(t, x)
				return x.Adapter.Run(ctx, x.Run)
			}, func(s *Server, _ string, start assistantRunStart) assistantRunStart {
				assistantFixtureReviewOff(s)
				changeMemory(t, s, memoryChange{Action: "settings", Settings: &memorySettings{Suggest: true}})
				return start
			})
			close(proceed)
			paused := assistantProtocolWait(t, s, token, e.record.ID, "awaiting-input")
			if stop {
				assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+paused.ID+"/actions", assistantRunAction{ID: randomStartupID(), Kind: "cancel", Revision: paused.Revision}), 200)
				assistantProtocolWait(t, s, token, e.record.ID, "stopped")
			}
			before := memoryReviewRead(t, s, token)
			if len(before.Content.Entries) != 0 || len(before.Proposals) != 1 {
				t.Fatal("Stop/reload accepted or lost a proposal")
			}
			accepted := memoryReviewPost(t, s, token, before.SHA256, memoryReviewAction(before.Proposals[0], "accept"))
			changeMemory(t, s, memoryChange{Action: "settings", Settings: &memorySettings{Suggest: true, Use: true}})
			if !stop {
				assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+paused.ID+"/actions", assistantContinuationActualAnswer(paused)), 200)
				complete := assistantProtocolWait(t, s, token, e.record.ID, "complete")
				book := memoryReload(t, *complete.Checkpoint).Memories
				if len(book.Proposed) != 1 || len(book.Recalled) != 1 || book.Proposed[0].ID != accepted.Content.Entries[0].Canonical.ID || book.Recalled[0].ID != book.Proposed[0].ID || book.Recalled[0].Digest != book.Proposed[0].Digest || complete.Checkpoint.Used != 3 || complete.Checkpoint.Limit != paused.Checkpoint.Limit {
					t.Fatal("proposal/acceptance/recall lost across checkpoint resume", book)
				}
			} else if requests.Load() != 2 {
				t.Fatal("Stop dispatched another generation")
			}
		})
	}
}

// Exercise actual host creation review, the shared resolver and released API
// adapter. These roles are root Work sessions rather than CriticReading.
func TestAssistantMemoryIndependentReview(t *testing.T) {
	var calls atomic.Int32
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		n := calls.Add(1)
		if n == 1 || n == 3 {
			_, _ = io.WriteString(w, `data: {"id":"fixture","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"remember-review","type":"function","function":{"name":"remember","arguments":"{\"kind\":\"preference\",\"text\":\"Keep outreach concise.\"}"}}]},"finish_reason":null}]}

data: {"id":"fixture","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`)
		} else if n == 2 {
			assistantBriefSSE(w, assistantAuthoringProposalFixture(json.RawMessage(assistantCreationCasesDocument)), "stop")
		} else {
			assistantBriefSSE(w, assistantAuthoringProposalFixture(json.RawMessage(assistantCreationCorrectionDocument)), "stop")
		}

	})
	s, token := assistantCreationActualFixture(t, u.server.URL)
	changeMemory(t, s, memoryChange{Action: "settings", Settings: &memorySettings{Suggest: true}})
	recovered := assistantCreationActualRun(t, s, token, assistantCreationAction{Kind: "recover", MessageID: "assistant-one", MessageDigest: digestOf([]byte(assistantAuthoringProposalFixture(json.RawMessage(reviewPack))))}, 1)
	digest := recovered.Creation.CandidateDigest
	for i, role := range []string{"establish-cases", "propose-correction"} {
		assistantCreationRetainDraft(t, s, token)
		action := assistantCreationAction{Kind: role, CandidateDigest: digest}
		if i == 1 {
			action.IssueID = "invalid"
		}
		run := assistantCreationActualRun(t, s, token, action, i+2)
		before := memoryReviewRead(t, s, token)
		var record assistantRunRecord
		for _, candidate := range assistantAuthoringLedger(t, s).Runs {
			if candidate.ID == run.ID {
				record = candidate
			}
		}
		if !before.Content.Settings.Suggest || record.Binding.MemoryProposals == nil || *record.Binding.MemoryProposals || len(before.Proposals) != 0 || len(run.Checkpoint.Memories.Proposed) != 0 || string(run.Checkpoint.Policy["budget.memoryProposalsPerRun"].Value) != "0" || calls.Load() != int32((i+1)*2) {
			t.Fatal("independent reviewer gained canonical proposal authority", role, calls.Load())
		}
		encodedEvents, _ := json.Marshal(run.Events)
		if !strings.Contains(string(encodedEvents), "memoryProposalsPerRun") {
			t.Fatal("canonical proposal refusal missing", role, string(encodedEvents))
		}
		// An older retained reviewer record cannot gain acceptance simply by claiming
		// the author bit; the latest saved human action independently excludes it.
		author := true
		record.Binding.MemoryProposals = &author
		r := httptest.NewRequest("POST", "/api/ai-memory", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		assistantRunMust(t, s.withMemory(func(root *os.Root, reply *memoryReply) error {
			if _, _, _, _, err := s.memoryReviewSource(r, root, record); err == nil {
				t.Fatal("reviewer source accepted as memory author", role)
			}
			return nil
		}))
		change := memoryReviewChange{ID: randomStartupID(), RunID: run.ID, RunRevision: run.Revision, ProposalID: "remember-review", ProposalDigest: "sha256:" + strings.Repeat("a", 64), Decision: "accept"}
		if w := policyWorkspaceCall(s, token, "POST", "/api/ai-memory", before.SHA256, memoryChange{Action: "review", Review: &change}); w.Code == 200 {
			t.Fatal("reviewer acceptance succeeded", role)
		}
		if memoryReviewRead(t, s, token).SHA256 != before.SHA256 {
			t.Fatal("reviewer acceptance refusal wrote private memory", role)
		}
	}
}

func TestAssistantMemoryReviewHostProvenance(t *testing.T) {
	s, token, view := memoryProposalFixture(t, false, "work", true)
	var record assistantRunRecord
	for _, run := range assistantAuthoringLedger(t, s).Runs {
		if run.ID == view.ID {
			record = run
		}
	}
	r := httptest.NewRequest("POST", "/api/ai-memory", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	before := memoryReviewRead(t, s, token)
	assistantRunMust(t, s.withMemory(func(root *os.Root, reply *memoryReply) error {
		for _, owner := range []string{"brief", "author-pack", "author-facts", "author-function", "background"} {
			other := record
			other.Binding.Owner.Kind = owner
			if _, _, _, _, err := s.memoryReviewSource(r, root, other); err == nil {
				t.Fatal("non-chat purpose gained human-turn attribution", owner)
			}
		}
		writeProjectFile(t, s.projectDir, "reviewed.pack.json", reviewPack)
		record.SavedPack = &assistantSavedPackBasis{Basis: assistantPackBasis{Path: "reviewed.pack.json"}, Read: assistantTestRead{Path: "reviewed.pack.json", Pack: json.RawMessage(reviewPack), PackDigest: digestOf([]byte(reviewPack))}}
		source, _, _, deps, err := s.memoryReviewSource(r, root, record)
		assistantRunMust(t, err)
		if source.ChatID != view.Owner.ID || source.TurnID != view.RequestID || len(deps) != 1 || deps[0].Path != "reviewed.pack.json" {
			t.Fatal("host source/dependency not retained", source, deps)
		}
		writeProjectFile(t, s.projectDir, "reviewed.pack.json", reviewPack+" ")
		if _, _, _, _, err := s.memoryReviewSource(r, root, record); err == nil {
			t.Fatal("changed dependency accepted")
		}
		return nil
	}))
	if memoryReviewRead(t, s, token).SHA256 != before.SHA256 {
		t.Fatal("host provenance reads wrote memory")
	}
}
