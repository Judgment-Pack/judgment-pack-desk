package desk

// Key custody for the desks Desk makes (ADR-0010, section 1, and the
// maintainer's answers to its questions 1 to 3).
//
// # Where a desk's key is kept
//
// One Ed25519 seed per desk, at `<config>/secrets/signing/<desk id>.seed`:
//
//   - `secrets/` is the directory Desk's custody already validates and holds
//     by descriptor (custody.go): every component a real directory, none
//     writable by group or other, Desk's own owned by the user and 0700.
//   - `signing/` is held to the same rule, through that descriptor: made 0700
//     where it is missing, narrowed to 0700, and refused, never repaired,
//     where it is a link, is not a directory, is another user's, or could be
//     written by group or other.
//   - The seed is the runtime's to write: `jpack audit key generate <path>
//     --format json` creates it 0600, and never over anything. Desk hands it
//     a path and not a descriptor, because the runtime opens the path itself,
//     and a `/proc/self/fd` path is a link, which it refuses. The window
//     between Desk's check of `signing/` and the runtime's open is in a
//     directory only this user can change. Desk then checks what the runtime
//     wrote, through its descriptor, and never reads the seed's bytes.
//
// One key per desk, not one per installation: a copied key then signs for one
// trail only, and a rotation is that desk's alone.
//
// # The public keys: `<desk id>.keys.jsonl`
//
// Beside the seed, Desk keeps the desk's public keys, in the order the trail
// uses them: one JSON object per line, each line ending in a newline.
//
//	{"publicKey":"<64 lowercase hex>","keyId":"<32 lowercase hex>","at":<sequence>}
//
//   - publicKey is the key's public half, as `audit key generate` printed it.
//   - keyId is the runtime's name for the key, as it printed it: the first 32
//     hexadecimal characters of the SHA-256 of the key's 32 bytes, which each
//     signature names. Desk checks that the two agree.
//   - at is the trail sequence the key took over from: it signs the records
//     after that sequence. A desk's first key takes over from 0.
//
// The members are in that order, with no spaces, exactly as `json.Marshal`
// writes `deskPublicKey`, and a line in any other spelling is refused, so the
// file has one spelling. It is public material, and never holds a seed. It is
// written whole, through a staging file linked into place, so it is never
// seen half written and never written over.
//
// # A creation marker
//
// Before the runtime is asked for a key, Desk writes `<desk id>.creating`,
// 0600, beside where the seed will be, and removes it only once the desk's
// manifest is written. It records the creation (issue #310, `deskCreation`):
// the desk's id and its folder, by device and inode, and, written again
// immediately before the manifest is, the manifest's digest:
//
//	{"id":"<32 hex>","folder":"<device>:<inode>"}
//	{"id":"<32 hex>","folder":"<device>:<inode>","manifest":"sha256:<64 hex>"}
//
// A Desk stopped in between leaves the marker, and the next start removes
// that id's seed, list and marker only where the desk was never published
// (`sweepUnfinishedKeys`). A seed or a list with no marker is never removed,
// whatever the desks folder says. The key the upgrade makes for the project
// Desk was started on keeps a marker the same way, until jpack.json names it
// and its lock is checked (startup_key.go).
//
// # One lock
//
// A creation, from before its marker to the marker's removal, and the sweep,
// from its first look to its last removal, each hold the signing folder's
// lock (signing_lock.go), so that a second Desk process sharing the
// configuration folder never sweeps away a key a creation is making (issue
// #230).
//
// # What this does not defend against
//
// Custody's own residual (custody.go): a process running as this user can
// read the seed. Mode 0600 keeps other users out, not this one. Against such a
// process, and against the owner, who holds the key, a signature binds
// nothing. The README's Gates section says whom it binds.
//
// The runtime writes the seed through a pathname, and Desk checks through the
// folder it holds. Desk checks that the pathname names the folder it holds
// immediately before the runtime runs, and that the seed's pathname names the
// file it found through that folder after the run and again immediately
// before the desk is published; a creation that finds otherwise is refused,
// and never answered as signed. What is left is the window between the first
// check and the runtime's own open, in a folder only this user can change
// (ADR-0010, section 1): a swap made in that moment by a process of this same
// user can have the runtime write the seed where the swapped-in folder is.
// Desk removes only what it finds through the folder it holds, so such a
// seed is left where it was written.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// signingDirName is the folder under `secrets/` that holds the desks'
	// seeds and their lists of public keys.
	signingDirName = "signing"
	// seedSuffix and keysSuffix follow a desk's id in the names of its seed
	// and of its list of public keys.
	seedSuffix = ".seed"
	keysSuffix = ".keys.jsonl"
	// creatingSuffix follows a desk's id in the name of the marker a creation
	// keeps beside its key until the desk is published.
	creatingSuffix = ".creating"
	// keysStagingPrefix names a list of public keys while it is being
	// written, before it is linked into place.
	keysStagingPrefix = ".keys-"
	// verifyKeysPrefix names a folder of public-key files written for one
	// `audit verify`, and removed after it.
	verifyKeysPrefix = ".verify-"
	// signedFromVersion is the first configuration version whose audit
	// member may name a signing key (runtime 0.26.0, `jpack.schema.json`).
	signedFromVersion = "6"
	// maxDeskKeys bounds a desk's list of public keys, and so the
	// `--public-key` arguments one verification is given.
	maxDeskKeys = 64
	// keysFileLimit is the most of a list of public keys Desk reads: a line
	// is at most 190 bytes, with a trail and a sequence of 16 digits.
	keysFileLimit = maxDeskKeys * 192
)

