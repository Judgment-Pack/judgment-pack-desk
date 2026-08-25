package desk

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// debounce coalesces the burst of events a single save produces (editors
// typically write, rename, and chmod) into one notification per path.
const debounce = 150 * time.Millisecond

// skipDirs are never watched. They are large, they churn constantly, and
// nothing under them is a Judgment Pack document.
var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"dist":         true,
	".venv":        true,
	"vendor":       true,
}

// watcher reports changes under the project tree as project-relative paths.
type watcher struct {
	root    string
	fsw     *fsnotify.Watcher
	log     *log.Logger
	emit    func(relPath string)
	closing chan struct{}
	once    sync.Once

	mu      sync.Mutex
	pending map[string]*time.Timer
}

func newWatcher(root string, logger *log.Logger, emit func(string)) (*watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &watcher{
		root:    root,
		fsw:     fsw,
		log:     logger,
		emit:    emit,
		closing: make(chan struct{}),
		pending: make(map[string]*time.Timer),
	}
	// The root itself must end up watched. `filepath.WalkDir` does not follow a
	// symlink handed to it as the walk root, so a symlinked project directory
	// would install zero watches and report success — live reload silently off,
	// and the page never told. The caller passes the *resolved* path (see
	// Server.projectDir), and this refuses to pretend either way.
	watched, err := w.addTree(root)
	if err != nil {
		_ = fsw.Close()
		return nil, err
	}
	if watched == 0 {
		_ = fsw.Close()
		return nil, fmt.Errorf("no directory under %s could be watched", root)
	}
	go w.loop()
	return w, nil
}

// addTree watches dir and every descendant directory. fsnotify is not
// recursive, so a directory created later is added when its Create arrives.
// addTree watches dir and every descendant directory, and reports how many
// watches it installed.
//
// The count is the point: zero means the tree was never reached, which is a
// startup failure rather than a quiet degradation.
//
// **fsnotify is pathname-based and cannot be pinned to a descriptor.** This is
// the one part of the chassis that watches by name, so a directory replaced
// after startup is watched by whatever now bears that name. It is a
// notification channel and not an authority: nothing is read, written or
// decided here — the file API holds the descriptor, and the worst a misdirected
// watch can do is invalidate a query that then re-reads through the root. Stated
// in the README rather than implied.
func (w *watcher) addTree(dir string) (int, error) {
	watched := 0
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable subtree is not a reason to refuse to watch the rest.
			return nil //nolint:nilerr // deliberate: skip and continue
		}
		if !d.IsDir() {
			return nil
		}
		if p != dir && skipDirs[d.Name()] {
			return filepath.SkipDir
		}
		if err := w.fsw.Add(p); err != nil {
			w.log.Printf("desk: cannot watch %s: %v", p, err)
			return filepath.SkipDir
		}
		watched++
		return nil
	})
	return watched, err
}

func (w *watcher) loop() {
	for {
		select {
		case <-w.closing:
			return
		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			w.log.Printf("desk: watch error: %v", err)
		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			w.handle(ev)
		}
	}
}

func (w *watcher) handle(ev fsnotify.Event) {
	base := filepath.Base(ev.Name)
	if skipDirs[base] {
		return
	}
	// A staging file is this desk replacing a document, not a document
	// changing. Reporting one would fire an invalidation for a file nobody
	// edited — and would do it in the middle of the save that created it.
	if strings.HasPrefix(base, stagingPrefix) {
		return
	}
	if ev.Op&fsnotify.Create != 0 {
		// A directory created after startup needs its own watch, or nothing
		// inside it is ever seen: fsnotify watches a directory, not a tree.
		if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
			_, _ = w.addTree(ev.Name)
		}
	}
	rel, err := filepath.Rel(w.root, ev.Name)
	if err != nil {
		return
	}
	w.schedule(filepath.ToSlash(rel))
}

// schedule emits rel once the path has been quiet for the debounce interval.
func (w *watcher) schedule(rel string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if t, ok := w.pending[rel]; ok {
		t.Reset(debounce)
		return
	}
	w.pending[rel] = time.AfterFunc(debounce, func() {
		w.mu.Lock()
		delete(w.pending, rel)
		w.mu.Unlock()
		select {
		case <-w.closing:
		default:
			w.emit(rel)
		}
	})
}

func (w *watcher) Close() error {
	var err error
	w.once.Do(func() {
		close(w.closing)
		w.mu.Lock()
		for _, t := range w.pending {
			t.Stop()
		}
		w.pending = map[string]*time.Timer{}
		w.mu.Unlock()
		err = w.fsw.Close()
	})
	return err
}
