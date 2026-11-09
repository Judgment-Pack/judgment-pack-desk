package desk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"reflect"
	"sort"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func storageEnrollmentDictionaryIndex(c storageOwnerCatalogV2, kind int, id storagebudget.ID) (uint16, error) {
	var ids []storagebudget.ID
	switch kind {
	case 0:
		for _, v := range c.Filesystems {
			ids = append(ids, v.Definition.ID)
		}
	case 1:
		for _, v := range c.Constraints {
			ids = append(ids, v.Definition.ID)
		}
	case 2:
		for _, v := range c.Roots {
			ids = append(ids, v.Definition.ID)
		}
	case 3:
		for _, v := range c.Workspaces {
			ids = append(ids, v.Definition.ID)
		}
	default:
		return 0, storagebudget.ErrBounds
	}
	for i, v := range ids {
		if v == id {
			return uint16(i), nil
		}
	}
	return 0, storagebudget.ErrStale
}

func storageEnrollmentBeforeProjection(before, after storageOwnerCatalogV2, raw []byte) (storageEnrollmentPeakProjection, error) {
	if len(raw) == storageOwnerCatalogBytes && bytes.Equal(raw[:8], storageOwnerV6Magic[:]) {
		return storagePolicyBeforeProjection(before, after, raw)
	}
	if storageTransferProjectionChanged(before, after) {
		return storageTransferHistoricalProjection(before, after, raw)
	}
	p := storageEnrollmentPeakProjection{Kind: 0, Catalog: before, SavedOuter: append([]byte{}, raw[:64]...)}
	if string(raw[:8]) == "DSKONP1\x00" {
		p.Kind = 1
		p.OriginalRef = 65535
		p.SavedOuter = append([]byte{}, raw[:8320]...)
	}
	old, next := storageEnrollmentPeakDictionaries(before), storageEnrollmentPeakDictionaries(after)
	for k := range old {
		p.Select[k] = []uint16{}
		for i := 0; i < old[k].Len(); i++ {
			found := -1
			for j := 0; j < next[k].Len(); j++ {
				match := false
				switch k {
				case 0:
					match = reflect.DeepEqual(before.objectMaterial(uint32(i)), after.objectMaterial(uint32(j)))
				case 1:
					a, e := before.controlDigest(before.OwnerControls[i])
					b, f := after.controlDigest(after.OwnerControls[j])
					match = e == nil && f == nil && a == b && before.OwnerControls[i].MaxBytes == after.OwnerControls[j].MaxBytes
				case 2:
					match = before.Roots[i].Definition.ID == after.Roots[j].Definition.ID
				default:
					match = reflect.DeepEqual(old[k].Index(i).Interface(), next[k].Index(j).Interface())
				}
				if match {
					if found >= 0 {
						return p, storagebudget.ErrBounds
					}
					found = j
				}
			}
			if found < 0 {
				return p, storagebudget.ErrStale
			}
			p.Select[k] = append(p.Select[k], uint16(found))
		}
	}
	return p, nil
}

func (p *storageEnrollmentEpoch) initialPacketModel(ctx context.Context, catalog storageOwnerCatalogV2, source storageEnrollmentPeakSource) (storageEnrollmentPeakEvidence, error) {
	return p.initialPacketModelOriginal(ctx, catalog, source, nil)
}

