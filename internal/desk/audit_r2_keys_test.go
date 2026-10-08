package desk

// The second ADR-0010 line audit's key findings (issues #309, #310, #311):
// a copy started after its original moved away takes no custody; a sidecar
// that merely lacks a rotation removes no next key; a made desk moved out of
// the desks folder after publication keeps its key through any start's
// sweep; and a rotation the runtime answered is finished only on the trail it
// was answered for, held through the rename. A stand-in runtime, by absolute
// path, answers each command; the test that drives the published runtime
// skips without one.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// plantedAs is what a test plants as file in Desk's signing folder: for a
// made desk's creation marker, the record a creation stopped before its
// manifest was about to be written leaves (issue #310); otherwise a line no
// reader takes for a key.
func plantedAs(file string) []byte {
	if id, ok := strings.CutSuffix(file, creatingSuffix); ok && deskIDPattern.MatchString(id) {
		return deskCreation{ID: id, Folder: "1:1"}.line()
	}
	return []byte("planted\n")
}

// folderOf is the folder at dir, by device and inode, as the identity file
// records it.
func folderOf(t *testing.T, dir string) string {
	t.Helper()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	return identityKey(info)
}

// seenOf is the sidecar at path as a rotation's journal records it, read now.
func seenOf(t *testing.T, path string) *sidecarSeen {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return &sidecarSeen{File: identityKey(info), Size: int64(len(data)), Digest: "sha256:" + hex.EncodeToString(sum[:])}
}

// bareServer is a Server over project, with Desk's configuration folder
// config, named id where it is a desk Desk made ("" for the project Desk was
// started on), built without serving, so that a test can call the start's
// own steps one at a time, and a stand-in runtime, by absolute path, that
// reads, generates and rotates keys by seed. It is the auditor's probe rig
// (the second line audit), kept as the basis of these tests. jpack.json and
// a trail of one line are written where the project has none.
func bareServer(t *testing.T, project, config, id string) (*Server, *bytes.Buffer) {
	t.Helper()
	root := bareRoot(t, project)
	configRoot := bareRoot(t, config)
	secrets := bareRoot(t, filepath.Join(config, "secrets"))
	bareRoot(t, filepath.Join(config, "secrets", "signing"))
	folder, err := os.Open(project)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { folder.Close() })
	info, err := folder.Stat()
	if err != nil {
		t.Fatal(err)
	}
	logged := &bytes.Buffer{}
	s := &Server{cfg: Config{deskID: id}, root: root, projectDir: project, configDir: config, log: log.New(logged, "", 0),
		assistant: &assistantStore{dir: config, root: configRoot, secrets: secrets},
		project:   &ProjectRoot{dir: project, info: info, own: &ownership{root: root, dirFile: folder}}}
	for name, data := range map[string]string{"jpack.json": `{"configVersion":"6","packs":{},"audit":{"dir":".desk-private/audit"}}`, ".desk-private/audit/evaluations.jsonl": "{}\n"} {
		if _, err := os.Lstat(filepath.Join(project, name)); os.IsNotExist(err) {
			writeBare(t, filepath.Join(project, name), data)
		}
	}
	bin := filepath.Join(config, "..", filepath.Base(config)+"-runtime")
	calls := bin + ".calls"
	writeBare(t, bin, "#!/bin/sh\ncase \"$1 $2 $3\" in\n'audit key public')\n"+publicBySeed+"\n;;\n'audit key generate')\n"+generateBySeed(calls)+"\n;;\n'audit key rotate')\n"+rotateAsTheRuntime(calls)+"\n;;\n*) exit 64;;\nesac\n")
	if err := os.Chmod(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	s.cfg.JpackBin = bin
	return s, logged
}

// bareRoot is the folder at path, made owner-only where it is not there,
// held open for the test.
func bareRoot(t *testing.T, path string) *os.Root {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return root
}

