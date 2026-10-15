package desk

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

const qualificationTestPack = `{"id":"example","version":"1.0.0","decision":{"question":"Ready?"},"rules":[{"when":{"op":"fact","path":"/company/country","value":"CA"}}],"evidenceRequirements":[{"id":"license"}]}`

func qualificationFixture() *inputQualification {
	return &inputQualification{Kind: "pack", Key: "example", Pack: qualificationTestPack, Digest: "sha256:" + digestOf([]byte(qualificationTestPack)), Mapping: inputRecordDialect{Version: 2, Case: &inputCaseDialect{Facts: []inputDialectFact{{"/company/country", "/values/Country"}}, Evidence: []inputDialectEvidence{{"license", "/values/License"}}}, Sources: []json.RawMessage{}}}
}
func TestInputQualificationRefusesUnboundOrGeneratedMappings(t *testing.T) {
	if e := validateInputQualification(qualificationFixture()); e != nil {
		t.Fatal(e)
	}
	for _, scenario := range []string{"digest", "graph", "generated", "undeclared fact", "undeclared evidence", "duplicate target", "omitted assigned", "outside values", "invalid pointer", "prototype", "bad pack", "bad evidence shape", "bad title"} {
		t.Run(scenario, func(t *testing.T) {
			q := qualificationFixture()
			switch scenario {
			case "digest":
				q.Digest = "sha256:forged"
			case "graph":
				q.Kind = "graph"
			case "generated":
				q.Mapping.Sources = []json.RawMessage{json.RawMessage(`{"class":"generated"}`)}
			case "undeclared fact":
				q.Mapping.Case.Facts[0].Target = "/unlisted"
			case "undeclared evidence":
				q.Mapping.Case.Evidence[0].Requirement = "other"
			case "duplicate target":
				q.Mapping.Case.Facts = append(q.Mapping.Case.Facts, q.Mapping.Case.Facts[0])
			case "omitted assigned":
				q.Mapping.Unmapped = []string{"/company/country"}
			case "outside values":
				q.Mapping.Case.Facts[0].Source = "/origin/sourceDigest"
			case "invalid pointer":
				q.Mapping.Case.Facts[0].Source = "/values/~bad"
			case "prototype":
				q.Mapping.Case.Facts[0].Source = "/values/__proto__/x"
			case "bad evidence shape":
				q.Pack = strings.Replace(qualificationTestPack, `[{"id":"license"}]`, `"not an array"`, 1)
				q.Digest = "sha256:" + digestOf([]byte(q.Pack))
			case "bad title":
				q.Pack = strings.Replace(qualificationTestPack, `"version":"1.0.0"`, `"version":"1.0.0","title":{}`, 1)
				q.Digest = "sha256:" + digestOf([]byte(q.Pack))
			case "bad pack":
				q.Pack = `{"id":"one","id":"two","decision":{}}`
				q.Digest = "sha256:" + digestOf([]byte(q.Pack))
			}
			if validateInputQualification(q) == nil {
				t.Fatal("unsafe binding accepted")
			}
		})
	}
}
func TestInputRecipeRetainsBoundPackAndMappingsAcrossEdits(t *testing.T) {
	s, _ := newTestServer(t, false)
	call := func(method, path string, recipe inputRecipe, sha string) (int, inputPreparationReply) {
		raw, _ := json.Marshal(map[string]any{"name": "Companies", "recipe": recipe})
		r := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
		r.Header.Set("Authorization", "Bearer "+testToken)
		r.Header.Set("If-Match", sha)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		var result inputPreparationReply
		json.Unmarshal(w.Body.Bytes(), &result)
		return w.Code, result
	}
	q := qualificationFixture()
	code, created := call("POST", "/api/input-preparations", inputRecipe{Intent: "Find companies", Qualification: q}, "")
	if code != 201 {
		t.Fatal(code)
	}
	q.Mapping.Case.Facts[0].Source = "/values/Region"
	path := "/api/input-preparations/" + created.Document.ID
	code, changed := call("PUT", path, inputRecipe{Intent: "Changed research", Qualification: q}, created.SHA256)
	if code != 200 {
		t.Fatal(code)
	}
	if changed.Document.Recipes[0].Recipe.Qualification.Mapping.Case.Facts[0].Source != "/values/Country" || changed.Document.Recipe.Qualification.Mapping.Case.Facts[0].Source != "/values/Region" {
		t.Fatal("mapping history overwritten")
	}
	if changed.Document.Recipes[0].Recipe.Qualification.Pack != qualificationTestPack || changed.Document.Recipe.Qualification.Digest != created.Document.Recipe.Qualification.Digest {
		t.Fatal("pack snapshot lost")
	}
	code, _ = call("PUT", path, inputRecipe{Qualification: q}, created.SHA256)
	if code != 409 {
		t.Fatal("stale recipe binding accepted")
	}
	q.Digest = "forged"
	code, _ = call("PUT", path, inputRecipe{Qualification: q}, changed.SHA256)
	if code != 422 {
		t.Fatal("mismatched pack bytes accepted")
	}
	code, removed := call("PUT", path, inputRecipe{Intent: "Unbound"}, changed.SHA256)
	if code != 200 || removed.Document.Recipe.Qualification != nil || removed.Document.Recipes[1].Recipe.Qualification == nil {
		t.Fatal("removing current binding damaged history")
	}
}
