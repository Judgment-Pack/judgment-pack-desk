package desk

// Custody: the one directory this desk keeps a credential in, validated once
// and then held as a descriptor.
//
// # Why a pathname was not enough
//
// The first version of this stored the configuration directory as a string and
// reached the key through it — `os.ReadFile`, `MkdirAll`, `Chmod`, `Rename`.
// Every one of those follows symbolic links, and none of them looks at who
// owns the directory it lands in. In a configuration tree another local user
// can write to, that is enough to preplant a `secrets/assistant` symlink and
// have this process read a file of the attacker's choosing as the key — and
// then present it to an endpoint named by a `desk.json` the same attacker
// preplanted. The project's pinned `os.Root` is no help: it contains the
// *project*, and the credential is deliberately not in the project.
//
// So the credential directory gets the same treatment the project got, and one
// more check besides:
//
//   - **Every component is validated before anything is opened.** Each must be
//     a real directory rather than a symlink, and must not be writable by group
//     or other — because a directory anybody may write to is a directory in
//     which anybody may replace a name.
//   - **The desk's own two directories must be owned by the effective user**,
//     and are created or narrowed to 0700. Their ancestors — `/`, `/home`,
//     someone's home directory — are allowed to be owned by root as well,
//     because they are on every ordinary system and refusing them would refuse
//     every desk. That split is the whole of the ownership rule and is stated
//     here rather than left to be read out of the code.
//   - **The validated directory is then pinned**, and every read, write, rename
//     and mode change goes through that descriptor with no-follow semantics.
//     What was checked is what is opened.
//
// # What this does not defend against
//
// The same residual the file API states, and for the same reason: a hostile
// local process running **as this user** already owns these files and can
// replace them whenever it likes. Validation closes the window in which
// *another* user's writable directory redirects the open; it does not make the
// machine somebody else's problem. And on Unix `Root.Chmod` is documented as
// racing a regular-file-to-symlink swap, so the mode is set on a descriptor
// this process already holds rather than by name wherever that is possible.
//
// # When it fails
//
// The store refuses, once, with a sentence naming what was wrong — and the
// rest of the desk carries on. A desk whose `~/.config` is group-writable can
// still read packs, run rehearsals and walk graphs; what it cannot do is keep
// a credential, and Admin says exactly that rather than the desk failing to
// start or, worse, keeping one anyway.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// The modes the desk's own directories are held to, and the one the key file
// must have. 0700 and 0600: nothing for group, nothing for other.
const (
	custodyDirMode  = 0o700
	custodyFileMode = 0o600
)

// worldMode is the bits whose presence on any ancestor is disqualifying.
//
// Group and other **write**, and not read. A readable ancestor directory
// discloses that a `jpack-desk` directory exists, which is not a secret; a
// writable one lets its entries be replaced, which is the whole attack.
const worldMode fs.FileMode = 0o022

// assistantStore is the validated custody root, or the reason there is none.
//
// **`problem` is checked by every operation and is never nil-and-open.** A
// store that failed validation holds no descriptors at all, so there is no
// state in which a caller could reach the filesystem past a refusal.
type assistantStore struct {
	// dir is the desk-level directory, absolute, as a name for diagnostics
	// only. Nothing opens anything through it.
	dir string
	// root is pinned on dir; secrets is pinned on dir/secrets.
	root    *os.Root
	secrets *os.Root
	// problem is why there is no store, where there is none.
	problem error
}

// errNoConfigDir is the absence of any directory to validate.
var errNoConfigDir = errors.New(
	"this machine has no configuration directory: set XDG_CONFIG_HOME or HOME")

// testHookAfterCustodyCheck runs between validating a component and opening
// through it, and is nil outside tests.
//
// It exists so a test can perform exactly the swap a time-of-check /
// time-of-use attack would, at the instant where it would matter. Without it
// the ordering argument above would rest on reading the code and believing it.
var testHookAfterCustodyCheck func(path string)

func afterCustodyCheck(path string) {
	if testHookAfterCustodyCheck != nil {
		testHookAfterCustodyCheck(path)
	}
}

// testHookAfterKeyStat runs between establishing what the key file is and
// opening it, and is nil outside tests.
//
// The residual the type check cannot close: a swap performed *after* it. It is
// what proves `O_NOFOLLOW` is doing work rather than being decorative beside
// an `Lstat` that already refused the ordinary case.
var testHookAfterKeyStat func(path string)

func afterKeyStat(path string) {
	if testHookAfterKeyStat != nil {
		testHookAfterKeyStat(path)
	}
}

