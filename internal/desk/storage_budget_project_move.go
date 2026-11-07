package desk

// Current move rows are detached storage-owned comparison operands. They select
// immutable P/C/F; they cannot open a source, construct custody or admit effects.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// One row per retained P, never a list of its moves. A pending row retains the
// previous committed publication while the existing original intent retains
// complete BEFORE/planned AFTER. The next current value replaces this row.
type storageMoveCurrent struct {
	PlanID                       storagebudget.ID
	PlanDigest, CompletionDigest storagebudget.Digest
	Current                      *storageMovePublication
	PendingIntent                storagebudget.Digest
}

const storageMoveCurrentVersion = 1
const storageMoveCurrentRowMaximum = 922 // 2version+80origin+1tag+807publication+32pending

func (v storageMoveCurrent) valid() bool {
	if v.PlanID == (storagebudget.ID{}) || v.PlanDigest == (storagebudget.Digest{}) || v.CompletionDigest == (storagebudget.Digest{}) {
		return false
	}
	if v.Current == nil {
		return v.PendingIntent != (storagebudget.Digest{})
	}
	c := v.Current
	if c.RequestDigest == (storagebudget.Digest{}) || !c.Receipt.valid(2) || len(c.Body) == 0 || len(c.Body) > startupIdentityLimit || sha256.Sum256(c.Body) != c.Receipt.PublishedBytesDigest {
		return false
	}
	identity, e := parseIdentity(c.Body)
	return e == nil && bytes.Equal(identity.line(), c.Body)
}
func storageMoveEncodeCurrent(v storageMoveCurrent) ([]byte, error) {
	if !v.valid() {
		return nil, storagebudget.ErrBounds
	}
	b := make([]byte, 0, storageMoveCurrentRowMaximum)
	b = binary.LittleEndian.AppendUint16(b, storageMoveCurrentVersion)
	b = append(b, v.PlanID[:]...)
	b = append(b, v.PlanDigest[:]...)
	b = append(b, v.CompletionDigest[:]...)
	if v.Current == nil {
		b = append(b, 0)
	} else {
		b = append(b, 1)
		b = append(b, v.Current.RequestDigest[:]...)
		b = append(b, storageMoveReceiptWire(v.Current.Receipt)...)
		b = storageMoveAppendBytes(b, v.Current.Body)
	}
	b = append(b, v.PendingIntent[:]...)
	if len(b) > storageMoveCurrentRowMaximum {
		return nil, storagebudget.ErrBounds
	}
	return b, nil
}
func storageMoveDecodeCurrent(raw []byte) (storageMoveCurrent, error) {
	var v storageMoveCurrent
	if len(raw) > storageMoveCurrentRowMaximum {
		return v, storagebudget.ErrBounds
	}
	r := storageEnrollmentPeakReader{raw: raw}
	if r.u16() != storageMoveCurrentVersion {
		return v, storagebudget.ErrBounds
	}
	copy(v.PlanID[:], r.take(16))
	copy(v.PlanDigest[:], r.take(32))
	copy(v.CompletionDigest[:], r.take(32))
	switch r.u8() {
	case 0:
	case 1:
		v.Current = &storageMovePublication{}
		copy(v.Current.RequestDigest[:], r.take(32))
		v.Current.Receipt = storageMoveReadReceipt(&r)
		v.Current.Body = storageMoveReadBytes(&r, startupIdentityLimit)
	default:
		r.bad = true
	}
	copy(v.PendingIntent[:], r.take(32))
	if !r.done() || !v.valid() {
		return storageMoveCurrent{}, storagebudget.ErrBounds
	}
	return v, nil
}
func storageMoveEncodeCurrents(rows []storageMoveCurrent) ([]byte, error) {
	if rows == nil || len(rows) > storageOwnerV5Rows {
		return nil, storagebudget.ErrBounds
	}
	b := binary.LittleEndian.AppendUint16(nil, storageMoveCurrentVersion)
	b = binary.LittleEndian.AppendUint16(b, uint16(len(rows)))
	for i, row := range rows {
		if i > 0 && bytes.Compare(rows[i-1].PlanID[:], row.PlanID[:]) >= 0 {
			return nil, storagebudget.ErrBounds
		}
		raw, e := storageMoveEncodeCurrent(row)
		if e != nil {
			return nil, e
		}
		b = storageMoveAppendBytes(b, raw)
	}
	return b, nil
}
func storageMoveDecodeCurrents(raw []byte) ([]storageMoveCurrent, error) {
	if len(raw) > 4+storageOwnerV5Rows*(2+storageMoveCurrentRowMaximum) {
		return nil, storagebudget.ErrBounds
	}
	r := storageEnrollmentPeakReader{raw: raw}
	if r.u16() != storageMoveCurrentVersion {
		return nil, storagebudget.ErrBounds
	}
	n := int(r.u16())
	if n > storageOwnerV5Rows {
		return nil, storagebudget.ErrBounds
	}
	rows := make([]storageMoveCurrent, 0, n)
	for i := 0; i < n; i++ {
		row, e := storageMoveDecodeCurrent(storageMoveReadBytes(&r, storageMoveCurrentRowMaximum))
		if e != nil || i > 0 && bytes.Compare(rows[i-1].PlanID[:], row.PlanID[:]) >= 0 {
			return nil, storagebudget.ErrBounds
		}
		rows = append(rows, row)
	}
	if !r.done() {
		return nil, storagebudget.ErrBounds
	}
	return rows, nil
}

// Select the actual immutable origin. Even a valid current row is not enough:
// original dictionary/native/source/Q validation remains with the caller.
func (v storageMoveCurrent) origin(lineage storageCompositionLineage) (storageOwnerV5LineagePlan, storageOwnerV5Receipt, error) {
	if !v.valid() {
		return storageOwnerV5LineagePlan{}, storageOwnerV5Receipt{}, storagebudget.ErrBounds
	}
	return v.originOperands(lineage)
}

// Pure original selector; used while constructing a first immutable request,
// before its commitment exists. It grants no current-row or effect authority.
func (v storageMoveCurrent) originOperands(lineage storageCompositionLineage) (storageOwnerV5LineagePlan, storageOwnerV5Receipt, error) {
	var plan *storageOwnerV5LineagePlan
	var receipt storageOwnerV5Receipt
	for i := range lineage.Rows.LineagePlans {
		p := &lineage.Rows.LineagePlans[i]
		if p.PlanID == v.PlanID {
			if plan != nil {
				return storageOwnerV5LineagePlan{}, receipt, storagebudget.ErrBounds
			}
			plan = p
		}
	}
	if plan == nil {
		return storageOwnerV5LineagePlan{}, receipt, storagebudget.ErrBounds
	}
	d, e := storageOwnerV5PlanDigest(*plan)
	if e != nil || d != v.PlanDigest {
		return storageOwnerV5LineagePlan{}, receipt, storagebudget.ErrBounds
	}
	identity, e := parseIdentity(plan.IdentityInitial)
	if e != nil {
		return storageOwnerV5LineagePlan{}, receipt, storagebudget.ErrBounds
	}
	found := false
	if identity.From == "" {
		for _, c := range lineage.Rows.LineageCompletions {
			if c.PlanID == plan.PlanID {
				if found || c.PlanDigest != d {
					return storageOwnerV5LineagePlan{}, receipt, storagebudget.ErrBounds
				}
				found = true
				receipt = c.Initial
			}
		}
	} else {
		for _, c := range lineage.Rows.LineageContinuations {
			if c.PlanID == plan.PlanID {
				if found || c.PlanDigest != d {
					return storageOwnerV5LineagePlan{}, receipt, storagebudget.ErrBounds
				}
				found = true
				receipt = c.Rewrite
			}
		}
		identity.From = ""
	}
	if !found || !receipt.valid(2) || sha256.Sum256(storageMoveReceiptWire(receipt)) != v.CompletionDigest {
		return storageOwnerV5LineagePlan{}, storageOwnerV5Receipt{}, storagebudget.ErrBounds
	}
	if v.Current != nil {
		now, e := parseIdentity(v.Current.Body)
		if e != nil || now.ID != identity.ID || now.Folder != identity.Folder || now.From != identity.From {
			return storageOwnerV5LineagePlan{}, storageOwnerV5Receipt{}, storagebudget.ErrBounds
		}
	}
	// Return detached origin operands; original rows are never edited.
	out := *plan
	out.HistoryIdentity = bytes.Clone(plan.HistoryIdentity)
	out.AccountingOwnerKey = bytes.Clone(plan.AccountingOwnerKey)
	out.IdentityInitial = bytes.Clone(plan.IdentityInitial)
	out.IdentityFromCleared = bytes.Clone(plan.IdentityFromCleared)
	return out, receipt, nil
}

// Whole-value replacement does not prove an admitted transition. Live callers
// must additionally validate the typed intent, native outcome and retained Q
// receipt under original exclusion before writing the returned comparison row.
func storageMoveReplaceCurrent(rows []storageMoveCurrent, before *storageMoveCurrent, next storageMoveCurrent) ([]storageMoveCurrent, error) {
	if !next.valid() || rows == nil {
		return nil, storagebudget.ErrBounds
	}
	if _, e := storageMoveEncodeCurrents(rows); e != nil {
		return nil, e
	}
	out := make([]storageMoveCurrent, 0, len(rows)+1)
	found := false
	for _, old := range rows {
		if old.PlanID != next.PlanID {
			out = append(out, old)
			continue
		}
		if found || before == nil || !reflect.DeepEqual(old, *before) || old.PlanDigest != next.PlanDigest || old.CompletionDigest != next.CompletionDigest {
			return nil, storagebudget.ErrStale
		}
		found = true
		out = append(out, next)
	}
	if before != nil && !found || before == nil && found {
		return nil, storagebudget.ErrStale
	}
	if !found {
		at := len(out)
		for i, row := range out {
			if bytes.Compare(next.PlanID[:], row.PlanID[:]) < 0 {
				at = i
				break
			}
		}
		out = append(out, storageMoveCurrent{})
		copy(out[at+1:], out[at:])
		out[at] = next
	}
	// Round-trip copies every caller-mutable body, including untouched rows.
	raw, e := storageMoveEncodeCurrents(out)
	if e != nil {
		return nil, e
	}
	return storageMoveDecodeCurrents(raw)
}

const storageCompositionMove byte = 7

func storageMoveOptionalCurrents(rows []storageMoveCurrent) ([]byte, error) {
	if rows == nil {
		return nil, nil
	}
	return storageMoveEncodeCurrents(rows)
}
func storageMoveSameCurrents(a, b []storageMoveCurrent) bool {
	x, e := storageMoveOptionalCurrents(a)
	y, f := storageMoveOptionalCurrents(b)
	return e == nil && f == nil && (a == nil) == (b == nil) && bytes.Equal(x, y)
}
func storageMoveValidateCurrents(rows []storageMoveCurrent, lineage storageCompositionLineage, record storageOwnerRecordV2) error {
	if rows == nil {
		return nil
	}
	if _, e := storageMoveEncodeCurrents(rows); e != nil {
		return e
	}
	for _, row := range rows {
		p, _, e := row.origin(lineage)
		if e != nil || p.Installation != record.Installation || p.QBinding != record.QBinding || p.ConfigObject != record.Config {
			return storagebudget.ErrBounds
		}
	}
	return nil
}

// Only the actual original issuer/recovery may produce these bank states.
// Pair selection proves a closed byte relation; it is not admission/effect or
// applied-outcome proof and must be joined to the exact original move intent/Q.
func storageMoveBankEdge(low, high storageCompositionBank) bool {
	if high.Transition != storageCompositionMove || high.Moves == nil || !storageCompositionFrozenCore(low, high) || !storageCompositionSameLineage(low, high) {
		return false
	}
	before, after := low.Moves, high.Moves
	if high.Publication == storageCompositionPrepared && low.Publication == storageCompositionActive {
		// Introduction may add one current row; an existing row only gets a new
		// pending immutable-request commitment, retaining its exact current receipt.
		changed := 0
		for _, row := range after {
			var previous *storageMoveCurrent
			for i := range before {
				if before[i].PlanID == row.PlanID {
					previous = &before[i]
					break
				}
			}
			if previous != nil && reflect.DeepEqual(*previous, row) {
				continue
			}
			changed++
			if row.PendingIntent == (storagebudget.Digest{}) || previous != nil && (previous.PendingIntent != (storagebudget.Digest{}) || previous.PlanDigest != row.PlanDigest || previous.CompletionDigest != row.CompletionDigest || !reflect.DeepEqual(previous.Current, row.Current) || previous.Current != nil && previous.Current.RequestDigest == row.PendingIntent) || previous == nil && row.Current != nil {
				return false
			}
		}
		for _, row := range before {
			found := false
			for _, r := range after {
				found = found || r.PlanID == row.PlanID
			}
			if !found {
				return false
			}
		}
		return changed == 1
	}
	if low.Transition != storageCompositionMove || low.Publication != storageCompositionPrepared || high.Publication != storageCompositionActive || low.TransitionID != high.TransitionID {
		return false
	}
	// A factual abort restores the previous current publication, or removes
	// an introduced pending-only row. This relation is distinct from commit:
	// commit must carry a new receipt for this immutable request/operation.
	// The original issuer must separately prove exact aborted J/Q/native facts.
	aborted := make([]storageMoveCurrent, 0, len(before))
	pending := 0
	for _, old := range before {
		if old.PendingIntent != (storagebudget.Digest{}) {
			pending++
			old.PendingIntent = storagebudget.Digest{}
			if old.Current == nil {
				continue
			}
		}
		aborted = append(aborted, old)
	}
	if pending == 1 && storageMoveSameCurrents(aborted, after) {
		return true
	}
	if len(before) != len(after) {
		return false
	}
	changed := 0
	for i, old := range before {
		next := after[i]
		if reflect.DeepEqual(old, next) {
			continue
		}
		changed++
		if old.PlanID != next.PlanID || old.PlanDigest != next.PlanDigest || old.CompletionDigest != next.CompletionDigest || old.PendingIntent == (storagebudget.Digest{}) || next.PendingIntent != (storagebudget.Digest{}) || next.Current == nil || next.Current.RequestDigest != old.PendingIntent || next.Current.Receipt.OperationID != high.TransitionID {
			return false
		}
	}
	return changed == 1
}

// The table interns byte-identical ACTUAL row sets used by current/retained
// native recipes. It is comparison data inside the existing physical payload,
// not another file, registry, authority or history of unreferenced moves.
// Recipe mappings retain original order, so canonical sorting never relabels
// an original physical prefix's local selector or reconstructs data from SHA.
type storageMoveOperandTable struct{ Sets [][]storageMoveCurrent }

