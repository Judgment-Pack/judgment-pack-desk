//go:build unix

package desk

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// **The census measures what custody reads, and refuses what it refuses.** In
// v0.14.0 the census counts content owned by the user running Desk that neither
// group nor others can write, wherever it is under a registered root: a
// desk.json an editor left at 0644, a named desk's project files at the umask's
// mode, a gateway's connections and hand-off folders, a readable file in any
// folder. Each was once refused by a private-only rule, which started storage
// read-only ("new work is paused"). Content someone else could write, a link
// where a file or folder is expected, and a special file are still refused.
func TestStorageCensusMeasuresOwnContentNoOneElseCanWrite(t *testing.T) {
	const desk = "c7000000000000000000000000000001"
	type entry struct {
		path string
		mode os.FileMode
		dir  bool
	}
	seeded := func(t *testing.T, entries ...entry) string {
		t.Helper()
		config := filepath.Join(t.TempDir(), "config")
		storageTestMust(t, os.Mkdir(config, 0o700))
		for _, e := range entries {
			path := filepath.Join(config, filepath.FromSlash(e.path))
			storageTestMust(t, os.MkdirAll(filepath.Dir(path), 0o700))
			if e.dir {
				storageTestMust(t, os.Mkdir(path, 0o700))
			} else {
				storageTestMust(t, os.WriteFile(path, []byte(`{"version":1}`), 0o600))
			}
			storageTestMust(t, os.Chmod(path, e.mode))
		}
		return config
	}
	for _, measured := range []struct {
		name    string
		entries []entry
	}{
		{"a desk.json an editor left", []entry{{deskConfigName, 0o644, false}}},
		{"another readable name", []entry{{"notes.json", 0o644, false}}},
		{"a named desk's lock file", []entry{{"desks/" + desk + "/jpack.lock.json", 0o644, false}}},
		{"a named desk's Project Files folder", []entry{{"desks/" + desk + "/packs", 0o755, true}, {"desks/" + desk + "/packs/one.pack.json", 0o644, false}}},
		{"a readable file in a desk's private data", []entry{{"desks/" + desk + "/.desk-private/data/chat.json", 0o644, false}}},
		{"a readable file in a folder that is no desk", []entry{{"desks/not-a-desk/jpack.lock.json", 0o644, false}}},
		{"the gateway's connections folder", []entry{{"gateway-connections", 0o755, true}, {"gateway-connections/drive.json", 0o644, false}}},
		{"the gateway's input hand-offs", []entry{{"input-handoffs", 0o755, true}, {"input-handoffs/one.json", 0o640, false}}},
	} {
		t.Run("measured: "+measured.name, func(t *testing.T) {
			s, _, _ := assistantServerIn(t, seeded(t, measured.entries...))
			if s.storageReadOnly || s.storageEnrollment == nil {
				t.Fatalf("%s started read-only: %v", measured.name, s.storageProblem)
			}
		})
	}
	t.Run("measured: a desk.json saved after start", func(t *testing.T) {
		s, _, _ := assistantServer(t)
		writeDeskConfig(t, s, `{"deskConfigVersion":1}`)
		if info, err := os.Stat(s.deskConfigPath()); err != nil || info.Mode().Perm() != 0o644 {
			t.Fatalf("fixture desk.json is %v (%v)", info, err)
		}
		if err := s.ensureDefaultHistoryEnrollment(t.Context()); err != nil {
			t.Fatalf("first-use history after a 0644 desk.json was saved: %v", err)
		}
	})
	for _, refused := range []struct {
		name  string
		seed  []entry
		after func(t *testing.T, config string)
	}{
		{name: "a file writable by group", seed: []entry{{deskConfigName, 0o664, false}}},
		{name: "a file writable by others", seed: []entry{{"notes.json", 0o646, false}}},
		{name: "a folder writable by group", seed: []entry{{"desks/" + desk + "/packs", 0o775, true}}},
		{name: "a folder writable by others", seed: []entry{{"gateway-connections", 0o757, true}}},
		{name: "a link where a file is expected", after: func(t *testing.T, config string) {
			target := filepath.Join(t.TempDir(), "elsewhere.json")
			storageTestMust(t, os.WriteFile(target, []byte("{}"), 0o600))
			storageTestMust(t, os.Symlink(target, filepath.Join(config, "notes.json")))
		}},
		{name: "a link where a folder is expected", after: func(t *testing.T, config string) {
			storageTestMust(t, os.Symlink(t.TempDir(), filepath.Join(config, "gateway-connections")))
		}},
		{name: "a special file", after: func(t *testing.T, config string) {
			storageTestMust(t, syscall.Mkfifo(filepath.Join(config, "input-handoffs.fifo"), 0o600))
		}},
	} {
		t.Run("refused: "+refused.name, func(t *testing.T) {
			config := seeded(t, refused.seed...)
			if refused.after != nil {
				refused.after(t, config)
			}
			s, _, _ := assistantServerIn(t, config)
			if !s.storageReadOnly || !errors.Is(s.storageProblem, storagebudget.ErrUnknown) {
				t.Fatalf("%s was measured: read-only=%v problem=%v", refused.name, s.storageReadOnly, s.storageProblem)
			}
		})
	}
}

// censusRuleInfo is an entry's real metadata with another owner, which a test
// cannot make on disk without privileges.
type censusRuleInfo struct {
	os.FileInfo
	stat *syscall.Stat_t
}

func (i censusRuleInfo) Sys() any { return i.stat }

// **Another user's file or folder is refused, whatever its mode.** The rule
// asks who owns an entry before its mode: a file another user owns, even
// owner-only, is not measured, so the census refuses the installation.
func TestStorageCensusRuleRefusesAnotherUsersEntry(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file.json")
	storageTestMust(t, os.WriteFile(file, []byte("{}"), 0o600))
	folder := filepath.Join(dir, "folder")
	storageTestMust(t, os.Mkdir(folder, 0o700))
	for _, path := range []string{file, folder} {
		info, err := os.Lstat(path)
		storageTestMust(t, err)
		measured := storageCensusMeasuredFile
		if info.IsDir() {
			measured = storageCensusMeasuredDirectory
		}
		if !measured(info) {
			t.Fatalf("%s owned by this user, mode %v, was refused", path, info.Mode())
		}
		stat := *info.Sys().(*syscall.Stat_t)
		stat.Uid = effectiveUser() + 1
		if measured(censusRuleInfo{info, &stat}) {
			t.Fatalf("%s owned by user %d was measured", path, stat.Uid)
		}
	}
	// Special mode bits stay refused, as they were under the private rule.
	if err := os.Chmod(file, 0o600|os.ModeSetuid); err == nil {
		if info, err := os.Lstat(file); err == nil && info.Mode()&os.ModeSetuid != 0 && storageCensusMeasuredFile(info) {
			t.Fatal("a setuid file was measured")
		}
	}
}
