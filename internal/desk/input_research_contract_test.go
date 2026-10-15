package desk

import (
	"encoding/json"
	"strings"
	"testing"
)

func researchRecipeFixture() inputResearchRecipe {
	return inputResearchRecipe{Version: 1, Target: "companies", Fields: []inputResearchField{{Name: "country", Description: "Country of operations", Type: "string"}}, Criteria: []inputResearchCriterion{}, AI: inputResearchAI{Connection: "legacy-codex", Revision: "revision", Model: "test-model"}, Limits: inputResearchLimits{Records: 25, Pages: 30, ModelCalls: 30}}
}
func TestInputResearchTargetsAndLimits(t *testing.T) {
	for _, target := range []string{"companies", "people", "people-at-companies"} {
		r := researchRecipeFixture()
		r.Target = target
		if target == "people-at-companies" {
			r.CompanyIDs = []string{"company-1"}
		}
		if e := validateInputResearch(&r); e != nil {
			t.Fatal(target, e)
		}
	}
	changes := []func(*inputResearchRecipe){func(r *inputResearchRecipe) { r.Target = "anything" }, func(r *inputResearchRecipe) { r.Target = "people-at-companies" }, func(r *inputResearchRecipe) { r.Limits.Records = 51 }, func(r *inputResearchRecipe) { r.Limits.Searches = 1 }, func(r *inputResearchRecipe) { r.AI.Revision = "" }, func(r *inputResearchRecipe) { r.Fields = append(r.Fields, r.Fields[0]) }, func(r *inputResearchRecipe) { r.Fields[0].Type = "object" }}
	for i, change := range changes {
		r := researchRecipeFixture()
		change(&r)
		if validateInputResearch(&r) == nil {
			t.Fatalf("accepted invalid recipe %d", i)
		}
	}
}
func TestInputResearchEvidenceCannotBeInventedOrPromoted(t *testing.T) {
	recipe := researchRecipeFixture()
	source := inputResearchSource{ID: "page-1", Kind: "page", Text: "Our Canadian team is based in Toronto.", Verification: "verified"}
	entity := inputResearchEntity{Kind: "company", Name: "Example", URL: "https://example.com", Fields: map[string]inputResearchValue{"country": {Value: json.RawMessage(`"Canada"`), Status: "supported", Sources: []inputResearchCitation{{SourceID: source.ID, Quote: "Canadian team"}}}}}
	sources := map[string]inputResearchSource{source.ID: source}
	allowed := map[string]bool{"https://example.com/": true}
	if e := validateResearchEntity(&entity, recipe, sources, allowed, nil); e != nil {
		t.Fatal(e)
	}
	original := entity.ID
	entity.URL = "https://www.example.com/"
	allowed[entity.URL] = true
	if e := validateResearchEntity(&entity, recipe, sources, allowed, nil); e != nil || entity.ID != original {
		t.Fatal("company URL identity drift", e)
	}
	value := entity.Fields["country"]
	value.Sources[0].Quote = "US headquarters"
	entity.Fields["country"] = value
	if validateResearchEntity(&entity, recipe, sources, allowed, nil) == nil {
		t.Fatal("invented quotation accepted")
	}
	value.Sources[0].Quote = "Canadian team"
	entity.Fields["country"] = value
	source.Kind = "search"
	sources[source.ID] = source
	if validateResearchEntity(&entity, recipe, sources, allowed, nil) == nil {
		t.Fatal("search snippet promoted to page support")
	}
	value.Status = "interpretation"
	entity.Fields["country"] = value
	if e := validateResearchEntity(&entity, recipe, sources, allowed, nil); e != nil {
		t.Fatal(e)
	}
	entity.URL = "https://invented.example/"
	if validateResearchEntity(&entity, recipe, sources, allowed, nil) == nil {
		t.Fatal("invented URL accepted")
	}
}
func TestInputResearchPeopleRequireReviewedCompanyAndMissingRemainsUnknown(t *testing.T) {
	r := researchRecipeFixture()
	r.Target = "people-at-companies"
	r.CompanyIDs = []string{"company-1"}
	e := inputResearchEntity{Kind: "person", Name: "Person", URL: "https://example.com/team/person", CompanyID: "company-2", Fields: map[string]inputResearchValue{"country": {Status: "missing", Sources: []inputResearchCitation{}}}}
	urls := map[string]bool{e.URL: true}
	companies := map[string]bool{"company-1": true}
	if validateResearchEntity(&e, r, nil, urls, companies) == nil {
		t.Fatal("unreviewed company accepted")
	}
	e.CompanyID = "company-1"
	if err := validateResearchEntity(&e, r, nil, urls, companies); err != nil {
		t.Fatal(err)
	}
	v := e.Fields["country"]
	v.Value = json.RawMessage(`"Canada"`)
	e.Fields["country"] = v
	if validateResearchEntity(&e, r, nil, urls, companies) == nil {
		t.Fatal("missing value fabricated")
	}
}

