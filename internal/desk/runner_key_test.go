package desk

// Runner's signing key (ADR-0010, section 5; runner_key.go): made where
// nothing is kept, named on the boot line only where every check holds, and
// otherwise left as it is while Runner starts without one, with the reason
// reported and no path in it. A stand-in runtime and a stand-in Runner, by
// absolute path, answer, so these run where neither is installed; the test
// that drives the pinned Runner and runtime is in jobs_test.go.

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
)

// syncBuffer is a log a Runner's start writes to from the companion's own
// goroutine while the test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// runnerFolderOf is the folder Desk keeps Runner keys in, on s.
func runnerFolderOf(s *Server) string { return filepath.Join(signingFolderOf(s), "runner") }

// decide is a start's decision on the key (`prepare`), recorded as if Runner
// had then started and answered with it (`started`), so that report says it.
// It answers the path the boot line would name.
func (k *runnerKey) decide() string {
	path, decided := k.prepare()
	k.started(decided)
	return path
}

// recordingRunner is a stand-in Runner that appends each boot line it is
// given to received, and the JPACK_SIGNING_KEY of its own environment to
// received.env. Where refuse is true it refuses a boot line that names a
// signing key, as Runner v0.5.0 does, and exits; otherwise it answers the
// handshake. Shell builtins only: Desk starts the Runner with an empty PATH.
func recordingRunner(t *testing.T, refuse bool) (bin, received string) {
	t.Helper()
	dir := t.TempDir()
	received = filepath.Join(dir, "boot.jsonl")
	bin = filepath.Join(dir, "runner")
	script := "#!/bin/sh\nIFS= read -r line\nprintf '%s\\n' \"$line\" >> " + received + "\n" +
		"printf '%s\\n' \"${JPACK_SIGNING_KEY-unset}\" >> " + received + ".env\n"
	if refuse {
		script += "case \"$line\" in *'\"signingKey\"'*) printf 'runner: the signing key is refused: it must have one name, with no hard link elsewhere\\n' >&2; exit 1;; esac\n"
	}
	script += "printf '{\"protocol\":\"jobs/1\",\"url\":\"http://127.0.0.1:9\"}\\n'\nwhile IFS= read -r _; do :; done\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return bin, received
}

// bootLines is every boot line the recording Runner was given, in order.
func bootLines(t *testing.T, received string) []map[string]json.RawMessage {
	t.Helper()
	var lines []map[string]json.RawMessage
	for _, line := range strings.Split(strings.TrimSuffix(readFile(t, received), "\n"), "\n") {
		var boot map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &boot); err != nil {
			t.Fatalf("a boot line was not one JSON object: %q", line)
		}
		lines = append(lines, boot)
	}
	return lines
}

// bootKey is the boot line's signingKey, or "" where it names none.
func bootKey(t *testing.T, boot map[string]json.RawMessage) string {
	t.Helper()
	raw, named := boot["signingKey"]
	if !named {
		return ""
	}
	var path string
	if err := json.Unmarshal(raw, &path); err != nil || path == "" {
		t.Fatalf("the boot line's signingKey is %s", raw)
	}
	return path
}

// runnerKeyReported is what desk-config says of the Runner key of desk id
// ("" for the startup desk), and the whole answer.
func runnerKeyReported(t *testing.T, ts *httptest.Server, id string) (RunnerKeyStatus, string) {
	t.Helper()
	status, data := deskCall(t, ts, "GET", "/api/desk-config", id, "", true)
	var answer struct {
		Jobs struct {
			RunnerKey *RunnerKeyStatus `json:"runnerKey"`
		} `json:"jobs"`
	}
	if status != 200 || json.Unmarshal(data, &answer) != nil || answer.Jobs.RunnerKey == nil {
		t.Fatalf("desk-config: %d %s", status, data)
	}
	return *answer.Jobs.RunnerKey, string(data)
}

// runnerDesk is a chassis with the stand-in runtime at bin and the Runner at
// runner, over a configuration folder of its own, logging to the buffer.
func runnerDesk(t *testing.T, bin, runner, project string) (*Server, *httptest.Server, *syncBuffer) {
	t.Helper()
	config := t.TempDir()
	if err := os.Chmod(config, 0o700); err != nil {
		t.Fatal(err)
	}
	logged := &syncBuffer{}
	s, ts := startDesk(t, Config{RunnerBin: runner, JpackBin: bin, ProjectDir: project, DeskConfigDir: config, Token: testToken, Logger: log.New(logged, "", 0)})
	t.Cleanup(func() { ts.Close(); s.Close() })
	return s, ts, logged
}

// started waits for the companion's Runner, as a request does.
func started(t *testing.T, companion *jobsCompanion) {
	t.Helper()
	if companion == nil {
		t.Fatal("the Jobs companion was not configured")
	}
	if _, _, err := companion.endpoint(); err != nil {
		t.Fatal(err)
	}
}

