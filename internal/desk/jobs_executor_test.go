package desk

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const runnerOwnershipReport = `{"version":2,"capabilities":["jobs/1","required-boot-capabilities/1","executor-ownership/1"],"governanceAdmission":false}`

func executorShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func ownedTestCompanion(t *testing.T, probe string, normal string) (*jobsCompanion, string) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "runner")
	script := "#!/bin/sh\nif [ \"$1 $2 $3\" = 'capabilities --format json' ]; then\n" +
		"printf 'probe\\n' >> " + executorShellQuote(filepath.Join(dir, "probes")) + "\n" +
		"if IFS= read -r unexpected; then exit 20; fi\n" + probe + "\nexit\nfi\n" + normal
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	source, err := pinRunnerExecutable(bin)
	if err != nil {
		t.Fatal(err)
	}
	j := &jobsCompanion{bin: bin, runtime: os.Args[0], dir: filepath.Join(dir, "state"), workspace: "runner-workspace", owner: "local-owner", stop: make(chan struct{}), runnerSource: source,
		ownership: &runnerExecutorOwnership{Version: 1, Root: "/machine/executors/machine-workspace", MachineWorkspace: "machine-workspace", RunnerWorkspace: "runner-workspace", RootDevice: "1", RootInode: "2", FileDevice: "1", FileInode: "3"}, ownershipCheck: func() error { return nil }}
	t.Cleanup(j.close)
	return j, dir
}

func goodExecutorProbe() string { return "printf '%s\\n' " + executorShellQuote(runnerOwnershipReport) }

func recordOwnedBoot(path string) string {
	return "IFS= read -r line\nprintf '%s\\n' \"$line\" >> " + executorShellQuote(path) + "\nprintf '{\"protocol\":\"jobs/1\",\"url\":\"http://127.0.0.1:9\"}\\n'\nwhile IFS= read -r _; do :; done\n"
}

func TestJobsExecutorCapabilityRefusalBeforeBootIsSticky(t *testing.T) {
	for _, row := range []struct{ name, probe string }{
		{"old", "printf '%s\\n' '{\"version\":1,\"capabilities\":[\"jobs/1\",\"required-boot-capabilities/1\"],\"governanceAdmission\":false}'"},
		{"unknown", "printf '%s\\n' " + executorShellQuote(strings.Replace(runnerOwnershipReport, `"version":2`, `"extra":true,"version":2`, 1))},
		{"duplicate", "printf '%s\\n' " + executorShellQuote(strings.Replace(runnerOwnershipReport, `"version":2`, `"version":1,"version":2`, 1))},
		{"malformed", "printf 'not-json\\n'"},
		{"missing-governance", "printf '%s\\n' " + executorShellQuote(strings.Replace(runnerOwnershipReport, `,"governanceAdmission":false`, "", 1))},
		{"governance-true", "printf '%s\\n' " + executorShellQuote(strings.Replace(runnerOwnershipReport, `false`, `true`, 1))},
		{"stderr", goodExecutorProbe() + "\nprintf 'unexpected\\n' >&2"},
		{"overflow", "i=0; while [ \"$i\" -lt 5000 ]; do printf x; i=$((i+1)); done"},
		{"extra-document", goodExecutorProbe() + "\nprintf '{}\\n'"},
		{"timeout", "/bin/sleep 4"},
	} {
		t.Run(row.name, func(t *testing.T) {
			boot := filepath.Join(t.TempDir(), "boot")
			j, dir := ownedTestCompanion(t, row.probe, recordOwnedBoot(boot))
			started := time.Now()
			if _, _, err := j.endpoint(); err == nil {
				t.Fatal("invalid capability started")
			}
			if time.Since(started) > 3*time.Second {
				t.Fatal("probe exceeded bounded timeout")
			}
			if _, _, err := j.endpoint(); err == nil {
				t.Fatal("held endpoint retried")
			}
			if raw, err := os.ReadFile(filepath.Join(dir, "probes")); err != nil || string(raw) != "probe\n" {
				t.Fatal("probe repeated", string(raw), err)
			}
			if _, err := os.Stat(boot); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("boot reached refused companion", err)
			}
		})
	}
}

