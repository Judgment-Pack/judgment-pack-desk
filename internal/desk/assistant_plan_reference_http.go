package desk

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
)

// Notification only, before waiting on the existing conversation write owner.
var testHookAssistantPlanReferenceQueued func()

// This authenticated lookup accepts only a saved Plan handle and exact saved
// chat/Plan revisions. The caller never supplies a locator or a trusted digest.
func (s *Server) handleAssistantPlanReference(w http.ResponseWriter, r *http.Request) {
	if !s.guardAssistantRun(w, r) {
		return
	}
	fail := func(err error) {
		if errors.Is(err, errAssistantRunScope) || errors.Is(err, errAssistantRunConflict) {
			err = withCode(CodeStale, fmt.Errorf("Review the current Plan references again before starting work. %w", err))
		}
		writeJSONCoded(w, statusForRefusal(err), codeOf(err), err.Error())
	}
	identity, _, err := s.assistantRunIdentity(r)
	if err != nil {
		fail(err)
		return
	}
	revision, err := strconv.Atoi(r.Header.Get("X-Plan-Revision"))
	if err != nil || revision < 1 || revision > 24 || strconv.Itoa(revision) != r.Header.Get("X-Plan-Revision") || !assistantRunDigest(r.Header.Get("If-Match")) {
		writeJSONCoded(w, 400, CodeBadRequest, "Invalid saved plan.")
		return
	}
	if testHookAssistantPlanReferenceQueued != nil {
		testHookAssistantPlanReferenceQueued()
	}
	s.writes.Lock()
	defer s.writes.Unlock()
	lock, err := s.privateDataLockContext(r.Context(), true)
	if err != nil {
		fail(err)
		return
	}
	defer lock.Close()
	store, err := s.openChatData()
	if err != nil {
		fail(err)
		return
	}
	defer store.root.Close()
	name, err := resolveConversationName(store.root, s.conversationIdentity())
	if err != nil {
		fail(err)
		return
	}
	read := func() (conversationReply, error) {
		reply, err := s.readWorkspaceRecord(store.root, name, false)
		if err == nil {
			err = s.authorizePolicyWorkspace(r, reply.Content, false)
		}
		return reply, err
	}
	reply, err := read()
	if err != nil {
		fail(err)
		return
	}
	var doc conversationDocument
	if json.Unmarshal(reply.Content, &doc) != nil {
		fail(errAssistantRunInvalid)
		return
	}
	var source []byte
	var plan *chatPlan
	for _, raw := range doc.Chats {
		var chat struct {
			ID    string     `json:"id"`
			Plans []chatPlan `json:"plans"`
		}
		if json.Unmarshal(raw, &chat) != nil {
			fail(errAssistantRunInvalid)
			return
		}
		if chat.ID != r.PathValue("chat") {
			continue
		}
		if source != nil || digestOf(raw) != r.Header.Get("If-Match") {
			fail(errAssistantRunConflict)
			return
		}
		source = raw
		for i := range chat.Plans {
			p := &chat.Plans[i]
			if p.ID == r.PathValue("plan") && p.Revision == revision {
				plan = p
			}
		}
	}
	if source == nil || plan == nil {
		fail(errAssistantRunScope)
		return
	}
	locators, err := s.assistantPlanLocatorsLocked(source, *plan, &identity)
	if err != nil {
		fail(err)
		return
	}
	index := -1
	for i, ref := range plan.References {
		if ref.ID == r.PathValue("reference") && ref.Revision == "" {
			if index >= 0 {
				fail(errAssistantRunInvalid)
				return
			}
			index = i
		}
	}
	if index < 0 {
		fail(errAssistantRunScope)
		return
	}
	view, err := s.assistantPlanView(r.Context(), locators)
	if err != nil {
		fail(err)
		return
	}
	if view != nil {
		defer view.Close()
	}
	live, err := s.resolveAssistantPlanReferences(locators, view)
	if err != nil {
		fail(err)
		return
	}
	// Repeat immutable source/session checks after lock acquisition and reads.
	current, _, err := s.assistantRunIdentity(r)
	if err != nil {
		fail(err)
		return
	}
	after, err := read()
	if err != nil {
		fail(err)
		return
	}
	if !assistantRunIdentityMatches(identity, current) || after.SHA256 != reply.SHA256 {
		fail(errAssistantRunConflict)
		return
	}
	if err = r.Context().Err(); err != nil {
		fail(err)
		return
	}
	ref := plan.References[index]
	writeJSON(w, http.StatusOK, chatPlanReference{Kind: ref.Kind, ID: ref.ID, Digest: live[index].Revision})
}
