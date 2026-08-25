package desk

// The file API: list, read and write files inside the served project root.
//
// This is the one part of the chassis that is not a verbatim pipe, and it
// exists because the runtime deliberately has no write tools. Runtime ADR-0006
// makes the runtime a stateless oracle; the authoring lifecycle therefore lives
// in the client, and the desk is the client. Writes go through here and never
// through the relay.
//
// **This API is the user's hand, not a policy layer.** Any path inside the
// project root is readable and writable, subject only to the exclusions below:
// they are the user's own files, on the user's own machine, reached over
// loopback with the session token. It does not consult jpack.json, it does not
// care whether a path is a pack, and it forms no opinion about what the bytes
// mean. The runtime remains the only judge of that, and it judges after the
// bytes land.
//
// # Containment
//
// Every operation goes through one *os.Root, opened once when the server is
// built and closed with it. That is the whole containment mechanism, and it is
// deliberately not a path-string check:
//
//   - A pathname validated and then opened is a check against one filesystem
//     and an open against another. Replacing an approved ancestor directory
//     with a symlink between the two redirects the open, and no amount of
//     `EvalSymlinks` before the fact prevents it. `os.Root` resolves each
//     component against a held directory descriptor, so the thing checked is
//     the thing opened.
//   - Pinning the root once is the other half. Re-resolving `ProjectDir` per
//     request lets the *authority itself* be retargeted — rename the directory,
//     or repoint the symlink it was reached through, and later requests adopt a
//     different tree without racing anything.
//
// A lexical check still runs first. It refuses a superset of what any
// filesystem does, costs nothing, and is tested on its own so that deleting it
// is visible — see `wireRelativePath`.
//
// # Concurrency
//
// Last write wins, but never silently *between clients of this API*: a write
// carries the digest of the bytes the editor loaded, the compare-and-commit is
// serialized per target path, and a disagreement is refused with both digests.
// A writer that does not come through this API — an editor, a git checkout —
// is not serialized with, and cannot be; the file watcher is what tells the
// page about those, and it is a notification rather than a guarantee.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"
)

// maxFileBytes bounds a single read or write. The desk edits documents, and a
// document that does not fit here is one this editor has no business loading
// into a browser tab in the first place.
const maxFileBytes = 4 << 20

// stagingPrefix names the files atomicWrite creates while replacing a target.
// They are excluded from the listing and from the watcher: a staged edit is
// not a document, and reporting one would both expose a half-written file to a
// second tab and fire a change notification for something nobody edited.
const stagingPrefix = ".jpack-desk-"

// errOutsideProject is the one containment failure, however it was reached.
// The reason is deliberately not itemized to the caller: dot-dot, an absolute
// path and a symlink escape are the same refusal, and describing which one
// would narrate the shape of the filesystem to whoever asked.
var errOutsideProject = errors.New("path is not inside the project")

// testHookAfterResolve runs between validating a path and acting on it, and is
// nil outside tests.
//
// It exists so a test can perform exactly the swap a time-of-check/time-of-use
// attack would — replace an approved directory with a symlink pointing out —
// at the one instant where it would matter. Without it the containment story
// would rest on reading the code and believing it.
var testHookAfterResolve func(rel string)

func afterResolve(rel string) {
	if testHookAfterResolve != nil {
		testHookAfterResolve(rel)
	}
}

// FileEntry is one file the project contains.
type FileEntry struct {
	// Path is project-relative and slash-separated, on every platform.
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// FileContent is one file's bytes and what they hash to. A write answers with
// this too, read back off the disk after the rename, so the client can verify
// what actually landed rather than trust that what it sent is what is there.
type FileContent struct {
	Path    string `json:"path"`
	Bytes   int    `json:"bytes"`
	SHA256  string `json:"sha256"`
	Content string `json:"content"`
	// Created is true when the write brought the file into existence.
	Created bool `json:"created,omitempty"`
}

// WriteRequest is the body of a write.
type WriteRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	// BaseSHA256 is the digest of the bytes the editor loaded, bare hex — the
	// payload-member convention the runtime uses, not the `sha256:` prefixed
	// form. Empty means the editor believes the file does not exist yet.
	BaseSHA256 string `json:"baseSha256"`
	// Override writes anyway when BaseSHA256 disagrees with the disk. It exists
	// so the user can make that choice deliberately, and it is never the
	// default: a client that always sent it would have no concurrency story at
	// all, only an unstated one.
	Override bool `json:"override"`
}

