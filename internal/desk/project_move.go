package desk

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const projectMoveName = "project-move.json"
const projectRestartWords = "The startup project has moved. Restart Desk at its new location before continuing."

type projectLocationState struct {
	use     sync.RWMutex
	mu      sync.Mutex
	moving  atomic.Bool
	picking atomic.Bool
	moved   string
	problem string
}
type projectLocation struct {
	Path               string `json:"path"`
	DefaultDestination string `json:"defaultDestination"`
	RestartRequired    bool   `json:"restartRequired"`
	Moving             bool   `json:"moving"`
	MovedTo            string `json:"movedTo,omitempty"`
	Problem            string `json:"problem,omitempty"`
}
type projectMoveRequest struct {
	Destination string `json:"destination"`
	Token       string `json:"token"`
}
type projectMovePreview struct {
	Project        string `json:"project"`
	Destination    string `json:"destination"`
	Token          string `json:"token"`
	parentIdentity string
	parentPath     string
	CreateParent   bool `json:"createParent,omitempty"`
}

// The intent is owner-only installation data. It retains the old path even if
// a crash occurs between rename, history binding, identity and startup config.
// It contains no credential, decision data or rewritten evidence.
type projectMoveIntent struct {
	Version        int    `json:"version"`
	Previous       string `json:"previous"`
	Destination    string `json:"destination"`
	Folder         string `json:"folder"`
	Identity       string `json:"identity"`
	ConfigRevision string `json:"configRevision"`
}

func (s *Server) setHistoryMoveProblem(problem string) {
	s.location.mu.Lock()
	s.location.problem = problem
	s.location.mu.Unlock()
}
func (s *Server) locationStatus() projectLocation {
	s.location.mu.Lock()
	defer s.location.mu.Unlock()
	return projectLocation{Path: s.projectDir, DefaultDestination: s.defaultProjectDestination(), Moving: s.location.moving.Load(), RestartRequired: s.location.moved != "", MovedTo: s.location.moved, Problem: s.location.problem}
}
func (s *Server) projectHasMoved() bool { return s.locationStatus().RestartRequired }
func (s *Server) markProjectMoved(destination string) {
	s.location.mu.Lock()
	s.location.moved = destination
	s.location.mu.Unlock()
}

func (s *Server) handleProjectLocation(w http.ResponseWriter, r *http.Request) {
	// This installation-wide card always names home, never a managed desk.
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, s.locationStatus())
}

