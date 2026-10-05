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
// # One lock for all of Desk's key custody
//
// Every decision to make, remove or name a Runner key is taken under an
// exclusive lock on the signing folder Desk holds: a `flock` on that folder's
// own descriptor (`lockPrivateData`), so the lock follows the folder and not
// a name, and it excludes another Desk process, and another desk's Runner in
// this one, alike. A start's sweep of the desks' keys takes the same lock
// (`sweepUnfinishedKeysLocked`). The lock is held from the first inspection
// to the last effect of the decision, and released before Runner is started:
//
//   - a sweep, a start that finds a creation marker, does not wait: where
//     the lock is held, it changes nothing, and Runner starts without a key;
//   - any other start waits for it a bounded time (`runnerKeyCreationWait`),
//     and then starts Runner without a key, changing nothing;
//   - a build that cannot take the lock names no key.
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
// written whole. The marker is then removed by identity. All of it under the
// lock.
//
// **A key is named to Runner only where, under the lock, it has no marker.**
// A creation holds the lock from before its marker is written to after the
// marker is removed, so a marker seen under the lock is never a creation in
// progress: it is one that was stopped, or whose marker could not be
// removed, and either way that key was never named, and never signed
// anything. So it is removed at whatever start finds it (`removeUnfinished`):
// every name inspected first, then removed through the folder held, each
// only while it is the file inspected, the marker last, so that a removal
// that stops leaves the marker for the next start to finish. Another key is
// then made. Nothing else ever removes a Runner key.
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
// process running as this user, and not Desk, can read the seed or swap a
// name in a folder only this user can change. Runner opens the seed by the
// path it is given after the lock is released; what holds then is Runner's
// own check of that path at boot (absolute, no link, one name, the user's,
// readable by nobody else), the runtime's own check each time it signs, and
// Desk's checks, just before the lock is released, that the path still names
// the folder and the seed it found. Against such a process, and against the
// owner, who holds the key, a signature binds nothing.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

// runnerSigningDirName is the folder under `signing/` that holds the Runner
// keys, a name no desk id can take.
const runnerSigningDirName = "runner"

// runnerKeyName is the name a Runner key is kept under: a desk's id, or the
// 64 hexadecimal characters naming the startup desk's Runner state directory.
var runnerKeyName = regexp.MustCompile(`^[0-9a-f]{32}(?:[0-9a-f]{32})?$`)

// What Desk reports of a desk's Runner key (`RunnerKeyStatus.State`).
const (
	// runnerKeyStarting: Runner is being started, or has not been yet.
	runnerKeyStarting = "starting"
	// runnerKeySigned: Runner started, and answered, with the key on its boot
	// line; PublicKey and KeyID are its public half, as the runtime read it
	// from the seed.
	runnerKeySigned = "signed"
	// runnerKeyUnsigned: Runner started, and answered, without a key; Reason
	// says why.
	runnerKeyUnsigned = "unsigned"
	// runnerKeyNotRunning: Runner did not start; Detail says why.
	runnerKeyNotRunning = "not-running"
)

// Why a desk's Runner was started without a key (`RunnerKeyStatus.Reason`).
// The page has a sentence for each; Detail, where there is one, is the words
// of the custody check, the runtime or Runner, with no path in them.
const (
	// The signing folder or `runner/` is not one custody keeps a key in.
	runnerKeyNoCustody = "custody"
	// The runtime did not make the key.
	runnerKeyNotMade = "not-made"
	// A creation of the key could not remove its marker; the next start
	// removes what it left, and makes another.
	runnerKeyUnfinished = "unfinished"
	// The list of public keys is kept, and the seed is not.
	runnerKeyLost = "lost"
	// The seed, its list, its marker, their folder or the lock could not be
	// inspected, taken or read now.
	runnerKeyNotNow = "not-read-now"
	// The signing folder's lock was held by another start.
	runnerKeyInUse = "in-use"
	// The seed or its list is not one Desk names to Runner.
	runnerKeyNotUsed = "not-used"
	// The runtime refuses the seed.
	runnerKeyRuntimeRefused = "runtime-refused"
	// Runner refused the key at its boot.
	runnerKeyRunnerRefused = "runner-refused"
)