// writeBare writes data at path, 0600, its folders owner-only.
func writeBare(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

// appendBare appends data to the file at path, which stays the same file.
func appendBare(t *testing.T, path, data string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString(data); err != nil {
		t.Fatal(err)
	}
}

// bareKeys gives s the first key, as Desk keeps one, under its signing name:
// the seed, its list, and a sidecar whose one record it signed.
func bareKeys(t *testing.T, s *Server) {
	t.Helper()
	name, signing := s.signingKeyName(), filepath.Join(s.configDir, "secrets", "signing")
	writeBare(t, filepath.Join(signing, name+seedSuffix), standInSeed+"\n")
	writeBare(t, filepath.Join(signing, name+keysSuffix), string(key1.line()))
	writeBare(t, filepath.Join(s.projectDir, ".desk-private", "audit", "signatures.jsonl"), recordLine(standInKeyID, 1))
}

// copyPrivateTree copies the folder from to to, file by file, owner-only, as a copy
// of a project carries its private folder.
func copyPrivateTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, path)
		if entry.IsDir() {
			return os.MkdirAll(filepath.Join(to, rel), 0o700)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(to, rel), data, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// renamedOver puts a new file holding data in the place of the file at path,
// as a restore does: written beside it, then renamed over it, so that it is
// another file while the first is still there.
func renamedOver(t *testing.T, path, data string) {
	t.Helper()
	writeBare(t, path+".restored", data)
	if err := os.Rename(path+".restored", path); err != nil {
		t.Fatal(err)
	}
}

// heldUnder is what Desk's signing folder under config keeps under name, in
// one line: the names that begin with it, which key its seed and its next
// seed hold, and how many keys its list names.
func heldUnder(t *testing.T, config, name string) string {
	t.Helper()
	signing := filepath.Join(config, "secrets", "signing")
	var names []string
	for _, entry := range namesIn(t, signing) {
		if rest, ok := strings.CutPrefix(entry, name); ok {
			names = append(names, rest)
		}
	}
	list, _ := os.ReadFile(filepath.Join(signing, name+keysSuffix))
	return fmt.Sprintf("%s seed=%s next=%s keys=%d", strings.Join(names, ","), seedIs(filepath.Join(signing, name+seedSuffix)),
		seedIs(filepath.Join(signing, name+nextSeedSuffix)), bytes.Count(list, []byte("\n")))
}

// **A copy started after its original moved away takes nothing** (issue
// #309, the second line audit's finding N1, the auditor's scenario). A
// project is copied before a rotation: both hold one identity and one trail.
// The original rotates its key and stops at "rotate", after the runtime
// wrote the rotation into the original's sidecar. The original is moved
// away, and the copy started: its identity's folder no longer holds it, and
// the copy is not that folder, so its identity is unresolved, its file is
// not written, and no recovery is made under it; both seeds and the journal
// stay. With the original put back at its path, the copy is a copy again,
// and the original's own start finishes the rotation its trail committed to.
func TestACopyStartedAfterItsOriginalMovedTakesNothing(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	t.Setenv("JPACK_SIGNING_KEY", "")
	base := t.TempDir()
	config, original, copied := filepath.Join(base, "config"), filepath.Join(base, "original"), filepath.Join(base, "copy")
	id := strings.Repeat("b", 64)
	a, _ := bareServer(t, original, config, "")
	a.setStartup(identityKept, id, "", "")
	bareKeys(t, a)
	identity := string(identityRecord{ID: id, Path: original, Folder: folderOf(t, original)}.line())
	writeBare(t, filepath.Join(original, ".desk-private", "project.json"), identity)
	copyPrivateTree(t, original, copied)
	b, logged := bareServer(t, copied, config, "")
	b.resolveStartupIdentity()
	if !b.startupShared() {
		t.Fatalf("the copy is not taken for a copy while its original is there: %s", logged)
	}

	// The original rotates, and stops at "rotate", the runtime's line written.
	sidecar := filepath.Join(original, ".desk-private", "audit", "signatures.jsonl")
	signing := filepath.Join(config, "secrets", "signing")
	writeBare(t, filepath.Join(signing, id+nextSeedSuffix), secondSeed+"\n")
	writeBare(t, filepath.Join(signing, id+rotatingSuffix), string(rotationJournal{Version: "1", Phase: journalRotate, Trail: fixtureTrail, Next: secondPublicKey, Sidecar: seenOf(t, sidecar)}.line()))
	appendBare(t, sidecar, rotationLine(1, standInKeyID, secondPublicKey))
	left := heldUnder(t, config, id)
	if left != ".keys.jsonl,.next.seed,.rotating,.seed seed=1 next=2 keys=1" {
		t.Fatalf("the stop left %s", left)
	}
	journal := readFile(t, filepath.Join(signing, id+rotatingSuffix))

	moved := original + " moved"
	if err := os.Rename(original, moved); err != nil {
		t.Fatal(err)
	}
	logged.Reset()
	b.resolveStartupIdentity()
	if !b.startupUnresolved() || b.startupShared() || b.startupBound() || b.signingKeyName() != id {
		t.Errorf("the copy, its original moved away, is unresolved=%v shared=%v bound=%v %s: %s", b.startupUnresolved(), b.startupShared(), b.startupBound(), b.signingKeyName(), logged)
	}
	if got := readFile(t, filepath.Join(copied, ".desk-private", "project.json")); got != identity {
		t.Errorf("the copy's identity was written again: %q", got)
	}
	if shared, why := b.identityShared(); !shared || why != unresolvedWords {
		t.Errorf("the copy's identity is shared=%v: %q", shared, why)
	}
	if !strings.Contains(logged.String(), "is not shown to be that one moved here") {
		t.Errorf("the start did not say why: %s", logged)
	}
	b.recoverRotations()
	if got := heldUnder(t, config, id); got != left || readFile(t, filepath.Join(signing, id+rotatingSuffix)) != journal {
		t.Errorf("a start on the copy left %s, want %s: %s", got, left, logged)
	}
	if !strings.Contains(logged.String(), "was left as it is, because "+unresolvedWords) {
		t.Errorf("the start did not say why it recovered nothing: %s", logged)
	}

	// The original put back at its path: the copy is a copy again.
	if err := os.Rename(moved, original); err != nil {
		t.Fatal(err)
	}
	logged.Reset()
	b.resolveStartupIdentity()
	b.recoverRotations()
	if !b.startupShared() || heldUnder(t, config, id) != left {
		t.Errorf("the copy beside its original put back is shared=%v and left %s: %s", b.startupShared(), heldUnder(t, config, id), logged)
	}

	// The original's own start finishes the rotation its trail committed to.
	a.resolveStartupIdentity()
	a.recoverRotations()
	if got := heldUnder(t, config, id); got != ".keys.jsonl,.seed seed=2 next=absent keys=2" {
		t.Errorf("the original's start left %s", got)
	}
}

// **A sidecar that merely lacks the rotation removes no next key** (issue
// #309). A made desk's rotation is journalled at "rotate" beside its sidecar,
// and the next key made; the runtime's line is not in the sidecar the start
// finds. The next key and the marker go only where that sidecar is the file
// the rotation was begun beside, holding its bytes first: as it was, or grown
// by a later record. A copy of the same bytes put in its place, the file cut
// back, other bytes in it, or a journal that records no sidecar, as an
// earlier Desk's, keep both seeds and the journal, and the start says why.
func TestASidecarThatMerelyLacksTheRotationRemovesNoKey(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	t.Setenv("JPACK_SIGNING_KEY", "")
	const kept, undone = ".keys.jsonl,.next.seed,.rotating,.seed seed=1 next=2 keys=1", ".keys.jsonl,.seed seed=1 next=absent keys=1"
	other := strings.Replace(recordLine(standInKeyID, 2), fixtureSignature, strings.Repeat("6b", 64), 1)
	for _, tc := range []struct {
		name     string
		recorded bool
		then     func(t *testing.T, sidecar string)
		after    string
	}{
		{"the sidecar it was begun beside", true, func(*testing.T, string) {}, undone},
		{"that sidecar, grown by a later record", true, func(t *testing.T, sidecar string) { appendBare(t, sidecar, recordLine(standInKeyID, 3)) }, undone},
		{"a copy of its bytes put in its place", true, func(t *testing.T, sidecar string) { renamedOver(t, sidecar, readFile(t, sidecar)) }, kept},
		{"the same file cut back to its first record", true, func(t *testing.T, sidecar string) { writeBare(t, sidecar, recordLine(standInKeyID, 1)) }, kept},
		{"the same file holding other bytes of the same length", true, func(t *testing.T, sidecar string) {
			writeBare(t, sidecar, recordLine(standInKeyID, 1)+other)
		}, kept},
		{"a journal that records no sidecar", false, func(*testing.T, string) {}, kept},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const id = "e3090000000000000000000000000001"
			config := filepath.Join(t.TempDir(), "config")
			s, logged := bareServer(t, filepath.Join(t.TempDir(), "desk"), config, id)
			bareKeys(t, s)
			sidecar := filepath.Join(s.projectDir, ".desk-private", "audit", "signatures.jsonl")
			writeBare(t, sidecar, recordLine(standInKeyID, 1)+recordLine(standInKeyID, 2))
			journal := rotationJournal{Version: "1", Phase: journalRotate, Trail: fixtureTrail, Next: secondPublicKey}
			if tc.recorded {
				journal.Sidecar = seenOf(t, sidecar)
			}
			signing := filepath.Join(config, "secrets", "signing")
			writeBare(t, filepath.Join(signing, id+nextSeedSuffix), secondSeed+"\n")
			writeBare(t, filepath.Join(signing, id+rotatingSuffix), string(journal.line()))
			tc.then(t, sidecar)
			s.recoverRotations()
			if got := heldUnder(t, config, id); got != tc.after {
				t.Errorf("the start left %s, want %s: %s", got, tc.after, logged)
			}
			if tc.after == kept && !strings.Contains(logged.String(), rotationSidecarNotKept) {
				t.Errorf("the start did not say why it kept both keys: %s", logged)
			}
		})
	}
}

