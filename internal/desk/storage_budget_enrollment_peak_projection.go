package desk

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"reflect"
	"sort"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

type storageEnrollmentPeakProjection struct {
	Kind            uint8
	Catalog         storageOwnerCatalogV2
	Select          [8][]uint16
	OriginalRef     uint16
	SavedOuter, Raw []byte
	Physical        storageCompositionProjection
	TransferPrior   *storageEnrollmentTransferPriorControl
	Policy          *storagePolicyProjection
	Native          *storagePolicyNativeProjection
}

type storagePolicyProjection struct {
	Header         [64]byte
	SourceEncoding byte
	NativeSource   [736]byte
	// Explicit source4 retains only its recipe here; actual row-set bytes live
	// in the containing physical/evidence payload, selected by SourceMoveMap.
	MoveNativeSource        []byte
	SourceMoveMap           []uint16
	MoveOperands            storageMoveOperandTable
	Kind                    uint8
	Phase                   byte
	Transition              storagebudget.ID
	Previous, Before, After storagebudget.Digest
	InverseLength           uint16
	Inverse                 [2048]byte
	Effective               storagePolicyEffectiveSet
	Lineage                 storageCompositionLineage
	// Exact Prepared peer operand captured with the original native pair.
	// The final Previous digest commits to, but cannot reconstruct, these bytes.
	PeerPrevious storagebudget.Digest
	SelectedBank byte
	PeerRetained bool
}

func storagePolicyBeforeProjection(before, after storageOwnerCatalogV2, raw []byte) (storageEnrollmentPeakProjection, error) {
	payload, sequence, err := storageOwnerV6Payload(raw)
	if err != nil || !storageOwnerV6PackedPreflight(payload) {
		return storageEnrollmentPeakProjection{}, storagebudget.ErrBounds
	}
	var outer storageOwnerV6PackedCatalog
	if json.Unmarshal(payload, &outer) != nil || outer.Sequence != sequence {
		return storageEnrollmentPeakProjection{}, storagebudget.ErrBounds
	}
	canonical, err := json.Marshal(outer)
	if err != nil || !bytes.Equal(canonical, payload) {
		return storageEnrollmentPeakProjection{}, storagebudget.ErrBounds
	}
	source, lineage, err := storageOwnerDecodePackedSource(outer.Source)
	if err != nil {
		return storageEnrollmentPeakProjection{}, err
	}
	core := make([]byte, storageOwnerCatalogBytes)
	copy(core, source.Core)
	base, err := storageEnrollmentBeforeProjection(before, after, core)
	if err != nil {
		return storageEnrollmentPeakProjection{}, err
	}
	if base.Kind != 1 {
		return storageEnrollmentPeakProjection{}, storagebudget.ErrBounds
	}
	if source.Encoding == 2 {
		copy(base.Physical[:], source.Physical)
	}
	_, definitions, err := before.definitions()
	if err != nil {
		return storageEnrollmentPeakProjection{}, err
	}
	common, err := outer.common(definitions)
	if err != nil {
		return storageEnrollmentPeakProjection{}, err
	}
	effective, err := storageOwnerV6Expand(definitions, outer.EffectivePolicy, common.Overrides)
	if err != nil {
		return storageEnrollmentPeakProjection{}, err
	}
	set := storagePolicyEffectiveSet{Policy: effective.Policy, Workspaces: append([]storagebudget.RegisteredWorkspace{}, effective.Workspaces...)}
	sort.Slice(set.Workspaces, func(i, j int) bool { return bytes.Compare(set.Workspaces[i].ID[:], set.Workspaces[j].ID[:]) < 0 })
	phases := map[string]byte{"prepared": 1, "active": 2, "aborted": 3, "aborted-unfenced": 4}
	phase, ok := phases[outer.Phase]
	if !ok || len(outer.PolicyTransition) != 0 && len(outer.PolicyTransition) != 2048 {
		return storageEnrollmentPeakProjection{}, storagebudget.ErrBounds
	}
	m := storagePolicyProjection{SourceEncoding: source.Encoding, Kind: outer.TransitionKind, Phase: phase, Transition: outer.Transition, Previous: outer.PreviousCatalogDigest, Before: outer.BeforeEffectiveDigest, After: outer.AfterEffectiveDigest, InverseLength: uint16(len(outer.PolicyTransition)), Effective: set, Lineage: lineage}
	if source.Encoding == 3 {
		copy(m.NativeSource[:], source.Physical)
	} else if source.Encoding == 4 {
		m.MoveNativeSource = bytes.Clone(source.Physical)
		m.MoveOperands, err = storageMoveDecodeOperandTable(source.MoveOperands)
		if err != nil {
			return storageEnrollmentPeakProjection{}, err
		}
	}
	copy(m.Header[:], raw[:64])
	copy(m.Inverse[:], outer.PolicyTransition)
	base.Policy = &m
	restored, err := storagePolicyProjectionOuter(base)
	if err != nil || !bytes.Equal(restored, raw) {
		return storageEnrollmentPeakProjection{}, storagebudget.ErrBounds
	}
	return base, nil
}

// The physical field has already passed its full original-record/claim pair
// reader. These fixed offsets only select the exact retained transition; they
// neither replace that validation nor establish current native authority.
func storagePolicySourceTransition(c storageOwnerCatalogV2, physical storageCompositionProjection) (storagebudget.ID, bool) {
	if physical == (storageCompositionProjection{}) {
		return c.Transition, false
	}
	bank := 0
	if storageEnrollmentPeakU64(physical[336:344]) >= storageEnrollmentPeakU64(physical[16:24]) {
		bank = 1
	}
	start := bank * 320
	var id storagebudget.ID
	copy(id[:], physical[start+80:start+96])
	return id, physical[start+68] != storageCompositionEnrollment
}

