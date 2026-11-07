package desk

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Installed only by an original machine-owner constructor after original root,
// workspace, capacity registration and census. There is no production observer
// default, path/marker adoption, request option or serialized write capability.
type assistantDialectReadRecoveryStorageWriter struct {
	self                    *assistantDialectReadRecoveryStorageWriter
	mu                      sync.Mutex
	server                  *Server
	owner                   *storageBudgetOwner
	enrollment              *storageServerEnrollment
	settings                *os.Root
	root, config            *storageRootBinding
	workspace               storagebudget.ID
	home                    *assistantDialectRecoveryHomeWitness
	projectInfo, configInfo os.FileInfo
	pointer                 []byte
	revoked                 atomic.Bool
	operations              map[*assistantDialectReadRecoveryStorageOperation]context.CancelFunc
	drained, closedDone     chan struct{}
	closed                  bool
	closeErr                error
}

// Select only an actual current-launch source retained by New's enrolled
// owner. A path/marker match alone, an external source, or a newly created
// private/history directory cannot install recovery authority.
func originalDialectReadRecoveryBinding(ctx context.Context, s *Server) (o *storageBudgetOwner, root *storageRootBinding, workspace storagebudget.ID, settings *os.Root, err error) {
	if s == nil || s.storageReadOnly || s.storageEnrollment == nil {
		return nil, nil, workspace, nil, nil
	}
	e := s.storageEnrollment
	c := e.construction
	if e.self != e || e.closed || e.server != s || c == nil || c.self != c || c.server != s.deskOwner() || c.closing || c.sources == nil || c.owner == nil || c.ref == nil || e.ref == nil || e.ref.cell != c.ref.cell {
		return nil, nil, workspace, nil, errAssistantRunScope
	}
	if err = c.current(ctx, false); err != nil {
		return nil, nil, workspace, nil, err
	}
	o = c.owner
	if _, ok := o.observer.(*storageAdvisoryObserver); !ok || o.censusEpoch == 0 {
		return nil, nil, workspace, nil, errAssistantDialectReadRecoveryBudget
	}
	key, err := c.storageAccountingKey(ctx, s)
	if err != nil {
		return nil, nil, workspace, nil, err
	}
	for _, w := range c.sources.workspaces {
		if string(w.OwnerKey) == key && string(w.HistoryIdentity) == s.conversationIdentity() {
			if workspace != (storagebudget.ID{}) {
				return nil, nil, workspace, nil, errAssistantRunScope
			}
			workspace = w.Definition.ID
		}
	}
	if workspace == (storagebudget.ID{}) {
		return nil, nil, workspace, nil, nil
	}
	for _, selected := range c.sources.selections {
		if s.containedDeskData() {
			if selected.path != filepath.Join(s.projectDir, ".desk-private") {
				continue
			}
		} else if selected.settings != c.original.config {
			continue
		}
		// A missing original pointer remains read-only/unavailable here. Opening
		// chat data would create/claim/write it outside the enrolled transition.
		if !selected.explicit {
			return nil, nil, workspace, nil, nil
		}
		for _, r := range o.roots {
			source := r.source
			if source == nil || source.path != selected.location.Path || r.qControl {
				continue
			}
			if r.configControl {
				if selected.settings != c.original.config || source.role != 1 {
					continue
				}
			} else if source.role != 4 || source.parent == nil || source.parent.root != selected.settings || source.relative == "" {
				continue // external/reattached histories need their own transition.
			}
			found := false
			for _, id := range source.workspaces {
				found = found || id == workspace
			}
			if !found || root != nil {
				return nil, nil, workspace, nil, errAssistantRunScope
			}
			root, settings = r, selected.settings
		}
	}
	return o, root, workspace, settings, nil
}

func (s *Server) attachOriginalDialectReadRecoveryStorage(ctx context.Context) error {
	o, root, workspace, _, err := originalDialectReadRecoveryBinding(ctx, s)
	if err != nil || root == nil {
		return err
	}
	_, err = newAssistantDialectReadRecoveryStorage(s, o, root, workspace)
	return err
}

