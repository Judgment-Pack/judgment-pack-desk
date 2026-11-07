package desk

import (
	"bytes"
	"crypto/sha256"
	"reflect"
	"strings"
	"sync"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Pure operands belong to one synchronous original epoch. They contain no
// descriptors, observations, comparison verdicts, grants or completion state.
// Every reuse requires complete bytes and the complete interpretation context.
// Fixed packet/bank counts and a conservative 256 MiB retained-data ceiling
// bound even maximum evidence graphs. An oversized entry simply stays strict.
type storageEnrollmentOperands struct {
	entries [216]storageEnrollmentOperand
	next    uint64
	bytes   uint64
}
type storageEnrollmentOperand struct {
	kind    uint8
	age     uint64
	raw     []byte
	context any
	value   any
	bytes   uint64
}

const (
	storageEnrollmentOperandEvidence uint8 = iota + 1
	storageEnrollmentOperandEnvelope
	storageEnrollmentOperandPeer
	storageEnrollmentOperandCheckpoint
	storageEnrollmentOperandTicket
	storageEnrollmentOperandCatalog
	storageEnrollmentOperandComposition
	storageEnrollmentOperandProjection
	storageEnrollmentOperandComposedPair
	storageEnrollmentOperandLimit = 256 << 20
)

var storageEnrollmentOperandCounts = [10]int{0, 6, 4, 1, 6, 128, 2, 1, 66, 2}

// Clone mutable data recursively; fixed value arrays use ordinary value copies.
// Byte slices use one copy, never a per-byte reflection loop. Strings and
// scalar fields are immutable. On retention, strings are also detached from
// potentially larger caller backing allocations. Hits may share those privately
// retained immutable strings. This isolates both returned planner/comparison
// objects and nested retained lineage from every caller-owned buffer.
func storageEnrollmentOperandMutable(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Interface:
		return true
	case reflect.Array:
		return storageEnrollmentOperandMutable(t.Elem())
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			if storageEnrollmentOperandMutable(t.Field(i).Type) {
				return true
			}
		}
	}
	return false
}

// Pure facts about a static type, computed once per type. They depend on the
// type alone, never on a value, owner, epoch or caller.
type storageEnrollmentOperandTypeFacts struct{ mutable, dynamic bool }

var storageEnrollmentOperandTypes sync.Map // reflect.Type -> storageEnrollmentOperandTypeFacts

func storageEnrollmentOperandDetaches(t reflect.Type, freezeStrings bool) bool {
	facts, ok := storageEnrollmentOperandTypes.Load(t)
	if !ok {
		facts, _ = storageEnrollmentOperandTypes.LoadOrStore(t, storageEnrollmentOperandTypeFacts{storageEnrollmentOperandMutable(t), storageEnrollmentOperandDynamic(t)})
	}
	f := facts.(storageEnrollmentOperandTypeFacts)
	return f.mutable || freezeStrings && f.dynamic
}

var storageEnrollmentOperandByte = reflect.TypeFor[byte]()

func storageEnrollmentOperandCloneValue(v reflect.Value, freezeStrings bool) reflect.Value {
	out := reflect.New(v.Type()).Elem()
	out.Set(v)
	storageEnrollmentOperandDetach(out, freezeStrings)
	return out
}

// Detach replaces, in place, every mutable reference reachable from the
// addressable out, which already holds a shallow copy of the source value.
// Each pointer target and slice backing array is allocated and copied once,
// and a byte copy is not zero-filled first: every byte is overwritten by the
// copy. The result is the same complete deep copy as before.
func storageEnrollmentOperandDetach(out reflect.Value, freezeStrings bool) {
	switch out.Kind() {
	case reflect.String:
		if freezeStrings {
			out.SetString(strings.Clone(out.String()))
		}
	case reflect.Pointer:
		if !out.IsNil() {
			target := reflect.New(out.Type().Elem())
			target.Elem().Set(out.Elem())
			if storageEnrollmentOperandDetaches(out.Type().Elem(), freezeStrings) {
				storageEnrollmentOperandDetach(target.Elem(), freezeStrings)
			}
			out.Set(target)
		}
	case reflect.Interface:
		if !out.IsNil() {
			out.Set(storageEnrollmentOperandCloneValue(out.Elem(), freezeStrings))
		}
	case reflect.Slice:
		if out.IsNil() {
			return
		}
		n := out.Len()
		if out.Type().Elem() == storageEnrollmentOperandByte && n > 0 {
			b := append([]byte(nil), out.Bytes()...)
			out.Set(reflect.ValueOf(b[:n:n]).Convert(out.Type()))
			return
		}
		copied := reflect.MakeSlice(out.Type(), n, n)
		reflect.Copy(copied, out)
		if storageEnrollmentOperandDetaches(out.Type().Elem(), freezeStrings) {
			for i := 0; i < n; i++ {
				storageEnrollmentOperandDetach(copied.Index(i), freezeStrings)
			}
		}
		out.Set(copied)
	case reflect.Array:
		if storageEnrollmentOperandDetaches(out.Type().Elem(), freezeStrings) {
			for i := 0; i < out.Len(); i++ {
				storageEnrollmentOperandDetach(out.Index(i), freezeStrings)
			}
		}
	case reflect.Struct:
		for i := 0; i < out.NumField(); i++ {
			if storageEnrollmentOperandDetaches(out.Type().Field(i).Type, freezeStrings) {
				storageEnrollmentOperandDetach(out.Field(i), freezeStrings)
			}
		}
	case reflect.Map:
		if !out.IsNil() {
			copied := reflect.MakeMapWithSize(out.Type(), out.Len())
			iter := out.MapRange()
			for iter.Next() {
				copied.SetMapIndex(storageEnrollmentOperandCloneValue(iter.Key(), freezeStrings), storageEnrollmentOperandCloneValue(iter.Value(), freezeStrings))
			}
			out.Set(copied)
		}
	}
}
func storageEnrollmentOperandClone[T any](v T) T {
	return storageEnrollmentOperandCloneValue(reflect.ValueOf(&v).Elem(), false).Interface().(T)
}
func storageEnrollmentOperandFreeze[T any](v T) T {
	return storageEnrollmentOperandCloneValue(reflect.ValueOf(&v).Elem(), true).Interface().(T)
}

