package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
)

// Bound once to a server-authorized workspace. The coordinator owns discovery
// and batch/admission leases; this interface owns only journal content.
type evaluationJournalProvider interface {
	Batch(string) evaluationJournal
}
type evaluationSnapshot struct {
	Document inputEvaluation
	SHA256   string
}

// Empty revision is create-only; otherwise the exact saved bytes must match.
// Failed or uncertain writes return no success snapshot and are never retried.
// Runner calls MUST remain outside these short transactions. The owning workflow
// reconciles Runner's durable release/run/event identities on its next pass.
type evaluationJournal interface {
	Read(context.Context) (evaluationSnapshot, error)
	CompareAndSwap(context.Context, inputEvaluation, string) (evaluationSnapshot, error)
}

var errEvaluationConflict = errors.New("Evaluation history changed. Reload before continuing.")

func (s *Server) evaluationJournal(root *os.Root, id string) evaluationJournal {
	if s.evaluationJournals != nil {
		return s.evaluationJournals.Batch(id)
	}
	return fileEvaluationJournal{root: root, id: id}
}

// A checkpoint belongs to one coordinator lease. A failed acknowledgement ends
// its use, even if the write may have committed. Read fresh state on recovery.
type evaluationCheckpoint struct {
	journal  evaluationJournal
	revision string
	failure  error
}

func (c *evaluationCheckpoint) save(ctx context.Context, b inputEvaluation) error {
	if c.failure != nil {
		return c.failure
	}
	saved, err := c.journal.CompareAndSwap(ctx, b, c.revision)
	if err != nil {
		c.failure = err
		return err
	}
	c.revision = saved.SHA256
	return nil
}
func (c *evaluationCheckpoint) retain(b inputEvaluation) error {
	// Preserve a Runner acknowledgement even after its request was cancelled.
	ctx, cancel := context.WithTimeout(context.Background(), privateDataLockWait)
	defer cancel()
	return c.save(ctx, b)
}
func decodeEvaluationSnapshot(raw []byte, id string) (evaluationSnapshot, error) {
	var b inputEvaluation
	if len(raw) > inputEvaluationLimit {
		return evaluationSnapshot{}, errors.New("This evaluation exceeds its storage limit.")
	}
	if err := decodeDataJSON(raw, &b); err != nil {
		return evaluationSnapshot{}, err
	}
	if err := validateEvaluationJournal(b, id); err != nil {
		return evaluationSnapshot{}, err
	}
	return evaluationSnapshot{Document: b, SHA256: digestOf(raw)}, nil
}
func validateEvaluationJournal(b inputEvaluation, id string) error {
	if b.Version != 1 || !inputEvaluationID.MatchString(id) || b.ID != id || !inputPreparationID.MatchString(b.PreparationID) || len(b.Rows) > inputRecordLimit {
		return errors.New("Invalid evaluation record.")
	}
	seen := map[string]bool{}
	for _, row := range b.Rows {
		key := row.RecordID
		if b.Subscription != nil {
			key = row.EventID
		}
		if key == "" || seen[key] {
			return errors.New("Invalid or duplicate evaluation row.")
		}
		seen[key] = true
	}
	if b.Subscription != nil && (b.Subscription.Mode != "new" && b.Subscription.Mode != "changed") {
		return errors.New("Invalid subscription mode.")
	}
	return nil
}
func encodeEvaluationJournal(b inputEvaluation, id string) ([]byte, error) {
	if err := validateEvaluationJournal(b, id); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return nil, err
	}
	if len(raw) > inputEvaluationLimit {
		return nil, errors.New("This evaluation exceeds its storage limit.")
	}
	return raw, nil
}
func sameEvaluationValue(a, b any) bool {
	x, e := json.Marshal(a)
	y, f := json.Marshal(b)
	return e == nil && f == nil && bytes.Equal(x, y)
}
func frozenEvaluation(b inputEvaluation) inputEvaluation {
	b.JobID = ""
	b.ConfirmedAt = ""
	b.State = ""
	b.Problem = ""
	b.Rows = nil
	b.Subscription = nil
	return b
}
func evaluationRowTerminal(state string) bool {
	return contains([]string{"completed", "failed", "interrupted", "expired", "skipped", "cancelled", "superseded"}, state)
}

// Validate retained intent, not just document shape. A different storage adapter
// must not reset an accepted job, regenerate delivery bytes or erase history.
func validateEvaluationTransition(before, next inputEvaluation) error {
	bad := errors.New("Retained evaluation intent and results cannot be replaced.")
	if !sameEvaluationValue(frozenEvaluation(before), frozenEvaluation(next)) || before.ConfirmedAt != "" && before.ConfirmedAt != next.ConfirmedAt || before.JobID != "" && before.JobID != next.JobID {
		return bad
	}
	if before.Subscription != nil {
		a, b := before.Subscription, next.Subscription
		if b == nil || a.Mode != b.Mode || a.IncludeExisting != b.IncludeExisting || a.SourceClass != b.SourceClass || a.TriggerID != "" && a.TriggerID != b.TriggerID {
			return bad
		}
		if a.Initialized && (!b.Initialized || !sameEvaluationValue(a.Baseline, b.Baseline) || a.PolicyDigest != b.PolicyDigest) {
			return bad
		}
	} else if next.Subscription != nil {
		if before.ConfirmedAt != "" || next.ConfirmedAt == "" || len(next.Rows) != 0 {
			return bad
		}
		return nil // Explicit subscription confirmation replaces the preview selection.
	}
	if len(next.Rows) < len(before.Rows) || next.Subscription == nil && len(next.Rows) != len(before.Rows) {
		return bad
	}
	for i, old := range before.Rows {
		row := next.Rows[i]
		if row.RecordID != old.RecordID || row.RecordDigest != old.RecordDigest || row.EventID != old.EventID || row.EventAt != old.EventAt || old.RunID != "" && row.RunID != old.RunID || old.OccurrenceID != "" && row.OccurrenceID != old.OccurrenceID {
			return bad
		}
		if !sameEvaluationValue(old.Input, row.Input) {
			if next.Subscription == nil || len(row.Input) != 0 || len(old.Input) == 0 || (row.OccurrenceID == "" && row.State != "superseded") {
				return bad
			}
		}
		if evaluationRowTerminal(old.State) && (row.State != old.State || !sameEvaluationValue(row.Result, old.Result) || !sameEvaluationValue(row.Signing, old.Signing)) {
			return bad
		}
	}
	return nil
}

func (s *Server) createEvaluationReview(ctx context.Context, root *os.Root, b inputEvaluation) error {
	if err := validateEvaluationJournal(b, b.ID); err != nil {
		return err
	}
	staging := ".review-" + b.ID
	for _, name := range []string{b.ID, staging} {
		if _, err := root.Lstat(name); err == nil {
			return os.ErrExist
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	br, _, err := openOwnFolder(root, []string{staging}, true, "evaluation review")
	if err != nil {
		return err
	}
	defer br.Close()
	// Retain hidden staging if creation or publication is uncertain. Discovery
	// ignores it; an automatic retry must never replace an accepted review.
	if _, err = s.evaluationJournal(br, b.ID).CompareAndSwap(ctx, b, ""); err != nil {
		return err
	}
	if err = root.Rename(staging, b.ID); err != nil {
		return err
	}
	return syncPrivateDirectory(root)
}