// RunnerKeyStatus is what Desk reports of the key this desk's Runner signs
// its runs with: in `GET /api/desk-config` under `jobs.runnerKey`, and in
// `GET /api/runner-key`, which the page polls. It never carries a path.
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

	mu     sync.Mutex
	status RunnerKeyStatus
	// said is the status last written to Desk's log, so that a Runner that
	// is started again and again is not logged again and again.
	said RunnerKeyStatus
}

// newRunnerKey is the key Desk keeps for the Runner whose workspace is name.
// Until its Runner has started and answered, it is reported as starting.
func (s *Server) newRunnerKey(name string) *runnerKey {
	return &runnerKey{s: s, name: name, status: RunnerKeyStatus{State: runnerKeyStarting}}
}

// keyStatus is what Desk reports of this Runner's key; nil where this desk
// has no Runner.
func (j *jobsCompanion) keyStatus() *RunnerKeyStatus {
	if j == nil {
		return nil
	}
	return j.key.report()
}

// report is what Desk reports of the key; nil where there is none.
func (k *runnerKey) report() *RunnerKeyStatus {
	if k == nil {
		return nil
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	status := k.status
	return &status
}

// set records the state of the key, and says it in Desk's log where it
// changed. Starting is not said.
func (k *runnerKey) set(status RunnerKeyStatus) {
	if k == nil {
		return
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	k.status = status
	if status.State == runnerKeyStarting || status == k.said {
		return
	}
	k.said = status
	switch status.State {
	case runnerKeySigned:
		k.s.log.Printf("desk: Runner signs this desk's runs with the key Desk keeps for it, keyId %s", status.KeyID)
	case runnerKeyUnsigned:
		k.s.log.Printf("desk: Runner was started without a signing key, so this desk's runs are not signed (%s): %s", status.Reason, status.Detail)
	case runnerKeyNotRunning:
		k.s.log.Printf("desk: Runner did not start, so this desk runs and signs nothing now: %s", status.Detail)
	}
}

// unsignedRunner is a start without a key, for reason, with detail.
func unsignedRunner(reason, detail string) RunnerKeyStatus {
	return RunnerKeyStatus{State: runnerKeyUnsigned, Reason: reason, Detail: strings.TrimRight(detail, ".")}
}

// prepare is the path of the key to name to Runner on the boot line, or ""
// where Runner is to start without one, and the state to report once Runner
// has started and answered (`started`). Until then the key is reported as
// starting: a key is never reported as signing before Runner has taken it.
// It runs before each start, under the companion's lock.
func (k *runnerKey) prepare() (string, RunnerKeyStatus) {
	if k == nil {
		return "", RunnerKeyStatus{}
	}
	k.set(RunnerKeyStatus{State: runnerKeyStarting})
	status, path := k.examine(context.Background())
	return path, status
}

// started records what Runner started and answered with: decided, which
// prepare or refused gave.
func (k *runnerKey) started(decided RunnerKeyStatus) { k.set(decided) }

// refused is the state of a Runner that refused the key at boot, in its
// words, and was started again without it. The key is left as it is.
func (k *runnerKey) refused(words string) RunnerKeyStatus {
	if k == nil {
		return RunnerKeyStatus{}
	}
	return unsignedRunner(runnerKeyRunnerRefused, k.s.runnerKeyWords(words, k.name, false))
}

// notRunning records that Runner did not start, and why, with no path.
func (k *runnerKey) notRunning(err error) {
	if k == nil {
		return
	}
	k.set(RunnerKeyStatus{State: runnerKeyNotRunning, Detail: strings.TrimRight(k.s.runnerKeyWords(err.Error(), k.name, false), ".")})
}

// handleRunnerKey answers `GET /api/runner-key`: what this desk's Runner
// signs its runs with, or why it signs none, as the page shows it in Help &
// About and asks again while it is open. `null` where this desk has no Runner.
func (s *Server) handleRunnerKey(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, struct {
		RunnerKey *RunnerKeyStatus `json:"runnerKey"`
	}{s.jobs.keyStatus()})
}

// runnerKeyNames are the three names a Runner key is kept under in `runner/`.
type runnerKeyNames struct{ seed, keys, marker string }

// runnerKeyFound is what an inspection found at each of them: nil where
// nothing is there.
type runnerKeyFound struct{ seed, keys, marker os.FileInfo }

// examine finds the key kept under k.name, under the lock, and decides: the
// seed's absolute path where every check holds, "" otherwise, and the state
// to report once Runner has started either way.
func (k *runnerKey) examine(ctx context.Context) (RunnerKeyStatus, string) {
	s := k.s
	if !runnerKeyName.MatchString(k.name) {
		return unsignedRunner(runnerKeyNoCustody, "this desk's Runner has no name a key can be kept under"), ""
	}
	project, ok := s.runnerKeyRuntimeDir()
	if !ok {
		return unsignedRunner(runnerKeyNoCustody, "this desk holds no folder to run the runtime in"), ""
	}
	signing, dir, err := s.assistant.openRunnerSigning()
	if err != nil {
		var notNow runnerKeyNotNowError
		if errors.As(err, &notNow) {
			return unsignedRunner(runnerKeyNotNow, s.runnerKeyWords(err.Error(), k.name, true)), ""
		}
		return unsignedRunner(runnerKeyNoCustody, s.runnerKeyWords(err.Error(), k.name, true)), ""
	}
	defer signing.Close()
	defer dir.Close()
	if !utf8.ValidString(dir.path) {
		return unsignedRunner(runnerKeyNoCustody, "the path of Desk's signing folder is not valid UTF-8, which Runner's boot line cannot carry"), ""
	}
	names := runnerKeyNames{seed: k.name + seedSuffix, keys: k.name + keysSuffix, marker: k.name + creatingSuffix}
	// **A sweep never waits for the lock.** Whether this start would remove
	// an unfinished creation is seen before the lock only to choose how long
	// to wait for it; what is done is decided again under it.
	wait := runnerKeyCreationWait
	if _, err := lstatRunnerKey(dir, names.marker); err == nil {
		wait = 0
	}
	unlock, err := lockKeyCustody(signing, wait)
	switch {
	case errors.Is(err, errKeyCustodyInUse):
		return unsignedRunner(runnerKeyInUse, ""), ""
	case err != nil:
		return unsignedRunner(runnerKeyNotNow, s.runnerKeyWords("the folder Desk keeps signing keys in could not be locked: "+err.Error(), k.name, true)), ""
	}
	defer unlock()
	// **Every name inspected before anything is decided.** Anything but
	// "not there" for any of the three is "could not be read now": nothing is
	// removed, made or named.
	found, err := inspectRunnerKey(dir, names)
	if err != nil {
		return unsignedRunner(runnerKeyNotNow, err.Error()), ""
	}
	if found.marker != nil {
		keyBetween("before removal")
		if err := k.removeUnfinished(dir, names, found); err != nil {
			return unsignedRunner(runnerKeyNotNow, s.runnerKeyWords(err.Error(), k.name, true)), ""
		}
		found = runnerKeyFound{}
	}
	seed := found.seed
	if seed == nil {
		// **Made only where nothing is kept.** A list without its seed is a
		// lost key, and is said; nothing is made in its place.
		if found.keys != nil {
			return unsignedRunner(runnerKeyLost, ""), ""
		}
		if status, made := k.make(ctx, project, dir); !made {
			return status, ""
		}
		if seed, err = lstatRunnerKey(dir, names.seed); err != nil {
			return unsignedRunner(runnerKeyNotNow, "the key could not be inspected"), ""
		}
	}
	if err := checkSeed("the key", seed); err != nil {
		return unsignedRunner(runnerKeyNotUsed, err.Error()), ""
	}
	keys, listed, err := dir.readKeys(names.keys)
	switch {
	case err != nil:
		return unsignedRunner(runnerKeyNotUsed, "its list of public keys was not read: "+err.Error()), ""
	case !listed:
		return unsignedRunner(runnerKeyNotUsed, "Desk keeps no list of its public keys"), ""
	case len(keys) != 1:
		return unsignedRunner(runnerKeyNotUsed, "its list of public keys holds more than one key, and this version of Desk keeps one key for Runner and rotates none"), ""
	}
	// The pathname Runner and the runtime are given must name the folder and
	// the seed found through it.
	if dir.namesHeld() != nil || dir.namesFile(names.seed, seed) != nil {
		return unsignedRunner(runnerKeyNotUsed, "the key is not at the path Runner would be given"), ""
	}
	path := filepath.Join(dir.path, names.seed)
	public, status := s.readRunnerKey(ctx, project, path, k.name)
	if status != nil {
		return *status, ""
	}
	if public.PublicKey != keys[0].PublicKey || public.KeyID != keys[0].KeyID {
		return unsignedRunner(runnerKeyNotUsed, "its list of public keys does not name it"), ""
	}
	// Last, still under the lock: the path still names the seed found. After
	// the lock is released, Runner opens it by that path (see "What this does
	// not defend against").
	if dir.namesFile(names.seed, seed) != nil {
		return unsignedRunner(runnerKeyNotUsed, "the key is not at the path Runner would be given"), ""
	}
	return RunnerKeyStatus{State: runnerKeySigned, PublicKey: public.PublicKey, KeyID: public.KeyID}, path
}

// inspectRunnerKey inspects all three names, not following a link. Absence is
// an answer; any other failure is an error that names what could not be
// inspected, and no name.
func inspectRunnerKey(dir *signingDir, names runnerKeyNames) (runnerKeyFound, error) {
	var found runnerKeyFound
	for _, each := range []struct {
		name string
		into *os.FileInfo
		what string
	}{{names.marker, &found.marker, "its creation marker"}, {names.seed, &found.seed, "the key"}, {names.keys, &found.keys, "its list of public keys"}} {
		info, err := lstatRunnerKey(dir, each.name)
		switch {
		case err == nil:
			*each.into = info
		case !errors.Is(err, fs.ErrNotExist):
			return runnerKeyFound{}, fmt.Errorf("%s could not be inspected, so nothing was removed or made", each.what)
		}
	}
	return found, nil
}

// removeUnfinished removes what a creation that did not finish left under
// this name, under the lock: the list, the seed and then the marker, each
// through the folder held and only while it is the file just inspected
// (`removeMade`). The marker goes last, so a removal that stops at any point
// leaves it, and the next start, under the lock, finishes the job.
func (k *runnerKey) removeUnfinished(dir *signingDir, names runnerKeyNames, found runnerKeyFound) error {
	for _, each := range []struct {
		name string
		info os.FileInfo
	}{{names.keys, found.keys}, {names.seed, found.seed}, {names.marker, found.marker}} {
		if each.info == nil {
			continue
		}
		if err := runnerKeyIO("remove " + each.name); err != nil {
			return fmt.Errorf("what an unfinished creation of its key left could not all be removed: %w", err)
		}
		if err := dir.removeMade(each.name, each.info); err != nil {
			return fmt.Errorf("what an unfinished creation of its key left could not all be removed: %w", err)
		}
	}
	k.s.log.Print("desk: the signing key of an unfinished creation for this desk's Runner was removed; another is made now")
	return nil
}

// make has the runtime make the key in dir, through generateDeskKey, and
// removes its creation marker, all under the lock; or says why there is no
// key to name.
func (k *runnerKey) make(ctx context.Context, project heldDir, dir *signingDir) (RunnerKeyStatus, bool) {
	s := k.s
	made, err := generateDeskKey(ctx, s.cfg.JpackBin, project, dir, k.name)
	if err != nil {
		why := strings.TrimPrefix(err.Error(), "the runtime did not generate its signing key: ")
		return unsignedRunner(runnerKeyNotMade, s.runnerKeyWords(why, k.name, false)), false
	}
	// **Named only once its marker is gone.** A marker that stays marks a key
	// that was never named, which the next start removes.
	if err := made.settle(); err != nil {
		s.log.Printf("desk: Runner's new signing key keeps its creation marker, so it is not named to Runner; the next start removes it: %s", s.runnerKeyWords(err.Error(), k.name, true))
		return unsignedRunner(runnerKeyUnfinished, ""), false
	}
	return RunnerKeyStatus{}, true
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

// runnerKeyCreationWait is how long a start that may make or name a key
// waits for the key-custody lock before it starts Runner without one. A
// variable only so that a test can shorten it.
var runnerKeyCreationWait = 10 * time.Second

// runnerKeyLockPoll is how often a start that waits asks for the lock again.
const runnerKeyLockPoll = 25 * time.Millisecond

// errKeyCustodyInUse is the key-custody lock held by another start, for
// longer than this one waits.
var errKeyCustodyInUse = errors.New("the folder Desk keeps signing keys in was in use")

// lockKeyCustody takes the one exclusive lock for Desk's key custody: a
// `flock` on the descriptor of the signing folder held (`lockPrivateData`),
// so that it follows the folder Desk holds and not a name. It asks at once,
// and again until wait has passed; with no wait, once. A lock held elsewhere
// all that time is errKeyCustodyInUse; any other failure, a build that
// cannot lock among them, is returned as itself. Either way nothing is held.
// The lock is released by the function it answers, and by nothing else.
func lockKeyCustody(signing *signingDir, wait time.Duration) (func(), error) {
	if err := runnerKeyIO("lock"); err != nil {
		return nil, err
	}
	file, err := signing.root.Open(".")
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(wait)
	for busy := false; ; {
		err := lockPrivateData(file, true)
		if err == nil {
			return func() { file.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) && !errors.Is(err, syscall.EINTR) {
			file.Close()
			return nil, err
		}
		if !busy {
			busy = true
			keyBetween("lock busy")
		}
		if !time.Now().Before(deadline) {
			file.Close()
			return nil, errKeyCustodyInUse
		}
		time.Sleep(runnerKeyLockPoll)
	}
}

// sweepUnfinishedKeysLocked is the start's sweep of the desks' unfinished
// keys (`sweepUnfinishedKeys`, signing.go), under the key-custody lock, so
// that it and a Runner key's decisions never run at once. A sweep that cannot
// take the lock at once does not wait: it leaves everything for the next
// start, and says so.
func (s *Server) sweepUnfinishedKeysLocked() {
	signing, err := s.assistant.openSigning(false)
	if errors.Is(err, errNoSigningDir) {
		return
	}
	if err != nil {
		s.log.Printf("desk: the signing folder could not be opened to look for unfinished creations: %s", s.custodyWords(err.Error()))
		return
	}
	defer signing.Close()
	unlock, err := lockKeyCustody(signing, 0)
	if err != nil {
		s.log.Printf("desk: unfinished creations of desks' keys were not looked for, and are left for the next start: %s", s.custodyWords(err.Error()))
		return
	}
	defer unlock()
	s.sweepUnfinishedKeys()
}

// runnerKeyNotNowError is a failure to inspect or open `runner/` now, as
// against a folder custody refuses.
type runnerKeyNotNowError struct{ err error }

func (e runnerKeyNotNowError) Error() string { return e.err.Error() }
func (e runnerKeyNotNowError) Unwrap() error { return e.err }

// openRunnerSigning validates `secrets/signing/` and `secrets/signing/runner/`
// and holds both: the signing folder as openSigning holds it, and `runner/`
// through its descriptor, made 0700 where it is missing, narrowed to 0700,
// and refused, never repaired, where it is a link, not a directory, another
// user's, or could be written by group or other; and the folder opened must
// be the folder checked. The caller closes both.
func (a *assistantStore) openRunnerSigning() (*signingDir, *signingDir, error) {
	signing, err := a.openSigning(true)
	if err != nil {
		return nil, nil, err
	}
	failed := func(err error) (*signingDir, *signingDir, error) {
		signing.Close()
		return nil, nil, err
	}
	path := filepath.Join(signing.path, runnerSigningDirName)
	if err := signing.root.Mkdir(runnerSigningDirName, custodyDirMode); err != nil && !errors.Is(err, fs.ErrExist) {
		return failed(fmt.Errorf("%s could not be created: %w", path, err))
	}
	if err := ensureOwnedDirectoryIn(signing.root, signing.path, runnerSigningDirName); err != nil {
		return failed(err)
	}
	if err := runnerKeyIO(runnerSigningDirName); err != nil {
		return failed(runnerKeyNotNowError{fmt.Errorf("%s could not be inspected: %w", path, err)})
	}
	checked, err := signing.root.Lstat(runnerSigningDirName)
	if err != nil {
		return failed(runnerKeyNotNowError{fmt.Errorf("%s could not be inspected: %w", path, err)})
	}
	if err := safeDirectory(path, checked, true); err != nil {
		return failed(err)
	}
	afterCustodyCheck(path)
	root, err := signing.root.OpenRoot(runnerSigningDirName)
	if err != nil {
		return failed(runnerKeyNotNowError{fmt.Errorf("%s could not be opened: %w", path, err)})
	}
	if opened, err := root.Stat("."); err != nil || !os.SameFile(checked, opened) {
		root.Close()
		return failed(fmt.Errorf("%s changed between being checked and being opened, and was not used", path))
	}
	return signing, &signingDir{root: root, path: path}, nil
}

// testHookRunnerKeyIO runs before each step of a Runner key's that a failure
// can stop, and is nil outside tests: the inspection of its seed, list or
// marker (by name), of `runner/` ("runner"), the taking of the lock ("lock")
// and each removal ("remove <name>"). An error it returns stands for that
// step failing now, such as an I/O error, so a test can tell "could not be
// done now" from "not there", and stop a removal part way.
var testHookRunnerKeyIO func(step string) error

func runnerKeyIO(step string) error {
	if testHookRunnerKeyIO != nil {
		return testHookRunnerKeyIO(step)
	}
	return nil
}

// lstatRunnerKey inspects name in the folder held, not following a link.
func lstatRunnerKey(dir *signingDir, name string) (fs.FileInfo, error) {
	if err := runnerKeyIO(name); err != nil {
		return nil, err
	}
	return dir.root.Lstat(name)
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
// path in it: the key's path and its list's, whole, by "…", wherever they
// stand and however they are spelled (`pathSpans`); with named, Desk's own
// folders by name, as custodyWords names them, and the Runner keys' folder
// too; then every folder on the way to the key by "…", and every other path
// from a root.
func (s *Server) runnerKeyWords(message, name string, named bool) string {
	if !filepath.IsAbs(s.configDir) {
		return withoutAbsolutePaths(message)
	}
	runner := filepath.Join(s.configDir, secretsDirName, signingDirName, runnerSigningDirName)
	var files []pathSpan
	for _, path := range []string{filepath.Join(runner, name+seedSuffix), filepath.Join(runner, name+keysSuffix)} {
		for _, span := range pathSpans(path, false, "…") {
			if !span.directory {
				files = append(files, span)
			}
		}
	}
	message = replaceSpans(message, files)
	if named {
		message = s.custodyWords(replaceSpans(message, []pathSpan{{value: runner, with: "the folder Desk keeps Runner's keys in"}}))
	}
	const held = "\x00"
	message = strings.ReplaceAll(message, held, "")
	spans := append(pathSpans(runner, true, held), s.custodySpans("", held)...)
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
