package desk

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter/wire"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

// Identity comes from the actual describe_runtime MCP result, independently of
// the process-generation nonce used to guard execution ownership.
// The returned value is untrusted admission input; Agent validates it at opening.
func assistantRuntimeCacheIdentity(result json.RawMessage) *runcontext.RuntimeIdentity {
	descriptor, version, ok := assistantRuntimeCacheDescriptor(result)
	if !ok || version == "" {
		return nil
	}
	lower := strings.ToLower(version)
	if utf8.ValidString(version) && (strings.Contains(lower, "dev") || strings.Contains(lower, "unknown") || strings.HasPrefix(lower, "0.0.0")) {
		return nil
	}
	return &runcontext.RuntimeIdentity{Identity: digestOf(descriptor), Version: version}
}

// String tokens are reversibly encoded as fixed-width UTF-16 hex units before
// wire.Parse. Every string is transformed, so literal marker/backslash text
// cannot collide with lone surrogate escapes. wire retains JS number semantics;
// the private writer restores original UTF-16 values and JSON.stringify rules.
func assistantRuntimeCacheDescriptor(result []byte) ([]byte, string, bool) {
	transformed, ok := assistantRuntimeTransformStrings(result)
	if !ok {
		return nil, "", false
	}
	reply, err := wire.Parse(transformed)
	if err != nil || !reply.IsObject() {
		return nil, "", false
	}
	get := func(v *wire.Value, key string) *wire.Value { return v.Get(assistantRuntimeEncodeString(key)) }
	descriptor := get(reply, "structuredContent")
	if descriptor == nil || descriptor.Kind() == wire.KindNull {
		var texts []uint16
		textCount := 0
		for _, part := range get(reply, "content").Items() {
			if assistantRuntimeDecodeString(get(part, "type").Text()) == "text" {
				if textCount > 0 {
					texts = append(texts, 10)
				}
				textCount++
				texts = append(texts, assistantRuntimeUnits(get(part, "text").Text())...)
			}
		}
		raw, ok := assistantRuntimeTransformUnits(texts)
		if !ok {
			return nil, "", false
		}
		descriptor, err = wire.Parse(raw)
		if err != nil {
			return nil, "", false
		}
	}
	if !descriptor.IsObject() {
		return nil, "", false
	}
	if assistantRuntimeTruthy(get(reply, "isError")) && assistantRuntimeDecodeString(get(descriptor, "status").Text()) != "invalid" {
		return nil, "", false
	}
	tool := get(descriptor, "tool")
	if !tool.IsObject() || !get(tool, "name").IsString() || !get(tool, "version").IsString() {
		return nil, "", false
	}
	version := assistantRuntimeVersionString(get(tool, "version").Text())
	var canonical bytes.Buffer
	assistantRuntimeWriteJSON(&canonical, descriptor)
	return canonical.Bytes(), version, true
}
func assistantRuntimeEncodeString(s string) string {
	var b strings.Builder
	for _, unit := range utf16.Encode([]rune(s)) {
		var raw [2]byte
		raw[0] = byte(unit >> 8)
		raw[1] = byte(unit)
		b.WriteString(hex.EncodeToString(raw[:]))
	}
	return b.String()
}
func assistantRuntimeUnits(s string) []uint16 {
	raw, err := hex.DecodeString(s)
	if err != nil || len(raw)%2 != 0 {
		return nil
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = uint16(raw[i*2])<<8 | uint16(raw[i*2+1])
	}
	return units
}
func assistantRuntimeDecodeString(s string) string {
	return string(utf16.Decode(assistantRuntimeUnits(s)))
}

