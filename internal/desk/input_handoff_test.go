package desk

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const handoffProofSourceID = "attempt_eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"

// The same executable identity must serve both trusted companion operations in
// integration tests, just as it does in the installed Desk binary.
func TestMain(m *testing.M) {
	if len(os.Args) == 2 {
		var err error
		switch os.Args[1] {
		case "--model-source-adapter":
			err = RunModelSourceAdapter(os.Stdin, os.Stdout)
		case "--input-source-adapter":
			err = RunInputSourceAdapter(os.Stdin, os.Stdout)
		case "--local-gateway-worker":
			err = RunLocalGatewayWorker(os.Stdin, os.Stdout)
		default:
			os.Exit(m.Run())
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func TestReviewedInputAdapterBoundsAndBinding(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	root, err := openPrivateDataRoot(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	t.Setenv("JPACK_INPUT_HANDOFFS", root.Name())
	result := json.RawMessage(`{"class":"generated","prepared":{"/country":"CA"}}`)
	grant := inputHandoff{Expires: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano), Digest: sha256Digest(result), Result: result}
	id := "grant-" + strings.Repeat("a", 32)
	write := func() {
		raw, _ := json.Marshal(grant)
		if err := writePrivateData(root, id+".json", raw); err != nil {
			t.Fatal(err)
		}
	}
	write()
	request := func(grantID, digest string) string {
		raw, _ := json.Marshal(map[string]any{"tool": reviewedInputTool, "arguments": map[string]string{"grant": grantID, "digest": digest}})
		return string(raw)
	}
	var out bytes.Buffer
	valid := request(id, grant.Digest)
	if err = RunInputSourceAdapter(strings.NewReader(valid), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"class":"generated"`) || !strings.Contains(out.String(), reviewedInputEndpoint) {
		t.Fatal("origin lost")
	}
	for _, bad := range []string{request("../"+id, grant.Digest), request(id, "sha256:wrong"), strings.Replace(valid, reviewedInputTool, "evaluate", 1), strings.TrimSuffix(valid, "}") + `,"path":"/tmp/secret"}`, strings.Repeat("x", 4097)} {
		if RunInputSourceAdapter(strings.NewReader(bad), &out) == nil {
			t.Fatal("invalid request admitted")
		}
	}
	grant.Result = json.RawMessage(`{"class":"asserted"}`)
	write()
	if RunInputSourceAdapter(strings.NewReader(valid), &out) == nil {
		t.Fatal("changed result admitted")
	}
	grant.Result = result
	grant.Expires = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	write()
	if RunInputSourceAdapter(strings.NewReader(valid), &out) == nil {
		t.Fatal("expired grant admitted")
	}
}
func reviewedFixture(t *testing.T, s *Server, q *inputQualification) (inputPreparationReply, inputResearchRun) {
	t.Helper()
	_, run := researchRunFixture(t)
	run.State = "completed"
	run.Seeds = []string{"https://example.com/"}
	run.Recipe.Fields = []inputResearchField{{Name: "country", Type: "string", Description: "Country"}}
	args := json.RawMessage(`{"records":[{"kind":"company","name":"Example","url":"https://example.com/","fields":{"country":{"value":"CA","status":"interpretation","sources":[]}}}]}`)
	var retainedProof []byte
	if bundle := os.Getenv("JPS_TEST_GATEWAY_BUNDLE"); bundle != "" {
		var source inputResearchSource
		source, retainedProof, run.Gateway = handoffProofFixture(t, filepath.Join(bundle, "gateway"))
		run.Sources = []inputResearchSource{source}
		args = json.RawMessage(strings.ReplaceAll(`{"records":[{"kind":"company","name":"Example","url":"https://example.com/","fields":{"country":{"value":"CA","status":"supported","sources":[{"sourceId":"attempt_page","quote":"Example operates in CA."}]}}}]}`, "attempt_page", handoffProofSourceID))
	}
	if err := applyResearchRecords(&run, args); err != nil {
		t.Fatal(err)
	}
	result, _ := json.Marshal(inputResearchAction{Tool: "records", Arguments: args})
	run.Attempts = []inputResearchAttempt{{ID: "attempt_model", Kind: "model", State: "completed", Result: result, FinishedAt: time.Now().UTC().Format(time.RFC3339Nano)}}
	root, err := s.inputResearchRoot(true)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	rr, err := openInputResearch(root, run.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	defer rr.Close()
	if retainedProof != nil {
		if err = writePrivateData(rr, handoffProofSourceID+".proof.json", retainedProof); err != nil {
			t.Fatal(err)
		}
	}
	if err = writeInputResearch(rr, run); err != nil {
		t.Fatal(err)
	}
	doc := inputPreparation{Version: 1, ID: run.PreparationID, Name: "Research", RecipeVersion: 1, Recipe: inputRecipe{Intent: "Find companies", Research: &run.Recipe, Qualification: q}, CreatedAt: run.CreatedAt, UpdatedAt: run.CreatedAt}
	pr, err := s.openInputPreparations(true)
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Close()
	if _, err = writeInputPreparation(pr, doc); err != nil {
		t.Fatal(err)
	}
	if err = s.publishResearchRecords(run); err != nil {
		t.Fatal(err)
	}
	saved, err := readInputPreparation(pr, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	return saved, run
}
func acceptFixture(t *testing.T, s *Server, saved inputPreparationReply) inputPreparationReply {
	t.Helper()
	raw, _ := json.Marshal(map[string]string{"recordId": saved.Document.Records[0].ID, "decision": "accepted", "note": "Fixture only"})
	r := httptest.NewRequest("POST", "/api/input-preparations/"+saved.Document.ID+"/record-reviews", bytes.NewReader(raw))
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("If-Match", saved.SHA256)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &saved) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	return saved
}
func TestReviewedInputSelectionRefusesStaleExcludedAndMissingReviews(t *testing.T) {
	s, _ := newTestServer(t, false)
	saved, _ := reviewedFixture(t, s, qualificationFixture())
	id := saved.Document.Records[0].ID
	if _, err := prepareInputSelection(saved.Document, []string{id}); err == nil {
		t.Fatal("unreviewed admitted")
	}
	saved = acceptFixture(t, s, saved)
	if _, err := prepareInputSelection(saved.Document, []string{id}); err != nil {
		t.Fatal(err)
	}
	for _, decision := range []string{"needs-information", "rejected"} {
		doc := saved.Document
		row := doc.Records[0]
		r := *row.Review
		r.Decision = decision
		row.Review = &r
		doc.Records = []preparedInputRecord{row}
		if _, err := prepareInputSelection(doc, []string{id}); err == nil {
			t.Fatal("nonready admitted")
		}
	}
	stale := saved.Document
	stale.RecipeVersion++
	if _, err := prepareInputSelection(stale, []string{id}); err == nil {
		t.Fatal("stale admitted")
	}
	missing := saved.Document
	missing.Reviews = nil
	if _, err := prepareInputSelection(missing, []string{id}); err == nil {
		t.Fatal("missing history admitted")
	}
	mixed := saved.Document
	mixed.Records = append(mixed.Records, preparedInputRecord{ID: "supplied", Class: "asserted"})
	if _, err := prepareInputSelection(mixed, []string{id, "supplied"}); err == nil {
		t.Fatal("mixed selection silently skipped")
	}
}
func TestReviewedInputRealGatewayRunner(t *testing.T) { reviewedInputRealGatewayRunner(t, false) }
func TestPublishedReviewedInputRealGatewayRunner(t *testing.T) {
	reviewedInputRealGatewayRunner(t, true)
}
func reviewedInputRealGatewayRunner(t *testing.T, published bool) {
	bundle, bin, runtime := os.Getenv("JPS_TEST_GATEWAY_BUNDLE"), os.Getenv("JPACK_RUNNER_TEST_BIN"), os.Getenv("JPACK_BIN")
	if bundle == "" || bin == "" || runtime == "" {
		t.Skip("requires pinned companions")
	}
	project, config := t.TempDir(), t.TempDir()
	os.Chmod(config, 0700)
	q := qualificationFixture()
	pack, err := os.ReadFile("testdata/graphs/sanctions-screening-0.1.0.pack.json")
	if err != nil {
		t.Fatal(err)
	}
	pack = []byte(strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(string(pack), "/screening/matches", "/company/country"), `"value": "0"`, `"value": "CA"`), `"value": "1"`, `"value": "US"`), "greater-than-or-equal", "equals"))
	var packDoc map[string]any
	json.Unmarshal(pack, &packDoc)
	packDoc["evidenceRequirements"] = []any{}
	pack, _ = json.Marshal(packDoc)
	q.Pack = string(pack)
	q.Digest = sha256Digest(pack)
	q.Mapping.Case.Evidence = []inputDialectEvidence{}
	q.Mapping.Case.Facts = []inputDialectFact{{Target: "/company/country", Source: "/values/country"}}
	writeProject(t, project, map[string]string{"pack.json": q.Pack, "jpack.json": `{"configVersion":"2","packs":{"` + q.Key + `":{"path":"pack.json"}}}`})
	s, ts := startDesk(t, Config{RunnerBin: bin, JpackBin: runtime, LocalGatewayBundle: bundle, Static: os.DirFS("../../web/dist"), LocalAccess: true, ProjectDir: project, DeskConfigDir: config, Token: testToken, RunnerAllowUntestedReleases: true})
	defer ts.Close()
	defer s.Close()
	saved, run := reviewedFixture(t, s, q)
	saved = acceptFixture(t, s, saved)
	if published {
		checkAndPublishDialect(t, s, saved, "generated", "1.0.0")
	}
	if script := os.Getenv("JPS_INPUT_REVIEW_BROWSER"); script != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "/home/onword/.nvm/versions/node/v22.23.1/bin/node", script, ts.URL, saved.Document.ID, testToken)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("browser: %s %v", out, err)
		} else {
			t.Log(string(out))
		}
	}
	id := saved.Document.Records[0].ID
	rows, err := prepareInputSelection(saved.Document, []string{id})
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.inputResearchRoot(false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	rr, err := openInputResearch(root, run.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	defer rr.Close()
	altered := run
	altered.Attempts = nil
	writeInputResearch(rr, altered)
	if _, err = s.reviewedInputSnapshot(context.Background(), saved.Document, saved.Document.Records[0], rows[0]); err == nil {
		t.Fatal("missing model response admitted")
	}
	writeInputResearch(rr, run)
	proof, err := readPrivateData(rr, handoffProofSourceID+".proof.json", inputResearchRecordLimit)
	if err != nil {
		t.Fatal(err)
	}
	writePrivateData(rr, handoffProofSourceID+".proof.json", bytes.ReplaceAll(proof, []byte("Example operates in CA."), []byte("Modified source text.")))
	if _, err = s.reviewedInputSnapshot(context.Background(), saved.Document, saved.Document.Records[0], rows[0]); err == nil {
		t.Fatal("modified page proof admitted")
	}
	writePrivateData(rr, handoffProofSourceID+".proof.json", proof)
	call := func(suffix string, body any, revision string) (int, []byte) {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", "/api/input-preparations/"+saved.Document.ID+suffix, bytes.NewReader(raw))
		r.Header.Set("Authorization", "Bearer "+testToken)
		r.Header.Set("If-Match", revision)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code, w.Body.Bytes()
	}
	code, raw := call("/prepare", map[string]any{"recordIds": []string{id}}, saved.SHA256)
	if code != 200 {
		t.Fatal(code, string(raw))
	}
	var prepared struct {
		Rows []inputPreparedRow `json:"rows"`
	}
	json.Unmarshal(raw, &prepared)
	row := prepared.Rows[0]
	if row.Origin != "generated" || len(row.Input) == 0 {
		t.Fatal("no generated handoff")
	}
	preview, err := s.inputRunner(context.Background(), "inputs/preview", row.Input, "")
	if err != nil {
		t.Fatal("Runner rejected signed research", err)
	}
	if !strings.Contains(string(preview), `"class":"generated"`) || !strings.Contains(string(preview), `"generatedInfluence":true`) {
		t.Fatal("lineage lost", string(preview))
	}
	// Alter a prepared value while retaining the receipt: admission must fail.
	changed := bytes.ReplaceAll(row.Input, []byte(`"CA"`), []byte(`"US"`))
	if _, err = s.inputRunner(context.Background(), "inputs/preview", json.RawMessage(changed), ""); err == nil {
		t.Fatal("tampered source admitted")
	}
	code, raw = call("/evaluations", map[string]any{"recordIds": []string{id}}, saved.SHA256)
	if code != 201 {
		t.Fatal("review", code, string(raw))
	}
	var batch inputEvaluation
	json.Unmarshal(raw, &batch)
	if batch.ConfirmedAt != "" || batch.JobID != "" {
		t.Fatal("review started a job")
	}
	code, raw = call("/evaluations/"+batch.ID+"/confirm", map[string]bool{"reviewed": true}, "stale")
	if code != 409 {
		t.Fatal("stale confirmation admitted")
	}
	code, raw = call("/evaluations/"+batch.ID+"/confirm", map[string]bool{"reviewed": true}, saved.SHA256)
	if code != 202 {
		t.Fatal("confirm", code, string(raw))
	}
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); {
		root, err := s.inputEvaluationRoot(false)
		if err != nil {
			t.Fatal(err)
		}
		dir, err := openInputEvaluation(root, batch.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		batch, err = readInputEvaluation(dir)
		dir.Close()
		root.Close()
		if err != nil {
			t.Fatal(err)
		}
		if batch.State == "completed" || batch.State == "partial" || batch.State == "blocked" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if batch.State != "completed" {
		t.Fatalf("did not complete: %+v", batch)
	}
	exported, err := s.inputRunner(context.Background(), "runs/"+batch.Rows[0].RunID+"/verification", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{`"class":"generated"`, run.ID, `"reviewer"`, `"accepted"`, `"modelResponses"`} {
		if !strings.Contains(string(exported), part) {
			t.Fatal("export lost origin/review", part)
		}
	}
	verification := s.checkRunSignature(context.Background(), batch.Rows[0].RunID)
	if verification.Status != "verified" || verification.References != "verified" {
		t.Fatal("record verification", verification)
	}
	entries, err := os.ReadDir(filepath.Join(s.assistant.dir, "input-handoffs"))
	if err != nil || len(entries) != 0 {
		t.Fatal("grants not removed", err)
	}
}

func TestGeneratedConversionsKeepUnresolvedFieldsUnknown(t *testing.T) {
	q := qualificationFixture()
	q.Mapping.Case.Facts = []inputDialectFact{{"/count", "/values/count"}, {"/flag", "/values/flag"}, {"/stale", "/values/stale"}, {"/missing", "/values/missing"}}
	q.Conversions = map[string]inputConversion{"/count": {Kind: "number"}, "/flag": {Kind: "boolean", TrueValue: "true", FalseValue: "false"}}
	record := preparedInputRecord{ID: "row", Class: "generated", Values: map[string]string{"count": "0", "flag": "false", "stale": "old", "missing": ""}, Research: &inputResearchRecord{Entity: inputResearchEntity{Fields: map[string]inputResearchValue{"count": {Status: "interpretation"}, "flag": {Status: "interpretation"}, "stale": {Status: "stale"}, "missing": {Status: "missing"}}}}}
	row, err := prepareInputRecord(inputPreparation{Recipe: inputRecipe{Qualification: q}}, record)
	if err != nil || len(row.Issues) != 0 || row.Prepared["/count"] != json.Number("0") || row.Prepared["/flag"] != false || len(row.Prepared) != 2 || len(row.Input) != 0 {
		t.Fatal("unknowns or generated classification changed", row, err)
	}
	if _, err = prepareInputSelection(inputPreparation{Recipe: inputRecipe{Qualification: q}, Records: []preparedInputRecord{record}}, []string{"row"}); err == nil {
		t.Fatal("conversion preview authorized evaluation")
	}
}

// Constructed page acquisition with a test-only signer; no website is fetched.
func handoffProofFixture(t *testing.T, program string) (inputResearchSource, []byte, inputResearchPin) {
	return handoffProofFixtureFor(t, program, "https://example.com/", "Example operates in CA.")
}
func handoffProofFixtureFor(t *testing.T, program, sourceURL, sourceText string) (inputResearchSource, []byte, inputResearchPin) {
	t.Helper()
	ctx := context.Background()
	key := ed25519.NewKeyFromSeed(make([]byte, 32))
	public := key.Public().(ed25519.PublicKey)
	keyID := digestOf(public)[:32]
	canon := func(v any) []byte {
		raw, _ := json.Marshal(v)
		out, err := researchCanon(ctx, program, raw)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	args := json.RawMessage(canon(map[string]string{"url": sourceURL}))
	result := json.RawMessage(canon(map[string]any{"attachmentVersion": "1", "content": map[string]any{"pages": []any{map[string]any{"text": sourceText, "status": "ok", "extraction": "verbatim"}}}, "processing": map[string]any{"status": "complete"}, "provenance": map[string]any{"source": map[string]any{"kind": "web", "url": sourceURL, "requestedUrl": sourceURL}}}))
	salt := make([]byte, 32)
	commitment := append(append(salt, []byte("args:")...), canon(args)...)
	stamp := time.Now().UTC().Format(time.RFC3339)
	receipt := map[string]any{"receiptVersion": "3", "kind": "acquisition", "sessionId": "research-test", "callIndex": 0, "prevSignature": nil, "source": "web", "authority": "gateway:test", "keyId": keyID, "resultDigest": sha256Digest(canon(result)), "argumentsCommitment": sha256Digest(commitment), "caller": nil, "servedAt": stamp, "acquisition": map[string]any{"shape": "http", "endpoint": sourceURL, "adapter": map[string]any{"name": "test-adapter", "version": "1", "digest": "sha256:" + strings.Repeat("a", 64)}, "observedAt": stamp, "schema": nil, "snapshot": nil, "statement": nil, "peerIdentity": nil, "upstreamToken": nil}}
	receipt["signature"] = hex.EncodeToString(ed25519.Sign(key, append([]byte("judgment-pack-gateway/receipt/3:"), canon(receipt)...)))
	seal := map[string]any{"sessionId": "research-test", "finalCount": 1, "sealedAt": stamp, "keyId": keyID}
	seal["signature"] = hex.EncodeToString(ed25519.Sign(key, append([]byte("judgment-pack-gateway/seal/2:"), canon(seal)...)))
	response := canon(map[string]any{"receipt": receipt, "result": result, "salts": map[string]string{"args": hex.EncodeToString(salt)}})
	pin := inputResearchPin{Authority: "gateway:test", Public: hex.EncodeToString(public)}
	proof := inputResearchProof{Session: "research-test", Source: "web", Arguments: args, Pin: pin, Response: response, Seal: canon(seal)}
	raw, _ := json.Marshal(proof)
	source, err := decodeInputResearchSource(inputResearchSource{ID: handoffProofSourceID, Kind: "page", At: stamp, Digest: sha256Digest(canon(result)), Verification: "verified", Bytes: len(response) + len(proof.Seal)}, args, result)
	if err != nil {
		t.Fatal(err)
	}
	return source, raw, pin
}
