package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/jsonscalar"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Integer/scalar/code-point profile from Gateway SPEC.md/canon corpus. This is
// deliberately not RFC8785 or encoding/json's HTML-escaped serialization.
func gatewayCanonicalJSON(_ context.Context, raw []byte) ([]byte, error) {
	if !utf8.Valid(raw) || !json.Valid(raw) || jsonscalar.ValidateEscapes(raw) != nil || checkJSONMembers(raw, opaqueJSONMembers) != nil {
		return nil, errors.New("invalid canonical JSON")
	}
	var value any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&value); err != nil {
		return nil, err
	}
	var b strings.Builder
	writeString := func(s string) {
		b.WriteByte('"')
		for _, r := range s {
			switch r {
			case '"':
				b.WriteString(`\"`)
			case '\\':
				b.WriteString(`\\`)
			case '\b':
				b.WriteString(`\b`)
			case '\f':
				b.WriteString(`\f`)
			case '\n':
				b.WriteString(`\n`)
			case '\r':
				b.WriteString(`\r`)
			case '\t':
				b.WriteString(`\t`)
			default:
				if r < 32 {
					fmt.Fprintf(&b, `\u%04x`, r)
				} else {
					b.WriteRune(r)
				}
			}
		}
		b.WriteByte('"')
	}
	var write func(any) error
	write = func(v any) error {
		switch x := v.(type) {
		case nil:
			b.WriteString("null")
		case bool:
			b.WriteString(strconv.FormatBool(x))
		case string:
			writeString(x)
		case json.Number:
			n, err := strconv.ParseInt(string(x), 10, 64)
			if err != nil || n > 9007199254740991 || n < -9007199254740991 {
				return errors.New("outside canonical integer domain")
			}
			b.WriteString(strconv.FormatInt(n, 10))
		case []any:
			b.WriteByte('[')
			for i, item := range x {
				if i > 0 {
					b.WriteByte(',')
				}
				if err := write(item); err != nil {
					return err
				}
			}
			b.WriteByte(']')
		case map[string]any:
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			b.WriteByte('{')
			for i, k := range keys {
				if i > 0 {
					b.WriteByte(',')
				}
				writeString(k)
				b.WriteByte(':')
				if err := write(x[k]); err != nil {
					return err
				}
			}
			b.WriteByte('}')
		default:
			return errors.New("unsupported canonical value")
		}
		return nil
	}
	if err := write(value); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}