// replacedTrail moves the rig's trail and sidecar aside, as the guide says
// to after a lost key, and begins a replacement trail of one record signed
// with the first key, as a deciding run would while the first key is still
// the one the desk names.
func (r *rotationRig) replacedTrail(t *testing.T) {
	t.Helper()
	r.moveTrailAside(t, 1, onTrail(recordLine(standInKeyID, 1), otherTrail))
}

// **A rotation the runtime answered is finished only on the trail it was
// answered for** (issue #311, the second line audit's finding N3, the
// auditor's scenario). The runtime answers that it rotated; the trail and its
// sidecar are then moved aside and a replacement trail begun with the first
// key: right after the answer, after the decision and before the list is
// written, or after the list and immediately before the rename. Each time
// Desk keeps the current key, the next key and the journal, answers the
// conflict and never "rotated", and the panel and the next start say the
// rotation did not finish. The ordinary path still answers "rotated"
// (`TestARotationRunsItsStepsInOrder`).
func TestARotationIsFinishedOnlyOnTheTrailItWasAnsweredFor(t *testing.T) {
	for _, tc := range []struct{ at, left, why string }{
		{"rotation: line written", ".keys.jsonl,.next.seed,.rotating,.seed seed=1 next=2 keys=1 rotations=0", rotationTrailGone},
		{"trail: before the list", ".keys.jsonl,.next.seed,.rotating,.seed seed=1 next=2 keys=1 rotations=0", errTrailMoved.Error()},
		{"trail: before the rename", ".keys.jsonl,.next.seed,.rotating,.seed seed=1 next=2 keys=2 rotations=0", errTrailMoved.Error()},
	} {
		t.Run(tc.at, func(t *testing.T) {
			r := newRotationRig(t, "e3110000000000000000000000000001", "")
			r.writeTrail(t, 1, recordLine(standInKeyID, 1))
			token := r.token(t)
			testHookKeyBetween = func(at string) {
				if at == tc.at {
					r.replacedTrail(t)
				}
			}
			t.Cleanup(func() { testHookKeyBetween = nil })
			status, data := r.rotate(t, token)
			testHookKeyBetween = nil
			if want := fmt.Sprintf(rotationConflictWords, strings.TrimRight(tc.why, ".")); status != http.StatusConflict || refusalOf(data) != want {
				t.Errorf("the rotation answered %d %q, want 409 %q", status, refusalOf(data), want)
			}
			if got := r.describe(t); got != tc.left {
				t.Errorf("the rotation left %s, want %s", got, tc.left)
			}
			if !strings.Contains(r.marker(t), `"phase":"finish"`) {
				t.Errorf("the marker is %q", r.marker(t))
			}
			answer, _ := r.panel(t)
			if answer.Rotation.State != rotationUnfinished || !strings.Contains(answer.Rotation.Reason, rotationTrailGone) {
				t.Errorf("the panel says %+v", answer.Rotation)
			}
			_, _, logged := r.restart(t)
			if got := r.describe(t); got != tc.left {
				t.Errorf("the next start left %s, want %s: %s", got, tc.left, logged)
			}
		})
	}
}

