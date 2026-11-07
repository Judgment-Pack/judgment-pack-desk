package desk

// All values in this codec are detached comparison data. Decoding does not
// establish original custody, native qualification, a reservation or Q authority.
import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"reflect"
	"strconv"
	"unicode/utf8"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

const (
	storageEnrollmentPeakScopeBytes    = 4096
	storageEnrollmentPeakEvidenceMax   = 71303168
	storageEnrollmentPeakProjectionMax = 135168
	storageEnrollmentPeakChunk         = 35651584
)

var storageEnrollmentPeakSectionMax = [9]uint64{8388608, 8388608, 35143680, 1081344, 8921088, 2113536, 2097152, 2097152, 65536}

// V1 limits remain byte-for-byte encoded in historical packets. V2 changes
// only Section7, retaining the unchanged total evidence ceiling.
func storageEnrollmentPeakSectionMaximum(s storageEnrollmentPeakScope, i int) uint64 {
	if i == 6 && s.hasOriginalPeer() {
		return 4718592
	}
	return storageEnrollmentPeakSectionMax[i]
}

func storageEnrollmentPeakEvidenceSectionMaximum(version uint32, s storageEnrollmentPeakScope, i int) uint64 {
	if (version == 2 || version == 3) && i == 4 {
		return 8968080
	}
	return storageEnrollmentPeakSectionMaximum(s, i)
}

var storageEnrollmentPeakSectionCounts = [9]uint32{1, 1, 4224, 66, 66, 128, 1, 1, 1}

func storageEnrollmentPeakZero(b []byte) bool {
	return storageAllZero(b)
}
func storageEnrollmentPeakHash(domain string, b []byte) storagebudget.Digest {
	h := sha256.New()
	h.Write([]byte(domain))
	h.Write([]byte{0})
	h.Write(b)
	var d storagebudget.Digest
	copy(d[:], h.Sum(nil))
	return d
}
func storageEnrollmentPeakU16(b []byte) uint16      { return binary.LittleEndian.Uint16(b) }
func storageEnrollmentPeakU32(b []byte) uint32      { return binary.LittleEndian.Uint32(b) }
func storageEnrollmentPeakU64(b []byte) uint64      { return binary.LittleEndian.Uint64(b) }
func storageEnrollmentPeakPut16(b []byte, v uint16) { binary.LittleEndian.PutUint16(b, v) }
func storageEnrollmentPeakPut32(b []byte, v uint32) { binary.LittleEndian.PutUint32(b, v) }
func storageEnrollmentPeakPut64(b []byte, v uint64) { binary.LittleEndian.PutUint64(b, v) }
func storageEnrollmentPeakNative(b []byte) storageOwnerPersistentObject {
	var p storageOwnerPersistentObject
	if len(b) != 35 {
		return p
	}
	p.Backend = b[0]
	copy(p.VolumeUUID[:], b[1:17])
	p.Object = storageEnrollmentPeakU64(b[17:25])
	p.Incarnation = storageEnrollmentPeakU64(b[25:33])
	p.IncarnationKind = b[33]
	p.ObjectKind = b[34]
	return p
}
func storageEnrollmentPeakNativeBytes(p storageOwnerPersistentObject) []byte {
	b := make([]byte, 35)
	b[0] = p.Backend
	copy(b[1:17], p.VolumeUUID[:])
	storageEnrollmentPeakPut64(b[17:25], p.Object)
	storageEnrollmentPeakPut64(b[25:33], p.Incarnation)
	b[33] = p.IncarnationKind
	b[34] = p.ObjectKind
	return b
}

type storageEnrollmentPeakReader struct {
	raw []byte
	pos int
	bad bool
}

