package desk

import (
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
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

// **A private folder Desk made outside the producer keeps an enrollment start
// writable, and what needs registration says why in words.** Once first-use
// history has registered the project's scope, a start resumes enrollment,
// which inspects sources by its own rule. A .desk-private a Policy made later
// was held as a private root there, the startup identity demand then found no
// registered root for it, and storage started read-only ("transition
// required"). The start now leaves it to the project scope, lists it, and
// keeps working; dialect drafts and a move refuse in words that say what is
// wrong, since the producer never adopts a folder it did not make.
func TestStorageEnrollmentStartKeepsAnUnregisteredPrivateFolderWritable(t *testing.T) {
	cfg := storageProducerConfig(t)
	s := storageProducerStart(t, cfg)
	storageTestMust(t, s.ensureDefaultHistoryEnrollment(t.Context()))
	s.cfg.JpackBin = policyVersionRuntime(t)
	policyMutation(t, policyCall(s, policyTestSession(t, s), "POST", "/api/policies", policyEditRequest{OperationID: policyOp(t), Title: "Made after the scope", Narrative: "A Policy made after the project scope", ArtifactVersion: "1.0.0"}))
	storageTestMust(t, s.Close())
	next := storageProducerStart(t, cfg)
	defer next.Close()
	if next.storageReadOnly || next.storageEnrollment == nil {
		t.Fatalf("an enrollment start with an unregistered private folder was read-only: %v", next.storageProblem)
	}
	private := filepath.Join(cfg.ProjectDir, startupIdentityDir)
	if status := next.readStorageStatus(t.Context()); status.Registration == nil || !slices.Contains(status.Registration.Folders, private) {
		t.Fatalf("the unregistered private folder was not reported: %+v", status.Registration)
	}
	for name, err := range map[string]error{
		"a dialect draft": next.ensureDialectDraftEnrollment(t.Context()),
		"a move preview": func() error {
			_, err := next.moveSnapshotOriginal(t.Context(), filepath.Join(t.TempDir(), "moved"), nil)
			return err
		}(),
	} {
		if !errors.Is(err, errStorageOwnerTransitionRequired) || !strings.Contains(fmtError(err), "made before storage registered it") {
			t.Fatalf("%s in a project whose private folder is unregistered answered %v", name, err)
		}
	}
	// Desk keeps working: new work is admitted.
	next.cfg.JpackBin = policyVersionRuntime(t)
	policyMutation(t, policyCall(next, policyTestSession(t, next), "POST", "/api/policies", policyEditRequest{OperationID: policyOp(t), Title: "After the restart", Narrative: "A Policy made after the restart", ArtifactVersion: "1.0.0"}))
}

func fmtError(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// **A move gives a project with no private folder one through the producer.**
// A move carries the project's identity, which the producer keeps in the
// project's registered private folder. A project with none refused every move
// ("transition required"). The preview now makes it through the producer's own
// creation path before reading the identity its token covers, and the move
// completes.
func TestProjectMovePreviewMakesTheProducersPrivateFolder(t *testing.T) {
	cfg := storageProducerConfig(t)
	s, ts := startDesk(t, cfg)
	defer ts.Close()
	defer s.Close()
	private := filepath.Join(cfg.ProjectDir, startupIdentityDir)
	destination := filepath.Join(t.TempDir(), "moved")
	request := movePreview(t, ts, destination)
	if info, err := os.Stat(private); err != nil || !info.IsDir() {
		t.Fatalf("the preview made no private folder: %v", err)
	}
	if err := s.requireRegisteredPrivate(t.Context()); err != nil {
		t.Fatalf("the preview's private folder is not registered: %v", err)
	}
	if code, data := moveRequest(t, ts, "move", request); code != 200 {
		t.Fatalf("move %d %s", code, data)
	}
	if _, err := os.Stat(filepath.Join(destination, startupIdentityDir, startupIdentityName)); err != nil {
		t.Fatalf("the moved project has no identity: %v", err)
	}
}
