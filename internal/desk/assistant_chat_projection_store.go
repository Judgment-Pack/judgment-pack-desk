package desk

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
)

// Caller owns the current private-data transaction; opening a root does not
// reacquire that lock. No caller-provided JSON, path or authorization callback.
func (s *Server) assistantChatProjectionOwner(r *http.Request, store assistantRunStore, run *assistantRunRecord) (*os.Root, string, []byte, []byte, error) {
	identity, _, err := s.assistantRunIdentity(r)
	if err != nil || !assistantRunIdentityMatches(identity, run.Binding) || run.Historical || run.Binding.Project != store.project || run.Binding.Owner.Kind != "chat" || !assistantRunTerminal(run.State) {
		return nil, "", nil, nil, errAssistantRunScope
	}
	data, err := s.openChatData()
	if err != nil {
		return nil, "", nil, nil, err
	}
	fail := func(e error) (*os.Root, string, []byte, []byte, error) {
		data.root.Close()
		return nil, "", nil, nil, e
	}
	if err = checkPrivateDataPath(data); err != nil {
		return fail(err)
	}
	if err = store.checkScope(data.root); err != nil {
		return fail(err)
	}
	name, err := resolveConversationName(data.root, s.conversationIdentity())
	if err != nil {
		return fail(err)
	}
	full, err := readPrivateData(data.root, name, maxConversationBytes)
	if err != nil {
		return fail(err)
	}
	_, rows, err := assistantChatRows(full)
	if err != nil {
		return fail(err)
	}
	index, err := assistantChatSelected(rows, run.Binding.Owner.ID)
	if err != nil {
		return fail(err)
	}
	selected := rows[index]
	if !assistantChatRequest(selected, run.Binding.RequestID, run.Binding.RequestDigest) {
		return fail(errAssistantRunScope)
	}
	if err = s.authorizePolicyConversations(r, full); err != nil {
		return fail(err)
	}
	return data.root, name, full, selected, nil
}
func assistantChatProjectionRun(ledger *assistantRunLedger, id string) (*assistantRunRecord, error) {
	for i := range ledger.Runs {
		if ledger.Runs[i].ID == id {
			return &ledger.Runs[i], nil
		}
	}
	return nil, errAssistantRunNotFound
}
func assistantChatProjectionMatches(run *assistantRunRecord, selected []byte) bool {
	digest := digestOf(selected)
	return digest == run.Binding.Owner.Revision || run.ChatProjection != nil && digest == run.ChatProjection.Digest
}

// Prepare persists immutable output before any conversation write. Repeated
// preparation never rebuilds a timestamp, candidate or response identity.

func (s *Server) prepareAssistantChatProjection(r *http.Request, id string) error {
	view, err := s.assistantDialectProjectionView(r, id, false)
	if err != nil {
		return err
	}
	if view != nil {
		defer view.Close()
	}
	store := s.assistantRuns()
	issue := false
	_, err = store.transaction(r.Context(), func(ledger *assistantRunLedger) (bool, error) {
		run, err := assistantChatProjectionRun(ledger, id)
		if err != nil {
			return false, err
		}
		root, conversation, full, selected, err := s.assistantChatProjectionOwner(r, store, run)
		if err != nil {
			return false, err
		}
		defer root.Close()
		if !assistantChatProjectionMatches(run, selected) {
			return false, errAssistantRunConflict
		}
		if run.ChatProjection != nil {
			return false, nil
		}
		// A terminal link is prepared only after its exact suite proposal exists.
		if err := validateAssistantDomainOutputs(*run); err != nil {
			return false, err
		}
		if err := s.checkAssistantDialectProjection(r, *run, view); err != nil {
			return false, err
		}
		if err := validateAssistantTestProjectionSource(selected, *run); err != nil {
			return false, err
		}
		if err := s.materializeAssistantTestOutputsLocked(r, root, conversation, *run); err != nil {
			return false, err
		}
		output, err := compileAssistantChatProjection(selected, *run, assistantChatProjectionTime())
		code := ""
		if errors.Is(err, errAssistantChatProjectionUnsupported) {
			code = "proposal-target"
			if errors.Is(err, errAssistantChatProjectionQuestion) {
				code = "pending-question"
			}
		} else if err != nil {
			return false, err
		} else {
			fields, rows, err := assistantChatRows(full)
			if err != nil {
				return false, err
			}
			index, err := assistantChatSelected(rows, run.Binding.Owner.ID)
			if err != nil {
				return false, err
			}
			rows[index] = output
			fields["chats"] = assistantChatEncodeArray(rows)
			candidate := assistantChatEncodeObject(fields)
			// The existing conversation validator may be narrower than canonical
			// event/operation data. Keep that refusal explicit; never rewrite
			// typed originals or persist an unusable prepared projection.
			if !assistantChatProjectionUnicode(output) {
				code = "unsupported-unicode"
			} else if len(candidate) > maxConversationBytes || validateConversations(candidate) != nil {
				code = "saved-chat-validation"
			}
		}
		if code != "" {
			issue = true
			if run.ProjectionIssue == code {
				return false, nil
			}
			run.ProjectionIssue = code
			run.Revision++
			return true, nil
		}
		run.ChatProjection = &assistantChatProjection{Version: 1, SourceDigest: run.Binding.Owner.Revision, EventDigest: assistantChatEventDigest(*run), Output: output, Digest: digestOf(output), ResponseID: assistantChatResponseID(run.ID)}
		run.ProjectionIssue = ""
		run.Revision++
		return true, nil
	})
	if err == nil && issue {
		return errAssistantChatProjectionUnsupported
	}
	return err
}

