//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package desk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

type recoveryStorageFixture struct {
	server    *Server
	owner     *storageBudgetOwner
	root      *storageRootBinding
	run       assistantRunRecord
	operation string
	header    http.Header
	config    string
}

// All production authority is installed by actual New before the producer runs.
// Fixtures seed ordinary existing bytes/selection/identity before first startup;
// they never inject an observer, bootstrap a second owner or attach a writer.
func recoveryStorageOriginalServer(t *testing.T, legacy bool) *Server {
	t.Helper()
	cfg := ownerActualConfig(t)
	cfg.JpackBin = assistantMappingBinary(t)
	assistantRunMust(t, os.Mkdir(cfg.DeskConfigDir, 0700))
	assistantRunMust(t, os.MkdirAll(filepath.Join(cfg.ProjectDir, retainedArtifactDirectory), 0700))
	project, err := os.Stat(cfg.ProjectDir)
	assistantRunMust(t, err)
	identity := identityRecord{ID: strings.Repeat("a", 64), Path: cfg.ProjectDir, Folder: identityKey(project)}
	assistantRunMust(t, os.WriteFile(filepath.Join(cfg.ProjectDir, startupIdentityDir, startupIdentityName), identity.line(), 0600))
	path := cfg.DeskConfigDir
	if !legacy {
		path = filepath.Join(cfg.DeskConfigDir, "data")
		assistantRunMust(t, os.Mkdir(path, 0700))
		marker := assistantChatJSON(dataMarker{Version: 1, Owner: digestOf([]byte(cfg.DeskConfigDir))})
		assistantRunMust(t, os.WriteFile(filepath.Join(path, dataMarkerName), marker, 0600))
	}
	assistantRunMust(t, os.WriteFile(filepath.Join(cfg.DeskConfigDir, dataLocationName), assistantChatJSON(dataLocation{Version: 1, Path: path}), 0600))
	s := ownerActualNew(t, cfg)
	if s.recoveryStorage == nil || s.recoveryStorage.owner != s.storageConstruction.owner || s.recoveryStorage.enrollment != s.storageEnrollment {
		t.Fatal("actual New did not attach recovery through its original enrollment")
	}
	return s
}
func recoveryStorageSetup(t *testing.T) recoveryStorageFixture {
	t.Helper()
	s := recoveryStorageOriginalServer(t, false)
	s, e, run, operation := recoveryStorageStoppedOn(t, s)
	return recoveryStorageEnroll(t, s, e, run, operation)
}
func recoveryStorageEnroll(t *testing.T, s *Server, e *assistantRunExecution, run assistantRunRecord, operation string) recoveryStorageFixture {
	t.Helper()
	w := s.recoveryStorage
	if w == nil || w.enrollment != s.storageEnrollment || w.owner != s.storageEnrollment.construction.owner || w.owner.censusEpoch == 0 {
		t.Fatal("fixture bypassed actual New/owner/census")
	}
	if _, ok := w.owner.observer.(*storageAdvisoryObserver); !ok {
		t.Fatal("synthetic observer cannot qualify actual recovery join")
	}
	return recoveryStorageFixture{s, w.owner, w.root, run, operation, e.request.Header.Clone(), s.configDir}
}
func (f recoveryStorageFixture) request(t *testing.T, id, revision string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", "/api/assistant-runs/"+f.run.ID+"/dialect-reads/"+f.operation+"/observe", strings.NewReader(`{"runRevision":"`+runRevisionForRecovery(f.run)+`","recoveryRevision":"`+revision+`","requestId":"`+id+`"}`))
	r.Header = f.header.Clone()
	answer := httptest.NewRecorder()
	f.server.ServeHTTP(answer, r)
	return answer
}
func (f recoveryStorageFixture) snapshot(t *testing.T) storagebudget.Snapshot {
	t.Helper()
	v, e := f.owner.ref.cell.q.Snapshot(t.Context())
	assistantRunMust(t, e)
	return v
}
func (f recoveryStorageFixture) sidecar(t *testing.T) (assistantDialectReadRecovery, []byte) {
	t.Helper()
	lock, e := f.server.privateDataLockContext(t.Context(), true)
	assistantRunMust(t, e)
	defer lock.Close()
	data, e := f.server.openChatData()
	assistantRunMust(t, e)
	defer data.root.Close()
	v, raw, e := readAssistantDialectReadRecoveryLocked(t.Context(), data.root, f.run)
	assistantRunMust(t, e)
	return v, raw
}

func TestDialectReadRecoveryStorageActualOwnerRestartAndSecondWrite(t *testing.T) {
	f := recoveryStorageSetup(t)
	first := f.request(t, "actual-new-first", "0")
	if first.Code != 200 {
		t.Fatal(first.Code, first.Body.String())
	}
	before := assistantChatJSON(f.run)
	cfg := f.server.cfg
	assistantRunMust(t, f.server.Close())
	again := ownerActualNew(t, cfg)
	w := again.recoveryStorage
	if w == nil || w.enrollment != again.storageEnrollment || w.owner != again.storageConstruction.owner {
		t.Fatal("second actual New did not attach original recovery owner")
	}
	f.server, f.owner, f.root = again, w.owner, w.root
	f.header.Set("Authorization", "Bearer "+policyTestSession(t, again))
	initial := f.snapshot(t)
	_, old := f.sidecar(t)
	second := f.request(t, "actual-new-second", "1")
	if second.Code != 200 {
		t.Fatal(second.Code, second.Body.String())
	}
	saved, raw := f.sidecar(t)
	if saved.Revision != 2 || saved.Records[0].LatestObservation.Ordinal != 2 {
		t.Fatal("second original write did not replace exact sidecar")
	}
	after, err := again.assistantRuns().Read(t.Context(), f.run.Binding, f.run.ID)
	assistantRunMust(t, err)
	if !bytes.Equal(before, assistantChatJSON(after)) || after.DialectReads[0].Receipt != nil {
		t.Fatal("restart observation acknowledged/changed original run")
	}
	final := f.snapshot(t)
	if final.Installation[storagebudget.Control].Bytes != initial.Installation[storagebudget.Control].Bytes-uint64(len(old))+uint64(len(raw)) || final.Installation[storagebudget.Control].Files != initial.Installation[storagebudget.Control].Files || len(final.Tickets) != 0 || len(final.CapacityPromised) != 0 {
		t.Fatal("actual restart replacement lost exact usage/terminal accounting")
	}
	assistantRunMust(t, again.Close())
}

func TestDialectReadRecoveryStorageActualParentChildHTTP(t *testing.T) {
	cfg, entry := ownerReviewHome(t, true)
	cfg.JpackBin = assistantMappingBinary(t)
	for _, project := range []string{cfg.ProjectDir, filepath.Join(filepath.Dir(cfg.DeskConfigDir), entry.Folder)} {
		assistantRunMust(t, os.MkdirAll(filepath.Join(project, retainedArtifactDirectory), 0700))
	}
	parent := ownerActualNew(t, cfg)
	child := parent.desks[entry.ID]
	if parent.recoveryStorage == nil || child == nil || child.recoveryStorage == nil || child.storageEnrollment == nil || child.recoveryStorage.enrollment != child.storageEnrollment || child.recoveryStorage.owner != parent.recoveryStorage.owner || child.recoveryStorage.home != parent.recoveryStorage.home || child.recoveryStorage.root == parent.recoveryStorage.root {
		t.Fatal("actual child did not borrow original owner/home with its own enrolled history")
	}
	_, e, run, operation := recoveryStorageStoppedOn(t, child)
	f := recoveryStorageEnroll(t, child, e, run, operation)
	answer := f.request(t, "actual-child", "0")
	if answer.Code != 200 {
		t.Fatal(answer.Code, answer.Body.String())
	}
	data, err := child.openChatData()
	assistantRunMust(t, err)
	actual, err := data.root.Stat(".")
	assistantRunMust(t, err)
	original, err := f.root.root.Stat(".")
	assistantRunMust(t, err)
	assistantRunMust(t, data.root.Close())
	if !os.SameFile(actual, original) || !os.SameFile(f.root.domain.identity, child.recoveryStorage.config.domain.identity) {
		t.Fatal("HTTP bypassed actual original child/root/shared private domain")
	}
	_, parentExecution, parentRun, parentOperation := recoveryStorageStoppedOn(t, parent)
	pf := recoveryStorageEnroll(t, parent, parentExecution, parentRun, parentOperation)
	answer = pf.request(t, "actual-parent", "0")
	if answer.Code != 200 {
		t.Fatal(answer.Code, answer.Body.String())
	}
	assistantRunMust(t, child.Close())
	assistantRunMust(t, parent.storageConstruction.ref.cell.current())
	answer = pf.request(t, "parent-after-child-close", "1")
	if answer.Code != 200 {
		t.Fatal("legitimate child Close revoked parent recovery", answer.Code, answer.Body.String())
	}
}

func TestDialectReadRecoveryStorageActualAbsentOriginalHistoryUnavailable(t *testing.T) {
	cfg, entry := ownerReviewHome(t, false)
	parent := ownerActualNew(t, cfg)
	child := parent.desks[entry.ID]
	if child == nil || child.storageEnrollment == nil || child.recoveryStorage != nil {
		t.Fatal("absent original child private/history minted recovery authority")
	}
	if _, _, _, _, err := originalDialectReadRecoveryBinding(t.Context(), child); err != nil {
		t.Fatal("original absent history should remain unavailable", err)
	}
	if _, err := os.Lstat(filepath.Join(child.projectDir, ".desk-private")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("recovery attachment created private history", err)
	}
	assistantRunMust(t, child.Close())
	assistantRunMust(t, parent.storageConstruction.ref.cell.current())
}

func TestDialectReadRecoveryStorageActualExternalHistoryUnavailable(t *testing.T) {
	cfg := ownerActualConfig(t)
	cfg.JpackBin = assistantMappingBinary(t)
	assistantRunMust(t, os.Mkdir(cfg.DeskConfigDir, 0700))
	external := t.TempDir()
	assistantRunMust(t, os.WriteFile(filepath.Join(external, dataMarkerName), assistantChatJSON(dataMarker{Version: 1, Owner: digestOf([]byte(cfg.DeskConfigDir))}), 0600))
	assistantRunMust(t, os.WriteFile(filepath.Join(cfg.DeskConfigDir, dataLocationName), assistantChatJSON(dataLocation{Version: 1, Path: external}), 0600))
	s := ownerActualNew(t, cfg)
	if s.recoveryStorage != nil {
		t.Fatal("external path/marker installed original recovery writer")
	}
	_, e, run, operation := recoveryStorageStoppedOn(t, s)
	c := s.storageConstruction
	var root *storageRootBinding
	for _, r := range c.owner.roots {
		if r.source != nil && r.source.path == external {
			root = r
		}
	}
	if root == nil || root.source.parent != nil {
		t.Fatal("fixture did not retain the actual external source")
	}
	f := recoveryStorageFixture{s, c.owner, root, run, operation, e.request.Header.Clone(), s.configDir}
	state, tree := f.snapshot(t), storageTestTree(t, f.config)
	answer := f.request(t, "external-unavailable", "0")
	if answer.Code != 503 || len(storageWriterRecords(t, c.owner)) != 0 || !reflect.DeepEqual(state, f.snapshot(t)) || !reflect.DeepEqual(tree, storageTestTree(t, f.config)) {
		t.Fatal("unsupported external history reserved/wrote/settled or acknowledged", answer.Code, answer.Body.String())
	}
}

