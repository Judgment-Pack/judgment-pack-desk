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
// project root is readable and writable: they are the user's own files, on the
// user's own machine, reached over loopback with the session token. It does not
// consult jpack.json, it does not care whether a path is a pack, and it forms
// no opinion about what the bytes mean. The runtime remains the only judge of
// that, and it judges after the bytes land — which is why every editor surface
// round-trips through the runtime's own tools rather than through anything
// here.
//
// Two things it does enforce, because they are not opinions about content:
//
//   - **Containment.** A path must resolve inside the project root, after
//     symlinks. Dot-dot, absolute paths, and a symlink inside the root pointing
//     out are all refused, on reads and on writes alike.
//   - **Honest concurrency.** Last write wins, but never silently: a write
//     carries the digest of the bytes the editor loaded, and a disagreement
//     with what is on disk is refused with both digests so the client can
//     reload. Overriding is available and explicit — the same discipline the
//     graph binding uses, for the same reason.

import (
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
	"unicode/utf8"
)

// maxFileBytes bounds a single read or write. The desk edits documents, and a
// document that does not fit here is one this editor has no business loading
// into a browser tab in the first place.
const maxFileBytes = 4 << 20

// errOutsideProject is the one containment failure, however it was reached.
// The reason is deliberately not itemized to the caller: dot-dot, an absolute
// path and a symlink escape are the same refusal, and describing which one
// would narrate the shape of the filesystem to whoever asked.
var errOutsideProject = errors.New("path is not inside the project")

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

// resolveInProject turns a client-supplied relative path into an absolute one
// inside the project, or refuses.
//
// The refusal is what matters, so it is done in one place and reached by every
// caller. Three families are stopped, and the third is the one a naive
// implementation misses:
//
//   - a path that is absolute, or empty, or names no file;
//   - a path whose cleaned form climbs out with `..`;
//   - a path that stays inside lexically and leaves through a **symlink** —
//     either because a component is a link out, or because the file itself is.
//
// The last is why this resolves symlinks rather than only cleaning strings.
// `filepath.Clean` is a lexical operation and cannot see a link, so a check
// built on it alone reports `notes/escape.json` as contained while the open
// call writes to wherever `escape.json` points.
//
// A file that does not exist yet is resolved through its parent, because a
// write must be able to create one — the parent still has to resolve inside.
func (s *Server) resolveInProject(rel string) (string, error) {
	clean, err := lexicallyInsideProject(rel)
	if err != nil {
		return "", err
	}

	// The root is resolved too. A project reached through a symlinked path is
	// perfectly ordinary — /tmp is one on macOS — and comparing a resolved
	// child against an unresolved root would refuse every file in it.
	root, rerr := filepath.EvalSymlinks(s.cfg.ProjectDir)
	if rerr != nil {
		return "", fmt.Errorf("project directory: %w", rerr)
	}
	full := filepath.Join(root, filepath.FromSlash(clean))

	real, resolveErr := resolveExisting(root, full)
	if resolveErr != nil {
		return "", resolveErr
	}
	if !within(root, real) {
		return "", errOutsideProject
	}
	return real, nil
}

// lexicallyInsideProject is the first of the two containment layers: the one
// that can be decided from the string alone.
//
// It refuses an empty path, an absolute one, and any path whose cleaned form is
// or climbs out through `..`, and returns the cleaned slash-path otherwise.
//
// The second layer — resolving symlinks and comparing against the resolved root
// — refuses a superset of what this does, so removing this function would not
// let an escape through today. It is kept, and tested on its own, for the
// reason defence in depth is usually kept: the layers refuse for different
// reasons, this one costs no filesystem call, and a change to the resolver
// would otherwise silently become the only thing standing between a `..` and
// the disk. A test that only asked "was it refused?" could not tell that the
// layer had stopped running.
func lexicallyInsideProject(rel string) (string, error) {
	if rel == "" {
		return "", errors.New("path is required")
	}
	// The wire form is slash-separated on every platform, so it is converted
	// once at the join rather than assumed anywhere else.
	if path.IsAbs(rel) || filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") || strings.Contains(rel, ":") {
		return "", errOutsideProject
	}
	clean := path.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errOutsideProject
	}
	return clean, nil
}

