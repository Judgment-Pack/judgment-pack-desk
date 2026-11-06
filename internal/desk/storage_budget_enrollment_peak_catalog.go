package desk

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"math"
	"path/filepath"
	"reflect"
	"sort"
	"unicode/utf8"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

type storageEnrollmentPeakInner struct {
	Catalog  storageOwnerCatalogV2
	Controls [4]storageOwnerPersistentObject
	Raw      []byte
}

// Packed policy source images are comparison material. Callers retain original
// custody and separately compare actual current V6 native banks. In particular,
// the reconstructed old-format pair is never a new native witness or permit.
func storageOwnerPackedSourceOriginal(pair [2][]byte, evidenceRaw []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) ([]byte, error) {
	cores, selected, err := storageEnrollmentOriginalCorePair(pair, record, claim)
	if err != nil {
		return nil, err
	}
	lineage := storageCompositionEmptyLineage()
	source := storageOwnerV6PackedSource{Version: 1, Encoding: 1, Physical: []byte{}}
	if string(pair[selected][:8]) == "DSKONL1\x00" {
		physical, e := storageCompositionReadPair(pair[0], pair[1], record, claim)
		if e != nil || physical.RecoveryOnly {
			return nil, storagebudget.ErrBounds
		}
		projection, e := storageCompositionCaptureProjection(pair[0], pair[1], record, claim)
		if e != nil {
			return nil, e
		}
		lineage = physical.Banks[physical.Selected].Lineage
		source.Encoding = 2
		source.Physical = append([]byte{}, projection[:]...)
	} else if !bytes.Equal(pair[0], pair[1]) {
		// One bare core is lossless only after the actual original duplicate
		// pair is positively compared, never from a selected core alone.
		return nil, storagebudget.ErrBounds
	}
	source.Core, err = storageCompositionCompactCore(cores[selected])
	if err != nil || len(source.Core) > 8320 {
		return nil, storagebudget.ErrBounds
	}
	l, err := lineage.encode()
	if err != nil {
		return nil, err
	}
	n := int(storageEnrollmentPeakU32(l[:4]))
	source.LineageRows = append([]byte{}, l[4:4+n]...)
	source.LineageSuffix = append([]byte{}, l[4+n:]...)
	raw, err := json.Marshal(source)
	if err != nil {
		return nil, err
	}
	_, restored, _, _, err := storageOwnerPackedSourceImage(raw, evidenceRaw, record, claim)
	if err != nil || !bytes.Equal(restored[0], pair[0]) || !bytes.Equal(restored[1], pair[1]) {
		return nil, storagebudget.ErrBounds
	}
	return raw, nil
}

// Resolve a currently selected source against its exact complete source
// evidence. Historical projections instead reconstruct bytes from retained
// selectors/lineage cutoffs; they must not read an overwritten old reference.
// Native ownership/currentness remains the lexical host's independent duty.
func storageOwnerPackedSourceImage(raw, evidenceRaw []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageOwnerCatalogV2, [2][]byte, storageCompositionLineage, storageEnrollmentPeakEvidence, error) {
	fail := func(err error) (storageOwnerCatalogV2, [2][]byte, storageCompositionLineage, storageEnrollmentPeakEvidence, error) {
		return storageOwnerCatalogV2{}, [2][]byte{}, storageCompositionLineage{}, storageEnrollmentPeakEvidence{}, err
	}
	source, lineage, err := storageOwnerDecodePackedSource(raw)
	if err == nil && (source.Encoding == 3 || source.Encoding == 4) {
		return fail(storagebudget.ErrBounds)
	}
	if err != nil {
		return fail(err)
	}
	core := make([]byte, storageOwnerCatalogBytes)
	copy(core, source.Core)
	compact, err := storageCompositionCompactCore(core)
	if err != nil || !bytes.Equal(compact, source.Core) {
		return fail(storagebudget.ErrBounds)
	}
	pair := [2][]byte{append([]byte{}, core...), append([]byte{}, core...)}
	if source.Encoding == 2 {
		var p storageCompositionProjection
		copy(p[:], source.Physical)
		pair, err = p.restore(core, lineage, record, claim)
		if err != nil {
			return fail(err)
		}
	}
	evidence, err := storageEnrollmentPeakDecodeEvidenceWithLineage(evidenceRaw, &lineage, record, claim)
	if err != nil {
		return fail(err)
	}
	outer, err := storageEnrollmentPeakDecodeEnvelopeVariant(core, record, claim, true, source.Encoding == 2)
	if err != nil || outer.Kind != 2 || outer.Marker == nil || outer.Marker.Phase != 3 || storageEnrollmentPeakJoinMarker(outer, evidence, evidence.Ready) != nil {
		return fail(storagebudget.ErrBounds)
	}
	if lineage.validateGraph(evidence.Inner.Catalog, record) != nil {
		return fail(storagebudget.ErrBounds)
	}
	return evidence.Inner.Catalog, pair, lineage, evidence, nil
}

var storageEnrollmentPeakNames = [4]string{"storage-enrollment.a", "storage-enrollment.b", "storage-enrollment-step.a", "storage-enrollment-step.b"}

func storageEnrollmentPeakDecodeInner(raw []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageEnrollmentPeakInner, error) {
	var out storageEnrollmentPeakInner
	payload, seq, err := storageEnrollmentPeakPayload(raw, "DSKIPK1\x00", false)
	if err != nil {
		return out, err
	}
	r := storageEnrollmentPeakReader{raw: payload}
	n := r.u32()
	if n > uint32(len(payload)) || n == 0 {
		return out, storagebudget.ErrBounds
	}
	packed := r.take(int(n))
	decoded, err := storageEnrollmentPeakUnpackInner(packed)
	if err != nil {
		return out, err
	}
	if storageEnrollmentPeakCatalogJSON(decoded, &out.Catalog) != nil || out.Catalog.Sequence != seq || storageEnrollmentPeakValidateCatalog(out.Catalog, record, claim) != nil || r.u32() != 7 {
		return storageEnrollmentPeakInner{}, storagebudget.ErrBounds
	}
	seen := map[storageOwnerPersistentObject]bool{}
	for i := range out.Controls {
		p := r.native()
		name := r.data(len(storageEnrollmentPeakNames[i]))
		extent := r.u64()
		want := uint64(71303168)
		if i >= 2 {
			want = 4096
		}
		if !p.valid(2) || seen[p] || !bytes.Equal(name, []byte(storageEnrollmentPeakNames[i])) || extent != want {
			return storageEnrollmentPeakInner{}, storagebudget.ErrBounds
		}
		seen[p] = true
		out.Controls[i] = p
	}
	if !r.done() {
		return storageEnrollmentPeakInner{}, storagebudget.ErrBounds
	}
	out.Raw = append([]byte{}, raw...)
	return out, nil
}
func storageEnrollmentPeakEncodeInner(value storageEnrollmentPeakInner, record storageOwnerRecordV2, claim storageOwnerClaimV2) ([]byte, error) {
	if storageEnrollmentPeakValidateCatalog(value.Catalog, record, claim) != nil {
		return nil, storagebudget.ErrBounds
	}
	j, err := json.Marshal(value.Catalog)
	if err != nil {
		return nil, err
	}
	p, err := storageEnrollmentPeakPackInner(j)
	if err != nil {
		return nil, err
	}
	w := storageEnrollmentPeakWriter{max: storageOwnerCatalogBytes - 64}
	w.u32(uint32(len(p)))
	w.put(p)
	w.u32(7)
	for i, v := range value.Controls {
		w.put(storageEnrollmentPeakNativeBytes(v))
		w.data([]byte(storageEnrollmentPeakNames[i]), 25)
		if i < 2 {
			w.u64(71303168)
		} else {
			w.u64(4096)
		}
	}
	if w.bad {
		return nil, storagebudget.ErrBounds
	}
	raw, err := storageEnrollmentPeakFrame("DSKIPK1\x00", w.raw, value.Catalog.Sequence, false)
	if err != nil {
		return nil, err
	}
	if _, err = storageEnrollmentPeakDecodeInner(raw, record, claim); err != nil {
		return nil, err
	}
	return raw, nil
}
func storageEnrollmentPeakInnerDigest(raw []byte) storagebudget.Digest {
	return storageEnrollmentPeakHash("desk-enrollment-inner-catalog-peak-v1", raw)
}

type storageEnrollmentPeakMarker struct {
	Phase, Bank                                            uint8
	Installation                                           storagebudget.ID
	QBinding                                               storagebudget.Digest
	Bridge                                                 storagebudget.ID
	Previous, Before, Inner, Evidence                      storagebudget.Digest
	Extent                                                 uint64
	Controls                                               [4]storageOwnerPersistentObject
	CoveredEpoch, Q0Revision, Q0Epoch, Q3Revision, Q3Epoch uint64
	Record, Claim, Ready                                   storagebudget.Digest
}

func storageEnrollmentPeakReadMarker(raw []byte, s storageEnrollmentPeakScope) (storageEnrollmentPeakMarker, error) {
	var m storageEnrollmentPeakMarker
	if len(raw) != 4096 {
		return m, storagebudget.ErrBounds
	}
	r := storageEnrollmentPeakReader{raw: raw}
	if r.u32() != 7 {
		return m, storagebudget.ErrBounds
	}
	m.Phase = r.u8()
	m.Bank = r.u8()
	if r.u16() != 0 {
		return m, storagebudget.ErrBounds
	}
	m.Installation = r.id()
	m.QBinding = r.digest()
	m.Bridge = r.id()
	m.Previous = r.digest()
	m.Before = r.digest()
	m.Inner = r.digest()
	m.Evidence = r.digest()
	m.Extent = r.u64()
	for i := range m.Controls {
		m.Controls[i] = r.native()
	}
	m.CoveredEpoch = r.u64()
	m.Q0Revision = r.u64()
	m.Q0Epoch = r.u64()
	m.Q3Revision = r.u64()
	m.Q3Epoch = r.u64()
	m.Record = r.digest()
	m.Claim = r.digest()
	m.Ready = r.digest()
	if !r.zeroTail() || m.Phase > 3 || m.Bank > 1 || !bytes.Equal(m.Installation[:], s[40:56]) || !bytes.Equal(m.QBinding[:], s[56:88]) || !bytes.Equal(m.Bridge[:], s[24:40]) || !bytes.Equal(m.Before[:], s[88:120]) || m.Previous == (storagebudget.Digest{}) || m.Inner == (storagebudget.Digest{}) || m.Evidence == (storagebudget.Digest{}) || m.Extent != s.final(m.Bank) || m.Q0Revision == 0 || m.Q0Epoch == 0 || m.Q0Revision > ^uint64(0)-3 || m.Q0Epoch == ^uint64(0) || m.Q3Revision != m.Q0Revision+3 || m.Q3Epoch != m.Q0Epoch+1 || m.CoveredEpoch != m.Q3Epoch || m.Record == (storagebudget.Digest{}) || m.Claim == (storagebudget.Digest{}) || m.Ready == (storagebudget.Digest{}) {
		return storageEnrollmentPeakMarker{}, storagebudget.ErrBounds
	}
	for i, p := range m.Controls {
		if p != s.target(byte(i+1)) {
			return storageEnrollmentPeakMarker{}, storagebudget.ErrBounds
		}
	}
	return m, nil
}
func storageEnrollmentPeakWriteMarker(m storageEnrollmentPeakMarker, s storageEnrollmentPeakScope) ([]byte, error) {
	w := storageEnrollmentPeakWriter{max: 4096}
	w.u32(7)
	w.u8(m.Phase)
	w.u8(m.Bank)
	w.u16(0)
	w.put(m.Installation[:])
	w.put(m.QBinding[:])
	w.put(m.Bridge[:])
	for _, d := range []storagebudget.Digest{m.Previous, m.Before, m.Inner, m.Evidence} {
		w.put(d[:])
	}
	w.u64(m.Extent)
	for _, p := range m.Controls {
		w.put(storageEnrollmentPeakNativeBytes(p))
	}
	for _, v := range []uint64{m.CoveredEpoch, m.Q0Revision, m.Q0Epoch, m.Q3Revision, m.Q3Epoch} {
		w.u64(v)
	}
	for _, d := range []storagebudget.Digest{m.Record, m.Claim, m.Ready} {
		w.put(d[:])
	}
	if w.bad {
		return nil, storagebudget.ErrBounds
	}
	out := make([]byte, 4096)
	copy(out, w.raw)
	if _, err := storageEnrollmentPeakReadMarker(out, s); err != nil {
		return nil, err
	}
	return out, nil
}

type storageEnrollmentPeakSnapshot struct {
	Kind, Encoding      uint8
	Header              [64]byte
	Full, Inner         storagebudget.Digest
	Evidence            storageOwnerPersistentObject
	Bank                uint8
	Extent              uint64
	EvidenceDigest      storagebudget.Digest
	Q3Revision, Q3Epoch uint64
	Before              []byte
	Intent              *storageEnrollmentSuccessorIntent
	Physical            storageCompositionProjection
	PolicyPeerPrevious  storagebudget.Digest
	PolicyBank          byte
}
type storageEnrollmentPeakEnvelope struct {
	Kind, BeforeCodec uint8
	Sequence          uint64
	Scope             storageEnrollmentPeakScope
	Snapshot          *storageEnrollmentPeakSnapshot
	Marker            *storageEnrollmentPeakMarker
}

func storageEnrollmentPeakReadSnapshot(raw []byte, s storageEnrollmentPeakScope, encoding byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageEnrollmentPeakSnapshot, error) {
	return storageEnrollmentPeakReadSnapshotNative(raw, s, encoding, record, claim, false)
}

