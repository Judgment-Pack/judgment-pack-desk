package desk

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// New owns real native roots and lifecycle resources. Every program path is
// deliberately absent in this test's own temporary directory; no listener or
// provider is needed by the actual authenticated admission/launch path below.
func dialectArtifactExecutionServer(t *testing.T, cfg Config) *Server {
	t.Helper()
	missing := t.TempDir()
	cfg.JpackBin = filepath.Join(missing, "unavailable-runtime")
	cfg.RunnerBin = filepath.Join(missing, "unavailable-runner")
	cfg.CodexBin = filepath.Join(missing, "unavailable-codex")
	cfg.Token, cfg.Port = testToken, 8791
	s, err := New(cfg)
	assistantRunMust(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func dialectArtifactExecutionStart(t *testing.T, s *Server, canonical bool) *assistantRunExecution {
	t.Helper()
	e, _ := dialectArtifactExecutionStartHeld(t, s, canonical)
	return e
}

type dialectArtifactExecutionMonitor struct {
	checked chan *assistantRunExecution
	release chan struct{}
}

func dialectArtifactExecutionStartHeld(t *testing.T, s *Server, canonical bool) (*assistantRunExecution, *dialectArtifactExecutionMonitor) {
	t.Helper()
	token := policyTestSession(t, s)
	connection := aiTestConnection(testAIAlpha, "Native owner fixture", "https://example.invalid")
	raw, err := encodeAIRegistry(aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{connection}})
	assistantRunMust(t, err)
	assistantRunMust(t, s.assistant.writeConfigNamed(aiConnectionsFile, raw, nil))
	assistantRunMust(t, s.assistant.storeKeyNamed(connectionKeyName(&connection), storedKey{present: true, key: "fixture-only-key", origin: "https://example.invalid", kind: connection.Assistant.Endpoint.Kind}))
	chat := json.RawMessage(`{"id":"chat-one","interaction":"work","aiConnection":"` + testAIAlpha + `","model":"shared-model","checkpoint":{"state":{"turns":[{"id":"message-one","role":"user","kind":"message","text":"Review exact input","at":"2026-10-20T00:00:00Z"}]}}}`)
	w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", "absent", map[string]any{"version": 1, "chats": []json.RawMessage{chat}})
	if w.Code != 200 {
		dialectArtifactExecutionStorageFailure(t, s)
		t.Fatal("save source", w.Code, w.Body.String())
	}
	saved := policyWorkspaceRead(t, s, token, "/api/conversations")
	var doc conversationDocument
	assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
	if s.testAssistantRunMonitorChecked != nil {
		t.Fatal("monitor barrier already installed")
	}
	monitor := &dialectArtifactExecutionMonitor{checked: make(chan *assistantRunExecution, 1), release: make(chan struct{})}
	// Install before launch, then retain this closure for the owner's entire
	// lifetime. Authorize has released source/private custody before calling it.
	s.testAssistantRunMonitorChecked = func(ctx context.Context, e *assistantRunExecution) {
		select {
		case monitor.checked <- e:
		case <-ctx.Done():
			return
		}
		select {
		case <-monitor.release:
		case <-ctx.Done():
		}
	}
	entered := make(chan *assistantRunExecution, 1)
	x := &assistantProtocolExecutor{execute: func(ctx context.Context, e *assistantRunExecution) error {
		entered <- e
		<-ctx.Done()
		return ctx.Err()
	}}
	s.assistantRunExecutor = x
	if canonical {
		s.assistantRunExecutor = &assistantCanonicalProtocolExecutor{x}
	}
	start := assistantRunStart{AdmissionID: "native-owner-admission", Purpose: "chat", OwnerID: "chat-one", Revision: digestOf(doc.Chats[0]), RequestID: "message-one"}
	view := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
	select {
	case e := <-entered:
		if e.record.ID != view.ID {
			t.Fatal("captured foreign execution")
		}
		t.Cleanup(func() { dialectArtifactExecutionDrain(t, e) })
		select {
		case checked := <-monitor.checked:
			if checked != e {
				t.Fatal("monitor checked a foreign execution")
			}
		case <-time.After(3 * time.Second):
			t.Fatal("actual periodic authorization did not finish")
		}
		return e, monitor
	case <-time.After(3 * time.Second):
		t.Fatal("actual native execution did not enter")
	}
	return nil, nil
}

// Report startup refusal at the failed write without changing its success requirement.
func dialectArtifactExecutionStorageFailure(t *testing.T, s *Server) {
	t.Helper()
	t.Logf("storage startup: readOnly=%t problemPresent=%t assistantUsable=%t infrastructure=%t construction=%t enrollment=%t provider=%T registry=%t startupProviders=%t",
		s.storageReadOnly, s.storageProblem != nil, s.assistant.usable(), s.storageInfrastructure != nil, s.storageConstruction != nil,
		s.storageEnrollment != nil, s.retainedArtifacts, s.retainedArtifactProviders != nil, s.storageStartupProviders != nil)
	if c := s.storageConstruction; c != nil {
		files := 0
		if c.original != nil {
			files = len(c.original.files)
		}
		t.Logf("storage construction: original=%t files=%d anchor=%t ref=%t owner=%t sources=%t selected=%T registryMatches=%t",
			c.original != nil, files, c.anchor != nil, c.ref != nil, c.owner != nil, c.sources != nil,
			c.selected, c.providers == s.retainedArtifactProviders)
	}
	// Visit joined error branches within a fixed budget, and bound each rendered
	// message so failure logs stay small. The budget also bounds unwrap cycles.
	remaining := 16
	var logError func(error, int)
	logError = func(err error, depth int) {
		if remaining == 0 {
			return
		}
		remaining--
		if err == nil {
			return
		}
		t.Logf("storage problem: depth=%d type=%T message=%.512q", depth, err, err.Error())
		switch e := err.(type) {
		case interface{ Unwrap() []error }:
			for _, child := range e.Unwrap() {
				if remaining == 0 {
					break
				}
				logError(child, depth+1)
			}
		case interface{ Unwrap() error }:
			logError(e.Unwrap(), depth+1)
		}
	}
	logError(s.storageProblem, 0)
	if remaining == 0 {
		t.Log("storage problem: error traversal reached 16-node limit")
	}
}

func dialectArtifactExecutionDrain(t *testing.T, e *assistantRunExecution) {
	t.Helper()
	e.cancel()
	select {
	case <-e.done:
	case <-time.After(5 * time.Second):
		t.Error("native execution did not drain")
	}
}

func dialectArtifactExecutionFixture(t *testing.T, canonical bool) (*Server, *assistantRunExecution) {
	t.Helper()
	s := dialectArtifactExecutionServer(t, Config{ProjectDir: t.TempDir(), DeskConfigDir: t.TempDir()})
	return s, dialectArtifactExecutionStart(t, s, canonical)
}

func TestDialectArtifactExecutionOwnerMonitorAuthorization(t *testing.T) {
	for _, canonical := range []bool{false, true} {
		for _, stop := range []string{"cancel-held", "source-disabled"} {
			t.Run(map[bool]string{false: "ordinary/", true: "canonical/"}[canonical]+stop, func(t *testing.T) {
				s := dialectArtifactExecutionServer(t, Config{ProjectDir: t.TempDir(), DeskConfigDir: t.TempDir()})
				e, monitor := dialectArtifactExecutionStartHeld(t, s, canonical)
				work, executor := dialectArtifactExecutionFDs(t, e)
				dialectArtifactExecutionLive(t, e, work, executor)
				if stop == "cancel-held" {
					// Leave release open: context cancellation must
					// release the callback before monitorDone and e.done can close.
					e.cancel()
				} else {
					// This saved profile changes the source revision. Its version-1 API
					// override does not disable shared-model on testAIAlpha; Authorize
					// rejects the changed saved source when its revision no longer matches.
					// File-change notifications cannot cancel an owner; session and policy
					// remain live, and the executor only returns when its context is canceled.
					assistantRunMust(t, os.WriteFile(filepath.Join(s.projectDir, assistantProfilePath), []byte(`{"profileVersion":1,"api":{"inherit":false,"models":[],"model":null}}`), 0600))
					identity, session, err := s.assistantRunIdentity(e.request.WithContext(t.Context()))
					assistantRunMust(t, err)
					if !assistantRunIdentityMatches(identity, e.source.Binding) || session.Err() != nil || e.ctx.Err() != nil {
						t.Fatal("source change directly revoked execution authority")
					}
					close(monitor.release)
				}
				select {
				case checked := <-monitor.checked:
					t.Fatal("monitor acknowledged another successful authorization", checked == e)
				case <-e.done:
				case <-time.After(5 * time.Second):
					t.Fatal("monitor and original owner did not drain", stop)
				}
				// A buffered acknowledgement must not hide behind the completed
				// owner select. No later callback can run after monitorDone joins.
				select {
				case <-monitor.checked:
					t.Fatal("failed authorization reported successful readiness")
				default:
				}
				if e.ctx.Err() == nil {
					t.Fatal("owner drained without actual execution cancellation")
				}
				e.service.mu.Lock()
				idle := len(e.service.active) == 0 && e.service.lease == nil
				e.service.mu.Unlock()
				if !idle {
					t.Fatal("monitor cancellation retained original service ownership")
				}
			})
		}
	}
}

func dialectArtifactExecutionFDs(t *testing.T, e *assistantRunExecution) (os.FileInfo, os.FileInfo) {
	t.Helper()
	l := e.writer.lease
	l.mu.Lock()
	defer l.mu.Unlock()
	work, err := l.work.Stat()
	assistantRunMust(t, err)
	executor, err := l.file.Stat()
	assistantRunMust(t, err)
	return work, executor
}

func dialectArtifactExecutionLive(t *testing.T, e *assistantRunExecution, work, executor os.FileInfo) {
	t.Helper()
	gotWork, gotExecutor := dialectArtifactExecutionFDs(t, e)
	if !os.SameFile(work, gotWork) || !os.SameFile(executor, gotExecutor) {
		t.Fatal("original execution descriptors changed")
	}
	for _, name := range []string{".data-work.lock", "assistant-run-executor-" + e.writer.lease.store.project + ".lock"} {
		probe, err := e.service.server.openPrivateDataFileLock(t.Context(), name, true, false)
		if err == nil {
			_ = probe.Close()
			t.Fatal("original exclusion lost", name)
		}
	}
	assistantRunMust(t, e.writer.Err())
	_, err := e.writer.lease.store.Read(t.Context(), e.writer.binding, e.writer.id)
	assistantRunMust(t, err)
}

func dialectArtifactExecutionRefused(t *testing.T, e *assistantRunExecution, ctx context.Context) {
	t.Helper()
	got, err := e.enrollDialectArtifactOwner(ctx)
	if err == nil || got != (dialectArtifactOwnerEnrollment{}) {
		t.Fatal("invalid execution acknowledged", got, err)
	}
}

func TestDialectArtifactExecutionOwnerActualLiveLease(t *testing.T) {
	for _, canonical := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "canonical"}[canonical], func(t *testing.T) {
			s, e := dialectArtifactExecutionFixture(t, canonical)
			work, executor := dialectArtifactExecutionFDs(t, e)
			before := assistantExecutionRecord(t, e)
			beforeNames, beforeRaw := dialectArtifactExecutionJournal(t, e)
			first, err := e.enrollDialectArtifactOwner(t.Context())
			assistantRunMust(t, err)
			if first.Owner == "" || first.Namespace != e.writer.lease.store.project || first.HomeAnchor != "" {
				t.Fatal("incorrect actual external provenance", first)
			}
			second, err := e.enrollDialectArtifactOwner(e.generationContext(t.Context(), e.generation))
			assistantRunMust(t, err)
			afterNames, afterRaw := dialectArtifactExecutionJournal(t, e)
			if first != second || !reflect.DeepEqual(before, assistantExecutionRecord(t, e)) || !reflect.DeepEqual(beforeNames, afterNames) || !bytes.Equal(beforeRaw, afterRaw) {
				t.Fatal("enrollment changed provenance or journal")
			}
			if got, err := s.enrollDialectArtifactOwner(e.request); err == nil || got != (dialectArtifactOwnerEnrollment{}) {
				t.Fatal("standalone bypassed original SH-work")
			}
			dialectArtifactExecutionLive(t, e, work, executor)
			// A genuine writer transaction still persists after all helper locks
			// are released. Enrollment itself performed no journal mutation.
			assistantRunMust(t, e.writer.change(t.Context(), false, func(*assistantRunRecord) (bool, error) { return true, nil }))
			advanced, err := e.enrollDialectArtifactOwner(t.Context())
			assistantRunMust(t, err)
			if advanced != first {
				t.Fatal("older immutable execution record revision rejected live writer head")
			}
			dialectArtifactExecutionDrain(t, e)
			standalone, err := s.enrollDialectArtifactOwner(e.request.WithContext(t.Context()))
			assistantRunMust(t, err)
			if standalone != first {
				t.Fatal("live and standalone metadata disagree", first, standalone)
			}
		})
	}
}

