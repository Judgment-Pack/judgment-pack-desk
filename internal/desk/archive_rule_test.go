package desk

// The archive rule's invariant, and the third line audit's probes kept as
// regression tests (issues #319 to #325). Each probe once ended with a seed
// deleted, overwritten, or kept without a word; each ends now with every key
// present, live or archived, and a sentence.

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
)

/* The invariant ------------------------------------------------------------- */

// keyWatch is every seed, next seed and list of public keys a signing folder
// holds, at its live names, in Runner's folder or in an archive, at every
// moment a test hook runs, by device and inode: what the archive rule says
// no path of Desk's removes.
type keyWatch struct {
	dir  string
	mu   sync.Mutex
	seen map[string]string
}

// watchKeys watches dir from now on, at every test hook, before then, where
// then is not nil.
func watchKeys(t *testing.T, dir string, then func(at string)) *keyWatch {
	t.Helper()
	w := &keyWatch{dir: dir, seen: map[string]string{}}
	w.look()
	testHookKeyBetween = func(at string) {
		w.look()
		if then != nil {
			then(at)
		}
	}
	t.Cleanup(func() { testHookKeyBetween = nil })
	return w
}

// isKeyFile is whether name is a seed, a next seed or a list of public keys,
// at a live name or archived.
func isKeyFile(name string) bool {
	if strings.HasPrefix(name, keysStagingPrefix) {
		// A list of public keys while it is staged (review round 1 of #327,
		// finding 3).
		return true
	}
	if parts := archiveFileForm.FindStringSubmatch(name); parts != nil {
		return parts[4] == "seed" || parts[4] == "next.seed" || parts[4] == "keys.jsonl"
	}
	kind, _, ok := archiveKindOf(name)
	return ok && (kind == "seed" || kind == "next.seed" || kind == "keys.jsonl")
}

// now is every key file under the folder watched, by device and inode.
func (w *keyWatch) now() map[string]string {
	held := map[string]string{}
	_ = filepath.WalkDir(w.dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !isKeyFile(entry.Name()) {
			return nil
		}
		if info, err := os.Lstat(path); err == nil {
			held[identityKey(info)] = path
		}
		return nil
	})
	return held
}

func (w *keyWatch) look() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for inode, path := range w.now() {
		if _, ok := w.seen[inode]; !ok {
			w.seen[inode] = path
		}
	}
}

// kept fails the test where a key file seen at any moment is not held now,
// live or archived, and answers how many were seen.
func (w *keyWatch) kept(t *testing.T) int {
	t.Helper()
	w.look()
	now := w.now()
	w.mu.Lock()
	defer w.mu.Unlock()
	for inode, path := range w.seen {
		if _, ok := now[inode]; !ok {
			t.Errorf("a key Desk held, %s, is gone", strings.TrimPrefix(path, w.dir))
		}
	}
	return len(w.seen)
}

// archivedFiles is every file archived under dir, in its archive and in
// Runner's, each held to its journal line (`archivedIn`).
func archivedFiles(t *testing.T, dir string) int {
	t.Helper()
	count := 0
	for _, folder := range []string{dir, filepath.Join(dir, runnerSigningDirName)} {
		for _, id := range namesIn(t, filepath.Join(folder, archiveDirName)) {
			count += len(archivedIn(t, folder, id))
		}
	}
	return count
}

