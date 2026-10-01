package desk

// Desks are complete, isolated project folders. The registry only creates
// fresh folders below the installation-owned desks directory; no browser
// supplied path can widen the file API's authority. Existing projects keep
// their established storage locations until explicitly migrated.
import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

var deskIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

const deskManifest = ".desk-private/desk.json"

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
	owner.desksMu.Lock()
	defer owner.desksMu.Unlock()
	if r.Method == http.MethodPost {
		if owner.desksClosed || len(owner.desks) >= 256 {
			writeJSONCoded(w, http.StatusConflict, CodeBadRequest, "Desk creation is unavailable while shutting down or at the 256 desk limit.")
			return
		}
		var request struct {
			Name string `json:"name"`
		}
		data, err := readBounded(r.Body, 4096)
		if err != nil || decodeDataJSON(data, &request) != nil || !validDeskName(request.Name) {
			writeJSONCoded(w, 400, CodeBadRequest, "Enter a desk name between 1 and 80 characters.")
			return
		}
		root, err := owner.desksRoot()
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
		folder, err := root.OpenRoot(id)
		if err != nil {
			storageFailure(w, err)
			return
		}
		defer folder.Close()
		// Metadata is written last. Failed initialization never appears as a desk.
		for _, dir := range []string{"packs", "sources", ".desk", ".desk/job-drafts", ".desk-private"} {
			if err = folder.Mkdir(dir, 0700); err != nil {
				storageFailure(w, err)
				return
			}
		}
		for name, body := range map[string]string{
			"jpack.json":      "{\"configVersion\":\"3\",\"packs\":{}}\n",
			"jpack-desk.json": "{\"deskConfigVersion\":1}\n",
			".gitignore":      ".desk-private/\n",
		} {
			if err = folder.WriteFile(name, []byte(body), 0600); err != nil {
				storageFailure(w, err)
				return
			}
		}
		manifest, _ := json.Marshal(struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}{id, request.Name})
		if err = folder.WriteFile(deskManifest, manifest, 0600); err != nil {
			storageFailure(w, err)
			return
		}
		child, err := owner.openDeskLocked(id)
		if err != nil {
			storageFailure(w, err)
			return
		}
		record, err := child.deskRecord()
		if err != nil {
			storageFailure(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, record)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
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
	entries, err := dir.ReadDir(257)
	if err != nil && len(entries) == 0 {
		return
	}
	if len(entries) > 256 {
		s.log.Print("desk: too many saved desks; registry not opened")
		return
	}
	for _, entry := range entries {
		if !deskIDPattern.MatchString(entry.Name()) || entry.Name() == s.cfg.deskID {
			continue
		}
		if _, err := s.openDeskLocked(entry.Name()); err != nil {
			s.log.Printf("desk: could not resume %s: %v", entry.Name(), err)
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
