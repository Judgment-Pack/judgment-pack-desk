package desk

// Recipe bindings retain an exact, user-selected pack snapshot. They are not
// a reviewed-set lock, a test result, or permission to run a decision. This
// supplied-input slice accepts only mapping-v2 case copies; generated sources
// must use verified source profiles before they can enter this boundary.
import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"

	agentcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

type inputDialectFact struct {
	Target     string                   `json:"target"`
	Source     string                   `json:"source"`
	Conversion *inputRetainedConversion `json:"conversion,omitempty"`
}
type inputDialectEvidence struct {
	Requirement  string                     `json:"requirement"`
	Source       string                     `json:"source"`
	Availability *inputRetainedAvailability `json:"availability,omitempty"`
}
type inputCaseDialect struct {
	Facts    []inputDialectFact     `json:"facts"`
	Evidence []inputDialectEvidence `json:"evidence"`
}
type inputRecordDialect struct {
	Version int               `json:"version"`
	Profile *string           `json:"profile,omitempty"`
	Case    *inputCaseDialect `json:"case,omitempty"`
	// Raw entries are refused below. Keeping the array makes this directly
	// consumable by the existing shared mapping editor and Runner v2 contract.
	Sources          []json.RawMessage `json:"sources"`
	Unmapped         []string          `json:"unmapped,omitempty"`
	UnmappedEvidence []string          `json:"unmappedEvidence,omitempty"`
}
type inputQualification struct {
	ConversionContract *retainedConversionContract `json:"conversionContract,omitempty"`
	Conversions        map[string]inputConversion  `json:"conversions,omitempty"`
	Kind               string                      `json:"kind"`
	Key                string                      `json:"key"`
	Pack               string                      `json:"pack"`
	Digest             string                      `json:"digest"`
	Mapping            inputRecordDialect          `json:"mapping"`
}

func inputPointerValid(p string) bool {
	if !strings.HasPrefix(p, "/") || len(p) > 1024 {
		return false
	}
	for i := 0; i < len(p); i++ {
		if p[i] == '~' {
			if i+1 >= len(p) || p[i+1] != '0' && p[i+1] != '1' {
				return false
			}
			i++
		}
	}
	for _, part := range strings.Split(p[1:], "/") {
		if part == "__proto__" || part == "constructor" || part == "prototype" {
			return false
		}
	}
	return true
}

// Desk reads these controls by exact name. Other pack branches belong to
// Runtime's grammar and remain opaque here, including literal fact values.
// These shapes validate names only; they never replace retained pack bytes.
type inputPackMembers struct {
	ID                   json.RawMessage `json:"id"`
	Version              json.RawMessage `json:"version"`
	Decision             json.RawMessage `json:"decision"`
	Title                json.RawMessage `json:"title"`
	Description          json.RawMessage `json:"description"`
	EvidenceRequirements []struct {
		ID                json.RawMessage `json:"id"`
		Description       json.RawMessage `json:"description"`
		AdditionalMembers json.RawMessage `json:"-"`
	} `json:"evidenceRequirements"`
	AdditionalMembers json.RawMessage `json:"-"`
}

func readInputPack(raw []byte) (map[string]any, error) {
	if err := checkJSONMembers(raw, reflect.TypeOf(inputPackMembers{})); err != nil {
		return nil, err
	}
	var pack map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&pack); err != nil || pack == nil {
		return nil, errors.New("Invalid retained pack.")
	}
	return pack, nil
}

