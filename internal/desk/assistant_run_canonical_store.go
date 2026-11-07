package desk

// Host-scoped canonical persistence in the existing private run transaction.
// Desk's physical operation receipts remain separate from Agent observations.
import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math"
	"reflect"
	"sync"
	"unicode"
	"unicode/utf8"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

const assistantCanonicalCommitLimit = 4096

type assistantCanonicalHead struct {
	RunID      string `json:"runId"`
	Revision   string `json:"revision"`
	Cursor     string `json:"cursor"`
	CommitID   string `json:"commitId"`
	Checkpoint []byte `json:"checkpoint,omitempty"`
}
type assistantCanonicalEvent struct {
	Cursor string `json:"cursor"`
	Bytes  []byte `json:"bytes"`
}
type assistantCanonicalCommit struct {
	ID               string `json:"id"`
	ExpectedRevision string `json:"expectedRevision"`
	// Fingerprint hashes a length-framed exact transaction, including checkpoint
	// presence and event order. It is not a parsed/normalized JSON digest.
	Fingerprint string `json:"fingerprint"`
	Revision    string `json:"revision"`
	Cursor      string `json:"cursor"`
}
type assistantCanonicalJournal struct {
	Version int                        `json:"version"`
	Active  bool                       `json:"active,omitempty"`
	Head    assistantCanonicalHead     `json:"head"`
	Events  []assistantCanonicalEvent  `json:"events"`
	Commits []assistantCanonicalCommit `json:"commits"`
}

func assistantCanonicalActive(run assistantRunRecord) bool {
	return run.CanonicalRequired && run.Canonical != nil && run.Canonical.Active
}

// Package-private host construction selects the persistence profile. Neither
// a browser request nor a retained checkpoint can select or downgrade it.
type assistantCanonicalExecutor interface{ assistantCanonicalRuns() }

func (*assistantProductionExecutor) assistantCanonicalRuns() {}
func (*assistantCreationExecutor) assistantCanonicalRuns()   {}
func (*assistantAuthoringExecutor) assistantCanonicalRuns()  {}
func (*assistantBriefExecutor) assistantCanonicalRuns()      {}
func (*assistantDiagnosticExecutor) assistantCanonicalRuns() {}

func assistantCanonicalAdmission(executor assistantRunExecutor, _ assistantRunSource) (bool, error) {
	_, canonical := executor.(assistantCanonicalExecutor)
	return canonical, nil
}

func (e *assistantRunExecution) bindCanonicalInput(ctx context.Context, input *runcontext.Input) error {
	if err := e.bindContextInput(input); err != nil {
		return err
	}
	if e.record.Observation != nil {
		return &runcontext.ContinuationNotRecoverable{}
	}
	if e.record.CanonicalRequired && !assistantCanonicalActive(e.record) {
		return &runcontext.ContinuationNotRecoverable{}
	}
	if !assistantCanonicalActive(e.record) {
		if e.record.Checkpoint != nil && e.record.Checkpoint.Durable != nil {
			return &runcontext.ContinuationNotRecoverable{}
		}
		if e.record.Canonical != nil {
			value, err := runcontext.ParseJSON(e.record.Canonical.Head.Checkpoint)
			if err == nil && value.Get("durable") != nil {
				return &runcontext.ContinuationNotRecoverable{}
			}
		}
		return nil
	}
	if e.record.Checkpoint == nil || validateAssistantCanonicalProjection(e.record) != nil {
		return &runcontext.ContinuationNotRecoverable{}
	}
	e.canonicalMu.Lock()
	defer e.canonicalMu.Unlock()
	if input.Persistence != nil && input.Persistence != e.canonicalBinding {
		return &runcontext.ContinuationIdentityChanged{}
	}
	if e.canonicalBinding == nil {
		store := e.canonicalStore
		var err error
		if store == nil {
			store, err = e.writer.canonicalStore(ctx, e.record.Canonical.Head.RunID)
		}
		if err != nil {
			return err
		}
		latest, err := store.read(ctx)
		if err != nil {
			return err
		}
		if !assistantCanonicalActive(latest) || validateAssistantCanonicalProjection(latest) != nil {
			return &runcontext.ContinuationNotRecoverable{}
		}
		prepared, err := runcontext.PrepareDurableInput(ctx, &runcontext.Input{
			Chat:      input.Chat,
			Runtime:   input.Runtime,
			Execution: latest.Checkpoint,
			RunStore:  &runcontext.DurableRunInput{Store: store, ID: e.record.Canonical.Head.RunID, Resume: true},
		})
		if err != nil {
			return err
		}
		if prepared == nil || prepared.Persistence == nil {
			return &runcontext.ContinuationNotRecoverable{}
		}
		e.canonicalStore = store
		store.owner, store.generation, store.accept = e, e.generation, true
		e.canonicalBinding = prepared.Persistence
		if e.stopRequested {
			if err := e.canonicalBinding.Writer.CloseDispatch(*e.canonicalBinding); err != nil {
				return err
			}
		}
	}
	e.canonicalInput = input
	binding := e.canonicalBinding
	generation := e.generation
	input.Execution = binding.Writer.Checkpoint()
	input.Persistence = binding
	input.RunStore = &runcontext.DurableRunInput{Store: binding.Writer.Store, ID: binding.Writer.ID, Resume: true}
	input.Checkpoint = func(ctx context.Context, cp contract.Checkpoint) error {
		return binding.Writer.Save(e.generationContext(ctx, generation), *binding, cp)
	}
	return nil
}

