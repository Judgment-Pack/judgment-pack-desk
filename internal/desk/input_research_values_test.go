package desk

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPortableResearchValueEncodings(t *testing.T) {
	for _, tc := range []struct {
		kind, raw string
		valid     bool
	}{
		{"integer", "0", true}, {"integer", "-0", true}, {"integer", "9007199254740991", true}, {"integer", "9007199254740992", false}, {"integer", "1.0", false}, {"integer", "1e2", false}, {"integer", `"1"`, false},
		{"decimal", `"0.00"`, true}, {"decimal", `"-50.250"`, true}, {"decimal", `"1e2"`, false}, {"decimal", "1.5", false}, {"decimal", `"01"`, false}, {"decimal", `"+1"`, false},
		{"boolean", "false", true}, {"boolean", `"false"`, false}, {"text", `""`, true}, {"text", "null", false},
		{"text-list", `[]`, true}, {"text-list", `["a",""]`, true}, {"text-list", `["a",1]`, false}, {"text-list", `"a, b"`, false},
		{"range", `{"min":0,"max":50}`, true}, {"range", `{"min":"0.00","max":"50.5"}`, true}, {"range", `{"min":50,"max":0}`, false}, {"range", `{"min":0,"max":"50"}`, false}, {"range", `{"min":1.0,"max":2.0}`, false}, {"range", `{"min":0}`, false}, {"range", `{"min":0,"max":1,"extra":true}`, false}, {"range", `{"min":0,"min":1,"max":2}`, false},
		{"string", `"legacy"`, true}, {"number", "1.5", true}, {"number", "1e2", true}, {"number", `"1.5"`, false}, {"unknown", "0", false},
	} {
		if got := validResearchValue(json.RawMessage(tc.raw), tc.kind); got != tc.valid {
			t.Errorf("%s %s: got %v", tc.kind, tc.raw, got)
		}
	}
	for _, kind := range researchFieldTypes {
		if validResearchValue(json.RawMessage(`null`), kind) {
			t.Error("null became a typed value", kind)
		}
	}
}
func TestPortableResearchValueBounds(t *testing.T) {
	encode := func(v any) json.RawMessage { raw, _ := json.Marshal(v); return raw }
	if validResearchValue(encode(strings.Repeat("9", 129)), "decimal") {
		t.Fatal("oversized decimal")
	}
	if validResearchValue(encode(strings.Repeat("é", 4097)), "text") {
		t.Fatal("text measured in characters rather than UTF-8 bytes")
	}
	list := make([]string, 65)
	if validResearchValue(encode(list), "text-list") {
		t.Fatal("oversized list")
	}
	if validResearchValue(encode([]string{strings.Repeat("x", 1025)}), "text-list") {
		t.Fatal("oversized item")
	}
	if !validResearchValue(encode([]string{"", strings.Repeat("x", 1024)}), "text-list") {
		t.Fatal("valid boundary refused")
	}
}
func TestPortableResearchValuesSurviveRecordPublication(t *testing.T) {
	_, run := researchRunFixture(t)
	run.Recipe.Target = "records"
	run.Recipe.Fields = []inputResearchField{{Name: "count", Type: "integer", Description: "Count"}, {Name: "amount", Type: "decimal", Description: "Amount"}, {Name: "tags", Type: "text-list", Description: "Tags"}, {Name: "size", Type: "range", Description: "Size"}, {Name: "active", Type: "boolean", Description: "Active"}}
	if err := validateInputResearch(&run.Recipe); err != nil {
		t.Fatal(err)
	}
	entity := inputResearchEntity{Kind: "record", Name: "Example", URL: "https://example.com/", Fields: map[string]inputResearchValue{}}
	for name, raw := range map[string]string{"count": "0", "amount": `"0.00"`, "tags": `[]`, "size": `{"min":0,"max":49}`, "active": "false"} {
		entity.Fields[name] = inputResearchValue{Status: "interpretation", Value: json.RawMessage(raw)}
	}
	if err := validateResearchEntity(&entity, run.Recipe, nil, map[string]bool{entity.URL: true}, nil); err != nil {
		t.Fatal(err)
	}
	row := researchPreparedRecord(run, entity)
	for name, field := range entity.Fields {
		if string(row.Research.Entity.Fields[name].Value) != string(field.Value) {
			t.Fatal("typed value changed", name)
		}
	}
	if row.Values["amount"] != "0.00" || row.Values["active"] != "false" || row.Values["tags"] != "[]" {
		t.Fatal("publication lost zero, false, empty list or decimal encoding", row.Values)
	}
	entity.Fields["size"] = inputResearchValue{Status: "interpretation", Value: json.RawMessage(`"0 to 49"`)}
	if validateResearchEntity(&entity, run.Recipe, nil, map[string]bool{entity.URL: true}, nil) == nil {
		t.Fatal("narrative range accepted")
	}
}

func TestPortableResearchUnicode(t *testing.T) {
	for _, raw := range []string{`"\ud800"`, `"\udc00"`, `"\ud800x"`, "\"\xff\""} {
		if validResearchValue(json.RawMessage(raw), "text") || validResearchValue(json.RawMessage("["+raw+"]"), "text-list") {
			t.Errorf("malformed Unicode accepted: %q", raw)
		}
		if !validResearchValue(json.RawMessage(raw), "string") {
			t.Errorf("legacy replay changed: %q", raw)
		}
	}
	for _, raw := range []string{`"\ud83d\ude00"`, `"\\ud800"`, `"�"`, `"é"`} {
		if !validResearchValue(json.RawMessage(raw), "text") || !validResearchValue(json.RawMessage("["+raw+"]"), "text-list") {
			t.Errorf("valid Unicode refused: %q", raw)
		}
	}
}
