package desk

// Answer preflight is exact source data, never a writer or execution authority.
// It is prepared before the lease transaction and consumed by the first open.
import (
	"context"
	"net/http"
	"reflect"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

type assistantPreparedAnswerInput struct {
	chat      *assistantChatInput
	custody   *assistantChatCustody
	savedPack *assistantSavedPackBasis
	policies  map[string]string
	creation  *assistantCreationInput
}

func (s *Server) prepareAssistantAnswerInput(r *http.Request, source assistantRunSource, history []assistantRunAnswerOrigin) (*assistantPreparedAnswerInput, error) {
	ctx := r.Context()
	authorize := func(ctx context.Context) error {
		identity, _, err := s.assistantRunIdentity(r.WithContext(ctx))
		if err != nil || !assistantRunIdentityMatches(identity, source.Binding) {
			return errAssistantRunScope
		}
		fresh, err := s.readAssistantRunSource(r.WithContext(ctx), source.Start, identity, source.Binding.Project)
		if err != nil {
			return err
		}
		fresh.Binding.ExecutionKey, fresh.Binding.Unit = source.Binding.ExecutionKey, source.Binding.Unit
		if !assistantRunFreshSourceEqual(source, fresh) {
			return errAssistantRunScope
		}
		return nil
	}
	if err := authorize(ctx); err != nil {
		return nil, err
	}
	chat, err := compileAssistantChatInput(source)
	if err != nil {
		return nil, err
	}
	prepared := &assistantPreparedAnswerInput{chat: chat, custody: &assistantChatCustody{}}
	creation, err := compileAssistantCreationInput(source)
	if err != nil {
		return nil, err
	}
	if creation != nil && contains([]string{"establish-cases", "propose-correction"}, creation.Action.Kind) {
		if _, _, err := s.verifyAssistantCreationSources(ctx, r, source, prepared.custody, authorize, creation); err != nil {
			return nil, err
		}
		if err := creation.Fresh(ctx); err != nil {
			return nil, err
		}
		prepared.creation = creation
	}
	id, _, err := assistantSavedPackLink(source.Original)
	if err != nil {
		return nil, err
	}
	if id != "" {
		read, err := s.readAssistantTestTarget(ctx, source, id)
		if err != nil {
			return nil, err
		}
		prepared.savedPack, err = prepareAssistantSavedPackInput(source, read, chat)
		if err != nil {
			return nil, err
		}
	}
	service := s.policyServiceWithAuthority(func() bool {
		if ctx.Err() != nil {
			return false
		}
		held, ok := s.sessionOf(r.WithContext(ctx))
		return ok && validPolicyActor(policyActorOf(held))
	})
	// Capture before candidate/artifact validation. A Policy change during that
	// validation must fail the final CAS rather than bless a new unseen basis.
	if chat.Policy != nil {
		err = service.repository.View(ctx, func(ledger policyLedger) error {
			if err := service.authorized(ctx); err != nil {
				return err
			}
			var err error
			prepared.policies, err = assistantAnswerPolicyRevisions(ledger, chat.Policy)
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	if err := prepareAssistantPolicy(ctx, service, prepared.custody, chat.Policy); err != nil {
		return nil, err
	}
	held, err := s.holdAssistantDocuments(ctx, r, source, prepared.custody, chat.References)
	if err != nil {
		return nil, err
	}
	if err := appendAssistantChatDocuments(chat, held); err != nil {
		return nil, err
	}
	if err := assistantRunAnswerHistory(assistantRunRecord{AnswerHistory: history}, chat); err != nil {
		return nil, err
	}
	if assistantCriticEnabled(chat, source.Binding.Phase) && runcontext.CriticForcedOff(runcontext.CriticForcedFacts{Purpose: chat.CriticPurpose}) == "" {
		prepared.custody.critic, err = s.readAssistantChatCriticSnapshot(ctx, source, false)
		if err != nil {
			return nil, err
		}
	}
	if err := authorize(ctx); err != nil {
		return nil, err
	}
	return prepared, nil
}

func assistantAnswerPolicyRevisions(ledger policyLedger, scope *assistantPolicyScope) (map[string]string, error) {
	ids := []string{scope.ID}
	for _, source := range scope.Sources {
		ids = append(ids, source.PolicyID)
	}
	revisions := map[string]string{}
	for _, id := range ids {
		if id == "" {
			continue
		}
		record, ok := ledger.Policies[id]
		if !ok {
			return nil, errAssistantRunScope
		}
		// Include publications and the full current source-authority stream.
		revisions[id] = policyRecordRevision(record) + ":" + policyAuthorityRevision(ledger.SourceAuthorities[id])
	}
	return revisions, nil
}

// Caller holds the existing private workspace transaction. Only current local
// custody reads occur here; verification and context construction already ended.
func (s *Server) recheckAssistantAnswerInputLocked(r *http.Request, source assistantRunSource, prepared *assistantPreparedAnswerInput) error {
	if prepared.savedPack != nil {
		config, _, err := s.assistantDecisionDeclarations()
		if err != nil {
			return err
		}
		store, err := s.openChatData()
		if err != nil {
			return err
		}
		defer store.root.Close()
		name, err := resolveConversationName(store.root, s.conversationIdentity())
		if err != nil {
			return err
		}
		read, err := s.readAssistantTestTargetLocked(store.root, name, config, prepared.savedPack.Read.Owner)
		if err != nil {
			return err
		}
		if !assistantTestSame(read, prepared.savedPack.Read) {
			return errAssistantRunConflict
		}
	}
	if prepared.chat.Policy != nil {
		// This is the established workspace -> Policy read order documented by
		// admitPolicyWorkspaceUpdate. Never use e.Authorize or service.get here:
		// those would reacquire the workspace lock or reverify artifact bytes.
		err := s.policyRepository().View(r.Context(), func(ledger policyLedger) error {
			if err := s.authorizePolicyHistory(r); err != nil {
				return err
			}
			revisions, err := assistantAnswerPolicyRevisions(ledger, prepared.chat.Policy)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(revisions, prepared.policies) {
				return errAssistantRunConflict
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	for _, document := range prepared.custody.documents {
		raw, pin, err := s.assistantDocumentSnapshotLocked(r, document.owner, document.reference)
		if err != nil {
			return err
		}
		if digestOf(raw) != document.attachmentDigest || !reflect.DeepEqual(pin, document.gateway) {
			return errAssistantDocument
		}
	}
	if prepared.creation != nil {
		if err := prepared.creation.FreshLocked(r.Context()); err != nil {
			return err
		}
	}
	if held := prepared.custody.critic; held != nil {
		current, err := s.readAssistantChatCriticSnapshot(r.Context(), source, true)
		if err != nil {
			return err
		}
		if !assistantCriticSame(held, current) || !reflect.DeepEqual(held.cases, current.cases) {
			return errAssistantRunConflict
		}
	}
	return r.Context().Err()
}
