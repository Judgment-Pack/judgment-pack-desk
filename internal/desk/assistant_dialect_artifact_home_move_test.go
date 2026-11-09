package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Actual queue -> shutdown -> pre-New finish entry. Fixtures are authored by
// hand; no provider/model or installed program supplies expected bytes.
func TestDialectArtifactHomeMoveQueueAndRecovery(t *testing.T) {
	for _, consolidate := range []bool{false, true} {
		for _, fault := range []bool{false, true} {
			name := "whole"
			if consolidate {
				name = "consolidate"
			}
			if fault {
				name += "-uncertain-cas"
			}
			t.Run(name, func(t *testing.T) {
				t.Setenv("XDG_DATA_HOME", t.TempDir())
				cfg, _ := homeFixture(t)
				if consolidate {
					cfg.ProjectDir = t.TempDir()
					cfg.DeskConfigDir = t.TempDir()
					privateFixture(t, filepath.Join(cfg.ProjectDir, projectConfigName), `{"deskConfigVersion":1}`)
				}
				cfg.HomeMoveShutdown = func() {}
				s, ts := startDesk(t, cfg)
				t.Cleanup(func() { ts.Close(); s.Close(); testHookDialectArtifactOwnerStep = nil })
				if code, _ := chatRequest(t, ts, "PUT", "absent", storageChat, true, ""); code != 200 {
					t.Fatal(code)
				}
				stampMoveIdentity(t, s)
				first := enrollDialectArtifactMoveServer(t, s)
				named := createTestDesk(t, ts, "Second retained desk")
				child := s.desks[named.ID]
				second := enrollDialectArtifactMoveServer(t, child)
				ownerPath, scopePath := dialectArtifactOwnerRowPaths(s, first)
				childOwnerPath, childScopePath := dialectArtifactOwnerRowPaths(child, second)
				original := map[string][]byte{}
				for _, path := range []string{ownerPath, scopePath, childOwnerPath, childScopePath, filepath.Join(s.projectDir, ".desk-private", dialectArtifactOwnerMarker), filepath.Join(child.projectDir, ".desk-private", dialectArtifactOwnerMarker)} {
					original[path] = dialectArtifactOwnerRead(t, path)
				}
				rawObject := []byte("retained object\n\x00\xff")
				ref, err := s.artifactRepository().Put(context.Background(), rawObject)
				if err != nil {
					t.Fatal(err)
				}
				data, err := s.openChatData()
				if err != nil {
					t.Fatal(err)
				}
				namespace, err := assistantRunNamespace(data.root, s.conversationIdentity())
				if err != nil {
					t.Fatal(err)
				}
				runName, sourceName := "assistant-runs-"+namespace+".json", "assistant-source-approvals-"+namespace+".json"
				// Original ledger bytes, including lexical whitespace, survive consolidation.
				runRaw := []byte(`{ "version":1,"project":"` + namespace + `","runs":[]}`)
				sourceRaw := []byte(`{"version":1,"project":"` + namespace + `","reviews":[],"approvals":[]}`)
				if err = writePrivateData(data.root, runName, runRaw); err != nil {
					t.Fatal(err)
				}
				if err = writePrivateData(data.root, sourceName, sourceRaw); err != nil {
					t.Fatal(err)
				}
				if err = data.root.Close(); err != nil {
					t.Fatal(err)
				}
				destination := filepath.Join(t.TempDir(), "next-home")
				plan, err := s.planDeskHome(destination)
				if err != nil || len(plan.Blockers) != 0 || plan.Retained == nil {
					t.Fatal("retained plan", plan.Blockers, err)
				}
				if _, err = os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("preview created destination", err)
				}
				if err = s.queueHomeMove(context.Background(), projectMoveRequest{Destination: destination, Token: plan.Token}); err != nil {
					t.Fatal("queue", err)
				}
				journal := homeJournalPath(s.configDir)
				tx, err := readHomeTransaction(journal)
				if err != nil || tx == nil || tx.Retained == nil || tx.Complete {
					t.Fatal("saved intent", err)
				}
				workRows := 0
				for _, row := range tx.Retained.Rows {
					if row.Table == "work-domains" {
						workRows++
					}
					if bytes.HasSuffix(row.After, []byte{'\n'}) {
						t.Fatal("raw metadata reserialized with newline")
					}
				}
				if workRows != 1 {
					t.Fatal("shared work domain not deduplicated", workRows)
				}
				if consolidate {
					fi, e := os.Lstat(destination)
					if e != nil || identityKey(fi) != tx.Retained.DestinationIdentity || !tx.Retained.DestinationCreated {
						t.Fatal("real initial home inode not frozen", e)
					}
				}
				pointer := tx.Writes[len(tx.Writes)-1].Path
				ts.Close()
				s.Close()
				if fault {
					fired := false
					testHookDialectArtifactOwnerStep = func(at string) error {
						if at == "home-move-row.after-cas" && !fired {
							fired = true
							return errors.New("interrupted after exact row CAS")
						}
						return nil
					}
					if err = finishHomeTransaction(journal); err == nil || !fired {
						t.Fatal("uncertain CAS not retained", err)
					}
					testHookDialectArtifactOwnerStep = nil
					pending, e := readHomeTransaction(journal)
					if e != nil || pending == nil || pending.Complete {
						t.Fatal("uncertain intent cleared", e)
					}
					if _, e = os.Lstat(pointer); !errors.Is(e, os.ErrNotExist) {
						t.Fatal("bootstrap completed after uncertain row", e)
					}
				}
				if err = finishHomeTransaction(journal); err != nil {
					t.Fatal("pre-New recovery", err)
				}
				done, err := readHomeTransaction(journal)
				if err != nil || done == nil || !done.Complete {
					t.Fatal("completion", err)
				}
				for path, before := range original {
					if got := dialectArtifactOwnerRead(t, tx.relocate(path)); !bytes.Equal(got, before) {
						t.Fatal("immutable evidence changed", filepath.Base(path))
					}
				}
				for _, project := range tx.Retained.Projects {
					if project.Owner == "" {
						continue
					}
					raw := dialectArtifactOwnerRead(t, homeMoveRowPath(filepath.Join(destination, deskHomePlatform), "owner-locations", project.Owner))
					v, e := readDialectArtifactLocationRecord(raw)
					if e != nil || v.Location.Kind != "home" || string(v.Location.PathBytes) != project.Folder || v.FolderIdentity != project.Identity {
						t.Fatal("current locator", e)
					}
				}
				var startup dialectArtifactHomeMoveProject
				for _, project := range tx.Retained.Projects {
					if project.Owner == first.Owner {
						startup = project
					}
				}
				moved, err := OpenProjectRoot(string(startup.To))
				if err != nil {
					t.Fatal(err)
				}
				got, err := retainedArtifactsForProject(moved.own.root).Get(context.Background(), ref)
				if err != nil || !bytes.Equal(got, rawObject) {
					t.Fatal("raw object changed", err)
				}
				if err = moved.Close(); err != nil {
					t.Fatal(err)
				}
				movedData := filepath.Join(string(startup.To), ".desk-private", "data")
				if !consolidate {
					movedData = tx.relocate(filepath.Join(cfg.ProjectDir, ".desk-private", "data"))
				}
				if !bytes.Equal(dialectArtifactOwnerRead(t, filepath.Join(movedData, runName)), runRaw) || !bytes.Equal(dialectArtifactOwnerRead(t, filepath.Join(movedData, sourceName)), sourceRaw) {
					t.Fatal("namespace ledgers changed")
				}
			})
		}
	}
}