// A retained origin is consumed only by the exact new successor Fence route;
// no decoded operand issues authority or relaxes the ordinary live entry.
func (p *storageEnrollmentEpoch) initialPacketModelOriginal(ctx context.Context, catalog storageOwnerCatalogV2, source storageEnrollmentPeakSource, recovered *storageEnrollmentSuccessorIntent) (storageEnrollmentPeakEvidence, error) {
	var e storageEnrollmentPeakEvidence
	if p == nil || p.self != p || p.original == nil || p.initialInspection == nil || p.scope != nil && recovered == nil && (!p.startup || p.scope.path() != 1 || p.scope[321] != 15) || p.c.ref.cell.ownerRecord == nil {
		return e, storagebudget.ErrAuthority
	}
	if err := p.held(ctx); err != nil {
		return e, err
	}
	if recovered != nil {
		if !p.startup || p.scope == nil || p.scope.path() == 1 || p.previousEvidence == nil {
			return e, storagebudget.ErrAuthority
		}
		actual, err := p.enrollmentEnvelope(p.initialFence, true)
		if err != nil || actual.Scope != *p.scope || actual.Snapshot == nil || actual.Snapshot.Intent == nil {
			return e, storagebudget.ErrBounds
		}
		a, err := storageEnrollmentWriteSuccessorIntent(*actual.Snapshot.Intent)
		if err != nil {
			return e, err
		}
		b, err := storageEnrollmentWriteSuccessorIntent(*recovered)
		if err != nil || !bytes.Equal(a, b) {
			return e, storagebudget.ErrBounds
		}
		encoded, err := storageEnrollmentPeakEncodeSource(source)
		if err != nil || !bytes.Equal(encoded, recovered.Source) {
			return e, storagebudget.ErrBounds
		}
		raw, err := json.Marshal(catalog)
		if err != nil || sha256.Sum256(raw) != recovered.Catalog {
			return e, storagebudget.ErrBounds
		}
	}
	record, claim := *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim
	cores, originalBank, err := p.originalCorePair(p.original.Catalogs, record, claim)
	if err != nil {
		return e, err
	}
	var physical storageCompositionProjection
	var lineage *storageCompositionLineage
	native := storagePolicyActualPair(p.original.Catalogs)
	wrapped := string(p.original.Catalogs[0][:8]) == "DSKONL1\x00"
	if wrapped {
		pair, e0 := storageCompositionReadPair(p.original.Catalogs[0], p.original.Catalogs[1], record, claim)
		if e0 != nil || pair.RecoveryOnly {
			return e, storagebudget.ErrBounds
		}
		physical, e0 = storageCompositionCaptureProjection(p.original.Catalogs[0], p.original.Catalogs[1], record, claim)
		if e0 != nil {
			return e, e0
		}
		retained := pair.Banks[pair.Selected].Lineage
		lineage = &retained
	}
	if native {
		lineage = p.compositionLineage()
		if lineage == nil {
			return e, errStorageEnrollmentUnresolved
		}
	}
	var before storageOwnerCatalogV2
	bank := 1
	originalV3 := []byte(nil)
	generation := uint64(1)
	previous := storagebudget.Digest{}
	if p.previousEvidence != nil {
		prior, selected, e0 := storageEnrollmentPeakDecodePairVariant(cores[0], cores[1], record, claim, wrapped || native)
		if e0 != nil || prior.Marker == nil || prior.Marker.Phase != 3 || !bytes.Equal(cores[0], cores[1]) || storageEnrollmentPeakJoinMarker(prior, *p.previousEvidence, p.original.Ready) != nil {
			return e, storagebudget.ErrBounds
		}
		bank = selected
		if wrapped || native {
			bank = int(originalBank)
		}
		before = p.previousEvidence.Inner.Catalog
		originalV3 = p.previousEvidence.OriginalV3
		generation = p.previousEvidence.Generation + 1
		previous = sha256.Sum256(p.previousEvidence.Raw)
	} else {
		beforePair, e0 := storageOwnerDecodeCatalogPairV3(cores[0], cores[1], record, claim)
		if e0 != nil {
			return e, e0
		}
		before = beforePair.High
		if storageEnrollmentPeakU64(cores[0][16:24]) > storageEnrollmentPeakU64(cores[1][16:24]) {
			bank = 0
		}
		if wrapped || native {
			bank = int(originalBank)
		}
		originalV3 = cores[bank]
	}
	old := cores[bank]
	if native {
		old = p.original.Catalogs[bank]
	}
	anchor := storageEnrollmentPeakQHeldAnchor{Installation: storageEnrollmentPeakQID(record.Installation), Binding: storageEnrollmentPeakQDigest(record.QBinding)}
	q0, err := storageEnrollmentPeakQDecodeCheckpoint(append([]byte{}, p.initialInspection.Checkpoints[p.initialInspection.CheckpointBank]...), anchor)
	if err != nil {
		return e, err
	}
	if err = storageEnrollmentPeakValidateCatalog(catalog, record, claim); err != nil {
		return e, err
	}
	if !storageEnrollmentRetainsOriginalWithDefault(before, catalog, &source) {
		return e, errStorageCustody
	}
	e = storageEnrollmentPeakEvidence{Generation: generation, Previous: previous, Phase: 0, Bank: 0, Installation: record.Installation, QBinding: record.QBinding, Bridge: catalog.Transition, Ready: p.original.Ready, Before: sha256.Sum256(old), BeforeRevision: q0.Generation, BeforeEpoch: q0.Epoch, AfterRevision: q0.Generation + 3, AfterEpoch: q0.Epoch + 1, CoveredEpoch: q0.Epoch + 1, Project: p.original.Project, Inner: storageEnrollmentPeakInner{Catalog: catalog}, OriginalV3: append([]byte{}, originalV3...)}
	if wrapped {
		e.Version, e.CompositionLineage = 2, lineage
	}
	if native {
		e.Version, e.CompositionLineage = 3, lineage
	}
	recordRaw, err := storageOwnerEncodeRecordV2(record)
	if err != nil {
		return e, err
	}
	claimRaw, err := storageOwnerEncodeClaimV2(claim)
	if err != nil {
		return e, err
	}
	e.Record, e.Claim = sha256.Sum256(recordRaw), sha256.Sum256(claimRaw)
	beforeJSON, err := json.Marshal(before)
	if err != nil {
		return e, err
	}
	afterJSON, err := json.Marshal(catalog)
	if err != nil {
		return e, err
	}
	e.BeforeInner, e.AfterInner = sha256.Sum256(beforeJSON), sha256.Sum256(afterJSON)
	length := 64 + int(storageEnrollmentPeakU32(originalV3[24:28]))
	if length > len(originalV3) {
		return e, storagebudget.ErrBounds
	}
	e.Sections[0] = storageEnrollmentPeakSection{Count: 1, Raw: append([]byte{}, originalV3[:length]...)}
	view := storageEnrollmentPeakView{Revision: q0.Generation, Epoch: q0.Epoch, Maintenance: q0.Maintenance, Policy: storageOwnerV6HostPolicy(p.initialInspection.View.Configuration.Policy), Slots: p.initialInspection.View.Slots}
	if native {
		definitions := storageOwnerDefinitionSnapshot(p.initialInspection.View)
		set := storagePolicyEffectiveSet{Policy: definitions.Policy, Workspaces: append([]storagebudget.RegisteredWorkspace{}, definitions.Workspaces...)}
		sort.Slice(set.Workspaces, func(i, j int) bool { return bytes.Compare(set.Workspaces[i].ID[:], set.Workspaces[j].ID[:]) < 0 })
		view.Effective = &set
		e.EffectiveSets = []storagePolicyEffectiveSet{set}
	}
	// Ticket dictionary is canonical by complete serialized value, not hash.
	type ticketBody struct {
		ticket storagebudget.Ticket
		raw    []byte
	}
	var bodies []ticketBody
	for _, slot := range view.Slots {
		raw, err := storageEnrollmentPeakWriteTicket(slot.Ticket)
		if err != nil {
			return e, err
		}
		bodies = append(bodies, ticketBody{slot.Ticket, raw})
	}
	sort.Slice(bodies, func(i, j int) bool { return bytes.Compare(bodies[i].raw, bodies[j].raw) < 0 })
	for i, body := range bodies {
		if i == 0 || !bytes.Equal(body.raw, bodies[i-1].raw) {
			e.Tickets = append(e.Tickets, body.ticket)
		}
	}
	if native {
		e.Sections[2].Raw, e.EffectiveSets, err = storagePolicyBuildEffectiveTable(e.Tickets, e.EffectiveSets, catalog)
	} else {
		e.Sections[2].Raw, err = storageEnrollmentPeakWriteTickets(e.Tickets)
	}
	if err != nil {
		return e, err
	}
	e.Sections[2].Count = uint32(len(e.Tickets))
	var cp storageEnrollmentPeakCheckpointProjection
	cp.Q = q0
	cp.Bank = p.initialInspection.CheckpointBank
	cp.PeerCheckpoint = append([]byte{}, p.original.Checkpoints[1-cp.Bank]...)
	cp.TicketBanks = p.initialInspection.TicketBanks
	for bank := 0; bank < 2; bank++ {
		cp.Checkpoints[bank] = sha256.Sum256(p.original.Checkpoints[bank])
		for slot := 0; slot < 64; slot++ {
			cp.Tickets[slot][bank] = sha256.Sum256(p.original.Tickets[bank][slot*16384 : (slot+1)*16384])
		}
	}
	for kind, array := range []reflect.Value{reflect.ValueOf(q0.Filesystems), reflect.ValueOf(q0.CapacityConstraints), reflect.ValueOf(q0.Roots), reflect.ValueOf(q0.Workspaces)} {
		view.DefinitionRefs[kind] = []uint16{}
		for i := 0; i < array.Len(); i++ {
			value := array.Index(i).FieldByName("ID")
			if value.IsZero() {
				continue
			}
			id := value.Convert(reflect.TypeOf(storagebudget.ID{})).Interface().(storagebudget.ID)
			index, err := storageEnrollmentDictionaryIndex(catalog, kind, id)
			if err != nil {
				return e, err
			}
			view.DefinitionRefs[kind] = append(view.DefinitionRefs[kind], index)
		}
	}
	cp.RootRefs = append([]uint16{}, view.DefinitionRefs[2]...)
	cp.WorkspaceRefs = append([]uint16{}, view.DefinitionRefs[3]...)
	cp.FilesystemRefs = append([]uint16{}, view.DefinitionRefs[0]...)
	cp.ConstraintRefs = append([]uint16{}, view.DefinitionRefs[1]...)
	if native {
		e.Sections[3].Raw, err = storagePolicyWriteViews([]storageEnrollmentPeakView{view}, catalog, e.Tickets, e.EffectiveSets)
	} else {
		e.Sections[3].Raw, err = storageEnrollmentPeakWriteViews([]storageEnrollmentPeakView{view}, catalog, e.Tickets)
	}
	if err != nil {
		return e, err
	}
	e.Sections[3].Count = 1
	if native {
		e.Views, err = storagePolicyReadViews(e.Sections[3].Raw, 1, catalog, e.Tickets, e.EffectiveSets)
	} else {
		e.Views, err = storageEnrollmentPeakReadViews(e.Sections[3].Raw, 1, catalog, e.Tickets)
	}
	if err != nil {
		return e, err
	}
	projection, err := storageEnrollmentBeforeProjection(before, catalog, old)
	if native {
		payload, _, pe := storageOwnerV6Payload(old)
		if pe != nil {
			return e, pe
		}
		if bytes.HasPrefix(payload, storageOwnerV6NativeMagic[:]) {
			projection, err = storagePolicyBeforeNativeProjection(before, catalog, p.original.Catalogs, byte(bank), p.previousEvidence.Raw, record, claim)
		} else {
			projection, err = storagePolicyBeforeProjection(before, catalog, old)
			if err == nil {
				err = storagePolicyRetainProjectionPeer(&projection, p.original.Catalogs, byte(bank), p.previousEvidence.Raw, record, claim)
			}
		}
	}

	if !native && source.Default != nil && source.Default.Workflow == 5 {
		op := p.c.originalTransferOperation()
		if op == nil {
			return e, storagebudget.ErrAuthority
		}
		projection, err = storageEnrollmentTransferBeforeProjection(before, catalog, old, op.record)
	}
	if err != nil {
		return e, err
	}
	if native {
		e.Sections[4].Raw, err = storagePolicyWriteProjection(projection, catalog, originalV3, lineage, e.EffectiveSets, record, claim)
	} else if wrapped {
		projection.Physical = physical
		e.Sections[4].Raw, err = storageEnrollmentPeakWriteProjectionV2(projection, catalog, originalV3, lineage, record, claim)
	} else {
		e.Sections[4].Raw, err = storageEnrollmentPeakWriteProjection(projection, catalog, originalV3, record, claim)
	}
	if err != nil {
		return e, err
	}
	e.Sections[4].Count = 1
	e.Projections = []storageEnrollmentPeakProjection{projection}
	board := storageEnrollmentPeakBoard{SourceKind: 7, Source: source}
	if p.previousEvidence != nil {
		old := p.previousEvidence
		board.Prior = storageEnrollmentPeakEndpoint{Bridge: old.Bridge, Epoch: old.AfterEpoch, Q3Revision: old.AfterRevision, Catalog: e.Before, Evidence: sha256.Sum256(old.Raw)}
	}
	for k := range board.Registration {
		board.Registration[k] = []uint16{}
	}
	for kind, rows := range []reflect.Value{reflect.ValueOf(catalog.Roots), reflect.ValueOf(catalog.Workspaces), reflect.ValueOf(catalog.Filesystems), reflect.ValueOf(catalog.Constraints)} {
		oldIDs := [][]storagebudget.ID{catalog.BeforeDefinitionIDs.Roots, catalog.BeforeDefinitionIDs.Workspaces, catalog.BeforeDefinitionIDs.Filesystems, catalog.BeforeDefinitionIDs.Constraints}[kind]
		for i := 0; i < rows.Len(); i++ {
			id := rows.Index(i).FieldByName("Definition").FieldByName("ID").Interface().(storagebudget.ID)
			found := false
			for _, oldID := range oldIDs {
				found = found || oldID == id
			}
			if !found {
				board.Registration[kind] = append(board.Registration[kind], uint16(i))
			}
		}
	}
	if source.Default != nil {
		board.SourceKind = 9
		if source.Default.Workflow == 2 || source.Default.Workflow == 4 {
			board.SourceKind = 10
		}
		if source.Default.Workflow == 6 {
			board.SourceKind = 11
		}
		if source.Default.Workflow == 6 {
			board.SourceKind = 11
		}
	} else if len(board.Registration[0]) == 1 {
		board.SourceKind = catalog.Roots[board.Registration[0][0]].Source.Kind
	}
	e.Registration, err = storageEnrollmentPeakRegistration(catalog, board.Registration, q0)
	if err != nil {
		return e, err
	}
	e.RegistrationDigest, err = storageEnrollmentPeakRegistrationDigest(e.Registration)
	if err != nil {
		return e, err
	}
	e.Predictions, err = storageEnrollmentPeakPredict(q0, e.Registration)
	if err != nil {
		return e, err
	}
	for i := 0; i < 2; i++ {
		raw, err := storageEnrollmentPeakQEncodeCheckpoint(e.Predictions[i], anchor)
		if err != nil {
			return e, err
		}
		e.PredictionBytes[i] = raw
		e.Digests.Q[i] = sha256.Sum256(raw)
	}
	material := append(append(append(append(append([]byte{}, e.Installation[:]...), e.QBinding[:]...), e.Bridge[:]...), storageEnrollmentPeakNativeBytes(e.Project)...), e.RegistrationDigest[:]...)
	e.Digests.Origin = storageEnrollmentPeakHash("desk-enrollment-origin-e1", material)
	for _, row := range e.Predictions[1].Roots {
		if row.ID != (storageEnrollmentPeakQID{}) {
			e.Census.Roots = append(e.Census.Roots, storageEnrollmentPeakQRootMeasurement{ID: row.ID, Generation: row.Generation})
		}
	}
	for _, row := range e.Predictions[1].Workspaces {
		if row.ID != (storageEnrollmentPeakQID{}) {
			e.Census.Workspaces = append(e.Census.Workspaces, storageEnrollmentPeakQWorkspaceMeasurement{ID: row.ID, Generation: row.Generation})
		}
	}
	e.Sections[7].Raw, err = storageEnrollmentPeakWriteCensus(e.Census, e.Predictions[1], 0)
	if err != nil {
		return e, err
	}
	e.Sections[7].Count = 1
	e.CensusDigest = sha256.Sum256(e.Sections[7].Raw)
	origin := storageEnrollmentPeakOrigin{Bridge: e.Bridge, Epoch: q0.Epoch, Revision: q0.Generation}
	for slot, pages := range p.original.Hosts {
		choice, err := storageSelectCreationHost(pages[0], pages[1], slot, record.Installation, record.QBinding)
		if err != nil {
			return e, err
		}
		if choice.Slot.record.Phase == storageIntentEmpty {
			continue
		}
		selected := choice.Slot.bank
		origin.Slots |= uint64(1) << uint(slot)
		for bank, page := range pages {
			w := storageEnrollmentPeakWitness{Slot: uint16(slot), Bank: byte(bank), Page: append([]byte{}, page...), Digest: sha256.Sum256(page), Selected: bank == selected}
			if !storageEnrollmentPeakZero(page) {
				decoded, err := storageDecodeCreationHostPage(page, slot, record.Installation, record.QBinding)
				if err != nil {
					return e, err
				}
				w.Core = decoded.Slot.record
				switch {
				case decoded.Creation != nil:
					w.Version = 4
					if decoded.Creation.CapacityPolicy == storageEnrollmentCapacityEstimatePolicy {
						w.Version = 6
					}
					w.Creation = decoded.Creation
				case decoded.Identity != nil:
					w.Version = 3
					w.Identity = decoded.Identity
				default:
					w.Version = 2
					if w.Core.Fanout != nil {
						w.Version = 8
					}
					if w.Core.Transfer != nil {
						w.Version = 9
					}
				}
				w.Generation, w.Operation, w.Phase = w.Core.Generation, w.Core.OperationID, byte(w.Core.Phase)
				if w.Selected && w.Core.Phase == storageIntentRetired {
					if view.Slots[w.Core.Token.Slot].PersistedApplied == w.Core.Token {
						w.Branch = 1
					} else {
						w.Branch = 2
					}
				}
			}
			ref := uint16(len(e.Witnesses))
			origin.Witnesses = append(origin.Witnesses, ref)
			e.Witnesses = append(e.Witnesses, w)
			if w.Selected {
				board.Coverage = append(board.Coverage, storageEnrollmentPeakCoverage{Slot: uint16(slot), Witness: ref, Generation: w.Generation, Operation: w.Operation, Page: w.Digest, Epoch: e.AfterEpoch, Bridge: e.Bridge, State: 1})
			}
		}
	}
	if origin.Slots != 0 {
		if recovered == nil {
			origin.ID, err = storagebudget.NewID()
			if err != nil {
				return e, err
			}
		} else {
			origin.ID = recovered.Origin
			if origin.ID == (storagebudget.ID{}) {
				return e, storagebudget.ErrBounds
			}
		}
		e.FreshOrigin = origin.ID
		w := storageEnrollmentPeakWriter{max: 1024}
		w.put(origin.ID[:])
		w.put(origin.Bridge[:])
		w.u64(origin.Epoch)
		w.u64(origin.Revision)
		w.u16(0)
		w.u16(0)
		w.u64(origin.Slots)
		w.u16(uint16(len(origin.Witnesses)))
		for _, ref := range origin.Witnesses {
			w.u16(ref)
		}
		if w.bad {
			return e, storagebudget.ErrBounds
		}
		origin.Digest = storageEnrollmentPeakHash("desk-enrollment-origin-record-e1", w.raw)
		board.Origins = []storageEnrollmentPeakOrigin{origin}
	}
	if recovered != nil && origin.Slots == 0 && recovered.Origin != (storagebudget.ID{}) {
		return e, storagebudget.ErrBounds
	}
	e.Sections[5].Raw, err = storageEnrollmentPeakWriteWitnesses(e.Witnesses, record.Installation, record.QBinding)
	if err != nil {
		return e, err
	}
	e.Sections[5].Count = uint32(len(e.Witnesses))
	var checkpointScope storageEnrollmentPeakScope
	storageEnrollmentPeakPut16(checkpointScope[:2], 2)
	checkpointScope[325] = cp.Bank
	copy(checkpointScope[120:152], cp.Checkpoints[cp.Bank][:])
	if native {
		cp.Raw, err = storagePolicyWriteCheckpoint(cp, catalog, e.Views, checkpointScope, e.EffectiveSets)
	} else {
		cp.Raw, err = storageEnrollmentPeakWriteCheckpoint(cp, catalog, e.Views, checkpointScope)
	}
	if err != nil {
		return e, err
	}
	cp.Checkpoint = append([]byte{}, p.original.Checkpoints[cp.Bank]...)
	e.Sections[6] = storageEnrollmentPeakSection{Count: 1, Raw: append([]byte{}, cp.Raw...)}
	e.Board = board
	e.Checkpoint = cp
	if p.previousEvidence != nil {
		if err = p.rebaseEnrollmentOrigins(&e); err != nil {
			return e, err
		}
	}
	return e, p.held(ctx)
}

