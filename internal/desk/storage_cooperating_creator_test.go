//go:build unix

package desk

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// **A cooperating creator never waits for another.** While another creator
// holds the parent's creator lock, a folder it has already made is used as
// found, and none made yet is a worded, retryable busy refusal (409): nothing
// is made under the other creator's lock.
func TestACooperatingCreatorUsesAFinishedFolderAndOtherwiseRefusesInWords(t *testing.T) {
	path := t.TempDir()
	parent, err := os.OpenRoot(path)
	storageTestMust(t, err)
	defer parent.Close()
	other, err := parent.Open(".")
	storageTestMust(t, err)
	defer other.Close()
	storageTestMust(t, lockPrivateData(other, true))

	root, made, err := storageCooperatingDirectory(t.Context(), parent, "absent", nil)
	if root != nil || made || !errors.Is(err, storagebudget.ErrBusy) || codeOf(err) != CodeStale || statusForRefusal(err) != 409 {
		t.Fatalf("a folder not yet made answered %v %v %v (%s)", root, made, err, codeOf(err))
	}
	if _, err := os.Lstat(filepath.Join(path, "absent")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a folder was made under another creator's lock", err)
	}

	storageTestMust(t, os.Mkdir(filepath.Join(path, "finished"), 0o700))
	root, made, err = storageCooperatingDirectory(t.Context(), parent, "finished", nil)
	storageTestMust(t, err)
	defer root.Close()
	held, err := root.Stat(".")
	storageTestMust(t, err)
	named, err := os.Lstat(filepath.Join(path, "finished"))
	storageTestMust(t, err)
	if made || !os.SameFile(held, named) {
		t.Fatal("the finished folder was not the one opened, or was reported made", made)
	}

	storageTestMust(t, os.Symlink(filepath.Join(path, "finished"), filepath.Join(path, "link")))
	if root, _, err := storageCooperatingDirectory(t.Context(), parent, "link", nil); root != nil || !errors.Is(err, errStorageCustody) {
		t.Fatal("a link was used as the finished folder", err)
	}
}