// **No path of Desk's removes a key on its own** (the maintainer's decision of
// 2026-10-08; issues #319 to #323). Every path that once removed a seed, a
// next seed or a list of public keys is walked, each through Desk's own
// code: a creation the runtime stopped, a creation stopped once its key was
// made, an upgrade taken back, the start's sweep, a Runner key's unfinished
// creation, a rotation whose next key the runtime failed to make, one it
// refused, a start's recovery of one never written, a rotation finished, one
// whose trail moved after its last check, a stopped promotion finished, and
// a journal no project open holds. At every test hook, every key file is
// noted by its device and inode; at the end, each one noted is still held,
// at a live name or in an archive, every archived file has its journal line
// and sentence, and the log says each move.
func TestNoPathRemovesAKeyOnItsOwn(t *testing.T) {
	const desk = "a1d00000000000000000000000000001"
	type outcome struct {
		dir  string
		logs func() string
	}
	for _, tc := range []struct {
		name     string
		archives bool
		run      func(t *testing.T) (*keyWatch, outcome)
	}{
		{"a desk's creation the runtime stopped, a seed half written", true, func(t *testing.T) (*keyWatch, outcome) {
			bin := filepath.Join(t.TempDir(), "jpack")
			calls := writeStandInRuntime(t, bin, reading(withAuditVersions), lockingAs(wantGatedConfig))
			s, ts, logged := gatesServer(t, bin)
			generatingAs(t, calls, "  printf 'part of a seed' > \"$4\"\n  printf '%s\\n' "+shellQuote(`{"outputVersion":"2","command":"audit key generate","status":"error","diagnostics":[{"code":"JPS-AUDIT-KEY-WRITE","message":"The seed could not be written."}]}`)+"\n  exit 4")
			signsDesks(t, calls, s.configDir, desk)
			w := watchKeys(t, signingFolderOf(s), nil)
			if status, data := deskCall(t, ts, "POST", "/api/desks", "", `{"name":"Stopped"}`, true); status == http.StatusCreated {
				t.Fatalf("a desk was made: %s", data)
			}
			return w, outcome{signingFolderOf(s), logged.String}
		}},
		{"a desk's creation stopped once its key was made", true, func(t *testing.T) (*keyWatch, outcome) {
			calls, s, ts, logged := signingStandIn(t)
			signsDesks(t, calls, s.configDir, desk)
			private := filepath.Join(s.configDir, "desks", desk, ".desk-private")
			t.Cleanup(func() { os.Chmod(private, 0o700) })
			w := watchKeys(t, signingFolderOf(s), func(at string) {
				if at == "before publish" {
					os.Chmod(private, 0o500)
				}
			})
			if status, data := deskCall(t, ts, "POST", "/api/desks", "", `{"name":"Unwritten"}`, true); status == http.StatusCreated {
				t.Fatalf("a desk was made: %s", data)
			}
			return w, outcome{signingFolderOf(s), logged.String}
		}},
		{"an upgrade taken back", true, func(t *testing.T) (*keyWatch, outcome) {
			u := newSigningUpgrade(t, nil)
			answer := u.offer(t, true)
			u.rig.locks(t, upgradeLock(t, u.project, u.signed(), bothPacks))
			os.Remove(u.rig.lock)
			if err := os.WriteFile(u.rig.bin, []byte(strings.Replace(readFile(t, u.rig.bin), "'packs lock')\n", "'packs lock')\n  exit 1\n", 1)), 0o755); err != nil {
				t.Fatal(err)
			}
			w := watchKeys(t, filepath.Dir(u.seed), nil)
			if status, data := u.confirm(t, answer.Token, true); status == http.StatusOK || !strings.Contains(refusalOf(data), "moved to Desk's archive of keys") {
				t.Fatalf("the upgrade answered %d %s", status, data)
			}
			return w, outcome{filepath.Dir(u.seed), func() string { return "" }}
		}},
		{"the start's sweep of a creation never published", true, func(t *testing.T) (*keyWatch, outcome) {
			calls, s, ts, _ := signingStandIn(t)
			stoppedCreation(t, calls, s, ts, desk, "before publish")
			w := watchKeys(t, signingFolderOf(s), nil)
			_, logged := restartedServer(t, s)
			return w, outcome{signingFolderOf(s), logged.String}
		}},
		{"a Runner key's unfinished creation", true, func(t *testing.T) (*keyWatch, outcome) {
			bin := filepath.Join(t.TempDir(), "jpack")
			writeStandInRuntime(t, bin, reading(allConfigVersions), lockingAs(wantGatedConfig))
			s, _, logged := gatesServer(t, bin)
			leaveUnfinished(t, s, desk)
			w := watchKeys(t, signingFolderOf(s), nil)
			if s.newRunnerKey(desk).decide() == "" {
				t.Fatal("no key was made again")
			}
			return w, outcome{signingFolderOf(s), logged.String}
		}},
		{"a rotation whose next key the runtime failed to make", true, func(t *testing.T) (*keyWatch, outcome) {
			r := newRotationRig(t, desk, "")
			r.writeTrail(t, 1, recordLine(standInKeyID, 1))
			token := r.token(t)
			generatingAs(t, r.rig.calls, "  printf 'part of a seed' > \"$4\"\n  printf '%s\\n' "+shellQuote(`{"outputVersion":"2","command":"audit key generate","status":"error","diagnostics":[{"code":"JPS-AUDIT-KEY-EXISTS","message":"Something is already there."}]}`)+"\n  exit 4")
			w := watchKeys(t, r.signing, nil)
			r.rotate(t, token)
			return w, outcome{r.signing, r.logged.String}
		}},
		{"a rotation the runtime refused", true, func(t *testing.T) (*keyWatch, outcome) {
			r := newRotationRig(t, desk, "")
			r.writeTrail(t, 1, recordLine(standInKeyID, 1))
			token := r.token(t)
			rotatingAs(t, r.rig.calls, refusingWith(rotateNotInForce, 1))
			w := watchKeys(t, r.signing, nil)
			r.rotate(t, token)
			return w, outcome{r.signing, r.logged.String}
		}},
		{"a start's recovery of a rotation the sidecar does not record", true, func(t *testing.T) (*keyWatch, outcome) {
			r := newRotationRig(t, desk, "")
			r.writeTrail(t, 1, recordLine(standInKeyID, 1))
			r.abandonRotation(t, "rotation: rotate journalled")
			w := watchKeys(t, r.signing, nil)
			_, _, logged := r.restart(t)
			return w, outcome{r.signing, logged.String}
		}},
		{"a rotation finished", true, func(t *testing.T) (*keyWatch, outcome) {
			r := newRotationRig(t, desk, "")
			r.writeTrail(t, 1, recordLine(standInKeyID, 1))
			token := r.token(t)
			w := watchKeys(t, r.signing, nil)
			if status, data := r.rotate(t, token); status != http.StatusOK {
				t.Fatalf("the rotation answered %d %s", status, data)
			}
			return w, outcome{r.signing, r.logged.String}
		}},
		{"a rotation whose trail moved after its last check", true, func(t *testing.T) (*keyWatch, outcome) {
			r := newRotationRig(t, desk, "")
			r.writeTrail(t, 1, recordLine(standInKeyID, 1))
			token := r.token(t)
			w := watchKeys(t, r.signing, func(at string) {
				if at == "rotation: after the last check" {
					r.replacedTrail(t)
				}
			})
			if status, data := r.rotate(t, token); status != http.StatusConflict || refusalOf(data) != promotedConflictAnswer {
				t.Errorf("the rotation answered %d %s", status, data)
			}
			return w, outcome{r.signing, r.logged.String}
		}},
		{"a start's recovery of a promotion a stop cut short", true, func(t *testing.T) (*keyWatch, outcome) {
			r := newRotationRig(t, desk, "")
			r.writeTrail(t, 1, recordLine(standInKeyID, 1))
			r.abandonRotation(t, "rotation: previous archived")
			before := r.logged.String()
			w := watchKeys(t, r.signing, nil)
			_, _, logged := r.restart(t)
			if got := r.describe(t); got != ".keys.jsonl,.seed seed=2 next=absent keys=2 rotations=1 archived=seed rotation-promoted" {
				t.Errorf("the start left %s", got)
			}
			return w, outcome{r.signing, func() string { return before + logged.String() }}
		}},
		{"a list of public keys that could not be published", true, func(t *testing.T) (*keyWatch, outcome) {
			s, logged := bareServer(t, filepath.Join(t.TempDir(), "project"), filepath.Join(t.TempDir(), "config"), desk)
			bareKeys(t, s)
			dir, err := s.assistant.openSigning(false)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(dir.Close)
			w := watchKeys(t, dir.path, nil)
			if _, err := dir.writeNewKeys(desk+keysSuffix, []deskPublicKey{key1}); err == nil {
				t.Fatal("a list was written over another")
			}
			return w, outcome{dir.path, logged.String}
		}},
		{"a rotation's journal no project open here holds", true, func(t *testing.T) (*keyWatch, outcome) {
			s, logged := bareServer(t, filepath.Join(t.TempDir(), "project"), filepath.Join(t.TempDir(), "config"), "")
			orphan := strings.Repeat("f", 64)
			s.setStartup(identityKept, orphan, "", "")
			bareKeys(t, s)
			signing := signingFolderOf(s)
			writeBare(t, filepath.Join(signing, orphan+nextSeedSuffix), secondSeed+"\n")
			writeBare(t, filepath.Join(signing, orphan+rotatingSuffix), string(rotationJournal{Version: "1", Phase: journalRotate, Trail: fixtureTrail, Next: secondPublicKey}.line()))
			s.setStartup(identityNone, "", "", "")
			w := watchKeys(t, signing, nil)
			s.resolveStartupIdentity()
			s.recoverRotations()
			return w, outcome{signing, logged.String}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, got := tc.run(t)
			if seen := w.kept(t); seen == 0 {
				t.Fatal("no key was ever seen: the path was not walked")
			}
			archived := archivedFiles(t, got.dir)
			if tc.archives && archived == 0 {
				t.Errorf("nothing was archived")
			}
			if logs := got.logs(); logs != "" && strings.Count(logs, "was moved to Desk's archive of keys") < archived {
				t.Errorf("%d files were archived, and the log says %d moves: %s", archived, strings.Count(logs, "was moved to Desk's archive of keys"), logs)
			}
		})
	}
}

