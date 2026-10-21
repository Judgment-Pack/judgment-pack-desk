package desk

import (
	"bytes"
	"net/http"
)

// Acquire the owning preparation view before entering the private ledger. Never
// acquire a preparation/private lock from a queued artifact authority callback.
// Already persisted chat output acknowledges by exact readback, without effects.
func (s *Server) assistantDialectProjectionView(r *http.Request, id string, write bool) (inputPreparationRead, error) {
	need := false
	store := s.assistantRuns()
	_, err := store.transaction(r.Context(), func(ledger *assistantRunLedger) (bool, error) {
		run, err := assistantChatProjectionRun(ledger, id)
		if err != nil {
			return false, err
		}
		if err = s.authorizeAssistantRun(r, *run); err != nil {
			return false, err
		}
		if !write && run.ChatProjection != nil {
			return false, nil
		}
		if write && run.ChatProjection != nil {
			root, _, _, selected, err := s.assistantChatProjectionOwner(r, store, run)
			if err != nil {
				return false, err
			}
			defer root.Close()
			if digestOf(selected) == run.ChatProjection.Digest {
				return false, nil
			}
		}
		for _, out := range run.DomainOutputs {
			if out.Kind == "dialect-action" && out.DialectAction != nil && out.DialectAction.Action != nil {
				need = true
			}
		}
		return false, nil
	})
	if err != nil || !need {
		return nil, err
	}
	return s.preparationRepository().TryRead(r.Context())
}
func (s *Server) checkAssistantDialectProjection(r *http.Request, run assistantRunRecord, view inputPreparationReader) error {
	for _, out := range run.DomainOutputs {
		if out.Kind != "dialect-action" {
			continue
		}
		p := out.DialectAction
		if p == nil {
			return errAssistantRunInvalid
		}
		if p.Published != nil {
			if err := s.checkAssistantDialectPublicationEvidence(r.Context(), *p); err != nil {
				return err
			}
			continue
		}
		if view == nil || p.Action == nil {
			return errAssistantRunScope
		}
		current, err := view.Get(p.Read.ID)
		if err != nil {
			return err
		}
		if current.SHA256 != p.Read.Revision {
			return errAssistantRunConflict
		}
		if p.Action.Kind == "update-dialect" {
			config, _, err := s.assistantDecisionDeclarations()
			if err != nil {
				return err
			}
			entry, ok := config.Packs[p.Read.PackKey]
			if !ok || entry.Path != p.Read.PackPath || assistantDeclarationDigest(entry) != p.Read.Declaration {
				return errAssistantRunConflict
			}
			file := s.readProjectFile(entry.Path)
			if file.err != nil || !file.present || !bytes.Equal(file.data, p.Read.Pack) {
				return errAssistantRunConflict
			}
		} else {
			ledger, err := s.readDialectPublicationLedger()
			if err != nil {
				return err
			}
			if latestDialectPublication(ledger, p.Read.ID, p.Read.Variant) != p.Read.BasePublication {
				return errAssistantRunConflict
			}
			pub := p.Action.Publication
			if pub == nil || pub.Answer.Publication.Dialect == nil || current.Document.Recipe.Qualification == nil {
				return errAssistantRunInvalid
			}
			dependencies := pub.Answer.Publication.Dialect
			if dependencies.Recipe != artifactReference(dialectRecipeBytes(current.Document)) || dependencies.Pack != artifactReference([]byte(current.Document.Recipe.Qualification.Pack)) {
				return errAssistantRunConflict
			}
			if err = s.checkAssistantDialectPublicationEvidence(r.Context(), *p); err != nil {
				return err
			}
		}
	}
	return s.authorizeAssistantRun(r, run)
}
