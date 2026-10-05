package desk

// Desks are complete, isolated project folders. The registry only creates
// fresh folders below the installation-owned desks directory; no browser
// supplied path can widen the file API's authority. Existing projects keep
// their established storage locations until explicitly migrated.
import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"syscall"
	"unicode"
	"unicode/utf8"
)

var deskIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

const deskManifest = ".desk-private/desk.json"

// A new desk starts under reviewed law, with deciding runs recorded (ADR-0009,
// section 1): `requireReviewed` refuses a deciding run of a draft, the audit
// trail records each deciding run in `.desk-private/audit`, which is private
// and never committed, and `requireComparableFacts` refuses a fact of a type
// no comparison in the pack can match.
//
// configVersion "5" needs runtime 0.25.0 or later. A runtime that reads "4"
// but not "5" gets the same gates without `requireComparableFacts`, and the
// creation says so. One that reads neither cannot hold a desk to its reviewed
// set, and no desk is created.
//
// **A new desk is signed where it can be** (ADR-0010, section 1 and question
// 3): where the runtime reads "6" and Desk's custody can keep a key, the
// configuration is "5"'s at configVersion "6", and its audit member names the
// key Desk keeps for the desk (`signedDeskConfig`). Every runtime that reads
// the desk's configuration then signs its records. The fallbacks:
//
//	| The runtime reads | Custody keeps a key | jpack.json         | Signed | The creation says   |
//	|-------------------|---------------------|--------------------|--------|---------------------|
//	| "6"               | yes                 | "6", signingKey    | yes    | nothing             |
//	| "6"               | no                  | "5"                | no     | unsignedByCustody   |
//	| "5", not "6"      | not asked           | "5"                | no     | unsignedByRuntime   |
//	| "4", not "5"      | not asked           | "4"                | no     | the "4" notice, and |
//	|                   |                     |                    |        | unsignedByRuntime   |
//	| neither "4" nor "5" | not asked         | no desk            |        | why                 |
//
// A key the runtime fails to generate makes no desk, and leaves no seed and
// no list of public keys (`generateDeskKey`). A build that cannot establish
// who owns a directory keeps no key (`openSigning`); there, today, the desks
// folder is refused by the same custody, so no desk is made at all.
const (
	deskAuditDir               = ".desk-private/audit"
	gatedDeskConfig            = `{"configVersion":"5","requireReviewed":true,"requireComparableFacts":true,"audit":{"dir":".desk-private/audit"},"packs":{}}` + "\n"
	gatedDeskConfigVersion4    = `{"configVersion":"4","requireReviewed":true,"audit":{"dir":".desk-private/audit"},"packs":{}}` + "\n"
	comparableFactsFromVersion = "5"
	reviewedFromVersion        = "4"
)

// The creation's notices for a desk made unsigned. Each is its own paragraph
// of the notice, after the "4" one where both apply, so that the page can
// show each in the owner's language.
const (
	// unsignedByRuntime: the runtime's version, and the first that reads "6".
	unsignedByRuntime = "This desk is not signed: a desk names its signing key at configVersion 6, and the runtime this Desk runs (jpack %s) does not read it. A runtime of %s or later creates desks signed."
	// unsignedByCustody: why custody keeps no key, in words with no path.
	unsignedByCustody = "This desk is not signed, because Desk could not keep a signing key for it: %s. It was created at configVersion 5, which names no signing key."
)

// deskGates is the configuration a new desk is written with, and what the
// creation says about it.
type deskGates struct {
	config                 []byte
	configVersion          string
	requireComparableFacts bool
	// signed is whether the configuration names a key Desk keeps for the
	// desk, key.
	signed bool
	key    *madeKey
	notice string
}

// unsigned adds a paragraph to the creation's notice.
func (g *deskGates) unsigned(paragraph string) {
	if g.notice != "" {
		g.notice += "\n\n"
	}
	g.notice += paragraph
}

