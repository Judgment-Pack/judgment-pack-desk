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
// A lexical check still runs first. The two layers overlap without either
// containing the other: `os.Root` refuses escapes the string check never sees,
// and the string check refuses spellings — a colon, a backslash — that Unix
// `os.Root` would treat as an ordinary filename. It costs no syscall and is
// tested on its own, so deleting it is visible — see `wireRelativePath`.
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
	"sort"
	"strings"
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

// Bounds on one traversal of the project. A tree can be adversarial without
// anybody being hostile — a build directory nobody excluded, a bind mount, a
// directory hard link — and a walk with no budget answers such a tree by never
// answering at all. Exhausting any of these is reported through the same
// `partial` channel as an unreadable subtree, because it is the same fact: what
// came back is not all of it.
const (
	maxWalkDepth   = 64
	maxWalkEntries = 100_000
	readdirBatch   = 512
)

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

// testHookAfterLockEntry runs immediately after a write takes the serializing
// mutex, and is nil outside tests.
//
// It is what proves the lock is a lock: a barrier placed before it can hold the
// first request inside the critical section and show that the second cannot
// enter. A barrier placed only before the lock proves nothing — the scheduler
// is free to produce the expected answer by luck.
var testHookAfterLockEntry func(rel string)

func afterLockEntry(rel string) {
	if testHookAfterLockEntry != nil {
		testHookAfterLockEntry(rel)
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
	// CreateParents asks for the missing directories of Path to be created
	// inside the project before the file is written. Opt-in per request and
	// never a default: with it absent this API creates no directory at all,
	// which is what every existing caller and every existing test still gets.
	//
	// Containment is the same held descriptor the rest of this file uses, and
	// the create is reached only after every check a write already passes:
	//
	//   - Root.MkdirAll resolves each component against the *os.Root opened
	//     once when the server was built. It takes no pathname and re-derives
	//     nothing, so the thing checked is the thing created, and a component
	//     that would resolve outside the root errors rather than creating.
	//   - wireRelativePath has already refused an absolute path, a `..`, a NUL,
	//     a backslash, a colon, `.` itself, a staging name, and any component
	//     named by an excluded directory. A directory this API would refuse to
	//     write a file into is therefore one it will not create either.
	//   - refuseSymlinkedPath has already walked the existing components
	//     through the root and refused the whole path if any of them is a
	//     symlink — whatever it points at, including a target inside the root,
	//     because this desk's rule is stricter than os.Root's. That walk stops
	//     at the first component that does not exist, which is exactly where
	//     MkdirAll begins: a symlinked parent is refused before one directory
	//     is created, and there is no window in which a create follows a link.
	//   - It sits after the stale-digest check and inside the per-path write
	//     lock, so a write refused as stale creates nothing and two concurrent
	//     creates of one path are serialized exactly as two saves are.
	//
	// **There is no unwind.** If the directories are created and the write then
	// fails, an empty directory is left behind. Removing it would need a delete
	// verb this API does not have, applied to a directory another process may
	// have populated in the interval; an empty directory is inert and the next
	// attempt uses it.
	//
	// The mode is 0o777 masked by the process umask — the ordinary convention
	// for a directory in the user's own project, which is committed to their
	// repository and read by their other tools. The *file* written into it is
	// created the same way, 0o666 masked by the same umask (see createStaging),
	// so a directory and the document inside it do not disagree about who may
	// read them. Neither is this desk deciding access control: the umask the
	// desk was started under is the user's own answer to that question.
	//
	// **Bounded by what the listing can report.** A create is refused past
	// maxWalkDepth, because GET /api/files stops there and reports the tree as
	// partial: a write that succeeded into a directory the listing cannot reach
	// would leave the API describing a project it had itself made undescribable.
	CreateParents bool `json:"createParents"`
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
	// The excluded directories are excluded from the *endpoints*, not only from
	// the listing. Documenting an exclusion that a direct GET or PUT walks
	// straight through would be documenting something else — and the startup
	// cleanup skips these trees, so debris written into one would never be
	// collected either.
	for _, part := range strings.Split(clean, "/") {
		if isExcludedName(part) {
			return "", fmt.Errorf("%s is under %s, which this desk does not edit", clean, part)
		}
	}
	return clean, nil
}

// refuseSymlinkedPath refuses a path any component of which is a symlink.
//
// GET follows a terminal in-root symlink — `Root.OpenFile` resolves it — while
// a save renames over the *link* rather than its target. So without this the
// two endpoints address two different objects under one name: the editor shows
// you the target's bytes and the save replaces the link. The README says
// symlinks are not documents here; this is what makes that true of both verbs.
//
// Each component is checked through the pinned root, deepest last, and a
// component that does not exist ends the walk: there is nothing to follow, and
// a write is allowed to create it.
func (s *Server) refuseSymlinkedPath(clean string) error {
	parts := strings.Split(clean, "/")
	for i := range parts {
		info, err := s.root.Lstat(osPath(strings.Join(parts[:i+1], "/")))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return errOutsideProject
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s passes through a symbolic link, which this desk does not edit", clean)
		}
	}
	return nil
}