func (r *storageEnrollmentPeakReader) take(n int) []byte {
	if r.bad || n < 0 || n > len(r.raw)-r.pos {
		r.bad = true
		return nil
	}
	b := r.raw[r.pos : r.pos+n]
	r.pos += n
	return b
}
func (r *storageEnrollmentPeakReader) u8() byte {
	b := r.take(1)
	if len(b) == 0 {
		return 0
	}
	return b[0]
}
func (r *storageEnrollmentPeakReader) u16() uint16 {
	b := r.take(2)
	if len(b) == 0 {
		return 0
	}
	return storageEnrollmentPeakU16(b)
}
func (r *storageEnrollmentPeakReader) u32() uint32 {
	b := r.take(4)
	if len(b) == 0 {
		return 0
	}
	return storageEnrollmentPeakU32(b)
}
func (r *storageEnrollmentPeakReader) u64() uint64 {
	b := r.take(8)
	if len(b) == 0 {
		return 0
	}
	return storageEnrollmentPeakU64(b)
}
func (r *storageEnrollmentPeakReader) id() (v storagebudget.ID) { copy(v[:], r.take(16)); return }
func (r *storageEnrollmentPeakReader) digest() (v storagebudget.Digest) {
	copy(v[:], r.take(32))
	return
}
func (r *storageEnrollmentPeakReader) native() storageOwnerPersistentObject {
	return storageEnrollmentPeakNative(r.take(35))
}
func (r *storageEnrollmentPeakReader) data(max int) []byte {
	n := int(r.u16())
	if n > max {
		r.bad = true
		return nil
	}
	return r.take(n)
}
func (r *storageEnrollmentPeakReader) done() bool { return !r.bad && r.pos == len(r.raw) }
func (r *storageEnrollmentPeakReader) zeroTail() bool {
	return !r.bad && storageEnrollmentPeakZero(r.raw[r.pos:])
}

type storageEnrollmentPeakWriter struct {
	raw []byte
	max int
	bad bool
}

func (w *storageEnrollmentPeakWriter) put(b []byte) {
	if w.bad || len(b) > w.max-len(w.raw) {
		w.bad = true
		return
	}
	w.raw = append(w.raw, b...)
}
func (w *storageEnrollmentPeakWriter) u8(v byte) { w.put([]byte{v}) }
func (w *storageEnrollmentPeakWriter) u16(v uint16) {
	var b [2]byte
	storageEnrollmentPeakPut16(b[:], v)
	w.put(b[:])
}
func (w *storageEnrollmentPeakWriter) u32(v uint32) {
	var b [4]byte
	storageEnrollmentPeakPut32(b[:], v)
	w.put(b[:])
}
func (w *storageEnrollmentPeakWriter) u64(v uint64) {
	var b [8]byte
	storageEnrollmentPeakPut64(b[:], v)
	w.put(b[:])
}
func (w *storageEnrollmentPeakWriter) data(b []byte, max int) {
	if len(b) > max || len(b) > 65535 {
		w.bad = true
		return
	}
	w.u16(uint16(len(b)))
	w.put(b)
}

// The imported fixed DTOs use LE scalar widths, exact arrays, no counts for
// fixed arrays and U16 counts for their bounded dynamic arrays. Byte slices
// and strings use U16 lengths; no caller-supplied type is accepted by entry points.
func storageEnrollmentPeakWriteFixed(w *storageEnrollmentPeakWriter, v reflect.Value) {
	switch v.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			storageEnrollmentPeakWriteFixed(w, v.Field(i))
		}
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			storageEnrollmentPeakWriteFixed(w, v.Index(i))
		}
	case reflect.Uint8:
		w.u8(byte(v.Uint()))
	case reflect.Uint16:
		w.u16(uint16(v.Uint()))
	case reflect.Uint32:
		w.u32(uint32(v.Uint()))
	case reflect.Uint64:
		w.u64(v.Uint())
	case reflect.Bool:
		if v.Bool() {
			w.u8(1)
		} else {
			w.u8(0)
		}
	case reflect.String:
		if !utf8.ValidString(v.String()) {
			w.bad = true
			return
		}
		w.data([]byte(v.String()), 65535)
	case reflect.Slice:
		if v.IsNil() || v.Len() > 65534 {
			w.bad = true
			return
		}
		w.u16(uint16(v.Len()))
		for i := 0; i < v.Len(); i++ {
			storageEnrollmentPeakWriteFixed(w, v.Index(i))
		}
	default:
		w.bad = true
	}
}
func storageEnrollmentPeakReadFixed(r *storageEnrollmentPeakReader, v reflect.Value) {
	switch v.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			storageEnrollmentPeakReadFixed(r, v.Field(i))
		}
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			storageEnrollmentPeakReadFixed(r, v.Index(i))
		}
	case reflect.Uint8:
		v.SetUint(uint64(r.u8()))
	case reflect.Uint16:
		v.SetUint(uint64(r.u16()))
	case reflect.Uint32:
		v.SetUint(uint64(r.u32()))
	case reflect.Uint64:
		v.SetUint(r.u64())
	case reflect.Bool:
		b := r.u8()
		if b > 1 {
			r.bad = true
		}
		v.SetBool(b == 1)
	default:
		r.bad = true // Dynamic members have a field-specific bounded reader.
	}
}

