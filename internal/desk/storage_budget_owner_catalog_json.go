package desk

import (
	"bytes"
	"encoding/json"
	"reflect"
	"unicode/utf8"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// This preflight reads the entire closed tree before encoding/json allocates
// decoded lists or byte slices. Struct order rejects duplicate/unknown/missing
// members; fixed arrays have exact lengths; aggregate counters include every
// occurrence rather than deduplicating equal data.
type storageOwnerCatalogScanner struct {
	storageOwnerJSONPreflight
	objects       int
	references    int
	sourceBytes   int
	transferPaths bool
}

func storageOwnerCatalogPreflight(raw []byte) bool {
	if len(raw) > storageOwnerCatalogBytes-64 || !utf8.Valid(raw) {
		return false
	}
	p := storageOwnerCatalogScanner{storageOwnerJSONPreflight: storageOwnerJSONPreflight{raw: raw}}
	return p.value(reflect.TypeOf(storageOwnerCatalogV2{}), "") && p.pos == len(raw)
}
func storageOwnerCatalogJSON(raw []byte, out *storageOwnerCatalogV2) error {
	if !storageOwnerCatalogPreflight(raw) {
		return storagebudget.ErrBounds
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return storagebudget.ErrBounds
	}
	b, err := json.Marshal(out)
	if err != nil || !bytes.Equal(raw, b) {
		return storagebudget.ErrBounds
	}
	return nil
}
func (p *storageOwnerCatalogScanner) value(t reflect.Type, field string) bool {
	switch t.Kind() {
	case reflect.Struct:
		if !p.take('{') {
			return false
		}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if i > 0 && !p.take(',') {
				return false
			}
			childField := f.Name
			if t == reflect.TypeOf(storageOwnerWorkspace{}) && f.Name == "OwnerKey" {
				childField = "WorkspaceOwnerKey"
			}
			if !p.literal(`"`+f.Name+`":`) || !p.value(f.Type, childField) {
				return false
			}
		}
		return p.take('}')
	case reflect.Array:
		if !p.take('[') {
			return false
		}
		for i := 0; i < t.Len(); i++ {
			if i > 0 && !p.take(',') {
				return false
			}
			if !p.value(t.Elem(), field) {
				return false
			}
		}
		return p.take(']')
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return p.byteString(field)
		}
		limit := 0
		switch field {
		case "Objects", "OwnerControls", "ExistingObjects":
			limit = 8192
		case "Roots":
			limit = 4096
		case "Workspaces", "Entries", "References":
			limit = 1024
		case "Filesystems":
			limit = 128
		case "Constraints":
			limit = 512
		case "Profiles":
			limit = 64
		default:
			return false
		}
		if !p.take('[') {
			return false
		}
		if p.take(']') {
			return true
		}
		for count := 0; ; count++ {
			if count >= limit {
				return false
			}
			if field == "Objects" || field == "OwnerControls" {
				p.objects++
				if p.objects > 8192 {
					return false
				}
			}
			if field == "References" {
				p.references++
				if p.references > 65536 {
					return false
				}
			}
			if !p.value(t.Elem(), field) {
				return false
			}
			if p.take(']') {
				return true
			}
			if !p.take(',') {
				return false
			}
		}
	case reflect.String:
		limit := 0
		switch field {
		case "Phase":
			limit = 8
		case "HomeOwner":
			limit = 64
		case "DeskID":
			limit = 32
		case "StorageOwner", "RunnerWorkspace":
			limit = 64
		case "Locator":
			limit = 256
		default:
			return false
		}
		return p.textString(limit)
	default:
		return p.storageOwnerJSONPreflight.value(t, field)
	}
}
func storageOwnerBase64Value(b byte) int {
	switch {
	case b >= 'A' && b <= 'Z':
		return int(b - 'A')
	case b >= 'a' && b <= 'z':
		return int(b-'a') + 26
	case b >= '0' && b <= '9':
		return int(b-'0') + 52
	case b == '+':
		return 62
	case b == '/':
		return 63
	}
	return -1
}
func (p *storageOwnerCatalogScanner) byteString(field string) bool {
	limit := 4096
	if p.transferPaths && (field == "Path" || field == "Previous") {
		limit = storageTransferPointerBytes
	}
	switch field {
	case "Name":
		limit = 1024
	case "Folder":
		limit = 120
	case "WorkspaceOwnerKey":
		limit = 256
	case "Relative":
		limit = 65599
	case "ConfigPath", "HomePath", "HistoryIdentity", "Path", "Previous", "OwnerKey":
	default:
		return false
	}
	if !p.take('"') {
		return false
	}
	start := p.pos
	for p.pos < len(p.raw) && p.raw[p.pos] != '"' {
		if p.pos-start >= 4*((limit+2)/3) || storageOwnerBase64Value(p.raw[p.pos]) < 0 && p.raw[p.pos] != '=' {
			return false
		}
		p.pos++
	}
	if p.pos >= len(p.raw) {
		return false
	}
	n := p.pos - start
	if n%4 != 0 {
		return false
	}
	decoded := n / 4 * 3
	if n > 0 {
		padding := 0
		if p.raw[p.pos-1] == '=' {
			padding++
			if p.raw[p.pos-2] == '=' {
				padding++
			}
		}
		for i := start; i < p.pos-padding; i++ {
			if storageOwnerBase64Value(p.raw[i]) < 0 {
				return false
			}
		}
		if padding == 1 && storageOwnerBase64Value(p.raw[p.pos-2])&3 != 0 || padding == 2 && storageOwnerBase64Value(p.raw[p.pos-3])&15 != 0 {
			return false
		}
		decoded -= padding
	}
	if decoded > limit {
		return false
	}
	p.pos++
	if storageOwnerAggregateByteField(field) {
		p.sourceBytes += decoded
		if p.sourceBytes > 4<<20 {
			return false
		}
	}
	return true
}