// **A Runner key of its own, on the boot line, for the startup desk and for a
// desk Desk made** (ADR-0010, section 5; issue #215's first two criteria).
// With JPACK_SIGNING_KEY and JPACK_CONFIG set where Desk was started, and a
// project whose jpack.json names a key of its own:
//   - the startup desk's Runner is named the key Desk keeps under the name of
//     its state directory, and a made desk's under the desk's id;
//   - neither Runner's environment, nor the runtime commands that made and
//     read the keys, carry the inherited key or configuration;
//   - the runtime made each key, and Desk kept its public half; the folder is
//     0700, the seed and the list 0600, and nothing else is left there;
//   - desk-config reports each public key, and no path;
//   - no jpack.json changed.
func TestEachDesksRunnerIsNamedAKeyOfItsOwn(t *testing.T) {
	inherited := filepath.Join(t.TempDir(), "owner.seed")
	t.Setenv("JPACK_SIGNING_KEY", inherited)
	t.Setenv("JPACK_CONFIG", filepath.Join(t.TempDir(), "jpack.json"))
	project := t.TempDir()
	projectKey := filepath.Join(t.TempDir(), "project.seed")
	projectConfig := `{"configVersion":"6","audit":{"dir":"audit","signingKey":"` + projectKey + `"},"packs":{}}` + "\n"
	if err := os.WriteFile(filepath.Join(project, "jpack.json"), []byte(projectConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "jpack")
	calls := writeStandInRuntime(t, bin, reading(allConfigVersions), lockingAs(wantGatedConfig))
	runner, received := recordingRunner(t, false)
	s, ts, logged := runnerDesk(t, bin, runner, project)
	started(t, s.jobs)

	folder := runnerFolderOf(s)
	state := filepath.Base(s.jobs.dir)
	if s.jobs.dir != filepath.Join(s.configDir, "jobs", state) || len(state) != 64 {
		t.Fatalf("the startup desk's state directory is %s", s.jobs.dir)
	}
	boots := bootLines(t, received)
	if len(boots) != 1 || bootKey(t, boots[0]) != filepath.Join(folder, state+".seed") {
		t.Fatalf("the startup desk's Runner was booted with %q, want its own key under the name of its state directory", readFile(t, received))
	}
	reported, _ := runnerKeyReported(t, ts, "")
	if reported != (RunnerKeyStatus{State: "signed", PublicKey: standInPublicKey, KeyID: standInKeyID}) {
		t.Errorf("desk-config reports %+v", reported)
	}

	named := createTestDesk(t, ts, "Named")
	s.desksMu.Lock()
	child := s.desks[named.ID]
	s.desksMu.Unlock()
	if child == nil {
		t.Fatal("the named desk is not open")
	}
	config := readFile(t, filepath.Join(named.Folder, "jpack.json"))
	started(t, child.jobs)
	boots = bootLines(t, received)
	if len(boots) != 2 || bootKey(t, boots[1]) != filepath.Join(folder, named.ID+".seed") {
		t.Fatalf("the named desk's Runner was booted with %q, want its own key under the desk's id", readFile(t, received))
	}
	if reported, _ := runnerKeyReported(t, ts, named.ID); reported.State != "signed" || reported.PublicKey != standInPublicKey {
		t.Errorf("desk-config reports %+v for the named desk", reported)
	}

	for _, line := range strings.Split(strings.TrimSpace(readFile(t, received+".env")), "\n") {
		if line != "unset" {
			t.Errorf("a Runner inherited JPACK_SIGNING_KEY=%s", line)
		}
	}
	for _, boot := range boots {
		if key := bootKey(t, boot); key == inherited || key == projectKey || !strings.HasPrefix(key, folder+string(filepath.Separator)) {
			t.Errorf("a Runner was named %s", key)
		}
	}
	var keyCalls int
	for _, line := range strings.Split(readFile(t, calls+".env"), "\n") {
		if strings.HasPrefix(line, "audit key ") && strings.Contains(line, folder) {
			keyCalls++
			if !strings.Contains(line, "[JPACK_SIGNING_KEY=unset]") {
				t.Errorf("a Runner key's command inherited the signing key: %s", line)
			}
		}
	}
	for _, line := range strings.Split(readFile(t, calls), "\n") {
		if strings.HasPrefix(line, "audit key ") && !strings.HasSuffix(line, "[JPACK_CONFIG=unset]") {
			t.Errorf("a Runner key's command inherited JPACK_CONFIG: %s", line)
		}
	}
	if keyCalls != 4 {
		t.Errorf("the runtime made and read the Runner keys in %d commands, want 4 (generate and public, twice)", keyCalls)
	}

	for _, name := range []string{state, named.ID} {
		if got := readFile(t, filepath.Join(folder, name+".keys.jsonl")); got != wantKeyLine(standInPublicKey, standInKeyID, 0) {
			t.Errorf("%s's list of public keys is %q", name, got)
		}
		for path, perm := range map[string]os.FileMode{folder: 0o700, filepath.Join(folder, name+".seed"): 0o600, filepath.Join(folder, name+".keys.jsonl"): 0o600} {
			if got := permOf(t, path); got != perm {
				t.Errorf("%s is %v, want %v", filepath.Base(path), got, perm)
			}
		}
	}
	want := []string{named.ID + ".keys.jsonl", named.ID + ".seed", state + ".keys.jsonl", state + ".seed"}
	slices.Sort(want)
	if names := namesIn(t, folder); !slices.Equal(names, want) {
		t.Errorf("the Runner keys' folder holds %q", names)
	}
	if got := readFile(t, filepath.Join(project, "jpack.json")); got != projectConfig {
		t.Errorf("the startup project's jpack.json changed: %s", got)
	}
	if got := readFile(t, filepath.Join(named.Folder, "jpack.json")); got != config {
		t.Errorf("the named desk's jpack.json changed: %s", got)
	}
	for _, line := range strings.Split(logged.String(), "\n") {
		if strings.Contains(line, "Runner") && strings.Contains(line, s.configDir) {
			t.Errorf("Desk's log names the configuration folder: %s", line)
		}
	}
}

// **The key is made once and then found.** A second start of the same
// Runner, in this process or the next, names the same key, and the runtime is
// asked only to read it: nothing is made again.
func TestARunnerKeyIsMadeOnceAndFoundAfter(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "jpack")
	calls := writeStandInRuntime(t, bin, reading(allConfigVersions), lockingAs(wantGatedConfig))
	s, _, _ := gatesServer(t, bin)
	name := strings.Repeat("ab", 32)
	first := s.newRunnerKey(name)
	path := first.decide()
	seed := filepath.Join(runnerFolderOf(s), name+".seed")
	info, err := os.Lstat(seed)
	if path != seed || err != nil {
		t.Fatalf("the first start named %q: %v", path, err)
	}
	for _, k := range []*runnerKey{first, s.newRunnerKey(name)} {
		if got := k.decide(); got != seed {
			t.Fatalf("a later start named %q", got)
		}
	}
	if again, err := os.Lstat(seed); err != nil || !os.SameFile(info, again) {
		t.Errorf("the key was made again: %v", err)
	}
	if n := strings.Count(readFile(t, calls), "audit key generate"); n != 1 {
		t.Errorf("the runtime generated %d keys, want 1", n)
	}
	if n := strings.Count(readFile(t, calls), "audit key public"); n != 3 {
		t.Errorf("the runtime read the key %d times, want 3", n)
	}
}

