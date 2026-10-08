package desk

// A rotation's journal and the trail it names (issue #285), and the start's
// recovery of a desk opened directly, and before any resumed desk's Runner
// (issue #286): the ADR-0010 line audit's findings 3 and 4. A stand-in
// runtime, by absolute path, answers each command; the last test drives the
// published runtime and skips without one.

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// otherTrail is a second trail id in the runtime's form: a trail begun after
// the fixture's was moved aside.
const otherTrail = "4c7e2a91d03b55f6a8e1c9b2047d6f13"

// onTrail is a sidecar line of fixtureTrail's, as it would be on trail.
func onTrail(line, trail string) string {
	return strings.Replace(line, `"trail":"`+fixtureTrail+`"`, `"trail":"`+trail+`"`, 1)
}

// moveTrailAside moves the desk's trail and its sidecar aside together, as
// the guide says to after a lost key, and begins a new trail of records
// lines with sidecar, where they are not empty.
func (r *rotationRig) moveTrailAside(t *testing.T, records int, sidecar string) {
	t.Helper()
	for _, name := range []string{"evaluations.jsonl", "signatures.jsonl"} {
		path := filepath.Join(r.auditFolder(), name)
		if err := os.Rename(path, path+".aside"); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	r.writeTrail(t, records, sidecar)
}

// marker is the rotation's marker, as the signing folder holds it now.
func (r *rotationRig) marker(t *testing.T) string {
	t.Helper()
	return readFile(t, filepath.Join(r.signing, r.id+rotatingSuffix))
}

// **A rotation whose trail was moved aside after the runtime wrote it keeps
// both keys** (the line audit's finding 3). Stopped after the runtime wrote
// its line, before the list, and after the list, before the rename; then the
// trail and its sidecar moved aside together, or replaced by another trail.
// The next start keeps the current seed, the next seed, the list and the
// marker as they are, and says why; the decision record says the rotation
// did not finish, and why.
func TestARotationWhoseTrailWasMovedAsideKeepsBothKeys(t *testing.T) {
	const id = "c9100000000000000000000000000001"
	for _, at := range []string{"rotation: line written", "rotation: list written"} {
		for _, then := range []string{"moved aside", "replaced"} {
			t.Run(at+", "+then, func(t *testing.T) {
				r := newRotationRig(t, id, "")
				r.writeTrail(t, 1, recordLine(standInKeyID, 1))
				r.abandonRotation(t, at)
				left := r.describe(t)
				if !strings.Contains(left, ".next.seed,.rotating,.seed seed=1 next=2") {
					t.Fatalf("the stop left %s", left)
				}
				list := readFile(t, filepath.Join(r.signing, id+keysSuffix))
				switch then {
				case "moved aside":
					r.moveTrailAside(t, 0, "")
				case "replaced":
					r.moveTrailAside(t, 1, onTrail(recordLine(standInKeyID, 1), otherTrail))
				}
				_, ts, logged := r.restart(t)
				if got, want := r.describe(t), strings.Replace(left, "rotations=1", "rotations=0", 1); got != want {
					t.Errorf("after the next start the folder holds %s, want %s (%s)", got, want, logged)
				}
				if got := readFile(t, filepath.Join(r.signing, id+keysSuffix)); got != list {
					t.Errorf("the list was changed to %q", got)
				}
				if !strings.Contains(logged.String(), "moved aside or replaced") {
					t.Errorf("the start did not say why: %s", logged)
				}
				answer, _ := panelOn(t, ts, id)
				if answer.Rotation.State != rotationUnfinished || !strings.Contains(answer.Rotation.Reason, "the trail it was made on was moved aside or replaced since") ||
					!strings.Contains(answer.Rotation.Reason, "Desk keeps the current key, the next key and the list of public keys as they are") {
					t.Errorf("the panel says %+v", answer.Rotation)
				}
			})
		}
	}
}

// **A rotation journals each step before it makes it**: the trail it is made
// on, the next key once it is made, and the sequence the runtime answered.
// Each marker is the user's alone and in one spelling.
func TestARotationJournalsEachStep(t *testing.T) {
	const id = "c9200000000000000000000000000001"
	r := newRotationRig(t, id, "")
	r.writeTrail(t, 1, recordLine(standInKeyID, 1))
	token := r.token(t)
	seen := map[string]string{}
	testHookKeyBetween = func(at string) {
		switch at {
		case "rotation: marker written", "rotation: rotate journalled", "rotation: finish journalled", "rotation: list written":
			seen[at] = r.marker(t)
			if mode := permOf(t, filepath.Join(r.signing, id+rotatingSuffix)); mode != 0o600 {
				t.Errorf("at %s the marker is %v", at, mode)
			}
		}
	}
	t.Cleanup(func() { testHookKeyBetween = nil })
	if status, data := r.rotate(t, token); status != 200 {
		t.Fatalf("the rotation answered %d %s", status, data)
	}
	testHookKeyBetween = nil
	for at, want := range map[string]string{
		"rotation: marker written":    `{"version":"1","phase":"generate","trail":"` + fixtureTrail + `","next":"","at":0}` + "\n",
		"rotation: rotate journalled": `{"version":"1","phase":"rotate","trail":"` + fixtureTrail + `","next":"` + secondPublicKey + `","at":0}` + "\n",
		"rotation: finish journalled": `{"version":"1","phase":"finish","trail":"` + fixtureTrail + `","next":"` + secondPublicKey + `","at":1}` + "\n",
		"rotation: list written":      `{"version":"1","phase":"finish","trail":"` + fixtureTrail + `","next":"` + secondPublicKey + `","at":1}` + "\n",
	} {
		if seen[at] != want {
			t.Errorf("at %s the marker was %q, want %q", at, seen[at], want)
		}
	}

	t.Run("a stop before the runtime was asked goes, whatever the trail", func(t *testing.T) {
		const id = "c9200000000000000000000000000002"
		r := newRotationRig(t, id, "")
		r.writeTrail(t, 1, recordLine(standInKeyID, 1))
		r.abandonRotation(t, "rotation: next generated")
		r.moveTrailAside(t, 0, "")
		r.restart(t)
		if got := r.describe(t); got != ".keys.jsonl,.seed seed=1 next=absent keys=1 rotations=0" {
			t.Errorf("after the next start the folder holds %s", got)
		}
	})

	t.Run("a marker that is not a journal decides nothing", func(t *testing.T) {
		const id = "c9200000000000000000000000000003"
		r := newRotationRig(t, id, "")
		r.writeTrail(t, 1, recordLine(standInKeyID, 1))
		r.abandonRotation(t, "rotation: line written")
		journal := r.marker(t)
		for _, other := range []string{
			strings.Replace(journal, `"phase":"rotate"`, `"phase": "rotate"`, 1),
			strings.Replace(journal, `"phase":"rotate"`, `"phase":"finish"`, 1),
			strings.TrimSuffix(journal, "\n"),
			journal + strings.Repeat(" ", rotationJournalLimit),
		} {
			if err := os.WriteFile(filepath.Join(r.signing, id+rotatingSuffix), []byte(other), 0o600); err != nil {
				t.Fatal(err)
			}
			before := r.snapshot(t)
			again, _, logged := r.restart(t)
			if got := r.snapshot(t); got != before {
				t.Errorf("a marker %q changed the folders: %s", other, logged)
			}
			if !strings.Contains(logged.String(), "its marker could not be read as the journal Desk writes") {
				t.Errorf("a marker %q was not said: %s", other, logged)
			}
			r.s = again
		}
	})
}

// **A marker an earlier Desk left is finished only where the sidecar hands
// over to its next key.** An empty marker records no trail: where the
// sidecar's last rotation hands over to the next key, the runtime wrote it
// there, and it is finished; where no line names the next key, the trail may
// be one begun since, and nothing is changed.
func TestAMarkerWithNoJournalIsFinishedOnlyWhereTheSidecarHandsOver(t *testing.T) {
	for _, tc := range []struct {
		name, sidecar, after string
	}{
		{"handed over", recordLine(standInKeyID, 1) + rotationLine(1, standInKeyID, secondPublicKey), ".keys.jsonl,.seed seed=2 next=absent keys=2 rotations=1"},
		{"not named", recordLine(standInKeyID, 1), ".keys.jsonl,.next.seed,.rotating,.seed seed=1 next=2 keys=1 rotations=0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const id = "c9300000000000000000000000000001"
			r := newRotationRig(t, id, "")
			r.writeTrail(t, 1, tc.sidecar)
			if os.WriteFile(filepath.Join(r.signing, id+rotatingSuffix), nil, 0o600) != nil || os.WriteFile(r.nextPath(), []byte(secondSeed+"\n"), 0o600) != nil {
				t.Fatal("could not leave the stop")
			}
			_, ts, logged := r.restart(t)
			if got := r.describe(t); got != tc.after {
				t.Errorf("after the next start the folder holds %s, want %s (%s)", got, tc.after, logged)
			}
			if tc.name == "not named" {
				if answer, _ := panelOn(t, ts, id); answer.Rotation.State != rotationUnfinished || !strings.Contains(answer.Rotation.Reason, "begun by an earlier Desk") {
					t.Errorf("the panel says %+v", answer.Rotation)
				}
			}
		})
	}
}