// conflict is the body of a refused stale write. Both digests are reported so
// the client can say what it had, what is there, and reload.
type conflict struct {
	Error          string `json:"error"`
	Path           string `json:"path"`
	ExpectedSHA256 string `json:"expectedSha256"`
	ActualSHA256   string `json:"actualSha256"`
	// Exists distinguishes "someone changed it" from "someone deleted it", and
	// from "you thought it was new and it is not".
	Exists bool `json:"exists"`
}

func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

/* Path validation --------------------------------------------------------- */

// wireRelativePath validates a client-supplied path and returns its cleaned
// slash form.
//
// This is the lexical half of containment: everything decidable from the string
// alone, before any filesystem is consulted. `os.Root` refuses a superset of
// what this refuses, so removing this would not open an escape today — it is
// kept, and tested on its own, because the two layers refuse for different
// reasons, this one costs no syscall, and a change to the other would otherwise
// silently become the only thing between a `..` and the disk.
//
// Backslash is refused outright rather than cleaned. On Windows it is a
// separator, so `..\secret` is a traversal that slash-only cleaning does not
// see; on Unix it is a legal filename character. Refusing it everywhere costs a
// filename nobody puts in a Judgment Pack project and removes the platform
// difference from the containment argument entirely.
func wireRelativePath(rel string) (string, error) {
	if rel == "" {
		return "", errors.New("path is required")
	}
	if strings.ContainsRune(rel, 0) {
		return "", errOutsideProject
	}
	if strings.ContainsRune(rel, '\\') {
		return "", errOutsideProject
	}
	if path.IsAbs(rel) || filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") || strings.Contains(rel, ":") {
		return "", errOutsideProject
	}
	clean := path.Clean(rel)
	// `.` is the project directory itself, which is a valid fs path and not a
	// document, so it is refused separately.
	if clean == "." {
		return "", errOutsideProject
	}
	// fs.ValidPath is the standard library's own answer to "is this a
	// well-formed slash path for an fs.FS": unrooted, no empty or dot elements,
	// and no dot-dot. It is the whole lexical dot-dot rule — an explicit `..`
	// check beside it would be dead code, and dead code in a containment
	// argument reads as a layer that is doing something.
	if !fs.ValidPath(clean) {
		return "", errOutsideProject
	}
	if strings.HasPrefix(path.Base(clean), stagingPrefix) {
		return "", fmt.Errorf("%s is a staging file this desk owns, not a document", clean)
	}
	return clean, nil
}

// osPath converts a validated wire path to the form Root's methods take.
func osPath(clean string) string { return filepath.FromSlash(clean) }

/* Per-target serialization ------------------------------------------------ */

// pathLocks serializes the compare-and-commit of a write per target path.
//
// The digest check and the rename are one decision — "replace these bytes, if
// these are still the bytes" — and a check that is not held across its commit
// is not a conditional commit at all: two writers sharing a base can both pass
// the check and both rename, and the second silently discards the first.
//
// In-process is the right scope, and the only honest one: the chassis is one
// process serving one project, and no lock here can serialize an editor or a
// git checkout. Those are the residual, and the file watcher is what surfaces
// them.
type pathLocks struct {
	mu    sync.Mutex
	locks map[string]*pathLock
}

type pathLock struct {
	mu   sync.Mutex
	refs int
}

func newPathLocks() *pathLocks { return &pathLocks{locks: map[string]*pathLock{}} }

// acquire blocks until the caller holds the lock for key, and returns the
// release. Entries are reference-counted so the map does not grow with every
// path a long session touches.
func (p *pathLocks) acquire(key string) func() {
	p.mu.Lock()
	entry := p.locks[key]
	if entry == nil {
		entry = &pathLock{}
		p.locks[key] = entry
	}
	entry.refs++
	p.mu.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		p.mu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(p.locks, key)
		}
		p.mu.Unlock()
	}
}

/* Reading ----------------------------------------------------------------- */

