package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func retainedD3bAdmission(t *testing.T, limits map[string]int64) *retainedAdmission {
	t.Helper()
	selection := retainedInstalledSelector("producer-carrier")
	grammar := retainedProducerGrammar
	capability := retainedConversionCapability{Contract: selection.Contract, Spec: selection.Spec, Profile: selection.Profile, Role: "producer-carrier", Limits: retainedRoleCaps("producer-carrier"), ProducerGrammar: &grammar}
	var lower map[string]map[string]int64
	if limits != nil {
		lower = map[string]map[string]int64{"producer-carrier": limits}
	}
	result, err := negotiateRetainedRoles(selection, []string{"producer-carrier"}, true, retainedOwnerReports{Producer: []retainedConversionCapability{capability}}, lower)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func retainedD3bSyntheticProduction(t *testing.T) retainedReviewedInputProduction {
	t.Helper()
	original := []byte(`{"version":1,"class":"generated"}`)
	carrier := retainedReviewedCarrier{ContractVersion: "1", OriginalSnapshotJSON: string(original), OriginalSnapshotSHA256: sha256Digest(original), Retained: retainedColumnFrame{ContractVersion: "1", Origin: "desk-reviewed-input"}}
	raw, err := json.Marshal(carrier)
	if err != nil {
		t.Fatal(err)
	}
	node, err := parseRetainedJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	return retainedReviewedInputProduction{Carrier: carrier, CarrierJSON: raw, OriginalSnapshotSHA256: sha256Digest(original), ProjectedFrameSHA256: sha256Digest(node.member("retained").raw)}
}

func retainedD3bPrivateRoot(t *testing.T) *os.Root {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := openPrivateDataRoot(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	t.Setenv("JPACK_INPUT_HANDOFFS", dir)
	return root
}

func retainedD3bRequest(id, digest string) []byte {
	raw, _ := json.Marshal(map[string]any{"tool": reviewedInputTool, "arguments": map[string]string{"grant": id, "digest": digest}})
	return raw
}

func TestRetainedD3bGrantDigestSelectorAndCollisionRefusal(t *testing.T) {
	root := retainedD3bPrivateRoot(t)
	_, adapter, err := deskInputAdapter()
	if err != nil {
		t.Fatal(err)
	}
	approvedDigest := "sha256:" + strings.Repeat("a", 64)
	production := retainedD3bSyntheticProduction(t)
	admission := retainedD3bAdmission(t, nil)
	grant, err := retainedGrantBytes(time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano), approvedDigest, adapter["digest"], production, admission)
	if err != nil {
		t.Fatal(err)
	}
	id := "grant-" + strings.Repeat("a", 32)
	if err := writePrivateData(root, id+retainedGrantSuffix, grant); err != nil {
		t.Fatal(err)
	}
	request := retainedD3bRequest(id, sha256Digest(grant))
	var output bytes.Buffer
	if err := RunInputSourceAdapter(bytes.NewReader(request), &output); err != nil {
		t.Fatal(err)
	}
	node, err := parseRetainedJSON(output.Bytes())
	if err != nil || !retainedExactObject(node, []string{"result", "acquisition"}) || !bytes.Equal(node.member("result").raw, production.CarrierJSON) {
		t.Fatal("selected adapter changed the exact carrier token", err)
	}
	acquisition := node.member("acquisition")
	version, _ := retainedMemberString(acquisition.member("adapter"), "version")
	snapshot, _ := retainedMemberString(acquisition, "snapshot")
	if version != "2" || snapshot != sha256Digest(production.CarrierJSON) {
		t.Fatal("selected adapter identity or carrier digest changed")
	}
	for _, change := range []struct {
		name    string
		replace []byte
	}{
		{"selector", bytes.Replace(grant, []byte(`"profile":"base"`), []byte(`"profile":"other"`), 1)},
		{"limits", bytes.Replace(grant, []byte(`"responseBytes":5242880`), []byte(`"responseBytes":5242881`), 1)},
		{"carrier-digest", bytes.Replace(grant, []byte(sha256Digest(production.CarrierJSON)), []byte(approvedDigest), 1)},
		{"adapter-digest", bytes.Replace(grant, []byte(adapter["digest"]), []byte(approvedDigest), 1)},
	} {
		t.Run(change.name, func(t *testing.T) {
			if bytes.Equal(change.replace, grant) {
				t.Fatal("mutation did not change grant")
			}
			if err := writePrivateData(root, id+retainedGrantSuffix, change.replace); err != nil {
				t.Fatal(err)
			}
			output.Reset()
			if err := RunInputSourceAdapter(bytes.NewReader(request), &output); err == nil || output.Len() != 0 {
				t.Fatal("changed selected grant admitted")
			}
		})
	}
	if err := writePrivateData(root, id+retainedGrantSuffix, grant); err != nil {
		t.Fatal(err)
	}
	legacy, _ := json.Marshal(inputHandoff{Expires: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano), Digest: sha256Digest(production.CarrierJSON), Result: production.CarrierJSON})
	if err := writePrivateData(root, id+".json", legacy); err != nil {
		t.Fatal(err)
	}
	for _, request := range [][]byte{request, retainedD3bRequest(id, sha256Digest(production.CarrierJSON))} {
		output.Reset()
		if err := RunInputSourceAdapter(bytes.NewReader(request), &output); err == nil || output.Len() != 0 {
			t.Fatal("coexisting grant channels admitted")
		}
	}
}