func storageEnrollmentPeakFrame(magic string, payload []byte, sequence uint64, full bool) ([]byte, error) {
	if len(magic) != 8 || sequence == 0 || len(payload) > storageOwnerCatalogBytes-64 {
		return nil, storagebudget.ErrBounds
	}
	n := 64 + len(payload)
	if full {
		n = storageOwnerCatalogBytes
	}
	b := make([]byte, n)
	copy(b[:8], magic)
	storageEnrollmentPeakPut32(b[8:12], 1)
	storageEnrollmentPeakPut32(b[12:16], 64)
	storageEnrollmentPeakPut64(b[16:24], sequence)
	storageEnrollmentPeakPut32(b[24:28], uint32(len(payload)))
	copy(b[64:], payload)
	h := sha256.New()
	h.Write(b[:32])
	h.Write(payload)
	copy(b[32:64], h.Sum(nil))
	return b, nil
}
func storageEnrollmentPeakPayload(b []byte, magic string, full bool) ([]byte, uint64, error) {
	if len(b) < 64 || len(b) > storageOwnerCatalogBytes || full && len(b) != storageOwnerCatalogBytes || !bytes.Equal(b[:8], []byte(magic)) || storageEnrollmentPeakU32(b[8:12]) != 1 || storageEnrollmentPeakU32(b[12:16]) != 64 || !storageEnrollmentPeakZero(b[28:32]) {
		return nil, 0, storagebudget.ErrBounds
	}
	n := uint64(storageEnrollmentPeakU32(b[24:28]))
	seq := storageEnrollmentPeakU64(b[16:24])
	if seq == 0 || n > uint64(len(b)-64) || !full && n != uint64(len(b)-64) || !storageEnrollmentPeakZero(b[64+int(n):]) {
		return nil, 0, storagebudget.ErrBounds
	}
	h := sha256.New()
	h.Write(b[:32])
	h.Write(b[64 : 64+int(n)])
	if !bytes.Equal(h.Sum(nil), b[32:64]) {
		return nil, 0, storagebudget.ErrBounds
	}
	return b[64 : 64+int(n)], seq, nil
}

// Reversible P catalog packing shares this codec source and its source pin.
const storageEnrollmentPeakOldPayloadMax = storageOwnerCatalogBytes - 64

// The only accepted root is the pinned closed V3 declaration tree. Reflection
// does not permit callers to select a type, omit a member or add an extension.
func storageEnrollmentPeakPackV3(raw []byte) ([]byte, error) {
	return storageEnrollmentPeakPackCatalog(raw, 3)
}

// PackInner has the identical pinned primitive tree and its own Version1
// root. It cannot make an old V3 frame or decoder accept a new-family value.
func storageEnrollmentPeakPackInner(raw []byte) ([]byte, error) {
	return storageEnrollmentPeakPackCatalog(raw, 1)
}

func storageEnrollmentPeakPackCatalog(raw []byte, version uint32) ([]byte, error) {
	var value storageOwnerCatalogV2
	decode := storageOwnerCatalogJSON
	if version == 1 {
		decode = storageEnrollmentPeakCatalogJSON
	}
	if version != 1 && version != 3 || decode(raw, &value) != nil || value.Version != version {
		return nil, storagebudget.ErrBounds
	}
	out := make([]byte, 0, len(raw))
	if !storageEnrollmentPeakPackValue(&out, reflect.ValueOf(value)) || len(out) > storageEnrollmentPeakOldPayloadMax {
		return nil, storagebudget.ErrBounds
	}
	return out, nil
}

