package desk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"reflect"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func storageEnrollmentFactualCensus(value storageEnrollmentPeakQCensus) storagebudget.Census {
	out := storagebudget.Census{Evidence: storagebudget.Digest(value.Evidence), Roots: []storagebudget.RootMeasurement{}, Workspaces: []storagebudget.WorkspaceMeasurement{}}
	for _, row := range value.Roots {
		m := storagebudget.RootMeasurement{ID: storagebudget.ID(row.ID), Generation: row.Generation}
		for i, v := range row.Usage {
			m.Usage[i] = storagebudget.Vector{Bytes: v.Bytes, Files: v.Files, Directories: v.Directories}
		}
		out.Roots = append(out.Roots, m)
	}
	for _, row := range value.Workspaces {
		m := storagebudget.WorkspaceMeasurement{ID: storagebudget.ID(row.ID), Generation: row.Generation}
		for i, v := range row.Usage {
			m.Usage[i] = storagebudget.Vector{Bytes: v.Bytes, Files: v.Files, Directories: v.Directories}
		}
		out.Workspaces = append(out.Workspaces, m)
	}
	return out
}

func storageEnrollmentCensusEqual(a, b storagebudget.Census) bool {
	if a.Evidence != b.Evidence || len(a.Roots) != len(b.Roots) || len(a.Workspaces) != len(b.Workspaces) {
		return false
	}
	roots := map[storagebudget.ID]storagebudget.RootMeasurement{}
	for _, row := range a.Roots {
		if _, ok := roots[row.ID]; ok {
			return false
		}
		roots[row.ID] = row
	}
	for _, row := range b.Roots {
		if got, ok := roots[row.ID]; !ok || got != row {
			return false
		}
		delete(roots, row.ID)
	}
	workspaces := map[storagebudget.ID]storagebudget.WorkspaceMeasurement{}
	for _, row := range a.Workspaces {
		if _, ok := workspaces[row.ID]; ok {
			return false
		}
		workspaces[row.ID] = row
	}
	for _, row := range b.Workspaces {
		if got, ok := workspaces[row.ID]; !ok || got != row {
			return false
		}
		delete(workspaces, row.ID)
	}
	return len(roots) == 0 && len(workspaces) == 0
}

func (p *storageEnrollmentEpoch) currentEnrollmentOriginal(ctx context.Context) (out storageEnrollmentPeakOriginalComparison, err error) {
	if p == nil || p.self != p || p.original == nil || p.scope == nil {
		return out, storagebudget.ErrAuthority
	}
	if err = p.nativePermit(ctx); err != nil {
		return out, err
	}
	out = *p.original
	exact := !(p.evidence != nil && p.evidence.Phase == 0 && p.scope.path() == 3)
	for target := byte(1); target <= 8; target++ {
		if err = p.targetCurrent(ctx, target, exact); err != nil {
			return storageEnrollmentPeakOriginalComparison{}, err
		}
		out.Targets[target-1], out.Extents[target-1] = p.targets[target-1].native, uint64(p.targets[target-1].named.size)
	}
	ready, err := p.readOriginalEnrollmentControl(ctx, storageReadyName, 32)
	if err != nil || sha256.Sum256(ready) != out.Ready {
		return storageEnrollmentPeakOriginalComparison{}, errors.Join(errStorageCustody, err)
	}
	hosts, err := p.readOriginalEnrollmentControl(ctx, storageIntentName, storageIntentBytes)
	if err != nil {
		return storageEnrollmentPeakOriginalComparison{}, err
	}
	for slot, pages := range out.Hosts {
		for bank, page := range pages {
			start := (slot*2 + bank) * storageIntentPage
			if !bytes.Equal(page, hosts[start:start+storageIntentPage]) {
				return storageEnrollmentPeakOriginalComparison{}, errStorageCustody
			}
		}
	}
	return out, p.nativePermit(ctx)
}

func (p *storageEnrollmentEpoch) bindEnrollmentArmed(ctx context.Context, raw []byte) error {
	if p == nil || p.self != p || p.scope == nil || p.factualCensus == nil || p.candidateObserver == nil || p.evidence != nil {
		return storagebudget.ErrAuthority
	}
	e, err := p.decodeEnrollmentEvidence(raw)
	if err != nil || e.Phase != 1 || e.Scope != *p.scope || !reflect.DeepEqual(e.Inner.Catalog, p.candidateObserver.catalog) || !storageEnrollmentCensusEqual(storageEnrollmentFactualCensus(e.Census), *p.factualCensus) {
		return errors.Join(storagebudget.ErrBounds, err)
	}
	original, err := p.currentEnrollmentOriginal(ctx)
	if err != nil {
		return err
	}
	if err = storageEnrollmentPeakCompareOriginalOperands(e, original, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim, &p.operands); err != nil {
		return err
	}
	q, err := p.c.ref.cell.q.InspectEnrollment(ctx)
	if err != nil {
		return err
	}
	comparison, err := storageEnrollmentPeakCompareQOperands(e, q.Checkpoints, q.Tickets, storageEnrollmentPeakDispatch{Bridge: e.Bridge}, &p.operands)
	if err != nil || comparison.Stage != 0 || comparison.UnresolvedQAttempt {
		return errors.Join(errStorageEnrollmentUnresolved, err)
	}
	p.evidence = &e
	return nil
}