// **The start finishes or undoes a rotation only while the trail it decided
// on is the trail there** (issue #311). Stopped at "rotate" before the
// runtime ran, and at "finish" after it wrote; the start decides, and then,
// before it acts, the trail and its sidecar are moved aside and a
// replacement begun: the next key stays, the list is not written, the
// marker stays, and the start says why. A trail a runtime holds locked past
// the bound, and a trail renamed while the panel binds it, decide nothing.
func TestARecoveryActsOnlyOnTheTrailItDecidedOn(t *testing.T) {
	const id = "e3110000000000000000000000000002"
	for _, tc := range []struct{ stop, left string }{
		{"rotation: rotate journalled", ".keys.jsonl,.next.seed,.rotating,.seed seed=1 next=2 keys=1 rotations=0"},
		{"rotation: finish journalled", ".keys.jsonl,.next.seed,.rotating,.seed seed=1 next=2 keys=1 rotations=0"},
	} {
		t.Run(tc.stop, func(t *testing.T) {
			r := newRotationRig(t, id, "")
			r.writeTrail(t, 1, recordLine(standInKeyID, 1))
			r.abandonRotation(t, tc.stop)
			testHookKeyBetween = func(at string) {
				if at == "rotation: inspected" {
					r.replacedTrail(t)
				}
			}
			t.Cleanup(func() { testHookKeyBetween = nil })
			_, _, logged := r.restart(t)
			testHookKeyBetween = nil
			if got := r.describe(t); got != tc.left {
				t.Errorf("the start left %s, want %s: %s", got, tc.left, logged)
			}
			if !strings.Contains(logged.String(), errTrailMoved.Error()) {
				t.Errorf("the start did not say why: %s", logged)
			}
		})
	}

	t.Run("a trail a runtime holds locked", func(t *testing.T) {
		r := newRotationRig(t, id, "")
		r.writeTrail(t, 1, recordLine(standInKeyID, 1))
		r.abandonRotation(t, "rotation: rotate journalled")
		left := r.describe(t)
		trail, err := os.Open(filepath.Join(r.auditFolder(), "evaluations.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		defer trail.Close()
		if err := syscall.Flock(int(trail.Fd()), syscall.LOCK_EX); err != nil {
			t.Fatal(err)
		}
		saved := auditLockWait
		auditLockWait = 50 * time.Millisecond
		t.Cleanup(func() { auditLockWait = saved })
		_, _, logged := r.restart(t)
		if got := r.describe(t); got != left || !strings.Contains(logged.String(), "a runtime held the trail's lock for too long") {
			t.Errorf("the start left %s, want %s: %s", got, left, logged)
		}
	})

	t.Run("a trail renamed while the panel binds it", func(t *testing.T) {
		r := newRotationRig(t, id, "")
		r.writeTrail(t, 1, recordLine(standInKeyID, 1))
		r.abandonRotation(t, "rotation: rotate journalled")
		once := false
		testHookKeyBetween = func(at string) {
			if at == "trail: bound" && !once {
				once = true
				trail := filepath.Join(r.auditFolder(), "evaluations.jsonl")
				if err := os.Rename(trail, trail+".aside"); err != nil {
					t.Fatal(err)
				}
				writeBare(t, trail, "{\"line\":1}\n")
			}
		}
		t.Cleanup(func() { testHookKeyBetween = nil })
		answer, _ := r.panel(t)
		testHookKeyBetween = nil
		want := "Desk cannot tell whether the runtime wrote the rotation, so it changes nothing: the trail's signature sidecar could not be read: the way to it changed while Desk was opening it."
		if answer.Rotation.State != rotationUnfinished || answer.Rotation.Reason != want {
			t.Errorf("the panel says %+v, want %q", answer.Rotation, want)
		}
	})
}

// **A refused rotation removes its next key only on the trail it decided
// on** (issue #311). The runtime refuses and writes nothing; Desk decides
// that the rotation was not written, and the trail is then replaced before
// the next key is removed: the next key and the marker stay, and the answer
// says so.
func TestARefusedRotationKeepsItsNextKeyWhereTheTrailMoved(t *testing.T) {
	r := newRotationRig(t, "e3110000000000000000000000000003", "")
	r.writeTrail(t, 1, recordLine(standInKeyID, 1))
	token := r.token(t)
	rotatingAs(t, r.rig.calls, refusingWith(rotateNotInForce, 1))
	testHookKeyBetween = func(at string) {
		if at == "trail: refusal decided" {
			r.replacedTrail(t)
		}
	}
	t.Cleanup(func() { testHookKeyBetween = nil })
	status, data := r.rotate(t, token)
	testHookKeyBetween = nil
	want := "The runtime did not rotate the key. It said: The project's signing key is not the key in force in the signature sidecar, so it cannot hand signing over; only the key in force can. Desk did not remove the next key it made, because " + strings.TrimRight(errTrailMoved.Error(), ".") + ". The decision record says a rotation did not finish."
	if status != http.StatusConflict || refusalOf(data) != want {
		t.Errorf("the rotation answered %d %q, want 409 %q", status, refusalOf(data), want)
	}
	if got := r.describe(t); got != ".keys.jsonl,.next.seed,.rotating,.seed seed=1 next=2 keys=1 rotations=0" {
		t.Errorf("the refusal left %s", got)
	}
}

// **The journal's record of the sidecar is held to its form, and fits what
// Desk reads** (issue #309). A journal that records a sidecar at "generate",
// or one in another form, is no journal Desk writes; the longest journal is
// under the bound Desk reads markers to.
func TestARotationsRecordOfItsSidecarIsHeldToItsForm(t *testing.T) {
	seen := &sidecarSeen{File: "2049:131", Size: 285, Digest: "sha256:" + strings.Repeat("ab", 32)}
	rotate := rotationJournal{Version: "1", Phase: journalRotate, Trail: fixtureTrail, Next: secondPublicKey, Sidecar: seen}
	if _, _, err := parseJournal(rotate.line()); err != nil {
		t.Errorf("a journal Desk writes was refused: %v", err)
	}
	for name, journal := range map[string]rotationJournal{
		"a sidecar at generate":    {Version: "1", Phase: journalGenerate, Trail: fixtureTrail, Sidecar: seen},
		"a file in another form":   {Version: "1", Phase: journalRotate, Trail: fixtureTrail, Next: secondPublicKey, Sidecar: &sidecarSeen{File: "2049-131", Size: 285, Digest: seen.Digest}},
		"a digest in another form": {Version: "1", Phase: journalRotate, Trail: fixtureTrail, Next: secondPublicKey, Sidecar: &sidecarSeen{File: seen.File, Size: 285, Digest: strings.Repeat("ab", 32)}},
		"a size below zero":        {Version: "1", Phase: journalRotate, Trail: fixtureTrail, Next: secondPublicKey, Sidecar: &sidecarSeen{File: seen.File, Size: -1, Digest: seen.Digest}},
	} {
		if _, _, err := parseJournal(journal.line()); !errors.Is(err, errJournal) {
			t.Errorf("%s was read: %v", name, err)
		}
	}
	longest := rotationJournal{Version: "1", Phase: journalFinish, Trail: fixtureTrail, Next: secondPublicKey, At: sidecarIntegerLimit - 1,
		Sidecar: &sidecarSeen{File: strings.Repeat("9", 20) + ":" + strings.Repeat("9", 20), Size: sidecarIntegerLimit - 1, Digest: seen.Digest}}
	if n := len(longest.line()); n > rotationJournalLimit {
		t.Errorf("the longest journal is %d bytes, past the %d Desk reads", n, rotationJournalLimit)
	}
}

// stoppedCreation makes desk id on s, signed, and stops it at the step at,
// as a crash would; ts is closed after. It answers the desk's folder in the
// desks folder.
func stoppedCreation(t *testing.T, calls string, s *Server, ts *httptest.Server, id, at string) string {
	t.Helper()
	signsDesks(t, calls, s.configDir, id)
	testHookKeyBetween = func(step string) {
		if step == at {
			panic(http.ErrAbortHandler)
		}
	}
	t.Cleanup(func() { testHookKeyBetween = nil })
	postAbandoned(t, s, ts.URL)
	testHookKeyBetween = nil
	ts.Close()
	if names := namesIn(t, signingFolderOf(s)); !slices.Equal(names, []string{id + ".creating", id + ".keys.jsonl", id + ".seed"}) {
		t.Fatalf("the stopped creation left %q", names)
	}
	return filepath.Join(s.configDir, "desks", id)
}

// startedOn closes s and starts Desk on project with s's configuration and
// runtime, unserved, with a log the test reads.
func startedOn(t *testing.T, s *Server, project string) (*Server, *bytes.Buffer) {
	t.Helper()
	cfg := s.cfg
	cfg.Root, cfg.ProjectDir, cfg.deskID, cfg.parent = nil, project, "", nil
	logged := &bytes.Buffer{}
	cfg.Logger = log.New(logged, "", 0)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { again.Close() })
	return again, logged
}

// **A published desk moved out of the desks folder keeps its key through
// every start's sweep** (issue #310, the second line audit's finding N2, the
// auditor's scenario). A creation is stopped after its manifest is published
// and before its marker goes; its marker records its folder and its
// manifest's digest. The desk is moved out of the desks folder. A start on
// another project against the same configuration removes nothing, and says
// why; a start on the desk itself, opened directly, finds it published and
// removes the marker alone.
func TestAPublishedDeskMovedOutOfTheDesksFolderKeepsItsKey(t *testing.T) {
	const id = "e3100000000000000000000000000001"
	calls, s, ts, _ := signingStandIn(t)
	registry := stoppedCreation(t, calls, s, ts, id, "published")
	folder := signingFolderOf(s)
	manifest := readFile(t, filepath.Join(registry, ".desk-private", "desk.json"))
	if got, want := readFile(t, filepath.Join(folder, id+creatingSuffix)), string(deskCreation{ID: id, Folder: folderOf(t, registry), Manifest: sha256Digest([]byte(manifest))}.line()); got != want {
		t.Errorf("the marker is %q, want %q", got, want)
	}
	moved := filepath.Join(t.TempDir(), "Moved desk\tout here")
	if err := os.Rename(registry, moved); err != nil {
		t.Fatal(err)
	}
	all := []string{id + ".creating", id + ".keys.jsonl", id + ".seed"}

	other, logged := startedOn(t, s, t.TempDir())
	if names := namesIn(t, folder); !slices.Equal(names, all) {
		t.Errorf("a start on another project left %q: %s", names, logged)
	}
	if !strings.Contains(logged.String(), deskMovedWords) {
		t.Errorf("a start on another project did not say why it kept the key: %s", logged)
	}

	direct, logged := startedOn(t, other, moved)
	if direct.cfg.deskID != id {
		t.Fatalf("Desk opened the moved desk as %q", direct.cfg.deskID)
	}
	if names := namesIn(t, folder); !slices.Equal(names, all[1:]) {
		t.Errorf("a start on the desk itself left %q: %s", names, logged)
	}
}

// **A stopped creation's key goes only where the desk was never published**
// (issue #310). Stopped before its manifest was about to be written, its key
// goes, wherever its folder is; stopped once it was about to be written, its
// key goes only where the desks folder still holds the very folder it was
// made in, with no manifest, and stays where that folder was moved out or
// another folder holds its name. An earlier Desk's empty marker goes with its
// key only where a folder of its id with no manifest is there; and a marker
// in no form Desk writes keeps everything.
func TestACreationIsSweptOnlyWhereItWasNeverPublished(t *testing.T) {
	const id = "e3100000000000000000000000000002"
	all := []string{id + ".creating", id + ".keys.jsonl", id + ".seed"}
	moveOut := func(t *testing.T, registry string) {
		if err := os.Rename(registry, filepath.Join(t.TempDir(), "moved")); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name, at string
		then     func(t *testing.T, registry, marker string)
		left     []string
		says     string
	}{
		{"before its manifest was about to be written, its folder moved out", "before publish", func(t *testing.T, registry, _ string) { moveOut(t, registry) }, nil, ""},
		{"as its manifest was about to be written, its folder there", "publishing recorded", func(*testing.T, string, string) {}, nil, ""},
		{"as its manifest was about to be written, its folder moved out", "publishing recorded", func(t *testing.T, registry, _ string) { moveOut(t, registry) }, all, deskMovedWords},
		{"as its manifest was about to be written, another folder at its name", "publishing recorded", func(t *testing.T, registry, _ string) {
			moveOut(t, registry)
			if err := os.Mkdir(registry, 0o700); err != nil {
				t.Fatal(err)
			}
		}, all, deskMovedWords},
		{"an earlier Desk's marker, its folder there", "before publish", func(t *testing.T, _, marker string) { writeBare(t, marker, "") }, nil, ""},
		{"an earlier Desk's marker, its folder moved out", "before publish", func(t *testing.T, registry, marker string) {
			writeBare(t, marker, "")
			moveOut(t, registry)
		}, all, deskMovedWords},
		{"a marker in no form Desk writes", "before publish", func(t *testing.T, _, marker string) { writeBare(t, marker, "planted\n") }, all, "could not be read as the record Desk writes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, s, ts, _ := signingStandIn(t)
			registry := stoppedCreation(t, calls, s, ts, id, tc.at)
			marker := filepath.Join(signingFolderOf(s), id+creatingSuffix)
			tc.then(t, registry, marker)
			_, logged := restartedServer(t, s)
			if names := namesIn(t, signingFolderOf(s)); !slices.Equal(names, tc.left) {
				t.Errorf("the next start left %q, want %q: %s", names, tc.left, logged)
			}
			if tc.says != "" && !strings.Contains(logged.String(), tc.says) {
				t.Errorf("the next start did not say %q: %s", tc.says, logged)
			}
		})
	}
}

