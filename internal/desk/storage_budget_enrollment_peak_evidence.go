package desk

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"reflect"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

type storageEnrollmentPeakWitness struct {
	Slot          uint16
	Bank, Version uint8
	Generation    uint64
	Operation     storagebudget.ID
	Digest        storagebudget.Digest
	Origin        uint16
	Selected      bool
	Phase, Branch uint8
	Page          []byte
	Core          storageIntentRecord
	Identity      *storageIdentityIntent
	Creation      *storageCreationIntent
}
type storageEnrollmentPeakSection struct {
	Count          uint32
	Offset, Length uint64
	Digest         storagebudget.Digest
	Raw            []byte
}
type storageEnrollmentPeakEvidence struct {
	Version                                                         uint32
	Generation, Packed, Extent                                      uint64
	Phase, Bank                                                     uint8
	Installation                                                    storagebudget.ID
	QBinding                                                        storagebudget.Digest
	Bridge                                                          storagebudget.ID
	Previous, Record, Claim, Ready, Before, BeforeInner, AfterInner storagebudget.Digest
	BeforeRevision, BeforeEpoch, AfterRevision, AfterEpoch          uint64
	Controls                                                        [4]storageOwnerPersistentObject
	Project                                                         storageOwnerPersistentObject
	RegistrationDigest, CensusDigest                                storagebudget.Digest
	CoveredEpoch                                                    uint64
	Scope                                                           storageEnrollmentPeakScope
	Sections                                                        [9]storageEnrollmentPeakSection
	OriginalV3                                                      []byte
	Inner                                                           storageEnrollmentPeakInner
	Tickets                                                         []storagebudget.Ticket
	Views                                                           []storageEnrollmentPeakView
	Projections                                                     []storageEnrollmentPeakProjection
	Witnesses                                                       []storageEnrollmentPeakWitness
	Checkpoint                                                      storageEnrollmentPeakCheckpointProjection
	Census                                                          storageEnrollmentPeakQCensus
	Board                                                           storageEnrollmentPeakBoard
	Registration                                                    storagebudget.Registration
	Predictions                                                     [3]*storageEnrollmentPeakQCheckpoint
	PredictionBytes                                                 [3][]byte
	Digests                                                         storageEnrollmentPeakBoardDigests
	Raw                                                             []byte
	// Planning-only generated candidate; never encoded in evidence. The
	// successor Snapshot retains it even if canonical rebasing discards it.
	FreshOrigin storagebudget.ID
	// External immutable row context supplied by the selected physical wrapper.
	// Never serialized, discovered afresh or accepted as native/Q authority.
	CompositionLineage *storageCompositionLineage
	// Version3 only: canonical complete effective sets from section2.
	EffectiveSets []storagePolicyEffectiveSet
}

