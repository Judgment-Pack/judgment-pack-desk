package desk

// These helpers check consistency of already retained bytes. Signing, grants,
// source verification and reviewedInputSnapshot admission remain producer work.
import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/jsonscalar"
)

const (
	retainedSnapshotBytes    = 524288
	retainedFrameBytes       = 2097152
	retainedCarrierBytes     = 3145728
	retainedFieldCount       = 1000
	retainedDeclarationBytes = 16384
)

var retainedRunID = regexp.MustCompile(`^research_[a-f0-9]{32}$`)
var retainedPositiveInteger = regexp.MustCompile(`^[1-9][0-9]*$`)

type retainedColumnFrame struct {
	ContractVersion string                 `json:"contractVersion"`
	Origin          string                 `json:"origin"`
	Values          map[string]string      `json:"values"`
	Research        *retainedFrameResearch `json:"research"`
}
type retainedFrameResearch struct {
	RecordRecipeRevision string                        `json:"recordRecipeRevision"`
	Declaration          retainedFrameDeclaration      `json:"declaration"`
	Fields               map[string]retainedFrameField `json:"fields"`
}
type retainedFrameDeclaration struct {
	RecipeRevision string                          `json:"recipeRevision"`
	Fields         []retainedFrameDeclarationField `json:"fields"`
}
type retainedFrameDeclarationField struct {
	Name      string `json:"name"`
	FieldJSON string `json:"fieldJson"`
}
type retainedFrameField struct {
	Status    string  `json:"status"`
	ValueJSON *string `json:"valueJson,omitempty"`
}
type retainedReviewedCarrier struct {
	ContractVersion        string              `json:"contractVersion"`
	OriginalSnapshotJSON   string              `json:"originalSnapshotJson"`
	OriginalSnapshotSHA256 string              `json:"originalSnapshotSha256"`
	Retained               retainedColumnFrame `json:"retained"`
}

func retainedSafeVersion(version int) bool { return version > 0 && uint64(version) <= 9007199254740991 }
func retainedColumnName(name string) bool {
	return name != "" && len(name) <= 160 && utf8.ValidString(name)
}
func retainedDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
func retainedRevision(run string, version int) (string, error) {
	if !retainedRunID.MatchString(run) || !retainedSafeVersion(version) {
		return "", errors.New("Invalid original research revision.")
	}
	return retainedDigest([]byte("judgment-pack/retained-column/1/desk-reviewed-input/revision\x00" + run + "\x00" + strconv.Itoa(version))), nil
}

// retainedJSONNode keeps exact token spans. No JSON numbers enter binary64.
type retainedJSONNode struct {
	raw      []byte
	kind     byte
	members  map[string]*retainedJSONNode
	elements []*retainedJSONNode
	depth    int
}
type retainedJSONReader struct {
	raw []byte
	at  int
}

func retainedJSONSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }
func (r *retainedJSONReader) space() {
	for r.at < len(r.raw) && retainedJSONSpace(r.raw[r.at]) {
		r.at++
	}
}
func (r *retainedJSONReader) stringToken() []byte {
	start := r.at
	r.at++
	for r.raw[r.at] != '"' {
		if r.raw[r.at] == '\\' {
			r.at++
		}
		r.at++
	}
	r.at++
	return r.raw[start:r.at]
}

