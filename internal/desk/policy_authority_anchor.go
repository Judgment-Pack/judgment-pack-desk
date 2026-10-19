package desk

// This inactive primitive detects a portable ledger rolling back behind a
// separately retained machine-local high water. It grants no execution authority.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
)

const policyAuthorityAnchorName = "authority-anchor.json"
const policyAuthorityAnchorLimit = 4096
const policyAuthorityAnchorMaxGeneration = uint64(1<<53 - 1)

var policyAuthorityAnchorIdentityForm = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
var policyAuthorityAnchorNonceForm = regexp.MustCompile(`^[a-f0-9]{48}$`)

type policyAuthorityAnchorIdentity struct {
	InstallationID string `json:"installationId"`
	WorkspaceID    string `json:"workspaceId"`
}
type policyAuthorityAnchorIntent struct {
	Generation uint64 `json:"generation"`
	OldDigest  string `json:"oldDigest"`
	NewDigest  string `json:"newDigest"`
	Nonce      string `json:"nonce"`
}
type policyAuthorityAnchorState struct {
	Version        int                          `json:"version"`
	InstallationID string                       `json:"installationId"`
	WorkspaceID    string                       `json:"workspaceId"`
	Generation     uint64                       `json:"generation"`
	LedgerDigest   string                       `json:"ledgerDigest"`
	Intent         *policyAuthorityAnchorIntent `json:"intent,omitempty"`
}
type policyAuthorityAnchorObservation struct {
	Classification string
	Generation     uint64
}
type policyAuthorityAnchorError struct {
	Code           string
	Classification string
}

func (e *policyAuthorityAnchorError) Error() string { return e.Code }
func policyAuthorityAnchorRefusal(code string) *policyAuthorityAnchorError {
	return &policyAuthorityAnchorError{Code: "policy-authority-anchor-" + code}
}

type policyAuthorityAnchorWritePhase string

const (
	policyAuthorityAnchorEnrollment  policyAuthorityAnchorWritePhase = "enrollment"
	policyAuthorityAnchorIntentWrite policyAuthorityAnchorWritePhase = "intent"
	policyAuthorityAnchorFinalWrite  policyAuthorityAnchorWritePhase = "final"
)

type policyAuthorityAnchor struct {
	directory string
	identity  policyAuthorityAnchorIdentity
	poisoned  atomic.Bool
	// A local test dependency only; production always uses writePrivateData.
	persistForTest func(policyAuthorityAnchorWritePhase, *os.Root, []byte) error
	// A trusted machine binding opens through its pinned independent root.
	openRoot     func(bool) (*os.Root, error)
	checkCustody func() error
}

// The trusted host must select a machine-local directory outside all portable
// Desk/home/Runner roots. No default path, enrollment or service hook exists.
func newPolicyAuthorityAnchor(directory string, identity policyAuthorityAnchorIdentity) (*policyAuthorityAnchor, error) {
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory || filepath.Dir(directory) == directory || strings.ContainsAny(directory, "\x00\r\n") || !validPolicyAuthorityAnchorIdentity(identity) {
		return nil, policyAuthorityAnchorRefusal("invalid-input")
	}
	return &policyAuthorityAnchor{directory: directory, identity: identity}, nil
}
func validPolicyAuthorityAnchorIdentity(i policyAuthorityAnchorIdentity) bool {
	return policyAuthorityAnchorIdentityForm.MatchString(i.InstallationID) && policyAuthorityAnchorIdentityForm.MatchString(i.WorkspaceID)
}
func validatePolicyAuthorityAnchorState(s policyAuthorityAnchorState) error {
	bad := policyAuthorityAnchorRefusal("integrity")
	if s.Version != 1 || !validPolicyAuthorityAnchorIdentity(policyAuthorityAnchorIdentity{s.InstallationID, s.WorkspaceID}) || s.Generation < 1 || s.Generation > policyAuthorityAnchorMaxGeneration || !retainedArtifactDigest.MatchString(s.LedgerDigest) {
		return bad
	}
	if p := s.Intent; p != nil {
		if s.Generation == policyAuthorityAnchorMaxGeneration || p.Generation != s.Generation+1 || p.OldDigest != s.LedgerDigest || !retainedArtifactDigest.MatchString(p.NewDigest) || p.NewDigest == p.OldDigest || !policyAuthorityAnchorNonceForm.MatchString(p.Nonce) {
			return bad
		}
	}
	return nil
}
func policyAuthorityAnchorObject(raw []byte, fields []string, optional string) error {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return policyAuthorityAnchorRefusal("integrity")
	}
	for key, value := range object {
		if !contains(fields, key) || bytes.Equal(value, []byte("null")) {
			return policyAuthorityAnchorRefusal("integrity")
		}
	}
	for _, field := range fields {
		if field != optional && object[field] == nil {
			return policyAuthorityAnchorRefusal("integrity")
		}
	}
	return nil
}
func decodePolicyAuthorityAnchor(raw []byte) (policyAuthorityAnchorState, error) {
	var s policyAuthorityAnchorState
	if len(raw) > policyAuthorityAnchorLimit || rejectDuplicateMembers(raw) != nil || decodeDataJSON(raw, &s) != nil || policyAuthorityAnchorObject(raw, []string{"version", "installationId", "workspaceId", "generation", "ledgerDigest", "intent"}, "intent") != nil {
		return s, policyAuthorityAnchorRefusal("integrity")
	}
	if s.Intent != nil {
		var object map[string]json.RawMessage
		_ = json.Unmarshal(raw, &object)
		if policyAuthorityAnchorObject(object["intent"], []string{"generation", "oldDigest", "newDigest", "nonce"}, "") != nil {
			return s, policyAuthorityAnchorRefusal("integrity")
		}
	}
	return s, validatePolicyAuthorityAnchorState(s)
}