// unresolvedProject is a signing upgrade's project whose identity file was
// written in another folder, which no longer holds it, started again: its
// identity is unresolved (issue #309). It answers the server, its handler,
// and the identity file as written.
func unresolvedProject(t *testing.T) (*signingUpgrade, *Server, *httptest.Server, string) {
	t.Helper()
	u := newSigningUpgrade(t, map[string]string{"jpack.json": auditedConfig})
	identity := string(identityRecord{ID: strings.Repeat("d", 64), Path: u.project + " elsewhere", Folder: "1:2"}.line())
	writeBare(t, filepath.Join(u.project, ".desk-private", "project.json"), identity)
	if err := os.Chmod(filepath.Join(u.project, ".desk-private"), 0o700); err != nil {
		t.Fatal(err)
	}
	u.ts.Close()
	again, ts, logged := startedAt(t, u.s, u.project)
	if !again.startupUnresolved() {
		t.Fatalf("the project's identity is not unresolved: %s", logged)
	}
	u.s, u.ts = again, ts
	return u, again, ts, identity
}

// resolve answers the decision record's question on the project's identity.
func resolve(t *testing.T, ts *httptest.Server, choice, token string) (int, []byte) {
	t.Helper()
	return reviewCall(t, ts, "POST", "/api/project/identity", "", map[string]string{"choice": choice, "token": token}, bearer)
}

