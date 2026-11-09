package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// These are observation artifacts, never recorded expected outputs. The helper
// forwards only to the explicitly supplied executable, preserving argv, stdin,
// cwd, stdout, stderr and exit status. No installed-program lookup is allowed.
type retainedInitialActualRuntimeCall struct {
	Args                        []string
	Input, Pack, Stdout, Stderr []byte
	Exit                        int
}

func TestRetainedReviewActualRuntimeForward(t *testing.T) {
	if os.Getenv("JPS_RETAINED_INITIAL_FORWARD") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) < 3 || args[1] != "dialect" || (args[2] != "contract" && args[2] != "validate" && args[2] != "check") {
		os.Exit(125)
	}
	prior, err := filepath.Glob(filepath.Join(os.Getenv("JPS_RETAINED_INITIAL_EVIDENCE"), "runtime-call-*.json"))
	if err != nil || len(prior) >= 9 {
		os.Exit(125)
	}
	call := retainedInitialActualRuntimeCall{Args: append([]string(nil), args[1:]...)}
	call.Input, err = io.ReadAll(io.LimitReader(os.Stdin, (2<<20)+1))
	if err != nil || len(call.Input) > 2<<20 {
		os.Exit(125)
	}
	for i, arg := range call.Args {
		if arg == "--pack" && i+1 < len(call.Args) {
			call.Pack, err = os.ReadFile(call.Args[i+1])
			if err != nil {
				os.Exit(125)
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Getenv("JPS_RETAINED_INITIAL_RUNTIME_TARGET"), call.Args...)
	cmd.Stdin = bytes.NewReader(call.Input)
	stdout, stderr := &cappedBuffer{limit: 32 << 20}, &cappedBuffer{limit: 1 << 20}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err = cmd.Run()
	call.Stdout, call.Stderr = stdout.Bytes(), stderr.Bytes()
	if err != nil {
		call.Exit = 125
		if exited, ok := err.(*exec.ExitError); ok {
			call.Exit = exited.ExitCode()
		}
	}
	if stdout.exceeded() || stderr.exceeded() {
		call.Exit = 125
	}
	raw, marshalErr := json.Marshal(call)
	f, writeErr := os.CreateTemp(os.Getenv("JPS_RETAINED_INITIAL_EVIDENCE"), "runtime-call-*.json")
	if marshalErr != nil || writeErr != nil {
		os.Exit(125)
	}
	_, writeErr = f.Write(raw)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		os.Exit(125)
	}
	_, _ = os.Stdout.Write(call.Stdout)
	_, _ = os.Stderr.Write(call.Stderr)
	os.Exit(call.Exit)
}

