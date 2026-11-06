package desk

import (
	"encoding/json"
	"net/http"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

// Only private production construction requires a browser reuse scope. Test
// executors can exercise isolated legacy profiles; supplied selectors still
// always pass through the same authenticated registry admission.
type assistantContextExecutor interface{ assistantRetainedChat() }

func (*assistantProductionExecutor) assistantRetainedChat() {}
func (*assistantCreationExecutor) assistantRetainedChat()   {}
func (*assistantAuthoringExecutor) assistantRetainedChat()  {}
func assistantRequiresContext(executor assistantRunExecutor, purpose string) bool {
	_, required := executor.(assistantContextExecutor)
	return required && (purpose == "chat" || assistantAuthoringPurpose(purpose))
}

// service.mu owns this record. A run reference survives a settled invocation's
// pause, independently of document membership and any reader. Unknown work is
// deliberately bounded and quarantined, never treated as settled by a timeout.
type assistantContextOwnership struct {
	binding     assistantRunBinding
	paused      bool
	retired     bool // authority end or authoritative saved-owner deletion
	quarantined bool
}

func (h *assistantRunService) prepareContext(source *assistantRunSource) (*assistantChatContextHandle, error) {
	if source.Start.Context == nil {
		if assistantRequiresContext(h.executor(source.Start.Purpose), source.Start.Purpose) {
			return nil, errAssistantRunScope
		}
		return nil, nil
	}
	if !assistantContextSelectorValid(*source.Start.Context, source.Start.Purpose) || h.contexts == nil {
		return nil, errAssistantRunScope
	}
	handle, err := h.contexts.registry.retain(source.Binding, *source.Start.Context, source.Start.Purpose, source.Start.OwnerID)
	if err != nil {
		return nil, err
	}
	if err = h.contexts.registry.reserveRun(handle); err != nil {
		handle.Release()
		return nil, err
	}
	source.chat = handle.Chat()
	return handle, nil
}
func (h *assistantRunService) rememberContext(record assistantRunRecord) {
	if h.contextOwners == nil {
		h.contextOwners = map[string]*assistantContextOwnership{}
	}
	h.contextOwners[record.ID] = &assistantContextOwnership{binding: assistantContextCopyBinding(record.Binding)}
}

// These lookups name the already admitted run. Neither a new browser selector
// nor durable source metadata can synthesize a missing in-memory Chat owner.
func (h *assistantRunService) continuationContext(record assistantRunRecord) (*assistantChatContextHandle, error) {
	owner := h.contextOwners[record.ID]
	if owner == nil {
		if assistantRequiresContext(h.executor(record.Binding.Owner.Kind), record.Binding.Owner.Kind) {
			return nil, errAssistantRunUnavailable
		}
		return nil, nil
	}
	if owner.retired || owner.quarantined || h.contexts == nil {
		return nil, errAssistantRunScope
	}
	return h.contexts.registry.retainRun(record.ID, record.Binding)
}
func (h *assistantRunService) launchContext(record assistantRunRecord, source *assistantRunSource) (*assistantChatContextHandle, error) {
	handle, err := h.continuationContext(record)
	if err != nil {
		return nil, err
	}
	if handle == nil {
		if source.chat != nil || source.Start.Context != nil {
			return nil, errAssistantRunScope
		}
		return nil, nil
	}
	if source.chat != nil && source.chat != handle.Chat() {
		handle.Release()
		return nil, errAssistantRunScope
	}
	source.chat = handle.Chat()
	return handle, nil
}
func (e *assistantRunExecution) bindContextInput(input *runcontext.Input) error {
	if e.contextHandle == nil {
		return nil
	}
	chat := e.contextHandle.Chat()
	if chat == nil || e.source.chat != chat || input.Chat != nil && input.Chat != chat {
		return errAssistantRunScope
	}
	input.Chat = chat
	return nil
}

// Called after the enclosing adapter/delegate returned. The same positive
// physical/callback/receipt proof used by P3 is required; an End or nil return
// alone never authorizes disposal or subsequent reuse of uncertain context.
func (e *assistantRunExecution) contextInvocationStarted() {
	if e.contextHandle == nil {
		return
	}
	e.canonicalMu.Lock()
	e.contextInvoked = true
	e.canonicalMu.Unlock()
}
func (e *assistantRunExecution) contextInvocationReturned(invocation *assistantExecutionInvocation) {
	if e.contextHandle == nil {
		return
	}
	_, err := invocation.proof()
	e.canonicalMu.Lock()
	relay := e.gateNotices
	e.canonicalMu.Unlock()
	safe := err == nil && relay != nil && relay.contextQuiescent(invocation)
	e.canonicalMu.Lock()
	e.contextInvoked = false
	e.contextUnknown = e.contextUnknown || !safe
	e.canonicalMu.Unlock()
}
func (r *assistantGateNoticeRelay) contextQuiescent(s *assistantExecutionInvocation) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active == 0 && r.failure == nil && (r.current == s || r.retired == s) && s.quiescent()
}

