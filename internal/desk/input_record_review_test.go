package desk

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInputRecordReviewRetainsSnapshotAndDoesNotEvaluate(t *testing.T) {
	s, _ := newTestServer(t, false)
	root, e := s.openInputPreparations(true)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	_, run := researchRunFixture(t)
	doc := inputPreparation{Version: 1, ID: run.PreparationID, Name: "Research", RecipeVersion: 1, Recipe: inputRecipe{Intent: "Find companies", Research: &run.Recipe}}
	if _, e = writeInputPreparation(root, doc); e != nil {
		t.Fatal(e)
	}
	run.Records = []inputResearchEntity{{ID: "entity-company", Kind: "company", Name: "Example", URL: "https://example.com/", Fields: map[string]inputResearchValue{"country": {Value: json.RawMessage(`"Canada"`), Status: "interpretation"}}}}
	if e = s.publishResearchRecords(run); e != nil {
		t.Fatal(e)
	}
	saved, e := readInputPreparation(root, doc.ID)
	if e != nil {
		t.Fatal(e)
	}
	original := saved.Document.Records[0]
	path := "/api/input-preparations/" + doc.ID
	call := func(method, suffix, body, revision string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path+suffix, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+policyTestSession(t, s))
		r.Header.Set("If-Match", revision)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	reviewBody := `{"recordId":"` + original.ID + `","decision":"accepted","note":"Check fit separately"}`
	res := call("POST", "/record-reviews", reviewBody, saved.SHA256)
	var reviewed inputPreparationReply
	if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &reviewed) != nil {
		t.Fatalf("review: %d %s", res.Code, res.Body.String())
	}
	row := reviewed.Document.Records[0]
	if row.Class != "generated" || row.Status != "researched" || inputRecordDigest(row) != inputRecordDigest(original) || row.Review == nil || row.Review.Decision != "accepted" || len(reviewed.Document.Reviews) != 1 || reviewed.Document.RecipeVersion != 1 {
		t.Fatal("review mutated source data or lost its binding")
	}
	if inputRecordDigest(reviewed.Document.Reviews[0].Record) != inputRecordDigest(original) || reviewed.Document.Reviews[0].Record.Review != nil {
		t.Fatal("review snapshot differs from original")
	}
	if _, e = prepareInputRecord(reviewed.Document, row); e == nil {
		t.Fatal("accepted AI record entered asserted evaluation path")
	}
	repeat := call("POST", "/record-reviews", reviewBody, reviewed.SHA256)
	var again inputPreparationReply
	json.Unmarshal(repeat.Body.Bytes(), &again)
	if repeat.Code != 200 || again.SHA256 != reviewed.SHA256 {
		t.Fatal("unchanged review duplicated history")
	}
	if call("POST", "/record-reviews", reviewBody, saved.SHA256).Code != 409 {
		t.Fatal("stale review accepted")
	}
	for _, body := range []string{
		strings.Replace(reviewBody, "accepted", "verified", 1),
		strings.Replace(reviewBody, original.ID, "missing", 1),
		strings.TrimSuffix(reviewBody, "}") + `,"reviewer":{"subject":"forged"}}`,
		strings.TrimSuffix(reviewBody, "}") + `,"class":"asserted"}`,
		strings.Replace(reviewBody, "Check fit separately", strings.Repeat("x", 2001), 1),
	} {
		if call("POST", "/record-reviews", body, reviewed.SHA256).Code != 422 {
			t.Fatal("invalid review accepted")
		}
	}
	// Session guard and origin checks apply to this route as to every input edit.
	for _, authenticated := range []bool{false, true} {
		r := httptest.NewRequest("POST", path+"/record-reviews", strings.NewReader(reviewBody))
		r.Header.Set("If-Match", reviewed.SHA256)
		want := 401
		if authenticated {
			r.Header.Set("Authorization", "Bearer "+testToken)
			r.Header.Set("Origin", "https://untrusted.invalid")
			want = 403
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("guard: %d", w.Code)
		}
	}
	if e = s.publishResearchRecords(run); e != nil {
		t.Fatal(e)
	}
	republished, _ := readInputPreparation(root, doc.ID)
	if republished.SHA256 != reviewed.SHA256 {
		t.Fatal("unchanged research publication erased review")
	}
	// A revised entity gets no inherited approval. Earlier bytes remain in history.
	run.Records[0].Name = "Example revised"
	if e = s.publishResearchRecords(run); e != nil {
		t.Fatal(e)
	}
	changed, _ := readInputPreparation(root, doc.ID)
	if changed.Document.Records[0].Review != nil || len(changed.Document.Reviews) != 1 || changed.Document.Reviews[0].Record.Name != "Example" {
		t.Fatal("changed research inherited acceptance or lost history")
	}
	res = call("POST", "/record-reviews", strings.Replace(reviewBody, "accepted", "needs-information", 1), changed.SHA256)
	if res.Code != 200 {
		t.Fatal(res.Body.String())
	}
	json.Unmarshal(res.Body.Bytes(), &changed)
	// A recipe edit leaves the earlier decision attached to its old version.
	recipeBody, _ := json.Marshal(map[string]any{"name": doc.Name, "recipe": inputRecipe{Intent: "Changed scope", Research: &run.Recipe}})
	res = call("PUT", "", string(recipeBody), changed.SHA256)
	if res.Code != 200 {
		t.Fatal(res.Body.String())
	}
	json.Unmarshal(res.Body.Bytes(), &changed)
	if changed.Document.RecipeVersion != 2 || changed.Document.Records[0].Review.RecipeVersion != 1 || len(changed.Document.Reviews) != 2 {
		t.Fatal("recipe edit upgraded an old review")
	}
	if res = call("GET", "/evaluations", "", ""); res.Code != 200 || !strings.Contains(res.Body.String(), `"items":[]`) {
		t.Fatal("review created evaluation")
	}
}

