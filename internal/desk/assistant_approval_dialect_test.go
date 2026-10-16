package desk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type publicationApprovalFixture struct {
	*actionFixture
	saved  inputPreparationReply
	action assistantPublicationAction
}

func newPublicationApprovalFixture(t *testing.T, memory ...bool) *publicationApprovalFixture {
	t.Helper()
	requireBinary(t)
	f := newActionFixture(t)
	f.s.cfg.JpackBin = jpackBinary()
	if len(memory) > 0 && memory[0] {
		f.s.inputPreparations = &memoryPreparationRepository{data: map[string][]byte{}}
	}
	doc := repositoryDocument(95)
	doc.Recipe.Qualification = qualificationFixture()
	saved, e := f.s.preparationRepository().CompareAndSwap(context.Background(), doc, "")
	if e != nil {
		t.Fatal(e)
	}
	req := dialectPublicationRequest{Action: "check", Revision: saved.SHA256, Variant: "asserted", Version: "1.0.0", Impact: artifactImpact{"initial", "Reviewed first publication"}}
	code, raw := publicationCall(t, f.s, doc.ID, req)
	var answer dialectPublicationAnswer
	if code != 200 || json.Unmarshal(raw, &answer) != nil {
		t.Fatalf("check %d %s", code, raw)
	}
	a := assistantPublicationAction{Version: 1, ID: "publication-one", Kind: "publish-dialect", InputID: doc.ID, Revision: saved.SHA256, Publication: &assistantPublicationProposal{Variant: req.Variant, Version: req.Version, Impact: req.Impact, Answer: answer}}
	p := &publicationApprovalFixture{f, saved, a}
	p.persist(t)
	return p
}
func (f *publicationApprovalFixture) persist(t *testing.T) {
	f.chat["inputActions"] = []assistantPublicationAction{f.action}
	f.save(t)
}
func (f *publicationApprovalFixture) body(id string) map[string]any {
	raw, _ := json.Marshal(f.action.request())
	return map[string]any{"action": "dialect-publish", "target": f.action.ID, "id": id, "revision": f.saved.SHA256, "reviewed": true, "content": string(raw)}
}
func (f *publicationApprovalFixture) allow(t *testing.T) string {
	t.Helper()
	code, body := sendJSON(t, f.ts, "POST", "/api/assistant-approvals/chat-one/grant", f.body(""))
	if code != 200 {
		t.Fatal(code, body)
	}
	return body["grant"].(map[string]any)["id"].(string)
}
func (f *publicationApprovalFixture) receipt(t *testing.T) map[string]any {
	t.Helper()
	code, body := sendJSON(t, f.ts, "GET", "/api/input-preparations/"+f.action.InputID+"/publications?request="+f.action.Publication.Answer.Publication.Dialect.Request, nil)
	if code != 200 {
		t.Fatal(code, body)
	}
	return body
}
func TestDialectApprovalManualAndAutomatic(t *testing.T) {
	for _, mode := range []string{"apply", "commit"} {
		t.Run(mode, func(t *testing.T) {
			f := newPublicationApprovalFixture(t)
			id := ""
			if mode == "commit" {
				id = f.allow(t)
			}
			code, body := sendJSON(t, f.ts, "POST", "/api/assistant-approvals/chat-one/"+mode, f.body(id))
			if code != 200 || body["saved"] != true || body["document"] != f.action.Publication.Answer.Document {
				t.Fatal(code, body)
			}
			if f.receipt(t)["result"] == nil {
				t.Fatal("receipt not recoverable")
			}
			after, e := f.s.preparationRepository().Get(context.Background(), f.saved.Document.ID)
			if e != nil || after.SHA256 != f.saved.SHA256 {
				t.Fatal("recipe changed", e)
			}
			_, status := sendJSON(t, f.ts, "GET", "/api/assistant-approvals/chat-one", nil)
			if mode == "apply" && status["grant"] != nil {
				t.Fatal("manual apply minted a grant", status)
			}
			if mode == "commit" && status["grant"].(map[string]any)["state"] != "used" {
				t.Fatal(status)
			}
			code, _ = sendJSON(t, f.ts, "POST", "/api/assistant-approvals/chat-one/"+mode, f.body(id))
			if code != 409 {
				t.Fatal("replayed publication", code)
			}
		})
	}
}
func TestDialectApprovalFailsClosed(t *testing.T) {
	tests := []struct {
		name   string
		change func(*publicationApprovalFixture, map[string]any)
	}{
		{"plan", func(f *publicationApprovalFixture, _ map[string]any) { f.chat["interaction"] = "plan" }},
		{"running", func(f *publicationApprovalFixture, _ map[string]any) { f.state["status"] = "running" }},
		{"question", func(f *publicationApprovalFixture, _ map[string]any) {
			f.state["questions"] = []any{map[string]any{"id": "policy"}}
		}},
		{"unknown", func(f *publicationApprovalFixture, _ map[string]any) { f.state["unknowns"] = []string{"policy"} }},
		{"different target", func(_ *publicationApprovalFixture, r map[string]any) { r["target"] = "other" }},
		{"different action", func(_ *publicationApprovalFixture, r map[string]any) { r["action"] = "research" }},
		{"different content", func(_ *publicationApprovalFixture, r map[string]any) { r["content"] = `{"action":"publish"}` }},
		{"different version", func(f *publicationApprovalFixture, _ map[string]any) { f.action.Publication.Version = "2.0.0" }},
		{"different impact", func(f *publicationApprovalFixture, _ map[string]any) {
			f.action.Publication.Impact.Rationale = "Unreviewed change"
		}},
		{"different document", func(f *publicationApprovalFixture, _ map[string]any) { f.action.Publication.Answer.Document += " " }},
		{"different review", func(f *publicationApprovalFixture, _ map[string]any) {
			f.action.Publication.Answer.Review = &retainedArtifactRef{strings.Repeat("a", 64), 1}
		}},
		{"different recipe", func(f *publicationApprovalFixture, _ map[string]any) {
			doc := f.saved.Document
			doc.Name += " changed"
			if _, e := f.s.preparationRepository().CompareAndSwap(context.Background(), doc, f.saved.SHA256); e != nil {
				panic(e)
			}
		}},
		{"already applied", func(f *publicationApprovalFixture, _ map[string]any) {
			f.action.Applied = json.RawMessage(`{"at":"2026-10-16"}`)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newPublicationApprovalFixture(t)
			id := f.allow(t)
			body := f.body(id)
			tc.change(f, body)
			f.persist(t)
			code, b := sendJSON(t, f.ts, "POST", "/api/assistant-approvals/chat-one/commit", body)
			if code != 409 {
				t.Fatal(code, b)
			}
			if f.receipt(t)["result"] != nil {
				t.Fatal("refused publication was saved")
			}
		})
	}
}
func TestDialectApprovalManualRequiresReviewAndWork(t *testing.T) {
	f := newPublicationApprovalFixture(t)
	for _, kind := range []string{"unreviewed", "plan", "no grant", "pack grant"} {
		t.Run(kind, func(t *testing.T) {
			body := f.body("")
			op := "apply"
			f.chat["interaction"] = "work"
			switch kind {
			case "unreviewed":
				body["reviewed"] = false
			case "plan":
				f.chat["interaction"] = "plan"
			case "no grant":
				op = "commit"
			case "pack grant":
				body["id"] = f.actionFixture.grant(t)["id"]
				op = "commit"
			}
			f.persist(t)
			code, b := sendJSON(t, f.ts, "POST", "/api/assistant-approvals/chat-one/"+op, body)
			if code != 409 {
				t.Fatal(code, b)
			}
		})
	}
}
func TestDialectApprovalLifecycleAndConcurrency(t *testing.T) {
	for _, kind := range []string{"expired", "revoked", "session", "concurrent"} {
		t.Run(kind, func(t *testing.T) {
			f := newPublicationApprovalFixture(t)
			id := f.allow(t)
			switch kind {
			case "expired":
				for k, g := range f.s.actionGrants {
					g.ExpiresAt = time.Now().Add(-time.Second)
					f.s.actionGrants[k] = g
				}
			case "revoked":
				sendJSON(t, f.ts, "POST", "/api/assistant-approvals/chat-one/revoke", map[string]any{"id": id})
			case "session":
				for k, g := range f.s.actionGrants {
					delete(f.s.actionGrants, k)
					f.s.actionGrants["another-session"] = g
				}
			}
			if kind != "concurrent" {
				code, b := sendJSON(t, f.ts, "POST", "/api/assistant-approvals/chat-one/commit", f.body(id))
				if code != 409 {
					t.Fatal(code, b)
				}
				return
			}
			codes := make(chan int, 2)
			var wg sync.WaitGroup
			for range 2 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					code, _ := sendJSON(t, f.ts, "POST", "/api/assistant-approvals/chat-one/commit", f.body(id))
					codes <- code
				}()
			}
			wg.Wait()
			close(codes)
			counts := map[int]int{}
			for c := range codes {
				counts[c]++
			}
			if counts[200] != 1 || counts[409] != 1 {
				t.Fatal(counts)
			}
		})
	}
}
func TestDialectApprovalConsumedFailureAndReadOnlyRecovery(t *testing.T) {
	f := newPublicationApprovalFixture(t)
	id := f.allow(t)
	bin := f.s.cfg.JpackBin
	f.s.cfg.JpackBin = "/missing-runtime"
	code, _ := sendJSON(t, f.ts, "POST", "/api/assistant-approvals/chat-one/commit", f.body(id))
	if code == 200 {
		t.Fatal("saved without Runtime")
	}
	_, status := sendJSON(t, f.ts, "GET", "/api/assistant-approvals/chat-one", nil)
	if status["grant"].(map[string]any)["state"] != "used" {
		t.Fatal(status)
	}
	f.s.cfg.JpackBin = bin
	id = f.allow(t)
	code, body := sendJSON(t, f.ts, "POST", "/api/assistant-approvals/chat-one/commit", f.body(id))
	if code != 200 {
		t.Fatal(code, body)
	}
	// A recipe update, Plan transition and unavailable Runtime cannot hide receipts.
	doc := f.saved.Document
	doc.Name = "Later recipe"
	if _, e := f.s.preparationRepository().CompareAndSwap(context.Background(), doc, f.saved.SHA256); e != nil {
		t.Fatal(e)
	}
	f.s.cfg.JpackBin = "/missing-runtime"
	f.chat["interaction"] = "plan"
	f.persist(t)
	got := f.receipt(t)["result"].(map[string]any)
	if got["document"] != body["document"] || got["saved"] != true {
		t.Fatal(got)
	}
	ledger, e := f.s.readDialectPublicationLedger()
	if e != nil || len(ledger.Items) != 1 {
		t.Fatal(e, ledger)
	}
}
func TestDialectApprovalChangedBaseAndCandidateInvalidateAllowance(t *testing.T) {
	f := newPublicationApprovalFixture(t)
	f.allow(t)
	checkAndPublishDialect(t, f.s, f.saved, "asserted", "1.0.0")
	_, status := sendJSON(t, f.ts, http.MethodGet, "/api/assistant-approvals/chat-one", nil)
	if status["grant"].(map[string]any)["state"] != "stale" {
		t.Fatal(status)
	}
}