// readThroughRoot opens one file inside the root and returns its bytes.
//
// The open, the type check and the read all happen on one descriptor: `Stat`
// on a pathname and `ReadFile` on the same pathname are two resolutions, and
// what arrives between them is not this process's to assume. The regular-file
// requirement is what keeps a FIFO from blocking a handler forever and a device
// from being read at all, and it is checked on the handle rather than on the
// name.
func (s *Server) readThroughRoot(clean string) ([]byte, int, error) {
	// O_NONBLOCK so that a FIFO does not block the open itself: the type check
	// below can only decide if it gets to run.
	f, err := s.root.OpenFile(osPath(clean), os.O_RDONLY|openNonBlocking, 0)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, http.StatusNotFound, fmt.Errorf("no such file in the project: %s", clean)
		}
		// Root refuses an escape with its own error; every refusal reaching here
		// is reported as the one containment refusal.
		return nil, http.StatusForbidden, errOutsideProject
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	if info.IsDir() {
		return nil, http.StatusBadRequest, fmt.Errorf("%s is a directory", clean)
	}
	if !info.Mode().IsRegular() {
		return nil, http.StatusBadRequest,
			fmt.Errorf("%s is not a regular file, so this editor will not read it", clean)
	}
	// Bounded by the reader, not by the size the metadata claimed: a file that
	// grows between the stat and the read would otherwise be unbounded.
	data, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	if len(data) > maxFileBytes {
		return nil, http.StatusRequestEntityTooLarge,
			fmt.Errorf("%s is larger than %d bytes, which is the most this editor reads", clean, maxFileBytes)
	}
	return data, http.StatusOK, nil
}

// contentOf packages bytes for the wire, refusing what JSON cannot carry.
func contentOf(clean string, data []byte) (*FileContent, int, error) {
	// JSON strings are UTF-8, and Go's encoder substitutes U+FFFD for bytes
	// that are not. Handing back a silently mangled document would be worse
	// than handing back nothing, and far worse if the editor then saved it.
	if !utf8.Valid(data) {
		return nil, http.StatusUnsupportedMediaType,
			fmt.Errorf("%s is not UTF-8 text, so this editor will not load it", clean)
	}
	return &FileContent{
		Path:    clean,
		Bytes:   len(data),
		SHA256:  digestOf(data),
		Content: string(data),
	}, http.StatusOK, nil
}

/* Handlers ---------------------------------------------------------------- */

// handleFiles lists every regular file in the project.
//
// The walk goes through `Root.FS()`, so it cannot leave the pinned root even if
// the tree changes underneath it. Directories the watcher ignores are ignored
// here for the same reason: they are large, they churn, and nothing in them is
// a document anyone opened this desk to edit. Anything that is not a regular
// file — a symlink, a socket, a device — is left out rather than listed as
// something the read endpoint would then refuse.
func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	afterResolve(".")

	files := []FileEntry{}
	walkErr := fs.WalkDir(s.root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// One unreadable directory is not a reason to answer nothing.
			return nil //nolint:nilerr // reported as an absence, not a failure
		}
		if d.IsDir() {
			if p != "." && skipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || strings.HasPrefix(d.Name(), stagingPrefix) {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		entry := FileEntry{Path: p, Bytes: info.Size()}
		// The digest is what lets an editor open a file and later prove which
		// bytes it opened, so it is read here rather than left to a second call.
		// A file too large to read is listed with an empty digest rather than
		// omitted: it is really there, and saying so is more use than hiding it.
		if data, _, derr := s.readThroughRoot(p); derr == nil {
			entry.SHA256 = digestOf(data)
		}
		files = append(files, entry)
		return nil
	})
	if walkErr != nil {
		writeJSONError(w, http.StatusInternalServerError, walkErr.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"root":     s.cfg.ProjectDir,
		"files":    files,
		"excluded": excludedNames(),
		"note": "Every regular file in the project tree, except the excluded directories " +
			"and this desk's own staging files. This endpoint reads the filesystem and " +
			"nothing else: it does not consult jpack.json and forms no opinion about what " +
			"any file is. A file too large to read is listed with an empty digest.",
	})
}

func excludedNames() []string {
	names := make([]string, 0, len(skipDirs)+1)
	for name := range skipDirs {
		names = append(names, name)
	}
	names = append(names, stagingPrefix+"*")
	return names
}

// handleFileRead returns one file's bytes.
func (s *Server) handleFileRead(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	clean, err := wireRelativePath(r.URL.Query().Get("path"))
	if err != nil {
		writeJSONError(w, http.StatusForbidden, err.Error())
		return
	}
	afterResolve(clean)

	data, status, err := s.readThroughRoot(clean)
	if err != nil {
		writeJSONError(w, status, err.Error())
		return
	}
	content, status, err := contentOf(clean, data)
	if err != nil {
		writeJSONError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, content)
}

