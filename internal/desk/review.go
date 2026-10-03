package desk

// Review and lock (ADR-0009, section 2).
//
// The review shows what the runtime's `packs verify` finds, and every file a
// lock would cover; the lock is the runtime's `packs lock`. Desk forms no
// verdict of its own: it passes the runtime's findings through, and shows a
// diff only where it kept a copy of exactly the bytes the lock names.
//
// **What the owner confirms is bound to what the owner was shown.** One
// reading of the project — the configuration, every declared pack and graph,
// and the lock — is the review: the contents shown, the digests confirmed,
// and the runtime's findings (taken over a private copy of exactly those
// bytes) all come from it, and nothing is read twice. The review hands back
// a token: a MAC, under a key this desk alone holds, over the desk, that
// reading's digests and the lock's bytes. A confirmation carries the token,
// and is honoured only if a fresh reading, under the desk's review lock,
// yields the same token: the same desk, the same files, the same lock. The
// lock the runtime then writes must pin exactly those digests, or the
// previous lock is put back.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	// reviewedCopiesDir holds a copy of each file Desk locked, named by the
	// hex SHA-256 of its bytes. It is private: the watcher skips it, and the
	// file API refuses it.
	reviewedCopiesDir = ".desk-private/reviewed"
	// reviewTextLimit is the largest file the review reads, shows, and so
	// lets the owner confirm. No read takes more than this, plus one byte.
	reviewTextLimit = 1 << 20
	// reviewReadingLimit bounds everything one reading retains: each
	// distinct file once, the configuration and the lock included. It is
	// enforced before each read, so no reading retains more than it, plus one
	// byte.
	reviewReadingLimit = 8 << 20
	// reviewEarlierLimit bounds the earlier copies one review shows. Past it,
	// a copy is reported as not shown rather than read.
	reviewEarlierLimit = 8 << 20
	// reviewConfirmLimit bounds a confirmation's body: it carries a token.
	reviewConfirmLimit = 4 << 10
	// reviewEntryLimit bounds how many documents a configuration may declare
	// for Desk to review it. Past it, nothing is read.
	reviewEntryLimit = 1024
)

// reviewEntry is one declared document a lock covers.
type reviewEntry struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Path   string `json:"path"`
	Digest string `json:"digest"`
}

// reviewSet is everything a lock covers: the configuration and every
// declared pack and graph, each by the digest of its bytes.
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

// snapshotDocument is one declared document as the snapshot read it. Ids
// that name one file share one reading of it.
type snapshotDocument struct {
	entry reviewEntry
	clean string
	data  []byte
}

// reviewSnapshot is one reading of everything the review step is about.
type reviewSnapshot struct {
	set     reviewSet
	config  []byte
	docs    []snapshotDocument
	lock    []byte
	hasLock bool
	// files is each distinct file read, by its clean path, once.
	files map[string][]byte
}

// bytesOf is the snapshot's bytes for a digest it holds.
func (snap *reviewSnapshot) bytesOf(digest string) ([]byte, bool) {
	if digest == snap.set.Config {
		return snap.config, true
	}
	for _, doc := range snap.docs {
		if doc.entry.Digest == digest {
			return doc.data, true
		}
	}
	return nil, false
}

// readReviewFile reads one project file under the file API's own rules: a
// path the API would refuse — private, excluded, a staging file, or one
// that passes through a link — is refused here too.
func (s *Server) readReviewFile(rel string) ([]byte, error) {
	return s.readReviewFileWithin(rel, maxFileBytes)
}

func (s *Server) readReviewFileWithin(rel string, limit int) ([]byte, error) {
	clean, err := wireRelativePath(rel)
	if err != nil {
		return nil, err
	}
	if err := s.refuseSymlinkedPath(clean); err != nil {
		return nil, err
	}
	data, _, err := s.readThroughRootWithin(clean, limit)
	return data, err
}

// testHookReviewRetained runs after the review retains a file's bytes, and
// is nil outside tests. It lets a test count what a reading holds.
var testHookReviewRetained func(clean string, retained int)

// blockedReading is a reading the review stopped: a file it cannot show, or
// a budget it would pass. Nothing past it was read.
type blockedReading struct{ reason string }

func (b *blockedReading) Error() string { return b.reason }

