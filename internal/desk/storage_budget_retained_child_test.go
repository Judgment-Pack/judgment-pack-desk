package desk

import (
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
)

// **A retained-artifacts folder made after the storage catalog is measured
// inside its private folder, not refused at the next start.** The catalog
// registers .desk-private/retained-artifacts as its own root only when it
// exists as the catalog is built. A first Policy, research proof or
// publication makes it later, inside the registered private folder, and the
// census measures it there. Every later start split it out again as a root the
// catalog did not list, and refused custody, so Desk began read-only ("new
// work is paused"). A registered child is still compared exactly: one that was
// replaced is refused.
func TestStorageRetainedChildMadeAfterTheCatalogStaysInItsPrivateRoot(t *testing.T) {
	start := func(t *testing.T, project, config string) *Server {
		t.Helper()
		s, err := New(Config{ProjectDir: project, DeskConfigDir: config, JpackBin: jpackBinary(), Token: testToken, Port: 8791, Logger: log.New(io.Discard, "", 0)})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	seeded := func(t *testing.T, retained bool) (string, string) {
		t.Helper()
		t.Setenv("HOME", t.TempDir())
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		t.Setenv("JPACK_DESK_HOME", "")
		project := t.TempDir()
		storageTestMust(t, os.Mkdir(filepath.Join(project, ".desk-private"), 0o700))
		if retained {
			storageTestMust(t, os.Mkdir(filepath.Join(project, ".desk-private", "retained-artifacts"), 0o700))
		}
		return project, t.TempDir()
	}
	restart := func(t *testing.T, s *Server, project, config string) *Server {
		t.Helper()
		storageTestMust(t, s.Close())
		next := start(t, project, config)
		t.Cleanup(func() { next.Close() })
		return next
	}

	t.Run("made by a first Policy", func(t *testing.T) {
		project, config := seeded(t, false)
		s := start(t, project, config)
		if s.storageReadOnly {
			t.Fatalf("first start read-only: %v", s.storageProblem)
		}
		token := policyTestSession(t, s)
		policyMutation(t, policyCall(s, token, "POST", "/api/policies", policyEditRequest{OperationID: policyOp(t), Title: "Retained child", Narrative: "Retained child narrative", ArtifactVersion: "1.0.0"}))
		if _, err := os.Lstat(filepath.Join(project, ".desk-private", "retained-artifacts")); err != nil {
			t.Fatalf("the Policy made no retained-artifacts folder: %v", err)
		}
		next := restart(t, s, project, config)
		if next.storageReadOnly || next.storageEnrollment == nil {
			t.Fatalf("a retained-artifacts folder made after the catalog started read-only: %v", next.storageProblem)
		}
	})
	t.Run("registered at the first start", func(t *testing.T) {
		project, config := seeded(t, true)
		s := start(t, project, config)
		next := restart(t, s, project, config)
		if next.storageReadOnly || next.storageEnrollment == nil {
			t.Fatalf("a registered retained-artifacts folder started read-only: %v", next.storageProblem)
		}
	})
	t.Run("registered and then replaced", func(t *testing.T) {
		project, config := seeded(t, true)
		s := start(t, project, config)
		storageTestMust(t, s.Close())
		retained := filepath.Join(project, ".desk-private", "retained-artifacts")
		storageTestMust(t, os.Rename(retained, retained+"-old"))
		storageTestMust(t, os.Mkdir(retained, 0o700))
		next := start(t, project, config)
		t.Cleanup(func() { next.Close() })
		if !next.storageReadOnly || !errors.Is(next.storageProblem, errStorageCustody) {
			t.Fatalf("a replaced registered retained-artifacts folder was accepted: read-only=%v problem=%v", next.storageReadOnly, next.storageProblem)
		}
	})
}