func TestInputRecordReviewConcurrentDecisionsHaveOneWinner(t *testing.T) {
	s, _ := newTestServer(t, false)
	root, e := s.openInputPreparations(true)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	doc := inputPreparation{Version: 1, ID: "prep_" + strings.Repeat("c", 32), RecipeVersion: 1, Records: []preparedInputRecord{{ID: "row", Class: "generated", Research: &inputResearchRecord{RunID: "run"}}}}
	saved, e := writeInputPreparation(root, doc)
	if e != nil {
		t.Fatal(e)
	}
	codes := make(chan int, 2)
	for _, decision := range []string{"accepted", "rejected"} {
		go func(decision string) {
			r := httptest.NewRequest("POST", "/api/input-preparations/"+doc.ID+"/record-reviews", strings.NewReader(`{"recordId":"row","decision":"`+decision+`","note":""}`))
			r.Header.Set("Authorization", "Bearer "+testToken)
			r.Header.Set("If-Match", saved.SHA256)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			codes <- w.Code
		}(decision)
	}
	a, b := <-codes, <-codes
	if !(a == 200 && b == 409 || a == 409 && b == 200) {
		t.Fatalf("concurrent reviews: %d %d", a, b)
	}
	saved, e = readInputPreparation(root, doc.ID)
	if e != nil || len(saved.Document.Reviews) != 1 {
		t.Fatal("concurrent reviews overwrote history", e)
	}
}

