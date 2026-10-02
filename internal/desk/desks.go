package desk

// Desks are complete, isolated project folders. The registry only creates
// fresh folders below the installation-owned desks directory; no browser
// supplied path can widen the file API's authority. Existing projects keep
// their established storage locations until explicitly migrated.
import (
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
const (
	deskAuditDir               = ".desk-private/audit"
	gatedDeskConfig            = `{"configVersion":"5","requireReviewed":true,"requireComparableFacts":true,"audit":{"dir":".desk-private/audit"},"packs":{}}` + "\n"
	gatedDeskConfigVersion4    = `{"configVersion":"4","requireReviewed":true,"audit":{"dir":".desk-private/audit"},"packs":{}}` + "\n"
	comparableFactsFromVersion = "5"
	reviewedFromVersion        = "4"
)

// deskGates is the configuration a new desk is written with, and what the
// creation says about it.
type deskGates struct {
	config                 []byte
	configVersion          string
	requireComparableFacts bool
	notice                 string
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
	data, err := readPrivateData(s.root, deskManifest, 4096)
	if err != nil {
		return row, err
	}
	var saved struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if json.Unmarshal(data, &saved) != nil || saved.ID != s.cfg.deskID || !validDeskName(saved.Name) {
		return row, errors.New("desk metadata is invalid")
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
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
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
	data, err := readPrivateData(pinned.own.root, deskManifest, 4096)
	var manifest struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err != nil || json.Unmarshal(data, &manifest) != nil || manifest.ID != id || !validDeskName(manifest.Name) {
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
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		storageFailure(w, err)
		return
	}
	id := hex.EncodeToString(random[:])
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
		return s.makeDeskFolder(r.Context(), folder, entry)
	}()
	s.deskCreations--
	if failure != nil {
		s.abandonDesk(w, folder, entry, failure)
		return
	}
	if s.desksClosed {
		s.abandonDesk(w, folder, entry, &deskFailure{http.StatusConflict, CodeBadRequest, "Desk is shutting down, so the desk was not created."})
		return
	}
	// Metadata is written last. Until it is, the folder is not a desk.
	manifest, _ := json.Marshal(struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}{id, request.Name})
	if err = folder.WriteFile(deskManifest, manifest, 0600); err != nil {
		s.abandonDesk(w, folder, entry, storageRefusal(err))
		return
	}
	if gates.notice != "" {
		s.log.Printf("desk: %s", gates.notice)
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
		Notice                 string `json:"notice,omitempty"`
	}{record, gates.configVersion, gates.requireComparableFacts, gates.notice})
}

// makeDeskFolder makes a new desk's folder, through folder, the root this
// request opened when it made the directory: the folders, the configuration
// the runtime can hold the desk to, and the runtime's lock of it. It holds no
// lock, and writes no manifest.
func (s *Server) makeDeskFolder(ctx context.Context, folder *os.Root, entry string) (deskGates, *deskFailure) {
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
	for name, body := range map[string]string{
		runtimeConfigName: string(gates.config),
		"jpack-desk.json": "{\"deskConfigVersion\":1}\n",
		".gitignore":      ".desk-private/\n",
	} {
		if err = folder.WriteFile(name, []byte(body), 0600); err != nil {
			return deskGates{}, storageRefusal(err)
		}
	}
	if err = lockRuntimeProject(ctx, bin, held, folder, gates.config); err != nil {
		return deskGates{}, runtimeRefusal(err)
	}
	return gates, nil
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