func TestJobsExecutorSamePrivateSnapshotAndBoot(t *testing.T) {
	dir := t.TempDir()
	paths, boot := filepath.Join(dir, "paths"), filepath.Join(dir, "boot")
	trace := "printf '%s\\n' \"$0\" >> " + executorShellQuote(paths) + "\n"
	j, _ := ownedTestCompanion(t, trace+goodExecutorProbe(), trace+recordOwnedBoot(boot))
	if _, _, err := j.endpoint(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(paths)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 || lines[0] != lines[1] || lines[0] == j.bin {
		t.Fatal("probe and start did not share snapshot", lines)
	}
	var got struct {
		Required  runnerBootRequirements  `json:"requiredCapabilities"`
		Ownership runnerExecutorOwnership `json:"executorOwnership"`
	}
	raw, err = os.ReadFile(boot)
	if err != nil || json.Unmarshal(raw, &got) != nil || got.Ownership != *j.ownership || got.Required.Version != 1 || strings.Join(got.Required.Capabilities, ",") != strings.Join(runnerRequiredOwnershipCapabilities().Capabilities, ",") {
		t.Fatal("wrong ownership boot", string(raw), err)
	}
	j.close()
	if _, err := os.Stat(filepath.Dir(lines[0])); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("snapshot not cleaned", err)
	}
}

func TestJobsExecutorRejectsReplacementAndSetupFailure(t *testing.T) {
	for _, edge := range []string{"source-replaced", "source-rewritten", "source-after-probe", "state-unavailable", "binding-changed"} {
		t.Run(edge, func(t *testing.T) {
			boot := filepath.Join(t.TempDir(), "boot")
			j, dir := ownedTestCompanion(t, goodExecutorProbe(), recordOwnedBoot(boot))
			switch edge {
			case "source-replaced":
				os.Rename(j.bin, j.bin+"-old")
				os.WriteFile(j.bin, []byte("#!/bin/sh\nexit 1\n"), 0700)
			case "source-rewritten":
				os.WriteFile(j.bin, []byte("#!/bin/sh\nexit 1\n"), 0700)
			case "source-after-probe":
				// A trusted executable changing its installed source during the
				// probe cannot make Desk execute the replacement or re-pin it.
				j.runnerSource.close()
				probe := goodExecutorProbe() + "\n/bin/mv " + executorShellQuote(j.bin) + " " + executorShellQuote(j.bin+"-old")
				script := "#!/bin/sh\nif [ \"$1\" = capabilities ]; then\n" + probe + "\nexit\nfi\n" + recordOwnedBoot(boot)
				os.WriteFile(j.bin, []byte(script), 0700)
				var err error
				j.runnerSource, err = pinRunnerExecutable(j.bin)
				if err != nil {
					t.Fatal(err)
				}
			case "state-unavailable":
				os.WriteFile(j.dir, nil, 0600)
			case "binding-changed":
				j.ownershipCheck = func() error { return errors.New("changed") }
			}
			if _, _, err := j.endpoint(); err == nil {
				t.Fatal("invalid setup launched")
			}
			if _, _, err := j.endpoint(); err == nil {
				t.Fatal("invalid setup retried")
			}
			if _, err := os.Stat(boot); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("boot sent during failed setup", dir, err)
			}
		})
	}
}

