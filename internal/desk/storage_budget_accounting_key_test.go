package desk

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
)

// **First-use history finds a project's workspace after its identity is
// written.** A project's workspace is registered under the key the project had
// then: the hash of its path, when it had no identity yet. The identity
// producer then writes an identity and records the registered key in its
// lineage. History enrollment looked the workspace up by the new identity,
// found none, and refused custody, so every chat, Policy recovery and other
// history-backed operation answered that storage was unavailable.
func TestStorageHistoryEnrollmentUsesTheRegisteredKeyAfterAnIdentityIsWritten(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("JPACK_DESK_HOME", "")
	project := t.TempDir()
	// A private folder without an identity: the start registers it under the
	// path's hash, then the identity producer writes one.
	storageTestMust(t, os.Mkdir(filepath.Join(project, startupIdentityDir), 0o700))
	s, err := New(Config{ProjectDir: project, DeskConfigDir: t.TempDir(), JpackBin: jpackBinary(), Token: testToken, Port: 8791, Logger: log.New(io.Discard, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if s.storageReadOnly || s.storageEnrollment == nil {
		t.Fatalf("first start read-only: %v", s.storageProblem)
	}
	private, err := s.openIdentityFolder()
	storageTestMust(t, err)
	record, _, found, err := readIdentity(private)
	storageTestMust(t, private.Close())
	if err != nil || !found || record.ID == digestOf([]byte(project)) {
		t.Fatalf("the start wrote no identity of its own: found=%v id=%q err=%v", found, record.ID, err)
	}
	if err := s.ensureDefaultHistoryEnrollment(t.Context()); err != nil {
		t.Fatalf("first-use history after the identity was written: %v", err)
	}
}
