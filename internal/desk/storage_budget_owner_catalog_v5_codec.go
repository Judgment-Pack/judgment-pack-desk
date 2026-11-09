package desk

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"reflect"
	"unicode/utf8"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

var storageOwnerV5Magic = [8]byte{'D', 'S', 'K', 'O', 'W', 'N', '5', 0}

func storageOwnerV5Frame(payload []byte, sequence uint64) ([]byte, error) {
	if sequence == 0 || len(payload) > storageOwnerCatalogBytes-64 {
		return nil, storagebudget.ErrBounds
	}
	raw := make([]byte, storageOwnerCatalogBytes)
	copy(raw, storageOwnerV5Magic[:])
	binary.LittleEndian.PutUint32(raw[8:12], 5)
	binary.LittleEndian.PutUint32(raw[12:16], 64)
	binary.LittleEndian.PutUint64(raw[16:24], sequence)
	binary.LittleEndian.PutUint32(raw[24:28], uint32(len(payload)))
	copy(raw[64:], payload)
	h := sha256.New()
	h.Write(raw[:32])
	h.Write(payload)
	copy(raw[32:64], h.Sum(nil))
	return raw, nil
}
func storageOwnerV5Payload(raw []byte) ([]byte, uint64, error) {
	if len(raw) != storageOwnerCatalogBytes || !bytes.Equal(raw[:8], storageOwnerV5Magic[:]) || binary.LittleEndian.Uint32(raw[8:12]) != 5 || binary.LittleEndian.Uint32(raw[12:16]) != 64 || binary.LittleEndian.Uint32(raw[28:32]) != 0 {
		return nil, 0, storagebudget.ErrBounds
	}
	n := binary.LittleEndian.Uint32(raw[24:28])
	seq := binary.LittleEndian.Uint64(raw[16:24])
	if seq == 0 || n > storageOwnerCatalogBytes-64 {
		return nil, 0, storagebudget.ErrBounds
	}
	end := 64 + int(n)
	if !storageAllZero(raw[end:]) {
		return nil, 0, storagebudget.ErrBounds
	}
	h := sha256.New()
	h.Write(raw[:32])
	h.Write(raw[64:end])
	if !bytes.Equal(h.Sum(nil), raw[32:64]) {
		return nil, 0, storagebudget.ErrBounds
	}
	return raw[64:end], seq, nil
}
func storageOwnerV5Encode(c storageOwnerV5Catalog, r storageOwnerRecordV2, claim storageOwnerClaimV2) ([]byte, error) {
	if err := c.validate(r, claim); err != nil {
		return nil, err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	if !storageOwnerV5Preflight(b) {
		return nil, storagebudget.ErrBounds
	}
	return storageOwnerV5Frame(b, c.Sequence)
}
func storageOwnerV5Decode(raw []byte, r storageOwnerRecordV2, claim storageOwnerClaimV2) (storageOwnerV5Catalog, error) {
	var c storageOwnerV5Catalog
	b, seq, err := storageOwnerV5Payload(raw)
	if err != nil {
		return c, err
	}
	if !storageOwnerV5Preflight(b) || json.Unmarshal(b, &c) != nil {
		return storageOwnerV5Catalog{}, storagebudget.ErrBounds
	}
	canonical, err := json.Marshal(c)
	if err != nil || !bytes.Equal(b, canonical) || c.Sequence != seq {
		return storageOwnerV5Catalog{}, storagebudget.ErrBounds
	}
	if err = c.validate(r, claim); err != nil {
		return storageOwnerV5Catalog{}, err
	}
	return c, nil
}

// New rows are counted and bounded before Unmarshal allocates their slices.
// Existing subtrees use the unchanged v3 scanner and its shared aggregate counters.
type storageOwnerV5Scanner struct{ old storageOwnerCatalogScanner }

func storageOwnerV5Preflight(raw []byte) bool {
	if len(raw) > storageOwnerCatalogBytes-64 || !utf8.Valid(raw) {
		return false
	}
	p := storageOwnerV5Scanner{old: storageOwnerCatalogScanner{storageOwnerJSONPreflight: storageOwnerJSONPreflight{raw: raw}}}
	return p.value(reflect.TypeOf(storageOwnerV5Catalog{}), "") && p.old.pos == len(raw)
}
func (p *storageOwnerV5Scanner) value(t reflect.Type, field string) bool {
	switch t {
	case reflect.TypeOf(storageOwnerV5Catalog{}), reflect.TypeOf(storageOwnerV5UpgradeOrigin{}), reflect.TypeOf(storageOwnerV5Workspace{}), reflect.TypeOf(storageOwnerV5PrivateRoot{}), reflect.TypeOf(storageOwnerV5LineagePlan{}), reflect.TypeOf(storageOwnerV5Receipt{}), reflect.TypeOf(storageOwnerV5LineageCompletion{}), reflect.TypeOf(storageOwnerV5FromContinuation{}), reflect.TypeOf(storageOwnerV5StampCompletionRow{}):
		if !p.old.take('{') {
			return false
		}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if i > 0 && !p.old.take(',') {
				return false
			}
			if !p.old.literal(`"`+f.Name+`":`) || !p.value(f.Type, f.Name) {
				return false
			}
		}
		return p.old.take('}')
	}
	switch field {
	case "LineagePlans", "LineageCompletions", "LineageContinuations", "LineageStampCompletions":
		rowMax, totalMax := storageOwnerV5PlanBytes, 2<<20
		if field == "LineageCompletions" {
			rowMax, totalMax = storageOwnerV5CompletionBytes, 1<<20
		}
		if field == "LineageContinuations" || field == "LineageStampCompletions" {
			rowMax, totalMax = storageOwnerV5ContinuationBytes, 2<<20
		}
		if !p.old.take('[') {
			return false
		}
		start := p.old.pos - 1
		if p.old.take(']') {
			return true
		}
		for count := 0; ; count++ {
			if count >= storageOwnerV5Rows {
				return false
			}
			rowStart := p.old.pos
			if !p.value(t.Elem(), "") || p.old.pos-rowStart > rowMax {
				return false
			}
			if p.old.take(']') {
				return p.old.pos-start <= totalMax
			}
			if p.old.pos-start >= totalMax || !p.old.take(',') {
				return false
			}
		}
	case "AccountingOwnerKey", "IdentityInitial", "IdentityFromCleared", "HistoryIdentity":
		limit := 512
		if field == "AccountingOwnerKey" {
			limit = 256
		}
		if field == "HistoryIdentity" {
			limit = 4096
		}
		start := p.old.pos
		if !p.old.byteString("OwnerKey") {
			return false
		}
		n := p.old.pos - start - 2
		decoded := n / 4 * 3
		if n > 0 && p.old.raw[p.old.pos-2] == '=' {
			decoded--
			if p.old.raw[p.old.pos-3] == '=' {
				decoded--
			}
		}
		return decoded <= limit
	}
	return p.old.value(t, field)
}

var storageOwnerV6Magic = [8]byte{'D', 'S', 'K', 'O', 'W', 'N', '6', 0}

// Packed mode is a separate closed schema. Legacy V6 readers intentionally
// refuse its Encoding member and its binary Overrides value.
type storageOwnerV6PackedCatalog struct {
	Version               uint32
	Encoding              string
	Sequence              uint64
	TransitionKind        uint8
	Phase                 string
	Transition            storagebudget.ID
	PreviousCatalogDigest storagebudget.Digest
	BeforeEffectiveDigest storagebudget.Digest
	AfterEffectiveDigest  storagebudget.Digest
	Source                json.RawMessage
	EffectivePolicy       storageOwnerPolicy
	Overrides             []byte
	PolicyTransition      []byte
}

