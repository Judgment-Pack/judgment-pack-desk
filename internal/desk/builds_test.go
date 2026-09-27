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
	if componentBuilds("", "").Runner != nil {
		t.Fatal("unconfigured runner reported installed")
	}
}
