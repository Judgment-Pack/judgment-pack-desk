//go:build linux

package desk

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// Observe the actual pinned Runtime while it waits on an independent audit
// flock. Matching both executable and attempt audit inode excludes unrelated
// utilities; that process must retain the lease and neither authority endpoint.
func observeManualRuntimeFD(t *testing.T, pinnedRuntime string, ownership *runnerExecutorOwnership, audit *os.File, authority []os.FileInfo) {
	t.Helper()
	// Runner copies the configured binary into its immutable digest-named store;
	// the original supplied path has a different inode from the actual process.
	exe, err := os.Stat(pinnedRuntime)
	if err != nil {
		t.Fatal(err)
	}
	trail, err := audit.Stat()
	if err != nil {
		t.Fatal(err)
	}
	lease, err := openPinnedExecutorLease(ownership)
	if err != nil {
		t.Fatal(err)
	}
	expectedLease, err := lease.Stat()
	lease.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(authority) != 2 {
		t.Fatal("both actual authority endpoint identities are required")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		processes, err := os.ReadDir("/proc")
		if err != nil {
			t.Fatal(err)
		}
		for _, process := range processes {
			if _, err := strconv.Atoi(process.Name()); err != nil {
				continue
			}
			base := filepath.Join("/proc", process.Name())
			actual, err := os.Stat(filepath.Join(base, "exe"))
			if err != nil || !os.SameFile(exe, actual) {
				continue
			}
			descriptors, err := os.ReadDir(filepath.Join(base, "fd"))
			if err != nil {
				continue
			}
			hasAudit, hasLease, hasAuthority := false, false, false
			for _, entry := range descriptors {
				info, err := os.Stat(filepath.Join(base, "fd", entry.Name()))
				if err != nil {
					continue
				}
				if os.SameFile(trail, info) {
					hasAudit = true
				}
				if os.SameFile(expectedLease, info) {
					hasLease = true
				}
				for _, endpoint := range authority {
					if os.SameFile(endpoint, info) {
						hasAuthority = true
					}
				}
			}
			if !hasAudit {
				continue
			}
			if !hasLease || hasAuthority {
				t.Fatalf("actual Runtime fd custody: lease=%t authority=%t", hasLease, hasAuthority)
			}
			t.Log("actual audit-blocked Runtime retains its external lease and neither Desk authority endpoint")
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("actual Runtime was not observed holding its exact audit file")
}