func TestDialectArtifactHomeMoveTokenAndClosedIntent(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	cfg, _ := homeFixture(t)
	cfg.HomeMoveShutdown = func() {}
	s, ts := startDesk(t, cfg)
	t.Cleanup(func() { ts.Close(); s.Close() })
	owner := enrollDialectArtifactMoveServer(t, s)
	destination := filepath.Join(t.TempDir(), "next")
	plan, err := s.planDeskHome(destination)
	if err != nil {
		t.Fatal(err)
	}
	locationPath := dialectArtifactLocationPath(s, owner.Owner)
	before := dialectArtifactOwnerRead(t, locationPath)
	// A new inode containing the same bytes is still a changed selected row.
	if err = os.Rename(locationPath, locationPath+".original"); err != nil {
		t.Fatal(err)
	}
	privateFixture(t, locationPath, string(before))
	if err = s.queueHomeMove(context.Background(), projectMoveRequest{Destination: destination, Token: plan.Token}); err == nil {
		t.Fatal("stale selected inode token accepted")
	}
	if _, err = os.Lstat(homeJournalPath(s.configDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("stale token wrote journal", err)
	}
	if _, err = os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("stale token created home", err)
	}
	// Legacy v1 does not acquire the new optional continuation control, even null.
	const legacy = `{"version":1,"destination":"/new","config":"/old","renames":[],"writes":[],"parent":"","parentIdentity":"","complete":false}`
	for _, extra := range []string{`,"retained":null`, `,"Retained":null`} {
		if _, err := decodeHomeTransaction([]byte(strings.TrimSuffix(legacy, "}") + extra + "}")); err == nil {
			t.Fatal("legacy admitted retained control")
		}
	}
	// The codec is closed recursively before any replay root is opened.
	wire := []byte(`{"version":2,"destination":"/new","config":"/old","renames":[],"writes":[],"parent":"","parentIdentity":"","complete":false,"retained":{"version":1,"version":1}}`)
	if _, err := decodeHomeTransaction(wire); err == nil {
		t.Fatal("duplicate retained control admitted")
	}
}

