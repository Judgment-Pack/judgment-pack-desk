package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func portableResearchFixture(t *testing.T, handler http.HandlerFunc) (*Server, *httptest.Server, inputResearchRun) {
	t.Helper()
	bundle, runner, runtime := os.Getenv("JPS_TEST_GATEWAY_BUNDLE"), os.Getenv("JPACK_RUNNER_TEST_BIN"), os.Getenv("JPACK_BIN")
	if bundle == "" || runner == "" || runtime == "" {
		t.Skip("requires pinned Gateway, Runner and Runtime")
	}
	provider := httptest.NewServer(handler)
	t.Cleanup(provider.Close)
	s, ts := startDesk(t, Config{LocalAccess: true, ProjectDir: t.TempDir(), DeskConfigDir: t.TempDir(), Token: testToken, LocalGatewayBundle: bundle, RunnerBin: runner, JpackBin: runtime})
	s.inputResearchCancel()
	<-s.inputResearchDone
	t.Cleanup(func() { ts.Close(); s.Close() })
	registry := saveTestAIRegistry(t, s, ts, aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{aiTestConnection(testAIAlpha, "Portable", provider.URL)}})
	c := registry.Connections[0]
	code, raw := aiTestRequest(t, ts, "PUT", "/api/assistant/key", map[string]string{"key": "portable-fixture-key"}, map[string]string{aiConnectionHeader: c.ID, aiRevisionHeader: c.Revision})
	if code != 200 {
		t.Fatalf("key %d %s", code, raw)
	}
	choice := backgroundModelChoice{Connection: c.ID, Revision: c.Revision, Model: "shared-model"}
	raw, err := s.registerModelSourceProfile(t.Context(), choice)
	if err != nil {
		t.Fatal(err)
	}
	var entry portableResearchProfile
	if json.Unmarshal(raw, &entry) != nil {
		t.Fatal("bad profile")
	}
	var profile portableProfileDetails
	if json.Unmarshal(entry.Profile, &profile) != nil {
		t.Fatal("bad profile details")
	}
	mapping := map[string]any{"version": 3, "case": map[string]any{"parameters": map[string]any{"subject": map[string]string{"pointer": "/subject", "type": "string"}}, "facts": []any{}, "evidence": []any{}}, "admits": map[string]any{"facts": map[string]any{"/eligible": []string{"generated"}}}, "sources": []any{map[string]any{"name": "research", "kind": "research", "profile": profile.ID, "profileDigest": entry.Digest, "maxAge": 3600, "question": map[string]string{"$param": "subject"}, "constraints": map[string]any{}, "model": choice.Model, "capabilities": []string{"answer"}, "webProfiles": map[string]any{}, "evidence": []string{}, "fields": []any{map[string]any{"name": "eligible", "description": "Whether this record is eligible.", "type": "boolean", "required": true}}, "read": map[string]any{"copy": map[string]any{"facts": []any{map[string]string{"source": "/fields/eligible/value", "target": "/eligible"}}, "evidence": []any{}}}}}}
	input, _ := json.Marshal(map[string]any{"source": map[string]any{"mapping": mapping, "case": map[string]any{"subject": "Generic procurement record"}, "sources": map[string]any{}}})
	run, err := s.reviewPortableResearch(t.Context(), input, choice, inputResearchLimits{Records: 1, ModelCalls: 3}, "", "fixture-scope")
	if err != nil {
		t.Fatal(err)
	}
	var request portableResearchRequest
	if json.Unmarshal(run.Portable.Frozen.Plan.Arguments, &request) != nil || request.Question != "Generic procurement record" {
		t.Fatal("Runner-resolved question was not retained")
	}
	return s, ts, run
}
func portableResearchStoreFixture(t *testing.T, run inputResearchRun, memory bool) researchRunStore {
	t.Helper()
	var journal researchJournal
	if memory {
		journal = newMemoryResearchJournals().Run(run.ID)
	} else {
		root, err := os.OpenRoot(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { root.Close() })
		journal = fileResearchJournal{root: root, id: run.ID}
	}
	if err := journal.Create(t.Context(), run); err != nil {
		t.Fatal(err)
	}
	return researchRunStore{journal: journal}
}
func portableTestReply(w http.ResponseWriter, action string) {
	_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": action}}}})
}
func TestPortableResearchUsesSharedJournalAndRealReceipts(t *testing.T) {
	for _, memory := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "memory"}[memory], func(t *testing.T) {
			var calls atomic.Int32
			var store researchRunStore
			s, _, run := portableResearchFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				held, err := store.read()
				if err != nil || len(held.Attempts) != 1 || held.Attempts[0].State != "started" {
					t.Error("dispatch preceded durable admission", err)
				}
				portableTestReply(w, `{"kind":"answer","fields":{"eligible":{"status":"found","value":false,"cites":[0]}}}`)
			})
			if calls.Load() != 0 {
				t.Fatal("review dispatched a model")
			}
			store = portableResearchStoreFixture(t, run, memory)
			if _, err := store.change(func(r *inputResearchRun) error { r.State = "running"; return nil }); err != nil {
				t.Fatal(err)
			}
			s.executePortableResearch(t.Context(), store, run)
			held, err := store.read()
			if err != nil {
				t.Fatal(err)
			}
			if held.State != "completed" || calls.Load() != 1 || len(held.Attempts) != 1 || !bytes.Contains(held.Portable.Response, []byte(`"value":false`)) {
				t.Fatalf("not completed: state=%s problem=%s attempts=%+v calls=%d", held.State, held.Problem, held.Attempts, calls.Load())
			}
			input, err := portableResearchInputWithResponse(held, held.Portable.Response)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := s.inputRunner(t.Context(), "inputs/preview", input, "")
			if err != nil || !bytes.Contains(prepared, []byte(`"generatedInfluence":true`)) {
				t.Fatalf("retained replay %s %v", prepared, err)
			}
			for _, change := range []func(*inputResearchRun){func(r *inputResearchRun) {
				r.Portable.Frozen.Choice.Model = "changed"
				r.Revision = portableResearchRevision(r.Portable.Frozen)
			}, func(r *inputResearchRun) { r.Portable.Response = json.RawMessage(`{}`) }, func(r *inputResearchRun) { r.Attempts[0].Result = json.RawMessage(`{}`) }} {
				if _, err := store.change(func(r *inputResearchRun) error { change(r); return nil }); err == nil {
					t.Fatal("retained contract/response was replaceable")
				}
			}
			if calls.Load() != 1 {
				t.Fatal("offline check invoked a model")
			}
		})
	}
}
func TestPortableResearchCorrectsWithinOriginalAllowance(t *testing.T) {
	var calls atomic.Int32
	s, _, run := portableResearchFixture(t, func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			portableTestReply(w, `{"kind":"answer","fields":{"eligible":{"status":"found","value":"false","cites":[0]}}}`)
		} else {
			portableTestReply(w, `{"kind":"answer","fields":{"eligible":{"status":"found","value":false,"cites":[1]}}}`)
		}
	})
	store := portableResearchStoreFixture(t, run, false)
	_, err := store.change(func(r *inputResearchRun) error { r.State = "running"; return nil })
	if err != nil {
		t.Fatal(err)
	}
	s.executePortableResearch(t.Context(), store, run)
	held, err := store.read()
	if err != nil {
		t.Fatal(err)
	}
	if held.State != "completed" || calls.Load() != 2 || held.Attempts[0].ActionProblem == "" || len(held.Attempts) != 2 {
		t.Fatalf("wrong correction state %s problem %s calls %d", held.State, held.Problem, calls.Load())
	}
	responses, err := portableResearchResponses(held)
	if err != nil || len(responses) != 2 {
		t.Fatal("correction erased earlier acquisition", err)
	}
}
func TestPortableResearchUncertainOperationNeverReplays(t *testing.T) {
	s, _, run := portableResearchFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected model dispatch") })
	_ = s
	store := portableResearchStoreFixture(t, run, true)
	_, err := store.change(func(r *inputResearchRun) error { r.State = "running"; return nil })
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	operation := func(context.Context, inputResearchRun, inputResearchAttempt) (json.RawMessage, error) {
		calls.Add(1)
		return nil, errors.New("test interruption")
	}
	for i := 0; i < 2; i++ {
		if _, err := researchAttempt(t.Context(), store, "model", json.RawMessage(`{"request":"one"}`), operation); err == nil {
			t.Fatal("uncertain operation accepted")
		}
	}
	if calls.Load() != 1 {
		t.Fatal("uncertain operation repeated")
	}
	held, err := store.change(func(r *inputResearchRun) error { recoverInputResearch(r); return nil })
	if err != nil || held.State != "interrupted" || held.Attempts[0].State != "uncertain" {
		t.Fatal("recovery lost uncertainty", err)
	}
}
func TestPortableResearchReviewRejectsChangedModelAndUnadmittedFacts(t *testing.T) {
	s, _, run := portableResearchFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("review invoked a model") })
	frozen := run.Portable.Frozen
	for _, input := range []json.RawMessage{json.RawMessage(strings.Replace(string(frozen.Input), `"generated"`, `"asserted"`, 1)), json.RawMessage(strings.Replace(string(frozen.Input), `"shared-model"`, `"different-model"`, 1))} {
		if _, err := s.reviewPortableResearch(t.Context(), input, frozen.Choice, frozen.Limits, "", frozen.SessionScope); err == nil {
			t.Fatal("changed contract admitted")
		}
	}
}

