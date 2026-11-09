package desk

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// **A publication in a project with no private folder yet borrows the project
// lock it already holds.** A review's lock holds the project folder's
// exclusive lock; the publication ledger and the reviewed copies are the first
// folders made in the project, and each used to try that same folder's lock
// again, which the held lease refused ("storage coordinator busy"). Nothing was
// locked or published on a fresh project.
func TestPublicationFoldersBorrowTheHeldProjectLock(t *testing.T) {
	s, ts, rig, project := reviewProject(t, "", "")
	if _, err := os.Lstat(filepath.Join(project, ".desk-private")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the fixture already has a private folder: %v", err)
	}
	rig.answers(t, "valid")
	rig.locks(t, lockOf(t, project, map[string]string{"alpha": "packs/a.json", "beta": "packs/b.json"}))
	if status, body := confirm(t, ts, "", readReview(t, ts, "").Token); status != 200 {
		t.Fatalf("a fresh project's review lock answered %d %s", status, body)
	}
	for _, folder := range []string{publicationDirectory, reviewedCopiesDir} {
		if info, err := os.Stat(filepath.Join(project, folder)); err != nil || !info.IsDir() {
			t.Fatalf("%s was not made: %v", folder, err)
		}
	}
	if _, err := os.Stat(filepath.Join(project, publicationDirectory, "history.json")); err != nil {
		t.Fatalf("the publication was not recorded: %v", err)
	}
	// Publication runs only under the held lock; no caller may begin one bare.
	snap, err := s.readSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if p, err := s.beginPublication(t.Context(), snap, nil); !errors.Is(err, storagebudget.ErrAuthority) {
		if p != nil {
			p.close()
		}
		t.Fatalf("a publication began without the project lock: %v", err)
	}
}