func (p *storageEnrollmentEpoch) readEnrollmentTarget(ctx context.Context, target byte) (raw []byte, err error) {
	if target < 1 || target > 8 {
		return nil, storagebudget.ErrBounds
	}
	if err = p.nativePermit(ctx); err != nil {
		return nil, err
	}
	if err = p.targetCurrent(ctx, target, true); err != nil {
		return nil, err
	}
	t := &p.targets[target-1]
	raw = make([]byte, int(t.final))
	count, err := t.named.file.ReadAt(raw, 0)
	if err != nil || count != len(raw) {
		return nil, errors.Join(storagebudget.ErrUnknown, err)
	}
	if err = p.targetCurrent(ctx, target, true); err != nil {
		return nil, err
	}
	return raw, p.nativePermit(ctx)
}

// Inspect only actual original native targets and the same-load original Q
// banks. Detached frames or a clean revision number cannot authorize a Q call.
func (p *storageEnrollmentEpoch) compareEnrollmentActual(ctx context.Context) (out storageEnrollmentPeakComparison, err error) {
	if p == nil || p.self != p || p.evidence == nil || p.scope == nil {
		return out, storagebudget.ErrAuthority
	}
	input := storageEnrollmentPeakComparisonInput{}
	input.Original, err = p.currentEnrollmentOriginal(ctx)
	if err != nil {
		return out, err
	}
	for bank := 0; bank < 2; bank++ {
		input.Catalogs[bank], err = p.readEnrollmentTarget(ctx, byte(5+bank))
		if err != nil {
			return out, err
		}
		input.Dispatch[bank], err = p.readEnrollmentTarget(ctx, byte(3+bank))
		if err != nil {
			return out, err
		}
	}
	outer, _, err := storageEnrollmentPeakDecodeCurrentPairOperands(input.Catalogs, *p.evidence, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim, &p.operands)
	if err != nil || outer.Kind != 2 || outer.Marker == nil {
		return out, errors.Join(errStorageEnrollmentUnresolved, err)
	}
	input.Evidence, err = p.readEnrollmentTarget(ctx, 1+outer.Marker.Bank)
	if err != nil {
		return out, err
	}
	if outer.Marker.Phase == 3 {
		input.ArmedEvidence = append([]byte{}, p.evidence.Raw...)
	}
	q, err := p.c.ref.cell.q.InspectEnrollment(ctx)
	if err != nil {
		return out, err
	}
	input.CurrentCheckpoints, input.CurrentTickets = q.Checkpoints, q.Tickets
	if p.previousEvidence != nil {
		input.PreviousEvidence = append([]byte{}, p.previousEvidence.Raw...)
	}
	if p.scope.path() != 1 && p.previousEvidence == nil {
		if !p.startup {
			return out, storagebudget.ErrAuthority
		}
		boundary := storageEnrollmentPeakCurrentBoundary{Catalogs: input.Catalogs}
		if p.anchoredStaged != nil {
			boundary.ReferencedStaged = append([]byte{}, p.anchoredStaged.Raw...)
		}
		if err = p.nativePermit(ctx); err != nil {
			return out, err
		}
		return storageEnrollmentPeakCompareOperands(input, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim, &boundary, &p.operands)
	}
	return storageEnrollmentPeakCompareOperands(input, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim, nil, &p.operands)
}

