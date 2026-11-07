package desk

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// **A named desk's project files at the umask's mode are measured, not
// refused.** In an installation with no Desk home a named desk is a project
// folder below the config folder's desks directory. The runtime writes its
// jpack.lock.json, and Project Files its files and folders, at the mode the
// person's umask gives (0644 and 0755 under 022). The storage census refused
// them as non-private, so every start after a named desk was made began
// read-only ("new work is paused"). A desk's .desk-private, anything someone
// else could write, and the same modes outside a named desk are still refused.
func TestStorageCensusMeasuresANamedDesksProjectFilesAtTheUmasksMode(t *testing.T) {
	const id = "c7000000000000000000000000000001"
	type seed struct {
		file      string
		fileMode  os.FileMode
		directory string
		dirMode   os.FileMode
	}
	seeded := func(t *testing.T, s seed) string {
		t.Helper()
		config := filepath.Join(t.TempDir(), "config")
		storageTestMust(t, os.MkdirAll(filepath.Join(config, "desks", id), 0o700))
		for _, dir := range []string{config, filepath.Join(config, "desks"), filepath.Join(config, "desks", id)} {
			storageTestMust(t, os.Chmod(dir, 0o700))
		}
		path := filepath.Join(config, filepath.FromSlash(s.file))
		storageTestMust(t, os.MkdirAll(filepath.Dir(path), 0o700))
		storageTestMust(t, os.WriteFile(path, []byte(`{"lockVersion":"1"}`), 0o600))
		storageTestMust(t, os.Chmod(path, s.fileMode))
		if s.directory != "" {
			storageTestMust(t, os.Chmod(filepath.Join(config, filepath.FromSlash(s.directory)), s.dirMode))
		}
		return config
	}
	for _, measured := range []struct {
		name string
		seed seed
	}{
		{"the runtime's lock file", seed{file: "desks/" + id + "/jpack.lock.json", fileMode: 0o644}},
		{"a Project Files folder and file", seed{file: "desks/" + id + "/packs/drafts/one.pack.json", fileMode: 0o644, directory: "desks/" + id + "/packs/drafts", dirMode: 0o755}},
	} {
		t.Run(measured.name, func(t *testing.T) {
			s, _, _ := assistantServerIn(t, seeded(t, measured.seed))
			if s.storageReadOnly || s.storageEnrollment == nil {
				t.Fatalf("a named desk's project content at the umask's mode started read-only: %v", s.storageProblem)
			}
		})
	}
	for _, refused := range []struct {
		name string
		seed seed
	}{
		{"a project file writable by group", seed{file: "desks/" + id + "/jpack.lock.json", fileMode: 0o664}},
		{"a project folder writable by group", seed{file: "desks/" + id + "/packs/drafts/one.pack.json", fileMode: 0o600, directory: "desks/" + id + "/packs/drafts", dirMode: 0o775}},
		{"a readable file in the desk's private data", seed{file: "desks/" + id + "/.desk-private/data/chat.json", fileMode: 0o644}},
		{"a readable file in a folder that is no desk", seed{file: "desks/not-a-desk/jpack.lock.json", fileMode: 0o644}},
	} {
		t.Run(refused.name, func(t *testing.T) {
			s, _, _ := assistantServerIn(t, seeded(t, refused.seed))
			if !s.storageReadOnly || !errors.Is(s.storageProblem, storagebudget.ErrUnknown) {
				t.Fatalf("%s was measured: read-only=%v problem=%v", refused.name, s.storageReadOnly, s.storageProblem)
			}
		})
	}
}