// **An unresolved identity waits for the owner's word, bound to the choice
// and to the file** (issue #309). A project whose identity was written in a
// folder that no longer holds it, and which is not shown to be that folder:
// no key is offered or rotated, and the decision record asks which it is,
// with a token for each answer. The other answer's token, or a token for a
// file that changed, changes nothing; "moved here" writes the file again with
// this folder, and the identity is this project's own; "a copy" gives it an
// identity of its own; and neither answer is asked again after. No answer
// names a path.
func TestAnUnresolvedIdentityWaitsForTheOwnersWord(t *testing.T) {
	t.Run("moved here", func(t *testing.T) {
		u, s, ts, identity := unresolvedProject(t)
		offer := u.offer(t, true)
		if want := (upgradeSigning{signingNotOffered, "This project is not offered a signing key: " + unresolvedWords + "."}); offer.SigningKey == nil || *offer.SigningKey != want {
			t.Errorf("the upgrade offers %+v", offer.SigningKey)
		}
		_, panel, refusal := readAudit(t, ts, "")
		if panel.Identity == nil || panel.Identity.State != "unresolved" || len(panel.Identity.Moved) != 64 || panel.Identity.Moved == panel.Identity.Copy {
			t.Fatalf("the decision record asks %+v %q", panel.Identity, refusal)
		}
		if panel.Rotation == nil || panel.Rotation.Reason != "Desk rotates no key here: "+unresolvedWords+"." {
			t.Errorf("the decision record offers %+v", panel.Rotation)
		}
		file := filepath.Join(u.project, ".desk-private", "project.json")
		status, data := resolve(t, ts, identityMoved, panel.Identity.Copy)
		if status != http.StatusConflict || refusalOf(data) != "This project's identity changed after the decision record showed it, so nothing was changed. Check the decision record again." || readFile(t, file) != identity {
			t.Errorf("the other answer's token answered %d %s", status, data)
		}
		status, data = resolve(t, ts, identityMoved, panel.Identity.Moved)
		if status != http.StatusOK || string(data) != `{"state":"moved"}`+"\n" {
			t.Fatalf("moved here answered %d %s", status, data)
		}
		if got, want := readFile(t, file), identityLine(strings.Repeat("d", 64), s.projectDir, ""); got != want || !s.startupBound() {
			t.Errorf("the identity is %q bound=%v, want %q", got, s.startupBound(), want)
		}
		if _, again, _ := readAudit(t, ts, ""); again.Identity != nil {
			t.Errorf("the decision record asks again: %+v", again.Identity)
		}
		if status, data := resolve(t, ts, identityMoved, panel.Identity.Moved); status != http.StatusConflict || !strings.Contains(refusalOf(data), "does not wait for your word now") {
			t.Errorf("a second answer answered %d %s", status, data)
		}
	})

	t.Run("a copy", func(t *testing.T) {
		u, s, ts, identity := unresolvedProject(t)
		_, panel, _ := readAudit(t, ts, "")
		if panel.Identity == nil {
			t.Fatal("the decision record asks nothing")
		}
		file := filepath.Join(u.project, ".desk-private", "project.json")
		renamedOver(t, file, identity)
		if status, _ := resolve(t, ts, identityCopy, panel.Identity.Copy); status != http.StatusConflict || readFile(t, file) != identity {
			t.Errorf("a token for a file written since answered %d", status)
		}
		_, panel, _ = readAudit(t, ts, "")
		if panel.Identity == nil {
			t.Fatal("the decision record asks nothing")
		}
		status, data := resolve(t, ts, identityCopy, panel.Identity.Copy)
		if status != http.StatusOK || string(data) != `{"state":"copy"}`+"\n" || strings.Contains(string(data), u.project) {
			t.Fatalf("a copy answered %d %s", status, data)
		}
		name := s.signingKeyName()
		if name == strings.Repeat("d", 64) || !startupIDForm.MatchString(name) || readFile(t, file) != identityLine(name, s.projectDir, "") || !s.startupBound() {
			t.Errorf("the copy is named %s, its identity %q", name, readFile(t, file))
		}
	})
}

