package desk

// A question finishes its Agent run. An explicit answer admits a new run and
// carries only Desk history; a completed canonical checkpoint stays read-only.
import (
	"encoding/hex"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

type assistantRunAnswerOrigin struct {
	ParentRunID string                        `json:"parentRunId"`
	AdmissionID string                        `json:"admissionId"`
	ActionID    string                        `json:"actionId"`
	Question    contract.RunEventUserQuestion `json:"question"`
	Answer      assistantRunAnswer            `json:"answer"`
}

func validAssistantDeskRunID(id string) bool {
	if !strings.HasPrefix(id, "arun_") || len(id) != 37 || strings.ToLower(id) != id {
		return false
	}
	_, err := hex.DecodeString(id[5:])
	return err == nil
}

// This disposable projection is used only for model history and saved display.
// It is never passed to canonical persistence or execution admission.
func assistantRunHistoryProjection(run assistantRunRecord) assistantRunRecord {
	events := make([]assistantRunEvent, 0, len(run.AnswerHistory)+len(run.Events))
	actions := make([]assistantRunActionReceipt, 0, len(run.AnswerHistory)+len(run.Actions))
	for _, origin := range run.AnswerHistory {
		q, a := origin.Question, origin.Answer
		// Question/call IDs belong to their run. Qualify display copies so a later
		// producer may reuse its local IDs without merging separate person answers.
		q.ID = "retained-question-" + digestOf([]byte(origin.ParentRunID + "\x00" + q.ID))[:32]
		a.QuestionID = q.ID
		events = append(events, assistantRunEvent{ID: "history-" + origin.ParentRunID, Event: contract.RunEvent{UserQuestion: &q}})
		actions = append(actions, assistantRunActionReceipt{ID: "history-action-" + digestOf([]byte(origin.ParentRunID + "\x00" + origin.ActionID))[:32], Kind: "answer", Answer: &a})
	}
	run.Events = append(events, run.Events...)
	run.Actions = append(actions, run.Actions...)
	return run
}

func validateAssistantAnswerLinks(ledger assistantRunLedger) error {
	byID := map[string]assistantRunRecord{}
	for _, run := range ledger.Runs {
		byID[run.ID] = run
	}
	for _, run := range ledger.Runs {
		if len(run.AnswerHistory) > assistantRunCountLimit {
			return errAssistantRunInvalid
		}
		seen := map[string]bool{}
		for i, origin := range run.AnswerHistory {
			parent, ok := byID[origin.ParentRunID]
			if !ok || parent.ID == run.ID || seen[parent.ID] || parent.State != "complete" || origin.AdmissionID != parent.AdmissionID || !assistantRunText(origin.ActionID, 128) || len(parent.AnswerHistory) != i || i > 0 && !reflect.DeepEqual(parent.AnswerHistory, run.AnswerHistory[:i]) {
				return errAssistantRunInvalid
			}
			seen[parent.ID] = true
			binding := parent.Binding
			binding.ExecutionKey = run.Binding.ExecutionKey
			binding.Unit = run.Binding.Unit
			if !reflect.DeepEqual(binding, run.Binding) || validateAssistantRunAnswer(parent, origin.Answer) != nil {
				return errAssistantRunInvalid
			}
			q, err := assistantRunQuestion(parent, origin.Answer.QuestionID)
			if err != nil || !reflect.DeepEqual(*q, origin.Question) {
				return errAssistantRunInvalid
			}
			child := run.ID
			if i+1 < len(run.AnswerHistory) {
				child = run.AnswerHistory[i+1].ParentRunID
			}
			found := false
			for _, receipt := range parent.Actions {
				if receipt.ID == origin.ActionID && receipt.SuccessorRunID == child && receipt.Answer != nil && reflect.DeepEqual(*receipt.Answer, origin.Answer) {
					found = true
				}
			}
			if !found {
				return errAssistantRunInvalid
			}
		}
		for _, receipt := range run.Actions {
			if receipt.SuccessorRunID == "" {
				continue
			}
			child, ok := byID[receipt.SuccessorRunID]
			if !ok || len(child.AnswerHistory) != len(run.AnswerHistory)+1 || child.AnswerHistory[len(run.AnswerHistory)].ParentRunID != run.ID || child.AnswerHistory[len(run.AnswerHistory)].ActionID != receipt.ID {
				return errAssistantRunInvalid
			}
		}
	}
	return nil
}

type assistantRunAnswerAdmission struct {
	parent     assistantRunRecord
	action     assistantRunAction
	origin     assistantRunAnswerOrigin
	checkpoint contract.Checkpoint
	savedPack  *assistantSavedPackBasis
	critic     *assistantCriticSnapshot
	canonical  bool
	promote    func(assistantRunRecord) error
	prepare    func(assistantRunRecord) error
}

func (a *assistantRunAnswerAdmission) validateParent(ledger *assistantRunLedger, binding assistantRunBinding) error {
	for _, parent := range ledger.Runs {
		if parent.ID != a.parent.ID {
			continue
		}
		b := parent.Binding
		b.ExecutionKey = binding.ExecutionKey
		b.Unit = binding.Unit
		pending, err := pendingAssistantRunQuestion(parent)
		if err != nil || parent.Historical || parent.Revision != a.action.Revision || parent.State != "awaiting-input" || parent.Checkpoint == nil || parent.Checkpoint.Status != "complete" || !assistantRunContinuationSafe(parent) || pending == nil || pending.ID != a.origin.Answer.QuestionID || !reflect.DeepEqual(b, binding) || validateAssistantRunAnswer(parent, a.origin.Answer) != nil {
			return errAssistantRunConflict
		}
		return nil
	}
	return errAssistantRunNotFound
}
func (a *assistantRunAnswerAdmission) initialize(run *assistantRunRecord) error {
	cp := a.checkpoint
	// Fresh preparation owns the default allowance. No counters, grants, effect
	// receipts or approval decisions are copied from a completed checkpoint.
	if cp.Status != "running" || cp.Used != 0 || len(cp.Grants) != 0 || len(cp.Operations) != 0 || cp.Approval != nil || cp.Handoff != nil || cp.Durable != nil || cp.ID == a.parent.Checkpoint.ID {
		return errAssistantRunScope
	}
	if a.canonical {
		if _, err := initializeAssistantCanonical(run, cp); err != nil {
			return err
		}
	} else {
		saved, err := readAssistantRunCheckpoint(cp, run.Binding)
		if err != nil {
			return err
		}
		run.Checkpoint = saved
	}
	run.SavedPack = a.savedPack
	if a.critic != nil {
		a.critic.fingerprint = assistantCriticFingerprint(run.ID, run.Binding, a.critic)
		run.CriticCustody = &assistantCriticCustody{Version: 2, Fingerprint: a.critic.fingerprint}
	}
	run.AnswerHistory = append(append([]assistantRunAnswerOrigin{}, a.parent.AnswerHistory...), a.origin)
	return nil
}

// Caller holds service.mu. Preparation is read-only and precedes the atomic
// parent receipt/new admission replacement. A retry observes the receipt only.
func (h *assistantRunService) answerSuccessor(r *http.Request, parent assistantRunRecord, action assistantRunAction) (assistantRunRecord, error) {
	if parent.Binding.Owner.Kind != "chat" {
		return parent, errAssistantRunScope
	}
	if parent.Historical || parent.Revision != action.Revision || parent.State != "awaiting-input" || parent.Checkpoint == nil || parent.Checkpoint.Status != "complete" || !assistantRunContinuationSafe(parent) {
		return parent, errAssistantRunConflict
	}
	if h.executor(parent.Binding.Owner.Kind) == nil || h.active[parent.ID] != nil || len(h.active) >= assistantRunActiveLimit {
		return parent, errAssistantRunUnavailable
	}
	pending, err := pendingAssistantRunQuestion(parent)
	if err != nil || pending == nil || pending.ID != action.QuestionID {
		return parent, errAssistantRunConflict
	}
	answer := assistantRunAnswer{QuestionID: action.QuestionID, QuestionDigest: action.QuestionDigest, Selected: append([]string{}, action.Answer.Selected...), Text: *action.Answer.Text, At: time.Now().UTC().Format(time.RFC3339Nano)}
	if err = validateAssistantRunAnswer(parent, answer); err != nil {
		return parent, err
	}
	if err = h.acquire(r.Context()); err != nil {
		return parent, err
	}
	defer h.releaseIdle()
	handle, err := h.continuationContext(parent)
	if err != nil {
		return parent, err
	}
	defer handle.Release()
	if handle != nil {
		if err = h.contexts.registry.reserveRun(handle); err != nil {
			return parent, err
		}
	}
	identity, _, err := h.server.assistantRunIdentity(r)
	if err != nil {
		return parent, err
	}
	// The opaque admission key is Desk-owned and stable for this person action.
	admissionID := "answer-" + digestOf([]byte(parent.ID+"\x00"+action.ID))
	start := assistantRunStart{Purpose: parent.Binding.Owner.Kind, OwnerID: parent.Binding.Owner.ID, Revision: parent.Binding.Owner.Revision, RequestID: parent.Binding.RequestID, AdmissionID: admissionID}
	source, err := h.server.readAssistantRunSource(r, start, identity, parent.Binding.Project)
	if err != nil {
		return parent, err
	}
	same := source.Binding
	same.ExecutionKey = parent.Binding.ExecutionKey
	same.Unit = parent.Binding.Unit
	if !reflect.DeepEqual(same, parent.Binding) {
		return parent, errAssistantRunScope
	}
	assistantContextOnSource(&source, handle)
	canonical, err := assistantCanonicalAdmission(h.executor(start.Purpose), source)
	if err != nil {
		return parent, err
	}
	cp, err := h.executor(start.Purpose).Prepare(r.Context(), source)
	if err != nil {
		return parent, err
	}
	source.Binding.ExecutionKey = cp.RequestKey
	source.Binding.Unit = cp.Unit
	if cp.Phase != source.Binding.Phase {
		return parent, errAssistantRunScope
	}
	admission := &assistantRunAnswerAdmission{parent: parent, action: action, origin: assistantRunAnswerOrigin{ParentRunID: parent.ID, AdmissionID: parent.AdmissionID, ActionID: action.ID, Question: *pending, Answer: answer}, checkpoint: cp, canonical: canonical}
	history := append(append([]assistantRunAnswerOrigin{}, parent.AnswerHistory...), admission.origin)
	initial, err := h.server.prepareAssistantAnswerInput(r, source, history)
	if err != nil {
		return parent, err
	}
	admission.savedPack, admission.critic = initial.savedPack, initial.custody.critic
	promoted := ""
	var prepared *assistantPreparedLaunch
	admission.promote = func(run assistantRunRecord) error {
		if handle != nil {
			if err := h.contexts.registry.promoteRun(handle, run.ID, run.Binding); err != nil {
				return err
			}
			promoted = run.ID
			h.rememberContext(run)
		}
		return nil
	}
	admission.prepare = func(run assistantRunRecord) error {
		// Admit owns the live lease and this exact not-yet-persisted candidate.
		writer := &assistantRunWriter{lease: h.lease, binding: run.Binding, id: run.ID, revision: run.Revision}
		var err error
		prepared, err = h.prepareLaunch(r, run, source, writer, nil, &run)
		if err == nil {
			prepared.execution.input = initial.custody
			prepared.execution.initialInput = initial.chat
		}
		return err
	}
	record, created, err := h.lease.Admit(r.Context(), source.Binding, admissionID, assistantRunActionDigest(action), func() error {
		current, _, err := h.server.assistantRunIdentity(r)
		if err != nil {
			return err
		}
		if !assistantRunIdentityMatches(current, identity) {
			return errAssistantRunScope
		}
		fresh, err := h.server.resolveAssistantRun(r, start, identity, parent.Binding.Project)
		if err != nil {
			return err
		}
		fresh.Binding.ExecutionKey = source.Binding.ExecutionKey
		fresh.Binding.Unit = source.Binding.Unit
		if !assistantRunFreshSourceEqual(source, fresh) {
			return errAssistantRunConflict
		}
		return h.server.recheckAssistantAnswerInputLocked(r, source, initial)
	}, admission)
	if err != nil || !created {
		if prepared != nil {
			prepared.discard()
		}
		if promoted != "" {
			h.settleContextLocked(promoted)
		}
		if err == nil {
			err = errAssistantRunConflict
		}
		return parent, err
	}
	// The exact prepared writer/binding now observes the acknowledged durable
	// candidate. Activation has no second preparation or fallible admission.
	if prepared.execution.canonicalStore != nil {
		prepared.execution.canonicalStore.prepared = nil
	}
	h.settleContextLocked(parent.ID)
	prepared.activate()
	h.notifyLocked(parent.ID)
	h.notifyLocked(record.ID)
	// Keep the action endpoint bound to the parent. Readers follow its saved
	// successor link without granting another admission or replaying any work.
	return h.lease.store.Read(r.Context(), parent.Binding, parent.ID)
}
