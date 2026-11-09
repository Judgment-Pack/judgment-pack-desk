package desk

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Cells and expected outcomes are handwritten; no fixture records a program run.
const retainedB2Cells = `{"blank":"","null":null,"text":"  kept  ","unmapped":"<opaque>\\tail\n","decimal":"-0.00","number":"1e3","flag":"no","evidence":"absent"}`

func retainedB2Snapshot(t *testing.T, cells string) inputPreparationReply {
	t.Helper()
	content := "name,value\nExample,original\n"
	sourceID := "source_" + digestOf([]byte("csv\n"+content))
	doc := inputPreparation{Version: 1, ID: "prep_" + strings.Repeat("a", 32), Name: "Original", Recipe: inputRecipe{Intent: "Original recipe"}, RecipeVersion: 1, CreatedAt: "2026-10-01T00:00:00Z", UpdatedAt: "2026-10-01T00:00:00Z", Records: []preparedInputRecord{{ID: "row-original", Name: "Example", Values: map[string]string{"placeholder": "value"}, SourceID: sourceID, Row: 1, Class: "asserted", Status: "provided"}}, Imports: []inputImport{{ID: sourceID, Kind: "csv", Name: "original.csv", Digest: retainedDigest([]byte(content)), Content: content, At: "2026-10-01T00:00:00Z", RecipeVersion: 1}}, Activity: []inputActivity{}}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.Replace(string(raw), `{"placeholder":"value"}`, cells, 1))
	saved, err := decodeInputPreparation(doc.ID, raw)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}
