package desk

// The identity of the project Desk was started on (issue #283, the ADR-0010
// line audit's finding 1).
//
// # Why not its path
//
// Desk named what it keeps for the project it was started on, its signing
// key, the key's list and markers, and its stamping settings, by the hex
// SHA-256 of the project's resolved path. A move then broke every link, while
// the project's jpack.json still named the old seed; and a path reused by
// another project made the start's recovery of an unfinished creation read
// the other project's jpack.json, find no key named there, and remove the
// moved project's key.
//
// # What it is
//
// A name of 64 lowercase hexadecimal characters, kept in the project, in
// Desk's private folder, as `.desk-private/project.json`, with the resolved
// path of the folder it was written in:
//
//	{"id":"<64 hex>","path":"<the project's resolved path>"}
//
// It moves with the project. A new name is random.
//
// # A copy holds it too
//
// A copy of the project carries the file, and so the name (review round 1 of
// #296). A start whose file names another folder looks there: where that
// folder holds the same name still, two folders hold one identity, and this
// one is a copy, or the other is: Desk makes no destructive recovery under
// that name, makes and rotates no key for it here, and says so
// (`startupShared`). Where the other folder is gone, or holds another name,
// the project was moved, and the file is written again with this folder's
// path. Where it cannot be told now, the identity is taken as shared. It is not kept in `jpack-desk.json`: that file is the project's
// shared configuration, committed and the same in every clone, and its
// decoders, here and in the page, refuse the whole file for a member they do
// not know. `.desk-private/` is never committed: the upgrade adds it to
// `.gitignore`. The folder must be the user's and open to nobody else, and
// the file is read whole, in the one spelling Desk writes, or not used.
//
// Runner's state is not named by it: Runner's own store is scoped to the
// project's path by Runner itself, which binds its input root, and moving it
// is Runner's to provide (its README, "State and backups").
//
// # When it is written
//
// Once, never over anything, through a staging file linked into place:
//
//   - by the upgrade that makes the project's signing key, before the key,
//     with the name the offer showed (startup_key.go);
//   - at a start on a project whose `.desk-private/` is there and holds no
//     name, under this project's lock and the signing folder's, each taken
//     once: the name of the seed in Desk's signing folder that the project's
//     jpack.json names, where it names one Desk keeps (a project moved
//     before this identity existed keeps its key so); otherwise a new one.
//
// Where nothing is kept, a project with no `.desk-private/` is named by its
// path's hash, as before, for what Desk already keeps under it; it is
// written nothing by a start.
//
// # The migration of what was kept under the path's hash
//
// A seed is never renamed: jpack.json names it by its absolute path, and a
// renamed seed would leave the project unsigned until the owner wrote
// jpack.json again. A key under the path's hash is taken as this project's
// only where its jpack.json names it, as above. Where no key, list or marker
// is kept under the path's hash, its stamping settings, `stamping/<hash>/`,
// are renamed to the new name, journalled in the identity file itself:
//
//	{"id":"<64 hex>","from":"<64 hex>"}
//
// is written first; then the folder is renamed, never over anything; then
// the file is written again without `from`. A stop at any moment leaves
// either no name, which the next start chooses again, or a name with `from`,
// which the next start finishes. Nothing is removed on a failure.
//
// # What recovery may remove
//
// A start's sweep removes an unfinished creation's key only under this
// project's own name, read from its identity file and held by no other
// folder (`startupBound`), and only where the creation's marker binds it to
// this project and this transaction (`creationBound`, startup_key.go): the
// identity, the project's resolved path and the digest of the jpack.json the
// upgrade set out to replace, all three as found. A creation marker under the
// path's hash, left by a Desk before this one, is not bound to any project:
// where jpack.json names its seed, only the marker goes; otherwise nothing
// does.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