func TestPortableResearchFinalVerificationCannotEraseStop(t *testing.T) {
	_, _, run := portableResearchFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected model dispatch") })
	for _, stop := range []bool{false, true} {
		t.Run(map[bool]string{false: "context cancelled", true: "stop saved"}[stop], func(t *testing.T) {
			store := portableResearchStoreFixture(t, run, true)
			_, err := store.change(func(r *inputResearchRun) error { r.State = "running"; r.StopRequested = stop; return nil })
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if !stop {
				cancel()
			}
			response := json.RawMessage(`{"result":{"fields":{},"responses":[]}}`)
			if err := savePortableResearchResult(ctx, store, response); err != nil {
				t.Fatal(err)
			}
			held, err := store.read()
			want := "interrupted"
			if stop {
				want = "stopped"
			}
			if err != nil || held.State != want || !bytes.Equal(response, held.Portable.Response) {
				t.Fatal("verified result lost stop or completion", held.State, err)
			}
		})
	}
}
func TestPortableResearchRetainedSizeStopsBeforeMoreCompute(t *testing.T) {
	_, _, run := portableResearchFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected model dispatch") })
	response := portableRetainedResponse{Request: json.RawMessage(`{}`), Result: json.RawMessage(`"` + strings.Repeat("x", 1100<<10) + `"`), Receipt: json.RawMessage(`{}`), Salts: json.RawMessage(`{}`)}
	response.Profile.ID = "retained"
	response.Profile.Digest = "sha256:" + strings.Repeat("a", 64)
	raw, _ := portableResearchJSON(response)
	run.Attempts = []inputResearchAttempt{{ID: "attempt_retained", Kind: "page", State: "completed", Result: raw}}
	if !errors.Is(portableResearchCapacity(run), errPortableResearchSize) {
		t.Fatal("oversized retained source invited more model spending")
	}
}
func TestPortableResearchRefusesMixedDependencySessionsBeforeAcquisition(t *testing.T) {
	for _, test := range []struct {
		name, first, second string
		refuse              bool
	}{
		{"shared", `{"sessionId":"shared","callIndex":0}`, `{"sessionId":"shared","callIndex":1}`, true},
		{"singleton", `{"sessionId":"first","callIndex":0}`, `{"sessionId":"second","callIndex":0}`, false},
		{"duplicate", `{"sessionId":"shared","callIndex":0}`, `{"sessionId":"shared","callIndex":0}`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := json.RawMessage(`{"source":{"mapping":{"sources":[{"name":"a","kind":"operation"},{"name":"b","kind":"research"}]},"sources":{"a":{"response":{"result":1,"receipt":` + test.first + `}},"b":{"response":{"result":{"fields":{},"responses":[{"receipt":` + test.second + `}]}}}}}}`)
			if err := validatePortableSessionExtension(raw); (err != nil) != test.refuse {
				t.Fatal("wrong session compatibility", err)
			}
		})
	}
}
func TestPortableResearchHTTPAndSharedChatApproval(t *testing.T) {
	var calls atomic.Int32
	s, ts, basis := portableResearchFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		portableTestReply(w, `{"kind":"answer","fields":{"eligible":{"status":"found","value":false,"cites":[0]}}}`)
	})
	chat := &actionFixture{s: s, ts: ts, chat: map[string]any{"id": "portable-chat", "interaction": "plan"}, cases: []any{}}
	chat.save(t)
	frozen := basis.Portable.Frozen
	body := map[string]any{"input": frozen.Input, "choice": frozen.Choice, "limits": frozen.Limits, "chatId": "portable-chat"}
	code, raw := aiTestRequest(t, ts, "POST", "/api/portable-research", body, nil)
	if code != 201 {
		t.Fatalf("review %d %s", code, raw)
	}
	var review struct{ ID, Revision, State string }
	if json.Unmarshal(raw, &review) != nil {
		t.Fatal("invalid review")
	}
	path := "/api/portable-research/" + review.ID
	code, _ = aiTestRequest(t, ts, "POST", path+"/confirm", map[string]any{}, map[string]string{"If-Match": review.Revision})
	if code != 409 {
		t.Fatal("Plan dispatched", code)
	}
	code, _ = aiTestRequest(t, ts, "POST", "/api/portable-research/profiles", map[string]any{"choice": frozen.Choice, "reviewed": true, "chatId": "portable-chat"}, nil)
	if code != 409 {
		t.Fatal("Plan changed bindings", code)
	}
	if calls.Load() != 0 {
		t.Fatal("review or Plan spent compute")
	}
	chat.chat["interaction"] = "work"
	chat.save(t)
	// Auto must use the same scoped framework as other chat actions.
	grantBody := map[string]any{"action": "portable-research", "target": review.ID, "revision": review.Revision}
	code, raw = aiTestRequest(t, ts, "POST", "/api/assistant-approvals/portable-chat/grant", grantBody, nil)
	if code != 200 {
		t.Fatalf("grant %d %s", code, raw)
	}
	var granted struct {
		Grant assistantActionGrant `json:"grant"`
	}
	if json.Unmarshal(raw, &granted) != nil {
		t.Fatal("bad grant")
	}
	commit := map[string]any{"action": "portable-research", "target": review.ID, "revision": review.Revision, "id": granted.Grant.ID, "content": "{}"}
	code, raw = aiTestRequest(t, ts, "POST", "/api/assistant-approvals/portable-chat/commit", commit, nil)
	if code != 200 {
		t.Fatalf("commit %d %s", code, raw)
	}
	code, _ = aiTestRequest(t, ts, "POST", "/api/assistant-approvals/portable-chat/commit", commit, nil)
	if code != 409 {
		t.Fatal("grant replay accepted", code)
	}
	if calls.Load() != 0 {
		t.Fatal("queueing directly dispatched instead of durable worker")
	}
	s.scanInputResearch(t.Context())
	code, raw = aiTestRequest(t, ts, "GET", path, nil, nil)
	if code != 200 {
		t.Fatal(code, string(raw))
	}
	var finished struct{ State, ResponseText string }
	_ = json.Unmarshal(raw, &finished)
	if finished.State != "completed" || finished.ResponseText == "" || calls.Load() != 1 {
		t.Fatalf("worker result %d %s calls=%d", code, raw, calls.Load())
	}
	s.scanInputResearch(t.Context())
	if calls.Load() != 1 {
		t.Fatal("completed run replayed")
	}
}

