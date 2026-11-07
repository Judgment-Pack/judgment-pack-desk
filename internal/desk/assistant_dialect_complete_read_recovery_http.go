package desk

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strconv"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/jsonscalar"
)

const assistantDialectReadRecoveryPublicLimit = 1 << 20

// Lossless decimal revisions belong to this separate display resource, not the
// canonical snapshot/cursor. Public rows contain no storage selectors or grants.
type assistantDialectReadRecoveryView struct {
	RunID            string                               `json:"runId"`
	RunRevision      string                               `json:"runRevision"`
	RecoveryRevision string                               `json:"recoveryRevision"`
	Reads            []assistantDialectReadRecoveryPublic `json:"reads"`
}

type assistantDialectReadRecoveryPublic struct {
	OperationID     string `json:"operationId"`
	OriginalOutcome string `json:"originalOutcome"`
	BackingState    string `json:"backingState"`
	Reason          string `json:"reason,omitempty"`
	Ordinal         string `json:"ordinal"`
	StorageState    string `json:"storageState"`
	CanInspect      bool   `json:"canInspect"`
}

type assistantDialectReadRecoveryRequest struct {
	RunRevision      string `json:"runRevision"`
	RecoveryRevision string `json:"recoveryRevision"`
	RequestID        string `json:"requestId"`
}

func assistantDialectReadRecoveryDecimal(value string, zero bool) (uint64, error) {
	n, err := strconv.ParseUint(value, 10, 64)
	if err != nil || strconv.FormatUint(n, 10) != value || !zero && n == 0 {
		return 0, errAssistantRunInvalid
	}
	return n, nil
}

func validateAssistantDialectReadRecoveryPublic(value assistantDialectReadRecoveryView, expectedRun string) error {
	if value.RunID != expectedRun || !assistantDialectReadRecoveryRunID.MatchString(value.RunID) || value.Reads == nil || len(value.Reads) > assistantRunOperationLimit {
		return errAssistantRunInvalid
	}
	if _, err := assistantDialectReadRecoveryDecimal(value.RunRevision, false); err != nil {
		return err
	}
	if _, err := assistantDialectReadRecoveryDecimal(value.RecoveryRevision, true); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, row := range value.Reads {
		if !assistantRunText(row.OperationID, 128) || seen[row.OperationID] || !contains([]string{"completed", "failed", "interrupted", "unknown"}, row.OriginalOutcome) || !contains([]string{"not-observed", "present-valid", "absent", "corrupt", "unavailable"}, row.BackingState) || !contains([]string{"unresolved", "accounted-retained", "accounted-absent"}, row.StorageState) {
			return errAssistantRunInvalid
		}
		if _, err := assistantDialectReadRecoveryDecimal(row.Ordinal, true); err != nil {
			return err
		}
		if row.Reason != "" && !contains([]string{"owner-unavailable", "provider-unavailable", "ownership-mismatch", "storage-io", "close-uncertain", "cancelled", "budget-unresolved"}, row.Reason) {
			return errAssistantRunInvalid
		}
		if row.BackingState == "unavailable" && row.Reason == "" || row.BackingState == "not-observed" && row.Ordinal != "0" || row.BackingState != "not-observed" && row.Ordinal == "0" || row.BackingState != "not-observed" && row.BackingState != "unavailable" && row.Reason != "" || value.RecoveryRevision == "0" && row.Ordinal != "0" {
			return errAssistantRunInvalid
		}
		seen[row.OperationID] = true
	}
	return nil
}

// The cross-language vectors exercise this closed display schema without I/O.
func decodeAssistantDialectReadRecoveryPublic(raw []byte, expectedRun string) (assistantDialectReadRecoveryView, error) {
	var value assistantDialectReadRecoveryView
	if len(raw) > assistantDialectReadRecoveryPublicLimit || !validUTF8(raw) || jsonscalar.ValidateEscapes(raw) != nil || !uniqueJSON(raw) || readAssistantDialectFactoryShape(raw, reflect.TypeOf(value), false, 1) != nil || json.Unmarshal(raw, &value) != nil || validateAssistantDialectReadRecoveryPublic(value, expectedRun) != nil {
		return value, errAssistantRunInvalid
	}
	var wire struct {
		Reads []map[string]json.RawMessage `json:"reads"`
	}
	if json.Unmarshal(raw, &wire) != nil {
		return value, errAssistantRunInvalid
	}
	for _, row := range wire.Reads {
		if reason, ok := row["reason"]; ok {
			var code string
			if json.Unmarshal(reason, &code) != nil || code == "" {
				return value, errAssistantRunInvalid
			}
		}
	}
	return value, nil
}