var (
	publicKeyForm = regexp.MustCompile(`^[0-9a-f]{64}$`)
	keyIDForm     = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

// errNoSigningDir is a `secrets/` that holds no `signing/`: Desk keeps no key
// there yet. Only a read sees it; a creation makes the folder.
var errNoSigningDir = errors.New("Desk keeps no signing folder")

// The pathname of Desk's signing folder, or of the key in it, no longer
// naming the folder Desk holds or the key it found there, at each moment Desk
// asks.
var (
	errMovedBeforeGenerate = errors.New("Desk's signing folder was replaced before the runtime made the key, so no key was made")
	errMovedAfterGenerate  = errors.New("Desk's signing folder was replaced while the runtime made the key, so the key is not where the desk would name it")
	errMovedBeforePublish  = errors.New("Desk's signing folder was replaced before the desk was published, so its key is not where the desk would name it")
	errNotNamed            = errors.New("the pathname does not name what Desk holds")
)

// testHookKeyBetween runs at the moments a swap of Desk's signing folder, or
// of a file in it, would matter, and is nil outside tests: "before generate"
// (the marker written, the runtime not yet run), "after generate" (the
// runtime run, its seed not yet checked), "before publish" (the desk locked,
// its manifest not yet written), "published" (the manifest written, the
// marker not yet removed) and "read list" (a list of keys inspected, not yet
// opened). It lets a test put another folder or file under a name, or stop a
// creation there, as a crash would.
var testHookKeyBetween func(at string)

func keyBetween(at string) {
	if testHookKeyBetween != nil {
		testHookKeyBetween(at)
	}
}

// deskPublicKey is one line of `<desk id>.keys.jsonl`, and one key the
// decision-record panel shows and passes. The order of the members is the
// order of the line.
//
// Trail is, for a key a rotation added, the trail identity the rotation was
// made on (issue #285), as the trail's signature sidecar names it: what tells
// a trail begun after a rotation from the same trail with a rotation missing
// (`trailFirst`). The first key, and a key a Desk before it added, has none.
// It is in the list's line, and never in what the page is told.
type deskPublicKey struct {
	PublicKey string `json:"publicKey"`
	KeyID     string `json:"keyId"`
	At        int64  `json:"at"`
	Trail     string `json:"-"`
}

// listedKey is a key's line in the list, Trail included where it has one.
type listedKey struct {
	PublicKey string `json:"publicKey"`
	KeyID     string `json:"keyId"`
	At        int64  `json:"at"`
	Trail     string `json:"trail,omitempty"`
}

// keyIDOf is the runtime's name for a public key: the first 32 hexadecimal
// characters of the SHA-256 of its 32 bytes (runtime 0.26.0, `audit key
// public --help`). Desk computes it only to check the one the runtime printed.
func keyIDOf(publicKey string) string {
	raw, err := hex.DecodeString(publicKey)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:32]
}

// check says why a key is not one Desk keeps: a public key that is not 64
// lowercase hexadecimal characters, a keyId that is not the one it has, or a
// sequence before the trail's first.
func (k deskPublicKey) check() error {
	switch {
	case !publicKeyForm.MatchString(k.PublicKey):
		return errors.New("a public key is not 64 lowercase hexadecimal characters")
	case !keyIDForm.MatchString(k.KeyID) || k.KeyID != keyIDOf(k.PublicKey):
		return errors.New("a keyId is not the one its public key has")
	case k.At < 0:
		return errors.New("a key takes over from a sequence before the trail's first")
	case k.Trail != "" && !keyIDForm.MatchString(k.Trail):
		return errors.New("a trail identity is not 32 lowercase hexadecimal characters")
	}
	return nil
}

// line is the key's line in `<desk id>.keys.jsonl`, exactly as Desk writes
// it: with its trail, where it has one.
func (k deskPublicKey) line() []byte {
	data, _ := json.Marshal(listedKey(k))
	return append(data, '\n')
}

// sameKey is whether a and b are one key taking over from one sequence,
// whether or not either line records its trail.
func sameKey(a, b deskPublicKey) bool {
	return a.PublicKey == b.PublicKey && a.KeyID == b.KeyID && a.At == b.At
}

// parseDeskKeys reads a list of public keys in the one spelling Desk writes:
// at least one line, at most maxDeskKeys, each its key's own line, the first
// taking over from 0 on no named trail, each later one from a sequence from 1
// on, a later one than the key before it where both took over on the same
// trail (issue #285: a key that took over on another trail than the key
// before it counts its sequence on that trail), and no key twice. Its errors
// name no path.
func parseDeskKeys(data []byte) ([]deskPublicKey, error) {
	if len(data) == 0 || data[len(data)-1] != '\n' {
		return nil, errors.New("it is empty, or its last line does not end")
	}
	lines := strings.Split(string(data[:len(data)-1]), "\n")
	if len(lines) > maxDeskKeys {
		return nil, fmt.Errorf("it lists more than %d keys", maxDeskKeys)
	}
	keys := make([]deskPublicKey, 0, len(lines))
	seen := map[string]bool{}
	for i, line := range lines {
		var listed listedKey
		if json.Unmarshal([]byte(line), &listed) != nil {
			return nil, fmt.Errorf("line %d is not in the form Desk writes", i+1)
		}
		key := deskPublicKey(listed)
		if string(key.line()) != line+"\n" {
			return nil, fmt.Errorf("line %d is not in the form Desk writes", i+1)
		}
		if err := key.check(); err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		if i == 0 && (key.At != 0 || key.Trail != "") || i > 0 && (key.At < 1 || key.Trail == keys[i-1].Trail && key.At <= keys[i-1].At) {
			return nil, fmt.Errorf("line %d does not take over after the key before it", i+1)
		}
		if seen[key.PublicKey] {
			return nil, fmt.Errorf("line %d lists a key a second time", i+1)
		}
		seen[key.PublicKey] = true
		keys = append(keys, key)
	}
	return keys, nil
}

// signingDir is `secrets/signing/`, validated and held by descriptor, with
// the absolute path the runtime is given its names under.
type signingDir struct {
	root *os.Root
	path string
}

func (d *signingDir) Close() {
	if d != nil && d.root != nil {
		d.root.Close()
	}
}

