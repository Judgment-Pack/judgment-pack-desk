package desk

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

const assistantRunProgressBytes = 64 * 1024
const assistantRunProgressViews = 17 // One answer and at most sixteen child views.

// No credentials or durable event identity. Cursor is a reconciliation anchor,
// never an SSE id or a replay position contributed by this observation.
type assistantRunProgress struct {
	ID        string            `json:"id"`
	Owner     assistantRunOwner `json:"owner"`
	RequestID string            `json:"requestId"`
	Epoch     string            `json:"epoch"`
	Sequence  uint64            `json:"sequence"`
	Cursor    string            `json:"cursor"`
	Text      string            `json:"text"`
	TaskID    *string           `json:"taskId,omitempty"`
}
type assistantRunProgressState struct {
	epoch    string
	sequence uint64
	views    map[string]assistantRunProgress
	retained map[string]assistantRunPreview
	events   int
}

func newAssistantRunProgressEpoch() (string, error) {
	var token [16]byte
	_, err := rand.Read(token[:])
	return hex.EncodeToString(token[:]), err
}
func validAssistantRunProgress(p contract.RunEventMessageProgress) bool {
	return p.Type == "message_progress" && len(p.AdditionalMembers) == 0 && len(p.Text) <= assistantRunProgressBytes && utf8.ValidString(p.Text) && !strings.ContainsRune(p.Text, 0) && (p.TaskID == nil || assistantRunText(*p.TaskID, 128))
}

// Progress observes the current authenticated execution; it cannot dispatch work
// or persist semantic data. The service mutex protects bounded replacement only.
func (e *assistantRunExecution) Progress(ctx context.Context, p contract.RunEventMessageProgress) error {
	if !validAssistantRunProgress(p) {
		return errAssistantRunInvalid
	}
	if err := e.Authorize(ctx); err != nil {
		e.clearProgress()
		return err
	}
	h := e.service
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.active[e.record.ID] != e || e.ctx.Err() != nil || ctx.Err() != nil {
		return errAssistantRunScope
	}
	run, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
	if err != nil {
		return err
	}
	if run.State != "running" && run.State != "admitted" || run.Checkpoint == nil || run.Checkpoint.Status != "running" {
		return errAssistantRunScope
	}
	key := ""
	if p.TaskID != nil {
		key = *p.TaskID
	}
	if e.progress.events != len(run.Events) {
		e.progress.views = nil
	}
	if e.progress.retained == nil {
		e.progress.retained = map[string]assistantRunPreview{}
	}
	if e.progress.views == nil {
		e.progress.views = map[string]assistantRunProgress{}
	}
	if _, exists := e.progress.views[key]; !exists && len(e.progress.views) >= assistantRunProgressViews {
		return errAssistantRunUnavailable
	}
	// Budget the actual escaped observation envelopes, with room for the
	// later durable reconciliation cursor and SSE framing on every row.
	total := 0
	for heldKey, held := range e.progress.views {
		if heldKey != key {
			raw, _ := json.Marshal(held)
			total += len(raw) + 128
		}
	}
	candidate := assistantRunProgress{ID: run.ID, Owner: run.Binding.Owner, RequestID: run.Binding.RequestID, Epoch: e.progress.epoch, Sequence: e.progress.sequence + 1, Text: p.Text, TaskID: p.TaskID}
	raw, _ := json.Marshal(candidate)
	if total+len(raw)+128 > 1024*1024 {
		return errAssistantRunUnavailable
	}
	var task *string
	if p.TaskID != nil {
		copy := *p.TaskID
		task = &copy
	}
	preview := assistantRunPreview{Text: p.Text, TaskID: task}
	old, had := e.progress.retained[key]
	if p.Text == "" {
		delete(e.progress.retained, key)
	} else {
		e.progress.retained[key] = preview
	}
	if validateAssistantRunPreview(e.progressPreviewLocked()) != nil {
		if had {
			e.progress.retained[key] = old
		} else {
			delete(e.progress.retained, key)
		}
		return errAssistantRunUnavailable
	}
	if e.progress.sequence >= (1<<53)-1 {
		return errAssistantRunUnavailable
	}
	e.progress.sequence++
	e.progress.views[key] = assistantRunProgress{ID: run.ID, Owner: run.Binding.Owner, RequestID: run.Binding.RequestID, Epoch: e.progress.epoch, Sequence: e.progress.sequence, Text: p.Text, TaskID: task}
	e.progress.events = len(run.Events)
	h.notifyLocked(run.ID)
	return nil
}
func (e *assistantRunExecution) clearProgress() {
	e.service.mu.Lock()
	e.progress.views = nil
	e.service.notifyLocked(e.record.ID)
	e.service.mu.Unlock()
}
func (h *assistantRunService) readProgress(r *http.Request, run assistantRunRecord) ([]assistantRunProgress, error) {
	if err := h.server.authorizeAssistantRun(r, run); err != nil {
		return nil, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	e := h.active[run.ID]
	if e == nil || !reflect.DeepEqual(e.record.Binding, run.Binding) || e.ctx.Err() != nil || run.Historical || run.State != "running" && run.State != "admitted" || run.Checkpoint == nil || run.Checkpoint.Status != "running" {
		return nil, nil
	}
	// The exact saved source and current identity are checked on both sides.
	if err := e.Authorize(r.Context()); err != nil {
		e.progress.views = nil
		return nil, err
	}
	if e.progress.events != len(run.Events) {
		return nil, nil
	}
	out := make([]assistantRunProgress, 0, len(e.progress.views))
	for _, p := range e.progress.views {
		p.Cursor = assistantRunCursor(run)
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Sequence < out[j].Sequence })
	return out, nil
}

