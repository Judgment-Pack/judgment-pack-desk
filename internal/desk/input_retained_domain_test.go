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
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// D7 W1/W5 are classification sentinels. A passing test preserves known
// compatibility debt; it does not qualify selected parity or a public import.
// One owned Runner handles one listing and four ordinary preparations. The
// Gateway value enables the ordinary installed v1 identity only: no service,
// acquisition, grant, model, or external source is invoked.
func TestRetainedD7ActualSmallDomainClassification(t *testing.T) {
	bin, runtime := os.Getenv("JPACK_RUNNER_TEST_BIN"), os.Getenv("JPACK_BIN")
	evidenceParent := os.Getenv("JPS_TEST_RETAINED_DOMAIN_EVIDENCE_DIR")
	if bin == "" || runtime == "" || evidenceParent == "" {
		t.Skip("requires explicitly verified scratch Runner/Runtime and an evidence directory; skip is not qualification")
	}
	for _, path := range []string{bin, runtime, evidenceParent} {
		if !filepath.IsAbs(path) {
			t.Fatal("explicit absolute scratch paths required")
		}
	}
	evidence, err := os.MkdirTemp(evidenceParent, "retained-d7-domain-")
	if err != nil {
		t.Fatal(err)
	}
	// Deliberately retained for the parent's evidence ledger, including failure.
	t.Log("complete bounded witness artifacts:", evidence)
	writeEvidence := func(name string, raw []byte) error {
		if len(raw) > 256<<10 {
			return fmt.Errorf("witness artifact exceeds 256 KiB: %s", name)
		}
		return os.WriteFile(filepath.Join(evidence, name), raw, 0600)
	}
	mustWrite := func(name string, raw []byte) {
		t.Helper()
		if err := writeEvidence(name, raw); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
	defer cancel()
	s, _ := newTestServer(t, false)
	s.cfg.JpackBin = runtime
	s.localGateway = &localGateway{} // identity construction only; never ensure/start
	// The known lawful record-profile shape is the existing actual jobs fixture.
	// All names and policy members below are handwritten; only installed v1's
	// local key/executable identity comes from its ordinary owner.
	names := []string{"witness01", "witness02", "witness03", "witness04", "witness05", "witness06", "witness07", "witness08", "witness09", "witness10", "witness11", "witness12", "witness13", "witness14", "witness15"}
	configured := make([]map[string]any, 0, 15)
	for _, name := range names {
		configured = append(configured, map[string]any{"id": name, "publicKey": "58" + strings.Repeat("66", 31), "class": "record", "source": name, "authority": "test", "shape": "mcp", "adapter": map[string]string{"name": "mcp", "version": "1", "digest": "sha256:" + strings.Repeat("a", 64)}, "endpoint": nil, "tools": []string{"lookup"}})
	}
	s.cfg.RunnerInputProfiles, err = json.Marshal(configured)
	if err != nil {
		t.Fatal(err)
	}
	configuredBefore := bytes.Clone(s.cfg.RunnerInputProfiles)
	ordinary, err := s.reviewedInputProfiles()
	if err != nil {
		t.Fatal(err)
	}
	var ordinaryProfiles []json.RawMessage
	if json.Unmarshal(ordinary, &ordinaryProfiles) != nil || len(ordinaryProfiles) != 16 {
		t.Fatal("expected fifteen configured profiles plus ordinary installed v1")
	}
	mustWrite("configured-profiles.json", configuredBefore)
	mustWrite("ordinary-profiles.json", ordinary)
	state, inputRoot := t.TempDir(), t.TempDir()
	for _, dir := range []string{state, inputRoot} {
		if err := os.Chmod(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	j := &jobsCompanion{bin: bin, runtime: runtime, dir: state, workspace: "retained-d7-domain", owner: "local-owner", inputRoot: inputRoot, profiles: ordinary, stop: make(chan struct{})}
	if _, err := j.start("", ""); err != nil {
		t.Fatal("actual ordinary sixteen-profile registry admission", err)
	}
	t.Cleanup(j.close)
	endpoint, token, err := j.endpoint()
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
	var calls, listings, preparations atomic.Int64
	type observationKey struct{}
	proxy.ModifyResponse = func(resp *http.Response) error {
		raw, err := io.ReadAll(io.LimitReader(resp.Body, (256<<10)+1))
		resp.Body.Close()
		if err != nil {
			return err
		}
		index, _ := resp.Request.Context().Value(observationKey{}).(int64)
		if err := writeEvidence(fmt.Sprintf("http-%d-response.json", index), raw); err != nil {
			return err
		}
		metadata, _ := json.Marshal(map[string]any{"status": resp.StatusCode, "path": resp.Request.URL.Path, "bytes": len(raw), "sha256": retainedDigest(raw)})
		if err := writeEvidence(fmt.Sprintf("http-%d-response-metadata.json", index), metadata); err != nil {
			return err
		}
		resp.Body = io.NopCloser(bytes.NewReader(raw))
		return nil
	}
	observer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		index := calls.Add(1)
		allowed := index <= 5
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/input-profiles":
			allowed = allowed && listings.Add(1) <= 1
		case r.Method == http.MethodPost && r.URL.Path == "/v1/inputs/next":
			allowed = allowed && preparations.Add(1) <= 4
		default:
			allowed = false
		}
		if !allowed {
			t.Error("ordinary witness attempted another Runner route/request", r.Method, r.URL.Path)
			w.WriteHeader(500)
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, (256<<10)+1))
		r.Body.Close()
		if err == nil {
			err = writeEvidence(fmt.Sprintf("http-%d-request.json", index), raw)
		}
		if err != nil {
			t.Error("bounded request observation", err)
			w.WriteHeader(500)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		proxy.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), observationKey{}, index)))
	}))
	t.Cleanup(observer.Close)
	done := make(chan struct{})
	s.jobs = &jobsCompanion{url: observer.URL, token: token, done: done, stop: make(chan struct{})}
	t.Cleanup(func() { close(done) })
	listingRaw, err := s.inputRunner(ctx, "input-profiles", nil, "")
	if err != nil {
		t.Fatal("ordinary installed profile listing", err)
	}
	listing, err := readInputProfileListing(listingRaw)
	if err != nil || len(listing) != 16 {
		t.Fatal("ordinary profile listing count", len(listing), err)
	}
	expectedProfiles := map[string]*retainedJSONNode{}
	for _, raw := range ordinaryProfiles {
		node, err := parseRetainedJSON(raw)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := retainedMemberString(node, "id")
		expectedProfiles[id] = node
	}
	identities := map[string]string{}
	for _, profile := range listing {
		if !profile.installed() || !retainedSpecDigest.MatchString(profile.Digest) || identities[profile.id] != "" {
			t.Fatal("ordinary profile not admitted exactly once", profile.id)
		}
		actual, err := parseRetainedJSON(profile.Profile)
		if err != nil || !retainedJSONEqual(actual, expectedProfiles[profile.id]) {
			t.Fatal("ordinary installed profile bytes changed semantically", profile.id, err)
		}
		identities[profile.id] = profile.Digest
	}
	for _, name := range append(append([]string{}, names...), reviewedInputProfile) {
		if identities[name] == "" {
			t.Fatal("ordinary profile missing", name)
		}
	}
	identitiesRaw, _ := json.Marshal(identities)
	mustWrite("ordinary-profile-digests.json", identitiesRaw)
	// Measure the selected producer-profile construction before any grant or
	// acquisition. Preserve its source-known refusal as unresolved W1 debt.
	profiles, selected, adapter, selectedErr := s.retainedPrivateProfiles()
	if retainedContractCode(selectedErr) != "capability-unsupported" || len(profiles) != 0 || len(selected) != 0 || adapter != "" {
		t.Fatal("W1 expected full-registry selected refusal changed; reassess compatibility", selectedErr)
	}
	ordinaryAfter, err := s.reviewedInputProfiles()
	if err != nil || !bytes.Equal(ordinaryAfter, ordinary) || !bytes.Equal(s.cfg.RunnerInputProfiles, configuredBefore) || !bytes.Equal(j.profiles, ordinary) {
		t.Fatal("selected refusal changed the intact ordinary registry", err)
	}
	w1, _ := json.Marshal(map[string]any{"witness": "W1", "ordinaryProfiles": 16, "listingSHA256": retainedDigest(listingRaw), "selectedProfilesCode": retainedContractCode(selectedErr), "disposition": "compatibility debt; selected parity not qualified", "generatedPreparation": "not exercised", "v2Grants": 0, "acquisitions": 0})
	mustWrite("w1-classification.json", w1)
	t.Logf("W1 compatibility debt: ordinary count=16 listingSHA256=%s selectedProfiles=capability-unsupported; no v2 grant/acquisition", retainedDigest(listingRaw))

	// The CSV documents describe each exact row. Invalid imports remain invalid:
	// saving a synthetic historical row via CAS is storage classification only.
	// No historical supported public ingress or generated preparation is claimed.
	largeCell := strings.Repeat("v", 16001)
	longColumn := strings.Repeat("c", 161)
	manyHeaders, manyCells := []string{"Country", "License"}, []string{"CA", "absent"}
	manyValues := map[string]string{"Country": "CA", "License": "absent"}
	for i := 0; i < 999; i++ {
		name := fmt.Sprintf("cell%03d", i)
		manyHeaders = append(manyHeaders, name)
		manyCells = append(manyCells, "v")
		manyValues[name] = "v"
	}
	cases := []struct {
		name, id, csv, selectedCode string
		values                      map[string]string
		publicImport                bool
	}{
		{"ordinary-control", "1", "Country,License\nCA,absent\n", "", map[string]string{"Country": "CA", "License": "absent"}, true},
		{"unselected-cell-16001", "2", "Country,License,unselected\nCA,absent," + largeCell + "\n", "input-over-bound", map[string]string{"Country": "CA", "License": "absent", "unselected": largeCell}, false},
		{"column-name-161", "3", "Country,License," + longColumn + "\nCA,absent,v\n", "input-malformed", map[string]string{"Country": "CA", "License": "absent", longColumn: "v"}, false},
		{"short-cells-1001", "4", strings.Join(manyHeaders, ",") + "\n" + strings.Join(manyCells, ",") + "\n", "input-over-bound", manyValues, false},
	}
	for _, c := range cases {
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
		sourceID := "source_" + digestOf([]byte("csv\n"+c.csv))
		imported, importErr := normalizeInputImport("csv", c.csv, sourceID)
		if c.publicImport {
			if importErr != nil || len(imported) != 1 || !reflect.DeepEqual(imported[0].Values, c.values) {
				t.Fatal("ordinary control public import", importErr)
			}
		} else if importErr == nil {
			t.Fatal("historical storage shape unexpectedly accepted at current public CSV ingress", c.name)
		}
		q := qualificationFixture()
		q.Pack, q.Digest = retainedRuntimeTestPack, retainedDigest([]byte(retainedRuntimeTestPack))
		q.Conversions = map[string]inputConversion{"/company/country": {Kind: "text"}}
		doc := inputPreparation{Version: 1, ID: "prep_" + strings.Repeat(c.id, 32), Name: c.name, Recipe: inputRecipe{Intent: "Small domain classification", Qualification: q}, RecipeVersion: 1, CreatedAt: d0aStamp, UpdatedAt: d0aStamp, Records: []preparedInputRecord{{ID: "row-domain", Name: "Example", Values: c.values, SourceID: sourceID, Row: 1, Class: "asserted", Status: "provided"}}, Imports: []inputImport{{ID: sourceID, Kind: "csv", Name: "domain.csv", Digest: retainedDigest([]byte(c.csv)), Content: c.csv, At: d0aStamp, RecipeVersion: 1}}, Activity: []inputActivity{}}
		if c.publicImport {
			doc.Records = imported
		}
		before, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		mustWrite(c.name+"-collection.json", before)
		saved, err := s.preparationRepository().CompareAndSwap(ctx, doc, "")
		if err != nil {
			t.Fatal("bounded historical collection CAS", c.name, err)
		}
		reloaded, err := s.preparationRepository().Get(ctx, doc.ID)
		if err != nil || reloaded.SHA256 != saved.SHA256 {
			t.Fatal("historical collection readback", c.name, err)
		}
		after, err := json.Marshal(reloaded.Document)
		if err != nil || !bytes.Equal(before, after) || saved.SHA256 != digestOf(before) {
			t.Fatal("historical collection changed on CAS/readback", c.name, err)
		}
		record := reloaded.Document.Records[0]
		rows, err := prepareInputSelection(reloaded.Document, []string{record.ID})
		if err != nil || len(rows) != 1 || len(rows[0].Issues) != 0 || !reflect.DeepEqual(rows[0].Original, c.values) {
			t.Fatal("ordinary historical direct preparation", c.name, err)
		}
		// This is the ordinary actual Runtime boundary, including canonical
		// mapping identity; then the actual ordinary Runner preparation route.
		if err := s.checkPreparedMappings(ctx, reloaded.Document, rows); err != nil {
			t.Fatal("ordinary actual Runtime mapping admission", c.name, err)
		}
		mustWrite(c.name+"-ordinary-input-request.json", rows[0].Input)
		reply, err := s.inputRunner(ctx, "inputs/next", rows[0].Input, "")
		if err != nil {
			t.Fatal("ordinary actual Runner preparation", c.name, err)
		}
		mustWrite(c.name+"-ordinary-input-reply.json", reply)
		root, err := parseRetainedJSON(reply)
		if err != nil || root.member("input").member("preparation") == nil {
			t.Fatal("ordinary actual preparation absent", c.name, err)
		}
		wantCells, _ := json.Marshal(c.values)
		want, _ := parseRetainedJSON(wantCells)
		original := root.member("input").member("source").member("case").member("origin").member("originalValues")
		if !retainedJSONEqual(original, want) {
			t.Fatal("ordinary actual preparation lost original cells", c.name)
		}
		if record.rawCells == nil {
			t.Fatal("actual storage decoder did not retain cell custody", c.name)
		}
		_, selectedErr := retainedAssertedCells(record.rawCells.raw)
		if retainedContractCode(selectedErr) != c.selectedCode {
			t.Fatal("selected frame predicate classification changed", c.name, selectedErr)
		}
		storedAfter, err := s.preparationRepository().Get(ctx, doc.ID)
		if err != nil || storedAfter.SHA256 != saved.SHA256 {
			t.Fatal("ordinary preparation changed saved historical collection", c.name, err)
		}
		classification, _ := json.Marshal(map[string]any{"witness": "W5", "case": c.name, "storageCASReadback": true, "currentPublicCSVImport": c.publicImport, "historicalPublicIngressProven": false, "ordinaryRuntimeRunnerPreparation": true, "selectedFrameCode": c.selectedCode, "selectedPreparation": "not exercised", "counterexampleClaim": false, "storedSHA256": saved.SHA256, "sourceSHA256": retainedDigest([]byte(c.csv)), "replySHA256": retainedDigest(reply)})
		mustWrite(c.name+"-classification.json", classification)
		t.Logf("W5 %s: storageCAS/readback=true publicCSVImport=%t ordinaryRuntime/Runner=true selectedFrameCode=%q storedSHA256=%s requestBytes=%d replyBytes=%d replySHA256=%s", c.name, c.publicImport, c.selectedCode, saved.SHA256, len(rows[0].Input), len(reply), retainedDigest(reply))
	}
	if calls.Load() != 5 || listings.Load() != 1 || preparations.Load() != 4 {
		t.Fatal("ordinary witness call counts", calls.Load(), listings.Load(), preparations.Load())
	}
	if s.localGateway.cmd != nil || s.localGateway.pin != nil {
		t.Fatal("classification started a Gateway")
	}
}