func storagePolicyProjectionOuter(p storageEnrollmentPeakProjection) ([]byte, error) {
	if p.Policy == nil {
		return nil, storagebudget.ErrBounds
	}
	return storagePolicyProjectionFrame(p, *p.Policy, true)
}
func storagePolicyProjectionFrame(p storageEnrollmentPeakProjection, meta storagePolicyProjection, exactHeader bool) ([]byte, error) {
	if p.Policy == nil || p.Kind != 1 {
		return nil, storagebudget.ErrBounds
	}
	base := p
	base.Policy = nil
	core, err := storageEnrollmentPeakProjectionCore(base)
	if err != nil {
		return nil, err
	}
	compact, err := storageCompositionCompactCore(core)
	if err != nil {
		return nil, err
	}
	lineageRaw, err := meta.Lineage.encode()
	if err != nil {
		return nil, err
	}
	n := int(storageEnrollmentPeakU32(lineageRaw[:4]))
	source := storageOwnerV6PackedSource{Version: 1, Encoding: meta.SourceEncoding, Core: compact, Physical: []byte{}, LineageRows: append([]byte{}, lineageRaw[4:4+n]...), LineageSuffix: append([]byte{}, lineageRaw[4+n:]...)}
	if meta.SourceEncoding == 2 {
		if p.Physical == (storageCompositionProjection{}) {
			return nil, storagebudget.ErrBounds
		}
		source.Physical = append([]byte{}, p.Physical[:]...)
	} else if meta.SourceEncoding == 3 {
		if meta.Kind != storageOwnerV6PolicyTransition {
			return nil, storagebudget.ErrBounds
		}
		recipe, e := storageOwnerV6ReadBefore(meta.NativeSource[:])
		if e != nil || recipe.Kind != 2 {
			return nil, storagebudget.ErrBounds
		}
		if p.Physical != (storageCompositionProjection{}) {
			return nil, storagebudget.ErrBounds
		}
		source.Physical = append([]byte{}, meta.NativeSource[:]...)
	} else if meta.SourceEncoding == 4 {
		if meta.Kind != storageOwnerV6PolicyTransition || p.Physical != (storageCompositionProjection{}) || meta.NativeSource != ([736]byte{}) {
			return nil, storagebudget.ErrBounds
		}
		recipe, e := storageOwnerV6ReadBefore(meta.MoveNativeSource)
		if e != nil || recipe.Kind != 3 || storageMoveBeforeReferences(recipe, meta.MoveOperands) != nil {
			return nil, storagebudget.ErrBounds
		}
		source.Version = 2
		source.Physical = bytes.Clone(meta.MoveNativeSource)
		source.MoveOperands, err = storageMoveEncodeOperandTable(meta.MoveOperands)
		if err != nil {
			return nil, err
		}
	} else if meta.SourceEncoding != 1 || p.Physical != (storageCompositionProjection{}) {
		return nil, storagebudget.ErrBounds
	}
	sourceRaw, err := json.Marshal(source)
	if err != nil {
		return nil, err
	}
	if _, _, err = storageOwnerDecodePackedSource(sourceRaw); err != nil {
		return nil, err
	}
	_, definitions, err := p.Catalog.definitions()
	if err != nil {
		return nil, err
	}
	if _, err = storagePolicyEffectiveKey(meta.Effective, p.Catalog); err != nil {
		return nil, err
	}
	if len(meta.Effective.Workspaces) != len(definitions.Workspaces) {
		return nil, storagebudget.ErrBounds
	}
	rows := make([]storageOwnerV6Override, 0)
	for _, actual := range meta.Effective.Workspaces {
		found := false
		for _, original := range definitions.Workspaces {
			if original.ID != actual.ID {
				continue
			}
			if found || original.Generation != actual.Generation {
				return nil, storagebudget.ErrBounds
			}
			found = true
			if original.Limits != actual.Limits {
				rows = append(rows, storageOwnerV6Override{ID: actual.ID, Generation: actual.Generation, Limits: actual.Limits})
			}
		}
		if !found {
			return nil, storagebudget.ErrBounds
		}
	}
	sparse, err := storageOwnerV6EncodeSparse(rows, definitions)
	if err != nil {
		return nil, err
	}
	phases := map[byte]string{1: "prepared", 2: "active", 3: "aborted", 4: "aborted-unfenced"}
	phase, ok := phases[meta.Phase]
	if !ok || meta.InverseLength != 0 && meta.InverseLength != 2048 || meta.InverseLength == 0 && !storageEnrollmentPeakZero(meta.Inverse[:]) {
		return nil, storagebudget.ErrBounds
	}
	outer := storageOwnerV6PackedCatalog{Version: 6, Encoding: "packed-binary-1", Sequence: storageEnrollmentPeakU64(meta.Header[16:24]), TransitionKind: meta.Kind, Phase: phase, Transition: meta.Transition, PreviousCatalogDigest: meta.Previous, BeforeEffectiveDigest: meta.Before, AfterEffectiveDigest: meta.After, Source: sourceRaw, EffectivePolicy: meta.Effective.Policy, Overrides: sparse, PolicyTransition: append([]byte{}, meta.Inverse[:meta.InverseLength]...)}
	common, err := outer.common(definitions)
	if err != nil {
		return nil, err
	}
	if common.TransitionKind == storageOwnerV6PolicyTransition {
		if err = common.validatePolicyDefinitions(definitions, storageOwnerRecordV2{Installation: p.Catalog.Installation, QBinding: p.Catalog.QBinding}); err != nil {
			return nil, err
		}
	} else if common.TransitionKind == storageOwnerV6SourceTransition {
		id, lineageOnly := storagePolicySourceTransition(p.Catalog, p.Physical)
		if len(common.PolicyTransition) != 0 || phase == "aborted" || phase == "aborted-unfenced" || common.Sequence == 0 || phase == "prepared" && common.Sequence == ^uint64(0) || common.Transition != id {
			return nil, storagebudget.ErrBounds
		}
		before, after, e := p.Catalog.definitions()
		if e != nil {
			return nil, e
		}
		if lineageOnly {
			before = after
		}
		if e = common.validateSourceDefinitions(before, after); e != nil {
			return nil, e
		}
	} else {
		return nil, storagebudget.ErrBounds
	}
	payload, err := json.Marshal(outer)
	if err != nil || !storageOwnerV6PackedPreflight(payload) {
		return nil, storagebudget.ErrBounds
	}
	frame, err := storageOwnerV6Frame(payload, outer.Sequence)
	if err != nil || exactHeader && !bytes.Equal(frame[:64], meta.Header[:]) {
		return nil, storagebudget.ErrBounds
	}
	return frame, nil
}

// Both actual complete native bank images are supplied under the caller's
// existing original custody. Strictly rebuilding this pair grants no custody,
// write or Q permission and cannot be replaced with a guessed peer digest.
func storagePolicyRetainProjectionPeer(p *storageEnrollmentPeakProjection, actual [2][]byte, selected byte, evidenceRaw []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) error {
	if p == nil || p.Policy == nil || selected > 1 {
		return storagebudget.ErrBounds
	}
	for _, raw := range actual {
		if _, err := storageOwnerV6PackedDecode(raw, evidenceRaw, record, claim); err != nil {
			return err
		}
	}
	low, err := storageOwnerV6PackedDecode(actual[1-selected], evidenceRaw, record, claim)
	if err != nil || low.Phase != "prepared" {
		return storagebudget.ErrBounds
	}
	meta := *p.Policy
	meta.PeerPrevious = low.PreviousCatalogDigest
	meta.SelectedBank = selected
	meta.PeerRetained = true
	candidate := *p
	candidate.Policy = &meta
	restored, err := storagePolicyProjectionPair(candidate)
	if err != nil || !bytes.Equal(restored[0], actual[0]) || !bytes.Equal(restored[1], actual[1]) {
		return storagebudget.ErrBounds
	}
	p.Policy = &meta
	return nil
}
func storagePolicyProjectionPair(p storageEnrollmentPeakProjection) ([2][]byte, error) {
	var out [2][]byte
	if p.Policy == nil || !p.Policy.PeerRetained || p.Policy.SelectedBank > 1 || p.Policy.PeerPrevious == (storagebudget.Digest{}) || p.Policy.Phase != 2 && p.Policy.Phase != 3 && p.Policy.Phase != 4 {
		return out, storagebudget.ErrBounds
	}
	meta := *p.Policy
	if meta.SourceEncoding == 3 || meta.SourceEncoding == 4 {
		rawRecipe := meta.NativeSource[:]
		kind := byte(2)
		if meta.SourceEncoding == 4 {
			rawRecipe, kind = meta.MoveNativeSource, 3
		}
		source, err := storageOwnerV6ReadBefore(rawRecipe)
		if err != nil || source.Kind != kind || source.Selected != meta.SelectedBank {
			return out, storagebudget.ErrBounds
		}
	}
	selected, err := storagePolicyProjectionOuter(p)
	if err != nil || storageEnrollmentPeakU64(meta.Header[16:24]) < 2 {
		return out, storagebudget.ErrBounds
	}
	prepared := meta
	storageEnrollmentPeakPut64(prepared.Header[16:24], storageEnrollmentPeakU64(meta.Header[16:24])-1)
	prepared.Phase = 1
	prepared.Previous = meta.PeerPrevious
	prepared.Effective.Workspaces = append([]storagebudget.RegisteredWorkspace{}, meta.Effective.Workspaces...)
	if meta.Phase == 3 || meta.Phase == 4 {
		if meta.Kind != storageOwnerV6PolicyTransition || meta.InverseLength != 2048 {
			return out, storagebudget.ErrBounds
		}
		inverse, err := storagebudget.DecodePolicyTransition(meta.Inverse[:])
		if err != nil {
			return out, err
		}
		prepared.Effective.Policy = storageOwnerV6HostPolicy(inverse.After)
		if inverse.Workspace != (storagebudget.ID{}) {
			found := false
			for i, row := range prepared.Effective.Workspaces {
				if row.ID == inverse.Workspace {
					if found || row.Generation != inverse.WorkspaceGeneration || row.Limits != inverse.BeforeLimits {
						return out, storagebudget.ErrBounds
					}
					prepared.Effective.Workspaces[i].Limits = inverse.AfterLimits
					found = true
				}
			}
			if !found {
				return out, storagebudget.ErrBounds
			}
		}
	}
	peer, err := storagePolicyProjectionFrame(p, prepared, false)
	if err != nil || !bytes.Equal(peer[32:64], meta.Previous[:]) {
		return out, storagebudget.ErrBounds
	}
	out[meta.SelectedBank] = selected
	out[1-meta.SelectedBank] = peer
	return out, nil
}

