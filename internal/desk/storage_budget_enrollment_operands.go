package desk

import (
	"bytes"
	"crypto/sha256"
	"reflect"
	"strings"

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
// Byte slices use reflect.Copy, never a per-byte reflection loop. Strings and
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
func storageEnrollmentOperandCloneValue(v reflect.Value, freezeStrings bool) reflect.Value {
	out := reflect.New(v.Type()).Elem()
	out.Set(v)
	switch v.Kind() {
	case reflect.String:
		if freezeStrings {
			out.SetString(strings.Clone(v.String()))
		}
	case reflect.Pointer:
		if !v.IsNil() {
			out.Set(reflect.New(v.Type().Elem()))
			out.Elem().Set(storageEnrollmentOperandCloneValue(v.Elem(), freezeStrings))
		}
	case reflect.Interface:
		if !v.IsNil() {
			out.Set(storageEnrollmentOperandCloneValue(v.Elem(), freezeStrings))
		}
	case reflect.Slice:
		if !v.IsNil() {
			out.Set(reflect.MakeSlice(v.Type(), v.Len(), v.Len()))
			reflect.Copy(out, v)
			if storageEnrollmentOperandMutable(v.Type().Elem()) || freezeStrings && storageEnrollmentOperandDynamic(v.Type().Elem()) {
				for i := 0; i < v.Len(); i++ {
					out.Index(i).Set(storageEnrollmentOperandCloneValue(v.Index(i), freezeStrings))
				}
			}
		}
	case reflect.Array:
		if storageEnrollmentOperandMutable(v.Type().Elem()) || freezeStrings && storageEnrollmentOperandDynamic(v.Type().Elem()) {
			for i := 0; i < v.Len(); i++ {
				out.Index(i).Set(storageEnrollmentOperandCloneValue(v.Index(i), freezeStrings))
			}
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if storageEnrollmentOperandMutable(v.Type().Field(i).Type) || freezeStrings && storageEnrollmentOperandDynamic(v.Type().Field(i).Type) {
				out.Field(i).Set(storageEnrollmentOperandCloneValue(v.Field(i), freezeStrings))
			}
		}
	case reflect.Map:
		if !v.IsNil() {
			out.Set(reflect.MakeMapWithSize(v.Type(), v.Len()))
			iter := v.MapRange()
			for iter.Next() {
				out.SetMapIndex(storageEnrollmentOperandCloneValue(iter.Key(), freezeStrings), storageEnrollmentOperandCloneValue(iter.Value(), freezeStrings))
			}
		}
	}
	return out
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
		return storageEnrollmentPeakDecodeEvidenceWithLineage(held, lineage, record, claim)
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

func (o *storageEnrollmentOperands) checkpoint(raw []byte, anchor storageEnrollmentPeakQHeldAnchor) (storageEnrollmentCheckpointOperand, error) {
	return storageEnrollmentOperandGet(o, storageEnrollmentOperandCheckpoint, raw, anchor, func(held []byte) (storageEnrollmentCheckpointOperand, error) {
		q, err := storageEnrollmentPeakQDecodeCheckpoint(append([]byte(nil), held...), anchor)
		if err != nil {
			return storageEnrollmentCheckpointOperand{}, err
		}
		return storageEnrollmentCheckpointOperand{q, sha256.Sum256(held)}, nil
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
	context := struct {
		Scope       storageEnrollmentPeakScope
		Record      storageOwnerRecordV2
		Claim       storageOwnerClaimV2
		Composition bool
	}{scope, record, claim, composition}
	return storageEnrollmentOperandGet(o, storageEnrollmentOperandPeer, raw, context, func(held []byte) (storageEnrollmentPeerOperand, error) {
		p, b, err := storageEnrollmentPeakRestoreOriginalPeerVariant(scope, held, record, claim, composition)
		return storageEnrollmentPeerOperand{p, b}, err
	})
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
