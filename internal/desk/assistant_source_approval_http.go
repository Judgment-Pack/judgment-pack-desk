package desk

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"time"
)

type assistantSourceApproveRequest struct {
	ReviewID       string `json:"reviewId"`
	ReviewRevision uint64 `json:"reviewRevision"`
}
type assistantSourceRevokeRequest struct {
	ApprovalID       string `json:"approvalId"`
	ApprovalRevision uint64 `json:"approvalRevision"`
}
type assistantSourceApprovalReference struct {
	ID         string `json:"id"`
	Revision   uint64 `json:"revision"`
	ApprovedAt string `json:"approvedAt"`
	RevokedAt  string `json:"revokedAt,omitempty"`
}
type assistantSourceReviewView struct {
	Snapshot       assistantSourceSnapshot           `json:"snapshot"`
	Status         string                            `json:"status"`
	ReviewID       string                            `json:"reviewId,omitempty"`
	ReviewRevision uint64                            `json:"reviewRevision,omitempty"`
	Approval       *assistantSourceApprovalReference `json:"approval,omitempty"`
}
type assistantSourceCatalogView struct {
	Version int                         `json:"version"`
	Sources []assistantSourceReviewView `json:"sources"`
}

func readAssistantSourceRequest(reader io.Reader, target any) error {
	raw, err := io.ReadAll(io.LimitReader(reader, 4097))
	if err != nil || len(raw) > 4096 || !validUTF8(raw) || !uniqueJSON(raw) || readAssistantDialectCustody(raw, reflect.TypeOf(target)) != nil || json.Unmarshal(raw, target) != nil {
		return errAssistantRunInvalid
	}
	switch request := target.(type) {
	case *assistantSourceApproveRequest:
		if !assistantSourceRecordID.MatchString(request.ReviewID) || request.ReviewRevision != 1 {
			return errAssistantRunInvalid
		}
	case *assistantSourceRevokeRequest:
		if !assistantSourceRecordID.MatchString(request.ApprovalID) || request.ApprovalRevision != 1 {
			return errAssistantRunInvalid
		}
	default:
		return errAssistantRunInvalid
	}
	return nil
}
func (s *Server) handleAssistantSourceApproval(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) || s.refuseUnusableStore(w) || !s.guardPrivateWorkspace(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	chat := r.PathValue("chat")
	if !researchChatID.MatchString(chat) || r.URL.RawQuery != "" {
		writeJSONCoded(w, 400, CodeBadRequest, "Invalid source review.")
		return
	}
	var approve assistantSourceApproveRequest
	var revoke assistantSourceRevokeRequest
	operation := "review"
	if r.Method == http.MethodPost {
		if strings.HasSuffix(r.URL.Path, "/approve") {
			operation = "approve"
			if readAssistantSourceRequest(r.Body, &approve) != nil {
				writeJSONCoded(w, 400, CodeBadRequest, "Invalid source review selector.")
				return
			}
		} else if strings.HasSuffix(r.URL.Path, "/revoke") {
			operation = "revoke"
			if readAssistantSourceRequest(r.Body, &revoke) != nil {
				writeJSONCoded(w, 400, CodeBadRequest, "Invalid source approval selector.")
				return
			}
		} else {
			writeJSONCoded(w, 400, CodeBadRequest, "Invalid source operation.")
			return
		}
	} else if r.Method != http.MethodGet {
		writeJSONCoded(w, 405, CodeBadRequest, "Invalid source operation.")
		return
	}
	view, err := s.assistantSourceApprovalTransaction(r, chat, operation, approve, revoke)
	if err != nil {
		status := 409
		if errors.Is(err, errAssistantRunScope) {
			status = 403
		}
		if codeOf(err) == CodeTooLarge {
			status = 413
		}
		writeJSONCoded(w, status, CodeBadRequest, "Source review is unavailable or changed. Refresh the complete catalog before reviewing again.")
		return
	}
	if operation == "revoke" {
		writeJSON(w, 200, map[string]any{"version": 1, "status": "revoked", "approval": view.Sources[0].Approval})
		return
	}
	writeJSON(w, 200, view)
}
func (s *Server) assistantSourceApprovalTransaction(r *http.Request, chat, operation string, approve assistantSourceApproveRequest, revoke assistantSourceRevokeRequest) (assistantSourceCatalogView, error) {
	empty := assistantSourceCatalogView{}
	// Serialize Desk chat/config writes while observations are taken. Companion
	// state is independently mutable: this approves exactly what was observed,
	// never promises atomic exclusion of future external connection revisions.
	s.writes.Lock()
	defer s.writes.Unlock()
	initial, err := s.privateDataLockContext(r.Context(), true)
	if err != nil {
		return empty, err
	}
	scope, source, err := s.assistantSourceScopeCurrent(r, chat)
	initial.Close()
	if err != nil {
		return empty, err
	}
	var catalog []assistantSourceSnapshot
	if operation != "revoke" {
		catalog, err = s.assistantSourceCatalog(r.Context(), source, scope.Project)
		if err != nil {
			return empty, err
		}
	}
	lock, err := s.privateDataLockContext(r.Context(), true)
	if err != nil {
		return empty, err
	}
	defer lock.Close()
	current, fresh, err := s.assistantSourceScopeCurrent(r, chat)
	if err != nil {
		return empty, err
	}
	if current != scope {
		return empty, errAssistantRunScope
	}
	if operation != "revoke" {
		if err = s.assistantSourceCatalogLocalCurrent(fresh, catalog); err != nil {
			return empty, err
		}
	}
	store, err := s.openChatData()
	if err != nil {
		return empty, err
	}
	defer store.root.Close()
	project, err := assistantRunNamespace(store.root, s.conversationIdentity())
	if err != nil || project != scope.Project {
		return empty, errAssistantRunScope
	}
	ledger, err := readAssistantSourceLedger(store.root, scope.Project)
	if err != nil {
		return empty, err
	}
	changed := false
	now := time.Now().UTC().Format(time.RFC3339Nano)
	switch operation {
	case "approve":
		var review *assistantSourceReview
		for i := range ledger.Reviews {
			if ledger.Reviews[i].ID == approve.ReviewID {
				review = &ledger.Reviews[i]
			}
		}
		if review == nil || review.Historical || review.Scope != scope || review.Revision != approve.ReviewRevision || review.Consumed {
			return empty, errAssistantRunConflict
		}
		var actual *assistantSourceSnapshot
		for i := range catalog {
			if catalog[i].AdapterID == review.Snapshot.AdapterID {
				actual = &catalog[i]
			}
		}
		if actual == nil || actual.Status != "review-required" || !assistantSourceSnapshotEqual(*actual, review.Snapshot) {
			return empty, errAssistantRunConflict
		}
		if len(ledger.Approvals) >= assistantRunCountLimit {
			return empty, withCode(CodeTooLarge, errAssistantRunInvalid)
		}
		id, err := assistantSourceID()
		if err != nil {
			return empty, err
		}
		// Explicit approval replaces this session's previous adapter approval;
		// older records remain closed historical evidence and cannot reactivate.
		for i := range ledger.Approvals {
			a := &ledger.Approvals[i]
			if !a.Historical && a.Scope == scope && a.Snapshot.AdapterID == actual.AdapterID && a.RevokedAt == "" {
				a.RevokedAt = now
				a.Revision++
			}
		}
		review.Consumed = true
		ledger.Approvals = append(ledger.Approvals, assistantSourceApproval{Version: 1, ID: id, Revision: 1, ReviewID: review.ID, ReviewRevision: review.Revision, Scope: scope, Snapshot: *actual, Reviewer: scope.Principal, ApprovedAt: now})
		changed = true
	case "revoke":
		found := false
		for i := range ledger.Approvals {
			a := &ledger.Approvals[i]
			if !a.Historical && a.ID == revoke.ApprovalID && a.Scope == scope && a.Revision == revoke.ApprovalRevision && a.RevokedAt == "" {
				a.RevokedAt = now
				a.Revision++
				found = true
			}
		}
		if !found {
			return empty, errAssistantRunConflict
		}
		changed = true
	case "review":
	default:
		return empty, errAssistantRunInvalid
	}
	view := assistantSourceCatalogView{Version: 1, Sources: make([]assistantSourceReviewView, 0, len(catalog))}
	for _, snapshot := range catalog {
		row := assistantSourceReviewView{Snapshot: snapshot, Status: snapshot.Status}
		approval := assistantSourceApprovalCurrent(ledger, scope, snapshot)
		if approval != nil {
			row.Status = "approved"
			row.Approval = &assistantSourceApprovalReference{ID: approval.ID, Revision: approval.Revision, ApprovedAt: approval.ApprovedAt}
		} else {
			for i := len(ledger.Approvals) - 1; i >= 0; i-- {
				a := ledger.Approvals[i]
				if !a.Historical && a.Scope == scope && a.Snapshot.AdapterID == snapshot.AdapterID {
					row.Approval = &assistantSourceApprovalReference{ID: a.ID, Revision: a.Revision, ApprovedAt: a.ApprovedAt, RevokedAt: a.RevokedAt}
					if snapshot.Status != "setup-required" {
						if a.RevokedAt != "" {
							row.Status = "revoked"
						} else {
							row.Status = "changed"
						}
					}
					break
				}
			}
			if snapshot.Status == "review-required" && operation == "review" {
				var review *assistantSourceReview
				for i := range ledger.Reviews {
					a := &ledger.Reviews[i]
					if !a.Historical && a.Scope == scope && !a.Consumed && assistantSourceSnapshotEqual(a.Snapshot, snapshot) {
						review = a
					}
				}
				if review == nil {
					if len(ledger.Reviews) >= assistantRunCountLimit {
						return empty, withCode(CodeTooLarge, errAssistantRunInvalid)
					}
					id, err := assistantSourceID()
					if err != nil {
						return empty, err
					}
					ledger.Reviews = append(ledger.Reviews, assistantSourceReview{Version: 1, ID: id, Revision: 1, Scope: scope, Snapshot: snapshot})
					review = &ledger.Reviews[len(ledger.Reviews)-1]
					changed = true
				}
				row.ReviewID, row.ReviewRevision = review.ID, review.Revision
			}
		}
		view.Sources = append(view.Sources, row)
	}
	if changed {
		if err = writeAssistantSourceLedger(r.Context(), store, ledger, writePrivateData); err != nil {
			return empty, err
		}
	}
	if operation == "revoke" {
		// Revocation needs no owner observation. Return the exact persisted receipt;
		// the next GET supplies the complete freshly observed catalog.
		for _, a := range ledger.Approvals {
			if a.ID == revoke.ApprovalID {
				view.Sources = []assistantSourceReviewView{{Snapshot: a.Snapshot, Status: "revoked", Approval: &assistantSourceApprovalReference{ID: a.ID, Revision: a.Revision, ApprovedAt: a.ApprovedAt, RevokedAt: a.RevokedAt}}}
			}
		}
	}
	return view, nil
}