// **A trail begun after a rotation is read as its own** (the line audit's
// finding 3, its second part; ADR-0010 section 1, "moved aside"). A desk
// rotates from the first key to the second, and the list records the trail
// the rotation was made on; the trail and its sidecar are moved aside, and
// the next record begins a new trail, signed by the key Desk kept. The
// decision record passes that key alone to `audit verify`, shows it as the
// new trail's first key, and offers the next rotation, which is made, its
// sequence counted on the new trail; before the new trail's first record, it
// is read so too. Where the sidecar is the same trail the rotation recorded,
// with its rotation line gone, no key is passed.
func TestATrailBegunAfterARotationIsReadAsItsOwn(t *testing.T) {
	const id = "c9400000000000000000000000000001"
	r := newRotationRig(t, id, "")
	r.writeTrail(t, 1, recordLine(standInKeyID, 1))
	if status, data := r.rotate(t, r.token(t)); status != 200 {
		t.Fatalf("the rotation answered %d %s", status, data)
	}
	if got := readFile(t, filepath.Join(r.signing, id+keysSuffix)); got != wantKeyLine(standInPublicKey, standInKeyID, 0)+wantKeyLineOn(secondPublicKey, secondKeyID, 1, fixtureTrail) {
		t.Fatalf("the rotation wrote the list %q", got)
	}
	r.moveTrailAside(t, 1, onTrail(recordLine(secondKeyID, 1), otherTrail))
	if err := os.Remove(r.rig.calls + ".keys"); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	answer, data := r.panel(t)
	if keys := keysOf(t, answer); keys.State != keysKept || !slices.Equal(keys.Public, []deskPublicKey{{secondPublicKey, secondKeyID, 0, ""}}) {
		t.Fatalf("the panel shows %+v: %s", keys, data)
	}
	if got := readFile(t, r.rig.calls+".keys"); got != secondPublicKey+"\n" {
		t.Errorf("audit verify was given the keys %q, want the new trail's alone", got)
	}
	if answer.Rotation.State != rotationAvailable {
		t.Fatalf("the panel offers %+v", answer.Rotation)
	}
	if got := readFile(t, filepath.Join(r.signing, id+keysSuffix)); got != wantKeyLine(standInPublicKey, standInKeyID, 0)+wantKeyLineOn(secondPublicKey, secondKeyID, 1, fixtureTrail) {
		t.Errorf("the list Desk keeps is %q", got)
	}
	if status, data := r.rotate(t, answer.Rotation.Token); status != 200 {
		t.Fatalf("the rotation on the new trail answered %d %s", status, data)
	}
	if got := r.describe(t); got != ".keys.jsonl,.seed seed=3 next=absent keys=3 rotations=1" {
		t.Errorf("after the rotation on the new trail the folder holds %s", got)
	}
	// The second rotation took over at the new trail's first record, before
	// the first rotation's sequence: counted on its own trail.
	if got := readFile(t, filepath.Join(r.signing, id+keysSuffix)); got != wantKeyLine(standInPublicKey, standInKeyID, 0)+wantKeyLineOn(secondPublicKey, secondKeyID, 1, fixtureTrail)+wantKeyLineOn(thirdPublicKey, thirdKeyID, 1, otherTrail) {
		t.Errorf("after the rotation on the new trail the list is %q", got)
	}

	t.Run("before the new trail's first record", func(t *testing.T) {
		const id = "c9400000000000000000000000000003"
		r := newRotationRig(t, id, "")
		r.writeTrail(t, 1, recordLine(standInKeyID, 1))
		if status, data := r.rotate(t, r.token(t)); status != 200 {
			t.Fatalf("the rotation answered %d %s", status, data)
		}
		r.moveTrailAside(t, 0, "")
		answer, data := r.panel(t)
		if keys := keysOf(t, answer); keys.State != keysKept || !slices.Equal(keys.Public, []deskPublicKey{{secondPublicKey, secondKeyID, 0, ""}}) {
			t.Errorf("before the new trail's first record the panel shows %s", data)
		}
	})

	t.Run("the same trail with its rotation gone", func(t *testing.T) {
		const id = "c9400000000000000000000000000002"
		r := newRotationRig(t, id, "")
		r.writeTrail(t, 1, recordLine(standInKeyID, 1))
		if status, data := r.rotate(t, r.token(t)); status != 200 {
			t.Fatalf("the rotation answered %d %s", status, data)
		}
		// The rotation line gone from the sidecar of the trail it was made
		// on, whose identity the list recorded.
		r.writeTrail(t, 2, recordLine(standInKeyID, 1)+recordLine(secondKeyID, 2))
		answer, _ := r.panel(t)
		if keys := keysOf(t, answer); keys.State != keysUnread || !strings.Contains(keys.Problem, "records 0 key rotations, and the list names 1 keys after this trail's first") {
			t.Errorf("the panel shows %+v", keys)
		}
	})

	t.Run("a rotation finished by a start records its trail", func(t *testing.T) {
		const id = "c9400000000000000000000000000004"
		r := newRotationRig(t, id, "")
		r.writeTrail(t, 1, recordLine(standInKeyID, 1))
		r.abandonRotation(t, "rotation: line written")
		_, ts, _ := r.restart(t)
		if got := readFile(t, filepath.Join(r.signing, id+keysSuffix)); got != wantKeyLine(standInPublicKey, standInKeyID, 0)+wantKeyLineOn(secondPublicKey, secondKeyID, 1, fixtureTrail) {
			t.Fatalf("the start finished the list as %q", got)
		}
		r.moveTrailAside(t, 1, onTrail(recordLine(secondKeyID, 1), otherTrail))
		if answer, data := panelOn(t, ts, id); keysOf(t, answer).State != keysKept {
			t.Errorf("on the new trail the panel shows %s", data)
		}
	})
}