// Caller holds service.mu after actual invocation/Runtime cleanup. The active
// entry is removed only after this disposition, making revocation callbacks
// serialize against the active-to-paused transition.
func (h *assistantRunService) finishContextLocked(e *assistantRunExecution, latest assistantRunRecord, readErr error, stopped bool) {
	owner := h.contextOwners[e.record.ID]
	if owner == nil {
		e.contextHandle.Release()
		return
	}
	e.canonicalMu.Lock()
	unknown := e.contextInvoked || e.contextUnknown
	e.canonicalMu.Unlock()
	if unknown {
		owner.quarantined = true
		h.contexts.registry.quarantineRun(e.record.ID)
	} else if readErr == nil && assistantRunPaused(latest.State) && !stopped && !h.closed && !owner.retired {
		owner.paused = true
	} else {
		h.settleContextLocked(e.record.ID)
	}
	// Quarantine retains its separate run reference; releasing the invocation
	// handle cannot Dispose while unknown callbacks might still hold Chat access.
	e.contextHandle.Release()
}
func (h *assistantRunService) settleContextLocked(id string) {
	owner := h.contextOwners[id]
	if owner == nil || owner.quarantined {
		return
	}
	delete(h.contextOwners, id)
	h.contexts.registry.settleRun(id)
}
func (h *assistantRunService) settleIdleContextsLocked(binding *assistantRunBinding) {
	for id, owner := range h.contextOwners {
		if binding != nil && assistantContextAuthorityOf(owner.binding) != assistantContextAuthorityOf(*binding) {
			continue
		}
		owner.retired = true
		if h.active[id] == nil {
			h.settleContextLocked(id)
		}
	}
}
func (h *assistantRunService) retireContextAuthority(binding assistantRunBinding) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.settleIdleContextsLocked(&binding)
}

// Called only after successful authoritative conversation replacement and after
// the workspace/private-data locks release. Do not create a service to clean up
// a registry which never existed. The current service still checks the exact
// authenticated principal/project and removed saved-owner identity.
func (s *Server) retireSavedContextOwners(binding assistantRunBinding, ids []string) {
	s.assistantRunServiceMu.Lock()
	host := s.assistantRunHost
	s.assistantRunServiceMu.Unlock()
	if host == nil || host.contexts == nil {
		return
	}
	host.mu.Lock()
	defer host.mu.Unlock()
	for _, id := range ids {
		host.contexts.registry.retireOwner(binding.Principal, binding.Project, id)
		for runID, owner := range host.contextOwners {
			if owner.binding.Principal != binding.Principal || owner.binding.Project != binding.Project || owner.binding.Owner.Kind != "chat" || owner.binding.Owner.ID != id {
				continue
			}
			owner.retired = true
			if active := host.active[runID]; active != nil {
				active.cancel()
			} else {
				host.settleContextLocked(runID)
			}
		}
	}
}

// Conversation deletion is already authorized by its existing workspace CAS.
// Capture only the authenticated principal/project for retirement, without
// granting assistant admission or changing that workspace's access policy.
func (s *Server) contextDeletionBinding(r *http.Request) (assistantRunBinding, bool) {
	held, ok := s.sessionOf(r)
	if !ok {
		return assistantRunBinding{}, false
	}
	issuer := ""
	if held.issuer != nil {
		issuer = *held.issuer
	}
	store := s.assistantRuns()
	if store.problem != nil {
		return assistantRunBinding{}, false
	}
	return assistantRunBinding{Principal: digestOf([]byte(issuer + "\x00" + held.subject)), Project: store.project}, true
}

// Fresh authorization is still performed by the original source/owner paths.
// This helper is only for using an already retained private capability.
func assistantContextOnSource(source *assistantRunSource, handle *assistantChatContextHandle) {
	if handle != nil {
		source.chat = handle.Chat()
	}
}

func removedSavedContextOwners(before, after []byte) []string {
	var previous, incoming conversationDocument
	if json.Unmarshal(before, &previous) != nil || json.Unmarshal(after, &incoming) != nil {
		return nil
	}
	kept := map[string]bool{}
	for _, raw := range incoming.Chats {
		var chat struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &chat) == nil {
			kept[chat.ID] = true
		}
	}
	var removed []string
	for _, raw := range previous.Chats {
		var chat struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &chat) == nil && chat.ID != "" && !kept[chat.ID] {
			removed = append(removed, chat.ID)
		}
	}
	return removed
}

func (source assistantRunSource) chatScope() runcontext.ChatScope {
	if source.chat == nil {
		return nil
	}
	return source.chat
}