// Exercise real queue and pre-New recovery, including the uncertain window
// after a rename has landed but before its directory acknowledgement.
func queuedDialectArtifactHomeMoveForAcknowledgement(t *testing.T) (string, *deskHomeTransaction, string) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	cfg, _ := homeFixture(t)

	cfg.HomeMoveShutdown = func() {}
	s, ts := startDesk(t, cfg)
	t.Cleanup(func() {
		ts.Close()
		s.Close()
		testHookDialectArtifactOwnerStep = nil
		testHomeMigrationStep = nil
	})
	if code, _ := chatRequest(t, ts, "PUT", "absent", storageChat, true, ""); code != 200 {
		t.Fatal(code)
	}
	stampMoveIdentity(t, s)
	enrollDialectArtifactMoveServer(t, s)
	destination := filepath.Join(t.TempDir(), "acknowledged-home")
	plan, err := s.planDeskHome(destination)
	if err != nil || len(plan.Blockers) != 0 || plan.Retained == nil {
		t.Fatal("retained plan", plan.Blockers, err)
	}
	if err = s.queueHomeMove(context.Background(), projectMoveRequest{Destination: destination, Token: plan.Token}); err != nil {
		t.Fatal("queue", err)
	}
	journal := homeJournalPath(s.configDir)
	tx, err := readHomeTransaction(journal)
	if err != nil || tx == nil || tx.Retained == nil || len(tx.Renames) == 0 {
		t.Fatal("saved intent", err)
	}
	if filepath.Dir(tx.Renames[0].From) == filepath.Dir(tx.Renames[0].To) {
		t.Fatal("fixture requires distinct rename parents")
	}
	ts.Close()
	s.Close()
	return journal, tx, tx.Writes[len(tx.Writes)-1].Path
}

func assertDialectArtifactHomeMoveUnacknowledged(t *testing.T, journal, pointer string) {
	t.Helper()
	pending, err := readHomeTransaction(journal)
	if err != nil || pending == nil || pending.Complete {
		t.Fatal("unacknowledged intent completed", err)
	}
	if _, err = os.Lstat(pointer); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unacknowledged move published bootstrap", err)
	}
}

func assertDialectArtifactHomeMoveAcknowledged(t *testing.T, journal, pointer string) {
	t.Helper()
	if err := finishHomeTransaction(journal); err != nil {
		t.Fatal("acknowledged retry", err)
	}
	done, err := readHomeTransaction(journal)
	if err != nil || done == nil || !done.Complete {
		t.Fatal("acknowledged intent not complete", err)
	}
	if _, err = os.Lstat(pointer); err != nil {
		t.Fatal("acknowledged move missing bootstrap", err)
	}
}

