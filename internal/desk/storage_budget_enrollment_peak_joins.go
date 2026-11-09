package desk

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"reflect"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Prefix inputs are the already planned original DTO/sections, before any new
// evidence/native capture exists. They reuse the accepted encodings; they are
// detached comparison data, never a substitute for live exclusion or custody.
type storageEnrollmentPeakPrefix struct {
	Catalog, Projection, Tickets, Views, Checkpoint []byte
	TicketCount, ViewCount                          uint32
	SourceKind                                      uint8
	Source                                          []byte
	FreshOrigin                                     storagebudget.ID
	OriginalStep3                                   []byte
}

func storageEnrollmentPeakComparePrefix(input storageEnrollmentPeakComparisonInput, outer storageEnrollmentPeakEnvelope, record storageOwnerRecordV2, claim storageOwnerClaimV2) error {
	if outer.Kind == 2 || outer.Snapshot == nil || input.Original.Ready == (storagebudget.Digest{}) || input.Original.Project != storageEnrollmentPeakNative(outer.Scope[286:321]) {
		return storagebudget.ErrBounds
	}
	s := outer.Scope
	p := input.Prefix
	physicalOriginal := input.Original
	var retained *storageCompositionLineage
	native := outer.Snapshot.Encoding == 6
	wrapped := outer.Snapshot.Encoding == 4
	if native {
		if s[322] > 1 || !storagePolicyActualPair(input.Original.Catalogs) {
			return storagebudget.ErrBounds
		}
		v, err := storageOwnerV6NativeReadEndpoint(input.Original.Catalogs[s[322]], input.PreviousEvidence, record, claim)
		if err != nil {
			return err
		}
		lineage := v.Stage.Lineage
		retained = &lineage
		e, err := storageEnrollmentPeakDecodeEvidenceWithLineage(input.PreviousEvidence, retained, record, claim)
		if err != nil {
			return err
		}
		_, core, _, _, _, err := storageOwnerV6CaptureBefore(input.Original.Catalogs, s[322], input.PreviousEvidence, e.Inner.Catalog, record, claim)
		if err != nil || !bytes.Equal(core, outer.Snapshot.Before) {
			return storagebudget.ErrBounds
		}
		input.Original.Catalogs = [2][]byte{core, append([]byte{}, core...)}
	}
	if wrapped {
		pair, err := storageCompositionReadPair(input.Original.Catalogs[0], input.Original.Catalogs[1], record, claim)
		projection, e0 := storageCompositionCaptureProjection(input.Original.Catalogs[0], input.Original.Catalogs[1], record, claim)
		if err != nil || e0 != nil || projection != outer.Snapshot.Physical || pair.RecoveryOnly || pair.Selected != s[322] || !bytes.Equal(pair.Banks[pair.Selected].Core, outer.Snapshot.Before) {
			return storagebudget.ErrBounds
		}
		lineage := pair.Banks[pair.Selected].Lineage
		retained = &lineage
		input.Original.Catalogs = [2][]byte{pair.Banks[0].Core, pair.Banks[1].Core}
	}
	var catalog storageOwnerCatalogV2
	if storageEnrollmentPeakCatalogJSON(p.Catalog, &catalog) != nil || storageEnrollmentPeakValidateCatalog(catalog, record, claim) != nil || !s.checkCapacity(catalog) {
		return storagebudget.ErrBounds
	}
	var before storageOwnerCatalogV2
	originalV3 := outer.Snapshot.Before
	var previous *storageEnrollmentPeakEvidence
	if outer.Snapshot.Kind == 0 {
		pair, err := storageOwnerDecodeCatalogPairV3(input.Original.Catalogs[0], input.Original.Catalogs[1], record, claim)
		if err != nil {
			return err
		}
		before = pair.High
		bank := 1
		if storageEnrollmentPeakU64(input.Original.Catalogs[0][16:24]) > storageEnrollmentPeakU64(input.Original.Catalogs[1][16:24]) {
			bank = 0
		}
		if (wrapped || native) && bytes.Equal(input.Original.Catalogs[0], input.Original.Catalogs[1]) {
			bank = int(s[322])
		}
		if s.hasOriginalPeer() {
			restored, _, err := storageEnrollmentPeakRestoreOriginalPeerVariant(s, outer.Snapshot.Before, record, claim, wrapped)
			if err != nil || !bytes.Equal(restored[0], input.Original.Catalogs[0]) || !bytes.Equal(restored[1], input.Original.Catalogs[1]) {
				return storagebudget.ErrBounds
			}
		}
		if bank != int(s[322]) || !bytes.Equal(input.Original.Catalogs[bank], outer.Snapshot.Before) {
			return storagebudget.ErrBounds
		}
	} else {
		old, bank, err := storageEnrollmentPeakDecodePairVariant(input.Original.Catalogs[0], input.Original.Catalogs[1], record, claim, wrapped || native)
		if (wrapped || native) && bytes.Equal(input.Original.Catalogs[0], input.Original.Catalogs[1]) {
			bank = int(s[322])
		}
		if err != nil || old.Marker == nil || old.Marker.Phase != 3 || !bytes.Equal(input.Original.Catalogs[0], input.Original.Catalogs[1]) || bank != int(s[322]) || !bytes.Equal(input.Original.Catalogs[bank], outer.Snapshot.Before) {
			return storagebudget.ErrBounds
		}
		e, err := storageEnrollmentPeakDecodeEvidenceWithLineage(input.PreviousEvidence, retained, record, claim)
		if err != nil || !storageEnrollmentPeakOriginalPeerRetained(s, e.Scope) || storageEnrollmentPeakCompareSnapshot(outer, e, record, claim) != nil {
			return storagebudget.ErrBounds
		}
		previous = &e
		before = e.Inner.Catalog
		originalV3 = e.OriginalV3
		if input.Original.Extents[s[323]] != e.Extent {
			return storagebudget.ErrBounds
		}
	}
	if outer.Snapshot.Intent != nil {
		intent := outer.Snapshot.Intent
		if previous == nil || intent.SourceKind != p.SourceKind || !bytes.Equal(intent.Source, p.Source) || intent.Origin != p.FreshOrigin || sha256.Sum256(p.Catalog) != intent.Catalog {
			return storagebudget.ErrBounds
		}
		canonical, err := json.Marshal(catalog)
		if err != nil || !bytes.Equal(canonical, p.Catalog) {
			return storagebudget.ErrBounds
		}
		flags, err := storageEnrollmentSuccessorOriginFlags(input.Original, *previous, intent.Origin, record)
		if err != nil || flags != intent.OriginFlags {
			return storagebudget.ErrBounds
		}
		old, err := storageEnrollmentPeakDecodeDispatch(p.OriginalStep3, *previous)
		if err != nil || old.Step != 3 {
			return storagebudget.ErrBounds
		}
		digest, err := storageEnrollmentSuccessorInputs(s, input.Original, previous.Raw, record.QBinding[:], p.OriginalStep3)
		if native {
			digest, err = storageEnrollmentSuccessorNativeInputs(s, physicalOriginal, previous.Raw, record.QBinding[:], p.OriginalStep3, record, claim)
		} else if wrapped {
			digest, err = storageEnrollmentSuccessorCompositionInputs(s, physicalOriginal, previous.Raw, record.QBinding[:], p.OriginalStep3, outer.Snapshot.Physical, record, claim)
		}
		if err != nil || digest != intent.Inputs {
			return storagebudget.ErrBounds
		}
	} else if p.FreshOrigin != (storagebudget.ID{}) || len(p.OriginalStep3) != 0 {
		return storagebudget.ErrBounds
	}
	var projection storageEnrollmentPeakProjection
	var tickets []storagebudget.Ticket
	var sets []storagePolicyEffectiveSet
	var views []storageEnrollmentPeakView
	var cp storageEnrollmentPeakCheckpointProjection
	var err error
	if native {
		tickets, sets, err = storagePolicyReadEffectiveTable(p.Tickets, p.TicketCount, catalog)
		if err == nil {
			projection, err = storagePolicyReadProjection(p.Projection, catalog, originalV3, retained, sets, record, claim)
		}
		if err == nil {
			views, err = storagePolicyReadViews(p.Views, p.ViewCount, catalog, tickets, sets)
		}
		if err == nil {
			cp, err = storagePolicyReadCheckpoint(p.Checkpoint, catalog, views, s, sets)
		}
	} else {
		if wrapped {
			projection, err = storageEnrollmentPeakReadProjectionV2(p.Projection, catalog, originalV3, retained, record, claim)
		} else {
			projection, err = storageEnrollmentPeakReadProjection(p.Projection, catalog, originalV3, record, claim)
		}
		if err == nil {
			tickets, err = storageEnrollmentPeakReadTickets(p.Tickets, p.TicketCount)
		}
		if err == nil {
			views, err = storageEnrollmentPeakReadViews(p.Views, p.ViewCount, catalog, tickets)
		}
		if err == nil {
			cp, err = storageEnrollmentPeakReadCheckpoint(p.Checkpoint, catalog, views, s)
		}
	}
	if err != nil || storageEnrollmentPeakProjectionFullDigest(projection) != outer.Snapshot.Full || !reflect.DeepEqual(projection.Catalog, before) || wrapped && projection.Physical != outer.Snapshot.Physical || !storageEnrollmentPeakBeforeDefinitions(catalog, cp) || cp.Q == nil || cp.Q.Maintenance || cp.Q.Generation > ^uint64(0)-3 || cp.Q.Epoch == ^uint64(0) {
		return storagebudget.ErrBounds
	}
	if native {
		var actual [2][]byte
		if projection.Native != nil {
			actual, err = storagePolicyNativeProjectionPair(projection)
		} else {
			actual, err = storagePolicyProjectionPair(projection)
		}
		if err != nil || !bytes.Equal(actual[0], physicalOriginal.Catalogs[0]) || !bytes.Equal(actual[1], physicalOriginal.Catalogs[1]) || !storagePolicyViewProjectionPair(views[cp.View], projection, catalog) {
			return storagebudget.ErrBounds
		}
	}
	if s.hasOriginalPeer() {
		restored, err := storageEnrollmentPeakRestoreOriginalCheckpoints(cp, catalog)
		if err != nil || !bytes.Equal(restored[0], input.Original.Checkpoints[0]) || !bytes.Equal(restored[1], input.Original.Checkpoints[1]) {
			return storagebudget.ErrBounds
		}
	}
	view := views[cp.View]
	vd := sha256.Sum256(view.Raw)
	if !bytes.Equal(vd[:], s[152:184]) || catalog.Sequence != before.Sequence+1 || before.Sequence == ^uint64(0) || catalog.Transition != (storagebudget.ID([16]byte(s[24:40]))) || catalog.ExpectedQRevision != cp.Q.Generation+1 || !bytes.Equal(catalog.PreviousCatalogDigest[:], outer.Snapshot.Before[32:64]) || !native && catalog.Policy != view.Policy {
		return storagebudget.ErrBounds
	}
	_, oldDefinitions, e1 := before.definitions()
	beforeDefinitions, _, e2 := catalog.definitions()
	if e1 != nil || e2 != nil || !reflect.DeepEqual(oldDefinitions, beforeDefinitions) {
		return storagebudget.ErrBounds
	}
	// Exact dependency-closed append, using the existing board/source joins.
	b := storageEnrollmentPeakBoard{}
	dict := []reflect.Value{reflect.ValueOf(catalog.Roots), reflect.ValueOf(catalog.Workspaces), reflect.ValueOf(catalog.Filesystems), reflect.ValueOf(catalog.Constraints)}
	oldIDs := [][]storagebudget.ID{catalog.BeforeDefinitionIDs.Roots, catalog.BeforeDefinitionIDs.Workspaces, catalog.BeforeDefinitionIDs.Filesystems, catalog.BeforeDefinitionIDs.Constraints}
	limits := []int{1, 0, 1, 4}
	for k, rows := range dict {
		b.Registration[k] = []uint16{}
		for i := 0; i < rows.Len(); i++ {
			id := rows.Index(i).FieldByName("Definition").FieldByName("ID").Interface().(storagebudget.ID)
			found := false
			for _, old := range oldIDs[k] {
				found = found || id == old
			}
			if !found {
				b.Registration[k] = append(b.Registration[k], uint16(i))
			}
		}
		if len(b.Registration[k]) > limits[k] {
			return storagebudget.ErrBounds
		}
	}
	if p.SourceKind == 7 || p.SourceKind == 8 || p.SourceKind == 9 || p.SourceKind == 10 || p.SourceKind == 11 {
		if !s.hasOriginalPeer() {
			return storagebudget.ErrBounds
		}
		b.SourceKind = p.SourceKind
		b.Source, err = storageEnrollmentPeakDecodeSource(p.Source)
		if p.SourceKind == 8 && (outer.Snapshot.Intent == nil || outer.Snapshot.Intent.Family != 6 || b.Source.ActionPageDigest == (storagebudget.Digest{})) {
			return storagebudget.ErrBounds
		}
	} else {
		if p.SourceKind != 0 || len(p.Source) != 0 || len(b.Registration[0]) != 1 {
			return storagebudget.ErrBounds
		}
		root := catalog.Roots[b.Registration[0][0]]
		b.SourceKind = root.Source.Kind
		sourceRaw := root.Source.Relative
		if b.SourceKind == 8 {
			found := false
			for _, parent := range catalog.Roots {
				if parent.Definition.ID == root.Source.ParentRoot {
					sourceRaw = parent.Source.Relative
					found = true
				}
			}
			if !found {
				return storagebudget.ErrBounds
			}
		}
		b.Source, err = storageEnrollmentPeakDecodeSource(sourceRaw)
	}
	if err != nil || b.Source.Project != input.Original.Project || !storageEnrollmentPeakRegistrationJoins(b, catalog) {
		return storagebudget.ErrBounds
	}
	if !storageEnrollmentRetainsOriginalWithDefault(before, catalog, &b.Source) {
		return storagebudget.ErrBounds
	}
	proofContext := storageEnrollmentPeakEvidence{Installation: record.Installation, QBinding: record.QBinding, Scope: s, Board: b, Project: input.Original.Project, Bridge: catalog.Transition, Inner: storageEnrollmentPeakInner{Catalog: catalog}, Before: storageEnrollmentPeakProjectionFullDigest(projection), Projections: []storageEnrollmentPeakProjection{projection}}
	for i, native := range input.Original.Targets {
		descriptor := s[1360+i*32 : 1360+(i+1)*32]
		bef, fin := storageEnrollmentPeakU64(descriptor[4:12]), storageEnrollmentPeakU64(descriptor[12:20])
		if native != s.target(byte(i+1)) || input.Original.Extents[i] < bef || input.Original.Extents[i] > fin || s.path() == 2 && input.Original.Extents[i] != fin || s[321] == 0 && i < 4 && input.Original.Extents[i] != 0 {
			return storagebudget.ErrBounds
		}
	}
	context := storageEnrollmentPeakEvidence{Phase: 1, Installation: record.Installation, QBinding: record.QBinding, Bridge: catalog.Transition, Scope: s, Checkpoint: cp}
	if q, err := storageEnrollmentPeakCompareQ(context, input.Original.Checkpoints, input.Original.Tickets, storageEnrollmentPeakDispatch{Bridge: catalog.Transition, Step: 0}); err != nil || q.Stage != 0 {
		return storagebudget.ErrBounds
	}
	for bank := 0; bank < 2; bank++ {
		if !bytes.Equal(input.CurrentCheckpoints[bank], input.Original.Checkpoints[bank]) || !bytes.Equal(input.CurrentTickets[bank], input.Original.Tickets[bank]) {
			return storagebudget.ErrBounds
		}
	}
	state := storageEnrollmentPeakHostState(view)
	for _, slot := range state.Slots {
		if storageUnretired(slot) || slot.Ticket.State != storagebudget.Empty && slot.PersistedNextSequence <= slot.Ticket.Token.Sequence {
			return storagebudget.ErrBounds
		}
	}
	for slot, pages := range input.Original.Hosts {
		if len(pages[0]) != 16384 || len(pages[1]) != 16384 {
			return storagebudget.ErrBounds
		}
		choice, err := storageSelectCreationHost(pages[0], pages[1], slot, record.Installation, record.QBinding)
		if err != nil {
			return err
		}
		if choice.Slot.record.Phase == storageIntentEmpty {
			continue
		}
		core, identity, bank := choice.Slot.record, choice.Identity, choice.Slot.bank
		if choice.Creation != nil {
			proofContext.Witnesses = append(proofContext.Witnesses, storageEnrollmentPeakWitness{Selected: true, Creation: choice.Creation, Page: pages[bank], Digest: sha256.Sum256(pages[bank])})
		}
		if choice.Creation != nil && core.Phase == storageIntentRetired {
			r := *choice.Creation
			if !s.hasOriginalPeer() || !storageCreationTailCatalog(r, catalog) {
				return storagebudget.ErrBounds
			}
			if r.Tail.Bridge == catalog.Transition {
				if r.Tail.Config != s.target(9) || r.Tail.Anchor != s.target(10) || r.Tail.Project != storageEnrollmentPeakNative(s[286:321]) || !bytes.Equal(r.Tail.Qualification[:], s[408:440]) {
					return storagebudget.ErrBounds
				}
			} else if previous == nil || !storageCreationTailIncluded(r, *previous) {
				return storagebudget.ErrBounds
			}
		}
		if !storageEnrollmentPeakCoreCatalog(core, before) {
			return storagebudget.ErrBounds
		}
		retained := false
		if previous != nil {
			for _, w := range previous.Witnesses {
				retained = retained || w.Selected && int(w.Slot) == slot && bytes.Equal(w.Page, pages[bank])
			}
		}
		if previous != nil && s.hasOriginalPeer() {
			retained = storageEnrollmentPeakPrefixCovered(*previous, slot, pages, state)
		}
		switch core.Phase {
		case storageIntentRejected:
			if !retained && core.AdmissionEpoch != state.Epoch {
				return storagebudget.ErrBounds
			}
			for _, s := range state.Slots {
				if storageIntentMatchingTicket(core, s.Ticket, record.QBinding) {
					return storagebudget.ErrBounds
				}
			}
		case storageIntentRetired:
			if !storageIntentRetiredProof(core, state) && !(native && storagePolicyRetiredAtProjection(core, state, projection)) && !(s.hasOriginalPeer() && retained) {
				return storagebudget.ErrBounds
			}
			if identity != nil && state.Slots[core.Token.Slot].PersistedApplied == core.Token && !storageIdentityIntentExactRetired(*identity, state, record.QBinding) && !retained {
				return storagebudget.ErrBounds
			}
		default:
			return storagebudget.ErrBounds
		}
	}
	if b.Source.Default != nil && (b.Source.Default.Workflow == 4 || b.Source.Default.Workflow == 5 || b.Source.Default.Workflow == 6) {
		if !storageTransferPrefixActionProof(b.Source, before, catalog, state, input.Original.Hosts) {
			return storagebudget.ErrBounds
		}
	} else if !storageEnrollmentDefaultActionProof(proofContext) {
		return storagebudget.ErrBounds
	}
	return nil
}
func storageEnrollmentPeakCoreCatalog(core storageIntentRecord, c storageOwnerCatalogV2) bool {
	for i, component := range core.Components {
		rootFound, fsFound := false, false
		for _, r := range c.Roots {
			d := r.Definition
			if d.ID == component.Root && d.Owner == component.Owner && d.Generation == component.RootGeneration && d.Identity == core.RootIdentities[i] && d.Locator == core.Locators[i] && d.Filesystem == component.Filesystem && d.CapacityBinding == component.CapacityBinding && d.CapacityCount == component.CapacityCount {
				rootFound = true
				for j := 0; j < int(d.CapacityCount); j++ {
					if d.CapacityRefs[j].Constraint != component.Capacity[j].Constraint || d.CapacityRefs[j].Generation != component.Capacity[j].Generation {
						rootFound = false
					}
				}
			}
		}
		for _, f := range c.Filesystems {
			fsFound = fsFound || f.Definition.ID == component.Filesystem && f.Definition.Generation == component.FilesystemGeneration && f.Definition.Identity == core.FilesystemIdentities[i]
		}
		if !rootFound || !fsFound {
			return false
		}
	}
	return true
}

