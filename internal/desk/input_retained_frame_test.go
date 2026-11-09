package desk

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestRetainedD1ExactSnapshotProjection(t *testing.T) {
	doc, record, run, snapshot := retainedD1Fixture(t)
	snapshot = bytes.Replace(snapshot, []byte(`["a","b"]`), []byte(`[ "a", "b" ]`), 1)
	history, err := resolveRetainedResearchHistory(doc, record, run)
	if err != nil {
		t.Fatal(err)
	}
	carrier, err := projectRetainedReviewedInput(snapshot, history)
	if err != nil {
		t.Fatal(err)
	}
	if carrier.OriginalSnapshotJSON != string(snapshot) || carrier.OriginalSnapshotSHA256 != retainedDigest(snapshot) {
		t.Fatal("original snapshot bytes changed")
	}
	research := carrier.Retained.Research
	if len(research.Declaration.Fields) != 11 || len(research.Fields) != 11 || carrier.Retained.Values["stale_value"] != "unavailable" {
		t.Fatal("complete original declaration/field/display inventory lost")
	}
	if value, present := carrier.Retained.Values["text_value"]; !present || value != "" {
		t.Fatal("blank display value filtered")
	}
	for name, want := range map[string]string{"text_value": `""`, "integer_value": `-0`, "decimal_value": `"0.000000000000000001"`, "boolean_value": `false`, "text_list_value": `[ "a", "b" ]`, "range_value": `{"min":"0.00","max":"1.01"}`, "legacy_string_value": `"CA"`, "legacy_number_value": `1.2300e+2`, "null_value": `null`, "stale_value": `"unavailable"`} {
		field := research.Fields[name]
		if field.ValueJSON == nil || *field.ValueJSON != want {
			t.Fatalf("%s lost raw token: %#v want %s", name, field, want)
		}
	}
	if research.Fields["missing_value"].ValueJSON != nil || research.Fields["null_value"].Status != "inaccessible" {
		t.Fatal("absent and null/unavailable conflated")
	}
	if research.RecordRecipeRevision != "sha256:ef9d26d79a6f5f1a147bd95bc610f13406581d98f48a971070513b20b595f6ae" || research.RecordRecipeRevision != research.Declaration.RecipeRevision || len(research.RecordRecipeRevision) != 71 {
		t.Fatal("revision identity domains disagree")
	}
	raw, err := json.Marshal(carrier)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := checkRetainedReviewedCarrier(raw, history)
	if err != nil || restored.OriginalSnapshotJSON != string(snapshot) {
		t.Fatalf("carrier restoration: %v", err)
	}
	// The D0a actual producer already demonstrated byte-identical snapshots for
	// this unselected history mutation. D1 now refuses that exact inconsistency.
	changed := *doc.Recipes[0].Recipe.Research
	changed.Fields = append([]inputResearchField(nil), changed.Fields...)
	changed.Fields[10].Description = "Changed unselected original metadata"
	doc.Recipes[0].Recipe.Research = &changed
	if _, err := resolveRetainedResearchHistory(doc, record, run); err == nil {
		t.Fatal("D0a unselected metadata gap remains")
	}
}