func TestDialectArtifactHomeMoveAfterRowDirectoryAcknowledgement(t *testing.T) {
	for _, retryFault := range []string{"home-move-row.directory-sync", "home-move-row.readback", "home-move-cas-folder.close"} {
		t.Run(retryFault, func(t *testing.T) {
			journal, tx, pointer := queuedDialectArtifactHomeMoveForAcknowledgement(t)
			landed := false
			testHookDialectArtifactOwnerStep = func(at string) error {
				if at == "home-move-row.before-directory-sync" && !landed {
					landed = true
					return errors.New("row rename landed; directory sync failed")
				}
				return nil
			}
			if err := finishHomeTransaction(journal); err == nil || !landed {
				t.Fatal("row sync failure not exercised", err)
			}
			assertDialectArtifactHomeMoveUnacknowledged(t, journal, pointer)
			var afterPath string
			var afterInfo os.FileInfo
			for _, row := range tx.Retained.Rows {
				if row.BeforeKind != "present" || bytes.Equal(row.Before, row.After) {
					continue
				}
				path := homeMoveRowPath(filepath.Join(tx.Destination, deskHomePlatform), row.Table, row.ID)
				raw, err := os.ReadFile(path)
				if err == nil && bytes.Equal(raw, row.After) {
					afterPath = path
					afterInfo, err = os.Lstat(path)
					if err != nil {
						t.Fatal(err)
					}
					break
				}
			}
			if afterPath == "" {
				t.Fatal("failed directory sync did not leave exact AFTER row")
			}
			fired := false
			testHookDialectArtifactOwnerStep = func(at string) error {
				if at == retryFault {
					fired = true
					return errors.New("AFTER acknowledgement still fails")
				}
				return nil
			}
			if err := finishHomeTransaction(journal); err == nil || !fired {
				t.Fatal("AFTER retry skipped acknowledgement", err)
			}
			assertDialectArtifactHomeMoveUnacknowledged(t, journal, pointer)
			testHookDialectArtifactOwnerStep = nil
			assertDialectArtifactHomeMoveAcknowledged(t, journal, pointer)
			current, err := os.Lstat(afterPath)
			if err != nil || !os.SameFile(afterInfo, current) {
				t.Fatal("AFTER acknowledgement replaced the retained row", err)
			}
		})
	}
}

func TestDialectArtifactHomeMoveLandedRenameDirectoryAcknowledgement(t *testing.T) {
	for _, retryFault := range []string{"rename-source-directory-sync", "rename-directory-readback", "rename-target-directory-close"} {
		t.Run(retryFault, func(t *testing.T) {
			journal, tx, pointer := queuedDialectArtifactHomeMoveForAcknowledgement(t)
			landed := false
			testHomeMigrationStep = func(at string) error {
				if at == "rename-target-directory-sync" && !landed {
					landed = true
					return errors.New("rename landed; destination directory sync failed")
				}
				return nil
			}
			if err := finishHomeTransaction(journal); err == nil || !landed {
				t.Fatal("landed rename sync failure not exercised", err)
			}
			op := tx.Renames[0]
			current, err := os.Lstat(op.To)
			if err != nil || identityKey(current) != op.Identity {
				t.Fatal("rename did not land", err)
			}
			if _, err = os.Lstat(op.From); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("rename source still present", err)
			}
			assertDialectArtifactHomeMoveUnacknowledged(t, journal, pointer)
			fired := false
			testHomeMigrationStep = func(at string) error {
				if at == retryFault {
					fired = true
					return errors.New("landed directory acknowledgement still fails")
				}
				return nil
			}
			if err := finishHomeTransaction(journal); err == nil || !fired {
				t.Fatal("landed retry skipped acknowledgement", err)
			}
			assertDialectArtifactHomeMoveUnacknowledged(t, journal, pointer)
			sourceSync, targetSync := false, false
			testHomeMigrationStep = func(at string) error {
				sourceSync = sourceSync || at == "rename-source-directory-sync"
				targetSync = targetSync || at == "rename-target-directory-sync"
				return nil
			}
			assertDialectArtifactHomeMoveAcknowledged(t, journal, pointer)
			if !sourceSync || !targetSync {
				t.Fatal("successful retry did not acknowledge both parents")
			}
			testHomeMigrationStep = nil
		})
	}
}

