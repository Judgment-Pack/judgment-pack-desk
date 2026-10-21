package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"
)

// These records contain only finite identities and exact byte receipts. Raw
// response, seal, original and proof bodies belong exclusively to attachments.
type assistantLinkCustody struct {
	Discovery                 bool                   `json:"discovery,omitempty"`
	SeedAuthorityPacket       *attachmentReceipt     `json:"seedAuthorityPacket,omitempty"`
	AuthorityPacket           *attachmentReceipt     `json:"authorityPacket,omitempty"`
	SearchConfigurationDigest string                 `json:"searchConfigurationDigest,omitempty"`
	SearchPreferenceDigest    string                 `json:"searchPreferenceDigest,omitempty"`
	ConfigurationDigest       string                 `json:"configurationDigest"`
	DispatchClosed            bool                   `json:"dispatchClosed,omitempty"`
	HostOperationID           string                 `json:"hostOperationId"`
	TurnID                    string                 `json:"turnId"`
	RequestDigest             string                 `json:"requestDigest"`
	Request                   assistantLinkRequest   `json:"request"`
	Gateway                   assistantSearchGateway `json:"gateway"`
	Session                   string                 `json:"session"`
	StageID                   string                 `json:"stageId"`
	SealID                    string                 `json:"sealId"`
	FinalID                   string                 `json:"finalId"`
	Acquire                   string                 `json:"acquire"`
	Seal                      string                 `json:"seal"`
	Stage                     assistantSearchStorage `json:"stage"`
	SealStage                 assistantSearchStorage `json:"sealStage"`
	Original                  assistantSearchStorage `json:"original"`
	Final                     assistantSearchStorage `json:"final"`
	Digest                    string                 `json:"digest,omitempty"`
}

func assistantLinkConfigurationDigest(c assistantLinkConfiguration) string {
	return digestOf(assistantChatJSON(c))
}

