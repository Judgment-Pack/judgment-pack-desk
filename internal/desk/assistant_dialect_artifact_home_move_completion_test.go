package desk

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type homeCompletionSnapshot struct {
	raw  []byte
	info os.FileInfo
}

func snapshotHomeCompletion(t *testing.T, journal string, tx *deskHomeTransaction) map[string]homeCompletionSnapshot {
	t.Helper()
	paths := []string{journal}
	for _, write := range tx.Writes {
		paths = append(paths, write.Path)
	}
	for _, pin := range tx.Retained.Pins {
		parts, name, _, ok := homeMoveRowKey(pin.Table, pin.ID, pin.Namespace)
		if !ok {
			t.Fatal("invalid immutable fixture pin")
		}
		paths = append(paths, filepath.Join(append([]string{tx.Destination, deskHomePlatform, secretsDirName, dialectArtifactOwnersFolder}, append(parts, name)...)...))
	}
	for _, project := range tx.Retained.Projects {
		if project.Owner != "" {
			paths = append(paths, filepath.Join(string(project.To), ".desk-private", dialectArtifactOwnerMarker))
		}
	}
	out := map[string]homeCompletionSnapshot{}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		assistantRunMust(t, err)
		info, err := os.Lstat(path)
		assistantRunMust(t, err)
		out[path] = homeCompletionSnapshot{raw, info}
	}
	return out
}

func assertHomeCompletionSnapshot(t *testing.T, snapshots map[string]homeCompletionSnapshot) {
	t.Helper()
	for path, before := range snapshots {
		raw, err := os.ReadFile(path)
		info, statErr := os.Lstat(path)
		if err != nil || statErr != nil || !bytes.Equal(raw, before.raw) || !os.SameFile(info, before.info) {
			t.Fatal("acknowledgement changed original bytes or inode", filepath.Base(path), err, statErr)
		}
	}
}

func TestDialectArtifactHomeMoveCompletedJournalAcknowledgement(t *testing.T) {
	for _, phase := range []string{"sync", "file-close", "directory-sync", "readback", "root-close"} {
		t.Run(phase, func(t *testing.T) {
			journal, tx, pointer := queuedDialectArtifactHomeMoveForAcknowledgement(t)
			failure, closeFailure := errors.New("final journal acknowledgement uncertain"), errors.New("journal descriptor Close uncertain")
			var snapshots map[string]homeCompletionSnapshot
			for attempt := 0; attempt < 3; attempt++ {
				reached, fileClosed, rootClosed := 0, 0, 0
				testHookDialectArtifactOwnerStep = func(at string) error {
					if at == "home-move-journal.file-close" {
						fileClosed++
						if phase == "sync" {
							return closeFailure
						}
					}
					if at == "home-move-journal.root-close" {
						rootClosed++
					}
					if at == "home-move-journal."+phase {
						reached++
						return failure
					}
					return nil
				}
				err := finishHomeTransaction(journal)
				if !errors.Is(err, failure) || reached != 1 || fileClosed != 1 || rootClosed != 1 || phase == "sync" && !errors.Is(err, closeFailure) {
					t.Fatal("landed Complete acknowledgement skipped or Close not joined once", attempt, reached, fileClosed, rootClosed, err)
				}
				done, err := readHomeTransaction(journal)
				if err != nil || done == nil || !done.Complete {
					t.Fatal("fault did not leave actual Complete bytes", err)
				}
				if attempt == 0 {
					snapshots = snapshotHomeCompletion(t, journal, tx)
					// Any move/ordinary-write replay after Complete is a failure.
					testHomeMigrationStep = func(at string) error { t.Error("completed journal replayed effects", at); return failure }
				} else {
					assertHomeCompletionSnapshot(t, snapshots)
				}
			}
			testHookDialectArtifactOwnerStep = nil
			// Ordinary concurrent startup already holds a shared home lease.
			lease, err := openHomeLease(filepath.Join(tx.Destination, deskHomePlatform), false)
			assistantRunMust(t, err)
			t.Cleanup(func() { _ = lease.Close() })
			assertDialectArtifactHomeMoveAcknowledged(t, journal, pointer)
			assertDialectArtifactHomeMoveAcknowledged(t, journal, pointer)
			assertHomeCompletionSnapshot(t, snapshots)
		})
	}
}