// testHookAfterConfigStat is testHookAfterKeyStat for the desk-level file.
var testHookAfterConfigStat func(path string)

func afterConfigStat(path string) {
	if testHookAfterConfigStat != nil {
		testHookAfterConfigStat(path)
	}
}

// openAssistantStore validates the chain and pins what it found.
//
// Called once, when the server is built. Everything it can repair — a missing
// directory, one of ours that is too loose — it repairs; everything else it
// refuses and reports.
func openAssistantStore(dir string) *assistantStore {
	if dir == "" {
		return &assistantStore{problem: errNoConfigDir}
	}
	if !filepath.IsAbs(dir) {
		return &assistantStore{dir: dir, problem: fmt.Errorf(
			"the configuration directory %s is not an absolute path", dir)}
	}
	if !custodyChecked {
		// Said plainly rather than skipped. A custody claim that cannot be
		// checked on this platform is a claim, and this package does not make
		// claims it does not hold.
		return &assistantStore{dir: dir, problem: errors.New(
			"this build cannot establish who owns a directory, so it will not keep a key")}
	}

	// The ancestors, from the filesystem root down to the parent of the desk's
	// own directory. Each must already exist: creating one would mean creating
	// somebody's home directory, which is not this program's business.
	parent := filepath.Dir(dir)
	if err := checkAncestors(parent); err != nil {
		return &assistantStore{dir: dir, problem: err}
	}

	// The desk's own directory: created if absent, narrowed if loose, and
	// required to be ours.
	if err := ensureOwnedDirectory(parent, filepath.Base(dir)); err != nil {
		return &assistantStore{dir: dir, problem: err}
	}
	afterCustodyCheck(dir)

	root, err := os.OpenRoot(dir)
	if err != nil {
		return &assistantStore{dir: dir, problem: fmt.Errorf(
			"the configuration directory %s could not be opened: %w", dir, err)}
	}
	// From here on the *descriptor* is the authority: `secrets` is created,
	// checked and opened relative to a directory that has already been
	// validated, so the name cannot be redirected out from under it.
	if err := ensureOwnedDirectoryIn(root, dir, secretsDirName); err != nil {
		root.Close()
		return &assistantStore{dir: dir, problem: err}
	}
	afterCustodyCheck(filepath.Join(dir, secretsDirName))

	secrets, err := root.OpenRoot(secretsDirName)
	if err != nil {
		root.Close()
		return &assistantStore{dir: dir, problem: fmt.Errorf(
			"the credential directory in %s could not be opened: %w", dir, err)}
	}
	return &assistantStore{dir: dir, root: root, secrets: secrets}
}

// Close releases the two descriptors. A refused store holds none.
func (s *assistantStore) Close() error {
	var err error
	if s.secrets != nil {
		err = s.secrets.Close()
	}
	if s.root != nil {
		if rerr := s.root.Close(); err == nil {
			err = rerr
		}
	}
	return err
}

// usable reports whether this store may be read or written at all.
func (s *assistantStore) usable() bool { return s.problem == nil }

// checkAncestors walks every component of an existing path and refuses the
// first one that is not a safe directory.
//
// Walked from the root downwards rather than upwards, so the refusal names the
// **outermost** thing that is wrong: told that `/home/someone/.config` is
// group-writable when `/home` is the loose one, a reader fixes the wrong
// directory.
func checkAncestors(path string) error {
	components := splitPath(path)
	walked := components[0]
	for index, component := range components {
		if index > 0 {
			walked = filepath.Join(walked, component)
		}
		info, err := os.Lstat(walked)
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%s does not exist, so there is nowhere to keep a key", walked)
		}
		if err != nil {
			return fmt.Errorf("%s could not be inspected: %w", walked, err)
		}
		if err := safeDirectory(walked, info, false); err != nil {
			return err
		}
	}
	return nil
}

// splitPath is an absolute path as its components, starting with the root.
func splitPath(path string) []string {
	cleaned := filepath.Clean(path)
	volume := filepath.VolumeName(cleaned)
	rest := strings.TrimPrefix(cleaned, volume)
	separator := string(filepath.Separator)
	parts := strings.Split(strings.TrimPrefix(rest, separator), separator)
	components := []string{volume + separator}
	for _, part := range parts {
		if part != "" {
			components = append(components, part)
		}
	}
	return components
}