// **A list holds a trail only of the runtime's form, and never on its first
// key**; a key that took over on the same trail as the key before it does so
// at a later sequence (issue #285).
func TestAListOfKeysHoldsItsTrailsToTheirRule(t *testing.T) {
	first := wantKeyLine(standInPublicKey, standInKeyID, 0)
	for _, tc := range []struct {
		name, list string
		ok         bool
	}{
		{"a rotation that recorded its trail", first + wantKeyLineOn(secondPublicKey, secondKeyID, 3, fixtureTrail), true},
		{"a second trail counted from its own start", first + wantKeyLineOn(secondPublicKey, secondKeyID, 3, fixtureTrail) + wantKeyLineOn(thirdPublicKey, thirdKeyID, 1, otherTrail), true},
		{"the same trail counted again", first + wantKeyLineOn(secondPublicKey, secondKeyID, 3, fixtureTrail) + wantKeyLineOn(thirdPublicKey, thirdKeyID, 3, fixtureTrail), false},
		{"a trail of another form", first + wantKeyLineOn(secondPublicKey, secondKeyID, 3, "ZZ"), false},
		{"a first key with a trail", wantKeyLineOn(standInPublicKey, standInKeyID, 0, fixtureTrail), false},
		{"a key from no record", first + wantKeyLineOn(secondPublicKey, secondKeyID, 0, otherTrail), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseDeskKeys([]byte(tc.list)); (err == nil) != tc.ok {
				t.Errorf("the list was read as %v, want %v", err, tc.ok)
			}
		})
	}
	// Bounds on both ends: the most keys a list holds, each with a trail and
	// a sequence of 16 digits, is within what Desk reads.
	var list strings.Builder
	list.WriteString(first)
	for i := 1; i < maxDeskKeys; i++ {
		seed := fmt.Sprintf("%064x", i)
		key := deskPublicKey{PublicKey: seed, KeyID: keyIDOf(seed), At: 9_007_199_254_740_000 + int64(i), Trail: fixtureTrail}
		list.Write(key.line())
	}
	if list.Len() > keysFileLimit {
		t.Errorf("a list of %d keys is %d bytes, past the %d Desk reads", maxDeskKeys, list.Len(), keysFileLimit)
	}
	if _, err := parseDeskKeys([]byte(list.String())); err != nil {
		t.Errorf("the longest list was not read: %v", err)
	}
}

