package desk

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runnerPathProbe(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	marker := filepath.Join(dir, "dispatched")
	bin := filepath.Join(dir, "runner")
	// Authored fixture: no installed Runner or Runtime is ever invoked.
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n: > '"+marker+"'\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return bin, marker
}

func runnerPathAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected side effect at %q: %v", path, err)
	}
}

func TestRunnerPathBootAdmission(t *testing.T) {
	setters := map[string]func(*jobsCompanion, string){
		"binary":        func(j *jobsCompanion, v string) { j.bin += v },
		"runtime":       func(j *jobsCompanion, v string) { j.runtime += v },
		"dir":           func(j *jobsCompanion, v string) { j.dir += v },
		"workspace":     func(j *jobsCompanion, v string) { j.workspace += v },
		"owner":         func(j *jobsCompanion, v string) { j.owner += v },
		"inputRoot":     func(j *jobsCompanion, v string) { j.inputRoot += v },
		"keyConfig":     func(j *jobsCompanion, v string) { j.key.s.configDir += v },
		"keyName":       func(j *jobsCompanion, v string) { j.key.name += v },
		"ownershipRoot": func(j *jobsCompanion, v string) { j.ownership.Root += v },
	}
	for _, bad := range []struct{ name, value string }{{"invalidUTF8", string([]byte{0xff})}, {"NUL", "\x00"}} {
		for field, set := range setters {
			for _, owned := range []bool{false, true} {
				t.Run(bad.name+"/"+field+map[bool]string{false: "/legacy", true: "/owned"}[owned], func(t *testing.T) {
					bin, marker := runnerPathProbe(t)
					base := t.TempDir()
					config := filepath.Join(base, "config")
					j := &jobsCompanion{bin: bin, runtime: bin, dir: filepath.Join(base, "state"), workspace: "stable-workspace", owner: "local-owner", inputRoot: base, key: (&Server{configDir: config}).newRunnerKey("stable-workspace")}
					if owned {
						j.ownership = &runnerExecutorOwnership{}
						j.key = nil
					}
					// Key fields belong only to the signing route; ownership belongs to the owned route.
					if !owned && field == "ownershipRoot" {
						return
					}
					if owned && strings.HasPrefix(field, "key") {
						return
					}
					set(j, bad.value)
					if _, _, err := j.endpoint(); !errors.Is(err, errRunnerPathEncoding) {
						t.Fatalf("endpoint admitted %s: %v", field, err)
					}
					if _, err := j.start("", ""); !errors.Is(err, errRunnerPathEncoding) {
						t.Fatalf("direct start admitted %s: %v", field, err)
					}
					runnerPathAbsent(t, filepath.Join(base, "state"))
					runnerPathAbsent(t, config)
					runnerPathAbsent(t, marker)
					if j.token != "" || j.done != nil || j.input != nil {
						t.Fatal("startup state changed")
					}
				})
			}
		}
		t.Run(bad.name+"/signingKey", func(t *testing.T) {
			bin, marker := runnerPathProbe(t)
			j := &jobsCompanion{bin: bin, runtime: bin, dir: t.TempDir()}
			if _, err := j.start("seed"+bad.value, ""); !errors.Is(err, errRunnerPathEncoding) {
				t.Fatal(err)
			}
			runnerPathAbsent(t, marker)
		})
	}
	t.Run("ownershipRoot/literalReplacementCharacter", func(t *testing.T) {
		j := &jobsCompanion{ownership: &runnerExecutorOwnership{Root: filepath.Join(t.TempDir(), "root-�")}}
		if err := j.validateRunnerPaths(); err != nil {
			t.Fatalf("literal U+FFFD ownership root refused: %v", err)
		}
	})
	t.Run("literalReplacementCharacter", func(t *testing.T) {
		bin, marker := runnerPathProbe(t)
		base := t.TempDir()
		j := &jobsCompanion{bin: bin, runtime: bin, dir: filepath.Join(base, "�"), workspace: "workspace-�", owner: "owner-�", inputRoot: filepath.Join(base, "input-�")}
		_, _, err := j.endpoint()
		if err == nil || errors.Is(err, errRunnerPathEncoding) {
			t.Fatalf("expected fixture failure after admission: %v", err)
		}
		if _, err := os.Stat(j.dir); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(marker); err != nil {
			t.Fatal("literal U+FFFD did not dispatch", err)
		}
	})
}