// safeDirectory is the one rule every component is held to.
//
// `ours` is the difference between the desk's own two directories and their
// ancestors: ours must belong to the effective user, while an ancestor may
// belong to root as well. `/` and `/home` are root's on every ordinary system,
// and a rule that refused them would refuse every desk there is.
func safeDirectory(path string, info fs.FileInfo, ours bool) error {
	if info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf(
			"%s is a symbolic link; a key is not kept anywhere reached through one", path)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}
	owner, known := ownerOf(info)
	if !known {
		return fmt.Errorf("%s: this build could not establish who owns it", path)
	}
	me := effectiveUser()
	if ours && owner != me {
		return fmt.Errorf("%s is owned by user %d rather than by %d, who is running this desk",
			path, owner, me)
	}
	if !ours && owner != me && owner != 0 {
		return fmt.Errorf("%s is owned by user %d, who is neither the user running this desk "+
			"nor the system", path, owner)
	}
	if loose := info.Mode().Perm() & worldMode; loose != 0 && ours {
		// **A directory of ours that anybody could write to is refused, not
		// repaired**, and the earlier version of this was wrong in a way worth
		// recording. It narrowed such a directory to 0700 and carried on, on
		// the argument that tightening it closed the attack. It closes the
		// *future*: what it cannot touch is what was already planted while the
		// directory stood open. The key file is checked, so a planted key is
		// caught — but `desk.json` sits in the same directory and names the
		// endpoint a credential is presented to, so a planted configuration
		// turns the desk's own key into an outbound gift. Narrowing made that
		// arrangement *look* repaired.
		//
		// So the invariant is the simple one: a custody directory that was
		// ever writable by anyone else is not a custody directory. The fix a
		// person makes is one `chmod`, and the sentence says which bits.
		return fmt.Errorf(
			"%s is writable by group or others (mode %#o); a directory anyone could write to "+
				"may already hold something they put there, so narrowing it now would not make "+
				"it safe — remove the %#o bits and restart the desk",
			path, info.Mode().Perm(), loose)
	}
	if ours {
		// Ours, and not writable by anyone else. `0755` is what a umask of 022
		// produces and is repaired to `0700` by the caller: nobody else could
		// have written into it, so there is nothing that might already be
		// there, and refusing it would strand a desk over bits it can simply
		// take away.
		return nil
	}
	if loose := info.Mode().Perm() & worldMode; loose != 0 {
		// **The sticky bit is the exception, and it is the whole reason `/tmp`
		// is a safe place to put a file of your own.** In a sticky directory
		// only the owner of an entry may rename or remove it, which is exactly
		// the capability the rule above exists to deny: another user may
		// create a *new* name there, but cannot replace the one this desk
		// made. A directory of ours found inside such a parent is still
		// required to be ours, so preplanting it is caught by the ownership
		// check rather than by this one.
		if info.Mode()&fs.ModeSticky == 0 {
			return fmt.Errorf(
				"%s is writable by group or others (mode %#o); anyone who may write to a "+
					"directory may replace what is in it", path, info.Mode().Perm())
		}
	}
	return nil
}

// ensureOwnedDirectory creates or narrows one of the desk's own directories,
// named inside an already-validated parent.
func ensureOwnedDirectory(parent, name string) error {
	path := filepath.Join(parent, name)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if mkerr := os.Mkdir(path, custodyDirMode); mkerr != nil {
			return fmt.Errorf("%s could not be created: %w", path, mkerr)
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return fmt.Errorf("%s could not be inspected: %w", path, err)
	}
	if err := safeDirectory(path, info, true); err != nil {
		return err
	}
	// Narrowed even where it was already acceptable: `safeDirectory` refuses
	// group and other **write**, and this takes read and execute away too. The
	// order matters — the check refuses what cannot be repaired, and only then
	// is the repair applied.
	if info.Mode().Perm() != custodyDirMode {
		if err := os.Chmod(path, custodyDirMode); err != nil {
			return fmt.Errorf("%s could not be narrowed to %#o: %w", path, custodyDirMode, err)
		}
	}
	return nil
}

// ensureOwnedDirectoryIn is ensureOwnedDirectory relative to a pinned root.
//
// The same rule, applied through a descriptor rather than a pathname, so the
// directory checked is the directory opened. `label` is only for the sentence.
func ensureOwnedDirectoryIn(root *os.Root, label, name string) error {
	path := filepath.Join(label, name)
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		if mkerr := root.Mkdir(name, custodyDirMode); mkerr != nil {
			return fmt.Errorf("%s could not be created: %w", path, mkerr)
		}
		info, err = root.Lstat(name)
	}
	if err != nil {
		return fmt.Errorf("%s could not be inspected: %w", path, err)
	}
	if err := safeDirectory(path, info, true); err != nil {
		return err
	}
	if info.Mode().Perm() != custodyDirMode {
		if err := root.Chmod(name, custodyDirMode); err != nil {
			return fmt.Errorf("%s could not be narrowed to %#o: %w", path, custodyDirMode, err)
		}
	}
	return nil
}