func storageEnrollmentPeakReadWitnesses(raw []byte, count uint32, installation storagebudget.ID, binding storagebudget.Digest) ([]storageEnrollmentPeakWitness, error) {
	if count > 128 || len(raw) != int(count)*16512 {
		return nil, storagebudget.ErrBounds
	}
	r := storageEnrollmentPeakReader{raw: raw}
	out := make([]storageEnrollmentPeakWitness, 0, count)
	for i := uint32(0); i < count; i++ {
		header := storageEnrollmentPeakReader{raw: r.take(128)}
		w := storageEnrollmentPeakWitness{Slot: header.u16(), Bank: header.u8(), Version: header.u8(), Generation: header.u64(), Operation: header.id(), Digest: header.digest(), Origin: header.u16()}
		selected := header.u8()
		w.Selected = selected == 1
		w.Phase = header.u8()
		w.Branch = header.u8()
		reserved := header.u8()
		if header.bad || !header.zeroTail() || w.Slot >= 64 || w.Bank > 1 || selected > 1 || reserved != 0 || w.Branch > 2 || len(out) > 0 && (out[len(out)-1].Slot > w.Slot || out[len(out)-1].Slot == w.Slot && out[len(out)-1].Bank >= w.Bank) {
			return nil, storagebudget.ErrBounds
		}
		w.Page = append([]byte{}, r.take(16384)...)
		if r.bad || sha256.Sum256(w.Page) != w.Digest {
			return nil, storagebudget.ErrBounds
		}
		if storageEnrollmentPeakZero(w.Page) {
			if w.Selected || w.Generation != 0 || w.Operation != (storagebudget.ID{}) || w.Phase != 0 || w.Branch != 0 || w.Version != 0 {
				return nil, storagebudget.ErrBounds
			}
		} else {
			switch w.Version {
			case 2, 8, 9:
				core, err := storageDecodeIntent(w.Page, int(w.Slot), installation, binding)
				if err != nil {
					return nil, err
				}
				if (w.Version == 8) != (core.Fanout != nil) || (w.Version == 9) != (core.Transfer != nil) {
					return nil, storagebudget.ErrBounds
				}
				w.Core = core
			case 3:
				identity, err := storageDecodeIdentityIntent(w.Page, int(w.Slot), installation, binding)
				if err != nil {
					return nil, err
				}
				w.Core = identity.Core
				w.Identity = &identity
			case 4, 6:
				creation, err := storageDecodeCreationIntent(w.Page, int(w.Slot), installation, binding)
				if err != nil {
					return nil, err
				}
				if (w.Version == 6) != (creation.CapacityPolicy == storageEnrollmentCapacityEstimatePolicy) {
					return nil, storagebudget.ErrBounds
				}
				w.Core = creation.Core
				w.Creation = &creation
			default:
				return nil, storagebudget.ErrBounds
			}
			if w.Core.Generation != w.Generation || w.Core.OperationID != w.Operation || byte(w.Core.Phase) != w.Phase {
				return nil, storagebudget.ErrBounds
			}
		}
		out = append(out, w)
	}
	if !r.done() {
		return nil, storagebudget.ErrBounds
	}
	return out, nil
}
func storageEnrollmentPeakHostState(v storageEnrollmentPeakView) storagebudget.HostState {
	return storagebudget.HostState{Revision: v.Revision, Epoch: v.Epoch, Maintenance: v.Maintenance, Slots: v.Slots}
}
func storageEnrollmentPeakProofs(e *storageEnrollmentPeakEvidence) bool {
	if e == nil || !storageEnrollmentDefaultActionProof(*e) || !storageTransferNamespaceProof(*e) {
		return false
	}
	b := e.Board
	current := e.Views[e.Checkpoint.View]
	selected := map[uint16]int{}
	used := map[uint16]bool{}
	for oi, o := range b.Origins {
		v := e.Views[o.View]
		p := e.Projections[o.Projection]
		if v.Revision != o.Revision || v.Epoch != o.Epoch || e.Version != 3 && v.Policy != e.Inner.Catalog.Policy || e.Version == 3 && !storagePolicyViewProjectionPair(v, p, e.Inner.Catalog) {
			return false
		}
		if len(v.DefinitionRefs[2]) != len(p.Catalog.Roots) || len(v.DefinitionRefs[3]) != len(p.Catalog.Workspaces) || len(v.DefinitionRefs[0]) != len(p.Catalog.Filesystems) || len(v.DefinitionRefs[1]) != len(p.Catalog.Constraints) {
			return false
		}
		for kind, refs := range v.DefinitionRefs {
			for _, index := range refs {
				var id storagebudget.ID
				found := false
				switch kind {
				case 0:
					id = e.Inner.Catalog.Filesystems[index].Definition.ID
					for _, row := range p.Catalog.Filesystems {
						found = found || row.Definition.ID == id
					}
				case 1:
					id = e.Inner.Catalog.Constraints[index].Definition.ID
					for _, row := range p.Catalog.Constraints {
						found = found || row.Definition.ID == id
					}
				case 2:
					id = e.Inner.Catalog.Roots[index].Definition.ID
					for _, row := range p.Catalog.Roots {
						found = found || row.Definition.ID == id
					}
				case 3:
					id = e.Inner.Catalog.Workspaces[index].Definition.ID
					for _, row := range p.Catalog.Workspaces {
						found = found || row.Definition.ID == id
					}
				}
				if !found {
					return false
				}
			}
		}
		mask := uint64(0)
		counts := [64]int{}
		for _, ref := range o.Witnesses {
			if used[ref] {
				return false
			}
			used[ref] = true
			w := e.Witnesses[ref]
			if int(w.Origin) != oi || o.Slots&(uint64(1)<<w.Slot) == 0 {
				return false
			}
			counts[w.Slot]++
			mask |= uint64(1) << w.Slot
			if w.Selected {
				if _, ok := selected[w.Slot]; ok {
					return false
				}
				selected[w.Slot] = int(ref)
				core := w.Core
				if w.Creation != nil && core.Phase == storageIntentRetired && (!e.Scope.hasOriginalPeer() || !storageCreationTailBound(*w.Creation, *e)) {
					return false
				}
				if core.Transfer != nil && core.Phase == storageIntentRetired && !storageTransferTailBound(core, *e) {
					return false
				}
				if core.Phase == storageIntentRejected {
					if w.Branch != 0 || core.AdmissionEpoch != v.Epoch || core.Transfer != nil && !storageTransferCompactNoTicket(core, storageEnrollmentPeakHostState(v), true) {
						return false
					}
					for _, slot := range v.Slots {
						if storageIntentMatchingTicket(core, slot.Ticket, e.QBinding) {
							return false
						}
					}
				} else if core.Phase == storageIntentRetired {
					state := storageEnrollmentPeakHostState(v)
					a := state.Slots[core.Token.Slot]
					switch w.Branch {
					case 1:
						if a.PersistedApplied != core.Token || a.PersistedNextSequence <= core.Token.Sequence {
							return false
						}
						if !storageIntentFanoutRetiredOperand(core, p.Catalog, a) {
							return false
						}
						if w.Identity != nil && !storageIdentityIntentExactRetired(*w.Identity, state, e.QBinding) {
							return false
						}
					case 2:
						if v.Epoch != core.RetirementEpoch || a.PersistedApplied.ID == (storagebudget.ID{}) || a.PersistedApplied.Sequence <= core.Token.Sequence || a.PersistedApplied.Generation == 0 || a.PersistedNextSequence <= a.PersistedApplied.Sequence {
							return false
						}
					default:
						return false
					}
				} else {
					return false
				}
				if core.Fanout != nil || core.Transfer != nil {
					if !e.Scope.capacityEstimate() {
						return false
					}
					if _, err := storageCatalogIntentFanout(core, p.Catalog); err != nil {
						return false
					}
				}
				for i, component := range core.Components {
					rootFound, fsFound := false, false
					for _, root := range p.Catalog.Roots {
						d := root.Definition
						if d.ID == component.Root && d.Owner == component.Owner && d.Generation == component.RootGeneration && d.Identity == core.RootIdentities[i] && d.Locator == core.Locators[i] && d.Filesystem == component.Filesystem && d.CapacityBinding == component.CapacityBinding && d.CapacityCount == component.CapacityCount {
							rootFound = true
							for j := 0; j < int(d.CapacityCount); j++ {
								if d.CapacityRefs[j].Constraint != component.Capacity[j].Constraint || d.CapacityRefs[j].Generation != component.Capacity[j].Generation {
									rootFound = false
								}
							}
						}
					}
					for _, f := range p.Catalog.Filesystems {
						if f.Definition.ID == component.Filesystem && f.Definition.Generation == component.FilesystemGeneration && f.Definition.Identity == core.FilesystemIdentities[i] {
							fsFound = true
						}
					}
					if !rootFound || !fsFound {
						return false
					}
				}
			}
		}
		if mask != o.Slots {
			return false
		}
		for slot := 0; slot < 64; slot++ {
			if o.Slots&(uint64(1)<<slot) != 0 {
				var pair [2][]byte
				for _, ref := range o.Witnesses {
					w := e.Witnesses[ref]
					if int(w.Slot) == slot {
						pair[w.Bank] = w.Page
					}
				}
				choice, err := storageSelectCreationHost(pair[0], pair[1], slot, e.Installation, e.QBinding)
				if err != nil {
					return false
				}
				chosenBank := choice.Slot.bank
				index, ok := selected[uint16(slot)]
				if !ok || int(e.Witnesses[index].Bank) != chosenBank {
					return false
				}
			}
		}
		for slot := 0; slot < 64; slot++ {
			if o.Slots&(uint64(1)<<slot) != 0 && counts[slot] != 2 {
				return false
			}
		}
	}
	if len(used) != len(e.Witnesses) {
		return false
	}
	for _, v := range b.Coverage {
		index, ok := selected[v.Slot]
		if !ok || int(v.Witness) != index {
			return false
		}
		w := e.Witnesses[index]
		o := b.Origins[v.Origin]
		if w.Generation != v.Generation || w.Operation != v.Operation || w.Digest != v.Page || v.Epoch != e.CoveredEpoch || v.Bridge != e.Bridge {
			return false
		}
		if v.State == 1 && o.Epoch != current.Epoch || v.State == 2 && o.Epoch > current.Epoch {
			return false
		}
		core := w.Core
		if core.Phase == storageIntentRejected {
			if core.Transfer != nil && !storageTransferCompactNoTicket(core, storageEnrollmentPeakHostState(current), false) {
				return false
			}
			for _, slot := range current.Slots {
				if storageIntentMatchingTicket(core, slot.Ticket, e.QBinding) {
					return false
				}
			}
		} else {
			a := current.Slots[core.Token.Slot]
			if a.PersistedApplied == core.Token {
				if a.PersistedNextSequence <= core.Token.Sequence {
					return false
				}
			} else if a.PersistedApplied.ID == (storagebudget.ID{}) || a.PersistedApplied.Sequence <= core.Token.Sequence || a.PersistedApplied.Generation == 0 || a.PersistedNextSequence <= a.PersistedApplied.Sequence {
				return false
			}
		}
	}
	return true
}