// osPath converts a validated wire path to the form Root's methods take.
func osPath(clean string) string { return filepath.FromSlash(clean) }

// isExcludedName reports whether one whole path component names a directory
// this desk does not edit.
//
// Whole components, so `node_modules.json` is an ordinary document and only
// `node_modules` itself is excluded. Case-insensitively, because a
// case-insensitive volume reaches `.git` through `.GIT`, and an exclusion a
// spelling walks around is not an exclusion. One function, so the endpoints, the
// listing and the watcher cannot drift into three answers.
func isExcludedName(name string) bool {
	for excluded := range skipDirs {
		if strings.EqualFold(name, excluded) {
			return true
		}
	}
	return false
}

/* Serialization ----------------------------------------------------------- */
//
// One server-wide mutex guards every write, from the current-bytes read through
// the rename to the read-back. That is coarser than one lock per file and deliberately so:
// a per-path lock is keyed by a *spelling*, and `A.json` and `a.json` name one
// file on a case-insensitive filesystem while taking two different locks — both
// writers would then compare against the same base and both commit. A key that
// is not filesystem identity is not a lock. One editor's saves do not contend.

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
		// A file inside the project that this process may not open is not a
		// containment failure, and saying so would send someone hunting a
		// security problem that is not there — the same mistake a missing
		// parent directory used to produce on the write path.
		if errors.Is(err, fs.ErrPermission) {
			return nil, http.StatusForbidden, fmt.Errorf("%s cannot be read: %w", clean, fs.ErrPermission)
		}
		// Everything else is the one containment refusal, deliberately not
		// itemized: which rule stopped it is not the caller's business.
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

