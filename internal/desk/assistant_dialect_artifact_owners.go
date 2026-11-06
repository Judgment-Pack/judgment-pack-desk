package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Inactive D5B1b native producer. No factory, route or startup path calls enrollment.
// These local records provide storage provenance, never execution authority.
// Every future caller must also integrate owner resolution and project/home
// move recovery before it can publish a D5 intent.
const (
	dialectArtifactOwnersFolder   = "dialect-artifact-owners"
	dialectArtifactOwnerMarker    = "dialect-artifact-owner.json"
	dialectArtifactOwnerPathBytes = 4096
	// Two base64 paths: 2 * (4 * ceil(4096/3)) = 10928 bytes. The
	// remaining 353 bytes are fixed JSON labels, two 64-byte IDs and two
	// identities of at most 20 decimal digits + ':' + 20 decimal digits.
	dialectArtifactOwnerRecordBytes = 11281
	dialectArtifactOwnerScopeBytes  = 231 // native:183; import:231
	dialectArtifactOwnerMarkerBytes = 88
)

var errDialectArtifactOwner = errors.New("retained Dialect owner enrollment is unavailable or changed")

type dialectArtifactWorkOwner struct {
	Kind           string `json:"kind"`
	ID             string `json:"id"`
	PathBytes      []byte `json:"pathBytes"`
	FolderIdentity string `json:"folderIdentity"`
}

// Paths are byte strings. JSON string encoding could discard an accepted
// non-UTF-8 Unix pathname or expand its control characters sixfold.
type dialectArtifactOwnerRecord struct {
	Version        int                      `json:"version"`
	Owner          string                   `json:"owner"`
	Kind           string                   `json:"kind"`
	PathBytes      []byte                   `json:"pathBytes"`
	FolderIdentity string                   `json:"folderIdentity"`
	Work           dialectArtifactWorkOwner `json:"work"`
}

type dialectArtifactOwnerScope struct {
	Version   int    `json:"version"`
	Kind      string `json:"kind"`
	Owner     string `json:"owner"`
	Namespace string `json:"namespace"`
	RunID     string `json:"runId,omitempty"`
}

type dialectArtifactOwnerMarkerRecord struct {
	Version int    `json:"version"`
	Owner   string `json:"owner"`
}

// A snapshot of acknowledged local provenance, not a bearer capability. It
// cannot dispatch, publish objects, locate another root or survive a move by
// itself. There is intentionally no exported constructor or general resolver.
type dialectArtifactOwnerEnrollment struct {
	Owner             string
	Namespace         string
	OwnerSHA256       string
	LegacyOwnerSHA256 string
	HomeAnchor        string
	HomeSHA256        string
	ScopeSHA256       string
	ProjectIdentity   string
	WorkIdentity      string
	WorkDomain        string
	WorkSHA256        string
}

func dialectArtifactOwnerPath(raw []byte) bool {
	return len(raw) > 0 && len(raw) <= dialectArtifactOwnerPathBytes &&
		bytes.IndexByte(raw, 0) < 0 && filepath.IsAbs(string(raw))
}

// identityKey formats native Stat_t fields with %d. Some Unix targets use
// signed device fields; the signing identity regex is not that full domain.
// A signed or unsigned 64-bit decimal still occupies at most 20 bytes.
func dialectArtifactOwnerFileIdentity(v string) bool {
	parts := strings.Split(v, ":")
	if len(parts) != 2 {
		return false
	}
	for _, part := range parts {
		if len(part) == 0 || len(part) > 20 {
			return false
		}
		if n, err := strconv.ParseInt(part, 10, 64); err == nil && strconv.FormatInt(n, 10) == part {
			continue
		}
		if n, err := strconv.ParseUint(part, 10, 64); err == nil && strconv.FormatUint(n, 10) == part {
			continue
		}
		return false
	}
	return true
}

func (v dialectArtifactOwnerRecord) valid() bool {
	return v.Version == 1 && assistantRunDigest(v.Owner) && v.Kind == "file" &&
		dialectArtifactOwnerPath(v.PathBytes) && dialectArtifactOwnerFileIdentity(v.FolderIdentity) &&
		v.Work.Kind == "settings" && assistantRunDigest(v.Work.ID) &&
		dialectArtifactOwnerPath(v.Work.PathBytes) && dialectArtifactOwnerFileIdentity(v.Work.FolderIdentity)
}

func (v dialectArtifactOwnerScope) valid() bool {
	if v.Version != 1 || !assistantRunDigest(v.Owner) || !assistantRunDigest(v.Namespace) {
		return false
	}
	if v.Kind == "native" {
		return v.RunID == ""
	}
	return v.Kind == "import" && len(v.RunID) == 37 && strings.HasPrefix(v.RunID, "arun_") &&
		assistantRunDigest(v.RunID[5:]+v.RunID[5:])
}

