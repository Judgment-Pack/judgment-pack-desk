package desk

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func selectedProviderFixture(t *testing.T, id string) *persistentInjectedArtifactProvider {
	t.Helper()
	path := t.TempDir()
	assistantRunMust(t, os.Chmod(path, 0700))
	root, err := os.OpenRoot(path)
	assistantRunMust(t, err)
	t.Cleanup(func() { _ = root.Close() })
	provider, err := newPersistentInjectedArtifactProvider(t.Context(), id, root)
	assistantRunMust(t, err)
	t.Cleanup(func() { _ = provider.Close() })
	return provider
}

func TestDialectArtifactSelectedProviderOriginalOperations(t *testing.T) {
	a, b := selectedProviderFixture(t, strings.Repeat("a", 64)), selectedProviderFixture(t, strings.Repeat("b", 64))
	registry, err := newRetainedArtifactProviderRegistry(a, b)
	assistantRunMust(t, err)
	s := &Server{retainedArtifactProviders: registry, retainedArtifacts: a}
	// Saved history selected b. The live Put provider a has no role in this
	// original pin, including when the current default changes mid-request.
	pins, err := pinDialectArtifactSelectedProviders(t.Context(), s, []string{b.id, b.id})
	assistantRunMust(t, err)
	defer pins.Close()
	if len(pins.ordered) != 1 || pins.ordered[0].registration != registry.byID[b.id] || pins.ordered[0].operation.provider != b {
		t.Fatal("did not retain the exact selected registered instance")
	}
	s.retainedArtifacts = nil
	assistantRunMust(t, pins.check(t.Context()))
	copy := *pins
	if copy.check(t.Context()) == nil || copy.Close() == nil || pins.ordered[0].operation.finished.Load() {
		t.Fatal("copied set checked or finished the original operation")
	}
	assistantRunMust(t, pins.Close())
	assistantRunMust(t, pins.Close())
	if !pins.ordered[0].operation.finished.Load() || pins.check(t.Context()) == nil || pins.ordered[0].check(t.Context()) == nil {
		t.Fatal("finished operation remained available")
	}
}

func TestDialectArtifactSelectedProviderStableOrderingAndRegistry(t *testing.T) {
	a, b := selectedProviderFixture(t, strings.Repeat("a", 64)), selectedProviderFixture(t, strings.Repeat("b", 64))
	registry, err := newRetainedArtifactProviderRegistry(a, b)
	assistantRunMust(t, err)
	s := &Server{retainedArtifactProviders: registry}
	pins, err := pinDialectArtifactSelectedProviders(t.Context(), s, []string{b.id, a.id, b.id})
	assistantRunMust(t, err)
	defer pins.Close()
	if len(pins.ordered) != 2 || pins.ordered[0].registration.provider != a || pins.ordered[1].registration.provider != b {
		t.Fatal("selection was not deduplicated and ordered by stable provider ID")
	}
	old := registry.byID[b.id]
	registry.byID[b.id] = &retainedArtifactProviderRegistration{provider: b, root: old.root, info: old.info, identity: old.identity, registry: registry}
	if pins.check(t.Context()) == nil {
		t.Fatal("registration replacement retained source authority")
	}
	registry.byID[b.id] = old
	assistantRunMust(t, pins.check(t.Context()))
	s.retainedArtifactProviders = nil
	if pins.check(t.Context()) == nil {
		t.Fatal("registry replacement retained source authority")
	}
	// Cleanup owns its original operations even after trusted registry changes.
	assistantRunMust(t, pins.Close())
}

