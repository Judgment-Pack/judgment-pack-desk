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
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func retainedB3Qualification(t *testing.T, generated bool) *inputQualification {
	t.Helper()
	selection := inputRetainedContract()
	rules := retainedInputRules{Facts: []retainedInputFactBinding{}, Evidence: []retainedInputEvidenceBinding{}}
	if generated {
		rules.Facts = append(rules.Facts, retainedInputFactBinding{Target: "/company/country", Column: "legacy_string_value", Conversion: inputConversion{Kind: "research-value"}})
	} else {
		for _, rule := range []struct{ target, column, kind string }{{"/company/country", "number", "number"}, {"/decimal", "decimal", "decimal"}, {"/flag", "flag", "boolean"}, {"/text", "text", "text"}, {"/blank", "blank", "text"}, {"/null", "null", "text"}, {"/missing", "missing", "text"}} {
			c := inputConversion{Kind: rule.kind}
			if rule.kind == "boolean" {
				c.TrueValue, c.FalseValue = "yes", "no"
			}
			rules.Facts = append(rules.Facts, retainedInputFactBinding{Target: rule.target, Column: rule.column, Conversion: c})
		}
		rules.Evidence = append(rules.Evidence, retainedInputEvidenceBinding{Requirement: "license", Column: "evidence"})
	}
	omitted := []string(nil)
	if generated {
		omitted = []string{"license"}
	}
	profile := selection.Profile
	mapping := inputRecordDialect{Version: 2, Profile: &profile, Case: &inputCaseDialect{Facts: []inputDialectFact{}, Evidence: []inputDialectEvidence{}}, Sources: []json.RawMessage{}, UnmappedEvidence: omitted}
	sidecars := map[string]inputConversion{}
	for _, rule := range rules.Facts {
		leaf := &inputRetainedConversion{ContractVersion: "1", Column: rule.Column, Kind: rule.Conversion.Kind}
		if rule.Conversion.Kind == "boolean" {
			yes, no := "yes", "no"
			leaf.TrueValue, leaf.FalseValue = &yes, &no
		}
		mapping.Case.Facts = append(mapping.Case.Facts, inputDialectFact{Target: rule.Target, Source: "/retained", Conversion: leaf})
		sidecars[rule.Target] = rule.Conversion
	}
	for _, rule := range rules.Evidence {
		mapping.Case.Evidence = append(mapping.Case.Evidence, inputDialectEvidence{Requirement: rule.Requirement, Source: "/retained", Availability: &inputRetainedAvailability{ContractVersion: "1", Column: rule.Column}})
	}
	return &inputQualification{Kind: "pack", Key: "pack.json", Pack: retainedRuntimeTestPack, Digest: retainedDigest([]byte(retainedRuntimeTestPack)), ConversionContract: &selection, Mapping: mapping, Conversions: sidecars}
}

// Internal-only fixture attachment: public selected decode/save remains gated
// until B4. Raw JSON is handwritten/assembled before the real custody decoder;
// no output of Runtime or Runner is recorded as an expected result.
func retainedB3SealFixture(t *testing.T, doc inputPreparation) inputPreparationReply {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var copy inputPreparation
	if err = decodeDataJSON(raw, &copy); err != nil {
		t.Fatal(err)
	}
	if err = attachInputCellCustody(&copy, raw); err != nil {
		t.Fatal(err)
	}
	result := inputPreparationReply{SHA256: digestOf(raw), Document: copy}
	if copy.cellCustody == nil || copy.cellCustody.snapshotDigest == "" {
		t.Fatal("fixture custody unavailable")
	}
	return result
}
func retainedB3AssertedFixture(t *testing.T) inputPreparationReply {
	t.Helper()
	saved := retainedB2Snapshot(t, strings.Replace(retainedB2Cells, `"number":"1e3"`, `"number":"-0"`, 1))
	saved.Document.Recipe.Qualification = retainedB3Qualification(t, false)
	return retainedB3SealFixture(t, saved.Document)
}
func retainedB3Request(t *testing.T) ([]byte, retainedAssertedCase, *retainedAdmission) {
	t.Helper()
	saved := retainedB3AssertedFixture(t)
	admission := retainedB2Admission(t)
	projected, err := projectRetainedAssertedInput(saved, "row-original", admission)
	if err != nil {
		t.Fatal(err)
	}
	mapping, err := materializeRetainedInputMapping(saved.Document.Recipe.Qualification, false, "")
	if err != nil {
		t.Fatal(err)
	}
	checked := retainedDialectAuthoringCheck{Accepted: true, Document: string(mapping), Digest: retainedDigest(mapping)}
	request, err := retainedAssertedPreparationRequest(checked, projected, admission)
	if err != nil {
		t.Fatal(err)
	}
	return request, projected, admission
}

