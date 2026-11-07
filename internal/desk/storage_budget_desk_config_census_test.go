package desk

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// **A desk.json at the mode an editor leaves is measured, not refused.**
// Custody reads a 0644 desk.json by design (readConfigNamed); the storage
// census refused the same file, so a Desk whose desk.json had ordinary
// permissions started read-only ("new work is paused"), and one whose
// desk.json was written after start could not enroll first-use history, which
// refused the Settings ping, chats and every other history-backed operation.
// A desk.json someone else could write, and any other non-private name, is
// still refused.
func TestStorageCensusMeasuresTheDeskConfigAtAnEditorsMode(t *testing.T) {
	seeded := func(t *testing.T, name string, mode os.FileMode) string {
		t.Helper()
		config := filepath.Join(t.TempDir(), "config")
		storageTestMust(t, os.Mkdir(config, 0o700))
		path := filepath.Join(config, name)
		storageTestMust(t, os.WriteFile(path, []byte(`{"deskConfigVersion":1}`), 0o600))
		storageTestMust(t, os.Chmod(path, mode))
		return config
	}
	t.Run("readable at start", func(t *testing.T) {
		s, _, _ := assistantServerIn(t, seeded(t, deskConfigName, 0o644))
		if s.storageReadOnly || s.storageEnrollment == nil {
			t.Fatalf("a 0644 desk.json started read-only: %v", s.storageProblem)
		}
		if err := s.ensureDefaultHistoryEnrollment(t.Context()); err != nil {
			t.Fatalf("first-use history beside a 0644 desk.json: %v", err)
		}
	})
	t.Run("readable after start", func(t *testing.T) {
		s, _, _ := assistantServer(t)
		writeDeskConfig(t, s, `{"deskConfigVersion":1}`)
		if info, err := os.Stat(s.deskConfigPath()); err != nil || info.Mode().Perm() != 0o644 {
			t.Fatalf("fixture desk.json is %v (%v)", info, err)
		}
		if err := s.ensureDefaultHistoryEnrollment(t.Context()); err != nil {
			t.Fatalf("first-use history after a 0644 desk.json was saved: %v", err)
		}
	})
	for _, refused := range []struct{ name, file string }{{"writable by group", deskConfigName}, {"another readable name", "notes.json"}} {
		t.Run(refused.name, func(t *testing.T) {
			mode := os.FileMode(0o644)
			if refused.file == deskConfigName {
				mode = 0o664
			}
			s, _, _ := assistantServerIn(t, seeded(t, refused.file, mode))
			if !s.storageReadOnly || !errors.Is(s.storageProblem, storagebudget.ErrUnknown) {
				t.Fatalf("%s at %#o was measured: read-only=%v problem=%v", refused.file, mode, s.storageReadOnly, s.storageProblem)
			}
		})
	}
}