// Completed references the exact preceding Armed image by full digest, even
// after both C copies replace the last D marker. Diagnostic states/generations
// may differ; every immutable section/board/source/F byte remains exact.
func storageEnrollmentPeakJoinArmed(completed, armed storageEnrollmentPeakEvidence) bool {
	if completed.Phase != 2 || armed.Phase != 1 || completed.Bank > 1 || armed.Bank > 1 || completed.Scope != armed.Scope || completed.Extent != completed.Scope.final(completed.Bank) || armed.Extent != armed.Scope.final(armed.Bank) || completed.Bank == armed.Bank || armed.Generation == ^uint64(0) || completed.Generation != armed.Generation+1 || completed.Previous != sha256.Sum256(armed.Raw) || completed.Packed != armed.Packed || completed.BeforeRevision != armed.BeforeRevision || completed.BeforeEpoch != armed.BeforeEpoch || completed.AfterRevision != armed.AfterRevision || completed.AfterEpoch != armed.AfterEpoch || completed.Record != armed.Record || completed.Claim != armed.Claim || completed.Ready != armed.Ready || completed.Before != armed.Before || completed.BeforeInner != armed.BeforeInner || completed.AfterInner != armed.AfterInner || completed.RegistrationDigest != armed.RegistrationDigest || completed.CensusDigest != armed.CensusDigest || completed.CoveredEpoch != armed.CoveredEpoch {
		return false
	}
	for i := 0; i < 8; i++ {
		if completed.Sections[i].Count != armed.Sections[i].Count || completed.Sections[i].Offset != armed.Sections[i].Offset || !bytes.Equal(completed.Sections[i].Raw, armed.Sections[i].Raw) {
			return false
		}
	}
	a, b := append([]byte{}, completed.Sections[8].Raw...), append([]byte{}, armed.Sections[8].Raw...)
	if len(a) != len(b) || completed.Sections[8].Count != armed.Sections[8].Count || completed.Sections[8].Offset != armed.Sections[8].Offset {
		return false
	}
	start := 174 + 24*int(completed.Scope[326])
	for i := range completed.Board.Groups {
		off := start + i*115
		if off > len(a)-115 || off > len(b)-115 {
			return false
		}
		a[off+38] = 0
		b[off+38] = 0
		clear(a[off+107 : off+115])
		clear(b[off+107 : off+115])
	}
	return bytes.Equal(a, b)
}
