package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDialectAuthoringRuntimeBoundary(t *testing.T) {
	bin := requireBinary(t)
	s := &Server{cfg: Config{JpackBin: bin}}
	q := qualificationFixture()
	mapping, _ := json.Marshal(q.Mapping)
	raw, report, err := s.checkDialectAuthoring(context.Background(), mapping, q.Pack)
	if err != nil || !report.accepted(true) {
		t.Fatalf("check: %v %s", err, raw)
	}
	if report.Digest != "sha256:"+digestOf([]byte(report.Document)) {
		t.Fatal("canonical digest mismatch")
	}
	for _, version := range []string{"2", "3"} {
		raw, report, err := s.checkDialectAuthoring(context.Background(), []byte(`{"version":`+version+`,"case":{"facts":[],"evidence":[]}}`), "")
		if err != nil || !report.accepted(false) {
			t.Fatalf("v%s: %v %s", version, err, raw)
		}
	}
	for _, document := range []string{
		`{"version":2,"version":3}`,
		`{"version":3,"sources":[{"name":"unsupported","kind":"research"}]}`,
		`{"version":2,"case":{"facts":[{"target":"/company/country","source":"/values/Country"},{"target":"/company/country","source":"/values/Other"}],"evidence":[]}}`,
	} {
		raw, report, err := s.checkDialectAuthoring(context.Background(), []byte(document), q.Pack)
		if err != nil || report.accepted(true) || len(report.Diagnostics) == 0 {
			t.Fatalf("refusal lost: %v %s", err, raw)
		}
	}
	// Acknowledged omissions require review; uncovered inputs are not valid.
	for _, omitted := range []bool{false, true} {
		doc := `{"version":2,"case":{"facts":[],"evidence":[]}`
		if omitted {
			doc += `,"unmapped":["/company/country"],"unmappedEvidence":["license"]`
		}
		doc += `}`
		raw, report, err := s.checkDialectAuthoring(context.Background(), []byte(doc), q.Pack)
		if err != nil || report.accepted(true) != omitted {
			t.Fatalf("coverage: %v %s", err, raw)
		}
	}
	// No source acquisition or workspace file is needed for these declarations.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err = s.checkDialectAuthoring(ctx, mapping, q.Pack); err == nil {
		t.Fatal("cancelled check accepted")
	}
	if _, _, err = s.checkDialectAuthoring(context.Background(), []byte(strings.Repeat(" ", dialectDocumentLimit+1)), q.Pack); err == nil {
		t.Fatal("oversized input accepted")
	}
	s.cfg.JpackBin = "/unavailable/runtime"
	if _, _, err = s.checkDialectAuthoring(context.Background(), mapping, q.Pack); err == nil {
		t.Fatal("missing Runtime accepted")
	}
}
func TestDialectAuthoringEndpointAndPersistence(t *testing.T) {
	requireBinary(t)
	s, _ := newTestServer(t, false)
	q := qualificationFixture()
	mapping, _ := json.Marshal(q.Mapping)
	call := func(path string, body any, auth bool) (int, []byte) {
		data, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", path, strings.NewReader(string(data)))
		if auth {
			r.Header.Set("Authorization", "Bearer "+testToken)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code, w.Body.Bytes()
	}
	code, _ := call("/api/dialect-authoring/check", map[string]any{"document": string(mapping), "pack": q.Pack}, false)
	if code == 200 {
		t.Fatal("unauthenticated check admitted")
	}
	code, raw := call("/api/dialect-authoring/check", map[string]any{"document": string(mapping), "pack": q.Pack}, true)
	var report dialectAuthoringReport
	if code != 200 || json.Unmarshal(raw, &report) != nil || !report.accepted(true) {
		t.Fatalf("%d %s", code, raw)
	}
	// Manual/API writes cannot bypass the same coverage failure shown to chat.
	q.Mapping.Case.Facts = nil
	code, raw = call("/api/input-preparations", map[string]any{"name": "Incomplete", "recipe": inputRecipe{Qualification: q}}, true)
	if code != 422 {
		t.Fatalf("unchecked save: %d %s", code, raw)
	}
	q = qualificationFixture()
	code, raw = call("/api/input-preparations", map[string]any{"name": "Checked", "recipe": inputRecipe{Qualification: q}}, true)
	var saved inputPreparationReply
	if code != 201 || json.Unmarshal(raw, &saved) != nil {
		t.Fatalf("checked save: %d %s", code, raw)
	}
	s.cfg.JpackBin = "/unavailable/runtime"
	loaded, err := s.preparationRepository().Get(context.Background(), saved.Document.ID)
	if err != nil || loaded.SHA256 != saved.SHA256 {
		t.Fatal("Runtime outage hid saved data", err)
	}
	// Draft autosave does not invoke Runtime; publication does.
	d := dialectDraftDocument{Name: "Draft", Recipe: inputRecipe{Qualification: q}}
	if _, err = s.publishDialectDraft(context.Background(), d); err == nil {
		t.Fatal("unchecked draft published")
	}
}
func TestDialectAuthoringLargeBoundedReport(t *testing.T) {
	bin := requireBinary(t)
	s := &Server{cfg: Config{JpackBin: bin}}
	// Many declared paths create a legitimate report larger than the old 64 KiB
	// subprocess limit. Strings remain exact across the snapshot transport.
	facts := []map[string]string{}
	for i := 0; i < 100; i++ {
		path := "/" + strings.Repeat("x", 600) + strconv.Itoa(i)
		facts = append(facts, map[string]string{"target": path, "source": path})
	}
	doc, _ := json.Marshal(map[string]any{"version": 2, "case": map[string]any{"facts": facts, "evidence": []any{}}})
	// Stay within Runtime's document bound while exceeding its old transport cap.
	raw, report, err := s.checkDialectAuthoring(context.Background(), doc, "")
	if err != nil || !report.accepted(false) || len(raw) <= runtimeAnswerLimit {
		t.Fatalf("large check: %v %d %s", err, len(raw), string(raw))
	}

}

func TestDialectAuthoringCompletedProposalReplayDoesNotRecheck(t *testing.T) {
	requireBinary(t)
	s, _ := newTestServer(t, false)
	body, _ := json.Marshal(map[string]any{"name": "Reviewed proposal", "recipe": inputRecipe{Qualification: qualificationFixture()}, "chatOrigin": inputChatOrigin{ChatID: "chat", DraftID: "proposal"}})
	call := func() (int, []byte) {
		r := httptest.NewRequest("POST", "/api/input-preparations", bytes.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+testToken)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code, w.Body.Bytes()
	}
	code, saved := call()
	if code != 201 {
		t.Fatalf("%d %s", code, saved)
	}
	s.cfg.JpackBin = "/unavailable/runtime"
	code, replayed := call()
	if code != 200 || !bytes.Equal(saved, replayed) {
		t.Fatalf("completed operation was rechecked: %d %s", code, replayed)
	}
}
func TestDialectAuthoringConvertedMappingRetainsExactCase(t *testing.T) {
	bin := requireBinary(t)
	s := &Server{cfg: Config{JpackBin: bin}}
	q := qualificationFixture()
	mapping, _ := json.Marshal(q.Mapping)
	caseData := `{"count":9007199254740993,"zero":-0,"missing":null,"flag":false}`
	rows := []inputPreparedRow{{Input: json.RawMessage(`{"source":{"mapping":` + string(mapping) + `,"case":` + caseData + `,"sources":{}}}`)}}
	if err := s.checkPreparedMappings(context.Background(), inputPreparation{Recipe: inputRecipe{Qualification: q}}, rows); err != nil {
		t.Fatal(err)
	}
	var input struct {
		Source struct {
			Mapping json.RawMessage `json:"mapping"`
			Digest  string          `json:"mappingDigest"`
			Case    json.RawMessage `json:"case"`
		} `json:"source"`
	}
	if json.Unmarshal(rows[0].Input, &input) != nil || string(input.Source.Case) != caseData || input.Source.Digest != "sha256:"+digestOf(input.Source.Mapping) {
		t.Fatalf("case or mapping identity changed: %s", rows[0].Input)
	}
}

func TestDialectAuthoringRejectsBrokenRuntimeAndCleansSnapshots(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX process fault fixture")
	}
	contract := `{"outputVersion":"2","contractVersion":"1","command":"dialect contract","status":"available","capabilities":{"validationVersions":[2,3],"packCheckVersions":[2,3]}}`
	doc := `{"version":2,"case":{"facts":[],"evidence":[]}}`
	good := map[string]any{"outputVersion": "2", "contractVersion": "1", "command": "dialect validate", "status": "document-valid", "dialectVersion": 2, "document": doc, "digest": "sha256:" + digestOf([]byte(doc)), "diagnostics": []any{}}
	quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\\''") + "'" }
	for _, scenario := range []string{"wrong-contract", "unsupported-version", "wrong-command", "wrong-digest", "different-document", "successful-output-failed-process", "overflow", "interrupted"} {
		t.Run(scenario, func(t *testing.T) {
			folder := t.TempDir()
			bin := filepath.Join(folder, "runtime")
			scratch := t.TempDir()
			t.Setenv("TMPDIR", scratch)
			report := map[string]any{}
			for k, v := range good {
				report[k] = v
			}
			declared := contract
			exitCode := "0"
			extra := ""
			switch scenario {
			case "wrong-contract":
				declared = strings.Replace(contract, `"contractVersion":"1"`, `"contractVersion":"99"`, 1)
			case "unsupported-version":
				declared = strings.ReplaceAll(contract, `[2,3]`, `[3]`)
			case "wrong-command":
				report["command"] = "other"
			case "wrong-digest":
				report["digest"] = "sha256:forged"
			case "different-document":
				other := strings.Replace(doc, `"version":2`, `"version":3`, 1)
				report["document"] = other
				report["digest"] = "sha256:" + digestOf([]byte(other))
			case "successful-output-failed-process":
				exitCode = "1"
			case "overflow":
				extra = "head -c 4194305 /dev/zero\n"
			case "interrupted":
				extra = "sleep 2\n"
			}
			encoded, _ := json.Marshal(report)
			script := "#!/bin/sh\nif [ \"$2\" = contract ]; then printf '%s' " + quote(declared) + "; exit 0; fi\n" + extra + "printf '%s' " + quote(string(encoded)) + "\nexit " + exitCode + "\n"
			if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if scenario == "interrupted" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
				defer cancel()
			}
			s := &Server{cfg: Config{JpackBin: bin}}
			if _, _, err := s.checkDialectAuthoring(ctx, []byte(doc), ""); err == nil {
				t.Fatal("broken Runtime accepted")
			}
			entries, err := os.ReadDir(scratch)
			if err != nil || len(entries) != 0 {
				t.Fatal("private snapshot left behind", entries, err)
			}
		})
	}
}
