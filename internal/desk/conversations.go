package desk

// Conversations are private UI checkpoints, not runtime inputs or authority.
// They use a separate owner-only data root, with explicit legacy migration.
// The server chooses the name from its pinned project identity.
import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
)

const maxConversationBytes = 16 << 20

type conversationDocument struct {
	Version int               `json:"version"`
	Chats   []json.RawMessage `json:"chats"`
}
type conversationReply struct {
	Project string          `json:"project"`
	SHA256  string          `json:"sha256"`
	Content json.RawMessage `json:"content"`
}

func (s *Server) conversationName() string {
	return "conversations-" + digestOf([]byte(s.projectDir)) + ".json"
}
func validateConversations(data []byte) error {
	if !validUTF8(data) || !json.Valid(data) {
		return errors.New("chat history must be one UTF-8 JSON document")
	}
	var doc conversationDocument
	if err := json.Unmarshal(data, &doc); err != nil || doc.Version != 1 || doc.Chats == nil || len(doc.Chats) > 256 {
		return errors.New("chat history must have version 1 and at most 256 chats")
	}
	return nil
}

var draftPackFileName = regexp.MustCompile(`^draft-packs-[a-f0-9]{64}\.json$`)

var briefFileName = regexp.MustCompile(`^briefs-[a-f0-9]{64}\.json$`)

var packTestsFileName = regexp.MustCompile(`^pack-tests-[a-f0-9]{64}\.json$`)

func validatePackTests(data []byte) error {
	if !validUTF8(data) || !json.Valid(data) {
		return errors.New("tests must be one UTF-8 JSON document")
	}
	var doc struct {
		Version int                        `json:"version"`
		Suites  map[string]json.RawMessage `json:"suites"`
	}
	if err := json.Unmarshal(data, &doc); err != nil || doc.Version != 1 || doc.Suites == nil || len(doc.Suites) > 512 {
		return errors.New("test storage must have version 1 and at most 512 suites")
	}
	return nil
}
func (s *Server) handlePackTests(w http.ResponseWriter, r *http.Request) {
	s.handleWorkspaceRecord(w, r, false)
}

