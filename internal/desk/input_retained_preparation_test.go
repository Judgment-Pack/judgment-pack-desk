package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func retainedD4aReports() retainedOwnerReports {
	selected := retainedInstalledSelector()
	grammar := retainedProducerGrammar
	report := retainedOwnerReports{}
	for _, role := range retainedPreparationRoles() {
		entry := retainedConversionCapability{Contract: selected.Contract, Spec: selected.Spec, Profile: selected.Profile, Role: role, Limits: retainedRoleCaps(role)}
		if retainedGrammarRole(role) {
			entry.ProducerGrammar = &grammar
		}
		switch role {
		case "document-validation":
			report.Runtime = append(report.Runtime, entry)
		case "producer-carrier":
			report.Producer = append(report.Producer, entry)
		default:
			report.Runner = append(report.Runner, entry)
		}
	}
	return report
}

func retainedD4aAdmission(t *testing.T, lower map[string]map[string]int64) *retainedAdmission {
	t.Helper()
	admission, err := negotiateRetainedRoles(retainedInstalledSelector(retainedPreparationRoles()...), retainedPreparationRoles(), true, retainedD4aReports(), lower)
	if err != nil {
		t.Fatal(err)
	}
	return admission
}

// Handwritten direct host candidate. The historical empty v2 root members
// are deliberately authored here, as by the existing generated materializer.
func retainedD4aMapping(t *testing.T, profile string) []byte {
	t.Helper()
	return retainedRuntimeTestJSON(t, map[string]any{
		"version": 2, "profile": "base", "provider": "", "facts": nil, "evidence": nil,
		"case":    map[string]any{"parameters": map[string]any{"grant": map[string]string{"pointer": "/grant", "type": "string"}, "digest": map[string]string{"pointer": "/digest", "type": "string"}}, "facts": []any{}, "evidence": []any{}},
		"sources": []any{map[string]any{"name": "research", "kind": "operation", "profile": retainedReviewedInputProfile, "profileDigest": profile, "maxAge": 86400, "arguments": map[string]any{"tool": reviewedInputTool, "arguments": map[string]any{"grant": map[string]string{"$param": "grant"}, "digest": map[string]string{"$param": "digest"}}}, "read": map[string]any{"copy": map[string]any{"facts": []any{map[string]any{"target": "/company/country", "source": "/retained", "conversion": map[string]string{"contractVersion": "1", "column": "legacy_string_value", "kind": "research-value"}}}, "evidence": []any{}}}}},
		"admits":  map[string]any{"facts": map[string][]string{"/company/country": {"generated"}}}, "unmappedEvidence": []string{"license"},
	})
}

func retainedD4aSynthetic(t *testing.T) (retainedDialectAuthoringCheck, retainedPrivateSignedInput, *retainedAdmission) {
	t.Helper()
	production := retainedD3bSyntheticProduction(t)
	profile := "sha256:" + strings.Repeat("a", 64)
	mapping := retainedD4aMapping(t, profile)
	response := append([]byte(" {\n\"result\":"), production.CarrierJSON...)
	response = append(response, []byte(",\"receipt\":{\"signature\":\"transport-only\"},\"salts\":{\"args\":\"opaque\\ntext\"}}\n")...)
	request := retainedRuntimeTestJSON(t, map[string]any{"session": "handwritten", "source": reviewedInputSource, "arguments": map[string]any{"tool": reviewedInputTool, "arguments": map[string]string{"grant": "grant-" + strings.Repeat("b", 32), "digest": "sha256:" + strings.Repeat("c", 64)}}})
	signed := retainedPrivateSignedInput{Response: response, Request: request, CarrierSHA256: sha256Digest(production.CarrierJSON), OriginalSnapshotSHA256: production.OriginalSnapshotSHA256, ProjectedFrameSHA256: production.ProjectedFrameSHA256, ReceivedCarrierSHA256: sha256Digest(production.CarrierJSON), ReceivedFrameSHA256: production.ProjectedFrameSHA256, ProfileDigest: profile}
	return retainedDialectAuthoringCheck{Accepted: true, Document: string(mapping), Digest: retainedDigest(mapping)}, signed, retainedD4aAdmission(t, nil)
}

// Transport-only success deliberately makes no semantic/signature claim.
func retainedD4aReply(t *testing.T, request []byte, selection retainedConversionSelector) []byte {
	t.Helper()
	node, err := parseRetainedJSON(request)
	if err != nil {
		t.Fatal(err)
	}
	digest := "sha256:" + strings.Repeat("d", 64) // independent Runner digest domain
	prep := retainedRuntimeTestJSON(t, map[string]any{"conversionContract": selection, "mappingDigest": digest, "version": 2, "verifiedAt": d0aStamp, "verification": "transport fixture only", "cites": []any{}, "lineage": []any{}, "outcomes": []any{}})
	source := node.member("source").raw
	completed := string(source[:len(source)-1]) + `,"mappingDigest":"` + digest + `"}`
	return []byte(`{"input":{"facts":{"company":{"country":"CA"}},"source":` + completed + `,"preparation":` + string(prep) + `},"factsText":"transport fixture"}` + "\n")
}