// walkProject visits every regular file in the project, descriptor by
// descriptor, and reports what it could not look at.
//
// It does not use `Root.FS()`. On the Go version this module declares, the
// DirEntry that yields resolves `Info` by pathname, and on a filesystem that
// does not report entry types in the directory block it *lstats by pathname*
// to classify at all — both of which walk back out through whatever the root's
// path currently names. Opening each directory through the root and classifying
// each child with `Root.Lstat` keeps every step on the held descriptor —
// `Lstat` rather than `Stat`, because a symlink must be seen as one rather than
// followed to whatever it points at.
//
// A symlink is never followed and never visited: it is not a document here, and
// following one is the escape this file exists to prevent. Anything unreadable
// is collected rather than skipped in silence, because a listing that quietly
// thins out is indistinguishable from a project that is smaller than it is.
func (s *Server) walkProject(visit func(rel string, info fs.FileInfo)) []string {
	problems := []string{}
	budget := maxWalkEntries
	// The identity of every directory currently open above this one. A tree
	// reached through bind mounts or directory hard links can contain itself
	// without a single symlink, and a depth cap alone does not save you: two
	// aliases to an ancestor branch and the work doubles per level until the cap
	// is reached. Comparing identities stops it at the first repeat.
	var ancestors []fs.FileInfo

	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if depth > maxWalkDepth {
			problems = append(problems, fmt.Sprintf("%s: nested deeper than %d directories", dir, maxWalkDepth))
			return
		}
		d, err := s.root.Open(osPath(dir))
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", dir, err))
			return
		}
		info, serr := d.Stat()
		if serr != nil {
			d.Close()
			problems = append(problems, fmt.Sprintf("%s: %v", dir, serr))
			return
		}
		if sameAsAnyAncestor(ancestors, info) {
			d.Close()
			problems = append(problems, fmt.Sprintf(
				"%s: is the same directory as one of its own ancestors, so the walk stops here", dir))
			return
		}

		// Names come in bounded batches and the descriptor is closed before
		// recursing: a directory with a million entries is not read into memory
		// in one go, and the walk holds one descriptor per level rather than one
		// per directory.
		names := []string{}
		for {
			batch, rerr := d.Readdirnames(readdirBatch)
			names = append(names, batch...)
			if rerr == io.EOF {
				break
			}
			if rerr != nil {
				problems = append(problems, fmt.Sprintf("%s: %v", dir, rerr))
				break
			}
			if len(names) >= budget {
				break
			}
		}
		d.Close()
		sort.Strings(names)

		ancestors = append(ancestors, info)
		defer func() { ancestors = ancestors[:len(ancestors)-1] }()

		for _, name := range names {
			if budget <= 0 {
				problems = append(problems, fmt.Sprintf(
					"the walk stopped after %d entries, which is all this listing reads", maxWalkEntries))
				return
			}
			budget--

			child := name
			if dir != "." {
				child = path.Join(dir, name)
			}
			childInfo, lerr := s.root.Lstat(osPath(child))
			if lerr != nil {
				problems = append(problems, fmt.Sprintf("%s: %v", child, lerr))
				continue
			}
			switch {
			case childInfo.Mode()&fs.ModeSymlink != 0:
				// Not a document, and not followed.
			case childInfo.IsDir():
				if isExcludedName(name) {
					continue
				}
				walk(child, depth+1)
			case childInfo.Mode().IsRegular():
				// A regular file bearing an excluded directory's name is omitted
				// too: GET and PUT refuse that path, and listing something the
				// endpoints will not open is an offer the API does not honour.
				if isExcludedName(name) {
					continue
				}
				visit(child, childInfo)
			}
		}
	}
	walk(".", 0)
	// One exhaustion message, however many directories reached it.
	return dedupe(problems)
}

// sameAsAnyAncestor reports whether info is one of the directories already open
// above it.
//
// Identity, not pathname: a tree can contain itself through a bind mount or a
// directory hard link with no symlink anywhere in it, and those reach the same
// directory under a name that looks new. `os.SameFile` is what sees through
// that.
//
// A named predicate rather than a loop inline, because the end-to-end case needs
// privileges this project's tests do not assume — so the *logic* is pinned here
// by a unit test that does not.
func sameAsAnyAncestor(ancestors []fs.FileInfo, info fs.FileInfo) bool {
	for _, ancestor := range ancestors {
		if os.SameFile(ancestor, info) {
			return true
		}
	}
	return false
}

// dedupe keeps the first occurrence of each problem, in order.
func dedupe(problems []string) []string {
	seen := map[string]bool{}
	out := problems[:0]
	for _, problem := range problems {
		if seen[problem] {
			continue
		}
		seen[problem] = true
		out = append(out, problem)
	}
	return out
}

