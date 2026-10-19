//go:build linux

package desk

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestJobsAuthorityUnrelatedForkHasNoSocket(t *testing.T) {
	if forbidden := os.Getenv("JPACK_TEST_AUTHORITY_INODES"); forbidden != "" {
		entries, err := os.ReadDir("/proc/self/fd")
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			info, err := os.Stat(filepath.Join("/proc/self/fd", entry.Name()))
			if err == nil && strings.Contains(forbidden, "|"+identityKey(info)+"|") {
				t.Fatal("unrelated child inherited authority socket")
			}
		}
		return
	}
	host, child, err := jobsAuthoritySocket()
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	defer child.Close()
	copy, err := host.(*net.UnixConn).File()
	if err != nil {
		t.Fatal(err)
	}
	hi, err := copy.Stat()
	copy.Close()
	if err != nil {
		t.Fatal(err)
	}
	ci, err := child.Stat()
	if err != nil {
		t.Fatal(err)
	}
	forbidden := "|" + identityKey(hi) + "||" + identityKey(ci) + "|"
	var wg sync.WaitGroup
	failures := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			other, leaf, err := jobsAuthoritySocket()
			if err != nil {
				failures <- err
				return
			}
			defer other.Close()
			defer leaf.Close()
			cmd := exec.Command(os.Args[0], "-test.run=^TestJobsAuthorityUnrelatedForkHasNoSocket$", "-test.count=1")
			cmd.Env = append(os.Environ(), "JPACK_TEST_AUTHORITY_INODES="+forbidden)
			out, err := cmd.CombinedOutput()
			if err != nil {
				failures <- fmt.Errorf("unrelated process: %w: %.1024s", err, out)
			}
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
}
