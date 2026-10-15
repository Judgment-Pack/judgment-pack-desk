package desk

import (
	"encoding/json"
	"testing"
)

func TestGenericResearchRecordsPreserveIdentityAndTypes(t *testing.T) {
	recipe := researchRecipeFixture()
	recipe.Target = "records"
	if err := validateInputResearch(&recipe); err != nil {
		t.Fatal(err)
	}
	source := inputResearchSource{ID: "page", Kind: "page", URL: "https://example.com/items/1", URLs: []string{"https://example.com/items/1", "https://example.com/items/2"}, Text: "Recorded in Canada.", Verification: "verified"}
	record := inputResearchEntity{Kind: "record", Name: "Work item", URL: source.URL, Fields: map[string]inputResearchValue{"country": {Status: "supported", Value: json.RawMessage(`"Canada"`), Sources: []inputResearchCitation{{SourceID: source.ID, Quote: "Recorded in Canada."}}}}}
	sources := map[string]inputResearchSource{source.ID: source}
	urls := map[string]bool{source.URL: true, "https://example.com/items/2": true}
	if err := validateResearchEntity(&record, recipe, sources, urls, nil); err != nil {
		t.Fatal(err)
	}
	other := record
	other.URL = "https://example.com/items/2"
	if err := validateResearchEntity(&other, recipe, sources, urls, nil); err != nil {
		t.Fatal(err)
	}
	if researchSameIdentity(record, other) || record.ID == other.ID {
		t.Fatal("different generic records on one domain collapsed")
	}
	if !researchSameIdentity(record, record) {
		t.Fatal("same record not deduplicated")
	}
	other.Kind = "company"
	if validateResearchEntity(&other, recipe, sources, urls, nil) == nil {
		t.Fatal("wrong target admitted")
	}
	record.URL = "https://invented.example/"
	if validateResearchEntity(&record, recipe, sources, urls, nil) == nil {
		t.Fatal("invented evidence admitted")
	}
}

func TestDialectTemplateOriginValidation(t *testing.T) {
	good := inputTemplateOrigin{ID: "work-items", Name: "Work items", License: "Apache-2.0", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	if err := validateInputTemplate(&good); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*inputTemplateOrigin){func(v *inputTemplateOrigin) { v.Digest = "latest" }, func(v *inputTemplateOrigin) { v.ID = "../anything" }, func(v *inputTemplateOrigin) { v.License = "" }} {
		v := good
		change(&v)
		if validateInputTemplate(&v) == nil {
			t.Fatal("bad template admitted")
		}
	}
}

func TestPersonRelationshipColumnsAreVersioned(t *testing.T) {
	person := inputResearchEntity{ID: "person", Kind: "person", Name: "Alex", URL: "https://example.com/team/alex", CompanyID: "company", Fields: map[string]inputResearchValue{}}
	run := inputResearchRun{ID: "run", Companies: []inputResearchEntity{{ID: "company", Kind: "company", Name: "Example", URL: "https://example.com/"}}}
	before := researchPreparedRecord(run, person)
	if _, ok := before.Values["company_id"]; ok {
		t.Fatal("legacy published bytes changed")
	}
	run.RelationshipVersion = 1
	after := researchPreparedRecord(run, person)
	if after.Values["company_id"] != "company" || after.Values["company_name"] != "Example" || after.Values["company_url"] != "https://example.com/" {
		t.Fatal("new relationship columns missing")
	}
	if after.ID != before.ID {
		t.Fatal("record identity changed")
	}
}

func TestGenericRecipeDefaultsAndSelectedCompanyScope(t *testing.T) {
	recipe := researchRecipeFixture()
	recipe.Target = ""
	if err := validateInputResearch(&recipe); err != nil || recipe.Target != "records" {
		t.Fatalf("omitted target must default to records: %q %v", recipe.Target, err)
	}
	draft := inputRecipe{Research: &inputResearchRecipe{Version: 1}}
	if err := validateDialectDraft("Draft", draft); err != nil || draft.Research.Target != "records" {
		t.Fatalf("draft default: %v", err)
	}
	recipe.CompanyIDs = []string{"reviewed-company"}
	if err := validateInputResearch(&recipe); err != nil {
		t.Fatal(err)
	}
	entity := inputResearchEntity{Kind: "record", Name: "Contact", URL: "https://example.com/team", Fields: map[string]inputResearchValue{"country": {Status: "missing"}}}
	urls := map[string]bool{entity.URL: true}
	companies := map[string]bool{"reviewed-company": true}
	if validateResearchEntity(&entity, recipe, nil, urls, companies) == nil {
		t.Fatal("missing selected-company relationship accepted")
	}
	entity.CompanyID = "invented"
	if validateResearchEntity(&entity, recipe, nil, urls, companies) == nil {
		t.Fatal("invented selected-company relationship accepted")
	}
	entity.CompanyID = "reviewed-company"
	if err := validateResearchEntity(&entity, recipe, nil, urls, companies); err != nil {
		t.Fatal(err)
	}
}

func TestGenericRecipeReprocessesLegacyIdentityWithoutCreatingLegacyKinds(t *testing.T) {
	s, rr, original := researchV2Fixture(t)
	original = v2Apply(t, rr, v2Record(original, "Example"))
	if err := s.publishResearchRecords(original); err != nil {
		t.Fatal(err)
	}
	saved := v2Rows(t, s, original.PreparationID)
	row := saved.Document.Records[0]
	next := original
	next.Recipe.Target = "records"
	next.Records = nil
	next.Outcomes = nil
	next.Attempts = nil
	next.Existing = researchRecordSnapshots(saved.Document)
	next.Reprocess = true
	next.Selection = next.Existing
	next.Existing[0].SourceIDs = []string{next.Sources[0].ID}
	if !researchKindAllowed(next.Recipe.Target, next.Existing[0].Entity.Kind) {
		t.Fatal("legacy row refused for generic reprocessing")
	}
	candidate := v2Record(next, "Example")
	candidate["recordId"] = row.ID
	raw, _ := json.Marshal(map[string]any{"records": []any{candidate}})
	if err := applyResearchRecordsV2(&next, raw); err != nil {
		t.Fatal(err)
	}
	if len(next.Records) != 1 || next.Records[0].ID != row.Research.Entity.ID || next.Records[0].Kind != "company" {
		t.Fatalf("legacy identity lost: %+v", next.Outcomes)
	}
	next.Reprocess = false
	next.Records = nil
	next.Outcomes = nil
	delete(candidate, "recordId")
	raw, _ = json.Marshal(map[string]any{"records": []any{candidate}})
	_ = applyResearchRecordsV2(&next, raw)
	if len(next.Records) != 0 {
		t.Fatal("new company-kind record accepted without existing row scope")
	}
}
