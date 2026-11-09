package desk

import (
	"bytes"
	"context"
	"errors"
	"os"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// The no-new-name interval has one bounded forward pass: six original-target
// persistence operations, then identical Fence publication in both originals.
// Neither a first Fence nor an in-memory reservation permits new namespace.
func (p *storageEnrollmentEpoch) persistInitialEnrollmentFences(ctx context.Context) error {
	if p == nil || p.self != p || p.scope == nil || (p.scope.path() == 1 && p.scope[321] != 0 || p.scope.path() != 1 && p.scope[321] != 15) || p.original == nil || p.fenced || len(p.initialFence) != storageOwnerCatalogBytes {
		return storagebudget.ErrAuthority
	}
	if err := p.nativePermit(ctx); err != nil {
		return err
	}
	first := byte(5)
	groups := []uint16{60, 61}
	if p.scope.path() != 1 {
		first = 1
		groups = []uint16{9, 10}
	}
	for target := first; target <= 10; target++ {
		if err := p.persistEnrollmentTarget(ctx, target); err != nil {
			return err
		}
	}
	for _, group := range groups {
		if err := p.publishEnrollmentControl(ctx, group, p.initialFence); err != nil {
			return err
		}
		if group == 9 {
			if err := p.successorBoundary("successor-one-fence"); err != nil {
				return err
			}
		}
	}
	// Read both actual targets after the complete second publication. This
	// checks a native-bound original pair; detached identical bytes alone are
	// never the live two-Fence barrier.
	for target := byte(5); target <= 6; target++ {
		if err := p.targetCurrent(ctx, target, true); err != nil {
			return err
		}
		raw := make([]byte, storageOwnerCatalogBytes)
		count, err := p.targets[target-1].named.file.ReadAt(raw, 0)
		if err == nil && count == len(raw) {
			raw, err = p.enrollmentCore(raw)
		}
		if err != nil || !bytes.Equal(raw, p.initialFence) {
			return errors.Join(errStorageEnrollmentUnresolved, err)
		}
	}
	if err := p.held(ctx); err != nil {
		return err
	}
	p.fenced = true
	if p.scope.path() != 1 {
		return p.successorBoundary("successor-two-fence")
	}
	return nil
}

func storageEnrollmentCaptureScope(before storageEnrollmentPeakScope, targets [4]storageOwnerPersistentObject) (storageEnrollmentPeakScope, error) {
	return storageEnrollmentCaptureScopeVariant(before, targets, false)
}
func storageEnrollmentCaptureScopeVariant(before storageEnrollmentPeakScope, targets [4]storageOwnerPersistentObject, composition bool) (storageEnrollmentPeakScope, error) {
	if _, err := storageEnrollmentPeakDecodeScopeVariant(before[:], composition); err != nil {
		return storageEnrollmentPeakScope{}, err
	}
	if before.path() != 1 || before[321] != 0 {
		return storageEnrollmentPeakScope{}, storagebudget.ErrBounds
	}
	after := before
	after[321] = 15
	for i, target := range targets {
		copy(after[1080+i*35:1080+(i+1)*35], storageEnrollmentPeakNativeBytes(target))
	}
	if _, err := storageEnrollmentPeakDecodeScopeVariant(after[:], composition); err != nil {
		return storageEnrollmentPeakScope{}, err
	}
	if !storageEnrollmentPeakCaptureMatches(before, after) {
		return storageEnrollmentPeakScope{}, storagebudget.ErrBounds
	}
	return after, nil
}

// Only the live original issuer can create initial controls. Every returned
// O_EXCL descriptor is retained by this same executor until finish. A failure
// never opens the next name, adopts a preexisting name or unlinks a partial one.
// Across process death before CapturedScope, these effects remain the accepted
// unresolved initial-capture cut, with full P retained by the Fence.
func (p *storageEnrollmentEpoch) createInitialEnrollmentControls(ctx context.Context) error {
	if p == nil || p.self != p || !p.fenced || p.scope == nil || p.scope.path() != 1 || p.scope[321] != 0 || p.original == nil {
		return storagebudget.ErrAuthority
	}
	var captured [4]storageOwnerPersistentObject
	for target := byte(1); target <= 4; target++ {
		if err := p.createInitialEnrollmentControl(ctx, target); err != nil {
			return err
		}
		captured[target-1] = p.targets[target-1].native
	}
	scope, err := storageEnrollmentCaptureScopeVariant(*p.scope, captured, p.compositionLineage() != nil)
	if err != nil {
		return err
	}
	record, claim := *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim
	fence, err := storageEnrollmentPeakDecodeEnvelopeVariant(p.initialFence, record, claim, true, p.compositionLineage() != nil)
	if err != nil || fence.Sequence == ^uint64(0) {
		return errors.Join(storagebudget.ErrBounds, err)
	}
	fence.Kind, fence.Scope, fence.Sequence = 1, scope, fence.Sequence+1
	raw, err := storageEnrollmentPeakEncodeEnvelope(fence, record, claim)
	if err != nil {
		return err
	}
	// Reservation still binds the pre-create Scope and permits only this exact
	// four-native capture relation. No other Scope byte or allowance changes.
	p.scope = &scope
	if err = p.publishEnrollmentControl(ctx, 66, raw); err != nil {
		return err
	}
	for target := byte(1); target <= 4; target++ {
		if err = p.persistEnrollmentTarget(ctx, target); err != nil {
			return err
		}
	}
	return p.held(ctx)
}

func (p *storageEnrollmentEpoch) createInitialEnrollmentControl(ctx context.Context, target byte) (err error) {
	if p == nil || p.self != p || !p.fenced || p.scope == nil || p.scope.path() != 1 || p.scope[321] != 0 || target < 1 || target > 4 || p.targets[target-1].named.file != nil {
		return storagebudget.ErrAuthority
	}
	group := uint16(61 + target)
	if err = p.takeEnrollmentGroups(group, group+20); err != nil {
		return err
	}
	if err = p.enterNative(ctx); err != nil {
		return err
	}
	defer func() { err = p.leaveNative(err) }()
	if err = p.boundary("initial-control-create"); err != nil {
		return err
	}
	if err = p.permit(ctx); err != nil {
		return err
	}
	name := storageEnrollmentNativeName(target)
	f, err := p.c.original.config.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR|openNoFollow, custodyFileMode)
	if err != nil {
		return err
	}
	t := &p.targets[target-1]
	// Install the actual returned descriptor before any fallible observation.
	// finish owns its Close even if Stat/native/Sync subsequently fails.
	t.named = storageNamedFile{root: p.c.original.config, name: name, file: f, size: 0}
	t.before, t.final, t.created = 0, storageEnrollmentPeakU64(p.scope[1360+int(target-1)*32+12:]), true
	info, err := f.Stat()
	if err != nil || !storagePrivate(info, false) || info.Size() != 0 {
		return errors.Join(errStorageCustody, err)
	}
	t.named.identity = info
	if err = t.named.current(); err != nil {
		return err
	}
	t.native, err = storageOwnerPersistentFile(ctx, t.named)
	if err != nil {
		return err
	}
	if err = p.boundary("initial-control-captured"); err != nil {
		return err
	}
	if err = p.permit(ctx); err != nil {
		return err
	}
	if err = storageEnrollmentNativeSync(ctx, f); err != nil {
		return err
	}
	parent, err := p.c.original.config.Open(".")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, parent.Close()) }()
	native, err := storageAdvisoryPersistentHeld(ctx, parent)
	if err != nil || native != p.scope.target(9) {
		return errors.Join(errStorageCustody, err)
	}
	if err = p.boundary("initial-control-parent-sync"); err != nil {
		return err
	}
	if err = p.permit(ctx); err != nil {
		return err
	}
	if err = storageEnrollmentNativeSync(ctx, parent); err != nil {
		return err
	}
	if err = t.named.current(); err != nil {
		return err
	}
	actual, err := storageOwnerPersistentFile(ctx, t.named)
	if err != nil || actual != t.native {
		return errors.Join(errStorageCustody, err)
	}
	return p.permit(ctx)
}