func storagePolicyWriteProjection(p storageEnrollmentPeakProjection, dict storageOwnerCatalogV2, originalV3 []byte, retained *storageCompositionLineage, sets []storagePolicyEffectiveSet, record storageOwnerRecordV2, claim storageOwnerClaimV2) ([]byte, error) {
	if p.Native != nil {
		return storagePolicyWriteNativeProjection(p, dict, originalV3, retained, sets, record, claim)
	}
	if p.Policy == nil {
		return storageEnrollmentPeakWriteProjectionV2(p, dict, originalV3, retained, record, claim)
	}
	if _, err := storagePolicyProjectionPair(p); err != nil {
		return nil, err
	}
	index, err := storagePolicySetIndex(p.Policy.Effective, sets, dict)
	if err != nil {
		return nil, err
	}
	base := p
	base.Policy = nil
	inner, err := storageEnrollmentPeakWriteProjectionV2(base, dict, originalV3, retained, record, claim)
	if err != nil {
		return nil, err
	}
	if len(inner) > 132092 || len(inner)+2288 > 135168 {
		return nil, storagebudget.ErrBounds
	}
	m := p.Policy
	extra := 0
	magic := "DSKV6P1\x00"
	if p.Policy.SourceEncoding == 3 {
		extra = 736
		magic = "DSKV6P2\x00"
	}
	var moveTable []byte
	if m.SourceEncoding == 4 {
		moveTable, err = storageMoveEncodeOperandTable(m.MoveOperands)
		if err != nil {
			return nil, err
		}
		extra = 8 + len(m.MoveNativeSource) + len(moveTable)
		magic = "DSKV6P3\x00"
	}
	if len(inner)+2288+extra > 135168 {
		return nil, storagebudget.ErrBounds
	}
	w := storageEnrollmentPeakWriter{max: 135168}
	w.put([]byte(magic))
	w.u16(1)
	w.u16(0)
	w.u32(uint32(len(inner)))
	w.u16(index)
	w.u8(m.SourceEncoding)
	w.u8(m.Kind)
	w.u8(m.Phase)
	w.u8(m.SelectedBank)
	if m.PeerRetained {
		w.u8(1)
	} else {
		w.u8(0)
	}
	w.u8(0)
	w.u16(m.InverseLength)
	w.u16(0)
	w.u32(uint32(2288 + extra + len(inner)))
	w.put(m.Header[:])
	w.put(m.Transition[:])
	w.put(m.Previous[:])
	w.put(m.Before[:])
	w.put(m.After[:])
	w.put(m.Inverse[:])
	w.put(m.PeerPrevious[:])
	if m.SourceEncoding == 4 {
		w.u32(uint32(len(m.MoveNativeSource)))
		w.u32(uint32(len(moveTable)))
		w.put(m.MoveNativeSource)
		w.put(moveTable)
	} else if extra != 0 {
		w.put(m.NativeSource[:])
	}
	w.put(inner)
	if w.bad {
		return nil, storagebudget.ErrBounds
	}
	return w.raw, nil
}

func storagePolicyReadProjection(raw []byte, dict storageOwnerCatalogV2, originalV3 []byte, retained *storageCompositionLineage, sets []storagePolicyEffectiveSet, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageEnrollmentPeakProjection, error) {
	fail := func() (storageEnrollmentPeakProjection, error) {
		return storageEnrollmentPeakProjection{}, storagebudget.ErrBounds
	}
	if len(raw) < 8 {
		return fail()
	}
	if bytes.Equal(raw[:8], []byte("DSKV6H1\x00")) || bytes.Equal(raw[:8], []byte("DSKV6H2\x00")) {
		return storagePolicyReadNativeProjection(raw, dict, originalV3, retained, sets, record, claim)
	}
	extra := uint64(0)
	moveSource := bytes.Equal(raw[:8], []byte("DSKV6P3\x00"))
	if bytes.Equal(raw[:8], []byte("DSKV6P2\x00")) {
		extra = 736
	}
	if extra == 0 && !moveSource && !bytes.Equal(raw[:8], []byte("DSKV6P1\x00")) {
		return storageEnrollmentPeakReadProjectionV2(raw, dict, originalV3, retained, record, claim)
	}
	if len(raw) < 2288 || len(raw) > 135168 {
		return fail()
	}
	r := storageEnrollmentPeakReader{raw: raw}
	r.take(8)
	if r.u16() != 1 || r.u16() != 0 {
		return fail()
	}
	n := uint64(r.u32())
	index := int(r.u16())
	m := storagePolicyProjection{SourceEncoding: r.u8(), Kind: r.u8(), Phase: r.u8()}
	m.SelectedBank = r.u8()
	peerFlag := r.u8()
	reserved := r.u8()
	if m.SelectedBank > 1 || peerFlag != 1 || reserved != 0 {
		return fail()
	}
	m.PeerRetained = true
	m.InverseLength = r.u16()
	if r.u16() != 0 || uint64(r.u32()) != uint64(len(raw)) || !moveSource && n+2288+extra != uint64(len(raw)) || index >= len(sets) {
		return fail()
	}
	copy(m.Header[:], r.take(64))
	m.Transition = r.id()
	m.Previous = r.digest()
	m.Before = r.digest()
	m.After = r.digest()
	copy(m.Inverse[:], r.take(2048))
	m.PeerPrevious = r.digest()
	if moveSource {
		if m.SourceEncoding != 4 {
			return fail()
		}
		rn, tn := uint64(r.u32()), uint64(r.u32())
		if rn == 0 || rn > storageOwnerV6BeforeMax || tn < 4 || 2288+8+rn+tn+n != uint64(len(raw)) {
			return fail()
		}
		m.MoveNativeSource = bytes.Clone(r.take(int(rn)))
		var e error
		m.MoveOperands, e = storageMoveDecodeOperandTable(r.take(int(tn)))
		if e != nil {
			return fail()
		}
	} else if extra != 0 {
		if m.SourceEncoding != 3 {
			return fail()
		}
		copy(m.NativeSource[:], r.take(736))
	} else if m.SourceEncoding == 3 || m.SourceEncoding == 4 {
		return fail()
	}
	base, err := storageEnrollmentPeakReadProjectionV2(r.take(int(n)), dict, originalV3, retained, record, claim)
	if err != nil || !r.done() {
		return fail()
	}
	m.Effective = sets[index]
	m.Effective.Workspaces = append([]storagebudget.RegisteredWorkspace{}, sets[index].Workspaces...)
	if m.SourceEncoding == 1 {
		m.Lineage = storageCompositionEmptyLineage()
	} else if (m.SourceEncoding == 3 || m.SourceEncoding == 4) && retained != nil {
		// BeforePair itself retains the two exact prefix cutoffs. This full
		// source lineage is the selected native source lineage, never inferred
		// from another origin or from the current effective-set index.
		rawRecipe, kind := m.NativeSource[:], byte(2)
		if m.SourceEncoding == 4 {
			rawRecipe, kind = m.MoveNativeSource, 3
		}
		recipe, e := storageOwnerV6ReadBefore(rawRecipe)
		if e != nil || recipe.Kind != kind {
			return fail()
		}
		m.Lineage, err = retained.prefix(recipe.Cut[recipe.Selected])
		if err != nil {
			return fail()
		}
	} else if m.SourceEncoding == 2 && retained != nil {
		var count [4]uint16
		for i := range count {
			count[i] = storageEnrollmentPeakU16(base.Physical[640+2*i : 642+2*i])
		}
		m.Lineage, err = retained.prefix(count)
		if err != nil {
			return fail()
		}
	} else {
		return fail()
	}
	base.Policy = &m
	base.Raw = append([]byte{}, raw...)
	if _, err = storagePolicyProjectionPair(base); err != nil {
		return fail()
	}
	canonical, err := storagePolicyWriteProjection(base, dict, originalV3, retained, sets, record, claim)
	if err != nil || !bytes.Equal(canonical, raw) {
		return fail()
	}
	return base, nil
}