// A successful fresh arm publication followed by this direct call is the live
// effect-once boundary. A recovered consumed arm with actual-before is refused
// by compareEnrollmentActual and never enters this path. Each selected Q group
// is consumed once before invocation, including a complete-but-error outcome.
func (p *storageEnrollmentEpoch) dispatchEnrollmentQ(ctx context.Context) (err error) {
	if p == nil || p.self != p || p.evidence == nil || p.factualCensus == nil {
		return storagebudget.ErrAuthority
	}
	actual, err := p.compareEnrollmentActual(ctx)
	if err != nil || actual.Q.UnresolvedQAttempt {
		return errors.Join(errStorageEnrollmentUnresolved, err)
	}
	stage := actual.Q.Stage
	if stage == 3 {
		return nil
	}
	if stage < 0 || stage > 2 || actual.Dispatch.Step != byte(stage) || actual.Evidence.Phase != 1 {
		return storagebudget.ErrAuthority
	}
	if stage < 2 {
		// Both exact P copies must survive before the first two Q operations.
		for target := byte(5); target <= 6; target++ {
			raw, e := p.readEnrollmentTarget(ctx, target)
			if e != nil {
				return e
			}
			// Targets contain whole physical publications. Validate their exact
			// retained wrapper/lineage before decoding the comparison core with
			// the same original epoch context as the marker producer.
			core, e := p.enrollmentCore(raw)
			if e != nil {
				return errors.Join(errStorageEnrollmentUnresolved, e)
			}
			outer, e := p.enrollmentEnvelope(core, true)
			if e != nil || outer.Marker == nil || outer.Marker.Phase != 1 || storageEnrollmentPeakJoinMarker(outer, *p.evidence, p.original.Ready) != nil {
				return errors.Join(errStorageEnrollmentUnresolved, e)
			}
		}
	} else if actual.Envelope.Marker == nil || actual.Envelope.Marker.Phase != 2 {
		return storagebudget.ErrAuthority
	}
	e := p.evidence
	d := storageEnrollmentPeakDispatch{Generation: actual.Dispatch.Generation + 1, Installation: e.Installation, Binding: e.QBinding, Bridge: e.Bridge, Previous: sha256.Sum256(actual.Dispatch.Raw), Step: byte(stage + 1), Mask: [4]byte{0, 1, 3, 7}[stage+1], BeforeRevision: e.BeforeRevision + uint64(stage), BeforeEpoch: e.BeforeEpoch, Before: e.Checkpoint.Checkpoints[e.Checkpoint.Bank], After: e.Digests.Q[stage], Origin: e.Digests.Origin, Controls: [2]storageOwnerPersistentObject{e.Controls[2], e.Controls[3]}}
	if actual.Dispatch.Generation == ^uint64(0) {
		return storagebudget.ErrBounds
	}
	if stage > 0 {
		d.BeforeEpoch++
		d.Before = e.Digests.Q[stage-1]
	}
	raw, err := storageEnrollmentPeakEncodeDispatch(d, *e)
	if err != nil {
		return err
	}
	if err = p.publishEnrollmentControl(ctx, uint16(18+stage), raw); err != nil {
		return err
	}
	return p.invokeFreshEnrollmentQ(ctx, stage, raw)
}

// Only the immediately successful arm publication in this invocation calls this
// leaf. Recovered consumed-arm/actual-before paths never enter it.
func (p *storageEnrollmentEpoch) invokeFreshEnrollmentQ(ctx context.Context, stage int, raw []byte) (err error) {
	if p == nil || p.evidence == nil || stage < 0 || stage > 2 || p.factualCensus == nil {
		return storagebudget.ErrAuthority
	}
	e := p.evidence
	armed, err := p.compareEnrollmentActual(ctx)
	if err != nil || !armed.Q.UnresolvedQAttempt || armed.Q.Stage != stage || armed.Dispatch.Step != byte(stage+1) || !bytes.Equal(armed.Dispatch.Raw, raw) {
		return errors.Join(errStorageEnrollmentUnresolved, err)
	}
	if err = p.takeEnrollmentGroups(uint16(21+stage), uint16(56+stage)); err != nil {
		return err
	}
	if err = p.enterNative(ctx); err != nil {
		return err
	}
	// No retry, generic completion callback or compensation surrounds Q.
	switch stage {
	case 0:
		var epoch uint64
		epoch, err = p.c.ref.cell.q.BeginMaintenance(ctx, e.BeforeRevision)
		if err == nil && epoch != e.AfterEpoch {
			err = storagebudget.ErrStale
		}
	case 1:
		err = p.c.ref.cell.q.Register(ctx, e.BeforeRevision+1, e.Registration)
	case 2:
		err = p.c.ref.cell.q.PublishCensus(ctx, e.AfterEpoch, *p.factualCensus)
	}
	err = p.leaveNative(err)
	if err != nil {
		return err
	}
	confirmed, err := p.compareEnrollmentActual(ctx)
	if err != nil || confirmed.Q.UnresolvedQAttempt || confirmed.Q.Stage != stage+1 {
		return errors.Join(errStorageEnrollmentUnresolved, err)
	}
	if p.scope.path() != 1 {
		// Existing invocation-local fault seam, after the original Q call and
		// its complete confirmation. It never reissues an already spent arm.
		return p.successorBoundary([]string{"successor-q1", "successor-q2", "successor-q3"}[stage])
	}
	return nil
}