// The four unknown native tuples have fixed35-byte fields. This computes exact
// packed extent from field widths without inventing tuples or emitting a
// decodable pre-capture evidence record.
func storageEnrollmentInitialExtent(e storageEnrollmentPeakEvidence) (uint64, error) {
	raw, err := json.Marshal(e.Inner.Catalog)
	if err != nil {
		return 0, err
	}
	packed, err := storageEnrollmentPeakPackInner(raw)
	if err != nil {
		return 0, err
	}
	inner := uint64(64 + 4 + len(packed) + 4)
	for _, name := range storageEnrollmentPeakNames {
		inner += 35 + 2 + uint64(len(name)) + 8
	}
	last := uint64(65536)
	for i := 0; i < 9; i++ {
		n := uint64(len(e.Sections[i].Raw))
		if i == 1 {
			n = inner
		}
		if i == 6 {
			n = uint64(len(e.Checkpoint.Raw))
		}
		if i == 8 {
			n = 65536
		}
		if n > storageEnrollmentPeakSectionMaximum(func() storageEnrollmentPeakScope {
			var s storageEnrollmentPeakScope
			storageEnrollmentPeakPut16(s[:2], 2)
			return s
		}(), i) {
			return 0, storagebudget.ErrBounds
		}
		last = (last + 7) &^ uint64(7)
		last += n
	}
	extent := (last + 65535) &^ uint64(65535)
	if extent == 0 || extent > storageEnrollmentPeakEvidenceMax {
		return 0, storagebudget.ErrBounds
	}
	return extent, nil
}

