package desk

// One lock for everything that changes keys in Desk's signing folder (issue
// #230). A second "process" is a second descriptor of the folder holding
// flock's exclusive lock, which excludes this one as another process's would:
// the lock belongs to the open file description. Each test holds it at a
// moment a real second Desk could, and shows what the sweep, the recovery, a
// creation and a rotation then do; and, under the lock, that what is removed
// is only what was inspected.

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// holdSigningLock takes the signing folder's lock through a descriptor of the
// test's own, as a second Desk process would, and answers its release.
func holdSigningLock(t *testing.T, folder string) (release func()) {
	t.Helper()
	file, err := os.Open(folder)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		t.Fatalf("the lock could not be held: %v", err)
	}
	released := false
	release = func() {
		if !released {
			released = true
			file.Close()
		}
	}
	t.Cleanup(release)
	return release
}

// shortLockWait makes a creation or a rotation wait for at most wait.
func shortLockWait(t *testing.T, wait time.Duration) {
	t.Helper()
	was := signingLockWait
	signingLockWait = wait
	t.Cleanup(func() { signingLockWait = was })
}

// noSigningLock stands in a file system on which no flock can be taken.
func noSigningLock(t *testing.T) {
	t.Helper()
	was := lockSigningFile
	lockSigningFile = func(*os.File) error { return syscall.ENOTSUP }
	t.Cleanup(func() { lockSigningFile = was })
}

// **While another process holds the lock, a start's sweep removes nothing.**
// A creation stopped after the runtime wrote its seed leaves its marker and
// seed. A second Desk holds the signing folder's lock, as one making a desk
// there would: the next start leaves both, and says why; once the lock is
// released, the start after removes them.
func TestTheSweepRemovesNothingWhileTheLockIsHeld(t *testing.T) {
	const id = "d1000000000000000000000000000001"
	calls, s, ts, _ := signingStandIn(t)
	signsDesks(t, calls, s.configDir, id)
	testHookKeyBetween = func(at string) {
		if at == "after generate" {
			panic(http.ErrAbortHandler)
		}
	}
	t.Cleanup(func() { testHookKeyBetween = nil })
	postAbandoned(t, s, ts.URL)
	testHookKeyBetween = nil
	folder := signingFolderOf(s)
	left := []string{id + ".creating", id + ".seed"}
	if names := namesIn(t, folder); !slices.Equal(names, left) {
		t.Fatalf("the stopped creation left %q", names)
	}
	ts.Close()
	release := holdSigningLock(t, folder)
	again, logged := restartedServer(t, s)
	if names := namesIn(t, folder); !slices.Equal(names, left) {
		t.Errorf("with the lock held elsewhere the start left %q, want %q", names, left)
	}
	if !strings.Contains(logged.String(), "unfinished creations' keys were left for the next start, because the signing folder's lock was not taken: another Desk process is changing keys in this configuration folder") {
		t.Errorf("Desk's log does not say why: %s", logged)
	}
	release()
	_, logged = restartedServer(t, again)
	if names := liveIn(t, folder); len(names) != 0 {
		t.Errorf("with the lock free the start left %q (%s)", names, logged)
	}
	if got := archivedIn(t, folder, id); !slices.Equal(got, kindsArchived(archiveNeverPublished, "seed", "creating")) {
		t.Errorf("with the lock free the start archived %q", got)
	}
}

// **While another process holds the lock, a start's recovery changes
// nothing.** A rotation stopped after the runtime wrote its line is left as it
// is, byte for byte, while a second Desk holds the lock; once it is released,
// the next start finishes it.
func TestTheRecoveryChangesNothingWhileTheLockIsHeld(t *testing.T) {
	r := newRotationRig(t, "d2000000000000000000000000000001", "")
	r.writeTrail(t, 1, recordLine(standInKeyID, 1))
	r.abandonRotation(t, "rotation: line written")
	before := r.snapshot(t)
	r.ts.Close()
	release := holdSigningLock(t, r.signing)
	again, logged := restartedServer(t, r.s)
	if after := r.snapshot(t); after != before {
		t.Errorf("with the lock held elsewhere the start changed\n%s\ninto\n%s", before, after)
	}
	if !strings.Contains(logged.String(), "unfinished rotations were left for the next start, because the signing folder's lock was not taken: another Desk process is changing keys in this configuration folder") {
		t.Errorf("Desk's log does not say why: %s", logged)
	}
	release()
	_, logged = restartedServer(t, again)
	if got := r.describe(t); got != ".keys.jsonl,.seed seed=2 next=absent keys=2 rotations=1 archived=seed rotation-promoted" {
		t.Errorf("with the lock free the start left %s (%s)", got, logged)
	}
}