func TestRetainedD1ProjectionRefusals(t *testing.T) {
	doc, record, run, snapshot := retainedD1Fixture(t)
	history, err := resolveRetainedResearchHistory(doc, record, run)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][]byte{
		"opaque-duplicate":           bytes.Replace(snapshot, []byte(`"number":1.2300e+2`), []byte(`"number":1,"\u006eumber":2`), 1),
		"field-duplicate":            bytes.Replace(snapshot, []byte(`"value":-0`), []byte(`"value":-0,"\u0076alue":0`), 1),
		"wrong-version":              bytes.Replace(snapshot, []byte(`"version":1`), []byte(`"version":1.0`), 1),
		"run-mismatch":               bytes.Replace(snapshot, []byte(`"runId":"research_`+strings.Repeat("a", 32)), []byte(`"runId":"research_`+strings.Repeat("b", 32)), 1),
		"revision-exponent":          bytes.Replace(snapshot, []byte(`"recipeVersion":1`), []byte(`"recipeVersion":1e0`), 1),
		"revision-zero":              bytes.Replace(snapshot, []byte(`"recipeVersion":1`), []byte(`"recipeVersion":0`), 1),
		"revision-unsafe":            bytes.Replace(snapshot, []byte(`"recipeVersion":1`), []byte(`"recipeVersion":9007199254740992`), 1),
		"null-values":                bytes.Replace(snapshot, []byte(`"integer_value":"-0"`), []byte(`"integer_value":null`), 1),
		"unavailable-inventory":      bytes.Replace(snapshot, []byte(`"fields":[`), []byte(`"fields":null,"unused":[`), 1),
		"status":                     bytes.Replace(snapshot, []byte(`"status":"stale"`), []byte(`"status":"unknown"`), 1),
		"unselected-metadata":        bytes.Replace(snapshot, []byte(`Original stale value remains in the unfiltered record`), []byte(`Changed unselected original metadata`), 1),
		"unknown-declaration-member": bytes.Replace(snapshot, []byte(`"description":"Original empty text"`), []byte(`"opaque":1,"description":"Original empty text"`), 1),
		"nonobject":                  []byte(`[]`),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if bytes.Equal(raw, snapshot) {
				t.Fatal("mutation did not apply")
			}
			if _, err := projectRetainedReviewedInput(raw, history); err == nil {
				t.Fatal("malformed or inconsistent projection accepted")
			}
		})
	}
}