func (e *assistantRunExecution) reserveAssistantLink(ctx context.Context, host string, request assistantLinkRequest, c assistantLinkConfiguration) (assistantLinkCustody, error) {
	return e.reserveAssistantMaterial(ctx, host, request, c, false)
}
func (e *assistantRunExecution) reserveAssistantMaterial(ctx context.Context, host string, request assistantLinkRequest, c assistantLinkConfiguration, discovery bool) (assistantLinkCustody, error) {
	var held assistantLinkCustody
	ids := make([]string, 4)
	for i := range ids {
		var err error
		ids[i], err = assistantSearchUUID()
		if err != nil {
			return held, err
		}
	}
	held = assistantLinkCustody{Discovery: discovery, SeedAuthorityPacket: c.SeedAuthorityPacket, AuthorityPacket: c.AuthorityPacket, SearchConfigurationDigest: c.SearchConfigurationDigest, SearchPreferenceDigest: c.SearchPreferenceDigest, ConfigurationDigest: assistantLinkConfigurationDigest(c), HostOperationID: host, TurnID: e.record.Binding.RequestID, RequestDigest: e.record.Binding.RequestDigest, Request: request, Gateway: assistantSearchGatewaySnapshot(c.Gateway), Session: "chat-link-" + ids[0], StageID: ids[1], SealID: ids[2], FinalID: ids[3], Acquire: "reserved", Seal: "not-started"}
	// Writer revision, live lease, global reservation count and append share one
	// private ledger transaction. Root/children/retries cannot race this budget.
	w := e.writer
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failure != nil {
		return held, w.failure
	}
	w.lease.mu.Lock()
	defer w.lease.mu.Unlock()
	if w.lease.file == nil || w.lease.recoveryErr != nil {
		return held, w.fail(errAssistantRunScope)
	}
	ledger, err := w.lease.store.transaction(ctx, func(ledger *assistantRunLedger) (bool, error) {
		run, err := assistantRunBound(ledger, w.binding, w.id)
		if err != nil {
			return false, err
		}
		if run.Revision != w.revision || assistantRunTerminal(run.State) {
			return false, errAssistantRunConflict
		}
		tool := assistantLinkTool
		if discovery {
			tool = assistantWebsiteTool
		}
		hostOp := assistantDialectOperation(*run, host)
		if hostOp == nil || hostOp.Kind != "host" || hostOp.Name != tool || hostOp.State != "dispatched" {
			return false, errAssistantRunScope
		}
		attempts, retained := 0, map[string]bool{}
		seeds := map[string]bool{}
		explorations := 0
		for _, other := range ledger.Runs {
			if other.Binding.Owner.Kind != run.Binding.Owner.Kind || other.Binding.Owner.ID != run.Binding.Owner.ID {
				continue
			}
			for _, website := range other.Websites {
				seeds[website.Request.URL] = true
				if website.TurnID == run.Binding.RequestID {
					explorations++
				}
			}
			for _, search := range other.Links {
				if other.ID == run.ID && search.HostOperationID == host {
					return false, errAssistantRunDispatch
				}
				// Stable saved user-turn identity deliberately excludes run/session nonce,
				// chat revision and mutable choice bytes. A later choice continuation must
				// preserve TurnID before it can be activated.
				if search.TurnID == run.Binding.RequestID {
					if search.DispatchClosed || assistantLinkPending(search) {
						return false, errAssistantRunDispatch
					}
					attempts++
				}
				retained[search.FinalID] = true
			}
		}
		var chat struct {
			Documents []assistantLinkAttachment `json:"documents"`
		}
		if json.Unmarshal(e.source.Original, &chat) != nil {
			return false, errAssistantRunInvalid
		}
		for _, ref := range assistantSavedLinkAttachments(e.source.Original) {
			retained[ref.ID] = true
		}
		if discovery {
			for _, ref := range assistantSavedWebsites(e.source.Original) {
				if !validAssistantWebsiteReference(ref) {
					return false, errAssistantRunInvalid
				}
				seeds[ref.Seed] = true
			}
			if explorations >= 1 {
				return false, &assistantLinkLimitError{"One website exploration per saved message; use its map or send another message."}
			}
			if !seeds[request.URL] && len(seeds) >= 16 {
				return false, &assistantLinkLimitError{"This chat holds sixteen distinct website seeds. Start another chat."}
			}
			run.Websites = append(run.Websites, assistantWebsiteCustody(held))
			run.Revision++
			return true, nil
		}
		if attempts >= 3 {
			if request.Site == "" || attempts >= 10 {
				return false, &assistantLinkLimitError{"The shared new-link budget is spent (ordinary three, verified discovery ten total). Use retained pages or send another message."}
			}
		}
		if len(retained) >= 256 {
			return false, &assistantLinkLimitError{"This chat holds 256 documents. Start a new chat to read more links."}
		}
		run.Links = append(run.Links, held)
		run.Revision++
		return true, nil
	})
	if err != nil {
		var limit *assistantLinkLimitError
		if errors.As(err, &limit) || errors.Is(err, errAssistantRunDispatch) {
			return held, err
		}
		return held, w.fail(err)
	}
	for _, run := range ledger.Runs {
		if run.ID == w.id {
			w.revision = run.Revision
			return held, nil
		}
	}
	return held, w.fail(errAssistantRunInvalid)
}
func (e *assistantRunExecution) changeAssistantLink(ctx context.Context, host string, retention bool, change func(*assistantLinkCustody) error) error {
	if retention {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
	}
	err := e.writer.change(ctx, retention, func(run *assistantRunRecord) (bool, error) {
		for i := range run.Links {
			if run.Links[i].HostOperationID == host {
				before := assistantChatJSON(run.Links[i])
				if err := change(&run.Links[i]); err != nil {
					return false, err
				}
				return !bytes.Equal(before, assistantChatJSON(run.Links[i])), nil
			}
		}
		for i := range run.Websites {
			if run.Websites[i].HostOperationID == host {
				held := assistantLinkCustody(run.Websites[i])
				before := assistantChatJSON(held)
				if err := change(&held); err != nil {
					return false, err
				}
				run.Websites[i] = assistantWebsiteCustody(held)
				return !bytes.Equal(before, assistantChatJSON(held)), nil
			}
		}
		return false, errAssistantRunInvalid
	})
	e.changed(err)
	return err
}
func validateAssistantLinkCustody(run assistantRunRecord) error {
	return validateAssistantMaterialCustody(run, run.Links, assistantLinkTool)
}
func validateAssistantMaterialCustody(run assistantRunRecord, materials []assistantLinkCustody, tool string) error {
	if len(materials) > 256 {
		return errAssistantRunInvalid
	}
	ids, hosts := map[string]bool{}, map[string]bool{}
	for _, held := range materials {
		if held.SeedAuthorityPacket != nil && (!attachmentID.MatchString(held.SeedAuthorityPacket.ID) || !assistantRunDigest(held.SeedAuthorityPacket.SHA256)) {
			return errAssistantRunInvalid
		}
		if held.AuthorityPacket != nil && (!attachmentID.MatchString(held.AuthorityPacket.ID) || !assistantRunDigest(held.AuthorityPacket.SHA256)) {
			return errAssistantRunInvalid
		}
		host := assistantDialectOperation(run, held.HostOperationID)
		var intent assistantHostIntent
		args, err := assistantLinkArguments{}, error(nil)
		if host != nil {
			_ = readAssistantHostReceipt(host.Intent, &intent)
			args, err = readAssistantLinkArguments(intent.Arguments)
		}
		if !assistantRunDigest(held.ConfigurationDigest) || host == nil || host.Kind != "host" || host.Name != tool || err != nil || args.Link.FetchURL != held.Request.URL || hosts[held.HostOperationID] || held.TurnID != run.Binding.RequestID || held.RequestDigest != run.Binding.RequestDigest || !adWebURL(held.Request.URL) || held.Gateway.URL == "" || held.Gateway.Authority == "" || !researchConnectionRevision.MatchString(held.Gateway.Public) || !assistantRunText(held.Session, 100) {
			return errAssistantRunInvalid
		}
		if held.Request.Site != "" && (tool != assistantLinkTool || !assistantSameOrigin(held.Request.URL, held.Request.Site) || held.AuthorityPacket == nil || held.SearchPreferenceDigest == "") {
			return errAssistantRunInvalid
		}
		if held.Discovery != (tool == assistantWebsiteTool) {
			return errAssistantRunInvalid
		}
		hosts[held.HostOperationID] = true
		for _, id := range []string{held.StageID, held.SealID, held.FinalID} {
			if !attachmentID.MatchString(id) || ids[id] {
				return errAssistantRunInvalid
			}
			ids[id] = true
		}
		if !contains([]string{"reserved", "dispatched", "completed", "refused"}, held.Acquire) || !contains([]string{"not-started", "dispatched", "completed", "refused"}, held.Seal) {
			return errAssistantRunInvalid
		}
		for _, pair := range []struct {
			Storage assistantSearchStorage
			ID      string
		}{{held.Stage, held.StageID}, {held.SealStage, held.SealID}, {held.Original, held.FinalID}, {held.Final, held.FinalID}} {
			if !validAssistantSearchStorage(pair.Storage, pair.ID) {
				return errAssistantRunInvalid
			}
		}
		if held.Stage.Intent != nil && held.Acquire != "completed" || held.Seal != "not-started" && held.Original.Receipt == nil || held.SealStage.Intent != nil && held.Seal != "completed" || held.Original.Intent != nil && held.Stage.Receipt == nil || held.Final.Intent != nil && (held.SealStage.Receipt == nil || held.Original.Receipt == nil || !adDigest(held.Digest)) || held.Digest != "" && !adDigest(held.Digest) {
			return errAssistantRunInvalid
		}
	}
	return nil
}

