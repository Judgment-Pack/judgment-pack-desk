package desk

// Review and lock (ADR-0009, section 2).
//
// The review shows what the runtime's `packs verify` finds, file by file, and
// the lock is the runtime's `packs lock`. Desk forms no verdict of its own: it
// passes the runtime's findings through, and shows a diff only where it kept a
// copy of exactly the bytes the lock names. What it does add is the guarantee
// that **the bytes the owner confirmed are the bytes locked**: the confirmation
// carries the digests the owner was shown, they are checked against the files
// before the lock and against the lock afterwards, and anything that moved in
// between puts the previous lock back.

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	// reviewedCopiesDir holds a copy of each file Desk locked, named by the
	// hex SHA-256 of its bytes. It is private: the watcher skips it, and the
	// file API refuses it.
	reviewedCopiesDir = ".desk-private/reviewed"
	// reviewTextLimit is the most of one file the review shows as text.
	reviewTextLimit = 256 << 10
	// reviewConfirmLimit bounds a confirmation's body.
	reviewConfirmLimit = 2 << 20
	// reviewEntryLimit bounds how many documents one confirmation names.
	reviewEntryLimit = 4096
)

// reviewEntry is one declared document a lock covers, as Desk read it.
type reviewEntry struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Path   string `json:"path"`
	Digest string `json:"digest"`
}

// reviewSet is everything a lock covers: the configuration and every
// declared pack and graph, each by the digest of its bytes. The review shows
// it, the confirmation returns it, and the lock must pin exactly it.
type reviewSet struct {
	Config  string        `json:"config"`
	Entries []reviewEntry `json:"entries"`
}

func (a reviewSet) equal(b reviewSet) bool {
	if a.Config != b.Config || len(a.Entries) != len(b.Entries) {
		return false
	}
	for i := range a.Entries {
		if a.Entries[i] != b.Entries[i] {
			return false
		}
	}
	return true
}

func sortEntries(entries []reviewEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Kind != entries[j].Kind {
			return entries[i].Kind < entries[j].Kind
		}
		return entries[i].ID < entries[j].ID
	})
}

func sha256Digest(data []byte) string { return "sha256:" + digestOf(data) }

// declaredDocument is the one member of a configuration's entry Desk reads.
type declaredDocument struct {
	Path string `json:"path"`
}

// readReviewSet reads the configuration and every document it declares, and
// returns what a lock would cover, with the bytes each digest was taken of.
//
// It reads `packs` and `graphs` and each entry's `path`, and nothing else:
// which configuration is valid is the runtime's to say. Every read goes
// through the project's root.
func (s *Server) readReviewSet() (reviewSet, map[string][]byte, error) {
	config, _, err := s.readThroughRoot(runtimeConfigName)
	if err != nil {
		return reviewSet{}, nil, fmt.Errorf("%s could not be read: %w", runtimeConfigName, err)
	}
	var declared struct {
		Packs  map[string]declaredDocument `json:"packs"`
		Graphs map[string]declaredDocument `json:"graphs"`
	}
	if err := json.Unmarshal(config, &declared); err != nil {
		return reviewSet{}, nil, fmt.Errorf("%s is not a configuration Desk can read: %w", runtimeConfigName, err)
	}
	set := reviewSet{Config: sha256Digest(config), Entries: []reviewEntry{}}
	read := map[string][]byte{set.Config: config}
	for kind, entries := range map[string]map[string]declaredDocument{"pack": declared.Packs, "graph": declared.Graphs} {
		for id, entry := range entries {
			clean, err := wireRelativePath(entry.Path)
			if err != nil {
				return reviewSet{}, nil, fmt.Errorf("the %s %q is declared at %q, which Desk cannot read: %w", kind, id, entry.Path, err)
			}
			data, _, err := s.readThroughRoot(clean)
			if err != nil {
				return reviewSet{}, nil, fmt.Errorf("the %s %q could not be read: %w", kind, id, err)
			}
			digest := sha256Digest(data)
			read[digest] = data
			set.Entries = append(set.Entries, reviewEntry{Kind: kind, ID: id, Path: entry.Path, Digest: digest})
		}
	}
	if len(set.Entries) > reviewEntryLimit {
		return reviewSet{}, nil, fmt.Errorf("the project declares more than %d documents, which is more than Desk reviews", reviewEntryLimit)
	}
	sortEntries(set.Entries)
	return set, read, nil
}