// keptFiles is each name's bytes and identity in folder, for showing that
// nothing kept was changed, removed or replaced.
type keptFile struct {
	data string
	info os.FileInfo
}

func keptIn(t *testing.T, folder string) map[string]keptFile {
	t.Helper()
	kept := map[string]keptFile{}
	for _, name := range namesIn(t, folder) {
		info, err := os.Lstat(filepath.Join(folder, name))
		if err != nil {
			t.Fatal(err)
		}
		var data []byte
		if info.Mode().IsRegular() {
			data, _ = os.ReadFile(filepath.Join(folder, name))
		}
		kept[name] = keptFile{string(data), info}
	}
	return kept
}

func assertKept(t *testing.T, folder string, before map[string]keptFile) {
	t.Helper()
	after := keptIn(t, folder)
	if len(after) != len(before) {
		t.Errorf("the Runner keys' folder held %d names and holds %d", len(before), len(after))
	}
	for name, was := range before {
		now, found := after[name]
		if !found || now.data != was.data || !os.SameFile(now.info, was.info) {
			t.Errorf("%s was changed, removed or replaced", name)
		}
	}
}

// **Every state a start can find, and what it does** (runner_key.go). Each
// case sets up the Runner keys' folder, starts once, and requires: the path
// named, or none; the reason reported; nothing kept removed, made again or
// changed; and no path in the words.
func TestEachStateARunnerKeyCanBeFoundIn(t *testing.T) {
	const name = "c0000000000000000000000000000002"
	other := strings.Repeat("ab", 32)
	type setup struct {
		s      *Server
		folder string
		calls  string
	}
	// made makes the key as a first start does, and returns it.
	made := func(t *testing.T, st setup) {
		t.Helper()
		if path := st.s.newRunnerKey(name).decide(); path == "" {
			t.Fatal("the key was not made")
		}
	}
	for _, c := range []struct {
		name    string
		arrange func(t *testing.T, st setup)
		reason  string
		detail  string
	}{
		{name: "a link at the seed's name", arrange: func(t *testing.T, st setup) {
			made(t, st)
			seed := filepath.Join(st.folder, name+".seed")
			if err := os.Rename(seed, seed+".real"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(name+".seed.real", seed); err != nil {
				t.Fatal(err)
			}
		}, reason: "not-used", detail: "the key is a symbolic link, not a signing key"},
		{name: "a seed others can read", arrange: func(t *testing.T, st setup) {
			made(t, st)
			if err := os.Chmod(filepath.Join(st.folder, name+".seed"), 0o640); err != nil {
				t.Fatal(err)
			}
		}, reason: "not-used", detail: "the key is readable or writable by someone other than its owner (mode 0640), so it is not treated as this desk's key"},
		{name: "a seed with a second name", arrange: func(t *testing.T, st setup) {
			made(t, st)
			if err := os.Link(filepath.Join(st.folder, name+".seed"), filepath.Join(st.folder, "copy")); err != nil {
				t.Fatal(err)
			}
		}, reason: "not-used", detail: "the key has more than one name, or this build cannot say how many"},
		{name: "a folder at the seed's name", arrange: func(t *testing.T, st setup) {
			if err := os.Mkdir(filepath.Join(st.folder, name+".seed"), 0o700); err != nil {
				t.Fatal(err)
			}
		}, reason: "not-used", detail: "the key is not a regular file, and was not read"},
		{name: "a lost seed", arrange: func(t *testing.T, st setup) {
			made(t, st)
			if err := os.Remove(filepath.Join(st.folder, name+".seed")); err != nil {
				t.Fatal(err)
			}
		}, reason: "lost"},
		{name: "a seed with no list", arrange: func(t *testing.T, st setup) {
			made(t, st)
			if err := os.Remove(filepath.Join(st.folder, name+".keys.jsonl")); err != nil {
				t.Fatal(err)
			}
		}, reason: "not-used", detail: "Desk keeps no list of its public keys"},
		{name: "a list naming another key", arrange: func(t *testing.T, st setup) {
			made(t, st)
			writeKeys(t, st.folder, name, wantKeyLine(secondPublicKey, secondKeyID, 0))
		}, reason: "not-used", detail: "its list of public keys does not name it"},
		{name: "a list of two keys", arrange: func(t *testing.T, st setup) {
			made(t, st)
			writeKeys(t, st.folder, name, wantKeyLine(standInPublicKey, standInKeyID, 0)+wantKeyLine(secondPublicKey, secondKeyID, 4))
		}, reason: "not-used", detail: "its list of public keys holds more than one key, and this version of Desk keeps one key for Runner and rotates none"},
		{name: "a list others can write", arrange: func(t *testing.T, st setup) {
			made(t, st)
			if err := os.Chmod(filepath.Join(st.folder, name+".keys.jsonl"), 0o620); err != nil {
				t.Fatal(err)
			}
		}, reason: "not-used", detail: "its list of public keys was not read: its group or other users can write it"},
		{name: "the runtime refuses the seed", arrange: func(t *testing.T, st setup) {
			made(t, st)
			readingKeyAs(t, st.calls, `  printf '%s\n' "{\"outputVersion\":\"2\",\"command\":\"audit key public\",\"status\":\"error\",\"diagnostics\":[{\"code\":\"JPS-AUDIT-KEY-REFUSED\",\"message\":\"The key at $4 is refused: the directory ${4%/*} on the signing key's path can be written by its group or by other users (mode 0770) and has no sticky bit, so another user could remove or replace the key; chmod go-w ${4%/*} fixes it.\"}]}"; exit 1`)
		}, reason: "runtime-refused", detail: "The key at … is refused: the directory … on the signing key's path can be written by its group or by other users (mode 0770) and has no sticky bit, so another user could remove or replace the key; chmod go-w … fixes it"},
		{name: "the runtime does not answer", arrange: func(t *testing.T, st setup) {
			made(t, st)
			readingKeyAs(t, st.calls, "  exit 70")
		}, reason: "not-read-now", detail: "the runtime's audit key public … --format json failed: exit status 70"},
	} {
		t.Run(c.name, func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "jpack")
			calls := writeStandInRuntime(t, bin, reading(allConfigVersions), lockingAs(wantGatedConfig))
			s, _, logged := gatesServer(t, bin)
			if path := s.newRunnerKey(other).decide(); path == "" {
				t.Fatal("the folder was not made")
			}
			st := setup{s, runnerFolderOf(s), calls}
			c.arrange(t, st)
			before := keptIn(t, st.folder)
			generated := strings.Count(readFile(t, calls), "audit key generate")
			k := s.newRunnerKey(name)
			if path := k.decide(); path != "" {
				t.Fatalf("the start named %s", path)
			}
			got := *k.report()
			want := RunnerKeyStatus{State: "unsigned", Reason: c.reason, Detail: c.detail}
			if got != want {
				t.Errorf("reported\n%+v\nwant\n%+v", got, want)
			}
			assertKept(t, st.folder, before)
			if n := strings.Count(readFile(t, calls), "audit key generate"); n != generated {
				t.Errorf("a key was generated")
			}
			if strings.Contains(got.Detail, s.configDir) || strings.Contains(logged.String(), s.configDir) {
				t.Errorf("a path was said: %q; %s", got.Detail, logged)
			}
		})
	}
}

