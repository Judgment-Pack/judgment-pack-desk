package desk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func approvalFixture(t *testing.T) (*Server, *os.Root, inputResearchRun, string) {
	t.Helper()
	s, rr, run, _ := budgetHTTPFixture(t)
	var shared map[string]any
	json.Unmarshal([]byte(researchDeskFile("http://127.0.0.1:1")), &shared)
	shared["assistant"] = map[string]any{"engine": "codex", "agent": map[string]any{"provider": "openai", "authMethod": "subscription", "model": "test-model", "tools": []string{}}}
	raw, _ := json.Marshal(shared)
	writeDeskConfig(t, s, string(raw))
	registry, err := s.readAIRegistry()
	if err != nil || len(registry.Connections) != 1 {
		t.Fatal(err)
	}
	run.State = "review"
	run.Phase = ""
	run.Step = 0
	run.Attempts = nil
	run.Recipe.AI.Revision = registry.Connections[0].Revision
	run.Gateway = inputResearchPin{Authority: "gateway:test", Public: testSignerPublic}
	p, err := s.openInputPreparations(false)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	doc, err := readInputPreparation(p, run.PreparationID)
	if err != nil {
		t.Fatal(err)
	}
	doc.Document.Recipe.Research = &run.Recipe
	if _, err = writeInputPreparation(p, doc.Document); err != nil {
		t.Fatal(err)
	}
	doc, err = readInputPreparation(p, run.PreparationID)
	if err != nil {
		t.Fatal(err)
	}
	run.Revision = doc.SHA256
	if err = writeInputResearch(rr, run); err != nil {
		t.Fatal(err)
	}
	return s, rr, run, doc.SHA256
}
func approvalHTTP(s *Server, chat, suffix, body string) *httptest.ResponseRecorder {
	method := "POST"
	if body == "" {
		method = "GET"
	}
	r := httptest.NewRequest(method, "/api/input-research-approvals/"+chat+suffix, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func requestApprovalRun(s *Server, run inputResearchRun, chat, turn string) *httptest.ResponseRecorder {
	return approvalHTTP(s, chat, "/request", fmt.Sprintf(`{"preparationId":%q,"revision":%q,"turnId":%q}`, run.PreparationID, run.Revision, turn))
}
func authorizeFixture(t *testing.T, s *Server, run inputResearchRun) {
	t.Helper()
	w := budgetHTTP(s, run, "/authorize", run.Revision, `{"chatId":"chat-1","minutes":60}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestResearchApprovalManualAndAutoShareAdmission(t *testing.T) {
	for _, mode := range []string{"manual", "auto"} {
		t.Run(mode, func(t *testing.T) {
			s, rr, run, _ := approvalFixture(t)
			if mode == "auto" {
				authorizeFixture(t, s, run)
				granted, err := readInputResearch(rr)
				if err != nil || granted.State != "review" || len(granted.Attempts) != 0 {
					t.Fatal("enabling Auto started work", err)
				}
				s.scanInputResearch(context.Background())
				granted, _ = readInputResearch(rr)
				if granted.State != "review" {
					t.Fatal("worker dispatched unused grant")
				}
				status := approvalHTTP(s, "chat-1", "", "")
				if status.Code != 200 || !strings.Contains(status.Body.String(), `"mode":"auto"`) {
					t.Fatal(status.Code, status.Body.String())
				}
				if w := budgetHTTP(s, run, "/confirm", "", `{}`); w.Code != 409 {
					t.Fatal("manual confirmation bypassed Auto scope")
				}
				w := requestApprovalRun(s, run, "chat-1", "turn-1")
				if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"started"`) {
					t.Fatal(w.Code, w.Body.String())
				}
				same := requestApprovalRun(s, run, "chat-1", "turn-1")
				if same.Code != 200 || same.Body.String() != w.Body.String() {
					t.Fatal("response retry changed operation")
				}
				another := requestApprovalRun(s, run, "chat-1", "turn-2")
				if another.Code != 200 || !strings.Contains(another.Body.String(), "review-required") {
					t.Fatal("spent allowance reused")
				}
			} else {
				w := requestApprovalRun(s, run, "chat-1", "turn-1")
				if w.Code != 200 || !strings.Contains(w.Body.String(), "review-required") {
					t.Fatal("manual chat request started work")
				}
				w = budgetHTTP(s, run, "/confirm", "", `{}`)
				if w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
			}
			got, err := readInputResearch(rr)
			if err != nil || got.State != "queued" || got.Recipe.Limits != run.Recipe.Limits || len(got.Attempts) != 0 {
				t.Fatal("admission changed allowance or dispatched synchronously", err)
			}
			if mode == "auto" && (got.Approval.UsedAt == "" || got.Approval.TurnID != "turn-1") {
				t.Fatal("approval consumption not retained")
			}
		})
	}
}
func TestResearchApprovalScopeExpiryAndRevocation(t *testing.T) {
	for _, scenario := range []string{"other-chat", "other-input", "other-session", "restart", "expired", "revoked", "recipe", "connection", "gateway", "newer-run"} {
		t.Run(scenario, func(t *testing.T) {
			s, rr, run, _ := approvalFixture(t)
			authorizeFixture(t, s, run)
			candidate := run
			chat := "chat-1"
			switch scenario {
			case "other-chat":
				chat = "chat-2"
			case "other-input":
				candidate.PreparationID = "prep_" + strings.Repeat("c", 32)
			case "other-session":
				changeInputResearch(rr, func(r *inputResearchRun) error { r.Approval.SessionScope = strings.Repeat("f", 64); return nil })
			case "restart":
				s.sessions, _ = newSessionStore()
			case "expired":
				changeInputResearch(rr, func(r *inputResearchRun) error {
					r.Approval.ApprovedAt = time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339Nano)
					r.Approval.ExpiresAt = time.Now().Add(-time.Hour - time.Second).UTC().Format(time.RFC3339Nano)
					return nil
				})
			case "revoked":
				w := approvalHTTP(s, chat, "/revoke", fmt.Sprintf(`{"runId":%q}`, run.ID))
				if w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
			case "recipe":
				p, _ := s.openInputPreparations(false)
				doc, _ := readInputPreparation(p, run.PreparationID)
				doc.Document.RecipeVersion++
				writeInputPreparation(p, doc.Document)
				p.Close()
			case "connection":
				writeDeskConfig(t, s, researchDeskFile("http://127.0.0.1:1"))
			case "gateway":
				present, raw, err := s.readDeskFile()
				if !present || err != nil {
					t.Fatal(err)
				}
				writeDeskConfig(t, s, strings.Replace(string(raw), "gateway:test", "gateway:changed", 1))
			case "newer-run":
				root, _ := s.inputResearchRoot(false)
				defer root.Close()
				other := run
				other.ID = "research_" + strings.Repeat("f", 32)
				other.State = "completed"
				other.CreatedAt = time.Now().Add(time.Second).UTC().Format(time.RFC3339Nano)
				other.Attempts = []inputResearchAttempt{{Kind: "model", State: "completed"}}
				nr, _ := openInputResearch(root, other.ID, true)
				defer nr.Close()
				writeInputResearch(nr, other)
			}
			w := requestApprovalRun(s, candidate, chat, "turn-1")
			if w.Code != 409 && (w.Code != 200 || !strings.Contains(w.Body.String(), "review-required")) {
				t.Fatal("out of scope Auto admitted", scenario, w.Code, w.Body.String())
			}
			got, err := readInputResearch(rr)
			if err != nil || got.State != "review" || got.Approval.UsedAt != "" || len(got.Attempts) != 0 {
				t.Fatal("refused request used allowance", err)
			}
		})
	}
}
func TestResearchApprovalRejectsUnreviewedArgumentsAndConcurrentStarts(t *testing.T) {
	s, rr, run, _ := approvalFixture(t)
	for _, body := range []string{`{"chatId":"chat-1","minutes":0}`, `{"chatId":"chat-1","minutes":61}`, `{"chatId":"chat-1","minutes":60,"limits":{"modelCalls":100}}`} {
		if w := budgetHTTP(s, run, "/authorize", run.Revision, body); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := budgetHTTP(s, run, "/authorize", "stale", `{"chatId":"chat-1","minutes":60}`); w.Code != 409 {
		t.Fatal("stale allowance accepted")
	}
	authorizeFixture(t, s, run)
	if w := approvalHTTP(s, "chat-1", "/request", fmt.Sprintf(`{"preparationId":%q,"revision":%q,"turnId":"turn-1","limits":{"modelCalls":100}}`, run.PreparationID, run.Revision)); w.Code != 400 {
		t.Fatal("client expanded grant")
	}
	var wg sync.WaitGroup
	responses := make(chan *httptest.ResponseRecorder, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			responses <- requestApprovalRun(s, run, "chat-1", fmt.Sprint("turn-", i))
		}(i)
	}
	wg.Wait()
	close(responses)
	started := 0
	for w := range responses {
		if w.Code == 200 && strings.Contains(w.Body.String(), `"status":"started"`) {
			started++
		} else if w.Code != 409 && (w.Code != 200 || !strings.Contains(w.Body.String(), "review-required")) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	got, err := readInputResearch(rr)
	if err != nil || started != 1 || got.State != "queued" {
		t.Fatal("multiple starts", started, err)
	}
	if w := approvalHTTP(s, "chat-1", "/revoke", fmt.Sprintf(`{"runId":%q}`, run.ID)); w.Code != 409 {
		t.Fatal("revocation silently stopped admitted work")
	}
}
func TestResearchApprovalGuardsAndInvalidSavedGrants(t *testing.T) {
	s, rr, run, _ := approvalFixture(t)
	r := httptest.NewRequest("POST", "/api/input-research-approvals/chat-1/request", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("unauthenticated approval action", w.Code)
	}
	authorizeFixture(t, s, run)
	granted, err := readInputResearch(rr)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*inputResearchRun){
		func(r *inputResearchRun) {
			r.Approval.ExpiresAt = time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339Nano)
		},
		func(r *inputResearchRun) { r.Approval.UsedAt = time.Now().UTC().Format(time.RFC3339Nano) },
		func(r *inputResearchRun) { r.State = "queued" },
		func(r *inputResearchRun) { r.ContinuationOf = "research_" + strings.Repeat("c", 32) },
	} {
		raw, _ := json.Marshal(granted)
		var changed inputResearchRun
		json.Unmarshal(raw, &changed)
		change(&changed)
		if validateResearchApproval(changed) == nil {
			t.Fatal("invalid allowance accepted")
		}
	}
}

func TestResearchApprovalRejectsMalformedRequestsWithoutSavedRuns(t *testing.T) {
	s, _ := newTestServer(t, false)
	for _, suffix := range []string{"/request", "/revoke"} {
		if w := approvalHTTP(s, "chat-1", suffix, `{"auto":true}`); w.Code != 400 {
			t.Fatal("missing store bypassed argument validation", w.Code)
		}
	}
}

func TestResearchApprovalCancelledRequestKeepsUnusedAllowance(t *testing.T) {
	s, rr, run, _ := approvalFixture(t)
	authorizeFixture(t, s, run)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	body := fmt.Sprintf(`{"preparationId":%q,"revision":%q,"turnId":"turn-1"}`, run.PreparationID, run.Revision)
	r := httptest.NewRequest("POST", "/api/input-research-approvals/chat-1/request", strings.NewReader(body)).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 409 {
		t.Fatal("cancelled request admitted", w.Code, w.Body.String())
	}
	got, err := readInputResearch(rr)
	if err != nil || got.State != "review" || got.Approval.UsedAt != "" {
		t.Fatal("cancelled request consumed allowance", err)
	}
}