// openSigning validates `secrets/signing/` and holds it.
//
// With create, a missing one is made 0700 and a loose one narrowed, through
// `secrets/`'s descriptor (`ensureOwnedDirectoryIn`). Without it, nothing is
// made or changed, and a missing one is errNoSigningDir. Either way the folder
// must be a real directory, the user's, and writable by nobody else
// (`safeDirectory`), and the folder opened must be the folder checked.
//
// A build that cannot establish who owns a directory keeps no key, and says
// so (`custodyChecked`).
func (a *assistantStore) openSigning(create bool) (*signingDir, error) {
	if !custodyChecked {
		return nil, errors.New("this build cannot establish who owns a directory, so it will not keep a key")
	}
	if !a.usable() {
		return nil, a.problem
	}
	secrets := filepath.Join(a.dir, secretsDirName)
	path := filepath.Join(secrets, signingDirName)
	if create {
		// **Made first, and one that is there already is checked as usual.**
		// Two first creations at once can both find no folder; the one whose
		// `Mkdir` loses finds it made, and validates it, rather than keeping
		// no key.
		if err := a.secrets.Mkdir(signingDirName, custodyDirMode); err != nil && !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("%s could not be created: %w", path, err)
		}
		if err := ensureOwnedDirectoryIn(a.secrets, secrets, signingDirName); err != nil {
			return nil, err
		}
	}
	checked, err := a.secrets.Lstat(signingDirName)
	if errors.Is(err, fs.ErrNotExist) && !create {
		return nil, errNoSigningDir
	}
	if err != nil {
		return nil, fmt.Errorf("%s could not be inspected: %w", path, err)
	}
	if err := safeDirectory(path, checked, true); err != nil {
		return nil, err
	}
	// The instant a swap would matter: after the check, before the open.
	afterCustodyCheck(path)
	root, err := a.secrets.OpenRoot(signingDirName)
	if err != nil {
		return nil, fmt.Errorf("%s could not be opened: %w", path, err)
	}
	if opened, err := root.Stat("."); err != nil || !os.SameFile(checked, opened) {
		root.Close()
		return nil, fmt.Errorf("%s changed between being checked and being opened, and was not used", path)
	}
	return &signingDir{root: root, path: path}, nil
}

// namesHeld is whether the signing folder's pathname, not followed at its
// end, still names the folder this holds: what the runtime is about to be
// given a path in.
func (d *signingDir) namesHeld() error {
	named, err := os.Lstat(d.path)
	if err != nil || !named.IsDir() {
		return errNotNamed
	}
	held, err := d.root.Stat(".")
	if err != nil || !os.SameFile(named, held) {
		return errNotNamed
	}
	return nil
}

// namesFile is whether the pathname of name in the signing folder, not
// followed, names info: the file found through the folder this holds.
func (d *signingDir) namesFile(name string, info os.FileInfo) error {
	named, err := os.Lstat(filepath.Join(d.path, name))
	if err != nil || named.Mode()&fs.ModeSymlink != 0 || !os.SameFile(named, info) {
		return errNotNamed
	}
	return nil
}

// madeKey is a key a creation generated: the marker it wrote, the seed the
// runtime wrote and the list of public keys Desk wrote beside it, each as it
// was found once written, so that a creation that stops removes these files
// and nothing it did not make.
type madeKey struct {
	dir                            *signingDir
	seedName, keysName, markerName string
	seed, keys, marker             os.FileInfo
	public                         deskPublicKey
	// unlock releases the signing folder's lock the creation holds, from
	// before its marker until the marker is removed (signing_lock.go).
	unlock func()
}

// seedPath is the seed's absolute path: what `audit key generate` was given,
// and what the desk's `audit.signingKey` names.
func (k *madeKey) seedPath() string { return filepath.Join(k.dir.path, k.seedName) }

// close releases the signing folder, and its lock. A nil key holds nothing.
func (k *madeKey) close() {
	if k != nil {
		k.dir.Close()
		if k.unlock != nil {
			k.unlock()
		}
	}
}

// publishing records in the creation's marker the digest of the manifest
// about to be written (issue #310): from here on, a start that finds the
// marker cannot take the desk for one never published unless the desk's own
// folder, in the desks folder, shows it holds no manifest. The marker is
// written again whole, over the marker this creation wrote, and only while
// its name still holds it. A nil key made none.
func (k *madeKey) publishing(id string, folder *os.Root, manifest []byte) error {
	if k == nil {
		return nil
	}
	held, err := folder.Stat(".")
	if err != nil {
		return err
	}
	record := deskCreation{ID: id, Folder: identityKey(held), Manifest: sha256Digest(manifest)}
	written, err := k.dir.rewriteMarker(k.markerName, k.marker, record.line())
	if err != nil {
		return err
	}
	k.marker = written
	return nil
}

// stillNamed is whether the key is still where the desk's configuration names
// it: the seed's pathname names the seed found through the folder this holds.
// It is asked immediately before the desk is published.
func (k *madeKey) stillNamed() error {
	if k == nil {
		return nil
	}
	if k.dir.namesFile(k.seedName, k.seed) != nil {
		return errMovedBeforePublish
	}
	return nil
}

// settle removes the creation's marker, once the desk is published, and only
// while its name still holds the marker this creation wrote. A nil key made
// none.
func (k *madeKey) settle() error {
	if k == nil {
		return nil
	}
	return k.dir.removeMade(k.markerName, k.marker)
}

// removeMade removes name from the folder this holds, only while it is info.
// What is not there is already gone.
func (d *signingDir) removeMade(name string, info os.FileInfo) error {
	if info == nil {
		return nil
	}
	found, err := d.root.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil || !os.SameFile(found, info) {
		return fmt.Errorf("%s is not the file this creation made, and was left", name)
	}
	return d.root.Remove(name)
}

// unmake removes the list of public keys, the seed and the marker a creation
// made, in that order, each only while its name still holds the file that
// was made, and only through the folder this holds; it stops at the first
// that cannot be removed, so the marker stays wherever anything else does,
// for the next start's sweep (review round 1 of #296). A nil key made
// nothing.
func (k *madeKey) unmake() error {
	if k == nil {
		return nil
	}
	for _, made := range []struct {
		name string
		info os.FileInfo
	}{{k.keysName, k.keys}, {k.seedName, k.seed}, {k.markerName, k.marker}} {
		if err := k.dir.removeMade(made.name, made.info); err != nil {
			return err
		}
	}
	return nil
}

// generateDeskKey has the runtime write desk id's seed into dir, checks what
// it wrote, and writes the desk's list of public keys beside it (ADR-0010,
// section 1, "Creating it"). It runs through `runRuntime`, in the new desk's
// folder, as every command a creation runs does.
//
// **Nothing already there is touched.** None of the names may hold anything
// before the runtime runs. The marker is written first, through the folder
// held. Immediately before the run, the folder's pathname must still name the
// folder held. After a run that failed, whatever the seed's name holds in the
// folder held was made by that run, and is removed with the marker; once the
// seed is checked, a later failure removes it by identity (`unmake`). The
// seed's pathname must then name the seed found through the folder held.
func generateDeskKey(ctx context.Context, bin string, held heldDir, dir *signingDir, id string) (*madeKey, error) {
	return generateKeyMarked(ctx, bin, held, dir, id, nil)
}