func storageEnrollmentPeakDecodeEvidence(raw []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageEnrollmentPeakEvidence, error) {
	return storageEnrollmentPeakDecodeEvidenceWithLineage(raw, nil, record, claim)
}

func storageEnrollmentPeakDecodeEvidenceWithLineage(raw []byte, retained *storageCompositionLineage, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageEnrollmentPeakEvidence, error) {
	return storageEnrollmentPeakDecodeEvidenceOperands(raw, retained, record, claim, nil)
}

// The same strict decoder. Nonnil operands only reuse this epoch's exact
// earlier results for pure sub-decodes (original peer restoration, original
// checkpoint pair, full projection digests) on identical bytes and context.
func storageEnrollmentPeakDecodeEvidenceOperands(raw []byte, retained *storageCompositionLineage, record storageOwnerRecordV2, claim storageOwnerClaimV2, operands *storageEnrollmentOperands) (storageEnrollmentPeakEvidence, error) {
	var e storageEnrollmentPeakEvidence
	fail := func() (storageEnrollmentPeakEvidence, error) {
		return storageEnrollmentPeakEvidence{}, storagebudget.ErrBounds
	}
	if len(raw) < 65536 || len(raw) > storageEnrollmentPeakEvidenceMax || len(raw)%65536 != 0 {
		return fail()
	}
	r := storageEnrollmentPeakReader{raw: raw[:4096]}
	magic := string(r.take(8))
	e.Version = r.u32()
	if (e.Version != 1 || magic != "DSKEPP1\x00") && (e.Version != 2 || magic != "DSKEPP2\x00") && (e.Version != 3 || magic != "DSKEPP3\x00") || r.u32() != 65536 {
		return fail()
	}
	if e.Version == 2 || e.Version == 3 {
		e.CompositionLineage = retained
	}
	e.Generation = r.u64()
	e.Packed = r.u64()
	e.Extent = r.u64()
	e.Phase = r.u8()
	e.Bank = r.u8()
	features := r.u16()
	e.Installation = r.id()
	e.QBinding = r.digest()
	e.Bridge = r.id()
	e.Previous = r.digest()
	e.Record = r.digest()
	e.Claim = r.digest()
	e.Ready = r.digest()
	e.Before = r.digest()
	e.BeforeInner = r.digest()
	e.AfterInner = r.digest()
	e.BeforeRevision = r.u64()
	e.BeforeEpoch = r.u64()
	e.AfterRevision = r.u64()
	e.AfterEpoch = r.u64()
	for i := range e.Controls {
		e.Controls[i] = r.native()
	}
	e.Project = r.native()
	e.RegistrationDigest = r.digest()
	e.CensusDigest = r.digest()
	e.CoveredEpoch = r.u64()
	if r.u16() != 9 {
		return fail()
	}
	last := uint64(65536)
	for i := range e.Sections {
		kind, flags := r.u16(), r.u16()
		s := storageEnrollmentPeakSection{Count: r.u32(), Offset: r.u64(), Length: r.u64()}
		maximum := r.u64()
		s.Digest = r.digest()
		if kind != uint16(i+1) || flags != 0 || maximum != storageEnrollmentPeakEvidenceSectionMaximum(e.Version, func() storageEnrollmentPeakScope {
			var s storageEnrollmentPeakScope
			copy(s[:], raw[4096:8192])
			return s
		}(), i) || s.Count > storageEnrollmentPeakSectionCounts[i] || s.Length > maximum || s.Offset%8 != 0 || s.Offset < last || s.Offset > uint64(len(raw)) || s.Length > uint64(len(raw))-s.Offset || !storageEnrollmentPeakZero(raw[last:s.Offset]) {
			return fail()
		}
		if (i == 0 || i == 1 || i == 6 || i == 7 || i == 8) && s.Count != 1 {
			return fail()
		}
		s.Raw = raw[s.Offset : s.Offset+s.Length]
		if sha256.Sum256(s.Raw) != s.Digest {
			return fail()
		}
		e.Sections[i] = s
		last = s.Offset + s.Length
	}
	checksumOffset := r.pos
	checksum := r.digest()
	if r.bad || !r.zeroTail() || e.Packed != last-65536 || e.Extent != uint64(len(raw)) || !storageEnrollmentPeakZero(raw[last:]) || !storageEnrollmentPeakZero(raw[8192:65536]) || e.Generation == 0 || e.Generation == 1 && e.Previous != (storagebudget.Digest{}) || e.Generation > 1 && e.Previous == (storagebudget.Digest{}) || e.Phase > 2 || e.Bank > 1 || features != 7 || e.Installation != record.Installation || e.QBinding != record.QBinding || !claim.matches(record) {
		return fail()
	}
	h := sha256.New()
	h.Write([]byte("desk-enrollment-bank-peak-v1\x00"))
	h.Write(raw[:checksumOffset])
	h.Write(make([]byte, 32))
	h.Write(raw[checksumOffset+32:])
	if !bytes.Equal(h.Sum(nil), checksum[:]) {
		return fail()
	}
	scope, err := storageEnrollmentPeakDecodeScopeVariant(raw[4096:8192], e.Version == 2 || e.Version == 3)
	if err != nil {
		return fail()
	}
	e.Scope = scope
	if (e.Version == 2 || e.Version == 3) && !scope.hasOriginalPeer() {
		return fail()
	}
	if scope.path() == 2 && e.Phase == 0 || e.Phase > 0 && e.Bank != storageEnrollmentPeakEvidenceBank(scope, e.Phase) {
		return fail()
	}
	if scope[321] != 15 || e.Extent != scope.final(e.Bank) || !bytes.Equal(e.Bridge[:], scope[24:40]) || !bytes.Equal(e.Before[:], scope[88:120]) || e.Project != storageEnrollmentPeakNative(scope[286:321]) {
		return fail()
	}
	for i, p := range e.Controls {
		if p != scope.target(byte(i+1)) {
			return fail()
		}
	}
	section1 := e.Sections[0].Raw
	if len(section1) < 64 || len(section1) != 64+int(storageEnrollmentPeakU32(section1[24:28])) {
		return fail()
	}
	e.OriginalV3 = make([]byte, 8388608)
	copy(e.OriginalV3, section1)
	if _, err := storageOwnerDecodeCatalogV3(e.OriginalV3, record, claim); err != nil {
		return fail()
	}
	if scope.path() == 1 && sha256.Sum256(e.OriginalV3) != e.Before {
		return fail()
	}
	if scope.hasOriginalPeer() {
		if err := operands.peerValid(scope, e.OriginalV3, record, claim, e.Version == 2 || e.Version == 3); err != nil {
			return fail()
		}
	}
	e.Inner, err = storageEnrollmentPeakDecodeInner(e.Sections[1].Raw, record, claim)
	if err != nil || e.Inner.Controls != e.Controls || !scope.checkCapacity(e.Inner.Catalog) {
		return fail()
	}
	if e.Version == 3 {
		e.Tickets, e.EffectiveSets, err = storagePolicyReadEffectiveTable(e.Sections[2].Raw, e.Sections[2].Count, e.Inner.Catalog)
	} else {
		e.Tickets, err = storageEnrollmentPeakReadTickets(e.Sections[2].Raw, e.Sections[2].Count)
	}
	if err != nil {
		return fail()
	}
	for _, ticket := range e.Tickets {
		if ticket.Fanout != nil && !scope.capacityEstimate() {
			return fail()
		}
	}
	if e.Version == 3 {
		e.Views, err = storagePolicyReadViews(e.Sections[3].Raw, e.Sections[3].Count, e.Inner.Catalog, e.Tickets, e.EffectiveSets)
	} else {
		e.Views, err = storageEnrollmentPeakReadViews(e.Sections[3].Raw, e.Sections[3].Count, e.Inner.Catalog, e.Tickets)
	}
	if err != nil {
		return fail()
	}
	pr := storageEnrollmentPeakReader{raw: e.Sections[4].Raw}
	var previous []byte
	for i := uint32(0); i < e.Sections[4].Count; i++ {
		if pr.pos > len(pr.raw)-8 {
			return fail()
		}
		tagged := e.Version == 3 && len(pr.raw)-pr.pos >= 32 && (string(pr.raw[pr.pos:pr.pos+8]) == "DSKV6P1\x00" || string(pr.raw[pr.pos:pr.pos+8]) == "DSKV6P2\x00" || string(pr.raw[pr.pos:pr.pos+8]) == "DSKV6H1\x00")
		n := uint64(storageEnrollmentPeakU32(pr.raw[pr.pos+4 : pr.pos+8]))
		if tagged {
			if string(pr.raw[pr.pos:pr.pos+8]) == "DSKV6H1\x00" {
				n = uint64(storageEnrollmentPeakU32(pr.raw[pr.pos+12 : pr.pos+16]))
			} else {
				n = uint64(storageEnrollmentPeakU32(pr.raw[pr.pos+28 : pr.pos+32]))
			}
		}
		if n > 135168 || n < 8 {
			return fail()
		}
		rowN := n
		if !tagged && (e.Version == 2 || e.Version == 3) {
			rowN += storageCompositionProjectionBytes
		}
		if e.Version == 3 && rowN > 135168 {
			return fail()
		}
		body := pr.take(int(rowN))
		if pr.bad || i > 0 && bytes.Compare(previous, body) >= 0 {
			return fail()
		}
		var p storageEnrollmentPeakProjection
		var err error
		if e.Version == 3 {
			p, err = storagePolicyReadProjection(body, e.Inner.Catalog, e.OriginalV3, retained, e.EffectiveSets, record, claim)
		} else if e.Version == 2 {
			p, err = storageEnrollmentPeakReadProjectionV2(body, e.Inner.Catalog, e.OriginalV3, retained, record, claim)
		} else {
			p, err = storageEnrollmentPeakReadProjection(body, e.Inner.Catalog, e.OriginalV3, record, claim)
		}
		if err != nil {
			return fail()
		}
		e.Projections = append(e.Projections, p)
		previous = body
	}
	if !pr.done() {
		return fail()
	}
	e.Witnesses, err = storageEnrollmentPeakReadWitnesses(e.Sections[5].Raw, e.Sections[5].Count, e.Installation, e.QBinding)
	for _, witness := range e.Witnesses {
		if (witness.Core.Fanout != nil || witness.Core.Transfer != nil) && !scope.capacityEstimate() {
			return fail()
		}
	}
	if storageEnrollmentPeakU16(scope[:2]) == 1 {
		for _, w := range e.Witnesses {
			if w.Creation != nil {
				return fail()
			}
		}
	}
	if err != nil {
		return fail()
	}
	if e.Version == 3 {
		e.Checkpoint, err = storagePolicyReadCheckpointOperands(e.Sections[6].Raw, e.Inner.Catalog, e.Views, scope, e.EffectiveSets, operands)
	} else {
		e.Checkpoint, err = storageEnrollmentPeakReadCheckpointOperands(e.Sections[6].Raw, e.Inner.Catalog, e.Views, scope, operands)
	}
	if err != nil {
		return fail()
	}
	q0 := e.Checkpoint.Q
	if !storageEnrollmentPeakBeforeDefinitions(e.Inner.Catalog, e.Checkpoint) {
		return fail()
	}
	if e.BeforeRevision != q0.Generation || e.BeforeEpoch != q0.Epoch || q0.Generation > ^uint64(0)-3 || q0.Epoch == ^uint64(0) || e.AfterRevision != q0.Generation+3 || e.AfterEpoch != q0.Epoch+1 || e.CoveredEpoch != e.AfterEpoch || e.Inner.Catalog.ExpectedQRevision != q0.Generation+1 || e.Inner.Catalog.Transition != e.Bridge || (e.Version != 3 && e.Inner.Catalog.Policy != e.Views[e.Checkpoint.View].Policy) || sha256.Sum256(e.Views[e.Checkpoint.View].Raw) != *(*storagebudget.Digest)(scope[152:184]) {
		return fail()
	}
	// Locate the exact before projection by full original bank digest. Its
	// complete canonical DTO supplies the before-inner comparison material.
	matches := 0
	for _, p := range e.Projections {
		if operands.projectionDigest(p) == e.Before {
			matches++
			j, _ := json.Marshal(p.Catalog)
			if sha256.Sum256(j) != e.BeforeInner {
				return fail()
			}
			if p.Catalog.Sequence == ^uint64(0) || e.Inner.Catalog.Sequence != p.Catalog.Sequence+1 || !bytes.Equal(e.Inner.Catalog.PreviousCatalogDigest[:], p.SavedOuter[32:64]) {
				return fail()
			}
			_, old, err := p.Catalog.definitions()
			before, _, err2 := e.Inner.Catalog.definitions()
			if err != nil || err2 != nil || !reflect.DeepEqual(old, before) {
				return fail()
			}
		}
	}
	if matches != 1 {
		return fail()
	}
	if e.Version == 2 {
		if _, err := e.compositionBefore(record, claim); err != nil {
			return fail()
		}
	}
	afterJSON, _ := json.Marshal(e.Inner.Catalog)
	if sha256.Sum256(afterJSON) != e.AfterInner {
		return fail()
	}
	recordRaw, err := storageOwnerEncodeRecordV2(record)
	if err != nil || sha256.Sum256(recordRaw) != e.Record {
		return fail()
	}
	claimRaw, err := storageOwnerEncodeClaimV2(claim)
	if err != nil || sha256.Sum256(claimRaw) != e.Claim || e.Ready == (storagebudget.Digest{}) {
		return fail()
	}
	// Registration indices precede a variable source only after the finite
	// board/origins/coverage. A first bounded pass extracts exactly that tail;
	// the complete strict board reader below validates it and every earlier row.
	tail, err := storageEnrollmentPeakBoardTail(e.Sections[8].Raw)
	if err != nil {
		return fail()
	}
	e.Registration, err = storageEnrollmentPeakRegistration(e.Inner.Catalog, tail.Registration, q0)
	if err != nil || !storageEnrollmentPeakRegistrationJoins(tail, e.Inner.Catalog) {
		return fail()
	}
	regDigest, err := storageEnrollmentPeakRegistrationDigest(e.Registration)
	if err != nil || regDigest != e.RegistrationDigest {
		return fail()
	}
	e.Predictions, err = storageEnrollmentPeakPredict(q0, e.Registration)
	if err != nil {
		return fail()
	}
	e.Census, err = storageEnrollmentPeakReadCensus(e.Sections[7].Raw, e.Predictions[1], e.Phase)
	if err != nil || sha256.Sum256(e.Sections[7].Raw) != e.CensusDigest {
		return fail()
	}
	if e.Phase != 0 {
		q3 := *e.Predictions[1]
		q3.Generation++
		q3.Maintenance = false
		if storageEnrollmentPeakQPublishCensus(&q3, e.Census) != nil || !(storageEnrollmentPeakQVector{67108864, 16, 1}).le(q3.Installation[storageEnrollmentPeakQControl]) || storageEnrollmentPeakQValidateCheckpoint(&q3) != nil {
			return fail()
		}
		e.Predictions[2] = &q3
	}
	anchor := storageEnrollmentPeakQHeldAnchor{Installation: storageEnrollmentPeakQID(e.Installation), Binding: storageEnrollmentPeakQDigest(e.QBinding)}
	for i, q := range e.Predictions {
		if q == nil {
			continue
		}
		raw, err := storageEnrollmentPeakQEncodeCheckpoint(q, anchor)
		if err != nil {
			return fail()
		}
		e.PredictionBytes[i] = raw
		e.Digests.Q[i] = sha256.Sum256(raw)
	}
	originMaterial := append(append(append(append(append([]byte{}, e.Installation[:]...), e.QBinding[:]...), e.Bridge[:]...), storageEnrollmentPeakNativeBytes(e.Project)...), regDigest[:]...)
	e.Digests.Origin = storageEnrollmentPeakHash("desk-enrollment-origin-e1", originMaterial)
	e.Board, err = storageEnrollmentPeakReadBoard(e.Sections[8].Raw, scope, e.Phase, e.Digests, e.Inner.Catalog, len(e.Views), len(e.Projections), len(e.Witnesses))
	if err != nil || e.Board.Source.Project != e.Project || !storageEnrollmentPeakProofs(&e) || e.Version == 3 && !storagePolicyEvidenceSets(e) {
		return fail()
	}
	if storageEnrollmentPeakU16(scope[:2]) == 1 {
		if e.Inner.Catalog.validateUses() != nil {
			return fail()
		}
		for _, root := range e.Inner.Catalog.Roots {
			if root.Role == 8 || root.Source.Kind == 9 {
				return fail()
			}
		}
	}
	for _, projection := range e.Projections {
		if operands.projectionDigest(projection) == e.Before && !storageEnrollmentRetainsOriginalWithDefault(projection.Catalog, e.Inner.Catalog, &e.Board.Source) {
			return fail()
		}
	}
	e.Raw = append([]byte{}, raw...)
	return e, nil
}

