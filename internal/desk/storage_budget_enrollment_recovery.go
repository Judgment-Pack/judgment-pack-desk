package desk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Inspect before legacy selection or host interpretation. A recognized peak
// always routes here; an old v3 peer is never a fallback from a peak frame.
func (c *storageBudgetConstruction) selectStartupEnrollment(ctx context.Context, record storageOwnerRecordV2, claim storageOwnerClaimV2) (*storageEnrollmentPeakEnvelope, [2][]byte, error) {
	var raws [2][]byte
	err := c.ownerGate(ctx, func(ctx context.Context) (err error) {
		for bank, name := range []string{"storage-owner.a", "storage-owner.b"} {
			raw, n, e := storageOwnerReadFixed(c.original.config, name, storageOwnerCatalogBytes)
			if e != nil {
				return e
			}
			raws[bank] = raw
			if e = errors.Join(n.current(), n.file.Close()); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		return nil, raws, err
	}
	if storagePolicyActualPair(raws) {
		c.startupPhysical = [2][]byte{append([]byte{}, raws[0]...), append([]byte{}, raws[1]...)}
		return nil, raws, nil
	}
	cores := raws
	composed := false
	for _, raw := range raws {
		composed = composed || string(raw[:8]) == "DSKONL1\x00"
	}
	if composed {
		c.startupComposed = true
		for i, raw := range raws {
			c.startupPhysical[i] = append([]byte{}, raw...)
		}
		pair, e := storageCompositionReadPair(raws[0], raws[1], record, claim)
		if e == nil && pair.Banks[pair.Selected].Transition != storageCompositionBase && (!pair.RecoveryOnly || storageIdentityPreparedLineage(pair) || pair.Banks[pair.Selected].Transition == storageCompositionEnrollment) {
			cores = [2][]byte{pair.Banks[0].Core, pair.Banks[1].Core}
			lineage := pair.Banks[pair.Selected].Lineage
			c.startupLineage = &lineage
		} else {
			// An exact founding cut is provisionally decoded from its retained base.
			// The complete original Q commitment is checked after OpenHeld, before
			// discovery, attachment or an enrollment/native permit can be issued.
			var base *storageCompositionBank
			for _, raw := range raws {
				if string(raw[:8]) != "DSKONL1\x00" {
					continue
				}
				bank, e0 := storageCompositionDecode(raw, record, claim)
				if e0 != nil || bank.Transition != storageCompositionBase {
					return nil, raws, errors.Join(errStorageEnrollmentUnresolved, e0)
				}
				if base != nil && base.TransitionID != bank.TransitionID {
					return nil, raws, errStorageEnrollmentUnresolved
				}
				if base == nil || bank.Sequence > base.Sequence {
					copy := bank
					base = &copy
				}
			}
			if base == nil {
				return nil, raws, errStorageEnrollmentUnresolved
			}
			var e0 error
			cores, e0 = base.firstBaseOriginal(record, claim)
			if e0 != nil {
				return nil, raws, e0
			}
			c.startupFirstBase = true
			lineage := base.Lineage
			c.startupLineage = &lineage
		}
		for i, raw := range cores {
			c.startupCores[i] = append([]byte{}, raw...)
		}
	}
	peak := false
	for _, raw := range cores {
		if string(raw[:8]) == "DSKONP1\x00" || storageEnrollmentPeakKnownOld(raw) {
			peak = true
		}
	}
	if !peak {
		return nil, raws, nil
	}
	selected, _, err := storageEnrollmentPeakDecodePairVariant(cores[0], cores[1], record, claim, composed)
	if err != nil {
		return nil, raws, errors.Join(errStorageEnrollmentUnresolved, err)
	}
	return &selected, raws, nil
}

// Startup keeps its original M3 home EX and startup retain. The enrollment
// invocation adds its own drain retain, blocks startup issuance while selected,
// and restores exactly that original startup role after reconciled cleanup.
func (c *storageBudgetConstruction) beginStartupEnrollment(ctx context.Context) (*storageEnrollmentEpoch, error) {
	if c == nil || c.self != c || c.owner == nil || c.ref == nil || c.ref.cell.startup != c || c.mutation == nil {
		return nil, storagebudget.ErrAuthority
	}
	if err := c.mutation.current(ctx, true); err != nil {
		return nil, err
	}
	o := c.owner
	o.mu.Lock()
	g := &o.admission
	g.mu.Lock()
	c.mu.Lock()
	defer o.mu.Unlock()
	defer g.mu.Unlock()
	defer c.mu.Unlock()
	a := c.mutation
	if c.closing || c.closed || !c.home.exclusive || o.closing || o.closed || o.transitionRequired || o.active != 1 || g.phase != storageAdmissionStartup || g.startup != a || a.closed || !a.retained || g.generation != a.generation || g.shared != 0 || g.prefixes != 0 || g.preparation != nil || g.exclusiveCensus != nil || g.enrollment != nil || c.startupUse != nil || c.startupWork != nil || len(c.startupHolds) != 0 {
		return nil, storagebudget.ErrBusy
	}
	callCtx, cancel := context.WithCancel(ctx)
	p := &storageEnrollmentEpoch{measurementCaller: ctx, c: c, generation: g.generation, ctx: callCtx, cancel: cancel, running: true, done: make(chan struct{}), startup: true}
	p.self = p
	o.active++
	g.phase = storageAdmissionEnrollment
	g.enrollment = p
	p.stopCancel = context.AfterFunc(callCtx, p.revoke)
	return p, nil
}

func (p *storageEnrollmentEpoch) enrollmentPassPlan() []storageEnrollmentPeakGroup {
	if p == nil || p.scope == nil {
		return nil
	}
	if p.passGroups != nil {
		return p.passGroups
	}
	return storageEnrollmentPeakPlan(*p.scope)
}

// Select a fresh bounded subset for this invocation only. It says nothing
// about whether a historical publication was spent. Prefix/repair repetitions
// retain the same full P; no new rows, counters or recovered permits are minted.
func (p *storageEnrollmentEpoch) recoveryPass(ids []uint16) error {
	if !p.startup || p.planCursor != 0 || p.passGroups != nil || p.scope == nil {
		return storagebudget.ErrAuthority
	}
	full := storageEnrollmentPeakPlan(*p.scope)
	rows := map[uint16]storageEnrollmentPeakGroup{}
	for _, g := range full {
		rows[g.ID] = g
	}
	seen := map[uint16]bool{}
	for _, id := range ids {
		g, ok := rows[id]
		if !ok || seen[id] {
			return storagebudget.ErrBounds
		}
		seen[id] = true
		p.passGroups = append(p.passGroups, g)
	}
	if len(p.passGroups) > len(full) {
		return storagebudget.ErrBounds
	}
	return nil
}

func (p *storageEnrollmentEpoch) openRecoveryTarget(ctx context.Context, target byte) error {
	if target < 1 || target > 8 || p.targets[target-1].named.file != nil || p.scope == nil {
		return storagebudget.ErrAuthority
	}
	if err := p.held(ctx); err != nil {
		return err
	}
	parent := p.targetParent(target)
	name := storageEnrollmentNativeName(target)
	info, err := parent.Lstat(name)
	if err != nil {
		return err
	}
	if info.Size() < 0 {
		return errStorageCustody
	}
	before, final := storageEnrollmentPeakU64(p.scope[1360+32*int(target-1)+4:]), storageEnrollmentPeakU64(p.scope[1360+32*int(target-1)+12:])
	if uint64(info.Size()) < before || uint64(info.Size()) > final {
		return errStorageCustody
	}
	var n storageNamedFile
	if target >= 7 {
		n, err = p.openEnrollmentCheckpoint(ctx, name)
	} else if target >= 5 {
		n, err = storageOpenOriginalControl(p.c.ref.cell, name)
	} else {
		n, err = storageOpenFile(parent, name, info.Size())
	}
	if err != nil {
		return err
	}
	t := &p.targets[target-1]
	t.named = n
	t.before = before
	t.final = final
	t.native, err = storageOwnerPersistentFile(ctx, n)
	if err != nil || t.native != p.scope.target(target) {
		return errors.Join(errStorageCustody, err)
	}
	return p.targetCurrent(ctx, target, false)
}

func (p *storageEnrollmentEpoch) completeStartupSourceHolds(ctx context.Context) error {
	if !p.startup || p.sourceHoldsComplete || len(p.holds) != 0 || len(p.originalRoots) != 0 {
		return storagebudget.ErrAuthority
	}
	p.originalRoots = p.c.owner.orderedRoots()
	info, err := p.project.Stat()
	if err != nil {
		return err
	}
	for _, r := range p.originalRoots {
		if !r.domainDirectory || r.domain.file == nil || !os.SameFile(info, r.domain.identity) {
			continue
		}
		id, e := storageFileIdentity(r.domain.identity)
		if e != nil {
			return e
		}
		if p.use.domains == nil {
			p.use.domains = map[storageDirectoryIdentity]*storageDomainHold{}
		}
		if p.use.domains[id] == nil {
			h := &storageDomainHold{owner: p.c.owner, root: r, lifetime: p.use, file: p.project}
			h.self = h
			p.use.domains[id] = h
		}
	}
	// Reuse only actual pre-binding descriptions whose inode is now also
	// the validated original domain. sourceFiles retains their sole Close.
	for _, r := range p.originalRoots {
		if r.qControl {
			continue
		}
		id, e := storageFileIdentity(r.domain.identity)
		if e != nil {
			return e
		}
		if p.use.domains == nil {
			p.use.domains = map[storageDirectoryIdentity]*storageDomainHold{}
		}
		if p.use.domains[id] != nil {
			continue
		}
		for _, n := range p.sourceFiles {
			if os.SameFile(n.identity, r.domain.identity) {
				h := &storageDomainHold{owner: p.c.owner, root: r, lifetime: p.use, file: n.file}
				h.self = h
				p.use.domains[id] = h
				break
			}
		}
	}
	for _, r := range p.originalRoots {
		h, e := p.c.owner.domain(ctx, r, p.use)
		if e != nil {
			return e
		}
		p.holds = append(p.holds, h)
	}
	p.sourceHoldsComplete = true
	return p.held(ctx)
}

// This join owns the actual original construction and installs Scope P before
// opening any of its four new fixed targets. Prior to source-domain acquisition
// only bounded reads occur; enterNative refuses until all actual source holds
// have been acquired. No decoded frame is itself a native capability.
func (c *storageBudgetConstruction) resumeOriginalEnrollmentStartup(ctx context.Context, outer storageEnrollmentPeakEnvelope, catalogs [2][]byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) (err error) {
	if c.owner != nil || c.sources == nil || !c.home.exclusive || outer.Scope.target(5) != record.CatalogA || outer.Scope.target(6) != record.CatalogB {
		return storagebudget.ErrAuthority
	}
	// No ordinary observer is installed while historical rows are read. The
	// actual after observer is separately validated under this phase below.
	owner, err := newStorageBudgetOwner(c.ref, &storageAdvisoryObserver{}, storageBudgetOptions{})
	if err != nil {
		return err
	}
	if err = c.installAdmissionOwner(ctx, owner); err != nil {
		return errors.Join(err, owner.Close())
	}
	p, err := c.beginStartupEnrollment(ctx)
	if err != nil {
		return err
	}
	defer func() { err = p.finish(err) }()
	if err = p.acquire(ctx); err != nil {
		return err
	}
	p.sourceHoldsComplete = false
	scope := outer.Scope
	actualProject, err := storageOwnerPersistentRoot(ctx, c.project.own.root)
	if err != nil || actualProject != storageEnrollmentPeakNative(scope[286:321]) {
		return errors.Join(errStorageCustody, err)
	}
	// Installation/control originals and complete immutable Scope were decoded
	// under actual home EX. Reconstruct precisely the same P, without claiming
	// another free-space admission or subtracting already allocated bytes.
	p.scope = &scope
	p.reservation = storageEnrollmentReservation{phase: p, scope: scope}
	if err = scope.requireCapacityEstimate(); err != nil {
		return err
	}
	if err = p.qualifyOriginalNative(ctx); err != nil {
		return err
	}
	if !bytes.Equal(scope[408:440], p.qualified.digest[:]) {
		return errStorageEnrollmentQualification
	}
	for target := byte(5); target <= 8; target++ {
		if err = p.openRecoveryTarget(ctx, target); err != nil {
			return err
		}
	}
	if scope[321] != 15 {
		return errStorageEnrollmentUnresolved
	} // lost initial custody; no recreate/adopt/zero refund
	for target := byte(1); target <= 4; target++ {
		if err = p.openRecoveryTarget(ctx, target); err != nil {
			return err
		}
	}
	coreCatalogs := catalogs
	if c.startupComposed || storagePolicyActualPair(c.startupPhysical) {
		coreCatalogs = c.startupCores
	}
	if scope.path() != 1 {
		// A surviving encoding3 Fence commits exact intent even when the other
		// original already contains the first R/P. Unknown/torn peers were refused
		// by strict pair selection before this original startup epoch was entered.
		for _, raw := range coreCatalogs {
			f, e := storageEnrollmentPeakDecodeEnvelopeNative(raw, record, claim, true, c.startupComposed || storagePolicyActualPair(c.startupPhysical), storagePolicyActualPair(c.startupPhysical))
			if e != nil {
				return e
			}
			if f.Kind == 0 && f.Snapshot != nil && f.Snapshot.Intent != nil {
				return p.resumeSuccessorFence(ctx, outer, catalogs, f, raw)
			}
		}
	}
	if outer.Kind != 2 || outer.Marker == nil {
		if scope.path() != 1 {
			return errStorageEnrollmentUnresolved
		} // old encoding2 lacks operands
		return p.resumeCapturedInitialEnrollment(ctx, outer)
	}
	evidenceRaw, err := p.readEnrollmentTarget(ctx, 1+outer.Marker.Bank)
	if err != nil {
		return err
	}
	e, err := p.decodeEnrollmentEvidence(evidenceRaw)
	if err != nil {
		return err
	}
	ready, err := p.readOriginalEnrollmentControl(ctx, storageReadyName, 32)
	if err != nil || !bytes.Equal(ready, record.QBinding[:]) || storageEnrollmentPeakJoinMarker(outer, e, sha256.Sum256(ready)) != nil {
		return errors.Join(errStorageCustody, err)
	}
	if err = c.sources.observeCatalogLegacyHistoryControl(ctx, e.Inner.Catalog); err != nil {
		return err
	}
	// Fully Completed C/C has a current after-definition boundary. Acquire
	// its actual original source domains before binding comparison; no old-before
	// binding or synthetic candidate supplies exclusion for a pending creation.
	if outer.Marker.Phase == 3 && e.Phase == 2 && bytes.Equal(coreCatalogs[0], coreCatalogs[1]) {
		return p.resumeCompletedCreationStartup(ctx, outer, e)
	}
	var originals [2][]byte
	var before storageOwnerCatalogV2
	if e.Scope.path() == 1 {
		originals, _, err = storageEnrollmentPeakRestoreOriginalPeer(scope, e.OriginalV3, record, claim)
		if err != nil {
			return err
		}
		old, e0 := storageOwnerDecodeCatalogV3(e.OriginalV3, record, claim)
		if e0 != nil {
			return e0
		}
		before = storageOwnerCatalogV2(old)
	} else {
		_, old, e0 := storageEnrollmentPeakImmediateBefore(e, record, claim)
		if e0 != nil {
			return e0
		}
		originals = [2][]byte{old, append([]byte{}, old...)}
		for _, projection := range e.Projections {
			if storageEnrollmentPeakProjectionFullDigest(projection) == e.Before {
				before = projection.Catalog
			}
		}
	}
	if e.Version == 3 {
		var e0 error
		originals, _, e0 = storagePolicyEvidenceNativeBefore(e, record, claim)
		if e0 != nil {
			return e0
		}
	} else if e.Version == 2 {
		physical, e0 := e.compositionBefore(record, claim)
		if e0 != nil {
			return e0
		}
		originals = [2][]byte{physical.Banks[0].Raw, physical.Banks[1].Raw}
	}
	// This fully decoded, native-marker-bound packet supplies the explicit
	// default-control reindex context before either original/candidate attach.
	// The exact encoded originals remain in e and its before projections.
	p.initialPacket = &e
	p.original = &storageEnrollmentPeakOriginalComparison{Ready: e.Ready, Project: e.Project, Catalogs: originals}
	if !c.sources.originalOwnerEqual(before) {
		return errStorageCustody
	}
	c.ref.cell.ownerCatalog = before
	if err = c.attachOwnerBindings(ctx, before, c.sources); err != nil {
		return err
	}
	if err = p.completeStartupSourceHolds(ctx); err != nil {
		return err
	}
	p.original = &storageEnrollmentPeakOriginalComparison{Ready: e.Ready, Project: e.Project, Catalogs: originals}
	p.original.Checkpoints, err = storageEnrollmentPeakRestoreOriginalCheckpoints(e.Checkpoint, e.Inner.Catalog)
	if err != nil {
		return err
	}
	q, err := c.ref.cell.q.InspectEnrollment(ctx)
	if err != nil {
		return err
	}
	p.original.Tickets = q.Tickets
	for slot := range p.original.Hosts {
		for bank := range p.original.Hosts[slot] {
			p.original.Hosts[slot][bank] = make([]byte, storageIntentPage)
		}
	}
	for _, w := range e.Witnesses {
		p.original.Hosts[w.Slot][w.Bank] = append([]byte{}, w.Page...)
	}
	if err = p.prepareEnrollmentCandidate(ctx, e.Inner.Catalog); err != nil {
		return err
	}
	if e.Scope.path() != 1 {
		peerRaw, e0 := p.readObservedEnrollmentTarget(ctx, 1+(1-e.Bank))
		if e0 != nil {
			return e0
		}
		peer, e0 := p.decodeEnrollmentEvidence(peerRaw)
		if e0 == nil && peer.Phase == 2 && sha256.Sum256(peer.Raw) == e.Board.Prior.Evidence {
			prior, _, e1 := storageEnrollmentPeakImmediateBefore(e, record, claim)
			if e1 != nil || storageEnrollmentPeakJoinMarker(prior, peer, e.Ready) != nil || !storageEnrollmentPeakRetainsOrigins(e, peer) || !storageEnrollmentPeakOriginalPeerRetained(e.Scope, peer.Scope) || !bytes.Equal(e.OriginalV3, peer.OriginalV3) {
				return errStorageCustody
			}
			p.previousEvidence = &peer
		}
		if e.Phase == 0 {
			p.anchoredStaged = &e
			p.anchoredReference = &outer
		} else if e0 == nil && peer.Phase == 0 && peer.Scope == e.Scope {
			p.anchoredStaged = &peer
			for _, raw := range coreCatalogs {
				ref, e1 := p.enrollmentEnvelope(raw, true)
				if e1 == nil && ref.Marker != nil && ref.Marker.Phase == 0 && storageEnrollmentPeakJoinMarker(ref, peer, e.Ready) == nil {
					p.anchoredReference = &ref
				}
			}
		}
	}
	if outer.Marker.Phase == 0 && e.Scope.path() == 3 {
		return p.resumeReferencedGrowth(ctx, outer, e)
	}
	return p.continueOriginalEnrollment(ctx, outer, e)
}

func storageEnrollmentRecoveryPrefix() []uint16 {
	return []uint16{70, 71, 72, 73, 74, 75, 76, 77, 78, 79}
}
func (p *storageEnrollmentEpoch) persistRecoveryPrefix(ctx context.Context) error {
	if err := p.admitScopeLaunch(ctx, *p.scope); err != nil {
		return err
	}
	for target := byte(1); target <= 10; target++ {
		if err := p.persistEnrollmentTarget(ctx, target); err != nil {
			return err
		}
	}
	return nil
}

func (p *storageEnrollmentEpoch) resumeCapturedInitialEnrollment(ctx context.Context, outer storageEnrollmentPeakEnvelope) error {
	if outer.Kind != 1 || outer.Snapshot == nil || outer.Scope.path() != 1 || outer.Scope[321] != 15 || outer.Snapshot.Kind != 0 {
		return errStorageEnrollmentUnresolved
	}
	record, claim := *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim
	originals, _, err := storageEnrollmentPeakRestoreOriginalPeer(*p.scope, outer.Snapshot.Before, record, claim)
	if err != nil {
		return err
	}
	pair, err := storageOwnerDecodeCatalogPairV3(originals[0], originals[1], record, claim)
	if err != nil {
		return err
	}
	if outer.Snapshot.Encoding == 4 {
		lineage := p.compositionLineage()
		if lineage == nil {
			return errStorageCustody
		}
		originals, err = outer.Snapshot.Physical.restore(outer.Snapshot.Before, *lineage, record, claim)
		if err != nil {
			return err
		}
	}
	p.c.ref.cell.ownerCatalog = pair.High
	if !p.c.sources.originalOwnerEqual(pair.High) {
		return errStorageCustody
	}
	if err = p.c.attachOwnerBindings(ctx, pair.High, p.c.sources); err != nil {
		return err
	}
	if err = p.completeStartupSourceHolds(ctx); err != nil {
		return err
	}
	view, err := p.c.ref.cell.q.InspectEnrollment(ctx)
	if err != nil || view.View.Configuration.Maintenance {
		return errors.Join(errStorageEnrollmentUnresolved, err)
	}
	ready, err := p.readOriginalEnrollmentControl(ctx, storageReadyName, 32)
	if err != nil || !bytes.Equal(ready, record.QBinding[:]) {
		return errors.Join(errStorageCustody, err)
	}
	p.initialInspection = &view
	p.original = &storageEnrollmentPeakOriginalComparison{Ready: sha256.Sum256(ready), Project: storageEnrollmentPeakNative(p.scope[286:321]), Catalogs: originals, Checkpoints: view.Checkpoints, Tickets: view.Tickets}
	hosts, err := p.readOriginalEnrollmentControl(ctx, storageIntentName, storageIntentBytes)
	if err != nil {
		return err
	}
	for slot := range p.original.Hosts {
		for bank := range p.original.Hosts[slot] {
			start := (slot*2 + bank) * storageIntentPage
			p.original.Hosts[slot][bank] = append([]byte{}, hosts[start:start+storageIntentPage]...)
		}
	}
	for i, t := range p.targets {
		p.original.Targets[i] = t.native
		p.original.Extents[i] = uint64(t.named.size)
	}
	catalog, source, err := p.planOriginalProjectScope(ctx)
	if err != nil {
		return err
	}
	model, err := p.initialPacketModel(ctx, catalog, source)
	if err != nil {
		return err
	}
	extent, err := storageEnrollmentInitialExtent(model)
	if err != nil || extent > p.scope.final(0) || extent > p.scope.final(1) {
		return errors.Join(storagebudget.ErrBounds, err)
	}
	model.Scope = *p.scope
	model.Extent = p.scope.final(0)
	model.Board.Groups = storageEnrollmentPeakPlan(*p.scope)
	for i := range model.Board.Groups {
		g := &model.Board.Groups[i]
		g.Generation = storageEnrollmentPeakU64(p.scope[1616:1624])
		if g.ID >= 16 && g.ID <= 20 || g.ID >= 51 && g.ID <= 55 {
			g.Expected = model.Digests.Origin
		}
		switch g.ID {
		case 21, 56:
			g.Expected = model.Digests.Q[0]
		case 22, 57:
			g.Expected = model.Digests.Q[1]
		}
	}
	p.initialPacket = &model
	prefix := storageEnrollmentPeakPrefix{Projection: model.Sections[4].Raw, Tickets: model.Sections[2].Raw, Views: model.Sections[3].Raw, Checkpoint: model.Sections[6].Raw, TicketCount: uint32(len(model.Tickets)), ViewCount: uint32(len(model.Views))}
	prefix.Catalog, err = json.Marshal(catalog)
	if err != nil {
		return err
	}
	if err = storageEnrollmentPeakComparePrefix(storageEnrollmentPeakComparisonInput{Original: *p.original, CurrentCheckpoints: view.Checkpoints, CurrentTickets: view.Tickets, Prefix: prefix}, outer, record, claim); err != nil {
		return err
	}
	if err = p.prepareEnrollmentCandidate(ctx, catalog); err != nil {
		return err
	}
	ids := storageEnrollmentRecoveryPrefix()
	include := false
	for _, g := range storageEnrollmentPeakPlan(*p.scope) {
		if g.ID == 1 {
			include = true
		}
		if include {
			ids = append(ids, g.ID)
		}
	}
	if err = p.recoveryPass(ids); err != nil {
		return err
	}
	if err = p.persistRecoveryPrefix(ctx); err != nil {
		return err
	}
	p.fenced = true
	staged, err := p.initialStagedPacket(ctx)
	if err != nil {
		return err
	}
	return p.runInitialEnrollmentControls(ctx, staged)
}

func (p *storageEnrollmentEpoch) continueOriginalEnrollment(ctx context.Context, outer storageEnrollmentPeakEnvelope, e storageEnrollmentPeakEvidence) error {
	if outer.Marker == nil || e.Phase == 0 {
		return errStorageEnrollmentUnresolved
	}
	if e.Phase == 2 {
		armedRaw, err := p.readEnrollmentTarget(ctx, 1+(1-e.Bank))
		if err != nil {
			return err
		}
		armed, err := p.decodeEnrollmentEvidence(armedRaw)
		if err != nil || !storageEnrollmentPeakJoinArmed(e, armed) {
			return errors.Join(errStorageEnrollmentUnresolved, err)
		}
		p.evidence = &armed
		p.completedEvidence = &e
	} else {
		p.evidence = &e
	}
	catalogs := [2][]byte{}
	for bank := range catalogs {
		var err error
		catalogs[bank], err = p.readEnrollmentTarget(ctx, byte(5+bank))
		if err != nil {
			return err
		}
	}
	actual, err := p.compareEnrollmentActual(ctx)
	var repairArm []byte
	if err != nil {
		// Only this qualified deterministic-next-peer case may repair a failed
		// strict dispatch pair. Revalidate every non-dispatch comparison first.
		if e.Phase != 1 || outer.Marker.Phase < 1 || outer.Marker.Phase > 2 {
			return errors.Join(errStorageEnrollmentUnresolved, err)
		}
		original, e0 := p.currentEnrollmentOriginal(ctx)
		if e0 != nil || storageEnrollmentPeakCompareOriginal(e, original, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim) != nil {
			return errors.Join(errStorageEnrollmentUnresolved, e0)
		}
		var steps [2][]byte
		for i := range steps {
			steps[i], e0 = p.readEnrollmentTarget(ctx, byte(3+i))
			if e0 != nil {
				return e0
			}
		}
		q, e0 := p.c.ref.cell.q.InspectEnrollment(ctx)
		if e0 != nil {
			return e0
		}
		selected, _, arm, e0 := storageEnrollmentRecoverNextArm(e, steps, q.Checkpoints, q.Tickets)
		if e0 != nil {
			return errors.Join(errStorageEnrollmentUnresolved, e0)
		}
		stage := int(selected.Step)
		if outer.Marker.Phase == 2 && stage != 2 || outer.Marker.Phase == 1 && stage > 0 && !bytes.Equal(catalogs[0], catalogs[1]) {
			return errStorageEnrollmentUnresolved
		}
		_, bank, e0 := p.enrollmentCatalogPair(catalogs)
		if e0 != nil {
			return e0
		}
		actual = storageEnrollmentPeakComparison{Envelope: outer, Evidence: e, CatalogBank: bank, Dispatch: selected, Q: storageEnrollmentPeakQComparison{Stage: stage}}
		repairArm = arm
	}
	if actual.Q.UnresolvedQAttempt {
		return errStorageEnrollmentUnresolved
	}
	stage := actual.Q.Stage
	if err = p.comparePublishedCreation(ctx, e); err != nil {
		return err
	}
	ids := storageEnrollmentRecoveryPrefix()
	repairP := outer.Marker.Phase == 1 && !bytes.Equal(catalogs[0], catalogs[1])
	if repairP {
		ids = append(ids, 12, 47)
	}
	for k := stage; k < 3; k++ {
		if k == 2 && outer.Marker.Phase < 2 {
			ids = append(ids, 13, 48)
		}
		ids = append(ids, uint16(18+k), uint16(53+k), uint16(21+k), uint16(56+k))
	}
	if stage == 3 && outer.Marker.Phase == 1 {
		ids = append(ids, 13, 48)
	}
	if p.completedEvidence == nil {
		ids = append(ids, p.enrollmentPublicationGroups(7)...)
	}
	if outer.Marker.Phase < 3 {
		ids = append(ids, 14, 49, 15, 50)
	} else if !bytes.Equal(catalogs[0], catalogs[1]) {
		ids = append(ids, 15, 50)
	}
	if err = p.recoveryPass(ids); err != nil {
		return err
	}
	if err = p.persistRecoveryPrefix(ctx); err != nil {
		return err
	}
	measured, err := p.measureEnrollmentCandidate(ctx)
	if err != nil {
		return err
	}
	if !storageEnrollmentCensusEqual(measured, storageEnrollmentFactualCensus(e.Census)) {
		return errStorageCustody
	}
	if repairP {
		if err = p.publishEnrollmentControl(ctx, 12, catalogs[actual.CatalogBank]); err != nil {
			return err
		}
	}
	for k := stage; k < 3; k++ {
		if k == 2 && outer.Marker.Phase < 2 {
			prior, err := p.selectedEnrollmentCatalog(ctx)
			if err != nil {
				return err
			}
			var digest storagebudget.Digest
			copy(digest[:], prior[32:64])
			marker := storageEnrollmentMarkerFor(*p.evidence, 2, digest)
			d := storageEnrollmentPeakEnvelope{Kind: 2, BeforeCodec: storageEnrollmentPeakBeforeCodec(*p.scope), Sequence: storageEnrollmentPeakMarkerSequence(*p.scope, 2), Scope: *p.scope, Marker: &marker}
			raw, err := p.encodeEnrollmentEnvelope(d)
			if err != nil {
				return err
			}
			if err = p.publishEnrollmentControl(ctx, 13, raw); err != nil {
				return err
			}
		}
		if k == stage && repairArm != nil {
			if err = p.publishEnrollmentControl(ctx, uint16(18+k), repairArm); err != nil {
				return err
			}
			if err = p.invokeFreshEnrollmentQ(ctx, k, repairArm); err != nil {
				return err
			}
		} else if err = p.dispatchEnrollmentQ(ctx); err != nil {
			return err
		}
	}
	if stage == 3 && outer.Marker.Phase == 1 {
		raw, e0 := p.enrollmentMarkerBytes(ctx, 2)
		if e0 != nil {
			return e0
		}
		if e0 = p.publishEnrollmentControl(ctx, 13, raw); e0 != nil {
			return e0
		}
	}
	if p.completedEvidence == nil {
		completed := *p.evidence
		completed.Phase = 2
		completed.Bank = storageEnrollmentPeakEvidenceBank(*p.scope, 2)
		completed.Generation = p.evidence.Generation + 1
		completed.Extent = p.scope.final(completed.Bank)
		completed.Previous = sha256.Sum256(p.evidence.Raw)
		raw, err := storageEnrollmentPeakEncodeEvidenceOperands(completed, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim, &p.operands)
		if err != nil {
			return err
		}
		if err = p.publishEnrollmentControl(ctx, 7, raw); err != nil {
			return err
		}
	}
	finalRaw := catalogs[actual.CatalogBank]
	if outer.Marker.Phase < 3 {
		prior, err := p.readEnrollmentTarget(ctx, p.enrollmentGroupTarget(13))
		if err != nil {
			return err
		}
		var digest storagebudget.Digest
		copy(digest[:], prior[32:64])
		marker := storageEnrollmentMarkerFor(*p.completedEvidence, 3, digest)
		final := storageEnrollmentPeakEnvelope{Kind: 2, BeforeCodec: storageEnrollmentPeakBeforeCodec(*p.scope), Sequence: storageEnrollmentPeakMarkerSequence(*p.scope, 3), Scope: *p.scope, Marker: &marker}
		finalRaw, err = p.encodeEnrollmentEnvelope(final)
		if err != nil {
			return err
		}
		if err = p.publishEnrollmentControl(ctx, 14, finalRaw); err != nil {
			return err
		}
		if err = p.publishEnrollmentControl(ctx, 15, finalRaw); err != nil {
			return err
		}
	} else if !bytes.Equal(catalogs[0], catalogs[1]) {
		if err = p.publishEnrollmentControl(ctx, 15, finalRaw); err != nil {
			return err
		}
	}
	return p.reconcileEnrollment(ctx, finalRaw)
}

func (p *storageEnrollmentEpoch) reattachCompletedEnrollment(ctx context.Context, outer storageEnrollmentPeakEnvelope, e storageEnrollmentPeakEvidence, current storagebudget.EnrollmentInspection, pending *storageCreationIntent) error {
	if !p.startup || outer.Marker == nil || outer.Marker.Phase != 3 || e.Phase != 2 || current.View.Configuration.Maintenance || current.View.Configuration.Epoch != e.AfterEpoch || current.View.Configuration.Revision < e.AfterRevision {
		return errStorageEnrollmentUnresolved
	}
	armedRaw, err := p.readEnrollmentTarget(ctx, 1+(1-e.Bank))
	if err != nil {
		return err
	}
	armed, err := p.decodeEnrollmentEvidence(armedRaw)
	if err != nil || !storageEnrollmentPeakJoinArmed(e, armed) {
		return errors.Join(errStorageCustody, err)
	}
	_, after, err := storageOwnerActualDefinitions(current.View, e.Inner.Catalog)
	if err != nil || !after {
		return errors.Join(errStorageCustody, err)
	}
	anchor := storageEnrollmentPeakQHeldAnchor{Installation: storageEnrollmentPeakQID(e.Installation), Binding: storageEnrollmentPeakQDigest(e.QBinding)}
	q, err := storageEnrollmentPeakQDecodeCheckpoint(append([]byte{}, current.Checkpoints[current.CheckpointBank]...), anchor)
	if err != nil {
		return err
	}
	for _, r := range q.Roots {
		if r.ID != (storageEnrollmentPeakQID{}) && !r.Known {
			return storagebudget.ErrUnknown
		}
	}
	for _, w := range q.Workspaces {
		if w.ID != (storageEnrollmentPeakQID{}) && !w.Known {
			return storagebudget.ErrUnknown
		}
	}
	// Both exact C select a fully decoded Completed under fixed original native
	// custody. Later ordinary Q state is checked in full against the unchanged
	// definitions/epoch, strict persisted retirement and actual current all64 host
	// mappings; historical ticket bytes are never substituted with new bytes.
	table := storageIntentTable{owner: p.c.owner}
	raw, err := p.readOriginalEnrollmentControl(ctx, storageIntentName, storageIntentBytes)
	if err != nil {
		return err
	}
	for slot := range table.slots {
		start := slot * 2 * storageIntentPage
		selection, err := storageSelectCreationHost(raw[start:start+storageIntentPage], raw[start+storageIntentPage:start+2*storageIntentPage], slot, e.Installation, e.QBinding)
		if err != nil {
			return err
		}
		table.slots[slot] = selection.Slot
		table.identity[slot] = selection.Identity
		table.creation[slot] = selection.Creation
		if selection.Creation != nil && selection.Creation.Kind != storageCreationLocalPrivate && selection.Creation.Core.Phase == storageIntentRetired && !storageCreationTailIncluded(*selection.Creation, e) && !storageCreationRecordEqual(selection.Creation, pending) {
			return errStorageEnrollmentUnresolved
		}
		if selection.PredecessorCreation != nil && selection.PredecessorCreation.Kind != storageCreationLocalPrivate && selection.PredecessorCreation.Core.Phase == storageIntentRetired && !storageCreationTailIncluded(*selection.PredecessorCreation, e) {
			return errStorageEnrollmentUnresolved
		}
		if !table.slots[slot].record.reusable() && !(p.c.startupComposed && (selection.Identity != nil || selection.Creation != nil && selection.Creation.Kind == storageCreationLocalPrivate)) {
			return errStorageOwnerRecovery
		}
	}
	if err = p.c.owner.validateIntentMappings(ctx, &table, storageHostState(current.View)); err != nil {
		return err
	}
	if err = p.c.sources.current(ctx); err != nil {
		return err
	}
	if pending != nil {
		page, e0 := storageEncodeCreationIntent(*pending, pending.Tail.Installation, pending.Tail.QBinding)
		if e0 != nil {
			return e0
		}
		if _, e0 = p.compareRetiredCreation(ctx, *pending, page, e.Inner.Catalog, current); e0 != nil {
			return e0
		}
	} else if err = p.comparePublishedCreation(ctx, e); err != nil {
		return err
	}
	// Completed startup entered before the historical-before reconstruction.
	// Capture its actual current originals after the full C/Q/host/source join;
	// retained witnesses and the old Q0 are not this invocation's originals.
	original := &storageEnrollmentPeakOriginalComparison{Ready: e.Ready, Project: e.Project, Checkpoints: current.Checkpoints, Tickets: current.Tickets}
	completedCore, err := p.encodeEnrollmentEnvelope(outer)
	if err != nil {
		return err
	}
	for target := byte(1); target <= 8; target++ {
		if err = p.targetCurrent(ctx, target, true); err != nil {
			return err
		}
		t := p.targets[target-1]
		original.Targets[target-1], original.Extents[target-1] = t.native, uint64(t.named.size)
		if target >= 5 {
			observed, e0 := p.readEnrollmentTarget(ctx, target)
			if e0 != nil {
				return e0
			}
			if target <= 6 {
				original.Catalogs[target-5] = observed
				core, e0 := p.enrollmentCore(observed)
				if e0 != nil || !bytes.Equal(core, completedCore) {
					return errors.Join(errStorageCustody, e0)
				}
			} else if !bytes.Equal(observed, current.Checkpoints[target-7]) {
				return errStorageCustody
			}
		}
	}
	selected, _, err := p.enrollmentCatalogPair(original.Catalogs)
	if err != nil || !reflect.DeepEqual(selected, outer) {
		return errors.Join(errStorageCustody, err)
	}
	for slot := range original.Hosts {
		for bank := range original.Hosts[slot] {
			start := (slot*2 + bank) * storageIntentPage
			original.Hosts[slot][bank] = append([]byte{}, raw[start:start+storageIntentPage]...)
		}
	}
	p.original = original
	if _, err = p.currentEnrollmentOriginal(ctx); err != nil {
		return err
	}
	ids := storageEnrollmentRecoveryPrefix()
	if err = p.recoveryPass(ids); err != nil {
		return err
	}
	if err = p.persistRecoveryPrefix(ctx); err != nil {
		return err
	}
	p.completedEvidence = &e
	if err = p.candidate.install(p.c.owner); err != nil {
		return err
	}
	p.c.ref.cell.mu.Lock()
	p.c.ref.cell.enrollmentEndpoint = &e
	p.c.ref.cell.ownerCatalog = e.Inner.Catalog
	p.c.ref.cell.mu.Unlock()

	p.c.owner.mu.Lock()
	p.c.owner.observer = p.candidateObserver
	p.c.owner.censusEpoch = current.View.Configuration.Epoch
	p.c.owner.mu.Unlock()
	p.completeScopeLaunch(e)
	p.mu.Lock()
	p.reconciled = true
	p.mu.Unlock()
	return p.nativePermit(ctx)
}

// A qualified successful consumed-arm barrier means an arm cannot disappear
// after its Q call begins. Exactly one invalid deterministic next-arm peer with
// an exact valid current prefix and complete actual Qk therefore permits one
// fresh arm publication. Neither a selected torn arm nor Q beyond that prefix
// is repaired. This pure classifier never performs or authorizes a Q operation.
func storageEnrollmentRecoverNextArm(e storageEnrollmentPeakEvidence, raws [2][]byte, checkpoints, tickets [2][]byte) (storageEnrollmentPeakDispatch, int, []byte, error) {
	var d [2]storageEnrollmentPeakDispatch
	var valid [2]bool
	for i := range d {
		v, err := storageEnrollmentPeakDecodeDispatch(raws[i], e)
		if err == nil {
			d[i] = v
			valid[i] = true
		}
	}
	if valid[0] == valid[1] {
		return storageEnrollmentPeakDispatch{}, 0, nil, storagebudget.ErrBounds
	}
	bank := 0
	if valid[1] {
		bank = 1
	}
	selected := d[bank]
	if e.Scope.path() != 1 || e.Phase != 1 || selected.Step >= 3 || selected.Generation != uint64(selected.Step)+1 {
		return storageEnrollmentPeakDispatch{}, 0, nil, storagebudget.ErrBounds
	}
	stage := int(selected.Step)
	q, err := storageEnrollmentPeakCompareQ(e, checkpoints, tickets, selected)
	if err != nil || q.UnresolvedQAttempt || q.Stage != stage {
		return storageEnrollmentPeakDispatch{}, 0, nil, storagebudget.ErrBounds
	}
	var target byte
	for _, g := range storageEnrollmentPeakPlan(e.Scope) {
		if g.ID == uint16(18+stage) {
			target = g.Target
		}
	}
	if target < 3 || target > 4 || int(target-3) != 1-bank {
		return storageEnrollmentPeakDispatch{}, 0, nil, storagebudget.ErrBounds
	}
	next := storageEnrollmentPeakDispatch{Generation: selected.Generation + 1, Installation: e.Installation, Binding: e.QBinding, Bridge: e.Bridge, Previous: sha256.Sum256(selected.Raw), Step: byte(stage + 1), Mask: [4]byte{0, 1, 3, 7}[stage+1], BeforeRevision: e.BeforeRevision + uint64(stage), BeforeEpoch: e.BeforeEpoch, Before: e.Checkpoint.Checkpoints[e.Checkpoint.Bank], After: e.Digests.Q[stage], Origin: e.Digests.Origin, Controls: [2]storageOwnerPersistentObject{e.Controls[2], e.Controls[3]}}
	if stage > 0 {
		next.BeforeEpoch++
		next.Before = e.Digests.Q[stage-1]
	}
	raw, err := storageEnrollmentPeakEncodeDispatch(next, e)
	if err != nil {
		return storageEnrollmentPeakDispatch{}, 0, nil, err
	}
	return selected, 1 - bank, raw, nil
}

// Initial enrollment retains the V3 catalog; successors retain a peak envelope.
// Marker producers carry that same before-image selector through every phase.
func storageEnrollmentPeakBeforeCodec(s storageEnrollmentPeakScope) byte {
	if s.path() == 1 {
		return 0
	}
	return 1
}

func storageEnrollmentPeakMarkerSequence(s storageEnrollmentPeakScope, phase byte) uint64 {
	offset := uint64(phase) + 2
	if s.path() == 2 {
		offset--
	}
	return storageEnrollmentPeakU64(s[1624:1632]) + offset
}
func (p *storageEnrollmentEpoch) enrollmentGroupTarget(id uint16) byte {
	if p == nil || p.scope == nil {
		return 0
	}
	for _, g := range storageEnrollmentPeakPlan(*p.scope) {
		if g.ID == id {
			return g.Target
		}
	}
	return 0
}
func (p *storageEnrollmentEpoch) selectedEnrollmentCatalog(ctx context.Context) ([]byte, error) {
	var raws [2][]byte
	for i := range raws {
		raw, err := p.readEnrollmentTarget(ctx, byte(5+i))
		if err != nil {
			return nil, err
		}
		raws[i] = raw
	}
	if storagePolicyActualPair(raws) {
		_, bank, err := p.enrollmentCatalogPair(raws)
		if err != nil {
			return nil, err
		}
		selected, err := storageOwnerV6NativeDecode(raws[bank], *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
		if err != nil {
			return nil, err
		}
		// Return the exact selected core bytes. Both actual native banks stay
		// retained by the original publication/identity/custody owner.
		return selected.Stage.Core, nil
	}
	if p.compositionLineage() != nil {
		pair, err := storageCompositionReadPair(raws[0], raws[1], *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
		if err != nil || !reflect.DeepEqual(pair.Banks[pair.Selected].Lineage, *p.compositionLineage()) {
			return nil, storagebudget.ErrBounds
		}
		return pair.Banks[pair.Selected].Core, nil
	}
	_, bank, err := p.enrollmentCatalogPair(raws)
	if err != nil {
		return nil, err
	}
	return raws[bank], nil
}
func (p *storageEnrollmentEpoch) enrollmentMarkerBytes(ctx context.Context, phase byte) ([]byte, error) {
	e := p.evidence
	if phase == 3 {
		e = p.completedEvidence
	}
	if e == nil || phase < 1 || phase > 3 {
		return nil, storagebudget.ErrAuthority
	}
	previous, err := p.selectedEnrollmentCatalog(ctx)
	if err != nil {
		return nil, err
	}
	var digest storagebudget.Digest
	copy(digest[:], previous[32:64])
	marker := storageEnrollmentMarkerFor(*e, phase, digest)
	outer := storageEnrollmentPeakEnvelope{Kind: 2, BeforeCodec: storageEnrollmentPeakBeforeCodec(*p.scope), Sequence: storageEnrollmentPeakMarkerSequence(*p.scope, phase), Scope: *p.scope, Marker: &marker}
	return p.encodeEnrollmentEnvelope(outer)
}
func (p *storageEnrollmentEpoch) completePreparedEnrollment(ctx context.Context) error {
	if p.evidence == nil || p.evidence.Phase != 1 || p.factualCensus == nil {
		return storagebudget.ErrAuthority
	}
	prepared, err := p.selectedEnrollmentCatalog(ctx)
	if err != nil {
		return err
	}
	outer, err := p.enrollmentEnvelope(prepared, true)
	if err != nil || outer.Marker == nil || outer.Marker.Phase != 1 || storageEnrollmentPeakJoinMarker(outer, *p.evidence, p.original.Ready) != nil {
		return errors.Join(errStorageCustody, err)
	}
	if err = p.requireEnrollmentCatalogImage(ctx, prepared); err != nil {
		return err
	}
	for i := 0; i < 2; i++ {
		if err := p.dispatchEnrollmentQ(ctx); err != nil {
			return err
		}
	}
	d, err := p.enrollmentMarkerBytes(ctx, 2)
	if err != nil {
		return err
	}
	if err = p.publishEnrollmentControl(ctx, 13, d); err != nil {
		return err
	}
	if err = p.dispatchEnrollmentQ(ctx); err != nil {
		return err
	}
	completed := *p.evidence
	completed.Phase = 2
	completed.Bank = storageEnrollmentPeakEvidenceBank(*p.scope, 2)
	completed.Extent = p.scope.final(completed.Bank)
	completed.Generation++
	completed.Previous = sha256.Sum256(p.evidence.Raw)
	raw, err := storageEnrollmentPeakEncodeEvidenceOperands(completed, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim, &p.operands)
	if err != nil {
		return err
	}
	if err = p.publishEnrollmentControl(ctx, 7, raw); err != nil {
		return err
	}
	final, err := p.enrollmentMarkerBytes(ctx, 3)
	if err != nil {
		return err
	}
	for _, group := range []uint16{14, 15} {
		if err = p.publishEnrollmentControl(ctx, group, final); err != nil {
			return err
		}
	}
	return p.reconcileEnrollment(ctx, final)
}

// Both R barriers retain the exact Stage-A before old selected evidence may be
// overwritten by Stage-B/Armed. Only that actually referenced Stage-A supplies
// the recovery package; a missing/torn Stage-A never selects another frame.
func (p *storageEnrollmentEpoch) resumeReferencedGrowth(ctx context.Context, outer storageEnrollmentPeakEnvelope, stageA storageEnrollmentPeakEvidence) error {
	if !p.startup || outer.Marker == nil || outer.Marker.Phase != 0 || stageA.Phase != 0 || stageA.Scope.path() != 3 || stageA.Bank != 1-p.scope[323] || !p.scope.hasOriginalPeer() || storageEnrollmentPeakJoinMarker(outer, stageA, p.original.Ready) != nil {
		return errStorageEnrollmentUnresolved
	}
	record, claim := *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim
	var catalogs [2][]byte
	for i := range catalogs {
		raw, err := p.readEnrollmentTarget(ctx, byte(5+i))
		if err != nil {
			return err
		}
		catalogs[i] = raw
	}
	p.evidence = &stageA
	original, err := p.currentEnrollmentOriginal(ctx)
	if err != nil || storageEnrollmentPeakCompareOriginal(stageA, original, record, claim) != nil {
		return errors.Join(errStorageCustody, err)
	}
	current, err := p.c.ref.cell.q.InspectEnrollment(ctx)
	if err != nil {
		return err
	}
	q, err := storageEnrollmentPeakCompareQ(stageA, current.Checkpoints, current.Tickets, storageEnrollmentPeakDispatch{Bridge: stageA.Bridge})
	if err != nil || q.Stage != 0 || q.UnresolvedQAttempt {
		return errors.Join(errStorageEnrollmentUnresolved, err)
	}
	if _, _, err = storageEnrollmentPeakImmediateBefore(stageA, record, claim); err != nil {
		return err
	}
	repairR := !bytes.Equal(catalogs[0], catalogs[1])
	if repairR && p.previousEvidence == nil {
		return errStorageEnrollmentUnresolved
	}
	if p.previousEvidence != nil && (!storageEnrollmentPeakRetainsOrigins(stageA, *p.previousEvidence) || !bytes.Equal(stageA.OriginalV3, p.previousEvidence.OriginalV3)) {
		return errStorageCustody
	}
	unreferenced, err := p.compareSuccessorUnreferenced(ctx, stageA, &outer)
	if err != nil {
		return err
	}
	dispatch, err := p.successorEntered(ctx, stageA)
	if err != nil {
		return err
	}
	if err = p.comparePublishedCreation(ctx, stageA); err != nil {
		return err
	}
	ids := storageEnrollmentRecoveryPrefix()
	if repairR {
		ids = append(ids, 68, 88)
	}
	include := false
	for _, g := range storageEnrollmentPeakPlan(*p.scope) {
		if g.ID == 3 {
			include = true
		}
		if include {
			ids = append(ids, g.ID)
		}
	}
	if err = p.recoveryPass(ids); err != nil {
		return err
	}
	if err = p.persistRecoveryPrefix(ctx); err != nil {
		return err
	}
	if repairR {
		selected, err := p.selectedEnrollmentCatalog(ctx)
		if err != nil {
			return err
		}
		if err = p.publishEnrollmentControl(ctx, 68, selected); err != nil {
			return err
		}
	}
	selectedR, err := p.selectedEnrollmentCatalog(ctx)
	if err != nil {
		return err
	}
	if err = p.requireEnrollmentCatalogImage(ctx, selectedR); err != nil {
		return err
	}
	stageB := stageA
	stageB.Bank = p.scope[323]
	stageB.Extent = p.scope.final(stageB.Bank)
	stageB.Generation++
	stageB.Previous = sha256.Sum256(stageA.Raw)
	raw, err := storageEnrollmentPeakEncodeEvidenceOperands(stageB, record, claim, &p.operands)
	if err != nil {
		return err
	}
	if err = p.publishEnrollmentControl(ctx, 3, raw); err != nil {
		return err
	}
	stageB, err = p.decodeEnrollmentEvidence(raw)
	if err != nil {
		return err
	}
	if err = p.publishEnrollmentControl(ctx, 16, dispatch); err != nil {
		return err
	}
	p.evidence = nil
	if _, err = p.measureEnrollmentCandidate(ctx); err != nil {
		return err
	}
	if unreferenced != nil && unreferenced.Phase == 1 && !storageEnrollmentCensusEqual(*p.factualCensus, storageEnrollmentFactualCensus(unreferenced.Census)) {
		return errStorageCustody
	}
	armedRaw, err := storageEnrollmentPeakArmStagedOperands(stageB, *p.factualCensus, nil, record, claim, &p.operands)
	if err != nil {
		return err
	}
	if err = p.bindEnrollmentArmed(ctx, armedRaw); err != nil {
		return err
	}
	if !storageEnrollmentPeakArmedJoinsReferencedStage(*p.evidence, stageA, outer, record, claim) {
		return errStorageCustody
	}
	if err = p.publishEnrollmentControl(ctx, 5, armedRaw); err != nil {
		return err
	}
	prepared, err := p.enrollmentMarkerBytes(ctx, 1)
	if err != nil {
		return err
	}
	for _, id := range []uint16{11, 12} {
		if err = p.publishEnrollmentControl(ctx, id, prepared); err != nil {
			return err
		}
	}
	return p.completePreparedEnrollment(ctx)
}

// Bounded read of a captured peer while growth may not yet have reached its
// final extent. It never writes, truncates or promotes an unreferenced packet.
func (p *storageEnrollmentEpoch) readObservedEnrollmentTarget(ctx context.Context, target byte) ([]byte, error) {
	if target < 1 || target > 8 {
		return nil, storagebudget.ErrBounds
	}
	if err := p.nativePermit(ctx); err != nil {
		return nil, err
	}
	if err := p.targetCurrent(ctx, target, false); err != nil {
		return nil, err
	}
	t := p.targets[target-1]
	raw := make([]byte, int(t.named.size))
	n, err := t.named.file.ReadAt(raw, 0)
	if err != nil || n != len(raw) {
		return nil, errors.Join(storagebudget.ErrUnknown, err)
	}
	if err = p.targetCurrent(ctx, target, false); err != nil {
		return nil, err
	}
	return raw, p.nativePermit(ctx)
}

func (p *storageEnrollmentEpoch) enrollmentPublicationGroups(id uint16) []uint16 {
	if p == nil || p.scope == nil {
		return nil
	}
	plan := storageEnrollmentPeakPlan(*p.scope)
	var selected storageEnrollmentPeakGroup
	for _, g := range plan {
		if g.ID == id {
			selected = g
			break
		}
	}
	if selected.ID == 0 {
		return nil
	}
	ids := []uint16{id}
	if id == 1 || id == 3 || id == 5 || id == 7 {
		for _, g := range plan {
			if g.ID == id+1 {
				ids = append(ids, id+1)
			}
		}
	}
	return append(ids, selected.Completion)
}

// Encoded equality is data comparison only. It cannot manufacture a live
// creation member or discharge the source/catalog tail by itself.
func storageCreationRecordEqual(a, b *storageCreationIntent) bool {
	if a == nil || b == nil {
		return false
	}
	x, e := storageEncodeCreationIntent(*a, a.Tail.Installation, a.Tail.QBinding)
	y, f := storageEncodeCreationIntent(*b, b.Tail.Installation, b.Tail.QBinding)
	return e == nil && f == nil && bytes.Equal(x, y)
}
func storageEnrollmentUncoveredCreation(raw []byte, e storageEnrollmentPeakEvidence) (*storageCreationIntent, []byte, error) {
	return storageEnrollmentUncoveredCreationForIdentity(raw, e, false)
}
func storageEnrollmentUncoveredCreationForIdentity(raw []byte, e storageEnrollmentPeakEvidence, allowIdentity bool) (*storageCreationIntent, []byte, error) {
	if len(raw) != storageIntentBytes || e.Phase != 2 {
		return nil, nil, storagebudget.ErrBounds
	}
	var pending *storageCreationIntent
	var page []byte
	for slot := 0; slot < 64; slot++ {
		start := slot * 2 * storageIntentPage
		choice, err := storageSelectCreationHost(raw[start:start+storageIntentPage], raw[start+storageIntentPage:start+2*storageIntentPage], slot, e.Installation, e.QBinding)
		if err != nil {
			return nil, nil, err
		}
		if choice.PredecessorCreation != nil && choice.PredecessorCreation.Core.Phase == storageIntentRetired && !storageCreationTailIncluded(*choice.PredecessorCreation, e) {
			return nil, nil, errStorageEnrollmentUnresolved
		}
		if allowIdentity && (choice.Identity != nil || choice.Creation != nil && choice.Creation.Kind == storageCreationLocalPrivate) {
			continue
		}
		if !choice.Slot.record.reusable() {
			return nil, nil, errStorageEnrollmentUnresolved
		}
		for _, old := range e.Witnesses {
			if old.Selected && int(old.Slot) == slot && old.Core.Phase != storageIntentEmpty && choice.Slot.record.Phase == storageIntentEmpty {
				return nil, nil, errStorageOwnerRecovery
			}
		}
		if choice.Creation == nil || choice.Creation.Core.Phase != storageIntentRetired || storageCreationTailIncluded(*choice.Creation, e) {
			continue
		}
		if pending != nil || choice.Creation.Kind != storageCreationHistory && choice.Creation.Kind != storageCreationAncestor && choice.Creation.Kind != storageCreationPrivate {
			return nil, nil, errStorageEnrollmentUnresolved
		}
		value := *choice.Creation
		pending = &value
		page = append([]byte{}, choice.Slot.page...)
	}
	return pending, page, nil
}

// Only exclusion is acquired here. Inputs name already held startup sources;
// scope rows merely select a matching original description. Native permission
// remains closed until ordinary bindings and their full domain set validate.
func (p *storageEnrollmentEpoch) acquireStartupSourceDescriptions(ctx context.Context, catalog storageOwnerCatalogV2) error {
	if !p.startup || p.sourceHoldsComplete || len(p.sourceFiles) != 0 || len(p.holds) != 0 {
		return storagebudget.ErrAuthority
	}
	if err := p.held(ctx); err != nil {
		return err
	}
	if err := p.c.sources.current(ctx); err != nil {
		return err
	}
	for _, row := range catalog.Roots {
		if row.Role == 8 {
			if _, err := p.c.sources.exactHistoryAncestorScope(ctx, catalog, row); err != nil {
				return err
			}
		}
	}
	// Select descriptions already held by startup, in exactly the ordinary
	// orderedRoots order. Catalog data neither opens saved paths nor supplies a
	// domain capability. The subsequent exact binding pass must still validate
	// every source/owner/relation and reuse these actual inode holds.
	rows := append([]storageOwnerRoot{}, catalog.Roots...)
	sort.Slice(rows, func(i, j int) bool { return bytes.Compare(rows[i].Definition.ID[:], rows[j].Definition.ID[:]) < 0 })
	shared := false
	for _, row := range rows {
		if row.Role == 2 {
			continue
		}
		if int(row.DirectoryObject) >= len(catalog.Objects) {
			return storagebudget.ErrBounds
		}
		native := catalog.Objects[row.DirectoryObject].Persistent
		directory := row.DomainObject == row.DirectoryObject || row.Role == 7 && row.Source.Kind == 7 || row.Role == 8 && row.Source.Kind == 9
		if !directory {
			if int(row.DomainObject) >= len(catalog.Objects) {
				return storagebudget.ErrBounds
			}
			domain := catalog.Objects[row.DomainObject]
			config, err := storageOwnerPersistentRoot(ctx, p.c.original.config)
			if err != nil {
				return err
			}
			if domain.Role != 18 || string(domain.Name) != ".data.lock" || int(domain.Parent) >= len(catalog.Objects) || catalog.Objects[domain.Parent].Persistent != config {
				return errStorageCustody
			}
			if shared {
				continue
			}
			data, err := storageOpenOriginalControl(p.c.ref.cell, ".data.lock")
			if err != nil {
				return err
			}
			if err = p.keepStartupSourceFile(data); err != nil {
				return err
			}
			actual, err := storageOwnerPersistentFile(ctx, data)
			if err != nil || actual != domain.Persistent {
				return errors.Join(errStorageCustody, err)
			}
			if err = lockPrivateData(data.file, true); err != nil {
				return errors.Join(storagebudget.ErrBusy, err)
			}
			shared = true
			continue
		}
		var root *os.Root
		for _, source := range p.c.sources.roots {
			if source.persistent == native {
				if root != nil && root != source.root {
					return errStorageCustody
				}
				if err := source.current(ctx, p.c); err != nil {
					return err
				}
				root = source.root
			}
		}
		if row.Role == 7 {
			actual, err := storageOwnerPersistentRoot(ctx, p.c.project.own.root)
			if err != nil {
				return err
			}
			if actual == native {
				root = p.c.project.own.root
			}
			for _, project := range p.c.sources.projects {
				actual, err := storageOwnerPersistentRoot(ctx, project.named.root)
				if err != nil {
					return err
				}
				if actual == native {
					if err = project.named.current(); err != nil {
						return err
					}
					root = project.named.root
				}
			}
		}
		if root == nil {
			return errStorageCustody
		}
		if _, err := p.holdStartupSourceDirectory(ctx, root); err != nil {
			return err
		}
	}
	return p.current(ctx)
}
func (p *storageEnrollmentEpoch) holdStartupSourceDirectory(ctx context.Context, root *os.Root) (*os.File, error) {
	if !p.startup || root == nil {
		return nil, storagebudget.ErrAuthority
	}
	info, err := root.Stat(".")
	if err != nil {
		return nil, err
	}
	project, e := p.project.Stat()
	if e != nil {
		return nil, e
	}
	if os.SameFile(info, project) {
		return p.project, nil
	}
	for _, n := range p.sourceFiles {
		if os.SameFile(info, n.identity) {
			return n.file, nil
		}
	}
	for _, hold := range p.holds {
		if hold.root.domainDirectory && os.SameFile(info, hold.root.domain.identity) {
			if err := hold.current(ctx); err != nil {
				return nil, err
			}
			actual := hold
			for actual.borrowed != nil {
				actual = actual.borrowed
			}
			if actual.file == nil {
				return nil, storagebudget.ErrAuthority
			}
			return actual.file, nil
		}
	}
	file, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	held, err := file.Stat()
	if err != nil || !os.SameFile(info, held) {
		return nil, errors.Join(errStorageCustody, err, file.Close())
	}
	if err = lockPrivateData(file, true); err != nil {
		return nil, errors.Join(storagebudget.ErrBusy, err, file.Close())
	}
	if err = p.keepStartupSourceFile(storageNamedFile{root: root, name: ".", file: file, identity: held}); err != nil {
		return nil, err
	}
	return file, p.current(ctx)
}

// Restore the dictionary view, never the observed source descriptions or
// selected pointer. Exact registry/control material is compared before any
// index is copied; a new control must be the one captured by the retired J4.
func storageEnrollmentRestoreCreationDictionary(original storageOwnerCatalogV2, current storageOwnerCatalogV2, source *storageEnrollmentPeakSource) (storageOwnerCatalogV2, error) {
	var restored storageOwnerCatalogV2
	if !storageEnrollmentOriginalOwnerEqual(original, current, source) {
		return restored, errStorageCustody
	}
	extra := 0
	if source != nil {
		if !storageEnrollmentDefaultSourceNamespace(*source) || !storageEnrollmentDefaultControlAppendEqual(original, current, source) {
			return restored, errStorageCustody
		}
		extra = 1
	}
	if len(current.OwnerControls) != len(original.OwnerControls)+extra {
		return restored, errStorageCustody
	}
	for _, old := range original.OwnerControls {
		if int(old.ParentObject) >= len(original.Objects) {
			return restored, storagebudget.ErrBounds
		}
		found := false
		for _, next := range current.OwnerControls {
			if int(next.ParentObject) >= len(current.Objects) {
				return restored, storagebudget.ErrBounds
			}
			if original.Objects[old.ParentObject].Persistent == current.Objects[next.ParentObject].Persistent && bytes.Equal(old.Name, next.Name) {
				if found {
					return restored, errStorageCustody
				}
				a, e := original.controlDigest(old)
				b, f := current.controlDigest(next)
				if e != nil || f != nil || a != b || old.RelationDigest != next.RelationDigest || old.MaxBytes != next.MaxBytes {
					return restored, errStorageCustody
				}
				found = true
			}
		}
		if !found {
			return restored, errStorageCustody
		}
	}
	raw, err := json.Marshal(original)
	if err != nil {
		return restored, err
	}
	err = storageEnrollmentPeakCatalogJSON(raw, &restored)
	return restored, err
}
func (x *storageOriginalSources) restoreCreationDictionary(original storageOwnerCatalogV2, source *storageEnrollmentPeakSource) error {
	current := storageOwnerCatalogV2{OriginalOwner: x.owner, Objects: x.objects, OwnerControls: x.controls}
	controlSource := source
	if source != nil && source.Default == nil {
		if source.ActionPageDigest == (storagebudget.Digest{}) || !storageEnrollmentDefaultSourceNamespace(*source) {
			return errStorageCustody
		}
		controlSource = nil
	}
	restored, err := storageEnrollmentRestoreCreationDictionary(original, current, controlSource)
	if err != nil {
		return err
	}
	config := storageOwnerNoObject
	for i, o := range restored.Objects {
		if o.Role == 1 && o.Persistent == x.construction.ref.cell.ownerRecord.Config {
			if config != storageOwnerNoObject {
				return errStorageCustody
			}
			config = uint32(i)
		}
	}
	if config == storageOwnerNoObject {
		return errStorageCustody
	}
	x.owner = restored.OriginalOwner
	x.objects = restored.Objects
	x.controls = restored.OwnerControls
	x.configObject = config
	return nil
}

func storageCreationOriginalParentRows(record storageCreationIntent, catalog storageOwnerCatalogV2) ([]storageOwnerRoot, error) {
	if record.validate() != nil {
		return nil, storagebudget.ErrBounds
	}
	out := make([]storageOwnerRoot, len(record.Parents))
	for i, parent := range record.Parents {
		found := false
		for _, row := range catalog.Roots {
			if int(row.DirectoryObject) >= len(catalog.Objects) {
				return nil, storagebudget.ErrBounds
			}
			if catalog.Objects[row.DirectoryObject].Persistent != parent.Native {
				continue
			}
			if found {
				return nil, errStorageCustody
			}
			found = true
			out[i] = row
		}
		if !found {
			return nil, errStorageCustody
		}
		row := out[i]
		if storageTransferCatalogView(row) && row.Source.ControlSelection == 4 {
			return nil, storagebudget.ErrAuthority
		}
		switch parent.Role {
		case 1:
			if record.Kind != storageCreationPrivate || record.Branch != storageCreationContained || row.Role != 7 || row.Source.Kind != 7 || row.Category != storagebudget.Control || parent.Native != record.Tail.Project {
				return nil, errStorageCustody
			}
		case 2:
			if record.Kind != storageCreationPrivate || row.Role != 3 && row.Role != 6 || row.Category != storagebudget.Control {
				return nil, errStorageCustody
			}
		case 3:
			// Existing draft is verified separately as the exact named descendant;
			// it is not itself a registered root or a substitute for private custody.
			return nil, errStorageCustody
		case 4:
			if record.Branch == storageCreationPersonal && row.Role != 8 || record.Branch == storageCreationContained && row.Role != 3 && row.Role != 6 || record.Branch == storageCreationConfigured && row.Role != 1 {
				return nil, errStorageCustody
			}
		case 5:
			if row.Role != 1 && row.Role != 3 && row.Role != 6 || row.Category != storagebudget.Control {
				return nil, errStorageCustody
			}
		case 6:
			if record.legacyConfigSelection() {
				if row.Role != 1 || parent.Native != record.Tail.Config {
					return nil, errStorageCustody
				}
			} else if row.Role != 4 {
				return nil, errStorageCustody
			}
		case 7:
			if row.Role != 8 || row.Source.Kind != 9 {
				return nil, errStorageCustody
			}
		default:
			return nil, storagebudget.ErrAuthority
		}
		if parent.Component == 255 {
			if parent.Role != 6 {
				return nil, storagebudget.ErrBounds
			}
			continue
		}
		index := int(parent.Component)
		if index >= len(record.Core.Components) {
			return nil, storagebudget.ErrBounds
		}
		component := record.Core.Components[index]
		if component.Root != row.Definition.ID || component.RootGeneration != row.Definition.Generation || component.Owner != row.Definition.Owner || component.Filesystem != row.Definition.Filesystem || record.Core.RootIdentities[index] != row.Definition.Identity || record.Core.Locators[index] != row.Definition.Locator || component.CapacityCount != row.Definition.CapacityCount || component.CapacityBinding != row.Definition.CapacityBinding {
			return nil, errStorageCustody
		}
		fs := false
		for _, f := range catalog.Filesystems {
			if f.Definition.ID == component.Filesystem {
				if fs || f.Definition.Generation != component.FilesystemGeneration || f.Definition.Identity != record.Core.FilesystemIdentities[index] {
					return nil, errStorageCustody
				}
				fs = true
			}
		}
		if !fs {
			return nil, errStorageCustody
		}
	}
	return out, nil
}
func storageCreationRetiredQCurrent(record storageCreationIntent, current storagebudget.EnrollmentInspection, catalog storageOwnerCatalogV2) bool {
	if record.validate() != nil || record.Core.Phase != storageIntentRetired || record.Core.RetirementEpoch != record.Core.AdmissionEpoch || current.CheckpointBank > 1 || current.View.Configuration.Maintenance || current.View.Configuration.Epoch != record.Core.RetirementEpoch {
		return false
	}
	_, after, err := storageOwnerActualDefinitions(current.View, catalog)
	if err != nil || !after {
		return false
	}
	slot := current.View.Slots[record.Core.Token.Slot]
	if slot.PersistedApplied != record.Core.Token || slot.PersistedNextSequence <= record.Core.Token.Sequence {
		return false
	}
	if slot.Ticket.State != storagebudget.Empty && slot.Ticket.Token == record.Core.Token && (!storageCreationTicketMatches(record, slot.Ticket) || slot.Ticket.State != storagebudget.Terminal || slot.Ticket.Evidence != record.Core.Evidence) {
		return false
	}
	anchor := storageEnrollmentPeakQHeldAnchor{Installation: storageEnrollmentPeakQID(record.Tail.Installation), Binding: storageEnrollmentPeakQDigest(record.Tail.QBinding)}
	q, err := storageEnrollmentPeakQDecodeCheckpoint(current.Checkpoints[current.CheckpointBank], anchor)
	if err != nil {
		return false
	}
	for _, row := range q.Roots {
		if row.ID != (storageEnrollmentPeakQID{}) && !row.Known {
			return false
		}
	}
	for _, row := range q.Workspaces {
		if row.ID != (storageEnrollmentPeakQID{}) && !row.Known {
			return false
		}
	}
	return true
}

// Read-only recovery comparison under actual original startup exclusion. The
// installed feature profile must cover captured-object restart identity. All
// parents are original source descriptions; every child reopens its recorded
// native and exact payload. No member/call flags or effect is reconstructed.
func (p *storageEnrollmentEpoch) compareRetiredCreation(ctx context.Context, record storageCreationIntent, page []byte, catalog storageOwnerCatalogV2, current storagebudget.EnrollmentInspection) (*storageEnrollmentPeakSource, error) {
	if err := record.requireCapacityEstimate(); err != nil {
		return nil, err
	}
	if !p.startup || p.qualified == nil || record.Kind != storageCreationHistory && record.Kind != storageCreationAncestor && record.Kind != storageCreationPrivate || record.Tail.Installation != catalog.Installation || record.Tail.QBinding != catalog.QBinding || record.Tail.Project != p.scopeProject() || record.Tail.Config != p.c.ref.cell.ownerRecord.Config || record.Tail.Anchor != p.c.ref.cell.ownerRecord.Anchor || record.Tail.Qualification != p.qualified.digest || !storageCreationRetiredQCurrent(record, current, catalog) {
		return nil, errStorageEnrollmentUnresolved
	}
	encoded, err := storageEncodeCreationIntent(record, record.Tail.Installation, record.Tail.QBinding)
	if err != nil || !bytes.Equal(encoded, page) {
		return nil, errors.Join(errStorageCustody, err)
	}
	// The retained tail names the exact current old C/C and original control
	// natives/extents. No restored metadata is substituted for these bytes.
	for i := range p.targets {
		t := &p.targets[i]
		if t.native != record.Tail.Targets[i] || uint64(t.named.size) != record.Tail.Before[i] {
			return nil, errStorageCustody
		}
		if err = t.named.current(); err != nil {
			return nil, err
		}
		native, e := storageOwnerPersistentFile(ctx, t.named)
		if e != nil || native != t.native {
			return nil, errors.Join(errStorageCustody, e)
		}
		if i == 4 || i == 5 {
			raw := make([]byte, int(t.named.size))
			n, e := t.named.file.ReadAt(raw, 0)
			if e != nil || n != len(raw) || sha256.Sum256(raw) != record.Tail.Catalogs[i-4] {
				return nil, errors.Join(errStorageCustody, e)
			}
		}
	}
	return p.compareCapturedCreation(ctx, record, page, catalog)
}

// Shared read-side captured-effect comparison. The caller separately proves
// either the exact pre-successor current Q/C cut, or the existing published
// Scope/current-Q classifier and its exact retained immediate-before lineage.
// This method never derives a current Q stage from a historical checkpoint.
func (p *storageEnrollmentEpoch) compareCapturedCreation(ctx context.Context, record storageCreationIntent, page []byte, catalog storageOwnerCatalogV2) (*storageEnrollmentPeakSource, error) {
	if err := record.requireCapacityEstimate(); err != nil {
		return nil, err
	}
	if p == nil || !p.startup || p.qualified == nil || record.Core.Phase != storageIntentRetired || record.Kind != storageCreationHistory && record.Kind != storageCreationAncestor && record.Kind != storageCreationPrivate || record.Tail.Installation != catalog.Installation || record.Tail.QBinding != catalog.QBinding || record.Tail.Project != p.scopeProject() || record.Tail.Config != p.c.ref.cell.ownerRecord.Config || record.Tail.Anchor != p.c.ref.cell.ownerRecord.Anchor || record.Tail.Qualification != p.qualified.digest {
		return nil, errStorageEnrollmentUnresolved
	}
	encoded, err := storageEncodeCreationIntent(record, record.Tail.Installation, record.Tail.QBinding)
	if err != nil || !bytes.Equal(encoded, page) {
		return nil, errors.Join(errStorageCustody, err)
	}
	if err = p.held(ctx); err != nil {
		return nil, err
	}
	rows, err := storageCreationOriginalParentRows(record, catalog)
	if err != nil {
		return nil, err
	}
	roots := make([]*os.Root, len(rows))
	files := make([]*os.File, len(rows))
	for i, row := range rows {
		native := catalog.Objects[row.DirectoryObject].Persistent
		for _, source := range p.c.sources.roots {
			if source.persistent == native {
				if roots[i] != nil && roots[i] != source.root {
					return nil, errStorageCustody
				}
				if err = source.current(ctx, p.c); err != nil {
					return nil, err
				}
				roots[i] = source.root
			}
		}
		if roots[i] == nil {
			return nil, errStorageCustody
		}
		files[i], err = p.holdStartupSourceDirectory(ctx, roots[i])
		if err != nil {
			return nil, err
		}
		actual, e := storageAdvisoryPersistentHeld(ctx, files[i])
		if e != nil || actual != native {
			return nil, errors.Join(errStorageCustody, e)
		}
	}
	if err = p.qualified.creationParents(ctx, p, record.Kind, record.Branch, storageCreationOriginalRoles(record), files); err != nil {
		return nil, err
	}
	selected, marker, location := storageOwnerPersistentObject{}, storageOwnerPersistentObject{}, storageCreationEffect{}
	for i, parent := range record.Parents {
		if parent.Role == 6 {
			selected = parent.Native
			for _, object := range catalog.Objects {
				if object.Role == 23 && object.Parent == rows[i].DirectoryObject {
					if marker != (storageOwnerPersistentObject{}) {
						return nil, errStorageCustody
					}
					marker = object.Persistent
				}
			}
		}
	}
	effectRoots := make([]*os.Root, len(record.Effects))
	for i, effect := range record.Effects {
		if effect.State != storageCreationDurable {
			return nil, errStorageEnrollmentUnresolved
		}
		var parent *os.Root
		if int(effect.Parent) < len(roots) {
			parent = roots[effect.Parent]
		} else {
			at := int(effect.Parent) - len(roots)
			if at >= i || effectRoots[at] == nil {
				return nil, storagebudget.ErrBounds
			}
			parent = effectRoots[at]
		}
		if record.Kind == storageCreationPrivate {
			parentFile, e := p.holdStartupSourceDirectory(ctx, parent)
			if e != nil {
				return nil, e
			}
			if e = p.qualified.privateEffectParent(ctx, p, record.Kind, parentFile); e != nil {
				return nil, e
			}
		}
		if effect.Kind == 1 {
			edge, e := storagePinChild(parent, effect.Name, true)
			if e != nil {
				return nil, e
			}
			p.sourceRoots = append(p.sourceRoots, edge.root)
			p.sourceEdges = append(p.sourceEdges, edge)
			effectRoots[i] = edge.root
			native, e := storageOwnerPersistentRoot(ctx, edge.root)
			if e != nil || native != effect.Native {
				return nil, errors.Join(errStorageCustody, e)
			}
			if _, err = p.holdStartupSourceDirectory(ctx, edge.root); err != nil {
				return nil, err
			}
			selected = native
		} else {
			named, e := storageOpenFile(parent, effect.Name, int64(effect.Bytes))
			if e != nil {
				return nil, e
			}
			if err = p.keepStartupSourceFile(named); err != nil {
				return nil, err
			}
			raw := make([]byte, int(effect.Bytes))
			if len(raw) > 0 {
				n, e := named.file.ReadAt(raw, 0)
				if e != nil || n != len(raw) {
					return nil, errors.Join(errStorageCustody, e)
				}
			}
			if sha256.Sum256(raw) != effect.Payload {
				return nil, errStorageCustody
			}
			native, e := storageOwnerPersistentFile(ctx, named)
			if e != nil || native != effect.Native {
				return nil, errors.Join(errStorageCustody, e)
			}
			p.sourceFiles[len(p.sourceFiles)-1].immutable = append([]byte{}, raw...)
			if effect.Name == dataMarkerName {
				marker = native
			}
			if effect.Name == dataLocationName {
				location = effect
			}
		}
	}
	if err = p.current(ctx); err != nil {
		return nil, err
	}
	if record.Kind == storageCreationPrivate {
		if p.qualified.record.Version < 2 || record.Branch != storageCreationContained || len(rows) != 1 || len(record.Parents) != 1 || record.Parents[0].Role != 1 || rows[0].Role != 7 || rows[0].Source.Kind != 7 {
			return nil, errStorageCustody
		}
		binding, e := storageEnrollmentPeakDecodeSource(rows[0].Source.Relative)
		if e != nil || binding.Project != record.Parents[0].Native || binding.ActionPageDigest != (storagebudget.Digest{}) || !storageEnrollmentPeakSourceJoins(binding, catalog, rows[0]) {
			return nil, errStorageCustody
		}
		parent, e := p.c.sources.exactProjectScope(ctx, catalog, rows[0])
		if e != nil {
			return nil, e
		}
		child := storageOwnerPersistentObject{}
		for _, effect := range record.Effects {
			if effect.Parent == 0 && effect.Kind == 1 && effect.Name == ".desk-private" {
				if child != (storageOwnerPersistentObject{}) {
					return nil, errStorageCustody
				}
				child = effect.Native
			}
		}
		if !child.valid(1) {
			return nil, errStorageCustody
		}
		for _, root := range catalog.Roots {
			if catalog.Objects[root.DirectoryObject].Persistent == child {
				return nil, errStorageCustody
			}
		}
		var captured *storageOriginalSourceRoot
		for _, source := range p.c.sources.roots {
			if source.persistent != child {
				continue
			}
			if captured != nil || source.path != filepath.Join(parent.path, ".desk-private") || len(source.witnesses) == 0 || source.witnesses[len(source.witnesses)-1].parent != parent.root {
				return nil, errStorageCustody
			}
			if e = source.current(ctx, p.c); e != nil {
				return nil, e
			}
			captured = source
		}
		if captured == nil {
			return nil, errStorageCustody
		}
		captured.parent, captured.relative = parent, ".desk-private"
		captured.workspaces = append([]storagebudget.ID{}, rows[0].References...)
		binding.ActionPageDigest = sha256.Sum256(page)
		return &binding, nil
	}
	if record.Kind == storageCreationAncestor {
		if len(rows) != 1 || rows[0].Role != 8 {
			return nil, errStorageCustody
		}
		source, err := storageEnrollmentPeakDecodeSource(rows[0].Source.Relative)
		if err != nil || !p.c.sources.historyAncestorSelected(ctx, source) || source.Default == nil || string(source.Default.Next) != record.Core.Target || filepath.Join(string(source.Default.SelectedPath), record.Core.Target) != record.SelectedPath {
			return nil, errors.Join(errStorageCustody, err)
		}
		return nil, nil
	}
	settings := storageOwnerPersistentObject{}
	var settingsRoot *os.Root
	var settingsRow storageOwnerRoot
	for i, parent := range record.Parents {
		if parent.Role == 5 {
			settings = parent.Native
			settingsRoot = roots[i]
			settingsRow = rows[i]
		}
	}
	if !selected.valid(1) || settingsRoot == nil || !location.Native.valid(2) {
		return nil, errStorageCustody
	}
	found := false
	for _, selection := range p.c.sources.selections {
		if selection.settings == settingsRoot {
			if found || !selection.explicit || selection.location.Path != record.SelectedPath || selection.location.Previous != "" {
				return nil, errStorageCustody
			}
			found = true
		}
	}
	if !found {
		return nil, errStorageCustody
	}
	expected := p.c.server.defaultChatDataPath()
	if record.Branch == storageCreationContained {
		path := ""
		for _, source := range p.c.sources.roots {
			if source.root == settingsRoot {
				path = source.path
			}
		}
		expected = filepath.Join(path, "data")
	}
	if record.legacyConfigSelection() {
		expected = p.c.server.configDir
	}
	if record.SelectedPath != expected {
		return nil, errStorageCustody
	}
	source := &storageEnrollmentPeakSource{Namespace: []byte("data"), Default: &storageEnrollmentDefaultSource{Workflow: 1, Branch: record.Branch, Settings: settings, Selected: selected, Marker: marker, Location: location.Native, LocationPayload: location.Payload, CreationPage: sha256.Sum256(page), SelectedPath: []byte(record.SelectedPath), Next: []byte{}}}
	if record.Branch == storageCreationPersonal {
		source.Namespace = []byte("history")
	}
	if record.legacyConfigSelection() {
		source.Namespace = []byte("legacy")
		source.Default.Workflow = 3
	}
	if record.Branch == storageCreationContained {
		if len(settingsRow.References) != 1 {
			return nil, errStorageCustody
		}
		for _, w := range catalog.Workspaces {
			if w.Definition.ID == settingsRow.References[0] {
				source.OwnerKey = append([]byte{}, w.OwnerKey...)
				source.HistoryIdentity = append([]byte{}, w.HistoryIdentity...)
			}
		}
	}
	if !storageEnrollmentDefaultSourceNamespace(*source) {
		return nil, errStorageCustody
	}
	return source, p.current(ctx)
}
func (p *storageEnrollmentEpoch) scopeProject() storageOwnerPersistentObject {
	if p.scope != nil {
		return storageEnrollmentPeakNative(p.scope[286:321])
	}
	if p.original != nil {
		return p.original.Project
	}
	return storageOwnerPersistentObject{}
}

func (p *storageEnrollmentEpoch) resumeCompletedCreationStartup(ctx context.Context, outer storageEnrollmentPeakEnvelope, e storageEnrollmentPeakEvidence) (err error) {
	if !p.startup || outer.Marker == nil || outer.Marker.Phase != 3 || e.Phase != 2 {
		return errStorageEnrollmentUnresolved
	}
	// No persistence occurs before the original descriptions, complete current Q
	// and selected fixed host banks have been compared at this Completed cut.
	raw, err := p.readOriginalEnrollmentControl(ctx, storageIntentName, storageIntentBytes)
	if err != nil {
		return err
	}
	pending, page, err := storageEnrollmentUncoveredCreationForIdentity(raw, e, p.c.startupComposed)
	if err != nil {
		return err
	}
	var current storagebudget.EnrollmentInspection
	if p.c.startupComposed {
		current, err = p.c.ref.cell.q.InspectAction(ctx)
	} else {
		current, err = p.c.ref.cell.q.InspectEnrollment(ctx)
	}
	if err != nil {
		return err
	}
	_, after, err := storageOwnerActualDefinitions(current.View, e.Inner.Catalog)
	if err != nil || !after || current.View.Configuration.Maintenance || current.View.Configuration.Epoch != e.AfterEpoch || current.View.Configuration.Revision < e.AfterRevision {
		return errors.Join(errStorageEnrollmentUnresolved, err)
	}
	if err = p.acquireStartupSourceDescriptions(ctx, e.Inner.Catalog); err != nil {
		return err
	}
	var source *storageEnrollmentPeakSource
	if pending != nil {
		source, err = p.compareRetiredCreation(ctx, *pending, page, e.Inner.Catalog, current)
		if err != nil {
			return err
		}
		if source != nil && source.Default != nil && source.Default.Workflow == 3 {
			if err = p.c.sources.observeLegacyHistoryControl(ctx); err != nil {
				return err
			}
		}
	}
	if err = p.compareOriginalObjectDescriptions(ctx, e.Inner.Catalog); err != nil {
		return err
	}
	// This restores only the exact decoded after dictionary. Actual original
	// roots/files, registry edges, selected pointer and immutable observations
	// stay held/current. Ordinary binding comparison then remains exact.
	if err = p.c.sources.restoreCreationDictionary(e.Inner.Catalog, source); err != nil {
		return err
	}
	p.initialPacket = &e
	p.initialInspection = &current
	p.c.ref.cell.ownerCatalog = e.Inner.Catalog
	if err = p.c.attachOwnerBindings(ctx, e.Inner.Catalog, p.c.sources); err != nil {
		return err
	}
	if err = p.completeStartupSourceHolds(ctx); err != nil {
		return err
	}
	if err = p.prepareEnrollmentCandidate(ctx, e.Inner.Catalog); err != nil {
		return err
	}
	if pending != nil {
		if err = p.c.owner.validateCreationReferencedComponents(*pending); err != nil {
			return err
		}
	}
	if err = p.reattachCompletedEnrollment(ctx, outer, e, current, pending); err != nil {
		return err
	}
	if pending == nil {
		return nil
	}
	// Finish this already Completed invocation back into the same original
	// startup authority. A fresh existing epoch obtains the full installed after
	// domain set and repeats the captured-native/Q/J4 comparison before append.
	if err = p.finish(nil); err != nil {
		return err
	}
	return p.c.resumeRetiredCreationStartup(ctx, e, *pending, page)
}
func (c *storageBudgetConstruction) resumeRetiredCreationStartup(ctx context.Context, prior storageEnrollmentPeakEvidence, expected storageCreationIntent, page []byte) (err error) {
	p, err := c.beginStartupEnrollment(ctx)
	if err != nil {
		return err
	}
	defer func() { err = p.finish(err) }()
	if err = p.boundary("retired-successor-startup-before-acquire"); err != nil {
		return err
	}
	if err = p.acquire(ctx); err != nil {
		return err
	}
	if err = p.captureExistingOriginalForRetired(ctx, &expected); err != nil {
		return err
	}
	if err = p.qualifyOriginalNative(ctx); err != nil {
		return err
	}
	// Read the fixed host again after reacquisition; no caller data or retained
	// pointer substitutes for this invocation's current selected record.
	hosts, err := p.readOriginalEnrollmentControl(ctx, storageIntentName, storageIntentBytes)
	if err != nil {
		return err
	}
	pending, currentPage, err := storageEnrollmentUncoveredCreation(hosts, prior)
	if err != nil || !storageCreationRecordEqual(pending, &expected) || !bytes.Equal(currentPage, page) {
		return errors.Join(errStorageEnrollmentUnresolved, err)
	}
	if _, err = p.compareRetiredCreation(ctx, *pending, currentPage, prior.Inner.Catalog, *p.initialInspection); err != nil {
		return err
	}
	if err = c.owner.validateCreationReferencedComponents(*pending); err != nil {
		return err
	}
	rows, err := storageCreationOriginalParentRows(*pending, prior.Inner.Catalog)
	if err != nil {
		return err
	}
	parents := make([]*storageRootBinding, len(rows))
	for i, row := range rows {
		parents[i] = c.owner.roots[row.Definition.ID]
		if parents[i] == nil || parents[i].source == nil || parents[i].source.persistent != pending.Parents[i].Native {
			return errStorageCustody
		}
		if err = parents[i].current(ctx); err != nil {
			return err
		}
	}
	var catalog storageOwnerCatalogV2
	var source storageEnrollmentPeakSource
	if pending.Kind == storageCreationPrivate {
		catalog, source, err = p.planPrivateCatalogOriginal(ctx, *pending, currentPage, nil)
	} else if pending.Kind == storageCreationHistory {
		workspace, owner, e := p.historyCreationOwner(ctx, *pending, parents)
		if e != nil {
			return e
		}
		catalog, source, err = p.planHistoryCatalog(ctx, *pending, currentPage, parents, workspace, owner)
	} else {
		next, e := storageHistoryNextComponent(pending.SelectedPath, c.server.defaultChatDataPath())
		if e != nil {
			return e
		}
		catalog, source, err = p.planHistoryAncestorCatalog(ctx, c.server, pending.Effects[0].Native, pending.SelectedPath, next, 1, sha256.Sum256(currentPage), pending.Tail.Bridge)
	}
	if err != nil {
		return err
	}
	fence, prefix, err := p.planSuccessorEnrollment(ctx, catalog, source)
	if err != nil {
		return err
	}
	if err = p.admitInitialEnrollment(ctx, fence, prefix); err != nil {
		return err
	}
	if err = p.persistInitialEnrollmentFences(ctx); err != nil {
		return err
	}
	return p.runSuccessorEnrollmentControls(ctx)
}

// This is a resource bound derived from existing source/catalog cardinalities,
// not another operation allowance or durable ownership/accounting registry.
func (p *storageEnrollmentEpoch) keepStartupSourceFile(named storageNamedFile) error {
	if len(p.sourceFiles) >= 2*4096+9 {
		return errors.Join(storagebudget.ErrBounds, named.file.Close())
	}
	p.sourceFiles = append(p.sourceFiles, named)
	return nil
}

// Compare original object relations against observations from actual retained
// descriptions before restoring dictionary indices. Extra observations never
// replace missing originals. An independently rooted selected History keeps
// its old boundary/provenance even when discovery also has a named config edge.
func storageEnrollmentOriginalObjectsObserved(original, observed storageOwnerCatalogV2, boundaries []storageOwnerPersistentObject) bool {
	byNative := map[storageOwnerPersistentObject]storageOwnerObject{}
	for _, o := range observed.Objects {
		if _, duplicate := byNative[o.Persistent]; duplicate {
			return false
		}
		byNative[o.Persistent] = o
	}
	roots := map[storageOwnerPersistentObject]bool{}
	for _, native := range boundaries {
		roots[native] = true
	}
	originalSeen := map[storageOwnerPersistentObject]bool{}
	historyBoundary := func(index uint32) bool {
		for _, row := range original.Roots {
			if row.DirectoryObject == index && row.Role == 4 && row.Source.Kind == 4 && row.Source.ParentRoot == (storagebudget.ID{}) && original.Objects[index].Parent == storageOwnerNoObject {
				return true
			}
		}
		return false
	}
	for i, old := range original.Objects {
		if originalSeen[old.Persistent] {
			return false
		}
		originalSeen[old.Persistent] = true
		next, ok := byNative[old.Persistent]
		if !ok {
			scope := false
			for _, row := range original.Roots {
				if row.DirectoryObject == uint32(i) && (row.Role == 7 && row.Source.Kind == 7 || row.Role == 8 && row.Source.Kind == 9) {
					scope = true
				}
			}
			if !scope || !roots[old.Persistent] || old.Role != 20 || old.Source != 3 || old.OwnerClass != 2 || old.ModeClass != 1 || old.Parent != storageOwnerNoObject || len(old.Name) != 0 || old.FixedLength != 0 {
				return false
			}
			continue
		}
		if old.Role != next.Role || old.ModeClass != next.ModeClass || old.FixedLength != next.FixedLength {
			return false
		}
		if old.Source != next.Source || old.OwnerClass != next.OwnerClass {
			captured := old.Role == 21 && historyBoundary(uint32(i)) || old.Role == 23 && int(old.Parent) < len(original.Objects) && historyBoundary(old.Parent)
			if !captured || old.Source != 3 || old.OwnerClass != 2 || next.Source != 1 || next.OwnerClass != 1 {
				return false
			}
		}
		if old.Parent == storageOwnerNoObject {
			if !roots[old.Persistent] || len(old.Name) != 0 {
				return false
			}
		} else {
			if int(old.Parent) >= len(original.Objects) || int(next.Parent) >= len(observed.Objects) || original.Objects[old.Parent].Persistent != observed.Objects[next.Parent].Persistent || !bytes.Equal(old.Name, next.Name) {
				return false
			}
		}
	}
	return true
}
func (p *storageEnrollmentEpoch) compareOriginalObjectDescriptions(ctx context.Context, catalog storageOwnerCatalogV2) error {
	if !p.startup || len(p.sourceFiles) == 0 && !p.sourceHoldsComplete {
		return storagebudget.ErrAuthority
	}
	if err := p.held(ctx); err != nil {
		return err
	}
	if err := p.c.sources.current(ctx); err != nil {
		return err
	}
	boundaries := []storageOwnerPersistentObject{}
	roots := []*os.Root{p.c.project.own.root}
	for _, source := range p.c.sources.roots {
		roots = append(roots, source.root)
	}
	for _, project := range p.c.sources.projects {
		if err := project.named.current(); err != nil {
			return err
		}
		roots = append(roots, project.named.root)
	}
	if p.c.sources.homeNamed != nil {
		roots = append(roots, p.c.sources.homeNamed.root)
	}
	for _, root := range roots {
		native, err := storageOwnerPersistentRoot(ctx, root)
		if err != nil {
			return err
		}
		boundaries = append(boundaries, native)
	}
	observed := storageOwnerCatalogV2{Objects: p.c.sources.objects}
	if !storageEnrollmentOriginalObjectsObserved(catalog, observed, boundaries) {
		return errStorageCustody
	}
	return p.current(ctx)
}

// The published packet, never a caller-selected source page, selects its one
// creation witness. Exact saved-before bytes retain the pre-successor C/C and
// retirement Q0; actual current Q stage remains the surrounding classifier's
// responsibility (including valid later ordinary Q writes at Completed).
func storageEnrollmentPublishedCreation(e storageEnrollmentPeakEvidence, record storageOwnerRecordV2, claim storageOwnerClaimV2) (*storageCreationIntent, []byte, storageOwnerCatalogV2, error) {
	var before storageOwnerCatalogV2
	d := e.Board.Source.Default
	private := e.Board.SourceKind == 8 && e.Board.Source.ActionPageDigest != (storagebudget.Digest{})
	if !private && (d == nil || d.Workflow == 2 && d.OwnSelf == 0) {
		return nil, nil, before, nil
	}
	pageDigest := e.Board.Source.ActionPageDigest
	if d != nil {
		pageDigest = d.CreationPage
	}
	if pageDigest == (storagebudget.Digest{}) || !storageEnrollmentDefaultSourceNamespace(e.Board.Source) || e.Scope.path() == 1 {
		return nil, nil, before, errStorageCustody
	}
	var selected *storageCreationIntent
	var page []byte
	for _, w := range e.Witnesses {
		if w.Selected && w.Digest == pageDigest {
			if selected != nil || w.Creation == nil || sha256.Sum256(w.Page) != w.Digest {
				return nil, nil, before, errStorageCustody
			}
			value := *w.Creation
			selected = &value
			page = append([]byte{}, w.Page...)
		}
	}
	if selected == nil || !storageCreationTailBound(*selected, e) || selected.Tail.Bridge != e.Bridge || selected.Kind == storageCreationPrivate && !private || selected.Kind == storageCreationHistory && (d == nil || d.Workflow != 1 && d.Workflow != 3) || selected.Kind == storageCreationAncestor && (d == nil || d.Workflow != 2 || d.OwnSelf != 1) || selected.Kind != storageCreationHistory && selected.Kind != storageCreationAncestor && selected.Kind != storageCreationPrivate {
		return nil, nil, before, errStorageCustody
	}
	outer, raw, err := storageEnrollmentPeakImmediateBefore(e, record, claim)
	if err != nil {
		return nil, nil, before, err
	}
	found := false
	for _, projection := range e.Projections {
		if storageEnrollmentPeakProjectionFullDigest(projection) == e.Before {
			if found {
				return nil, nil, before, errStorageCustody
			}
			found = true
			before = projection.Catalog
		}
	}
	if !found || selected.Core.RetirementEpoch != e.BeforeEpoch {
		return nil, nil, before, errStorageCustody
	}
	hashes, err := storageEnrollmentCreationBeforeDigests(e, raw, record, claim)
	if err != nil {
		return nil, nil, before, err
	}
	err = storageEnrollmentCreationBeforeCatalogs(*selected, e.Scope, outer.Scope, hashes, e.Checkpoint.Checkpoint)
	if err != nil {
		return nil, nil, before, err
	}
	return selected, page, before, nil
}
func (p *storageEnrollmentEpoch) comparePublishedCreation(ctx context.Context, e storageEnrollmentPeakEvidence) error {
	if !p.startup || p.scope == nil || e.Scope != *p.scope || !p.sourceHoldsComplete {
		return storagebudget.ErrAuthority
	}
	selected, page, before, err := storageEnrollmentPublishedCreation(e, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
	if err != nil || selected == nil {
		return err
	}
	source, err := p.compareCapturedCreation(ctx, *selected, page, before)
	if err != nil {
		return err
	}
	if selected.Kind == storageCreationPrivate {
		if source == nil || !reflect.DeepEqual(*source, e.Board.Source) {
			return errStorageCustody
		}
	} else if selected.Kind == storageCreationHistory {
		if source == nil || !reflect.DeepEqual(source.Default, e.Board.Source.Default) || !bytes.Equal(source.Namespace, e.Board.Source.Namespace) {
			return errStorageCustody
		}
		if selected.Branch == storageCreationContained && (!bytes.Equal(source.OwnerKey, e.Board.Source.OwnerKey) || !bytes.Equal(source.HistoryIdentity, e.Board.Source.HistoryIdentity)) {
			return errStorageCustody
		}
	} else {
		d := e.Board.Source.Default
		if d == nil || d.Selected != selected.Effects[0].Native || !bytes.Equal(d.SelectedPath, []byte(selected.SelectedPath)) || d.CreationPage != sha256.Sum256(page) {
			return errStorageCustody
		}
	}
	if err = p.c.owner.validateCreationReferencedComponents(*selected); err != nil {
		return err
	}
	if err = p.compareOriginalObjectDescriptions(ctx, before); err != nil {
		return err
	}
	return p.current(ctx)
}

// Tail.Catalogs commits to the actual ORIGINAL two-bank images captured by
// originalCreationTail. ImmediateBefore retains a CORE commitment separately.
// Restore physical bytes only through the existing strict historical recipe;
// no bank reordering, core-hash substitution, current lookup or new authority.
func storageEnrollmentCreationBeforeDigests(e storageEnrollmentPeakEvidence, core []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) ([2]storagebudget.Digest, error) {
	switch e.Version {
	case 1:
		return [2]storagebudget.Digest{sha256.Sum256(core), sha256.Sum256(core)}, nil
	case 2:
		pair, err := e.compositionBefore(record, claim)
		if err != nil {
			return [2]storagebudget.Digest{}, err
		}
		return [2]storagebudget.Digest{sha256.Sum256(pair.Banks[0].Raw), sha256.Sum256(pair.Banks[1].Raw)}, nil
	case 3:
		actual, _, err := storagePolicyEvidenceNativeBefore(e, record, claim)
		if err != nil {
			return [2]storagebudget.Digest{}, err
		}
		return [2]storagebudget.Digest{sha256.Sum256(actual[0]), sha256.Sum256(actual[1])}, nil
	default:
		return [2]storagebudget.Digest{}, storagebudget.ErrBounds
	}
}

// Closed data equality after the caller validates the exact published packet,
// immediate-before marker/projection and current phase/Q. Retained retirement
// bytes are never presented as current physical Q by this helper.
func storageEnrollmentCreationBeforeLineage(selected storageCreationIntent, current, prior storageEnrollmentPeakScope, raw, checkpoint []byte) error {
	return storageEnrollmentCreationBeforeCatalogs(selected, current, prior, [2]storagebudget.Digest{sha256.Sum256(raw), sha256.Sum256(raw)}, checkpoint)
}

// Exact original two-bank hashes are supplied only after strict V6 projection
// restoration. Legacy callers retain their original duplicate-image contract.
func storageEnrollmentCreationBeforeCatalogs(selected storageCreationIntent, current, prior storageEnrollmentPeakScope, hashes [2]storagebudget.Digest, checkpoint []byte) error {
	if selected.validate() != nil || selected.Core.Phase != storageIntentRetired || selected.Tail.Catalogs != hashes {
		return errStorageCustody
	}
	for i := 0; i < 8; i++ {
		at := 1360 + i*32
		if selected.Tail.Targets[i] != prior.target(byte(i+1)) || selected.Tail.Targets[i] != current.target(byte(i+1)) || selected.Tail.Before[i] != storageEnrollmentPeakU64(prior[at+12:at+20]) || selected.Tail.Before[i] != storageEnrollmentPeakU64(current[at+4:at+12]) {
			return errStorageCustody
		}
	}
	anchor := storageEnrollmentPeakQHeldAnchor{Installation: storageEnrollmentPeakQID(selected.Tail.Installation), Binding: storageEnrollmentPeakQDigest(selected.Tail.QBinding)}
	q, err := storageEnrollmentPeakQDecodeCheckpoint(append([]byte{}, checkpoint...), anchor)
	if err != nil {
		return err
	}
	applied := q.Applied[selected.Core.Token.Slot]
	if q.Maintenance || q.Epoch != selected.Core.RetirementEpoch || applied.ID != storageEnrollmentPeakQID(selected.Core.Token.ID) || applied.Sequence != selected.Core.Token.Sequence || applied.Generation != selected.Core.Token.Generation || applied.NextSequence <= selected.Core.Token.Sequence {
		return errStorageCustody
	}
	for _, row := range q.Roots {
		if row.ID != (storageEnrollmentPeakQID{}) && !row.Known {
			return errStorageCustody
		}
	}
	for _, row := range q.Workspaces {
		if row.ID != (storageEnrollmentPeakQID{}) && !row.Known {
			return errStorageCustody
		}
	}
	return nil
}

// Bounded read-only observation before source binding. The original startup
// epoch has already opened/qualified this captured fixed native. No write,
// repair, Sync, borrowed source capability or historical/current substitution.
func (p *storageEnrollmentEpoch) readStartupCapturedTarget(ctx context.Context, target byte) ([]byte, error) {
	if !p.startup || p.scope == nil || p.qualified == nil || target < 1 || target > 8 {
		return nil, storagebudget.ErrAuthority
	}
	if err := p.held(ctx); err != nil {
		return nil, err
	}
	t := p.targets[target-1]
	if t.named.file == nil || t.native != p.scope.target(target) || t.named.size < 0 || uint64(t.named.size) < t.before || uint64(t.named.size) > t.final {
		return nil, errStorageCustody
	}
	if err := t.named.current(); err != nil {
		return nil, err
	}
	native, err := storageOwnerPersistentFile(ctx, t.named)
	if err != nil || native != t.native {
		return nil, errors.Join(errStorageCustody, err)
	}
	raw := make([]byte, int(t.named.size))
	n, err := t.named.file.ReadAt(raw, 0)
	if err != nil || n != len(raw) {
		return nil, errors.Join(storagebudget.ErrUnknown, err)
	}
	if err = t.named.current(); err != nil {
		return nil, err
	}
	return raw, p.held(ctx)
}

// Workspace/owner reconstruction uses complete prior rows. The source commits
// the focal identity; it never replaces the full settings reference census.
func storageEnrollmentSuccessorHistoryOwner(before storageOwnerCatalogV2, source storageEnrollmentPeakSource, record storageCreationIntent, parents []storageOwnerRoot) (storageOwnerWorkspace, []byte, error) {
	var workspace storageOwnerWorkspace
	for _, w := range before.Workspaces {
		if w.Definition.ID == source.Workspace {
			if workspace.Definition.ID != (storagebudget.ID{}) || w.Definition.Generation != source.WorkspaceGeneration || !bytes.Equal(w.OwnerKey, source.OwnerKey) || !bytes.Equal(w.HistoryIdentity, source.HistoryIdentity) {
				return workspace, nil, errStorageCustody
			}
			workspace = w
		}
	}
	if workspace.Definition.ID == (storagebudget.ID{}) || source.Default == nil {
		return workspace, nil, errStorageCustody
	}
	var settings *storageOwnerRoot
	for i, parent := range record.Parents {
		if parent.Role == 5 {
			if settings != nil || parent.Native != source.Default.Settings {
				return workspace, nil, errStorageCustody
			}
			settings = &parents[i]
		}
	}
	if settings == nil {
		return workspace, nil, errStorageCustody
	}
	if record.Branch != storageCreationContained {
		if settings.Role != 1 || source.Default.Settings != record.Tail.Config {
			return workspace, nil, errStorageCustody
		}
		return workspace, []byte(before.OriginalOwner.HomeOwner), nil
	}
	if settings.Role != 3 && settings.Role != 6 || len(settings.References) != 1 || settings.References[0] != workspace.Definition.ID {
		return workspace, nil, errStorageCustody
	}
	if before.OriginalOwner.Kind == 1 {
		return workspace, append([]byte{}, workspace.OwnerKey...), nil
	}
	if before.OriginalOwner.Kind != 2 {
		return workspace, nil, errStorageCustody
	}
	var owner []byte
	for _, entry := range before.OriginalOwner.Entries {
		if entry.RunnerWorkspace == string(workspace.OwnerKey) && bytes.Equal(entry.HistoryIdentity, workspace.HistoryIdentity) {
			if owner != nil {
				return workspace, nil, errStorageCustody
			}
			owner = []byte(entry.StorageOwner)
		}
	}
	if len(owner) == 0 {
		return workspace, nil, errStorageCustody
	}
	return workspace, owner, nil
}

// Recover only the exact encoding3 intent on this actual original pair. The
// data-only planned packet is checked before attachment; native permission
// remains closed until all actual original source domains have been acquired.
func (p *storageEnrollmentEpoch) resumeSuccessorFence(ctx context.Context, selected storageEnrollmentPeakEnvelope, catalogs [2][]byte, fence storageEnrollmentPeakEnvelope, fenceRaw []byte) error {
	if !p.startup || p.scope == nil || fence.Kind != 0 || fence.Scope != *p.scope || fence.Snapshot == nil || fence.Snapshot.Intent == nil || (fence.Snapshot.Encoding != 3 && fence.Snapshot.Encoding != 4 && fence.Snapshot.Encoding != 6) || p.scope.path() == 1 {
		return errStorageEnrollmentUnresolved
	}
	record, claim := *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim
	pair, _, err := p.enrollmentCatalogPair(catalogs)
	if err != nil || !reflect.DeepEqual(pair, selected) {
		return errors.Join(errStorageCustody, err)
	}
	intent := fence.Snapshot.Intent
	source, err := storageEnrollmentPeakDecodeSource(intent.Source)
	if err != nil {
		return err
	}
	priorRaw, err := p.readStartupCapturedTarget(ctx, 1+fence.Snapshot.Bank)
	if err != nil {
		return err
	}
	prior, err := p.decodeEnrollmentEvidence(priorRaw)
	if err != nil || storageEnrollmentPeakCompareSnapshot(fence, prior, record, claim) != nil || !storageEnrollmentPeakOriginalPeerRetained(fence.Scope, prior.Scope) {
		return errors.Join(errStorageCustody, err)
	}
	ready, err := p.readOriginalEnrollmentControl(ctx, storageReadyName, 32)
	if err != nil || !bytes.Equal(ready, record.QBinding[:]) || sha256.Sum256(ready) != prior.Ready {
		return errors.Join(errStorageCustody, err)
	}
	view, err := p.c.ref.cell.q.InspectEnrollment(ctx)
	if err != nil || view.View.Configuration.Maintenance {
		return errors.Join(errStorageEnrollmentUnresolved, err)
	}
	if fence.Snapshot.Encoding != 6 {
		_, after, err := storageOwnerActualDefinitions(view.View, prior.Inner.Catalog)
		if err != nil || !after || view.View.Configuration.Epoch != prior.AfterEpoch || view.View.Configuration.Revision < prior.AfterRevision {
			return errors.Join(errStorageCustody, err)
		}
	}
	hosts, err := p.readOriginalEnrollmentControl(ctx, storageIntentName, storageIntentBytes)
	if err != nil {
		return err
	}
	original := storageEnrollmentPeakOriginalComparison{Ready: sha256.Sum256(ready), Project: storageEnrollmentPeakNative(p.scope[286:321]), Catalogs: [2][]byte{append([]byte{}, fence.Snapshot.Before...), append([]byte{}, fence.Snapshot.Before...)}, Checkpoints: view.Checkpoints, Tickets: view.Tickets}
	if fence.Snapshot.Encoding == 6 {
		v, e := storageOwnerV6NativeDecode(catalogs[p.scope[322]], record, claim)
		if e != nil || v.Before == nil {
			v, e = storageOwnerV6NativeDecode(catalogs[1-p.scope[322]], record, claim)
		}
		if e != nil || v.Before == nil {
			return errors.Join(errStorageCustody, e)
		}
		original.Catalogs, e = v.Before.restore(fence.Snapshot.Before, v.Stage.Lineage, v.Policy, v.Overrides, prior.Inner.Catalog, record, claim)
		if e != nil || storageOwnerV6NativeCompareSnapshot(v, prior.Raw, original.Catalogs, record, claim) != nil {
			return errors.Join(errStorageCustody, e)
		}
		old, e := storageOwnerV6NativeReadEndpoint(original.Catalogs[p.scope[322]], prior.Raw, record, claim)
		if e != nil {
			return e
		}
		_, defs, e := prior.Inner.Catalog.definitions()
		if e != nil {
			return e
		}
		rows, e := storageOwnerV6DecodeSparse(old.Overrides, defs)
		if e != nil {
			return e
		}
		effective, e := storageOwnerV6Expand(defs, old.Policy, rows)
		if e != nil {
			return e
		}
		if e = storagePolicyQAtBoundary(effective, old.Boundary, view); e != nil {
			return e
		}

	} else if fence.Snapshot.Encoding == 4 {
		lineage := p.compositionLineage()
		if lineage == nil {
			return errStorageCustody
		}
		original.Catalogs, err = fence.Snapshot.Physical.restore(fence.Snapshot.Before, *lineage, record, claim)
		if err != nil {
			return err
		}
	}
	for slot := range original.Hosts {
		for bank := range original.Hosts[slot] {
			start := (slot*2 + bank) * storageIntentPage
			original.Hosts[slot][bank] = append([]byte{}, hosts[start:start+storageIntentPage]...)
		}
	}
	for i := range original.Targets {
		original.Targets[i] = p.scope.target(byte(i + 1))
		original.Extents[i] = storageEnrollmentPeakU64(p.scope[1360+i*32+4 : 1360+i*32+12])
	}
	oldStep3, err := p.readStartupCapturedTarget(ctx, 3+p.scope[324])
	if err != nil {
		return err
	}
	old, err := storageEnrollmentPeakDecodeDispatch(oldStep3, prior)
	if err != nil || old.Step != 3 {
		return errors.Join(errStorageCustody, err)
	}
	var inputs storagebudget.Digest
	if fence.Snapshot.Encoding == 6 {
		inputs, err = storageEnrollmentSuccessorNativeInputs(*p.scope, original, prior.Raw, ready, oldStep3, record, claim)
	} else if fence.Snapshot.Encoding == 4 {
		inputs, err = storageEnrollmentSuccessorCompositionInputs(*p.scope, original, prior.Raw, ready, oldStep3, fence.Snapshot.Physical, record, claim)
	} else {
		inputs, err = storageEnrollmentSuccessorInputs(*p.scope, original, prior.Raw, ready, oldStep3)
	}
	if err != nil || inputs != intent.Inputs {
		return errors.Join(errStorageCustody, err)
	}
	flags, err := storageEnrollmentSuccessorOriginFlags(original, prior, intent.Origin, record)
	if err != nil || flags != intent.OriginFlags {
		return errors.Join(errStorageCustody, err)
	}
	pending, page, err := storageEnrollmentUncoveredCreation(hosts, prior)
	if err != nil {
		return err
	}
	if intent.Family == 2 || intent.Family == 5 {
		if pending != nil {
			return errStorageCustody
		}
	} else {
		pageDigest, branch := source.ActionPageDigest, byte(storageCreationContained)
		if source.Default != nil {
			pageDigest, branch = source.Default.CreationPage, source.Default.Branch
		}
		if pending == nil || sha256.Sum256(page) != pageDigest || pending.Tail.Bridge != (storagebudget.ID([16]byte(p.scope[24:40]))) || pending.Branch != branch || !storageCreationRetiredQCurrent(*pending, view, prior.Inner.Catalog) {
			return errStorageCustody
		}
		oldOuter, err := storageEnrollmentPeakDecodeEnvelopeVariant(fence.Snapshot.Before, record, claim, true, p.compositionLineage() != nil)
		if err != nil {
			return err
		}
		if fence.Snapshot.Encoding == 6 {
			err = storageEnrollmentCreationBeforeCatalogs(*pending, *p.scope, oldOuter.Scope, [2]storagebudget.Digest{sha256.Sum256(original.Catalogs[0]), sha256.Sum256(original.Catalogs[1])}, view.Checkpoints[view.CheckpointBank])
		} else {
			err = storageEnrollmentCreationBeforeLineage(*pending, *p.scope, oldOuter.Scope, fence.Snapshot.Before, view.Checkpoints[view.CheckpointBank])
		}
		if err != nil {
			return err
		}
	}
	p.previousEvidence = &prior
	p.original = &original
	p.initialInspection = &view
	p.initialFence = append([]byte{}, fenceRaw...)
	var catalog storageOwnerCatalogV2
	var rebuilt storageEnrollmentPeakSource
	if intent.Family == 5 {
		catalog, rebuilt, err = p.planOriginalProjectScopeSource(ctx, &source)
	} else if intent.Family == 6 {
		if pending == nil || pending.Kind != storageCreationPrivate {
			return errStorageCustody
		}
		catalog, rebuilt, err = p.planPrivateCatalogOriginal(ctx, *pending, page, &source)
	} else if source.Default != nil && source.Default.Workflow == 2 {
		catalog, rebuilt, err = p.planHistoryAncestorCatalogOriginal(ctx, p.c.server, source.Default.Selected, string(source.Default.SelectedPath), string(source.Default.Next), source.Default.OwnSelf, source.Default.CreationPage, storagebudget.ID([16]byte(p.scope[24:40])), &source)
	} else {
		if pending == nil || pending.Kind != storageCreationHistory {
			return errStorageCustody
		}
		parents, e := storageCreationOriginalParentRows(*pending, prior.Inner.Catalog)
		if e != nil {
			return e
		}
		workspace, owner, e := storageEnrollmentSuccessorHistoryOwner(prior.Inner.Catalog, source, *pending, parents)
		if e != nil {
			return e
		}
		catalog, rebuilt, err = p.planHistoryCatalogOriginal(ctx, *pending, page, parents, workspace, owner)
	}
	if err != nil {
		return err
	}
	rawSource, err := storageEnrollmentPeakEncodeSource(rebuilt)
	if err != nil || !bytes.Equal(rawSource, intent.Source) {
		return errors.Join(errStorageCustody, err)
	}
	model, err := p.initialPacketModelOriginal(ctx, catalog, rebuilt, intent)
	if err != nil {
		return err
	}
	model, err = p.bindSuccessorModel(model, *p.scope)
	if err != nil {
		return err
	}
	prefix, err := storageEnrollmentSuccessorPrefix(model)
	if err != nil {
		return err
	}
	prefix.FreshOrigin = model.FreshOrigin
	prefix.OriginalStep3 = oldStep3
	if err = storageEnrollmentPeakComparePrefix(storageEnrollmentPeakComparisonInput{Original: original, PreviousEvidence: prior.Raw, CurrentCheckpoints: view.Checkpoints, CurrentTickets: view.Tickets, Prefix: prefix}, fence, record, claim); err != nil {
		return err
	}
	p.initialPacket = &model
	// The full checked intent supplies only the narrow existing mapping
	// context. Actual descriptions/locks and captured effects are independent.
	if err = p.c.sources.observeCatalogLegacyHistoryControl(ctx, catalog); err != nil {
		return err
	}
	if err = p.acquireStartupSourceDescriptions(ctx, catalog); err != nil {
		return err
	}
	var restoredSource *storageEnrollmentPeakSource
	if pending != nil {
		captured, e := p.compareCapturedCreation(ctx, *pending, page, prior.Inner.Catalog)
		if e != nil {
			return e
		}
		if pending.Kind == storageCreationPrivate {
			if captured == nil || !reflect.DeepEqual(*captured, source) {
				return errStorageCustody
			}
		} else if pending.Kind == storageCreationHistory {
			if captured == nil || !reflect.DeepEqual(captured.Default, source.Default) {
				return errStorageCustody
			}
			restoredSource = &source
		}
	}
	if err = p.compareOriginalObjectDescriptions(ctx, prior.Inner.Catalog); err != nil {
		return err
	}
	if err = p.c.sources.restoreCreationDictionary(prior.Inner.Catalog, restoredSource); err != nil {
		return err
	}
	p.c.ref.cell.ownerCatalog = prior.Inner.Catalog
	if err = p.c.attachOwnerBindings(ctx, prior.Inner.Catalog, p.c.sources); err != nil {
		return err
	}
	if err = p.completeStartupSourceHolds(ctx); err != nil {
		return err
	}
	if pending != nil {
		if err = p.c.owner.validateCreationReferencedComponents(*pending); err != nil {
			return err
		}
	}
	if err = p.prepareEnrollmentCandidate(ctx, catalog); err != nil {
		return err
	}
	if err = p.comparePublishedCreation(ctx, model); err != nil {
		return err
	}
	if err = p.recheckSuccessorFenceInputs(ctx, catalogs, *intent); err != nil {
		return err
	}
	if selected.Marker != nil {
		observed, err := p.readEnrollmentTarget(ctx, 1+selected.Marker.Bank)
		if err != nil {
			return err
		}
		evidence, err := p.decodeEnrollmentEvidence(observed)
		if err != nil || storageEnrollmentPeakJoinMarker(selected, evidence, original.Ready) != nil {
			return errors.Join(errStorageCustody, err)
		}
		if selected.Marker.Phase == 0 && p.scope.path() == 3 {
			expected, err := p.successorStagedImage(model)
			if err != nil || !bytes.Equal(expected, evidence.Raw) {
				return errors.Join(errStorageCustody, err)
			}
			p.anchoredStaged = &evidence
			p.anchoredReference = &selected
			p.fenced = true
			return p.resumeReferencedGrowth(ctx, selected, evidence)
		}
		if selected.Marker.Phase == 1 && p.scope.path() == 2 {
			if err = storageEnrollmentSuccessorArmedMatches(evidence, model, prior, record, claim); err != nil {
				return err
			}
			p.fenced = true
			return p.continueOriginalEnrollment(ctx, selected, evidence)
		}
		return errStorageEnrollmentUnresolved
	}
	unreferenced, err := p.compareSuccessorUnreferenced(ctx, model, nil)
	if err != nil {
		return err
	}
	if !bytes.Equal(catalogs[0], catalogs[1]) {
		if unreferenced != nil {
			return errStorageCustody
		}
		peer, err := p.readEnrollmentTarget(ctx, 3+(1-p.scope[324]))
		if err != nil {
			return err
		}
		if d, e := storageEnrollmentPeakDecodeDispatch(peer, model); e == nil && d.Bridge == model.Bridge {
			return errStorageCustody
		}
	}
	ids := storageEnrollmentRecoveryPrefix()
	if !bytes.Equal(catalogs[0], catalogs[1]) {
		ids = append(ids, 10, 45)
	}
	include := false
	for _, g := range storageEnrollmentPeakPlan(*p.scope) {
		if g.ID == 16 && p.scope.path() == 2 || g.ID == 1 && p.scope.path() == 3 {
			include = true
		}
		if include {
			ids = append(ids, g.ID)
		}
	}
	if err = p.recoveryPass(ids); err != nil {
		return err
	}
	if err = p.persistRecoveryPrefix(ctx); err != nil {
		return err
	}
	if !bytes.Equal(catalogs[0], catalogs[1]) {
		if err = p.publishEnrollmentControl(ctx, 10, p.initialFence); err != nil {
			return err
		}
	}
	for target := byte(5); target <= 6; target++ {
		raw, e := p.readEnrollmentTarget(ctx, target)
		if e != nil || !bytes.Equal(raw, p.initialFence) {
			return errors.Join(errStorageEnrollmentUnresolved, e)
		}
	}
	p.fenced = true
	return p.runSuccessorEnrollmentControls(ctx)
}

func (p *storageEnrollmentEpoch) successorStagedImage(model storageEnrollmentPeakEvidence) ([]byte, error) {
	model.Scope = *p.scope
	model.Bank = 1 - p.scope[323]
	model.Extent = p.scope.final(model.Bank)
	board, err := storageEnrollmentPeakWriteBoard(model.Board, model.Scope, 0, model.Digests, model.Inner.Catalog, len(model.Views), len(model.Projections), len(model.Witnesses))
	if err != nil {
		return nil, err
	}
	model.Sections[8] = storageEnrollmentPeakSection{Count: 1, Raw: board}
	return storageEnrollmentPeakEncodeEvidenceOperands(model, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim, &p.operands)
}

func storageEnrollmentSuccessorArmedMatches(armed, model, prior storageEnrollmentPeakEvidence, record storageOwnerRecordV2, claim storageOwnerClaimV2) error {
	if model.Scope.path() != 2 || armed.Scope != model.Scope || armed.Phase != 1 {
		return errStorageCustody
	}
	raw, err := storageEnrollmentPeakArmPlanned(model, storageEnrollmentFactualCensus(armed.Census), prior.Generation+1, sha256.Sum256(prior.Raw), nil, record, claim)
	if err != nil || !bytes.Equal(raw, armed.Raw) {
		return errors.Join(errStorageCustody, err)
	}
	return nil
}

// A recognized complete unreferenced image must be the exact old peer or this
// one planned transition. A torn pure image is never promoted; the existing
// fixed-native write may replace it only before a later outer selects it.
func (p *storageEnrollmentEpoch) compareSuccessorUnreferenced(ctx context.Context, model storageEnrollmentPeakEvidence, reference *storageEnrollmentPeakEnvelope) (*storageEnrollmentPeakEvidence, error) {
	if p.scope == nil || model.Scope != *p.scope {
		return nil, storagebudget.ErrAuthority
	}
	bank := byte(1 - p.scope[323])
	if reference != nil {
		bank = p.scope[323]
	}
	raw, err := p.readObservedEnrollmentTarget(ctx, 1+bank)
	if err != nil {
		return nil, err
	}
	value, err := p.decodeEnrollmentEvidence(raw)
	if err != nil {
		if len(raw) < 16 || string(raw[:8]) != "DSKEPP1\x00" || storageEnrollmentPeakU32(raw[8:12]) != 1 || storageEnrollmentPeakU32(raw[12:16]) != 65536 {
			return nil, errStorageCustody
		}
		return nil, nil
	}
	if reference == nil && p.previousEvidence != nil && storageEnrollmentPeakJoinArmed(*p.previousEvidence, value) {
		return nil, nil
	}
	if reference != nil && value.Phase == 2 && sha256.Sum256(value.Raw) == model.Board.Prior.Evidence {
		return nil, nil
	}
	if value.Scope != model.Scope {
		return nil, errStorageCustody
	}
	if reference == nil {
		if p.scope.path() == 3 {
			expected, err := p.successorStagedImage(model)
			if err != nil || !bytes.Equal(expected, value.Raw) {
				return nil, errors.Join(errStorageCustody, err)
			}
			return &value, nil
		}
		if err = storageEnrollmentSuccessorArmedMatches(value, model, *p.previousEvidence, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim); err != nil {
			return nil, err
		}
		return &value, nil
	}
	if value.Phase == 1 {
		if !storageEnrollmentPeakArmedJoinsReferencedStage(value, model, *reference, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim) {
			return nil, errStorageCustody
		}
		return &value, nil
	}
	expected := model
	expected.Bank = p.scope[323]
	expected.Extent = p.scope.final(expected.Bank)
	expected.Generation++
	expected.Previous = sha256.Sum256(model.Raw)
	body, err := storageEnrollmentPeakEncodeEvidenceOperands(expected, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim, &p.operands)
	if err != nil || !bytes.Equal(body, value.Raw) {
		return nil, errors.Join(errStorageCustody, err)
	}
	return &value, nil
}

// Exact oldStep3 plus current immutable packet operands reproduce only
// Entered. This grants no fresh Q call and refuses any recognized later arm.
func (p *storageEnrollmentEpoch) successorEntered(ctx context.Context, model storageEnrollmentPeakEvidence) ([]byte, error) {
	var steps [2][]byte
	for bank := range steps {
		raw, err := p.readEnrollmentTarget(ctx, byte(3+bank))
		if err != nil {
			return nil, err
		}
		steps[bank] = raw
	}
	var old storageEnrollmentPeakDispatch
	var err error
	if p.previousEvidence != nil {
		old, err = storageEnrollmentPeakDecodeDispatch(steps[p.scope[324]], *p.previousEvidence)
	} else {
		old, err = storageEnrollmentPeakDecodeCompletedPredecessor(steps[p.scope[324]], model, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
	}
	if err != nil || old.Step != 3 || old.Generation == ^uint64(0) {
		return nil, errors.Join(errStorageCustody, err)
	}
	entered := storageEnrollmentPeakDispatch{Generation: old.Generation + 1, Installation: model.Installation, Binding: model.QBinding, Bridge: model.Bridge, Previous: sha256.Sum256(old.Raw), BeforeRevision: model.BeforeRevision, BeforeEpoch: model.BeforeEpoch, Before: model.Checkpoint.Checkpoints[model.Checkpoint.Bank], Origin: model.Digests.Origin, Controls: [2]storageOwnerPersistentObject{model.Controls[2], model.Controls[3]}}
	raw, err := storageEnrollmentPeakEncodeDispatch(entered, model)
	if err != nil {
		return nil, err
	}
	if p.previousEvidence != nil {
		selected, bank, e := storageEnrollmentPeakDecodeDispatchPair(steps[0], steps[1], *p.previousEvidence, nil)
		if e == nil && selected.Step == 3 && byte(bank) == p.scope[324] {
			return raw, nil
		}
	}
	if !p.startup {
		return nil, errStorageCustody
	}
	var catalogs [2][]byte
	for bank := range catalogs {
		catalogs[bank], err = p.readEnrollmentTarget(ctx, byte(5+bank))
		if err != nil {
			return nil, err
		}
	}
	outer, _, err := p.enrollmentCatalogPair(catalogs)
	if err != nil || outer.Scope != *p.scope {
		return nil, errors.Join(errStorageCustody, err)
	}
	if outer.Kind == 0 {
		if outer.Snapshot == nil || outer.Snapshot.Intent == nil {
			return nil, errStorageEnrollmentUnresolved
		}
	} else if outer.Marker == nil || outer.Marker.Phase != 0 || p.anchoredStaged == nil || storageEnrollmentPeakJoinMarker(outer, *p.anchoredStaged, model.Ready) != nil {
		return nil, errStorageCustody
	}
	q, err := p.c.ref.cell.q.InspectEnrollment(ctx)
	if err != nil {
		return nil, err
	}
	stage, err := storageEnrollmentPeakCompareQ(model, q.Checkpoints, q.Tickets, storageEnrollmentPeakDispatch{Bridge: model.Bridge})
	if err != nil || stage.Stage != 0 || stage.UnresolvedQAttempt {
		return nil, errors.Join(errStorageEnrollmentUnresolved, err)
	}
	peer := steps[1-p.scope[324]]
	if bytes.Equal(peer, raw) {
		return raw, nil
	}
	// Known complete pages of either lineage cannot be erased as torn writes.
	if _, e := storageEnrollmentPeakDecodeDispatch(peer, model); e == nil {
		return nil, errStorageCustody
	}
	if p.previousEvidence != nil {
		if _, e := storageEnrollmentPeakDecodeDispatch(peer, *p.previousEvidence); e == nil {
			return nil, errStorageCustody
		}
	}
	if len(peer) != 4096 || string(peer[:8]) != "DSKEPS1\x00" || storageEnrollmentPeakU32(peer[8:12]) != 1 || storageEnrollmentPeakU32(peer[12:16]) != 192 {
		return nil, errStorageCustody
	}
	return raw, nil
}

func (p *storageEnrollmentEpoch) requireEnrollmentCatalogImage(ctx context.Context, want []byte) error {
	if len(want) != 8388608 {
		return storagebudget.ErrBounds
	}
	var raws [2][]byte
	for bank := range raws {
		var err error
		raws[bank], err = p.readEnrollmentTarget(ctx, byte(5+bank))
		if err != nil {
			return err
		}
	}
	if _, _, err := p.enrollmentCatalogPair(raws); err != nil {
		return err
	}
	for _, raw := range raws {
		actual, err := p.enrollmentCore(raw)
		if err != nil || !bytes.Equal(actual, want) {
			return errors.Join(errStorageEnrollmentUnresolved, err)
		}
	}
	return p.held(ctx)
}

// Repeat the complete physical read after original source exclusion and
// binding validation, immediately before selecting a publication pass. Old C/C
// remain retained Before operands; current C bytes are compared separately.
func (p *storageEnrollmentEpoch) recheckSuccessorFenceInputs(ctx context.Context, catalogs [2][]byte, intent storageEnrollmentSuccessorIntent) error {
	if !p.startup || !p.sourceHoldsComplete || p.original == nil || p.previousEvidence == nil {
		return storagebudget.ErrAuthority
	}
	for bank := range catalogs {
		actual, err := p.readEnrollmentTarget(ctx, byte(5+bank))
		if err != nil || !bytes.Equal(actual, catalogs[bank]) {
			return errors.Join(errStorageCustody, err)
		}
	}
	current := *p.original
	view, err := p.c.ref.cell.q.InspectEnrollment(ctx)
	if err != nil {
		return err
	}
	current.Checkpoints, current.Tickets = view.Checkpoints, view.Tickets
	raw, err := p.readOriginalEnrollmentControl(ctx, storageIntentName, storageIntentBytes)
	if err != nil {
		return err
	}
	for slot := range current.Hosts {
		for bank := range current.Hosts[slot] {
			at := (slot*2 + bank) * storageIntentPage
			current.Hosts[slot][bank] = raw[at : at+storageIntentPage]
		}
	}
	ready, err := p.readOriginalEnrollmentControl(ctx, storageReadyName, 32)
	if err != nil {
		return err
	}
	prior, err := p.readObservedEnrollmentTarget(ctx, 1+p.scope[323])
	if err != nil || !bytes.Equal(prior, p.previousEvidence.Raw) {
		return errors.Join(errStorageCustody, err)
	}
	step, err := p.readEnrollmentTarget(ctx, 3+p.scope[324])
	if err != nil {
		return err
	}
	var digest storagebudget.Digest
	if storagePolicyActualPair(current.Catalogs) {
		digest, err = storageEnrollmentSuccessorNativeInputs(*p.scope, current, prior, ready, step, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
	} else if p.compositionLineage() != nil {
		projection, e := storageCompositionCaptureProjection(current.Catalogs[0], current.Catalogs[1], *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
		if e != nil {
			return e
		}
		digest, err = storageEnrollmentSuccessorCompositionInputs(*p.scope, current, prior, ready, step, projection, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
	} else {
		digest, err = storageEnrollmentSuccessorInputs(*p.scope, current, prior, ready, step)
	}
	if err != nil || digest != intent.Inputs {
		return errors.Join(errStorageCustody, err)
	}
	return p.current(ctx)
}

// First-base recovery must reproduce the exact full original Q commitment
// before source discovery or ordinary census. Established wrappers are checked
// again under the original identity/enrollment epoch and all native domains.
func (c *storageBudgetConstruction) validateStartupCompositionQ(ctx context.Context, record storageOwnerRecordV2, claim storageOwnerClaimV2) error {
	if !c.startupFirstBase {
		return nil
	}
	read := func(name string, size int64) (raw []byte, err error) {
		raw, n, err := storageOwnerReadFixed(c.original.config, name, size)
		if err != nil {
			return nil, err
		}
		defer func() { err = errors.Join(err, n.current(), n.file.Close()) }()
		return raw, nil
	}
	recordRaw, err := read(storageRecordName, storageOwnerRecordBytes)
	if err != nil {
		return err
	}
	claimRaw, err := read(storageClaimName, storageOwnerRecordBytes)
	if err != nil {
		return err
	}
	ready, err := read(storageReadyName, 32)
	if err != nil {
		return err
	}
	q, err := c.ref.cell.q.InspectAction(ctx)
	if err != nil {
		return err
	}
	plan, _, err := storageCompositionRecoverFirstBase(c.startupPhysical, recordRaw, claimRaw, ready, q.View, record, claim)
	if err != nil || !bytes.Equal(plan.Original[0], c.startupCores[0]) || !bytes.Equal(plan.Original[1], c.startupCores[1]) {
		return errors.Join(errStorageEnrollmentUnresolved, err)
	}
	return c.current(ctx, true)
}

// A retained P supplies only the immutable discovery comparison key. It is
// joined to this launch's adopted project and original config, never copied
// into the identity/signing state. Full original dictionary, Q definitions,
// native sources and every retained reference must still validate at attach
// and action admission; this read-only selection grants no authority.
func (c *storageBudgetConstruction) inspectOriginalStartupKey(ctx context.Context) (string, error) {
	if c.server.home != nil {
		return c.server.inspectStorageStartupKey()
	}
	lineage := c.startupLineage
	if c.owner != nil {
		g := &c.owner.admission
		g.mu.Lock()
		epoch := g.enrollment
		g.mu.Unlock()
		if epoch != nil && epoch.c == c {
			lineage = epoch.compositionLineage()
		} else if lineage == nil {
			current, e := c.owner.identityLineage(ctx)
			if e == nil {
				lineage = &current
			}
		}
	}
	if lineage == nil {
		return c.server.inspectStorageStartupKey()
	}
	project, err := storageOwnerPersistentRoot(ctx, c.project.own.root)
	if err != nil {
		return "", err
	}
	config, err := storageOwnerPersistentRoot(ctx, c.original.config)
	if err != nil {
		return "", err
	}
	history, err := c.originalStorageHistory(ctx)
	if err != nil {
		return "", err
	}
	key := ""
	for _, plan := range lineage.Rows.LineagePlans {
		if plan.ProjectObject != project {
			continue
		}
		if plan.ConfigObject != config {
			return "", errStorageCustody
		}
		if key != "" {
			return "", errStorageCustody
		}
		if len(plan.AccountingOwnerKey) == 0 {
			return "", errStorageCustody
		}
		if string(plan.HistoryIdentity) != history {
			return "", errStorageCustody
		}
		key = string(plan.AccountingOwnerKey)
	}
	if key != "" {
		return key, nil
	}
	return c.server.inspectStorageStartupKey()
}

// Decode only a strict actual current physical pair. The detached original
// cores feed the existing E1 selector; all comparisons/publication retain raw.
func (p *storageEnrollmentEpoch) enrollmentCatalogPair(raw [2][]byte) (storageEnrollmentPeakEnvelope, int, error) {
	record, claim := *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim
	if storagePolicyActualPair(raw) {
		var before *storageOwnerCatalogV2
		var evidence []byte
		if p.previousEvidence != nil {
			before = &p.previousEvidence.Inner.Catalog
			evidence = p.previousEvidence.Raw
		} else if p.c.ref.cell.enrollmentEndpoint != nil {
			before = &p.c.ref.cell.ownerCatalog
			evidence = p.c.ref.cell.enrollmentEndpoint.Raw
		}
		pair, e := storageOwnerV6NativeReadPair(raw[0], raw[1], before, evidence, record, claim)
		if e != nil {
			return storageEnrollmentPeakEnvelope{}, 0, e
		}
		outer, e := storageEnrollmentPeakDecodeEnvelopeNative(pair.Banks[pair.Selected].Stage.Core, record, claim, true, true, true)
		return outer, int(pair.Selected), e
	}
	lineage := p.compositionLineage()
	if lineage == nil {
		return storageEnrollmentPeakDecodePair(raw[0], raw[1], record, claim)
	}
	pair, err := storageCompositionReadPair(raw[0], raw[1], record, claim)
	if err != nil || !reflect.DeepEqual(pair.Banks[pair.Selected].Lineage, *lineage) {
		return storageEnrollmentPeakEnvelope{}, 0, errors.Join(errStorageCustody, err)
	}
	return storageEnrollmentPeakDecodePairVariant(pair.Banks[0].Core, pair.Banks[1].Core, record, claim, true)
}

// Read the finite original native source cut before ordinary source discovery.
// This selects the existing E1 recovery path; it returns no ordinary readiness
// or native permit and never substitutes a decoded source for an actual bank.
func (c *storageBudgetConstruction) resumeOriginalNativeSourceCut(ctx context.Context, v storageOwnerV6NativeBank, outer storageEnrollmentPeakEnvelope, record storageOwnerRecordV2, claim storageOwnerClaimV2) error {
	if c.owner != nil || c.sources != nil || !c.home.exclusive || v.Stage.Transition != storageCompositionEnrollment || outer.Scope.requireCapacityEstimate() != nil {
		return storagebudget.ErrAuthority
	}
	read := func(bank byte, extent uint64, native storageOwnerPersistentObject) (raw []byte, err error) {
		if bank > 1 || extent == 0 || extent > storageEnrollmentPeakEvidenceMax {
			return nil, storagebudget.ErrBounds
		}
		n, err := storageOpenFile(c.original.config, storageEnrollmentNativeName(1+bank), int64(extent))
		if err != nil {
			return nil, err
		}
		defer func() { err = errors.Join(err, n.current(), n.file.Close()) }()
		actual, e := storageOwnerPersistentFile(ctx, n)
		if e != nil || actual != native {
			return nil, errors.Join(errStorageCustody, e)
		}
		raw = make([]byte, int(extent))
		count, e := n.file.ReadAt(raw, 0)
		if e != nil || count != len(raw) {
			return nil, errors.Join(errStorageCustody, e)
		}
		return raw, nil
	}
	var prior *storageEnrollmentPeakEvidence
	var before storageOwnerCatalogV2
	if outer.Snapshot != nil {
		x := outer.Snapshot
		if x.Encoding != 6 || v.Before == nil {
			return errStorageEnrollmentUnresolved
		}
		raw, err := read(x.Bank, x.Extent, x.Evidence)
		if err != nil {
			return err
		}
		e, err := storageEnrollmentPeakDecodeEvidenceWithLineage(raw, &v.Stage.Lineage, record, claim)
		if err != nil || storageEnrollmentPeakCompareSnapshot(outer, e, record, claim) != nil {
			return errors.Join(errStorageCustody, err)
		}
		originals, err := v.Before.restore(x.Before, v.Stage.Lineage, v.Policy, v.Overrides, e.Inner.Catalog, record, claim)
		if err != nil || storageOwnerV6NativeCompareSnapshot(v, raw, originals, record, claim) != nil {
			return errors.Join(errStorageCustody, err)
		}
		before = e.Inner.Catalog
		prior = &e
	} else {
		if outer.Marker == nil {
			return errStorageEnrollmentUnresolved
		}
		m := outer.Marker
		raw, err := read(m.Bank, m.Extent, m.Controls[m.Bank])
		if err != nil {
			return err
		}
		e, err := storageEnrollmentPeakDecodeEvidenceWithLineage(raw, &v.Stage.Lineage, record, claim)
		if err != nil || e.Version != 3 || storageEnrollmentPeakJoinMarker(outer, e, e.Ready) != nil {
			return errors.Join(errStorageCustody, err)
		}
		originals, selected, err := storagePolicyEvidenceNativeBefore(e, record, claim)
		if err != nil || selected != outer.Scope[322] || sha256.Sum256(originals[selected]) != e.Before {
			return errors.Join(errStorageCustody, err)
		}
		found := false
		for _, p := range e.Projections {
			if storageEnrollmentPeakProjectionFullDigest(p) == e.Before {
				if found {
					return errStorageCustody
				}
				before = p.Catalog
				found = true
			}
		}
		if !found {
			return errStorageCustody
		}
	}
	var evidence []byte
	if prior != nil {
		evidence = prior.Raw
	}
	pair, err := storageOwnerV6NativeReadPair(c.startupPhysical[0], c.startupPhysical[1], &before, evidence, record, claim)
	if err != nil {
		return err
	}
	c.startupCores = [2][]byte{pair.Banks[0].Stage.Core, pair.Banks[1].Stage.Core}
	lineage := pair.Banks[pair.Selected].Stage.Lineage
	c.startupLineage = &lineage
	c.ref.cell.mu.Lock()
	c.ref.cell.ownerCatalog = before
	c.ref.cell.enrollmentEndpoint = prior
	c.ref.cell.mu.Unlock()
	moveState := storagePolicyOriginal{Actual: c.startupPhysical, Selected: pair.Selected, Catalog: before, Lineage: lineage, Moves: pair.Banks[pair.Selected].Stage.Moves}
	if prior != nil {
		moveState.Evidence = *prior
	}
	if err = c.selectMoveHistory(ctx, moveState); err != nil {
		return err
	}
	sources, err := c.inspectOriginalSources(ctx, record.Installation, nil)
	if err != nil {
		return err
	}
	c.sources = sources
	if err = sources.buildObjects(ctx); err != nil {
		return err
	}
	return c.resumeOriginalEnrollmentStartup(ctx, outer, c.startupPhysical, record, claim)
}