// Unpublished v3 reconstructs exact historical transfer catalogs from the
// current append-only dictionaries. Definition IDs use kind-checked indices;
// memberships use canonical current (root,workspace) edges. These operands are
// comparison data, never a replacement for actual original endpoint custody.
// Max core: legacy131380 -5760*14 +16+8192*8+8192+512=124996.
// The unchanged V2 physical wrapper adds712:125708 <135168+712.
type storageEnrollmentTransferPriorControl struct {
	Controls   []storageEnrollmentTransferPriorLocation
	References [8192]byte // <=65536 canonical current root-reference edges
	Inactive   [512]byte  // projected original root ordinals; exact old scalar4
}
type storageEnrollmentTransferPriorLocation struct{ Control, Selected, Previous uint16 }

func storageEnrollmentPeakDictionaries(c storageOwnerCatalogV2) [8]reflect.Value {
	return [8]reflect.Value{reflect.ValueOf(c.Objects), reflect.ValueOf(c.OwnerControls), reflect.ValueOf(c.Roots), reflect.ValueOf(c.Workspaces), reflect.ValueOf(c.Filesystems), reflect.ValueOf(c.Constraints), reflect.ValueOf(c.Profiles), reflect.ValueOf(c.OriginalOwner.Entries)}
}
func storageEnrollmentPeakProjectionExpand(p storageEnrollmentPeakProjection, d storageOwnerCatalogV2) (storageOwnerCatalogV2, error) {
	c := p.Catalog
	dict := storageEnrollmentPeakDictionaries(d)
	out := storageEnrollmentPeakDictionaries(c)
	_ = out
	values := [8]reflect.Value{}
	inverse := [2]map[uint32]uint32{{}, {}}
	for k := range dict {
		v := reflect.MakeSlice(dict[k].Type(), len(p.Select[k]), len(p.Select[k]))
		seen := map[uint16]bool{}
		for i, index := range p.Select[k] {
			if int(index) >= dict[k].Len() || seen[index] {
				return storageOwnerCatalogV2{}, storagebudget.ErrBounds
			}
			seen[index] = true
			v.Index(i).Set(dict[k].Index(int(index)))
			if k < 2 {
				inverse[k][uint32(index)] = uint32(i)
			}
		}
		values[k] = v
	}
	c.Objects = values[0].Interface().([]storageOwnerObject)
	c.OwnerControls = values[1].Interface().([]storageOwnerControl)
	c.Roots = values[2].Interface().([]storageOwnerRoot)
	c.Workspaces = values[3].Interface().([]storageOwnerWorkspace)
	c.Filesystems = values[4].Interface().([]storageOwnerFilesystem)
	c.Constraints = values[5].Interface().([]storageOwnerConstraint)
	c.Profiles = values[6].Interface().([]storageOwnerProfile)
	registry := c.OriginalOwner.RegistryControl
	c.OriginalOwner = d.OriginalOwner
	c.OriginalOwner.RegistryControl = registry
	c.OriginalOwner.Entries = values[7].Interface().([]storageOwnerHomeRelation)
	mapIndex := func(index uint32, kind int) (uint32, bool) {
		if index == storageOwnerNoObject {
			return index, true
		}
		v, ok := inverse[kind][index]
		return v, ok
	}
	for i := range c.Objects {
		v, ok := mapIndex(c.Objects[i].Parent, 0)
		if !ok {
			return storageOwnerCatalogV2{}, storagebudget.ErrBounds
		}
		c.Objects[i].Parent = v
	}
	for i := range c.OwnerControls {
		v, ok := mapIndex(c.OwnerControls[i].ParentObject, 0)
		if !ok {
			return storageOwnerCatalogV2{}, storagebudget.ErrBounds
		}
		c.OwnerControls[i].ParentObject = v
	}
	for i := range c.Roots {
		r := &c.Roots[i]
		r.Membership.ExistingObjects = append([]uint32{}, r.Membership.ExistingObjects...)
		for _, index := range []*uint32{&r.DirectoryObject, &r.DomainObject, &r.WorkObject} {
			v, ok := mapIndex(*index, 0)
			if !ok {
				return storageOwnerCatalogV2{}, storagebudget.ErrBounds
			}
			*index = v
		}
		for j := range r.Membership.ExistingObjects {
			v, ok := mapIndex(r.Membership.ExistingObjects[j], 0)
			if !ok {
				return storageOwnerCatalogV2{}, storagebudget.ErrBounds
			}
			r.Membership.ExistingObjects[j] = v
		}
		if r.Source.ControlKind == 1 || r.Source.ControlKind == 2 {
			kind := int(r.Source.ControlKind - 1)
			v, ok := mapIndex(r.Source.ControlIndex, kind)
			if !ok {
				return storageOwnerCatalogV2{}, storagebudget.ErrBounds
			}
			r.Source.ControlIndex = v
		} else if r.Source.ControlIndex != storageOwnerNoControl {
			return storageOwnerCatalogV2{}, storagebudget.ErrBounds
		}
	}
	if prior := p.TransferPrior; prior != nil {
		changed := map[uint16]bool{}
		for _, location := range prior.Controls {
			if int(location.Control) >= len(c.OwnerControls) || int(location.Selected) >= len(c.Roots) || location.Previous != 65535 && int(location.Previous) >= len(c.Roots) || changed[location.Control] {
				return storageOwnerCatalogV2{}, storagebudget.ErrBounds
			}
			changed[location.Control] = true
			control := &c.OwnerControls[location.Control]
			selected := c.Roots[location.Selected]
			if control.Role != 2 || string(control.Name) != dataLocationName || !storageTransferControlRoot(c, selected, uint32(location.Control)) {
				return storageOwnerCatalogV2{}, storagebudget.ErrBounds
			}
			control.Location = storageOwnerLocation{Version: 1, Path: append([]byte{}, selected.Source.Path...), Previous: []byte{}, Selected: c.Objects[selected.DirectoryObject].Persistent}
			if location.Previous != 65535 {
				old := c.Roots[location.Previous]
				if location.Previous == location.Selected || !storageTransferControlRoot(c, old, uint32(location.Control)) {
					return storageOwnerCatalogV2{}, storagebudget.ErrBounds
				}
				control.Location.Previous = append([]byte{}, old.Source.Path...)
				control.Location.PreviousSelected = c.Objects[old.DirectoryObject].Persistent
			}
			var err error
			control.RelationDigest, err = c.controlDigest(*control)
			if err != nil {
				return storageOwnerCatalogV2{}, err
			}
			current := d.OwnerControls[p.Select[1][location.Control]]
			if control.RelationDigest == current.RelationDigest {
				return storageOwnerCatalogV2{}, storagebudget.ErrBounds
			}
			for i := range c.Roots {
				root := &c.Roots[i]
				if root.Source.ControlKind != 2 || root.Source.ControlIndex != uint32(location.Control) {
					continue
				}
				root.Source.ControlSelection = 3
				if bytes.Equal(root.Source.Path, control.Location.Path) && c.Objects[root.DirectoryObject].Persistent == control.Location.Selected {
					root.Source.ControlSelection = 1
				}
				if len(control.Location.Previous) > 0 && bytes.Equal(root.Source.Path, control.Location.Previous) && c.Objects[root.DirectoryObject].Persistent == control.Location.PreviousSelected {
					root.Source.ControlSelection = 2
				}
			}
		}
		selectedRoots := map[uint16]int{}
		selectedWorkspaces := map[storagebudget.ID]bool{}
		for i, index := range p.Select[2] {
			selectedRoots[index] = i
		}
		for _, index := range p.Select[3] {
			selectedWorkspaces[d.Workspaces[index].Definition.ID] = true
		}
		edge := 0
		for currentIndex, row := range d.Roots {
			originalIndex, projected := selectedRoots[uint16(currentIndex)]
			if projected {
				c.Roots[originalIndex].References = []storagebudget.ID{}
			}
			for _, id := range row.References {
				if edge >= 65536 {
					return storageOwnerCatalogV2{}, storagebudget.ErrBounds
				}
				if prior.References[edge/8]&(1<<uint(edge%8)) != 0 {
					if !projected || !selectedWorkspaces[id] {
						return storageOwnerCatalogV2{}, storagebudget.ErrBounds
					}
					c.Roots[originalIndex].References = append(c.Roots[originalIndex].References, id)
				}
				edge++
			}
		}
		for ; edge < 65536; edge++ {
			if prior.References[edge/8]&(1<<uint(edge%8)) != 0 {
				return storageOwnerCatalogV2{}, storagebudget.ErrBounds
			}
		}
		for ordinal := 0; ordinal < 4096; ordinal++ {
			inactive := prior.Inactive[ordinal/8]&(1<<uint(ordinal%8)) != 0
			if ordinal >= len(c.Roots) {
				if inactive {
					return storageOwnerCatalogV2{}, storagebudget.ErrBounds
				}
				continue
			}
			root := &c.Roots[ordinal]
			if storageTransferCatalogView(*root) {
				if root.Source.ControlSelection != 0 && root.Source.ControlSelection != 4 {
					return storageOwnerCatalogV2{}, storagebudget.ErrBounds
				}
				root.Source.ControlSelection = 0
				if inactive {
					root.Source.ControlSelection = 4
				}
			} else if inactive {
				return storageOwnerCatalogV2{}, storagebudget.ErrBounds
			}
		}
	}

	if registry == storageOwnerNoControl {
		if d.OriginalOwner.RegistryControl != storageOwnerNoControl {
			return storageOwnerCatalogV2{}, storagebudget.ErrBounds
		}
	} else if int(registry) >= len(p.Select[1]) || uint32(p.Select[1][registry]) != d.OriginalOwner.RegistryControl {
		return storageOwnerCatalogV2{}, storagebudget.ErrBounds
	}
	return c, nil
}