func TestJobsExecutorSigningFallbackRetainsDemandAndFreshCustody(t *testing.T) {
	dir := t.TempDir()
	boot, paths := filepath.Join(dir, "boot"), filepath.Join(dir, "paths")
	trace := "printf '%s\\n' \"$0\" >> " + executorShellQuote(paths) + "\n"
	normal := "IFS= read -r line\nprintf '%s\\n' \"$line\" >> " + executorShellQuote(boot) + "\ncase \"$line\" in *'\"signingKey\"'*) printf 'runner: the signing key is refused: synthetic fixture refusal\\n' >&2; exit 1;; esac\nprintf '{\"protocol\":\"jobs/1\",\"url\":\"http://127.0.0.1:9\"}\\n'\nwhile IFS= read -r _; do :; done\n"
	j, _ := ownedTestCompanion(t, trace+goodExecutorProbe(), trace+normal)
	if err := os.Mkdir(j.dir, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := j.startWithSigningFallback("synthetic-key", RunnerKeyStatus{PublicKey: "synthetic-public"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(boot)
	if err != nil {
		t.Fatal(err)
	}
	boots := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(boots) != 2 {
		t.Fatal("did not exercise fallback", string(raw))
	}
	for i, line := range boots {
		var got map[string]json.RawMessage
		if json.Unmarshal([]byte(line), &got) != nil || got["executorOwnership"] == nil || got["requiredCapabilities"] == nil {
			t.Fatal("fallback lost demand", line)
		}
		if (got["signingKey"] != nil) != (i == 0) {
			t.Fatal("wrong signing fallback", line)
		}
	}
	raw, _ = os.ReadFile(paths)
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 4 || lines[0] != lines[1] || lines[2] != lines[3] || lines[0] == lines[2] {
		t.Fatal("fallback did not use fresh paired custody", lines)
	}
}

func TestJobsExecutorUnexpectedExitHoldsWithoutRelaunch(t *testing.T) {
	j, dir := ownedTestCompanion(t, goodExecutorProbe(), "IFS= read -r line\nprintf '{\"protocol\":\"jobs/1\",\"url\":\"http://127.0.0.1:9\"}\\n'\nwhile IFS= read -r _; do :; done\n")
	if _, _, err := j.endpoint(); err != nil {
		t.Fatal(err)
	}
	j.key = &runnerKey{s: &Server{log: log.New(io.Discard, "", 0)}, status: RunnerKeyStatus{State: runnerKeySigned}}
	j.cmd.Process.Kill()
	<-j.done
	if _, _, err := j.endpoint(); err == nil {
		t.Fatal("unexpected exit relaunched")
	}
	if j.key.status.State != runnerKeyNotRunning {
		t.Fatal("stopped Runner retained signing status", j.key.status)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "probes"))
	if string(raw) != "probe\n" {
		t.Fatal("unexpected exit repeated probe", string(raw))
	}
}

func TestJobsExecutorInitialHoldReportsRunnerUnavailable(t *testing.T) {
	j, _ := ownedTestCompanion(t, goodExecutorProbe(), "exit 1\n")
	j.key = &runnerKey{s: &Server{log: log.New(io.Discard, "", 0)}, status: RunnerKeyStatus{State: runnerKeyStarting}}
	j.held = runnerOwnershipUnavailable()
	if _, _, err := j.endpoint(); err == nil {
		t.Fatal("initial hold was ignored")
	}
	if j.key.status.State != runnerKeyNotRunning {
		t.Fatal("held Runner retained starting status", j.key.status)
	}
}

func TestJobsExecutorSharedWireFixture(t *testing.T) {
	raw, err := os.ReadFile("../../docs/design/executor-ownership-boot.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Required  runnerBootRequirements  `json:"requiredCapabilities"`
		Ownership runnerExecutorOwnership `json:"executorOwnership"`
	}
	if decodeDataJSON(raw, &fixture) != nil || fixture.Required.Version != 1 || fixture.Ownership.Version != 1 {
		t.Fatal("shared fixture was not decoded")
	}
	if fixture.Ownership.MachineWorkspace != "ws_machine" || fixture.Ownership.RunnerWorkspace != "runner-existing-identity" || fixture.Ownership.RootDevice != "123" || fixture.Ownership.RootInode != "456" || fixture.Ownership.FileDevice != "123" || fixture.Ownership.FileInode != "789" {
		t.Fatal("shared fixture identity drift", fixture)
	}
	if strings.Join(fixture.Required.Capabilities, ",") != strings.Join(runnerRequiredOwnershipCapabilities().Capabilities, ",") {
		t.Fatal("shared capability drift")
	}
}

func TestJobsExecutorConcurrentEndpointsLaunchOnce(t *testing.T) {
	boot := filepath.Join(t.TempDir(), "boot")
	j, dir := ownedTestCompanion(t, goodExecutorProbe(), recordOwnedBoot(boot))
	var work sync.WaitGroup
	failures := make(chan error, 8)
	for i := 0; i < 8; i++ {
		work.Add(1)
		go func() { defer work.Done(); _, _, err := j.endpoint(); failures <- err }()
	}
	work.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, "probes"))
	if err != nil || string(raw) != "probe\n" {
		t.Fatal("concurrent endpoints repeated probe", string(raw), err)
	}
	raw, err = os.ReadFile(boot)
	if err != nil || len(strings.Split(strings.TrimSpace(string(raw)), "\n")) != 1 {
		t.Fatal("concurrent endpoints repeated boot", err)
	}
}

