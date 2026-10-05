package desk

// The key a creation makes against swaps, crashes and a second creation at
// once (ADR-0010, section 1): the signing folder's pathname must name the
// folder Desk holds before the runtime runs, and the seed's pathname the seed
// Desk found, after it and before the desk is published; a creation stopped
// at any step leaves no key past the next start, and a key with no marker is
// never removed; two first creations at once are both signed. A stand-in
// runtime, by absolute path, answers each command.

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
)

// swapSigningFolder moves Desk's signing folder aside and puts a new, empty,
// owner-only folder at its name, as a process of the same user could. It
// answers where the folder Desk holds now is.
func swapSigningFolder(t *testing.T, folder string) string {
	t.Helper()
	aside := folder + ".held"
	if os.Rename(folder, aside) != nil || os.Mkdir(folder, 0o700) != nil {
		t.Error("could not swap the signing folder")
	}
	return aside
}

// **A swapped signing folder never makes a signed desk.** At each moment a
// swap would matter, before the runtime is given its path, after it wrote the
// seed, and before the desk is published, the creation is refused, says when,
// and is never answered as signed. Nothing is written into the folder swapped
// in, and what the creation made is removed from the folder it holds.
func TestASwappedSigningFolderNeverMakesASignedDesk(t *testing.T) {
	for _, tc := range []struct{ at, why string }{
		{"before generate", "Desk's signing folder was replaced before the runtime made the key, so no key was made"},
		{"after generate", "Desk's signing folder was replaced while the runtime made the key, so the key is not where the desk would name it"},
		{"before publish", "Desk's signing folder was replaced before the desk was published, so its key is not where the desk would name it"},
	} {
		t.Run(tc.at, func(t *testing.T) {
			calls, s, ts, _ := signingStandIn(t)
			const id = "b1000000000000000000000000000001"
			signsDesks(t, calls, s.configDir, id)
			folder := signingFolderOf(s)
			var aside string
			testHookKeyBetween = func(at string) {
				if at == tc.at {
					aside = swapSigningFolder(t, folder)
				}
			}
			t.Cleanup(func() { testHookKeyBetween = nil })
			status, data := deskCall(t, ts, "POST", "/api/desks", "", `{"name":"Swapped"}`, true)
			testHookKeyBetween = nil
			assertNoDesk(t, s, ts, status, data, tc.why)
			if strings.Contains(string(data), `"signed":true`) {
				t.Errorf("a swapped creation was answered as signed: %s", data)
			}
			if names := namesIn(t, folder); len(names) != 0 {
				t.Errorf("the folder swapped in holds %q", names)
			}
			if aside == "" {
				t.Fatal("the swap was never made")
			}
			if names := namesIn(t, aside); len(names) != 0 {
				t.Errorf("the folder Desk held keeps %q", names)
			}
		})
	}
}

// postAbandoned sends a creation whose handler is stopped, as a crash would
// stop it, and ignores the broken answer.
func postAbandoned(t *testing.T, s *Server, url string) {
	t.Helper()
	request, _ := http.NewRequest("POST", url+"/api/desks", strings.NewReader(`{"name":"Stopped"}`))
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "application/json")
	if response, err := http.DefaultClient.Do(request); err == nil {
		response.Body.Close()
	}
}