// generateKeyMarked is generateDeskKey with a marker that holds record, one
// line binding the creation to what it was made for, where record is not nil
// (the startup project's key, `startupCreation`; a made desk's,
// `deskCreation`); the Runner key's marker is empty.
func generateKeyMarked(ctx context.Context, bin string, held heldDir, dir *signingDir, id string, record []byte) (*madeKey, error) {
	made := &madeKey{dir: dir, seedName: id + seedSuffix, keysName: id + keysSuffix, markerName: id + creatingSuffix}
	for _, name := range []string{made.seedName, made.keysName, made.markerName} {
		if _, err := dir.root.Lstat(name); !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("something is already kept as %s, and a key is never written over anything", name)
		}
	}
	marker, err := dir.writeMarkerHolding(made.markerName, record)
	if err != nil {
		return nil, fmt.Errorf("the creation's marker could not be written: %w", err)
	}
	made.marker = marker
	keyBetween("before generate")
	if dir.namesHeld() != nil {
		return nil, unmadeAfter(made, errMovedBeforeGenerate)
	}
	out, runErr := runRuntime(ctx, bin, held, "audit", "key", "generate", made.seedPath(), "--format", "json")
	keyBetween("after generate")
	var answer struct {
		Command     string              `json:"command"`
		Status      string              `json:"status"`
		PublicKey   string              `json:"publicKey"`
		KeyID       string              `json:"keyId"`
		Diagnostics []runtimeDiagnostic `json:"diagnostics"`
	}
	decoded := out != nil && json.Unmarshal(out, &answer) == nil
	if runErr != nil || !decoded || answer.Command != "audit key generate" || answer.Status != "generated" {
		removed := dir.root.Remove(made.seedName)
		failure := generationFailure(answer.Diagnostics, runErr)
		if removed != nil && !errors.Is(removed, fs.ErrNotExist) {
			failure = keyMadeLeft{fmt.Errorf("%w; what it left at %s could not be removed: %v", failure, made.seedName, removed)}
		}
		return nil, unmadeAfter(made, failure)
	}
	seed, err := dir.root.Lstat(made.seedName)
	if err != nil {
		return nil, unmadeAfter(made, fmt.Errorf("the runtime reported a signing key, but there is none in the signing folder Desk holds: %w", err))
	}
	made.seed = seed
	if dir.namesFile(made.seedName, seed) != nil {
		return nil, unmadeAfter(made, errMovedAfterGenerate)
	}
	public := deskPublicKey{PublicKey: answer.PublicKey, KeyID: answer.KeyID, At: 0}
	if err := checkSeed(made.seedName, seed); err != nil {
		return nil, unmadeAfter(made, err)
	}
	if err := public.check(); err != nil {
		return nil, unmadeAfter(made, fmt.Errorf("its audit key generate did not answer as documented: %w", err))
	}
	made.public = public
	keys, err := dir.writeNewKeys(made.keysName, []deskPublicKey{public})
	if err != nil {
		return nil, unmadeAfter(made, fmt.Errorf("the list of its public keys could not be written: %w", err))
	}
	made.keys = keys
	return made, nil
}

// checkSeed is the rule the seed the runtime wrote is held to before a desk
// names it: one regular file, not a link, with one name, the user's, and
// readable and writable by nobody else. The runtime refuses a key that breaks
// any of these, and would sign nothing with it.
func checkSeed(name string, info os.FileInfo) error {
	if info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symbolic link, not a signing key", name)
	}
	if err := ownerOnlyFile(name, info.Mode()); err != nil {
		return err
	}
	if err := ownedByUs(name, info); err != nil {
		return err
	}
	if links, known := linkCount(info); !known || links != 1 {
		return fmt.Errorf("%s has more than one name, or this build cannot say how many", name)
	}
	return nil
}

// unmadeAfter is err, after removing what a creation made of its key so far;
// a keyMadeLeft where any of it could not be removed.
func unmadeAfter(made *madeKey, err error) error {
	if removed := made.unmake(); removed != nil {
		return keyMadeLeft{fmt.Errorf("%w; and what was made of its key could not be removed: %v", err, removed)}
	}
	return err
}

// keyMadeLeft is a creation that stopped and left some of what it made of
// its key, in its own words: the upgrade keeps the project's identity beside
// it (review round 1 of #296).
type keyMadeLeft struct{ error }

func (e keyMadeLeft) Unwrap() error { return e.error }

// generationFailure is why the runtime did not generate a key: its own words
// where it gave any, else how the run failed.
func generationFailure(diagnostics []runtimeDiagnostic, runErr error) error {
	var said []string
	for _, diagnostic := range diagnostics {
		if message := strings.TrimSpace(diagnostic.Message); message != "" {
			said = append(said, message)
		}
	}
	switch {
	case len(said) > 0:
		return fmt.Errorf("the runtime did not generate its signing key: %s", strings.Join(said, " "))
	case runErr != nil:
		return fmt.Errorf("the runtime did not generate its signing key: %w", runErr)
	}
	return errors.New("the runtime did not generate its signing key: its audit key generate did not answer as documented")
}

// deskCreation is the marker of a made desk's key while the desk is made
// (issue #310): one line naming the desk's id and its folder, by device and
// inode, and, from immediately before its manifest is written, the
// manifest's digest. An empty marker is one an earlier Desk left.
type deskCreation struct {
	ID       string `json:"id"`
	Folder   string `json:"folder"`
	Manifest string `json:"manifest,omitempty"`
}

// line is the marker's one spelling.
func (c deskCreation) line() []byte {
	data, _ := json.Marshal(c)
	return append(data, '\n')
}

// deskCreationLimit is the most of a made desk's creation marker Desk reads:
// the longest is 180 bytes.
const deskCreationLimit = 512