// reviewReader reads a snapshot's files within its budgets: each distinct
// file once, none larger than reviewTextLimit, and all of them together no
// more than reviewReadingLimit. Each limit is applied before the read, so a
// file is never retained past it.
type reviewReader struct {
	s        *Server
	files    map[string][]byte
	retained int
}

func (r *reviewReader) take(name string, absentOK bool) ([]byte, bool, error) {
	clean, err := wireRelativePath(name)
	if err != nil {
		return nil, false, err
	}
	if data, ok := r.files[clean]; ok {
		return data, true, nil
	}
	limit := reviewTextLimit
	if remaining := reviewReadingLimit - r.retained; remaining < limit {
		limit = remaining
	}
	data, err := r.s.readReviewFileWithin(clean, limit)
	switch {
	case absentOK && codeOf(err) == CodeNotFound:
		return nil, false, nil
	case codeOf(err) == CodeTooLarge && limit < reviewTextLimit:
		return nil, false, &blockedReading{fmt.Sprintf("the files a lock would cover add up to more than %d bytes, which is more than Desk shows at once", reviewReadingLimit)}
	case codeOf(err) == CodeTooLarge:
		return nil, false, &blockedReading{fmt.Sprintf("%s is larger than %d bytes, which is more than Desk shows", name, reviewTextLimit)}
	case err != nil:
		return nil, false, err
	case !utf8.Valid(data):
		return nil, false, &blockedReading{fmt.Sprintf("%s is not text Desk can show", name)}
	}
	r.retained += len(data)
	r.files[clean] = data
	if testHookReviewRetained != nil {
		testHookReviewRetained(clean, r.retained)
	}
	return data, true, nil
}

// readSnapshot reads the configuration, every document it declares, and the
// lock, once, within the review's budgets. It reads `packs` and `graphs` and
// each entry's `path`, and nothing else: which configuration is valid is the
// runtime's to say. A budget it would pass stops it, with a *blockedReading.
func (s *Server) readSnapshot() (*reviewSnapshot, error) {
	reader := &reviewReader{s: s, files: map[string][]byte{}}
	config, _, err := reader.take(runtimeConfigName, false)
	if err != nil {
		return nil, fmt.Errorf("%s could not be read: %w", runtimeConfigName, err)
	}
	var declared struct {
		Packs  map[string]declaredDocument `json:"packs"`
		Graphs map[string]declaredDocument `json:"graphs"`
	}
	if err := json.Unmarshal(config, &declared); err != nil {
		return nil, fmt.Errorf("%s is not a configuration Desk can read: %w", runtimeConfigName, err)
	}
	if len(declared.Packs)+len(declared.Graphs) > reviewEntryLimit {
		return nil, &blockedReading{fmt.Sprintf("the project declares more than %d documents, which is more than Desk reviews", reviewEntryLimit)}
	}
	snap := &reviewSnapshot{config: config, set: reviewSet{Config: sha256Digest(config), Entries: []reviewEntry{}}}
	for _, kind := range []string{"graph", "pack"} {
		entries := declared.Packs
		if kind == "graph" {
			entries = declared.Graphs
		}
		ids := make([]string, 0, len(entries))
		for id := range entries {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			path := entries[id].Path
			clean, err := wireRelativePath(path)
			if err != nil {
				return nil, fmt.Errorf("the %s %q is declared at %q, which Desk does not read: %w", kind, id, path, err)
			}
			data, _, err := reader.take(clean, false)
			if err != nil {
				return nil, fmt.Errorf("the %s %q could not be read: %w", kind, id, err)
			}
			item := reviewEntry{Kind: kind, ID: id, Path: path, Digest: sha256Digest(data)}
			snap.docs = append(snap.docs, snapshotDocument{entry: item, clean: clean, data: data})
			snap.set.Entries = append(snap.set.Entries, item)
		}
	}
	sortEntries(snap.set.Entries)
	lock, present, err := reader.take(runtimeLockName, true)
	if err != nil {
		return nil, fmt.Errorf("%s could not be read: %w", runtimeLockName, err)
	}
	snap.lock, snap.hasLock, snap.files = lock, present, reader.files
	return snap, nil
}