// A closed host workflow may finish/reopen its admitted model-free work after
// an adapter returns. Publish a new CAS head; never edit the acknowledged head.
func (e *assistantRunExecution) canonicalHostStatus(ctx context.Context, status string) error {
	if status != "running" && status != "complete" {
		return errAssistantRunScope
	}
	creation, err := compileAssistantCreationInput(e.source)
	if err != nil || creation == nil || status == "running" && !contains([]string{"establish-cases", "propose-correction"}, creation.Action.Kind) {
		return errAssistantRunScope
	}
	err = e.writer.change(ctx, false, func(run *assistantRunRecord) (bool, error) {
		if !assistantCanonicalActive(*run) || status == "complete" && run.Creation == nil || run.State != "running" && run.State != "admitted" || run.Checkpoint == nil || run.Checkpoint.Approval != nil && run.Checkpoint.Approval.State != "answered" || run.Checkpoint.Handoff != nil && run.Checkpoint.Handoff.State != "continued" || run.Checkpoint.Durable != nil && run.Checkpoint.Durable.PendingHandoff != nil {
			return false, errAssistantRunDispatch
		}
		if assistantCheckpointHasPendingPlan(run.Checkpoint) {
			return false, errAssistantRunConflict
		}
		if status == "running" && run.Checkpoint.Status != "complete" || status == "complete" && run.Checkpoint.Status != "running" {
			return false, errAssistantRunConflict
		}
		for _, op := range run.Operations {
			if op.State != "completed" && op.State != "refused" {
				return false, errAssistantRunDispatch
			}
		}
		if pending, err := pendingAssistantRunQuestion(*run); err != nil || pending != nil {
			return false, errAssistantRunDispatch
		}
		cp, _, err := runcontext.MutateCheckpoint(*run.Checkpoint, runcontext.CheckpointPatch{"Status": status})
		if err != nil {
			return false, err
		}
		if cp.Digest != nil {
			raw, _ := json.Marshal(cp)
			value, err := runcontext.ParseJSON(raw)
			if err != nil {
				return false, err
			}
			digest := runcontext.RecordDigest(value)
			cp.Digest = &digest
		}
		raw, err := json.Marshal(cp)
		if err != nil {
			return false, err
		}
		input, err := freezeAssistantCanonicalCommit(runcontext.RunCommit{RunID: run.Canonical.Head.RunID, ExpectedRevision: run.Canonical.Head.Revision, CommitID: rand.Text(), Checkpoint: raw, Events: [][]byte{}})
		if err == nil {
			_, err = commitAssistantCanonical(run, input, "")
		}
		return err == nil, err
	})
	if err == nil {
		// An adapter that has returned no longer owns the canonical revision.
		e.canonicalMu.Lock()
		e.canonicalBinding = nil
		e.canonicalMu.Unlock()
	}
	e.changed(err)
	return err
}

