package desk

import (
	"net/http"
	"sort"
)

// Read-only discovery binds the exact saved user request, not the old whole-chat
// digest, which a terminal projection necessarily changes. Retries stay distinct.
func (s *Server) discoverAssistantChatRuns(r *http.Request, owner, request string) ([]assistantRunView, error) {
	if !researchChatID.MatchString(owner) || !assistantChatProjectionID(request) {
		return nil, errAssistantRunInvalid
	}
	store := s.assistantRuns()
	out := []assistantRunView{}
	_, err := store.transaction(r.Context(), func(ledger *assistantRunLedger) (bool, error) {
		identity, _, err := s.assistantRunIdentity(r)
		if err != nil {
			return false, err
		}
		data, err := s.openChatData()
		if err != nil {
			return false, err
		}
		defer data.root.Close()
		if err = checkPrivateDataPath(data); err != nil {
			return false, err
		}
		if err = store.checkScope(data.root); err != nil {
			return false, err
		}
		name, err := resolveConversationName(data.root, s.conversationIdentity())
		if err != nil {
			return false, err
		}
		raw, err := readPrivateData(data.root, name, maxConversationBytes)
		if err != nil {
			return false, err
		}
		if err = s.authorizePolicyConversations(r, raw); err != nil {
			return false, err
		}
		_, rows, err := assistantChatRows(raw)
		if err != nil {
			return false, err
		}
		index, err := assistantChatSelected(rows, owner)
		if err != nil {
			return false, err
		}
		for _, run := range ledger.Runs {
			if run.Binding.Principal != identity.Principal || run.Binding.Project != store.project || run.Binding.Owner.Kind != "chat" || run.Binding.Owner.ID != owner || run.Binding.RequestID != request {
				continue
			}
			current := assistantRunIdentityMatches(identity, run.Binding)
			if !current && !assistantRunTerminal(run.State) && !assistantRunPaused(run.State) {
				continue
			}
			if !assistantChatRequest(rows[index], request, run.Binding.RequestDigest) {
				continue
			}
			view := assistantRunPublic(run)
			if !current {
				view.Historical = true
			}
			out = append(out, view)
		}
		return false, nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, err
}
func (s *Server) handleAssistantChatRuns(w http.ResponseWriter, r *http.Request) {
	if !s.guardAssistantRun(w, r) {
		return
	}
	out, err := s.discoverAssistantChatRuns(r, r.PathValue("owner"), r.PathValue("request"))
	if err != nil {
		assistantRunHTTPError(w, err)
		return
	}
	writeJSON(w, 200, struct {
		Runs []assistantRunView `json:"runs"`
	}{out})
}
