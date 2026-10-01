package desk

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

func TestBuildIdentityExcludesCompilerSecrets(t *testing.T) {
	info := &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}, Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: "1234567"}, {Key: "vcs.modified", Value: "true"},
		{Key: "-ldflags", Value: "secret-build-value"}, {Key: "CGO_CFLAGS", Value: "private-path"},
	}}
	identity := buildIdentity(info)
	data, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	if identity.ModuleVersion != "" || identity.Revision != "1234567" || !identity.Modified {
		t.Fatalf("identity: %#v", identity)
	}
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "private") {
		t.Fatal("compiler configuration escaped into public metadata")
	}
}

func TestUnreadableBuildIsUnknownAndNeverExecuted(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "executed")
	runner := filepath.Join(dir, "runner")
	if err := os.WriteFile(runner, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	builds := componentBuilds(filepath.Join(dir, "missing"), runner)
	if builds.Runtime != (BuildIdentity{}) || builds.Runner == nil || *builds.Runner != (BuildIdentity{}) {
		t.Fatalf("invented build: %#v", builds)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("metadata inspection executed the companion")
	}
	if builds.SourceWorker == nil || *builds.SourceWorker != (BuildIdentity{}) {
		t.Fatal("missing source worker borrowed Runner identity")
	}
	if componentBuilds("", "").Runner != nil || componentBuilds("", "").SourceWorker != nil {
		t.Fatal("unconfigured runner reported installed")
	}
}

func TestSourceWorkerIdentityIsReadBesideRunnerAndNeverBorrowed(t *testing.T) {
	dir := t.TempDir()
	runner := filepath.Join(dir, executableName("jpack-runner"))
	worker := filepath.Join(dir, executableName("jpack-source-worker"))
	stamped := func(revision string) *debug.BuildInfo {
		return &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}, Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: revision}}}
	}
	read := map[string]*debug.BuildInfo{runner: stamped("runner-commit"), worker: stamped("worker-commit")}
	defer func(original func(string) (*debug.BuildInfo, error)) { readBuildInfo = original }(readBuildInfo)
	var asked []string
	readBuildInfo = func(path string) (*debug.BuildInfo, error) {
		asked = append(asked, path)
		if info := read[path]; info != nil {
			return info, nil
		}
		return nil, os.ErrNotExist
	}
	// Runtime lives elsewhere, so a worker looked up beside it is not found.
	runtime := filepath.Join(t.TempDir(), executableName("jpack"))
	builds := componentBuilds(runtime, runner)
	if builds.Runner == nil || builds.Runner.Revision != "runner-commit" || builds.SourceWorker == nil || builds.SourceWorker.Revision != "worker-commit" {
		t.Fatalf("runner and worker identities: %#v %#v", builds.Runner, builds.SourceWorker)
	}
	delete(read, worker)
	builds = componentBuilds(runtime, runner)
	if builds.SourceWorker == nil || *builds.SourceWorker != (BuildIdentity{}) {
		t.Fatalf("missing worker reported as %#v", builds.SourceWorker)
	}
	for _, path := range asked {
		if path != runtime && filepath.Dir(path) != dir {
			t.Fatalf("read outside the installation: %s", path)
		}
	}
}