func retainedB2Selected(t *testing.T, saved *inputPreparationReply) {
	t.Helper()
	selection := inputRetainedContract()
	profile := "base"
	q := &inputQualification{Kind: "pack", Key: "pack.json", Pack: `{"id":"pack","version":"1","decision":{}}`, ConversionContract: &selection, Conversions: map[string]inputConversion{}, Mapping: inputRecordDialect{Version: 2, Profile: &profile, Sources: []json.RawMessage{}, Case: &inputCaseDialect{Facts: []inputDialectFact{}, Evidence: []inputDialectEvidence{}}}}
	q.Digest = retainedDigest([]byte(q.Pack))
	for _, rule := range []struct{ target, column, kind string }{{"/text", "text", "text"}, {"/decimal", "decimal", "decimal"}, {"/number", "number", "number"}, {"/flag", "flag", "boolean"}, {"/blank", "blank", "text"}, {"/null", "null", "text"}, {"/missing", "missing", "text"}, {"/research", "missing", "research-value"}} {
		conversion := &inputRetainedConversion{ContractVersion: "1", Column: rule.column, Kind: rule.kind}
		sidecar := inputConversion{Kind: rule.kind}
		if rule.kind == "boolean" {
			yes, no := "yes", "no"
			conversion.TrueValue = &yes
			conversion.FalseValue = &no
			sidecar.TrueValue = yes
			sidecar.FalseValue = no
		}
		q.Mapping.Case.Facts = append(q.Mapping.Case.Facts, inputDialectFact{Target: rule.target, Source: "/retained", Conversion: conversion})
		q.Conversions[rule.target] = sidecar
	}
	q.Mapping.Case.Evidence = append(q.Mapping.Case.Evidence, inputDialectEvidence{Requirement: "proof", Source: "/retained", Availability: &inputRetainedAvailability{ContractVersion: "1", Column: "evidence"}})
	saved.Document.Recipe.Qualification = q
}
func retainedB2Admission(t *testing.T) *retainedAdmission {
	t.Helper()
	admission, err := negotiateRetainedRoles(retainedD2Selection(t, false), []string{"document-validation", "preview", "preparation"}, false, retainedD2Reports(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	return admission
}

func TestRetainedB2AssertedCompleteCase(t *testing.T) {
	saved := retainedB2Snapshot(t, retainedB2Cells)
	retainedB2Selected(t, &saved)
	result, err := projectRetainedAssertedInput(saved, "row-original", retainedB2Admission(t))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := parseRetainedJSON(result.FrameJSON)
	if err != nil {
		t.Fatal(err)
	}
	if !retainedExactObject(frame, []string{"contractVersion", "origin", "values", "research"}) || string(frame.member("research").raw) != "null" {
		t.Fatal("closed asserted frame", string(result.FrameJSON))
	}
	want, _ := parseRetainedJSON([]byte(retainedB2Cells))
	if !retainedJSONEqual(frame.member("values"), want) {
		t.Fatal("all original columns must survive", string(result.FrameJSON))
	}
	sourceCase, err := parseRetainedJSON([]byte(result.CaseJSON))
	if err != nil {
		t.Fatal(err)
	}
	if !retainedExactObject(sourceCase, []string{"values", "prepared", "origin", "retained"}) || !bytesEqualRetained(sourceCase.member("retained").raw, result.FrameJSON) {
		t.Fatal("case siblings/frame occurrence changed", result.CaseJSON)
	}
	origin := sourceCase.member("origin")
	if !retainedExactObject(origin, []string{"preparationId", "recordId", "sourceId", "sourceDigest", "row", "importedAt", "importRecipeVersion", "recipeVersion", "packDigest", "originalValues", "conversions"}) || !retainedJSONEqual(origin.member("originalValues"), want) {
		t.Fatal("complete provenance", result.CaseJSON)
	}
	if origin.member("sourceDigest") == nil || string(sourceCase.member("prepared").member("/decimal").raw) != `"-0.00"` || string(sourceCase.member("prepared").member("/number").raw) != "1e3" || string(sourceCase.member("prepared").member("/flag").raw) != "false" {
		t.Fatal("exact prepared candidates", result.CaseJSON)
	}
	got := map[string]retainedAssertedDiagnostic{}
	for _, d := range result.Diagnostics {
		got[d.Target] = d
	}
	for target, want := range map[string]string{"/blank": "blank", "/null": "legacy-null", "/missing": "missing"} {
		if got[target].Omitted != want {
			t.Fatal(target, got[target])
		}
	}
	if got["/research"].Code != "research-context" || got["proof"].CandidateJSON != `"absent"` {
		t.Fatal("ordered context/availability", got)
	}
	if saved.Document.Imports[0].Content != "name,value\nExample,original\n" {
		t.Fatal("import mutated")
	}
}
func bytesEqualRetained(a, b []byte) bool { return string(a) == string(b) }

func TestRetainedB2CodecCASReloadAndLegacyMembership(t *testing.T) {
	saved := retainedB2Snapshot(t, retainedB2Cells)
	raw, err := json.Marshal(saved.Document)
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range []string{"memory", "file"} {
		t.Run(backend, func(t *testing.T) {
			var repo inputPreparationRepository
			if backend == "memory" {
				repo = &memoryPreparationRepository{data: map[string][]byte{saved.Document.ID: raw}}
			} else {
				server, _ := newTestServer(t, false)
				repo = server.preparationRepository()
				root, err := server.openInputPreparations(true)
				if err != nil {
					t.Fatal(err)
				}
				root.Close()
				if err = os.WriteFile(filepath.Join(server.projectDir, inputPreparationDir, saved.Document.ID+".json"), raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			current, err := repo.Get(context.Background(), saved.Document.ID)
			if err != nil {
				t.Fatal(err)
			}
			beforeImport := current.Document.Imports[0]
			current.Document.Name = "Reviewed revision"
			current.Document.RecipeVersion = 2
			current.Document.Recipes = append(current.Document.Recipes, inputRecipeRevision{Version: 2, Name: current.Document.Name, Recipe: current.Document.Recipe, At: "2026-10-02T00:00:00Z"})
			next, err := repo.CompareAndSwap(context.Background(), current.Document, current.SHA256)
			if err != nil {
				t.Fatal(err)
			}
			reloaded, err := repo.Get(context.Background(), saved.Document.ID)
			if err != nil {
				t.Fatal(err)
			}
			if next.SHA256 != reloaded.SHA256 || next.SHA256 == current.SHA256 || reloaded.Document.Imports[0] != beforeImport {
				t.Fatal("CAS/readback/import custody changed")
			}
			row := reloaded.Document.Records[0]
			if _, present := row.Values["null"]; present {
				t.Fatal("null cannot masquerade as mutable blank")
			}
			if blank, present := row.Values["blank"]; !present || blank != "" {
				t.Fatal("blank lost")
			}
			if _, present := legacyInputValues(row)["null"]; !present {
				t.Fatal("legacy null column lost")
			}
			columns := assistantDialectColumns(reloaded.Document)
			if !contains(columns, "null") || !contains(columns, "blank") || contains(columns, "missing") {
				t.Fatal("inventory membership changed", columns)
			}
			view := assistantDialectRecordView(row, 2)
			values := view["values"].(map[string]any)
			if value, ok := values["null"]; !ok || value.(map[string]any)["text"] != "" {
				t.Fatal("legacy record view lost null-as-blank")
			}
			// Legacy preparation retains its former Original/origin.originalValues map.
			q := &inputQualification{Digest: "sha256:pack", Mapping: inputRecordDialect{Version: 2, Sources: []json.RawMessage{}, Case: &inputCaseDialect{Facts: []inputDialectFact{{Target: "/text", Source: "/values/text"}}, Evidence: []inputDialectEvidence{}}}, Conversions: map[string]inputConversion{"/text": {Kind: "text"}}}
			reloaded.Document.Recipe.Qualification = q
			prepared, err := prepareInputRecord(reloaded.Document, row)
			if err != nil {
				t.Fatal(err)
			}
			if value, present := prepared.Original["null"]; !present || value != "" {
				t.Fatal("legacy Original changed")
			}
			legacy, _ := parseRetainedJSON(prepared.Input)
			originals := legacy.member("source").member("case").member("origin").member("originalValues")
			if originals.member("null") == nil || string(originals.member("null").raw) != `""` || originals.member("missing") != nil {
				t.Fatal("legacy serialized original values changed", string(prepared.Input))
			}
			retainedB2Selected(t, &reloaded)
			exact, err := projectRetainedAssertedInput(reloaded, row.ID, retainedB2Admission(t))
			if err != nil {
				t.Fatal(err)
			}
			exactNode, _ := parseRetainedJSON([]byte(exact.CaseJSON))
			if string(exactNode.member("retained").member("values").member("null").raw) != "null" {
				t.Fatal("null collapsed across CAS", exact.CaseJSON)
			}
		})
	}
}

func TestRetainedB2MutationAndSnapshotRefusals(t *testing.T) {
	mutations := map[string]func(*inputPreparationReply){
		"explicit-null-to-blank": func(s *inputPreparationReply) { s.Document.Records[0].Values["null"] = "" },
		"changed-string":         func(s *inputPreparationReply) { s.Document.Records[0].Values["text"] = "changed" },
		"deleted-string":         func(s *inputPreparationReply) { delete(s.Document.Records[0].Values, "text") },
		"added-cell":             func(s *inputPreparationReply) { s.Document.Records[0].Values["new"] = "" },
		"record-id":              func(s *inputPreparationReply) { s.Document.Records[0].ID = "replacement" },
		"source-id":              func(s *inputPreparationReply) { s.Document.Records[0].SourceID = "replacement" },
		"row":                    func(s *inputPreparationReply) { s.Document.Records[0].Row = 2 },
		"class":                  func(s *inputPreparationReply) { s.Document.Records[0].Class = "generated" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			saved := retainedB2Snapshot(t, retainedB2Cells)
			retainedB2Selected(t, &saved)
			mutate(&saved)
			if _, err := json.Marshal(saved.Document); err == nil {
				t.Fatal("mutated custody encoded")
			}
			if _, err := projectRetainedAssertedInput(saved, saved.Document.Records[0].ID, retainedB2Admission(t)); err == nil {
				t.Fatal("mutated custody projected")
			}
		})
	}
	for name, mutate := range map[string]func(*inputPreparationReply){
		"collection-sha":     func(s *inputPreparationReply) { s.SHA256 = strings.Repeat("0", 64) },
		"source-content":     func(s *inputPreparationReply) { s.Document.Imports[0].Content = "changed" },
		"source-digest":      func(s *inputPreparationReply) { s.Document.Imports[0].Digest = "sha256:" + strings.Repeat("0", 64) },
		"missing-source":     func(s *inputPreparationReply) { s.Document.Imports = nil },
		"duplicate-record":   func(s *inputPreparationReply) { s.Document.Records = append(s.Document.Records, s.Document.Records[0]) },
		"all-string-mutated": func(s *inputPreparationReply) { s.Document.Records[0].Values["text"] = "changed" },
	} {
		t.Run(name, func(t *testing.T) {
			saved := retainedB2Snapshot(t, `{"text":"original"}`)
			retainedB2Selected(t, &saved)
			mutate(&saved)
			if _, err := projectRetainedAssertedInput(saved, "row-original", retainedB2Admission(t)); err == nil {
				t.Fatal("mismatched source/raw snapshot projected")
			}
		})
	}
	// Generic JSON decoding is not the private custody attacher.
	saved := retainedB2Snapshot(t, retainedB2Cells)
	raw, _ := json.Marshal(saved.Document)
	var untrusted inputPreparation
	if err := json.Unmarshal(raw, &untrusted); err != nil {
		t.Fatal(err)
	}
	saved.Document = untrusted
	retainedB2Selected(t, &saved)
	if _, err := projectRetainedAssertedInput(saved, "row-original", retainedB2Admission(t)); err == nil {
		t.Fatal("browser-shaped document created custody")
	}
}

func TestRetainedB2ReviewCopiesAndLegacyDigest(t *testing.T) {
	saved := retainedB2Snapshot(t, retainedB2Cells)
	record := saved.Document.Records[0]
	legacy := record
	legacy.rawCells = nil
	legacy.Values = map[string]string{"blank": "", "null": "", "text": "  kept  ", "unmapped": "<opaque>\\tail\n", "decimal": "-0.00", "number": "1e3", "flag": "no", "evidence": "absent"}
	legacyRaw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if inputRecordDigest(record) != digestOf(legacyRaw) {
		t.Fatal("historical review digest changed")
	}
	review := inputRecordReview{RecordID: record.ID, RecordDigest: digestOf(legacyRaw), RecipeVersion: 1, Decision: "accepted", At: "2026-10-02T00:00:00Z", Reviewer: inputReviewActor{Kind: "local-access"}}
	saved.Document.Records[0].Review = &review
	saved.Document.Reviews = []inputRecordReviewEvent{{Review: review, Record: record}}
	raw, err := json.Marshal(saved.Document)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := decodeInputPreparation(saved.Document.ID, raw)
	if err != nil {
		t.Fatal(err)
	}
	if err = validateInputRecordReviews(reloaded.Document); err != nil {
		t.Fatal("unrelated review edits rejected", err)
	}
	// Review removal on a copied record never erases or mutates original raw cells.
	copied := reloaded.Document.Records[0]
	copied.Review = nil
	copied.Name = "Renamed"
	copied.Status = "inspected"
	copyJSON, err := json.Marshal(copied)
	if err != nil {
		t.Fatal(err)
	}
	node, _ := parseRetainedJSON(copyJSON)
	if string(node.member("values").member("null").raw) != "null" || reloaded.Document.Records[0].Review == nil {
		t.Fatal("copy/raw custody changed")
	}
	reloaded.Document.Records[0].Values["null"] = ""
	if inputRecordDigest(reloaded.Document.Records[0]) != "" {
		t.Fatal("invalid custody supplied a review identity")
	}
	reloaded.Document.Records[0].Review.RecordDigest = ""
	if validateInputRecordReviews(reloaded.Document) == nil {
		t.Fatal("empty invalid digest accepted")
	}
	if _, err = json.Marshal(reloaded.Document); err == nil {
		t.Fatal("corrupted reviewed custody encoded")
	}
}

func TestRetainedB2LegacyStringEncodingUnchanged(t *testing.T) {
	record := preparedInputRecord{ID: "row", Name: "Example", Values: map[string]string{"blank": "", "value": "001"}, SourceID: "source", Row: 1, Class: "asserted", Status: "provided"}
	want := `{"id":"row","name":"Example","values":{"blank":"","value":"001"},"sourceId":"source","row":1,"class":"asserted","status":"provided"}`
	raw, err := json.Marshal(record)
	if err != nil || string(raw) != want {
		t.Fatal("legacy string encoding changed", string(raw), err)
	}
	saved := retainedB2Snapshot(t, `{"blank":"","value":"001"}`)
	raw, err = json.Marshal(saved.Document.Records[0])
	if err != nil {
		t.Fatal(err)
	}
	var decoded preparedInputRecord
	if err = json.Unmarshal(raw, &decoded); err != nil || !reflect.DeepEqual(decoded.Values, map[string]string{"blank": "", "value": "001"}) {
		t.Fatal("string record changed", err)
	}
	// Empty and nil legacy record collections keep their historical read forms.
	for _, collection := range []string{"null", "[]"} {
		legacyDoc := `{"version":1,"id":"prep_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","name":"Empty","recipe":{"intent":"Empty"},"recipeVersion":1,"recipes":null,"createdAt":"","updatedAt":"","records":` + collection + `,"imports":null,"activity":null}`
		if _, err := decodeInputPreparation("prep_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", []byte(legacyDoc)); err != nil {
			t.Fatal("legacy empty collection refused", err)
		}
	}
	// Existing all-string record edits remain legal, and become fresh custody on reload.
	saved.Document.Records[0].Values["value"] = "002"
	if _, err = json.Marshal(saved.Document); err != nil {
		t.Fatal("legacy string edit refused", err)
	}
}

func TestRetainedB2IndependentBounds(t *testing.T) {
	for _, n := range []int{16000, 16001} {
		saved := retainedB2Snapshot(t, `{"text":"`+strings.Repeat("v", n)+`"}`)
		retainedB2Selected(t, &saved)
		_, err := projectRetainedAssertedInput(saved, "row-original", retainedB2Admission(t))
		if n == 16000 && err != nil || n == 16001 && retainedContractCode(err) != "input-over-bound" {
			t.Fatal("cell boundary", n, err)
		}
	}
	for _, count := range []int{1000, 1001} {
		cells := make([]string, count)
		for i := range cells {
			cells[i] = fmt.Sprintf(`"column-%d":""`, i)
		}
		saved := retainedB2Snapshot(t, "{"+strings.Join(cells, ",")+"}")
		retainedB2Selected(t, &saved)
		_, err := projectRetainedAssertedInput(saved, "row-original", retainedB2Admission(t))
		if count == 1000 && err != nil || count == 1001 && retainedContractCode(err) != "input-over-bound" {
			t.Fatal("count boundary", count, err)
		}
	}
	for name, cells := range map[string]string{"nested": "{\"text\":" + strings.Repeat("[", 65) + "0" + strings.Repeat("]", 65) + "}", "surrogate": `{"text":"\ud800"}`, "nonstring": `{"text":false}`, "duplicate": `{"text":"first","te\u0078t":"second"}`} {
		t.Run(name, func(t *testing.T) {
			if _, err := retainedAssertedCells([]byte(cells)); err == nil {
				t.Fatal("malformed/deep frame accepted")
			}
		})
	}
	saved := retainedB2Snapshot(t, retainedB2Cells)
	retainedB2Selected(t, &saved)
	original, err := projectRetainedAssertedInput(saved, "row-original", retainedB2Admission(t))
	if err != nil {
		t.Fatal(err)
	}
	mapping, _ := json.Marshal(saved.Document.Recipe.Qualification.Mapping)
	for key, size := range map[string]int{"documentBytes": len(mapping), "frameBytes": len(original.FrameJSON), "combinedBytes": len(original.CaseJSON)} {
		for _, delta := range []int{0, -1} {
			admission := retainedB2Admission(t)
			// A lower owner cap constrains the actual encoded artifact at its boundary.
			owner := "preview"
			if key == "frameBytes" {
				owner = "document-validation"
			}
			admission.limits[owner][key] = int64(size + delta)
			_, err := projectRetainedAssertedInput(saved, "row-original", admission)
			if delta == 0 && err != nil || delta == -1 && retainedContractCode(err) != "input-over-bound" {
				t.Fatal(key, "actual complete byte boundary", size, delta, err)
			}
		}
	}
	for name, mutate := range map[string]func(*retainedAdmission){"missing-role": func(a *retainedAdmission) { delete(a.limits, "preview") }, "generated": func(a *retainedAdmission) { a.generated = true }} {
		admission := retainedB2Admission(t)
		mutate(admission)
		if _, err := projectRetainedAssertedInput(saved, "row-original", admission); err == nil {
			t.Fatal(name, "accepted")
		}
	}
}

func TestRetainedB2DiagnosticConformance(t *testing.T) {
	for _, row := range []struct{ value, kind, want, code string }{
		{"-0", "number", "-0", ""}, {"1e3", "number", "1e3", ""}, {"1.00", "decimal", `"1.00"`, ""}, {"-0.00", "decimal", `"-0.00"`, ""}, {"no", "boolean", "false", ""}, {"  kept  ", "text", `"  kept  "`, ""},
		{" 1", "number", "", "number-encoding"}, {"01", "number", "", "number-encoding"}, {"1e309", "number", "", "number-range"}, {"0e309", "number", "", "number-exponent"}, {"1e-309", "number", "", "number-exponent"}, {"1e-999", "number", "", "number-exponent"}, {"9007199254740992", "number", "", "number-range"}, {"0.10000000000000001", "number", "", "number-precision"}, {"0.1", "number", "0.1", ""}, {"1e3", "decimal", "", "decimal-encoding"}, {"No", "boolean", "", "boolean-spelling"},
	} {
		token, _ := json.Marshal(row.value)
		d := retainedAssertedConversion(map[string]json.RawMessage{"column": token}, "/target", "column", inputConversion{Kind: row.kind, TrueValue: "yes", FalseValue: "no"}, false)
		if d.CandidateJSON != row.want || d.Code != row.code || d.Code != "" && d.Message == "" {
			t.Fatal(row, d)
		}
	}
	for _, blank := range []string{"", "\t\n\v\f\r \u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000"} {
		token, _ := json.Marshal(blank)
		d := retainedAssertedConversion(map[string]json.RawMessage{"column": token}, "/target", "column", inputConversion{Kind: "text"}, false)
		if d.Omitted != "blank" {
			t.Fatal("pinned blank set", d)
		}
	}
	// Zero width space is outside the pinned blank set.
	token, _ := json.Marshal("\u200b")
	if d := retainedAssertedConversion(map[string]json.RawMessage{"column": token}, "/target", "column", inputConversion{Kind: "text"}, false); d.CandidateJSON == "" {
		t.Fatal(d)
	}
}

func TestRetainedB2LegacyAmbiguousHistoryRemainsReadable(t *testing.T) {
	original := retainedB2Snapshot(t, retainedB2Cells)
	raw, err := json.Marshal(original.Document)
	if err != nil {
		t.Fatal(err)
	}
	for name, rewrite := range map[string]func(string) string{
		"nested-duplicate": func(s string) string { return strings.Replace(s, `"blank":""`, `"blank":"first","blank":"last"`, 1) },
		"records-alias":    func(s string) string { return strings.Replace(s, `"records":`, `"Records":`, 1) },
		"values-alias":     func(s string) string { return strings.Replace(s, `"values":`, `"Values":`, 1) },
		"competing-values-alias": func(s string) string {
			return strings.Replace(s, `"values":`, `"Values":{"earlier":"value"},"values":`, 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			ambiguous := []byte(rewrite(string(raw)))
			var legacy inputPreparation
			if err := decodeDataJSON(ambiguous, &legacy); err != nil {
				t.Fatal("fixture is outside prior readable domain", err)
			}
			saved, err := decodeInputPreparation(original.Document.ID, ambiguous)
			if err != nil {
				t.Fatal("legacy history newly refused", err)
			}
			if saved.Document.cellCustody != nil || !reflect.DeepEqual(saved.Document.Records[0].Values, legacy.Records[0].Values) {
				t.Fatal("ambiguous source was silently selected or legacy values changed")
			}
			// Ordinary legacy serialization still has its former decoded string view.
			if _, err := json.Marshal(saved.Document); err != nil {
				t.Fatal("legacy encoding newly refused", err)
			}
			q := &inputQualification{Digest: "sha256:pack", Mapping: inputRecordDialect{Version: 2, Sources: []json.RawMessage{}, Case: &inputCaseDialect{Facts: []inputDialectFact{{Target: "/text", Source: "/values/text"}}, Evidence: []inputDialectEvidence{}}}, Conversions: map[string]inputConversion{"/text": {Kind: "text"}}}
			saved.Document.Recipe.Qualification = q
			prepared, err := prepareInputRecord(saved.Document, saved.Document.Records[0])
			if err != nil {
				t.Fatal("ordinary legacy prepare newly refused", err)
			}
			if !reflect.DeepEqual(prepared.Original, legacy.Records[0].Values) {
				t.Fatal("ordinary ambiguous legacy view changed")
			}
			retainedB2Selected(t, &saved)
			if _, err := projectRetainedAssertedInput(saved, "row-original", retainedB2Admission(t)); err == nil {
				t.Fatal("ambiguous history produced selected case")
			}
			if _, err := json.Marshal(saved.Document); err == nil {
				t.Fatal("selected opt-in erased ambiguous custody")
			}
		})
	}
}
