package desk

// The identity of the project Desk was started on (issue #283): written once
// in `.desk-private/project.json`, by the upgrade that makes the project's
// key or by a start where the private folder is there; the name everything
// Desk keeps for the project is kept under, wherever the project is moved;
// and the only name under which a start's sweep removes a key. A stand-in
// runtime, by absolute path, answers each command.

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// identityLine is the identity file Desk writes for id in the folder dir,
// recording that folder by device and inode (issue #309), with the stamping
// settings' move from from, where it is not empty.
func identityLine(id, dir, from string) string {
	folder := ""
	if info, err := os.Stat(dir); err == nil {
		folder = identityKey(info)
	}
	return string(identityRecord{ID: id, Path: dir, Folder: folder, From: from}.line())
}

// startedAt closes s and starts Desk on dir with s's folders and runtime,
// and a log the test reads.
func startedAt(t *testing.T, s *Server, dir string) (*Server, *httptest.Server, *bytes.Buffer) {
	t.Helper()
	cfg := s.cfg
	cfg.Root, cfg.ProjectDir = nil, dir
	logged := &bytes.Buffer{}
	cfg.Logger = log.New(logged, "", 0)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	again, ts := startDesk(t, cfg)
	t.Cleanup(func() { again.Close(); ts.Close() })
	return again, ts, logged
}

// stoppedAfterNaming is a signing upgrade stopped after jpack.json names the
// key and the lock pins it, before its marker is removed: the stop the line
// audit's finding 1 begins with. It answers the name the key was made under.
func stoppedAfterNaming(t *testing.T, u *signingUpgrade) string {
	t.Helper()
	answer := u.offer(t, true)
	u.rig.locks(t, upgradeLock(t, u.project, u.signed(), bothPacks))
	testHookKeyBetween = func(at string) {
		if at == "upgrade: named" {
			panic(http.ErrAbortHandler)
		}
	}
	t.Cleanup(func() { testHookKeyBetween = nil })
	u.abandon(t, "/api/upgrade", map[string]any{"token": answer.Token, "requireComparableFacts": true, "signingKey": true})
	testHookKeyBetween = nil
	name := startupNameOf(t, u.s)
	if got := u.keyFiles(t); !slices.Equal(got, []string{name + ".creating", name + ".keys.jsonl", name + ".seed"}) {
		t.Fatalf("the stopped upgrade left %q", got)
	}
	return name
}

