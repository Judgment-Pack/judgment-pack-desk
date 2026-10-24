package desk

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/jsonscalar"
)

type assistantApprovalReference struct {
	CallID     string `json:"callId"`
	ArgsDigest string `json:"argsDigest"`
}

type assistantRunView struct {
	PackChecks        []assistantPackCheck              `json:"packChecks,omitempty"`
	ApprovalReference *assistantApprovalReference       `json:"approvalReference,omitempty"`
	AdmissionID       string                            `json:"admissionId"`
	DisplayPreview    []assistantRunPreview             `json:"displayPreview,omitempty"`
	Continuations     []assistantRunContinuationReceipt `json:"continuations,omitempty"`
	ChatProjection    *assistantChatProjectionView      `json:"chatProjection,omitempty"`
	ProjectionIssue   string                            `json:"projectionIssue,omitempty"`
	ID                string                            `json:"id"`
	Revision          uint64                            `json:"revision"`
	Owner             assistantRunOwner                 `json:"owner"`
	RequestID         string                            `json:"requestId"`
	State             string                            `json:"state"`
	Historical        bool                              `json:"historical,omitempty"`
	Checkpoint        *contract.Checkpoint              `json:"checkpoint,omitempty"`
	Operations        []assistantRunOperation           `json:"operations"`
	Events            []assistantRunEvent               `json:"events"`
	Cursor            string                            `json:"cursor"`
}