func draftPackName(conversation string) string {
	return strings.Replace(conversation, "conversations-", "draft-packs-", 1)
}
func validateDraftPacks(data []byte) error {
	if !validUTF8(data) || !json.Valid(data) {
		return errors.New("draft packs must be one UTF-8 JSON document")
	}
	var doc struct {
		Version int               `json:"version"`
		Drafts  []json.RawMessage `json:"drafts"`
		Deleted []string          `json:"deleted"`
	}
	if err := json.Unmarshal(data, &doc); err != nil || doc.Version != 1 || doc.Drafts == nil || len(doc.Drafts) > 256 || len(doc.Deleted) > 4096 {
		return errors.New("draft packs must have version 1 and at most 256 drafts")
	}
	return nil
}
func (s *Server) readConversationFile(root *os.Root, name string) (conversationReply, error) {
	return s.readWorkspaceRecord(root, name, false)
}
func (s *Server) readWorkspaceRecord(root *os.Root, name string, drafts bool) (conversationReply, error) {
	reply := conversationReply{Project: s.projectDir, SHA256: "absent", Content: json.RawMessage(`{"version":1,"chats":[]}`)}
	if briefFileName.MatchString(name) {
		reply.Content = json.RawMessage(`{"version":1,"subjects":{}}`)
	} else if sourceReviewsFileName.MatchString(name) {
		reply.Content = json.RawMessage(`{"version":1,"reviews":[]}`)
	} else if packTestsFileName.MatchString(name) {
		reply.Content = json.RawMessage(`{"version":1,"suites":{}}`)
	} else if drafts {
		reply.Content = json.RawMessage(`{"version":1,"drafts":[],"deleted":[]}`)
	}
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return reply, nil
	}
	if err != nil {
		return reply, err
	}
	if err = ownerOnlyFile(name, info.Mode()); err != nil {
		return reply, withCode(CodeForbidden, err)
	}
	if err = ownedByUs(name, info); err != nil {
		return reply, withCode(CodeForbidden, err)
	}
	file, err := root.OpenFile(name, os.O_RDONLY|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return reply, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return reply, err
	}
	if !os.SameFile(info, opened) {
		return reply, withCode(CodeForbidden, errors.New("chat history changed while being opened"))
	}
	if err = ownerOnlyFile(name, opened.Mode()); err != nil {
		return reply, withCode(CodeForbidden, err)
	}
	if err = ownedByUs(name, opened); err != nil {
		return reply, withCode(CodeForbidden, err)
	}
	data, err := readBounded(file, maxConversationBytes)
	if err != nil {
		return reply, withCode(CodeTooLarge, errors.New("chat history exceeds its 16 MiB limit"))
	}
	validate := validateConversations
	if briefFileName.MatchString(name) {
		validate = validateBriefs
	} else if sourceReviewsFileName.MatchString(name) {
		validate = validateSourceReviews
	} else if packTestsFileName.MatchString(name) {
		validate = validatePackTests
	} else if drafts {
		validate = validateDraftPacks
	}
	if err = validate(data); err != nil {
		return reply, withCode(CodeBadRequest, err)
	}
	reply.SHA256 = digestOf(data)
	reply.Content = data
	return reply, nil
}
func (s *Server) handleConversations(w http.ResponseWriter, r *http.Request) {
	s.handleWorkspaceRecord(w, r, false)
}
func (s *Server) handleDraftPacks(w http.ResponseWriter, r *http.Request) {
	s.handleWorkspaceRecord(w, r, true)
}
func (s *Server) handleWorkspaceRecord(w http.ResponseWriter, r *http.Request, drafts bool) {
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
	root := store.root
	recordName, err := resolveConversationName(root, s.projectDir)
	if err != nil {
		storageFailure(w, err)
		return
	}
	briefs := r.URL.Path == "/api/briefs"
	reviews := r.URL.Path == "/api/source-reviews"
	tests := r.URL.Path == "/api/pack-tests"
	if briefs {
		recordName = strings.Replace(recordName, "conversations-", "briefs-", 1)
	} else if reviews {
		recordName = strings.Replace(recordName, "conversations-", "source-reviews-", 1)
	} else if tests {
		recordName = strings.Replace(recordName, "conversations-", "pack-tests-", 1)
	} else if drafts {
		recordName = draftPackName(recordName)
	}
	read := func() (conversationReply, error) { return s.readWorkspaceRecord(root, recordName, drafts) }
	validate := validateConversations
	if briefs {
		validate = validateBriefs
	} else if reviews {
		validate = validateSourceReviews
	} else if tests {
		validate = validatePackTests
	} else if drafts {
		validate = validateDraftPacks
	}
	fail := func(err error) {
		code := codeOf(err)
		writeJSONCoded(w, statusForRefusal(err), code, "Workspace data could not be saved or read: "+err.Error())
	}
	if r.Method == http.MethodGet {
		reply, err := read()
		if err != nil {
			fail(err)
			return
		}
		writeConversationReply(w, reply)
		return
	}
	expected := r.Header.Get("If-Match")
	if expected == "" && !briefs {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, "If-Match is required; reload history before saving")
		return
	}
	data, err := readBounded(r.Body, maxConversationBytes)
	if err != nil {
		writeJSONCoded(w, http.StatusRequestEntityTooLarge, CodeTooLarge, "Workspace records are limited to 16 MiB; nothing was written.")
		return
	}
	if briefs {
		current, e := read()
		if e != nil {
			fail(e)
			return
		}
		data, e = s.changeBrief(root, recordName, current.Content, data)
		if e != nil {
			writeJSONCoded(w, http.StatusConflict, CodeStale, e.Error())
			return
		}
		expected = current.SHA256
		if len(data) > maxConversationBytes {
			writeJSONCoded(w, 413, CodeTooLarge, "Brief storage is full; nothing was changed.")
			return
		}
	}
	if err = validate(data); err != nil {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, err.Error())
		return
	}
	matches := func() error {
		current, err := read()
		if err != nil {
			return err
		}
		if current.SHA256 != expected {
			return withCode(CodeStale, errors.New("history changed in another window; reload before saving"))
		}
		return nil
	}
	if err = matches(); err != nil {
		fail(err)
		return
	}
	stage, name, err := newDataStage(root)
	if err != nil {
		fail(err)
		return
	}
	defer root.Remove(name)
	defer stage.Close()
	if _, err = stage.Write(data); err != nil {
		fail(err)
		return
	}
	if err = stage.Chmod(custodyFileMode); err != nil {
		fail(err)
		return
	}
	if err = stage.Sync(); err != nil {
		fail(err)
		return
	}
	if err = stage.Close(); err != nil {
		fail(err)
		return
	}
	// Same bounded compare-before-rename guarantee as desk.json; unrelated
	// processes can still race the final rename (see custody.go).
	if err = matches(); err != nil {
		fail(err)
		return
	}
	if err = root.Rename(name, recordName); err != nil {
		fail(err)
		return
	}
	if dir, err := root.Open("."); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	reply, err := read()
	if err != nil {
		fail(err)
		return
	}
	if reply.SHA256 != digestOf(data) {
		fail(withCode(CodeStale, fmt.Errorf("history changed after the write; reload before saving again")))
		return
	}
	writeConversationReply(w, reply)
}

// JSON is served as data, never embedded in HTML. Disabling HTML escaping keeps
// a bounded checkpoint from expanding sixfold in its response envelope.
func writeConversationReply(w http.ResponseWriter, reply conversationReply) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(reply)
}