// **A desk Desk made, opened directly, finishes its own rotation** (the line
// audit's finding 4). A rotation stopped after the runtime wrote its line;
// Desk is then started on the desk's folder itself, which its registry
// leaves out: the start finishes the rotation by the desk's own id.
func TestADeskOpenedDirectlyFinishesItsRotation(t *testing.T) {
	const id = "c9500000000000000000000000000001"
	r := newRotationRig(t, id, "")
	r.writeTrail(t, 1, recordLine(standInKeyID, 1))
	r.abandonRotation(t, "rotation: line written")
	r.ts.Close()
	again, _, logged := startedAt(t, r.s, r.desk)
	if again.cfg.deskID != id {
		t.Fatalf("Desk was opened on %q", again.cfg.deskID)
	}
	if got := r.describe(t); got != ".keys.jsonl,.seed seed=2 next=absent keys=2 rotations=1" {
		t.Errorf("after the start on the desk itself the folder holds %s (%s)", got, logged)
	}
}

// **A recovery that finds the lock held waits for it once, a bounded time.**
// Another Desk process holds the signing folder's lock when the start
// recovers a stopped rotation: released within the wait, the rotation is
// finished; held past it, it is left for the next start, and said.
func TestARecoveryWaitsOnceForTheSigningLock(t *testing.T) {
	for _, released := range []bool{true, false} {
		t.Run(map[bool]string{true: "released", false: "held"}[released], func(t *testing.T) {
			const id = "c9600000000000000000000000000001"
			r := newRotationRig(t, id, "")
			r.writeTrail(t, 1, recordLine(standInKeyID, 1))
			r.abandonRotation(t, "rotation: line written")
			left := r.describe(t)
			release := holdSigningLock(t, r.signing)
			if released {
				shortLockWait(t, 10*time.Second)
				go func() {
					time.Sleep(300 * time.Millisecond)
					release()
				}()
			} else {
				shortLockWait(t, 300*time.Millisecond)
			}
			started := time.Now()
			_, _, logged := r.restart(t)
			waited := time.Since(started)
			if !strings.Contains(logged.String(), "unfinished rotations wait for it") {
				t.Errorf("the start did not say it waited: %s", logged)
			}
			if released {
				if waited < 300*time.Millisecond || r.describe(t) != ".keys.jsonl,.seed seed=2 next=absent keys=2 rotations=1" {
					t.Errorf("after %v the folder holds %s (%s)", waited, r.describe(t), logged)
				}
				return
			}
			if got := r.describe(t); got != left || !strings.Contains(logged.String(), "were left for the next start") {
				t.Errorf("held past the wait, the folder holds %s, want %s (%s)", got, left, logged)
			}
		})
	}
}