func (s *Server) moveSnapshot(destination string) (projectMovePreview, error) {
	preview := projectMovePreview{Project: s.projectDir, Destination: destination}
	if s.cfg.PolicyAuthority != nil {
		return preview, policyMachineRefusal()
	}
	if s.home != nil {
		return preview, errors.New("This project belongs to Desk home. Use Admin > General > Desk home to move its files and history together.")
	}
	if len((identityRecord{ID: strings.Repeat("0", 64), Path: destination, Folder: s.folderKey()}).line()) > startupIdentityLimit {
		return preview, errors.New("Choose a shorter destination path so the project identity can be saved.")
	}
	if s.cfg.deskID != "" {
		return preview, errors.New("Named desks keep their managed locations.")
	}
	if s.projectHasMoved() {
		return preview, errors.New(projectRestartWords)
	}
	if s.locationStatus().Problem != "" {
		return preview, errors.New(s.locationStatus().Problem)
	}
	if !validProjectHistoryPath(destination) || len(destination) > 2048 || destination == s.projectDir || destination == string(filepath.Separator) {
		return preview, errors.New("Enter an unused absolute folder path.")
	}
	parent, err := s.projectDestinationParent(destination)
	if err != nil {
		return preview, err
	}
	defer parent.Close()
	preview.parentPath = parent.dir
	preview.CreateParent = parent.dir != filepath.Dir(destination)
	root := parent.own.root
	if _, err = root.Lstat(relativeMoveDestination(parent.dir, destination)); !errors.Is(err, os.ErrNotExist) {
		return preview, errors.New("The destination already exists or cannot be checked. Choose an unused folder.")
	}
	here, err := s.root.Stat(".")
	if err != nil {
		return preview, err
	}
	if !sameDirectory(s.projectDir, here) {
		return preview, errors.New("The startup project folder changed. Restart Desk before moving it.")
	}
	there, err := root.Stat(".")
	if err != nil {
		return preview, err
	}
	preview.parentIdentity = identityKey(there)
	if err = safeMoveParent(there); err != nil {
		return preview, err
	}
	if err = checkMoveFilesystem(here, there); err != nil {
		return preview, err
	}
	if pathContains(s.projectDir, destination) || pathContains(destination, s.projectDir) || pathContains(s.configDir, destination) || pathContains(destination, s.configDir) || pathContains(s.projectDir, s.configDir) {
		return preview, errors.New("Choose a folder separate from the project and Desk installation data.")
	}
	if s.startupShared() || s.startupUnresolved() {
		return preview, errors.New("Resolve this project's copy or move identity in Decision safeguards before moving it.")
	}
	present, config, err := s.readDeskFile()
	if err != nil {
		return preview, err
	}
	if present && decodeDeskFile(config).refused() {
		return preview, errors.New("Fix the Desk configuration before moving this project.")
	}
	private, err := s.openIdentityFolder()
	identity := "absent"
	if err == nil {
		defer private.Close()
		record, _, found, e := readIdentity(private)
		if e != nil {
			return preview, e
		}
		if found {
			identity = string(record.line())
			if record.Path != s.projectDir || record.Folder != s.folderKey() || record.From != "" {
				return preview, errors.New("Finish the project's identity recovery before moving it.")
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return preview, err
	}
	lock, err := s.privateDataLock(true)
	if err != nil {
		return preview, err
	}
	defer lock.Close()
	store, err := s.openChatData()
	if err != nil {
		return preview, err
	}
	defer store.root.Close()
	if pathContains(destination, store.location.Path) || pathContains(store.location.Path, destination) || pathContains(s.projectDir, store.location.Path) {
		return preview, errors.New("Choose a folder separate from private chat storage.")
	}
	_, _, _, err = movedHistoryBinding(store.root, s.projectDir, destination)
	if err != nil {
		return preview, err
	}
	_, bindingsRev, err := readProjectBindings(store.root)
	if err != nil {
		return preview, err
	}
	projectConfig, e := s.readReviewFileWithin(projectConfigName, maxDeskConfigBytes)
	if e != nil && codeOf(e) != CodeNotFound {
		return preview, e
	}
	mac := hmac.New(sha256.New, s.reviewKey[:])
	fmt.Fprintf(mac, "project-move\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s", s.projectDir, destination, identityKey(here), parent.dir+"\x00"+identityKey(there), identity, digestOf(config), bindingsRev, digestOf(projectConfig))
	preview.Token = hex.EncodeToString(mac.Sum(nil))
	return preview, nil
}

func (s *Server) handleProjectMove(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) || s.refuseUnusableStore(w) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var request projectMoveRequest
	raw, err := readBounded(r.Body, 16<<10)
	if err != nil || decodeDataJSON(raw, &request) != nil {
		writeJSONCoded(w, 400, CodeBadRequest, "Enter a destination folder.")
		return
	}
	if strings.HasSuffix(r.URL.Path, "/preview") {
		s.writes.Lock()
		preview, err := s.moveSnapshot(request.Destination)
		s.writes.Unlock()
		if err == nil {
			err = s.projectMoveIdle(r.Context())
		}
		if err != nil {
			writeJSONCoded(w, 409, CodeStale, err.Error())
			return
		}
		writeJSON(w, 200, preview)
		return
	}
	if !s.location.use.TryLock() {
		writeJSONCoded(w, 409, CodeStale, "Finish active project work before moving the folder.")
		return
	}
	defer s.location.use.Unlock()
	s.location.moving.Store(true)
	defer s.location.moving.Store(false)
	unlock, err := s.lockProject(r.Context(), 0)
	if err != nil {
		writeJSONCoded(w, 409, CodeStale, "The project is busy in another operation. Finish it before moving.")
		return
	}
	defer unlock()
	work, err := s.privateDataFileLock(".data-work.lock", true)
	if err != nil {
		storageFailure(w, err)
		return
	}
	defer work.Close()
	s.writes.Lock()
	defer s.writes.Unlock()
	preview, err := s.moveSnapshot(request.Destination)
	if err == nil && (request.Token == "" || !hmac.Equal([]byte(request.Token), []byte(preview.Token))) {
		err = errors.New("Project settings changed. Review the move again.")
	}
	if err == nil {
		err = s.projectMoveIdle(r.Context())
	}
	if err != nil {
		writeJSONCoded(w, 409, CodeStale, err.Error())
		return
	}
	parentIdentity, err := prepareProjectDestination(preview)
	if err == nil {
		err = s.moveProjectFolder(request.Destination, parentIdentity)
	}
	if err != nil {
		if s.projectHasMoved() {
			s.setHistoryMoveProblem("The folder moved, but setup is incomplete. Restart Desk at the new location to finish recovery. " + err.Error())
			writeJSON(w, 200, s.locationStatus())
			return
		}
		writeJSONCoded(w, 409, CodeStale, err.Error())
		return
	}
	writeJSON(w, 200, s.locationStatus())
}

// Requests and background preparation hold use while working. Idle MCP sockets
// are disconnected only at commit; an outstanding tool call blocks the move.
func (s *Server) projectMoveIdle(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.conns {
		c.requestsMu.Lock()
		busy := c.silent || len(c.requests) > 0
		c.requestsMu.Unlock()
		if busy {
			return errors.New("Finish active assistant and runtime work before moving the project.")
		}
	}
	if s.stamping != nil && s.stamping.running.Load() {
		return errors.New("Wait for decision-record stamping to finish before moving the project.")
	}
	if s.jobs != nil {
		// Runner's workspace/key identity is path-scoped. Moving that namespace is
		// outside #388; do not strand live jobs or silently grant a new workspace
		// the old signing key. An empty Runner is safe to stop at commit.
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		raw, err := s.inputRunner(ctx, "jobs", nil, "")
		if err != nil {
			return errors.New("Jobs could not be checked. Retry after Runner is available.")
		}
		var page struct {
			Items []json.RawMessage `json:"items"`
			Next  int               `json:"next"`
		}
		if json.Unmarshal(raw, &page) != nil || page.Items == nil {
			return errors.New("Jobs could not be checked safely.")
		}
		if len(page.Items) > 0 || page.Next != 0 {
			return errors.New("This startup project has Runner jobs. Moving Runner workspaces is not supported yet; keep this project at its current location.")
		}
	}
	return nil
}

func readProjectMove(root *os.Root) (projectMoveIntent, bool, error) {
	raw, err := readPrivateData(root, projectMoveName, 16<<10)
	if errors.Is(err, os.ErrNotExist) {
		return projectMoveIntent{}, false, nil
	}
	var intent projectMoveIntent
	if err == nil {
		err = decodeDataJSON(raw, &intent)
	}
	if err == nil && (intent.Version != 1 || !validProjectHistoryPath(intent.Previous) || !validProjectHistoryPath(intent.Destination) || intent.Previous == intent.Destination || intent.Folder == "" || !startupIDForm.MatchString(intent.Identity) || !fileIdentityForm.MatchString(intent.Folder) || intent.ConfigRevision != "" && !startupIDForm.MatchString(intent.ConfigRevision)) {
		err = errors.New("Project move recovery data is invalid.")
	}
	return intent, true, err
}

// Called under the project, work and writes locks. Native no-replace rename
// prevents an occupied destination from being replaced even after validation.
func (s *Server) moveProjectFolder(destination, parentIdentity string) error {
	if _, found, err := readProjectMove(s.assistant.root); err != nil {
		return err
	} else if found {
		return errors.New("An earlier project move needs recovery. Restart Desk first.")
	}
	private, _, err := openOwnFolder(s.root, []string{startupIdentityDir}, true, startupIdentityKept)
	if err != nil {
		return err
	}
	defer private.Close()
	record, info, found, err := readIdentity(private)
	if err != nil {
		return err
	}
	if !found {
		// Use the existing identity chooser; never invent an identity for a project
		// whose signing key already names one. Those legacy projects need the
		// established identity resolution flow first.
		dir, e := s.assistant.openSigning(false)
		if e != nil && !errors.Is(e, errNoSigningDir) {
			return e
		}
		if dir != nil {
			defer dir.Close()
			unlockSigning, e := lockSigning(dir)
			if e != nil {
				return e
			}
			defer unlockSigning()
		}
		record, err = s.chooseStartupIdentity(dir)
		if err != nil {
			return err
		}
		if record.Folder == "" || record.From != "" {
			return errors.New("Restart Desk and resolve its project identity in Decision safeguards before moving it.")
		}
		info, err = writeIdentity(private, record, nil)
		if err != nil {
			return err
		}
	}
	if record.Path != s.projectDir || record.Folder != s.folderKey() {
		return errors.New("Project identity changed. Review the move again.")
	}
	if _, err = s.root.Lstat(projectConfigName); errors.Is(err, os.ErrNotExist) {
		err = claimPrivateData(s.root, projectConfigName, []byte("{\"deskConfigVersion\":1}\n"))
	}
	if err != nil {
		return err
	}
	present, config, err := s.readDeskFile()
	if err != nil {
		return err
	}
	rev := ""
	if present {
		rev = digestOf(config)
	}
	intent := projectMoveIntent{Version: 1, Previous: s.projectDir, Destination: destination, Folder: record.Folder, Identity: record.ID, ConfigRevision: rev}
	data, _ := json.Marshal(intent)
	if err = claimPrivateData(s.assistant.root, projectMoveName, data); err != nil {
		return err
	}
	if testProjectMoveStep != nil {
		if err = testProjectMoveStep("before-rename"); err != nil {
			return err
		}
	}
	if err = renameProjectNoReplace(s, destination, parentIdentity); err != nil {
		if !s.projectHasMoved() {
			_ = s.assistant.root.Remove(projectMoveName)
		}
		return err
	}
	s.markProjectMoved(destination)
	// From this point on keep the recovery intent on ANY error. Never guess that
	// an uncertain config write failed, roll files back, or replace a new folder.
	s.mu.Lock()
	for c := range s.conns {
		c.cancel()
		c.stop()
	}
	s.mu.Unlock()
	if s.jobs != nil {
		s.jobs.close()
	}
	if testProjectMoveStep != nil {
		if err = testProjectMoveStep("after-rename"); err != nil {
			return err
		}
	}
	lock, err := s.privateDataLock(true)
	if err != nil {
		return err
	}
	defer lock.Close()
	store, err := s.openChatData()
	if err != nil {
		return err
	}
	defer store.root.Close()
	if err = linkMovedHistory(store.root, intent.Previous, intent.Destination); err != nil {
		return err
	}
	record.Path = destination
	if _, err = writeIdentity(private, record, info); err != nil {
		return err
	}
	return s.finishProjectMoveConfig(intent)
}

var testProjectMoveStep func(string) error

func (s *Server) finishProjectMoveConfig(intent projectMoveIntent) error {
	if !sameDirectory(intent.Destination, s.project.info) {
		return errors.New("The move destination changed. Recovery information was kept.")
	}
	project, _ := json.Marshal(map[string]string{"file": filepath.Join(intent.Destination, projectConfigName)})
	present, current, err := s.readDeskFile()
	if err != nil {
		return err
	}
	if !present || decodeDeskFile(current).refused() || decodeDeskFile(current).ProjectFile != filepath.Join(intent.Destination, projectConfigName) {
		status, _ := s.commitDeskConfigLocked(DeskConfigWrite{Project: project, IfMatch: &intent.ConfigRevision})
		if status != 200 && status != 201 {
			return errors.New("Startup settings changed or could not be saved. The project folder and recovery information are retained.")
		}
	}
	if !sameDirectory(intent.Destination, s.project.info) {
		return errors.New("The move destination changed while saving. Recovery information was kept.")
	}
	if err = s.assistant.root.Remove(projectMoveName); err != nil {
		return err
	}
	return syncPrivateDirectory(s.assistant.root)
}

func safeMoveParent(info os.FileInfo) error {
	if !info.IsDir() || info.Mode().Perm()&0022 != 0 || ownedByUs("project parent", info) != nil {
		return errors.New("Use parent folders owned by you that other users cannot modify.")
	}
	return nil
}