const (
	// startupIdentityDir and startupIdentityName are where the project's
	// name is kept, and startupIdentityLimit the most of it Desk reads: the
	// file is 146 bytes with `from`.
	startupIdentityDir   = ".desk-private"
	startupIdentityName  = "project.json"
	startupIdentityLimit = 512
	// startupIdentityKept is what Desk keeps in `.desk-private/`, as a
	// refusal to open it names it.
	startupIdentityKept = "this project's identity"
	// identityStagingPrefix names the identity file while it is written,
	// before it is linked or renamed into place.
	identityStagingPrefix = ".project-json-"
)

// startupIDForm is a project's name: 64 lowercase hexadecimal characters, the
// form the path's hash has, and never a desk's id, which has 32.
var startupIDForm = regexp.MustCompile(`^[0-9a-f]{64}$`)

// identityState is what the start found of the project's identity.
type identityState int

const (
	// identityNone: no identity file; the project's name is its path's hash,
	// for what Desk keeps under it already.
	identityNone identityState = iota
	// identityKept: the identity file names the project.
	identityKept
	// identityUnread: there is an identity file, or a private folder Desk
	// cannot hold, and it could not be read now: the project has no name.
	identityUnread
)

// startupIdentity is the project's identity, as this server holds it.
type startupIdentity struct {
	mu    sync.RWMutex
	state identityState
	id    string
	// from is the name a migration of stamping settings is from, where it is
	// not finished.
	from string
	// problem says, for identityUnread, why, in words with no path.
	problem string
	// shared is set where another folder holds this identity too.
	shared bool
	// candidate is the name the offer shows a first key under, where the
	// project has none yet: random, the same for this server's life.
	candidate string
}

// errIdentityChanged is an identity file that is not the one Desk wrote or
// read, or a name already taken where Desk would write one.
var errIdentityChanged = errors.New("this project's identity changed while Desk was writing it")

// testHookIdentityStep runs before each step of a migration that a stop can
// cut short ("written", "moved"), and is nil outside tests. An error it
// returns stands for that step failing now, as a stop would leave it.
var testHookIdentityStep func(step string) error

func identityStep(step string) error {
	if testHookIdentityStep != nil {
		return testHookIdentityStep(step)
	}
	return nil
}

// startupName is the name Desk keeps this project's key and settings under:
// its identity, or, where it has none, its path's hash; "" where its identity
// could not be read now.
func (s *Server) startupName() string {
	s.startup.mu.RLock()
	defer s.startup.mu.RUnlock()
	switch s.startup.state {
	case identityKept:
		return s.startup.id
	case identityUnread:
		return ""
	}
	return digestOf([]byte(s.projectDir))
}

// startupKept is whether the project's name is the one its own identity
// file holds.
func (s *Server) startupKept() bool {
	s.startup.mu.RLock()
	defer s.startup.mu.RUnlock()
	return s.cfg.deskID == "" && s.startup.state == identityKept
}

// startupBound is whether the project's name is the one its own identity
// file holds, and no other folder holds: the only name a recovery may remove
// a key under. Never the path's hash, which another project at the same
// path has too, and never an identity a copy shares.
func (s *Server) startupBound() bool {
	s.startup.mu.RLock()
	defer s.startup.mu.RUnlock()
	return s.cfg.deskID == "" && s.startup.state == identityKept && !s.startup.shared
}

// startupShared is whether another folder holds this project's identity
// too, as a copy of the project does.
func (s *Server) startupShared() bool {
	s.startup.mu.RLock()
	defer s.startup.mu.RUnlock()
	return s.cfg.deskID == "" && s.startup.state == identityKept && s.startup.shared
}

// sharedWords is what the panel says of a project whose identity another
// folder holds too.
const sharedWords = "this project's identity is also held by another folder, as a copy of the project holds it, so Desk makes and rotates no key for it here, and recovers nothing under it"

// newStartupKeyName is the name a first key of this project is made under:
// its identity, or the candidate the offer shows until one is written; an
// error, in words with no path, where its identity could not be read now.
func (s *Server) newStartupKeyName() (string, error) {
	s.startup.mu.Lock()
	defer s.startup.mu.Unlock()
	switch s.startup.state {
	case identityKept:
		return s.startup.id, nil
	case identityUnread:
		return "", errors.New(s.startup.problem)
	}
	if s.startup.candidate == "" {
		s.startup.candidate = randomStartupID()
	}
	return s.startup.candidate, nil
}