// movedAside moves the project to a new path beside it, and puts another
// project where it was, with no key and no private folder.
func movedAside(t *testing.T, project string) string {
	t.Helper()
	moved := project + " moved"
	if err := os.Rename(project, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	writeProject(t, project, map[string]string{"jpack.json": upgradeBefore, "packs/a.json": reviewPack, "packs/b.json": otherPack})
	return moved
}

// **A moved project keeps its key, and a project at its old path takes
// nothing** (the line audit's finding 1). A signing upgrade publishes
// jpack.json and its lock and stops before its marker goes. The project is
// moved, and another project is put at its old path: a start there names
// that project otherwise, and leaves the key, its list and its marker; a
// start on the moved project names it as before, removes only the marker,
// and its decision record reads the key.
func TestAMovedProjectKeepsItsKeyAndItsPathTakesNothing(t *testing.T) {
	u := newSigningUpgrade(t, nil)
	name := stoppedAfterNaming(t, u)
	u.ts.Close()
	moved := movedAside(t, u.project)

	there, _, logged := startedAt(t, u.s, u.project)
	if got := there.signingKeyName(); got == name {
		t.Fatalf("the project at the old path is named %s, the moved project's name", got)
	}
	if got := u.keyFiles(t); !slices.Equal(got, []string{name + ".creating", name + ".keys.jsonl", name + ".seed"}) {
		t.Fatalf("a start on the project at the old path left %q: %s", got, logged)
	}

	again, ts, logged := startedAt(t, there, moved)
	if got := again.signingKeyName(); got != name {
		t.Errorf("the moved project is named %s, want %s", got, name)
	}
	if got := u.keyFiles(t); !slices.Equal(got, []string{name + ".keys.jsonl", name + ".seed"}) {
		t.Errorf("a start on the moved project left %q: %s", got, logged)
	}
	if _, panel, refusal := readAudit(t, ts, ""); panel.Keys == nil || panel.Keys.State != keysKept || !slices.Equal(panel.Keys.Public, []deskPublicKey{key1}) {
		t.Errorf("the moved project's decision record shows %+v %q", panel.Keys, refusal)
	}
}

// legacyStartupKey puts in Desk's signing folder what a Desk before the
// project's identity left of a signing upgrade stopped after naming its key:
// under the hex SHA-256 of the project's path, the seed, its list and its
// marker; and jpack.json naming that seed. It answers that name.
func legacyStartupKey(t *testing.T, u *signingUpgrade) string {
	t.Helper()
	legacy := digestOf([]byte(u.s.projectDir))
	folder := filepath.Dir(u.seed)
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{legacy + ".seed": standInSeed + "\n", legacy + ".keys.jsonl": wantKeyLine(standInPublicKey, standInKeyID, 0), legacy + ".creating": ""} {
		if err := os.WriteFile(filepath.Join(folder, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	signed := strings.Replace(u.signed(), u.seed, filepath.Join(folder, legacy+".seed"), 1)
	writeProject(t, u.project, map[string]string{"jpack.json": signed})
	if err := os.MkdirAll(filepath.Join(u.project, ".desk-private", "audit"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(u.project, ".desk-private"), 0o700); err != nil {
		t.Fatal(err)
	}
	return legacy
}

// keysUnder is the names in Desk's signing folder that begin with name.
func keysUnder(t *testing.T, u *signingUpgrade, name string) []string {
	t.Helper()
	var names []string
	for _, entry := range namesIn(t, filepath.Dir(u.seed)) {
		if strings.HasPrefix(entry, name) {
			names = append(names, entry)
		}
	}
	return names
}

// **A key made before the project had an identity is kept when its project
// moves.** A Desk before this one named the key by the path's hash, and left
// it unfinished, named by jpack.json. The project is moved and another is
// put at its path: a start there, whether or not that project has a private
// folder, removes nothing, since a name made from a path is not bound to one
// project; a start on the moved project takes the name its jpack.json names
// as its identity, and removes only the marker. That identity records no
// folder, since a copy's jpack.json names the same key (issue #319): it is
// unresolved, and asks the owner, until the owner says this folder is the
// project the key was made for, which binds it to this folder.
func TestAKeyFromBeforeTheIdentityIsKeptWhenItsProjectMoves(t *testing.T) {
	for _, private := range []bool{false, true} {
		t.Run(map[bool]string{false: "the project at the path has no private folder", true: "the project at the path has a private folder"}[private], func(t *testing.T) {
			u := newSigningUpgrade(t, nil)
			legacy := legacyStartupKey(t, u)
			u.ts.Close()
			moved := movedAside(t, u.project)
			if private {
				if err := os.Mkdir(filepath.Join(u.project, ".desk-private"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			all := []string{legacy + ".creating", legacy + ".keys.jsonl", legacy + ".seed"}

			there, _, logged := startedAt(t, u.s, u.project)
			if got := keysUnder(t, u, legacy); !slices.Equal(got, all) {
				t.Fatalf("a start on the project at the path left %q: %s", got, logged)
			}
			if private && there.signingKeyName() == legacy {
				t.Errorf("the project at the path took the path's name %s as its identity", legacy)
			}

			again, ts, logged := startedAt(t, there, moved)
			if got := again.signingKeyName(); got != legacy {
				t.Errorf("the moved project is named %s, want the name its jpack.json names, %s: %s", got, legacy, logged)
			}
			if got := readFile(t, filepath.Join(moved, ".desk-private", "project.json")); got != string(identityRecord{ID: legacy, Path: again.projectDir}.line()) {
				t.Errorf("the moved project's identity is %q", got)
			}
			if !again.startupUnresolved() || !strings.Contains(logged.String(), "a copy's jpack.json names the same key") {
				t.Errorf("an identity taken from jpack.json is not unresolved: %s", logged)
			}
			if got := keysUnder(t, u, legacy); !slices.Equal(got, all[1:]) {
				t.Errorf("a start on the moved project left %q: %s", got, logged)
			}
			if _, panel, refusal := readAudit(t, ts, ""); panel.Keys == nil || panel.Keys.State != keysKept {
				t.Errorf("the moved project's decision record shows %+v %q", panel.Keys, refusal)
			}
			offer := offerOf(t, again)
			if offer == nil || offer.Kind != "unbound" {
				t.Fatalf("the decision record asks %+v", offer)
			}
			if status, data := resolve(t, ts, identityMoved, offer.Moved); status != http.StatusOK || again.startupUnresolved() || readFile(t, filepath.Join(moved, ".desk-private", "project.json")) != identityLine(legacy, again.projectDir, "") {
				t.Errorf("the owner's answer was answered %d %s", status, data)
			}
		})
	}

	t.Run("a marker under the path whose seed jpack.json does not name is left", func(t *testing.T) {
		u := newSigningUpgrade(t, nil)
		legacy := legacyStartupKey(t, u)
		writeProject(t, u.project, map[string]string{"jpack.json": upgradeBefore})
		if err := os.RemoveAll(filepath.Join(u.project, ".desk-private")); err != nil {
			t.Fatal(err)
		}
		u.ts.Close()
		_, logged := restartedServer(t, u.s)
		if got := keysUnder(t, u, legacy); len(got) != 3 {
			t.Errorf("the start left %q", got)
		}
		if !strings.Contains(logged.String(), "is not bound to one project") {
			t.Errorf("the start did not say why it left the key: %s", logged)
		}
	})
}

// **The identity is written at a start where the private folder is there,
// and a stop inside the migration is finished at the next.** A project with
// a private folder and no key, whose stamping settings Desk kept under its
// path's hash: a start writes a new name with the move to it journalled,
// then renames the settings, then writes the name alone. Stopped after the
// journal, the next start finishes the move; stopped before it, the next
// start writes a name. Nothing is removed at any step.
func TestTheIdentityMovesStampingSettingsAndAStopIsFinished(t *testing.T) {
	setUp := func(t *testing.T) (*signingUpgrade, string, string) {
		t.Helper()
		u := newSigningUpgrade(t, nil)
		if err := os.Mkdir(filepath.Join(u.project, ".desk-private"), 0o700); err != nil {
			t.Fatal(err)
		}
		legacy := digestOf([]byte(u.s.projectDir))
		settings := filepath.Join(u.s.configDir, "stamping", legacy)
		if err := os.MkdirAll(settings, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(settings, "settings.json"), []byte("kept\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		u.ts.Close()
		return u, legacy, filepath.Join(u.project, ".desk-private", "project.json")
	}
	stopAt := func(t *testing.T, step string) {
		t.Helper()
		testHookIdentityStep = func(at string) error {
			if at == step {
				return errors.New("stopped")
			}
			return nil
		}
		t.Cleanup(func() { testHookIdentityStep = nil })
	}

	t.Run("whole", func(t *testing.T) {
		u, legacy, file := setUp(t)
		again, logged := restartedServer(t, u.s)
		id := again.signingKeyName()
		if id == legacy || !startupIDForm.MatchString(id) || !again.startupBound() {
			t.Fatalf("the project is named %s after the start: %s", id, logged)
		}
		if got := readFile(t, file); got != identityLine(id, again.projectDir, "") {
			t.Errorf("the identity file is %q", got)
		}
		if got := readFile(t, filepath.Join(u.s.configDir, "stamping", id, "settings.json")); got != "kept\n" {
			t.Errorf("the settings under the new name are %q", got)
		}
		if _, err := os.Lstat(filepath.Join(u.s.configDir, "stamping", legacy)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("the settings are still under the path's hash: %v", err)
		}
	})

	t.Run("stopped after the journal", func(t *testing.T) {
		u, legacy, file := setUp(t)
		stopAt(t, "moved")
		stopped, logged := restartedServer(t, u.s)
		testHookIdentityStep = nil
		id := stopped.signingKeyName()
		if got := readFile(t, file); got != identityLine(id, stopped.projectDir, legacy) {
			t.Fatalf("the stopped migration left the identity file %q: %s", got, logged)
		}
		if got := readFile(t, filepath.Join(u.s.configDir, "stamping", legacy, "settings.json")); got != "kept\n" {
			t.Fatalf("the stopped migration left the settings %q", got)
		}
		again, logged := restartedServer(t, stopped)
		if got := again.signingKeyName(); got != id {
			t.Errorf("the next start named the project %s, want %s", got, id)
		}
		if got := readFile(t, file); got != identityLine(id, again.projectDir, "") {
			t.Errorf("the next start left the identity file %q: %s", got, logged)
		}
		if got := readFile(t, filepath.Join(u.s.configDir, "stamping", id, "settings.json")); got != "kept\n" {
			t.Errorf("the next start left the settings under the new name as %q", got)
		}
	})

	t.Run("stopped before the name was written", func(t *testing.T) {
		u, legacy, file := setUp(t)
		stopAt(t, "written")
		stopped, _ := restartedServer(t, u.s)
		testHookIdentityStep = nil
		if _, err := os.Lstat(file); !errors.Is(err, os.ErrNotExist) || stopped.signingKeyName() != legacy {
			t.Fatalf("the stopped migration left an identity (%v), the project named %s", err, stopped.signingKeyName())
		}
		if got := namesIn(t, filepath.Join(u.project, ".desk-private")); len(got) != 0 {
			t.Errorf("the stopped migration left %q in the private folder", got)
		}
		again, _ := restartedServer(t, stopped)
		if id := again.signingKeyName(); id == legacy || readFile(t, file) != identityLine(id, again.projectDir, "") {
			t.Errorf("the next start named the project %s, with the file %q", id, readFile(t, file))
		}
	})

	t.Run("a key under the path's hash keeps its settings", func(t *testing.T) {
		u, legacy, _ := setUp(t)
		folder := filepath.Dir(u.seed)
		if err := os.MkdirAll(folder, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(folder, legacy+".seed"), []byte(standInSeed+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		again, _ := restartedServer(t, u.s)
		if id := again.signingKeyName(); id == legacy || !again.startupBound() {
			t.Errorf("the project is named %s", id)
		}
		if got := readFile(t, filepath.Join(u.s.configDir, "stamping", legacy, "settings.json")); got != "kept\n" {
			t.Errorf("the settings a key under the path's hash stays with were moved: %q", got)
		}
	})
}

// **An identity that cannot be read names nothing.** Where the identity file
// is there and not one Desk writes, or cannot be read now, the project has
// no name: it is not taken for one with none, which the path's hash would
// name. The offer makes no key, the decision record says it could not read
// the project's identity, and the sweep leaves what is kept under the path.
func TestAnIdentityThatCannotBeReadNamesNothing(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		mode       os.FileMode
	}{
		{"another spelling", `{"id": "` + strings.Repeat("a", 64) + `","path":"/p"}` + "\n", 0o600},
		{"no newline", `{"id":"` + strings.Repeat("a", 64) + `","path":"/p"}`, 0o600},
		{"a desk's id", `{"id":"` + strings.Repeat("a", 32) + `","path":"/p"}` + "\n", 0o600},
		{"a member besides", `{"id":"` + strings.Repeat("a", 64) + `","path":"/p","x":1}` + "\n", 0o600},
		{"a move from itself", `{"id":"` + strings.Repeat("a", 64) + `","path":"/p","from":"` + strings.Repeat("a", 64) + `"}` + "\n", 0o600},
		{"a folder that is not a path", `{"id":"` + strings.Repeat("a", 64) + `","path":"p"}` + "\n", 0o600},
		{"past its bound", `{"id":"` + strings.Repeat("a", 64) + `","path":"/p"}` + strings.Repeat(" ", startupIdentityLimit) + "\n", 0o600},
		{"open to others", `{"id":"` + strings.Repeat("a", 64) + `","path":"/p"}` + "\n", 0o644},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := newSigningUpgrade(t, nil)
			legacy := legacyStartupKey(t, u)
			// Gated, with a trail and no key named: the decision record reads
			// the keys, and the offer has a key to offer.
			writeProject(t, u.project, map[string]string{"jpack.json": upgradeAfter})
			file := filepath.Join(u.project, ".desk-private", "project.json")
			if err := os.WriteFile(file, []byte(tc.data), tc.mode); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(file, tc.mode); err != nil {
				t.Fatal(err)
			}
			u.ts.Close()
			again, ts, logged := startedAt(t, u.s, u.project)
			if got := again.signingKeyName(); got != "" {
				t.Errorf("the project is named %q", got)
			}
			if got := keysUnder(t, u, legacy); len(got) != 3 {
				t.Errorf("the start left %q: %s", got, logged)
			}
			if got := readFile(t, file); got != tc.data {
				t.Errorf("the identity file was written over: %q", got)
			}
			if _, panel, _ := readAudit(t, ts, ""); panel.Keys == nil || panel.Keys.State != keysUnread || !strings.Contains(panel.Keys.Problem, "could not read this project's identity") {
				t.Errorf("the decision record shows %+v", panel.Keys)
			}
			status, data := reviewCall(t, ts, "GET", "/api/upgrade?signingKey=true", "", nil, bearer)
			if status != http.StatusOK || !strings.Contains(string(data), "this project's identity, in its private folder, could not be read") {
				t.Errorf("the offer answered %d %s", status, data)
			}
		})
	}
}

// **The identity is written only under this project's lock and the signing
// folder's.** While another Desk process holds either, a start writes
// nothing, and the next start writes it.
func TestTheIdentityIsWrittenOnlyUnderBothLocks(t *testing.T) {
	for _, held := range []string{"project", "signing"} {
		t.Run(held, func(t *testing.T) {
			u := newSigningUpgrade(t, nil)
			if err := os.Mkdir(filepath.Join(u.project, ".desk-private"), 0o700); err != nil {
				t.Fatal(err)
			}
			folder := filepath.Dir(u.seed)
			if err := os.MkdirAll(folder, 0o700); err != nil {
				t.Fatal(err)
			}
			u.ts.Close()
			release := holdSigningLock(t, map[string]string{"project": u.project, "signing": folder}[held])
			stopped, logged := restartedServer(t, u.s)
			file := filepath.Join(u.project, ".desk-private", "project.json")
			if _, err := os.Lstat(file); !errors.Is(err, os.ErrNotExist) || stopped.startupBound() {
				t.Fatalf("a start while the %s lock was held wrote the identity (%v): %s", held, err, logged)
			}
			if !strings.Contains(logged.String(), "lock was not taken") {
				t.Errorf("the start did not say why: %s", logged)
			}
			release()
			again, _ := restartedServer(t, stopped)
			if !again.startupBound() || readFile(t, file) != identityLine(again.signingKeyName(), again.projectDir, "") {
				t.Errorf("the next start did not write the identity")
			}
		})
	}
}

// **The upgrade writes the project's identity before its key, and takes it
// away with everything else.** The offer names the key under the name the
// project will keep; the confirmation writes that name into a private folder
// it makes owner-only, then the key. Where the upgrade is put back whole,
// the identity file and the folder go too; where it cannot be, the identity
// stays with the key it names, for the next start.
func TestTheUpgradeWritesTheIdentityBeforeTheKey(t *testing.T) {
	u := newSigningUpgrade(t, nil)
	answer := u.offer(t, true)
	name := startupNameOf(t, u.s)
	if !strings.Contains(answer.ConfigAfter, name+".seed") || name == digestOf([]byte(u.s.projectDir)) {
		t.Fatalf("the offer names the key %s in %s", name, answer.ConfigAfter)
	}
	file := filepath.Join(u.project, ".desk-private", "project.json")
	testHookKeyBetween = func(at string) {
		if at == "before generate" {
			if got := readFile(t, file); got != identityLine(name, u.s.projectDir, "") {
				t.Errorf("the key was made with the identity file %q", got)
			}
			if mode := permOf(t, filepath.Dir(file)); mode != 0o700 {
				t.Errorf("the private folder is %v", mode)
			}
		}
	}
	t.Cleanup(func() { testHookKeyBetween = nil })
	before := treeOf(t, u.project)
	// The lock pins one pack fewer: the upgrade is put back whole.
	u.rig.locks(t, upgradeLock(t, u.project, u.signed(), map[string]string{"alpha": "packs/a.json"}))
	status, data := u.confirm(t, answer.Token, true)
	testHookKeyBetween = nil
	if status != http.StatusConflict {
		t.Fatalf("the confirmation answered %d %s", status, data)
	}
	sameProject(t, before, treeOf(t, u.project), "an upgrade put back whole")
	if _, err := os.Lstat(filepath.Dir(file)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the private folder the upgrade made was left: %v", err)
	}
	if u.s.startupBound() {
		t.Errorf("the server still holds the identity it removed")
	}

	again := u.offer(t, true)
	u.rig.locks(t, upgradeLock(t, u.project, u.signed(), bothPacks))
	if status, data := u.confirm(t, again.Token, true); status != http.StatusOK {
		t.Fatalf("the confirmation answered %d %s", status, data)
	}
	if got := readFile(t, file); got != identityLine(name, u.s.projectDir, "") || u.s.signingKeyName() != name {
		t.Errorf("after the upgrade the identity file is %q, the project named %s", got, u.s.signingKeyName())
	}
}

// **A key is made only under the name the project keeps.** The upgrade
// writes the project's identity before its key; a key asked for with no
// identity written is refused as stale, and nothing is made: under the name
// the offer showed, and under the path's hash, the name a project with no
// identity is read by, which binds the key to no project.
func TestAKeyIsMadeOnlyUnderTheProjectsIdentity(t *testing.T) {
	u := newSigningUpgrade(t, nil)
	u.offer(t, true)
	dir, refusal := u.s.reviewRuntime()
	if refusal != "" {
		t.Fatal(refusal)
	}
	byPath := filepath.Join(filepath.Dir(u.seed), digestOf([]byte(u.s.projectDir))+".seed")
	for _, seed := range []string{u.seed, byPath} {
		key, failure, _ := u.s.makeStartupKey(context.Background(), dir, seed, []byte(upgradeBefore))
		if key != nil || failure == nil || failure.status != http.StatusConflict || failure.code != CodeStale || !strings.Contains(failure.message, "identity") {
			t.Errorf("a key at %s with no identity written answered %+v %+v", filepath.Base(seed), key, failure)
		}
		key.close()
	}
	if names := namesIn(t, filepath.Dir(u.seed)); len(names) != 0 {
		t.Errorf("a key with no identity written made %q", names)
	}
}

// **No key is offered where the project's identity could not be kept.** A
// private folder open to other users holds no identity: the item says why,
// and the offer names no key.
func TestAKeyIsNotOfferedWhereItsIdentityCannotBeKept(t *testing.T) {
	u := newSigningUpgrade(t, nil)
	private := filepath.Join(u.project, ".desk-private")
	if err := os.Mkdir(private, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(private, 0o755); err != nil {
		t.Fatal(err)
	}
	u.ts.Close()
	_, ts, _ := startedAt(t, u.s, u.project)
	status, data := reviewCall(t, ts, "GET", "/api/upgrade?signingKey=true", "", nil, bearer)
	if status != http.StatusOK || !strings.Contains(string(data), `"state":"unavailable"`) || !strings.Contains(string(data), "its private folder is not one Desk keeps this project's identity in") || strings.Contains(string(data), ".seed") {
		t.Errorf("the offer answered %d %s", status, data)
	}
}