func storageEnrollmentPeakBoardTail(raw []byte) (storageEnrollmentPeakBoard, error) {
	var b storageEnrollmentPeakBoard
	r := storageEnrollmentPeakReader{raw: raw}
	r.take(2 + 16 + 70 + 32)
	n := int(r.u8())
	if n > 8 {
		return b, storagebudget.ErrBounds
	}
	r.take(n*24 + 24 + 8 + 16 + 4)
	n = int(r.u8())
	if n > 62 {
		return b, storagebudget.ErrBounds
	}
	r.take(n * 115)
	n = int(r.u16())
	if n > 64 {
		return b, storagebudget.ErrBounds
	}
	for i := 0; i < n; i++ {
		r.take(16 + 16 + 8 + 8 + 2 + 2 + 8)
		m := int(r.u16())
		if m > 128 {
			return b, storagebudget.ErrBounds
		}
		r.take(m*2 + 32)
	}
	n = int(r.u16())
	if n > 64 {
		return b, storagebudget.ErrBounds
	}
	r.take(n*87 + 96)
	limits := [4]int{1, 0, 1, 4}
	for k := range b.Registration {
		n := int(r.u16())
		if n > limits[k] {
			return b, storagebudget.ErrBounds
		}
		b.Registration[k] = make([]uint16, n)
		for i := range b.Registration[k] {
			b.Registration[k][i] = r.u16()
		}
	}
	b.SourceKind = r.u8()
	source, err := storageEnrollmentPeakScanSource(&r)
	if err != nil || !r.zeroTail() {
		return b, storagebudget.ErrBounds
	}
	b.Source = source
	return b, nil
}

