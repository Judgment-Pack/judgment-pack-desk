package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	exchangeCatalog "github.com/Judgment-Pack/judgment-pack-desk/internal/exchange"
)

func typedAdmissionVector(t *testing.T) (json.RawMessage, json.RawMessage, string) {
	t.Helper()
	raw, err := os.ReadFile("testdata/typed-admission-v1.json")
	assistantRunMust(t, err)
	var vector struct {
		Facts    json.RawMessage `json:"facts"`
		Evidence json.RawMessage `json:"evidence"`
		Pack     json.RawMessage `json:"pack"`
	}
	assistantRunMust(t, json.Unmarshal(raw, &vector))
	return vector.Facts, vector.Evidence, string(vector.Pack)
}

// This synthetic Runtime only records dispatch and returns the declared test
// document. These are Desk admission tests, not Runtime semantic conformance.
func typedAdmissionRuntime(t *testing.T, document, pack string) (string, string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("requires a shell fixture")
	}
	dir := t.TempDir()
	bin, calls, retained := filepath.Join(dir, "runtime"), filepath.Join(dir, "calls"), filepath.Join(dir, "pack")
	contract := `{"outputVersion":"2","contractVersion":"1","command":"dialect contract","status":"available","capabilities":{"formatVersions":[],"validationVersions":[2],"packCheckVersions":[2]},"diagnostics":[]}`
	report := `{"outputVersion":"2","contractVersion":"1","command":"dialect check","status":"complete","dialectVersion":2,"document":` + strconvJSON(document) + `,"digest":"sha256:` + digestOf([]byte(document)) + `","diagnostics":[],"compatibility":{"scope":"declared-inventory-coverage","pack":{"sha256":"` + digestOf([]byte(pack)) + `"}}}`
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	script := "#!/bin/sh\nprintf '%s\\n' \"$2\" >> " + quote(calls) + "\nif [ \"$2\" = contract ]; then printf '%s' " + quote(contract) + "; exit 0; fi\ncat pack.json > " + quote(retained) + "\nprintf '%s' " + quote(report) + "\n"
	assistantRunMust(t, os.WriteFile(bin, []byte(script), 0700))
	return bin, calls, retained
}

func strconvJSON(s string) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}

