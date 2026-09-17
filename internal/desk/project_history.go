package desk

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const projectBindingsName = "project-bindings.json"
const maxProjectBindingsBytes = 128 << 10

type projectBindings struct {
	Version  int               `json:"version"`
	Projects map[string]string `json:"projects"`
}
type projectHistoryRequest struct {
	PreviousProject  string `json:"previousProject"`
	SourceRevision   string `json:"sourceRevision"`
	BindingsRevision string `json:"bindingsRevision"`
}
type projectHistoryPreview struct {
	PreviousProject  string `json:"previousProject"`
	Project          string `json:"project"`
	ChatCount        int    `json:"chatCount"`
	SourceRevision   string `json:"sourceRevision"`
	BindingsRevision string `json:"bindingsRevision"`
}

func decodeProjectBindings(data []byte) (projectBindings, error) {
	var value projectBindings
	if len(data) > maxProjectBindingsBytes {
		return value, errors.New("project history bindings exceed their limit")
	}
	if err := decodeDataJSON(data, &value); err != nil {
		return value, err
	}
	if value.Version != 1 || value.Projects == nil || len(value.Projects) > maxStorageFiles {
		return value, errors.New("unsupported project history bindings")
	}
	for path, name := range value.Projects {
		if !validProjectHistoryPath(path) || !conversationFileName.MatchString(name) {
			return value, errors.New("invalid project history binding")
		}
	}
	return value, nil
}
func readProjectBindings(root *os.Root) (projectBindings, string, error) {
	data, err := readPrivateData(root, projectBindingsName, maxProjectBindingsBytes)
	if errors.Is(err, os.ErrNotExist) {
		return projectBindings{Version: 1, Projects: map[string]string{}}, "absent", nil
	}
	if err != nil {
		return projectBindings{}, "", err
	}
	value, err := decodeProjectBindings(data)
	return value, digestOf(data), err
}
func validProjectHistoryPath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.ContainsAny(path, "\x00\r\n")
}
func resolveConversationName(root *os.Root, project string) (string, error) {
	bindings, _, err := readProjectBindings(root)
	if err != nil {
		return "", err
	}
	if name, ok := bindings.Projects[project]; ok {
		if _, err := root.Lstat(name); err != nil {
			return "", errors.New("the linked project history is missing; restore its backup before continuing")
		}
		return name, nil
	}
	return "conversations-" + digestOf([]byte(project)) + ".json", nil
}
func validateStoredBindings(root *os.Root) error {
	bindings, _, err := readProjectBindings(root)
	if err != nil {
		return err
	}
	for _, name := range bindings.Projects {
		info, err := root.Lstat(name)
		if err != nil {
			return errors.New("a project history binding points to a missing record")
		}
		if err = ownerOnlyFile(name, info.Mode()); err != nil {
			return err
		}
		if err = ownedByUs(name, info); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) handleProjectHistory(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) || s.refuseUnusableStore(w) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	data, err := readBounded(r.Body, 16<<10)
	var request projectHistoryRequest
	if err != nil || decodeDataJSON(data, &request) != nil || !validProjectHistoryPath(request.PreviousProject) || request.PreviousProject == s.projectDir {
		writeJSONCoded(w, 400, CodeBadRequest, "Enter the absolute folder path this project used previously.")
		return
	}
	work, err := s.privateDataFileLock(".data-work.lock", true)
	if err != nil {
		storageFailure(w, err)
		return
	}
	defer work.Close()
	s.writes.Lock()
	defer s.writes.Unlock()
	lock, err := s.privateDataLock(true)
	if err != nil {
		storageFailure(w, err)
		return
	}
	defer lock.Close()
	store, err := s.openChatData()
	if err != nil {
		storageFailure(w, err)
		return
	}
	defer store.root.Close()
	bindings, revision, err := readProjectBindings(store.root)
	if err != nil {
		storageFailure(w, err)
		return
	}
	// Relinking preserves one record, rather than guessing that two copies are
	// the same project. Never merge with history already at this location.
	if _, exists := bindings.Projects[s.projectDir]; exists {
		writeJSONCoded(w, 409, CodeStale, "This project already has linked history.")
		return
	}
	if _, err := store.root.Lstat(s.conversationName()); !errors.Is(err, os.ErrNotExist) {
		writeJSONCoded(w, 409, CodeStale, "This project already has saved history; it cannot be combined automatically.")
		return
	}
	source, err := resolveConversationName(store.root, request.PreviousProject)
	if err != nil {
		storageFailure(w, err)
		return
	}
	history, err := s.readConversationFile(store.root, source)
	if err != nil {
		storageFailure(w, err)
		return
	}
	if history.SHA256 == "absent" {
		writeJSONCoded(w, 404, CodeNotFound, "No saved chats were found for that previous project folder.")
		return
	}
	var doc conversationDocument
	_ = json.Unmarshal(history.Content, &doc)
	preview := projectHistoryPreview{PreviousProject: request.PreviousProject, Project: s.projectDir, ChatCount: len(doc.Chats), SourceRevision: history.SHA256, BindingsRevision: revision}
	if strings.HasSuffix(r.URL.Path, "/preview") {
		writeJSON(w, 200, preview)
		return
	}
	if request.SourceRevision != history.SHA256 || request.BindingsRevision != revision {
		writeJSONCoded(w, 409, CodeStale, "Project history changed. Preview it again before linking.")
		return
	}
	bindings.Projects[s.projectDir] = source
	data, _ = json.Marshal(bindings)
	if _, err = decodeProjectBindings(data); err != nil {
		storageFailure(w, err)
		return
	}
	if err = writePrivateData(store.root, projectBindingsName, data); err != nil {
		storageFailure(w, err)
		return
	}
	writeJSON(w, 200, preview)
}
