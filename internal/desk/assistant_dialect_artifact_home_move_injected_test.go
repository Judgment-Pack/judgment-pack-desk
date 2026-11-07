package desk

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type injectedHomeMoveFixture struct {
	cfg          Config
	server       *Server
	http         *httptest.Server
	provider     *persistentInjectedArtifactProvider
	borrowedRoot *os.Root
	providerPath string
	storageInfo  os.FileInfo
	projectInfo  os.FileInfo
	request      *http.Request
	owner        dialectArtifactOwnerEnrollment
	raw          []byte
	immutable    map[string][]byte
	ledgers      map[string][]byte
}

// Reuse the durable provider fixture, but place its actual inode inside the
// settings root before construction. Neither restart nor the journal selects
// a provider root: the test host supplies a new descriptor after recovery.
func newInjectedHomeMoveFixture(t *testing.T, consolidate bool) *injectedHomeMoveFixture {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	cfg, _ := homeFixture(t)
	if consolidate {
		cfg.ProjectDir, cfg.DeskConfigDir = t.TempDir(), t.TempDir()
		privateFixture(t, filepath.Join(cfg.ProjectDir, projectConfigName), `{"deskConfigVersion":1}`)
	}
	cfg.HomeMoveShutdown = func() {}
	cfg.CodexBin = "off"
	p, registry, root, oldProviderPath := injectedArtifactProviderFixture(t)
	providerPath := filepath.Join(cfg.DeskConfigDir, "injected-storage")
	assistantRunMust(t, os.Rename(oldProviderPath, providerPath))
	projectInfo, err := os.Stat(cfg.ProjectDir)
	assistantRunMust(t, err)
	construction, err := newRetainedArtifactServerConstruction(registry, map[string]string{identityKey(projectInfo): p.id})
	assistantRunMust(t, err)
	cfg.retainedArtifactConstruction = construction
	s, ts := startDesk(t, cfg)
	t.Cleanup(func() {
		testHookDialectArtifactOwnerStep = nil
		testHomeMigrationStep = nil
		ts.Close()
		_ = s.Close()
	})
	if s.retainedArtifacts != p || s.retainedArtifactProviders != registry {
		t.Fatal("New did not adopt the exact registered provider")
	}
	if consolidate != (s.home == nil) {
		t.Fatal("fixture selected the wrong home lifecycle")
	}
	if code, _ := chatRequest(t, ts, "PUT", "absent", storageChat, true, ""); code != 200 {
		t.Fatal("initialize actual chat storage", code)
	}
	stampMoveIdentity(t, s)
	request := injectedHomeMoveRequest(t, s)
	raw := []byte(" \n{\"retained\":1.00}\n\x00\xff")
	owner := injectedArtifactPublish(t, s, request, raw)
	injectedArtifactExpectedRows(t, s, providerPath, owner)
	hostOwner, hostScope := dialectArtifactOwnerRowPaths(s, owner)
	immutable := map[string][]byte{}
	for _, path := range []string{
		hostOwner, hostScope,
		filepath.Join(s.projectDir, ".desk-private", dialectArtifactOwnerMarker),
		filepath.Join(providerPath, "d5-provider.json"),
		filepath.Join(providerPath, "d5-owners", owner.Owner[:2], owner.Owner[2:]+".json"),
		filepath.Join(providerPath, "d5-native", owner.Namespace, owner.Owner+".json"),
		filepath.Join(providerPath, "d5-objects", owner.Owner, owner.Namespace, artifactReference(raw).SHA256),
	} {
		immutable[path] = dialectArtifactOwnerRead(t, path)
	}
	data, err := s.openChatData()
	assistantRunMust(t, err)
	namespace, err := assistantRunNamespace(data.root, s.conversationIdentity())
	assistantRunMust(t, err)
	if namespace != owner.Namespace {
		t.Fatal("artifact and actual chat selected different namespaces")
	}
	ledgers := map[string][]byte{
		"assistant-runs-" + namespace + ".json":             []byte(`{ "version":1,"project":"` + namespace + `","runs":[]}`),
		"assistant-source-approvals-" + namespace + ".json": []byte(`{"version":1,"project":"` + namespace + `","reviews":[],"approvals":[]}`),
	}
	for name, value := range ledgers {
		assistantRunMust(t, writePrivateData(data.root, name, value))
	}
	assistantRunMust(t, data.root.Close())
	return &injectedHomeMoveFixture{cfg: cfg, server: s, http: ts, provider: p, borrowedRoot: root,
		providerPath: providerPath, storageInfo: p.info, projectInfo: projectInfo,
		request: request, owner: owner, raw: raw, immutable: immutable,
		ledgers: ledgers}
}