func assistantDialectReadRecoveryPublicView(run assistantRunRecord, value assistantDialectReadRecovery, available bool, blocked ...string) (assistantDialectReadRecoveryView, error) {
	out := assistantDialectReadRecoveryView{RunID: run.ID, RunRevision: strconv.FormatUint(run.Revision, 10), RecoveryRevision: strconv.FormatUint(value.Revision, 10), Reads: []assistantDialectReadRecoveryPublic{}}
	for _, op := range run.Operations {
		if _, _, err := assistantDialectCompleteReadRoot(run, op.ID); err != nil {
			continue
		}
		row := assistantDialectReadRecoveryPublic{OperationID: op.ID, OriginalOutcome: "unknown", BackingState: "not-observed", Ordinal: "0", StorageState: "unresolved", CanInspect: available && assistantRunTerminal(run.State) && !run.Historical}
		switch op.State {
		case "completed":
			row.OriginalOutcome = "completed"
		case "refused":
			row.OriginalOutcome = "failed"
		case "uncertain":
			row.OriginalOutcome = "interrupted"
		}
		if saved := assistantDialectReadRecoveryOf(value, op.ID); saved != nil {
			row.BackingState, row.Reason, row.Ordinal, row.StorageState = saved.LatestObservation.State, saved.LatestObservation.Reason, strconv.FormatUint(saved.LatestObservation.Ordinal, 10), saved.Settlement.State
			// Imported observation bytes are retained history, not current local
			// raw ownership or accounting. R7 must select the admitted local import
			// owner before a later concrete observation can establish availability.
			if run.Historical {
				row.BackingState, row.Reason, row.StorageState = "unavailable", "owner-unavailable", "unresolved"
			}
		}
		if !available && row.BackingState == "not-observed" {
			row.Reason = "budget-unresolved"
			if len(blocked) > 0 {
				row.Reason = blocked[0]
			}
		}
		out.Reads = append(out.Reads, row)
	}
	raw, err := json.Marshal(out)
	if err != nil || validateAssistantDialectReadRecoveryPublic(out, run.ID) != nil || len(raw) > assistantDialectReadRecoveryPublicLimit {
		return out, errAssistantRunInvalid
	}
	return out, nil
}

// Saved status read only. No original executor acquisition, raw observation,
// source/provider I/O or lazy sidecar creation occurs on GET.
func (s *Server) readAssistantDialectReadRecoveryView(r *http.Request, id string) (_ assistantDialectReadRecoveryView, err error) {
	if err = s.authorizeAssistantRunStorage(r); err != nil {
		return assistantDialectReadRecoveryView{}, err
	}
	identity, _, err := s.assistantRunIdentity(r)
	if err != nil {
		return assistantDialectReadRecoveryView{}, err
	}
	lock, err := s.privateDataLockContext(r.Context(), true)
	if err != nil {
		return assistantDialectReadRecoveryView{}, err
	}
	defer func() { err = errors.Join(err, lock.Close()) }()
	data, err := s.openChatData()
	if err != nil {
		return assistantDialectReadRecoveryView{}, err
	}
	defer func() {
		err = errors.Join(err, closeDialectArtifactOwner(r.Context(), "read-recovery-status-data", data.root.Close))
	}()
	store := s.assistantRunsLocked(data.root)
	ledger, err := store.readLocked(r.Context(), data)
	if err != nil {
		return assistantDialectReadRecoveryView{}, err
	}
	var selected *assistantRunRecord
	for i := range ledger.Runs {
		if ledger.Runs[i].ID == id {
			selected = &ledger.Runs[i]
			break
		}
	}
	if selected == nil || selected.Binding.Principal != identity.Principal || selected.Binding.Project != store.project {
		return assistantDialectReadRecoveryView{}, errAssistantRunNotFound
	}
	if !assistantRunIdentityMatches(identity, selected.Binding) && !assistantRunTerminal(selected.State) && !assistantRunPaused(selected.State) {
		return assistantDialectReadRecoveryView{}, errAssistantRunScope
	}
	value, _, err := readAssistantDialectReadRecoveryLocked(r.Context(), data.root, *selected)
	if err != nil {
		return assistantDialectReadRecoveryView{}, err
	}
	h := s.assistantRunService()
	available := false
	blocked := "owner-unavailable"
	if h.mu.TryLock() {
		available = !h.closed && h.dialectReadRecoveryWriter != nil && h.active[id] == nil && s.retainedArtifacts == nil
		if h.dialectReadRecoveryWriter == nil {
			blocked = "budget-unresolved"
		} else if s.retainedArtifacts != nil {
			blocked = "provider-unavailable"
		}
		h.mu.Unlock()
	}
	if err = checkPrivateDataPath(data); err != nil {
		return assistantDialectReadRecoveryView{}, err
	}
	current, _, identityErr := s.assistantRunIdentity(r)
	if identityErr != nil || !assistantRunIdentityMatches(identity, current) {
		return assistantDialectReadRecoveryView{}, errors.Join(errAssistantRunScope, identityErr)
	}
	if err = s.authorizeAssistantRunStorage(r); err != nil {
		return assistantDialectReadRecoveryView{}, err
	}
	return assistantDialectReadRecoveryPublicView(*selected, value, available, blocked)
}