// DisplayPreview is host custody only. It is never projected into model input,
// completed message journals, canonical checkpoints, admission or replay cursors.
type assistantRunPreview struct {
	Text   string  `json:"text"`
	TaskID *string `json:"taskId,omitempty"`
}

func validateAssistantRunPreview(rows []assistantRunPreview) error {
	if len(rows) > assistantRunProgressViews {
		return errAssistantRunInvalid
	}
	seen := map[string]bool{}
	for _, p := range rows {
		if p.Text == "" || !validAssistantRunProgress(contract.RunEventMessageProgress{Type: "message_progress", Text: p.Text, TaskID: p.TaskID}) {
			return errAssistantRunInvalid
		}
		key := ""
		if p.TaskID != nil {
			key = *p.TaskID
		}
		if seen[key] {
			return errAssistantRunInvalid
		}
		seen[key] = true
	}
	raw, err := json.Marshal(rows)
	if err != nil || len(raw) > 1024*1024 {
		return errAssistantRunInvalid
	}
	return nil
}
func (e *assistantRunExecution) progressPreviewLocked() []assistantRunPreview {
	keys := make([]string, 0, len(e.progress.retained))
	for key := range e.progress.retained {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var rows []assistantRunPreview
	for _, key := range keys {
		p := e.progress.retained[key]
		if p.TaskID != nil {
			task := *p.TaskID
			p.TaskID = &task
		}
		rows = append(rows, p)
	}
	return rows
}
func (e *assistantRunExecution) boundaryProgressPreview(completed bool) []assistantRunPreview {
	e.service.mu.Lock()
	defer e.service.mu.Unlock()
	if completed {
		delete(e.progress.retained, "")
		e.progress.views = nil
	}
	return e.progressPreviewLocked()
}
func (e *assistantRunExecution) saveProgressPreview(ctx context.Context, completed bool) error {
	rows := e.boundaryProgressPreview(completed)
	return e.writer.change(ctx, false, func(run *assistantRunRecord) (bool, error) {
		if reflect.DeepEqual(run.DisplayPreview, rows) {
			return false, nil
		}
		run.DisplayPreview = rows
		return true, nil
	})
}

func readAssistantRunPreview(raw []byte) error {
	var rows []assistantRunPreview
	if err := readAssistantDialectCustody(raw, reflect.TypeOf(rows)); err != nil {
		return err
	}
	if len(raw) > 1024*1024 || json.Unmarshal(raw, &rows) != nil || len(rows) == 0 {
		return errAssistantRunInvalid
	}
	return validateAssistantRunPreview(rows)
}