// **Could not be inspected now is not "not there"** (#219's round 3, and
// review round 1 of this PR, for the Runner key). Under the lock, every name
// is inspected before anything is decided: a failure for any reason but
// absence, at the marker, the seed, the list, the folder or the lock, leaves
// every file byte for byte, removes nothing, makes nothing and names nothing,
// whether a key is kept, a creation was left unfinished (marker, seed and
// list), or nothing is kept; the next start that can inspect them names a
// key.
func TestARunnerKeyNotInspectedNowIsLeftAndNotMade(t *testing.T) {
	const name = "c0000000000000000000000000000003"
	for _, failing := range []string{name + ".creating", name + ".seed", name + ".keys.jsonl", "runner", "lock"} {
		for _, kept := range []string{"a key kept", "an unfinished creation", "nothing kept"} {
			t.Run(failing+", "+kept, func(t *testing.T) {
				bin := filepath.Join(t.TempDir(), "jpack")
				calls := writeStandInRuntime(t, bin, reading(allConfigVersions), lockingAs(wantGatedConfig))
				s, _, _ := gatesServer(t, bin)
				switch kept {
				case "a key kept":
					if s.newRunnerKey(name).decide() == "" {
						t.Fatal("the key was not made")
					}
				case "an unfinished creation":
					leaveUnfinished(t, s, name)
				default:
					if s.newRunnerKey(strings.Repeat("ab", 32)).decide() == "" {
						t.Fatal("the folder was not made")
					}
				}
				folder := runnerFolderOf(s)
				before := keptIn(t, folder)
				generated := strings.Count(readFile(t, calls), "audit key generate")
				testHookRunnerKeyIO = func(step string) error {
					if step == failing {
						return syscall.EIO
					}
					return nil
				}
				t.Cleanup(func() { testHookRunnerKeyIO = nil })
				k := s.newRunnerKey(name)
				if path := k.decide(); path != "" {
					t.Fatalf("named %s while %s could not be inspected", path, failing)
				}
				if got := *k.report(); got.State != "unsigned" || got.Reason != "not-read-now" {
					t.Errorf("reported %+v", got)
				}
				assertKept(t, folder, before)
				if n := strings.Count(readFile(t, calls), "audit key generate"); n != generated {
					t.Errorf("a key was generated while %s could not be inspected", failing)
				}
				testHookRunnerKeyIO = nil
				if path := k.decide(); path != filepath.Join(folder, name+".seed") {
					t.Errorf("the start after named %q: %+v", path, *k.report())
				}
			})
		}
	}
}

// leaveUnfinished leaves what a creation stopped before it removed its marker
// leaves: the marker, the seed the runtime wrote and the list of its public
// keys, in the Runner keys' folder.
func leaveUnfinished(t *testing.T, s *Server, name string) {
	t.Helper()
	signing, dir, err := s.assistant.openRunnerSigning()
	if err != nil {
		t.Fatal(err)
	}
	defer signing.Close()
	defer dir.Close()
	project, _ := s.runnerKeyRuntimeDir()
	if _, err := generateDeskKey(context.Background(), s.cfg.JpackBin, project, dir, name); err != nil {
		t.Fatal(err)
	}
	if names := namesIn(t, dir.path); !slices.Contains(names, name+".creating") || !slices.Contains(names, name+".seed") || !slices.Contains(names, name+".keys.jsonl") {
		t.Fatalf("the unfinished creation left %q", names)
	}
}

