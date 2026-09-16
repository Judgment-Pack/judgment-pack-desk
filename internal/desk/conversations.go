package desk

// Conversations are private UI checkpoints, not runtime inputs or authority.
// They share credential custody's pinned owner-only directory, but never its
// secret file. The server chooses the name from its pinned project identity.
import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
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
func (s *Server) readConversations() (conversationReply, error) {
	reply := conversationReply{Project: s.projectDir, SHA256: "absent", Content: json.RawMessage(`{"version":1,"chats":[]}`)}
	name := s.conversationName()
	info, err := s.assistant.root.Lstat(name)
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
	file, err := s.assistant.root.OpenFile(name, os.O_RDONLY|openNoFollow|openNonBlocking, 0)
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
	if err = validateConversations(data); err != nil {
		return reply, withCode(CodeBadRequest, err)
	}
	reply.SHA256 = digestOf(data)
	reply.Content = data
	return reply, nil
}
func (s *Server) handleConversations(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) || s.refuseUnusableStore(w) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	fail := func(err error) {
		code := codeOf(err)
		writeJSONCoded(w, statusForRefusal(err), code, "Chat history could not be saved or read: "+err.Error())
	}
	if r.Method == http.MethodGet {
		reply, err := s.readConversations()
		if err != nil {
			fail(err)
			return
		}
		writeConversationReply(w, reply)
		return
	}
	expected := r.Header.Get("If-Match")
	if expected == "" {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, "If-Match is required; reload history before saving")
		return
	}
	data, err := readBounded(r.Body, maxConversationBytes)
	if err != nil {
		writeJSONCoded(w, http.StatusRequestEntityTooLarge, CodeTooLarge, "Chat history is limited to 16 MiB. Export and delete older chats; nothing was written.")
		return
	}
	if err = validateConversations(data); err != nil {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, err.Error())
		return
	}
	s.writes.Lock()
	defer s.writes.Unlock()
	matches := func() error {
		current, err := s.readConversations()
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
	stage, name, err := s.assistant.stageConfig()
	if err != nil {
		fail(err)
		return
	}
	defer s.assistant.root.Remove(name)
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
	if err = s.assistant.root.Rename(name, s.conversationName()); err != nil {
		fail(err)
		return
	}
	if dir, err := s.assistant.root.Open("."); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	reply, err := s.readConversations()
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