func TestJobsExecutorPrivateSnapshotWithSymlinkTMPDIR(t *testing.T) {
	physical, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	paths, boot := filepath.Join(physical, "paths"), filepath.Join(physical, "boot")
	trace := "printf '%s\\n' \"$0\" >> " + executorShellQuote(paths) + "\n"
	// Pin the configured Runner from its physical path before changing TMPDIR.
	j, _ := ownedTestCompanion(t, trace+goodExecutorProbe(), trace+recordOwnedBoot(boot))
	link := filepath.Join(physical, "temporary-link")
	if err = os.Symlink(physical, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", link)
	if _, _, err = j.endpoint(); err != nil {
		t.Fatal("legitimate symlink TMPDIR refused", err)
	}
	raw, err := os.ReadFile(paths)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 || lines[0] != lines[1] || filepath.Dir(filepath.Dir(lines[0])) != physical || strings.Contains(lines[0], "temporary-link") {
		t.Fatal("probe and boot did not share physical scratch custody", lines)
	}
	if _, err = os.Stat(boot); err != nil {
		t.Fatal("boot was not delivered", err)
	}
	j.close()
	if _, err = os.Stat(filepath.Dir(lines[0])); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("physical scratch custody was not cleaned", err)
	}
}

func TestJobsExecutorUnsafeTemporaryParentRefusesBeforeCreation(t *testing.T) {
	for _, route := range []string{"direct", "alias-in-unsafe-parent", "unsafe-symlink-target-route"} {
		t.Run(route, func(t *testing.T) {
			base, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			boot := filepath.Join(base, "boot")
			j, sourceDir := ownedTestCompanion(t, goodExecutorProbe(), recordOwnedBoot(boot))
			unsafe, safe := filepath.Join(base, "unsafe"), filepath.Join(base, "safe")
			if err = os.Mkdir(unsafe, 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.Chmod(unsafe, 0777); err != nil {
				t.Fatal(err)
			}
			if err = os.Mkdir(safe, 0700); err != nil {
				t.Fatal(err)
			}
			temporary := unsafe
			if route != "direct" {
				alias := filepath.Join(unsafe, "alias")
				if err = os.Symlink(safe, alias); err != nil {
					t.Fatal(err)
				}
				temporary = alias
				if route == "unsafe-symlink-target-route" {
					temporary = filepath.Join(base, "outer-alias")
					if err = os.Symlink(alias, temporary); err != nil {
						t.Fatal(err)
					}
				}
			}
			t.Setenv("TMPDIR", temporary)
			if _, _, err = j.endpoint(); err == nil {
				t.Fatal("unsafe temporary route accepted")
			}
			for _, dir := range []string{safe, unsafe} {
				entries, err := os.ReadDir(dir)
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries {
					if strings.HasPrefix(entry.Name(), "jpack-desk-runner-") {
						t.Fatal("created scratch custody before validating parent")
					}
				}
			}
			if _, err = os.Stat(boot); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("unsafe temporary parent received boot", err)
			}
			if _, err = os.Stat(filepath.Join(sourceDir, "probes")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("unsafe temporary parent executed probe", err)
			}
		})
	}
}

func TestJobsExecutorSubstitutedScratchLeafPreservesVictim(t *testing.T) {
	for _, at := range []string{"before-copy", "at-cleanup"} {
		t.Run(at, func(t *testing.T) {
			base, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			victim := filepath.Join(base, "victim")
			if err = os.Mkdir(victim, 0700); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"sentinel", "runner"} {
				if err = os.WriteFile(filepath.Join(victim, name), []byte("preserve"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			j, _ := ownedTestCompanion(t, goodExecutorProbe(), "exit 1\n")
			t.Setenv("TMPDIR", base)
			var substituted string
			replace := func(dir string) {
				substituted = dir
				if err := os.Rename(dir, dir+"-retained"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(victim, dir); err != nil {
					t.Fatal(err)
				}
			}
			if at == "before-copy" {
				j.runnerSource.afterSnapshotDirectoryForTest = replace
				if _, _, err = j.endpoint(); err == nil {
					t.Fatal("substituted scratch leaf was followed")
				}
			} else {
				bin, cleanup, err := j.runnerSource.snapshot()
				if err != nil {
					t.Fatal(err)
				}
				replace(filepath.Dir(bin))
				cleanup()
				cleanup() // idempotent cleanup must not resolve the replacement.
			}
			for _, name := range []string{"sentinel", "runner"} {
				raw, err := os.ReadFile(filepath.Join(victim, name))
				if err != nil || string(raw) != "preserve" {
					t.Fatal("cleanup touched unverified target", name, err)
				}
			}
			if target, err := os.Readlink(substituted); err != nil || target != victim {
				t.Fatal("cleanup removed substituted entry", target, err)
			}
			if _, err = os.Stat(substituted + "-retained"); err != nil {
				t.Fatal("cleanup removed renamed scratch directory", err)
			}
		})
	}
}