func storageMoveEncodeOperandTable(t storageMoveOperandTable) ([]byte, error) {
	if t.Sets == nil || len(t.Sets) > 65535 {
		return nil, storagebudget.ErrBounds
	}
	w := storageEnrollmentPeakWriter{max: storageOwnerCatalogBytes - 64}
	w.u16(1)
	w.u16(uint16(len(t.Sets)))
	var previous []byte
	for _, set := range t.Sets {
		raw, e := storageMoveEncodeCurrents(set)
		if e != nil || previous != nil && bytes.Compare(previous, raw) >= 0 {
			return nil, storagebudget.ErrBounds
		}
		w.u32(uint32(len(raw)))
		w.put(raw)
		previous = raw
	}
	if w.bad {
		return nil, storagebudget.ErrBounds
	}
	return w.raw, nil
}
func storageMoveDecodeOperandTable(raw []byte) (storageMoveOperandTable, error) {
	var t storageMoveOperandTable
	if len(raw) < 4 || len(raw) > storageOwnerCatalogBytes-64 {
		return t, storagebudget.ErrBounds
	}
	r := storageEnrollmentPeakReader{raw: raw}
	if r.u16() != 1 {
		return t, storagebudget.ErrBounds
	}
	n := int(r.u16())
	if n > (len(raw)-4)/8 {
		return t, storagebudget.ErrBounds
	}
	t.Sets = make([][]storageMoveCurrent, 0, n)
	var previous []byte
	for i := 0; i < n; i++ {
		size := uint64(r.u32())
		if size > uint64(len(raw)-r.pos) {
			return storageMoveOperandTable{}, storagebudget.ErrBounds
		}
		b := r.take(int(size))
		if previous != nil && bytes.Compare(previous, b) >= 0 {
			return storageMoveOperandTable{}, storagebudget.ErrBounds
		}
		set, e := storageMoveDecodeCurrents(b)
		if e != nil {
			return storageMoveOperandTable{}, e
		}
		t.Sets = append(t.Sets, set)
		previous = b
	}
	if !r.done() {
		return storageMoveOperandTable{}, storagebudget.ErrBounds
	}
	return t, nil
}
func storageMoveInternOperands(actual [][]storageMoveCurrent) (storageMoveOperandTable, []uint16, error) {
	type item struct {
		raw    []byte
		source int
	}
	items := make([]item, len(actual))
	for i, set := range actual {
		raw, e := storageMoveEncodeCurrents(set)
		if e != nil {
			return storageMoveOperandTable{}, nil, e
		}
		items[i] = item{raw, i}
	}
	sort.Slice(items, func(i, j int) bool { return bytes.Compare(items[i].raw, items[j].raw) < 0 })
	t := storageMoveOperandTable{Sets: [][]storageMoveCurrent{}}
	selectors := make([]uint16, len(actual))
	var previous []byte
	for _, item := range items {
		if previous == nil || !bytes.Equal(previous, item.raw) {
			if len(t.Sets) == 65535 {
				return storageMoveOperandTable{}, nil, storagebudget.ErrBounds
			}
			set, e := storageMoveDecodeCurrents(item.raw)
			if e != nil {
				return storageMoveOperandTable{}, nil, e
			}
			t.Sets = append(t.Sets, set)
			previous = item.raw
		}
		selectors[item.source] = uint16(len(t.Sets) - 1)
	}
	if _, e := storageMoveEncodeOperandTable(t); e != nil {
		return storageMoveOperandTable{}, nil, e
	}
	return t, selectors, nil
}
func (t storageMoveOperandTable) selectSet(index uint16) ([]storageMoveCurrent, error) {
	if int(index) >= len(t.Sets) {
		return nil, storagebudget.ErrBounds
	}
	raw, e := storageMoveEncodeCurrents(t.Sets[index])
	if e != nil {
		return nil, e
	}
	return storageMoveDecodeCurrents(raw)
}
func (t storageMoveOperandTable) reconstruct(mapping []uint16) (storageMoveOperandTable, error) {
	if mapping == nil {
		return storageMoveOperandTable{}, storagebudget.ErrBounds
	}
	out := storageMoveOperandTable{Sets: make([][]storageMoveCurrent, 0, len(mapping))}
	for _, index := range mapping {
		set, e := t.selectSet(index)
		if e != nil {
			return storageMoveOperandTable{}, e
		}
		out.Sets = append(out.Sets, set)
	}
	// Original table order must itself be canonical, not silently normalized.
	raw, e := storageMoveEncodeOperandTable(out)
	if e != nil {
		return storageMoveOperandTable{}, e
	}
	return storageMoveDecodeOperandTable(raw)
}
func storageMoveBankOperands(v storageCompositionBank) (storageMoveOperandTable, uint16, []byte, error) {
	if v.Moves == nil {
		if v.MoveOperands.Sets != nil {
			return storageMoveOperandTable{}, 0, nil, storagebudget.ErrBounds
		}
		return storageMoveOperandTable{}, 0, nil, nil
	}
	t := v.MoveOperands
	if t.Sets == nil {
		var e error
		t, _, e = storageMoveInternOperands([][]storageMoveCurrent{v.Moves})
		if e != nil {
			return storageMoveOperandTable{}, 0, nil, e
		}
	}
	current, e := storageMoveEncodeCurrents(v.Moves)
	if e != nil {
		return storageMoveOperandTable{}, 0, nil, e
	}
	found := -1
	for i, set := range t.Sets {
		b, e := storageMoveEncodeCurrents(set)
		if e != nil {
			return storageMoveOperandTable{}, 0, nil, e
		}
		if bytes.Equal(b, current) {
			found = i
		}
	}
	if found < 0 {
		return storageMoveOperandTable{}, 0, nil, storagebudget.ErrBounds
	}
	raw, e := storageMoveEncodeOperandTable(t)
	return t, uint16(found), raw, e
}

func storageMoveWriteMapping(w *storageEnrollmentPeakWriter, mapping []uint16) error {
	if len(mapping) > 65535 {
		return storagebudget.ErrBounds
	}
	w.u16(uint16(len(mapping)))
	for _, index := range mapping {
		w.u16(index)
	}
	if w.bad {
		return storagebudget.ErrBounds
	}
	return nil
}
func storageMoveReadMapping(r *storageEnrollmentPeakReader) []uint16 {
	n := int(r.u16())
	if n == 0 {
		return nil
	}
	if n > (len(r.raw)-r.pos)/2 {
		r.bad = true
		return nil
	}
	out := make([]uint16, n)
	for i := range out {
		out[i] = r.u16()
	}
	return out
}

// Every table entry is actual retained data selected by this immediate recipe.
// A mapping reconstructs the complete original table in its original canonical
// order; the old prefix still selects within that table, never this union.
func storageMoveBeforeReferences(p storageOwnerV6BeforePair, t storageMoveOperandTable) error {
	return storageMoveBeforeReferencesCurrent(p, t, nil, false)
}
func storageMoveBeforeReferencesCurrent(p storageOwnerV6BeforePair, t storageMoveOperandTable, current []storageMoveCurrent, allowUnreferenced bool) error {
	if _, err := storageMoveEncodeOperandTable(t); err != nil {
		return err
	}
	used := make([]bool, len(t.Sets))
	mark := func(mapping []uint16) error {
		if len(mapping) == 0 {
			return storagebudget.ErrBounds
		}
		if _, err := t.reconstruct(mapping); err != nil {
			return err
		}
		for _, index := range mapping {
			used[index] = true
		}
		return nil
	}
	if p.Kind == 3 {
		for i, prefix := range p.Native {
			switch storageEnrollmentPeakU16(prefix[:2]) {
			case 3:
				if p.NativeMoveMap[i] != nil {
					return storagebudget.ErrBounds
				}
			case 4:
				if err := mark(p.NativeMoveMap[i]); err != nil {
					return err
				}
				original, err := t.reconstruct(p.NativeMoveMap[i])
				if err != nil {
					return err
				}
				b, err := storageMoveEncodeOperandTable(original)
				if err != nil || uint32(len(b)) != storageEnrollmentPeakU32(prefix[251:255]) || int(storageEnrollmentPeakU16(prefix[249:251])) >= len(original.Sets) {
					return storagebudget.ErrBounds
				}
			default:
				return storagebudget.ErrBounds
			}
		}
	} else if p.Kind == 1 && p.Packed.SourceEncoding == 4 {
		if err := mark(p.Packed.SourceMoveMap); err != nil {
			return err
		}
		original, err := t.reconstruct(p.Packed.SourceMoveMap)
		if err != nil {
			return err
		}
		recipe, err := storageOwnerV6ReadBefore(p.Packed.MoveNativeSource)
		if err != nil || storageMoveBeforeReferences(recipe, original) != nil {
			return storagebudget.ErrBounds
		}
	} else {
		return storagebudget.ErrBounds
	}
	if current != nil {
		raw, err := storageMoveEncodeCurrents(current)
		if err != nil {
			return err
		}
		found := false
		for i, set := range t.Sets {
			b, err := storageMoveEncodeCurrents(set)
			if err != nil {
				return err
			}
			if bytes.Equal(b, raw) {
				used[i], found = true, true
			}
		}
		if !found {
			return storagebudget.ErrBounds
		}
	}
	for _, present := range used {
		if !present && !allowUnreferenced {
			return storagebudget.ErrBounds
		}
	}
	return nil
}

// Compose actual before/current operands in one payload and remap only the
// recipe's union selectors. The retained native prefixes and their local
// selectors remain byte-exact, so restore can verify both original full frames.
func storageMoveNativeOperands(v storageOwnerV6NativeBank) (storageOwnerV6NativeBank, error) {
	current, _, _, err := storageMoveBankOperands(v.Stage)
	if err != nil {
		return v, err
	}
	if v.Before == nil {
		if v.Stage.Moves != nil && len(current.Sets) != 1 {
			return v, storagebudget.ErrBounds
		}
		v.Stage.MoveOperands = current
		return v, nil
	}
	p := *v.Before
	if p.Kind != 3 && !(p.Kind == 1 && p.Packed.SourceEncoding == 4) {
		if p.MoveOperands.Sets != nil {
			return v, storagebudget.ErrBounds
		}
		if v.Stage.Moves != nil && len(current.Sets) != 1 {
			return v, storagebudget.ErrBounds
		}
		v.Stage.MoveOperands = current
		return v, nil
	}
	if err := storageMoveBeforeReferencesCurrent(p, p.MoveOperands, nil, true); err != nil {
		return v, err
	}
	actual := append([][]storageMoveCurrent{}, p.MoveOperands.Sets...)
	// Only the selected current set is an additional operand. Any other set
	// in the input Stage must already be retained by the immediate recipe.
	if v.Stage.Moves == nil {
		return v, storagebudget.ErrBounds
	}
	actual = append(actual, v.Stage.Moves)
	for _, set := range current.Sets {
		raw, e := storageMoveEncodeCurrents(set)
		if e != nil {
			return v, e
		}
		found := false
		for _, retained := range actual {
			b, e := storageMoveEncodeCurrents(retained)
			if e != nil {
				return v, e
			}
			found = found || bytes.Equal(raw, b)
		}
		if !found {
			return v, storagebudget.ErrBounds
		}
	}
	t, mapping, err := storageMoveInternOperands(actual)
	if err != nil {
		return v, err
	}
	remap := func(old []uint16) []uint16 {
		if old == nil {
			return nil
		}
		out := make([]uint16, len(old))
		for i, index := range old {
			out[i] = mapping[index]
		}
		return out
	}
	if p.Kind == 3 {
		p.NativeMoveMap[0], p.NativeMoveMap[1] = remap(p.NativeMoveMap[0]), remap(p.NativeMoveMap[1])
	} else {
		p.Packed.SourceMoveMap = remap(p.Packed.SourceMoveMap)
	}
	p.MoveOperands, v.Stage.MoveOperands = t, t
	v.Before = &p
	return v, nil
}

// Prepare one exact current-row publication from the original complete pair.
// This pure adapter performs no write and grants no mutation/recovery permit.
// The original live operation joins its separately admitted request, J outcome,
// native descriptors and pre-funded tails before dispatching either byte image.
func storageMoveNativePublication(actual [2][]byte, selected byte, evidence []byte, catalog storageOwnerCatalogV2, operation storagebudget.ID, rows []storageMoveCurrent, record storageOwnerRecordV2, claim storageOwnerClaimV2) ([]byte, byte, error) {
	if selected > 1 || operation == (storagebudget.ID{}) {
		return nil, 0, storagebudget.ErrBounds
	}
	pair, err := storageOwnerV6NativeReadPair(actual[0], actual[1], nil, evidence, record, claim)
	if err != nil || pair.Selected != selected {
		return nil, 0, storagebudget.ErrBounds
	}
	old := pair.Banks[selected]
	if old.Stage.Lineage.validateGraph(catalog, record) != nil {
		return nil, 0, storagebudget.ErrBounds
	}
	// The pair reader has already validated each ACTUAL legacy composition
	// endpoint with storageCompositionBank.validateEvidence, retaining genuine
	// Version1/2 evidence. Native Enrollment requires Version3 and must keep
	// using its distinct validator; a comparison view is not a native carrier.
	if string(actual[selected][:8]) != "DSKONL1\x00" {
		if _, err = old.validateEvidence(evidence, record, claim); err != nil {
			return nil, 0, err
		}
	}
	if old.Stage.Sequence == ^uint64(0) {
		return nil, 0, storagebudget.ErrBounds
	}
	next := old
	next.Packed, next.Before = nil, nil
	next.Stage.Sequence++
	next.Stage.Transition, next.Stage.TransitionID = storageCompositionMove, operation
	next.Stage.Raw = nil
	next.Stage.MoveOperands = storageMoveOperandTable{}
	wireRows, err := storageMoveEncodeCurrents(rows)
	if err != nil {
		return nil, 0, err
	}
	next.Stage.Moves, err = storageMoveDecodeCurrents(wireRows)
	if err != nil {
		return nil, 0, err
	}
	next.Stage.Previous, err = storageCompositionChecksumOf(actual[selected])
	if err != nil {
		return nil, 0, err
	}
	if pair.RecoveryOnly {
		if old.Packed != nil || old.Stage.Transition != storageCompositionMove || old.Stage.Publication != storageCompositionPrepared || old.Stage.TransitionID != operation {
			return nil, 0, storagebudget.ErrBounds
		}
		next.Stage.Publication = storageCompositionActive
	} else {
		next.Stage.Publication = storageCompositionPrepared
	}
	if !storageMoveBankEdge(old.Stage, next.Stage) {
		return nil, 0, storagebudget.ErrStale
	}
	if !pair.RecoveryOnly {
		room, e := storageOwnerV6NativeClosureSize(next)
		if e != nil {
			return nil, 0, e
		}
		// A first pending-only row may acquire its bounded current receipt/body.
		// Existing rows already carry the full fixed receipt and bounded body;
		// each new move has exactly one pending row. This is a size shadow,
		// never fabricated factual outcome or fresh post-effect admission.
		growth := 0
		for _, row := range rows {
			if row.PendingIntent != (storagebudget.Digest{}) {
				if row.Current == nil {
					growth = 807
				} else {
					growth = startupIdentityLimit - len(row.Current.Body)
				}
			}
		}
		if room.Future > storageOwnerCatalogBytes-growth {
			return nil, 0, storagebudget.ErrBounds
		}
	}
	raw, err := next.encode(record, claim)
	if err != nil {
		return nil, 0, err
	}
	bank := 1 - selected
	check := actual
	check[bank] = raw
	if _, err = storageOwnerV6NativeReadPair(check[0], check[1], nil, evidence, record, claim); err != nil {
		return nil, 0, err
	}
	return raw, bank, nil
}

// The immutable part of the existing move intent is also the exact J/Q request
// commitment. Mutable staging/terminal observations are held in J, not included
// in this commitment. No receipt hashes its own future storage publication.
// Paths are comparison operands only; this DTO never opens them.
type storageMoveRequest struct {
	Installation                                storagebudget.ID
	Binding                                     storagebudget.Digest
	Operation, Plan                             storagebudget.ID
	PlanDigest, CompletionDigest, BeforeCurrent storagebudget.Digest
	ConfigRevision                              storagebudget.Digest
	Previous, Destination                       []byte
	Parent, ExpectedOld                         storageOwnerPersistentObject
	PayloadDigest                               storagebudget.Digest
	Tails                                       []storageMoveTail
	Artifact                                    *storageMoveArtifactRequest
}

const storageMoveRequestMaximum = 2048

func (r storageMoveRequest) encode() ([]byte, error) {
	if r.Installation == (storagebudget.ID{}) || r.Binding == (storagebudget.Digest{}) || r.Operation == (storagebudget.ID{}) || r.Plan == (storagebudget.ID{}) || r.PlanDigest == (storagebudget.Digest{}) || r.CompletionDigest == (storagebudget.Digest{}) || r.PayloadDigest == (storagebudget.Digest{}) || !r.Parent.valid(1) || !r.ExpectedOld.valid(2) || !storageIdentitySameVolume(r.Parent, r.ExpectedOld) || !validProjectHistoryPath(string(r.Previous)) || !validProjectHistoryPath(string(r.Destination)) || bytes.Equal(r.Previous, r.Destination) {
		return nil, storagebudget.ErrBounds
	}
	// The existing identity512 domain bounds both current/planned Path values.
	// The exact identity join below, rather than this wire bound, proves fit.
	if len(r.Previous) > startupIdentityLimit || len(r.Destination) > startupIdentityLimit {
		return nil, storagebudget.ErrBounds
	}
	w := storageEnrollmentPeakWriter{max: storageMoveRequestMaximum}
	w.put([]byte("DSKMREQ1"))
	w.put(r.Installation[:])
	w.put(r.Binding[:])
	w.put(r.Operation[:])
	w.put(r.Plan[:])
	w.put(r.PlanDigest[:])
	w.put(r.CompletionDigest[:])
	w.put(r.BeforeCurrent[:])
	w.put(r.ConfigRevision[:])
	w.put(storageEnrollmentPeakNativeBytes(r.Parent))
	w.put(storageEnrollmentPeakNativeBytes(r.ExpectedOld))
	w.put(r.PayloadDigest[:])
	w.put(storageMoveAppendBytes(nil, r.Previous))
	w.put(storageMoveAppendBytes(nil, r.Destination))
	tails, err := storageMoveEncodeTails(r.Tails)
	if err != nil {
		return nil, err
	}
	if len(r.Tails) > storageMoveFixedTailMaximum {
		return nil, storagebudget.ErrBounds
	}
	for _, t := range r.Tails {
		if t.Kind > storageMoveFixedTailMaximum {
			return nil, storagebudget.ErrBounds
		}
	}
	w.put(tails)
	artifact, e := storageMoveEncodeArtifact(r.Artifact)
	if e != nil {
		return nil, e
	}
	w.put(artifact)
	if w.bad {
		return nil, storagebudget.ErrBounds
	}
	return w.raw, nil
}
func storageMoveReadRequest(raw []byte) (storageMoveRequest, error) {
	var out storageMoveRequest
	if len(raw) > storageMoveRequestMaximum {
		return out, storagebudget.ErrBounds
	}
	r := storageEnrollmentPeakReader{raw: raw}
	if !bytes.Equal(r.take(8), []byte("DSKMREQ1")) {
		return out, storagebudget.ErrBounds
	}
	out.Installation, out.Binding, out.Operation, out.Plan = r.id(), r.digest(), r.id(), r.id()
	out.PlanDigest, out.CompletionDigest, out.BeforeCurrent, out.ConfigRevision = r.digest(), r.digest(), r.digest(), r.digest()
	out.Parent, out.ExpectedOld = r.native(), r.native()
	out.PayloadDigest = r.digest()
	out.Previous, out.Destination = storageMoveReadBytes(&r, startupIdentityLimit), storageMoveReadBytes(&r, startupIdentityLimit)
	var tailErr error
	out.Tails, tailErr = storageMoveReadTails(&r)
	out.Artifact = storageMoveReadArtifact(&r)
	if tailErr != nil || !r.done() {
		return storageMoveRequest{}, storagebudget.ErrBounds
	}
	canonical, err := out.encode()
	if err != nil || !bytes.Equal(canonical, raw) {
		return storageMoveRequest{}, storagebudget.ErrBounds
	}
	return out, nil
}
func (r storageMoveRequest) digest() (storagebudget.Digest, error) {
	raw, err := r.encode()
	if err != nil {
		return storagebudget.Digest{}, err
	}
	return sha256.Sum256(raw), nil
}

