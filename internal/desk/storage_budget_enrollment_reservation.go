package desk

import (
	"bytes"
	"context"
	"errors"
	"os"
	"reflect"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Pure room check. Its inputs are detached observations, and success issues no
// reservation or native authority. The original issuer below supplies the
// complete Q0, original mappings, fresh observations and exclusion separately.
func storageEnrollmentInitialRoom(s storageEnrollmentPeakScope, q *storageEnrollmentPeakQCheckpoint, observations []storagebudget.CapacityObservation, now time.Time) error {
	return storageEnrollmentInitialRoomVariant(s, q, observations, now, false)
}
func storageEnrollmentInitialRoomVariant(s storageEnrollmentPeakScope, q *storageEnrollmentPeakQCheckpoint, observations []storagebudget.CapacityObservation, now time.Time, composition bool) error {
	if _, err := storageEnrollmentPeakDecodeScopeVariant(s[:], composition); err != nil {
		return err
	}
	if (s.path() == 1 && s[321] != 0 || s.path() != 1 && s[321] != 15) || q == nil || storageEnrollmentPeakQValidateCheckpoint(q) != nil || q.Maintenance || now.IsZero() || len(observations) != int(s[326]) {
		return storagebudget.ErrBounds
	}
	for _, row := range q.Roots {
		if row.ID != (storageEnrollmentPeakQID{}) && !row.Known {
			return storagebudget.ErrUnknown
		}
	}
	for _, row := range q.Workspaces {
		if row.ID != (storageEnrollmentPeakQID{}) && !row.Known {
			return storagebudget.ErrUnknown
		}
	}
	// The issuer separately compares both actual ticket banks through the
	// strict original InspectEnrollment/ComparePrefix boundary, requiring every
	// nonempty ticket to be exactly persisted-applied before this room check.
	growth := storageEnrollmentPeakQVector{Bytes: storageEnrollmentPeakU64(s[360:368]), Files: storageEnrollmentPeakU64(s[368:376]), Directories: storageEnrollmentPeakU64(s[376:384])}
	control, err := storageEnrollmentPeakQAdd(q.Installation[storagebudget.Control], growth)
	if err != nil {
		return err
	}
	all, err := storageEnrollmentPeakQSum(q.Installation)
	if err != nil {
		return err
	}
	all, err = storageEnrollmentPeakQAdd(all, growth)
	if err != nil {
		return err
	}
	if !storageLimitsAdvisory && (!control.le(q.Policy.Installation.Category[storagebudget.Control]) || !all.le(q.Policy.Installation.Total)) {
		return storagebudget.ErrLimit
	}
	for i := 0; i < int(s[326]); i++ {
		raw := s[440+i*80 : 440+(i+1)*80]
		var id storagebudget.ID
		copy(id[:], raw[:16])
		generation := storageEnrollmentPeakU64(raw[16:24])
		var row *storageEnrollmentPeakQCapacityConstraint
		for j := range q.CapacityConstraints {
			if storagebudget.ID(q.CapacityConstraints[j].ID) == id {
				row = &q.CapacityConstraints[j]
			}
		}
		if row == nil || row.Generation != generation {
			return storagebudget.ErrStale
		}
		var observation *storagebudget.CapacityObservation
		for j := range observations {
			if observations[j].Constraint == id {
				if observation != nil {
					return storagebudget.ErrBounds
				}
				observation = &observations[j]
			}
		}
		if observation == nil || observation.Generation != generation || observation.KnownResources != row.Resources || observation.At.IsZero() || observation.At.After(now) || now.Sub(observation.At) > q.Policy.ObservationMaxAge {
			return storagebudget.ErrUnknown
		}
		if observation.Available.Bytes > 1<<60 || observation.Available.Inodes > 1<<40 || row.Resources&storagebudget.CapacityBytes == 0 && observation.Available.Bytes != 0 || row.Resources&storagebudget.CapacityInodes == 0 && observation.Available.Inodes != 0 {
			return storagebudget.ErrBounds
		}
		for _, axis := range []struct {
			enabled                       bool
			available, headroom, promised uint64
		}{
			{row.Resources&storagebudget.CapacityBytes != 0, observation.Available.Bytes, row.Headroom.Bytes, storageEnrollmentPeakU64(raw[32:40])},
			{row.Resources&storagebudget.CapacityInodes != 0, observation.Available.Inodes, row.Headroom.Inodes, storageEnrollmentPeakU64(raw[40:48])},
		} {
			if axis.enabled && (axis.available < axis.headroom || axis.available-axis.headroom < axis.promised) {
				return storagebudget.ErrHeadroom
			}
		}
	}
	return nil
}

// Supply observations from the actual original enrolled roots. This reuses the
// existing observer's native query/derivation, without opening another SH use
// lease or an ordinary capacity pass while this invocation owns use EX. The
// temporary pass here is data for derive only and is never exposed as authority.
func (p *storageEnrollmentEpoch) initialEnrollmentObservations(ctx context.Context, s storageEnrollmentPeakScope) ([]storagebudget.CapacityObservation, error) {
	wanted := make([]storagebudget.ID, 0, int(s[326]))
	for i := 0; i < int(s[326]); i++ {
		var id storagebudget.ID
		copy(id[:], s[440+i*80:456+i*80])
		wanted = append(wanted, id)
	}
	return p.originalEnrollmentObservations(ctx, wanted)
}

func (p *storageEnrollmentEpoch) originalEnrollmentObservations(ctx context.Context, ids []storagebudget.ID) ([]storagebudget.CapacityObservation, error) {
	if err := p.held(ctx); err != nil {
		return nil, err
	}
	o := p.c.owner
	a, ok := o.observer.(*storageAdvisoryObserver)
	if !ok || a.owner != o {
		// Startup recovery has not installed its observer yet. Its already
		// validated candidate may supply read-only samples for retained original
		// rows only; this neither installs it nor admits candidate definitions.
		if !p.startup || p.scope == nil || p.candidate == nil || p.candidate.installed || p.candidateObserver == nil || p.candidateObserver.owner != o || !storageEnrollmentOriginalObserverRows(p.c.ref.cell.ownerCatalog, p.candidateObserver.catalog) {
			return nil, storagebudget.ErrAuthority
		}
		a = p.candidateObserver
		for _, root := range o.orderedRoots() {
			row, exists := a.roots[root.row.ID]
			if !exists || storageOwnerRootValue(row.Definition) != root.row || row.Category != root.category || !reflect.DeepEqual(row.References, root.references) || row.Definition.Filesystem == (storagebudget.ID{}) || a.filesystems[row.Definition.Filesystem].Definition != o.filesystems[row.Definition.Filesystem] {
				return nil, storagebudget.ErrAuthority
			}
			for i := 0; i < int(root.row.CapacityCount); i++ {
				id := root.row.CapacityRefs[i].Constraint
				if a.constraints[id].Definition != o.constraints[id] {
					return nil, storagebudget.ErrAuthority
				}
			}
		}
	}
	pass := &storageCapacityPass{owner: o, samples: map[*os.Root]storageFilesystemSample{}, maxAge: a.maxAge}
	for _, root := range o.orderedRoots() {
		if err := root.current(ctx); err != nil {
			return nil, err
		}
		sample, err := a.query(ctx, root, root.root)
		if err != nil {
			return nil, err
		}
		if err = o.validateStorageSample(sample); err != nil {
			return nil, err
		}
		if sample.Filesystem != o.filesystems[root.row.Filesystem] || sample.RootGeneration != root.row.Generation || sample.CapacityCount != root.row.CapacityCount || sample.CapacityRefs != root.row.CapacityRefs {
			return nil, errStorageOwnerTransitionRequired
		}
		for _, row := range sample.Constraints {
			if row != o.constraints[row.ID] {
				return nil, errStorageOwnerTransitionRequired
			}
		}
		pass.samples[root.root] = sample
	}
	if err := pass.derive(a); err != nil {
		return nil, err
	}
	wanted := map[storagebudget.ID]bool{}
	for _, id := range ids {
		if id == (storagebudget.ID{}) || wanted[id] {
			return nil, storagebudget.ErrBounds
		}
		wanted[id] = true
	}
	byID := map[storagebudget.ID]storagebudget.CapacityObservation{}
	for _, sample := range pass.samples {
		for _, observation := range sample.Observations {
			if !wanted[observation.Constraint] {
				continue
			}
			if old, exists := byID[observation.Constraint]; exists {
				if old.Generation != observation.Generation || old.KnownResources != observation.KnownResources {
					return nil, errStorageOwnerTransitionRequired
				}
				observation.Available.Bytes = min(old.Available.Bytes, observation.Available.Bytes)
				observation.Available.Inodes = min(old.Available.Inodes, observation.Available.Inodes)
				if old.At.Before(observation.At) {
					observation.At = old.At
				}
			}
			byID[observation.Constraint] = observation
		}
	}
	out := make([]storagebudget.CapacityObservation, 0, len(wanted))
	for _, id := range ids {
		observation, present := byID[id]
		if !present {
			return nil, storagebudget.ErrUnknown
		}
		out = append(out, observation)
	}
	return out, p.held(ctx)
}

// The candidate was independently validated and mapped by
// prepareEnrollmentCandidate. Sampling it before installation additionally
// requires every original identity, reference, membership and object row to be
// retained exactly. Additional candidate rows confer no sampling authority.
func storageEnrollmentOriginalObserverRows(original, candidate storageOwnerCatalogV2) bool {
	if original.Installation != candidate.Installation || original.Policy != candidate.Policy || len(original.Roots) == 0 || !storageOwnerCatalogRetains(original, candidate) {
		return false
	}
	for _, row := range original.Roots {
		matches := 0
		for _, next := range candidate.Roots {
			if next.Definition.ID != row.Definition.ID {
				continue
			}
			matches++
			if row.Definition != next.Definition || row.Role != next.Role || row.Category != next.Category || !reflect.DeepEqual(row.References, next.References) {
				return false
			}
			// Catalog sorting may change dictionary indices. Existing semantic
			// material retains exact native objects, source, membership and controls.
			oldSource, e := original.sourceDigest(row)
			if e != nil {
				return false
			}
			newSource, e := candidate.sourceDigest(next)
			if e != nil || oldSource != newSource {
				return false
			}
			oldIdentity, e := original.rootIdentity(row)
			if e != nil {
				return false
			}
			newIdentity, e := candidate.rootIdentity(next)
			if e != nil || oldIdentity != newIdentity {
				return false
			}
		}
		if matches != 1 {
			return false
		}
	}
	return true
}

// Only this original issuer fills reservation.phase. The proposed Fence and
// prefix are untrusted bounded data until compared against this invocation's
// actual original capture. No effect runs here. Both fences must subsequently
// persist before any new name is created or any extent grows.
func (p *storageEnrollmentEpoch) admitInitialEnrollment(ctx context.Context, fenceRaw []byte, prefix storageEnrollmentPeakPrefix) error {
	if p == nil || p.self != p || p.scope != nil || p.original == nil || p.qualified == nil || p.reservation.phase != nil || p.planCursor != 0 {
		return storagebudget.ErrAuthority
	}
	if err := p.held(ctx); err != nil {
		return err
	}
	record, claim := *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim
	fence, err := p.enrollmentEnvelope(fenceRaw, true)
	if err != nil || fence.Kind != 0 || (p.previousEvidence == nil && (fence.Scope.path() != 1 || fence.Scope[321] != 0) || p.previousEvidence != nil && (fence.Scope.path() == 1 || fence.Scope[321] != 15)) || storageEnrollmentPeakU64(fence.Scope[1616:1624]) != p.generation {
		return errors.Join(storagebudget.ErrBounds, err)
	}
	s := fence.Scope
	if err = s.requireCapacityEstimate(); err != nil {
		return err
	}
	if !bytes.Equal(s[408:440], p.qualified.digest[:]) {
		return errStorageEnrollmentQualification
	}
	if err = p.qualified.current(ctx, p); err != nil {
		return err
	}
	for _, pair := range []struct {
		root   *os.Root
		target byte
	}{{p.c.original.config, 9}, {p.c.anchor, 10}} {
		native, e := storageOwnerPersistentRoot(ctx, pair.root)
		if e != nil || native != s.target(pair.target) {
			return errors.Join(errStorageCustody, e)
		}
	}
	input := storageEnrollmentPeakComparisonInput{Catalogs: [2][]byte{fenceRaw, fenceRaw}, Original: *p.original, CurrentCheckpoints: p.original.Checkpoints, CurrentTickets: p.original.Tickets, Prefix: prefix}
	if p.previousEvidence != nil {
		input.PreviousEvidence = append([]byte{}, p.previousEvidence.Raw...)
	}
	if err = storageEnrollmentPeakComparePrefix(input, fence, record, claim); err != nil {
		return err
	}
	var before storageOwnerCatalogV2
	if p.previousEvidence == nil {
		cores, _, e0 := p.originalCorePair(p.original.Catalogs, record, claim)
		if e0 != nil {
			return e0
		}
		pair, e0 := storageOwnerDecodeCatalogPairV3(cores[0], cores[1], record, claim)
		if e0 != nil {
			return e0
		}
		before = pair.High
	} else {
		before = p.previousEvidence.Inner.Catalog
	}
	var intended storageOwnerCatalogV2
	if err = storageEnrollmentPeakCatalogJSON(prefix.Catalog, &intended); err != nil {
		return err
	}
	var source *storageEnrollmentPeakSource
	if prefix.SourceKind == 9 || prefix.SourceKind == 10 || prefix.SourceKind == 11 {
		v, e := storageEnrollmentPeakDecodeSource(prefix.Source)
		if e != nil {
			return e
		}
		source = &v
	}
	if !storageEnrollmentRetainsOriginalWithDefault(before, intended, source) {
		return errStorageCustody
	}
	anchor := storageEnrollmentPeakQHeldAnchor{Installation: storageEnrollmentPeakQID(record.Installation), Binding: storageEnrollmentPeakQDigest(record.QBinding)}
	q0, err := storageEnrollmentPeakQDecodeCheckpoint(append([]byte{}, p.original.Checkpoints[s[325]]...), anchor)
	if err != nil {
		return err
	}
	observations, err := p.initialEnrollmentObservations(ctx, s)
	if err != nil {
		return err
	}
	if err = storageEnrollmentInitialRoomVariant(s, q0, observations, time.Now(), p.compositionLineage() != nil); err != nil {
		return err
	}
	if err = p.held(ctx); err != nil {
		return err
	}
	if err = p.admitScopeLaunch(ctx, s); err != nil {
		return err
	}
	p.scope = &s
	p.reservation = storageEnrollmentReservation{phase: p, scope: s}
	p.initialFence = append([]byte{}, fenceRaw...)
	for i := range p.targets {
		if p.targets[i].named.file == nil {
			continue
		}
		descriptor := s[1360+i*32 : 1360+(i+1)*32]
		p.targets[i].before = storageEnrollmentPeakU64(descriptor[4:12])
		p.targets[i].final = storageEnrollmentPeakU64(descriptor[12:20])
	}
	return p.prepareEnrollmentCandidate(ctx, intended)
}

// Enrollment is an append, not an owner/control/source transition. Reuse the
// existing retention comparison and add the complete observations that its
// ordinary move-friendly comparison intentionally permits to change.
func storageEnrollmentRetainsOriginal(before, after storageOwnerCatalogV2) bool {
	return storageEnrollmentRetainsOriginalWithDefault(before, after, nil)
}
func storageEnrollmentRetainsOriginalWithDefault(before, after storageOwnerCatalogV2, source *storageEnrollmentPeakSource) bool {
	if source != nil && source.Default != nil && source.Default.Workflow == 5 {
		return storageTransferSourceSelection(before, after, *source)
	}
	if source != nil && source.Default != nil && source.Default.Workflow == 4 {
		return storageTransferNamespaceCatalogRetains(before, after, *source)
	}
	if !storageOwnerCatalogRetains(before, after) || !storageEnrollmentOriginalOwnerEqual(before, after, source) || before.Policy != after.Policy {
		return false
	}
	if len(before.OwnerControls) != len(after.OwnerControls) {
		if source == nil || source.Default == nil || source.Default.Workflow != 1 && source.Default.Workflow != 3 || len(after.OwnerControls) != len(before.OwnerControls)+1 || !storageEnrollmentDefaultHistorySourceJoins(*source, after) {
			return false
		}
		d := source.Default
		found := 0
		for _, control := range after.OwnerControls {
			if control.Role != 2 || after.Objects[control.ParentObject].Persistent != d.Settings || string(control.Name) != dataLocationName {
				continue
			}
			for _, old := range before.OwnerControls {
				if before.Objects[old.ParentObject].Persistent == d.Settings && bytes.Equal(old.Name, control.Name) {
					return false
				}
			}
			if !bytes.Equal(control.Location.Path, d.SelectedPath) || len(control.Location.Previous) != 0 || control.Location.Selected != d.Selected {
				return false
			}
			found++
		}
		if found != 1 {
			return false
		}
	}
	for _, old := range before.OwnerControls {
		matched := false
		for _, next := range after.OwnerControls {
			if before.Objects[old.ParentObject].Persistent == after.Objects[next.ParentObject].Persistent && bytes.Equal(old.Name, next.Name) {
				a, e := before.controlDigest(old)
				b, f := after.controlDigest(next)
				if e != nil || f != nil || a != b || old.MaxBytes != next.MaxBytes || old.RelationDigest != next.RelationDigest {
					return false
				}
				matched = true
			}
		}
		if !matched {
			return false
		}
	}
	for _, old := range before.Roots {
		matched := false
		for _, next := range after.Roots {
			if old.Definition.ID != next.Definition.ID {
				continue
			}
			a, e := before.sourceDigest(old)
			b, f := after.sourceDigest(next)
			if e != nil || f != nil || a != b || old.Definition != next.Definition || old.Role != next.Role || old.Category != next.Category {
				return false
			}
			matched = true
		}
		if !matched {
			return false
		}
	}
	for _, old := range before.Profiles {
		matched := false
		for _, next := range after.Profiles {
			if old.ID == next.ID {
				if old != next {
					return false
				}
				matched = true
			}
		}
		if !matched {
			return false
		}
	}
	for _, old := range before.Constraints {
		matched := false
		for _, next := range after.Constraints {
			if old.Definition.ID == next.Definition.ID {
				if !reflect.DeepEqual(old, next) {
					return false
				}
				matched = true
			}
		}
		if !matched {
			return false
		}
	}
	for _, old := range before.Filesystems {
		matched := false
		for _, next := range after.Filesystems {
			if old.Definition.ID == next.Definition.ID {
				if !reflect.DeepEqual(old, next) {
					return false
				}
				matched = true
			}
		}
		if !matched {
			return false
		}
	}
	return true
}