// **No identity file Desk would not read back** (bounds on both ends). A
// record whose line is as long as Desk reads is written; one byte more is
// refused before anything is written.
func TestTheIdentityFileIsWrittenOnlyWithinWhatDeskReads(t *testing.T) {
	private := bareRoot(t, t.TempDir())
	record := identityRecord{ID: strings.Repeat("e", 64), Folder: "2049:131"}
	record.Path = "/" + strings.Repeat("p", startupIdentityLimit-len(identityRecord{ID: record.ID, Path: "/", Folder: record.Folder}.line()))
	if n := len(record.line()); n != startupIdentityLimit {
		t.Fatalf("the record is %d bytes", n)
	}
	if _, err := writeIdentity(private, record, nil); err != nil {
		t.Errorf("a record at the bound was refused: %v", err)
	}
	if _, _, found, err := readIdentity(private); !found || err != nil {
		t.Errorf("a record at the bound was not read back: %v", err)
	}
	if err := private.Remove(startupIdentityName); err != nil {
		t.Fatal(err)
	}
	record.Path += "p"
	if _, err := writeIdentity(private, record, nil); !errors.Is(err, errIdentityTooLong) {
		t.Errorf("a record past the bound answered %v", err)
	}
	if _, err := private.Lstat(startupIdentityName); !os.IsNotExist(err) {
		t.Errorf("a record past the bound was written: %v", err)
	}
}