func TestInputResearchExternalCompanySelectionIsBounded(t *testing.T) {
	recipe := researchRecipeFixture()
	recipe.Target = "people-at-companies"
	ref := inputResearchCompanyRef{PreparationID: "prep_" + strings.Repeat("a", 32), Revision: strings.Repeat("b", 64), EntityID: "company-1"}
	recipe.CompanyInputs = []inputResearchCompanyRef{ref}
	if err := validateInputResearch(&recipe); err != nil {
		t.Fatal(err)
	}
	recipe.CompanyInputs = append(recipe.CompanyInputs, ref)
	if validateInputResearch(&recipe) == nil {
		t.Fatal("duplicate company accepted")
	}
	recipe.CompanyInputs = []inputResearchCompanyRef{ref}
	recipe.CompanyInputs[0].PreparationID = "../outside"
	if validateInputResearch(&recipe) == nil {
		t.Fatal("invalid input reference accepted")
	}
}

func TestInputResearchExternalCompanySnapshotRejectsStaleOrInventedSelection(t *testing.T) {
	s, _ := newTestServer(t, false)
	root, err := s.openInputPreparations(true)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	company := inputResearchEntity{ID: "company-1", Name: "Example", Kind: "company", URL: "https://example.org/", Fields: map[string]inputResearchValue{}}
	doc := inputPreparation{Version: 1, ID: "prep_" + strings.Repeat("a", 32), Name: "Companies", RecipeVersion: 1, Recipe: inputRecipe{Intent: "Find companies"}, Records: []preparedInputRecord{{ID: "row", Class: "generated", Research: &inputResearchRecord{Entity: company}}}}
	saved, err := writeInputPreparation(root, doc)
	if err != nil {
		t.Fatal(err)
	}
	recipe := researchRecipeFixture()
	recipe.Target = "people-at-companies"
	recipe.CompanyInputs = []inputResearchCompanyRef{{PreparationID: doc.ID, Revision: saved.SHA256, EntityID: company.ID}}
	companies, err := selectedResearchCompanies(root, inputPreparation{}, recipe)
	if err != nil || len(companies) != 1 || companies[0].Name != "Example" {
		t.Fatalf("selection %v %v", companies, err)
	}
	doc.Records[0].Research.Entity.Name = "Changed"
	if _, err = writeInputPreparation(root, doc); err != nil {
		t.Fatal(err)
	}
	if _, err = selectedResearchCompanies(root, inputPreparation{}, recipe); err == nil {
		t.Fatal("stale company selection accepted")
	}
	if companies[0].Name != "Example" {
		t.Fatal("reviewed snapshot changed")
	}
	updated, err := readInputPreparation(root, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	recipe.CompanyInputs[0].Revision = updated.SHA256
	recipe.CompanyInputs[0].EntityID = "invented"
	if _, err = selectedResearchCompanies(root, inputPreparation{}, recipe); err == nil {
		t.Fatal("invented company accepted")
	}
}