type storageOwnerV6PackedSource struct {
	Version       uint32
	Encoding      byte
	Core          []byte
	Physical      []byte
	LineageRows   json.RawMessage
	LineageSuffix []byte
	MoveOperands  []byte `json:",omitempty"`
}

// Scan only canonical base64 before allocation; the encoded width is bounded
// independently of aggregate legacy byte-string accounting.
func storageOwnerPackedByteString(p *storageOwnerCatalogScanner, max int) bool {
	if !p.take('"') {
		return false
	}
	start := p.pos
	for p.pos < len(p.raw) && p.raw[p.pos] != '"' {
		if p.pos-start >= 4*((max+2)/3) || storageOwnerBase64Value(p.raw[p.pos]) < 0 && p.raw[p.pos] != '=' {
			return false
		}
		p.pos++
	}
	if p.pos >= len(p.raw) {
		return false
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(string(p.raw[start:p.pos]))
	if err != nil || len(decoded) > max {
		return false
	}
	p.pos++
	return true
}

func storageOwnerPackedSourceScan(p *storageOwnerV5Scanner) bool {
	version := byte(1)
	if !p.old.literal(`{"Version":1,"Encoding":`) {
		if !p.old.literal(`{"Version":2,"Encoding":`) {
			return false
		}
		version = 2
	}
	physicalMax := 736
	if version == 2 {
		physicalMax = storageOwnerV6BeforeMax
	}
	if !p.old.value(reflect.TypeOf(byte(0)), "") || !p.old.literal(`,"Core":`) || !storageOwnerPackedByteString(&p.old, 8320) || !p.old.literal(`,"Physical":`) || !storageOwnerPackedByteString(&p.old, physicalMax) || !p.old.literal(`,"LineageRows":{`) {
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
	if !p.old.literal(`},"LineageSuffix":`) || !storageOwnerPackedByteString(&p.old, 102408) {
		return false
	}
	if version == 2 && (!p.old.literal(`,"MoveOperands":`) || !storageOwnerPackedByteString(&p.old, storageOwnerCatalogBytes-64)) {
		return false
	}
	return p.old.take('}')
}

func storageOwnerV6PackedPreflight(raw []byte) bool {
	if len(raw) > storageOwnerCatalogBytes-64 || !utf8.Valid(raw) {
		return false
	}
	p := storageOwnerV5Scanner{old: storageOwnerCatalogScanner{storageOwnerJSONPreflight: storageOwnerJSONPreflight{raw: raw}}}
	t := reflect.TypeOf(storageOwnerV6PackedCatalog{})
	if !p.old.take('{') {
		return false
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if i > 0 && !p.old.take(',') {
			return false
		}
		if !p.old.literal(`"` + f.Name + `":`) {
			return false
		}
		switch f.Name {
		case "Phase":
			// V6 has one additional closed outcome. The shared legacy Phase
			// scanner stays unchanged; a string-length increase is not an enum.
			if !p.old.literal(`"prepared"`) && !p.old.literal(`"active"`) && !p.old.literal(`"aborted"`) && !p.old.literal(`"aborted-unfenced"`) {
				return false
			}
		case "Encoding":
			if !p.old.literal(`"packed-binary-1"`) {
				return false
			}
		case "Source":
			if !storageOwnerPackedSourceScan(&p) {
				return false
			}
		case "Overrides":
			if !storageOwnerPackedByteString(&p.old, 270338) {
				return false
			}
		case "PolicyTransition":
			if !storageOwnerPackedByteString(&p.old, 2048) {
				return false
			}
		default:
			if !p.old.value(f.Type, f.Name) {
				return false
			}
		}
	}
	return p.old.take('}') && p.old.pos == len(raw)
}

func storageOwnerDecodePackedSource(raw []byte) (storageOwnerV6PackedSource, storageCompositionLineage, error) {
	var out storageOwnerV6PackedSource
	p := storageOwnerV5Scanner{old: storageOwnerCatalogScanner{storageOwnerJSONPreflight: storageOwnerJSONPreflight{raw: raw}}}
	if len(raw) > storageOwnerCatalogBytes-64 || !utf8.Valid(raw) || !storageOwnerPackedSourceScan(&p) || p.old.pos != len(raw) || json.Unmarshal(raw, &out) != nil {
		return out, storageCompositionLineage{}, storagebudget.ErrBounds
	}
	canonical, err := json.Marshal(out)
	if err != nil || !bytes.Equal(canonical, raw) || (out.Version != 1 || out.Encoding < 1 || out.Encoding > 3 || out.MoveOperands != nil) && (out.Version != 2 || out.Encoding != 4 || len(out.Physical) == 0 || len(out.Physical) > storageOwnerV6BeforeMax || out.MoveOperands == nil) || len(out.Core) < 64 || len(out.Core) > 8320 || out.Encoding == 1 && len(out.Physical) != 0 || out.Encoding == 2 && len(out.Physical) != 712 || out.Encoding == 3 && len(out.Physical) != 736 {
		return storageOwnerV6PackedSource{}, storageCompositionLineage{}, storagebudget.ErrBounds
	}
	w := storageEnrollmentPeakWriter{max: storageCompositionLineageMax}
	w.u32(uint32(len(out.LineageRows)))
	w.put(out.LineageRows)
	w.put(out.LineageSuffix)
	if w.bad {
		return storageOwnerV6PackedSource{}, storageCompositionLineage{}, storagebudget.ErrBounds
	}
	lineage, err := storageCompositionReadLineage(w.raw)
	if err != nil {
		return storageOwnerV6PackedSource{}, storageCompositionLineage{}, err
	}
	empty, emptyErr := storageCompositionEmptyLineage().encode()
	if emptyErr != nil || out.Encoding == 1 && !bytes.Equal(w.raw, empty) {
		return storageOwnerV6PackedSource{}, storageCompositionLineage{}, storagebudget.ErrBounds
	}
	if out.Encoding == 3 {
		recipe, e := storageOwnerV6ReadBefore(out.Physical)
		if e != nil || recipe.Kind != 2 {
			return storageOwnerV6PackedSource{}, storageCompositionLineage{}, storagebudget.ErrBounds
		}
	}
	if out.Encoding == 4 {
		recipe, e := storageOwnerV6ReadBefore(out.Physical)
		table, te := storageMoveDecodeOperandTable(out.MoveOperands)
		if e != nil || te != nil || recipe.Kind != 3 || storageMoveBeforeReferences(recipe, table) != nil {
			return storageOwnerV6PackedSource{}, storageCompositionLineage{}, storagebudget.ErrBounds
		}
	}
	return out, lineage, nil
}

func (p storageOwnerV6PackedCatalog) common(source storageOwnerDefinitions) (storageOwnerV6Catalog, error) {
	rows, err := storageOwnerV6DecodeSparse(p.Overrides, source)
	if err != nil {
		return storageOwnerV6Catalog{}, err
	}
	return storageOwnerV6Catalog{Version: p.Version, Sequence: p.Sequence, TransitionKind: p.TransitionKind, Phase: p.Phase, Transition: p.Transition, PreviousCatalogDigest: p.PreviousCatalogDigest, BeforeEffectiveDigest: p.BeforeEffectiveDigest, AfterEffectiveDigest: p.AfterEffectiveDigest, Source: append([]byte{}, p.Source...), EffectivePolicy: p.EffectivePolicy, Overrides: rows, PolicyTransition: append([]byte{}, p.PolicyTransition...)}, nil
}

func (p storageOwnerV6PackedCatalog) validate(evidenceRaw []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageOwnerV6Catalog, storageOwnerCatalogV2, storageCompositionLineage, error) {
	if p.Version != 6 || p.Encoding != "packed-binary-1" {
		return storageOwnerV6Catalog{}, storageOwnerCatalogV2{}, storageCompositionLineage{}, storagebudget.ErrBounds
	}
	source, pair, lineage, _, err := storageOwnerV6PackedSourceForPolicy(p, evidenceRaw, record, claim)
	if err != nil {
		return storageOwnerV6Catalog{}, storageOwnerCatalogV2{}, storageCompositionLineage{}, err
	}
	before, after, err := source.definitions()
	if err != nil {
		return storageOwnerV6Catalog{}, storageOwnerCatalogV2{}, storageCompositionLineage{}, err
	}
	c, err := p.common(after)
	if err != nil {
		return c, source, lineage, err
	}
	switch c.TransitionKind {
	case storageOwnerV6PolicyTransition:
		err = c.validatePolicyDefinitions(after, record)
	case storageOwnerV6SourceTransition:
		if sourceDTO, _, e := storageOwnerDecodePackedSource(p.Source); e != nil || sourceDTO.Encoding == 3 || sourceDTO.Encoding == 4 {
			return c, source, lineage, storagebudget.ErrBounds
		}
		// E1 inner remains Version1/prepared. Its completed physical envelope
		// supplies publication; it is never relabeled into a legacy source.
		transition := source.Transition
		if string(pair[0][:8]) == "DSKONL1\x00" {
			physical, e := storageCompositionCaptureProjection(pair[0], pair[1], record, claim)
			if e != nil {
				return c, source, lineage, e
			}
			var lineageOnly bool
			transition, lineageOnly = storagePolicySourceTransition(source, physical)
			if lineageOnly {
				before = after
			}
		}
		if c.Sequence == 0 || c.Phase == "prepared" && c.Sequence == ^uint64(0) || c.Transition == (storagebudget.ID{}) || c.PreviousCatalogDigest == (storagebudget.Digest{}) || c.Phase != "active" && c.Phase != "prepared" || c.PolicyTransition == nil || len(c.PolicyTransition) != 0 || c.Transition != transition {
			err = storagebudget.ErrBounds
		} else {
			err = c.validateSourceDefinitions(before, after)
		}
	default:
		err = storagebudget.ErrBounds
	}
	return c, source, lineage, err
}

func storageOwnerV6PackedEncode(p storageOwnerV6PackedCatalog, evidenceRaw []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) ([]byte, error) {
	_, source, lineage, err := p.validate(evidenceRaw, record, claim)
	if err != nil {
		return nil, err
	}
	if _, err = storageOwnerV6PackedClosureSize(p, source, lineage); err != nil {
		return nil, err
	}
	b, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	if !storageOwnerV6PackedPreflight(b) {
		return nil, storagebudget.ErrBounds
	}
	return storageOwnerV6Frame(b, p.Sequence)
}

func storageOwnerV6PackedDecode(raw, evidenceRaw []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageOwnerV6PackedCatalog, error) {
	var p storageOwnerV6PackedCatalog
	b, sequence, err := storageOwnerV6Payload(raw)
	if err != nil {
		return p, err
	}
	if !storageOwnerV6PackedPreflight(b) || json.Unmarshal(b, &p) != nil {
		return storageOwnerV6PackedCatalog{}, storagebudget.ErrBounds
	}
	canonical, err := json.Marshal(p)
	if err != nil || !bytes.Equal(canonical, b) || p.Sequence != sequence {
		return storageOwnerV6PackedCatalog{}, storagebudget.ErrBounds
	}
	_, source, lineage, err := p.validate(evidenceRaw, record, claim)
	if err != nil {
		return storageOwnerV6PackedCatalog{}, err
	}
	if _, err = storageOwnerV6PackedClosureSize(p, source, lineage); err != nil {
		return storageOwnerV6PackedCatalog{}, err
	}
	return p, nil
}

// The packed branch uses an explicitly different outer schema; these bytes
// never reinterpret the legacy V6 Overrides JSON array. Zero rows encode as
// exactly two zero bytes, not nil/null, and absent rows retain admission limits.
func storageOwnerV6EncodeSparse(rows []storageOwnerV6Override, source storageOwnerDefinitions) ([]byte, error) {
	if _, err := storageOwnerV6Expand(source, source.Policy, rows); err != nil {
		return nil, err
	}
	w := storageEnrollmentPeakWriter{max: 2 + 1024*264}
	w.u16(uint16(len(rows)))
	for _, row := range rows {
		w.put(row.ID[:])
		w.u64(row.Generation)
		storageEnrollmentPeakWriteFixed(&w, reflect.ValueOf(row.Limits))
	}
	if w.bad {
		return nil, storagebudget.ErrBounds
	}
	return w.raw, nil
}

func storageOwnerV6DecodeSparse(raw []byte, source storageOwnerDefinitions) ([]storageOwnerV6Override, error) {
	if len(raw) < 2 || len(raw) > 2+1024*264 {
		return nil, storagebudget.ErrBounds
	}
	r := storageEnrollmentPeakReader{raw: raw}
	n := int(r.u16())
	if n > 1024 || len(raw) != 2+264*n {
		return nil, storagebudget.ErrBounds
	}
	rows := make([]storageOwnerV6Override, n)
	for i := range rows {
		rows[i].ID = r.id()
		rows[i].Generation = r.u64()
		storageEnrollmentPeakReadFixed(&r, reflect.ValueOf(&rows[i].Limits).Elem())
	}
	if !r.done() {
		return nil, storagebudget.ErrBounds
	}
	if _, err := storageOwnerV6Expand(source, source.Policy, rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func storageOwnerV6Frame(payload []byte, sequence uint64) ([]byte, error) {
	if sequence == 0 || len(payload) > storageOwnerCatalogBytes-64 {
		return nil, storagebudget.ErrBounds
	}
	raw := make([]byte, storageOwnerCatalogBytes)
	copy(raw, storageOwnerV6Magic[:])
	binary.LittleEndian.PutUint32(raw[8:12], 6)
	binary.LittleEndian.PutUint32(raw[12:16], 64)
	binary.LittleEndian.PutUint64(raw[16:24], sequence)
	binary.LittleEndian.PutUint32(raw[24:28], uint32(len(payload)))
	copy(raw[64:], payload)
	h := sha256.New()
	h.Write(raw[:32])
	h.Write(payload)
	copy(raw[32:64], h.Sum(nil))
	return raw, nil
}
func storageOwnerV6Payload(raw []byte) ([]byte, uint64, error) {
	if len(raw) != storageOwnerCatalogBytes || !bytes.Equal(raw[:8], storageOwnerV6Magic[:]) || binary.LittleEndian.Uint32(raw[8:12]) != 6 || binary.LittleEndian.Uint32(raw[12:16]) != 64 || binary.LittleEndian.Uint32(raw[28:32]) != 0 {
		return nil, 0, storagebudget.ErrBounds
	}
	n := binary.LittleEndian.Uint32(raw[24:28])
	seq := binary.LittleEndian.Uint64(raw[16:24])
	if seq == 0 || n > storageOwnerCatalogBytes-64 {
		return nil, 0, storagebudget.ErrBounds
	}
	end := 64 + int(n)
	if !storageAllZero(raw[end:]) {
		return nil, 0, storagebudget.ErrBounds
	}
	h := sha256.New()
	h.Write(raw[:32])
	h.Write(raw[64:end])
	if !bytes.Equal(h.Sum(nil), raw[32:64]) {
		return nil, 0, storagebudget.ErrBounds
	}
	return raw[64:end], seq, nil
}

// Source payloads retain their own exact closed schema and validation. V6 does
// not reinterpret V4 bridge or historical provider receipts as V5 material.
func storageOwnerV6Source(raw []byte, r storageOwnerRecordV2, cl storageOwnerClaimV2) (storageOwnerCatalogV2, error) {
	var canonical []byte
	var out storageOwnerCatalogV2
	var err error
	switch {
	case bytes.HasPrefix(raw, []byte(`{"Version":3,`)):
		if !storageOwnerCatalogPreflight(raw) || json.Unmarshal(raw, &out) != nil {
			return out, storagebudget.ErrBounds
		}
		canonical, err = json.Marshal(out)
		if err == nil {
			err = out.validateVersion(r, cl, 3)
		}
	case bytes.HasPrefix(raw, []byte(`{"Version":4,`)):
		var c storageOwnerV4Catalog
		if !storageOwnerV4Preflight(raw) || json.Unmarshal(raw, &c) != nil {
			return out, storagebudget.ErrBounds
		}
		canonical, err = json.Marshal(c)
		if err == nil {
			err = c.validate(r, cl)
		}
		out = c.old()
	case bytes.HasPrefix(raw, []byte(`{"Version":5,`)):
		var c storageOwnerV5Catalog
		if !storageOwnerV5Preflight(raw) || json.Unmarshal(raw, &c) != nil {
			return out, storagebudget.ErrBounds
		}
		canonical, err = json.Marshal(c)
		if err == nil {
			err = c.validate(r, cl)
		}
		out = c.old()
	default:
		return out, storagebudget.ErrBounds
	}
	if err != nil {
		return storageOwnerCatalogV2{}, err
	}
	if !bytes.Equal(raw, canonical) {
		return storageOwnerCatalogV2{}, storagebudget.ErrBounds
	}
	return out, nil
}

type storageOwnerV6Scanner struct{ old storageOwnerCatalogScanner }

func storageOwnerV6Preflight(raw []byte) bool {
	if len(raw) > storageOwnerCatalogBytes-64 || !utf8.Valid(raw) {
		return false
	}
	p := storageOwnerV6Scanner{old: storageOwnerCatalogScanner{storageOwnerJSONPreflight: storageOwnerJSONPreflight{raw: raw}}}
	return p.value(reflect.TypeOf(storageOwnerV6Catalog{}), "") && p.old.pos == len(raw)
}
func (p *storageOwnerV6Scanner) value(t reflect.Type, field string) bool {
	if field == "Phase" {
		// Source delegates to its original legacy scanner below. This closed
		// list applies only to the V6 envelope, including its closure shadow.
		return p.old.literal(`"prepared"`) || p.old.literal(`"active"`) || p.old.literal(`"aborted"`) || p.old.literal(`"aborted-unfenced"`)
	}
	if field == "Source" {
		switch {
		case bytes.HasPrefix(p.old.raw[p.old.pos:], []byte(`{"Version":3,`)):
			return p.old.value(reflect.TypeOf(storageOwnerCatalogV2{}), "")
		case bytes.HasPrefix(p.old.raw[p.old.pos:], []byte(`{"Version":4,`)):
			v := storageOwnerV4Scanner{old: p.old}
			ok := v.value(reflect.TypeOf(storageOwnerV4Catalog{}), "")
			p.old = v.old
			return ok
		case bytes.HasPrefix(p.old.raw[p.old.pos:], []byte(`{"Version":5,`)):
			v := storageOwnerV5Scanner{old: p.old}
			ok := v.value(reflect.TypeOf(storageOwnerV5Catalog{}), "")
			p.old = v.old
			return ok
		default:
			return false
		}
	}
	if field == "PolicyTransition" {
		if p.old.literal(`""`) {
			return true
		}
		start, sourceBytes := p.old.pos, p.old.sourceBytes
		ok := p.old.byteString("OwnerKey")
		p.old.sourceBytes = sourceBytes
		return ok && p.old.pos-start == 2734 && p.old.raw[p.old.pos-2] == '=' && p.old.raw[p.old.pos-3] != '='
	}
	if field == "Overrides" {
		if !p.old.take('[') {
			return false
		}
		if p.old.take(']') {
			return true
		}
		for n := 0; ; n++ {
			if n >= storagebudget.MaxWorkspaces || !p.value(reflect.TypeOf(storageOwnerV6Override{}), "") {
				return false
			}
			if p.old.take(']') {
				return true
			}
			if !p.old.take(',') {
				return false
			}
		}
	}
	if t == reflect.TypeOf(storageOwnerV6Catalog{}) || t == reflect.TypeOf(storageOwnerV6Override{}) {
		if !p.old.take('{') {
			return false
		}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if i > 0 && !p.old.take(',') {
				return false
			}
			if !p.old.literal(`"`+f.Name+`":`) || !p.value(f.Type, f.Name) {
				return false
			}
		}
		return p.old.take('}')
	}
	return p.old.value(t, field)
}
func storageOwnerV6Encode(c storageOwnerV6Catalog, r storageOwnerRecordV2, cl storageOwnerClaimV2) ([]byte, error) {
	if err := c.validateV6(r, cl); err != nil {
		return nil, err
	}
	if _, err := storageOwnerV6ClosureSize(c); err != nil {
		return nil, err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	if !storageOwnerV6Preflight(b) {
		return nil, storagebudget.ErrBounds
	}
	return storageOwnerV6Frame(b, c.Sequence)
}
func storageOwnerV6Decode(raw []byte, r storageOwnerRecordV2, cl storageOwnerClaimV2) (storageOwnerV6Catalog, error) {
	var c storageOwnerV6Catalog
	b, seq, err := storageOwnerV6Payload(raw)
	if err != nil {
		return c, err
	}
	if !storageOwnerV6Preflight(b) || json.Unmarshal(b, &c) != nil {
		return c, storagebudget.ErrBounds
	}
	canonical, err := json.Marshal(c)
	if err != nil || !bytes.Equal(b, canonical) || c.Sequence != seq {
		return storageOwnerV6Catalog{}, storagebudget.ErrBounds
	}
	if err = c.validateV6(r, cl); err != nil {
		return storageOwnerV6Catalog{}, err
	}
	if _, err = storageOwnerV6ClosureSize(c); err != nil {
		return storageOwnerV6Catalog{}, err
	}
	return c, nil
}

// V6Digest is the exact V6 frame commitment, matching all existing catalog
// chains. It hashes the canonical header/payload without allocating zero padding.
func storageOwnerV6Digest(c storageOwnerV6Catalog) (storagebudget.Digest, error) {
	var d storagebudget.Digest
	b, err := json.Marshal(c)
	if err != nil {
		return d, err
	}
	if c.Sequence == 0 || !storageOwnerV6Preflight(b) {
		return d, storagebudget.ErrBounds
	}
	var head [32]byte
	copy(head[:], storageOwnerV6Magic[:])
	binary.LittleEndian.PutUint32(head[8:12], 6)
	binary.LittleEndian.PutUint32(head[12:16], 64)
	binary.LittleEndian.PutUint64(head[16:24], c.Sequence)
	binary.LittleEndian.PutUint32(head[24:28], uint32(len(b)))
	h := sha256.New()
	h.Write(head[:])
	h.Write(b)
	copy(d[:], h.Sum(nil))
	return d, nil
}

// Classification is comparison data only. Original startup must separately join
// this exact pair to original Q/custody and finite phase permissions before any
// effect. No unrecognized prefix falls back to ordinary serving.
type storageOwnerV6PairKind uint8

const (
	storageOwnerV6PreparedFromLegacy storageOwnerV6PairKind = iota + 1
	storageOwnerV6PreparedFromV6
	storageOwnerV6Committed
	storageOwnerV6Aborted
	storageOwnerV6AbortedUnfenced
)

type storageOwnerV6Pair struct {
	Kind     storageOwnerV6PairKind
	Selected storageOwnerV6Catalog
	Prepared storageOwnerV6Catalog
}

func storageOwnerV6Classify(a, b []byte, r storageOwnerRecordV2, cl storageOwnerClaimV2) (storageOwnerV6Pair, error) {
	var out storageOwnerV6Pair
	av6 := len(a) >= 8 && bytes.Equal(a[:8], storageOwnerV6Magic[:])
	bv6 := len(b) >= 8 && bytes.Equal(b[:8], storageOwnerV6Magic[:])
	if !av6 && !bv6 {
		return out, storagebudget.ErrBounds
	}
	if !av6 || !bv6 {
		current, legacy := a, b
		if !av6 {
			current, legacy = b, a
		}
		c, err := storageOwnerV6Decode(current, r, cl)
		if err != nil {
			return out, err
		}
		if err = storageOwnerV6Starts(legacy, c, r, cl); err != nil {
			return out, err
		}
		return storageOwnerV6Pair{storageOwnerV6PreparedFromLegacy, c, c}, nil
	}
	x, err := storageOwnerV6Decode(a, r, cl)
	if err != nil {
		return out, err
	}
	y, err := storageOwnerV6Decode(b, r, cl)
	if err != nil {
		return out, err
	}
	if x.Sequence > y.Sequence {
		x, y = y, x
	}
	if x.Sequence == ^uint64(0) || y.Sequence != x.Sequence+1 {
		return out, storagebudget.ErrBounds
	}
	if y.Phase == "prepared" {
		if err = storageOwnerV6Follows(x, y, r, cl); err != nil {
			return out, err
		}
		return storageOwnerV6Pair{storageOwnerV6PreparedFromV6, y, y}, nil
	}
	if err = storageOwnerV6Publication(x, y, r, cl); err != nil {
		return out, err
	}
	kind := storageOwnerV6Committed
	if y.Phase == "aborted" {
		kind = storageOwnerV6Aborted
	} else if y.Phase == "aborted-unfenced" {
		kind = storageOwnerV6AbortedUnfenced
	}
	return storageOwnerV6Pair{kind, y, x}, nil
}

func storageOwnerV6SourceFrame(raw []byte, r storageOwnerRecordV2, cl storageOwnerClaimV2) ([]byte, error) {
	source, err := storageOwnerV6Source(raw, r, cl)
	if err != nil {
		return nil, err
	}
	switch {
	case bytes.HasPrefix(raw, []byte(`{"Version":3,`)):
		return storageOwnerCatalogFrameVersion(raw, source.Sequence, 3)
	case bytes.HasPrefix(raw, []byte(`{"Version":4,`)):
		return storageOwnerV4Frame(raw, source.Sequence)
	case bytes.HasPrefix(raw, []byte(`{"Version":5,`)):
		return storageOwnerV5Frame(raw, source.Sequence)
	default:
		return nil, storagebudget.ErrBounds
	}
}
func storageOwnerV6SourceAdjacent(a, b []byte, r storageOwnerRecordV2, cl storageOwnerClaimV2) error {
	low, err := storageOwnerV6SourceFrame(a, r, cl)
	if err != nil {
		return err
	}
	high, err := storageOwnerV6SourceFrame(b, r, cl)
	if err != nil {
		return err
	}
	lo, hi := binary.LittleEndian.Uint64(low[16:24]), binary.LittleEndian.Uint64(high[16:24])
	if lo == ^uint64(0) || hi != lo+1 {
		return storagebudget.ErrBounds
	}
	switch {
	case bytes.HasPrefix(a, []byte(`{"Version":3,`)) && bytes.HasPrefix(b, []byte(`{"Version":3,`)):
		pair, err := storageOwnerDecodeCatalogPairV3(low, high, r, cl)
		if err != nil {
			return err
		}
		if !pair.PeerLinked {
			return storagebudget.ErrBounds
		}
	case bytes.HasPrefix(a, []byte(`{"Version":3,`)) && bytes.HasPrefix(b, []byte(`{"Version":4,`)):
		// Reuse only the existing exact bridge comparison. Its native original
		// barrier/duplicate predicates remain mandatory in later host wiring.
		pair, err := storageOwnerV4Classify(low, high, r, cl)
		if err != nil {
			return err
		}
		if pair.Class != storageOwnerV4BridgeNeedsDuplicate || pair.Selected != 1 {
			return storagebudget.ErrBounds
		}
	case bytes.HasPrefix(a, []byte(`{"Version":4,`)) && bytes.HasPrefix(b, []byte(`{"Version":4,`)):
		var x, y storageOwnerV4Catalog
		if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil || !storageOwnerV4Adjacent(x, y, low) {
			return storagebudget.ErrBounds
		}
	case bytes.HasPrefix(a, []byte(`{"Version":5,`)) && bytes.HasPrefix(b, []byte(`{"Version":5,`)):
		var x, y storageOwnerV5Catalog
		if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil || !storageOwnerV5Adjacent(x, y, low) {
			return storagebudget.ErrBounds
		}
	default:
		return storagebudget.ErrBounds
	}
	return nil
}

// V6's native-source payload is binary and closed. The old JSON V6 readers
// refuse this tag. Its actual outer sequence/checksum is never a Source header.
var storageOwnerV6NativeMagic = [8]byte{'D', 'S', 'K', 'V', '6', 'N', '1', 0}

const storageOwnerV6NativePrefix = 800
const storageOwnerV6BeforeMax = 4096

func storageOwnerV6NativeStagePrefix(v storageCompositionBank, base uint64, boundary storagePolicyQBoundary) ([]byte, error) {
	core, err := storageCompositionCompactCore(v.Core)
	if err != nil || !boundary.valid() {
		return nil, storagebudget.ErrBounds
	}
	lineage, err := v.Lineage.encode()
	if err != nil || sha256.Sum256(lineage) != v.LineageDigest || storageCompositionCoreDigest(sha256.Sum256(v.Core)) != v.CoreFull {
		return nil, storagebudget.ErrBounds
	}
	b := make([]byte, 256)
	storageEnrollmentPeakPut16(b[:2], 3)
	if v.Moves != nil {
		_, moveIndex, moves, e := storageMoveBankOperands(v)
		if e != nil {
			return nil, e
		}
		storageEnrollmentPeakPut16(b[:2], 4)
		storageEnrollmentPeakPut16(b[249:251], moveIndex)
		storageEnrollmentPeakPut32(b[251:255], uint32(len(moves)))
	}
	storageEnrollmentPeakPut64(b[225:233], base)
	storageEnrollmentPeakPut64(b[233:241], boundary.Epoch)
	storageEnrollmentPeakPut64(b[241:249], boundary.MinimumRevision)
	b[2], b[3], b[4], b[5] = v.CoreCodec, v.Publication, v.Transition, v.BeforeBank
	storageEnrollmentPeakPut32(b[8:12], uint32(len(core)))
	storageEnrollmentPeakPut32(b[12:16], uint32(len(lineage)))
	copy(b[16:32], v.TransitionID[:])
	copy(b[32:64], v.Previous[:])
	copy(b[64:96], v.CoreFull[:])
	copy(b[96:128], v.LineageDigest[:])
	copy(b[128:160], v.FirstBase[:])
	copy(b[160:225], v.BeforePeer[:])
	return b, nil
}
func storageOwnerV6NativeReadStagePrefix(b []byte, sequence uint64, core []byte, lineage storageCompositionLineage) (storageCompositionBank, uint64, storagePolicyQBoundary, error) {
	return storageOwnerV6NativeReadStagePrefixWithOperands(b, sequence, core, lineage, storageMoveOperandTable{})
}
func storageOwnerV6NativeReadStagePrefixWithOperands(b []byte, sequence uint64, core []byte, lineage storageCompositionLineage, operands storageMoveOperandTable) (storageCompositionBank, uint64, storagePolicyQBoundary, error) {
	var v storageCompositionBank
	if len(b) != 256 || !storageEnrollmentPeakZero(b[6:8]) {
		return v, 0, storagePolicyQBoundary{}, storagebudget.ErrBounds
	}
	version := storageEnrollmentPeakU16(b[:2])
	var moves []storageMoveCurrent
	var wire []byte
	var e error
	if operands.Sets != nil {
		wire, e = storageMoveEncodeOperandTable(operands)
		if e == nil {
			moves, e = operands.selectSet(storageEnrollmentPeakU16(b[249:251]))
		}
	}
	if e != nil || version == 3 && (moves != nil || !storageEnrollmentPeakZero(b[249:])) || version == 4 && (moves == nil || uint64(storageEnrollmentPeakU32(b[251:255])) != uint64(len(wire)) || !storageEnrollmentPeakZero(b[255:])) || version != 3 && version != 4 {
		return v, 0, storagePolicyQBoundary{}, storagebudget.ErrBounds
	}
	v.Sequence, v.CoreCodec, v.Publication, v.Transition, v.BeforeBank = sequence, b[2], b[3], b[4], b[5]
	copy(v.TransitionID[:], b[16:32])
	copy(v.Previous[:], b[32:64])
	copy(v.CoreFull[:], b[64:96])
	copy(v.LineageDigest[:], b[96:128])
	copy(v.FirstBase[:], b[128:160])
	copy(v.BeforePeer[:], b[160:225])
	v.Core = append([]byte{}, core...)
	v.Lineage = lineage
	v.Moves = moves
	v.MoveOperands = operands
	boundary := storagePolicyQBoundary{storageEnrollmentPeakU64(b[233:241]), storageEnrollmentPeakU64(b[241:249])}
	exact, err := storageOwnerV6NativeStagePrefix(v, storageEnrollmentPeakU64(b[225:233]), boundary)
	if err != nil || !bytes.Equal(b, exact) {
		return storageCompositionBank{}, 0, storagePolicyQBoundary{}, storagebudget.ErrBounds
	}
	return v, storageEnrollmentPeakU64(b[225:233]), boundary, nil
}

func (p storageOwnerV6BeforePair) encode() ([]byte, error) {
	if p.Selected > 1 || p.Kind < 1 || p.Kind > 3 {
		return nil, storagebudget.ErrBounds
	}
	w := storageEnrollmentPeakWriter{max: storageOwnerV6BeforeMax}
	magic := "DSKV6B1\x00"
	if p.Kind == 3 || p.Kind == 1 && p.Packed.SourceEncoding == 4 {
		magic = "DSKV6B2\x00"
	}
	w.put([]byte(magic))
	w.u8(p.Kind)
	w.u8(p.Selected)
	w.u16(0)
	w.u32(0)
	for i := range p.Headers {
		h := p.Headers[i]
		if !bytes.Equal(h[:8], storageOwnerV6Magic[:]) || storageEnrollmentPeakU32(h[8:12]) != 6 || storageEnrollmentPeakU32(h[12:16]) != 64 || storageEnrollmentPeakU64(h[16:24]) == 0 || storageEnrollmentPeakU32(h[24:28]) > storageOwnerCatalogBytes-64 || storageEnrollmentPeakU32(h[28:32]) != 0 || p.Full[i] == (storagebudget.Digest{}) {
			return nil, storagebudget.ErrBounds
		}
		w.put(p.Headers[i][:])
		w.put(p.Full[i][:])
		for _, n := range p.Cut[i] {
			if n > 1024 {
				return nil, storagebudget.ErrBounds
			}
			w.u16(n)
		}
	}
	if p.Kind == 2 || p.Kind == 3 {
		if !reflect.DeepEqual(p.Packed, storagePolicyProjection{}) || p.Physical != (storageCompositionProjection{}) {
			return nil, storagebudget.ErrBounds
		}
		for i := range p.Native {
			w.put(p.Native[i][:])
		}
		if p.Kind == 2 {
			if p.NativeMoveMap[0] != nil || p.NativeMoveMap[1] != nil {
				return nil, storagebudget.ErrBounds
			}
		} else {
			for i, mapping := range p.NativeMoveMap {
				version := storageEnrollmentPeakU16(p.Native[i][:2])
				if version == 3 && mapping != nil || version == 4 && len(mapping) == 0 || version != 3 && version != 4 {
					return nil, storagebudget.ErrBounds
				}
				if err := storageMoveWriteMapping(&w, mapping); err != nil {
					return nil, err
				}
			}
		}
	} else {
		m := p.Packed
		if p.Native != ([2][256]byte{}) || p.NativeMoveMap[0] != nil || p.NativeMoveMap[1] != nil || m.SourceEncoding < 1 || m.SourceEncoding > 4 || m.Phase != 2 && m.Phase != 3 && m.Phase != 4 || m.Kind != storageOwnerV6PolicyTransition && m.Kind != storageOwnerV6SourceTransition || m.SelectedBank != p.Selected || !m.PeerRetained || m.PeerPrevious == (storagebudget.Digest{}) || m.InverseLength != 0 && m.InverseLength != 2048 || m.InverseLength == 0 && !storageEnrollmentPeakZero(m.Inverse[:]) || m.SourceEncoding == 1 && p.Physical != (storageCompositionProjection{}) || m.SourceEncoding == 2 && !p.Physical.shape() {
			return nil, storagebudget.ErrBounds
		}
		w.u8(m.SourceEncoding)
		w.u8(m.Kind)
		w.u8(m.Phase)
		w.u8(0)
		w.put(m.Transition[:])
		w.put(m.Previous[:])
		w.put(m.Before[:])
		w.put(m.After[:])
		w.put(m.PeerPrevious[:])
		w.u16(m.InverseLength)
		w.u16(0)
		w.put(m.Inverse[:])
		if m.SourceEncoding == 4 {
			if p.Physical != (storageCompositionProjection{}) || m.NativeSource != ([736]byte{}) || len(m.MoveNativeSource) == 0 || len(m.SourceMoveMap) == 0 {
				return nil, storagebudget.ErrBounds
			}
			recipe, e := storageOwnerV6ReadBefore(m.MoveNativeSource)
			if e != nil || recipe.Kind != 3 {
				return nil, storagebudget.ErrBounds
			}
			w.data(m.MoveNativeSource, storageOwnerV6BeforeMax)
			if e = storageMoveWriteMapping(&w, m.SourceMoveMap); e != nil {
				return nil, e
			}
		} else if m.SourceEncoding == 3 {
			if m.MoveNativeSource != nil || m.SourceMoveMap != nil {
				return nil, storagebudget.ErrBounds
			}
			if p.Physical != (storageCompositionProjection{}) {
				return nil, storagebudget.ErrBounds
			}
			w.put(m.NativeSource[:])
		} else {
			if m.NativeSource != ([736]byte{}) || m.MoveNativeSource != nil || m.SourceMoveMap != nil {
				return nil, storagebudget.ErrBounds
			}
			w.put(p.Physical[:])
		}
	}
	if w.bad {
		return nil, storagebudget.ErrBounds
	}
	storageEnrollmentPeakPut32(w.raw[12:16], uint32(len(w.raw)))
	return w.raw, nil
}
func storageOwnerV6ReadBefore(raw []byte) (storageOwnerV6BeforePair, error) {
	var p storageOwnerV6BeforePair
	if len(raw) < 16 || len(raw) > storageOwnerV6BeforeMax || (string(raw[:8]) != "DSKV6B1\x00" && string(raw[:8]) != "DSKV6B2\x00") || int(storageEnrollmentPeakU32(raw[12:16])) != len(raw) {
		return p, storagebudget.ErrBounds
	}
	r := storageEnrollmentPeakReader{raw: raw[8:]}
	p.Kind, p.Selected = r.u8(), r.u8()
	if r.u16() != 0 {
		return p, storagebudget.ErrBounds
	}
	r.u32()
	for i := range p.Headers {
		copy(p.Headers[i][:], r.take(64))
		p.Full[i] = r.digest()
		for j := range p.Cut[i] {
			p.Cut[i][j] = r.u16()
		}
	}
	if p.Kind == 2 || p.Kind == 3 {
		for i := range p.Native {
			copy(p.Native[i][:], r.take(256))
		}
		if p.Kind == 3 {
			for i := range p.NativeMoveMap {
				p.NativeMoveMap[i] = storageMoveReadMapping(&r)
			}
		}
	} else if p.Kind == 1 {
		m := &p.Packed
		m.SourceEncoding, m.Kind, m.Phase = r.u8(), r.u8(), r.u8()
		if r.u8() != 0 {
			return p, storagebudget.ErrBounds
		}
		m.Transition = r.id()
		m.Previous = r.digest()
		m.Before = r.digest()
		m.After = r.digest()
		m.PeerPrevious = r.digest()
		m.InverseLength = r.u16()
		if r.u16() != 0 {
			return p, storagebudget.ErrBounds
		}
		copy(m.Inverse[:], r.take(2048))
		if m.SourceEncoding == 4 {
			m.MoveNativeSource = bytes.Clone(r.data(storageOwnerV6BeforeMax))
			m.SourceMoveMap = storageMoveReadMapping(&r)
		} else if m.SourceEncoding == 3 {
			copy(m.NativeSource[:], r.take(736))
		} else {
			copy(p.Physical[:], r.take(712))
		}
		m.SelectedBank, m.PeerRetained = p.Selected, true
		m.Header = p.Headers[p.Selected&1]
	} else {
		return p, storagebudget.ErrBounds
	}
	if !r.done() {
		return storageOwnerV6BeforePair{}, storagebudget.ErrBounds
	}
	canonical, err := p.encode()
	if err != nil || !bytes.Equal(canonical, raw) {
		return storageOwnerV6BeforePair{}, storagebudget.ErrBounds
	}
	return p, nil
}

// Decode enforces framing, exact stage shape and all locally available
// operands. The original evidence/Q/native joins remain explicit separate
// requirements; merely obtaining this DTO cannot register or recover a source.
func (v storageOwnerV6NativeBank) validate(record storageOwnerRecordV2, claim storageOwnerClaimV2) error {
	s := v.Stage
	// Both digest halves are original operands. The predecessor half is not
	// padding; entry/pair/Snapshot joins compare the complete 65 bytes.
	if v.Packed != nil || !claim.matches(record) || !v.Policy.valid() || s.Sequence == 0 || s.BeforeBank > 1 || s.CoreCodec != 2 || s.TransitionID == (storagebudget.ID{}) || s.Previous == (storageCompositionChecksum{}) || s.BeforePeer[0] != 0 || storageEnrollmentPeakZero(s.BeforePeer[1:33]) || s.Transition < storageCompositionEnrollment || s.Transition > storageCompositionMove {
		return storagebudget.ErrBounds
	}
	// FirstBase is the founding commitment of the composition lineage, part of
	// its frozen core, under the same rule as storageCompositionBank.validateCore.
	// An Enrollment stage founds its own lineage and carries none. A later stage
	// keeps its root's value: zero below an enrollment, or the exact first-base
	// commitment below a composition founded on an already completed E1 pair.
	// Every edge compares it exactly (storageCompositionFrozenCore); a native
	// frame never founds a first base, so a Base transition stays refused above.
	if s.Transition == storageCompositionEnrollment && s.FirstBase != (storagebudget.Digest{}) {
		return storagebudget.ErrBounds
	}
	if len(v.Overrides) < 2 || len(v.Overrides) > 270338 {
		return storagebudget.ErrBounds
	}
	n := int(storageEnrollmentPeakU16(v.Overrides[:2]))
	if n > 1024 || len(v.Overrides) != 2+264*n {
		return storagebudget.ErrBounds
	}
	var previous storagebudget.ID
	for i := 0; i < n; i++ {
		r := storageEnrollmentPeakReader{raw: v.Overrides[2+264*i : 2+264*(i+1)]}
		id, gen := r.id(), r.u64()
		var limits storagebudget.Limits
		storageEnrollmentPeakReadFixed(&r, reflect.ValueOf(&limits).Elem())
		if !r.done() || id == (storagebudget.ID{}) || gen == 0 || i > 0 && bytes.Compare(previous[:], id[:]) >= 0 || !storageOwnerLimitsValid(limits) {
			return storagebudget.ErrBounds
		}
		previous = id
	}
	if _, err := storageOwnerV6NativeStagePrefix(s, v.NativeBase, v.Boundary); err != nil {
		return err
	}
	if err := storageMoveValidateCurrents(s.Moves, s.Lineage, record); err != nil {
		return err
	}
	e, err := storageEnrollmentPeakDecodeEnvelopeNative(s.Core, record, claim, true, true, true)
	if err != nil {
		return err
	}
	completed := e.Kind == 2 && e.Marker != nil && e.Marker.Phase == 3
	if s.Transition == storageCompositionEnrollment && completed && v.Boundary != (storagePolicyQBoundary{e.Marker.Q3Epoch, e.Marker.Q3Revision}) {
		return storagebudget.ErrBounds
	}
	if s.Transition == storageCompositionMove {
		if s.Moves == nil || !completed || s.Publication != storageCompositionPrepared && s.Publication != storageCompositionActive || v.Before != nil {
			return storagebudget.ErrBounds
		}
		return nil
	}
	if s.Transition != storageCompositionEnrollment {
		group := map[byte]int{storageCompositionPlan: 0, storageCompositionCompletion: 1, storageCompositionStamp: 2, storageCompositionContinuation: 3}[s.Transition]
		if len(s.Lineage.Order[group]) == 0 || s.Lineage.Order[group][len(s.Lineage.Order[group])-1] != s.TransitionID {
			return storagebudget.ErrBounds
		}
		if !completed || s.Publication != storageCompositionPrepared && s.Publication != storageCompositionActive || v.Before != nil {
			return storagebudget.ErrBounds
		}
		return nil
	}
	if s.TransitionID != storagebudget.ID([16]byte(e.Scope[24:40])) || s.BeforeBank != e.Scope[322] || completed && s.Publication != storageCompositionActive || !completed && s.Publication != storageCompositionCorePublication {
		return storagebudget.ErrBounds
	}
	if e.Kind < 2 {
		if e.Snapshot == nil || e.Snapshot.Encoding != 6 || v.Before == nil || v.Before.Selected != s.BeforeBank || e.Snapshot.Full != v.Before.Full[s.BeforeBank] || e.Snapshot.Full != (storagebudget.Digest([32]byte(e.Scope[88:120]))) || s.BeforePeer != storageCompositionDuplicateCorePeer(e.Snapshot.Before, record, claim) {
			return storagebudget.ErrBounds
		}
		if _, err = v.Before.encode(); err != nil {
			return err
		}
	} else if v.Before != nil {
		return storagebudget.ErrBounds
	}
	group := uint16(0)
	if e.Kind == 0 {
		group = 9
		if e.Scope.path() == 1 {
			group = 60
		}
	} else if e.Kind == 1 {
		group = 66
	} else {
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
	shape, err := storageCompositionPublication(e.Scope.path(), e.Scope[322], group)
	if err != nil || shape.Publication != s.Publication {
		return storagebudget.ErrBounds
	}
	sequence, err := storageCompositionSequence(v.NativeBase, shape.Offset)
	if err != nil || sequence != s.Sequence {
		return storagebudget.ErrBounds
	}
	return nil
}

func (v storageOwnerV6NativeBank) encode(record storageOwnerRecordV2, claim storageOwnerClaimV2) ([]byte, error) {
	var err error
	v, err = storageMoveNativeOperands(v)
	if err != nil {
		return nil, err
	}
	if err := v.validate(record, claim); err != nil {
		return nil, err
	}
	core, err := storageCompositionCompactCore(v.Stage.Core)
	if err != nil {
		return nil, err
	}
	lineage, err := v.Stage.Lineage.encode()
	if err != nil {
		return nil, err
	}
	prefix, err := storageOwnerV6NativeStagePrefix(v.Stage, v.NativeBase, v.Boundary)
	if err != nil {
		return nil, err
	}
	var before []byte
	if v.Before != nil {
		before, err = v.Before.encode()
		if err != nil {
			return nil, err
		}
	}
	w := storageEnrollmentPeakWriter{max: storageOwnerCatalogBytes - 64}
	_, _, moves, err := storageMoveBankOperands(v.Stage)
	if err != nil {
		return nil, err
	}
	w.put(storageOwnerV6NativeMagic[:])
	if v.Stage.Moves == nil {
		w.u16(1)
	} else {
		w.u16(2)
	}
	w.u16(0)
	w.u32(uint32(len(core)))
	w.u32(uint32(len(lineage)))
	w.u32(uint32(len(v.Overrides)))
	w.u32(uint32(len(before)))
	w.u32(uint32(len(moves)))
	storageEnrollmentPeakWriteFixed(&w, reflect.ValueOf(v.Policy))
	w.put(prefix)
	w.put(make([]byte, 24))
	if len(w.raw) != storageOwnerV6NativePrefix {
		return nil, storagebudget.ErrBounds
	}
	w.put(core)
	w.put(lineage)
	w.put(v.Overrides)
	w.put(before)
	w.put(moves)
	if w.bad {
		return nil, storagebudget.ErrBounds
	}
	return storageOwnerV6Frame(w.raw, v.Stage.Sequence)
}
func storageOwnerV6NativeDecode(raw []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageOwnerV6NativeBank, error) {
	var v storageOwnerV6NativeBank
	payload, sequence, err := storageOwnerV6Payload(raw)
	if err != nil || len(payload) < storageOwnerV6NativePrefix || !bytes.Equal(payload[:8], storageOwnerV6NativeMagic[:]) {
		return v, storagebudget.ErrBounds
	}
	r := storageEnrollmentPeakReader{raw: payload[8:storageOwnerV6NativePrefix]}
	version := r.u16()
	if (version != 1 && version != 2) || r.u16() != 0 {
		return v, storagebudget.ErrBounds
	}
	cn, ln, sn, bn := uint64(r.u32()), uint64(r.u32()), uint64(r.u32()), uint64(r.u32())
	mn := uint64(r.u32())
	if version == 1 && mn != 0 || version == 2 && (mn < 4 || mn > storageOwnerCatalogBytes-64) || cn < 64 || cn > storageOwnerCatalogBytes || ln > storageCompositionLineageMax || sn < 2 || sn > 270338 || bn > storageOwnerV6BeforeMax || uint64(len(payload)) != storageOwnerV6NativePrefix+cn+ln+sn+bn+mn {
		return v, storagebudget.ErrBounds
	}
	storageEnrollmentPeakReadFixed(&r, reflect.ValueOf(&v.Policy).Elem())
	prefix := r.take(256)
	if !r.zeroTail() {
		return v, storagebudget.ErrBounds
	}
	body := storageEnrollmentPeakReader{raw: payload[storageOwnerV6NativePrefix:]}
	compact := body.take(int(cn))
	if int(storageEnrollmentPeakU32(compact[24:28]))+64 != len(compact) {
		return v, storagebudget.ErrBounds
	}
	core := make([]byte, storageOwnerCatalogBytes)
	copy(core, compact)
	lineage, err := storageCompositionReadLineage(body.take(int(ln)))
	if err != nil {
		return v, err
	}
	v.Overrides = append([]byte{}, body.take(int(sn))...)
	if bn > 0 {
		before, err := storageOwnerV6ReadBefore(body.take(int(bn)))
		if err != nil {
			return v, err
		}
		v.Before = &before
	}
	var operands storageMoveOperandTable
	if version == 2 {
		operands, err = storageMoveDecodeOperandTable(body.take(int(mn)))
		if err != nil {
			return v, err
		}
	}
	v.Stage, v.NativeBase, v.Boundary, err = storageOwnerV6NativeReadStagePrefixWithOperands(prefix, sequence, core, lineage, operands)
	if err != nil {
		return v, err
	}
	if v.Before != nil && (v.Before.Kind == 3 || v.Before.Kind == 1 && v.Before.Packed.SourceEncoding == 4) {
		v.Before.MoveOperands = operands
	}
	checked, checkErr := storageMoveNativeOperands(v)
	if checkErr != nil {
		return storageOwnerV6NativeBank{}, checkErr
	}
	_, _, expectedOperands, checkErr := storageMoveBankOperands(checked.Stage)
	_, _, actualOperands, actualErr := storageMoveBankOperands(v.Stage)
	if checkErr != nil || actualErr != nil || !bytes.Equal(expectedOperands, actualOperands) {
		return storageOwnerV6NativeBank{}, storagebudget.ErrBounds
	}
	if !body.done() || v.validate(record, claim) != nil {
		return storageOwnerV6NativeBank{}, storagebudget.ErrBounds
	}
	v.Stage.Raw = append([]byte{}, raw...)
	return v, nil
}