func storageEnrollmentPeakEncodeEvidence(e storageEnrollmentPeakEvidence, record storageOwnerRecordV2, claim storageOwnerClaimV2) ([]byte, error) {
	return storageEnrollmentPeakEncodeEvidenceOperands(e, record, claim, nil)
}
func storageEnrollmentPeakEncodeEvidenceOperands(e storageEnrollmentPeakEvidence, record storageOwnerRecordV2, claim storageOwnerClaimV2, operands *storageEnrollmentOperands) ([]byte, error) {
	if e.Version == 0 {
		e.Version = 1
	}
	if e.Version != 1 && e.Version != 2 && e.Version != 3 {
		return nil, storagebudget.ErrBounds
	}
	if e.Version == 1 {
		for _, p := range e.Projections {
			if p.Physical != (storageCompositionProjection{}) {
				return nil, storagebudget.ErrBounds
			}
		}
	}
	if e.Extent < 65536 || e.Extent > 71303168 || e.Extent%65536 != 0 {
		return nil, storagebudget.ErrBounds
	}
	raw := make([]byte, int(e.Extent))
	w := storageEnrollmentPeakWriter{max: 4096}
	if e.Version == 3 {
		w.put([]byte("DSKEPP3\x00"))
	} else if e.Version == 2 {
		w.put([]byte("DSKEPP2\x00"))
	} else {
		w.put([]byte("DSKEPP1\x00"))
	}
	w.u32(e.Version)
	w.u32(65536)
	w.u64(e.Generation)
	last := uint64(65536)
	sections := e.Sections
	for i := range sections {
		offset := (last + 7) &^ uint64(7)
		if sections[i].Count > storageEnrollmentPeakSectionCounts[i] || uint64(len(sections[i].Raw)) > storageEnrollmentPeakEvidenceSectionMaximum(e.Version, e.Scope, i) || offset > e.Extent || uint64(len(sections[i].Raw)) > e.Extent-offset {
			return nil, storagebudget.ErrBounds
		}
		sections[i].Offset = offset
		sections[i].Length = uint64(len(sections[i].Raw))
		sections[i].Digest = sha256.Sum256(sections[i].Raw)
		copy(raw[offset:], sections[i].Raw)
		last = offset + sections[i].Length
	}
	w.u64(last - 65536)
	w.u64(e.Extent)
	w.u8(e.Phase)
	w.u8(e.Bank)
	w.u16(7)
	w.put(e.Installation[:])
	w.put(e.QBinding[:])
	w.put(e.Bridge[:])
	for _, d := range []storagebudget.Digest{e.Previous, e.Record, e.Claim, e.Ready, e.Before, e.BeforeInner, e.AfterInner} {
		w.put(d[:])
	}
	for _, v := range []uint64{e.BeforeRevision, e.BeforeEpoch, e.AfterRevision, e.AfterEpoch} {
		w.u64(v)
	}
	for _, p := range e.Controls {
		w.put(storageEnrollmentPeakNativeBytes(p))
	}
	w.put(storageEnrollmentPeakNativeBytes(e.Project))
	w.put(e.RegistrationDigest[:])
	w.put(e.CensusDigest[:])
	w.u64(e.CoveredEpoch)
	w.u16(9)
	for i, s := range sections {
		w.u16(uint16(i + 1))
		w.u16(0)
		w.u32(s.Count)
		w.u64(s.Offset)
		w.u64(s.Length)
		w.u64(storageEnrollmentPeakEvidenceSectionMaximum(e.Version, e.Scope, i))
		w.put(s.Digest[:])
	}
	checksumOffset := len(w.raw)
	w.put(make([]byte, 32))
	if w.bad {
		return nil, storagebudget.ErrBounds
	}
	copy(raw, w.raw)
	copy(raw[4096:8192], e.Scope[:])
	digest := storageEnrollmentPeakHash("desk-enrollment-bank-peak-v1", raw)
	copy(raw[checksumOffset:checksumOffset+32], digest[:])
	if _, err := operands.evidence(raw, e.CompositionLineage, record, claim); err != nil {
		return nil, err
	}
	return raw, nil
}

