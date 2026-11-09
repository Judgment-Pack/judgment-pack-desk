package desk

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Constructed events keep these tests independent of OS event ordering. Timer
// assertions use virtual time; only watch installation needs real directories.
func watcherForTest(root string, fsw *fsnotify.Watcher, emit func(string)) *watcher {
	// Timer callbacks may run concurrently; serialize writes to the test sink.
	var emitMu sync.Mutex
	return &watcher{
		root: root, fsw: fsw, log: log.New(io.Discard, "", 0),
		emit: func(rel string) {
			emitMu.Lock()
			defer emitMu.Unlock()
			emit(rel)
		}, closing: make(chan struct{}), pending: make(map[string]*time.Timer),
	}
}

func TestWatcherHandleIgnoresExcludedAndStagingNames(t *testing.T) {
	// Spell these out rather than deriving expectations from the exclusion map.
	for _, name := range []string{
		".desk-private", ".DESK-PRIVATE", ".git", ".GIT", "node_modules",
		"NODE_MODULES", "dist", "DIST", ".venv", ".VENV", "vendor", "VENDOR",
		".jpack-desk-0123456789abcdef.tmp", ".jpack-desk-",
	} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var got []string
				w := watcherForTest(filepath.FromSlash("/project"), nil, func(rel string) { got = append(got, rel) })
				for _, op := range []fsnotify.Op{fsnotify.Create, fsnotify.Write, fsnotify.Rename, fsnotify.Remove, fsnotify.Chmod} {
					w.handle(fsnotify.Event{Name: filepath.Join(w.root, name), Op: op})
				}
				if len(w.pending) != 0 {
					t.Errorf("ignored name scheduled %d invalidations", len(w.pending))
				}
				time.Sleep(2 * debounce)
				synctest.Wait()
				if len(got) != 0 {
					t.Errorf("ignored name emitted %v", got)
				}
			})
		})
	}
}

func TestWatcherHandleRelativePathsAndOrdinaryNames(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var got []string
		w := watcherForTest(filepath.FromSlash("/project"), nil, func(rel string) { got = append(got, rel) })
		want := []string{"packs/nested/example.json", "node_modules.json", "dist.json", "vendor.json", "draft.jpack-desk-note.json"}
		for _, rel := range want {
			w.handle(fsnotify.Event{Name: filepath.Join(w.root, filepath.FromSlash(rel)), Op: fsnotify.Write | fsnotify.Chmod})
		}
		time.Sleep(debounce)
		synctest.Wait()
		slices.Sort(got)
		slices.Sort(want)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("emitted paths = %v, want %v", got, want)
		}
		if len(w.pending) != 0 {
			t.Fatal("completed paths remain pending")
		}
	})
}

func TestWatcherScheduleCoalescesAndResetsPerPath(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var got []string
		w := watcherForTest("", nil, func(rel string) { got = append(got, rel) })
		w.schedule("a.json")
		w.schedule("b.json")
		time.Sleep(debounce / 2)
		w.schedule("a.json")
		w.schedule("a.json")
		if len(w.pending) != 2 {
			t.Fatalf("pending paths = %d, want 2", len(w.pending))
		}
		time.Sleep(debounce / 2)
		synctest.Wait()
		if !reflect.DeepEqual(got, []string{"b.json"}) {
			t.Fatalf("at original deadline emitted %v, want only b.json", got)
		}
		time.Sleep(debounce / 2)
		synctest.Wait()
		if !reflect.DeepEqual(got, []string{"b.json", "a.json"}) {
			t.Fatalf("at reset deadline emitted %v", got)
		}
		// A later save of an emitted path starts a fresh debounce window.
		w.schedule("a.json")
		time.Sleep(debounce - time.Nanosecond)
		synctest.Wait()
		if len(got) != 2 {
			t.Fatalf("later save emitted before its deadline: %v", got)
		}
		time.Sleep(time.Nanosecond)
		synctest.Wait()
		if !reflect.DeepEqual(got, []string{"b.json", "a.json", "a.json"}) || len(w.pending) != 0 {
			t.Fatalf("later save emitted %v; pending %d", got, len(w.pending))
		}
	})
}

func TestWatcherHandleCreateAddsDirectoryAndDescendantWatches(t *testing.T) {
	root := t.TempDir()
	created := filepath.Join(root, "new")
	child := filepath.Join(created, "nested", "child")
	for _, dir := range []string{child, filepath.Join(created, ".git", "hidden"), filepath.Join(created, "NODE_MODULES", "hidden")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(created, "document.json")
	if err := os.WriteFile(file, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fsw.Close() })
	synctest.Test(t, func(t *testing.T) {
		var got []string
		w := watcherForTest(root, fsw, func(rel string) { got = append(got, rel) })
		w.handle(fsnotify.Event{Name: created, Op: fsnotify.Create | fsnotify.Chmod})
		w.handle(fsnotify.Event{Name: file, Op: fsnotify.Create})
		w.handle(fsnotify.Event{Name: filepath.Join(root, "already-removed.json"), Op: fsnotify.Create})
		gotWatches := fsw.WatchList()
		wantWatches := []string{created, filepath.Join(created, "nested"), child}
		slices.Sort(gotWatches)
		slices.Sort(wantWatches)
		if !reflect.DeepEqual(gotWatches, wantWatches) {
			t.Errorf("Create installed watches %v, want %v", gotWatches, wantWatches)
		}
		time.Sleep(debounce)
		synctest.Wait()
		slices.Sort(got)
		want := []string{"already-removed.json", "new", "new/document.json"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Create invalidations = %v, want %v", got, want)
		}
	})
}

func TestWatcherCloseCancelsPendingAndSuppressesLaterEmissions(t *testing.T) {
	// Create the OS watcher outside the virtual-time bubble: its reader waits
	// on kernel I/O, whereas the timers under test must use virtual time.
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fsw.Close() })
	synctest.Test(t, func(t *testing.T) {
		var got []string
		w := watcherForTest("", fsw, func(rel string) { got = append(got, rel) })
		w.schedule("pending.json")
		time.Sleep(debounce / 2)
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		if len(w.pending) != 0 {
			t.Fatal("Close left pending timers")
		}
		// Exercise the callback's closing guard as well as timer cancellation.
		w.schedule("late.json")
		time.Sleep(2 * debounce)
		synctest.Wait()
		if len(got) != 0 || len(w.pending) != 0 {
			t.Fatalf("after Close emitted %v; pending %d", got, len(w.pending))
		}
	})
}

func TestWatcherNewDirectoryReportsChildChanges(t *testing.T) {
	root := t.TempDir()
	emitted := make(chan string, 16)
	w, err := newWatcher(root, log.New(io.Discard, "", 0), func(rel string) { emitted <- rel })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	})
	await := func(want string) {
		t.Helper()
		deadline := time.NewTimer(5 * time.Second)
		defer deadline.Stop()
		for {
			select {
			case got := <-emitted:
				if got == want {
					return
				}
			case <-deadline.C:
				t.Fatalf("timed out waiting for %q; watches %v", want, w.fsw.WatchList())
			}
		}
	}
	created := filepath.Join(root, "created")
	if err := os.Mkdir(created, 0700); err != nil {
		t.Fatal(err)
	}
	// The directory invalidation follows addTree, so receiving it synchronizes
	// the child write with registration instead of guessing at OS timing.
	await("created")
	if !slices.Contains(w.fsw.WatchList(), created) {
		t.Fatal("created directory was reported without installing its watch")
	}
	if err := os.WriteFile(filepath.Join(created, "child.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	await("created/child.json")
}