func storageEnrollmentPeakReadProjection(raw []byte, dict storageOwnerCatalogV2, originalV3 []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageEnrollmentPeakProjection, error) {
	return storageEnrollmentPeakReadProjectionVariant(raw, dict, originalV3, record, claim, false)
}

func storageEnrollmentPeakReadProjectionVariant(raw []byte, dict storageOwnerCatalogV2, originalV3 []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2, composition bool) (storageEnrollmentPeakProjection, error) {
	var p storageEnrollmentPeakProjection
	if len(raw) < 8 || len(raw) > storageEnrollmentPeakProjectionMax || int(storageEnrollmentPeakU32(raw[4:8])) != len(raw) {
		return p, storagebudget.ErrBounds
	}
	r := storageEnrollmentPeakReader{raw: raw}
	version := r.u16()
	p.Kind = r.u8()
	flags := r.u8()
	r.u32()
	c := storageOwnerCatalogV2{}
	c.Version = r.u32()
	c.Installation = r.id()
	c.QBinding = r.digest()
	c.Sequence = r.u64()
	phase := r.data(8)
	if !storageOwnerText(string(phase), 8, false) {
		return p, storagebudget.ErrBounds
	}
	c.Phase = string(phase)
	c.Transition = r.id()
	c.PreviousCatalogDigest = r.digest()
	c.BeforeDefinitionsDigest = r.digest()
	c.AfterDefinitionsDigest = r.digest()
	c.ExpectedQRevision = r.u64()
	storageEnrollmentPeakReadFixed(&r, reflect.ValueOf(&c.Policy).Elem())
	sourceRef := r.u16()
	c.OriginalOwner.RegistryControl = r.u32()
	limits := [8]int{8192, 8192, 4096, 1024, 128, 512, 64, 1024}
	dictionaries := storageEnrollmentPeakDictionaries(dict)
	for k := range p.Select {
		n := int(r.u16())
		if n > limits[k] || n > dictionaries[k].Len() {
			return storageEnrollmentPeakProjection{}, storagebudget.ErrBounds
		}
		p.Select[k] = make([]uint16, n)
		for i := range p.Select[k] {
			p.Select[k][i] = r.u16()
		}
	}
	ids := []*[]storagebudget.ID{&c.BeforeDefinitionIDs.Roots, &c.BeforeDefinitionIDs.Workspaces, &c.BeforeDefinitionIDs.Filesystems, &c.BeforeDefinitionIDs.Constraints}
	idLimits := [4]int{4096, 1024, 128, 512}
	for k, target := range ids {
		n := int(r.u16())
		if n > idLimits[k] {
			return storageEnrollmentPeakProjection{}, storagebudget.ErrBounds
		}
		*target = make([]storagebudget.ID, n)
		seen := map[uint16]bool{}
		for i := range *target {
			if version == 3 {
				index := r.u16()
				id, ok := storageTransferProjectionDefinition(dict, k, index)
				if !ok || seen[index] || !storageTransferProjectionSelected(p.Select[[4]int{2, 3, 4, 5}[k]], index) {
					return storageEnrollmentPeakProjection{}, storagebudget.ErrBounds
				}
				seen[index] = true
				(*target)[i] = id
			} else {
				(*target)[i] = r.id()
			}
		}

	}
	p.OriginalRef = r.u16()
	savedLength := int(r.u16())
	p.SavedOuter = append([]byte{}, r.take(savedLength)...)
	p.Catalog = c
	if version == 3 {
		if !bytes.Equal(r.take(8), []byte("DSKTC3\x00\x00")) {
			return storageEnrollmentPeakProjection{}, storagebudget.ErrBounds
		}
		count := r.u16()
		if count > 8192 || r.u16() != 0 || r.u32() != 0 {
			return storageEnrollmentPeakProjection{}, storagebudget.ErrBounds
		}
		p.TransferPrior = &storageEnrollmentTransferPriorControl{}
		for i := uint16(0); i < count; i++ {
			row := storageEnrollmentTransferPriorLocation{Control: r.u16(), Selected: r.u16(), Previous: r.u16()}
			if r.u16() != 0 || i > 0 && p.TransferPrior.Controls[i-1].Control >= row.Control {
				return storageEnrollmentPeakProjection{}, storagebudget.ErrBounds
			}
			p.TransferPrior.Controls = append(p.TransferPrior.Controls, row)
		}
		copy(p.TransferPrior.References[:], r.take(8192))
		copy(p.TransferPrior.Inactive[:], r.take(512))
	}
	if !r.done() || version != 1 && version != 3 || p.Kind > 1 || flags != 0 || sourceRef != 0 || p.Kind == 0 && c.Version != 3 || p.Kind == 1 && c.Version != 1 || p.Kind == 0 && savedLength != 64 || p.Kind == 1 && (savedLength != 8320 || p.OriginalRef != 65535) || p.OriginalRef != 0 && p.OriginalRef != 65535 {
		return storageEnrollmentPeakProjection{}, storagebudget.ErrBounds
	}
	expanded, err := storageEnrollmentPeakProjectionExpand(p, dict)
	if err != nil {
		return storageEnrollmentPeakProjection{}, err
	}
	payload, err := json.Marshal(expanded)
	if err != nil || !storageEnrollmentPeakCatalogPreflight(payload) {
		return storageEnrollmentPeakProjection{}, storagebudget.ErrBounds
	}
	if p.Kind == 0 {
		full := make([]byte, storageOwnerCatalogBytes)
		copy(full, p.SavedOuter)
		copy(full[64:], payload)
		if _, err := storageOwnerDecodeCatalogV3(full, record, claim); err != nil {
			return storageEnrollmentPeakProjection{}, err
		}
		if (p.OriginalRef == 0) != bytes.Equal(full, originalV3) {
			return storageEnrollmentPeakProjection{}, storagebudget.ErrBounds
		}
	} else {
		outer, err := storageEnrollmentPeakDecodeEnvelopeVariant(p.SavedOuter, record, claim, false, composition)
		if err != nil || outer.Kind != 2 || outer.Marker == nil || outer.Marker.Phase != 3 {
			return storageEnrollmentPeakProjection{}, storagebudget.ErrBounds
		}
		innerRaw, err := storageEnrollmentPeakEncodeInner(storageEnrollmentPeakInner{Catalog: expanded, Controls: outer.Marker.Controls}, record, claim)
		if err != nil || storageEnrollmentPeakInnerDigest(innerRaw) != outer.Marker.Inner {
			return storageEnrollmentPeakProjection{}, storagebudget.ErrBounds
		}
	}
	p.Catalog = expanded
	p.Raw = append([]byte{}, raw...)
	return p, nil
}
func storageEnrollmentPeakWriteProjection(p storageEnrollmentPeakProjection, dict storageOwnerCatalogV2, originalV3 []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) ([]byte, error) {
	return storageEnrollmentPeakWriteProjectionVariant(p, dict, originalV3, record, claim, false)
}