// Transport fixture only. Its preparation is deliberately labelled; it is not
// an execution/authenticity witness. Actual-component fixtures below own that.
func retainedB3Reply(t *testing.T, request []byte, admission *retainedAdmission) []byte {
	t.Helper()
	root, err := parseRetainedJSON(request)
	if err != nil {
		t.Fatal(err)
	}
	var source map[string]json.RawMessage
	if err = json.Unmarshal(root.member("source").raw, &source); err != nil {
		t.Fatal(err)
	}
	var mapping map[string]json.RawMessage
	if err = json.Unmarshal(source["mapping"], &mapping); err != nil {
		t.Fatal(err)
	}
	mapping["provider"], mapping["facts"], mapping["evidence"] = json.RawMessage(`""`), json.RawMessage(`null`), json.RawMessage(`null`)
	delete(mapping, "sources") // documented Runner omitempty for asserted mapping
	source["mapping"], err = json.Marshal(mapping)
	if err != nil {
		t.Fatal(err)
	}
	digest := "sha256:" + strings.Repeat("d", 64)
	source["mappingDigest"], _ = json.Marshal(digest)
	prep := map[string]any{"conversionContract": admission.selection, "version": 2, "verifiedAt": d0aStamp, "mappingDigest": digest, "verification": "transport fixture only", "cites": []any{}, "lineage": []any{}, "outcomes": []any{}}
	return retainedRuntimeTestJSON(t, map[string]any{"input": map[string]any{"source": source, "facts": json.RawMessage(`{"company":{"country":-0},"decimal":"-0.00","flag":false,"text":"  kept  "}`), "evidence": map[string]string{"license": "absent"}, "preparation": prep}})
}

func TestRetainedB3Materialization(t *testing.T) {
	for _, generated := range []bool{false, true} {
		t.Run(map[bool]string{false: "asserted", true: "generated"}[generated], func(t *testing.T) {
			q := retainedB3Qualification(t, generated)
			before, _ := json.Marshal(q)
			profile := ""
			if generated {
				profile = "sha256:" + strings.Repeat("a", 64)
			}
			mapping, err := materializeRetainedInputMapping(q, generated, profile)
			if err != nil {
				t.Fatal(err)
			}
			after, _ := json.Marshal(q)
			if !bytes.Equal(before, after) {
				t.Fatal("saved logical mapping mutated")
			}
			n, err := parseRetainedJSON(mapping)
			if err != nil {
				t.Fatal(err)
			}
			admits := n.member("admits")
			class := "asserted"
			if generated {
				class = "generated"
			}
			for _, f := range q.Mapping.Case.Facts {
				got, _ := admits.member("facts").member(f.Target).elements[0].scalarString()
				if got != class {
					t.Fatal("wrong host admission", got)
				}
			}
			if generated {
				if err = retainedPreparationMapping(mapping, profile); err != nil {
					t.Fatal(err)
				}
				if len(n.member("case").member("facts").elements) != 0 || len(n.member("case").member("evidence").elements) != 0 {
					t.Fatal("generated facts copied into case")
				}
				source := n.member("sources").elements[0]
				if source.member("import") != nil || string(source.member("maxAge").raw) != "86400" {
					t.Fatal("wrong fixed source")
				}
				copy := source.member("read").member("copy")
				if len(copy.member("facts").elements) != len(q.Mapping.Case.Facts) {
					t.Fatal("saved leaves missing")
				}
			} else if n.member("sources").kind != '[' || len(n.member("sources").elements) != 0 {
				t.Fatal("asserted acquired a source")
			}
			// Every sidecar and saved tuple is mandatory before a profile or request.
			q.ConversionContract = nil
			if _, err = materializeRetainedInputMapping(q, generated, profile); err == nil {
				t.Fatal("implicit selection")
			}
		})
	}
	for _, scenario := range []string{"missing sidecar", "extra sidecar", "wrong sidecar", "wrong profile", "ordinary leaf"} {
		t.Run(scenario, func(t *testing.T) {
			q := retainedB3Qualification(t, true)
			switch scenario {
			case "missing sidecar":
				delete(q.Conversions, "/company/country")
			case "extra sidecar":
				q.Conversions["/extra"] = inputConversion{Kind: "text"}
			case "wrong sidecar":
				q.Conversions["/company/country"] = inputConversion{Kind: "number"}
			case "wrong profile":
				*q.Mapping.Profile = "extended"
			case "ordinary leaf":
				q.Mapping.Case.Facts[0].Source = "/values/legacy_string_value"
			}
			if _, err := materializeRetainedInputMapping(q, true, "sha256:"+strings.Repeat("a", 64)); err == nil {
				t.Fatal("saved mismatch accepted")
			}
		})
	}
}