// Classification is an observation, never approval, operation outcome or a
// recovery instruction. Every pending classification remains held.
func classifyPolicyAuthorityAnchor(s policyAuthorityAnchorState, identity policyAuthorityAnchorIdentity, observedDigest string) (policyAuthorityAnchorObservation, error) {
	var observation policyAuthorityAnchorObservation
	if err := validatePolicyAuthorityAnchorState(s); err != nil {
		return observation, err
	}
	if !validPolicyAuthorityAnchorIdentity(identity) || s.InstallationID != identity.InstallationID || s.WorkspaceID != identity.WorkspaceID {
		return observation, policyAuthorityAnchorRefusal("identity")
	}
	if !retainedArtifactDigest.MatchString(observedDigest) {
		return observation, policyAuthorityAnchorRefusal("invalid-input")
	}
	observation.Generation = s.Generation
	if s.Intent != nil {
		observation.Classification = "pending-other"
		if observedDigest == s.Intent.OldDigest {
			observation.Classification = "pending-old"
		} else if observedDigest == s.Intent.NewDigest {
			observation.Classification = "pending-new"
		}
		return observation, nil
	}
	observation.Classification = "mismatch"
	if observedDigest == s.LedgerDigest {
		observation.Classification = "current"
	}
	return observation, nil
}
func policyAuthorityAnchorLedgerBytes(raw []byte) ([]byte, error) {
	if len(raw) == 0 || len(raw) > policyLedgerLimit {
		return nil, policyAuthorityAnchorRefusal("invalid-input")
	}
	return bytes.Clone(raw), nil
}
func (a *policyAuthorityAnchor) open(ctx context.Context, enroll bool) (*os.Root, *os.File, error) {
	if a == nil || ctx.Err() != nil {
		return nil, nil, policyAuthorityAnchorRefusal("unavailable")
	}
	if a.poisoned.Load() {
		return nil, nil, policyAuthorityAnchorRefusal("uncertain")
	}
	open := a.openRoot
	if open == nil {
		open = func(create bool) (*os.Root, error) { return openPrivateDataRoot(a.directory, create) }
	}
	root, err := open(enroll)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, policyAuthorityAnchorRefusal("missing")
		}
		return nil, nil, policyAuthorityAnchorRefusal("unavailable")
	}
	lock, err := root.Open(".")
	if err == nil {
		err = lockPrivateData(lock, true)
	}
	if err != nil {
		if lock != nil {
			lock.Close()
		}
		root.Close()
		if lockHeld(err) {
			return nil, nil, policyAuthorityAnchorRefusal("busy")
		}
		return nil, nil, policyAuthorityAnchorRefusal("unavailable")
	}
	if ctx.Err() != nil || a.poisoned.Load() {
		lock.Close()
		root.Close()
		return nil, nil, policyAuthorityAnchorRefusal("unavailable")
	}
	return root, lock, nil
}
func (a *policyAuthorityAnchor) read(root *os.Root) (policyAuthorityAnchorState, error) {
	raw, err := readPrivateData(root, policyAuthorityAnchorName, policyAuthorityAnchorLimit)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return policyAuthorityAnchorState{}, policyAuthorityAnchorRefusal("missing")
		}
		return policyAuthorityAnchorState{}, policyAuthorityAnchorRefusal("unavailable")
	}
	return decodePolicyAuthorityAnchor(raw)
}
func (a *policyAuthorityAnchor) persist(phase policyAuthorityAnchorWritePhase, root *os.Root, state policyAuthorityAnchorState) error {
	if err := validatePolicyAuthorityAnchorState(state); err != nil {
		return err
	}
	raw, err := json.Marshal(state)
	if err != nil || len(raw) > policyAuthorityAnchorLimit {
		return policyAuthorityAnchorRefusal("integrity")
	}
	if a.persistForTest != nil {
		err = a.persistForTest(phase, root, raw)
	} else {
		err = writePrivateData(root, policyAuthorityAnchorName, raw)
	}
	if err != nil {
		a.poisoned.Store(true)
		return policyAuthorityAnchorRefusal("uncertain")
	}
	return nil
}