func validateInputQualification(q *inputQualification) error {
	if q == nil {
		return nil
	}
	if q.ConversionContract != nil {
		return validateRetainedInputQualification(q)
	}
	if q.Mapping.Profile != nil {
		return errors.New("Selected retained-column recipes are not available on this path.")
	}
	if q.Mapping.Case != nil {
		for _, f := range q.Mapping.Case.Facts {
			if f.Conversion != nil {
				return errors.New("Select the retained-column contract before using conversion leaves.")
			}
		}
		for _, e := range q.Mapping.Case.Evidence {
			if e.Availability != nil {
				return errors.New("Select the retained-column contract before using availability leaves.")
			}
		}
	}
	fail := func() error { return errors.New("Choose a saved pack and review its dialect.") }
	if q.Kind != "pack" || strings.TrimSpace(q.Key) == "" || len(q.Key) > 512 || len(q.Pack) > 1<<20 || q.Digest != "sha256:"+digestOf([]byte(q.Pack)) {
		return fail()
	}
	pack, err := readInputPack([]byte(q.Pack))
	if err != nil {
		return fail()
	}
	// Identity belongs to the retained bytes, not a title supplied separately.
	if id, ok := pack["id"].(string); !ok || id == "" {
		return fail()
	}
	if _, ok := pack["decision"].(map[string]any); !ok {
		return fail()
	}
	if version, ok := pack["version"].(string); !ok || version == "" {
		return fail()
	}
	for _, field := range []string{"title", "description"} {
		if value, exists := pack[field]; exists {
			if _, ok := value.(string); !ok {
				return fail()
			}
		}
	}
	facts, evidence := map[string]bool{}, map[string]bool{}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if x["op"] == "fact" {
				if p, ok := x["path"].(string); ok {
					facts[p] = true
				}
			}
			for _, child := range x {
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(pack)
	if value, exists := pack["evidenceRequirements"]; exists {
		rows, ok := value.([]any)
		if !ok {
			return fail()
		}
		for _, value := range rows {
			row, ok := value.(map[string]any)
			if !ok {
				return fail()
			}
			id, ok := row["id"].(string)
			if !ok || id == "" || evidence[id] {
				return fail()
			}
			if description, exists := row["description"]; exists {
				if _, ok := description.(string); !ok {
					return fail()
				}
			}
			evidence[id] = true
		}
	}
	m := q.Mapping
	if m.Version != 2 || len(m.Sources) > 0 || len(m.Unmapped) > 1000 || len(m.UnmappedEvidence) > 1000 {
		return fail()
	}
	targets, requirements := map[string]bool{}, map[string]bool{}
	if m.Case != nil {
		if len(m.Case.Facts) > 1000 || len(m.Case.Evidence) > 1000 {
			return fail()
		}
		for _, f := range m.Case.Facts {
			if !facts[f.Target] || !inputPointerValid(f.Target) || !inputPointerValid(f.Source) || !strings.HasPrefix(f.Source, "/values/") || strings.Contains(strings.TrimPrefix(f.Source, "/values/"), "/") || targets[f.Target] {
				return fail()
			}
			for prior := range targets {
				if strings.HasPrefix(prior, f.Target+"/") || strings.HasPrefix(f.Target, prior+"/") {
					return fail()
				}
			}
			targets[f.Target] = true
		}
		for _, e := range m.Case.Evidence {
			if !evidence[e.Requirement] || requirements[e.Requirement] || !inputPointerValid(e.Source) || !strings.HasPrefix(e.Source, "/values/") || strings.Contains(strings.TrimPrefix(e.Source, "/values/"), "/") {
				return fail()
			}
			requirements[e.Requirement] = true
		}
	}
	for _, target := range m.Unmapped {
		if !facts[target] || targets[target] {
			return fail()
		}
		targets[target] = true
	}
	for _, requirement := range m.UnmappedEvidence {
		if !evidence[requirement] || requirements[requirement] {
			return fail()
		}
		requirements[requirement] = true
	}
	if q.Mapping.Sources == nil {
		q.Mapping.Sources = []json.RawMessage{}
	}
	if c := q.Mapping.Case; c != nil {
		if c.Facts == nil {
			c.Facts = []inputDialectFact{}
		}
		if c.Evidence == nil {
			c.Evidence = []inputDialectEvidence{}
		}
	}
	return validateInputConversions(q)
}

// Selected saved recipes retain logical rules only. Runtime checks fresh save
// candidates separately; local decoding never acquires or executes an input.
func validateRetainedInputQualification(q *inputQualification) error {
	if _, err := retainedInputMappingRules(q); err != nil {
		return err
	}
	if q.Kind != "pack" || strings.TrimSpace(q.Key) == "" || len(q.Key) > 512 || len(q.Pack) > 1<<20 || q.Digest != retainedDigest([]byte(q.Pack)) {
		return retainedContractFailure("input-malformed")
	}
	pack, err := readInputPack([]byte(q.Pack))
	if err != nil {
		return err
	}
	if _, ok := pack["decision"].(map[string]any); !ok {
		return retainedContractFailure("input-malformed")
	}
	for _, name := range []string{"title", "description"} {
		if value, exists := pack[name]; exists {
			if _, ok := value.(string); !ok {
				return retainedContractFailure("input-malformed")
			}
		}
	}
	inventory, err := retainedDialectPackInventory([]byte(q.Pack))
	if err != nil {
		return err
	}
	for _, fact := range q.Mapping.Case.Facts {
		if !contains(inventory.Facts, fact.Target) {
			return retainedContractFailure("input-malformed")
		}
	}
	for _, target := range q.Mapping.Unmapped {
		if !contains(inventory.Facts, target) {
			return retainedContractFailure("input-malformed")
		}
	}
	for _, evidence := range q.Mapping.Case.Evidence {
		if !contains(inventory.Evidence, evidence.Requirement) {
			return retainedContractFailure("input-malformed")
		}
	}
	for _, requirement := range q.Mapping.UnmappedEvidence {
		if !contains(inventory.Evidence, requirement) {
			return retainedContractFailure("input-malformed")
		}
	}
	raw, err := json.Marshal(q.Mapping)
	if err != nil {
		return err
	}
	if len(raw) > 1<<20 {
		return retainedContractFailure("input-over-bound")
	}
	return nil
}

// New selected controls cannot enter legacy conversion, publication or durable
// execution. This is a refusal guard, not an opt-in or an authorization grant.
func requireOrdinaryInputQualification(q *inputQualification) error {
	if q == nil {
		return nil
	}
	selected := q.ConversionContract != nil || q.Mapping.Profile != nil
	if q.Mapping.Case != nil {
		for _, fact := range q.Mapping.Case.Facts {
			selected = selected || fact.Conversion != nil
		}
		for _, evidence := range q.Mapping.Case.Evidence {
			selected = selected || evidence.Availability != nil
		}
	}
	if selected {
		return retainedContractFailure("capability-unsupported")
	}
	return nil
}

// Saved identity has no operation roles. Host owners derive their own required
// roles only after admitting this exact, explicitly selected recipe revision.
type retainedConversionContract = agentcontext.DialectConversionContract

func inputRetainedContract() retainedConversionContract {
	return agentcontext.DialectRetainedColumnDraft()
}

// Custom readers preserve absent optional members while refusing explicit null,
// aliases, duplicates and unknown controls before encoding/json can erase them.
// They do not opt a recipe into any public operation.
func decodeInputShape(raw []byte, target any, shape reflect.Type, nonnull ...string) error {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(raw, &members); err != nil {
		return err
	}
	selected := len(nonnull) == 0
	for name := range members {
		for _, control := range nonnull {
			if strings.EqualFold(name, control) {
				selected = true
			}
		}
	}
	// Preserve the historical decoder when no new member is supplied. Requests
	// still pass their existing outer exact-control checker independently.
	if selected {
		if err := checkJSONMembers(raw, shape); err != nil {
			return err
		}
	}
	for _, name := range nonnull {
		if value, present := members[name]; present && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errors.New("Optional selected members must be omitted, not null.")
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}
func (q *inputQualification) UnmarshalJSON(raw []byte) error {
	type plain inputQualification
	var value plain
	if err := decodeInputShape(raw, &value, reflect.TypeOf(value), "conversionContract"); err != nil {
		return err
	}
	if value.ConversionContract != nil {
		node, err := parseRetainedJSON(raw)
		if err != nil {
			return err
		}
		mapping := node.member("mapping")
		if !retainedExactObject(mapping, []string{"version", "profile", "case", "sources"}, "unmapped", "unmappedEvidence") || !retainedExactObject(mapping.member("case"), []string{"facts", "evidence"}) {
			return retainedContractFailure("input-malformed")
		}
		for _, list := range []*retainedJSONNode{mapping.member("sources"), mapping.member("case").member("facts"), mapping.member("case").member("evidence")} {
			if list == nil || list.kind != '[' {
				return retainedContractFailure("input-malformed")
			}
		}
		selection := node.member("conversionContract")
		if !retainedExactObject(selection, []string{"contract", "spec", "profile"}) {
			return retainedContractFailure("input-malformed")
		}
		if _, _, _, err := retainedReadIdentity(selection); err != nil {
			return err
		}
		if sidecars := node.member("conversions"); sidecars != nil {
			if sidecars.kind != '{' {
				return retainedContractFailure("input-malformed")
			}
			for _, sidecar := range sidecars.members {
				kind, err := retainedMemberString(sidecar, "kind")
				if err != nil {
					return err
				}
				required := []string{"kind"}
				if kind == "boolean" {
					required = append(required, "trueValue", "falseValue")
				}
				if !retainedExactObject(sidecar, required) {
					return retainedContractFailure("input-malformed")
				}
				for _, key := range required {
					if _, err := retainedMemberString(sidecar, key); err != nil {
						return err
					}
				}
			}
		}
	}
	*q = inputQualification(value)
	return nil
}
func (m *inputRecordDialect) UnmarshalJSON(raw []byte) error {
	type plain inputRecordDialect
	var value plain
	if err := decodeInputShape(raw, &value, reflect.TypeOf(value), "profile"); err != nil {
		return err
	}
	*m = inputRecordDialect(value)
	return nil
}
func (f *inputDialectFact) UnmarshalJSON(raw []byte) error {
	type plain inputDialectFact
	var value plain
	if err := decodeInputShape(raw, &value, reflect.TypeOf(value), "conversion"); err != nil {
		return err
	}
	*f = inputDialectFact(value)
	return nil
}
func (e *inputDialectEvidence) UnmarshalJSON(raw []byte) error {
	type plain inputDialectEvidence
	var value plain
	if err := decodeInputShape(raw, &value, reflect.TypeOf(value), "availability"); err != nil {
		return err
	}
	*e = inputDialectEvidence(value)
	return nil
}