func newAssistantDialectReadRecoveryStorage(s *Server, o *storageBudgetOwner, root *storageRootBinding, workspace storagebudget.ID) (out *assistantDialectReadRecoveryStorageWriter, err error) {
	if s == nil || s.assistant == nil || !s.assistant.usable() || !o.valid() || root == nil || root.self != root || root.owner != o || o.roots[root.row.ID] != root || root.qControl || o.censusEpoch == 0 || s.recoveryStorage != nil {
		return nil, errAssistantDialectReadRecoveryBudget
	}
	actualOwner, actualRoot, actualWorkspace, settings, err := originalDialectReadRecoveryBinding(context.Background(), s)
	if err != nil || o != actualOwner || root != actualRoot || workspace != actualWorkspace || settings == nil {
		return nil, errors.Join(errAssistantDialectReadRecoveryBudget, err)
	}
	if e := o.retain(); e != nil {
		return nil, e
	}
	w := &assistantDialectReadRecoveryStorageWriter{server: s, owner: o, enrollment: s.storageEnrollment, settings: settings, root: root, workspace: workspace, home: s.originalDialectRecoveryHome(), operations: map[*assistantDialectReadRecoveryStorageOperation]context.CancelFunc{}, drained: make(chan struct{}), closedDone: make(chan struct{})}
	w.self = w
	defer func() {
		if err != nil {
			err = errors.Join(err, o.drop())
		}
	}()
	if err = w.home.current(); err != nil {
		return nil, err
	}
	if o.workspaces[workspace].Generation == 0 {
		return nil, errAssistantRunScope
	}
	for _, r := range o.roots {
		if r.configControl {
			if w.config != nil {
				return nil, errAssistantRunScope
			}
			w.config = r
		}
	}
	if w.config == nil || w.config.domainName != ".data.lock" {
		return nil, errAssistantRunScope
	}
	if w.projectInfo, err = s.root.Stat("."); err != nil {
		return nil, err
	}
	if w.configInfo, err = s.assistant.root.Stat("."); err != nil {
		return nil, err
	}
	original, e := o.ref.cell.original.config.Stat(".")
	if e != nil || !os.SameFile(original, w.configInfo) {
		return nil, errors.Join(errAssistantRunScope, e)
	}
	// Preparation precedes any FIFO turn even at initial attachment.
	err = func() (err error) {
		prepared, e := w.prepareOriginalDialectReadRecovery(context.Background())
		if e != nil {
			return e
		}
		defer func() { err = errors.Join(err, prepared.Close()) }()
		lock, e := s.privateDataLockContext(context.Background(), true)
		if e != nil {
			return e
		}
		defer func() { err = errors.Join(err, lock.Close()) }()
		w.pointer, e = readPrivateData(w.settings, dataLocationName, 16<<10)
		if e != nil {
			return e
		}
		data, e := s.openChatData()
		if e != nil {
			return e
		}
		defer func() { err = errors.Join(err, data.root.Close()) }()
		if e = w.matchData(data); e != nil {
			return e
		}
		view, e := o.ref.cell.q.InspectHost(context.Background())
		if e != nil {
			return e
		}
		missing, e := o.registrationMissingFrom(view)
		if e != nil || view.Configuration.Maintenance || storageRegistrationCount(missing) != 0 {
			return errors.Join(errAssistantDialectReadRecoveryBudget, e)
		}
		return nil
	}()
	if err != nil {
		return nil, err
	}
	h := s.assistantRunService()
	if !h.mu.TryLock() {
		return nil, errRetainedArtifactBusy
	}
	defer h.mu.Unlock()
	if h.closed || h.lease != nil || h.dialectReadRecoveryPins != 0 || len(h.active) != 0 || h.dialectReadRecoveryWriter != nil {
		return nil, errRetainedArtifactBusy
	}
	h.dialectReadRecoveryWriter = w
	s.recoveryStorage = w
	return w, nil
}
func (w *assistantDialectReadRecoveryStorageWriter) current(ctx context.Context) error {
	if w == nil || w.self != w || w.revoked.Load() || w.server == nil || w.owner == nil || w.root == nil || w.enrollment == nil || w.enrollment.self != w.enrollment || w.enrollment.closed || w.server.storageEnrollment != w.enrollment || w.enrollment.server != w.server || w.enrollment.construction == nil || w.enrollment.construction.owner != w.owner || w.owner.roots[w.root.row.ID] != w.root {
		return errAssistantDialectReadRecoveryBudget
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	if e := w.home.current(); e != nil {
		return e
	}
	s := w.server
	if s.homeMove.queued.Load() || s.location.moving.Load() || s.deskOwner().homeMove.queued.Load() {
		return errAssistantRunScope
	}
	p, e := s.root.Stat(".")
	if e != nil || !os.SameFile(p, w.projectInfo) || !sameDirectory(s.projectDir, p) {
		return errors.Join(errAssistantRunScope, e)
	}
	c, e := s.assistant.root.Stat(".")
	if e != nil || !os.SameFile(c, w.configInfo) || !sameDirectory(s.configDir, c) {
		return errors.Join(errAssistantRunScope, e)
	}
	if e = w.root.current(ctx); e != nil {
		return e
	}
	return w.config.current(ctx)
}
func (w *assistantDialectReadRecoveryStorageWriter) matchData(data *chatDataStore) error {
	if data == nil || data.root == nil || checkPrivateDataPath(data) != nil {
		return errAssistantRunScope
	}
	info, e := data.root.Stat(".")
	if e != nil {
		return e
	}
	original, e := w.root.root.Stat(".")
	if e != nil || !os.SameFile(info, original) {
		return errors.Join(errAssistantRunScope, e)
	}
	category, refs := w.root.categoryOf("assistant-dialect-read-recovery-" + string(bytes.Repeat([]byte{'a'}, 64)) + "-arun_" + string(bytes.Repeat([]byte{'b'}, 32)) + ".json")
	found := false
	for _, id := range refs {
		found = found || id == w.workspace
	}
	if category != storagebudget.Control || !found {
		return errAssistantRunScope
	}
	if len(w.pointer) > 0 && data.revision != digestOf(w.pointer) {
		return errAssistantRunScope
	}
	return nil
}
func (w *assistantDialectReadRecoveryStorageWriter) Close() error {
	if w == nil {
		return nil
	}
	if w.self != w {
		return errAssistantRunScope
	}
	w.revoked.Store(true)
	w.mu.Lock()
	if w.closed {
		done := w.closedDone
		w.mu.Unlock()
		<-done
		return w.closeErr
	}
	w.closed = true
	cancel := make([]context.CancelFunc, 0, len(w.operations))
	for _, stop := range w.operations {
		cancel = append(cancel, stop)
	}
	if len(w.operations) == 0 {
		close(w.drained)
	}
	w.mu.Unlock()
	for _, stop := range cancel {
		stop()
	}
	// No host/service/lease or writer mutex is held while canceled FIFO waits
	// and already-entered completion return their original custody.
	<-w.drained
	err := w.owner.drop()
	w.mu.Lock()
	w.closeErr = errors.Join(w.closeErr, err)
	err = w.closeErr
	close(w.closedDone)
	w.mu.Unlock()
	return err
}

func (w *assistantDialectReadRecoveryStorageWriter) releaseOperation(op *assistantDialectReadRecoveryStorageOperation) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.operations, op)
	if w.closed {
		w.closeErr = errors.Join(w.closeErr, op.closeErr)
	}
	if w.closed && len(w.operations) == 0 {
		close(w.drained)
	}
}