func assistantRunPublic(run assistantRunRecord) assistantRunView {
	var approval *assistantApprovalReference
	if run.State == "awaiting-input" && run.Checkpoint != nil && run.Checkpoint.Approval != nil && run.Checkpoint.Approval.State == "asked" {
		approval = &assistantApprovalReference{run.Checkpoint.Approval.CallID, assistantRunArgsDigest(run.Checkpoint.Approval.Args)}
	}
	latest := map[string]assistantPackCheck{}
	for _, receipt := range run.PackChecks {
		if receipt.Check != nil {
			latest[receipt.DocumentDigest] = *receipt.Check
		}
	}
	keys := make([]string, 0, len(latest))
	for digest := range latest {
		keys = append(keys, digest)
	}
	sort.Strings(keys)
	var checks []assistantPackCheck
	for _, digest := range keys {
		checks = append(checks, latest[digest])
	}
	return assistantRunView{PackChecks: checks, ApprovalReference: approval, AdmissionID: run.AdmissionID, DisplayPreview: run.DisplayPreview, Continuations: assistantRunContinuations(run), ChatProjection: assistantChatProjectionPublic(run.ChatProjection), ProjectionIssue: run.ProjectionIssue, ID: run.ID, Revision: run.Revision, Owner: run.Binding.Owner, RequestID: run.Binding.RequestID, State: run.State, Historical: run.Historical, Checkpoint: run.Checkpoint, Operations: run.Operations, Events: run.Events, Cursor: assistantRunCursor(run)}
}
func (s *Server) registerAssistantRuns() {
	s.registerAssistantBriefRuns()
	s.registerAssistantAuthoringRuns()
	s.mux.HandleFunc("POST /api/assistant-runs/{run}/pack-checks", s.handleAssistantPackRecheck)
	s.mux.HandleFunc("GET /api/assistant-runs/owners/{owner}/requests/{request}", s.handleAssistantChatRuns)
	s.mux.HandleFunc("POST /api/assistant-runs", s.handleAssistantRunStart)
	s.mux.HandleFunc("GET /api/assistant-runs/{run}", s.handleAssistantRunRead)
	s.mux.HandleFunc("GET /api/assistant-runs/{run}/events", s.handleAssistantRunEvents)
	s.mux.HandleFunc("POST /api/assistant-runs/{run}/actions", s.handleAssistantRunAction)
}
func (s *Server) guardAssistantRun(w http.ResponseWriter, r *http.Request) bool {
	if !s.guard(w, r) || s.refuseUnusableStore(w) || !s.guardPrivateWorkspace(w, r) {
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.RawQuery != "" {
		writeJSONCoded(w, 400, CodeBadRequest, "Assistant run credentials and cursors belong in request headers.")
		return false
	}
	return true
}

// Decode only exact declared names and reject duplicate/unknown members before
// Go's case-insensitive struct matching. This body can never supply a binding.
func assistantRunRequest(r *http.Request, target any) error {
	raw, err := readBounded(r.Body, 16<<10)
	if err != nil || !validUTF8(raw) || jsonscalar.ValidateEscapes(raw) != nil || !uniqueJSON(raw) {
		return errAssistantRunInvalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return errAssistantRunInvalid
	}
	shape := reflect.TypeOf(target).Elem()
	for key := range fields {
		found := false
		for i := 0; i < shape.NumField(); i++ {
			if strings.Split(shape.Field(i).Tag.Get("json"), ",")[0] == key {
				found = true
				break
			}
		}
		if !found {
			return errAssistantRunInvalid
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(target); err != nil {
		return errAssistantRunInvalid
	}
	if action, ok := target.(*assistantRunAction); ok {
		allowed := []string{"id", "kind", "revision"}
		switch action.Kind {
		case "approval":
			allowed = append(allowed, "callId", "argsDigest", "approved", "note")
		case "answer":
			allowed = append(allowed, "questionId", "questionDigest", "answer")
		case "cancel", "abandon", "extend-budget":
		default:
			return errAssistantRunInvalid
		}
		for key := range fields {
			if !contains(allowed, key) {
				return errAssistantRunInvalid
			}
		}
		if action.Kind == "answer" {
			var draft map[string]json.RawMessage
			if json.Unmarshal(fields["answer"], &draft) != nil || len(draft) != 2 || draft["selected"] == nil || draft["text"] == nil {
				return errAssistantRunInvalid
			}
		}
	}
	return nil
}
func assistantRunHTTPError(w http.ResponseWriter, err error) {
	status, code, message := 409, CodeStale, "Assistant run state changed. Reload its saved state before continuing."
	switch {
	case errors.Is(err, errAssistantRunScope), errors.Is(err, errAssistantRunNotFound):
		status = 404
		code = CodeBadRequest
		message = "Assistant run is unavailable in this signed-in window."
	case errors.Is(err, errAssistantRunUnavailable):
		status = 503
		code = CodeBadRequest
		message = errAssistantRunUnavailable.Error()
	case errors.Is(err, errAssistantRunInvalid):
		status = 400
		code = CodeBadRequest
		message = "Invalid assistant run request."
	default:
		if codeOf(err) == CodeUnauthorized {
			status = 401
			code = CodeUnauthorized
			message = "Sign in before accessing assistant runs."
		}
	}
	writeJSONCoded(w, status, code, message)
}
func (s *Server) handleAssistantRunStart(w http.ResponseWriter, r *http.Request) {
	if !s.guardAssistantRun(w, r) {
		return
	}
	var start assistantRunStart
	if assistantRunRequest(r, &start) != nil {
		assistantRunHTTPError(w, errAssistantRunInvalid)
		return
	}
	ownerValid := start.Purpose == "chat" && researchChatID.MatchString(start.OwnerID) || start.Purpose == "brief" && assistantBriefSubject(start.OwnerID) || assistantAuthoringPurpose(start.Purpose) && assistantAuthoringID.MatchString(start.OwnerID)
	if !assistantRunText(start.AdmissionID, 128) || !ownerValid || !assistantRunDigest(start.Revision) || !assistantRunText(start.RequestID, 128) {
		assistantRunHTTPError(w, errAssistantRunInvalid)
		return
	}
	run, created, err := s.assistantRunService().start(r, start)
	if err != nil {
		assistantRunHTTPError(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, assistantRunPublic(run))
}
func (s *Server) handleAssistantRunRead(w http.ResponseWriter, r *http.Request) {
	if !s.guardAssistantRun(w, r) {
		return
	}
	run, err := s.assistantRunService().read(r, r.PathValue("run"))
	if err != nil {
		assistantRunHTTPError(w, err)
		return
	}
	writeJSON(w, 200, assistantRunPublic(run))
}
func (s *Server) handleAssistantRunAction(w http.ResponseWriter, r *http.Request) {
	if !s.guardAssistantRun(w, r) {
		return
	}
	var action assistantRunAction
	if assistantRunRequest(r, &action) != nil {
		assistantRunHTTPError(w, errAssistantRunInvalid)
		return
	}
	run, err := s.assistantRunService().action(r, r.PathValue("run"), action)
	if err != nil {
		assistantRunHTTPError(w, err)
		return
	}
	writeJSON(w, 200, assistantRunPublic(run))
}
func assistantRunParseCursor(cursor string, run assistantRunRecord) (uint64, int, bool) {
	parts := strings.Split(cursor, ":")
	if len(parts) != 2 {
		return 0, 0, false
	}
	revision, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	count, err := strconv.Atoi(parts[1])
	if err != nil || count < 0 || count > len(run.Events) || revision == 0 || revision > run.Revision || fmt.Sprintf("%d:%d", revision, count) != cursor {
		return 0, 0, false
	}
	return revision, count, true
}
func assistantRunStreamWrite(controller *http.ResponseController, write func() error) error {
	// A slow reader never holds storage/executor locks or an unbounded event
	// queue. Disconnect it after the bounded write and recover from the ledger.
	if err := controller.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	if err := write(); err != nil {
		return err
	}
	return controller.Flush()
}
func assistantRunSSE(w http.ResponseWriter, controller *http.ResponseController, kind, cursor string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return assistantRunStreamWrite(controller, func() error {
		id := ""
		if cursor != "" {
			id = "id: " + cursor + "\n"
		}
		_, err := fmt.Fprintf(w, "event: %s\n%sdata: %s\n\n", kind, id, raw)
		return err
	})
}
func assistantRunHeartbeat(w http.ResponseWriter, controller *http.ResponseController) error {
	return assistantRunStreamWrite(controller, func() error {
		_, err := io.WriteString(w, ": keepalive\n\n")
		return err
	})
}
func (s *Server) handleAssistantRunEvents(w http.ResponseWriter, r *http.Request) {
	if !s.guardAssistantRun(w, r) {
		return
	}
	host := s.assistantRunService()
	id := r.PathValue("run")
	// Authenticate before reserving subscriber capacity. Subscribe before the
	// snapshot, so a commit between the two always wakes this reader.
	if _, err := host.read(r, id); err != nil {
		assistantRunHTTPError(w, err)
		return
	}
	wake, release, err := host.subscribe(id)
	if err != nil {
		assistantRunHTTPError(w, err)
		return
	}
	defer release()
	controller := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	cursor := r.Header.Get("Last-Event-ID")
	first := true
	liveSequence := uint64(0)
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		run, err := host.read(r, id)
		if err != nil {
			return
		}
		revision, count, known := assistantRunParseCursor(cursor, run)
		// Event count identifies the semantic suffix independently of revision.
		// A semantic event and its following snapshot share a cursor, so every
		// subscription must reconcile even when its supplied cursor is current.
		if known {
			for i := count; i < len(run.Events); i++ {
				if assistantRunSSE(w, controller, "event", fmt.Sprintf("%d:%d", run.Revision, i+1), run.Events[i]) != nil {
					return
				}
			}
		}
		if first || !known || revision != run.Revision || count != len(run.Events) {
			if assistantRunSSE(w, controller, "snapshot", assistantRunCursor(run), assistantRunPublic(run)) != nil {
				return
			}
		}
		first = false
		cursor = assistantRunCursor(run)
		if assistantRunTerminal(run.State) || run.State == "awaiting-input" || run.State == "paused-budget" || run.State == "awaiting-client" {
			return
		}
		progress, err := host.readProgress(r, run)
		if err != nil {
			return
		}
		for _, observation := range progress {
			if observation.Sequence > liveSequence {
				if assistantRunSSE(w, controller, "progress", "", observation) != nil {
					return
				}
				liveSequence = observation.Sequence
			}
		}
		select {
		case <-r.Context().Done():
			return
		case <-wake:
		case <-heartbeat.C:
			if assistantRunHeartbeat(w, controller) != nil {
				return
			}
		}
	}
}
