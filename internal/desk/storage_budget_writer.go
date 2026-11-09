package desk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path"
	"sort"
	"sync"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// The original domain selects the exact names, expected raw baseline and its
// established finite object limit. This API adds no generic 2 MiB write limit.
// An admission never grants action permission or substitutes a serialized ID
// for the live original root/domain/use lifetime. Raw bytes stay only in memory.
type storageSingleFilePlan struct {
	Root                               *storageRootBinding
	Target, Stage, Workflow, Operation string
	ExpectedAbsent                     bool
	Expected                           []byte
	Bytes                              []byte
	ObjectLimit                        uint64
}
type storageSingleFileAdmission struct {
	self              *storageSingleFileAdmission
	mu                sync.Mutex
	owner             *storageBudgetOwner
	plan              storageSingleFilePlan
	use               *storageInstallationUse
	home              *os.File
	oldTarget         *os.File
	domain            *storageDomainHold
	authority         *storageMutationAuthority
	ownsAuthority     bool
	recoveryPrepared  *assistantDialectReadRecoveryStorageOperation
	publication       *storageSingleFilePublication
	parent            *os.Root
	parents           []storageNamedRoot
	target, stage     string
	slot              uint16
	generation        uint64
	operationID       storagebudget.ID
	token             storagebudget.Token
	closed, attempted bool
}

