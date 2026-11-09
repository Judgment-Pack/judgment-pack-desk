package desk

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"
)

// These records contain only finite identities and exact byte receipts. Raw
// response, seal, original and proof bodies belong exclusively to attachments.
type assistantSearchStorage struct {
	Intent  *attachmentReceipt `json:"intent,omitempty"`
	Receipt *attachmentReceipt `json:"receipt,omitempty"`
}
type assistantSearchCustody struct {
	ConfigurationDigest string                 `json:"configurationDigest"`
	DispatchClosed      bool                   `json:"dispatchClosed,omitempty"`
	HostOperationID     string                 `json:"hostOperationId"`
	TurnID              string                 `json:"turnId"`
	RequestDigest       string                 `json:"requestDigest"`
	Request             assistantSearchRequest `json:"request"`
	Gateway             assistantSearchGateway `json:"gateway"`
	Provider            string                 `json:"provider"`
	Session             string                 `json:"session"`
	StageID             string                 `json:"stageId"`
	SealID              string                 `json:"sealId"`
	FinalID             string                 `json:"finalId"`
	Acquire             string                 `json:"acquire"`
	Seal                string                 `json:"seal"`
	Stage               assistantSearchStorage `json:"stage"`
	SealStage           assistantSearchStorage `json:"sealStage"`
	Original            assistantSearchStorage `json:"original"`
	Final               assistantSearchStorage `json:"final"`
	Digest              string                 `json:"digest,omitempty"`
}
type assistantSearchGateway struct {
	URL                string `json:"url"`
	Authority          string `json:"authority"`
	Public             string `json:"public"`
	ManagedLocal       bool   `json:"managedLocal"`
	DocumentProcessing bool   `json:"documentProcessing"`
	MaxRequestBytes    int64  `json:"maxRequestBytes"`
	MaxFileBytes       int64  `json:"maxFileBytes"`
}

func assistantSearchGatewaySnapshot(g researchGateway) assistantSearchGateway {
	return assistantSearchGateway{URL: g.url, Authority: g.authority, Public: g.signerPublic, ManagedLocal: g.managedLocal, DocumentProcessing: g.documentProcessing, MaxRequestBytes: g.maxRequestBytes, MaxFileBytes: g.maxFileBytes}
}
func assistantSearchGatewayOf(c assistantSearchConfiguration) assistantSearchGateway {
	return assistantSearchGatewaySnapshot(c.Gateway)
}
func assistantSearchConfigurationDigest(c assistantSearchConfiguration) string {
	return digestOf(assistantChatJSON(struct {
		Gateway                                            assistantSearchGateway
		Connection, Revision, Provider, Preference, Status string
	}{assistantSearchGatewayOf(c), c.Connection, c.Revision, c.Provider, c.PreferenceDigest, c.StatusDigest}))
}

