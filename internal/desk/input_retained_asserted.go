package desk

// Asserted projection is local retained-data custody, never acquired research or
// permission to prepare. The selected Runner remains the execution authority.
import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"reflect"
	"strconv"
	"strings"
	"unicode"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/jsonscalar"
)

// Only decodeInputPreparation attaches this custody, from private bounded bytes.
// Null cells are absent from the mutable string view so an explicit blank write
// is observable. A null-bearing view is immutable: any cell change refuses
// encoding/production. Review, name and status edits do not change cell custody.
// An absent null key cannot be deleted through the string view; deleting it is
// a no-op. A future cell editor needs an explicit reviewed replacement API.
type inputRawCellCustody struct {
	preparationID, collectionSHA, recordID, sourceID, class string
	position, row                                           int
	raw                                                     json.RawMessage
	strings                                                 map[string]string
	nulls                                                   map[string]bool
}

type inputPreparationCellCustody struct {
	sha            string
	snapshotDigest string
	records        []*inputRawCellCustody
	imports        []inputImport
}

func inputCellFailure() error {
	return errors.New("The original input cells changed or their retained custody is unavailable.")
}

func attachInputCellCustody(doc *inputPreparation, raw []byte) error {
	// Legacy decoding keeps its historical acceptance rules. Exact selected
	// custody is optional on reads: ambiguity must make selected production
	// unavailable, never make an otherwise readable old collection unreadable.
	if len(raw) > inputPreparationLimit || checkJSONMembers(raw, reflect.TypeOf(inputPreparation{})) != nil {
		return nil
	}
	root, err := parseRetainedJSON(raw)
	if err != nil {
		return nil
	}
	original := doc
	projected := *doc
	if doc.Records != nil {
		projected.Records = append([]preparedInputRecord{}, doc.Records...)
	}
	if doc.Reviews != nil {
		projected.Reviews = append([]inputRecordReviewEvent{}, doc.Reviews...)
	}
	doc = &projected
	records := root.member("records")
	if (records == nil || records.kind == 'n') && len(doc.Records) == 0 {
		records = &retainedJSONNode{kind: '['}
	}
	if records == nil || records.kind != '[' || len(records.elements) != len(doc.Records) {
		return nil
	}
	state := &inputPreparationCellCustody{sha: digestOf(raw)}
	// Keep a detached inventory with the same nil/empty shape as the held
	// document. Both are valid no-import forms and remain part of its seal.
	if doc.Imports != nil {
		state.imports = make([]inputImport, len(doc.Imports))
		copy(state.imports, doc.Imports)
	}
	attach := func(record *preparedInputRecord, node *retainedJSONNode, position int) (*inputRawCellCustody, error) {
		values := node.member("values")
		// Preserve the historical nil-map form on legacy reads. It is not a selected frame.
		if values == nil || values.kind == 'n' {
			return nil, nil
		}
		if values.kind != '{' {
			return nil, inputCellFailure()
		}
		var cells map[string]json.RawMessage
		if json.Unmarshal(values.raw, &cells) != nil {
			return nil, inputCellFailure()
		}
		custody := &inputRawCellCustody{preparationID: doc.ID, collectionSHA: state.sha, recordID: record.ID, sourceID: record.SourceID, class: record.Class, position: position, row: record.Row, raw: append(json.RawMessage(nil), values.raw...), strings: map[string]string{}, nulls: map[string]bool{}}
		for name, token := range cells {
			if bytes.Equal(token, []byte("null")) {
				custody.nulls[name] = true
				continue
			}
			var value string
			if json.Unmarshal(token, &value) != nil {
				return nil, inputCellFailure()
			}
			custody.strings[name] = value
		}
		if len(custody.nulls) > 0 {
			record.Values = make(map[string]string, len(custody.strings))
			for name, value := range custody.strings {
				record.Values[name] = value
			}
		}
		record.rawCells = custody
		return custody, nil
	}
	for i := range doc.Records {
		custody, err := attach(&doc.Records[i], records.elements[i], i)
		if err != nil {
			return nil
		}
		state.records = append(state.records, custody)
	}
	// Review snapshots are independent retained records, including when a review
	// copied a record by value. Reattach from each actual snapshot, never today's row.
	if reviews := root.member("reviews"); reviews != nil && reviews.kind == '[' {
		if len(reviews.elements) != len(doc.Reviews) {
			return nil
		}
		for i := range doc.Reviews {
			if _, err := attach(&doc.Reviews[i].Record, reviews.elements[i].member("record"), -1); err != nil {
				return nil
			}
		}
	}
	doc.cellCustody = state
	// Bind the complete decoded view for selected execution. This is a local
	// mutation seal, not the identity of the original stored bytes (state.sha).
	snapshot, err := json.Marshal(doc)
	if err != nil {
		return nil
	}
	state.snapshotDigest = digestOf(snapshot)
	*original = *doc
	return nil
}