// handleFiles lists every regular file in the project.
//
// The walk holds a descriptor at every level, so it cannot leave the pinned
// root even if the tree changes underneath it. Directories the watcher ignores
// are ignored
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
	unread := []string{}
	problems := s.walkProject(func(rel string, info fs.FileInfo) {
		if strings.HasPrefix(path.Base(rel), stagingPrefix) {
			return
		}
		entry := FileEntry{Path: rel, Bytes: info.Size()}
		// The digest is what lets an editor open a file and later prove which
		// bytes it opened, so it is read here rather than left to a second call.
		data, status, derr := s.readThroughRoot(rel)
		switch {
		case derr == nil:
			entry.SHA256 = digestOf(data)
		case status == http.StatusRequestEntityTooLarge:
			// Listed with an empty digest, deliberately: the file is really
			// there and too big to hash, and that is a documented shape.
		default:
			// Everything else is a file this listing could not read, which is
			// the same fact as an unreadable subtree and belongs in the same
			// channel. Folding it into the oversized shape would say "too
			// large" about a permission error.
			unread = append(unread, fmt.Sprintf("%s: %v", rel, derr))
		}
		files = append(files, entry)
	})
	problems = append(problems, unread...)

	body := map[string]any{
		"root":     s.projectDir,
		"files":    files,
		"excluded": excludedNames(),
		"note": "Every regular file in the project tree, except the excluded directories " +
			"and this desk's own staging files. This endpoint reads the filesystem and " +
			"nothing else: it does not consult jpack.json and forms no opinion about what " +
			"any file is. A file too large to read is listed with an empty digest.",
	}
	// A thinned answer says it is thinned. A listing that dropped a subtree and
	// still returned a bare 200 would be read as the project's contents, and a
	// caller cannot tell a small project from a partial answer.
	if len(problems) > 0 {
		for _, problem := range problems {
			s.log.Printf("desk: listing could not read %s", problem)
		}
		body["partial"] = problems
		body["note"] = body["note"].(string) +
			" This listing is PARTIAL: `partial` names what could not be read, and the files " +
			"below are therefore not all of them."
	}
	writeJSON(w, http.StatusOK, body)
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

	if err := s.refuseSymlinkedPath(clean); err != nil {
		writeJSONError(w, http.StatusForbidden, err.Error())
		return
	}
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

	// The lock covers the filesystem transaction and nothing else. Encoding a
	// response is client-speed work — a caller that stops reading its socket
	// holds the write mid-flight for as long as it likes — and holding the
	// write mutex across that would let one stalled client stop every later
	// save on the machine.
	// Before the lock, deliberately. The hook is where a test performs the
	// symlink swap a time-of-check attack would — which must happen before any
	// filesystem access — and it is also where a test can hold two requests
	// until both have arrived. Inside the lock the second request would still
	// be waiting at the door, and no barrier could ever see it.
	afterResolve(clean)

	s.writes.Lock()
	status, body := s.commitWriteLocked(clean, req)
	s.writes.Unlock()
	writeJSON(w, status, body)
}

// commitWrite is the whole locked transaction: read what is there, compare it
// to the base the editor stated, replace it, and read back what landed.
//
// It returns what the caller should send and touches no ResponseWriter, which
// is what keeps client-speed I/O out of the critical section.
// The caller holds s.writes, and releases it before encoding anything. The lock
// is taken at the call site rather than here so that its extent is one readable
// span: the filesystem transaction, and nothing that talks to a client.
func (s *Server) commitWriteLocked(clean string, req WriteRequest) (int, any) {
	afterLockEntry(clean)

	if err := s.refuseSymlinkedPath(clean); err != nil {
		return http.StatusForbidden, errorBody(err)
	}

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
		return readStatus, errorBody(readErr)
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
		return http.StatusConflict, conflict{
			Error: "the file on disk is not the file this edit started from; " +
				"reload it, or write again with override",
			Path:           clean,
			ExpectedSHA256: strings.ToLower(strings.TrimSpace(req.BaseSHA256)),
			ActualSHA256:   actual,
			Exists:         exists,
		}
	}

	// A directory that is not there is not a containment failure and must not
	// be reported as one. This API writes files; it does not create the tree
	// they live in, and saying so plainly is better than a mkdir nobody asked
	// for.
	if parent := path.Dir(clean); parent != "." {
		info, derr := s.root.Stat(osPath(parent))
		switch {
		case derr == nil && info.IsDir():
			// Already there. Nothing to do, and nothing to create.
		case derr == nil || !req.CreateParents:
			// Present but not a directory, or missing with no request to make
			// it: the existing refusal, unchanged. `derr == nil` here is
			// "exists and is not a directory" — a regular file cannot be turned
			// into a directory, so CreateParents is not a licence to try.
			return http.StatusNotFound, map[string]string{"error": fmt.Sprintf(
				"the directory %s does not exist in the project; create it first", parent)}
		default:
			// Not deeper than the listing walks. See CreateParents above: the
			// walk gives up at maxWalkDepth and says so, and a create this API
			// allowed past that point would put a file where its own listing
			// can never name it.
			if strings.Count(parent, "/")+1 > maxWalkDepth {
				return http.StatusBadRequest, map[string]string{"error": fmt.Sprintf(
					"this API creates at most %d levels of directory", maxWalkDepth)}
			}
			if err := s.root.MkdirAll(osPath(parent), 0o777); err != nil {
				return http.StatusInternalServerError, errorBody(err)
			}
		}
	}

	if err := s.atomicWrite(clean, []byte(req.Content)); err != nil {
		return http.StatusInternalServerError, errorBody(err)
	}

	// Read back from the disk rather than echo the request. The point of the
	// answer is that the client can verify what landed, and an echo verifies
	// only that the request survived the trip out.
	data, status, err := s.readThroughRoot(clean)
	if err != nil {
		return status, errorBody(err)
	}
	content, status, err := contentOf(clean, data)
	if err != nil {
		return status, errorBody(err)
	}
	content.Created = !exists
	return http.StatusOK, content
}

