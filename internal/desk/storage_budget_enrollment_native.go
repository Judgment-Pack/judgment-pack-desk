package desk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"reflect"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

type storageEnrollmentNativeTarget struct {
	named  storageNamedFile
	native storageOwnerPersistentObject
	before uint64
	final  uint64
	// The retained witness owns actual original custody while each publication
	// independently opens and closes its write/readback descriptions.
	created bool
}

func storageEnrollmentNativeWriteResult(n, wanted int, err error) error {
	if wanted <= 0 || wanted > 65536 || n < 0 || n > wanted {
		return errors.Join(storagebudget.ErrBounds, err)
	}
	if n != wanted {
		return errors.Join(io.ErrShortWrite, err)
	}
	return err
}

// Fixed original controls only: ticket bank1MiB, host2MiB, checkpoint4MiB,
// catalog8MiB and manifest32MiB. No arbitrary caller-selected extent.
func storageEnrollmentDenseCount(length uint64) int {
	switch length {
	case 1048576, 2097152, 4194304, 8388608:
		return 2048
	case 33554432:
		return 8192
	}
	return 0
}

func storageEnrollmentNativeDenseRecord(raw []byte, length uint64) error {
	count := storageEnrollmentDenseCount(length)
	if count == 0 || len(raw) != 32+56*count || binary.LittleEndian.Uint64(raw[:8]) != 0 || binary.LittleEndian.Uint64(raw[8:16]) != length || binary.LittleEndian.Uint32(raw[16:20]) != 0 || binary.LittleEndian.Uint32(raw[24:28]) != uint32(count) || !storageEnrollmentPeakZero(raw[28:32]) {
		return storagebudget.ErrBounds
	}
	n := int(binary.LittleEndian.Uint32(raw[20:24]))
	if n == 0 || n > count {
		return storagebudget.ErrUnsupported
	}
	end := uint64(0)
	for i := 0; i < n; i++ {
		v := raw[32+56*i : 32+56*(i+1)]
		logical, physical, size := binary.LittleEndian.Uint64(v[:8]), binary.LittleEndian.Uint64(v[8:16]), binary.LittleEndian.Uint64(v[16:24])
		flags := binary.LittleEndian.Uint32(v[40:44])
		wantFlags := uint32(0)
		if i == n-1 {
			wantFlags = 1
		}
		if logical != end || size == 0 || physical == 0 || logical%4096 != 0 || physical%4096 != 0 || size%4096 != 0 || size > length-end || physical > ^uint64(0)-size || flags != wantFlags || !storageEnrollmentPeakZero(v[24:40]) || !storageEnrollmentPeakZero(v[44:56]) {
			return storagebudget.ErrUnsupported
		}
		end += size
	}
	if end != length || !storageEnrollmentPeakZero(raw[32+56*n:]) {
		return storagebudget.ErrUnsupported
	}
	return nil
}

func (p *storageEnrollmentEpoch) openEnrollmentCheckpoint(ctx context.Context, name string) (storageNamedFile, error) {
	if name != "checkpoint.a" && name != "checkpoint.b" {
		return storageNamedFile{}, storagebudget.ErrAuthority
	}
	if err := p.held(ctx); err != nil {
		return storageNamedFile{}, err
	}
	for _, original := range p.c.original.files {
		if original.root != p.c.anchor || original.name != name {
			continue
		}
		if original.size != storagebudget.CheckpointBytes {
			return storageNamedFile{}, errStorageCustody
		}
		if err := original.current(); err != nil {
			return storageNamedFile{}, err
		}
		n, err := storageOpenFile(p.c.anchor, name, storagebudget.CheckpointBytes)
		if err != nil {
			return storageNamedFile{}, err
		}
		if !os.SameFile(original.identity, n.identity) {
			return storageNamedFile{}, errors.Join(errStorageCustody, n.file.Close())
		}
		return n, nil
	}
	return storageNamedFile{}, errStorageCustody
}

func storageEnrollmentNativeName(target byte) string {
	switch target {
	case 1:
		return "storage-enrollment.a"
	case 2:
		return "storage-enrollment.b"
	case 3:
		return "storage-enrollment-step.a"
	case 4:
		return "storage-enrollment-step.b"
	case 5:
		return "storage-owner.a"
	case 6:
		return "storage-owner.b"
	case 7:
		return "checkpoint.a"
	case 8:
		return "checkpoint.b"
	}
	return ""
}

func (p *storageEnrollmentEpoch) targetParent(target byte) *os.Root {
	if target >= 1 && target <= 6 {
		return p.c.original.config
	}
	if target == 7 || target == 8 {
		return p.c.anchor
	}
	return nil
}

func (p *storageEnrollmentEpoch) nativePermit(ctx context.Context) error {
	if p != nil && p.self == p && p.policy != nil {
		return p.policy.nativeCurrent(ctx)
	}
	if p == nil || p.self != p || p.scope == nil || p.qualified == nil || p.reservation.phase != p || p.reservation.scope != *p.scope && !storageEnrollmentPeakCaptureMatches(p.reservation.scope, *p.scope) {
		return storagebudget.ErrAuthority
	}
	if err := p.scope.requireCapacityEstimate(); err != nil {
		return err
	}
	if !bytes.Equal(p.scope[408:440], p.qualified.digest[:]) {
		return storagebudget.ErrAuthority
	}
	if err := p.qualified.current(ctx, p); err != nil {
		return err
	}
	// qualificationFacts brackets its fresh system/parent observations with
	// held-current frames. Only pure applicability comparisons follow its final
	// frame; check cancellation/admission once more without repeating all native
	// witnesses a third time. Each later primitive still calls nativePermit.
	return p.permit(ctx)
}

// Consume a finite original schedule segment before its first effect. This is
// an invocation-local cursor, not a persistent allowance or recovered permit.
// A failed segment is never entered again in this invocation.
func (p *storageEnrollmentEpoch) takeEnrollmentGroups(ids ...uint16) error {
	if p == nil || p.self != p || p.scope == nil || len(ids) == 0 {
		return storagebudget.ErrAuthority
	}
	if p.identityLaunch != nil {
		if err := p.checkIdentityLaunch(p.ctx, p.identityLaunch); err != nil {
			return err
		}
	}
	completion := p.originalTransferCompletion()
	if completion != nil {
		if err := completion.current(completion.operation.completionContext); err != nil {
			return err
		}
	}
	plan := p.enrollmentPassPlan()
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.running || p.revoked && completion == nil || p.finishing || p.entered || p.err != nil || p.planCursor < 0 || p.planCursor > len(plan)-len(ids) {
		return storagebudget.ErrAuthority
	}
	for i, id := range ids {
		if plan[p.planCursor+i].ID != id {
			return storagebudget.ErrAuthority
		}
	}
	p.planCursor += len(ids)
	return nil
}