func dialectArtifactExecutionHome(t *testing.T) (*Server, *Server) {
	t.Helper()
	home := filepath.Join(t.TempDir(), "native-home\t")
	config := filepath.Join(home, deskHomePlatform)
	parentPath, childPath := filepath.Join(home, "startup"), filepath.Join(home, "child")
	owner, parentID, childID := strings.Repeat("a", 64), strings.Repeat("b", 32), strings.Repeat("c", 32)
	registry := deskHomeRegistry{Version: 1, Owner: owner, Startup: parentID, Desks: []deskHomeEntry{
		{ID: parentID, Name: "Startup", Folder: "startup", Kind: "project", HistoryIdentity: "/original/startup", StorageOwner: owner, RunnerWorkspace: owner},
		{ID: childID, Name: "Child", Folder: "child", Kind: "desk", HistoryIdentity: "desk:" + childID, StorageOwner: childID, RunnerWorkspace: childID},
	}}
	assistantRunMust(t, os.MkdirAll(config, 0700))
	raw, err := json.Marshal(registry)
	assistantRunMust(t, err)
	assistantRunMust(t, os.WriteFile(filepath.Join(config, deskHomeRegistryName), raw, 0600))
	for i, path := range []string{parentPath, childPath} {
		assistantRunMust(t, os.MkdirAll(filepath.Join(path, ".desk-private", "data"), 0700))
		assistantRunMust(t, os.WriteFile(filepath.Join(path, projectConfigName), []byte(`{"deskConfigVersion":1}`), 0600))
		assistantRunMust(t, os.WriteFile(filepath.Join(path, ".desk-private", dataLocationName), []byte(`{"version":1,"path":"data"}`), 0600))
		storageOwner := owner
		if i == 1 {
			storageOwner = childID
			manifest, err := json.Marshal(deskRecord{ID: childID, Name: "Child"})
			assistantRunMust(t, err)
			assistantRunMust(t, os.WriteFile(filepath.Join(path, deskManifest), manifest, 0600))
		}
		marker, err := json.Marshal(dataMarker{Version: 1, Owner: storageOwner})
		assistantRunMust(t, err)
		assistantRunMust(t, os.WriteFile(filepath.Join(path, ".desk-private", "data", dataMarkerName), marker, 0600))
	}
	parent := dialectArtifactExecutionServer(t, Config{ProjectDir: parentPath, DeskConfigDir: config, LocalAccess: true})
	parent.desksMu.Lock()
	child, err := parent.openDeskLocked(childID)
	parent.desksMu.Unlock()
	assistantRunMust(t, err)
	if child.cfg.parent != parent || parent.homeLease == nil || child.deskOwner() != parent {
		t.Fatal("native home ownership not installed")
	}
	return parent, child
}