func (e *assistantRunExecution) retainAssistantLinkObject(ctx context.Context, held assistantLinkCustody, kind string, raw []byte, c assistantLinkConfiguration, completedEffect bool) (attachmentReceipt, error) {
	id := held.FinalID
	match := "absent"
	if kind == "stage" {
		id = held.StageID
	} else if kind == "seal" {
		id = held.SealID
	} else if kind == "final" {
		if held.Original.Receipt == nil {
			return attachmentReceipt{}, errAssistantRunScope
		}
		match = held.Original.Receipt.SHA256
	}
	expected := attachmentReceipt{ID: id, SHA256: digestOf(raw)}
	selectStorage := func(v *assistantLinkCustody) *assistantSearchStorage {
		switch kind {
		case "stage":
			return &v.Stage
		case "seal":
			return &v.SealStage
		case "original":
			return &v.Original
		default:
			return &v.Final
		}
	}
	if err := e.changeAssistantLink(ctx, held.HostOperationID, completedEffect, func(v *assistantLinkCustody) error {
		slot := selectStorage(v)
		if slot.Intent != nil {
			return errAssistantRunDispatch
		}
		slot.Intent = &expected
		return nil
	}); err != nil {
		return attachmentReceipt{}, err
	}
	check := func() error { return e.recheckAssistantLinkLocked(ctx, c) }
	if completedEffect {
		// Completed acquisition/seal custody may finish despite Stop. This precise
		// already-declared packet write grants no subsequent external or final IO.
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		check = func() error {
			return e.recheckAssistantLinkRetentionLocked(ctx, held.HostOperationID, kind, expected)
		}
	}
	receipt, err := e.service.server.attachmentStorage().write(ctx, attachmentWriteRequest{id: id, ifMatch: match, data: raw, checkCurrent: check})
	if err != nil {
		return attachmentReceipt{}, err
	}
	if receipt != expected {
		return attachmentReceipt{}, errAssistantRunConflict
	}
	err = e.changeAssistantLink(ctx, held.HostOperationID, true, func(v *assistantLinkCustody) error {
		slot := selectStorage(v)
		if slot.Intent == nil || *slot.Intent != receipt {
			return errAssistantRunConflict
		}
		slot.Receipt = &receipt
		return nil
	})
	return receipt, err
}

