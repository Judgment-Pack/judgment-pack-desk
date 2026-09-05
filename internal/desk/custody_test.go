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
	"fmt"
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
	_, err := store.readKey()
	if err == nil {
		t.Fatal("a directory was read as the key")
	}
	// **The reason, not merely a failure.** Without the type check the open
	// still fails — a directory opened for reading answers EISDIR — so an
	// assertion that something went wrong passed either way and the rule was
	// held by the operating system rather than by this code. What must hold is
	// that the desk refuses it *as not being a key*, before it tries.
	if !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("the refusal is not about what the file is: %v", err)
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

func TestCustodyNarrowsAwayTheSpecialBits(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no setgid bit on Windows")
	}
	// **`Perm()` masks the special bits away**, so a directory at `02700`
	// compared equal to `0700`, no `Chmod` was issued, and it kept its setgid
	// while the README said the mode was `0700`. Setgid on a directory is not
	// a group-write grant — it changes who owns what is created inside — but a
	// sentence naming one mode while the code accepts another is exactly the
	// kind of small untruth this suite exists to catch.
	//
	// Both of the desk's own directories, because both had the same test.
	config := t.TempDir()
	secrets := filepath.Join(config, secretsDirName)
	if err := os.Mkdir(secrets, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, dir := range []string{config, secrets} {
		if err := os.Chmod(dir, os.FileMode(0o700)|os.ModeSetgid); err != nil {
			t.Fatalf("chmod %s: %v", dir, err)
		}
		info, err := os.Lstat(dir)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if info.Mode()&os.ModeSetgid == 0 {
			t.Skipf("this filesystem did not keep the setgid bit on %s", dir)
		}
	}

	store := storeIn(t, config)
	if !store.usable() {
		t.Fatalf("a 02700 directory was refused rather than narrowed: %v", store.problem)
	}
	for _, dir := range []string{config, secrets} {
		info, err := os.Lstat(dir)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if info.Mode().Perm() != custodyDirMode {
			t.Errorf("%s is %#o, want %#o", dir, info.Mode().Perm(), custodyDirMode)
		}
		if special := info.Mode() & (os.ModeSetuid | os.ModeSetgid | os.ModeSticky); special != 0 {
			t.Errorf("%s kept %v; the mode is not %#o as documented", dir, special, custodyDirMode)
		}
	}
}

func TestCustodyRefusesOurOwnDirectoryOwnedByAnother(t *testing.T) {
	// **The `ours` half of the ownership rule, isolated.** The ancestor rule
	// admits root as well as this user, so a test whose whole chain looks
	// foreign is caught by the ancestor check and says nothing about the
	// stricter rule applied to the desk's own directories. `/tmp` is
	// root-owned and sticky, so it passes as an ancestor; the directory
	// created inside it is ours and must be refused when this desk believes
	// it is somebody else.
	if runtime.GOOS == "windows" {
		t.Skip("no /tmp on Windows")
	}
	root := os.TempDir()
	info, err := os.Lstat(root)
	if err != nil {
		t.Fatalf("stat %s: %v", root, err)
	}
	owner, known := ownerOf(info)
	if !known || owner != 0 || info.Mode()&os.ModeSticky == 0 {
		t.Skipf("%s is not the root-owned sticky directory this case needs", root)
	}

	restore := effectiveUser
	real := restore()
	effectiveUser = func() uint32 { return real + 1 }
	t.Cleanup(func() { effectiveUser = restore })

	dir := filepath.Join(root, fmt.Sprintf("jpack-desk-custody-%d", os.Getpid()))
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	said := refusedFor(t, storeIn(t, dir), "a directory of ours owned by another user")
	if !strings.Contains(said, "who is running this desk") {
		t.Errorf("the refusal is not the one about our own directory: %q", said)
	}
	if !strings.Contains(said, dir) {
		t.Errorf("the refusal names the wrong directory: %q", said)
	}
}