func storageEnrollmentPeakWriteProjectionVariant(p storageEnrollmentPeakProjection, dict storageOwnerCatalogV2, originalV3 []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2, composition bool) ([]byte, error) {
	if p.Policy != nil || p.Native != nil {
		return nil, storagebudget.ErrBounds
	}
	if !composition && p.Physical != (storageCompositionProjection{}) {
		return nil, storagebudget.ErrBounds
	}
	c := p.Catalog
	w := storageEnrollmentPeakWriter{max: storageEnrollmentPeakProjectionMax}
	version := uint16(1)
	if p.TransferPrior != nil {
		version = 3
	}
	w.u16(version)
	w.u8(p.Kind)
	w.u8(0)
	w.u32(0)
	w.u32(c.Version)
	w.put(c.Installation[:])
	w.put(c.QBinding[:])
	w.u64(c.Sequence)
	w.data([]byte(c.Phase), 8)
	w.put(c.Transition[:])
	w.put(c.PreviousCatalogDigest[:])
	w.put(c.BeforeDefinitionsDigest[:])
	w.put(c.AfterDefinitionsDigest[:])
	w.u64(c.ExpectedQRevision)
	storageEnrollmentPeakWriteFixed(&w, reflect.ValueOf(c.Policy))
	w.u16(0)
	w.u32(c.OriginalOwner.RegistryControl)
	for _, list := range p.Select {
		if len(list) > 8192 {
			return nil, storagebudget.ErrBounds
		}
		w.u16(uint16(len(list)))
		for _, v := range list {
			w.u16(v)
		}
	}
	for kind, list := range [][]storagebudget.ID{c.BeforeDefinitionIDs.Roots, c.BeforeDefinitionIDs.Workspaces, c.BeforeDefinitionIDs.Filesystems, c.BeforeDefinitionIDs.Constraints} {
		if len(list) > 4096 {
			return nil, storagebudget.ErrBounds
		}
		w.u16(uint16(len(list)))
		seen := map[uint16]bool{}
		for _, v := range list {
			if version == 3 {
				index, ok := storageTransferProjectionDefinitionIndex(dict, kind, v)
				if !ok || seen[index] || !storageTransferProjectionSelected(p.Select[[4]int{2, 3, 4, 5}[kind]], index) {
					return nil, storagebudget.ErrBounds
				}
				seen[index] = true
				w.u16(index)
			} else {
				w.put(v[:])
			}
		}

	}
	w.u16(p.OriginalRef)
	w.data(p.SavedOuter, 8320)
	if prior := p.TransferPrior; prior != nil {
		if len(prior.Controls) > 8192 {
			return nil, storagebudget.ErrBounds
		}
		w.put([]byte("DSKTC3\x00\x00"))
		w.u16(uint16(len(prior.Controls)))
		w.u16(0)
		w.u32(0)
		for _, location := range prior.Controls {
			w.u16(location.Control)
			w.u16(location.Selected)
			w.u16(location.Previous)
			w.u16(0)
		}
		w.put(prior.References[:])
		w.put(prior.Inactive[:])
	}

	if w.bad {
		return nil, storagebudget.ErrBounds
	}
	storageEnrollmentPeakPut32(w.raw[4:8], uint32(len(w.raw)))
	got, err := storageEnrollmentPeakReadProjectionVariant(w.raw, dict, originalV3, record, claim, composition)
	if err != nil {
		return nil, err
	}
	a, err := json.Marshal(got.Catalog)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(c)
	if err != nil || !bytes.Equal(a, b) {
		return nil, storagebudget.ErrBounds
	}
	return w.raw, nil
}
func storageEnrollmentPeakProjectionFullDigest(p storageEnrollmentPeakProjection) storagebudget.Digest {
	if p.Native != nil {
		pair, err := storagePolicyNativeProjectionPair(p)
		if err != nil {
			return storagebudget.Digest{}
		}
		return sha256.Sum256(pair[p.Native.Before.Selected])
	}
	if p.Policy != nil {
		frame, err := storagePolicyProjectionOuter(p)
		if err != nil {
			return storagebudget.Digest{}
		}
		return sha256.Sum256(frame)
	}
	if p.Kind == 0 {
		b := make([]byte, storageOwnerCatalogBytes)
		copy(b, p.SavedOuter)
		raw, _ := json.Marshal(p.Catalog)
		copy(b[64:], raw)
		return sha256.Sum256(b)
	}
	b := make([]byte, storageOwnerCatalogBytes)
	copy(b, p.SavedOuter)
	return sha256.Sum256(b)
}

