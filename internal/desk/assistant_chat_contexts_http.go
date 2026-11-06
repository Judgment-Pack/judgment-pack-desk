package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"sync"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/jsonscalar"
)

var assistantContextSelectorToken = regexp.MustCompile(`^[a-f0-9]{64}$`)

func assistantContextSelectorValid(selector assistantRunContextSelector, purpose string) bool {
	if !assistantContextSelectorToken.MatchString(selector.Document) {
		return false
	}
	if purpose == "chat" {
		return selector.Pane == ""
	}
	return assistantAuthoringPurpose(purpose) && assistantContextSelectorToken.MatchString(selector.Pane)
}
func assistantContextSelectorJSON(raw json.RawMessage, purpose string) error {
	if !validUTF8(raw) || jsonscalar.ValidateEscapes(raw) != nil || !uniqueJSON(raw) {
		return errAssistantRunInvalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil || fields["document"] == nil || len(fields) > 2 {
		return errAssistantRunInvalid
	}
	for key := range fields {
		if key != "document" && key != "pane" {
			return errAssistantRunInvalid
		}
	}
	if value, present := fields["pane"]; present && (purpose == "chat" || bytes.Equal(value, []byte("null"))) {
		return errAssistantRunInvalid
	}
	var selector assistantRunContextSelector
	if json.Unmarshal(raw, &selector) != nil || !assistantContextSelectorValid(selector, purpose) {
		return errAssistantRunInvalid
	}
	return nil
}

type assistantContextDocumentRequest struct {
	RegistrationID string `json:"registrationId"`
}
type assistantContextPaneRequest struct {
	Document       string `json:"document"`
	RegistrationID string `json:"registrationId"`
	Purpose        string `json:"purpose"`
}
type assistantContextDocumentRelease struct {
	Document string `json:"document"`
}
type assistantContextPaneRelease struct {
	Document string `json:"document"`
	Pane     string `json:"pane"`
}

// Pure closed request reader, shared by the actual handlers and manual vectors.
func readAssistantContextRequest(kind string, r *http.Request) (any, error) {
	switch kind {
	case "document":
		var q assistantContextDocumentRequest
		if assistantRunRequest(r, &q) != nil || !assistantRunText(q.RegistrationID, 128) {
			return nil, errAssistantRunInvalid
		}
		return q, nil
	case "pane":
		var q assistantContextPaneRequest
		if assistantRunRequest(r, &q) != nil || !assistantContextSelectorToken.MatchString(q.Document) || !assistantRunText(q.RegistrationID, 128) || !assistantAuthoringPurpose(q.Purpose) {
			return nil, errAssistantRunInvalid
		}
		return q, nil
	case "release-document":
		var q assistantContextDocumentRelease
		if assistantRunRequest(r, &q) != nil || !assistantContextSelectorToken.MatchString(q.Document) {
			return nil, errAssistantRunInvalid
		}
		return q, nil
	case "release-pane":
		var q assistantContextPaneRelease
		if assistantRunRequest(r, &q) != nil || !assistantContextSelectorToken.MatchString(q.Document) || !assistantContextSelectorToken.MatchString(q.Pane) {
			return nil, errAssistantRunInvalid
		}
		return q, nil
	default:
		return nil, errAssistantRunInvalid
	}
}

// One service owns this registry. Authority watchers retire future reuse; the
// registry's separate pending/run references prevent disposal under live work.
// The service binds/settles those references at the original owner boundaries.
type assistantContextHTTPState struct {
	mu       sync.Mutex
	registry *assistantChatContexts
	closed   bool
	watches  map[assistantContextAuthority][]func() bool
	onRetire func(assistantRunBinding)
}

func newAssistantContextHTTPState() *assistantContextHTTPState {
	return &assistantContextHTTPState{registry: newAssistantChatContexts(), watches: map[assistantContextAuthority][]func() bool{}}
}
func (h *assistantContextHTTPState) retire(binding assistantRunBinding) {
	h.mu.Lock()
	key := assistantContextAuthorityOf(binding)
	for _, stop := range h.watches[key] {
		stop()
	}
	delete(h.watches, key)
	h.registry.retireAuthority(binding)
	notify := h.onRetire
	h.mu.Unlock()
	if notify != nil {
		notify(binding)
	}
}
func (h *assistantContextHTTPState) close() {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for _, stops := range h.watches {
		for _, stop := range stops {
			stop()
		}
	}
	h.watches = map[assistantContextAuthority][]func() bool{}
	h.registry.close()
}
func (s *Server) registerAssistantContexts() {
	for path, kind := range map[string]string{
		"POST /api/assistant-contexts/documents":         "document",
		"POST /api/assistant-contexts/panes":             "pane",
		"POST /api/assistant-contexts/documents/release": "release-document",
		"POST /api/assistant-contexts/panes/release":     "release-pane",
	} {
		s.mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) { s.handleAssistantContext(w, r, kind) })
	}
}
func (s *Server) handleAssistantContext(w http.ResponseWriter, r *http.Request, kind string) {
	if !s.guardAssistantRun(w, r) {
		return
	}
	q, err := readAssistantContextRequest(kind, r)
	if err != nil {
		assistantRunHTTPError(w, err)
		return
	}
	binding, session, err := s.assistantRunIdentity(r)
	if err != nil {
		assistantRunHTTPError(w, err)
		return
	}
	store := s.assistantRuns()
	if store.problem != nil {
		assistantRunHTTPError(w, store.problem)
		return
	}
	binding.Project = store.project
	var policy context.Context
	if s.signIn != nil {
		s.signIn.mu.Lock()
		policy = s.signIn.epoch
		s.signIn.mu.Unlock()
	}
	// The captured epoch must belong to the identity just authorized. Its later
	// cancellation queues retirement behind the registration lock below.
	fresh, _, err := s.assistantRunIdentity(r)
	if err != nil || !assistantRunIdentityMatches(binding, fresh) {
		assistantRunHTTPError(w, errAssistantRunScope)
		return
	}
	h := s.assistantRunService().contexts
	h.mu.Lock()
	if h.closed || session.Err() != nil || policy != nil && policy.Err() != nil {
		h.mu.Unlock()
		assistantRunHTTPError(w, errAssistantRunScope)
		return
	}
	key := assistantContextAuthorityOf(binding)
	var reply any
	switch value := q.(type) {
	case assistantContextDocumentRequest:
		reply, err = h.registry.openDocument(binding, value.RegistrationID)
	case assistantContextPaneRequest:
		reply, err = h.registry.openPane(binding, value.Document, value.RegistrationID, value.Purpose)
	case assistantContextDocumentRelease:
		err = h.registry.retireDocument(binding, value.Document)
	case assistantContextPaneRelease:
		err = h.registry.retirePane(binding, assistantRunContextSelector{Document: value.Document, Pane: value.Pane})
	}
	if err == nil {
		if _, ok := h.watches[key]; !ok {
			stops := []func() bool{context.AfterFunc(session, func() { h.retire(binding) })}
			if policy != nil {
				stops = append(stops, context.AfterFunc(policy, func() { h.retire(binding) }))
			}
			h.watches[key] = stops
		}
	}
	h.mu.Unlock()
	if err != nil {
		assistantRunHTTPError(w, err)
		return
	}
	if reply == nil {
		reply = struct {
			Released bool `json:"released"`
		}{true}
	}
	writeJSON(w, 200, reply)
}
