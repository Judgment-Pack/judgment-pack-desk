package desk

import (
	"errors"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// storageTreeUsage is a folder's logical usage the way the census counts it:
// the folder and every folder below it, and each regular file's length.
func storageTreeUsage(t *testing.T, root string) storagebudget.Vector {
	t.Helper()
	var v storagebudget.Vector
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			v.Directories++
		} else {
			v.Files++
			v.Bytes += uint64(info.Size())
		}
		return nil
	})
	storageTestMust(t, err)
	return v
}

// storagePrivateUsage is a project private folder's usage split the way the
// census attributes it: its retained-artifacts folder and everything in it
// as Retained, the rest as Control.
func storagePrivateUsage(t *testing.T, private string) (control, retained storagebudget.Vector) {
	t.Helper()
	all := storageTreeUsage(t, private)
	if _, err := os.Lstat(filepath.Join(private, "retained-artifacts")); err == nil {
		retained = storageTreeUsage(t, filepath.Join(private, "retained-artifacts"))
	}
	control = storagebudget.Vector{Bytes: all.Bytes - retained.Bytes, Files: all.Files - retained.Files, Directories: all.Directories - retained.Directories}
	return control, retained
}

// **A private folder Desk made after the storage catalog is measured, and
// the next start is writable with a warning.** A standalone project has no
// .desk-private when Desk first starts on it, so the catalog registers none.
// A publication, a Policy and its retained artifacts then make one outside the
// identity producer. Every later start split it out as a root the catalog did
// not list, refused custody, and began read-only ("new work is paused"). The
// start now leaves it to the census, which counts its bytes for the project's
// workspace and the installation, and storage status reports the folder.
func TestStorageRestartMeasuresAPrivateFolderMadeAfterTheCatalog(t *testing.T) {
	s, ts, rig, project := reviewProject(t, "", "")
	private := filepath.Join(project, startupIdentityDir)
	if _, err := os.Lstat(private); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the fixture already has a private folder: %v", err)
	}
	if s.storageReadOnly || s.storageEnrollment == nil {
		t.Fatalf("first start read-only: %v", s.storageProblem)
	}
	cfg := s.cfg
	rig.answers(t, "valid")
	rig.locks(t, lockOf(t, project, map[string]string{"alpha": "packs/a.json", "beta": "packs/b.json"}))
	if status, body := confirm(t, ts, "", readReview(t, ts, "").Token); status != 200 {
		t.Fatalf("the publication answered %d %s", status, body)
	}
	s.cfg.JpackBin = policyVersionRuntime(t)
	token := policyTestSession(t, s)
	policyMutation(t, policyCall(s, token, "POST", "/api/policies", policyEditRequest{OperationID: policyOp(t), Title: "Made after the catalog", Narrative: "A Policy made after the storage catalog", ArtifactVersion: "1.0.0"}))
	for _, folder := range []string{publicationDirectory, policyDirectory, filepath.Join(startupIdentityDir, "retained-artifacts")} {
		if info, err := os.Stat(filepath.Join(project, folder)); err != nil || !info.IsDir() {
			t.Fatalf("%s was not made: %v", folder, err)
		}
	}
	want := storageTreeUsage(t, private)
	wantControl, wantRetained := storagePrivateUsage(t, private)
	if wantRetained.Files == 0 {
		t.Fatal("the fixture retained no artifact")
	}
	ts.Close()
	storageTestMust(t, s.Close())

	cfg.Root = nil
	next, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { next.Close() })
	if next.storageReadOnly || next.storageEnrollment == nil {
		t.Fatalf("a private folder made after the catalog started read-only: %v", next.storageProblem)
	}
	v, err := next.storageConstruction.ref.cell.q.Snapshot(t.Context())
	storageTestMust(t, err)
	// Retained artifacts are Retained by path, the rest Control.
	if len(v.Workspaces) != 1 || !v.Workspaces[0].Known || v.Workspaces[0].Usage[storagebudget.Control] != wantControl || v.Workspaces[0].Usage[storagebudget.Retained] != wantRetained {
		t.Fatalf("the private folder's %+v (Control %+v, Retained %+v) was not counted for its workspace: %+v", want, wantControl, wantRetained, v.Workspaces)
	}
	if !storageVectorLE(wantControl, v.Installation[storagebudget.Control]) || !storageVectorLE(wantRetained, v.Installation[storagebudget.Retained]) {
		t.Fatalf("the private folder's %+v was not counted for the installation: %+v", want, v.Installation)
	}
	status := next.readStorageStatus(t.Context())
	if status.Registration == nil || status.Registration.Enforcement != "warning-only" || !slices.Contains(status.Registration.Folders, private) {
		t.Fatalf("storage status did not report the unregistered folder: %+v", status.Registration)
	}
	// Desk keeps working: new work is admitted, not paused.
	next.cfg.JpackBin = policyVersionRuntime(t)
	policyMutation(t, policyCall(next, policyTestSession(t, next), "POST", "/api/policies", policyEditRequest{OperationID: policyOp(t), Title: "After the restart", Narrative: "A Policy made after the restart", ArtifactVersion: "1.0.0"}))
}

