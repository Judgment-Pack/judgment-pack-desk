package desk

import (
	"context"
	"net/http"
	"reflect"
)

type assistantLinkVerified struct {
	Held          assistantLinkCustody
	Attachment    assistantLinkAttachment
	Stored        attachmentReceipt
	Configuration assistantLinkConfiguration
}

// Only explicit terminal projection reconciles exact declared receipts. Generic
// GET/list/SSE/Stop never call this path, and no network/storage effect is replayed.
func (s *Server) assistantLinkProjectionSnapshot(r *http.Request, id string) ([]assistantLinkVerified, error) {
	identity, _, err := s.assistantRunIdentity(r)
	if err != nil {
		return nil, err
	}
	store := s.assistantRuns()
	run, err := store.lookup(r.Context(), identity, id, "", false)
	if err != nil {
		return nil, err
	}
	if len(run.Links) == 0 || run.ChatProjection != nil {
		return nil, nil
	}
	_, err = store.transaction(r.Context(), func(ledger *assistantRunLedger) (bool, error) {
		current, err := assistantChatProjectionRun(ledger, id)
		if err != nil {
			return false, err
		}
		if !assistantRunTerminal(current.State) {
			return false, errAssistantRunConflict
		}
		if err = s.authorizeAssistantRun(r, *current); err != nil {
			return false, err
		}
		data, err := s.openChatData()
		if err != nil {
			return false, err
		}
		defer data.root.Close()
		if err = store.checkScope(data.root); err != nil {
			return false, err
		}
		name, err := resolveConversationName(data.root, s.conversationIdentity())
		if err != nil {
			return false, err
		}
		changed := false
		for i := range current.Links {
			found, err := reconcileAssistantLinkStorage(&current.Links[i], func(id string) ([]byte, error) {
				return readPrivateData(data.root, attachmentProjectPrefix(name)+id+".json", maxAttachmentObjectBytes)
			})
			if err != nil {
				return false, err
			}
			changed = changed || found
		}
		if changed {
			current.Revision++
		}
		return changed, nil
	})
	if err != nil {
		return nil, err
	}
	run, err = store.lookup(r.Context(), identity, id, "", false)
	if err != nil {
		return nil, err
	}
	lock, err := s.privateDataLockContext(r.Context(), true)
	if err != nil {
		return nil, err
	}
	var source assistantRunSource
	packets := map[string][]byte{}
	capture := func() error {
		defer lock.Close()
		if err = s.authorizeAssistantRun(r, run); err != nil {
			return err
		}
		source, err = s.assistantSearchProjectionSourceLocked(r, run)
		if err != nil {
			return err
		}
		data, err := s.openChatData()
		if err != nil {
			return err
		}
		defer data.root.Close()
		if err = store.checkScope(data.root); err != nil {
			return err
		}
		name, err := resolveConversationName(data.root, s.conversationIdentity())
		if err != nil {
			return err
		}
		for _, held := range run.Links {
			if held.Final.Receipt == nil {
				continue
			}
			receipts := []*attachmentReceipt{held.Stage.Receipt, held.SealStage.Receipt, held.Final.Receipt}
			if held.AuthorityPacket != nil {
				receipts = append(receipts, held.AuthorityPacket)
			}
			for _, receipt := range receipts {
				if receipt == nil {
					return errAssistantDocument
				}
				raw, err := readPrivateData(data.root, attachmentProjectPrefix(name)+receipt.ID+".json", maxAttachmentObjectBytes)
				if err != nil || digestOf(raw) != receipt.SHA256 {
					return errAssistantDocument
				}
				if receipt.ID == held.FinalID {
					packets[held.HostOperationID] = raw
				}
			}
		}
		return nil
	}
	if err = capture(); err != nil {
		return nil, err
	}
	if len(packets) == 0 {
		return nil, nil
	}
	program, err := s.researchVerifier()
	if err != nil {
		return nil, err
	}
	verified := []assistantLinkVerified{}
	for _, held := range run.Links {
		raw := packets[held.HostOperationID]
		if raw == nil {
			continue
		}
		config, err := s.currentAssistantLinkConfiguration(r.Context(), source, held)
		if err != nil {
			return nil, err
		}
		_, file, err := verifyAssistantLinkObject(r.Context(), program, raw, held)
		if err != nil {
			return nil, err
		}
		var intent assistantHostIntent
		op := assistantDialectOperation(run, held.HostOperationID)
		if op == nil || readAssistantHostReceipt(op.Intent, &intent) != nil {
			return nil, errAssistantDocument
		}
		args, err := readAssistantLinkArguments(intent.Arguments)
		if err != nil {
			return nil, err
		}
		file.Link.Anchor = args.Link.Anchor
		if current, err := s.currentAssistantLinkConfiguration(r.Context(), source, held); err != nil || !reflect.DeepEqual(current, config) {
			return nil, errAssistantRunScope
		}
		verified = append(verified, assistantLinkVerified{held, file, *held.Final.Receipt, config})
	}
	return verified, nil
}
func (s *Server) currentAssistantLinkConfiguration(ctx context.Context, source assistantRunSource, held assistantLinkCustody) (assistantLinkConfiguration, error) {
	c, err := s.assistantLinkConfiguration(false)
	if err != nil {
		return c, err
	}
	c.SearchConfigurationDigest, c.SearchPreferenceDigest = held.SearchConfigurationDigest, held.SearchPreferenceDigest
	c.AuthorityPacket = held.AuthorityPacket
	if c.SearchPreferenceDigest != "" {
		_, mode, pref, err := s.assistantSearchPreference(source)
		if err != nil || pref != c.SearchPreferenceDigest {
			return c, errAssistantRunScope
		}
		choice, err := assistantSearchChoice(source, mode)
		if err != nil || choice != "research" {
			return c, errAssistantRunScope
		}
	}
	if c.SearchConfigurationDigest != "" {
		search, err := s.assistantSearchConfigurationCurrent(ctx, source, false)
		if err != nil || assistantSearchConfigurationDigest(search) != c.SearchConfigurationDigest {
			return c, errAssistantRunScope
		}
	}
	if c.Snapshot != held.Gateway || assistantLinkConfigurationDigest(c) != held.ConfigurationDigest {
		return c, errAssistantRunScope
	}
	return c, nil
}
func (s *Server) checkAssistantLinkProjectionLocked(r *http.Request, run *assistantRunRecord, verified []assistantLinkVerified) error {
	if len(verified) == 0 {
		return nil
	}
	source, err := s.assistantSearchProjectionSourceLocked(r, *run)
	if err != nil {
		return err
	}
	current, err := s.assistantLinkConfiguration(false)
	if err != nil {
		return err
	}
	data, err := s.openChatData()
	if err != nil {
		return err
	}
	defer data.root.Close()
	name, err := resolveConversationName(data.root, s.conversationIdentity())
	if err != nil {
		return err
	}
	for _, v := range verified {
		c := current
		c.SearchConfigurationDigest, c.SearchPreferenceDigest = v.Configuration.SearchConfigurationDigest, v.Configuration.SearchPreferenceDigest
		c.AuthorityPacket = v.Configuration.AuthorityPacket
		if !reflect.DeepEqual(c, v.Configuration) {
			return errAssistantRunScope
		}
		if c.SearchPreferenceDigest != "" {
			_, _, pref, err := s.assistantSearchPreference(source)
			if err != nil || pref != c.SearchPreferenceDigest {
				return errAssistantRunScope
			}
		}
		held, err := assistantLinkCustodyOf(*run, v.Held.HostOperationID)
		if err != nil || !reflect.DeepEqual(held, v.Held) {
			return errAssistantRunConflict
		}
		receipts := []*attachmentReceipt{held.Stage.Receipt, held.SealStage.Receipt, held.Final.Receipt}
		if held.AuthorityPacket != nil {
			receipts = append(receipts, held.AuthorityPacket)
		}
		for _, receipt := range receipts {
			if receipt == nil {
				return errAssistantDocument
			}
			raw, err := readPrivateData(data.root, attachmentProjectPrefix(name)+receipt.ID+".json", maxAttachmentObjectBytes)
			if err != nil || digestOf(raw) != receipt.SHA256 {
				return errAssistantDocument
			}
		}
	}
	return nil
}
func (s *Server) recoverAssistantLinkOutputs(r *http.Request, id string, verified []assistantLinkVerified) error {
	if len(verified) == 0 {
		return nil
	}
	_, err := s.assistantRuns().transaction(r.Context(), func(ledger *assistantRunLedger) (bool, error) {
		run, err := assistantChatProjectionRun(ledger, id)
		if err != nil {
			return false, err
		}
		if !assistantRunTerminal(run.State) || run.Historical || run.ChatProjection != nil {
			return false, nil
		}
		if err = s.authorizeAssistantRun(r, *run); err != nil {
			return false, err
		}
		if err = s.checkAssistantLinkProjectionLocked(r, run, verified); err != nil {
			return false, err
		}
		changed := false
		for _, v := range verified {
			exists := false
			for _, out := range run.DomainOutputs {
				if out.OperationID == v.Held.HostOperationID {
					exists = true
				}
			}
			if exists {
				continue
			}
			op := assistantDialectOperation(*run, v.Held.HostOperationID)
			// Known refusals stay refused. Only exact completed unknown final writes
			// have an explicit recovery path; the original host result stays unchanged.
			if op == nil || op.State != "refused" || !v.Held.DispatchClosed {
				continue
			}
			var in assistantHostIntent
			if readAssistantHostReceipt(op.Intent, &in) != nil {
				return false, errAssistantRunInvalid
			}
			run.DomainOutputs = append(run.DomainOutputs, assistantDomainOutput{Kind: "link-document", RunID: run.ID, RequestKey: run.Binding.ExecutionKey, OperationID: op.ID, CallID: in.CallID, Link: &assistantLinkOutput{Recovered: true, Attachment: v.Attachment, Stored: v.Stored, AcquisitionID: op.ID}})
			changed = true
		}
		if changed {
			run.Revision++
		}
		return changed, nil
	})
	return err
}
