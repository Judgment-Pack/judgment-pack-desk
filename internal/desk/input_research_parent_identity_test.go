package desk

import (
	"encoding/json"
	"testing"
)

func TestResearchParentIdentityRetainsSamePersonForTwoSelectedParents(t *testing.T) {
	s, rr, run := researchV2Fixture(t)
	run.IdentityVersion = 2
	run.RelationshipVersion = 1
	run.Recipe.Target = "records"
	run.Companies = []inputResearchEntity{{ID: "company-a", Name: "A", URL: "https://a.example/"}, {ID: "company-b", Name: "B", URL: "https://b.example/"}}
	if err := writeInputResearch(rr, run); err != nil {
		t.Fatal(err)
	}
	makeRecord := func(parent string) map[string]any {
		r := v2Record(run, "Example")
		r["kind"] = "record"
		r["companyId"] = parent
		return r
	}
	run = v2Apply(t, rr, makeRecord("company-a"), makeRecord("company-b"))
	if len(run.Records) != 2 || len(run.Outcomes) != 2 || run.Records[0].ID == run.Records[1].ID || run.Outcomes[0].Key == run.Outcomes[1].Key {
		t.Fatalf("selected parents collapsed: %+v %+v", run.Records, run.Outcomes)
	}
	if err := s.publishResearchRecords(run); err != nil {
		t.Fatal(err)
	}
	saved := v2Rows(t, s, run.PreparationID)
	if len(saved.Document.Records) != 2 {
		t.Fatalf("lost a published relationship: %+v", saved.Document.Records)
	}
	parents := map[string]bool{}
	for _, row := range saved.Document.Records {
		parents[row.Values["company_id"]] = true
	}
	if !parents["company-a"] || !parents["company-b"] {
		t.Fatal("host-owned parent links lost", parents)
	}
	// A refresh/replayed publication must not add, overwrite or reparent rows.
	if err := s.publishResearchRecords(run); err != nil {
		t.Fatal(err)
	}
	if v2Rows(t, s, run.PreparationID).SHA256 != saved.SHA256 {
		t.Fatal("publication replay changed rows")
	}
	run, _ = readInputResearch(rr)
	run.Existing = researchRecordSnapshots(saved.Document)
	run.Records = nil
	run.Outcomes = nil
	if err := writeInputResearch(rr, run); err != nil {
		t.Fatal(err)
	}
	moved := makeRecord("company-b")
	moved["recordId"] = saved.Document.Records[0].ID
	run = v2Apply(t, rr, moved)
	if len(run.Records) != 0 || len(run.Outcomes) != 1 || run.Outcomes[0].State != "needs-correction" {
		t.Fatal("explicit update moved a result to another parent", run.Outcomes)
	}
}

func TestResearchParentIdentityKeepsDifferentRecordsFromSamePage(t *testing.T) {
	_, rr, run := researchV2Fixture(t)
	run.IdentityVersion = 2
	run.Recipe.Target = "records"
	if err := writeInputResearch(rr, run); err != nil {
		t.Fatal(err)
	}
	a, b := v2Record(run, "Example"), v2Record(run, "Another")
	a["kind"] = "record"
	b["kind"] = "record"
	run = v2Apply(t, rr, a, b)
	if len(run.Records) != 2 || run.Records[0].ID == run.Records[1].ID {
		t.Fatal("distinct names on one page collapsed", run.Records)
	}
}

func TestResearchParentIdentityPreservesLegacyReplay(t *testing.T) {
	entity := inputResearchEntity{Kind: "person", Name: "Example", URL: "https://example.com/person", CompanyID: "company-a"}
	for _, version := range []int{0, 1} {
		run := inputResearchRun{IdentityVersion: version}
		if researchEntityKeyForRun(run, entity) != researchEntityKey(entity) {
			t.Fatal("historical key changed", version)
		}
		other := entity
		other.CompanyID = "company-b"
		if researchSameIdentityForRun(run, entity, other) != researchSameIdentity(entity, other) {
			t.Fatal("historical replay changed", version)
		}
	}
	run := inputResearchRun{IdentityVersion: 2}
	other := entity
	other.CompanyID = "company-b"
	if researchEntityKeyForRun(run, entity) == researchEntityKeyForRun(run, other) || researchSameIdentityForRun(run, entity, other) {
		t.Fatal("new parent-aware identity collapsed")
	}
	// The version survives storage encoding and does not depend on current defaults.
	raw, _ := json.Marshal(run)
	var restored inputResearchRun
	if json.Unmarshal(raw, &restored) != nil || restored.IdentityVersion != 2 {
		t.Fatal("identity policy not retained")
	}
}

func TestResearchParentIdentityProjectsParentForExplicitUpdates(t *testing.T) {
	run := inputResearchRun{IdentityVersion: 2, Existing: []researchRecordSnapshot{
		{RecordID: "row-a", Entity: inputResearchEntity{Kind: "record", Name: "Example", URL: "https://example.com/person", CompanyID: "company-a"}},
		{RecordID: "row-b", Entity: inputResearchEntity{Kind: "record", Name: "Example", URL: "https://example.com/person", CompanyID: "company-b"}},
	}}
	projected := researchExistingContext(run)
	if len(projected) != 2 || projected[0]["companyId"] != "company-a" || projected[1]["companyId"] != "company-b" || projected[0]["recordId"] != "row-a" || projected[1]["recordId"] != "row-b" {
		t.Fatal("the prompt cannot identify each existing row's parent", projected)
	}
}