// setStartup records what the project's identity now is.
func (s *Server) setStartup(state identityState, id, from, problem string) {
	s.startup.mu.Lock()
	defer s.startup.mu.Unlock()
	s.startup.state, s.startup.id, s.startup.from, s.startup.problem, s.startup.shared = state, id, from, problem, false
}

// setShared records that another folder holds this identity too.
func (s *Server) setShared() {
	s.startup.mu.Lock()
	defer s.startup.mu.Unlock()
	s.startup.shared = true
}

// randomStartupID is a new project name. crypto/rand never fails, and never
// returns short (Go 1.24 and later).
func randomStartupID() string {
	var raw [32]byte
	_, _ = rand.Read(raw[:])
	return hex.EncodeToString(raw[:])
}

// identityRecord is the identity file, as Desk writes it.
type identityRecord struct {
	ID   string `json:"id"`
	Path string `json:"path"`
	From string `json:"from,omitempty"`
}

// line is the file's one spelling.
func (r identityRecord) line() []byte {
	data, _ := json.Marshal(r)
	return append(data, '\n')
}

// parseIdentity holds an identity file to its record: one line, in the
// spelling Desk writes, naming a project, and a migration from another name
// where it says so.
func parseIdentity(data []byte) (identityRecord, error) {
	var record identityRecord
	if json.Unmarshal(data, &record) != nil || !startupIDForm.MatchString(record.ID) || !filepath.IsAbs(record.Path) ||
		record.From != "" && (!startupIDForm.MatchString(record.From) || record.From == record.ID) ||
		!bytes.Equal(record.line(), data) {
		return identityRecord{}, errors.New("its identity file is not one Desk writes")
	}
	return record, nil
}

// openIdentityFolder holds `.desk-private/` as the hand-over's folder is held
// (`openOwnFolder`): a real folder, the user's, open to nobody else. It is
// fs.ErrNotExist where the project has none.
func (s *Server) openIdentityFolder() (*os.Root, error) {
	root, _, err := openOwnFolder(s.root, []string{startupIdentityDir}, false, startupIdentityKept)
	return root, err
}

// identityFolderUsable is nil where the project's identity can be written:
// its `.desk-private/` is not there, and would be made owner-only, or is a
// folder Desk holds. Otherwise it says why, in words with no path.
func (s *Server) identityFolderUsable() error {
	private, err := s.openIdentityFolder()
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("its private folder is not one Desk keeps this project's identity in: %w", err)
	}
	private.Close()
	return nil
}

// readIdentity reads the identity file through private, the folder held.
// found is false where there is none.
func readIdentity(private *os.Root) (record identityRecord, info os.FileInfo, found bool, err error) {
	data, info, err := readPrivateFile(private, startupIdentityName, startupIdentityLimit)
	if errors.Is(err, fs.ErrNotExist) {
		return identityRecord{}, nil, false, nil
	}
	if err != nil {
		return identityRecord{}, nil, false, err
	}
	record, err = parseIdentity(data)
	if err != nil {
		return identityRecord{}, nil, false, err
	}
	return record, info, true, nil
}