// Agent refuses invalid UTF-8 RuntimeIdentity fields at run opening. Preserve
// lone version surrogates as their invalid UTF-8 encoding so they reach that
// refusal instead of becoming an admitted U+FFFD or an unavailable/dev identity.
// Other descriptor strings keep their original UTF-16 canonicalization rules.
func assistantRuntimeVersionString(s string) string {
	units := assistantRuntimeUnits(s)
	var b strings.Builder
	for i := 0; i < len(units); i++ {
		unit := units[i]
		if unit >= 0xd800 && unit <= 0xdbff && i+1 < len(units) && units[i+1] >= 0xdc00 && units[i+1] <= 0xdfff {
			b.WriteRune(utf16.DecodeRune(rune(unit), rune(units[i+1])))
			i++
		} else if unit >= 0xd800 && unit <= 0xdfff {
			b.WriteByte(0xe0 | byte(unit>>12))
			b.WriteByte(0x80 | byte(unit>>6&0x3f))
			b.WriteByte(0x80 | byte(unit&0x3f))
		} else {
			b.WriteRune(rune(unit))
		}
	}
	return b.String()
}
func assistantRuntimeTransformStrings(raw []byte) ([]byte, bool) {
	if !utf8.Valid(raw) {
		return nil, false
	}
	return assistantRuntimeTransformUnits(utf16.Encode([]rune(string(raw))))
}
func assistantRuntimeTransformUnits(raw []uint16) ([]byte, bool) {
	var b bytes.Buffer
	for i := 0; i < len(raw); {
		if raw[i] != '"' {
			if raw[i] > 127 {
				return nil, false
			}
			b.WriteByte(byte(raw[i]))
			i++
			continue
		}
		i++
		var units []uint16
		closed := false
		for i < len(raw) {
			ch := raw[i]
			i++
			if ch == '"' {
				closed = true
				break
			}
			if ch < 0x20 {
				return nil, false
			}
			if ch == '\\' {
				if i == len(raw) {
					return nil, false
				}
				escape := raw[i]
				i++
				switch escape {
				case '"', '\\', '/':
					units = append(units, escape)
				case 'b':
					units = append(units, 8)
				case 'f':
					units = append(units, 12)
				case 'n':
					units = append(units, 10)
				case 'r':
					units = append(units, 13)
				case 't':
					units = append(units, 9)
				case 'u':
					if i+4 > len(raw) {
						return nil, false
					}
					var digits [4]byte
					for j := range digits {
						ch := raw[i+j]
						if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')) {
							return nil, false
						}
						digits[j] = byte(ch)
					}
					number, err := strconv.ParseUint(string(digits[:]), 16, 16)
					if err != nil {
						return nil, false
					}
					units = append(units, uint16(number))
					i += 4
				default:
					return nil, false
				}
			} else {
				units = append(units, ch)
			}
		}
		if !closed {
			return nil, false
		}
		b.WriteByte('"')
		for _, unit := range units {
			var pair [2]byte
			pair[0] = byte(unit >> 8)
			pair[1] = byte(unit)
			b.WriteString(hex.EncodeToString(pair[:]))
		}
		b.WriteByte('"')
	}
	return b.Bytes(), true
}
func assistantRuntimeWriteString(b *bytes.Buffer, encoded string) {
	units := assistantRuntimeUnits(encoded)
	b.WriteByte('"')
	for i := 0; i < len(units); i++ {
		unit := units[i]
		switch unit {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteByte(byte(unit))
		case 8:
			b.WriteString(`\b`)
		case 12:
			b.WriteString(`\f`)
		case 10:
			b.WriteString(`\n`)
		case 13:
			b.WriteString(`\r`)
		case 9:
			b.WriteString(`\t`)
		default:
			if unit < 0x20 || (unit >= 0xd800 && unit <= 0xdfff) {
				if unit >= 0xd800 && unit <= 0xdbff && i+1 < len(units) && units[i+1] >= 0xdc00 && units[i+1] <= 0xdfff {
					b.WriteRune(utf16.DecodeRune(rune(unit), rune(units[i+1])))
					i++
				} else {
					b.WriteString(`\u`)
					var pair [2]byte
					pair[0] = byte(unit >> 8)
					pair[1] = byte(unit)
					b.WriteString(hex.EncodeToString(pair[:]))
				}
			} else {
				b.WriteRune(rune(unit))
			}
		}
	}
	b.WriteByte('"')
}
func assistantRuntimeArrayIndex(encoded string) (uint64, bool) {
	key := assistantRuntimeDecodeString(encoded)
	if key == "" || (len(key) > 1 && key[0] == '0') {
		return 0, false
	}
	for _, ch := range key {
		if ch < '0' || ch > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseUint(key, 10, 32)
	return n, err == nil && n < 4294967295
}
func assistantRuntimeWriteJSON(b *bytes.Buffer, v *wire.Value) {
	switch v.Kind() {
	case wire.KindString:
		assistantRuntimeWriteString(b, v.Text())
	case wire.KindArray:
		b.WriteByte('[')
		for i, item := range v.Items() {
			if i > 0 {
				b.WriteByte(',')
			}
			assistantRuntimeWriteJSON(b, item)
		}
		b.WriteByte(']')
	case wire.KindObject:
		keys := v.Keys()
		sort.Slice(keys, func(i, j int) bool {
			a, ai := assistantRuntimeArrayIndex(keys[i])
			z, zi := assistantRuntimeArrayIndex(keys[j])
			if ai && zi {
				return a < z
			}
			if ai != zi {
				return ai
			}
			return keys[i] < keys[j] // Fixed-width hex compares original UTF-16 units.
		})
		b.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			assistantRuntimeWriteString(b, key)
			b.WriteByte(':')
			assistantRuntimeWriteJSON(b, v.Get(key))
		}
		b.WriteByte('}')
	default:
		b.Write(v.JSON())
	}
}

func assistantRuntimeTruthy(v *wire.Value) bool {
	switch v.Kind() {
	case wire.KindNull:
		return false
	case wire.KindBool:
		b, _ := v.Boolean()
		return b
	case wire.KindNumber:
		n, _ := v.Number()
		return n != 0
	case wire.KindString:
		return v.Text() != ""
	default:
		return true
	}
}