func storageEnrollmentPeakEvidenceBank(s storageEnrollmentPeakScope, phase byte) byte {
	if s.path() == 1 {
		if phase == 2 {
			return 1
		}
		return 0
	}
	if s.path() == 2 {
		if phase == 2 {
			return s[323]
		}
		return 1 - s[323]
	}
	if phase == 2 || phase == 0 {
		return 1 - s[323]
	}
	return s[323]
}

// Return the exact historical physical before pair, leaving all existing
// Before/BeforeInner/prior and original V3 operands as CORE commitments.
func (e storageEnrollmentPeakEvidence) compositionBefore(record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageCompositionPair, error) {
	if e.Version != 2 || e.CompositionLineage == nil {
		return storageCompositionPair{}, storagebudget.ErrBounds
	}
	var result storageCompositionPair
	matches := 0
	for _, p := range e.Projections {
		if storageEnrollmentPeakProjectionFullDigest(p) != e.Before {
			continue
		}
		matches++
		if p.Physical == (storageCompositionProjection{}) {
			return storageCompositionPair{}, storagebudget.ErrBounds
		}
		var cutoffs [4]uint16
		for i := range cutoffs {
			cutoffs[i] = storageEnrollmentPeakU16(p.Physical[640+2*i : 642+2*i])
		}
		historical, err := e.CompositionLineage.prefix(cutoffs)
		if err != nil || historical.validateGraph(p.Catalog, record) != nil {
			return storageCompositionPair{}, storagebudget.ErrBounds
		}
		core, err := storageEnrollmentPeakProjectionCore(p)
		if err != nil {
			return storageCompositionPair{}, err
		}
		raw, err := p.Physical.restore(core, *e.CompositionLineage, record, claim)
		if err != nil {
			return storageCompositionPair{}, err
		}
		result, err = storageCompositionReadPair(raw[0], raw[1], record, claim)
		if err != nil || result.RecoveryOnly || result.Selected != e.Scope[322] || !bytes.Equal(result.Banks[result.Selected].Core, core) {
			return storageCompositionPair{}, storagebudget.ErrBounds
		}
	}
	if matches != 1 {
		return storageCompositionPair{}, storagebudget.ErrBounds
	}
	return result, nil
}