func (record preparedInputRecord) checkInputCellCustody() error {
	c := record.rawCells
	if c == nil {
		return nil
	}
	if c.recordID != record.ID || c.sourceID != record.SourceID || c.row != record.Row || c.class != record.Class || !reflect.DeepEqual(c.strings, record.Values) {
		return inputCellFailure()
	}
	return nil
}

func (record preparedInputRecord) MarshalJSON() ([]byte, error) {
	type plain preparedInputRecord
	if record.rawCells == nil || len(record.rawCells.nulls) == 0 {
		return json.Marshal(plain(record))
	}
	if err := record.checkInputCellCustody(); err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		*plain
		Values json.RawMessage `json:"values"`
	}{(*plain)(&record), record.rawCells.raw})
}
func (doc inputPreparation) MarshalJSON() ([]byte, error) {
	type plain inputPreparation
	if doc.Recipe.Qualification != nil && doc.Recipe.Qualification.ConversionContract != nil && len(doc.Records) > 0 && doc.cellCustody == nil {
		return nil, inputCellFailure()
	}
	for i, record := range doc.Records {
		if c := record.rawCells; c != nil && len(c.nulls) > 0 && (c.preparationID != doc.ID || c.position != i) {
			return nil, inputCellFailure()
		}
	}
	for _, event := range doc.Reviews {
		if c := event.Record.rawCells; c != nil && len(c.nulls) > 0 && c.preparationID != doc.ID {
			return nil, inputCellFailure()
		}
	}
	return json.Marshal(plain(doc))
}

// Legacy display/inventory intentionally retains its old null-as-blank view.
// This detached projection cannot be marshaled back as raw-cell custody.
func legacyInputValues(record preparedInputRecord) map[string]string {
	c := record.rawCells
	if c == nil || len(c.nulls) == 0 {
		return record.Values
	}
	values := make(map[string]string, len(record.Values)+len(c.nulls))
	for key, value := range record.Values {
		values[key] = value
	}
	for key := range c.nulls {
		if _, present := values[key]; !present {
			values[key] = ""
		}
	}
	return values
}
func legacyInputRecord(record preparedInputRecord) (preparedInputRecord, error) {
	if record.rawCells != nil && len(record.rawCells.nulls) > 0 {
		if err := record.checkInputCellCustody(); err != nil {
			return record, err
		}
		record.Values = legacyInputValues(record)
		record.rawCells = nil
	}
	return record, nil
}

type retainedAssertedDiagnostic struct {
	Target        string `json:"target"`
	Column        string `json:"column"`
	Kind          string `json:"kind"`
	CandidateJSON string `json:"candidateJson,omitempty"`
	Omitted       string `json:"omitted,omitempty"`
	Code          string `json:"code,omitempty"`
	Message       string `json:"message,omitempty"`
}
type retainedAssertedCase struct {
	RecordID    string
	FrameJSON   json.RawMessage
	CaseJSON    string
	Diagnostics []retainedAssertedDiagnostic
}

type retainedAssertedFrame struct {
	ContractVersion string          `json:"contractVersion"`
	Origin          string          `json:"origin"`
	Values          json.RawMessage `json:"values"`
	Research        any             `json:"research"`
}

