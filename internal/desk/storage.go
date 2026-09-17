package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

const (
	dataLocationName = "data-location.json"
	dataMarkerName   = ".jpack-data.json"
	maxStorageFiles  = 4096
	maxMoveBytes     = 1 << 30
)

var conversationFileName = regexp.MustCompile(`^conversations-[a-f0-9]{64}\.json$`)

type dataLocation struct {
	Version  int    `json:"version"`
	Path     string `json:"path"`
	Previous string `json:"previous,omitempty"`
}
type dataMarker struct {
	Version int    `json:"version"`
	Owner   string `json:"owner"`
}
type chatDataStore struct {
	root     *os.Root
	location dataLocation
	revision string
	legacy   bool
}
type storageStatus struct {
	Path            string `json:"path"`
	RecommendedPath string `json:"recommendedPath"`
	Revision        string `json:"revision"`
	Legacy          bool   `json:"legacy"`
	PreviousPath    string `json:"previousPath,omitempty"`
	ProjectCount    int    `json:"projectCount"`
	Bytes           int64  `json:"bytes"`
	ProjectBytes    int64  `json:"projectBytes"`
	Scope           string `json:"scope"`
	Problem         string `json:"problem,omitempty"`
	MaxMoveBytes    int64  `json:"maxMoveBytes"`
	MaxBackupBytes  int64  `json:"maxBackupBytes"`
}

func (s *Server) defaultChatDataPath() string {
	// Keep hermetic server fixtures inside their supplied private root.
	if s.cfg.DeskConfigDir != "" {
		return filepath.Join(s.configDir, "data")
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(xdg) {
		return filepath.Join(xdg, deskDirName)
	}
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", deskDirName)
	}
	if runtime.GOOS == "windows" {
		if local := os.Getenv("LOCALAPPDATA"); filepath.IsAbs(local) {
			return filepath.Join(local, deskDirName)
		}
	}
	return filepath.Join(home, ".local", "share", deskDirName)
}