// Count each reachable allocation separately, including aliased slices. This
// overestimates retained storage; cloning never retains spare slice capacity.
func storageEnrollmentOperandDynamic(t reflect.Type) bool {
	if t.Kind() == reflect.String || storageEnrollmentOperandMutable(t) {
		return true
	}
	if t.Kind() == reflect.Array {
		return storageEnrollmentOperandDynamic(t.Elem())
	}
	if t.Kind() == reflect.Struct {
		for i := 0; i < t.NumField(); i++ {
			if storageEnrollmentOperandDynamic(t.Field(i).Type) {
				return true
			}
		}
	}
	return false
}
func storageEnrollmentOperandSize(v reflect.Value) uint64 {
	n := uint64(v.Type().Size())
	switch v.Kind() {
	case reflect.String:
		n += uint64(v.Len())
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			n += storageEnrollmentOperandSize(v.Elem())
		}
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice {
			n += uint64(v.Len()) * uint64(v.Type().Elem().Size())
		}
		if storageEnrollmentOperandDynamic(v.Type().Elem()) {
			for i := 0; i < v.Len(); i++ {
				n += storageEnrollmentOperandSize(v.Index(i))
			}
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			n += storageEnrollmentOperandSize(v.Field(i))
		}
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			n += storageEnrollmentOperandSize(iter.Key()) + storageEnrollmentOperandSize(iter.Value()) + 64
		}
	}
	return n
}

func storageEnrollmentOperandGet[T any](o *storageEnrollmentOperands, kind uint8, raw []byte, context any, decode func([]byte) (T, error)) (T, error) {
	if o == nil {
		return decode(raw)
	}
	for i := range o.entries {
		e := &o.entries[i]
		if e.kind == kind && bytes.Equal(e.raw, raw) && reflect.DeepEqual(e.context, context) {
			return storageEnrollmentOperandClone(e.value.(T)), nil
		}
	}
	// Do not copy invalid/unbounded input before the strict parser's own bounds.
	if len(raw) > storageEnrollmentPeakEvidenceMax {
		return decode(raw)
	}
	value, err := decode(append([]byte(nil), raw...))
	if err != nil {
		return value, err
	}
	size := uint64(len(raw)) + storageEnrollmentOperandSize(reflect.ValueOf(value)) + storageEnrollmentOperandSize(reflect.ValueOf(context))
	if size > storageEnrollmentOperandLimit {
		return value, nil
	}
	if o.next == ^uint64(0) {
		*o = storageEnrollmentOperands{}
	}
	count, empty, oldest := 0, -1, -1
	for i := range o.entries {
		e := &o.entries[i]
		if e.kind == 0 {
			empty = i
		}
		if e.kind == kind {
			count++
			if oldest < 0 || e.age < o.entries[oldest].age {
				oldest = i
			}
		}
	}
	if count >= storageEnrollmentOperandCounts[kind] {
		empty = oldest
		o.bytes -= o.entries[empty].bytes
		o.entries[empty] = storageEnrollmentOperand{}
	}
	for o.bytes+size > storageEnrollmentOperandLimit || empty < 0 {
		oldest = -1
		for i := range o.entries {
			e := &o.entries[i]
			if e.kind != 0 && (oldest < 0 || e.age < o.entries[oldest].age) {
				oldest = i
			}
		}
		o.bytes -= o.entries[oldest].bytes
		o.entries[oldest] = storageEnrollmentOperand{}
		empty = oldest
	}
	o.bytes -= o.entries[empty].bytes
	o.next++
	// The decoder input may also appear in its returned sections. Neither it
	// nor the returned model is the private key or retained model.
	o.entries[empty] = storageEnrollmentOperand{kind: kind, age: o.next, raw: append([]byte(nil), raw...), context: storageEnrollmentOperandFreeze(context), value: storageEnrollmentOperandFreeze(value), bytes: size}
	o.bytes += size
	return value, nil
}