func TestDialectArtifactSelectedProviderInvalidAndUnwound(t *testing.T) {
	p := selectedProviderFixture(t, strings.Repeat("a", 64))
	registry, err := newRetainedArtifactProviderRegistry(p)
	assistantRunMust(t, err)
	s := &Server{retainedArtifactProviders: registry, retainedArtifacts: p}
	for _, ids := range [][]string{{""}, {"../provider"}, {strings.Repeat("b", 64)}, {p.id, strings.Repeat("b", 64)}} {
		if pins, err := pinDialectArtifactSelectedProviders(t.Context(), s, ids); err == nil || pins != nil {
			t.Fatal("unknown selection was accepted", ids)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if pins, err := pinDialectArtifactSelectedProviders(ctx, s, []string{p.id}); err == nil || pins != nil {
		t.Fatal("cancelled selection was accepted")
	}
	// A partial pin from the final unknown-ID case above must have finished;
	// provider Close would otherwise wait forever on its real active WaitGroup.
	assistantRunMust(t, p.Close())
	if pins, err := pinDialectArtifactSelectedProviders(t.Context(), s, []string{p.id}); err == nil || pins != nil {
		t.Fatal("revoked selected provider was accepted")
	}
}

func TestDialectArtifactSelectedProviderRevocationFinish(t *testing.T) {
	p := selectedProviderFixture(t, strings.Repeat("a", 64))
	registry, err := newRetainedArtifactProviderRegistry(p)
	assistantRunMust(t, err)
	pins, err := pinDialectArtifactSelectedProviders(t.Context(), &Server{retainedArtifactProviders: registry}, []string{p.id})
	assistantRunMust(t, err)
	defer pins.Close()
	// Exercise the exact production revocation latch without starting a
	// goroutine that would wait for this lexical caller to finish its pin.
	p.revoked.Store(true)
	if pins.check(t.Context()) == nil {
		t.Fatal("revocation was hidden behind the retained root")
	}
	err = pins.Close()
	if !errors.Is(err, errRetainedArtifactAuthorityUnavailable) || !pins.ordered[0].operation.finished.Load() || pins.Close() != err {
		t.Fatal("original finish failure was dropped or retried", err)
	}
}

func selectedWorkFixture(t *testing.T, path string) dialectArtifactWorkDomainRecord {
	t.Helper()
	assistantRunMust(t, os.MkdirAll(path, 0700))
	assistantRunMust(t, os.Chmod(path, 0700))
	assistantRunMust(t, os.WriteFile(filepath.Join(path, ".data-work.lock"), nil, 0600))
	folder, err := os.Lstat(path)
	assistantRunMust(t, err)
	lock, err := os.Lstat(filepath.Join(path, ".data-work.lock"))
	assistantRunMust(t, err)
	return dialectArtifactWorkDomainRecord{Version: 1, Kind: "settings", PathBytes: []byte(path),
		ID: dialectArtifactWorkDomainID(identityKey(folder), identityKey(lock)), FolderIdentity: identityKey(folder), LockIdentity: identityKey(lock)}
}

func TestDialectArtifactSelectedWorkSortedOnceAndNoncreating(t *testing.T) {
	a := selectedWorkFixture(t, t.TempDir())
	b := selectedWorkFixture(t, t.TempDir())
	works, err := openDialectArtifactSelectedWorkDomains(t.Context(), []dialectArtifactWorkDomainRecord{b, a, b})
	assistantRunMust(t, err)
	defer works.Close()
	if len(works.ordered) != 2 || works.ordered[0].record.ID > works.ordered[1].record.ID {
		t.Fatal("work domains were not deduplicated and sorted")
	}
	assistantRunMust(t, works.acquire(t.Context()))
	assistantRunMust(t, works.check(t.Context()))
	if works.acquire(t.Context()) == nil {
		t.Fatal("reacquired original work custody")
	}
	copy := *works
	if copy.check(t.Context()) == nil || copy.Close() == nil {
		t.Fatal("copied collection borrowed original work custody")
	}
	assistantRunMust(t, works.Close())
	if works.check(t.Context()) == nil {
		t.Fatal("closed work custody remained available")
	}
	assistantRunMust(t, os.Remove(filepath.Join(string(a.PathBytes), ".data-work.lock")))
	if got, err := openDialectArtifactSelectedWorkDomains(t.Context(), []dialectArtifactWorkDomainRecord{a}); err == nil || got != nil {
		t.Fatal("missing source lock was recreated")
	}
	if _, err := os.Lstat(filepath.Join(string(a.PathBytes), ".data-work.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("source lock lookup mutated storage", err)
	}
}

func TestDialectArtifactSelectedWorkHeldQueueAndReplacedLock(t *testing.T) {
	record := selectedWorkFixture(t, t.TempDir())
	works, err := openDialectArtifactSelectedWorkDomains(t.Context(), []dialectArtifactWorkDomainRecord{record})
	assistantRunMust(t, err)
	defer works.Close()
	queue, err := os.OpenFile(filepath.Join(string(record.PathBytes), ".data-work.lock"), os.O_RDWR, 0)
	assistantRunMust(t, err)
	assistantRunMust(t, lockPrivateData(queue, true))
	if err := works.acquire(t.Context()); !errors.Is(err, errRetainedArtifactBusy) {
		t.Fatal("busy actual work inode did not refuse boundedly", err)
	}
	assistantRunMust(t, queue.Close())
	if works.acquire(t.Context()) == nil {
		t.Fatal("refused work acquisition was retried")
	}
	assistantRunMust(t, works.Close())
	works, err = openDialectArtifactSelectedWorkDomains(t.Context(), []dialectArtifactWorkDomainRecord{record})
	assistantRunMust(t, err)
	defer works.Close()
	assistantRunMust(t, works.acquire(t.Context()))
	path := filepath.Join(string(record.PathBytes), ".data-work.lock")
	assistantRunMust(t, os.Rename(path, path+".original"))
	assistantRunMust(t, os.WriteFile(path, nil, 0600))
	if works.check(t.Context()) == nil {
		t.Fatal("replacement work inode retained original custody")
	}
}

func TestDialectArtifactSelectedWorkSettingsExactPathDomain(t *testing.T) {
	// Native settings custody accepts these legal Unix bytes. The source opener
	// must not borrow the stricter chat-storage path grammar to reject them.
	path := filepath.Join(t.TempDir(), "private\nsettings\\\"雪")
	record := selectedWorkFixture(t, path)
	works, err := openDialectArtifactSelectedWorkDomains(t.Context(), []dialectArtifactWorkDomainRecord{record})
	assistantRunMust(t, err)
	defer works.Close()
	assistantRunMust(t, works.acquire(t.Context()))
	assistantRunMust(t, works.check(t.Context()))
	assistantRunMust(t, os.Rename(path, path+"-original"))
	assistantRunMust(t, os.Mkdir(path, 0700))
	if works.check(t.Context()) == nil {
		t.Fatal("same-path settings replacement retained the old domain")
	}
}

func TestDialectArtifactSelectedWorkActualLockDeduplication(t *testing.T) {
	a := selectedWorkFixture(t, t.TempDir())
	b := selectedWorkFixture(t, t.TempDir())
	secondLock := filepath.Join(string(b.PathBytes), ".data-work.lock")
	assistantRunMust(t, os.Remove(secondLock))
	assistantRunMust(t, os.Link(filepath.Join(string(a.PathBytes), ".data-work.lock"), secondLock))
	b.LockIdentity, b.ID = a.LockIdentity, dialectArtifactWorkDomainID(b.FolderIdentity, a.LockIdentity)
	works, err := openDialectArtifactSelectedWorkDomains(t.Context(), []dialectArtifactWorkDomainRecord{a, b})
	assistantRunMust(t, err)
	defer works.Close()
	assistantRunMust(t, works.acquire(t.Context()))
	assistantRunMust(t, works.check(t.Context()))
	if works.ordered[0].lockOwner != nil || works.ordered[1].lockOwner != works.ordered[0] || works.ordered[1].held {
		t.Fatal("actual shared lock inode was flocked twice")
	}
}

func TestDialectArtifactSelectedWorkBorrowsOriginalCurrentFD(t *testing.T) {
	s, _ := dialectArtifactOwnerSetup(t)
	file, err := s.assistant.root.OpenFile(".data-work.lock", os.O_RDWR|openNoFollow|openNonBlocking, 0)
	assistantRunMust(t, err)
	defer file.Close()
	info, err := file.Stat()
	assistantRunMust(t, err)
	folder, err := s.assistant.root.Stat(".")
	assistantRunMust(t, err)
	record := dialectArtifactWorkDomainRecord{Version: 1, Kind: "settings", PathBytes: []byte(s.configDir),
		FolderIdentity: identityKey(folder), LockIdentity: identityKey(info), ID: dialectArtifactWorkDomainID(identityKey(folder), identityKey(info))}
	works, err := openDialectArtifactSelectedWorkDomains(t.Context(), []dialectArtifactWorkDomainRecord{record})
	assistantRunMust(t, err)
	defer works.Close()
	assistantRunMust(t, works.includeCurrent(t.Context(), s, file))
	if len(works.ordered) != 1 || works.ordered[0].file != file || !works.ordered[0].borrowedFile {
		t.Fatal("current original FD was duplicated or replaced")
	}
	assistantRunMust(t, works.acquire(t.Context()))
	assistantRunMust(t, works.Close())
	if _, err := file.Stat(); err != nil {
		t.Fatal("selected collection closed caller's original current FD", err)
	}
	probe, err := s.assistant.root.OpenFile(".data-work.lock", os.O_RDWR|openNoFollow|openNonBlocking, 0)
	assistantRunMust(t, err)
	defer probe.Close()
	if err := lockPrivateData(probe, true); !lockHeld(err) {
		t.Fatal("original current lock was released before its owner closed it", err)
	}
	assistantRunMust(t, file.Close())
	assistantRunMust(t, lockPrivateData(probe, true))
}

func TestDialectArtifactSelectedMetadataActualRowsAndReread(t *testing.T) {
	s, request := dialectArtifactOwnerSetup(t)
	enrolled, err := s.enrollDialectArtifactOwner(request)
	assistantRunMust(t, err)
	metadata, err := s.openDialectArtifactSelectedMetadata(t.Context())
	assistantRunMust(t, err)
	defer metadata.Close()
	owner, err := metadata.ownerRow(t.Context(), enrolled.Owner)
	assistantRunMust(t, err)
	location, err := metadata.locationRow(t.Context(), owner)
	assistantRunMust(t, err)
	_, err = metadata.scopeRow(t.Context(), dialectArtifactOwnerScope{Version: 1, Kind: "native", Owner: enrolled.Owner, Namespace: enrolled.Namespace})
	assistantRunMust(t, err)
	_, err = metadata.workRow(t.Context(), enrolled.WorkDomain)
	assistantRunMust(t, err)
	for _, row := range metadata.rows {
		assistantRunMust(t, row.check(t.Context()))
	}
	copy := *metadata
	if copy.check(t.Context()) == nil || copy.Close() == nil {
		t.Fatal("copied metadata holder retained original lifetime")
	}
	path := dialectArtifactOwnerTablePath(s, "owner-locations", enrolled.Owner[:2], enrolled.Owner[2:]+".json")
	prior := dialectArtifactOwnerRead(t, path)
	assistantRunMust(t, os.Rename(path, path+".prior"))
	assistantRunMust(t, os.WriteFile(path, prior, 0600))
	if location.check(t.Context()) == nil {
		t.Fatal("same-byte replaced location row bypassed pinned row identity")
	}
}

func TestDialectArtifactSelectedMetadataNoHistoricalLocationFallback(t *testing.T) {
	s, request := dialectArtifactOwnerSetup(t)
	enrolled, err := s.enrollDialectArtifactOwner(request)
	assistantRunMust(t, err)
	metadata, err := s.openDialectArtifactSelectedMetadata(t.Context())
	assistantRunMust(t, err)
	defer metadata.Close()
	owner, err := metadata.ownerRow(t.Context(), enrolled.Owner)
	assistantRunMust(t, err)
	path := dialectArtifactOwnerTablePath(s, "owner-locations", enrolled.Owner[:2], enrolled.Owner[2:]+".json")
	assistantRunMust(t, os.Remove(path))
	if row, err := metadata.locationRow(t.Context(), owner); err == nil || row != nil {
		t.Fatal("missing selected v2 companion fell back to native v1 path")
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("selected resolver recreated missing v2 companion", err)
	}
	wrongScope := dialectArtifactOwnerScope{Version: 1, Kind: "native", Owner: enrolled.Owner, Namespace: strings.Repeat("f", 64)}
	if row, err := metadata.scopeRow(t.Context(), wrongScope); err == nil || row != nil {
		t.Fatal("another native namespace was authorized by owner hash")
	}
}

func selectedNativeSourceFixture(t *testing.T, s *Server, enrolled dialectArtifactOwnerEnrollment) (*dialectArtifactSelectedNativeSource, *dialectArtifactSelectedWorkDomains) {
	t.Helper()
	metadata, err := s.openDialectArtifactSelectedMetadata(t.Context())
	assistantRunMust(t, err)
	t.Cleanup(func() { _ = metadata.Close() })
	owner, err := metadata.ownerRow(t.Context(), enrolled.Owner)
	assistantRunMust(t, err)
	location, err := metadata.locationRow(t.Context(), owner)
	assistantRunMust(t, err)
	workRow, err := metadata.workRow(t.Context(), enrolled.WorkDomain)
	assistantRunMust(t, err)
	record, err := readDialectArtifactWorkDomainRecord(workRow.raw)
	assistantRunMust(t, err)
	works, err := openDialectArtifactSelectedWorkDomains(t.Context(), []dialectArtifactWorkDomainRecord{record})
	assistantRunMust(t, err)
	t.Cleanup(func() { _ = works.Close() })
	source, err := openDialectArtifactSelectedNativeSource(t.Context(), owner, location, works.byID[record.ID])
	assistantRunMust(t, err)
	t.Cleanup(func() { _ = source.Close() })
	return source, works
}

func TestDialectArtifactSelectedNativeActualAbsoluteAndWorkSources(t *testing.T) {
	for _, kind := range []string{"absolute", "work"} {
		t.Run(kind, func(t *testing.T) {
			config := t.TempDir()
			project := t.TempDir()
			if kind == "work" {
				project = filepath.Join(config, "ordinary-project")
			}
			s, request := dialectArtifactOwnerFixture(t, project, config)
			enrolled, err := s.enrollDialectArtifactOwner(request)
			assistantRunMust(t, err)
			source, works := selectedNativeSourceFixture(t, s, enrolled)
			if source.location.Location.Kind != kind {
				t.Fatal("fixture did not exercise selected location", source.location.Location.Kind)
			}
			assistantRunMust(t, source.check(t.Context(), false))
			if source.check(t.Context(), true) == nil {
				t.Fatal("opened work descriptor was treated as owned EX exclusion")
			}
			assistantRunMust(t, works.acquire(t.Context()))
			assistantRunMust(t, source.check(t.Context(), true))
			copy := *source
			if copy.check(t.Context(), true) == nil || copy.Close() == nil {
				t.Fatal("copied source retained the original project lifetime")
			}
			originalMarker := dialectArtifactOwnerRead(t, filepath.Join(project, ".desk-private", dialectArtifactOwnerMarker))
			assistantRunMust(t, os.Rename(project, project+"-original"))
			assistantRunMust(t, os.MkdirAll(filepath.Join(project, ".desk-private"), 0700))
			assistantRunMust(t, os.WriteFile(filepath.Join(project, ".desk-private", dialectArtifactOwnerMarker), originalMarker, 0600))
			if source.check(t.Context(), true) == nil {
				t.Fatal("copied marker in a replacement project retained physical authority")
			}
		})
	}
}

func TestDialectArtifactSelectedNativeForeignProjectUsesItsOwnWork(t *testing.T) {
	viewer, _ := dialectArtifactOwnerSetup(t)
	foreign, request := dialectArtifactOwnerSetup(t)
	foreign.cfg.parent = viewer
	enrolled, err := foreign.enrollDialectArtifactOwner(request)
	assistantRunMust(t, err)
	// Discovery is rooted in the actual installation owner, while all project
	// and work handles come from the selected foreign rows, never viewer.root.
	source, works := selectedNativeSourceFixture(t, viewer, enrolled)
	assistantRunMust(t, works.acquire(t.Context()))
	assistantRunMust(t, source.check(t.Context(), true))
	if !os.SameFile(source.info, foreign.project.info) || os.SameFile(source.info, viewer.project.info) ||
		string(source.work.record.PathBytes) != foreign.configDir {
		t.Fatal("foreign owner substituted the ambient project or work domain")
	}
}

func TestDialectArtifactSelectedNativeNeverOpensHistoricalV1Paths(t *testing.T) {
	s, request := dialectArtifactOwnerSetup(t)
	enrolled, err := s.enrollDialectArtifactOwner(request)
	assistantRunMust(t, err)
	ownerPath, _ := dialectArtifactOwnerRowPaths(s, enrolled)
	var old dialectArtifactOwnerRecord
	assistantRunMust(t, json.Unmarshal(dialectArtifactOwnerRead(t, ownerPath), &old))
	old.PathBytes, old.Work.PathBytes = []byte("/missing/historical/project"), []byte("/missing/historical/settings")
	oldRaw := assistantChatJSON(old)
	assistantRunMust(t, os.WriteFile(ownerPath, oldRaw, 0600))
	locationPath := dialectArtifactOwnerTablePath(s, "owner-locations", enrolled.Owner[:2], enrolled.Owner[2:]+".json")
	var current dialectArtifactLocationRecord
	assistantRunMust(t, json.Unmarshal(dialectArtifactOwnerRead(t, locationPath), &current))
	current.LegacySHA256 = digestOf(oldRaw)
	assistantRunMust(t, os.WriteFile(locationPath, assistantChatJSON(current), 0600))
	source, works := selectedNativeSourceFixture(t, s, enrolled)
	assistantRunMust(t, works.acquire(t.Context()))
	assistantRunMust(t, source.check(t.Context(), true))
	if source.project == nil || source.project.dir != s.projectDir {
		t.Fatal("selected current location was not used")
	}
}

func TestDialectArtifactSelectedNativeHomeFreshRegistryAndLease(t *testing.T) {
	parent, _, child, request := dialectArtifactHomeAnchorFixture(t, "-selected")
	enrolled, err := child.enrollDialectArtifactOwner(request)
	assistantRunMust(t, err)
	source, works := selectedNativeSourceFixture(t, parent, enrolled)
	if source.home == nil || source.home.entry.ID != child.cfg.deskID || source.project != nil {
		t.Fatal("home source required a fabricated foreign Server or absolute project fallback")
	}
	assistantRunMust(t, works.acquire(t.Context()))
	assistantRunMust(t, source.check(t.Context(), true))
	data, err := child.openChatData()
	assistantRunMust(t, err)
	defer data.root.Close()
	assistantRunMust(t, source.namespace(t.Context(), data.root, dialectArtifactOwnerScope{Version: 1, Kind: "native", Owner: enrolled.Owner, Namespace: enrolled.Namespace}))
	if source.namespace(t.Context(), data.root, dialectArtifactOwnerScope{Version: 1, Kind: "native", Owner: enrolled.Owner, Namespace: strings.Repeat("f", 64)}) == nil {
		t.Fatal("home namespace ignored fresh selected history identity")
	}
	// An original native scope is required only for native history. A future
	// admitted imported run has its separately checked import-only namespace.
	assistantRunMust(t, source.namespace(t.Context(), data.root, dialectArtifactOwnerScope{Version: 1, Kind: "import", Owner: enrolled.Owner, Namespace: strings.Repeat("f", 64), RunID: "arun_" + strings.Repeat("a", 32)}))
	registryPath := filepath.Join(parent.configDir, deskHomeRegistryName)
	assistantRunMust(t, os.WriteFile(registryPath, []byte(`{"version":1}`), 0600))
	if source.check(t.Context(), true) == nil {
		t.Fatal("fresh home registry change was ignored")
	}
}
