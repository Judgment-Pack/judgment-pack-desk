package desk

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInputPreparationImportsRetainProvidedLineage(t *testing.T) {
	s, _ := newTestServer(t, false)
	call := func(method, path, body, revision string) (int, []byte) {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+testToken)
		if revision != "" {
			r.Header.Set("If-Match", revision)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code, w.Body.Bytes()
	}
	code, raw := call("GET", "/api/input-preparations", "", "")
	if code != 200 || !strings.Contains(string(raw), `"items":[]`) {
		t.Fatalf("list %d %s", code, raw)
	}
	if _, err := os.Stat(filepath.Join(s.projectDir, inputPreparationDir)); !os.IsNotExist(err) {
		t.Fatal("listing created storage")
	}
	code, raw = call("POST", "/api/input-preparations", `{"name":"Supplier intake","recipe":{"intent":"Prepare a supplier list"}}`, "")
	var created inputPreparationReply
	if code != 201 || json.Unmarshal(raw, &created) != nil {
		t.Fatalf("create %d %s", code, raw)
	}
	path := "/api/input-preparations/" + created.Document.ID
	body := `{"kind":"csv","name":"suppliers.csv","content":"Name,Employees,Domain\nExample,00123,example.org\nExample,unknown,example.org\n"}`
	code, raw = call("POST", path+"/imports", body, created.SHA256)
	var imported inputPreparationReply
	if code != 200 || json.Unmarshal(raw, &imported) != nil {
		t.Fatalf("import %d %s", code, raw)
	}
	doc := imported.Document
	if len(doc.Records) != 2 || doc.Records[0].Values["Employees"] != "00123" || doc.Records[0].Class != "asserted" || doc.Records[0].ID == doc.Records[1].ID {
		t.Fatalf("invented types, trust or identity: %+v", doc.Records)
	}
	if doc.Imports[0].Content != "Name,Employees,Domain\nExample,00123,example.org\nExample,unknown,example.org\n" || doc.Imports[0].Digest != "sha256:"+digestOf([]byte(doc.Imports[0].Content)) {
		t.Fatal("lost original source bytes")
	}
	code, raw = call("POST", path+"/imports", body, imported.SHA256)
	var repeated inputPreparationReply
	json.Unmarshal(raw, &repeated)
	if code != 200 || repeated.SHA256 != imported.SHA256 || len(repeated.Document.Records) != 2 {
		t.Fatal("retry duplicated an import")
	}
	code, _ = call("PUT", path, `{"name":"Changed","recipe":{"intent":"Changed criteria"}}`, created.SHA256)
	if code != 409 {
		t.Fatal("stale recipe edit accepted")
	}
	code, raw = call("PUT", path, `{"name":"Changed","recipe":{"intent":"Changed criteria"}}`, imported.SHA256)
	var changed inputPreparationReply
	json.Unmarshal(raw, &changed)
	if code != 200 || changed.Document.RecipeVersion != 2 || changed.Document.Imports[0].RecipeVersion != 1 || changed.Document.Records[0].ID != doc.Records[0].ID {
		t.Fatal("recipe change relabeled historical input")
	}
	if len(changed.Document.Recipes) != 2 || changed.Document.Recipes[0].Recipe.Intent != "Prepare a supplier list" || changed.Document.Recipes[1].Recipe.Intent != "Changed criteria" {
		t.Fatal("recipe history lost original instructions")
	}
	if info, err := os.Stat(filepath.Join(s.projectDir, inputPreparationDir, doc.ID+".json")); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("records are not private")
	}
	for _, body := range []string{`{"name":"a","name":"b","recipe":{"intent":""}}`, `{"name":"a","recipe":{"intent":"one","Intent":"two"}}`, `{"name":"a","recipe":{"intent":""},"records":[{"class":"record"}]}`} {
		code, _ = call("POST", "/api/input-preparations", body, "")
		if code != 400 {
			t.Fatal("ambiguous or forged preparation accepted")
		}
	}
	before, _ := os.ReadFile(filepath.Join(s.projectDir, inputPreparationDir, doc.ID+".json"))
	code, _ = call("POST", path+"/imports", `{"kind":"csv","name":"bad.csv","content":"name,name\na,b\n"}`, changed.SHA256)
	if code != 422 {
		t.Fatal("ambiguous columns accepted")
	}
	after, _ := os.ReadFile(filepath.Join(s.projectDir, inputPreparationDir, doc.ID+".json"))
	if string(before) != string(after) {
		t.Fatal("failed import partially wrote data")
	}
	// A prepared record never enters the ordinary project file writer.
	code, _ = call("GET", "/api/file?path="+inputPreparationDir+"/"+doc.ID+".json", "", "")
	if code == 200 {
		t.Fatal("private inputs exposed as authored project file")
	}
}
func TestInputImportKeepsUnresolvedDataAndBounds(t *testing.T) {
	rows, err := normalizeInputImport("urls", "https://EXAMPLE.org/a#one\nhttps://example.org/a#two\n", "source")
	if err != nil || len(rows) != 1 || rows[0].Status != "needs-research" || rows[0].Values["url"] != "https://example.org/a" {
		t.Fatalf("URL seeds %+v %v", rows, err)
	}
	for _, bad := range []string{"example.org", "file:///etc/passwd", "https://user:secret@example.org", "javascript:alert(1)"} {
		if _, err := normalizeInputImport("urls", bad, "source"); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	for _, bad := range []string{"name,name\na,b", "name,age\na,b,c", "name\n" + strings.Repeat("a\n", 1001)} {
		if _, err := normalizeInputImport("csv", bad, "source"); err == nil {
			t.Fatal("bad or excessive import accepted")
		}
	}
	rows, err = normalizeInputImport("csv", "Name,Comment\r\n\"Quoted, name\",\"line one\nline two\"\r\n", "source")
	if err != nil || rows[0].Values["Comment"] != "line one\nline two" {
		t.Fatalf("quoted CSV %v %v", rows, err)
	}
}

func TestInputPreparationGuardsConcurrentEditsAndPrivateFiles(t *testing.T) {
	s, _ := newTestServer(t, false)
	request := func(method, path, body, revision, origin string, authenticated bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if authenticated {
			r.Header.Set("Authorization", "Bearer "+testToken)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		r.Header.Set("If-Match", revision)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	for _, path := range []string{"/api/input-preparations", "/api/input-preparations/prep_" + strings.Repeat("a", 32) + "/imports"} {
		if got := request("POST", path, `{}`, "", "", false).Code; got != 401 {
			t.Fatalf("unauthenticated write: %d", got)
		}
		if got := request("POST", path, `{}`, "", "https://untrusted.invalid", true).Code; got != 403 {
			t.Fatalf("cross-origin write: %d", got)
		}
	}
	created := request("POST", "/api/input-preparations", `{"name":"Original","recipe":{"intent":"Review supplied data"}}`, "", "", true)
	var saved inputPreparationReply
	if created.Code != 201 || json.Unmarshal(created.Body.Bytes(), &saved) != nil {
		t.Fatalf("create: %d", created.Code)
	}
	path := "/api/input-preparations/" + saved.Document.ID
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		go func() {
			codes <- request("PUT", path, `{"name":"Revised","recipe":{"intent":"Updated criteria"}}`, saved.SHA256, "", true).Code
		}()
	}
	first, second := <-codes, <-codes
	if !((first == 200 && second == 409) || (first == 409 && second == 200)) {
		t.Fatalf("concurrent edits: %d, %d", first, second)
	}
	current := request("GET", path, "", "", "", true)
	var edited inputPreparationReply
	if json.Unmarshal(current.Body.Bytes(), &edited) != nil || edited.Document.RecipeVersion != 2 {
		t.Fatal("concurrent edits overwrote a revision")
	}
	other, _ := newTestServer(t, false)
	r := httptest.NewRequest("GET", path, nil)
	r.Header.Set("Authorization", "Bearer "+testToken)
	w := httptest.NewRecorder()
	other.ServeHTTP(w, r)
	if w.Code == 200 {
		t.Fatal("preparation escaped its project scope")
	}
	// A file swapped for a symlink cannot be read through the private API.
	file := filepath.Join(s.projectDir, inputPreparationDir, saved.Document.ID+".json")
	target := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(target, current.Body.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, file); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if got := request("GET", path, "", "", "", true).Code; got != 409 {
		t.Fatalf("symlink read: %d", got)
	}
	if got := request("GET", "/api/input-preparations", "", "", "", true).Code; got != 409 {
		t.Fatalf("unreadable storage reported as an empty list: %d", got)
	}
}

func TestInputChatSaveIsIdempotentAndNeverStartsResearch(t *testing.T) {
	s, _ := newTestServer(t, false)
	call := func(method, path, body, revision string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+testToken)
		if revision != "" {
			r.Header.Set("If-Match", revision)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	recipe := researchRecipeFixture()
	body, _ := json.Marshal(map[string]any{"name": "Owners", "recipe": inputRecipe{Intent: "Find owners", Research: &recipe}, "chatOrigin": map[string]string{"chatId": "chat-1", "draftId": "draft-1"}})
	first := call("POST", "/api/input-preparations", string(body), "")
	if first.Code != 201 {
		t.Fatalf("create %d %s", first.Code, first.Body.String())
	}
	var saved inputPreparationReply
	if err := json.Unmarshal(first.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	again := call("POST", "/api/input-preparations", string(body), "")
	if again.Code != 200 || again.Body.String() != first.Body.String() {
		t.Fatalf("retry did not return original: %d %s", again.Code, again.Body.String())
	}
	changed := call("PUT", "/api/input-preparations/"+saved.Document.ID, `{"name":"Edited later","recipe":{"intent":"Updated"}}`, saved.SHA256)
	if changed.Code != 200 {
		t.Fatal(changed.Body.String())
	}
	again = call("POST", "/api/input-preparations", string(body), "")
	if again.Code != 200 || !strings.Contains(again.Body.String(), "Edited later") {
		t.Fatal("replayed chat save replaced a newer input")
	}
	conflict := call("POST", "/api/input-preparations", strings.Replace(string(body), "Find owners", "Find executives", 1), "")
	if conflict.Code != 409 {
		t.Fatal("changed save reused an existing proposal key")
	}
	list := call("GET", "/api/input-preparations", "", "")
	var listed struct {
		Items []json.RawMessage `json:"items"`
	}
	json.Unmarshal(list.Body.Bytes(), &listed)
	if len(listed.Items) != 1 {
		t.Fatal("duplicate inputs created")
	}
	runs := call("GET", "/api/input-preparations/"+saved.Document.ID+"/research", "", "")
	if runs.Code != 200 || !strings.Contains(runs.Body.String(), `"items":[]`) {
		t.Fatal("saving started research")
	}
	for _, origin := range []string{`{"chatId":"../escape","draftId":"draft-2"}`, `{"chatId":"chat-1","draftId":"draft-2","requestDigest":"forged"}`} {
		result := call("POST", "/api/input-preparations", `{"name":"Bad","recipe":{"intent":"test"},"chatOrigin":`+origin+`}`, "")
		if result.Code != 400 {
			t.Fatal("invalid origin accepted")
		}
	}
}