// lockDocument is a `jpack.lock.json`, as runtime ADR-0019 writes it.
type lockDocument struct {
	LockVersion string `json:"lockVersion"`
	Config      struct {
		Digest string `json:"digest"`
	} `json:"config"`
	Packs  map[string]lockEntry `json:"packs"`
	Graphs map[string]lockEntry `json:"graphs"`
}

type lockEntry struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
}

// readLock reads the project's lock. A missing lock is not an error.
func (s *Server) readLock() (data []byte, present bool, err error) {
	data, _, err = s.readThroughRoot(runtimeLockName)
	if errors.Is(err, fs.ErrNotExist) || codeOf(err) == CodeNotFound {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

// lockedSet is what a lock pins, in the review's terms.
func lockedSet(data []byte) (reviewSet, lockDocument, error) {
	var lock lockDocument
	if err := json.Unmarshal(data, &lock); err != nil {
		return reviewSet{}, lock, err
	}
	set := reviewSet{Config: lock.Config.Digest, Entries: []reviewEntry{}}
	for kind, entries := range map[string]map[string]lockEntry{"pack": lock.Packs, "graph": lock.Graphs} {
		for id, entry := range entries {
			set.Entries = append(set.Entries, reviewEntry{Kind: kind, ID: id, Path: entry.Path, Digest: entry.Digest})
		}
	}
	sortEntries(set.Entries)
	return set, lock, nil
}

// reviewRuntime is the directory and refusal for running the runtime over
// this desk's project.
//
// A desk Desk made never reads another project's configuration, and the
// commands here name `--config jpack.json` and run without `JPACK_CONFIG`
// (`runRuntime`). The startup project keeps an inherited `JPACK_CONFIG` for
// its relay (`runtimeEnv`); where that names another file, the runtime is not
// reading the configuration this step would review, so the step refuses
// rather than lock one file while the runtime holds the project to another.
func (s *Server) reviewRuntime() (heldDir, string) {
	if s.project == nil || s.project.own == nil {
		return heldDir{}, "This desk holds no project to review."
	}
	dir := heldDir{file: s.project.own.dirFile, path: s.projectDir, info: s.project.info}
	if s.cfg.deskID != "" {
		return dir, ""
	}
	named := strings.TrimSpace(os.Getenv(runtimeConfigEnv))
	if named == "" {
		return dir, ""
	}
	if !filepath.IsAbs(named) {
		named = filepath.Join(s.projectDir, named)
	}
	there, err := os.Stat(named)
	own, ownErr := s.root.Stat(runtimeConfigName)
	if err == nil && ownErr == nil && os.SameFile(there, own) {
		return dir, ""
	}
	return dir, fmt.Sprintf("This project's runtime reads %s, which JPACK_CONFIG names, and not this project's %s, so Desk does not review or lock it here.", named, runtimeConfigName)
}

// reviewSide is one side of a finding's comparison.
type reviewSide struct {
	// State is "text"; "no-copy", where the lock names bytes Desk kept no copy
	// of; "unlocked", where the lock names nothing here; "absent", where there
	// is no file; or "too-large".
	State string `json:"state"`
	Text  string `json:"text,omitempty"`
}

// reviewFinding is one of the runtime's findings, passed through, with the
// two sides Desk can show for it.
type reviewFinding struct {
	Name    string     `json:"name"`
	Kind    string     `json:"kind,omitempty"`
	ID      string     `json:"id,omitempty"`
	Path    string     `json:"path,omitempty"`
	Detail  string     `json:"detail,omitempty"`
	Earlier reviewSide `json:"earlier"`
	Now     reviewSide `json:"now"`
}

type runtimeDiagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// reviewAnswer is what the review step shows.
type reviewAnswer struct {
	// Status is the runtime's own: "valid", "invalid", or "error".
	Status      string              `json:"status"`
	Locked      bool                `json:"locked"`
	Findings    []reviewFinding     `json:"findings"`
	Diagnostics []runtimeDiagnostic `json:"diagnostics"`
	// Set is what a lock would cover. It is absent where Desk could not read
	// every file, and Unreadable then says why: nothing can be confirmed.
	Set        *reviewSet `json:"set"`
	Unreadable string     `json:"unreadable,omitempty"`
}

// handleReview answers `GET /api/review`: the runtime's findings, and what a
// lock would cover.
func (s *Server) handleReview(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	dir, refusal := s.reviewRuntime()
	if refusal != "" {
		writeJSONCoded(w, http.StatusConflict, CodeBadRequest, refusal)
		return
	}
	answer, err := s.review(r.Context(), dir)
	if err != nil {
		writeJSONCoded(w, http.StatusInternalServerError, CodeInternal, "The project could not be reviewed: "+strings.TrimRight(err.Error(), ".")+".")
		return
	}
	writeJSON(w, http.StatusOK, answer)
}

func (s *Server) review(ctx context.Context, dir heldDir) (reviewAnswer, error) {
	out, runErr := runRuntime(ctx, s.cfg.JpackBin, dir, "packs", "verify", "--config", runtimeConfigName, "--format", "json")
	var verified struct {
		Command     string              `json:"command"`
		Status      string              `json:"status"`
		Diagnostics []runtimeDiagnostic `json:"diagnostics"`
		Findings    []struct {
			Name   string `json:"name"`
			Kind   string `json:"kind"`
			ID     string `json:"id"`
			Path   string `json:"path"`
			Detail string `json:"detail"`
		} `json:"findings"`
	}
	// A difference is a non-zero exit, so the answer is read whatever the
	// status; only an answer that is not the documented one is a failure.
	if out == nil || json.Unmarshal(out, &verified) != nil || verified.Command != "packs verify" || verified.Status == "" {
		if runErr == nil {
			runErr = errors.New("its packs verify did not answer as documented")
		}
		return reviewAnswer{}, fmt.Errorf("the runtime could not verify it: %w", runErr)
	}
	answer := reviewAnswer{Status: verified.Status, Findings: []reviewFinding{}, Diagnostics: verified.Diagnostics}
	if answer.Diagnostics == nil {
		answer.Diagnostics = []runtimeDiagnostic{}
	}
	lockData, present, err := s.readLock()
	if err != nil {
		return reviewAnswer{}, fmt.Errorf("%s could not be read: %w", runtimeLockName, err)
	}
	answer.Locked = present
	var lock lockDocument
	if present {
		// An unreadable lock is the runtime's to report; Desk then has no
		// digests to look copies up by, and shows none.
		_, lock, _ = lockedSet(lockData)
	}
	copies, _ := s.reviewedCopies(false)
	if copies != nil {
		defer copies.Close()
	}
	for _, found := range verified.Findings {
		finding := reviewFinding{Name: found.Name, Kind: found.Kind, ID: found.ID, Path: found.Path, Detail: found.Detail}
		lockedDigest := ""
		switch {
		case found.Name == "config-drift":
			lockedDigest = lock.Config.Digest
		case found.Kind == "pack":
			lockedDigest = lock.Packs[found.ID].Digest
		case found.Kind == "graph":
			lockedDigest = lock.Graphs[found.ID].Digest
		}
		finding.Earlier = earlierCopy(copies, lockedDigest)
		switch {
		case found.Name == "locked-but-undeclared":
			finding.Now = reviewSide{State: "absent"}
		case found.Name == "config-drift":
			finding.Now = s.currentText(runtimeConfigName)
		default:
			finding.Now = s.currentText(found.Path)
		}
		answer.Findings = append(answer.Findings, finding)
	}
	if set, _, err := s.readReviewSet(); err != nil {
		answer.Unreadable = err.Error()
	} else {
		answer.Set = &set
	}
	return answer, nil
}

// earlierCopy is the reviewed copy of the bytes a lock names, shown only when
// Desk kept a copy whose own digest is that one. The copy helps the owner
// read; the lock is the record.
func earlierCopy(copies *os.Root, digest string) reviewSide {
	if digest == "" {
		return reviewSide{State: "unlocked"}
	}
	name, ok := copyName(digest)
	if !ok || copies == nil {
		return reviewSide{State: "no-copy"}
	}
	data, err := readPrivateData(copies, name, maxFileBytes)
	if err != nil || sha256Digest(data) != digest {
		return reviewSide{State: "no-copy"}
	}
	return shownText(data)
}

// copyName is the file a copy of digest's bytes is kept under.
func copyName(digest string) (string, bool) {
	hexDigest, ok := strings.CutPrefix(digest, "sha256:")
	if !ok || len(hexDigest) != 64 {
		return "", false
	}
	if _, err := hex.DecodeString(hexDigest); err != nil || strings.ToLower(hexDigest) != hexDigest {
		return "", false
	}
	return hexDigest, true
}

func (s *Server) currentText(declared string) reviewSide {
	clean, err := wireRelativePath(declared)
	if err != nil {
		return reviewSide{State: "absent"}
	}
	data, _, err := s.readThroughRoot(clean)
	if err != nil {
		return reviewSide{State: "absent"}
	}
	return shownText(data)
}

func shownText(data []byte) reviewSide {
	if len(data) > reviewTextLimit || !utf8.Valid(data) {
		return reviewSide{State: "too-large"}
	}
	return reviewSide{State: "text", Text: string(data)}
}

// reviewedCopies opens `.desk-private/reviewed` through the project's root,
// one component at a time, refusing a link or anything but a directory at
// either step. With create, missing directories are made owner-only, and a
// new copies folder ignores itself in Git: in a project Desk did not make,
// `.desk-private/` may not be ignored yet, and these copies duplicate the
// project's own files.
func (s *Server) reviewedCopies(create bool) (*os.Root, error) {
	current := s.root
	var opened *os.Root
	made := false
	for _, part := range strings.Split(reviewedCopiesDir, "/") {
		info, err := current.Lstat(part)
		if errors.Is(err, fs.ErrNotExist) && create {
			if err = current.Mkdir(part, custodyDirMode); err != nil && !errors.Is(err, fs.ErrExist) {
				break
			}
			made = true
			info, err = current.Lstat(part)
		}
		if err == nil && (!info.IsDir() || info.Mode()&fs.ModeSymlink != 0) {
			err = fmt.Errorf("%s is not a directory Desk can keep copies in", part)
		}
		var next *os.Root
		if err == nil {
			next, err = current.OpenRoot(part)
		}
		if err == nil {
			if held, statErr := next.Stat("."); statErr != nil || !os.SameFile(info, held) {
				next.Close()
				err = fmt.Errorf("%s changed while being opened", part)
			}
		}
		if opened != nil {
			opened.Close()
		}
		if err != nil {
			return nil, err
		}
		opened, current = next, next
	}
	if made {
		if err := writePrivateData(opened, ".gitignore", []byte("*\n")); err != nil {
			opened.Close()
			return nil, err
		}
	}
	return opened, nil
}

// storeReviewedCopies keeps a copy of each locked file, named by its digest.
func (s *Server) storeReviewedCopies(read map[string][]byte, set reviewSet) error {
	copies, err := s.reviewedCopies(true)
	if err != nil {
		return err
	}
	defer copies.Close()
	digests := []string{set.Config}
	for _, entry := range set.Entries {
		digests = append(digests, entry.Digest)
	}
	for _, digest := range digests {
		name, ok := copyName(digest)
		data, have := read[digest]
		if !ok || !have || sha256Digest(data) != digest {
			return fmt.Errorf("no verified bytes for %s", digest)
		}
		if kept, err := readPrivateData(copies, name, maxFileBytes); err == nil && sha256Digest(kept) == digest {
			continue
		}
		if err := writePrivateData(copies, name, data); err != nil {
			return err
		}
	}
	return nil
}

// lockFailure is why a confirmation locked nothing, as its answer says it.
type lockFailure struct {
	status  int
	code    string
	message string
}

// handleReviewLock answers `POST /api/review/lock`: lock exactly the set the
// owner confirmed, or nothing.
//
// It is a `POST` with a JSON body and the desk's bearer, which a cross-site
// page cannot send: the guard checks the session and the Origin, and a
// request a browser marks cross-site is refused besides.
func (s *Server) handleReviewLock(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		writeJSONCoded(w, http.StatusForbidden, CodeForbidden, "A cross-site request cannot lock this project.")
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		writeJSONCoded(w, http.StatusUnsupportedMediaType, CodeBadRequest, "Send the confirmation as JSON.")
		return
	}
	var request struct {
		Set *reviewSet `json:"set"`
	}
	data, err := readBounded(r.Body, reviewConfirmLimit)
	if err != nil || decodeDataJSON(data, &request) != nil || request.Set == nil || len(request.Set.Entries) > reviewEntryLimit {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, "Confirm the reviewed set as the review showed it.")
		return
	}
	dir, refusal := s.reviewRuntime()
	if refusal != "" {
		writeJSONCoded(w, http.StatusConflict, CodeBadRequest, refusal)
		return
	}
	s.reviewMu.Lock()
	answer, failure := s.lockConfirmed(r.Context(), dir, *request.Set)
	s.reviewMu.Unlock()
	if failure != nil {
		writeJSONCoded(w, failure.status, failure.code, failure.message)
		return
	}
	writeJSON(w, http.StatusOK, answer)
}

