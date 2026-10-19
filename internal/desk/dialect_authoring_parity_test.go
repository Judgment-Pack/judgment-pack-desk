package desk

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// These same fixtures drive the real manual and chat clients in the web test.
// Reports are recorded from the pinned Runtime, then verified here against the
// actual binary, including diagnostic locations and the exact pack byte digest.
func TestDialectAuthoringNonGTMParity(t *testing.T) {
	requireBinary(t)
	s, _ := newTestServer(t, false)
	raw, err := os.ReadFile("../../web/src/dialect/testdata/authoring-parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Pack      string
		Mapping, Report json.RawMessage
	}
	if err = json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			for _, spaced := range []bool{false, true} {
				document := fixture.Mapping
				if spaced {
					var value any
					json.Unmarshal(document, &value)
					document, _ = json.MarshalIndent(value, "", "  ")
				}
				body, _ := json.Marshal(map[string]string{"document": string(document), "pack": fixture.Pack})
				r := httptest.NewRequest("POST", "/api/dialect-authoring/check", strings.NewReader(string(body)))
				r.Header.Set("Authorization", "Bearer "+testToken)
				w := httptest.NewRecorder()
				s.ServeHTTP(w, r)
				if w.Code != 200 {
					t.Fatalf("%d %s", w.Code, w.Body.String())
				}
				var actual, expected any
				if json.Unmarshal(w.Body.Bytes(), &actual) != nil || json.Unmarshal(fixture.Report, &expected) != nil {
					t.Fatal("invalid authoring report")
				}
				// Build provenance is recorded in the fixture; a compatible
				// component update need not re-record identical semantics.
				normalizeBuild := func(value any) {
					root := value.(map[string]any)
					for _, scope := range []map[string]any{root, func() map[string]any { item, _ := root["packValidation"].(map[string]any); return item }()} {
						if tool, ok := scope["tool"].(map[string]any); ok {
							delete(tool, "version")
						}
					}
				}
				normalizeBuild(actual)
				normalizeBuild(expected)
				if !sameEvaluationValue(actual, expected) {
					t.Fatalf("authoring report changed:\n%s\nwant:\n%s", w.Body.String(), fixture.Report)
				}
			}
		})
	}
}