func portableResearchPageFixture(t *testing.T, s *Server, basis inputResearchRun, chat string) inputResearchRun {
	t.Helper()
	var profile map[string]any
	if err := json.Unmarshal(basis.Portable.Frozen.Profiles[0].Profile, &profile); err != nil {
		t.Fatal(err)
	}
	profile["id"] = "portable-page-fixture"
	profile["source"] = "page-fixture"
	profile["endpoint"] = "fixture:page"
	profile["tools"] = []string{"page"}
	profile["research"] = map[string]any{"capabilities": []string{"page"}}
	raw, _ := json.Marshal(profile)
	registered, err := s.inputRunner(t.Context(), "input-profiles", json.RawMessage(raw), "")
	if err != nil {
		t.Fatal(err)
	}
	var entry portableResearchProfile
	if err = json.Unmarshal(registered, &entry); err != nil {
		t.Fatal(err)
	}
	var input map[string]any
	_ = json.Unmarshal(basis.Portable.Frozen.Input, &input)
	source := input["source"].(map[string]any)["mapping"].(map[string]any)["sources"].([]any)[0].(map[string]any)
	source["capabilities"] = []string{"answer", "page"}
	source["webProfiles"] = map[string]any{"page": map[string]any{"profile": profile["id"], "profileDigest": entry.Digest}}
	raw, _ = json.Marshal(input)
	f := basis.Portable.Frozen
	run, err := s.reviewPortableResearch(t.Context(), raw, f.Choice, f.Limits, chat, f.SessionScope)
	if err != nil {
		t.Fatal(err)
	}
	return run
}
func TestPortableResearchExplicitBudgetPauseAndResume(t *testing.T) {
	for _, sourceLimit := range []bool{false, true} {
		t.Run(map[bool]string{false: "model limit", true: "page limit"}[sourceLimit], func(t *testing.T) {
			var calls atomic.Int32
			s, _, run := portableResearchFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					if sourceLimit {
						portableTestReply(w, `{"kind":"page","arguments":{"tool":"page"}}`)
					} else {
						portableTestReply(w, `{"kind":"answer","fields":{"eligible":{"status":"found","value":"false","cites":[0]}}}`)
					}
				} else {
					portableTestReply(w, `{"kind":"answer","fields":{"eligible":{"status":"found","value":false,"cites":[1]}}}`)
				}
			})
			if sourceLimit {
				run = portableResearchPageFixture(t, s, run, "")
			} else {
				run.Portable.Frozen.Limits.ModelCalls = 1
				run.Revision = portableResearchRevision(run.Portable.Frozen)
			}
			revision := run.Revision
			store := portableResearchStoreFixture(t, run, false)
			if _, err := store.change(func(r *inputResearchRun) error { r.State = "running"; return nil }); err != nil {
				t.Fatal(err)
			}
			s.executePortableResearch(t.Context(), store, run)
			paused, err := store.read()
			if err != nil {
				t.Fatal(err)
			}
			pending := pendingResearchBudget(paused)
			if paused.State != "paused" || pending == nil || calls.Load() != 1 || len(paused.Attempts) != 1 {
				t.Fatalf("wrong pause: state=%s problem=%s requests=%d attempts=%d", paused.State, paused.Problem, calls.Load(), len(paused.Attempts))
			}
			if sourceLimit && (pending.Used.Pages != 0 || pending.Proposed.Pages != 3) {
				t.Fatal("source dispatched without allowance")
			}
			if _, err = store.change(func(r *inputResearchRun) error {
				return decideResearchBudget(r, pending.ID, "approve", inputReviewActor{Kind: "local-access"})
			}); err != nil {
				t.Fatal(err)
			}
			resumed, err := store.change(func(r *inputResearchRun) error { r.State = "running"; return nil })
			if err != nil {
				t.Fatal(err)
			}
			s.executePortableResearch(t.Context(), store, resumed)
			held, err := store.read()
			if err != nil {
				t.Fatal(err)
			}
			if held.State != "completed" || calls.Load() != 2 || len(held.Attempts) != 2 || held.Revision != revision || held.BudgetRequests[0].State != "approved" {
				t.Fatalf("wrong resume state=%s problem=%s calls=%d", held.State, held.Problem, calls.Load())
			}
		})
	}
}
func TestPortableResearchSwitchToPlanBeforeWebDispatch(t *testing.T) {
	var chat *actionFixture
	var calls atomic.Int32
	s, ts, basis := portableResearchFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		chat.chat["interaction"] = "plan"
		chat.save(t)
		portableTestReply(w, `{"kind":"page","arguments":{"tool":"page"}}`)
	})
	chat = &actionFixture{s: s, ts: ts, chat: map[string]any{"id": "portable-plan-switch", "interaction": "work"}, cases: []any{}}
	chat.save(t)
	run := portableResearchPageFixture(t, s, basis, "portable-plan-switch")
	run.Portable.Frozen.Limits.Pages = 1
	run.Revision = portableResearchRevision(run.Portable.Frozen)
	store := portableResearchStoreFixture(t, run, false)
	if _, err := store.change(func(r *inputResearchRun) error { r.State = "running"; return nil }); err != nil {
		t.Fatal(err)
	}
	s.executePortableResearch(t.Context(), store, run)
	held, err := store.read()
	if err != nil {
		t.Fatal(err)
	}
	if held.State != "blocked" || len(held.Attempts) != 1 || calls.Load() != 1 || !strings.Contains(held.Problem, "Switch to Work") {
		t.Fatalf("Plan dispatched: state=%s problem=%s attempts=%d", held.State, held.Problem, len(held.Attempts))
	}
}
func TestPortableResearchJournalEscapingRoundTrip(t *testing.T) {
	_, _, basis := portableResearchFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected acquisition") })
	for _, memory := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "memory"}[memory], func(t *testing.T) {
			run := basis
			portable := *basis.Portable
			run.Portable = &portable
			var input map[string]json.RawMessage
			_ = json.Unmarshal(run.Portable.Frozen.Input, &input)
			// Unused case data is legal; its original raw JSON size must survive saving.
			run.Portable.Frozen.Input = json.RawMessage(strings.TrimSuffix(string(run.Portable.Frozen.Input), "}") + `,"padding":"` + strings.Repeat("<&\u2028", 50000) + `"}`)
			run.Revision = portableResearchRevision(run.Portable.Frozen)
			store := portableResearchStoreFixture(t, run, memory)
			held, err := store.read()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(held.Portable.Frozen.Input, run.Portable.Frozen.Input) {
				t.Fatal("frozen input changed during storage")
			}
			_, err = store.change(func(r *inputResearchRun) error { r.State = "running"; return nil })
			if err != nil {
				t.Fatal(err)
			}
			response := json.RawMessage(`{"result":{"fields":{"text":"` + strings.Repeat("<&\u2028", 50000) + `"},"responses":[]}}`)
			if err = savePortableResearchResult(t.Context(), store, response); err != nil {
				t.Fatal(err)
			}
			held, err = store.read()
			if err != nil || !bytes.Equal(held.Portable.Response, response) {
				t.Fatal("saved response unreadable", err)
			}
		})
	}
}