// **A creation waits for the lock a bounded time, and then refuses.** While
// another process holds it, a creation waits, and then refuses in plain words
// with no path: no desk, and nothing in the signing folder. Released while it
// waits, the creation goes on, and the desk is made signed.
func TestACreationWaitsForTheLockAndThenRefuses(t *testing.T) {
	const id, later = "d3000000000000000000000000000001", "d3000000000000000000000000000002"
	calls, s, ts, _ := signingStandIn(t)
	signsDesks(t, calls, s.configDir, id, later)
	folder := signingFolderOf(s)
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	release := holdSigningLock(t, folder)
	shortLockWait(t, 300*time.Millisecond)
	started := time.Now()
	status, data := deskCall(t, ts, "POST", "/api/desks", "", `{"name":"Waited"}`, true)
	if waited := time.Since(started); waited < 300*time.Millisecond {
		t.Errorf("the creation refused after %v, without waiting", waited)
	}
	if status != http.StatusConflict || refusalOf(data) != "The desk was not created: another Desk process is changing keys in this configuration folder; try again." {
		t.Errorf("the creation answered %d %s", status, data)
	}
	if strings.Contains(string(data), s.configDir) {
		t.Errorf("the refusal names a path: %s", data)
	}
	if names := namesIn(t, folder); len(names) != 0 {
		t.Errorf("a refused creation left %q", names)
	}
	if names := namesIn(t, filepath.Join(s.configDir, "desks")); slices.Contains(names, id) {
		t.Errorf("a refused creation left its folder: %q", names)
	}

	shortLockWait(t, 10*time.Second)
	go func() {
		time.Sleep(200 * time.Millisecond)
		release()
	}()
	started = time.Now()
	row := createGatedDesk(t, ts)
	if waited := time.Since(started); waited < 200*time.Millisecond {
		t.Errorf("the creation went on after %v, while the lock was held", waited)
	}
	if row.ID != later || !row.Signed {
		t.Errorf("once the lock was released the creation made %+v", row)
	}
}

// **A rotation waits for the lock a bounded time, and then refuses.** While
// another process holds it, a rotation waits, refuses in plain words, and
// changes nothing, the runtime never asked for a key. Released while it
// waits, the rotation goes on.
func TestARotationWaitsForTheLockAndThenRefuses(t *testing.T) {
	r := newRotationRig(t, "d4000000000000000000000000000001", "")
	r.writeTrail(t, 1, recordLine(standInKeyID, 1))
	token := r.token(t)
	r.rig.ran(t)
	release := holdSigningLock(t, r.signing)
	shortLockWait(t, 300*time.Millisecond)
	started := time.Now()
	status, data := r.rotate(t, token)
	if waited := time.Since(started); waited < 300*time.Millisecond {
		t.Errorf("the rotation refused after %v, without waiting", waited)
	}
	if status != http.StatusConflict || refusalOf(data) != "Nothing was rotated: another Desk process is changing keys in this configuration folder; try again." {
		t.Errorf("the rotation answered %d %s", status, data)
	}
	if calls := r.rig.ran(t); calls != nil {
		t.Errorf("a rotation that did not take the lock ran %q", calls)
	}
	r.assertUnchanged(t)

	shortLockWait(t, 10*time.Second)
	go func() {
		time.Sleep(200 * time.Millisecond)
		release()
	}()
	status, data = r.rotate(t, token)
	if status != http.StatusOK {
		t.Errorf("once the lock was released the rotation answered %d %s", status, data)
	}
}