func storageEnrollmentRebaseView(view storageEnrollmentPeakView, before, after storageOwnerCatalogV2) (storageEnrollmentPeakView, error) {
	out := view
	if view.Effective != nil {
		owned := *view.Effective
		owned.Workspaces = append([]storagebudget.RegisteredWorkspace{}, owned.Workspaces...)
		out.Effective = &owned
	}
	for kind, refs := range view.DefinitionRefs {
		out.DefinitionRefs[kind] = []uint16{}
		for _, ref := range refs {
			var id storagebudget.ID
			switch kind {
			case 0:
				if int(ref) >= len(before.Filesystems) {
					return out, storagebudget.ErrBounds
				}
				id = before.Filesystems[ref].Definition.ID
			case 1:
				if int(ref) >= len(before.Constraints) {
					return out, storagebudget.ErrBounds
				}
				id = before.Constraints[ref].Definition.ID
			case 2:
				if int(ref) >= len(before.Roots) {
					return out, storagebudget.ErrBounds
				}
				id = before.Roots[ref].Definition.ID
			case 3:
				if int(ref) >= len(before.Workspaces) {
					return out, storagebudget.ErrBounds
				}
				id = before.Workspaces[ref].Definition.ID
			}
			index, err := storageEnrollmentDictionaryIndex(after, kind, id)
			if err != nil {
				return out, err
			}
			out.DefinitionRefs[kind] = append(out.DefinitionRefs[kind], index)
		}
	}
	out.Raw = nil
	return out, nil
}

