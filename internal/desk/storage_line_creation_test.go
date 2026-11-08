package desk

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// creationServer is a bare Server on a fresh project folder, with the storage
// line off or on: enough for the private-folder openers, which read nothing
// else of it.
func creationServer(t *testing.T, off bool) *Server {
	t.Helper()
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return &Server{root: root, storageLineOff: off}
}

func storageLineName(off bool) string {
	if off {
		return "off"
	}
	return "on"
}

func requireFolder(t *testing.T, root *os.Root, name string) {
	t.Helper()
	if info, err := root.Lstat(name); err != nil || !info.IsDir() {
		t.Fatalf("%s was not made: %v", name, err)
	}
}

// Starting research and creating portable research make their run's folder in
// the research folder while holding its admission, the exclusive lock on that
// folder. Off, the folder is made as v0.13.2 made it; on, under the admission
// as its creator window. Neither is refused by the request's own admission.
func TestResearchAdmissionMakesItsRunFolders(t *testing.T) {
	for _, off := range []bool{true, false} {
		t.Run(storageLineName(off), func(t *testing.T) {
			s, _, run, _ := approvalFixture(t)
			useMemoryResearchFixture(t, s)
			s.storageLineOff = off
			root, err := s.inputResearchRoot(true)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			admission, admitted, err := s.lockResearchAdmission(root)
			if err != nil {
				t.Fatal(err)
			}
			if !off {
				// Only the admission's holder borrows it: a creator that holds
				// nothing is still refused, worded, while it is held.
				other, err := openInputResearch(root, "research_"+strings.Repeat("c", 32), true)
				if err == nil {
					other.Close()
				}
				if !errors.Is(err, storagebudget.ErrBusy) || codeOf(err) != CodeStale {
					t.Fatal("a creator that holds nothing made a folder under another's admission:", err)
				}
			}
			run.ID = "research_" + strings.Repeat("d", 32)
			run.State = "review"
			if err := s.writeResearchReview(t.Context(), root, run, nil, admitted); err != nil {
				t.Fatal("research start refused under its own admission:", err)
			}
			requireFolder(t, root, run.ID)
			admission.Close()

			portable := run
			portable.ID = "research_" + strings.Repeat("e", 32)
			if err := s.admitPortableResearch(t.Context(), root, portable); err != nil {
				t.Fatal("portable research creation refused under its own admission:", err)
			}
			requireFolder(t, root, portable.ID)
			probe, err := root.Open(".")
			if err != nil {
				t.Fatal(err)
			}
			defer probe.Close()
			if err := lockPrivateData(probe, true); err != nil {
				t.Fatal("the admission was kept after the creation:", err)
			}
		})
	}
}

// With the storage line off a private folder, the chat data location and the
// chat data marker are made as v0.13.2 made them: a lock another request
// holds on the folder they are made in (the project's, or another creator's
// window there) refuses none of them. On, the creator window still refuses
// in words, as before.
func TestStorageLineOffMakesPrivateDataAsV0132(t *testing.T) {
	for _, off := range []bool{true, false} {
		t.Run(storageLineName(off), func(t *testing.T) {
			refused := func(what string, err error) {
				t.Helper()
				if off && err != nil {
					t.Fatalf("%s refused with the line off: %v", what, err)
				}
				if !off && !errors.Is(err, storagebudget.ErrBusy) {
					t.Fatalf("%s escaped a held creator window with the line on: %v", what, err)
				}
			}
			s := creationServer(t, off)
			// Another request holds the project lock while a fresh project's
			// first .desk-private folder is made.
			unlock, err := s.lockProject(t.Context(), 0)
			if err != nil {
				t.Fatal(err)
			}
			drafts, err := s.openDialectDrafts(true)
			if err == nil {
				drafts.Close()
			}
			refused("first private folder under another request's project lock", err)
			if !off && codeOf(err) != CodeStale {
				t.Fatal("refusal is not the worded, retryable one", err)
			}
			unlock()

			// Another creator holds the window on .desk-private while a
			// folder is made in it.
			private, _, err := openOwnFolderBy(s.root, []string{startupIdentityDir}, true, "project state", s.creation(nil))
			if err != nil {
				t.Fatal(err)
			}
			defer private.Close()
			held, err := storageCreatorAcquire(private)
			if err != nil {
				t.Fatal(err)
			}
			preparations, err := s.openInputPreparations(true)
			if err == nil {
				preparations.Close()
			}
			refused("a folder made beside another creator's", err)
			storageTestMust(t, held.Close())
			if off {
				// The drafts workflow lock, made while another creator holds
				// the drafts folder's window (on, that window is tried for a
				// bound, then refused in words).
				drafts, err := s.openDialectDrafts(true)
				if err != nil {
					t.Fatal(err)
				}
				defer drafts.Close()
				held, err := storageCreatorAcquire(drafts)
				if err != nil {
					t.Fatal(err)
				}
				release, err := s.lockDialectDraftWorkflow(t.Context())
				if err != nil {
					t.Fatal("drafts workflow lock refused with the line off:", err)
				}
				release()
				storageTestMust(t, held.Close())
			}

			// The chat data location and marker, written while another
			// creator holds their folder's window.
			dir := t.TempDir()
			settings, err := os.OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer settings.Close()
			held, err = storageCreatorAcquire(settings)
			if err != nil {
				t.Fatal(err)
			}
			refused("chat data location", s.writeDataLocation(settings, []byte(`{"version":1,"path":"`+filepath.Join(dir, "data")+`"}`)))
			refused("chat data marker", s.claimDataMarker(settings, []byte(`{"version":1,"owner":"fixture"}`)))
			// A chat data folder made in a folder another creator holds.
			made, err := openPrivateDataRootBy(filepath.Join(dir, "history"), true, s.creation(nil))
			if err == nil {
				made.Close()
			}
			refused("chat data folder", err)
			storageTestMust(t, held.Close())
			if off {
				for _, name := range []string{dataLocationName, dataMarkerName} {
					if _, err := settings.Lstat(name); err != nil {
						t.Fatal(name, "not written", err)
					}
				}
			}
		})
	}
}

