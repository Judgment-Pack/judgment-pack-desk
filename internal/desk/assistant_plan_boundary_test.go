package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

func TestAssistantPlanAcceptanceCurrentState(t *testing.T) {
	for _, mode := range []string{"current", "file", "dialect", "missing", "unavailable", "linked", "questions", "forged", "new-accepted", "prior-edit"} {
		t.Run(mode, func(t *testing.T) {
			s, token, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "plan")
			dialect := assistantPlanSeed(t, s)
			if result := assistantPlanInvoke(t, e, "proposal", true, assistantPlanCandidate); result.Err() {
				t.Fatal(result.Value().Text())
			}
			assistantPlanComplete(t, e)
			assistantRunMust(t, e.ProjectChat(t.Context()))
			saved := policyWorkspaceRead(t, s, token, "/api/conversations")
			var doc conversationDocument
			assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
			chat, err := assistantTestObject(doc.Chats[0])
			assistantRunMust(t, err)
			plans, err := assistantPlanSource(doc.Chats[0])
			assistantRunMust(t, err)
			switch mode {
			case "file":
				assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "one.json"), []byte("changed"), 0600))
			case "dialect":
				dialect.Document.Name = "Changed"
				_, err = s.preparationRepository().CompareAndSwap(t.Context(), dialect.Document, dialect.SHA256)
				assistantRunMust(t, err)
			case "missing":
				assistantRunMust(t, os.Remove(filepath.Join(s.cfg.ProjectDir, "one.json")))
			case "unavailable":
				assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, inputPreparationDir, dialect.Document.ID+".json"), []byte(`{"broken":`), 0600))
			case "linked":
				chat["pack"] = assistantChatJSON(map[string]string{"id": "other", "path": "other.json", "digest": strings.Repeat("a", 64)})
			case "questions":
				plans[0].Questions = []string{"Unresolved review"}
			case "forged":
				plans[0].References[0].Revision = strings.Repeat("f", 64)
			case "new-accepted":
				plans[0].ID = "model-accepted"
			case "prior-edit":
				plans[0].Objective = "Changed after review"
			}
			plans[0].Status = "accepted"
			plans[0].AcceptedAt = "2026-10-21T12:00:00Z"
			chat["plans"] = assistantChatJSON(plans)
			chat["interaction"] = assistantChatJSON("work")
			doc.Chats[0] = assistantChatEncodeObject(chat)
			w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, doc)
			status, code := 409, CodeStale
			switch mode {
			case "current":
				status, code = 200, ""
			case "missing":
				status, code = 404, CodeNotFound
			case "unavailable":
				status, code = 500, CodeInternal
			}
			if w.Code != status {
				t.Fatal("Plan acceptance classification differs", mode, w.Code, w.Body.String())
			}
			if mode != "current" {
				var failure struct{ Code, Error string }
				assistantRunMust(t, json.Unmarshal(w.Body.Bytes(), &failure))
				guidance := strings.Contains(failure.Error, "Review the current Plan references again before starting work.")
				if failure.Code != code || guidance != (status == 409) {
					t.Fatal("Plan acceptance code/guidance differs", mode, w.Body.String())
				}
				after := policyWorkspaceRead(t, s, token, "/api/conversations")
				if after.SHA256 != saved.SHA256 || !bytes.Equal(after.Content, saved.Content) {
					t.Fatal("refused Plan acceptance changed saved conversation", mode)
				}
				if mode == "file" || mode == "unavailable" {
					next, err := json.Marshal(doc)
					assistantRunMust(t, err)
					view, err := s.holdAssistantPlanAcceptance(httptest.NewRequest("PUT", "/api/conversations", nil), saved.Content, next)
					if view != nil {
						view.Close()
					}
					if errors.Is(err, errAssistantRunConflict) != (mode == "file") || codeOf(err) != code {
						t.Fatal("Plan acceptance lost typed conflict or storage error identity", err)
					}
				}
			}
			if mode == "current" {
				assistantRunMust(t, s.checkChatExecution("chat-one"))
				assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "one.json"), []byte("changed after acceptance"), 0600))
				if s.checkChatExecution("chat-one") == nil {
					t.Fatal("stale accepted Plan authorized work")
				}
			}
		})
	}
}

