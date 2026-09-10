package desk

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveRuntimeRefusesWhatItCannotRun(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "no-such-jpack")
	if _, err := ResolveRuntime(missing); err == nil || !strings.Contains(err.Error(), missing) {
		t.Fatalf("a missing path must be refused by name; got %v", err)
	}
	if _, err := ResolveRuntime(dir); err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatalf("a directory must be refused as one; got %v", err)
	}
	plain := filepath.Join(dir, "jpack-plain")
	if err := os.WriteFile(plain, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveRuntime(plain); err == nil || !strings.Contains(err.Error(), "not executable") {
		t.Fatalf("a file without an execute bit must be refused; got %v", err)
	}
	if _, err := ResolveRuntime(""); err == nil {
		t.Fatal("an empty name must be refused")
	}
}

func TestResolveRuntimeReturnsAnAbsolutePathForAPathOrAName(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "jpack-stand-in")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveRuntime(bin)
	if err != nil || got != bin {
		t.Fatalf("an executable path resolves to itself; got %q, %v", got, err)
	}
	t.Setenv("PATH", dir)
	got, err = ResolveRuntime("jpack-stand-in")
	if err != nil || got != bin {
		t.Fatalf("a bare name resolves on PATH to the absolute path; got %q, %v", got, err)
	}
	if _, err := ResolveRuntime("jpack-not-on-path"); err == nil || !strings.Contains(err.Error(), "not on PATH") {
		t.Fatalf("a bare name absent from PATH must be refused; got %v", err)
	}
}