func typedAdmissionCall(s *Server, method, path, revision string, raw []byte, tokens ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, bytes.NewReader(raw))
	token := testToken
	if len(tokens) > 0 {
		token = tokens[0]
	}
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("If-Match", revision)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func TestTypedAdmissionQualificationHTTPAndReload(t *testing.T) {
	_, _, pack := typedAdmissionVector(t)
	q := &inputQualification{Kind: "pack", Key: "typed", Pack: pack, Digest: "sha256:" + digestOf([]byte(pack)), Mapping: inputRecordDialect{Version: 2, Case: &inputCaseDialect{Facts: []inputDialectFact{{"/Fact", "/values/Fact"}, {"/fact", "/values/fact"}}, Evidence: []inputDialectEvidence{}}, Sources: []json.RawMessage{}}, Conversions: map[string]inputConversion{"/Fact": {Kind: "boolean", TrueValue: "true", FalseValue: "false"}, "/fact": {Kind: "number"}}}
	document, err := json.Marshal(q.Mapping)
	assistantRunMust(t, err)
	bin, calls, retained := typedAdmissionRuntime(t, string(document), pack)
	s, _ := newTestServer(t, false)
	s.cfg.JpackBin = bin
	body, err := json.Marshal(map[string]any{"name": "Typed records", "recipe": inputRecipe{Qualification: q}})
	assistantRunMust(t, err)
	w := typedAdmissionCall(s, "POST", "/api/input-preparations", "", body)
	if w.Code != 201 {
		t.Fatal("case-distinct qualification refused", w.Code, w.Body.String())
	}
	var saved inputPreparationReply
	assistantRunMust(t, json.Unmarshal(w.Body.Bytes(), &saved))
	passed, err := os.ReadFile(retained)
	if err != nil || string(passed) != pack || saved.Document.Recipe.Qualification.Pack != pack {
		t.Fatal("retained pack bytes changed", err)
	}
	path := "/api/input-preparations/" + saved.Document.ID
	w = typedAdmissionCall(s, "POST", path+"/imports", saved.SHA256, []byte(`{"kind":"csv","name":"facts.csv","content":"Fact,fact,Blank\nfalse,0,\n"}`))
	if w.Code != 200 {
		t.Fatal("case-distinct import refused", w.Code, w.Body.String())
	}
	assistantRunMust(t, json.Unmarshal(w.Body.Bytes(), &saved))
	if saved.Document.Records[0].Values["Fact"] != "false" || saved.Document.Records[0].Values["fact"] != "0" {
		t.Fatal("case-distinct original values changed")
	}
	preview, _ := json.Marshal(map[string]any{"qualification": q})
	w = typedAdmissionCall(s, "POST", path+"/conversion-preview", saved.SHA256, preview, policyTestSession(t, s))
	var result struct {
		Rows []inputPreparedRow `json:"rows"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Rows) != 1 || result.Rows[0].Prepared["/Fact"] != false || result.Rows[0].Prepared["/fact"] != float64(0) {
		t.Fatal("qualification path lost typed case-distinct data", w.Code, w.Body.String())
	}
	if _, exists := result.Rows[0].Prepared["/Blank"]; exists {
		t.Fatal("omitted mapping invented a value")
	}
	stored, err := s.preparationRepository().Get(t.Context(), saved.Document.ID)
	assistantRunMust(t, err)
	if stored.Document.Recipe.Qualification.Pack != pack || len(stored.Document.Recipe.Qualification.Conversions) != 2 || stored.Document.Records[0].Values["Fact"] != "false" || stored.Document.Records[0].Values["fact"] != "0" {
		t.Fatal("reloaded qualification lost data or exact pack")
	}
	file := filepath.Join(s.projectDir, inputPreparationDir, saved.Document.ID+".json")
	prior, err := os.ReadFile(file)
	assistantRunMust(t, err)
	dispatched, err := os.ReadFile(calls)
	assistantRunMust(t, err)
	for _, invalid := range [][]byte{
		bytes.Replace(body, []byte(`"name":`), []byte(`"Name":`), 1),
		bytes.Replace(body, []byte(`"recipe":`), []byte(`"Recipe":`), 1),
		bytes.Replace(body, []byte(`"conversions":`), []byte(`"Conversions":`), 1),
		bytes.Replace(body, []byte(`"kind":"number"`), []byte(`"Kind":"number"`), 1),
		bytes.Replace(body, []byte(`"kind":"number"`), []byte(`"kind":"number","kind":"text"`), 1),
		bytes.Replace(body, []byte(`"kind":"number"`), []byte(`"kind":"number","k\u0069nd":"text"`), 1),
		bytes.Replace(body, []byte(`"name":`), []byte(`"unknown":false,"name":`), 1),
		[]byte(`{"name":"\ud800"}`),
		append([]byte(`{"name":"`), bytes.Repeat([]byte("x"), 2<<20)...),
		[]byte(`{"name":` + strings.Repeat("[", 66) + `0` + strings.Repeat("]", 66) + `}`),
	} {
		w := typedAdmissionCall(s, "PUT", path, saved.SHA256, invalid)
		if w.Code != 400 {
			t.Fatal("malformed request/control admitted", w.Code, w.Body.String())
		}
		after, err := os.ReadFile(file)
		assistantRunMust(t, err)
		currentCalls, err := os.ReadFile(calls)
		assistantRunMust(t, err)
		if !bytes.Equal(prior, after) || !bytes.Equal(dispatched, currentCalls) {
			t.Fatal("failed write changed saved state or dispatched Runtime")
		}
	}
	for _, invalid := range []string{
		strings.Replace(pack, `"id":`, `"id":"other","ID":`, 1),
		strings.Replace(pack, `"Fact":false,"fact":null`, `"fact":false,"f\u0061ct":null`, 1),
		strings.Replace(pack, `"literal":"50"`, `"literal":"\ud800"`, 1),
	} {
		bad := *q
		bad.Pack, bad.Digest = invalid, "sha256:"+digestOf([]byte(invalid))
		body, _ := json.Marshal(map[string]any{"name": "Typed records", "recipe": inputRecipe{Qualification: &bad}})
		w := typedAdmissionCall(s, "PUT", path, saved.SHA256, body)
		if w.Code != 422 {
			t.Fatal("invalid qualification pack accepted", w.Code, w.Body.String())
		}
		after, err := os.ReadFile(file)
		assistantRunMust(t, err)
		currentCalls, err := os.ReadFile(calls)
		assistantRunMust(t, err)
		if !bytes.Equal(prior, after) || !bytes.Equal(dispatched, currentCalls) {
			t.Fatal("invalid qualification pack wrote or dispatched Runtime")
		}
	}
}

type typedAdmissionArtifacts struct {
	retainedArtifactRepository
	puts int
}

func (a *typedAdmissionArtifacts) Put(ctx context.Context, raw []byte) (retainedArtifactRef, error) {
	a.puts++
	return a.retainedArtifactRepository.Put(ctx, raw)
}

func TestTypedAdmissionPolicyIntentBytesAndRecovery(t *testing.T) {
	f := newExecutionFixture(t)
	facts, evidence, _ := typedAdmissionVector(t)
	for index, value := range []string{string(facts), `null`, `false`, `0`, `50`, `"50"`} {
		key := string(rune('a' + index))
		raw := `{"facts":` + value + `,"evidence":` + string(evidence) + `}`
		intent, _ := f.intent(t, key, raw)
		kept, err := f.core.artifacts.Get(t.Context(), intent.Request)
		assistantRunMust(t, err)
		keptFacts, err := f.core.artifacts.Get(t.Context(), intent.Facts)
		assistantRunMust(t, err)
		if string(kept) != raw || string(keptFacts) != value || intent.Evidence == nil {
			t.Fatal("Policy admission changed case-distinct typed bytes")
		}
		keptEvidence, err := f.core.artifacts.Get(t.Context(), *intent.Evidence)
		if err != nil || !bytes.Equal(keptEvidence, evidence) {
			t.Fatal("Policy evidence bytes changed", err)
		}
		before := f.raw(t)
		fresh := f.core
		recovered, delivery, err := fresh.prepareIntent(t.Context(), intent.Resource, intent.Registration, key, []byte(raw))
		if err != nil || delivery != "recovered" || !samePolicyStatusValue(recovered, intent) || !bytes.Equal(before, f.raw(t)) {
			t.Fatal("persisted intent recovery changed exact data", delivery, err)
		}
	}
	for index, raw := range []string{`{"facts":false}`, `{"facts":false,"evidence":null}`} {
		intent, _ := f.intent(t, string(rune('j'+index)), raw)
		if (intent.Evidence == nil) != (index == 0) {
			t.Fatal("omitted and null evidence collapsed")
		}
	}
	tracker := &typedAdmissionArtifacts{retainedArtifactRepository: f.core.artifacts}
	f.core.artifacts = tracker
	authorizations := 0
	f.core.authorize = func() bool { authorizations++; return true }
	before := f.raw(t)
	for _, raw := range []string{
		`{"Facts":false}`, `{"facts":false,"Facts":true}`, `{"facts":false,"extra":0}`, `{}`,
		`{"facts":{"fact":0,"fact":1}}`, `{"facts":{"fact":0,"f\u0061ct":1}}`,
		`{"facts":false,"facts":null}`, `{"facts":false,"f\u0061cts":null}`, `{"facts":"\ud800"}`,
		`{"facts":` + strings.Repeat("[", 66) + `0` + strings.Repeat("]", 66) + `}`,
		`{"facts":"` + strings.Repeat("x", 2<<20) + `"}`,
		string([]byte{'{', '"', 'f', 'a', 'c', 't', 's', '"', ':', '"', 0xff, '"', '}'}),
	} {
		if _, _, err := f.core.prepareIntent(t.Context(), f.registration.Resource, f.registration.Registration, "refused", []byte(raw)); err == nil {
			t.Fatal("malformed Policy input admitted")
		}
		if authorizations != 0 || tracker.puts != 0 || !bytes.Equal(before, f.raw(t)) {
			t.Fatal("malformed Policy input authorized, wrote artifacts or changed ledger")
		}
	}
}

func TestTypedAdmissionPackControls(t *testing.T) {
	_, _, pack := typedAdmissionVector(t)
	if _, err := readInputPack([]byte(pack)); err != nil {
		t.Fatal("case-distinct pack data refused", err)
	}
	for _, raw := range []string{
		strings.Replace(pack, `"id":`, `"ID":`, 1),
		strings.Replace(pack, `"version":`, `"Version":`, 1),
		strings.Replace(pack, `"id":`, `"id":"other","ID":`, 1),
		strings.Replace(pack, `"Fact":false,"fact":null`, `"fact":false,"fact":null`, 1),
		strings.Replace(pack, `"Fact":false,"fact":null`, `"fact":false,"f\u0061ct":null`, 1),
		strings.Replace(pack, `"literal":"50"`, `"literal":"\ud800"`, 1),
		`{"id":` + strings.Repeat("[", 66) + `0` + strings.Repeat("]", 66) + `}`,
	} {
		if _, err := readInputPack([]byte(raw)); err == nil {
			t.Fatal("malformed pack/control admitted")
		}
	}
}

func TestTypedAdmissionExchangeTargetHTTP(t *testing.T) {
	_, _, pack := typedAdmissionVector(t)
	const document = `{"version":2,"case":{"facts":[],"evidence":[]}}`
	s, _ := newTestServer(t, false)
	bin, calls, retained := typedAdmissionRuntime(t, document, pack)
	s.cfg.JpackBin = bin
	candidate := exchangeCatalog.Candidate{Row: exchangeCatalog.Row{ID: "typed.fixture"}, Entry: exchangeCatalog.Preset{Pack: &exchangeCatalog.PackReference{ID: "https://example.invalid/typed-admission", Digests: []string{"sha256:" + digestOf([]byte(pack))}}}, Document: document, Digest: "sha256:" + digestOf([]byte(document))}
	review, raw, err := encodeExchangeReview(exchangeReviewed{Base: "retained", Release: exchangeCatalog.Release{Location: "catalog", Tag: "v1.0.0", Commit: strings.Repeat("a", 40), Candidates: []exchangeCatalog.Candidate{candidate}}})
	assistantRunMust(t, err)
	ref, err := s.artifactRepository().Put(t.Context(), raw)
	assistantRunMust(t, err)
	record := exchangeImport{ID: digestOf([]byte(review.ID + "\n" + candidate.Row.ID)), EntryID: candidate.Row.ID, Location: "catalog", Tag: "v1.0.0", Commit: review.Release.Commit, Review: ref, At: "2030-01-01T00:00:00Z"}
	root, err := s.exchangeRoot()
	assistantRunMust(t, err)
	defer root.Close()
	history, err := json.Marshal(exchangeHistory{Version: 1, Imports: []exchangeImport{record}})
	assistantRunMust(t, err)
	assistantRunMust(t, writePrivateData(root, "history.json", history))
	path := "/api/exchange/imports/" + record.ID + "/check"
	body, _ := json.Marshal(map[string]string{"pack": pack, "document": document})
	w := typedAdmissionCall(s, "POST", path, "", body)
	if w.Code != 200 {
		t.Fatal("case-distinct Exchange pack refused", w.Code, w.Body.String())
	}
	passed, err := os.ReadFile(retained)
	if err != nil || string(passed) != pack {
		t.Fatal("Exchange rewrote retained pack bytes", err)
	}
	dispatched, err := os.ReadFile(calls)
	assistantRunMust(t, err)
	for _, invalid := range []string{
		strings.Replace(pack, `"id":`, `"ID":`, 1),
		strings.Replace(pack, `"version":"1.0.0"`, `"version":1`, 1),
		strings.Replace(pack, `"id":`, `"id":"other","ID":`, 1),
		strings.Replace(pack, `"Fact":false,"fact":null`, `"fact":false,"f\u0061ct":null`, 1),
		strings.Replace(pack, `"literal":"50"`, `"literal":"\ud800"`, 1),
		strings.Replace(pack, `https://example.invalid/typed-admission`, `https://example.invalid/other`, 1),
	} {
		body, _ := json.Marshal(map[string]string{"pack": invalid, "document": document})
		w := typedAdmissionCall(s, "POST", path, "", body)
		if w.Code != 422 {
			t.Fatal("invalid Exchange pack accepted", w.Code, w.Body.String())
		}
		current, err := os.ReadFile(calls)
		assistantRunMust(t, err)
		kept, err := readPrivateData(root, "history.json", exchangeHistoryLimit)
		if err != nil || !bytes.Equal(current, dispatched) || !bytes.Equal(history, kept) {
			t.Fatal("invalid Exchange target dispatched or changed import history", err)
		}
	}
	body = bytes.Replace(body, []byte(`"pack":`), []byte(`"Pack":`), 1)
	if w := typedAdmissionCall(s, "POST", path, "", body); w.Code != 400 {
		t.Fatal("Exchange request control alias admitted", w.Code)
	}
}
