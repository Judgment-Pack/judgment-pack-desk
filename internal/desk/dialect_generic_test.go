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
