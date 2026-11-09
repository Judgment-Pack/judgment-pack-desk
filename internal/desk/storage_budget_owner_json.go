package desk

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strconv"
	"unicode/utf8"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// The preflight walks the closed type before Unmarshal can allocate decoded
// data. It requires each declared member exactly once in wire order, so aliases,
// duplicate members, unknown fields and omitted fields cannot be collapsed by
// encoding/json. Fixed arrays must have their exact declared lengths (Unmarshal
// alone accepts short arrays and discards extra elements).
func storageOwnerClosedJSON(raw []byte, out any) error {
	t := reflect.TypeOf(out)
	if t == nil || t.Kind() != reflect.Pointer || !utf8.Valid(raw) || len(raw) > storageOwnerRecordBytes-storageOwnerHeaderBytes {
		return storagebudget.ErrBounds
	}
	p := storageOwnerJSONPreflight{raw: raw}
	if !p.value(t.Elem(), "") || p.pos != len(raw) {
		return storagebudget.ErrBounds
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return storagebudget.ErrBounds
	}
	canonical, err := json.Marshal(out)
	if err != nil || !bytes.Equal(canonical, raw) {
		return storagebudget.ErrBounds
	}
	return nil
}

type storageOwnerJSONPreflight struct {
	raw []byte
	pos int
}

func (p *storageOwnerJSONPreflight) take(b byte) bool {
	if p.pos >= len(p.raw) || p.raw[p.pos] != b {
		return false
	}
	p.pos++
	return true
}
func (p *storageOwnerJSONPreflight) literal(s string) bool {
	if !bytes.HasPrefix(p.raw[p.pos:], []byte(s)) {
		return false
	}
	p.pos += len(s)
	return true
}
func (p *storageOwnerJSONPreflight) value(t reflect.Type, field string) bool {
	switch t.Kind() {
	case reflect.Struct:
		if !p.take('{') {
			return false
		}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if i != 0 && !p.take(',') {
				return false
			}
			if !p.literal(`"`+f.Name+`":`) || !p.value(f.Type, f.Name) {
				return false
			}
		}
		return p.take('}')
	case reflect.Array:
		if !p.take('[') {
			return false
		}
		for i := 0; i < t.Len(); i++ {
			if i != 0 && !p.take(',') {
				return false
			}
			if !p.value(t.Elem(), field) {
				return false
			}
		}
		return p.take(']')
	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		start := p.pos
		for p.pos < len(p.raw) && p.raw[p.pos] >= '0' && p.raw[p.pos] <= '9' {
			p.pos++
		}
		if p.pos == start || p.pos-start > 20 || p.pos-start > 1 && p.raw[start] == '0' {
			return false
		}
		_, err := strconv.ParseUint(string(p.raw[start:p.pos]), 10, t.Bits())
		return err == nil
	case reflect.String:
		// Record and claim have only these two closed text fields. Bound the raw
		// spelling before any string allocation; canonical comparison below rejects
		// escapes, surrogate replacement and all alternate spellings.
		limit := 0
		switch field {
		case "Kind":
			limit = 12
		case "HomeOwner":
			limit = 64
		default:
			return false
		}
		if !p.take('"') {
			return false
		}
		start := p.pos
		for p.pos < len(p.raw) {
			b := p.raw[p.pos]
			if b == '"' {
				if p.pos-start > limit {
					return false
				}
				p.pos++
				return true
			}
			// Neither field permits an escaped byte or a non-ASCII character.
			if b < ' ' || b >= 0x7f || b == '\\' || p.pos-start >= limit {
				return false
			}
			p.pos++
		}
	}
	return false
}