// The v1 core body and its own declared length remain unchanged. Evidence2
// appends one separate fixed physical field to every row, including all-zero
// fields for genuinely unwrapped historical projections.
func storageEnrollmentPeakReadProjectionV2(raw []byte, dict storageOwnerCatalogV2, originalV3 []byte, retained *storageCompositionLineage, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageEnrollmentPeakProjection, error) {
	if len(raw) < 8+storageCompositionProjectionBytes {
		return storageEnrollmentPeakProjection{}, storagebudget.ErrBounds
	}
	n := int(storageEnrollmentPeakU32(raw[4:8]))
	if n < 8 || n > storageEnrollmentPeakProjectionMax || len(raw) != n+storageCompositionProjectionBytes {
		return storageEnrollmentPeakProjection{}, storagebudget.ErrBounds
	}
	p, err := storageEnrollmentPeakReadProjectionVariant(raw[:n], dict, originalV3, record, claim, true)
	if err != nil {
		return storageEnrollmentPeakProjection{}, err
	}
	copy(p.Physical[:], raw[n:])
	if p.Physical != (storageCompositionProjection{}) {
		if retained == nil || !p.Physical.shape() {
			return storageEnrollmentPeakProjection{}, storagebudget.ErrBounds
		}
		core, e := storageEnrollmentPeakProjectionCore(p)
		if e != nil {
			return storageEnrollmentPeakProjection{}, e
		}
		if _, e = p.Physical.restore(core, *retained, record, claim); e != nil {
			return storageEnrollmentPeakProjection{}, e
		}
	}
	p.Raw = append([]byte{}, raw...)
	return p, nil
}

func storageEnrollmentPeakWriteProjectionV2(p storageEnrollmentPeakProjection, dict storageOwnerCatalogV2, originalV3 []byte, retained *storageCompositionLineage, record storageOwnerRecordV2, claim storageOwnerClaimV2) ([]byte, error) {
	core, err := storageEnrollmentPeakWriteProjectionVariant(p, dict, originalV3, record, claim, true)
	if err != nil {
		return nil, err
	}
	raw := append(core, p.Physical[:]...)
	if _, err = storageEnrollmentPeakReadProjectionV2(raw, dict, originalV3, retained, record, claim); err != nil {
		return nil, err
	}
	return raw, nil
}

func storageEnrollmentPeakProjectionCore(p storageEnrollmentPeakProjection) ([]byte, error) {
	if p.Native != nil {
		pair, err := storagePolicyNativeProjectionPair(p)
		if err != nil {
			return nil, err
		}
		return pair[p.Native.Before.Selected], nil
	}
	if p.Policy != nil {
		return storagePolicyProjectionOuter(p)
	}
	raw := make([]byte, storageOwnerCatalogBytes)
	if p.Kind == 0 {
		if len(p.SavedOuter) != 64 {
			return nil, storagebudget.ErrBounds
		}
		payload, err := json.Marshal(p.Catalog)
		if err != nil || len(payload) > len(raw)-64 {
			return nil, storagebudget.ErrBounds
		}
		copy(raw[64:], payload)
	} else if p.Kind != 1 || len(p.SavedOuter) != 8320 {
		return nil, storagebudget.ErrBounds
	}
	copy(raw, p.SavedOuter)
	return raw, nil
}

// Native historical rows use the existing source projection plus one closed
// two-bank recipe and an independently bound effective-set index. The immutable
// lineage is supplied by exact retained Order cutoffs, not copied into this row.
type storagePolicyNativeProjection struct {
	Before    storageOwnerV6BeforePair
	Effective storagePolicyEffectiveSet
	Lineage   storageCompositionLineage
	Record    storageOwnerRecordV2
	Claim     storageOwnerClaimV2
}