// readConfigFile reads the desk-level configuration through the pinned
// directory, held to the same rules the key is.
//
// **`desk.json` is not an ordinary file to this desk.** It names the endpoint
// a credential is presented to, which makes writing it equivalent to choosing
// where the key goes — so it is checked the way the key is checked: owned by
// the user running the desk, a regular file, not writable by anyone else, and
// the descriptor that was opened compared to the entry that was inspected.
//
// A world-*readable* `desk.json` is fine and deliberately so: it holds no
// secret, and refusing a `0644` configuration file would refuse the state a
// plain `git checkout` or a text editor leaves. What is refused is one anybody
// else could have *written*.
func (s *assistantStore) readConfigFile() (present bool, data []byte, err error) {
	if !s.usable() {
		return false, nil, s.problem
	}
	at := filepath.Join(s.dir, deskConfigName)
	info, err := s.root.Lstat(deskConfigName)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return false, nil, withCode(CodeNotAFile, fmt.Errorf(
			"%s is a symbolic link rather than a configuration file, and was not read", at))
	}
	if !info.Mode().IsRegular() {
		return false, nil, withCode(CodeNotAFile,
			fmt.Errorf("%s is not a regular file", at))
	}
	if err := ownedByUs(at, info); err != nil {
		return false, nil, withCode(CodeForbidden, err)
	}
	if loose := info.Mode().Perm() & worldMode; loose != 0 {
		return false, nil, withCode(CodeForbidden, fmt.Errorf(
			"%s is writable by group or others (mode %#o); a file anyone could write names the "+
				"endpoint this desk would present its key to, so it is not read",
			at, info.Mode().Perm()))
	}
	// The same instant the key's hook covers, for the same reason: this file
	// decides where a credential is sent, so a swap between the check and the
	// open is worth a test of its own.
	afterConfigStat(at)
	file, err := s.root.OpenFile(deskConfigName, os.O_RDONLY|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return false, nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return false, nil, err
	}
	// The same identity check the key gets, and for the same reason: what was
	// inspected must be what was opened, or a swap between the two decides
	// where a credential goes.
	if !os.SameFile(info, opened) {
		return false, nil, withCode(CodeForbidden, fmt.Errorf(
			"%s changed between being inspected and being opened, and was not read", at))
	}
	data, err = readBounded(file, maxFileBytes)
	if err != nil {
		return false, nil, withCode(CodeTooLarge, fmt.Errorf("%s: %w", at, err))
	}
	return true, data, nil
}

// ownerOnlyFile is the rule a credential file is held to: a regular file that
// nobody but its owner may read or write.
//
// One function, called on the name and again on the descriptor, so that the
// rule has one spelling and one row in the mutation table. Written out twice
// it was invisible to both: breaking either copy left the other refusing the
// same file.
func ownerOnlyFile(path string, mode fs.FileMode) error {
	if !mode.IsRegular() {
		return fmt.Errorf("%s is not a regular file, and was not read", path)
	}
	if perm := mode.Perm(); perm&0o077 != 0 {
		return fmt.Errorf(
			"%s is readable or writable by someone other than its owner (mode %#o), "+
				"so it is not treated as this desk's key", path, perm)
	}
	return nil
}

// ownedByUs is the ownership half of the rule above, shared by the two files
// this desk treats as its own.
func ownedByUs(path string, info fs.FileInfo) error {
	owner, known := ownerOf(info)
	if !known {
		return fmt.Errorf("%s: this build could not establish who owns it", path)
	}
	if me := effectiveUser(); owner != me {
		return fmt.Errorf("%s is owned by user %d rather than by %d, who is running this desk",
			path, owner, me)
	}
	return nil
}

/* The key, through the pinned directory ------------------------------------ */