// Initialization is part of explicit new admission, never a read or recovery.
// The prepared checkpoint is the sole legacy input; Agent creates its own book
// on the first supported opening. Historical receipts never become observations.
func (w *assistantRunWriter) initializeCanonical(ctx context.Context, state contract.Checkpoint) error {
	return w.change(ctx, false, func(run *assistantRunRecord) (bool, error) {
		return initializeAssistantCanonical(run, state)
	})
}

func initializeAssistantCanonical(run *assistantRunRecord, state contract.Checkpoint) (bool, error) {
	if run.Canonical != nil || run.Checkpoint != nil || run.Historical || run.State != "admitted" || state.Durable != nil {
		return false, errAssistantRunConflict
	}
	cp, err := readAssistantRunCheckpoint(state, run.Binding)
	if err != nil {
		return false, err
	}
	raw, err := json.Marshal(cp)
	if err != nil {
		return false, err
	}
	run.Checkpoint = cp
	run.CanonicalRequired = true
	run.Canonical = &assistantCanonicalJournal{Version: 1, Active: true, Head: assistantCanonicalHead{RunID: cp.ID}, Events: []assistantCanonicalEvent{}, Commits: []assistantCanonicalCommit{}}
	input, err := freezeAssistantCanonicalCommit(runcontext.RunCommit{RunID: cp.ID, CommitID: rand.Text(), Checkpoint: raw, Events: [][]byte{}})
	if err == nil {
		_, err = commitAssistantCanonical(run, input, "")
	}
	return err == nil, err
}

// This helper is used only within the existing private transaction. Human
// continuations use it in the same replacement as the host action receipt.
func commitAssistantCanonical(run *assistantRunRecord, input runcontext.RunCommit, fingerprint string) (runcontext.RunReceipt, error) {
	journal := run.Canonical
	if journal == nil {
		journal = &assistantCanonicalJournal{Version: 1, Head: assistantCanonicalHead{RunID: input.RunID}, Events: []assistantCanonicalEvent{}, Commits: []assistantCanonicalCommit{}}
	}
	if journal.Head.RunID != input.RunID || journal.Head.Revision != input.ExpectedRevision {
		return runcontext.RunReceipt{}, &runcontext.StorageConflict{ExpectedRevision: input.ExpectedRevision, ActualRevision: journal.Head.Revision}
	}
	if len(journal.Commits) >= assistantCanonicalCommitLimit || len(journal.Events)+len(input.Events) > assistantRunEventLimit {
		return runcontext.RunReceipt{}, &runcontext.StorageInputError{}
	}
	if fingerprint == "" {
		fingerprint = assistantCanonicalFingerprint(input)
	}
	cursor := journal.Head.Cursor
	for _, event := range input.Events {
		cursor = rand.Text()
		journal.Events = append(journal.Events, assistantCanonicalEvent{Cursor: cursor, Bytes: event})
		if journal.Active {
			if err := projectAssistantCanonicalEvent(run, input.RunID, cursor, event); err != nil {
				return runcontext.RunReceipt{}, err
			}
		}
	}
	if input.Checkpoint != nil {
		if journal.Active {
			value, err := runcontext.ParseJSON(input.Checkpoint)
			if err != nil {
				return runcontext.RunReceipt{}, err
			}
			cp, err := runcontext.ReadExecution(value)
			if err != nil || cp == nil || cp.ID != input.RunID {
				return runcontext.RunReceipt{}, errAssistantRunScope
			}
			cp, err = readAssistantRunCheckpoint(*cp, run.Binding)
			if err != nil || run.Checkpoint != nil && (cp.ID != run.Checkpoint.ID || cp.Used < run.Checkpoint.Used) {
				return runcontext.RunReceipt{}, errAssistantRunConflict
			}
			run.Checkpoint = cp
			if run.State == "admitted" {
				run.State = "running"
			}
		}
		journal.Head.Checkpoint = input.Checkpoint
	}
	receipt := runcontext.RunReceipt{Revision: rand.Text(), Cursor: cursor, CommitID: input.CommitID}
	journal.Head.Revision, journal.Head.Cursor, journal.Head.CommitID = receipt.Revision, cursor, input.CommitID
	journal.Commits = append(journal.Commits, assistantCanonicalCommit{ID: input.CommitID, ExpectedRevision: input.ExpectedRevision, Fingerprint: fingerprint, Revision: receipt.Revision, Cursor: cursor})
	run.Canonical = journal
	return receipt, nil
}

