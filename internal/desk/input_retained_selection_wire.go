package desk

// Inactive transport primitives only. The caller must supply already admitted
// source identity and B3 InputJSON; hashes establish byte consistency, not trust.
import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"io"
	"math"
	"regexp"
	"unicode/utf8"
)

const retainedSelectionMediaType = "application/vnd.jpack-desk.retained-selection.v1"
const retainedSelectionMagic = "JPACK-RS/1\n"
const retainedSelectionIDsDomain = "jpack-desk/retained-selection/1:ids\x00"
const retainedSelectionIndexDomain = "jpack-desk/retained-selection/1:index\x00"
const retainedSelectionRequestBytes = 2 << 20         // Existing inputRequest owner.
const retainedSelectionMaxInputBytes int64 = 37748736 // Base inputReplyBytes owner.

var retainedSelectionViewID = regexp.MustCompile(`^rv_[a-f0-9]{48}$`)
var retainedSelectionSHA = regexp.MustCompile(`^[a-f0-9]{64}$`)
var errRetainedSelectionWire = errors.New("Invalid or incomplete retained selection stream.")

// Explicit DTOs own the key order, independently of external helper layout.
type retainedSelectionWireSpec struct {
	Version      string `json:"version"`
	Revision     string `json:"revision"`
	SchemaSHA256 string `json:"schemaSha256"`
	CorpusSHA256 string `json:"corpusSha256"`
}
type retainedSelectionWireContract struct {
	Contract string                    `json:"contract"`
	Spec     retainedSelectionWireSpec `json:"spec"`
	Profile  string                    `json:"profile"`
}

func retainedSelectionInstalledContract() retainedSelectionWireContract {
	v := retainedInstalledSelector()
	return retainedSelectionWireContract{v.Contract, retainedSelectionWireSpec{v.Spec.Version, v.Spec.Revision, v.Spec.SchemaSHA256, v.Spec.CorpusSHA256}, v.Profile}
}

type retainedSelectionWireSource struct {
	PreparationID      string
	Revision           string
	PackDigest         string
	ConversionContract retainedSelectionWireContract
	Origin             string
	RecordIDs          []string
	// Actual captured request byte count, not a caller-selected wire limit.
	RequestBytes int64
}
type retainedSelectionWireHeader struct {
	WireVersion        int                           `json:"wireVersion"`
	ViewID             string                        `json:"viewId"`
	PreparationID      string                        `json:"preparationId"`
	Revision           string                        `json:"revision"`
	PackDigest         string                        `json:"packDigest"`
	ConversionContract retainedSelectionWireContract `json:"conversionContract"`
	Origin             string                        `json:"origin"`
	Rows               int                           `json:"rows"`
	MaxInputBytes      int64                         `json:"maxInputBytes"`
	SelectionSHA256    string                        `json:"selectionSha256"`
}
type retainedSelectionWireRow struct {
	Index       int    `json:"index"`
	ID          string `json:"id"`
	InputBytes  int64  `json:"inputBytes"`
	InputSHA256 string `json:"inputSha256"`
}
type retainedSelectionWireEnd struct {
	Rows            int    `json:"rows"`
	InputBytesTotal int64  `json:"inputBytesTotal"`
	IndexSHA256     string `json:"indexSha256"`
}
type retainedSelectionWireDescriptor struct {
	retainedSelectionWireRow
	Offset int64 // Raw Input offset in the complete stream.
}