// Home/use are prepared before external work/executor acquisition. Active
// borrowed work additionally needs the constructor witness stored by Acquire.
// No Recover or service lease release occurs on this path.
type assistantDialectReadRecoveryStorageOperation struct {
	self                   *assistantDialectReadRecoveryStorageOperation
	mu                     sync.Mutex
	writer                 *assistantDialectReadRecoveryStorageWriter
	ctx                    context.Context
	cancel                 context.CancelFunc
	home                   *os.File
	use                    *storageInstallationUse
	authority              *storageMutationAuthority
	capacity               *storageCapacityPass
	pin                    *assistantDialectReadRecoveryPin
	private                *assistantDialectRecoveryPrivateHold
	intent                 assistantDialectReadRecoveryWriteIntent
	operationID, requestID string
	prior                  []byte
	admission              *assistantDialectReadRecoveryStorageAdmission
	revoked                atomic.Bool
	closed                 bool
	closeErr               error
}

func (w *assistantDialectReadRecoveryStorageWriter) prepareOriginalDialectReadRecovery(ctx context.Context) (out *assistantDialectReadRecoveryStorageOperation, err error) {
	if w == nil || w.self != w {
		return nil, errAssistantDialectReadRecoveryBudget
	}
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil, errAssistantDialectReadRecoveryBudget
	}
	// Preparation itself owns descriptors and an observer pass. Bound it to
	// one live recovery operation per original Server, before host/FIFO waits.
	if len(w.operations) != 0 {
		w.mu.Unlock()
		return nil, errRetainedArtifactBusy
	}
	if err = w.current(ctx); err != nil {
		w.mu.Unlock()
		return nil, err
	}
	if err = w.owner.retain(); err != nil {
		w.mu.Unlock()
		return nil, err
	}
	op := &assistantDialectReadRecoveryStorageOperation{writer: w}
	op.self = op
	op.ctx, op.cancel = context.WithCancel(ctx)
	w.operations[op] = op.cancel
	w.mu.Unlock()
	ctx = op.ctx
	defer func() {
		if err != nil {
			err = errors.Join(err, op.Close())
		}
	}()
	op.authority, err = w.owner.beginAdmission(ctx)
	if err != nil {
		return nil, err
	}
	ctx = storageMutationContext(ctx, op.authority)
	recoveryStorageStep("admission-prepared")
	named, e := storageOpenOriginalControl(w.owner.ref.cell, ".home-use.lock")
	if e != nil {
		return nil, e
	}
	op.home = named.file
	if e = lockPrivateData(op.home, false); e != nil {
		return nil, storagebudget.ErrBusy
	}
	held, e := op.home.Stat()
	if e != nil || !os.SameFile(held, w.home.info) {
		return nil, errors.Join(errAssistantRunScope, e)
	}
	recoveryStorageStep("home-prepared")
	op.use, e = w.owner.ref.acquire()
	if e != nil {
		return nil, e
	}
	recoveryStorageStep("use-prepared")
	// Advisory observation custody is also prepared before work/private/host.
	// Sidecars are flat leaves, so this is the exact later effect parent. The
	// pass remains bounded/current; expiry refuses rather than refreshing below
	// a held prefix or reacquiring installation-use custody there.
	op.capacity, e = w.owner.beginStorageCapacityPass(ctx, []storageCapacityEffectParent{{binding: w.root, root: w.root.root}})
	if e != nil {
		return nil, e
	}
	recoveryStorageStep("capacity-prepared")
	if e = op.current(ctx); e != nil {
		return nil, e
	}
	return op, nil
}
func (op *assistantDialectReadRecoveryStorageOperation) current(ctx context.Context) error {
	if op == nil || op.self != op || op.revoked.Load() || op.use == nil || op.home == nil {
		return errAssistantRunScope
	}
	if e := op.authority.current(ctx, true); e != nil {
		return e
	}
	if e := op.writer.current(ctx); e != nil {
		return e
	}
	if e := op.use.Current(); e != nil {
		return e
	}
	if op.capacity != nil {
		if e := op.capacity.current(ctx); e != nil {
			return e
		}
	}
	info, e := op.home.Stat()
	if e != nil || !os.SameFile(info, op.writer.home.info) {
		return errors.Join(errAssistantRunScope, e)
	}
	return ctx.Err()
}
func (op *assistantDialectReadRecoveryStorageOperation) bindPin(p *assistantDialectReadRecoveryPin) error {
	if op == nil || op.self != op || op.pin != nil || p == nil || p.self != p || p.host.server != op.writer.server || p.lease.recoveryHome != op.writer.home {
		return errAssistantRunScope
	}
	if e := op.current(context.Background()); e != nil {
		return e
	}
	op.pin = p
	p.storage = op
	recoveryStorageStep("pin-bound")
	return nil
}
func (op *assistantDialectReadRecoveryStorageOperation) transferPrivate(ctx context.Context, g *assistantDialectReadRecoveryGuard, intent assistantDialectReadRecoveryWriteIntent, prior []byte, row assistantDialectReadRecoveryRecord) error {
	op.mu.Lock()
	defer op.mu.Unlock()
	if op.private != nil || op.admission != nil || g == nil || g.pin != op.pin || g.private == nil || g.privateBorrow != nil {
		return errAssistantRunScope
	}
	if e := op.current(ctx); e != nil {
		return e
	}
	if e := g.Check(ctx); e != nil {
		return e
	}
	if e := op.writer.matchData(g.data); e != nil {
		return e
	}
	pointer, e := readPrivateData(op.writer.settings, dataLocationName, 16<<10)
	if e != nil || !bytes.Equal(pointer, op.writer.pointer) {
		return errors.Join(errAssistantRunScope, e)
	}
	if intent.Target != assistantDialectReadRecoveryName(g.run.Binding.Project, g.run.ID) || len(intent.Bytes) > assistantDialectReadRecoveryFileLimit || intent.OriginalRunSHA256 != digestOf(assistantChatJSON(g.run)) || intent.ExpectedAbsent != (prior == nil) || intent.ExpectedSHA256 != digestOf(prior) {
		return errAssistantRunScope
	}
	info, e := g.private.Stat()
	if e != nil || !os.SameFile(info, op.writer.config.domain.identity) {
		return errors.Join(errAssistantRunScope, e)
	}
	hold := &assistantDialectRecoveryPrivateHold{operation: op, config: op.writer.config, lock: g.private, info: info}
	hold.self = hold
	op.private = hold
	op.operationID = row.OperationID
	op.requestID = row.LatestObservation.RequestID
	op.intent = intent
	op.intent.Bytes = bytes.Clone(intent.Bytes)
	op.prior = bytes.Clone(prior)
	g.private = nil // exact ownership moves; first guard Close cannot release turn.
	recoveryStorageStep("private-transferred")
	return nil
}
func (op *assistantDialectReadRecoveryStorageOperation) Close() error {
	if op == nil {
		return nil
	}
	if op.self != op {
		return errAssistantRunScope
	}
	// Revocation is immediate, even while an already-entered completion owns
	// the lifetime barrier. Actual release waits outside every host prefix.
	op.revoked.Store(true)
	recoveryStorageStep("operation-close-revoked")
	op.mu.Lock()
	defer op.mu.Unlock()
	if op.closed {
		return op.closeErr
	}
	if op.private != nil {
		e := op.private.Close()
		if e == storagebudget.ErrBusy {
			return e
		}
		op.closeErr = errors.Join(op.closeErr, e)
	}
	op.closed = true
	if op.capacity != nil {
		op.closeErr = errors.Join(op.closeErr, op.capacity.Close())
		recoveryStorageStep("capacity-closed")
	}
	if op.use != nil {
		op.closeErr = errors.Join(op.closeErr, op.use.Close())
		recoveryStorageStep("use-closed")
	}
	if op.home != nil {
		op.closeErr = errors.Join(op.closeErr, op.home.Close())
		recoveryStorageStep("home-closed")
	}
	op.closeErr = errors.Join(op.closeErr, op.writer.owner.drop())
	clear(op.prior)
	clear(op.intent.Bytes)
	op.cancel()
	op.writer.releaseOperation(op)
	// The operation-owned exclusion outlives every nested permit/pass/borrower.
	if op.authority != nil {
		op.closeErr = errors.Join(op.closeErr, op.authority.Close())
	}
	return op.closeErr
}
func recoveryStorageOperationKey(run assistantRunRecord, operation, request string) string {
	return "read-recovery:" + digestOf(assistantChatJSON([]string{run.Binding.Project, run.ID, operation, request, digestOf(assistantChatJSON(run))}))
}
func (w *assistantDialectReadRecoveryStorageWriter) reserveOriginalDialectReadRecovery(ctx context.Context, p *assistantDialectReadRecoveryPin, intent assistantDialectReadRecoveryWriteIntent) (assistantDialectReadRecoveryWriteAdmission, error) {
	if p == nil || p.storage == nil || p.storage.writer != w {
		return nil, errAssistantDialectReadRecoveryBudget
	}
	op := p.storage
	op.mu.Lock()
	defer op.mu.Unlock()
	if e := op.current(ctx); e != nil {
		return nil, e
	}
	if op.private == nil || op.admission != nil || !reflect.DeepEqual(intent, op.intent) {
		return nil, errAssistantRunScope
	}
	if e := op.private.current(ctx); e != nil {
		return nil, e
	}
	value, e := decodeAssistantDialectReadRecovery(intent.Target, intent.Bytes)
	if e != nil {
		return nil, e
	}
	row := assistantDialectReadRecoveryOf(value, op.operationID)
	if row == nil || row.LatestObservation.RequestID != op.requestID || row.LatestObservation.BaselineRunSHA256 != intent.OriginalRunSHA256 {
		return nil, errAssistantRunScope
	}
	entries, e := storageEntries(w.root.root)
	if e != nil {
		return nil, e
	}
	if len(entries) >= maxStorageFiles {
		return nil, storagebudget.ErrBounds
	} // stage needs one namespace slot.
	plan := storageSingleFilePlan{Root: w.root, Target: intent.Target, Stage: intent.Stage, Workflow: "dialect-read-recovery-v1", Operation: recoveryStorageOperationKey(p.run, row.OperationID, row.LatestObservation.RequestID), ExpectedAbsent: intent.ExpectedAbsent, Expected: op.prior, Bytes: intent.Bytes, ObjectLimit: assistantDialectReadRecoveryFileLimit}
	single, e := w.owner.admitSingleFilePrepared(ctx, plan, op)
	if e != nil {
		return nil, errors.Join(errAssistantDialectReadRecoveryBudget, e)
	}
	a := &assistantDialectReadRecoveryStorageAdmission{operation: op, single: single}
	a.self = a
	op.admission = a
	recoveryStorageStep("reserved")
	return a, nil
}

