package desk

// Runner's signing key (ADR-0010, section 5 and the maintainer's answer to
// its question 7; delivery row 11).
//
// # What it is
//
// Runner v0.5.0 and later take a signing key on the boot line, `signingKey`,
// check it at boot, and hand it to the runtime of each operational evaluation
// only, as JPACK_SIGNING_KEY. A run whose runtime signs keeps the signature
// sidecar, and its version-5 export carries it as `run.auditSignatures`. Desk
// keeps one such key for each desk's Runner: a key of Runner's own, never a
// project's, and never a JPACK_SIGNING_KEY inherited where Desk was started.
//
// # Where it is kept
//
// `<config>/secrets/signing/runner/<name>.seed`, with its list of public keys
// `<name>.keys.jsonl` and, while it is being made, its creation marker
// `<name>.creating`, each as a desk's key has them (signing.go), and held by
// the same custody: `runner/` is made 0700 through the signing folder's
// descriptor, and refused, never repaired, where it is a link, another
// user's, or writable by group or others. `<name>` names the desk as ADR-0010
// names a project's key: a desk Desk made by its id, and the project Desk was
// started on by the hex SHA-256 of its path, which is also the name of its
// Runner's state directory, `<config>/jobs/<name>`.
//
// No desk id can be `runner`, so a desk's files and its sweep
// (`sweepUnfinishedKeys`, which acts only on markers directly under
// `signing/` named by a desk id) never reach into `runner/`; and this file
// never lists `signing/`, and touches only the files of its own name in
// `runner/`.
//
// # When it is made, and how
//
// At a Runner's start, where nothing is kept under its name: a new desk's
// Runner starts as the desk opens, and a desk that exists already, or the
// startup desk, at its first start under this version. The key is in no
// jpack.json and no lock pins it, so nothing has to come before or after it
// in a desk's creation. The runtime Desk runs, which is also the runtime Desk
// names to Runner, writes the seed (`audit key generate`), through
// generateDeskKey: the marker first, the folder's pathname checked, the seed
// written by the runtime and checked through the folder held, the list
// written whole. The marker is then removed by identity.
//
// **A key is named to Runner only where it has no marker.** A marker is left
// only by a creation that did not finish, so a marked key has never signed
// anything, and the first start of a desk's Runner in a process removes it
// and its list (`sweepUnfinished`); the next start makes another.
//
// # What is never done
//
//   - No key is removed or made again because it could not be read, was
//     refused, or has lost its seed. Runner is then started without one, and
//     what Desk reports says why. Rotation is ADR-0010's PR 3b.
//   - No project's key, and no inherited JPACK_SIGNING_KEY, is ever named to
//     Runner: the boot line's path is built from Desk's configuration folder
//     alone, Runner's environment is LANG and LC_ALL only (`endpoint`), and
//     the runtime commands here run as a desk Desk made runs them, without
//     JPACK_SIGNING_KEY or JPACK_CONFIG.
//   - No path of the key, its list or its folders reaches a log line or the
//     page (`runnerKeyWords`).
//
// # What this does not defend against
//
// Custody's residual and the signing folder's (custody.go, signing.go): a
// process running as this user can read the seed or swap a name in a folder
// only this user can change, between Desk's check and Runner's or the
// runtime's open. Against such a process, and against the owner, who holds
// the key, a signature binds nothing. Two Desk processes over one
// configuration folder can see each other's unfinished creation as one a
// stopped Desk left: Desk keeps no lock on that folder.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

// runnerSigningDirName is the folder under `signing/` that holds the Runner
// keys, a name no desk id can take.
const runnerSigningDirName = "runner"

// runnerKeyName is the name a Runner key is kept under: a desk's id, or the
// 64 hexadecimal characters naming the startup desk's Runner state directory.
var runnerKeyName = regexp.MustCompile(`^[0-9a-f]{32}(?:[0-9a-f]{32})?$`)

