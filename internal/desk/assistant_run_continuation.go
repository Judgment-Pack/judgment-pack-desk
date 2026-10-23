package desk

// Continuation is a finite host admission, never a retry of an operation.
import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter/turn"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

type assistantRunAnswer struct {
	QuestionID     string   `json:"questionId"`
	QuestionDigest string   `json:"questionDigest"`
	Selected       []string `json:"selected"`
	Text           string   `json:"text"`
	At             string   `json:"at"`
}
type assistantRunContinuationReceipt struct {
	ID     string                    `json:"id"`
	Digest string                    `json:"digest"`
	Kind   string                    `json:"kind"`
	Answer *assistantRunAnswer       `json:"answer,omitempty"`
	Grant  *contract.CheckpointGrant `json:"grant,omitempty"`
}

func assistantRunContinuations(run assistantRunRecord) []assistantRunContinuationReceipt {
	var out []assistantRunContinuationReceipt
	for _, a := range run.Actions {
		if a.Kind == "answer" || a.Kind == "extend-budget" {
			out = append(out, assistantRunContinuationReceipt{ID: a.ID, Digest: a.Digest, Kind: a.Kind, Answer: a.Answer, Grant: a.Grant})
		}
	}
	return out
}
func validateAssistantRunActionShape(a assistantRunAction) error {
	if a.Kind != "answer" && (a.QuestionID != "" || a.QuestionDigest != "" || a.Answer != nil) {
		return errAssistantRunInvalid
	}
	if a.Kind != "approval" && (a.CallID != "" || a.ArgsDigest != "" || a.Approved != nil || a.Note != nil) {
		return errAssistantRunInvalid
	}
	if a.Kind == "answer" && (!assistantChatID.MatchString(a.QuestionID) || !assistantRunDigest(a.QuestionDigest) || a.Answer == nil || a.Answer.Selected == nil || a.Answer.Text == nil) {
		return errAssistantRunInvalid
	}
	return nil
}
func assistantRunQuestionDigest(q contract.Question) string {
	raw, _ := json.Marshal(q)
	value, _ := runcontext.ParseJSON(raw)
	return strings.TrimPrefix(runcontext.RecordDigest(value), "sha256:")
}
func assistantRunQuestion(run assistantRunRecord, id string) (*contract.RunEventUserQuestion, error) {
	var out *contract.RunEventUserQuestion
	for _, event := range run.Events {
		if q := event.Event.UserQuestion; q != nil && q.ID == id {
			if out != nil {
				return nil, errAssistantRunInvalid
			}
			out = q
		}
	}
	if out == nil {
		return nil, errAssistantRunConflict
	}
	raw, _ := json.Marshal(out.Question)
	if !assistantChatID.MatchString(out.ID) {
		return nil, errAssistantRunInvalid
	}
	if _, err := turn.ReadUserQuestion(raw); err != nil {
		return nil, errAssistantRunInvalid
	}
	return out, nil
}
func pendingAssistantRunQuestion(run assistantRunRecord) (*contract.RunEventUserQuestion, error) {
	answered := map[string]bool{}
	for _, a := range run.Actions {
		if a.Answer != nil {
			if answered[a.Answer.QuestionID] {
				return nil, errAssistantRunInvalid
			}
			answered[a.Answer.QuestionID] = true
		}
	}
	seen := map[string]bool{}
	var pending *contract.RunEventUserQuestion
	for _, event := range run.Events {
		if q := event.Event.UserQuestion; q != nil {
			if seen[q.ID] {
				return nil, errAssistantRunInvalid
			}
			seen[q.ID] = true
			if !answered[q.ID] {
				if pending != nil {
					return nil, errAssistantRunInvalid
				}
				if _, err := assistantRunQuestion(run, q.ID); err != nil {
					return nil, err
				}
				pending = q
			}
		}
	}
	return pending, nil
}
func validateAssistantRunAnswer(run assistantRunRecord, a assistantRunAnswer) error {
	q, err := assistantRunQuestion(run, a.QuestionID)
	if err != nil || a.QuestionDigest != assistantRunQuestionDigest(q.Question) || a.Selected == nil || !utf8.ValidString(a.Text) || strings.ContainsRune(a.Text, 0) || runcontext.U16Length(a.Text) > 4000 || len(a.Selected) == 0 && strings.TrimSpace(a.Text) == "" {
		return errAssistantRunInvalid
	}
	if _, err = time.Parse(time.RFC3339Nano, a.At); err != nil {
		return errAssistantRunInvalid
	}
	if q.Question.Selection == "single" && len(a.Selected) > 1 {
		return errAssistantRunInvalid
	}
	seen := map[string]bool{}
	for _, id := range a.Selected {
		found := false
		for _, o := range q.Question.Options {
			found = found || id == o.ID
		}
		if !found || seen[id] {
			return errAssistantRunInvalid
		}
		seen[id] = true
	}
	return nil
}
func assistantRunContinuationSafe(run assistantRunRecord) bool {
	cp := run.Checkpoint
	if cp == nil || cp.Approval != nil && cp.Approval.State != "answered" || cp.Handoff != nil && cp.Handoff.State != "continued" {
		return false
	}
	for _, op := range run.Operations {
		if op.State != "completed" && op.State != "refused" {
			return false
		}
	}
	for _, op := range cp.Operations {
		if op.Status == "running" || op.Status == "interrupted" {
			return false
		}
	}
	return true
}

