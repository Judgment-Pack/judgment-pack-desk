package desk

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func policyTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	// HTTP recorders exercise the real guard/routes without opening a listener.
	s, err := New(Config{ProjectDir: t.TempDir(), DeskConfigDir: t.TempDir(), JpackBin: jpackBinary(), Token: testToken, Port: 8791, Logger: log.New(io.Discard, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	// This fixture exercises the existing guarded subprocess boundary, not a
	// second version parser. Domain race tests substitute a controlled read.
	bin := filepath.Join(t.TempDir(), "version-runtime")
	script := `#!/bin/sh
if [ "$1 $2" != 'artifact version' ] || [ "$3" != '--base' ] || [ "$5" != '--candidate' ]; then exit 1; fi
case "$4" in
  1.0.0|1.1.0|2.0.0) comparison=equal; if [ "$4" != "$6" ]; then comparison=greater; fi; printf '{"outputVersion":"2","command":"artifact version","status":"available","scope":"version-arithmetic","base":"%s","candidate":"%s","comparison":"%s","compatibilityChecked":false}' "$4" "$6" "$comparison" ;;
  *) printf '{"outputVersion":"2","command":"artifact version","status":"refused","scope":"version-arithmetic","diagnostics":[{"code":"JPS-ARTIFACT-VERSION"}]}'; exit 2 ;;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	s.cfg.JpackBin = bin
	return s, policyTestSession(t, s)
}
func policyTestSession(t *testing.T, s *Server) string {
	t.Helper()
	token, err := s.sessions.createExpiring("local user", nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	return token
}
func policyCall(s *Server, token, method, path string, body any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func policyMutation(t *testing.T, w *httptest.ResponseRecorder) policyMutationReply {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("policy mutation %d: %s", w.Code, w.Body.String())
	}
	var result policyMutationReply
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func policyRead(t *testing.T, s *Server, token, id string) policyReply {
	t.Helper()
	w := policyCall(s, token, "GET", "/api/policies/"+id, nil)
	if w.Code != 200 {
		t.Fatalf("policy read %d: %s", w.Code, w.Body.String())
	}
	var result policyReply
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func policyOp(t *testing.T) string {
	t.Helper()
	id, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func policyCreate(t *testing.T, s *Server, token string) policyReply {
	t.Helper()
	return policyMutation(t, policyCall(s, token, "POST", "/api/policies", policyEditRequest{OperationID: policyOp(t), Title: "Data access", Narrative: "Only approved staff may access records.\r\nRetain missing values, 中文, < and >.", ArtifactVersion: "1.0.0"})).Policy
}
func policyReviewBody(t *testing.T, p policyReply, decision string) policyReviewRequest {
	t.Helper()
	return policyReviewRequest{OperationID: policyOp(t), BaseRevision: p.Revision, CandidateDigest: p.Document.Candidate.Digest, WorkflowDigest: p.Document.WorkflowDigest, Decision: decision}
}
func policyApprove(t *testing.T, s *Server, token string, p policyReply) policyReply {
	t.Helper()
	return policyMutation(t, policyCall(s, token, "POST", "/api/policies/"+p.Document.ID+"/review", policyReviewBody(t, p, "approve"))).Policy
}
func policyPublishBody(t *testing.T, p policyReply) policyPublishRequest {
	t.Helper()
	return policyPublishRequest{OperationID: policyOp(t), BaseRevision: p.Revision, CandidateDigest: p.Document.Candidate.Digest, WorkflowDigest: p.Document.WorkflowDigest, ReviewID: p.Document.CurrentReviewID}
}
func policyEditBody(t *testing.T, p policyReply) policyEditRequest {
	t.Helper()
	return policyEditRequest{OperationID: policyOp(t), BaseRevision: p.Revision, Title: p.Document.Candidate.Title, Narrative: p.Document.Candidate.Narrative, ArtifactVersion: p.Document.Candidate.ArtifactVersion, Workflow: &p.Document.Workflow}
}
func policyError(t *testing.T, w *httptest.ResponseRecorder, status int, code string) map[string]string {
	t.Helper()
	var result map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	if w.Code != status || result["code"] != code || w.Header().Get(RefusalHeader) != code {
		t.Fatalf("expected %d %s, got %d %s", status, code, w.Code, w.Body.String())
	}
	return result
}

func TestPolicyLocalLifecyclePersistenceAndHistory(t *testing.T) {
	s, token := policyTestServer(t)
	created := policyCreate(t, s, token)
	id := created.Document.ID
	if created.Document.ReviewState != "unreviewed" || created.Document.ManagementMode != "desk-managed" {
		t.Fatal(created)
	}
	actor := created.Document.Revisions[0].Actor
	if actor.Kind != "local" || actor.Subject != "local user" || actor.Issuer != "" {
		t.Fatal("actor was not the real session", actor)
	}
	policyError(t, policyCall(s, token, "POST", "/api/policies/"+id+"/publish", policyPublishBody(t, created)), 409, "review-incomplete")
	approved := policyApprove(t, s, token, created)
	wrong := policyPublishBody(t, approved)
	wrong.ReviewID = "review_" + strings.Repeat("a", 48)
	policyError(t, policyCall(s, token, "POST", "/api/policies/"+id+"/publish", wrong), 409, "review-incomplete")
	published := policyMutation(t, policyCall(s, token, "POST", "/api/policies/"+id+"/publish", policyPublishBody(t, approved))).Policy
	if len(published.Document.Publications) != 1 || published.Document.Publications[0].Actor != actor || published.Document.Reviews[0].Actor != actor || published.Document.Publications[0].Content != created.Document.Candidate.Content {
		t.Fatal("publication lost exact basis or real actor", published)
	}
	var list struct {
		Items []policySummary `json:"items"`
	}
	w := policyCall(s, token, "GET", "/api/policies", nil)
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if w.Code != 200 || len(list.Items) != 1 || !list.Items[0].CurrentCandidatePublished {
		t.Fatal(w.Code, w.Body.String())
	}
	edit := policyEditBody(t, published)
	edit.Narrative = "Changed successor narrative"
	edit.ArtifactVersion = "1.1.0"
	edited := policyMutation(t, policyCall(s, token, "PUT", "/api/policies/"+id, edit)).Policy
	if edited.Document.ReviewState != "unreviewed" || edited.Document.CurrentReviewID != "" || len(edited.Document.Reviews) != 1 || len(edited.Document.Publications) != 1 {
		t.Fatal("edit carried current review or lost history", edited)
	}
	w = policyCall(s, token, "GET", "/api/policies/"+id+"/revisions/"+created.Document.Candidate.RevisionID, nil)
	var revision struct {
		Candidate policyCandidate `json:"candidate"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &revision)
	if w.Code != 200 || revision.Candidate.Narrative != created.Document.Candidate.Narrative || revision.Candidate.Content != created.Document.Candidate.Content {
		t.Fatal("old exact content unavailable", w.Body.String())
	}
	// Conversation deletion owns no policy content or lifecycle record.
	current := draftCall(s, "GET", "/api/conversations", nil, "")
	var chats conversationReply
	_ = json.Unmarshal(current.Body.Bytes(), &chats)
	removed := draftCall(s, "PUT", "/api/conversations", map[string]any{"version": 1, "chats": []any{}}, chats.SHA256)
	if removed.Code != 200 {
		t.Fatal(removed.Code, removed.Body.String())
	}
	if got := policyRead(t, s, token, id); got.Document.Candidate.Narrative != edit.Narrative {
		t.Fatal("chat deletion lost policy")
	}
	// A second service/process context opens the same persisted project data.
	config := s.cfg
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	token = policyTestSession(t, reopened)
	got := policyRead(t, reopened, token, id)
	if got.Revision != edited.Revision || len(got.Document.Revisions) != 2 || len(got.Document.Reviews) != 1 || len(got.Document.Publications) != 1 {
		t.Fatal("reload lost retained history", got)
	}
	w = policyCall(reopened, token, "GET", "/api/policies", nil)
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Items) != 1 || list.Items[0].CurrentCandidatePublished || list.Items[0].CandidateDigest != edited.Document.Candidate.Digest {
		t.Fatal("old publication was attributed to changed candidate", w.Body.String())
	}
}