func retainedD4aHTTP(t *testing.T, handler http.HandlerFunc) *Server {
	t.Helper()
	s, _ := newTestServer(t, false)
	standIn := httptest.NewServer(handler)
	t.Cleanup(standIn.Close)
	done := make(chan struct{})
	s.jobs = &jobsCompanion{url: standIn.URL, token: "private", done: done, stop: make(chan struct{})}
	// An HTTP fixture has no owned process. Complete it before Server cleanup.
	t.Cleanup(func() { close(done) })
	return s
}

func TestRetainedD4aAdmission(t *testing.T) {
	for _, role := range retainedPreparationRoles() {
		t.Run("missing-"+role, func(t *testing.T) {
			reports := retainedD4aReports()
			switch role {
			case "document-validation":
				reports.Runtime = nil
			case "producer-carrier":
				reports.Producer = nil
			default:
				filtered := []retainedConversionCapability{}
				for _, entry := range reports.Runner {
					if entry.Role != role {
						filtered = append(filtered, entry)
					}
				}
				reports.Runner = filtered
			}
			if _, err := negotiateRetainedRoles(retainedInstalledSelector(retainedPreparationRoles()...), retainedPreparationRoles(), true, reports, nil); retainedContractCode(err) != "capability-unsupported" {
				t.Fatal("missing owner admitted", err)
			}
		})
	}
	for _, role := range []string{"preparation", "producer-carrier"} {
		t.Run("grammar-"+role, func(t *testing.T) {
			reports := retainedD4aReports()
			for i := range reports.Runner {
				if reports.Runner[i].Role == role {
					reports.Runner[i].ProducerGrammar = nil
				}
			}
			if role == "producer-carrier" {
				reports.Producer[0].ProducerGrammar = nil
			}
			if _, err := negotiateRetainedRoles(retainedInstalledSelector(retainedPreparationRoles()...), retainedPreparationRoles(), true, reports, nil); err == nil {
				t.Fatal("missing exact grammar admitted")
			}
		})
	}
	s, _ := newTestServer(t, false)
	for _, selection := range []retainedConversionSelector{{}, retainedInstalledSelector("document-validation"), retainedInstalledSelector("preview", "document-validation", "preparation", "producer-carrier")} {
		got, err := s.prepareRetainedReviewedInput(t.Context(), selection, []byte(`{}`), []byte(`{}`), inputPreparation{}, preparedInputRecord{}, inputPreparedRow{}, nil)
		if err == nil || len(got.InputJSON) != 0 {
			t.Fatal("unsupported selector returned input")
		}
	}
	entries, err := os.ReadDir(s.inputHandoffPath())
	if err != nil && !os.IsNotExist(err) || len(entries) != 0 {
		t.Fatal("refusal wrote a grant", err)
	}
	for _, limits := range []map[string]map[string]int64{{"preparation": {}}, {"unknown": {"profilesBytes": 1}}, {"producer-carrier": {"profilesBytes": 0}}} {
		if _, err := retainedPreparationDownward(limits); err == nil {
			t.Fatal("incomplete/malformed maxima admitted")
		}
	}
}

func TestRetainedD4aAdmissionProfilesBeforeGrant(t *testing.T) {
	reports := retainedD4aReports()
	reports.Runner[1].Limits["profilesBytes"] = 1
	status := retainedRuntimeTestJSON(t, map[string]any{"capabilities": []string{"selected-profile-preparation"}, "conversionContracts": reports.Runner})
	var requests, selected atomic.Int64
	s := retainedD4aHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/v1/status" {
			selected.Add(1)
			w.WriteHeader(500)
			return
		}
		w.Write(status)
	})
	s.localGateway = &localGateway{}
	record := preparedInputRecord{ID: "record-handwritten", Class: "generated", Research: &inputResearchRecord{}}
	doc := inputPreparation{RecipeVersion: 1}
	review := inputRecordReview{RecordID: record.ID, RecordDigest: inputRecordDigest(record), RecipeVersion: 1, Decision: "accepted"}
	doc.Reviews = []inputRecordReviewEvent{{Review: review, Record: record}}
	record.Review = &review
	row := inputPreparedRow{ID: record.ID, Origin: "generated"}
	got, err := s.prepareRetainedReviewedInput(t.Context(), retainedInstalledSelector(retainedPreparationRoles()...), []byte(`{}`), []byte(`{}`), doc, record, row, nil)
	if retainedContractCode(err) != "input-over-bound" || len(got.InputJSON) != 0 || requests.Load() != 1 || selected.Load() != 0 {
		t.Fatal("lower Runner profiles cap failed before listing/grant/acquisition/preparation", err, requests.Load(), selected.Load())
	}
	entries, err := os.ReadDir(s.inputHandoffPath())
	if err != nil && !os.IsNotExist(err) || len(entries) != 0 {
		t.Fatal("profile refusal wrote a grant", err)
	}
}