func errorBody(err error) map[string]string { return map[string]string{"error": err.Error()} }

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
	// The live descriptor, never a name reopened. Closing the exclusive file and
	// opening its name again is a window in which that name can become
	// something else — a FIFO, most unpleasantly, whose open would then block
	// this handler for as long as nobody writes to it.
	f, staged, err := s.createStaging(dir)
	if err != nil {
		return err
	}
	remove := func() { _ = s.root.Remove(osPath(staged)) }

	if _, err := f.Write(data); err != nil {
		f.Close()
		remove()
		return fmt.Errorf("could not stage the write: %w", err)
	}
	// Mode first, then the flush, so nothing about the file is still pending
	// when the rename publishes it. Only the POSIX rwx bits are carried across:
	// owner, group, ACLs, extended attributes and the inode identity are not,
	// because a replace is a new file by construction. Where the target does
	// not exist there is nothing to carry, and the staging file's own mode —
	// 0o666 masked by the umask, see createStaging — is what gets published.
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
// returns the open file together with its project-relative path.
//
// The *file* is returned, not the name: the caller writes, chmods and syncs
// that descriptor and never reopens it. `os.Root` has no CreateTemp, so the
// exclusivity is asked for directly — O_EXCL is what makes a name collision a
// retry rather than a silent overwrite of somebody's staged edit.
//
// **0o666, masked by the umask — not 0o600.** This mode is the one a *new*
// file keeps: atomicWrite carries a mode across only for a file that already
// exists, so for a create there is nothing to carry and the staging mode is
// the published mode. 0o600 made every file this API brought into existence
// owner-only, in a project whose other files are whatever the user's umask
// makes them and whose directories this same request creates 0o777-masked. The
// umask is the user's answer to who may read their project; asking the kernel
// to apply it here is how this API stops having an opinion of its own.
func (s *Server) createStaging(dir string) (*os.File, string, error) {
	for attempt := 0; attempt < 10; attempt++ {
		var raw [12]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return nil, "", fmt.Errorf("could not stage the write: %w", err)
		}
		name := stagingPrefix + hex.EncodeToString(raw[:]) + ".tmp"
		full := name
		if dir != "." {
			full = path.Join(dir, name)
		}
		f, err := s.root.OpenFile(osPath(full), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o666)
		if err == nil {
			return f, full, nil
		}
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		return nil, "", fmt.Errorf("could not stage the write: %w", err)
	}
	return nil, "", errors.New("could not stage the write: no unused staging name")
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
	problems := s.walkProject(func(rel string, _ fs.FileInfo) {
		base := path.Base(rel)
		if !strings.HasPrefix(base, stagingPrefix) || !strings.HasSuffix(base, ".tmp") {
			return
		}
		if rerr := s.root.Remove(osPath(rel)); rerr != nil {
			s.log.Printf("desk: could not remove the stale staging file %s: %v", rel, rerr)
		}
	})
	for _, problem := range problems {
		s.log.Printf("desk: startup cleanup could not read %s", problem)
	}
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