// custodyFiles are the files that keep keys, where every removal and every
// rename is held to removalsAllowed.
var custodyFiles = []string{"archive.go", "custody.go", "desks.go", "rotation.go", "runner_key.go", "signing.go", "signing_lock.go", "startup_identity.go", "startup_key.go", "upgrade.go"}

// removalsAllowed is every Remove and Rename in those files, by file,
// function and call, and why it is not a key removed on Desk's own.
var removalsAllowed = map[string]string{
	"archive.go archiveRename Rename":                        "the archive move itself, never over anything",
	"archive.go (*signingDir).removeArchivedFile Remove":     "the owner's Remove, confirmed with a token bound to the entry's digest",
	"rotation.go (*signingDir).placeNext Rename":             "the next seed into the current seed's name, which must hold nothing",
	"rotation.go (*signingDir).writeJournal Remove":          "a rotation's journal it could not write whole",
	"rotation.go (*signingDir).rewriteJournal Remove":        "a staging file",
	"rotation.go (*signingDir).rewriteJournal Rename":        "a staged journal over the marker it replaces",
	"signing.go (*signingDir).removeMade Remove":             "a marker; it refuses a seed, a next seed and a list",
	"signing.go (*signingDir).rewriteMarker Remove":          "a staging file",
	"signing.go (*signingDir).rewriteMarker Rename":          "a staged creation marker over the marker it replaces",
	"signing.go (*signingDir).writeMarkerHolding Remove":     "a creation marker it could not write whole",
	"signing.go (*signingDir).writeNewKeys Remove":           "the staging name of a list, only once the list is linked to its own name and both name one file; unpublished, it is archived",
	"signing.go (*signingDir).publicKeyFiles Remove":         "the public-key files written for one audit verify",
	"custody.go (*assistantStore).writeConfigNamed Remove":   "desk.json's staging file",
	"custody.go (*assistantStore).writeConfigNamed Rename":   "desk.json, staged",
	"custody.go (*assistantStore).storeKeyNamed Remove":      "the assistant credential's staging file, not a signing key",
	"custody.go (*assistantStore).storeKeyNamed Rename":      "the assistant credential, staged, not a signing key",
	"custody.go (*assistantStore).removeKeyNamed Remove":     "the assistant credential, on the owner's word, not a signing key",
	"desks.go publishDeskManifest Remove":                    "the manifest's staging file",
	"desks.go publishDeskManifest Rename":                    "the manifest, staged",
	"desks.go unmakeDeskFolder Remove":                       "what a failed creation made in the desk's own folder, never in the signing folder",
	"startup_identity.go writeIdentity Remove":               "the identity file's staging file",
	"startup_identity.go writeIdentity Rename":               "the identity file, staged",
	"startup_identity.go (*Server).finishStartupMove Rename": "stamping settings, never over anything",
	"upgrade.go (*upgradeUndo).run Remove":                   "the identity file and folders the upgrade made, once nothing is kept under its name",
}