func TestRetainedD4aBounds(t *testing.T) {
	for _, key := range []string{"documentBytes", "frameBytes", "profilesBytes", "combinedBytes", "preparationBytes", "requestBodyBytes", "inputReplyBytes"} {
		t.Run(key, func(t *testing.T) {
			for _, role := range retainedPreparationRoles() {
				limits := retainedRoleCaps(role)
				if _, ok := limits[key]; !ok {
					continue
				}
				t.Run(role, func(t *testing.T) {
					limits[key] = 128
					admission := retainedD4aAdmission(t, map[string]map[string]int64{role: limits})
					if err := checkRetainedArtifact(admission, key, bytes.Repeat([]byte{'x'}, 128)); err != nil {
						t.Fatal(err)
					}
					if err := checkRetainedArtifact(admission, key, bytes.Repeat([]byte{'x'}, 129)); retainedContractCode(err) != "input-over-bound" {
						t.Fatal("one-over artifact admitted", err)
					}
					limits[key] = 1
					limit, _ := retainedArtifactLimit(admission, key)
					if limit != 128 {
						t.Fatal("caller mutation changed admitted minimum")
					}
				})
			}
		})
	}
	checked, signed, admission := retainedD4aSynthetic(t)
	request, err := retainedPreparationRequest(checked, signed, admission)
	if err != nil {
		t.Fatal(err)
	}
	node, _ := parseRetainedJSON(request)
	for _, bound := range []struct {
		key  string
		size int64
	}{{"combinedBytes", int64(len(node.member("source").raw))}, {"requestBodyBytes", int64(len(request))}} {
		for _, delta := range []int64{0, -1} {
			lower := map[string]map[string]int64{}
			for _, role := range []string{"preview", "preparation"} {
				limits := retainedRoleCaps(role)
				limits[bound.key] = bound.size + delta
				lower[role] = limits
			}
			got, err := retainedPreparationRequest(checked, signed, retainedD4aAdmission(t, lower))
			if delta == 0 && (err != nil || !bytes.Equal(got, request)) || delta < 0 && (retainedContractCode(err) != "input-over-bound" || len(got) != 0) {
				t.Fatal("complete request boundary failed", bound.key, delta, err)
			}
		}
	}
	// Charge the mapping token as actually escaped inside the wire object,
	// independently of Runtime's exact canonical document spelling.
	checked.Document = strings.Replace(checked.Document, `"column":"legacy_string_value"`, `"column":"legacy_string_value<"`, 1)
	checked.Digest = retainedDigest([]byte(checked.Document))
	wire, err := json.Marshal(json.RawMessage(checked.Document))
	if err != nil {
		t.Fatal(err)
	}
	if len(wire) <= len(checked.Document) {
		t.Fatal("escaping fixture did not grow")
	}
	for _, delta := range []int64{0, -1} {
		limits := retainedRoleCaps("document-validation")
		limits["documentBytes"] = int64(len(wire)) + delta
		got, err := retainedPreparationRequest(checked, signed, retainedD4aAdmission(t, map[string]map[string]int64{"document-validation": limits}))
		if delta == 0 && (err != nil || len(got) == 0) || delta < 0 && (retainedContractCode(err) != "input-over-bound" || len(got) != 0) {
			t.Fatal("actual escaped document token boundary failed", delta, err)
		}
	}
}

