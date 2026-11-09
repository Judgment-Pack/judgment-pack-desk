package desk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"sort"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func (p *storageEnrollmentEpoch) initialScopeForPacket(e storageEnrollmentPeakEvidence) (storageEnrollmentPeakScope, error) {
	var s storageEnrollmentPeakScope
	if p == nil || p.self != p || p.qualified == nil || p.original == nil || p.initialInspection == nil || e.Phase != 0 || e.BeforeRevision != p.initialInspection.View.Configuration.Revision {
		return s, storagebudget.ErrAuthority
	}
	cores, selectedCore, err := p.originalCorePair(p.original.Catalogs, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
	if err != nil {
		return s, err
	}
	extent, err := storageEnrollmentInitialExtent(e)
	if err != nil {
		return s, err
	}
	scopeID, err := storagebudget.NewID()
	if err != nil {
		return s, err
	}
	storageEnrollmentPeakPut16(s[:2], storageEnrollmentCapacityEstimateVersion)
	s[2] = storageEnrollmentCapacityEstimatePolicy
	s[3] = 1
	storageEnrollmentPeakPut32(s[4:8], 7)
	copy(s[8:24], scopeID[:])
	copy(s[24:40], e.Bridge[:])
	copy(s[40:56], e.Installation[:])
	copy(s[56:88], e.QBinding[:])
	copy(s[88:120], e.Before[:])
	copy(s[120:152], e.Checkpoint.Checkpoints[e.Checkpoint.Bank][:])
	viewDigest := sha256.Sum256(e.Views[e.Checkpoint.View].Raw)
	copy(s[152:184], viewDigest[:])
	record := p.c.ref.cell.ownerRecord
	copy(s[216:251], storageEnrollmentPeakNativeBytes(record.Config))
	copy(s[251:286], storageEnrollmentPeakNativeBytes(record.Anchor))
	copy(s[286:321], storageEnrollmentPeakNativeBytes(p.original.Project))
	s[322] = 1
	if storageEnrollmentPeakU64(cores[0][16:24]) > storageEnrollmentPeakU64(cores[1][16:24]) {
		s[322] = 0
	}
	if e.Version >= 2 {
		s[322] = byte(selectedCore)
	}
	s[323] = 255
	s[324] = 255
	s[325] = e.Checkpoint.Bank
	beforeExtents := [2]uint64{}
	finalExtents := [2]uint64{extent, extent}
	if p.previousEvidence != nil {
		prior := p.previousEvidence
		if !prior.Scope.capacityEstimate() || prior.Generation > ^uint64(0)-4 {
			return s, storagebudget.ErrBounds
		}
		s[3] = 2
		s[321] = 15
		s[322] = 0
		if e.Version >= 2 {
			s[322] = byte(selectedCore)
		}
		s[323] = prior.Bank
		for i := 0; i < 2; i++ {
			beforeExtents[i] = p.original.Extents[i]
			finalExtents[i] = max(beforeExtents[i], extent)
			if finalExtents[i] != beforeExtents[i] {
				s[3] = 3
			}
		}
		var steps [2][]byte
		for i := range steps {
			t := p.targets[2+i]
			steps[i] = make([]byte, 4096)
			n, e0 := t.named.file.ReadAt(steps[i], 0)
			if e0 != nil || n != 4096 {
				return s, storagebudget.ErrUnknown
			}
		}
		selected, bank, e0 := storageEnrollmentPeakDecodeDispatchPair(steps[0], steps[1], *prior, nil)
		if e0 != nil || selected.Step != 3 {
			return s, storagebudget.ErrBounds
		}
		s[324] = byte(bank)
	}
	for i := 0; i < 2; i++ {
		storageEnrollmentPeakPut64(s[328+i*8:336+i*8], beforeExtents[i])
		storageEnrollmentPeakPut64(s[344+i*8:352+i*8], finalExtents[i])
	}
	growth := finalExtents[0] - beforeExtents[0] + finalExtents[1] - beforeExtents[1]
	files := uint64(0)
	if s.path() == 1 {
		growth += 8192
		files = 4
	}
	storageEnrollmentPeakPut64(s[360:368], growth)
	storageEnrollmentPeakPut64(s[368:376], files)
	storageEnrollmentPeakPut64(s[400:408], 134217728)
	copy(s[408:440], p.qualified.digest[:])
	firstTarget := byte(5)
	if p.previousEvidence != nil {
		firstTarget = 1
	}
	for target := firstTarget; target <= 8; target++ {
		copy(s[1080+35*int(target-1):1080+35*int(target)], storageEnrollmentPeakNativeBytes(p.original.Targets[target-1]))
	}
	storageEnrollmentPeakPut64(s[1616:1624], p.generation)
	storageEnrollmentPeakPut64(s[1624:1632], storageEnrollmentPeakU64(cores[s[322]][16:24]))
	storageEnrollmentPeakPut16(s[1634:1636], 2)
	storageEnrollmentPeakPut32(s[1636:1640], 31)
	plan := storageEnrollmentPeakPlan(s)
	storageEnrollmentPeakPut16(s[1632:1634], uint16(len(plan)))
	var parents [2]storagebudget.CapacityVector
	var attributed [10]uint64
	for _, group := range plan {
		cost, ok := storageEnrollmentPeakEstimate(group)
		if !ok {
			return s, storagebudget.ErrBounds
		}
		parents[group.Parent-1].Bytes += cost.Bytes
		parents[group.Parent-1].Inodes += cost.Inodes
		attributed[group.Target-1] += cost.Bytes
	}
	storageEnrollmentPeakPut64(s[384:392], parents[0].Bytes+parents[1].Bytes)
	storageEnrollmentPeakPut64(s[392:400], parents[0].Inodes+parents[1].Inodes)
	for i := 0; i < 8; i++ {
		d := s[1360+i*32 : 1360+(i+1)*32]
		d[0] = 1
		if i >= 6 {
			d[0] = 2
		}
		d[1] = byte(i + 1)
		before, final := uint64(0), extent
		if i < 4 && s.path() == 1 {
			d[2] = 1
		}
		if i < 2 {
			before = beforeExtents[i]
			final = finalExtents[i]
		}
		if i >= 2 && i < 4 {
			final = 4096
			if s.path() != 1 {
				before = 4096
			}
		}
		if i >= 4 && i < 6 {
			before = 8388608
			final = before
		}
		if i >= 6 {
			before = 4194304
			final = before
		}
		storageEnrollmentPeakPut64(d[4:12], before)
		storageEnrollmentPeakPut64(d[12:20], final)
		storageEnrollmentPeakPut64(d[20:28], attributed[i])
	}
	type union struct {
		id         storagebudget.ID
		generation uint64
		mask       byte
		fs         [2]storagebudget.ID
		peak       storagebudget.CapacityVector
	}
	rows := map[storagebudget.ID]*union{}
	for _, root := range e.Inner.Catalog.Roots {
		if root.Role != 1 && root.Role != 2 {
			continue
		}
		parent := int(root.Role - 1)
		for i := 0; i < int(root.Definition.CapacityCount); i++ {
			ref := root.Definition.CapacityRefs[i]
			row := rows[ref.Constraint]
			if row == nil {
				row = &union{id: ref.Constraint, generation: ref.Generation}
				rows[ref.Constraint] = row
			}
			if row.generation != ref.Generation {
				return s, storagebudget.ErrStale
			}
			if row.mask&(1<<parent) != 0 {
				return s, storagebudget.ErrBounds
			}
			row.mask |= 1 << parent
			row.fs[parent] = root.Definition.Filesystem
			row.peak.Bytes += parents[parent].Bytes
			row.peak.Inodes += parents[parent].Inodes
		}
	}
	if len(rows) == 0 || len(rows) > 8 {
		return s, storagebudget.ErrBounds
	}
	ordered := make([]*union, 0, len(rows))
	for _, row := range rows {
		ordered = append(ordered, row)
	}
	sort.Slice(ordered, func(i, j int) bool { return bytes.Compare(ordered[i].id[:], ordered[j].id[:]) < 0 })
	s[326] = byte(len(ordered))
	for i, row := range ordered {
		raw := s[440+i*80 : 440+(i+1)*80]
		copy(raw[:16], row.id[:])
		storageEnrollmentPeakPut64(raw[16:24], row.generation)
		raw[24] = row.mask
		storageEnrollmentPeakPut64(raw[32:40], row.peak.Bytes)
		storageEnrollmentPeakPut64(raw[40:48], row.peak.Inodes)
		copy(raw[48:64], row.fs[0][:])
		copy(raw[64:80], row.fs[1][:])
	}
	if p.previousEvidence == nil {
		if e.Version >= 2 && bytes.Equal(cores[0], cores[1]) {
			peer := storageCompositionDuplicateCorePeer(cores[s[322]], *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
			copy(s[1640:1704], peer[1:65])
			s[1704] = 1
		} else if err := storageEnrollmentPeakRetainOriginalPeer(&s, cores, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim); err != nil {
			return s, err
		}
	} else {
		copy(s[1640:1705], p.previousEvidence.Scope[1640:1705])
	}
	digest := s.policyDigest()
	copy(s[184:216], digest[:])
	return storageEnrollmentPeakDecodeScopeVariant(s[:], e.Version >= 2)
}

func (p *storageEnrollmentEpoch) planInitialProjectEnrollment(ctx context.Context) ([]byte, storageEnrollmentPeakPrefix, error) {
	var prefix storageEnrollmentPeakPrefix
	catalog, source, err := p.planOriginalProjectScope(ctx)
	if err != nil {
		return nil, prefix, err
	}
	return p.planInitialEnrollment(ctx, catalog, source)
}

func (p *storageEnrollmentEpoch) planInitialEnrollment(ctx context.Context, catalog storageOwnerCatalogV2, source storageEnrollmentPeakSource) ([]byte, storageEnrollmentPeakPrefix, error) {
	var prefix storageEnrollmentPeakPrefix
	if p == nil || p.previousEvidence != nil {
		return nil, prefix, storagebudget.ErrAuthority
	}
	model, err := p.initialPacketModel(ctx, catalog, source)
	if err != nil {
		return nil, prefix, err
	}
	scope, err := p.initialScopeForPacket(model)
	if err != nil {
		return nil, prefix, err
	}
	model.Scope = scope
	model.Extent = scope.final(0)
	model.Board.Groups = storageEnrollmentPeakPlan(scope)
	for i := range model.Board.Groups {
		g := &model.Board.Groups[i]
		g.Generation = p.generation
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
	prefix.Catalog, err = json.Marshal(catalog)
	if err != nil {
		return nil, prefix, err
	}
	if source.Default != nil || source.ActionPageDigest != (storagebudget.Digest{}) {
		prefix.SourceKind = 8
		if source.Default != nil {
			prefix.SourceKind = 9
		}
		if source.Default != nil && (source.Default.Workflow == 2 || source.Default.Workflow == 4) {
			prefix.SourceKind = 10
		}
		if source.Default != nil && source.Default.Workflow == 6 {
			prefix.SourceKind = 11
		}
		prefix.Source, err = storageEnrollmentPeakEncodeSource(source)
		if err != nil {
			return nil, prefix, err
		}
	}
	prefix.Projection = append([]byte{}, model.Sections[4].Raw...)
	prefix.Tickets = append([]byte{}, model.Sections[2].Raw...)
	prefix.Views = append([]byte{}, model.Sections[3].Raw...)
	prefix.Checkpoint = append([]byte{}, model.Sections[6].Raw...)
	prefix.TicketCount = uint32(len(model.Tickets))
	prefix.ViewCount = 1
	snapshot := storageEnrollmentPeakSnapshot{Kind: 0, Full: model.Before, Bank: 255, Before: append([]byte{}, p.original.Catalogs[scope[322]]...)}
	if model.Version == 2 {
		cores, _, e0 := p.originalCorePair(p.original.Catalogs, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
		if e0 != nil {
			return nil, prefix, e0
		}
		snapshot.Encoding, snapshot.Before, snapshot.Physical = 4, append([]byte{}, cores[scope[322]]...), model.Projections[0].Physical
	}
	// The Fence commits the exact original V3 payload in its own domain;
	// model.BeforeInner separately commits the decoded catalog JSON.
	n := int(storageEnrollmentPeakU32(snapshot.Before[24:28]))
	snapshot.Inner = storageEnrollmentPeakHash("desk-enrollment-original-v3-inner-peak-v1", snapshot.Before[64:64+n])
	fence := storageEnrollmentPeakEnvelope{Kind: 0, Sequence: storageEnrollmentPeakU64(scope[1624:1632]) + 1, Scope: scope, Snapshot: &snapshot}
	raw, err := storageEnrollmentPeakEncodeEnvelope(fence, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
	if err != nil {
		return nil, prefix, err
	}
	p.initialPacket = &model
	return raw, prefix, p.held(ctx)
}

func (p *storageEnrollmentEpoch) initialStagedPacket(ctx context.Context) ([]byte, error) {
	if p == nil || p.self != p || p.scope == nil || p.scope[321] != 15 || p.initialPacket == nil {
		return nil, storagebudget.ErrAuthority
	}
	if err := p.nativePermit(ctx); err != nil {
		return nil, err
	}
	e := *p.initialPacket
	e.Scope = *p.scope
	e.Extent = p.scope.final(0)
	for i := range e.Controls {
		e.Controls[i] = p.targets[i].native
	}
	e.Inner.Controls = e.Controls
	inner, err := storageEnrollmentPeakEncodeInner(e.Inner, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
	if err != nil {
		return nil, err
	}
	e.Inner.Raw = inner
	e.Sections[1] = storageEnrollmentPeakSection{Count: 1, Raw: inner}
	groups := storageEnrollmentPeakPlan(*p.scope)
	if len(groups) != len(e.Board.Groups) {
		return nil, storagebudget.ErrBounds
	}
	e.Board.Groups = append([]storageEnrollmentPeakGroup{}, e.Board.Groups...)
	for i, group := range groups {
		if group.ID != e.Board.Groups[i].ID {
			return nil, storagebudget.ErrBounds
		}
		e.Board.Groups[i].Native = group.Native
	}
	board, err := storageEnrollmentPeakWriteBoard(e.Board, *p.scope, 0, e.Digests, e.Inner.Catalog, len(e.Views), len(e.Projections), len(e.Witnesses))
	if err != nil {
		return nil, err
	}
	e.Sections[8] = storageEnrollmentPeakSection{Count: 1, Raw: board}
	raw, err := storageEnrollmentPeakEncodeEvidenceOperands(e, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim, &p.operands)
	if err != nil {
		return nil, err
	}
	return raw, p.nativePermit(ctx)
}

func (c *storageBudgetConstruction) enrollOriginalProjectParent(ctx context.Context) error {
	return c.enrollOriginalProjectParentFor(ctx, c.server)
}
func (c *storageBudgetConstruction) enrollOriginalProjectParentFor(ctx context.Context, requester *Server) (err error) {
	p, err := c.beginEnrollmentEpoch(ctx)
	if err != nil {
		return err
	}
	defer func() { err = p.finish(err) }()
	c.ref.cell.mu.Lock()
	existing := c.ref.cell.enrollmentEndpoint != nil
	c.ref.cell.mu.Unlock()
	if err = p.selectOriginalRequester(ctx, requester); err != nil {
		return err
	}
	if err = p.acquire(ctx); err != nil {
		return err
	}
	if existing {
		err = p.captureExistingOriginal(ctx)
	} else {
		err = p.captureInitialOriginal(ctx)
	}
	if err != nil {
		return err
	}
	if err = p.qualifyOriginalNative(ctx); err != nil {
		return err
	}
	return p.completeOriginalProjectParent(ctx, existing)
}

// This is a separate explicit preparation operation, before the caller obtains
// the policy snapshot it will later submit. It never runs from New or GET and
// never rebases an update. The requester is the actual admitted project/Desk;
// genuine founding enrollment does not create a project or private identity.
func (c *storageBudgetConstruction) prepareOriginalPolicyFor(ctx context.Context, requester *Server) (err error) {
	p, err := c.beginEnrollmentEpoch(ctx)
	if err != nil {
		return err
	}
	defer func() { err = p.finish(err) }()
	ctx = p.ctx
	if err = p.selectOriginalRequester(ctx, requester); err != nil {
		return err
	}
	if err = p.acquire(ctx); err != nil {
		return err
	}
	c.ref.cell.mu.Lock()
	existing := c.ref.cell.enrollmentEndpoint != nil
	c.ref.cell.mu.Unlock()
	if existing {
		state, e := c.owner.readOriginalPolicy(ctx)
		if e != nil {
			return e
		}
		if state.Packed != nil && state.Packed.Phase == "prepared" {
			return errStorageEnrollmentUnresolved
		}
		q, e := c.ref.cell.q.InspectAction(ctx)
		if e != nil {
			return e
		}
		if e = storagePolicyCompletedQ(state, q); e != nil {
			return e
		}
		if e = p.qualifyOriginalNative(ctx); e != nil {
			return e
		}
		return p.held(ctx)
	}
	if err = p.captureInitialOriginal(ctx); err != nil {
		return err
	}
	if err = p.qualifyOriginalNative(ctx); err != nil {
		return err
	}
	return p.completeOriginalProjectParent(ctx, false)
}

// The original enrollment and explicit policy preparation use the same actual
// finite constructor once, under their one registered original epoch.
func (p *storageEnrollmentEpoch) completeOriginalProjectParent(ctx context.Context, existing bool) error {
	var err error
	var fence []byte
	var prefix storageEnrollmentPeakPrefix
	if existing {
		catalog, source, e := p.planOriginalProjectScope(ctx)
		if e != nil {
			return e
		}
		fence, prefix, err = p.planSuccessorEnrollment(ctx, catalog, source)
	} else {
		fence, prefix, err = p.planInitialProjectEnrollment(ctx)
	}
	if err != nil {
		return err
	}
	if err = p.admitInitialEnrollment(ctx, fence, prefix); err != nil {
		return err
	}
	if err = p.persistInitialEnrollmentFences(ctx); err != nil {
		return err
	}
	if existing {
		return p.runSuccessorEnrollmentControls(ctx)
	}
	if err = p.createInitialEnrollmentControls(ctx); err != nil {
		return err
	}
	staged, err := p.initialStagedPacket(ctx)
	if err != nil {
		return err
	}
	return p.runInitialEnrollmentControls(ctx, staged)
}

// Original callers derive the intended closed append from retained source or
// creator custody. The catalog/source arguments here remain untrusted data;
// admission independently joins complete originals and actual source bindings.
func (p *storageEnrollmentEpoch) planSuccessorEnrollment(ctx context.Context, catalog storageOwnerCatalogV2, source storageEnrollmentPeakSource) ([]byte, storageEnrollmentPeakPrefix, error) {
	var prefix storageEnrollmentPeakPrefix
	if p == nil || p.self != p || p.previousEvidence == nil || p.original == nil || p.scope != nil {
		return nil, prefix, storagebudget.ErrAuthority
	}
	model, err := p.initialPacketModel(ctx, catalog, source)
	if err != nil {
		return nil, prefix, err
	}
	scope, err := p.initialScopeForPacket(model)
	if err != nil {
		return nil, prefix, err
	}
	model, err = p.bindSuccessorModel(model, scope)
	if err != nil {
		return nil, prefix, err
	}
	prefix, err = storageEnrollmentSuccessorPrefix(model)
	if err != nil {
		return nil, prefix, err
	}
	prior := p.previousEvidence
	snapshot := storageEnrollmentPeakSnapshot{Kind: 1, Full: model.Before, Inner: storageEnrollmentPeakInnerDigest(prior.Inner.Raw), Evidence: prior.Controls[prior.Bank], Bank: prior.Bank, Extent: prior.Extent, EvidenceDigest: sha256.Sum256(prior.Raw), Q3Revision: prior.AfterRevision, Q3Epoch: prior.AfterEpoch, Before: append([]byte{}, p.original.Catalogs[scope[322]]...)}
	if model.Version == 2 {
		cores, _, e0 := p.originalCorePair(p.original.Catalogs, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
		if e0 != nil {
			return nil, prefix, e0
		}
		snapshot.Encoding, snapshot.Before, snapshot.Physical = 4, append([]byte{}, cores[scope[322]]...), model.Projections[0].Physical
	}
	// Every successor uses its closed deterministic constructor and exact source.
	if storageEnrollmentSuccessorFamily(prefix.SourceKind, source) != 0 {
		prefix.FreshOrigin = model.FreshOrigin
		prefix.OriginalStep3 = make([]byte, 4096)
		step := p.targets[2+int(scope[324])].named
		if err = step.current(); err != nil {
			return nil, prefix, err
		}
		n, e := step.file.ReadAt(prefix.OriginalStep3, 0)
		if e != nil || n != 4096 {
			return nil, prefix, storagebudget.ErrUnknown
		}
		old, e := storageEnrollmentPeakDecodeDispatch(prefix.OriginalStep3, *prior)
		if e != nil || old.Step != 3 {
			return nil, prefix, storagebudget.ErrBounds
		}
		flags, e := storageEnrollmentSuccessorOriginFlags(*p.original, *prior, model.FreshOrigin, *p.c.ref.cell.ownerRecord)
		if e != nil {
			return nil, prefix, e
		}
		inputs, e := storageEnrollmentSuccessorInputs(scope, *p.original, prior.Raw, p.c.ref.cell.ownerRecord.QBinding[:], prefix.OriginalStep3)
		if model.Version == 3 {
			inputs, e = storageEnrollmentSuccessorNativeInputs(scope, *p.original, prior.Raw, p.c.ref.cell.ownerRecord.QBinding[:], prefix.OriginalStep3, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
		} else if model.Version == 2 {
			inputs, e = storageEnrollmentSuccessorCompositionInputs(scope, *p.original, prior.Raw, p.c.ref.cell.ownerRecord.QBinding[:], prefix.OriginalStep3, snapshot.Physical, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
		}
		if e != nil {
			return nil, prefix, e
		}
		snapshot.Intent = &storageEnrollmentSuccessorIntent{Family: storageEnrollmentSuccessorFamily(prefix.SourceKind, source), SourceKind: prefix.SourceKind, OriginFlags: flags, Origin: model.FreshOrigin, Catalog: sha256.Sum256(prefix.Catalog), Inputs: inputs, Source: append([]byte{}, prefix.Source...)}
	}
	if model.Version == 3 {
		snapshot, _, err = storageOwnerV6NativeSnapshot(p.original.Catalogs, scope[322], prior.Raw, prior.Inner.Catalog, scope, snapshot.Intent, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
		if err != nil {
			return nil, prefix, err
		}
	}
	fence := storageEnrollmentPeakEnvelope{Kind: 0, BeforeCodec: 1, Sequence: storageEnrollmentPeakU64(scope[1624:1632]) + 1, Scope: scope, Snapshot: &snapshot}
	raw, err := p.encodeEnrollmentEnvelope(fence)
	if err != nil {
		return nil, prefix, err
	}
	p.initialPacket = &model
	return raw, prefix, p.held(ctx)
}

// Shared closed deterministic annotations. Stored diagnostic generation is
// distinct from the current invocation's live original permit generation.
func (p *storageEnrollmentEpoch) bindSuccessorModel(model storageEnrollmentPeakEvidence, scope storageEnrollmentPeakScope) (storageEnrollmentPeakEvidence, error) {
	if p.previousEvidence == nil || scope.path() == 1 || model.Bridge != (storagebudget.ID([16]byte(scope[24:40]))) {
		return model, storagebudget.ErrBounds
	}
	extent, err := storageEnrollmentInitialExtent(model)
	if err != nil || extent > scope.final(0) || extent > scope.final(1) {
		return model, storagebudget.ErrBounds
	}
	model.Scope = scope
	model.Bank = storageEnrollmentPeakEvidenceBank(scope, 0)
	model.Extent = scope.final(model.Bank)
	for i := range model.Controls {
		model.Controls[i] = scope.target(byte(i + 1))
	}
	model.Inner.Controls = model.Controls
	inner, err := storageEnrollmentPeakEncodeInner(model.Inner, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
	if err != nil {
		return model, err
	}
	model.Inner.Raw = inner
	model.Sections[1] = storageEnrollmentPeakSection{Count: 1, Raw: inner}
	model.Board.Groups = storageEnrollmentPeakPlan(scope)
	for i := range model.Board.Groups {
		g := &model.Board.Groups[i]
		g.Generation = storageEnrollmentPeakU64(scope[1616:1624])
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
	if !storageEnrollmentPeakRetainsOrigins(model, *p.previousEvidence) {
		return model, storagebudget.ErrBounds
	}
	return model, nil
}

func storageEnrollmentSuccessorPrefix(model storageEnrollmentPeakEvidence) (storageEnrollmentPeakPrefix, error) {
	var prefix storageEnrollmentPeakPrefix
	var err error
	prefix.Catalog, err = json.Marshal(model.Inner.Catalog)
	if err != nil {
		return prefix, err
	}
	source := model.Board.Source
	if model.Board.SourceKind >= 7 && model.Board.SourceKind <= 11 {
		prefix.SourceKind = model.Board.SourceKind
		prefix.Source, err = storageEnrollmentPeakEncodeSource(source)
		if err != nil {
			return prefix, err
		}
	}
	for _, projection := range model.Projections {
		if storageEnrollmentPeakProjectionFullDigest(projection) == model.Before {
			if len(prefix.Projection) != 0 {
				return prefix, storagebudget.ErrBounds
			}
			prefix.Projection = append([]byte{}, projection.Raw...)
		}
	}
	if len(prefix.Projection) == 0 {
		return prefix, storagebudget.ErrBounds
	}
	prefix.Tickets = append([]byte{}, model.Sections[2].Raw...)
	prefix.Views = append([]byte{}, model.Sections[3].Raw...)
	prefix.Checkpoint = append([]byte{}, model.Sections[6].Raw...)
	prefix.TicketCount = uint32(len(model.Tickets))
	prefix.ViewCount = uint32(len(model.Views))
	return prefix, nil
}

func (p *storageEnrollmentEpoch) successorStagedPacket(ctx context.Context) ([]byte, error) {
	if p == nil || p.scope == nil || p.scope.path() != 3 || p.initialPacket == nil || p.previousEvidence == nil || !p.fenced {
		return nil, storagebudget.ErrAuthority
	}
	e := *p.initialPacket
	e.Scope = *p.scope
	e.Bank = 1 - p.scope[323]
	e.Extent = p.scope.final(e.Bank)
	board, err := storageEnrollmentPeakWriteBoard(e.Board, e.Scope, 0, e.Digests, e.Inner.Catalog, len(e.Views), len(e.Projections), len(e.Witnesses))
	if err != nil {
		return nil, err
	}
	e.Sections[8] = storageEnrollmentPeakSection{Count: 1, Raw: board}
	return storageEnrollmentPeakEncodeEvidenceOperands(e, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim, &p.operands)
}