type assistantDialectReadRecoveryStorageAdmission struct {
	self              *assistantDialectReadRecoveryStorageAdmission
	mu                sync.Mutex
	operation         *assistantDialectReadRecoveryStorageOperation
	single            *storageSingleFileAdmission
	verified          *assistantDialectReadRecoveryGuard
	publication       *storageSingleFilePublication
	completed, closed bool
	closeErr          error
}

func (a *assistantDialectReadRecoveryStorageAdmission) writeOriginalDialectReadRecovery(ctx context.Context, g *assistantDialectReadRecoveryGuard, intent assistantDialectReadRecoveryWriteIntent) error {
	if a == nil || a.self != a {
		return errAssistantRunScope
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.publication != nil || g == nil || g.pin != a.operation.pin || g.privateBorrow != a.operation.private || !reflect.DeepEqual(intent, a.operation.intent) {
		return errAssistantRunScope
	}
	if e := g.Check(ctx); e != nil {
		return e
	}
	if e := a.single.write(ctx, true); e != nil {
		return e
	}
	a.publication = a.single.publication
	recoveryStorageStep("published")
	return a.operation.writer.owner.fail("recovery", "after-publication")
}
func (a *assistantDialectReadRecoveryStorageAdmission) verifyOriginalDialectReadRecovery(ctx context.Context, g *assistantDialectReadRecoveryGuard) error {
	if a == nil || a.self != a {
		return errAssistantRunScope
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.publication == nil || a.verified != nil || g == nil || g.pin != a.operation.pin || g.privateBorrow != a.operation.private {
		return errAssistantRunScope
	}
	if e := g.Check(ctx); e != nil {
		return e
	}
	a.verified = g
	return nil
}
func (a *assistantDialectReadRecoveryStorageAdmission) completeOriginalDialectReadRecovery(ctx context.Context, g *assistantDialectReadRecoveryGuard) error {
	if a == nil || a.self != a {
		return errAssistantRunScope
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.completed || a.verified != g || g == nil || !g.closed || g.closeErr != nil || a.publication == nil {
		return errAssistantRunScope
	}
	// The consumer guard is closed: no host/lease mutex is held here. Keep
	// operation custody continuously through all terminal/control persistence.
	// Close revokes before waiting on this barrier; current() never relocks it.
	op := a.operation
	op.mu.Lock()
	defer op.mu.Unlock()
	if e := op.current(ctx); e != nil {
		return e
	}
	a.completed = true
	if e := op.writer.owner.fail("recovery", "before-completion"); e != nil {
		return e
	}
	if e := a.single.completePublication(ctx, a.publication); e != nil {
		return e
	}
	recoveryStorageStep("completed")
	return nil
}
func (a *assistantDialectReadRecoveryStorageAdmission) Close() error {
	if a == nil {
		return nil
	}
	if a.self != a {
		return errAssistantRunScope
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return a.closeErr
	}
	a.closed = true
	a.closeErr = errors.Join(a.single.Close(), a.operation.private.Close(), a.operation.writer.owner.fail("recovery", "custody-close"))
	return a.closeErr
}

// RC2: bounded history is proof only while the exact original outcome remains.
// Missing/evicted/corrupt/foreign/ambiguous history never grants another attempt.
func (w *assistantDialectReadRecoveryStorageWriter) recognizeOriginalDialectReadRecovery(ctx context.Context, g *assistantDialectReadRecoveryGuard, value assistantDialectReadRecovery, raw []byte, row assistantDialectReadRecoveryRecord) error {
	if g == nil || g.pin.storage == nil || g.pin.storage.writer != w || value.Revision == 0 {
		return errAssistantDialectReadRecoveryBudget
	}
	if e := g.Check(ctx); e != nil {
		return e
	}
	decoded, e := decodeAssistantDialectReadRecovery(assistantDialectReadRecoveryName(g.run.Binding.Project, g.run.ID), raw)
	selected := assistantDialectReadRecoveryOf(value, row.OperationID)
	if e != nil || validateAssistantDialectReadRecoveryBound(decoded, g.run) != nil || !reflect.DeepEqual(decoded, value) || selected == nil || !reflect.DeepEqual(*selected, row) || row.LatestObservation.BaselineRunRevision != g.run.Revision || row.LatestObservation.BaselineRunSHA256 != digestOf(assistantChatJSON(g.run)) {
		return errors.Join(errAssistantDialectReadRecoveryBudget, e)
	}
	if e := w.matchData(g.data); e != nil {
		return e
	}
	target := assistantDialectReadRecoveryName(g.run.Binding.Project, g.run.ID)
	stage := ".data-read-recovery-" + digestOf(assistantChatJSON([]any{target, value.Revision - 1, row.LatestObservation.RequestID, digestOf(raw)}))
	operation := recoveryStorageOperationKey(g.run, row.OperationID, row.LatestObservation.RequestID)
	e = w.owner.withControl(ctx, func(ctx context.Context, table *storageIntentTable) error {
		status, e := w.owner.ref.cell.q.InspectHostState(ctx)
		if e != nil {
			return e
		}
		if e = w.owner.validateIntentMappings(ctx, table, status); e != nil {
			return e
		}
		found := 0
		for _, slot := range table.slots {
			r := slot.record
			if r.Workflow != "dialect-read-recovery-v1" || r.Operation != operation {
				continue
			}
			found++
			if len(r.Components) == 0 || r.Components[0].Root != w.root.row.ID || r.Components[0].Category != storagebudget.Control || r.Components[0].Physical != (storagebudget.Vector{Bytes: uint64(len(raw)), Files: 1}) {
				return errAssistantDialectReadRecoveryBudget
			}
			if r.Phase != storageIntentRetired || r.Outcome != storageIntentPublished || r.Target != target || r.Stage != stage || r.NewLength != uint64(len(raw)) || !r.StageExists || r.PolicyRevision == 0 {
				return errAssistantDialectReadRecoveryBudget
			}
			ticket := status.Slots[r.Token.Slot]
			if ticket.Ticket.State != storagebudget.Terminal || ticket.Ticket.Token != r.Token || ticket.PersistedApplied != r.Token || ticket.PersistedNextSequence <= r.Token.Sequence || !storageIntentMatchingTicket(r, ticket.Ticket, w.owner.ref.cell.binding) {
				return errAssistantDialectReadRecoveryBudget
			}
			if _, e := g.data.root.Lstat(stage); !errors.Is(e, os.ErrNotExist) {
				return errors.Join(errAssistantDialectReadRecoveryBudget, e)
			}
			info, e := g.data.root.Lstat(target)
			if e != nil {
				return e
			}
			id, e := storageFileIdentity(info)
			if e != nil || id != r.StageIdentity || uint64(info.Size()) != r.NewLength {
				return errors.Join(errAssistantDialectReadRecoveryBudget, e)
			}
			for _, component := range r.Components {
				if w.owner.roots[component.Root] == nil || !w.owner.intentCapacityMatches(w.owner.roots[component.Root], component) {
					return errAssistantDialectReadRecoveryBudget
				}
			}
		}
		if found != 1 {
			return errAssistantDialectReadRecoveryBudget
		}
		return ctx.Err()
	})
	if e != nil {
		return errors.Join(errAssistantDialectReadRecoveryBudget, e)
	}
	return g.Check(ctx)
}