func assistantSearchUUID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = (raw[6] & 15) | 64
	raw[8] = (raw[8] & 63) | 128
	encoded := hex.EncodeToString(raw[:])
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}
func (e *assistantRunExecution) reserveAssistantSearch(ctx context.Context, host string, request assistantSearchRequest, c assistantSearchConfiguration) (assistantSearchCustody, error) {
	var held assistantSearchCustody
	ids := make([]string, 4)
	for i := range ids {
		var err error
		ids[i], err = assistantSearchUUID()
		if err != nil {
			return held, err
		}
	}
	held = assistantSearchCustody{ConfigurationDigest: assistantSearchConfigurationDigest(c), HostOperationID: host, TurnID: e.record.Binding.RequestID, RequestDigest: e.record.Binding.RequestDigest, Request: request, Gateway: assistantSearchGatewayOf(c), Provider: c.Provider, Session: "chat-search-" + ids[0], StageID: ids[1], SealID: ids[2], FinalID: ids[3], Acquire: "reserved", Seal: "not-started"}
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
		hostOp := assistantDialectOperation(*run, host)
		if hostOp == nil || hostOp.Kind != "host" || hostOp.Name != assistantSearchTool || hostOp.State != "dispatched" {
			return false, errAssistantRunScope
		}
		attempts, retained := 0, map[string]bool{}
		for _, other := range ledger.Runs {
			if other.Binding.Owner.Kind != run.Binding.Owner.Kind || other.Binding.Owner.ID != run.Binding.Owner.ID {
				continue
			}
			for _, search := range other.Searches {
				if other.ID == run.ID && search.HostOperationID == host {
					return false, errAssistantRunDispatch
				}
				// Stable saved user-turn identity deliberately excludes run/session nonce,
				// chat revision and mutable choice bytes. A later choice continuation must
				// preserve TurnID before it can be activated.
				if search.TurnID == run.Binding.RequestID {
					if search.DispatchClosed {
						return false, assistantSearchFailure("search-failed", c.Provider)
					}
					attempts++
				}
				retained[search.FinalID] = true
			}
		}
		var chat struct {
			Searches []assistantSearchReference `json:"searches"`
		}
		if json.Unmarshal(e.source.Original, &chat) != nil {
			return false, errAssistantRunInvalid
		}
		for _, ref := range chat.Searches {
			retained[ref.ID] = true
		}
		if attempts >= 3 {
			return false, assistantSearchFailure("search-turn-limit", c.Provider)
		}
		if len(retained) >= 64 {
			return false, assistantSearchFailure("search-chat-limit", c.Provider)
		}
		run.Searches = append(run.Searches, held)
		run.Revision++
		return true, nil
	})
	if err != nil {
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
func (e *assistantRunExecution) changeAssistantSearch(ctx context.Context, host string, retention bool, change func(*assistantSearchCustody) error) error {
	if retention {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
	}
	err := e.writer.change(ctx, retention, func(run *assistantRunRecord) (bool, error) {
		for i := range run.Searches {
			if run.Searches[i].HostOperationID == host {
				before := assistantChatJSON(run.Searches[i])
				if err := change(&run.Searches[i]); err != nil {
					return false, err
				}
				return !bytes.Equal(before, assistantChatJSON(run.Searches[i])), nil
			}
		}
		return false, errAssistantRunInvalid
	})
	e.changed(err)
	return err
}
func validAssistantSearchStorage(value assistantSearchStorage, id string) bool {
	if value.Receipt != nil && (value.Intent == nil || *value.Receipt != *value.Intent) {
		return false
	}
	return value.Intent == nil || value.Intent.ID == id && assistantRunDigest(value.Intent.SHA256)
}
func validateAssistantSearchCustody(run assistantRunRecord) error {
	if len(run.Searches) > 64 {
		return errAssistantRunInvalid
	}
	ids, hosts := map[string]bool{}, map[string]bool{}
	for _, held := range run.Searches {
		host := assistantDialectOperation(run, held.HostOperationID)
		var intent assistantHostIntent
		query, err := "", error(nil)
		if host != nil {
			_ = readAssistantHostReceipt(host.Intent, &intent)
			query, err = readAssistantSearchQuery(intent.Arguments)
		}
		if !assistantRunDigest(held.ConfigurationDigest) || host == nil || host.Kind != "host" || host.Name != assistantSearchTool || err != nil || query != held.Request.Query || hosts[held.HostOperationID] || held.TurnID != run.Binding.RequestID || held.RequestDigest != run.Binding.RequestDigest || !validAssistantSearchRequest(held.Request) || !researchSourceName.MatchString(held.Provider) || held.Gateway.URL == "" || held.Gateway.Authority == "" || !researchConnectionRevision.MatchString(held.Gateway.Public) || !assistantRunText(held.Session, 100) {
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

func (e *assistantRunExecution) retainAssistantSearchObject(ctx context.Context, held assistantSearchCustody, kind string, raw []byte, c assistantSearchConfiguration, completedEffect bool) (attachmentReceipt, error) {
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
	selectStorage := func(v *assistantSearchCustody) *assistantSearchStorage {
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
	if err := e.changeAssistantSearch(ctx, held.HostOperationID, completedEffect, func(v *assistantSearchCustody) error {
		slot := selectStorage(v)
		if slot.Intent != nil {
			return errAssistantRunDispatch
		}
		slot.Intent = &expected
		return nil
	}); err != nil {
		return attachmentReceipt{}, err
	}
	check := func() error { return e.recheckAssistantSearchLocked(ctx, c) }
	if completedEffect {
		// Completed acquisition/seal custody may finish despite Stop. This precise
		// already-declared packet write grants no subsequent external or final IO.
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		check = func() error {
			return e.recheckAssistantSearchRetentionLocked(ctx, held.HostOperationID, kind, expected)
		}
	}
	receipt, err := e.service.server.attachmentStorage().write(ctx, attachmentWriteRequest{id: id, ifMatch: match, data: raw, checkCurrent: check})
	if err != nil {
		return attachmentReceipt{}, err
	}
	if receipt != expected {
		return attachmentReceipt{}, errAssistantRunConflict
	}
	err = e.changeAssistantSearch(ctx, held.HostOperationID, true, func(v *assistantSearchCustody) error {
		slot := selectStorage(v)
		if slot.Intent == nil || *slot.Intent != receipt {
			return errAssistantRunConflict
		}
		slot.Receipt = &receipt
		return nil
	})
	return receipt, err
}

func assistantSearchCustodyOf(run assistantRunRecord, host string) (assistantSearchCustody, error) {
	for _, held := range run.Searches {
		if held.HostOperationID == host {
			return held, nil
		}
	}
	return assistantSearchCustody{}, errAssistantRunInvalid
}
func (e *assistantRunExecution) currentAssistantSearch(ctx context.Context, host string) (assistantSearchCustody, error) {
	run, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
	if err != nil {
		return assistantSearchCustody{}, err
	}
	return assistantSearchCustodyOf(run, host)
}

// Reconciliation is deliberately observation-only. It may attach a receipt to
// an already-declared exact storage intent; it never reacquires, reseals or writes
// an attachment. Missing or mismatched objects remain retained uncertainty.
func reconcileAssistantSearchStorage(held *assistantSearchCustody, read func(string) ([]byte, error)) (bool, error) {
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
			return false, errAssistantSearch
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
func (e *assistantRunExecution) recheckAssistantSearchRetentionLocked(ctx context.Context, host, kind string, expected attachmentReceipt) error {
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
	held, err := assistantSearchCustodyOf(*run, host)
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