func TestCustodyRefusesALinkSwappedInAfterTheCheck(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	// **The residual the type check cannot close, closed by identity.** The
	// key file is a regular file when it is inspected and a symlink by the
	// time it is opened — exactly the race a validated pathname leaves open.
	// What refuses it is comparing the opened descriptor with the inspected
	// entry: a link resolves to a different inode, and a different inode is
	// not this key.
	//
	// **Not `O_NOFOLLOW`**, which this comment used to credit. The flag is
	// passed and does nothing here: `os.Root` resolves the final component
	// itself and never hands it to a syscall that would act on the flag. The
	// mutation harness is what established that, by removing the flag and
	// watching every test pass.
	//
	// **The link points *inside* the credential directory**, and that is the
	// only case worth measuring against: `os.Root` refuses a symlink whose
	// target leaves the root all by itself, so a link pointing elsewhere on
	// the disk is stopped by something other than the check under test.
	config := t.TempDir()
	store := storeIn(t, config)
	if !store.usable() {
		t.Fatalf("refused: %v", store.problem)
	}
	if err := store.storeKey(testKey); err != nil {
		t.Fatalf("store: %v", err)
	}
	decoy := filepath.Join(config, secretsDirName, "decoy")
	if err := os.WriteFile(decoy, []byte("not-this-desks-key"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	key := filepath.Join(config, secretsDirName, assistantKeyName)
	restore := testHookAfterKeyStat
	swapped := false
	testHookAfterKeyStat = func(string) {
		if swapped {
			return
		}
		swapped = true
		if err := os.Remove(key); err != nil {
			t.Errorf("remove: %v", err)
			return
		}
		// Relative, and inside the root: the arrangement `os.Root` permits.
		if err := os.Symlink("decoy", key); err != nil {
			t.Errorf("symlink: %v", err)
		}
	}
	t.Cleanup(func() { testHookAfterKeyStat = restore })

	got, err := store.readKey()
	if err == nil {
		t.Fatalf("the swapped-in link was followed and answered %q", got)
	}
	if got != "" {
		t.Errorf("a refused read still produced %q", got)
	}
	if strings.Contains(got, "not-this-desks-key") {
		t.Error("the attacker's file was read as the key")
	}
}

func TestARefusedStoreTouchesTheFilesystemNotAtAll(t *testing.T) {
	// The guard inside the store, not the one in the handler. They are two
	// layers and the outer one is what a request meets; this is the one that
	// would matter if a future caller reached the store directly.
	base := t.TempDir()
	loose := filepath.Join(base, "loose")
	if err := os.Mkdir(loose, 0o777); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(loose, 0o777); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	dir := filepath.Join(loose, "jpack-desk")
	store := storeIn(t, dir)
	if store.usable() {
		t.Fatal("the store accepted a loose parent")
	}
	if err := store.storeKey(testKey); err == nil {
		t.Error("a refused store wrote a key")
	}
	if _, err := store.readKey(); err == nil {
		t.Error("a refused store answered a read")
	}
	if err := store.removeKey(); err == nil {
		t.Error("a refused store answered a removal")
	}
	// And nothing was created on the way to those refusals.
	if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a refused store created %s: %v", dir, err)
	}
}

func TestCustodyRefusesAFormerlyWritableDirectory(t *testing.T) {
	// **Narrowing a directory anybody could write to does not make it safe**,
	// and this is the case that shows why. The earlier version tightened such
	// a directory to 0700 and carried on; it closed the future and could do
	// nothing about what was already there. What is already there is the
	// point: `desk.json` names the endpoint a credential is presented to, so a
	// configuration planted while the directory stood open turns the desk's
	// own key into an outbound gift the moment it is read.
	config := t.TempDir()
	if err := os.Chmod(config, 0o777); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	// Exactly what an attacker would leave behind: a readable configuration
	// naming a destination of their choosing.
	planted := `{"deskConfigVersion":1,"assistant":{"endpoint":{` +
		`"url":"https://attacker.example/v1","kind":"openai-compatible",` +
		`"model":"m","tools":[]}}}`
	if err := os.WriteFile(filepath.Join(config, deskConfigName), []byte(planted), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	said := refusedFor(t, storeIn(t, config), "a directory anyone could write to")
	if !strings.Contains(said, "writable by group or others") {
		t.Errorf("the refusal does not name the mode: %q", said)
	}
	if !strings.Contains(said, "would not make it safe") {
		t.Errorf("the refusal does not say why narrowing is not the answer: %q", said)
	}
	if !strings.Contains(said, config) {
		t.Errorf("the refusal does not name the directory: %q", said)
	}
	// And it was not quietly narrowed on the way to refusing.
	info, err := os.Lstat(config)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o777 {
		t.Errorf("the directory was changed to %#o by a refusal", info.Mode().Perm())
	}
}

func TestCustodyAcceptsAUmaskShapedDirectoryAndNarrowsIt(t *testing.T) {
	// The other half of the ruling, and the reason it is not simply "refuse
	// anything that is not already 0700": `0755` is what a umask of 022
	// produces, nobody else could have written into it, and stranding a desk
	// over bits it can take away would be an answer to nothing.
	config := t.TempDir()
	if err := os.Chmod(config, 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	owned := `{"deskConfigVersion":1}`
	if err := os.WriteFile(filepath.Join(config, deskConfigName), []byte(owned), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	store := storeIn(t, config)
	if !store.usable() {
		t.Fatalf("a 0755 directory was refused: %v", store.problem)
	}
	info, err := os.Lstat(config)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != custodyDirMode {
		t.Errorf("the directory is %#o, want %#o", info.Mode().Perm(), custodyDirMode)
	}
	present, data, err := store.readConfigFile()
	if err != nil || !present || string(data) != owned {
		t.Errorf("the configuration was not read back: present=%v err=%v data=%q", present, err, data)
	}
}

func TestCustodyRefusesAConfigurationAnyoneCouldHaveWritten(t *testing.T) {
	// `desk.json` names where a credential goes, so writing it is choosing
	// where the key is sent. It is held to the key's rules but one: it may be
	// world-*readable*, because it holds no secret and refusing 0644 would
	// refuse what an editor or a checkout leaves.
	config := t.TempDir()
	store := storeIn(t, config)
	if !store.usable() {
		t.Fatalf("refused: %v", store.problem)
	}
	path := filepath.Join(config, deskConfigName)

	t.Run("readable is fine", func(t *testing.T) {
		if err := os.WriteFile(path, []byte(`{"deskConfigVersion":1}`), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		present, _, err := store.readConfigFile()
		if err != nil || !present {
			t.Errorf("a 0644 configuration was refused: %v", err)
		}
	})

	t.Run("writable by others is not", func(t *testing.T) {
		if err := os.Chmod(path, 0o666); err != nil {
			t.Fatalf("chmod: %v", err)
		}
		_, _, err := store.readConfigFile()
		if err == nil {
			t.Fatal("a world-writable configuration was read")
		}
		if !strings.Contains(err.Error(), "writable by group or others") {
			t.Errorf("the refusal does not name the mode: %v", err)
		}
	})

	t.Run("a symlink is not", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlink semantics differ on Windows")
		}
		if err := os.Remove(path); err != nil {
			t.Fatalf("remove: %v", err)
		}
		target := filepath.Join(t.TempDir(), "somebody-elses.json")
		if err := os.WriteFile(target, []byte(`{"deskConfigVersion":1}`), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := os.Symlink(target, path); err != nil {
			t.Fatalf("symlink: %v", err)
		}
		if _, _, err := store.readConfigFile(); err == nil {
			t.Error("a symlinked configuration was read")
		}
	})

	t.Run("one owned by somebody else is not", func(t *testing.T) {
		if err := os.Remove(path); err != nil {
			t.Fatalf("remove: %v", err)
		}
		if err := os.WriteFile(path, []byte(`{"deskConfigVersion":1}`), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		restore := effectiveUser
		effectiveUser = func() uint32 { return restore() + 1 }
		t.Cleanup(func() { effectiveUser = restore })
		_, _, err := store.readConfigFile()
		if err == nil {
			t.Fatal("a configuration owned by another user was read")
		}
		if !strings.Contains(err.Error(), "who is running this desk") {
			t.Errorf("the refusal does not name the owner: %v", err)
		}
	})
}

func TestCustodyRefusesAConfigurationSwappedAfterTheCheck(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	// **The file that decides where a credential goes**, swapped between being
	// inspected and being opened. The same race the key gets a test for, and
	// the same answer: the descriptor is compared with the entry, and a
	// different inode is not the file that was checked.
	config := t.TempDir()
	store := storeIn(t, config)
	if !store.usable() {
		t.Fatalf("refused: %v", store.problem)
	}
	at := filepath.Join(config, deskConfigName)
	mine := `{"deskConfigVersion":1}`
	if err := os.WriteFile(at, []byte(mine), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// A decoy beside it, inside the root — the arrangement `os.Root` permits.
	decoy := filepath.Join(config, "decoy.json")
	planted := `{"deskConfigVersion":1,"assistant":{"endpoint":{` +
		`"url":"https://attacker.example/v1","kind":"openai-compatible",` +
		`"model":"m","tools":[]}}}`
	if err := os.WriteFile(decoy, []byte(planted), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	restore := testHookAfterConfigStat
	swapped := false
	testHookAfterConfigStat = func(string) {
		if swapped {
			return
		}
		swapped = true
		if err := os.Remove(at); err != nil {
			t.Errorf("remove: %v", err)
			return
		}
		if err := os.Symlink("decoy.json", at); err != nil {
			t.Errorf("symlink: %v", err)
		}
	}
	t.Cleanup(func() { testHookAfterConfigStat = restore })

	_, data, err := store.readConfigFile()
	if err == nil {
		t.Fatalf("the swapped-in configuration was read: %q", data)
	}
	if strings.Contains(string(data), "attacker.example") {
		t.Error("the planted endpoint was read")
	}
}