func TestRetainedD1CarrierClosedAndExact(t *testing.T) {
	doc, record, run, snapshot := retainedD1Fixture(t)
	history, err := resolveRetainedResearchHistory(doc, record, run)
	if err != nil {
		t.Fatal(err)
	}
	carrier, err := projectRetainedReviewedInput(snapshot, history)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(carrier)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutated := range map[string][]byte{
		"extra":                   append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"extra":true}`)...),
		"duplicate":               bytes.Replace(raw, []byte(`"contractVersion":"1"`), []byte(`"contractVersion":"1","\u0063ontractVersion":"1"`), 1),
		"wrong-case-frame-member": bytes.Replace(raw, []byte(`"origin":"generated"`), []byte(`"Origin":"generated"`), 1),
		"wrong-case-field-member": bytes.Replace(raw, []byte(`"status":"interpretation"`), []byte(`"Status":"interpretation"`), 1),
		"wrong-hash":              bytes.Replace(raw, []byte(carrier.OriginalSnapshotSHA256), []byte("sha256:"+strings.Repeat("0", 64)), 1),
		"frame-extra":             bytes.Replace(raw, []byte(`"origin":"generated"`), []byte(`"origin":"generated","extra":true`), 1),
		"numeric-spelling":        bytes.Replace(raw, []byte(`"valueJson":"-0"`), []byte(`"valueJson":"0"`), 1),
		"absent-to-null":          bytes.Replace(raw, []byte(`"status":"missing"`), []byte(`"status":"missing","valueJson":"null"`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			if bytes.Equal(mutated, raw) {
				t.Fatal("mutation did not apply")
			}
			if _, err := checkRetainedReviewedCarrier(mutated, history); err == nil {
				t.Fatal("carrier mutation accepted")
			}
		})
	}
}

func TestRetainedD1RawKeysAndCompaction(t *testing.T) {
	for _, raw := range []string{`{"x":1,"\u0078":2}`, `{"😀":1,"\ud83d\ude00":2}`, `{"nested":[{"a":0,"a":1}]}`, `{"\ud800":0,"\ud800":1}`} {
		if _, err := parseRetainedJSON([]byte(raw)); err == nil {
			t.Fatalf("duplicate accepted: %s", raw)
		}
	}
	for _, raw := range []string{`{"A":1,"a":2}`, `{"é":1,"é":2}`, `{"\ud800":0,"�":1}`, `{"\ud800":0,"\ud801":1}`} {
		if _, err := parseRetainedJSON([]byte(raw)); err != nil {
			t.Fatalf("distinct exact keys refused: %s: %v", raw, err)
		}
	}
	token := " { \n\t\"name\" : \"x y\\t\\u0061\", \"opaque\" : [ -0,1.2300e+2 ] } "
	if got := retainedCompactToken([]byte(token)); got != `{"name":"x y\t\u0061","opaque":[-0,1.2300e+2]}` {
		t.Fatalf("compaction altered strings/tokens: %s", got)
	}
}

func TestRetainedD1IndependentBoundsAndRevision(t *testing.T) {
	doc, record, run, snapshot := retainedD1Fixture(t)
	history, err := resolveRetainedResearchHistory(doc, record, run)
	if err != nil {
		t.Fatal(err)
	}
	// A legal opaque sibling reaches the original snapshot owner's exact guard.
	base := append(append([]byte{}, snapshot[:len(snapshot)-1]...), []byte(`,"padding":""}`)...)
	padding := retainedSnapshotBytes - len(base)
	atLimit := bytes.Replace(base, []byte(`"padding":""`), []byte(`"padding":"`+strings.Repeat("x", padding)+`"`), 1)
	if len(atLimit) != retainedSnapshotBytes {
		t.Fatal("incorrect handwritten boundary construction")
	}
	if _, err := projectRetainedReviewedInput(atLimit, history); err != nil {
		t.Fatal("exact original snapshot bound refused:", err)
	}
	over := bytes.Replace(atLimit, []byte(`"padding":"`), []byte(`"padding":"x`), 1)
	if _, err := projectRetainedReviewedInput(over, history); err == nil {
		t.Fatal("snapshot one-over accepted")
	}
	for _, version := range []int{1, 9007199254740991} {
		revision, err := retainedRevision(run.ID, version)
		if err != nil || len(revision) != 71 {
			t.Fatalf("safe revision endpoint %d: %q %v", version, revision, err)
		}
	}
	for _, version := range []int{0, -1, 9007199254740992} {
		if _, err := retainedRevision(run.ID, version); err == nil {
			t.Fatal("unsafe revision accepted")
		}
	}
}

func TestRetainedD1ExistingFiveConversions(t *testing.T) {
	doc, record, run, _ := retainedD1Fixture(t)
	var entity inputResearchEntity
	action, err := retainedD1ModelEntity()
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(action, &entity); err != nil {
		t.Fatal(err)
	}
	record.Research.Entity = entity
	if _, err := resolveRetainedResearchHistory(doc, record, run); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		value      string
		conversion inputConversion
		want       string
	}{{"CA", inputConversion{Kind: "text"}, `"CA"`}, {"-0", inputConversion{Kind: "number"}, `-0`}, {"0.000000000000000001", inputConversion{Kind: "decimal"}, `"0.000000000000000001"`}, {"false", inputConversion{Kind: "boolean", TrueValue: "true", FalseValue: "false"}, `false`}} {
		got, err := convertInput(row.value, row.conversion)
		raw, _ := json.Marshal(got)
		if err != nil || string(raw) != row.want {
			t.Fatalf("conversion %s: %s %v", row.conversion.Kind, raw, err)
		}
	}
	for name, want := range map[string]string{"integer_value": `-0`, "legacy_number_value": `1.2300e+2`, "boolean_value": `false`, "text_value": `""`} {
		got, err := typedResearchValue(doc, record, name)
		raw, _ := json.Marshal(got)
		if err != nil || string(raw) != want {
			t.Fatalf("research-value %s: %s %v", name, raw, err)
		}
	}
}

func retainedD1ModelEntity() ([]byte, error) {
	raw, err := os.ReadFile("testdata/retained-column/model-action.json")
	if err != nil {
		return nil, err
	}
	var model struct {
		Arguments struct {
			Records []json.RawMessage `json:"records"`
		} `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &model); err != nil {
		return nil, err
	}
	return model.Arguments.Records[0], nil
}