// Count decoded UTF-8 bytes without allocating the string. Canonical reencode
// subsequently rejects alternate escapes, including surrogate spellings.
func (p *storageOwnerCatalogScanner) textString(limit int) bool {
	if !p.take('"') {
		return false
	}
	decoded := 0
	for p.pos < len(p.raw) {
		b := p.raw[p.pos]
		p.pos++
		if b == '"' {
			return decoded <= limit
		}
		if b < 0x20 {
			return false
		}
		if b == '\\' {
			if p.pos >= len(p.raw) {
				return false
			}
			e := p.raw[p.pos]
			p.pos++
			if e == 'u' {
				v, ok := p.hex4()
				if !ok || v >= 0xd800 && v <= 0xdfff {
					return false
				}
				switch {
				case v < 0x80:
					decoded++
				case v < 0x800:
					decoded += 2
				default:
					decoded += 3
				}
			} else {
				if !bytes.ContainsRune([]byte(`"\/bfnrt`), rune(e)) {
					return false
				}
				decoded++
			}
		} else {
			decoded++
		}
		if decoded > limit {
			return false
		}
	}
	return false
}
func (p *storageOwnerCatalogScanner) hex4() (uint16, bool) {
	var v uint16
	for i := 0; i < 4; i++ {
		if p.pos >= len(p.raw) {
			return 0, false
		}
		b := p.raw[p.pos]
		p.pos++
		var n byte
		switch {
		case b >= '0' && b <= '9':
			n = b - '0'
		case b >= 'a' && b <= 'f':
			n = b - 'a' + 10
		case b >= 'A' && b <= 'F':
			n = b - 'A' + 10
		default:
			return 0, false
		}
		v = v*16 + uint16(n)
	}
	return v, true
}

// P's private logical catalog is explicitly Version1. Historical V2/V3 catalog
// entry points keep the scanner's original4096-byte path limit. This is only a
// preallocation/canonical-data check; native source and transfer action joins
// remain mandatory before any effect or retained-source reconstruction.
func storageEnrollmentPeakCatalogPreflight(raw []byte) bool {
	if len(raw) > storageOwnerCatalogBytes-64 || !utf8.Valid(raw) {
		return false
	}
	wide := bytes.HasPrefix(raw, []byte(`{"Version":1,`))
	scanner := storageOwnerCatalogScanner{storageOwnerJSONPreflight: storageOwnerJSONPreflight{raw: raw}, transferPaths: wide}
	return scanner.value(reflect.TypeOf(storageOwnerCatalogV2{}), "") && scanner.pos == len(raw)
}
func storageEnrollmentPeakCatalogJSON(raw []byte, out *storageOwnerCatalogV2) error {
	if !storageEnrollmentPeakCatalogPreflight(raw) {
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