func assistantLinkCustodyOf(run assistantRunRecord, host string) (assistantLinkCustody, error) {
	for _, held := range run.Links {
		if held.HostOperationID == host {
			return held, nil
		}
	}
	for _, held := range run.Websites {
		if held.HostOperationID == host {
			return assistantLinkCustody(held), nil
		}
	}
	return assistantLinkCustody{}, errAssistantRunInvalid
}
func (e *assistantRunExecution) currentAssistantLink(ctx context.Context, host string) (assistantLinkCustody, error) {
	run, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
	if err != nil {
		return assistantLinkCustody{}, err
	}
	return assistantLinkCustodyOf(run, host)
}

// Reconciliation is deliberately observation-only. It may attach a receipt to
// an already-declared exact storage intent; it never reacquires, reseals or writes
// an attachment. Missing or mismatched objects remain retained uncertainty.
func reconcileAssistantLinkStorage(held *assistantLinkCustody, read func(string) ([]byte, error)) (bool, error) {
	changed := false
	for _, slot := range []*assistantSearchStorage{&held.Stage, &held.SealStage, &held.Original, &held.Final} {
		if slot.Intent == nil || slot.Receipt != nil {
			continue
		}
		raw, err := read(slot.Intent.ID)
		if err != nil {
			continue
		}
		// A final proof object supersedes the original. Its exact final intent takes
		// precedence; original receipts are never inferred from a different wrapper.
		if digestOf(raw) != slot.Intent.SHA256 {
			continue
		}
		var object attachmentObject
		if validateAttachmentObject(raw) != nil || json.Unmarshal(raw, &object) != nil {
			return false, errAssistantDocument
		}
		receipt := *slot.Intent
		slot.Receipt = &receipt
		changed = true
	}
	return changed, nil
}

// Completed-effect custody is scoped to the original ledger namespace even if
// chat Stop or a source change has closed further dispatch. It is never a nil
// admission callback and it authorizes only the exact predeclared packet digest.
func (e *assistantRunExecution) recheckAssistantLinkRetentionLocked(ctx context.Context, host, kind string, expected attachmentReceipt) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s := e.service.server
	store, err := s.openChatData()
	if err != nil {
		return err
	}
	defer store.root.Close()
	if err = e.writer.lease.store.checkScope(store.root); err != nil {
		return err
	}
	raw, err := readPrivateData(store.root, e.writer.lease.store.name, assistantRunRecordLimit)
	if err != nil {
		return err
	}
	ledger, err := decodeAssistantRunStorage(e.writer.lease.store.name, raw)
	if err != nil {
		return err
	}
	run, err := assistantRunBound(&ledger, e.record.Binding, e.record.ID)
	if err != nil {
		return err
	}
	held, err := assistantLinkCustodyOf(*run, host)
	if err != nil {
		return err
	}
	slot := held.Stage
	if kind == "seal" {
		slot = held.SealStage
	} else if kind != "stage" {
		return errAssistantRunScope
	}
	if slot.Intent == nil || *slot.Intent != expected || slot.Receipt != nil {
		return errAssistantRunScope
	}
	return nil
}

// A process can disappear before it journals an unknown outcome. The durable
// dispatched intent itself closes fresh acquisition; no reload needs to write
// a flag merely to preserve that boundary.
func assistantLinkPending(v assistantLinkCustody) bool {
	if v.Acquire == "dispatched" || v.Seal == "dispatched" {
		return true
	}
	for _, slot := range []assistantSearchStorage{v.Stage, v.SealStage, v.Original, v.Final} {
		if slot.Intent != nil && slot.Receipt == nil {
			return true
		}
	}
	return false
}

type assistantLinkLimitError struct{ message string }

func (e *assistantLinkLimitError) Error() string { return e.message }