// **A creation that did not finish is removed under the lock, at whatever
// start finds it, and only by its marker** (runner_key.go,
// `removeUnfinished`). A seed and a list with no marker are never removed. A
// marker left with a seed and a list: the next start, in this process or
// another, removes all three and makes another key, and names it. Nothing
// but a marker found under the lock removes a key: the first-start rule of
// the round before is gone, since a creation holds the lock for as long as
// its marker stands.
func TestAnUnfinishedRunnerKeyIsRemovedUnderTheLock(t *testing.T) {
	const name = "c0000000000000000000000000000004"
	bin := filepath.Join(t.TempDir(), "jpack")
	calls := writeStandInRuntime(t, bin, reading(allConfigVersions), lockingAs(wantGatedConfig))
	s, _, _ := gatesServer(t, bin)
	k := s.newRunnerKey(name)
	if k.decide() == "" {
		t.Fatal("the key was not made")
	}
	folder := runnerFolderOf(s)
	seed := filepath.Join(folder, name+".seed")
	unmarked := keptIn(t, folder)
	if k.decide() != seed || s.newRunnerKey(name).decide() != seed {
		t.Fatal("an unmarked key was not named")
	}
	assertKept(t, folder, unmarked)

	// The seed's bytes are changed too, so that one made again is told from
	// it by its bytes, whatever inode it is given.
	for file, data := range map[string]string{name + ".creating": "", name + ".seed": "unfinished\n"} {
		if err := os.WriteFile(filepath.Join(folder, file), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	generated := strings.Count(readFile(t, calls), "audit key generate")
	if path := k.decide(); path != seed {
		t.Fatalf("a later start in this process reported %+v", *k.report())
	}
	if got := readFile(t, seed); got != standInSeed+"\n" {
		t.Errorf("the unfinished key was not removed and made again: %q", got)
	}
	if n := strings.Count(readFile(t, calls), "audit key generate"); n != generated+1 {
		t.Errorf("the runtime generated %d keys, want 1", n-generated)
	}
	if names := liveIn(t, folder); !slices.Equal(names, []string{name + ".keys.jsonl", name + ".seed"}) {
		t.Errorf("the folder holds %q", names)
	}
	// The unfinished key is in the archive of Runner's keys, never removed.
	if got := archivedIn(t, folder, name); !slices.Equal(got, kindsArchived(archiveCreationStopped, "keys.jsonl", "seed", "creating")) {
		t.Errorf("the archive of Runner's keys holds %q", got)
	}
	if got := readFile(t, archivedFile(t, folder, name, "seed")); got != "unfinished\n" {
		t.Errorf("the archived seed is %q", got)
	}
}

// **A move that stops is finished by the next start.** It archives the list,
// then the seed, then the marker, each only while it is the file inspected:
// stopped before any of the three, the marker always stays, and the next
// start archives what is left and makes and names another key. Nothing is
// removed.
func TestAnInterruptedRemovalIsFinishedByTheNextStart(t *testing.T) {
	const name = "c000000000000000000000000000000c"
	all := kindsArchived(archiveCreationStopped, "keys.jsonl", "seed", "creating")
	for _, c := range []struct {
		stop            string
		left            []string
		archived, final []string
	}{
		{name + ".keys.jsonl", []string{name + ".creating", name + ".keys.jsonl", name + ".seed"}, nil, all},
		{name + ".seed", []string{name + ".creating", name + ".seed"}, kindsArchived(archiveCreationStopped, "keys.jsonl"), all},
		// The marker left alone, with nothing made beside it, is removed:
		// it records nothing Desk could not decide.
		{name + ".creating", []string{name + ".creating"}, kindsArchived(archiveCreationStopped, "keys.jsonl", "seed"), kindsArchived(archiveCreationStopped, "keys.jsonl", "seed")},
	} {
		t.Run("stopped before "+c.stop, func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "jpack")
			writeStandInRuntime(t, bin, reading(allConfigVersions), lockingAs(wantGatedConfig))
			s, _, _ := gatesServer(t, bin)
			leaveUnfinished(t, s, name)
			folder := runnerFolderOf(s)
			testHookRunnerKeyIO = func(step string) error {
				if step == "remove "+c.stop {
					return syscall.EIO
				}
				return nil
			}
			t.Cleanup(func() { testHookRunnerKeyIO = nil })
			k := s.newRunnerKey(name)
			if path := k.decide(); path != "" {
				t.Fatalf("named %s", path)
			}
			if got := *k.report(); got.Reason != "not-read-now" {
				t.Errorf("reported %+v", got)
			}
			if names := liveIn(t, folder); !slices.Equal(names, c.left) {
				t.Fatalf("a move stopped before %s left %q, want %q", c.stop, names, c.left)
			}
			if got := archivedIn(t, folder, name); !slices.Equal(got, c.archived) {
				t.Fatalf("a move stopped before %s archived %q, want %q", c.stop, got, c.archived)
			}
			testHookRunnerKeyIO = nil
			if path := k.decide(); path != filepath.Join(folder, name+".seed") {
				t.Fatalf("the next start reported %+v", *k.report())
			}
			if names := liveIn(t, folder); !slices.Equal(names, []string{name + ".keys.jsonl", name + ".seed"}) {
				t.Errorf("the folder holds %q", names)
			}
			if got := archivedIn(t, folder, name); !slices.Equal(got, c.final) {
				t.Errorf("the archive holds %q, want %q", got, c.final)
			}
		})
	}
}

// **A removal acts on exactly the files inspected.** Where the seed is
// replaced between the inspection and its removal, it is not removed, the
// marker stays, and no key is named; the next start, which inspects the
// replacement, removes it.
func TestARemovalLeavesAFileReplacedSinceItsInspection(t *testing.T) {
	const name = "c000000000000000000000000000000d"
	bin := filepath.Join(t.TempDir(), "jpack")
	writeStandInRuntime(t, bin, reading(allConfigVersions), lockingAs(wantGatedConfig))
	s, _, _ := gatesServer(t, bin)
	leaveUnfinished(t, s, name)
	folder := runnerFolderOf(s)
	seed := filepath.Join(folder, name+".seed")
	testHookKeyBetween = func(at string) {
		if at != "before removal" {
			return
		}
		if err := os.WriteFile(seed+".other", []byte("replaced\n"), 0o600); err != nil {
			t.Error(err)
		}
		if err := os.Rename(seed+".other", seed); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { testHookKeyBetween = nil })
	k := s.newRunnerKey(name)
	if path := k.decide(); path != "" {
		t.Fatalf("named %s", path)
	}
	testHookKeyBetween = nil
	if got := readFile(t, seed); got != "replaced\n" {
		t.Errorf("a seed replaced since its inspection was removed: %q", got)
	}
	if _, err := os.Lstat(filepath.Join(folder, name+".creating")); err != nil {
		t.Errorf("the marker did not stay: %v", err)
	}
	if path := k.decide(); path != seed || readFile(t, seed) != standInSeed+"\n" {
		t.Errorf("the next start named %q", path)
	}
}