func TestDialectArtifactExecutionOwnerActualHome(t *testing.T) {
	parent, child := dialectArtifactExecutionHome(t)
	var prior dialectArtifactOwnerEnrollment
	for _, s := range []*Server{parent, child} {
		e := dialectArtifactExecutionStart(t, s, true)
		work, executor := dialectArtifactExecutionFDs(t, e)
		beforeNames, beforeRaw := dialectArtifactExecutionJournal(t, e)
		got, err := e.enrollDialectArtifactOwner(t.Context())
		assistantRunMust(t, err)
		afterNames, afterRaw := dialectArtifactExecutionJournal(t, e)
		if !reflect.DeepEqual(beforeNames, afterNames) || !bytes.Equal(beforeRaw, afterRaw) {
			t.Fatal("native home enrollment changed chat root or journal")
		}
		if got.HomeAnchor == "" || got.HomeSHA256 == "" || prior.Owner != "" && (prior.Owner == got.Owner || prior.Namespace == got.Namespace || prior.HomeAnchor != got.HomeAnchor || prior.WorkDomain != got.WorkDomain) {
			t.Fatal("home namespaces or anchors not original", prior, got)
		}
		dialectArtifactExecutionLive(t, e, work, executor)
		dialectArtifactExecutionDrain(t, e)
		standalone, err := s.enrollDialectArtifactOwner(e.request.WithContext(t.Context()))
		assistantRunMust(t, err)
		if got != standalone {
			t.Fatal("native home producer readback disagrees")
		}
		pointer := dialectArtifactOwnerRead(t, filepath.Join(s.projectDir, ".desk-private", dataLocationName))
		if !bytes.Equal(pointer, []byte(`{"version":1,"path":"data"}`)) {
			t.Fatal("home data pointer changed")
		}
		prior = got
	}
}