// projectRetainedAssertedInput requires the held repository snapshot and a
// complete independently negotiated three-role admission. It performs no I/O.
// B3 owns complete source/request/reply accounting and actual selected dispatch.
func projectRetainedAssertedInput(saved inputPreparationReply, recordID string, admission *retainedAdmission) (retainedAssertedCase, error) {
	var result retainedAssertedCase
	required := []string{"document-validation", "preview", "preparation"}
	if admission == nil || admission.generated || retainedValidateSelector(admission.selection, required...) != nil || retainedValidateAdmission(admission) != nil {
		return result, retainedContractFailure("capability-unsupported")
	}
	for _, role := range required {
		if admission.limits[role] == nil {
			return result, retainedContractFailure("capability-unsupported")
		}
	}
	doc := saved.Document
	if doc.cellCustody == nil || doc.cellCustody.sha != saved.SHA256 || !reflect.DeepEqual(doc.Imports, doc.cellCustody.imports) {
		return result, inputCellFailure()
	}
	q := doc.Recipe.Qualification
	rules, err := retainedInputMappingRules(q)
	if err != nil {
		return result, err
	}
	if q.ConversionContract.Contract != admission.selection.Contract || q.ConversionContract.Spec != admission.selection.Spec || q.ConversionContract.Profile != admission.selection.Profile {
		return result, retainedContractFailure("capability-unsupported")
	}
	mappingJSON, err := json.Marshal(q.Mapping)
	if err != nil {
		return result, err
	}
	if err = checkRetainedArtifact(admission, "documentBytes", mappingJSON); err != nil {
		return result, err
	}
	position := -1
	for i, record := range doc.Records {
		if record.ID == recordID {
			if position != -1 {
				return result, inputCellFailure()
			}
			position = i
		}
	}
	if position < 0 || position >= len(doc.cellCustody.records) {
		return result, inputCellFailure()
	}
	record := doc.Records[position]
	c := doc.cellCustody.records[position]
	if c == nil || record.rawCells != c || c.collectionSHA != saved.SHA256 || c.preparationID != doc.ID || c.position != position || record.Class != "asserted" || record.Research != nil || record.checkInputCellCustody() != nil {
		return result, inputCellFailure()
	}
	var source *inputImport
	for i := range doc.Imports {
		if doc.Imports[i].ID == record.SourceID {
			if source != nil {
				return result, inputCellFailure()
			}
			source = &doc.Imports[i]
		}
	}
	if source == nil || source.Digest != retainedDigest([]byte(source.Content)) || source.ID != "source_"+digestOf([]byte(source.Kind+"\n"+source.Content)) || (source.Kind != "csv" && source.Kind != "urls") || record.Row < 1 || !retainedSafeVersion(source.RecipeVersion) || !retainedSafeVersion(doc.RecipeVersion) {
		return result, inputCellFailure()
	}
	values, err := retainedAssertedCells(c.raw)
	if err != nil {
		return result, err
	}
	frame, err := json.Marshal(retainedAssertedFrame{ContractVersion: "1", Origin: "asserted", Values: c.raw, Research: nil})
	if err != nil {
		return result, err
	}
	if err = checkRetainedArtifact(admission, "frameBytes", frame); err != nil {
		return result, err
	}
	prepared := map[string]json.RawMessage{}
	diagnostics := make([]retainedAssertedDiagnostic, 0, len(rules.Facts)+len(rules.Evidence))
	for _, rule := range rules.Facts {
		diagnostic := retainedAssertedConversion(values, rule.Target, rule.Column, rule.Conversion, false)
		diagnostics = append(diagnostics, diagnostic)
		if diagnostic.CandidateJSON != "" {
			prepared[rule.Target] = json.RawMessage(diagnostic.CandidateJSON)
		}
	}
	for _, rule := range rules.Evidence {
		diagnostics = append(diagnostics, retainedAssertedConversion(values, rule.Requirement, rule.Column, inputConversion{}, true))
	}
	// All legacy case siblings survive. The selected executable rules still read
	// /retained; prepared is inspectable provenance, never conversion authority.
	caseValue := map[string]any{"values": availableInputValues(record), "prepared": prepared, "origin": map[string]any{
		"preparationId": doc.ID, "recordId": record.ID, "sourceId": source.ID, "sourceDigest": source.Digest, "row": record.Row, "importedAt": source.At, "importRecipeVersion": source.RecipeVersion, "recipeVersion": doc.RecipeVersion, "packDigest": q.Digest, "originalValues": json.RawMessage(c.raw), "conversions": q.Conversions,
	}, "retained": json.RawMessage(frame)}
	caseJSON, err := json.Marshal(caseValue)
	if err != nil {
		return result, err
	}
	node, err := parseRetainedJSON(caseJSON)
	if err != nil {
		return result, err
	}
	if node.depth > 64 {
		return result, retainedContractFailure("input-over-bound")
	}
	if err = checkRetainedArtifact(admission, "combinedBytes", caseJSON); err != nil {
		return result, err
	}
	return retainedAssertedCase{RecordID: record.ID, FrameJSON: frame, CaseJSON: string(caseJSON), Diagnostics: diagnostics}, nil
}