// Write and read back exactly the prepared selected chat. A separate ack lets
// recovery distinguish retained output from its observed conversation write.
// The workspace write lock precedes the private-data lock, as in conversations.
func (s *Server) writeAssistantChatProjection(r *http.Request, id string) error {
	s.writes.Lock()
	defer s.writes.Unlock()
	view, err := s.assistantDialectProjectionView(r, id, true)
	if err != nil {
		return err
	}
	if view != nil {
		defer view.Close()
	}
	store := s.assistantRuns()
	_, err = store.transaction(r.Context(), func(ledger *assistantRunLedger) (bool, error) {
		run, err := assistantChatProjectionRun(ledger, id)
		if err != nil {
			return false, err
		}
		root, name, full, selected, err := s.assistantChatProjectionOwner(r, store, run)
		if err != nil {
			return false, err
		}
		defer root.Close()
		projection := run.ChatProjection
		if projection == nil {
			return false, errAssistantRunDispatch
		}
		if !assistantChatProjectionMatches(run, selected) {
			return false, errAssistantRunConflict
		}
		// Preparation and write are separate recoverable transactions. Verify
		// suite custody again under this final write/readback lock so a removed
		// or conflicting proposal cannot leave a newly persisted dangling link.
		if err := s.materializeAssistantTestOutputsLocked(r, root, name, *run); err != nil {
			return false, err
		}
		if digestOf(selected) == projection.Digest {
			return false, nil
		}
		if err := s.checkAssistantDialectProjection(r, *run, view); err != nil {
			return false, err
		}
		fields, rows, err := assistantChatRows(full)
		if err != nil {
			return false, err
		}
		index, err := assistantChatSelected(rows, run.Binding.Owner.ID)
		if err != nil {
			return false, err
		}
		rows[index] = projection.Output
		fields["chats"] = assistantChatEncodeArray(rows)
		next := assistantChatEncodeObject(fields)
		if len(next) > maxConversationBytes || validateConversations(next) != nil {
			return false, errAssistantRunInvalid
		}
		if err = s.authorizePolicyConversations(r, next); err != nil {
			return false, err
		}
		if err = s.admitPolicyWorkspaceUpdate(r, full, next, false); err != nil {
			return false, err
		}
		if err = s.retainConversationGraphDrafts(r, root, name, full, next); err != nil {
			return false, err
		}
		needsRecipeGuard := false
		for _, out := range run.DomainOutputs {
			if out.Kind == "dialect-recipe" {
				needsRecipeGuard = true
			}
		}
		if needsRecipeGuard {
			// Hold the normal workflow through conversation write/readback; a
			// queued edit/discard cannot race the independent draft observation.
			unlockDrafts, err := s.lockDialectDraftWorkflow(r.Context())
			if err != nil {
				return false, err
			}
			defer unlockDrafts()
			if err = s.checkAssistantDialectRecipeDrafts(r, *run); err != nil {
				return false, err
			}
			if err = s.retainConversationDialectDraftsLocked(r.Context(), full, next); err != nil {
				return false, err
			}
		} else if err = s.retainConversationDialectDrafts(r.Context(), full, next); err != nil {
			return false, err
		}
		if err := s.checkAssistantDialectProjection(r, *run, view); err != nil {
			return false, err
		}
		// Recheck current authentication immediately before the bounded private write.
		if err = s.authorizeAssistantRun(r, *run); err != nil {
			return false, err
		}
		if err = writePrivateData(root, name, next); err != nil {
			return false, err
		}
		saved, err := readPrivateData(root, name, maxConversationBytes)
		if err != nil {
			return false, err
		}
		if !bytes.Equal(saved, next) {
			return false, errAssistantRunConflict
		}
		return false, nil
	})
	return err
}
func (s *Server) acknowledgeAssistantChatProjection(r *http.Request, id string) error {
	store := s.assistantRuns()
	_, err := store.transaction(r.Context(), func(ledger *assistantRunLedger) (bool, error) {
		run, err := assistantChatProjectionRun(ledger, id)
		if err != nil {
			return false, err
		}
		root, _, _, selected, err := s.assistantChatProjectionOwner(r, store, run)
		if err != nil {
			return false, err
		}
		defer root.Close()
		p := run.ChatProjection
		if p == nil || digestOf(selected) != p.Digest || !bytes.Equal(selected, p.Output) {
			return false, errAssistantRunConflict
		}
		if p.Acknowledged {
			return false, nil
		}
		p.Acknowledged = true
		run.Revision++
		return true, nil
	})
	return err
}

// An explicit model-free recovery repeats only the saved bytes/readback/ack.
// It never starts an executor, continues a checkpoint or dispatches an operation.
func (s *Server) reconcileAssistantChatProjection(r *http.Request, id string) error {
	if err := s.prepareAssistantChatProjection(r, id); err != nil {
		return err
	}
	if err := s.writeAssistantChatProjection(r, id); err != nil {
		return err
	}
	return s.acknowledgeAssistantChatProjection(r, id)
}

// The concrete terminal executor may call this after its final checkpoint and
// host state have been saved. No production executor is activated by this method.
func (e *assistantRunExecution) ProjectChat(ctx context.Context) error {
	return e.service.server.reconcileAssistantChatProjection(e.request.Clone(ctx), e.record.ID)
}
