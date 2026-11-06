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
	MemoryProposals    *bool             `json:"memoryProposals,omitempty"`
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
	AnswerHistory         []assistantRunAnswerOrigin            `json:"answerHistory,omitempty"`
	CanonicalRequired     bool                                  `json:"canonicalRequired,omitempty"`
	Observation           *assistantObservationCustody          `json:"observation,omitempty"`
	Canonical             *assistantCanonicalJournal            `json:"canonical,omitempty"`
	CriticCustody         *assistantCriticCustody               `json:"criticCustody,omitempty"`
	Creation              *assistantCreationReceipt             `json:"creation,omitempty"`
	DisplayPreview        []assistantRunPreview                 `json:"displayPreview,omitempty"`
	PackChecks            []assistantPackRecheck                `json:"packChecks,omitempty"`
	SavedPack             *assistantSavedPackBasis              `json:"savedPack,omitempty"`
	Websites              []assistantWebsiteCustody             `json:"websites,omitempty"`
	Links                 []assistantLinkCustody                `json:"links,omitempty"`
	Searches              []assistantSearchCustody              `json:"searches,omitempty"`
	DialectReads          []assistantDialectCompleteReadCustody `json:"dialectReads,omitempty"`
	DialectDrafts         []assistantDialectDraftCustody        `json:"dialectDrafts,omitempty"`
	DialectArtifactOwner  string                                `json:"dialectArtifactOwner,omitempty"`
	DialectArtifactImport *assistantDialectArtifactImport       `json:"dialectArtifactImport,omitempty"`
	DomainOutputs         []assistantDomainOutput               `json:"domainOutputs,omitempty"`
	ChatProjection        *assistantChatProjection              `json:"chatProjection,omitempty"`
	ProjectionIssue       string                                `json:"projectionIssue,omitempty"`
	ID                    string                                `json:"id"`
	Revision              uint64                                `json:"revision"`
	Binding               assistantRunBinding                   `json:"binding"`
	AdmissionID           string                                `json:"admissionId"`
	BodyDigest            string                                `json:"bodyDigest"`
	State                 string                                `json:"state"`
	Historical            bool                                  `json:"historical,omitempty"`
	Checkpoint            *contract.Checkpoint                  `json:"checkpoint,omitempty"`
	Operations            []assistantRunOperation               `json:"operations"`
	Events                []assistantRunEvent                   `json:"events"`
	Actions               []assistantRunActionReceipt           `json:"actions,omitempty"`
}
type assistantRunLedger struct {
	AuthoringCaptures []assistantAuthoringCapture `json:"authoringCaptures,omitempty"`
	Version           int                         `json:"version"`
	Project           string                      `json:"project"`
	Runs              []assistantRunRecord        `json:"runs"`
}

type assistantRunStore struct {
	server        *Server
	project, name string
	problem       error
	// Recovery's independently owned namespace/snapshot views must finish
	// read custody successfully before any admission or terminal recognition.
	strictReadCustody bool
}

func (s *Server) assistantRuns() assistantRunStore {
	return s.assistantRunsNamespace(context.Background(), false)
}

func (s *Server) assistantRunsContext(ctx context.Context) assistantRunStore {
	return s.assistantRunsNamespace(ctx, true)
}

func (s *Server) assistantRunsNamespace(ctx context.Context, strict bool) (held assistantRunStore) {
	held = assistantRunStore{server: s, strictReadCustody: strict}
	lock, err := s.privateDataLockContext(ctx, true)
	if err != nil {
		held.problem = err
		return held
	}
	defer func() {
		closed := lock.Close()
		if strict {
			held.problem = errors.Join(held.problem, assistantRunReadCloseError(ctx, "assistant-run-namespace-private", closed))
		}
	}()
	store, err := s.openChatData()
	if err != nil {
		held.problem = err
		return held
	}
	defer func() {
		closed := store.root.Close()
		if strict {
			held.problem = errors.Join(held.problem, assistantRunReadCloseError(ctx, "assistant-run-namespace-data", closed))
		}
	}()
	held = s.assistantRunsLocked(store.root)
	held.strictReadCustody = strict
	return held
}

// The actual root/lock Close has already executed. Reuse the existing owner
// fault seam only after that boundary, and keep cancellation alongside every
// real or injected Close error. This never runs under Q or accepts a callback.
func assistantRunReadCloseError(ctx context.Context, label string, closed error) error {
	return errors.Join(closed, dialectArtifactOwnerStep(context.Background(), label+".close"), ctx.Err())
}

// Caller holds private-data custody; namespace selection never acquires it again.
func (s *Server) assistantRunsLocked(root *os.Root) assistantRunStore {
	held := assistantRunStore{server: s}
	held.project, held.problem = assistantRunNamespace(root, s.conversationIdentity())
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
	return s.transactionLocked(ctx, store, nil, nil, change)
}