// **A resumed desk's Runner starts only after the start has recovered the
// rotations** (the line audit's finding 4). A made desk's rotation is
// stopped after the runtime wrote its line; Desk starts again with Runner
// configured. Each resumed desk's Runner decides on its key under the
// signing folder's lock only once the recovery has taken and let go of it,
// so the recovery never finds a Runner's key creation holding it, and the
// rotation is finished; and every Runner then starts on its own.
func TestAResumedDesksRunnerStartsAfterTheRecovery(t *testing.T) {
	const id = "c9700000000000000000000000000001"
	r := newRotationRig(t, id, "")
	r.writeTrail(t, 1, recordLine(standInKeyID, 1))
	r.abandonRotation(t, "rotation: line written")
	r.ts.Close()
	runnerHasLock, recovered := make(chan struct{}), make(chan struct{})
	var hasOnce, recoveredOnce sync.Once
	var runnerFirst atomic.Bool
	testHookKeyBetween = func(at string) {
		switch at {
		case "before generate":
			// A Runner key's creation, under the lock.
			hasOnce.Do(func() { close(runnerHasLock) })
			select {
			case <-recovered:
			case <-time.After(5 * time.Second):
			}
		case "rotations: before the lock":
			select {
			case <-runnerHasLock:
				runnerFirst.Store(true)
			case <-time.After(2 * time.Second):
			}
		case "rotations: locked":
			recoveredOnce.Do(func() { close(recovered) })
		}
	}
	t.Cleanup(func() { testHookKeyBetween = nil })
	runner, booted := recordingRunner(t, false)
	cfg := r.s.cfg
	cfg.Root, cfg.ProjectDir, cfg.RunnerBin, cfg.Logger = nil, r.s.projectDir, runner, log.New(io.Discard, "", 0)
	if err := r.s.Close(); err != nil {
		t.Fatal(err)
	}
	again, ts := startDesk(t, cfg)
	t.Cleanup(func() { again.Close(); ts.Close() })
	again.desksMu.Lock()
	child := again.desks[id]
	again.desksMu.Unlock()
	if child == nil || child.jobs == nil {
		t.Fatal("the desk was not resumed with a Runner")
	}
	// Each Runner starts on its own once its gate is released: the startup
	// desk's and the resumed desk's, with no page open.
	for deadline := time.Now().Add(15 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		data, _ := os.ReadFile(booted)
		if strings.Count(string(data), "\n") >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the Runners did not start on their own: %q", data)
		}
	}
	testHookKeyBetween = nil
	if runnerFirst.Load() {
		t.Error("a resumed desk's Runner held the signing folder's lock before the start recovered the rotations")
	}
	if got := r.describe(t); got != ".keys.jsonl,.seed seed=2 next=absent keys=2 rotations=1" {
		t.Errorf("after the start the folder holds %s", got)
	}
}