// Exact compact producer encoding also closes duplicate keys, escaped aliases,
// encoding/json's case aliases, null/omitted controls and noncanonical base64.
func readDialectArtifactOwnerJSON(raw []byte, limit int, out any) error {
	if len(raw) == 0 || len(raw) > limit || decodeDataJSON(raw, out) != nil {
		return errDialectArtifactOwner
	}
	canonical, err := json.Marshal(out)
	if err != nil || !bytes.Equal(raw, canonical) {
		return errDialectArtifactOwner
	}
	return nil
}

func readDialectArtifactOwnerRecord(raw []byte) (dialectArtifactOwnerRecord, error) {
	var v dialectArtifactOwnerRecord
	err := readDialectArtifactOwnerJSON(raw, dialectArtifactOwnerRecordBytes, &v)
	if err == nil && !v.valid() {
		err = errDialectArtifactOwner
	}
	return v, err
}

func readDialectArtifactOwnerScope(raw []byte) (dialectArtifactOwnerScope, error) {
	var v dialectArtifactOwnerScope
	err := readDialectArtifactOwnerJSON(raw, dialectArtifactOwnerScopeBytes, &v)
	if err == nil && !v.valid() {
		err = errDialectArtifactOwner
	}
	return v, err
}

func readDialectArtifactOwnerMarker(raw []byte) (dialectArtifactOwnerMarkerRecord, error) {
	var v dialectArtifactOwnerMarkerRecord
	err := readDialectArtifactOwnerJSON(raw, dialectArtifactOwnerMarkerBytes, &v)
	if err == nil && (v.Version != 1 || !assistantRunDigest(v.Owner)) {
		err = errDialectArtifactOwner
	}
	return v, err
}

// Nil in production. Tests inject failures at real filesystem boundaries;
// they do not replace the producer, its roots, namespace or authorization.
var testHookDialectArtifactOwnerStep func(string) error

func dialectArtifactOwnerStep(ctx context.Context, step string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if testHookDialectArtifactOwnerStep != nil {
		if err := testHookDialectArtifactOwnerStep(step); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func closeDialectArtifactOwner(ctx context.Context, label string, close func() error) error {
	return errors.Join(close(), dialectArtifactOwnerStep(ctx, label+".close"))
}

func syncDialectArtifactOwnerDirectory(root *os.Root) (err error) {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, dir.Close()) }()
	return dir.Sync()
}

// Keep held-file custody identical for reads and existing-file durability
// acknowledgments. An exact byte match never repairs a mismatched entry.
func openDialectArtifactOwnerFile(root *os.Root, name string) (_ *os.File, err error) {
	entry, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if err = ownerOnlyFile(name, entry.Mode()); err != nil {
		return nil, err
	}
	if err = ownedByUs(name, entry); err != nil {
		return nil, err
	}
	file, err := root.OpenFile(name, os.O_RDONLY|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, file.Close())
		}
	}()
	held, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(entry, held) || ownerOnlyFile(name, held.Mode()) != nil || ownedByUs(name, held) != nil {
		return nil, errDialectArtifactOwner
	}
	return file, nil
}

// Metadata read acknowledgment includes the descriptor's Close result.
func readDialectArtifactOwnerFile(root *os.Root, name string, limit int) (raw []byte, err error) {
	file, err := openDialectArtifactOwnerFile(root, name)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, file.Close())
		if err != nil {
			raw = nil
		}
	}()
	return readBounded(file, limit)
}

// A previous failed call may have left an exact visible entry whose durability
// was not acknowledged. Sync only a held exact file; never rewrite its bytes.
func syncExistingDialectArtifactOwner(ctx context.Context, root *os.Root, name string, raw []byte, label string) (err error) {
	file, err := openDialectArtifactOwnerFile(root, name)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			err = errors.Join(err, file.Close())
		}
	}()
	prior, err := readBounded(file, len(raw))
	if err != nil {
		return err
	}
	if !bytes.Equal(prior, raw) {
		return errDialectArtifactOwner
	}
	held, err := file.Stat()
	if err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = dialectArtifactOwnerStep(ctx, label+".sync"); err != nil {
		return err
	}
	entry, err := root.Lstat(name)
	if err != nil || !os.SameFile(held, entry) {
		return errors.Join(errDialectArtifactOwner, err)
	}
	closed = true
	return errors.Join(file.Close(), dialectArtifactOwnerStep(ctx, label+".file-close"))
}

