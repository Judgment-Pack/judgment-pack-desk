package desk

// Key custody for the desks Desk makes (ADR-0010, section 1): the key first,
// then the configuration that names it, then the lock; each fallback made
// unsigned and said; a key the runtime fails to generate leaves no desk and
// no key; the signing folder held to custody's rules; and the decision-record
// panel passing each public key Desk keeps, and showing the runtime's word on
// the key. A stand-in runtime, by absolute path, answers each command, so
// these run where no runtime is installed. The tests that drive the real
// runtime skip without one.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// A second public key, with its keyId as the first 32 hexadecimal
// characters of its SHA-256, computed outside this code; runtime 0.26.0
// printed the pair for a seed it generated.
const (
	secondPublicKey = "272ad95977cf9721551ead07d2c9563cb7403b3688a8c23508b6a02ba4302623"
	secondKeyID     = "1cba17c1fa03b21777f6bf729e3f1a2b"
)

// wantKeyLine is a line of a desk's list of public keys, spelled out.
func wantKeyLine(publicKey, keyID string, at int) string {
	return `{"publicKey":"` + publicKey + `","keyId":"` + keyID + `","at":` + strconv.Itoa(at) + "}\n"
}

// wantKeyLineOn is wantKeyLine for a key a rotation on trail added: its line
// records that trail (issue #285).
func wantKeyLineOn(publicKey, keyID string, at int, trail string) string {
	return `{"publicKey":"` + publicKey + `","keyId":"` + keyID + `","at":` + strconv.Itoa(at) + `,"trail":"` + trail + `"}` + "\n"
}

// signingFolderOf is Desk's signing folder on s, by its path.
func signingFolderOf(s *Server) string { return filepath.Join(s.configDir, "secrets", "signing") }

// namesIn is every name in dir, sorted; none where it does not exist.
func namesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func permOf(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

// signingStandIn is a stand-in that reads "6", whose `packs lock` refuses
// every desk signsDesks did not name, and a chassis over it.
func signingStandIn(t *testing.T) (string, *Server, *httptest.Server, *bytes.Buffer) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "jpack")
	calls := writeStandInRuntime(t, bin, reading(withAuditVersions), lockingAs(wantGatedConfig))
	s, ts, logged := gatesServer(t, bin)
	return calls, s, ts, logged
}

// **The key first, then the configuration that names it, then the lock that
// pins it** (ADR-0010, section 1). Where the runtime reads "6", the runtime
// generates the seed at Desk's signing folder, named by the desk's id, with
// neither JPACK_CONFIG nor an inherited JPACK_SIGNING_KEY; Desk keeps the
// public key it printed, and writes jpack.json at "6" naming the seed by its
// absolute path, byte for byte; the lock pins those bytes. The folder is
// 0700, the seed 0600 and untouched, and the list of keys 0600, with nothing
// staged left beside them.
func TestANewDeskIsSignedWhereTheRuntimeReads6(t *testing.T) {
	t.Setenv("JPACK_CONFIG", filepath.Join(t.TempDir(), "jpack.json"))
	t.Setenv("JPACK_SIGNING_KEY", filepath.Join(t.TempDir(), "owner.seed"))
	bin := filepath.Join(t.TempDir(), "jpack")
	// Every lock but the one signsDesks names is refused: the desk is locked
	// only if its configuration's digest is the one spelled out here.
	calls := writeStandInRuntime(t, bin, reading(withAuditVersions), lockRefusal)
	s, ts, logged := gatesServer(t, bin)
	generatingAs(t, calls, "  [ -e jpack.json ] && printf 'config before key\\n' >> '"+calls+".order'\n"+generateAsTheRuntime)
	const id = "b0000000000000000000000000000001"
	row := createSignedDesk(t, s, ts, calls, id)
	assertGatedFolder(t, row.Folder, wantSignedConfig(s.configDir, id))
	if !row.RequireComparableFacts || row.Name != "Gated" || !row.Managed {
		t.Errorf("the creation answered %+v", row)
	}

	folder := signingFolderOf(s)
	seed := filepath.Join(folder, id+".seed")
	want := "packs schema --format json [JPACK_CONFIG=unset]\n" +
		"audit key generate " + seed + " --format json [JPACK_CONFIG=unset]\n" +
		"packs lock --config jpack.json --format json [JPACK_CONFIG=unset]\n"
	if got := readFile(t, calls); got != want {
		t.Errorf("the runtime was run as\n%s\nwant\n%s", got, want)
	}
	for _, line := range envSeen(t, calls) {
		if !strings.Contains(line, "[JPACK_SIGNING_KEY=unset]") {
			t.Errorf("a made desk's command inherited the signing key: %s", line)
		}
	}
	if _, err := os.Stat(calls + ".order"); !os.IsNotExist(err) {
		t.Errorf("out of order: %s", readFile(t, calls+".order"))
	}
	if got := readFile(t, filepath.Join(folder, id+".keys.jsonl")); got != wantKeyLine(standInPublicKey, standInKeyID, 0) {
		t.Errorf("the list of public keys is %q", got)
	}
	if got := readFile(t, seed); got != standInSeed+"\n" {
		t.Errorf("the seed is not the runtime's: %q", got)
	}
	for path, perm := range map[string]os.FileMode{folder: 0o700, seed: 0o600, filepath.Join(folder, id+".keys.jsonl"): 0o600} {
		if got := permOf(t, path); got != perm {
			t.Errorf("%s is %v, want %v", filepath.Base(path), got, perm)
		}
	}
	if names := namesIn(t, folder); !slices.Equal(names, []string{id + ".keys.jsonl", id + ".seed"}) {
		t.Errorf("the signing folder holds %q", names)
	}
	if strings.Contains(logged.String(), "not signed") {
		t.Errorf("a signed desk was logged unsigned: %s", logged)
	}
}

// What a creation says where custody keeps no key, with why.
func wantUnsignedByCustody(why string) string {
	return "This desk is not signed, because Desk could not keep a signing key for it: " + why + ". It was created at configVersion 5, which names no signing key."
}

// **Where custody cannot keep a key, the desk is made unsigned, at "5", and
// says why** (ADR-0010, question 3). A signing folder that is a link, that
// others can write, or that is not a folder is refused and never repaired;
// the runtime is asked for no key, nothing is written where the link points,
// and the answer names Desk's folders by name, never by path.
func TestADeskCustodyCannotKeepAKeyForIsMadeUnsignedAndSaysWhy(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setUp func(t *testing.T, folder string) string
		why   string
	}{
		{"a linked signing folder", func(t *testing.T, folder string) string {
			elsewhere := t.TempDir()
			if err := os.Symlink(elsewhere, folder); err != nil {
				t.Fatal(err)
			}
			return elsewhere
		}, "Desk's signing folder is a symbolic link; a key is not kept anywhere reached through one"},
		{"a signing folder others can write", func(t *testing.T, folder string) string {
			if os.Mkdir(folder, 0o700) != nil || os.Chmod(folder, 0o770) != nil {
				t.Fatal("could not make the folder")
			}
			return folder
		}, "Desk's signing folder is writable by group or others (mode 0770); a directory anyone could write to may already hold something they put there, so narrowing it now would not make it safe — remove the 020 bits and restart the desk"},
		{"a signing folder that is a file", func(t *testing.T, folder string) string {
			if err := os.WriteFile(folder, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			return ""
		}, "Desk's signing folder is not a directory"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, s, ts, logged := signingStandIn(t)
			fixDeskIDs(t, "c0000000000000000000000000000001")
			target := tc.setUp(t, signingFolderOf(s))
			status, data := deskCall(t, ts, "POST", "/api/desks", "", `{"name":"Unsigned"}`, true)
			var row createdDesk
			if status != http.StatusCreated || json.Unmarshal(data, &row) != nil {
				t.Fatalf("create: %d %s", status, data)
			}
			assertGatedFolder(t, row.Folder, wantGatedConfig)
			if row.ConfigVersion != "5" || row.Signed || row.Notice != wantUnsignedByCustody(tc.why) {
				t.Errorf("the creation answered %+v, want it unsigned at 5 saying %q", row, tc.why)
			}
			if strings.Contains(row.Notice, s.configDir) || strings.Contains(row.Notice, "/") && !strings.Contains(tc.why, "/") {
				t.Errorf("the notice names a path: %q", row.Notice)
			}
			if !strings.Contains(logged.String(), "desk: "+row.Notice) {
				t.Errorf("the unsigned desk was not logged: %s", logged)
			}
			if strings.Contains(readFile(t, calls), "audit key") {
				t.Errorf("the runtime was asked for a key: %s", readFile(t, calls))
			}
			if target != "" {
				if names := namesIn(t, target); len(names) != 0 {
					t.Errorf("something was written into the refused folder: %q", names)
				}
			}
		})
	}
}

