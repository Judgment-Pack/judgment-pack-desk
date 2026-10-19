package desk

// Requested values use the portable research encodings. This is the host's
// early authoring check; Runtime/Runner remain authoritative for a Dialect and
// its preparation. Legacy string/number recipes keep their saved encoding.
import (
	"bytes"
	"encoding/json"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/jsonscalar"
	"math/big"
	"regexp"
	"unicode/utf8"
)

var researchDecimal = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?$`)
var researchInteger = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)
var researchFieldTypes = []string{"text", "integer", "decimal", "boolean", "text-list", "range", "string", "number"}

func researchIntegerValue(value any) (*big.Int, bool) {
	n, ok := value.(json.Number)
	if !ok || !researchInteger.MatchString(string(n)) {
		return nil, false
	}
	v, ok := new(big.Int).SetString(string(n), 10)
	return v, ok && v.CmpAbs(big.NewInt(1<<53-1)) <= 0
}
func researchDecimalValue(value any) (*big.Rat, bool) {
	s, ok := value.(string)
	if !ok || len(s) > 128 || !researchDecimal.MatchString(s) {
		return nil, false
	}
	v, ok := new(big.Rat).SetString(s)
	return v, ok
}
func validResearchValue(raw json.RawMessage, kind string) bool {
	if len(raw) == 0 || len(raw) > 512<<10 || rejectDuplicateMembers(raw) != nil {
		return false
	}
	if (kind == "string" || kind == "number") && len(raw) > 8192 {
		return false
	}
	if kind != "string" && kind != "number" && (!utf8.Valid(raw) || jsonscalar.ValidateEscapes(raw) != nil) {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value any
	if d.Decode(&value) != nil || value == nil {
		return false
	}
	switch kind {
	case "text", "string":
		s, ok := value.(string)
		return ok && (kind == "string" || len(s) <= 8192)
	case "number":
		n, ok := value.(json.Number)
		if !ok {
			return false
		}
		_, err := n.Float64()
		return err == nil
	case "integer":
		_, ok := researchIntegerValue(value)
		return ok
	case "decimal":
		_, ok := researchDecimalValue(value)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "text-list":
		list, ok := value.([]any)
		if !ok || len(list) > 64 {
			return false
		}
		for _, v := range list {
			s, ok := v.(string)
			if !ok || len(s) > 1024 {
				return false
			}
		}
		return true
	case "range":
		obj, ok := value.(map[string]any)
		if !ok || len(obj) != 2 {
			return false
		}
		if low, ok := researchIntegerValue(obj["min"]); ok {
			high, ok := researchIntegerValue(obj["max"])
			return ok && low.Cmp(high) <= 0
		}
		low, ok := researchDecimalValue(obj["min"])
		if !ok {
			return false
		}
		high, ok := researchDecimalValue(obj["max"])
		return ok && low.Cmp(high) <= 0
	}
	return false
}