// Open only directly selected components. Neither this table nor its shards
// are enumerated, and no lifetime count or aggregate-byte quota is imposed.
func openDialectArtifactOwnerFolder(ctx context.Context, start *os.Root, create bool, label string, parts ...string) (_ *os.Root, err error) {
	current, err := start.OpenRoot(".")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, current.Close())
		}
	}()
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, "/\\\x00") {
			return nil, errDialectArtifactOwner
		}
		info, lookErr := current.Lstat(part)
		if errors.Is(lookErr, os.ErrNotExist) && create {
			observed, made, createErr := storageCooperatingDirectory(ctx, current, part, nil)
			if createErr != nil {
				return nil, createErr
			}
			closeErr := observed.Close()
			if made {
				createErr = dialectArtifactOwnerStep(ctx, label+".directory-publish")
			}
			lookErr = errors.Join(createErr, closeErr)
			if lookErr == nil {
				info, lookErr = current.Lstat(part)
			}
		}
		if lookErr != nil {
			return nil, lookErr
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 || ownedByUs(part, info) != nil {
			return nil, errDialectArtifactOwner
		}
		next, openErr := current.OpenRoot(part)
		if openErr != nil {
			return nil, openErr
		}
		held, statErr := next.Stat(".")
		if statErr != nil || !os.SameFile(info, held) {
			return nil, errors.Join(errDialectArtifactOwner, statErr, next.Close())
		}
		// Re-entry must establish every selected edge again, even when a
		// prior mkdir was visible before its parent sync failed.
		if syncErr := syncDialectArtifactOwnerDirectory(current); syncErr != nil {
			return nil, errors.Join(syncErr, next.Close())
		}
		if stepErr := dialectArtifactOwnerStep(ctx, label+".directory-sync"); stepErr != nil {
			return nil, errors.Join(stepErr, next.Close())
		}
		entry, entryErr := current.Lstat(part)
		if entryErr != nil || !os.SameFile(held, entry) {
			return nil, errors.Join(errDialectArtifactOwner, entryErr, next.Close())
		}
		prior := current
		current = next
		if closeErr := prior.Close(); closeErr != nil {
			return nil, closeErr
		}
	}
	return current, nil
}

func lockDialectArtifactOwner(root *os.Root, name string) (_ *os.File, err error) {
	file, err := root.OpenFile(name, os.O_RDWR|os.O_CREATE|openNoFollow|openNonBlocking, custodyFileMode)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, file.Close())
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	entry, err := root.Lstat(name)
	if err != nil || !os.SameFile(info, entry) || info.Size() != 0 ||
		ownerOnlyFile(name, info.Mode()) != nil || ownedByUs(name, info) != nil {
		return nil, errors.Join(errDialectArtifactOwner, err)
	}
	if err = lockPrivateData(file, true); err != nil {
		return nil, err
	}
	return file, nil
}

// Claim immutable metadata once, without replacing an existing name. A failed
// acknowledgment may leave a complete unused row, but never repairs corruption.
func claimDialectArtifactOwner(ctx context.Context, root *os.Root, name string, raw []byte, label string, check func() error) (err error) {
	prior, err := readDialectArtifactOwnerFile(root, name, len(raw))
	if err == nil {
		if !bytes.Equal(prior, raw) {
			return errDialectArtifactOwner
		}
		if err = check(); err != nil {
			return err
		}
		if err = syncExistingDialectArtifactOwner(ctx, root, name, raw, label); err != nil {
			return err
		}
		return acknowledgeDialectArtifactOwner(ctx, root, name, raw, label, check)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err = check(); err != nil {
		return err
	}
	file, stage, err := newDataStage(root)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			err = errors.Join(err, file.Close())
		}
		if removeErr := root.Remove(stage); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, removeErr)
		}
	}()
	if _, err = file.Write(raw); err != nil {
		return err
	}
	if err = dialectArtifactOwnerStep(ctx, label+".write"); err != nil {
		return err
	}
	if err = file.Chmod(custodyFileMode); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = dialectArtifactOwnerStep(ctx, label+".sync"); err != nil {
		return err
	}
	closed = true
	if err = errors.Join(file.Close(), dialectArtifactOwnerStep(ctx, label+".file-close")); err != nil {
		return err
	}
	if err = check(); err != nil {
		return err
	}
	if err = root.Link(stage, name); err != nil {
		return err
	}
	if err = dialectArtifactOwnerStep(ctx, label+".publish"); err != nil {
		return err
	}
	return acknowledgeDialectArtifactOwner(ctx, root, name, raw, label, check)
}

func acknowledgeDialectArtifactOwner(ctx context.Context, root *os.Root, name string, raw []byte, label string, check func() error) (err error) {
	if err = syncDialectArtifactOwnerDirectory(root); err != nil {
		return err
	}
	if err = dialectArtifactOwnerStep(ctx, label+".directory-sync"); err != nil {
		return err
	}
	read, err := readDialectArtifactOwnerFile(root, name, len(raw))
	if err != nil {
		return err
	}
	if !bytes.Equal(read, raw) {
		return errDialectArtifactOwner
	}
	if err = dialectArtifactOwnerStep(ctx, label+".readback"); err != nil {
		return err
	}
	return check()
}

