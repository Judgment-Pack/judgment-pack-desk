//go:build unix

package desk

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A FIFO or directory where a companion should be has no identity, and reading
// it cannot stall startup; a real executable is read through the same handle.
func TestBuildInfoReadsOnlyRegularFilesWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	runner := filepath.Join(dir, executableName("jpack-runner"))
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runner, raw, 0700); err != nil {
		t.Fatal(err)
	}
	if info, err := readBuildInfo(runner); err != nil || info == nil {
		t.Fatalf("a regular Go executable was not read: %v", err)
	}
	worker := filepath.Join(dir, executableName("jpack-source-worker"))
	if err := syscall.Mkfifo(worker, 0600); err != nil {
		t.Skip("cannot create a FIFO here:", err)
	}
	done := make(chan ComponentBuilds, 1)
	go func() { done <- componentBuilds(filepath.Join(dir, "missing"), runner) }()
	select {
	case builds := <-done:
		if builds.SourceWorker == nil || *builds.SourceWorker != (BuildIdentity{}) {
			t.Fatalf("FIFO worker reported as %#v", builds.SourceWorker)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reading a FIFO in the worker's place blocked startup")
	}
	if _, err := readBuildInfo(dir); err == nil {
		t.Fatal("a directory produced build information")
	}
}