func retainedSelectionAdd(a, b int64) (int64, error) {
	if a < 0 || b < 0 || a > math.MaxInt64-b {
		return 0, errRetainedSelectionWire
	}
	return a + b, nil
}
func retainedSelectionU32(w io.Writer, n uint32) error {
	var raw [4]byte
	binary.BigEndian.PutUint32(raw[:], n)
	return retainedSelectionWrite(w, raw[:])
}
func retainedSelectionWrite(w io.Writer, raw []byte) error {
	n, err := w.Write(raw)
	if err == nil && n != len(raw) {
		return io.ErrShortWrite
	}
	return err
}
func retainedSelectionHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func retainedSelectionSource(source retainedSelectionWireSource) (retainedSelectionWireSource, error) {
	if !inputPreparationID.MatchString(source.PreparationID) || !retainedSelectionSHA.MatchString(source.Revision) || !retainedSpecDigest.MatchString(source.PackDigest) || source.ConversionContract != retainedSelectionInstalledContract() || (source.Origin != "asserted" && source.Origin != "generated") || len(source.RecordIDs) < 1 || len(source.RecordIDs) > inputRecordLimit || source.RequestBytes < 1 || source.RequestBytes > retainedSelectionRequestBytes {
		return source, errRetainedSelectionWire
	}
	var total int64
	seen := make(map[string]bool, len(source.RecordIDs))
	for _, id := range source.RecordIDs {
		if !utf8.ValidString(id) || id == "" || seen[id] {
			return source, errRetainedSelectionWire
		}
		seen[id] = true
		var err error
		total, err = retainedSelectionAdd(total, int64(len(id)))
		if err != nil || total > source.RequestBytes {
			return source, errRetainedSelectionWire
		}
	}
	source.RecordIDs = append([]string(nil), source.RecordIDs...)
	return source, nil
}
func retainedSelectionIDs(ids []string) string {
	h := sha256.New()
	_, _ = io.WriteString(h, retainedSelectionIDsDomain)
	_ = retainedSelectionU32(h, uint32(len(ids)))
	for _, id := range ids {
		_ = retainedSelectionU32(h, uint32(len(id)))
		_, _ = io.WriteString(h, id)
	}
	return hex.EncodeToString(h.Sum(nil))
}
func retainedSelectionHeader(source retainedSelectionWireSource, viewID string) retainedSelectionWireHeader {
	return retainedSelectionWireHeader{1, viewID, source.PreparationID, source.Revision, source.PackDigest, source.ConversionContract, source.Origin, len(source.RecordIDs), retainedSelectionMaxInputBytes, retainedSelectionIDs(source.RecordIDs)}
}
func retainedSelectionIndex(header []byte, rows int) hash.Hash {
	h := sha256.New()
	_, _ = io.WriteString(h, retainedSelectionIndexDomain)
	sum := sha256.Sum256(header)
	_, _ = h.Write(sum[:])
	_ = retainedSelectionU32(h, uint32(rows))
	return h
}
func retainedSelectionIndexRow(h hash.Hash, row retainedSelectionWireRow) {
	_ = retainedSelectionU32(h, uint32(row.Index))
	_ = retainedSelectionU32(h, uint32(len(row.ID)))
	_, _ = io.WriteString(h, row.ID)
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(row.InputBytes))
	_, _ = h.Write(size[:])
	digest, _ := hex.DecodeString(row.InputSHA256)
	_, _ = h.Write(digest)
}
func retainedSelectionJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v) // Default Go HTML and U+2028/U+2029 escaping.
	if err != nil || uint64(len(raw)) > math.MaxUint32 {
		return nil, errRetainedSelectionWire
	}
	return raw, nil
}

// Exact DTO-derived bounds, using the actual captured IDs and installed tuple.
// No arbitrary per-ID/header allowance and no aggregate application cap.
func retainedSelectionBodyBound(source retainedSelectionWireSource) (int64, int, int, error) {
	header, err := retainedSelectionJSON(retainedSelectionHeader(source, "rv_000000000000000000000000000000000000000000000000"))
	if err != nil {
		return 0, 0, 0, err
	}
	total := int64(20 + len(header))
	var inputs int64
	for i, id := range source.RecordIDs {
		metadata, err := retainedSelectionJSON(retainedSelectionWireRow{i, id, retainedSelectionMaxInputBytes, string(bytes.Repeat([]byte{'0'}, 64))})
		if err != nil {
			return 0, 0, 0, err
		}
		total, err = retainedSelectionAdd(total, 5+int64(len(metadata))+retainedSelectionMaxInputBytes)
		if err != nil {
			return 0, 0, 0, err
		}
		inputs, err = retainedSelectionAdd(inputs, retainedSelectionMaxInputBytes)
		if err != nil {
			return 0, 0, 0, err
		}
	}
	end, err := retainedSelectionJSON(retainedSelectionWireEnd{len(source.RecordIDs), inputs, string(bytes.Repeat([]byte{'0'}, 64))})
	if err != nil {
		return 0, 0, 0, err
	}
	total, err = retainedSelectionAdd(total, int64(len(end)))
	return total, len(header), len(end), err
}

// A writer accepts one exact payload at a time and retains no payload bytes.
// Any failure poisons it; its sink/spool must be discarded, never retried in place.
type retainedSelectionWireWriter struct {
	out      io.Writer
	source   retainedSelectionWireSource
	index    hash.Hash
	next     int
	bytes    int64
	inputs   int64
	bound    int64
	failed   bool
	finished bool
}

