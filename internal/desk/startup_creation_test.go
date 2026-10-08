package desk

// The startup project's key is removed by a start only with the transaction
// it was made in, and an upgrade put back never leaves its key without the
// identity its recovery is bound to (review round 1 of #296, findings 1 and
// 2). A stand-in runtime, by absolute path, answers each command.

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// copyProject copies the folder from to to, files and folders with their modes,
// as `cp -a` would, links left out.
func copyProject(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, path)
		target := filepath.Join(to, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case entry.IsDir():
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
			return os.Chmod(target, info.Mode().Perm())
		case info.Mode().IsRegular():
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return os.WriteFile(target, data, info.Mode().Perm())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// offerSays is the upgrade's offer with the signing key chosen, on ts, as
// JSON.
func offerSays(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	status, data := reviewCall(t, ts, "GET", "/api/upgrade?signingKey=true", "", nil, bearer)
	if status != http.StatusOK {
		t.Fatalf("the offer answered %d %s", status, data)
	}
	return string(data)
}

// **A copy of a project removes no key of the original** (review round 1 of
// #296, finding 1). A signing upgrade publishes jpack.json and its lock and
// stops before its marker goes. The project is copied, its identity file
// with it, and the copy's jpack.json put back unsigned. A start on the copy
// finds its identity held by the original folder still: it removes nothing,
// says why, and offers no key; a start on the original then removes only
// the marker, since its jpack.json names the key.
func TestACopyOfAProjectRemovesNoKeyOfTheOriginal(t *testing.T) {
	u := newSigningUpgrade(t, nil)
	name := stoppedAfterNaming(t, u)
	u.ts.Close()
	copied := u.project + " copy"
	copyProject(t, u.project, copied)
	writeProject(t, copied, map[string]string{"jpack.json": upgradeBefore})

	there, ts, logged := startedAt(t, u.s, copied)
	if got := u.keyFiles(t); !slices.Equal(got, []string{name + ".creating", name + ".keys.jsonl", name + ".seed"}) {
		t.Fatalf("a start on the copy left %q: %s", got, logged)
	}
	if !there.startupShared() || !strings.Contains(logged.String(), "is also held by") {
		t.Errorf("the copy does not know its identity is shared: %s", logged)
	}
	said := offerSays(t, ts)
	if !strings.Contains(said, "is also held by another folder") {
		t.Errorf("the copy's offer says %s", said)
	}

	again, _, logged := startedAt(t, there, u.project)
	if again.startupShared() {
		t.Errorf("the original takes its identity for a copy's")
	}
	if got := u.keyFiles(t); !slices.Equal(got, []string{name + ".keys.jsonl", name + ".seed"}) {
		t.Errorf("a start on the original left %q: %s", got, logged)
	}
}

// **A rotation under an identity two folders hold is neither recovered nor
// offered.** The startup project's rotation stops after the runtime wrote
// its line; the project is copied. A start on the copy leaves the next key
// and the marker, and its decision record offers no rotation, and says why;
// a start on the original finishes the rotation.
func TestARotationUnderASharedIdentityIsLeft(t *testing.T) {
	u, token := signedThenRotatable(t)
	testHookKeyBetween = func(at string) {
		if at == "rotation: line written" {
			panic(http.ErrAbortHandler)
		}
	}
	t.Cleanup(func() { testHookKeyBetween = nil })
	u.abandon(t, "/api/audit/key/rotate", map[string]any{"token": token})
	testHookKeyBetween = nil
	n := startupNameOf(t, u.s)
	left := u.keyFiles(t)
	if !slices.Contains(left, n+".rotating") || !slices.Contains(left, n+".next.seed") {
		t.Fatalf("the stopped rotation left %q", left)
	}
	u.ts.Close()
	copied := u.project + " copy"
	copyProject(t, u.project, copied)

	there, ts, logged := startedAt(t, u.s, copied)
	if got := u.keyFiles(t); !slices.Equal(got, left) || seedIs(u.seed) != "1" {
		t.Errorf("a start on the copy left %q, seed %s: %s", got, seedIs(u.seed), logged)
	}
	if _, panel, _ := readAudit(t, ts, ""); panel.Rotation == nil || panel.Rotation.State != rotationUnavailable || !strings.Contains(panel.Rotation.Reason, "is also held by another folder") {
		t.Errorf("the copy's decision record offers %+v", panel.Rotation)
	}

	startedAt(t, there, u.project)
	if got := u.keyFiles(t); !slices.Equal(got, []string{n + ".keys.jsonl", n + ".seed"}) || seedIs(u.seed) != "2" {
		t.Errorf("a start on the original left %q, seed %s", got, seedIs(u.seed))
	}
}

// stoppedBeforeNaming is a signing upgrade stopped after its key was made and
// before jpack.json named it.
func stoppedBeforeNaming(t *testing.T, u *signingUpgrade) string {
	t.Helper()
	answer := u.offer(t, true)
	testHookKeyBetween = func(at string) {
		if at == "upgrade: before naming" {
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

// **An unfinished creation's key goes only where its marker binds it to this
// project and this transaction** (review round 1 of #296, finding 1): the
// identity, the folder and the jpack.json the upgrade set out to replace,
// all three as found. Moved since, with jpack.json changed since, or with a
// marker that names none of them, the key, its list and its marker stay, the
// start says why, and the offer says so; jpack.json put back as it was, the
// next start removes them.
func TestACreationIsRemovedOnlyWhereItsMarkerBindsIt(t *testing.T) {
	t.Run("moved since", func(t *testing.T) {
		u := newSigningUpgrade(t, nil)
		stoppedBeforeNaming(t, u)
		u.ts.Close()
		moved := u.project + " moved"
		if err := os.Rename(u.project, moved); err != nil {
			t.Fatal(err)
		}
		there, ts, logged := startedAt(t, u.s, moved)
		if there.startupShared() || len(u.keyFiles(t)) != 3 || !strings.Contains(logged.String(), "its marker names another folder") {
			t.Errorf("a start on the moved project left %q: %s", u.keyFiles(t), logged)
		}
		said := offerSays(t, ts)
		if !strings.Contains(said, "a signing key's creation for this project did not finish") {
			t.Errorf("the offer says %s", said)
		}
	})

	t.Run("jpack.json changed since", func(t *testing.T) {
		u := newSigningUpgrade(t, nil)
		stoppedBeforeNaming(t, u)
		writeProject(t, u.project, map[string]string{"jpack.json": upgradeBefore + " "})
		u.ts.Close()
		again, logged := restartedServer(t, u.s)
		if len(u.keyFiles(t)) != 3 || !strings.Contains(logged.String(), "is not the file the upgrade set out to replace") {
			t.Errorf("a start with jpack.json changed left %q: %s", u.keyFiles(t), logged)
		}
		writeProject(t, u.project, map[string]string{"jpack.json": upgradeBefore})
		restartedServer(t, again)
		if got := u.keyFiles(t); len(got) != 0 {
			t.Errorf("with jpack.json as it was, the next start left %q", got)
		}
	})

	t.Run("a marker that binds nothing", func(t *testing.T) {
		u := newSigningUpgrade(t, nil)
		name := stoppedBeforeNaming(t, u)
		if err := os.WriteFile(filepath.Join(filepath.Dir(u.seed), name+".creating"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		u.ts.Close()
		_, logged := restartedServer(t, u.s)
		if len(u.keyFiles(t)) != 3 || !strings.Contains(logged.String(), "its marker does not say which project") {
			t.Errorf("a start with an empty marker left %q: %s", u.keyFiles(t), logged)
		}
	})
}

// **An upgrade put back never leaves its key without its identity** (review
// round 1 of #296, finding 2). The key goes once every file is put back, and
// the identity only after it: where a folder cannot be removed after that,
// for another runtime's record in it, nothing of the key is left; where the
// key cannot be removed, the identity stays beside it and the next start
// removes the key; and a stop between the files and the key leaves the
// identity, and the next start removes the key.
func TestAnUpgradePutBackLeavesNoKeyWithoutItsIdentity(t *testing.T) {
	duringLock := func(t *testing.T, u *signingUpgrade, fragment string) {
		t.Helper()
		script := strings.Replace(readFile(t, u.rig.bin), "'packs lock')\n", "'packs lock')\n"+fragment+"\n", 1)
		if err := os.WriteFile(u.rig.bin, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	identity := func(u *signingUpgrade) string { return filepath.Join(u.project, ".desk-private", "project.json") }

	t.Run("a folder another runtime wrote in", func(t *testing.T) {
		u := newSigningUpgrade(t, nil)
		answer := u.offer(t, true)
		u.rig.locks(t, upgradeLock(t, u.project, u.signed(), map[string]string{"alpha": "packs/a.json"}))
		duringLock(t, u, "  printf '{}\\n' > .desk-private/audit/evaluations.jsonl")
		status, data := u.confirm(t, answer.Token, true)
		if status != http.StatusInternalServerError || !strings.Contains(refusalOf(data), "could not be put back as it was") || strings.Contains(refusalOf(data), keyLeftWords) {
			t.Errorf("the confirmation answered %d %s", status, data)
		}
		if got := u.keyFiles(t); len(got) != 0 {
			t.Errorf("the upgrade left its key %q", got)
		}
		if readFile(t, filepath.Join(u.project, "jpack.json")) != upgradeBefore {
			t.Error("jpack.json was not put back")
		}
	})

	t.Run("a key that cannot be removed", func(t *testing.T) {
		u := newSigningUpgrade(t, nil)
		answer := u.offer(t, true)
		u.rig.locks(t, upgradeLock(t, u.project, u.signed(), map[string]string{"alpha": "packs/a.json"}))
		list := strings.TrimSuffix(u.seed, ".seed") + ".keys.jsonl"
		duringLock(t, u, "  cp '"+list+"' '"+list+".x' && mv -f '"+list+".x' '"+list+"'")
		status, data := u.confirm(t, answer.Token, true)
		if status != http.StatusConflict || !strings.Contains(refusalOf(data), "could not be removed, and was left with its creation marker in Desk's signing folder, with the project's identity") {
			t.Errorf("the confirmation answered %d %s", status, data)
		}
		name := startupNameOf(t, u.s)
		if got := u.keyFiles(t); !slices.Contains(got, name+".creating") {
			t.Fatalf("the upgrade left %q, and no marker", got)
		}
		if _, err := os.Lstat(identity(u)); err != nil {
			t.Fatalf("the upgrade removed the identity its key needs: %v", err)
		}
		u.ts.Close()
		restartedServer(t, u.s)
		if got := u.keyFiles(t); len(got) != 0 {
			t.Errorf("the next start left %q", got)
		}
	})

	t.Run("a stop after the files were put back", func(t *testing.T) {
		u := newSigningUpgrade(t, nil)
		answer := u.offer(t, true)
		u.rig.locks(t, upgradeLock(t, u.project, u.signed(), map[string]string{"alpha": "packs/a.json"}))
		testHookKeyBetween = func(at string) {
			if at == "upgrade: files put back" {
				panic(http.ErrAbortHandler)
			}
		}
		t.Cleanup(func() { testHookKeyBetween = nil })
		u.abandon(t, "/api/upgrade", map[string]any{"token": answer.Token, "requireComparableFacts": true, "signingKey": true})
		testHookKeyBetween = nil
		if len(u.keyFiles(t)) != 3 {
			t.Fatalf("the stop left %q", u.keyFiles(t))
		}
		if _, err := os.Lstat(identity(u)); err != nil {
			t.Fatalf("the stop left no identity: %v", err)
		}
		u.ts.Close()
		restartedServer(t, u.s)
		if got := u.keyFiles(t); len(got) != 0 {
			t.Errorf("the next start left %q", got)
		}
	})
}