// enrollDialectArtifactOwner derives every locator from this Server's actual
// pinned owners. It acquires its own custody; it must not be called from an
// already-held run/work/private transaction. All acquisitions after work are
// try-only, so a future caller cannot create a private/metadata wait cycle.
func (s *Server) enrollDialectArtifactOwner(r *http.Request) (dialectArtifactOwnerEnrollment, error) {
	return s.withDialectArtifactOwner(r, nil)
}

// The optional use remains inside real lexical custody. It is the current
// native-scope witness, never historical/archive authorization.
func (s *Server) withDialectArtifactOwner(r *http.Request, use *dialectArtifactInjectedUse) (out dialectArtifactOwnerEnrollment, err error) {
	if s == nil || r == nil || s.project == nil || s.project.own == nil ||
		s.root == nil || s.root != s.project.own.root || s.assistant == nil || !s.assistant.usable() ||
		s.writes == nil || s.sessions == nil {
		return out, errDialectArtifactOwner
	}
	ctx := r.Context()
	registration, operation, err := s.prepareInjectedArtifactEnrollment(ctx)
	if err != nil {
		return out, err
	}
	if use != nil && registration == nil {
		return out, errRetainedArtifactAuthorityUnavailable
	}
	if operation != nil {
		defer func() {
			err = errors.Join(err, operation.finish())
			if err != nil {
				out = dialectArtifactOwnerEnrollment{}
			}
		}()
	}
	project, assistant, projectRoot := s.project, s.assistant, s.root
	checkPhysical := func() error {
		if registration != nil && (s.project != project || s.assistant != assistant || s.root != projectRoot || s.root != project.own.root) {
			return errDialectArtifactOwner
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !sameDirectory(s.projectDir, s.project.info) {
			return errDialectArtifactOwner
		}
		return nil
	}
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.authorizeAssistantRunStorage(r); err != nil {
			return err
		}
		return checkPhysical()
	}
	defer func() {
		err = errors.Join(err, check())
		if err != nil {
			out = dialectArtifactOwnerEnrollment{}
		}
	}()
	if err = check(); err != nil {
		return out, err
	}
	var heldWorkInfo, heldWorkRoot os.FileInfo
	checkWorkPins := func() error {
		if heldWorkInfo != nil {
			entry, err := s.assistant.root.Lstat(".data-work.lock")
			if err != nil || !os.SameFile(entry, heldWorkInfo) || !sameDirectory(s.configDir, heldWorkRoot) {
				return errors.Join(errDialectArtifactOwner, err)
			}
		}
		return nil
	}
	leafOuter := func() error {
		if err := checkPhysical(); err != nil {
			return err
		}
		return checkWorkPins()
	}
	checkOuter := func() error {
		if err := check(); err != nil {
			return err
		}
		return checkWorkPins()
	}
	return s.withDialectArtifactOwnerOuter(ctx, checkOuter, leafOuter, func(owner *Server, home *dialectArtifactHomeHold, outerCheck, outerLeaf func() error) (out dialectArtifactOwnerEnrollment, err error) {
		work, err := s.openPrivateDataFileLock(ctx, ".data-work.lock", true, false)
		if err != nil {
			return out, err
		}
		defer func() { err = errors.Join(err, closeDialectArtifactOwner(ctx, "work", work.Close)) }()
		workInfo, err := work.Stat()
		if err != nil {
			return out, err
		}
		workRoot, err := s.assistant.root.Stat(".")
		if err != nil {
			return out, err
		}
		heldWorkInfo, heldWorkRoot = workInfo, workRoot
		checkPins := outerCheck
		if !s.writes.TryLock() {
			return out, errDialectArtifactOwner
		}
		defer s.writes.Unlock()
		private, err := s.tryPrivateDataLockContext(ctx, true)
		if err != nil {
			return out, err
		}
		defer func() { err = errors.Join(err, closeDialectArtifactOwner(ctx, "private", private.Close)) }()
		return s.enrollDialectArtifactOwnerLocked(ctx, owner, home, work, checkPins, outerLeaf, nil, r, registration, operation, use)
	})
}

// Lexical outer custody is shared by the standalone EX-work producer and the
// inactive live SH-work producer. The callback cannot outlive these real holds.
// The acquisition half contains the original nonblocking outer custody and
// physical home checks. It performs no authorization or enrollment callback.
// Complete-read short admission may release this hold before coordinating a
// canonical/writer mutex conflict; a failed check or Close is always terminal.
type dialectArtifactOwnerOuterHold struct {
	s        *Server
	owner    *Server
	home     *dialectArtifactHomeHold
	releases []func()
	closed   bool
	closeErr error
}