// **A path jpack.json cannot name is not named.** A configuration folder
// whose path is not UTF-8 cannot be written as a JSON string without naming
// another file, so the desk is made unsigned and says so.
func TestASigningFolderJpackJSONCannotNameIsNotUsed(t *testing.T) {
	config := filepath.Join(t.TempDir(), "not-utf8-\xff")
	if err := os.Mkdir(config, 0o700); err != nil {
		t.Skipf("this file system refuses the name: %v", err)
	}
	bin := filepath.Join(t.TempDir(), "jpack")
	calls := writeStandInRuntime(t, bin, reading(withAuditVersions), lockingAs(wantGatedConfig))
	s, ts := startDesk(t, Config{ProjectDir: t.TempDir(), JpackBin: bin, Token: testToken, DeskConfigDir: config, Logger: log.New(io.Discard, "", 0)})
	t.Cleanup(func() { s.Close() })
	t.Cleanup(ts.Close)
	row := createGatedDesk(t, ts)
	if row.ConfigVersion != "5" || row.Signed || row.Notice != wantUnsignedByCustody("the path of Desk's signing folder is not valid UTF-8, which jpack.json cannot name") {
		t.Errorf("the creation answered %+v", row)
	}
	if strings.Contains(readFile(t, calls), "audit key") {
		t.Error("the runtime was asked for a key")
	}
}

// **A key the runtime fails to generate makes no desk, and leaves no key.**
// Its refusal is said in its words, with Desk's folders named by name; a seed
// a failed run left is removed; a seed the runtime reported but did not
// write, one others can read, and an answer whose keyId is not its key's, or
// that is not `audit key generate`'s, are each refused; and a lock that
// fails after the key was made moves the seed and the list of keys to Desk's
// archive of keys, never removing them.
func TestAKeyTheRuntimeFailsToGenerateMakesNoDeskAndLeavesNoKey(t *testing.T) {
	const id = "d0000000000000000000000000000001"
	generated := func(publicKey, keyID string) string {
		return "  (umask 077; printf '%s\\n' '" + standInSeed + "' > \"$4\")\n  printf '%s\\n' '" +
			`{"outputVersion":"2","command":"audit key generate","status":"generated","publicKey":"` + publicKey + `","keyId":"` + keyID + `"}'`
	}
	for _, tc := range []struct{ name, generate, lock, why string }{
		{"refused in the runtime's words",
			"  printf '{\"outputVersion\":\"2\",\"command\":\"audit key generate\",\"status\":\"error\",\"diagnostics\":[{\"code\":\"JPS-AUDIT-KEY-WRITE\",\"message\":\"The seed could not be written to %s.\"}]}\\n' \"$4\"\n  exit 4",
			"", "the runtime did not generate its signing key: The seed could not be written to …."},
		{"a seed left by a run that failed",
			"  (umask 077; printf 'partial\\n' > \"$4\")\n  exit 1",
			"", "the runtime did not generate its signing key: the runtime's audit key generate … --format json failed: exit status 1"},
		{"reported and not written",
			"  printf '%s\\n' '" + standInGenerated + "'",
			"", "the runtime reported a signing key, but there is none in the signing folder"},
		{"a seed others can read",
			"  (umask 022; printf '%s\\n' '" + standInSeed + "' > \"$4\")\n  printf '%s\\n' '" + standInGenerated + "'",
			"", id + ".seed is readable or writable by someone other than its owner (mode 0644), so it is not treated as this desk's key"},
		{"a keyId that is not its key's", generated(standInPublicKey, secondKeyID),
			"", "its audit key generate did not answer as documented: a keyId is not the one its public key has"},
		{"a public key in capitals", generated(strings.ToUpper(standInPublicKey), standInKeyID),
			"", "its audit key generate did not answer as documented: a public key is not 64 lowercase hexadecimal characters"},
		{"a status other than generated",
			"  (umask 077; printf '%s\\n' '" + standInSeed + "' > \"$4\")\n  printf '%s\\n' '{\"command\":\"audit key generate\",\"status\":\"read\",\"publicKey\":\"" + standInPublicKey + "\",\"keyId\":\"" + standInKeyID + "\"}'",
			"", "the runtime did not generate its signing key: its audit key generate did not answer as documented"},
		{"another command's answer, as generated",
			"  (umask 077; printf '%s\\n' '" + standInSeed + "' > \"$4\")\n  printf '%s\\n' '{\"command\":\"audit key rotate\",\"status\":\"generated\",\"publicKey\":\"" + standInPublicKey + "\",\"keyId\":\"" + standInKeyID + "\"}'",
			"", "the runtime did not generate its signing key: its audit key generate did not answer as documented"},
		{"another command's answer",
			"  (umask 077; printf '%s\\n' '" + standInSeed + "' > \"$4\")\n  printf '%s\\n' '{\"command\":\"audit key public\",\"status\":\"read\",\"publicKey\":\"" + standInPublicKey + "\",\"keyId\":\"" + standInKeyID + "\"}'",
			"", "the runtime did not generate its signing key: its audit key generate did not answer as documented"},
		{"a lock that fails after it", generateAsTheRuntime, lockRefusal,
			"the runtime did not lock it: The stand-in refuses this configuration"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "jpack")
			calls := writeStandInRuntime(t, bin, reading(withAuditVersions), lockRefusal)
			s, ts, _ := gatesServer(t, bin)
			generatingAs(t, calls, tc.generate)
			if tc.lock == "" {
				signsDesks(t, calls, s.configDir, id)
			} else {
				fixDeskIDs(t, id)
			}
			status, data := deskCall(t, ts, "POST", "/api/desks", "", `{"name":"Keyless"}`, true)
			assertNoDesk(t, s, ts, status, data, tc.why)
			if strings.Contains(string(data), s.configDir) {
				t.Errorf("the refusal names a path: %s", data)
			}
			if names := liveIn(t, signingFolderOf(s)); len(names) != 0 {
				t.Errorf("a failed generation left %q in the signing folder", names)
			}
			// What the failed run made is in the archive, never removed.
			for _, archived := range archivedIn(t, signingFolderOf(s), id) {
				if !strings.HasSuffix(archived, " "+archiveCreationStopped) {
					t.Errorf("a failed generation archived %q", archived)
				}
			}
		})
	}
}

