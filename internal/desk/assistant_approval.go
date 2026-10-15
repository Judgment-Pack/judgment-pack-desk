package desk

// Scoped host permissions. Model output, chat preferences and restored checks
// cannot mint an allowance. Each explicit user grant admits one artifact commit.
import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"
)

type assistantActionGrant struct {
	ID            string    `json:"id"`
	Action        string    `json:"action"`
	ChatID        string    `json:"chatId"`
	Path          string    `json:"path"`
	Revision      string    `json:"revision"`
	SuiteRevision string    `json:"-"`
	Scope         string    `json:"-"`
	Name          string    `json:"name"`
	State         string    `json:"state"`
	ExpiresAt     time.Time `json:"expiresAt"`
}
type assistantApprovalChat struct {
	ID          string `json:"id"`
	Interaction string `json:"interaction"`
	Pack        *struct {
		ID   string `json:"id"`
		Path string `json:"path"`
	} `json:"pack"`
	Checkpoint struct {
		State json.RawMessage `json:"state"`
	} `json:"checkpoint"`
}
type assistantActionAdapter struct {
	target func(*Server, assistantApprovalChat) (string, string, error)
	verify func(*Server, assistantApprovalChat, string, string, json.RawMessage) error
}

// New automatic actions must declare their target and check contract here.
// Unsupported actions remain manual; a research grant never matches a file edit.
var assistantActionAdapters = map[string]assistantActionAdapter{
	"pack-update": {target: packApprovalTarget, verify: verifyPackApproval},
}

type assistantApprovalRequest struct {
	Action   string `json:"action"`
	ID       string `json:"id"`
	Revision string `json:"revision"`
	Content  string `json:"content"`
}

func (s *Server) approvalChat(chat string) (assistantApprovalChat, json.RawMessage, error) {
	store, e := s.openChatData()
	if e != nil {
		return assistantApprovalChat{}, nil, e
	}
	defer store.root.Close()
	name, e := resolveConversationName(store.root, s.conversationIdentity())
	if e != nil {
		return assistantApprovalChat{}, nil, e
	}
	data, e := readPrivateData(store.root, name, maxConversationBytes)
	if e != nil {
		return assistantApprovalChat{}, nil, e
	}
	if e = validateConversations(data); e != nil {
		return assistantApprovalChat{}, nil, e
	}
	var doc struct {
		Chats []assistantApprovalChat `json:"chats"`
	}
	if e = json.Unmarshal(data, &doc); e != nil {
		return assistantApprovalChat{}, nil, e
	}
	for _, c := range doc.Chats {
		if c.ID == chat {
			tests, e := readPrivateData(store.root, strings.Replace(name, "conversations-", "pack-tests-", 1), maxConversationBytes)
			if errors.Is(e, os.ErrNotExist) {
				tests = []byte(`{"version":1,"suites":{}}`)
				e = nil
			}
			return c, tests, e
		}
	}
	return assistantApprovalChat{}, nil, errors.New("Save this conversation before allowing automatic actions.")
}
func (s *Server) handleAssistantApproval(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) || s.refuseUnusableStore(w) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	chatID := r.PathValue("chat")
	if !researchChatID.MatchString(chatID) || r.URL.RawQuery != "" {
		writeJSONCoded(w, 400, CodeBadRequest, "Invalid conversation.")
		return
	}
	var req assistantApprovalRequest
	if r.Method != "GET" && inputRequest(r, &req) != nil {
		writeJSONCoded(w, 400, CodeBadRequest, "Invalid action allowance.")
		return
	}
	status, body := s.assistantApprovalTransaction(r, chatID, req)
	writeJSON(w, status, body)
}