// readKey answers the stored key, or the empty string where there is none.
//
// **`Lstat` first, and `O_NOFOLLOW` on the open.** `os.Root` follows a symlink
// that stays inside the root, which is exactly the case an attacker who can
// write to the directory would arrange — so the type is checked before the
// open and the open refuses to traverse a link regardless. The mode is checked
// too: a key file somebody else can read is not a key this desk will present.
func (s *assistantStore) readKey() (string, error) {
	if !s.usable() {
		return "", s.problem
	}
	keyPath := filepath.Join(s.dir, secretsDirName, assistantKeyName)
	info, err := s.secrets.Lstat(assistantKeyName)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return "", fmt.Errorf(
			"%s is a symbolic link rather than a key, and was not read", keyPath)
	}
	// **The rule lives in one function, applied twice.** It used to be written
	// out at both points, which made each copy invisible to a mutation: break
	// one and the other refuses the same file a moment later, so the harness
	// reported a safeguard nothing was holding when in fact two things were.
	// Defence in depth is worth having; two spellings of one rule are not.
	if err := ownerOnlyFile(keyPath, info.Mode()); err != nil {
		return "", err
	}
	// **The open is checked against the thing that was inspected, by
	// identity.** The `Lstat` above establishes what is at that name at that
	// instant, and a swap performed *after* it is the race a validated
	// pathname leaves open.
	//
	// `O_NOFOLLOW` is passed and is **not** what closes it. `os.Root` resolves
	// the final component itself — a symlink whose target stays inside the
	// root is followed, and the flag does not reach the syscall that would
	// refuse it. That was asserted here in a comment and was false; the
	// mutation harness is what said so, by removing the flag and watching
	// every test still pass. What actually closes the window is comparing the
	// file that was opened with the file that was inspected: a swapped-in link
	// resolves to a different inode, and a different inode is not this key.
	afterKeyStat(filepath.Join(s.dir, secretsDirName, assistantKeyName))
	file, err := s.secrets.OpenFile(assistantKeyName, os.O_RDONLY|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return "", err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !os.SameFile(info, opened) {
		return "", fmt.Errorf(
			"%s changed between being inspected and being opened, and was not read",
			filepath.Join(s.dir, secretsDirName, assistantKeyName))
	}
	// Re-asserted on the descriptor, because the checks above were made on a
	// name. These two are cheap and they are the ones that matter.
	// Re-asserted on the descriptor, because the check above was made on a
	// name. Same function, so there is one rule to break and one row for it.
	if err := ownerOnlyFile(keyPath, opened.Mode()); err != nil {
		return "", err
	}
	data, err := readBounded(file, maxKeyBytes)
	if err != nil {
		return "", err
	}
	// Trimmed on the way out as well as in, so a file a person wrote by hand
	// with a trailing newline presents the same key this desk would have
	// stored from the same paste.
	return strings.TrimSpace(string(data)), nil
}

// storeKey writes the key, atomically, owner-only, through the pinned
// directory.
//
// The mode is set on the **descriptor** rather than by name: `Root.Chmod` is
// documented as racing a regular-file-to-symlink swap on Unix, and a chmod
// that lands on a link is a chmod on somebody else's file.
func (s *assistantStore) storeKey(key string) error {
	if !s.usable() {
		return s.problem
	}
	staged, name, err := s.stage()
	if err != nil {
		return err
	}
	remove := func() { _ = s.secrets.Remove(name) }
	if _, err := staged.WriteString(key); err != nil {
		staged.Close()
		remove()
		return err
	}
	if err := staged.Chmod(custodyFileMode); err != nil {
		staged.Close()
		remove()
		return err
	}
	if err := staged.Sync(); err != nil {
		staged.Close()
		remove()
		return err
	}
	if err := staged.Close(); err != nil {
		remove()
		return err
	}
	if err := s.secrets.Rename(name, assistantKeyName); err != nil {
		remove()
		return err
	}
	if d, derr := s.secrets.Open("."); derr == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// stage makes an exclusive, randomly named staging file beside the key.
//
// `O_EXCL` is what makes a name collision a retry rather than a silent
// overwrite — and, here, what makes a preplanted name a refusal rather than a
// write through somebody else's symlink.
func (s *assistantStore) stage() (*os.File, string, error) {
	for attempt := 0; attempt < 10; attempt++ {
		name, err := stagingName()
		if err != nil {
			return nil, "", err
		}
		file, err := s.secrets.OpenFile(
			name, os.O_RDWR|os.O_CREATE|os.O_EXCL|openNoFollow, custodyFileMode)
		if err == nil {
			return file, name, nil
		}
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		return nil, "", err
	}
	return nil, "", errors.New("no unused staging name")
}

// removeKey deletes the key. Deleting one that is not there is not a failure:
// the caller asked for a state, and that state already holds.
func (s *assistantStore) removeKey() error {
	if !s.usable() {
		return s.problem
	}
	err := s.secrets.Remove(assistantKeyName)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