// **Nothing already kept is touched.** A seed or a list of keys already kept
// under a new desk's id is never written over or removed: the runtime is not
// asked, and no desk is made.
func TestAKeyAlreadyKeptUnderTheIdIsNeverTouched(t *testing.T) {
	const id = "d0000000000000000000000000000002"
	for _, name := range []string{id + ".seed", id + ".keys.jsonl"} {
		t.Run(name, func(t *testing.T) {
			calls, s, ts, _ := signingStandIn(t)
			signsDesks(t, calls, s.configDir, id)
			folder := signingFolderOf(s)
			if err := os.Mkdir(folder, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(folder, name), []byte("kept\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			status, data := deskCall(t, ts, "POST", "/api/desks", "", `{"name":"Taken"}`, true)
			assertNoDesk(t, s, ts, status, data, "something is already kept as "+name+", and a key is never written over anything")
			if got := readFile(t, filepath.Join(folder, name)); got != "kept\n" {
				t.Errorf("what was kept became %q", got)
			}
			if names := namesIn(t, folder); !slices.Equal(names, []string{name}) {
				t.Errorf("the signing folder holds %q", names)
			}
			if strings.Contains(readFile(t, calls), "audit key") {
				t.Error("the runtime was asked for a key over one already kept")
			}
		})
	}
}

// **A list of public keys is never written over.** Linked into place, where
// a rename would replace whatever the name held; what is kept is left as it
// is, and nothing staged is left beside it.
func TestAListOfKeysIsNeverWrittenOver(t *testing.T) {
	folder := t.TempDir()
	root, err := os.OpenRoot(folder)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	dir := &signingDir{root: root, path: folder}
	if err := os.WriteFile(filepath.Join(folder, "a.keys.jsonl"), []byte("kept\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := dir.writeNewKeys("a.keys.jsonl", []deskPublicKey{{standInPublicKey, standInKeyID, 0, ""}}); err == nil {
		t.Error("a list was written over one already kept")
	}
	if got := readFile(t, filepath.Join(folder, "a.keys.jsonl")); got != "kept\n" {
		t.Errorf("what was kept became %q", got)
	}
	if _, err := dir.writeNewKeys("b.keys.jsonl", []deskPublicKey{{standInPublicKey, standInKeyID, 0, ""}}); err != nil {
		t.Fatal(err)
	}
	if names := namesIn(t, folder); !slices.Equal(names, []string{"a.keys.jsonl", "b.keys.jsonl"}) {
		t.Errorf("the folder holds %q", names)
	}
}

// **A seed with more than one name, or that is a link, is not named.** The
// runtime refuses either and would sign nothing with it.
func TestASeedWithAnotherNameOrALinkIsRefused(t *testing.T) {
	dir := t.TempDir()
	seed := filepath.Join(dir, "a.seed")
	if err := os.WriteFile(seed, []byte(standInSeed+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Lstat(seed)
	if err := checkSeed("a.seed", info); err != nil {
		t.Fatalf("a seed with one name was refused: %v", err)
	}
	if err := os.Link(seed, filepath.Join(dir, "b.seed")); err != nil {
		t.Fatal(err)
	}
	info, _ = os.Lstat(seed)
	if err := checkSeed("a.seed", info); err == nil || !strings.Contains(err.Error(), "more than one name") {
		t.Errorf("a seed with two names was accepted: %v", err)
	}
	if err := os.Symlink(seed, filepath.Join(dir, "c.seed")); err != nil {
		t.Fatal(err)
	}
	info, _ = os.Lstat(filepath.Join(dir, "c.seed"))
	if err := checkSeed("c.seed", info); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Errorf("a linked seed was accepted: %v", err)
	}
}

// **A desk made during shutdown leaves no key at a live name.** The key was
// generated, but the desk is not published, so the seed, the list of keys and
// the marker go to Desk's archive of keys, never removed.
func TestADeskMadeDuringShutdownLeavesNoKey(t *testing.T) {
	p := newPause(t)
	calls, s, ts, _ := signingStandIn(t)
	generatingAs(t, calls, p.fragment+generateAsTheRuntime)
	signsDesks(t, calls, s.configDir, "e0000000000000000000000000000001")
	pending := createAsync(ts, "Late")
	p.reached(t)
	s.closeDesks()
	p.release(t)
	got := await(t, pending)
	if got.status != http.StatusConflict || !strings.Contains(string(got.body), "shutting down") {
		t.Errorf("a creation finished during shutdown answered %d %s", got.status, got.body)
	}
	if names := liveIn(t, signingFolderOf(s)); len(names) != 0 {
		t.Errorf("a desk made during shutdown left %q in the signing folder", names)
	}
	if got := archivedIn(t, signingFolderOf(s), "e0000000000000000000000000000001"); !slices.Equal(got, kindsArchived(archiveCreationStopped, "keys.jsonl", "seed", "creating")) {
		t.Errorf("a desk made during shutdown archived %q", got)
	}
}

// **The signing folder is custody's.** One of ours that others cannot write
// is narrowed to 0700 and used; one swapped for another between the check and
// the open is refused, and the desk is made unsigned.
func TestTheSigningFolderIsHeldToCustody(t *testing.T) {
	calls, s, ts, _ := signingStandIn(t)
	folder := signingFolderOf(s)
	if os.Mkdir(folder, 0o700) != nil || os.Chmod(folder, 0o755) != nil {
		t.Fatal("could not make the folder")
	}
	createSignedDesk(t, s, ts, calls, "f0000000000000000000000000000001")
	if perm := permOf(t, folder); perm != 0o700 {
		t.Errorf("the signing folder was left %v, want it narrowed to 0700", perm)
	}

	calls, s, ts, _ = signingStandIn(t)
	folder = signingFolderOf(s)
	decoy := filepath.Join(s.configDir, "secrets", "decoy")
	testHookAfterCustodyCheck = func(path string) {
		if path != folder {
			return
		}
		if os.Rename(folder, folder+".checked") != nil || os.Mkdir(decoy, 0o700) != nil || os.Symlink("decoy", folder) != nil {
			t.Error("could not swap the folder")
		}
	}
	t.Cleanup(func() { testHookAfterCustodyCheck = nil })
	fixDeskIDs(t, "f0000000000000000000000000000002")
	row := createGatedDesk(t, ts)
	if row.Signed || row.ConfigVersion != "5" || row.Notice != wantUnsignedByCustody("Desk's signing folder changed between being checked and being opened, and was not used") {
		t.Errorf("a swapped signing folder answered %+v", row)
	}
	if names := namesIn(t, decoy); len(names) != 0 {
		t.Errorf("a key was kept in the folder swapped in: %q", names)
	}
	if strings.Contains(readFile(t, calls), "audit key") {
		t.Error("the runtime was asked for a key in a swapped folder")
	}
}

// **The panel passes the key Desk keeps, and only with the seed's own.** It
// asks the runtime for the seed's public key (`audit key public`, with the
// seed's path), passes the list's key from a file of its own, written for
// that run in Desk's signing folder and removed after it, and shows the key
// as kept.
func TestThePanelPassesTheKeyDeskKeepsWithTheSeedsOwn(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	rig := newAuditRig(t, withAuditVersions)
	rig.answers(t, 0, auditValidReport)
	s, ts, _ := gatesServer(t, rig.bin)
	const id = "a1000000000000000000000000000001"
	row := createSignedDesk(t, s, ts, rig.calls, id)
	rig.ran(t)
	status, answer, refusal := readAudit(t, ts, row.ID)
	if status != http.StatusOK || answer.State != auditStateReport {
		t.Fatalf("the panel answered %d %+v %q", status, answer, refusal)
	}
	if want := "audit key public " + filepath.Join(signingFolderOf(s), id+".seed") + " --format json [JPACK_CONFIG=unset]"; !strings.Contains(readFile(t, rig.calls), want+"\n") {
		t.Errorf("the runtime was not asked for the seed's public key as %q: %s", want, readFile(t, rig.calls))
	}
	if seen := rig.keysSeen(t); !slices.Equal(seen, []string{standInPublicKey}) {
		t.Errorf("the runtime was given %q, want the desk's key", seen)
	}
	if calls := rig.ran(t); !slices.Equal(calls, []string{schemaCall, validateCall, publicCall, verifyWithKeyCall}) {
		t.Errorf("the panel ran %q", calls)
	}
	if answer.Keys == nil || answer.Keys.State != keysKept || !slices.Equal(answer.Keys.Public, []deskPublicKey{{standInPublicKey, standInKeyID, 0, ""}}) || answer.Keys.Problem != "" {
		t.Errorf("the panel shows the keys %+v", answer.Keys)
	}
	if names := namesIn(t, signingFolderOf(s)); !slices.Equal(names, []string{id + ".keys.jsonl", id + ".seed"}) {
		t.Errorf("the keys' files were not removed after the run: %q", names)
	}
}

// **The files of keys keep the keys' order.** Each key is written to a file
// of its own, in a new folder, and the paths come back in the keys' order;
// removing them leaves the folder as it was.
func TestPublicKeyFilesKeepTheKeysOrder(t *testing.T) {
	folder := t.TempDir()
	root, err := os.OpenRoot(folder)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	dir := &signingDir{root: root, path: folder}
	paths, remove, err := dir.publicKeyFiles([]deskPublicKey{{standInPublicKey, standInKeyID, 0, ""}, {secondPublicKey, secondKeyID, 7, ""}})
	if err != nil {
		t.Fatal(err)
	}
	var read []string
	for _, path := range paths {
		read = append(read, strings.TrimSuffix(readFile(t, path), "\n"))
	}
	if !slices.Equal(read, []string{standInPublicKey, secondPublicKey}) {
		t.Errorf("the files hold %q, want the keys in order", read)
	}
	if err := remove(); err != nil {
		t.Fatal(err)
	}
	if names := namesIn(t, folder); len(names) != 0 {
		t.Errorf("removing the files left %q", names)
	}
}

// **A list of keys is passed only with the seed it belongs to.** A list whose
// last key is not the seed's, whether it lists one key or more, a seed with
// no list, a list with no seed, a seed the runtime cannot read or Desk will
// not use, and an answer that is not `audit key public`'s each pass no key,
// and say so in words with no path; only a desk with neither has none.
func TestAKeyListThatIsNotTheSeedsPassesNoKey(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	const id = "a8000000000000000000000000000001"
	for _, tc := range []struct {
		name   string
		change func(t *testing.T, folder string, calls string)
		asked  bool
		why    string
	}{
		{"another key with its own keyId", func(t *testing.T, folder, _ string) {
			writeKeys(t, folder, id, wantKeyLine(secondPublicKey, secondKeyID, 0))
		}, true, "Desk's list of this desk's public keys does not name the key Desk keeps for it, so it passed no key."},
		{"no list", func(t *testing.T, folder, _ string) {
			removeNamed(t, folder, id+".keys.jsonl")
		}, false, "Desk keeps a key for this desk, but no list of its public keys, so it passed no key."},
		{"no seed", func(t *testing.T, folder, _ string) {
			removeNamed(t, folder, id+".seed")
		}, false, "Desk keeps a list of public keys for this desk, but not its key, so it passed no key."},
		{"two keys, the last another's", func(t *testing.T, folder, _ string) {
			writeKeys(t, folder, id, wantKeyLine(standInPublicKey, standInKeyID, 0)+wantKeyLine(secondPublicKey, secondKeyID, 7))
		}, true, "Desk's list of this desk's public keys does not name the key Desk keeps for it, so it passed no key."},
		{"a seed others can read", func(t *testing.T, folder, _ string) {
			if err := os.Chmod(filepath.Join(folder, id+".seed"), 0o640); err != nil {
				t.Fatal(err)
			}
		}, false, "Desk could not use the key it keeps for this desk: " + id + ".seed is readable or writable by someone other than its owner (mode 0640), so it is not treated as this desk's key."},
		{"refused by the runtime", func(t *testing.T, folder, calls string) {
			readingKeyAs(t, calls, "  printf '{\"outputVersion\":\"2\",\"command\":\"audit key public\",\"status\":\"invalid\",\"diagnostics\":[{\"code\":\"JPS-AUDIT-KEY-REFUSED\",\"message\":\"The key at %s is refused: no file is there.\"}]}\\n' \"$4\"\n  exit 1")
		}, true, "The runtime could not read the key Desk keeps for this desk, so it passed no key: The key at … is refused: no file is there."},
		{"another command's answer", func(t *testing.T, folder, calls string) {
			readingKeyAs(t, calls, "  printf '%s\\n' '"+strings.Replace(standInRead, `"command":"audit key public"`, `"command":"audit key generate"`, 1)+"'")
		}, true, "The runtime could not read the key Desk keeps for this desk, so it passed no key: its audit key public did not answer as documented."},
		{"another status", func(t *testing.T, folder, calls string) {
			readingKeyAs(t, calls, "  printf '%s\\n' '"+strings.Replace(standInRead, `"status":"read"`, `"status":"generated"`, 1)+"'")
		}, true, "The runtime could not read the key Desk keeps for this desk, so it passed no key: its audit key public did not answer as documented."},
		{"another keyId", func(t *testing.T, folder, calls string) {
			readingKeyAs(t, calls, "  printf '%s\\n' '"+strings.Replace(standInRead, standInKeyID, secondKeyID, 1)+"'")
		}, true, "Desk's list of this desk's public keys does not name the key Desk keeps for it, so it passed no key."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig := newAuditRig(t, withAuditVersions)
			rig.answers(t, 0, auditValidReport)
			s, ts, _ := gatesServer(t, rig.bin)
			row := createSignedDesk(t, s, ts, rig.calls, id)
			tc.change(t, signingFolderOf(s), rig.calls)
			rig.ran(t)
			status, data := reviewCall(t, ts, "GET", "/api/audit/verify", row.ID, nil, bearer)
			var answer auditAnswer
			if status != http.StatusOK || json.Unmarshal(data, &answer) != nil || answer.State != auditStateReport {
				t.Fatalf("the panel answered %d %s", status, data)
			}
			if answer.Keys == nil || answer.Keys.State != keysUnread || answer.Keys.Problem != tc.why || answer.Keys.Public != nil {
				t.Errorf("the panel shows the keys %+v, want unread: %q", answer.Keys, tc.why)
			}
			want := []string{schemaCall, validateCall, verifyCall}
			if tc.asked {
				want = []string{schemaCall, validateCall, publicCall, verifyCall}
			}
			if calls := rig.ran(t); !slices.Equal(calls, want) {
				t.Errorf("the panel ran %q, want %q", calls, want)
			}
			if seen := rig.keysSeen(t); seen != nil {
				t.Errorf("the runtime was given %q", seen)
			}
			if strings.Contains(string(data), s.configDir) {
				t.Errorf("the panel names a path: %s", data)
			}
		})
	}
}

// writeKeys replaces desk id's list of keys in folder with data.
func writeKeys(t *testing.T, folder, id, data string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(folder, id+".keys.jsonl"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

// removeNamed removes name from folder.
func removeNamed(t *testing.T, folder, name string) {
	t.Helper()
	if err := os.Remove(filepath.Join(folder, name)); err != nil {
		t.Fatal(err)
	}
}

// **A list of keys Desk cannot read passes no key, and says why.** Each way a
// list can be other than Desk writes it is refused, with words that name no
// path; the runtime is run with no key, and its report is still shown.
func TestAListOfKeysDeskCannotReadPassesNoKey(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	first, second := wantKeyLine(standInPublicKey, standInKeyID, 0), wantKeyLine(secondPublicKey, secondKeyID, 3)
	for _, tc := range []struct {
		name, data string
		mode       os.FileMode
		why        string
	}{
		{"empty", "", 0o600, "it is empty, or its last line does not end"},
		{"unended", strings.TrimSuffix(first, "\n"), 0o600, "it is empty, or its last line does not end"},
		{"another member order", `{"keyId":"` + standInKeyID + `","publicKey":"` + standInPublicKey + `","at":0}` + "\n", 0o600, "line 1 is not in the form Desk writes"},
		{"a space", strings.Replace(first, `,"at"`, `, "at"`, 1), 0o600, "line 1 is not in the form Desk writes"},
		{"another member", strings.Replace(first, `}`, `,"note":"x"}`, 1), 0o600, "line 1 is not in the form Desk writes"},
		{"no sequence", `{"publicKey":"` + standInPublicKey + `","keyId":"` + standInKeyID + `"}` + "\n", 0o600, "line 1 is not in the form Desk writes"},
		{"not JSON", "keys\n", 0o600, "line 1 is not in the form Desk writes"},
		{"a keyId not its key's", wantKeyLine(standInPublicKey, secondKeyID, 0), 0o600, "line 1: a keyId is not the one its public key has"},
		{"a short key", wantKeyLine(standInPublicKey[:62], standInKeyID, 0), 0o600, "line 1: a public key is not 64 lowercase hexadecimal characters"},
		{"a negative sequence", wantKeyLine(standInPublicKey, standInKeyID, -1), 0o600, "line 1: a key takes over from a sequence before the trail's first"},
		{"a first key after 0", wantKeyLine(standInPublicKey, standInKeyID, 2), 0o600, "line 1 does not take over after the key before it"},
		{"a second key at the first's sequence", first + wantKeyLine(secondPublicKey, secondKeyID, 0), 0o600, "line 2 does not take over after the key before it"},
		{"a key twice", first + wantKeyLine(standInPublicKey, standInKeyID, 4), 0o600, "line 2 lists a key a second time"},
		{"too many keys", strings.Repeat(second, 65), 0o600, "it lists more than 64 keys"},
		{"too long", strings.Repeat("x", keysFileLimit+1), 0o600, fmt.Sprintf("it could not be read whole within %d bytes", keysFileLimit)},
		{"writable by its group", first, 0o620, "its group or other users can write it"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig := newAuditRig(t, withAuditVersions)
			rig.answers(t, 0, auditValidReport)
			s, ts, _ := gatesServer(t, rig.bin)
			row := createSignedDesk(t, s, ts, rig.calls, "a2000000000000000000000000000001")
			keys := filepath.Join(signingFolderOf(s), row.ID+".keys.jsonl")
			if os.WriteFile(keys, []byte(tc.data), 0o600) != nil || os.Chmod(keys, tc.mode) != nil {
				t.Fatal("could not write the list")
			}
			assertKeysUnread(t, ts, rig, row.ID, "Desk could not read the public keys it keeps for this desk: "+tc.why+".")
		})
	}
	for _, tc := range []struct {
		name  string
		setUp func(t *testing.T, keys string)
	}{
		{"a link", func(t *testing.T, keys string) {
			other := filepath.Join(t.TempDir(), "keys.jsonl")
			if os.WriteFile(other, []byte(first), 0o600) != nil || os.Remove(keys) != nil || os.Symlink(other, keys) != nil {
				t.Fatal("could not link the list")
			}
		}},
		{"a folder", func(t *testing.T, keys string) {
			if os.Remove(keys) != nil || os.Mkdir(keys, 0o700) != nil {
				t.Fatal("could not replace the list")
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig := newAuditRig(t, withAuditVersions)
			rig.answers(t, 0, auditValidReport)
			s, ts, _ := gatesServer(t, rig.bin)
			row := createSignedDesk(t, s, ts, rig.calls, "a3000000000000000000000000000001")
			tc.setUp(t, filepath.Join(signingFolderOf(s), row.ID+".keys.jsonl"))
			assertKeysUnread(t, ts, rig, row.ID, "Desk could not read the public keys it keeps for this desk: it is not a regular file.")
		})
	}
	// A signing folder others can write is not read either.
	rig := newAuditRig(t, withAuditVersions)
	rig.answers(t, 0, auditValidReport)
	s, ts, _ := gatesServer(t, rig.bin)
	row := createSignedDesk(t, s, ts, rig.calls, "a4000000000000000000000000000001")
	if err := os.Chmod(signingFolderOf(s), 0o770); err != nil {
		t.Fatal(err)
	}
	assertKeysUnread(t, ts, rig, row.ID, "Desk could not open the folder it keeps signing keys in: Desk's signing folder is writable by group or others (mode 0770); a directory anyone could write to may already hold something they put there, so narrowing it now would not make it safe — remove the 020 bits and restart the desk.")
	if perm := permOf(t, signingFolderOf(s)); perm != 0o770 {
		t.Errorf("reading the keys changed the folder to %v", perm)
	}
}

// assertKeysUnread checks a panel whose keys Desk could not read: the
// runtime's report, run with no key, and why, in words with no path.
func assertKeysUnread(t *testing.T, ts *httptest.Server, rig *auditRig, desk, why string) {
	t.Helper()
	rig.ran(t)
	status, data := reviewCall(t, ts, "GET", "/api/audit/verify", desk, nil, bearer)
	var answer auditAnswer
	if status != http.StatusOK || json.Unmarshal(data, &answer) != nil || answer.State != auditStateReport {
		t.Fatalf("the panel answered %d %s", status, data)
	}
	if answer.Keys == nil || answer.Keys.State != keysUnread || answer.Keys.Problem != why || answer.Keys.Public != nil {
		t.Errorf("the panel shows the keys %+v, want unread: %q", answer.Keys, why)
	}
	if calls := rig.ran(t); !slices.Equal(calls, []string{schemaCall, validateCall, verifyCall}) {
		t.Errorf("the panel ran %q, want no key passed", calls)
	}
	if seen := rig.keysSeen(t); seen != nil {
		t.Errorf("the runtime was given %q", seen)
	}
	if strings.Contains(string(data), "/tmp/") || strings.Contains(string(data), "secrets/") {
		t.Errorf("the panel names a path: %s", data)
	}
}

// **Where Desk keeps no key, none is passed, and the panel says which.** A
// desk Desk made with neither a key nor a list of keys, or no signing folder
// at all, is "none"; the startup desk is "startup". No key is passed to either, and
// nothing is made in Desk's custody to find out.
func TestWhereDeskKeepsNoKeyNoneIsPassed(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	rig := newAuditRig(t, withAuditVersions)
	rig.answers(t, 0, auditValidReport)
	s, ts, _ := gatesServer(t, rig.bin)
	writeProject(t, s.projectDir, map[string]string{"jpack.json": auditedConfig})
	row := createSignedDesk(t, s, ts, rig.calls, "a5000000000000000000000000000001")
	check := func(desk, state string) {
		t.Helper()
		rig.ran(t)
		status, answer, refusal := readAudit(t, ts, desk)
		if status != http.StatusOK || answer.Keys == nil || answer.Keys.State != state || answer.Keys.Public != nil || answer.Keys.Problem != "" {
			t.Errorf("desk %q answered %d %+v %q, want keys %q", desk, status, answer.Keys, refusal, state)
		}
		if calls := rig.ran(t); !slices.Equal(calls, []string{schemaCall, validateCall, verifyCall}) {
			t.Errorf("desk %q ran %q, want no key passed", desk, calls)
		}
	}
	check("", keysStartup)
	for _, name := range []string{row.ID + ".keys.jsonl", row.ID + ".seed"} {
		removeNamed(t, signingFolderOf(s), name)
	}
	check(row.ID, keysNone)
	if err := os.RemoveAll(signingFolderOf(s)); err != nil {
		t.Fatal(err)
	}
	check(row.ID, keysNone)
	if _, err := os.Lstat(signingFolderOf(s)); !os.IsNotExist(err) {
		t.Errorf("reading the keys made a signing folder: %v", err)
	}
}

// The runtime's `packs validate --format json` answers, measured on 0.26.0
// over a desk whose key it accepts, and over one whose seed its group can
// read (exit 1). %s is the seed's path.
const (
	validatedSigned  = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.26.0"},"command":"packs validate","status":"valid","kind":"non-normative-runtime-convention","configPath":"jpack.json","configVersion":"6","summary":{"total":0,"passed":0,"failed":0},"checks":[{"name":"audit-dir-inside-root","status":"passed"},{"name":"audit-signing-key","status":"passed","detail":"Chained records are signed with key 4ba1de706a3baa4d8f5456340604190a, named by the audit member's signingKey."}],"packs":[]}`
	validatedRefused = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.26.0"},"command":"packs validate","status":"invalid","kind":"non-normative-runtime-convention","configPath":"jpack.json","configVersion":"6","summary":{"total":0,"passed":0,"failed":0},"checks":[{"name":"audit-dir-inside-root","status":"passed"},{"name":"audit-signing-key","status":"failed","detail":"The signing key the audit member's signingKey names, %s, is refused, so records are written unsigned: the signing key can be read or written by its group or by other users."}],"packs":[]}`
)

// **The runtime's word on the key, as it said it.** `packs validate`'s
// `audit-signing-key` check is shown with its status and its sentence, with
// Desk's folders named by name and an inherited key's path by "…"; no check
// is no key named; a refusal is the runtime's, in its words; and an answer the
// runtime does not document is said as one. Where the project declares packs,
// the first is named, so one pack's report is read.
func TestThePanelShowsTheRuntimesWordOnTheKey(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	rig := newAuditRig(t, withAuditVersions)
	rig.answers(t, 0, auditValidReport)
	s, ts, _ := gatesServer(t, rig.bin)
	const id = "a6000000000000000000000000000001"
	row := createSignedDesk(t, s, ts, rig.calls, id)
	seed := filepath.Join(signingFolderOf(s), id+".seed")
	edit := func(body string, change func(map[string]any)) string {
		return edited(t, body, change)
	}
	twice := edit(validatedSigned, func(value map[string]any) {
		checks := value["checks"].([]any)
		value["checks"] = append(checks, checks[1])
	})
	for _, tc := range []struct {
		name string
		code int
		body string
		want auditSigning
	}{
		{"passed", 0, validatedSigned, auditSigning{State: signingChecked, Status: "passed", Detail: "Chained records are signed with key 4ba1de706a3baa4d8f5456340604190a, named by the audit member's signingKey."}},
		{"failed", 1, strings.Replace(validatedRefused, "%s", seed, 1), auditSigning{State: signingChecked, Status: "failed", Detail: "The signing key the audit member's signingKey names, …, is refused, so records are written unsigned: the signing key can be read or written by its group or by other users."}},
		{"skipped", 0, strings.Replace(validatedSigned, `"status":"passed","detail":"Chained`, `"status":"skipped","detail":"Chained`, 1), auditSigning{State: signingChecked, Status: "skipped", Detail: "Chained records are signed with key 4ba1de706a3baa4d8f5456340604190a, named by the audit member's signingKey."}},
		{"no key named", 0, standInValidated, auditSigning{State: signingNoKey}},
		{"refused", 2, auditBadVersion, auditSigning{State: signingUnread, Diagnostics: []runtimeDiagnostic{{Code: "JPS-PROJECT-CONFIG-VERSION", Message: `The project configuration jpack.json declares configVersion "99", which this runtime does not support. It accepts: 1, 2, 3, 4, 5, 6. This configuration comes from a newer toolchain: upgrade the runtime. Do not edit the declaration to an older version — that discards what this configuration declares.`}}}},
		{"failed under valid", 0, strings.Replace(strings.Replace(validatedRefused, "%s", seed, 1), `"status":"invalid"`, `"status":"valid"`, 1), auditSigning{State: signingUnread, Problem: "Its packs validate did not answer as documented."}},
		{"invalid on exit 0", 0, strings.Replace(validatedSigned, `"status":"valid"`, `"status":"invalid"`, 1), auditSigning{State: signingUnread, Problem: "Its packs validate did not answer as documented."}},
		{"the check twice", 0, twice, auditSigning{State: signingUnread, Problem: "Its packs validate did not answer as documented."}},
		{"a status no check has", 0, strings.Replace(validatedSigned, `"status":"passed","detail":"Chained`, `"status":"maybe","detail":"Chained`, 1), auditSigning{State: signingUnread, Problem: "Its packs validate did not answer as documented."}},
		{"another command", 0, strings.Replace(validatedSigned, `"command":"packs validate"`, `"command":"packs verify"`, 1), auditSigning{State: signingUnread, Problem: "Its packs validate did not answer as documented."}},
		{"a refusal on exit 0", 0, auditBadVersion, auditSigning{State: signingUnread, Problem: "Its packs validate did not answer as documented."}},
		{"not JSON", 1, "not json", auditSigning{State: signingUnread, Problem: "Its packs validate did not answer as documented."}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			validatingAs(t, rig.calls, tc.code, tc.body)
			status, data := reviewCall(t, ts, "GET", "/api/audit/verify", row.ID, nil, bearer)
			var answer auditAnswer
			if status != http.StatusOK || json.Unmarshal(data, &answer) != nil || answer.State != auditStateReport {
				t.Fatalf("the panel answered %d %s", status, data)
			}
			if got := answer.Signing; got == nil || mustJSON(*got) != mustJSON(tc.want) {
				t.Errorf("the panel shows %s, want %s", mustJSON(answer.Signing), mustJSON(tc.want))
			}
			if strings.Contains(string(data), s.configDir) {
				t.Errorf("the panel names a path: %s", data)
			}
		})
	}

	// One pack's report: the first the configuration declares.
	validatingAs(t, rig.calls, 0, validatedSigned)
	config := strings.Replace(wantSignedConfig(s.configDir, id), `"packs":{}`, `"packs":{"beta":{"path":"packs/b.json"},"alpha":{"path":"packs/a.json"}}`, 1)
	writeProject(t, row.Folder, map[string]string{"jpack.json": config})
	rig.ran(t)
	reportOf(t, ts, row.ID)
	if calls := rig.ran(t); len(calls) != 4 || calls[1] != "packs validate --config jpack.json --format json --id alpha [JPACK_CONFIG=unset]" {
		t.Errorf("the panel ran %q, want packs validate of the first pack declared", calls)
	}
}

// **An inherited key's path is never said.** On the startup desk, where the
// runtime names the key JPACK_SIGNING_KEY names by its path, the panel says
// "…", whatever the path holds.
func TestThePanelNeverSaysAnInheritedKeysPath(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	key := filepath.Join(t.TempDir(), "Owner SECRET (keys)", "owner.seed")
	t.Setenv("JPACK_SIGNING_KEY", key)
	ts, rig, _ := auditDesk(t, withAuditVersions, auditedConfig)
	rig.answers(t, 0, auditValidReport)
	validatingAs(t, rig.calls, 1, strings.Replace(strings.Replace(validatedRefused, "%s", key, 1), "the audit member's signingKey", "JPACK_SIGNING_KEY", 1))
	status, data := reviewCall(t, ts, "GET", "/api/audit/verify", "", nil, bearer)
	var answer auditAnswer
	if status != http.StatusOK || json.Unmarshal(data, &answer) != nil || answer.Signing == nil {
		t.Fatalf("the panel answered %d %s", status, data)
	}
	if want := "The signing key JPACK_SIGNING_KEY names, …, is refused, so records are written unsigned: the signing key can be read or written by its group or by other users."; answer.Signing.Detail != want {
		t.Errorf("the panel says %q, want %q", answer.Signing.Detail, want)
	}
	if strings.Contains(string(data), "SECRET") {
		t.Errorf("the panel names the key's path: %s", data)
	}
}

// **The report's word on the signatures is shown as the runtime gave it**,
// and one missing a member, or holding a negative count, is not a report.
func TestTheSignaturesTheRuntimeReadAreShown(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	ts, rig, _ := auditDesk(t, withAuditVersions, auditedConfig)
	signatures := map[string]any{"lines": 3, "unreadable": 1, "rotations": 0, "keysSupplied": 1, "revocations": 0, "firstKey": standInKeyID, "keyInForce": standInKeyID}
	rig.answers(t, 0, edited(t, auditValidReport, func(value map[string]any) { value["signatures"] = signatures }))
	want := auditSignatures{Lines: 3, Unreadable: 1, KeysSupplied: 1, FirstKey: standInKeyID, KeyInForce: standInKeyID}
	if report := reportOf(t, ts, ""); report.Signatures == nil || *report.Signatures != want {
		t.Errorf("the signatures are shown as %+v", report.Signatures)
	}
	for _, change := range []func(map[string]any){
		func(s map[string]any) { delete(s, "keyInForce") },
		func(s map[string]any) { s["unreadable"] = -1 },
		func(s map[string]any) { s["firstKey"] = "" },
	} {
		broken := map[string]any{}
		for k, v := range signatures {
			broken[k] = v
		}
		change(broken)
		rig.answers(t, 0, edited(t, auditValidReport, func(value map[string]any) { value["signatures"] = broken }))
		if status, answer, _ := readAudit(t, ts, ""); status != http.StatusInternalServerError || answer.State != "" {
			t.Errorf("signatures %v were shown: %d %+v", broken, status, answer)
		}
	}
}

// The reasons runtime #222 adds for a key it refuses, each naming a
// directory on the key's path (%s), and the refusal `audit key generate`
// gives with one, exit 1 (%s the seed, %s the reason). Spelled as that pull
// request words them; no released runtime prints them yet.
const (
	reasonDirectoryWritable = "the directory %s on the signing key's path can be written by its group or by other users (mode 0777) and has no sticky bit, so another user could remove or replace the key; chmod go-w %s fixes it"
	reasonDirectoryOwner    = "the directory %s on the signing key's path is owned by uid 4242, neither root nor the user this runtime runs as (uid 1000)"
	generateRefusedWith     = "No seed was written to %s, where a signing key is refused: %s"
)

// jsonString is value as a JSON string literal, built here by hand: only
// what these tests put in a path is escaped.
func jsonString(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\t", `\t`, "\u2028", `\u2028`).Replace(value) + `"`
}

// **No part of a path the runtime names on the key's way is passed on.**
// Desk's configuration folder here has a space, a tab and a line separator
// in its name, so the general rule, which ends a path at its first space,
// would leave the rest of it. The runtime names the seed, and from runtime
// #222 a directory on its path, as it prints them (runtimePrints, the
// oracle), and Desk's own errors name them as Go writes them. Neither
// reaches the panel's word on the key, a refusal to generate one, or a
// creation's answer.
func TestNoPartOfTheKeysPathIsPassedOn(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	root := t.TempDir()
	base := filepath.Join(root, "Home DIR (owner)")
	config := filepath.Join(base, "Owner SECRET\tKEYS\u2028TAIL dir")
	if os.Mkdir(base, 0o700) != nil || os.Mkdir(config, 0o700) != nil {
		t.Skip("this file system refuses the names")
	}
	const id = "a7000000000000000000000000000001"
	seed := filepath.Join(config, "secrets", "signing", id+".seed")
	leaks := func(t *testing.T, what, said string) {
		t.Helper()
		for _, part := range []string{"SECRET", "TAIL", "KEYS", "DIR", "(owner)", root, runtimePrints(config)} {
			if strings.Contains(said, part) {
				t.Errorf("%s names %q: %s", what, part, said)
			}
		}
	}
	server := func(t *testing.T) (*auditRig, *Server, *httptest.Server) {
		t.Helper()
		rig := newAuditRig(t, withAuditVersions)
		rig.answers(t, 0, auditValidReport)
		s, ts := startDesk(t, Config{ProjectDir: t.TempDir(), JpackBin: rig.bin, Token: testToken, DeskConfigDir: config, Logger: log.New(io.Discard, "", 0)})
		t.Cleanup(func() { s.Close() })
		t.Cleanup(ts.Close)
		return rig, s, ts
	}

	// A refusal to generate, in the runtime's words, at exit 1, naming the
	// seed and a directory on its path as the runtime prints them; and one
	// with no words, which Desk's own error names as Go writes it.
	for _, tc := range []struct{ name, generate, why string }{
		{"JPS-AUDIT-KEY-REFUSED", "  printf '%s\\n' " + shellQuote(`{"outputVersion":"2","command":"audit key generate","status":"error","diagnostics":[{"code":"JPS-AUDIT-KEY-REFUSED","message":`+
			jsonString(fmt.Sprintf(generateRefusedWith, runtimePrints(seed), fmt.Sprintf(reasonDirectoryWritable, runtimePrints(config), runtimePrints(filepath.Dir(config)))+"; a copy is at "+runtimePrints(filepath.Join(config, "SECRET-copy.seed"))))+`}]}`) + "\n  exit 1",
			"the runtime did not generate its signing key: No seed was written to …, where a signing key is refused: the directory … on the signing key's path can be written by its group or by other users (mode 0777) and has no sticky bit, so another user could remove or replace the key; chmod go-w … fixes it; a copy is at …."},
		{"a run with no words", "  exit 1",
			"the runtime did not generate its signing key: the runtime's audit key generate … --format json failed: exit status 1."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig, s, ts := server(t)
			generatingAs(t, rig.calls, tc.generate)
			fixDeskIDs(t, id)
			status, data := deskCall(t, ts, "POST", "/api/desks", "", `{"name":"Refused"}`, true)
			assertNoDesk(t, s, ts, status, data, tc.why)
			if refusalOf(data) != "The desk was not created: "+tc.why {
				t.Errorf("the refusal is %q, want %q", refusalOf(data), "The desk was not created: "+tc.why)
			}
			leaks(t, "the refusal", string(data))
			if names := liveIn(t, filepath.Join(config, "secrets", "signing")); len(names) != 0 {
				t.Errorf("a refused generation left %q", names)
			}
		})
	}

	// A lock the runtime refuses after the key was made, in words that name
	// the key and its folder: no desk, no key, and no part of the path.
	t.Run("a lock refused in words that name the key", func(t *testing.T) {
		rig, s, ts := server(t)
		const lockedID = "a7000000000000000000000000000003"
		lockedSeed := filepath.Join(config, "secrets", "signing", lockedID+".seed")
		fixDeskIDs(t, lockedID)
		refusal := `{"outputVersion":"2","command":"packs lock","status":"unsupported","diagnostics":[{"code":"JPS-PROJECT-CONFIG-SCHEMA","message":` +
			jsonString("The key "+runtimePrints(lockedSeed)+" is refused under "+runtimePrints(config)+".") + `}]}`
		if err := os.WriteFile(rig.calls+".lock-"+lockedID, []byte("{}\n"+refusal+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		status, data := deskCall(t, ts, "POST", "/api/desks", "", `{"name":"Unlocked"}`, true)
		why := "the runtime did not lock it: The key … is refused under …."
		assertNoDesk(t, s, ts, status, data, why)
		if refusalOf(data) != "The desk was not created: "+why+" "+keyArchivedWords {
			t.Errorf("the refusal is %q", refusalOf(data))
		}
		leaks(t, "the refusal", string(data))
		if names := liveIn(t, filepath.Join(config, "secrets", "signing")); len(names) != 0 {
			t.Errorf("a refused lock left %q", names)
		}
		// Archived with a sentence that names no part of the path.
		if got := archivedIn(t, filepath.Join(config, "secrets", "signing"), lockedID); !slices.Equal(got, kindsArchived(archiveCreationStopped, "keys.jsonl", "seed", "creating")) {
			t.Errorf("a refused lock archived %q", got)
		}
		journal := readFile(t, filepath.Join(config, "secrets", "signing", archiveDirName, lockedID, archiveJournalName))
		leaks(t, "the archive's journal", journal)
	})

	// A desk made unsigned, because the signing folder is a link: its
	// answer names the folder by words.
	t.Run("a creation that keeps no key", func(t *testing.T) {
		_, s, ts := server(t)
		signing := filepath.Join(config, "secrets", "signing")
		// The test's own folder, with what earlier cases archived in it.
		if err := os.RemoveAll(signing); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(t.TempDir(), signing); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Remove(signing) })
		fixDeskIDs(t, "a7000000000000000000000000000002")
		status, data := deskCall(t, ts, "POST", "/api/desks", "", `{"name":"Unsigned"}`, true)
		var row createdDesk
		if status != http.StatusCreated || json.Unmarshal(data, &row) != nil || row.Signed {
			t.Fatalf("create: %d %s", status, data)
		}
		if want := wantUnsignedByCustody("Desk's signing folder is a symbolic link; a key is not kept anywhere reached through one"); row.Notice != want {
			t.Errorf("the notice is %q, want %q", row.Notice, want)
		}
		leaks(t, "the creation's notice", row.Notice)
		if names := namesIn(t, filepath.Join(s.configDir, "desks", row.ID)); len(names) == 0 {
			t.Error("the unsigned desk was not made")
		}
	})

	// A desk made signed: its answer, and the panel's word on its key, where
	// the runtime refuses the key for a directory on its path.
	rig, _, ts := server(t)
	fixDeskIDs(t, id)
	signed := strings.Replace(wantSignedConfig(config, id), `"signingKey":"`+config+"/secrets/signing/"+id+`.seed"`, `"signingKey":`+jsonString(seed), 1)
	digest := "sha256:" + digestOf([]byte(signed))
	if err := os.WriteFile(rig.calls+".lock-"+id, []byte(`{"lockVersion":"1","config":{"digest":"`+digest+`"}}`+"\n"+lockAnswer(digest)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, data := deskCall(t, ts, "POST", "/api/desks", "", `{"name":"Signed"}`, true)
	var row createdDesk
	if status != http.StatusCreated || json.Unmarshal(data, &row) != nil || !row.Signed || row.ConfigVersion != "6" || row.Notice != "" {
		t.Fatalf("create: %d %s", status, data)
	}
	if got := readFile(t, filepath.Join(row.Folder, "jpack.json")); got != signed {
		t.Errorf("jpack.json is %q, want %q", got, signed)
	}
	// The answer's folder is the desk's record, which the desk list shows
	// its owner too; every word in it is free of the path.
	leaks(t, "the creation's words", row.Notice)

	refused := fmt.Sprintf("The signing key the audit member's signingKey names, %s, is refused, so records are written unsigned: %s.", runtimePrints(seed), fmt.Sprintf(reasonDirectoryOwner, runtimePrints(filepath.Join(config, "secrets"))))
	validatingAs(t, rig.calls, 1, `{"outputVersion":"2","command":"packs validate","status":"invalid","checks":[{"name":"audit-signing-key","status":"failed","detail":`+jsonString(refused)+`}],"packs":[]}`)
	status, data = reviewCall(t, ts, "GET", "/api/audit/verify", row.ID, nil, bearer)
	var answer auditAnswer
	if status != http.StatusOK || json.Unmarshal(data, &answer) != nil || answer.Signing == nil {
		t.Fatalf("the panel answered %d %s", status, data)
	}
	if want := "The signing key the audit member's signingKey names, …, is refused, so records are written unsigned: the directory … on the signing key's path is owned by uid 4242, neither root nor the user this runtime runs as (uid 1000)."; answer.Signing.Detail != want {
		t.Errorf("the panel says %q, want %q", answer.Signing.Detail, want)
	}
	leaks(t, "the panel", string(data))
}

// shellQuote is value in single quotes, for a shell.
func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'" }

// runtimeChecksKeyFolders reports whether bin refuses to generate a key in
// a folder other users can write, as runtime #222 (0.27.0) does and 0.26.0
// does not. It asks in a folder of the test's own.
func runtimeChecksKeyFolders(t *testing.T, bin string) bool {
	t.Helper()
	open := filepath.Join(t.TempDir(), "open")
	if os.Mkdir(open, 0o700) != nil || os.Chmod(open, 0o777) != nil {
		t.Fatal("could not make the folder")
	}
	cmd := exec.Command(bin, "audit", "key", "generate", filepath.Join(open, "probe.seed"), "--format", "json")
	cmd.Env = append(os.Environ(), "JPACK_CONFIG=", "JPACK_SIGNING_KEY=")
	out, _ := cmd.Output()
	return strings.Contains(string(out), `"JPS-AUDIT-KEY-REFUSED"`)
}

// **With a runtime that checks the folders on a key's path** (runtime #222,
// 0.27.0): Desk's placement, a chain of the user's own 0700 folders under
// ancestors the system owns, is accepted, and the desk is signed. A signing
// folder made writable by others after that is refused, and the panel shows
// the runtime's words with no part of any path; Desk, which holds the same
// rule, keeps no key there either, and says so. A folder made so between
// custody's check and the runtime's `audit key generate` is refused at exit
// 1, with JPS-AUDIT-KEY-REFUSED: no desk is made, and no seed or list of keys
// is left. Desk's configuration folder has a space, a tab and a line
// separator in its name throughout.
func TestTheRuntimesRefusalOfAKeysFolderWithTheRuntime(t *testing.T) {
	bin := requireBinary(t)
	t.Setenv("JPACK_CONFIG", "")
	t.Setenv("JPACK_SIGNING_KEY", "")
	if !runtimeChecksKeyFolders(t, bin) {
		t.Skip("this runtime does not check the folders on a key's path")
	}
	desk := func(t *testing.T) (string, string, *Server, *httptest.Server) {
		t.Helper()
		root := t.TempDir()
		config := filepath.Join(root, "Owner SECRET\tKEYS\u2028TAIL dir")
		if err := os.Mkdir(config, 0o700); err != nil {
			t.Skipf("this file system refuses the name: %v", err)
		}
		s, ts := startDesk(t, Config{ProjectDir: t.TempDir(), JpackBin: bin, Token: testToken, DeskConfigDir: config, Logger: log.New(io.Discard, "", 0)})
		t.Cleanup(func() { s.Close() })
		t.Cleanup(ts.Close)
		return root, config, s, ts
	}
	leaks := func(t *testing.T, root, what, said string) {
		t.Helper()
		for _, part := range []string{"SECRET", "TAIL", "KEYS", root, "/tmp", "/home"} {
			if strings.Contains(said, part) {
				t.Errorf("%s names %q: %s", what, part, said)
			}
		}
	}

	t.Run("a folder made open after the desk", func(t *testing.T) {
		root, config, _, ts := desk(t)
		row := createGatedDesk(t, ts)
		if row.ConfigVersion != "6" || !row.Signed || row.Notice != "" {
			t.Fatalf("the desk was made %+v, want it signed at 6", row)
		}
		status, data := reviewCall(t, ts, "GET", "/api/audit/verify", row.ID, nil, bearer)
		var answer auditAnswer
		if status != http.StatusOK || json.Unmarshal(data, &answer) != nil || answer.Signing == nil || answer.Signing.Status != "passed" {
			t.Fatalf("with Desk's placement the panel answered %d %s", status, data)
		}
		signing := filepath.Join(config, "secrets", "signing")
		if err := os.Chmod(signing, 0o777); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(signing, 0o700) })
		status, data = reviewCall(t, ts, "GET", "/api/audit/verify", row.ID, nil, bearer)
		answer = auditAnswer{}
		if status != http.StatusOK || json.Unmarshal(data, &answer) != nil || answer.Signing == nil {
			t.Fatalf("the panel answered %d %s", status, data)
		}
		if answer.Signing.State != signingChecked || answer.Signing.Status != "failed" ||
			!strings.HasPrefix(answer.Signing.Detail, "The signing key the audit member's signingKey names, …, is refused, so records are written unsigned: the directory … on the signing key's path can be written by its group or by other users (mode 0777)") {
			t.Errorf("the panel shows the key check %+v", answer.Signing)
		}
		if answer.Keys == nil || answer.Keys.State != keysUnread || !strings.HasPrefix(answer.Keys.Problem, "Desk could not open the folder it keeps signing keys in: Desk's signing folder is writable by group or others (mode 0777)") {
			t.Errorf("the panel shows the keys %+v", answer.Keys)
		}
		leaks(t, root, "the panel", string(data))
	})

	t.Run("a folder made open before the runtime generates the key", func(t *testing.T) {
		root, config, s, ts := desk(t)
		signing := filepath.Join(config, "secrets", "signing")
		testHookAfterCustodyCheck = func(path string) {
			if path == signing {
				os.Chmod(signing, 0o777)
			}
		}
		t.Cleanup(func() { testHookAfterCustodyCheck = nil })
		status, data := deskCall(t, ts, "POST", "/api/desks", "", `{"name":"Refused"}`, true)
		assertNoDesk(t, s, ts, status, data, "the runtime did not generate its signing key: No seed was written to …, where a signing key is refused: the directory … on the signing key's path can be written by its group or by other users (mode 0777)")
		leaks(t, root, "the refusal", string(data))
		if names := namesIn(t, signing); len(names) != 0 {
			t.Errorf("a refused generation left %q", names)
		}
	})
}

// gatedConfigFor is the configuration a creation on s that answered row
// should have written.
func gatedConfigFor(t *testing.T, s *Server, row createdDesk) string {
	t.Helper()
	want := map[string]string{"6": wantSignedConfig(s.configDir, row.ID), "5": wantGatedConfig, "4": wantGatedConfigV4}[row.ConfigVersion]
	if want == "" || row.Signed != (row.ConfigVersion == "6") {
		t.Fatalf("the creation answered configVersion %q, signed %v", row.ConfigVersion, row.Signed)
	}
	return want
}

// **With the runtime: a desk made signed, a record its key signed, and the
// runtime's own check agreeing.** With a runtime that reads "6", the desk is
// made at "6" naming a seed the runtime accepts; its list of keys holds the
// key `audit key public` prints for the seed; a deciding run is signed; the
// panel, with the desk's key, reports it signed by that key, and `packs
// validate` says the key signs; and the runtime's own `audit verify
// --public-key`, run apart from Desk, agrees. With an older runtime, the
// desk is made at "5", unsigned, and says so.
func TestASignedDeskWithTheRuntime(t *testing.T) {
	bin := requireBinary(t)
	t.Setenv("JPACK_CONFIG", "")
	t.Setenv("JPACK_SIGNING_KEY", "")
	s, ts, _ := gatesServer(t, bin)
	row := createGatedDesk(t, ts)
	var schema struct {
		Tool struct {
			Version string `json:"version"`
		} `json:"tool"`
		Supported []string `json:"supportedConfigVersions"`
	}
	if err := json.Unmarshal(jpackIn(t, bin, row.Folder, "packs", "schema", "--format", "json"), &schema); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(schema.Supported, "6") {
		want := "This desk is not signed: a desk names its signing key at configVersion 6, and the runtime this Desk runs (jpack " + schema.Tool.Version + ") does not read it. A runtime of 0.26.0 or later creates desks signed."
		if row.ConfigVersion != "5" || row.Signed || row.Notice != want {
			t.Errorf("runtime %s made %+v, want it at 5, unsigned, saying %q", schema.Tool.Version, row, want)
		}
		assertGatedFolder(t, row.Folder, wantGatedConfig)
		if names := namesIn(t, signingFolderOf(s)); names != nil {
			t.Errorf("runtime %s had Desk keep %q", schema.Tool.Version, names)
		}
		return
	}
	if row.ConfigVersion != "6" || !row.Signed || row.Notice != "" {
		t.Fatalf("runtime %s made %+v, want it signed at 6", schema.Tool.Version, row)
	}
	assertGatedFolder(t, row.Folder, wantSignedConfig(s.configDir, row.ID))
	seed := filepath.Join(signingFolderOf(s), row.ID+".seed")
	if perm := permOf(t, seed); perm != 0o600 {
		t.Errorf("the seed is %v", perm)
	}
	if perm := permOf(t, signingFolderOf(s)); perm != 0o700 {
		t.Errorf("the signing folder is %v", perm)
	}
	var public struct {
		PublicKey string `json:"publicKey"`
		KeyID     string `json:"keyId"`
	}
	if err := json.Unmarshal(jpackIn(t, bin, row.Folder, "audit", "key", "public", seed, "--format", "json"), &public); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(signingFolderOf(s), row.ID+".keys.jsonl")); got != wantKeyLine(public.PublicKey, public.KeyID, 0) {
		t.Errorf("the list of keys is %q, want the key the runtime reads from the seed, %s", got, public.PublicKey)
	}
	jpackIn(t, bin, row.Folder, "packs", "verify", "--config", "jpack.json", "--format", "json")

	// A deciding run, as an outside caller makes one.
	config := strings.Replace(wantSignedConfig(s.configDir, row.ID), `"packs":{}`, `"packs":{"alpha":{"path":"packs/a.json"}}`, 1)
	writeProject(t, row.Folder, map[string]string{"packs/a.json": reviewPack, "jpack.json": config})
	jpackIn(t, bin, row.Folder, "packs", "lock", "--config", "jpack.json", "--format", "json")
	facts := filepath.Join(t.TempDir(), "facts.json")
	if err := os.WriteFile(facts, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	jpackIn(t, bin, row.Folder, "experimental", "evaluate", "--config", "jpack.json", "--pack-id", "alpha", "--facts", facts, "--format", "json")

	status, data := reviewCall(t, ts, "GET", "/api/audit/verify", row.ID, nil, bearer)
	var answer auditAnswer
	if status != http.StatusOK || json.Unmarshal(data, &answer) != nil || answer.Report == nil {
		t.Fatalf("the panel answered %d %s", status, data)
	}
	report := answer.Report
	if report.Status != "valid" || report.Coverage.Signed != (auditCoverageState{Status: "through", Through: 1}) || report.Coverage.SignedRecords != 1 ||
		report.Signatures == nil || report.Signatures.KeyInForce != public.KeyID || report.Signatures.FirstKey != public.KeyID || report.Signatures.KeysSupplied != 1 {
		t.Errorf("the panel reports %+v, signatures %+v; want the record signed by key %s", report, report.Signatures, public.KeyID)
	}
	if answer.Keys == nil || answer.Keys.State != keysKept || !slices.Equal(answer.Keys.Public, []deskPublicKey{{public.PublicKey, public.KeyID, 0, ""}}) {
		t.Errorf("the panel shows the keys %+v", answer.Keys)
	}
	wantCheck := auditSigning{State: signingChecked, Status: "passed", Detail: "Chained records are signed with key " + public.KeyID + ", named by the audit member's signingKey."}
	if answer.Signing == nil || mustJSON(*answer.Signing) != mustJSON(wantCheck) {
		t.Errorf("the panel shows the key check %+v, want %+v", answer.Signing, wantCheck)
	}
	if strings.Contains(string(data), s.configDir) {
		t.Errorf("the panel names a path: %s", data)
	}
	if names := namesIn(t, signingFolderOf(s)); !slices.Equal(names, []string{row.ID + ".keys.jsonl", row.ID + ".seed"}) {
		t.Errorf("the signing folder holds %q after the check", names)
	}

	// The runtime's own check, run apart from Desk, with the public key in a
	// file of the holder's.
	held := filepath.Join(t.TempDir(), "desk.pub")
	if err := os.WriteFile(held, []byte(public.PublicKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "audit", "verify", "--config", "jpack.json", "--format", "json", "--public-key", held)
	cmd.Dir = row.Folder
	cmd.Env = append(os.Environ(), "JPACK_CONFIG=")
	out, err := cmd.Output()
	var own struct {
		Status   string `json:"status"`
		Coverage struct {
			Signed        auditCoverageState `json:"signed"`
			SignedRecords int64              `json:"signedRecords"`
		} `json:"coverage"`
	}
	if err != nil || json.Unmarshal(out, &own) != nil || own.Status != "valid" || own.Coverage.Signed != (auditCoverageState{Status: "through", Through: 1}) || own.Coverage.SignedRecords != 1 {
		t.Errorf("the runtime's own audit verify answered %v %s", err, out)
	}
}