// Identity uses decoded UTF-16 units, preserving lone surrogates in opaque keys
// while treating escaped and literal spellings of the same key as identical.
func retainedJSONKey(raw []byte) string {
	var units []uint16
	for i := 1; i < len(raw)-1; {
		if raw[i] == '\\' {
			i++
			if raw[i] == 'u' {
				n, _ := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
				units = append(units, uint16(n))
				i += 5
				continue
			}
			escapes := map[byte]uint16{'"': '"', '\\': '\\', '/': '/', 'b': '\b', 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t'}
			units = append(units, escapes[raw[i]])
			i++
			continue
		}
		ch, size := utf8.DecodeRune(raw[i:])
		units = append(units, utf16.Encode([]rune{ch})...)
		i += size
	}
	encoded := make([]byte, len(units)*2)
	for i, unit := range units {
		encoded[2*i] = byte(unit >> 8)
		encoded[2*i+1] = byte(unit)
	}
	return string(encoded)
}
func (r *retainedJSONReader) value() (*retainedJSONNode, error) {
	r.space()
	start := r.at
	node := &retainedJSONNode{kind: r.raw[r.at]}
	switch node.kind {
	case '{':
		node.depth = 1
		node.members = map[string]*retainedJSONNode{}
		r.at++
		r.space()
		for r.raw[r.at] != '}' {
			key := retainedJSONKey(r.stringToken())
			r.space()
			r.at++
			child, err := r.value()
			if err != nil {
				return nil, err
			}
			if _, exists := node.members[key]; exists {
				return nil, errors.New("Duplicate decoded JSON member.")
			}
			node.members[key] = child
			if child.depth+1 > node.depth {
				node.depth = child.depth + 1
			}
			r.space()
			if r.raw[r.at] == '}' {
				break
			}
			r.at++
			r.space()
		}
		r.at++
	case '[':
		node.depth = 1
		r.at++
		r.space()
		for r.raw[r.at] != ']' {
			child, err := r.value()
			if err != nil {
				return nil, err
			}
			node.elements = append(node.elements, child)
			if child.depth+1 > node.depth {
				node.depth = child.depth + 1
			}
			r.space()
			if r.raw[r.at] == ']' {
				break
			}
			r.at++
		}
		r.at++
	case '"':
		r.stringToken()
	default:
		for r.at < len(r.raw) && !retainedJSONSpace(r.raw[r.at]) && r.raw[r.at] != ',' && r.raw[r.at] != '}' && r.raw[r.at] != ']' {
			r.at++
		}
	}
	node.raw = r.raw[start:r.at]
	return node, nil
}
func parseRetainedJSON(raw []byte) (*retainedJSONNode, error) {
	if !utf8.Valid(raw) || !json.Valid(raw) {
		return nil, errors.New("Malformed retained JSON.")
	}
	return (&retainedJSONReader{raw: raw}).value()
}
func (n *retainedJSONNode) member(name string) *retainedJSONNode {
	if n == nil || n.kind != '{' {
		return nil
	}
	raw, _ := json.Marshal(name)
	return n.members[retainedJSONKey(raw)]
}
func (n *retainedJSONNode) scalarString() (string, error) {
	if n == nil || n.kind != '"' || jsonscalar.ValidateEscapes(n.raw) != nil {
		return "", errors.New("Expected scalar JSON string.")
	}
	var value string
	err := json.Unmarshal(n.raw, &value)
	return value, err
}
func retainedMemberString(n *retainedJSONNode, name string) (string, error) {
	return n.member(name).scalarString()
}
func retainedCompactToken(raw []byte) string {
	var out bytes.Buffer
	quoted, escaped := false, false
	for _, c := range raw {
		if quoted {
			out.WriteByte(c)
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				quoted = false
			}
			continue
		}
		if c == '"' {
			quoted = true
		}
		if !retainedJSONSpace(c) {
			out.WriteByte(c)
		}
	}
	return out.String()
}
func retainedVersionNode(n *retainedJSONNode) (int, error) {
	if n == nil || !retainedPositiveInteger.Match(n.raw) {
		return 0, errors.New("Invalid original recipe version.")
	}
	v, err := strconv.ParseUint(string(n.raw), 10, 53)
	if err != nil || v == 0 {
		return 0, errors.New("Invalid original recipe version.")
	}
	return int(v), nil
}

