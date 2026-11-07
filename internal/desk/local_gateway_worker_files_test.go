//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package desk

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// **What the local gateway creates is owner-only.** The worker owns the
// gateway, which makes its store's folders and files under the inherited
// mask; at the usual 022 they were 0755 and 0644 inside Desk's config folder,
// and the storage census refused the installation over them, so every start
// after the local gateway had run began read-only. The worker sets an
// owner-only mask before anything else, even for a request it then refuses.
// It runs in a child process, as the worker does, so this test's own mask is
// untouched.
func TestLocalGatewayWorkerCreatesOwnerOnlyFiles(t *testing.T) {
	if dir := os.Getenv("DESK_WORKER_FILES_PROBE"); dir != "" {
		_ = RunLocalGatewayWorker(strings.NewReader("not a request\n"), &strings.Builder{})
		if err := os.MkdirAll(filepath.Join(dir, "store", "receipts"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "store", "receipts", "one.json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	dir := t.TempDir()
	probe := exec.Command(os.Args[0], "-test.run=^TestLocalGatewayWorkerCreatesOwnerOnlyFiles$", "-test.count=1")
	probe.Env = append(os.Environ(), "DESK_WORKER_FILES_PROBE="+dir)
	if out, err := probe.CombinedOutput(); err != nil {
		t.Fatalf("probe: %v\n%s", err, out)
	}
	for path, want := range map[string]os.FileMode{
		filepath.Join(dir, "store"):                         0o700,
		filepath.Join(dir, "store", "receipts"):             0o700,
		filepath.Join(dir, "store", "receipts", "one.json"): 0o600,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Fatalf("%s is %#o, want %#o", path, got, want)
		}
	}
}