func projectAssistantCanonicalEvent(run *assistantRunRecord, root, cursor string, raw []byte) error {
	var envelope runcontext.StoredRunEnvelope
	if json.Unmarshal(raw, &envelope) != nil || envelope.Version != 1 || envelope.Scope.Execution == "" || envelope.Scope.Role == "" {
		return errAssistantRunInvalid
	}
	event := envelope.Event
	// Children publish their task result through the coordinator, not private
	// questions/proposals/messages. Handoffs and intermediate invocation ends
	// are also coordinator-internal. The outer lifecycle publishes its final end.
	if envelope.Scope.Execution != root || envelope.Scope.Task != "" || event.DelegationRequested != nil || event.End != nil || event.ExecutionCheckpoint != nil || event.BudgetPaused != nil || event.MessageProgress != nil {
		return nil
	}
	_, err := appendAssistantRunEvent(run, cursor, event)
	return err
}

func validateAssistantCanonicalProjection(run assistantRunRecord) error {
	if err := validateAssistantObservationRecord(run); err != nil {
		return err
	}
	if run.Observation != nil && run.Canonical == nil {
		return nil // Strict prepared observation; no canonical head exists yet.
	}
	if run.CanonicalRequired != (run.Canonical != nil && run.Canonical.Active) {
		return errAssistantRunInvalid
	}
	if !assistantCanonicalActive(run) {
		return nil
	}
	value, err := runcontext.ParseJSON(run.Canonical.Head.Checkpoint)
	if err != nil {
		return err
	}
	cp, err := runcontext.ReadExecution(value)
	if err != nil || cp == nil || cp.ID != run.Canonical.Head.RunID || !sameAssistantRunCheckpoint(cp, run.Checkpoint) {
		return errAssistantRunInvalid
	}
	canonical := map[string]contract.RunEvent{}
	for _, item := range run.Canonical.Events {
		var envelope runcontext.StoredRunEnvelope
		if json.Unmarshal(item.Bytes, &envelope) != nil || envelope.Version != 1 {
			return errAssistantRunInvalid
		}
		if envelope.Scope.Execution == run.Canonical.Head.RunID && envelope.Scope.Task == "" {
			canonical[item.Cursor] = envelope.Event
		}
	}
	for _, public := range run.Events {
		event, ok := canonical[public.ID]
		left, _ := json.Marshal(event)
		right, _ := json.Marshal(public.Event)
		if !ok || !bytes.Equal(left, right) {
			return errAssistantRunInvalid
		}
	}
	return nil
}

// An inner invocation's end is not the response's end. Publish only the latest
// acknowledged root end when the owning host closes or pauses the response.
func finishAssistantCanonicalEvents(run *assistantRunRecord) error {
	if !assistantCanonicalActive(*run) {
		return nil
	}
	for i := len(run.Canonical.Events) - 1; i >= 0; i-- {
		item := run.Canonical.Events[i]
		var envelope runcontext.StoredRunEnvelope
		if json.Unmarshal(item.Bytes, &envelope) != nil {
			return errAssistantRunInvalid
		}
		if envelope.Scope.Execution == run.Canonical.Head.RunID && envelope.Scope.Task == "" && envelope.Event.End != nil {
			for _, event := range run.Events {
				if event.ID == item.Cursor {
					return nil
				}
			}
			// The response was just closed; appendAssistantRunEvent's terminal
			// admission check does not apply to this acknowledged delivery.
			if len(run.Events) >= assistantRunEventLimit {
				return &runcontext.StorageInputError{}
			}
			run.Events = append(run.Events, assistantRunEvent{ID: item.Cursor, Event: envelope.Event})
			return nil
		}
	}
	return nil
}

type assistantCanonicalRunStore struct {
	// Private immutable admission view; exists only while the enclosing ledger
	// replacement is unacknowledged. No write is accepted in this state.
	prepared    *assistantRunRecord
	ingress     sync.Mutex // generation fence; held through each Commit
	owner       *assistantRunExecution
	generation  uint64
	accept      bool
	writer      *assistantRunWriter
	id          string
	observation *assistantObservationClaim
}