func newRetainedSelectionWireWriter(out io.Writer, source retainedSelectionWireSource, viewID string) (*retainedSelectionWireWriter, error) {
	source, err := retainedSelectionSource(source)
	if err != nil || out == nil || !retainedSelectionViewID.MatchString(viewID) {
		return nil, errRetainedSelectionWire
	}
	bound, _, _, err := retainedSelectionBodyBound(source)
	if err != nil {
		return nil, err
	}
	header, err := retainedSelectionJSON(retainedSelectionHeader(source, viewID))
	if err != nil {
		return nil, err
	}
	w := &retainedSelectionWireWriter{out: out, source: source, index: retainedSelectionIndex(header, len(source.RecordIDs)), bound: bound}
	if err := w.write([]byte(retainedSelectionMagic)); err != nil {
		return nil, err
	}
	if err := w.frame(0, header); err != nil {
		return nil, err
	}
	return w, nil
}
func (w *retainedSelectionWireWriter) write(raw []byte) error {
	if w.failed || w.finished {
		return errRetainedSelectionWire
	}
	n, err := retainedSelectionAdd(w.bytes, int64(len(raw)))
	if err != nil || n > w.bound {
		w.failed = true
		return errRetainedSelectionWire
	}
	if err := retainedSelectionWrite(w.out, raw); err != nil {
		w.failed = true
		return err
	}
	w.bytes = n
	return nil
}
func (w *retainedSelectionWireWriter) frame(tag byte, metadata []byte) error {
	if uint64(len(metadata)) > math.MaxUint32 {
		w.failed = true
		return errRetainedSelectionWire
	}
	if tag != 0 {
		if err := w.write([]byte{tag}); err != nil {
			return err
		}
	}
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(metadata)))
	if err := w.write(length[:]); err != nil {
		return err
	}
	return w.write(metadata)
}
func (w *retainedSelectionWireWriter) Add(id string, input []byte) (retainedSelectionWireDescriptor, error) {
	var descriptor retainedSelectionWireDescriptor
	if w.failed || w.finished || w.next >= len(w.source.RecordIDs) || id != w.source.RecordIDs[w.next] || len(input) < 1 || int64(len(input)) > retainedSelectionMaxInputBytes || !utf8.Valid(input) {
		w.failed = true
		return descriptor, errRetainedSelectionWire
	}
	row := retainedSelectionWireRow{w.next, id, int64(len(input)), retainedSelectionHash(input)}
	metadata, err := retainedSelectionJSON(row)
	if err != nil {
		w.failed = true
		return descriptor, err
	}
	total, err := retainedSelectionAdd(w.inputs, row.InputBytes)
	if err != nil {
		w.failed = true
		return descriptor, err
	}
	if err := w.frame(1, metadata); err != nil {
		return descriptor, err
	}
	descriptor = retainedSelectionWireDescriptor{row, w.bytes}
	if err := w.write(input); err != nil {
		return retainedSelectionWireDescriptor{}, err
	}
	retainedSelectionIndexRow(w.index, row)
	w.next++
	w.inputs = total
	return descriptor, nil
}
func (w *retainedSelectionWireWriter) Finish() (int64, error) {
	if w.failed || w.finished || w.next != len(w.source.RecordIDs) {
		w.failed = true
		return 0, errRetainedSelectionWire
	}
	end, err := retainedSelectionJSON(retainedSelectionWireEnd{w.next, w.inputs, hex.EncodeToString(w.index.Sum(nil))})
	if err != nil {
		w.failed = true
		return 0, err
	}
	if err := w.frame(2, end); err != nil {
		return 0, err
	}
	w.finished = true
	return w.bytes, nil
}

type retainedSelectionWireReader struct {
	in            io.Reader
	bytes, length int64
}

func (r *retainedSelectionWireReader) read(n int64) ([]byte, error) {
	// Check representability and remaining actual body BEFORE allocation.
	if n < 0 || n > int64(int(^uint(0)>>1)) || n > r.length-r.bytes {
		return nil, errRetainedSelectionWire
	}
	raw := make([]byte, int(n))
	if _, err := io.ReadFull(r.in, raw); err != nil {
		return nil, errRetainedSelectionWire
	}
	r.bytes += n
	return raw, nil
}
func (r *retainedSelectionWireReader) metadata(maximum int, exact bool) ([]byte, error) {
	prefix, err := r.read(4)
	if err != nil {
		return nil, err
	}
	n := int64(binary.BigEndian.Uint32(prefix))
	if n < 1 || n > int64(maximum) || exact && n != int64(maximum) {
		return nil, errRetainedSelectionWire
	}
	return r.read(n)
}
func retainedSelectionCanonical(raw []byte, target any) bool {
	if !utf8.Valid(raw) || json.Unmarshal(raw, target) != nil {
		return false
	}
	canonical, err := retainedSelectionJSON(target)
	// This also rejects duplicate/unknown/case-folded keys, nulls, alternate
	// escapes/order/number spellings, BOM, and whitespace in closed metadata.
	return err == nil && bytes.Equal(raw, canonical)
}

