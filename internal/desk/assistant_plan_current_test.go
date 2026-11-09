package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func assistantPlanReferenceCall(s *Server, token string, plan chatPlan, handle, revision string) *httptest.ResponseRecorder {
	path := "/api/conversations/chat-one/plans/" + url.PathEscape(plan.ID) + "/references/" + url.PathEscape(handle)
	r := httptest.NewRequest("GET", path, nil)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("If-Match", revision)
	r.Header.Set("X-Plan-Revision", strconv.Itoa(plan.Revision))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func TestAssistantPlanCurrentLookupAndCustody(t *testing.T) {
	for _, mode := range []string{"current", "changed", "deleted", "symlink", "unknown", "revision", "plan-revision", "chat", "plan", "missing-custody", "locator", "handle", "kind", "result", "output", "session", "project", "historical", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			s, token, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "plan")
			assistantPlanSeed(t, s)
			if result := assistantPlanInvoke(t, e, "current-lookup", true, assistantPlanCandidate); result.Err() {
				t.Fatal(result.Value().Text())
			}
			assistantPlanComplete(t, e)
			assistantRunMust(t, e.ProjectChat(t.Context()))
			saved := policyWorkspaceRead(t, s, token, "/api/conversations")
			var doc conversationDocument
			assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
			plans, err := assistantPlanSource(doc.Chats[0])
			assistantRunMust(t, err)
			plan := plans[0]
			if plan.References[0].Kind != "artifact" || plan.References[0].Revision != "" || !strings.HasPrefix(plan.References[0].ID, "plan-ref-") || plan.References[0].ID == "one.json" {
				t.Fatal("writer did not project current opaque references", plan)
			}
			handle, revision := plan.References[0].ID, saved.ChatRevisions["chat-one"]
			store := s.assistantRuns()
			original := assistantRunRaw(t, store)
			switch mode {
			case "changed":
				assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "one.json"), []byte("changed after review"), 0600))
			case "deleted":
				assistantRunMust(t, os.Remove(filepath.Join(s.cfg.ProjectDir, "one.json")))
			case "symlink":
				outside := filepath.Join(t.TempDir(), "outside.json")
				assistantRunMust(t, os.WriteFile(outside, []byte("outside"), 0600))
				assistantRunMust(t, os.Remove(filepath.Join(s.cfg.ProjectDir, "one.json")))
				assistantRunMust(t, os.Symlink(outside, filepath.Join(s.cfg.ProjectDir, "one.json")))
			case "unknown":
				handle = "guessed-handle"
			case "revision":
				revision = strings.Repeat("f", 64)
			case "plan-revision":
				plan.Revision++
			case "plan":
				plan.ID = "different-plan"
			case "missing-custody":
				data, err := s.openChatData()
				assistantRunMust(t, err)
				defer data.root.Close()
				assistantRunMust(t, data.root.Remove(store.name))
			default:
				if mode == "current" {
					break
				}
				var ledger assistantRunLedger
				assistantRunMust(t, json.Unmarshal(original, &ledger))
				run := &ledger.Runs[0]
				op := assistantDialectOperation(*run, run.DomainOutputs[0].OperationID)
				var intent assistantHostIntent
				assistantRunMust(t, readAssistantHostReceipt(op.Intent, &intent))
				switch mode {
				case "chat":
					run.Binding.Owner.ID = "other-chat"
				case "locator":
					intent.Plan.Bindings[0].Locator.ID = "other.json"
				case "handle":
					intent.Plan.Bindings[0].ID = "guessed-handle"
				case "kind":
					intent.Plan.Bindings[0].Kind = "dialect"
				case "result":
					var receipt assistantHostResult
					assistantRunMust(t, readAssistantHostReceipt(op.Result, &receipt))
					receipt.Result = bytes.Replace(receipt.Result, []byte("Plan saved"), []byte("Plan altered"), 1)
					op.Result = assistantChatJSON(receipt)
				case "output":
					run.DomainOutputs[0].Plan.References[0].Digest = strings.Repeat("f", 64)
				case "session":
					run.Binding.Session = strings.Repeat("f", 64)
				case "project":
					run.Binding.Project = strings.Repeat("f", 64)
				case "historical":
					run.Historical = true
				case "duplicate":
					intent.Plan.Bindings = append(intent.Plan.Bindings, intent.Plan.Bindings[0])
				}
				op.Intent = assistantChatJSON(intent)
				data, err := s.openChatData()
				assistantRunMust(t, err)
				defer data.root.Close()
				assistantRunMust(t, writePrivateData(data.root, store.name, assistantChatJSON(ledger)))
			}
			response := assistantPlanReferenceCall(s, token, plan, handle, revision)
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("reference response is cacheable")
			}
			if mode == "current" || mode == "changed" {
				if response.Code != 200 {
					t.Fatal(response.Code, response.Body.String())
				}
				var ref chatPlanReference
				assistantRunMust(t, json.Unmarshal(response.Body.Bytes(), &ref))
				want := plan.References[0]
				if mode == "changed" {
					want.Digest = digestOf([]byte("changed after review"))
				}
				if ref != want {
					t.Fatal("lookup copied pin instead of live content", ref, want)
				}
			} else if response.Code == 200 {
				t.Fatal("unresolved custody accepted", mode, response.Body.String())
			}
			// Display/export and unrelated saves retain even an unresolved Plan.
			if after := policyWorkspaceRead(t, s, token, "/api/conversations"); !bytes.Equal(after.Content, saved.Content) {
				t.Fatal("lookup changed original conversation")
			}
			if mode == "missing-custody" {
				fields, err := assistantTestObject(doc.Chats[0])
				assistantRunMust(t, err)
				plans[0].Status, plans[0].AcceptedAt = "accepted", "2026-10-24T12:00:00Z"
				fields["plans"], fields["interaction"] = assistantChatJSON(plans), assistantChatJSON("work")
				doc.Chats[0] = assistantChatEncodeObject(fields)
				if response := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, doc); response.Code != 409 {
					t.Fatal("import without custody accepted", response.Code, response.Body.String())
				}
				// An unrelated save must still preserve and export the unresolved row.
				assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
				fields, err = assistantTestObject(doc.Chats[0])
				assistantRunMust(t, err)
				fields["title"] = assistantChatJSON("Unrelated title")
				doc.Chats[0] = assistantChatEncodeObject(fields)
				if response := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, doc); response.Code != 200 {
					t.Fatal("unresolved Plan blocked unrelated save", response.Code, response.Body.String())
				}
			}
		})
	}
}

