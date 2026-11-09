package desk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"reflect"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// This capture borrows only the actual original construction's fixed controls.
// It performs no repair, Sync, create, publication, census or Q mutation. The
// caller must already own the complete enrollment holds. Observations alone do
// not install Scope or reserve its peak margin.
func (p *storageEnrollmentEpoch) captureOriginalControls(ctx context.Context) (err error) {
	if p == nil || p.self != p || p.original != nil || p.scope != nil {
		return storagebudget.ErrAuthority
	}
	if err = p.held(ctx); err != nil {
		return err
	}
	observed := &storageEnrollmentPeakOriginalComparison{}
	observed.Project, err = storageOwnerPersistentRoot(ctx, p.originalProjectRoot())
	if err != nil {
		return err
	}
	for target := byte(5); target <= 8; target++ {
		if p.targets[target-1].named.file != nil {
			return storagebudget.ErrAuthority
		}
		name := storageEnrollmentNativeName(target)
		var n storageNamedFile
		if target < 7 {
			n, err = storageOpenOriginalControl(p.c.ref.cell, name)
		} else {
			n, err = p.openEnrollmentCheckpoint(ctx, name)
		}
		if err != nil {
			return err
		}
		// Transfer immediately, so the synchronous executor closes the actual
		// descriptor even if native observation or a later check fails.
		t := &p.targets[target-1]
		t.named, t.before, t.final = n, uint64(n.size), uint64(n.size)
		t.native, err = storageOwnerPersistentFile(ctx, n)
		if err != nil {
			return err
		}
		observed.Targets[target-1], observed.Extents[target-1] = t.native, uint64(n.size)
		if target <= 6 {
			raw := make([]byte, storageOwnerCatalogBytes)
			count, e := n.file.ReadAt(raw, 0)
			if e != nil || count != len(raw) {
				return errors.Join(storagebudget.ErrUnknown, e)
			}
			observed.Catalogs[target-5] = raw
		}
		if err = n.current(); err != nil {
			return err
		}
	}
	if p.c.ref.cell.ownerRecord == nil {
		return storagebudget.ErrAuthority
	}
	// Strict same-load Q inspection retains both raw peers and every original
	// ticket/applied/next row. It never incorporates an unapplied terminal.
	var q storagebudget.EnrollmentInspection
	if p.identityPrefixes {
		q, err = p.c.ref.cell.q.InspectAction(ctx)
	} else {
		q, err = p.c.ref.cell.q.InspectEnrollment(ctx)
	}
	if err != nil || q.View.Configuration.Maintenance {
		return errors.Join(errStorageEnrollmentUnresolved, err)
	}
	observed.Checkpoints, observed.Tickets = q.Checkpoints, q.Tickets
	for bank := range q.Checkpoints {
		actual := make([]byte, storagebudget.CheckpointBytes)
		n := p.targets[6+bank].named
		count, e := n.file.ReadAt(actual, 0)
		if e != nil || count != len(actual) || !bytes.Equal(actual, q.Checkpoints[bank]) {
			return errors.Join(errStorageCustody, e)
		}
	}
	hosts, err := p.readOriginalEnrollmentControl(ctx, storageIntentName, storageIntentBytes)
	if err != nil {
		return err
	}
	for slot := range observed.Hosts {
		for bank := range observed.Hosts[slot] {
			start := (slot*2 + bank) * storageIntentPage
			observed.Hosts[slot][bank] = append([]byte{}, hosts[start:start+storageIntentPage]...)
		}
		if _, err = storageSelectCreationHost(observed.Hosts[slot][0], observed.Hosts[slot][1], slot, p.c.ref.cell.record.Installation, p.c.ref.cell.binding); err != nil {
			return err
		}
	}
	ready, err := p.readOriginalEnrollmentControl(ctx, storageReadyName, 32)
	if err != nil || !bytes.Equal(ready, p.c.ref.cell.binding[:]) {
		return errors.Join(errStorageCustody, err)
	}
	// Evidence Ready uses the complete original ready bytes' ordinary digest,
	// as the existing codec's original comparison contract specifies.
	observed.Ready = sha256.Sum256(ready)
	if err = p.held(ctx); err != nil {
		return err
	}
	p.original = observed
	p.initialInspection = &q
	return nil
}