func storageEnrollmentPeakReadSnapshotNative(raw []byte, s storageEnrollmentPeakScope, encoding byte, record storageOwnerRecordV2, claim storageOwnerClaimV2, native bool) (storageEnrollmentPeakSnapshot, error) {
	if encoding == 6 && !native {
		return storageEnrollmentPeakSnapshot{}, storagebudget.ErrBounds
	}
	var x storageEnrollmentPeakSnapshot
	if len(raw) < 256 {
		return x, storagebudget.ErrBounds
	}
	r := storageEnrollmentPeakReader{raw: raw[:256]}
	x.Kind = r.u8()
	x.Encoding = r.u8()
	reserved := r.u16()
	originalN := r.u32()
	dataN := r.u32()
	reserved2 := r.u32()
	copy(x.Header[:], r.take(64))
	x.Full = r.digest()
	x.Inner = r.digest()
	x.Evidence = r.native()
	x.Bank = r.u8()
	x.Extent = r.u64()
	x.EvidenceDigest = r.digest()
	x.Q3Revision = r.u64()
	x.Q3Epoch = r.u64()
	if encoding == 5 {
		x.PolicyBank = r.u8()
		if x.PolicyBank > 1 {
			return storageEnrollmentPeakSnapshot{}, storagebudget.ErrBounds
		}
	}
	if !r.zeroTail() || reserved != 0 || reserved2 != 0 || dataN != uint32(len(raw)-256) || x.Kind > 1 || x.Encoding != encoding || !bytes.Equal(x.Full[:], s[88:120]) || originalN > storageOwnerCatalogBytes-64 {
		return storageEnrollmentPeakSnapshot{}, storagebudget.ErrBounds
	}
	data := raw[256:]
	if encoding == 6 {
		// The actual pair is in the enclosing native capsule. This payload is
		// only the projected completed E1 core plus original successor intent.
		// Its sequence and SHA deliberately need not equal the actual V6 before.
		if !native || x.Kind != 1 || s.path() == 1 || !s.hasOriginalPeer() || originalN != 8256 || len(data) < 8320 || !bytes.Equal(data[:64], x.Header[:]) {
			return storageEnrollmentPeakSnapshot{}, storagebudget.ErrBounds
		}
		core := make([]byte, storageOwnerCatalogBytes)
		copy(core, data[:8320])
		old, err := storageEnrollmentPeakDecodeEnvelopeVariant(core, record, claim, true, true)
		if err != nil || old.Kind != 2 || old.Marker == nil || old.Marker.Phase != 3 {
			return storageEnrollmentPeakSnapshot{}, storagebudget.ErrBounds
		}
		m := old.Marker
		if x.Inner != m.Inner || x.Bank != m.Bank || x.Evidence != m.Controls[m.Bank] || x.Extent != m.Extent || x.EvidenceDigest != m.Evidence || x.Q3Revision != m.Q3Revision || x.Q3Epoch != m.Q3Epoch {
			return storageEnrollmentPeakSnapshot{}, storagebudget.ErrBounds
		}
		if len(data) > 8320 {
			intent, err := storageEnrollmentReadSuccessorIntent(data[8320:])
			if err != nil {
				return storageEnrollmentPeakSnapshot{}, err
			}
			x.Intent = &intent
		}
		x.Before = core
		return x, nil
	}
	if encoding == 5 {
		// The full original V6 header/payload is retained, not its digest alone.
		// The source marker is still independently joined to original evidence by
		// ComparePrefix/recovery. Structural parsing issues no native authority.
		if x.Kind != 1 || s.path() == 1 || !s.hasOriginalPeer() || x.Physical != (storageCompositionProjection{}) || uint64(len(data)) < 96+uint64(originalN) {
			return storageEnrollmentPeakSnapshot{}, storagebudget.ErrBounds
		}
		compact := data[:64+int(originalN)]
		if !bytes.Equal(compact[:64], x.Header[:]) {
			return storageEnrollmentPeakSnapshot{}, storagebudget.ErrBounds
		}
		before := make([]byte, storageOwnerCatalogBytes)
		copy(before, compact)
		payload, seq, err := storageOwnerV6Payload(before)
		if err != nil || seq != storageEnrollmentPeakU64(s[1624:1632]) || !storageOwnerV6PackedPreflight(payload) || sha256.Sum256(before) != x.Full {
			return storageEnrollmentPeakSnapshot{}, storagebudget.ErrBounds
		}
		var outer storageOwnerV6PackedCatalog
		if json.Unmarshal(payload, &outer) != nil {
			return storageEnrollmentPeakSnapshot{}, storagebudget.ErrBounds
		}
		canonical, err := json.Marshal(outer)
		if err != nil || !bytes.Equal(canonical, payload) || outer.Sequence != seq || outer.Phase != "active" && outer.Phase != "aborted" && outer.Phase != "aborted-unfenced" {
			return storageEnrollmentPeakSnapshot{}, storagebudget.ErrBounds
		}
		source, _, err := storageOwnerDecodePackedSource(outer.Source)
		if err != nil {
			return storageEnrollmentPeakSnapshot{}, err
		}
		core := make([]byte, storageOwnerCatalogBytes)
		copy(core, source.Core)
		marker, err := storageEnrollmentPeakDecodeEnvelopeVariant(core, record, claim, true, source.Encoding == 2)
		if err != nil || marker.Kind != 2 || marker.Marker == nil || marker.Marker.Phase != 3 {
			return storageEnrollmentPeakSnapshot{}, storagebudget.ErrBounds
		}
		m := marker.Marker
		if x.Inner != m.Inner || x.Bank != m.Bank || x.Evidence != m.Controls[m.Bank] || x.Extent != m.Extent || x.EvidenceDigest != m.Evidence || x.Q3Revision != m.Q3Revision || x.Q3Epoch != m.Q3Epoch {
			return storageEnrollmentPeakSnapshot{}, storagebudget.ErrBounds
		}
		copy(x.PolicyPeerPrevious[:], data[len(compact):len(compact)+32])
		if x.PolicyPeerPrevious == (storagebudget.Digest{}) || x.PolicyBank != s[322] {
			return storageEnrollmentPeakSnapshot{}, storagebudget.ErrBounds
		}
		tail := data[len(compact)+32:]
		if len(tail) > 0 {
			intent, err := storageEnrollmentReadSuccessorIntent(tail)
			if err != nil {
				return storageEnrollmentPeakSnapshot{}, err
			}
			x.Intent = &intent
		}
		x.Before = before
		return x, nil
	}
	coreEncoding := encoding
	if encoding == 4 {
		if !s.hasOriginalPeer() || len(data) < storageCompositionProjectionBytes {
			return storageEnrollmentPeakSnapshot{}, storagebudget.ErrBounds
		}
		copy(x.Physical[:], data[len(data)-storageCompositionProjectionBytes:])
		data = data[:len(data)-storageCompositionProjectionBytes]
		if !x.Physical.shape() {
			return storageEnrollmentPeakSnapshot{}, storagebudget.ErrBounds
		}
		coreEncoding = 3
		if x.Kind == 0 {
			coreEncoding = 0
			if originalN > 8384128 {
				coreEncoding = 1
			}
		}
	}
	var payload []byte
	if x.Kind == 0 {
		if s.path() != 1 || coreEncoding > 1 || originalN <= 8384128 && coreEncoding != 0 || originalN > 8384128 && coreEncoding != 1 || x.Evidence != (storageOwnerPersistentObject{}) || x.Bank != 255 || x.Extent != 0 || x.EvidenceDigest != (storagebudget.Digest{}) || x.Q3Revision != 0 || x.Q3Epoch != 0 {
			return storageEnrollmentPeakSnapshot{}, storagebudget.ErrBounds
		}
		if coreEncoding == 0 {
			payload = data
			if len(payload) != int(originalN) {
				return storageEnrollmentPeakSnapshot{}, storagebudget.ErrBounds
			}
		} else {
			var err error
			payload, err = storageEnrollmentPeakUnpackV3(data, originalN)
			if err != nil {
				return storageEnrollmentPeakSnapshot{}, err
			}
		}
		if storageEnrollmentPeakHash("desk-enrollment-original-v3-inner-peak-v1", payload) != x.Inner {
			return storageEnrollmentPeakSnapshot{}, storagebudget.ErrBounds
		}
	} else {
		if s.path() == 1 || (coreEncoding != 2 && coreEncoding != 3) || len(data) < 8320 || originalN != 8256 || !bytes.Equal(x.Header[:], data[:64]) {
			return storageEnrollmentPeakSnapshot{}, storagebudget.ErrBounds
		}
		if coreEncoding == 2 {
			if len(data) != 8320 {
				return storageEnrollmentPeakSnapshot{}, storagebudget.ErrBounds
			}
		} else {
			if !s.hasOriginalPeer() {
				return storageEnrollmentPeakSnapshot{}, storagebudget.ErrBounds
			}
			intent, err := storageEnrollmentReadSuccessorIntent(data[8320:])
			if err != nil {
				return storageEnrollmentPeakSnapshot{}, err
			}
			x.Intent = &intent
		}
		old, err := storageEnrollmentPeakDecodeEnvelopeVariant(data[:8320], record, claim, false, encoding == 4)
		if err != nil || old.Kind != 2 || old.Marker == nil || old.Marker.Phase != 3 {
			return storageEnrollmentPeakSnapshot{}, storagebudget.ErrBounds
		}
		m := old.Marker
		if x.Inner != m.Inner || x.Bank != m.Bank || x.Evidence != m.Controls[m.Bank] || x.Extent != m.Extent || x.EvidenceDigest != m.Evidence || x.Q3Revision != m.Q3Revision || x.Q3Epoch != m.Q3Epoch {
			return storageEnrollmentPeakSnapshot{}, storagebudget.ErrBounds
		}
		payload = data[64:8320]
	}
	before := make([]byte, storageOwnerCatalogBytes)
	copy(before[:64], x.Header[:])
	copy(before[64:], payload)
	if sha256.Sum256(before) != x.Full {
		return storageEnrollmentPeakSnapshot{}, storagebudget.ErrBounds
	}
	if x.Kind == 0 {
		if _, err := storageOwnerDecodeCatalogV3(before, record, claim); err != nil {
			return storageEnrollmentPeakSnapshot{}, err
		}
	} else {
		if _, _, err := storageEnrollmentPeakPayload(before, "DSKONP1\x00", true); err != nil {
			return storageEnrollmentPeakSnapshot{}, err
		}
	}
	if storageEnrollmentPeakU64(x.Header[16:24]) != storageEnrollmentPeakU64(s[1624:1632]) {
		return storageEnrollmentPeakSnapshot{}, storagebudget.ErrBounds
	}
	x.Before = before
	return x, nil
}

func storageEnrollmentPeakDecodeEnvelope(raw []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2, full bool) (storageEnrollmentPeakEnvelope, error) {
	return storageEnrollmentPeakDecodeEnvelopeVariant(raw, record, claim, full, false)
}

func storageEnrollmentPeakDecodeEnvelopeVariant(raw []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2, full, composition bool) (storageEnrollmentPeakEnvelope, error) {
	return storageEnrollmentPeakDecodeEnvelopeNative(raw, record, claim, full, composition, false)
}

func storageEnrollmentPeakDecodeEnvelopeNative(raw []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2, full, composition, native bool) (storageEnrollmentPeakEnvelope, error) {
	var e storageEnrollmentPeakEnvelope
	p, seq, err := storageEnrollmentPeakPayload(raw, "DSKONP1\x00", full)
	if err != nil {
		return e, err
	}
	if len(p) < 4160 {
		return e, storagebudget.ErrBounds
	}
	composition = composition || p[0] < 2 && (p[2] == 4 || p[2] == 5 || native && p[2] == 6)
	s, err := storageEnrollmentPeakDecodeScopeVariant(p[64:4160], composition)
	if err != nil {
		return e, err
	}
	if !bytes.Equal(s[40:56], record.Installation[:]) || !bytes.Equal(s[56:88], record.QBinding[:]) || s.target(5) != record.CatalogA || s.target(6) != record.CatalogB || !claim.matches(record) {
		return e, storagebudget.ErrBounds
	}
	kind, before, enc := p[0], p[1], p[2]
	sn, mn := uint64(storageEnrollmentPeakU32(p[8:12])), uint64(storageEnrollmentPeakU32(p[12:16]))
	if kind > 2 || before > 1 || p[3] != 0 || storageEnrollmentPeakU32(p[4:8]) != 4096 || uint64(len(p)) != 4160+sn+mn || !bytes.Equal(p[16:48], s[88:120]) || !bytes.Equal(p[48:64], s[8:24]) || s.path() == 1 && before != 0 || s.path() != 1 && before != 1 {
		return e, storagebudget.ErrBounds
	}
	e = storageEnrollmentPeakEnvelope{Kind: kind, BeforeCodec: before, Sequence: seq, Scope: s}
	base := storageEnrollmentPeakU64(s[1624:1632])
	if kind < 2 {
		if sn < 256 || mn != 0 || kind == 0 && (s.path() == 1 && s[321] != 0 || s.path() != 1 && s[321] != 15) || kind == 1 && (s.path() != 1 || s[321] != 15) || kind == 0 && seq != base+1 || kind == 1 && seq != base+2 {
			return storageEnrollmentPeakEnvelope{}, storagebudget.ErrBounds
		}
		x, err := storageEnrollmentPeakReadSnapshotNative(p[4160:], s, enc, record, claim, native)
		if err != nil {
			return storageEnrollmentPeakEnvelope{}, err
		}
		if x.Kind != before || x.Intent != nil && (kind != 0 || before != 1) {
			return storageEnrollmentPeakEnvelope{}, storagebudget.ErrBounds
		}
		e.Snapshot = &x
	} else {
		if sn != 0 || mn != 4096 || enc != 255 || s[321] != 15 {
			return storageEnrollmentPeakEnvelope{}, storagebudget.ErrBounds
		}
		m, err := storageEnrollmentPeakReadMarker(p[4160:], s)
		if err != nil {
			return storageEnrollmentPeakEnvelope{}, err
		}
		offset := uint64(0)
		switch m.Phase {
		case 0:
			if s.path() != 3 {
				return storageEnrollmentPeakEnvelope{}, storagebudget.ErrBounds
			}
			offset = 2
		case 1:
			offset = 3
			if s.path() == 2 {
				offset = 2
			}
		case 2:
			offset = 4
			if s.path() == 2 {
				offset = 3
			}
		case 3:
			offset = 5
			if s.path() == 2 {
				offset = 4
			}
		}
		if seq != base+offset {
			return storageEnrollmentPeakEnvelope{}, storagebudget.ErrBounds
		}
		e.Marker = &m
	}
	return e, nil
}

func storageEnrollmentPeakEncodeEnvelope(e storageEnrollmentPeakEnvelope, record storageOwnerRecordV2, claim storageOwnerClaimV2) ([]byte, error) {
	return storageEnrollmentPeakEncodeEnvelopeVariant(e, record, claim, false)
}

func storageEnrollmentPeakEncodeEnvelopeVariant(e storageEnrollmentPeakEnvelope, record storageOwnerRecordV2, claim storageOwnerClaimV2, composition bool) ([]byte, error) {
	return storageEnrollmentPeakEncodeEnvelopeNative(e, record, claim, composition, false)
}

func storageEnrollmentPeakEncodeEnvelopeNative(e storageEnrollmentPeakEnvelope, record storageOwnerRecordV2, claim storageOwnerClaimV2, composition, native bool) ([]byte, error) {
	if e.Snapshot != nil && e.Snapshot.Encoding == 6 && !native {
		return nil, storagebudget.ErrBounds
	}
	composition = composition || e.Snapshot != nil && (e.Snapshot.Physical != (storageCompositionProjection{}) || e.Snapshot.Encoding == 5 || native && e.Snapshot.Encoding == 6)
	if _, err := storageEnrollmentPeakDecodeScopeVariant(e.Scope[:], composition); err != nil {
		return nil, err
	}
	p := make([]byte, 4160)
	p[0] = e.Kind
	p[1] = e.BeforeCodec
	storageEnrollmentPeakPut32(p[4:8], 4096)
	copy(p[16:48], e.Scope[88:120])
	copy(p[48:64], e.Scope[8:24])
	copy(p[64:], e.Scope[:])
	if e.Kind == 2 {
		if e.Marker == nil || e.Snapshot != nil {
			return nil, storagebudget.ErrBounds
		}
		b, err := storageEnrollmentPeakWriteMarker(*e.Marker, e.Scope)
		if err != nil {
			return nil, err
		}
		p[2] = 255
		storageEnrollmentPeakPut32(p[12:16], 4096)
		p = append(p, b...)
	} else {
		if e.Snapshot == nil || e.Marker != nil {
			return nil, storagebudget.ErrBounds
		}
		x := e.Snapshot
		if x.Encoding == 4 && x.Physical == (storageCompositionProjection{}) {
			return nil, storagebudget.ErrBounds
		}
		if x.Intent != nil && (x.Kind != 1 || e.BeforeCodec != 1 || e.Kind != 0 || e.Scope.path() == 1 || !e.Scope.hasOriginalPeer()) {
			return nil, storagebudget.ErrBounds
		}
		if len(x.Before) != storageOwnerCatalogBytes {
			return nil, storagebudget.ErrBounds
		}
		n := storageEnrollmentPeakU32(x.Before[24:28])
		if n > storageOwnerCatalogBytes-64 {
			return nil, storagebudget.ErrBounds
		}
		data := x.Before[64 : 64+int(n)]
		enc := byte(0)
		if x.Encoding == 6 {
			if !native || x.Kind != 1 || e.BeforeCodec != 1 || e.Scope.path() == 1 || !e.Scope.hasOriginalPeer() || x.Physical != (storageCompositionProjection{}) || n != 8256 {
				return nil, storagebudget.ErrBounds
			}
			data = append([]byte{}, x.Before[:8320]...)
			if x.Intent != nil {
				intent, err := storageEnrollmentWriteSuccessorIntent(*x.Intent)
				if err != nil {
					return nil, err
				}
				data = append(data, intent...)
			}
			enc = 6
		} else if x.Encoding == 5 {
			if x.Kind != 1 || e.BeforeCodec != 1 || e.Kind != 0 || e.Scope.path() == 1 || !e.Scope.hasOriginalPeer() || x.Physical != (storageCompositionProjection{}) {
				return nil, storagebudget.ErrBounds
			}
			payload, _, err := storageOwnerV6Payload(x.Before)
			if err != nil || !storageOwnerV6PackedPreflight(payload) {
				return nil, storagebudget.ErrBounds
			}
			data = append([]byte{}, x.Before[:64+int(n)]...)
			if x.PolicyPeerPrevious == (storagebudget.Digest{}) || x.PolicyBank > 1 || x.PolicyBank != e.Scope[322] {
				return nil, storagebudget.ErrBounds
			}
			data = append(data, x.PolicyPeerPrevious[:]...)
			if x.Intent != nil {
				intent, err := storageEnrollmentWriteSuccessorIntent(*x.Intent)
				if err != nil {
					return nil, err
				}
				data = append(data, intent...)
			}
			enc = 5
		} else if e.BeforeCodec == 1 {
			if n != 8256 {
				return nil, storagebudget.ErrBounds
			}
			enc = 2
			data = append([]byte{}, x.Before[:8320]...)
			if x.Intent != nil {
				if e.Kind != 0 || !e.Scope.hasOriginalPeer() {
					return nil, storagebudget.ErrBounds
				}
				intent, err := storageEnrollmentWriteSuccessorIntent(*x.Intent)
				if err != nil {
					return nil, err
				}
				data = append(data, intent...)
				enc = 3
			}
		} else if n > 8384128 {
			var err error
			data, err = storageEnrollmentPeakPackV3(data)
			if err != nil {
				return nil, err
			}
			enc = 1
		}
		if x.Physical != (storageCompositionProjection{}) {
			if !x.Physical.shape() || e.BeforeCodec == 1 && enc != 3 {
				return nil, storagebudget.ErrBounds
			}
			data = append(append([]byte{}, data...), x.Physical[:]...)
			enc = 4
		}
		b := make([]byte, 256)
		b[0] = e.BeforeCodec
		b[1] = enc
		storageEnrollmentPeakPut32(b[4:8], n)
		storageEnrollmentPeakPut32(b[8:12], uint32(len(data)))
		copy(b[16:80], x.Before[:64])
		copy(b[80:112], x.Full[:])
		copy(b[112:144], x.Inner[:])
		copy(b[144:179], storageEnrollmentPeakNativeBytes(x.Evidence))
		b[179] = x.Bank
		storageEnrollmentPeakPut64(b[180:188], x.Extent)
		copy(b[188:220], x.EvidenceDigest[:])
		storageEnrollmentPeakPut64(b[220:228], x.Q3Revision)
		storageEnrollmentPeakPut64(b[228:236], x.Q3Epoch)
		if enc == 5 {
			b[236] = x.PolicyBank
		}
		b = append(b, data...)
		p[2] = enc
		storageEnrollmentPeakPut32(p[8:12], uint32(len(b)))
		p = append(p, b...)
	}
	raw, err := storageEnrollmentPeakFrame("DSKONP1\x00", p, e.Sequence, true)
	if err != nil {
		return nil, err
	}
	if _, err = storageEnrollmentPeakDecodeEnvelopeNative(raw, record, claim, true, composition, native); err != nil {
		return nil, err
	}
	return raw, nil
}

// Detached intent operands in the existing successor Snapshot. These bytes
// establish neither original custody nor a native/Q/creation permit.
type storageEnrollmentSuccessorIntent struct {
	Family, SourceKind, OriginFlags byte
	Origin                          storagebudget.ID
	Catalog, Inputs                 storagebudget.Digest
	Source                          []byte
}

func storageEnrollmentSuccessorFamily(kind byte, source storageEnrollmentPeakSource) byte {
	if !storageEnrollmentDefaultSourceNamespace(source) {
		return 0
	}
	if source.Default == nil {
		if kind == 7 && source.ActionPageDigest == (storagebudget.Digest{}) {
			return 5
		}
		if kind == 8 && source.ActionPageDigest != (storagebudget.Digest{}) {
			return 6
		}
		return 0
	}
	if source.ActionPageDigest != (storagebudget.Digest{}) {
		return 0
	}
	d := source.Default
	switch {
	case kind == 11 && d.Workflow == 6:
		return 9
	case kind == 9 && d.Workflow == 5:
		return 7
	case kind == 10 && d.Workflow == 4:
		return 8
	case kind == 9 && d.Workflow == 1:
		return 1
	case kind == 10 && d.Workflow == 2 && d.OwnSelf == 0:
		return 2
	case kind == 10 && d.Workflow == 2 && d.OwnSelf == 1:
		return 3
	case kind == 9 && d.Workflow == 3:
		return 4
	}
	return 0
}