func TestDialectArtifactExecutionOwnerIdentityRefusals(t *testing.T) {
	for _, name := range []string{"membership", "service-lease", "service-server", "writer", "writer-lease", "store-server", "store-project", "store-name", "store-problem", "work-nil", "executor-nil", "recovery", "writer-failure", "revision", "binding", "source", "canonical-binding", "canonical-store", "canonical-id", "canonical-writer", "observation", "purpose", "closed-service", "late-finalizing", "late-stop", "late-generation"} {
		t.Run(name, func(t *testing.T) {
			s, e := dialectArtifactExecutionFixture(t, strings.HasPrefix(name, "canonical"))
			work, executor := dialectArtifactExecutionFDs(t, e)
			h, w, l := e.service, e.writer, e.writer.lease
			// Fault boundaries execute under the real service/canonical/writer/
			// lease holds. The monitor cannot observe a temporary invalid pointer:
			// restore at data Close before those holds are released.
			originalStore, originalBinding, originalSource := l.store, w.binding, e.source
			originalWork, originalExecutor, revision := l.work, l.file, w.revision
			canonicalBinding, canonicalStore := e.canonicalBinding, e.canonicalStore
			canonicalID := ""
			restoreCanonicalWriter := func() {}
			if canonicalStore != nil {
				canonicalID = canonicalStore.id
			}
			if canonicalBinding != nil {
				originalWriter := canonicalBinding.Writer
				restoreCanonicalWriter = func() { canonicalBinding.Writer = originalWriter }
			}
			fired, restored := false, false
			testHookDialectArtifactOwnerStep = func(at string) error {
				if at == "work.sync" && !fired {
					fired = true
					switch name {
					case "membership":
						delete(h.active, e.record.ID)
					case "service-lease":
						h.lease = nil
					case "service-server":
						h.server = nil
					case "writer":
						e.writer = nil
					case "writer-lease":
						w.lease = nil
					case "store-server":
						l.store.server = nil
					case "store-project":
						l.store.project = strings.Repeat("f", 64)
					case "store-name":
						l.store.name = "foreign.json"
					case "store-problem":
						l.store.problem = errors.New("unavailable")
					case "work-nil":
						l.work = nil
					case "executor-nil":
						l.file = nil
					case "recovery":
						l.recoveryErr = errors.New("unrecovered")
					case "writer-failure":
						w.failure = errors.New("failed")
					case "revision":
						w.revision++
					case "binding":
						w.binding.Project = strings.Repeat("f", 64)
					case "source":
						// Replace authoritative bytes without changing originalSource backing storage.
						e.source.Request = []byte("foreign source")
					case "canonical-binding":
						e.canonicalBinding = nil
					case "canonical-store":
						e.canonicalStore = nil
					case "canonical-id":
						canonicalStore.id = "foreign"
					case "canonical-writer":
						canonicalBinding.Writer = nil
					case "observation":
						e.observation = &assistantObservationClaim{}
					case "purpose":
						e.source.Start.Purpose = "brief"
					case "closed-service":
						h.closed = true
					case "late-finalizing":
						e.finalizing = true
					case "late-stop":
						e.stopRequested = true
					case "late-generation":
						e.generation = 0
					}
				}
				if at == "data.close" && fired && !restored {
					h.active[e.record.ID], h.lease, h.server, e.writer, w.lease = e, l, s, w, l
					l.store, w.binding, e.source, l.work, l.file, w.revision = originalStore, originalBinding, originalSource, originalWork, originalExecutor, revision
					l.recoveryErr, w.failure = nil, nil
					e.canonicalBinding, e.canonicalStore = canonicalBinding, canonicalStore
					if canonicalStore != nil {
						canonicalStore.id = canonicalID
					}
					restoreCanonicalWriter()
					e.observation, h.closed, e.finalizing, e.stopRequested, e.generation = nil, false, false, false, 1
					restored = true
				}
				return nil
			}
			t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
			dialectArtifactExecutionRefused(t, e, t.Context())
			testHookDialectArtifactOwnerStep = nil
			if !fired || !restored {
				t.Fatal("original-custody mutation barrier not exercised", fired, restored)
			}
			dialectArtifactExecutionLive(t, e, work, executor)
		})
	}
	t.Run("nil-and-copy-wrapper", func(t *testing.T) {
		_, e := dialectArtifactExecutionFixture(t, false)
		dialectArtifactExecutionRefused(t, nil, t.Context())
		// Deliberately copy only pointer-shaped fields, never copy a mutex.
		copy := &assistantRunExecution{service: e.service, writer: e.writer, request: e.request, ctx: e.ctx, record: e.record, source: e.source, generation: 1, lifecycle: "running"}
		dialectArtifactExecutionRefused(t, copy, t.Context())
	})
	for _, name := range []string{"work-replaced", "work-unlinked", "work-symlink", "work-insecure", "executor-replaced", "executor-unlinked", "executor-symlink", "executor-insecure"} {
		t.Run(name, func(t *testing.T) {
			s, e := dialectArtifactExecutionFixture(t, false)
			work, executor := dialectArtifactExecutionFDs(t, e)
			leaf := ".data-work.lock"
			if strings.HasPrefix(name, "executor") {
				leaf = "assistant-run-executor-" + e.writer.lease.store.project + ".lock"
			}
			path := filepath.Join(s.configDir, leaf)
			if strings.HasSuffix(name, "insecure") {
				assistantRunMust(t, os.Chmod(path, 0644))
			} else {
				assistantRunMust(t, os.Rename(path, path+".original"))
				if strings.HasSuffix(name, "symlink") {
					assistantRunMust(t, os.Symlink(path+".original", path))
				}
				if strings.HasSuffix(name, "replaced") {
					assistantRunMust(t, os.WriteFile(path, nil, 0600))
				}
			}
			dialectArtifactExecutionRefused(t, e, t.Context())
			if strings.HasSuffix(name, "insecure") {
				assistantRunMust(t, os.Chmod(path, 0600))
			} else {
				if !strings.HasSuffix(name, "unlinked") {
					assistantRunMust(t, os.Remove(path))
				}
				assistantRunMust(t, os.Rename(path+".original", path))
			}
			dialectArtifactExecutionLive(t, e, work, executor)
		})
	}
}