// Only the concrete original reservation owns these exact forward-acquired
// handles. The ordinary and reservation paths share this persistence body.
func (s assistantRunStore) transactionDialectDraftLocked(ctx context.Context, g *assistantDialectDraftReservationGuard, change func(*assistantRunLedger) (bool, error)) (assistantRunLedger, error) {
	if g == nil || g.self != g || g.owner.lease.store != s || g.data == nil || g.private == nil {
		return assistantRunLedger{}, errAssistantRunScope
	}
	if err := g.Check(ctx); err != nil {
		return assistantRunLedger{}, err
	}
	return s.transactionLocked(ctx, g.data, g, nil, change)
}

func (s assistantRunStore) transactionLocked(ctx context.Context, store *chatDataStore, g *assistantDialectDraftReservationGuard, readGuard *assistantDialectCompleteReadGuard, change func(*assistantRunLedger) (bool, error)) (assistantRunLedger, error) {
	var empty assistantRunLedger
	ledger, captured, root, err := s.readValidatedLocked(ctx, store)
	if err != nil {
		return empty, err
	}
	// Decode an independent baseline before exposing the callback graph. No
	// retained alias or marshaler may redefine the original transition proof.
	before, err := captured.decodeValidated(ctx)
	if err != nil {
		return empty, err
	}
	var admission *assistantDialectArtifactOwnerAdmission
	if g != nil {
		if err = g.bindBaseline(ctx, before); err != nil {
			return empty, err
		}
		proof := g.admission
		admission = &proof
	}
	if readGuard != nil {
		if g != nil {
			return empty, errAssistantRunScope
		}
		if err = readGuard.bindBaseline(ctx, before); err != nil {
			return empty, err
		}
		proof := readGuard.admission
		admission = &proof
	}
	changed, err := change(&ledger)
	if err != nil {
		return empty, err
	}
	if !changed {
		// False means no ledger persistence. Preserve closure side effects, but
		// return the captured durable baseline, not transient callback edits.
		return captured.decodeValidated(ctx)
	}
	if err = validateAssistantDialectArtifactOwnerTransition(before, ledger, admission); err != nil {
		return empty, err
	}
	raw, saved, err := prepareAssistantRunStorage(s.name, ledger)
	if err != nil {
		return empty, err
	}
	// The marshaler may have changed the callback graph or emitted another
	// graph. Check both against the original immutable admission and baseline.
	if err = validateAssistantDialectArtifactOwnerTransition(before, saved, admission); err != nil {
		return empty, err
	}
	if g != nil {
		if err = g.Check(ctx); err != nil {
			return empty, err
		}
	}
	if readGuard != nil {
		if err = readGuard.Check(ctx); err != nil {
			return empty, err
		}
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
	// Publish only after the existing verified write succeeds. On uncertain
	// writes the valid baseline fact remains; the next read must match bytes.
	s.server.rememberAssistantRunStorage(root, store, s.project, s.name, raw)
	return saved, nil
}

// readLocked is bounded read-only journal lookup under the caller's existing
// private-data lock. It does not recover, lease, write, dispatch or take a service
// mutex, and therefore is safe in the conversation writes -> private lock span.
func (s assistantRunStore) readLocked(ctx context.Context, store *chatDataStore) (assistantRunLedger, error) {
	ledger, _, _, err := s.readValidatedLocked(ctx, store)
	return ledger, err
}

// A read captures independently owned bytes under private-data custody, then
// releases both root and lock before performing whole-ledger validation. Mutation
// callers continue to use readLocked and retain custody through validation/CAS.
func (s assistantRunStore) readSnapshot(ctx context.Context) (assistantRunLedger, error) {
	captured, err := func() (captured assistantRunCapturedRead, err error) {
		if s.problem != nil {
			return assistantRunCapturedRead{}, s.problem
		}
		if s.server == nil {
			return assistantRunCapturedRead{}, errAssistantRunScope
		}
		lock, err := s.server.privateDataLockContext(ctx, true)
		if err != nil {
			return assistantRunCapturedRead{}, err
		}
		defer func() {
			closed := lock.Close()
			if s.strictReadCustody {
				err = errors.Join(err, assistantRunReadCloseError(ctx, "assistant-run-snapshot-private", closed))
			}
		}()
		store, err := s.server.openChatData()
		if err != nil {
			return assistantRunCapturedRead{}, err
		}
		defer func() {
			closed := store.root.Close()
			if s.strictReadCustody {
				err = errors.Join(err, assistantRunReadCloseError(ctx, "assistant-run-snapshot-data", closed))
			}
		}()
		return s.captureLocked(store)
	}()
	if err != nil {
		return assistantRunLedger{}, err
	}
	return captured.decode(ctx)
}

type assistantRunCapturedRead struct {
	name, project string
	raw           []byte
	missing       bool
}

func (s assistantRunStore) captureLocked(store *chatDataStore) (assistantRunCapturedRead, error) {
	var empty assistantRunCapturedRead
	if err := checkPrivateDataPath(store); err != nil {
		return empty, err
	}
	if err := s.checkScope(store.root); err != nil {
		return empty, err
	}
	raw, err := readPrivateData(store.root, s.name, assistantRunRecordLimit)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return empty, err
	}
	return assistantRunCapturedRead{name: s.name, project: s.project, raw: raw, missing: errors.Is(err, os.ErrNotExist)}, nil
}