// keyArgument is an argument that names a key, a next key or a list.
var keyArgument = regexp.MustCompile(`(?i)seed|keysName|keysSuffix|nextName|\.keys`)

// **No code removes a key outside the owner's word** (the archive rule). Every
// Remove and Rename in the files that keep keys is one of removalsAllowed,
// each with its reason, and no other file of the package removes or renames
// anything named as a key: a new removal is a failing test until it is
// argued for here. Every allowed one is still there, so the list says what
// the code does.
func TestNoCodeRemovesAKeyOutsideTheOwnersWord(t *testing.T) {
	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	found := map[string]bool{}
	for _, source := range sources {
		if strings.HasSuffix(source, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, source, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		custody := slices.Contains(custodyFiles, source)
		// Each function, and each function a package variable holds (a seam
		// a test may stand in for), by its name.
		bodies := map[string]ast.Node{}
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				name := decl.Name.Name
				if decl.Recv != nil {
					var recv bytes.Buffer
					printer.Fprint(&recv, fset, decl.Recv.List[0].Type)
					name = "(" + recv.String() + ")." + name
				}
				bodies[name] = decl
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					value, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, held := range value.Values {
						if _, ok := held.(*ast.FuncLit); ok && i < len(value.Names) {
							bodies[value.Names[i].Name] = held
						}
					}
				}
			}
		}
		for name, fn := range bodies {
			ast.Inspect(fn, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Remove" && sel.Sel.Name != "RemoveAll" && sel.Sel.Name != "Rename" {
					return true
				}
				key := source + " " + name + " " + sel.Sel.Name
				var args bytes.Buffer
				for _, arg := range call.Args {
					printer.Fprint(&args, fset, arg)
					args.WriteByte(' ')
				}
				if _, allowed := removalsAllowed[key]; !allowed && (custody || keyArgument.MatchString(args.String())) {
					t.Errorf("%s: %s(%s) removes or renames what Desk may keep as a key; archive it, or say here why it is not a key", fset.Position(call.Pos()), sel.Sel.Name, strings.TrimSpace(args.String()))
				}
				found[key] = true
				return true
			})
		}
	}
	for key := range removalsAllowed {
		if !found[key] {
			t.Errorf("%s is allowed and no longer there: take it off the list", key)
		}
	}
}