// lockConfirmed locks the confirmed set, or puts the previous lock back.
func (s *Server) lockConfirmed(ctx context.Context, dir heldDir, confirmed reviewSet) (any, *lockFailure) {
	sortEntries(confirmed.Entries)
	current, read, err := s.readReviewSet()
	if err != nil || !current.equal(confirmed) {
		return nil, &lockFailure{http.StatusConflict, CodeStale, "The project changed after you reviewed it, so nothing was locked. Review it again."}
	}
	previous, hadLock, err := s.readLock()
	if err != nil {
		return nil, &lockFailure{http.StatusInternalServerError, CodeInternal, "The current lock could not be read, so nothing was locked: " + err.Error()}
	}
	lockErr := lockRuntimeProjectAt(ctx, s.cfg.JpackBin, dir)
	locked := false
	if lockErr == nil {
		if after, present, err := s.readLock(); err == nil && present {
			if pinned, _, err := lockedSet(after); err == nil && pinned.equal(current) {
				locked = true
			}
		}
	}
	if !locked {
		restoreErr := s.restoreLock(previous, hadLock)
		message := "A file changed while it was being locked, so the previous lock was put back and nothing else was written. Review it again."
		status, code := http.StatusConflict, CodeStale
		if lockErr != nil {
			message = "The runtime did not lock the project (" + strings.TrimRight(lockErr.Error(), ".") + "), so the previous lock was put back and nothing else was written."
			status, code = http.StatusInternalServerError, CodeInternal
		}
		if restoreErr != nil {
			message = "The lock could not be confirmed, and the previous lock could not be put back: " + restoreErr.Error() + ". Run jpack packs verify in the project before relying on it."
			status, code = http.StatusInternalServerError, CodeInternal
		}
		return nil, &lockFailure{status, code, message}
	}
	result := struct {
		Files  int    `json:"files"`
		Copies string `json:"copies"`
	}{Files: 1 + len(current.Entries), Copies: "stored"}
	if err := s.storeReviewedCopies(read, current); err != nil {
		s.log.Printf("desk: the reviewed copies were not stored: %v", err)
		result.Copies = "not-stored"
	}
	return result, nil
}