func TestAssistantPlanCurrentFinalAcceptanceAfterLookup(t *testing.T) {
	s, token, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "plan")
	assistantPlanSeed(t, s)
	if result := assistantPlanInvoke(t, e, "lookup-then-accept", true, assistantPlanCandidate); result.Err() {
		t.Fatal(result.Value().Text())
	}
	assistantPlanComplete(t, e)
	assistantRunMust(t, e.ProjectChat(t.Context()))
	saved := policyWorkspaceRead(t, s, token, "/api/conversations")
	var doc conversationDocument
	assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
	plans, err := assistantPlanSource(doc.Chats[0])
	assistantRunMust(t, err)
	if response := assistantPlanReferenceCall(s, token, plans[0], plans[0].References[0].ID, saved.ChatRevisions["chat-one"]); response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "one.json"), []byte("changed after browser lookup"), 0600))
	chat, err := assistantTestObject(doc.Chats[0])
	assistantRunMust(t, err)
	plans[0].Status, plans[0].AcceptedAt = "accepted", "2026-10-24T12:00:00Z"
	chat["interaction"], chat["plans"] = assistantChatJSON("work"), assistantChatJSON(plans)
	doc.Chats[0] = assistantChatEncodeObject(chat)
	response := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, doc)
	if response.Code != 409 || !strings.Contains(response.Body.String(), "Review the current Plan references") {
		t.Fatal("final acceptance used prior lookup", response.Code, response.Body.String())
	}
}

func TestAssistantPlanCurrentLookupNonblockingDialect(t *testing.T) {
	s, token, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "plan")
	assistantPlanSeed(t, s)
	if result := assistantPlanInvoke(t, e, "contended-lookup", true, assistantPlanCandidate); result.Err() {
		t.Fatal(result.Value().Text())
	}
	assistantPlanComplete(t, e)
	assistantRunMust(t, e.ProjectChat(t.Context()))
	saved := policyWorkspaceRead(t, s, token, "/api/conversations")
	var doc conversationDocument
	assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
	plans, err := assistantPlanSource(doc.Chats[0])
	assistantRunMust(t, err)
	root, err := s.openInputPreparations(false)
	assistantRunMust(t, err)
	defer root.Close()
	held, err := root.Open(".")
	assistantRunMust(t, err)
	defer held.Close()
	assistantRunMust(t, lockPrivateData(held, true))
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- assistantPlanReferenceCall(s, token, plans[0], plans[0].References[1].ID, saved.ChatRevisions["chat-one"])
	}()
	select {
	case response := <-done:
		if response.Code == 200 {
			t.Fatal("busy collection admitted lookup")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("private-data lookup waited for Dialect writer")
	}
	// Prove the refused private -> Dialect edge released private custody while
	// the opposite collection -> private edge remains held by this goroutine.
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	lock, err := s.privateDataLockContext(ctx, true)
	assistantRunMust(t, err)
	lock.Close()
	assistantRunMust(t, held.Close())
}