// writeIdentity writes record as the identity file, whole: staged under a
// name of its own, made 0600 on its descriptor, synced, and then linked into
// place where over is false, so that it never replaces a name another
// writer put there first (errIdentityChanged), or renamed over the file
// read, info, where over is true. The folder is synced after. It answers
// the file as written.
func writeIdentity(private *os.Root, record identityRecord, over os.FileInfo) (os.FileInfo, error) {
	stage, err := randomStagingName(identityStagingPrefix)
	if err != nil {
		return nil, err
	}
	file, err := private.OpenFile(stage, os.O_RDWR|os.O_CREATE|os.O_EXCL|openNoFollow, custodyFileMode)
	if err != nil {
		return nil, err
	}
	defer private.Remove(stage)
	failed := func(err error) (os.FileInfo, error) {
		file.Close()
		return nil, err
	}
	if _, err := file.Write(record.line()); err != nil {
		return failed(err)
	}
	if err := file.Chmod(custodyFileMode); err != nil {
		return failed(err)
	}
	if err := file.Sync(); err != nil {
		return failed(err)
	}
	written, err := file.Stat()
	if err != nil {
		return failed(err)
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	if over == nil {
		if err := private.Link(stage, startupIdentityName); errors.Is(err, fs.ErrExist) {
			return nil, errIdentityChanged
		} else if err != nil {
			return nil, err
		}
	} else {
		if found, err := private.Lstat(startupIdentityName); err != nil || !os.SameFile(found, over) {
			return nil, errIdentityChanged
		}
		if err := private.Rename(stage, startupIdentityName); err != nil {
			return nil, err
		}
	}
	_ = syncPrivateDirectory(private)
	return written, nil
}

// resolveStartupIdentity reads the identity of the project Desk was started
// on, once, at start, before anything is kept or recovered under its name;
// and, where its private folder holds none, or a migration is not finished,
// makes it so (`migrateStartupIdentity`). A desk Desk made is named by its id
// and has none.
func (s *Server) resolveStartupIdentity() {
	if s.cfg.deskID != "" {
		return
	}
	private, err := s.openIdentityFolder()
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return
	case err != nil:
		// A folder Desk cannot hold: its name is the path's hash only where
		// nothing is kept there as an identity file.
		if _, lookErr := s.root.Lstat(filepath.Join(startupIdentityDir, startupIdentityName)); errors.Is(lookErr, fs.ErrNotExist) {
			s.log.Printf("desk: this project's private folder is not one Desk keeps its identity in, so it is named by its path: %v", err)
			return
		}
		s.log.Printf("desk: this project's identity could not be read, so Desk acts on nothing it keeps for it: %v", err)
		s.setStartup(identityUnread, "", "", "this project's identity, in its private folder, could not be read")
		return
	}
	defer private.Close()
	record, info, found, err := readIdentity(private)
	switch {
	case err != nil:
		s.log.Printf("desk: this project's identity could not be read, so Desk acts on nothing it keeps for it: %v", err)
		s.setStartup(identityUnread, "", "", "this project's identity, in its private folder, could not be read")
		return
	case found:
		s.setStartup(identityKept, record.ID, record.From, "")
		if record.Path != s.projectDir {
			if record, info = s.copiedOrMoved(private, record, info); s.startupShared() {
				return
			}
		}
		if record.From == "" {
			return
		}
	}
	s.migrateStartupIdentity(private, record, info, found)
}

// copiedOrMoved tells, for an identity file written in another folder,
// whether that folder holds the same identity still: a copy, which makes the
// identity shared (`startupShared`); or not, a move, after which the file is
// written again with this folder's path, under this project's lock taken
// once. Where it cannot be told now, the identity is taken as shared. It
// answers the record and the file as they are then.
func (s *Server) copiedOrMoved(private *os.Root, record identityRecord, info os.FileInfo) (identityRecord, os.FileInfo) {
	holds, err := s.folderHoldsIdentity(record.Path, record.ID)
	if err != nil || holds {
		s.setShared()
		s.log.Printf("desk: this project's identity is also held by %s, or that could not be told now (%v), so Desk makes and rotates no key under it here, and recovers nothing under it", record.Path, err)
		return record, info
	}
	unlock, err := s.lockProject(context.Background(), 0)
	if err != nil {
		s.log.Printf("desk: this project was moved from %s; its identity is written again with its folder at the next start, because this project's lock was not taken: %v", record.Path, err)
		return record, info
	}
	defer unlock()
	moved := identityRecord{ID: record.ID, Path: s.projectDir, From: record.From}
	written, err := writeIdentity(private, moved, info)
	if err != nil {
		s.log.Printf("desk: this project was moved from %s, and its identity could not be written again with its folder: %v", record.Path, err)
		return record, info
	}
	s.log.Printf("desk: this project was moved from %s, and keeps its identity", record.Path)
	return moved, written
}