func TestDialectArtifactExecutionOwnerLifecycleAndContention(t *testing.T) {
	for _, name := range []string{"service", "desks", "location-use", "location-fence", "writes", "canonical", "writer", "lease", "private", "marker", "owner"} {
		t.Run(name, func(t *testing.T) {
			s, e := dialectArtifactExecutionFixture(t, false)
			work, executor := dialectArtifactExecutionFDs(t, e)
			var release func()
			switch name {
			case "service":
				e.service.mu.Lock()
				release = e.service.mu.Unlock
			case "desks":
				s.desksMu.Lock()
				release = s.desksMu.Unlock
			case "location-use":
				s.location.use.Lock()
				release = s.location.use.Unlock
			case "location-fence":
				s.location.mu.Lock()
				release = s.location.mu.Unlock
			case "writes":
				s.writes.Lock()
				release = s.writes.Unlock
			case "canonical":
				e.canonicalMu.Lock()
				release = e.canonicalMu.Unlock
			case "writer":
				e.writer.mu.Lock()
				release = e.writer.mu.Unlock
			case "lease":
				e.writer.lease.mu.Lock()
				release = e.writer.lease.mu.Unlock
			case "private":
				lock, err := s.privateDataLockContext(t.Context(), true)
				assistantRunMust(t, err)
				release = func() { assistantRunMust(t, lock.Close()) }
			case "marker", "owner":
				got, err := e.enrollDialectArtifactOwner(t.Context())
				assistantRunMust(t, err)
				path := filepath.Join(s.projectDir, ".desk-private", ".dialect-artifact-owner.lock")
				if name == "owner" {
					path = dialectArtifactOwnerTablePath(s, "locks", got.Owner[:2], got.Owner[2:]+".lock")
				}
				file, err := os.OpenFile(path, os.O_RDWR, 0600)
				assistantRunMust(t, err)
				assistantRunMust(t, lockPrivateData(file, true))
				release = func() { assistantRunMust(t, file.Close()) }
			}
			// Call synchronously: a blocking acquisition is caught by the parent
			// test timeout, and no sleeps or scheduler timing grant admission.
			dialectArtifactExecutionRefused(t, e, t.Context())
			release()
			dialectArtifactExecutionLive(t, e, work, executor)
		})
	}
	for _, lifecycle := range []string{"settling", "parked", "resuming", "closed"} {
		t.Run(lifecycle, func(t *testing.T) {
			_, e := dialectArtifactExecutionFixture(t, false)
			e.canonicalMu.Lock()
			e.lifecycle = lifecycle
			e.canonicalMu.Unlock()
			dialectArtifactExecutionRefused(t, e, t.Context())
			e.canonicalMu.Lock()
			e.lifecycle = "running"
			e.canonicalMu.Unlock()
		})
	}
	for _, name := range []string{"stop", "finalizing", "generation-zero", "stale-generation", "finalizer", "cancel-call", "cancel-execution", "revoke-session"} {
		t.Run(name, func(t *testing.T) {
			s, e := dialectArtifactExecutionFixture(t, false)
			ctx := t.Context()
			switch name {
			case "stop":
				e.canonicalMu.Lock()
				e.stopRequested = true
				e.canonicalMu.Unlock()
			case "finalizing":
				e.service.mu.Lock()
				e.finalizing = true
				e.service.mu.Unlock()
			case "generation-zero":
				e.canonicalMu.Lock()
				e.generation = 0
				e.canonicalMu.Unlock()
			case "stale-generation":
				ctx = e.generationContext(ctx, e.generation+1)
			case "finalizer":
				ctx = context.WithValue(ctx, assistantInvocationGenerationKey{}, assistantInvocationGeneration{owner: e, number: e.generation, finalizer: true})
			case "cancel-call":
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			case "cancel-execution":
				e.cancel()
			case "revoke-session":
				s.sessions.revoke(bearerOf(e.request))
			}
			dialectArtifactExecutionRefused(t, e, ctx)
			e.service.mu.Lock()
			e.finalizing = false
			e.service.mu.Unlock()
			e.canonicalMu.Lock()
			e.stopRequested, e.generation = false, 1
			e.canonicalMu.Unlock()
		})
	}
}

func TestDialectArtifactExecutionOwnerFaultsAndReadback(t *testing.T) {
	for _, reuse := range []bool{false, true} {
		for _, step := range []string{"work.sync", "work.directory-sync", "work-domain.sync", "owner.sync", "location.sync", "marker.publish", "scope.sync", "selected-readback", "acknowledge", "data.close", "private.close", "metadata.close", "owner-lock.close"} {
			if reuse && step == "marker.publish" {
				continue
			}
			t.Run(map[bool]string{false: "fresh/", true: "reused/"}[reuse]+step, func(t *testing.T) {
				_, e := dialectArtifactExecutionFixture(t, false)
				if reuse {
					_, err := e.enrollDialectArtifactOwner(t.Context())
					assistantRunMust(t, err)
				}
				work, executor := dialectArtifactExecutionFDs(t, e)
				injected := errors.New("native owner fault")
				seen := false
				testHookDialectArtifactOwnerStep = func(at string) error {
					if at == step {
						seen = true
						return injected
					}
					if at == "work.close" {
						t.Error("live helper closed borrowed work")
					}
					return nil
				}
				t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
				got, err := e.enrollDialectArtifactOwner(t.Context())
				testHookDialectArtifactOwnerStep = nil
				if !seen || !errors.Is(err, injected) || got != (dialectArtifactOwnerEnrollment{}) {
					t.Fatal("live fault acknowledged or not exercised", seen, got, err)
				}
				dialectArtifactExecutionLive(t, e, work, executor)
			})
		}
	}
	for _, revoke := range []bool{false, true} {
		for _, boundary := range []string{"owner.publish", "marker.publish", "acknowledge"} {
			t.Run(map[bool]string{false: "cancel-at-", true: "revoke-at-"}[revoke]+boundary, func(t *testing.T) {
				s, e := dialectArtifactExecutionFixture(t, false)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				seen := false
				testHookDialectArtifactOwnerStep = func(at string) error {
					if at == boundary {
						seen = true
						if revoke {
							s.sessions.revoke(bearerOf(e.request))
						} else {
							cancel()
						}
					}
					return nil
				}
				t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
				dialectArtifactExecutionRefused(t, e, ctx)
				if !seen {
					t.Fatal("cancellation boundary not exercised")
				}
				testHookDialectArtifactOwnerStep = nil
			})
		}
	}
	t.Run("close-waits-for-original-lease-span", func(t *testing.T) {
		_, e := dialectArtifactExecutionFixture(t, false)
		l := e.writer.lease
		begun, closed := make(chan struct{}), make(chan error, 1)
		seen := false
		testHookDialectArtifactOwnerStep = func(at string) error {
			if seen || at != "owner.publish" {
				return nil
			}
			seen = true
			go func() { close(begun); closed <- l.Close() }()
			<-begun
			select {
			case err := <-closed:
				t.Error("borrowed lease closed inside metadata span", err)
			default:
			}
			_, err := l.work.Stat()
			return err
		}
		t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
		_, err := e.enrollDialectArtifactOwner(t.Context())
		testHookDialectArtifactOwnerStep = nil
		assistantRunMust(t, err)
		if !seen {
			t.Fatal("lease barrier not reached")
		}
		select {
		case err := <-closed:
			assistantRunMust(t, err)
		case <-time.After(3 * time.Second):
			t.Fatal("real lease Close did not resume")
		}
	})
}