// The complete raw action page is selected by the ordinary origin/view/Q
// proof as well. This join authorizes only the closed default-history relation;
// matching hashes, native tuples or path text alone never issue live custody.
func storageEnrollmentDefaultActionProof(e storageEnrollmentPeakEvidence) bool {
	b := e.Board
	if b.SourceKind == 8 && b.Source.ActionPageDigest != (storagebudget.Digest{}) {
		return storageEnrollmentPrivateActionProof(e)
	}
	if b.SourceKind == 11 {
		if len(b.Registration[0]) == 0 {
			return storageTransferOwningAbortProof(e)
		}
		return b.Source.Default != nil && b.Source.Default.Workflow == 6 && storageEnrollmentDefaultSourceNamespace(b.Source)
	}
	if b.SourceKind != 9 && b.SourceKind != 10 {
		return b.Source.Default == nil && b.Source.ActionPageDigest == (storagebudget.Digest{})
	}
	if !e.Scope.hasOriginalPeer() || b.Source.Default == nil {
		return false
	}
	d := b.Source.Default
	if b.SourceKind == 10 && d.Workflow == 4 && d.OwnSelf == 0 {
		if len(b.Registration[0]) == 0 {
			return storageTransferRetentionProof(e)
		}
		return storageTransferNamespaceProof(e)
	}
	if b.SourceKind == 10 && d.Workflow == 2 && d.OwnSelf == 0 {
		return d.CreationPage == (storagebudget.Digest{})
	}
	if b.SourceKind == 9 && d.Workflow == 5 {
		return storageTransferFinalActionProof(e)
	}

	for _, w := range e.Witnesses {
		if !w.Selected || w.Creation == nil || w.Digest != d.CreationPage {
			continue
		}
		r := *w.Creation
		if r.Core.Phase != storageIntentRetired || r.Tail.Bridge != e.Bridge || r.Tail.Project != e.Project || r.Tail.Config != e.Scope.target(9) || r.Tail.Anchor != e.Scope.target(10) || !bytes.Equal(r.Tail.Qualification[:], e.Scope[408:440]) || r.SelectedPath != string(d.SelectedPath) || r.Branch != d.Branch || !storageCreationTailCatalog(r, e.Inner.Catalog) {
			return false
		}
		if b.SourceKind == 10 {
			return d.Workflow == 2 && d.OwnSelf == 1 && r.Kind == storageCreationAncestor && len(r.Effects) == 1 && r.Effects[0].Native == d.Selected
		}
		if d.Workflow != 1 && d.Workflow != 3 || r.Kind != storageCreationHistory {
			return false
		}
		if d.Workflow == 3 && !r.legacyConfigSelection() {
			return false
		}
		settings, selected, marker, location := false, false, d.Workflow == 3, false
		for _, p := range r.Parents {
			if p.Role == 5 && p.Native == d.Settings {
				settings = true
			}
			if p.Role == 6 && p.Native == d.Selected {
				selected = true
			}
		}
		for _, effect := range r.Effects {
			if effect.Kind == 1 && effect.Name == string(b.Source.Namespace) && effect.Native == d.Selected {
				selected = true
			}
			if effect.Kind == 2 && effect.Name == dataMarkerName && effect.Native == d.Marker {
				marker = true
			}
			if effect.Kind == 2 && effect.Name == dataLocationName && effect.Native == d.Location && effect.Payload == d.LocationPayload {
				location = true
			}
		}
		if !marker {
			// Existing compatible History/marker is immutable catalog evidence from
			// the complete immediate-before projection, independently of creation.
			for _, projection := range e.Projections {
				if storageEnrollmentPeakProjectionFullDigest(projection) != e.Before {
					continue
				}
				for _, object := range projection.Catalog.Objects {
					if object.Role == 23 && object.Persistent == d.Marker && int(object.Parent) < len(projection.Catalog.Objects) && projection.Catalog.Objects[object.Parent].Persistent == d.Selected {
						marker = true
					}
				}
			}
		}
		return settings && selected && marker && location && storageEnrollmentDefaultHistorySourceJoins(b.Source, e.Inner.Catalog)
	}
	return false
}