// folderHoldsIdentity is whether the folder at path, another than this
// project's, holds id in its identity file: false where there is no folder
// there, or it holds no identity or another; an error where that could not
// be read now. The folder at path that is this project's own, reached by
// another spelling, holds none other.
func (s *Server) folderHoldsIdentity(path, id string) (bool, error) {
	other, err := os.OpenRoot(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer other.Close()
	if here, err := s.root.Stat("."); err == nil {
		if there, err := other.Stat("."); err == nil && os.SameFile(here, there) {
			return false, nil
		}
	}
	private, _, err := openOwnFolder(other, []string{startupIdentityDir}, false, startupIdentityKept)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer private.Close()
	record, _, found, err := readIdentity(private)
	if err != nil {
		return false, err
	}
	return found && record.ID == id, nil
}

// migrateStartupIdentity writes the project's identity where its private
// folder holds none, and finishes a migration of its stamping settings that
// a stop cut short. It takes this project's lock and the signing folder's,
// each once: where either is held, or none can be taken here, it changes
// nothing, and the next start does it.
func (s *Server) migrateStartupIdentity(private *os.Root, record identityRecord, info os.FileInfo, found bool) {
	unlock, err := s.lockProject(context.Background(), 0)
	if err != nil {
		s.log.Printf("desk: this project's identity was left for the next start, because this project's lock was not taken: %v", err)
		return
	}
	defer unlock()
	dir, err := s.assistant.openSigning(false)
	switch {
	case errors.Is(err, errNoSigningDir):
		dir = nil
	case err != nil:
		s.log.Printf("desk: this project's identity was left for the next start, because the signing folder could not be opened: %v", err)
		return
	default:
		defer dir.Close()
		unlockSigning, err := lockSigning(dir)
		if err != nil {
			s.log.Printf("desk: this project's identity was left for the next start, because the signing folder's lock was not taken: %v", err)
			return
		}
		defer unlockSigning()
	}
	if !found {
		if record, err = s.chooseStartupIdentity(dir); err != nil {
			s.log.Printf("desk: this project's identity was left for the next start: %v", err)
			return
		}
		if err := identityStep("written"); err != nil {
			s.log.Printf("desk: this project's identity was not written: %v", err)
			return
		}
		if info, err = writeIdentity(private, record, nil); err != nil {
			s.log.Printf("desk: this project's identity was not written: %v", err)
			return
		}
		s.setStartup(identityKept, record.ID, record.From, "")
		s.log.Printf("desk: this project is named %s from now on, wherever it is moved", record.ID)
	}
	if record.From != "" {
		s.finishStartupMove(private, record, info)
	}
}

// chooseStartupIdentity is the name a project with no identity takes: the
// seed's name, where its jpack.json names a seed Desk keeps in its signing
// folder, dir (nil where there is none); otherwise a new one, with the
// stamping settings kept under the path's hash to move to it where no key,
// list or marker is kept under that hash. Anything that decides it and could
// not be read now is an error: nothing is chosen.
func (s *Server) chooseStartupIdentity(dir *signingDir) (identityRecord, error) {
	if dir != nil {
		named, err := s.namedStartupSeed(dir)
		if err != nil {
			return identityRecord{}, err
		}
		if named != "" {
			return identityRecord{ID: named, Path: s.projectDir}, nil
		}
	}
	record := identityRecord{ID: randomStartupID(), Path: s.projectDir}
	from, err := s.legacyStampingToMove(dir)
	if err != nil {
		return identityRecord{}, err
	}
	record.From = from
	return record, nil
}

// namedStartupSeed is the name of the seed this project's jpack.json names,
// where it names, by Desk's own path to it, a seed kept in dir under a
// project's name; "" where it names none, or another file. A jpack.json that
// could not be read now is an error.
func (s *Server) namedStartupSeed(dir *signingDir) (string, error) {
	data, err := s.readReviewFileWithin(runtimeConfigName, reviewTextLimit)
	if codeOf(err) == CodeNotFound {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("its %s could not be read: %w", runtimeConfigName, err)
	}
	var config struct {
		Audit *struct {
			SigningKey *string `json:"signingKey"`
		} `json:"audit"`
	}
	if json.Unmarshal(data, &config) != nil || config.Audit == nil || config.Audit.SigningKey == nil {
		return "", nil
	}
	named := *config.Audit.SigningKey
	if filepath.Dir(named) != dir.path {
		return "", nil
	}
	id, isSeed := strings.CutSuffix(filepath.Base(named), seedSuffix)
	if !isSeed || !startupIDForm.MatchString(id) {
		return "", nil
	}
	seed, err := dir.root.Lstat(id + seedSuffix)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("the seed its %s names could not be inspected: %w", runtimeConfigName, err)
	case !seed.Mode().IsRegular():
		return "", nil
	}
	return id, nil
}