func TestDialectReadRecoveryStorageActualServerCloseCancelsPreparedQueue(t *testing.T) {
	recoveryStorageShutdownPreparedQueue(t, false)
}

func TestDialectReadRecoveryStorageActualWriterCloseCancelsPreparedQueue(t *testing.T) {
	recoveryStorageShutdownPreparedQueue(t, true)
}

func recoveryStorageShutdownPreparedQueue(t *testing.T, writerOnly bool) {
	t.Helper()
	f := recoveryStorageSetup(t)
	w := f.server.recoveryStorage
	held, err := f.server.privateDataLockContext(t.Context(), true)
	assistantRunMust(t, err)
	t.Cleanup(func() { assistantRunMust(t, held.Close()) })
	type contextKey struct{}
	ctx := context.WithValue(t.Context(), contextKey{}, w)
	queued := make(chan struct{}, 1)
	testHookPrivateDataTurnQueued = func(observed context.Context) {
		if observed.Value(contextKey{}) == w {
			select {
			case queued <- struct{}{}:
			default:
			}
		}
	}
	defer func() { testHookPrivateDataTurnQueued = nil }()
	before := storageTestTree(t, f.config)
	r := httptest.NewRequest("POST", "/api/assistant-runs/"+f.run.ID+"/dialect-reads/"+f.operation+"/observe", strings.NewReader(`{"runRevision":"`+runRevisionForRecovery(f.run)+`","recoveryRevision":"0","requestId":"server-close-queued"}`)).WithContext(ctx)
	r.Header = f.header.Clone()
	answer := httptest.NewRecorder()
	requestDone := make(chan struct{})
	go func() { defer close(requestDone); f.server.ServeHTTP(answer, r) }()
	select {
	case <-queued:
	case <-time.After(time.Second):
		t.Fatal("actual prepared request did not enter FIFO wait")
	}
	h := f.server.assistantRunService()
	if !h.mu.TryLock() {
		t.Fatal("queued prepared request held host mutex")
	}
	h.mu.Unlock()
	closeDone := make(chan error, 1)
	closeOriginal := f.server.Close
	if writerOnly {
		// Keep the authenticated session and service open. Server shutdown's
		// session revocation must not mask this writer's own cancellation.
		closeOriginal = w.Close
	}
	go func() { closeDone <- closeOriginal() }()
	select {
	case <-requestDone:
	case <-time.After(time.Second):
		t.Fatal("original Close did not cancel prepared FIFO wait")
	}
	select {
	case err := <-closeDone:
		assistantRunMust(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("original Close did not drain prepared custody")
	}
	if answer.Code == 200 || !reflect.DeepEqual(before, storageTestTree(t, f.config)) {
		t.Fatal("close/cancellation acknowledged or changed durable bytes")
	}
	w.mu.Lock()
	pending := len(w.operations)
	w.mu.Unlock()
	h.mu.Lock()
	pins := h.dialectReadRecoveryPins
	serviceClosed := h.closed
	h.mu.Unlock()
	if pending != 0 || pins != 0 {
		t.Fatal("Server.Close left prepared operation/pin custody")
	}
	if writerOnly {
		session, ok := f.server.sessions.lookup(strings.TrimPrefix(f.header.Get("Authorization"), "Bearer "))
		if serviceClosed || !ok || session.ctx.Err() != nil {
			t.Fatal("service/session shutdown masked writer cancellation")
		}
	}
	assistantRunMust(t, held.Close())
}

func TestDialectReadRecoveryStorageAuthenticatedHTTPPositive(t *testing.T) {
	f := recoveryStorageSetup(t)
	before := assistantChatJSON(f.run)
	initial := f.snapshot(t)
	var mu sync.Mutex
	var steps []string
	testHookDialectRecoveryStorageStep = func(step string) { mu.Lock(); steps = append(steps, step); mu.Unlock() }
	t.Cleanup(func() { testHookDialectRecoveryStorageStep = nil })
	answer := f.request(t, "real-b2-one", "0")
	if answer.Code != 200 {
		t.Fatalf("actual admitted POST: %d %s", answer.Code, answer.Body.String())
	}
	var view assistantDialectReadRecoveryView
	assistantRunMust(t, json.Unmarshal(answer.Body.Bytes(), &view))
	if view.RecoveryRevision != "1" || len(view.Reads) != 1 || view.Reads[0].BackingState != "present-valid" || view.Reads[0].Ordinal != "1" || view.Reads[0].StorageState != "unresolved" {
		t.Fatal("concrete HTTP observation lost original classification", answer.Body.String())
	}
	saved, raw := f.sidecar(t)
	if saved.Revision != 1 || len(saved.Records) != 1 || saved.Records[0].LatestObservation.RequestID != "real-b2-one" {
		t.Fatal("real sidecar not saved")
	}
	after, e := f.server.assistantRuns().Read(t.Context(), f.run.Binding, f.run.ID)
	assistantRunMust(t, e)
	if !bytes.Equal(before, assistantChatJSON(after)) || after.DialectReads[0].Receipt != nil {
		t.Fatal("recovery acknowledged or changed original read/run")
	}
	final := f.snapshot(t)
	if len(final.Tickets) != 0 || len(final.CapacityPromised) != 0 {
		t.Fatal("known completed storage operation not incorporated")
	}
	if final.Installation[storagebudget.Control].Bytes != initial.Installation[storagebudget.Control].Bytes+uint64(len(raw)) || final.Installation[storagebudget.Control].Files != initial.Installation[storagebudget.Control].Files+1 {
		t.Fatal("sidecar logical charge not exactly once")
	}
	rows := storageWriterRecords(t, f.owner)
	if len(rows) != 1 || rows[0].Phase != storageIntentRetired || rows[0].Target != assistantDialectReadRecoveryName(f.run.Binding.Project, f.run.ID) || rows[0].Stage == "" || rows[0].Token.ID == (storagebudget.ID{}) {
		t.Fatal("concrete writer did not retain real original token/journal")
	}
	sequence := []string{"home-prepared", "use-prepared", "capacity-prepared", "external-work-executor-acquired", "pin-bound", "private-acquired", "private-transferred", "reserved", "private-borrowed", "published", "private-borrow-released", "completed", "private-closed", "external-work-executor-closed", "capacity-closed", "use-closed", "home-closed"}
	at := 0
	for _, step := range steps {
		if at < len(sequence) && step == sequence[at] {
			at++
		}
	}
	if at != len(sequence) {
		t.Fatal("actual acquisition/release prefix", steps)
	}
	tree := storageTestTree(t, f.config)
	state := f.snapshot(t)
	answer = f.request(t, "real-b2-one", "0")
	if answer.Code != 200 {
		t.Fatalf("exact terminal recognition: %d %s", answer.Code, answer.Body.String())
	}
	if !reflect.DeepEqual(tree, storageTestTree(t, f.config)) || !reflect.DeepEqual(state, f.snapshot(t)) {
		t.Fatal("idempotent terminal recognition reserved, wrote or refunded again")
	}
	get := httptest.NewRequest("GET", "/api/assistant-runs/"+f.run.ID+"/dialect-reads", nil)
	get.Header = f.header.Clone()
	answer = httptest.NewRecorder()
	f.server.ServeHTTP(answer, get)
	if answer.Code != 200 || !bytes.Contains(answer.Body.Bytes(), []byte(`"recoveryRevision":"1"`)) {
		t.Fatal("canonical GET did not read concrete stored outcome")
	}
}
func recoveryStorageAssertEarlyReadDrained(t *testing.T, f recoveryStorageFixture, before map[string]string, state storagebudget.Snapshot, active uint64, records int) {
	t.Helper()
	w := f.server.recoveryStorage
	w.mu.Lock()
	operations := len(w.operations)
	w.mu.Unlock()
	h := f.server.assistantRunService()
	h.mu.Lock()
	pins := h.dialectReadRecoveryPins
	h.mu.Unlock()
	f.owner.mu.Lock()
	remaining := f.owner.active
	f.owner.mu.Unlock()
	if operations != 0 || pins != 0 || remaining != active {
		t.Fatal("early read failure leaked prepared/pin/owner lifetime")
	}
	if !reflect.DeepEqual(before, storageTestTree(t, f.config)) || !reflect.DeepEqual(state, f.snapshot(t)) || len(storageWriterRecords(t, f.owner)) != records {
		t.Fatal("early read failure changed bytes, reserved, published or settled")
	}
	// Verify the actual FIFO/private description and all prepared use/pass
	// custody have returned, in addition to checking local counters.
	lock, err := f.server.tryPrivateDataLockContext(t.Context(), true)
	assistantRunMust(t, err)
	assistantRunMust(t, lock.Close())
	use, err := f.owner.maintenanceUse()
	assistantRunMust(t, err)
	assistantRunMust(t, use.Close())
	saved, err := f.server.assistantRunsContext(t.Context()).Read(t.Context(), f.run.Binding, f.run.ID)
	assistantRunMust(t, err)
	if !reflect.DeepEqual(saved, f.run) {
		t.Fatal("early read cleanup changed the original run")
	}
}

func TestDialectReadRecoveryStorageEarlyReadCloseFailures(t *testing.T) {
	points := []struct {
		name, label string
		occurrence  int
	}{
		{"namespace-data", "assistant-run-namespace-data.close", 1},
		{"namespace-private", "assistant-run-namespace-private.close", 1},
		{"snapshot-data", "assistant-run-snapshot-data.close", 1},
		{"snapshot-private", "assistant-run-snapshot-private.close", 1},
		{"pin-namespace-data", "assistant-run-namespace-data.close", 2},
		{"pin-namespace-private", "assistant-run-namespace-private.close", 2},
	}
	for _, point := range points {
		t.Run(point.name, func(t *testing.T) {
			f := recoveryStorageSetup(t)
			before, state := storageTestTree(t, f.config), f.snapshot(t)
			f.owner.mu.Lock()
			active := f.owner.active
			f.owner.mu.Unlock()
			seen, fired, progressed := 0, false, false
			testHookDialectArtifactOwnerStep = func(step string) error {
				if step == point.label {
					seen++
					if seen == point.occurrence {
						fired = true
						return errAssistantDialectCompleteRead
					}
				}
				return nil
			}
			testHookDialectRecoveryStorageStep = func(step string) {
				if step == "reserved" || step == "published" || step == "completed" {
					progressed = true
				}
			}
			defer func() {
				testHookDialectArtifactOwnerStep = nil
				testHookDialectRecoveryStorageStep = nil
			}()
			answer := f.request(t, "early-read-close", "0")
			testHookDialectArtifactOwnerStep = nil
			testHookDialectRecoveryStorageStep = nil
			if !fired || answer.Code == 200 || progressed {
				t.Fatal("authenticated recovery advanced after early actual Close failure", point.name, answer.Code, answer.Body.String())
			}
			recoveryStorageAssertEarlyReadDrained(t, f, before, state, active, 0)
		})
	}
}

func TestDialectReadRecoveryStorageEarlyReadCloseRefusesKnownTerminal(t *testing.T) {
	f := recoveryStorageSetup(t)
	if answer := f.request(t, "known-terminal-read-close", "0"); answer.Code != 200 {
		t.Fatal(answer.Code, answer.Body.String())
	}
	before, state := storageTestTree(t, f.config), f.snapshot(t)
	f.owner.mu.Lock()
	active := f.owner.active
	f.owner.mu.Unlock()
	fired, progressed := false, false
	testHookDialectArtifactOwnerStep = func(step string) error {
		if step == "assistant-run-snapshot-private.close" {
			fired = true
			return errAssistantDialectCompleteRead
		}
		return nil
	}
	testHookDialectRecoveryStorageStep = func(step string) {
		if step == "reserved" || step == "published" || step == "completed" {
			progressed = true
		}
	}
	defer func() {
		testHookDialectArtifactOwnerStep = nil
		testHookDialectRecoveryStorageStep = nil
	}()
	answer := f.request(t, "known-terminal-read-close", "0")
	testHookDialectArtifactOwnerStep = nil
	testHookDialectRecoveryStorageStep = nil
	if !fired || progressed || answer.Code == 200 {
		t.Fatal("terminal recognition bypassed early read Close failure", answer.Code, answer.Body.String())
	}
	recoveryStorageAssertEarlyReadDrained(t, f, before, state, active, 1)
}

func TestDialectReadRecoveryStorageReadCustodyErrorsAndCancellation(t *testing.T) {
	points := []struct {
		name, source          string
		cancelAtClose, before bool
	}{
		{"namespace", "namespace", false, false},
		{"snapshot", "snapshot", false, false},
		{"snapshot-primary-error", "snapshot", false, false},
		{"prepared-pin", "prepared-pin", false, false},
		{"namespace-cancel", "namespace", true, false},
		{"snapshot-cancel", "snapshot", true, false},
		{"prepared-pin-cancel", "prepared-pin", true, false},
		{"namespace-already-cancelled", "namespace", false, true},
		{"snapshot-already-cancelled", "snapshot", false, true},
	}
	for _, point := range points {
		t.Run(point.name, func(t *testing.T) {
			f := recoveryStorageSetup(t)
			store := f.server.assistantRunsContext(t.Context())
			assistantRunMust(t, store.problem)
			before, state := storageTestTree(t, f.config), f.snapshot(t)
			f.owner.mu.Lock()
			active := f.owner.active
			f.owner.mu.Unlock()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var prepared *assistantDialectReadRecoveryStorageOperation
			if point.source == "prepared-pin" {
				var err error
				prepared, err = f.server.recoveryStorage.prepareOriginalDialectReadRecovery(ctx)
				assistantRunMust(t, err)
				defer func() { assistantRunMust(t, prepared.Close()) }()
			}
			if point.before {
				cancel()
			}
			prefix := "assistant-run-namespace-"
			if point.source == "snapshot" {
				prefix = "assistant-run-snapshot-"
			}
			rootFailure, privateFailure := errors.New("test read root Close failure"), errors.New("test read private Close failure")
			rootClosed, privateClosed := 0, 0
			testHookDialectArtifactOwnerStep = func(step string) error {
				switch step {
				case prefix + "data.close":
					rootClosed++
					if point.cancelAtClose {
						cancel()
					}
					return rootFailure
				case prefix + "private.close":
					privateClosed++
					return privateFailure
				}
				return nil
			}
			defer func() { testHookDialectArtifactOwnerStep = nil }()
			var err error
			switch point.source {
			case "namespace":
				selected := f.server.assistantRunsContext(ctx)
				err = selected.problem
				_, readErr := selected.Read(ctx, f.run.Binding, f.run.ID)
				if !point.before && (!errors.Is(readErr, rootFailure) || !errors.Is(readErr, privateFailure)) {
					t.Fatal("namespace Close errors disappeared at lookup", readErr)
				}
			case "snapshot":
				selected := store
				if point.name == "snapshot-primary-error" {
					selected.project = "foreign-namespace"
				}
				_, err = selected.Read(ctx, f.run.Binding, f.run.ID)
			case "prepared-pin":
				pin, pinErr := f.server.assistantRunService().pinDialectReadRecoveryPrepared(ctx, f.run, prepared)
				err = pinErr
				if pin != nil {
					_ = pin.Close()
					t.Fatal("namespace Close failure yielded original pin")
				}
			}
			testHookDialectArtifactOwnerStep = nil
			if prepared != nil {
				assistantRunMust(t, prepared.Close())
			}
			if point.before {
				if !errors.Is(err, context.Canceled) || rootClosed != 0 || privateClosed != 0 {
					t.Fatal("cancelled read acquired or fabricated custody", err)
				}
			} else if !errors.Is(err, rootFailure) || !errors.Is(err, privateFailure) || rootClosed != 1 || privateClosed != 1 || point.cancelAtClose && !errors.Is(err, context.Canceled) {
				t.Fatal("read lost joined Close/cancellation errors", err, rootClosed, privateClosed)
			}
			if point.name == "snapshot-primary-error" && !errors.Is(err, errAssistantRunScope) {
				t.Fatal("cleanup replaced the original read refusal", err)
			}
			recoveryStorageAssertEarlyReadDrained(t, f, before, state, active, 0)
		})
	}
}

func TestDialectReadRecoveryStoragePublicationUncertaintyNoReplay(t *testing.T) {
	for _, fault := range []storageBudgetFault{{Name: "recovery", Phase: "after-publication"}, {Name: "recovery", Phase: "before-completion"}, {Name: "stage", Phase: "after-close"}, {Name: "publish", Phase: "after-directory-close"}} {
		t.Run(fault.Name+fault.Phase, func(t *testing.T) {
			f := recoveryStorageSetup(t)
			f.owner.fault = &fault
			answer := f.request(t, "uncertain-one", "0")
			if answer.Code == 200 {
				t.Fatal("uncertain publication acknowledged")
			}
			state := f.snapshot(t)
			if len(state.Tickets) != 1 || len(state.CapacityPromised) == 0 || state.Tickets[0].State == storagebudget.Terminal {
				t.Fatal("pre-completion uncertainty lost original promise")
			}
			tree := storageTestTree(t, f.config)
			answer = f.request(t, "uncertain-one", "0")
			if answer.Code == 200 {
				t.Fatal("same request replayed or acknowledged unresolved storage")
			}
			if !reflect.DeepEqual(tree, storageTestTree(t, f.config)) || !reflect.DeepEqual(state, f.snapshot(t)) {
				t.Fatal("unresolved retry minted token, wrote or fabricated/released obligation")
			}
		})
	}
}
func TestDialectReadRecoveryStorageConsumerCloseBeforeCompletion(t *testing.T) {
	f := recoveryStorageSetup(t)
	fired := false
	testHookDialectArtifactOwnerStep = func(step string) error {
		if step == "read-recovery-data.close" && !fired {
			// First guard closes before reserve. Inject only after actual publication.
			rows := storageWriterRecords(t, f.owner)
			if len(rows) > 0 && rows[0].Phase == storageIntentPublishing {
				fired = true
				return errors.New("fixture consumer root Close")
			}
		}
		return nil
	}
	t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
	answer := f.request(t, "consumer-close", "0")
	testHookDialectArtifactOwnerStep = nil
	if !fired || answer.Code == 200 {
		t.Fatal("consumer read-only Close did not withhold completion")
	}
	state := f.snapshot(t)
	if len(state.Tickets) != 1 || len(state.CapacityPromised) == 0 {
		t.Fatal("consumer verification uncertainty lost promise")
	}
	before := storageTestTree(t, f.config)
	answer = f.request(t, "consumer-close", "0")
	if answer.Code == 200 || !reflect.DeepEqual(before, storageTestTree(t, f.config)) {
		t.Fatal("consumer Close uncertainty replayed or acknowledged")
	}
}
func TestDialectReadRecoveryStorageLateCustodyCloseRetainsTerminalTruth(t *testing.T) {
	f := recoveryStorageSetup(t)
	f.owner.fault = &storageBudgetFault{Name: "recovery", Phase: "custody-close"}
	answer := f.request(t, "late-close", "0")
	if answer.Code == 200 {
		t.Fatal("late custody Close acknowledged")
	}
	state := f.snapshot(t)
	if len(state.Tickets) != 0 || len(state.CapacityPromised) != 0 {
		t.Fatal("custody-only Close recreated a fictitious outstanding obligation")
	}
	before := storageTestTree(t, f.config)
	answer = f.request(t, "late-close", "0")
	if answer.Code != 200 {
		t.Fatalf("exact known terminal not recognized: %d %s", answer.Code, answer.Body.String())
	}
	if !reflect.DeepEqual(before, storageTestTree(t, f.config)) || !reflect.DeepEqual(state, f.snapshot(t)) {
		t.Fatal("late-close terminal recognition repeated effect/refund")
	}
}

// recoveryStorageSeedReusableHistory authors synthetic rejected prehistory, not
// completed Runtime operations. The original real terminal row and every Q byte
// remain unchanged. Only an ordinary subsequent writer may evict that terminal.
func recoveryStorageSeedReusableHistory(t *testing.T, f recoveryStorageFixture, original storageIntentRecord) {
	t.Helper()
	o := f.owner
	assistantRunMust(t, o.retain())
	defer func() { assistantRunMust(t, o.drop()) }()
	home, e := storageOpenOriginalControl(o.ref.cell, ".home-use.lock")
	assistantRunMust(t, e)
	defer func() { assistantRunMust(t, home.file.Close()) }()
	assistantRunMust(t, lockPrivateData(home.file, false))
	use, e := o.ref.acquire()
	assistantRunMust(t, e)
	defer func() { assistantRunMust(t, use.Close()) }()
	domain, e := o.domain(t.Context(), f.root, use)
	assistantRunMust(t, e)
	defer func() { assistantRunMust(t, domain.Close()) }()

	read := func() ([]byte, error) {
		file, e := storageOpenOriginalControl(o.ref.cell, storageIntentName)
		if e != nil {
			return nil, e
		}
		raw := make([]byte, storageIntentBytes)
		n, re := file.file.ReadAt(raw, 0)
		ce := file.file.Close()
		if re != nil || ce != nil || n != len(raw) {
			return nil, errors.Join(storagebudget.ErrUnknown, re, ce)
		}
		return raw, nil
	}
	current := func(ctx context.Context) error {
		if e := home.current(); e != nil {
			return e
		}
		return domain.current(ctx)
	}
	var captured storageIntentTable
	var status storagebudget.HostState
	var before []byte
	assistantRunMust(t, o.withControl(t.Context(), func(ctx context.Context, table *storageIntentTable) error {
		if e := current(ctx); e != nil {
			return e
		}
		var e error
		status, e = o.ref.cell.q.InspectHostState(ctx)
		if e != nil {
			return e
		}
		if e = o.validateIntentMappings(ctx, table, status); e != nil {
			return e
		}
		if status.Maintenance || original.Slot != 0 || !reflect.DeepEqual(table.slots[0].record, original) || !storageIntentRetiredProof(original, status) || status.Slots[original.Token.Slot].PersistedApplied != original.Token {
			return errors.New("history fixture requires the exact real retired row0")
		}
		for i := 1; i < len(table.slots); i++ {
			if table.slots[i].record.Phase != storageIntentEmpty {
				return errors.New("history fixture would overwrite existing history")
			}
		}
		for _, slot := range status.Slots {
			if storageUnretired(slot) {
				return errors.New("history fixture has an outstanding Q obligation")
			}
		}
		captured = *table
		before, e = read()
		return e
	}))
	quotaBefore := f.snapshot(t)

	// Codec construction is detached from the short control transaction. Each
	// filler has a legal Prepared->Rejected pair, zero tokens/evidence/deltas and
	// the actual enrollment/epoch. It neither asserts retirement nor grants a
	// current write capability. The rejection reason is synthetic fixture data.
	p := storageSingleFilePlan{Root: f.root, Target: "budget-history-test", Stage: ".budget-history-stage", Workflow: "bounded-history-fixture", Operation: "fixture-components", ExpectedAbsent: true, Bytes: []byte("0"), ObjectLimit: 32}
	pass, e := o.beginStorageCapacityPass(t.Context(), []storageCapacityEffectParent{{binding: f.root, root: f.root.root}})
	assistantRunMust(t, e)
	components, e := o.writerComponents(pass.context(t.Context()), p, f.root.root)
	assistantRunMust(t, pass.Close())
	assistantRunMust(t, e)
	proposed := captured
	image := append([]byte(nil), before...)
	for i := 1; i < len(proposed.slots); i++ {
		id := storagebudget.ID{0xf1, byte(i)}
		if id == original.OperationID {
			t.Fatal("fixture operation collides with original terminal")
		}
		for _, slot := range status.Slots {
			if slot.Ticket.Token.ID == id || slot.PersistedApplied.ID == id {
				t.Fatal("fixture operation collides with actual Q identity")
			}
		}
		r := storageIntentRecord{Slot: uint16(i), Generation: 1, Phase: storageIntentPrepared, OperationID: id, AdmissionEpoch: status.Epoch, NewLength: 1, Target: p.Target, Stage: p.Stage, Workflow: "bounded-history-fixture", Operation: fmt.Sprintf("synthetic-rejected-%d", i), Components: components, Changes: make([]storagebudget.Change, len(components))}
		for _, c := range components {
			root := o.roots[c.Root]
			r.RootIdentities = append(r.RootIdentities, root.row.Identity)
			r.FilesystemIdentities = append(r.FilesystemIdentities, o.filesystems[c.Filesystem].Identity)
			r.Locators = append(r.Locators, root.row.Locator)
		}
		metadata := storageMetadataDigest("desk-storage-single-file-cas-v1", r.OldIdentity, f.root.directory)
		r.CASBinding = sha256.Sum256(append(append(append([]byte(nil), metadata[:]...), id[:]...), []byte(r.Target+"\x00"+r.Stage)...))
		prepared, e := storageEncodeIntent(r, o.ref.cell.record.Installation, o.ref.cell.binding)
		assistantRunMust(t, e)
		rejected := storageNextIntent(storageIntentSlot{record: r, page: prepared, bank: 0}, storageIntentRejected)
		rejected.Outcome = storageIntentNotAdmitted
		rejected.Rejection = storageIntentBusy
		page, e := storageEncodeIntent(rejected, o.ref.cell.record.Installation, o.ref.cell.binding)
		assistantRunMust(t, e)
		selected, e := storageSelectIntent(prepared, page, i, o.ref.cell.record.Installation, o.ref.cell.binding)
		assistantRunMust(t, e)
		proposed.slots[i] = selected
		offset := i * 2 * storageIntentPage
		copy(image[offset:offset+storageIntentPage], prepared)
		copy(image[offset+storageIntentPage:offset+2*storageIntentPage], page)
	}

	// The real control gate protects the compare/write/Sync/Close/readback. Its
	// existing250ms deadline remains intact. Any uncertainty fails setup without
	// retrying; it is never classified as a successful eviction.
	assistantRunMust(t, o.withControl(t.Context(), func(ctx context.Context, table *storageIntentTable) error {
		if e := current(ctx); e != nil {
			return e
		}
		now, e := o.ref.cell.q.InspectHostState(ctx)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(now, status) || !reflect.DeepEqual(table.slots, captured.slots) {
			return errors.New("history fixture baseline changed")
		}
		if e = o.validateIntentMappings(ctx, &proposed, now); e != nil {
			return e
		}
		raw, e := read()
		if e != nil {
			return e
		}
		if !bytes.Equal(raw, before) {
			return errors.New("history fixture journal baseline changed")
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		file, e := storageOpenOriginalControl(o.ref.cell, storageIntentName)
		if e != nil {
			return e
		}
		// The original terminal's two banks are excluded from the write.
		offset := 2 * storageIntentPage
		n, we := file.file.WriteAt(image[offset:], int64(offset))
		if we != nil || n != len(image)-offset {
			return errors.Join(storagebudget.ErrUnknown, we, file.file.Close())
		}
		se := file.file.Sync()
		ce := file.file.Close()
		if se != nil || ce != nil {
			return errors.Join(storagebudget.ErrUnknown, se, ce)
		}
		back, e := read()
		if e != nil || !bytes.Equal(back, image) {
			return errors.Join(storagebudget.ErrUnknown, e)
		}
		if e = ctx.Err(); e != nil {
			return errors.Join(storagebudget.ErrUnknown, e)
		}
		return nil
	}))
	// Re-enter the unmodified loader/selector and mapping validator against the
	// actual durable file, rather than trusting the detached proposed table.
	assistantRunMust(t, o.withControl(t.Context(), func(ctx context.Context, table *storageIntentTable) error {
		if e := current(ctx); e != nil {
			return e
		}
		now, e := o.ref.cell.q.InspectHostState(ctx)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(now, status) || !reflect.DeepEqual(table.slots, proposed.slots) {
			return errors.New("durable history fixture did not preserve exact host/Q state")
		}
		return o.validateIntentMappings(ctx, table, now)
	}))
	if !reflect.DeepEqual(quotaBefore, f.snapshot(t)) {
		t.Fatal("synthetic prehistory changed actual quota state")
	}
}

func TestDialectReadRecoveryStorageRetiredHistoryEvictionUnavailable(t *testing.T) {
	f := recoveryStorageSetup(t)
	answer := f.request(t, "evicted-request", "0")
	if answer.Code != 200 {
		t.Fatal(answer.Code, answer.Body.String())
	}
	original := storageWriterRecords(t, f.owner)[0]
	// Authored reusable prehistory avoids requiring63 unrelated full writes to
	// succeed before the operation this test actually observes. It preserves the
	// original real terminal/Q state; only the real writer below evicts it.
	recoveryStorageSeedReusableHistory(t, f, original)
	p := storageSingleFilePlan{Root: f.root, Target: "budget-history-test", Stage: ".budget-history-stage", Workflow: "bounded-history-fixture", Operation: "evict-original", ExpectedAbsent: true, Bytes: []byte("0"), ObjectLimit: 32}
	a, e := f.owner.admitSingleFile(t.Context(), p)
	assistantRunMust(t, e)
	assistantRunMust(t, a.Write(t.Context()))
	assistantRunMust(t, a.Close())
	rows := storageWriterRecords(t, f.owner)
	if len(rows) != storagebudget.Slots {
		t.Fatal("fixture did not retain complete bounded host history")
	}
	for _, r := range rows {
		if r.OperationID == original.OperationID {
			t.Fatal("fixture did not evict exact original host history")
		}
	}
	status, e := f.owner.ref.cell.q.InspectHostState(t.Context())
	assistantRunMust(t, e)
	if status.Slots[original.Token.Slot].PersistedApplied == original.Token {
		t.Fatal("fixture did not reuse exact Q applied identity")
	}
	eviction := rows[0]
	if eviction.Slot != original.Slot || eviction.Phase != storageIntentRetired || eviction.Workflow != p.Workflow || eviction.Operation != p.Operation || eviction.Target != p.Target || !storageIntentRetiredProof(eviction, status) {
		t.Fatal("real eviction did not durably retire in the original host slot")
	}
	if eviction.Token.Slot != original.Token.Slot || eviction.Token.ID == original.Token.ID || eviction.Token.Sequence <= original.Token.Sequence || status.Slots[original.Token.Slot].PersistedApplied != eviction.Token || status.Slots[original.Token.Slot].PersistedNextSequence <= eviction.Token.Sequence {
		t.Fatal("real eviction did not supersede the exact original Q applied identity")
	}
	state := f.snapshot(t)
	tree := storageTestTree(t, f.config)
	answer = f.request(t, "evicted-request", "0")
	if answer.Code != 503 {
		t.Fatalf("evicted outcome falsely acknowledged: %d %s", answer.Code, answer.Body.String())
	}
	if !reflect.DeepEqual(tree, storageTestTree(t, f.config)) || !reflect.DeepEqual(state, f.snapshot(t)) {
		t.Fatal("missing history was converted to new effect or fabricated peak")
	}
}

// A legacy pointer is established before the first actual conversation/run or
// native read owner exists. The subsequent read and stop use real producers.
func recoveryStorageLegacyStopped(t *testing.T) (*Server, *assistantRunExecution, assistantRunRecord, string) {
	t.Helper()
	s := recoveryStorageOriginalServer(t, true)
	return recoveryStorageStoppedOn(t, s)
}

func recoveryStorageStoppedOn(t *testing.T, s *Server) (*Server, *assistantRunExecution, assistantRunRecord, string) {
	t.Helper()
	token := policyTestSession(t, s)
	connection := aiTestConnection(testAIAlpha, "Recovery legacy fixture", "http://127.0.0.1:1")
	connection.Assistant.Endpoint.Tools = []string{"validate", "experimental_evaluate", "get_schema"}
	raw, err := encodeAIRegistry(aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{connection}})
	assistantRunMust(t, err)
	assistantRunMust(t, s.assistant.writeConfigNamed(aiConnectionsFile, raw, nil))
	assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, assistantProfilePath), []byte(`{"profileVersion":1,"api":{"inherit":true}}`), 0600))
	origin, _ := endpointOrigin(connection.Assistant.Endpoint.URL)
	assistantRunMust(t, s.assistant.storeKeyNamed(connectionKeyName(&connection), storedKey{present: true, key: "host-fixture-key", origin: origin, kind: connection.Assistant.Endpoint.Kind}))
	chat := map[string]any{"id": "legacy-chat", "interaction": "work", "aiConnection": testAIAlpha, "model": "shared-model", "checkpoint": map[string]any{"state": map[string]any{"turns": []any{map[string]any{"id": "legacy-message", "role": "user", "kind": "message", "text": "Inspect original read", "at": "2026-10-20T00:00:00Z"}}}}}
	answer := policyWorkspaceCall(s, token, "PUT", "/api/conversations", "absent", map[string]any{"version": 1, "chats": []json.RawMessage{assistantChatJSON(chat)}})
	if answer.Code != 200 {
		t.Fatal(answer.Code, answer.Body.String())
	}
	savedChat := policyWorkspaceRead(t, s, token, "/api/conversations")
	var document conversationDocument
	assistantRunMust(t, json.Unmarshal(savedChat.Content, &document))
	entered := make(chan *assistantRunExecution, 1)
	checked := make(chan *assistantRunExecution, 1)
	s.assistantRunExecutor = &assistantProtocolExecutor{execute: func(ctx context.Context, e *assistantRunExecution) error {
		entered <- e
		<-ctx.Done()
		return ctx.Err()
	}}
	s.testAssistantRunMonitorChecked = func(ctx context.Context, e *assistantRunExecution) {
		select {
		case checked <- e:
		case <-ctx.Done():
			return
		}
		<-ctx.Done()
	}
	start := assistantRunStart{AdmissionID: "legacy-admission", Purpose: "chat", OwnerID: "legacy-chat", Revision: digestOf(document.Chats[0]), RequestID: "legacy-message"}
	view := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
	var e *assistantRunExecution
	select {
	case e = <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("legacy execution missing")
	}
	if e.record.ID != view.ID {
		t.Fatal("foreign legacy owner")
	}
	t.Cleanup(func() {
		e.cancel()
		select {
		case <-e.done:
		case <-time.After(3 * time.Second):
			t.Error("legacy owner did not drain")
		}
	})
	select {
	case actual := <-checked:
		if actual != e {
			t.Fatal("foreign monitor")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("legacy authorization barrier")
	}
	saved := assistantMappingSeed(t, s)
	e.canonicalMu.Lock()
	e.generation = 1
	e.lifecycle = "running"
	e.canonicalMu.Unlock()
	fired := false
	testHookDialectArtifactOwnerStep = func(step string) error {
		if step == "complete-read-raw.file-close" && !fired {
			fired = true
			return errAssistantDialectCompleteRead
		}
		return nil
	}
	result := invokeRecoveryFixtureRead(t, e, saved, "legacy-original")
	testHookDialectArtifactOwnerStep = nil
	if !fired || !result.Err() {
		t.Fatal("legacy original did not retain actual raw Close uncertainty")
	}
	assistantRunMust(t, e.writer.Stop(t.Context()))
	e.cancel()
	select {
	case <-e.done:
	case <-time.After(3 * time.Second):
		t.Fatal("legacy original not stopped")
	}
	run, err := s.assistantRuns().Read(t.Context(), e.record.Binding, e.record.ID)
	assistantRunMust(t, err)
	if len(run.DialectReads) != 1 || run.DialectReads[0].Receipt != nil {
		t.Fatal("legacy original altered")
	}
	return s, e, run, run.DialectReads[0].ReadOperationID
}
func TestDialectReadRecoveryStorageLegacyPrivateBorrow(t *testing.T) {
	s, e, run, op := recoveryStorageLegacyStopped(t)
	f := recoveryStorageEnroll(t, s, e, run, op)
	if f.root != f.server.recoveryStorage.config {
		t.Fatal("fixture did not share exact original private domain")
	}
	before := assistantChatJSON(run)
	answer := f.request(t, "legacy-b2", "0")
	if answer.Code != 200 {
		t.Fatal("recursive private EX was reacquired", answer.Code, answer.Body.String())
	}
	after, err := f.server.assistantRuns().Read(t.Context(), run.Binding, run.ID)
	assistantRunMust(t, err)
	if !bytes.Equal(before, assistantChatJSON(after)) {
		t.Fatal("legacy recovery changed original run")
	}
	if len(f.snapshot(t).Tickets) != 0 {
		t.Fatal("legacy known completion left promise")
	}
}
func TestDialectReadRecoveryStorageAdmissionAndStaleRefusals(t *testing.T) {
	for _, kind := range []string{"headroom", "stale-recovery", "stale-run", "cancelled", "maintenance-use", "home-move", "foreign-root", "copied-writer"} {
		t.Run(kind, func(t *testing.T) {
			f := recoveryStorageSetup(t)
			before := storageTestTree(t, f.config)
			state := f.snapshot(t)
			privateEntered := false
			testHookDialectRecoveryStorageStep = func(step string) {
				if step == "private-acquired" {
					privateEntered = true
				}
			}
			defer func() { testHookDialectRecoveryStorageStep = nil }()
			requestID, revision := "refusal", "0"
			switch kind {
			case "headroom":
				f.owner.options.HeadroomBytes = 1 << 60
			case "stale-recovery":
				revision = "1"
			case "maintenance-use":
				hold, err := f.owner.maintenanceUse()
				assistantRunMust(t, err)
				defer hold.Close()
			case "home-move":
				f.server.homeMove.queued.Store(true)
				defer f.server.homeMove.queued.Store(false)
			case "foreign-root":
				original := f.server.recoveryStorage.root
				copyRoot := *original
				f.server.recoveryStorage.root = &copyRoot
				defer func() { f.server.recoveryStorage.root = original }()
			case "copied-writer":
				copyWriter := *f.server.recoveryStorage
				if _, err := copyWriter.prepareOriginalDialectReadRecovery(t.Context()); err == nil {
					t.Fatal("copied writer became authority")
				}
				return
			}
			request := httptest.NewRequest("POST", "/api/assistant-runs/"+f.run.ID+"/dialect-reads/"+f.operation+"/observe", strings.NewReader(`{"runRevision":"`+runRevisionForRecovery(f.run)+`","recoveryRevision":"`+revision+`","requestId":"`+requestID+`"}`))
			request.Header = f.header.Clone()
			if kind == "stale-run" {
				request.Body = http.NoBody
				request = httptest.NewRequest("POST", request.URL.String(), strings.NewReader(`{"runRevision":"1","recoveryRevision":"0","requestId":"refusal"}`))
				request.Header = f.header.Clone()
				if f.run.Revision == 1 {
					t.Fatal("fixture not later than revision1")
				}
			}
			if kind == "cancelled" {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				request = request.WithContext(ctx)
			}
			answer := httptest.NewRecorder()
			f.server.ServeHTTP(answer, request)
			if answer.Code == 200 {
				t.Fatal("refused admission acknowledged", kind)
			}
			if !reflect.DeepEqual(state, f.snapshot(t)) {
				t.Fatal("refusal reserved/settled or fabricated promise", kind)
			}
			if !reflect.DeepEqual(before, storageTestTree(t, f.config)) {
				t.Fatal("pre-admission refusal changed bytes", kind)
			}
			if kind == "headroom" {
				if _, err := os.Stat(filepath.Join(f.root.root.Name(), assistantDialectReadRecoveryName(f.run.Binding.Project, f.run.ID))); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("headroom refusal published sidecar")
				}
				if len(storageWriterRecords(t, f.owner)) != 0 || privateEntered {
					t.Fatal("actual advisory headroom refusal reached private/token admission")
				}
			}
			if (kind == "maintenance-use" || kind == "home-move") && privateEntered {
				t.Fatal("maintenance/move reached private turn")
			}
		})
	}
}
func recoveryStoragePreparedGuard(t *testing.T, f recoveryStorageFixture) (*assistantDialectReadRecoveryStorageOperation, *assistantDialectReadRecoveryPin, *assistantDialectReadRecoveryGuard) {
	t.Helper()
	op, err := f.server.recoveryStorage.prepareOriginalDialectReadRecovery(t.Context())
	assistantRunMust(t, err)
	t.Cleanup(func() { assistantRunMust(t, op.Close()) })
	pin, err := f.server.assistantRunService().pinDialectReadRecoveryPrepared(t.Context(), f.run, op)
	assistantRunMust(t, err)
	t.Cleanup(func() { assistantRunMust(t, pin.Close()) })
	request := httptest.NewRequest("POST", "/api/assistant-runs/"+f.run.ID+"/dialect-reads/"+f.operation+"/observe", nil)
	request.Header = f.header.Clone()
	guard, err := pin.acquire(request)
	assistantRunMust(t, err)
	t.Cleanup(func() { assistantRunMust(t, guard.Close()) })
	return op, pin, guard
}
func TestDialectReadRecoveryStorageTransferRevocationAndOneBorrow(t *testing.T) {
	f := recoveryStorageSetup(t)
	op, pin, g := recoveryStoragePreparedGuard(t, f)
	storageAssertAdmissionCount(t, f.owner, 1)
	if extra, err := f.server.recoveryStorage.prepareOriginalDialectReadRecovery(t.Context()); err == nil {
		_ = extra.Close()
		t.Fatal("second prepared operation escaped the original Server bound")
	}
	prior, raw, err := readAssistantDialectReadRecoveryLocked(t.Context(), g.data.root, g.run)
	assistantRunMust(t, err)
	row, err := newAssistantDialectReadRecoveryRecord(g.run, f.operation, "borrow-once")
	assistantRunMust(t, err)
	row.LatestObservation.State = "present-valid"
	row.LatestObservation.Reason = ""
	_, _, provenance, err := g.observeNative(t.Context(), f.operation)
	assistantRunMust(t, err)
	row.LatestObservation.SourceProvenanceSHA256 = provenance
	intent, err := prepareAssistantDialectReadRecoveryWrite(prior, raw, g.run, row)
	assistantRunMust(t, err)
	assistantRunMust(t, op.transferPrivate(t.Context(), g, intent, raw, row))
	assistantRunMust(t, g.Close())
	foreignPin := &assistantDialectReadRecoveryPin{host: pin.host, lease: pin.lease, run: pin.run}
	foreignPin.self = foreignPin
	foreignGuard := &assistantDialectReadRecoveryGuard{pin: foreignPin}
	foreignGuard.self = foreignGuard
	if err = op.private.borrow(t.Context(), foreignGuard); err == nil {
		t.Fatal("foreign pin borrowed original private turn")
	}
	a, err := f.server.recoveryStorage.reserveOriginalDialectReadRecovery(t.Context(), pin, intent)
	assistantRunMust(t, err)
	t.Cleanup(func() { assistantRunMust(t, a.Close()) })
	next, err := pin.acquire(g.request)
	assistantRunMust(t, err)
	t.Cleanup(func() { assistantRunMust(t, next.Close()) })
	if next.private != op.private.lock {
		t.Fatal("borrow replaced original private file description")
	}
	foreign := &assistantDialectReadRecoveryGuard{pin: pin}
	foreign.self = foreign
	if err = op.private.borrow(t.Context(), foreign); err == nil {
		t.Fatal("second consumer borrowed transferred turn")
	}
	copied := *op.private
	if err = copied.current(t.Context()); err == nil {
		t.Fatal("copied hold retained authority")
	}
	state := f.snapshot(t)
	tree := storageTestTree(t, f.config)
	// Close concurrently while the lexical borrower remains live. Revocation is
	// immediate, actual private Close waits for that borrow's release.
	closed := 0
	testHookDialectRecoveryStorageStep = func(step string) {
		if step == "private-closed" {
			closed++
		}
	}
	defer func() { testHookDialectRecoveryStorageStep = nil }()
	done := make(chan error, 1)
	go func() { done <- op.Close() }()
	select {
	case err = <-done:
		if !errors.Is(err, storagebudget.ErrBusy) {
			t.Fatal("live borrow not retained", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close blocked on host/lease prefix")
	}
	storageAssertAdmissionCount(t, f.owner, 1) // Busy Close has not drained
	if err = a.writeOriginalDialectReadRecovery(t.Context(), next, intent); err == nil {
		t.Fatal("revoked operation wrote")
	}
	assistantRunMust(t, next.Close())
	assistantRunMust(t, a.Close())
	storageAssertAdmissionCount(t, f.owner, 1) // nested permit cannot release its parent
	assistantRunMust(t, op.Close())
	storageAssertAdmissionCount(t, f.owner, 0)
	assistantRunMust(t, op.Close())
	assistantRunMust(t, pin.Close())
	if closed != 1 {
		t.Fatal("transferred lock/turn did not close exactly once", closed)
	}
	if !reflect.DeepEqual(state, f.snapshot(t)) || !reflect.DeepEqual(tree, storageTestTree(t, f.config)) {
		t.Fatal("revocation wrote/refunded uncertain reservation")
	}
}
func TestDialectReadRecoveryStoragePrivateQueueCancellation(t *testing.T) {
	f := recoveryStorageSetup(t)
	state, tree := f.snapshot(t), storageTestTree(t, f.config)
	initialWriterRecords := storageWriterRecords(t, f.owner)
	held, err := f.server.privateDataLockContext(t.Context(), true)
	assistantRunMust(t, err)
	t.Cleanup(func() { assistantRunMust(t, held.Close()) })
	f.server.privateDataTurns.mu.Lock()
	holder := f.server.privateDataTurns.waiters[0]
	f.server.privateDataTurns.mu.Unlock()
	queued := make(chan struct{}, 1)
	testHookPrivateDataTurnQueued = func(context.Context) {
		select {
		case queued <- struct{}{}:
		default:
		}
	}
	defer func() { testHookPrivateDataTurnQueued = nil }()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	request := httptest.NewRequest("POST", "/api/assistant-runs/"+f.run.ID+"/dialect-reads/"+f.operation+"/observe", strings.NewReader(`{"runRevision":"`+runRevisionForRecovery(f.run)+`","recoveryRevision":"0","requestId":"queued"}`))
	request.Header = f.header.Clone()
	request = request.WithContext(ctx)
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() { answer := httptest.NewRecorder(); f.server.ServeHTTP(answer, request); result <- answer }()
	select {
	case <-queued:
	case <-time.After(time.Second):
		t.Fatal("actual private FIFO not entered")
	}
	h := f.server.assistantRunService()
	if !h.mu.TryLock() {
		t.Fatal("FIFO waited while host mutex held")
	}
	h.mu.Unlock()
	cancel()
	select {
	case answer := <-result:
		if answer.Code == 200 {
			t.Fatal("cancelled FIFO caller succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled FIFO did not drain")
	}
	f.server.privateDataTurns.mu.Lock()
	onlyHolder := len(f.server.privateDataTurns.waiters) == 1 && f.server.privateDataTurns.waiters[0] == holder
	f.server.privateDataTurns.mu.Unlock()
	if !onlyHolder {
		t.Fatal("cancelled FIFO retained or replaced a turn")
	}
	assistantRunMust(t, held.Close())
	finalWriterRecords := storageWriterRecords(t, f.owner)
	writerRecordsEqual := reflect.DeepEqual(initialWriterRecords, finalWriterRecords)
	stateEqual := reflect.DeepEqual(state, f.snapshot(t))
	treeEqual := reflect.DeepEqual(tree, storageTestTree(t, f.config))
	if !writerRecordsEqual || !stateEqual || !treeEqual {
		t.Fatalf("queued cancellation changed storage or reached B2 admission: initialWriterRecords=%d finalWriterRecords=%d writerRecordsEqual=%t snapshotEqual=%t treeEqual=%t", len(initialWriterRecords), len(finalWriterRecords), writerRecordsEqual, stateEqual, treeEqual)
	}
}

func TestDialectReadRecoveryStoragePreparedPinQueueCancellation(t *testing.T) {
	f := recoveryStorageSetup(t)
	state, tree := f.snapshot(t), storageTestTree(t, f.config)
	initialWriterRecords := storageWriterRecords(t, f.owner)
	op, err := f.server.recoveryStorage.prepareOriginalDialectReadRecovery(t.Context())
	assistantRunMust(t, err)
	t.Cleanup(func() { assistantRunMust(t, op.Close()) })
	held, err := f.server.privateDataLockContext(t.Context(), true)
	assistantRunMust(t, err)
	t.Cleanup(func() { assistantRunMust(t, held.Close()) })
	f.server.privateDataTurns.mu.Lock()
	holder := f.server.privateDataTurns.waiters[0]
	f.server.privateDataTurns.mu.Unlock()
	type contextKey struct{}
	ctx, cancel := context.WithCancel(context.WithValue(t.Context(), contextKey{}, op))
	defer cancel()
	queued := make(chan struct{}, 1)
	testHookPrivateDataTurnQueued = func(observed context.Context) {
		if observed.Value(contextKey{}) != op {
			return
		}
		select {
		case queued <- struct{}{}:
		default:
		}
	}
	defer func() { testHookPrivateDataTurnQueued = nil }()
	type pinResult struct {
		pin *assistantDialectReadRecoveryPin
		err error
	}
	result := make(chan pinResult, 1)
	h := f.server.assistantRunService()
	go func() {
		pin, err := h.pinDialectReadRecoveryPrepared(ctx, f.run, op)
		result <- pinResult{pin, err}
	}()
	select {
	case <-queued:
	case <-time.After(time.Second):
		t.Fatal("prepared pin did not queue with caller context")
	}
	storageAssertAdmissionCount(t, f.owner, 1) // prepared before FIFO wait
	if !h.mu.TryLock() {
		t.Fatal("prepared pin FIFO waited while host mutex held")
	}
	h.mu.Unlock()
	cancel()
	select {
	case answer := <-result:
		if answer.pin != nil {
			assistantRunMust(t, answer.pin.Close())
			t.Fatal("cancelled prepared pin acquired custody")
		}
		if answer.err == nil {
			t.Fatal("cancelled prepared pin succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled prepared pin FIFO did not drain")
	}
	f.server.privateDataTurns.mu.Lock()
	onlyHolder := len(f.server.privateDataTurns.waiters) == 1 && f.server.privateDataTurns.waiters[0] == holder
	f.server.privateDataTurns.mu.Unlock()
	if !onlyHolder {
		t.Fatal("cancelled prepared pin retained or replaced a turn")
	}
	assistantRunMust(t, held.Close())
	storageAssertAdmissionCount(t, f.owner, 1) // cancellation is not drain
	assistantRunMust(t, op.Close())
	storageAssertAdmissionCount(t, f.owner, 0)
	finalWriterRecords := storageWriterRecords(t, f.owner)
	writerRecordsEqual := reflect.DeepEqual(initialWriterRecords, finalWriterRecords)
	stateEqual := reflect.DeepEqual(state, f.snapshot(t))
	treeEqual := reflect.DeepEqual(tree, storageTestTree(t, f.config))
	if !writerRecordsEqual || !stateEqual || !treeEqual {
		t.Fatalf("prepared pin cancellation changed storage or reached B2 admission: initialWriterRecords=%d finalWriterRecords=%d writerRecordsEqual=%t snapshotEqual=%t treeEqual=%t", len(initialWriterRecords), len(finalWriterRecords), writerRecordsEqual, stateEqual, treeEqual)
	}
}
func TestDialectReadRecoveryStorageKnownTerminalForeignAndCancelledLookup(t *testing.T) {
	for _, kind := range []string{"foreign-operation", "foreign-target", "missing", "cancelled", "corrupt"} {
		t.Run(kind, func(t *testing.T) {
			f := recoveryStorageSetup(t)
			answer := f.request(t, "known-terminal", "0")
			if answer.Code != 200 {
				t.Fatal(answer.Code, answer.Body.String())
			}
			op, _, g := recoveryStoragePreparedGuard(t, f)
			_ = op
			value, raw, err := readAssistantDialectReadRecoveryLocked(t.Context(), g.data.root, g.run)
			assistantRunMust(t, err)
			row := *assistantDialectReadRecoveryOf(value, f.operation)
			ctx := t.Context()
			switch kind {
			case "foreign-operation":
				row.OperationID = "foreign-original-operation"
			case "foreign-target":
				foreign := *g
				foreign.self = &foreign
				foreign.run.ID = "arun_" + strings.Repeat("c", 32)
				g = &foreign
			case "missing":
				row.LatestObservation.RequestID = "missing-original-identity"
			case "cancelled":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			case "corrupt":
				control, e := storageOpenOriginalControl(f.owner.ref.cell, storageIntentName)
				assistantRunMust(t, e)
				_, e = control.file.WriteAt([]byte("broken!!"), 0)
				assistantRunMust(t, e)
				assistantRunMust(t, control.file.Sync())
				assistantRunMust(t, control.file.Close())
			}
			state := f.snapshot(t)
			tree := storageTestTree(t, f.config)
			if err = f.server.recoveryStorage.recognizeOriginalDialectReadRecovery(ctx, g, value, raw, row); err == nil {
				t.Fatal("unproved terminal outcome acknowledged", kind)
			}
			if !reflect.DeepEqual(state, f.snapshot(t)) || !reflect.DeepEqual(tree, storageTestTree(t, f.config)) {
				t.Fatal("unproved lookup changed effect/accounting", kind)
			}
		})
	}
}

func recoveryStorageStartUnrelated(t *testing.T, f recoveryStorageFixture) *assistantRunExecution {
	t.Helper()
	token := strings.TrimPrefix(f.header.Get("Authorization"), "Bearer ")
	saved := policyWorkspaceRead(t, f.server, token, "/api/conversations")
	var document conversationDocument
	assistantRunMust(t, json.Unmarshal(saved.Content, &document))
	chat := map[string]any{"id": "recovery-unrelated", "interaction": "work", "aiConnection": testAIAlpha, "model": "shared-model", "checkpoint": map[string]any{"state": map[string]any{"turns": []any{map[string]any{"id": "recovery-unrelated-message", "role": "user", "kind": "message", "text": "Independent admitted run", "at": "2026-10-20T00:00:00Z"}}}}}
	document.Chats = append(document.Chats, assistantChatJSON(chat))
	answer := policyWorkspaceCall(f.server, token, "PUT", "/api/conversations", saved.SHA256, document)
	if answer.Code != 200 {
		t.Fatal(answer.Code, answer.Body.String())
	}
	saved = policyWorkspaceRead(t, f.server, token, "/api/conversations")
	assistantRunMust(t, json.Unmarshal(saved.Content, &document))
	var raw json.RawMessage
	for _, row := range document.Chats {
		var id struct {
			ID string `json:"id"`
		}
		assistantRunMust(t, json.Unmarshal(row, &id))
		if id.ID == "recovery-unrelated" {
			raw = row
		}
	}
	if raw == nil {
		t.Fatal("unrelated chat missing")
	}
	entered := make(chan *assistantRunExecution, 1)
	checked := make(chan *assistantRunExecution, 1)
	f.server.assistantRunExecutor = &assistantProtocolExecutor{execute: func(ctx context.Context, e *assistantRunExecution) error {
		entered <- e
		<-ctx.Done()
		return ctx.Err()
	}}
	f.server.testAssistantRunMonitorChecked = func(ctx context.Context, e *assistantRunExecution) {
		select {
		case checked <- e:
		case <-ctx.Done():
			return
		}
		<-ctx.Done()
	}
	start := assistantRunStart{AdmissionID: "recovery-unrelated-admission", Purpose: "chat", OwnerID: "recovery-unrelated", Revision: digestOf(raw), RequestID: "recovery-unrelated-message"}
	view := assistantProtocolView(t, policyCall(f.server, token, "POST", "/api/assistant-runs", start), 201)
	var e *assistantRunExecution
	select {
	case e = <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("unrelated execution missing")
	}
	if view.ID != e.record.ID {
		t.Fatal("foreign unrelated owner")
	}
	t.Cleanup(func() {
		e.cancel()
		select {
		case <-e.done:
		case <-time.After(3 * time.Second):
			t.Error("unrelated owner did not drain")
		}
	})
	select {
	case actual := <-checked:
		if actual != e {
			t.Fatal("foreign unrelated monitor")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("unrelated authorization barrier")
	}
	return e
}
func TestDialectReadRecoveryStorageBorrowedActiveLifetime(t *testing.T) {
	f := recoveryStorageSetup(t)
	live := recoveryStorageStartUnrelated(t, f)
	originalHome := live.writer.lease.recoveryHome
	if originalHome == nil || originalHome != f.server.recoveryStorage.home || originalHome.file != f.server.homeLease {
		t.Fatal("active work did not retain actual earlier constructor home")
	}
	var steps []string
	borrowedWithAdmission := false
	testHookDialectRecoveryStorageStep = func(step string) {
		steps = append(steps, step)
		if step == "active-work-executor-borrowed" {
			f.owner.admission.mu.Lock()
			borrowedWithAdmission = f.owner.admission.shared == 1
			f.owner.admission.mu.Unlock()
		}
	}
	defer func() { testHookDialectRecoveryStorageStep = nil }()
	answer := f.request(t, "borrowed-real-b2", "0")
	if answer.Code != 200 {
		t.Fatal("actual borrowed HTTP", answer.Code, answer.Body.String())
	}
	found := false
	for _, step := range steps {
		if step == "active-work-executor-borrowed" {
			found = true
		}
		if step == "external-work-executor-acquired" {
			t.Fatal("active service was displaced/recovered")
		}
	}
	storageAssertAdmissionCount(t, f.owner, 0)
	if !borrowedWithAdmission {
		t.Fatal("active service borrow preceded admission lifetime")
	}
	if !found {
		t.Fatal("real active work was not borrowed")
	}
	h := f.server.assistantRunService()
	h.mu.Lock()
	same := h.lease == live.writer.lease && len(h.active) > 0 && h.dialectReadRecoveryPins == 0
	h.mu.Unlock()
	if !same {
		t.Fatal("recovery released unrelated active lease")
	}
	assistantRunMust(t, live.writer.State(t.Context(), "running"))
	assistantRunMust(t, live.writer.Stop(t.Context()))
	after, err := f.server.assistantRuns().Read(t.Context(), f.run.Binding, f.run.ID)
	assistantRunMust(t, err)
	if !reflect.DeepEqual(after, f.run) {
		t.Fatal("borrowed recovery changed original run")
	}
}
func TestDialectReadRecoveryStorageNoOriginalFactoryFromPaths(t *testing.T) {
	f := recoveryStorageSetup(t)
	s := f.server
	// An installed writer and unrelated Server cannot mint a second original
	// capability from the same saved or path-selected enrollment.
	if _, err := newAssistantDialectReadRecoveryStorage(s, f.owner, f.root, f.root.row.Workspace); err == nil {
		t.Fatal("duplicate attachment accepted")
	}
	foreign, _ := policyTestServer(t)
	if _, err := newAssistantDialectReadRecoveryStorage(foreign, f.owner, f.root, f.server.recoveryStorage.workspace); err == nil {
		t.Fatal("foreign Server paths installed original enrollment")
	}
	assistantRunMust(t, s.recoveryStorage.Close())
	if answer := f.request(t, "closed-factory", "0"); answer.Code == 200 {
		t.Fatal("closed factory granted operation")
	}
}

func TestDialectReadRecoveryStorageHTTPExactReplacement(t *testing.T) {
	f := recoveryStorageSetup(t)
	if answer := f.request(t, "first-version", "0"); answer.Code != 200 {
		t.Fatal(answer.Code, answer.Body.String())
	}
	first, rawBefore := f.sidecar(t)
	state := f.snapshot(t)
	if answer := f.request(t, "second-version", "1"); answer.Code != 200 {
		t.Fatal(answer.Code, answer.Body.String())
	}
	next, rawAfter := f.sidecar(t)
	after := f.snapshot(t)
	if first.Revision != 1 || next.Revision != 2 || next.Records[0].LatestObservation.Ordinal != 2 || next.Records[0].LatestObservation.RequestID != "second-version" {
		t.Fatal("exact replacement not observed")
	}
	prior := state.Installation[storagebudget.Control]
	current := after.Installation[storagebudget.Control]
	if current.Files != prior.Files || current.Bytes+uint64(len(rawBefore)) != prior.Bytes+uint64(len(rawAfter)) || len(after.Tickets) != 0 {
		t.Fatal("raw replacement was not logically charged exactly once")
	}
	rows := storageWriterRecords(t, f.owner)
	if len(rows) != 2 || !rows[1].OldExists || rows[1].OldLength != uint64(len(rawBefore)) || rows[1].Token == rows[0].Token {
		t.Fatal("replacement did not use actual old baseline/new original admission")
	}
	tree := storageTestTree(t, f.config)
	if answer := f.request(t, "stale-version", "1"); answer.Code != 409 {
		t.Fatal("stale sidecar CAS accepted", answer.Code)
	}
	if !reflect.DeepEqual(tree, storageTestTree(t, f.config)) || !reflect.DeepEqual(after, f.snapshot(t)) {
		t.Fatal("stale revision changed bytes or accounting")
	}
}

// Reach the exact post-consumer-guard window through real legacy B2 publication,
// unchanged-run/sidecar verification and successful Close, with no test-written
// recovery bytes or second token.
func recoveryStorageVerifiedLegacyPublication(t *testing.T) (recoveryStorageFixture, *assistantDialectReadRecoveryStorageOperation, *assistantDialectReadRecoveryGuard, *assistantDialectReadRecoveryStorageAdmission) {
	t.Helper()
	s, e, run, operation := recoveryStorageLegacyStopped(t)
	f := recoveryStorageEnroll(t, s, e, run, operation)
	op, pin, first := recoveryStoragePreparedGuard(t, f)
	prior, raw, err := readAssistantDialectReadRecoveryLocked(t.Context(), first.data.root, first.run)
	assistantRunMust(t, err)
	row, err := newAssistantDialectReadRecoveryRecord(first.run, f.operation, "completion-close-race")
	assistantRunMust(t, err)
	state, reason, provenance, err := first.observeNative(t.Context(), f.operation)
	assistantRunMust(t, err)
	row.LatestObservation.State, row.LatestObservation.Reason, row.LatestObservation.SourceProvenanceSHA256 = state, reason, provenance
	intent, err := prepareAssistantDialectReadRecoveryWrite(prior, raw, first.run, row)
	assistantRunMust(t, err)
	assistantRunMust(t, op.transferPrivate(t.Context(), first, intent, raw, row))
	assistantRunMust(t, first.Close())
	admitted, err := f.server.recoveryStorage.reserveOriginalDialectReadRecovery(t.Context(), pin, intent)
	assistantRunMust(t, err)
	a, ok := admitted.(*assistantDialectReadRecoveryStorageAdmission)
	if !ok {
		t.Fatal("concrete real B2 admission missing")
	}
	t.Cleanup(func() { _ = a.Close() })
	second, err := pin.acquire(first.request)
	assistantRunMust(t, err)
	t.Cleanup(func() { _ = second.Close() })
	_, err = second.commit(t.Context(), prior, raw, row, intent, a)
	assistantRunMust(t, err)
	assistantRunMust(t, second.Check(t.Context()))
	assistantRunMust(t, second.Close())
	if !second.closed || second.closeErr != nil || second.serviceHeld || second.writesHeld || second.leaseHeld || op.private.borrower != nil || a.verified != second || a.single.domain != nil || !op.private.matchesRoot(f.root) {
		t.Fatal("fixture did not close exact verified legacy consumer guard")
	}
	status, err := f.owner.ref.cell.q.InspectHostState(t.Context())
	assistantRunMust(t, err)
	rows := storageWriterRecords(t, f.owner)
	if len(rows) != 1 || rows[0].Phase != storageIntentPublishing || status.Slots[rows[0].Token.Slot].Ticket.State != storagebudget.Staging {
		t.Fatal("fixture did not retain original Publishing obligation")
	}
	return f, op, second, a
}

func TestDialectReadRecoveryStorageCompletionCloseLifetime(t *testing.T) {
	points := []struct {
		name   string
		fault  *storageBudgetFault
		host   storageIntentPhase
		ticket storagebudget.State
	}{
		{"success", nil, storageIntentRetired, storagebudget.Terminal},
		{"settle-before", &storageBudgetFault{Name: "settle", Phase: "before"}, storageIntentSettling, storagebudget.Staging},
		{"settle-after", &storageBudgetFault{Name: "settle", Phase: "after"}, storageIntentSettling, storagebudget.Terminal},
		{"checkpoint-after", &storageBudgetFault{Name: "checkpoint", Phase: "after"}, storageIntentSettled, storagebudget.Terminal},
		{"late-custody-close", &storageBudgetFault{Name: "recovery", Phase: "custody-close"}, storageIntentRetired, storagebudget.Terminal},
	}
	for _, point := range points {
		t.Run(point.name, func(t *testing.T) {
			f, op, g, a := recoveryStorageVerifiedLegacyPublication(t)
			f.owner.fault = point.fault
			ready, resume, revoked := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var readyOnce, resumeOnce, revokedOnce sync.Once
			var countsMu sync.Mutex
			counts := map[string]int{}
			testHookDialectRecoveryStorageStep = func(step string) {
				switch step {
				case "completion-current":
					readyOnce.Do(func() { close(ready) })
					<-resume
				case "operation-close-revoked":
					revokedOnce.Do(func() { close(revoked) })
				case "private-closed", "use-closed", "home-closed":
					countsMu.Lock()
					counts[step]++
					countsMu.Unlock()
				}
			}
			completion := make(chan error, 1)
			completionFinished := make(chan struct{})
			closing := make(chan error, 1)
			closeFinished := make(chan struct{})
			closeStarted := false
			go func() {
				defer close(completionFinished)
				completion <- a.completeOriginalDialectReadRecovery(t.Context(), g)
			}()
			defer func() {
				resumeOnce.Do(func() { close(resume) })
				select {
				case <-completionFinished:
				case <-time.After(3 * time.Second):
					t.Error("completion goroutine did not drain")
				}
				if closeStarted {
					select {
					case <-closeFinished:
					case <-time.After(3 * time.Second):
						t.Error("Close goroutine did not drain")
					}
				}
				testHookDialectRecoveryStorageStep = nil
			}()
			select {
			case <-ready:
			case <-time.After(3 * time.Second):
				t.Fatal("completion did not reach last-currentness boundary")
			}
			closeStarted = true
			go func() { defer close(closeFinished); closing <- op.Close() }()
			select {
			case <-revoked:
			case <-time.After(time.Second):
				t.Fatal("Close did not promptly revoke")
			}
			storageAssertAdmissionCount(t, f.owner, 1) // held through entered completion
			if !op.revoked.Load() {
				t.Fatal("Close waited without prompt revocation")
			}
			select {
			case err := <-closing:
				t.Fatal("Close released lifetime before completion", err)
			default:
			}
			// Retained actual descriptions and independent competing acquisitions prove
			// continuous custody, rather than relying on the notification order alone.
			for _, file := range []*os.File{op.private.lock.File, op.capacity.use.gate, op.use.gate, op.home} {
				if _, err := file.Stat(); err != nil {
					t.Fatal("in-progress completion lost actual description", err)
				}
			}
			if lock, err := f.server.tryPrivateDataLockContext(t.Context(), true); err == nil {
				lock.Close()
				t.Fatal("completion lost original FIFO turn")
			}
			if lock, err := openAssistantDialectRecoveryLock(t.Context(), f.server.assistant.root, ".data.lock", true); err == nil {
				lock.Close()
				t.Fatal("completion lost original private EX")
			}
			if use, err := f.owner.maintenanceUse(); err == nil {
				use.Close()
				t.Fatal("completion lost installation-use exclusion")
			}
			// These try-only host mutexes are available while completion/Close waits.
			h := f.server.assistantRunService()
			if !h.mu.TryLock() {
				t.Fatal("completion holds host mutex")
			}
			h.mu.Unlock()
			if !f.server.writes.TryLock() {
				t.Fatal("completion holds write mutex")
			}
			f.server.writes.Unlock()
			if !op.pin.lease.mu.TryLock() {
				t.Fatal("completion holds lease mutex")
			}
			op.pin.lease.mu.Unlock()
			rows := storageWriterRecords(t, f.owner)
			if len(rows) != 1 || rows[0].Phase != storageIntentPublishing {
				t.Fatal("completion wrote before held boundary was released")
			}
			original := f.snapshot(t)
			if len(original.Tickets) != 1 || original.Tickets[0].Token != rows[0].Token || original.Tickets[0].State != storagebudget.Staging || len(original.CapacityPromised) == 0 {
				t.Fatal("fixture lost exact original staging promise")
			}
			originalStatus, err := f.owner.ref.cell.q.InspectHostState(t.Context())
			assistantRunMust(t, err)
			resumeOnce.Do(func() { close(resume) })
			var completionErr, closeErr error
			select {
			case completionErr = <-completion:
			case <-time.After(3 * time.Second):
				t.Fatal("completion did not finish")
			}
			select {
			case closeErr = <-closing:
			case <-time.After(3 * time.Second):
				t.Fatal("Close did not finish after barrier")
			}
			assistantRunMust(t, closeErr)
			storageAssertAdmissionCount(t, f.owner, 0)
			if point.name == "success" || point.name == "late-custody-close" {
				assistantRunMust(t, completionErr)
			} else if completionErr == nil {
				t.Fatal("control uncertainty acknowledged")
			}
			closeAdmission := a.Close()
			if point.name == "late-custody-close" {
				if !errors.Is(closeAdmission, errStorageOwnerRecovery) {
					t.Fatal("late custody error lost", closeAdmission)
				}
			} else {
				assistantRunMust(t, closeAdmission)
			}
			if a.Close() != closeAdmission || op.Close() != closeErr {
				t.Fatal("repeated Close changed exact result")
			}
			for _, file := range []*os.File{op.private.lock.File, op.capacity.use.gate, op.use.gate, op.home} {
				if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
					t.Fatal("barrier completion did not physically close custody once", err)
				}
			}
			countsMu.Lock()
			defer countsMu.Unlock()
			for _, step := range []string{"private-closed", "use-closed", "home-closed"} {
				if counts[step] != 1 {
					t.Fatal("custody closed more/less than once", step, counts)
				}
			}
			rows = storageWriterRecords(t, f.owner)
			status, err := f.owner.ref.cell.q.InspectHostState(t.Context())
			assistantRunMust(t, err)
			if len(rows) != 1 || rows[0].Phase != point.host || status.Slots[rows[0].Token.Slot].Ticket.State != point.ticket {
				t.Fatal("Close changed actual control/terminal truth")
			}
			snapshot := f.snapshot(t)
			if point.ticket == storagebudget.Staging {
				if !reflect.DeepEqual(snapshot, original) {
					t.Fatal("pre-terminal uncertainty changed original promise or usage")
				}
			} else {
				terminal := original.Tickets[0]
				terminal.Token.Generation++
				terminal.State = storagebudget.Terminal
				terminal.Evidence = rows[0].Evidence
				terminal.Changes = rows[0].Changes
				if !reflect.DeepEqual(status.Slots[terminal.Token.Slot].Ticket, terminal) {
					t.Fatal("settlement changed original identity or terminal facts")
				}
				wantUsage := original.Installation
				wantUsage[storagebudget.Control].Bytes += rows[0].NewLength
				wantUsage[storagebudget.Control].Bytes -= rows[0].OldLength
				if !rows[0].OldExists {
					wantUsage[storagebudget.Control].Files++
				}
				if snapshot.Installation != wantUsage || snapshot.Promised != ([storagebudget.Categories]storagebudget.Vector{}) || len(snapshot.CapacityPromised) != 0 {
					t.Fatal("terminal truth rolled back, usage changed or fictitious promise minted")
				}
				if point.name == "settle-after" {
					// Settle durably writes the terminal; only Checkpoint retires it
					// from Snapshot. Retained history promises no further capacity.
					beforeSlot := originalStatus.Slots[terminal.Token.Slot]
					afterSlot := status.Slots[terminal.Token.Slot]
					if !reflect.DeepEqual(snapshot.Tickets, []storagebudget.Ticket{terminal}) || afterSlot.PersistedApplied != beforeSlot.PersistedApplied || afterSlot.PersistedNextSequence != beforeSlot.PersistedNextSequence {
						t.Fatal("uncheckpointed exact terminal history lost or falsely incorporated")
					}
				} else if len(snapshot.Tickets) != 0 || status.Slots[terminal.Token.Slot].PersistedApplied != terminal.Token {
					t.Fatal("checkpoint did not incorporate exact original terminal")
				}
			}
			bytesBefore := storageTestTree(t, f.config)
			if err = a.completeOriginalDialectReadRecovery(t.Context(), g); err == nil {
				t.Fatal("closed/consumed completion repeated")
			}
			if !reflect.DeepEqual(bytesBefore, storageTestTree(t, f.config)) || !reflect.DeepEqual(snapshot, f.snapshot(t)) {
				t.Fatal("completion/Close replayed effect, settlement or refund")
			}
		})
	}
	t.Run("pre-entry-revoked", func(t *testing.T) {
		f, op, g, a := recoveryStorageVerifiedLegacyPublication(t)
		before, state := storageTestTree(t, f.config), f.snapshot(t)
		assistantRunMust(t, op.Close())
		if err := a.completeOriginalDialectReadRecovery(t.Context(), g); err == nil {
			t.Fatal("Close-before-entry persisted completion")
		}
		if a.completed || a.publication.consumed {
			t.Fatal("revoked entry consumed a completion fact")
		}
		assistantRunMust(t, a.Close())
		if !reflect.DeepEqual(before, storageTestTree(t, f.config)) || !reflect.DeepEqual(state, f.snapshot(t)) {
			t.Fatal("revoked entry wrote journal/Q or released obligation")
		}
		rows := storageWriterRecords(t, f.owner)
		if len(rows) != 1 || rows[0].Phase != storageIntentPublishing {
			t.Fatal("revoked entry changed Publishing truth")
		}
	})
	t.Run("http-terminal-uncertainty", func(t *testing.T) {
		f := recoveryStorageSetup(t)
		f.owner.fault = &storageBudgetFault{Name: "settle", Phase: "after"}
		original := f.snapshot(t)
		answer := f.request(t, "http-control-uncertain", "0")
		if answer.Code == 200 {
			t.Fatal("actual HTTP acknowledged control uncertainty")
		}
		rows := storageWriterRecords(t, f.owner)
		state := f.snapshot(t)
		if len(rows) != 1 || rows[0].Phase != storageIntentSettling || len(state.Tickets) != 1 || len(state.CapacityPromised) != 0 || state.Promised != ([storagebudget.Categories]storagebudget.Vector{}) {
			t.Fatal("post-terminal error lost exact history or fabricated obligation")
		}
		terminal := state.Tickets[0]
		wantToken := rows[0].Token
		wantToken.Generation++
		status, err := f.owner.ref.cell.q.InspectHostState(t.Context())
		assistantRunMust(t, err)
		slot := status.Slots[wantToken.Slot]
		if terminal.Token != wantToken || terminal.State != storagebudget.Terminal || terminal.Owner != f.owner.ref.cell.binding || terminal.Evidence != rows[0].Evidence || !reflect.DeepEqual(terminal.Components, rows[0].Components) || !reflect.DeepEqual(terminal.Changes, rows[0].Changes) || !reflect.DeepEqual(slot.Ticket, terminal) || slot.PersistedApplied == wantToken || slot.PersistedNextSequence != wantToken.Sequence {
			t.Fatal("post-terminal error changed original settlement identity or falsely checkpointed it")
		}
		wantUsage := original.Installation
		wantUsage[storagebudget.Control].Bytes += rows[0].NewLength
		wantUsage[storagebudget.Control].Bytes -= rows[0].OldLength
		if !rows[0].OldExists {
			wantUsage[storagebudget.Control].Files++
		}
		if state.Installation != wantUsage {
			t.Fatal("post-terminal error rolled back or duplicated exact published usage")
		}
		before := storageTestTree(t, f.config)
		answer = f.request(t, "http-control-uncertain", "0")
		if answer.Code == 200 || !reflect.DeepEqual(before, storageTestTree(t, f.config)) || !reflect.DeepEqual(state, f.snapshot(t)) {
			t.Fatal("uncertain terminal retry acknowledged/replayed/refunded")
		}
	})
}

func TestDialectReadRecoveryStorageAdmissionBeforeHomeAndRefusedRegistration(t *testing.T) {
	f := recoveryStorageSetup(t)
	preparedBeforeHome := false
	homeOpened := false
	testHookDialectRecoveryStorageStep = func(step string) {
		if step == "admission-prepared" {
			storageAssertAdmissionCount(t, f.owner, 1)
			preparedBeforeHome = !homeOpened
		}
		if step == "home-prepared" {
			homeOpened = true
		}
	}
	defer func() { testHookDialectRecoveryStorageStep = nil }()
	op, err := f.server.recoveryStorage.prepareOriginalDialectReadRecovery(t.Context())
	assistantRunMust(t, err)
	assistantRunMust(t, op.Close())
	if !preparedBeforeHome || !homeOpened {
		t.Fatal("prepared operation did not obtain exclusion before home custody")
	}
	storageAssertAdmissionCount(t, f.owner, 0)
	state, tree := f.snapshot(t), storageTestTree(t, f.config)
	f.owner.mu.Lock()
	beforeActive := f.owner.active
	f.owner.mu.Unlock()
	assistantRunMust(t, f.owner.closeAdmission())
	homeOpened = false
	if op, err := f.server.recoveryStorage.prepareOriginalDialectReadRecovery(t.Context()); op != nil || !errors.Is(err, storagebudget.ErrBusy) {
		t.Fatal("blocked owner admitted a prepared operation", err)
	}
	if homeOpened {
		t.Fatal("refused admission opened/acquired home before refusal")
	}
	storageAssertAdmissionCount(t, f.owner, 0)
	w := f.server.recoveryStorage
	w.mu.Lock()
	registered := len(w.operations)
	w.mu.Unlock()
	f.owner.mu.Lock()
	afterActive := f.owner.active
	f.owner.mu.Unlock()
	if registered != 0 || beforeActive != afterActive || !reflect.DeepEqual(state, f.snapshot(t)) || !reflect.DeepEqual(tree, storageTestTree(t, f.config)) {
		t.Fatal("pre-home refusal leaked registration/retain or changed host/Q/data")
	}
}
