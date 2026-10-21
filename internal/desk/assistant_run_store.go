package desk

// This is a durable host ledger, not an execution engine. No method dispatches
// a model, Runtime call or action. Callers must resolve authorization and saved
// request/resource bindings before admission; generated history is never authority.
import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

const assistantRunRecordLimit = maxConversationBytes
const assistantRunCountLimit = 128
const assistantRunOperationLimit = 512
const assistantRunEventLimit = 1024

var (
	errAssistantRunScope    = errors.New("Assistant run ownership or immutable request binding changed.")
	errAssistantRunConflict = errors.New("Assistant run revision or idempotency delivery conflicts with saved work.")
	errAssistantRunDispatch = errors.New("This operation cannot be dispatched; inspect its retained receipt instead of replaying it.")
	errAssistantRunInvalid  = errors.New("Invalid assistant run ledger record.")
)

type assistantRunOwner struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Revision string `json:"revision"`
}

// Principal, Session and Policy are host-authored opaque digests, never raw
// bearer tokens. Project is the selected Server's conversation identity digest.
// ExecutionKey is the canonical framework request key for the current request.
// SourceDigests identify exact retained originals, not permission-bearing prose.
type assistantRunBinding struct {
	Principal          string            `json:"principal"`
	Session            string            `json:"session"`
	Policy             string            `json:"policy"`
	Project            string            `json:"project"`
	Owner              assistantRunOwner `json:"owner"`
	RequestID          string            `json:"requestId"`
	RequestDigest      string            `json:"requestDigest"`
	SourceDigests      []string          `json:"sourceDigests"`
	ConnectionID       string            `json:"connectionId"`
	ConnectionRevision string            `json:"connectionRevision"`
	ProfileRevision    string            `json:"profileRevision"`
	Model              string            `json:"model"`
	Phase              string            `json:"phase"`
	Unit               string            `json:"unit"`
	ExecutionKey       string            `json:"executionKey"`
}

type assistantRunOperation struct {
	ID     string `json:"id"`
	CallID string `json:"callId"`
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	// Base64 JSON storage preserves exact bytes (including formatting and missing
	// versus null), rather than reserializing a RawMessage during each checkpoint.
	Intent []byte `json:"intent"`
	State  string `json:"state"`
	Result []byte `json:"result"`
}
type assistantRunEvent struct {
	ID    string            `json:"id"`
	Event contract.RunEvent `json:"event"`
}
type assistantRunRecord struct {
	Websites        []assistantWebsiteCustody   `json:"websites,omitempty"`
	Links           []assistantLinkCustody      `json:"links,omitempty"`
	Searches        []assistantSearchCustody    `json:"searches,omitempty"`
	DomainOutputs   []assistantDomainOutput     `json:"domainOutputs,omitempty"`
	ChatProjection  *assistantChatProjection    `json:"chatProjection,omitempty"`
	ProjectionIssue string                      `json:"projectionIssue,omitempty"`
	ID              string                      `json:"id"`
	Revision        uint64                      `json:"revision"`
	Binding         assistantRunBinding         `json:"binding"`
	AdmissionID     string                      `json:"admissionId"`
	BodyDigest      string                      `json:"bodyDigest"`
	State           string                      `json:"state"`
	Historical      bool                        `json:"historical,omitempty"`
	Checkpoint      *contract.Checkpoint        `json:"checkpoint,omitempty"`
	Operations      []assistantRunOperation     `json:"operations"`
	Events          []assistantRunEvent         `json:"events"`
	Actions         []assistantRunActionReceipt `json:"actions,omitempty"`
}
type assistantRunLedger struct {
	Version int                  `json:"version"`
	Project string               `json:"project"`
	Runs    []assistantRunRecord `json:"runs"`
}

type assistantRunStore struct {
	server        *Server
	project, name string
	problem       error
}

func (s *Server) assistantRuns() assistantRunStore {
	held := assistantRunStore{server: s}
	lock, err := s.privateDataLockContext(context.Background(), true)
	if err != nil {
		held.problem = err
		return held
	}
	defer lock.Close()
	store, err := s.openChatData()
	if err != nil {
		held.problem = err
		return held
	}
	defer store.root.Close()
	held.project, held.problem = assistantRunNamespace(store.root, s.conversationIdentity())
	held.name = "assistant-runs-" + held.project + ".json"
	return held
}