// Join the exact original P/C/F, previous current tuple and canonical planned
// identity. A current pathname never replaces the immutable history namespace.
func (r storageMoveRequest) operands(lineage storageCompositionLineage, before *storageMoveCurrent) (old, after []byte, err error) {
	if _, err = r.encode(); err != nil {
		return nil, nil, err
	}
	selector := storageMoveCurrent{PlanID: r.Plan, PlanDigest: r.PlanDigest, CompletionDigest: r.CompletionDigest}
	plan, receipt, err := selector.originOperands(lineage)
	if err != nil || plan.Installation != r.Installation || plan.QBinding != r.Binding || plan.PrivateObject != r.Parent {
		return nil, nil, storagebudget.ErrAuthority
	}
	old = bytes.Clone(plan.IdentityInitial)
	identity, err := parseIdentity(old)
	if err != nil {
		return nil, nil, err
	}
	if identity.From != "" {
		old = bytes.Clone(plan.IdentityFromCleared)
	}
	expected := receipt.PublishedObject
	if before != nil {
		if before.PlanID != r.Plan || before.PlanDigest != r.PlanDigest || before.CompletionDigest != r.CompletionDigest || before.PendingIntent != (storagebudget.Digest{}) || before.Current == nil {
			return nil, nil, storagebudget.ErrStale
		}
		if _, _, err = before.origin(lineage); err != nil {
			return nil, nil, err
		}
		wire, e := storageMoveEncodeCurrent(*before)
		if e != nil || sha256.Sum256(wire) != r.BeforeCurrent {
			return nil, nil, storagebudget.ErrStale
		}
		old, expected = bytes.Clone(before.Current.Body), before.Current.Receipt.PublishedObject
	} else if r.BeforeCurrent != (storagebudget.Digest{}) {
		return nil, nil, storagebudget.ErrStale
	}
	identity, err = parseIdentity(old)
	if err != nil || !bytes.Equal(identity.line(), old) || identity.From != "" || identity.Path != string(r.Previous) || expected != r.ExpectedOld {
		return nil, nil, storagebudget.ErrAuthority
	}
	identity.Path = string(r.Destination)
	after = identity.line()
	if len(after) > startupIdentityLimit || sha256.Sum256(after) != r.PayloadDigest {
		return nil, nil, storagebudget.ErrBounds
	}
	return old, after, nil
}
func (r storageMoveRequest) identityBound(j storageIdentityIntent, lineage storageCompositionLineage, before *storageMoveCurrent) bool {
	old, after, err := r.operands(lineage, before)
	digest, e := r.digest()
	c := j.Core
	return err == nil && e == nil && r.tailBound(j.MoveTails, j.Core.Components) && j.validate() == nil && j.Action == storageIdentityIntentMove && j.PlanDigest == r.PlanDigest && j.Parent == r.Parent && j.ExpectedOld == r.ExpectedOld && j.PayloadDigest == r.PayloadDigest && c.OperationID == r.Operation && c.CASBinding == digest && c.Target == startupIdentityName && c.Stage == identityStagingPrefix+hex.EncodeToString(r.Operation[:]) && c.Workflow == "desk-signing-lineage-v1" && c.Operation == "p:"+hex.EncodeToString(r.PlanDigest[:])+":move" && c.OldExists && c.OldLength == uint64(len(old)) && c.NewLength == uint64(len(after))
}

// Reuse the existing retired J/Q relation with the new action's own retained
// current receipt. This runs before host-slot reuse; a request hash or an old
// InitialCAS token alone cannot establish a move publication.
func storageMoveReceiptBound(j storageIdentityIntent, page []byte, lineage storageCompositionLineage, rows []storageMoveCurrent, status storagebudget.HostState) bool {
	if j.validate() != nil || j.Action != storageIdentityIntentMove || j.Core.Phase != storageIntentRetired || j.Core.Outcome != storageIntentPublished || j.NativePhase != storageIdentityTargetDurable || j.Core.RetirementEpoch > status.Epoch {
		return false
	}
	var selected *storageMoveCurrent
	var plan storageOwnerV5LineagePlan
	for i := range rows {
		row := &rows[i]
		if row.PlanDigest != j.PlanDigest {
			continue
		}
		if selected != nil || row.Current == nil || row.PendingIntent != (storagebudget.Digest{}) {
			return false
		}
		p, _, err := row.origin(lineage)
		if err != nil {
			return false
		}
		selected, plan = row, p
	}
	if selected == nil || selected.Current.RequestDigest != j.Core.CASBinding || sha256.Sum256(selected.Current.Body) != j.PayloadDigest {
		return false
	}
	r := selected.Current.Receipt
	digest, err := storageIdentityIntentDigest(page, int(j.Core.Slot), plan.Installation, plan.QBinding, j)
	if err != nil || r.OperationID != j.Core.OperationID || r.Terminal != j.Core.Token || r.PersistedApplied != j.Core.Token || r.PersistedNextSequence <= j.Core.Token.Sequence || r.RetirementEpoch != j.Core.RetirementEpoch || r.IntentDigest != digest || r.PublishedObject != j.Target || r.PublishedBytesDigest != j.PayloadDigest || r.PublicationEvidence != j.Core.Evidence || r.TicketDigest == (storagebudget.Digest{}) {
		return false
	}
	applied := status.Slots[j.Core.Token.Slot]
	if applied.PersistedApplied == j.Core.Token {
		if applied.PersistedNextSequence <= j.Core.Token.Sequence {
			return false
		}
	} else if applied.PersistedApplied.ID == (storagebudget.ID{}) || applied.PersistedApplied.Sequence <= j.Core.Token.Sequence || applied.PersistedApplied.Generation == 0 || applied.PersistedNextSequence <= applied.PersistedApplied.Sequence {
		return false
	}
	if applied.Ticket.Token == j.Core.Token {
		d, e := storageOwnerV4TicketDigest(plan.Installation, plan.QBinding, applied.Ticket)
		if e != nil || d != r.TicketDigest || !storageIdentityIntentTicketMapping(j, applied.Ticket, plan.QBinding) {
			return false
		}
	}
	return true
}

func storageMovePendingPair(p storageOwnerV6NativePair) bool {
	if !p.RecoveryOnly || p.Duplicated || p.Selected > 1 {
		return false
	}
	high := p.Banks[p.Selected]
	return high.Packed == nil && high.Stage.Transition == storageCompositionMove && high.Stage.Publication == storageCompositionPrepared && storageMoveBankEdge(p.Banks[1-p.Selected].Stage, high.Stage)
}

// A bootstrap result is detached comparison data under the original startup
// project/config/private descriptions. It cannot retain a source, admit work,
// recover J/Q, or change the operational/current conversation pathname.
type storageMoveBootstrap struct {
	Plan    storageOwnerV5LineagePlan
	Origin  storageOwnerV5Receipt
	Current *storageMoveCurrent
	Pending bool
	Request *storageMoveRequest
	Journal *storageIdentityIntent
}

// Select immutable accounting/history through the exact same native project,
// private and installation roots. A pathname alias, equal persistent owner, or
// an absent intent never substitutes for the current native/body publication.
func storageMoveBootstrapJoin(lineage storageCompositionLineage, rows []storageMoveCurrent, project, private, config, identityNative storageOwnerPersistentObject, currentPath, identityBody []byte, request *storageMoveRequest, journal *storageIdentityIntent) (storageMoveBootstrap, error) {
	var out storageMoveBootstrap
	var selected *storageOwnerV5LineagePlan
	for i := range lineage.Rows.LineagePlans {
		p := &lineage.Rows.LineagePlans[i]
		if p.ProjectObject != project {
			continue
		}
		if selected != nil || p.PrivateObject != private || p.ConfigObject != config {
			return out, errStorageCustody
		}
		selected = p
	}
	if selected == nil {
		return out, errStorageCustody
	}
	digest, err := storageOwnerV5PlanDigest(*selected)
	if err != nil {
		return out, err
	}
	// Find the already completed immutable origin, with no invented current
	// row or pending commitment. From continuations retain their own receipt.
	var origin storageOwnerV5Receipt
	identity, err := parseIdentity(selected.IdentityInitial)
	if err != nil {
		return out, err
	}
	found := false
	if identity.From == "" {
		for _, c := range lineage.Rows.LineageCompletions {
			if c.PlanID == selected.PlanID {
				if found || c.PlanDigest != digest {
					return out, errStorageCustody
				}
				found = true
				origin = c.Initial
			}
		}
	} else {
		for _, f := range lineage.Rows.LineageContinuations {
			if f.PlanID == selected.PlanID {
				if found || f.PlanDigest != digest {
					return out, errStorageCustody
				}
				found = true
				origin = f.Rewrite
			}
		}
	}
	if !found || !origin.valid(2) {
		return out, errStorageCustody
	}
	selector := storageMoveCurrent{PlanID: selected.PlanID, PlanDigest: digest, CompletionDigest: sha256.Sum256(storageMoveReceiptWire(origin))}
	var row *storageMoveCurrent
	for i := range rows {
		if rows[i].PlanID != selected.PlanID {
			continue
		}
		if row != nil {
			return out, errStorageCustody
		}
		wire, e := storageMoveEncodeCurrent(rows[i])
		if e != nil {
			return out, e
		}
		copy, e := storageMoveDecodeCurrent(wire)
		if e != nil {
			return out, e
		}
		row = &copy
	}
	originalBody := selected.IdentityInitial
	if identity.From != "" {
		originalBody = selected.IdentityFromCleared
	}
	if row == nil {
		// Legacy currentness stays exact. A moved original requires the new
		// persisted selected-row relation, never a weaker path-based fallback.
		if request != nil || journal != nil || !bytes.Equal(currentPath, selected.HistoryIdentity) || identityNative != origin.PublishedObject || !bytes.Equal(identityBody, originalBody) {
			return out, errStorageCustody
		}
	} else {
		if row.PlanDigest != selector.PlanDigest || row.CompletionDigest != selector.CompletionDigest {
			return out, errStorageCustody
		}
		if _, _, err = row.origin(lineage); err != nil {
			return out, err
		}
		if row.PendingIntent == (storagebudget.Digest{}) {
			if request != nil || journal != nil || row.Current == nil || identityNative != row.Current.Receipt.PublishedObject || !bytes.Equal(identityBody, row.Current.Body) {
				return out, errStorageCustody
			}
			now, e := parseIdentity(row.Current.Body)
			if e != nil || !bytes.Equal(currentPath, []byte(now.Path)) {
				return out, errStorageCustody
			}
		} else {
			if request == nil || journal == nil || request.Plan != selected.PlanID {
				return out, errStorageCustody
			}
			d, e := request.digest()
			if e != nil || d != row.PendingIntent {
				return out, errStorageCustody
			}
			var before *storageMoveCurrent
			if row.Current != nil {
				previous := *row
				previous.PendingIntent = storagebudget.Digest{}
				before = &previous
			}
			if !request.identityBound(*journal, lineage, before) || !bytes.Equal(currentPath, request.Previous) && !bytes.Equal(currentPath, request.Destination) {
				return out, errStorageCustody
			}
			old, after, e := request.operands(lineage, before)
			if e != nil {
				return out, e
			}
			oldCurrent := identityNative == request.ExpectedOld && bytes.Equal(identityBody, old)
			afterCurrent := bytes.Equal(currentPath, request.Destination) && (journal.NativePhase == storageIdentityEntered || journal.NativePhase == storageIdentityLinkedObserved || journal.NativePhase == storageIdentityTargetDurable) && journal.AllocatedStage.valid(2) && identityNative == journal.AllocatedStage && bytes.Equal(identityBody, after)
			if !oldCurrent && !afterCurrent {
				return out, errStorageCustody
			}
			out.Pending = true
			requestRaw, e := request.encode()
			if e != nil {
				return out, e
			}
			requestCopy, e := storageMoveReadRequest(requestRaw)
			if e != nil {
				return out, e
			}
			journalRaw, e := storageEncodeIdentityIntent(*journal, selected.Installation, selected.QBinding)
			if e != nil {
				return out, e
			}
			journalCopy, e := storageDecodeIdentityIntent(journalRaw, int(journal.Core.Slot), selected.Installation, selected.QBinding)
			if e != nil {
				return out, e
			}
			out.Request, out.Journal = &requestCopy, &journalCopy
		}
	}
	// Detach immutable bytes through the original selector helper, without
	// changing P/C/F or turning the result into a writer capability.
	out.Plan, out.Origin, err = selector.originOperands(lineage)
	if err != nil {
		return storageMoveBootstrap{}, err
	}
	out.Current = row
	return out, nil
}

// The one registered existing enrollment epoch owns this invocation-local move
// comparison. It is never recovered from a DTO, installed as a registry, or
// accepted merely because an arbitrary request happens to name the same P.
type storageMoveOperation struct {
	self                              *storageMoveOperation
	phase                             *storageEnrollmentEpoch
	request                           storageMoveRequest
	before                            *storageMoveCurrent
	original                          storagePolicyOriginal
	lease                             *storageCreatorLease
	tails                             []storageMoveTailPlan
	store                             *chatDataStore
	intentClaimed, admissionAttempted bool
}

func (m *storageMoveOperation) admissionCurrent(ctx context.Context, j storageIdentityIntent, request storagebudget.Request) error {
	if m == nil || m.self != m || m.phase == nil || m.phase.self != m.phase || m.phase.move != m || m.phase.identity == nil || m.phase.identity.phase != m.phase || !reflect.DeepEqual(m.phase.identity.record, j) || j.Core.Phase != storageIntentPrepared || j.Action != storageIdentityIntentMove || !m.request.identityBound(j, m.original.Lineage, m.before) {
		return storagebudget.ErrAuthority
	}
	actual, err := storageIdentityRequest(j, m.request.Binding)
	if err != nil || !reflect.DeepEqual(actual, request) {
		return storagebudget.ErrAuthority
	}
	return m.phase.held(ctx)
}
func (o *storageBudgetOwner) originalMoveAdmissionCurrent(ctx context.Context, j storageIdentityIntent, request storagebudget.Request) error {
	if o == nil || !o.valid() {
		return storagebudget.ErrAuthority
	}
	g := &o.admission
	g.mu.Lock()
	p, generation, phase := g.enrollment, g.generation, g.phase
	g.mu.Unlock()
	if p == nil || p.c == nil || p.c.owner != o || p.generation != generation || phase != storageAdmissionEnrollment || p.move == nil {
		return storagebudget.ErrAuthority
	}
	return p.move.admissionCurrent(ctx, j, request)
}

// Read-only bootstrap uses the already adopted original project/config roots.
// Each read is bounded on the same private, non-symlink descriptor used for its
// native comparison. Cleanup failure invalidates the detached result, including
// an otherwise clean absence; no missing-child result suppresses parent Close.
func storageMoveReadPrivate(ctx context.Context, root *os.Root, name string, limit int) (raw []byte, native storageOwnerPersistentObject, err error) {
	if err = ctx.Err(); err != nil {
		return nil, native, err
	}
	info, err := root.Lstat(name)
	if err != nil {
		return nil, native, err
	}
	if !storagePrivate(info, false) || info.Size() <= 0 || info.Size() > int64(limit) {
		return nil, native, errStorageCustody
	}
	f, err := root.OpenFile(name, os.O_RDONLY|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return nil, native, err
	}
	n := storageNamedFile{root: root, name: name, file: f, identity: info, size: info.Size()}
	defer func() {
		err = errors.Join(err, n.current(), f.Close(), ctx.Err())
		if err != nil {
			raw = nil
			native = storageOwnerPersistentObject{}
		}
	}()
	if err = n.current(); err != nil {
		return nil, native, err
	}
	raw, err = readBounded(f, limit)
	if err != nil {
		return nil, native, err
	}
	native, err = storageOwnerPersistentFile(ctx, n)
	return raw, native, err
}