// readDeskCreation reads desk id's creation marker, inspected as marker,
// whole, under the rule Desk keeps a private file by, and holds it to its
// record: its one spelling, this id, a folder in its form, and a manifest
// digest in its form where it names one. legacy is an empty marker.
func (d *signingDir) readDeskCreation(id string, marker os.FileInfo) (creation deskCreation, legacy bool, err error) {
	data, opened, err := readPrivateFile(d.root, id+creatingSuffix, deskCreationLimit)
	if err != nil {
		return deskCreation{}, false, err
	}
	if !os.SameFile(opened, marker) {
		return deskCreation{}, false, errors.New("its marker changed while it was read")
	}
	if len(data) == 0 {
		return deskCreation{}, true, nil
	}
	if json.Unmarshal(data, &creation) != nil || !bytes.Equal(creation.line(), data) || creation.ID != id ||
		creation.Folder != "" && !fileIdentityForm.MatchString(creation.Folder) || creation.Manifest != "" && !recordForm.MatchString(creation.Manifest) {
		return deskCreation{}, false, errors.New("its marker is not the record of a creation Desk writes")
	}
	return creation, false, nil
}

// rewriteMarker puts data in the place of the marker found as held: staged
// under a name of its own, 0600, synced, and renamed over the marker only
// while its name still holds held; the folder is synced after. It answers
// the marker as written.
func (d *signingDir) rewriteMarker(name string, held os.FileInfo, data []byte) (os.FileInfo, error) {
	staged, stage, err := d.stage()
	if err != nil {
		return nil, err
	}
	defer d.root.Remove(stage)
	_, err = staged.Write(data)
	if err == nil {
		err = staged.Chmod(custodyFileMode)
	}
	if err == nil {
		err = staged.Sync()
	}
	written, statErr := staged.Stat()
	if closed := staged.Close(); err == nil {
		err = closed
	}
	if err == nil {
		err = statErr
	}
	if err != nil {
		return nil, err
	}
	if found, err := d.root.Lstat(name); err != nil || !os.SameFile(found, held) {
		return nil, errors.New("the creation's marker is not the file the creation wrote")
	}
	if err := d.root.Rename(stage, name); err != nil {
		return nil, err
	}
	_ = syncPrivateDirectory(d.root)
	return written, nil
}

// writeMarker writes a creation's marker, empty and 0600, never over
// anything, through the folder this holds, and answers it as written.
func (d *signingDir) writeMarker(name string) (os.FileInfo, error) {
	return d.writeMarkerHolding(name, nil)
}

// writeMarkerHolding is writeMarker, the marker holding data, synced.
func (d *signingDir) writeMarkerHolding(name string, data []byte) (os.FileInfo, error) {
	file, err := d.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL|openNoFollow, custodyFileMode)
	if err != nil {
		return nil, err
	}
	if len(data) > 0 {
		if _, err := file.Write(data); err != nil {
			file.Close()
			_ = d.root.Remove(name)
			return nil, err
		}
		if err := file.Sync(); err != nil {
			file.Close()
			_ = d.root.Remove(name)
			return nil, err
		}
	}
	info, err := file.Stat()
	if closed := file.Close(); err == nil {
		err = closed
	}
	if err != nil {
		_ = d.root.Remove(name)
		return nil, err
	}
	return info, nil
}

// writeNewKeys writes a list of public keys under name, whole, and never over
// anything: staged under a name of its own, made 0600 on its descriptor,
// synced, and then linked into place. A link never replaces a name that
// exists, where a rename would. It answers the file as written.
func (d *signingDir) writeNewKeys(name string, keys []deskPublicKey) (os.FileInfo, error) {
	var data []byte
	for _, key := range keys {
		data = append(data, key.line()...)
	}
	staged, stagedName, err := d.stage()
	if err != nil {
		return nil, err
	}
	defer d.root.Remove(stagedName)
	if _, err := staged.Write(data); err != nil {
		staged.Close()
		return nil, err
	}
	if err := staged.Chmod(custodyFileMode); err != nil {
		staged.Close()
		return nil, err
	}
	if err := staged.Sync(); err != nil {
		staged.Close()
		return nil, err
	}
	if err := staged.Close(); err != nil {
		return nil, err
	}
	if err := d.root.Link(stagedName, name); err != nil {
		return nil, err
	}
	info, err := d.root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if dir, err := d.root.Open("."); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return info, nil
}

// stage makes an exclusive, randomly named staging file in the signing
// folder, as custody's own `stage` does beside the assistant's key.
func (d *signingDir) stage() (*os.File, string, error) {
	for attempt := 0; attempt < 10; attempt++ {
		name, err := randomStagingName(keysStagingPrefix)
		if err != nil {
			return nil, "", err
		}
		file, err := d.root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL|openNoFollow, custodyFileMode)
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

// readKeys reads the list of public keys kept as name, under the rule Desk
// keeps it by: a regular file, not a link, the user's, writable by nobody
// else, the file opened the file inspected, at most keysFileLimit bytes, and
// in the one spelling Desk writes (`parseDeskKeys`). found is false where
// there is none. Its errors name no path.
func (d *signingDir) readKeys(name string) (keys []deskPublicKey, found bool, err error) {
	keys, _, found, err = d.readKeysFile(name)
	return keys, found, err
}

// readKeysFile is readKeys, with the list as it was read: the file inspected
// and its bytes, which a rotation checks are still the list's before it
// writes the list again (`replaceKeys`).
func (d *signingDir) readKeysFile(name string) (keys []deskPublicKey, read keysFile, found bool, err error) {
	info, err := d.root.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, keysFile{}, false, nil
	}
	if err != nil {
		return nil, keysFile{}, true, errors.New("it could not be inspected")
	}
	if !info.Mode().IsRegular() {
		return nil, keysFile{}, true, errors.New("it is not a regular file")
	}
	if ownedByUs(name, info) != nil {
		return nil, keysFile{}, true, errors.New("it is not owned by the user running Desk")
	}
	if info.Mode().Perm()&worldMode != 0 {
		return nil, keysFile{}, true, errors.New("its group or other users can write it")
	}
	keyBetween("read list")
	file, err := d.root.OpenFile(name, os.O_RDONLY|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return nil, keysFile{}, true, errors.New("it could not be opened")
	}
	defer file.Close()
	if opened, err := file.Stat(); err != nil || !os.SameFile(info, opened) {
		return nil, keysFile{}, true, errors.New("it changed between being inspected and being opened")
	}
	data, err := readBounded(file, keysFileLimit)
	if err != nil {
		return nil, keysFile{}, true, fmt.Errorf("it could not be read whole within %d bytes", keysFileLimit)
	}
	keys, err = parseDeskKeys(data)
	return keys, keysFile{info: info, data: data}, true, err
}