func TestRetainedD4aBeforeGrant(t *testing.T) {
	production := retainedD3bSyntheticProduction(t)
	node, _ := parseRetainedJSON(production.CarrierJSON)
	frameSize := int64(len(node.member("retained").raw))
	for _, role := range []string{"document-validation", "preview", "preparation"} {
		t.Run(role, func(t *testing.T) {
			lower := retainedRoleCaps(role)
			lower["frameBytes"] = frameSize
			if err := retainedPreparationProduction(production, retainedD4aAdmission(t, map[string]map[string]int64{role: lower})); err != nil {
				t.Fatal("exact same-frame bound refused", err)
			}
			lower["frameBytes"]--
			if err := retainedPreparationProduction(production, retainedD4aAdmission(t, map[string]map[string]int64{role: lower})); retainedContractCode(err) != "input-over-bound" {
				t.Fatal("one-over actual production frame admitted", err)
			}
		})
	}
	_, adapter, err := deskInputAdapter()
	if err != nil {
		t.Fatal(err)
	}
	approved := retainedPrivateProducerApproval{profileDigest: "sha256:" + strings.Repeat("a", 64), adapterDigest: adapter["digest"], admission: retainedD3bAdmission(t, nil)}
	approved.limitsDigest, err = retainedPrivateApprovalDigest(approved)
	if err != nil {
		t.Fatal(err)
	}
	admission := retainedD4aAdmission(t, nil)
	if err := retainedPreparationApproval(admission, approved); err != nil {
		t.Fatal(err)
	}
	admission.limits["producer-carrier"]["profilesBytes"]--
	if err := retainedPreparationApproval(admission, approved); err == nil {
		t.Fatal("different producer minima reused sealed approval")
	}
	admission = retainedD4aAdmission(t, nil)
	approved.admission.limits["producer-carrier"]["responseBytes"]--
	if err := retainedPreparationApproval(admission, approved); err == nil {
		t.Fatal("mutated approval admitted")
	}
}

func TestRetainedD4aTransport(t *testing.T) {
	checked, signed, admission := retainedD4aSynthetic(t)
	request, err := retainedPreparationRequest(checked, signed, admission)
	if err != nil {
		t.Fatal(err)
	}
	reply := retainedD4aReply(t, request, admission.selection)
	var requests atomic.Int64
	s := retainedD4aHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		path, header, err := retainedRunnerTransport(http.MethodPost, "/v1/selected-inputs/next", admission, true)
		body, readErr := io.ReadAll(r.Body)
		if err != nil || readErr != nil || r.Method != http.MethodPost || r.URL.RequestURI() != path || r.Header.Get("Authorization") != "Bearer private" || len(retainedHeaderValues(r.Header)) != 1 || r.Header.Get(retainedSelectorHeaderName) != header || !bytes.Equal(body, request) {
			t.Error("fixed authenticated selected request changed", err, readErr)
		}
		w.Write(reply)
	})
	limits := retainedRoleCaps("preparation")
	limits["inputReplyBytes"] = int64(len(reply))
	admission = retainedD4aAdmission(t, map[string]map[string]int64{"preparation": limits})
	got, err := s.retainedPreparationPOST(t.Context(), request, admission)
	if err != nil || !bytes.Equal(got, reply) || requests.Load() != 1 {
		t.Fatal("exact whole reply refused", err)
	}
	limits["inputReplyBytes"]--
	got, err = s.retainedPreparationPOST(t.Context(), request, retainedD4aAdmission(t, map[string]map[string]int64{"preparation": limits}))
	if retainedContractCode(err) != "input-over-bound" || len(got) != 0 || requests.Load() != 2 {
		t.Fatal("one-over reply parsed/retained or retried", err)
	}
	limits["requestBodyBytes"] = int64(len(request)) - 1
	got, err = s.retainedPreparationPOST(t.Context(), request, retainedD4aAdmission(t, map[string]map[string]int64{"preparation": limits}))
	if retainedContractCode(err) != "input-over-bound" || len(got) != 0 || requests.Load() != 2 {
		t.Fatal("one-over request dispatched", err)
	}
}