// Enroll is a separate explicitly authorized action against exact current ledger
// bytes. The caller holds the ledger lock before reading them and until return.
// An existing record, including a corrupt/pending one, is never overwritten.
func (a *policyAuthorityAnchor) Enroll(ctx context.Context, currentLedger []byte) error {
	ledger, err := policyAuthorityAnchorLedgerBytes(currentLedger)
	if err != nil {
		return err
	}
	root, lock, err := a.open(ctx, true)
	if err != nil {
		return err
	}
	defer root.Close()
	defer lock.Close()
	_, err = readPrivateData(root, policyAuthorityAnchorName, policyAuthorityAnchorLimit)
	if err == nil {
		return policyAuthorityAnchorRefusal("exists")
	}
	if !errors.Is(err, os.ErrNotExist) {
		return policyAuthorityAnchorRefusal("unavailable")
	}
	if ctx.Err() != nil {
		return policyAuthorityAnchorRefusal("unavailable")
	}
	state := policyAuthorityAnchorState{Version: 1, InstallationID: a.identity.InstallationID, WorkspaceID: a.identity.WorkspaceID, Generation: 1, LedgerDigest: digestOf(ledger)}
	if err := a.persist(policyAuthorityAnchorEnrollment, root, state); err != nil {
		return err
	}
	if ctx.Err() != nil {
		a.poisoned.Store(true)
		return policyAuthorityAnchorRefusal("uncertain")
	}
	return nil
}

type policyAuthorityAnchorHeld struct {
	mu        sync.Mutex
	anchor    *policyAuthorityAnchor
	root      *os.Root
	state     policyAuthorityAnchorState
	active    bool
	attempted bool
	advanced  bool
	failure   error
}

