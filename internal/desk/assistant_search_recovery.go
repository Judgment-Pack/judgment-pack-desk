package desk

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"strings"
)

type assistantSearchVerified struct {
	Held          assistantSearchCustody
	Result        assistantSearchResult
	Stored        attachmentReceipt
	Configuration assistantSearchConfiguration
}

// This explicit custody path requires a current run's exact final storage
// intent. It cannot nominate arbitrary attachment IDs or widen document access.
func verifyAssistantSearchObject(ctx context.Context, program string, raw []byte, held assistantSearchCustody) (assistantSearchVerified, error) {
	zero := assistantSearchVerified{}
	if held.Stage.Receipt == nil || held.SealStage.Receipt == nil || held.Original.Receipt == nil || held.Final.Intent == nil || held.Final.Intent.ID != held.FinalID || digestOf(raw) != held.Final.Intent.SHA256 || validateAttachmentObject(raw) != nil || !assistantDocumentPacketFields(raw) {
		return zero, errAssistantSearch
	}
	var object attachmentObject
	if json.Unmarshal(raw, &object) != nil || object.Proof == nil {
		return zero, errAssistantSearch
	}
	proof := object.Proof
	if proof.Source != "web-search" || proof.Session != held.Session || proof.Authority != held.Gateway.Authority || proof.PublicKey != held.Gateway.Public || proof.Drive != nil || proof.Gmail != nil || proof.Connected != nil || proof.Resource != nil || proof.Web != nil || object.Original.Name != "web-search.json" || object.Original.MediaType != "application/json" || len(proof.Response) > assistantSearchResponseLimit || len(proof.Registry) > assistantSearchSealLimit+1 {
		return zero, errAssistantSearch
	}
	result, digest, err := verifyResearchProof(ctx, program, inputResearchProof{Session: held.Session, Source: "web-search", Arguments: assistantChatJSON(held.Request), Pin: inputResearchPin{held.Gateway.Authority, held.Gateway.Public}, Response: []byte(proof.Response), Seal: []byte(proof.Registry)})
	if err != nil || digest != held.Digest {
		return zero, errAssistantSearch
	}
	original, err := base64.StdEncoding.Strict().DecodeString(object.Original.Bytes)
	if err != nil || !bytes.Equal(result, original) || object.Original.SHA256 != digest {
		return zero, errAssistantSearch
	}
	stage, stageErr := assistantSearchPacket("chat-search-acquisition.json", []byte(proof.Response))
	seal, sealErr := assistantSearchPacket("chat-search-seal.json", []byte(strings.TrimSuffix(proof.Registry, "\n")))
	initial, originalErr := assistantSearchOriginal(result)
	if stageErr != nil || sealErr != nil || originalErr != nil || digestOf(stage) != held.Stage.Receipt.SHA256 || digestOf(seal) != held.SealStage.Receipt.SHA256 || digestOf(initial) != held.Original.Receipt.SHA256 {
		return zero, errAssistantSearch
	}
	found, err := readAssistantSearchResult(result, held.Request)
	if err != nil {
		return zero, err
	}
	return assistantSearchVerified{Held: held, Result: found, Stored: *held.Final.Intent}, nil
}
func (s *Server) reconcileAssistantSearchReceipts(r *http.Request, id string) error {
	// The active writer owns its revision. Observational reads must not race a
	// checkpoint/Stop by advancing that revision behind the writer's back.
	host := s.assistantRunService()
	host.mu.Lock()
	active := host.active[id] != nil
	host.mu.Unlock()
	if active {
		return nil
	}
	store := s.assistantRuns()
	_, err := store.transaction(r.Context(), func(ledger *assistantRunLedger) (bool, error) {
		run, err := assistantChatProjectionRun(ledger, id)
		if err != nil {
			return false, err
		}
		if !assistantRunTerminal(run.State) {
			return false, nil
		}
		identity, _, err := s.assistantRunIdentity(r)
		if err != nil || !assistantRunIdentityMatches(identity, run.Binding) {
			return false, errAssistantRunScope
		}
		data, err := s.openChatData()
		if err != nil {
			return false, err
		}
		defer data.root.Close()
		if err = store.checkScope(data.root); err != nil {
			return false, err
		}
		conversation, err := resolveConversationName(data.root, s.conversationIdentity())
		if err != nil {
			return false, err
		}
		changed := false
		for i := range run.Searches {
			found, err := reconcileAssistantSearchStorage(&run.Searches[i], func(id string) ([]byte, error) {
				return readPrivateData(data.root, attachmentProjectPrefix(conversation)+id+".json", maxAttachmentObjectBytes)
			})
			if err != nil {
				return false, err
			}
			changed = changed || found
		}
		if changed {
			run.Revision++
		}
		return changed, nil
	})
	return err
}
func (s *Server) assistantSearchProjectionSnapshot(r *http.Request, id string) ([]assistantSearchVerified, error) {
	identity, _, err := s.assistantRunIdentity(r)
	if err != nil {
		return nil, err
	}
	store := s.assistantRuns()
	run, err := store.lookup(r.Context(), identity, id, "", false)
	if err != nil {
		return nil, err
	}
	if len(run.Searches) == 0 || run.ChatProjection != nil {
		return nil, nil
	}
	if err = s.reconcileAssistantSearchReceipts(r, id); err != nil {
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
	var packets [][]byte
	var source assistantRunSource
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
		for _, held := range run.Searches {
			if held.Final.Intent == nil {
				packets = append(packets, nil)
				continue
			}
			for _, receipt := range []*attachmentReceipt{held.Stage.Receipt, held.SealStage.Receipt} {
				if receipt == nil {
					return errAssistantSearch
				}
				packet, readErr := readPrivateData(data.root, attachmentProjectPrefix(name)+receipt.ID+".json", maxAttachmentObjectBytes)
				if readErr != nil || digestOf(packet) != receipt.SHA256 {
					return errAssistantSearch
				}
			}
			raw, err := readPrivateData(data.root, attachmentProjectPrefix(name)+held.FinalID+".json", maxAttachmentObjectBytes)
			if err != nil {
				if os.IsNotExist(err) {
					packets = append(packets, nil)
					continue
				}
				return err
			}
			packets = append(packets, raw)
		}
		return nil
	}
	if err = capture(); err != nil {
		return nil, err
	}
	hasFinal := false
	for _, packet := range packets {
		if packet != nil {
			hasFinal = true
		}
	}
	if !hasFinal {
		return nil, nil
	}
	configuration, err := s.assistantSearchConfigurationCurrent(r.Context(), source, false)
	if err != nil {
		return nil, err
	}
	program, err := s.researchVerifier()
	if err != nil {
		return nil, err
	}
	verified := []assistantSearchVerified{}
	for i, held := range run.Searches {
		if packets[i] == nil {
			continue
		}
		found, err := verifyAssistantSearchObject(r.Context(), program, packets[i], held)
		if err != nil {
			return nil, err
		}
		// Reconciliation is offline. A changed current source pin cannot convert a
		// historical result into new permission, even if its old proof is valid.
		gateway, err := s.configuredResearchCurrent(false)
		if err != nil {
			return nil, err
		}
		if assistantSearchGatewaySnapshot(gateway) != held.Gateway {
			return nil, errAssistantRunScope
		}
		if held.Request.Connection != configuration.Connection || held.Request.Revision != configuration.Revision || held.ConfigurationDigest != assistantSearchConfigurationDigest(configuration) {
			return nil, errAssistantRunScope
		}
		found.Configuration = configuration
		verified = append(verified, found)
	}
	current, err := s.assistantSearchConfigurationCurrent(r.Context(), source, false)
	if err != nil || !reflect.DeepEqual(current, configuration) {
		return nil, errAssistantRunScope
	}
	return verified, nil
}