func storagePolicyBeforeNativeProjection(before, after storageOwnerCatalogV2, actual [2][]byte, selected byte, evidence []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageEnrollmentPeakProjection, error) {
	capsule, core, lineage, policy, sparse, err := storageOwnerV6CaptureBefore(actual, selected, evidence, before, record, claim)
	if err != nil || capsule.Kind != 2 && capsule.Kind != 3 {
		return storageEnrollmentPeakProjection{}, storagebudget.ErrBounds
	}
	base, err := storageEnrollmentBeforeProjection(before, after, core)
	if err != nil || base.Kind != 1 {
		return storageEnrollmentPeakProjection{}, storagebudget.ErrBounds
	}
	_, defs, err := before.definitions()
	if err != nil {
		return storageEnrollmentPeakProjection{}, err
	}
	rows, err := storageOwnerV6DecodeSparse(sparse, defs)
	if err != nil {
		return storageEnrollmentPeakProjection{}, err
	}
	effective, err := storageOwnerV6Expand(defs, policy, rows)
	if err != nil {
		return storageEnrollmentPeakProjection{}, err
	}
	set := storagePolicyEffectiveSet{Policy: policy, Workspaces: append([]storagebudget.RegisteredWorkspace{}, effective.Workspaces...)}
	sort.Slice(set.Workspaces, func(i, j int) bool { return bytes.Compare(set.Workspaces[i].ID[:], set.Workspaces[j].ID[:]) < 0 })
	base.Native = &storagePolicyNativeProjection{Before: capsule, Effective: set, Lineage: lineage, Record: record, Claim: claim}
	pair, err := storagePolicyNativeProjectionPair(base)
	if err != nil || !bytes.Equal(pair[0], actual[0]) || !bytes.Equal(pair[1], actual[1]) {
		return storageEnrollmentPeakProjection{}, storagebudget.ErrBounds
	}
	return base, nil
}
func storagePolicyNativeProjectionPair(p storageEnrollmentPeakProjection) ([2][]byte, error) {
	var out [2][]byte
	if p.Native == nil || p.Policy != nil || p.Native.Before.Kind != 2 && p.Native.Before.Kind != 3 || p.Physical != (storageCompositionProjection{}) || p.Kind != 1 {
		return out, storagebudget.ErrBounds
	}
	m := p.Native
	base := p
	base.Native = nil
	core, err := storageEnrollmentPeakProjectionCore(base)
	if err != nil {
		return out, err
	}
	_, defs, err := p.Catalog.definitions()
	if err != nil {
		return out, err
	}
	if len(defs.Workspaces) != len(m.Effective.Workspaces) {
		return out, storagebudget.ErrBounds
	}
	rows := []storageOwnerV6Override{}
	for i, w := range m.Effective.Workspaces {
		if i > 0 && bytes.Compare(m.Effective.Workspaces[i-1].ID[:], w.ID[:]) >= 0 {
			return out, storagebudget.ErrBounds
		}
		found := false
		for _, original := range defs.Workspaces {
			if original.ID == w.ID {
				if found || original.Generation != w.Generation {
					return out, storagebudget.ErrBounds
				}
				found = true
				if original.Limits != w.Limits {
					rows = append(rows, storageOwnerV6Override{ID: w.ID, Generation: w.Generation, Limits: w.Limits})
				}
			}
		}
		if !found {
			return out, storagebudget.ErrBounds
		}
	}
	sparse, err := storageOwnerV6EncodeSparse(rows, defs)
	if err != nil {
		return out, err
	}
	return m.Before.restore(core, m.Lineage, m.Effective.Policy, sparse, p.Catalog, m.Record, m.Claim)
}
func storagePolicyWriteNativeProjection(p storageEnrollmentPeakProjection, dict storageOwnerCatalogV2, originalV3 []byte, retained *storageCompositionLineage, sets []storagePolicyEffectiveSet, record storageOwnerRecordV2, claim storageOwnerClaimV2) ([]byte, error) {
	if p.Native == nil || retained == nil || p.Native.Record != record || p.Native.Claim != claim {
		return nil, storagebudget.ErrBounds
	}
	if _, err := storagePolicyNativeProjectionPair(p); err != nil {
		return nil, err
	}
	index, err := storagePolicySetIndex(p.Native.Effective, sets, dict)
	if err != nil {
		return nil, err
	}
	recipe, err := p.Native.Before.encode()
	if err != nil {
		return nil, err
	}
	base := p
	base.Native = nil
	inner, err := storageEnrollmentPeakWriteProjectionV2(base, dict, originalV3, retained, record, claim)
	if err != nil {
		return nil, err
	}
	var moveTable []byte
	magic := "DSKV6H1\x00"
	if p.Native.Before.Kind == 3 {
		moveTable, err = storageMoveEncodeOperandTable(p.Native.Before.MoveOperands)
		if err != nil {
			return nil, err
		}
		magic = "DSKV6H2\x00"
	}
	w := storageEnrollmentPeakWriter{max: 135168}
	w.put([]byte(magic))
	w.u16(1)
	w.u16(0)
	w.u32(uint32(32 + len(recipe) + len(moveTable) + len(inner)))
	w.u16(index)
	w.u16(uint16(len(recipe)))
	w.u32(uint32(len(inner)))
	w.u32(uint32(len(moveTable)))
	w.u32(0)
	w.put(recipe)
	w.put(moveTable)
	w.put(inner)
	if w.bad {
		return nil, storagebudget.ErrBounds
	}
	return w.raw, nil
}
func storagePolicyReadNativeProjection(raw []byte, dict storageOwnerCatalogV2, originalV3 []byte, retained *storageCompositionLineage, sets []storagePolicyEffectiveSet, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageEnrollmentPeakProjection, error) {
	fail := func() (storageEnrollmentPeakProjection, error) {
		return storageEnrollmentPeakProjection{}, storagebudget.ErrBounds
	}
	if len(raw) < 32 || len(raw) > 135168 || retained == nil || string(raw[:8]) != "DSKV6H1\x00" && string(raw[:8]) != "DSKV6H2\x00" {
		return fail()
	}
	r := storageEnrollmentPeakReader{raw: raw[8:]}
	if r.u16() != 1 || r.u16() != 0 || int(r.u32()) != len(raw) {
		return fail()
	}
	index, rn, cn := int(r.u16()), int(r.u16()), uint64(r.u32())
	mn := uint64(r.u32())
	moveSource := string(raw[:8]) == "DSKV6H2\x00"
	if r.u32() != 0 || !moveSource && mn != 0 || moveSource && mn < 4 || index >= len(sets) || rn > storageOwnerV6BeforeMax || uint64(len(raw)) != 32+uint64(rn)+mn+cn {
		return fail()
	}
	recipe, err := storageOwnerV6ReadBefore(r.take(rn))
	if err != nil || !moveSource && recipe.Kind != 2 || moveSource && recipe.Kind != 3 {
		return fail()
	}
	if moveSource {
		recipe.MoveOperands, err = storageMoveDecodeOperandTable(r.take(int(mn)))
		if err != nil || storageMoveBeforeReferences(recipe, recipe.MoveOperands) != nil {
			return fail()
		}
	}
	base, err := storageEnrollmentPeakReadProjectionV2(r.take(int(cn)), dict, originalV3, retained, record, claim)
	if err != nil || !r.done() {
		return fail()
	}
	set := sets[index]
	set.Workspaces = append([]storagebudget.RegisteredWorkspace{}, set.Workspaces...)
	base.Native = &storagePolicyNativeProjection{Before: recipe, Effective: set, Lineage: *retained, Record: record, Claim: claim}
	if _, err = storagePolicyNativeProjectionPair(base); err != nil {
		return fail()
	}
	canonical, err := storagePolicyWriteNativeProjection(base, dict, originalV3, retained, sets, record, claim)
	if err != nil || !bytes.Equal(canonical, raw) {
		return fail()
	}
	base.Raw = append([]byte{}, raw...)
	return base, nil
}

// A validated actual V6 projection explains one current Q epoch. Its immutable
// E1 marker continues naming the historical source cut; editable policy never
// rewrites that marker or turns a generic maintenance epoch into a policy step.
func storagePolicyProjectionQBoundary(p storageEnrollmentPeakProjection, epoch, revision uint64) bool {
	if p.Policy != nil {
		m := p.Policy
		if m.Kind != storageOwnerV6PolicyTransition || m.InverseLength != 2048 {
			return false
		}
		t, err := storagebudget.DecodePolicyTransition(m.Inverse[:])
		if err != nil {
			return false
		}
		e, r := t.Original.Epoch, t.Original.Revision
		switch m.Phase {
		case 2:
			if e == ^uint64(0) || r > ^uint64(0)-3 {
				return false
			}
			e++
			r += 3
		case 3:
			if e == ^uint64(0) || r > ^uint64(0)-2 {
				return false
			}
			e++
			r += 2
		case 4:
		default:
			return false
		}
		return epoch == e && revision >= r
	}
	if p.Native != nil {
		actual, err := storagePolicyNativeProjectionPair(p)
		if err != nil {
			return false
		}
		v, err := storageOwnerV6NativeDecode(actual[p.Native.Before.Selected], p.Native.Record, p.Native.Claim)
		return err == nil && v.Boundary.valid() && epoch == v.Boundary.Epoch && revision >= v.Boundary.MinimumRevision
	}
	return false
}

func storagePolicyRetiredAtProjection(r storageIntentRecord, status storagebudget.HostState, p storageEnrollmentPeakProjection) bool {
	if r.validate() != nil || r.Phase != storageIntentRetired || !storageIntentToken(r.Token) || r.RetirementEpoch < r.AdmissionEpoch || r.RetirementEpoch > status.Epoch || !storagePolicyProjectionQBoundary(p, status.Epoch, status.Revision) {
		return false
	}
	s := status.Slots[r.Token.Slot]
	a := s.PersistedApplied
	return a == r.Token && s.PersistedNextSequence > r.Token.Sequence || a.ID != (storagebudget.ID{}) && a.Sequence > r.Token.Sequence && a.Generation > 0 && s.PersistedNextSequence > a.Sequence
}