func storageEnrollmentPeakPackUint(out *[]byte, n uint64) {
	for n >= 128 {
		*out = append(*out, byte(n)|128)
		n >>= 7
	}
	*out = append(*out, byte(n))
}

func storageEnrollmentPeakPackValue(out *[]byte, v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if !storageEnrollmentPeakPackValue(out, v.Field(i)) {
				return false
			}
		}
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if !storageEnrollmentPeakPackValue(out, v.Index(i)) {
				return false
			}
		}
	case reflect.Slice:
		if v.IsNil() {
			return false
		}
		storageEnrollmentPeakPackUint(out, uint64(v.Len()))
		if v.Type().Elem().Kind() == reflect.Uint8 {
			*out = append(*out, v.Bytes()...)
		} else {
			for i := 0; i < v.Len(); i++ {
				if !storageEnrollmentPeakPackValue(out, v.Index(i)) {
					return false
				}
			}
		}
	case reflect.String:
		s := v.String()
		if !utf8.ValidString(s) {
			return false
		}
		storageEnrollmentPeakPackUint(out, uint64(len(s)))
		*out = append(*out, s...)
	case reflect.Bool:
		if v.Bool() {
			*out = append(*out, 1)
		} else {
			*out = append(*out, 0)
		}
	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		storageEnrollmentPeakPackUint(out, v.Uint())
	default:
		return false
	}
	return len(*out) <= storageEnrollmentPeakOldPayloadMax
}

type storageEnrollmentPeakUnpacker struct {
	raw           []byte
	pos           int
	limit         int
	out           []byte
	transferPaths bool
}

// Restore canonical JSON under a hard output budget before the original
// preflight may allocate any DTO slices. Counts/bytes/text are independently
// bounded before expansion, and every packed byte must be consumed.
func storageEnrollmentPeakUnpackV3(raw []byte, originalLength uint32) ([]byte, error) {
	if len(raw) > storageEnrollmentPeakOldPayloadMax || originalLength == 0 || originalLength > storageEnrollmentPeakOldPayloadMax {
		return nil, storagebudget.ErrBounds
	}
	return storageEnrollmentPeakUnpackCatalog(raw, int(originalLength), true, 3)
}

func storageEnrollmentPeakUnpackInner(raw []byte) ([]byte, error) {
	if len(raw) > storageEnrollmentPeakOldPayloadMax {
		return nil, storagebudget.ErrBounds
	}
	return storageEnrollmentPeakUnpackCatalog(raw, storageEnrollmentPeakOldPayloadMax, false, 1)
}

func storageEnrollmentPeakUnpackCatalog(raw []byte, limit int, exact bool, version uint32) ([]byte, error) {
	p := storageEnrollmentPeakUnpacker{raw: raw, limit: limit, transferPaths: version == 1}
	if !p.value(reflect.TypeOf(storageOwnerCatalogV2{}), "") || p.pos != len(raw) || exact && len(p.out) != p.limit {
		return nil, storagebudget.ErrBounds
	}
	var value storageOwnerCatalogV2
	decode := storageOwnerCatalogJSON
	if version == 1 {
		decode = storageEnrollmentPeakCatalogJSON
	}
	if decode(p.out, &value) != nil || value.Version != version {
		return nil, storagebudget.ErrBounds
	}
	return p.out, nil
}

// Reject overflow as well as nonshortest encodings, including a tenth byte
// other than 0/1. The terminal zero of a multibyte value is never shortest.
func (p *storageEnrollmentPeakUnpacker) uint(bits int) (uint64, bool) {
	var n uint64
	for i := 0; i < 10; i++ {
		if p.pos >= len(p.raw) {
			return 0, false
		}
		b := p.raw[p.pos]
		p.pos++
		if i == 9 && b > 1 {
			return 0, false
		}
		n |= uint64(b&127) << (7 * i)
		if b < 128 {
			if i > 0 && b == 0 || bits < 64 && n >= uint64(1)<<bits {
				return 0, false
			}
			return n, true
		}
	}
	return 0, false
}

func (p *storageEnrollmentPeakUnpacker) append(raw []byte) bool {
	if len(raw) > p.limit-len(p.out) {
		return false
	}
	p.out = append(p.out, raw...)
	return true
}