func TestDialectArtifactExecutionOwnerInactiveAndNoBorrowedToken(t *testing.T) {
	// The public standalone producer must continue to refuse injected artifact
	// storage before acquiring work, independent of the new private live seam.
	s, e := dialectArtifactExecutionFixture(t, false)
	s.retainedArtifacts = &dialectArtifactOwnerInjected{}
	dialectArtifactExecutionRefused(t, e, t.Context())
	if got, err := s.enrollDialectArtifactOwner(httptest.NewRequest("POST", "/", nil)); err == nil || got != (dialectArtifactOwnerEnrollment{}) {
		t.Fatal("injected standalone accepted")
	}
	s.retainedArtifacts = nil
}

func TestDialectArtifactExecutionOwnerActualHomeRefusals(t *testing.T) {
	for _, name := range []string{"cached-home", "config-path", "project-path", "registry-owner", "registry-entry", "home-lease", "namespace"} {
		t.Run(name, func(t *testing.T) {
			parent, s := dialectArtifactExecutionHome(t)
			e := dialectArtifactExecutionStart(t, s, false)
			work, executor := dialectArtifactExecutionFDs(t, e)
			home, config, project, lease := s.home, s.configDir, s.projectDir, parent.homeLease
			registryPath := filepath.Join(config, deskHomeRegistryName)
			raw := dialectArtifactOwnerRead(t, registryPath)
			store := e.writer.lease.store
			fired, restored := false, false
			testHookDialectArtifactOwnerStep = func(at string) error {
				if at == "work.sync" && !fired {
					fired = true
					switch name {
					case "cached-home":
						copy := *s.home
						copy.Path = filepath.Join(t.TempDir(), "foreign-home")
						s.home = &copy
					case "config-path":
						s.configDir = t.TempDir()
					case "project-path":
						s.projectDir = t.TempDir()
					case "registry-owner":
						changed := bytes.Replace(raw, []byte(strings.Repeat("a", 64)), []byte(strings.Repeat("d", 64)), 1)
						if err := os.WriteFile(registryPath, changed, 0600); err != nil {
							return err
						}
					case "registry-entry":
						changed := bytes.Replace(raw, []byte(`"folder":"child"`), []byte(`"folder":"missing-child"`), 1)
						if err := os.WriteFile(registryPath, changed, 0600); err != nil {
							return err
						}
					case "home-lease":
						parent.homeLease = nil
					case "namespace":
						e.writer.lease.store.project = strings.Repeat("f", 64)
					}
				}
				if at == "data.close" && fired && !restored {
					s.home, s.configDir, s.projectDir, parent.homeLease = home, config, project, lease
					if err := os.WriteFile(registryPath, raw, 0600); err != nil {
						return err
					}
					e.writer.lease.store = store
					restored = true
				}
				return nil
			}
			t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
			dialectArtifactExecutionRefused(t, e, t.Context())
			testHookDialectArtifactOwnerStep = nil
			if !fired || !restored {
				t.Fatal("home mutation barrier not reached", fired, restored)
			}
			dialectArtifactExecutionLive(t, e, work, executor)
			if _, err := lease.Stat(); err != nil {
				t.Fatal("original home lease lost", err)
			}
		})
	}
}