func TestPolicyStaleEditorsAndExactCandidateReview(t *testing.T) {
	s, token := policyTestServer(t)
	created := policyCreate(t, s, token)
	approved := policyApprove(t, s, token, created)
	id := created.Document.ID
	first, stale := policyEditBody(t, approved), policyEditBody(t, approved)
	first.Title = "Changed title"
	stale.Narrative = "Stale editor's distinct content"
	saved := policyMutation(t, policyCall(s, token, "PUT", "/api/policies/"+id, first)).Policy
	if saved.Document.Candidate.Digest == approved.Document.Candidate.Digest {
		t.Fatal("title change did not change candidate digest")
	}
	conflict := policyError(t, policyCall(s, token, "PUT", "/api/policies/"+id, stale), 409, "revision-conflict")
	if conflict["currentRevision"] != saved.Revision {
		t.Fatal(conflict)
	}
	if got := policyRead(t, s, token, id); got.Document.Candidate.Title != first.Title || got.Document.Candidate.Narrative == stale.Narrative || got.Document.ReviewState != "unreviewed" {
		t.Fatal("stale save changed current content", got)
	}
	// Even returning to identical old bytes creates a new reviewable revision;
	// historical approval is not silently rebound to the later candidate.
	revert := policyEditBody(t, saved)
	revert.Title = approved.Document.Candidate.Title
	reverted := policyMutation(t, policyCall(s, token, "PUT", "/api/policies/"+id, revert)).Policy
	if reverted.Document.Candidate.Digest != approved.Document.Candidate.Digest || reverted.Document.Candidate.RevisionID == approved.Document.Candidate.RevisionID || reverted.Document.ReviewState != "unreviewed" {
		t.Fatal("old review carried across revisions", reverted)
	}
	for _, field := range []string{"narrative", "artifactVersion", "workflow"} {
		approved = policyApprove(t, s, token, reverted)
		edit := policyEditBody(t, approved)
		switch field {
		case "narrative":
			edit.Narrative += " changed"
		case "artifactVersion":
			edit.ArtifactVersion = "1.1.0"
		case "workflow":
			edit.Workflow = &policyWorkflow{IndependentReviewRequired: true}
		}
		reverted = policyMutation(t, policyCall(s, token, "PUT", "/api/policies/"+id, edit)).Policy
		if field == "workflow" {
			if reverted.Document.WorkflowDigest == approved.Document.WorkflowDigest {
				t.Fatal("workflow change retained workflow digest")
			}
		} else if reverted.Document.Candidate.Digest == approved.Document.Candidate.Digest {
			t.Fatal(field, "was omitted from candidate digest")
		}
		if reverted.Document.ReviewState != "unreviewed" || reverted.Document.CurrentReviewID != "" {
			t.Fatal(field, "did not invalidate review")
		}
	}
	policyError(t, policyCall(s, token, "POST", "/api/policies/"+id+"/review", policyReviewBody(t, reverted, "approve")), 422, "unsupported-workflow")
	policyError(t, policyCall(s, token, "POST", "/api/policies/"+id+"/publish", policyPublishBody(t, reverted)), 422, "unsupported-workflow")
	requested := policyMutation(t, policyCall(s, token, "POST", "/api/policies/"+id+"/review", policyReviewBody(t, reverted, "request-changes"))).Policy
	if requested.Document.ReviewState != "changes-requested" {
		t.Fatal(requested)
	}
}