// View reads a pointer-free projection of the exact retained (or freshly and
// strictly decoded) model without cloning that model. The view function only
// reads its argument, and V holds no pointer, slice, map or interface, so the
// result shares nothing with the retained model. Keys, retention, bounds and
// strict fallback are exactly those of storageEnrollmentOperandGet.
func storageEnrollmentOperandView[T, V any](o *storageEnrollmentOperands, kind uint8, raw []byte, context any, decode func([]byte) (T, error), view func(T) V) (V, error) {
	var zero V
	if storageEnrollmentOperandMutable(reflect.TypeFor[V]()) {
		panic("storage enrollment operand view can alias its model")
	}
	if o != nil {
		for i := range o.entries {
			e := &o.entries[i]
			if e.kind == kind && bytes.Equal(e.raw, raw) && reflect.DeepEqual(e.context, context) {
				return view(e.value.(T)), nil
			}
		}
	}
	value, err := storageEnrollmentOperandGet(o, kind, raw, context, decode)
	if err != nil {
		return zero, err
	}
	return view(value), nil
}

type storageEnrollmentEvidenceContext struct {
	Record  storageOwnerRecordV2
	Claim   storageOwnerClaimV2
	Lineage *storageCompositionLineage
}

func (o *storageEnrollmentOperands) evidence(raw []byte, lineage *storageCompositionLineage, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageEnrollmentPeakEvidence, error) {
	context := storageEnrollmentEvidenceContext{record, claim, lineage}
	return storageEnrollmentOperandGet(o, storageEnrollmentOperandEvidence, raw, context, func(held []byte) (storageEnrollmentPeakEvidence, error) {
		if o != nil {
			lineage = storageEnrollmentOperandFreeze(lineage)
		}
		return storageEnrollmentPeakDecodeEvidenceOperands(held, lineage, record, claim, o)
	})
}
func (o *storageEnrollmentOperands) envelope(raw []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2, validate, composition bool) (storageEnrollmentPeakEnvelope, error) {
	context := struct {
		Record                storageOwnerRecordV2
		Claim                 storageOwnerClaimV2
		Validate, Composition bool
	}{record, claim, validate, composition}
	return storageEnrollmentOperandGet(o, storageEnrollmentOperandEnvelope, raw, context, func(held []byte) (storageEnrollmentPeakEnvelope, error) {
		return storageEnrollmentPeakDecodeEnvelopeVariant(held, record, claim, validate, composition)
	})
}

type storageEnrollmentCheckpointOperand struct {
	Value  *storageEnrollmentPeakQCheckpoint
	Digest storagebudget.Digest
}

func storageEnrollmentCheckpointDecoder(anchor storageEnrollmentPeakQHeldAnchor) func([]byte) (storageEnrollmentCheckpointOperand, error) {
	return func(held []byte) (storageEnrollmentCheckpointOperand, error) {
		q, err := storageEnrollmentPeakQDecodeCheckpoint(append([]byte(nil), held...), anchor)
		if err != nil {
			return storageEnrollmentCheckpointOperand{}, err
		}
		return storageEnrollmentCheckpointOperand{q, sha256.Sum256(held)}, nil
	}
}

// The pair/transition facts that bank comparisons read from a strictly
// decoded checkpoint page, without copying its 2.5 MiB model.
type storageEnrollmentCheckpointFacts struct {
	Generation uint64
	Applied    [storageEnrollmentPeakQSlots]storageEnrollmentPeakQApplied
	Digest     storagebudget.Digest
}

func (o *storageEnrollmentOperands) checkpointFacts(raw []byte, anchor storageEnrollmentPeakQHeldAnchor) (storageEnrollmentCheckpointFacts, error) {
	return storageEnrollmentOperandView(o, storageEnrollmentOperandCheckpoint, raw, anchor, storageEnrollmentCheckpointDecoder(anchor), func(v storageEnrollmentCheckpointOperand) storageEnrollmentCheckpointFacts {
		return storageEnrollmentCheckpointFacts{Generation: v.Value.Generation, Applied: v.Value.Applied, Digest: v.Digest}
	})
}

type storageEnrollmentTicketOperand struct {
	Value  storageEnrollmentPeakQTicket
	Digest storagebudget.Digest
}