// These focused fixtures use the real collection repository and custody
// attachment, without claiming journal/source verification or preparation.
// The existing actual generated fixture below retains that separate proof.
func TestRetainedB3GeneratedImportCustody(t *testing.T) {
	for _, shape := range []string{"nil", "empty", "nonempty"} {
		t.Run(shape, func(t *testing.T) {
			s, _ := newTestServer(t, false)
			recipe := researchRecipeFixture()
			recipe.Fields = []inputResearchField{{Name: "legacy_string_value", Description: "Original country", Type: "string"}}
			run := inputResearchRun{Version: 1, ID: "research_" + strings.Repeat("a", 32), PreparationID: "prep_" + strings.Repeat("b", 32), RecipeVersion: 1, Recipe: recipe, Seeds: []string{"https://example.com/"}}
			if err := applyResearchRecords(&run, json.RawMessage(`{"records":[{"kind":"company","name":"Example","url":"https://example.com/","fields":{"legacy_string_value":{"value":"CA","status":"interpretation","sources":[]}}}]}`)); err != nil {
				t.Fatal(err)
			}
			var imports []inputImport
			if shape == "empty" {
				imports = []inputImport{}
			} else if shape == "nonempty" {
				content := "unused\nvalue\n"
				imports = []inputImport{{ID: "source_" + digestOf([]byte("csv\n"+content)), Kind: "csv", Name: "original.csv", Digest: retainedDigest([]byte(content)), Content: content, At: d0aStamp, RecipeVersion: 1}}
			}
			original := inputRecipe{Research: &recipe}
			doc := inputPreparation{Version: 1, ID: run.PreparationID, Name: "Generated import custody", Recipe: original, RecipeVersion: 1, Recipes: []inputRecipeRevision{{Version: 1, Name: "Original", Recipe: original, At: d0aStamp}}, Records: []preparedInputRecord{researchPreparedRecord(run, run.Records[0])}, Imports: imports}
			repository := s.preparationRepository()
			if _, err := repository.CompareAndSwap(t.Context(), doc, ""); err != nil {
				t.Fatal(err)
			}
			view, err := repository.Read(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer view.Close()
			held, err := view.Get(doc.ID)
			if err != nil {
				t.Fatal(err)
			}
			if (held.Document.Imports == nil) != (imports == nil) || len(held.Document.Imports) != len(imports) {
				t.Fatal("repository changed import inventory shape")
			}
			// Public selected load/save stays gated. Only the private test
			// attachment changes the qualification after the held legacy read.
			held.Document.Recipe.Qualification = retainedB3Qualification(t, true)
			saved := retainedB3SealFixture(t, held.Document)
			if (saved.Document.cellCustody.imports == nil) != (saved.Document.Imports == nil) || len(saved.Document.cellCustody.imports) != len(saved.Document.Imports) {
				t.Fatal("custody changed import inventory shape")
			}
			recordID := saved.Document.Records[0].ID
			if record, err := retainedSavedPreparationRecord(saved, recordID); err != nil || record.Class != "generated" || record.Research == nil {
				t.Fatal("unchanged generated snapshot refused", err)
			}
			mutations := []string{"representation", "added"}
			if shape == "nonempty" {
				mutations = append(mutations, "metadata")
			}
			for _, mutation := range mutations {
				t.Run(mutation, func(t *testing.T) {
					changed := retainedB3SealFixture(t, held.Document)
					switch mutation {
					case "representation":
						if changed.Document.Imports == nil {
							changed.Document.Imports = []inputImport{}
						} else {
							changed.Document.Imports = nil
						}
					case "added":
						changed.Document.Imports = append(changed.Document.Imports, inputImport{Name: "added.csv"})
					case "metadata":
						changed.Document.Imports[0].Name = "changed.csv"
						if changed.Document.cellCustody.imports[0].Name != "original.csv" {
							t.Fatal("custody inventory aliases the mutable document")
						}
					}
					var dispatch atomic.Int64
					owner := retainedD4aHTTP(t, func(w http.ResponseWriter, r *http.Request) { dispatch.Add(1); w.WriteHeader(500) })
					result, err := owner.prepareRetainedSavedInput(t.Context(), changed, recordID, nil)
					if err == nil || err.Error() != inputCellFailure().Error() || len(result.InputJSON) != 0 || dispatch.Load() != 0 {
						t.Fatal("changed generated import inventory dispatched", err, dispatch.Load())
					}
				})
			}
		})
	}
}

func TestRetainedB3SnapshotMutationRefusesBeforeDispatch(t *testing.T) {
	for _, scenario := range []string{"sha", "recipe version", "qualification", "record name", "record review", "import metadata", "source", "cells", "generic decode"} {
		t.Run(scenario, func(t *testing.T) {
			saved := retainedB3AssertedFixture(t)
			switch scenario {
			case "sha":
				saved.SHA256 = "changed"
			case "recipe version":
				saved.Document.RecipeVersion++
			case "qualification":
				saved.Document.Recipe.Qualification.Mapping.Case.Facts[0].Conversion.Column = "other"
			case "record name":
				saved.Document.Records[0].Name = "changed"
			case "record review":
				saved.Document.Records[0].Review = &inputRecordReview{Decision: "accepted"}
			case "import metadata":
				saved.Document.Imports[0].Name = "changed.csv"
			case "source":
				saved.Document.Records[0].SourceID = "changed"
			case "cells":
				saved.Document.Records[0].Values["null"] = ""
			case "generic decode":
				raw, _ := json.Marshal(saved)
				var detached inputPreparationReply
				if json.Unmarshal(raw, &detached) != nil {
					t.Fatal("decode")
				}
				saved = detached
			}
			var dispatch atomic.Int64
			s := retainedD4aHTTP(t, func(w http.ResponseWriter, r *http.Request) { dispatch.Add(1); w.WriteHeader(500) })
			result, err := s.prepareRetainedSavedInput(t.Context(), saved, "row-original", nil)
			if err == nil || len(result.InputJSON) != 0 || dispatch.Load() != 0 {
				t.Fatal("mutated held snapshot dispatched", err, dispatch.Load())
			}
		})
	}
}

func TestRetainedB3AssertedCustodyAndBounds(t *testing.T) {
	request, projected, admission := retainedB3Request(t)
	n, err := parseRetainedJSON(request)
	if err != nil {
		t.Fatal(err)
	}
	source := n.member("source")
	if !retainedExactObject(source, []string{"conversionContract", "mapping", "retainedCaseJson"}) {
		t.Fatal("redundant case/source custody")
	}
	text, _ := retainedMemberString(source, "retainedCaseJson")
	if text != projected.CaseJSON || !strings.Contains(text, `"number":"-0"`) || !strings.Contains(text, `"null":null`) || !strings.Contains(text, `"unmapped"`) {
		t.Fatal("raw case changed", text)
	}
	reply := retainedB3Reply(t, request, admission)
	if input, err := retainedAssertedPreparationReply(reply, request, projected, admission); err != nil || !bytes.Contains(input, []byte(`"country":-0`)) {
		t.Fatal("complete exact input lost", err)
	}
	for _, scenario := range []string{"case", "dual case", "source sibling", "selector", "mapping leaf", "mapping digest", "preparation digest", "unknown mapping", "lineage type"} {
		t.Run(scenario, func(t *testing.T) {
			changed := string(reply)
			switch scenario {
			case "case":
				changed = strings.Replace(changed, `\\tail`, `\\other`, 1)
			case "dual case":
				changed = strings.Replace(changed, `"retainedCaseJson":`, `"case":{},"retainedCaseJson":`, 1)
			case "source sibling":
				changed = strings.Replace(changed, `"retainedCaseJson":`, `"sources":{},"retainedCaseJson":`, 1)
			case "selector":
				changed = strings.Replace(changed, `"roles":["document-validation","preview","preparation"]`, `"roles":["document-validation","preview"]`, 1)
			case "mapping leaf":
				changed = strings.Replace(changed, `"column":"number"`, `"column":"other"`, 1)
			case "mapping digest":
				at := strings.Index(changed, `"source":`)
				changed = changed[:at] + strings.Replace(changed[at:], `"mappingDigest":"sha256:`+strings.Repeat("d", 64)+`"`, `"mappingDigest":"sha256:`+strings.Repeat("e", 64)+`"`, 1)
			case "preparation digest":
				at := strings.Index(changed, `"preparation":`)
				changed = changed[:at] + strings.Replace(changed[at:], strings.Repeat("d", 64), strings.Repeat("f", 64), 1)
			case "unknown mapping":
				changed = strings.Replace(changed, `"provider":""`, `"provider":"","unknown":true`, 1)
			case "lineage type":
				changed = strings.Replace(changed, `"lineage":[]`, `"lineage":{}`, 1)
			}
			if changed == string(reply) {
				t.Fatal("mutation did not match", scenario)
			}
			if input, err := retainedAssertedPreparationReply([]byte(changed), request, projected, admission); err == nil || len(input) != 0 {
				t.Fatal("custody mutation accepted", scenario)
			}
		})
	}
	replyNode, _ := parseRetainedJSON(reply)
	artifacts := []struct {
		key  string
		size int
	}{{"documentBytes", len(source.member("mapping").raw)}, {"frameBytes", len(projected.FrameJSON)}, {"combinedBytes", len(source.raw)}, {"requestBodyBytes", len(request)}, {"inputReplyBytes", len(reply)}, {"preparationBytes", len(replyNode.member("input").member("preparation").raw)}}
	for _, artifact := range artifacts {
		for _, role := range retainedAssertedPreparationRoles() {
			if _, ok := retainedRoleCaps(role)[artifact.key]; !ok {
				continue
			}
			t.Run(artifact.key+"/"+role, func(t *testing.T) {
				// Exercise the complete real encoded artifact at equality and one over.
				a := retainedB2Admission(t)
				a.limits[role][artifact.key] = int64(artifact.size)
				invoke := func() error {
					switch artifact.key {
					case "inputReplyBytes", "preparationBytes":
						_, err := retainedAssertedPreparationReply(reply, request, projected, a)
						return err
					default:
						checked := retainedDialectAuthoringCheck{Accepted: true, Document: string(source.member("mapping").raw), Digest: retainedDigest(source.member("mapping").raw)}
						_, err := retainedAssertedPreparationRequest(checked, projected, a)
						return err
					}
				}
				if err := invoke(); err != nil {
					t.Fatal("exact complete artifact bound", err)
				}
				a.limits[role][artifact.key]--
				err := invoke()
				if retainedContractCode(err) != "input-over-bound" {
					t.Fatal("independent lower artifact bound missed", artifact.key, role, err)
				}
			})
		}
	}
	// HTML escaping makes the encoded complete source larger than a case-sized
	// success. The lower complete source cap refuses rather than clipping cells.
	if len(source.raw) <= len(projected.CaseJSON) {
		t.Fatal("fixture does not exercise encoded envelope expansion")
	}
	a := retainedB2Admission(t)
	a.limits["preview"]["combinedBytes"] = int64(len(projected.CaseJSON))
	checked := retainedDialectAuthoringCheck{Accepted: true, Document: string(source.member("mapping").raw), Digest: retainedDigest(source.member("mapping").raw)}
	if _, err := retainedAssertedPreparationRequest(checked, projected, a); retainedContractCode(err) != "input-over-bound" {
		t.Fatal("case-sized success bypassed full source", err)
	}
}

func TestRetainedB3AssertedAdmissionAndTransport(t *testing.T) {
	request, _, _ := retainedB3Request(t)
	for _, role := range retainedAssertedPreparationRoles() {
		t.Run("missing-"+role, func(t *testing.T) {
			var calls atomic.Int64
			s := retainedD4aHTTP(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) })
			a := retainedB2Admission(t)
			delete(a.limits, role)
			if reply, err := s.retainedPreparationPOST(t.Context(), request, a); err == nil || len(reply) != 0 || calls.Load() != 0 {
				t.Fatal("missing role dispatched", err, calls.Load())
			}
		})
	}
	t.Run("wrong-role", func(t *testing.T) {
		a := retainedB2Admission(t)
		a.selection.Roles = append(a.selection.Roles, "producer-carrier")
		if retainedDirectPreparationAdmission(a) == nil {
			t.Fatal("producer role admitted for asserted")
		}
	})
	t.Run("missing-runner-owner", func(t *testing.T) {
		var status, selected atomic.Int64
		s := retainedD4aHTTP(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/status" {
				status.Add(1)
				w.Write([]byte(`{"conversionContracts":[]}`))
				return
			}
			selected.Add(1)
			w.WriteHeader(500)
		})
		got, err := s.prepareRetainedSavedInput(t.Context(), retainedB3AssertedFixture(t), "row-original", nil)
		if retainedContractCode(err) != "capability-unsupported" || len(got.InputJSON) != 0 || selected.Load() != 0 || status.Load() != 1 {
			t.Fatal("missing independent Runner owner", err, status.Load(), selected.Load())
		}
	})
	t.Run("canceled", func(t *testing.T) {
		var calls atomic.Int64
		s := retainedD4aHTTP(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		got, err := s.prepareRetainedSavedInput(ctx, retainedB3AssertedFixture(t), "row-original", nil)
		if !errors.Is(err, context.Canceled) || len(got.InputJSON) != 0 || calls.Load() != 0 {
			t.Fatal("canceled operation dispatched", err, calls.Load())
		}
	})
	t.Run("complete-request-refused-before-dispatch", func(t *testing.T) {
		var calls atomic.Int64
		s := retainedD4aHTTP(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
		a := retainedB2Admission(t)
		a.limits["preparation"]["requestBodyBytes"] = int64(len(request) - 1)
		if reply, err := s.retainedPreparationPOST(t.Context(), request, a); retainedContractCode(err) != "input-over-bound" || len(reply) != 0 || calls.Load() != 0 {
			t.Fatal("encoded request dispatched", err, calls.Load())
		}
	})
	t.Run("acknowledgement-unknown", func(t *testing.T) {
		var calls atomic.Int64
		s := retainedD4aHTTP(t, func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			connection.Close()
		})
		reply, err := s.retainedPreparationPOST(t.Context(), request, retainedB2Admission(t))
		if err == nil || len(reply) != 0 || calls.Load() != 1 || !strings.Contains(err.Error(), "acknowledgement is unknown") {
			t.Fatal("uncertainty retried/accepted", err, calls.Load())
		}
	})
}

