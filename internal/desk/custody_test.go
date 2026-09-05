package desk

// Custody, tested against the filesystem rather than described.
//
// Every case here is an arrangement another local user could actually make in
// a configuration tree they can write to. The store is expected to refuse each
// one **at startup**, name the thing that is wrong, and leave the rest of the
// desk working — and the refusals are read off a live server, not off the
// validator in isolation, because what matters is that the endpoints answer
// them.

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// storeIn validates one arranged directory, exactly as the server does.
func storeIn(t *testing.T, dir string) *assistantStore {
	t.Helper()
	store := openAssistantStore(dir)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// refusedFor asserts a store was refused and returns its sentence.
func refusedFor(t *testing.T, store *assistantStore, about string) string {
	t.Helper()
	if store.usable() {
		t.Fatalf("the store accepted %s", about)
	}
	return store.problem.Error()
}

func TestCustodyRefusesASymlinkedConfigDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	base := t.TempDir()
	real := filepath.Join(base, "elsewhere")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	link := filepath.Join(base, "jpack-desk")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	// A link is not repaired into a directory. It is the arrangement an
	// attacker makes, and the answer is no.
	said := refusedFor(t, storeIn(t, link), "a symlinked configuration directory")
	if !strings.Contains(said, "symbolic link") {
		t.Errorf("the refusal does not name the link: %q", said)
	}
}

func TestCustodyRefusesASymlinkedSecretsDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	config := t.TempDir()
	elsewhere := t.TempDir()
	if err := os.Symlink(elsewhere, filepath.Join(config, secretsDirName)); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	said := refusedFor(t, storeIn(t, config), "a symlinked secrets directory")
	if !strings.Contains(said, "symbolic link") {
		t.Errorf("the refusal does not name the link: %q", said)
	}
}

func TestCustodyRefusesASymlinkedKeyFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	// **The case the whole custody argument is about.** A preplanted link at
	// the key's own name made the desk read a file of the attacker's choosing
	// and present it to an endpoint the same attacker named.
	config := t.TempDir()
	secret := filepath.Join(t.TempDir(), "somebody-elses-secret")
	if err := os.WriteFile(secret, []byte("not-this-desks-key-at-all"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	store := storeIn(t, config)
	if !store.usable() {
		t.Fatalf("the store refused a good directory: %v", store.problem)
	}
	// Planted after the store was pinned, which is the harder case: the
	// directory was valid when it was checked.
	if err := os.Symlink(secret, filepath.Join(config, secretsDirName, assistantKeyName)); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	key, err := store.readKey()
	if err == nil {
		t.Fatalf("the link was followed and answered %q", key)
	}
	if !strings.Contains(err.Error(), "symbolic link") {
		t.Errorf("the refusal does not name the link: %v", err)
	}
	if key != "" {
		t.Errorf("a refused read still produced %q", key)
	}
}

func TestCustodyRefusesAKeyFileAnybodyElseCanRead(t *testing.T) {
	config := t.TempDir()
	store := storeIn(t, config)
	if !store.usable() {
		t.Fatalf("refused a good directory: %v", store.problem)
	}
	if err := store.storeKey(testKey); err != nil {
		t.Fatalf("store: %v", err)
	}
	path := filepath.Join(config, secretsDirName, assistantKeyName)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	// A key file the rest of the machine can read is not treated as this
	// desk's key, whatever is in it.
	if _, err := store.readKey(); err == nil {
		t.Error("a world-readable key file was read as the key")
	}
}

func TestCustodyRefusesANonRegularKey(t *testing.T) {
	config := t.TempDir()
	store := storeIn(t, config)
	if !store.usable() {
		t.Fatalf("refused a good directory: %v", store.problem)
	}
	if err := os.Mkdir(filepath.Join(config, secretsDirName, assistantKeyName), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := store.readKey(); err == nil {
		t.Error("a directory was read as the key")
	}
}

func TestCustodyRefusesALooseAncestor(t *testing.T) {
	// A parent anybody may write to is a parent in which anybody may replace
	// the desk's own directory. It is not narrowed — it is not ours to narrow
	// — so it is refused, and the sentence names it rather than the directory
	// inside it, because that is the one that has to be fixed.
	base := t.TempDir()
	loose := filepath.Join(base, "loose")
	if err := os.Mkdir(loose, 0o777); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(loose, 0o777); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	said := refusedFor(t, storeIn(t, filepath.Join(loose, "jpack-desk")), "a loose parent")
	if !strings.Contains(said, "writable by group or others") {
		t.Errorf("the refusal does not name the mode: %q", said)
	}
	if !strings.Contains(said, loose) {
		t.Errorf("the refusal names the wrong directory: %q", said)
	}
}

func TestCustodyAdmitsAStickyAncestor(t *testing.T) {
	// The `/tmp` case, and the reason it is an exception rather than an
	// oversight: in a sticky directory only the owner of an entry may rename
	// or remove it, which is exactly the capability the loose-ancestor rule
	// exists to deny.
	if runtime.GOOS == "windows" {
		t.Skip("no sticky bit on Windows")
	}
	base := t.TempDir()
	sticky := filepath.Join(base, "sticky")
	if err := os.Mkdir(sticky, 0o777); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(sticky, os.FileMode(0o777)|os.ModeSticky); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	store := storeIn(t, filepath.Join(sticky, "jpack-desk"))
	if !store.usable() {
		t.Fatalf("a sticky ancestor was refused: %v", store.problem)
	}
}

func TestCustodyRefusesADirectoryOwnedBySomebodyElse(t *testing.T) {
	// Ownership cannot be arranged in a test without a second account, so the
	// *comparison* is arranged instead: this desk believes it is a different
	// user than the one who owns the directory it was given, which is the
	// state an attacker's preplanted directory produces.
	restore := effectiveUser
	effectiveUser = func() uint32 { return restore() + 1 }
	t.Cleanup(func() { effectiveUser = restore })

	said := refusedFor(t, storeIn(t, filepath.Join(t.TempDir(), "jpack-desk")),
		"a directory owned by another user")
	if !strings.Contains(said, "owned by user") {
		t.Errorf("the refusal does not name the owner: %q", said)
	}
}

func TestCustodyNarrowsItsOwnDirectories(t *testing.T) {
	// Ours and too loose is repaired rather than refused: a desk that found
	// its own directory at 0755 — which a umask of 022 produces — would
	// otherwise be permanently unable to keep a key it could simply tighten.
	config := t.TempDir()
	if err := os.Chmod(config, 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	if err := os.Mkdir(filepath.Join(config, secretsDirName), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	store := storeIn(t, config)
	if !store.usable() {
		t.Fatalf("refused: %v", store.problem)
	}
	for _, path := range []string{config, filepath.Join(config, secretsDirName)} {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if info.Mode().Perm() != custodyDirMode {
			t.Errorf("%s is %#o, want %#o", path, info.Mode().Perm(), custodyDirMode)
		}
	}
}

func TestCustodyHoldsTheDirectoryItPinnedAcrossASwap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("rename-over-open-directory semantics differ on Windows")
	}
	// **What pinning buys, shown rather than asserted.** After the store is
	// open, the `secrets` *name* is moved aside and a symlink to an
	// attacker-controlled directory is put in its place. Every subsequent
	// operation still lands in the directory that was validated, because the
	// authority is a descriptor and not a name.
	config := t.TempDir()
	store := storeIn(t, config)
	if !store.usable() {
		t.Fatalf("refused: %v", store.problem)
	}
	real := filepath.Join(config, secretsDirName)
	moved := filepath.Join(config, "moved-aside")
	if err := os.Rename(real, moved); err != nil {
		t.Fatalf("rename: %v", err)
	}
	attacker := t.TempDir()
	if err := os.Symlink(attacker, real); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if err := store.storeKey(testKey); err != nil {
		t.Fatalf("store after the swap: %v", err)
	}
	// The key landed in the directory that was pinned, and nothing at all was
	// written where the swapped-in name points.
	if _, err := os.Lstat(filepath.Join(moved, assistantKeyName)); err != nil {
		t.Errorf("the key did not land in the pinned directory: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(attacker, assistantKeyName)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a key was written into the swapped-in directory: %v", err)
	}
}

func TestCustodyFiresItsHookBetweenTheCheckAndTheOpen(t *testing.T) {
	// The hook exists so a time-of-check / time-of-use swap can be performed
	// at the one instant where it would matter. This asserts it is reached,
	// so the ordering argument is not resting on reading the code.
	var seen []string
	restore := testHookAfterCustodyCheck
	testHookAfterCustodyCheck = func(path string) { seen = append(seen, filepath.Base(path)) }
	t.Cleanup(func() { testHookAfterCustodyCheck = restore })

	config := filepath.Join(t.TempDir(), "jpack-desk")
	if !storeIn(t, config).usable() {
		t.Fatal("refused a good directory")
	}
	if len(seen) != 2 || seen[0] != "jpack-desk" || seen[1] != secretsDirName {
		t.Errorf("the hook saw %v, want the two directories in order", seen)
	}
}

func TestAnUnusableStoreLeavesTheRestOfTheDeskAlone(t *testing.T) {
	// A desk whose configuration directory is not safe to keep a credential in
	// still serves the project. What is withdrawn is the key, and the refusal
	// says which directory is the reason so it can be repaired.
	base := t.TempDir()
	loose := filepath.Join(base, "loose")
	if err := os.Mkdir(loose, 0o777); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(loose, 0o777); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	server, ts, _ := assistantServerIn(t, filepath.Join(loose, "jpack-desk"))

	for _, call := range []struct{ method, path string }{
		{http.MethodGet, "/api/desk-config"},
		{http.MethodGet, "/api/assistant/key"},
		{http.MethodDelete, "/api/assistant/key"},
		{http.MethodPost, "/api/assistant/probe"},
	} {
		status, body := sendJSON(t, ts, call.method, call.path, nil)
		if status != http.StatusConflict {
			t.Errorf("%s %s answered %d, want 409", call.method, call.path, status)
		}
		if body["code"] != CodeAssistantUnusableStore {
			t.Errorf("%s %s: code %v", call.method, call.path, body["code"])
		}
		if message, _ := body["error"].(string); !strings.Contains(message, loose) {
			t.Errorf("%s %s does not name the directory: %v", call.method, call.path, message)
		}
	}
	// A store is a store, and a refused one wrote nothing.
	if status, body := storeKey(t, ts, testKey); status != http.StatusConflict {
		t.Errorf("store answered %d: %v", status, body)
	}
	if _, err := os.Lstat(server.assistantKeyPath()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a refused store wrote a key: %v", err)
	}

	// And the project half of the desk is untouched.
	status, listing := getJSON(t, ts, "/api/files?token="+testToken)
	if status != http.StatusOK {
		t.Errorf("the file listing answered %d: %v", status, listing)
	}
}