// **No key passed, and no rotation, over a sidecar that names more than one
// trail**: the list is held to one trail's sidecar, and a rotation journals
// the one trail it is made on.
func TestNoRotationOverASidecarOfTwoTrails(t *testing.T) {
	const id = "c9800000000000000000000000000001"
	r := newRotationRig(t, id, "")
	r.writeTrail(t, 2, recordLine(standInKeyID, 1)+onTrail(recordLine(standInKeyID, 2), otherTrail))
	answer, data := r.panel(t)
	if keys := keysOf(t, answer); answer.Rotation.State != rotationUnavailable || keys.State != keysUnread || !strings.Contains(keys.Problem, "names more than one trail") {
		t.Errorf("the panel offers %s", data)
	}
}

// **A rotation begun before any signature names its trail first** (review
// round 1 of #302, finding 1). The trail has a chained record and its sidecar
// no line yet: the rotation takes the trail's identity from the runtime's
// checkpoint and journals it before anything is made. A record is signed
// before the runtime rotates; the rotation completes, or stops after the
// runtime wrote its line and the trail is moved aside, when the next start
// keeps both seeds, the list and the marker, and says why. (Where the trail
// has no chained record, nothing is made: TestARotationTheRuntimeRefusesChangesNothing.)
func TestARotationBegunBeforeAnySignatureNamesItsTrail(t *testing.T) {
	const id = "c9900000000000000000000000000001"
	begin := func(t *testing.T) *rotationRig {
		t.Helper()
		r := newRotationRig(t, id, "")
		withCheckpoints(t, r.rig.bin, r.rig.calls)
		if err := os.WriteFile(r.rig.calls+".lines", []byte(chainOf(fixtureTrail, 1)), 0o600); err != nil {
			t.Fatal(err)
		}
		r.writeTrail(t, 1, "")
		return r
	}
	// signedMeanwhile signs a record into the sidecar once the rotation has
	// journalled its next step, before the runtime is asked, and stops the
	// rotation at stop, where it is not empty.
	signedMeanwhile := func(t *testing.T, r *rotationRig, stop string) {
		t.Helper()
		testHookKeyBetween = func(at string) {
			switch at {
			case "rotation: rotate journalled":
				if !strings.Contains(r.marker(t), `"trail":"`+fixtureTrail+`"`) {
					t.Errorf("the rotation journalled %q, not the checkpoint's trail", r.marker(t))
				}
				r.writeTrail(t, 0, recordLine(standInKeyID, 1))
			case stop:
				panic(http.ErrAbortHandler)
			}
		}
		t.Cleanup(func() { testHookKeyBetween = nil })
	}

	t.Run("stopped after the line, the trail moved aside", func(t *testing.T) {
		r := begin(t)
		token := r.token(t)
		signedMeanwhile(t, r, "rotation: line written")
		request, _ := http.NewRequest("POST", r.ts.URL+"/api/audit/key/rotate", strings.NewReader(`{"token":"`+token+`"}`))
		request.Header.Set("Authorization", "Bearer "+testToken)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Jpack-Desk", r.id)
		if response, err := http.DefaultClient.Do(request); err == nil {
			response.Body.Close()
		}
		testHookKeyBetween = nil
		left := r.describe(t)
		r.moveTrailAside(t, 0, "")
		_, ts, logged := r.restart(t)
		if got, want := r.describe(t), strings.Replace(left, "rotations=1", "rotations=0", 1); got != want || !strings.Contains(got, ".next.seed,.rotating,") {
			t.Errorf("after the next start the folder holds %s, want %s (%s)", got, want, logged)
		}
		if answer, _ := panelOn(t, ts, id); answer.Rotation.State != rotationUnfinished || !strings.Contains(answer.Rotation.Reason, "moved aside or replaced") {
			t.Errorf("the panel says %+v", answer.Rotation)
		}
	})

	t.Run("completed", func(t *testing.T) {
		r := begin(t)
		token := r.token(t)
		signedMeanwhile(t, r, "")
		if status, data := r.rotate(t, token); status != 200 {
			t.Fatalf("the rotation answered %d %s", status, data)
		}
		if got := readFile(t, filepath.Join(r.signing, id+keysSuffix)); got != wantKeyLine(standInPublicKey, standInKeyID, 0)+wantKeyLineOn(secondPublicKey, secondKeyID, 1, fixtureTrail) {
			t.Errorf("the list is %q", got)
		}
	})
}

