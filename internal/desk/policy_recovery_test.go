package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func recoveryUpload(s *Server, token, path string, settings any, archive []byte) *httptest.ResponseRecorder {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, _ := w.CreateFormField("settings")
	raw, _ := json.Marshal(settings)
	part.Write(raw)
	part, _ = w.CreateFormFile("archive", "policy.zip")
	part.Write(archive)
	w.Close()
	r := httptest.NewRequest("POST", path, &body)
	r.Header.Set("Content-Type", w.FormDataContentType())
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	result := httptest.NewRecorder()
	s.ServeHTTP(result, r)
	return result
}
func recoveryArchiveBytes(t *testing.T, s *Server, token string) []byte {
	t.Helper()
	status := policyCall(s, token, "GET", "/api/policy-recovery", nil)
	if status.Code != 200 {
		t.Fatal(status.Code, status.Body.String())
	}
	var info policyRecoveryStatus
	json.Unmarshal(status.Body.Bytes(), &info)
	if !info.Available || info.Counts == nil {
		t.Fatal(status.Body.String())
	}
	r := httptest.NewRequest("GET", "/api/policy-recovery/archive", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("If-Match", info.Revision)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w.Header().Get("X-Policy-Archive-Digest") != digestOf(w.Body.Bytes()) {
		t.Fatal("archive digest differs")
	}
	return append([]byte{}, w.Body.Bytes()...)
}
func recoveryInspect(t *testing.T, s *Server, token string, archive []byte, destination string) policyRecoveryPreview {
	t.Helper()
	w := recoveryUpload(s, token, "/api/policy-recovery/inspect", map[string]string{"destination": destination}, archive)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var p policyRecoveryPreview
	if json.Unmarshal(w.Body.Bytes(), &p) != nil {
		t.Fatal("invalid preview")
	}
	return p
}
func recoveryBody(t *testing.T, p policyRecoveryPreview) policyRecoveryRequest {
	return policyRecoveryRequest{OperationID: policyOp(t), ProjectID: p.ProjectID, ArchiveDigest: p.ArchiveDigest, ArchiveBytes: p.ArchiveBytes, LedgerDigest: p.LedgerDigest, Destination: p.Destination, DestinationRevision: p.DestinationRevision}
}
func recoveryRestore(t *testing.T, s *Server, token string, archive []byte, q policyRecoveryRequest) policyRecoveryResult {
	t.Helper()
	w := recoveryUpload(s, token, "/api/policy-recovery/restore", q, archive)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var r policyRecoveryResult
	if json.Unmarshal(w.Body.Bytes(), &r) != nil {
		t.Fatal("invalid restore")
	}
	return r
}
func recoveryOpen(t *testing.T, s *Server, destination string) (*Server, string) {
	t.Helper()
	cfg := s.cfg
	cfg.ProjectDir = destination
	cfg.Root = nil
	cfg.Port = 8792
	fresh, err := New(cfg)
	if err != nil {
		t.Fatal("restored scaffold cannot open", err)
	}
	t.Cleanup(func() { fresh.Close() })
	fresh.policySourceCancel()
	<-fresh.policySourceDone
	fresh.policySourceCancel = nil
	return fresh, policyTestSession(t, fresh)
}

func TestPolicyRecoveryPreservesHistoryAndRequiresNewReview(t *testing.T) {
	s, token, p, service := lifecycleFixture(t)
	archive := recoveryArchiveBytes(t, s, token)
	originalCut := archiveTestCut(t, s)
	destination := filepath.Join(t.TempDir(), "restored")
	preview := recoveryInspect(t, s, token, archive, destination)
	q := recoveryBody(t, preview)
	result := recoveryRestore(t, s, token, archive, q)
	if result.Delivery != "committed" || result.Receipt.SnapshotID == "" || result.Receipt.Destination != destination || result.Authority != recoveryAuthority() {
		t.Fatal(result)
	}
	if _, err := os.Stat(s.projectDir); err != nil {
		t.Fatal("source was moved", err)
	}
	for _, name := range []string{"secrets", ".desk-private/data", "jpack-assistant.json", ".desk-private/signing"} {
		if _, err := os.Stat(filepath.Join(destination, name)); !os.IsNotExist(err) {
			t.Fatal("unexpected restored component", name, err)
		}
	}
	if got, _ := os.ReadFile(filepath.Join(destination, runtimeConfigName)); !bytes.Equal(got, []byte(gatedDeskConfig)) {
		t.Fatal("not a fresh empty host scaffold", string(got))
	}
	fresh, newToken := recoveryOpen(t, s, destination)
	live := policyRead(t, fresh, newToken, p.Document.ID)
	if live.Revision == p.Revision || live.Document.ReviewState != "unreviewed" || live.Document.CurrentReviewID != "" || live.CurrentCandidatePublished == nil || *live.CurrentCandidatePublished || live.Recovery == nil || !live.Recovery.FreshReviewRequired || !live.Recovery.CurrentnessUnknown {
		t.Fatal("restored approvals became live", live)
	}
	if !samePolicyStatusValue(live.Document.Reviews, p.Document.Reviews) || !samePolicyStatusValue(live.Document.Publications, p.Document.Publications) || !samePolicyStatusValue(live.Document.Candidate, p.Document.Candidate) {
		t.Fatal("rewrote historical data")
	}
	for _, ref := range originalCut.Refs {
		a, _ := s.artifactRepository().Get(context.Background(), ref)
		b, e := fresh.artifactRepository().Get(context.Background(), ref)
		if e != nil || !bytes.Equal(a, b) {
			t.Fatal("historical bytes changed", ref, e)
		}
	}
	policyError(t, policyCall(fresh, newToken, "PUT", "/api/policies/"+p.Document.ID, policyEditBody(t, p)), 409, "revision-conflict")
	oldActivation := lifecycleBody(t, service, p, "activate")
	oldActivation.BaseRevision = live.Revision
	w := policyCall(fresh, newToken, "POST", "/api/policies/"+p.Document.ID+"/lifecycle", oldActivation)
	policyError(t, w, 409, "restored-review-required")
	// A retained operation remains historical; it does not replace live CAS.
	for id := range originalCut.Ledger.Operations {
		w := policyCall(fresh, newToken, "GET", "/api/policy-operations/"+id, nil)
		r := policyMutation(t, w)
		if r.Delivery != "recovered" || r.Policy.Revision == live.Revision {
			t.Fatal("reinterpreted historical receipt")
		}
		break
	}
	edit := policyEditBody(t, live)
	edit.ArtifactVersion = "1.1.0"
	saved := policyMutation(t, policyCall(fresh, newToken, "PUT", "/api/policies/"+p.Document.ID, edit)).Policy
	approved := policyApprove(t, fresh, newToken, saved)
	if approved.Recovery == nil || approved.Recovery.FreshReviewRequired {
		t.Fatal("fresh review remains held")
	}
	published := policyMutation(t, policyCall(fresh, newToken, "POST", "/api/policies/"+p.Document.ID+"/publish", policyPublishBody(t, approved))).Policy
	if published.CurrentCandidatePublished == nil || !*published.CurrentCandidatePublished {
		t.Fatal("new publication not live")
	}
	// Historical local withdrawal stays available with the draft blob unavailable.
	if err := os.Remove(filepath.Join(destination, retainedArtifactDirectory, published.Document.Candidate.Content.SHA256)); err != nil {
		t.Fatal(err)
	}
	view := policyCall(fresh, newToken, "GET", "/api/policies/"+p.Document.ID+"/lifecycle", nil)
	var lifecycle policyLifecycleReply
	json.Unmarshal(view.Body.Bytes(), &lifecycle)
	withdraw := policyLifecycleRequest{OperationID: policyOp(t), Action: "withdraw", BaseRevision: published.Revision, BaseLifecycleRevision: lifecycle.Revision, PublicationID: p.Document.Publications[0].ID, Rationale: "Historical safety decision"}
	policyMutation(t, policyCall(fresh, newToken, "POST", "/api/policies/"+p.Document.ID+"/lifecycle", withdraw))
	// Restore-receipt recovery is metadata-only even after narrative loss.
	w = policyCall(s, token, "GET", "/api/policy-recovery/operations/"+q.OperationID, nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestPolicyRecoveryLostReplyCollisionAndScope(t *testing.T) {
	s, token := policyTestServer(t)
	policyCreate(t, s, token)
	archive := recoveryArchiveBytes(t, s, token)
	destination := filepath.Join(t.TempDir(), "restored")
	q := recoveryBody(t, recoveryInspect(t, s, token, archive, destination))
	testPolicyRecoveryAfterPublish = func() error { return errors.New("lost reply") }
	t.Cleanup(func() { testPolicyRecoveryAfterPublish = nil })
	w := recoveryUpload(s, token, "/api/policy-recovery/restore", q, archive)
	var failure map[string]any
	json.Unmarshal(w.Body.Bytes(), &failure)
	if w.Code != 409 || failure["outcome"] != "unknown" {
		t.Fatal(w.Code, w.Body.String())
	}
	testPolicyRecoveryAfterPublish = nil
	info, _ := os.Stat(destination)
	for i := 0; i < 2; i++ {
		w = policyCall(s, token, "GET", "/api/policy-recovery/operations/"+q.OperationID, nil)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	r := recoveryRestore(t, s, token, archive, q)
	if r.Delivery != "recovered" {
		t.Fatal(r)
	}
	after, _ := os.Stat(destination)
	if !os.SameFile(info, after) {
		t.Fatal("replayed restore")
	}
	changed := q
	changed.Destination += "-other"
	w = recoveryUpload(s, token, "/api/policy-recovery/restore", changed, archive)
	policyError(t, w, 409, "idempotency-conflict")
	w = policyCall(s, "", "GET", "/api/policy-recovery/operations/"+q.OperationID, nil)
	if w.Code == 200 {
		t.Fatal("logout recovered private operation")
	}
	fresh, newToken := recoveryOpen(t, s, destination)
	policyError(t, policyCall(fresh, newToken, "GET", "/api/policy-recovery/operations/"+q.OperationID, nil), 404, "not-found")
	second := q
	second.OperationID = policyOp(t)
	w = recoveryUpload(s, token, "/api/policy-recovery/restore", second, archive)
	policyError(t, w, 409, "destination-conflict")
	// No query can turn recovery into an arbitrary path read.
	policyError(t, policyCall(s, token, "GET", "/api/policy-recovery/operations/"+q.OperationID+"?destination=/etc", nil), 400, "bad-request")
}

func TestPolicyRecoveryCorruptCurrentStoreDoesNotBlockNewDestination(t *testing.T) {
	s, token := policyTestServer(t)
	policyCreate(t, s, token)
	archive := recoveryArchiveBytes(t, s, token)
	ledger := filepath.Join(s.projectDir, policyDirectory, "ledger.json")
	if err := os.WriteFile(ledger, []byte("not-json"), 0600); err != nil {
		t.Fatal(err)
	}
	w := policyCall(s, token, "GET", "/api/policy-recovery", nil)
	var status policyRecoveryStatus
	json.Unmarshal(w.Body.Bytes(), &status)
	if w.Code != 200 || status.Available || status.Counts != nil || !status.CanInspect || !status.CanRestore {
		t.Fatal(w.Code, w.Body.String())
	}
	destination := filepath.Join(t.TempDir(), "restored")
	q := recoveryBody(t, recoveryInspect(t, s, token, archive, destination))
	recoveryRestore(t, s, token, archive, q)
	if raw, _ := os.ReadFile(ledger); string(raw) != "not-json" {
		t.Fatal("replaced running corrupt store")
	}
}

func TestPolicyRecoveryHeldSourcesNeverDispatchArchivedWork(t *testing.T) {
	for _, observe := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending-binding-no-observations", true: "retained-observation-and-cleanup"}[observe], func(t *testing.T) {
			f := newPolicySourceFixture(t)
			f.bind(t)
			if observe {
				f.poll(t)
			}
			if observe {
				err := (filePolicyRepository{open: f.s.openPolicies}).Update(context.Background(), func(l *policyLedger) (bool, error) {
					src := l.Sources[f.policy.Document.ID]
					src.Cleanup = append(src.Cleanup, strings.Repeat("c", 64))
					src.CleanupOperations = append(src.CleanupOperations, strings.Repeat("d", 64))
					return true, nil
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			archive := recoveryArchiveBytes(t, f.s, f.token)
			destination := filepath.Join(t.TempDir(), "restored")
			q := recoveryBody(t, recoveryInspect(t, f.s, f.token, archive, destination))
			recoveryRestore(t, f.s, f.token, archive, q)
			fresh, token := recoveryOpen(t, f.s, destination)
			fresh.policySourceCanon = f.s.policySourceCanon
			fresh.policySourceCall = f.s.policySourceCall
			before := len(f.calls)
			fresh.pollPolicySources(context.Background(), strings.Repeat("f", 64))
			fresh.pollPolicySources(context.Background(), strings.Repeat("f", 64))
			if len(f.calls) != before {
				t.Fatal("dispatched archived grant or cleanup", f.calls[before:])
			}
			live := policyRead(t, fresh, token, f.policy.Document.ID)
			if live.Recovery == nil || !live.Recovery.SourceConsentRequired {
				t.Fatal(live.Recovery)
			}
			w := policyCall(fresh, token, "GET", "/api/policies/"+f.policy.Document.ID+"/source-sync", nil)
			var state policySourceSyncReply
			json.Unmarshal(w.Body.Bytes(), &state)
			if w.Code != 200 || state.State.State != "authorization-ended" || (!observe && state.Pending) {
				t.Fatal(w.Code, w.Body.String())
			}
			for _, action := range []string{"resume", "check-now", "pause", "revoke"} {
				request := policySourceSyncRequest{OperationID: policyOp(t), BaseRevision: live.Revision, BaseSourceRevision: state.Revision, Action: action}
				policyError(t, policyCall(fresh, token, "POST", "/api/policies/"+f.policy.Document.ID+"/source-sync", request), 409, "restored-source-consent-required")
			}
			if observe {
				// Local resolution works without Gateway or current source permission,
				// even if the archived binding has no matching current observation.
				fresh.policySourceCall = nil
				latest := state.State.Observations[len(state.State.Observations)-1]
				request := policySourceSyncRequest{OperationID: policyOp(t), BaseRevision: live.Revision, BaseSourceRevision: state.Revision, Action: "keep-current", ObservationID: latest.ID, CandidateDigest: live.Document.Candidate.Digest, WorkflowDigest: live.Document.WorkflowDigest}
				policyMutation(t, policyCall(fresh, token, "POST", "/api/policies/"+f.policy.Document.ID+"/source-sync", request))
				live = policyRead(t, fresh, token, f.policy.Document.ID)
			}
			approved := policyApprove(t, fresh, token, live)
			if approved.Recovery.FreshReviewRequired {
				t.Fatal("held source stranded local review")
			}
			fresh.policySourceCall = f.s.policySourceCall
			w = policyCall(fresh, token, "GET", "/api/policies/"+f.policy.Document.ID+"/source-sync", nil)
			json.Unmarshal(w.Body.Bytes(), &state)
			request := policySourceSyncRequest{OperationID: policyOp(t), BaseRevision: approved.Revision, BaseSourceRevision: state.Revision, Action: "bind", ResourceID: "file-A", SelectionContext: strings.Repeat("a", 64), Consent: "recurring-read-after-sign-out", FrequencyMinutes: 5}
			connected := policyMutation(t, policyCall(fresh, token, "POST", "/api/policies/"+f.policy.Document.ID+"/source-sync", request))
			if connected.Policy.Recovery.SourceConsentRequired {
				t.Fatal("fresh consent did not release source hold")
			}
			before = len(f.calls)
			fresh.pollPolicySources(context.Background(), strings.Repeat("f", 64))
			calls := f.calls[before:]
			if len(calls) != 2 || calls[0] != "source-bind" || calls[1] != "source-observe" {
				t.Fatal("fresh bind dispatched archived cleanup", calls)
			}
		})
	}
}

func TestPolicyRecoveryInspectRefusalAndConcurrentNoReplace(t *testing.T) {
	s, token := policyTestServer(t)
	policyCreate(t, s, token)
	archive := recoveryArchiveBytes(t, s, token)
	parent := t.TempDir()
	destination := filepath.Join(parent, "restored")
	p := recoveryInspect(t, s, token, archive, destination)
	q := recoveryBody(t, p)
	testPolicyRecoveryBeforePublish = func() error { return os.Mkdir(destination, 0700) }
	t.Cleanup(func() { testPolicyRecoveryBeforePublish = nil })
	w := recoveryUpload(s, token, "/api/policy-recovery/restore", q, archive)
	policyError(t, w, 409, "destination-conflict")
	testPolicyRecoveryBeforePublish = nil
	entries, _ := os.ReadDir(destination)
	if len(entries) != 0 {
		t.Fatal("overwrote concurrent destination")
	}
	if entries, _ = os.ReadDir(parent); len(entries) != 1 {
		t.Fatal("failed restore leaked staging", entries)
	}
	// Distinct reviewed operations racing one absent destination cannot merge.
	destination = filepath.Join(parent, "race")
	p = recoveryInspect(t, s, token, archive, destination)
	a, b := recoveryBody(t, p), recoveryBody(t, p)
	var wg sync.WaitGroup
	results := make(chan *httptest.ResponseRecorder, 2)
	for _, request := range []policyRecoveryRequest{a, b} {
		wg.Add(1)
		go func(q policyRecoveryRequest) {
			defer wg.Done()
			results <- recoveryUpload(s, token, "/api/policy-recovery/restore", q, archive)
		}(request)
	}
	wg.Wait()
	close(results)
	committed := 0
	for r := range results {
		if r.Code == 200 {
			committed++
		} else if r.Code != 409 && r.Code != 503 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	if committed != 1 {
		t.Fatal("expected one publication", committed)
	}
	// The existing chat backup stays a chat-only v1 archive.
	w = policyCall(s, token, "GET", "/api/storage/backup", nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte("policy-ledger.json")) {
		t.Fatal("changed chat ZIP scope")
	}
}

func TestPolicyRecoverySecondGenerationKeepsReceiptsAndFencesAgain(t *testing.T) {
	f := authorityFixture(t)
	f.configureStatus(t, "published")
	f.ownStatus(t, "externally-managed")
	f.statusDo(t, f.statusBody(t, "apply-external"))
	importBody := f.statusBody(t, "import-draft")
	importBody.Draft = &policySourceStatusDraft{Title: "Reviewed imported text", Narrative: "Administrator-reviewed import", ArtifactVersion: "1.1.0"}
	imported := f.statusDo(t, importBody)
	if imported.SourceStatus == nil {
		t.Fatal("missing import result")
	}
	f.policy = policyRead(t, f.s, f.token, f.policy.Document.ID)
	f.policy = policyApprove(t, f.s, f.token, f.policy)
	f.policy = policyMutation(t, policyCall(f.s, f.token, "POST", "/api/policies/"+f.policy.Document.ID+"/publish", policyPublishBody(t, f.policy))).Policy
	archive := recoveryArchiveBytes(t, f.s, f.token)
	first := filepath.Join(t.TempDir(), "first")
	recoveryRestore(t, f.s, f.token, archive, recoveryBody(t, recoveryInspect(t, f.s, f.token, archive, first)))
	fresh, token := recoveryOpen(t, f.s, first)
	fresh.policySourceCall = f.s.policySourceCall
	fresh.policySourceCanon = f.s.policySourceCanon
	f.s, f.token = fresh, token
	f.policy = policyRead(t, fresh, token, f.policy.Document.ID)
	f.keep(t)
	// Retain a post-restore held-source receipt, then a fresh consent receipt.
	f.policy = policyRead(t, fresh, token, f.policy.Document.ID)
	f.bind(t)
	f.policy = policyRead(t, fresh, token, f.policy.Document.ID)
	f.poll(t)
	edit := policyEditBody(t, f.policy)
	edit.ArtifactVersion = "2.0.0"
	f.policy = policyMutation(t, policyCall(fresh, token, "PUT", "/api/policies/"+f.policy.Document.ID, edit)).Policy
	f.keep(t)
	f.policy = policyApprove(t, fresh, token, f.policy)
	f.policy = policyMutation(t, policyCall(fresh, token, "POST", "/api/policies/"+f.policy.Document.ID+"/publish", policyPublishBody(t, f.policy))).Policy
	before := archiveTestCut(t, fresh)
	archive = recoveryArchiveBytes(t, fresh, token)
	second := filepath.Join(t.TempDir(), "second")
	out := recoveryRestore(t, fresh, token, archive, recoveryBody(t, recoveryInspect(t, fresh, token, archive, second)))
	newest, newToken := recoveryOpen(t, fresh, second)
	newest.policySourceCall = fresh.policySourceCall
	live := policyRead(t, newest, newToken, f.policy.Document.ID)
	if live.Recovery == nil || live.Recovery.SnapshotID != out.Receipt.SnapshotID || live.Recovery.SnapshotID == f.policy.Recovery.SnapshotID || !live.Recovery.SourceConsentRequired || !live.Recovery.FreshReviewRequired || *live.CurrentCandidatePublished {
		t.Fatal("second restore revived current authority", live.Recovery)
	}
	for _, ref := range before.Refs {
		a, _ := fresh.artifactRepository().Get(context.Background(), ref)
		b, err := newest.artifactRepository().Get(context.Background(), ref)
		if err != nil || !bytes.Equal(a, b) {
			t.Fatal("changed old/new receipt or original", ref, err)
		}
	}
	count := len(f.calls)
	newest.pollPolicySources(context.Background(), strings.Repeat("f", 64))
	if len(f.calls) != count {
		t.Fatal("second restore dispatched old consent")
	}
	if len(archiveTestCut(t, newest).Ledger.Recoveries) != 2 {
		t.Fatal("lost restoration history")
	}
}

type recoveryBlockingArtifacts struct {
	base             retainedArtifactRepository
	digest           string
	entered, release chan struct{}
	once             sync.Once
}

func (a *recoveryBlockingArtifacts) Get(ctx context.Context, ref retainedArtifactRef) ([]byte, error) {
	if ref.SHA256 == a.digest {
		a.once.Do(func() { close(a.entered); <-a.release })
	}
	return a.base.Get(ctx, ref)
}
func (a *recoveryBlockingArtifacts) Put(ctx context.Context, raw []byte) (retainedArtifactRef, error) {
	return a.base.Put(ctx, raw)
}
func TestPolicyRecoverySnapshotReleasesLedgerBeforeImmutableReads(t *testing.T) {
	s, token := policyTestServer(t)
	policy := policyCreate(t, s, token)
	blocked := &recoveryBlockingArtifacts{base: s.artifactRepository(), digest: policy.Document.Candidate.Content.SHA256, entered: make(chan struct{}), release: make(chan struct{})}
	s.retainedArtifacts = blocked
	type reply struct {
		cut policyArchiveCut
		err error
	}
	done := make(chan reply, 1)
	go func() { cut, err := s.policyRecoveryCut(context.Background()); done <- reply{cut, err} }()
	<-blocked.entered
	edit := policyEditBody(t, policy)
	edit.Narrative += " A new concurrent draft."
	written := make(chan *httptest.ResponseRecorder, 1)
	go func() { written <- policyCall(s, token, "PUT", "/api/policies/"+policy.Document.ID, edit) }()
	select {
	case result := <-written:
		if result.Code != 200 {
			t.Fatal(result.Code, result.Body.String())
		}
	case <-time.After(2 * time.Second):
		close(blocked.release)
		t.Fatal("archive retained metadata lock during immutable reads")
	}
	close(blocked.release)
	captured := <-done
	if captured.err != nil || captured.cut.Ledger.Policies[policy.Document.ID].HeadRevisionID != policy.Document.Candidate.RevisionID || captured.cut.Counts.Revisions != 1 {
		t.Fatal("mixed snapshot cut", captured.err)
	}
}

func TestPolicyRecoveryHTTPAuthorizationExportCASAndUnknownJournal(t *testing.T) {
	s, token := policyTestServer(t)
	policyCreate(t, s, token)
	for _, route := range []struct{ method, path string }{{"GET", "/api/policy-recovery"}, {"GET", "/api/policy-recovery/archive"}, {"POST", "/api/policy-recovery/inspect"}, {"POST", "/api/policy-recovery/restore"}, {"GET", "/api/policy-recovery/operations/" + policyOp(t)}} {
		if w := policyCall(s, "", route.method, route.path, nil); w.Code == 200 {
			t.Fatal("unauthenticated recovery", route)
		}
		r := httptest.NewRequest(route.method, route.path, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Origin", "https://untrusted.invalid")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code == 200 {
			t.Fatal("foreign origin recovery", route)
		}
	}
	policyError(t, policyCall(s, token, "GET", "/api/policy-recovery/archive", nil), 409, "recovery-conflict")
	archive := recoveryArchiveBytes(t, s, token)
	destination := filepath.Join(t.TempDir(), "restored")
	q := recoveryBody(t, recoveryInspect(t, s, token, archive, destination))
	recoveryRestore(t, s, token, archive, q)
	// Journal/marker corruption cannot turn an uncertain prior publication into
	// a definitely-not-committed answer that releases the browser journal.
	if err := writePrivateData(s.assistant.root, policyRecoveryJournalName, []byte("corrupt")); err != nil {
		t.Fatal(err)
	}
	w := policyCall(s, token, "GET", "/api/policy-recovery/operations/"+q.OperationID, nil)
	var failure map[string]any
	json.Unmarshal(w.Body.Bytes(), &failure)
	if w.Code != 409 || failure["outcome"] != "unknown" {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestPolicyRecoveryProtectsUnavailableConfiguredChatStore(t *testing.T) {
	s, token := policyTestServer(t)
	policyCreate(t, s, token)
	archive := recoveryArchiveBytes(t, s, token)
	chat := t.TempDir()
	if err := writePrivateData(s.assistant.root, dataLocationName, s.encodeDataLocation(dataLocation{Version: 1, Path: chat})); err != nil {
		t.Fatal(err)
	}
	// The configured location exists but is not a usable chat store.
	if _, err := s.openChatData(); err == nil {
		t.Fatal("fixture must be unavailable")
	}
	inside := filepath.Join(chat, "not-a-project")
	w := recoveryUpload(s, token, "/api/policy-recovery/inspect", map[string]string{"destination": inside}, archive)
	policyError(t, w, 400, "bad-request")
	outside := filepath.Join(t.TempDir(), "restored")
	q := recoveryBody(t, recoveryInspect(t, s, token, archive, outside))
	recoveryRestore(t, s, token, archive, q)
}