func (p *storageEnrollmentEpoch) readOriginalEnrollmentControl(ctx context.Context, name string, length int64) (raw []byte, err error) {
	if name != storageIntentName && name != storageReadyName {
		return nil, storagebudget.ErrAuthority
	}
	if err = p.held(ctx); err != nil {
		return nil, err
	}
	if name == storageReadyName {
		// Ready has always been a lexical binding, not an immutable inode in
		// original.files. Borrow a fresh original-parent description and retain
		// its exact bytes only; never invent an old Ready inode witness.
		if length != 32 {
			return nil, storagebudget.ErrBounds
		}
		value, n, e := storageOwnerReadFixed(p.c.original.config, name, 32)
		if e != nil {
			return nil, e
		}
		e = errors.Join(n.current(), n.file.Close())
		if e != nil || !bytes.Equal(value, p.c.ref.cell.binding[:]) {
			return nil, errors.Join(errStorageCustody, e)
		}
		return value, p.held(ctx)
	}
	n, err := storageOpenOriginalControl(p.c.ref.cell, name)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, n.current(), n.file.Close())
		if err != nil {
			raw = nil
		}
	}()
	if n.size != length || length != 32 && length != storageIntentBytes {
		return nil, errStorageCustody
	}
	raw = make([]byte, int(length))
	count, err := n.file.ReadAt(raw, 0)
	if err != nil || count != len(raw) {
		return nil, errors.Join(storagebudget.ErrUnknown, err)
	}
	return raw, p.held(ctx)
}