// resolveExisting resolves the symlinks on the part of full that exists, and
// re-appends the part that does not.
//
// A path naming a file that is not there yet still has to be checked, because
// a write creates one — and the check that matters is on the deepest component
// that does exist, since that is what a symlink could be. Walking up rather
// than looking only at the immediate parent is what lets a write name a file in
// a directory that is also not there yet: `a/b/c.json` under a symlinked `a`
// is still an escape, and this sees it.
//
// Whether a missing directory may be *created* is a separate question, and not
// this function's: it answers where the path would be, and the caller decides
// what to do about a parent that is absent.
func resolveExisting(root, full string) (string, error) {
	missing := []string{}
	current := full
	for {
		real, err := filepath.EvalSymlinks(current)
		if err == nil {
			if !within(root, real) {
				return "", errOutsideProject
			}
			// Re-append what did not exist, innermost last.
			for i := len(missing) - 1; i >= 0; i-- {
				real = filepath.Join(real, missing[i])
			}
			return real, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", errOutsideProject
		}
		parent := filepath.Dir(current)
		if parent == current {
			// Walked to the filesystem root without finding anything that
			// exists. Nothing here is inside the project.
			return "", errOutsideProject
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

// within reports whether p is root or sits under it.
//
// The separator in the prefix test is load-bearing: without it `/tmp/proj-evil`
// tests as inside `/tmp/proj`.
func within(root, p string) bool {
	if p == root {
		return true
	}
	return strings.HasPrefix(p, root+string(filepath.Separator))
}

// handleFiles lists every regular file in the project.
//
// Directories the watcher ignores are ignored here for the same reason: they
// are large, they churn, and nothing in them is a document anyone opened this
// desk to edit. Anything that is not a regular file — a symlink, a socket, a
// device — is left out rather than listed as something the read endpoint would
// then refuse.
func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	root, err := filepath.EvalSymlinks(s.cfg.ProjectDir)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("project directory: %v", err))
		return
	}

	files := []FileEntry{}
	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// One unreadable directory is not a reason to answer nothing.
			return nil //nolint:nilerr // reported as an absence, not a failure
		}
		if d.IsDir() {
			if p != root && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return nil
		}
		entry := FileEntry{Path: filepath.ToSlash(rel), Bytes: info.Size()}
		// The digest is what lets an editor open a file and later prove which
		// bytes it opened, so it is read here rather than left to a second call.
		if info.Size() <= maxFileBytes {
			if data, derr := os.ReadFile(p); derr == nil {
				entry.SHA256 = digestOf(data)
			}
		}
		files = append(files, entry)
		return nil
	})
	if walkErr != nil {
		writeJSONError(w, http.StatusInternalServerError, walkErr.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"root":  s.cfg.ProjectDir,
		"files": files,
		"note": "Every regular file in the project tree. This endpoint reads the " +
			"filesystem and nothing else: it does not consult jpack.json and forms no " +
			"opinion about what any file is.",
	})
}

// handleFileRead returns one file's bytes.
func (s *Server) handleFileRead(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	rel := r.URL.Query().Get("path")
	abs, err := s.resolveInProject(rel)
	if err != nil {
		writeJSONError(w, http.StatusForbidden, err.Error())
		return
	}
	content, status, err := readFileContent(rel, abs)
	if err != nil {
		writeJSONError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, content)
}