// signedDeskConfig is the configuration of a signed desk: "5"'s gates at
// configVersion "6", with the audit member's signingKey naming seedPath after
// its dir (runtime 0.26.0, `jpack.schema.json`, `$defs.audit`). The path is a
// JSON string as Go's encoder writes one, with "<", ">" and "&" left as they
// are; it escapes a control character, U+2028 and U+2029, and the runtime
// reads each back as the character it was.
func signedDeskConfig(seedPath string) []byte {
	var quoted bytes.Buffer
	encoder := json.NewEncoder(&quoted)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(seedPath)
	return []byte(`{"configVersion":"6","requireReviewed":true,"requireComparableFacts":true,"audit":{"dir":".desk-private/audit","signingKey":` +
		strings.TrimSuffix(quoted.String(), "\n") + `},"packs":{}}` + "\n")
}

// newDeskID is a new desk's id: 128 random bits, in hex. A variable only so
// that a test can know a desk's id, and so its signing key's path and its
// configuration's exact bytes, before the desk is made.
var newDeskID = func() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(random[:]), nil
}

// chooseDeskGates picks the configuration from what the runtime reads.
func chooseDeskGates(schema runtimeSchema) (deskGates, error) {
	if slices.Contains(schema.supported, comparableFactsFromVersion) {
		return deskGates{config: []byte(gatedDeskConfig), configVersion: comparableFactsFromVersion, requireComparableFacts: true}, nil
	}
	reads := strings.Join(schema.supported, ", ")
	if slices.Contains(schema.supported, reviewedFromVersion) {
		return deskGates{config: []byte(gatedDeskConfigVersion4), configVersion: reviewedFromVersion, notice: fmt.Sprintf(
			"The runtime this Desk runs (jpack %s) reads configuration versions %s, not 5. This desk was created at configVersion 4, without requireComparableFacts, so a fact of a type no comparison can match is not refused. A runtime of 0.25.0 or later creates desks with it.",
			schema.version, reads)}, nil
	}
	return deskGates{}, fmt.Errorf(
		"the runtime this Desk runs (jpack %s) reads configuration versions %s, and a new desk needs 4 or later to refuse a deciding run of an unreviewed pack; update the runtime to 0.25.0 or later",
		schema.version, reads)
}

type deskRecord struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Folder  string `json:"folder"`
	Managed bool   `json:"managed"`
}