// The caller holds the private lock. Verified bytes are compared again after
// the verifier wait; this path never invokes it inside a storage transaction.
func (s *Server) checkAssistantSearchProjectionLocked(r *http.Request, run *assistantRunRecord, verified []assistantSearchVerified) error {
	if len(verified) == 0 {
		return nil
	}
	source, err := s.assistantSearchProjectionSourceLocked(r, *run)
	if err != nil {
		return err
	}
	_, _, preference, err := s.assistantSearchPreference(source)
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
	gateway, err := s.configuredResearchCurrent(false)
	if err != nil {
		return err
	}
	for _, v := range verified {
		if preference != v.Configuration.PreferenceDigest {
			return errAssistantRunScope
		}
		current, err := assistantSearchCustodyOf(*run, v.Held.HostOperationID)
		if err != nil || !reflect.DeepEqual(current, v.Held) {
			return errAssistantRunConflict
		}
		if assistantSearchGatewaySnapshot(gateway) != current.Gateway {
			return errAssistantRunScope
		}
		for _, receipt := range []*attachmentReceipt{current.Stage.Receipt, current.SealStage.Receipt} {
			if receipt == nil {
				return errAssistantSearch
			}
			packet, readErr := readPrivateData(data.root, attachmentProjectPrefix(name)+receipt.ID+".json", maxAttachmentObjectBytes)
			if readErr != nil || digestOf(packet) != receipt.SHA256 {
				return errAssistantSearch
			}
		}
		raw, err := readPrivateData(data.root, attachmentProjectPrefix(name)+current.FinalID+".json", maxAttachmentObjectBytes)
		if err != nil || digestOf(raw) != v.Stored.SHA256 {
			return errAssistantSearch
		}
	}
	return nil
}

