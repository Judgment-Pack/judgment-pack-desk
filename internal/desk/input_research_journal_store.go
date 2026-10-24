package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sort"
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
	saveProof func(context.Context, string, inputResearchProof, int) (int, error)
	loadProof func(context.Context, string) (researchProofSnapshot, error)
}

func (s *Server) researchRunStore(root *os.Root, id string) researchRunStore {
	return researchRunStore{journal: s.researchJournal(root, id), proofSize: func(attempt string) (int, error) { return retainedResearchProofSize(root, attempt) }, saveProof: func(ctx context.Context, attempt string, p inputResearchProof, used int) (int, error) {
		return s.saveResearchProof(ctx, root, attempt, p, used)
	}, loadProof: func(ctx context.Context, attempt string) (researchProofSnapshot, error) {
		return s.loadResearchProof(ctx, root, attempt)
	}}
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
	run, err := s.journal.Read(ctx)
	if err == nil {
		err = s.validatePortableProofs(ctx, run)
	}
	return run, err
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
	if run.Version == 1 && run.Recipe.Target == "" {
		run.Recipe.Target = "records"
	}
	return run, nil
}
func validateResearchJournal(run inputResearchRun, id string) error {
	for _, a := range run.Attempts {
		if r := a.Background; r != nil && (a.Kind != "model" || r.Digest != a.Digest || r.Owner != run.ID+"/"+a.ID) {
			return errBackgroundReceipt
		}
	}

	if run.Version == 2 {
		return validatePortableResearchJournal(run, id)
	}
	if run.Portable != nil {
		return errors.New("Portable research requires its own journal version.")
	}
	for _, a := range run.Attempts {
		if a.RetainedProof != nil {
			return errors.New("Portable proof reference requires portable research.")
		}
	}
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
	var raw []byte
	var err error
	if run.Portable != nil {
		raw, err = portableResearchJSON(run)
	} else {
		raw, err = json.Marshal(run)
	}
	if err != nil {
		return nil, err
	}
	if len(raw) > inputResearchRecordLimit {
		return nil, errors.New("Research storage limit reached.")
	}
	// Validate the actual stored representation before any provider commits it.
	// Raw JSON escaping can otherwise turn an accepted snapshot unreadable.
	if _, err := decodeResearchJournal(raw, id); err != nil {
		return nil, err
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
	if before.Approval != nil && before.Approval.UsedAt != "" && (!reflect.DeepEqual(before.Approval, next.Approval) || next.Revision != before.Revision) {
		return nil, errors.New("The consumed research allowance receipt cannot change.")
	}
	if before.Portable != nil {
		if next.Portable == nil || next.Revision != before.Revision || next.Gateway != before.Gateway || portableResearchRevision(next.Portable.Frozen) != portableResearchRevision(before.Portable.Frozen) || len(before.Portable.Response) > 0 && !bytes.Equal(before.Portable.Response, next.Portable.Response) {
			return nil, errors.New("The reviewed portable research contract and retained result cannot change.")
		}
	}
	for i, old := range before.Attempts {
		a := next.Attempts[i]
		if !backgroundReceiptTransition(old, a) {
			return nil, errBackgroundReceipt
		}
		if a.ID != old.ID || a.Kind != old.Kind || a.Digest != old.Digest || a.StartedAt != old.StartedAt || a.Number != old.Number || a.RetryOf != old.RetryOf || ((old.State == "completed" || old.State == "refused") && (a.State != old.State || !bytes.Equal(a.Result, old.Result) || !reflect.DeepEqual(a.RetainedProof, old.RetainedProof))) {
			return nil, errors.New("Retained research operations cannot be replaced or replayed.")
		}
	}
	next.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return encodeResearchJournal(next, id)
}

// readInputResearchSummaries is observation only: it never recovers or advances
// a worker. An injected content owner must also supply its discovery contract.
func (s *Server) readInputResearchSummaries(ctx context.Context, preparation string) ([]inputResearchSummary, error) {
	if s.researchJournals != nil {
		owner, ok := s.researchJournals.(interface {
			Summaries(context.Context, string) ([]inputResearchSummary, error)
		})
		if !ok {
			return nil, errors.New("Research history discovery is unavailable.")
		}
		return owner.Summaries(ctx, preparation)
	}
	root, err := s.inputResearchRoot(false)
	if errors.Is(err, os.ErrNotExist) {
		return []inputResearchSummary{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	dir, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	out := []inputResearchSummary{}
	for _, entry := range entries {
		if !inputResearchID.MatchString(entry.Name()) {
			continue
		}
		rr, err := openInputResearch(root, entry.Name(), false)
		if err != nil {
			return nil, err
		}
		run, err := s.researchJournal(rr, entry.Name()).Read(ctx)
		rr.Close()
		if err != nil {
			return nil, err
		}
		if run.PreparationID == preparation {
			out = append(out, researchSummary(run))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt == out[j].CreatedAt {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt > out[j].CreatedAt
	})
	return out, ctx.Err()
}