// transaction never retries its callback. The per-project file contains both
// records and their idempotency keys, so admission needs no multi-file commit.
func (s assistantRunStore) transaction(ctx context.Context, change func(*assistantRunLedger) (bool, error)) (assistantRunLedger, error) {
	var empty assistantRunLedger
	if s.problem != nil {
		return empty, s.problem
	}
	if s.server == nil {
		return empty, errAssistantRunScope
	}
	lock, err := s.server.privateDataLockContext(ctx, true)
	if err != nil {
		return empty, err
	}
	defer lock.Close()
	store, err := s.server.openChatData()
	if err != nil {
		return empty, err
	}
	defer store.root.Close()
	if err = checkPrivateDataPath(store); err != nil {
		return empty, err
	}
	if err = s.checkScope(store.root); err != nil {
		return empty, err
	}
	raw, err := readPrivateData(store.root, s.name, assistantRunRecordLimit)
	ledger := assistantRunLedger{Version: 1, Project: s.project, Runs: []assistantRunRecord{}}
	if err == nil {
		if ledger, err = decodeAssistantRunStorage(s.name, raw); err != nil {
			return empty, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return empty, err
	}
	if err = validateAssistantRunLedger(ledger, s.project); err != nil {
		return empty, err
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	changed, err := change(&ledger)
	if err != nil {
		return empty, err
	}
	if !changed {
		return ledger, nil
	}
	if err = validateAssistantRunLedger(ledger, s.project); err != nil {
		return empty, err
	}
	raw, err = json.Marshal(ledger)
	if err != nil {
		return empty, err
	}
	if len(raw) > assistantRunRecordLimit {
		return empty, withCode(CodeTooLarge, errors.New("Assistant run storage limit reached; retained work was not discarded."))
	}
	if err = assistantRunJSONMembers(raw); err != nil {
		return empty, err
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	if err = checkPrivateDataPath(store); err != nil {
		return empty, err
	}
	if err = writePrivateData(store.root, s.name, raw); err != nil {
		return empty, err
	}
	// Successful calls return independent, validated snapshots, never aliases to
	// caller-owned checkpoint maps or byte slices. No snapshot on uncertain write.
	var saved assistantRunLedger
	if err = decodeDataJSON(raw, &saved); err != nil {
		return empty, err
	}
	return saved, nil
}

// AdmissionID is stable within a project/principal across lost responses and
// session changes. Reusing it with another session/owner/request refuses rather
// than accidentally starting a second operation. Fresh attempts need fresh IDs.
// freshCAS runs only for new admission, under the same private-data lock as the
// saved owner. It must only read/validate current state; no dispatch or writes.
func (l *assistantRunLease) Admit(ctx context.Context, binding assistantRunBinding, admissionID, bodyDigest string, freshCAS func() error) (assistantRunRecord, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil || l.recoveryErr != nil {
		return assistantRunRecord{}, false, errAssistantRunRecovery
	}
	s := l.store
	var out assistantRunRecord
	if s.problem != nil {
		return out, false, s.problem
	}
	if s.server.assistantRunRecoveryFailure() != nil {
		return out, false, errAssistantRunRecovery
	}
	if err := validateAssistantRunBinding(binding, s.project); err != nil {
		return out, false, err
	}
	if !assistantRunText(admissionID, 128) || !assistantRunDigest(bodyDigest) || freshCAS == nil {
		return out, false, errAssistantRunInvalid
	}
	created := false
	id := ""
	ledger, err := s.transaction(ctx, func(ledger *assistantRunLedger) (bool, error) {
		for _, run := range ledger.Runs {
			if run.Binding.Principal == binding.Principal && run.AdmissionID == admissionID {
				if !reflect.DeepEqual(run.Binding, binding) || run.BodyDigest != bodyDigest {
					return false, errAssistantRunConflict
				}
				id = run.ID
				return false, nil
			}
		}
		for _, run := range ledger.Runs {
			if run.Binding.Owner.Kind == binding.Owner.Kind && run.Binding.Owner.ID == binding.Owner.ID && !assistantRunTerminal(run.State) {
				return false, errAssistantRunConflict
			}
		}
		if len(ledger.Runs) >= assistantRunCountLimit {
			return false, withCode(CodeTooLarge, errors.New("Assistant run retention is full."))
		}
		if err := freshCAS(); err != nil {
			return false, err
		}
		var token [16]byte
		if _, err := rand.Read(token[:]); err != nil {
			return false, err
		}
		id = "arun_" + hex.EncodeToString(token[:])
		ledger.Runs = append(ledger.Runs, assistantRunRecord{ID: id, Revision: 1, Binding: binding, AdmissionID: admissionID, BodyDigest: bodyDigest, State: "admitted", Operations: []assistantRunOperation{}, Events: []assistantRunEvent{}})
		created = true
		return true, nil
	})
	if err != nil {
		return out, false, err
	}
	for _, run := range ledger.Runs {
		if run.ID == id {
			return run, created, nil
		}
	}
	return out, false, errAssistantRunInvalid
}

func (s assistantRunStore) Read(ctx context.Context, binding assistantRunBinding, id string) (assistantRunRecord, error) {
	var out assistantRunRecord
	_, err := s.transaction(ctx, func(ledger *assistantRunLedger) (bool, error) {
		run, e := assistantRunBound(ledger, binding, id)
		if e == nil {
			out = *run
		}
		return false, e
	})
	return out, err
}
func assistantRunBound(ledger *assistantRunLedger, binding assistantRunBinding, id string) (*assistantRunRecord, error) {
	if err := validateAssistantRunBinding(binding, ledger.Project); err != nil {
		return nil, err
	}
	for i := range ledger.Runs {
		if ledger.Runs[i].ID == id {
			if !reflect.DeepEqual(ledger.Runs[i].Binding, binding) {
				return nil, errAssistantRunScope
			}
			return &ledger.Runs[i], nil
		}
	}
	return nil, os.ErrNotExist
}

// Executor ownership is separate from short storage locks. Only one process may
// recover/dispatch this project's assistant runs. Losing this lease never
// authorizes a replacement invocation or automatic replay.
type assistantRunLease struct {
	mu          sync.Mutex
	store       assistantRunStore
	file        *os.File
	work        *os.File
	recoveryErr error
}

func (s assistantRunStore) Acquire(ctx context.Context) (*assistantRunLease, error) {
	if s.problem != nil {
		return nil, s.problem
	}
	if s.server == nil || !assistantRunFileName.MatchString(s.name) || s.name != "assistant-runs-"+s.project+".json" {
		return nil, errAssistantRunScope
	}
	work, err := s.server.openPrivateDataFileLock(ctx, ".data-work.lock", false, false)
	if err != nil {
		return nil, err
	}
	file, err := s.server.openPrivateDataFileLock(ctx, "assistant-run-executor-"+s.project+".lock", true, false)
	if err != nil {
		work.Close()
		return nil, err
	}
	lease := &assistantRunLease{store: s, file: file, work: work}
	if err = lease.Recover(ctx); err != nil {
		lease.Close()
		return nil, err
	}
	return lease, nil
}

func (l *assistantRunLease) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	if l.work != nil {
		err = errors.Join(err, l.work.Close())
		l.work = nil
	}
	return err
}
func (l *assistantRunLease) Recover(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return errAssistantRunScope
	}
	_, err := l.store.transaction(ctx, func(ledger *assistantRunLedger) (bool, error) { return interruptAssistantRuns(ledger, false), nil })
	l.recoveryErr = err
	l.store.server.setAssistantRunRecoveryFailure(err)
	return err
}

func assistantRunTerminal(state string) bool {
	return state == "complete" || state == "failed" || state == "stopped" || state == "interrupted"
}
func assistantRunText(text string, max int) bool {
	return text != "" && len(text) <= max && !strings.ContainsAny(text, "\r\n\x00") && validUTF8([]byte(text))
}
func assistantRunDigest(text string) bool {
	if len(text) != 64 {
		return false
	}
	_, err := hex.DecodeString(text)
	return err == nil && strings.ToLower(text) == text
}
func validateAssistantRunBinding(b assistantRunBinding, project string) error {
	for _, v := range []string{b.Principal, b.Session, b.Policy, b.Project, b.Owner.Revision, b.RequestDigest, b.ConnectionRevision, b.ProfileRevision, b.ExecutionKey} {
		if !assistantRunDigest(v) {
			return errAssistantRunInvalid
		}
	}
	if b.Project != project {
		return errAssistantRunScope
	}
	if !contains([]string{"chat", "draft", "pane", "brief", "workflow"}, b.Owner.Kind) || !assistantRunText(b.Owner.ID, 200) || !assistantRunText(b.RequestID, 128) || !aiIDPattern.MatchString(b.ConnectionID) || !assistantRunText(b.Model, 128) || !contains([]string{"plan", "work"}, b.Phase) || !contains([]string{"model-requests", "provider-managed"}, b.Unit) || len(b.SourceDigests) > 256 {
		return errAssistantRunInvalid
	}
	for _, v := range b.SourceDigests {
		if !assistantRunDigest(v) {
			return errAssistantRunInvalid
		}
	}
	return nil
}
func readAssistantRunCheckpoint(cp contract.Checkpoint, b assistantRunBinding) (*contract.Checkpoint, error) {
	raw, err := json.Marshal(cp)
	if err != nil {
		return nil, err
	}
	if len(raw) > runcontext.MaxCheckpointBytes {
		return nil, withCode(CodeTooLarge, errAssistantRunInvalid)
	}
	value, err := runcontext.ParseJSON(raw)
	if err != nil {
		return nil, err
	}
	held, err := runcontext.ReadExecution(value)
	if err != nil {
		return nil, err
	}
	if held == nil || held.RequestKey != b.ExecutionKey || held.Phase != b.Phase || held.Unit != b.Unit {
		return nil, errAssistantRunScope
	}
	return held, nil
}
func assistantRunEventBytes(event contract.RunEvent) ([]byte, error) {
	value := reflect.ValueOf(event)
	count := 0
	for i := 0; i < value.NumField(); i++ {
		if !value.Field(i).IsNil() {
			count++
		}
	}
	if count != 1 || event.MessageProgress != nil || event.ExecutionCheckpoint != nil || event.BudgetPaused != nil {
		return nil, errors.New("Use the checkpoint sink for checkpoints and the live stream for progress.")
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	// Generated unions preserve open members but do not validate schemas. Verify
	// their discriminator round-trips to the same union leg before saving bytes;
	// domain payload validation remains the owning feature's responsibility.
	var decoded contract.RunEvent
	if err = json.Unmarshal(raw, &decoded); err != nil {
		return nil, err
	}
	held := reflect.ValueOf(decoded)
	for i := 0; i < value.NumField(); i++ {
		if value.Field(i).IsNil() != held.Field(i).IsNil() {
			return nil, errAssistantRunInvalid
		}
	}
	return raw, nil
}
func validateAssistantRunLedger(ledger assistantRunLedger, project string) error {
	if ledger.Version != 1 || ledger.Project != project || len(ledger.Runs) > assistantRunCountLimit {
		return errAssistantRunInvalid
	}
	ids := map[string]bool{}
	active := map[string]bool{}
	keys := map[string]bool{}
	for _, run := range ledger.Runs {
		if !strings.HasPrefix(run.ID, "arun_") || len(run.ID) != 37 || !assistantRunDigest(run.BodyDigest) || run.Revision == 0 || !assistantRunText(run.AdmissionID, 128) {
			return errAssistantRunInvalid
		}
		if _, err := hex.DecodeString(strings.TrimPrefix(run.ID, "arun_")); err != nil {
			return errAssistantRunInvalid
		}
		key := run.Binding.Principal + ":" + run.AdmissionID
		if ids[run.ID] || keys[key] {
			return errAssistantRunInvalid
		}
		ids[run.ID] = true
		keys[key] = true
		if err := validateAssistantRunBinding(run.Binding, project); err != nil {
			return err
		}
		if !contains([]string{"admitted", "running", "awaiting-input", "awaiting-client", "paused-budget", "stopped", "interrupted", "complete", "failed"}, run.State) || len(run.Operations) > assistantRunOperationLimit || len(run.Events) > assistantRunEventLimit {
			return errAssistantRunInvalid
		}
		if run.Historical && !assistantRunTerminal(run.State) {
			return errAssistantRunInvalid
		}
		if err := validateAssistantChatProjection(run); err != nil {
			return err
		}
		if err := validateAssistantDomainOutputs(run); err != nil {
			return err
		}
		if err := validateAssistantRunActionReceipts(run); err != nil {
			return err
		}
		if !assistantRunTerminal(run.State) {
			owner := run.Binding.Owner.Kind + ":" + run.Binding.Owner.ID
			if active[owner] {
				return errAssistantRunInvalid
			}
			active[owner] = true
		}
		if run.Checkpoint != nil {
			if _, err := readAssistantRunCheckpoint(*run.Checkpoint, run.Binding); err != nil {
				return err
			}
		}
		ops := map[string]bool{}
		calls := map[string]bool{}
		for _, op := range run.Operations {
			if !assistantRunText(op.ID, 128) || !assistantRunText(op.CallID, 256) || !assistantRunText(op.Name, 200) || !contains([]string{"model", "runtime", "host", "action", "callback"}, op.Kind) || len(op.Intent) == 0 || len(op.Intent) > assistantRunRecordLimit || !contains([]string{"not-started", "dispatched", "completed", "refused", "uncertain"}, op.State) || ops[op.ID] || calls[op.CallID] {
				return errAssistantRunInvalid
			}
			if (op.State == "completed" || op.State == "refused") != (op.Result != nil) {
				return errAssistantRunInvalid
			}
			if run.State == "complete" && op.State != "completed" && op.State != "refused" {
				return errAssistantRunInvalid
			}
			ops[op.ID] = true
			calls[op.CallID] = true
		}
		events := map[string]bool{}
		for _, event := range run.Events {
			if !assistantRunText(event.ID, 128) || events[event.ID] {
				return errAssistantRunInvalid
			}
			if _, err := assistantRunEventBytes(event.Event); err != nil {
				return fmt.Errorf("assistant event: %w", err)
			}
			events[event.ID] = true
		}
	}
	return nil
}

func sameAssistantRunCheckpoint(a, b *contract.Checkpoint) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return bytes.Equal(left, right)
}