func TestPolicyVersionReuseAndIncompleteDrafts(t *testing.T) {
	s, token := policyTestServer(t)
	created := policyMutation(t, policyCall(s, token, "POST", "/api/policies", policyEditRequest{OperationID: policyOp(t), Title: "", Narrative: "", ArtifactVersion: "unfinished"})).Policy
	policyError(t, policyCall(s, token, "POST", "/api/policies/"+created.Document.ID+"/review", policyReviewBody(t, created, "approve")), 422, CodeBadRequest)
	requested := policyMutation(t, policyCall(s, token, "POST", "/api/policies/"+created.Document.ID+"/review", policyReviewBody(t, created, "request-changes"))).Policy
	edit := policyEditBody(t, requested)
	edit.Title = "Refunds"
	edit.Narrative = "Explicit narrative"
	incomplete := policyMutation(t, policyCall(s, token, "PUT", "/api/policies/"+created.Document.ID, edit)).Policy
	bad := policyError(t, policyCall(s, token, "POST", "/api/policies/"+created.Document.ID+"/review", policyReviewBody(t, incomplete, "approve")), 422, "invalid-version")
	if bad["outcome"] != "not-committed" {
		t.Fatal(bad)
	}
	edit = policyEditBody(t, incomplete)
	edit.ArtifactVersion = "1.0.0"
	valid := policyMutation(t, policyCall(s, token, "PUT", "/api/policies/"+created.Document.ID, edit)).Policy
	approved := policyApprove(t, s, token, valid)
	published := policyMutation(t, policyCall(s, token, "POST", "/api/policies/"+created.Document.ID+"/publish", policyPublishBody(t, approved))).Policy
	edit = policyEditBody(t, published)
	edit.Narrative = "Different content with reused version"
	changed := policyMutation(t, policyCall(s, token, "PUT", "/api/policies/"+created.Document.ID, edit)).Policy
	approved = policyApprove(t, s, token, changed)
	policyError(t, policyCall(s, token, "POST", "/api/policies/"+created.Document.ID+"/publish", policyPublishBody(t, approved)), 409, "version-conflict")
	if got := policyRead(t, s, token, created.Document.ID); len(got.Document.Publications) != 1 || got.Document.Publications[0].CandidateDigest == got.Document.Candidate.Digest {
		t.Fatal("published version overwritten", got)
	}
}