func TestDialectArtifactHomeMovePointerAcknowledgement(t *testing.T) {
	for _, phase := range []string{"sync", "file-close", "directory-sync", "readback", "root-close"} {
		t.Run(phase, func(t *testing.T) {
			journal, tx, pointer := queuedDialectArtifactHomeMoveForAcknowledgement(t)
			if _, err := os.Lstat(pointer); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("pointer present before execution", err)
			}
			failure := errors.New("pointer acknowledgement uncertain")
			var snapshots map[string]homeCompletionSnapshot
			for attempt := 0; attempt < 2; attempt++ {
				reached, closed := 0, 0
				testHookDialectArtifactOwnerStep = func(at string) error {
					if at == "home-move-pointer.file-close" {
						closed++
					}
					if at == "home-move-pointer."+phase {
						reached++
						return failure
					}
					return nil
				}
				if err := finishHomeTransaction(journal); !errors.Is(err, failure) || reached != 1 || closed != 1 {
					t.Fatal("landed pointer acknowledgement skipped or Close repeated", attempt, reached, closed, err)
				}
				pending, err := readHomeTransaction(journal)
				if err != nil || pending == nil || pending.Complete {
					t.Fatal("uncertain pointer completed journal", err)
				}
				if got := dialectArtifactOwnerRead(t, pointer); !bytes.Equal(got, tx.Writes[len(tx.Writes)-1].Data) {
					t.Fatal("landed pointer absent or changed")
				}
				if attempt == 0 {
					snapshots = snapshotHomeCompletion(t, journal, tx)
				} else {
					assertHomeCompletionSnapshot(t, snapshots)
				}
			}
			testHookDialectArtifactOwnerStep = nil
			assertDialectArtifactHomeMoveAcknowledged(t, journal, pointer)
			delete(snapshots, journal) // final publication is the sole remaining write
			assertHomeCompletionSnapshot(t, snapshots)
		})
	}
}

func TestDialectArtifactHomeMoveCompletedJournalReplacementRefused(t *testing.T) {
	for _, kind := range []string{"bytes", "inode", "parent", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			journal, tx, pointer := queuedDialectArtifactHomeMoveForAcknowledgement(t)
			assertDialectArtifactHomeMoveAcknowledged(t, journal, pointer)
			// Isolate the journal parent so replacing its edge cannot also move
			// the test's otherwise unrelated completed home and pointer.
			isolated := filepath.Join(t.TempDir(), deskHomeTransactionName)
			assistantRunMust(t, os.Rename(journal, isolated))
			journal = isolated
			before := dialectArtifactOwnerRead(t, journal)
			fired := false
			testHookDialectArtifactOwnerStep = func(at string) error {
				if at != "home-move-journal.sync" || fired {
					return nil
				}
				fired = true
				switch kind {
				case "bytes":
					assistantRunMust(t, os.WriteFile(journal, append(append([]byte{}, before...), '\n'), 0600))
				case "inode":
					assistantRunMust(t, os.Rename(journal, journal+".original"))
					assistantRunMust(t, os.WriteFile(journal, before, 0600))
				case "parent", "symlink":
					parent := filepath.Dir(journal)
					assistantRunMust(t, os.Rename(parent, parent+".original"))
					if kind == "symlink" {
						assistantRunMust(t, os.Symlink(parent+".original", parent))
					} else {
						assistantRunMust(t, os.Mkdir(parent, 0700))
						assistantRunMust(t, os.WriteFile(journal, before, 0600))
					}
				}
				return nil
			}
			if err := finishHomeTransaction(journal); err == nil || !fired {
				t.Fatal("completed acknowledgement accepted changed bytes/inode/current edge", fired, err)
			}
			testHookDialectArtifactOwnerStep = nil
			want := before
			if kind == "bytes" {
				want = append(append([]byte{}, before...), '\n')
			}
			if got := dialectArtifactOwnerRead(t, journal); !bytes.Equal(got, want) {
				t.Fatal("replacement was compensated or rewritten")
			}
			for _, row := range tx.Retained.Rows {
				if !bytes.Equal(dialectArtifactOwnerRead(t, tx.Writes[row.Write].Path), row.After) {
					t.Fatal("replacement replayed retained effects")
				}
			}
		})
	}
}

