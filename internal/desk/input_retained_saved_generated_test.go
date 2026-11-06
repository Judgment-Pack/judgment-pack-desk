package desk

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// This exercises save-time logical validation, not generated preparation. The
// original research result is handwritten and enters through the same helpers
// as the B3 repository fixture. No journal, receipt, grant or profile is invented.
func TestRetainedB4ActualGeneratedRecordsLogicalSave(t *testing.T) {
	bin := os.Getenv("JPACK_RETAINED_DIALECT_RUNTIME")
	if bin == "" {
		t.Skip("explicit verified Runtime fixture required: JPACK_RETAINED_DIALECT_RUNTIME")
	}
	if !filepath.IsAbs(bin) {
		t.Fatal("actual Runtime must be explicitly selected by absolute path")
	}
	binary, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("explicit Runtime fixture sha256:%s", digestOf(binary))
	s, ts := newTestServer(t, false)
	s.cfg.JpackBin = bin
	if s.jobs != nil || s.localGateway != nil {
		t.Fatal("fixture unexpectedly has execution components")
	}

	research := researchRecipeFixture()
	research.Fields = []inputResearchField{{Name: "legacy_string_value", Description: "Original country", Type: "string"}}
	run := inputResearchRun{Version: 1, ID: "research_" + strings.Repeat("a", 32), PreparationID: "prep_" + strings.Repeat("b", 32), RecipeVersion: 1, Recipe: research, Seeds: []string{"https://example.com/"}}
	if err := applyResearchRecords(&run, json.RawMessage(`{"records":[{"kind":"company","name":"Example","url":"https://example.com/","fields":{"legacy_string_value":{"value":"CA","status":"interpretation","sources":[]}}}]}`)); err != nil {
		t.Fatal(err)
	}
	if len(run.Records) != 1 {
		t.Fatal("handwritten research fixture did not retain its record")
	}
	original := inputRecipe{Intent: "Retain original research", Research: &research}
	content := "unused\noriginal source bytes\n"
	doc := inputPreparation{
		Version: 1, ID: run.PreparationID, Name: "Original generated inputs", RecipeVersion: 1, Recipe: original,
		Recipes: []inputRecipeRevision{{Version: 1, Name: "Original generated inputs", Recipe: original, At: d0aStamp}},
		Records: []preparedInputRecord{researchPreparedRecord(run, run.Records[0])},
		Imports: []inputImport{{ID: "source_" + digestOf([]byte("csv\n"+content)), Kind: "csv", Name: "original.csv", Digest: retainedDigest([]byte(content)), Content: content, At: d0aStamp, RecipeVersion: 1}},
	}
	repository := s.preparationRepository()
	if _, err = repository.CompareAndSwap(t.Context(), doc, ""); err != nil {
		t.Fatal(err)
	}
	view, err := repository.Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	held, err := view.Get(doc.ID)
	view.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(held.Document.Records) != 1 || held.Document.Records[0].Class != "generated" || held.Document.Records[0].Research == nil || held.Document.cellCustody == nil {
		t.Fatal("real repository read did not retain generated record custody")
	}
	path := "/api/input-preparations/" + doc.ID
	file := filepath.Join(s.projectDir, inputPreparationDir, doc.ID+".json")
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	q := retainedB4Qualification(t)
	// Keep the complete five-kind mapping, availability rule and sidecars. None
	// of these planned columns exists in this original generated record.
	kinds := []string{"text", "decimal", "number", "boolean", "research-value"}
	if len(q.Mapping.Case.Facts) != len(kinds) || len(q.Mapping.Case.Evidence) != 1 || len(q.Conversions) != len(kinds) {
		t.Fatal("complete logical mapping fixture changed")
	}
	for i, fact := range q.Mapping.Case.Facts {
		if fact.Conversion == nil || fact.Conversion.Kind != kinds[i] {
			t.Fatal("logical conversion kind changed", i)
		}
		if _, exists := held.Document.Records[0].Values[fact.Conversion.Column]; exists {
			t.Fatal("planned column unexpectedly ready", fact.Conversion.Column)
		}
	}
	candidate := original
	candidate.Qualification = q
	body := map[string]any{"name": "Selected generated inputs", "recipe": candidate}
	w := retainedB4Request(t, s, ts, "PUT", path, body, held.SHA256)
	if w.Code != 200 {
		t.Fatalf("actual Runtime logical save with generated records: %d %s", w.Code, w.Body.Bytes())
	}
	var selected inputPreparationReply
	if err = json.Unmarshal(w.Body.Bytes(), &selected); err != nil {
		t.Fatal(err)
	}
	assertSaved := func(prior inputPreparationReply, expected *inputQualification, responseRaw, priorRaw []byte) []byte {
		t.Helper()
		current, err := repository.Get(t.Context(), doc.ID)
		if err != nil {
			t.Fatal(err)
		}
		// The HTTP projection has no private cell custody. Compare its exact wire
		// reply with the trusted repository reply, without resealing it.
		currentResponse, err := json.Marshal(current)
		if err != nil {
			t.Fatal(err)
		}
		if current.SHA256 == prior.SHA256 || !bytes.Equal(bytes.TrimSpace(responseRaw), currentResponse) {
			t.Fatal("save response is not the committed repository readback")
		}
		if current.Document.RecipeVersion != prior.Document.RecipeVersion+1 || len(current.Document.Recipes) != len(prior.Document.Recipes)+1 || !reflect.DeepEqual(current.Document.Recipes[:len(prior.Document.Recipes)], prior.Document.Recipes) {
			t.Fatal("save rewrote original recipe history")
		}
		if !reflect.DeepEqual(current.Document.Recipe.Qualification, expected) {
			t.Fatal("save changed the complete logical mapping or sidecars")
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		oldNode, err := parseRetainedJSON(priorRaw)
		if err != nil {
			t.Fatal(err)
		}
		node, err := parseRetainedJSON(raw)
		if err != nil {
			t.Fatal(err)
		}
		for _, member := range []string{"imports", "records"} {
			if oldNode.member(member) == nil || node.member(member) == nil || !bytes.Equal(oldNode.member(member).raw, node.member(member).raw) {
				t.Fatal("save changed exact original source/record/value bytes", member)
			}
		}
		for i := range oldNode.member("recipes").elements {
			if !bytes.Equal(oldNode.member("recipes").elements[i].raw, node.member("recipes").elements[i].raw) {
				t.Fatal("save changed exact original history bytes", i)
			}
		}
		if current.SHA256 != digestOf(raw) || current.Document.cellCustody == nil || current.Document.cellCustody.sha != current.SHA256 {
			t.Fatal("committed readback lacks fresh file custody")
		}
		return raw
	}
	afterFirst := assertSaved(held, q, w.Body.Bytes(), before)

	// A genuinely changed selected candidate must perform a fresh Runtime
	// check. An unavailable Runtime cannot reuse the first save's success.
	q.Mapping.Case.Facts[0].Conversion.Column = "Changed planned column"
	// Column belongs to the logical leaf, not inputConversion's sidecar.
	// Prove local admission remains valid before attributing the next refusal
	// to the unavailable Runtime rather than a malformed changed candidate.
	rules, err := retainedInputMappingRules(q)
	if err != nil || len(rules.Facts) != len(kinds) || rules.Facts[0].Column != "Changed planned column" {
		t.Fatal("changed logical mapping is locally inconsistent", err)
	}
	if err := validateInputQualification(q); err != nil {
		t.Fatal("changed selected qualification failed local admission", err)
	}
	s.cfg.JpackBin = filepath.Join(t.TempDir(), "unavailable-runtime")
	w = retainedB4Request(t, s, ts, "PUT", path, body, selected.SHA256)
	if w.Code != 422 {
		t.Fatalf("changed logical save reused earlier check: %d %s", w.Code, w.Body.Bytes())
	}
	failed, err := repository.Get(t.Context(), doc.ID)
	if err != nil || failed.SHA256 != selected.SHA256 {
		t.Fatal("failed fresh check changed repository revision", err)
	}
	failedRaw, err := os.ReadFile(file)
	if err != nil || !bytes.Equal(failedRaw, afterFirst) {
		t.Fatal("failed fresh check changed original file bytes", err)
	}
	s.cfg.JpackBin = bin
	w = retainedB4Request(t, s, ts, "PUT", path, body, selected.SHA256)
	if w.Code != 200 {
		t.Fatalf("actual Runtime changed logical save with generated records: %d %s", w.Code, w.Body.Bytes())
	}
	var changed inputPreparationReply
	if err = json.Unmarshal(w.Body.Bytes(), &changed); err != nil {
		t.Fatal(err)
	}
	assertSaved(selected, q, w.Body.Bytes(), afterFirst)
	if s.jobs != nil || s.localGateway != nil {
		t.Fatal("logical save acquired execution components")
	}
	t.Log("actual Runtime accepted two logical saves over held generated records, including a changed selected mapping; absent planned columns did not require per-record readiness")
}