func (c *storageBudgetConstruction) readMoveBootstrap(ctx context.Context, state storagePolicyOriginal) (out storageMoveBootstrap, err error) {
	if c == nil || c.self != c || c.server.home != nil || c.project == nil || c.original == nil || state.Moves == nil {
		return out, storagebudget.ErrAuthority
	}
	if err = c.originCurrent(ctx, c.home.exclusive); err != nil {
		return out, err
	}
	project, err := storageOwnerPersistentRoot(ctx, c.project.own.root)
	if err != nil {
		return out, err
	}
	config, err := storageOwnerPersistentRoot(ctx, c.original.config)
	if err != nil {
		return out, err
	}
	private, _, err := openOwnFolder(c.project.own.root, []string{startupIdentityDir}, false, startupIdentityKept)
	if err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, private.Close(), ctx.Err())
		if err != nil {
			out = storageMoveBootstrap{}
		}
	}()
	privateNative, err := storageOwnerPersistentRoot(ctx, private)
	if err != nil {
		return out, err
	}
	body, identityNative, err := storageMoveReadPrivate(ctx, private, startupIdentityName, startupIdentityLimit)
	if err != nil {
		return out, err
	}
	currentPath := c.server.projectDir
	var request *storageMoveRequest
	var journal *storageIdentityIntent
	intentRaw, _, e := storageMoveReadPrivate(ctx, c.original.config, projectMoveName, 16<<10)
	if e != nil && !injectedArtifactOnlyNotExist(e) {
		return out, e
	}
	if e == nil {
		intent, found, e := decodeProjectMove(intentRaw)
		if e != nil || !found || intent.Storage == nil {
			return out, errors.Join(errStorageCustody, e)
		}
		request = intent.Storage
		if moved := c.server.locationStatus().MovedTo; moved != "" {
			if moved != intent.Destination || !sameDirectory(moved, c.project.info) {
				return out, errStorageCustody
			}
			currentPath = moved
		}
		digest, e := request.digest()
		if e != nil {
			return out, e
		}
		var row *storageMoveCurrent
		for i := range state.Moves {
			if state.Moves[i].PlanID == request.Plan {
				if row != nil {
					return out, errStorageCustody
				}
				row = &state.Moves[i]
			}
		}
		if row == nil {
			// Intent publication precedes J/Q and Pending. An absent row proves
			// neither failed admission nor permission to repeat it. Keep the exact
			// intent and original Q/debt untouched for explicit reconciliation.
			return out, errStorageEnrollmentUnresolved
		}
		if row.PendingIntent == (storagebudget.Digest{}) {
			// A committed receipt may still owe config/ack/removal. Reading it does
			// not execute those tails or infer that the original J/Q may be replayed.
			if row.Current == nil || row.Current.RequestDigest != digest || row.Current.Receipt.OperationID != request.Operation || row.Current.Receipt.PublishedBytesDigest != request.PayloadDigest || !bytes.Equal(request.Destination, []byte(currentPath)) || request.Parent != privateNative || request.Installation != state.Catalog.Installation || request.Binding != state.Catalog.QBinding {
				return out, errStorageCustody
			}
			request = nil
		} else {
			if row.PendingIntent != digest {
				return out, errStorageCustody
			}
			raw, held, e := storageOwnerReadFixed(c.original.config, storageIntentName, storageIntentBytes)
			if e != nil {
				return out, e
			}
			for slot := 0; slot < storagebudget.Slots && e == nil; slot++ {
				start := slot * 2 * storageIntentPage
				var choice storageCreationHostSelection
				choice, e = storageSelectCreationHost(raw[start:start+storageIntentPage], raw[start+storageIntentPage:start+2*storageIntentPage], slot, state.Catalog.Installation, state.Catalog.QBinding)
				if e == nil && choice.Identity != nil && choice.Identity.Core.OperationID == request.Operation {
					if journal != nil {
						e = errStorageCustody
					} else {
						copy := *choice.Identity
						journal = &copy
					}
				}
			}
			e = errors.Join(e, held.current(), held.file.Close())
			if e != nil || journal == nil {
				return out, errors.Join(errStorageCustody, e)
			}
		}
	}
	return storageMoveBootstrapJoin(state.Lineage, state.Moves, project, privateNative, config, identityNative, []byte(currentPath), body, request, journal)
}

// Arm comparison only after the actual selected bank and original sources have
// joined. Consumers always re-read the selected physical pair and current
// identity; this flag cannot substitute stale cached history for authority.
func (c *storageBudgetConstruction) selectMoveHistory(ctx context.Context, state storagePolicyOriginal) error {
	if len(state.Moves) == 0 || c.server.home != nil {
		return nil
	}
	if _, err := c.readMoveBootstrap(ctx, state); err != nil {
		return err
	}
	c.mu.Lock()
	c.moveHistory = true
	c.moveStartup = &state
	c.mu.Unlock()
	return nil
}
func (c *storageBudgetConstruction) originalStorageHistory(ctx context.Context) (string, error) {
	c.mu.Lock()
	moved, startup := c.moveHistory, c.moveStartup
	c.mu.Unlock()
	if !moved {
		return c.server.conversationIdentity(), nil
	}
	var state storagePolicyOriginal
	var err error
	if c.owner != nil {
		state, err = c.owner.readOriginalPolicy(ctx)
		if err != nil {
			return "", err
		}
	} else {
		if startup == nil || !c.home.exclusive {
			return "", storagebudget.ErrAuthority
		}
		// During the original early source cut the owner has not attached yet.
		// Recompare both actual original banks before using its strict decoded view.
		for i, name := range []string{"storage-owner.a", "storage-owner.b"} {
			raw, held, e := storageOwnerReadFixed(c.anchor, name, storageOwnerCatalogBytes)
			if e != nil {
				return "", e
			}
			e = errors.Join(held.current(), held.file.Close())
			if e != nil || !bytes.Equal(raw, startup.Actual[i]) {
				return "", errors.Join(errStorageCustody, e)
			}
		}
		state = *startup
	}
	joined, err := c.readMoveBootstrap(ctx, state)
	if err != nil {
		return "", err
	}
	return string(joined.Plan.HistoryIdentity), nil
}

// Register the original owner before acquiring the existing project/work/private
// exclusions. The existing provider and move-location scopes remain outside it.
// The epoch owns only descriptions explicitly transferred from those original
// acquisition calls; it never reopens/relocks the project or work inode.
func (s *Server) beginOriginalStorageMove(ctx context.Context) (_ *storageMoveOperation, err error) {
	c := s.storageConstruction
	if c == nil || c.owner == nil {
		return nil, nil
	}
	if s != c.server || s.home != nil {
		return nil, storagebudget.ErrAuthority
	}
	var p *storageEnrollmentEpoch
	if c.home.exclusive {
		p, err = c.beginStartupEnrollment(ctx)
	} else {
		p, err = c.beginEnrollmentEpoch(ctx)
	}
	if err != nil {
		return nil, err
	}
	m := &storageMoveOperation{phase: p}
	m.self = m
	p.move = m
	defer func() {
		if err != nil {
			err = p.finish(err)
		}
	}()
	if !p.startup {
		if err = p.selectOriginalRequester(ctx, s); err != nil {
			return nil, err
		}
	}
	p.use, err = c.owner.maintenanceUse()
	if err != nil {
		return nil, err
	}
	return m, nil
}
func (m *storageMoveOperation) attachWork(ctx context.Context, work *os.File) error {
	if m == nil || m.self != m || m.phase == nil || m.phase.move != m || m.lease == nil || m.phase.project != m.lease.file || m.phase.work != nil || work == nil {
		return storagebudget.ErrAuthority
	}
	p := m.phase
	info, err := work.Stat()
	if err != nil {
		return err
	}
	for _, n := range p.c.original.files {
		if n.root != p.c.original.config || n.name != ".data-work.lock" {
			continue
		}
		if err = n.current(); err != nil {
			return err
		}
		if !os.SameFile(info, n.identity) {
			return errStorageCustody
		}
		n.file = work
		if err = n.current(); err != nil {
			return err
		}
		p.work = &n
		p.originalRoots = p.c.owner.orderedRoots()
		return p.current(ctx)
	}
	return errStorageCustody
}

// Only this exact registered move may acquire the settings/FIFO lock without
// entering lazy history enrollment again. Ordinary callers retain that demand
// and its owner.retain guard. The lexical caller still owns Close; borrowPrivate
// subsequently validates this exact original private inode before domain holds.
func (m *storageMoveOperation) privateDataLock(ctx context.Context, s *Server) (*privateDataShortLock, error) {
	if m == nil || m.self != m || m.phase == nil || m.phase.self != m.phase || m.phase.move != m || m.phase.c == nil || m.phase.c.server != s || s == nil || s.storageConstruction != m.phase.c || m.lease == nil || m.lease.move != m || m.phase.project != m.lease.file || m.phase.work == nil {
		return nil, storagebudget.ErrAuthority
	}
	if err := m.phase.current(ctx); err != nil {
		return nil, err
	}
	lock, err := s.privateDataSettingsLockContext(ctx, true)
	if err != nil {
		return nil, err
	}
	if err = m.phase.current(ctx); err != nil {
		return nil, errors.Join(err, lock.Close())
	}
	return lock, nil
}

// Alias only the original lexical private/project/signing locks before taking
// remaining domains in native order. The lexical private lock stays with its
// caller; returned domain holds borrow it and cannot close it.
func (m *storageMoveOperation) borrowPrivate(ctx context.Context, lock *privateDataShortLock) error {
	if m == nil || m.self != m || m.phase == nil || m.phase.move != m || lock == nil || lock.File == nil {
		return storagebudget.ErrAuthority
	}
	p := m.phase
	if p.use == nil || p.project == nil || p.work == nil || len(p.holds) != 0 {
		return storagebudget.ErrAuthority
	}
	if err := p.current(ctx); err != nil {
		return err
	}
	p.use.domains = map[storageDirectoryIdentity]*storageDomainHold{}
	for _, file := range []*os.File{lock.File, p.project, p.signingFile} {
		if file == nil {
			continue
		}
		info, err := file.Stat()
		if err != nil {
			return err
		}
		for _, root := range p.originalRoots {
			if root.domain.file == nil || !os.SameFile(info, root.domain.identity) {
				continue
			}
			if file == lock.File && (root.domainDirectory || root.domainName != ".data.lock") || file != lock.File && !root.domainDirectory {
				continue
			}
			id, err := storageFileIdentity(root.domain.identity)
			if err != nil {
				return err
			}
			if p.use.domains[id] == nil {
				h := &storageDomainHold{owner: p.c.owner, root: root, lifetime: p.use, file: file}
				h.self = h
				p.use.domains[id] = h
			}
		}
	}
	type entry struct {
		index  int
		native storageOwnerPersistentObject
	}
	order := make([]entry, 0, len(p.originalRoots))
	for i, root := range p.originalRoots {
		var native storageOwnerPersistentObject
		var err error
		if !root.qControl {
			if root.domainDirectory {
				native, err = storageOwnerPersistentRoot(ctx, root.domain.root)
			} else {
				native, err = storageOwnerPersistentFile(ctx, root.domain)
			}
			if err != nil {
				return err
			}
		}
		order = append(order, entry{i, native})
	}
	sort.SliceStable(order, func(i, j int) bool { return storageOwnerPOCompare(order[i].native, order[j].native) < 0 })
	p.holds = make([]*storageDomainHold, len(p.originalRoots))
	for _, row := range order {
		h, err := p.c.owner.domain(ctx, p.originalRoots[row.index], p.use)
		if err != nil {
			return err
		}
		p.holds[row.index] = h
	}
	p.sourceHoldsComplete = true
	return p.held(ctx)
}

// Four fixed existing project-move writes, not arbitrary paths or an export
// list: desk config, current history binding, source-approval historical mark,
// and the empty old conversation anchor when it is genuinely required.
const storageMoveFixedTailMaximum = 4
const storageMoveTailMaximum = 5

type storageMoveTail struct {
	Kind                      byte
	Root                      storagebudget.ID
	Generation                uint64
	Before                    storageOwnerPersistentObject
	BeforeDigest, AfterDigest storagebudget.Digest
	BeforeBytes, AfterBytes   uint64
}

func (v storageMoveTail) valid() bool {
	if v.Kind < 1 || v.Kind > storageMoveTailMaximum || v.Root == (storagebudget.ID{}) || v.Generation == 0 || v.AfterDigest == (storagebudget.Digest{}) {
		return false
	}
	max := uint64(maxDeskConfigBytes)
	switch v.Kind {
	case 2:
		max = maxProjectBindingsBytes
	case 3:
		max = assistantRunRecordLimit
	case 5:
		max = dialectArtifactLocationRecordBytes
	case 4:
		max = uint64(len(`{"version":1,"chats":[]}`))
	}
	if v.AfterBytes == 0 || v.AfterBytes > max || v.BeforeBytes > max {
		return false
	}
	if v.Before == (storageOwnerPersistentObject{}) {
		return v.BeforeBytes == 0 && v.BeforeDigest == (storagebudget.Digest{})
	}
	return v.Before.valid(2) && v.BeforeDigest != (storagebudget.Digest{}) && v.BeforeBytes != 0
}
func storageMoveEncodeTails(rows []storageMoveTail) ([]byte, error) {
	if len(rows) > storageMoveTailMaximum {
		return nil, storagebudget.ErrBounds
	}
	w := storageEnrollmentPeakWriter{max: 1 + storageMoveTailMaximum*140}
	w.put([]byte{byte(len(rows))})
	for i, v := range rows {
		if !v.valid() || i > 0 && rows[i-1].Kind >= v.Kind {
			return nil, storagebudget.ErrBounds
		}
		w.put([]byte{v.Kind})
		w.put(v.Root[:])
		w.u64(v.Generation)
		w.put(storageEnrollmentPeakNativeBytes(v.Before))
		w.put(v.BeforeDigest[:])
		w.put(v.AfterDigest[:])
		w.u64(v.BeforeBytes)
		w.u64(v.AfterBytes)
	}
	if w.bad {
		return nil, storagebudget.ErrBounds
	}
	return w.raw, nil
}
func storageMoveReadTails(r *storageEnrollmentPeakReader) ([]storageMoveTail, error) {
	count := int(r.u8())
	if count > storageMoveTailMaximum {
		return nil, storagebudget.ErrBounds
	}
	var out []storageMoveTail
	for i := 0; i < count; i++ {
		v := storageMoveTail{Kind: r.u8(), Root: r.id(), Generation: r.u64(), Before: r.native(), BeforeDigest: r.digest(), AfterDigest: r.digest(), BeforeBytes: r.u64(), AfterBytes: r.u64()}
		if !v.valid() || i > 0 && out[i-1].Kind >= v.Kind {
			return nil, storagebudget.ErrBounds
		}
		out = append(out, v)
	}
	return out, nil
}

// The existing fixed J page keeps only the sufficient per-component settlement
// operands and actual staged native observations. Full immutable before/after
// hashes stay in the existing move intent until original completion/ack.
type storageMoveTailJournal struct {
	Plan                            storagebudget.Digest
	Count, Allocated, Entered, Done byte
	Component                       [storageMoveTailMaximum]byte
	BeforeBytes, AfterBytes         [storageMoveTailMaximum]uint64
	BeforeExists                    [storageMoveTailMaximum]bool
	Staged                          [storageMoveTailMaximum]storageOwnerPersistentObject
}

func storageMoveTailRequestBound(tails []storageMoveTail, j storageMoveTailJournal, components []storagebudget.Component) bool {
	if len(tails) == 0 {
		return j == (storageMoveTailJournal{})
	}
	raw, err := storageMoveEncodeTails(tails)
	if err != nil || j.Count != byte(len(tails)) || j.Plan != sha256.Sum256(raw) {
		return false
	}
	for i, t := range tails {
		if int(j.Component[i]) >= len(components) || components[j.Component[i]].Root != t.Root || components[j.Component[i]].RootGeneration != t.Generation || j.BeforeBytes[i] != t.BeforeBytes || j.AfterBytes[i] != t.AfterBytes || j.BeforeExists[i] != (t.Before != (storageOwnerPersistentObject{})) {
			return false
		}
	}
	return true
}
func (j storageMoveTailJournal) valid(c storageIntentRecord) bool {
	if j.Count == 0 {
		return j == (storageMoveTailJournal{})
	}
	if j.Count > storageMoveTailMaximum || j.Plan == (storagebudget.Digest{}) {
		return false
	}
	mask := byte(1<<j.Count) - 1
	if (j.Allocated|j.Entered|j.Done)&^mask != 0 || j.Entered&^j.Allocated != 0 || j.Done&^j.Entered != 0 {
		return false
	}
	for i := 0; i < storageMoveTailMaximum; i++ {
		if i >= int(j.Count) {
			if j.Component[i] != 0 || j.BeforeBytes[i] != 0 || j.AfterBytes[i] != 0 || j.BeforeExists[i] || j.Staged[i] != (storageOwnerPersistentObject{}) {
				return false
			}
			continue
		}
		if int(j.Component[i]) >= len(c.Components) || j.AfterBytes[i] == 0 || j.AfterBytes[i] > assistantRunRecordLimit || j.BeforeBytes[i] > assistantRunRecordLimit || !j.BeforeExists[i] && j.BeforeBytes[i] != 0 {
			return false
		}
		if j.Allocated&(1<<i) == 0 {
			if j.Staged[i] != (storageOwnerPersistentObject{}) {
				return false
			}
		} else if !j.Staged[i].valid(2) {
			return false
		}
	}
	if c.Phase >= storageIntentSettling && c.Outcome == storageIntentPublished && j.Done != mask {
		return false
	}
	if c.Outcome == storageIntentAborted && (j.Allocated|j.Entered|j.Done) != 0 {
		return false
	}
	return true
}
func storageMoveTailJournalTransition(a, b storageMoveTailJournal) bool {
	originalA, originalB := a, b
	a.Allocated, a.Entered, a.Done = 0, 0, 0
	b.Allocated, b.Entered, b.Done = 0, 0, 0
	a.Staged, b.Staged = [storageMoveTailMaximum]storageOwnerPersistentObject{}, [storageMoveTailMaximum]storageOwnerPersistentObject{}
	if a != b || originalA.Allocated&^originalB.Allocated != 0 || originalA.Entered&^originalB.Entered != 0 || originalA.Done&^originalB.Done != 0 {
		return false
	}
	for i := 0; i < storageMoveTailMaximum; i++ {
		if originalA.Allocated&(1<<i) != 0 && originalA.Staged[i] != originalB.Staged[i] {
			return false
		}
	}
	return true
}

type storageMoveTailPlan struct {
	value         storageMoveTail
	binding       *storageRootBinding
	root          *os.Root
	name          string
	before, after []byte
	artifact      *dialectArtifactProjectMoveHold
}