func (p *storageEnrollmentEpoch) enterNative(ctx context.Context) error {
	if p == nil || !p.sourceHoldsComplete {
		return storagebudget.ErrAuthority
	}
	if err := p.nativePermit(ctx); err != nil {
		return err
	}
	completion := p.originalTransferCompletion()
	if completion != nil {
		if err := completion.operation.enterOriginalTransferCall(ctx, completion); err != nil {
			return err
		}
		p.mu.Lock()
		if !p.running || p.entered || p.finishing || p.err != nil || ctx.Err() != nil {
			p.mu.Unlock()
			return completion.operation.leaveOriginalTransferCall(storagebudget.ErrAuthority)
		}
		p.entered = true
		p.transferNative = completion
		p.mu.Unlock()
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.running || p.revoked || p.entered || p.finishing || p.err != nil || p.ctx.Err() != nil || ctx.Err() != nil {
		return storagebudget.ErrAuthority
	}
	p.entered = true
	return nil
}

func (p *storageEnrollmentEpoch) leaveNative(err error) error {
	p.mu.Lock()
	if !p.entered {
		p.mu.Unlock()
		return errors.Join(err, storagebudget.ErrAuthority)
	}
	completion := p.transferNative
	p.transferNative = nil
	p.entered = false
	p.err = errors.Join(p.err, err)
	if err != nil && p.identityLaunch != nil {
		p.identityLaunch.failed = errors.Join(p.identityLaunch.failed, err)
	}
	result := p.err
	p.mu.Unlock()
	if completion != nil {
		return completion.operation.leaveOriginalTransferCall(result)
	}
	return result
}

func (p *storageEnrollmentEpoch) targetCurrent(ctx context.Context, target byte, exact bool) error {
	if p == nil || p.self != p || target < 1 || target > 8 || p.scope == nil {
		return storagebudget.ErrAuthority
	}
	t := &p.targets[target-1]
	if t.named.file == nil || t.native != p.scope.target(target) {
		return errStorageCustody
	}
	info, err := t.named.file.Stat()
	if err != nil || !storagePrivate(info, false) || info.Size() < 0 {
		return errors.Join(errStorageCustody, err)
	}
	size := uint64(info.Size())
	if size < t.before || size > t.final || exact && size != t.final {
		return errStorageCustody
	}
	n := t.named
	n.size = info.Size()
	if err = n.current(); err != nil {
		return err
	}
	native, err := storageOwnerPersistentFile(ctx, n)
	if err != nil || native != t.native {
		return errors.Join(errStorageCustody, err)
	}
	t.named.size = info.Size()
	return nil
}

// Prefix groups have no data writes or proof-ledger side effects. A failure
// stops this invocation with the full peak reservation and predecessor retained.
func (p *storageEnrollmentEpoch) persistEnrollmentTarget(ctx context.Context, target byte) (err error) {
	if target < 1 || target > 10 {
		return storagebudget.ErrBounds
	}
	if err = p.takeEnrollmentGroups(uint16(69 + target)); err != nil {
		return err
	}
	if err = p.enterNative(ctx); err != nil {
		return err
	}
	defer func() { err = p.leaveNative(err) }()
	if target >= 9 {
		root := p.c.original.config
		if target == 10 {
			root = p.c.anchor
		}
		f, e := root.Open(".")
		if e != nil {
			return e
		}
		defer func() { err = errors.Join(err, f.Close()) }()
		got, e := storageAdvisoryPersistentHeld(ctx, f)
		if e != nil || got != p.scope.target(target) {
			return errors.Join(errStorageCustody, e)
		}
		if e = p.boundary("prefix-directory-sync"); e != nil {
			return e
		}
		if e = p.permit(ctx); e != nil {
			return e
		}
		return storageEnrollmentNativeSync(ctx, f)
	}
	if err = p.targetCurrent(ctx, target, false); err != nil {
		return err
	}
	t := &p.targets[target-1]
	n, err := storageOpenFile(t.named.root, t.named.name, t.named.size)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, n.file.Close()) }()
	if !os.SameFile(n.identity, t.named.identity) {
		return errStorageCustody
	}
	if err = p.boundary("prefix-file-sync"); err != nil {
		return err
	}
	if err = p.permit(ctx); err != nil {
		return err
	}
	return storageEnrollmentNativeSync(ctx, n.file)
}

// Publication accepts only an entire fixed original target image. Schedule
// selection/phase joins remain the caller's typed enrollment state machine;
// this leaf derives all names, offsets and extents from its retained table.
// The original witness is retained across the independent write/readback Close.
func (p *storageEnrollmentEpoch) publishEnrollmentControl(ctx context.Context, group uint16, raw []byte) (err error) {
	if p == nil || p.self != p || p.scope == nil {
		return storagebudget.ErrAuthority
	}
	plan := storageEnrollmentPeakPlan(*p.scope)
	var selected storageEnrollmentPeakGroup
	for _, g := range plan {
		if g.ID == group {
			selected = g
			break
		}
	}
	target := selected.Target
	if target < 1 || target > 6 || selected.Bytes == 0 || uint64(len(raw)) != p.targets[target-1].final {
		return storagebudget.ErrBounds
	}
	if err = p.validateEnrollmentPublication(group, raw); err != nil {
		return err
	}
	coreRaw := raw
	if target == 5 || target == 6 {
		raw, err = p.enrollmentPhysicalPublication(ctx, group, raw)
		if err != nil {
			return err
		}
	}
	ids := []uint16{group}
	if group == 1 || group == 3 || group == 5 || group == 7 {
		if uint64(len(raw)) > storageEnrollmentPeakChunk {
			ids = append(ids, group+1)
		}
	}
	ids = append(ids, selected.Completion)
	if err = p.takeEnrollmentGroups(ids...); err != nil {
		return err
	}
	if err = p.enterNative(ctx); err != nil {
		return err
	}
	defer func() { err = p.leaveNative(err) }()
	if err = p.targetCurrent(ctx, target, false); err != nil {
		return err
	}
	t := &p.targets[target-1]
	n, err := storageOpenFile(t.named.root, t.named.name, t.named.size)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			err = errors.Join(err, n.file.Close())
		}
	}()
	if !os.SameFile(n.identity, t.named.identity) {
		return errStorageCustody
	}
	for offset := 0; offset < len(raw); offset += 65536 {
		end := min(offset+65536, len(raw))
		if err = p.boundary("control-write"); err != nil {
			return err
		}
		if err = p.permit(ctx); err != nil {
			return err
		}
		if err = storageEnrollmentNativeWrite(ctx, n.file, raw[offset:end], int64(offset)); err != nil {
			return err
		}
		if p.scope.path() != 1 {
			point := ""
			switch group {
			case 1:
				point = "successor-stage-a-chunk"
			case 3:
				point = "successor-stage-b-chunk"
			case 5:
				point = "successor-armed-chunk"
			case 16:
				point = "successor-entered-write"
			}
			if point != "" {
				if err = p.successorBoundary(point); err != nil {
					return err
				}
			}
		}
	}
	if err = p.boundary("control-sync"); err != nil {
		return err
	}
	if err = p.permit(ctx); err != nil {
		return err
	}
	if err = storageEnrollmentNativeSync(ctx, n.file); err != nil {
		return err
	}
	closed = true
	err = n.file.Close()
	err = errors.Join(err, p.boundary("control-close"))
	if err != nil {
		return err
	}
	parent, err := n.root.Open(".")
	if err != nil {
		return err
	}
	parentNative, e := storageAdvisoryPersistentHeld(ctx, parent)
	parentTarget := byte(9)
	if target >= 7 {
		parentTarget = 10
	}
	if e == nil && parentNative != p.scope.target(parentTarget) {
		e = errStorageCustody
	}
	if e == nil {
		e = p.boundary("control-parent-sync")
	}
	if e == nil {
		e = p.permit(ctx)
	}
	if e == nil {
		e = storageEnrollmentNativeSync(ctx, parent)
	}
	err = errors.Join(e, parent.Close(), p.boundary("control-parent-close"))
	if err != nil {
		return err
	}
	if err = p.targetCurrent(ctx, target, true); err != nil {
		return err
	}
	read, err := storageOpenFile(t.named.root, t.named.name, int64(t.final))
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, read.file.Close()) }()
	if !os.SameFile(read.identity, t.named.identity) {
		return errStorageCustody
	}
	if err = p.boundary("control-readback"); err != nil {
		return err
	}
	buf := make([]byte, 65536)
	for offset := 0; offset < len(raw); offset += len(buf) {
		if err = p.permit(ctx); err != nil {
			return err
		}
		want := min(len(buf), len(raw)-offset)
		n, e := read.file.ReadAt(buf[:want], int64(offset))
		if e != nil || n != want || !bytes.Equal(buf[:want], raw[offset:offset+want]) {
			return errors.Join(errStorageEnrollmentUnresolved, e)
		}
	}
	if err = p.targetCurrent(ctx, target, true); err != nil {
		return err
	}
	if group == 1 && p.scope.path() == 3 {
		staged, e := p.decodeEnrollmentEvidence(coreRaw)
		if e != nil {
			return e
		}
		p.anchoredStaged = &staged
	}
	if group == 68 {
		reference, e := p.enrollmentEnvelope(coreRaw, true)
		if e != nil {
			return e
		}
		p.anchoredReference = &reference
	}
	if group == 7 {
		completed, e := p.decodeEnrollmentEvidence(coreRaw)
		if e != nil {
			return e
		}
		p.completedEvidence = &completed
	}
	return nil
}