func injectedHomeMoveRequest(t *testing.T, s *Server) *http.Request {
	t.Helper()
	token, err := s.sessions.createExpiring("home-move-owner", nil, "", "")
	assistantRunMust(t, err)
	r := httptest.NewRequest("POST", "http://localhost/private-injected-home-move", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	return r.WithContext(t.Context())
}

func (f *injectedHomeMoveFixture) queue(t *testing.T, closeFault string) (string, *deskHomeTransaction, string) {
	t.Helper()
	destination := filepath.Join(t.TempDir(), "next-home")
	plan, err := f.server.planDeskHome(destination)
	if err != nil || len(plan.Blockers) != 0 || plan.Retained == nil || len(plan.Retained.Projects) != 1 {
		t.Fatal("injected home plan", plan.Blockers, err)
	}
	selected := plan.Retained.Projects[0]
	if selected.Kind != "injected" || selected.Provider != f.provider.id || selected.Owner != f.owner.Owner || selected.Namespace != f.owner.Namespace {
		t.Fatal("plan lost original selected provider/owner", selected)
	}
	if _, err = os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("preview created destination", err)
	}
	failure, reached := errors.New("borrowed provider queue Close uncertain"), 0
	if closeFault != "" {
		testHookDialectArtifactOwnerStep = func(at string) error {
			if at == closeFault {
				reached++
				return failure
			}
			return nil
		}
	}
	err = f.server.queueHomeMove(context.Background(), projectMoveRequest{Destination: destination, Token: plan.Token})
	testHookDialectArtifactOwnerStep = nil
	if closeFault == "" && err != nil || closeFault != "" && (!errors.Is(err, failure) || reached != 1) {
		t.Fatal("queue Close outcome", closeFault, reached, err)
	}
	journal := homeJournalPath(f.server.configDir)
	tx, err := readHomeTransaction(journal)
	if err != nil || tx == nil || tx.Retained == nil || len(tx.Renames) == 0 || tx.Complete {
		t.Fatal("queue did not retain exact intent", err)
	}
	pointer := tx.Writes[len(tx.Writes)-1].Path
	assertDialectArtifactHomeMoveUnacknowledged(t, journal, pointer)
	if !f.server.homeMove.queued.Load() || !f.server.location.moving.Load() {
		t.Fatal("saved queue did not retire ordinary admission")
	}
	if got, out, err := injectedArtifactInspect(f.server, f.request, artifactReference(f.raw)); err == nil || got != nil || out != (dialectArtifactOwnerEnrollment{}) {
		t.Fatal("ordinary artifact read escaped queued gate", out, err)
	}
	if err = f.server.queueHomeMove(t.Context(), projectMoveRequest{Destination: destination, Token: plan.Token}); err == nil {
		t.Fatal("queued move admitted another confirmation")
	}
	return journal, tx, pointer
}

func (f *injectedHomeMoveFixture) shutdown(t *testing.T) error {
	t.Helper()
	f.http.Close()
	err := f.server.Close()
	assistantRunMust(t, f.borrowedRoot.Close())
	if !f.provider.revoked.Load() {
		t.Fatal("shutdown retained original provider authority")
	}
	if _, e := f.provider.root.Stat("."); e == nil {
		t.Fatal("shutdown did not close the provider-owned root")
	}
	if got, e := f.provider.Get(t.Context(), artifactReference(f.raw)); e == nil || got != nil {
		t.Fatal("old provider remained usable after shutdown")
	}
	return err
}