// Explicit terminal projection may recover a completed exact final write from
// its receipt. It closes the host operation and retains producer-bound output;
// it cannot dispatch an acquisition, seal or storage write.
func (s *Server) recoverAssistantSearchOutputs(r *http.Request, id string, verified []assistantSearchVerified) error {
	if len(verified) == 0 {
		return nil
	}
	store := s.assistantRuns()
	_, err := store.transaction(r.Context(), func(ledger *assistantRunLedger) (bool, error) {
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
		if err = s.checkAssistantSearchProjectionLocked(r, run, verified); err != nil {
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
			host := assistantDialectOperation(*run, v.Held.HostOperationID)
			if host == nil {
				return false, errAssistantRunInvalid
			}
			var in assistantHostIntent
			if readAssistantHostReceipt(host.Intent, &in) != nil {
				return false, errAssistantRunInvalid
			}
			ref := assistantSearchReference{ID: v.Held.FinalID, Digest: v.Held.Digest, Request: v.Held.Request}
			step := assistantSearchStep{Query: ref.Request.Query, Provider: v.Result.Provider, Submitted: true, Reference: &ref}

			// A known refused host result (e.g. changed configuration) remains refused;
			// completed storage is retained separately and grants no replacement result.
			if host.State == "refused" && !v.Held.DispatchClosed {
				continue
			}
			if host.State != "refused" || !v.Held.DispatchClosed {
				continue
			}
			// Retain the original unknown operation result unchanged.
			run.DomainOutputs = append(run.DomainOutputs, assistantDomainOutput{Kind: "search", RunID: run.ID, RequestKey: run.Binding.ExecutionKey, OperationID: host.ID, CallID: in.CallID, Search: &assistantSearchOutput{Recovered: true, Reference: ref, Stored: v.Stored, AcquisitionID: host.ID, Step: step}})
			changed = true
		}
		if changed {
			run.Revision++
		}
		return changed, nil
	})
	return err
}

func (s *Server) assistantSearchProjectionSourceLocked(r *http.Request, run assistantRunRecord) (assistantRunSource, error) {
	identity, _, err := s.assistantRunIdentity(r)
	if err != nil {
		return assistantRunSource{}, err
	}
	source, err := s.resolveAssistantRun(r, assistantRunStart{AdmissionID: run.AdmissionID, Purpose: run.Binding.Owner.Kind, OwnerID: run.Binding.Owner.ID, Revision: run.Binding.Owner.Revision, RequestID: run.Binding.RequestID}, identity, run.Binding.Project)
	if err != nil {
		return source, err
	}
	source.Binding.ExecutionKey, source.Binding.Unit = run.Binding.ExecutionKey, run.Binding.Unit
	if !reflect.DeepEqual(source.Binding, run.Binding) {
		return assistantRunSource{}, errAssistantRunScope
	}
	return source, nil
}