// **A copy of a made desk, opened directly, recovers nothing and rotates
// nothing** (review round 1 of #302, finding 2). The desk is copied before a
// rotation, with its manifest's id and its trail's identity; the original's
// rotation stops after the runtime wrote its line; the copy, whose sidecar
// has no rotation, is opened directly in the same configuration. Its start
// leaves the next seed, the list and the marker as they are, and the log and
// the panel say why. The desk moved out of Desk's desks folder, opened
// directly, is the desk's own, and its start finishes the rotation.
func TestACopyOfAMadeDeskOpenedDirectlyRecoversNothing(t *testing.T) {
	const id = "c9a00000000000000000000000000001"
	t.Run("a copy", func(t *testing.T) {
		r := newRotationRig(t, id, "")
		r.writeTrail(t, 1, recordLine(standInKeyID, 1))
		copied := filepath.Join(t.TempDir(), "copy")
		copyProject(t, r.desk, copied)
		r.abandonRotation(t, "rotation: line written")
		left, list, marker := r.describe(t), readFile(t, filepath.Join(r.signing, id+keysSuffix)), r.marker(t)
		if !strings.Contains(left, ".next.seed,.rotating,.seed seed=1 next=2") {
			t.Fatalf("the stop left %s", left)
		}
		r.ts.Close()
		again, ts, logged := startedAt(t, r.s, copied)
		if again.cfg.deskID != id {
			t.Fatalf("Desk was opened on %q", again.cfg.deskID)
		}
		if got := r.describe(t); got != left {
			t.Errorf("after the copy's start the folder holds %s, want %s (%s)", got, left, logged)
		}
		if readFile(t, filepath.Join(r.signing, id+keysSuffix)) != list || r.marker(t) != marker {
			t.Errorf("the copy's start changed the list or the marker (%s)", logged)
		}
		if !strings.Contains(logged.String(), "left as it is, because "+deskSharedWords) {
			t.Errorf("the start did not say why: %s", logged)
		}
		if answer, _ := panelOn(t, ts, ""); answer.Rotation.State != rotationUnavailable || answer.Rotation.Reason != "Desk rotates no key here: "+deskSharedWords+"." {
			t.Errorf("the copy's panel says %+v", answer.Rotation)
		}
	})

	t.Run("moved", func(t *testing.T) {
		r := newRotationRig(t, id, "")
		r.writeTrail(t, 1, recordLine(standInKeyID, 1))
		r.abandonRotation(t, "rotation: line written")
		r.ts.Close()
		moved := filepath.Join(t.TempDir(), "moved")
		if err := os.Rename(r.desk, moved); err != nil {
			t.Fatal(err)
		}
		r.desk = moved
		again, _, logged := startedAt(t, r.s, moved)
		if again.cfg.deskID != id {
			t.Fatalf("Desk was opened on %q", again.cfg.deskID)
		}
		if got := r.describe(t); got != ".keys.jsonl,.seed seed=2 next=absent keys=2 rotations=1" {
			t.Errorf("after the moved desk's start the folder holds %s (%s)", got, logged)
		}
	})
}

// swapMarker puts data in the place of the rotation's marker, in another
// file: a journal of the rotation's can then no longer be rewritten in its
// place, as one whose write failed.
func (r *rotationRig) swapMarker(t *testing.T, data string) {
	t.Helper()
	path := filepath.Join(r.signing, r.id+rotatingSuffix)
	if err := os.WriteFile(path+".swap", []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".swap", path); err != nil {
		t.Fatal(err)
	}
}