// **Where no lock can be taken, nothing is removed without one.** On a file
// system with no flock, a creation and a rotation go on as they would with
// the lock, and the start's sweep and recovery leave everything, and say why.
func TestWithNoLockTheStartRemovesNothing(t *testing.T) {
	noSigningLock(t)
	const id = "d5000000000000000000000000000001"
	calls, s, ts, _ := signingStandIn(t)
	signsDesks(t, calls, s.configDir, id)
	testHookKeyBetween = func(at string) {
		if at == "after generate" {
			panic(http.ErrAbortHandler)
		}
	}
	t.Cleanup(func() { testHookKeyBetween = nil })
	postAbandoned(t, s, ts.URL)
	testHookKeyBetween = nil
	folder := signingFolderOf(s)
	left := namesIn(t, folder)
	if !slices.Equal(left, []string{id + ".creating", id + ".seed"}) {
		t.Fatalf("the creation, without the lock, did not run as before: it left %q", left)
	}
	ts.Close()
	_, logged := restartedServer(t, s)
	if names := namesIn(t, folder); !slices.Equal(names, left) {
		t.Errorf("without the lock the start left %q, want %q", names, left)
	}
	if !strings.Contains(logged.String(), "the signing folder's lock was not taken: no lock can be taken on Desk's signing folder here") {
		t.Errorf("Desk's log does not say why: %s", logged)
	}

	r := newRotationRig(t, "d5000000000000000000000000000002", "")
	r.writeTrail(t, 1, recordLine(standInKeyID, 1))
	r.abandonRotation(t, "rotation: line written")
	before := r.snapshot(t)
	r.ts.Close()
	_, logged = restartedServer(t, r.s)
	if after := r.snapshot(t); after != before {
		t.Errorf("without the lock the start changed\n%s\ninto\n%s", before, after)
	}
	if !strings.Contains(logged.String(), "unfinished rotations were left for the next start") {
		t.Errorf("Desk's log does not say why: %s", logged)
	}

	other := newRotationRig(t, "d5000000000000000000000000000003", "")
	other.writeTrail(t, 1, recordLine(standInKeyID, 1))
	if status, data := other.rotate(t, other.token(t)); status != http.StatusOK {
		t.Errorf("without the lock a rotation answered %d %s, want it made as before", status, data)
	}
}

// **The sweep archives only what it inspected, the marker last.** Between the
// sweep's look and its moves, the unpublished creation's seed is replaced by
// another file, as another process could: the list goes to the archive, the
// file put in the seed's place stays, and so does the marker, for the next
// start.
func TestTheSweepRemovesOnlyWhatItInspected(t *testing.T) {
	const id = "d6000000000000000000000000000001"
	calls, s, ts, _ := signingStandIn(t)
	signsDesks(t, calls, s.configDir, id)
	testHookKeyBetween = func(at string) {
		if at == "before publish" {
			panic(http.ErrAbortHandler)
		}
	}
	t.Cleanup(func() { testHookKeyBetween = nil })
	postAbandoned(t, s, ts.URL)
	folder := signingFolderOf(s)
	seed := filepath.Join(folder, id+".seed")
	testHookKeyBetween = func(at string) {
		if at == "sweep: inspected" {
			if os.Rename(seed, seed+".aside") != nil || os.WriteFile(seed, []byte("another\n"), 0o600) != nil {
				t.Error("could not replace the seed")
			}
		}
	}
	ts.Close()
	_, logged := restartedServer(t, s)
	testHookKeyBetween = nil
	if names := liveIn(t, folder); !slices.Equal(names, []string{id + ".creating", id + ".seed", id + ".seed.aside"}) {
		t.Errorf("the sweep left %q (%s)", names, logged)
	}
	if got := archivedIn(t, folder, id); !slices.Equal(got, kindsArchived(archiveNeverPublished, "keys.jsonl")) {
		t.Errorf("the sweep archived %q", got)
	}
	if got := readFile(t, seed); got != "another\n" {
		t.Errorf("the file put in the seed's place was changed: %q", got)
	}
}

// **The recovery removes only what it inspected, the marker last.** A
// rotation stopped before the runtime wrote its line is undone at the next
// start; between the recovery's look and its removals, the next seed is
// replaced by another file. That file stays, and so does the marker.
func TestTheRecoveryRemovesOnlyWhatItInspected(t *testing.T) {
	r := newRotationRig(t, "d7000000000000000000000000000001", "")
	r.writeTrail(t, 1, recordLine(standInKeyID, 1))
	r.abandonRotation(t, "rotation: next generated")
	next := r.nextPath()
	testHookKeyBetween = func(at string) {
		if at == "rotation: inspected" {
			if os.Rename(next, next+".aside") != nil || os.WriteFile(next, []byte(thirdSeed+"\n"), 0o600) != nil {
				t.Error("could not replace the next key")
			}
		}
	}
	t.Cleanup(func() { testHookKeyBetween = nil })
	r.ts.Close()
	_, logged := restartedServer(t, r.s)
	testHookKeyBetween = nil
	if got := r.describe(t); got != ".keys.jsonl,.next.seed,.next.seed.aside,.rotating,.seed seed=1 next=3 keys=1 rotations=0" {
		t.Errorf("the recovery left %s (%s)", got, logged)
	}
}