func TestRunnerPathLocationAdmission(t *testing.T) {
	for _, bad := range []string{string([]byte{0xff}), "\x00"} {
		for _, field := range []string{"config", "project", "workspace", "owner"} {
			config, project := "/config", "/project"
			home := &deskHome{Registry: deskHomeRegistry{Owner: "owner", Desks: []deskHomeEntry{{Folder: "project", RunnerWorkspace: "workspace"}}}, Path: "/"}
			switch field {
			case "config":
				config += bad
			case "project":
				project += bad
				home.Registry.Desks[0].Folder += bad
			case "workspace":
				home.Registry.Desks[0].RunnerWorkspace += bad
			case "owner":
				home.Registry.Owner += bad
			}
			if _, err := resolveJobsLocation(config, project, "", home); !errors.Is(err, errRunnerPathEncoding) {
				t.Fatalf("%s admitted: %v", field, err)
			}
		}
		if _, err := offlineJobsLocation("/config"+bad, &ProjectRoot{dir: "/project"}); !errors.Is(err, errRunnerPathEncoding) {
			t.Fatal("offline route admitted", err)
		}
	}
	location, err := resolveJobsLocation("/config-�", "/project-�", "", nil)
	if err != nil || location.Workspace != digestOf([]byte("/project-�")) || location.Owner != "local-owner:"+digestOf([]byte("/config-�")) {
		t.Fatal("namespace identity changed", location, err)
	}
}

func TestRunnerPathRelocationAdmission(t *testing.T) {
	for _, bad := range []string{string([]byte{0xff}), "\x00"} {
		for _, field := range []string{"binary", "dir", "workspace", "owner", "previousInputRoot", "inputRoot"} {
			bin, marker := runnerPathProbe(t)
			q := homeRunnerRelocation{Version: 1, Dir: "/jobs", Workspace: "workspace", Owner: "owner", PreviousInputRoot: "/old", InputRoot: "/new"}
			switch field {
			case "binary":
				bin += bad
			case "dir":
				q.Dir += bad
			case "workspace":
				q.Workspace += bad
			case "owner":
				q.Owner += bad
			case "previousInputRoot":
				q.PreviousInputRoot += bad
			case "inputRoot":
				q.InputRoot += bad
			}
			if err := applyHomeRunnerRelocation(bin, q); !errors.Is(err, errRunnerPathEncoding) {
				t.Fatalf("%s admitted: %v", field, err)
			}
			runnerPathAbsent(t, marker)
		}
	}
	bin, marker := runnerPathProbe(t)
	if err := applyHomeRunnerRelocation(bin, homeRunnerRelocation{Dir: "/jobs-�", Workspace: "workspace-�", Owner: "owner-�", PreviousInputRoot: "/old-�", InputRoot: "/new-�"}); err == nil || errors.Is(err, errRunnerPathEncoding) {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("literal U+FFFD did not dispatch", err)
	}
}