// projectRetainedReviewedInput consumes the actual reviewedInputSnapshot result.
// It never reconstructs that snapshot from prepared or filtered values.
func projectRetainedReviewedInput(snapshot []byte, history retainedResearchHistory) (retainedReviewedCarrier, error) {
	var result retainedReviewedCarrier
	bad := errors.New("The original snapshot does not match its complete retained research history.")
	if len(snapshot) > retainedSnapshotBytes || history.Fields == nil {
		return result, bad
	}
	root, err := parseRetainedJSON(snapshot)
	if err != nil {
		return result, err
	}
	if root.kind != '{' {
		return result, bad
	}
	version := root.member("version")
	class, _ := retainedMemberString(root, "class")
	preparation, _ := retainedMemberString(root, "preparationId")
	record, research := root.member("record"), root.member("research")
	recordClass, _ := retainedMemberString(record, "class")
	recordID, _ := retainedMemberString(record, "id")
	runID, _ := retainedMemberString(research, "id")
	recordRunID, _ := retainedMemberString(record.member("research"), "runId")
	recipeVersion, err := retainedVersionNode(research.member("recipeVersion"))
	recordVersion, recordErr := retainedVersionNode(record.member("research").member("recipeVersion"))
	if version == nil || string(version.raw) != "1" || class != "generated" || recordClass != "generated" || preparation != history.PreparationID || recordID != history.RecordID || runID != history.RunID || recordRunID != runID || recipeVersion != history.RecipeVersion || recordVersion != recipeVersion || err != nil || recordErr != nil {
		return result, bad
	}
	revision, err := retainedRevision(runID, recipeVersion)
	if err != nil {
		return result, err
	}
	declarations := research.member("recipe").member("fields")
	if declarations == nil || declarations.kind != '[' || len(declarations.elements) != len(history.Fields) || len(history.Fields) > retainedFieldCount {
		return result, bad
	}
	inventory := make([]retainedFrameDeclarationField, 0, len(history.Fields))
	seen := map[string]bool{}
	for i, declaration := range declarations.elements {
		if declaration.kind != '{' || declaration.depth > 32 || jsonscalar.ValidateEscapes(declaration.raw) != nil {
			return result, bad
		}
		name, nameErr := retainedMemberString(declaration, "name")
		_, typeErr := retainedMemberString(declaration, "type")
		fieldJSON := retainedCompactToken(declaration.raw)
		expected, marshalErr := json.Marshal(history.Fields[i])
		if nameErr != nil || typeErr != nil || marshalErr != nil || !retainedColumnName(name) || seen[name] || len(fieldJSON) > retainedDeclarationBytes || fieldJSON != string(expected) {
			return result, bad
		}
		seen[name] = true
		inventory = append(inventory, retainedFrameDeclarationField{Name: name, FieldJSON: fieldJSON})
	}
	valuesNode := record.member("values")
	fieldsNode := record.member("research").member("entity").member("fields")
	if valuesNode == nil || valuesNode.kind != '{' || fieldsNode == nil || fieldsNode.kind != '{' || len(valuesNode.members) > retainedFieldCount || len(fieldsNode.members) > retainedFieldCount {
		return result, bad
	}
	values := map[string]string{}
	fields := map[string]retainedFrameField{}
	// Decode selected map keys only after scalar checking; opaque keys elsewhere
	// remain structural JSON data, including historical escaped lone surrogates.
	var selectedValues map[string]json.RawMessage
	if jsonscalar.ValidateEscapes(valuesNode.raw) != nil || json.Unmarshal(valuesNode.raw, &selectedValues) != nil {
		return result, bad
	}
	for name := range selectedValues {
		value, err := valuesNode.member(name).scalarString()
		if err != nil || !retainedColumnName(name) {
			return result, bad
		}
		values[name] = value
	}
	// Only names/status are scalar here. Legacy raw value tokens retain exemptions.
	for key, node := range fieldsNode.members {
		var name string
		units := make([]uint16, len(key)/2)
		for i := range units {
			units[i] = uint16(key[2*i])<<8 | uint16(key[2*i+1])
		}
		for i, unit := range units {
			if unit >= 0xd800 && unit <= 0xdbff {
				if i+1 >= len(units) || units[i+1] < 0xdc00 || units[i+1] > 0xdfff {
					return result, bad
				}
			} else if unit >= 0xdc00 && unit <= 0xdfff && (i == 0 || units[i-1] < 0xd800 || units[i-1] > 0xdbff) {
				return result, bad
			}
		}
		name = string(utf16.Decode(units))
		if !retainedColumnName(name) || node.kind != '{' {
			return result, bad
		}
		status, err := retainedMemberString(node, "status")
		if err != nil || !contains([]string{"supported", "interpretation", "missing", "conflicting", "inaccessible", "stale"}, status) {
			return result, bad
		}
		field := retainedFrameField{Status: status}
		if value := node.member("value"); value != nil {
			if len(value.raw) > retainedSnapshotBytes || value.depth > 64 {
				return result, bad
			}
			raw := string(value.raw)
			field.ValueJSON = &raw
		}
		fields[name] = field
	}
	frame := retainedColumnFrame{ContractVersion: "1", Origin: "generated", Values: values, Research: &retainedFrameResearch{RecordRecipeRevision: revision, Declaration: retainedFrameDeclaration{RecipeRevision: revision, Fields: inventory}, Fields: fields}}
	frameJSON, err := json.Marshal(frame)
	if err != nil || len(frameJSON) > retainedFrameBytes {
		return result, bad
	}
	result = retainedReviewedCarrier{ContractVersion: "1", OriginalSnapshotJSON: string(snapshot), OriginalSnapshotSHA256: retainedDigest(snapshot), Retained: frame}
	carrierJSON, err := json.Marshal(result)
	if err != nil || len(carrierJSON) > retainedCarrierBytes {
		return retainedReviewedCarrier{}, bad
	}
	return result, nil
}