// Synthetic relocation vectors isolate parent identity acknowledgement. These
// deliberately do not claim pre-New discovery of a journal inside moved roots;
// that topology is refused by the real preview/queue entry below.
func syntheticDialectArtifactHomeMoveParent(t *testing.T, suffix string) (*deskHomeTransaction, dialectArtifactHomeMoveParent) {
	t.Helper()
	project := filepath.Join(t.TempDir(), "project")
	config := filepath.Join(project, suffix)
	if err := os.MkdirAll(config, 0700); err != nil {
		t.Fatal(err)
	}
	projectInfo, err := os.Lstat(project)
	if err != nil {
		t.Fatal(err)
	}
	configInfo, err := os.Lstat(config)
	if err != nil {
		t.Fatal(err)
	}
	parentInfo, err := os.Lstat(filepath.Dir(config))
	if err != nil {
		t.Fatal(err)
	}
	destination := t.TempDir()
	tx := &deskHomeTransaction{Renames: []homeRename{
		{config, filepath.Join(destination, deskHomePlatform), identityKey(configInfo)},
		{project, filepath.Join(destination, "project"), identityKey(projectInfo)},
	}}
	pin := dialectArtifactHomeMoveParent{homePath(filepath.Dir(config)), identityKey(parentInfo)}
	for _, op := range tx.Renames {
		if err = applyHomeRename(op); err != nil {
			t.Fatal("synthetic landed rename", err)
		}
	}
	return tx, pin
}