func (s *Server) handleAssistantDialectReadRecoveryStatus(w http.ResponseWriter, r *http.Request) {
	if !s.guardAssistantRun(w, r) {
		return
	}
	view, err := s.readAssistantDialectReadRecoveryView(r, r.PathValue("run"))
	if err != nil {
		assistantRunHTTPError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) observeAssistantDialectReadRecovery(r *http.Request, id, operation string, request assistantDialectReadRecoveryRequest) (_ assistantDialectReadRecoveryView, err error) {
	ctx := r.Context()
	if s.recoveryStorage == nil {
		return assistantDialectReadRecoveryView{}, errAssistantDialectReadRecoveryBudget
	}
	var prepared *assistantDialectReadRecoveryStorageOperation
	if s.recoveryStorage != nil {
		prepared, err = s.recoveryStorage.prepareOriginalDialectReadRecovery(ctx)
		if err != nil {
			return assistantDialectReadRecoveryView{}, errors.Join(errAssistantDialectReadRecoveryBudget, err)
		}
		defer func() { err = errors.Join(err, prepared.Close()) }()
		ctx = prepared.ctx
		r = r.WithContext(ctx)
	}
	if err = s.authorizeAssistantRunStorage(r); err != nil {
		return assistantDialectReadRecoveryView{}, err
	}
	expectedRun, err := assistantDialectReadRecoveryDecimal(request.RunRevision, false)
	if err != nil {
		return assistantDialectReadRecoveryView{}, err
	}
	expectedRecovery, err := assistantDialectReadRecoveryDecimal(request.RecoveryRevision, true)
	if err != nil || !assistantRunText(request.RequestID, 128) || !assistantRunText(operation, 128) {
		return assistantDialectReadRecoveryView{}, errAssistantRunInvalid
	}
	identity, _, err := s.assistantRunIdentity(r)
	if err != nil {
		return assistantDialectReadRecoveryView{}, err
	}
	run, err := s.assistantRunsContext(ctx).lookup(ctx, identity, id, "", true)
	if err != nil {
		return assistantDialectReadRecoveryView{}, err
	}
	if run.Revision != expectedRun {
		return assistantDialectReadRecoveryView{}, errAssistantRunConflict
	}
	if _, _, err = assistantDialectCompleteReadRoot(run, operation); err != nil {
		return assistantDialectReadRecoveryView{}, err
	}
	pin, err := s.assistantRunService().pinDialectReadRecoveryPrepared(ctx, run, prepared)
	if err != nil {
		return assistantDialectReadRecoveryView{}, err
	}
	defer func() { err = errors.Join(err, pin.Close()) }()
	g, err := pin.acquire(r)
	if err != nil {
		return assistantDialectReadRecoveryView{}, err
	}
	defer func() { err = errors.Join(err, g.Close()) }()
	prior, priorRaw, err := readAssistantDialectReadRecoveryLocked(ctx, g.data.root, g.run)
	if err != nil {
		return assistantDialectReadRecoveryView{}, err
	}
	if saved := assistantDialectReadRecoveryOf(prior, operation); saved != nil && saved.LatestObservation.RequestID == request.RequestID {
		if err = g.Check(ctx); err != nil {
			return assistantDialectReadRecoveryView{}, err
		}
		writer := pin.host.dialectReadRecoveryWriter
		if writer == nil {
			return assistantDialectReadRecoveryView{}, errAssistantDialectReadRecoveryBudget
		}
		if err = writer.recognizeOriginalDialectReadRecovery(ctx, g, prior, priorRaw, *saved); err != nil {
			return assistantDialectReadRecoveryView{}, err
		}
		return assistantDialectReadRecoveryPublicView(run, prior, true)
	}
	if prior.Revision != expectedRecovery {
		return assistantDialectReadRecoveryView{}, errAssistantRunConflict
	}
	writer := pin.host.dialectReadRecoveryWriter
	if writer == nil {
		return assistantDialectReadRecoveryView{}, errAssistantDialectReadRecoveryBudget
	}
	row, err := newAssistantDialectReadRecoveryRecord(run, operation, request.RequestID)
	if err != nil {
		return assistantDialectReadRecoveryView{}, err
	}
	if old := assistantDialectReadRecoveryOf(prior, operation); old != nil {
		row = *old
		if row.LatestObservation.Ordinal == ^uint64(0) {
			return assistantDialectReadRecoveryView{}, errAssistantRunInvalid
		}
		row.LatestObservation.Ordinal++
		row.LatestObservation.RequestID = request.RequestID
		row.LatestObservation.BaselineRunRevision, row.LatestObservation.BaselineRunSHA256 = run.Revision, digestOf(assistantChatJSON(run))
	}
	state, reason, provenance, observedErr := g.observeNative(ctx, operation)
	if ctx.Err() != nil {
		return assistantDialectReadRecoveryView{}, ctx.Err()
	}
	// Only bounded typed outcome is retained. Caught errors never enter JSON.
	row.LatestObservation.State, row.LatestObservation.Reason, row.LatestObservation.SourceProvenanceSHA256 = state, reason, provenance
	if observedErr != nil && state != "unavailable" {
		return assistantDialectReadRecoveryView{}, observedErr
	}
	intent, err := prepareAssistantDialectReadRecoveryWrite(prior, priorRaw, run, row)
	if err != nil {
		return assistantDialectReadRecoveryView{}, err
	}
	if prepared == nil {
		return assistantDialectReadRecoveryView{}, errAssistantDialectReadRecoveryBudget
	}
	if err = prepared.transferPrivate(ctx, g, intent, priorRaw, row); err != nil {
		return assistantDialectReadRecoveryView{}, err
	}
	if err = g.Close(); err != nil {
		return assistantDialectReadRecoveryView{}, err
	}
	// Actual B2 reservation retains the transferred original private turn but
	// runs outside host mutex custody. Q remains a leaf; no callback under Q.
	admission, err := writer.reserveOriginalDialectReadRecovery(ctx, pin, intent)
	if err != nil {
		return assistantDialectReadRecoveryView{}, err
	}
	if admission == nil {
		return assistantDialectReadRecoveryView{}, errAssistantDialectReadRecoveryBudget
	}
	defer func() { err = errors.Join(err, admission.Close()) }()
	g, err = pin.acquire(r)
	if err != nil {
		return assistantDialectReadRecoveryView{}, err
	}
	defer func() { err = errors.Join(err, g.Close()) }()
	saved, err := g.commit(ctx, prior, priorRaw, row, intent, admission)
	if err != nil {
		return assistantDialectReadRecoveryView{}, err
	}
	if err = errors.Join(g.Check(ctx), g.Close()); err != nil {
		return assistantDialectReadRecoveryView{}, err
	}
	if err = admission.completeOriginalDialectReadRecovery(ctx, g); err != nil {
		return assistantDialectReadRecoveryView{}, err
	}
	return assistantDialectReadRecoveryPublicView(run, saved, true)
}

func (s *Server) handleAssistantDialectReadRecoveryObserve(w http.ResponseWriter, r *http.Request) {
	if !s.guardAssistantRun(w, r) {
		return
	}
	var request assistantDialectReadRecoveryRequest
	if assistantRunRequest(r, &request) != nil {
		assistantRunHTTPError(w, errAssistantRunInvalid)
		return
	}
	view, err := s.observeAssistantDialectReadRecovery(r, r.PathValue("run"), r.PathValue("operation"), request)
	if errors.Is(err, errAssistantDialectReadRecoveryBudget) {
		writeJSONCoded(w, http.StatusServiceUnavailable, CodeBadRequest, "Retained read observation needs storage recovery admission. Saved work is unchanged.")
		return
	}
	if err != nil {
		assistantRunHTTPError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}