// keysFile is a list of public keys as it was read: the file inspected, and
// its bytes.
type keysFile struct {
	info os.FileInfo
	data []byte
}

// publicKeyFiles writes each key to a file of its own, for `audit verify
// --public-key`, which reads a key from the file it names and never from the
// argument itself (runtime 0.26.0, `readSignatureOptions`). The files are in
// a new folder in the signing folder, which only this user can change, so
// that no one else can put another key in their place. It answers their
// paths, in the keys' order, and a function that removes them. A Desk
// stopped between the two leaves the folder behind, named `.verify-…`; it
// holds public keys only, and nothing reads it again.
func (d *signingDir) publicKeyFiles(keys []deskPublicKey) ([]string, func() error, error) {
	folder, err := randomStagingName(verifyKeysPrefix)
	if err != nil {
		return nil, nil, err
	}
	if err := d.root.Mkdir(folder, custodyDirMode); err != nil {
		return nil, nil, err
	}
	var written []string
	remove := func() error {
		var errs []error
		for _, name := range written {
			errs = append(errs, d.root.Remove(name))
		}
		errs = append(errs, d.root.Remove(folder))
		return errors.Join(errs...)
	}
	paths := make([]string, 0, len(keys))
	for i, key := range keys {
		name := filepath.Join(folder, fmt.Sprintf("%d.pub", i+1))
		file, err := d.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL|openNoFollow, custodyFileMode)
		if err != nil {
			return nil, nil, errors.Join(err, remove())
		}
		written = append(written, name)
		_, err = file.WriteString(key.PublicKey + "\n")
		if closed := file.Close(); err == nil {
			err = closed
		}
		if err != nil {
			return nil, nil, errors.Join(err, remove())
		}
		paths = append(paths, filepath.Join(d.path, name))
	}
	return paths, remove, nil
}

// pathSpan is one way a sentence can name a path Desk knows, and what it is
// replaced by: a file's whole path, or a directory's, which is taken with the
// rest of a path under it (`replaceDirectory`).
type pathSpan struct {
	value, with string
	directory   bool
}

// custodySpans is every way a sentence can name the key Desk keeps for the
// desk id, its list of public keys, Desk's signing folder or its
// configuration folder, or a folder on the way to any of them, each replaced
// by with (`pathSpans`, the one generator every such span comes from). The
// seed's and the list's spans are given where id is not empty.
//
// They are exact spans because the general rule (`withoutAbsolutePaths`)
// ends a path at its first space: a home folder with a space in its name
// would otherwise leave the rest of its path in the sentence. The runtime
// names the seed, and from runtime #222 a directory on the seed's path, in
// `packs validate`'s `audit-signing-key` check and in `audit key generate`'s
// refusals.
func (s *Server) custodySpans(id, with string) []pathSpan {
	if !filepath.IsAbs(s.configDir) {
		return nil
	}
	signing := filepath.Join(s.configDir, secretsDirName, signingDirName)
	var spans []pathSpan
	if id != "" {
		spans = append(spans, pathSpans(filepath.Join(signing, id+seedSuffix), false, with)...)
		spans = append(spans, pathSpans(filepath.Join(signing, id+keysSuffix), false, with)...)
	}
	spans = append(spans, pathSpans(signing, true, with)...)
	return append(spans, pathSpans(s.configDir, true, with)...)
}

// pathSpellings is every spelling a sentence can carry of path, which is
// absolute: path as it was given, as `filepath.Clean` writes it, and the path
// it resolves to (`filepath.EvalSymlinks`), where it resolves. The runtime
// prints a key's path as it was given, and a directory on the key's path as
// it cleans it; a resolved spelling covers a sentence that follows a link.
func pathSpellings(path string) []string {
	spellings := []string{path}
	spellings = append(spellings, filepath.Clean(path))
	if real, err := filepath.EvalSymlinks(path); err == nil {
		spellings = append(spellings, real)
	}
	return spellings
}

// pathSpans is the one generator of the spans by which a sentence can name
// path, which is absolute, or a directory on its way. For each of its
// spellings (`pathSpellings`) it gives the whole spelling, as a file or, with
// directory, as a directory, and every prefix of it that ends where a
// separator starts, as a directory, down to but not including the root; each
// as Go writes it and as the runtime prints it (`displayedPath`), each
// replaced by with. Repeats are given once; `replaceSpans` orders them
// longest first.
func pathSpans(path string, directory bool, with string) []pathSpan {
	if !filepath.IsAbs(path) {
		return nil
	}
	var spans []pathSpan
	seen := map[pathSpan]bool{}
	add := func(value string, directory bool) {
		for _, form := range []string{value, displayedPath(value)} {
			span := pathSpan{form, with, directory}
			if !seen[span] {
				seen[span] = true
				spans = append(spans, span)
			}
		}
	}
	for _, spelling := range pathSpellings(path) {
		add(spelling, directory)
		for at := 1; at < len(spelling); at++ {
			if os.IsPathSeparator(spelling[at]) && strings.TrimLeft(spelling[:at], string(filepath.Separator)) != "" {
				add(spelling[:at], true)
			}
		}
	}
	return spans
}

// replaceSpans replaces each span where message names it, longest first, so
// that a path inside another is never replaced before the one around it: a
// file's path wherever it stands, and a directory's as `replaceDirectory`
// says.
func replaceSpans(message string, spans []pathSpan) string {
	spans = slices.Clone(spans)
	slices.SortStableFunc(spans, func(a, b pathSpan) int { return len(b.value) - len(a.value) })
	for _, span := range spans {
		switch {
		case span.value == "" || span.value == span.with:
		case span.directory:
			message = replaceDirectory(message, span.value, span.with)
		default:
			message = strings.ReplaceAll(message, span.value, span.with)
		}
	}
	return message
}