func (f *injectedHomeMoveFixture) reopenAndRead(t *testing.T, journal string, tx *deskHomeTransaction) {
	t.Helper()
	// At this point the process has not constructed or OPENed any provider.
	// Recovery has only the bounded host intent and original filesystem pins.
	testHookDialectArtifactOwnerStep = func(at string) error {
		if strings.HasPrefix(at, "injected-provider") || strings.HasPrefix(at, "injected-constructor") {
			t.Error("pre-New recovery entered provider construction or ownership", at)
		}
		return nil
	}
	assertDialectArtifactHomeMoveAcknowledged(t, journal, tx.Writes[len(tx.Writes)-1].Path)
	testHookDialectArtifactOwnerStep = nil
	if _, err := f.provider.root.Stat("."); err == nil || !f.provider.revoked.Load() {
		t.Fatal("host-only recovery resurrected the old provider")
	}
	for path, before := range f.immutable {
		if got := dialectArtifactOwnerRead(t, tx.relocate(path)); !bytes.Equal(got, before) {
			t.Fatal("move changed original immutable bytes", filepath.Base(path))
		}
	}
	providerPath := tx.relocate(f.providerPath)
	info, err := os.Stat(providerPath)
	if err != nil || !os.SameFile(info, f.storageInfo) {
		t.Fatal("provider storage inode changed", err)
	}
	root, err := os.OpenRoot(providerPath) // trusted host selection, never a journal provider locator
	assistantRunMust(t, err)
	t.Cleanup(func() { _ = root.Close() })
	opened, err := openPersistentInjectedArtifactProvider(t.Context(), injectedFixtureProviderID, root)
	assistantRunMust(t, err)
	if opened == f.provider || opened.id != f.provider.id || !os.SameFile(opened.info, f.storageInfo) || !bytes.Equal(opened.identity, f.provider.identity) {
		t.Fatal("trusted OPEN lost original provider/storage identity")
	}
	registry, err := newRetainedArtifactProviderRegistry(opened)
	assistantRunMust(t, err)
	t.Cleanup(func() { _ = registry.Close() })
	construction, err := newRetainedArtifactServerConstruction(registry, map[string]string{identityKey(f.projectInfo): opened.id})
	assistantRunMust(t, err)
	cfg := f.cfg
	cfg.ProjectDir = tx.relocate(f.cfg.ProjectDir)
	cfg.DeskConfigDir = filepath.Join(tx.Destination, deskHomePlatform)
	cfg.Port = testPort
	cfg.retainedArtifactConstruction = construction
	again, err := New(cfg)
	assistantRunMust(t, err)
	t.Cleanup(func() { _ = again.Close() })
	if again.retainedArtifacts != opened || again.retainedArtifactProviders != registry || registry.byID[opened.id].provider != opened {
		t.Fatal("restart New substituted the configured provider instance")
	}
	projectInfo, err := again.root.Stat(".")
	if err != nil || !os.SameFile(projectInfo, f.projectInfo) {
		t.Fatal("project inode changed", err)
	}
	got, out, err := injectedArtifactInspect(again, injectedHomeMoveRequest(t, again), artifactReference(f.raw))
	if err != nil || !bytes.Equal(got, f.raw) || out.Owner != f.owner.Owner || out.Namespace != f.owner.Namespace || out.WorkDomain != f.owner.WorkDomain || out.OwnerSHA256 != f.owner.OwnerSHA256 || out.ScopeSHA256 != f.owner.ScopeSHA256 {
		t.Fatal("actual restarted artifact read lost original binding", out, err)
	}
	injectedArtifactExpectedRows(t, again, providerPath, out)
	data, err := again.openChatData()
	assistantRunMust(t, err)
	namespace, err := assistantRunNamespace(data.root, again.conversationIdentity())
	assistantRunMust(t, err)
	if namespace != f.owner.Namespace {
		t.Fatal("restart selected another saved namespace")
	}
	for name, expected := range f.ledgers {
		got, err := readPrivateData(data.root, name, 1<<20)
		if err != nil || !bytes.Equal(got, expected) {
			t.Fatal("restart changed exact namespace journal bytes", name, err)
		}
	}
	assistantRunMust(t, data.root.Close())
}