// readFileContent reads one resolved file, with the checks a JSON response
// needs made before the bytes go into one.
func readFileContent(rel, abs string) (*FileContent, int, error) {
	info, err := os.Stat(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, http.StatusNotFound, fmt.Errorf("no such file in the project: %s", rel)
		}
		return nil, http.StatusInternalServerError, err
	}
	if info.IsDir() {
		return nil, http.StatusBadRequest, fmt.Errorf("%s is a directory", rel)
	}
	if info.Size() > maxFileBytes {
		return nil, http.StatusRequestEntityTooLarge,
			fmt.Errorf("%s is %d bytes; this editor reads at most %d", rel, info.Size(), maxFileBytes)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	// JSON strings are UTF-8, and Go's encoder substitutes U+FFFD for bytes
	// that are not. Handing back a silently mangled document would be worse
	// than handing back nothing, and far worse if the editor then saved it.
	if !utf8.Valid(data) {
		return nil, http.StatusUnsupportedMediaType,
			fmt.Errorf("%s is not UTF-8 text, so this editor will not load it", rel)
	}
	return &FileContent{
		Path:    filepath.ToSlash(path.Clean(rel)),
		Bytes:   len(data),
		SHA256:  digestOf(data),
		Content: string(data),
	}, http.StatusOK, nil
}

// handleFileWrite replaces one file's bytes, atomically, and answers with what
// is on disk afterwards.
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
	abs, err := s.resolveInProject(req.Path)
	if err != nil {
		writeJSONError(w, http.StatusForbidden, err.Error())
		return
	}

	// What is there now, before anything is written.
	current, err := os.ReadFile(abs)
	exists := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	actual := ""
	if exists {
		actual = digestOf(current)
	}

	// Last write wins, but never silently. The editor states which bytes it
	// loaded; a disagreement means someone else changed the file underneath it,
	// and the client is told both digests rather than having its work quietly
	// overwrite theirs.
	if !req.Override && !strings.EqualFold(strings.TrimSpace(req.BaseSHA256), actual) {
		writeJSON(w, http.StatusConflict, conflict{
			Error: "the file on disk is not the file this edit started from; " +
				"reload it, or write again with override",
			Path:           filepath.ToSlash(path.Clean(req.Path)),
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
	if info, derr := os.Stat(filepath.Dir(abs)); derr != nil || !info.IsDir() {
		writeJSONError(w, http.StatusNotFound, fmt.Sprintf(
			"the directory %s does not exist in the project; create it first",
			path.Dir(path.Clean(req.Path))))
		return
	}

	if err := atomicWrite(abs, []byte(req.Content)); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Read back from the disk rather than echo the request. The point of the
	// answer is that the client can verify what landed, and an echo verifies
	// only that the request survived the trip out.
	content, status, err := readFileContent(req.Path, abs)
	if err != nil {
		writeJSONError(w, status, err.Error())
		return
	}
	content.Created = !exists
	writeJSON(w, http.StatusOK, content)
}

// atomicWrite replaces a file's contents without a window in which the file is
// half written.
//
// The temporary file is created in the **same directory** as the target,
// because rename is only atomic within a filesystem and a temp directory may
// be on another one. Everything is durable before the rename: a crash then
// leaves either the old file or the new one, and never a truncated file where
// a document used to be.
//
// A failure removes the temporary file. A crash between create and rename
// leaves one behind — it is a dotfile, it is not the document, and nothing
// reads it; that is the honest limit of this approach rather than a bug in it.
func atomicWrite(abs string, data []byte) error {
	dir := filepath.Dir(abs)
	tmp, err := os.CreateTemp(dir, ".jpack-desk-*.tmp")
	if err != nil {
		return fmt.Errorf("could not stage the write: %w", err)
	}
	name := tmp.Name()
	cleanup := func() {
		tmp.Close()
		os.Remove(name)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("could not stage the write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("could not flush the write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return fmt.Errorf("could not close the staged write: %w", err)
	}
	// Keep the mode a pre-existing file already had; CreateTemp makes 0600.
	if info, err := os.Stat(abs); err == nil {
		if cerr := os.Chmod(name, info.Mode().Perm()); cerr != nil {
			os.Remove(name)
			return fmt.Errorf("could not set the mode of the staged write: %w", cerr)
		}
	}
	if err := os.Rename(name, abs); err != nil {
		os.Remove(name)
		return fmt.Errorf("could not replace the file: %w", err)
	}
	return nil
}

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