func (m *storageMoveOperation) captureTails(ctx context.Context, store *chatDataStore, intent projectMoveIntent) error {
	p := m.phase
	if p == nil || p.move != m || len(m.tails) != 0 || store == nil || store.root == nil {
		return storagebudget.ErrAuthority
	}
	var control, history *storageRootBinding
	if err := p.held(ctx); err != nil {
		return err
	}
	if err := p.c.sources.current(ctx); err != nil {
		return err
	}
	// Bind the exact currently selected store, not another same-device root or
	// a pathname supplied by the request. The original pointer/sources are held.
	selected := false
	for _, v := range p.c.sources.selections {
		if v.location == store.location && v.path == p.c.server.configDir {
			selected = true
		}
	}
	if !selected {
		return errStorageCustody
	}
	native, err := storageOwnerPersistentRoot(ctx, store.root)
	if err != nil {
		return err
	}
	for _, r := range p.originalRoots {
		if r.configControl {
			if control != nil {
				return errStorageCustody
			}
			control = r
		}
		if r.source != nil && r.source.role == 4 && r.source.path == store.location.Path && r.category == storagebudget.History && r.source.persistent == native {
			member := false
			for _, id := range r.references {
				member = member || id == p.identityPlan.Workspace.ID
			}
			if member {
				if history != nil {
					return errStorageCustody
				}
				history = r
			}
		}
	}
	if control == nil || history == nil {
		return errStorageCustody
	}
	appendTail := func(kind byte, binding *storageRootBinding, root *os.Root, name string, after []byte, limit int) error {
		if len(after) == 0 || len(after) > limit {
			return storagebudget.ErrBounds
		}
		before, beforeNative, e := storageMoveReadPrivate(ctx, root, name, limit)
		absent := injectedArtifactOnlyNotExist(e)
		if e != nil && !absent {
			return e
		}
		if !absent && bytes.Equal(before, after) {
			return nil
		}
		v := storageMoveTail{Kind: kind, Root: binding.row.ID, Generation: binding.row.Generation, AfterDigest: sha256.Sum256(after), AfterBytes: uint64(len(after))}
		if !absent {
			v.Before = beforeNative
			v.BeforeDigest = sha256.Sum256(before)
			v.BeforeBytes = uint64(len(before))
		}
		if !v.valid() {
			return storagebudget.ErrBounds
		}
		m.tails = append(m.tails, storageMoveTailPlan{value: v, binding: binding, root: root, name: name, before: bytes.Clone(before), after: bytes.Clone(after)})
		return nil
	}
	present, config, err := p.c.server.readDeskFile()
	if err != nil {
		return err
	}
	revision := ""
	if present {
		revision = digestOf(config)
	}
	if revision != intent.ConfigRevision {
		return storagebudget.ErrStale
	}
	project, _ := json.Marshal(map[string]string{"file": filepath.Join(intent.Destination, projectConfigName)})
	composed, problems := composeDeskFile(config, present, nil, project, nil)
	if len(problems) != 0 || len(composed) > maxDeskConfigBytes || !validUTF8(composed) || decodeDeskFile(composed).refused() {
		return storagebudget.ErrBounds
	}
	if err = appendTail(1, control, p.c.original.config, deskConfigName, composed, maxDeskConfigBytes); err != nil {
		return err
	}
	bindings, source, found, err := movedHistoryBinding(store.root, intent.Previous, intent.Destination)
	if err != nil {
		return err
	}
	if intent.Retained != nil {
		namespace, e := assistantRunNamespace(store.root, intent.Previous)
		if e != nil || namespace != intent.Retained.Namespace {
			return errors.Join(errStorageCustody, e)
		}
		found = true
	}
	if found && bindings.Projects[intent.Destination] != source {
		bindings.Projects[intent.Destination] = source
		after, e := json.Marshal(bindings)
		if e != nil {
			return e
		}
		if _, e = decodeProjectBindings(after); e != nil {
			return e
		}
		if err = appendTail(2, history, store.root, projectBindingsName, after, maxProjectBindingsBytes); err != nil {
			return err
		}
		name := strings.Replace(source, "conversations-", "assistant-source-approvals-", 1)
		raw, _, e := storageMoveReadPrivate(ctx, store.root, name, assistantRunRecordLimit)
		if e != nil && !injectedArtifactOnlyNotExist(e) {
			return e
		}
		if e == nil {
			after, e := historicalAssistantSourceStorage(name, raw)
			if e != nil {
				return e
			}
			if err = appendTail(3, history, store.root, name, after, assistantRunRecordLimit); err != nil {
				return err
			}
		}
		if _, e = store.root.Lstat(source); e != nil {
			if !injectedArtifactOnlyNotExist(e) {
				return e
			}
			if err = appendTail(4, history, store.root, source, []byte(`{"version":1,"chats":[]}`), len(`{"version":1,"chats":[]}`)); err != nil {
				return err
			}
		}
	}
	// The optional retained wrapper creates the anchor even for an already
	// linked history. Keep that exact old branch in the counted closure too.
	if intent.Retained != nil && bindings.Projects[intent.Destination] == source {
		has := false
		for _, v := range m.tails {
			has = has || v.value.Kind == 4
		}
		if !has {
			if _, e := store.root.Lstat(source); e != nil {
				if !injectedArtifactOnlyNotExist(e) {
					return e
				}
				if err = appendTail(4, history, store.root, source, []byte(`{"version":1,"chats":[]}`), len(`{"version":1,"chats":[]}`)); err != nil {
					return err
				}
			}
		}
	}
	sort.Slice(m.tails, func(i, j int) bool { return m.tails[i].value.Kind < m.tails[j].value.Kind })
	m.store = store
	return p.held(ctx)
}

func (m *storageMoveOperation) tailJournal(components []storagebudget.Component) (storageMoveTailJournal, error) {
	var out storageMoveTailJournal
	if len(m.tails) == 0 {
		return out, nil
	}
	digest, err := m.request.tailCommit()
	if err != nil {
		return out, err
	}
	out.Count, out.Plan = byte(len(m.tails)), digest
	for i, v := range m.tails {
		t := v.value
		index := -1
		for k, c := range components {
			if c.Root == t.Root && c.RootGeneration == t.Generation {
				index = k
				break
			}
		}
		if index < 0 || i >= storageMoveTailMaximum {
			return storageMoveTailJournal{}, errStorageCustody
		}
		out.Component[i], out.BeforeBytes[i], out.AfterBytes[i], out.BeforeExists[i] = byte(index), t.BeforeBytes, t.AfterBytes, t.Before != (storageOwnerPersistentObject{})
	}
	return out, nil
}

func (m *storageMoveOperation) prepare(ctx context.Context, private *os.Root, store *chatDataStore, intent projectMoveIntent, hold *dialectArtifactProjectMoveHold) (projectMoveIntent, error) {
	p := m.phase
	if p == nil || p.move != m || m.self != m || p.original != nil || p.identity != nil {
		return intent, storagebudget.ErrAuthority
	}
	if err := p.captureIdentityOriginal(ctx); err != nil {
		return intent, err
	}
	if err := p.captureOriginalIdentityPlan(ctx); err != nil {
		return intent, err
	}
	if p.qualified == nil {
		if err := p.qualifyOriginalNative(ctx); err != nil {
			return intent, err
		}
	}
	complete, err := p.identityCompletedCurrent(ctx)
	if err != nil || !complete {
		return intent, errors.Join(errStorageEnrollmentUnresolved, err)
	}
	p.identityDone = true
	state, err := p.c.owner.readOriginalPolicy(ctx)
	if err != nil {
		return intent, err
	}
	if state.RecoveryOnly {
		return intent, errStorageEnrollmentUnresolved
	}
	m.original = state
	privateNative, err := storageOwnerPersistentRoot(ctx, private)
	if err != nil {
		return intent, err
	}
	if privateNative != p.identityPlan.PrivateObject {
		return intent, errStorageCustody
	}
	body, native, err := storageMoveReadPrivate(ctx, private, startupIdentityName, startupIdentityLimit)
	if err != nil {
		return intent, err
	}
	joined, err := storageMoveBootstrapJoin(state.Lineage, state.Moves, p.identityPlan.ProjectObject, privateNative, p.identityPlan.ConfigObject, native, []byte(intent.Previous), body, nil, nil)
	if err != nil || !reflect.DeepEqual(joined.Plan, *p.identityPlan) || joined.Pending {
		return intent, errors.Join(errStorageCustody, err)
	}
	m.before = joined.Current
	planDigest, err := storageOwnerV5PlanDigest(joined.Plan)
	if err != nil {
		return intent, err
	}
	operation, err := storagebudget.NewID()
	if err != nil {
		return intent, err
	}
	record, err := parseIdentity(body)
	if err != nil {
		return intent, err
	}
	record.Path = intent.Destination
	request := storageMoveRequest{Installation: state.Catalog.Installation, Binding: state.Catalog.QBinding, Operation: operation, Plan: joined.Plan.PlanID, PlanDigest: planDigest, CompletionDigest: sha256.Sum256(storageMoveReceiptWire(joined.Origin)), Previous: []byte(intent.Previous), Destination: []byte(intent.Destination), Parent: privateNative, ExpectedOld: native, PayloadDigest: sha256.Sum256(record.line())}
	if intent.ConfigRevision != "" {
		b, e := hex.DecodeString(intent.ConfigRevision)
		if e != nil || len(b) != 32 {
			return intent, storagebudget.ErrBounds
		}
		copy(request.ConfigRevision[:], b)
	}
	if m.before != nil {
		raw, e := storageMoveEncodeCurrent(*m.before)
		if e != nil {
			return intent, e
		}
		request.BeforeCurrent = sha256.Sum256(raw)
	}
	if err = m.captureTails(ctx, store, intent); err != nil {
		return intent, err
	}
	for _, tail := range m.tails {
		request.Tails = append(request.Tails, tail.value)
	}
	m.request = request
	if err = m.captureArtifact(ctx, intent.Retained, hold, false); err != nil {
		return intent, err
	}
	if _, err = m.request.encode(); err != nil {
		return intent, err
	}
	if err = m.prepareLaunch(ctx); err != nil {
		return intent, err
	}
	live, err := p.issueOriginalIdentity(ctx, storageIdentityIntentMove)
	if err != nil {
		return intent, err
	}
	p.identity = live
	// The immutable original P is complete, but this newly issued move is not.
	// Only the outer successful cleanup finalizer may finish this action.
	p.identityDone = false
	intent.Version = 2
	intent.Storage = &m.request
	if _, err = storageMoveEncodeIntent(intent); err != nil {
		return intent, err
	}
	return intent, nil
}

func (m *storageMoveOperation) prepareLaunch(ctx context.Context) error {
	p := m.phase
	if p == nil || p.move != m || p.identityPlan == nil || p.identityPrivate == nil || p.identityLaunch != nil {
		return storagebudget.ErrAuthority
	}
	groups, err := storageIdentityFiniteGroups(storageIdentityIntentMove, false, true)
	if err != nil {
		return err
	}
	// Two fixed actual native current-row publications. The original immutable
	// P/C/S/F completion room remains a separate retained closure obligation.
	groups = append(groups, storageIdentityFiniteGroup{ID: 50, Parent: 1, Bytes: storageOwnerCatalogBytes, Metadata: 4}, storageIdentityFiniteGroup{ID: 51, Parent: 1, Metadata: 4}, storageIdentityFiniteGroup{ID: 52, Parent: 1, Bytes: storageOwnerCatalogBytes, Metadata: 4}, storageIdentityFiniteGroup{ID: 53, Parent: 1, Metadata: 4}, storageIdentityFiniteGroup{ID: 54, Parent: 1, Bytes: 5898, Files: 1, Metadata: 16}, storageIdentityFiniteGroup{ID: 56, Parent: 1, Metadata: 16})
	var control, anchor *storageRootBinding
	for _, r := range p.originalRoots {
		if r.configControl {
			control = r
		}
		if r.qControl {
			anchor = r
		}
	}
	if control == nil || anchor == nil {
		return errStorageCustody
	}
	roots := map[storagebudget.ID]*storageRootBinding{}
	debt := map[storagebudget.ID]storagebudget.Vector{}
	peaks := map[storagebudget.ID]storagebudget.CapacityVector{}
	var next storageIdentityLaunch
	if old := p.c.identityLaunch; old != nil {
		if old.self != old || old.construction != p.c || old.failed != nil {
			return storagebudget.ErrAuthority
		}
		next = *old
		next.groups = append([]storageIdentityLaunchGroup{}, old.groups...)
		next.applications = append([]storageIdentityLaunchApplication{}, old.applications...)
		next.components = append([]storagebudget.Component{}, old.components...)
	}
	next.construction = p.c
	add := func(binding *storageRootBinding, parent *os.Root, id uint16, b, f, d, metadata uint64) error {
		native, e := storageOwnerPersistentRoot(ctx, parent)
		if e != nil {
			return e
		}
		key := storageIdentityLaunchKey{Plan: m.request.Operation, Action: storageIdentityIntentMove, Attempt: func() byte {
			if p.identity != nil && p.identity.recovering {
				return 2
			}
			return 1
		}(), Group: id, Parent: native}
		for _, v := range next.groups {
			if v.key == key {
				return storagebudget.ErrBounds
			}
		}
		peak, e := storageAdvisoryEstimate(b, f, d, metadata)
		if e != nil {
			return e
		}
		next.peak, e = storageIdentityAddCapacity(next.peak, peak)
		if e != nil {
			return e
		}
		peaks[binding.row.ID], e = storageIdentityAddCapacity(peaks[binding.row.ID], peak)
		if e != nil {
			return e
		}
		debt[binding.row.ID], e = storageIdentityAddVector(debt[binding.row.ID], storagebudget.Vector{Bytes: b, Files: f, Directories: d})
		if e != nil {
			return e
		}
		roots[binding.row.ID] = binding
		next.groups = append(next.groups, storageIdentityLaunchGroup{key: key, parent: parent, bytes: b, files: f, directories: d, metadata: metadata})
		return nil
	}
	for _, g := range groups {
		binding := control
		if g.Parent == 2 {
			binding = anchor
		} else if g.Parent == 3 {
			binding = p.identityPrivate
		}
		if g.Parent == 4 {
			return storagebudget.ErrAuthority
		}
		if err = add(binding, binding.root, g.ID, g.Bytes, g.Files, g.Directories, g.Metadata); err != nil {
			return err
		}
	}
	for i, t := range m.tails {
		// create, payload/sync and single entered CAS/readback/parent sync; every
		// original native call remains inside its complete consumed finite group.
		for step, row := range [][3]uint64{{0, 1, 8}, {t.value.AfterBytes, 0, 8}, {0, 0, 16}} {
			if err = add(t.binding, t.root, uint16(60+3*i+step), row[0], row[1], 0, row[2]); err != nil {
				return err
			}
		}
	}
	// Identity remains component zero; roots and all attribution peers are exact
	// original registered mappings even when a history tail is on another volume.
	ordered := []*storageRootBinding{p.identityPrivate}
	ids := make([]storagebudget.ID, 0, len(roots))
	for id := range roots {
		if id != p.identityPrivate.row.ID {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return bytes.Compare(ids[i][:], ids[j][:]) < 0 })
	for _, id := range ids {
		ordered = append(ordered, roots[id])
	}
	var components []storagebudget.Component
	for _, r := range ordered {
		refs := r.references
		if r.configControl || r.qControl {
			refs = nil
		}
		if len(refs) == 0 {
			refs = []storagebudget.ID{{}}
		}
		fresh := storagebudget.Vector{}
		if r == p.identityPrivate {
			fresh = storagebudget.Vector{Bytes: uint64(len(m.request.Destination)) + startupIdentityLimit, Files: 1}
		}
		for _, t := range m.tails {
			if t.binding == r {
				fresh, err = storageIdentityAddVector(fresh, storagebudget.Vector{Bytes: t.value.AfterBytes, Files: 1})
				if err != nil {
					return err
				}
			}
		}
		for i, ref := range refs {
			physical, settlement := fresh, debt[r.row.ID]
			if i > 0 {
				physical, settlement = storagebudget.Vector{}, storagebudget.Vector{}
			}
			attribution, settledAttribution := fresh, debt[r.row.ID]
			if ref == (storagebudget.ID{}) {
				attribution, settledAttribution = storagebudget.Vector{}, storagebudget.Vector{}
			}
			c := storageOwnerComponent(r, ref, r.category, physical, attribution, settlement)
			c.SettlementAttribution = settledAttribution
			if i == 0 {
				for k := 0; k < int(c.CapacityCount); k++ {
					c.Capacity[k].Peak = peaks[r.row.ID]
				}
			}
			components = append(components, c)
		}
	}
	if p.identity != nil && p.identity.recovering {
		// Reconstruct the original admitted request byte-for-byte. Current
		// usage, policy or completed tail bytes cannot replace its promises.
		components = append([]storagebudget.Component{}, p.identity.record.Core.Components...)
		for _, root := range ordered {
			var promised storagebudget.Vector
			for _, c := range components {
				if c.Root == root.row.ID {
					promised, err = storageIdentityAddVector(promised, c.SettlementPhysical)
					if err != nil {
						return err
					}
				}
			}
			need := debt[root.row.ID]
			if promised.Bytes < need.Bytes || promised.Files < need.Files || promised.Directories < need.Directories {
				return errStorageEnrollmentUnresolved
			}
		}
	}
	components, err = storageIdentityComponentUnion(nil, components)
	if err != nil {
		return err
	}
	// ComponentUnion preserves first occurrence/root order and every fanout.
	p.identityApplications = map[byte][]storagebudget.Component{storageIdentityIntentMove: components}
	next.components = append(next.components, components...)
	next.applications = append(next.applications, storageIdentityLaunchApplication{plan: p.identityPlan.PlanID, action: storageIdentityIntentMove, components: components, settled: p.identity != nil && p.identity.recovering && p.identity.record.Core.Phase >= storageIntentSettling})
	for _, c := range components {
		next.debt, err = storageIdentityAddVector(next.debt, c.SettlementPhysical)
		if err != nil {
			return err
		}
	}
	if err = p.checkIdentityLaunch(ctx, &next); err != nil {
		return err
	}
	next.self = &next
	p.identityLaunch = &next
	return p.held(ctx)
}