// handleFileWrite replaces one file's bytes and answers with what is on disk
// afterwards.
//
// Everything from the current-bytes read to the read-back happens under the
// per-path lock, which is what makes the digest check a conditional commit
// rather than a suggestion.
func (s *Server) handleFileWrite(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	var req WriteRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxFileBytes*2)).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("could not read the request: %v", err))
		return
	}
	if len(req.Content) > maxFileBytes {
		writeJSONError(w, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("this editor writes at most %d bytes", maxFileBytes))
		return
	}
	clean, err := wireRelativePath(req.Path)
	if err != nil {
		writeJSONError(w, http.StatusForbidden, err.Error())
		return
	}

	// Before the lock, deliberately. The hook is where a test performs the
	// symlink swap a time-of-check attack would — which must happen before any
	// filesystem access — and it is also where a test can hold two requests
	// until both have arrived. Inside the lock the second request would still
	// be waiting at the door, and no barrier could ever see it.
	afterResolve(clean)

	release := s.writes.acquire(clean)
	defer release()

	// What is there now, under the lock.
	//
	// Only "there is no such file" means this write creates one. Any other
	// refusal — a symlink pointing out of the project, a FIFO, a directory, a
	// file too large to read — is a refusal to *write* as well. Treating them
	// as absence is what would let a write replace an out-pointing symlink with
	// a regular file: nothing escapes, but the read of that same path is
	// refused, and an API that reads and writes a path by different rules is
	// one nobody can reason about.
	current, readStatus, readErr := s.readThroughRoot(clean)
	exists := readErr == nil
	if readErr != nil && readStatus != http.StatusNotFound {
		writeJSONError(w, readStatus, readErr.Error())
		return
	}
	actual := ""
	if exists {
		actual = digestOf(current)
	}

	// Last write wins, but never silently. The editor states which bytes it
	// loaded; a disagreement means the file changed underneath it, and the
	// client is told both digests rather than having its work quietly overwrite
	// someone else's.
	if !req.Override && !strings.EqualFold(strings.TrimSpace(req.BaseSHA256), actual) {
		writeJSON(w, http.StatusConflict, conflict{
			Error: "the file on disk is not the file this edit started from; " +
				"reload it, or write again with override",
			Path:           clean,
			ExpectedSHA256: strings.ToLower(strings.TrimSpace(req.BaseSHA256)),
			ActualSHA256:   actual,
			Exists:         exists,
		})
		return
	}

	// A directory that is not there is not a containment failure and must not
	// be reported as one. This API writes files; it does not create the tree
	// they live in, and saying so plainly is better than a mkdir nobody asked
	// for.
	if parent := path.Dir(clean); parent != "." {
		info, derr := s.root.Stat(osPath(parent))
		if derr != nil || !info.IsDir() {
			writeJSONError(w, http.StatusNotFound, fmt.Sprintf(
				"the directory %s does not exist in the project; create it first", parent))
			return
		}
	}

	if err := s.atomicWrite(clean, []byte(req.Content)); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Read back from the disk rather than echo the request. The point of the
	// answer is that the client can verify what landed, and an echo verifies
	// only that the request survived the trip out.
	data, status, err := s.readThroughRoot(clean)
	if err != nil {
		writeJSONError(w, status, err.Error())
		return
	}
	content, status, err := contentOf(clean, data)
	if err != nil {
		writeJSONError(w, status, err.Error())
		return
	}
	content.Created = !exists
	writeJSON(w, http.StatusOK, content)
}

/* Atomic replace ---------------------------------------------------------- */

