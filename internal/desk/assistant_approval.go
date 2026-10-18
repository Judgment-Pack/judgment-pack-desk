package desk

// Scoped host permissions. Model output, chat preferences and restored checks
// cannot mint an allowance. Each explicit user grant admits one artifact commit.
import (
	"context"
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
	Target        string    `json:"target,omitempty"`
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
	Policy *struct {
		ID string `json:"id"`
	} `json:"policy"`
	PolicyProposals []assistantPolicyProposal    `json:"policyProposals"`
	InputActions    []assistantPublicationAction `json:"inputActions"`
	ID              string                       `json:"id"`
	Interaction     string                       `json:"interaction"`
	Pack            *struct {
		ID   string `json:"id"`
		Path string `json:"path"`
	} `json:"pack"`
	Checkpoint struct {
		State json.RawMessage `json:"state"`
	} `json:"checkpoint"`
}
type assistantActionResource struct {
	Path, Name, Revision, Checks string
	Verify                       func(string) error
	Commit                       func(string) (int, any)
	Close                        func()
}
type assistantActionAdapter struct {
	resolve func(context.Context, *Server, assistantApprovalChat, json.RawMessage, string) (assistantActionResource, error)
	manual  bool
}

// Adapters own resource admission; the dispatcher owns session/phase scope,
// explicit permission, expiry and single use. Unknown actions stay manual.
var assistantActionAdapters = map[string]assistantActionAdapter{
	"policy-update":     {resolve: resolvePolicyApproval, manual: true},
	"pack-update":       {resolve: resolvePackApproval},
	"portable-research": {resolve: resolvePortableResearchApproval, manual: true},
	"dialect-publish":   {resolve: resolveDialectPublicationApproval, manual: true},
}

type assistantApprovalRequest struct {
	Target   string `json:"target,omitempty"`
	Reviewed bool   `json:"reviewed,omitempty"`
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
	ctx := context.WithValue(r.Context(), assistantPolicyRequestKey{}, r)
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
				resource, err := adapter.resolve(ctx, s, chat, tests, grant.Target)
				stale = err != nil || resource.Path != grant.Path || resource.Name != grant.Name || resource.Revision != grant.Revision || resource.Checks != grant.SuiteRevision
				if resource.Close != nil {
					resource.Close()
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
	resource, e := adapter.resolve(ctx, s, chat, tests, req.Target)
	if e != nil {
		return fail(e)
	}
	if resource.Close != nil {
		defer resource.Close()
	}
	path, name, revision := resource.Path, resource.Name, resource.Revision
	if strings.HasSuffix(r.URL.Path, "/apply") {
		if !adapter.manual || !req.Reviewed || req.Revision != revision {
			return fail(errors.New("Review this exact resource before saving."))
		}
		if len(req.Content) > maxFileBytes {
			return 413, codedBody(CodeTooLarge, "The proposal is too large.")
		}
		if e = resource.Verify(req.Content); e != nil {
			return fail(e)
		}
		return resource.Commit(req.Content)
	}
	if strings.HasSuffix(r.URL.Path, "/grant") {
		if req.Revision != revision {
			return fail(errors.New("The resource changed. Review the allowance again."))
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
		grant = assistantActionGrant{ID: hex.EncodeToString(random[:]), Action: req.Action, ChatID: chatID, Path: path, Name: name, Revision: revision, SuiteRevision: resource.Checks, Target: req.Target, Scope: scope, State: "ready", ExpiresAt: time.Now().Add(15 * time.Minute)}
		s.actionGrants[key] = grant
		exists = true
		return respond()
	}
	if !strings.HasSuffix(r.URL.Path, "/commit") || !exists || grant.ID != req.ID || grant.Action != req.Action || grant.Path != path || grant.Name != name || grant.Target != req.Target || grant.Scope != scope || grant.State != "ready" {
		return fail(errors.New("Review and allow this action before saving automatically."))
	}
	if req.Revision != grant.Revision || revision != grant.Revision || resource.Checks != grant.SuiteRevision {
		grant.State = "stale"
		s.actionGrants[key] = grant
		return fail(errors.New("The resource or its checks changed. Review a new allowance."))
	}
	if len(req.Content) > maxFileBytes {
		return 413, codedBody(CodeTooLarge, "The proposal is too large.")
	}
	if e = resource.Verify(req.Content); e != nil {
		return fail(e)
	}
	// Consume before dispatch, including uncertain write outcomes. Never replay.
	grant.State = "used"
	s.actionGrants[key] = grant
	return resource.Commit(req.Content)
}

func resolvePackApproval(ctx context.Context, s *Server, chat assistantApprovalChat, tests json.RawMessage, target string) (assistantActionResource, error) {
	var result assistantActionResource
	if target != "" {
		return result, errors.New("This pack allowance has a different target.")
	}
	path, name, err := packApprovalTarget(s, chat)
	if err != nil {
		return result, err
	}
	if err = s.refuseSymlinkedPath(path); err != nil {
		return result, err
	}
	raw, _, err := s.readThroughRoot(path)
	if err != nil {
		return result, err
	}
	info, _ := s.root.Lstat(osPath(path))
	if s.fileAccessPolicy().readOnlyReason(path, info) != "" {
		return result, errors.New("This file is read-only.")
	}
	revision := digestOf(raw)
	return assistantActionResource{Path: path, Name: name, Revision: revision, Checks: approvalSuiteRevision(tests, chat), Verify: func(content string) error { return verifyPackApproval(s, chat, content, revision, tests) }, Commit: func(content string) (int, any) {
		return s.commitWriteLocked(path, WriteRequest{Path: path, Content: content, BaseSHA256: revision})
	}}, nil
}
