package desk

import (
	"bytes"
	"context"
	"fmt"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func assistantResearchHTTP(t *testing.T, s *Server, token, path, revision, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("If-Match", revision)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func assistantResearchSeed(t *testing.T, s *Server, token string, e *assistantRunExecution, grant bool) (inputPreparationReply, *os.Root, inputResearchRun) {
	t.Helper()
	writeDeskConfig(t, s, researchDeskFile("http://127.0.0.1:1"))
	d := repositoryDocument(1)
	recipe := researchRecipeFixture()
	recipe.Target = "records"
	recipe.AI = inputResearchAI{Connection: e.source.Binding.ConnectionID, Revision: e.source.Binding.ConnectionRevision, Model: e.source.Binding.Model}
	recipe.Limits.Records = 3
	recipe.Limits.Pages = 3
	recipe.Limits.ModelCalls = 6
	d.Recipe.Research = &recipe
	saved, err := s.preparationRepository().CompareAndSwap(t.Context(), d, "")
	assistantRunMust(t, err)
	root, err := s.inputResearchRoot(true)
	assistantRunMust(t, err)
	defer root.Close()
	rr, err := openInputResearch(root, assistantRecordRun, true)
	assistantRunMust(t, err)
	t.Cleanup(func() { rr.Close() })
	run := inputResearchRun{Version: 1, ID: assistantRecordRun, PreparationID: d.ID, Revision: saved.SHA256, RecipeVersion: d.RecipeVersion, Recipe: recipe, Gateway: inputResearchPin{Authority: "gateway:test", Public: testSignerPublic}, State: "review", CreatedAt: "2026-10-20T00:00:00Z", UpdatedAt: "2026-10-20T00:00:00Z", Records: []inputResearchEntity{}, Sources: []inputResearchSource{}, Attempts: []inputResearchAttempt{}, Seeds: []string{}, Companies: []inputResearchEntity{}}
	assistantRunMust(t, s.researchJournal(rr, run.ID).Create(t.Context(), run))
	// Keep the independent worker's execution lease; this fixture admits real
	// queued research without ever allowing a provider/background model dispatch.
	worker, err := rr.OpenFile("worker.lock", os.O_CREATE|os.O_RDWR, 0600)
	assistantRunMust(t, err)
	assistantRunMust(t, waitPrivateDataLock(t.Context(), worker, true))
	t.Cleanup(func() { worker.Close() })
	if grant {
		w := assistantResearchHTTP(t, s, token, "/api/input-preparations/"+run.PreparationID+"/research/"+run.ID+"/authorize", saved.SHA256, fmt.Sprintf(`{"chatId":%q,"minutes":60}`, e.record.Binding.Owner.ID))
		if w.Code != http.StatusOK {
			t.Fatal("real grant refused", w.Code, w.Body.String())
		}
	}
	return saved, rr, run
}
func assistantResearchArgs(saved inputPreparationReply) string {
	return string(assistantChatJSON(map[string]string{"id": saved.Document.ID, "revision": saved.SHA256}))
}
func assistantResearchUsed(t *testing.T, s *Server, rr *os.Root, run inputResearchRun) inputResearchRun {
	t.Helper()
	got, err := s.researchJournal(rr, run.ID).Read(t.Context())
	assistantRunMust(t, err)
	if got.State != "queued" || got.Approval == nil || got.Approval.UsedAt == "" || len(got.Attempts) != 0 || got.StopRequested {
		t.Fatalf("real allowance was not queued once without provider work: %+v", got)
	}
	return got
}
func TestAssistantDialectResearchRequestAndReplay(t *testing.T) {
	for _, mode := range []string{"review", "grant", "manual-first", "other-host-first"} {
		t.Run(mode, func(t *testing.T) {
			s, token, e := assistantRecipeBrowserFixture(t, "http://127.0.0.1:1")
			saved, rr, run := assistantResearchSeed(t, s, token, e, mode != "review")
			x := assistantExecutionOpen(t, s, e)
			assistantRecordRead(t, e, x, saved)
			if mode == "manual-first" || mode == "other-host-first" {
				q := inputResearchRequest{ChatID: e.record.Binding.Owner.ID, PreparationID: run.PreparationID, Revision: run.Revision, TurnID: e.record.Binding.RequestID}
				if mode == "other-host-first" {
					q.RequestKey = strings.Repeat("b", 64)
				}
				receipt, err := s.requestResearch(e.request.WithContext(t.Context()), q, nil)
				assistantRunMust(t, err)
				if receipt.Status != "started" {
					t.Fatal("earlier same-turn request not admitted")
				}
				// Historical receipt must survive changed preparation without another admission.
				doc := saved.Document
				doc.Name = "Changed after original admission"
				_, err = s.preparationRepository().CompareAndSwap(t.Context(), doc, saved.SHA256)
				assistantRunMust(t, err)
			}
			r := assistantRecipeInvoke(t, e, x, "request", assistantResearchTool, assistantResearchArgs(saved))
			if r.Err() {
				t.Fatal("research request refused", r.Value().Text())
			}
			record := assistantExecutionRecord(t, e)
			assistantRunMust(t, assistantRecipeImport(t, e, record))
			if len(record.DomainOutputs) != 1 || record.DomainOutputs[0].DialectAction.Research == nil {
				t.Fatal("finite research output missing")
			}
			p := record.DomainOutputs[0].DialectAction
			if mode == "review" {
				if p.Action.Applied != nil || !strings.Contains(r.Value().Text(), "review-required") {
					t.Fatal("no grant manufactured admission")
				}
			} else {
				used := assistantResearchUsed(t, s, rr, run)
				if p.Action.Applied == nil || p.Action.Applied.RunID != used.ID || p.Action.Applied.At != used.Approval.UsedAt {
					t.Fatal("applied does not derive from actual used receipt")
				}
				if mode == "manual-first" && used.Approval.RequestKey != "" || mode == "other-host-first" && used.Approval.RequestKey != strings.Repeat("b", 64) {
					t.Fatal("existing same-turn provenance rewritten")
				}
			}
			// Exact same call returns the immutable host receipt, never the owner again.
			before, err := s.researchJournal(rr, run.ID).Read(t.Context())
			assistantRunMust(t, err)
			replayCtx := context.WithValue(runcontext.WithCallID(t.Context(), "request"), assistantHostRootKey{}, true)
			r2, err := assistantGraphFind(t, x.Run.HostTools, assistantResearchTool).Execute(replayCtx, []byte(assistantResearchArgs(saved)))
			assistantRunMust(t, err)
			if r2.Err() || !bytes.Equal(r.Value().JSON(), r2.Value().JSON()) {
				t.Fatal("delivery changed retained result")
			}
			after, err := s.researchJournal(rr, run.ID).Read(t.Context())
			assistantRunMust(t, err)
			if !bytes.Equal(assistantChatJSON(before), assistantChatJSON(after)) {
				t.Fatal("redelivery mutated owner")
			}
		})
	}
}
func TestAssistantDialectResearchFiniteArguments(t *testing.T) {
	good := `{"id":"prep_00000000000000000000000000000001","revision":"` + strings.Repeat("a", 64) + `"}`
	if _, err := readAssistantDialectResearchArguments([]byte(good)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(good, `"id":`, `"ID":`, 1), strings.Replace(good, `"id":`, `"id":null,"id":`, 1), strings.Replace(good, `"revision":"`+strings.Repeat("a", 64)+`"`, `"revision":null`, 1), strings.TrimSuffix(good, "}") + `,"turnId":"injected"}`, strings.TrimSuffix(good, "}") + `,"approval":true}`, `null`, `[]`} {
		if _, err := readAssistantDialectResearchArguments([]byte(bad)); err == nil {
			t.Fatal("foreign request authority accepted", bad)
		}
	}
}