func TestDialectArtifactExecutionOwnerHomeFaultsAndEdges(t *testing.T) {
	for _, step := range []string{"home-registry.sync", "home-anchor.sync", "home-anchor.selected-readback", "home-registry.close", "home-root.close", "home-hold.close", "home-edge.close"} {
		t.Run(step, func(t *testing.T) {
			parent, _ := dialectArtifactExecutionHome(t)
			e := dialectArtifactExecutionStart(t, parent, false)
			work, executor := dialectArtifactExecutionFDs(t, e)
			seen := false
			injected := errors.New("native home durability fault")
			testHookDialectArtifactOwnerStep = func(at string) error {
				if at == step {
					seen = true
					return injected
				}
				return nil
			}
			t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
			got, err := e.enrollDialectArtifactOwner(t.Context())
			testHookDialectArtifactOwnerStep = nil
			if !seen || !errors.Is(err, injected) || got != (dialectArtifactOwnerEnrollment{}) {
				t.Fatal("home fault acknowledged or not reached", seen, got, err)
			}
			dialectArtifactExecutionLive(t, e, work, executor)
			_, err = parent.homeLease.Stat()
			assistantRunMust(t, err)
		})
	}
	for _, selected := range []string{deskHomePlatform, "startup"} {
		for _, closeFault := range []bool{false, true} {
			t.Run(selected+map[bool]string{false: "/edge", true: "/edge-close"}[closeFault], func(t *testing.T) {
				parent, _ := dialectArtifactExecutionHome(t)
				e := dialectArtifactExecutionStart(t, parent, false)
				work, executor := dialectArtifactExecutionFDs(t, e)
				fired, armed, restored := false, false, false
				path := filepath.Join(parent.home.Path, selected)
				injected := errors.New("native home edge Close fault")
				inspection := func(name string) error {
					if !armed || fired || name != selected {
						return nil
					}
					fired = true
					if err := os.Rename(path, path+".held"); err != nil {
						return err
					}
					return os.Symlink(selected+".held", path)
				}
				ctx := context.WithValue(t.Context(), dialectArtifactHomeEdgeInspectionKey{}, inspection)
				testHookDialectArtifactOwnerStep = func(at string) error {
					if at == "work.sync" {
						armed = true
					}
					if fired && closeFault && at == "home-edge.close" {
						return injected
					}
					if at == "data.close" && fired && !restored {
						if err := os.Remove(path); err != nil {
							return err
						}
						if err := os.Rename(path+".held", path); err != nil {
							return err
						}
						restored = true
					}
					return nil
				}
				t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
				got, err := e.enrollDialectArtifactOwner(ctx)
				testHookDialectArtifactOwnerStep = nil
				if !fired || !restored || err == nil || got != (dialectArtifactOwnerEnrollment{}) || closeFault && !errors.Is(err, injected) {
					t.Fatal("native home stale edge acknowledged", fired, got, err)
				}
				dialectArtifactExecutionLive(t, e, work, executor)
			})
		}
	}
}

func TestDialectArtifactExecutionOwnerPresentV2AndFinalSelection(t *testing.T) {
	for _, historical := range []bool{false, true} {
		for _, name := range []string{"leaf", "shard", "table", "corrupt", "final-branch"} {
			t.Run(map[bool]string{false: "current/", true: "historical/"}[historical]+name, func(t *testing.T) {
				s, e := dialectArtifactExecutionFixture(t, false)
				if historical {
					owner, legacy := dialectArtifactExecutionLegacy(t, e, true)
					dialectArtifactLocationWriteCurrentV2(t, s, owner, legacy)
				}
				work, executor := dialectArtifactExecutionFDs(t, e)
				first, err := e.enrollDialectArtifactOwner(t.Context())
				assistantRunMust(t, err)
				path := dialectArtifactLocationPath(s, first.Owner)
				if name == "shard" {
					path = filepath.Dir(path)
				}
				if name == "table" || name == "final-branch" {
					path = filepath.Dir(filepath.Dir(path))
				}
				fired := false
				if name == "corrupt" {
					assistantRunMust(t, os.WriteFile(path, []byte(`{}`), 0600))
					fired = true
				}
				testHookDialectArtifactOwnerStep = func(at string) error {
					boundary := "work-domain.readback"
					if name == "final-branch" {
						boundary = "selected-readback"
					}
					if fired || at != boundary {
						return nil
					}
					fired = true
					return os.Rename(path, path+".held")
				}
				t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
				dialectArtifactExecutionRefused(t, e, t.Context())
				testHookDialectArtifactOwnerStep = nil
				if !fired {
					t.Fatal("present v2 disappearance not exercised")
				}
				if name != "corrupt" {
					if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("present v2 was recreated", err)
					}
					assistantRunMust(t, os.Rename(path+".held", path))
				}
				dialectArtifactExecutionLive(t, e, work, executor)
			})
		}
	}
}

func TestDialectArtifactExecutionOwnerDurableRefusals(t *testing.T) {
	for _, name := range []string{"missing-run", "wrong-id", "binding", "project", "revision", "admitted", "terminal", "historical", "missing-checkpoint", "complete-checkpoint", "plan"} {
		t.Run(name, func(t *testing.T) {
			s, e := dialectArtifactExecutionFixture(t, false)
			work, executor := dialectArtifactExecutionFDs(t, e)
			store := e.writer.lease.store
			// Handwritten negative mutations use the real admitted ledger, never
			// generated expected results. Restore its exact bytes before drain.
			lock, err := s.privateDataLockContext(t.Context(), true)
			assistantRunMust(t, err)
			data, err := s.openChatData()
			assistantRunMust(t, err)
			raw, err := readPrivateData(data.root, store.name, assistantRunRecordLimit)
			assistantRunMust(t, err)
			var ledger assistantRunLedger
			assistantRunMust(t, json.Unmarshal(raw, &ledger))
			run := &ledger.Runs[0]
			switch name {
			case "missing-run":
				ledger.Runs = nil
			case "wrong-id":
				run.ID = "arun_" + strings.Repeat("f", 32)
			case "binding":
				run.Binding.Session = strings.Repeat("f", 64)
			case "project":
				run.Binding.Project = strings.Repeat("f", 64)
			case "revision":
				run.Revision++
			case "admitted":
				run.State = "admitted"
			case "terminal":
				run.State = "complete"
			case "historical":
				run.State, run.Historical = "interrupted", true
			case "missing-checkpoint":
				run.Checkpoint = nil
			case "complete-checkpoint":
				run.Checkpoint.Status = "complete"
			case "plan":
				run.Binding.Phase = "plan"
			}
			changed, err := json.Marshal(ledger)
			assistantRunMust(t, err)
			assistantRunMust(t, writePrivateData(data.root, store.name, changed))
			assistantRunMust(t, lock.Close())
			dialectArtifactExecutionRefused(t, e, t.Context())
			lock, err = s.privateDataLockContext(t.Context(), true)
			assistantRunMust(t, err)
			assistantRunMust(t, writePrivateData(data.root, store.name, raw))
			assistantRunMust(t, data.root.Close())
			assistantRunMust(t, lock.Close())
			dialectArtifactExecutionLive(t, e, work, executor)
		})
	}
}