func (s *Server) acquireDialectArtifactOwnerOuter(ctx context.Context) (_ *dialectArtifactOwnerOuterHold, err error) {
	g := &dialectArtifactOwnerOuterHold{s: s}
	defer func() {
		if err != nil {
			err = errors.Join(err, g.Close())
		}
	}()
	owner := s.deskOwner()
	if owner == nil || owner.assistant == nil || !owner.assistant.usable() || owner.assistant.secrets == nil || !owner.desksMu.TryLock() {
		return nil, errDialectArtifactOwner
	}
	g.owner = owner
	g.releases = append(g.releases, owner.desksMu.Unlock)
	if owner.desksClosed || owner.homeMove.queued.Load() || !s.location.use.TryRLock() {
		return nil, errDialectArtifactOwner
	}
	g.releases = append(g.releases, s.location.use.RUnlock)
	if !s.location.mu.TryLock() {
		return nil, errDialectArtifactOwner
	}
	moved := s.location.moved != ""
	s.location.mu.Unlock()
	if moved || s.location.moving.Load() {
		return nil, errDialectArtifactOwner
	}
	g.home, err = s.holdDialectArtifactHome(ctx)
	if err != nil {
		return nil, err
	}
	return g, nil
}
func (g *dialectArtifactOwnerOuterHold) check() error {
	if g == nil || g.closed || g.owner.desksClosed || g.owner.homeMove.queued.Load() || g.s.location.moving.Load() {
		return errDialectArtifactOwner
	}
	if g.home != nil {
		return g.home.check()
	}
	return nil
}
func (g *dialectArtifactOwnerOuterHold) Close() error {
	if g == nil {
		return nil
	}
	if g.closed {
		return g.closeErr
	}
	g.closed = true
	if g.home != nil {
		g.closeErr = errors.Join(g.closeErr, g.home.close())
	}
	for i := len(g.releases) - 1; i >= 0; i-- {
		g.releases[i]()
	}
	g.releases = nil
	return g.closeErr
}
func (s *Server) withDialectArtifactOwnerOuter(ctx context.Context, check, physical func() error, use func(*Server, *dialectArtifactHomeHold, func() error, func() error) (dialectArtifactOwnerEnrollment, error)) (out dialectArtifactOwnerEnrollment, err error) {
	defer func() {
		if err != nil {
			out = dialectArtifactOwnerEnrollment{}
		}
	}()
	g, err := s.acquireDialectArtifactOwnerOuter(ctx)
	if err != nil {
		return out, err
	}
	defer func() { err = errors.Join(err, g.Close()) }()
	return g.use(check, physical, use)
}

// Both producers share exactly these original initial/final checks. Admission
// calls this only once, after its full prefix has transferred real custody.
func (g *dialectArtifactOwnerOuterHold) use(check, physical func() error, use func(*Server, *dialectArtifactHomeHold, func() error, func() error) (dialectArtifactOwnerEnrollment, error)) (out dialectArtifactOwnerEnrollment, err error) {
	defer func() {
		if err != nil {
			out = dialectArtifactOwnerEnrollment{}
		}
	}()
	checkPins := func() error {
		if err := check(); err != nil {
			return err
		}
		return g.check()
	}
	defer func() { err = errors.Join(err, checkPins()) }()
	if err = checkPins(); err != nil {
		return out, err
	}
	leafPins := func() error {
		if err := physical(); err != nil {
			return err
		}
		return g.check()
	}
	return use(g.owner, g.home, checkPins, leafPins)
}