// What desk-config reports of a desk's Runner key (`RunnerKeyStatus.State`).
const (
	// runnerKeySigned: Runner was started with the key; PublicKey and KeyID
	// are its public half, as the runtime read it from the seed.
	runnerKeySigned = "signed"
	// runnerKeyUnsigned: Runner was started without a key; Reason says why.
	runnerKeyUnsigned = "unsigned"
	// runnerKeyStarting: this desk's Runner has not been started yet.
	runnerKeyStarting = "starting"
)

// Why a desk's Runner was started without a key (`RunnerKeyStatus.Reason`).
// The page has a sentence for each; Detail, where there is one, is the words
// of the custody check, the runtime or Runner, with no path in them.
const (
	// The signing folder or `runner/` is not one custody keeps a key in.
	runnerKeyNoCustody = "custody"
	// The runtime did not make the key.
	runnerKeyNotMade = "not-made"
	// A creation of the key did not finish; the next start of Desk removes
	// what it left.
	runnerKeyUnfinished = "unfinished"
	// The list of public keys is kept, and the seed is not.
	runnerKeyLost = "lost"
	// The seed, its list or its marker could not be inspected or read now.
	runnerKeyNotNow = "not-read-now"
	// The seed or its list is not one Desk names to Runner.
	runnerKeyNotUsed = "not-used"
	// The runtime refuses the seed.
	runnerKeyRuntimeRefused = "runtime-refused"
	// Runner refused the key at its boot.
	runnerKeyRunnerRefused = "runner-refused"
)