func TestRetainedD3bExpiredMalformedCrossChannelRefusal(t *testing.T) {
	root := retainedD3bPrivateRoot(t)
	_, adapter, err := deskInputAdapter()
	if err != nil {
		t.Fatal(err)
	}
	production := retainedD3bSyntheticProduction(t)
	approvedDigest := "sha256:" + strings.Repeat("b", 64)
	admission := retainedD3bAdmission(t, nil)
	id := "grant-" + strings.Repeat("b", 32)
	expired, err := retainedGrantBytes(time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano), approvedDigest, adapter["digest"], production, admission)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{expired, []byte("{}"), []byte(`{"handoffVersion":2,"handoffVersion":2}`)} {
		if err := writePrivateData(root, id+retainedGrantSuffix, raw); err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		if err := RunInputSourceAdapter(bytes.NewReader(retainedD3bRequest(id, sha256Digest(raw))), &output); err == nil || output.Len() != 0 {
			t.Fatal("expired or malformed selected grant admitted")
		}
	}
	if err := root.Remove(id + retainedGrantSuffix); err != nil {
		t.Fatal(err)
	}
	if err := writePrivateData(root, id+".json", expired); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := RunInputSourceAdapter(bytes.NewReader(retainedD3bRequest(id, sha256Digest(expired))), &output); err == nil || output.Len() != 0 {
		t.Fatal("selected grant in legacy channel admitted")
	}
	if err := root.Remove(id + ".json"); err != nil {
		t.Fatal(err)
	}
	if err := writePrivateData(root, id+retainedGrantSuffix, expired); err != nil {
		t.Fatal(err)
	}
	file, err := root.OpenFile(id+retainedGrantSuffix, os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Chmod(0000); err != nil {
		t.Fatal(err)
	}
	file.Close()
	if err := RunInputSourceAdapter(bytes.NewReader(retainedD3bRequest(id, sha256Digest(expired))), &output); err == nil || output.Len() != 0 {
		t.Fatal("present unreadable selected grant admitted or fell through")
	}
}

func TestRetainedD3bByteGuardHelperLimitsAndOneOver(t *testing.T) {
	for key, ceiling := range retainedRoleCaps("producer-carrier") {
		t.Run(key, func(t *testing.T) {
			lower := retainedRoleCaps("producer-carrier")
			lower[key] = 128
			admission := retainedD3bAdmission(t, lower)
			if err := checkRetainedArtifact(admission, key, bytes.Repeat([]byte{'x'}, 128)); err != nil {
				t.Fatal("exact complete limit refused", err)
			}
			if err := checkRetainedArtifact(admission, key, bytes.Repeat([]byte{'x'}, 129)); retainedContractCode(err) != "input-over-bound" {
				t.Fatal("one-over complete limit admitted", ceiling, err)
			}
		})
	}
	production := retainedD3bSyntheticProduction(t)
	limits := retainedRoleCaps("producer-carrier")
	limits["grantBytes"] = int64(len(production.CarrierJSON))
	if _, err := retainedGrantBytes(time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano), "sha256:"+strings.Repeat("a", 64), "sha256:"+strings.Repeat("b", 64), production, retainedD3bAdmission(t, limits)); retainedContractCode(err) != "input-over-bound" {
		t.Fatal("complete grant overhead was not counted", err)
	}
}

func TestRetainedD3bOrdinaryAdapterProfileBytesAndCapUnchanged(t *testing.T) {
	root := retainedD3bPrivateRoot(t)
	id := "grant-" + strings.Repeat("c", 32)
	result := json.RawMessage(`{"class":"generated","prepared":{"value":"ordinary"}}`)
	digest := sha256Digest(result)
	legacy, _ := json.Marshal(inputHandoff{Expires: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano), Digest: digest, Result: result})
	if err := writePrivateData(root, id+".json", legacy); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := RunInputSourceAdapter(bytes.NewReader(retainedD3bRequest(id, digest)), &output); err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Acquisition struct {
			Adapter map[string]string `json:"adapter"`
		} `json:"acquisition"`
	}
	if json.Unmarshal(output.Bytes(), &envelope) != nil || envelope.Acquisition.Adapter["version"] != "1" {
		t.Fatal("ordinary v1 adapter identity changed")
	}
	tooLarge := append(append([]byte(nil), legacy...), bytes.Repeat([]byte{' '}, inputHandoffLimit-len(legacy)+1)...)
	if err := writePrivateData(root, id+".json", tooLarge); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := RunInputSourceAdapter(bytes.NewReader(retainedD3bRequest(id, digest)), &output); err == nil || output.Len() != 0 {
		t.Fatal("ordinary v1 grant cap widened")
	}
}