// Structural/source comparisons are necessary but never replace live original
// holds, installed qualification, full escrow or exact Q/dispatch state joins.
// The writer cannot publish an arbitrary frame or an image for another Scope.
func (p *storageEnrollmentEpoch) validateEnrollmentPublication(group uint16, raw []byte) error {
	if p.scope == nil || p.c.ref.cell.ownerRecord == nil {
		return storagebudget.ErrAuthority
	}
	record, claim := *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim
	switch group {
	case 1, 3, 5, 7:
		e, err := p.operands.evidence(raw, p.compositionLineage(), record, claim)
		if err != nil || e.Scope != *p.scope || p.candidateObserver == nil || !reflect.DeepEqual(e.Inner.Catalog, p.candidateObserver.catalog) {
			return storagebudget.ErrBounds
		}
		phase := byte(0)
		if group == 5 {
			phase = 1
		}
		if group == 7 {
			phase = 2
		}
		if e.Phase != phase {
			return storagebudget.ErrBounds
		}
		if p.scope.path() != 1 && group != 7 {
			if p.previousEvidence != nil {
				previous := p.previousEvidence
				if previous.Phase != 2 || sha256.Sum256(previous.Raw) != e.Board.Prior.Evidence || !storageEnrollmentPeakRetainsOrigins(e, *previous) || !storageEnrollmentPeakOriginalPeerRetained(e.Scope, previous.Scope) || !bytes.Equal(e.OriginalV3, previous.OriginalV3) {
					return storagebudget.ErrBounds
				}
			} else {
				if !p.startup || p.anchoredStaged == nil || p.anchoredReference == nil {
					return storagebudget.ErrAuthority
				}
				if group == 3 {
					expected := *p.anchoredStaged
					expected.Bank = p.scope[323]
					expected.Extent = p.scope.final(expected.Bank)
					expected.Generation++
					expected.Previous = sha256.Sum256(p.anchoredStaged.Raw)
					encoded, err := storageEnrollmentPeakEncodeEvidenceOperands(expected, record, claim, &p.operands)
					if err != nil || !bytes.Equal(encoded, e.Raw) {
						return storagebudget.ErrBounds
					}
				} else if group != 5 || !storageEnrollmentPeakArmedJoinsReferencedStage(e, *p.anchoredStaged, *p.anchoredReference, record, claim) {
					return storagebudget.ErrBounds
				}
			}
		}
		if group == 5 && (p.evidence == nil || !bytes.Equal(e.Raw, p.evidence.Raw)) {
			return storagebudget.ErrAuthority
		}
		if group == 7 && (p.evidence == nil || !storageEnrollmentPeakJoinArmed(e, *p.evidence)) {
			return storagebudget.ErrAuthority
		}
	case 9, 10, 60, 61, 66, 67, 68, 11, 12, 13, 14, 15:
		e, err := p.enrollmentEnvelope(raw, true)
		if err != nil || e.Scope != *p.scope {
			return storagebudget.ErrBounds
		}
		switch group {
		case 9, 10, 60, 61:
			if e.Kind != 0 {
				return storagebudget.ErrBounds
			}
			if (group == 9 || group == 10) && (p.previousEvidence == nil || storageEnrollmentPeakCompareSnapshot(e, *p.previousEvidence, record, claim) != nil) {
				return storagebudget.ErrBounds
			}
		case 66:
			if e.Kind != 1 {
				return storagebudget.ErrBounds
			}
		default:
			phase := byte(0)
			if group == 11 || group == 12 {
				phase = 1
			}
			if group == 13 {
				phase = 2
			}
			if group == 14 || group == 15 {
				phase = 3
			}
			if e.Kind != 2 || e.Marker == nil || e.Marker.Phase != phase {
				return storagebudget.ErrBounds
			}
			if group == 67 || group == 68 {
				if p.anchoredStaged == nil || storageEnrollmentPeakJoinMarker(e, *p.anchoredStaged, p.original.Ready) != nil {
					return storagebudget.ErrAuthority
				}
			}
			if phase == 1 || phase == 2 {
				if p.evidence == nil || storageEnrollmentPeakJoinMarker(e, *p.evidence, p.original.Ready) != nil {
					return storagebudget.ErrAuthority
				}
			}
			if phase == 3 {
				if p.completedEvidence == nil || storageEnrollmentPeakJoinMarker(e, *p.completedEvidence, p.original.Ready) != nil {
					return storagebudget.ErrAuthority
				}
			}
		}
	case 16, 17, 18, 19, 20:
		if p.evidence == nil || p.evidence.Scope != *p.scope {
			return storagebudget.ErrAuthority
		}
		d, err := storageEnrollmentPeakDecodeDispatch(raw, *p.evidence)
		step := byte(0)
		if group >= 18 {
			step = byte(group - 17)
		}
		if err != nil || d.Step != step {
			return storagebudget.ErrBounds
		}
	default:
		return storagebudget.ErrAuthority
	}
	return nil
}

