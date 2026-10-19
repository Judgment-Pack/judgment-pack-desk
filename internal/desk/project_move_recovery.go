package desk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (s *Server) serveProject(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if strings.HasPrefix(path, "/api/desk-home") || strings.HasPrefix(path, "/api/project-location") || strings.HasPrefix(path, "/api/auth/") || path == "/api/session" || (!strings.HasPrefix(path, "/api/") && path != "/ws") {
		s.mux.ServeHTTP(w, r)
		return
	}
	if s.projectHasMoved() {
		if s.authorized(r) {
			writeJSONCoded(w, 409, CodeStale, projectRestartWords)
		} else {
			writeJSONCoded(w, 401, CodeUnauthorized, "A Desk session is required.")
		}
		return
	}
	if path != "/ws" {
		if !s.location.use.TryRLock() {
			writeJSONCoded(w, 409, CodeStale, "The project is being moved. Wait for the result before continuing.")
			return
		}
		defer s.location.use.RUnlock()
		if s.projectHasMoved() {
			writeJSONCoded(w, 409, CodeStale, projectRestartWords)
			return
		}
	}
	if problem := s.locationStatus().Problem; problem != "" && historyRequestPath(path) {
		if !s.guard(w, r) {
			return
		}
		writeJSONCoded(w, 409, CodeStale, problem)
		return
	}
	if strings.HasPrefix(path, "/api/input-preparations") && r.Method != "GET" {
		enabled, err := s.manualExecutionDemand()
		if err != nil {
			if s.guard(w, r) {
				policyHTTPFailure(w, err)
			}
			return
		}
		if enabled {
			if s.guard(w, r) {
				policyHTTPFailure(w, executionFailure("unsupported-profile"))
			}
			return
		}
	}
	s.mux.ServeHTTP(w, r)
}
func historyRequestPath(path string) bool {
	switch path {
	case "/api/conversations", "/api/draft-graphs", "/api/draft-packs", "/api/briefs", "/api/pack-tests", "/api/source-reviews":
		return true
	}
	return strings.HasPrefix(path, "/api/attachments/")
}
func (s *Server) beginProjectRequest(c *conn, raw []byte) bool {
	if !s.location.use.TryRLock() {
		return false
	}
	defer s.location.use.RUnlock()
	if s.location.moving.Load() || s.projectHasMoved() {
		return false
	}
	var message struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if json.Unmarshal(raw, &message) == nil && message.Method != "" && len(message.ID) > 0 {
		c.requestsMu.Lock()
		defer c.requestsMu.Unlock()
		if c.requests == nil {
			c.requests = map[string]bool{}
		}
		if c.requests[string(message.ID)] || len(c.requests) >= 128 {
			return false
		}
		c.requests[string(message.ID)] = true
	}
	return true
}
func (c *conn) finishProjectRequest(raw []byte) {
	var message struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if json.Unmarshal(raw, &message) == nil && message.Method == "" && len(message.ID) > 0 {
		c.requestsMu.Lock()
		delete(c.requests, string(message.ID))
		c.requestsMu.Unlock()
	}
}

// A launch without an explicit folder can follow an unfinished, installation-
// owned move, but only to the EXACT folder and project identity already moved.
func pendingMovedProject(configDir string) (*projectChoice, error) {
	store := openAssistantStore(configDir)
	defer store.Close()
	if !store.usable() {
		return nil, nil
	}
	intent, found, err := readProjectMove(store.root)
	if err != nil || !found {
		return nil, err
	}
	old, err := os.Stat(intent.Previous)
	if err == nil && identityKey(old) == intent.Folder {
		return nil, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	root, err := OpenProjectRoot(intent.Destination)
	if err != nil {
		return nil, errors.New("An unfinished project move needs recovery at " + intent.Destination)
	}
	defer root.Close()
	if identityKey(root.info) != intent.Folder {
		return nil, errors.New("The moved project folder no longer matches its recovery record.")
	}
	private, _, err := openOwnFolder(root.own.root, []string{startupIdentityDir}, false, startupIdentityKept)
	if err != nil {
		return nil, err
	}
	defer private.Close()
	record, _, found, err := readIdentity(private)
	if err != nil || !found || record.ID != intent.Identity || record.Folder != intent.Folder || (record.Path != intent.Previous && record.Path != intent.Destination) {
		return nil, errors.New("The moved project's identity could not be confirmed.")
	}
	// A deliberate later startup preference is never overwritten by recovery.
	present, data, err := store.readConfigFile()
	if err != nil {
		return nil, err
	}
	revision := ""
	if present {
		revision = digestOf(data)
	}
	if revision != intent.ConfigRevision && (!present || decodeDeskFile(data).ProjectFile != filepath.Join(intent.Destination, projectConfigName)) {
		return nil, errors.New("Startup settings changed during a project move. Open the moved project explicitly at " + intent.Destination + " and review the installation settings.")
	}
	chosen, err := usableProjectDir(filepath.Join(intent.Destination, projectConfigName))
	if err != nil {
		return nil, err
	}
	if !os.SameFile(root.info, chosen.dirInfo) {
		return nil, errors.New("The move destination changed during recovery. Nothing was opened.")
	}
	return &chosen, nil
}

func (s *Server) recoverProjectMove() {
	if s.cfg.deskID != "" || !s.assistant.usable() {
		return
	}
	intent, found, err := readProjectMove(s.assistant.root)
	if err != nil {
		s.setHistoryMoveProblem(err.Error())
		return
	}
	if !found {
		return
	}
	// A different project's pending move must not affect this project.
	if s.projectDir != intent.Previous && s.projectDir != intent.Destination {
		return
	}
	unlock, err := s.lockProject(context.Background(), 0)
	if err != nil {
		s.setHistoryMoveProblem("Project move recovery is waiting for the project lock. Restart Desk after active work finishes.")
		return
	}
	defer unlock()
	if s.folderKey() != intent.Folder {
		s.setHistoryMoveProblem("The project does not match the unfinished move. Recovery information was kept.")
		return
	}
	work, err := s.privateDataFileLock(".data-work.lock", true)
	if err != nil {
		s.setHistoryMoveProblem(err.Error())
		return
	}
	defer work.Close()
	s.writes.Lock()
	defer s.writes.Unlock()
	if s.projectDir == intent.Previous {
		// Intent saved, rename never happened. Leave the existing project in place.
		if _, err = os.Lstat(intent.Destination); !errors.Is(err, os.ErrNotExist) {
			s.setHistoryMoveProblem("The move destination is occupied. Recovery information was kept for inspection.")
			return
		}
		if err = s.assistant.root.Remove(projectMoveName); err != nil {
			s.setHistoryMoveProblem(err.Error())
		}
		return
	}
	if s.locationStatus().Problem != "" {
		return
	}
	// resolveStartupIdentity already linked history, preserving normal move/copy
	// decisions. Do not finish a move whose identity is unresolved or shared.
	private, err := s.openIdentityFolder()
	if err != nil {
		s.setHistoryMoveProblem(err.Error())
		return
	}
	defer private.Close()
	record, _, found, err := readIdentity(private)
	if err != nil || !found || record.ID != intent.Identity || record.Path != intent.Destination || s.startupShared() || s.startupUnresolved() {
		s.setHistoryMoveProblem("Resolve the moved project's identity before finishing its startup settings.")
		return
	}
	if err = s.finishProjectMoveConfig(intent); err != nil {
		s.setHistoryMoveProblem(err.Error())
	}
}
