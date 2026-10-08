package desk

// Runner's signing key, review round 1: one lock for all of Desk's key
// custody, held from the first inspection to the last effect of a decision
// (runner_key.go, "One lock for all of Desk's key custody"); what is reported
// before, during and after Runner's start; and the bounds a single change
// could move unnoticed.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// holdKeyCustodyLockEnv names the folder TestHelperProcessHoldsTheKeyCustodyLock
// locks, when this test binary is started as that helper.
const holdKeyCustodyLockEnv = "DESK_TEST_HOLD_KEY_CUSTODY_LOCK"

// The helper process: another program holding the key-custody lock on the
// folder named, as another Desk would, until its standard input closes. It is
// this test binary, started again, and is skipped in an ordinary run.
func TestHelperProcessHoldsTheKeyCustodyLock(t *testing.T) {
	folder := os.Getenv(holdKeyCustodyLockEnv)
	if folder == "" {
		t.Skip("the helper process of TestAKeyCustodyLockHeldByAnotherProcess")
	}
	file, err := os.Open(folder)
	if err == nil {
		err = lockPrivateData(file, true)
	}
	if err != nil {
		fmt.Println("refused:", err)
		os.Exit(3)
	}
	fmt.Println("held")
	_, _ = io.Copy(io.Discard, os.Stdin)
	os.Exit(0)
}

// holdKeyCustodyLock has another process take the key-custody lock on folder,
// and answers once it holds it, with the function that ends that process.
func holdKeyCustodyLock(t *testing.T, folder string) func() {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcessHoldsTheKeyCustodyLock$", "-test.count=1")
	cmd.Env = append(os.Environ(), holdKeyCustodyLockEnv+"="+folder)
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	released := false
	release := func() {
		if !released {
			released = true
			input.Close()
			_ = cmd.Wait()
		}
	}
	t.Cleanup(release)
	line, _ := bufio.NewReader(output).ReadString('\n')
	if line != "held\n" {
		release()
		t.Fatalf("the helper process did not hold the lock: %q", line)
	}
	return release
}

// generations is how many keys the stand-in runtime whose calls file is
// calls has generated.
func generations(t *testing.T, calls string) int {
	t.Helper()
	return strings.Count(readFile(t, calls), "audit key generate")
}