// RunnerKeyStatus is what `GET /api/desk-config` reports of the key this
// desk's Runner signs its runs with, under `jobs.runnerKey`. It never carries
// a path.
type RunnerKeyStatus struct {
	State     string `json:"state"`
	PublicKey string `json:"publicKey,omitempty"`
	KeyID     string `json:"keyId,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

// runnerKey is the key Desk keeps for one desk's Runner, and what its last
// start found.
type runnerKey struct {
	s    *Server
	name string

	// swept is set once this process has looked for an unfinished creation
	// under name (`sweepUnfinished`). Read and written under the companion's
	// lock, which every start holds.
	swept bool

	mu     sync.Mutex
	status RunnerKeyStatus
	// said is the status last written to Desk's log, so that a Runner that
	// is started again and again is not logged again and again.
	said RunnerKeyStatus
}

// newRunnerKey is the key Desk keeps for the Runner whose workspace is name.
func (s *Server) newRunnerKey(name string) *runnerKey {
	return &runnerKey{s: s, name: name, status: RunnerKeyStatus{State: runnerKeyStarting}}
}

// keyStatus is what desk-config says of this Runner's key; nil where this
// desk has no Runner.
func (j *jobsCompanion) keyStatus() *RunnerKeyStatus {
	if j == nil {
		return nil
	}
	return j.key.report()
}

// report is what desk-config says of the key; nil where there is none.
func (k *runnerKey) report() *RunnerKeyStatus {
	if k == nil {
		return nil
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	status := k.status
	return &status
}

// set records what a start found, and says it in Desk's log where it changed.
func (k *runnerKey) set(status RunnerKeyStatus) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.status = status
	if status == k.said {
		return
	}
	k.said = status
	switch status.State {
	case runnerKeySigned:
		k.s.log.Printf("desk: Runner signs this desk's runs with the key Desk keeps for it, keyId %s", status.KeyID)
	case runnerKeyUnsigned:
		k.s.log.Printf("desk: Runner was started without a signing key, so this desk's runs are not signed (%s): %s", status.Reason, status.Detail)
	}
}

// unsigned is a start without a key, for reason, with detail.
func unsignedRunner(reason, detail string) RunnerKeyStatus {
	return RunnerKeyStatus{State: runnerKeyUnsigned, Reason: reason, Detail: strings.TrimRight(detail, ".")}
}

// prepare is the path of the key to name to Runner on the boot line, or ""
// where Runner is to start without one; what it found is reported. It runs
// before each start, under the companion's lock.
func (k *runnerKey) prepare() string {
	if k == nil {
		return ""
	}
	status, path := k.examine(context.Background())
	k.set(status)
	return path
}

// refusedByRunner records that Runner refused the key at boot, in its words.
// The key is left as it is.
func (k *runnerKey) refusedByRunner(words string) {
	if k == nil {
		return
	}
	k.set(unsignedRunner(runnerKeyRunnerRefused, k.s.runnerKeyWords(words, k.name, false)))
}

// examine finds the key kept under k.name, makes it where nothing is kept,
// and answers what Runner is to be started with: the seed's absolute path
// where every check holds, "" otherwise, and the status either way.
func (k *runnerKey) examine(ctx context.Context) (RunnerKeyStatus, string) {
	s := k.s
	if !runnerKeyName.MatchString(k.name) {
		return unsignedRunner(runnerKeyNoCustody, "this desk's Runner has no name a key can be kept under"), ""
	}
	project, ok := s.runnerKeyRuntimeDir()
	if !ok {
		return unsignedRunner(runnerKeyNoCustody, "this desk holds no folder to run the runtime in"), ""
	}
	dir, err := s.assistant.openRunnerSigning()
	if err != nil {
		return unsignedRunner(runnerKeyNoCustody, s.runnerKeyWords(err.Error(), k.name, true)), ""
	}
	defer dir.Close()
	if !utf8.ValidString(dir.path) {
		return unsignedRunner(runnerKeyNoCustody, "the path of Desk's signing folder is not valid UTF-8, which Runner's boot line cannot carry"), ""
	}
	if !k.swept {
		if err := k.sweepUnfinished(dir); err != nil {
			return unsignedRunner(runnerKeyNotNow, s.runnerKeyWords(err.Error(), k.name, true)), ""
		}
		k.swept = true
	}
	seedName, keysName, markerName := k.name+seedSuffix, k.name+keysSuffix, k.name+creatingSuffix
	// **A marked key is never named.** Its creation did not finish, so it has
	// signed nothing, and the next start of Desk removes it.
	switch _, err := dir.root.Lstat(markerName); {
	case err == nil:
		return unsignedRunner(runnerKeyUnfinished, ""), ""
	case !errors.Is(err, fs.ErrNotExist):
		return unsignedRunner(runnerKeyNotNow, "its creation marker could not be inspected"), ""
	}
	seed, err := dir.root.Lstat(seedName)
	if errors.Is(err, fs.ErrNotExist) {
		// **Made only where nothing is kept.** A list without its seed is a
		// lost key, and is said; nothing is made in its place.
		_, listErr := dir.root.Lstat(keysName)
		switch {
		case listErr == nil:
			return unsignedRunner(runnerKeyLost, ""), ""
		case !errors.Is(listErr, fs.ErrNotExist):
			return unsignedRunner(runnerKeyNotNow, "its list of public keys could not be inspected"), ""
		}
		made, err := generateDeskKey(ctx, s.cfg.JpackBin, project, dir, k.name)
		if err != nil {
			why := strings.TrimPrefix(err.Error(), "the runtime did not generate its signing key: ")
			return unsignedRunner(runnerKeyNotMade, s.runnerKeyWords(why, k.name, false)), ""
		}
		if err := made.settle(); err != nil {
			s.log.Printf("desk: Runner's new signing key keeps its creation marker, so it is not named to Runner; the next start removes it: %s", s.runnerKeyWords(err.Error(), k.name, true))
			return unsignedRunner(runnerKeyUnfinished, ""), ""
		}
		seed, err = dir.root.Lstat(seedName)
	}
	if err != nil {
		return unsignedRunner(runnerKeyNotNow, "the key could not be inspected"), ""
	}
	if err := checkSeed("the key", seed); err != nil {
		return unsignedRunner(runnerKeyNotUsed, err.Error()), ""
	}
	keys, found, err := dir.readKeys(keysName)
	switch {
	case err != nil:
		return unsignedRunner(runnerKeyNotUsed, "its list of public keys was not read: "+err.Error()), ""
	case !found:
		return unsignedRunner(runnerKeyNotUsed, "Desk keeps no list of its public keys"), ""
	case len(keys) != 1:
		return unsignedRunner(runnerKeyNotUsed, "its list of public keys holds more than one key, and this version of Desk keeps one key for Runner and rotates none"), ""
	}
	// The pathname Runner and the runtime are given must name the folder and
	// the seed found through it.
	if dir.namesHeld() != nil || dir.namesFile(seedName, seed) != nil {
		return unsignedRunner(runnerKeyNotUsed, "the key is not at the path Runner would be given"), ""
	}
	path := filepath.Join(dir.path, seedName)
	public, status := s.readRunnerKey(ctx, project, path, k.name)
	if status != nil {
		return *status, ""
	}
	if public.PublicKey != keys[0].PublicKey || public.KeyID != keys[0].KeyID {
		return unsignedRunner(runnerKeyNotUsed, "its list of public keys does not name it"), ""
	}
	if dir.namesFile(seedName, seed) != nil {
		return unsignedRunner(runnerKeyNotUsed, "the key is not at the path Runner would be given"), ""
	}
	return RunnerKeyStatus{State: runnerKeySigned, PublicKey: public.PublicKey, KeyID: public.KeyID}, path
}

// readRunnerKey is the runtime's word on the seed at path: its public half,
// where the runtime reads it as a key under its own rules, the directories on
// its path among them (`jpack audit key public <seed> --format json`); or why
// Runner is to start without it.
func (s *Server) readRunnerKey(ctx context.Context, project heldDir, path, name string) (deskPublicKey, *RunnerKeyStatus) {
	out, runErr := runRuntime(ctx, s.cfg.JpackBin, project, "audit", "key", "public", path, "--format", "json")
	var answer struct {
		Command     string              `json:"command"`
		Status      string              `json:"status"`
		PublicKey   string              `json:"publicKey"`
		KeyID       string              `json:"keyId"`
		Diagnostics []runtimeDiagnostic `json:"diagnostics"`
	}
	decoded := out != nil && json.Unmarshal(out, &answer) == nil
	if runErr == nil && decoded && answer.Command == "audit key public" && answer.Status == "read" {
		public := deskPublicKey{PublicKey: answer.PublicKey, KeyID: answer.KeyID}
		if public.check() != nil {
			status := unsignedRunner(runnerKeyNotNow, "the runtime's audit key public did not answer as documented")
			return deskPublicKey{}, &status
		}
		return public, nil
	}
	var said []string
	for _, diagnostic := range answer.Diagnostics {
		if message := strings.TrimSpace(diagnostic.Message); message != "" {
			said = append(said, message)
		}
	}
	var exit *exec.ExitError
	if decoded && len(said) > 0 && errors.As(runErr, &exit) {
		status := unsignedRunner(runnerKeyRuntimeRefused, s.runnerKeyWords(strings.Join(said, " "), name, false))
		return deskPublicKey{}, &status
	}
	why := "the runtime's audit key public did not answer as documented"
	if runErr != nil {
		why = runErr.Error()
	}
	status := unsignedRunner(runnerKeyNotNow, s.runnerKeyWords(why, name, false))
	return deskPublicKey{}, &status
}

// sweepUnfinished removes what a creation of this Runner's key left
// unfinished: where `<name>.creating` is in `runner/`, the list, the seed and
// the marker of that name, through the folder held. It acts on its own name
// only, and on nothing a marker does not name. A marker that cannot be
// inspected leaves everything as it is, and is an error.
func (k *runnerKey) sweepUnfinished(dir *signingDir) error {
	marker := k.name + creatingSuffix
	_, err := dir.root.Lstat(marker)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.New("its creation marker could not be inspected, so nothing was removed")
	}
	for _, name := range []string{k.name + keysSuffix, k.name + seedSuffix, marker} {
		if err := dir.root.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("what an unfinished creation of its key left could not all be removed: %w", err)
		}
	}
	k.s.log.Print("desk: the signing key of an unfinished creation for this desk's Runner was removed; another is made now")
	return nil
}

// openRunnerSigning validates `secrets/signing/runner/` and holds it, as
// openSigning holds `signing/`, through the signing folder's descriptor:
// made 0700 where it is missing, narrowed to 0700, and refused, never
// repaired, where it is a link, not a directory, another user's, or could be
// written by group or other; and the folder opened must be the folder
// checked.
func (a *assistantStore) openRunnerSigning() (*signingDir, error) {
	signing, err := a.openSigning(true)
	if err != nil {
		return nil, err
	}
	defer signing.Close()
	path := filepath.Join(signing.path, runnerSigningDirName)
	if err := signing.root.Mkdir(runnerSigningDirName, custodyDirMode); err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, fmt.Errorf("%s could not be created: %w", path, err)
	}
	if err := ensureOwnedDirectoryIn(signing.root, signing.path, runnerSigningDirName); err != nil {
		return nil, err
	}
	checked, err := signing.root.Lstat(runnerSigningDirName)
	if err != nil {
		return nil, fmt.Errorf("%s could not be inspected: %w", path, err)
	}
	if err := safeDirectory(path, checked, true); err != nil {
		return nil, err
	}
	afterCustodyCheck(path)
	root, err := signing.root.OpenRoot(runnerSigningDirName)
	if err != nil {
		return nil, fmt.Errorf("%s could not be opened: %w", path, err)
	}
	if opened, err := root.Stat("."); err != nil || !os.SameFile(checked, opened) {
		root.Close()
		return nil, fmt.Errorf("%s changed between being checked and being opened, and was not used", path)
	}
	return &signingDir{root: root, path: path}, nil
}

// runnerKeyRuntimeDir is the folder the runtime's key commands run in: this
// desk's project, held, and never marked as the startup desk's, so that the
// commands never inherit JPACK_SIGNING_KEY (`runRuntime`).
func (s *Server) runnerKeyRuntimeDir() (heldDir, bool) {
	if s.project == nil || s.project.own == nil || s.project.own.dirFile == nil {
		return heldDir{}, false
	}
	return heldDir{file: s.project.own.dirFile, path: s.projectDir, info: s.project.info}, true
}

// runnerKeyWords is a sentence about the Runner key kept under name, with no
// path in it. With named, Desk's own folders are named by name, as
// custodyWords names them, and the Runner keys' folder too; then the key's
// path, its list's and every folder on the way to them are replaced by "…",
// whatever their spelling (`pathSpans`), and every other path from a root.
func (s *Server) runnerKeyWords(message, name string, named bool) string {
	if !filepath.IsAbs(s.configDir) {
		return withoutAbsolutePaths(message)
	}
	runner := filepath.Join(s.configDir, secretsDirName, signingDirName, runnerSigningDirName)
	if named {
		message = replaceSpans(message, []pathSpan{{value: runner, with: "the folder Desk keeps Runner's keys in"}})
		message = s.custodyWords(message)
	}
	const held = "\x00"
	message = strings.ReplaceAll(message, held, "")
	spans := append(pathSpans(filepath.Join(runner, name+seedSuffix), false, held), pathSpans(filepath.Join(runner, name+keysSuffix), false, held)...)
	spans = append(spans, pathSpans(runner, true, held)...)
	spans = append(spans, s.custodySpans("", held)...)
	return strings.ReplaceAll(withoutAbsolutePaths(replaceSpans(message, spans)), held, "…")
}

// runnerKeyRefusedPrefix begins the line Runner writes to its standard error,
// before it exits, where it refuses the boot line's signing key (Runner
// v0.5.0, `runner.Open` and `cmd/jpack-runner`); the reason follows it. Its
// refusals never name the path.
const runnerKeyRefusedPrefix = "runner: the signing key is refused: "

// runnerSaidLimit is the most of a Runner's standard error Desk keeps: enough
// for the line it writes before it exits.
const runnerSaidLimit = 4 << 10

// runnerKeyRefusal is Runner's reason for refusing the signing key, where
// what it wrote to its standard error says it refused it.
func runnerKeyRefusal(said []byte) (string, bool) {
	for _, line := range strings.Split(string(said), "\n") {
		if reason, found := strings.CutPrefix(line, runnerKeyRefusedPrefix); found {
			return strings.ToValidUTF8(strings.TrimSpace(reason), "?"), true
		}
	}
	return "", false
}