var _ runcontext.RunStore = (*assistantCanonicalRunStore)(nil)

// The caller has already admitted the outer run. Only host code selects id;
// there is no registry, HTTP creation surface or dispatch capability here.
func (w *assistantRunWriter) canonicalStore(ctx context.Context, id string) (*assistantCanonicalRunStore, error) {
	if w == nil || w.lease == nil || !assistantCanonicalID(id) {
		return nil, errAssistantRunScope
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lease.mu.Lock()
	defer w.lease.mu.Unlock()
	if w.lease.file == nil || w.lease.recoveryErr != nil || w.failure != nil {
		return nil, errAssistantRunScope
	}
	run, err := w.lease.store.Read(ctx, w.binding, w.id)
	if err != nil {
		return nil, err
	}
	if run.Observation != nil || run.Historical || assistantRunTerminal(run.State) || run.Checkpoint == nil || run.Canonical != nil && run.Canonical.Head.RunID != id {
		return nil, errAssistantRunScope
	}
	return &assistantCanonicalRunStore{writer: w, id: id}, nil
}
func (*assistantCanonicalRunStore) Durability() string { return runcontext.RunStoreHost }
func assistantCanonicalID(value string) bool {
	if len(value) == 0 || len(value) > 256 || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func assistantCanonicalBytes(value []byte) bool {
	return len(value) > 0 && len(value) <= runcontext.StorageRecordBytes && utf8.Valid(value)
}
func freezeAssistantCanonicalCommit(input runcontext.RunCommit) (runcontext.RunCommit, error) {
	if !assistantCanonicalID(input.RunID) || !assistantCanonicalID(input.CommitID) || input.ExpectedRevision != "" && !assistantCanonicalID(input.ExpectedRevision) || len(input.Events) > runcontext.StorageEventLimit || input.Checkpoint == nil && len(input.Events) == 0 {
		return runcontext.RunCommit{}, &runcontext.StorageInputError{}
	}
	out := input
	total := 0
	if input.Checkpoint != nil {
		if !assistantCanonicalBytes(input.Checkpoint) {
			return runcontext.RunCommit{}, &runcontext.StorageInputError{}
		}
		out.Checkpoint = bytes.Clone(input.Checkpoint)
		total = len(input.Checkpoint)
	}
	out.Events = make([][]byte, len(input.Events))
	for i, event := range input.Events {
		if !assistantCanonicalBytes(event) {
			return runcontext.RunCommit{}, &runcontext.StorageInputError{}
		}
		total += len(event)
		if total > runcontext.StorageTransactionBytes {
			return runcontext.RunCommit{}, &runcontext.StorageInputError{}
		}
		out.Events[i] = bytes.Clone(event)
	}
	return out, nil
}
func assistantCanonicalFingerprint(input runcontext.RunCommit) string {
	h := sha256.New()
	field := func(value []byte) {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(value)))
		h.Write(size[:])
		h.Write(value)
	}
	field([]byte(input.RunID))
	field([]byte(input.ExpectedRevision))
	if input.Checkpoint == nil {
		field([]byte{0})
	} else {
		field([]byte{1})
		field(input.Checkpoint)
	}
	var count [8]byte
	binary.BigEndian.PutUint64(count[:], uint64(len(input.Events)))
	h.Write(count[:])
	for _, event := range input.Events {
		field(event)
	}
	return hex.EncodeToString(h.Sum(nil))
}
func (s *assistantCanonicalRunStore) read(ctx context.Context) (assistantRunRecord, error) {
	// Reads deliberately need no executor lease and never recover or initialize.
	if s.prepared != nil {
		if err := ctx.Err(); err != nil {
			return assistantRunRecord{}, err
		}
		return *s.prepared, nil
	}
	return s.writer.lease.store.Read(ctx, s.writer.binding, s.writer.id)
}
func (s *assistantCanonicalRunStore) Load(ctx context.Context, id string) (*runcontext.RunHead, error) {
	if !assistantCanonicalID(id) {
		return nil, &runcontext.StorageInputError{}
	}
	if s.observation != nil && id != s.id {
		return nil, &runcontext.ContinuationIdentityChanged{}
	}
	run, err := s.read(ctx)
	if err != nil {
		return nil, err
	}
	if run.Observation != nil {
		if s.observation == nil || s.observation.execution.observation != s.observation || !reflect.DeepEqual(run.Observation, &s.observation.custody) || !reflect.DeepEqual(run.Binding, s.observation.execution.source.Binding) || validateAssistantObservationRecord(run) != nil || run.Canonical == nil && (run.Historical || run.State != "admitted") {
			return nil, &runcontext.ContinuationNotRecoverable{}
		}
	}
	if id != s.id || run.Canonical == nil {
		return nil, nil
	}
	if run.Canonical.Head.RunID != s.id {
		return nil, errAssistantRunScope
	}
	head := run.Canonical.Head
	return &runcontext.RunHead{RunID: head.RunID, Checkpoint: bytes.Clone(head.Checkpoint), RunReceipt: runcontext.RunReceipt{Revision: head.Revision, Cursor: head.Cursor, CommitID: head.CommitID}}, nil
}
func (s *assistantCanonicalRunStore) Commit(ctx context.Context, input runcontext.RunCommit) (runcontext.RunReceipt, error) {
	var receipt runcontext.RunReceipt
	if s.prepared != nil {
		return receipt, errAssistantRunDispatch
	}
	frozen, err := freezeAssistantCanonicalCommit(input)
	if err != nil {
		return receipt, err
	}
	if frozen.RunID != s.id {
		return receipt, errAssistantRunScope
	}
	fingerprint := assistantCanonicalFingerprint(frozen)
	// Agent holds its writer mutex here. Cancellation only uses lock-free
	// CloseDispatch while holding canonicalMu; Attach snapshots before taking it.
	if s.observation != nil {
		s.observation.execution.canonicalMu.Lock()
		defer s.observation.execution.canonicalMu.Unlock()
	}
	s.ingress.Lock()
	defer s.ingress.Unlock()
	if s.owner != nil && s.generation != 0 {
		g, ok := ctx.Value(assistantInvocationGenerationKey{}).(assistantInvocationGeneration)
		if !ok || g.owner != s.owner || g.number != s.generation || !s.accept && !g.finalizer {
			return receipt, errAssistantRunScope
		}
	}
	w := s.writer
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lease.mu.Lock()
	defer w.lease.mu.Unlock()
	if w.lease.file == nil || w.lease.recoveryErr != nil {
		return receipt, errAssistantRunScope
	}
	ledger, err := w.lease.store.transaction(ctx, func(ledger *assistantRunLedger) (bool, error) {
		run, e := assistantRunBound(ledger, w.binding, w.id)
		if e != nil {
			return false, e
		}
		if (run.Observation != nil) != (s.observation != nil) {
			return false, &runcontext.ContinuationIdentityChanged{}
		}
		if s.observation != nil && (!reflect.DeepEqual(run.Observation, &s.observation.custody) || !reflect.DeepEqual(run.Binding, s.observation.execution.source.Binding)) {
			return false, &runcontext.ContinuationIdentityChanged{}
		}
		journal := run.Canonical
		if journal != nil {
			if journal.Head.RunID != s.id {
				return false, errAssistantRunScope
			}
			for _, commit := range journal.Commits {
				if commit.ID == frozen.CommitID {
					if commit.Fingerprint != fingerprint {
						return false, &runcontext.StorageCommitMismatch{}
					}
					receipt = runcontext.RunReceipt{Revision: commit.Revision, Cursor: commit.Cursor, CommitID: commit.ID}
					return false, nil
				}
			}
		}
		actual := ""
		if journal != nil {
			actual = journal.Head.Revision
		}
		if actual != frozen.ExpectedRevision {
			return false, &runcontext.StorageConflict{ExpectedRevision: frozen.ExpectedRevision, ActualRevision: actual}
		}
		if run.Historical || assistantRunTerminal(run.State) || run.Checkpoint == nil {
			return false, errAssistantRunDispatch
		}
		if w.failure != nil {
			return false, w.failure
		}
		if run.Revision != w.revision {
			return false, errAssistantRunConflict
		}
		if run.Revision == math.MaxUint64 {
			return false, &runcontext.StorageInputError{}
		}
		if s.observation != nil {
			if e = s.observation.validateCommit(run, frozen); e != nil {
				return false, e
			}
		}
		receipt, e = commitAssistantCanonical(run, frozen, fingerprint)
		if e != nil {
			return false, e
		}
		run.Revision++
		return true, nil
	})
	// No port failure poisons the shared data object. The canonical Agent writer
	// will latch execution uncertainty when it is activated in a later slice.
	if err != nil {
		return runcontext.RunReceipt{}, err
	}
	for _, run := range ledger.Runs {
		if run.ID == w.id {
			w.revision = run.Revision
			return receipt, nil
		}
	}
	return runcontext.RunReceipt{}, errAssistantRunInvalid
}
func (s *assistantCanonicalRunStore) ReadEvents(ctx context.Context, id, after string, limit int) (runcontext.RunEventPage, error) {
	page := runcontext.RunEventPage{Items: []runcontext.StoredRunEvent{}, NextCursor: after}
	if !assistantCanonicalID(id) || limit < 1 || limit > runcontext.StoragePageLimit {
		return page, &runcontext.StorageInputError{}
	}
	if after != "" && !assistantCanonicalID(after) {
		return page, &runcontext.StorageCursorInvalid{}
	}
	run, err := s.read(ctx)
	if err != nil {
		return page, err
	}
	events := []assistantCanonicalEvent{}
	if id == s.id && run.Canonical != nil {
		if run.Canonical.Head.RunID != s.id {
			return page, errAssistantRunScope
		}
		events = run.Canonical.Events
	}
	start := 0
	if after != "" {
		start = -1
		for i, event := range events {
			if event.Cursor == after {
				start = i + 1
				break
			}
		}
		if start < 0 {
			return page, &runcontext.StorageCursorInvalid{}
		}
	}
	total, index := 0, start
	for ; index < len(events) && len(page.Items) < limit; index++ {
		event := events[index]
		if total+len(event.Bytes) > runcontext.StorageTransactionBytes {
			break
		}
		page.Items = append(page.Items, runcontext.StoredRunEvent{Cursor: event.Cursor, Bytes: bytes.Clone(event.Bytes)})
		total += len(event.Bytes)
		page.NextCursor = event.Cursor
	}
	page.HasMore = index < len(events)
	return page, nil
}
func validAssistantCanonicalJournal(journal *assistantCanonicalJournal) bool {
	if journal == nil {
		return true
	}
	head := journal.Head
	if journal.Version != 1 || !assistantCanonicalID(head.RunID) || !assistantCanonicalID(head.Revision) || !assistantCanonicalID(head.CommitID) || head.Checkpoint != nil && !assistantCanonicalBytes(head.Checkpoint) || journal.Events == nil || len(journal.Events) > assistantRunEventLimit || journal.Commits == nil || len(journal.Commits) == 0 || len(journal.Commits) > assistantCanonicalCommitLimit {
		return false
	}
	cursors := map[string]int{"": 0}
	for i, event := range journal.Events {
		if !assistantCanonicalID(event.Cursor) || !assistantCanonicalBytes(event.Bytes) {
			return false
		}
		if _, seen := cursors[event.Cursor]; seen {
			return false
		}
		cursors[event.Cursor] = i + 1
	}
	at, found := cursors[head.Cursor]
	if !found || at != len(journal.Events) {
		return false
	}
	ids, revisions := map[string]bool{}, map[string]bool{}
	previous, cursor := "", 0
	for _, commit := range journal.Commits {
		at, found := cursors[commit.Cursor]
		if !assistantCanonicalID(commit.ID) || ids[commit.ID] || !assistantRunDigest(commit.Fingerprint) || commit.ExpectedRevision != previous || !assistantCanonicalID(commit.Revision) || revisions[commit.Revision] || !found || at < cursor {
			return false
		}
		ids[commit.ID] = true
		revisions[commit.Revision] = true
		previous = commit.Revision
		cursor = at
	}
	last := journal.Commits[len(journal.Commits)-1]
	return last.ID == head.CommitID && last.Revision == head.Revision && last.Cursor == head.Cursor
}