// Both callers own real work/writes/private custody for this entire leaf.
// It owns only the descriptors it opens; work belongs to its lexical caller.
func (s *Server) enrollDialectArtifactOwnerLocked(ctx context.Context, owner *Server, home *dialectArtifactHomeHold, work *os.File, checkPins, physical func() error, validateData func(*chatDataStore) error, request *http.Request, registration *retainedArtifactProviderRegistration, operation *retainedArtifactInjectedOperation, use *dialectArtifactInjectedUse) (out dialectArtifactOwnerEnrollment, err error) {
	defer func() {
		if err != nil {
			out = dialectArtifactOwnerEnrollment{}
		}
	}()
	if err = checkPins(); err != nil {
		return out, err
	}
	data, err := s.openChatData()
	if err != nil {
		return out, err
	}
	defer func() { err = errors.Join(err, closeDialectArtifactOwner(ctx, "data", data.root.Close)) }()
	store := s.assistantRunsLocked(data.root)
	if store.problem != nil || !assistantRunDigest(store.project) {
		return out, errors.Join(errDialectArtifactOwner, store.problem)
	}
	if validateData != nil {
		if err = validateData(data); err != nil {
			return out, err
		}
		outerCheck := checkPins
		checkPins = func() error {
			if err := outerCheck(); err != nil {
				return err
			}
			return validateData(data)
		}
	}
	projectInfo, err := s.root.Stat(".")
	if err != nil {
		return out, err
	}
	workInfo, err := s.assistant.root.Stat(".")
	if err != nil {
		return out, err
	}
	workFileInfo, err := work.Stat()
	if err != nil {
		return out, err
	}
	workEntry, err := s.assistant.root.Lstat(".data-work.lock")
	if err != nil || !os.SameFile(workFileInfo, workEntry) || !sameDirectory(s.configDir, workInfo) {
		return out, errors.Join(errDialectArtifactOwner, err)
	}
	projectIdentity, workIdentity := identityKey(projectInfo), identityKey(workInfo)
	workFileIdentity := identityKey(workFileInfo)
	if !dialectArtifactOwnerFileIdentity(projectIdentity) || !dialectArtifactOwnerFileIdentity(workIdentity) || !dialectArtifactOwnerFileIdentity(workFileIdentity) {
		return out, errDialectArtifactOwner
	}
	// The inode is retained in Work.ID, so exclusion alone is insufficient:
	// establish the held file and its parent before persisting that identity.
	// This stays local to enrollment; the shared lock opener only excludes.
	if err = work.Sync(); err != nil {
		return out, err
	}
	if err = dialectArtifactOwnerStep(ctx, "work.sync"); err != nil {
		return out, err
	}
	if err = syncDialectArtifactOwnerDirectory(s.assistant.root); err != nil {
		return out, err
	}
	if err = dialectArtifactOwnerStep(ctx, "work.directory-sync"); err != nil {
		return out, err
	}
	workEntry, err = s.assistant.root.Lstat(".data-work.lock")
	if err != nil || !os.SameFile(workFileInfo, workEntry) || !sameDirectory(s.configDir, workInfo) {
		return out, errors.Join(errDialectArtifactOwner, err)
	}
	projectPrivate, err := openDialectArtifactOwnerFolder(ctx, s.root, true, "project-folder", ".desk-private")
	if err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, closeDialectArtifactOwner(ctx, "project-private", projectPrivate.Close))
	}()
	markerLock, err := lockDialectArtifactOwner(projectPrivate, ".dialect-artifact-owner.lock")
	if err != nil {
		return out, err
	}
	defer func() { err = errors.Join(err, closeDialectArtifactOwner(ctx, "marker-lock", markerLock.Close)) }()
	markerNew := false
	markerRaw, err := readDialectArtifactOwnerFile(projectPrivate, dialectArtifactOwnerMarker, dialectArtifactOwnerMarkerBytes)
	if errors.Is(err, os.ErrNotExist) {
		markerRaw, err = json.Marshal(dialectArtifactOwnerMarkerRecord{Version: 1, Owner: randomStartupID()})
		markerNew = err == nil
	}
	if err != nil {
		return out, err
	}
	marker, err := readDialectArtifactOwnerMarker(markerRaw)
	if err != nil {
		return out, err
	}
	workRaw, err := json.Marshal([]string{"dialect-artifact-work-v1", workIdentity, workFileIdentity})
	if err != nil {
		return out, err
	}
	record := dialectArtifactOwnerRecord{Version: 1, Owner: marker.Owner, Kind: "file", PathBytes: []byte(s.projectDir), FolderIdentity: projectIdentity,
		Work: dialectArtifactWorkOwner{Kind: "settings", ID: digestOf(workRaw), PathBytes: []byte(s.configDir), FolderIdentity: workIdentity}}
	if !record.valid() {
		return out, errDialectArtifactOwner
	}
	ownerRaw, err := json.Marshal(record)
	if registration != nil {
		ownerRaw, err = json.Marshal(dialectArtifactInjectedOwnerRecord{Version: 2, Owner: marker.Owner, Kind: "injected", Provider: registration.provider.id, ProjectFolderIdentity: projectIdentity, WorkDomain: record.Work.ID})
	}
	if err != nil || len(ownerRaw) > dialectArtifactOwnerRecordBytes {
		return out, errors.Join(errDialectArtifactOwner, err)
	}
	metadata, err := openDialectArtifactOwnerFolder(ctx, owner.assistant.secrets, markerNew, "metadata-folder", dialectArtifactOwnersFolder)
	if err != nil {
		return out, err
	}
	defer func() { err = errors.Join(err, closeDialectArtifactOwner(ctx, "metadata", metadata.Close)) }()
	rows, err := openDialectArtifactOwnerFolder(ctx, metadata, markerNew, "owner-folder", "owners", marker.Owner[:2])
	if err != nil {
		return out, err
	}
	defer func() { err = errors.Join(err, closeDialectArtifactOwner(ctx, "owner-folder", rows.Close)) }()
	ownerName := marker.Owner[2:] + ".json"
	currentOwnerRaw := ownerRaw
	if !markerNew {
		// An orphan/copied marker is not permission to recreate a missing
		// locator. Recovery of a marker-only interruption needs an explicit
		// identity-bound recovery design, not an automatic new-root binding.
		prior, readErr := readDialectArtifactOwnerFile(rows, ownerName, dialectArtifactOwnerRecordBytes)
		if registration != nil {
			injected, decodeErr := readDialectArtifactInjectedOwnerRecord(prior)
			if readErr != nil || decodeErr != nil || !bytes.Equal(prior, ownerRaw) || injected.Owner != marker.Owner {
				return out, errors.Join(errDialectArtifactOwner, readErr, decodeErr)
			}
		} else {
			legacy, decodeErr := readDialectArtifactOwnerRecord(prior)
			if readErr != nil || decodeErr != nil || legacy.Owner != marker.Owner || legacy.FolderIdentity != projectIdentity ||
				legacy.Work.ID != record.Work.ID || legacy.Work.FolderIdentity != workIdentity {
				return out, errors.Join(errDialectArtifactOwner, readErr, decodeErr)
			}
		}
		ownerRaw = prior // immutable original bytes, including historical paths
	}
	locks, err := openDialectArtifactOwnerFolder(ctx, metadata, true, "lock-folder", "locks", marker.Owner[:2])
	if err != nil {
		return out, err
	}
	defer func() { err = errors.Join(err, closeDialectArtifactOwner(ctx, "lock-folder", locks.Close)) }()
	ownerLock, err := lockDialectArtifactOwner(locks, marker.Owner[2:]+".lock")
	if err != nil {
		return out, err
	}
	defer func() { err = errors.Join(err, closeDialectArtifactOwner(ctx, "owner-lock", ownerLock.Close)) }()
	if !markerNew {
		// Keep the early missing/corrupt refusal, then bind the same immutable
		// snapshot again under the actual per-owner exclusion before new rows.
		prior, readErr := readDialectArtifactOwnerFile(rows, ownerName, dialectArtifactOwnerRecordBytes)
		if readErr != nil || !bytes.Equal(prior, ownerRaw) {
			return out, errors.Join(errDialectArtifactOwner, readErr)
		}
	}
	if registration != nil {
		return s.enrollDialectArtifactInjectedOwnerRowsLocked(ctx, owner, home, work, data, physical, checkPins, request, registration, operation, use,
			dialectArtifactInjectedOwnerRows{projectPrivate: projectPrivate, metadata: metadata, rows: rows, locks: locks, markerLock: markerLock, ownerLock: ownerLock, marker: marker, markerRaw: markerRaw, ownerRaw: ownerRaw, markerNew: markerNew,
				projectIdentity: projectIdentity, workIdentity: workIdentity, workFileInfo: workFileInfo, workInfo: workInfo, workDomain: record.Work.ID, namespace: store.project})
	}
	if home != nil {
		if err = home.namespace(data.root, store.project); err != nil {
			return out, err
		}
	}
	location, err := s.dialectArtifactActualLocation(ctx, record.Work.ID, home)
	if err != nil {
		return out, err
	}
	locationRaw, err := marshalDialectArtifactLocation(dialectArtifactLocationRecord{Version: 2, Owner: marker.Owner, Kind: "file",
		LegacySHA256: digestOf(ownerRaw), FolderIdentity: projectIdentity, Location: location, WorkDomain: record.Work.ID})
	if err != nil {
		return out, err
	}
	// Preflight all selected existing metadata before publishing any new row.
	createLocation, err := preflightDialectArtifactLocation(ctx, metadata, marker.Owner, locationRaw, ownerRaw, currentOwnerRaw)
	if err != nil {
		return out, err
	}
	workDomainRaw, err := s.publishDialectArtifactWorkDomain(ctx, metadata, work, checkPins)
	if err != nil {
		return out, err
	}
	var homeRaw []byte
	if home != nil {
		homeRaw, err = home.publish(metadata, checkPins)
		if err != nil {
			return out, err
		}
	}
	if err = claimDialectArtifactOwner(ctx, rows, ownerName, ownerRaw, "owner", checkPins); err != nil {
		return out, err
	}
	if err = publishDialectArtifactLocation(ctx, metadata, locationRaw, createLocation, checkPins); err != nil {
		return out, err
	}
	// A fresh locator is durable and exact before its marker is published.
	// An interruption before that publication leaves only an unreferenced
	// owner row, never a marker that normally needs identity reconstruction.
	if markerNew {
		if err = dialectArtifactOwnerStep(ctx, "before-marker"); err != nil {
			return out, err
		}
	}
	// Existing markers need the same durability acknowledgment as a fresh
	// publication, after their exact owner row has been verified above.
	if err = claimDialectArtifactOwner(ctx, projectPrivate, dialectArtifactOwnerMarker, markerRaw, "marker", checkPins); err != nil {
		return out, err
	}
	scope := dialectArtifactOwnerScope{Version: 1, Kind: "native", Owner: marker.Owner, Namespace: store.project}
	scopeRaw, err := json.Marshal(scope)
	if err != nil {
		return out, err
	}
	scopes, err := openDialectArtifactOwnerFolder(ctx, metadata, true, "scope-folder", "native", store.project)
	if err != nil {
		return out, err
	}
	defer func() { err = errors.Join(err, closeDialectArtifactOwner(ctx, "scope-folder", scopes.Close)) }()
	if err = claimDialectArtifactOwner(ctx, scopes, marker.Owner+".json", scopeRaw, "scope", checkPins); err != nil {
		return out, err
	}
	// Reopen only the selected names from their original anchored roots. A
	// renamed table branch cannot turn writes into an acknowledged enrollment.
	if err = s.readbackDialectArtifactOwner(ctx, markerRaw, ownerRaw, scopeRaw, workDomainRaw, locationRaw, homeRaw, marker.Owner, store.project); err != nil {
		return out, err
	}
	if err = checkPrivateDataPath(data); err != nil {
		return out, err
	}
	if err = dialectArtifactOwnerStep(ctx, "acknowledge"); err != nil {
		return out, err
	}
	if !sameDirectory(s.configDir, workInfo) {
		return out, errDialectArtifactOwner
	}
	workEntry, err = s.assistant.root.Lstat(".data-work.lock")
	if err != nil || !os.SameFile(workFileInfo, workEntry) {
		return out, errors.Join(errDialectArtifactOwner, err)
	}
	selected, err := s.dialectArtifactActualLocation(ctx, record.Work.ID, home)
	if err != nil || selected.Kind != location.Kind || selected.Anchor != location.Anchor || !bytes.Equal(selected.PathBytes, location.PathBytes) {
		return out, errors.Join(errDialectArtifactOwner, err)
	}
	if home != nil {
		if err = home.namespace(data.root, store.project); err != nil {
			return out, err
		}
	}
	if err = checkPins(); err != nil {
		return out, err
	}
	out = dialectArtifactOwnerEnrollment{Owner: marker.Owner, Namespace: store.project, OwnerSHA256: digestOf(locationRaw), LegacyOwnerSHA256: digestOf(ownerRaw), ScopeSHA256: digestOf(scopeRaw),
		ProjectIdentity: projectIdentity, WorkIdentity: workIdentity, WorkDomain: record.Work.ID, WorkSHA256: digestOf(workDomainRaw)}
	if home != nil {
		out.HomeAnchor, out.HomeSHA256 = home.record.ID, digestOf(homeRaw)
	}
	return out, nil
}