func retainedInitialActualWrite(t *testing.T, dir, name string, raw []byte) {
	t.Helper()
	if filepath.Base(name) != name {
		t.Fatal("artifact basename required")
	}
	if err := os.WriteFile(filepath.Join(dir, name), raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("artifact %s bytes=%d sha256=%s", name, len(raw), digestOf(raw))
}

func retainedInitialActualCalls(t *testing.T, evidence string, wantEach int) []retainedInitialActualRuntimeCall {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(evidence, "runtime-call-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	calls := make([]retainedInitialActualRuntimeCall, 0, len(paths))
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		var call retainedInitialActualRuntimeCall
		if err != nil || json.Unmarshal(raw, &call) != nil || len(call.Args) < 2 || call.Exit != 0 || len(call.Stdout) == 0 {
			t.Fatal("incomplete actual Runtime invocation artifact", path, err)
		}
		counts[call.Args[1]]++
		calls = append(calls, call)
	}
	if !reflect.DeepEqual(counts, map[string]int{"contract": wantEach, "validate": wantEach, "check": wantEach}) {
		t.Fatal("independently observed Runtime operation counts", counts)
	}
	return calls
}

// Fixed two-row asserted qualification only. This does not qualify generated
// acquisition, a full producer installation, maxima, browser resources, or any
// public activation. Missing explicit components/evidence is a visible skip;
// the parent qualification command must require zero skips.
func TestRetainedReviewInitialActualAssertedTwoRows(t *testing.T) {
	runtime, runner, evidenceParent := os.Getenv("JPACK_BIN"), os.Getenv("JPACK_RUNNER_TEST_BIN"), os.Getenv("JPS_RETAINED_INITIAL_EVIDENCE_ROOT")
	if runtime == "" || runner == "" || evidenceParent == "" {
		t.Skip("requires explicit scratch JPACK_BIN, JPACK_RUNNER_TEST_BIN and JPS_RETAINED_INITIAL_EVIDENCE_ROOT; skip is not qualification")
	}
	for _, path := range []string{runtime, runner, evidenceParent} {
		if !filepath.IsAbs(path) {
			t.Fatal("explicit absolute scratch paths required")
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			t.Fatal(err)
		}
		rel, err := filepath.Rel(os.TempDir(), resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			t.Fatal("actual fixture paths must resolve inside scratch temporary storage", path)
		}
	}
	evidence, err := os.MkdirTemp(evidenceParent, "initial-asserted-")
	if err != nil {
		t.Fatal(err)
	}
	t.Log("retained actual initial evidence:", evidence)
	identities := map[string]any{}
	for name, path := range map[string]string{"runtime": runtime, "runner": runner} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		identities[name+"Path"], identities[name+"SHA256"] = path, digestOf(raw)
		versionArg := "--version"
		if name == "runner" {
			versionArg = "version"
		}
		identities[name+"VersionCommand"] = []string{path, versionArg}
		versionCtx, stopVersion := context.WithTimeout(t.Context(), 10*time.Second)
		versionCommand := exec.CommandContext(versionCtx, path, versionArg)
		versionOutput, versionErr := versionCommand.CombinedOutput()
		stopVersion()
		retainedInitialActualWrite(t, evidence, name+"-version.txt", versionOutput)
		if versionErr != nil || len(bytes.TrimSpace(versionOutput)) == 0 {
			t.Fatal("explicit component version probe failed; no fallback or retry", name, versionArg, versionErr)
		}
		identities[name+"VersionOutput"] = string(versionOutput)
	}
	retainedInitialActualWrite(t, evidence, "component-identities.json", d0aJSON(t, identities))
	ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
	defer cancel()
	s, ts := newTestServer(t, false)
	selected, control := retainedB3ActualRunner(t, s, []byte(`[]`), 2, 2)
	if s.localGateway != nil {
		t.Fatal("asserted fixture has Gateway authority")
	}
	// Reuse the actual Runner owner and its cleanup. A second transparent
	// observer captures complete replies independently of aggregate serialization.
	endpoint, _, err := s.jobs.endpoint()
	if err != nil {
		t.Fatal(err)
	}
	target, err := url.Parse(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	transport := &http.Transport{Proxy: nil}
	proxy.Transport = transport
	t.Cleanup(transport.CloseIdleConnections)
	var mu sync.Mutex
	var replies, requests, statuses [][]byte
	proxy.ModifyResponse = func(response *http.Response) error {
		raw, err := io.ReadAll(io.LimitReader(response.Body, retainedSelectionMaxInputBytes+1))
		_ = response.Body.Close()
		if err != nil || int64(len(raw)) > retainedSelectionMaxInputBytes {
			return fmt.Errorf("actual observer response incomplete")
		}
		response.Body = io.NopCloser(bytes.NewReader(raw))
		mu.Lock()
		defer mu.Unlock()
		if response.Request.URL.Path == "/v1/status" {
			statuses = append(statuses, bytes.Clone(raw))
		} else {
			replies = append(replies, bytes.Clone(raw))
		}
		return nil
	}
	observer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !(r.Method == http.MethodGet && r.URL.Path == "/v1/status" || r.Method == http.MethodPost && r.URL.Path == retainedRunnerPrefix+"inputs/next") {
			t.Error("unexpected asserted component route", r.Method, r.URL.Path)
			w.WriteHeader(500)
			return
		}
		if r.Method == http.MethodPost {
			raw, err := io.ReadAll(io.LimitReader(r.Body, (16<<20)+1))
			_ = r.Body.Close()
			if err != nil || len(raw) > 16<<20 {
				t.Error("actual request capture incomplete")
				w.WriteHeader(500)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(raw))
			mu.Lock()
			requests = append(requests, bytes.Clone(raw))
			mu.Unlock()
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(observer.Close)
	s.jobs.url = observer.URL
	// Only Desk's direct Runtime checks go through this observer. The owned
	// Runner keeps its explicitly selected real Runtime executable.
	t.Setenv("JPS_RETAINED_INITIAL_RUNTIME_TARGET", runtime)
	t.Setenv("JPS_RETAINED_INITIAL_EVIDENCE", evidence)
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	wrapper := filepath.Join(t.TempDir(), "runtime-observer")
	script := "#!/bin/sh\nexport JPS_RETAINED_INITIAL_FORWARD=1\nexec " + quote(os.Args[0]) + " -test.run='^TestRetainedReviewActualRuntimeForward$' -- \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	s.cfg.JpackBin = wrapper
	credential := privateWorkspaceTestSession(t, ts)
	call := func(name, method, path, revision string, body any, status int) *httptest.ResponseRecorder {
		t.Helper()
		raw := d0aJSON(t, body)
		retainedInitialActualWrite(t, evidence, name+"-request.json", raw)
		r := httptest.NewRequest(method, path, bytes.NewReader(raw)).WithContext(ctx)
		r.Header.Set("Authorization", "Bearer "+credential)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("If-Match", revision)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		retainedInitialActualWrite(t, evidence, name+"-reply.json", w.Body.Bytes())
		if w.Code != status {
			t.Fatalf("%s status=%d expected=%d", name, w.Code, status)
		}
		return w
	}
	decode := func(w *httptest.ResponseRecorder) inputPreparationReply {
		t.Helper()
		var saved inputPreparationReply
		if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil {
			t.Fatal(err)
		}
		return saved
	}
	created := decode(call("create", "POST", "/api/input-preparations", "", map[string]any{"name": "Actual asserted selection", "recipe": inputRecipe{Intent: "Import original asserted rows"}}, 201))
	path := "/api/input-preparations/" + created.Document.ID
	const csv = "Country,Amount,Flag,Text,License\nCA,-0.00,no,  first  ,absent\nGB,1.25,yes,second,present\n"
	retainedInitialActualWrite(t, evidence, "source.csv", []byte(csv))
	imported := decode(call("import", "POST", path+"/imports", created.SHA256, map[string]string{"kind": "csv", "name": "asserted.csv", "content": csv}, 200))
	if len(imported.Document.Records) != 2 || len(imported.Document.Imports) != 1 || imported.Document.Imports[0].Content != csv {
		t.Fatal("supported CSV ingress lost two-row source")
	}
	// Handwritten logical mapping and fixed expected facts. Only the current
	// shared contract helper supplies the installed tuple; no obsolete literal.
	q := retainedB4Qualification(t)
	selection := inputRetainedContract()
	q.ConversionContract = &selection
	profile := selection.Profile
	q.Mapping = inputRecordDialect{Version: 2, Profile: &profile, Case: &inputCaseDialect{Facts: []inputDialectFact{}, Evidence: []inputDialectEvidence{}}, Sources: []json.RawMessage{}}
	q.Conversions = map[string]inputConversion{}
	for _, rule := range []struct{ target, column, kind string }{{"/country", "Country", "text"}, {"/amount", "Amount", "decimal"}, {"/flag", "Flag", "boolean"}, {"/text", "Text", "text"}} {
		conversion := inputConversion{Kind: rule.kind}
		leaf := &inputRetainedConversion{ContractVersion: "1", Column: rule.column, Kind: rule.kind}
		if rule.kind == "boolean" {
			yes, no := "yes", "no"
			leaf.TrueValue, leaf.FalseValue = &yes, &no
			conversion.TrueValue, conversion.FalseValue = yes, no
		}
		q.Mapping.Case.Facts = append(q.Mapping.Case.Facts, inputDialectFact{Target: rule.target, Source: "/retained", Conversion: leaf})
		q.Conversions[rule.target] = conversion
	}
	q.Mapping.Case.Evidence = append(q.Mapping.Case.Evidence, inputDialectEvidence{Requirement: "license", Source: "/retained", Availability: &inputRetainedAvailability{ContractVersion: "1", Column: "License"}})
	q.Pack = `{"specVersion":"0.2.0-draft","id":"https://example.invalid/pack/initial-asserted","version":"1.0.0","title":"Actual asserted selection","decision":{"intent":"Inspect asserted values","question":"Ready?"},"outcomes":[{"id":"ready","label":"Ready"},{"id":"other","label":"Other"}],"rules":[{"id":"country","description":"Country","when":{"op":"fact","path":"/country","operator":"equals","value":"CA"},"outcome":"ready","onUnknown":"escalate"},{"id":"amount","description":"Amount","when":{"op":"fact","path":"/amount","operator":"equals","value":"0"},"outcome":"ready","onUnknown":"escalate"},{"id":"flag","description":"Flag","when":{"op":"fact","path":"/flag","operator":"equals","value":false},"outcome":"ready","onUnknown":"escalate"},{"id":"text","description":"Text","when":{"op":"fact","path":"/text","operator":"equals","value":"second"},"outcome":"ready","onUnknown":"escalate"}],"evidenceRequirements":[{"id":"license","description":"License","required":false}]}`
	q.Digest = retainedDigest([]byte(q.Pack))
	savedReply := decode(call("selected-save", "PUT", path, imported.SHA256, map[string]any{"name": "Selected asserted recipe", "recipe": inputRecipe{Intent: "Prepare asserted values", Qualification: q}}, 200))
	saved, err := s.preparationRepository().Get(ctx, created.Document.ID)
	if err != nil || saved.SHA256 != savedReply.SHA256 || saved.SHA256 == imported.SHA256 || saved.Document.cellCustody == nil || !reflect.DeepEqual(saved.Document.Imports, imported.Document.Imports) {
		t.Fatal("selected CAS/readback identity or original custody lost", err)
	}
	if !reflect.DeepEqual(saved.Document.Recipe.Qualification, q) {
		t.Fatal("selected save/readback changed handwritten recipe")
	}
	savedBytes, err := os.ReadFile(filepath.Join(s.projectDir, inputPreparationDir, saved.Document.ID+".json"))
	if err != nil || digestOf(savedBytes) != saved.SHA256 {
		t.Fatal("exact saved source bytes differ from readback SHA", err)
	}
	retainedInitialActualWrite(t, evidence, "saved-source.json", savedBytes)
	retainedInitialActualCalls(t, evidence, 1) // B4: contract/validate/check, independently observed.
	if selected.Load() != 0 || control.Load() != 0 {
		t.Fatal("B4 save dispatched Runner")
	}
	ids := []string{saved.Document.Records[1].ID, saved.Document.Records[0].ID}
	request := append([]byte(" \n"), d0aJSON(t, map[string]any{"recordIds": ids})...)
	request = append(request, '\n', ' ')
	savedContract := saved.Document.Recipe.Qualification.ConversionContract
	wireContract := retainedSelectionWireContract{Contract: savedContract.Contract, Spec: retainedSelectionWireSpec{Version: savedContract.Spec.Version, Revision: savedContract.Spec.Revision, SchemaSHA256: savedContract.Spec.SchemaSHA256, CorpusSHA256: savedContract.Spec.CorpusSHA256}, Profile: savedContract.Profile}
	source := retainedSelectionWireSource{PreparationID: saved.Document.ID, Revision: saved.SHA256, PackDigest: saved.Document.Recipe.Qualification.Digest, ConversionContract: wireContract, Origin: "asserted", RecordIDs: ids, RequestBytes: int64(len(request))}
	retainedInitialActualWrite(t, evidence, "source-identity.json", d0aJSON(t, source))
	retainedInitialActualWrite(t, evidence, "initial-request.json", request)
	retainedReviewInitialRegister(s)
	retainedReviewRegisterFixture(s)
	repository := retainedReviewInitialTracked(s)
	r := retainedReviewInitialRequest(credential, source, string(request)).WithContext(ctx)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	retainedInitialActualWrite(t, evidence, "initial-stream.bin", w.Body.Bytes())
	retainedInitialActualWrite(t, evidence, "initial-response-headers.json", d0aJSON(t, w.Header()))
	if w.Code != 200 || w.Header().Get("Content-Type") != retainedSelectionMediaType || w.Header().Get("Content-Encoding") != "" {
		t.Fatal("actual private initial response", w.Code, w.Header())
	}
	if repository.reads.Load() != 1 || repository.gets.Load() != 1 || repository.directGets.Load() != 0 || repository.closes.Load() != 1 {
		t.Fatal("initial handler did not own one real held Read/Get/Close")
	}
	if selected.Load() != 2 || control.Load() != 2 {
		t.Fatal("actual initial Runner counts", selected.Load(), control.Load())
	}
	calls := retainedInitialActualCalls(t, evidence, 3) // B4 three plus B3 six.
	mu.Lock()
	actualReplies, actualRequests, actualStatuses := append([][]byte(nil), replies...), append([][]byte(nil), requests...), append([][]byte(nil), statuses...)
	mu.Unlock()
	if len(actualReplies) != 2 || len(actualRequests) != 2 || len(actualStatuses) != 2 {
		t.Fatal("independent Runner observation counts")
	}
	actualInputs := make([][]byte, 2)
	for i, reply := range actualReplies {
		node, err := parseRetainedJSON(reply)
		if err != nil || node.member("input") == nil {
			t.Fatal("actual complete Runner reply missing Input", err)
		}
		actualInputs[i] = bytes.Clone(node.member("input").raw)
		retainedInitialActualWrite(t, evidence, fmt.Sprintf("row-%d-runner-reply.json", i), reply)
		retainedInitialActualWrite(t, evidence, fmt.Sprintf("row-%d-runner-request.json", i), actualRequests[i])
		retainedInitialActualWrite(t, evidence, fmt.Sprintf("row-%d-input.json", i), actualInputs[i])
		retainedInitialActualWrite(t, evidence, fmt.Sprintf("row-%d-status.json", i), actualStatuses[i])
		input, err := parseRetainedJSON(actualInputs[i])
		if err != nil {
			t.Fatal(err)
		}
		facts := input.member("facts")
		want := []map[string]string{{"country": `"GB"`, "amount": `"1.25"`, "flag": `true`, "text": `"second"`}, {"country": `"CA"`, "amount": `"-0.00"`, "flag": `false`, "text": `"  first  "`}}[i]
		for name, value := range want {
			if facts.member(name) == nil || string(facts.member(name).raw) != value {
				t.Fatal("actual handwritten fact mismatch", i, name)
			}
		}
		wantEvidence := []string{`"present"`, `"absent"`}[i]
		if string(input.member("evidence").member("license").raw) != wantEvidence {
			t.Fatal("actual handwritten evidence mismatch", i)
		}
	}
	length, err := strconv.ParseInt(w.Header().Get("Content-Length"), 10, 64)
	if err != nil || length != int64(w.Body.Len()) {
		t.Fatal("complete stream Content-Length", err)
	}
	seen := 0
	header, descriptors, err := readRetainedSelectionWire(bytes.NewReader(w.Body.Bytes()), source, length, func(row retainedSelectionWireDescriptor, input []byte) error {
		if row.Index != seen || row.ID != ids[seen] || !bytes.Equal(input, actualInputs[seen]) {
			return fmt.Errorf("stream row differs from independently captured actual Input")
		}
		seen++
		return nil
	})
	if err != nil || seen != 2 || len(descriptors) != 2 {
		t.Fatal("full stream framing/index/digest/EOF validation", err)
	}
	retainedInitialActualWrite(t, evidence, "stream-header.json", d0aJSON(t, header))
	retainedInitialActualWrite(t, evidence, "stream-descriptors.json", d0aJSON(t, descriptors))
	// Footer is parsed by the exact reader above; its final canonical JSON
	// occupies the stream suffix and retains the independently checked index SHA.
	endAt := bytes.LastIndex(w.Body.Bytes(), []byte(`{"rows":`))
	var end retainedSelectionWireEnd
	if endAt < 0 || json.Unmarshal(w.Body.Bytes()[endAt:], &end) != nil {
		t.Fatal("complete stream end missing")
	}
	retainedInitialActualWrite(t, evidence, "stream-end.json", d0aJSON(t, end))
	rowBody := d0aJSON(t, map[string]any{"viewId": header.ViewID, "indexSha256": end.IndexSHA256, "index": 0, "id": ids[0]})
	revisit := retainedReviewHTTPRequest(t, s, credential, saved.Document.ID, "prepared-row", string(rowBody), end.IndexSHA256)
	retainedInitialActualWrite(t, evidence, "revisit-input.json", revisit.Body.Bytes())
	if revisit.Code != 200 || !bytes.Equal(revisit.Body.Bytes(), actualInputs[0]) {
		t.Fatal("real private row revisit changed exact first Input")
	}
	closeBody := d0aJSON(t, map[string]any{"viewId": header.ViewID, "indexSha256": end.IndexSHA256})
	closed := retainedReviewHTTPRequest(t, s, credential, saved.Document.ID, "prepared-view-close", string(closeBody), end.IndexSHA256)
	if closed.Code != 204 {
		t.Fatal("real private view close", closed.Code)
	}
	if selected.Load() != 2 || control.Load() != 2 || len(retainedInitialActualCalls(t, evidence, 3)) != len(calls) || s.localGateway != nil {
		t.Fatal("revisit/close performed new component work")
	}
	mu.Lock()
	observedUnchanged := len(replies) == 2 && len(requests) == 2 && len(statuses) == 2
	mu.Unlock()
	if !observedUnchanged {
		t.Fatal("revisit/close changed independent Runner observations")
	}
	if repository.reads.Load() != 1 || repository.gets.Load() != 1 || repository.directGets.Load() != 0 || repository.closes.Load() != 1 {
		t.Fatal("revisit/close reacquired saved source")
	}
	retainedReviewAssertNoReservations(t, s)
	retainedInitialActualWrite(t, evidence, "completion.json", d0aJSON(t, map[string]any{"status": "passed", "rows": 2, "requestBytes": len(request), "source": source, "streamBytes": length, "streamSHA256": digestOf(w.Body.Bytes()), "indexSHA256": end.IndexSHA256, "runnerSelectedPOSTs": 2, "runnerStatusGETs": 2, "runtimeB4Calls": 3, "runtimeB3Calls": 6, "gatewayCalls": 0, "realRepositoryReadGetClose": []int{1, 1, 1}, "exactEOF": true, "exactRevisit": true, "viewClosed": true, "scope": "finite asserted initial-handler integration only; no public activation/generated/maxima/browser qualification"}))
}
