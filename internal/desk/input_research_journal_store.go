package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"time"
)

// A provider is scoped to one server-authorized workspace. Run selects a logical
// identity, never a path supplied by a request. Handles do not own worker leases.
type researchJournalProvider interface{ Run(string) researchJournal }

// Create is create-only. Update serializes one read/change/verified commit and
// invokes change at most once. It MUST NOT retry callbacks on transaction conflict
// or an uncertain commit. No provider calls belong in a journal transaction.
// Reads and successful updates return independent snapshots. Errors (including
// read-back failures after a possible commit) never return a success snapshot.
type researchJournal interface {
	Read(context.Context) (inputResearchRun, error)
	Create(context.Context, inputResearchRun) error
	Update(context.Context, func(*inputResearchRun) error) (inputResearchRun, error)
}

func (s *Server) researchJournal(root *os.Root, id string) researchJournal {
	if s.researchJournals != nil {
		return s.researchJournals.Run(id)
	}
	return fileResearchJournal{root: root, id: id}
}

// Provider execution is independent of journal storage. Proof manifests and
// execution/admission leases still use local storage in this migration slice.
type researchRunStore struct {
	journal   researchJournal
	proofSize func(string) (int, error)
}

func (s *Server) researchRunStore(root *os.Root, id string) researchRunStore {
	return researchRunStore{journal: s.researchJournal(root, id), proofSize: func(attempt string) (int, error) { return retainedResearchProofSize(root, attempt) }}
}
func (s *Server) researchSource(root *os.Root) func(context.Context, inputResearchRun, inputResearchAttempt, json.RawMessage) (inputResearchSource, error) {
	return func(ctx context.Context, run inputResearchRun, attempt inputResearchAttempt, args json.RawMessage) (inputResearchSource, error) {
		return s.acquireResearchSource(ctx, root, run, attempt, args)
	}
}

// Completion persistence must survive cancellation after a provider has replied.
// These bounded transactions do not authorize dispatch: admission checks the live
// operation context inside its callback, before persisting the attempt.
func (s researchRunStore) read() (inputResearchRun, error) {
	ctx, cancel := context.WithTimeout(context.Background(), privateDataLockWait)
	defer cancel()
	return s.journal.Read(ctx)
}
func (s researchRunStore) change(change func(*inputResearchRun) error) (inputResearchRun, error) {
	ctx, cancel := context.WithTimeout(context.Background(), privateDataLockWait)
	defer cancel()
	return s.journal.Update(ctx, change)
}

func decodeResearchJournal(raw []byte, id string) (inputResearchRun, error) {
	var run inputResearchRun
	if len(raw) > inputResearchRecordLimit {
		return run, errors.New("Research storage limit reached.")
	}
	if err := decodeDataJSON(raw, &run); err != nil {
		return inputResearchRun{}, err
	}
	if err := validateResearchJournal(run, id); err != nil {
		return inputResearchRun{}, err
	}
	if run.Recipe.Target == "" {
		run.Recipe.Target = "records"
	}
	return run, nil
}
func validateResearchJournal(run inputResearchRun, id string) error {
	if run.Version != 1 || !inputResearchID.MatchString(id) || run.ID != id || !inputPreparationID.MatchString(run.PreparationID) || len(run.Records) > 50 || len(run.Attempts) > 250 || len(run.Sources) > 130 {
		return errors.New("Invalid research record.")
	}
	// Validation may normalize its argument; never mutate the retained recipe.
	recipe := run.Recipe
	if err := validateInputResearch(&recipe); err != nil {
		return err
	}
	for _, check := range []func(inputResearchRun) error{validateResearchBudgets, validateResearchApproval, validateResearchCandidates, validateResearchRecordScope, validateResearchTasks} {
		if err := check(run); err != nil {
			return err
		}
	}
	return nil
}
func encodeResearchJournal(run inputResearchRun, id string) ([]byte, error) {
	if err := validateResearchJournal(run, id); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(run)
	if err != nil {
		return nil, err
	}
	if len(raw) > inputResearchRecordLimit {
		return nil, errors.New("Research storage limit reached.")
	}
	return raw, nil
}
func changeResearchJournal(raw []byte, id string, change func(*inputResearchRun) error) ([]byte, error) {
	before, err := decodeResearchJournal(raw, id)
	if err != nil {
		return nil, err
	}
	next, err := decodeResearchJournal(raw, id)
	if err != nil {
		return nil, err
	}
	if err = change(&next); err != nil {
		return nil, err
	}
	if next.ID != before.ID || next.Version != before.Version || next.PreparationID != before.PreparationID || next.CreatedAt != before.CreatedAt || len(next.Attempts) < len(before.Attempts) {
		return nil, errors.New("Research history cannot be replaced.")
	}
	for i, old := range before.Attempts {
		a := next.Attempts[i]
		if a.ID != old.ID || a.Kind != old.Kind || a.Digest != old.Digest || a.StartedAt != old.StartedAt || a.Number != old.Number || a.RetryOf != old.RetryOf || (old.State == "completed" && (a.State != old.State || !bytes.Equal(a.Result, old.Result))) {
			return nil, errors.New("Retained research operations cannot be replaced or replayed.")
		}
	}
	next.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return encodeResearchJournal(next, id)
}
