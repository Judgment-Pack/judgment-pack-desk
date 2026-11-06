package desk

// D0a exercises the existing producer and published factory boundary. It does
// not claim that the separately versioned retained-column producer exists yet.
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const d0aStamp = "2026-10-28T12:00:00Z"

var d0aTypedValues = []struct{ name, target, raw string }{
	{"text_value", "/typed/text", `""`},
	{"integer_value", "/typed/integer", `-0`},
	{"decimal_value", "/typed/decimal", `"0.000000000000000001"`},
	{"boolean_value", "/typed/boolean", `false`},
	{"text_list_value", "/typed/text-list", `["a","b"]`},
	{"range_value", "/typed/range", `{"min":"0.00","max":"1.01"}`},
	{"legacy_string_value", "/typed/string", `"CA"`},
	{"legacy_number_value", "/typed/number", `1.2300e+2`},
}

func d0aEvidence(t *testing.T, name string, raw []byte) {
	t.Helper()
	t.Logf("artifact %s bytes=%d sha256=%s", name, len(raw), digestOf(raw))
	dir := os.Getenv("JPS_DIALECT_D0A_EVIDENCE")
	if dir == "" {
		return
	}
	if !filepath.IsAbs(dir) || filepath.Base(name) != name {
		t.Fatal("D0a evidence requires an absolute scratch directory and basename")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func d0aJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func d0aObject(t *testing.T, raw []byte) map[string]json.RawMessage {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		t.Fatalf("D0a object: %v", err)
	}
	return object
}

func d0aQualification(t *testing.T) *inputQualification {
	t.Helper()
	q := qualificationFixture()
	q.Mapping.Case.Facts = []inputDialectFact{}
	q.Mapping.Case.Evidence = []inputDialectEvidence{}
	q.Conversions = map[string]inputConversion{}
	for _, value := range d0aTypedValues {
		q.Mapping.Case.Facts = append(q.Mapping.Case.Facts, inputDialectFact{Target: value.target, Source: "/values/" + value.name})
		q.Conversions[value.target] = inputConversion{Kind: "research-value"}
	}
	for _, name := range []string{"missing", "null", "stale"} {
		target := "/typed/" + name
		q.Mapping.Case.Facts = append(q.Mapping.Case.Facts, inputDialectFact{Target: target, Source: "/values/" + name + "_value"})
		q.Conversions[target] = inputConversion{Kind: "research-value"}
	}
	for _, value := range []struct {
		target, column string
		conversion     inputConversion
	}{
		{"/converted/text", "legacy_string_value", inputConversion{Kind: "text"}},
		{"/converted/decimal", "decimal_value", inputConversion{Kind: "decimal"}},
		{"/converted/number", "integer_value", inputConversion{Kind: "number"}},
		{"/converted/boolean", "boolean_value", inputConversion{Kind: "boolean", TrueValue: "true", FalseValue: "false"}},
	} {
		q.Mapping.Case.Facts = append(q.Mapping.Case.Facts, inputDialectFact{Target: value.target, Source: "/values/" + value.column})
		q.Conversions[value.target] = value.conversion
	}
	// Each target is authored into this fixture pack. D0a checks host storage and
	// conversion admission; it does not claim Runtime semantic pack validation.
	var pack map[string]any
	if err := json.Unmarshal([]byte(qualificationTestPack), &pack); err != nil {
		t.Fatal(err)
	}
	rules := []map[string]any{}
	for i, fact := range q.Mapping.Case.Facts {
		rules = append(rules, map[string]any{"id": fmt.Sprintf("fixture_%02d", i), "when": map[string]any{"op": "fact", "path": fact.Target, "operator": "equals", "value": true}, "outcome": "ready", "description": "D0a declared conversion target", "onUnknown": "escalate"})
	}
	pack["rules"], pack["evidenceRequirements"] = rules, []any{}
	q.Pack = string(d0aJSON(t, pack))
	q.Digest = sha256Digest([]byte(q.Pack))
	if err := validateInputQualification(q); err != nil {
		t.Fatal(err)
	}
	return q
}

func d0aStoreRun(t *testing.T, s *Server, run inputResearchRun, proof []byte) {
	t.Helper()
	root, err := s.inputResearchRoot(true)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	dir, err := openInputResearch(root, run.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if len(proof) > 0 {
		if err := writePrivateData(dir, handoffProofSourceID+".proof.json", proof); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeInputResearch(dir, run); err != nil {
		t.Fatal(err)
	}
}

func d0aBaseline(t *testing.T, bundle string) {
	s, _ := newTestServer(t, false)
	s.cfg.LocalGatewayBundle = bundle
	fieldsRaw, err := os.ReadFile("testdata/retained-column/original-fields.json")
	if err != nil {
		t.Fatal(err)
	}
	var fields []inputResearchField
	if err := json.Unmarshal(fieldsRaw, &fields); err != nil {
		t.Fatal(err)
	}
	actionRaw, err := os.ReadFile("testdata/retained-column/model-action.json")
	if err != nil {
		t.Fatal(err)
	}
	action, err := decodeResearchAction(actionRaw)
	if err != nil || action.Tool != "records" {
		t.Fatalf("handwritten model action: %v", err)
	}
	source, proof, pin := handoffProofFixture(t, filepath.Join(bundle, executableName("gateway")))
	recipe := researchRecipeFixture()
	recipe.Fields = fields
	if err := validateInputResearch(&recipe); err != nil {
		t.Fatal(err)
	}
	run := inputResearchRun{Version: 1, ID: "research_" + strings.Repeat("a", 32), PreparationID: "prep_" + strings.Repeat("b", 32), RecipeVersion: 1, Recipe: recipe, Gateway: pin, State: "completed", CreatedAt: d0aStamp, UpdatedAt: d0aStamp, Intent: "Handwritten original typed history fixture", Seeds: []string{"https://example.com/"}, Sources: []inputResearchSource{source}, Records: []inputResearchEntity{}, Attempts: []inputResearchAttempt{}}
	if err := applyResearchRecords(&run, action.Arguments); err != nil {
		t.Fatal(err)
	}
	run.Attempts = []inputResearchAttempt{{ID: "attempt_model", Kind: "model", State: "completed", Result: append(json.RawMessage(nil), actionRaw...), FinishedAt: d0aStamp}}
	d0aStoreRun(t, s, run, proof)
	q := d0aQualification(t)
	original := inputRecipe{Intent: run.Intent, Research: &recipe, Qualification: q}
	currentResearch := recipe
	currentResearch.Fields = append([]inputResearchField(nil), fields...)
	for i := range currentResearch.Fields {
		currentResearch.Fields[i].Type = "boolean"
		currentResearch.Fields[i].Description = "Current requested type differs from original."
	}
	current := inputRecipe{Intent: "Changed current recipe", Research: &currentResearch, Qualification: q}
	doc := inputPreparation{Version: 1, ID: run.PreparationID, Name: "D0a original history", Recipe: current, RecipeVersion: 2, Recipes: []inputRecipeRevision{{Version: 1, Name: "Original", Recipe: original, At: d0aStamp}, {Version: 2, Name: "Current", Recipe: current, At: d0aStamp}}, CreatedAt: d0aStamp, UpdatedAt: d0aStamp, Records: []preparedInputRecord{}, Imports: []inputImport{}, Activity: []inputActivity{}}
	if _, err := s.preparationRepository().CompareAndSwap(context.Background(), doc, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.publishResearchRecords(run); err != nil {
		t.Fatal(err)
	}
	saved, err := s.preparationRepository().Get(context.Background(), doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	saved = acceptFixture(t, s, saved)
	record := saved.Document.Records[0]
	rows, err := prepareInputSelection(saved.Document, []string{record.ID})
	if err != nil || len(rows) != 1 || len(rows[0].Issues) != 0 {
		t.Fatalf("actual retained selection: %v %#v", err, rows)
	}
	row := rows[0]
	withoutHistory := saved.Document
	withoutHistory.Recipes = nil
	for _, value := range d0aTypedValues {
		actual, err := typedResearchValue(saved.Document, record, value.name)
		if err != nil || string(d0aJSON(t, actual)) != value.raw || string(d0aJSON(t, row.Prepared[value.target])) != value.raw {
			t.Fatalf("original %s lost type/token: %s %v", value.name, d0aJSON(t, actual), err)
		}
		if _, err := typedResearchValue(withoutHistory, record, value.name); err == nil {
			t.Fatalf("original history was not needed for %s", value.name)
		}
	}
	for target, want := range map[string]string{"/converted/text": `"CA"`, "/converted/decimal": `"0.000000000000000001"`, "/converted/number": `-0`, "/converted/boolean": `false`} {
		if got := string(d0aJSON(t, row.Prepared[target])); got != want {
			t.Fatalf("%s: %s, want %s", target, got, want)
		}
	}
	for _, name := range []string{"missing", "null", "stale"} {
		if _, present := row.Prepared["/typed/"+name]; present {
			t.Fatalf("unavailable %s became a prepared fact", name)
		}
	}
	snapshot, err := s.reviewedInputSnapshot(context.Background(), saved.Document, record, row)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot) > 524288 {
		t.Fatal("existing snapshot exceeded its independent owner cap")
	}
	outer := d0aObject(t, snapshot)
	retainedRecord := d0aObject(t, outer["record"])
	retainedResearch := d0aObject(t, outer["research"])
	retainedRecipe := d0aObject(t, retainedResearch["recipe"])
	var observedFields []inputResearchField
	if err := json.Unmarshal(retainedRecipe["fields"], &observedFields); err != nil || !reflect.DeepEqual(observedFields, fields) {
		t.Fatalf("complete original ordered inventory changed: %v", err)
	}
	if !bytes.Equal(retainedRecipe["fields"], d0aJSON(t, fields)) {
		t.Fatal("original structured declaration serializer changed")
	}
	unfiltered := d0aObject(t, retainedRecord["values"])
	filtered := d0aObject(t, outer["values"])
	for _, name := range []string{"text_value", "stale_value"} {
		if _, ok := unfiltered[name]; !ok {
			t.Fatalf("unfiltered original value %s disappeared", name)
		}
		if _, ok := filtered[name]; ok {
			t.Fatalf("fixture did not demonstrate filtered-value difference for %s", name)
		}
	}
	entity := d0aObject(t, d0aObject(t, retainedRecord["research"])["entity"])
	typedFields := d0aObject(t, entity["fields"])
	for _, value := range d0aTypedValues {
		if got := string(d0aObject(t, typedFields[value.name])["value"]); got != value.raw {
			t.Fatalf("snapshot raw token %s: %s, want %s", value.name, got, value.raw)
		}
	}
	if _, present := d0aObject(t, typedFields["missing_value"])["value"]; present {
		t.Fatal("omitted missing value became present")
	}
	if got := string(d0aObject(t, typedFields["null_value"])["value"]); got != "null" {
		t.Fatalf("explicit null changed: %s", got)
	}
	// This known v1 gap is observed, not treated as proof that production loaded
	// the complete history. D1 will introduce the actual full-inventory check.
	changed := saved.Document
	changed.Recipes = append([]inputRecipeRevision(nil), saved.Document.Recipes...)
	changedOriginal := *changed.Recipes[0].Recipe.Research
	changedOriginal.Fields = append([]inputResearchField(nil), changedOriginal.Fields...)
	changedOriginal.Fields[len(changedOriginal.Fields)-1].Description = "Changed unselected original metadata"
	changed.Recipes[0].Recipe.Research = &changedOriginal
	unchangedSnapshot, err := s.reviewedInputSnapshot(context.Background(), changed, record, row)
	if err != nil || !bytes.Equal(snapshot, unchangedSnapshot) {
		t.Fatalf("known v1 full-history gap observation changed: %v", err)
	}
	d0aEvidence(t, "baseline-original-model-action.json", actionRaw)
	d0aEvidence(t, "baseline-source-proof.json", proof)
	d0aEvidence(t, "baseline-original-fields.json", d0aJSON(t, fields))
	d0aEvidence(t, "baseline-current-fields.json", d0aJSON(t, currentResearch.Fields))
	d0aEvidence(t, "baseline-collection.json", d0aJSON(t, saved.Document))
	d0aEvidence(t, "baseline-prepared.json", d0aJSON(t, row))
	d0aEvidence(t, "baseline-original-snapshot.json", snapshot)
	d0aEvidence(t, "baseline-observation.json", d0aJSON(t, map[string]any{"status": "baseline-preserved", "originalRecipeVersion": record.Research.RecipeVersion, "currentRecipeVersion": saved.Document.RecipeVersion, "originalFields": len(fields), "originalTypes": 8, "conversions": 5, "snapshotBytes": len(snapshot), "snapshotSha256": sha256Digest(snapshot), "fullHistoryProducerPrecondition": "not-implemented-in-v1", "unselectedHistoryMutationAffectsSnapshot": false, "sourceProof": "test-only-fixed-key-proof-verified-by-selected-Gateway", "newV2SignedCarrier": "not-exercised"}))
}

func TestRetainedD0aOriginalHistoryAndFactoryDomain(t *testing.T) {
	bundle := os.Getenv("JPS_TEST_GATEWAY_BUNDLE")
	if bundle == "" {
		t.Skip("requires explicitly selected scratch Gateway bundle")
	}
	if !filepath.IsAbs(bundle) {
		t.Fatal("selected test bundle must be an absolute scratch path")
	}
	if !t.Run("OriginalHistoryBaseline", func(t *testing.T) { d0aBaseline(t, bundle) }) {
		return
	}
	t.Run("ReachableFactoryDomain", func(t *testing.T) { d0aFactoryDomain(t, bundle) })
}