// replaceDirectory replaces each place message names dir, or a path under
// it, with with. dir must stand where a path starts: at the start, or after
// a space, a double quote, a parenthesis, a bracket or "=". It must then end,
// at the end of the message, before a space, a quote, a closing parenthesis
// or bracket, ";" or ":", or before "." or "," that ends a clause; or go on
// with a separator, and then the rest of the path under it is taken too, up
// to the next space, quote, parenthesis or bracket, less any ".,:;" that ends
// the clause. Anywhere else, as in "/homework" for "/home", it is left.
func replaceDirectory(message, dir, with string) string {
	var out strings.Builder
	for {
		at := strings.Index(message, dir)
		if at < 0 {
			out.WriteString(message)
			return out.String()
		}
		before, _ := utf8.DecodeLastRuneInString(message[:at])
		rest := message[at+len(dir):]
		starts := at == 0 || unicode.IsSpace(before) || strings.ContainsRune(`"(=[`, before)
		switch {
		case starts && rest != "" && (rest[0] == '/' || rest[0] == '\\'):
			tail := strings.IndexFunc(rest, func(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune(`"'()[]`, r) })
			if tail < 0 {
				tail = len(rest)
			}
			tail = len(strings.TrimRight(rest[:tail], ".,:;"))
			out.WriteString(message[:at] + with)
			message = rest[tail:]
		case starts && endsAPath(rest):
			out.WriteString(message[:at] + with)
			message = rest
		default:
			out.WriteString(message[:at+len(dir)])
			message = rest
		}
	}
}

// endsAPath reports whether rest, what follows a path in a sentence, ends it
// there.
func endsAPath(rest string) bool {
	if rest == "" {
		return true
	}
	next, size := utf8.DecodeRuneInString(rest)
	switch {
	case unicode.IsSpace(next) || strings.ContainsRune(`"')];:`, next):
		return true
	case next == '.' || next == ',':
		after, _ := utf8.DecodeRuneInString(rest[size:])
		return len(rest) == size || unicode.IsSpace(after)
	}
	return false
}

// withoutCustodyPaths is a sentence about the desk id's key, as a creation
// passes it on, with no path in it: the key's, its list's and every folder
// on the way to them replaced whole (`custodySpans`), and any other path from
// a root, by "…".
func (s *Server) withoutCustodyPaths(message, id string) string {
	const held = "\x00"
	message = strings.ReplaceAll(message, held, "")
	return strings.ReplaceAll(withoutAbsolutePaths(replaceSpans(message, s.custodySpans(id, held))), held, "…")
}

// custodyWords is one of Desk's own sentences about its custody, with no
// path in it: the signing folder, the secrets folder and the configuration
// folder named by those words, as Go writes them, and then every other path
// as withoutCustodyPaths replaces it.
func (s *Server) custodyWords(message string) string {
	if filepath.IsAbs(s.configDir) {
		secrets := filepath.Join(s.configDir, secretsDirName)
		message = replaceSpans(message, []pathSpan{
			{value: filepath.Join(secrets, signingDirName), with: "Desk's signing folder"},
			{value: secrets, with: "Desk's secrets folder"},
			{value: s.configDir, with: "Desk's configuration folder"},
		})
	}
	return s.withoutCustodyPaths(message, "")
}

// sweepUnfinishedKeys removes the keys of creations a stopped Desk left
// unfinished. It runs once, at start, under the registry's lock, before any
// desk is opened or any request served.
//
// It acts only on markers (`<id>.creating`) directly in the signing folder,
// never on a seed or a list without one, and never in a folder below it.
//
// **A made desk's creation is decided by what its marker records, and by
// where Desk opens desks from, never by the desks folder alone** (issue #310,
// the second line audit's finding N2). A desk published and then moved out
// of the desks folder, and opened directly, is published still: absence from
// the desks folder says nothing of it (`deskCreationLeft`).
//
//   - where a desk of that id is published where Desk opens it from, the
//     desks folder, or the folder this Desk was opened on directly, holding
//     its manifest, the desk was published and only the marker was left: the
//     marker alone is removed;
//   - otherwise, where the marker records that the manifest was never about
//     to be written, or where the desks folder holds the very folder the
//     creation made, by device and inode, with no manifest (an empty marker,
//     of an earlier Desk: a folder of that id with no manifest), the desk was
//     never published: its list, its seed and its marker are removed, in that
//     order, the marker last;
//   - otherwise, the desk may have been published and moved: nothing is
//     removed, and the log says so. A start on another project against the
//     same configuration removes no key a published desk's creation left;
//   - where anything cannot be told, because a name could not be inspected or
//     read for any reason but its absence, nothing is removed, and the log
//     says so. A manifest that cannot be read for a moment never costs a desk
//     its key.
//
// **And on the marker of the project Desk was started on**, under its own
// name (`signingKeyName`), on that project's start alone: "published" is
// there whether its jpack.json names the seed (`startupKeyNamed`), which the
// upgrade wrote only after the key was made, and a jpack.json that cannot be
// read now never costs the project its key. A marker of another project's
// name is left for a start on that project. **The key goes only under the
// name the project's own identity file holds** (`startupBound`, issue #283):
// under its path's hash, which a project moved away from that path left as
// well, a jpack.json that names no key says nothing of the project the
// creation was for, and only a marker whose seed jpack.json names is removed.
//
// **Under the signing folder's lock, taken once** (signing_lock.go). Where
// another Desk process holds it, a creation may be under way there: the sweep
// changes nothing, and leaves every marker for the next start. Where no lock
// can be taken here, it removes nothing either. Under the lock it inspects
// every name first, and removes each through the folder it holds only while
// the name still holds the file it inspected (`removeMade`): a file put in
// its place since is left, with the marker after it.
func (s *Server) sweepUnfinishedKeys() {
	dir, err := s.assistant.openSigning(false)
	if errors.Is(err, errNoSigningDir) {
		return
	}
	if err != nil {
		s.log.Printf("desk: the signing folder could not be opened to look for unfinished creations: %v", err)
		return
	}
	defer dir.Close()
	keyBetween("before desks sweep")
	unlock, err := lockSigning(dir)
	keyBetween("desks sweep tried")
	if err != nil {
		s.log.Printf("desk: unfinished creations' keys were left for the next start, because the signing folder's lock was not taken: %v", err)
		return
	}
	defer unlock()
	listing, err := dir.root.Open(".")
	if err != nil {
		return
	}
	entries, err := listing.ReadDir(registryReadLimit)
	listing.Close()
	if err != nil && len(entries) == 0 {
		return
	}
	for _, entry := range entries {
		id, isMarker := strings.CutSuffix(entry.Name(), creatingSuffix)
		if !isMarker || !deskIDPattern.MatchString(id) && !s.startupKey(id) {
			continue
		}
		// Every name first: what is removed is what was inspected here.
		names := []string{id + keysSuffix, id + seedSuffix, id + creatingSuffix}
		inspected := make([]os.FileInfo, len(names))
		for i, name := range names {
			info, err := dir.root.Lstat(name)
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				inspected = nil
				s.log.Printf("desk: an unfinished creation's key was left, because %s could not be inspected: %v", name, err)
				break
			}
			inspected[i] = info
		}
		if inspected == nil || inspected[2] == nil {
			continue
		}
		var published bool
		var left string
		if s.startupKey(id) {
			published, err = s.startupKeyNamed(filepath.Join(dir.path, id+seedSuffix))
		} else {
			published, left, err = s.deskCreationLeft(dir, id, inspected[2])
		}
		if err != nil {
			s.log.Printf("desk: an unfinished creation's key was left, because whether desk %s was made could not be told: %v", id, err)
			continue
		}
		if left != "" {
			s.log.Printf("desk: the key of an unfinished creation of desk %s was left, with its list and its marker: %s", id, left)
			continue
		}
		// **A key goes only with the transaction it was made in** (issue
		// #283; review round 1 of #296): its marker must name this project's
		// identity, held by no other folder, its folder, and the jpack.json it
		// set out to replace, as found now. Anything else leaves the key, its
		// list and its marker, and the panel says so.
		if !published && s.startupKey(id) {
			if why := s.creationBound(dir, id, inspected[2]); why != "" {
				s.log.Printf("desk: the key of an unfinished creation under this project's name, %s, was left, with its list and its marker: %s", id, why)
				continue
			}
		}
		keyBetween("sweep: inspected")
		remove := []int{0, 1, 2}
		if published {
			remove = []int{2}
		}
		for _, i := range remove {
			if err := dir.removeMade(names[i], inspected[i]); err != nil {
				s.log.Printf("desk: %s, left by an unfinished creation, was not removed: %v", names[i], err)
				break
			}
		}
		if !published {
			s.log.Printf("desk: the key of an unfinished creation of desk %s was removed", id)
		}
	}
}