func TestRetainedD3bRefusesUnsupportedOrSetAsideBeforeGrant(t *testing.T) {
	selection := retainedInstalledSelector("producer-carrier")
	grammar := retainedProducerGrammar
	capability := retainedConversionCapability{Contract: selection.Contract, Spec: selection.Spec, Profile: selection.Profile, Role: "producer-carrier", Limits: retainedRoleCaps("producer-carrier"), ProducerGrammar: &grammar}
	if _, err := negotiateRetainedRoles(selection, []string{"producer-carrier"}, true, retainedOwnerReports{}, nil); retainedContractCode(err) != "capability-unsupported" {
		t.Fatal("absent trusted producer admitted", err)
	}
	withoutGrammar := capability
	withoutGrammar.ProducerGrammar = nil
	if _, err := negotiateRetainedRoles(selection, []string{"producer-carrier"}, true, retainedOwnerReports{Producer: []retainedConversionCapability{withoutGrammar}}, nil); retainedContractCode(err) != "capability-unsupported" {
		t.Fatal("unsigned producer grammar admitted", err)
	}
	listing, err := readInputProfileListing(json.RawMessage(`[{"profile":{"id":"desk-reviewed-research-v2"},"digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","state":"set-aside","reason":"unsupported"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := listing.installedProfile(retainedReviewedInputProfile, digestIs("sha256:"+strings.Repeat("a", 64))); err == nil {
		t.Fatal("set-aside profile became a selected candidate")
	}
	s, _ := newTestServer(t, false)
	s.localGateway = &localGateway{}
	profile, _, err := retainedPrivateProfile(s)
	if err != nil {
		t.Fatal(err)
	}
	preview := retainedConversionCapability{Contract: selection.Contract, Spec: selection.Spec, Profile: selection.Profile, Role: "preview", Limits: retainedRoleCaps("preview")}
	preparation := retainedConversionCapability{Contract: selection.Contract, Spec: selection.Spec, Profile: selection.Profile, Role: "preparation", Limits: retainedRoleCaps("preparation"), ProducerGrammar: &grammar}
	status, _ := json.Marshal(map[string]any{"capabilities": []string{"selected-profile-preparation"}, "conversionContracts": []retainedConversionCapability{preview, preparation}})
	setAside, _ := json.Marshal([]any{map[string]any{"profile": profile, "digest": "sha256:" + strings.Repeat("a", 64), "state": "set-aside", "reason": "unsupported"}})
	standIn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v1/status":
			w.Write(status)
		case retainedRunnerPrefix + "selected-profiles":
			if r.Method == http.MethodPost {
				json.NewEncoder(w).Encode(map[string]any{"profile": json.RawMessage(profile), "digest": "sha256:" + strings.Repeat("a", 64)})
				return
			}
			w.Write(retainedSelectedTestReport(t, setAside))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer standIn.Close()
	done := make(chan struct{})
	s.jobs = &jobsCompanion{url: standIn.URL, token: "private", done: done, stop: make(chan struct{})}
	// The HTTP stand-in has no process; finish it before Server cleanup.
	t.Cleanup(func() { close(done) })
	if _, err := s.retainedPrivateApprove(context.Background(), nil); retainedContractCode(err) != "capability-unsupported" {
		t.Fatal("set-aside actual listing created private approval", err)
	}
	configured := make([]map[string]any, 15)
	for i := range configured {
		configured[i] = map[string]any{"id": "other-" + strconv.Itoa(i), "source": "other-" + strconv.Itoa(i)}
	}
	s.cfg.RunnerInputProfiles, _ = json.Marshal(configured)
	profiles, _, _, err := s.retainedPrivateProfiles()
	var selectedRows []json.RawMessage
	if err != nil || json.Unmarshal(profiles, &selectedRows) != nil || len(selectedRows) != 1 {
		t.Fatal("full ordinary profile capacity prevented selected document", err)
	}
}

func TestRetainedD3bPrivateTrustedV1V2ProfilesCoexist(t *testing.T) {
	bundle, bin, runtime := os.Getenv("JPS_TEST_GATEWAY_BUNDLE"), os.Getenv("JPACK_RUNNER_TEST_BIN"), os.Getenv("JPACK_BIN")
	if bundle == "" || bin == "" || runtime == "" {
		t.Skip("requires explicitly selected scratch Gateway, Runner and Runtime")
	}
	s, _ := newTestServer(t, false)
	s.localGateway = &localGateway{bundle: bundle, executable: os.Args[0], inputHandoffs: s.inputHandoffPath()}
	t.Cleanup(s.localGateway.close)
	profiles, selected, adapterDigest, err := s.retainedPrivateProfiles()
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := s.reviewedInputProfiles()
	if err != nil {
		t.Fatal(err)
	}
	var onlyOrdinary, withSelected []json.RawMessage
	if json.Unmarshal(ordinary, &onlyOrdinary) != nil || json.Unmarshal(profiles, &withSelected) != nil || len(withSelected) != 1 || bytes.Contains(ordinary, []byte(retainedReviewedInputProfile)) {
		t.Fatal("ordinary startup gained selected profile")
	}
	if !bytes.Contains(selected, []byte(`"version":"2"`)) || !bytes.Contains(selected, []byte(adapterDigest)) {
		t.Fatal("private v2 adapter identity absent")
	}
	// Runner requires an existing state directory to be private; TempDir
	// leaves its leaf permissions to the process umask.
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	j := &jobsCompanion{bin: bin, runtime: runtime, dir: dir, workspace: "retained-d3b", owner: "local-owner", inputRoot: t.TempDir(), profiles: ordinary, stop: make(chan struct{})}
	if _, err := j.start("", ""); err != nil {
		t.Fatal("actual selected Runner refused explicit v1+v2 profiles", err)
	}
	t.Cleanup(j.close)
	s.jobs = j
	approved, err := s.retainedPrivateApprove(context.Background(), nil)
	if err != nil || approved.profileDigest == "" || approved.adapterDigest != adapterDigest {
		t.Fatal("actual Runner did not list approved exact selected profile", err)
	}
}

func TestRetainedD3bActualOwnerGrantAdapterGatewaySignature(t *testing.T) {
	bundle, bin, runtime := os.Getenv("JPS_TEST_GATEWAY_BUNDLE"), os.Getenv("JPACK_RUNNER_TEST_BIN"), os.Getenv("JPACK_BIN")
	if bundle == "" || bin == "" || runtime == "" {
		t.Skip("requires explicitly selected scratch Gateway, Runner and Runtime")
	}
	s, doc, record, row, _ := retainedD3aOwnerFixture(t, bundle)
	s.localGateway = &localGateway{bundle: bundle, executable: os.Args[0], inputHandoffs: s.inputHandoffPath()}
	t.Cleanup(s.localGateway.close)
	profiles, err := s.reviewedInputProfiles()
	if err != nil {
		t.Fatal(err)
	}
	// Runner requires an existing state directory to be private; TempDir
	// leaves its leaf permissions to the process umask.
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	j := &jobsCompanion{bin: bin, runtime: runtime, dir: dir, workspace: "retained-d3b-signing", owner: "local-owner", inputRoot: t.TempDir(), profiles: profiles, stop: make(chan struct{})}
	if _, err := j.start("", ""); err != nil {
		t.Fatal("actual selected Runner refused explicit profiles", err)
	}
	t.Cleanup(j.close)
	s.jobs = j
	approved, err := s.retainedPrivateApprove(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := s.retainedPrivateAcquire(context.Background(), doc, record, row, approved)
	if err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(bundle, executableName("gateway"))
	pin, err := s.localGateway.ensure(s.assistant)
	if err != nil {
		t.Fatal(err)
	}
	retainedD3bVerifySignedResponse(t, program, pin, signed)
	// Exercise the complete actually signed bytes over HTTP at a finite lower
	// ceiling too. Nothing is recorded as an expected fixture from this run.
	retainedD3bAssertHTTPResponseLowerLimit(t, signed.Response)
	if signed.CarrierSHA256 == "" || signed.OriginalSnapshotSHA256 == "" || signed.ProjectedFrameSHA256 == "" || signed.ReceivedCarrierSHA256 == "" || signed.ReceivedFrameSHA256 == "" || signed.OriginalSnapshotSHA256 == signed.ProjectedFrameSHA256 {
		t.Fatal("producer or received digest domains missing or conflated")
	}
}

func retainedD3bVerifySignedResponse(t *testing.T, program string, pin *localGatewayPin, signed retainedPrivateSignedInput) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	response, err := parseRetainedJSON(signed.Response)
	if err != nil {
		t.Fatal(err)
	}
	carrier := response.member("result")
	if carrier == nil || carrier.member("retained") == nil || signed.ReceivedCarrierSHA256 != sha256Digest(carrier.raw) || signed.ReceivedFrameSHA256 != sha256Digest(carrier.member("retained").raw) {
		t.Fatal("received exact carrier or frame token digest changed")
	}
	original, err := retainedMemberString(carrier, "originalSnapshotJson")
	if err != nil || signed.OriginalSnapshotSHA256 != sha256Digest([]byte(original)) {
		t.Fatal("original snapshot digest changed after signing")
	}
	var request struct {
		Session   string          `json:"session"`
		Source    string          `json:"source"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if json.Unmarshal(signed.Request, &request) != nil || request.Source != reviewedInputSource || request.Session == "" {
		t.Fatal("signed Gateway request changed")
	}
	var receipt struct {
		Version     string `json:"receiptVersion"`
		Kind        string `json:"kind"`
		Source      string `json:"source"`
		Session     string `json:"sessionId"`
		Index       int    `json:"callIndex"`
		Digest      string `json:"resultDigest"`
		Commitment  string `json:"argumentsCommitment"`
		Acquisition struct {
			Shape string `json:"shape"`
		} `json:"acquisition"`
	}
	if json.Unmarshal(response.member("receipt").raw, &receipt) != nil || receipt.Version != "3" || receipt.Kind != "acquisition" || receipt.Source != reviewedInputSource || receipt.Session != request.Session || receipt.Index != 0 || receipt.Acquisition.Shape != "mcp" {
		t.Fatal("actual Gateway receipt tuple changed")
	}
	canonicalArguments, err := researchCanon(ctx, program, request.Arguments)
	if err != nil {
		t.Fatal(err)
	}
	var salts map[string]string
	if json.Unmarshal(response.member("salts").raw, &salts) != nil {
		t.Fatal("Gateway salts unavailable")
	}
	salt, err := hexDecode32(salts["args"])
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Commitment != "sha256:"+digestOf(append(append(salt, []byte("args:")...), canonicalArguments...)) {
		t.Fatal("Gateway receipt did not bind exact request arguments")
	}
	canonicalResult, err := researchCanon(ctx, program, response.member("result").raw)
	if err != nil || receipt.Digest != "sha256:"+digestOf(canonicalResult) {
		t.Fatal("Gateway receipt did not bind canonical result", err)
	}
	sealRequest, _ := json.Marshal(map[string]string{"session": request.Session})
	sealHTTP, err := http.NewRequestWithContext(ctx, http.MethodPost, pin.URL+"/seal", bytes.NewReader(sealRequest))
	if err != nil {
		t.Fatal(err)
	}
	sealHTTP.Header.Set("Content-Type", "application/json")
	client := &http.Client{Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	sealResponse, err := client.Do(sealHTTP)
	if err != nil {
		t.Fatal(err)
	}
	defer sealResponse.Body.Close()
	seal, err := io.ReadAll(io.LimitReader(sealResponse.Body, 8193))
	if err != nil || len(seal) > 8192 || sealResponse.StatusCode != http.StatusOK {
		t.Fatal("Gateway seal unavailable", err)
	}
	canonicalSeal, err := researchCanon(ctx, program, seal)
	if err != nil {
		t.Fatal(err)
	}
	store := t.TempDir()
	if err := os.MkdirAll(filepath.Join(store, "artifacts"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(store, "receipts", request.Session), 0700); err != nil {
		t.Fatal(err)
	}
	for path, raw := range map[string][]byte{filepath.Join("artifacts", digestOf(canonicalResult)): canonicalResult, filepath.Join("receipts", request.Session, "0.json"): response.member("receipt").raw, "registry.jsonl": append(canonicalSeal, '\n')} {
		if err := os.WriteFile(filepath.Join(store, path), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	command := exec.CommandContext(ctx, program, "verify", store, filepath.Join(store, "registry.jsonl"), pin.Authority)
	command.Stdin = strings.NewReader(pin.Signer.Public)
	verdict, err := command.Output()
	var verified struct {
		OK bool `json:"ok"`
	}
	if err != nil || json.Unmarshal(verdict, &verified) != nil || !verified.OK {
		t.Fatal("actual Gateway signature verification failed", err, string(verdict))
	}
}

func hexDecode32(raw string) ([]byte, error) {
	if len(raw) != 64 {
		return nil, errors.New("expected 32-byte salt")
	}
	decoded := make([]byte, 32)
	for i := 0; i < 32; i++ {
		var value byte
		for j := 0; j < 2; j++ {
			ch := raw[2*i+j]
			var n byte
			switch {
			case ch >= '0' && ch <= '9':
				n = ch - '0'
			case ch >= 'a' && ch <= 'f':
				n = ch - 'a' + 10
			default:
				return nil, errors.New("noncanonical hex salt")
			}
			value = value<<4 | n
		}
		decoded[i] = value
	}
	return decoded, nil
}

func TestRetainedD3bBoundedReaderHelperBeforeParse(t *testing.T) {
	limits := retainedRoleCaps("producer-carrier")
	limits["responseBytes"] = 128
	admission := retainedD3bAdmission(t, limits)
	// The bounded-reader property is independent of Gateway status/signature.
	raw, err := readRetainedArtifact(strings.NewReader(strings.Repeat("x", 129)), admission, "responseBytes")
	if retainedContractCode(err) != "input-over-bound" || len(raw) != 0 {
		t.Fatal("one-over Gateway response entered parsing or retention")
	}
}

// These are finite production artifacts from the handwritten D3a journal,
// declarations and model action. They do not qualify the D7 maximum family.
func TestRetainedD3bActualOwnerGrantAdapterLowerLimits(t *testing.T) {
	bundle := os.Getenv("JPS_TEST_GATEWAY_BUNDLE")
	if bundle == "" {
		t.Skip("requires explicitly selected scratch Gateway bundle")
	}
	s, doc, record, row, _ := retainedD3aOwnerFixture(t, bundle)
	s.localGateway = &localGateway{}
	production, err := s.retainedReviewedInputSnapshot(context.Background(), doc, record, row)
	if err != nil {
		t.Fatal(err)
	}
	root := retainedD3bPrivateRoot(t)
	_, adapter, err := deskInputAdapter()
	if err != nil {
		t.Fatal(err)
	}
	profileDigest := "sha256:" + strings.Repeat("a", 64)
	expires := time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)
	id := "grant-" + strings.Repeat("d", 32)
	makeGrant := func(limits map[string]int64) ([]byte, error) {
		return retainedGrantBytes(expires, profileDigest, adapter["digest"], production, retainedD3bAdmission(t, limits))
	}
	full, err := makeGrant(nil)
	if err != nil {
		t.Fatal(err)
	}
	adapterRun := func(raw []byte) (*retainedD3bOutput, error) {
		t.Helper()
		if err := writePrivateData(root, id+retainedGrantSuffix, raw); err != nil {
			t.Fatal(err)
		}
		output := &retainedD3bOutput{}
		err := RunInputSourceAdapter(bytes.NewReader(retainedD3bRequest(id, sha256Digest(raw))), output)
		return output, err
	}
	fullOutput, err := adapterRun(full)
	if err != nil || fullOutput.writes != 1 || !bytes.HasSuffix(fullOutput.Bytes(), []byte("}\n")) {
		t.Fatal("complete adapter fixture failed", err)
	}
	// The grant limit includes its own decimal spelling; converge from above
	// using successful serializations rather than assuming metadata overhead.
	grantLimit := int64(len(full))
	for i := 0; i < 10; i++ {
		limits := retainedRoleCaps("producer-carrier")
		limits["grantBytes"] = grantLimit
		raw, err := makeGrant(limits)
		if err != nil {
			t.Fatal(err)
		}
		if int64(len(raw)) == grantLimit {
			break
		}
		grantLimit = int64(len(raw))
	}
	for _, bound := range []struct {
		key   string
		exact int64
	}{
		{"originalSnapshotBytes", int64(len(production.Carrier.OriginalSnapshotJSON))},
		{"carrierBytes", int64(len(production.CarrierJSON))},
		{"grantBytes", grantLimit},
		{"adapterStdoutBytes", int64(fullOutput.Len())},
	} {
		key, exact := bound.key, bound.exact
		t.Run(key, func(t *testing.T) {
			limits := retainedRoleCaps("producer-carrier")
			limits[key] = exact
			grant, err := makeGrant(limits)
			if err != nil {
				t.Fatal("exact complete owner bound refused", err)
			}
			if key == "grantBytes" && int64(len(grant)) != exact {
				t.Fatal("grant bound omitted complete metadata")
			}
			output, err := adapterRun(grant)
			if err != nil || output.writes != 1 || output.Len() != fullOutput.Len() {
				t.Fatal("exact adapter bound refused", err)
			}
			if key == "adapterStdoutBytes" && int64(output.Len()-1) != exact-1 {
				t.Fatal("stdout newline was not counted")
			}
			limits[key] = exact - 1
			if key != "adapterStdoutBytes" {
				if raw, err := makeGrant(limits); err == nil || len(raw) != 0 {
					t.Fatal("one-over actual owner artifact produced a grant")
				}
			}
			// Preserve the actual valid result; only lower one owner ceiling in
			// the closed grant, and bind the whole new grant in the request.
			needle := []byte(`"` + key + `":` + strconv.FormatInt(exact, 10))
			oneOver := bytes.Replace(grant, needle, []byte(`"`+key+`":`+strconv.FormatInt(exact-1, 10)), 1)
			if bytes.Equal(oneOver, grant) {
				t.Fatal("lower-limit fixture did not change")
			}
			if key == "grantBytes" && int64(len(oneOver)) != exact {
				t.Fatal("finite grant fixture is no longer exactly one over")
			}
			output, err = adapterRun(oneOver)
			if err == nil || output.writes != 0 || output.Len() != 0 {
				t.Fatal("one-over actual artifact reached stdout", err)
			}
			if key == "adapterStdoutBytes" {
				return // This owner is checked in the adapter process.
			}
			if key == "grantBytes" {
				// Acquire authors a fresh RFC3339Nano expiry whose spelling may
				// vary by a byte. The exact one-over serialization is proved
				// above; use a clock-independent bound for its no-write check.
				limits[key] = int64(len(production.CarrierJSON))
			}
			approved := retainedPrivateProducerApproval{profileDigest: profileDigest, adapterDigest: adapter["digest"], admission: retainedD3bAdmission(t, limits)}
			approved.limitsDigest, err = retainedPrivateApprovalDigest(approved)
			if err != nil {
				t.Fatal(err)
			}
			if result, err := s.retainedPrivateAcquire(context.Background(), doc, record, row, approved); err == nil || len(result.Response) != 0 {
				t.Fatal("one-over actual acquisition admitted", err)
			}
			entries, err := os.ReadDir(s.inputHandoffPath())
			if err != nil && !os.IsNotExist(err) || len(entries) != 0 {
				t.Fatal("refused owner artifact wrote a private grant", err)
			}
		})
	}
}

type retainedD3bOutput struct {
	bytes.Buffer
	writes int
}

func (w *retainedD3bOutput) Write(raw []byte) (int, error) {
	w.writes++
	return w.Buffer.Write(raw)
}

func retainedD3bRunnerStatus(t *testing.T) []byte {
	t.Helper()
	selection := retainedInstalledSelector("producer-carrier")
	grammar := retainedProducerGrammar
	preview := retainedConversionCapability{Contract: selection.Contract, Spec: selection.Spec, Profile: selection.Profile, Role: "preview", Limits: retainedRoleCaps("preview")}
	preparation := retainedConversionCapability{Contract: selection.Contract, Spec: selection.Spec, Profile: selection.Profile, Role: "preparation", Limits: retainedRoleCaps("preparation"), ProducerGrammar: &grammar}
	raw, err := json.Marshal(map[string]any{"capabilities": []string{"selected-profile-preparation"}, "conversionContracts": []retainedConversionCapability{preview, preparation}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestRetainedD3bAuthenticatedCompleteProfileLowerLimits(t *testing.T) {
	s, _ := newTestServer(t, false)
	s.localGateway = &localGateway{}
	// Explicit configuration is handwritten; retained set-aside rows below
	// model unsupported stored profiles, not padding or installed authority.
	s.cfg.RunnerInputProfiles = json.RawMessage(`[{"id":"other","class":"literal","source":"other","authority":"other","publicKey":"other","shape":"mcp","endpoint":"other","tools":["read"]}]`)
	configured, _, _, err := s.retainedPrivateProfiles()
	if err != nil {
		t.Fatal(err)
	}
	var profiles []json.RawMessage
	if err := json.Unmarshal(configured, &profiles); err != nil {
		t.Fatal(err)
	}
	var listing []map[string]any
	for _, profile := range profiles {
		listing = append(listing, map[string]any{"profile": profile, "digest": "sha256:" + strings.Repeat("a", 64)})
	}
	// Fifteen retained rows plus the selected document fill all 16 selected
	// slots. Their bytes count even though none can become authority.
	for i := 0; i < 15; i++ {
		listing = append(listing, map[string]any{"profile": map[string]any{"id": "retained-" + strconv.Itoa(i), "class": "generated", "source": "retired", "shape": "mcp", "endpointScope": "unsupported-old-scope"}, "digest": "sha256:" + strings.Repeat("b", 64), "state": "set-aside", "reason": "unsupported stored endpoint scope"})
	}
	listingJSON, err := json.Marshal(listing)
	fullListing := retainedSelectedTestReport(t, listingJSON)
	if err != nil || len(fullListing) <= len(configured) {
		t.Fatal("listing fixture must include full retained overhead", err)
	}
	status := retainedD3bRunnerStatus(t)
	var listingBody atomic.Value
	listingBody.Store(fullListing)
	var requests atomic.Int64
	var refuseStatus atomic.Bool
	standIn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		requests.Add(1)
		switch r.URL.Path {
		case "/v1/status":
			if refuseStatus.Load() {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.Write(status)
		case retainedRunnerPrefix + "selected-profiles":
			if r.Method == http.MethodPost {
				json.NewEncoder(w).Encode(map[string]any{"profile": profiles[0], "digest": "sha256:" + strings.Repeat("a", 64)})
				return
			}
			w.Write(listingBody.Load().([]byte))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer standIn.Close()
	done := make(chan struct{})
	s.jobs = &jobsCompanion{url: standIn.URL, token: "private", done: done, stop: make(chan struct{})}
	// The HTTP stand-in has no process; finish it before Server cleanup.
	t.Cleanup(func() { close(done) })
	limits := retainedRoleCaps("producer-carrier")
	// Wrappers exceed the negotiated array limit, but all16 raw profiles fit.
	profileArray := []byte{'['}
	for index, row := range listing {
		if index > 0 {
			profileArray = append(profileArray, ',')
		}
		raw, err := json.Marshal(row["profile"])
		if err != nil {
			t.Fatal(err)
		}
		profileArray = append(profileArray, raw...)
	}
	profileArray = append(profileArray, ']')
	if len(fullListing) <= len(profileArray) {
		t.Fatal("fixture must separate report envelope and profile-array bounds")
	}
	limits["profilesBytes"] = int64(len(profileArray))
	approval, err := s.retainedPrivateApprove(context.Background(), limits)
	if err != nil || approval.admission == nil || requests.Load() != 3 {
		t.Fatal("exact full listing with retained rows refused", err, requests.Load())
	}
	limits["profilesBytes"]--
	if approval.admission.limits["producer-carrier"]["profilesBytes"] != int64(len(profileArray)) {
		t.Fatal("caller mutation changed detached approved ceiling")
	}
	if seal, err := retainedPrivateApprovalDigest(approval); err != nil || seal != approval.limitsDigest {
		t.Fatal("lower limits were not immutable in approval", err)
	}
	if refused, err := s.retainedPrivateApprove(context.Background(), limits); retainedContractCode(err) != "input-over-bound" || refused.admission != nil {
		t.Fatal("one-over full retained listing created approval", err)
	}
	// Count is checked independently of byte fit and includes retained rows.
	listing = append(listing, map[string]any{"profile": map[string]string{"id": "retained-overflow"}, "digest": "sha256:" + strings.Repeat("c", 64), "state": "set-aside", "reason": "unsupported"})
	overflowListing, err := json.Marshal(listing)
	if err != nil {
		t.Fatal(err)
	}
	listingBody.Store(retainedSelectedTestReport(t, overflowListing))
	if refused, err := s.retainedPrivateApprove(context.Background(), nil); err == nil || refused.admission != nil {
		t.Fatal("17-entry full listing created approval")
	}
	// At the exact configured-array ceiling, control-plane discovery is
	// reached. One byte less refuses before any status or listing request.
	refuseStatus.Store(true)
	requests.Store(0)
	limits["profilesBytes"] = int64(len(configured))
	if refused, err := s.retainedPrivateApprove(context.Background(), limits); retainedContractCode(err) != "capability-unsupported" || refused.admission != nil || requests.Load() != 1 {
		t.Fatal("exact complete configured array did not reach status", err, requests.Load())
	}
	requests.Store(0)
	limits["profilesBytes"]--
	if refused, err := s.retainedPrivateApprove(context.Background(), limits); retainedContractCode(err) != "input-over-bound" || refused.admission != nil || requests.Load() != 0 {
		t.Fatal("one-over complete configured array reached Runner", err, requests.Load())
	}
	entries, err := os.ReadDir(s.inputHandoffPath())
	if err != nil && !os.IsNotExist(err) || len(entries) != 0 {
		t.Fatal("profile refusal wrote a private grant", err)
	}
}

func TestRetainedD3bHTTPCompleteResponseLowerLimitBeforeParse(t *testing.T) {
	// Handwritten complete transport envelope. Receipt signature validity is
	// proved by ActualOwnerGrantAdapterGatewaySignature, not this HTTP bound.
	response := []byte(`{"result":{"contractVersion":"1","originalSnapshotJson":"{}","originalSnapshotSha256":"sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a","retained":{"contractVersion":"1","origin":"desk-reviewed-input"}},"receipt":{"receiptVersion":"3","kind":"acquisition","source":"desk-inputs","sessionId":"finite-http-fixture","callIndex":0,"acquisition":{"shape":"mcp"}},"salts":{"args":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`)
	retainedD3bAssertHTTPResponseLowerLimit(t, response)
}

// The same transport-bound assertions cover a handwritten JSON envelope and
// the complete signature-verified Gateway response, without persisting either.
func retainedD3bAssertHTTPResponseLowerLimit(t *testing.T, response []byte) {
	t.Helper()
	if !json.Valid(response) {
		t.Fatal("complete HTTP response is invalid JSON")
	}
	var acquisitions atomic.Int64
	standIn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/publickey":
			json.NewEncoder(w).Encode(map[string]string{"algorithm": "ed25519", "authority": localAuthority, "publicKey": "finite-http-key"})
		case "/acquire":
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			acquisitions.Add(1)
			w.Write(response)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer standIn.Close()
	pin := &localGatewayPin{URL: standIn.URL, Authority: localAuthority, Signer: localSigner{Public: "finite-http-key"}}
	request := []byte(`{"session":"finite-http-fixture","source":"desk-inputs","arguments":{"tool":"read_reviewed_input","arguments":{"grant":"grant-dddddddddddddddddddddddddddddddd","digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}`)
	limits := retainedRoleCaps("producer-carrier")
	limits["responseBytes"] = int64(len(response))
	raw, err := retainedPrivateAcquireBounded(context.Background(), pin, request, retainedD3bAdmission(t, limits))
	if err != nil || !bytes.Equal(raw, response) || acquisitions.Load() != 1 {
		t.Fatal("exact complete HTTP response refused", err)
	}
	if _, err := parseRetainedJSON(raw); err != nil {
		t.Fatal("admitted full HTTP response did not reach parsing", err)
	}
	limits["responseBytes"]--
	raw, err = retainedPrivateAcquireBounded(context.Background(), pin, request, retainedD3bAdmission(t, limits))
	if retainedContractCode(err) != "input-over-bound" || len(raw) != 0 || acquisitions.Load() != 2 {
		t.Fatal("one-over HTTP response entered parsing or retention", err)
	}
}