// reviewToken binds a confirmation to this desk and to one reading of its
// project: a MAC, under the desk's own key, over the desk's identity, the
// set's digests, and the lock's bytes. A desk's key is random per process,
// so a token names one desk and does not outlive it.
func (s *Server) reviewToken(snap *reviewSnapshot) string {
	lock := "absent"
	if snap.hasLock {
		lock = sha256Digest(snap.lock)
	}
	payload, _ := json.Marshal(struct {
		Desk    string    `json:"desk"`
		Project string    `json:"project"`
		Set     reviewSet `json:"set"`
		Lock    string    `json:"lock"`
	}{s.cfg.deskID, s.projectDir, snap.set, lock})
	mac := hmac.New(sha256.New, s.reviewKey[:])
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
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
// A desk Desk made never reads another project's configuration: the commands
// here name `--config jpack.json` and run without `JPACK_CONFIG`
// (`runRuntime`). The startup project keeps an inherited `JPACK_CONFIG` for
// its relay (`runtimeEnv`), so the step proceeds only where that names this
// project's own `jpack.json` **by path**: the runtime resolves a relative
// value against the project, and reads every document and the lock relative
// to the configuration's own directory. A hard link to `jpack.json` from
// another directory is the same file in another project, so an identity
// check is not enough; the directory has to be this one.
//
// The directory is marked as the startup desk's where it is, so the commands
// keep the owner's inherited `JPACK_SIGNING_KEY` there, and only there.
func (s *Server) reviewRuntime() (heldDir, string) {
	return s.projectRuntime("This desk holds no project to review.", "review or lock it")
}

// projectRuntime is reviewRuntime for any command Desk runs over this desk's
// project: the same directory, and the same refusal, in which none says why
// there is no project and doing says what Desk does not do there.
func (s *Server) projectRuntime(none, doing string) (heldDir, string) {
	if s.project == nil || s.project.own == nil {
		return heldDir{}, none
	}
	dir := heldDir{file: s.project.own.dirFile, path: s.projectDir, info: s.project.info, startup: s.cfg.deskID == ""}
	if s.cfg.deskID != "" {
		return dir, ""
	}
	named := strings.TrimSpace(os.Getenv(runtimeConfigEnv))
	if named == "" || configNamesProject(named, s.projectDir, s.project.info) {
		return dir, ""
	}
	return dir, fmt.Sprintf("This project's runtime reads %s, which JPACK_CONFIG names, and not this project's %s, so Desk does not %s here.", named, runtimeConfigName, doing)
}

// configNamesProject reports whether a JPACK_CONFIG value names the
// project's own jpack.json: a file of that name, not a link, in a directory
// that resolves to the project itself.
func configNamesProject(named, projectDir string, project fs.FileInfo) bool {
	if !filepath.IsAbs(named) {
		named = filepath.Join(projectDir, named)
	}
	named = filepath.Clean(named)
	if filepath.Base(named) != runtimeConfigName {
		return false
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(named))
	if err != nil || dir != projectDir {
		return false
	}
	there, err := os.Stat(filepath.Dir(named))
	if err != nil || !os.SameFile(there, project) {
		return false
	}
	file, err := os.Lstat(named)
	return err == nil && file.Mode().IsRegular()
}

// reviewSide is one side of a file's comparison.
type reviewSide struct {
	// State is "text"; "no-copy", where the lock names bytes Desk kept no copy
	// of; "absent", where there is no file; or "not-shown", for an earlier
	// copy too large to show, or past the review's budget for them.
	State string `json:"state"`
	// Digest names the text in the answer's contents, where State is "text".
	Digest string `json:"digest,omitempty"`
}

// reviewFile is one file the review is about: one the lock would cover, or
// one the lock covers that the configuration no longer declares.
type reviewFile struct {
	Kind   string `json:"kind"`
	ID     string `json:"id,omitempty"`
	Path   string `json:"path"`
	Digest string `json:"digest,omitempty"`
	// Lock is how the current lock stands to this file: "same", where it pins
	// these bytes; "other", where it pins other bytes; "none", where it names
	// nothing here; or "removed", where only the lock names it.
	Lock string `json:"lock"`
	// Locked is the digest the current lock pins for this file, as this
	// reading read the lock, and absent where it pins none. It is the lock's
	// own entry, so a page can compare other bytes with it: Jobs compares the
	// bytes a release is made from (ADR-0009, question 4).
	Locked string     `json:"locked,omitempty"`
	Now    reviewSide `json:"now"`
	// Earlier is the bytes the lock names, where it names other bytes or only
	// the lock names this file.
	Earlier *reviewSide `json:"earlier,omitempty"`
}

type reviewFinding struct {
	Name   string `json:"name"`
	Kind   string `json:"kind,omitempty"`
	ID     string `json:"id,omitempty"`
	Path   string `json:"path,omitempty"`
	Detail string `json:"detail,omitempty"`
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
	Files       []reviewFile        `json:"files"`
	// Contents is each text the answer shows, once, by its digest: ids that
	// name one file, and files with one content, share it.
	Contents map[string]string `json:"contents"`
	// Token confirms exactly what this answer shows. It is absent where the
	// reading stopped, and Blocked then says why.
	Token   string `json:"token,omitempty"`
	Blocked string `json:"blocked,omitempty"`
}

// handleReview answers `GET /api/review`.
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

type verifiedAnswer struct {
	Command     string              `json:"command"`
	Status      string              `json:"status"`
	Diagnostics []runtimeDiagnostic `json:"diagnostics"`
	Findings    []reviewFinding     `json:"findings"`
}

// runVerify runs `packs verify` in dir, and reads its answer whatever the
// exit: a difference is a non-zero exit.
func (s *Server) runVerify(ctx context.Context, dir heldDir) (verifiedAnswer, error) {
	out, runErr := runRuntime(ctx, s.cfg.JpackBin, dir, "packs", "verify", "--config", runtimeConfigName, "--format", "json")
	var verified verifiedAnswer
	if out == nil || json.Unmarshal(out, &verified) != nil || verified.Command != "packs verify" || verified.Status == "" {
		if runErr == nil {
			runErr = errors.New("its packs verify did not answer as documented")
		}
		return verified, fmt.Errorf("the runtime could not verify it: %w", runErr)
	}
	return verified, nil
}

// verifySnapshot runs `packs verify` over a private copy of exactly the
// snapshot's bytes, so the findings are about what the review shows, and the
// runtime reads nothing of the project while it runs.
func (s *Server) verifySnapshot(ctx context.Context, snap *reviewSnapshot) (verifiedAnswer, error) {
	// A fresh owner-only directory of this process's own, removed after.
	where, err := os.MkdirTemp("", "jpack-desk-review-")
	if err != nil {
		return verifiedAnswer{}, err
	}
	defer os.RemoveAll(where)
	root, err := os.OpenRoot(where)
	if err != nil {
		return verifiedAnswer{}, err
	}
	defer root.Close()
	write := func(clean string, data []byte) error {
		if dir := path.Dir(clean); dir != "." {
			if err := root.MkdirAll(filepath.FromSlash(dir), 0o700); err != nil {
				return err
			}
		}
		return root.WriteFile(filepath.FromSlash(clean), data, 0o600)
	}
	// Each distinct file once, as the reading holds it.
	for clean, data := range snap.files {
		if err := write(clean, data); err != nil {
			return verifiedAnswer{}, err
		}
	}
	file, err := root.Open(".")
	if err != nil {
		return verifiedAnswer{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return verifiedAnswer{}, err
	}
	// A copy of the startup desk's project is still the startup desk's.
	return s.runVerify(ctx, heldDir{file: file, path: where, info: info, startup: s.cfg.deskID == ""})
}

func (s *Server) review(ctx context.Context, dir heldDir) (reviewAnswer, error) {
	snap, readErr := s.readSnapshot()
	answer, err := s.reviewOf(ctx, dir, snap, readErr)
	if err == nil && readErr == nil {
		answer.Token = s.reviewToken(snap)
	}
	return answer, err
}

// reviewOf is the review of one reading, without a token: the runtime's
// findings over a private copy of it, and every file it holds. The upgrade
// offer reviews a reading whose configuration it has replaced
// (`withConfig`), and gives its own token.
func (s *Server) reviewOf(ctx context.Context, dir heldDir, snap *reviewSnapshot, readErr error) (reviewAnswer, error) {
	var verified verifiedAnswer
	var err error
	if readErr != nil {
		// Nothing can be confirmed; the runtime's own findings still say
		// what it sees, over the project itself.
		verified, err = s.runVerify(ctx, dir)
	} else {
		verified, err = s.verifySnapshot(ctx, snap)
	}
	if err != nil {
		return reviewAnswer{}, err
	}
	answer := reviewAnswer{Status: verified.Status, Findings: verified.Findings, Diagnostics: verified.Diagnostics, Files: []reviewFile{}, Contents: map[string]string{}}
	if answer.Findings == nil {
		answer.Findings = []reviewFinding{}
	}
	if answer.Diagnostics == nil {
		answer.Diagnostics = []runtimeDiagnostic{}
	}
	if readErr != nil {
		answer.Blocked = readErr.Error()
		return answer, nil
	}
	answer.Locked = snap.hasLock
	var lock lockDocument
	if snap.hasLock {
		// An unreadable lock is the runtime's to report; Desk then has no
		// digests to look copies up by, and shows none.
		_, lock, _ = lockedSet(snap.lock)
	}
	copies, _ := s.reviewedCopies(false)
	if copies != nil {
		defer copies.Close()
	}
	earlier := &earlierReader{copies: copies, contents: answer.Contents}
	show := func(data []byte) reviewSide {
		digest := sha256Digest(data)
		answer.Contents[digest] = string(data)
		return reviewSide{State: "text", Digest: digest}
	}
	file := func(kind, id, path, digest, locked string, data []byte) reviewFile {
		row := reviewFile{Kind: kind, ID: id, Path: path, Digest: digest, Lock: "none", Now: show(data)}
		if snap.hasLock {
			row.Locked = locked
		}
		switch {
		case row.Locked == "":
		case locked == digest:
			row.Lock = "same"
		default:
			row.Lock = "other"
			side := earlier.side(locked)
			row.Earlier = &side
		}
		return row
	}
	answer.Files = append(answer.Files, file("config", "", runtimeConfigName, snap.set.Config, lock.Config.Digest, snap.config))
	declared := map[string]bool{}
	for _, doc := range snap.docs {
		entries := lock.Packs
		if doc.entry.Kind == "graph" {
			entries = lock.Graphs
		}
		declared[doc.entry.Kind+"\x00"+doc.entry.ID] = true
		answer.Files = append(answer.Files, file(doc.entry.Kind, doc.entry.ID, doc.entry.Path, doc.entry.Digest, entries[doc.entry.ID].Digest, doc.data))
	}
	// What only the lock names: shown as it was locked, where Desk kept it.
	for kind, entries := range map[string]map[string]lockEntry{"pack": lock.Packs, "graph": lock.Graphs} {
		for id, entry := range entries {
			if declared[kind+"\x00"+id] {
				continue
			}
			side := earlier.side(entry.Digest)
			answer.Files = append(answer.Files, reviewFile{Kind: kind, ID: id, Path: entry.Path, Lock: "removed", Locked: entry.Digest, Now: reviewSide{State: "absent"}, Earlier: &side})
		}
	}
	sort.SliceStable(answer.Files[1:], func(i, j int) bool {
		a, b := answer.Files[1+i], answer.Files[1+j]
		return a.Kind < b.Kind || a.Kind == b.Kind && a.ID < b.ID
	})
	return answer, nil
}

// withConfig is the reading with its configuration replaced by config: the
// same documents and lock, as the runtime would read them once config is
// written.
func (snap *reviewSnapshot) withConfig(config []byte) *reviewSnapshot {
	next := *snap
	next.config = config
	next.set.Config = sha256Digest(config)
	next.set.Entries = slices.Clone(snap.set.Entries)
	next.docs = slices.Clone(snap.docs)
	next.files = maps.Clone(snap.files)
	next.files[runtimeConfigName] = config
	return &next
}

// earlierReader shows the reviewed copies of the bytes a lock names: each
// digest once, none larger than reviewTextLimit, and all of them together no
// more than reviewEarlierLimit. Past a budget, a copy is reported as not
// shown, and not read.
type earlierReader struct {
	copies   *os.Root
	contents map[string]string
	shown    int
	sides    map[string]reviewSide
}

// side is the reviewed copy of the bytes a lock names, shown only when Desk
// kept a copy whose own digest is that one. The copy helps the owner read;
// the lock is the record.
func (e *earlierReader) side(digest string) reviewSide {
	if side, ok := e.sides[digest]; ok {
		return side
	}
	if e.sides == nil {
		e.sides = map[string]reviewSide{}
	}
	side := e.read(digest)
	e.sides[digest] = side
	return side
}

func (e *earlierReader) read(digest string) reviewSide {
	name, ok := copyName(digest)
	if !ok || e.copies == nil {
		return reviewSide{State: "no-copy"}
	}
	if _, ok := e.contents[digest]; ok {
		return reviewSide{State: "text", Digest: digest}
	}
	limit := reviewTextLimit
	if remaining := reviewEarlierLimit - e.shown; remaining < limit {
		limit = remaining
	}
	info, err := e.copies.Lstat(name)
	if err != nil {
		return reviewSide{State: "no-copy"}
	}
	if info.Size() > int64(limit) {
		return reviewSide{State: "not-shown"}
	}
	data, err := readPrivateData(e.copies, name, limit)
	if codeOf(err) == CodeTooLarge {
		return reviewSide{State: "not-shown"}
	}
	if err != nil || sha256Digest(data) != digest || !utf8.Valid(data) {
		return reviewSide{State: "no-copy"}
	}
	e.shown += len(data)
	e.contents[digest] = string(data)
	return reviewSide{State: "text", Digest: digest}
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

// reviewedCopies opens `.desk-private/reviewed` through the project's root,
// one component at a time. Each must be a directory, not a link, owned by
// this user and open to no one else; one that is not is refused rather than
// trusted with copies or believed when read. With create, a missing
// directory is made owner-only, and a new copies folder ignores itself in
// Git: in a project Desk did not make, `.desk-private/` may not be ignored
// yet, and these copies duplicate the project's own files.
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
		if err == nil && info.Mode().Perm()&0o077 != 0 {
			err = fmt.Errorf("%s is open to other users (%v), so Desk does not keep copies in it", part, info.Mode().Perm())
		}
		if err == nil {
			err = ownedByUs(part, info)
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
func (s *Server) storeReviewedCopies(snap *reviewSnapshot) error {
	copies, err := s.reviewedCopies(true)
	if err != nil {
		return err
	}
	defer copies.Close()
	digests := []string{snap.set.Config}
	for _, entry := range snap.set.Entries {
		digests = append(digests, entry.Digest)
	}
	for _, digest := range digests {
		name, ok := copyName(digest)
		data, have := snap.bytesOf(digest)
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

// handleReviewLock answers `POST /api/review/lock`: lock exactly what the
// review the token names showed, or nothing.
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
		Token string `json:"token"`
	}
	data, err := readBounded(r.Body, reviewConfirmLimit)
	if err != nil || decodeDataJSON(data, &request) != nil || len(request.Token) != 64 {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, "Confirm the review with the token it gave.")
		return
	}
	dir, refusal := s.reviewRuntime()
	if refusal != "" {
		writeJSONCoded(w, http.StatusConflict, CodeBadRequest, refusal)
		return
	}
	s.reviewMu.Lock()
	answer, failure := s.lockConfirmed(r.Context(), dir, request.Token)
	s.reviewMu.Unlock()
	if failure != nil {
		writeJSONCoded(w, failure.status, failure.code, failure.message)
		return
	}
	writeJSON(w, http.StatusOK, answer)
}

// lockConfirmed locks what the review the token names showed, or puts the
// previous lock back.
func (s *Server) lockConfirmed(ctx context.Context, dir heldDir, token string) (any, *lockFailure) {
	snap, err := s.readSnapshot()
	if err != nil || !hmac.Equal([]byte(s.reviewToken(snap)), []byte(token)) {
		return nil, &lockFailure{http.StatusConflict, CodeStale, "The project changed after you reviewed it, so nothing was locked. Review it again."}
	}
	lockErr := lockRuntimeProjectAt(ctx, s.cfg.JpackBin, dir)
	locked := false
	if lockErr == nil {
		if after, err := s.readReviewFile(runtimeLockName); err == nil {
			if pinned, _, err := lockedSet(after); err == nil && pinned.equal(snap.set) {
				locked = true
			}
		}
	}
	if !locked {
		restoreErr := s.restoreLock(snap.lock, snap.hasLock)
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
		Files         int    `json:"files"`
		Copies        string `json:"copies"`
		CopiesProblem string `json:"copiesProblem,omitempty"`
	}{Files: 1 + len(snap.set.Entries), Copies: "stored"}
	if err := s.storeReviewedCopies(snap); err != nil {
		s.log.Printf("desk: the reviewed copies were not stored: %v", err)
		result.Copies, result.CopiesProblem = "not-stored", err.Error()
	}
	return result, nil
}

// restoreLock puts the previous lock's bytes back, or removes the lock where
// there was none, under the file API's rules for the path.
func (s *Server) restoreLock(previous []byte, hadLock bool) error {
	if err := s.refuseSymlinkedPath(runtimeLockName); err != nil {
		return err
	}
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