func (a *storageSingleFileAdmission) current(ctx context.Context) error {
	if a == nil || a.self != a || a.closed || a.owner == nil || a.domain == nil && a.recoveryPrepared == nil || a.use == nil {
		return storagebudget.ErrAuthority
	}
	if e := a.authority.current(ctx, true); e != nil {
		return e
	}
	ctx = storageMutationContext(ctx, a.authority)
	if a.recoveryPrepared != nil {
		if e := a.recoveryPrepared.current(ctx); e != nil {
			return e
		}
		if a.recoveryPrepared.capacity == nil {
			return storagebudget.ErrAuthority
		}
		ctx = a.recoveryPrepared.capacity.context(ctx)
	}
	if a.domain != nil {
		if e := a.domain.current(ctx); e != nil {
			return e
		}
	} else if e := a.recoveryPrepared.private.current(ctx); e != nil {
		return e
	}
	for _, p := range a.parents {
		if e := p.current(); e != nil {
			return e
		}
	}
	if _, ok := a.owner.observer.(*storageAdvisoryObserver); ok {
		if _, present := ctx.Value(storageCapacityPassKey{}).(*storageCapacityPass); !present {
			p, e := a.owner.beginStorageCapacityPass(ctx, []storageCapacityEffectParent{{binding: a.plan.Root, root: a.parent, witnesses: a.parents}})
			if e != nil {
				return e
			}
			defer p.Close()
			ctx = p.context(ctx)
		}
	}
	// Exact held parents must still belong to the enrolled uniform domain
	// before any later effect. A changed quota/capability relation retains the
	// existing ticket; these metadata checks grant no replacement permission.
	if _, e := a.owner.rootStorageSample(ctx, a.plan.Root, a.parent); e != nil {
		return e
	}
	for _, r := range a.owner.roots {
		if r.configControl {
			if _, e := a.owner.rootStorageSample(ctx, r, r.root); e != nil {
				return e
			}
		}
	}
	return nil
}
func (a *storageSingleFileAdmission) closeLocked() (err error) {
	if a.closed {
		return nil
	}
	a.closed = true
	if a.oldTarget != nil {
		err = errors.Join(err, a.oldTarget.Close())
	}
	for i := len(a.parents) - 1; i >= 0; i-- {
		err = errors.Join(err, a.parents[i].root.Close())
	}
	if a.domain != nil {
		err = errors.Join(err, a.domain.Close())
	}
	if a.use != nil && a.recoveryPrepared == nil {
		err = errors.Join(err, a.use.Close())
	}
	if a.home != nil {
		err = errors.Join(err, a.home.Close())
	}
	if a.owner != nil {
		err = errors.Join(err, a.owner.drop())
	}
	if a.ownsAuthority && a.authority != nil {
		err = errors.Join(err, a.authority.Close())
	}
	clear(a.plan.Bytes)
	clear(a.plan.Expected)
	return err
}
func (a *storageSingleFileAdmission) Close() error {
	if a == nil || a.self != a {
		return storagebudget.ErrAuthority
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.closeLocked()
}
func (a *storageSingleFileAdmission) originalRecord(t *storageIntentTable) (storageIntentRecord, error) {
	if int(a.slot) >= len(t.slots) {
		return storageIntentRecord{}, storagebudget.ErrAuthority
	}
	r := t.slots[a.slot].record
	if r.Generation != a.generation || r.OperationID != a.operationID || r.Token != a.token || r.Target != a.plan.Target || r.Stage != a.plan.Stage || r.Workflow != a.plan.Workflow || r.Operation != a.plan.Operation || r.Phase != storageIntentStaging {
		return storageIntentRecord{}, storagebudget.ErrStale
	}
	return r, nil
}

// storageReadOriginalFile returns an independently opened original description.
// The caller owns its actual Close; every failure closes it before returning.
func storageReadOriginalFile(root *os.Root, name string, limit uint64) (raw []byte, held *os.File, info os.FileInfo, err error) {
	info, err = root.Lstat(name)
	if err != nil {
		return nil, nil, nil, err
	}
	if !storageCensusPrivateFile(info) || uint64(info.Size()) > limit {
		return nil, nil, nil, storagebudget.ErrAuthority
	}
	links, ok := linkCount(info)
	if !ok || links != 1 {
		return nil, nil, nil, storagebudget.ErrAuthority
	}
	n, err := storageOpenFile(root, name, info.Size())
	if err != nil {
		return nil, nil, nil, err
	}
	held = n.file
	defer func() {
		if err != nil {
			err = errors.Join(err, n.file.Close())
			raw, held, info = nil, nil, nil
		}
	}()
	raw = make([]byte, int(info.Size()))
	count, re := held.ReadAt(raw, 0)
	if (re != nil && !(len(raw) == 0 && errors.Is(re, io.EOF))) || count != len(raw) {
		return nil, nil, nil, errors.Join(storagebudget.ErrUnknown, re)
	}
	after, err := root.Lstat(name)
	if err != nil || !os.SameFile(info, after) || info.Size() != after.Size() || info.Mode() != after.Mode() || !info.ModTime().Equal(after.ModTime()) {
		return nil, nil, nil, storagebudget.ErrStale
	}
	return raw, held, info, nil
}
func storageOriginalFile(root *os.Root, name string, limit uint64) ([]byte, os.FileInfo, error) {
	raw, held, info, err := storageReadOriginalFile(root, name, limit)
	if err != nil {
		return nil, nil, err
	}
	if err = held.Close(); err != nil {
		return nil, nil, errors.Join(storagebudget.ErrUnknown, err)
	}
	return raw, info, nil
}
func (a *storageSingleFileAdmission) baseline(ctx context.Context, retain bool) (info os.FileInfo, err error) {
	if err = a.current(ctx); err != nil {
		return nil, err
	}
	raw, held, info, err := storageReadOriginalFile(a.parent, a.target, uint64(len(a.plan.Expected)))
	if a.plan.ExpectedAbsent {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return nil, errors.Join(storagebudget.ErrStale, held.Close())
	}
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(raw, a.plan.Expected) {
		return nil, errors.Join(storagebudget.ErrStale, held.Close())
	}
	if retain {
		// Keep the admitted original inode allocated through publication and
		// final Close; unlink/recreate cannot recycle its inode number into a
		// new target that impersonates the admitted durable OldIdentity.
		a.oldTarget = held
		return info, nil
	}
	if err = held.Close(); err != nil {
		return nil, errors.Join(storagebudget.ErrUnknown, err)
	}
	return info, nil
}
func (a *storageSingleFileAdmission) admittedBaseline(ctx context.Context, admitted storageIntentRecord) (os.FileInfo, error) {
	info, err := a.baseline(ctx, false)
	if err != nil {
		return nil, err
	}
	if admitted.OldExists != (info != nil) {
		return nil, storagebudget.ErrStale
	}
	if !admitted.OldExists {
		if a.oldTarget != nil {
			return nil, storagebudget.ErrAuthority
		}
		return nil, nil
	}
	if a.oldTarget == nil {
		return nil, storagebudget.ErrAuthority
	}
	original, err := a.oldTarget.Stat()
	if err != nil || !storagePrivate(original, false) {
		return nil, storagebudget.ErrStale
	}
	identity, err := storageFileIdentity(info)
	originalIdentity, originalErr := storageFileIdentity(original)
	links, known := linkCount(info)
	originalLinks, originalKnown := linkCount(original)
	if err != nil || originalErr != nil || identity != admitted.OldIdentity || originalIdentity != admitted.OldIdentity || !os.SameFile(info, original) || uint64(info.Size()) != admitted.OldLength || uint64(original.Size()) != admitted.OldLength || !known || !originalKnown || uint64(links) != admitted.OldLinkCount || uint64(originalLinks) != admitted.OldLinkCount {
		return nil, storagebudget.ErrStale
	}
	return info, nil
}

// Close the retained original only after observing its exact removal. An
// alias or unknown Close keeps the full promise; settlement cannot credit it.
func (a *storageSingleFileAdmission) closeRemovedOriginal(admitted storageIntentRecord) error {
	if !admitted.OldExists {
		if a.oldTarget != nil {
			return storagebudget.ErrAuthority
		}
		return nil
	}
	if a.oldTarget == nil {
		return storagebudget.ErrAuthority
	}
	info, err := a.oldTarget.Stat()
	if err != nil {
		return errors.Join(storagebudget.ErrUnknown, err)
	}
	identity, err := storageFileIdentity(info)
	links, known := linkCount(info)
	if err != nil || identity != admitted.OldIdentity || uint64(info.Size()) != admitted.OldLength || !known || links != 0 {
		return storagebudget.ErrUnknown
	}
	err = errors.Join(a.oldTarget.Close(), a.owner.fail("original", "close"))
	a.oldTarget = nil
	if err != nil {
		return errors.Join(storagebudget.ErrUnknown, err)
	}
	return nil
}
func storageOwnerComponent(r *storageRootBinding, workspace storagebudget.ID, category uint8, physical, attribution, settlement storagebudget.Vector) storagebudget.Component {
	o := r.owner
	fs := o.filesystems[r.row.Filesystem]
	c := storagebudget.Component{Filesystem: fs.ID, FilesystemGeneration: fs.Generation, Root: r.row.ID, RootGeneration: r.row.Generation, Owner: r.row.Owner, Workspace: workspace, Category: category, Physical: physical, Attribution: attribution, SettlementPhysical: settlement, CapacityCount: r.row.CapacityCount, CapacityBinding: r.row.CapacityBinding}
	if workspace != (storagebudget.ID{}) {
		c.WorkspaceGeneration = o.workspaces[workspace].Generation
	}
	for i, ref := range r.row.CapacityRefs {
		if i < int(r.row.CapacityCount) {
			c.Capacity[i] = storagebudget.CapacityPromise{Constraint: ref.Constraint, Generation: ref.Generation}
		}
	}
	return c
}
func (o *storageBudgetOwner) writerComponents(ctx context.Context, plan storageSingleFilePlan, parent *os.Root) ([]storagebudget.Component, error) {
	r := plan.Root
	category, refs := r.categoryOf(plan.Target)
	if r.qControl || plan.Target == r.domainName || plan.Stage == r.domainName || r.fixedControl(plan.Target) || r.fixedControl(plan.Stage) {
		return nil, storagebudget.ErrAuthority
	}
	physical := storagebudget.Vector{Bytes: uint64(len(plan.Bytes)), Files: 1}
	if len(refs) > storagebudget.MaxWorkspaces {
		return nil, storagebudget.ErrBounds
	}
	fanout := len(refs) > storagebudget.MaxComponents-1
	first := storagebudget.ID{}
	if len(refs) > 0 && !fanout {
		first = refs[0]
	}
	attribution := storagebudget.Vector{}
	if len(refs) > 0 {
		attribution = physical
	}
	c := []storagebudget.Component{storageOwnerComponent(r, first, category, physical, attribution, storagebudget.Vector{})}
	for _, id := range refs[min(1, len(refs)):] {
		if fanout {
			break
		}
		c = append(c, storageOwnerComponent(r, id, category, storagebudget.Vector{}, physical, storagebudget.Vector{}))
	}
	var control *storageRootBinding
	for _, root := range o.roots {
		if root.configControl {
			control = root
		}
	}
	if control == nil {
		return nil, storagebudget.ErrAuthority
	}
	// The fixed journal fits its already charged host floor. Retain a separate
	// bounded settlement promise until all actual persistence/readback completes.
	merged := false
	for i := range c {
		if c[i].Root == control.row.ID && c[i].Workspace == (storagebudget.ID{}) && c[i].Category == storagebudget.Control {
			c[i].SettlementPhysical = storagebudget.Vector{Bytes: 64 << 10}
			merged = true
		}
	}
	if !merged {
		c = append(c, storageOwnerComponent(control, storagebudget.ID{}, storagebudget.Control, storagebudget.Vector{}, storagebudget.Vector{}, storagebudget.Vector{Bytes: 64 << 10}))
	}
	if len(c) > storagebudget.MaxComponents {
		return nil, storagebudget.ErrBounds
	}
	// Each physical component receives an explicitly authored complete peak. The
	// backend inspects the exact held parent, not merely the root path or file size.
	for i := range c {
		component := &c[i]
		root := o.roots[component.Root]
		held := root.root
		if root == r {
			held = parent
		}
		if _, e := o.rootStorageSample(ctx, root, held); e != nil {
			return nil, e
		}
		// Merged logical components can grow in two actual parents. Author the
		// data envelope at its held target parent, control settlement at the original
		// control root, then add each independent peak with finite checked arithmetic.
		for _, allocation := range []struct {
			parent *os.Root
			demand storageCapacityDemand
		}{
			{held, storageCapacityDemand{Physical: component.Physical}},
			{root.root, storageCapacityDemand{Settlement: component.SettlementPhysical}},
		} {
			if allocation.demand.Physical == (storagebudget.Vector{}) && allocation.demand.Settlement == (storagebudget.Vector{}) {
				continue
			}
			if _, e := o.rootStorageSample(ctx, root, allocation.parent); e != nil {
				return nil, e
			}
			peaks, e := o.observer.storageCapacityEnvelope(ctx, allocation.parent, allocation.demand)
			if e != nil {
				return nil, e
			}
			for j, peak := range peaks {
				prior := component.Capacity[j].Peak
				if peak.Bytes > 1<<60 || prior.Bytes > 1<<60-peak.Bytes || peak.Inodes > 1<<40 || prior.Inodes > 1<<40-peak.Inodes {
					return nil, storagebudget.ErrBounds
				}
				component.Capacity[j].Peak = storagebudget.CapacityVector{Bytes: prior.Bytes + peak.Bytes, Inodes: prior.Inodes + peak.Inodes}
			}
		}
		if !o.intentCapacityMatches(root, *component) {
			return nil, storagebudget.ErrUnknown
		}
	}
	return c, nil
}

// Snapshot the union before sampling, outside Q. A concurrent new obligation
// using another constraint makes Reserve refuse; this observation cannot admit it.
func (o *storageBudgetOwner) writerObservations(ctx context.Context, components []storagebudget.Component) ([]storagebudget.CapacityObservation, error) {
	if _, ok := o.observer.(*storageAdvisoryObserver); ok {
		p, e := storagePassFrom(ctx, o)
		if e != nil {
			return nil, e
		}
		// Every enrolled view and exact effect parent already contributed to Kind7.
		// Keep independent exact rows in addition, with conservative same-ID minima
		// across non-atomic queries and the oldest contributing query timestamp.
		observed := map[storagebudget.ID]storagebudget.CapacityObservation{}
		for _, s := range p.samples {
			for _, obs := range s.Observations {
				row, ok := o.constraints[obs.Constraint]
				if !ok || obs.Generation != row.Generation || obs.KnownResources != row.Resources || obs.At.IsZero() {
					return nil, storagebudget.ErrUnknown
				}
				if old, ok := observed[obs.Constraint]; ok {
					obs.Available.Bytes = min(obs.Available.Bytes, old.Available.Bytes)
					obs.Available.Inodes = min(obs.Available.Inodes, old.Available.Inodes)
					if old.At.Before(obs.At) {
						obs.At = old.At
					}
				}
				observed[obs.Constraint] = obs
			}
		}
		out := make([]storagebudget.CapacityObservation, 0, len(observed))
		for _, obs := range observed {
			out = append(out, obs)
		}
		sort.Slice(out, func(i, j int) bool { return string(out[i].Constraint[:]) < string(out[j].Constraint[:]) })
		return out, p.current(ctx)
	}
	v, e := o.ref.cell.q.InspectHost(ctx)
	if e != nil {
		return nil, e
	}
	all := append([]storagebudget.Component(nil), components...)
	for _, slot := range v.Slots {
		if slot.Ticket.State != storagebudget.Empty && slot.Ticket.State != storagebudget.Terminal {
			all = append(all, slot.Ticket.Components...)
		}
	}
	required := map[storagebudget.ID]bool{}
	roots := map[storagebudget.ID]*storageRootBinding{}
	for _, c := range all {
		r := o.roots[c.Root]
		if r == nil || !o.intentCapacityMatches(r, c) {
			return nil, storagebudget.ErrUnknown
		}
		roots[r.row.ID] = r
		for i, p := range c.Capacity {
			if i < int(c.CapacityCount) && p.Peak != (storagebudget.CapacityVector{}) {
				required[p.Constraint] = true
			}
		}
	}
	observed := map[storagebudget.ID]storagebudget.CapacityObservation{}
	// Canonical traversal also makes corroboration and refusal deterministic.
	for _, r := range o.orderedRoots() {
		if roots[r.row.ID] == nil {
			continue
		}
		sample, e := o.rootStorageSample(ctx, r, r.root)
		if e != nil {
			return nil, e
		}
		if len(sample.Observations) != int(sample.CapacityCount) {
			return nil, storagebudget.ErrUnknown
		}
		seen := map[storagebudget.ID]bool{}
		for _, obs := range sample.Observations {
			row, ok := o.constraints[obs.Constraint]
			member := false
			for i, ref := range sample.CapacityRefs {
				if i < int(sample.CapacityCount) && ref.Constraint == obs.Constraint {
					member = true
				}
			}
			if !ok || !member || seen[obs.Constraint] || obs.Generation != row.Generation || obs.KnownResources != row.Resources || obs.Available.Bytes > 1<<60 || obs.Available.Inodes > 1<<40 || row.Resources&storagebudget.CapacityBytes == 0 && obs.Available.Bytes != 0 || row.Resources&storagebudget.CapacityInodes == 0 && obs.Available.Inodes != 0 || obs.At.IsZero() {
				return nil, storagebudget.ErrUnknown
			}
			seen[obs.Constraint] = true
			if old, ok := observed[obs.Constraint]; ok {
				// Independent restrictions have distinct IDs. Corroboration of one exact
				// counter must agree; never take a cross-volume minimum as a new pool.
				if old.Generation != obs.Generation || old.KnownResources != obs.KnownResources || old.Available != obs.Available {
					return nil, storagebudget.ErrUnknown
				}
				if old.At.Before(obs.At) {
					obs.At = old.At
				}
			}
			observed[obs.Constraint] = obs
		}
	}
	out := make([]storagebudget.CapacityObservation, 0, len(required))
	for id := range required {
		obs, ok := observed[id]
		if !ok {
			return nil, storagebudget.ErrUnknown
		}
		out = append(out, obs)
	}
	sort.Slice(out, func(i, j int) bool { return string(out[i].Constraint[:]) < string(out[j].Constraint[:]) })
	return out, nil
}
func storageDefinitelyRejected(err error) storageIntentRejection {
	// Exact equality is deliberate: errors.Is on a joined cleanup/persistence
	// unknown could turn an uncertain Reserve into a cheaper reusable slot.
	switch err {
	case storagebudget.ErrLimit:
		return storageIntentQuota
	case storagebudget.ErrHeadroom:
		return storageIntentHeadroom
	case storagebudget.ErrBusy:
		return storageIntentBusy
	}
	return storageIntentNoRejection
}
func (o *storageBudgetOwner) admitSingleFile(ctx context.Context, plan storageSingleFilePlan) (*storageSingleFileAdmission, error) {
	return o.admitSingleFilePrepared(ctx, plan, nil)
}
func (o *storageBudgetOwner) admitSingleFilePrepared(ctx context.Context, plan storageSingleFilePlan, prepared *assistantDialectReadRecoveryStorageOperation) (a *storageSingleFileAdmission, err error) {
	if plan.Root == nil || plan.Root.self != plan.Root || plan.Root.owner != o || plan.Root.transferInactive || !storageIntentRelative(plan.Target) || !storageIntentRelative(plan.Stage) || plan.Target == plan.Stage || path.Dir(plan.Target) != path.Dir(plan.Stage) || !storageIntentText(plan.Workflow, 128, false) || !storageIntentText(plan.Operation, 128, false) || plan.ObjectLimit == 0 || plan.ObjectLimit > 1<<60 || uint64(len(plan.Bytes)) > plan.ObjectLimit || uint64(len(plan.Expected)) > plan.ObjectLimit || plan.ExpectedAbsent && len(plan.Expected) != 0 {
		return nil, storagebudget.ErrAuthority
	}
	if e := o.retain(); e != nil {
		return nil, e
	}
	permit := &storageSingleFileAdmission{owner: o, plan: plan}
	permit.self = permit
	permit.plan.Bytes = append([]byte(nil), plan.Bytes...)
	permit.plan.Expected = append([]byte(nil), plan.Expected...)
	defer func() {
		if err != nil {
			err = errors.Join(err, permit.closeLocked())
			a = nil
		}
	}()
	var e error
	if prepared == nil {
		permit.authority, e = o.beginAdmission(ctx)
		if e != nil {
			return nil, e
		}
		permit.ownsAuthority = true
	} else {
		permit.authority = prepared.authority
		if e = permit.authority.current(ctx, true); e != nil {
			return nil, e
		}
	}
	ctx = storageMutationContext(ctx, permit.authority)
	if prepared == nil {
		home, err := storageOpenOriginalControl(o.ref.cell, ".home-use.lock")
		if err != nil {
			return nil, err
		}
		permit.home = home.file
		if e = lockPrivateData(home.file, false); e != nil {
			return nil, storagebudget.ErrBusy
		}
		permit.use, e = o.ref.acquire()
		if e != nil {
			return nil, e
		}
	} else {
		if prepared.self != prepared || prepared.writer.owner != o || prepared.writer.root != plan.Root || prepared.private == nil {
			return nil, storagebudget.ErrAuthority
		}
		if e = prepared.current(ctx); e != nil {
			return nil, e
		}
		permit.recoveryPrepared = prepared
		permit.use = prepared.use
		if prepared.capacity == nil {
			return nil, storagebudget.ErrAuthority
		}
		ctx = prepared.capacity.context(ctx)
	}
	if prepared != nil && prepared.private.matchesRoot(plan.Root) {
		if e = prepared.private.current(ctx); e != nil {
			return nil, e
		}
	} else {
		permit.domain, e = o.domain(ctx, plan.Root, permit.use)
		if e != nil {
			return nil, e
		}
	}
	permit.parent = plan.Root.root
	directory := path.Dir(plan.Target)
	if directory != "." {
		for _, name := range stringsSplitStoragePath(directory) {
			p, e := storagePinChild(permit.parent, name, true)
			if e != nil {
				return nil, e
			}
			permit.parents = append(permit.parents, p)
			identity, e := storageDirectoryID(p.root)
			if e != nil {
				return nil, e
			}
			for _, bound := range o.roots {
				if bound != plan.Root && bound.directory == identity {
					return nil, storagebudget.ErrAuthority
				}
			}
			permit.parent = p.root
		}
	}
	if _, ok := o.observer.(*storageAdvisoryObserver); ok && prepared == nil {
		p, e := o.beginStorageCapacityPass(ctx, []storageCapacityEffectParent{{binding: plan.Root, root: permit.parent, witnesses: permit.parents}})
		if e != nil {
			return nil, e
		}
		defer p.Close()
		ctx = p.context(ctx)
	}
	permit.target = path.Base(plan.Target)
	permit.stage = path.Base(plan.Stage)
	old, e := permit.baseline(ctx, true)
	if e != nil {
		return nil, e
	}
	if _, e = permit.parent.Lstat(permit.stage); !errors.Is(e, os.ErrNotExist) {
		if e != nil {
			return nil, e
		}
		return nil, os.ErrExist
	}
	components, e := o.writerComponents(ctx, plan, permit.parent)
	if e != nil {
		return nil, e
	}
	observations, e := o.writerObservations(ctx, components)
	if e != nil {
		return nil, e
	}
	admissionParent := ctx
	e = o.withControl(ctx, func(ctx context.Context, table *storageIntentTable) error {
		if e := permit.current(ctx); e != nil {
			return e
		}
		if e := o.requireMutation(ctx); e != nil {
			return e
		}
		view, e := o.ref.cell.q.InspectHost(ctx)
		if e != nil {
			return e
		}
		status := storageHostState(view)
		if status.Maintenance {
			return storagebudget.ErrBusy
		}
		add, e := o.registrationMissingFrom(view)
		if e != nil {
			return e
		}
		if storageRegistrationCount(add) != 0 {
			return storagebudget.ErrUnknown
		}
		if e = o.validateIntentMappings(ctx, table, status); e != nil {
			return e
		}
		for _, slot := range table.slots {
			r := slot.record
			if !r.reusable() && r.Workflow == plan.Workflow && r.Operation == plan.Operation {
				return errStorageOwnerRecovery
			}
		}
		if e = o.requireMutation(ctx); e != nil {
			return e
		}
		slot := -1
		for i, s := range table.slots {
			if s.record.Phase == storageIntentEmpty {
				slot = i
				break
			}
		}
		if slot < 0 {
			for i, s := range table.slots {
				if s.record.reusable() && s.record.Generation != ^uint64(0) {
					slot = i
					break
				}
			}
		}
		if slot < 0 {
			return storagebudget.ErrBounds
		}
		previous := table.slots[slot]
		id, e := storagebudget.NewID()
		if e != nil {
			return e
		}
		r := storageIntentRecord{Slot: uint16(slot), Generation: previous.record.Generation + 1, Phase: storageIntentPrepared, OperationID: id, AdmissionEpoch: status.Epoch, NewLength: uint64(len(plan.Bytes)), Target: plan.Target, Stage: plan.Stage, Workflow: plan.Workflow, Operation: plan.Operation, Components: components, Changes: make([]storagebudget.Change, len(components))}
		if components[0].Workspace == (storagebudget.ID{}) && components[0].Attribution != (storagebudget.Vector{}) {
			_, binding, err := storageSingleFileFanout(r, o.roots, o.workspaces)
			if err != nil {
				return err
			}
			r.Fanout = &storageIntentFanout{Binding: binding}
		}
		if previous.record.Phase != storageIntentEmpty {
			r.PreviousPageDigest = sha256.Sum256(previous.page)
		}
		if old != nil {
			r.OldExists = true
			r.OldLength = uint64(old.Size())
			r.OldLinkCount = 1
			r.OldIdentity, e = storageFileIdentity(old)
			if e != nil {
				return e
			}
		}
		for _, c := range components {
			root := o.roots[c.Root]
			r.RootIdentities = append(r.RootIdentities, root.row.Identity)
			r.FilesystemIdentities = append(r.FilesystemIdentities, o.filesystems[c.Filesystem].Identity)
			r.Locators = append(r.Locators, root.row.Locator)
		}
		// CASBinding covers only original metadata and nonce, never baseline bytes.
		metadata := storageMetadataDigest("desk-storage-single-file-cas-v1", r.OldIdentity, plan.Root.directory)
		r.CASBinding = sha256.Sum256(append(append(append([]byte(nil), metadata[:]...), id[:]...), []byte(plan.Target+"\x00"+plan.Stage)...))
		request, err := o.originalIntentRequest(r)
		if err != nil {
			return err
		}
		comparison, err := o.captureOriginalReserve(ctx, request, table, r.OperationID, nil)
		if err != nil {
			return err
		}
		if e = table.save(ctx, r); e != nil {
			return e
		}
		// AdmissionEpoch is NOT an atomic Reserve return. This original SH lifetime
		// proves: a preceding maintenance fence refuses Reserve; a later fence cannot
		// be cleared by census/Repair until this permit closes its SH gate.
		if e = o.fail("reserve", "before"); e != nil {
			return e
		}
		if e = o.requireMutation(ctx); e != nil {
			return e
		}
		token, reserveErr := o.ref.cell.q.ReserveCompared(ctx, request, observations, comparison)
		if reserveErr != nil {
			if ctx.Err() != nil {
				return errors.Join(reserveErr, ctx.Err())
			}
			kind := storageDefinitelyRejected(reserveErr)
			if kind == storageIntentNoRejection {
				return reserveErr
			}
			checked, e := o.ref.cell.q.InspectHostState(ctx)
			if e != nil {
				return errors.Join(reserveErr, e)
			}
			if checked.Epoch != r.AdmissionEpoch {
				return errors.Join(reserveErr, errStorageOwnerRecovery)
			}
			for _, s := range checked.Slots {
				if storageIntentMatchingTicket(r, s.Ticket, o.ref.cell.binding) {
					return errors.Join(reserveErr, errStorageOwnerRecovery)
				}
			}
			rejected := storageNextIntent(table.slots[slot], storageIntentRejected)
			rejected.Outcome = storageIntentNotAdmitted
			rejected.Rejection = kind
			if e = table.save(ctx, rejected); e != nil {
				return errors.Join(reserveErr, e)
			}
			return reserveErr
		}
		if e = o.fail("reserve", "after"); e != nil {
			return e
		}
		if e = o.recordSingleFileReserved(admissionParent, ctx, table, r, token); e != nil {
			return e
		}
		if e = o.fail("advance", "before"); e != nil {
			return e
		}
		if e = o.requireMutation(ctx); e != nil {
			return e
		}
		advanced, e := o.ref.cell.q.Advance(ctx, token, o.ref.cell.binding, storagebudget.Staging, r.CASBinding)
		if e != nil {
			return e
		}
		if e = o.fail("advance", "after"); e != nil {
			return e
		}
		staged, e := o.recordSingleFileStaging(admissionParent, ctx, table, r, token, advanced)
		if e != nil {
			return e
		}
		if e = o.requireMutation(ctx); e != nil {
			return e
		}
		permit.slot = uint16(slot)
		permit.generation = staged.Generation
		permit.operationID = id
		permit.token = advanced
		return nil
	})
	if e != nil {
		return nil, e
	}
	return permit, nil
}

// A positive Q return may arrive after the internal coordination deadline.
// Only recording that completed result gets a separate 250 ms metadata budget,
// under the same retained control/home/domain/use lifetime. Parent cancellation
// is never removed. The original budget still forbids any next Q mutation and
// no permit is issued after it expires. Total control residency can therefore
// include this extra confirmation budget; this is not an end-to-end 250 ms cap.
func storageSingleFileResultContext(parent context.Context) (context.Context, context.CancelFunc, error) {
	if err := parent.Err(); err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(parent, 250*time.Millisecond)
	return ctx, cancel, nil
}

// Called only after Reserve returned a positive token with nil error. This
// method cannot Reserve/Advance; it reads the exact returned tuple and records
// only the already-planned Reserved page. An unknown/error return never enters.
func (o *storageBudgetOwner) recordSingleFileReserved(parent, coordination context.Context, table *storageIntentTable, original storageIntentRecord, token storagebudget.Token) error {
	ctx, cancel, err := storageSingleFileResultContext(parent)
	if err != nil {
		return err
	}
	defer cancel()
	old := table.slots[original.Slot].record
	if old.Phase != storageIntentPrepared || old.Generation != original.Generation || old.OperationID != original.OperationID {
		return errStorageOwnerRecovery
	}
	checked, err := o.ref.cell.q.InspectHostState(ctx)
	if err != nil {
		return err
	}
	ticket := checked.Slots[token.Slot].Ticket
	if ticket.Token != token || ticket.State != storagebudget.Reserved || !storageIntentMatchingTicket(original, ticket, o.ref.cell.binding) {
		return errStorageOwnerRecovery
	}
	reserved := storageNextIntent(table.slots[original.Slot], storageIntentReserved)
	if reserved.Fanout != nil {
		f := *reserved.Fanout
		f.Initial = token
		reserved.Fanout = &f
	}
	reserved.Token, reserved.PolicyRevision = token, ticket.PolicyRevision
	if err = table.save(ctx, reserved); err != nil {
		return err
	}
	if err = parent.Err(); err != nil {
		return err
	}
	return coordination.Err()
}

// Called only after the original Advance positively returned. Persist that
// exact result without another Q mutation, then honor both caller/budget expiry.
func (o *storageBudgetOwner) recordSingleFileStaging(parent, coordination context.Context, table *storageIntentTable, original storageIntentRecord, reserved, advanced storagebudget.Token) (storageIntentRecord, error) {
	ctx, cancel, err := storageSingleFileResultContext(parent)
	if err != nil {
		return storageIntentRecord{}, err
	}
	defer cancel()
	old := table.slots[original.Slot].record
	if old.Phase != storageIntentReserved || old.OperationID != original.OperationID || old.Token != reserved || !storageIntentNextToken(reserved, advanced) {
		return storageIntentRecord{}, errStorageOwnerRecovery
	}
	staged := storageNextIntent(table.slots[original.Slot], storageIntentStaging)
	staged.PreviousToken, staged.Token, staged.Evidence = reserved, advanced, original.CASBinding
	if err = table.save(ctx, staged); err != nil {
		return storageIntentRecord{}, err
	}
	if err = parent.Err(); err != nil {
		return staged, err
	}
	return staged, coordination.Err()
}

func stringsSplitStoragePath(s string) []string {
	var out []string
	for s != "." && s != "" {
		d, b := path.Split(s)
		out = append([]string{b}, out...)
		s = path.Clean(d)
	}
	return out
}

// Write is single-attempt and has no replay path. Uncertainty retains the latest
// original journal truth; nonterminal effects keep their original promise.
// Close releases descriptor custody only; it never cancels/refunds a ticket.
func (a *storageSingleFileAdmission) Write(ctx context.Context) error { return a.write(ctx, false) }
func (a *storageSingleFileAdmission) write(ctx context.Context, deferred bool) (err error) {
	if a == nil || a.self != a {
		return storagebudget.ErrAuthority
	}
	ctx = storageMutationContext(ctx, a.authority)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.attempted {
		return storagebudget.ErrStale
	}
	if e := a.current(ctx); e != nil {
		return e
	}
	a.attempted = true
	o := a.owner
	var admitted storageIntentRecord
	if e := o.withControl(ctx, func(ctx context.Context, t *storageIntentTable) error {
		r, e := a.originalRecord(t)
		if e != nil {
			return e
		}
		status, e := o.ref.cell.q.InspectHostState(ctx)
		if e != nil {
			return e
		}
		if e = o.validateIntentMappings(ctx, t, status); e != nil {
			return e
		}
		ticket := status.Slots[r.Token.Slot].Ticket
		if ticket.Token != r.Token || ticket.State != storagebudget.Staging {
			return storagebudget.ErrStale
		}
		admitted = r
		return nil
	}); e != nil {
		return e
	}
	_, e := a.admittedBaseline(ctx, admitted)
	if e != nil {
		return e
	}
	if e = o.fail("stage", "before-create"); e != nil {
		return e
	}
	f, e := a.parent.OpenFile(a.stage, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	closed := false
	defer func() {
		if !closed {
			err = errors.Join(err, f.Close())
		}
	}()
	if e = o.fail("stage", "after-create"); e != nil {
		return e
	}
	data := a.plan.Bytes
	if o.fault != nil && o.fault.Name == "stage" && o.fault.Phase == "short-write" {
		data = data[:len(data)/2]
		o.fault = nil
	}
	n, e := f.Write(data)
	if e != nil || n != len(a.plan.Bytes) {
		return errors.Join(storagebudget.ErrUnknown, e, io.ErrShortWrite)
	}
	if e = o.fail("stage", "after-write"); e != nil {
		return e
	}
	if e = f.Chmod(0600); e != nil {
		return e
	}
	if e = o.fail("stage", "after-chmod"); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = o.fail("stage", "after-sync"); e != nil {
		return e
	}
	stagedInfo, e := f.Stat()
	if e != nil || !storagePrivate(stagedInfo, false) || stagedInfo.Size() != int64(len(a.plan.Bytes)) {
		return storagebudget.ErrUnknown
	}
	stageID, e := storageFileIdentity(stagedInfo)
	if e != nil {
		return e
	}
	ce := f.Close()
	closed = true
	if e = errors.Join(ce, o.fail("stage", "after-close")); e != nil {
		return e
	}
	if e = o.withControl(ctx, func(ctx context.Context, t *storageIntentTable) error {
		_, e := a.originalRecord(t)
		if e != nil {
			return e
		}
		next := storageNextIntent(t.slots[a.slot], storageIntentPublishing)
		next.StageExists = true
		next.StageIdentity = stageID
		if e = t.save(ctx, next); e != nil {
			return e
		}
		a.generation = next.Generation
		return nil
	}); e != nil {
		return e
	}
	if e = a.current(ctx); e != nil {
		return e
	}
	_, e = a.admittedBaseline(ctx, admitted)
	if e != nil {
		return e
	}
	stagedBytes, currentStage, e := storageOriginalFile(a.parent, a.stage, uint64(len(a.plan.Bytes)))
	if e != nil || !os.SameFile(stagedInfo, currentStage) || !bytes.Equal(stagedBytes, a.plan.Bytes) {
		return errors.Join(storagebudget.ErrUnknown, e)
	}
	if e = o.fail("publish", "before-rename"); e != nil {
		return e
	}
	if e = a.parent.Rename(a.stage, a.target); e != nil {
		return e
	}
	if e = o.fail("publish", "after-rename"); e != nil {
		return e
	}
	dir, e := a.parent.Open(".")
	if e != nil {
		return e
	}
	se := o.fail("publish", "before-directory-sync")
	if se == nil {
		se = dir.Sync()
	}
	se = errors.Join(se, o.fail("publish", "after-directory-sync"))
	ce = dir.Close()
	if e = errors.Join(se, ce, o.fail("publish", "after-directory-close"), o.fail("publish", "after-directory")); e != nil {
		return e
	}
	if e = a.current(ctx); e != nil {
		return e
	}
	actual, info, e := storageOriginalFile(a.parent, a.target, uint64(len(a.plan.Bytes)))
	if e != nil || !os.SameFile(stagedInfo, info) || !bytes.Equal(actual, a.plan.Bytes) {
		return errors.Join(storagebudget.ErrUnknown, e)
	}
	if _, e = a.parent.Lstat(a.stage); !errors.Is(e, os.ErrNotExist) {
		return errors.Join(storagebudget.ErrUnknown, e)
	}
	if e = o.fail("publish", "after-readback"); e != nil {
		return e
	}
	if e = a.closeRemovedOriginal(admitted); e != nil {
		return e
	}
	// This in-memory fact is a sealed continuation of this exact admission,
	// not another token or durable state. Publishing remains the crash obligation.
	fact := &storageSingleFilePublication{admission: a, token: a.token, generation: a.generation, stage: stageID}
	fact.self = fact
	a.publication = fact
	if deferred {
		return nil
	}
	return a.completePublicationLocked(ctx, fact)
}

type storageSingleFilePublication struct {
	self       *storageSingleFilePublication
	admission  *storageSingleFileAdmission
	token      storagebudget.Token
	generation uint64
	stage      storageDirectoryIdentity
	consumed   bool
}

func (a *storageSingleFileAdmission) completePublication(ctx context.Context, f *storageSingleFilePublication) error {
	if a == nil || a.self != a {
		return storagebudget.ErrAuthority
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.completePublicationLocked(ctx, f)
}
func (a *storageSingleFileAdmission) completePublicationLocked(ctx context.Context, f *storageSingleFilePublication) error {
	ctx = storageMutationContext(ctx, a.authority)
	if f == nil || f.self != f || f.admission != a || a.publication != f || f.consumed || a.closed || f.token != a.token || f.generation != a.generation {
		return storagebudget.ErrAuthority
	}
	if e := a.current(ctx); e != nil {
		return e
	}
	if a.recoveryPrepared != nil {
		// Observation-only seam, outside Q and before the first completion save.
		// The recovery caller retains its operation lifetime barrier throughout.
		recoveryStorageStep("completion-current")
	}
	f.consumed = true
	o := a.owner
	stageID := f.stage
	var e error
	// All deltas are observed by this original held owner, outside Q. The stage
	// namespace entry disappeared in rename; only the final target remains.
	return o.withControl(ctx, func(ctx context.Context, t *storageIntentTable) error {
		r := t.slots[a.slot].record
		if r.Phase != storageIntentPublishing || r.Generation != a.generation || r.OperationID != a.operationID || r.Token != a.token {
			return storagebudget.ErrStale
		}
		settling := storageNextIntent(t.slots[a.slot], storageIntentSettling)
		settling.Outcome = storageIntentPublished
		added := storagebudget.Vector{Bytes: r.NewLength, Files: 1}
		removed := storagebudget.Vector{}
		if r.OldExists {
			removed = storagebudget.Vector{Bytes: r.OldLength, Files: 1}
		}
		for i, c := range r.Components {
			ch := storagebudget.Change{}
			if c.Physical != (storagebudget.Vector{}) {
				ch.PhysicalAdd = added
				ch.PhysicalRemove = removed
			}
			if c.Workspace != (storagebudget.ID{}) || r.Fanout != nil && c.Attribution != (storagebudget.Vector{}) {
				ch.AttributionAdd = added
				ch.AttributionRemove = removed
			}
			settling.Changes[i] = ch
		}
		// Evidence hashes only fixed original observation metadata, never bytes.
		evidence := storageMetadataDigest("desk-storage-single-file-published-v1", r.OldIdentity, stageID)
		settling.Evidence = sha256.Sum256(append(append(evidence[:], r.OperationID[:]...), r.CASBinding[:]...))
		if e = t.save(ctx, settling); e != nil {
			return e
		}
		if e = o.fail("settle", "before"); e != nil {
			return e
		}
		if e = o.requireMutation(ctx); e != nil {
			return e
		}
		terminal, e := o.ref.cell.q.Settle(ctx, storagebudget.SettlementFact{Token: r.Token, Owner: o.ref.cell.binding, Evidence: settling.Evidence, Changes: settling.Changes})
		if e != nil {
			return e
		}
		if e = o.fail("settle", "after"); e != nil {
			return e
		}
		settled := storageNextIntent(t.slots[a.slot], storageIntentSettled)
		settled.PreviousToken = r.Token
		settled.Token = terminal
		if e = t.save(ctx, settled); e != nil {
			return e
		}
		if e = o.fail("checkpoint", "before"); e != nil {
			return e
		}
		if e = o.requireMutation(ctx); e != nil {
			return e
		}
		if e = o.ref.cell.q.Checkpoint(ctx); e != nil {
			return e
		}
		if e = o.fail("checkpoint", "after"); e != nil {
			return e
		}
		status, e := o.ref.cell.q.InspectHostState(ctx)
		if e != nil {
			return e
		}
		row := status.Slots[terminal.Slot]
		if row.PersistedApplied != terminal || row.PersistedNextSequence <= terminal.Sequence {
			return errStorageOwnerRecovery
		}
		retired := storageNextIntent(t.slots[a.slot], storageIntentRetired)
		retired.RetirementEpoch = status.Epoch
		if e = t.save(ctx, retired); e != nil {
			return e
		}
		a.generation = retired.Generation
		a.token = terminal
		return nil
	})
}