// **What the census rule refuses is still refused in such a folder.** The
// unregistered private folder is held to the census rule like any measured
// tree: content group or others can write, a link where a file or folder is
// expected, or a special file in it still starts storage read-only. A readable
// file there is measured.
func TestStorageRestartRefusesWhatTheRuleRefusesInAnUnregisteredPrivateFolder(t *testing.T) {
	seeded := func(t *testing.T) (Config, string) {
		t.Helper()
		t.Setenv("HOME", t.TempDir())
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		t.Setenv("JPACK_DESK_HOME", "")
		cfg := Config{ProjectDir: t.TempDir(), DeskConfigDir: t.TempDir(), JpackBin: jpackBinary(), Token: testToken, Port: 8791, Logger: log.New(io.Discard, "", 0)}
		s, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		s.cfg.JpackBin = policyVersionRuntime(t)
		policyMutation(t, policyCall(s, policyTestSession(t, s), "POST", "/api/policies", policyEditRequest{OperationID: policyOp(t), Title: "Made after the catalog", Narrative: "A Policy made after the storage catalog", ArtifactVersion: "1.0.0"}))
		storageTestMust(t, s.Close())
		return cfg, filepath.Join(cfg.ProjectDir, startupIdentityDir)
	}
	restart := func(t *testing.T, cfg Config) *Server {
		t.Helper()
		next, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { next.Close() })
		return next
	}
	plant := func(t *testing.T, path string, mode os.FileMode) {
		t.Helper()
		storageTestMust(t, os.WriteFile(path, []byte(`{"planted":true}`), 0o600))
		storageTestMust(t, os.Chmod(path, mode))
	}
	t.Run("measured: a readable file", func(t *testing.T) {
		cfg, private := seeded(t)
		plant(t, filepath.Join(private, "policies", "notes.json"), 0o644)
		if next := restart(t, cfg); next.storageReadOnly {
			t.Fatalf("a readable file in the private folder started read-only: %v", next.storageProblem)
		}
	})
	for _, refused := range []struct {
		name  string
		plant func(t *testing.T, private string)
	}{
		{"a file writable by group", func(t *testing.T, private string) {
			plant(t, filepath.Join(private, "policies", "notes.json"), 0o664)
		}},
		{"a folder writable by others", func(t *testing.T, private string) {
			storageTestMust(t, os.Chmod(filepath.Join(private, "policies"), 0o707))
		}},
		{"the private folder writable by group", func(t *testing.T, private string) {
			storageTestMust(t, os.Chmod(private, 0o770))
		}},
		{"a link where a file is expected", func(t *testing.T, private string) {
			target := filepath.Join(t.TempDir(), "elsewhere.json")
			storageTestMust(t, os.WriteFile(target, []byte("{}"), 0o600))
			storageTestMust(t, os.Symlink(target, filepath.Join(private, "policies", "notes.json")))
		}},
		{"a link where a folder is expected", func(t *testing.T, private string) {
			storageTestMust(t, os.Symlink(t.TempDir(), filepath.Join(private, "elsewhere")))
		}},
	} {
		t.Run("refused: "+refused.name, func(t *testing.T) {
			cfg, private := seeded(t)
			refused.plant(t, private)
			next := restart(t, cfg)
			// The census refuses an entry in the folder; the identity check
			// refuses the folder itself first. Either way storage is read only.
			if !next.storageReadOnly || next.storageProblem == nil {
				t.Fatalf("%s was measured: read-only=%v problem=%v", refused.name, next.storageReadOnly, next.storageProblem)
			}
		})
	}
}

// **A registered private folder that was replaced is still refused.** Only a
// folder the catalog never registered is measured as unregistered. A start
// that finds the registered .desk-private gone and another folder at its name
// misses a registered root, and refuses custody as before.
func TestStorageRestartRefusesAReplacedRegisteredPrivateFolder(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("JPACK_DESK_HOME", "")
	cfg := Config{ProjectDir: t.TempDir(), DeskConfigDir: t.TempDir(), JpackBin: jpackBinary(), Token: testToken, Port: 8791, Logger: log.New(io.Discard, "", 0)}
	private := filepath.Join(cfg.ProjectDir, startupIdentityDir)
	storageTestMust(t, os.Mkdir(private, 0o700))
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if s.storageReadOnly {
		t.Fatalf("first start read-only: %v", s.storageProblem)
	}
	storageTestMust(t, s.Close())
	storageTestMust(t, os.Rename(private, private+"-old"))
	storageTestMust(t, os.Mkdir(private, 0o700))
	next, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { next.Close() })
	if !next.storageReadOnly || !errors.Is(next.storageProblem, errStorageCustody) {
		t.Fatalf("a replaced registered private folder was measured as unregistered: read-only=%v problem=%v", next.storageReadOnly, next.storageProblem)
	}
}
