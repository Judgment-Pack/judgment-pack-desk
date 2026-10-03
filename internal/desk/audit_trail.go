package desk

// Downloading the trail as exact bytes (ADR-0010, section 2; ADR-0009,
// question 3's answer).
//
// `GET /api/audit/trail?file=evaluations|signatures|stamps` hands over one of
// the runtime's own files in the project's audit directory: the trail
// (`evaluations.jsonl`), its signature sidecar (`signatures.jsonl`) or its
// stamps (`stamps.jsonl`), under the runtime's own name, as the bytes on disk.
// Nothing is parsed, decoded or encoded again on the way.
//
// **Opened through the project's root, refusing links.** The audit directory
// is the one `jpack.json` declares, and one that leaves the project is
// refused. Each directory on the way is opened as a root of its own, and must
// be the directory, not a link, that was looked at; the file must be a
// regular file, not a link, and the one that was looked at. The file API
// refuses `.desk-private`, so this is the one route that reads there, and it
// reads only these three names.
//
// **Read between two writes, under the runtime's own locks.** The runtime's
// writer appends to the trail, and to the sidecar in step with it, while it
// holds `flock`'s exclusive lock on the trail, and its verifier reads their
// sizes under the shared lock on the trail. Its stamps writer appends while
// it holds the exclusive lock on the stamps file itself. So this takes the
// shared lock on the trail for the trail and the sidecar, and the shared lock
// on the stamps file for the stamps: the size, read under it; the lock
// released; and exactly that many bytes streamed from the same descriptor.
// (The verifier reads the stamps under the trail's lock and counts a line a
// write left incomplete as unreadable; a download hands over whole lines.)
// Writers only append, so those bytes do not change while they are read.
//
// **Where no such lock can be taken, nothing is handed over.** The runtime
// takes `flock` on Darwin, Dragonfly, FreeBSD, Linux, NetBSD and OpenBSD, and
// a byte-range lock through `LockFileEx` on Windows. Desk takes the first, on
// the same systems, and does not take the second: on every other build, and
// on a file system that answers that it supports no `flock`, the download is
// refused, and says so, rather than reading a size that may fall inside a
// write.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// auditTrailFiles is each value `file` may take, and the runtime's own name
// for that file beside the trail (runtime 0.26.0: `audit.FileName`,
// `audit.SidecarName` and `audit.StampsName`).
var auditTrailFiles = map[string]string{
	"evaluations": "evaluations.jsonl",
	"signatures":  "signatures.jsonl",
	"stamps":      "stamps.jsonl",
}

// auditTrailOrder is the order the panel lists them in.
var auditTrailOrder = []string{"evaluations", "signatures", "stamps"}

// auditLockWait bounds how long a download waits for a writer to release
// the lock. A writer holds it across one append and its sync. A variable only
// so a test can see a lock held too long refused without waiting it out.
var auditLockWait = 10 * time.Second

var (
	errAuditNoLock    = errors.New("no lock can be taken on this file here")
	errAuditLockBusy  = errors.New("a writer held the lock for too long")
	errAuditLinked    = errors.New("passes through a symbolic link")
	errAuditNotAFile  = errors.New("is not a regular file")
	errAuditNoTrail   = errors.New("there is no trail beside it")
	errAuditOutside   = errors.New("is outside the project")
	errAuditUnchecked = errors.New("changed while it was being opened")
)

// auditTrailRequest reads the one thing a download may ask: `file`, once,
// naming one of the three files. Anything else in the query, a repeated or
// unknown value, or a query that does not parse, is refused.
func auditTrailRequest(raw string) (string, bool) {
	values, err := url.ParseQuery(raw)
	if err != nil || len(values) != 1 || len(values["file"]) != 1 {
		return "", false
	}
	which := values["file"][0]
	if _, ok := auditTrailFiles[which]; !ok {
		return "", false
	}
	return which, true
}