func TestAssistantPlanProjectionCurrentState(t *testing.T) {
	for _, mode := range []string{"prepare-file", "write-file", "write-dialect", "chat-cas", "prepare-missing"} {
		t.Run(mode, func(t *testing.T) {
			s, token, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "plan")
			dialect := assistantPlanSeed(t, s)
			if result := assistantPlanInvoke(t, e, "proposal", true, assistantPlanCandidate); result.Err() {
				t.Fatal(result.Value().Text())
			}
			assistantPlanComplete(t, e)
			if strings.HasPrefix(mode, "write-") {
				assistantRunMust(t, s.prepareAssistantChatProjection(e.request.Clone(t.Context()), e.record.ID))
			}
			switch mode {
			case "write-dialect":
				dialect.Document.Name = "Changed"
				_, err := s.preparationRepository().CompareAndSwap(t.Context(), dialect.Document, dialect.SHA256)
				assistantRunMust(t, err)
			case "chat-cas":
				saved := policyWorkspaceRead(t, s, token, "/api/conversations")
				changed := bytes.ReplaceAll(saved.Content, []byte("Review exact input"), []byte("Changed exact input"))
				var doc conversationDocument
				assistantRunMust(t, json.Unmarshal(changed, &doc))
				if w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, doc); w.Code != 200 {
					t.Fatal(w.Body.String())
				}
			case "prepare-missing":
				assistantRunMust(t, os.Remove(filepath.Join(s.cfg.ProjectDir, "one.json")))
			default:
				assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "one.json"), []byte("changed"), 0600))
			}
			if e.ProjectChat(t.Context()) == nil {
				t.Fatal("stale Plan was projected", mode)
			}
			saved := policyWorkspaceRead(t, s, token, "/api/conversations")
			if bytes.Contains(saved.Content, []byte(`"plans"`)) {
				t.Fatal("failed projection partially appended Plan")
			}
		})
	}
}

func TestAssistantPlanCancelledObservedAndUnknown(t *testing.T) {
	t.Run("cancelled-observed", func(t *testing.T) {
		release := make(chan struct{})
		s, _, e := assistantExecutionFixtureExecute(t, "http://127.0.0.1:1", "plan", func(ctx context.Context, _ *assistantRunExecution) error {
			<-ctx.Done()
			<-release
			return ctx.Err()
		})
		// Keep cancellation live, but let the test finish its synthetic checkpoint
		// and projection before the service finalizer closes the executor lease.
		t.Cleanup(func() { close(release) })
		assistantPlanSeed(t, s)
		testHookAssistantPlanObserved = e.cancel
		defer func() { testHookAssistantPlanObserved = nil }()
		result := assistantPlanInvoke(t, e, "cancelled-plan", true, assistantPlanCandidate)
		if !errors.Is(e.ctx.Err(), context.Canceled) {
			t.Fatal("observed Plan did not cancel the execution")
		}
		if result.Err() {
			t.Fatal(result.Value().Text())
		}
		r := assistantExecutionRecord(t, e)
		if len(r.DomainOutputs) != 1 {
			t.Fatal("observed Plan lost on cancellation")
		}
		assistantRunMust(t, assistantRecipeImport(t, e, r))
		assistantPlanComplete(t, e)
		assistantRunMust(t, e.ProjectChat(t.Context()))
		assistantRunMust(t, e.ProjectChat(t.Context()))
	})
	t.Run("uncertain-intent", func(t *testing.T) {
		s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "plan")
		assistantPlanSeed(t, s)
		call := "lost-ack"
		args := json.RawMessage(assistantPlanCandidate)
		assistantRunMust(t, e.writer.boundEvent(t.Context(), contract.RunEvent{ToolCall: &contract.ToolCall{Type: "tool_call", CallID: &call, Name: assistantPlanTool, Args: args}}))
		in := assistantHostIntent{CallID: call, Root: true, Arguments: args}
		id := assistantPlanOperationID(in)
		assistantRunMust(t, e.writer.change(t.Context(), false, func(r *assistantRunRecord) (bool, error) {
			if err := prepareAssistantPlanIntent(*r, e.source.Original, &in); err != nil {
				return false, err
			}
			raw, _ := json.Marshal(in)
			return appendAssistantRunIntent(r, assistantRunOperation{ID: id, CallID: id, Kind: "host", Name: assistantPlanTool, Intent: raw})
		}))
		assistantRunMust(t, e.Dispatch(t.Context(), id))
		// An interrupted durable dispatch has no trustworthy observation. Import
		// carries its deterministic intent; reconstructed registry cannot execute it.
		r := assistantExecutionRecord(t, e)
		assistantRunMust(t, assistantRecipeImport(t, e, r))
		ctx := context.WithValue(runcontext.WithCallID(t.Context(), call), assistantHostRootKey{}, true)
		result, err := e.assistantPlanTools()[0].Execute(ctx, args)
		assistantRunMust(t, err)
		if !result.Err() || len(assistantExecutionRecord(t, e).DomainOutputs) != 0 {
			t.Fatal("unknown Plan operation replayed")
		}
	})
}