// Retain exactly the still-selected original histories. Q-slot reuse is not
// origin collection. Complete original views/projections/raw peers survive;
// only lossless dictionary references, slot subsets and their canonical digest
// annotations are rebased. No old epoch or host page is rewritten.
func (p *storageEnrollmentEpoch) rebaseEnrollmentOrigins(e *storageEnrollmentPeakEvidence) error {
	prior := p.previousEvidence
	if prior == nil || e == nil || len(e.Views) != 1 || len(e.Projections) != 1 {
		return storagebudget.ErrAuthority
	}
	record, claim := *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim
	type originMaterial struct {
		origin                 storageEnrollmentPeakOrigin
		view                   storageEnrollmentPeakView
		projection             storageEnrollmentPeakProjection
		viewRaw, projectionRaw []byte
		fresh                  bool
	}
	materials := map[storagebudget.ID]*originMaterial{}
	var fresh *originMaterial
	if len(e.Board.Origins) > 0 {
		o := e.Board.Origins[0]
		o.Slots = 0
		o.Witnesses = nil
		fresh = &originMaterial{origin: o, view: e.Views[0], projection: e.Projections[0], fresh: true}
	}
	witnesses := append([]storageEnrollmentPeakWitness{}, e.Witnesses...)
	ownerOrigin := map[uint16]storagebudget.ID{}
	for i := range witnesses {
		if !witnesses[i].Selected {
			continue
		}
		w := witnesses[i]
		var old *storageEnrollmentPeakWitness
		for j := range prior.Witnesses {
			v := &prior.Witnesses[j]
			if v.Selected && v.Slot == w.Slot && bytes.Equal(v.Page, w.Page) {
				old = v
				break
			}
		}
		if old == nil {
			if fresh == nil {
				return storagebudget.ErrBounds
			}
			fresh.origin.Slots |= uint64(1) << w.Slot
			ownerOrigin[w.Slot] = fresh.origin.ID
			continue
		}
		if int(old.Origin) >= len(prior.Board.Origins) {
			return storagebudget.ErrBounds
		}
		o := prior.Board.Origins[old.Origin]
		material := materials[o.ID]
		if material == nil {
			view, err := storageEnrollmentRebaseView(prior.Views[o.View], prior.Inner.Catalog, e.Inner.Catalog)
			if err != nil {
				return err
			}
			material = &originMaterial{origin: o, view: view, projection: prior.Projections[o.Projection]}
			material.origin.Slots = 0
			material.origin.Witnesses = nil
			materials[o.ID] = material
		}
		material.origin.Slots |= uint64(1) << w.Slot
		ownerOrigin[w.Slot] = o.ID
		// Both complete original raw banks for the selected history are immutable.
		for j := range witnesses {
			if witnesses[j].Slot != w.Slot {
				continue
			}
			found := false
			for _, v := range prior.Witnesses {
				if v.Slot == w.Slot && v.Bank == witnesses[j].Bank {
					if !bytes.Equal(v.Page, witnesses[j].Page) {
						return storagebudget.ErrBounds
					}
					witnesses[j] = v
					found = true
					break
				}
			}
			if !found {
				return storagebudget.ErrBounds
			}
		}
	}
	if fresh != nil && fresh.origin.Slots != 0 {
		if materials[fresh.origin.ID] != nil {
			return storagebudget.ErrBounds
		}
		materials[fresh.origin.ID] = fresh
	}
	if len(materials) > 64 {
		return storagebudget.ErrBounds
	}
	ordered := make([]*originMaterial, 0, len(materials))
	for _, m := range materials {
		ordered = append(ordered, m)
	}
	sort.Slice(ordered, func(i, j int) bool { return bytes.Compare(ordered[i].origin.ID[:], ordered[j].origin.ID[:]) < 0 })
	// Build a canonical full-ticket dictionary for every retained original view
	// plus current Q0. Current Q0 remains view0, with its physical checkpoint refs.
	views := []storageEnrollmentPeakView{e.Views[0]}
	for _, m := range ordered {
		views = append(views, m.view)
	}
	type ticketBody struct {
		value storagebudget.Ticket
		raw   []byte
	}
	var tickets []ticketBody
	for _, v := range views {
		for _, slot := range v.Slots {
			raw, err := storageEnrollmentPeakWriteTicket(slot.Ticket)
			if err != nil {
				return err
			}
			tickets = append(tickets, ticketBody{slot.Ticket, raw})
		}
	}
	sort.Slice(tickets, func(i, j int) bool { return bytes.Compare(tickets[i].raw, tickets[j].raw) < 0 })
	e.Tickets = nil
	for i, t := range tickets {
		if i == 0 || !bytes.Equal(t.raw, tickets[i-1].raw) {
			e.Tickets = append(e.Tickets, t.value)
		}
	}
	var err error
	if e.Version == 3 {
		sets := []storagePolicyEffectiveSet{}
		for i := range views {
			if views[i].Effective == nil {
				var own storageOwnerCatalogV2
				if i == 0 {
					own = e.Projections[0].Catalog
				} else {
					own = ordered[i-1].projection.Catalog
				}
				set := storagePolicyProjectionSet(storageEnrollmentPeakProjection{Catalog: own})
				views[i].Effective = &set
			}
			sets = append(sets, *views[i].Effective)
		}
		for i, m := range ordered {
			m.view = views[i+1]
		}
		e.Sections[2].Raw, e.EffectiveSets, err = storagePolicyBuildEffectiveTable(e.Tickets, sets, e.Inner.Catalog)
	} else {
		e.Sections[2].Raw, err = storageEnrollmentPeakWriteTickets(e.Tickets)
	}
	if err != nil {
		return err
	}
	e.Sections[2].Count = uint32(len(e.Tickets))
	e.Views = nil
	viewIndex := map[string]uint16{}
	internView := func(v storageEnrollmentPeakView) (uint16, error) {
		var raw []byte
		var err error
		if e.Version == 3 {
			raw, err = storagePolicyWriteViews([]storageEnrollmentPeakView{v}, e.Inner.Catalog, e.Tickets, e.EffectiveSets)
		} else {
			raw, err = storageEnrollmentPeakWriteViews([]storageEnrollmentPeakView{v}, e.Inner.Catalog, e.Tickets)
		}
		if err != nil {
			return 0, err
		}
		if index, ok := viewIndex[string(raw)]; ok {
			return index, nil
		}
		if len(e.Views) >= 66 {
			return 0, storagebudget.ErrBounds
		}
		var decoded []storageEnrollmentPeakView
		if e.Version == 3 {
			decoded, err = storagePolicyReadViews(raw, 1, e.Inner.Catalog, e.Tickets, e.EffectiveSets)
		} else {
			decoded, err = storageEnrollmentPeakReadViews(raw, 1, e.Inner.Catalog, e.Tickets)
		}
		if err != nil {
			return 0, err
		}
		index := uint16(len(e.Views))
		viewIndex[string(raw)] = index
		e.Views = append(e.Views, decoded[0])
		return index, nil
	}
	currentView, err := internView(views[0])
	if err != nil {
		return err
	}
	e.Checkpoint.View = currentView
	for _, m := range ordered {
		m.origin.View, err = internView(m.view)
		if err != nil {
			return err
		}
	}
	if e.Version == 3 {
		e.Sections[3].Raw, err = storagePolicyWriteViews(e.Views, e.Inner.Catalog, e.Tickets, e.EffectiveSets)
	} else {
		e.Sections[3].Raw, err = storageEnrollmentPeakWriteViews(e.Views, e.Inner.Catalog, e.Tickets)
	}
	if err != nil {
		return err
	}
	e.Sections[3].Count = uint32(len(e.Views))
	// Projections are losslessly rebased, interned and byte-sorted independently
	// of origin order. Their complete original SavedOuter bytes remain exact.
	projections := []storageEnrollmentPeakProjection{e.Projections[0]}
	for _, m := range ordered {
		old := m.projection
		var raw []byte
		if old.Native != nil {
			base := old
			base.Native = nil
			raw, err = storageEnrollmentPeakProjectionCore(base)
			if err != nil {
				return err
			}
		} else if old.Policy != nil {
			raw, err = storagePolicyProjectionOuter(old)
			if err != nil {
				return err
			}
		} else if old.Kind == 0 {
			raw, err = storageOwnerEncodeCatalogV3(storageOwnerCatalogV3(old.Catalog), record, claim)
			if err != nil {
				return err
			}
		} else {
			raw = make([]byte, 8388608)
			copy(raw, old.SavedOuter)
		}
		rebased, err := storageEnrollmentBeforeProjection(old.Catalog, e.Inner.Catalog, raw)
		if err != nil {
			return err
		}
		rebased.OriginalRef = old.OriginalRef
		rebased.Physical = old.Physical
		if old.Policy != nil {
			// The outer frame alone cannot recreate the original selected bank,
			// independent peer previous digest or exact inverse/outcome metadata.
			rebased.Policy = storageEnrollmentOperandClone(old.Policy)
		}
		if old.Native != nil {
			meta := *old.Native
			meta.Effective.Workspaces = append([]storagebudget.RegisteredWorkspace{}, old.Native.Effective.Workspaces...)
			rebased.Native = &meta
		}
		m.projection = rebased
		projections = append(projections, rebased)
	}
	type projectionBody struct {
		value storageEnrollmentPeakProjection
		raw   []byte
	}
	var bodies []projectionBody
	writeProjection := func(v storageEnrollmentPeakProjection) ([]byte, error) {
		if e.Version == 3 {
			return storagePolicyWriteProjection(v, e.Inner.Catalog, e.OriginalV3, e.CompositionLineage, e.EffectiveSets, record, claim)
		}
		if e.Version == 2 {
			return storageEnrollmentPeakWriteProjectionV2(v, e.Inner.Catalog, e.OriginalV3, e.CompositionLineage, record, claim)
		}
		return storageEnrollmentPeakWriteProjection(v, e.Inner.Catalog, e.OriginalV3, record, claim)
	}
	for _, v := range projections {
		raw, err := writeProjection(v)
		if err != nil {
			return err
		}
		bodies = append(bodies, projectionBody{v, raw})
	}
	sort.Slice(bodies, func(i, j int) bool { return bytes.Compare(bodies[i].raw, bodies[j].raw) < 0 })
	e.Projections = nil
	e.Sections[4].Raw = nil
	projectionIndex := map[string]uint16{}
	for i, b := range bodies {
		if i > 0 && bytes.Equal(b.raw, bodies[i-1].raw) {
			continue
		}
		if len(e.Projections) >= 66 {
			return storagebudget.ErrBounds
		}
		var decoded storageEnrollmentPeakProjection
		if e.Version == 3 {
			decoded, err = storagePolicyReadProjection(b.raw, e.Inner.Catalog, e.OriginalV3, e.CompositionLineage, e.EffectiveSets, record, claim)
		} else if e.Version == 2 {
			decoded, err = storageEnrollmentPeakReadProjectionV2(b.raw, e.Inner.Catalog, e.OriginalV3, e.CompositionLineage, record, claim)
		} else {
			decoded, err = storageEnrollmentPeakReadProjection(b.raw, e.Inner.Catalog, e.OriginalV3, record, claim)
		}
		if err != nil {
			return err
		}
		projectionIndex[string(b.raw)] = uint16(len(e.Projections))
		e.Projections = append(e.Projections, decoded)
		e.Sections[4].Raw = append(e.Sections[4].Raw, b.raw...)
	}
	e.Sections[4].Count = uint32(len(e.Projections))
	for _, m := range ordered {
		raw, err := writeProjection(m.projection)
		if err != nil {
			return err
		}
		m.origin.Projection = projectionIndex[string(raw)]
	}
	e.Witnesses = witnesses
	e.Board.Origins = nil
	e.Board.Coverage = nil
	originIndex := map[storagebudget.ID]uint16{}
	for i, m := range ordered {
		originIndex[m.origin.ID] = uint16(i)
	}
	for i := range e.Witnesses {
		w := &e.Witnesses[i]
		id, ok := ownerOrigin[w.Slot]
		if !ok {
			return storagebudget.ErrBounds
		}
		index := originIndex[id]
		w.Origin = index
		ordered[index].origin.Witnesses = append(ordered[index].origin.Witnesses, uint16(i))
		if w.Selected {
			state := byte(2)
			if ordered[index].fresh {
				state = 1
			}
			e.Board.Coverage = append(e.Board.Coverage, storageEnrollmentPeakCoverage{Slot: w.Slot, Origin: index, Witness: uint16(i), Generation: w.Generation, Operation: w.Operation, Page: w.Digest, Epoch: e.AfterEpoch, Bridge: e.Bridge, State: state})
		}
	}
	for _, m := range ordered {
		o := m.origin
		w := storageEnrollmentPeakWriter{max: 1024}
		w.put(o.ID[:])
		w.put(o.Bridge[:])
		w.u64(o.Epoch)
		w.u64(o.Revision)
		w.u16(o.View)
		w.u16(o.Projection)
		w.u64(o.Slots)
		w.u16(uint16(len(o.Witnesses)))
		for _, ref := range o.Witnesses {
			w.u16(ref)
		}
		if w.bad {
			return storagebudget.ErrBounds
		}
		o.Digest = storageEnrollmentPeakHash("desk-enrollment-origin-record-e1", w.raw)
		e.Board.Origins = append(e.Board.Origins, o)
	}
	e.Sections[5].Raw, err = storageEnrollmentPeakWriteWitnesses(e.Witnesses, e.Installation, e.QBinding)
	if err != nil {
		return err
	}
	e.Sections[5].Count = uint32(len(e.Witnesses))
	var scope storageEnrollmentPeakScope
	storageEnrollmentPeakPut16(scope[:2], 2)
	scope[325] = e.Checkpoint.Bank
	copy(scope[120:152], e.Checkpoint.Checkpoints[e.Checkpoint.Bank][:])
	if e.Version == 3 {
		e.Checkpoint.Raw, err = storagePolicyWriteCheckpoint(e.Checkpoint, e.Inner.Catalog, e.Views, scope, e.EffectiveSets)
	} else {
		e.Checkpoint.Raw, err = storageEnrollmentPeakWriteCheckpoint(e.Checkpoint, e.Inner.Catalog, e.Views, scope)
	}
	if err != nil {
		return err
	}
	e.Sections[6].Raw = append([]byte{}, e.Checkpoint.Raw...)
	return nil
}