func TestRetainedD1KnownEmptyProjection(t *testing.T) {
	doc, record, run, snapshot := retainedD1Fixture(t)
	empty := inputResearchRecipe{Fields: []inputResearchField{}}
	doc.RecipeVersion = 1
	doc.Recipe.Research = &empty
	doc.Recipes = nil
	run.Recipe = empty
	history, err := resolveRetainedResearchHistory(doc, record, run)
	if err != nil {
		t.Fatal(err)
	}
	node, err := parseRetainedJSON(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	old := node.member("research").member("recipe").member("fields").raw
	snapshot = bytes.Replace(snapshot, old, []byte(`[]`), 1)
	carrier, err := projectRetainedReviewedInput(snapshot, history)
	if err != nil || carrier.Retained.Research.Declaration.Fields == nil || len(carrier.Retained.Research.Declaration.Fields) != 0 {
		t.Fatalf("known empty inventory lost: %v", err)
	}
	for _, replacement := range []string{`null`, `{}`, `false`} {
		mutated := bytes.Replace(snapshot, []byte(`"fields":[]`), []byte(`"fields":`+replacement), 1)
		if _, err := projectRetainedReviewedInput(mutated, history); err == nil {
			t.Fatal("unavailable/wrong-form inventory accepted")
		}
	}
}

func TestRetainedD1RawDepthAndDeclarationBounds(t *testing.T) {
	doc, record, run, snapshot := retainedD1Fixture(t)
	history, err := resolveRetainedResearchHistory(doc, record, run)
	if err != nil {
		t.Fatal(err)
	}
	for _, depth := range []int{64, 65} {
		value := strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth)
		raw := bytes.Replace(snapshot, []byte(`"value":-0`), []byte(`"value":`+value), 1)
		_, err := projectRetainedReviewedInput(raw, history)
		if (err != nil) != (depth == 65) {
			t.Fatalf("raw depth %d: %v", depth, err)
		}
	}
	// The declaration owner counts the compact structured field, independently
	// of snapshot and raw-value guards.
	for _, size := range []int{retainedDeclarationBytes, retainedDeclarationBytes + 1} {
		d, r, u, raw := retainedD1Fixture(t)
		original := *d.Recipes[0].Recipe.Research
		original.Fields = append([]inputResearchField(nil), original.Fields...)
		field := original.Fields[0]
		field.Description = ""
		base, _ := json.Marshal(field)
		field.Description = strings.Repeat("x", size-len(base))
		original.Fields[0] = field
		d.Recipes[0].Recipe.Research = &original
		u.Recipe = original
		h, err := resolveRetainedResearchHistory(d, r, u)
		if size > retainedDeclarationBytes {
			if err == nil {
				t.Fatal("one-over declaration accepted")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		n, err := parseRetainedJSON(raw)
		if err != nil {
			t.Fatal(err)
		}
		old := n.member("research").member("recipe").member("fields").elements[0].raw
		replacement, _ := json.Marshal(field)
		raw = bytes.Replace(raw, old, replacement, 1)
		if _, err := projectRetainedReviewedInput(raw, h); err != nil {
			t.Fatal("exact declaration limit refused:", err)
		}
	}
}

func TestRetainedD1RawEscapesAndUnresolvedStatus(t *testing.T) {
	doc, record, run, snapshot := retainedD1Fixture(t)
	history, err := resolveRetainedResearchHistory(doc, record, run)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{`"\u0043\u0041"`, `"\ud800"`, `"😀é"`} {
		raw := bytes.Replace(snapshot, []byte(`"value":"CA"`), []byte(`"value":`+token), 1)
		carrier, err := projectRetainedReviewedInput(raw, history)
		if err != nil || carrier.Retained.Research.Fields["legacy_string_value"].ValueJSON == nil || *carrier.Retained.Research.Fields["legacy_string_value"].ValueJSON != token || carrier.OriginalSnapshotJSON != string(raw) {
			t.Fatalf("raw legacy spelling changed: %s: %v", token, err)
		}
	}
	raw := bytes.Replace(snapshot, []byte(`"status":"stale"`), []byte(`"status":"conflicting"`), 1)
	carrier, err := projectRetainedReviewedInput(raw, history)
	if err != nil || carrier.Retained.Research.Fields["stale_value"].Status != "conflicting" {
		t.Fatalf("unresolved status lost: %v", err)
	}
}