func retainedAssertedCells(raw []byte) (map[string]json.RawMessage, error) {
	node, err := parseRetainedJSON(raw)
	if err != nil || node.kind != '{' || jsonscalar.ValidateEscapes(raw) != nil {
		return nil, retainedContractFailure("input-malformed")
	}
	if len(node.members) > retainedFieldCount {
		return nil, retainedContractFailure("input-over-bound")
	}
	var values map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil {
		return nil, retainedContractFailure("input-malformed")
	}
	for name, token := range values {
		if !retainedColumnName(name) {
			return nil, retainedContractFailure("input-malformed")
		}
		if bytes.Equal(token, []byte("null")) {
			continue
		}
		value, err := node.member(name).scalarString()
		if err != nil {
			return nil, retainedContractFailure("input-malformed")
		}
		if len(value) > 16000 {
			return nil, retainedContractFailure("input-over-bound")
		}
	}
	return values, nil
}

var retainedAssertedMessages = map[string]string{
	"decimal-encoding":      "Use a plain decimal without separators, units, exponents, or leading zeros.",
	"number-encoding":       "Use an unambiguous number without separators, units, or leading zeros.",
	"number-range":          "Number is outside the supported precise range.",
	"number-exponent":       "Number exponent is outside the supported range.",
	"number-precision":      "This number would lose precision. Keep it as text or revise it.",
	"boolean-spelling":      "Does not match either declared Yes or No value.",
	"research-context":      "Research value requires a typed research field.",
	"evidence-availability": "Evidence availability must be present, absent, or unknown.",
}

func retainedAssertedConversion(values map[string]json.RawMessage, target, column string, conversion inputConversion, evidence bool) retainedAssertedDiagnostic {
	d := retainedAssertedDiagnostic{Target: target, Column: column, Kind: conversion.Kind}
	issue := func(code string) retainedAssertedDiagnostic {
		d.Code = code
		d.Message = retainedAssertedMessages[code]
		return d
	}
	if !evidence && conversion.Kind == "research-value" {
		return issue("research-context")
	}
	token, present := values[column]
	if !present {
		d.Omitted = "missing"
		return d
	}
	if bytes.Equal(token, []byte("null")) {
		d.Omitted = "legacy-null"
		return d
	}
	var value string
	_ = json.Unmarshal(token, &value) // validated frame is mandatory at the caller
	if strings.TrimFunc(value, unicode.IsSpace) == "" {
		d.Omitted = "blank"
		return d
	}
	if evidence {
		d.Kind = "availability"
		if value != "present" && value != "absent" && value != "unknown" {
			return issue("evidence-availability")
		}
		raw, _ := json.Marshal(value)
		d.CandidateJSON = string(raw)
		return d
	}
	switch conversion.Kind {
	case "text":
		raw, _ := json.Marshal(value)
		d.CandidateJSON = string(raw)
	case "decimal":
		if len(value) > 128 || !inputDecimal.MatchString(value) {
			return issue("decimal-encoding")
		}
		raw, _ := json.Marshal(value)
		d.CandidateJSON = string(raw)
	case "boolean":
		if value == conversion.TrueValue {
			d.CandidateJSON = "true"
		} else if value == conversion.FalseValue {
			d.CandidateJSON = "false"
		} else {
			return issue("boolean-spelling")
		}
	case "number":
		if len(value) > 128 || !inputNumber.MatchString(value) {
			return issue("number-encoding")
		}
		n, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || math.Abs(n) > 9007199254740991 {
			return issue("number-range")
		}
		if at := strings.IndexAny(value, "eE"); at >= 0 {
			exponent, ok := new(big.Int).SetString(value[at+1:], 10)
			if !ok || new(big.Int).Abs(exponent).Cmp(big.NewInt(308)) > 0 {
				return issue("number-exponent")
			}
		}
		// Go's shortest round-trip formatter chooses the shortest coefficient,
		// nearest decimal and even final digit on a tie. Compare exact rationals;
		// only the guard uses binary64. The candidate retains the original token.
		if !sameDecimal(value, strconv.FormatFloat(n, 'g', -1, 64)) {
			return issue("number-precision")
		}
		d.CandidateJSON = value
	}
	return d
}
