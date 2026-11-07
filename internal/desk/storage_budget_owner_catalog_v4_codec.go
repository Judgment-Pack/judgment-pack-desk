package desk

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"reflect"
	"unicode/utf8"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

var storageOwnerV4Magic = [8]byte{'D', 'S', 'K', 'O', 'W', 'N', '4', 0}

func storageOwnerV4Frame(payload []byte, sequence uint64) ([]byte, error) {
	if sequence == 0 || len(payload) > storageOwnerCatalogBytes-64 {
		return nil, storagebudget.ErrBounds
	}
	raw := make([]byte, storageOwnerCatalogBytes)
	copy(raw, storageOwnerV4Magic[:])
	binary.LittleEndian.PutUint32(raw[8:12], 4)
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
func storageOwnerV4Payload(raw []byte) ([]byte, uint64, error) {
	if len(raw) != storageOwnerCatalogBytes || !bytes.Equal(raw[:8], storageOwnerV4Magic[:]) || binary.LittleEndian.Uint32(raw[8:12]) != 4 || binary.LittleEndian.Uint32(raw[12:16]) != 64 || binary.LittleEndian.Uint32(raw[28:32]) != 0 {
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
func storageOwnerV4Encode(c storageOwnerV4Catalog, r storageOwnerRecordV2, claim storageOwnerClaimV2) ([]byte, error) {
	if err := c.validate(r, claim); err != nil {
		return nil, err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	if !storageOwnerV4Preflight(b) {
		return nil, storagebudget.ErrBounds
	}
	return storageOwnerV4Frame(b, c.Sequence)
}
func storageOwnerV4Decode(raw []byte, r storageOwnerRecordV2, claim storageOwnerClaimV2) (storageOwnerV4Catalog, error) {
	var c storageOwnerV4Catalog
	b, seq, err := storageOwnerV4Payload(raw)
	if err != nil {
		return c, err
	}
	if !storageOwnerV4Preflight(b) || json.Unmarshal(b, &c) != nil {
		return storageOwnerV4Catalog{}, storagebudget.ErrBounds
	}
	canonical, err := json.Marshal(c)
	if err != nil || !bytes.Equal(b, canonical) || c.Sequence != seq {
		return storageOwnerV4Catalog{}, storagebudget.ErrBounds
	}
	if err = c.validate(r, claim); err != nil {
		return storageOwnerV4Catalog{}, err
	}
	return c, nil
}

// New rows are counted and bounded before Unmarshal allocates their slices.
// Existing subtrees use the unchanged v3 scanner and its shared aggregate counters.
type storageOwnerV4Scanner struct{ old storageOwnerCatalogScanner }

func storageOwnerV4Preflight(raw []byte) bool {
	if len(raw) > storageOwnerCatalogBytes-64 || !utf8.Valid(raw) {
		return false
	}
	p := storageOwnerV4Scanner{old: storageOwnerCatalogScanner{storageOwnerJSONPreflight: storageOwnerJSONPreflight{raw: raw}}}
	return p.value(reflect.TypeOf(storageOwnerV4Catalog{}), "") && p.old.pos == len(raw)
}
func (p *storageOwnerV4Scanner) value(t reflect.Type, field string) bool {
	switch t {
	case reflect.TypeOf(storageOwnerV4Catalog{}), reflect.TypeOf(storageOwnerV4UpgradeOrigin{}), reflect.TypeOf(storageOwnerV4Workspace{}), reflect.TypeOf(storageOwnerV4PrivateRoot{}), reflect.TypeOf(storageOwnerV4LineagePlan{}), reflect.TypeOf(storageOwnerV4Receipt{}), reflect.TypeOf(storageOwnerV4LineageCompletion{}), reflect.TypeOf(storageOwnerV4FromContinuation{}):
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
	case "LineagePlans", "LineageCompletions", "LineageContinuations":
		rowMax, totalMax := storageOwnerV4PlanBytes, 2<<20
		if field == "LineageCompletions" {
			rowMax, totalMax = storageOwnerV4CompletionBytes, 1<<20
		}
		if field == "LineageContinuations" {
			rowMax, totalMax = storageOwnerV4ContinuationBytes, 2<<20
		}
		if !p.old.take('[') {
			return false
		}
		start := p.old.pos - 1
		if p.old.take(']') {
			return true
		}
		for count := 0; ; count++ {
			if count >= storageOwnerV4Rows {
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