func TestDialectArtifactHomeMoveInjectedConstructorReplay(t *testing.T) {
	for _, consolidate := range []bool{false, true} {
		for _, interrupted := range []bool{false, true} {
			name := map[bool]string{false: "whole", true: "consolidate"}[consolidate]
			if interrupted {
				name += "-landed-rename"
			}
			t.Run(name, func(t *testing.T) {
				f := newInjectedHomeMoveFixture(t, consolidate)
				journal, tx, pointer := f.queue(t, "")
				assistantRunMust(t, f.shutdown(t))
				if interrupted {
					failure := errors.New("injected home rename directory acknowledgement uncertain")
					for attempt := 0; attempt < 2; attempt++ {
						reached := false
						testHomeMigrationStep = func(at string) error {
							if at == "rename-target-directory-sync" {
								reached = true
								return failure
							}
							return nil
						}
						if err := finishHomeTransaction(journal); !errors.Is(err, failure) || !reached {
							t.Fatal("continuing landed rename failure admitted recovery", attempt, reached, err)
						}
						assertDialectArtifactHomeMoveUnacknowledged(t, journal, pointer)
					}
					op := tx.Renames[0]
					landed, err := os.Stat(op.To)
					if err != nil || identityKey(landed) != op.Identity {
						t.Fatal("interruption did not preserve landed rename inode", err)
					}
					if _, err = os.Lstat(op.From); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("landed rename source still present", err)
					}
					testHomeMigrationStep = nil
				}
				f.reopenAndRead(t, journal, tx)
			})
		}
	}
}

func TestDialectArtifactHomeMoveInjectedOwnedClose(t *testing.T) {
	for _, phase := range []string{"queue", "recovery", "shutdown"} {
		t.Run(phase, func(t *testing.T) {
			f := newInjectedHomeMoveFixture(t, false)
			queueFault := ""
			if phase == "queue" {
				queueFault = "home-move-queue.close"
			}
			journal, tx, pointer := f.queue(t, queueFault)
			failure, reached := errors.New("owned participant Close uncertain"), 0
			if phase == "shutdown" {
				testHookDialectArtifactOwnerStep = func(at string) error {
					if at == "injected-provider-root.close" {
						reached++
						return failure
					}
					return nil
				}
			}
			err := f.shutdown(t)
			if phase == "shutdown" {
				if !errors.Is(err, failure) || reached != 1 {
					t.Fatal("real provider-owned Close uncertainty lost", reached, err)
				}
				if err = f.server.Close(); !errors.Is(err, failure) || reached != 1 {
					t.Fatal("shutdown Close was repeated or lost its original error", reached, err)
				}
				// main returns the Close error before FinishDeskHomeMove. The
				// old error is not a durable provider-failure quarantine; only a
				// later process proceeds with host recovery and trusted OPEN.
				assertDialectArtifactHomeMoveUnacknowledged(t, journal, pointer)
			} else {
				assistantRunMust(t, err)
			}
			testHookDialectArtifactOwnerStep = nil
			if phase == "recovery" {
				for attempt := 0; attempt < 2; attempt++ {
					reached = 0
					testHookDialectArtifactOwnerStep = func(at string) error {
						if strings.HasPrefix(at, "injected-provider") || strings.HasPrefix(at, "injected-constructor") {
							t.Error("pre-New recovery entered provider construction or ownership", at)
						}
						if at == "home-move-metadata.close" {
							reached++
							return failure
						}
						return nil
					}
					if err = finishHomeTransaction(journal); !errors.Is(err, failure) || reached != 1 {
						t.Fatal("continuing retained metadata Close failure admitted recovery", attempt, reached, err)
					}
					assertDialectArtifactHomeMoveUnacknowledged(t, journal, pointer)
				}
				testHookDialectArtifactOwnerStep = nil
			}
			f.reopenAndRead(t, journal, tx)
		})
	}
}
