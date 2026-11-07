package desk

import (
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
)

func storageProducerConfig(t *testing.T) Config {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("JPACK_DESK_HOME", "")
	return Config{ProjectDir: t.TempDir(), DeskConfigDir: t.TempDir(), JpackBin: jpackBinary(), Token: testToken, Port: 8791, Logger: log.New(io.Discard, "", 0)}
}
func storageProducerStart(t *testing.T, cfg Config) *Server {
	t.Helper()
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// **A private folder the identity producer made keeps the next start
// writable.** A first dialect draft in a project with no .desk-private makes
// one through the producer, registered as a child of the project's scope. The
// start that resumed enrollment refused it twice over: no domain object for the
// child ("storage format or finite capacity exceeded"), then custody, because
// the catalog records the child under the project folder while discovery held
// it as a folder of its own. Both starts are now writable and drafts keep
// working; a registered folder that was replaced is still refused.
func TestStorageRestartKeepsAProducerMadePrivateFolderWritable(t *testing.T) {
	cfg := storageProducerConfig(t)
	s := storageProducerStart(t, cfg)
	if _, err := os.Lstat(filepath.Join(cfg.ProjectDir, startupIdentityDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the fixture already has a private folder: %v", err)
	}
	storageTestMust(t, s.ensureDialectDraftEnrollment(t.Context()))
	storageTestMust(t, s.Close())
	for i := 0; i < 2; i++ {
		next := storageProducerStart(t, cfg)
		if next.storageReadOnly || next.storageEnrollment == nil {
			next.Close()
			t.Fatalf("start %d after the producer made the private folder was read-only: %v", i+1, next.storageProblem)
		}
		if status := next.readStorageStatus(t.Context()); status.Registration != nil {
			next.Close()
			t.Fatalf("a registered private folder was reported as unregistered: %+v", status.Registration)
		}
		if err := next.ensureDialectDraftEnrollment(t.Context()); err != nil {
			next.Close()
			t.Fatalf("dialect drafts after start %d: %v", i+1, err)
		}
		storageTestMust(t, next.Close())
	}
	private := filepath.Join(cfg.ProjectDir, startupIdentityDir)
	storageTestMust(t, os.Rename(private, private+"-old"))
	storageTestMust(t, os.Mkdir(private, 0o700))
	replaced := storageProducerStart(t, cfg)
	defer replaced.Close()
	if !replaced.storageReadOnly || !errors.Is(replaced.storageProblem, errStorageCustody) {
		t.Fatalf("a replaced registered private folder was accepted: read-only=%v problem=%v", replaced.storageReadOnly, replaced.storageProblem)
	}
}