func (m *storageMoveOperation) pendingRows() ([]storageMoveCurrent, error) {
	digest, err := m.request.digest()
	if err != nil {
		return nil, err
	}
	row := storageMoveCurrent{PlanID: m.request.Plan, PlanDigest: m.request.PlanDigest, CompletionDigest: m.request.CompletionDigest, PendingIntent: digest}
	if m.before != nil {
		row.Current = m.before.Current
	}
	if m.original.RecoveryOnly {
		for _, existing := range m.original.Moves {
			if existing.PlanID == row.PlanID && existing.PendingIntent == digest && reflect.DeepEqual(existing.Current, row.Current) {
				return m.original.Moves, nil
			}
		}
		return nil, errStorageCustody
	}
	rows := m.original.Moves
	if rows == nil {
		rows = []storageMoveCurrent{}
	} // canonical internal constructor only
	return storageMoveReplaceCurrent(rows, m.before, row)
}
func (m *storageMoveOperation) publishRows(ctx context.Context, rows []storageMoveCurrent, complete bool) error {
	p := m.phase
	if p == nil || p.move != m || p.identity == nil || !m.request.identityBound(p.identity.record, m.original.Lineage, m.before) {
		return storagebudget.ErrAuthority
	}
	actual := p.original.Catalogs
	// Initial completed duplication and subsequent Pending/Active publications
	// retain the strict current pair's semantic bank, not a header tie-break.
	pair, err := storageOwnerV6NativeReadPair(actual[0], actual[1], nil, m.original.Evidence.Raw, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
	if err != nil {
		return storagebudget.ErrBounds
	}
	raw, bank, err := storageMoveNativePublication(actual, pair.Selected, m.original.Evidence.Raw, m.original.Catalog, m.request.Operation, rows, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
	if err != nil {
		return err
	}
	phase := "prepared"
	if complete {
		phase = "active"
	}
	if err = p.identityCatalogGroup(ctx, "move/"+phase+"/publish", bank, raw, true); err != nil {
		return err
	}
	if err = p.identityCatalogGroup(ctx, "move/"+phase+"/persist", bank, raw, false); err != nil {
		return err
	}
	p.identityCatalogsKnown()
	return p.held(ctx)
}
func (m *storageMoveOperation) begin(ctx context.Context, intent projectMoveIntent) error {
	p := m.phase
	if p == nil || p.identity == nil || intent.Storage != &m.request {
		return storagebudget.ErrAuthority
	}
	rows, err := m.pendingRows()
	if err != nil {
		return err
	}
	// Refuse the actual two-bank current+owed size before the intent/J/Q prefix.
	pendingRaw, _, err := storageMoveNativePublication(p.original.Catalogs, m.original.Selected, m.original.Evidence.Raw, m.original.Catalog, m.request.Operation, rows, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
	if err != nil {
		return err
	}
	pendingView, err := storageOwnerV6NativeDecode(pendingRaw, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
	if err != nil {
		return err
	}
	room, err := storageOwnerV6NativeClosureSize(pendingView)
	if err != nil {
		return err
	}
	set, err := storageMoveEncodeCurrents(rows)
	if err != nil {
		return err
	}
	growth := 295 + len(p.identity.payload)
	if m.before != nil && m.before.Current != nil {
		growth = max(0, len(p.identity.payload)-len(m.before.Current.Body))
	}
	// A later mandatory source cut retains BOTH factual bank sets. The new
	// current set differs from the pending set; intern only actual equality,
	// never assume shared history. Existing owed C/S/F is already in Future.
	extra := len(set) + growth + 16
	if extra > 135168 || room.Future > 135168-extra {
		return storagebudget.ErrBounds
	}
	raw, err := storageMoveEncodeIntent(intent)
	if err != nil {
		return err
	}
	if err = p.identity.native(ctx, 54, func() error { return claimPrivateData(p.c.original.config, projectMoveName, raw) }); err != nil {
		return err
	}
	m.intentClaimed = true
	if testProjectMoveStep != nil {
		if err = testProjectMoveStep("intent-claimed"); err != nil {
			return err
		}
	}
	if err = p.identity.admit(ctx); err != nil {
		return err
	}
	if testProjectMoveStep != nil {
		if err = testProjectMoveStep("admitted-before-stage"); err != nil {
			return err
		}
	}
	// Capture actual staged identity native/body before project rename, then
	// durably select Pending. Recovery never needs a hash to reconstruct bytes.
	if err = p.identity.publishIdentityTo(ctx, true); err != nil {
		return err
	}
	return m.publishRows(ctx, rows, false)
}
func (m *storageMoveOperation) complete(ctx context.Context) error {
	p := m.phase
	if p == nil || p.move != m || p.identity == nil {
		return storagebudget.ErrAuthority
	}
	// A terminal-before-current cut owes only factual validation and the
	// already-funded current-row publication. Never re-enter native CAS.
	if p.identity.record.Core.Phase < storageIntentSettling {
		if err := p.identity.publishIdentity(ctx); err != nil {
			return err
		}
	} else if p.identity.record.NativePhase != storageIdentityTargetDurable {
		return errStorageEnrollmentUnresolved
	} else if err := p.identity.recoveryTargetCurrent(ctx); err != nil {
		return err
	}
	if err := m.writeTails(ctx); err != nil {
		return err
	}
	if err := p.identity.settle(ctx); err != nil {
		return err
	}
	receipt, err := p.identity.receipt(ctx)
	if err != nil {
		return err
	}
	if testProjectMoveStep != nil {
		if err = testProjectMoveStep("terminal-before-current"); err != nil {
			return err
		}
	}
	rows, err := m.pendingRows()
	if err != nil {
		return err
	}
	var pending *storageMoveCurrent
	for i := range rows {
		if rows[i].PlanID == m.request.Plan {
			pending = &rows[i]
		}
	}
	if pending == nil {
		return errStorageCustody
	}
	current := *pending
	current.PendingIntent = storagebudget.Digest{}
	digest, err := m.request.digest()
	if err != nil {
		return err
	}
	current.Current = &storageMovePublication{RequestDigest: digest, Receipt: receipt, Body: bytes.Clone(p.identity.payload)}
	rows, err = storageMoveReplaceCurrent(rows, pending, current)
	if err != nil {
		return err
	}
	if err = m.publishRows(ctx, rows, true); err != nil {
		return err
	}
	// The lexical outer caller owns the artifact hold and all other cleanup.
	// It marks completion only after those original Close operations succeed.
	return nil
}
func (m *storageMoveOperation) closePrivate() error {
	p := m.phase
	if p == nil {
		return storagebudget.ErrAuthority
	}
	var err error
	if p.identity != nil {
		err = errors.Join(err, p.identity.Close())
	}
	if p.identityOriginal != nil {
		old := p.identityOriginal
		p.identityOriginal = nil
		if p.identity != nil && p.identity.old == old {
			p.identity.old = nil
		}
		err = errors.Join(err, old.Close())
	}
	return err
}
func (m *storageMoveOperation) tailBefore(ctx context.Context, t storageMoveTailPlan) error {
	raw, native, err := storageMoveReadPrivate(ctx, t.root, t.name, int(max(t.value.BeforeBytes, t.value.AfterBytes)))
	if t.value.Before == (storageOwnerPersistentObject{}) {
		if injectedArtifactOnlyNotExist(err) {
			return nil
		}
		return errors.Join(errStorageCustody, err)
	}
	if err != nil || native != t.value.Before || uint64(len(raw)) != t.value.BeforeBytes || sha256.Sum256(raw) != t.value.BeforeDigest {
		return errors.Join(errStorageCustody, err)
	}
	return t.binding.current(ctx)
}
func (m *storageMoveOperation) writeTails(ctx context.Context) error {
	live := m.phase.identity
	if live == nil || live.record.Action != storageIdentityIntentMove || live.record.NativePhase != storageIdentityTargetDurable || !m.request.tailBound(live.record.MoveTails, live.record.Core.Components) {
		return storagebudget.ErrAuthority
	}
	// Original history ordering: anchor, historical source mark, binding, config.
	// The immutable request uses canonical kind order, independent of execution.
	for _, kind := range []byte{4, 3, 2, 1} {
		for i, t := range m.tails {
			if t.value.Kind != kind {
				continue
			}
			if err := m.writeTail(ctx, i, t); err != nil {
				return err
			}
		}
	}
	return live.current(ctx)
}
func (m *storageMoveOperation) writeTail(ctx context.Context, index int, t storageMoveTailPlan) (err error) {
	live := m.phase.identity
	if index < 0 || index >= len(m.tails) || t.value != m.tails[index].value || t.binding.row.ID != t.value.Root || t.binding.row.Generation != t.value.Generation {
		return storagebudget.ErrAuthority
	}
	if t.artifact != nil {
		if err = t.artifact.check(); err != nil {
			return err
		}
		parent, e := storageOwnerPersistentRoot(ctx, t.root)
		if e != nil || m.request.Artifact == nil || parent != m.request.Artifact.Parent {
			return errors.Join(errStorageCustody, e)
		}
	}
	bit := byte(1 << index)
	stage := identityStagingPrefix + hex.EncodeToString(m.request.Operation[:]) + "-m" + string([]byte{'0' + t.value.Kind})
	validateAfter := func(name string) error {
		raw, native, e := storageMoveReadPrivate(ctx, t.root, name, int(t.value.AfterBytes))
		if e != nil || native != live.record.MoveTails.Staged[index] || uint64(len(raw)) != t.value.AfterBytes || sha256.Sum256(raw) != t.value.AfterDigest {
			return errors.Join(errStorageCustody, e)
		}
		return t.binding.current(ctx)
	}
	if live.record.MoveTails.Done&bit != 0 {
		return validateAfter(t.name)
	}
	if live.record.MoveTails.Allocated&bit == 0 {
		var native storageOwnerPersistentObject
		if err = live.native(ctx, uint16(60+3*index), func() (err error) {
			if err = m.tailBefore(ctx, t); err != nil {
				return err
			}
			f, e := t.root.OpenFile(stage, os.O_RDWR|os.O_CREATE|os.O_EXCL|openNoFollow, 0600)
			if e != nil {
				return e
			}
			defer func() { err = errors.Join(err, f.Close()) }()
			parent, e := t.root.Open(".")
			if e != nil {
				return e
			}
			native, e = storageAdvisoryPersistentFileHeld(ctx, parent, f)
			return errors.Join(e, parent.Close())
		}); err != nil {
			return err
		}
		next := live.next(live.record.Core.Phase)
		next.MoveTails.Allocated |= bit
		next.MoveTails.Staged[index] = native
		if err = live.publish(ctx, next); err != nil {
			return err
		}
	}
	if live.record.MoveTails.Entered&bit == 0 {
		if err = live.native(ctx, uint16(61+3*index), func() (err error) {
			if err = m.tailBefore(ctx, t); err != nil {
				return err
			}
			info, e := t.root.Lstat(stage)
			if e != nil || info.Size() < 0 || uint64(info.Size()) > t.value.AfterBytes {
				return errors.Join(errStorageCustody, e)
			}
			file, e := storageOpenFile(t.root, stage, info.Size())
			if e != nil {
				return e
			}
			defer func() { err = errors.Join(err, file.file.Close()) }()
			native, e := storageOwnerPersistentFile(ctx, file)
			if e != nil || native != live.record.MoveTails.Staged[index] {
				return errors.Join(errStorageCustody, e)
			}
			if err = storageEnrollmentNativeWrite(ctx, file.file, t.after, 0); err != nil {
				return err
			}
			if err = m.phase.permit(ctx); err != nil {
				return err
			}
			if err = file.file.Chmod(0600); err != nil {
				return err
			}
			if err = file.file.Sync(); err != nil {
				return err
			}
			return nil
		}); err != nil {
			return err
		}
		if err = validateAfter(stage); err != nil {
			return err
		}
		if err = syncPrivateDirectory(t.root); err != nil {
			return err
		}
		next := live.next(live.record.Core.Phase)
		next.MoveTails.Entered |= bit
		if err = live.publish(ctx, next); err != nil {
			return err
		}
	} else {
		// An entered unknown rename never authorizes another effect. Only its exact
		// already-published staged native/body can be factually completed.
		if err = validateAfter(t.name); err != nil {
			return errors.Join(errStorageEnrollmentUnresolved, err)
		}
		if _, e := t.root.Lstat(stage); !injectedArtifactOnlyNotExist(e) {
			return errors.Join(errStorageEnrollmentUnresolved, e)
		}
		next := live.next(live.record.Core.Phase)
		next.MoveTails.Done |= bit
		return live.publish(ctx, next)
	}
	if err = live.native(ctx, uint16(62+3*index), func() (err error) {
		if err = m.tailBefore(ctx, t); err != nil {
			return err
		}
		if err = validateAfter(stage); err != nil {
			return err
		}
		if err = m.phase.permit(ctx); err != nil {
			return err
		}
		if t.artifact != nil {
			if err = dialectArtifactOwnerStep(ctx, "move-location.before-cas"); err != nil {
				return err
			}
			if err = t.artifact.check(); err != nil {
				return err
			}
		}
		if err = t.root.Rename(stage, t.name); err != nil {
			return err
		}
		if err = syncPrivateDirectory(t.root); err != nil {
			return err
		}
		if t.artifact != nil {
			if err = dialectArtifactOwnerStep(ctx, "move-location.after-cas"); err != nil {
				return err
			}
		}
		return validateAfter(t.name)
	}); err != nil {
		return err
	}
	next := live.next(live.record.Core.Phase)
	next.MoveTails.Done |= bit
	return live.publish(ctx, next)
}

func (m *storageMoveOperation) currentQ(ctx context.Context, state storagePolicyOriginal, q storagebudget.EnrollmentInspection) error {
	p := m.phase
	if p == nil || p.move != m || p.original == nil || !bytes.Equal(state.Actual[0], p.original.Catalogs[0]) || !bytes.Equal(state.Actual[1], p.original.Catalogs[1]) {
		return storagebudget.ErrAuthority
	}
	digest, err := m.request.digest()
	if err != nil {
		return err
	}
	found := false
	for _, row := range state.Moves {
		if row.PendingIntent != (storagebudget.Digest{}) {
			if found || row.PlanID != m.request.Plan || row.PendingIntent != digest {
				return errStorageCustody
			}
			found = true
		}
	}
	if !found {
		return errStorageCustody
	}
	var selected *storageIdentityIntent
	for slot, pages := range p.original.Hosts {
		choice, e := storageSelectCreationHost(pages[0], pages[1], slot, state.Catalog.Installation, state.Catalog.QBinding)
		if e != nil {
			return e
		}
		if choice.Identity != nil && choice.Identity.Core.OperationID == m.request.Operation {
			if selected != nil {
				return errStorageCustody
			}
			copy := *choice.Identity
			selected = &copy
		}
	}
	if selected == nil || !m.request.identityBound(*selected, state.Lineage, m.before) {
		return errStorageCustody
	}
	if selected.Core.Phase < storageIntentStaging {
		return errStorageEnrollmentUnresolved
	}
	configuration := q.View.Configuration
	if configuration.Maintenance || !reflect.DeepEqual(storageOwnerDefinitionSnapshot(q.View), state.Effective) || !state.Boundary.valid() || configuration.Epoch != state.Boundary.Epoch || configuration.Revision < state.Boundary.MinimumRevision {
		return errStorageOwnerTransitionRequired
	}
	status := storageHostState(q.View)
	request, err := storageIdentityRequest(*selected, state.Catalog.QBinding)
	if err != nil {
		return err
	}
	if int(selected.Core.Token.Slot) >= len(status.Slots) {
		return storagebudget.ErrBounds
	}
	token := status.Slots[selected.Core.Token.Slot].Ticket.Token
	if err = storageOriginalActionRequest(ctx, p.c.ref.cell.q, selected.Core, request, status, token); err != nil {
		return err
	}
	for _, slot := range q.View.Slots {
		if slot.Ticket.State == storagebudget.Terminal && slot.Ticket.Token != slot.PersistedApplied && slot.Ticket.Token.ID != token.ID {
			return errStorageEnrollmentUnresolved
		}
	}
	return p.held(ctx)
}

func (m *storageMoveOperation) recoverTails(ctx context.Context, store *chatDataStore, j storageIdentityIntent) error {
	if m.phase == nil || m.phase.move != m || m.phase.identityPlan == nil || len(m.tails) != 0 || !m.request.tailBound(j.MoveTails, j.Core.Components) {
		return storagebudget.ErrAuthority
	}
	p := m.phase
	for i, v := range m.request.Tails {
		binding := p.c.owner.roots[v.Root]
		if binding == nil || binding.row.Generation != v.Generation {
			return errStorageCustody
		}
		root := store.root
		name := ""
		source, err := resolveConversationName(store.root, string(m.request.Previous))
		if err != nil {
			return err
		}
		switch v.Kind {
		case 1:
			root = p.c.original.config
			name = deskConfigName
			if !binding.configControl {
				return errStorageCustody
			}
		case 2:
			name = projectBindingsName
		case 3:
			name = strings.Replace(source, "conversations-", "assistant-source-approvals-", 1)
		case 4:
			name = source
		default:
			return errStorageCustody
		}
		a, err := root.Stat(".")
		if err != nil {
			return err
		}
		b, err := binding.root.Stat(".")
		if err != nil || !os.SameFile(a, b) {
			return errors.Join(errStorageCustody, err)
		}
		if v.Kind != 1 {
			selected := false
			for _, q := range p.c.sources.selections {
				selected = selected || q.location == store.location && q.path == p.c.server.configDir
			}
			member := false
			for _, id := range binding.references {
				member = member || id == p.identityPlan.Workspace.ID
			}
			if !selected || !member || binding.source == nil || binding.source.role != 4 || binding.source.path != store.location.Path || binding.category != storagebudget.History {
				return errStorageCustody
			}
		}
		raw, native, readErr := storageMoveReadPrivate(ctx, root, name, int(max(v.BeforeBytes, v.AfterBytes)))
		absent := injectedArtifactOnlyNotExist(readErr)
		if readErr != nil && !absent {
			return readErr
		}
		var before, after []byte
		if !absent && uint64(len(raw)) == v.AfterBytes && sha256.Sum256(raw) == v.AfterDigest {
			if j.MoveTails.Entered&(1<<i) == 0 || native != j.MoveTails.Staged[i] {
				return errStorageCustody
			}
			after = raw
		} else {
			if v.Kind == 1 && !absent && (v.Before == (storageOwnerPersistentObject{}) || native != v.Before || uint64(len(raw)) != v.BeforeBytes || sha256.Sum256(raw) != v.BeforeDigest) {
				// The settings were edited after the move's rename. Say so, in
				// words, instead of a bare custody refusal; recovery stays
				// unresolved until the person chooses (storageMoveSettingsConflict).
				return newStorageMoveSettingsConflict(filepath.Join(p.c.server.configDir, deskConfigName), raw, v, string(m.request.Destination))
			}
			if v.Before == (storageOwnerPersistentObject{}) {
				if !absent {
					return errStorageCustody
				}
			} else if absent || native != v.Before || uint64(len(raw)) != v.BeforeBytes || sha256.Sum256(raw) != v.BeforeDigest {
				return errStorageCustody
			}
			before = raw
			switch v.Kind {
			case 1:
				project, _ := json.Marshal(map[string]string{"file": filepath.Join(string(m.request.Destination), projectConfigName)})
				var problems []deskProblem
				after, problems = composeDeskFile(raw, !absent, nil, project, nil)
				if len(problems) != 0 || !validUTF8(after) || decodeDeskFile(after).refused() {
					return storagebudget.ErrBounds
				}
			case 2:
				bindings := projectBindings{Version: 1, Projects: map[string]string{}}
				if !absent {
					bindings, err = decodeProjectBindings(raw)
					if err != nil {
						return err
					}
				}
				bindings.Projects[string(m.request.Destination)] = source
				after, err = json.Marshal(bindings)
				if err != nil {
					return err
				}
			case 3:
				after, err = historicalAssistantSourceStorage(name, raw)
				if err != nil {
					return err
				}
			case 4:
				after = []byte(`{"version":1,"chats":[]}`)
			}
		}
		if uint64(len(after)) != v.AfterBytes || sha256.Sum256(after) != v.AfterDigest {
			return errStorageCustody
		}
		m.tails = append(m.tails, storageMoveTailPlan{value: v, binding: binding, root: root, name: name, before: bytes.Clone(before), after: bytes.Clone(after)})
	}
	m.store = store
	return p.held(ctx)
}

// This is an original startup/live host continuation. It never calls Reserve
// or Advance for a persisted J. Exact native absence/entered/terminal guards
// remain the original identity recovery relation.
func (s *Server) recoverOriginalStorageMove(ctx context.Context) (err error) {
	intent, found, err := readProjectMove(s.assistant.root)
	if err != nil || !found || intent.Storage == nil {
		return err
	}
	if s.storageConstruction == nil || s.storageConstruction.owner == nil {
		return errStorageCustody
	}
	aborted := false
	err = s.withDialectArtifactProjectMoveHostOwned(ctx, func(work *os.File, pins *dialectArtifactSelectedProviderPins, lease *storageCreatorLease) (err error) {
		if lease == nil || lease.move == nil {
			return storagebudget.ErrAuthority
		}
		m := lease.move
		p := m.phase

		// Compare J/Q while this exact registered move still holds original
		// use/project/work/domain exclusion. Callback/epoch cleanup still joins
		// before completion/admission can succeed; no expired startup ticket is
		// reused by New's later post-handoff acknowledgment pass.
		checkCompletion := func() error {
			return p.c.owner.withRegisteredMoveControl(ctx, m, func(ctx context.Context, table *storageIntentTable, original *storagePolicyOriginal) error {
				status, e := p.c.ref.cell.q.InspectHostState(ctx)
				if e != nil {
					return e
				}
				return p.c.owner.validateIntentMappingsOriginal(ctx, table, status, original)
			})
		}
		// A nil body result alone does not finish joined recovery. Arm this only
		// after exact original settlement and the matching active publication.
		// This defer runs after every callback-owned Close below; epoch finish
		// still owns and checks its remaining cleanup before opening admission.
		reconciled := false
		defer func() {
			if err != nil || !reconciled {
				return
			}
			if err = p.permit(ctx); err != nil {
				return
			}
			p.identity.finished, p.identityDone, p.reconciled = true, true, true
			p.markApplication(p.identityPlan.PlanID, storageIdentityIntentMove, true, true)
		}()
		m.request = *intent.Storage
		lock, err := m.privateDataLock(ctx, s)
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, lock.Close()) }()
		if err = m.borrowPrivate(ctx, lock); err != nil {
			return err
		}
		defer func() { err = errors.Join(err, m.closePrivate()) }()
		store, err := s.openChatData()
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, closeDialectArtifactOwner(ctx, "move-replay-data", store.root.Close)) }()
		state, err := p.c.owner.readOriginalPolicy(ctx)
		if err != nil {
			return err
		}
		m.original = state
		digest, err := m.request.digest()
		if err != nil {
			return err
		}
		selected := false
		for _, row := range state.Moves {
			if row.PlanID == m.request.Plan && (row.PendingIntent == digest || row.Current != nil && row.PendingIntent == (storagebudget.Digest{}) && row.Current.RequestDigest == digest) {
				selected = true
			}
		}
		if !selected {
			// The intent/J/Q prefix may have stopped before Pending. Neither an
			// absent row nor a missing ticket proves an unknown Reserve failed.
			// Preserve every operand/debt; no Reserve, Advance or removal here.
			return errStorageEnrollmentUnresolved
		}
		joined, err := p.c.readMoveBootstrap(ctx, state)
		if err != nil {
			return err
		}
		if joined.Current == nil {
			return errStorageEnrollmentUnresolved
		}
		if !joined.Pending {
			digest, e := m.request.digest()
			if e != nil || joined.Current.Current == nil || joined.Current.Current.RequestDigest != digest || joined.Current.Current.Receipt.OperationID != m.request.Operation {
				return errors.Join(errStorageCustody, e)
			}
			return checkCompletion() // committed receipt owes only nonallocating acknowledgment/removal
		}
		if joined.Current.Current != nil {
			v := *joined.Current
			v.PendingIntent = storagebudget.Digest{}
			m.before = &v
		}
		if err = p.captureIdentityOriginal(ctx); err != nil {
			return err
		}
		if err = p.captureOriginalIdentityPlan(ctx); err != nil {
			return err
		}
		if p.qualified == nil {
			if err = p.qualifyOriginalNative(ctx); err != nil {
				return err
			}
		}
		yes, err := p.joinOriginalIdentityAction(ctx)
		if err != nil || !yes {
			return errors.Join(errStorageEnrollmentUnresolved, err)
		}
		hold, err := s.holdDialectArtifactProjectMove(ctx, intent.Retained, work, pins, s.projectDir)
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, hold.Close()) }()
		if err = m.recoverTails(ctx, store, p.identity.record); err != nil {
			return err
		}
		if err = m.captureArtifact(ctx, intent.Retained, hold, true); err != nil {
			return err
		}
		if err = m.prepareLaunch(ctx); err != nil {
			return err
		}
		if err = p.identity.persistOriginalAction(ctx); err != nil {
			return err
		}
		if err = p.identity.persistOriginalQ(ctx); err != nil {
			return err
		}
		if s.projectDir == intent.Previous {
			if _, e := os.Lstat(intent.Destination); !injectedArtifactOnlyNotExist(e) {
				return errors.Join(errStorageCustody, e)
			}
			if p.identity.record.NativePhase != storageIdentityAllocatedObserved && p.identity.record.NativePhase != storageIdentityPayloadReady && p.identity.record.NativePhase != storageIdentityAbortPrepared && p.identity.record.NativePhase != storageIdentityAbortObserved {
				return errStorageEnrollmentUnresolved
			}
			if hold != nil && hold.location != nil && !bytes.Equal(hold.location.raw, intent.Retained.Before) {
				return errStorageCustody
			}
			if p.identity.record.NativePhase == storageIdentityAllocatedObserved || p.identity.record.NativePhase == storageIdentityPayloadReady {
				next := p.identity.next(storageIntentStaging)
				next.NativePhase = storageIdentityAbortPrepared
				if err = p.identity.publish(ctx, next); err != nil {
					return err
				}
			}
			if p.identity.record.NativePhase == storageIdentityAbortPrepared {
				if err = p.identity.completeOriginalAbort(ctx); err != nil {
					return err
				}
			}
			if err = p.identity.settle(ctx); err != nil {
				return err
			}
			rows := make([]storageMoveCurrent, 0, len(state.Moves))
			for _, v := range state.Moves {
				if v.PlanID != m.request.Plan {
					rows = append(rows, v)
				} else if m.before != nil {
					rows = append(rows, *m.before)
				}
			}
			if err = m.publishRows(ctx, rows, true); err != nil {
				return err
			}
			if err = checkCompletion(); err != nil {
				return err
			}
			reconciled = true
			aborted = true
			return nil
		}
		if s.projectDir != intent.Destination || !sameDirectory(intent.Destination, s.project.info) {
			return errStorageCustody
		}
		if err = m.publishArtifact(ctx, hold, intent.Destination); err != nil {
			return err
		}
		if err = m.complete(ctx); err != nil {
			return err
		}
		if err = hold.Close(); err != nil {
			return err
		}
		if err = checkCompletion(); err != nil {
			return err
		}
		reconciled = true
		return nil
	})
	if err != nil {
		return err
	}
	if aborted {
		raw, e := storageMoveEncodeIntent(intent)
		if e != nil {
			return e
		}
		return s.removeDialectArtifactMoveIntent(projectMoveName, raw)
	}
	return nil
}