// auditDirParts is the audit directory jpack.json declares, as the
// components under the project's root it names, or errAuditOutside where it
// names a place outside the project, as the runtime's own containment does
// (an absolute path, or one that climbs out), or a path the file API would
// not read, with a backslash or a colon in it. "." is the project's root
// itself.
func auditDirParts(dir string) ([]string, error) {
	if strings.ContainsAny(dir, "\x00\\:") || path.IsAbs(dir) || filepath.IsAbs(dir) {
		return nil, errAuditOutside
	}
	clean := path.Clean(dir)
	if clean == "." {
		return nil, nil
	}
	if !fs.ValidPath(clean) {
		return nil, errAuditOutside
	}
	return strings.Split(clean, "/"), nil
}

// openAuditDir opens the audit directory through the project's root, one
// component at a time, each a directory and not a link, and each the
// directory that was looked at.
func (s *Server) openAuditDir(parts []string) (*os.Root, error) {
	current, err := s.root.OpenRoot(".")
	if err != nil {
		return nil, err
	}
	for _, part := range parts {
		info, err := current.Lstat(part)
		if err == nil && info.Mode()&fs.ModeSymlink != 0 {
			err = errAuditLinked
		} else if err == nil && !info.IsDir() {
			err = errAuditNotAFile
		}
		var next *os.Root
		if err == nil {
			next, err = current.OpenRoot(part)
		}
		if err == nil {
			if held, statErr := next.Stat("."); statErr != nil || !os.SameFile(info, held) {
				next.Close()
				err = errAuditUnchecked
			}
		}
		current.Close()
		if err != nil {
			return nil, err
		}
		current = next
	}
	return current, nil
}

// openAuditFile opens one file in the audit directory for reading: a regular
// file, not a link, and the file that was looked at.
func openAuditFile(dir *os.Root, name string) (*os.File, error) {
	info, err := dir.Lstat(name)
	if err != nil {
		return nil, err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return nil, errAuditLinked
	}
	if !info.Mode().IsRegular() {
		return nil, errAuditNotAFile
	}
	file, err := dir.OpenFile(name, os.O_RDONLY|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		file.Close()
		return nil, errAuditUnchecked
	}
	return file, nil
}

// auditSnapshot is one file opened for a download, and the size read under
// its writer's lock: the bytes to stream.
type auditSnapshot struct {
	file *os.File
	size int64
}

// snapshotAuditFile opens which and reads its size between two writes, under
// the lock its writer takes: the trail's for the trail and the sidecar, the
// stamps file's own for the stamps.
func snapshotAuditFile(ctx context.Context, dir *os.Root, which string) (auditSnapshot, error) {
	file, err := openAuditFile(dir, auditTrailFiles[which])
	if err != nil {
		return auditSnapshot{}, err
	}
	locked := file
	if which == "signatures" {
		trail, err := openAuditFile(dir, auditTrailFiles["evaluations"])
		if errors.Is(err, fs.ErrNotExist) {
			err = errAuditNoTrail
		}
		if err != nil {
			file.Close()
			return auditSnapshot{}, err
		}
		defer trail.Close()
		locked = trail
	}
	unlock, err := lockAuditShared(ctx, locked, auditLockWait)
	if err != nil {
		file.Close()
		return auditSnapshot{}, err
	}
	info, err := file.Stat()
	unlock()
	if err != nil {
		file.Close()
		return auditSnapshot{}, err
	}
	return auditSnapshot{file: file, size: info.Size()}, nil
}

// auditFilesPresent is which of the three files the audit directory holds,
// as the download would open them, in the panel's order. It takes no lock.
func (s *Server) auditFilesPresent() []string {
	present := []string{}
	dir, declared, err := s.projectAuditDir()
	if err != nil || !declared {
		return present
	}
	parts, err := auditDirParts(dir)
	if err != nil {
		return present
	}
	root, err := s.openAuditDir(parts)
	if err != nil {
		return present
	}
	defer root.Close()
	for _, which := range auditTrailOrder {
		if file, err := openAuditFile(root, auditTrailFiles[which]); err == nil {
			file.Close()
			present = append(present, which)
		}
	}
	return present
}

