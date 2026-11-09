package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// This is the sole D4-A actual-component fixture. It owns one Runner service,
// one local Gateway service and one new reviewed-input acquisition. Its three
// mutations reuse that acquisition and never call the producer again.
func TestRetainedD4aActualGeneratedPreparation(t *testing.T) {
	bundle, bin, runtime := os.Getenv("JPS_TEST_GATEWAY_BUNDLE"), os.Getenv("JPACK_RUNNER_TEST_BIN"), os.Getenv("JPACK_BIN")
	if bundle == "" || bin == "" || runtime == "" {
		t.Skip("requires explicitly selected scratch Gateway, Runner and Runtime; a skip is not qualification")
	}
	for _, path := range []string{bundle, bin, runtime} {
		if !filepath.IsAbs(path) {
			t.Fatal("actual components require explicit absolute scratch paths")
		}
	}
	s, doc, record, row, _ := retainedD3aOwnerFixture(t, bundle)
	s.cfg.JpackBin = runtime
	s.localGateway = &localGateway{bundle: bundle, executable: os.Args[0], inputHandoffs: s.inputHandoffPath()}
	t.Cleanup(s.localGateway.close)
	profiles, _, _, err := s.retainedPrivateProfiles()
	if err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	if err := os.Chmod(state, 0700); err != nil {
		t.Fatal(err)
	}
	inputRoot := t.TempDir()
	if err := os.Chmod(inputRoot, 0700); err != nil {
		t.Fatal(err)
	}
	j := &jobsCompanion{bin: bin, runtime: runtime, dir: state, workspace: "retained-d4a", owner: "local-owner", inputRoot: inputRoot, profiles: profiles, stop: make(chan struct{})}
	if _, err := j.start("", ""); err != nil {
		t.Fatal("actual Runner did not admit trusted v1+v2 profiles", err)
	}
	t.Cleanup(j.close)
	// Transparent HTTP observers preserve the actual verified executables and
	// record directly observable requests, without replacing binary identities.
	runnerURL, token, err := j.endpoint()
	if err != nil {
		t.Fatal(err)
	}
	runnerTarget, err := url.Parse(runnerURL)
	if err != nil {
		t.Fatal(err)
	}
	runnerForward := httputil.NewSingleHostReverseProxy(runnerTarget)
	runnerTransport := &http.Transport{Proxy: nil}
	runnerForward.Transport = runnerTransport
	t.Cleanup(runnerTransport.CloseIdleConnections)
	var selectedRequests, controlReads, acquisitions atomic.Int64
	var capturedMu sync.Mutex
	var captured []byte
	runnerObserver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !(r.Method == http.MethodGet && (r.URL.Path == "/v1/status" || r.URL.Path == "/v1/input-profiles") || r.Method == http.MethodPost && r.URL.Path == retainedRunnerPrefix+"inputs/next") {
			t.Error("private preparation attempted another Runner route", r.Method, r.URL.Path)
			w.WriteHeader(500)
			return
		}
		if r.URL.Path == "/v1/status" || r.URL.Path == "/v1/input-profiles" {
			if controlReads.Add(1) > 6 {
				t.Error("fixture exceeded six Runner control reads")
				w.WriteHeader(500)
				return
			}
		}
		if r.URL.Path == retainedRunnerPrefix+"inputs/next" {
			if selectedRequests.Add(1) > 4 {
				t.Error("fixture exceeded four selected requests")
				w.WriteHeader(500)
				return
			}
			body, err := io.ReadAll(io.LimitReader(r.Body, 16777217))
			if err != nil || len(body) > 16777216 {
				t.Error("request observer overflow", err)
				w.WriteHeader(500)
				return
			}
			r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(body))
			capturedMu.Lock()
			if captured == nil {
				captured = bytes.Clone(body)
			}
			capturedMu.Unlock()
		}
		runnerForward.ServeHTTP(w, r)
	}))
	t.Cleanup(runnerObserver.Close)
	done := make(chan struct{})
	s.jobs = &jobsCompanion{url: runnerObserver.URL, token: token, done: done, stop: make(chan struct{})}
	t.Cleanup(func() { close(done) }) // HTTP-only companion must finish before Server cleanup.

	// Starting the owned service is explicit and counted here. The original
	// proof fixture and validation helpers can also run auxiliary Gateway CLI
	// commands; their process count is not observable here or claimed enforced.
	pin, err := s.localGateway.ensure(s.assistant)
	if err != nil {
		t.Fatal(err)
	}
	gatewayTarget, err := url.Parse(pin.URL)
	if err != nil {
		t.Fatal(err)
	}
	gatewayForward := httputil.NewSingleHostReverseProxy(gatewayTarget)
	gatewayTransport := &http.Transport{Proxy: nil}
	gatewayForward.Transport = gatewayTransport
	t.Cleanup(gatewayTransport.CloseIdleConnections)
	gatewayObserver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/acquire" && acquisitions.Add(1) > 1 {
			t.Error("fixture attempted another reviewed-input acquisition")
			w.WriteHeader(500)
			return
		}
		gatewayForward.ServeHTTP(w, r)
	}))
	t.Cleanup(gatewayObserver.Close)
	// The pin's key/authority and the owned process are unchanged. Only this
	// fixture's local HTTP destination passes through the observer.
	s.localGateway.mu.Lock()
	s.localGateway.pin.URL = gatewayObserver.URL
	s.localGateway.mu.Unlock()

	listing, err := s.retainedPrivateRunnerGET(t.Context(), "input-profiles", 81920)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := parseRetainedJSON(listing)
	if err != nil || listed.kind != '[' {
		t.Fatal("actual profile listing unavailable", err)
	}
	profileDigest := ""
	for _, entry := range listed.elements {
		id, _ := retainedMemberString(entry.member("profile"), "id")
		if id == retainedReviewedInputProfile {
			profileDigest, _ = retainedMemberString(entry, "digest")
		}
	}
	if !retainedSpecDigest.MatchString(profileDigest) {
		t.Fatal("actual selected profile digest absent")
	}
	mapping := retainedD4aMapping(t, profileDigest)
	// Exercise legal omission in the actual owner. Runner may emit its
	// documented v2 defaults without borrowing Runtime's document digest.
	// Remove only root members. Nested case/read facts and evidence arrays
	// remain part of the mapping; member position must not affect omission.
	var mappingMembers map[string]json.RawMessage
	if err := json.Unmarshal(mapping, &mappingMembers); err != nil {
		t.Fatal("actual omission fixture mapping is invalid", err)
	}
	for _, member := range []struct {
		name string
		raw  string
	}{{"provider", `""`}, {"facts", "null"}, {"evidence", "null"}} {
		if string(mappingMembers[member.name]) != member.raw {
			t.Fatal("actual omission fixture root default changed", member.name)
		}
		delete(mappingMembers, member.name)
	}
	mapping, err = json.Marshal(mappingMembers)
	if err != nil {
		t.Fatal("actual omission fixture encoding failed", err)
	}
	omittedMapping, err := parseRetainedJSON(mapping)
	if err != nil {
		t.Fatal("actual omission fixture is invalid", err)
	}
	for _, member := range []string{"provider", "facts", "evidence"} {
		if omittedMapping.member(member) != nil {
			t.Fatal("actual omission fixture retained root default", member)
		}
	}
	before, err := s.preparationRepository().Get(t.Context(), doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	selection := retainedInstalledSelector(retainedPreparationRoles()...)
	result, err := s.prepareRetainedReviewedInput(t.Context(), selection, mapping, []byte(retainedRuntimeTestPack), doc, record, row, nil)
	if err != nil || len(result.InputJSON) == 0 || len(result.ReplyJSON) == 0 {
		t.Fatal("actual authenticated generated preparation failed", err)
	}
	if !bytes.Equal(result.SuppliedMappingJSON, mapping) || !result.Runtime.Accepted || !retainedDialectSameDocument(mapping, []byte(result.Runtime.Document)) || !retainedSpecDigest.MatchString(result.RunnerMappingDigest) {
		t.Fatal("independent mapping identities missing")
	}
	if result.RunnerMappingDigest == result.Runtime.Digest {
		t.Fatal("omitted v2 defaults did not retain distinct digest domains")
	}
	// Each complete report comes from the three fixed D2 CLI calls. No fourth
	// canonicalization command or rehearsal/evaluation is used by this owner.
	for _, evidence := range []retainedDialectRuntimeEvidence{result.Runtime.Discovery, result.Runtime.Validation, result.Runtime.Coverage} {
		if evidence.RunFailed || len(evidence.Stdout) == 0 {
			t.Fatal("missing actual D2 operation evidence")
		}
	}
	input, err := parseRetainedJSON(result.InputJSON)
	if err != nil {
		t.Fatal(err)
	}
	facts := input.member("facts")
	country, err := retainedMemberString(facts.member("company"), "country")
	if err != nil || country != "CA" {
		t.Fatal("actual Runner did not derive handwritten original field", err)
	}
	preparation := input.member("preparation")
	lineage := preparation.member("lineage")
	if lineage == nil || lineage.kind != '[' || len(lineage.elements) != 1 {
		t.Fatal("actual original lineage missing")
	}
	leaf := lineage.elements[0]
	class, _ := retainedMemberString(leaf, "class")
	source, _ := retainedMemberString(leaf, "source")
	target, _ := retainedMemberString(leaf, "target")
	if class != "generated" || source != "research" || target != "/company/country" || string(leaf.member("generatedInfluence").raw) != "true" || leaf.member("receipt") == nil || len(preparation.member("cites").elements) != 1 {
		t.Fatal("actual generated class/influence/citation missing")
	}
	responseText, err := retainedMemberString(input.member("source").member("sources").member("research"), "retainedResponseJson")
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := parseRetainedJSON([]byte(responseText))
	if err != nil {
		t.Fatal(err)
	}
	carrier := envelope.member("result")
	frame := carrier.member("retained")
	original, err := retainedMemberString(carrier, "originalSnapshotJson")
	if err != nil {
		t.Fatal(err)
	}
	originalDigest, _ := retainedMemberString(carrier, "originalSnapshotSha256")
	if originalDigest != sha256Digest([]byte(original)) || frame == nil {
		t.Fatal("original snapshot/frame custody failed")
	}
	frameDigest, _ := retainedMemberString(leaf.member("columnTransformation").member("input").member("ref"), "frameSha256")
	location, _ := retainedMemberString(leaf.member("columnTransformation").member("input").member("ref"), "location")
	if frameDigest != sha256Digest(frame.raw) || location != "research-value" || leaf.member("columnTransformation").member("researchIdentity") == nil {
		t.Fatal("original frame/reference/research lineage missing")
	}
	if declarations := frame.member("research").member("declaration").member("fields"); declarations == nil || len(declarations.elements) != 11 {
		t.Fatal("full original declaration inventory omitted")
	}
	// Exercise every traversed frame owner's finite lower ceiling over these
	// complete actual bytes, using the same checker called before grant in the
	// production seam. This is finite propagation evidence, not a D7 maximum.
	actualProduction := retainedReviewedInputProduction{CarrierJSON: bytes.Clone(carrier.raw), OriginalSnapshotSHA256: originalDigest, ProjectedFrameSHA256: sha256Digest(frame.raw)}
	for _, role := range []string{"document-validation", "preview", "preparation"} {
		limits := retainedRoleCaps(role)
		limits["frameBytes"] = int64(len(frame.raw))
		if err := retainedPreparationProduction(actualProduction, retainedD4aAdmission(t, map[string]map[string]int64{role: limits})); err != nil {
			t.Fatal("exact complete actual frame refused", role, err)
		}
		limits["frameBytes"]--
		if err := retainedPreparationProduction(actualProduction, retainedD4aAdmission(t, map[string]map[string]int64{role: limits})); retainedContractCode(err) != "input-over-bound" {
			t.Fatal("one-over complete actual frame admitted", role, err)
		}
	}
	admission := retainedD4aAdmission(t, result.Limits)
	capturedMu.Lock()
	request := bytes.Clone(captured)
	capturedMu.Unlock()
	requestNode, err := parseRetainedJSON(request)
	if err != nil {
		t.Fatal(err)
	}
	textNode := requestNode.member("source").member("sources").member("research").member("retainedResponseJson")
	if sent, _ := textNode.scalarString(); sent != responseText {
		t.Fatal("exact signed response did not round-trip")
	}
	if requestNode.member("source").member("mappingDigest") != nil {
		t.Fatal("Runtime digest crossed into Runner mapping identity")
	}

	// Real signature and frozen argument refusals, using the same acquisition.
	// Runner refuses those proofs with 422. Duplicate custody is a malformed
	// SourceValue (retainedResponseJson must be its sole member), so strict
	// request decoding refuses it earlier with 400.
	signature, err := retainedMemberString(envelope.member("receipt"), "signature")
	if err != nil || len(signature) != 128 {
		t.Fatal("actual receipt signature unavailable")
	}
	changedSignature := "0" + signature[1:]
	if signature[0] == '0' {
		changedSignature = "1" + signature[1:]
	}
	oldSignature, _ := json.Marshal(signature)
	newSignature, _ := json.Marshal(changedSignature)
	tamperedEnvelope := bytes.Replace([]byte(responseText), oldSignature, newSignature, 1)
	tamperedText, _ := json.Marshal(string(tamperedEnvelope))
	signatureRequest := bytes.Replace(request, textNode.raw, tamperedText, 1)
	caseNode := requestNode.member("source").member("case")
	digestNode := caseNode.member("digest")
	wrongDigest, _ := json.Marshal("sha256:" + strings.Repeat("0", 64))
	argumentRequest := bytes.Replace(request, digestNode.raw, wrongDigest, 1)
	custodyRequest := bytes.Replace(request, []byte(`"retainedResponseJson":`), []byte(`"response":{},"retainedResponseJson":`), 1)
	for _, mutation := range []struct {
		name           string
		raw            []byte
		expectedStatus int
	}{
		{"signature", signatureRequest, http.StatusUnprocessableEntity},
		{"frozen-arguments", argumentRequest, http.StatusUnprocessableEntity},
		{"duplicate-custody", custodyRequest, http.StatusBadRequest},
	} {
		if bytes.Equal(mutation.raw, request) {
			t.Fatal("actual mutation did not change request", mutation.name)
		}
		reply, err := s.retainedPreparationPOST(context.Background(), mutation.raw, admission)
		var refused *inputRunnerError
		observedStatus := 0
		if errors.As(err, &refused) {
			observedStatus = refused.Status
		}
		if refused == nil || observedStatus != mutation.expectedStatus || len(reply) != 0 {
			t.Fatalf("actual Runner mutation %s: expected refusal status=%d and no successful input; observed status=%d replyBytes=%d errorType=%T error=%v", mutation.name, mutation.expectedStatus, observedStatus, len(reply), err, err)
		}
	}
	after, err := s.preparationRepository().Get(t.Context(), doc.ID)
	if err != nil || before.SHA256 != after.SHA256 {
		t.Fatal("private preparation changed product input rows", err)
	}
	entries, err := os.ReadDir(s.inputHandoffPath())
	if err != nil && !os.IsNotExist(err) || len(entries) != 0 {
		t.Fatal("completed preparation retained a grant", err)
	}
	if selectedRequests.Load() != 4 || controlReads.Load() > 6 || acquisitions.Load() != 1 {
		t.Fatal("fixture operation counts changed", selectedRequests.Load(), controlReads.Load(), acquisitions.Load())
	}
	t.Logf("Observed: Runner owned service starts=1, Gateway owned service starts=1, explicit completed D2 commands=3, selected requests=%d, Runner status/listing reads=%d, new reviewed-input acquisitions=%d. Auxiliary Gateway CLI process count unavailable here; outer parent bound remains required.", selectedRequests.Load(), controlReads.Load(), acquisitions.Load())
}