// Startup may compare one already-admitted pending move before the writable
// registered recovery epoch begins. This supplies no permit: both actual banks,
// original source/bootstrap relation and the exact J/manifest must still join.
// Only this action's terminal may be unapplied; no other terminal is waived.
func (c *storageBudgetConstruction) moveStartupQ(ctx context.Context, state storagePolicyOriginal, q storagebudget.EnrollmentInspection) error {
	if !state.RecoveryOnly {
		return storagePolicyQAtBoundary(state.Effective, state.Boundary, q)
	}
	joined, err := c.readMoveBootstrap(ctx, state)
	if err != nil {
		return err
	}
	if !joined.Pending || joined.Request == nil || joined.Journal == nil {
		return errStorageEnrollmentUnresolved
	}
	j := *joined.Journal
	if j.Core.Phase < storageIntentStaging || j.Core.Phase == storageIntentRejected {
		return errStorageEnrollmentUnresolved
	}
	v := q.View.Configuration
	if v.Maintenance || !reflect.DeepEqual(storageOwnerDefinitionSnapshot(q.View), state.Effective) || !state.Boundary.valid() || v.Epoch != state.Boundary.Epoch || v.Revision < state.Boundary.MinimumRevision {
		return errStorageEnrollmentUnresolved
	}
	status := storageHostState(q.View)
	if int(j.Core.Token.Slot) >= len(status.Slots) {
		return storagebudget.ErrBounds
	}
	ticket := status.Slots[j.Core.Token.Slot].Ticket
	if !storageIdentityIntentTicketMapping(j, ticket, state.Catalog.QBinding) {
		return errStorageEnrollmentUnresolved
	}
	request, err := storageIdentityRequest(j, state.Catalog.QBinding)
	if err != nil {
		return err
	}
	if err = storageOriginalActionRequest(ctx, c.ref.cell.q, j.Core, request, status, ticket.Token); err != nil {
		return err
	}
	for _, row := range q.View.Slots {
		if row.Ticket.State == storagebudget.Terminal && row.Ticket.Token != row.PersistedApplied && row.Ticket.Token != ticket.Token {
			return errStorageEnrollmentUnresolved
		}
	}
	return nil
}

// A committed latest row is original retained evidence after its J slot has
// legitimately been reused. The persisted-applied tuple must still cover the
// receipt. If its ticket remains present, compare its exact original digest.
func storageMoveAppliedCurrent(row storageMoveCurrent, installation storagebudget.ID, binding storagebudget.Digest, status storagebudget.HostState) bool {
	if _, err := storageMoveEncodeCurrent(row); err != nil {
		return false
	}
	if row.Current == nil || row.PendingIntent != (storagebudget.Digest{}) {
		return false
	}
	receipt := row.Current.Receipt
	if !receipt.valid(2) || receipt.RetirementEpoch > status.Epoch || int(receipt.Terminal.Slot) >= len(status.Slots) {
		return false
	}
	slot := status.Slots[receipt.Terminal.Slot]
	if slot.PersistedApplied == receipt.Terminal {
		if slot.PersistedNextSequence <= receipt.Terminal.Sequence {
			return false
		}
	} else if slot.PersistedApplied.ID == (storagebudget.ID{}) || slot.PersistedApplied.Generation == 0 || slot.PersistedApplied.Sequence <= receipt.Terminal.Sequence || slot.PersistedNextSequence <= slot.PersistedApplied.Sequence {
		return false
	}
	if slot.Ticket.Token == receipt.Terminal {
		digest, err := storageOwnerV4TicketDigest(installation, binding, slot.Ticket)
		if err != nil || slot.Ticket.State != storagebudget.Terminal || digest != receipt.TicketDigest {
			return false
		}
	}
	return true
}

// No new allocation, reservation, settlement or current-row publication occurs
// here. These existing sync/readback/ack/removal operations are the provisioned
// tail of the original move. Removal follows every original outer Close.
func (s *Server) finishOriginalStorageMove(ctx context.Context, intent projectMoveIntent) (err error) {
	if intent.Storage == nil || s.storageConstruction == nil || s.storageConstruction.owner == nil || s.locationStatus().Problem != "" || s.startupShared() || s.startupUnresolved() {
		return errStorageEnrollmentUnresolved
	}
	err = s.withDialectArtifactProjectMoveHostOwned(ctx, func(work *os.File, pins *dialectArtifactSelectedProviderPins, lease *storageCreatorLease) (err error) {
		if lease == nil || lease.move == nil {
			return storagebudget.ErrAuthority
		}
		m := lease.move
		m.request = *intent.Storage
		lock, err := m.privateDataLock(ctx, s)
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, lock.Close()) }()
		if err = m.borrowPrivate(ctx, lock); err != nil {
			return err
		}
		defer func() { err = errors.Join(err, m.closePrivate()) }()
		state, err := m.phase.c.owner.readOriginalPolicy(ctx)
		if err != nil {
			return err
		}
		joined, err := m.phase.c.readMoveBootstrap(ctx, state)
		if err != nil {
			return err
		}
		digest, err := m.request.digest()
		if err != nil || joined.Pending || joined.Current == nil || joined.Current.Current == nil || joined.Current.Current.RequestDigest != digest || joined.Current.Current.Receipt.OperationID != m.request.Operation {
			return errors.Join(errStorageCustody, err)
		}
		status, err := m.phase.c.ref.cell.q.InspectHostState(ctx)
		if err != nil {
			return err
		}
		if !storageMoveAppliedCurrent(*joined.Current, state.Catalog.Installation, state.Catalog.QBinding, status) {
			return errStorageEnrollmentUnresolved
		}
		if !m.request.artifactPlanBound(intent.Retained) {
			return errStorageCustody
		}
		if intent.Retained != nil {
			data, e := s.openChatData()
			if e != nil {
				return e
			}
			defer func() { err = errors.Join(err, closeDialectArtifactOwner(ctx, "move-finish-data", data.root.Close)) }()
			namespace, e := assistantRunNamespace(data.root, intent.Previous)
			if e != nil || namespace != intent.Retained.Namespace {
				return errors.Join(errDialectArtifactOwner, e)
			}
			hold, e := s.holdDialectArtifactProjectMove(ctx, intent.Retained, work, pins, intent.Destination)
			if e != nil {
				return e
			}
			defer func() { err = errors.Join(err, hold.Close()) }()
			if e = hold.publish(intent.Destination); e != nil {
				return e
			}
			if e = hold.Close(); e != nil {
				return e
			}
		}
		private, err := s.openIdentityFolder()
		if err != nil {
			return err
		}
		record, _, ok, readErr := readIdentity(private)
		if readErr == nil && (!ok || !bytes.Equal(record.line(), joined.Current.Current.Body) || record.ID != intent.Identity || record.Folder != intent.Folder || record.Path != intent.Destination) {
			readErr = errStorageCustody
		}
		if readErr == nil {
			readErr = s.acknowledgeDialectArtifactMoveIdentity(private, record)
		}
		closeErr := closeDialectArtifactOwner(ctx, "move-finish-identity", private.Close)
		if readErr != nil || closeErr != nil {
			return errors.Join(readErr, closeErr)
		}
		return s.finishProjectMoveConfig(intent)
	})
	if err != nil {
		return err
	}
	raw, err := storageMoveEncodeIntent(intent)
	if err != nil {
		return err
	}
	return s.removeDialectArtifactMoveIntent(projectMoveName, raw)
}