func storageEnrollmentPeakListLimit(field string) int {
	switch field {
	case "Objects", "OwnerControls", "ExistingObjects":
		return 8192
	case "Roots":
		return 4096
	case "Workspaces", "Entries", "References":
		return 1024
	case "Filesystems":
		return 128
	case "Constraints":
		return 512
	case "Profiles":
		return 64
	}
	return -1
}

func storageEnrollmentPeakByteLimit(field string) int {
	switch field {
	case "Name":
		return 1024
	case "Folder":
		return 120
	case "WorkspaceOwnerKey":
		return 256
	case "Relative":
		return 65599
	case "ConfigPath", "HomePath", "HistoryIdentity", "Path", "Previous", "OwnerKey":
		return 4096
	}
	return -1
}

func storageEnrollmentPeakTextLimit(field string) int {
	switch field {
	case "Phase":
		return 8
	case "HomeOwner", "StorageOwner", "RunnerWorkspace":
		return 64
	case "DeskID":
		return 32
	case "Locator":
		return 256
	}
	return -1
}

func (p *storageEnrollmentPeakUnpacker) bytes(limit int) ([]byte, bool) {
	n, ok := p.uint(64)
	if !ok || limit < 0 || n > uint64(limit) || n > uint64(len(p.raw)-p.pos) {
		return nil, false
	}
	raw := p.raw[p.pos : p.pos+int(n)]
	p.pos += int(n)
	return raw, true
}

func (p *storageEnrollmentPeakUnpacker) value(t reflect.Type, field string) bool {
	switch t.Kind() {
	case reflect.Struct:
		if !p.append([]byte{'{'}) {
			return false
		}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if i != 0 && !p.append([]byte{','}) {
				return false
			}
			childField := f.Name
			if t == reflect.TypeOf(storageOwnerWorkspace{}) && childField == "OwnerKey" {
				childField = "WorkspaceOwnerKey"
			}
			if !p.append([]byte(`"`+f.Name+`":`)) || !p.value(f.Type, childField) {
				return false
			}
		}
		return p.append([]byte{'}'})
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			limit := storageEnrollmentPeakByteLimit(field)
			if p.transferPaths && (field == "Path" || field == "Previous") {
				limit = storageTransferPointerBytes
			}
			raw, ok := p.bytes(limit)
			encodedLength := base64.StdEncoding.EncodedLen(len(raw))
			if !ok || encodedLength+2 > p.limit-len(p.out) {
				return false
			}
			p.out = append(p.out, '"')
			p.out = base64.StdEncoding.AppendEncode(p.out, raw)
			p.out = append(p.out, '"')
			return true
		}
		n, ok := p.uint(64)
		limit := storageEnrollmentPeakListLimit(field)
		if !ok || limit < 0 || n > uint64(limit) || n > uint64(len(p.raw)-p.pos) {
			return false
		}
		return p.array(t.Elem(), int(n), field)
	case reflect.Array:
		return p.array(t.Elem(), t.Len(), field)
	case reflect.String:
		raw, ok := p.bytes(storageEnrollmentPeakTextLimit(field))
		if !ok || !utf8.Valid(raw) {
			return false
		}
		// json.Marshal preserves the original HTML/control escaping rules.
		encoded, err := json.Marshal(string(raw))
		return err == nil && p.append(encoded)
	case reflect.Bool:
		if p.pos >= len(p.raw) || p.raw[p.pos] > 1 {
			return false
		}
		b := p.raw[p.pos]
		p.pos++
		if b == 0 {
			return p.append([]byte("false"))
		}
		return p.append([]byte("true"))
	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, ok := p.uint(t.Bits())
		return ok && p.append(strconv.AppendUint(nil, n, 10))
	}
	return false
}

func (p *storageEnrollmentPeakUnpacker) array(t reflect.Type, count int, field string) bool {
	if !p.append([]byte{'['}) {
		return false
	}
	for i := 0; i < count; i++ {
		if i != 0 && !p.append([]byte{','}) {
			return false
		}
		if !p.value(t, field) {
			return false
		}
	}
	return p.append([]byte{']'})
}
