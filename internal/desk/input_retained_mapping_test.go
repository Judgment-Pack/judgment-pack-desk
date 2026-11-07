package desk

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func retainedMappingFixture(t *testing.T) *inputQualification {
	t.Helper()
	raw, err := os.ReadFile("../../web/src/dialects/testdata/retained-recipe-shapes.json")
	if err != nil {
		t.Fatal(err)
	}
	var q inputQualification
	if err = json.Unmarshal(raw, &q); err != nil {
		t.Fatal(err)
	}
	selection := inputRetainedContract()
	q.ConversionContract = &selection
	return &q
}
func TestRetainedMappingSharedShapes(t *testing.T) {
	q := retainedMappingFixture(t)
	rules, err := retainedInputMappingRules(q)
	if err != nil {
		t.Fatal(err)
	}
	kinds := []string{"text", "decimal", "number", "boolean", "research-value"}
	for i, kind := range kinds {
		if rules.Facts[i].Conversion.Kind != kind {
			t.Fatal(rules)
		}
	}
	if len(rules.Evidence) != 1 || rules.Evidence[0].Column != "License" {
		t.Fatal(rules)
	}
	mapping, sidecars, err := buildRetainedInputMapping(*q.ConversionContract, rules, q.Mapping.Unmapped, q.Mapping.UnmappedEvidence)
	if err != nil || !reflect.DeepEqual(mapping, q.Mapping) || !reflect.DeepEqual(sidecars, q.Conversions) {
		t.Fatal(mapping, sidecars, err)
	}
	raw, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	var reread inputQualification
	if err = json.Unmarshal(raw, &reread); err != nil {
		t.Fatal(err)
	}
	if _, err = retainedInputMappingRules(&reread); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"roles"`) {
		t.Fatal("roles persisted")
	}
	// Saved logical selection does not admit the ordinary execution owner.
	if requireOrdinaryInputQualification(q) == nil {
		t.Fatal("selected template entered the ordinary owner")
	}
}
func TestRetainedMappingLegacyOmission(t *testing.T) {
	const raw = `{"version":2,"case":{"facts":[{"target":"/country","source":"/values/Country"}],"evidence":[{"requirement":"license","source":"/values/License"}]},"sources":[]}`
	var mapping inputRecordDialect
	if err := json.Unmarshal([]byte(raw), &mapping); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(mapping)
	if err != nil || string(encoded) != raw {
		t.Fatal(string(encoded), err)
	}
	q := qualificationFixture()
	before, _ := json.Marshal(q)
	if validateInputQualification(q) != nil {
		t.Fatal("legacy fixture refused")
	}
	after, _ := json.Marshal(q)
	if string(before) != string(after) {
		t.Fatal("legacy qualification changed")
	}
	for _, member := range []string{"conversionContract", "profile", "availability", `"conversion":`} {
		if strings.Contains(string(after), member) {
			t.Fatal("new member inserted", member)
		}
	}
	// Stored legacy controls retain their historical encoding/json case behavior;
	// existing request exact-control checking remains independently enforced.
	var old inputQualification
	if err := decodeDataJSON([]byte(`{"Kind":"pack","mapping":{"version":2,"sources":[]}}`), &old); err != nil || old.Kind != "pack" {
		t.Fatal(old, err)
	}
}
func TestRetainedMappingClosedSelectedJSON(t *testing.T) {
	q := retainedMappingFixture(t)
	encoded, _ := json.Marshal(q)
	raw := string(encoded)
	changes := [][2]string{
		{`"conversionContract":` + string(mustRetainedContractJSON(t)), `"conversionContract":null`},
		{`"conversionContract":{`, `"ConversionContract":{`},
		{`"conversionContract":{`, `"conversionContract":{"roles":[],`},
		{`"contract":"retained-column/1"`, `"contract":"retained-column/1","contract":"retained-column/1"`},
		{`"profile":"base"`, `"profile":"extended"`},
		{`"schemaSha256":`, `"SchemaSha256":`},
		{`"contractVersion":"1"`, `"contractVersion":1`},
		{`"contractVersion":"1"`, `"contractVersion":"2"`},
		{`"column":"Name"`, `"Column":"Name"`},
		{`"column":"Name"`, `"column":"Name","column":"Other"`},
		{`"column":"Name"`, `"column":null`},
		{`"kind":"text"`, `"kind":"unsupported"`},
		{`"kind":"text"`, `"kind":["text"]`},
		{`"mapping":`, `"unknown":true,"mapping":`},
		{`"kind":"text"`, `"kind":"text","trueValue":""`},
		{`"trueValue":"YES"`, `"trueValue":null`},
		{`"trueValue":"YES"`, `"trueValue":"NO"`},
		{`,"falseValue":"NO"`, ``},
		{`"trueValue":"YES"`, `"trueValue":"\u0085"`},
		{`"contractVersion":"1","column":"Name",`, `"column":"Name",`},
		{`"contractVersion":"1","column":"License"`, `"column":"License"`},
		{`"column":"License"`, `"column":""`},
		{`"contract":"retained-column/1",`, ``},
		{`"spec":{`, `"spec":{"roles":[],`},
		{`"conversionContract":{`, `"conversionContract":{"unknown":true,`},
		{`"kind":"text"`, `"Kind":"text"`},
		{`"availability":{"contractVersion":"1","column":"License"}`, `"availability":null`},
		{`"availability":{"contractVersion":"1","column":"License"}`, `"availability":{"contractVersion":"1","column":"License","other":true}`},
		{`"conversion":{"contractVersion":"1","column":"Name","kind":"text"}`, `"conversion":null`},
		{`"profile":"base","case"`, `"profile":null,"case"`},
		{`"sources":[]`, `"sources":null`},
		{`"/text":{"kind":"text"}`, `"/text":{"kind":"text","trueValue":null}`},
	}
	for _, change := range changes {
		t.Run(change[1], func(t *testing.T) {
			mutated := strings.Replace(raw, change[0], change[1], 1)
			if mutated == raw {
				t.Fatal("mutation did not match")
			}
			var got inputQualification
			err := json.Unmarshal([]byte(mutated), &got)
			if err == nil {
				_, err = retainedInputMappingRules(&got)
			}
			if err == nil {
				t.Fatal("selected mutation accepted", mutated)
			}
		})
	}
}
func TestRetainedMappingRequiresCompleteEqualSidecars(t *testing.T) {
	for _, scenario := range []string{"missing", "extra", "kind", "spelling", "no selector", "no profile", "ordinary source", "no conversion", "no availability", "duplicate", "duplicate evidence", "overlap"} {
		t.Run(scenario, func(t *testing.T) {
			q := retainedMappingFixture(t)
			switch scenario {
			case "missing":
				delete(q.Conversions, "/text")
			case "extra":
				q.Conversions["/extra"] = inputConversion{Kind: "text"}
			case "kind":
				q.Conversions["/text"] = inputConversion{Kind: "decimal"}
			case "spelling":
				q.Conversions["/boolean"] = inputConversion{Kind: "boolean", TrueValue: "yes", FalseValue: "NO"}
			case "no selector":
				q.ConversionContract = nil
			case "no profile":
				q.Mapping.Profile = nil
			case "ordinary source":
				q.Mapping.Case.Facts[0].Source = "/values/Name"
			case "no conversion":
				q.Mapping.Case.Facts[0].Conversion = nil
			case "no availability":
				q.Mapping.Case.Evidence[0].Availability = nil
			case "duplicate":
				q.Mapping.Case.Facts = append(q.Mapping.Case.Facts, q.Mapping.Case.Facts[0])
			case "duplicate evidence":
				q.Mapping.Case.Evidence = append(q.Mapping.Case.Evidence, q.Mapping.Case.Evidence[0])
			case "overlap":
				q.Mapping.Case.Facts[1].Target = "/text/child"
			}
			if _, err := retainedInputMappingRules(q); err == nil {
				t.Fatal("incomplete rules accepted")
			}
		})
	}
}
func TestRetainedMappingScalarBounds(t *testing.T) {
	for _, column := range []string{strings.Repeat("é", 80), " ", "__proto__", "constructor", "😀"} {
		q := retainedMappingFixture(t)
		q.Mapping.Case.Facts[0].Conversion.Column = column
		if _, err := retainedInputMappingRules(q); err != nil {
			t.Fatal(column, err)
		}
	}
	for _, column := range []string{"", strings.Repeat("é", 81), string([]byte{0xff})} {
		q := retainedMappingFixture(t)
		q.Mapping.Case.Facts[0].Conversion.Column = column
		if _, err := retainedInputMappingRules(q); err == nil {
			t.Fatal("bad scalar column accepted")
		}
	}
}

func mustRetainedContractJSON(t *testing.T) []byte {
	t.Helper()
	raw, err := json.Marshal(inputRetainedContract())
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