// restoreLock puts the previous lock's bytes back, or removes the lock where
// there was none.
func (s *Server) restoreLock(previous []byte, hadLock bool) error {
	if hadLock {
		return s.atomicWrite(runtimeLockName, previous)
	}
	if err := s.root.Remove(runtimeLockName); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// lockRuntimeProjectAt runs `packs lock` over the project's configuration,
// named, and reports whether the runtime said it locked it. What the lock
// pins is checked by the caller, against the file it wrote.
func lockRuntimeProjectAt(ctx context.Context, bin string, dir heldDir) error {
	out, err := runRuntime(ctx, bin, dir, "packs", "lock", "--config", runtimeConfigName, "--format", "json")
	var answer struct {
		Command     string              `json:"command"`
		Status      string              `json:"status"`
		Diagnostics []runtimeDiagnostic `json:"diagnostics"`
	}
	if out != nil && json.Unmarshal(out, &answer) == nil && answer.Command == "packs lock" && answer.Status == "valid" && err == nil {
		return nil
	}
	var said []string
	for _, diagnostic := range answer.Diagnostics {
		if message := strings.TrimSpace(diagnostic.Message); message != "" {
			said = append(said, message)
		}
	}
	if len(said) > 0 {
		return errors.New(strings.Join(said, " "))
	}
	if err != nil {
		return err
	}
	return errors.New("its packs lock did not answer as documented")
}