func TestAssistantPlanCurrentLookupRechecksAfterWait(t *testing.T) {
	for _, mode := range []string{"file", "source", "session"} {
		t.Run(mode, func(t *testing.T) {
			s, token, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "plan")
			assistantPlanSeed(t, s)
			if result := assistantPlanInvoke(t, e, "queued-lookup", true, assistantPlanCandidate); result.Err() {
				t.Fatal(result.Value().Text())
			}
			assistantPlanComplete(t, e)
			assistantRunMust(t, e.ProjectChat(t.Context()))
			saved := policyWorkspaceRead(t, s, token, "/api/conversations")
			var doc conversationDocument
			assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
			plans, err := assistantPlanSource(doc.Chats[0])
			assistantRunMust(t, err)
			queued := make(chan struct{})
			testHookAssistantPlanReferenceQueued = func() { close(queued) }
			defer func() { testHookAssistantPlanReferenceQueued = nil }()
			s.writes.Lock()
			held := true
			defer func() {
				if held {
					s.writes.Unlock()
				}
			}()
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				done <- assistantPlanReferenceCall(s, token, plans[0], plans[0].References[0].ID, saved.ChatRevisions["chat-one"])
			}()
			select {
			case <-queued:
			case <-time.After(2 * time.Second):
				t.Fatal("lookup did not reach write barrier")
			}
			switch mode {
			case "file":
				assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "one.json"), []byte("changed while queued"), 0600))
			case "session":
				s.sessions.revoke(token)
			case "source":
				lock, err := s.privateDataLockContext(t.Context(), true)
				assistantRunMust(t, err)
				data, err := s.openChatData()
				assistantRunMust(t, err)
				fields, err := assistantTestObject(doc.Chats[0])
				assistantRunMust(t, err)
				fields["title"] = assistantChatJSON("Changed source while queued")
				doc.Chats[0] = assistantChatEncodeObject(fields)
				assistantRunMust(t, writePrivateData(data.root, s.conversationName(), assistantChatJSON(doc)))
				data.root.Close()
				lock.Close()
			}
			s.writes.Unlock()
			held = false
			select {
			case response := <-done:
				if mode == "file" {
					var ref chatPlanReference
					if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &ref) != nil || ref.Digest != digestOf([]byte("changed while queued")) {
						t.Fatal("lookup read pre-wait content", response.Code, response.Body.String())
					}
				} else if response.Code == 200 {
					t.Fatal("queued identity/source change admitted", response.Body.String())
				}
			case <-time.After(2 * time.Second):
				t.Fatal("queued lookup failed to drain")
			}
		})
	}
}

func TestAssistantPlanHistoricalAcceptanceWithoutJournal(t *testing.T) {
	s, token, _ := assistantExecutionFixture(t, "http://127.0.0.1:1", "plan")
	prep := assistantPlanSeed(t, s)
	saved := policyWorkspaceRead(t, s, token, "/api/conversations")
	var doc conversationDocument
	assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
	fields, err := assistantTestObject(doc.Chats[0])
	assistantRunMust(t, err)
	file, err := os.ReadFile(filepath.Join(s.cfg.ProjectDir, "one.json"))
	assistantRunMust(t, err)
	plan := chatPlan{ID: "historical-plan", Revision: 1, Objective: "Review historical references", Steps: []string{"Read the saved inputs"}, Questions: []string{}, References: []chatPlanReference{{Kind: "file", ID: "one.json", Revision: digestOf(file)}, {Kind: "dialect", ID: prep.Document.ID, Revision: prep.SHA256}}, Status: "proposed", CreatedAt: "2026-10-21T12:00:00Z"}
	fields["plans"] = assistantChatJSON([]chatPlan{plan})
	doc.Chats[0] = assistantChatEncodeObject(fields)
	if response := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, doc); response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	saved = policyWorkspaceRead(t, s, token, "/api/conversations")
	plan.Status, plan.AcceptedAt = "accepted", "2026-10-24T12:00:00Z"
	fields["plans"], fields["interaction"] = assistantChatJSON([]chatPlan{plan}), assistantChatJSON("work")
	doc.Chats[0] = assistantChatEncodeObject(fields)
	if response := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, doc); response.Code != 200 {
		t.Fatal("legacy acceptance acquired new custody requirement", response.Code, response.Body.String())
	}
	retained := policyWorkspaceRead(t, s, token, "/api/conversations")
	if !bytes.Contains(retained.Content, []byte(`"revision":"`+prep.SHA256+`"`)) || bytes.Contains(retained.Content, []byte(`"digest":"`+prep.SHA256+`"`)) {
		t.Fatal("historical pins were rewritten", string(retained.Content))
	}
}