func (p *storageEnrollmentEpoch) captureInitialOriginal(ctx context.Context) (err error) {
	if p == nil || p.self != p || p.original != nil {
		return storagebudget.ErrAuthority
	}
	defer func() {
		if err != nil {
			p.original = nil
			p.initialInspection = nil
		}
	}()
	if err = p.captureOriginalControls(ctx); err != nil {
		return err
	}
	cores, _, err := p.originalCorePair(p.original.Catalogs, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
	if err != nil {
		return err
	}
	pair, err := storageOwnerDecodeCatalogPairV3(cores[0], cores[1], *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
	if err != nil || pair.High.Phase != "active" {
		return errors.Join(errStorageEnrollmentUnresolved, err)
	}
	for _, name := range []string{"storage-enrollment.a", "storage-enrollment.b", "storage-enrollment-step.a", "storage-enrollment-step.b"} {
		if _, err := p.c.original.config.Lstat(name); !errors.Is(err, os.ErrNotExist) {
			return errors.Join(errStorageEnrollmentUnresolved, err)
		}
	}
	return p.held(ctx)
}

func (p *storageEnrollmentEpoch) captureExistingOriginal(ctx context.Context) error {
	return p.captureExistingOriginalForRetired(ctx, nil)
}
func (p *storageEnrollmentEpoch) captureExistingOriginalForRetired(ctx context.Context, pending *storageCreationIntent) error {
	if err := p.captureOriginalControls(ctx); err != nil {
		return err
	}
	return p.captureExistingOriginalMetadata(ctx, pending)
}
func (p *storageEnrollmentEpoch) captureExistingOriginalMetadata(ctx context.Context, pending *storageCreationIntent) error {
	if p.original == nil || p.initialInspection == nil || p.previousEvidence != nil {
		return storagebudget.ErrAuthority
	}
	record, claim := *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim
	catalogs := p.original.Catalogs
	if p.identityPrefixes && p.compositionEntry != nil && p.compositionCut < 4 {
		catalogs = p.compositionEntry.Original
	}
	cores, _, err := p.identityCorePair(catalogs, record, claim)
	if err != nil {
		return err
	}
	var retained *storageCompositionLineage
	native := storagePolicyActualPair(catalogs)
	wrapped := string(catalogs[0][:8]) == "DSKONL1\x00"
	if native {
		retained = p.compositionLineage()
		if retained == nil {
			return errStorageEnrollmentUnresolved
		}
	}
	if wrapped {
		pair, e0 := storageCompositionReadPair(catalogs[0], catalogs[1], record, claim)
		if e0 != nil || pair.RecoveryOnly && (!p.identityPrefixes || !storageIdentityPreparedLineage(pair)) {
			return storagebudget.ErrBounds
		}
		lineage := pair.Banks[pair.Selected].Lineage
		retained = &lineage
	}
	outer, _, err := storageEnrollmentPeakDecodePairVariant(cores[0], cores[1], record, claim, wrapped || native)
	if err != nil || outer.Marker == nil || outer.Marker.Phase != 3 || !bytes.Equal(cores[0], cores[1]) || !outer.Scope.hasOriginalPeer() {
		return errors.Join(errStorageEnrollmentUnresolved, err)
	}
	if err = outer.Scope.requireCapacityEstimate(); err != nil {
		return err
	}
	for target := byte(1); target <= 4; target++ {
		name := storageEnrollmentNativeName(target)
		info, err := p.c.original.config.Lstat(name)
		if err != nil {
			return err
		}
		length := int64(4096)
		if target <= 2 {
			length = int64(outer.Scope.final(target - 1))
		}
		if info.Size() != length {
			return errStorageCustody
		}
		n, err := storageOpenFile(p.c.original.config, name, length)
		if err != nil {
			return err
		}
		t := &p.targets[target-1]
		t.named = n
		t.before = uint64(length)
		t.final = uint64(length)
		t.native, err = storageOwnerPersistentFile(ctx, n)
		if err != nil || t.native != outer.Scope.target(target) {
			return errors.Join(errStorageCustody, err)
		}
		p.original.Targets[target-1] = t.native
		p.original.Extents[target-1] = uint64(length)
	}
	selected := p.targets[outer.Marker.Bank].named
	raw := make([]byte, int(selected.size))
	n, err := selected.file.ReadAt(raw, 0)
	if err != nil || n != len(raw) {
		return errors.Join(storagebudget.ErrUnknown, err)
	}
	e, err := storageEnrollmentPeakDecodeEvidenceWithLineage(raw, retained, record, claim)
	if err != nil || e.Phase != 2 || storageEnrollmentPeakJoinMarker(outer, e, p.original.Ready) != nil {
		return errors.Join(errStorageCustody, err)
	}
	peer := p.targets[1-e.Bank].named
	peerRaw := make([]byte, int(peer.size))
	n, err = peer.file.ReadAt(peerRaw, 0)
	if err != nil || n != len(peerRaw) {
		return errors.Join(storagebudget.ErrUnknown, err)
	}
	armed, err := storageEnrollmentPeakDecodeEvidenceWithLineage(peerRaw, retained, record, claim)
	if err != nil || !storageEnrollmentPeakJoinArmed(e, armed) {
		return errors.Join(errStorageCustody, err)
	}
	p.c.ref.cell.mu.Lock()
	endpoint := p.c.ref.cell.enrollmentEndpoint
	catalog := p.c.ref.cell.ownerCatalog
	p.c.ref.cell.mu.Unlock()
	if endpoint == nil || !bytes.Equal(endpoint.Raw, e.Raw) || !reflect.DeepEqual(catalog, e.Inner.Catalog) || !native && (p.initialInspection.View.Configuration.Epoch != e.AfterEpoch || p.initialInspection.View.Configuration.Revision < e.AfterRevision) {
		return errStorageCustody
	}
	if native {
		state, err := p.c.owner.readOriginalPolicy(ctx)
		if err != nil || !bytes.Equal(state.Actual[0], catalogs[0]) || !bytes.Equal(state.Actual[1], catalogs[1]) {
			return errors.Join(errStorageCustody, err)
		}
		if err = p.originalPolicyCurrentQ(ctx, state, *p.initialInspection); err != nil {
			return err
		}
	} else {
		_, after, err := storageOwnerActualDefinitions(p.initialInspection.View, e.Inner.Catalog)
		if err != nil || !after {
			return errors.Join(errStorageCustody, err)
		}
	}
	table := storageIntentTable{owner: p.c.owner}
	for slot, pages := range p.original.Hosts {
		choice, err := storageSelectCreationHost(pages[0], pages[1], slot, record.Installation, record.QBinding)
		if err != nil {
			return err
		}
		table.slots[slot] = choice.Slot
		table.identity[slot] = choice.Identity
		table.creation[slot] = choice.Creation
		if choice.Creation != nil && choice.Creation.Kind != storageCreationLocalPrivate && choice.Creation.Core.Phase == storageIntentRetired && !storageCreationTailIncluded(*choice.Creation, e) && !storageCreationRecordEqual(choice.Creation, pending) {
			return errStorageEnrollmentUnresolved
		}
		if choice.PredecessorCreation != nil && choice.PredecessorCreation.Kind != storageCreationLocalPrivate && choice.PredecessorCreation.Core.Phase == storageIntentRetired && !storageCreationTailIncluded(*choice.PredecessorCreation, e) {
			return errStorageEnrollmentUnresolved
		}
		if !table.slots[slot].record.reusable() && (!p.identityPrefixes || choice.Identity == nil && (choice.Creation == nil || choice.Creation.Kind != storageCreationLocalPrivate)) {
			return storagebudget.ErrBusy
		}
	}
	if err = p.c.owner.validateIntentMappings(ctx, &table, storageHostState(p.initialInspection.View)); err != nil {
		return err
	}
	p.previousEvidence = &e
	return p.held(ctx)
}

// The complete early-cut tuple is comparison evidence only. Bytes live in the
// unchanged originals; hashing never substitutes for their availability.
func storageEnrollmentSuccessorInputs(s storageEnrollmentPeakScope, original storageEnrollmentPeakOriginalComparison, prior []byte, ready, oldStep3 []byte) (storagebudget.Digest, error) {
	return storageEnrollmentSuccessorInputsTuple(s, original, prior, ready, oldStep3, nil, [2][]byte{}, false)
}

func storageEnrollmentSuccessorCompositionInputs(s storageEnrollmentPeakScope, original storageEnrollmentPeakOriginalComparison, prior []byte, ready, oldStep3 []byte, projection storageCompositionProjection, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storagebudget.Digest, error) {
	pair, err := storageCompositionReadPair(original.Catalogs[0], original.Catalogs[1], record, claim)
	actual, err2 := storageCompositionCaptureProjection(original.Catalogs[0], original.Catalogs[1], record, claim)
	if err != nil || err2 != nil || actual != projection || pair.RecoveryOnly || pair.Selected != s[322] {
		return storagebudget.Digest{}, storagebudget.ErrBounds
	}
	cores := [2][]byte{pair.Banks[0].Core, pair.Banks[1].Core}
	return storageEnrollmentSuccessorInputsTuple(s, original, prior, ready, oldStep3, &projection, cores, false)
}

func storageEnrollmentSuccessorInputsTuple(s storageEnrollmentPeakScope, original storageEnrollmentPeakOriginalComparison, prior []byte, ready, oldStep3 []byte, physical *storageCompositionProjection, cores [2][]byte, native bool) (storagebudget.Digest, error) {
	var zero storagebudget.Digest
	if _, err := storageEnrollmentPeakDecodeScopeVariant(s[:], physical != nil || native); err != nil {
		return zero, err
	}
	if s.path() == 1 || s[323] > 1 || len(prior) != int(storageEnrollmentPeakU64(s[328+8*int(s[323]):336+8*int(s[323])])) || len(ready) != 32 || len(oldStep3) != 4096 {
		return zero, storagebudget.ErrBounds
	}
	h := sha256.New()
	if native {
		h.Write([]byte("desk-successor-fence-original-inputs-v3\x00"))
	} else if physical == nil {
		h.Write([]byte("desk-successor-fence-original-inputs-v1\x00"))
	} else {
		h.Write([]byte("desk-successor-fence-original-inputs-v2\x00"))
	}
	h.Write([]byte{1, 0})
	h.Write(s[:])
	for _, raw := range original.Catalogs {
		if len(raw) != 8388608 {
			return zero, storagebudget.ErrBounds
		}
		h.Write(raw)
	}
	var n [8]byte
	storageEnrollmentPeakPut64(n[:], uint64(len(prior)))
	h.Write(n[:])
	h.Write(prior)
	h.Write(ready)
	for _, raw := range original.Checkpoints {
		if len(raw) != 4194304 {
			return zero, storagebudget.ErrBounds
		}
		h.Write(raw)
	}
	for _, raw := range original.Tickets {
		if len(raw) != 1048576 {
			return zero, storagebudget.ErrBounds
		}
		h.Write(raw)
	}
	for _, pair := range original.Hosts {
		for _, raw := range pair {
			if len(raw) != 16384 {
				return zero, storagebudget.ErrBounds
			}
			h.Write(raw)
		}
	}
	h.Write(oldStep3)
	if physical != nil {
		for _, raw := range cores {
			if len(raw) != storageOwnerCatalogBytes {
				return zero, storagebudget.ErrBounds
			}
			h.Write(raw)
		}
		h.Write(physical[:])
	}
	var out storagebudget.Digest
	copy(out[:], h.Sum(nil))
	return out, nil
}

func storageEnrollmentSuccessorOriginFlags(original storageEnrollmentPeakOriginalComparison, prior storageEnrollmentPeakEvidence, candidate storagebudget.ID, record storageOwnerRecordV2) (byte, error) {
	flags := byte(0)
	retainedOrigins := make(map[storagebudget.ID]bool)
	for slot, pair := range original.Hosts {
		choice, err := storageSelectCreationHost(pair[0], pair[1], slot, record.Installation, record.QBinding)
		if err != nil {
			return 0, err
		}
		if choice.Slot.record.Phase == storageIntentEmpty {
			continue
		}
		if flags == 0 {
			flags = 1
		}
		retained := false
		for _, old := range prior.Witnesses {
			if old.Selected && int(old.Slot) == slot && bytes.Equal(old.Page, choice.Slot.page) {
				if int(old.Origin) >= len(prior.Board.Origins) {
					return 0, storagebudget.ErrBounds
				}
				retained = true
				retainedOrigins[prior.Board.Origins[old.Origin].ID] = true
			}
		}
		if !retained {
			flags = 3
		}
	}
	if (flags == 0) != (candidate == (storagebudget.ID{})) || flags == 3 && retainedOrigins[candidate] {
		return 0, storagebudget.ErrBounds
	}
	return flags, nil
}

// The native variant commits the complete ACTUAL pair under a separate closed
// interpretation. The restored E1 core is comparison material, never substituted
// into original physical inputs or used as a currentness/native permit.
func storageEnrollmentSuccessorNativeInputs(s storageEnrollmentPeakScope, original storageEnrollmentPeakOriginalComparison, prior, ready, oldStep3 []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storagebudget.Digest, error) {
	if s[322] > 1 || !storagePolicyActualPair(original.Catalogs) {
		return storagebudget.Digest{}, storagebudget.ErrBounds
	}
	v, err := storageOwnerV6NativeReadEndpoint(original.Catalogs[s[322]], prior, record, claim)
	if err != nil {
		return storagebudget.Digest{}, err
	}
	e, err := storageEnrollmentPeakDecodeEvidenceWithLineage(prior, &v.Stage.Lineage, record, claim)
	if err != nil {
		return storagebudget.Digest{}, err
	}
	capsule, core, _, _, _, err := storageOwnerV6CaptureBefore(original.Catalogs, s[322], prior, e.Inner.Catalog, record, claim)
	if err != nil || !bytes.Equal(s[88:120], capsule.Full[capsule.Selected][:]) || storageEnrollmentPeakU64(s[1624:1632]) != storageEnrollmentPeakU64(core[16:24]) {
		return storagebudget.Digest{}, storagebudget.ErrBounds
	}
	return storageEnrollmentSuccessorInputsTuple(s, original, prior, ready, oldStep3, nil, [2][]byte{}, true)
}