// **Another process holding the lock changes nothing, and says so.** With a
// second process holding the key-custody lock on `signing/`:
//   - a start that would make or name a key waits its bounded time
//     (shortened here), then starts Runner without one, reporting the folder
//     in use, and changes nothing;
//   - a start that finds an unfinished creation, a sweep, does not wait, and
//     changes nothing;
//   - the desks' sweep at Desk's start does not wait, and changes nothing.
//
// Once the lock is released, each does what it would have done.
func TestAKeyCustodyLockHeldByAnotherProcess(t *testing.T) {
	const kept, unfinished, fresh = "c0000000000000000000000000000010", "c0000000000000000000000000000011", "c0000000000000000000000000000012"
	const desk = "c0000000000000000000000000000013"
	bin := filepath.Join(t.TempDir(), "jpack")
	calls := writeStandInRuntime(t, bin, reading(allConfigVersions), lockingAs(wantGatedConfig))
	s, _, _ := gatesServer(t, bin)
	if s.newRunnerKey(kept).decide() == "" {
		t.Fatal("the key was not made")
	}
	leaveUnfinished(t, s, unfinished)
	signing, runner := signingFolderOf(s), runnerFolderOf(s)
	for _, file := range []string{desk + ".seed", desk + ".keys.jsonl", desk + ".creating"} {
		if err := os.WriteFile(filepath.Join(signing, file), plantedAs(file), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	saved := runnerKeyCreationWait
	t.Cleanup(func() { runnerKeyCreationWait = saved })
	release := holdKeyCustodyLock(t, signing)
	before, deskBefore, generated := keptIn(t, runner), keptIn(t, signing), generations(t, calls)

	runnerKeyCreationWait = 300 * time.Millisecond
	for _, name := range []string{kept, fresh} {
		k := s.newRunnerKey(name)
		began := time.Now()
		path, decided := k.prepare()
		if waited := time.Since(began); waited < runnerKeyCreationWait {
			t.Errorf("%s: gave up after %v, before its bounded wait of %v", name, waited, runnerKeyCreationWait)
		}
		if path != "" || decided != (RunnerKeyStatus{State: "unsigned", Reason: "in-use"}) {
			t.Errorf("%s: named %q, decided %+v", name, path, decided)
		}
	}
	runnerKeyCreationWait = 5 * time.Second
	began := time.Now()
	path, decided := s.newRunnerKey(unfinished).prepare()
	if waited := time.Since(began); waited >= 2*time.Second {
		t.Errorf("a sweep waited %v for the lock", waited)
	}
	if path != "" || decided != (RunnerKeyStatus{State: "unsigned", Reason: "in-use"}) {
		t.Errorf("a sweep under another's lock named %q, decided %+v", path, decided)
	}
	began = time.Now()
	s.sweepUnfinishedKeys()
	if waited := time.Since(began); waited >= 2*time.Second {
		t.Errorf("the desks' sweep waited %v for the lock", waited)
	}
	assertKept(t, runner, before)
	assertKept(t, signing, deskBefore)
	if generations(t, calls) != generated {
		t.Error("a key was generated under another's lock")
	}

	release()
	for _, name := range []string{kept, fresh, unfinished} {
		if path := s.newRunnerKey(name).decide(); path != filepath.Join(runner, name+".seed") {
			t.Errorf("%s: after the lock was released, the start named %q", name, path)
		}
	}
	s.sweepUnfinishedKeys()
	if names := namesIn(t, signing); !slices.Equal(names, []string{"runner"}) {
		t.Errorf("after the lock was released, the desks' sweep left %q", names)
	}
}

// **The reviewer's two starts, now under the lock** (review round 1, HIGH 1).
// A is making the key: its marker is written and the runtime has made the
// seed. B starts then, as a second Desk process would:
//   - B sees A's marker, so it would remove an unfinished creation; it does
//     not wait for A's lock, changes nothing, and starts Runner without a key;
//   - another start, C, comes while A, its marker removed, still holds the
//     lock; C waits, and once A is done finds A's key and names it.
//
// A finishes, names its key and boots Runner with it; that key is intact at
// the end, and B's next start names it too.
func TestTwoStartsNeverRemoveANamedKey(t *testing.T) {
	const name = "c0000000000000000000000000000014"
	bin := filepath.Join(t.TempDir(), "jpack")
	calls := writeStandInRuntime(t, bin, reading(allConfigVersions), lockingAs(wantGatedConfig))
	s, _, _ := gatesServer(t, bin)
	if s.newRunnerKey(strings.Repeat("ab", 32)).decide() == "" {
		t.Fatal("the folder was not made")
	}
	runner, received := recordingRunner(t, false)
	a := &jobsCompanion{bin: runner, runtime: s.cfg.JpackBin, dir: t.TempDir(), workspace: name, key: s.newRunnerKey(name), stop: make(chan struct{})}
	t.Cleanup(a.close)
	b, c := s.newRunnerKey(name), s.newRunnerKey(name)

	var bPath string
	var bDecided RunnerKeyStatus
	cDone := make(chan string, 1)
	cWaiting := make(chan struct{})
	// Flags, not sync.Once: where a start does not wait for the lock, B's
	// start runs into these hooks from inside A's, and a Once would wait on
	// itself.
	var bStarted, cStarted, cLaunched atomic.Bool
	var peekedOnce sync.Once
	testHookRunnerKeyIO = func(step string) error {
		// C's look for a marker, the step before it asks for the lock.
		if cLaunched.Load() && step == name+".creating" {
			peekedOnce.Do(func() { close(cWaiting) })
		}
		return nil
	}
	t.Cleanup(func() { testHookRunnerKeyIO = nil })
	testHookKeyBetween = func(at string) {
		switch at {
		case "after generate":
			// A holds the lock, and its marker stands: B starts now.
			if bStarted.CompareAndSwap(false, true) {
				bPath, bDecided = b.prepare()
			}
		case "read list":
			// A, its marker removed, reads its list under the lock: C starts
			// now, and A goes on only once C is asking for the lock.
			if cStarted.CompareAndSwap(false, true) {
				cLaunched.Store(true)
				go func() {
					path, _ := c.prepare()
					cDone <- path
				}()
				select {
				case <-cWaiting:
					time.Sleep(100 * time.Millisecond)
				case <-time.After(20 * time.Second):
					t.Error("C did not start")
				}
			}
		}
	}
	t.Cleanup(func() { testHookKeyBetween = nil })
	if _, _, err := a.endpoint(); err != nil {
		t.Fatal(err)
	}
	// **C is waited for only where it was started, and for a bounded time**
	// (issue #252). C starts from inside A's start, once A reads its list
	// under the lock; an A that made no key never gets there, and a wait on C
	// with no bound then held the whole suite past its own.
	if !cLaunched.Load() {
		t.Fatalf("A never read its list under the lock, so C never started: A reports %+v", *a.keyStatus())
	}
	var cPath string
	select {
	case cPath = <-cDone:
	case <-time.After(30 * time.Second):
		t.Fatal("C, waiting on A's lock, never decided")
	}
	testHookKeyBetween = nil
	testHookRunnerKeyIO = nil

	seed := filepath.Join(runnerFolderOf(s), name+".seed")
	boots := bootLines(t, received)
	if len(boots) != 1 || bootKey(t, boots[0]) != seed {
		t.Fatalf("A booted Runner with %q, want its own key", readFile(t, received))
	}
	if got := *a.keyStatus(); got.State != "signed" {
		t.Errorf("A reports %+v", got)
	}
	if bPath != "" || bDecided != (RunnerKeyStatus{State: "unsigned", Reason: "in-use"}) {
		t.Errorf("B, during A's creation, named %q and decided %+v", bPath, bDecided)
	}
	if cPath != seed {
		t.Errorf("C, waiting on A, named %q, want A's key", cPath)
	}
	if got := readFile(t, seed); got != standInSeed+"\n" {
		t.Errorf("A's key was changed: %q", got)
	}
	if names := namesIn(t, runnerFolderOf(s)); !slices.Contains(names, name+".seed") || slices.Contains(names, name+".creating") {
		t.Errorf("the folder holds %q", names)
	}
	if b.decide() != seed {
		t.Errorf("B's next start did not name A's key: %+v", *b.report())
	}
	if n := generations(t, calls); n != 2 {
		t.Errorf("the runtime generated %d keys, want 2 (the folder's first, and A's)", n)
	}
}

// **The desks' sweep and a Runner key's decisions exclude each other.**
// While the desks' sweep holds the lock (it reads a published desk's
// manifest), a Runner key's start that would sweep changes nothing; while a
// Runner key's start holds it (between its inspection and its removal), the
// desks' sweep changes nothing. Each does its work once the other is done.
func TestTheDesksSweepAndARunnerKeyExcludeEachOther(t *testing.T) {
	const runnerName = "c0000000000000000000000000000015"
	const orphan = "c0000000000000000000000000000016"
	bin := filepath.Join(t.TempDir(), "jpack")
	writeStandInRuntime(t, bin, reading(allConfigVersions), lockingAs(wantGatedConfig))
	s, ts, _ := gatesServer(t, bin)
	published := createTestDesk(t, ts, "Published").ID
	signing, runner := signingFolderOf(s), runnerFolderOf(s)
	leaveUnfinished(t, s, runnerName)
	if err := os.WriteFile(filepath.Join(signing, published+".creating"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	runnerBefore := keptIn(t, runner)
	var during RunnerKeyStatus
	testHookPrivateRead = func(name string) error {
		if during.State == "" {
			_, during = s.newRunnerKey(runnerName).prepare()
			assertKept(t, runner, runnerBefore)
		}
		return nil
	}
	t.Cleanup(func() { testHookPrivateRead = nil })
	s.sweepUnfinishedKeys()
	testHookPrivateRead = nil
	if during != (RunnerKeyStatus{State: "unsigned", Reason: "in-use"}) {
		t.Errorf("a Runner key's sweep during the desks' sweep decided %+v", during)
	}
	if names := namesIn(t, signing); slices.Contains(names, published+".creating") {
		t.Errorf("the desks' sweep did not remove a published desk's marker: %q", names)
	}

	for _, file := range []string{orphan + ".seed", orphan + ".keys.jsonl", orphan + ".creating"} {
		if err := os.WriteFile(filepath.Join(signing, file), plantedAs(file), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	deskBefore := keptIn(t, signing)
	testHookKeyBetween = func(at string) {
		if at == "before removal" {
			s.sweepUnfinishedKeys()
			assertKept(t, signing, deskBefore)
		}
	}
	t.Cleanup(func() { testHookKeyBetween = nil })
	if path := s.newRunnerKey(runnerName).decide(); path != filepath.Join(runner, runnerName+".seed") {
		t.Fatalf("the Runner key's start named %q", path)
	}
	testHookKeyBetween = nil
	s.sweepUnfinishedKeys()
	for _, file := range []string{orphan + ".seed", orphan + ".keys.jsonl", orphan + ".creating"} {
		if _, err := os.Lstat(filepath.Join(signing, file)); !os.IsNotExist(err) {
			t.Errorf("the desks' sweep, once free, left %s: %v", file, err)
		}
	}
}

// waitingRunner is a stand-in Runner that records its boot line, then waits
// until a line is written to the named pipe go before it answers, so that a
// test can see what is reported while Runner is starting.
func waitingRunner(t *testing.T) (bin, received, gate string) {
	t.Helper()
	dir := t.TempDir()
	received, gate, bin = filepath.Join(dir, "boot.jsonl"), filepath.Join(dir, "go"), filepath.Join(dir, "runner")
	if err := syscall.Mkfifo(gate, 0o600); err != nil {
		t.Skipf("no named pipe here: %v", err)
	}
	script := "#!/bin/sh\nIFS= read -r line\nprintf '%s\\n' \"$line\" >> " + received + "\nIFS= read -r _ < " + gate + "\n" +
		"printf '{\"protocol\":\"jobs/1\",\"url\":\"http://127.0.0.1:9\"}\\n'\nwhile IFS= read -r _; do :; done\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return bin, received, gate
}

// **What is reported follows Runner, never ahead of it** (review round 1,
// MEDIUM 1):
//   - before any start, and while Runner is starting with the key on its
//     boot line, the key is starting, never signed;
//   - once Runner has answered with the key, signed;
//   - a start that fails, here with no Runner at the path given, is not
//     running, with why and no path, and never signed;
//   - a Runner that refuses the key at boot, and refuses again without it,
//     was started exactly twice, and is not running.
func TestWhatRunnerSignsIsReportedOnlyOnceRunnerAnswers(t *testing.T) {
	const name = "c0000000000000000000000000000017"
	bin := filepath.Join(t.TempDir(), "jpack")
	writeStandInRuntime(t, bin, reading(allConfigVersions), lockingAs(wantGatedConfig))
	s, _, _ := gatesServer(t, bin)

	runner, received, gate := waitingRunner(t)
	j := &jobsCompanion{bin: runner, runtime: s.cfg.JpackBin, dir: t.TempDir(), workspace: name, key: s.newRunnerKey(name), stop: make(chan struct{})}
	t.Cleanup(j.close)
	if got := *j.keyStatus(); got != (RunnerKeyStatus{State: "starting"}) {
		t.Errorf("before any start: %+v", got)
	}
	done := make(chan error, 1)
	go func() { _, _, err := j.endpoint(); done <- err }()
	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if data, err := os.ReadFile(received); err == nil && strings.HasSuffix(string(data), "\n") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Runner was not started")
		}
	}
	if got := *j.keyStatus(); got != (RunnerKeyStatus{State: "starting"}) {
		t.Errorf("while Runner starts with the key: %+v", got)
	}
	if bootKey(t, bootLines(t, received)[0]) == "" {
		t.Fatal("Runner was started without the key")
	}
	if err := os.WriteFile(gate, []byte("go\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := *j.keyStatus(); got.State != "signed" || got.PublicKey != standInPublicKey {
		t.Errorf("once Runner answered with the key: %+v", got)
	}

	missing := &jobsCompanion{bin: filepath.Join(t.TempDir(), "no-runner-here"), runtime: s.cfg.JpackBin, dir: t.TempDir(), workspace: name, key: s.newRunnerKey(name), stop: make(chan struct{})}
	t.Cleanup(missing.close)
	if _, _, err := missing.endpoint(); err == nil {
		t.Fatal("a Runner that is not there started")
	}
	got := *missing.keyStatus()
	if got.State != "not-running" || got.Detail == "" || strings.Contains(got.Detail, "no-runner-here") || strings.Contains(got.Detail, os.TempDir()) {
		t.Errorf("a start that failed reports %+v", got)
	}

	refusing, twice := recordingRunner(t, false)
	script := "#!/bin/sh\nIFS= read -r line\nprintf '%s\\n' \"$line\" >> " + twice + "\nprintf 'runner: the signing key is refused: a refusal even without a key\\n' >&2\nexit 1\n"
	if err := os.WriteFile(refusing, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	both := &jobsCompanion{bin: refusing, runtime: s.cfg.JpackBin, dir: t.TempDir(), workspace: name, key: s.newRunnerKey(name), stop: make(chan struct{})}
	t.Cleanup(both.close)
	if _, _, err := both.endpoint(); err == nil {
		t.Fatal("a Runner that refused both boots started")
	}
	if boots := bootLines(t, twice); len(boots) != 2 || bootKey(t, boots[0]) == "" || bootKey(t, boots[1]) != "" {
		t.Errorf("Runner was booted %d times, want the key and then none", len(boots))
	}
	if got := *both.keyStatus(); got.State != "not-running" {
		t.Errorf("a Runner that refused both boots reports %+v", got)
	}
}

// **`GET /api/runner-key`** is guarded as every route is, answers null where
// the desk has no Runner, and the Runner key's state where it has one, for
// each desk.
func TestTheRunnerKeyRouteIsGuardedAndAnswersEachDesk(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "jpack")
	writeStandInRuntime(t, bin, reading(allConfigVersions), lockingAs(wantGatedConfig))
	without, plain, _ := gatesServer(t, bin)
	_ = without
	if status, _ := deskCall(t, plain, "GET", "/api/runner-key", "", "", false); status != 401 {
		t.Errorf("without a session: %d", status)
	}
	if status, data := deskCall(t, plain, "GET", "/api/runner-key", "", "", true); status != 200 || string(data) != `{"runnerKey":null}`+"\n" {
		t.Errorf("without a Runner: %d %s", status, data)
	}
	runner, _ := recordingRunner(t, false)
	s, ts, _ := runnerDesk(t, bin, runner, t.TempDir())
	started(t, s.jobs)
	named := createTestDesk(t, ts, "Named").ID
	s.desksMu.Lock()
	child := s.desks[named]
	s.desksMu.Unlock()
	started(t, child.jobs)
	for _, id := range []string{"", named} {
		status, data := deskCall(t, ts, "GET", "/api/runner-key", id, "", true)
		var answer struct{ RunnerKey *RunnerKeyStatus }
		if status != 200 || json.Unmarshal(data, &answer) != nil || answer.RunnerKey == nil || answer.RunnerKey.State != "signed" || answer.RunnerKey.PublicKey != standInPublicKey {
			t.Errorf("desk %q: %d %s", id, status, data)
		}
	}
}

// **A name that is not 32 or 64 lowercase hexadecimal characters is refused
// before any path is built**: no folder is made, and no key.
func TestARunnerKeyNameThatIsNotHexIsRefusedBeforeAnyPath(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "jpack")
	calls := writeStandInRuntime(t, bin, reading(allConfigVersions), lockingAs(wantGatedConfig))
	s, _, _ := gatesServer(t, bin)
	ok := strings.Repeat("a", 32)
	for _, name := range []string{"", "runner", ".", "..", "../" + ok, ok + "/x", ok + ".seed", strings.Repeat("a", 31), strings.Repeat("a", 33), strings.Repeat("a", 63), strings.Repeat("a", 65), strings.Repeat("A", 32), strings.Repeat("g", 32)} {
		k := s.newRunnerKey(name)
		path, decided := k.prepare()
		if path != "" || decided.Reason != "custody" {
			t.Errorf("%q: named %q, decided %+v", name, path, decided)
		}
	}
	if _, err := os.Lstat(signingFolderOf(s)); !os.IsNotExist(err) {
		t.Errorf("a name that is not one made a folder: %v", err)
	}
	if _, err := os.Stat(calls); err == nil && generations(t, calls) != 0 {
		t.Error("a name that is not one had a key made")
	}
}

// **Runner's standard error is kept to its bound, and no further**: of 9000
// bytes, the first 4096; a refusal whose line starts within them is read, and
// one past them is not.
func TestRunnersStandardErrorIsKeptToItsBound(t *testing.T) {
	kept := &cappedBuffer{limit: runnerSaidLimit}
	_, _ = kept.Write([]byte(strings.Repeat("x", 9000)))
	if n := len(kept.Bytes()); n != 4096 {
		t.Errorf("kept %d bytes of 9000, want 4096", n)
	}
	for filler, refused := range map[int]bool{4096 - len(runnerKeyRefusedPrefix) - 1: true, 4096 - len(runnerKeyRefusedPrefix): true, 4096 - len(runnerKeyRefusedPrefix) + 1: false} {
		said := &cappedBuffer{limit: runnerSaidLimit}
		_, _ = said.Write([]byte(strings.Repeat("x", filler-1) + "\n" + runnerKeyRefusedPrefix + "it must be a regular file\n"))
		if _, got := runnerKeyRefusal(said.Bytes()); got != refused {
			t.Errorf("a refusal whose prefix ends at byte %d: read %v, want %v", filler+len(runnerKeyRefusedPrefix), got, refused)
		}
	}
}

// **The desks' sweep at Desk's start takes the lock**, and does not wait for
// it: a Desk started while another process holds it leaves an unfinished
// desk key as it is; the next start, once the lock is free, removes it.
func TestDesksStartSweepsUnderTheLock(t *testing.T) {
	const orphan = "c0000000000000000000000000000018"
	bin := filepath.Join(t.TempDir(), "jpack")
	writeStandInRuntime(t, bin, reading(allConfigVersions), lockingAs(wantGatedConfig))
	first, _, _ := gatesServer(t, bin)
	signing, err := first.assistant.openSigning(true)
	if err != nil {
		t.Fatal(err)
	}
	signing.Close()
	folder := signingFolderOf(first)
	for _, file := range []string{orphan + ".seed", orphan + ".keys.jsonl", orphan + ".creating"} {
		if err := os.WriteFile(filepath.Join(folder, file), plantedAs(file), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	before := keptIn(t, folder)
	release := holdKeyCustodyLock(t, folder)
	began := time.Now()
	held, heldServer := startDesk(t, Config{ProjectDir: t.TempDir(), JpackBin: bin, DeskConfigDir: first.configDir, Token: testToken})
	if waited := time.Since(began); waited >= 5*time.Second {
		t.Errorf("Desk's start waited %v for the lock", waited)
	}
	heldServer.Close()
	held.Close()
	assertKept(t, folder, before)
	release()
	free, freeServer := startDesk(t, Config{ProjectDir: t.TempDir(), JpackBin: bin, DeskConfigDir: first.configDir, Token: testToken})
	freeServer.Close()
	free.Close()
	if names := namesIn(t, folder); len(names) != 0 {
		t.Errorf("Desk's start, the lock free, left %q", names)
	}
}

// **The folder opened is the folder checked.** `runner/` replaced, between
// custody's making or narrowing of it and the check of what is opened, by one
// its group can write: it is refused, and no key is made or named in it.
func TestARunnerKeysFolderSwappedAfterItsNarrowingIsRefused(t *testing.T) {
	const name = "c0000000000000000000000000000019"
	bin := filepath.Join(t.TempDir(), "jpack")
	calls := writeStandInRuntime(t, bin, reading(allConfigVersions), lockingAs(wantGatedConfig))
	s, _, _ := gatesServer(t, bin)
	runner := runnerFolderOf(s)
	testHookRunnerKeyIO = func(step string) error {
		if step != "runner" {
			return nil
		}
		if err := os.Rename(runner, runner+".aside"); err != nil {
			t.Error(err)
		}
		if err := os.Mkdir(runner, 0o700); err != nil {
			t.Error(err)
		}
		if err := os.Chmod(runner, 0o770); err != nil {
			t.Error(err)
		}
		return nil
	}
	t.Cleanup(func() { testHookRunnerKeyIO = nil })
	k := s.newRunnerKey(name)
	if path := k.decide(); path != "" {
		t.Fatalf("named %s in a folder its group can write", path)
	}
	testHookRunnerKeyIO = nil
	if got := *k.report(); got.Reason != "custody" || !strings.Contains(got.Detail, "writable by group or others") {
		t.Errorf("reported %+v", got)
	}
	if names := namesIn(t, runner); len(names) != 0 {
		t.Errorf("the folder swapped in holds %q", names)
	}
	if _, err := os.Stat(calls); err == nil && generations(t, calls) != 0 {
		t.Error("a key was made")
	}
}

// **Desk's start sweeps the desks' keys before its Runner takes the lock.**
// The startup desk's Runner is started from Desk's start, and its key's
// decision holds the lock; the desks' sweep, which never waits, must not find
// it held. Here the sweep, about to take the lock, waits up to two seconds
// for a Runner key's decision to take it first, and a decision that has it
// holds it until the sweep has tried: where Runner's first start comes before
// the sweep, the sweep finds the lock held and leaves the unfinished desk key.
// With a Runner configured, Desk's start removes that key, and the Runner's
// key is decided after it.
func TestDesksStartSweepsBeforeItsRunnerTakesTheLock(t *testing.T) {
	const orphan = "c000000000000000000000000000001a"
	bin := filepath.Join(t.TempDir(), "jpack")
	writeStandInRuntime(t, bin, reading(allConfigVersions), lockingAs(wantGatedConfig))
	first, _, _ := gatesServer(t, bin)
	signing, err := first.assistant.openSigning(true)
	if err != nil {
		t.Fatal(err)
	}
	signing.Close()
	folder := signingFolderOf(first)
	for _, file := range []string{orphan + ".seed", orphan + ".keys.jsonl", orphan + ".creating"} {
		if err := os.WriteFile(filepath.Join(folder, file), plantedAs(file), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runnerHasLock, sweepTried := make(chan struct{}), make(chan struct{})
	var hasOnce, triedOnce sync.Once
	var runnerFirst atomic.Bool
	testHookKeyBetween = func(at string) {
		switch at {
		case "before generate":
			// A Runner key's decision, under the lock.
			hasOnce.Do(func() { close(runnerHasLock) })
			select {
			case <-sweepTried:
			case <-time.After(5 * time.Second):
			}
		case "before desks sweep":
			select {
			case <-runnerHasLock:
				runnerFirst.Store(true)
			case <-time.After(2 * time.Second):
			}
		case "desks sweep tried":
			triedOnce.Do(func() { close(sweepTried) })
		}
	}
	t.Cleanup(func() { testHookKeyBetween = nil })
	runner, _ := recordingRunner(t, false)
	s, ts := startDesk(t, Config{ProjectDir: t.TempDir(), JpackBin: bin, RunnerBin: runner, DeskConfigDir: first.configDir, Token: testToken})
	t.Cleanup(func() { ts.Close(); s.Close() })
	started(t, s.jobs)
	testHookKeyBetween = nil
	if runnerFirst.Load() {
		t.Error("the Runner's key was decided before Desk's start swept the desks' keys")
	}
	for _, file := range []string{orphan + ".seed", orphan + ".keys.jsonl", orphan + ".creating"} {
		if _, err := os.Lstat(filepath.Join(folder, file)); !os.IsNotExist(err) {
			t.Errorf("Desk's start left %s: %v", file, err)
		}
	}
	if got := *s.jobs.keyStatus(); got.State != "signed" {
		t.Errorf("the startup desk's Runner reports %+v", got)
	}
}
