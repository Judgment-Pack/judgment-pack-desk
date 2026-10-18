package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type authorityStoreFixture struct {
	s           *Server
	token       string
	policy      policyReply
	anchor      *policyAuthorityAnchor
	original    []byte
	resolutions int
}

func stopAuthorityStoreWorker(s *Server) {
	if s.policySourceCancel != nil {
		s.policySourceCancel()
		<-s.policySourceDone
		s.policySourceCancel = nil
	}
}
func bindAuthorityStoreServer(t *testing.T, legacy *Server) (*Server, string, *policyAuthorityAnchor, []byte, *int) {
	t.Helper()
	stopAuthorityStoreWorker(legacy)
	anchor, err := newPolicyAuthorityAnchor(filepath.Join(t.TempDir(), "authority"), policyAuthorityAnchorIdentity{InstallationID: "test-installation", WorkspaceID: digestOf([]byte(legacy.projectDir))})
	if err != nil {
		t.Fatal(err)
	}
	root, err := legacy.openPolicies(false)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	if err = lockPrivateData(lock, true); err != nil {
		t.Fatal(err)
	}
	raw, err := readPrivateData(root, "ledger.json", policyLedgerLimit)
	if err != nil {
		t.Fatal(err)
	}
	if err = anchor.Enroll(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	lock.Close()
	root.Close()
	cfg := legacy.cfg
	cfg.Root = nil
	cfg.ProjectDir = legacy.projectDir
	resolutions := new(int)
	cfg.policyAuthorityForProject = func(pinned *ProjectRoot) (*policyAuthorityAnchor, error) {
		*resolutions++
		if pinned.dir != cfg.ProjectDir {
			return nil, errors.New("unmapped workspace")
		}
		return anchor, nil
	}
	if err = legacy.Close(); err != nil {
		t.Fatal(err)
	}
	server, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	stopAuthorityStoreWorker(server)
	return server, policyTestSession(t, server), anchor, raw, resolutions
}
func newAuthorityStoreFixture(t *testing.T) *authorityStoreFixture {
	t.Helper()
	return newAuthorityStoreFixtureWithImport(t, &policyImport{Name: "source.txt", MediaType: "text/plain", Data: []byte("retained original bytes\n")})
}
func newAuthorityStoreFixtureWithImport(t *testing.T, source *policyImport) *authorityStoreFixture {
	t.Helper()
	legacy, token := policyTestServer(t)
	created := policyMutation(t, policyCall(legacy, token, "POST", "/api/policies", policyEditRequest{OperationID: policyOp(t), Title: "Authority fixture", Narrative: "Exact retained narrative", ArtifactVersion: "1.0.0", Import: source}))
	s, newToken, anchor, raw, resolutions := bindAuthorityStoreServer(t, legacy)
	return &authorityStoreFixture{s: s, token: newToken, policy: created.Policy, anchor: anchor, original: raw, resolutions: *resolutions}
}
func (f *authorityStoreFixture) raw(t *testing.T) []byte {
	t.Helper()
	root, err := f.s.openPolicies(false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	raw, err := readPrivateData(root, "ledger.json", policyLedgerLimit)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func (f *authorityStoreFixture) state(t *testing.T) policyAuthorityAnchorState {
	t.Helper()
	root, err := openPrivateDataRoot(f.anchor.directory, false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	state, err := f.anchor.read(root)
	if err != nil {
		t.Fatal(err)
	}
	return state
}
func (f *authorityStoreFixture) restoreLedger(t *testing.T, raw []byte) {
	t.Helper()
	root, err := f.s.openPolicies(false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	lock, err := root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err = lockPrivateData(lock, true); err != nil {
		t.Fatal(err)
	}
	if err = writePrivateData(root, "ledger.json", raw); err != nil {
		t.Fatal(err)
	}
}
func (f *authorityStoreFixture) hold(t *testing.T) {
	t.Helper()
	state := f.state(t)
	state.Intent = &policyAuthorityAnchorIntent{Generation: state.Generation + 1, OldDigest: state.LedgerDigest, NewDigest: strings.Repeat("f", 64), Nonce: strings.Repeat("a", 48)}
	root, err := openPrivateDataRoot(f.anchor.directory, false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	raw, _ := json.Marshal(state)
	if err = writePrivateData(root, policyAuthorityAnchorName, raw); err != nil {
		t.Fatal(err)
	}
}
func authorityStoreError(t *testing.T, err error, code string, uncertain bool) {
	t.Helper()
	var f *policyFailure
	if !errors.As(err, &f) || f.Code != code || f.Uncertain != uncertain {
		t.Fatalf("want %s uncertain=%v; got %#v", code, uncertain, err)
	}
}
func authorityStoreCall(s *Server, token string, ctx context.Context, method, path string, body any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, bytes.NewReader(raw)).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func TestPolicyAuthorityStoreFactoryCurrentAndVerifiedDomainErrors(t *testing.T) {
	f := newAuthorityStoreFixture(t)
	if f.resolutions != 1 || f.s.policyAuthorityAnchor != f.anchor {
		t.Fatal("binding not captured once")
	}
	f.s.cfg.policyAuthorityForProject = func(*ProjectRoot) (*policyAuthorityAnchor, error) {
		t.Fatal("request re-resolved authority")
		return nil, nil
	}
	for _, path := range []string{"/api/policies", "/api/policies/" + f.policy.Document.ID, "/api/policies/" + f.policy.Document.ID + "/lifecycle", "/api/policy-adoptions"} {
		if w := policyCall(f.s, f.token, "GET", path, nil); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if _, ok := f.s.policyHistoryReader().(policyRepository); ok {
		t.Fatal("history reader exposes current read/write")
	}
	generation := f.state(t).Generation
	if err := f.s.policyRepository().Update(context.Background(), func(*policyLedger) (bool, error) { return false, nil }); err != nil {
		t.Fatal(err)
	}
	known := policyRefusal(409, "revision-conflict", "Known exact domain conflict.")
	known.CurrentRevision = strings.Repeat("a", 64)
	err := f.s.policyRepository().Update(context.Background(), func(*policyLedger) (bool, error) { return false, known })
	if err != known {
		t.Fatal("verified domain failure was replaced", err)
	}
	err = f.s.policyRepository().Update(context.Background(), func(l *policyLedger) (bool, error) { l.SchemaVersion = 99; return true, nil })
	authorityStoreError(t, err, "storage-unavailable", false)
	if f.state(t).Generation != generation || !bytes.Equal(f.raw(t), f.original) {
		t.Fatal("no-op/error changed durable state")
	}
	stale := policyEditBody(t, f.policy)
	stale.BaseRevision = strings.Repeat("b", 64)
	conflict := policyError(t, policyCall(f.s, f.token, "PUT", "/api/policies/"+f.policy.Document.ID, stale), 409, "revision-conflict")
	if conflict["outcome"] != "not-committed" || f.state(t).Generation != generation {
		t.Fatal("verified domain conflict lost certainty", conflict)
	}
	phases := []policyAuthorityAnchorWritePhase{}
	f.anchor.persistForTest = func(phase policyAuthorityAnchorWritePhase, root *os.Root, raw []byte) error {
		phases = append(phases, phase)
		state, err := decodePolicyAuthorityAnchor(raw)
		if err != nil {
			t.Fatal(err)
		}
		ledger := f.raw(t)
		if phase == policyAuthorityAnchorIntentWrite {
			if !bytes.Equal(ledger, f.original) || state.Intent == nil {
				t.Fatal("intent not before ledger")
			}
		} else if phase == policyAuthorityAnchorFinalWrite {
			if digestOf(ledger) != state.LedgerDigest || state.Generation != generation+1 || state.Intent != nil {
				t.Fatal("final not after exact durable ledger")
			}
		}
		return writePrivateData(root, policyAuthorityAnchorName, raw)
	}
	q := policyEditBody(t, f.policy)
	q.Narrative = "A new exact retained narrative."
	changed := policyMutation(t, policyCall(f.s, f.token, "PUT", "/api/policies/"+f.policy.Document.ID, q))
	if changed.Delivery != "committed" || len(phases) != 2 || phases[0] != policyAuthorityAnchorIntentWrite || phases[1] != policyAuthorityAnchorFinalWrite || f.state(t).LedgerDigest != digestOf(f.raw(t)) {
		t.Fatal(changed, phases)
	}
}
func TestPolicyAuthorityStoreUncertainEdgesAndHistoricalRecovery(t *testing.T) {
	for _, edge := range []string{"intent-before", "intent-after", "ledger-readback", "final-before", "final-after", "cancel-intent", "cancel-final"} {
		t.Run(edge, func(t *testing.T) {
			f := newAuthorityStoreFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			phases := 0
			f.anchor.persistForTest = func(phase policyAuthorityAnchorWritePhase, root *os.Root, raw []byte) error {
				phases++
				if edge == "intent-before" && phase == policyAuthorityAnchorIntentWrite || edge == "final-before" && phase == policyAuthorityAnchorFinalWrite {
					return errors.New("controlled before persistence")
				}
				if err := writePrivateData(root, policyAuthorityAnchorName, raw); err != nil {
					return err
				}
				if edge == "intent-after" && phase == policyAuthorityAnchorIntentWrite || edge == "final-after" && phase == policyAuthorityAnchorFinalWrite {
					return errors.New("controlled after persistence")
				}
				if edge == "cancel-intent" && phase == policyAuthorityAnchorIntentWrite || edge == "cancel-final" && phase == policyAuthorityAnchorFinalWrite {
					cancel()
				}
				return nil
			}
			if edge == "ledger-readback" {
				testHookPrivateRead = func(name string) error {
					if name == "ledger.json" && f.state(t).Intent != nil {
						return errors.New("controlled durable ledger read-back failure")
					}
					return nil
				}
				t.Cleanup(func() { testHookPrivateRead = nil })
			}
			q := policyEditBody(t, f.policy)
			q.Narrative = "Durable attempted update " + edge
			failure := policyError(t, authorityStoreCall(f.s, f.token, ctx, "PUT", "/api/policies/"+f.policy.Document.ID, q), 503, "authority-unresolved")
			testHookPrivateRead = nil
			if failure["outcome"] != "unknown" {
				t.Fatal("uncertain write became no-commit", failure)
			}
			ledger := f.raw(t)
			afterLedger := !bytes.Equal(ledger, f.original)
			if edge == "final-before" || edge == "final-after" || edge == "ledger-readback" || edge == "cancel-final" {
				if !afterLedger {
					t.Fatal("fixture did not reach durable ledger")
				}
			} else if afterLedger {
				t.Fatal("ledger written before confirmed intent")
			}
			policyError(t, policyCall(f.s, f.token, "GET", "/api/policies/"+f.policy.Document.ID, nil), 503, "authority-unresolved")
			recovered := policyCall(f.s, f.token, "GET", "/api/policy-operations/"+q.OperationID, nil)
			if afterLedger {
				result := policyMutation(t, recovered)
				if result.Delivery != "recovered" || result.Policy.Document.Candidate.Narrative != q.Narrative {
					t.Fatal(result)
				}
			} else {
				policyError(t, recovered, 404, CodeNotFound)
			}
			cut, err := f.s.policyRecoveryCut(context.Background())
			if err != nil || !bytes.Equal(cut.Raw, ledger) {
				t.Fatal("history cut lost exact persisted state", err)
			}
			if phases > 2 {
				t.Fatal("extra repair persistence")
			}
			before := bytes.Clone(ledger)
			replay := policyError(t, policyCall(f.s, f.token, "PUT", "/api/policies/"+f.policy.Document.ID, q), 503, "authority-unresolved")
			if replay["outcome"] != "unknown" || !bytes.Equal(f.raw(t), before) {
				t.Fatal("held operation replayed", replay)
			}
		})
	}
}
func TestPolicyAuthorityStoreMissingRestoredAndCorruptNeverUnbind(t *testing.T) {
	for _, edge := range []string{"anchor", "ledger", "directory", "corrupt", "rollback"} {
		t.Run(edge, func(t *testing.T) {
			f := newAuthorityStoreFixture(t)
			q := policyEditBody(t, f.policy)
			q.Narrative = "Committed before rollback"
			result := policyMutation(t, policyCall(f.s, f.token, "PUT", "/api/policies/"+f.policy.Document.ID, q))
			switch edge {
			case "anchor":
				if err := os.RemoveAll(f.anchor.directory); err != nil {
					t.Fatal(err)
				}
			case "ledger":
				root, err := f.s.openPolicies(false)
				if err != nil {
					t.Fatal(err)
				}
				if err = root.Remove("ledger.json"); err != nil {
					t.Fatal(err)
				}
				root.Close()
			case "directory":
				if err := f.s.root.RemoveAll(policyDirectory); err != nil {
					t.Fatal(err)
				}
			case "corrupt":
				f.restoreLedger(t, []byte(`{"unknown":true}`))
			case "rollback":
				f.restoreLedger(t, f.original)
			}
			code, status := "authority-unavailable", 503
			if edge == "rollback" {
				code, status = "authority-conflict", 409
			}
			if edge == "corrupt" {
				code, status = "storage-integrity", 409
			}
			for _, method := range []string{"GET", "PUT"} {
				failure := policyError(t, policyCall(f.s, f.token, method, "/api/policies/"+f.policy.Document.ID, q), status, code)
				if failure["outcome"] != "unknown" {
					t.Fatal("unverified head cleared operation journal", failure)
				}
			}
			history := policyCall(f.s, f.token, "GET", "/api/policy-operations/"+q.OperationID, nil)
			if edge == "anchor" {
				recovered := policyMutation(t, history)
				if recovered.Delivery != "recovered" || recovered.Policy.Revision != result.Policy.Revision {
					t.Fatal(recovered)
				}
			} else if edge == "rollback" {
				policyError(t, history, 404, CodeNotFound)
			} else {
				if history.Code == 200 || history.Code == 404 {
					t.Fatal("missing/corrupt history synthesized", history.Code, history.Body.String())
				}
			}
			if edge == "ledger" || edge == "directory" {
				if _, err := f.s.policyRecoveryCut(context.Background()); err == nil {
					t.Fatal("missing bound history synthesized archive")
				}
				if _, err := os.Stat(filepath.Join(f.s.projectDir, policyDirectory, "ledger.json")); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("bound write created missing ledger", err)
				}
			}
		})
	}
}
func TestPolicyAuthorityStoreHistoryAuthAndCurrentSurfaceSeparation(t *testing.T) {
	f := newAuthorityStoreFixture(t)
	f.hold(t)
	var firstOperation string
	if err := f.s.policyHistoryReader().ReadHistory(context.Background(), func(l policyLedger) error {
		for key := range l.Operations {
			firstOperation = key
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/policies", "/api/policies/" + f.policy.Document.ID, "/api/policies/" + f.policy.Document.ID + "/lifecycle", "/api/policies/" + f.policy.Document.ID + "/authoring-source?revisionId=" + f.policy.Document.Candidate.RevisionID, "/api/policy-adoptions"} {
		failure := policyError(t, policyCall(f.s, f.token, "GET", path, nil), 503, "authority-unresolved")
		if failure["outcome"] != "unknown" {
			t.Fatal(failure)
		}
	}
	for _, path := range []string{"/api/policy-operations/" + firstOperation, "/api/policies/" + f.policy.Document.ID + "/revisions/" + f.policy.Document.Candidate.RevisionID, "/api/policies/" + f.policy.Document.ID + "/source", "/api/policy-recovery"} {
		w := policyCall(f.s, f.token, "GET", path, nil)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	var cut policyArchiveCut
	var err error
	cut, err = f.s.policyRecoveryCut(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/api/policy-recovery/archive", nil)
	r.Header.Set("Authorization", "Bearer "+f.token)
	r.Header.Set("If-Match", digestOf(cut.Raw))
	w := httptest.NewRecorder()
	f.s.ServeHTTP(w, r)
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/zip" {
		t.Fatal(w.Code, w.Body.String())
	}
	f.s.sessions.revoke(f.token)
	for _, path := range []string{"/api/policy-operations/" + firstOperation, "/api/policies/" + f.policy.Document.ID + "/source", "/api/policy-recovery"} {
		policyError(t, policyCall(f.s, f.token, "GET", path, nil), 401, CodeUnauthorized)
	}
}
func TestPolicyAuthorityStoreSourceWritersUseBoundFactory(t *testing.T) {
	source := newPolicySourceFixture(t)
	source.bind(t)
	oldCall, oldCanon := source.s.policySourceCall, source.s.policySourceCanon
	s, token, anchor, raw, _ := bindAuthorityStoreServer(t, source.s)
	source.s, source.token = s, token
	s.policySourceCall, s.policySourceCanon = oldCall, oldCanon
	f := &authorityStoreFixture{s: s, token: token, policy: source.policy, anchor: anchor, original: raw}
	s.pollPolicySources(context.Background(), strings.Repeat("f", 64))
	if len(source.calls) == 0 || f.state(t).Generation < 3 || f.state(t).LedgerDigest != digestOf(f.raw(t)) {
		t.Fatal("polling bypassed anchor", source.calls, f.state(t))
	}
	retained := f.raw(t)
	calls := len(source.calls)
	f.hold(t)
	s.pollPolicySources(context.Background(), strings.Repeat("f", 64))
	s.pollPolicySource(context.Background(), strings.Repeat("f", 64), source.policy.Document.ID)
	s.signIn.policySourceMu.Lock()
	s.endPolicySourceConnection(context.Background())
	s.signIn.policySourceMu.Unlock()
	if len(source.calls) != calls || !bytes.Equal(retained, f.raw(t)) {
		t.Fatal("background authority mutation bypassed held anchor")
	}
}
func TestPolicyAuthorityStoreLedgerLockIsUnknownBeforeCurrentCheck(t *testing.T) {
	f := newAuthorityStoreFixture(t)
	root, err := f.s.openPolicies(false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	lock, err := root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err = lockPrivateData(lock, true); err != nil {
		t.Fatal(err)
	}
	for _, write := range []bool{false, true} {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
		if write {
			err = f.s.policyRepository().Update(ctx, func(*policyLedger) (bool, error) { t.Fatal("busy ledger admitted callback"); return true, nil })
		} else {
			err = f.s.policyRepository().View(ctx, func(policyLedger) error { t.Fatal("canceled current reader admitted callback"); return nil })
		}
		cancel()
		authorityStoreError(t, err, "storage-busy", true)
	}

}
func TestPolicyAuthorityStoreProductionConstructorsStayCentral(t *testing.T) {
	rows, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	current, history := 0, 0
	for _, row := range rows {
		if row.IsDir() || !strings.HasSuffix(row.Name(), ".go") || strings.HasSuffix(row.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), row.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			kind, ok := literal.Type.(*ast.Ident)
			if !ok {
				return true
			}
			if kind.Name == "filePolicyRepository" || kind.Name == "filePolicyHistoryReader" {
				if row.Name() != "policy_repository.go" {
					t.Errorf("production constructor bypasses factory: %s", row.Name())
				}
				if kind.Name == "filePolicyRepository" {
					current++
				} else {
					history++
				}
			}
			return true
		})
	}
	if current != 1 || history != 1 {
		t.Fatal("unexpected repository factory constructor count", current, history)
	}
}

func TestPolicyAuthorityStoreReadersWaitWithoutAnchorBusy(t *testing.T) {
	f := newAuthorityStoreFixture(t)
	entered, release, secondStarted := make(chan struct{}), make(chan struct{}), make(chan struct{})
	first, second := make(chan error, 1), make(chan error, 1)
	go func() {
		first <- f.s.policyRepository().View(context.Background(), func(policyLedger) error { close(entered); <-release; return nil })
	}()
	<-entered
	go func() {
		close(secondStarted)
		second <- f.s.policyRepository().View(context.Background(), func(policyLedger) error { return nil })
	}()
	<-secondStarted
	select {
	case err := <-second:
		t.Fatal("second current reader did not wait", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal("same-workspace reader returned busy", err)
	}
	if f.state(t).Generation != 1 {
		t.Fatal("protected reads wrote metadata")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	err := f.s.policyHistoryReader().ReadHistory(canceled, func(policyLedger) error { t.Fatal("canceled history admitted callback"); return nil })
	authorityStoreError(t, err, "storage-unavailable", false)
	if strings.Contains(err.Error(), "committed") {
		t.Fatal("history cancellation claimed a write outcome", err)
	}

}
func TestPolicyAuthorityStoreResolverIsPerPinnedWorkspace(t *testing.T) {
	legacy, token := policyTestServer(t)
	_ = policyCreate(t, legacy, token)
	stopAuthorityStoreWorker(legacy)
	firstPath := legacy.projectDir
	secondPath := t.TempDir()
	knownLegacyPath := t.TempDir()
	first, err := newPolicyAuthorityAnchor(filepath.Join(t.TempDir(), "first"), policyAuthorityAnchorIdentity{InstallationID: "installation", WorkspaceID: "first"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := newPolicyAuthorityAnchor(filepath.Join(t.TempDir(), "second"), policyAuthorityAnchorIdentity{InstallationID: "installation", WorkspaceID: "second"})
	if err != nil {
		t.Fatal(err)
	}
	calls := map[string]int{}
	cfg := legacy.cfg
	cfg.Root = nil
	cfg.ProjectDir = firstPath
	cfg.policyAuthorityForProject = func(root *ProjectRoot) (*policyAuthorityAnchor, error) {
		calls[root.dir]++
		switch root.dir {
		case firstPath:
			return first, nil
		case secondPath:
			return second, nil
		case knownLegacyPath:
			return nil, nil
		}
		return nil, errors.New("secret unmapped path")
	}
	legacy.Close()
	primary, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { primary.Close() })
	stopAuthorityStoreWorker(primary)
	childCfg := cfg
	childCfg.parent = primary
	childCfg.ProjectDir = secondPath
	child, err := New(childCfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { child.Close() })
	stopAuthorityStoreWorker(child)
	if primary.policyAuthorityAnchor != first || child.policyAuthorityAnchor != second || calls[firstPath] != 1 || calls[secondPath] != 1 {
		t.Fatal("resolver inherited parent anchor", calls)
	}
	legacyCfg := childCfg
	legacyCfg.ProjectDir = knownLegacyPath
	knownLegacy, err := New(legacyCfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { knownLegacy.Close() })
	stopAuthorityStoreWorker(knownLegacy)
	if knownLegacy.policyAuthorityAnchor != nil || calls[knownLegacyPath] != 1 {
		t.Fatal("known legacy workspace borrowed anchor")
	}
	if err := knownLegacy.policyRepository().View(context.Background(), func(l policyLedger) error {
		if len(l.Policies) != 0 {
			t.Fatal("legacy empty store changed")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rejected := childCfg
	rejected.ProjectDir = t.TempDir()
	server, err := New(rejected)
	if server != nil || err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("resolver failure fell back or leaked", server, err)
	}
	if calls[rejected.ProjectDir] != 1 {
		t.Fatal("resolver not called exactly once")
	}
}

type authorityStoreRevokingArtifacts struct {
	base    retainedArtifactRepository
	trigger string
	once    sync.Once
	revoke  func()
}

func (a *authorityStoreRevokingArtifacts) Get(ctx context.Context, ref retainedArtifactRef) ([]byte, error) {
	raw, err := a.base.Get(ctx, ref)
	if err == nil && ref.SHA256 == a.trigger {
		a.once.Do(a.revoke)
	}
	return raw, err
}
func (a *authorityStoreRevokingArtifacts) Put(ctx context.Context, raw []byte) (retainedArtifactRef, error) {
	return a.base.Put(ctx, raw)
}
func TestPolicyAuthorityStoreHistoricalReadsRecheckAuthorization(t *testing.T) {
	for _, kind := range []string{"operation", "archive", "revision", "source", "source-without-original"} {
		t.Run(kind, func(t *testing.T) {
			var f *authorityStoreFixture
			if kind == "source-without-original" {
				f = newAuthorityStoreFixtureWithImport(t, nil)
			} else {
				f = newAuthorityStoreFixture(t)
			}
			f.hold(t)
			var operation policyStoredOperation
			if err := f.s.policyHistoryReader().ReadHistory(context.Background(), func(l policyLedger) error {
				for _, v := range l.Operations {
					operation = v
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			trigger := f.policy.Document.Candidate.Content.SHA256
			path := "/api/policy-recovery"
			switch kind {
			case "operation":
				path = "/api/policy-operations/" + operation.OperationID
				trigger = operation.Result.SHA256
			case "revision":
				path = "/api/policies/" + f.policy.Document.ID + "/revisions/" + f.policy.Document.Candidate.RevisionID
			case "source", "source-without-original":
				path = "/api/policies/" + f.policy.Document.ID + "/source"
			}
			f.s.retainedArtifacts = &authorityStoreRevokingArtifacts{base: f.s.artifactRepository(), trigger: trigger, revoke: func() { f.s.sessions.revoke(f.token) }}
			failure := policyCall(f.s, f.token, "GET", path, nil)
			policyError(t, failure, 401, CodeUnauthorized)
			if strings.Contains(failure.Body.String(), f.policy.Document.Candidate.Narrative) {
				t.Fatal("history disclosed after authorization ended")
			}
		})
	}
}

func TestPolicyAuthorityStoreHistoryStorageFailureRechecksAuthorization(t *testing.T) {
	authorized := true
	service := policyService{
		authorize: func() bool { return authorized },
		history: filePolicyHistoryReader{requireExisting: true, open: func(bool) (*os.Root, error) {
			authorized = false
			return nil, errors.New("history storage unavailable")
		}},
	}
	result, err := service.operation(context.Background(), "retained-operation")
	var failure *policyFailure
	if !errors.As(err, &failure) || failure.Status != 401 || failure.Code != CodeUnauthorized {
		t.Fatal("history storage error masked ended authorization", err)
	}
	if result.Delivery != "" || result.Receipt.OperationID != "" {
		t.Fatal("history returned a result after authorization ended")
	}
}