func TestRetainedD4aCancellation(t *testing.T) {
	var calls, targetCalls atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls.Add(1) }))
	defer target.Close()
	s := retainedD4aHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	})
	admission := retainedD4aAdmission(t, nil)
	if got, err := s.retainedPreparationPOST(t.Context(), []byte(`{}`), admission); err == nil || len(got) != 0 || calls.Load() != 1 || targetCalls.Load() != 0 {
		t.Fatal("redirect followed or retry occurred", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got, err := s.retainedPreparationPOST(ctx, []byte(`{}`), admission); err == nil || len(got) != 0 || calls.Load() != 1 {
		t.Fatal("cancelled request dispatched", err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	var blockedCalls atomic.Int64
	blocked := retainedD4aHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		if blockedCalls.Add(1) == 1 {
			close(started)
		}
		<-release
	})
	// Registered after the fixture so even Fatal releases the handler before
	// httptest.Server.Close. Do not rely on the server observing client cancellation.
	t.Cleanup(func() { close(release) })
	ctx, cancel = context.WithCancel(t.Context())
	t.Cleanup(cancel)
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	type result struct {
		input []byte
		err   error
	}
	finished := make(chan result, 1)
	go func() {
		input, err := blocked.retainedPreparationPOST(ctx, []byte(`{}`), admission)
		finished <- result{input: input, err: err}
	}()
	select {
	case <-started:
	case <-timer.C:
		t.Fatal("in-flight cancellation handler did not start")
	}
	cancel()
	// The handler stays blocked until cleanup: the caller must return without
	// a server response or fixture release, and must not dispatch a retry.
	select {
	case got := <-finished:
		if got.err == nil || len(got.input) != 0 || blockedCalls.Load() != 1 {
			t.Fatal("in-flight cancellation became success or retried", got.err)
		}
	case <-timer.C:
		t.Fatal("in-flight cancellation did not return before handler release")
	}
}

func TestRetainedD4aCustody(t *testing.T) {
	checked, signed, admission := retainedD4aSynthetic(t)
	if err := retainedPreparationMapping([]byte(checked.Document), signed.ProfileDigest); err != nil {
		t.Fatal(err)
	}
	request, err := retainedPreparationRequest(checked, signed, admission)
	if err != nil {
		t.Fatal(err)
	}
	root, _ := parseRetainedJSON(request)
	if !retainedExactObject(root, []string{"source"}) {
		t.Fatal("request is not source-only")
	}
	text, _ := retainedMemberString(root.member("source").member("sources").member("research"), "retainedResponseJson")
	if text != string(signed.Response) {
		t.Fatal("exact envelope text changed")
	}
	reply := retainedD4aReply(t, request, admission.selection)
	input, err := retainedPreparationReply(reply, request, signed, admission)
	if err != nil || len(input) == 0 {
		t.Fatal("transport-only complete input refused", err)
	}
	parsed, err := parseRetainedJSON(reply)
	if err != nil {
		t.Fatal(err)
	}
	preparation := parsed.member("input").member("preparation")
	selector := preparation.member("conversionContract")
	if selector == nil || bytes.Count(reply, preparation.raw) != 1 {
		t.Fatal("preparation selector fixture missing or ambiguous")
	}
	// Change only the preparation selector, regardless of object member order.
	changedSelector := bytes.Replace(selector.raw, []byte(`"contract":"retained-column/1"`), []byte(`"contract":"other"`), 1)
	if bytes.Equal(changedSelector, selector.raw) {
		t.Fatal("preparation selector mutation did not change fixture")
	}
	changedPreparation := bytes.Replace(preparation.raw, selector.raw, changedSelector, 1)
	for _, change := range []struct {
		name string
		raw  []byte
	}{
		{"next", bytes.Replace(reply, []byte(`{"input":`), []byte(`{"next":null,"input":`), 1)},
		{"null-input", []byte(`{"input":null}`)},
		{"duplicate", bytes.Replace(reply, []byte(`"facts":`), []byte(`"facts":{},"facts":`), 1)},
		{"preparation-selector", bytes.Replace(reply, preparation.raw, changedPreparation, 1)},
		{"response-text", bytes.Replace(reply, []byte(`opaque\\ntext`), []byte(`changed\\ntext`), 1)},
		{"custody-sibling", bytes.Replace(reply, []byte(`"retainedResponseJson":`), []byte(`"response":{},"retainedResponseJson":`), 1)},
		{"changed-mapping", bytes.Replace(reply, []byte(`"column":"legacy_string_value"`), []byte(`"column":"text_value"`), 1)},
		{"changed-arguments", bytes.Replace(reply, []byte(`"$param":"grant"`), []byte(`"$param":"digest"`), 1)},
		{"digest-disagreement", bytes.Replace(reply, []byte(`"mappingDigest":"sha256:`+strings.Repeat("d", 64)), []byte(`"mappingDigest":"sha256:`+strings.Repeat("e", 64)), 1)},
	} {
		t.Run(change.name, func(t *testing.T) {
			if bytes.Equal(change.raw, reply) {
				t.Fatal("mutation did not change fixture")
			}
			got, err := retainedPreparationReply(change.raw, request, signed, admission)
			if err == nil || len(got) != 0 {
				t.Fatal("malformed custody admitted")
			}
		})
	}
	for _, change := range []struct {
		name     string
		from, to string
	}{
		{"profile", retainedReviewedInputProfile, reviewedInputProfile},
		{"source-pointer", `"source":"/retained"`, `"source":"/prepared/value"`},
		{"base", `"profile":"base"`, `"profile":"extended"`},
		{"operation", `"kind":"operation"`, `"kind":"research"`},
	} {
		t.Run(change.name, func(t *testing.T) {
			raw := bytes.Replace([]byte(checked.Document), []byte(change.from), []byte(change.to), 1)
			if bytes.Equal(raw, []byte(checked.Document)) || retainedPreparationMapping(raw, signed.ProfileDigest) == nil {
				t.Fatal("unsupported mapping admitted")
			}
		})
	}
	prepSize := int64(len(parsed.member("input").member("preparation").raw))
	for _, delta := range []int64{0, -1} {
		limits := retainedRoleCaps("preparation")
		limits["preparationBytes"] = prepSize + delta
		got, err := retainedPreparationReply(reply, request, signed, retainedD4aAdmission(t, map[string]map[string]int64{"preparation": limits}))
		if delta == 0 && (err != nil || !bytes.Equal(got, input)) || delta < 0 && (retainedContractCode(err) != "input-over-bound" || len(got) != 0) {
			t.Fatal("complete preparation limit failed", delta, err)
		}
	}
	input[0] = 'x'
	if !json.Valid(reply) {
		t.Fatal("returned input aliases reply custody")
	}
}

func TestRetainedD4aCustodyDistinctMappingDomains(t *testing.T) {
	explicit := retainedD4aMapping(t, "sha256:"+strings.Repeat("a", 64))
	var omittedMembers map[string]json.RawMessage
	if err := json.Unmarshal(explicit, &omittedMembers); err != nil {
		t.Fatal(err)
	}
	for _, member := range []struct{ name, value string }{{"provider", `""`}, {"facts", `null`}, {"evidence", `null`}} {
		if string(omittedMembers[member.name]) != member.value {
			t.Fatal("omitted-member fixture missing expected value", member.name)
		}
		delete(omittedMembers, member.name)
	}
	omitted := retainedRuntimeTestJSON(t, omittedMembers)
	if bytes.Equal(explicit, omitted) {
		t.Fatal("omitted-member fixture unchanged")
	}
	left, err := parseRetainedJSON(omitted)
	if err != nil {
		t.Fatal(err)
	}
	right, err := parseRetainedJSON(explicit)
	if err != nil {
		t.Fatal(err)
	}
	if retainedPreparationMapping(omitted, "sha256:"+strings.Repeat("a", 64)) != nil || !retainedPreparationSameMapping(left, right) || retainedDigest(omitted) == retainedDigest(explicit) {
		t.Fatal("legal omitted legacy members forced to Runtime identity")
	}
	// Runner preserves non-nil empty root slices; it does not turn [] into null.
	arrays := bytes.Replace(explicit, []byte(`"facts":null`), []byte(`"facts":[]`), 1)
	arrayNode, err := parseRetainedJSON(arrays)
	if err != nil || bytes.Equal(arrays, explicit) {
		t.Fatal("explicit slice mutation did not change valid fixture", err)
	}
	if retainedPreparationSameMapping(arrayNode, right) {
		t.Fatal("distinct explicit slice value silently normalized")
	}
	for _, change := range []struct{ from, to string }{{`"kind":"research-value"`, `"kind":"text"`}, {`"target":"/company/country"`, `"target":"/other"`}, {`"maxAge":86400`, `"maxAge":300`}} {
		mutated := bytes.Replace(explicit, []byte(change.from), []byte(change.to), 1)
		changed, err := parseRetainedJSON(mutated)
		if err != nil || bytes.Equal(mutated, explicit) || retainedPreparationSameMapping(left, changed) {
			t.Fatal("meaningful mapping mutation admitted", change.from, err)
		}
	}
}

// Handwritten transport peers exercise the generated host entry point. They
// establish no released Runtime semantics or Gateway signature qualification.
func TestRetainedD4aSelectedProfilesIndependentOrdinaryBounds(t *testing.T) {
	for _, scenario := range []string{"exact-listing", "selected-array-one-over", "listing-array-one-over"} {
		t.Run(scenario, func(t *testing.T) {
			s, _ := newTestServer(t, false)
			s.cfg.LocalGatewayBundle = os.Getenv("JPS_TEST_GATEWAY_BUNDLE")
			gatewayDone := make(chan struct{})
			s.localGateway = &localGateway{done: gatewayDone}
			t.Cleanup(func() { close(gatewayDone) })
			if err := verifyGatewayBundle(s.cfg.LocalGatewayBundle); err != nil {
				t.Fatal("explicit integrity-checked fixture bundle required", err)
			}
			// Fill all ordinary slots with valid configuration whose bytes exceed
			// the selected cap. Its independently materialized registry must survive.
			ordinaryRows := make([]map[string]any, 15)
			for i := range ordinaryRows {
				name := fmt.Sprintf("ordinary-%d", i)
				ordinaryRows[i] = map[string]any{"id": name, "source": name, "class": "record", "publicKey": "58" + strings.Repeat("66", 31), "authority": "test", "shape": "mcp", "adapter": map[string]string{"name": "mcp", "version": "1", "digest": "sha256:" + strings.Repeat("a", 64)}, "endpoint": nil, "tools": []string{"lookup"}}
			}
			s.cfg.RunnerInputProfiles = retainedRuntimeTestJSON(t, ordinaryRows)
			originalConfig := bytes.Clone(s.cfg.RunnerInputProfiles)
			ordinary, err := s.reviewedInputProfiles()
			if err != nil {
				t.Fatal(err)
			}
			var ordinaryProfiles []json.RawMessage
			if json.Unmarshal(ordinary, &ordinaryProfiles) != nil || len(ordinaryProfiles) != 16 {
				t.Fatal("ordinary registry fixture is not full")
			}

			research := researchRecipeFixture()
			research.Fields = []inputResearchField{{Name: "legacy_string_value", Description: "Original country", Type: "string"}}
			actionRaw := []byte(`{"tool":"records","arguments":{"records":[{"kind":"company","name":"Example","url":"https://example.com/","fields":{"legacy_string_value":{"value":"CA","status":"interpretation","sources":[]}}}]}}`)
			action, err := decodeResearchAction(actionRaw)
			if err != nil {
				t.Fatal(err)
			}
			run := inputResearchRun{Version: 1, ID: "research_" + strings.Repeat("a", 32), PreparationID: "prep_" + strings.Repeat("b", 32), RecipeVersion: 1, Recipe: research, State: "completed", Seeds: []string{"https://example.com/"}, Sources: []inputResearchSource{}, Attempts: []inputResearchAttempt{{ID: "attempt_model", Kind: "model", State: "completed", Result: actionRaw, FinishedAt: d0aStamp}}}
			if err := applyResearchRecords(&run, action.Arguments); err != nil || len(run.Records) != 1 {
				t.Fatal("handwritten research action refused", err)
			}
			d0aStoreRun(t, s, run, nil)
			record := researchPreparedRecord(run, run.Records[0])
			recipe := inputRecipe{Research: &research}
			doc := inputPreparation{ID: run.PreparationID, RecipeVersion: 1, Recipe: recipe, Recipes: []inputRecipeRevision{{Version: 1, Recipe: recipe}}}
			review := inputRecordReview{RecordID: record.ID, RecordDigest: inputRecordDigest(record), RecipeVersion: 1, Decision: "accepted"}
			doc.Reviews = []inputRecordReviewEvent{{Review: review, Record: record}}
			record.Review = &review
			row := inputPreparedRow{ID: record.ID, Origin: "generated", Prepared: map[string]any{"company": map[string]any{"country": "CA"}}}

			profile, _, err := retainedPrivateProfile(s)
			if err != nil {
				t.Fatal(err)
			}
			selectedArray := append(append([]byte{'['}, profile...), ']')
			// The complete selected inventory includes an unrelated immutable
			// document, even when that entry is set aside. Wrappers are separate.
			other := bytes.Replace(profile, []byte(retainedReviewedInputProfile), []byte("held-selected"), 1)
			other = bytes.Replace(other, []byte(reviewedInputSource), []byte("held-source"), 1)
			profileArray := []byte(`[` + string(profile) + `,` + string(other) + `]`)
			limit := int64(len(profileArray))
			if scenario == "selected-array-one-over" {
				limit = int64(len(selectedArray) - 1)
			}
			if scenario == "listing-array-one-over" {
				other = bytes.Replace(other, []byte("}"), []byte(" }"), 1)
			}
			if int64(len(originalConfig)) <= limit || limit >= retainedRoleCaps("preparation")["profilesBytes"] {
				t.Fatal("fixture did not establish independent lower selected cap")
			}
			digest := "sha256:" + strings.Repeat("a", 64)
			listing := retainedSelectedTestReport(t, []byte(`[{"profile":`+string(profile)+`,"digest":"`+digest+`"},{"profile":`+string(other)+`,"digest":"sha256:`+strings.Repeat("b", 64)+`","state":"set-aside","reason":"handwritten"}]`))
			reports := retainedD4aReports()
			for i := range reports.Runner {
				reports.Runner[i].Limits["profilesBytes"] = limit
			}
			status := retainedRuntimeTestJSON(t, map[string]any{"capabilities": []string{"selected-profile-preparation"}, "conversionContracts": reports.Runner})
			mapping := retainedD4aMapping(t, digest)
			h := retainedRuntimeHarness(t)
			s.cfg.JpackBin = h.peer
			for _, operation := range []string{"validate", "check"} {
				r := retainedRuntimeTestReport(operation)
				r["document"], r["digest"] = string(mapping), retainedDigest(mapping)
				h.output(t, operation, retainedRuntimeTestJSON(t, r), 0)
			}
			var controls, preparations, acquisitions atomic.Int64
			peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer private" {
					t.Error("Runner authentication lost")
					w.WriteHeader(401)
					return
				}
				switch r.Method + " " + r.URL.RequestURI() {
				case "GET /v1/status":
					controls.Add(1)
					w.Write(status)
				case "POST " + retainedRunnerPrefix + "selected-profiles":
					controls.Add(1)
					body, _ := io.ReadAll(r.Body)
					if !bytes.Equal(body, profile) {
						t.Error("selected admission changed trusted profile")
					}
					w.Write([]byte(`{"profile":` + string(profile) + `,"digest":"` + digest + `"}`))
				case "GET " + retainedRunnerPrefix + "selected-profiles":
					controls.Add(1)
					w.Write(listing)
				case "POST " + retainedRunnerPrefix + "selected-inputs/next":
					preparations.Add(1)
					if _, err := readRetainedRunnerSelection(r.Method, r.URL.Path, r.Header, true); err != nil {
						t.Error("generated selection lost", err)
					}
					body, _ := io.ReadAll(r.Body)
					w.Write(retainedD4aReply(t, body, retainedInstalledSelector(retainedPreparationRoles()...)))
				default:
					t.Error("unexpected Runner route", r.Method, r.URL.RequestURI())
					w.WriteHeader(404)
				}
			}))
			defer peer.Close()
			done := make(chan struct{})
			s.jobs = &jobsCompanion{url: peer.URL, token: "private", done: done, stop: make(chan struct{})}
			t.Cleanup(func() { close(done) })
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method + " " + r.URL.RequestURI() {
				case "GET /publickey":
					w.Write([]byte(`{"algorithm":"ed25519","authority":"` + localAuthority + `","publicKey":"transport-peer"}`))
				case "POST /acquire":
					acquisitions.Add(1)
					var request struct {
						Arguments struct {
							Arguments struct {
								Grant string `json:"grant"`
							} `json:"arguments"`
						} `json:"arguments"`
					}
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil || !inputGrantID.MatchString(request.Arguments.Arguments.Grant) {
						t.Error("invalid acquisition request", err)
						w.WriteHeader(400)
						return
					}
					root, err := openPrivateDataRoot(s.inputHandoffPath(), false)
					if err != nil {
						t.Error(err)
						w.WriteHeader(500)
						return
					}
					defer root.Close()
					raw, err := readPrivateData(root, request.Arguments.Arguments.Grant+retainedGrantSuffix, int(retainedRoleCaps("producer-carrier")["grantBytes"]))
					node, parseErr := parseRetainedJSON(raw)
					if err != nil || parseErr != nil {
						t.Error("grant unavailable", err, parseErr)
						w.WriteHeader(500)
						return
					}
					w.Write([]byte(`{"result":` + string(node.member("result").raw) + `,"receipt":{"signature":"transport-only"},"salts":{"args":"fixture"}}`))
				default:
					t.Error("unexpected Gateway route", r.Method, r.URL.RequestURI())
					w.WriteHeader(404)
				}
			}))
			defer gateway.Close()
			s.localGateway.pin = &localGatewayPin{URL: gateway.URL, Authority: localAuthority, Signer: localSigner{Public: "transport-peer"}}
			result, err := s.prepareRetainedReviewedInput(t.Context(), retainedInstalledSelector(retainedPreparationRoles()...), mapping, []byte(retainedRuntimeTestPack), doc, record, row, nil)
			if scenario == "exact-listing" {
				if err != nil || len(result.InputJSON) == 0 || !result.Runtime.Accepted || result.Limits["preparation"]["profilesBytes"] != limit || preparations.Load() != 1 || acquisitions.Load() != 1 || controls.Load() != 4 || len(h.calls(t)) != 3 {
					t.Fatal("generated selected preparation failed under independent cap", err, preparations.Load(), acquisitions.Load(), controls.Load())
				}
			} else {
				wantControls := int64(4)
				if scenario == "selected-array-one-over" {
					wantControls = 1
				}
				if retainedContractCode(err) != "input-over-bound" || len(result.InputJSON) != 0 || preparations.Load() != 0 || acquisitions.Load() != 0 || controls.Load() != wantControls || len(h.calls(t)) != 0 {
					t.Fatal("selected array overflow crossed grant/acquisition/preparation boundary", err, controls.Load(), acquisitions.Load(), preparations.Load())
				}
			}
			after, err := s.reviewedInputProfiles()
			if err != nil || !bytes.Equal(after, ordinary) || !bytes.Equal(originalConfig, s.cfg.RunnerInputProfiles) {
				t.Fatal("selected preparation changed ordinary registry", err)
			}
			entries, err := os.ReadDir(s.inputHandoffPath())
			if err != nil && !os.IsNotExist(err) || len(entries) != 0 {
				t.Fatal("preparation left or refusal wrote grant", err)
			}
		})
	}
}
