package desk

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func runtimePressureOccurrences(p *storageStatusPressureSlot) uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.occurrences
}

func runtimePressureWorkspaceRequest(t *testing.T, s *Server, token, method, revision, body string) (*httptest.ResponseRecorder, conversationReply) {
	t.Helper()
	r := httptest.NewRequest(method, "/api/conversations", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	if revision != "" {
		r.Header.Set("If-Match", revision)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	var reply conversationReply
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
			t.Fatal(err)
		}
	}
	return w, reply
}

// Actual supported New and actual guarded workspace HTTP path; no fabricated
// enrollment, response fulfillment, global file hook, provider or disk exhaustion.
// The refusal occurs before the first stage write, and proves only that boundary.
func TestStorageRuntimeActualPersistenceBoundaries(t *testing.T) {
	requireStorageLine(t)
	s := ownerActualNew(t, ownerActualConfig(t))
	token := policyTestSession(t, s)
	var refuse atomic.Bool
	var calls atomic.Int64
	// Install before any request/handle under test; only atomic state changes.
	s.testBeforeStoragePersistence = func(target storagePersistenceTarget) error {
		calls.Add(1)
		if refuse.Load() {
			return &os.PathError{Op: "stage", Path: target.name, Err: syscall.ENOSPC}
		}
		return nil
	}
	w, initial := runtimePressureWorkspaceRequest(t, s, token, "GET", "", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	beforeBody := `{"version":1,"chats":[{"id":"chat-one","composer":"retained"}]}`
	w, saved := runtimePressureWorkspaceRequest(t, s, token, "PUT", initial.SHA256, beforeBody)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	owner := s.storageConstruction.owner
	baseline := runtimePressureOccurrences(&owner.statusPressure)
	beforeFiles := storageTestTree(t, s.defaultChatDataPath())
	refuse.Store(true)
	for i := 0; i < 2; i++ {
		w, _ = runtimePressureWorkspaceRequest(t, s, token, "PUT", saved.SHA256, `{"version":1,"chats":[{"id":"chat-one","composer":"unsaved"}]}`)
		if w.Code == 200 {
			t.Fatal("failed save acknowledged")
		}
	}
	if !reflect.DeepEqual(beforeFiles, storageTestTree(t, s.defaultChatDataPath())) {
		t.Fatal("pre-stage refusal changed old bytes or left a stage")
	}
	w, unchanged := runtimePressureWorkspaceRequest(t, s, token, "GET", "", "")
	if w.Code != 200 || unchanged.SHA256 != saved.SHA256 || !bytes.Equal(unchanged.Content, saved.Content) {
		t.Fatal("old checkpoint/revision lost", w.Code, unchanged)
	}
	if runtimePressureOccurrences(&owner.statusPressure) != baseline+2 {
		t.Fatal("failed saves not recorded exactly once")
	}
	boundaryCalls := calls.Load()
	for _, tc := range []struct{ revision, body string }{
		{saved.SHA256, `{"version":9,"chats":[]}`},
		{"stale", beforeBody},
		{"", beforeBody},
	} {
		w, _ = runtimePressureWorkspaceRequest(t, s, token, "PUT", tc.revision, tc.body)
		if w.Code == 200 {
			t.Fatal("invalid/conflicting save accepted")
		}
	}
	w, _ = runtimePressureWorkspaceRequest(t, s, "invalid-session", "PUT", saved.SHA256, beforeBody)
	if w.Code != 401 || calls.Load() != boundaryCalls || runtimePressureOccurrences(&owner.statusPressure) != baseline+2 {
		t.Fatal("validation/conflict/auth refusal manufactured pressure or reached writer")
	}

	// Use the default factory and actual file adapter, not an injected journal.
	root, run := researchRunFixture(t)
	if err := root.Remove("record.json"); err != nil {
		t.Fatal(err)
	}
	journal := s.researchJournal(root, run.ID)
	if _, ok := journal.(runtimeFileResearchJournal); !ok {
		t.Fatal("default file boundary not exercised")
	}
	if err := journal.Create(t.Context(), run); !errors.Is(err, syscall.ENOSPC) {
		t.Fatal("create refusal", err)
	}
	if _, err := root.Lstat("record.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed create published bytes", err)
	}
	refuse.Store(false)
	if err := journal.Create(t.Context(), run); err != nil {
		t.Fatal(err)
	}
	old, err := readPrivateData(root, "record.json", inputResearchRecordLimit)
	if err != nil {
		t.Fatal(err)
	}
	refuse.Store(true)
	var changes atomic.Int64
	failed, err := journal.Update(t.Context(), func(next *inputResearchRun) error { changes.Add(1); next.Step++; return nil })
	if !errors.Is(err, syscall.ENOSPC) || failed.ID != "" || changes.Load() != 1 {
		t.Fatal("failed update replayed callback or acknowledged a snapshot", err, failed)
	}
	current, err := readPrivateData(root, "record.json", inputResearchRecordLimit)
	if err != nil || !bytes.Equal(old, current) {
		t.Fatal("failed update replaced journal", err)
	}
	if runtimePressureOccurrences(&owner.statusPressure) != baseline+4 {
		t.Fatal("journal operation not recorded exactly once")
	}
	boundaryCalls = calls.Load()
	if err := journal.Create(t.Context(), run); !errors.Is(err, os.ErrExist) {
		t.Fatal(err)
	}
	invalid := run
	invalid.ID = "invalid"
	if err := journal.Create(t.Context(), invalid); err == nil {
		t.Fatal("invalid journal accepted")
	}
	// Even an ENOSPC-shaped callback refusal is not a persistence failure.
	if _, err := journal.Update(t.Context(), func(*inputResearchRun) error { return syscall.ENOSPC }); !errors.Is(err, syscall.ENOSPC) {
		t.Fatal(err)
	}
	if _, err := journal.Read(t.Context()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != boundaryCalls || runtimePressureOccurrences(&owner.statusPressure) != baseline+4 {
		t.Fatal("nonwrite journal path generated pressure")
	}
	beforeStatus := storageTestTree(t, s.configDir)
	beforeJournal, err := readPrivateData(root, "record.json", inputResearchRecordLimit)
	if err != nil {
		t.Fatal(err)
	}
	var first *string
	for i := 0; i < 8; i++ {
		response, out := storageStatusTestRequest(t, s, "GET", "/api/storage/status", nil)
		if response.Code != 200 || out.Snapshot == nil || !out.Pressure.Active || !reflect.DeepEqual(out.Pressure.Codes, []string{storageStatusDiskFull}) {
			t.Fatal("real diagnostic lost coalesced pressure", response.Body.String())
		}
		if first != nil && *first != *out.Pressure.FirstSeenAt {
			t.Fatal("poll reset first occurrence")
		}
		first = out.Pressure.FirstSeenAt
	}
	afterJournal, err := readPrivateData(root, "record.json", inputResearchRecordLimit)
	if err != nil || !bytes.Equal(beforeJournal, afterJournal) || !reflect.DeepEqual(beforeStatus, storageTestTree(t, s.configDir)) || calls.Load() != boundaryCalls || runtimePressureOccurrences(&owner.statusPressure) != baseline+4 {
		t.Fatal("diagnostic recursively persisted, retried, or created an occurrence", err)
	}
}

// Real staging errors after validation are distinguished from nonwrite errors.
// Closing a fixture-owned root at its per-handle boundary needs no global hook
// and no host filesystem pressure. No postpublication failure is simulated here.
func TestStorageRuntimeFileJournalActualStageError(t *testing.T) {
	requireStorageLine(t)
	s := ownerActualNew(t, ownerActualConfig(t))
	owner := s.storageConstruction.owner
	for _, operation := range []string{storagePersistenceResearchCreate, storagePersistenceResearchUpdate} {
		t.Run(operation, func(t *testing.T) {
			root, run := researchRunFixture(t)
			if operation == storagePersistenceResearchCreate {
				if err := root.Remove("record.json"); err != nil {
					t.Fatal(err)
				}
			}
			journal := s.researchJournal(root, run.ID).(runtimeFileResearchJournal)
			journal.beforeStage = func(target storagePersistenceTarget) error {
				if target.operation != operation {
					t.Fatal("wrong boundary", target.operation)
				}
				return root.Close()
			}
			before := runtimePressureOccurrences(&owner.statusPressure)
			var err error
			if operation == storagePersistenceResearchCreate {
				err = journal.Create(t.Context(), run)
			} else {
				_, err = journal.Update(t.Context(), func(next *inputResearchRun) error { next.Step++; return nil })
			}
			if !errors.Is(err, os.ErrClosed) || runtimePressureOccurrences(&owner.statusPressure) != before+1 {
				t.Fatal("real staging failure not reported once", err)
			}
			// A later nonwrite read failure does not add an occurrence.
			_, _ = journal.Read(t.Context())
			if runtimePressureOccurrences(&owner.statusPressure) != before+1 {
				t.Fatal("read created pressure")
			}
		})
	}
}

// A handwritten memory-only scope fixture tests routing/lifetime mechanics. It
// is not an enrollment, status endpoint or supported-New acceptance fixture.
func runtimePressureScopeFixture() (*Server, *storageServerEnrollment, *storageBudgetOwner) {
	s := &Server{configDir: "synthetic-scope", writes: &sync.Mutex{}}
	o := &storageBudgetOwner{}
	o.self = o
	cell := &storageInstallation{}
	cell.self = cell
	ref := &storageInstallationRef{cell: cell}
	ref.self = ref
	c := &storageBudgetConstruction{server: s, owner: o, ref: ref}
	c.self = c
	e := &storageServerEnrollment{server: s, construction: c, ref: ref}
	e.self = e
	e.runtimePressure = &storageRuntimePressureScope{server: s, enrollment: e, owner: o, construction: c}
	s.storageEnrollment = e
	return s, e, o
}

func TestStorageRuntimeReporterScopeAndRetirement(t *testing.T) {
	s, e, owner := runtimePressureScopeFixture()
	r := s.runtimeStoragePressureReporter()
	other, _, foreign := runtimePressureScopeFixture()
	rebound := &Server{configDir: s.configDir, storageEnrollment: e}
	rebound.runtimeStoragePressureReporter().report(syscall.ENOSPC, false)
	rebound.runtimeStoragePressureReporter().retire()
	copied := &storageServerEnrollment{self: e, server: s, construction: e.construction, ref: e.ref, runtimePressure: e.runtimePressure}
	s.storageEnrollment = copied
	s.runtimeStoragePressureReporter().report(syscall.ENOSPC, false)
	r.report(syscall.ENOSPC, false)
	s.storageEnrollment = other.storageEnrollment
	r.report(syscall.ENOSPC, false)
	s.storageEnrollment = e
	originalOwner := e.construction.owner
	e.construction.owner = foreign
	r.report(syscall.ENOSPC, false)
	e.construction.owner = originalOwner
	if runtimePressureOccurrences(&owner.statusPressure) != 0 || runtimePressureOccurrences(&foreign.statusPressure) != 0 {
		t.Fatal("copied/replaced/rebound scope acquired routing authority")
	}
	// Reporting must complete even when domain/server/enrollment/owner locks
	// are held. In particular it cannot add an owner lock beneath a writer.
	s.writes.Lock()
	s.mu.Lock()
	e.mu.Lock()
	owner.mu.Lock()
	done := make(chan struct{})
	go func() { r.report(syscall.ENOSPC, false); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("runtime reporter acquired a writer/server/enrollment/owner mutex")
	}
	owner.mu.Unlock()
	e.mu.Unlock()
	s.mu.Unlock()
	s.writes.Unlock()
	<-done
	if runtimePressureOccurrences(&owner.statusPressure) != 1 || runtimePressureOccurrences(&foreign.statusPressure) != 0 {
		t.Fatal("wrong original owner")
	}
	r.report(errors.New("opaque verification failure"), true)
	if !reflect.DeepEqual(owner.statusPressure.view().Codes, []string{storageStatusDiskFull, storageStatusUncertain}) {
		t.Fatal("postpublication report escaped closed catalogue")
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Go(func() { r.report(syscall.ENOSPC, false) })
	}
	wg.Go(r.retire)
	wg.Wait()
	before := runtimePressureOccurrences(&owner.statusPressure)
	for i := 0; i < 32; i++ {
		r.report(syscall.ENOSPC, false)
	}
	if runtimePressureOccurrences(&owner.statusPressure) != before {
		t.Fatal("closed scope resurrected")
	}
	// Independently, owner retirement rejects a still-live child scope.
	child, _, childOwner := runtimePressureScopeFixture()
	childReporter := child.runtimeStoragePressureReporter()
	childOwner.statusPressure.retire()
	childReporter.report(syscall.ENOSPC, false)
	if runtimePressureOccurrences(&childOwner.statusPressure) != 0 || runtimePressureOccurrences(&child.storageStatusPressure) != 0 {
		t.Fatal("closed owner accepted a late report")
	}
}

func TestStorageRuntimeActualChildHookAndCloseIsolation(t *testing.T) {
	requireStorageLine(t)
	cfg, registry := homeFixture(t)
	cfg.CodexBin, cfg.DevMode, cfg.Port = "off", true, testPort
	infrastructureRequireNative(t, cfg.DeskConfigDir)
	// The startup desk is the parent itself; enroll one additional original
	// registry member before New, following the existing actual-child fixture.
	entry := registry.Desks[0]
	entry.ID, entry.Name, entry.Folder = strings.Repeat("d", 32), "Second", "second"
	entry.HistoryIdentity, entry.RunnerWorkspace = "/original/second", strings.Repeat("e", 64)
	project := filepath.Join(filepath.Dir(cfg.DeskConfigDir), entry.Folder)
	if err := os.MkdirAll(filepath.Join(project, ".desk-private", "data"), 0700); err != nil {
		t.Fatal(err)
	}
	privateFixture(t, filepath.Join(project, projectConfigName), `{"deskConfigVersion":1}`)
	privateFixture(t, filepath.Join(project, ".desk-private", dataLocationName), `{"version":1,"path":"data"}`)
	marker, err := json.Marshal(dataMarker{Version: 1, Owner: registry.Owner})
	if err != nil {
		t.Fatal(err)
	}
	privateFixture(t, filepath.Join(project, ".desk-private", "data", dataMarkerName), string(marker))
	registry.Desks = append(registry.Desks, entry)
	raw, err := json.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	privateFixture(t, filepath.Join(cfg.DeskConfigDir, deskHomeRegistryName), string(raw))
	parent := ownerActualNew(t, cfg)
	child := parent.desks[entry.ID]
	if child == nil || child.storageEnrollment == nil {
		t.Fatal("actual child absent")
	}
	var parentCalls, childCalls atomic.Int64
	parent.testBeforeStoragePersistence = func(storagePersistenceTarget) error { parentCalls.Add(1); return syscall.ENOSPC }
	child.testBeforeStoragePersistence = func(storagePersistenceTarget) error { childCalls.Add(1); return syscall.ENOSPC }
	root, run := researchRunFixture(t)
	if err := root.Remove("record.json"); err != nil {
		t.Fatal(err)
	}
	journal := child.researchJournal(root, run.ID)
	before := runtimePressureOccurrences(&parent.storageConstruction.owner.statusPressure)
	if err := journal.Create(t.Context(), run); !errors.Is(err, syscall.ENOSPC) {
		t.Fatal(err)
	}
	if parentCalls.Load() != 0 || childCalls.Load() != 1 || runtimePressureOccurrences(&parent.storageStatusPressure) != 0 || runtimePressureOccurrences(&child.storageStatusPressure) != 1 || runtimePressureOccurrences(&parent.storageConstruction.owner.statusPressure) != before+1 {
		t.Fatal("child hook/local hint escaped scope or original owner missed it")
	}
	if err := child.Close(); err != nil {
		t.Fatal(err)
	}
	if err := journal.Create(t.Context(), run); !errors.Is(err, syscall.ENOSPC) {
		t.Fatal(err)
	}
	if runtimePressureOccurrences(&parent.storageConstruction.owner.statusPressure) != before+1 {
		t.Fatal("closed child reported")
	}
	// Parent reporter is live after the child closes, and its status remains real.
	parentRoot, parentRun := researchRunFixture(t)
	if err := parentRoot.Remove("record.json"); err != nil {
		t.Fatal(err)
	}
	if err := parent.researchJournal(parentRoot, parentRun.ID).Create(t.Context(), parentRun); !errors.Is(err, syscall.ENOSPC) {
		t.Fatal(err)
	}
	w, out := storageStatusTestRequest(t, parent, "GET", "/api/storage/status", nil)
	if w.Code != 200 || out.Snapshot == nil || parentCalls.Load() != 1 || runtimePressureOccurrences(&parent.storageConstruction.owner.statusPressure) != before+2 {
		t.Fatal("child close revoked parent")
	}
	// A frozen handle reports concurrently with actual Close. It can refuse
	// the write without retaining or touching owner descriptors after retirement.
	frozen := parent.researchJournal(parentRoot, parentRun.ID)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Go(func() { _ = frozen.Create(t.Context(), parentRun) })
	}
	wg.Go(func() {
		if err := parent.Close(); err != nil {
			t.Error(err)
		}
	})
	wg.Wait()
	closedCount := runtimePressureOccurrences(&parent.storageConstruction.owner.statusPressure)
	_ = frozen.Create(t.Context(), parentRun)
	if runtimePressureOccurrences(&parent.storageConstruction.owner.statusPressure) != closedCount {
		t.Fatal("actual closed owner accepted report")
	}
}

// Successful verified publication sets the observation bit. This check does
// not inject or claim evidence for a postpublication uncertainty path.
func TestPrivateDataPublicationObservation(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	published, err := writePrivateDataCommit(root, "record.json", []byte(`{"version":1}`))
	if err != nil || !published {
		t.Fatal(published, err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "record.json"))
	if err != nil || string(raw) != `{"version":1}` {
		t.Fatal(string(raw), err)
	}
}
