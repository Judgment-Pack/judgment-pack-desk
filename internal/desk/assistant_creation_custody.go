package desk

import (
	"net/http"
	"os"
)

// Terminal projection shares the original source and artifact CAS. An earlier
// successful check cannot carry a changed artifact or source through this write.
// The caller owns the canonical private-data transaction.
func (s *Server) checkAssistantCreationProjectionLocked(r *http.Request, root *os.Root, name string, selected []byte, run assistantRunRecord) error {
	chat, err := assistantTestObject(selected)
	if err != nil {
		return err
	}
	cp, err := assistantTestObject(chat["checkpoint"])
	if err != nil {
		return err
	}
	state, err := assistantTestObject(cp["state"])
	if err != nil {
		return err
	}
	if chat["draftGeneration"] != nil || run.Creation != nil {
		if err := s.validateAssistantCreationDraftLocked(r, root, name, chat, state); err != nil {
			return err
		}
	}
	if run.Creation == nil {
		return nil
	}
	start := assistantRunStart{Purpose: "chat", AdmissionID: run.AdmissionID, OwnerID: run.Binding.Owner.ID, Revision: run.Binding.Owner.Revision, RequestID: run.Binding.RequestID}
	source := assistantRunSource{Start: start, Binding: run.Binding, Original: run.Creation.Source, Request: run.Creation.Request}
	owner := assistantDocumentOwnerOf(source)
	files := assistantSavedLinkAttachments(source.Original)
	for _, proof := range run.Creation.Sources {
		match := assistantCreationPage.FindStringSubmatch(proof.Location)
		if len(match) != 4 {
			return errAssistantDocument
		}
		found := false
		for _, file := range files {
			if file.Document == nil || file.Document.ID != match[1] || file.Document.Digest != match[2] {
				continue
			}
			raw, gateway, err := s.assistantDocumentSnapshotLocked(r, owner, *file.Document)
			if err != nil || digestOf(raw) != proof.AttachmentDigest || digestOf(assistantChatJSON(assistantSearchGatewaySnapshot(gateway))) != proof.GatewayDigest {
				return errAssistantDocument
			}
			found = true
			break
		}
		if !found {
			return errAssistantDocument
		}
	}
	return nil
}
