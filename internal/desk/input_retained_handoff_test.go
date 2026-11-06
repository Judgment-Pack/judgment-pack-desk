package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The original declarations and model action are handwritten fixtures. This
// owner fixture persists and reviews a real run before calling either producer.
func retainedD3aOwnerFixture(t *testing.T, bundle string) (*Server, inputPreparation, preparedInputRecord, inputPreparedRow, inputResearchRun) {
	t.Helper()
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
	doc := inputPreparation{Version: 1, ID: run.PreparationID, Name: "D3a original history", Recipe: current, RecipeVersion: 2, Recipes: []inputRecipeRevision{{Version: 1, Name: "Original", Recipe: original, At: d0aStamp}, {Version: 2, Name: "Current", Recipe: current, At: d0aStamp}}, CreatedAt: d0aStamp, UpdatedAt: d0aStamp, Records: []preparedInputRecord{}, Imports: []inputImport{}, Activity: []inputActivity{}}
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
	return s, saved.Document, record, rows[0], run
}

func TestRetainedD3aActualReviewedSnapshotOwner(t *testing.T) {
	bundle := os.Getenv("JPS_TEST_GATEWAY_BUNDLE")
	if bundle == "" {
		t.Skip("requires explicitly selected scratch Gateway bundle")
	}
	if !filepath.IsAbs(bundle) {
		t.Fatal("selected test bundle must be an absolute scratch path")
	}
	s, doc, record, row, run := retainedD3aOwnerFixture(t, bundle)
	ctx := context.Background()
	legacy, err := s.reviewedInputSnapshot(ctx, doc, record, row)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.retainedReviewedInputSnapshot(ctx, doc, record, row)
	if err != nil {
		t.Fatal(err)
	}
	legacyAfter, err := s.reviewedInputSnapshot(ctx, doc, record, row)
	if err != nil || !bytes.Equal(legacy, legacyAfter) {
		t.Fatal("ordinary v1 snapshot bytes changed")
	}
	if !bytes.Equal([]byte(got.Carrier.OriginalSnapshotJSON), legacy) || got.OriginalSnapshotSHA256 != sha256Digest(legacy) || got.Carrier.OriginalSnapshotSHA256 != got.OriginalSnapshotSHA256 || got.RunRevision != run.Revision {
		t.Fatal("exact legacy snapshot or checked run revision changed")
	}
	carrierNode, err := parseRetainedJSON(got.CarrierJSON)
	if err != nil || carrierNode.member("retained") == nil || got.ProjectedFrameSHA256 != sha256Digest(carrierNode.member("retained").raw) || got.ProjectedFrameSHA256 == got.OriginalSnapshotSHA256 {
		t.Fatal("frame and original snapshot digests were conflated")
	}
	if len(got.Carrier.Retained.Research.Declaration.Fields) != 11 {
		t.Fatal("complete ordered original declaration inventory lost")
	}
	for i, field := range run.Recipe.Fields {
		want := d0aJSON(t, field)
		declaration := got.Carrier.Retained.Research.Declaration.Fields[i]
		if declaration.Name != field.Name || declaration.FieldJSON != string(want) {
			t.Fatalf("original declaration %d changed", i)
		}
	}
	for _, value := range d0aTypedValues {
		if raw := got.Carrier.Retained.Research.Fields[value.name].ValueJSON; raw == nil || *raw != value.raw {
			t.Fatalf("original raw token %s changed", value.name)
		}
		if string(d0aJSON(t, row.Prepared[value.target])) != value.raw {
			t.Fatalf("prepared token %s changed", value.name)
		}
	}
	for target, want := range map[string]string{"/converted/text": `"CA"`, "/converted/decimal": `"0.000000000000000001"`, "/converted/number": `-0`, "/converted/boolean": `false`} {
		if string(d0aJSON(t, row.Prepared[target])) != want {
			t.Fatalf("conversion %s changed", target)
		}
	}
	if got.Carrier.Retained.Research.Fields["missing_value"].ValueJSON != nil || got.Carrier.Retained.Research.Fields["null_value"].ValueJSON == nil || *got.Carrier.Retained.Research.Fields["null_value"].ValueJSON != "null" || got.Carrier.Retained.Research.Fields["stale_value"].Status != "stale" {
		t.Fatal("missing, null or status distinction changed")
	}
	for _, name := range []string{"missing", "repeated", "unselected-description", "current-disagreement"} {
		t.Run(name, func(t *testing.T) {
			changed := doc
			changed.Recipes = append([]inputRecipeRevision(nil), doc.Recipes...)
			original := *changed.Recipes[0].Recipe.Research
			original.Fields = append([]inputResearchField(nil), original.Fields...)
			changed.Recipes[0].Recipe.Research = &original
			switch name {
			case "missing":
				changed.Recipes = nil
			case "repeated":
				changed.Recipes = append(changed.Recipes, changed.Recipes[0])
			case "unselected-description":
				original.Fields[len(original.Fields)-1].Description = "Changed unselected original metadata"
			case "current-disagreement":
				changed.RecipeVersion = 1
			}
			if result, err := s.retainedReviewedInputSnapshot(ctx, changed, record, row); err == nil || result.Carrier.ContractVersion != "" {
				t.Fatal("inconsistent original history produced a carrier")
			}
		})
	}
	// The actual model and source verifier still own admission on this path.
	root, err := s.inputResearchRoot(false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	dir, err := openInputResearch(root, run.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	withoutModel := run
	withoutModel.Attempts = nil
	if err := writeInputResearch(dir, withoutModel); err != nil {
		t.Fatal(err)
	}
	if result, err := s.retainedReviewedInputSnapshot(ctx, doc, record, row); err == nil || result.Carrier.ContractVersion != "" {
		t.Fatal("missing retained model response produced a carrier")
	}
	if err := writeInputResearch(dir, run); err != nil {
		t.Fatal(err)
	}
	changedRun := run
	changedRun.Recipe.Fields = append([]inputResearchField(nil), run.Recipe.Fields...)
	changedRun.Recipe.Fields[len(changedRun.Recipe.Fields)-1].Description = "Changed original run inventory"
	if err := writeInputResearch(dir, changedRun); err != nil {
		t.Fatal(err)
	}
	if result, err := s.retainedReviewedInputSnapshot(ctx, doc, record, row); err == nil || result.Carrier.ContractVersion != "" {
		t.Fatal("run/original history disagreement produced a carrier")
	}
	if err := writeInputResearch(dir, run); err != nil {
		t.Fatal(err)
	}
	proof, err := readPrivateData(dir, handoffProofSourceID+".proof.json", inputResearchRecordLimit)
	if err != nil {
		t.Fatal(err)
	}
	if err := writePrivateData(dir, handoffProofSourceID+".proof.json", bytes.ReplaceAll(proof, []byte("Example operates in CA."), []byte("Modified source text."))); err != nil {
		t.Fatal(err)
	}
	if result, err := s.retainedReviewedInputSnapshot(ctx, doc, record, row); err == nil || !reflect.DeepEqual(result, retainedReviewedInputProduction{}) {
		t.Fatal("changed retained proof produced a carrier")
	}
}