func TestAssistantPlanQueuedAuthority(t *testing.T) {
	for _, mode := range []string{"phase", "source", "session", "profile", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			s, token, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "plan")
			assistantPlanSeed(t, s)
			s.writes.Lock()
			held := true
			defer func() {
				if held {
					s.writes.Unlock()
				}
			}()
			done := make(chan runcontext.ToolResult, 1)
			go func() { done <- assistantPlanInvoke(t, e, "queued-plan", true, assistantPlanCandidate) }()
			deadline := time.Now().Add(3 * time.Second)
			for {
				r := assistantExecutionRecord(t, e)
				if len(r.Operations) > 0 && r.Operations[len(r.Operations)-1].State == "dispatched" {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("Plan did not wait behind writes")
				}
				time.Sleep(time.Millisecond)
			}
			switch mode {
			case "phase", "source":
				data, err := s.openChatData()
				assistantRunMust(t, err)
				raw, err := readPrivateData(data.root, s.conversationName(), maxConversationBytes)
				assistantRunMust(t, err)
				if mode == "phase" {
					raw = bytes.Replace(raw, []byte(`"plan"`), []byte(`"work"`), 1)
				} else {
					raw = bytes.ReplaceAll(raw, []byte("Review exact input"), []byte("Changed input"))
				}
				assistantRunMust(t, writePrivateData(data.root, s.conversationName(), raw))
				data.root.Close()
			case "session":
				s.sessions.revoke(token)
			case "profile":
				assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, assistantProfilePath), []byte(`{"profileVersion":1,"api":{"inherit":true},"projectInstructions":"changed"}`), 0600))
			case "cancel":
				e.cancel()
			}
			s.writes.Unlock()
			held = false
			select {
			case result := <-done:
				if !result.Err() {
					t.Fatal("queued stale authority proposed Plan", mode)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("queued Plan failed to drain")
			}
			if len(assistantExecutionRecord(t, e).DomainOutputs) != 0 {
				t.Fatal("queued authority loss retained Plan")
			}
		})
	}
}

func TestAssistantPlanRevisionLimit(t *testing.T) {
	s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "plan")
	assistantPlanSeed(t, s)
	for i := 0; i < 24; i++ {
		if result := assistantPlanInvoke(t, e, fmtPlanCall(i), true, assistantPlanCandidate); result.Err() {
			t.Fatal("bounded revision refused", i, result.Value().Text())
		}
	}
	if result := assistantPlanInvoke(t, e, "overflow", true, assistantPlanCandidate); !result.Err() {
		t.Fatal("25th Plan revision accepted")
	}
	assistantPlanComplete(t, e)
	assistantRunMust(t, e.ProjectChat(t.Context()))
}
func fmtPlanCall(i int) string { return "plan-revision-" + string(rune('a'+i)) }