func TestInputRecordBulkReviewIsAtomicAndPreservesOrigins(t *testing.T) {
	s, _ := newTestServer(t, false)
	root, err := s.openInputPreparations(true)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	doc := inputPreparation{Version: 1, ID: "prep_" + strings.Repeat("b", 32), RecipeVersion: 3, Records: []preparedInputRecord{}}
	for _, id := range []string{"first", "second", "unselected"} {
		doc.Records = append(doc.Records, preparedInputRecord{ID: id, Name: id, Class: "generated", Status: "researched", Research: &inputResearchRecord{RunID: "run"}})
	}
	doc.Records = append(doc.Records, preparedInputRecord{ID: "supplied", Class: "asserted"})
	original, err := writeInputPreparation(root, doc)
	if err != nil {
		t.Fatal(err)
	}
	call := func(body, revision string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/input-preparations/"+doc.ID+"/record-reviews", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+testToken)
		r.Header.Set("If-Match", revision)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	body := func(ids []string) string {
		raw, _ := json.Marshal(map[string]any{"recordIds": ids, "decision": "accepted", "note": "Reviewed evidence"})
		return string(raw)
	}
	invalid := []string{
		body([]string{"first", "missing"}), body([]string{"first", "supplied"}), body([]string{"first", "first"}),
		body([]string{"first", ""}), body([]string{}), body(make([]string, inputRecordLimit+1)),
		`{"recordIds":["first"],"recordId":"first","decision":"accepted"}`,
		`{"recordIds":["first","second"],"decision":"verified"}`,
		`{"recordIds":["first"],"decision":"accepted","reviewer":{"subject":"forged"}}`,
	}
	for _, request := range invalid {
		if response := call(request, original.SHA256); response.Code != 422 {
			t.Fatalf("invalid batch: %d %s", response.Code, response.Body)
		}
		retained, e := readInputPreparation(root, doc.ID)
		if e != nil || retained.SHA256 != original.SHA256 {
			t.Fatal("invalid batch partially changed records", e)
		}
	}
	response := call(body([]string{"first", "second"}), original.SHA256)
	var reviewed inputPreparationReply
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &reviewed) != nil {
		t.Fatalf("bulk review: %d %s", response.Code, response.Body)
	}
	if reviewed.Document.RecipeVersion != 3 || len(reviewed.Document.Reviews) != 2 || reviewed.Document.Records[2].Review != nil || reviewed.Document.Records[3].Review != nil {
		t.Fatal("bulk review changed its scope or recipe")
	}
	for i := 0; i < 2; i++ {
		row := reviewed.Document.Records[i]
		if row.Review == nil || row.Review.Decision != "accepted" || row.Review.RecipeVersion != 3 || row.Review.Note != "Reviewed evidence" || row.Class != "generated" || inputRecordDigest(row) != inputRecordDigest(doc.Records[i]) {
			t.Fatal("bulk review changed research or lost its review binding")
		}
		event := reviewed.Document.Reviews[i]
		if event.Record.Review != nil || event.Review.RecordDigest != inputRecordDigest(event.Record) {
			t.Fatal("bulk review lost its individual history snapshot")
		}
	}
	if reviewed.Document.Records[0].Review.At != reviewed.Document.Records[1].Review.At {
		t.Fatal("batch timestamps differ")
	}
	repeat := call(body([]string{"first", "second"}), reviewed.SHA256)
	var unchanged inputPreparationReply
	if repeat.Code != 200 || json.Unmarshal(repeat.Body.Bytes(), &unchanged) != nil || unchanged.SHA256 != reviewed.SHA256 {
		t.Fatal("unchanged bulk review duplicated history")
	}
	if response := call(body([]string{"first", "second"}), original.SHA256); response.Code != 409 {
		t.Fatal("stale bulk review accepted")
	}
	if response := call(body([]string{"first", "second", "unselected"}), reviewed.SHA256); response.Code != 200 {
		t.Fatal(response.Body)
	}
	retained, e := readInputPreparation(root, doc.ID)
	if e != nil || len(retained.Document.Reviews) != 3 {
		t.Fatal("repeat members duplicated history", e)
	}
	r := httptest.NewRequest("GET", "/api/input-preparations/"+doc.ID+"/evaluations", nil)
	r.Header.Set("Authorization", "Bearer "+policyTestSession(t, s))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatal("bulk review created evaluation")
	}
}

func TestInputRecordBulkAndSingleReviewCannotPartiallyOverwrite(t *testing.T) {
	s, _ := newTestServer(t, false)
	root, err := s.openInputPreparations(true)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	doc := inputPreparation{Version: 1, ID: "prep_" + strings.Repeat("d", 32), RecipeVersion: 1, Records: []preparedInputRecord{
		{ID: "first", Class: "generated", Research: &inputResearchRecord{RunID: "run"}},
		{ID: "second", Class: "generated", Research: &inputResearchRecord{RunID: "run"}},
	}}
	saved, err := writeInputPreparation(root, doc)
	if err != nil {
		t.Fatal(err)
	}
	codes := make(chan int, 2)
	for _, body := range []string{`{"recordIds":["first","second"],"decision":"accepted"}`, `{"recordId":"first","decision":"rejected"}`} {
		go func(body string) {
			r := httptest.NewRequest("POST", "/api/input-preparations/"+doc.ID+"/record-reviews", strings.NewReader(body))
			r.Header.Set("Authorization", "Bearer "+testToken)
			r.Header.Set("If-Match", saved.SHA256)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			codes <- w.Code
		}(body)
	}
	a, b := <-codes, <-codes
	if !(a == 200 && b == 409 || a == 409 && b == 200) {
		t.Fatalf("concurrent review responses: %d %d", a, b)
	}
	result, err := readInputPreparation(root, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	first, second := result.Document.Records[0].Review, result.Document.Records[1].Review
	if first == nil {
		t.Fatal("winning review missing")
	}
	if first.Decision == "accepted" {
		if second == nil || second.Decision != "accepted" || len(result.Document.Reviews) != 2 {
			t.Fatal("winning batch was partially saved")
		}
	} else if first.Decision != "rejected" || second != nil || len(result.Document.Reviews) != 1 {
		t.Fatal("losing batch left partial changes")
	}
}