// **A marker is all removeMade removes** (the archive rule): handed a seed, a
// next seed or a list of public keys, it refuses, and the file stays.
func TestRemoveMadeRefusesAKey(t *testing.T) {
	s, _ := bareServer(t, filepath.Join(t.TempDir(), "project"), filepath.Join(t.TempDir(), "config"), desk2)
	bareKeys(t, s)
	dir, err := s.assistant.openSigning(false)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	writeBare(t, filepath.Join(dir.path, desk2+nextSeedSuffix), secondSeed+"\n")
	for _, name := range []string{desk2 + seedSuffix, desk2 + nextSeedSuffix, desk2 + keysSuffix} {
		info, err := dir.root.Lstat(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := dir.removeMade(name, info); err == nil {
			t.Errorf("removeMade removed %s", name)
		}
		if _, err := os.Lstat(filepath.Join(dir.path, name)); err != nil {
			t.Errorf("%s is gone: %v", name, err)
		}
	}
}

// desk2 is a made desk's id for the tests below.
const desk2 = "a1d00000000000000000000000000002"

/* The third line audit's probes, kept (issues #319 to #325) ---------------- */

// madeLocked is a rotation's second phase over reading, under the desk's key
// lock and the signing folder's, as rotateKey holds them: the auditors' rig.
func madeLocked(t *testing.T, s *Server, project heldDir, reading *keyReading) (*rotationAnswer, *lockFailure) {
	t.Helper()
	s.keyMu.Lock()
	defer s.keyMu.Unlock()
	unlock, err := lockSigning(reading.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	return s.makeRotation(context.Background(), project, reading)
}

// readingOf is the keys s keeps, read as a rotation reads them, through its
// signing folder.
func readingOf(t *testing.T, s *Server) (heldDir, auditKeys, *keyReading) {
	t.Helper()
	dir, err := s.assistant.openSigning(false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dir.Close)
	project, why := s.auditRuntime()
	if why != "" {
		t.Fatal(why)
	}
	keys, reading := s.keysIn(context.Background(), project, dir, nil)
	return project, keys, reading
}

// signedConfig is a configuration naming seed as its signing key.
func signedConfig(seed string) string {
	return `{"configVersion":"6","packs":{},"audit":{"dir":".desk-private/audit","signingKey":` + jsonString(seed) + `}}`
}

// **A legacy identity at a reused pathname takes no custody** (issue #319,
// the third line audit's finding 1, cell one; the auditor's probe). The
// project is copied before its signing configuration is published, the
// original publishes it and leaves its creation marker, moves away, and the
// copy is put at its pathname. The copy's identity records no folder: it is
// unresolved, the sweep archives nothing, and the seed the moved original
// names stays at its name, with a sentence for each.
func TestALegacyIdentityAtAReusedPathTakesNoCustody(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	base := t.TempDir()
	config, original, copied := filepath.Join(base, "config"), filepath.Join(base, "original"), filepath.Join(base, "copy")
	id := strings.Repeat("a", 64)
	s, _ := bareServer(t, original, config, "")
	s.setStartup(identityKept, id, "", "")
	bareKeys(t, s)
	writeBare(t, filepath.Join(original, ".desk-private", "project.json"), string(identityRecord{ID: id, Path: original}.line()))
	unsigned := readFile(t, filepath.Join(original, "jpack.json"))
	copyPrivateTree(t, original, copied)
	signing := filepath.Join(config, "secrets", "signing")
	writeBare(t, filepath.Join(signing, id+creatingSuffix), string(startupCreation{ID: id, Project: original, Config: sha256Digest([]byte(unsigned))}.line()))
	writeBare(t, filepath.Join(original, "jpack.json"), signedConfig(filepath.Join(signing, id+seedSuffix)))
	if os.Rename(original, original+"-moved") != nil || os.Rename(copied, original) != nil {
		t.Fatal("could not move the original and put the copy at its path")
	}
	b, logs := bareServer(t, original, config, "")
	b.resolveStartupIdentity()
	b.sweepUnfinishedKeys()
	if b.startupBound() || !b.startupUnresolved() {
		t.Errorf("the copy's legacy identity is bound: %s", logs)
	}
	if got := heldUnder(t, config, id); got != ".creating,.keys.jsonl,.seed seed=1 next=absent keys=1" {
		t.Errorf("the start left %s: %s", got, logs)
	}
	if !strings.Contains(logs.String(), "records no folder") || !strings.Contains(logs.String(), unresolvedWords) {
		t.Errorf("the start did not say why: %s", logs)
	}
	if offer := offerOf(t, b); offer.Kind != "unbound" {
		t.Errorf("the decision record asks %+v", offer)
	}
}

// **A copy with no identity file takes no custody through its configuration**
// (issue #319, cell two; the auditor's probe). The copy's jpack.json names the
// original's seed; its start records that name with no folder, unresolved,
// and the copy rotates nothing: the original's seed is untouched, and its
// trail has no hand-over it did not make.
func TestAMissingIdentityTakesNoCustodyThroughACopiedConfiguration(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	t.Setenv("JPACK_SIGNING_KEY", "")
	base := t.TempDir()
	config, original, copyPath := filepath.Join(base, "config"), filepath.Join(base, "original"), filepath.Join(base, "copy")
	id := strings.Repeat("d", 64)
	s, _ := bareServer(t, original, config, "")
	s.setStartup(identityKept, id, "", "")
	bareKeys(t, s)
	signing := filepath.Join(config, "secrets", "signing")
	writeBare(t, filepath.Join(original, ".desk-private", "project.json"), string(identityRecord{ID: id, Path: original, Folder: s.folderKey()}.line()))
	writeBare(t, filepath.Join(original, "jpack.json"), signedConfig(filepath.Join(signing, id+seedSuffix)))
	copyPrivateTree(t, original, copyPath)
	if err := os.Remove(filepath.Join(copyPath, ".desk-private", "project.json")); err != nil {
		t.Fatal(err)
	}
	b, logs := bareServer(t, copyPath, config, "")
	b.resolveStartupIdentity()
	if b.startupName() != id || !b.startupUnresolved() {
		t.Fatalf("the copy is named %s, unresolved %v: %s", b.startupName(), b.startupUnresolved(), logs)
	}
	project, keys, reading := readingOf(t, b)
	if offer := b.rotationOffer(context.Background(), project, keys, reading); offer.State != rotationUnavailable || !strings.Contains(offer.Reason, unresolvedWords) {
		t.Errorf("the copy offers %+v", offer)
	}
	b.sweepUnfinishedKeys()
	b.recoverRotations()
	if got := heldUnder(t, config, id); got != ".keys.jsonl,.seed seed=1 next=absent keys=1" || strings.Contains(readFile(t, filepath.Join(original, ".desk-private", "audit", "signatures.jsonl")), "key-rotation") {
		t.Errorf("the original's custody is %s", got)
	}
}

// **A made desk's copy that rotates keeps the shared seed** (issue #319, cell
// three; the auditor's probe). The original moved out of the desks folder,
// and its copy, opened directly, is taken as the desk moved: a residual the
// archive rule leaves (a vacant registry slot proves no exclusive custody).
// Its rotation no longer writes over the seed the original names: the
// original's key is in the archive, with the sentence that says why.
func TestAMadeDesksCopyThatRotatesKeepsTheSharedSeed(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	t.Setenv("JPACK_SIGNING_KEY", "")
	base := t.TempDir()
	config := filepath.Join(base, "config")
	const id = "e3100000000000000000000000000094"
	original, copyPath := filepath.Join(config, "desks", id), filepath.Join(base, "copy")
	s, _ := bareServer(t, original, config, id)
	bareKeys(t, s)
	writeBare(t, filepath.Join(original, deskManifest), `{"id":"`+id+`","name":"live"}`)
	copyPrivateTree(t, original, copyPath)
	if err := os.Rename(original, filepath.Join(base, "moved")); err != nil {
		t.Fatal(err)
	}
	b, logs := bareServer(t, copyPath, config, id)
	project, _, reading := readingOf(t, b)
	if reading == nil {
		t.Fatal("no keys")
	}
	if _, failure := madeLocked(t, b, project, reading); failure != nil {
		t.Fatalf("%+v", failure)
	}
	signing := filepath.Join(config, "secrets", "signing")
	if seedIs(filepath.Join(signing, id+seedSuffix)) != "2" || seedIs(archivedFile(t, signing, id, "seed")) != "1" {
		t.Errorf("the original's seed was not kept: %s", heldUnder(t, config, id))
	}
	if got := archivedIn(t, signing, id); !slices.Equal(got, kindsArchived(archivePromoted, "seed")) || !strings.Contains(logs.String(), "was moved to Desk's archive of keys") {
		t.Errorf("the archive holds %q: %s", got, logs)
	}
}

// **Identity and seed reference lost: the rotation is archived, and said**
// (issue #320, the third line audit's finding 2; the auditor's probe). The
// project starts with no identity file and a configuration that names no
// seed, while custody holds its former identity's seed, next seed and
// rotation journal on this project's trail. The start names it anew, and the
// former rotation's next seed and journal go to the archive, each with its
// sentence; the former key in force stays at its name. A journal of another
// trail is left, and said.
func TestALostIdentitysRotationIsArchivedAndSaid(t *testing.T) {
	for _, tc := range []struct {
		name     string
		trail    string
		archived []string
		says     string
	}{
		{"on this project's trail", fixtureTrail, kindsArchived(archiveRotationOrphaned, "next.seed", "rotating"), "was moved to Desk's archive of keys"},
		{"on another trail", otherTrail, nil, "its journal does not name this project's trail"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := filepath.Join(t.TempDir(), "config")
			s, logs := bareServer(t, filepath.Join(t.TempDir(), "project"), config, "")
			id := strings.Repeat("f", 64)
			s.setStartup(identityKept, id, "", "")
			bareKeys(t, s)
			signing := filepath.Join(config, "secrets", "signing")
			writeBare(t, filepath.Join(signing, id+nextSeedSuffix), secondSeed+"\n")
			writeBare(t, filepath.Join(signing, id+rotatingSuffix), string(rotationJournal{Version: "1", Phase: journalRotate, Trail: tc.trail, Next: secondPublicKey}.line()))
			s.setStartup(identityNone, "", "", "")
			s.resolveStartupIdentity()
			s.recoverRotations()
			if s.startupName() == id {
				t.Fatal("the project took its former name")
			}
			if seedIs(filepath.Join(signing, id+seedSuffix)) != "1" {
				t.Errorf("the former key in force is not kept: %s", heldUnder(t, config, id))
			}
			if got := archivedIn(t, signing, id); !slices.Equal(got, tc.archived) {
				t.Errorf("the archive holds %q, want %q", got, tc.archived)
			}
			if tc.archived != nil && seedIs(archivedFile(t, signing, id, "next.seed")) != "2" {
				t.Error("the former next key is not in the archive")
			}
			if !strings.Contains(logs.String(), tc.says) {
				t.Errorf("the start did not say %q: %s", tc.says, logs)
			}
			// Left at its name, it is said on the decision record (review
			// round 1 of #327, finding 5).
			if tc.archived == nil {
				if listing := s.archiveListing(); listing == nil || len(listing.Entries) != 1 || !listing.Entries[0].Unresolved {
					t.Errorf("the decision record lists %+v", listing)
				}
			}
		})
	}
}

// **An exact rollback of the sidecar archives the next key, never removes it**
// (issue #321, the third line audit's finding 3; the auditor's probe). The
// rotation commits, and the same sidecar file is put back to exactly the
// bytes the journal recorded: every check passes, so Desk cannot tell it from
// one the runtime never wrote, and the next key goes to the archive with the
// sentence that says so, beside the key in force.
func TestAnExactSidecarRollbackArchivesTheNextKey(t *testing.T) {
	config := filepath.Join(t.TempDir(), "config")
	const id = "e3100000000000000000000000000098"
	s, logs := bareServer(t, filepath.Join(t.TempDir(), "project"), config, id)
	bareKeys(t, s)
	sidecar := filepath.Join(s.projectDir, ".desk-private", "audit", "signatures.jsonl")
	before := readFile(t, sidecar)
	signing := filepath.Join(config, "secrets", "signing")
	writeBare(t, filepath.Join(signing, id+nextSeedSuffix), secondSeed+"\n")
	writeBare(t, filepath.Join(signing, id+rotatingSuffix), string(rotationJournal{Version: "1", Phase: journalRotate, Trail: fixtureTrail, Next: secondPublicKey, Sidecar: seenOf(t, sidecar)}.line()))
	appendBare(t, sidecar, rotationLine(1, standInKeyID, secondPublicKey))
	writeBare(t, sidecar, before)
	s.recoverRotations()
	if seedIs(filepath.Join(signing, id+seedSuffix)) != "1" || seedIs(archivedFile(t, signing, id, "next.seed")) != "2" {
		t.Errorf("both keys are not kept: %s", heldUnder(t, config, id))
	}
	if got := archivedIn(t, signing, id); !slices.Equal(got, kindsArchived(archiveRotationNotWritten, "next.seed", "rotating")) {
		t.Errorf("the archive holds %q", got)
	}
	if !strings.Contains(logs.String(), "Desk cannot rule out that the runtime wrote the rotation") {
		t.Errorf("the log does not say what Desk could not rule out: %s", logs)
	}
}

// **A published desk whose manifest is gone keeps its key** (issue #322, the
// third line audit's finding 4; the auditor's probe). The marker recorded
// that the manifest was about to be written; the manifest is then removed
// from the desk's folder, still in the desks folder, while a copy elsewhere
// is published. An unrelated start archives nothing, and says why.
func TestAPublishedDeskWhoseManifestIsGoneKeepsItsKey(t *testing.T) {
	base := t.TempDir()
	config := filepath.Join(base, "config")
	const id = "e3100000000000000000000000000099"
	folder := filepath.Join(config, "desks", id)
	s, _ := bareServer(t, folder, config, id)
	bareKeys(t, s)
	manifest := `{"id":"` + id + `","name":"live"}`
	writeBare(t, filepath.Join(folder, deskManifest), manifest)
	signing := filepath.Join(config, "secrets", "signing")
	writeBare(t, filepath.Join(signing, id+creatingSuffix), string(deskCreation{ID: id, Folder: folderOf(t, folder), Manifest: sha256Digest([]byte(manifest))}.line()))
	writeBare(t, filepath.Join(folder, "jpack.json"), signedConfig(filepath.Join(signing, id+seedSuffix)))
	copyPrivateTree(t, folder, filepath.Join(base, "live-copy"))
	if err := os.Remove(filepath.Join(folder, deskManifest)); err != nil {
		t.Fatal(err)
	}
	other, logs := bareServer(t, filepath.Join(base, "unrelated"), config, "")
	other.sweepUnfinishedKeys()
	if got := heldUnder(t, config, id); got != ".creating,.keys.jsonl,.seed seed=1 next=absent keys=1" {
		t.Errorf("an unrelated start left %s", got)
	}
	if archivedFiles(t, signing) != 0 || !strings.Contains(logs.String(), deskMovedWords) {
		t.Errorf("an unrelated start archived the key, or did not say why: %s", logs)
	}
}

// **The moment after the last check keeps both keys, and says so** (issue
// #323, the third line audit's finding 5; the auditor's probe, without its
// overlay: the hook is Desk's own). With the signing and key locks held, the
// trail and its sidecar are moved aside and a replacement begun with the
// first key, after the last check and before the moves. The previous key is
// in the archive, the next key in force, the rotation's journal archived
// beside them with the sentence that says the trail moved, and the answer
// never says the key was rotated.
func TestTheMomentAfterTheLastCheckKeepsBothKeys(t *testing.T) {
	config := filepath.Join(t.TempDir(), "config")
	const id = "e3100000000000000000000000000096"
	s, logs := bareServer(t, filepath.Join(t.TempDir(), "project"), config, id)
	bareKeys(t, s)
	project, _, reading := readingOf(t, s)
	audit := filepath.Join(s.projectDir, ".desk-private", "audit")
	fired := false
	testHookKeyBetween = func(at string) {
		if at != "rotation: after the last check" {
			return
		}
		fired = true
		for _, name := range []string{"evaluations.jsonl", "signatures.jsonl"} {
			if err := os.Rename(filepath.Join(audit, name), filepath.Join(audit, name+".old")); err != nil {
				t.Fatal(err)
			}
		}
		writeBare(t, filepath.Join(audit, "evaluations.jsonl"), "{}\n")
		writeBare(t, filepath.Join(audit, "signatures.jsonl"), onTrail(recordLine(standInKeyID, 1), otherTrail))
	}
	t.Cleanup(func() { testHookKeyBetween = nil })
	answer, failure := madeLocked(t, s, project, reading)
	testHookKeyBetween = nil
	if !fired {
		t.Fatal("the moment after the last check was never reached")
	}
	if answer != nil || failure == nil || failure.status != http.StatusConflict || failure.message != promotedConflictAnswer {
		t.Errorf("the rotation answered %+v %+v", answer, failure)
	}
	signing := filepath.Join(config, "secrets", "signing")
	if seedIs(filepath.Join(signing, id+seedSuffix)) != "2" || seedIs(archivedFile(t, signing, id, "seed")) != "1" {
		t.Errorf("both keys are not kept: %s", heldUnder(t, config, id))
	}
	if got := archivedIn(t, signing, id); !slices.Equal(got, []string{"seed " + archivePromoted, "rotating " + archiveRotationConflict}) {
		t.Errorf("the archive holds %q", got)
	}
	if !strings.Contains(logs.String(), "was moved aside or replaced while Desk named the next key") {
		t.Errorf("the log does not say the trail moved: %s", logs)
	}
}

// **A list of public keys that could not be published is kept** (review
// round 1 of #327, finding 3, the reviewer's scenario). The list is staged
// whole, and its link into place fails, here because its name holds a list
// already: the staged list is not removed but moved to Desk's archive of
// keys, with the bytes written and the sentence that says why; the list at
// its name is untouched, and no staging file is left.
func TestAListThatCouldNotBePublishedIsKept(t *testing.T) {
	s, logs := bareServer(t, filepath.Join(t.TempDir(), "project"), filepath.Join(t.TempDir(), "config"), desk2)
	bareKeys(t, s)
	dir, err := s.assistant.openSigning(false)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	before := readFile(t, filepath.Join(dir.path, desk2+keysSuffix))
	if _, err := dir.writeNewKeys(desk2+keysSuffix, []deskPublicKey{key1, {secondPublicKey, secondKeyID, 1, fixtureTrail}}); err == nil {
		t.Fatal("a list was written over another")
	}
	if got := readFile(t, filepath.Join(dir.path, desk2+keysSuffix)); got != before {
		t.Errorf("the list at its name is %q", got)
	}
	for _, name := range namesIn(t, dir.path) {
		if strings.HasPrefix(name, keysStagingPrefix) {
			t.Errorf("a staging file was left: %s", name)
		}
	}
	if got := archivedIn(t, dir.path, desk2); !slices.Equal(got, kindsArchived(archiveCreationStopped, "keys.jsonl")) {
		t.Fatalf("the archive holds %q", got)
	}
	if got := readFile(t, archivedFile(t, dir.path, desk2, "keys.jsonl")); got != string(key1.line())+wantKeyLineOn(secondPublicKey, secondKeyID, 1, fixtureTrail) {
		t.Errorf("the archived list is %q", got)
	}
	if !strings.Contains(logs.String(), "could not be put under its name") {
		t.Errorf("the log does not say why: %s", logs)
	}
}

// **Custody no start settles is said on the decision record** (review round
// 1 of #327, finding 5, the reviewer's scenario). The project's identity file
// is lost, and its trail moved aside, while custody holds its former
// identity's seed, next seed and rotation journal: the start names the
// project anew, and cannot match the journal to a trail it reads, so it keeps
// all three at their names. The decision record, a refusal of it among its
// answers, lists the journal as unresolved, with the sentence that says what
// Desk could not match, and offers no Remove for it.
func TestCustodyNoStartSettlesIsSaidOnTheDecisionRecord(t *testing.T) {
	config := filepath.Join(t.TempDir(), "config")
	s, logs := bareServer(t, filepath.Join(t.TempDir(), "project"), config, "")
	s.cfg.Token, s.sessions = "probe", &sessionStore{}
	id := strings.Repeat("f", 64)
	s.setStartup(identityKept, id, "", "")
	bareKeys(t, s)
	signing := filepath.Join(config, "secrets", "signing")
	writeBare(t, filepath.Join(signing, id+nextSeedSuffix), secondSeed+"\n")
	writeBare(t, filepath.Join(signing, id+rotatingSuffix), string(rotationJournal{Version: "1", Phase: journalRotate, Trail: fixtureTrail, Next: secondPublicKey}.line()))
	s.setStartup(identityNone, "", "", "")
	s.resolveStartupIdentity()
	audit := filepath.Join(s.projectDir, ".desk-private", "audit")
	if err := os.Rename(audit, audit+".old"); err != nil {
		t.Fatal(err)
	}
	s.recoverRotations()
	if got := heldUnder(t, config, id); got != ".keys.jsonl,.next.seed,.rotating,.seed seed=1 next=2 keys=1" {
		t.Fatalf("the start left %s: %s", got, logs)
	}
	listing := s.archiveListing()
	if listing == nil || len(listing.Entries) != 1 {
		t.Fatalf("the decision record lists %+v", listing)
	}
	entry := listing.Entries[0]
	if !entry.Unresolved || entry.Identity != id || entry.File != id+rotatingSuffix || entry.Kind != "rotating" || entry.Trail != fixtureTrail || entry.Token != "" ||
		entry.Why != fmt.Sprintf(unresolvedRotationWords, journalRotate, fixtureTrail) {
		t.Errorf("the decision record lists %+v", entry)
	}
	request := httptest.NewRequest("GET", "http://localhost/api/audit/verify", nil)
	request.Header.Set("Authorization", "Bearer probe")
	w := httptest.NewRecorder()
	s.handleAuditVerify(w, request)
	if !strings.Contains(w.Body.String(), `"unresolved":true`) || !strings.Contains(w.Body.String(), `"file":"`+id+rotatingSuffix+`"`) {
		t.Errorf("the decision record answered %d %s", w.Code, w.Body)
	}
	if w := removeOn(s, `{"scope":"desk","identity":"`+id+`","file":"`+id+rotatingSuffix+`","token":"`+strings.Repeat("a", 64)+`"}`, nil); w.Code != http.StatusBadRequest {
		t.Errorf("a Remove of unresolved custody answered %d %s", w.Code, w.Body)
	}
	// The project's own name is never listed so.
	if own := (&signingDir{root: bareRoot(t, signing), path: signing}).unresolvedIn(id); len(own) != 0 {
		t.Errorf("a project's own marker is listed as unresolved: %+v", own)
	}
}