// keptAsLeft checks that the next start, ts, left the signing folder as
// left, with the list and the marker as they were, and that its log and the
// panel say why, in words that hold why.
func (r *rotationRig) keptAsLeft(t *testing.T, ts *httptest.Server, logged *bytes.Buffer, left, list, marker, why string) {
	t.Helper()
	if got := r.describe(t); got != left {
		t.Errorf("after the next start the folder holds %s, want %s (%s)", got, left, logged)
	}
	if got := readFile(t, filepath.Join(r.signing, r.id+keysSuffix)); got != list {
		t.Errorf("the list was changed to %q", got)
	}
	if got := r.marker(t); got != marker {
		t.Errorf("the marker was changed to %q", got)
	}
	if !strings.Contains(logged.String(), "was left as it is: "+why) {
		t.Errorf("the start did not say why: %s", logged)
	}
	answer, _ := panelOn(t, ts, r.id)
	if answer.Rotation.State != rotationUnfinished || answer.Rotation.Reason != "Desk cannot tell whether the runtime wrote the rotation, so it changes nothing: "+why+"." {
		t.Errorf("the panel says %+v", answer.Rotation)
	}
}

// **No list the journal does not stand behind, and no undo the list
// contradicts** (review round 1 of #302, finding 3). The runtime wrote the
// rotation, and its answer could not be journalled: the rotation stops
// there, before the list names the next key, says it did not finish, and the
// next start finishes it from the sidecar. And a list written with the next
// key over a marker left at "rotate", as the answer's failed journal left it
// before, with the trail's sidecar put back from before the rotation: the
// start keeps both seeds, the list and the marker, and says why.
func TestARotationsListIsHeldToItsJournal(t *testing.T) {
	const id = "c9b00000000000000000000000000001"
	t.Run("the answer not journalled", func(t *testing.T) {
		r := newRotationRig(t, id, "")
		r.writeTrail(t, 1, recordLine(standInKeyID, 1))
		token := r.token(t)
		testHookKeyBetween = func(at string) {
			if at == "rotation: line written" {
				r.swapMarker(t, r.marker(t))
			}
		}
		t.Cleanup(func() { testHookKeyBetween = nil })
		status, data := r.rotate(t, token)
		testHookKeyBetween = nil
		if status != http.StatusInternalServerError || !strings.HasPrefix(refusalOf(data), "The rotation of this desk's key did not finish: the runtime wrote it, and Desk could not finish it: its marker could not record the runtime's answer: ") {
			t.Errorf("the rotation answered %d %s", status, data)
		}
		if got := r.describe(t); got != ".keys.jsonl,.next.seed,.rotating,.seed seed=1 next=2 keys=1 rotations=1" {
			t.Errorf("the stop left %s", got)
		}
		if got := readFile(t, filepath.Join(r.signing, id+keysSuffix)); got != wantKeyLine(standInPublicKey, standInKeyID, 0) {
			t.Errorf("the list was written: %q", got)
		}
		if !strings.Contains(r.marker(t), `"phase":"rotate"`) {
			t.Errorf("the marker is %q", r.marker(t))
		}
		_, _, logged := r.restart(t)
		if got := r.describe(t); got != ".keys.jsonl,.seed seed=2 next=absent keys=2 rotations=1" {
			t.Errorf("after the next start the folder holds %s (%s)", got, logged)
		}
	})

	t.Run("a sidecar put back under a list that names the next key", func(t *testing.T) {
		r := newRotationRig(t, id, "")
		before := recordLine(standInKeyID, 1)
		r.writeTrail(t, 1, before)
		r.abandonRotation(t, "rotation: list written")
		r.swapMarker(t, string(rotationJournal{Version: "1", Phase: journalRotate, Trail: fixtureTrail, Next: secondPublicKey}.line()))
		r.writeTrail(t, 0, before)
		left, list, marker := r.describe(t), readFile(t, filepath.Join(r.signing, id+keysSuffix)), r.marker(t)
		if left != ".keys.jsonl,.next.seed,.rotating,.seed seed=1 next=2 keys=2 rotations=0" {
			t.Fatalf("the stop left %s", left)
		}
		_, ts, logged := r.restart(t)
		r.keptAsLeft(t, ts, logged, left, list, marker, rotationListNamesNext)
	})
}