func storageEnrollmentReadSuccessorIntent(raw []byte) (storageEnrollmentSuccessorIntent, error) {
	var v storageEnrollmentSuccessorIntent
	if len(raw) < 96 || string(raw[:4]) != "DSF1" || raw[4] != 1 || raw[5] != 1 || !storageEnrollmentPeakZero(raw[9:12]) {
		return v, storagebudget.ErrBounds
	}
	n := uint64(storageEnrollmentPeakU32(raw[12:16]))
	if n == 0 || n > storageEnrollmentPeakSourceMaximum || uint64(len(raw)) != 96+n {
		return v, storagebudget.ErrBounds
	}
	v.Family, v.SourceKind, v.OriginFlags = raw[6], raw[7], raw[8]
	copy(v.Origin[:], raw[16:32])
	copy(v.Catalog[:], raw[32:64])
	copy(v.Inputs[:], raw[64:96])
	if (v.OriginFlags != 0 && v.OriginFlags != 1 && v.OriginFlags != 3) || (v.OriginFlags == 0) != (v.Origin == (storagebudget.ID{})) || v.Catalog == (storagebudget.Digest{}) || v.Inputs == (storagebudget.Digest{}) {
		return storageEnrollmentSuccessorIntent{}, storagebudget.ErrBounds
	}
	source, err := storageEnrollmentPeakDecodeSource(raw[96:])
	if err != nil || v.Family == 0 || storageEnrollmentSuccessorFamily(v.SourceKind, source) != v.Family {
		return storageEnrollmentSuccessorIntent{}, storagebudget.ErrBounds
	}
	canonical, err := storageEnrollmentPeakEncodeSource(source)
	if err != nil || !bytes.Equal(canonical, raw[96:]) {
		return storageEnrollmentSuccessorIntent{}, storagebudget.ErrBounds
	}
	v.Source = append([]byte{}, raw[96:]...)
	return v, nil
}