// checkRetainedReviewedCarrier checks a closed wrapper against trusted local
// original history. Its success establishes consistency, never authenticity.
func checkRetainedReviewedCarrier(raw []byte, history retainedResearchHistory) (retainedReviewedCarrier, error) {
	var result retainedReviewedCarrier
	if len(raw) > retainedCarrierBytes {
		return result, errors.New("Retained carrier exceeds its byte limit.")
	}
	node, err := parseRetainedJSON(raw)
	if err != nil {
		return result, err
	}
	if node.kind != '{' || len(node.members) != 4 {
		return result, errors.New("Malformed retained carrier.")
	}
	for _, name := range []string{"contractVersion", "originalSnapshotJson", "originalSnapshotSha256", "retained"} {
		if node.member(name) == nil {
			return result, fmt.Errorf("Missing retained carrier member %s.", name)
		}
	}
	// Snapshot text itself is opaque: its embedded escaped surrogates are bytes,
	// whereas the outer string must decode without replacing any literal units.
	snapshot, err := retainedMemberString(node, "originalSnapshotJson")
	if err != nil {
		return result, err
	}
	expected, err := projectRetainedReviewedInput([]byte(snapshot), history)
	if err != nil {
		return result, err
	}
	frame := node.member("retained")
	if len(frame.raw) > retainedFrameBytes {
		return result, errors.New("Retained frame exceeds its byte limit.")
	}
	expectedFrame, _ := json.Marshal(expected.Retained)
	expectedNode, err := parseRetainedJSON(expectedFrame)
	if err != nil || !retainedJSONEqual(frame, expectedNode) {
		return result, errors.New("Retained frame differs from its exact projection.")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&result); err != nil || !reflect.DeepEqual(result, expected) {
		return retainedReviewedCarrier{}, errors.New("Retained carrier differs from its original snapshot projection.")
	}
	return result, nil
}

// Object membership is exact and case-sensitive; encoding/json's struct matching
// alone would incorrectly accept differently cased members in the closed frame.
func retainedJSONEqual(a, b *retainedJSONNode) bool {
	if a == nil || b == nil || a.kind != b.kind {
		return false
	}
	switch a.kind {
	case '{':
		if len(a.members) != len(b.members) {
			return false
		}
		for key, child := range a.members {
			if !retainedJSONEqual(child, b.members[key]) {
				return false
			}
		}
		return true
	case '[':
		if len(a.elements) != len(b.elements) {
			return false
		}
		for i, child := range a.elements {
			if !retainedJSONEqual(child, b.elements[i]) {
				return false
			}
		}
		return true
	case '"':
		left, err := a.scalarString()
		right, rightErr := b.scalarString()
		return err == nil && rightErr == nil && left == right
	default:
		return bytes.Equal(a.raw, b.raw)
	}
}