// Owned actual Runner, with a transparent observer that only counts permitted
// routes. No Gateway/profile route is available to an asserted preparation.
func retainedB3ActualRunner(t *testing.T, s *Server, profiles []byte, maxSelected, maxControl int64) (*atomic.Int64, *atomic.Int64) {
	t.Helper()
	bin, runtime := os.Getenv("JPACK_RUNNER_TEST_BIN"), os.Getenv("JPACK_BIN")
	if bin == "" || runtime == "" {
		t.Skip("requires explicitly verified scratch Runner and Runtime; skip is not qualification")
	}
	for _, path := range []string{bin, runtime} {
		if !filepath.IsAbs(path) {
			t.Fatal("explicit absolute component paths required")
		}
	}
	s.cfg.JpackBin = runtime
	state, inputRoot := t.TempDir(), t.TempDir()
	for _, dir := range []string{state, inputRoot} {
		if err := os.Chmod(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	j := &jobsCompanion{bin: bin, runtime: runtime, dir: state, workspace: "retained-b3", owner: "local-owner", inputRoot: inputRoot, profiles: profiles, stop: make(chan struct{})}
	if _, err := j.start("", ""); err != nil {
		t.Fatal("actual Runner start", err)
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
	selected, control := new(atomic.Int64), new(atomic.Int64)
	observer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/status":
			if control.Add(1) > maxControl {
				t.Error("actual fixture control cap")
				w.WriteHeader(500)
				return
			}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/input-profiles" && maxControl > 1:
			if control.Add(1) > maxControl {
				t.Error("actual fixture control cap")
				w.WriteHeader(500)
				return
			}
		case r.Method == http.MethodPost && r.URL.Path == retainedRunnerPrefix+"inputs/next":
			if selected.Add(1) > maxSelected {
				t.Error("actual fixture selected request cap")
				w.WriteHeader(500)
				return
			}
		default:
			t.Error("unexpected component route", r.Method, r.URL.Path)
			w.WriteHeader(500)
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(observer.Close)
	done := make(chan struct{})
	s.jobs = &jobsCompanion{url: observer.URL, token: token, done: done, stop: make(chan struct{})}
	t.Cleanup(func() { close(done) })
	return selected, control
}

func TestRetainedB3ActualAssertedPreparation(t *testing.T) {
	s, _ := newTestServer(t, false)
	selected, control := retainedB3ActualRunner(t, s, []byte(`[]`), 1, 1)
	if s.localGateway != nil {
		t.Fatal("asserted fixture unexpectedly has Gateway authority")
	}
	saved := retainedB3AssertedFixture(t)
	before, _ := json.Marshal(saved)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	result, err := s.prepareRetainedSavedInput(ctx, saved, "row-original", nil)
	if err != nil || len(result.InputJSON) == 0 {
		t.Fatal("actual asserted selected preparation", err)
	}
	// This handwritten materialization deliberately omits the legacy v2 root
	// defaults that Runner emits, so this fixture distinguishes both domains.
	if result.Runtime.Digest != retainedDigest([]byte(result.Runtime.Document)) || !retainedSpecDigest.MatchString(result.RunnerMappingDigest) || result.RunnerMappingDigest == result.Runtime.Digest {
		t.Fatal("Runtime document and Runner mapping digest domains were conflated")
	}
	if selected.Load() != 1 || control.Load() != 1 {
		t.Fatal("actual call counts", selected.Load(), control.Load())
	}
	after, _ := json.Marshal(saved)
	if !bytes.Equal(before, after) {
		t.Fatal("preparation changed original saved input")
	}
	if !result.Runtime.Accepted || !slices.Equal(result.Selection.Roles, retainedAssertedPreparationRoles()) || len(result.Limits) != 3 {
		t.Fatal("asserted role evidence incomplete")
	}
	node, err := parseRetainedJSON(result.InputJSON)
	if err != nil {
		t.Fatal(err)
	}
	if !retainedExactObject(node.member("source"), []string{"conversionContract", "mapping", "mappingDigest", "retainedCaseJson"}) {
		t.Fatal("actual sole case custody missing")
	}
	admission, err := negotiateRetainedRoles(result.Selection, retainedAssertedPreparationRoles(), false, retainedD4aReports(), result.Limits)
	if err != nil {
		t.Fatal(err)
	}
	projected, err := projectRetainedAssertedInput(saved, "row-original", admission)
	if err != nil {
		t.Fatal(err)
	}
	text, _ := retainedMemberString(node.member("source"), "retainedCaseJson")
	if text != projected.CaseJSON {
		t.Fatal("actual exact original case lost")
	}
	facts := node.member("facts")
	if string(facts.member("decimal").raw) != `"-0.00"` || string(facts.member("flag").raw) != "false" || string(facts.member("text").raw) != `"  kept  "` || facts.member("null") != nil || facts.member("blank") != nil || facts.member("missing") != nil {
		t.Fatal("actual selected conversion results", string(facts.raw))
	}
	if !strings.Contains(text, `"number":"-0"`) || !strings.Contains(text, `"null":null`) || !strings.Contains(text, `"unmapped"`) {
		t.Fatal("actual raw cells/spelling lost")
	}
	if string(node.member("evidence").member("license").raw) != `"absent"` {
		t.Fatal("actual evidence")
	}
	prep := node.member("preparation")
	if len(prep.member("lineage").elements) == 0 || len(prep.member("outcomes").elements) == 0 || !bytes.Contains(prep.raw, []byte(`"columnTransformation"`)) {
		t.Fatal("actual recomputed transformation evidence missing")
	}
	t.Logf("actual asserted: owned Runner=1, selected Runtime contract/validate/check=3 (fixed owner), selected requests=%d, control reads=%d, Gateway calls=0; development components, no public activation", selected.Load(), control.Load())
}

func TestRetainedB3ActualMaterializedGeneratedPreparation(t *testing.T) {
	bundle := os.Getenv("JPS_TEST_GATEWAY_BUNDLE")
	if bundle == "" || os.Getenv("JPACK_RUNNER_TEST_BIN") == "" || os.Getenv("JPACK_BIN") == "" {
		t.Skip("requires explicitly verified scratch Gateway, Runner and Runtime; skip is not qualification")
	}
	if !filepath.IsAbs(bundle) {
		t.Fatal("explicit absolute Gateway bundle required")
	}
	s, doc, record, _, _ := retainedD3aOwnerFixture(t, bundle)
	// Internal fixture only: public B4 decode/save is intentionally unopened.
	doc.Recipe.Qualification = retainedB3Qualification(t, true)
	saved := retainedB3SealFixture(t, doc)
	if saved.Document.Imports == nil || len(saved.Document.Imports) != 0 {
		t.Fatal("actual generated regression requires the original explicit empty imports array")
	}
	s.localGateway = &localGateway{bundle: bundle, executable: os.Args[0], inputHandoffs: s.inputHandoffPath()}
	t.Cleanup(s.localGateway.close)
	profiles, _, _, err := s.retainedPrivateProfiles()
	if err != nil {
		t.Fatal(err)
	}
	selected, control := retainedB3ActualRunner(t, s, profiles, 1, 3)
	// Count the single new acquisition without changing the verified binary/pin.
	pin, err := s.localGateway.ensure(s.assistant)
	if err != nil {
		t.Fatal(err)
	}
	target, err := url.Parse(pin.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	transport := &http.Transport{Proxy: nil}
	proxy.Transport = transport
	t.Cleanup(transport.CloseIdleConnections)
	var acquisitions atomic.Int64
	observer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/acquire" && acquisitions.Add(1) > 1 {
			t.Error("fixture exceeded one new acquisition")
			w.WriteHeader(500)
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(observer.Close)
	s.localGateway.mu.Lock()
	s.localGateway.pin.URL = observer.URL
	s.localGateway.mu.Unlock()
	before, _ := json.Marshal(saved)
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	result, err := s.prepareRetainedSavedInput(ctx, saved, record.ID, nil)
	if err != nil || len(result.InputJSON) == 0 {
		t.Fatal("actual materialized generated selected preparation", err)
	}
	// This handwritten materialization deliberately omits the legacy v2 root
	// defaults that Runner emits, so this fixture distinguishes both domains.
	if result.Runtime.Digest != retainedDigest([]byte(result.Runtime.Document)) || !retainedSpecDigest.MatchString(result.RunnerMappingDigest) || result.RunnerMappingDigest == result.Runtime.Digest {
		t.Fatal("Runtime document and Runner mapping digest domains were conflated")
	}
	if acquisitions.Load() != 1 || selected.Load() != 1 || control.Load() != 3 {
		t.Fatal("actual generated call caps", acquisitions.Load(), selected.Load(), control.Load())
	}
	after, _ := json.Marshal(saved)
	if !bytes.Equal(before, after) {
		t.Fatal("saved snapshot changed")
	}
	n, err := parseRetainedJSON(result.InputJSON)
	if err != nil {
		t.Fatal(err)
	}
	source := n.member("source")
	research := source.member("sources").member("research")
	if !retainedExactObject(research, []string{"retainedResponseJson"}) || source.member("import") != nil || source.member("retainedCaseJson") != nil {
		t.Fatal("actual sole signed source missing")
	}
	response, _ := retainedMemberString(research, "retainedResponseJson")
	signed, err := parseRetainedJSON([]byte(response))
	if err != nil {
		t.Fatal(err)
	}
	carrier := signed.member("result")
	snapshotText, _ := retainedMemberString(carrier, "originalSnapshotJson")
	snapshot, err := parseRetainedJSON([]byte(snapshotText))
	if err != nil {
		t.Fatal(err)
	}
	if !retainedExactObject(snapshot.member("prepared"), nil) {
		t.Fatal("pre-Runner selected projection must be explicitly empty")
	}
	if snapshot.member("record") == nil || snapshot.member("research") == nil || snapshot.member("qualification") == nil || snapshot.member("reviews") == nil {
		t.Fatal("original retained snapshot siblings lost")
	}
	hash, _ := retainedMemberString(carrier, "originalSnapshotSha256")
	if hash != retainedDigest([]byte(snapshotText)) {
		t.Fatal("exact signed snapshot identity changed")
	}
	if !slices.Equal(result.Selection.Roles, retainedPreparationRoles()) || !bytes.Contains(n.member("preparation").raw, []byte(`"columnTransformation"`)) {
		t.Fatal("generated owner did not produce actual retained transformation")
	}
	if string(n.member("facts").member("company").member("country").raw) != `"CA"` {
		t.Fatal("actual signed research result missing")
	}
	mapping := source.member("mapping")
	if len(mapping.member("case").member("facts").elements) != 0 || len(mapping.member("case").member("evidence").elements) != 0 {
		t.Fatal("duplicate generated facts")
	}
	t.Logf("actual generated: owned Runner=1, owned Gateway=1, selected Runtime contract/validate/check=3 (fixed owner), acquisitions=%d, selected requests=%d, control reads=%d; auxiliary Gateway CLI count unavailable, bounded outer timeout only", acquisitions.Load(), selected.Load(), control.Load())
}

// The reply reader must detect limit+1 without returning a clipped artifact.
func TestRetainedB3CompleteReplyReader(t *testing.T) {
	a := retainedB2Admission(t)
	a.limits["preview"]["inputReplyBytes"] = 3
	raw, err := readRetainedArtifact(io.LimitReader(strings.NewReader("1234"), 4), a, "inputReplyBytes")
	if retainedContractCode(err) != "input-over-bound" || raw != nil {
		t.Fatal("reply reader clipped instead of refusing", raw, err)
	}
}