// deskMovedWords is why a made desk's creation is left: it may have been
// published, and moved out of the desks folder (issue #310).
const deskMovedWords = "its marker records that its manifest was about to be written, and Desk's desks folder does not hold the folder it was made in with no manifest, as a desk published and then moved out of that folder, or opened from elsewhere, would not: Desk keeps that key, its list and its marker as they are"

// deskCreationLeft is what the creation of desk id left, its marker inspected
// as marker (issue #310): published, where a desk of that id is published
// where Desk opens desks from, the desks folder (`deskRegistered`) or the
// folder this Desk was opened on directly, holding its manifest; otherwise
// left, the words why its key stays, where it may have been published and
// moved; otherwise, never published. An error where anything that decides it
// could not be read now, or the marker is not the record Desk writes.
func (s *Server) deskCreationLeft(dir *signingDir, id string, marker os.FileInfo) (published bool, left string, err error) {
	creation, legacy, err := dir.readDeskCreation(id, marker)
	if err != nil {
		return false, "", fmt.Errorf("its marker could not be read as the record Desk writes: %w", err)
	}
	folder, published, err := s.deskRegistered(id)
	if err != nil {
		return false, "", err
	}
	if !published && s.cfg.deskID == id && s.cfg.parent == nil {
		// The desk this Desk was opened on directly, which the desks folder
		// does not hold.
		if _, err := readDeskManifest(s.root, id); err == nil {
			published = true
		} else if !errors.Is(err, errNotPublished) {
			return false, "", err
		}
	}
	switch {
	case published:
		return true, "", nil
	case !legacy && creation.Manifest == "":
		// Its manifest was never about to be written.
		return false, "", nil
	case folder != nil && (legacy || creation.Folder != "" && identityKey(folder) == creation.Folder):
		// The folder it was made in is in the desks folder, with no manifest.
		return false, "", nil
	}
	return false, deskMovedWords, nil
}

// deskRegistered is whether the registry would open desk id: its folder is one
// the registry accepts (`deskFolderAccepted`), and its manifest is one the
// registry reads (`readDeskManifest`, the same reader); and folder, that
// folder as inspected, where the desks folder holds one the registry accepts.
// published is false where the desks folder holds no folder of that id, a
// folder the registry refuses, or one with no manifest; an error where the
// folder or its manifest could not be inspected or read now, and where its
// manifest is there and empty, cut short or not a desk's, which says nothing
// of whether the desk was published (review round 1 of #315).
func (s *Server) deskRegistered(id string) (folder os.FileInfo, published bool, err error) {
	desks, err := s.assistant.root.OpenRoot("desks")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer desks.Close()
	info, err := desks.Lstat(id)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !deskFolderAccepted(info) {
		return nil, false, nil
	}
	opened, err := desks.OpenRoot(id)
	if err != nil {
		return nil, false, err
	}
	defer opened.Close()
	if held, err := opened.Stat("."); err != nil || !os.SameFile(info, held) {
		return nil, false, errors.New("its folder changed while it was being opened")
	}
	if _, err := opened.Lstat(deskManifest); errors.Is(err, fs.ErrNotExist) {
		// No manifest at all: the one state its folder says "not published"
		// by.
		return info, false, nil
	}
	_, err = readDeskManifest(opened, id)
	if errors.Is(err, errNotPublished) {
		// **A manifest there, and not one the registry reads, is not "none"**
		// (review round 1 of #315, finding 2): a manifest published and then
		// cut short, or written over, says nothing of whether the desk was
		// published. It cannot be told, and nothing is removed.
		return info, false, fmt.Errorf("its folder in the desks folder holds a manifest that is not one Desk reads: %w", err)
	}
	return info, err == nil, err
}