func validDeskName(name string) bool {
	return name == strings.TrimSpace(name) && len([]rune(name)) > 0 && len([]rune(name)) <= 80 && !strings.ContainsFunc(name, unicode.IsControl)
}
func (s *Server) deskOwner() *Server {
	if s.cfg.parent != nil {
		return s.cfg.parent
	}
	return s
}
func (s *Server) deskRecord() (deskRecord, error) {
	row := deskRecord{ID: s.cfg.deskID, Name: filepath.Base(s.projectDir), Folder: s.projectDir, Managed: s.cfg.deskID != ""}
	if s.cfg.deskID == "" {
		return row, nil
	}
	saved, err := readDeskManifest(s.root, s.cfg.deskID)
	if errors.Is(err, errNotPublished) {
		return row, errors.New("desk metadata is invalid")
	}
	if err != nil {
		return row, err
	}
	row.Name = saved.Name
	if s.cfg.parent == nil {
		row.ID = ""
	}
	return row, nil
}
func (s *Server) desksRoot() (*os.Root, error) {
	if !s.assistant.usable() {
		return nil, errors.New("desk storage is unavailable")
	}
	if err := s.assistant.root.Mkdir("desks", 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	info, err := s.assistant.root.Lstat("desks")
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("desks must be an owner-only directory")
	}
	if err = ownedByUs("desks", info); err != nil {
		return nil, err
	}
	return s.assistant.root.OpenRoot("desks")
}
func (s *Server) openDeskLocked(id string) (*Server, error) {
	if s.desksClosed {
		return nil, errors.New("Desk is shutting down")
	}
	if child := s.desks[id]; child != nil {
		return child, nil
	}
	if !deskIDPattern.MatchString(id) {
		return nil, errors.New("invalid desk identifier")
	}
	root, err := s.desksRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Lstat(id)
	if err != nil {
		return nil, err
	}
	if !deskFolderAccepted(info) {
		return nil, errors.New("invalid desk folder")
	}
	dir := filepath.Join(s.configDir, "desks", id)
	pinned, err := OpenProjectRoot(dir)
	if err != nil {
		return nil, err
	}
	// Check the held tree is the tree in the authorized registry, including a
	// concurrent replacement of its directory or an ancestor.
	held, err := pinned.own.root.Stat(".")
	if err != nil || !os.SameFile(info, held) {
		pinned.Close()
		return nil, errors.New("desk folder changed while opening")
	}
	if _, err := readDeskManifest(pinned.own.root, id); err != nil {
		pinned.Close()
		return nil, errors.New("desk metadata is invalid")
	}
	cfg := s.cfg
	cfg.parent = s
	cfg.deskID = id
	cfg.Root = pinned
	cfg.ProjectDir = ""
	child, err := New(cfg)
	if err != nil {
		return nil, err
	}
	if s.desks == nil {
		s.desks = make(map[string]*Server)
	}
	s.desks[id] = child
	return child, nil
}
func (s *Server) handleDesks(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	owner := s.deskOwner()
	if r.Method == http.MethodPost {
		owner.createDesk(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	owner.desksMu.Lock()
	defer owner.desksMu.Unlock()
	current, err := s.deskRecord()
	if err != nil {
		storageFailure(w, err)
		return
	}
	home, err := owner.deskRecord()
	if err != nil {
		storageFailure(w, err)
		return
	}
	rows := []deskRecord{home}
	for _, child := range owner.desks {
		row, err := child.deskRecord()
		if err != nil {
			storageFailure(w, err)
			return
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	writeJSON(w, 200, struct {
		Current  deskRecord   `json:"current"`
		Desks    []deskRecord `json:"desks"`
		Location string       `json:"location"`
	}{current, rows, filepath.Join(owner.configDir, "desks")})
}

// maxDesks bounds the registry: the desks one installation opens, with those
// it is still creating. A variable only so a test can reach the bound
// without making 256 desks.
var maxDesks = 256

// registryReadLimit bounds how much of the desks folder a start reads. It is
// wider than maxDesks because unfinished folders are read past, not counted.
const registryReadLimit = 4096

// createDesk makes a new desk: a fresh folder below the registry, started
// under reviewed law (ADR-0009, section 1), and published only once it is
// complete.
//
// **The registry's lock is not held while the folder is made.** Making it runs
// the runtime twice, and every named desk's requests, and every event
// delivery, take that lock to find their desk. Held throughout, one slow or
// hung runtime stalled all of them for as long as its bounds allow. So the
// lock is taken to check the bound and reserve a place, released while the
// folder is made, and taken again to publish:
//
//   - the reservation counts toward the bound, so creations in flight cannot
//     pass it between them;
//   - publishing re-checks shutdown, and writes the manifest under the lock,
//     so a desk made during shutdown is removed rather than left half open;
//   - the id needs no reservation: it is 128 random bits, and `Mkdir` refuses
//     one that exists.
func (s *Server) createDesk(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name string `json:"name"`
	}
	data, err := readBounded(r.Body, 4096)
	if err != nil || decodeDataJSON(data, &request) != nil || !validDeskName(request.Name) {
		writeJSONCoded(w, 400, CodeBadRequest, "Enter a desk name between 1 and 80 characters.")
		return
	}
	s.desksMu.Lock()
	defer s.desksMu.Unlock()
	if s.desksClosed || len(s.desks)+s.deskCreations >= maxDesks {
		writeJSONCoded(w, http.StatusConflict, CodeBadRequest, fmt.Sprintf("Desk creation is unavailable while shutting down or at the %d desk limit.", maxDesks))
		return
	}
	root, err := s.desksRoot()
	if err != nil {
		storageFailure(w, err)
		return
	}
	defer root.Close()
	id, err := newDeskID()
	if err != nil {
		storageFailure(w, err)
		return
	}
	if err = root.Mkdir(id, 0700); err != nil {
		storageFailure(w, err)
		return
	}
	entry := filepath.Join(s.configDir, "desks", id)
	folder, err := root.OpenRoot(id)
	if err != nil {
		s.abandonDesk(w, nil, entry, storageRefusal(err))
		return
	}
	defer folder.Close()
	s.deskCreations++
	gates, failure := func() (deskGates, *deskFailure) {
		// Released while the folder is made. A panic takes the lock back
		// before it unwinds, so the release deferred above stays balanced.
		s.desksMu.Unlock()
		defer s.desksMu.Lock()
		return s.makeDeskFolder(r.Context(), folder, entry, id)
	}()
	s.deskCreations--
	if failure != nil {
		s.abandonDesk(w, folder, entry, failure)
		return
	}
	defer gates.key.close()
	if s.desksClosed {
		s.abandonDesk(w, folder, entry, s.dropKey(gates.key, &deskFailure{http.StatusConflict, CodeBadRequest, "Desk is shutting down, so the desk was not created."}))
		return
	}
	// **The key is still where the configuration names it**, or the desk is
	// not published, and never answered as signed: the seed's pathname must
	// name the seed found through the folder held (`stillNamed`).
	keyBetween("before publish")
	if err = gates.key.stillNamed(); err != nil {
		s.log.Printf("desk: the new desk %s was not published: %v", id, err)
		s.abandonDesk(w, folder, entry, s.dropKey(gates.key, runtimeRefusal(err)))
		return
	}
	// Metadata is written last, as one event (`publishDeskManifest`). Until
	// it is, the folder is not a desk.
	manifest, _ := json.Marshal(struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}{id, request.Name})
	if err = publishDeskManifest(folder, manifest); err != nil {
		s.abandonDesk(w, folder, entry, s.dropKey(gates.key, storageRefusal(err)))
		return
	}
	// The desk is published: its key's marker goes. Where it cannot, the
	// next start removes the marker alone (`sweepUnfinishedKeys`).
	keyBetween("published")
	if err := gates.key.settle(); err != nil {
		s.log.Printf("desk: the new desk %s's creation marker was left; the next start removes it: %v", id, err)
	}
	if gates.notice != "" {
		s.log.Printf("desk: %s", strings.ReplaceAll(gates.notice, "\n\n", " "))
	}
	child, err := s.openDeskLocked(id)
	if err != nil {
		storageFailure(w, err)
		return
	}
	record, err := child.deskRecord()
	if err != nil {
		storageFailure(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, struct {
		deskRecord
		ConfigVersion          string `json:"configVersion"`
		RequireComparableFacts bool   `json:"requireComparableFacts"`
		Signed                 bool   `json:"signed"`
		Notice                 string `json:"notice,omitempty"`
	}{record, gates.configVersion, gates.requireComparableFacts, gates.signed, gates.notice})
}

// dropKey is failure, after removing the key a creation that stopped had
// made. Where that cannot be done, Desk's log says where, and the answer says
// so in words with no path.
func (s *Server) dropKey(key *madeKey, failure *deskFailure) *deskFailure {
	if err := key.unmake(); err != nil {
		s.log.Printf("desk: a failed creation could not remove the signing key it made in %s: %v", key.dir.path, err)
		failure.message += " The signing key made for it could not be removed, and was left in Desk's signing folder; no desk names it."
	}
	return failure
}

// makeDeskFolder makes a new desk's folder, through folder, the root this
// request opened when it made the directory: the folders, the desk's signing
// key where it is signed, the configuration the runtime can hold the desk to,
// and the runtime's lock of it, in that order. It holds no lock, and writes no
// manifest. A failure removes the key it made; on success, the caller closes
// it.
func (s *Server) makeDeskFolder(ctx context.Context, folder *os.Root, entry, id string) (deskGates, *deskFailure) {
	for _, dir := range []string{"packs", "sources", ".desk", ".desk/job-drafts", ".desk-private", deskAuditDir} {
		if err := folder.Mkdir(dir, 0700); err != nil {
			return deskGates{}, storageRefusal(err)
		}
	}
	held, err := holdDeskFolder(folder, entry)
	if err != nil {
		return deskGates{}, storageRefusal(err)
	}
	defer held.file.Close()
	// Ask the runtime the relay will run which configurations it reads,
	// write the one it can hold the desk to, and lock it before the
	// manifest: without a lock, `requireReviewed` refuses every deciding
	// run, not only a draft's.
	bin := s.cfg.JpackBin
	schema, err := readRuntimeSchema(ctx, bin, held)
	if err != nil {
		return deskGates{}, runtimeRefusal(err)
	}
	gates, err := chooseDeskGates(schema)
	if err != nil {
		return deskGates{}, runtimeRefusal(err)
	}
	// The key first, then the configuration that names it, then the lock
	// that pins that configuration.
	if slices.Contains(schema.supported, signedFromVersion) {
		dir, err := s.assistant.openSigning(true)
		if err == nil && !utf8.ValidString(dir.path) {
			dir.Close()
			err = errors.New("the path of Desk's signing folder is not valid UTF-8, which jpack.json cannot name")
		}
		if err != nil {
			s.log.Printf("desk: no signing key is kept for the new desk %s: %v", id, err)
			gates.unsigned(fmt.Sprintf(unsignedByCustody, strings.TrimRight(s.custodyWords(err.Error()), ".")))
		} else {
			// The signing folder's lock, from before the marker until the
			// marker is removed (`madeKey.close`), so that no other Desk
			// process's sweep removes the key meanwhile (issue #230).
			unlock, err := lockSigningWithin(ctx, dir, signingLockWait)
			if errors.Is(err, errSigningBusy) {
				dir.Close()
				return deskGates{}, &deskFailure{http.StatusConflict, CodeBadRequest, "The desk was not created: " + signingBusyWords}
			}
			if err != nil {
				s.log.Printf("desk: the new desk %s's key is made without the signing folder's lock: %v", id, err)
				unlock = func() {}
			}
			// Released however the creation ends before its key holds the
			// lock, a panic included, as a stopped process releases it.
			handedOver := false
			defer func() {
				if !handedOver {
					unlock()
				}
			}()
			key, err := generateDeskKey(ctx, bin, held, dir, id)
			if err != nil {
				dir.Close()
				s.log.Printf("desk: the new desk %s's signing key was not generated: %v", id, err)
				return deskGates{}, runtimeRefusal(errors.New(s.withoutCustodyPaths(err.Error(), id)))
			}
			key.unlock, handedOver = unlock, true
			gates = deskGates{config: signedDeskConfig(key.seedPath()), configVersion: signedFromVersion, requireComparableFacts: true, signed: true, key: key}
		}
	} else {
		gates.unsigned(fmt.Sprintf(unsignedByRuntime, schema.version, auditRuntimeFloor))
	}
	failed := func(failure *deskFailure) (deskGates, *deskFailure) {
		failure = s.dropKey(gates.key, failure)
		gates.key.close()
		return deskGates{}, failure
	}
	for name, body := range map[string]string{
		runtimeConfigName: string(gates.config),
		"jpack-desk.json": "{\"deskConfigVersion\":1}\n",
		".gitignore":      ".desk-private/\n",
	} {
		if err = folder.WriteFile(name, []byte(body), 0600); err != nil {
			return failed(storageRefusal(err))
		}
	}
	if err = lockRuntimeProject(ctx, bin, held, folder, gates.config); err != nil {
		return failed(runtimeRefusal(errors.New(s.withoutCustodyPaths(err.Error(), id))))
	}
	return gates, nil
}

// errNotPublished is a desk folder the registry would not open as a desk:
// one with no manifest, or one whose manifest is not a desk's.
var errNotPublished = errors.New("not a published desk")

// savedDesk is a desk's manifest, as the registry reads it.
type savedDesk struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// readDeskManifest reads desk id's manifest through folder, the desk's own
// folder, and is the one reader of it: the registry opens a desk
// (`openDeskLocked`) and names it (`deskRecord`) only where it answers one,
// and a start's sweep counts a desk as published only where it does
// (`deskPublished`). Its manifest must be a private file of the user's, within
// its bound, whose JSON names this id and a valid name.
//
// A manifest that is not there, or is there and is not one, is
// errNotPublished. Any other error says only that it could not be read now:
// it could not be inspected or opened, or it changed while it was opened.
func readDeskManifest(folder *os.Root, id string) (savedDesk, error) {
	data, err := readPrivateData(folder, deskManifest, 4096)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return savedDesk{}, fmt.Errorf("%w: it has no manifest", errNotPublished)
	case err != nil && !errors.Is(err, errPrivateDataChanged) && (codeOf(err) == CodeForbidden || codeOf(err) == CodeTooLarge):
		return savedDesk{}, fmt.Errorf("%w: its manifest is not one Desk reads: %v", errNotPublished, err)
	case err != nil:
		return savedDesk{}, err
	}
	var saved savedDesk
	if json.Unmarshal(data, &saved) != nil || saved.ID != id || !validDeskName(saved.Name) {
		return savedDesk{}, fmt.Errorf("%w: its manifest is not a desk's", errNotPublished)
	}
	return saved, nil
}

// deskFolderAccepted is whether an entry of the desks folder is one the
// registry opens: a real directory, not a link.
func deskFolderAccepted(info fs.FileInfo) bool {
	return info.IsDir() && info.Mode()&os.ModeSymlink == 0
}

// manifestStagingPrefix names a desk's manifest while it is being written,
// in `.desk-private`, before it is renamed into place.
const manifestStagingPrefix = ".desk-json-"

// publishDeskManifest publishes a new desk: its manifest, written as one
// event. It is staged in `.desk-private` under a name of its own, made 0600 on
// its descriptor, synced, and renamed into place, and the folder is synced. A
// crash at any moment leaves either no manifest or a whole one at its name,
// never a part of one; a stage it leaves is not a manifest.
func publishDeskManifest(folder *os.Root, data []byte) error {
	keyBetween("before staging")
	private, err := folder.OpenRoot(".desk-private")
	if err != nil {
		return err
	}
	defer private.Close()
	stage, err := randomStagingName(manifestStagingPrefix)
	if err != nil {
		return err
	}
	file, err := private.OpenFile(stage, os.O_RDWR|os.O_CREATE|os.O_EXCL|openNoFollow, custodyFileMode)
	if err != nil {
		return err
	}
	// Removed on each failure by name, not by a deferred call, so that a
	// stop at any moment leaves exactly what a crash would.
	failed := func(err error) error {
		file.Close()
		_ = private.Remove(stage)
		return err
	}
	if _, err := file.Write(data); err != nil {
		return failed(err)
	}
	if err := file.Chmod(custodyFileMode); err != nil {
		return failed(err)
	}
	if err := file.Sync(); err != nil {
		return failed(err)
	}
	if err := file.Close(); err != nil {
		return failed(err)
	}
	keyBetween("after staging")
	if err := private.Rename(stage, filepath.Base(deskManifest)); err != nil {
		return failed(err)
	}
	keyBetween("after rename")
	if dir, err := private.Open("."); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

// deskFailure is why a creation stopped, as its answer says it.
type deskFailure struct {
	status  int
	code    string
	message string
}

func storageRefusal(err error) *deskFailure {
	return &deskFailure{statusForRefusal(err), codeOf(err), "Chat storage: " + err.Error()}
}

// runtimeRefusal is a creation the runtime could not complete.
func runtimeRefusal(err error) *deskFailure {
	return &deskFailure{http.StatusInternalServerError, CodeInternal, "The desk was not created: " + strings.TrimRight(err.Error(), ".") + "."}
}

// abandonDesk answers a creation that stopped, after removing what it made.
// Where that cannot be done safely, the folder is left, and the answer and
// Desk's log say where. A folder left so has no manifest: it is not a desk,
// and a start names it and does not count it (`resumeDesks`).
func (s *Server) abandonDesk(w http.ResponseWriter, folder *os.Root, entry string, failure *deskFailure) {
	message := failure.message
	if err := unmakeDeskFolder(folder, entry); err != nil {
		s.log.Printf("desk: a failed creation could not remove its unfinished folder %s: %v", entry, err)
		message += " Its unfinished folder could not be removed safely and was left at " + entry + ". It is not a desk: Desk does not open it or count it toward the desk limit, and it can be removed."
	}
	writeJSONCoded(w, failure.status, failure.code, message)
}

// deskFolderMade is everything a creation makes inside a new desk's folder,
// in the order it is removed: what is in a directory before the directory.
var deskFolderMade = []string{
	deskManifest, runtimeLockName, runtimeConfigName, "jpack-desk.json", ".gitignore",
	deskAuditDir, ".desk-private", ".desk/job-drafts", ".desk", "packs", "sources",
}

// unmakeDeskFolder removes what a failed creation made, and nothing else.
//
// **Nothing is removed recursively, and nothing inside by the registry's
// name.** The name in the registry can be made to point elsewhere while the
// runtime runs: renamed aside, with another desk moved onto it. Removing by
// that name, recursively, deleted the other desk.
//
//   - What this request made is removed through folder, the root opened on
//     the directory it created. That follows the directory wherever it is
//     now, never the name. Each name is removed alone: a directory that now
//     holds anything this request did not put there is not empty, so it is
//     not removed, and neither is what it holds.
//   - The folder's own entry is then removed with rmdir, which removes only
//     an empty directory, and never a file or a link. If the name now holds
//     another desk, it fails, and that desk is untouched.
//
// folder is nil when the directory was made but could not be opened. It is
// then empty, and rmdir alone is enough.
func unmakeDeskFolder(folder *os.Root, entry string) error {
	if folder != nil {
		for _, name := range deskFolderMade {
			if err := folder.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
	}
	return syscall.Rmdir(entry)
}

// holdDeskFolder is a new desk's folder as a directory the runtime can be
// started in: a descriptor through the root that created it, and the pathname
// and identity it was opened at for the hosts that start a child by name.
func holdDeskFolder(folder *os.Root, path string) (heldDir, error) {
	info, err := folder.Stat(".")
	if err != nil {
		return heldDir{}, err
	}
	file, err := folder.Open(".")
	if err != nil {
		return heldDir{}, err
	}
	return heldDir{file: file, path: path, info: info}, nil
}

func (s *Server) resumeDesks() {
	if !s.assistant.usable() {
		return
	}
	s.desksMu.Lock()
	defer s.desksMu.Unlock()
	// Before any desk is opened, the keys of creations a stopped Desk left
	// unfinished.
	s.sweepUnfinishedKeys()
	// Read-only on startup: starting an existing Desk creates no registry.
	root, err := s.assistant.root.OpenRoot("desks")
	if err != nil {
		return
	}
	defer root.Close()
	dir, err := root.Open(".")
	if err != nil {
		return
	}
	defer dir.Close()
	entries, err := dir.ReadDir(registryReadLimit + 1)
	if err != nil && len(entries) == 0 {
		return
	}
	if len(entries) > registryReadLimit {
		s.log.Print("desk: too many entries in the desks folder; registry not opened")
		return
	}
	// **An unfinished folder is not a desk.** A creation that stopped and
	// could not remove what it made safely leaves a folder with no manifest
	// (`abandonDesk`). It is named here, and neither opened nor counted:
	// counted, a few of them would hold the registry shut at the bound.
	var saved []string
	for _, entry := range entries {
		name := entry.Name()
		if !deskIDPattern.MatchString(name) || name == s.cfg.deskID {
			continue
		}
		if _, err := root.Lstat(filepath.Join(name, deskManifest)); errors.Is(err, fs.ErrNotExist) {
			s.log.Printf("desk: %s is an unfinished folder a failed creation left; it is not a desk and was not opened, and it can be removed", filepath.Join(s.configDir, "desks", name))
			continue
		}
		saved = append(saved, name)
	}
	if len(saved) > maxDesks {
		s.log.Print("desk: too many saved desks; registry not opened")
		return
	}
	for _, name := range saved {
		if _, err := s.openDeskLocked(name); err != nil {
			s.log.Printf("desk: could not resume %s: %v", name, err)
		}
	}
	// Once the desks are open, and still before any request is served, the
	// rotations of their keys a stopped Desk left unfinished.
	s.recoverRotations()
}
func (s *Server) closeDesks() {
	s.desksMu.Lock()
	s.desksClosed = true
	children := s.desks
	s.desks = nil
	s.desksMu.Unlock()
	for _, child := range children {
		_ = child.Close()
	}
}
func (s *Server) routeDesk(w http.ResponseWriter, r *http.Request) bool {
	if s.cfg.parent != nil {
		return false
	}
	id := r.Header.Get("X-Jpack-Desk")
	socket := r.URL.Path == "/ws" || r.URL.Path == "/api/agent/run"
	if socket && id == "" {
		id = r.URL.Query().Get("desk")
	}
	if id == "" {
		return false
	}
	// Authenticate before opening a desk or revealing registry information.
	check := r
	if socket {
		if token, refusal := offeredSessionID(r); refusal == "" && token != "" {
			check = r.Clone(r.Context())
			check.Header.Set("Authorization", "Bearer "+token)
		}
	}
	if !s.guard(w, check) {
		return true
	}
	if !deskIDPattern.MatchString(id) {
		writeJSONCoded(w, 400, CodeBadRequest, "Invalid desk identifier.")
		return true
	}
	s.desksMu.Lock()
	child, err := s.openDeskLocked(id)
	s.desksMu.Unlock()
	if err != nil {
		writeJSONCoded(w, 404, CodeNotFound, "This desk could not be opened. Its files have not been changed.")
		return true
	}
	child.mux.ServeHTTP(w, r)
	return true
}

// Event credentials belong to the trigger, not the browser session. Only
// already-open registered desks can receive deliveries, or answer the read of
// a delivery's result, through these routes.
func (s *Server) handleDeskJobEvent(w http.ResponseWriter, r *http.Request) {
	if child := s.eventDesk(w, r); child != nil {
		child.handleJobEvent(w, r)
	}
}
func (s *Server) handleDeskJobEventResult(w http.ResponseWriter, r *http.Request) {
	if child := s.eventDesk(w, r); child != nil {
		child.handleJobEventResult(w, r)
	}
}
func (s *Server) eventDesk(w http.ResponseWriter, r *http.Request) *Server {
	owner := s.deskOwner()
	id := r.PathValue("desk")
	owner.desksMu.Lock()
	child := owner.desks[id]
	owner.desksMu.Unlock()
	if !deskIDPattern.MatchString(id) || child == nil {
		writeJSONCoded(w, 404, CodeNotFound, "Unknown event endpoint.")
		return nil
	}
	return child
}