func TestPolicyOperationRecoveryBindsBodyRouteAndCurrentAuthorization(t *testing.T) {
	s, token := policyTestServer(t)
	create := policyEditRequest{OperationID: policyOp(t), Title: "Procurement", Narrative: "Retained policy", ArtifactVersion: "1.0.0"}
	created := policyMutation(t, policyCall(s, token, "POST", "/api/policies", create))
	recovered := policyMutation(t, policyCall(s, token, "GET", "/api/policy-operations/"+create.OperationID, nil))
	if recovered.Delivery != "recovered" || recovered.Policy.Document.ID != created.Policy.Document.ID {
		t.Fatal("lost create result could not be read", recovered)
	}
	id := created.Policy.Document.ID
	edit := policyEditBody(t, created.Policy)
	edit.Title = "Reviewed procurement"
	saved := policyMutation(t, policyCall(s, token, "PUT", "/api/policies/"+id, edit))
	approved := policyApprove(t, s, token, saved.Policy)
	again := policyMutation(t, policyCall(s, token, "PUT", "/api/policies/"+id, edit))
	if again.Delivery != "recovered" || again.Policy.Revision != saved.Policy.Revision || again.Policy.Revision == approved.Revision {
		t.Fatal("recovery used stale checks or substituted current data", again)
	}
	changed := edit
	changed.Narrative += " different"
	policyError(t, policyCall(s, token, "PUT", "/api/policies/"+id, changed), 409, "idempotency-conflict")
	other := policyCreate(t, s, token)
	policyError(t, policyCall(s, token, "PUT", "/api/policies/"+other.Document.ID, edit), 409, "idempotency-conflict")
	review := policyReviewBody(t, approved, "approve")
	review.OperationID = edit.OperationID
	policyError(t, policyCall(s, token, "POST", "/api/policies/"+id+"/review", review), 409, "idempotency-conflict")
	publish := policyPublishBody(t, approved)
	committed := policyMutation(t, policyCall(s, token, "POST", "/api/policies/"+id+"/publish", publish))
	s.cfg.JpackBin = filepath.Join(t.TempDir(), "missing-runtime")
	replayed := policyMutation(t, policyCall(s, token, "POST", "/api/policies/"+id+"/publish", publish))
	if replayed.Delivery != "recovered" || replayed.Receipt.PublicationID != committed.Receipt.PublicationID {
		t.Fatal("recovery called fresh unavailable validator", replayed)
	}
	policyError(t, policyCall(s, token, "GET", "/api/policy-operations/"+policyOp(t), nil), 404, CodeNotFound)
	s.sessions.revoke(token)
	policyError(t, policyCall(s, token, "GET", "/api/policy-operations/"+publish.OperationID, nil), 401, CodeUnauthorized)
	policyError(t, policyCall(s, token, "POST", "/api/policies/"+id+"/publish", publish), 401, CodeUnauthorized)
}