// Caller holds h.mu; the process lease excludes every other server and the
// active registry excludes a still-draining invocation of this run.
func (h *assistantRunService) continueRun(r *http.Request, record assistantRunRecord, a assistantRunAction) (assistantRunRecord, error) {
	if record.Historical || record.Revision != a.Revision || !assistantRunPaused(record.State) {
		return record, errAssistantRunConflict
	}
	if h.server.assistantRunExecutor == nil || h.active[record.ID] != nil || len(h.active) >= assistantRunActiveLimit {
		return record, errAssistantRunUnavailable
	}
	if err := h.acquire(r.Context()); err != nil {
		return record, err
	}
	defer h.releaseIdle()
	writer, err := h.lease.Writer(r.Context(), record.Binding, record.ID)
	if err != nil {
		return record, err
	}
	var source assistantRunSource
	err = writer.change(r.Context(), false, func(run *assistantRunRecord) (bool, error) {
		if run.Revision != a.Revision || run.Historical || !assistantRunContinuationSafe(*run) {
			return false, errAssistantRunConflict
		}
		identity, _, err := h.server.assistantRunIdentity(r)
		if err != nil {
			return false, err
		}
		if !assistantRunIdentityMatches(identity, run.Binding) {
			return false, errAssistantRunScope
		}
		start := assistantRunStart{Purpose: "chat", OwnerID: run.Binding.Owner.ID, Revision: run.Binding.Owner.Revision, RequestID: run.Binding.RequestID, AdmissionID: run.AdmissionID}
		// Already under the private storage transaction: do not nest its lock.
		source, err = h.server.resolveAssistantRun(r, start, identity, run.Binding.Project)
		if err != nil {
			return false, err
		}
		source.Binding.ExecutionKey = run.Binding.ExecutionKey
		source.Binding.Unit = run.Binding.Unit
		if !assistantRunFreshSourceEqual(source, assistantRunSource{Binding: run.Binding, Original: source.Original, Request: source.Request}) {
			return false, errAssistantRunScope
		}
		receipt := assistantRunActionReceipt{ID: a.ID, Digest: assistantRunActionDigest(a), Kind: a.Kind}
		pending, err := pendingAssistantRunQuestion(*run)
		if err != nil {
			return false, err
		}
		switch a.Kind {
		case "answer":
			if run.State != "awaiting-input" || pending == nil || pending.ID != a.QuestionID || run.Checkpoint.Status != "complete" || run.Checkpoint.Used >= run.Checkpoint.Limit {
				return false, errAssistantRunConflict
			}
			answer := &assistantRunAnswer{QuestionID: a.QuestionID, QuestionDigest: a.QuestionDigest, Selected: append([]string{}, a.Answer.Selected...), Text: *a.Answer.Text, At: time.Now().UTC().Format(time.RFC3339Nano)}
			if err = validateAssistantRunAnswer(*run, *answer); err != nil {
				return false, err
			}
			cp, err := reopenAssistantRunQuestionCheckpoint(*run)
			if err != nil {
				return false, err
			}
			run.Checkpoint = cp
			receipt.Answer = answer
		case "extend-budget":
			if run.State != "paused-budget" || pending != nil {
				return false, errAssistantRunConflict
			}
			cp, err := runcontext.ExtendExecution(*run.Checkpoint, false, time.Now())
			if err != nil {
				return false, errAssistantRunConflict
			}
			receipt.Grant = &cp.Grants[len(cp.Grants)-1]
			run.Checkpoint = cp
		default:
			return false, errAssistantRunInvalid
		}
		run.Actions = append(run.Actions, receipt)
		run.State = "admitted"
		return true, nil
	})
	if err != nil {
		return record, err
	}
	latest, err := h.lease.store.Read(r.Context(), record.Binding, record.ID)
	if err != nil {
		return record, err
	}
	if err = h.launch(r, latest, source, writer, nil); err != nil {
		_ = writer.Stop(context.Background())
		return record, err
	}
	h.notifyLocked(record.ID)
	return latest, nil
}