// legacyStampingToMove is the path's hash, where stamping settings are kept
// under it and no key, list or marker is: settings a key under that hash
// stays with are not moved. "" where there is nothing to move.
func (s *Server) legacyStampingToMove(dir *signingDir) (string, error) {
	legacy := digestOf([]byte(s.projectDir))
	if dir != nil {
		for _, suffix := range []string{seedSuffix, keysSuffix, creatingSuffix, rotatingSuffix, nextSeedSuffix} {
			_, err := dir.root.Lstat(legacy + suffix)
			if err == nil {
				return "", nil
			}
			if !errors.Is(err, fs.ErrNotExist) {
				return "", fmt.Errorf("what Desk keeps under this project's former name could not be inspected: %w", err)
			}
		}
	}
	if !s.assistant.usable() {
		return "", nil
	}
	_, err := s.assistant.root.Lstat(filepath.Join(stampingDirName, legacy))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("the stamping settings kept under this project's former name could not be inspected: %w", err)
	}
	return legacy, nil
}

// finishStartupMove renames the stamping settings kept under record.From to
// record.ID, never over anything, and then writes the identity file again
// without `from`, over info, the file read or written. A step that fails
// leaves `from` for the next start; settings already kept under the new name
// are left as they are, and so are the old.
func (s *Server) finishStartupMove(private *os.Root, record identityRecord, info os.FileInfo) {
	if !s.assistant.usable() {
		return
	}
	from, to := filepath.Join(stampingDirName, record.From), filepath.Join(stampingDirName, record.ID)
	_, fromErr := s.assistant.root.Lstat(from)
	_, toErr := s.assistant.root.Lstat(to)
	switch {
	case fromErr != nil && !errors.Is(fromErr, fs.ErrNotExist), toErr != nil && !errors.Is(toErr, fs.ErrNotExist):
		s.log.Printf("desk: the stamping settings kept under this project's former name were left for the next start: %v %v", fromErr, toErr)
		return
	case fromErr == nil && toErr == nil:
		s.log.Printf("desk: the stamping settings kept under this project's former name, %s, were left as they are, because settings are kept under its name already", record.From)
	case fromErr == nil:
		if err := identityStep("moved"); err != nil {
			s.log.Printf("desk: the stamping settings kept under this project's former name were left for the next start: %v", err)
			return
		}
		if err := s.assistant.root.Rename(from, to); err != nil {
			s.log.Printf("desk: the stamping settings kept under this project's former name were left for the next start: %v", err)
			return
		}
		if stamping, err := s.assistant.root.Open(stampingDirName); err == nil {
			_ = stamping.Sync()
			_ = stamping.Close()
		}
		s.log.Printf("desk: the stamping settings kept under this project's former name, %s, are kept under its name from now on", record.From)
	}
	if _, err := writeIdentity(private, identityRecord{ID: record.ID, Path: record.Path}, info); err != nil {
		s.log.Printf("desk: this project's identity still names its former name, for the next start: %v", err)
		return
	}
	s.setStartup(identityKept, record.ID, "", "")
}