// **Neither sweep touches the other's files.** A desk's key and a Runner's
// key under the same id, each with its creation marker: the desk sweep at
// start (`sweepUnfinishedKeys`, no desk of that id published) archives the
// desk's three files and none of the Runner's; the Runner key's start
// archives the Runner's three, makes another key, and touches none of the
// desk's.
func TestNeitherSweepTouchesTheOthersKey(t *testing.T) {
	const id = "c0000000000000000000000000000005"
	bin := filepath.Join(t.TempDir(), "jpack")
	writeStandInRuntime(t, bin, reading(allConfigVersions), lockingAs(wantGatedConfig))
	s, _, _ := gatesServer(t, bin)
	k := s.newRunnerKey(id)
	if k.decide() == "" {
		t.Fatal("the key was not made")
	}
	signing, runner := signingFolderOf(s), runnerFolderOf(s)
	plant := func(folder string) {
		t.Helper()
		for _, file := range []string{id + ".seed", id + ".keys.jsonl", id + ".creating"} {
			if _, err := os.Lstat(filepath.Join(folder, file)); err == nil {
				continue
			}
			if err := os.WriteFile(filepath.Join(folder, file), plantedAs(file), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	plant(signing)
	plant(runner)
	runnerFiles := keptIn(t, runner)
	s.sweepUnfinishedKeys()
	if names := liveIn(t, signing); !slices.Equal(names, []string{"runner"}) {
		t.Errorf("the desk sweep left %q in the signing folder", names)
	}
	if got := archivedIn(t, signing, id); !slices.Equal(got, kindsArchived(archiveNeverPublished, "keys.jsonl", "seed", "creating")) {
		t.Errorf("the desk sweep archived %q", got)
	}
	assertKept(t, runner, runnerFiles)

	plant(signing)
	deskFiles := keptIn(t, signing)
	if path := s.newRunnerKey(id).decide(); path != filepath.Join(runner, id+".seed") {
		t.Fatalf("the Runner key's start named %q", path)
	}
	if names := liveIn(t, runner); !slices.Equal(names, []string{id + ".keys.jsonl", id + ".seed"}) || readFile(t, filepath.Join(runner, id+".seed")) != standInSeed+"\n" {
		t.Errorf("the Runner sweep left %q, not a key made again", names)
	}
	if got := archivedIn(t, runner, id); !slices.Equal(got, kindsArchived(archiveCreationStopped, "keys.jsonl", "seed", "creating")) {
		t.Errorf("the Runner sweep archived %q", got)
	}
	assertKept(t, signing, deskFiles)
}

// **A folder on the key's path that others can write is refused, and said**
// (issue #215's third criterion). `runner/` or `signing/` made writable by
// group: custody refuses it, as a folder of Desk's own that it does not
// repair; Runner is named no key; nothing kept is changed; and the reason
// names the folder by name, never by path. The runtime's own wording for the
// same placement is held in jobs_test.go, with the pinned runtime.
func TestAGroupWritableFolderOnTheKeysPathIsRefused(t *testing.T) {
	const name = "c0000000000000000000000000000006"
	for _, c := range []struct {
		folder func(s *Server) string
		detail string
	}{
		{runnerFolderOf, "the folder Desk keeps Runner's keys in is writable by group or others (mode 0770); a directory anyone could write to may already hold something they put there, so narrowing it now would not make it safe — remove the 020 bits and restart the desk"},
		{signingFolderOf, "Desk's signing folder is writable by group or others (mode 0770); a directory anyone could write to may already hold something they put there, so narrowing it now would not make it safe — remove the 020 bits and restart the desk"},
	} {
		bin := filepath.Join(t.TempDir(), "jpack")
		writeStandInRuntime(t, bin, reading(allConfigVersions), lockingAs(wantGatedConfig))
		s, _, logged := gatesServer(t, bin)
		if s.newRunnerKey(name).decide() == "" {
			t.Fatal("the key was not made")
		}
		before := keptIn(t, runnerFolderOf(s))
		if err := os.Chmod(c.folder(s), 0o770); err != nil {
			t.Fatal(err)
		}
		k := s.newRunnerKey(name)
		if path := k.decide(); path != "" {
			t.Fatalf("named %s under a group-writable folder", path)
		}
		if got, want := *k.report(), (RunnerKeyStatus{State: "unsigned", Reason: "custody", Detail: c.detail}); got != want {
			t.Errorf("reported\n%+v\nwant\n%+v", got, want)
		}
		if got := permOf(t, c.folder(s)); got != 0o770 {
			t.Errorf("the refused folder was changed to %v", got)
		}
		if err := os.Chmod(c.folder(s), 0o700); err != nil {
			t.Fatal(err)
		}
		assertKept(t, runnerFolderOf(s), before)
		if strings.Contains(logged.String(), s.configDir) {
			t.Errorf("the log names a path: %s", logged)
		}
	}
}

// **A key the runtime does not make leaves nothing, and is said** in the
// runtime's words with no path; the next start tries again.
func TestARunnerKeyTheRuntimeDoesNotMakeLeavesNothing(t *testing.T) {
	const name = "c0000000000000000000000000000007"
	bin := filepath.Join(t.TempDir(), "jpack")
	calls := writeStandInRuntime(t, bin, reading(allConfigVersions), lockingAs(wantGatedConfig))
	s, _, _ := gatesServer(t, bin)
	generatingAs(t, calls, `  printf '%s\n' "{\"outputVersion\":\"2\",\"command\":\"audit key generate\",\"status\":\"error\",\"diagnostics\":[{\"code\":\"JPS-AUDIT-KEY-REFUSED\",\"message\":\"No seed was written to $4, where a signing key is refused: the directory ${4%/*} is owned by uid 4242, neither root nor the user this runtime runs as (uid 1000).\"}]}"; exit 1`)
	k := s.newRunnerKey(name)
	if path := k.decide(); path != "" {
		t.Fatalf("named %s", path)
	}
	want := RunnerKeyStatus{State: "unsigned", Reason: "not-made", Detail: "No seed was written to …, where a signing key is refused: the directory … is owned by uid 4242, neither root nor the user this runtime runs as (uid 1000)"}
	if got := *k.report(); got != want {
		t.Errorf("reported\n%+v\nwant\n%+v", got, want)
	}
	if names := namesIn(t, runnerFolderOf(s)); len(names) != 0 {
		t.Errorf("a failed creation left %q", names)
	}
	if err := os.Remove(calls + ".generate"); err != nil {
		t.Fatal(err)
	}
	if path := k.decide(); path != filepath.Join(runnerFolderOf(s), name+".seed") {
		t.Errorf("the next start named %q", path)
	}
}

// **A creation whose marker cannot be removed names no key**: its marker is
// replaced while the runtime makes the key, so the creation finds it is not
// the marker it wrote and leaves it. Runner starts without the key; the next
// start, under the lock, removes all three and makes another.
func TestARunnerKeyWhoseMarkerStaysIsNotNamed(t *testing.T) {
	const name = "c0000000000000000000000000000008"
	bin := filepath.Join(t.TempDir(), "jpack")
	writeStandInRuntime(t, bin, reading(allConfigVersions), lockingAs(wantGatedConfig))
	s, _, _ := gatesServer(t, bin)
	if s.newRunnerKey(strings.Repeat("ab", 32)).decide() == "" {
		t.Fatal("the folder was not made")
	}
	marker := filepath.Join(runnerFolderOf(s), name+".creating")
	testHookKeyBetween = func(at string) {
		if at == "after generate" {
			// Written beside it and renamed over it, so that it is another
			// file, never the same inode given again.
			if err := os.WriteFile(marker+".other", nil, 0o600); err != nil {
				t.Error(err)
			}
			if err := os.Rename(marker+".other", marker); err != nil {
				t.Error(err)
			}
		}
	}
	t.Cleanup(func() { testHookKeyBetween = nil })
	k := s.newRunnerKey(name)
	if path := k.decide(); path != "" {
		t.Fatalf("a key whose marker stayed was named: %s", path)
	}
	testHookKeyBetween = nil
	if got := *k.report(); got != (RunnerKeyStatus{State: "unsigned", Reason: "unfinished"}) {
		t.Errorf("reported %+v", got)
	}
	if _, err := os.Lstat(marker); err != nil {
		t.Errorf("the marker is gone: %v", err)
	}
	if path := k.decide(); path != filepath.Join(runnerFolderOf(s), name+".seed") {
		t.Errorf("the next start named %q", path)
	}
	if _, err := os.Lstat(marker); !os.IsNotExist(err) {
		t.Errorf("the marker was left: %v", err)
	}
}

// **Runner's refusal at boot, as Runner says it.** A Runner that refuses the
// key is started again at once without it, so its runs go on, unsigned; the
// key is left as it is, and desk-config says why in Runner's words.
func TestARunnerThatRefusesItsKeyStartsWithout(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "jpack")
	writeStandInRuntime(t, bin, reading(allConfigVersions), lockingAs(wantGatedConfig))
	runner, received := recordingRunner(t, true)
	s, ts, _ := runnerDesk(t, bin, runner, t.TempDir())
	started(t, s.jobs)
	boots := bootLines(t, received)
	if len(boots) != 2 || bootKey(t, boots[0]) == "" || bootKey(t, boots[1]) != "" {
		t.Fatalf("the Runner was booted with %q, want the key and then none", readFile(t, received))
	}
	reported, _ := runnerKeyReported(t, ts, "")
	if want := (RunnerKeyStatus{State: "unsigned", Reason: "runner-refused", Detail: "it must have one name, with no hard link elsewhere"}); reported != want {
		t.Errorf("reported\n%+v\nwant\n%+v", reported, want)
	}
	if _, err := os.Lstat(bootKey(t, boots[0])); err != nil {
		t.Errorf("the refused key was removed: %v", err)
	}
}

// Runner's refusal is read only from the line that says it refused the key.
func TestOnlyRunnersRefusalOfTheKeyIsTakenForOne(t *testing.T) {
	for said, want := range map[string]string{
		"runner: the signing key is refused: its path must be absolute and clean\n": "its path must be absolute and clean",
		"note\nrunner: the signing key is refused: it must be a regular file\n":     "it must be a regular file",
	} {
		if got, refused := runnerKeyRefusal([]byte(said)); !refused || got != want {
			t.Errorf("%q: %q %v", said, got, refused)
		}
	}
	for _, said := range []string{"", "runner: absolute paths and workspace owner required\n", "the signing key is refused: x\n", " runner: the signing key is refused: x\n"} {
		if _, refused := runnerKeyRefusal([]byte(said)); refused {
			t.Errorf("%q was taken for a refusal of the key", said)
		}
	}
}

// **No part of the key's path is said**, however the configuration folder is
// spelled: with a space, through a link, or with a separator doubled, each
// spelling of the key's path and its folders in the runtime's refusal is
// replaced whole.
func TestNoPartOfARunnerKeysPathIsSaid(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(base, "a folder with spaces", "jpack-desk")
	if err := os.MkdirAll(config, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(filepath.Join(base, "a folder with spaces"), link); err != nil {
		t.Fatal(err)
	}
	s := &Server{configDir: config}
	const name = "c0000000000000000000000000000009"
	runner := filepath.Join(config, "secrets", "signing", "runner")
	seed := filepath.Join(runner, name+".seed")
	for _, said := range []string{
		"The key at " + seed + " is refused: the directory " + runner + " on the signing key's path can be written by its group; chmod go-w " + runner + " fixes it.",
		"The key at " + strings.Replace(seed, "/runner/", "//runner/", 1) + " is refused.",
		"The key at " + filepath.Join(link, "jpack-desk", "secrets", "signing", "runner", name+".seed") + " is refused.",
		"No seed was written to " + filepath.Join(config, "secrets", "signing") + ".",
	} {
		got := s.runnerKeyWords(said, name, false)
		for _, part := range []string{"folder with spaces", "jpack-desk", "secrets/", "signing/", "/runner", base} {
			if strings.Contains(got, part) {
				t.Errorf("%q\nbecame %q, which names %q", said, got, part)
			}
		}
	}
	for said, want := range map[string]string{
		runner + " is writable by group or others (mode 0770)":                   "the folder Desk keeps Runner's keys in is writable by group or others (mode 0770)",
		seed + " is a symbolic link; " + runner + " is not":                      "… is a symbolic link; the folder Desk keeps Runner's keys in is not",
		filepath.Join(config, "secrets", "signing") + " is another user's":       "Desk's signing folder is another user's",
		filepath.Join(link, "jpack-desk", "secrets") + " could not be inspected": "… could not be inspected",
	} {
		if got := s.runnerKeyWords(said, name, true); got != want {
			t.Errorf("Desk's own sentence %q\nbecame %q\nwant %q", said, got, want)
		}
	}
}

// **Runner's folder is custody's**: a loose one, 0755 as a umask of 022
// makes it, is narrowed to 0700 and used; one swapped for another between
// its check and its open is not used, and the key is not named.
func TestTheRunnerKeysFolderIsHeldToCustody(t *testing.T) {
	const name = "c000000000000000000000000000000a"
	bin := filepath.Join(t.TempDir(), "jpack")
	writeStandInRuntime(t, bin, reading(allConfigVersions), lockingAs(wantGatedConfig))
	s, _, _ := gatesServer(t, bin)
	folder := runnerFolderOf(s)
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if path := s.newRunnerKey(name).decide(); path != filepath.Join(folder, name+".seed") {
		t.Fatalf("a loose folder of Desk's own was not narrowed and used: %q", path)
	}
	if got := permOf(t, folder); got != 0o700 {
		t.Errorf("the folder is %v, want 0700", got)
	}

	testHookAfterCustodyCheck = func(path string) {
		if path != folder {
			return
		}
		if err := os.Rename(folder, folder+".aside"); err != nil {
			t.Error(err)
		}
		if err := os.Mkdir(folder, 0o700); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { testHookAfterCustodyCheck = nil })
	k := s.newRunnerKey(name)
	if path := k.decide(); path != "" {
		t.Fatalf("a folder swapped between its check and its open was used: %s", path)
	}
	testHookAfterCustodyCheck = nil
	if got := *k.report(); got.Reason != "custody" || got.Detail != "the folder Desk keeps Runner's keys in changed between being checked and being opened, and was not used" {
		t.Errorf("reported %+v", got)
	}
}

// **The path Runner is given names the key Desk found.** The Runner keys'
// folder replaced by another holding a seed of the same name, after the key
// is found and before it is named; or the seed replaced while the runtime
// reads it: either way the key is not named.
func TestARunnerKeyWhosePathNamesAnotherIsNotNamed(t *testing.T) {
	const name = "c000000000000000000000000000000b"
	for _, c := range []struct {
		name    string
		arrange func(t *testing.T, s *Server, calls, folder string)
	}{
		{"the folder replaced", func(t *testing.T, s *Server, calls, folder string) {
			testHookKeyBetween = func(at string) {
				if at != "read list" {
					return
				}
				if err := os.Rename(folder, folder+".aside"); err != nil {
					t.Error(err)
				}
				if err := os.Mkdir(folder, 0o700); err != nil {
					t.Error(err)
				}
				if err := os.WriteFile(filepath.Join(folder, name+".seed"), []byte("swapped\n"), 0o600); err != nil {
					t.Error(err)
				}
			}
			// The runtime, given the path, would read the seed swapped in,
			// and answer another key for it.
			readingKeyAs(t, calls, `  if [ "$(cat "$4")" = swapped ]; then printf '%s\n' '`+strings.Replace(standInRead, standInPublicKey+`","keyId":"`+standInKeyID, secondPublicKey+`","keyId":"`+secondKeyID, 1)+`'; else printf '%s\n' '`+standInRead+`'; fi`)
		}},
		{"the seed replaced while the runtime reads it", func(t *testing.T, s *Server, calls, folder string) {
			readingKeyAs(t, calls, `  mv "$4" "$4.aside" && cp -p "$4.aside" "$4" && printf '%s\n' '`+standInRead+`'`)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "jpack")
			calls := writeStandInRuntime(t, bin, reading(allConfigVersions), lockingAs(wantGatedConfig))
			s, _, _ := gatesServer(t, bin)
			folder := runnerFolderOf(s)
			if s.newRunnerKey(name).decide() == "" {
				t.Fatal("the key was not made")
			}
			c.arrange(t, s, calls, folder)
			t.Cleanup(func() { testHookKeyBetween = nil })
			k := s.newRunnerKey(name)
			if path := k.decide(); path != "" {
				t.Fatalf("named %s", path)
			}
			testHookKeyBetween = nil
			if got := *k.report(); got != (RunnerKeyStatus{State: "unsigned", Reason: "not-used", Detail: "the key is not at the path Runner would be given"}) {
				t.Errorf("reported %+v", got)
			}
		})
	}
}

// A configuration folder whose path is not valid UTF-8 cannot be carried on
// Runner's boot line, a JSON object, as itself: no key is named, and why is
// said.
func TestARunnerKeyWhosePathIsNotUTF8IsNotNamed(t *testing.T) {
	config := filepath.Join(t.TempDir(), "conf\xff")
	if err := os.Mkdir(config, 0o700); err != nil {
		t.Skipf("this file system takes no such name: %v", err)
	}
	bin := filepath.Join(t.TempDir(), "jpack")
	writeStandInRuntime(t, bin, reading(allConfigVersions), lockingAs(wantGatedConfig))
	s, ts := startDesk(t, Config{ProjectDir: t.TempDir(), JpackBin: bin, DeskConfigDir: config, Token: testToken})
	t.Cleanup(func() { ts.Close(); s.Close() })
	k := s.newRunnerKey(strings.Repeat("ab", 32))
	if path := k.decide(); path != "" {
		t.Fatalf("named %q", path)
	}
	if got := *k.report(); got != (RunnerKeyStatus{State: "unsigned", Reason: "custody", Detail: "the path of Desk's signing folder is not valid UTF-8, which Runner's boot line cannot carry"}) {
		t.Errorf("reported %+v", got)
	}
}