func TestDialectArtifactHomeMoveCompletedInjectedJournalAcknowledgement(t *testing.T) {
	f := newInjectedHomeMoveFixture(t, false)
	journal, tx, _ := f.queue(t, "")
	assistantRunMust(t, f.shutdown(t))
	failure := errors.New("final injected journal acknowledgement uncertain")
	for attempt := 0; attempt < 3; attempt++ {
		reached := false
		testHookDialectArtifactOwnerStep = func(at string) error {
			if strings.HasPrefix(at, "injected-provider") || strings.HasPrefix(at, "injected-constructor") {
				t.Error("completed recovery entered provider authority", at)
			}
			if at == "home-move-journal.directory-sync" {
				reached = true
				return failure
			}
			return nil
		}
		if err := finishHomeTransaction(journal); !errors.Is(err, failure) || !reached {
			t.Fatal("injected completion failure lost", attempt, reached, err)
		}
		for path, before := range f.immutable {
			if !bytes.Equal(dialectArtifactOwnerRead(t, tx.relocate(path)), before) {
				t.Fatal("completed retry changed provider or immutable data")
			}
		}
	}
	testHookDialectArtifactOwnerStep = nil
	f.reopenAndRead(t, journal, tx)
}

func TestDialectArtifactHomeMovePointerReplacementRefused(t *testing.T) {
	for _, kind := range []string{"bytes", "inode"} {
		t.Run(kind, func(t *testing.T) {
			journal, tx, pointer := queuedDialectArtifactHomeMoveForAcknowledgement(t)
			before := tx.Writes[len(tx.Writes)-1].Data
			fired := false
			testHookDialectArtifactOwnerStep = func(at string) error {
				if at != "home-move-pointer.sync" || fired {
					return nil
				}
				fired = true
				if kind == "inode" {
					assistantRunMust(t, os.Rename(pointer, pointer+".original"))
					assistantRunMust(t, os.WriteFile(pointer, before, 0600))
				} else {
					assistantRunMust(t, os.WriteFile(pointer, []byte(`{"third":true}`), 0600))
				}
				return nil
			}
			if err := finishHomeTransaction(journal); err == nil || !fired {
				t.Fatal("pointer acknowledgement accepted replacement", fired, err)
			}
			testHookDialectArtifactOwnerStep = nil
			pending, err := readHomeTransaction(journal)
			if err != nil || pending == nil || pending.Complete {
				t.Fatal("pointer replacement completed journal", err)
			}
			want := before
			if kind == "bytes" {
				want = []byte(`{"third":true}`)
				if err := finishHomeTransaction(journal); err == nil {
					t.Fatal("third pointer bytes accepted on restart")
				}
			}
			if got := dialectArtifactOwnerRead(t, pointer); !bytes.Equal(got, want) {
				t.Fatal("uncertain pointer compensated or overwritten")
			}
		})
	}
}

func TestDialectArtifactHomeMoveLegacyCompletedJournal(t *testing.T) {
	journal := filepath.Join(t.TempDir(), deskHomeTransactionName)
	const raw = `{"version":1,"destination":"/new","config":"/old","renames":[],"writes":[],"parent":"","parentIdentity":"","complete":true}`
	privateFixture(t, journal, raw)
	testHookDialectArtifactOwnerStep = func(at string) error {
		t.Error("legacy completed journal entered retained acknowledgement", at)
		return errors.New("unexpected retained acknowledgement")
	}
	t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
	assistantRunMust(t, finishHomeTransaction(journal))
	mustBytes(t, journal, raw)
}
