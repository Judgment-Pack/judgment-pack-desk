package desk

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// withoutStorage leaves the settings folder config as an installation whose
// storage was never set up, as before this version: its storage files and
// folder (storageInstallationNames) are removed. A home move journalled then is still finished by
// recovery, and the moved installation sets storage up at its first start.
func withoutStorage(t *testing.T, config string) {
	t.Helper()
	entries, err := os.ReadDir(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if name := entry.Name(); slices.Contains(storageInstallationNames, name) {
			if err = os.RemoveAll(filepath.Join(config, name)); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// **Moving an installation into a Desk home is refused once storage is set
// up, and a move journalled before storage is still finished** (v0.14.0).
// A start of this version sets storage up, so the confirmation refuses in
// words (errDeskHomeMoveAfterStorage). Below it, the planner and the queue
// keep their own coverage for a move asked for without storage. The journal and its recovery are
// unchanged: a move journalled by an installation without storage, stopped
// after a parent rename, is finished at the next attempt; the moved
// installation keeps its history, credentials, named desk and retained
// evidence byte for byte, and its locator opens the moved startup project.
// Starting it is not checked: history written by this version carries
// storage state that only a storage-aware home move carries (Desk #698).
func TestDeskHomeMigrationPreservesHistoryCredentialsAndNamedDesk(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	s, ts, _ := assistantServer(t)
	if code, _ := chatRequest(t, ts, "PUT", "absent", storageChat, true, ""); code != 200 {
		t.Fatal(code)
	}
	storageChat := savedConversationBytes(t, s, ts)
	history := s.conversationName()
	named := createTestDesk(t, ts, "Research · Demo")
	privateFixture(t, filepath.Join(s.configDir, "secrets", "migration-fixture"), "opaque credential bytes\n")
	privateFixture(t, filepath.Join(named.Folder, ".desk-private", "retained-proof.json"), "{\"signed\":\"opaque exact bytes\"}\n")
	destination := filepath.Join(t.TempDir(), "jpack-desk")
	if code, raw := deskCall(t, ts, "POST", "/api/desk-home/move", "", `{"destination":`+jsonString(destination)+`}`, true); code != 409 || refusalOf(raw) != errDeskHomeMoveAfterStorage.Error() {
		t.Fatalf("a move into a Desk home after storage was set up was not refused in words: %d %s", code, raw)
	}
	plan, err := s.planDeskHome(destination)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Blockers) != 0 {
		t.Fatal(plan.Blockers)
	}
	if _, err = os.Stat(destination); !os.IsNotExist(err) {
		t.Fatal("preview created destination", err)
	}
	tx, err := s.prepareHomeTransaction(plan)
	if err != nil {
		t.Fatal(err)
	}
	cfg := s.cfg
	ts.Close()
	s.Close()
	withoutStorage(t, tx.Config)
	journal := homeJournalPath(tx.Config)
	if err = saveHomeTransaction(journal, tx, true); err != nil {
		t.Fatal(err)
	}
	// Stop after a parent rename. Recovery must infer that completed step from
	// its retained folder identity, then continue the nested relocations.
	testHomeMigrationStep = func(step string) error {
		if step == "rename-0" {
			return errors.New("simulated crash")
		}
		return nil
	}
	t.Cleanup(func() { testHomeMigrationStep = nil })
	if err = applyHomeTransaction(journal, &tx); err == nil {
		t.Fatal("crash hook ignored")
	}
	testHomeMigrationStep = nil
	if err = applyHomeTransaction(journal, &tx); err != nil {
		t.Fatal(err)
	}
	if !tx.Complete {
		t.Fatal("not committed")
	}
	var startup deskHomeEntry
	var movedNamed deskHomeEntry
	for _, entry := range plan.Registry.Desks {
		if entry.Kind == "project" {
			startup = entry
		} else if entry.ID == named.ID {
			movedNamed = entry
		}
	}
	cfg.ProjectDir = filepath.Join(destination, startup.Folder)
	cfg.Root = nil
	cfg.DeskConfigDir = filepath.Join(destination, deskHomePlatform)
	if chosen, err := ResolveProjectDir("", cfg.DeskConfigDir); err != nil || chosen != cfg.ProjectDir {
		t.Fatal("saved startup cannot open", chosen, err)
	}
	// The startup chat namespace moves into the startup Desk's own folder.
	if raw, err := os.ReadFile(filepath.Join(cfg.ProjectDir, ".desk-private", "data", history)); err != nil || string(raw) != storageChat {
		t.Fatal("history lost", err)
	}
	if movedNamed.ID != named.ID || movedNamed.HistoryIdentity == "" {
		t.Fatal("named Desk was not moved", movedNamed)
	}
	if raw, err := os.ReadFile(filepath.Join(cfg.DeskConfigDir, "secrets", "migration-fixture")); err != nil || string(raw) != "opaque credential bytes\n" {
		t.Fatal("credential changed", err)
	}
	if raw, err := os.ReadFile(filepath.Join(destination, movedNamed.Folder, ".desk-private", "retained-proof.json")); err != nil || string(raw) != "{\"signed\":\"opaque exact bytes\"}\n" {
		t.Fatal("retained evidence changed", err)
	}
}
func TestDeskHomePlanRefusesConflictsAndChangesToken(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	s, ts, _ := assistantServer(t)
	destination := filepath.Join(t.TempDir(), "jpack-desk")
	first, err := s.planDeskHome(destination)
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := chatRequest(t, ts, "PUT", "absent", storageChat, true, ""); code != 200 {
		t.Fatal(code)
	}
	second, err := s.planDeskHome(destination)
	if err != nil {
		t.Fatal(err)
	}
	if second.Token == first.Token {
		t.Fatal("history change did not invalidate preview")
	}
	if err = os.MkdirAll(filepath.Join(destination, deskHomePlatform), 0700); err != nil {
		t.Fatal(err)
	}
	conflict, err := s.planDeskHome(destination)
	if err != nil {
		t.Fatal(err)
	}
	if len(conflict.Blockers) == 0 {
		t.Fatal("occupied destination accepted")
	}
	if _, err = s.prepareHomeTransaction(conflict); err == nil {
		t.Fatal("prepared blocked move")
	}
}
func TestDeskHomeTransactionNeverOverwritesChangedFiles(t *testing.T) {
	parent := t.TempDir()
	from, to := filepath.Join(parent, "source"), filepath.Join(parent, "destination")
	privateFixture(t, from, "original")
	info, _ := os.Stat(from)
	op := homeRename{From: from, To: to, Identity: identityKey(info)}
	privateFixture(t, to, "keep me")
	if err := applyHomeRename(op); err == nil {
		t.Fatal("replaced occupied destination")
	}
	if raw, _ := os.ReadFile(to); string(raw) != "keep me" {
		t.Fatal("overwritten")
	}
	write := homeWrite{Path: to, Before: digestOf([]byte("original")), Data: []byte("new")}
	if err := applyHomeWrite(write); err == nil {
		t.Fatal("overwrote changed settings")
	}
	if raw, _ := os.ReadFile(to); string(raw) != "keep me" {
		t.Fatal("overwritten")
	}
	// Paths obtained from native selection remain data.
	for _, name := range []string{"../escape", "_platform", "bad/name", "CON/..", "CON", "aux.txt", "COM1", "lpt³"} {
		if validHomeFolder(name) {
			t.Fatal(name)
		}
	}
	if !strings.HasPrefix(homeFolderName("Research · Demo", strings.Repeat("a", 32)), "research-demo-") {
		t.Fatal("bad friendly folder")
	}
}

// **A Desk home move is refused in words once storage is set up** (v0.14.0).
// Storage registers its folders where they are, and no transition carries
// its owner record to another folder yet (Desk #698), so moving a whole
// installation into a Desk home, as here, stopped with "storage coordinator
// busy" or left a moved installation whose storage start was refused. Every
// start of this version sets storage up, so the API still requires
// authentication first, then says why in words: Desk home status before a
// move is offered, and preview and move with or without a reviewed token.
// Nothing is scheduled or moved, no shutdown is requested and ordinary writes
// continue.
func TestDeskHomeAPIRequiresReviewAndStopsBeforeMove(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	shutdown := make(chan struct{}, 1)
	cfg := Config{ProjectDir: t.TempDir(), DeskConfigDir: t.TempDir(), Token: testToken, JpackBin: os.Getenv("JPACK_BIN"), HomeMoveShutdown: func() { shutdown <- struct{}{} }}
	privateFixture(t, filepath.Join(cfg.ProjectDir, projectConfigName), `{"deskConfigVersion":1}`)
	s, ts := startDesk(t, cfg)
	destination := filepath.Join(t.TempDir(), "jpack-desk")
	request, _ := json.Marshal(projectMoveRequest{Destination: destination})
	if code, _ := deskCall(t, ts, "POST", "/api/desk-home/preview", "", string(request), false); code != 401 {
		t.Fatal("unauthenticated preview", code)
	}
	code, raw := deskCall(t, ts, "GET", "/api/desk-home", "", "", true)
	var status struct {
		CanMove     bool   `json:"canMove"`
		MoveRefusal string `json:"moveRefusal"`
	}
	if code != 200 || json.Unmarshal(raw, &status) != nil || status.CanMove || status.MoveRefusal != errDeskHomeMoveAfterStorage.Error() {
		t.Fatalf("Desk home status did not say why no move is offered: %d %s", code, raw)
	}
	for _, path := range []string{"/api/desk-home/preview", "/api/desk-home/move"} {
		for _, token := range []string{"", "a-reviewed-token"} {
			request, _ = json.Marshal(projectMoveRequest{Destination: destination, Token: token})
			if code, raw := deskCall(t, ts, "POST", path, "", string(request), true); code != 409 || refusalOf(raw) != errDeskHomeMoveAfterStorage.Error() {
				t.Fatalf("%s with token %q answered %d %s", path, token, code, raw)
			}
		}
	}
	if s.homeMove.queued.Load() {
		t.Fatal("a refusal scheduled a move")
	}
	select {
	case <-shutdown:
		t.Fatal("a refusal requested a shutdown")
	case <-time.After(300 * time.Millisecond):
	}
	if _, err := os.Stat(cfg.ProjectDir); err != nil {
		t.Fatal("the project moved", err)
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatal("the refusal made the destination", err)
	}
	if code, _ := chatRequest(t, ts, "PUT", "absent", storageChat, true, ""); code != 200 {
		t.Fatalf("a refused move stopped ordinary writes: %d", code)
	}
	if tx, err := readHomeTransaction(homeJournalPath(cfg.DeskConfigDir)); err != nil || tx != nil {
		t.Fatalf("a refused move left a journal: %+v %v", tx, err)
	}
}
func TestDeskHomeOtherProcessBlocksMigrationLease(t *testing.T) {
	config := t.TempDir()
	os.Chmod(config, 0700)
	first, err := openHomeLease(config, false)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := openHomeLease(config, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = lockPrivateData(first, true); err == nil {
		t.Fatal("other reader did not exclude move")
	}
	if err = lockPrivateData(first, false); err != nil {
		t.Fatal(err)
	}
	second.Close()
	if err = lockPrivateData(first, true); err != nil {
		t.Fatal("idle install cannot move", err)
	}
}

// **Moving a Desk home is refused once storage is set up, and a whole move
// journalled before storage is still finished** (v0.14.0). The confirmation
// refuses in words (errDeskHomeMoveAfterStorage). A whole-home move journalled
// by a home without storage, stopped after its pointer write, is finished at
// the next attempt and keeps a desk created after the registry was cached.
func TestDeskHomeWholeMoveKeepsNewDesksAndRecoversAfterPointer(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	cfg, _ := homeFixture(t)
	s, ts := startDesk(t, cfg)
	named := createTestDesk(t, ts, "New research")
	destination := filepath.Join(t.TempDir(), "jpack-desk")
	if code, raw := deskCall(t, ts, "POST", "/api/desk-home/move", "", `{"destination":`+jsonString(destination)+`}`, true); code != 409 || refusalOf(raw) != errDeskHomeMoveAfterStorage.Error() {
		t.Fatalf("a Desk home move after storage was set up was not refused in words: %d %s", code, raw)
	}
	plan, err := s.planDeskHome(destination)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Registry.Desks) != 2 {
		t.Fatal("cached registry omitted newly created Desk")
	}
	tx, err := s.prepareHomeTransaction(plan)
	if err != nil {
		t.Fatal(err)
	}
	ts.Close()
	s.Close()
	withoutStorage(t, tx.Config)
	journal := homeJournalPath(tx.Config)
	if err = saveHomeTransaction(journal, tx, true); err != nil {
		t.Fatal(err)
	}
	testHomeMigrationStep = func(step string) error {
		if step == "write-"+strconv.Itoa(len(tx.Writes)-1) {
			return errors.New("stopped after pointer")
		}
		return nil
	}
	t.Cleanup(func() { testHomeMigrationStep = nil })
	if err = finishHomeTransaction(journal); err == nil {
		t.Fatal("missing simulated crash")
	}
	testHomeMigrationStep = nil
	if err = finishHomeTransaction(journal); err != nil {
		t.Fatal("recovery failed", err)
	}
	cfg.ProjectDir = filepath.Join(destination, "unveil")
	cfg.DeskConfigDir = filepath.Join(destination, deskHomePlatform)
	again, ats := startDesk(t, cfg)
	t.Cleanup(func() { ats.Close(); again.Close() })
	if again.desks[named.ID] == nil {
		t.Fatal("new Desk lost after move")
	}
}

func TestDeskHomeLauncherWorksAcrossInterruptedInstallMove(t *testing.T) {
	parent := t.TempDir()
	previous, next := filepath.Join(parent, "old"), filepath.Join(parent, "new")
	// A harmless fixture records which updater and root the launcher selected.
	os.MkdirAll(previous, 0700)
	script := `import json,sys; print(json.dumps([__file__,sys.argv[1:]]))`
	privateFixture(t, filepath.Join(previous, "desk-update.py"), script)
	before := []byte("#!/bin/sh\nexport PRESERVE_LOCAL=1\nexec /usr/bin/python3 " + previous + "/desk-update.py run -- \"$@\"\n")
	bridge, err := homeLauncherBridge(before, previous, next)
	if err != nil {
		t.Fatal(err)
	}
	after, err := migratedLauncher(before, previous, next)
	if err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(parent, "launcher")
	privateFixture(t, launcher, string(before))
	info, _ := os.Stat(previous)
	tx := deskHomeTransaction{Version: 1, Config: previous, Destination: next, Prelude: []homeWrite{{Path: launcher, Before: digestOf(before), Data: bridge, Executable: true}}, Renames: []homeRename{{From: previous, To: next, Identity: identityKey(info)}}, Writes: []homeWrite{{Path: launcher, Before: digestOf(bridge), Data: after, Executable: true}}}
	journal := filepath.Join(parent, deskHomeTransactionName)
	if err = saveHomeTransaction(journal, tx, true); err != nil {
		t.Fatal(err)
	}
	if err = applyHomeWrite(tx.Prelude[0]); err != nil {
		t.Fatal(err)
	}
	run := func(want string) {
		t.Helper()
		out, err := exec.Command("/bin/sh", launcher, "--help").CombinedOutput()
		if err != nil || !strings.Contains(string(out), want) {
			t.Fatalf("launcher unavailable: %s %v", out, err)
		}
	}
	run(previous)
	testHomeMigrationStep = func(step string) error {
		if step == "rename-0" {
			return errors.New("stopped after install rename")
		}
		return nil
	}
	t.Cleanup(func() { testHomeMigrationStep = nil })
	if err = applyHomeTransaction(journal, &tx); err == nil {
		t.Fatal("missing simulated crash")
	}
	run(next)
	testHomeMigrationStep = nil
	if err = applyHomeTransaction(journal, &tx); err != nil {
		t.Fatal(err)
	}
	run(next)
	// Repeat a recovery after final launcher publication but before journal commit.
	tx.Complete = false
	if err = applyHomeTransaction(journal, &tx); err != nil {
		t.Fatal("final launcher recovery", err)
	}
	if got, _ := os.ReadFile(launcher); string(got) != string(after) {
		t.Fatal("launcher regressed to bridge")
	}
}

func TestDeskHomeManagedInstallChecksInstalledVersion(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	privateFixture(t, filepath.Join(dir, "installation.json"), `{"schemaVersion":1,"current":{"version":"0.12.0"}}`)
	binary := filepath.Join(dir, "releases", "0.12.0", "jpack-desk")
	os.MkdirAll(filepath.Dir(binary), 0700)
	privateFixture(t, binary, "#!/bin/sh\nexit 2\n")
	os.Chmod(binary, 0700)
	if err := managedInstallHomeReady(dir); err == nil || !strings.Contains(err.Error(), "Update the managed") {
		t.Fatal("did not inspect supported version format", err)
	}
	privateFixture(t, binary, "#!/bin/sh\n[ \"$1\" = --desk-home-layout-version ] || exit 2\nprintf '1\\n'\n")
	os.Chmod(binary, 0700)
	if err := managedInstallHomeReady(dir); err != nil {
		t.Fatal(err)
	}
}

func TestDeskHomeSigningPathMoveKeepsReviewedPins(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	s, ts, _ := assistantServer(t)
	// This test exercises location metadata only; it never creates a signing seed.
	config := `{"configVersion":"6","audit":{"dir":"audit","signingKey":` + quotedPath(filepath.Join(s.configDir, "secrets", "fixture.key")) + `},"packs":{}}`
	privateFixture(t, filepath.Join(s.projectDir, "jpack.json"), config)
	pins := `{"retained":{"path":"packs/retained.json","digest":"sha256:` + strings.Repeat("a", 64) + `"}}`
	lock := `{"lockVersion":"1","config":{"digest":"sha256:` + digestOf([]byte(config)) + `"},"packs":` + pins + `}`
	privateFixture(t, filepath.Join(s.projectDir, runtimeLockName), lock)
	destination := filepath.Join(t.TempDir(), "jpack-desk")
	plan, err := s.planDeskHome(destination)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.prepareHomeTransaction(plan)
	if err != nil {
		t.Fatal(err)
	}
	var changedConfig, changedLock []byte
	for _, write := range tx.Writes {
		switch filepath.Base(write.Path) {
		case "jpack.json":
			changedConfig = write.Data
		case runtimeLockName:
			changedLock = write.Data
		}
	}
	var after struct {
		Config struct {
			Digest string `json:"digest"`
		} `json:"config"`
		Packs json.RawMessage `json:"packs"`
	}
	if json.Unmarshal(changedLock, &after) != nil || after.Config.Digest != "sha256:"+digestOf(changedConfig) || string(after.Packs) != pins {
		t.Fatal("reviewed pins changed or config detached", string(changedLock))
	}
	privateFixture(t, filepath.Join(s.projectDir, "jpack.json"), config+"\n")
	stale, err := s.prepareHomeTransaction(plan)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, write := range stale.Writes {
		if filepath.Base(write.Path) == runtimeLockName {
			found = true
			if string(write.Data) != lock {
				t.Fatal("silently refreshed already stale lock")
			}
		}
	}
	if !found {
		t.Fatal("stale lock missing conflict check")
	}
	// A lock already naming the destination config must not become current.
	var collision map[string]any
	json.Unmarshal([]byte(lock), &collision)
	collision["config"] = map[string]string{"digest": "sha256:" + digestOf(changedConfig)}
	collisionRaw, _ := json.Marshal(collision)
	privateFixture(t, filepath.Join(s.projectDir, runtimeLockName), string(collisionRaw))
	if _, err = s.prepareHomeTransaction(plan); err == nil {
		t.Fatal("move approved a stale lock")
	}
	ts.Close()
	s.Close()
}

func TestDeskHomeLocalLauncher(t *testing.T) {
	previous := t.TempDir()
	os.Chmod(previous, 0700)
	next := filepath.Join(t.TempDir(), "moved")
	script := "print('1')\n"
	privateFixture(t, filepath.Join(previous, "desk-local.py"), script)
	marker := `{"schemaVersion":1,"manifestDigest":"` + strings.Repeat("a", 64) + `","launcherDigest":"` + digestOf([]byte(script)) + `"}`
	privateFixture(t, filepath.Join(previous, "local-launch.json"), marker)
	if err := managedInstallHomeReady(previous); err != nil {
		t.Fatal(err)
	}
	before := []byte("#!/bin/sh\nexec /usr/bin/python3 " + quoteHomeShell(filepath.Join(previous, "desk-local.py")) + " --root " + quoteHomeShell(previous) + ` run -- "$@"` + "\n")
	after, err := migratedLauncher(before, previous, next)
	if err != nil || !strings.Contains(string(after), next+"/desk-local.py") {
		t.Fatal(string(after), err)
	}
	bridge, err := homeLauncherBridge(before, previous, next)
	if err != nil || !strings.Contains(string(bridge), next+"/desk-local.py") {
		t.Fatal(string(bridge), err)
	}
	old := []byte("exec /usr/bin/python3 " + filepath.Join(previous, "desk-update.py") + ` run -- "$@"`)
	if _, err := migratedLauncher(old, previous, next); err == nil {
		t.Fatal("selected local build but launcher starts old release")
	}
	privateFixture(t, filepath.Join(previous, "desk-local.py"), "raise Exception('changed')\n")
	if err := managedInstallHomeReady(previous); err == nil {
		t.Fatal("changed local launcher accepted")
	}
}