func TestDialectArtifactHomeMoveSyntheticRelocatedOriginalParent(t *testing.T) {
	for _, suffix := range []string{"settings", filepath.Join("subdir", "settings")} {
		t.Run(suffix, func(t *testing.T) {
			t.Cleanup(func() { testHomeMigrationStep = nil })
			tx, pin := syntheticDialectArtifactHomeMoveParent(t, suffix)
			current, err := currentHomeMoveParentPath(tx, pin)
			if err != nil || current == string(pin.Path) {
				t.Fatal("parent mapping", err)
			}
			root, err := OpenProjectRoot(current)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			h := &dialectArtifactHomeMoveHold{tx: tx, parents: []homeMoveParentHold{{pin, root}}}
			for _, fault := range []string{"rename-source-directory-sync", "rename-directory-readback"} {
				fired := false
				testHomeMigrationStep = func(at string) error {
					if at == fault {
						fired = true
						return errors.New("synthetic relocated acknowledgement failure")
					}
					return nil
				}
				if err = applyHomeRenameWithParent(tx.Renames[0], root, h.checkRenameParents); err == nil || !fired {
					t.Fatal("relocated acknowledgement failure skipped", fault, err)
				}
				if _, err = os.Lstat(string(pin.Path)); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("recreated old source parent", err)
				}
			}
			testHomeMigrationStep = nil
			if err = applyHomeRenameWithParent(tx.Renames[0], root, h.checkRenameParents); err != nil {
				t.Fatal("successful relocated acknowledgement", err)
			}
			if err = h.checkRenameParents(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDialectArtifactHomeMoveSyntheticReplacedRelocatedParent(t *testing.T) {
	tx, pin := syntheticDialectArtifactHomeMoveParent(t, filepath.Join("subdir", "settings"))
	current, err := currentHomeMoveParentPath(tx, pin)
	if err != nil {
		t.Fatal(err)
	}
	root, err := OpenProjectRoot(current)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	h := &dialectArtifactHomeMoveHold{tx: tx, parents: []homeMoveParentHold{{pin, root}}}
	if err = os.Rename(current, current+".original"); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(current, 0700); err != nil {
		t.Fatal(err)
	}
	if err = h.checkRenameParents(); err == nil {
		t.Fatal("replacement descendant parent accepted")
	}
	if err = applyHomeRenameWithParent(tx.Renames[0], root, h.checkRenameParents); err == nil {
		t.Fatal("replacement parent acknowledged")
	}
	if err = os.Rename(current, current+".replacement"); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(current+".original", current); err != nil {
		t.Fatal(err)
	}
	if err = applyHomeRenameWithParent(tx.Renames[0], root, h.checkRenameParents); err != nil {
		t.Fatal("restored original parent", err)
	}
}

// Explicit DeskConfigDir fixtures default history to settings/data. Keep that
// history outside the project when settings itself is the topology under test.
func externalDialectArtifactHomeMoveChatFixture(t *testing.T, config, data string) {
	t.Helper()
	if err := os.Chmod(data, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(config, 0700); err != nil {
		t.Fatal(err)
	}
	location, err := json.Marshal(dataLocation{Version: 1, Path: data})
	if err != nil {
		t.Fatal(err)
	}
	// These standalone settings fixtures have no home registry; their storage
	// owner is derived from the settings path.
	marker, err := json.Marshal(dataMarker{Version: 1, Owner: digestOf([]byte(config))})
	if err != nil {
		t.Fatal(err)
	}
	privateFixture(t, filepath.Join(config, dataLocationName), string(location))
	privateFixture(t, filepath.Join(data, dataMarkerName), string(marker))
}

func TestDialectArtifactHomeMoveJournalMovingLayoutRefused(t *testing.T) {
	for _, layout := range []string{"settings", filepath.Join("subdir", "settings"), "production-xdg"} {
		t.Run(layout, func(t *testing.T) {
			dataHome := t.TempDir()
			t.Setenv("XDG_DATA_HOME", dataHome)
			t.Setenv("JPACK_DESK_HOME", "")
			cfg, _ := homeFixture(t)
			cfg.ProjectDir = t.TempDir()
			cfg.DeskConfigDir = filepath.Join(cfg.ProjectDir, layout)
			if layout == "production-xdg" {
				configHome := filepath.Join(cfg.ProjectDir, "subdir")
				// Custody requires the configuration home's ancestors to exist.
				if err := os.MkdirAll(configHome, 0700); err != nil {
					t.Fatal(err)
				}
				t.Setenv("XDG_CONFIG_HOME", configHome)
				cfg.DeskConfigDir = ""
			} else {
				externalDialectArtifactHomeMoveChatFixture(t, cfg.DeskConfigDir, t.TempDir())
			}
			stopped := false
			cfg.HomeMoveShutdown = func() { stopped = true }
			privateFixture(t, filepath.Join(cfg.ProjectDir, projectConfigName), `{"deskConfigVersion":1}`)
			var s *Server
			var ts *httptest.Server
			if layout == "production-xdg" {
				// startDesk supplies a DeskConfigDir override when empty. Enter New
				// directly so this case exercises production XDG settings/history.
				ts = httptest.NewUnstartedServer(nil)
				cfg.Port = ts.Listener.Addr().(*net.TCPAddr).Port
				var err error
				s, err = New(cfg)
				if err != nil {
					ts.Close()
					t.Fatal("production XDG setup", err)
				}
				ts.Config.Handler = s
				ts.Start()
			} else {
				s, ts = startDesk(t, cfg)
			}
			t.Cleanup(func() { ts.Close(); s.Close() })
			if !s.assistant.usable() {
				t.Fatal("fixture assistant custody", s.assistant.problem)
			}
			if layout == "production-xdg" && s.configDir != filepath.Join(cfg.ProjectDir, "subdir", deskDirName) {
				t.Fatal("fixture bypassed production settings selection", s.configDir)
			}
			if layout == "production-xdg" && s.defaultChatDataPath() != filepath.Join(dataHome, deskDirName, "history") {
				t.Fatal("fixture bypassed production history selection", s.defaultChatDataPath())
			}
			enrollDialectArtifactMoveServer(t, s)
			configInfo, err := os.Lstat(s.configDir)
			if err != nil {
				t.Fatal(err)
			}
			projectInfo, err := os.Lstat(s.projectDir)
			if err != nil {
				t.Fatal(err)
			}
			destination := filepath.Join(t.TempDir(), "next")
			plan, err := s.planDeskHome(destination)
			if err != nil {
				t.Fatal("refusal preview", err)
			}
			found := false
			for _, blocker := range plan.Blockers {
				found = found || blocker == deskHomeJournalMoveRefusal
			}
			if !found {
				t.Fatal("journal-moving layout not blocked in preview")
			}
			if err = s.queueHomeMove(context.Background(), projectMoveRequest{Destination: destination, Token: plan.Token}); err == nil || !strings.Contains(err.Error(), deskHomeJournalMoveRefusal) {
				t.Fatal("journal-moving queue accepted", err)
			}
			for _, path := range []string{destination, homeJournalPath(s.configDir), filepath.Join(s.configDir, deskHomePointerName)} {
				if _, err = os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("refusal wrote destination/journal/bootstrap", path, err)
				}
			}
			configAfter, e1 := os.Lstat(s.configDir)
			projectAfter, e2 := os.Lstat(s.projectDir)
			if e1 != nil || e2 != nil || !os.SameFile(configInfo, configAfter) || !os.SameFile(projectInfo, projectAfter) || stopped {
				t.Fatal("refusal moved roots or shut down", e1, e2)
			}
		})
	}
}

func TestDialectArtifactHomeMoveLayoutChangeAfterPreviewRefused(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	cfg, _ := homeFixture(t)
	cfg.ProjectDir = t.TempDir()
	cfg.DeskConfigDir = filepath.Join(t.TempDir(), "settings")
	chatData := t.TempDir()
	externalDialectArtifactHomeMoveChatFixture(t, cfg.DeskConfigDir, chatData)
	cfg.HomeMoveShutdown = func() {}
	privateFixture(t, filepath.Join(cfg.ProjectDir, projectConfigName), `{"deskConfigVersion":1}`)
	s, ts := startDesk(t, cfg)
	destination := filepath.Join(t.TempDir(), "next")
	plan, err := s.planDeskHome(destination)
	if err != nil || len(plan.Blockers) != 0 {
		ts.Close()
		s.Close()
		t.Fatal("initial preview", plan.Blockers, err)
	}
	ts.Close()
	s.Close()
	oldConfig := cfg.DeskConfigDir
	cfg.DeskConfigDir = filepath.Join(cfg.ProjectDir, "settings")
	if err = os.Rename(oldConfig, cfg.DeskConfigDir); err != nil {
		t.Fatal(err)
	}
	// Keep the same external history root, rebinding its fixture marker to the
	// standalone installation owner at the changed settings path before reopen.
	externalDialectArtifactHomeMoveChatFixture(t, cfg.DeskConfigDir, chatData)
	// Reopen the same project and settings inodes at the changed layout; submit
	// the original unmodified preview token through the real confirmation entry.
	s, ts = startDesk(t, cfg)
	t.Cleanup(func() { ts.Close(); s.Close() })
	if err = s.queueHomeMove(context.Background(), projectMoveRequest{Destination: destination, Token: plan.Token}); err == nil || !strings.Contains(err.Error(), deskHomeJournalMoveRefusal) {
		t.Fatal("changed-layout token bypassed topology refusal", err)
	}
	for _, path := range []string{destination, homeJournalPath(s.configDir), filepath.Join(s.configDir, deskHomePointerName)} {
		if _, err = os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("changed-layout refusal wrote move state", path, err)
		}
	}
}

func TestDialectArtifactHomeMoveParentPinsBindToken(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	cfg, _ := homeFixture(t)
	cfg.ProjectDir = t.TempDir()
	cfg.DeskConfigDir = filepath.Join(t.TempDir(), "subdir", "settings")
	if err := os.MkdirAll(filepath.Dir(cfg.DeskConfigDir), 0700); err != nil {
		t.Fatal(err)
	}
	cfg.HomeMoveShutdown = func() {}
	privateFixture(t, filepath.Join(cfg.ProjectDir, projectConfigName), `{"deskConfigVersion":1}`)
	s, ts := startDesk(t, cfg)
	t.Cleanup(func() { ts.Close(); s.Close() })
	enrollDialectArtifactMoveServer(t, s)
	destination := filepath.Join(t.TempDir(), "next")
	plan, err := s.planDeskHome(destination)
	if err != nil || plan.Retained == nil {
		t.Fatal("retained preview", err)
	}
	projectInfo, err := os.Lstat(cfg.ProjectDir)
	if err != nil {
		t.Fatal(err)
	}
	configInfo, err := os.Lstat(cfg.DeskConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	installationOwner := s.installationOwner()
	savedSettings := filepath.Join(t.TempDir(), "settings")
	if err = os.Rename(cfg.DeskConfigDir, savedSettings); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Dir(cfg.DeskConfigDir)
	if err = os.Rename(parent, parent+".original"); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(savedSettings, cfg.DeskConfigDir); err != nil {
		t.Fatal(err)
	}
	projectAfter, e1 := os.Lstat(cfg.ProjectDir)
	configAfter, e2 := os.Lstat(cfg.DeskConfigDir)
	if e1 != nil || e2 != nil || !os.SameFile(projectInfo, projectAfter) || !os.SameFile(configInfo, configAfter) || s.installationOwner() != installationOwner {
		t.Fatal("fixture changed original project/config inodes or installation owner", e1, e2)
	}
	if err = s.queueHomeMove(context.Background(), projectMoveRequest{Destination: destination, Token: plan.Token}); err == nil {
		t.Fatal("stale source-parent token accepted")
	}
	if _, err = os.Lstat(homeJournalPath(s.configDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("stale parent token claimed journal", err)
	}
	if _, err = os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("stale parent token created destination", err)
	}
}

func TestDialectArtifactHomeMoveParentPinsClosedAndBounded(t *testing.T) {
	_, tx, _ := queuedDialectArtifactHomeMoveForAcknowledgement(t)
	raw, err := marshalHomeTransaction(*tx)
	if err != nil {
		t.Fatal(err)
	}
	clone := func() *deskHomeTransaction {
		v, e := decodeHomeTransaction(raw)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	for _, fault := range []string{"missing", "duplicate", "extra", "wrong-root-identity", "experimental-retained-v1"} {
		t.Run(fault, func(t *testing.T) {
			v := clone()
			switch fault {
			case "experimental-retained-v1":
				v.Retained.Version = 1
			case "missing":
				v.Retained.RenameParents = nil
			case "duplicate":
				v.Retained.RenameParents = append(v.Retained.RenameParents, v.Retained.RenameParents[0])
			case "extra":
				v.Retained.RenameParents = append(v.Retained.RenameParents, dialectArtifactHomeMoveParent{homePath("/unselected-parent"), v.Retained.ConfigIdentity})
			case "wrong-root-identity":
				v.Retained.RenameParents[0].Identity = "invalid"
			}
			if _, e := marshalHomeTransaction(*v); e == nil {
				t.Fatal("invalid parent controls admitted")
			}
		})
	}
	wrong := clone()
	wrong.Retained.RenameParents[0].Identity = wrong.Retained.ConfigIdentity
	if _, err = currentHomeMoveParentPath(wrong, wrong.Retained.RenameParents[0]); err == nil {
		t.Fatal("wrong recorded parent inode accepted")
	}
	legacyRetained := bytes.Replace(raw, []byte(`"retained":{"version":2,`), []byte(`"retained":{"version":1,`), 1)
	if bytes.Equal(legacyRetained, raw) {
		t.Fatal("experimental retained header not exercised")
	}
	if _, err = decodeHomeTransaction(legacyRetained); err == nil {
		t.Fatal("unreleased unpinned retained v1 admitted")
	}
	var wire map[string]json.RawMessage
	if err = json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	var retained map[string]json.RawMessage
	if err = json.Unmarshal(wire["retained"], &retained); err != nil {
		t.Fatal(err)
	}
	for _, replacement := range []string{"null", `[{"path":"/x","path":"/x","identity":"1:2"}]`, `[{"Path":"/x","identity":"1:2"}]`} {
		mutated := bytes.Replace(raw, retained["renameParents"], []byte(replacement), 1)
		if _, e := decodeHomeTransaction(mutated); e == nil {
			t.Fatal("noncanonical parent controls admitted")
		}
	}
	// Journal size, including the new pins, retains the exact existing boundary.
	v := clone()
	v.RunnerDigest = ""
	base, err := marshalHomeTransaction(*v)
	if err != nil {
		t.Fatal(err)
	}
	// ASCII codec padding hits the exact aggregate wire limit byte-for-byte.
	// This does not qualify a Runner executable or relocation request.
	v.RunnerDigest = strings.Repeat("a", homeTransactionLimit-len(base)-len(`,"runnerDigest":""`))
	at, err := marshalHomeTransaction(*v)
	if err != nil || len(at) != homeTransactionLimit {
		t.Fatal("exact aggregate boundary", len(at), err)
	}
	if _, err = decodeHomeTransaction(at); err != nil {
		t.Fatal("exact boundary decode", err)
	}
	v.RunnerDigest += "a"
	if _, err = marshalHomeTransaction(*v); err == nil {
		t.Fatal("aggregate boundary +1 admitted")
	}
}