func TestDialectApprovalMemoryRepositoryAndDeletedRecipeRecovery(t *testing.T) {
	for _, mode := range []string{"apply", "commit"} {
		t.Run(mode, func(t *testing.T) {
			f := newPublicationApprovalFixture(t, true)
			id := ""
			if mode == "commit" {
				id = f.allow(t)
			}
			code, body := sendJSON(t, f.ts, "POST", "/api/assistant-approvals/chat-one/"+mode, f.body(id))
			if code != 200 {
				t.Fatal(code, body)
			}
			repo := f.s.inputPreparations.(*memoryPreparationRepository)
			repo.mu.Lock()
			delete(repo.data, f.action.InputID)
			repo.mu.Unlock()
			f.s.cfg.JpackBin = "/missing-runtime"
			result := f.receipt(t)["result"].(map[string]any)
			if result["document"] != body["document"] {
				t.Fatal(result)
			}
		})
	}
}

// Simulate an acknowledgement/read-back failure after the durable ledger write,
// without adding a production fault hook or replaying the publication transaction.
type publicationReadbackFailure struct {
	retainedArtifactRepository
	afterCommit func() bool
}

func (r publicationReadbackFailure) Get(ctx context.Context, ref retainedArtifactRef) ([]byte, error) {
	if r.afterCommit() {
		return nil, errors.New("read-back unavailable after publication")
	}
	return r.retainedArtifactRepository.Get(ctx, ref)
}
func TestDialectApprovalLostAcknowledgementRetainsExactReceipt(t *testing.T) {
	f := newPublicationApprovalFixture(t)
	id := f.allow(t)
	failed := false
	f.s.retainedArtifacts = publicationReadbackFailure{f.s.artifactRepository(), func() bool {
		ledger, e := f.s.readDialectPublicationLedger()
		if e == nil && len(ledger.Items) == 1 && !failed {
			failed = true
			return true
		}
		return false
	}}
	code, body := sendJSON(t, f.ts, "POST", "/api/assistant-approvals/chat-one/commit", f.body(id))
	if code != 503 || !failed {
		t.Fatal(code, body)
	}
	_, status := sendJSON(t, f.ts, "GET", "/api/assistant-approvals/chat-one", nil)
	if status["grant"].(map[string]any)["state"] != "used" {
		t.Fatal(status)
	}
	f.s.cfg.JpackBin = "/missing-runtime"
	result := f.receipt(t)["result"].(map[string]any)
	if result["document"] != f.action.Publication.Answer.Document || result["saved"] != true {
		t.Fatal(result)
	}
	code, _ = sendJSON(t, f.ts, "POST", "/api/assistant-approvals/chat-one/commit", f.body(id))
	if code != 409 {
		t.Fatal("replayed consumed allowance", code)
	}
	ledger, e := f.s.readDialectPublicationLedger()
	if e != nil || len(ledger.Items) != 1 {
		t.Fatal(e, ledger)
	}
}
