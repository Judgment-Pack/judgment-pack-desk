//go:build unix

package desk

import (
	"os"
	"path/filepath"
	"testing"
)

// **A project's private folder that others can read but not write is read
// and measured, not refused** (desk #685's custody rule). A .desk-private
// made under a umask of 022 (0755) neither names the storage key wrongly
// nor starts storage read-only: the key is read from it, and a first start
// measures it as a folder storage does not register. Desk still keeps no
// identity there, and one it cannot read names nothing. A folder a group can
// write is still refused.
func TestAPrivateFolderOthersCanReadIsReadAndMeasured(t *testing.T) {
	requireStorageLine(t)
	for _, tc := range []struct {
		name     string
		mode     os.FileMode
		identity string
		readOnly bool
	}{
		{"others can read", 0o755, "", false},
		{"others can read, an identity Desk cannot read", 0o755, "not an identity\n", false},
		{"a group can write", 0o775, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			project := t.TempDir()
			private := filepath.Join(project, startupIdentityDir)
			storageTestMust(t, os.Mkdir(private, 0o700))
			if tc.identity != "" {
				storageTestMust(t, os.WriteFile(filepath.Join(private, startupIdentityName), []byte(tc.identity), 0o600))
			}
			storageTestMust(t, os.Chmod(private, tc.mode))
			root, err := os.OpenRoot(project)
			storageTestMust(t, err)
			defer root.Close()
			// The storage key alone, before any Desk starts on the project.
			key, err := (&Server{root: root, projectDir: project}).inspectStorageStartupKey()
			if tc.readOnly {
				if err == nil {
					t.Fatalf("a folder a group can write named the key %q", key)
				}
			} else if err != nil || key != digestOf([]byte(project)) {
				t.Fatalf("the key is %q (%v), want the project's path key", key, err)
			}
			s, _, _ := assistantServerAt(t, project, t.TempDir())
			if s.storageReadOnly != tc.readOnly {
				t.Fatalf("read-only %v, want %v: %v", s.storageReadOnly, tc.readOnly, s.storageProblem)
			}
			// With no identity file the project is named by its path; an
			// identity file there that Desk cannot read names nothing.
			if want := map[bool]string{true: "", false: key}[tc.identity != ""]; !tc.readOnly && s.signingKeyName() != want {
				t.Fatalf("the project is named %q, want %q", s.signingKeyName(), want)
			}
		})
	}
}