// WithCurrent's total lock order is POLICY LEDGER -> AUTHORITY ANCHOR. The
// caller must already hold the ledger lock, having read the supplied exact bytes
// under that lock, and keep it through return. The callback may not escape held.
// There is no separate writable check token, and the anchor remains locked from
// this currentness check through the caller's validation, mutation and Advance.
func (a *policyAuthorityAnchor) WithCurrent(ctx context.Context, currentLedger []byte, use func(*policyAuthorityAnchorHeld) error) error {
	ledger, err := policyAuthorityAnchorLedgerBytes(currentLedger)
	if err != nil {
		return err
	}
	if use == nil {
		return policyAuthorityAnchorRefusal("invalid-input")
	}
	root, lock, err := a.open(ctx, false)
	if err != nil {
		return err
	}
	defer root.Close()
	defer lock.Close()
	state, err := a.read(root)
	if err != nil {
		return err
	}
	observation, err := classifyPolicyAuthorityAnchor(state, a.identity, digestOf(ledger))
	if err != nil {
		return err
	}
	if observation.Classification != "current" {
		failure := policyAuthorityAnchorRefusal("mismatch")
		if state.Intent != nil {
			failure = policyAuthorityAnchorRefusal("pending")
			failure.Classification = observation.Classification
		}
		return failure
	}
	if ctx.Err() != nil || a.checkCustody != nil && a.checkCustody() != nil {
		return policyAuthorityAnchorRefusal("unavailable")
	}
	held := &policyAuthorityAnchorHeld{anchor: a, root: root, state: state, active: true}
	callbackReturned := false
	defer func() {
		held.mu.Lock()
		held.active = false
		if !callbackReturned && held.advanced {
			a.poisoned.Store(true)
		}
		held.mu.Unlock()
	}()
	callbackErr := use(held)
	callbackReturned = true
	held.mu.Lock()
	defer held.mu.Unlock()
	held.active = false
	if a.checkCustody != nil && a.checkCustody() != nil {
		a.poisoned.Store(true)
		return policyAuthorityAnchorRefusal("uncertain")
	}
	if held.failure != nil {
		return held.failure
	}
	if held.state.Intent != nil || a.poisoned.Load() {
		a.poisoned.Store(true)
		return policyAuthorityAnchorRefusal("uncertain")
	}
	if callbackErr != nil || ctx.Err() != nil {
		if held.advanced {
			a.poisoned.Store(true)
			return policyAuthorityAnchorRefusal("uncertain")
		}
		return policyAuthorityAnchorRefusal("callback-refused")
	}
	return nil
}
func (h *policyAuthorityAnchorHeld) Current() (policyAuthorityAnchorObservation, error) {
	if h == nil {
		return policyAuthorityAnchorObservation{}, policyAuthorityAnchorRefusal("not-held")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.active || h.failure != nil || h.state.Intent != nil || h.anchor.poisoned.Load() {
		return policyAuthorityAnchorObservation{}, policyAuthorityAnchorRefusal("not-held")
	}
	return policyAuthorityAnchorObservation{Classification: "current", Generation: h.state.Generation}, nil
}

// Advance invokes durableWrite at most once with a defensive copy of nextLedger.
// That trusted callback must write those exact bytes durably and verify them,
// while continuing to hold the caller's ledger lock. An error is an uncertain
// outcome; no callback replay, intent resolution or rollback write is performed.
func (h *policyAuthorityAnchorHeld) Advance(ctx context.Context, nextLedger []byte, durableWrite func([]byte) error) (result error) {
	if h == nil {
		return policyAuthorityAnchorRefusal("not-held")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.active || h.failure != nil || h.anchor.poisoned.Load() {
		return policyAuthorityAnchorRefusal("not-held")
	}
	completed := false
	defer func() {
		if !completed {
			h.anchor.poisoned.Store(true)
			h.failure = policyAuthorityAnchorRefusal("uncertain")
			return
		}
		if result != nil {
			if h.advanced {
				h.anchor.poisoned.Store(true)
				result = policyAuthorityAnchorRefusal("uncertain")
			}
			h.failure = result
		}
	}()
	result = h.advanceLocked(ctx, nextLedger, durableWrite)
	completed = true
	return result
}

// The caller holds h.mu. The outer Advance records interrupted unwinding even
// when its caller recovers a panic; this body never manufactures an outcome.
func (h *policyAuthorityAnchorHeld) advanceLocked(ctx context.Context, nextLedger []byte, durableWrite func([]byte) error) error {
	if h.attempted {
		return policyAuthorityAnchorRefusal("already-attempted")
	}
	h.attempted = true
	next, err := policyAuthorityAnchorLedgerBytes(nextLedger)
	if err != nil {
		return err
	}
	if ctx.Err() != nil || h.anchor.checkCustody != nil && h.anchor.checkCustody() != nil {
		return policyAuthorityAnchorRefusal("unavailable")
	}
	nextDigest := digestOf(next)
	if nextDigest == h.state.LedgerDigest {
		return nil
	}
	if durableWrite == nil {
		return policyAuthorityAnchorRefusal("invalid-input")
	}
	if h.state.Generation == policyAuthorityAnchorMaxGeneration {
		return policyAuthorityAnchorRefusal("exhausted")
	}
	nonce, err := NewToken()
	if err != nil {
		return policyAuthorityAnchorRefusal("unavailable")
	}
	pending := h.state
	pending.Intent = &policyAuthorityAnchorIntent{Generation: pending.Generation + 1, OldDigest: pending.LedgerDigest, NewDigest: nextDigest, Nonce: nonce}
	if err = h.anchor.persist(policyAuthorityAnchorIntentWrite, h.root, pending); err != nil {
		return err
	}
	h.state = pending
	fail := func() error { h.anchor.poisoned.Store(true); return policyAuthorityAnchorRefusal("uncertain") }
	if ctx.Err() != nil || h.anchor.checkCustody != nil && h.anchor.checkCustody() != nil {
		return fail()
	}
	delivered := bytes.Clone(next)
	if err = durableWrite(delivered); err != nil || !bytes.Equal(delivered, next) {
		return fail()
	}
	if ctx.Err() != nil || h.anchor.checkCustody != nil && h.anchor.checkCustody() != nil {
		return fail()
	}
	committed := pending
	committed.Generation = pending.Intent.Generation
	committed.LedgerDigest = nextDigest
	committed.Intent = nil
	if err = h.anchor.persist(policyAuthorityAnchorFinalWrite, h.root, committed); err != nil {
		return err
	}
	h.state = committed
	h.advanced = true
	if ctx.Err() != nil || h.anchor.checkCustody != nil && h.anchor.checkCustody() != nil {
		return fail()
	}
	return nil
}