// The callback sees one provisional verified row. It must not retain payloads
// across callbacks or publish readiness until this function succeeds at EOF.
// No JSON parser, numeric conversion or re-encoder touches Input bytes.
func readRetainedSelectionWire(in io.Reader, source retainedSelectionWireSource, contentLength int64, visit func(retainedSelectionWireDescriptor, []byte) error) (retainedSelectionWireHeader, []retainedSelectionWireDescriptor, error) {
	var header retainedSelectionWireHeader
	source, err := retainedSelectionSource(source)
	if err != nil || in == nil {
		return header, nil, errRetainedSelectionWire
	}
	bound, headerBytes, endBytes, err := retainedSelectionBodyBound(source)
	if err != nil || contentLength < 1 || contentLength > bound {
		return header, nil, errRetainedSelectionWire
	}
	r := retainedSelectionWireReader{in: in, length: contentLength}
	magic, err := r.read(int64(len(retainedSelectionMagic)))
	if err != nil || string(magic) != retainedSelectionMagic {
		return header, nil, errRetainedSelectionWire
	}
	raw, err := r.metadata(headerBytes, true)
	if err != nil || !retainedSelectionCanonical(raw, &header) || !retainedSelectionViewID.MatchString(header.ViewID) || header != retainedSelectionHeader(source, header.ViewID) {
		return retainedSelectionWireHeader{}, nil, errRetainedSelectionWire
	}
	index := retainedSelectionIndex(raw, len(source.RecordIDs))
	descriptors := make([]retainedSelectionWireDescriptor, 0, len(source.RecordIDs))
	var inputs int64
	for i, id := range source.RecordIDs {
		tag, err := r.read(1)
		if err != nil || tag[0] != 1 {
			return header, nil, errRetainedSelectionWire
		}
		maximum, err := retainedSelectionJSON(retainedSelectionWireRow{i, id, retainedSelectionMaxInputBytes, string(bytes.Repeat([]byte{'0'}, 64))})
		if err != nil {
			return header, nil, err
		}
		raw, err := r.metadata(len(maximum), false)
		var row retainedSelectionWireRow
		if err != nil || !retainedSelectionCanonical(raw, &row) || row.Index != i || row.ID != id || row.InputBytes < 1 || row.InputBytes > retainedSelectionMaxInputBytes || !retainedSelectionSHA.MatchString(row.InputSHA256) {
			return header, nil, errRetainedSelectionWire
		}
		descriptor := retainedSelectionWireDescriptor{row, r.bytes}
		input, err := r.read(row.InputBytes)
		if err != nil || !utf8.Valid(input) || retainedSelectionHash(input) != row.InputSHA256 {
			return header, nil, errRetainedSelectionWire
		}
		inputs, err = retainedSelectionAdd(inputs, row.InputBytes)
		if err != nil {
			return header, nil, err
		}
		retainedSelectionIndexRow(index, row)
		if visit != nil {
			if err := visit(descriptor, input); err != nil {
				return header, nil, err
			}
		}
		descriptors = append(descriptors, descriptor)
	}
	tag, err := r.read(1)
	if err != nil || tag[0] != 2 {
		return header, nil, errRetainedSelectionWire
	}
	raw, err = r.metadata(endBytes, false)
	var end retainedSelectionWireEnd
	if err != nil || !retainedSelectionCanonical(raw, &end) || end != (retainedSelectionWireEnd{len(source.RecordIDs), inputs, hex.EncodeToString(index.Sum(nil))}) || r.bytes != contentLength {
		return header, nil, errRetainedSelectionWire
	}
	var extra [1]byte
	// io.ReadFull handles permitted partial/empty reads; success requires actual
	// EOF, not merely having consumed a claimed Content-Length.
	if n, err := io.ReadFull(in, extra[:]); n != 0 || err != io.EOF {
		return header, nil, errRetainedSelectionWire
	}
	return header, descriptors, nil
}
