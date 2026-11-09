package desk

// Conversions belong to a Desk recipe, never to the released mapping-v2 DSL.
// Supplied assertions and reviewed generated records enter this path. Original source bytes and values
// stay immutable; the source case retains the conversion policy and provenance.
import (
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

type inputConversion struct {
	Kind       string `json:"kind"`
	TrueValue  string `json:"trueValue,omitempty"`
	FalseValue string `json:"falseValue,omitempty"`
}
type inputConversionIssue struct {
	Target  string `json:"target"`
	Message string `json:"message"`
}
type inputPreparedRow struct {
	ID       string                 `json:"id"`
	Origin   string                 `json:"origin"`
	Original map[string]string      `json:"original"`
	Prepared map[string]any         `json:"prepared"`
	Issues   []inputConversionIssue `json:"issues"`
	Input    json.RawMessage        `json:"input,omitempty"`
}

var inputDecimal = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?$`)

var inputNumber = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

func inputUnescape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~1", "/"), "~0", "~")
}
func inputEscape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}
func validateInputConversions(q *inputQualification) error {
	assigned := map[string]bool{}
	if q.Mapping.Case != nil {
		for _, f := range q.Mapping.Case.Facts {
			assigned[f.Target] = true
		}
	}
	for target, c := range q.Conversions {
		if !assigned[target] {
			return errors.New("Conversions must belong to a mapped input.")
		}
		switch c.Kind {
		case "text", "number", "decimal", "research-value":
			if c.TrueValue != "" || c.FalseValue != "" {
				return errors.New("Only Yes / No conversions have matching values.")
			}
		case "boolean":
			if strings.TrimSpace(c.TrueValue) == "" || strings.TrimSpace(c.FalseValue) == "" || c.TrueValue == c.FalseValue || len(c.TrueValue) > 160 || len(c.FalseValue) > 160 {
				return errors.New("Provide distinct, nonempty Yes and No values.")
			}
		default:
			return errors.New("Choose Text, Decimal, JSON number, or Yes / No for an input.")
		}
	}
	return nil
}
func convertInput(value string, c inputConversion) (any, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	switch c.Kind {
	case "", "text":
		return value, nil
	case "boolean":
		if value == c.TrueValue {
			return true, nil
		}
		if value == c.FalseValue {
			return false, nil
		}
		return nil, errors.New("Does not match either declared Yes or No value.")
	case "decimal":
		// Explicit recipe conversion to a plain decimal string, without rounding.
		// Runtime remains the authority for operator comparability.
		if len(value) > 128 || !inputDecimal.MatchString(value) {
			return nil, errors.New("Use a plain decimal without separators, units, exponents, or leading zeros.")
		}
		return value, nil
	case "number":
		// JSON syntax only: no separators, units, whitespace or leading-zero IDs.
		if len(value) > 128 || !inputNumber.MatchString(value) {
			return nil, errors.New("Use an unambiguous number without separators, units, or leading zeros.")
		}
		n, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsInf(n, 0) || math.IsNaN(n) || math.Abs(n) > 9007199254740991 {
			return nil, errors.New("Number is outside the supported precise range.")
		}
		// Refuse loss through the JSON/JavaScript boundary, including underflow.
		if at := strings.IndexAny(value, "eE"); at >= 0 {
			exp, err := strconv.Atoi(value[at+1:])
			if err != nil || exp < -308 || exp > 308 {
				return nil, errors.New("Number exponent is outside the supported range.")
			}
		}
		canonical := strconv.FormatFloat(n, 'g', -1, 64)
		if !sameDecimal(value, canonical) {
			return nil, errors.New("This number would lose precision. Keep it as text or revise it.")
		}
		return json.Number(value), nil
	}
	return nil, errors.New("Unsupported conversion.")
}
func prepareInputRecord(doc inputPreparation, record preparedInputRecord) (inputPreparedRow, error) {
	if err := requireOrdinaryInputQualification(doc.Recipe.Qualification); err != nil {
		return inputPreparedRow{}, err
	}
	// Preserve the legacy null-as-blank view without rewriting retained cells.
	var err error
	record, err = legacyInputRecord(record)
	if err != nil {
		return inputPreparedRow{}, err
	}
	row := inputPreparedRow{ID: record.ID, Origin: record.Class, Original: record.Values, Prepared: map[string]any{}, Issues: []inputConversionIssue{}}
	q := doc.Recipe.Qualification
	if record.Class == "generated" && record.Research == nil {
		return row, errors.New("The research record is unavailable.")
	}
	if q == nil || (record.Class != "asserted" && record.Class != "generated") {
		return row, errors.New("Choose a supplied record and a saved pack dialect.")
	}
	var source *inputImport
	for i := range doc.Imports {
		if doc.Imports[i].ID == record.SourceID {
			source = &doc.Imports[i]
			break
		}
	}
	if source == nil && record.Class == "asserted" {
		return row, errors.New("The original source is unavailable.")
	}
	values := availableInputValues(record)
	mapping := q.Mapping
	if mapping.Case != nil {
		copied := *mapping.Case
		copied.Facts = append([]inputDialectFact{}, mapping.Case.Facts...)
		mapping.Case = &copied
		for i, f := range copied.Facts {
			column := inputUnescape(strings.TrimPrefix(f.Source, "/values/"))
			var v any
			var err error
			if q.Conversions[f.Target].Kind == "research-value" {
				v, err = typedResearchValue(doc, record, column)
			} else {
				v, err = convertInput(values[column], q.Conversions[f.Target])
			}
			if err != nil {
				row.Issues = append(row.Issues, inputConversionIssue{f.Target, err.Error()})
			} else if v != nil {
				row.Prepared[f.Target] = v
			}
			copied.Facts[i].Source = "/prepared/" + inputEscape(f.Target)
		}
	}
	if mapping.Case != nil {
		for _, f := range mapping.Case.Evidence {
			value := values[inputUnescape(strings.TrimPrefix(f.Source, "/values/"))]
			if strings.TrimSpace(value) != "" && value != "present" && value != "absent" && value != "unknown" {
				row.Issues = append(row.Issues, inputConversionIssue{f.Requirement, "Evidence availability must be present, absent, or unknown."})
			}
		}
	}
	// Generated records get a signed source response only at the server boundary.
	if record.Class == "generated" {
		return row, nil
	}
	input := map[string]any{"source": map[string]any{"mapping": mapping, "case": map[string]any{"values": values, "prepared": row.Prepared, "origin": map[string]any{
		"preparationId": doc.ID, "recordId": record.ID, "sourceId": source.ID, "sourceDigest": source.Digest, "row": record.Row, "importedAt": source.At, "importRecipeVersion": source.RecipeVersion, "recipeVersion": doc.RecipeVersion, "packDigest": q.Digest, "originalValues": record.Values, "conversions": q.Conversions,
	}}, "sources": map[string]any{}}}
	raw, err := json.Marshal(input)
	if err != nil {
		return row, err
	}
	if len(raw) > 2<<20 {
		row.Issues = append(row.Issues, inputConversionIssue{"record", "This record exceeds the run request limit."})
	}
	row.Input = raw
	return row, nil
}
func prepareInputSelection(doc inputPreparation, ids []string) ([]inputPreparedRow, error) {
	if err := requireOrdinaryInputQualification(doc.Recipe.Qualification); err != nil {
		return nil, err
	}
	if len(ids) == 0 || len(ids) > inputRecordLimit {
		return nil, errors.New("Select between 1 and 1,000 records.")
	}
	seen := map[string]bool{}
	byID := map[string]preparedInputRecord{}
	for _, r := range doc.Records {
		byID[r.ID] = r
	}
	rows := make([]inputPreparedRow, 0, len(ids))
	for _, id := range ids {
		r, ok := byID[id]
		if !ok || seen[id] {
			return nil, errors.New("The record selection changed. Review it again.")
		}
		seen[id] = true
		if r.Class == "generated" {
			if err := readyInputReview(doc, r); err != nil {
				return nil, err
			}
		}
		if len(rows) > 0 && rows[0].Origin != r.Class {
			return nil, errors.New("Evaluate AI research and supplied records in separate selections. No records were skipped.")
		}
		row, err := prepareInputRecord(doc, r)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func sameDecimal(a, b string) bool {
	x, ok := new(big.Rat).SetString(a)
	if !ok {
		return false
	}
	y, ok := new(big.Rat).SetString(b)
	return ok && x.Cmp(y) == 0
}

// These closed leaves describe execution rules, never a prepared value. Sidecar
// metadata is compared with them but cannot substitute for document rules.
type inputRetainedConversion struct {
	ContractVersion string  `json:"contractVersion"`
	Column          string  `json:"column"`
	Kind            string  `json:"kind"`
	TrueValue       *string `json:"trueValue,omitempty"`
	FalseValue      *string `json:"falseValue,omitempty"`
}
type inputRetainedAvailability struct {
	ContractVersion string `json:"contractVersion"`
	Column          string `json:"column"`
}

func (c *inputRetainedConversion) UnmarshalJSON(raw []byte) error {
	type plain inputRetainedConversion
	if err := checkJSONMembers(raw, reflect.TypeOf(plain{})); err != nil {
		return err
	}
	var value plain
	if err := decodeInputShape(raw, &value, reflect.TypeOf(value), "trueValue", "falseValue"); err != nil {
		return err
	}
	candidate := inputRetainedConversion(value)
	if _, err := candidate.sidecar(); err != nil {
		return err
	}
	*c = candidate
	return nil
}
func (a *inputRetainedAvailability) UnmarshalJSON(raw []byte) error {
	type plain inputRetainedAvailability
	var value plain
	if err := decodeInputShape(raw, &value, reflect.TypeOf(value)); err != nil {
		return err
	}
	if value.ContractVersion != "1" || !inputRetainedScalar(value.Column, false) {
		return retainedContractFailure("input-malformed")
	}
	*a = inputRetainedAvailability(value)
	return nil
}
func inputRetainedScalar(value string, nonblank bool) bool {
	return value != "" && len(value) <= 160 && utf8.ValidString(value) && (!nonblank || strings.TrimSpace(value) != "")
}
func (c inputRetainedConversion) sidecar() (inputConversion, error) {
	fail := retainedContractFailure("input-malformed")
	result := inputConversion{Kind: c.Kind}
	if c.ContractVersion != "1" || !inputRetainedScalar(c.Column, false) {
		return result, fail
	}
	switch c.Kind {
	case "text", "decimal", "number", "research-value":
		if c.TrueValue != nil || c.FalseValue != nil {
			return result, fail
		}
	case "boolean":
		if c.TrueValue == nil || c.FalseValue == nil || !inputRetainedScalar(*c.TrueValue, true) || !inputRetainedScalar(*c.FalseValue, true) || *c.TrueValue == *c.FalseValue {
			return result, fail
		}
		result.TrueValue, result.FalseValue = *c.TrueValue, *c.FalseValue
	default:
		return result, fail
	}
	return result, nil
}

type retainedInputFactBinding struct {
	Target     string
	Column     string
	Conversion inputConversion
}
type retainedInputEvidenceBinding struct {
	Requirement string
	Column      string
}
type retainedInputRules struct {
	Facts    []retainedInputFactBinding
	Evidence []retainedInputEvidenceBinding
}

// Extract the logical case template only. Runtime pack coverage, original cells,
// asserted/generated materialization and operation admission have separate owners.
func retainedInputMappingRules(q *inputQualification) (retainedInputRules, error) {
	rules := retainedInputRules{Facts: []retainedInputFactBinding{}, Evidence: []retainedInputEvidenceBinding{}}
	bad := retainedContractFailure("input-malformed")
	if q == nil || q.ConversionContract == nil || *q.ConversionContract != inputRetainedContract() {
		return rules, retainedContractFailure("capability-unsupported")
	}
	m := q.Mapping
	if m.Version != 2 || m.Profile == nil || *m.Profile != q.ConversionContract.Profile || m.Case == nil || m.Sources == nil || len(m.Sources) != 0 || m.Case.Facts == nil || m.Case.Evidence == nil || len(m.Case.Facts) > 256 || len(m.Case.Evidence) > 128 {
		return rules, bad
	}
	targets, requirements := map[string]bool{}, map[string]bool{}
	for _, f := range m.Case.Facts {
		if f.Source != "/retained" || !inputPointerValid(f.Target) || !utf8.ValidString(f.Target) || targets[f.Target] || f.Conversion == nil {
			return rules, bad
		}
		for prior := range targets {
			if strings.HasPrefix(prior, f.Target+"/") || strings.HasPrefix(f.Target, prior+"/") {
				return rules, bad
			}
		}
		sidecar, err := f.Conversion.sidecar()
		if err != nil {
			return rules, err
		}
		actual, exists := q.Conversions[f.Target]
		if !exists || actual != sidecar {
			return rules, bad
		}
		targets[f.Target] = true
		rules.Facts = append(rules.Facts, retainedInputFactBinding{Target: f.Target, Column: f.Conversion.Column, Conversion: sidecar})
	}
	if len(q.Conversions) != len(targets) {
		return rules, bad
	}
	for _, e := range m.Case.Evidence {
		if e.Requirement == "" || !utf8.ValidString(e.Requirement) || requirements[e.Requirement] || e.Source != "/retained" || e.Availability == nil || e.Availability.ContractVersion != "1" || !inputRetainedScalar(e.Availability.Column, false) {
			return rules, bad
		}
		requirements[e.Requirement] = true
		rules.Evidence = append(rules.Evidence, retainedInputEvidenceBinding{Requirement: e.Requirement, Column: e.Availability.Column})
	}
	for _, target := range m.Unmapped {
		if !inputPointerValid(target) || !utf8.ValidString(target) || targets[target] {
			return rules, bad
		}
		targets[target] = true
	}
	for _, requirement := range m.UnmappedEvidence {
		if requirement == "" || !utf8.ValidString(requirement) || requirements[requirement] {
			return rules, bad
		}
		requirements[requirement] = true
	}
	return rules, nil
}

// The explicit supplied identity is mandatory; helper availability never selects
// a recipe. The caller retains pack bytes, digest and review/save authority.
func buildRetainedInputMapping(selection retainedConversionContract, rules retainedInputRules, unmapped, unmappedEvidence []string) (inputRecordDialect, map[string]inputConversion, error) {
	profile := selection.Profile
	mapping := inputRecordDialect{Version: 2, Profile: &profile, Sources: []json.RawMessage{}, Case: &inputCaseDialect{Facts: []inputDialectFact{}, Evidence: []inputDialectEvidence{}}, Unmapped: append([]string(nil), unmapped...), UnmappedEvidence: append([]string(nil), unmappedEvidence...)}
	sidecars := map[string]inputConversion{}
	for _, f := range rules.Facts {
		c := &inputRetainedConversion{ContractVersion: "1", Column: f.Column, Kind: f.Conversion.Kind}
		if c.Kind == "boolean" {
			yes, no := f.Conversion.TrueValue, f.Conversion.FalseValue
			c.TrueValue, c.FalseValue = &yes, &no
		} else if f.Conversion.TrueValue != "" || f.Conversion.FalseValue != "" {
			return inputRecordDialect{}, nil, retainedContractFailure("input-malformed")
		}
		mapping.Case.Facts = append(mapping.Case.Facts, inputDialectFact{Target: f.Target, Source: "/retained", Conversion: c})
		sidecars[f.Target] = f.Conversion
	}
	for _, e := range rules.Evidence {
		mapping.Case.Evidence = append(mapping.Case.Evidence, inputDialectEvidence{Requirement: e.Requirement, Source: "/retained", Availability: &inputRetainedAvailability{ContractVersion: "1", Column: e.Column}})
	}
	q := &inputQualification{ConversionContract: &selection, Mapping: mapping, Conversions: sidecars}
	if _, err := retainedInputMappingRules(q); err != nil {
		return inputRecordDialect{}, nil, err
	}
	return mapping, sidecars, nil
}