// **The lock helper.** It is exclusive between two descriptors of one folder,
// says which way it failed, and its release may be called more than once.
func TestLockSigningIsExclusiveAndSaysWhy(t *testing.T) {
	folder := t.TempDir()
	open := func() *signingDir {
		root, err := os.OpenRoot(folder)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { root.Close() })
		return &signingDir{root: root, path: folder}
	}
	a, b := open(), open()
	unlock, err := lockSigning(a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockSigning(b); !errors.Is(err, errSigningBusy) {
		t.Errorf("a second descriptor took the lock: %v", err)
	}
	unlock()
	unlock()
	again, err := lockSigning(b)
	if err != nil {
		t.Errorf("the released lock could not be taken: %v", err)
	}
	again()
	noSigningLock(t)
	if _, err := lockSigning(a); !errors.Is(err, errSigningNoLock) {
		t.Errorf("a file system with no flock answered %v", err)
	}
}

// lockTaken is whether another descriptor holds the signing folder's lock
// now, asked through a descriptor of the test's own.
func lockTaken(t *testing.T, folder string) bool {
	t.Helper()
	file, err := os.Open(folder)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		return false
	}
	if !errors.Is(err, syscall.EWOULDBLOCK) {
		t.Fatal(err)
	}
	return true
}

// **The lock is held from the first look to the last change.** At every
// moment of a creation, from its marker to the manifest written with the
// marker not yet removed; of a rotation, from its marker to the seed renamed;
// of the start's sweep and recovery, once they have looked: another process
// would find the lock held. Once each has ended, it is free.
func TestTheLockIsHeldFromTheFirstLookToTheLastChange(t *testing.T) {
	const id = "d8000000000000000000000000000001"
	calls, s, ts, _ := signingStandIn(t)
	signsDesks(t, calls, s.configDir, id)
	folder := signingFolderOf(s)
	var seen []string
	watch := func(steps ...string) {
		seen = nil
		testHookKeyBetween = func(at string) {
			if slices.Contains(steps, at) {
				seen = append(seen, at+": "+map[bool]string{true: "held", false: "free"}[lockTaken(t, folder)])
			}
		}
	}
	t.Cleanup(func() { testHookKeyBetween = nil })
	creation := []string{"before generate", "after generate", "before publish", "published"}
	watch(creation...)
	createSignedDesk(t, s, ts, calls, id)
	testHookKeyBetween = nil
	want := []string{"before generate: held", "after generate: held", "before publish: held", "published: held"}
	if !slices.Equal(seen, want) {
		t.Errorf("during a creation the lock was %q, want %q", seen, want)
	}
	if lockTaken(t, folder) {
		t.Error("the lock is held after the creation ended")
	}

	r := newRotationRig(t, "d8000000000000000000000000000002", "")
	r.writeTrail(t, 1, recordLine(standInKeyID, 1))
	token := r.token(t)
	folder = r.signing
	steps := []string{"rotation: marker written", "rotation: next generated", "rotation: line written", "rotation: list written", "rotation: seed renamed"}
	watch(steps...)
	if status, data := r.rotate(t, token); status != http.StatusOK {
		t.Fatalf("the rotation answered %d %s", status, data)
	}
	testHookKeyBetween = nil
	want = nil
	for _, step := range steps {
		want = append(want, step+": held")
	}
	if !slices.Equal(seen, want) {
		t.Errorf("during a rotation the lock was %q, want %q", seen, want)
	}
	if lockTaken(t, folder) {
		t.Error("the lock is held after the rotation ended")
	}

	// The start's sweep and recovery, over a creation and a rotation each
	// stopped.
	other := newRotationRig(t, "d8000000000000000000000000000003", "")
	other.writeTrail(t, 1, recordLine(standInKeyID, 1))
	other.abandonRotation(t, "rotation: next generated")
	if err := os.WriteFile(filepath.Join(other.signing, "d8000000000000000000000000000009.creating"), plantedAs("d8000000000000000000000000000009.creating"), 0o600); err != nil {
		t.Fatal(err)
	}
	folder = other.signing
	other.ts.Close()
	watch("sweep: inspected", "rotation: inspected")
	_, logged := restartedServer(t, other.s)
	testHookKeyBetween = nil
	if want := []string{"sweep: inspected: held", "rotation: inspected: held"}; !slices.Equal(seen, want) {
		t.Errorf("during the start the lock was %q, want %q (%s)", seen, want, logged)
	}
	if lockTaken(t, folder) {
		t.Error("the lock is held after the start")
	}
}