// Release storage/file locks before writing a potentially slow HTTP response.
func (s *Server) assistantApprovalTransaction(r *http.Request, chatID string, req assistantApprovalRequest) (int, any) {
	s.writes.Lock()
	defer s.writes.Unlock()
	lock, e := s.privateDataLockContext(r.Context(), true)
	if e != nil {
		return statusForRefusal(e), codedBody(codeOf(e), "Chat storage: "+e.Error())
	}
	defer lock.Close()
	scope := s.sessions.handle("assistant-action-approval\n" + bearerOf(r))
	key := scope + "\n" + chatID
	if s.actionGrants == nil {
		s.actionGrants = map[string]assistantActionGrant{}
	}
	grant, exists := s.actionGrants[key]
	if exists && grant.State == "ready" && !time.Now().Before(grant.ExpiresAt) {
		grant.State = "expired"
		s.actionGrants[key] = grant
	}
	respond := func() (int, any) {
		mode := "manual"
		if exists && grant.State == "ready" {
			mode = "auto"
		}
		out := map[string]any{"mode": mode}
		if exists {
			out["grant"] = grant
		}
		return 200, out
	}
	if r.Method == "GET" {
		// A displayed Auto state reflects its current resource, suite and phase.
		if exists && grant.State == "ready" {
			chat, tests, err := s.approvalChat(chatID)
			adapter, supported := assistantActionAdapters[grant.Action]
			stale := err != nil || !supported || chat.Interaction == "plan"
			if !stale {
				path, name, err := adapter.target(s, chat)
				stale = err != nil || path != grant.Path || name != grant.Name || approvalSuiteRevision(tests, chat) != grant.SuiteRevision
				if !stale {
					data, _, err := s.readThroughRoot(path)
					stale = err != nil || digestOf(data) != grant.Revision
				}
			}
			if stale {
				grant.State = "stale"
				s.actionGrants[key] = grant
			}
		}
		return respond()
	}
	fail := func(e error) (int, any) { return 409, codedBody(CodeBadRequest, e.Error()) }
	if strings.HasSuffix(r.URL.Path, "/revoke") {
		if !exists || grant.ID != req.ID {
			return fail(errors.New("This allowance is unavailable in this signed-in window."))
		}
		grant.State = "revoked"
		s.actionGrants[key] = grant
		return respond()
	}
	adapter, ok := assistantActionAdapters[req.Action]
	if !ok {
		return fail(errors.New("This action requires explicit review."))
	}
	chat, tests, e := s.approvalChat(chatID)
	if e != nil {
		return fail(e)
	}
	if chat.Interaction == "plan" {
		return fail(errors.New("Switch to Work before allowing or saving an automatic action."))
	}
	path, name, e := adapter.target(s, chat)
	if e != nil {
		return fail(e)
	}
	if e = s.refuseSymlinkedPath(path); e != nil {
		return statusForRefusal(e), errorBody(e)
	}
	bytes, status, e := s.readThroughRoot(path)
	if e != nil {
		return status, errorBody(e)
	}
	revision := digestOf(bytes)
	info, _ := s.root.Lstat(osPath(path))
	if s.fileAccessPolicy().readOnlyReason(path, info) != "" {
		return fail(errors.New("This file is read-only."))
	}
	if strings.HasSuffix(r.URL.Path, "/grant") {
		if req.Revision != revision {
			return fail(errors.New("The file changed. Review the allowance again."))
		}
		for k, g := range s.actionGrants {
			if g.State != "ready" || !time.Now().Before(g.ExpiresAt) {
				delete(s.actionGrants, k)
			}
		}
		if len(s.actionGrants) >= 256 {
			return fail(errors.New("Too many active action allowances."))
		}
		var random [16]byte
		if _, e = rand.Read(random[:]); e != nil {
			return fail(errors.New("The allowance could not be created."))
		}
		grant = assistantActionGrant{ID: hex.EncodeToString(random[:]), Action: req.Action, ChatID: chatID, Path: path, Name: name, Revision: revision, SuiteRevision: approvalSuiteRevision(tests, chat), Scope: scope, State: "ready", ExpiresAt: time.Now().Add(15 * time.Minute)}
		s.actionGrants[key] = grant
		exists = true
		return respond()
	}
	if !strings.HasSuffix(r.URL.Path, "/commit") || !exists || grant.ID != req.ID || grant.Action != req.Action || grant.Path != path || grant.Name != name || grant.Scope != scope || grant.State != "ready" {
		return fail(errors.New("Review and allow this action before saving automatically."))
	}
	if req.Revision != grant.Revision || revision != grant.Revision || approvalSuiteRevision(tests, chat) != grant.SuiteRevision {
		grant.State = "stale"
		s.actionGrants[key] = grant
		return fail(errors.New("The file or test suite changed. Review a new allowance."))
	}
	if len(req.Content) > maxFileBytes {
		return 413, codedBody(CodeTooLarge, "The proposal is too large.")
	}
	if e = adapter.verify(s, chat, req.Content, grant.Revision, tests); e != nil {
		return fail(e)
	}
	// Consume before dispatch, including uncertain write outcomes. Never replay.
	grant.State = "used"
	s.actionGrants[key] = grant
	return s.commitWriteLocked(path, WriteRequest{Path: path, Content: req.Content, BaseSHA256: grant.Revision})
}