func storageEntries(root *os.Root) ([]os.DirEntry, error) {
	dir, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(maxStorageFiles + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(entries) > maxStorageFiles {
		return nil, withCode(CodeTooLarge, errors.New("chat storage contains too many entries to inspect safely"))
	}
	return entries, nil
}

func (s *Server) storageOwner() string { return digestOf([]byte(s.configDir)) }

// Read the storage pointer under the cross-process lock on every operation.
// Another Desk may have moved the store since this server's last request.
func (s *Server) openChatData() (*chatDataStore, error) {
	data, err := readPrivateData(s.assistant.root, dataLocationName, 16<<10)
	if errors.Is(err, os.ErrNotExist) {
		entries, err := storageEntries(s.assistant.root)
		if err != nil {
			return nil, err
		}
		path := s.defaultChatDataPath()
		for _, entry := range entries {
			if conversationFileName.MatchString(entry.Name()) {
				path = s.configDir
				break
			}
		}
		location := dataLocation{Version: 1, Path: path}
		store, err := s.openChatDataAt(location, "", path != s.configDir)
		if err != nil {
			return nil, err
		}
		data, _ = json.Marshal(location)
		if err = writePrivateData(s.assistant.root, dataLocationName, data); err != nil {
			store.root.Close()
			return nil, err
		}
		store.revision = digestOf(data)
		return store, nil
	}
	if err != nil {
		return nil, err
	}
	var location dataLocation
	if err = decodeDataJSON(data, &location); err != nil || location.Version != 1 || !filepath.IsAbs(location.Path) || filepath.Clean(location.Path) != location.Path {
		return nil, errors.New("chat storage settings are invalid; the existing files have not been changed")
	}
	return s.openChatDataAt(location, digestOf(data), false)
}

func (s *Server) openChatDataAt(location dataLocation, revision string, create bool) (*chatDataStore, error) {
	if location.Path != s.configDir && pathContains(s.projectDir, location.Path) {
		return nil, errors.New("private chat storage must be outside the project")
	}
	var root *os.Root
	var err error
	if location.Path == s.configDir {
		root, err = s.assistant.root.OpenRoot(".")
	} else {
		root, err = openPrivateDataRoot(location.Path, create)
	}
	if err != nil {
		return nil, err
	}
	store := &chatDataStore{root: root, location: location, revision: revision, legacy: location.Path == s.configDir}
	if store.legacy {
		return store, nil
	}
	markerBytes, err := readPrivateData(root, dataMarkerName, 4096)
	if errors.Is(err, os.ErrNotExist) && create {
		entries, listErr := storageEntries(root)
		if listErr != nil || len(entries) != 0 {
			root.Close()
			if listErr != nil {
				return nil, listErr
			}
			return nil, errors.New("choose an empty folder for chat data; existing files will not be combined")
		}
		markerBytes, _ = json.Marshal(dataMarker{Version: 1, Owner: s.storageOwner()})
		err = claimPrivateData(root, dataMarkerName, markerBytes)
	}
	var marker dataMarker
	if err == nil {
		err = decodeDataJSON(markerBytes, &marker)
	}
	if err == nil && (marker.Version != 1 || marker.Owner != s.storageOwner()) {
		err = errors.New("this folder belongs to a different Desk data store")
	}
	if err != nil {
		root.Close()
		return nil, err
	}
	return store, nil
}

func pathContains(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (s *Server) describeStorage(store *chatDataStore) (storageStatus, error) {
	status := storageStatus{Path: store.location.Path, RecommendedPath: s.defaultChatDataPath(), Revision: store.revision,
		Legacy: store.legacy, PreviousPath: store.location.Previous, Scope: "personal", MaxMoveBytes: maxMoveBytes, MaxBackupBytes: maxBackupData}
	entries, err := storageEntries(store.root)
	if err != nil {
		return status, err
	}
	recordName, err := resolveConversationName(store.root, s.projectDir)
	if err != nil {
		status.Problem = err.Error()
	}
	for _, entry := range entries {
		if !chatStorageFile(entry.Name()) {
			continue
		}
		info, err := store.root.Lstat(entry.Name())
		if err != nil {
			return status, err
		}
		if err = ownerOnlyFile(entry.Name(), info.Mode()); err != nil {
			status.Problem = err.Error()
			continue
		}
		if err = ownedByUs(entry.Name(), info); err != nil {
			status.Problem = err.Error()
			continue
		}
		if conversationFileName.MatchString(entry.Name()) {
			status.ProjectCount++
		}
		status.Bytes += info.Size()
		if entry.Name() == recordName {
			status.ProjectBytes = info.Size()
		}
	}
	return status, nil
}

func (s *Server) handleStorage(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) || s.refuseUnusableStore(w) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
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
	status, err := s.describeStorage(store)
	if err != nil {
		storageFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func storageFailure(w http.ResponseWriter, err error) {
	writeJSONCoded(w, statusForRefusal(err), codeOf(err), "Chat storage: "+err.Error())
}

type moveChatData struct {
	Path     string `json:"path"`
	Revision string `json:"revision"`
}

// testBeforeDataMoveCommit models a competing editor or disk failure after the
// copy, before changing the authoritative pointer. No production callback.
var testBeforeDataMoveCommit func() error

func (s *Server) handleStorageMove(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) || s.refuseUnusableStore(w) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	data, err := readBounded(r.Body, 16<<10)
	var request moveChatData
	if err != nil || decodeDataJSON(data, &request) != nil || !validMoveRequest(request) {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, "Choose an absolute folder path and reload storage settings before moving data.")
		return
	}
	s.changeChatStorage(w, r, request, copyChatData)
}

func validMoveRequest(request moveChatData) bool {
	return request.Revision != "" && filepath.IsAbs(request.Path) && !strings.ContainsAny(request.Path, "\x00\r\n")
}

// Moves and restores share the same locked copy/verify/cutover protocol.
func (s *Server) changeChatStorage(w http.ResponseWriter, r *http.Request, request moveChatData, copyData func(context.Context, *chatDataStore, *chatDataStore, *[]string) error) {
	request.Path = filepath.Clean(request.Path)
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
	if request.Revision != store.revision {
		storageFailure(w, withCode(CodeStale, errors.New("the location changed; reload storage settings before moving data")))
		return
	}
	if request.Path == store.location.Path || pathContains(request.Path, store.location.Path) || pathContains(store.location.Path, request.Path) || pathContains(s.projectDir, request.Path) || pathContains(request.Path, s.projectDir) || request.Path == s.configDir {
		// A legacy config root may migrate to its dedicated child data folder.
		if !(store.legacy && request.Path == s.defaultChatDataPath() && request.Path != s.configDir && !pathContains(s.projectDir, request.Path)) {
			storageFailure(w, withCode(CodeBadRequest, errors.New("choose a separate private folder outside the project and current data folder")))
			return
		}
	}
	status, err := s.describeStorage(store)
	if err != nil {
		storageFailure(w, err)
		return
	}
	if status.Bytes > maxMoveBytes {
		storageFailure(w, withCode(CodeTooLarge, errors.New("this move exceeds the 1 GiB limit; export older chats before moving")))
		return
	}
	target, err := s.openChatDataAt(dataLocation{Version: 1, Path: request.Path, Previous: store.location.Path}, "", true)
	if err != nil {
		storageFailure(w, err)
		return
	}
	defer target.root.Close()
	// Even an already-marked folder is not a merge target or a rollback action.
	entries, err := storageEntries(target.root)
	if err == nil {
		for _, entry := range entries {
			if entry.Name() != dataMarkerName {
				err = errors.New("the destination already has data; choose an empty folder")
				break
			}
		}
	}
	if err != nil {
		storageFailure(w, err)
		return
	}
	copied := []string{}
	committed := false
	defer func() {
		if !committed {
			for _, name := range copied {
				_ = target.root.Remove(name)
			}
		}
	}()
	if err = copyData(r.Context(), store, target, &copied); err != nil {
		storageFailure(w, err)
		return
	}
	if testBeforeDataMoveCommit != nil {
		if err = testBeforeDataMoveCommit(); err != nil {
			storageFailure(w, err)
			return
		}
	}
	if err = r.Context().Err(); err != nil {
		storageFailure(w, err)
		return
	}
	current, err := readPrivateData(s.assistant.root, dataLocationName, 16<<10)
	if err != nil || digestOf(current) != store.revision {
		storageFailure(w, withCode(CodeStale, errors.New("storage settings changed during the move; the original is still available")))
		return
	}
	if err = checkPrivateDataPath(target); err != nil {
		storageFailure(w, err)
		return
	}
	updated, _ := json.Marshal(target.location)
	if err = writePrivateData(s.assistant.root, dataLocationName, updated); err != nil {
		// A rename may have landed before a read-back/fsync error. Retain the
		// destination if it is authoritative; never delete its copied files.
		landed, readErr := readPrivateData(s.assistant.root, dataLocationName, 16<<10)
		committed = readErr != nil || bytes.Equal(landed, updated)
		storageFailure(w, err)
		return
	}
	committed = true
	target.revision = digestOf(updated)
	status, err = s.describeStorage(target)
	if err != nil {
		storageFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func copyChatData(ctx context.Context, source, target *chatDataStore, copied *[]string) error {
	entries, err := storageEntries(source.root)
	if err != nil {
		return err
	}
	var total int64
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := entry.Name()
		if !chatStorageFile(name) {
			if source.legacy || name == dataMarkerName || strings.HasPrefix(name, ".data-") || strings.HasPrefix(name, configStagingPrefix) {
				continue
			}
			return fmt.Errorf("unrecognized item %s in chat storage; it was not moved", name)
		}
		data, err := readPrivateData(source.root, name, maxConversationBytes)
		if err != nil {
			return err
		}
		if err = validateStorageFile(name, data); err != nil {
			return err
		}
		total += int64(len(data))
		if total > maxMoveBytes {
			return withCode(CodeTooLarge, errors.New("chat data grew beyond the move limit"))
		}
		*copied = append(*copied, name)
		if err = writePrivateData(target.root, name, data); err != nil {
			return err
		}
	}
	return validateStoredBindings(target.root)
}