func TestAssistantPlanActualRefusalEvents(t *testing.T) {
	for _, vector := range []struct{ name, args string }{
		{"empty", `{"objective":"","steps":[],"questions":[],"references":[]}`},
		{"missing", `{}`},
		{"oversized", strings.Replace(assistantPlanCandidate, `"Review exact Δ 💡"`, `"`+strings.Repeat("💡", 1001)+`"`, 1)},
		{"unknown-member", strings.Replace(assistantPlanCandidate, `"objective":`, `"accept":true,"objective":`, 1)},
		{"missing-reference", strings.Replace(assistantPlanCandidate, `"one.json"`, `"missing.json"`, 1)},
	} {
		t.Run(vector.name, func(t *testing.T) {
			_, _, e, _ := assistantPlanAPI(t, []string{vector.args})
			r := assistantExecutionRecord(t, e)
			events := []json.RawMessage{}
			refused := false
			for _, event := range r.Events {
				raw, err := assistantRunEventBytes(event.Event)
				assistantRunMust(t, err)
				events = append(events, raw)
				if result := event.Event.ToolResult; result != nil && result.Name == assistantPlanTool {
					refused = result.IsError
				}
			}
			if !refused || len(r.DomainOutputs) != 0 {
				t.Fatal("malformed Plan did not emit refusal", vector.name)
			}
			if dir := os.Getenv("DESK_PLAN_READER_OUTPUTS"); dir != "" {
				assistantRunMust(t, os.WriteFile(filepath.Join(dir, "refusal-"+vector.name+".json"), assistantChatJSON(events), 0600))
			}
		})
	}
}

func TestAssistantPlanAcceptanceHoldsDialectView(t *testing.T) {
	s, token, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "plan")
	assistantPlanSeed(t, s)
	if result := assistantPlanInvoke(t, e, "accepted-view", true, assistantPlanCandidate); result.Err() {
		t.Fatal(result.Value().Text())
	}
	assistantPlanComplete(t, e)
	assistantRunMust(t, e.ProjectChat(t.Context()))
	saved := policyWorkspaceRead(t, s, token, "/api/conversations")
	var doc conversationDocument
	assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
	chat, err := assistantTestObject(doc.Chats[0])
	assistantRunMust(t, err)
	plans, err := assistantPlanSource(doc.Chats[0])
	assistantRunMust(t, err)
	plans[0].Status = "accepted"
	plans[0].AcceptedAt = "2026-10-21T12:00:00Z"
	chat["plans"] = assistantChatJSON(plans)
	chat["interaction"] = assistantChatJSON("work")
	doc.Chats[0] = assistantChatEncodeObject(chat)
	observed := 0
	testHookAssistantPlanAcceptanceCommit = func() {
		observed++
		root, err := s.openInputPreparations(false)
		assistantRunMust(t, err)
		defer root.Close()
		lock, err := root.Open(".")
		assistantRunMust(t, err)
		defer lock.Close()
		if lockPrivateData(lock, true) == nil {
			t.Fatal("Dialect acceptance view released before commit")
		}
	}
	defer func() { testHookAssistantPlanAcceptanceCommit = nil }()
	w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, doc)
	if w.Code != 200 || observed != 1 {
		t.Fatal("acceptance owner did not retain current collection", w.Code, w.Body.String(), observed)
	}
}

// Integration probe: sibling typed output variants must remain exclusive after
// both additive host registries and storage readers are installed together.
func TestAssistantPlanWebsiteVariantIsolation(t *testing.T) {
	s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "plan")
	assistantPlanSeed(t, s)
	if result := assistantPlanInvoke(t, e, "plan-variant", true, assistantPlanCandidate); result.Err() {
		t.Fatal(result.Value().Text())
	}
	planRun := assistantExecutionRecord(t, e)
	f := newAssistantWebsiteFixture(t, "api", "research", "plan", "http://127.0.0.1:1")
	_ = assistantExecutionOpen(t, f.s, f.e)
	if result := f.invokeWebsite(t, "website-variant", true, `{"url":"example.org/report"}`); result.Err() {
		t.Fatal(result.Value().Text())
	}
	websiteRun := assistantExecutionRecord(t, f.e)
	assistantRunMust(t, validateAssistantDomainOutputs(planRun))
	assistantRunMust(t, validateAssistantDomainOutputs(websiteRun))
	mixedPlan := assistantVariantClone(t, planRun)
	mixedPlan.DomainOutputs[0].Website = websiteRun.DomainOutputs[0].Website
	if validateAssistantDomainOutputs(mixedPlan) == nil {
		t.Fatal("Plan accepted Website payload")
	}
	mixedWebsite := assistantVariantClone(t, websiteRun)
	mixedWebsite.DomainOutputs[0].Plan = planRun.DomainOutputs[0].Plan
	if validateAssistantDomainOutputs(mixedWebsite) == nil {
		t.Fatal("Website accepted Plan payload")
	}
}