func TestPolicyAllRoutesRequireSessionAndRefuseActorSpoofing(t *testing.T) {
	s, token := policyTestServer(t)
	created := policyCreate(t, s, token)
	id := created.Document.ID
	paths := []struct {
		method, path string
		body         any
	}{
		{"GET", "/api/policies", nil}, {"GET", "/api/policies/" + id, nil}, {"GET", "/api/policies/" + id + "/revisions/" + created.Document.Candidate.RevisionID, nil}, {"GET", "/api/policy-operations/" + policyOp(t), nil},
		{"POST", "/api/policies", policyEditRequest{OperationID: policyOp(t)}}, {"PUT", "/api/policies/" + id, policyEditBody(t, created)}, {"POST", "/api/policies/" + id + "/review", policyReviewBody(t, created, "approve")}, {"POST", "/api/policies/" + id + "/publish", policyPublishBody(t, created)},
	}
	for _, path := range paths {
		for _, credential := range []string{"", testToken} {
			policyError(t, policyCall(s, credential, path.method, path.path, path.body), 401, CodeUnauthorized)
		}
	}
	spoof := map[string]any{"operationId": policyOp(t), "title": "Spoof", "narrative": "text", "artifactVersion": "1.0.0", "actor": map[string]string{"kind": "oidc", "subject": "another reviewer"}}
	policyError(t, policyCall(s, token, "POST", "/api/policies", spoof), 400, CodeBadRequest)
	r := httptest.NewRequest("GET", "/api/policies", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Origin", "https://foreign.example")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	policyError(t, w, 403, CodeForbidden)
	owner := signInOwner{Issuer: "https://identity.example", Subject: "verified-owner"}
	s.signIn.mu.Lock()
	s.signIn.active = &signInRecord{Version: 1, Owner: owner}
	s.signIn.mu.Unlock()
	verified, err := s.sessions.createVerified(owner)
	if err != nil {
		t.Fatal(err)
	}
	policyError(t, policyCall(s, token, "GET", "/api/policies", nil), 401, CodeUnauthorized)
	oidc := policyCreate(t, s, verified)
	if oidc.Document.Revisions[0].Actor != (policyActor{Kind: "oidc", Subject: owner.Subject, Issuer: owner.Issuer}) {
		t.Fatal("OIDC actor not derived from session", oidc)
	}
}

func TestPolicyUncertainReadbackRecoversCommittedReceipt(t *testing.T) {
	s, token := policyTestServer(t)
	created := policyCreate(t, s, token)
	edit := policyEditBody(t, created)
	edit.Narrative = "Durable despite lost acknowledgement"
	reads := 0
	testHookPrivateRead = func(name string) error {
		if name == "ledger.json" {
			reads++
			if reads == 3 {
				return errors.New("fixture read-back failure")
			}
		}
		return nil
	}
	t.Cleanup(func() { testHookPrivateRead = nil })
	failure := policyError(t, policyCall(s, token, "PUT", "/api/policies/"+created.Document.ID, edit), 503, "storage-unavailable")
	if failure["outcome"] != "unknown" {
		t.Fatal("uncertain commit was reported as refused", failure)
	}
	testHookPrivateRead = nil
	recovered := policyMutation(t, policyCall(s, token, "GET", "/api/policy-operations/"+edit.OperationID, nil))
	if recovered.Delivery != "recovered" || recovered.Policy.Document.Candidate.Narrative != edit.Narrative {
		t.Fatal("committed result unavailable", recovered)
	}
	again := policyMutation(t, policyCall(s, token, "PUT", "/api/policies/"+created.Document.ID, edit))
	if again.Delivery != "recovered" || again.Receipt.Revision != recovered.Receipt.Revision {
		t.Fatal("uncertain operation was repeated", again)
	}
	if current := policyRead(t, s, token, created.Document.ID); len(current.Document.Revisions) != 2 {
		t.Fatal("recovery duplicated revision", current)
	}
}

type policyFailingArtifacts struct {
	base         retainedArtifactRepository
	puts, failAt int
}

func (f *policyFailingArtifacts) Get(ctx context.Context, ref retainedArtifactRef) ([]byte, error) {
	return f.base.Get(ctx, ref)
}
func (f *policyFailingArtifacts) Put(ctx context.Context, raw []byte) (retainedArtifactRef, error) {
	f.puts++
	if f.puts == f.failAt {
		return retainedArtifactRef{}, errors.New("fixture failure before policy commit")
	}
	return f.base.Put(ctx, raw)
}

func TestPolicyPrecommitRetentionFailuresAreDefinitive(t *testing.T) {
	for _, failAt := range []int{1, 2} {
		t.Run(map[int]string{1: "candidate", 2: "receipt"}[failAt], func(t *testing.T) {
			s, token := policyTestServer(t)
			created := policyCreate(t, s, token)
			s.retainedArtifacts = &policyFailingArtifacts{base: s.artifactRepository(), failAt: failAt}
			edit := policyEditBody(t, created)
			edit.Narrative = "No reachable update until candidate and receipt are retained"
			failure := policyError(t, policyCall(s, token, "PUT", "/api/policies/"+created.Document.ID, edit), 503, "storage-unavailable")
			if failure["outcome"] != "not-committed" {
				t.Fatal("proven uncommitted write stranded recovery", failure)
			}
			if got := policyRead(t, s, token, created.Document.ID); got.Revision != created.Revision || len(got.Document.Revisions) != 1 {
				t.Fatal("failed retention exposed an incomplete revision", got)
			}
			policyError(t, policyCall(s, token, "GET", "/api/policy-operations/"+edit.OperationID, nil), 404, CodeNotFound)
			// A separate explicit save is safe after a definitive refusal. No retry
			// loop or read-only recovery endpoint dispatches this mutation.
			edit.OperationID = policyOp(t)
			saved := policyMutation(t, policyCall(s, token, "PUT", "/api/policies/"+created.Document.ID, edit)).Policy
			if len(saved.Document.Revisions) != 2 {
				t.Fatal(saved)
			}
		})
	}
}

func TestPolicyConcurrentPublishSerializesReceiptsAndVersion(t *testing.T) {
	for _, sameOperation := range []bool{true, false} {
		t.Run(map[bool]string{true: "same operation", false: "distinct operations"}[sameOperation], func(t *testing.T) {
			s, token := policyTestServer(t)
			approved := policyApprove(t, s, token, policyCreate(t, s, token))
			first, second := policyPublishBody(t, approved), policyPublishBody(t, approved)
			if sameOperation {
				second = first
			}
			var group sync.WaitGroup
			group.Add(2)
			responses := make(chan *httptest.ResponseRecorder, 2)
			for _, body := range []policyPublishRequest{first, second} {
				go func(body policyPublishRequest) {
					defer group.Done()
					responses <- policyCall(s, token, "POST", "/api/policies/"+approved.Document.ID+"/publish", body)
				}(body)
			}
			group.Wait()
			close(responses)
			committed, recovered, conflicts := 0, 0, 0
			for w := range responses {
				if w.Code == 200 {
					result := policyMutation(t, w)
					if result.Delivery == "committed" {
						committed++
					} else {
						recovered++
					}
				} else {
					policyError(t, w, 409, "revision-conflict")
					conflicts++
				}
			}
			if committed != 1 || sameOperation && recovered != 1 || !sameOperation && conflicts != 1 {
				t.Fatal(committed, recovered, conflicts)
			}
			if current := policyRead(t, s, token, approved.Document.ID); len(current.Document.Publications) != 1 {
				t.Fatal("concurrent publish duplicated immutable version", current)
			}
		})
	}
}

func TestPolicyRuntimeCheckDoesNotLockOrAuthorizeChangedCandidate(t *testing.T) {
	s, token := policyTestServer(t)
	created := policyCreate(t, s, token)
	r := httptest.NewRequest("POST", "/api/policies/"+created.Document.ID+"/review", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	held, _ := s.sessionOf(r)
	actor := policyActorOf(held)
	service := s.localPolicyService(r, actor)
	started, release := make(chan string, 1), make(chan struct{})
	service.validateVersion = func(ctx context.Context, base, version string) error {
		if base != version {
			return errors.New("unexpected comparison basis")
		}
		started <- version
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	body := policyReviewBody(t, created, "approve")
	command := policyCommand{Kind: "review", PolicyID: created.Document.ID, OperationID: body.OperationID, RequestDigest: digestOf([]byte("exact controlled review request")), Actor: actor, Review: &body}
	finished := make(chan error, 1)
	go func() { _, err := service.execute(t.Context(), command); finished <- err }()
	select {
	case version := <-started:
		if version != created.Document.Candidate.ArtifactVersion {
			t.Fatal("checked wrong version", version)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Runtime check did not start")
	}
	// Read-only recovery cannot transform a not-yet-completed attempt into a
	// new mutation. The original command remains active and can still finish.
	policyError(t, policyCall(s, token, "GET", "/api/policy-operations/"+body.OperationID, nil), 404, CodeNotFound)
	edit := policyEditBody(t, created)
	edit.Narrative = "Changed while version validation was running"
	saved := policyMutation(t, policyCall(s, token, "PUT", "/api/policies/"+created.Document.ID, edit)).Policy
	close(release)
	err := <-finished
	var failure *policyFailure
	if !errors.As(err, &failure) || failure.Code != "revision-conflict" {
		t.Fatal("checked old candidate authorized a changed one", err)
	}
	if got := policyRead(t, s, token, created.Document.ID); got.Revision != saved.Revision || got.Document.ReviewState != "unreviewed" {
		t.Fatal("stale review changed saved candidate", got)
	}
}

func TestPolicyRetainedContentIntegrityAndPrivateLedgerContainment(t *testing.T) {
	for _, damage := range []string{"retained candidate", "ledger symlink"} {
		t.Run(damage, func(t *testing.T) {
			s, token := policyTestServer(t)
			created := policyCreate(t, s, token)
			if damage == "retained candidate" {
				path := filepath.Join(s.projectDir, retainedArtifactDirectory, created.Document.Candidate.Content.SHA256)
				if err := os.WriteFile(path, []byte("Changed bytes"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				path := filepath.Join(s.projectDir, policyDirectory, "ledger.json")
				if err := os.Rename(path, path+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("ledger.json.original", path); err != nil {
					t.Fatal(err)
				}
			}
			result := policyError(t, policyCall(s, token, "GET", "/api/policies/"+created.Document.ID, nil), 503, "storage-unavailable")
			if strings.Contains(result["error"], s.projectDir) || strings.Contains(result["error"], "Changed bytes") {
				t.Fatal("storage detail leaked", result)
			}
		})
	}
}

func TestPolicyEscapedNarrativeRetainsAcceptedBytes(t *testing.T) {
	s, token := policyTestServer(t)
	request := policyEditRequest{OperationID: policyOp(t), Title: "Escaped content", Narrative: strings.Repeat("<", policyNarrativeLimit), ArtifactVersion: ""}
	created := policyMutation(t, policyCall(s, token, "POST", "/api/policies", request)).Policy
	if got := policyRead(t, s, token, created.Document.ID); got.Document.Candidate.Narrative != request.Narrative {
		t.Fatal("accepted narrative could not round-trip")
	}
}

func TestPolicyRealRuntimeVersionDelegation(t *testing.T) {
	bin := os.Getenv("JPACK_TEST_BIN")
	if bin == "" {
		t.Skip("set JPACK_TEST_BIN for real Runtime version delegation")
	}
	s, token := policyTestServer(t)
	s.cfg.JpackBin = bin
	initial := policyCreate(t, s, token)
	edit := policyEditBody(t, initial)
	edit.ArtifactVersion = "2.0.0"
	current := policyMutation(t, policyCall(s, token, "PUT", "/api/policies/"+initial.Document.ID, edit)).Policy
	current = policyApprove(t, s, token, current)
	current = policyMutation(t, policyCall(s, token, "POST", "/api/policies/"+current.Document.ID+"/publish", policyPublishBody(t, current))).Policy
	for _, version := range []string{"1.0.0", "2.0.0", "2.1.0"} {
		edit = policyEditBody(t, current)
		edit.ArtifactVersion = version
		edit.Narrative = "Revision for version " + version
		current = policyMutation(t, policyCall(s, token, "PUT", "/api/policies/"+current.Document.ID, edit)).Policy
		current = policyApprove(t, s, token, current)
		response := policyCall(s, token, "POST", "/api/policies/"+current.Document.ID+"/publish", policyPublishBody(t, current))
		if version == "2.1.0" {
			current = policyMutation(t, response).Policy
		} else {
			policyError(t, response, 409, "version-conflict")
		}
	}
	if len(current.Document.Publications) != 2 || current.Document.Publications[1].ArtifactVersion != "2.1.0" {
		t.Fatal("lower/equal versions changed publication history", current)
	}
	edit = policyEditBody(t, current)
	edit.ArtifactVersion = "1.1" + strings.Repeat("0", 251) + ".0"
	if len(edit.ArtifactVersion) != policyArtifactVersionLimit {
		t.Fatal("fixture length", len(edit.ArtifactVersion))
	}
	current = policyMutation(t, policyCall(s, token, "PUT", "/api/policies/"+current.Document.ID, edit)).Policy
	current = policyApprove(t, s, token, current)
	if current.Document.Candidate.ArtifactVersion != edit.ArtifactVersion {
		t.Fatal("Runtime-valid maximum-length version was narrowed")
	}
	edit = policyEditBody(t, current)
	edit.ArtifactVersion += "0"
	policyError(t, policyCall(s, token, "PUT", "/api/policies/"+current.Document.ID, edit), 400, CodeBadRequest)
	edit = policyEditBody(t, current)
	edit.ArtifactVersion = "unfinished"
	current = policyMutation(t, policyCall(s, token, "PUT", "/api/policies/"+current.Document.ID, edit)).Policy
	policyError(t, policyCall(s, token, "POST", "/api/policies/"+current.Document.ID+"/review", policyReviewBody(t, current, "approve")), 422, "invalid-version")
}