// Optional original locator data is already retained verbatim in the existing
// intent. Bind those bytes once; keep only its original root/parent/native and
// lengths here. Injected providers have no local locator write or invented row.
type storageMoveArtifactRequest struct {
	Plan                    storagebudget.Digest
	Root                    storagebudget.ID
	Parent, Before          storageOwnerPersistentObject
	BeforeBytes, AfterBytes uint64
}

func storageMoveEncodeArtifact(a *storageMoveArtifactRequest) ([]byte, error) {
	if a == nil {
		return []byte{0}, nil
	}
	if a.Plan == (storagebudget.Digest{}) {
		return nil, storagebudget.ErrBounds
	}
	if a.Root == (storagebudget.ID{}) {
		if a.Parent != (storageOwnerPersistentObject{}) || a.Before != (storageOwnerPersistentObject{}) || a.BeforeBytes != 0 || a.AfterBytes != 0 {
			return nil, storagebudget.ErrBounds
		}
		return append([]byte{2}, a.Plan[:]...), nil
	}
	if !a.Parent.valid(1) || !a.Before.valid(2) || !storageIdentitySameVolume(a.Parent, a.Before) || a.BeforeBytes == 0 || a.AfterBytes == 0 || a.BeforeBytes > dialectArtifactLocationRecordBytes || a.AfterBytes > dialectArtifactLocationRecordBytes {
		return nil, storagebudget.ErrBounds
	}
	out := append([]byte{1}, a.Plan[:]...)
	out = append(out, a.Root[:]...)
	out = append(out, storageEnrollmentPeakNativeBytes(a.Parent)...)
	out = append(out, storageEnrollmentPeakNativeBytes(a.Before)...)
	out = binary.LittleEndian.AppendUint64(out, a.BeforeBytes)
	out = binary.LittleEndian.AppendUint64(out, a.AfterBytes)
	return out, nil
}
func storageMoveReadArtifact(r *storageEnrollmentPeakReader) *storageMoveArtifactRequest {
	tag := r.take(1)
	if len(tag) != 1 {
		return nil
	}
	if tag[0] == 0 {
		return nil
	}
	a := &storageMoveArtifactRequest{Plan: r.digest()}
	if tag[0] == 1 {
		a.Root, a.Parent, a.Before = r.id(), r.native(), r.native()
		a.BeforeBytes, a.AfterBytes = r.u64(), r.u64()
	} else if tag[0] != 2 {
		r.bad = true
	}
	if _, err := storageMoveEncodeArtifact(a); err != nil {
		r.bad = true
	}
	return a
}
func storageMoveArtifactPlan(p *dialectArtifactProjectMovePlan) (storagebudget.Digest, error) {
	if p == nil || !p.valid() {
		return storagebudget.Digest{}, errDialectArtifactOwner
	}
	raw, err := json.Marshal(p)
	if err != nil || len(raw) > dialectArtifactProjectMovePlanBytes {
		return storagebudget.Digest{}, errors.Join(errDialectArtifactOwner, err)
	}
	return sha256.Sum256(raw), nil
}
func (r storageMoveRequest) artifactPlanBound(p *dialectArtifactProjectMovePlan) bool {
	if r.Artifact == nil {
		return p == nil
	}
	digest, err := storageMoveArtifactPlan(p)
	return err == nil && digest == r.Artifact.Plan && ((p.Kind == "injected" && r.Artifact.Root == (storagebudget.ID{})) || (p.Kind == "file" && r.Artifact.Root != (storagebudget.ID{}) && uint64(len(p.Before)) == r.Artifact.BeforeBytes && uint64(len(p.After)) == r.Artifact.AfterBytes))
}
func (r storageMoveRequest) tailCommit() (storagebudget.Digest, error) {
	raw, err := storageMoveEncodeTails(r.Tails)
	if err != nil {
		return storagebudget.Digest{}, err
	}
	if r.Artifact != nil {
		a, e := storageMoveEncodeArtifact(r.Artifact)
		if e != nil {
			return storagebudget.Digest{}, e
		}
		raw = append(raw, a...)
	}
	return sha256.Sum256(raw), nil
}
func (r storageMoveRequest) tailBound(j storageMoveTailJournal, components []storagebudget.Component) bool {
	count := len(r.Tails)
	if r.Artifact != nil && r.Artifact.Root != (storagebudget.ID{}) {
		count++
	}
	if count == 0 {
		return j == (storageMoveTailJournal{})
	}
	digest, err := r.tailCommit()
	if err != nil || j.Count != byte(count) || j.Plan != digest {
		return false
	}
	fixed := j
	fixed.Count = byte(len(r.Tails))
	raw, e := storageMoveEncodeTails(r.Tails)
	if e != nil {
		return false
	}
	fixed.Plan = sha256.Sum256(raw)
	if len(r.Tails) > 0 && !storageMoveTailRequestBound(r.Tails, fixed, components) {
		return false
	}
	if count > len(r.Tails) {
		i := len(r.Tails)
		if int(j.Component[i]) >= len(components) {
			return false
		}
		c := components[j.Component[i]]
		return c.Root == r.Artifact.Root && c.RootGeneration > 0 && j.BeforeExists[i] && j.BeforeBytes[i] == r.Artifact.BeforeBytes && j.AfterBytes[i] == r.Artifact.AfterBytes
	}
	return true
}
func (m *storageMoveOperation) captureArtifact(ctx context.Context, plan *dialectArtifactProjectMovePlan, hold *dialectArtifactProjectMoveHold, recovery bool) error {
	if plan == nil {
		if hold != nil || m.request.Artifact != nil {
			return errStorageCustody
		}
		return nil
	}
	if hold == nil || hold.plan == nil || hold.metadata == nil || hold.s != m.phase.c.server {
		return errStorageCustody
	}
	if err := hold.check(); err != nil {
		return err
	}
	digest, err := storageMoveArtifactPlan(plan)
	if err != nil {
		return err
	}
	if plan.Kind == "injected" {
		if recovery {
			if !m.request.artifactPlanBound(plan) {
				return errStorageCustody
			}
		} else {
			m.request.Artifact = &storageMoveArtifactRequest{Plan: digest}
		}
		return nil
	}
	if hold.rows == nil || hold.location == nil {
		return errStorageCustody
	}
	var binding *storageRootBinding
	actualConfig, err := storageOwnerPersistentRoot(ctx, m.phase.c.original.config)
	if err != nil {
		return err
	}
	heldConfig, err := storageOwnerPersistentRoot(ctx, hold.s.assistant.root)
	if err != nil || heldConfig != actualConfig {
		return errors.Join(errStorageCustody, err)
	}
	for _, r := range m.phase.originalRoots {
		if r.configControl {
			if binding != nil {
				return errStorageCustody
			}
			binding = r
		}
	}
	if binding == nil || binding.root != m.phase.c.original.config {
		return errStorageCustody
	}
	parent, err := storageOwnerPersistentRoot(ctx, hold.rows)
	if err != nil {
		return err
	}
	name := plan.Owner[2:] + ".json"
	raw, native, err := storageMoveReadPrivate(ctx, hold.rows, name, dialectArtifactLocationRecordBytes)
	if err != nil {
		return err
	}
	a := &storageMoveArtifactRequest{Plan: digest, Root: binding.row.ID, Parent: parent, Before: native, BeforeBytes: uint64(len(plan.Before)), AfterBytes: uint64(len(plan.After))}
	if recovery {
		if !m.request.artifactPlanBound(plan) || m.request.Artifact.Root != a.Root || m.request.Artifact.Parent != parent {
			return errStorageCustody
		}
		a = m.request.Artifact
		if bytes.Equal(raw, plan.Before) {
			if native != a.Before {
				return errStorageCustody
			}
		} else if bytes.Equal(raw, plan.After) {
			index := len(m.request.Tails)
			j := m.phase.identity.record.MoveTails
			if index >= int(j.Count) || j.Entered&(1<<index) == 0 || native != j.Staged[index] {
				return errStorageEnrollmentUnresolved
			}
		} else {
			return errStorageCustody
		}
	} else {
		if !bytes.Equal(raw, plan.Before) {
			return errStorageCustody
		}
		m.request.Artifact = a
	}
	value := storageMoveTail{Kind: 5, Root: a.Root, Generation: binding.row.Generation, Before: a.Before, BeforeDigest: sha256.Sum256(plan.Before), AfterDigest: sha256.Sum256(plan.After), BeforeBytes: a.BeforeBytes, AfterBytes: a.AfterBytes}
	if !value.valid() {
		return storagebudget.ErrBounds
	}
	m.tails = append(m.tails, storageMoveTailPlan{value: value, binding: binding, root: hold.rows, name: name, before: bytes.Clone(plan.Before), after: bytes.Clone(plan.After), artifact: hold})
	return nil
}
func (m *storageMoveOperation) publishArtifact(ctx context.Context, hold *dialectArtifactProjectMoveHold, destination string) error {
	if hold == nil {
		if m.request.Artifact != nil {
			return errStorageCustody
		}
		return nil
	}
	if !m.request.artifactPlanBound(hold.plan) {
		return errStorageCustody
	}
	if hold.plan.Kind == "injected" {
		return hold.publish(destination)
	}
	index := len(m.request.Tails)
	if index >= len(m.tails) || m.tails[index].artifact != hold {
		return storagebudget.ErrAuthority
	}
	hold.actualPath = destination
	if err := m.writeTail(ctx, index, m.tails[index]); err != nil {
		return err
	}
	raw, _, err := storageMoveReadPrivate(ctx, hold.rows, m.tails[index].name, dialectArtifactLocationRecordBytes)
	if err != nil || !bytes.Equal(raw, hold.plan.After) {
		return errors.Join(errStorageCustody, err)
	}
	info, err := hold.rows.Lstat(m.tails[index].name)
	if err != nil {
		return err
	}
	hold.location.raw, hold.location.info = bytes.Clone(raw), info
	return hold.check()
}

// Only a genuinely joined original move may discharge its already admitted
// promises above a later reduced logical policy. No caller can choose this
// branch, and it never excuses fresh admission, stale custody, native bounds or
// physical headroom. The original exact J/request/Q relation remains mandatory.
func (p *storageEnrollmentEpoch) originalMoveOwed(ctx context.Context) (bool, error) {
	m := p.identity
	if m == nil || !m.recovering || m.record.Action != storageIdentityIntentMove {
		return false, nil
	}
	if p.move == nil || p.move.self != p.move || p.move.phase != p || m.self != m || m.phase != p || p.identityPlan == nil || p.original == nil || m.record.Core.Phase < storageIntentStaging {
		return false, storagebudget.ErrAuthority
	}
	if !p.move.request.identityBound(m.record, p.move.original.Lineage, p.move.before) {
		return false, errStorageCustody
	}
	want, err := storageIdentityRequest(m.record, p.c.ref.cell.binding)
	if err != nil || !reflect.DeepEqual(want, m.request) {
		return false, errors.Join(errStorageCustody, err)
	}
	if err = m.hostCurrent(); err != nil {
		return false, err
	}
	status, err := p.c.ref.cell.q.InspectHostState(ctx)
	if err != nil {
		return false, err
	}
	if int(m.admitted.Slot) >= len(status.Slots) {
		return false, storagebudget.ErrBounds
	}
	ticket := status.Slots[m.admitted.Slot].Ticket
	if ticket.Token != m.admitted || !storageIdentityIntentTicketMapping(m.record, ticket, m.request.Owner) {
		return false, errStorageEnrollmentUnresolved
	}
	if err = storageOriginalActionRequest(ctx, p.c.ref.cell.q, m.record.Core, m.request, status, ticket.Token); err != nil {
		return false, err
	}
	return true, nil
}

// Match only the original P's private boundary (or its one already registered
// retained child), after strict selected move bootstrap has revalidated current
// identity and native roots. Immutable source paths/definitions are not edited.
func storageMovePrivateSourceMatches(joined storageMoveBootstrap, catalog storageOwnerCatalogV2, row storageOwnerRoot, source *storageOriginalSourceRoot, project *os.Root, currentPath string) bool {
	if joined.Current == nil || source == nil || project == nil || source.root == nil || int(row.DirectoryObject) >= len(catalog.Objects) || source.persistent != catalog.Objects[row.DirectoryObject].Persistent || source.role != row.Role || source.category != row.Category || source.key != string(row.Source.OwnerKey) {
		return false
	}
	p := joined.Plan
	var original *storageOwnerRoot
	for i := range catalog.Roots {
		if catalog.Roots[i].Definition.ID == p.PrivateRoot.ID {
			if original != nil {
				return false
			}
			original = &catalog.Roots[i]
		}
	}
	if original == nil || original.Role != 6 || original.Category != storagebudget.Control || original.Source.Kind != 6 || original.Definition.Generation != p.PrivateRoot.Generation || original.Definition.Identity != p.PrivateRoot.Identity || int(original.DirectoryObject) >= len(catalog.Objects) || catalog.Objects[original.DirectoryObject].Persistent != p.PrivateObject {
		return false
	}
	digest, err := catalog.sourceDigest(*original)
	if err != nil || digest != p.PrivateRoot.SourceDigest {
		return false
	}
	identity, err := parseIdentity(p.IdentityInitial)
	if err != nil || !storageOwnerAbsolute([]byte(currentPath)) || string(original.Source.Path) != filepath.Join(identity.Path, startupIdentityDir) || !bytes.Equal(original.Source.OwnerKey, p.AccountingOwnerKey) || original.Definition.Workspace != p.Workspace.ID || len(original.References) != 1 || original.References[0] != p.Workspace.ID {
		return false
	}
	if joined.Pending {
		if joined.Request == nil || joined.Journal == nil || !bytes.Equal([]byte(currentPath), joined.Request.Previous) && !bytes.Equal([]byte(currentPath), joined.Request.Destination) {
			return false
		}
	} else {
		if joined.Current.Current == nil {
			return false
		}
		now, e := parseIdentity(joined.Current.Current.Body)
		if e != nil || now.Path != currentPath {
			return false
		}
	}
	if len(source.witnesses) == 0 {
		return false
	}
	edge := source.witnesses[len(source.witnesses)-1]
	if edge.root != source.root {
		return false
	}
	if row.Definition.ID == original.Definition.ID {
		return reflect.DeepEqual(row, *original) && source.persistent == p.PrivateObject && source.parent == nil && source.path == filepath.Join(currentPath, startupIdentityDir) && edge.parent == project && edge.name == startupIdentityDir
	}
	// This fixed child already has its own admitted row. Neither arbitrary
	// descendants nor provider roots inherit the original P's move relation.
	return row.Role == 6 && row.Category == storagebudget.Retained && row.Source.Kind == 6 && row.Source.ParentRoot == original.Definition.ID && string(row.Source.Relative) == "retained-artifacts" && string(row.Source.Path) == filepath.Join(string(original.Source.Path), "retained-artifacts") && row.Definition.Workspace == p.Workspace.ID && len(row.References) == 1 && row.References[0] == p.Workspace.ID && bytes.Equal(row.Source.OwnerKey, p.AccountingOwnerKey) && source.parent != nil && source.parent.persistent == p.PrivateObject && source.parent.path == filepath.Join(currentPath, startupIdentityDir) && source.path == filepath.Join(source.parent.path, "retained-artifacts") && edge.parent == source.parent.root && edge.name == "retained-artifacts"
}
func (x *storageOriginalSources) movePrivateSourceCurrent(ctx context.Context, catalog storageOwnerCatalogV2, row storageOwnerRoot, source *storageOriginalSourceRoot) (bool, error) {
	c := x.construction
	if c == nil || c.owner == nil || c.project == nil {
		return false, nil
	}
	c.mu.Lock()
	moved := c.moveHistory
	c.mu.Unlock()
	if !moved {
		return false, nil
	}
	state, err := c.owner.readOriginalPolicy(ctx)
	if err != nil {
		return false, err
	}
	if !reflect.DeepEqual(state.Catalog, catalog) {
		return false, errStorageCustody
	}
	joined, err := c.readMoveBootstrap(ctx, state)
	if err != nil {
		return false, err
	}
	if !storageMovePrivateSourceMatches(joined, catalog, row, source, c.project.own.root, c.server.projectDir) {
		return false, nil
	}
	// Bootstrap compares the actual selected physical pair, current identity and
	// original project/private tuple; the source owns its existing named witness.
	if err = source.current(ctx, c); err != nil {
		return false, err
	}
	return true, nil
}