// History is additive data, with original request/phase/source identity intact.
func assistantRunAnswerHistory(run assistantRunRecord, chat *assistantChatInput) error {
	for _, receipt := range run.Actions {
		if a := receipt.Answer; a != nil {
			if err := validateAssistantRunAnswer(run, *a); err != nil {
				return err
			}
			q, _ := assistantRunQuestion(run, a.QuestionID)
			labels := []string{}
			for _, id := range a.Selected {
				for _, o := range q.Question.Options {
					if id == o.ID {
						labels = append(labels, o.Label)
					}
				}
			}
			raw, err := json.Marshal(struct {
				Question contract.Question   `json:"question"`
				Answer   *assistantRunAnswer `json:"answer"`
				Labels   []string            `json:"selectedLabels"`
			}{q.Question, a, labels})
			if err != nil {
				return err
			}
			value, err := runcontext.ParseJSON(raw)
			if err != nil {
				return err
			}
			chat.Documents = append(chat.Documents, runcontext.Document{ID: "question-answer-" + digestOf([]byte(a.QuestionID))[:32], Kind: "history", Label: "Person's explicit answer to a retained question; user history, no tool approval, policy change or compute grant", Value: value})
			chat.Transcript += "\n\nPERSON'S RETAINED QUESTION ANSWER (user history; preserve the original task and constraints; no tool approval or compute grant)\n" + string(raw)
		}
	}
	return nil
}

// Canonical Initialize makes this same complete-to-running transition. Host
// preflight Runtime calls precede adapter initialization, so acknowledge it in
// the action transaction first. This changes no allowance or operation receipt.
func reopenAssistantRunQuestionCheckpoint(run assistantRunRecord) (*contract.Checkpoint, error) {
	if run.Checkpoint == nil || run.Checkpoint.Status != "complete" || run.Checkpoint.Used >= run.Checkpoint.Limit || !assistantRunContinuationSafe(run) {
		return nil, errAssistantRunConflict
	}
	if _, err := readAssistantRunCheckpoint(*run.Checkpoint, run.Binding); err != nil {
		return nil, err
	}
	cp := *run.Checkpoint
	cp.Status = "running"
	if cp.Digest != nil {
		raw, err := json.Marshal(cp)
		if err != nil {
			return nil, err
		}
		value, err := runcontext.ParseJSON(raw)
		if err != nil {
			return nil, err
		}
		digest := runcontext.RecordDigest(value)
		cp.Digest = &digest
	}
	if _, err := readAssistantRunCheckpoint(cp, run.Binding); err != nil {
		return nil, err
	}
	return &cp, nil
}

// Original wire validation distinguishes an omitted empty answer text from a
// present empty text answer with selected options. Struct decoding alone cannot.
func readAssistantRunContinuation(raw []byte) (assistantRunContinuationReceipt, error) {
	var receipt assistantRunContinuationReceipt
	fields, err := assistantTestObject(raw)
	if err != nil {
		return receipt, err
	}
	if json.Unmarshal(raw, &receipt) != nil || !assistantRunText(receipt.ID, 128) || !assistantRunDigest(receipt.Digest) {
		return receipt, errAssistantRunInvalid
	}
	allowed := []string{"id", "kind", "digest"}
	switch receipt.Kind {
	case "answer":
		allowed = append(allowed, "answer")
		if fields["answer"] == nil || readAssistantDialectCustody(fields["answer"], reflect.TypeOf(assistantRunAnswer{})) != nil {
			return receipt, errAssistantRunInvalid
		}
	case "extend-budget":
		allowed = append(allowed, "grant")
		if fields["grant"] == nil || readAssistantDialectCustody(fields["grant"], reflect.TypeOf(struct {
			From int64  `json:"from"`
			To   int64  `json:"to"`
			At   string `json:"at"`
		}{})) != nil {
			return receipt, errAssistantRunInvalid
		}
	default:
		return receipt, errAssistantRunInvalid
	}
	for key := range fields {
		if !contains(allowed, key) {
			return receipt, errAssistantRunInvalid
		}
	}
	return receipt, nil
}