// With the storage line off, concurrent creators of the same folders never
// refuse each other: each uses the folder the first made, as in v0.13.2.
func TestStorageLineOffConcurrentCreators(t *testing.T) {
	s := creationServer(t, true)
	const creators = 16
	start := make(chan struct{})
	errs := make(chan error, creators)
	var wg sync.WaitGroup
	for range creators {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			root, err := s.openDialectDrafts(true)
			if err == nil {
				err = root.Close()
			}
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal("a concurrent creator was refused:", err)
		}
	}
	drafts, err := s.openDialectDrafts(false)
	if err != nil {
		t.Fatal(err)
	}
	defer drafts.Close()
	if data, err := readPrivateData(drafts, ".gitignore", 16); err != nil || string(data) != "*\n" {
		t.Fatalf("drafts folder .gitignore: %q %v", data, err)
	}
}

// The drafts folder gets its .gitignore wherever it has none, whoever made
// the folder: a creator that lost the race to write it does not leave it
// without one for good. One already there is kept as it is.
func TestDraftsIgnoreWrittenWhereMissing(t *testing.T) {
	for _, off := range []bool{true, false} {
		t.Run(storageLineName(off), func(t *testing.T) {
			s := creationServer(t, off)
			// The folder exists, made by a creator that never wrote its
			// .gitignore.
			bare, _, err := openOwnFolderBy(s.root, strings.Split(dialectDraftDir, "/"), true, "dialect drafts", folderCreation{legacy: true})
			if err != nil {
				t.Fatal(err)
			}
			defer bare.Close()
			if !off {
				// A window another request holds is not waited for and
				// refuses nothing; what is still missing is written later.
				held, err := storageCreatorAcquire(bare)
				if err != nil {
					t.Fatal(err)
				}
				drafts, err := s.openDialectDrafts(true)
				if err != nil {
					t.Fatal("drafts refused while another creator holds their window:", err)
				}
				drafts.Close()
				if _, err := bare.Lstat(".gitignore"); !errors.Is(err, os.ErrNotExist) {
					t.Fatal(".gitignore written outside the creator window", err)
				}
				storageTestMust(t, held.Close())
			}
			drafts, err := s.openDialectDrafts(true)
			if err != nil {
				t.Fatal(err)
			}
			drafts.Close()
			if data, err := readPrivateData(bare, ".gitignore", 16); err != nil || string(data) != "*\n" {
				t.Fatalf("missing .gitignore not written: %q %v", data, err)
			}
			storageTestMust(t, writePrivateData(bare, ".gitignore", []byte("*\n!kept\n")))
			drafts, err = s.openDialectDrafts(true)
			if err != nil {
				t.Fatal(err)
			}
			drafts.Close()
			if !off {
				// Inside the window too: one another creator wrote first stays.
				storageTestMust(t, storageCreateDraftIgnore(bare))
			}
			if data, err := readPrivateData(bare, ".gitignore", 16); err != nil || string(data) != "*\n!kept\n" {
				t.Fatalf("existing .gitignore replaced: %q %v", data, err)
			}
		})
	}
}