// **A creation stopped at any step leaves no key past the next start.** Its
// marker is there from before the runtime runs until the manifest is written;
// the next start removes a desk's seed, list and marker where it was never
// published, and only the marker where it was.
func TestAStoppedCreationLeavesNoKeyPastTheNextStart(t *testing.T) {
	const id = "b2000000000000000000000000000001"
	for _, tc := range []struct {
		at        string
		left      []string
		published bool
	}{
		{"before generate", []string{id + ".creating"}, false},
		{"after generate", []string{id + ".creating", id + ".seed"}, false},
		{"before publish", []string{id + ".creating", id + ".keys.jsonl", id + ".seed"}, false},
		{"before staging", []string{id + ".creating", id + ".keys.jsonl", id + ".seed"}, false},
		{"after staging", []string{id + ".creating", id + ".keys.jsonl", id + ".seed"}, false},
		{"after rename", []string{id + ".creating", id + ".keys.jsonl", id + ".seed"}, true},
		{"published", []string{id + ".creating", id + ".keys.jsonl", id + ".seed"}, true},
	} {
		t.Run(tc.at, func(t *testing.T) {
			calls, s, ts, _ := signingStandIn(t)
			signsDesks(t, calls, s.configDir, id)
			testHookKeyBetween = func(at string) {
				if at == tc.at {
					panic(http.ErrAbortHandler)
				}
			}
			t.Cleanup(func() { testHookKeyBetween = nil })
			postAbandoned(t, s, ts.URL)
			testHookKeyBetween = nil
			folder := signingFolderOf(s)
			if names := namesIn(t, folder); !slices.Equal(names, tc.left) {
				t.Fatalf("the stopped creation left %q, want %q", names, tc.left)
			}
			// Publication is one event: before the rename there is no manifest
			// at its name, after it a whole one.
			manifest, err := os.ReadFile(filepath.Join(s.configDir, "desks", id, ".desk-private", "desk.json"))
			if tc.published != (err == nil) || tc.published && !strings.Contains(string(manifest), `"id":"`+id+`"`) {
				t.Errorf("at %s the manifest is %q, %v; want it whole only once published", tc.at, manifest, err)
			}
			ts.Close()
			again, logged := restartedServer(t, s)
			want := []string(nil)
			if tc.published {
				want = []string{id + ".keys.jsonl", id + ".seed"}
				again.desksMu.Lock()
				opened := again.desks[id] != nil
				again.desksMu.Unlock()
				if !opened {
					t.Errorf("the published desk was not opened: %s", logged)
				}
			}
			if names := namesIn(t, folder); !slices.Equal(names, want) {
				t.Errorf("after the next start the signing folder holds %q, want %q (%s)", names, want, logged)
			}
		})
	}
}