func (c assistantRunCapturedRead) decode(ctx context.Context) (assistantRunLedger, error) {
	var empty assistantRunLedger
	ledger := assistantRunLedger{Version: 1, Project: c.project, Runs: []assistantRunRecord{}}
	if !c.missing {
		var err error
		if ledger, err = decodeAssistantRunStorage(c.name, c.raw); err != nil {
			return empty, err
		}
		// The decoder validates all records against the captured filename; the
		// scope check binds that filename to the project at capture time.
		if ledger.Project != c.project {
			return empty, errAssistantRunInvalid
		}
	} else if err := validateAssistantRunLedger(ledger, c.project); err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return ledger, nil
}

// AdmissionID is stable within a project/principal across lost responses and
// session changes. Reusing it with another session/owner/request refuses rather
// than accidentally starting a second operation. Fresh attempts need fresh IDs.
// freshCAS runs only for new admission, under the same private-data lock as the
// saved owner. It must only read/validate current state; no dispatch or writes.
func (l *assistantRunLease) Admit(ctx context.Context, binding assistantRunBinding, admissionID, bodyDigest string, freshCAS func() error, successor ...*assistantRunAnswerAdmission) (assistantRunRecord, bool, error) {
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
	if len(successor) > 1 {
		return out, false, errAssistantRunInvalid
	}
	var answer *assistantRunAnswerAdmission
	if len(successor) == 1 {
		answer = successor[0]
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
		if answer != nil {
			if err := answer.validateParent(ledger, binding); err != nil {
				return false, err
			}
		}
		for _, run := range ledger.Runs {
			if answer != nil && run.ID == answer.parent.ID {
				continue
			}
			if run.Binding.Owner.Kind == binding.Owner.Kind && run.Binding.Owner.ID == binding.Owner.ID && !assistantRunTerminal(run.State) {
				return false, errAssistantRunConflict
			}
		}
		if len(ledger.Runs)+len(ledger.AuthoringCaptures) >= assistantRunCountLimit {
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
		run := assistantRunRecord{ID: id, Revision: 1, Binding: binding, AdmissionID: admissionID, BodyDigest: bodyDigest, State: "admitted", Operations: []assistantRunOperation{}, Events: []assistantRunEvent{}}
		if answer != nil {
			if err := answer.initialize(&run); err != nil {
				return false, err
			}
			// Promote before the durable replacement. A failed replacement releases this
			// provisional reference; no partially admitted successor is discoverable.
			if answer.promote != nil {
				if err := answer.promote(run); err != nil {
					return false, err
				}
			}
			if answer.prepare != nil {
				if err := answer.prepare(run); err != nil {
					return false, err
				}
			}
			for i := range ledger.Runs {
				parent := &ledger.Runs[i]
				if parent.ID == answer.parent.ID {
					parent.Actions = append(parent.Actions, assistantRunActionReceipt{ID: answer.action.ID, Digest: assistantRunActionDigest(answer.action), Kind: "answer", Answer: &answer.origin.Answer, SuccessorRunID: id})
					parent.State = "complete"
					parent.Revision++
				}
			}
		}
		ledger.Runs = append(ledger.Runs, run)
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
	ledger, err := s.readSnapshot(ctx)
	if err != nil {
		return assistantRunRecord{}, err
	}
	run, err := assistantRunBound(&ledger, binding, id)
	if err != nil {
		return assistantRunRecord{}, err
	}
	return *run, nil
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
	mu           sync.Mutex
	store        assistantRunStore
	file         *os.File
	work         *os.File
	recoveryErr  error
	recoveryHome *assistantDialectRecoveryHomeWitness
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
	lease := &assistantRunLease{store: s, file: file, work: work, recoveryHome: s.server.originalDialectRecoveryHome()}
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
	ownerLimit := 200
	if b.Owner.Kind == "brief" {
		ownerLimit = 1024
	} // Existing BriefCommand.Subject byte bound.
	if !contains([]string{"chat", "draft", "pane", "brief", "workflow", "author-pack", "describe-pack", "author-graph", assistantDiagnosticPurpose, assistantSettingsObservationPurpose}, b.Owner.Kind) || !assistantRunText(b.Owner.ID, ownerLimit) || !assistantRunText(b.RequestID, 128) || !aiIDPattern.MatchString(b.ConnectionID) || !assistantRunText(b.Model, 128) || !contains([]string{"plan", "work"}, b.Phase) || !contains([]string{"model-requests", "provider-managed"}, b.Unit) || len(b.SourceDigests) > 256 {
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
	if err := validateAssistantAuthoringCaptures(ledger); err != nil {
		return err
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
		if !validAssistantCriticCustody(run.CriticCustody) {
			return errAssistantRunInvalid
		}
		if !validAssistantCanonicalJournal(run.Canonical) {
			return errAssistantRunInvalid
		}
		if err := validateAssistantCanonicalProjection(run); err != nil {
			return err
		}
		if err := validateAssistantRunPreview(run.DisplayPreview); err != nil {
			return err
		}
		if err := validateAssistantChatProjection(run); err != nil {
			return err
		}
		if err := validateAssistantDomainOutputs(run); err != nil {
			return err
		}
		if err := validateAssistantCreationReceipt(run); err != nil {
			return err
		}
		if _, err := pendingAssistantRunQuestion(run); err != nil {
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
	if err := validateAssistantAnswerLinks(ledger); err != nil {
		return err
	}
	return validateAssistantCreationHeldWitnesses(ledger)
}

func sameAssistantRunCheckpoint(a, b *contract.Checkpoint) bool {
	left, err := json.Marshal(a)
	if err != nil || checkJSONMembers(left, opaqueJSONMembers) != nil {
		return false
	}
	right, err := json.Marshal(b)
	if err != nil || checkJSONMembers(right, opaqueJSONMembers) != nil {
		return false
	}
	// Canonical storage reorders object members, including retained RawMessage
	// values. Keep all members, array order, strings and numeric lexemes exact.
	left, right = canonicalApprovalJSON(left), canonicalApprovalJSON(right)
	return left != nil && right != nil && bytes.Equal(left, right)
}

// Run custody is decoded by its original full validator. Known factory refs
// remain visible, but unexamined canonical/imported/operation operands prevent
// a negative reference proof for ANY candidate in the joined preview.
func retentionRunReferences(name string, raw []byte) storageRetentionSource {
	out := storageRetentionUnknown(name, "assistant-runs", raw, "opaque-run-custody")
	ledger, err := decodeAssistantRunStorage(name, raw)
	if err != nil || !uniqueJSON(raw) {
		out.Reasons = []string{"invalid-run-source"}
		return out
	}
	complete := storageRetentionMembers(raw, "version", "project", "runs", "authoringCaptures") && len(ledger.AuthoringCaptures) == 0
	for _, run := range ledger.Runs {
		// Full schema preservation is independent from preview support. Never
		// reinterpret absence of a decoded optional field as proof against an opaque
		// original-byte extension.
		if !assistantRunTerminal(run.State) || run.Checkpoint != nil || len(run.Binding.SourceDigests) != 0 || len(run.Operations) != 0 || len(run.Events) != 0 || len(run.Actions) != 0 || run.Canonical != nil || run.Creation != nil || run.Observation != nil || run.CriticCustody != nil || run.DialectArtifactImport != nil || len(run.DomainOutputs) != 0 || run.SavedPack != nil || len(run.PackChecks) != 0 || len(run.AnswerHistory) != 0 || len(run.Websites) != 0 || len(run.Links) != 0 || len(run.Searches) != 0 {
			complete = false
		}
		// An arbitrary run JSON can retain forward-compatible raw members. Until
		// every producer/consumer is covered, nonempty run rows are opaque roots.
		complete = false
		for _, held := range run.DialectDrafts {
			for _, slot := range []assistantDialectDraftSlot{held.Candidate, held.Checked, held.Final} {
				for _, operand := range []*assistantDialectDraftIntent{slot.Intent, slot.Receipt} {
					if operand != nil {
						out.add(storageRetentionReference{"retained", operand.Reference.SHA256, fmt.Sprint(operand.Reference.Bytes)})
					}
				}
			}
		}
		for _, held := range run.DialectReads {
			out.add(storageRetentionReference{"retained", held.Intent.Reference.SHA256, fmt.Sprint(held.Intent.Reference.Bytes)})
			if held.Receipt != nil {
				out.add(storageRetentionReference{"retained", held.Receipt.Reference.SHA256, fmt.Sprint(held.Receipt.Reference.Bytes)})
			}
		}
	}
	out.Complete = complete && len(out.Reasons) == 1
	if out.Complete {
		out.Reasons = []string{}
	}
	return out
}
