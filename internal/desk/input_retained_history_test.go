package desk

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// Reuses the handwritten D0a inventory/action, without running its integration
// or domain gate. This fixture grants no review/acquisition authority.
func retainedD1Fixture(t *testing.T) (inputPreparation, preparedInputRecord, inputResearchRun, []byte) {
	t.Helper()
	raw, err := os.ReadFile("testdata/retained-column/original-fields.json")
	if err != nil {
		t.Fatal(err)
	}
	var fields []inputResearchField
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	action, err := os.ReadFile("testdata/retained-column/model-action.json")
	if err != nil {
		t.Fatal(err)
	}
	var model struct {
		Arguments struct {
			Records []struct {
				Fields json.RawMessage `json:"fields"`
			} `json:"records"`
		} `json:"arguments"`
	}
	if err := json.Unmarshal(action, &model); err != nil {
		t.Fatal(err)
	}
	original := inputResearchRecipe{Version: 1, Fields: fields}
	current := original
	current.Fields = append([]inputResearchField(nil), fields...)
	for i := range current.Fields {
		current.Fields[i].Type = "boolean"
		current.Fields[i].Description = "Current requested type differs from original."
	}
	run := inputResearchRun{ID: "research_" + strings.Repeat("a", 32), PreparationID: "prep_" + strings.Repeat("b", 32), RecipeVersion: 1, Recipe: original, Revision: "original-run-revision"}
	record := preparedInputRecord{ID: "row_original", Class: "generated", Research: &inputResearchRecord{RunID: run.ID, RecipeVersion: 1}}
	doc := inputPreparation{ID: run.PreparationID, RecipeVersion: 2, Recipe: inputRecipe{Research: &current}, Recipes: []inputRecipeRevision{{Version: 1, Recipe: inputRecipe{Research: &original}}, {Version: 2, Recipe: inputRecipe{Research: &current}}}}
	snapshot := []byte(`{"version":1,"class":"generated","preparationId":"` + doc.ID + `","recipeVersion":2,"record":{"id":"row_original","class":"generated","sourceId":"original-source","values":{"text_value":"","stale_value":"unavailable","integer_value":"-0","name":"Example"},"research":{"runId":"` + run.ID + `","recipeVersion":1,"entity":{"fields":` + string(model.Arguments.Records[0].Fields) + `}}},"research":{"id":"` + run.ID + `","recipeVersion":1,"recipe":{"fields":` + string(raw) + `}},"values":{"integer_value":"-0"},"prepared":{"false":false},"opaque":{"legacy":"\ud800","number":1.2300e+2},"reviews":[{"note":"keep original provenance"}]}`)
	return doc, record, run, snapshot
}

func TestRetainedD1OriginalHistory(t *testing.T) {
	doc, record, run, _ := retainedD1Fixture(t)
	history, err := resolveRetainedResearchHistory(doc, record, run)
	if err != nil || len(history.Fields) != 11 || history.RecipeVersion != 1 || history.RunRevision != run.Revision {
		t.Fatalf("original inventory: %#v %v", history, err)
	}
	history.Fields[0].Type = "changed"
	if doc.Recipes[0].Recipe.Research.Fields[0].Type != "text" {
		t.Fatal("history aliases stored declaration")
	}
	for _, name := range []string{"missing", "repeated", "conflicting", "reordered", "unselected-type", "unselected-name", "unselected-description", "run-inventory", "run-id", "run-version", "run-preparation", "nil-inventory"} {
		t.Run(name, func(t *testing.T) {
			d, r, u, _ := retainedD1Fixture(t)
			original := *d.Recipes[0].Recipe.Research
			original.Fields = append([]inputResearchField(nil), original.Fields...)
			d.Recipes[0].Recipe.Research = &original
			switch name {
			case "missing":
				d.Recipes = nil
			case "repeated":
				d.Recipes = append(d.Recipes, d.Recipes[0])
			case "conflicting":
				d.RecipeVersion = 1
			case "reordered":
				original.Fields[0], original.Fields[1] = original.Fields[1], original.Fields[0]
			case "unselected-type":
				original.Fields[10].Type = "boolean"
			case "unselected-name":
				original.Fields[10].Name = "other"
			case "unselected-description":
				original.Fields[10].Description = "Changed unselected original metadata"
			case "run-inventory":
				u.Recipe.Fields = u.Recipe.Fields[:10]
			case "run-id":
				u.ID = "research_" + strings.Repeat("c", 32)
			case "run-version":
				u.RecipeVersion = 2
			case "run-preparation":
				u.PreparationID = "other"
			case "nil-inventory":
				original.Fields = nil
			}
			if _, err := resolveRetainedResearchHistory(d, r, u); err == nil {
				t.Fatal("inconsistent original inventory accepted")
			}
		})
	}
}

func TestRetainedD1CurrentWitnessAndKnownEmpty(t *testing.T) {
	doc, record, run, _ := retainedD1Fixture(t)
	doc.RecipeVersion = 1
	doc.Recipe = doc.Recipes[0].Recipe
	if _, err := resolveRetainedResearchHistory(doc, record, run); err != nil {
		t.Fatal("ordinary current plus same-version history refused:", err)
	}
	doc.Recipes = nil
	if _, err := resolveRetainedResearchHistory(doc, record, run); err != nil {
		t.Fatal("exact current original witness refused:", err)
	}
	empty := inputResearchRecipe{Fields: []inputResearchField{}}
	doc.Recipe.Research = &empty
	run.Recipe = empty
	history, err := resolveRetainedResearchHistory(doc, record, run)
	if err != nil || history.Fields == nil || !reflect.DeepEqual(history.Fields, []inputResearchField{}) {
		t.Fatalf("known empty lost: %#v %v", history, err)
	}
	run.Recipe.Fields = nil
	if _, err := resolveRetainedResearchHistory(doc, record, run); err == nil {
		t.Fatal("null/unavailable run inventory equated with known empty")
	}
}