// **With the runtime: a rotation answered on a trail replaced before Desk
// finished keeps the key the replacement needs** (issue #311, the auditor's
// scenario, with the published runtime; skipped without one). A signed desk,
// under a configuration folder whose path holds a space, a tab and U+2028,
// makes a deciding run; its key is rotated, and after the runtime answers,
// the audit folder is moved aside and a deciding run begins a replacement
// trail, signed with the first key. Desk answers the conflict, in words with
// no path, and keeps the first key as the desk's, the next key beside it and
// the journal; the runtime's own check of the replacement trail passes with
// the first key and not with the next; the decision record and the next
// start say the rotation did not finish, and change nothing.
func TestARotationAnsweredOnAReplacedTrailWithTheRuntime(t *testing.T) {
	bin := requireBinary(t)
	t.Setenv("JPACK_CONFIG", "")
	t.Setenv("JPACK_SIGNING_KEY", "")
	config := filepath.Join(t.TempDir(), "Top SECRET\tconfig TAIL")
	s, ts := startDesk(t, Config{ProjectDir: t.TempDir(), JpackBin: bin, Token: testToken, DeskConfigDir: config, Logger: log.New(io.Discard, "", 0)})
	t.Cleanup(func() { s.Close() })
	t.Cleanup(ts.Close)
	row := createGatedDesk(t, ts)
	if !row.Signed {
		t.Skip("the runtime does not read configVersion 6")
	}
	signing := signingFolderOf(s)
	seed := filepath.Join(signing, row.ID+seedSuffix)
	publicOf := func(t *testing.T, path string) deskPublicKey {
		t.Helper()
		var key deskPublicKey
		if err := json.Unmarshal(jpackIn(t, bin, row.Folder, "audit", "key", "public", path, "--format", "json"), &key); err != nil {
			t.Fatal(err)
		}
		return key
	}
	k1 := publicOf(t, seed)
	written := readFile(t, filepath.Join(row.Folder, "jpack.json"))
	writeProject(t, row.Folder, map[string]string{"packs/a.json": reviewPack, "jpack.json": strings.Replace(written, `"packs":{}`, `"packs":{"alpha":{"path":"packs/a.json"}}`, 1)})
	jpackIn(t, bin, row.Folder, "packs", "lock", "--config", "jpack.json", "--format", "json")
	facts := filepath.Join(t.TempDir(), "facts.json")
	if err := os.WriteFile(facts, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	evaluate := func() ([]byte, error) {
		cmd := exec.Command(bin, "experimental", "evaluate", "--config", "jpack.json", "--pack-id", "alpha", "--facts", facts, "--format", "json")
		cmd.Dir = row.Folder
		cmd.Env = append(os.Environ(), "JPACK_CONFIG=", "JPACK_SIGNING_KEY=")
		return cmd.Output()
	}
	if out, err := evaluate(); err != nil {
		t.Fatalf("the first deciding run: %v %s", err, out)
	}
	token := requireRotationWithTheRuntime(t, ts, row.ID)

	audit := filepath.Join(row.Folder, ".desk-private", "audit")
	var replaced error
	testHookKeyBetween = func(at string) {
		if at != "rotation: line written" {
			return
		}
		if replaced = os.Rename(audit, audit+".old"); replaced == nil {
			if replaced = os.Mkdir(audit, 0o700); replaced == nil {
				var out []byte
				if out, replaced = evaluate(); replaced != nil {
					replaced = fmt.Errorf("%w: %s", replaced, out)
				}
			}
		}
	}
	t.Cleanup(func() { testHookKeyBetween = nil })
	status, data := reviewCall(t, ts, "POST", "/api/audit/key/rotate", row.ID, map[string]string{"token": token}, bearer)
	testHookKeyBetween = nil
	if replaced != nil {
		t.Fatalf("the replacement trail was not begun: %v", replaced)
	}
	if want := fmt.Sprintf(rotationConflictWords, rotationTrailGone); status != http.StatusConflict || refusalOf(data) != want {
		t.Errorf("the rotation answered %d %s, want 409 %q", status, data, want)
	}
	if strings.Contains(string(data), config) || strings.Contains(string(data), "SECRET") {
		t.Errorf("the answer names a path: %s", data)
	}
	if got := publicOf(t, seed); got != k1 {
		t.Errorf("the desk's key is %+v, want the first key %+v", got, k1)
	}
	k2 := publicOf(t, filepath.Join(signing, row.ID+nextSeedSuffix))
	if names := namesIn(t, signing); !slices.Equal(names, []string{row.ID + keysSuffix, row.ID + nextSeedSuffix, row.ID + rotatingSuffix, row.ID + seedSuffix, "runner"}) &&
		!slices.Equal(names, []string{row.ID + keysSuffix, row.ID + nextSeedSuffix, row.ID + rotatingSuffix, row.ID + seedSuffix}) {
		t.Errorf("the signing folder holds %q", names)
	}
	verifyWith := func(key deskPublicKey) []byte {
		held := filepath.Join(t.TempDir(), "key.pub")
		if err := os.WriteFile(held, []byte(key.PublicKey+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(bin, "audit", "verify", "--config", "jpack.json", "--public-key", held, "--format", "json")
		cmd.Dir = row.Folder
		cmd.Env = append(os.Environ(), "JPACK_CONFIG=", "JPACK_SIGNING_KEY=")
		out, _ := cmd.Output()
		return out
	}
	if out := verifyWith(k1); !bytes.Contains(out, []byte(`"status":"valid"`)) {
		t.Errorf("the replacement trail does not pass the runtime's check with the first key: %s", out)
	}
	if out := verifyWith(k2); bytes.Contains(out, []byte(`"status":"valid"`)) {
		t.Errorf("the replacement trail passes the runtime's check with the next key: %s", out)
	}
	answer, panel := panelOn(t, ts, row.ID)
	if answer.Rotation.State != rotationUnfinished || !strings.Contains(answer.Rotation.Reason, rotationTrailGone) || strings.Contains(panel, "SECRET") {
		t.Errorf("the panel says %+v", answer.Rotation)
	}
	ts.Close()
	again, logged := restartedServer(t, s)
	_ = again
	if got := publicOf(t, seed); got != k1 || !strings.Contains(logged.String(), rotationTrailGone) {
		t.Errorf("the next start left the key %+v: %s", got, logged)
	}
}