// Structural source/action closure only. The original issuer independently
// proves current Q/covered receipt, exact two-bank selection and native custody.
func storageEnrollmentPrivateActionProof(e storageEnrollmentPeakEvidence) bool {
	b := e.Board
	if b.SourceKind != 8 || b.Source.Default != nil || b.Source.ActionPageDigest == (storagebudget.Digest{}) || !e.Scope.hasOriginalPeer() || !storageEnrollmentPeakRegistrationJoins(b, e.Inner.Catalog) {
		return false
	}
	matches := 0
	for _, w := range e.Witnesses {
		if !w.Selected || w.Creation == nil || w.Digest != b.Source.ActionPageDigest {
			continue
		}
		r := *w.Creation
		if r.validate() != nil || r.Kind != storageCreationPrivate || r.Branch != storageCreationContained || r.Core.Phase != storageIntentRetired || r.Core.Outcome != storageIntentPublished || r.Tail.Installation != e.Installation || r.Tail.QBinding != e.QBinding || r.Tail.Bridge != e.Bridge || r.Tail.Project != e.Project || r.Tail.Config != e.Scope.target(9) || r.Tail.Anchor != e.Scope.target(10) || !bytes.Equal(r.Tail.Qualification[:], e.Scope[408:440]) || !storageCreationTailCatalog(r, e.Inner.Catalog) {
			return false
		}
		page, err := storageEncodeCreationIntent(r, e.Installation, e.QBinding)
		if err != nil || len(w.Page) != storageIntentPage || !bytes.Equal(page, w.Page) || sha256.Sum256(page) != b.Source.ActionPageDigest || len(r.Parents) != 1 || r.Parents[0].Role != 1 || r.Parents[0].Native != b.Source.Project {
			return false
		}
		child := storageOwnerPersistentObject{}
		for _, effect := range r.Effects {
			if effect.State != storageCreationDurable {
				return false
			}
			if effect.Parent == 0 && effect.Kind == 1 && effect.Name == ".desk-private" {
				if child != (storageOwnerPersistentObject{}) {
					return false
				}
				child = effect.Native
			}
		}
		if !child.valid(1) {
			return false
		}
		root := e.Inner.Catalog.Roots[b.Registration[0][0]]
		if e.Inner.Catalog.Objects[root.DirectoryObject].Persistent != child {
			return false
		}
		matches++
	}
	return matches == 1
}