func TestRunnerPathRelocationQueueAdmission(t *testing.T) {
	for _, bad := range []string{string([]byte{0xff}), "\x00"} {
		for _, field := range []string{"destination", "workspace", "owner", "source"} {
			bin, marker := runnerPathProbe(t)
			source := t.TempDir()
			if field == "source" { // A filesystem can retain raw bytes; no JSON repair is allowed.
				if bad == "\x00" {
					continue
				}
				source = filepath.Join(source, "raw"+bad)
			}
			jobs := filepath.Join(source, ".desk-private", "jobs")
			if err := os.MkdirAll(jobs, 0700); err != nil {
				t.Fatal(err)
			}
			journal := []byte("retained-jobs-journal")
			if err := os.WriteFile(filepath.Join(jobs, "runs.sqlite"), journal, 0600); err != nil {
				t.Fatal(err)
			}
			s := &Server{projectDir: source, configDir: t.TempDir(), home: &deskHome{Path: filepath.Dir(source)}, cfg: Config{RunnerBin: bin}}
			destinationParent := t.TempDir()
			plan := deskHomePlan{Destination: filepath.Join(destinationParent, "new"), Registry: deskHomeRegistry{Owner: "owner", Desks: []deskHomeEntry{{Kind: "project", Folder: filepath.Base(source), RunnerWorkspace: "workspace"}}}}
			switch field {
			case "destination":
				plan.Destination += bad
			case "workspace":
				plan.Registry.Desks[0].RunnerWorkspace += bad
			case "owner":
				plan.Registry.Owner += bad
			}
			rows, err := s.homeRunnerRelocations(plan)
			if !errors.Is(err, errRunnerPathEncoding) || len(rows) != 0 {
				t.Fatalf("%s queued: %v %v", field, rows, err)
			}
			tx, err := s.prepareHomeTransaction(plan)
			if !errors.Is(err, errRunnerPathEncoding) || len(tx.RunnerRelocations) != 0 {
				t.Fatalf("%s prepared: %v", field, err)
			}
			runnerPathAbsent(t, marker)
			entries, err := os.ReadDir(destinationParent)
			if err != nil || len(entries) != 0 {
				t.Fatalf("destination container changed: %v %v", entries, err)
			}
			retained, err := os.ReadFile(filepath.Join(jobs, "runs.sqlite"))
			if err != nil || string(retained) != string(journal) {
				t.Fatalf("source journal changed: %q %v", retained, err)
			}
			entries, err = os.ReadDir(s.configDir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("config journal container changed: %v %v", entries, err)
			}
		}
	}
	// A real retained Jobs row with literal U+FFFD keeps its reviewed fields.
	bin, marker := runnerPathProbe(t)
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n: > '"+marker+"'\necho '{\"version\":1}'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	sourceParent := t.TempDir()
	source := filepath.Join(sourceParent, "source-�")
	jobs := filepath.Join(source, ".desk-private", "jobs")
	if err := os.MkdirAll(jobs, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jobs, "runs.sqlite"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	s := &Server{projectDir: source, home: &deskHome{Path: sourceParent}, cfg: Config{RunnerBin: bin}}
	plan := deskHomePlan{Destination: filepath.Join(t.TempDir(), "new-�"), Registry: deskHomeRegistry{Owner: "owner-�", Desks: []deskHomeEntry{{Kind: "project", Folder: "source-�", RunnerWorkspace: "workspace-�"}}}}
	rows, err := s.homeRunnerRelocations(plan)
	if err != nil || len(rows) != 1 || rows[0].PreviousInputRoot != source || rows[0].Workspace != "workspace-�" || rows[0].Owner != "local-owner:owner-�" {
		t.Fatal("literal U+FFFD queue changed", rows, err)
	}
	// Raw local folders with no retained Jobs remain eligible for local moves.
	source = filepath.Join(t.TempDir(), "raw"+string([]byte{0xff}))
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	s = &Server{projectDir: source, configDir: t.TempDir()}
	rows, err = s.homeRunnerRelocations(deskHomePlan{Destination: "/new", Registry: deskHomeRegistry{Desks: []deskHomeEntry{{Kind: "project", RunnerWorkspace: "workspace"}}}})
	if err != nil || len(rows) != 0 {
		t.Fatal("local raw path was refused", err)
	}
}

func TestRunnerPathManualStoreAdmission(t *testing.T) {
	for _, value := range []string{string([]byte{0xff}), "\x00", "�"} {
		for _, field := range []string{"binary", "dir", "workspace", "owner", "ownershipRoot"} {
			bin, marker := runnerPathProbe(t)
			dir := filepath.Join(t.TempDir(), "state")
			if field == "dir" {
				dir += value
			}
			// NUL cannot name a folder; refusal must precede any attempt to use it.
			if value != "\x00" || field != "dir" {
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			lease, err := os.CreateTemp(t.TempDir(), "lease")
			if err != nil {
				t.Fatal(err)
			}
			q := runnerStorePrepareRequest{Version: 1, PrepareID: strings.Repeat("a", 64), Dir: dir, Workspace: "workspace", Owner: "owner", LeaseFD: 3, ExecutionDemand: policyExecutionDemand{Version: 1, InstallationID: strings.Repeat("b", 64), MachineWorkspace: strings.Repeat("c", 64), RunnerWorkspace: "workspace", ExecutorID: strings.Repeat("d", 64), Profile: policyExecutionProfile}}
			q.ExecutorOwnership.RunnerWorkspace = q.Workspace
			q.ExecutorOwnership.MachineWorkspace = q.ExecutionDemand.MachineWorkspace
			// Literal U+FFFD is controlled on filesystem/owner fields; workspace has
			// an independent existing namespace grammar.
			if value == "�" && field == "workspace" {
				lease.Close()
				continue
			}
			ownershipParent := t.TempDir()
			switch field {
			case "binary":
				bin += value
			case "workspace":
				q.Workspace += value
				q.ExecutionDemand.RunnerWorkspace = q.Workspace
				q.ExecutorOwnership.RunnerWorkspace = q.Workspace
			case "owner":
				q.Owner += value
			case "ownershipRoot":
				q.ExecutorOwnership.Root = filepath.Join(ownershipParent, "root") + value
			}
			if field == "binary" && value != "\x00" { // Supply the fixture at the exact byte path.
				raw, e := os.ReadFile(strings.TrimSuffix(bin, value))
				if e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(bin, raw, 0700); e != nil {
					t.Fatal(e)
				}
			}
			err = prepareRunnerManualStore(context.Background(), bin, lease, q)
			lease.Close()
			if err == nil {
				t.Fatal("fixture unexpectedly succeeded")
			}
			if value == "�" {
				if _, e := os.Stat(marker); e != nil {
					t.Fatalf("%s literal U+FFFD did not dispatch: %v", field, e)
				}
			} else {
				runnerPathAbsent(t, marker)
				entries, e := os.ReadDir(ownershipParent)
				if e != nil || len(entries) != 0 {
					t.Fatalf("ownership root container changed on refusal: %v %v", entries, e)
				}
				if value != "\x00" || field != "dir" {
					entries, e := os.ReadDir(dir)
					if e != nil || len(entries) != 0 {
						t.Fatalf("store changed on refusal: %v %v", entries, e)
					}
				}
			}
		}
	}
}