// atomicWrite replaces a file's contents without a window in which the file is
// half written.
//
// The staging file is created in the **same directory** as the target, through
// the same pinned root, because rename is only atomic within a filesystem and
// only safe if both names resolve where they were checked. The order is
// deliberate: write, set the mode, flush the data, close, rename, then flush
// the directory. Setting the mode before the flush is what keeps the metadata
// from being the one thing not yet durable when the rename lands.
//
// **What is promised.** On Unix, `rename(2)` replaces the directory entry
// atomically, so a concurrent reader sees the old file or the new one and never
// a truncated one. On other platforms Go makes no such promise and neither does
// this.
//
// **What is not.** This is not a crash-durability guarantee. The data is
// flushed before the rename and the directory after it, which is the usual
// recipe, but power loss, a lying disk cache, or a filesystem with its own
// ordering rules can still lose the write. A crash between staging and rename
// leaves a staging file behind; it is excluded from the listing and from the
// watcher, and the server removes stale ones at startup.
func (s *Server) atomicWrite(clean string, data []byte) error {
	dir := path.Dir(clean)
	name, err := s.createStaging(dir)
	if err != nil {
		return err
	}
	staged := name
	if dir != "." {
		staged = path.Join(dir, name)
	}
	remove := func() { _ = s.root.Remove(osPath(staged)) }

	f, err := s.root.OpenFile(osPath(staged), os.O_WRONLY, 0o600)
	if err != nil {
		remove()
		return fmt.Errorf("could not stage the write: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		remove()
		return fmt.Errorf("could not stage the write: %w", err)
	}
	// Mode first, then the flush, so nothing about the file is still pending
	// when the rename publishes it. Only the POSIX rwx bits are carried across:
	// owner, group, ACLs, extended attributes and the inode identity are not,
	// because a replace is a new file by construction.
	if info, serr := s.root.Stat(osPath(clean)); serr == nil {
		if cerr := f.Chmod(info.Mode().Perm()); cerr != nil {
			f.Close()
			remove()
			return fmt.Errorf("could not set the mode of the staged write: %w", cerr)
		}
	}
	if err := f.Sync(); err != nil {
		f.Close()
		remove()
		return fmt.Errorf("could not flush the write: %w", err)
	}
	if err := f.Close(); err != nil {
		remove()
		return fmt.Errorf("could not close the staged write: %w", err)
	}
	if err := s.root.Rename(osPath(staged), osPath(clean)); err != nil {
		remove()
		return fmt.Errorf("could not replace the file: %w", err)
	}
	s.syncDir(dir)
	return nil
}

// createStaging makes an exclusive, randomly named staging file in dir and
// returns its base name. `os.Root` has no CreateTemp, so the exclusivity is
// asked for directly — O_EXCL is what makes a name collision a retry rather
// than a silent overwrite of somebody's staged edit.
func (s *Server) createStaging(dir string) (string, error) {
	for attempt := 0; attempt < 10; attempt++ {
		var raw [12]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return "", fmt.Errorf("could not stage the write: %w", err)
		}
		name := stagingPrefix + hex.EncodeToString(raw[:]) + ".tmp"
		full := name
		if dir != "." {
			full = path.Join(dir, name)
		}
		f, err := s.root.OpenFile(osPath(full), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			f.Close()
			return name, nil
		}
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		return "", fmt.Errorf("could not stage the write: %w", err)
	}
	return "", errors.New("could not stage the write: no unused staging name")
}

// syncDir flushes the directory entry a rename just changed.
//
// Best effort by design: on Unix this is what makes the *rename* durable rather
// than only the bytes, and on platforms where a directory cannot be opened for
// sync it fails harmlessly. A failure here does not undo a write that has
// already landed, so it is not reported as one.
func (s *Server) syncDir(dir string) {
	d, err := s.root.OpenFile(osPath(dir), os.O_RDONLY|openNonBlocking, 0)
	if err != nil {
		return
	}
	defer d.Close()
	_ = d.Sync()
}

// removeStaleStaging clears staging files left by a crash.
//
// Only at startup, and only files this desk names: a staging file that survived
// is not a document, nothing reads it, and leaving it to accumulate would mean
// a project slowly filling with the debris of interrupted saves.
func (s *Server) removeStaleStaging() {
	_ = fs.WalkDir(s.root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // a directory we cannot read holds nothing we may delete
		}
		if d.IsDir() {
			if p != "." && skipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() && strings.HasPrefix(d.Name(), stagingPrefix) && strings.HasSuffix(d.Name(), ".tmp") {
			if rerr := s.root.Remove(osPath(p)); rerr != nil {
				s.log.Printf("desk: could not remove the stale staging file %s: %v", p, rerr)
			}
		}
		return nil
	})
}

/* Plumbing ---------------------------------------------------------------- */

// guard applies the same two checks every other chassis endpoint applies, in
// the same order: the token first, then the origin. Sharing the function is
// what keeps a new endpoint from being a new place to forget one of them.
func (s *Server) guard(w http.ResponseWriter, r *http.Request) bool {
	if !s.authorized(r) {
		writeJSONError(w, http.StatusUnauthorized, "missing or invalid session token")
		return false
	}
	if !s.originAllowed(r) {
		writeJSONError(w, http.StatusForbidden,
			fmt.Sprintf("origin %q is not permitted", r.Header.Get("Origin")))
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