func (o *storageEnrollmentOperands) ticket(raw []byte, slot int, anchor storageEnrollmentPeakQHeldAnchor) (storageEnrollmentTicketOperand, error) {
	context := struct {
		Anchor storageEnrollmentPeakQHeldAnchor
		Slot   int
	}{anchor, slot}
	return storageEnrollmentOperandGet(o, storageEnrollmentOperandTicket, raw, context, func(held []byte) (storageEnrollmentTicketOperand, error) {
		t, err := storageEnrollmentPeakQDecodeTicket(append([]byte(nil), held...), slot, anchor)
		if err != nil {
			return storageEnrollmentTicketOperand{}, err
		}
		return storageEnrollmentTicketOperand{t, sha256.Sum256(held)}, nil
	})
}

type storageEnrollmentPeerOperand struct {
	Raw  [2][]byte
	Bank byte
}

func (o *storageEnrollmentOperands) peer(scope storageEnrollmentPeakScope, raw []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2, composition bool) (storageEnrollmentPeerOperand, error) {
	context, decode := storageEnrollmentPeerOperandKey(scope, record, claim, composition)
	return storageEnrollmentOperandGet(o, storageEnrollmentOperandPeer, raw, context, decode)
}

// peerValid is peer when only the strict success of the restoration is used.
func (o *storageEnrollmentOperands) peerValid(scope storageEnrollmentPeakScope, raw []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2, composition bool) error {
	context, decode := storageEnrollmentPeerOperandKey(scope, record, claim, composition)
	_, err := storageEnrollmentOperandView(o, storageEnrollmentOperandPeer, raw, context, decode, func(storageEnrollmentPeerOperand) struct{} { return struct{}{} })
	return err
}
func storageEnrollmentPeerOperandKey(scope storageEnrollmentPeakScope, record storageOwnerRecordV2, claim storageOwnerClaimV2, composition bool) (any, func([]byte) (storageEnrollmentPeerOperand, error)) {
	context := struct {
		Scope       storageEnrollmentPeakScope
		Record      storageOwnerRecordV2
		Claim       storageOwnerClaimV2
		Composition bool
	}{scope, record, claim, composition}
	return context, func(held []byte) (storageEnrollmentPeerOperand, error) {
		p, b, err := storageEnrollmentPeakRestoreOriginalPeerVariant(scope, held, record, claim, composition)
		return storageEnrollmentPeerOperand{p, b}, err
	}
}

func (o *storageEnrollmentOperands) catalog(raw [2][]byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageOwnerCatalogPair, error) {
	context := struct {
		Peer   []byte
		Record storageOwnerRecordV2
		Claim  storageOwnerClaimV2
	}{raw[1], record, claim}
	return storageEnrollmentOperandGet(o, storageEnrollmentOperandCatalog, raw[0], context, func(held []byte) (storageOwnerCatalogPair, error) {
		peer := raw[1]
		if o != nil {
			peer = append([]byte(nil), peer...)
		}
		return storageOwnerDecodeCatalogPairV3(held, peer, record, claim)
	})
}
func (o *storageEnrollmentOperands) composition(e storageEnrollmentPeakEvidence, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageCompositionPair, error) {
	context := struct {
		Version     uint32
		Lineage     *storageCompositionLineage
		Projections []storageEnrollmentPeakProjection
		Before      storagebudget.Digest
		Scope       storageEnrollmentPeakScope
		Record      storageOwnerRecordV2
		Claim       storageOwnerClaimV2
	}{e.Version, e.CompositionLineage, e.Projections, e.Before, e.Scope, record, claim}
	return storageEnrollmentOperandGet(o, storageEnrollmentOperandComposition, e.Raw, context, func([]byte) (storageCompositionPair, error) { return e.compositionBefore(record, claim) })
}
func (o *storageEnrollmentOperands) projectionDigest(p storageEnrollmentPeakProjection) storagebudget.Digest {
	digest, _ := storageEnrollmentOperandGet(o, storageEnrollmentOperandProjection, p.Raw, p, func([]byte) (storagebudget.Digest, error) { return storageEnrollmentPeakProjectionFullDigest(p), nil })
	return digest
}

func (o *storageEnrollmentOperands) composedPair(raw [2][]byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageCompositionPair, error) {
	context := struct {
		Peer   []byte
		Record storageOwnerRecordV2
		Claim  storageOwnerClaimV2
	}{raw[1], record, claim}
	return storageEnrollmentOperandGet(o, storageEnrollmentOperandComposedPair, raw[0], context, func(held []byte) (storageCompositionPair, error) {
		peer := raw[1]
		if o != nil {
			peer = append([]byte(nil), peer...)
		}
		return storageCompositionReadPair(held, peer, record, claim)
	})
}