// handleAuditTrail answers `GET /api/audit/trail?file=…` with the file's
// exact bytes, saved under the runtime's own name.
func (s *Server) handleAuditTrail(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	which, ok := auditTrailRequest(r.URL.RawQuery)
	if !ok {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, "Ask for one file, once: file=evaluations, file=signatures or file=stamps.")
		return
	}
	name := auditTrailFiles[which]
	if _, refusal := s.auditRuntime(); refusal != "" {
		writeJSONCoded(w, http.StatusConflict, CodeBadRequest, refusal)
		return
	}
	dir, declared, err := s.projectAuditDir()
	if err != nil {
		writeJSONCoded(w, http.StatusInternalServerError, CodeInternal, "The trail could not be read: "+strings.TrimRight(err.Error(), ".")+".")
		return
	}
	if !declared {
		writeJSONCoded(w, http.StatusNotFound, CodeNotFound, "This project's jpack.json declares no audit directory, so it keeps no trail.")
		return
	}
	parts, err := auditDirParts(dir)
	if err != nil {
		writeJSONCoded(w, http.StatusForbidden, CodeOutsideRoot, fmt.Sprintf("The audit directory %q that jpack.json declares is not a folder inside the project that Desk reads, so Desk does not read it.", dir))
		return
	}
	root, err := s.openAuditDir(parts)
	var snapshot auditSnapshot
	if err == nil {
		defer root.Close()
		snapshot, err = snapshotAuditFile(r.Context(), root, which)
	}
	if err != nil {
		status, code, message := auditTrailRefusal(err, dir, name)
		writeJSONCoded(w, status, code, message)
		return
	}
	defer snapshot.file.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("Content-Length", strconv.FormatInt(snapshot.size, 10))
	w.WriteHeader(http.StatusOK)
	// Exactly the bytes before the size read under the lock, from the
	// descriptor the size was read on. A file cut shorter since ends the
	// answer short of its length, which a client sees as a failed download.
	_, _ = io.Copy(w, io.NewSectionReader(snapshot.file, 0, snapshot.size))
}

// auditTrailRefusal is what a download that could not be made says.
func auditTrailRefusal(err error, dir, name string) (int, string, string) {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return http.StatusNotFound, CodeNotFound, fmt.Sprintf("There is no %s in this project's audit directory, %s.", name, dir)
	case errors.Is(err, errAuditLinked):
		return http.StatusForbidden, CodeSymlink, fmt.Sprintf("The way to %s in %s passes through a symbolic link, so Desk does not read it.", name, dir)
	case errors.Is(err, errAuditNotAFile):
		return http.StatusForbidden, CodeNotAFile, fmt.Sprintf("The way to %s in %s is not a folder and a regular file, so Desk does not read it.", name, dir)
	case errors.Is(err, errAuditUnchecked):
		return http.StatusConflict, CodeStale, fmt.Sprintf("The way to %s in %s changed while Desk was opening it. Try again.", name, dir)
	case errors.Is(err, errAuditNoTrail):
		return http.StatusConflict, CodeNotFound, fmt.Sprintf("The signature sidecar is read under the trail's lock, and there is no evaluations.jsonl beside it in %s.", dir)
	case errors.Is(err, errAuditNoLock):
		return http.StatusNotImplemented, CodeInternal, fmt.Sprintf("Desk can take no lock on %s here, so it cannot read it between two writes, and does not hand it over. Copy it while no runtime writes to this project.", name)
	case errors.Is(err, errAuditLockBusy):
		return http.StatusServiceUnavailable, CodeInternal, fmt.Sprintf("A runtime held the lock on %s for longer than %s. Try again.", name, auditLockWait)
	}
	return http.StatusInternalServerError, CodeInternal, fmt.Sprintf("%s could not be read: %s.", name, strings.TrimRight(err.Error(), "."))
}
