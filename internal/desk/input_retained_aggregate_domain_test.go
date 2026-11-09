package desk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
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

// Stream the actual old handler's complete reply to retained evidence. The
// fixture's ceiling refuses a whole write that would exceed it; no clipped
// reply is ever accepted or decoded as a successful witness.
type retainedD7AggregateReply struct {
	header http.Header
	file   *os.File
	hash   hash.Hash
	limit  int64
	bytes  int64
	status int
	err    error
}

func (w *retainedD7AggregateReply) Header() http.Header { return w.header }
func (w *retainedD7AggregateReply) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *retainedD7AggregateReply) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if w.err != nil {
		return 0, w.err
	}
	if int64(len(p)) > w.limit-w.bytes {
		w.err = fmt.Errorf("aggregate fixture reply exceeds %d bytes", w.limit)
		return 0, w.err
	}
	n, err := w.file.Write(p)
	w.hash.Write(p[:n])
	w.bytes += int64(n)
	w.err = err
	return n, err
}

// W3/W6 prerequisite: supported old ordinary /prepare aggregate only. It does
// not execute selected B4 or claim retained-column compatibility. The fixed
// pack/rules, targets, CSV and expected values are handwritten parameters,
// never recordings of Runtime, Runner or the handler's output.
func TestRetainedD7ActualOldPrepareAggregateDomain(t *testing.T) {
	bin, runtime := os.Getenv("JPACK_RUNNER_TEST_BIN"), os.Getenv("JPACK_BIN")
	evidenceParent := os.Getenv("JPS_TEST_RETAINED_AGGREGATE_EVIDENCE_DIR")
	if bin == "" || runtime == "" || evidenceParent == "" {
		t.Skip("requires explicitly verified scratch Runner/Runtime and an evidence directory; skip is not qualification")
	}
	for _, path := range []string{bin, runtime, evidenceParent} {
		if !filepath.IsAbs(path) {
			t.Fatal("explicit absolute scratch paths required")
		}
	}
	evidence, err := os.MkdirTemp(evidenceParent, "retained-d7-aggregate-")
	if err != nil {
		t.Fatal(err)
	}
	// Keep evidence on failure too. Parent owns its retention and resource guard.
	t.Log("complete aggregate witness artifacts:", evidence)
	mustWrite := func(name string, raw []byte) {
		t.Helper()
		if len(raw) > 4<<20 {
			t.Fatal("small witness artifact exceeded 4 MiB", name)
		}
		if err := os.WriteFile(filepath.Join(evidence, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
	defer cancel()
	s, _ := newTestServer(t, false)
	s.cfg.JpackBin = runtime
	token := policyTestSession(t, s)
	const rowCount, targetCount, cellBytes = 40, 256, 2000
	cell := strings.Repeat("v", cellBytes)
	csv := "value\n" + strings.Repeat(cell+"\n", rowCount)
	facts := make([]inputDialectFact, 0, targetCount)
	rules := make([]map[string]any, 0, targetCount)
	conversions := map[string]inputConversion{}
	targets := make([]string, 0, targetCount)
	for i := 0; i < targetCount; i++ {
		target := fmt.Sprintf("/witness/fact%03d", i)
		targets = append(targets, target)
		facts = append(facts, inputDialectFact{Target: target, Source: "/values/value"})
		conversions[target] = inputConversion{Kind: "text"}
		// Each declared read uses the same core fact-rule grammar as the
		// qualified existing Runtime fixture. Actual public save below must
		// pass Runtime's complete mapping and pack coverage checks.
		rules = append(rules, map[string]any{"id": fmt.Sprintf("rule%03d", i), "description": "Declared aggregate witness read", "when": map[string]any{"op": "fact", "path": target, "operator": "equals", "value": "v"}, "outcome": "ready", "onUnknown": "escalate"})
	}
	pack, err := json.Marshal(map[string]any{"specVersion": "0.2.0-draft", "id": "https://example.invalid/pack/aggregate-domain", "version": "1.0.0", "title": "Old aggregate domain witness", "decision": map[string]string{"intent": "Declare every aggregate witness target", "question": "Ready?"}, "outcomes": []map[string]string{{"id": "ready", "label": "Ready"}, {"id": "other", "label": "Other"}}, "rules": rules, "evidenceRequirements": []any{}})
	if err != nil {
		t.Fatal(err)
	}
	q := &inputQualification{Kind: "pack", Key: "pack.json", Pack: string(pack), Digest: retainedDigest(pack), Mapping: inputRecordDialect{Version: 2, Case: &inputCaseDialect{Facts: facts, Evidence: []inputDialectEvidence{}}, Sources: []json.RawMessage{}}, Conversions: conversions}
	if err := validateInputQualification(q); err != nil {
		t.Fatal("fixed aggregate family is not a valid ordinary Desk qualification", err)
	}
	mapping, err := json.Marshal(q.Mapping)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite("pack.json", pack)
	mustWrite("mapping.json", mapping)
	mustWrite("fresh-source.csv", []byte(csv))
	// Exactly four real application handlers: create, fresh import, and the
	// one-row/forty-row old prepare calls. Their ordinary Runtime checks are
	// real subprocess calls, not stub reports or preaccepted mappings.
	apiCalls := 0
	call := func(name, path, revision string, body any, expectedStatus int, limit int64) (string, int64, string) {
		t.Helper()
		apiCalls++
		if apiCalls > 4 || ctx.Err() != nil {
			t.Fatal("aggregate fixture application request/time bound", apiCalls, ctx.Err())
		}
		raw, err := json.Marshal(body)
		if err != nil || len(raw) > 2<<20 {
			t.Fatal("aggregate fixture request bound", name, err)
		}
		mustWrite(name+"-request.json", raw)
		replyPath := filepath.Join(evidence, name+"-reply.json")
		file, err := os.OpenFile(replyPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		w := &retainedD7AggregateReply{header: make(http.Header), file: file, hash: sha256.New(), limit: limit}
		r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw)).WithContext(ctx)
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		if revision != "" {
			r.Header.Set("If-Match", revision)
		}
		s.ServeHTTP(w, r)
		closeErr := file.Close()
		digest := "sha256:" + hex.EncodeToString(w.hash.Sum(nil))
		metadata, _ := json.Marshal(map[string]any{"path": path, "status": w.status, "bytes": w.bytes, "sha256": digest, "completeWithinFixtureBound": w.err == nil && closeErr == nil})
		mustWrite(name+"-reply-metadata.json", metadata)
		if w.err != nil || closeErr != nil || w.status != expectedStatus || ctx.Err() != nil {
			t.Fatal("actual old handler refused/failed; preserve evidence and reassess family", name, w.status, w.err, closeErr, ctx.Err())
		}
		return replyPath, w.bytes, digest
	}
	readSaved := func(path string) inputPreparationReply {
		t.Helper()
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		var saved inputPreparationReply
		if err := json.NewDecoder(io.LimitReader(file, (2<<20)+1)).Decode(&saved); err != nil {
			t.Fatal("public saved collection reply", err)
		}
		return saved
	}
	createdPath, _, _ := call("create", "/api/input-preparations", "", map[string]any{"name": "Old aggregate domain", "recipe": inputRecipe{Intent: "Fixed forty-row aggregate", Qualification: q}}, http.StatusCreated, 2<<20)
	created := readSaved(createdPath)
	if !inputPreparationID.MatchString(created.Document.ID) || len(created.Document.Records) != 0 {
		t.Fatal("public create identity/empty collection")
	}
	path := "/api/input-preparations/" + created.Document.ID
	importPath, _, _ := call("import", path+"/imports", created.SHA256, map[string]string{"kind": "csv", "name": "aggregate.csv", "content": csv}, http.StatusOK, 2<<20)
	imported := readSaved(importPath)
	saved, err := s.preparationRepository().Get(ctx, created.Document.ID)
	if err != nil || saved.SHA256 != imported.SHA256 || len(saved.Document.Records) != rowCount || len(saved.Document.Imports) != 1 || saved.Document.Imports[0].Content != csv || saved.Document.Imports[0].Digest != retainedDigest([]byte(csv)) {
		t.Fatal("fresh public import/CAS/readback lost exact source", err)
	}
	collection, err := json.Marshal(saved.Document)
	if err != nil || saved.SHA256 != digestOf(collection) {
		t.Fatal("saved collection byte identity", err)
	}
	mustWrite("saved-collection.json", collection)
	ids := make([]string, rowCount)
	for i, record := range saved.Document.Records {
		if record.Row != i+1 || record.SourceID != saved.Document.Imports[0].ID || record.Class != "asserted" || len(record.Values) != 1 || record.Values["value"] != cell {
			t.Fatal("fresh public import changed fixed row", i)
		}
		ids[i] = record.ID
	}
	// Reverse the aggregate selection to verify exact request order as well as
	// cardinality and membership; identical cells do not permit dropped rows.
	aggregateIDs := make([]string, rowCount)
	for i := range ids {
		aggregateIDs[i] = ids[len(ids)-1-i]
	}
	idsRaw, _ := json.Marshal(map[string]any{"control": ids[:1], "aggregate": aggregateIDs})
	mustWrite("requested-ids.json", idsRaw)
	var representative json.RawMessage
	var controlBytes int64
	measurements := []map[string]any{}
	for _, selection := range []struct {
		name string
		ids  []string
	}{{"prepare-one", ids[:1]}, {"prepare-forty", aggregateIDs}} {
		replyPath, replyBytes, replyDigest := call(selection.name, path+"/prepare", saved.SHA256, map[string]any{"recordIds": selection.ids}, http.StatusOK, 64<<20)
		file, err := os.Open(replyPath)
		if err != nil {
			t.Fatal(err)
		}
		decoder := json.NewDecoder(file)
		wantToken := func(want json.Token) {
			t.Helper()
			got, err := decoder.Token()
			if err != nil || got != want {
				file.Close()
				t.Fatal("complete old prepare envelope", selection.name, got, want, err)
			}
		}
		wantToken(json.Delim('{'))
		wantToken("rows")
		wantToken(json.Delim('['))
		returnedIDs := []string{}
		maxInputBytes := 0
		for decoder.More() {
			var row inputPreparedRow
			if err := decoder.Decode(&row); err != nil {
				file.Close()
				t.Fatal("complete old prepare row", selection.name, err)
			}
			if len(returnedIDs) >= len(selection.ids) || row.ID != selection.ids[len(returnedIDs)] || row.Origin != "asserted" || len(row.Issues) != 0 || len(row.Original) != 1 || row.Original["value"] != cell || len(row.Prepared) != targetCount || len(row.Input) == 0 || len(row.Input) > 2<<20 {
				file.Close()
				t.Fatal("old prepare row identity/complete per-row domain", selection.name, len(returnedIDs), row.ID, len(row.Input))
			}
			input, err := parseRetainedJSON(row.Input)
			if err != nil || input.member("source").member("case").member("origin").member("originalValues").member("value") == nil {
				file.Close()
				t.Fatal("old prepare source custody", selection.name, err)
			}
			sourceCase := input.member("source").member("case")
			for _, values := range []*retainedJSONNode{sourceCase.member("values"), sourceCase.member("origin").member("originalValues")} {
				value, err := values.member("value").scalarString()
				if err != nil || len(values.members) != 1 || value != cell {
					file.Close()
					t.Fatal("old complete original cell siblings", selection.name, err)
				}
			}
			recordID, _ := retainedMemberString(sourceCase.member("origin"), "recordId")
			sourceDigest, _ := retainedMemberString(sourceCase.member("origin"), "sourceDigest")
			mappingDigest, _ := retainedMemberString(input.member("source"), "mappingDigest")
			if recordID != row.ID || sourceDigest != retainedDigest([]byte(csv)) || !retainedSpecDigest.MatchString(mappingDigest) {
				file.Close()
				t.Fatal("old source identities or actual Runtime mapping digest absent", selection.name)
			}
			prepared := sourceCase.member("prepared")
			if prepared == nil || len(prepared.members) != targetCount {
				file.Close()
				t.Fatal("old complete source prepared siblings", selection.name)
			}
			for _, target := range targets {
				v, err := prepared.member(target).scalarString()
				if row.Prepared[target] != cell || err != nil || v != cell {
					file.Close()
					t.Fatal("old prepared value not copied intact", selection.name, target, err)
				}
			}
			if len(row.Input) > maxInputBytes {
				maxInputBytes = len(row.Input)
			}
			if representative == nil {
				representative = bytes.Clone(row.Input)
			}
			returnedIDs = append(returnedIDs, row.ID)
		}
		wantToken(json.Delim(']'))
		wantToken(json.Delim('}'))
		if decoder.Decode(new(any)) != io.EOF {
			file.Close()
			t.Fatal("old prepare reply had trailing data", selection.name)
		}
		file.Close()
		if !reflect.DeepEqual(returnedIDs, selection.ids) {
			t.Fatal("old prepare exact requested/returned IDs", selection.name)
		}
		if len(selection.ids) == 1 {
			controlBytes = replyBytes
		} else if replyBytes <= 37748736 || replyBytes <= controlBytes {
			t.Fatal("fixed old aggregate did not exceed selected per-row reply cap; reassess witness", replyBytes, controlBytes)
		}
		measurements = append(measurements, map[string]any{"selection": selection.name, "requestedIds": selection.ids, "returnedIds": returnedIDs, "replyBytes": replyBytes, "replySHA256": replyDigest, "maxCompletePerRowInputBytes": maxInputBytes})
		t.Logf("old aggregate %s: rows=%d replyBytes=%d SHA256=%s maxPerRowInputBytes=%d", selection.name, len(returnedIDs), replyBytes, replyDigest, maxInputBytes)
	}
	// One real ordinary Runner preparation suffices for the identical mapping
	// and semantic row data. No forty-row Runner loop or selected route exists.
	mustWrite("representative-old-input.json", representative)
	state, inputRoot := t.TempDir(), t.TempDir()
	for _, dir := range []string{state, inputRoot} {
		if err := os.Chmod(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	j := &jobsCompanion{bin: bin, runtime: runtime, dir: state, workspace: "retained-d7-aggregate", owner: "local-owner", inputRoot: inputRoot, profiles: []byte(`[]`), stop: make(chan struct{})}
	if _, err := j.start("", ""); err != nil {
		t.Fatal("actual ordinary Runner start", err)
	}
	t.Cleanup(j.close)
	endpoint, runnerToken, err := j.endpoint()
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
	var runnerCalls atomic.Int64
	// Observe the complete reply under the stricter existing product reader.
	// This changes no product capacity or other fixture artifact/request limit.
	const representativeReplyLimit = min(runnerAnswerLimit, runnerExportLimit)
	proxy.ModifyResponse = func(resp *http.Response) error {
		raw, err := io.ReadAll(io.LimitReader(resp.Body, int64(representativeReplyLimit)+1))
		resp.Body.Close()
		if err != nil {
			return err
		}
		if len(raw) > representativeReplyLimit {
			return fmt.Errorf("representative ordinary Runner reply exceeds existing reader limit of %d bytes", representativeReplyLimit)
		}
		if err := os.WriteFile(filepath.Join(evidence, "runner-reply.json"), raw, 0600); err != nil {
			return err
		}
		metadata := map[string]any{"status": resp.StatusCode, "bytes": len(raw), "sha256": retainedDigest(raw), "completeWithinObservationBound": true, "observationLimitBytes": representativeReplyLimit, "browserReplyLimitBytes": runnerAnswerLimit, "inputRunnerReplyLimitBytes": runnerExportLimit}
		if reply, err := parseRetainedJSON(raw); err == nil {
			if preparation := reply.member("input").member("preparation"); preparation != nil {
				metadata["completePreparationBytes"] = len(preparation.raw)
				if lineage := preparation.member("lineage"); lineage != nil && lineage.kind == '[' {
					metadata["lineageEntries"] = len(lineage.elements)
				}
				if outcomes := preparation.member("outcomes"); outcomes != nil && outcomes.kind == '[' {
					metadata["outcomeEntries"] = len(outcomes.elements)
				}
			}
		}
		metadataJSON, _ := json.Marshal(metadata)
		if err := os.WriteFile(filepath.Join(evidence, "runner-reply-metadata.json"), metadataJSON, 0600); err != nil {
			return err
		}
		resp.Body = io.NopCloser(bytes.NewReader(raw))
		return nil
	}
	observer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if runnerCalls.Add(1) > 1 || r.Method != http.MethodPost || r.URL.Path != "/v1/inputs/next" {
			t.Error("aggregate witness attempted another Runner request", r.Method, r.URL.Path)
			w.WriteHeader(500)
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, (2<<20)+1))
		r.Body.Close()
		if err != nil || len(raw) > 2<<20 {
			t.Error("representative ordinary request bound/read", err)
			w.WriteHeader(500)
			return
		}
		if err := os.WriteFile(filepath.Join(evidence, "runner-request.json"), raw, 0600); err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(observer.Close)
	done := make(chan struct{})
	s.jobs = &jobsCompanion{url: observer.URL, token: runnerToken, done: done, stop: make(chan struct{})}
	t.Cleanup(func() { close(done) })
	reply, err := s.inputRunner(ctx, "inputs/next", representative, "")
	if err != nil {
		t.Fatal("actual representative ordinary Runner preparation", err)
	}
	result, err := parseRetainedJSON(reply)
	prepared := result.member("input")
	if err != nil || prepared.member("preparation") == nil || prepared.member("facts").member("witness") == nil || len(prepared.member("facts").member("witness").members) != targetCount {
		t.Fatal("representative ordinary Runner facts/preparation", err)
	}
	for i := 0; i < targetCount; i++ {
		value, err := prepared.member("facts").member("witness").member(fmt.Sprintf("fact%03d", i)).scalarString()
		if err != nil || value != cell {
			t.Fatal("ordinary Runner changed actual target value", i, err)
		}
	}
	unchanged, err := s.preparationRepository().Get(ctx, saved.Document.ID)
	if err != nil || unchanged.SHA256 != saved.SHA256 || apiCalls != 4 || runnerCalls.Load() != 1 || s.localGateway != nil {
		t.Fatal("old aggregate changed storage or exceeded operation ownership", err, apiCalls, runnerCalls.Load())
	}
	// Each ordinary authoring call runs contract+check. Public create checks
	// once, and each /prepare checks the single identical produced mapping once
	// through checkPreparedMappings' request-local map: six direct CLI calls.
	// Runner startup/internal Runtime calls are not an instrumented process cap.
	summary, _ := json.Marshal(map[string]any{"scope": "actual supported old aggregate; selected B4 not exercised", "rows": rowCount, "targets": targetCount, "cellBytes": cellBytes, "publicFreshCSVImport": true, "savedCollectionSHA256": saved.SHA256, "packSHA256": retainedDigest(pack), "measurements": measurements, "applicationHandlerCalls": apiCalls, "observedRunnerHTTPCalls": runnerCalls.Load(), "directRuntimeAuthoringCallsFromSource": 6, "totalAuxiliaryProcessCount": "not measured", "selectedCompatibilityQualified": false})
	mustWrite("classification.json", summary)
}