// **A key with no marker is never removed, and neither is one whose desk
// cannot be told.** A seed and a list with no marker stay, whatever the desks
// folder holds; a marker whose desk's folder cannot be inspected leaves its
// seed, its list and itself.
func TestAKeyIsRemovedAtStartOnlyByItsMarker(t *testing.T) {
	_, s, ts, _ := signingStandIn(t)
	ts.Close()
	folder := signingFolderOf(s)
	if err := os.Mkdir(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	const unmarked, unsure = "b3000000000000000000000000000001", "b3000000000000000000000000000002"
	for _, name := range []string{unmarked + ".seed", unmarked + ".keys.jsonl", unsure + ".seed", unsure + ".keys.jsonl", unsure + ".creating"} {
		if err := os.WriteFile(filepath.Join(folder, name), []byte("kept\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// The unsure desk's folder is there, and nothing in it can be inspected.
	private := filepath.Join(s.configDir, "desks", unsure, ".desk-private")
	if err := os.MkdirAll(private, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(private, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(private, 0o700) })
	if _, err := os.Lstat(filepath.Join(private, "desk.json")); os.IsNotExist(err) || err == nil {
		t.Skip("this user can inspect a folder it may not read; the test needs one it cannot")
	}
	restartedServer(t, s)
	want := []string{unmarked + ".keys.jsonl", unmarked + ".seed", unsure + ".creating", unsure + ".keys.jsonl", unsure + ".seed"}
	if names := namesIn(t, folder); !slices.Equal(names, want) {
		t.Errorf("the start left %q, want %q", names, want)
	}
}

// **Two first creations at once are both signed.** Both find no signing
// folder; the one whose folder is made second finds it there, checks it as
// usual, and keeps its key.
func TestTwoFirstCreationsAtOnceAreBothSigned(t *testing.T) {
	p := newPause(t)
	bin := filepath.Join(t.TempDir(), "jpack")
	calls := writeStandInRuntime(t, bin, p.fragment+reading(withAuditVersions), lockRefusal)
	s, ts, _ := gatesServer(t, bin)
	const first, second = "b4000000000000000000000000000001", "b4000000000000000000000000000002"
	signsDesks(t, calls, s.configDir, first, second)
	a, b := createAsync(ts, "First"), createAsync(ts, "Second")
	p.reached(t)
	p.reached(t)
	if _, err := os.Lstat(signingFolderOf(s)); !os.IsNotExist(err) {
		t.Fatalf("the signing folder was made before either creation asked for a key: %v", err)
	}
	p.release(t)
	p.release(t)
	for _, pending := range []<-chan answer{a, b} {
		got := await(t, pending)
		if got.status != http.StatusCreated || !strings.Contains(string(got.body), `"signed":true`) {
			t.Errorf("a first creation answered %d %s, want it signed", got.status, got.body)
		}
	}
	if names := namesIn(t, signingFolderOf(s)); len(names) != 4 {
		t.Errorf("the signing folder holds %q, want both desks' keys", names)
	}
}

// **A seed another user owns is not named.** The runtime refuses a key that
// is not its own user's, and would sign nothing with it.
func TestASeedAnotherUserOwnsIsRefused(t *testing.T) {
	seed := filepath.Join(t.TempDir(), "a.seed")
	if err := os.WriteFile(seed, []byte(standInSeed+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Lstat(seed)
	if err := checkSeed("a.seed", info); err != nil {
		t.Fatalf("the user's own seed was refused: %v", err)
	}
	was := effectiveUser
	me := was()
	effectiveUser = func() uint32 { return me + 1 }
	t.Cleanup(func() { effectiveUser = was })
	if err := checkSeed("a.seed", info); err == nil || !strings.Contains(err.Error(), "is owned by user") {
		t.Errorf("a seed another user owns was accepted: %v", err)
	}
}

// **A list of keys swapped between being looked at and being opened is not
// read.** The panel passes no key, and says why.
func TestAListOfKeysSwappedWhileReadIsNotRead(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	rig := newAuditRig(t, withAuditVersions)
	rig.answers(t, 0, auditValidReport)
	s, ts, _ := gatesServer(t, rig.bin)
	const id = "b5000000000000000000000000000001"
	row := createSignedDesk(t, s, ts, rig.calls, id)
	list := filepath.Join(signingFolderOf(s), id+".keys.jsonl")
	testHookKeyBetween = func(at string) {
		if at != "read list" {
			return
		}
		other := list + ".other"
		if os.WriteFile(other, []byte(wantKeyLine(standInPublicKey, standInKeyID, 0)), 0o600) != nil || os.Rename(other, list) != nil {
			t.Error("could not swap the list")
		}
	}
	t.Cleanup(func() { testHookKeyBetween = nil })
	assertKeysUnread(t, ts, rig, row.ID, "Desk could not read the public keys it keeps for this desk: it changed between being inspected and being opened.")
}

// **A manifest that cannot be written leaves no key.** The key was made, the
// desk was not published, and what was made of its key is removed.
func TestAManifestThatCannotBeWrittenLeavesNoKey(t *testing.T) {
	calls, s, ts, _ := signingStandIn(t)
	const id = "b6000000000000000000000000000001"
	signsDesks(t, calls, s.configDir, id)
	private := filepath.Join(s.configDir, "desks", id, ".desk-private")
	testHookKeyBetween = func(at string) {
		if at == "before publish" {
			os.Chmod(private, 0o500)
		}
	}
	t.Cleanup(func() {
		testHookKeyBetween = nil
		os.Chmod(private, 0o700)
	})
	status, data := deskCall(t, ts, "POST", "/api/desks", "", `{"name":"Unwritten"}`, true)
	if status == http.StatusCreated {
		t.Fatalf("a desk whose manifest could not be written was made: %s", data)
	}
	if names := namesIn(t, signingFolderOf(s)); len(names) != 0 {
		t.Errorf("a creation whose manifest failed left %q", names)
	}
}

// inheritedKeyFolder is a folder for an inherited key whose name holds a
// space, a tab, a line separator, a double quote and a colon, under a parent
// whose name holds a space: each a way the general rule could leave part of
// the path behind.
func inheritedKeyFolder(t *testing.T) (root, folder string) {
	t.Helper()
	root = filepath.Join(t.TempDir(), "Home DIR")
	folder = filepath.Join(root, "Key STORE\t\"q\": a TAIL")
	if os.Mkdir(root, 0o700) != nil || os.Mkdir(folder, 0o700) != nil {
		t.Skip("this file system refuses the names")
	}
	return root, folder
}

// leaksOf fails the test for each part of an inherited key's path a sentence
// keeps.
func leaksOf(t *testing.T, what, said, root string) {
	t.Helper()
	for _, part := range []string{"Home", "DIR", "STORE", "TAIL", `\"q\"`, root, "owner.seed"} {
		if strings.Contains(said, part) {
			t.Errorf("%s names %q: %s", what, part, said)
		}
	}
}

// **No part of an inherited key's path is said, nor of a folder on its
// way.** The runtime names the key JPACK_SIGNING_KEY names, and from runtime
// #222 a folder on its path, as it prints them (runtimePrints, the oracle);
// the panel says "…" for each, whatever the folder's name holds.
func TestThePanelNeverSaysAFolderOnAnInheritedKeysPath(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	root, folder := inheritedKeyFolder(t)
	key := filepath.Join(folder, "owner.seed")
	t.Setenv("JPACK_SIGNING_KEY", key)
	ts, rig, _ := auditDesk(t, withAuditVersions, auditedConfig)
	rig.answers(t, 0, auditValidReport)
	refused := "The signing key JPACK_SIGNING_KEY names, " + runtimePrints(key) + ", is refused, so records are written unsigned: " +
		strings.Replace(reasonDirectoryWritable, "%s", runtimePrints(folder), 2) + "; and " + strings.Replace(reasonDirectoryOwner, "%s", runtimePrints(root), 1) + "."
	refused = strings.Replace(refused, "chmod go-w %s", "chmod go-w "+runtimePrints(folder), 1)
	validatingAs(t, rig.calls, 1, `{"outputVersion":"2","command":"packs validate","status":"invalid","checks":[{"name":"audit-signing-key","status":"failed","detail":`+jsonString(refused)+`}],"packs":[]}`)
	status, data := reviewCall(t, ts, "GET", "/api/audit/verify", "", nil, bearer)
	var answer auditAnswer
	if status != http.StatusOK || json.Unmarshal(data, &answer) != nil || answer.Signing == nil {
		t.Fatalf("the panel answered %d %s", status, data)
	}
	want := "The signing key JPACK_SIGNING_KEY names, …, is refused, so records are written unsigned: the directory … on the signing key's path can be written by its group or by other users (mode 0777) and has no sticky bit, so another user could remove or replace the key; chmod go-w … fixes it; and the directory … on the signing key's path is owned by uid 4242, neither root nor the user this runtime runs as (uid 1000)."
	if answer.Signing.Detail != want {
		t.Errorf("the panel says\n%q\nwant\n%q", answer.Signing.Detail, want)
	}
	leaksOf(t, "the panel", string(data), root)
}

// **With a runtime that checks the folders on a key's path** (0.27.0): an
// inherited key in a folder others can write is refused for that folder, and
// the panel shows the runtime's words with no part of the folder's path.
func TestAnInheritedKeysFolderWithTheRuntime(t *testing.T) {
	bin := requireBinary(t)
	t.Setenv("JPACK_CONFIG", "")
	t.Setenv("JPACK_SIGNING_KEY", "")
	if !runtimeChecksKeyFolders(t, bin) {
		t.Skip("this runtime does not check the folders on a key's path")
	}
	root, folder := inheritedKeyFolder(t)
	key := filepath.Join(folder, "owner.seed")
	jpackIn(t, bin, t.TempDir(), "audit", "key", "generate", key, "--format", "json")
	if err := os.Chmod(folder, 0o777); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JPACK_SIGNING_KEY", key)
	project := t.TempDir()
	writeProject(t, project, map[string]string{"jpack.json": auditedConfig})
	s, ts := startDesk(t, Config{ProjectDir: project, JpackBin: bin, Token: testToken, Logger: log.New(io.Discard, "", 0)})
	t.Cleanup(func() { s.Close() })
	t.Cleanup(ts.Close)
	status, data := reviewCall(t, ts, "GET", "/api/audit/verify", "", nil, bearer)
	var answer auditAnswer
	if status != http.StatusOK || json.Unmarshal(data, &answer) != nil || answer.Signing == nil {
		t.Fatalf("the panel answered %d %s", status, data)
	}
	if answer.Signing.Status != "failed" || !strings.HasPrefix(answer.Signing.Detail, "The signing key JPACK_SIGNING_KEY names, …, is refused, so records are written unsigned: the directory … on the signing key's path can be written by its group or by other users (mode 0777)") {
		t.Errorf("the panel shows the key check %+v", answer.Signing)
	}
	leaksOf(t, "the panel", string(data), root)
}

// **A manifest that is not whole is not a publication.** A desk folder whose
// manifest is empty, or cut short, at its published name is one the registry
// will not open; the next start counts it as never published, by the same
// reader, and removes the key its marker names.
func TestAManifestThatIsNotWholeIsNotAPublication(t *testing.T) {
	const id = "b7000000000000000000000000000001"
	for _, tc := range []struct{ name, manifest string }{
		{"empty", ""},
		{"cut short", `{"id":"` + id + `","na`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, s, ts, _ := signingStandIn(t)
			signsDesks(t, calls, s.configDir, id)
			testHookKeyBetween = func(at string) {
				if at == "published" {
					panic(http.ErrAbortHandler)
				}
			}
			t.Cleanup(func() { testHookKeyBetween = nil })
			postAbandoned(t, s, ts.URL)
			testHookKeyBetween = nil
			if err := os.WriteFile(filepath.Join(s.configDir, "desks", id, ".desk-private", "desk.json"), []byte(tc.manifest), 0o600); err != nil {
				t.Fatal(err)
			}
			ts.Close()
			again, logged := restartedServer(t, s)
			if names := namesIn(t, signingFolderOf(s)); len(names) != 0 {
				t.Errorf("a desk whose manifest is %s kept %q (%s)", tc.name, names, logged)
			}
			again.desksMu.Lock()
			opened := again.desks[id] != nil
			again.desksMu.Unlock()
			if opened {
				t.Errorf("a desk whose manifest is %s was opened", tc.name)
			}
		})
	}
}

// **A manifest that could not be read now is not taken for "not published".**
// A desk stopped after its manifest was published, and before its marker was
// removed, is a desk the registry opens. If the start's sweep cannot read
// that manifest once, an I/O error that says nothing of the file, the sweep
// cannot tell, and so keeps the seed, the list and the marker; the registry,
// which then reads the manifest, opens the desk with its key. (Review round
// 3: the sweep had read any failure to read as "too large", so "not a
// desk's", and removed a published desk's key.)
func TestAManifestNotReadNowKeepsItsKey(t *testing.T) {
	const id = "b7000000000000000000000000000002"
	calls, s, ts, _ := signingStandIn(t)
	signsDesks(t, calls, s.configDir, id)
	testHookKeyBetween = func(at string) {
		if at == "published" {
			panic(http.ErrAbortHandler)
		}
	}
	t.Cleanup(func() { testHookKeyBetween = nil })
	postAbandoned(t, s, ts.URL)
	testHookKeyBetween = nil
	if _, err := os.Stat(filepath.Join(s.configDir, "desks", id, deskManifest)); err != nil {
		t.Fatal("the desk's manifest was not published:", err)
	}
	failed := 0
	testHookPrivateRead = func(name string) error {
		if name == deskManifest && failed == 0 {
			failed++
			return syscall.EIO
		}
		return nil
	}
	t.Cleanup(func() { testHookPrivateRead = nil })
	ts.Close()
	again, logged := restartedServer(t, s)
	testHookPrivateRead = nil
	if failed != 1 {
		t.Fatal("no read of the manifest was failed")
	}
	names := namesIn(t, signingFolderOf(s))
	for _, want := range []string{id + seedSuffix, id + keysSuffix} {
		if !slices.Contains(names, want) {
			t.Errorf("a published desk lost %s to a read that failed once: %q (%s)", want, names, logged)
		}
	}
	again.desksMu.Lock()
	opened := again.desks[id] != nil
	again.desksMu.Unlock()
	if !opened {
		t.Errorf("the published desk was not opened (%s)", logged)
	}
}

// **One generator gives every spelling of a path, and every folder on its
// way.** For a path as given (with "..", a doubled separator, a "." and a
// link on its way), as it is cleaned, and as it resolves, it gives the whole
// path and each prefix that ends where a separator starts, down to but not
// including the root, raw and as the runtime prints each. The expectations
// are spelled out here, by hand.
func TestPathSpansCoverEverySpelling(t *testing.T) {
	root := filepath.Join(t.TempDir(), "A dir")
	real := filepath.Join(root, "real\tone")
	if os.MkdirAll(real, 0o700) != nil || os.Symlink("real\tone", filepath.Join(root, "link")) != nil {
		t.Fatal("could not make the folders")
	}
	if err := os.WriteFile(filepath.Join(real, "k.seed"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	given := root + "/x/../link//./k.seed"
	if err := os.Mkdir(filepath.Join(root, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	got := map[pathSpan]bool{}
	for _, span := range pathSpans(given, false, "@") {
		got[span] = true
	}
	want := []pathSpan{
		// As given: the whole path, a file, and each prefix before a
		// separator, the doubled one twice.
		{given, "@", false},
		{root + "/x/../link//.", "@", true},
		{root + "/x/../link/", "@", true},
		{root + "/x/../link", "@", true},
		{root + "/x/..", "@", true},
		{root + "/x", "@", true},
		// Cleaned.
		{root + "/link/k.seed", "@", false},
		{root + "/link", "@", true},
		// Resolved, and as the runtime prints it, with "?" for the tab.
		{real + "/k.seed", "@", false},
		{real, "@", true},
		{runtimePrints(real + "/k.seed"), "@", false},
		{runtimePrints(real), "@", true},
		// Every folder above, to the root but not the root.
		{root, "@", true},
		{filepath.Dir(root), "@", true},
	}
	for _, span := range want {
		if !got[span] {
			t.Errorf("no span %+v", span)
		}
	}
	for span := range got {
		if span.value == "/" || span.value == "" {
			t.Errorf("a span for the root: %+v", span)
		}
	}
	// Desk's own key, list, signing folder and configuration folder come from
	// the same generator: the configuration folder as it was given too.
	s := &Server{configDir: root + "/./cfg"}
	have := map[pathSpan]bool{}
	for _, span := range s.custodySpans("abc", "@") {
		have[span] = true
	}
	for _, span := range []pathSpan{
		{root + "/cfg/secrets/signing/abc.seed", "@", false},
		{root + "/cfg/secrets/signing/abc.keys.jsonl", "@", false},
		{root + "/cfg/secrets/signing", "@", true},
		{root + "/./cfg", "@", true},
		{root + "/.", "@", true},
		{root + "/cfg", "@", true},
		{root, "@", true},
	} {
		if !have[span] {
			t.Errorf("no custody span %+v", span)
		}
	}
}

// **With runtime 0.27.0: no part of any spelling of an inherited key's path
// reaches the answer.** The key is named with spaces, "..", a doubled
// separator, a "." and, in one case, a linked folder: the runtime refuses the
// linked one by its path as given, and the other for a folder others can
// write, by that folder's cleaned path. The oracle for what the runtime
// prints is runtimePrints.
func TestNoSpellingOfAnInheritedKeysPathWithTheRuntime(t *testing.T) {
	bin := requireBinary(t)
	t.Setenv("JPACK_CONFIG", "")
	t.Setenv("JPACK_SIGNING_KEY", "")
	if !runtimeChecksKeyFolders(t, bin) {
		t.Skip("this runtime does not check the folders on a key's path")
	}
	base := filepath.Join(t.TempDir(), "PRIVATE inherited folder")
	target := filepath.Join(base, "PRIVATE real target")
	if os.MkdirAll(target, 0o700) != nil || os.Chmod(base, 0o700) != nil || os.Symlink("PRIVATE real target", filepath.Join(base, "linked dir")) != nil {
		t.Fatal("could not make the folders")
	}
	jpackIn(t, bin, t.TempDir(), "audit", "key", "generate", filepath.Join(target, "owner.seed"), "--format", "json")
	for _, tc := range []struct {
		name, key, says string
		open            string
	}{
		{"a linked folder", base + "/../PRIVATE inherited folder//linked dir/./owner.seed", "the signing key's path goes through a symbolic link", ""},
		{"a folder others can write", base + "/../PRIVATE inherited folder//PRIVATE real target/./owner.seed", "the directory … on the signing key's path can be written by its group or by other users (mode 0777)", target},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.open != "" {
				if err := os.Chmod(tc.open, 0o777); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.Chmod(tc.open, 0o700) })
			}
			t.Setenv("JPACK_SIGNING_KEY", tc.key)
			project := t.TempDir()
			writeProject(t, project, map[string]string{"jpack.json": auditedConfig})
			s, ts := startDesk(t, Config{ProjectDir: project, JpackBin: bin, Token: testToken, Logger: log.New(io.Discard, "", 0)})
			t.Cleanup(func() { s.Close() })
			t.Cleanup(ts.Close)
			status, data := reviewCall(t, ts, "GET", "/api/audit/verify", "", nil, bearer)
			var answer auditAnswer
			if status != http.StatusOK || json.Unmarshal(data, &answer) != nil || answer.Signing == nil {
				t.Fatalf("the panel answered %d %s", status, data)
			}
			if answer.Signing.Status != "failed" || !strings.HasPrefix(answer.Signing.Detail, "The signing key JPACK_SIGNING_KEY names, …, is refused, so records are written unsigned: "+tc.says) {
				t.Errorf("the panel shows the key check %+v", answer.Signing)
			}
			for _, part := range []string{"PRIVATE", "inherited", "linked", "real target", "owner.seed", "/..", runtimePrints(base)} {
				if strings.Contains(string(data), part) {
					t.Errorf("the answer names %q: %s", part, data)
				}
			}
		})
	}
}