func TestPortableResearchCapacityWaitRechecksAdmission(t *testing.T) {
	s, ts, run := portableResearchFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected acquisition") })
	chat := &actionFixture{s: s, ts: ts, chat: map[string]any{"id": "portable-wait-plan", "interaction": "work"}, cases: []any{}}
	chat.save(t)
	for i := 0; i < cap(s.researchSlots); i++ {
		s.researchSlots <- struct{}{}
	}
	entered := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(entered)
		_, err := s.researchSourceCallWithAdmission(t.Context(), run.Gateway, "acquire", map[string]any{}, func() error { return s.portableResearchChatWork("portable-wait-plan") })
		done <- err
	}()
	<-entered
	chat.chat["interaction"] = "plan"
	chat.save(t)
	<-s.researchSlots
	if err := <-done; err == nil || !strings.Contains(err.Error(), "Switch to Work") {
		t.Fatal("capacity wait bypassed Plan", err)
	}
	for len(s.researchSlots) > 0 {
		<-s.researchSlots
	}
}
func TestPortableResearchCanonicalRequestReusesCompletedOperation(t *testing.T) {
	s, _, run := portableResearchFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected acquisition") })
	run.Portable.Frozen.Limits.Pages = 1
	run.Revision = portableResearchRevision(run.Portable.Frozen)
	store := portableResearchStoreFixture(t, run, true)
	if _, err := store.change(func(r *inputResearchRun) error { r.State = "running"; return nil }); err != nil {
		t.Fatal(err)
	}
	verifier, err := s.researchVerifier()
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	for _, args := range []string{`{"tool":"page","url":"https://example.test","n":-0}`, `{ "n":0, "url":"https://example.test", "tool":"page" }`} {
		identity, err := researchCanon(t.Context(), verifier, []byte(args))
		if err != nil {
			t.Fatal(err)
		}
		_, err = researchAttempt(t.Context(), store, "page", identity, func(context.Context, inputResearchRun, inputResearchAttempt) (json.RawMessage, error) {
			calls++
			return json.RawMessage(`{"retained":true}`), nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatal("equivalent requests acquired twice", calls)
	}
}

func TestPortableResearchRepeatedCompletedSourceStopsCorrection(t *testing.T) {
	var calls atomic.Int32
	s, _, basis := portableResearchFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		portableTestReply(w, `{"kind":"page","arguments":{ "tool" : "page" }}`)
	})
	run := portableResearchPageFixture(t, s, basis, "")
	run.Portable.Frozen.Limits.Pages = 1
	run.Revision = portableResearchRevision(run.Portable.Frozen)
	retained := portableRetainedResponse{Request: json.RawMessage(`{"tool":"page"}`), Result: json.RawMessage(`{"text":"retained"}`), Receipt: json.RawMessage(`{}`), Salts: json.RawMessage(`{}`)}
	retained.Profile.ID = "portable-page-fixture"
	retained.Profile.Digest = run.Portable.Frozen.Profiles[1].Digest
	raw, _ := portableResearchJSON(retained)
	identity := json.RawMessage(`{"tool":"page"}`)
	store := portableResearchStoreFixture(t, run, true)
	if _, err := store.change(func(r *inputResearchRun) error {
		r.State = "running"
		attempt, _, err := beginResearchAttempt(r, "page", identity)
		if err != nil {
			return err
		}
		return finishResearchAttempt(r, attempt.ID, raw, "")
	}); err != nil {
		t.Fatal(err)
	}
	s.executePortableResearch(t.Context(), store, run)
	held, err := store.read()
	if err != nil {
		t.Fatal(err)
	}
	if held.State != "partial" || calls.Load() != 3 || len(held.Attempts) != 4 || !strings.Contains(held.Problem, "already completed") {
		t.Fatalf("repeat did not stop: %s %s calls=%d attempts=%d", held.State, held.Problem, calls.Load(), len(held.Attempts))
	}
}