func storageEnrollmentWriteSuccessorIntent(v storageEnrollmentSuccessorIntent) ([]byte, error) {
	if len(v.Source) == 0 || len(v.Source) > storageEnrollmentPeakSourceMaximum {
		return nil, storagebudget.ErrBounds
	}
	raw := make([]byte, 96+len(v.Source))
	copy(raw[:4], "DSF1")
	raw[4], raw[5] = 1, 1
	raw[6], raw[7], raw[8] = v.Family, v.SourceKind, v.OriginFlags
	storageEnrollmentPeakPut32(raw[12:16], uint32(len(v.Source)))
	copy(raw[16:32], v.Origin[:])
	copy(raw[32:64], v.Catalog[:])
	copy(raw[64:96], v.Inputs[:])
	copy(raw[96:], v.Source)
	if _, err := storageEnrollmentReadSuccessorIntent(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// Composition is a physical bank format, not an owner or an effect capability.
// Core and physical full digests deliberately have different Go types. A decoded
// bank still needs original native custody, selected evidence/source/Q joins,
// persistence and a live admitted caller before it can participate in an effect.
type storageCompositionPhysicalDigest storagebudget.Digest
type storageCompositionCoreDigest storagebudget.Digest
type storageCompositionChecksum storagebudget.Digest

const (
	storageCompositionPrefixBytes          = 256
	storageCompositionLineageMax           = 7442534
	storageCompositionProjectionBytes      = 712
	storageCompositionPrepared        byte = 1
	storageCompositionActive          byte = 2
	storageCompositionCorePublication byte = 3
	storageCompositionBase            byte = 1
	storageCompositionEnrollment      byte = 2
	storageCompositionPlan            byte = 3
	storageCompositionCompletion      byte = 4
	storageCompositionStamp           byte = 5
	storageCompositionContinuation    byte = 6
)

// Declaration order is the unchanged v5 four-array wire order. Binary append
// lists below use P,C,S,F order, so S and F are intentionally transposed there.
type storageCompositionRows struct {
	LineagePlans            []storageOwnerV5LineagePlan
	LineageCompletions      []storageOwnerV5LineageCompletion
	LineageContinuations    []storageOwnerV5FromContinuation
	LineageStampCompletions []storageOwnerV5StampCompletionRow
}

type storageCompositionInitial struct {
	Mode        byte // 1: positively absent/create; 2: exact original same-bytes CAS
	ExpectedOld storageOwnerPersistentObject
}

type storageCompositionLineage struct {
	Rows    storageCompositionRows
	Order   [4][]storagebudget.ID       // immutable append order: P,C,S,F
	Initial []storageCompositionInitial // P append order, exactly 36 bytes each
}

func storageCompositionEmptyLineage() storageCompositionLineage {
	return storageCompositionLineage{Rows: storageCompositionRows{
		LineagePlans:            []storageOwnerV5LineagePlan{},
		LineageCompletions:      []storageOwnerV5LineageCompletion{},
		LineageContinuations:    []storageOwnerV5FromContinuation{},
		LineageStampCompletions: []storageOwnerV5StampCompletionRow{},
	}, Order: [4][]storagebudget.ID{{}, {}, {}, {}}, Initial: []storageCompositionInitial{}}
}

func (r storageCompositionRows) comparison() storageOwnerV5Catalog {
	// Only row helpers may consume this projection. Never call old(), validate(),
	// v5 frame encoding or its source validator with an E1 graph.
	return storageOwnerV5Catalog{LineagePlans: r.LineagePlans,
		LineageCompletions:      r.LineageCompletions,
		LineageContinuations:    r.LineageContinuations,
		LineageStampCompletions: r.LineageStampCompletions}
}

func storageCompositionRowsPreflight(raw []byte) bool {
	if len(raw) > 7*1048576+90 || !utf8.Valid(raw) {
		return false
	}
	p := storageOwnerV5Scanner{old: storageOwnerCatalogScanner{storageOwnerJSONPreflight: storageOwnerJSONPreflight{raw: raw}}}
	if !p.old.take('{') {
		return false
	}
	for i, field := range []struct {
		name string
		typ  reflect.Type
	}{
		{"LineagePlans", reflect.TypeOf([]storageOwnerV5LineagePlan{})},
		{"LineageCompletions", reflect.TypeOf([]storageOwnerV5LineageCompletion{})},
		{"LineageContinuations", reflect.TypeOf([]storageOwnerV5FromContinuation{})},
		{"LineageStampCompletions", reflect.TypeOf([]storageOwnerV5StampCompletionRow{})},
	} {
		if i > 0 && !p.old.take(',') {
			return false
		}
		if !p.old.literal(`"`+field.name+`":`) || !p.value(field.typ, field.name) {
			return false
		}
	}
	return p.old.take('}') && p.old.pos == len(raw)
}

func (l storageCompositionLineage) ids(group int) []storagebudget.ID {
	var ids []storagebudget.ID
	switch group {
	case 0:
		for _, r := range l.Rows.LineagePlans {
			ids = append(ids, r.PlanID)
		}
	case 1:
		for _, r := range l.Rows.LineageCompletions {
			ids = append(ids, r.PlanID)
		}
	case 2:
		for _, r := range l.Rows.LineageStampCompletions {
			ids = append(ids, r.PlanID)
		}
	case 3:
		for _, r := range l.Rows.LineageContinuations {
			ids = append(ids, r.PlanID)
		}
	}
	return ids
}

func (l storageCompositionLineage) valid() bool {
	if !storageOwnerV5RowChain(l.Rows.comparison()) || len(l.Initial) != len(l.Rows.LineagePlans) {
		return false
	}
	for group := range l.Order {
		ids := l.ids(group)
		if len(l.Order[group]) != len(ids) || len(ids) > 1024 {
			return false
		}
		remaining := make(map[storagebudget.ID]bool, len(ids))
		for _, id := range ids {
			remaining[id] = true
		}
		for _, id := range l.Order[group] {
			if !remaining[id] {
				return false
			}
			delete(remaining, id)
		}
		if len(remaining) != 0 {
			return false
		}
	}
	for i, id := range l.Order[0] {
		var p *storageOwnerV5LineagePlan
		for j := range l.Rows.LineagePlans {
			if l.Rows.LineagePlans[j].PlanID == id {
				p = &l.Rows.LineagePlans[j]
				break
			}
		}
		if p == nil {
			return false
		}
		initial, err := parseIdentity(p.IdentityInitial)
		if err != nil {
			return false
		}
		if initial.From == "" {
			if len(p.IdentityFromCleared) != 0 {
				return false
			}
		} else {
			initial.From = ""
			if !bytes.Equal(initial.line(), p.IdentityFromCleared) {
				return false
			}
		}
		switch l.Initial[i].Mode {
		case 1:
			if l.Initial[i].ExpectedOld != (storageOwnerPersistentObject{}) {
				return false
			}
		case 2:
			if !l.Initial[i].ExpectedOld.valid(2) || !storageIdentitySameVolume(p.PrivateObject, l.Initial[i].ExpectedOld) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func (l storageCompositionLineage) encode() ([]byte, error) {
	if !l.valid() {
		return nil, storagebudget.ErrBounds
	}
	j, err := json.Marshal(l.Rows)
	if err != nil || !storageCompositionRowsPreflight(j) {
		return nil, storagebudget.ErrBounds
	}
	w := storageEnrollmentPeakWriter{max: storageCompositionLineageMax}
	w.u32(uint32(len(j)))
	w.put(j)
	for _, order := range l.Order {
		w.u16(uint16(len(order)))
	}
	for _, order := range l.Order {
		for _, id := range order {
			w.put(id[:])
		}
	}
	for _, initial := range l.Initial {
		w.u8(initial.Mode)
		w.put(storageEnrollmentPeakNativeBytes(initial.ExpectedOld))
	}
	if w.bad {
		return nil, storagebudget.ErrBounds
	}
	return w.raw, nil
}

func storageCompositionReadLineage(raw []byte) (storageCompositionLineage, error) {
	l := storageCompositionEmptyLineage()
	if len(raw) > storageCompositionLineageMax || len(raw) < 12 {
		return storageCompositionLineage{}, storagebudget.ErrBounds
	}
	r := storageEnrollmentPeakReader{raw: raw}
	n := r.u32()
	if n > uint32(len(raw)-12) {
		return storageCompositionLineage{}, storagebudget.ErrBounds
	}
	j := r.take(int(n))
	if !storageCompositionRowsPreflight(j) || json.Unmarshal(j, &l.Rows) != nil {
		return storageCompositionLineage{}, storagebudget.ErrBounds
	}
	canonical, err := json.Marshal(l.Rows)
	if err != nil || !bytes.Equal(canonical, j) {
		return storageCompositionLineage{}, storagebudget.ErrBounds
	}
	var count [4]uint16
	want := 0
	for i := range count {
		count[i] = r.u16()
		if count[i] > 1024 || int(count[i]) != len(l.ids(i)) {
			return storageCompositionLineage{}, storagebudget.ErrBounds
		}
		want += int(count[i]) * 16
	}
	want += int(count[0]) * 36
	if r.bad || len(raw)-r.pos != want {
		return storageCompositionLineage{}, storagebudget.ErrBounds
	}
	for i := range l.Order {
		l.Order[i] = make([]storagebudget.ID, int(count[i]))
		for j := range l.Order[i] {
			copy(l.Order[i][j][:], r.take(16))
		}
	}
	l.Initial = make([]storageCompositionInitial, int(count[0]))
	for i := range l.Initial {
		l.Initial[i] = storageCompositionInitial{Mode: r.u8(), ExpectedOld: r.native()}
	}
	if !r.done() || !l.valid() {
		return storageCompositionLineage{}, storagebudget.ErrBounds
	}
	return l, nil
}

// prefix reconstructs exact old bytes from immutable rows and chronological
// ID prefixes. Sorted PlanIDs are not assumed to append at the end of JSON.
func (l storageCompositionLineage) prefix(count [4]uint16) (storageCompositionLineage, error) {
	if !l.valid() {
		return storageCompositionLineage{}, storagebudget.ErrBounds
	}
	out := storageCompositionEmptyLineage()
	var selected [4]map[storagebudget.ID]bool
	for group := range count {
		if int(count[group]) > len(l.Order[group]) {
			return storageCompositionLineage{}, storagebudget.ErrBounds
		}
		out.Order[group] = append([]storagebudget.ID{}, l.Order[group][:int(count[group])]...)
		selected[group] = map[storagebudget.ID]bool{}
		for _, id := range out.Order[group] {
			selected[group][id] = true
		}
	}
	for _, row := range l.Rows.LineagePlans {
		if selected[0][row.PlanID] {
			out.Rows.LineagePlans = append(out.Rows.LineagePlans, row)
		}
	}
	for _, row := range l.Rows.LineageCompletions {
		if selected[1][row.PlanID] {
			out.Rows.LineageCompletions = append(out.Rows.LineageCompletions, row)
		}
	}
	for _, row := range l.Rows.LineageStampCompletions {
		if selected[2][row.PlanID] {
			out.Rows.LineageStampCompletions = append(out.Rows.LineageStampCompletions, row)
		}
	}
	for _, row := range l.Rows.LineageContinuations {
		if selected[3][row.PlanID] {
			out.Rows.LineageContinuations = append(out.Rows.LineageContinuations, row)
		}
	}
	out.Initial = append([]storageCompositionInitial{}, l.Initial[:int(count[0])]...)
	if !out.valid() {
		return storageCompositionLineage{}, storagebudget.ErrBounds
	}
	return out, nil
}

func (l storageCompositionLineage) closureSize() (int, error) {
	if !l.valid() {
		return 0, storagebudget.ErrBounds
	}
	r := l.Rows
	r.LineageCompletions = append([]storageOwnerV5LineageCompletion{}, r.LineageCompletions...)
	r.LineageStampCompletions = append([]storageOwnerV5StampCompletionRow{}, r.LineageStampCompletions...)
	r.LineageContinuations = append([]storageOwnerV5FromContinuation{}, r.LineageContinuations...)
	var present [4]map[storagebudget.ID]bool
	for group := range present {
		present[group] = map[storagebudget.ID]bool{}
		for _, id := range l.ids(group) {
			present[group][id] = true
		}
	}
	for _, p := range r.LineagePlans {
		if !present[1][p.PlanID] {
			r.LineageCompletions = append(r.LineageCompletions, storageOwnerV5CompletionShadow(p))
		}
		if len(p.IdentityFromCleared) > 0 && !present[2][p.PlanID] {
			r.LineageStampCompletions = append(r.LineageStampCompletions, storageOwnerV5StampShadow(p))
		}
		if len(p.IdentityFromCleared) > 0 && !present[3][p.PlanID] {
			r.LineageContinuations = append(r.LineageContinuations, storageOwnerV5ContinuationShadow(p))
		}
	}
	sort.Slice(r.LineageCompletions, func(i, j int) bool {
		return bytes.Compare(r.LineageCompletions[i].PlanID[:], r.LineageCompletions[j].PlanID[:]) < 0
	})
	sort.Slice(r.LineageStampCompletions, func(i, j int) bool {
		return bytes.Compare(r.LineageStampCompletions[i].PlanID[:], r.LineageStampCompletions[j].PlanID[:]) < 0
	})
	sort.Slice(r.LineageContinuations, func(i, j int) bool {
		return bytes.Compare(r.LineageContinuations[i].PlanID[:], r.LineageContinuations[j].PlanID[:]) < 0
	})
	if !storageOwnerV5RowBounds(r.comparison()) {
		return 0, storagebudget.ErrBounds
	}
	j, err := json.Marshal(r)
	if err != nil || !storageCompositionRowsPreflight(j) {
		return 0, storagebudget.ErrBounds
	}
	n := 4 + len(j) + 8 + 36*len(r.LineagePlans) + 16*(len(r.LineagePlans)+len(r.LineageCompletions)+len(r.LineageStampCompletions)+len(r.LineageContinuations))
	if n > storageCompositionLineageMax {
		return 0, storagebudget.ErrBounds
	}
	return n, nil
}

func storageCompositionInitialBinding(p storageOwnerV5LineagePlan, initial storageCompositionInitial) (storagebudget.Digest, error) {
	d, err := storageOwnerV5PlanDigest(p)
	if err != nil || initial.Mode != 2 || !initial.ExpectedOld.valid(2) {
		return storagebudget.Digest{}, storagebudget.ErrBounds
	}
	raw := append([]byte{}, p.PlanID[:]...)
	raw = append(raw, d[:]...)
	raw = append(raw, initial.Mode)
	raw = append(raw, storageEnrollmentPeakNativeBytes(initial.ExpectedOld)...)
	bytesDigest := sha256.Sum256(p.IdentityInitial)
	raw = append(raw, bytesDigest[:]...)
	return storageEnrollmentPeakHash("desk-lineage-original-initial-v1", raw), nil
}

// validateGraph consumes an independently strict-decoded original graph. For an
// E1 marker that graph comes from its selected original evidence, not from a
// v5.old() cast or a source path chosen by the serialized plan. Actual project,
// private/config/provider custody remains a separate live issuer requirement.
func (l storageCompositionLineage) validateGraph(c storageOwnerCatalogV2, record storageOwnerRecordV2) error {
	if !l.valid() || c.Installation != record.Installation || c.QBinding != record.QBinding {
		return storagebudget.ErrBounds
	}
	for _, p := range l.Rows.LineagePlans {
		if p.Installation != c.Installation || p.QBinding != c.QBinding || !p.ProjectObject.valid(1) || !p.PrivateObject.valid(1) || p.ConfigObject != record.Config || !p.ConfigObject.valid(1) || p.ProviderRelationDigest == (storagebudget.Digest{}) {
			return storagebudget.ErrBounds
		}
		initial, err := parseIdentity(p.IdentityInitial)
		if err != nil {
			return storagebudget.ErrBounds
		}
		var w *storageOwnerWorkspace
		for i := range c.Workspaces {
			if c.Workspaces[i].Definition.ID == p.Workspace.ID {
				w = &c.Workspaces[i]
				break
			}
		}
		if w == nil || w.OwnerKind != 3 || p.Workspace.Generation != w.Definition.Generation || !bytes.Equal(w.OwnerKey, p.AccountingOwnerKey) || !bytes.Equal(w.HistoryIdentity, p.HistoryIdentity) {
			return storagebudget.ErrBounds
		}
		wd, err := storageOwnerV4WorkspaceDigest(*w)
		if err != nil || wd != p.Workspace.Digest {
			return storagebudget.ErrBounds
		}
		var home *storageOwnerHomeRelation
		switch c.OriginalOwner.Kind {
		case 1:
			if (!storageOwnerHex(string(w.OwnerKey), 64) && string(w.OwnerKey) != initial.ID) || w.Definition.ID != storageDerivedID("desk-storage-workspace-v2", c.Installation, w.OwnerKey) {
				return storagebudget.ErrBounds
			}
		case 2:
			for i := range c.OriginalOwner.Entries {
				e := &c.OriginalOwner.Entries[i]
				if e.Kind == 1 && e.RunnerWorkspace == string(w.OwnerKey) && bytes.Equal(e.HistoryIdentity, w.HistoryIdentity) && filepath.Join(string(c.OriginalOwner.HomePath), string(e.Folder)) == initial.Path {
					if home != nil {
						return storagebudget.ErrBounds
					}
					home = e
				}
			}
			if home == nil {
				return storagebudget.ErrBounds
			}
		default:
			return storagebudget.ErrBounds
		}
		var private *storageOwnerRoot
		for i := range c.Roots {
			if c.Roots[i].Definition.ID == p.PrivateRoot.ID {
				private = &c.Roots[i]
				break
			}
		}
		if private == nil || private.Category != storagebudget.Control || private.Definition.Generation != p.PrivateRoot.Generation || private.Definition.Identity != p.PrivateRoot.Identity || int(private.DirectoryObject) >= len(c.Objects) || c.Objects[private.DirectoryObject].Persistent != p.PrivateObject {
			return storagebudget.ErrBounds
		}
		if private.Source.Kind == 8 {
			if len(private.Source.Path) != 0 {
				return storagebudget.ErrBounds
			}
		} else if !bytes.Equal(private.Source.Path, []byte(filepath.Join(initial.Path, ".desk-private"))) {
			return storagebudget.ErrBounds
		}
		linked := false
		for _, id := range private.References {
			linked = linked || id == w.Definition.ID
		}
		if !linked {
			return storagebudget.ErrBounds
		}
		if home == nil {
			if private.Role != 6 || private.Source.Kind != 6 && private.Source.Kind != 8 || !bytes.Equal(private.Source.OwnerKey, w.OwnerKey) {
				return storagebudget.ErrBounds
			}
		} else {
			if private.Role != 3 || private.Source.Kind != 3 && private.Source.Kind != 8 {
				return storagebudget.ErrBounds
			}
			want := []byte(home.DeskID)
			if private.Source.Kind == 8 {
				want = w.OwnerKey
			}
			if !bytes.Equal(private.Source.OwnerKey, want) {
				return storagebudget.ErrBounds
			}
		}
		var sd storagebudget.Digest
		if private.Source.Kind == 8 {
			if string(private.Source.Relative) != ".desk-private" || private.Source.ParentRoot == (storagebudget.ID{}) || private.DomainObject != storageOwnerNoObject || private.WorkObject != storageOwnerNoObject {
				return storagebudget.ErrBounds
			}
			parentFound := false
			for _, parent := range c.Roots {
				if parent.Definition.ID != private.Source.ParentRoot || parent.Role != 7 || parent.Source.Kind != 7 {
					continue
				}
				b, e := storageEnrollmentPeakDecodeSource(parent.Source.Relative)
				if e != nil || b.Project != p.ProjectObject || b.Workspace != w.Definition.ID || !bytes.Equal(b.OwnerKey, w.OwnerKey) || !bytes.Equal(b.HistoryIdentity, w.HistoryIdentity) {
					return storagebudget.ErrBounds
				}
				parentFound = true
			}
			if !parentFound {
				return storagebudget.ErrBounds
			}
			sd, err = storageCompositionPrivateSourceDigest(c, *private)
		} else {
			if private.Source.ParentRoot != (storagebudget.ID{}) || len(private.Source.Relative) != 0 || private.Source.ControlKind != 0 || private.Source.ControlIndex != storageOwnerNoControl || private.Source.ControlSelection != 0 {
				return storagebudget.ErrBounds
			}
			sd, err = c.sourceDigest(*private)
		}
		if err != nil || sd != p.PrivateRoot.SourceDigest {
			return storagebudget.ErrBounds
		}
		var provider *storageOwnerRoot
		for i := range c.Roots {
			if c.Roots[i].Role != 5 {
				continue
			}
			for _, id := range c.Roots[i].References {
				if id == w.Definition.ID {
					if provider != nil {
						return storagebudget.ErrBounds
					}
					provider = &c.Roots[i]
				}
			}
		}
		pd, err := storageOwnerV4ProviderDigest(c, *w, provider)
		if err != nil || pd != p.ProviderRelationDigest {
			return storagebudget.ErrBounds
		}
	}
	return nil
}

type storageCompositionBank struct {
	Sequence                                       uint64
	CoreCodec, Publication, Transition, BeforeBank byte
	TransitionID                                   storagebudget.ID
	Previous                                       storageCompositionChecksum
	CoreFull                                       storageCompositionCoreDigest
	LineageDigest                                  storagebudget.Digest
	FirstBase                                      storagebudget.Digest
	BeforePeer                                     [65]byte
	Core                                           []byte // exact reconstructed full8MiB core; never physical bytes
	Lineage                                        storageCompositionLineage
	// Nil is the unchanged V1 frame. A V2 frame retains exact current rows.
	Moves        []storageMoveCurrent
	MoveOperands storageMoveOperandTable
	Raw          []byte
}

func storageCompositionCompactCore(raw []byte) ([]byte, error) {
	if len(raw) != storageOwnerCatalogBytes {
		return nil, storagebudget.ErrBounds
	}
	n := storageEnrollmentPeakU32(raw[24:28])
	if n > storageOwnerCatalogBytes-64 || !storageEnrollmentPeakZero(raw[64+int(n):]) {
		return nil, storagebudget.ErrBounds
	}
	return raw[:64+int(n)], nil
}

func (v storageCompositionBank) validateCore(record storageOwnerRecordV2, claim storageOwnerClaimV2) error {
	if err := storageMoveValidateCurrents(v.Moves, v.Lineage, record); err != nil {
		return err
	}
	if v.Transition == storageCompositionMove && v.Moves == nil {
		return storagebudget.ErrBounds
	}
	if len(v.Core) != storageOwnerCatalogBytes || storageCompositionCoreDigest(sha256.Sum256(v.Core)) != v.CoreFull || v.Sequence == 0 || v.BeforeBank > 1 || v.Publication < 1 || v.Publication > 3 || v.Transition < 1 || v.Transition > storageCompositionMove || v.TransitionID == (storagebudget.ID{}) || v.Previous == (storageCompositionChecksum{}) || v.BeforePeer[0] > 1 || storageEnrollmentPeakZero(v.BeforePeer[1:33]) {
		return storagebudget.ErrBounds
	}
	if v.Transition == storageCompositionBase && v.FirstBase == (storagebudget.Digest{}) || v.Transition == storageCompositionEnrollment && v.FirstBase != (storagebudget.Digest{}) {
		return storagebudget.ErrBounds
	}
	if v.CoreCodec == 1 {
		c, err := storageOwnerDecodeCatalogV3(v.Core, record, claim)
		if err != nil || c.Phase != "active" || v.Transition == storageCompositionEnrollment || v.Publication == storageCompositionCorePublication || v.FirstBase == (storagebudget.Digest{}) {
			return storagebudget.ErrBounds
		}
		var peerDigest storageCompositionCoreDigest
		copy(peerDigest[:], v.BeforePeer[1:33])
		var peerPrevious storagebudget.Digest
		copy(peerPrevious[:], v.BeforePeer[33:65])
		if v.BeforePeer[0] == 0 {
			if peerDigest != v.CoreFull || peerPrevious != c.PreviousCatalogDigest {
				return storagebudget.ErrBounds
			}
		} else {
			if c.Sequence <= 1 {
				return storagebudget.ErrBounds
			}
			peer := c
			peer.Sequence--
			peer.Phase = "prepared"
			peer.PreviousCatalogDigest = peerPrevious
			peerRaw, e := storageOwnerEncodeCatalogV3(peer, record, claim)
			if e != nil || storageCompositionCoreDigest(sha256.Sum256(peerRaw)) != peerDigest {
				return storagebudget.ErrBounds
			}
			if _, e = storageOwnerDecodeCatalogPairV3(peerRaw, v.Core, record, claim); e != nil {
				return e
			}
		}
		return v.Lineage.validateGraph(storageOwnerCatalogV2(c), record)
	}
	if v.CoreCodec != 2 {
		return storagebudget.ErrBounds
	}
	e, err := storageEnrollmentPeakDecodeEnvelopeVariant(v.Core, record, claim, true, true)
	if err != nil || v.BeforePeer[0] != 0 {
		return storagebudget.ErrBounds
	}
	if e.Snapshot != nil && e.Snapshot.Encoding == 4 {
		pair, e0 := e.Snapshot.Physical.restore(e.Snapshot.Before, v.Lineage, record, claim)
		if e0 != nil {
			return storagebudget.ErrBounds
		}
		before, e0 := storageCompositionReadPair(pair[0], pair[1], record, claim)
		if e0 != nil || before.Selected != e.Scope[322] || storageCompositionDuplicateCorePeer(e.Snapshot.Before, record, claim) != v.BeforePeer {
			return storagebudget.ErrBounds
		}
	}
	completed := e.Kind == 2 && e.Marker != nil && e.Marker.Phase == 3
	if v.Transition != storageCompositionEnrollment {
		if !completed || v.Publication == storageCompositionCorePublication {
			return storagebudget.ErrBounds
		}
		return nil
	}
	if e.Snapshot != nil && e.Snapshot.Encoding != 4 {
		return storagebudget.ErrBounds
	}
	var bridge storagebudget.ID
	copy(bridge[:], e.Scope[24:40])
	if v.TransitionID != bridge || v.BeforeBank != e.Scope[322] || completed && v.Publication != storageCompositionActive || !completed && v.Publication != storageCompositionCorePublication {
		return storagebudget.ErrBounds
	}
	return nil
}

func (v storageCompositionBank) encode(record storageOwnerRecordV2, claim storageOwnerClaimV2) ([]byte, error) {
	if err := v.validateCore(record, claim); err != nil {
		return nil, err
	}
	core, err := storageCompositionCompactCore(v.Core)
	if err != nil {
		return nil, err
	}
	l, err := v.Lineage.encode()
	if err != nil || sha256.Sum256(l) != v.LineageDigest {
		return nil, storagebudget.ErrBounds
	}
	_, moveIndex, moves, err := storageMoveBankOperands(v)
	if err != nil {
		return nil, err
	}
	room, err := v.Lineage.closureSize()
	if err != nil || 64+storageCompositionPrefixBytes+len(core)+room+len(moves) > storageOwnerCatalogBytes {
		return nil, storagebudget.ErrBounds
	}
	prefix := make([]byte, storageCompositionPrefixBytes)
	version := uint16(1)
	if v.Moves != nil {
		version = 2
		storageEnrollmentPeakPut32(prefix[225:229], uint32(len(moves)))
		storageEnrollmentPeakPut16(prefix[229:231], moveIndex)
	}
	storageEnrollmentPeakPut16(prefix[:2], version)
	prefix[2], prefix[3], prefix[4], prefix[5] = v.CoreCodec, v.Publication, v.Transition, v.BeforeBank
	storageEnrollmentPeakPut32(prefix[8:12], uint32(len(core)))
	storageEnrollmentPeakPut32(prefix[12:16], uint32(len(l)))
	copy(prefix[16:32], v.TransitionID[:])
	copy(prefix[32:64], v.Previous[:])
	copy(prefix[64:96], v.CoreFull[:])
	copy(prefix[96:128], v.LineageDigest[:])
	copy(prefix[128:160], v.FirstBase[:])
	copy(prefix[160:225], v.BeforePeer[:])
	payload := append(prefix, core...)
	payload = append(payload, l...)
	payload = append(payload, moves...)
	return storageEnrollmentPeakFrame("DSKONL1\x00", payload, v.Sequence, true)
}

func storageCompositionDecode(raw []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageCompositionBank, error) {
	var v storageCompositionBank
	payload, sequence, err := storageEnrollmentPeakPayload(raw, "DSKONL1\x00", true)
	if err != nil || len(payload) < storageCompositionPrefixBytes || !storageEnrollmentPeakZero(payload[6:8]) {
		return v, storagebudget.ErrBounds
	}
	version := storageEnrollmentPeakU16(payload[:2])
	moveN := uint64(0)
	switch version {
	case 1:
		if !storageEnrollmentPeakZero(payload[225:256]) {
			return v, storagebudget.ErrBounds
		}
	case 2:
		moveN = uint64(storageEnrollmentPeakU32(payload[225:229]))
		if moveN < 4 || moveN > storageOwnerCatalogBytes-64 || !storageEnrollmentPeakZero(payload[231:256]) {
			return v, storagebudget.ErrBounds
		}
	default:
		return v, storagebudget.ErrBounds
	}
	coreN, lineageN := uint64(storageEnrollmentPeakU32(payload[8:12])), uint64(storageEnrollmentPeakU32(payload[12:16]))
	if coreN < 64 || coreN > storageOwnerCatalogBytes || lineageN > storageCompositionLineageMax || uint64(len(payload)) != storageCompositionPrefixBytes+coreN+lineageN+moveN {
		return v, storagebudget.ErrBounds
	}
	v.Sequence, v.CoreCodec, v.Publication, v.Transition, v.BeforeBank = sequence, payload[2], payload[3], payload[4], payload[5]
	copy(v.TransitionID[:], payload[16:32])
	copy(v.Previous[:], payload[32:64])
	copy(v.CoreFull[:], payload[64:96])
	copy(v.LineageDigest[:], payload[96:128])
	copy(v.FirstBase[:], payload[128:160])
	copy(v.BeforePeer[:], payload[160:225])
	core := payload[256 : 256+int(coreN)]
	if uint64(storageEnrollmentPeakU32(core[24:28]))+64 != coreN {
		return storageCompositionBank{}, storagebudget.ErrBounds
	}
	v.Core = make([]byte, storageOwnerCatalogBytes)
	copy(v.Core, core)
	l := payload[256+int(coreN) : 256+int(coreN)+int(lineageN)]
	if version == 2 {
		v.MoveOperands, err = storageMoveDecodeOperandTable(payload[256+int(coreN)+int(lineageN):])
		if err == nil {
			v.Moves, err = v.MoveOperands.selectSet(storageEnrollmentPeakU16(payload[229:231]))
		}
		if err != nil {
			return storageCompositionBank{}, err
		}
	}
	if sha256.Sum256(l) != v.LineageDigest {
		return storageCompositionBank{}, storagebudget.ErrBounds
	}
	v.Lineage, err = storageCompositionReadLineage(l)
	if err != nil || v.validateCore(record, claim) != nil {
		return storageCompositionBank{}, storagebudget.ErrBounds
	}
	room, err := v.Lineage.closureSize()
	if err != nil || 64+256+int(coreN)+room+int(moveN) > storageOwnerCatalogBytes {
		return storageCompositionBank{}, storagebudget.ErrBounds
	}
	v.Raw = append([]byte{}, raw...)
	return v, nil
}

// The table is the existing Scope table, not an effect plan. It returns only
// the exact wrapper shape for that already named catalog publication. A caller
// must still consume the original group under original custody before writing.
type storageCompositionPublicationShape struct {
	Group, Completion                                uint16
	Bank, CoreKind, MarkerPhase, Offset, Publication byte
	Duplicate                                        bool
}

func storageCompositionPublication(path, before byte, group uint16) (storageCompositionPublicationShape, error) {
	if before > 1 {
		return storageCompositionPublicationShape{}, storagebudget.ErrBounds
	}
	var rows []storageCompositionPublicationShape
	switch path {
	case 1:
		rows = []storageCompositionPublicationShape{{60, 80, 1 - before, 0, 255, 1, 3, false}, {61, 81, before, 0, 255, 1, 3, true}, {66, 86, before, 1, 255, 2, 3, false}, {11, 46, 1 - before, 2, 1, 3, 3, false}, {12, 47, before, 2, 1, 3, 3, true}, {13, 48, before, 2, 2, 4, 3, false}, {14, 49, 1 - before, 2, 3, 5, 2, false}, {15, 50, before, 2, 3, 5, 2, true}}
	case 2:
		rows = []storageCompositionPublicationShape{{9, 44, 1 - before, 0, 255, 1, 3, false}, {10, 45, before, 0, 255, 1, 3, true}, {11, 46, before, 2, 1, 2, 3, false}, {12, 47, 1 - before, 2, 1, 2, 3, true}, {13, 48, 1 - before, 2, 2, 3, 3, false}, {14, 49, before, 2, 3, 4, 2, false}, {15, 50, 1 - before, 2, 3, 4, 2, true}}
	case 3:
		rows = []storageCompositionPublicationShape{{9, 44, 1 - before, 0, 255, 1, 3, false}, {10, 45, before, 0, 255, 1, 3, true}, {67, 87, before, 2, 0, 2, 3, false}, {68, 88, 1 - before, 2, 0, 2, 3, true}, {11, 46, 1 - before, 2, 1, 3, 3, false}, {12, 47, before, 2, 1, 3, 3, true}, {13, 48, before, 2, 2, 4, 3, false}, {14, 49, 1 - before, 2, 3, 5, 2, false}, {15, 50, before, 2, 3, 5, 2, true}}
	default:
		return storageCompositionPublicationShape{}, storagebudget.ErrBounds
	}
	for _, row := range rows {
		if row.Group == group {
			return row, nil
		}
	}
	return storageCompositionPublicationShape{}, storagebudget.ErrBounds
}

// completedPair is only a byte/phase predicate. It does not establish the
// required two-original-bank persistence, native source/Q join or live gate.
func storageCompositionCompletedPair(a, b storageCompositionBank, record storageOwnerRecordV2, claim storageOwnerClaimV2) (byte, error) {
	if a.validateCore(record, claim) != nil || b.validateCore(record, claim) != nil || a.Transition != storageCompositionEnrollment || a.Publication != storageCompositionActive || len(a.Raw) != storageOwnerCatalogBytes || !bytes.Equal(a.Raw, b.Raw) {
		return 0, storagebudget.ErrBounds
	}
	e, err := storageEnrollmentPeakDecodeEnvelopeVariant(a.Core, record, claim, true, true)
	if err != nil || e.Kind != 2 || e.Marker == nil || e.Marker.Phase != 3 {
		return 0, storagebudget.ErrBounds
	}
	shape, err := storageCompositionPublication(e.Scope.path(), e.Scope[322], 14)
	if err != nil {
		return 0, err
	}
	return shape.Bank, nil
}

func storageCompositionSequence(base uint64, offset byte) (uint64, error) {
	if base == 0 || offset == 0 || base > math.MaxUint64-uint64(offset) {
		return 0, storagebudget.ErrBounds
	}
	return base + uint64(offset), nil
}

func storageCompositionChecksumOf(raw []byte) (storageCompositionChecksum, error) {
	var d storageCompositionChecksum
	if len(raw) != storageOwnerCatalogBytes {
		return d, storagebudget.ErrBounds
	}
	copy(d[:], raw[32:64])
	return d, nil
}

func storageCompositionFrozenCore(a, b storageCompositionBank) bool {
	return a.CoreCodec == b.CoreCodec && a.BeforeBank == b.BeforeBank && a.CoreFull == b.CoreFull && a.FirstBase == b.FirstBase && a.BeforePeer == b.BeforePeer && bytes.Equal(a.Core, b.Core)
}

func storageCompositionSameLineage(a, b storageCompositionBank) bool {
	return a.LineageDigest == b.LineageDigest && reflect.DeepEqual(a.Lineage, b.Lineage)
}

func storageCompositionLineageAppend(low, high storageCompositionBank) bool {
	if !storageCompositionFrozenCore(low, high) || !low.Lineage.valid() || !high.Lineage.valid() || !storageOwnerV5Retains(low.Lineage.Rows.comparison(), high.Lineage.Rows.comparison()) {
		return false
	}
	group := -1
	switch high.Transition {
	case storageCompositionPlan:
		group = 0
	case storageCompositionCompletion:
		group = 1
	case storageCompositionStamp:
		group = 2
	case storageCompositionContinuation:
		group = 3
	default:
		return false
	}
	for i := range low.Lineage.Order {
		a, b := low.Lineage.Order[i], high.Lineage.Order[i]
		added := 0
		if i == group {
			added = 1
		}
		if len(b) != len(a)+added || !reflect.DeepEqual(a, b[:len(a)]) && !(len(a) == 0 && len(b[:len(a)]) == 0) {
			return false
		}
		if added != 0 && b[len(a)] != high.TransitionID {
			return false
		}
	}
	oldN := len(low.Lineage.Initial)
	added := 0
	if group == 0 {
		added = 1
	}
	if len(high.Lineage.Initial) != oldN+added || !reflect.DeepEqual(low.Lineage.Initial, high.Lineage.Initial[:oldN]) && oldN != 0 {
		return false
	}
	return true
}

// finalize is exact physical Prepared->Active comparison. It does not prove
// that either file or its parent has actually reached persistent storage.
func storageCompositionFinalize(low, high storageCompositionBank) bool {
	if low.Publication != storageCompositionPrepared || high.Publication != storageCompositionActive || low.Transition == storageCompositionEnrollment || low.Transition != high.Transition || low.TransitionID != high.TransitionID || !storageCompositionFrozenCore(low, high) || !storageCompositionSameLineage(low, high) {
		return false
	}
	return true
}

// corePublication compares one existing Scope primary edge. A duplicate must
// instead be identical in BOTH complete physical banks. This binds R1's final
// Publication2 to its real Definitions->Completed core edge, never to a retag.
func storageCompositionCoreEdge(low, high storageCompositionBank, highBank byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) bool {
	if highBank > 1 || high.Transition != storageCompositionEnrollment || !storageCompositionSameLineage(low, high) || high.CoreCodec != 2 {
		return false
	}
	e, err := storageEnrollmentPeakDecodeEnvelopeVariant(high.Core, record, claim, true, true)
	if err != nil {
		return false
	}
	var group uint16
	if e.Kind == 0 {
		if low.Publication != storageCompositionActive || e.Snapshot == nil || e.Snapshot.Encoding != 4 || !bytes.Equal(e.Snapshot.Before, low.Core) {
			return false
		}
		before, e0 := e.Snapshot.Physical.restore(e.Snapshot.Before, high.Lineage, record, claim)
		if e0 != nil || e.Scope[322] != 1-highBank || !bytes.Equal(before[e.Scope[322]], low.Raw) {
			return false
		}
		group = 9
		if e.Scope.path() == 1 {
			group = 60
		}
	} else {
		if low.Transition != storageCompositionEnrollment || low.BeforeBank != high.BeforeBank || low.BeforePeer != high.BeforePeer || low.FirstBase != high.FirstBase {
			return false
		}
		prior, e0 := storageEnrollmentPeakDecodeEnvelopeVariant(low.Core, record, claim, true, true)
		if e0 != nil {
			return false
		}
		if e.Kind == 1 {
			group = 66
		} else if e.Marker != nil {
			switch e.Marker.Phase {
			case 0:
				group = 67
			case 1:
				group = 11
			case 2:
				group = 13
			case 3:
				group = 14
			default:
				return false
			}
		} else {
			return false
		}
		if group == 14 && (prior.Kind != 2 || prior.Marker == nil || prior.Marker.Phase != 2 || low.Publication != storageCompositionCorePublication || high.Publication != storageCompositionActive) {
			return false
		}
	}
	shape, err := storageCompositionPublication(e.Scope.path(), e.Scope[322], group)
	if err != nil || shape.Bank != highBank || shape.Publication != high.Publication || shape.CoreKind != e.Kind {
		return false
	}
	if e.Marker != nil && shape.MarkerPhase != e.Marker.Phase {
		return false
	}
	coreRaw := [2][]byte{low.Core, low.Core}
	coreRaw[highBank] = high.Core
	if _, selected, e0 := storageEnrollmentPeakDecodePairVariant(coreRaw[0], coreRaw[1], record, claim, true); e0 != nil || selected != int(highBank) {
		return false
	}
	return true
}

type storageCompositionPair struct {
	Banks      [2]storageCompositionBank
	Selected   byte
	Duplicated bool
	// RecoveryOnly includes exact final C/D. Even a completed byte-pair remains
	// provisional until original persistence/native/source/Q gates complete.
	RecoveryOnly bool
}

func storageCompositionReadPair(a, b []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageCompositionPair, error) {
	var out storageCompositionPair
	for i, raw := range [2][]byte{a, b} {
		v, err := storageCompositionDecode(raw, record, claim)
		if err != nil {
			return storageCompositionPair{}, err
		}
		out.Banks[i] = v
	}
	x, y := out.Banks[0], out.Banks[1]
	if x.Sequence == y.Sequence {
		if !bytes.Equal(a, b) {
			return storageCompositionPair{}, storagebudget.ErrBounds
		}
		out.Duplicated = true
		if x.Transition == storageCompositionEnrollment {
			if x.Publication == storageCompositionActive {
				selected, err := storageCompositionCompletedPair(x, y, record, claim)
				if err != nil {
					return storageCompositionPair{}, err
				}
				out.Selected = selected
			} else {
				if _, _, err := storageEnrollmentPeakDecodePairVariant(x.Core, y.Core, record, claim, true); err != nil {
					return storageCompositionPair{}, err
				}
				out.RecoveryOnly = true
			}
		} else {
			out.RecoveryOnly = x.Publication != storageCompositionActive
		}
		return out, nil
	}
	lowBank, highBank := byte(0), byte(1)
	if x.Sequence > y.Sequence {
		lowBank, highBank = 1, 0
	}
	low, high := out.Banks[lowBank], out.Banks[highBank]
	previous, err := storageCompositionChecksumOf(low.Raw)
	if err != nil || low.Sequence == math.MaxUint64 || high.Sequence != low.Sequence+1 || high.Previous != previous {
		return storageCompositionPair{}, storagebudget.ErrBounds
	}
	legal := storageMoveBankEdge(low, high)
	if high.Transition != storageCompositionMove && !storageMoveSameCurrents(low.Moves, high.Moves) {
		return storageCompositionPair{}, storagebudget.ErrBounds
	}
	if high.Transition != storageCompositionMove {
		legal = storageCompositionFinalize(low, high)
	}
	if high.Transition != storageCompositionMove && low.Publication == storageCompositionActive && high.Publication == storageCompositionPrepared && storageCompositionLineageAppend(low, high) {
		legal = true
	}
	if high.Transition == storageCompositionEnrollment && storageCompositionCoreEdge(low, high, highBank, record, claim) {
		legal = true
	}
	if !legal {
		return storageCompositionPair{}, storagebudget.ErrBounds
	}
	out.Selected = highBank
	out.RecoveryOnly = high.Publication != storageCompositionActive || high.Transition == storageCompositionEnrollment
	return out, nil
}

func (v storageCompositionBank) validateEvidence(raw []byte, ready storagebudget.Digest, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageEnrollmentPeakEvidence, error) {
	if v.CoreCodec != 2 || v.validateCore(record, claim) != nil {
		return storageEnrollmentPeakEvidence{}, storagebudget.ErrBounds
	}
	outer, err := storageEnrollmentPeakDecodeEnvelopeVariant(v.Core, record, claim, true, true)
	if err != nil || outer.Marker == nil {
		return storageEnrollmentPeakEvidence{}, storagebudget.ErrBounds
	}
	e, err := storageEnrollmentPeakDecodeEvidenceWithLineage(raw, &v.Lineage, record, claim)
	if err != nil || storageEnrollmentPeakJoinMarker(outer, e, ready) != nil || v.Lineage.validateGraph(e.Inner.Catalog, record) != nil {
		return storageEnrollmentPeakEvidence{}, storagebudget.ErrBounds
	}
	if e.Version == 2 {
		before, e0 := e.compositionBefore(record, claim)
		if e0 != nil || storageCompositionDuplicateCorePeer(before.Banks[before.Selected].Core, record, claim) != v.BeforePeer {
			return storageEnrollmentPeakEvidence{}, storagebudget.ErrBounds
		}
	}
	return e, nil
}

// Each physical projection retains BOTH actual fixed prefixes, chronological
// cutoffs and full physical SHA values. It is comparison data, not proof that
// the two original files were persisted or held by the current invocation.
type storageCompositionProjection [storageCompositionProjectionBytes]byte

func storageCompositionDuplicateCorePeer(core []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) [65]byte {
	var peer [65]byte
	if len(core) != storageOwnerCatalogBytes {
		return peer
	}
	var previous storagebudget.Digest
	if bytes.Equal(core[:8], []byte("DSKOWN3\x00")) {
		c, err := storageOwnerDecodeCatalogV3(core, record, claim)
		if err != nil || c.Phase != "active" {
			return peer
		}
		previous = c.PreviousCatalogDigest
	} else {
		e, err := storageEnrollmentPeakDecodeEnvelopeVariant(core, record, claim, true, true)
		if err != nil || e.Marker == nil || e.Marker.Phase != 3 {
			return peer
		}
		previous = e.Marker.Previous
	}
	d := sha256.Sum256(core)
	copy(peer[1:33], d[:])
	copy(peer[33:65], previous[:])
	return peer
}

// shape is a bounded syntactic check only. Full byte reconstruction, row
// cutoffs, checksums and strict original pair selection are checked by restore.
func (p storageCompositionProjection) shape() bool {
	for bank := 0; bank < 2; bank++ {
		b := p[bank*320 : (bank+1)*320]
		if string(b[:8]) != "DSKONL1\x00" || storageEnrollmentPeakU32(b[8:12]) != 1 || storageEnrollmentPeakU32(b[12:16]) != 64 || storageEnrollmentPeakU16(b[64:66]) != 1 || storageEnrollmentPeakZero(p[648+bank*32:680+bank*32]) {
			return false
		}
	}
	for i := 0; i < 4; i++ {
		if storageEnrollmentPeakU16(p[640+2*i:642+2*i]) > 1024 {
			return false
		}
	}
	return true
}

func storageCompositionCaptureProjection(a, b []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageCompositionProjection, error) {
	var projection storageCompositionProjection
	pair, err := storageCompositionReadPair(a, b, record, claim)
	if err != nil || pair.RecoveryOnly || !storageCompositionFrozenCore(pair.Banks[0], pair.Banks[1]) || !storageCompositionSameLineage(pair.Banks[0], pair.Banks[1]) {
		return projection, storagebudget.ErrBounds
	}
	copy(projection[:320], a[:320])
	copy(projection[320:640], b[:320])
	for i, order := range pair.Banks[pair.Selected].Lineage.Order {
		storageEnrollmentPeakPut16(projection[640+2*i:642+2*i], uint16(len(order)))
	}
	for i, raw := range [2][]byte{a, b} {
		d := sha256.Sum256(raw)
		copy(projection[648+32*i:680+32*i], d[:])
	}
	return projection, nil
}

func (p storageCompositionProjection) restore(core []byte, retained storageCompositionLineage, record storageOwnerRecordV2, claim storageOwnerClaimV2) ([2][]byte, error) {
	var out [2][]byte
	if storageEnrollmentPeakZero(p[:]) {
		return out, storagebudget.ErrBounds
	}
	var count [4]uint16
	for i := range count {
		count[i] = storageEnrollmentPeakU16(p[640+2*i : 642+2*i])
	}
	old, err := retained.prefix(count)
	if err != nil {
		return out, err
	}
	l, err := old.encode()
	if err != nil {
		return out, err
	}
	compact, err := storageCompositionCompactCore(core)
	if err != nil {
		return out, err
	}
	coreFull, lineageFull := sha256.Sum256(core), sha256.Sum256(l)
	for bank := 0; bank < 2; bank++ {
		prefix := p[bank*320 : (bank+1)*320]
		if !bytes.Equal(prefix[:8], []byte("DSKONL1\x00")) || storageEnrollmentPeakU32(prefix[72:76]) != uint32(len(compact)) || storageEnrollmentPeakU32(prefix[76:80]) != uint32(len(l)) || !bytes.Equal(prefix[128:160], coreFull[:]) || !bytes.Equal(prefix[160:192], lineageFull[:]) || storageEnrollmentPeakU32(prefix[24:28]) != uint32(256+len(compact)+len(l)) || 320+len(compact)+len(l) > storageOwnerCatalogBytes {
			return [2][]byte{}, storagebudget.ErrBounds
		}
		raw := make([]byte, storageOwnerCatalogBytes)
		copy(raw[:320], prefix)
		copy(raw[320:], compact)
		copy(raw[320+len(compact):], l)
		d := sha256.Sum256(raw)
		if !bytes.Equal(d[:], p[648+32*bank:680+32*bank]) {
			return [2][]byte{}, storagebudget.ErrBounds
		}
		out[bank] = raw
	}
	again, err := storageCompositionCaptureProjection(out[0], out[1], record, claim)
	if err != nil || again != p {
		return [2][]byte{}, storagebudget.ErrBounds
	}
	return out, nil
}

// First-base bytes are a closed comparison plan, not an admission. The live
// original issuer must retain exclusion, native descriptions, Q/source proof
// and the complete combined remaining board before consuming any entry key.
type storageCompositionFirstBasePlan struct {
	Original              [2][]byte
	Prepared, Active      []byte
	BeforeBank, FirstBank byte
	Commitment            storagebudget.Digest
}

func storageCompositionFirstBaseCommitment(original [2][]byte, recordRaw, claimRaw, ready []byte, inspection storagebudget.HostInspection, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storagebudget.Digest, error) {
	var zero storagebudget.Digest
	r, err := storageOwnerEncodeRecordV2(record)
	if err != nil || !bytes.Equal(r, recordRaw) {
		return zero, storagebudget.ErrBounds
	}
	c, err := storageOwnerEncodeClaimV2(claim)
	if err != nil || !bytes.Equal(c, claimRaw) || len(ready) != 32 || storageEnrollmentPeakZero(ready) || inspection.Configuration.Revision == 0 || inspection.Configuration.Epoch == 0 || inspection.Configuration.Maintenance {
		return zero, storagebudget.ErrBounds
	}
	host, err := json.Marshal(inspection)
	if err != nil {
		return zero, err
	}
	h := sha256.New()
	h.Write([]byte("desk-owner-composition-first-base-v1\x00"))
	for _, raw := range original {
		if len(raw) != storageOwnerCatalogBytes {
			return zero, storagebudget.ErrBounds
		}
		h.Write(raw)
	}
	h.Write(recordRaw)
	h.Write(claimRaw)
	h.Write(ready)
	h.Write(host)
	copy(zero[:], h.Sum(nil))
	return zero, nil
}

func storageCompositionPlanFirstBase(original [2][]byte, id storagebudget.ID, recordRaw, claimRaw, ready []byte, inspection storagebudget.HostInspection, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageCompositionFirstBasePlan, error) {
	var out storageCompositionFirstBasePlan
	if id == (storagebudget.ID{}) {
		return out, storagebudget.ErrBounds
	}
	commitment, err := storageCompositionFirstBaseCommitment(original, recordRaw, claimRaw, ready, inspection, record, claim)
	if err != nil {
		return out, err
	}
	v := storageCompositionBank{Publication: storageCompositionPrepared, Transition: storageCompositionBase, TransitionID: id, FirstBase: commitment, Lineage: storageCompositionEmptyLineage()}
	lineage, err := v.Lineage.encode()
	if err != nil {
		return out, err
	}
	v.LineageDigest = sha256.Sum256(lineage)
	if len(original[0]) == storageOwnerCatalogBytes && string(original[0][:8]) == "DSKOWN3\x00" {
		pair, e := storageOwnerDecodeCatalogPairV3(original[0], original[1], record, claim)
		if e != nil || pair.High.Phase != "active" {
			return out, storagebudget.ErrBounds
		}
		v.CoreCodec, v.BeforeBank = 1, 1
		if storageEnrollmentPeakU64(original[0][16:24]) > storageEnrollmentPeakU64(original[1][16:24]) {
			v.BeforeBank = 0
		}
		peer, e := storageOwnerDecodeCatalogV3(original[1-v.BeforeBank], record, claim)
		if e != nil {
			return out, e
		}
		if pair.PeerLinked {
			v.BeforePeer[0] = 1
		}
		d := sha256.Sum256(original[1-v.BeforeBank])
		copy(v.BeforePeer[1:33], d[:])
		copy(v.BeforePeer[33:65], peer.PreviousCatalogDigest[:])
	} else {
		e, bank, e0 := storageEnrollmentPeakDecodePair(original[0], original[1], record, claim)
		if e0 != nil || e.Kind != 2 || e.Marker == nil || e.Marker.Phase != 3 || !bytes.Equal(original[0], original[1]) {
			return out, storagebudget.ErrBounds
		}
		v.CoreCodec, v.BeforeBank = 2, byte(bank)
		v.BeforePeer = storageCompositionDuplicateCorePeer(original[bank], record, claim)
	}
	v.Core = append([]byte{}, original[v.BeforeBank]...)
	v.CoreFull = storageCompositionCoreDigest(sha256.Sum256(v.Core))
	copy(v.Previous[:], v.Core[32:64])
	v.Sequence, err = storageCompositionSequence(storageEnrollmentPeakU64(v.Core[16:24]), 1)
	if err != nil || v.Sequence == math.MaxUint64 {
		return out, storagebudget.ErrBounds
	}
	prepared, err := v.encode(record, claim)
	if err != nil {
		return out, err
	}
	v.Sequence++
	v.Publication = storageCompositionActive
	copy(v.Previous[:], prepared[32:64])
	active, err := v.encode(record, claim)
	if err != nil {
		return out, err
	}
	out = storageCompositionFirstBasePlan{Original: original, Prepared: prepared, Active: active, BeforeBank: v.BeforeBank, FirstBank: 1 - v.BeforeBank, Commitment: commitment}
	return out, nil
}

func (v storageCompositionBank) firstBaseOriginal(record storageOwnerRecordV2, claim storageOwnerClaimV2) ([2][]byte, error) {
	var original [2][]byte
	if v.validateCore(record, claim) != nil || v.Transition != storageCompositionBase || len(v.Lineage.Rows.LineagePlans) != 0 || !v.Lineage.valid() {
		return original, storagebudget.ErrBounds
	}
	original[v.BeforeBank] = append([]byte{}, v.Core...)
	peer := append([]byte{}, v.Core...)
	if v.CoreCodec == 1 && v.BeforePeer[0] == 1 {
		c, err := storageOwnerDecodeCatalogV3(v.Core, record, claim)
		if err != nil || c.Sequence <= 1 {
			return original, storagebudget.ErrBounds
		}
		c.Sequence--
		c.Phase = "prepared"
		copy(c.PreviousCatalogDigest[:], v.BeforePeer[33:65])
		peer, err = storageOwnerEncodeCatalogV3(c, record, claim)
		if err != nil {
			return original, err
		}
	}
	d := sha256.Sum256(peer)
	if !bytes.Equal(d[:], v.BeforePeer[1:33]) {
		return original, storagebudget.ErrBounds
	}
	original[1-v.BeforeBank] = peer
	return original, nil
}

// Recreate only an exact already-retained founding plan. A surviving valid
// old peer must be its original image at its actual bank; malformed/torn banks
// never qualify. No write, grant replay, Q action or native proof occurs here.
func storageCompositionRecoverFirstBase(actual [2][]byte, recordRaw, claimRaw, ready []byte, inspection storagebudget.HostInspection, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageCompositionFirstBasePlan, byte, error) {
	var zero storageCompositionFirstBasePlan
	var v storageCompositionBank
	found := false
	for _, raw := range actual {
		if len(raw) != storageOwnerCatalogBytes {
			return zero, 0, storagebudget.ErrBounds
		}
		if string(raw[:8]) != "DSKONL1\x00" {
			continue
		}
		x, err := storageCompositionDecode(raw, record, claim)
		if err != nil || x.Transition != storageCompositionBase || found && x.TransitionID != v.TransitionID {
			return zero, 0, storagebudget.ErrBounds
		}
		if !found || x.Sequence > v.Sequence {
			v = x
		}
		found = true
	}
	if !found {
		return zero, 0, storagebudget.ErrBounds
	}
	original, err := v.firstBaseOriginal(record, claim)
	if err != nil {
		return zero, 0, err
	}
	plan, err := storageCompositionPlanFirstBase(original, v.TransitionID, recordRaw, claimRaw, ready, inspection, record, claim)
	if err != nil || plan.Commitment != v.FirstBase {
		return zero, 0, storagebudget.ErrBounds
	}
	b, f := plan.BeforeBank, plan.FirstBank
	switch {
	case bytes.Equal(actual[f], plan.Prepared) && bytes.Equal(actual[b], plan.Original[b]):
		return plan, 1, nil
	case bytes.Equal(actual[0], plan.Prepared) && bytes.Equal(actual[1], plan.Prepared):
		return plan, 2, nil
	case bytes.Equal(actual[f], plan.Active) && bytes.Equal(actual[b], plan.Prepared):
		return plan, 3, nil
	default:
		return zero, 0, storagebudget.ErrBounds
	}
}

func storageCompositionPrivateSourceDigest(c storageOwnerCatalogV2, r storageOwnerRoot) (storagebudget.Digest, error) {
	if r.Source.Kind != 8 {
		return c.sourceDigest(r)
	}
	s := r.Source
	return storageOwnerDigest("desk-storage-owner-source-relation-v2", storageOwnerSourceMaterial{Kind: 8, OwnerKey: s.OwnerKey, Path: s.Path, ParentRoot: s.ParentRoot, Relative: s.Relative, FixedControl: storageOwnerEmptyObjectMaterial(), ReplaceableControl: storageOwnerSelectedControlMaterial{Name: []byte{}, OwnerKey: []byte{}, SelectedPath: []byte{}}, Home: storageOwnerHomeRelation{Folder: []byte{}, HistoryIdentity: []byte{}}})
}

// An actual-native V6 source bank is a distinct physical carrier. Stage.Core is
// an E1/V3 comparison image; Stage.Raw is always the original V6 physical frame.
// Neither a reconstructed Source nor this decoded DTO grants native custody.
// A current healthy Q boundary is comparison data. Only the exact packed
// outcome or genuine completed E1 join derives it; P/C/S/F merely retain it.
type storagePolicyQBoundary struct{ Epoch, MinimumRevision uint64 }

func (b storagePolicyQBoundary) valid() bool { return b.Epoch != 0 && b.MinimumRevision != 0 }

func storagePolicyOutcomeBoundary(p storageOwnerV6PackedCatalog) (storagePolicyQBoundary, error) {
	if p.TransitionKind != storageOwnerV6PolicyTransition {
		return storagePolicyQBoundary{}, storagebudget.ErrBounds
	}
	t, err := storagebudget.DecodePolicyTransition(p.PolicyTransition)
	if err != nil {
		return storagePolicyQBoundary{}, err
	}
	b := storagePolicyQBoundary{t.Original.Epoch, t.Original.Revision}
	switch p.Phase {
	case "active":
		if b.Epoch == math.MaxUint64 || b.MinimumRevision > math.MaxUint64-3 {
			return storagePolicyQBoundary{}, storagebudget.ErrBounds
		}
		b.Epoch++
		b.MinimumRevision += 3
	case "aborted":
		if b.Epoch == math.MaxUint64 || b.MinimumRevision > math.MaxUint64-2 {
			return storagePolicyQBoundary{}, storagebudget.ErrBounds
		}
		b.Epoch++
		b.MinimumRevision += 2
	case "aborted-unfenced":
	default:
		return storagePolicyQBoundary{}, storagebudget.ErrBounds
	}
	if !b.valid() {
		return storagePolicyQBoundary{}, storagebudget.ErrBounds
	}
	return b, nil
}

type storageOwnerV6NativeBank struct {
	// Packed is populated only for an actual old endpoint in a mixed first
	// source edge. Such a comparison operand cannot be encoded as a native bank.
	Packed     *storageOwnerV6PackedCatalog
	Stage      storageCompositionBank
	NativeBase uint64
	Boundary   storagePolicyQBoundary
	Policy     storageOwnerPolicy
	Overrides  []byte
	Before     *storageOwnerV6BeforePair
}

// BeforePair is an immediate endpoint recipe, not a nested carrier. Its core
// comes from the enclosing Fence/Captured Snapshot, and each old lineage comes
// from the exact chronological prefix of the enclosing bank's ONE lineage.
// Effective values are supplied by the enclosing carrier (or an independently
// bound historical effective set). No digest is treated as recoverable bytes.
type storageOwnerV6BeforePair struct {
	Kind, Selected byte // 1: packed policy endpoint; 2: native source endpoint
	Headers        [2][64]byte
	Full           [2]storagebudget.Digest
	Cut            [2][4]uint16
	Native         [2][storageCompositionPrefixBytes]byte
	Packed         storagePolicyProjection
	Physical       storageCompositionProjection
	NativeMoveMap  [2][]uint16
	// Context bytes are carried once by the enclosing physical/evidence payload.
	MoveOperands storageMoveOperandTable
}

func storageOwnerV6LineageCut(l storageCompositionLineage) (out [4]uint16) {
	for i := range out {
		out[i] = uint16(len(l.Order[i]))
	}
	return
}

// Capture is deliberately completed-only. Transient source cuts are decoded by
// NativeReadPair for the original recovery path, never adopted as a new before.
func storageOwnerV6CaptureBefore(actual [2][]byte, selected byte, evidenceRaw []byte, catalog storageOwnerCatalogV2, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageOwnerV6BeforePair, []byte, storageCompositionLineage, storageOwnerPolicy, []byte, error) {
	var out storageOwnerV6BeforePair
	fail := func(err error) (storageOwnerV6BeforePair, []byte, storageCompositionLineage, storageOwnerPolicy, []byte, error) {
		return storageOwnerV6BeforePair{}, nil, storageCompositionLineage{}, storageOwnerPolicy{}, nil, err
	}
	if selected > 1 {
		return fail(storagebudget.ErrBounds)
	}
	out.Selected = selected
	var core []byte
	var lineage storageCompositionLineage
	var policy storageOwnerPolicy
	var sparse []byte
	payload, _, err := storageOwnerV6Payload(actual[selected])
	if err != nil {
		return fail(err)
	}
	if bytes.HasPrefix(payload, storageOwnerV6NativeMagic[:]) {
		pair, e := storageOwnerV6NativeReadPair(actual[0], actual[1], nil, nil, record, claim)
		if e != nil || pair.RecoveryOnly || pair.Selected != selected {
			return fail(storagebudget.ErrBounds)
		}
		out.Kind = 2
		v := pair.Banks[selected]
		core, lineage, policy, sparse = v.Stage.Core, v.Stage.Lineage, v.Policy, v.Overrides
		if _, e = v.validateEvidence(evidenceRaw, record, claim); e != nil {
			return fail(e)
		}
		for i, bank := range pair.Banks {
			if bank.Before != nil || !bytes.Equal(core, bank.Stage.Core) {
				return fail(storagebudget.ErrBounds)
			}
			prefix, e := storageOwnerV6NativeStagePrefix(bank.Stage, bank.NativeBase, bank.Boundary)
			if e != nil {
				return fail(e)
			}
			copy(out.Native[i][:], prefix)
			out.Cut[i] = storageOwnerV6LineageCut(bank.Stage.Lineage)
		}
		var actualSets [][]storageMoveCurrent
		var counts [2]int
		for i, bank := range pair.Banks {
			t, _, _, e := storageMoveBankOperands(bank.Stage)
			if e != nil {
				return fail(e)
			}
			counts[i] = len(t.Sets)
			actualSets = append(actualSets, t.Sets...)
		}
		if len(actualSets) != 0 {
			t, mapping, e := storageMoveInternOperands(actualSets)
			if e != nil {
				return fail(e)
			}
			out.Kind, out.MoveOperands = 3, t
			offset := 0
			for i, n := range counts {
				if n != 0 {
					out.NativeMoveMap[i] = append([]uint16{}, mapping[offset:offset+n]...)
				}
				offset += n
			}
		}
	} else {
		out.Kind = 1
		v, e := storageOwnerV6PackedDecode(actual[selected], evidenceRaw, record, claim)
		if e != nil || v.Phase != "active" && v.Phase != "aborted" && v.Phase != "aborted-unfenced" {
			return fail(storagebudget.ErrBounds)
		}
		p, e := storagePolicyBeforeProjection(catalog, catalog, actual[selected])
		if e != nil || storagePolicyRetainProjectionPeer(&p, actual, selected, evidenceRaw, record, claim) != nil {
			return fail(storagebudget.ErrBounds)
		}
		core = make([]byte, storageOwnerCatalogBytes)
		copy(core, p.SavedOuter)
		lineage, policy, sparse = p.Policy.Lineage, v.EffectivePolicy, v.Overrides
		out.Packed, out.Physical = *p.Policy, p.Physical
		if p.Policy.SourceEncoding == 4 {
			out.MoveOperands = p.Policy.MoveOperands
			out.Packed.SourceMoveMap = make([]uint16, len(out.MoveOperands.Sets))
			for i := range out.Packed.SourceMoveMap {
				out.Packed.SourceMoveMap[i] = uint16(i)
			}
			out.Packed.MoveOperands = storageMoveOperandTable{}
		}
		// The recipe never serializes this duplicate expanded set or lineage.
		out.Packed.Effective = storagePolicyEffectiveSet{}
		out.Packed.Lineage = storageCompositionLineage{}
		out.Cut[0], out.Cut[1] = storageOwnerV6LineageCut(lineage), storageOwnerV6LineageCut(lineage)
	}
	for i, raw := range actual {
		if len(raw) != storageOwnerCatalogBytes {
			return fail(storagebudget.ErrBounds)
		}
		copy(out.Headers[i][:], raw[:64])
		out.Full[i] = sha256.Sum256(raw)
	}
	restored, err := out.restore(core, lineage, policy, sparse, catalog, record, claim)
	if err != nil || !bytes.Equal(restored[0], actual[0]) || !bytes.Equal(restored[1], actual[1]) {
		return fail(storagebudget.ErrBounds)
	}
	return out, append([]byte{}, core...), lineage, policy, append([]byte{}, sparse...), nil
}

func (p storageOwnerV6BeforePair) restore(core []byte, retained storageCompositionLineage, policy storageOwnerPolicy, sparse []byte, catalog storageOwnerCatalogV2, record storageOwnerRecordV2, claim storageOwnerClaimV2) ([2][]byte, error) {
	var out [2][]byte
	if p.Selected > 1 || len(core) != storageOwnerCatalogBytes {
		return out, storagebudget.ErrBounds
	}
	if p.Kind == 1 {
		if p.Cut[0] != p.Cut[1] {
			return out, storagebudget.ErrBounds
		}
		lineage, err := retained.prefix(p.Cut[0])
		if err != nil {
			return out, err
		}
		_, defs, err := catalog.definitions()
		if err != nil {
			return out, err
		}
		rows, err := storageOwnerV6DecodeSparse(sparse, defs)
		if err != nil {
			return out, err
		}
		effective, err := storageOwnerV6Expand(defs, policy, rows)
		if err != nil {
			return out, err
		}
		meta := p.Packed
		if meta.SourceEncoding == 4 {
			meta.MoveOperands, err = p.MoveOperands.reconstruct(meta.SourceMoveMap)
			if err != nil {
				return out, err
			}
		}
		meta.Header = p.Headers[p.Selected]
		meta.Lineage = lineage
		meta.Effective = storagePolicyEffectiveSet{Policy: policy, Workspaces: append([]storagebudget.RegisteredWorkspace{}, effective.Workspaces...)}
		sort.Slice(meta.Effective.Workspaces, func(i, j int) bool {
			return bytes.Compare(meta.Effective.Workspaces[i].ID[:], meta.Effective.Workspaces[j].ID[:]) < 0
		})
		meta.SelectedBank, meta.PeerRetained = p.Selected, true
		compact, err := storageCompositionCompactCore(core)
		if err != nil {
			return out, err
		}
		projection := storageEnrollmentPeakProjection{Kind: 1, Catalog: catalog, SavedOuter: append([]byte{}, compact...), Physical: p.Physical, Policy: &meta}
		out, err = storagePolicyProjectionPair(projection)
		if err != nil {
			return [2][]byte{}, err
		}
	} else if p.Kind == 2 || p.Kind == 3 {
		if p.Kind == 3 && storageMoveBeforeReferencesCurrent(p, p.MoveOperands, nil, true) != nil {
			return out, storagebudget.ErrBounds
		}
		for i := range out {
			lineage, err := retained.prefix(p.Cut[i])
			if err != nil {
				return [2][]byte{}, err
			}
			var operands storageMoveOperandTable
			if p.Kind == 3 && len(p.NativeMoveMap[i]) != 0 {
				operands, err = p.MoveOperands.reconstruct(p.NativeMoveMap[i])
				if err != nil {
					return out, err
				}
			}
			stage, base, boundary, err := storageOwnerV6NativeReadStagePrefixWithOperands(p.Native[i][:], storageEnrollmentPeakU64(p.Headers[i][16:24]), core, lineage, operands)
			if err != nil {
				return [2][]byte{}, err
			}
			v := storageOwnerV6NativeBank{Stage: stage, NativeBase: base, Boundary: boundary, Policy: policy, Overrides: append([]byte{}, sparse...)}
			out[i], err = v.encode(record, claim)
			if err != nil {
				return [2][]byte{}, err
			}
		}
		pair, err := storageOwnerV6NativeReadPair(out[0], out[1], nil, nil, record, claim)
		if err != nil || pair.RecoveryOnly || pair.Selected != p.Selected {
			return [2][]byte{}, storagebudget.ErrBounds
		}
	} else {
		return out, storagebudget.ErrBounds
	}
	for i, raw := range out {
		if !bytes.Equal(raw[:64], p.Headers[i][:]) || sha256.Sum256(raw) != p.Full[i] {
			return [2][]byte{}, storagebudget.ErrBounds
		}
	}
	return out, nil
}

// Byte-level source publication relation. Graph, receipt, native identity,
// ready, Q and persistence joins are still required by the original issuer.
// BeforeCatalog is required ONLY for the first Fence's exact before recipe;
// it must come from that Snapshot's independently bound original evidence.
type storageOwnerV6NativePair struct {
	Banks                    [2]storageOwnerV6NativeBank
	Selected                 byte
	Duplicated, RecoveryOnly bool
}

func storageOwnerV6NativeSameEffective(a, b storageOwnerV6NativeBank) bool {
	return a.Policy == b.Policy && bytes.Equal(a.Overrides, b.Overrides)
}

func storageOwnerV6NativeReadPair(a, b []byte, beforeCatalog *storageOwnerCatalogV2, beforeEvidence []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageOwnerV6NativePair, error) {
	var out storageOwnerV6NativePair
	for i, raw := range [2][]byte{a, b} {
		v, err := storageOwnerV6NativeReadEndpoint(raw, beforeEvidence, record, claim)
		if err != nil {
			return out, err
		}
		out.Banks[i] = v
	}
	x, y := out.Banks[0], out.Banks[1]
	if string(a[:8]) == "DSKONL1\x00" && string(b[:8]) == "DSKONL1\x00" {
		// The unchanged strict composition relation selects actual originals.
		// Normalizing its completed E1 into a comparison view does not relabel
		// either physical image as V6 or grant publication permission.
		legacy, err := storageCompositionReadPair(a, b, record, claim)
		if err != nil || legacy.RecoveryOnly {
			return storageOwnerV6NativePair{}, storagebudget.ErrBounds
		}
		out.Selected, out.Duplicated = legacy.Selected, legacy.Duplicated
		for i := range out.Banks {
			out.Banks[i].NativeBase = legacy.Banks[legacy.Selected].Sequence
		}
		return out, nil
	}
	if x.Packed != nil && y.Packed != nil {
		return storageOwnerV6NativePair{}, storagebudget.ErrBounds
	}
	if !storageOwnerV6NativeSameEffective(x, y) {
		return storageOwnerV6NativePair{}, storagebudget.ErrBounds
	}
	if x.Stage.Sequence == y.Stage.Sequence {
		if !bytes.Equal(a, b) || x.Stage.Transition != storageCompositionEnrollment {
			return storageOwnerV6NativePair{}, storagebudget.ErrBounds
		}
		e, err := storageEnrollmentPeakDecodeEnvelopeNative(x.Stage.Core, record, claim, true, true, true)
		if err != nil {
			return storageOwnerV6NativePair{}, err
		}
		group := uint16(0)
		if e.Kind == 0 {
			group = 9
			if e.Scope.path() == 1 {
				group = 60
			}
		} else if e.Kind == 2 && e.Marker != nil {
			switch e.Marker.Phase {
			case 0:
				group = 67
			case 1:
				group = 11
			case 3:
				group = 14
			}
		}
		if group == 0 {
			return storageOwnerV6NativePair{}, storagebudget.ErrBounds
		}
		shape, err := storageCompositionPublication(e.Scope.path(), e.Scope[322], group)
		if err != nil {
			return storageOwnerV6NativePair{}, err
		}
		dup, err := storageCompositionPublication(e.Scope.path(), e.Scope[322], group+1)
		if err != nil || !dup.Duplicate || dup.Offset != shape.Offset || x.Stage.Publication != shape.Publication {
			return storageOwnerV6NativePair{}, storagebudget.ErrBounds
		}
		out.Duplicated, out.Selected, out.RecoveryOnly = true, shape.Bank, true
		if e.Kind == 2 && e.Marker.Phase == 3 {
			out.RecoveryOnly = false
		}
		return out, nil
	}
	lowBank, highBank := byte(0), byte(1)
	if x.Stage.Sequence > y.Stage.Sequence {
		lowBank, highBank = 1, 0
	}
	low, high := out.Banks[lowBank], out.Banks[highBank]
	if high.Packed != nil {
		return storageOwnerV6NativePair{}, storagebudget.ErrBounds
	}
	previous, err := storageCompositionChecksumOf(low.Stage.Raw)
	if err != nil || low.Stage.Sequence == math.MaxUint64 || high.Stage.Sequence != low.Stage.Sequence+1 || high.Stage.Previous != previous {
		return storageOwnerV6NativePair{}, storagebudget.ErrBounds
	}
	legal := false
	if high.Stage.Transition == storageCompositionMove {
		legal = low.NativeBase == high.NativeBase && low.Boundary.Epoch == high.Boundary.Epoch && high.Boundary.MinimumRevision >= low.Boundary.MinimumRevision && storageMoveBankEdge(low.Stage, high.Stage)
	} else {
		if !storageMoveSameCurrents(low.Stage.Moves, high.Stage.Moves) {
			return storageOwnerV6NativePair{}, storagebudget.ErrBounds
		}
		legal = low.Boundary == high.Boundary && low.NativeBase == high.NativeBase && storageCompositionFinalize(low.Stage, high.Stage)
	}
	if high.Stage.Transition != storageCompositionMove && low.Boundary == high.Boundary && low.NativeBase == high.NativeBase && low.Stage.Publication == storageCompositionActive && high.Stage.Publication == storageCompositionPrepared && storageCompositionLineageAppend(low.Stage, high.Stage) {
		legal = true
	}
	if high.Stage.Transition == storageCompositionEnrollment && storageOwnerV6NativeCoreEdge(low, high, highBank, beforeCatalog, record, claim) {
		legal = true
	}
	if !legal {
		return storageOwnerV6NativePair{}, storagebudget.ErrBounds
	}
	out.Selected = highBank
	out.RecoveryOnly = high.Stage.Publication != storageCompositionActive || high.Stage.Transition == storageCompositionEnrollment
	return out, nil
}

func storageOwnerV6NativeCoreEdge(low, high storageOwnerV6NativeBank, highBank byte, beforeCatalog *storageOwnerCatalogV2, record storageOwnerRecordV2, claim storageOwnerClaimV2) bool {
	if highBank > 1 || high.Stage.CoreCodec != 2 || high.Stage.Transition != storageCompositionEnrollment || !storageCompositionSameLineage(low.Stage, high.Stage) || !storageOwnerV6NativeSameEffective(low, high) {
		return false
	}
	e, err := storageEnrollmentPeakDecodeEnvelopeNative(high.Stage.Core, record, claim, true, true, true)
	if err != nil {
		return false
	}
	if e.Kind == 2 && e.Marker != nil && e.Marker.Phase == 3 {
		if high.Boundary != (storagePolicyQBoundary{e.Marker.Q3Epoch, e.Marker.Q3Revision}) {
			return false
		}
	} else if low.Boundary != high.Boundary {
		return false
	}
	group := uint16(0)
	if e.Kind == 0 {
		if beforeCatalog == nil || low.Stage.Publication != storageCompositionActive || high.Before == nil || e.Snapshot == nil || e.Snapshot.Encoding != 6 || !bytes.Equal(e.Snapshot.Before, low.Stage.Core) {
			return false
		}
		before, err := high.Before.restore(e.Snapshot.Before, high.Stage.Lineage, high.Policy, high.Overrides, *beforeCatalog, record, claim)
		if err != nil || high.NativeBase != storageEnrollmentPeakU64(high.Before.Headers[high.Before.Selected][16:24]) || high.Before.Selected != e.Scope[322] || e.Scope[322] != 1-highBank || !bytes.Equal(before[e.Scope[322]], low.Stage.Raw) {
			return false
		}
		group = 9
		if e.Scope.path() == 1 {
			group = 60
		}
	} else {
		if low.NativeBase != high.NativeBase || low.Stage.Transition != storageCompositionEnrollment || low.Stage.BeforeBank != high.Stage.BeforeBank || low.Stage.BeforePeer != high.Stage.BeforePeer || low.Stage.FirstBase != high.Stage.FirstBase {
			return false
		}
		if e.Kind == 1 {
			group = 66
		} else if e.Marker != nil {
			switch e.Marker.Phase {
			case 0:
				group = 67
			case 1:
				group = 11
			case 2:
				group = 13
			case 3:
				group = 14
			}
		}
		if group == 0 {
			return false
		}
		cores := [2][]byte{low.Stage.Core, low.Stage.Core}
		cores[highBank] = high.Stage.Core
		if _, selected, err := storageEnrollmentPeakDecodePairNative(cores[0], cores[1], record, claim, true, nil, true); err != nil || selected != int(highBank) {
			return false
		}
	}
	shape, err := storageCompositionPublication(e.Scope.path(), e.Scope[322], group)
	if err != nil || shape.Bank != highBank || shape.CoreKind != e.Kind || shape.Publication != high.Stage.Publication || e.Marker != nil && shape.MarkerPhase != e.Marker.Phase {
		return false
	}
	sequence, err := storageCompositionSequence(high.NativeBase, shape.Offset)
	return err == nil && high.Stage.Sequence == sequence
}

func (v storageOwnerV6NativeBank) validateEvidence(raw []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageEnrollmentPeakEvidence, error) {
	outer, err := storageEnrollmentPeakDecodeEnvelopeNative(v.Stage.Core, record, claim, true, true, true)
	if err != nil || outer.Marker == nil || outer.Marker.Phase != 3 {
		return storageEnrollmentPeakEvidence{}, storagebudget.ErrBounds
	}
	e, err := storageEnrollmentPeakDecodeEvidenceWithLineage(raw, &v.Stage.Lineage, record, claim)
	if err != nil || storageEnrollmentPeakJoinMarker(outer, e, e.Ready) != nil || v.Stage.Lineage.validateGraph(e.Inner.Catalog, record) != nil {
		return storageEnrollmentPeakEvidence{}, storagebudget.ErrBounds
	}
	_, defs, err := e.Inner.Catalog.definitions()
	if err != nil {
		return storageEnrollmentPeakEvidence{}, err
	}
	rows, err := storageOwnerV6DecodeSparse(v.Overrides, defs)
	if err != nil {
		return storageEnrollmentPeakEvidence{}, err
	}
	if _, err = storageOwnerV6Expand(defs, v.Policy, rows); err != nil {
		return storageEnrollmentPeakEvidence{}, err
	}
	if v.Stage.Transition == storageCompositionEnrollment {
		if e.Version != 3 {
			return storageEnrollmentPeakEvidence{}, storagebudget.ErrBounds
		}
		found := false
		for _, p := range e.Projections {
			if storageEnrollmentPeakProjectionFullDigest(p) == e.Before {
				if found {
					return storageEnrollmentPeakEvidence{}, storagebudget.ErrBounds
				}
				found = true
				if _, _, err = storageOwnerV6NativeSourceJoin(p, e.Inner.Catalog, v.Policy, v.Overrides); err != nil {
					return storageEnrollmentPeakEvidence{}, err
				}
			}
		}
		if !found {
			return storageEnrollmentPeakEvidence{}, storagebudget.ErrBounds
		}
	}
	return e, nil
}

// Read the old endpoint only as an explicit mixed-edge operand. It is strict
// decoded against its original evidence and retains Raw as the ACTUAL V6 bytes.
// Source images are used for frozen-core/lineage comparison, never as a physical
// peer, Previous checksum, selected bank or currentness witness.
func storageOwnerV6NativeReadEndpoint(raw, evidence []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageOwnerV6NativeBank, error) {
	if len(raw) == storageOwnerCatalogBytes && string(raw[:8]) == "DSKONL1\x00" {
		stage, err := storageCompositionDecode(raw, record, claim)
		if err != nil || stage.CoreCodec != 2 || stage.Publication != storageCompositionActive && stage.Publication != storageCompositionPrepared {
			return storageOwnerV6NativeBank{}, storagebudget.ErrBounds
		}
		outer, err := storageEnrollmentPeakDecodeEnvelopeVariant(stage.Core, record, claim, true, true)
		if err != nil || outer.Marker == nil || outer.Marker.Phase != 3 {
			return storageOwnerV6NativeBank{}, storagebudget.ErrBounds
		}
		e, err := stage.validateEvidence(evidence, outer.Marker.Ready, record, claim)
		if err != nil {
			return storageOwnerV6NativeBank{}, err
		}
		// The only original boundary is this completed genuine E1's Q3. The
		// actual selected outer sequence anchors the new native generation;
		// it never replaces the independently retained core sequence/domain.
		return storageOwnerV6NativeBank{Stage: stage, NativeBase: stage.Sequence, Boundary: storagePolicyQBoundary{e.AfterEpoch, e.AfterRevision}, Policy: e.Inner.Catalog.Policy, Overrides: []byte{0, 0}}, nil
	}
	payload, _, err := storageOwnerV6Payload(raw)
	if err != nil {
		return storageOwnerV6NativeBank{}, err
	}
	if bytes.HasPrefix(payload, storageOwnerV6NativeMagic[:]) {
		return storageOwnerV6NativeDecode(raw, record, claim)
	}
	p, err := storageOwnerV6PackedDecode(raw, evidence, record, claim)
	if err != nil || p.Phase != "active" && p.Phase != "aborted" && p.Phase != "aborted-unfenced" {
		return storageOwnerV6NativeBank{}, storagebudget.ErrBounds
	}
	_, source, lineage, _, err := storageOwnerV6PackedSourceForPolicy(p, evidence, record, claim)
	if err != nil {
		return storageOwnerV6NativeBank{}, err
	}
	var stage storageCompositionBank
	if sourceDTO, _, e := storageOwnerDecodePackedSource(p.Source); e == nil && (sourceDTO.Encoding == 3 || sourceDTO.Encoding == 4) {
		pair, e := storageOwnerV6NativeReadPair(source[0], source[1], nil, nil, record, claim)
		if e != nil || pair.RecoveryOnly {
			return storageOwnerV6NativeBank{}, storagebudget.ErrBounds
		}
		stage = pair.Banks[pair.Selected].Stage
	} else if string(source[0][:8]) == "DSKONL1\x00" {
		pair, err := storageCompositionReadPair(source[0], source[1], record, claim)
		if err != nil || pair.RecoveryOnly {
			return storageOwnerV6NativeBank{}, storagebudget.ErrBounds
		}
		stage = pair.Banks[pair.Selected]
	} else {
		core := source[0]
		outer, err := storageEnrollmentPeakDecodeEnvelopeVariant(core, record, claim, true, false)
		if err != nil || outer.Marker == nil || outer.Marker.Phase != 3 {
			return storageOwnerV6NativeBank{}, storagebudget.ErrBounds
		}
		l, err := lineage.encode()
		if err != nil {
			return storageOwnerV6NativeBank{}, err
		}
		stage = storageCompositionBank{CoreCodec: 2, Publication: storageCompositionActive, Transition: storageCompositionEnrollment, BeforeBank: outer.Scope[322], TransitionID: outer.Marker.Bridge, CoreFull: storageCompositionCoreDigest(sha256.Sum256(core)), LineageDigest: sha256.Sum256(l), BeforePeer: storageCompositionDuplicateCorePeer(core, record, claim), Core: append([]byte{}, core...), Lineage: lineage}
	}
	stage.Sequence = p.Sequence
	stage.Previous = storageCompositionChecksum(p.PreviousCatalogDigest)
	stage.Raw = append([]byte{}, raw...)
	boundary, err := storagePolicyOutcomeBoundary(p)
	if p.TransitionKind == storageOwnerV6SourceTransition {
		// The accepted source variant has its own completed genuine E1 source.
		// Strict current-Q activation must still compare this exact Q3 tuple.
		e, e0 := storageEnrollmentPeakDecodeEnvelopeVariant(stage.Core, record, claim, true, true)
		if e0 != nil || p.Phase != "active" || e.Marker == nil || e.Marker.Phase != 3 {
			return storageOwnerV6NativeBank{}, storagebudget.ErrBounds
		}
		boundary, err = storagePolicyQBoundary{e.Marker.Q3Epoch, e.Marker.Q3Revision}, nil
	}
	if err != nil || !boundary.valid() {
		return storageOwnerV6NativeBank{}, storagebudget.ErrBounds
	}
	return storageOwnerV6NativeBank{Packed: &p, Stage: stage, Boundary: boundary, Policy: p.EffectivePolicy, Overrides: append([]byte{}, p.Overrides...)}, nil
}

// Pure byte preparation for the EXISTING Prepared write/persist then Active
// write/persist groups. No dispatch, admission, recovery or native permission is
// returned. The caller retains its original keys and checks every group once.
func storageOwnerV6NativeAppend(actual [2][]byte, selected byte, evidence []byte, catalog storageOwnerCatalogV2, lineage storageCompositionLineage, transition byte, id storagebudget.ID, record storageOwnerRecordV2, claim storageOwnerClaimV2) ([2][]byte, error) {
	var out [2][]byte
	_, _, _, _, _, err := storageOwnerV6CaptureBefore(actual, selected, evidence, catalog, record, claim)
	if err != nil {
		return out, err
	}
	old, err := storageOwnerV6NativeReadEndpoint(actual[selected], evidence, record, claim)
	if err != nil || old.Stage.Sequence > math.MaxUint64-2 || lineage.validateGraph(catalog, record) != nil {
		return out, storagebudget.ErrBounds
	}
	block, err := lineage.encode()
	if err != nil {
		return out, err
	}
	prepared := old
	prepared.Packed = nil
	prepared.Before = nil
	// These completed rows retain only their current set. Historical sets
	// remain in the exact old physical images/recipes which still select them.
	prepared.Stage.MoveOperands = storageMoveOperandTable{}
	prepared.Stage.Sequence++
	prepared.Stage.Publication = storageCompositionPrepared
	prepared.Stage.Transition, prepared.Stage.TransitionID = transition, id
	prepared.Stage.Previous, err = storageCompositionChecksumOf(actual[selected])
	if err != nil {
		return out, err
	}
	prepared.Stage.Lineage, prepared.Stage.LineageDigest = lineage, sha256.Sum256(block)
	prepared.Stage.Raw = nil
	if !storageCompositionLineageAppend(old.Stage, prepared.Stage) {
		return out, storagebudget.ErrStale
	}
	if transition == storageCompositionPlan {
		if _, err = storageOwnerV6NativeClosureSize(prepared); err != nil {
			return out, err
		}
	}
	out[1-selected], err = prepared.encode(record, claim)
	if err != nil {
		return [2][]byte{}, err
	}
	active := prepared
	active.Stage.Sequence++
	active.Stage.Publication = storageCompositionActive
	active.Stage.Previous, err = storageCompositionChecksumOf(out[1-selected])
	if err != nil {
		return [2][]byte{}, err
	}
	if !storageCompositionFinalize(prepared.Stage, active.Stage) {
		return [2][]byte{}, storagebudget.ErrStale
	}
	out[selected], err = active.encode(record, claim)
	if err != nil {
		return [2][]byte{}, err
	}
	pair, err := storageOwnerV6NativeReadPair(out[0], out[1], nil, nil, record, claim)
	if err != nil || pair.RecoveryOnly || pair.Selected != selected {
		return [2][]byte{}, storagebudget.ErrBounds
	}
	return out, nil
}

// Construct the core-only Snapshot independently of the actual V6 endpoint.
// Scope.Full/base name the actual before; the compact old E1 sequence does not.
// Capsule remains in the enclosing native carrier until the first marker; after
// that the original evidence carries the retained projections/effective sets.
func storageOwnerV6NativeSnapshot(actual [2][]byte, selected byte, evidence []byte, catalog storageOwnerCatalogV2, scope storageEnrollmentPeakScope, intent *storageEnrollmentSuccessorIntent, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageEnrollmentPeakSnapshot, storageOwnerV6BeforePair, error) {
	capsule, core, lineage, _, _, err := storageOwnerV6CaptureBefore(actual, selected, evidence, catalog, record, claim)
	if err != nil {
		return storageEnrollmentPeakSnapshot{}, capsule, err
	}
	if scope.path() == 1 || !scope.hasOriginalPeer() || scope[322] != selected || storageEnrollmentPeakU64(scope[1624:1632]) != storageEnrollmentPeakU64(core[16:24]) || !bytes.Equal(scope[88:120], capsule.Full[selected][:]) {
		return storageEnrollmentPeakSnapshot{}, capsule, storagebudget.ErrBounds
	}
	prior, err := storageEnrollmentPeakDecodeEvidenceWithLineage(evidence, &lineage, record, claim)
	if err != nil {
		return storageEnrollmentPeakSnapshot{}, capsule, err
	}
	outer, err := storageEnrollmentPeakDecodeEnvelopeVariant(core, record, claim, true, true)
	if err != nil || storageEnrollmentPeakJoinMarker(outer, prior, prior.Ready) != nil {
		return storageEnrollmentPeakSnapshot{}, capsule, storagebudget.ErrBounds
	}
	x := storageEnrollmentPeakSnapshot{Kind: 1, Encoding: 6, Full: capsule.Full[selected], Inner: storageEnrollmentPeakInnerDigest(prior.Inner.Raw), Evidence: prior.Controls[prior.Bank], Bank: prior.Bank, Extent: prior.Extent, EvidenceDigest: sha256.Sum256(evidence), Q3Revision: prior.AfterRevision, Q3Epoch: prior.AfterEpoch, Before: core, Intent: intent}
	return x, capsule, nil
}

// The original caller passes the bytes it is already going to dispatch through
// the one named group. Duplicate mapping returns the ENTIRE actual peer image,
// including policy and recipe, without a frame/sequence/hash rewrite.
func storageOwnerV6NativePublication(actual [2][]byte, selected byte, evidence []byte, catalog storageOwnerCatalogV2, scope storageEnrollmentPeakScope, group uint16, core, peer []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) ([]byte, error) {
	capsule, before, lineage, policy, sparse, err := storageOwnerV6CaptureBefore(actual, selected, evidence, catalog, record, claim)
	if err != nil || scope[322] != selected {
		return nil, storagebudget.ErrBounds
	}
	shape, err := storageCompositionPublication(scope.path(), scope[322], group)
	if err != nil {
		return nil, err
	}
	outer, err := storageEnrollmentPeakDecodeEnvelopeNative(core, record, claim, true, true, true)
	if err != nil || outer.Scope != scope || outer.Kind != shape.CoreKind || outer.Marker != nil && outer.Marker.Phase != shape.MarkerPhase {
		return nil, storagebudget.ErrBounds
	}
	sequence, err := storageCompositionSequence(storageEnrollmentPeakU64(actual[selected][16:24]), shape.Offset)
	if err != nil {
		return nil, err
	}
	prior, err := storageOwnerV6NativeReadEndpoint(peer, evidence, record, claim)
	if err != nil || prior.Policy != policy || !bytes.Equal(prior.Overrides, sparse) || !reflect.DeepEqual(prior.Stage.Lineage, lineage) {
		return nil, storagebudget.ErrBounds
	}
	if shape.Duplicate {
		if prior.Packed != nil || prior.Stage.Sequence != sequence || prior.Stage.Transition != storageCompositionEnrollment || prior.Stage.TransitionID != storagebudget.ID([16]byte(scope[24:40])) || prior.Stage.BeforeBank != scope[322] || prior.Stage.Publication != shape.Publication || !bytes.Equal(prior.Stage.Core, core) {
			return nil, storagebudget.ErrBounds
		}
		return append([]byte{}, peer...), nil
	}
	if prior.Stage.Sequence == math.MaxUint64 || prior.Stage.Sequence+1 != sequence {
		return nil, storagebudget.ErrBounds
	}
	l, err := lineage.encode()
	if err != nil {
		return nil, err
	}
	previous, err := storageCompositionChecksumOf(peer)
	if err != nil {
		return nil, err
	}
	old, err := storageOwnerV6NativeReadEndpoint(actual[selected], evidence, record, claim)
	if err != nil {
		return nil, err
	}
	boundary := old.Boundary
	if outer.Kind == 2 && outer.Marker != nil && outer.Marker.Phase == 3 {
		boundary = storagePolicyQBoundary{outer.Marker.Q3Epoch, outer.Marker.Q3Revision}
	}
	v := storageOwnerV6NativeBank{NativeBase: storageEnrollmentPeakU64(actual[selected][16:24]), Boundary: boundary, Stage: storageCompositionBank{Sequence: sequence, CoreCodec: 2, Publication: shape.Publication, Transition: storageCompositionEnrollment, BeforeBank: scope[322], TransitionID: storagebudget.ID([16]byte(scope[24:40])), Previous: previous, CoreFull: storageCompositionCoreDigest(sha256.Sum256(core)), LineageDigest: sha256.Sum256(l), BeforePeer: storageCompositionDuplicateCorePeer(before, record, claim), Core: append([]byte{}, core...), Lineage: lineage}, Policy: policy, Overrides: append([]byte{}, sparse...)}
	v.Stage.Moves = old.Stage.Moves
	if outer.Kind < 2 {
		v.Before = &capsule
	}
	raw, err := v.encode(record, claim)
	if err != nil {
		return nil, err
	}
	current := [2][]byte{peer, peer}
	current[shape.Bank] = raw
	if _, err = storageOwnerV6NativeReadPair(current[0], current[1], &catalog, evidence, record, claim); err != nil {
		return nil, err
	}
	return raw, nil
}

// Completed native source is a different closed source encoding. Unlike the
// historical extractor, it retains an ACTUAL native before-pair recipe. The
// policy inverse supplies the old values; no new bytes are inferred from SHA.
func storageOwnerV6NativePolicySource(actual [2][]byte, selected byte, evidence []byte, catalog storageOwnerCatalogV2, record storageOwnerRecordV2, claim storageOwnerClaimV2) ([]byte, error) {
	capsule, core, lineage, _, _, err := storageOwnerV6CaptureBefore(actual, selected, evidence, catalog, record, claim)
	if err != nil || capsule.Kind != 2 && capsule.Kind != 3 {
		return nil, storagebudget.ErrBounds
	}
	physical, err := capsule.encode()
	if err != nil || capsule.Kind == 2 && len(physical) != 736 {
		return nil, storagebudget.ErrBounds
	}
	compact, err := storageCompositionCompactCore(core)
	if err != nil || len(compact) != 8320 {
		return nil, storagebudget.ErrBounds
	}
	block, err := lineage.encode()
	if err != nil {
		return nil, err
	}
	n := int(storageEnrollmentPeakU32(block[:4]))
	source := storageOwnerV6PackedSource{Version: 1, Encoding: 3, Core: append([]byte{}, compact...), Physical: physical, LineageRows: append([]byte{}, block[4:4+n]...), LineageSuffix: append([]byte{}, block[4+n:]...)}
	if capsule.Kind == 3 {
		source.Version, source.Encoding = 2, 4
		source.MoveOperands, err = storageMoveEncodeOperandTable(capsule.MoveOperands)
		if err != nil {
			return nil, err
		}
	}
	return json.Marshal(source)
}
func storageOwnerV6PackedSourceForPolicy(p storageOwnerV6PackedCatalog, evidence []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageOwnerCatalogV2, [2][]byte, storageCompositionLineage, storageEnrollmentPeakEvidence, error) {
	source, lineage, err := storageOwnerDecodePackedSource(p.Source)
	if err != nil || source.Encoding != 3 && source.Encoding != 4 {
		return storageOwnerPackedSourceImage(p.Source, evidence, record, claim)
	}
	fail := func(err error) (storageOwnerCatalogV2, [2][]byte, storageCompositionLineage, storageEnrollmentPeakEvidence, error) {
		return storageOwnerCatalogV2{}, [2][]byte{}, storageCompositionLineage{}, storageEnrollmentPeakEvidence{}, err
	}
	if p.TransitionKind != storageOwnerV6PolicyTransition {
		return fail(storagebudget.ErrBounds)
	}
	core := make([]byte, storageOwnerCatalogBytes)
	copy(core, source.Core)
	outer, err := storageEnrollmentPeakDecodeEnvelopeVariant(core, record, claim, true, true)
	if err != nil || outer.Kind != 2 || outer.Marker == nil || outer.Marker.Phase != 3 {
		return fail(storagebudget.ErrBounds)
	}
	e, err := storageEnrollmentPeakDecodeEvidenceWithLineage(evidence, &lineage, record, claim)
	if err != nil || storageEnrollmentPeakJoinMarker(outer, e, e.Ready) != nil || lineage.validateGraph(e.Inner.Catalog, record) != nil {
		return fail(storagebudget.ErrBounds)
	}
	_, defs, err := e.Inner.Catalog.definitions()
	if err != nil {
		return fail(err)
	}
	rows, err := storageOwnerV6DecodeSparse(p.Overrides, defs)
	if err != nil {
		return fail(err)
	}
	inverse, err := storagebudget.DecodePolicyTransition(p.PolicyTransition)
	if err != nil {
		return fail(err)
	}
	if inverse.Original.Installation != record.Installation || inverse.Original.Binding != record.QBinding {
		return fail(storagebudget.ErrBounds)
	}
	if inverse.Workspace != (storagebudget.ID{}) {
		rows, err = storageOwnerV6CanonicalSet(defs, rows, inverse.Workspace, inverse.WorkspaceGeneration, inverse.BeforeLimits)
		if err != nil {
			return fail(err)
		}
	}
	sparse, err := storageOwnerV6EncodeSparse(rows, defs)
	if err != nil {
		return fail(err)
	}
	capsule, err := storageOwnerV6ReadBefore(source.Physical)
	if err != nil || source.Encoding == 3 && capsule.Kind != 2 || source.Encoding == 4 && capsule.Kind != 3 {
		return fail(storagebudget.ErrBounds)
	}
	if source.Encoding == 4 {
		capsule.MoveOperands, err = storageMoveDecodeOperandTable(source.MoveOperands)
		if err != nil {
			return fail(err)
		}
	}
	pair, err := capsule.restore(core, lineage, storageOwnerV6HostPolicy(inverse.Before), sparse, e.Inner.Catalog, record, claim)
	if err != nil {
		return fail(err)
	}
	physical, err := storageOwnerV6NativeReadPair(pair[0], pair[1], nil, nil, record, claim)
	if err != nil || physical.RecoveryOnly || physical.Selected != capsule.Selected {
		return fail(storagebudget.ErrBounds)
	}
	if p.Phase == "prepared" && (p.Sequence != physical.Banks[capsule.Selected].Stage.Sequence+1 || !bytes.Equal(p.PreviousCatalogDigest[:], pair[capsule.Selected][32:64])) {
		return fail(storagebudget.ErrBounds)
	}
	return e.Inner.Catalog, pair, lineage, e, nil
}

// This is the explicit native->policy activation comparison, before any of the
// existing five policy groups. The original policy operation/Q commitment and
// both held native descriptors are still checked by its existing issuer.
func storageOwnerV6NativePolicyEntry(actual [2][]byte, selected byte, evidence []byte, catalog storageOwnerCatalogV2, prepared storageOwnerV6PackedCatalog, record storageOwnerRecordV2, claim storageOwnerClaimV2) error {
	if selected > 1 || len(actual[0]) != storageOwnerCatalogBytes || len(actual[1]) != storageOwnerCatalogBytes || prepared.Phase != "prepared" || prepared.TransitionKind != storageOwnerV6PolicyTransition || storageEnrollmentPeakU64(actual[selected][16:24]) == math.MaxUint64 || prepared.Sequence != storageEnrollmentPeakU64(actual[selected][16:24])+1 || !bytes.Equal(prepared.PreviousCatalogDigest[:], actual[selected][32:64]) {
		return storagebudget.ErrBounds
	}
	source, err := storageOwnerV6NativePolicySource(actual, selected, evidence, catalog, record, claim)
	if err != nil || !bytes.Equal(source, prepared.Source) {
		return storagebudget.ErrBounds
	}
	if _, _, _, err = prepared.validate(evidence, record, claim); err != nil {
		return err
	}
	original, err := storageOwnerV6NativeReadEndpoint(actual[selected], evidence, record, claim)
	if err != nil {
		return err
	}
	inverse, err := storagebudget.DecodePolicyTransition(prepared.PolicyTransition)
	if err != nil || !original.Boundary.valid() || inverse.Original.Epoch != original.Boundary.Epoch || inverse.Original.Revision < original.Boundary.MinimumRevision {
		return storagebudget.ErrBounds
	}
	_, pair, _, _, err := storageOwnerV6PackedSourceForPolicy(prepared, evidence, record, claim)
	if err != nil || !bytes.Equal(pair[0], actual[0]) || !bytes.Equal(pair[1], actual[1]) {
		return storagebudget.ErrBounds
	}
	return nil
}

// Restart's first-Fence join consumes only the still-bound original evidence
// and the durable immediate recipe. It compares BOTH complete actual originals
// before returning any source image. The caller still owns descriptor identity,
// freshness, original scope/inputs, Q/currentness and original remaining groups.
func storageOwnerV6NativeCompareSnapshot(v storageOwnerV6NativeBank, previousRaw []byte, actual [2][]byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) error {
	if v.Packed != nil || v.Before == nil || v.validate(record, claim) != nil {
		return storagebudget.ErrBounds
	}
	e, err := storageEnrollmentPeakDecodeEnvelopeNative(v.Stage.Core, record, claim, true, true, true)
	if err != nil || e.Snapshot == nil || e.Snapshot.Encoding != 6 {
		return storagebudget.ErrBounds
	}
	x := e.Snapshot
	previous, err := storageEnrollmentPeakDecodeEvidenceWithLineage(previousRaw, &v.Stage.Lineage, record, claim)
	if err != nil || previous.Phase != 2 || x.Bank != previous.Bank || x.Evidence != previous.Controls[previous.Bank] || x.Extent != previous.Extent || x.EvidenceDigest != sha256.Sum256(previousRaw) || x.Inner != storageEnrollmentPeakInnerDigest(previous.Inner.Raw) || x.Q3Revision != previous.AfterRevision || x.Q3Epoch != previous.AfterEpoch {
		return storagebudget.ErrBounds
	}
	old, err := storageEnrollmentPeakDecodeEnvelopeVariant(x.Before, record, claim, true, true)
	if err != nil || storageEnrollmentPeakJoinMarker(old, previous, previous.Ready) != nil {
		return storagebudget.ErrBounds
	}
	pair, err := v.Before.restore(x.Before, v.Stage.Lineage, v.Policy, v.Overrides, previous.Inner.Catalog, record, claim)
	if err != nil || v.NativeBase != storageEnrollmentPeakU64(v.Before.Headers[v.Before.Selected][16:24]) {
		return storagebudget.ErrBounds
	}
	for i := range pair {
		if !bytes.Equal(pair[i], actual[i]) {
			return storagebudget.ErrBounds
		}
	}
	return nil
}

// Evidence3's selected before projection names an actual native pair. Keep
// this separate from compositionBefore: its return values must never masquerade
// as DSKONL1 banks or grant original control custody.
func storagePolicyEvidenceNativeBefore(e storageEnrollmentPeakEvidence, record storageOwnerRecordV2, claim storageOwnerClaimV2) ([2][]byte, byte, error) {
	var actual [2][]byte
	if e.Version != 3 || e.CompositionLineage == nil {
		return actual, 0, storagebudget.ErrBounds
	}
	found := false
	selected := byte(0)
	for _, p := range e.Projections {
		if storageEnrollmentPeakProjectionFullDigest(p) != e.Before {
			continue
		}
		if found {
			return [2][]byte{}, 0, storagebudget.ErrBounds
		}
		found = true
		var err error
		if p.Native != nil {
			if p.Native.Record != record || p.Native.Claim != claim {
				return [2][]byte{}, 0, storagebudget.ErrBounds
			}
			actual, err = storagePolicyNativeProjectionPair(p)
			selected = p.Native.Before.Selected
		} else if p.Policy != nil {
			actual, err = storagePolicyProjectionPair(p)
			selected = p.Policy.SelectedBank
		} else {
			return [2][]byte{}, 0, storagebudget.ErrBounds
		}
		if err != nil {
			return [2][]byte{}, 0, err
		}
	}
	if !found || selected != e.Scope[322] || sha256.Sum256(actual[selected]) != e.Before {
		return [2][]byte{}, 0, storagebudget.ErrBounds
	}
	return actual, selected, nil
}

// Immutable source continuity and effective values are separate joins. This
// uses the original E1 append relation, never rewrites its embedded Policy, and
// lets only new members capture the current future default exactly once.
func storageOwnerV6NativeSourceJoin(before storageEnrollmentPeakProjection, after storageOwnerCatalogV2, policy storageOwnerPolicy, sparse []byte) (storageOwnerDefinitions, storageOwnerDefinitions, error) {
	var empty storageOwnerDefinitions
	if before.Native == nil && before.Policy == nil || !storageEnrollmentRetainsOriginal(before.Catalog, after) {
		return empty, empty, storagebudget.ErrBounds
	}
	oldSet := storagePolicyProjectionSet(before)
	if oldSet.Policy != policy {
		return empty, empty, storagebudget.ErrBounds
	}
	_, old, err := before.Catalog.definitions()
	if err != nil {
		return empty, empty, err
	}
	_, next, err := after.definitions()
	if err != nil {
		return empty, empty, err
	}
	rows, err := storageOwnerV6DecodeSparse(sparse, old)
	if err != nil {
		return empty, empty, err
	}
	previous, err := storageOwnerV6Expand(old, policy, rows)
	if err != nil {
		return empty, empty, err
	}
	if len(oldSet.Workspaces) != len(previous.Workspaces) {
		return empty, empty, storagebudget.ErrBounds
	}
	for _, w := range previous.Workspaces {
		found := false
		for _, actual := range oldSet.Workspaces {
			if actual.ID == w.ID {
				if found || actual != w {
					return empty, empty, storagebudget.ErrBounds
				}
				found = true
			}
		}
		if !found {
			return empty, empty, storagebudget.ErrBounds
		}
	}
	retained := map[storagebudget.ID]bool{}
	for _, w := range old.Workspaces {
		retained[w.ID] = true
	}
	for _, w := range next.Workspaces {
		if !retained[w.ID] && w.Limits != policy.Workspace {
			return empty, empty, storagebudget.ErrBounds
		}
	}
	current, err := storageOwnerV6Expand(next, policy, rows)
	if err != nil {
		return empty, empty, err
	}
	return previous, current, nil
}

// Strict NativeReadPair already checked complete original links and one append.
func storagePolicyNativePreparedLineage(pair storageOwnerV6NativePair) bool {
	high := pair.Banks[pair.Selected]
	return pair.RecoveryOnly && !pair.Duplicated && high.Packed == nil && high.Stage.Publication == storageCompositionPrepared && high.Stage.Transition >= storageCompositionPlan && high.Stage.Transition <= storageCompositionContinuation
}