func (s *Server) readbackDialectArtifactOwner(ctx context.Context, marker, row, scope, work, location, home []byte, owner, namespace string) (err error) {
	if !assistantRunDigest(owner) || !assistantRunDigest(namespace) {
		return errDialectArtifactOwner
	}
	if err = dialectArtifactOwnerStep(ctx, "selected-readback"); err != nil {
		return err
	}
	readExact := func(root *os.Root, name string, expected []byte) error {
		raw, err := readDialectArtifactOwnerFile(root, name, len(expected))
		if err != nil {
			return err
		}
		if !bytes.Equal(raw, expected) {
			return errDialectArtifactOwner
		}
		return nil
	}
	project, err := openDialectArtifactOwnerFolder(ctx, s.root, false, "project-folder", ".desk-private")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, project.Close()) }()
	if err = readExact(project, dialectArtifactOwnerMarker, marker); err != nil {
		return err
	}
	metadata, err := openDialectArtifactOwnerFolder(ctx, s.deskOwner().assistant.secrets, false, "metadata-folder", dialectArtifactOwnersFolder)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, metadata.Close()) }()
	if err = readbackDialectArtifactWorkDomain(ctx, metadata, work); err != nil {
		return err
	}
	if err = readbackDialectArtifactLocation(ctx, metadata, location); err != nil {
		return err
	}
	if len(home) != 0 {
		if err = readbackDialectArtifactHomeAnchor(ctx, metadata, home); err != nil {
			return err
		}
	}
	rows, err := openDialectArtifactOwnerFolder(ctx, metadata, false, "owner-folder", "owners", owner[:2])
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	if err = readExact(rows, owner[2:]+".json", row); err != nil {
		return err
	}
	scopes, err := openDialectArtifactOwnerFolder(ctx, metadata, false, "scope-folder", "native", namespace)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, scopes.Close()) }()
	return readExact(scopes, owner+".json", scope)
}
