package desk

import (
	"encoding/json"
	"strings"
	"testing"
)

func typedResearchFixture(t *testing.T, kind, raw string) (inputPreparation, preparedInputRecord) {
	t.Helper()
	q := qualificationFixture()
	q.Mapping.Case.Facts = []inputDialectFact{{"/value", "/values/value"}}
	q.Mapping.Case.Evidence = nil
	q.Conversions = map[string]inputConversion{"/value": {Kind: "research-value"}}
	original := inputRecipe{Research: &inputResearchRecipe{Fields: []inputResearchField{{Name: "value", Type: kind}}}}
	doc := inputPreparation{RecipeVersion: 2, Recipe: inputRecipe{Qualification: q, Research: &inputResearchRecipe{Fields: []inputResearchField{{Name: "value", Type: "boolean"}}}}, Recipes: []inputRecipeRevision{{Version: 1, Recipe: original}}}
	row := preparedInputRecord{ID: "row", Class: "generated", Values: map[string]string{"value": "misleading display"}, Research: &inputResearchRecord{RecipeVersion: 1, Entity: inputResearchEntity{Fields: map[string]inputResearchValue{"value": {Status: "supported", Value: json.RawMessage(raw)}}}}}
	return doc, row
}
func TestTypedResearchQualificationPreservesOriginalValues(t *testing.T) {
	for _, tc := range []struct{ kind, raw string }{{"text", `""`}, {"integer", `0`}, {"integer", `9007199254740991`}, {"decimal", `"0.000000000000000001"`}, {"boolean", `false`}, {"text-list", `[]`}, {"text-list", `["a","b"]`}, {"range", `{"min":"0.00","max":"1.01"}`}} {
		t.Run(tc.kind+tc.raw, func(t *testing.T) {
			doc, record := typedResearchFixture(t, tc.kind, tc.raw)
			if err := validateInputConversions(doc.Recipe.Qualification); err != nil {
				t.Fatal(err)
			}
			prepared, err := prepareInputRecord(doc, record)
			if err != nil || len(prepared.Issues) != 0 {
				t.Fatal(prepared, err)
			}
			raw, err := json.Marshal(prepared.Prepared["/value"])
			if err != nil || string(raw) != tc.raw {
				t.Fatalf("typed fact changed: %s %v", raw, err)
			}
			if prepared.Origin != "generated" || len(prepared.Input) != 0 {
				t.Fatal("conversion bypassed generated-source handoff")
			}
			doc.Records = []preparedInputRecord{record}
			if _, err := prepareInputSelection(doc, []string{record.ID}); err == nil || !strings.Contains(err.Error(), "Ready for evaluation") {
				t.Fatal("conversion authorized evaluation")
			}
		})
	}
}
func TestTypedResearchQualificationKeepsUnknownsAbsent(t *testing.T) {
	for _, status := range []string{"missing", "conflicting", "inaccessible", "stale"} {
		doc, record := typedResearchFixture(t, "boolean", `false`)
		field := record.Research.Entity.Fields["value"]
		field.Status = status
		record.Research.Entity.Fields["value"] = field
		row, err := prepareInputRecord(doc, record)
		if err != nil || len(row.Prepared) != 0 || len(row.Issues) != 0 {
			t.Fatal(status, row, err)
		}
	}
}
func TestTypedResearchQualificationRefusesWrongProvenance(t *testing.T) {
	for _, mutate := range []func(*inputPreparation, *preparedInputRecord){
		func(d *inputPreparation, r *preparedInputRecord) { d.Recipes = nil },
		func(d *inputPreparation, r *preparedInputRecord) { r.Class = "asserted" },
		func(d *inputPreparation, r *preparedInputRecord) {
			d.Recipes[0].Recipe.Research.Fields[0].Name = "other"
		},
		func(d *inputPreparation, r *preparedInputRecord) {
			f := r.Research.Entity.Fields["value"]
			f.Value = json.RawMessage(`"narrative"`)
			r.Research.Entity.Fields["value"] = f
		},
	} {
		doc, record := typedResearchFixture(t, "range", `{"min":0,"max":1}`)
		mutate(&doc, &record)
		if _, err := typedResearchValue(doc, record, "value"); err == nil {
			t.Fatal("invalid source accepted")
		}
	}
}

func TestTypedResearchCannotReplaceHostMetadata(t *testing.T) {
	for _, column := range []string{"name", "url", "website", "company_id", "company_name", "company_url"} {
		doc, record := typedResearchFixture(t, "text", `"model-parent"`)
		doc.Recipes[0].Recipe.Research.Fields[0].Name = column
		record.Research.Entity.Fields[column] = record.Research.Entity.Fields["value"]
		record.Values[column] = "selected-parent"
		record.Research.Entity.CompanyID = "selected-parent"
		if _, err := typedResearchValue(doc, record, column); err == nil {
			t.Fatal("model replaced host metadata", column)
		}
	}
}
