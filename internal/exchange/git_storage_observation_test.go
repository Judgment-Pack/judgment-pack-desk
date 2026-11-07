package exchange

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestGitOwnedScratchObservationBeforeFailureAndLocalExclusion(t *testing.T) {
	// The only executable is this test-owned refusing stub. No Git, remote or
	// component is installed/launched by the test fixture.
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	observed, released := 0, 0
	scratch := ""
	ctx := WithScratchObserver(context.Background(), func(root *os.Root) func() {
		observed++
		if root == nil {
			t.Fatal("created scratch unavailable")
		}
		scratch = root.Name()
		if info, err := root.Stat("."); err != nil || !info.IsDir() {
			t.Fatal("scratch description", err)
		}
		return func() {
			released++
			if _, err := os.Lstat(scratch); err != nil {
				t.Fatal("scratch cleaned before observer release", err)
			}
		}
	})
	tree, done, err := OpenRelease(ctx, "https://example.invalid/catalog.git", "v1.0.0")
	done()
	if err == nil || tree != nil || observed != 1 || released != 1 {
		t.Fatal("owned scratch failure lifecycle", observed, released, err)
	}
	if _, err := os.Lstat(scratch); !os.IsNotExist(err) {
		t.Fatal("scratch not cleaned after original release", err)
	}
	local := t.TempDir()
	_, done, err = OpenRelease(ctx, local, "v1.0.0")
	done()
	if err == nil || observed != 1 || released != 1 {
		t.Fatal("caller local catalog entered owned scratch observation", err)
	}
	if _, err := os.Stat(local); err != nil {
		t.Fatal("caller local catalog removed", err)
	}
}