// The lineage operand comes only from this epoch's retained original images or
// its fully joined packet. It supplies decoding context, never a native grant.
func (p *storageEnrollmentEpoch) compositionLineage() *storageCompositionLineage {
	if p == nil {
		return nil
	}
	for _, e := range []*storageEnrollmentPeakEvidence{p.evidence, p.initialPacket, p.completedEvidence, p.previousEvidence} {
		if e != nil && e.Version >= 2 && e.CompositionLineage != nil {
			return e.CompositionLineage
		}
	}
	if p.original != nil && storagePolicyActualPair(p.original.Catalogs) && p.c.ref.cell.ownerRecord != nil {
		p.c.ref.cell.mu.Lock()
		endpoint, catalog := p.c.ref.cell.enrollmentEndpoint, p.c.ref.cell.ownerCatalog
		p.c.ref.cell.mu.Unlock()
		if endpoint != nil {
			pair, e := storageOwnerV6NativeReadPair(p.original.Catalogs[0], p.original.Catalogs[1], &catalog, endpoint.Raw, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
			if e == nil {
				l := pair.Banks[pair.Selected].Stage.Lineage
				return &l
			}
			selected := byte(0)
			if storageEnrollmentPeakU64(p.original.Catalogs[1][16:24]) > storageEnrollmentPeakU64(p.original.Catalogs[0][16:24]) {
				selected = 1
			}
			_, _, lineage, _, _, err := storageOwnerV6CaptureBefore(p.original.Catalogs, selected, endpoint.Raw, catalog, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
			if err == nil {
				return &lineage
			}
		}
	}
	if p.original != nil && p.c.ref.cell.ownerRecord != nil && len(p.original.Catalogs[0]) == storageOwnerCatalogBytes && string(p.original.Catalogs[0][:8]) == "DSKONL1\x00" {
		pair, err := storageCompositionReadPair(p.original.Catalogs[0], p.original.Catalogs[1], *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
		if err == nil {
			l := pair.Banks[pair.Selected].Lineage
			return &l
		}
	}
	if p.c != nil && (p.c.startupComposed || storagePolicyActualPair(p.c.startupPhysical)) && p.c.startupLineage != nil {
		return p.c.startupLineage
	}
	return nil
}

func (p *storageEnrollmentEpoch) decodeEnrollmentEvidence(raw []byte) (storageEnrollmentPeakEvidence, error) {
	return p.operands.evidence(raw, p.compositionLineage(), *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
}

// A caller comparing a core against a physical original must first validate the
// complete wrapper and the exact retained lineage. Physical bytes stay in the
// original file and in all original-input commitments.
func (p *storageEnrollmentEpoch) enrollmentCore(raw []byte) ([]byte, error) {
	if len(raw) != storageOwnerCatalogBytes {
		return nil, storagebudget.ErrBounds
	}
	if bytes.Equal(raw[:8], storageOwnerV6Magic[:]) {
		p.c.ref.cell.mu.Lock()
		endpoint := p.c.ref.cell.enrollmentEndpoint
		p.c.ref.cell.mu.Unlock()
		if endpoint == nil {
			return nil, errStorageEnrollmentUnresolved
		}
		v, err := storageOwnerV6NativeReadEndpoint(raw, endpoint.Raw, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
		if err != nil {
			return nil, err
		}
		return v.Stage.Core, nil
	}
	if string(raw[:8]) != "DSKONL1\x00" {
		if p.compositionLineage() != nil {
			return nil, storagebudget.ErrBounds
		}
		return raw, nil
	}
	v, err := storageCompositionDecode(raw, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
	retained := p.compositionLineage()
	if err != nil || retained == nil || !reflect.DeepEqual(v.Lineage, *retained) {
		return nil, storagebudget.ErrBounds
	}
	return v.Core, nil
}

// Wrap only the exact closed R1 publication. Duplicate groups copy the full
// surviving physical image; new groups advance its physical checksum chain by
// the accepted offset while preserving the independent E1 core sequence.
func (p *storageEnrollmentEpoch) enrollmentPhysicalPublication(ctx context.Context, group uint16, core []byte) ([]byte, error) {
	if p.original != nil && storagePolicyActualPair(p.original.Catalogs) {
		if p.scope == nil || p.previousEvidence == nil {
			return nil, storagebudget.ErrAuthority
		}
		shape, err := storageCompositionPublication(p.scope.path(), p.scope[322], group)
		if err != nil {
			return nil, err
		}
		peer, err := p.readEnrollmentTarget(ctx, 5+(1-shape.Bank))
		if err != nil {
			return nil, err
		}
		return storageOwnerV6NativePublication(p.original.Catalogs, p.scope[322], p.previousEvidence.Raw, p.previousEvidence.Inner.Catalog, *p.scope, group, core, peer, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
	}
	lineage := p.compositionLineage()
	if lineage == nil {
		return core, nil
	}
	record, claim := *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim
	shape, err := storageCompositionPublication(p.scope.path(), p.scope[322], group)
	if err != nil {
		return nil, err
	}
	outer, err := p.operands.envelope(core, record, claim, true, true)
	if err != nil || outer.Kind != shape.CoreKind || outer.Scope != *p.scope || outer.Marker != nil && outer.Marker.Phase != shape.MarkerPhase {
		return nil, storagebudget.ErrBounds
	}
	base, err := storageCompositionReadPair(p.original.Catalogs[0], p.original.Catalogs[1], record, claim)
	if err != nil || base.RecoveryOnly {
		return nil, storagebudget.ErrBounds
	}
	sequence, err := storageCompositionSequence(base.Banks[base.Selected].Sequence, shape.Offset)
	if err != nil {
		return nil, err
	}
	peer, err := p.readEnrollmentTarget(ctx, 5+(1-shape.Bank))
	if err != nil {
		return nil, err
	}
	previous, err := storageCompositionDecode(peer, record, claim)
	if err != nil || !reflect.DeepEqual(previous.Lineage, *lineage) {
		return nil, storagebudget.ErrBounds
	}
	if shape.Duplicate {
		if previous.Sequence != sequence || previous.Transition != storageCompositionEnrollment || previous.TransitionID != storagebudget.ID([16]byte(outer.Scope[24:40])) || previous.BeforeBank != p.scope[322] || previous.Publication != shape.Publication || !bytes.Equal(previous.Core, core) {
			return nil, storagebudget.ErrBounds
		}
		return append([]byte{}, peer...), nil
	}
	if previous.Sequence == ^uint64(0) || previous.Sequence+1 != sequence {
		return nil, storagebudget.ErrBounds
	}
	checksum, err := storageCompositionChecksumOf(peer)
	if err != nil {
		return nil, err
	}
	before := base.Banks[base.Selected].Core
	v := storageCompositionBank{Sequence: sequence, CoreCodec: 2, Publication: shape.Publication, Transition: storageCompositionEnrollment, BeforeBank: p.scope[322], TransitionID: storagebudget.ID([16]byte(outer.Scope[24:40])), Previous: checksum, CoreFull: storageCompositionCoreDigest(sha256.Sum256(core)), LineageDigest: base.Banks[base.Selected].LineageDigest, BeforePeer: storageCompositionDuplicateCorePeer(before, record, claim), Core: append([]byte{}, core...), Lineage: *lineage}
	return v.encode(record, claim)
}

// Capture the founding byte plan under the original epoch. This reads the
// immutable controls through their original witnesses, not a caller's encoded
// replacement. Recognized mixed founding pairs use the exact retained ID and
// original commitment; no old/torn-peer fallback or new founding ID is used.
func (p *storageEnrollmentEpoch) planCompositionEntry(ctx context.Context) error {
	if p == nil || p.self != p || p.original == nil || p.initialInspection == nil || p.compositionEntry != nil || p.scope != nil {
		return storagebudget.ErrAuthority
	}
	if err := p.held(ctx); err != nil {
		return err
	}
	if storagePolicyActualPair(p.original.Catalogs) {
		state, err := p.c.owner.readOriginalPolicy(ctx)
		if err != nil {
			return err
		}
		if !bytes.Equal(state.Actual[0], p.original.Catalogs[0]) || !bytes.Equal(state.Actual[1], p.original.Catalogs[1]) {
			return errStorageCustody
		}
		if err = p.originalPolicyCurrentQ(ctx, state, *p.initialInspection); err != nil {
			return err
		}
		p.compositionCut = 4
		return p.held(ctx)
	}
	record, claim := *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim
	read := func(name string, length int64) (raw []byte, err error) {
		n, err := storageOpenOriginalControl(p.c.ref.cell, name)
		if err != nil {
			return nil, err
		}
		defer func() { err = errors.Join(err, n.current(), n.file.Close()) }()
		if n.size != length {
			return nil, errStorageCustody
		}
		raw = make([]byte, int(length))
		count, e := n.file.ReadAt(raw, 0)
		if e != nil || count != len(raw) {
			return nil, errors.Join(storagebudget.ErrUnknown, e)
		}
		return raw, nil
	}
	recordRaw, err := read(storageRecordName, 4096)
	if err != nil {
		return err
	}
	claimRaw, err := read(storageClaimName, 4096)
	if err != nil {
		return err
	}
	ready, err := p.readOriginalEnrollmentControl(ctx, storageReadyName, 32)
	if err != nil {
		return err
	}
	var plan storageCompositionFirstBasePlan
	cut := byte(0)
	wrapped := false
	for _, raw := range p.original.Catalogs {
		wrapped = wrapped || string(raw[:8]) == "DSKONL1\x00"
	}
	if wrapped {
		pair, e := storageCompositionReadPair(p.original.Catalogs[0], p.original.Catalogs[1], record, claim)
		if e == nil && pair.Banks[pair.Selected].Transition != storageCompositionBase && (!pair.RecoveryOnly || storageIdentityPreparedLineage(pair)) {
			if storageIdentityPreparedLineage(pair) {
				p.identityRecoveryPending = true
			}
			p.compositionCut = 4
			return p.held(ctx)
		}
		plan, cut, err = storageCompositionRecoverFirstBase(p.original.Catalogs, recordRaw, claimRaw, ready, p.initialInspection.View, record, claim)
	} else {
		id, e := storagebudget.NewID()
		if e != nil {
			return e
		}
		plan, err = storageCompositionPlanFirstBase(p.original.Catalogs, id, recordRaw, claimRaw, ready, p.initialInspection.View, record, claim)
	}
	if err != nil {
		return err
	}
	// Founding cannot discharge an unresolved original J/Q obligation. The
	// existing mapping checker proves the complete reusable original J2 table.
	table := storageIntentTable{owner: p.c.owner}
	for slot, pages := range p.original.Hosts {
		choice, e := storageSelectCreationHost(pages[0], pages[1], slot, record.Installation, record.QBinding)
		if e != nil {
			return e
		}
		if choice.Identity != nil || !choice.Slot.record.reusable() || choice.Creation != nil && !p.c.owner.creationTailCurrent(ctx, *choice.Creation, choice.Slot.page, storageHostState(p.initialInspection.View)) {
			return errStorageEnrollmentUnresolved
		}
		table.slots[slot] = choice.Slot
	}
	if err = p.c.owner.validateIntentMappings(ctx, &table, storageHostState(p.initialInspection.View)); err != nil {
		return err
	}
	p.compositionEntry = &plan
	p.compositionCut = cut
	return p.held(ctx)
}

// Compare the complete unchanged endpoint independently of the physical wrapper
// being written. First-base never makes a Q call or a namespace effect.
func (p *storageEnrollmentEpoch) compositionEntryCurrent(ctx context.Context) error {
	if p == nil || p.compositionEntry == nil || p.identityLaunch == nil || p.qualified == nil || p.qualified.record.Version < 2 {
		return storagebudget.ErrAuthority
	}
	if err := p.checkIdentityLaunch(ctx, p.identityLaunch); err != nil {
		return err
	}
	q, err := p.c.ref.cell.q.InspectEnrollment(ctx)
	if err != nil || !reflect.DeepEqual(q, *p.initialInspection) {
		return errors.Join(errStorageEnrollmentUnresolved, err)
	}
	ready, err := p.readOriginalEnrollmentControl(ctx, storageReadyName, 32)
	if err != nil || !bytes.Equal(ready, p.c.ref.cell.binding[:]) {
		return errors.Join(errStorageCustody, err)
	}
	host, err := p.readOriginalEnrollmentControl(ctx, storageIntentName, storageIntentBytes)
	if err != nil {
		return err
	}
	for slot, pages := range p.original.Hosts {
		for bank, page := range pages {
			at := (slot*2 + bank) * storageIntentPage
			if !bytes.Equal(host[at:at+storageIntentPage], page) {
				return errStorageCustody
			}
		}
	}
	for _, name := range []string{storageRecordName, storageClaimName} {
		var want []byte
		if name == storageRecordName {
			want, err = storageOwnerEncodeRecordV2(*p.c.ref.cell.ownerRecord)
		} else {
			want, err = storageOwnerEncodeClaimV2(p.c.ref.cell.ownerClaim)
		}
		if err != nil {
			return err
		}
		n, e := storageOpenOriginalControl(p.c.ref.cell, name)
		if e != nil {
			return e
		}
		got := make([]byte, len(want))
		count, e := n.file.ReadAt(got, 0)
		e = errors.Join(e, n.current(), n.file.Close())
		if e != nil || count != len(got) || !bytes.Equal(got, want) {
			return errors.Join(errStorageCustody, e)
		}
	}
	return p.held(ctx)
}

func (p *storageEnrollmentEpoch) compositionCatalogCurrent(ctx context.Context, bank byte, expected []byte) error {
	if bank > 1 || len(expected) != storageOwnerCatalogBytes {
		return storagebudget.ErrBounds
	}
	t := &p.targets[4+bank]
	if t.named.file == nil || t.native != p.original.Targets[4+bank] || t.named.size != storageOwnerCatalogBytes {
		return errStorageCustody
	}
	if err := t.named.current(); err != nil {
		return err
	}
	native, err := storageOwnerPersistentFile(ctx, t.named)
	if err != nil || native != t.native {
		return errors.Join(errStorageCustody, err)
	}
	buf := make([]byte, 65536)
	for at := 0; at < len(expected); at += len(buf) {
		if err = p.permit(ctx); err != nil {
			return err
		}
		count, e := t.named.file.ReadAt(buf, int64(at))
		if e != nil || count != len(buf) || !bytes.Equal(buf, expected[at:at+len(buf)]) {
			return errors.Join(errStorageCustody, e)
		}
	}
	return p.held(ctx)
}

// Publish/persist are separate exact original groups. The write is consumed
// before its first WriteAt and is never repeated, including a short write.
// Persistence uses fresh descriptions of the retained original inode and exact
// byte readback. Its errors retain both spent groups and the whole launch debt.
func (p *storageEnrollmentEpoch) compositionCatalogGroup(ctx context.Context, key string, bank byte, raw []byte, write bool) (err error) {
	if bank > 1 || len(raw) != storageOwnerCatalogBytes || p == nil || p.compositionEntry == nil {
		return storagebudget.ErrBounds
	}
	plan := p.compositionEntry
	wantBank, wantRaw, wantWrite := plan.FirstBank, plan.Prepared, false
	valid := false
	switch key {
	case "composition-entry/prepared/publish":
		valid = p.compositionCut == 0
		wantWrite = true
	case "composition-entry/prepared/persist":
		valid = p.compositionCut == 1 || p.compositionCut == 2
	case "lin4/bridge-duplicate/publish":
		valid = p.compositionCut == 1
		wantBank = plan.BeforeBank
		wantWrite = true
	case "lin4/bridge-duplicate/persist":
		valid = p.compositionCut == 2
		wantBank = plan.BeforeBank
	case "composition-entry/active/publish":
		valid = p.compositionCut == 2
		wantRaw = plan.Active
		wantWrite = true
	case "composition-entry/active/persist":
		valid = p.compositionCut == 3
		wantRaw = plan.Active
	}
	if !valid || bank != wantBank || write != wantWrite || !bytes.Equal(raw, wantRaw) {
		return storagebudget.ErrAuthority
	}
	if err = p.compositionEntryCurrent(ctx); err != nil {
		return err
	}
	before := plan.Original
	if p.compositionCut >= 1 {
		before[plan.FirstBank] = plan.Prepared
	}
	if p.compositionCut >= 2 {
		before[plan.BeforeBank] = plan.Prepared
	}
	if p.compositionCut == 3 {
		before[plan.FirstBank] = plan.Active
	}
	for bank, expected := range before {
		if err = p.compositionCatalogCurrent(ctx, byte(bank), expected); err != nil {
			return err
		}
	}
	if err = p.takeIdentityCatalog(ctx, key, bank); err != nil {
		return err
	}
	p.mu.Lock()
	if !p.running || p.revoked || p.entered || p.finishing || p.err != nil {
		p.mu.Unlock()
		return storagebudget.ErrAuthority
	}
	p.entered = true
	p.compositionStarted = true
	p.mu.Unlock()
	defer func() {
		err = p.leaveNative(err)
		if err != nil {
			p.identityLaunch.failed = errors.Join(p.identityLaunch.failed, err)
		}
	}()
	t := &p.targets[4+bank]
	if err = t.named.current(); err != nil {
		return err
	}
	n, err := storageOpenFile(t.named.root, t.named.name, storageOwnerCatalogBytes)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			err = errors.Join(err, n.file.Close())
		}
	}()
	if !os.SameFile(n.identity, t.named.identity) {
		return errStorageCustody
	}
	native, err := storageOwnerPersistentFile(ctx, n)
	if err != nil || native != t.native {
		return errors.Join(errStorageCustody, err)
	}
	if write {
		for at := 0; at < len(raw); at += 65536 {
			if err = p.boundary("composition-write"); err != nil {
				return err
			}
			if err = p.permit(ctx); err != nil {
				return err
			}
			if err = storageEnrollmentNativeWrite(ctx, n.file, raw[at:at+65536], int64(at)); err != nil {
				return err
			}
		}
	} else {
		if err = p.boundary("composition-sync"); err != nil {
			return err
		}
		if err = p.permit(ctx); err != nil {
			return err
		}
		if err = storageEnrollmentNativeSync(ctx, n.file); err != nil {
			return err
		}
	}
	closed = true
	if err = errors.Join(n.file.Close(), p.boundary("composition-close")); err != nil {
		return err
	}
	if !write {
		parent, e := p.c.original.config.Open(".")
		if e != nil {
			return e
		}
		got, e := storageAdvisoryPersistentHeld(ctx, parent)
		if e == nil && got != p.c.ref.cell.ownerRecord.Config {
			e = errStorageCustody
		}
		if e == nil {
			e = p.boundary("composition-parent-sync")
		}
		if e == nil {
			e = p.permit(ctx)
		}
		if e == nil {
			e = storageEnrollmentNativeSync(ctx, parent)
		}
		if err = errors.Join(e, parent.Close(), p.boundary("composition-parent-close")); err != nil {
			return err
		}
	}
	if err = p.boundary("composition-readback"); err != nil {
		return err
	}
	return p.compositionCatalogCurrent(ctx, bank, raw)
}

// This is the actual original first-base executor, including all three known
// mixed/restart cuts. It does not issue identity, enrollment or Q work. Its
// caller must have admitted the complete actual remaining identity board first.
func (p *storageEnrollmentEpoch) runCompositionEntry(ctx context.Context) error {
	if err := p.compositionEntryCurrent(ctx); err != nil {
		return err
	}
	plan := p.compositionEntry
	f, b := plan.FirstBank, plan.BeforeBank
	if p.compositionCut > 3 {
		return storagebudget.ErrAuthority
	}
	actual := plan.Original
	if p.compositionCut >= 1 {
		actual[f] = plan.Prepared
	}
	if p.compositionCut >= 2 {
		actual[b] = plan.Prepared
	}
	if p.compositionCut == 3 {
		actual[f] = plan.Active
	}
	for bank, raw := range actual {
		if err := p.compositionCatalogCurrent(ctx, byte(bank), raw); err != nil {
			return err
		}
	}
	if p.compositionCut == 0 {
		if err := p.compositionCatalogGroup(ctx, "composition-entry/prepared/publish", f, plan.Prepared, true); err != nil {
			return err
		}
		p.compositionCut = 1
	}
	if p.compositionCut <= 2 {
		if err := p.compositionCatalogGroup(ctx, "composition-entry/prepared/persist", f, plan.Prepared, false); err != nil {
			return err
		}
		if p.compositionCut == 1 {
			if err := p.compositionCatalogGroup(ctx, "lin4/bridge-duplicate/publish", b, plan.Prepared, true); err != nil {
				return err
			}
			p.compositionCut = 2
		}
		if err := p.compositionCatalogGroup(ctx, "lin4/bridge-duplicate/persist", b, plan.Prepared, false); err != nil {
			return err
		}
		for bank := byte(0); bank < 2; bank++ {
			if err := p.compositionCatalogCurrent(ctx, bank, plan.Prepared); err != nil {
				return err
			}
		}
		if err := p.compositionEntryCurrent(ctx); err != nil {
			return err
		}
		if err := p.boundary("composition-both-prepared"); err != nil {
			return err
		}
		if err := p.compositionCatalogGroup(ctx, "composition-entry/active/publish", f, plan.Active, true); err != nil {
			return err
		}
		p.compositionCut = 3
	}
	if err := p.compositionCatalogGroup(ctx, "composition-entry/active/persist", f, plan.Active, false); err != nil {
		return err
	}
	pair := [2][]byte{plan.Prepared, plan.Prepared}
	pair[f] = plan.Active
	for bank, raw := range pair {
		if err := p.compositionCatalogCurrent(ctx, byte(bank), raw); err != nil {
			return err
		}
	}
	record, claim := *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim
	if _, err := storageCompositionReadPair(pair[0], pair[1], record, claim); err != nil {
		return err
	}
	if err := p.compositionEntryCurrent(ctx); err != nil {
		return err
	}
	p.original.Catalogs = pair
	p.compositionCut = 4
	return p.boundary("composition-active-complete")
}

// Dependency bodies collocated here so the existing native source pin covers
// the complete original control/planning implementation. Algorithms unchanged.

func storageEnrollmentMarkerFor(e storageEnrollmentPeakEvidence, phase byte, previous storagebudget.Digest) storageEnrollmentPeakMarker {
	return storageEnrollmentPeakMarker{Phase: phase, Bank: e.Bank, Installation: e.Installation, QBinding: e.QBinding, Bridge: e.Bridge, Previous: previous, Before: e.Before, Inner: storageEnrollmentPeakInnerDigest(e.Inner.Raw), Evidence: sha256.Sum256(e.Raw), Extent: e.Extent, Controls: e.Controls, CoveredEpoch: e.CoveredEpoch, Q0Revision: e.BeforeRevision, Q0Epoch: e.BeforeEpoch, Q3Revision: e.AfterRevision, Q3Epoch: e.AfterEpoch, Record: e.Record, Claim: e.Claim, Ready: e.Ready}
}

func storageEnrollmentCensusInQOrder(value storagebudget.Census, q *storageEnrollmentPeakQCheckpoint) (storageEnrollmentPeakQCensus, error) {
	if q == nil {
		return storageEnrollmentPeakQCensus{}, storagebudget.ErrBounds
	}
	var out storageEnrollmentPeakQCensus
	out.Evidence = storageEnrollmentPeakQDigest(value.Evidence)
	roots := map[storagebudget.ID]storagebudget.RootMeasurement{}
	for _, row := range value.Roots {
		if _, ok := roots[row.ID]; ok {
			return out, storagebudget.ErrBounds
		}
		roots[row.ID] = row
	}
	for _, row := range q.Roots {
		if row.ID == (storageEnrollmentPeakQID{}) {
			continue
		}
		measurement, ok := roots[storagebudget.ID(row.ID)]
		if !ok || measurement.Generation != row.Generation {
			return out, storagebudget.ErrStale
		}
		m := storageEnrollmentPeakQRootMeasurement{ID: row.ID, Generation: row.Generation}
		for i, v := range measurement.Usage {
			m.Usage[i] = storageEnrollmentPeakQVector{Bytes: v.Bytes, Files: v.Files, Directories: v.Directories}
		}
		out.Roots = append(out.Roots, m)
		delete(roots, measurement.ID)
	}
	workspaces := map[storagebudget.ID]storagebudget.WorkspaceMeasurement{}
	for _, row := range value.Workspaces {
		if _, ok := workspaces[row.ID]; ok {
			return out, storagebudget.ErrBounds
		}
		workspaces[row.ID] = row
	}
	for _, row := range q.Workspaces {
		if row.ID == (storageEnrollmentPeakQID{}) {
			continue
		}
		measurement, ok := workspaces[storagebudget.ID(row.ID)]
		if !ok || measurement.Generation != row.Generation {
			return out, storagebudget.ErrStale
		}
		m := storageEnrollmentPeakQWorkspaceMeasurement{ID: row.ID, Generation: row.Generation}
		for i, v := range measurement.Usage {
			m.Usage[i] = storageEnrollmentPeakQVector{Bytes: v.Bytes, Files: v.Files, Directories: v.Directories}
		}
		out.Workspaces = append(out.Workspaces, m)
		delete(workspaces, measurement.ID)
	}
	if len(roots) != 0 || len(workspaces) != 0 {
		return out, storagebudget.ErrBounds
	}
	return out, nil
}

// Rebuild only the phase-qualified F/board fields from the actual original
// measurement. All original Q0/source/origin/registration sections survive
// byte-for-byte; extents and section shape cannot grow after measurement.
func (p *storageEnrollmentEpoch) armInitialEnrollment(ctx context.Context, staged storageEnrollmentPeakEvidence) (storageEnrollmentPeakEvidence, error) {
	if p == nil || p.self != p || p.factualCensus == nil || staged.Phase != 0 || staged.Bank != 1 || staged.Generation != 2 || staged.Scope != *p.scope {
		return storageEnrollmentPeakEvidence{}, storagebudget.ErrAuthority
	}
	raw, err := storageEnrollmentPeakArmStagedOperands(staged, *p.factualCensus, nil, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim, &p.operands)
	if err != nil {
		return storageEnrollmentPeakEvidence{}, err
	}
	if err = p.bindEnrollmentArmed(ctx, raw); err != nil {
		return storageEnrollmentPeakEvidence{}, err
	}
	return *p.evidence, nil
}

// One synchronous initial bridge. Inputs are host-authored pure packet data,
// independently checked against retained originals by the admission/capture
// steps; no callback or detached record supplies native permission. This path
// does not catch an uncertain Q result and does not replay the action.
func (p *storageEnrollmentEpoch) runInitialEnrollmentControls(ctx context.Context, stagedA []byte) (err error) {
	if p == nil || p.self != p || p.scope == nil || p.scope.path() != 1 || p.scope[321] != 15 || !p.fenced || p.candidate == nil || p.evidence != nil {
		return storagebudget.ErrAuthority
	}
	record, claim := *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim
	a, err := p.decodeEnrollmentEvidence(stagedA)
	if err != nil || a.Phase != 0 || a.Bank != 0 || a.Generation != 1 || a.Previous != (storagebudget.Digest{}) || a.Scope != *p.scope {
		return errors.Join(storagebudget.ErrBounds, err)
	}
	if err = p.publishEnrollmentControl(ctx, 1, stagedA); err != nil {
		return err
	}
	b := a
	b.Bank = 1
	b.Generation = 2
	b.Extent = b.Scope.final(1)
	b.Previous = sha256.Sum256(stagedA)
	stagedB, err := storageEnrollmentPeakEncodeEvidenceOperands(b, record, claim, &p.operands)
	if err != nil {
		return err
	}
	if err = p.publishEnrollmentControl(ctx, 3, stagedB); err != nil {
		return err
	}
	b, err = p.decodeEnrollmentEvidence(stagedB)
	if err != nil {
		return err
	}
	entered := storageEnrollmentPeakDispatch{Generation: 1, Installation: a.Installation, Binding: a.QBinding, Bridge: a.Bridge, BeforeRevision: a.BeforeRevision, BeforeEpoch: a.BeforeEpoch, Before: a.Checkpoint.Checkpoints[a.Checkpoint.Bank], Origin: a.Digests.Origin, Controls: [2]storageOwnerPersistentObject{a.Controls[2], a.Controls[3]}}
	dispatch, err := storageEnrollmentPeakEncodeDispatch(entered, a)
	if err != nil {
		return err
	}
	p.evidence = &a
	if err = p.publishEnrollmentControl(ctx, 16, dispatch); err != nil {
		return err
	}
	if err = p.publishEnrollmentControl(ctx, 17, dispatch); err != nil {
		return err
	}
	p.evidence = nil
	if _, err = p.measureEnrollmentCandidate(ctx); err != nil {
		return err
	}
	armed, err := p.armInitialEnrollment(ctx, b)
	if err != nil {
		return err
	}
	if err = p.publishEnrollmentControl(ctx, 5, armed.Raw); err != nil {
		return err
	}
	capturedRaw, err := p.readEnrollmentTarget(ctx, 5+p.scope[322])
	if err != nil {
		return err
	}
	capturedRaw, err = p.enrollmentCore(capturedRaw)
	if err != nil {
		return err
	}
	var predecessor storagebudget.Digest
	copy(predecessor[:], capturedRaw[32:64])
	marker := storageEnrollmentMarkerFor(armed, 1, predecessor)
	prepared := storageEnrollmentPeakEnvelope{Kind: 2, Sequence: storageEnrollmentPeakU64(p.scope[1624:1632]) + 3, Scope: *p.scope, Marker: &marker}
	// Marker-only frames retain the original epoch codec context; unlike
	// the initial Snapshot, they cannot infer it from a physical suffix.
	preparedRaw, err := p.encodeEnrollmentEnvelope(prepared)
	if err != nil {
		return err
	}
	for _, group := range []uint16{11, 12} {
		if err = p.publishEnrollmentControl(ctx, group, preparedRaw); err != nil {
			return err
		}
	}
	if err = p.dispatchEnrollmentQ(ctx); err != nil {
		return err
	}
	if err = p.dispatchEnrollmentQ(ctx); err != nil {
		return err
	}
	copy(predecessor[:], preparedRaw[32:64])
	marker = storageEnrollmentMarkerFor(armed, 2, predecessor)
	definitions := storageEnrollmentPeakEnvelope{Kind: 2, Sequence: prepared.Sequence + 1, Scope: *p.scope, Marker: &marker}
	definitionsRaw, err := p.encodeEnrollmentEnvelope(definitions)
	if err != nil {
		return err
	}
	if err = p.publishEnrollmentControl(ctx, 13, definitionsRaw); err != nil {
		return err
	}
	if err = p.dispatchEnrollmentQ(ctx); err != nil {
		return err
	}
	completed := armed
	completed.Phase = 2
	completed.Bank = 1
	completed.Generation = 4
	completed.Extent = p.scope.final(1)
	completed.Previous = sha256.Sum256(armed.Raw)
	completedRaw, err := storageEnrollmentPeakEncodeEvidenceOperands(completed, record, claim, &p.operands)
	if err != nil {
		return err
	}
	if err = p.publishEnrollmentControl(ctx, 7, completedRaw); err != nil {
		return err
	}
	completed = *p.completedEvidence
	copy(predecessor[:], definitionsRaw[32:64])
	marker = storageEnrollmentMarkerFor(completed, 3, predecessor)
	final := storageEnrollmentPeakEnvelope{Kind: 2, Sequence: definitions.Sequence + 1, Scope: *p.scope, Marker: &marker}
	finalRaw, err := p.encodeEnrollmentEnvelope(final)
	if err != nil {
		return err
	}
	for _, group := range []uint16{14, 15} {
		if err = p.publishEnrollmentControl(ctx, group, finalRaw); err != nil {
			return err
		}
	}
	return p.reconcileEnrollment(ctx, finalRaw)
}

func (p *storageEnrollmentEpoch) reconcileEnrollment(ctx context.Context, finalRaw []byte) error {
	if p == nil || p.self != p || p.completedEvidence == nil || p.candidate == nil || p.candidateObserver == nil || p.factualCensus == nil || p.planCursor != len(p.enrollmentPassPlan()) {
		return storagebudget.ErrAuthority
	}
	actual, err := p.compareEnrollmentActual(ctx)
	if err != nil || actual.Q.UnresolvedQAttempt || actual.Q.Stage != 3 || actual.Envelope.Marker == nil || actual.Envelope.Marker.Phase != 3 || actual.Evidence.Phase != 2 || !bytes.Equal(actual.Evidence.Raw, p.completedEvidence.Raw) {
		return errors.Join(errStorageEnrollmentUnresolved, err)
	}
	for target := byte(5); target <= 6; target++ {
		raw, e := p.readEnrollmentTarget(ctx, target)
		if e == nil {
			raw, e = p.enrollmentCore(raw)
		}
		if e != nil || !bytes.Equal(raw, finalRaw) {
			return errors.Join(errStorageEnrollmentUnresolved, e)
		}
	}
	if p.creation != nil && p.creation.record.Kind == storageCreationHistory {
		if err = p.creation.completeHistoryPointer(ctx, actual.Evidence); err != nil {
			return err
		}
	}
	if p.startup && p.creation == nil && actual.Evidence.Board.Source.Default != nil && (actual.Evidence.Board.Source.Default.Workflow == 1 || actual.Evidence.Board.Source.Default.Workflow == 3) {
		if err = p.completeStartupHistoryDictionary(ctx, actual.Evidence); err != nil {
			return err
		}
	}
	if err = p.c.sources.current(ctx); err != nil {
		return err
	}
	if err = p.nativePermit(ctx); err != nil {
		return err
	}
	if p.transfer != nil {
		err = p.installOriginalTransferBindings(ctx)
	} else {
		err = p.candidate.install(p.c.owner)
	}
	if err != nil {
		return err
	}
	if p.measurement != nil {
		p.measurement.value.Epoch = actual.Evidence.AfterEpoch
		p.measurement.value.Revision = actual.Evidence.AfterRevision
		p.measurement.value.BoundaryKnown = true
	}
	o := p.c.owner
	o.mu.Lock()
	o.observer = p.candidateObserver
	o.censusEpoch = actual.Evidence.AfterEpoch
	cache := o.statusCache
	o.mu.Unlock()
	p.c.ref.cell.mu.Lock()
	p.c.ref.cell.ownerCatalog = actual.Evidence.Inner.Catalog
	p.c.ref.cell.enrollmentEndpoint = p.completedEvidence
	p.c.ref.cell.mu.Unlock()
	if cache != nil {
		cache.invalidate()
	}
	p.completeScopeLaunch(actual.Evidence)
	p.mu.Lock()
	p.reconciled = true
	p.mu.Unlock()
	return nil
}

func (p *storageEnrollmentEpoch) runSuccessorEnrollmentControls(ctx context.Context) error {
	if p == nil || p.scope == nil || p.scope.path() == 1 || !p.fenced || p.previousEvidence == nil || p.initialPacket == nil || p.candidate == nil || p.evidence != nil {
		return storagebudget.ErrAuthority
	}
	record, claim := *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim
	model := *p.initialPacket
	var unreferenced *storageEnrollmentPeakEvidence
	var err error
	if p.startup {
		unreferenced, err = p.compareSuccessorUnreferenced(ctx, model, nil)
		if err != nil {
			return err
		}
	}
	var stagedB *storageEnrollmentPeakEvidence
	if p.scope.path() == 3 {
		raw, err := p.successorStagedPacket(ctx)
		if err != nil {
			return err
		}
		if err = p.publishEnrollmentControl(ctx, 1, raw); err != nil {
			return err
		}
		if err = p.successorBoundary("successor-stage-a"); err != nil {
			return err
		}
		a := *p.anchoredStaged
		reference, err := p.enrollmentStagedReference(ctx, a)
		if err != nil {
			return err
		}
		for _, id := range []uint16{67, 68} {
			if err = p.publishEnrollmentControl(ctx, id, reference); err != nil {
				return err
			}
			if id == 67 {
				if err = p.successorBoundary("successor-first-reference"); err != nil {
					return err
				}
			}
		}
		// Both actual R banks must retain the exact complete Stage-A before the
		// old selected evidence bank can be overwritten by Stage-B or Armed.
		for target := byte(5); target <= 6; target++ {
			actual, e0 := p.readEnrollmentTarget(ctx, target)
			if e0 == nil {
				actual, e0 = p.enrollmentCore(actual)
			}
			if e0 != nil || !bytes.Equal(actual, reference) {
				return errors.Join(errStorageEnrollmentUnresolved, e0)
			}
		}
		if err = p.successorBoundary("successor-two-reference"); err != nil {
			return err
		}
		b := a
		b.Bank = p.scope[323]
		b.Extent = p.scope.final(b.Bank)
		b.Generation++
		b.Previous = sha256.Sum256(a.Raw)
		raw, err = storageEnrollmentPeakEncodeEvidenceOperands(b, record, claim, &p.operands)
		if err != nil {
			return err
		}
		if err = p.publishEnrollmentControl(ctx, 3, raw); err != nil {
			return err
		}
		b, err = p.decodeEnrollmentEvidence(raw)
		if err != nil {
			return err
		}
		stagedB = &b
		model = a
	}
	raw, err := p.successorEntered(ctx, model)
	if err != nil {
		return err
	}
	p.evidence = &model
	if err = p.publishEnrollmentControl(ctx, 16, raw); err != nil {
		return err
	}
	if err = p.successorBoundary("successor-entered"); err != nil {
		return err
	}
	p.evidence = nil
	if _, err = p.measureEnrollmentCandidate(ctx); err != nil {
		return err
	}
	if unreferenced != nil && unreferenced.Phase == 1 && !storageEnrollmentCensusEqual(*p.factualCensus, storageEnrollmentFactualCensus(unreferenced.Census)) {
		return errStorageCustody
	}
	if stagedB != nil {
		raw, err = storageEnrollmentPeakArmStagedOperands(*stagedB, *p.factualCensus, nil, record, claim, &p.operands)
	} else {
		raw, err = storageEnrollmentPeakArmPlannedOperands(model, *p.factualCensus, p.previousEvidence.Generation+1, sha256.Sum256(p.previousEvidence.Raw), nil, record, claim, &p.operands)
	}
	if err != nil {
		return err
	}
	if err = p.bindEnrollmentArmed(ctx, raw); err != nil {
		return err
	}
	if err = p.publishEnrollmentControl(ctx, 5, raw); err != nil {
		return err
	}
	if err = p.successorBoundary("successor-armed"); err != nil {
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
		if id == 11 {
			if err = p.successorBoundary("successor-first-prepared"); err != nil {
				return err
			}
		}
	}
	if model.Board.Source.Default != nil && (model.Board.Source.Default.Workflow == 1 || model.Board.Source.Default.Workflow == 3) {
		if err = p.boundary("creation-scope-prepared-before-q"); err != nil {
			return err
		}
	}
	return p.completePreparedEnrollment(ctx)
}

func (p *storageEnrollmentEpoch) enrollmentStagedReference(ctx context.Context, e storageEnrollmentPeakEvidence) ([]byte, error) {
	if p.scope == nil || p.scope.path() != 3 || e.Phase != 0 || p.anchoredStaged == nil || !bytes.Equal(e.Raw, p.anchoredStaged.Raw) {
		return nil, storagebudget.ErrAuthority
	}
	old, err := p.selectedEnrollmentCatalog(ctx)
	if err != nil {
		return nil, err
	}
	var digest storagebudget.Digest
	copy(digest[:], old[32:64])
	marker := storageEnrollmentMarkerFor(e, 0, digest)
	envelope := storageEnrollmentPeakEnvelope{Kind: 2, BeforeCodec: 1, Sequence: storageEnrollmentPeakU64(p.scope[1624:1632]) + 2, Scope: *p.scope, Marker: &marker}
	return p.encodeEnrollmentEnvelope(envelope)
}

func (p *storageEnrollmentEpoch) enrollmentEnvelope(raw []byte, validate bool) (storageEnrollmentPeakEnvelope, error) {
	record, claim := *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim
	if p.original != nil && storagePolicyActualPair(p.original.Catalogs) {
		return storageEnrollmentPeakDecodeEnvelopeNative(raw, record, claim, validate, true, true)
	}
	return p.operands.envelope(raw, record, claim, validate, p.compositionLineage() != nil)
}

// Dispatch only from this original epoch's actual physical mode. A projected
// core or nonnil lineage alone never selects native decoding/encoding rights.
func (p *storageEnrollmentEpoch) encodeEnrollmentEnvelope(e storageEnrollmentPeakEnvelope) ([]byte, error) {
	record, claim := *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim
	if p.original != nil && storagePolicyActualPair(p.original.Catalogs) {
		return storageEnrollmentPeakEncodeEnvelopeNative(e, record, claim, true, true)
	}
	return storageEnrollmentPeakEncodeEnvelopeVariant(e, record, claim, p.compositionLineage() != nil)
}