func TestDialectArtifactExecutionOwnerClosedOriginalFDRefusal(t *testing.T) {
	for _, name := range []string{"work", "executor"} {
		t.Run(name, func(t *testing.T) {
			_, e := dialectArtifactExecutionFixture(t, false)
			l := e.writer.lease
			l.mu.Lock()
			file := l.work
			if name == "executor" {
				file = l.file
			}
			assistantRunMust(t, file.Close())
			l.mu.Unlock()
			dialectArtifactExecutionRefused(t, e, t.Context())
			if _, err := file.Stat(); err == nil {
				t.Fatal("closed original descriptor resurrected")
			}
		})
	}
}

func dialectArtifactExecutionJournal(t *testing.T, e *assistantRunExecution) ([]string, []byte) {
	t.Helper()
	s := e.service.server
	lock, err := s.privateDataLockContext(t.Context(), true)
	assistantRunMust(t, err)
	data, err := s.openChatData()
	assistantRunMust(t, err)
	names := dialectArtifactOwnerNames(t, data.root)
	raw, err := readPrivateData(data.root, e.writer.lease.store.name, assistantRunRecordLimit)
	assistantRunMust(t, err)
	assistantRunMust(t, data.root.Close())
	assistantRunMust(t, lock.Close())
	return names, raw
}

func TestDialectArtifactExecutionOwnerConcurrentStop(t *testing.T) {
	_, e := dialectArtifactExecutionFixture(t, true)
	work, executor := dialectArtifactExecutionFDs(t, e)
	begun, stopped := make(chan struct{}), make(chan struct{})
	seen := false
	testHookDialectArtifactOwnerStep = func(at string) error {
		if seen || at != "owner.publish" {
			return nil
		}
		seen = true
		go func() { close(begun); e.cancel(); close(stopped) }()
		<-begun
		select {
		case <-stopped:
			t.Error("Stop crossed held canonical admission")
		default:
		}
		gotWork, err := e.writer.lease.work.Stat()
		if err != nil || !os.SameFile(work, gotWork) {
			return errors.Join(errDialectArtifactOwner, err)
		}
		gotExecutor, err := e.writer.lease.file.Stat()
		if err != nil || !os.SameFile(executor, gotExecutor) {
			return errors.Join(errDialectArtifactOwner, err)
		}
		return nil
	}
	t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
	got, err := e.enrollDialectArtifactOwner(t.Context())
	testHookDialectArtifactOwnerStep = nil
	if !seen || err != nil && got != (dialectArtifactOwnerEnrollment{}) {
		t.Fatal("Stop race acknowledged failed enrollment", seen, got, err)
	}
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop did not resume")
	}
	dialectArtifactExecutionDrain(t, e)
}

// Handwritten legacy producer bytes. The live execution and its descriptor
// identities are still supplied only by the real native service fixture.
func dialectArtifactExecutionLegacy(t *testing.T, e *assistantRunExecution, historical bool) (string, []byte) {
	t.Helper()
	s := e.service.server
	const owner = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	project, err := s.root.Stat(".")
	assistantRunMust(t, err)
	work, err := s.assistant.root.Stat(".")
	assistantRunMust(t, err)
	lock, _ := dialectArtifactExecutionFDs(t, e)
	projectPath, workPath := s.projectDir, s.configDir
	if historical {
		projectPath, workPath = "/historical-never-open/project", "/historical-never-open/settings"
	}
	formula, err := json.Marshal([]string{"dialect-artifact-work-v1", identityKey(work), identityKey(lock)})
	assistantRunMust(t, err)
	raw := []byte(`{"version":1,"owner":"` + owner + `","kind":"file","pathBytes":"` + base64.StdEncoding.EncodeToString([]byte(projectPath)) + `","folderIdentity":"` + identityKey(project) + `","work":{"kind":"settings","id":"` + digestOf(formula) + `","pathBytes":"` + base64.StdEncoding.EncodeToString([]byte(workPath)) + `","folderIdentity":"` + identityKey(work) + `"}}`)
	path := dialectArtifactOwnerTablePath(s, "owners", owner[:2], owner[2:]+".json")
	assistantRunMust(t, os.MkdirAll(filepath.Dir(path), 0700))
	assistantRunMust(t, os.MkdirAll(filepath.Join(s.projectDir, ".desk-private"), 0700))
	assistantRunMust(t, os.WriteFile(path, raw, 0600))
	assistantRunMust(t, os.WriteFile(filepath.Join(s.projectDir, ".desk-private", dialectArtifactOwnerMarker), []byte(`{"version":1,"owner":"`+owner+`"}`), 0600))
	return owner, raw
}

func TestDialectArtifactExecutionOwnerHistoricalV1Selection(t *testing.T) {
	for _, present := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing-v2", true: "present-v2"}[present], func(t *testing.T) {
			s, e := dialectArtifactExecutionFixture(t, false)
			work, executor := dialectArtifactExecutionFDs(t, e)
			owner, legacy := dialectArtifactExecutionLegacy(t, e, true)
			if present {
				dialectArtifactLocationWriteCurrentV2(t, s, owner, legacy)
			}
			got, err := e.enrollDialectArtifactOwner(t.Context())
			if present {
				if err != nil || got.LegacyOwnerSHA256 != digestOf(legacy) {
					t.Fatal("historical live v2 selected wrong root", got, err)
				}
			} else if err == nil || got != (dialectArtifactOwnerEnrollment{}) {
				t.Fatal("historical live v1 reconstructed absent v2", got, err)
			}
			if !bytes.Equal(legacy, dialectArtifactOwnerRead(t, dialectArtifactOwnerTablePath(s, "owners", owner[:2], owner[2:]+".json"))) {
				t.Fatal("live enrollment rewrote historical v1")
			}
			dialectArtifactExecutionLive(t, e, work, executor)
		})
	}
}
