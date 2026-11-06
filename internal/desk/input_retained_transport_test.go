package desk

import (
	"bytes"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"
)

// Independently encoded from the handwritten selector literal, not captured
// from the transport helper under test. Canonicality is base64url encoding;
// JSON object member order itself is not a capability identity requirement.
const retainedD2HeaderVector = "eyJjb250cmFjdCI6InJldGFpbmVkLWNvbHVtbi8xIiwic3BlYyI6eyJ2ZXJzaW9uIjoiMC4zLjAtcmMuMiIsInJldmlzaW9uIjoiYmNhYzE1Yjc1MzdjZTIxODc5MjAxYjVlNWRmZTc5NTQ5NjIyNTEyMSIsInNjaGVtYVNoYTI1NiI6InNoYTI1NjowY2I0OWYyMzhjNzgzOTFjYjE2M2E2NTQ3NGNjOWFmOTYyZGYyYmIxMDI0YTU1YzUzMTBjMmI3ODRiMmYwNTU3IiwiY29ycHVzU2hhMjU2Ijoic2hhMjU2OjE3YjE2ZmY3YTliNzRmZTRjYzgxZWI2ZjYxMjlhNmFhYmRkOTgzNDYyMWQ5ZTc1NmFhMmQ0YzFhM2YyNDU4NjMifSwicHJvZmlsZSI6ImJhc2UiLCJyb2xlcyI6WyJkb2N1bWVudC12YWxpZGF0aW9uIiwicHJldmlldyIsInByZXBhcmF0aW9uIl19"

func retainedD2Admission(t *testing.T, generated bool) *retainedAdmission {
	t.Helper()
	selection := retainedD2Selection(t, generated)
	admitted, err := negotiateRetainedRoles(selection, selection.Roles, generated, retainedD2Reports(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	return admitted
}
func TestRetainedD2CanonicalHeaderAndSupportedRoutes(t *testing.T) {
	admission := retainedD2Admission(t, false)
	encoded, err := retainedSelectorHeader(admission.selection)
	if err != nil || encoded != retainedD2HeaderVector {
		t.Fatalf("header differs from independent vector: %s %v", encoded, err)
	}
	for _, path := range []string{"/v1/inputs/next", "/v1/inputs/preview", "/v1/previews", "/v1/jobs/job_example/runs"} {
		selected, header, err := retainedRunnerTransport(http.MethodPost, path, admission, false)
		if err != nil || selected != retainedRunnerPrefix+strings.TrimPrefix(path, "/v1/") || header != retainedD2HeaderVector {
			t.Fatalf("supported route %s: %s %v", path, selected, err)
		}
		parsed, err := readRetainedRunnerSelection(http.MethodPost, selected, http.Header{retainedSelectorHeaderName: {header}}, false)
		if err != nil || parsed == nil || parsed.Spec != admission.selection.Spec {
			t.Fatalf("selected header parse: %v", err)
		}
	}
	verifier := retainedInstalledSelector("preparation-verification")
	verified, err := negotiateRetainedRoles(verifier, verifier.Roles, false, retainedD2Reports(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	path, header, err := retainedRunnerTransport(http.MethodGet, "/v1/runs/run_example/verification", verified, false)
	if err != nil || path != "/v1/retained-column/1/runs/run_example/verification" || header == "" {
		t.Fatalf("offline-verification route: %s %v", path, err)
	}
	if limit, err := retainedArtifactLimit(verified, "exportWireBytes"); err != nil || limit != 90177536 {
		t.Fatalf("selected export cap: %d %v", limit, err)
	}
}

func TestRetainedD2UnsupportedAndOrdinaryRoutes(t *testing.T) {
	admission := retainedD2Admission(t, false)
	for _, row := range []struct{ method, path string }{{"GET", "/v1/status"}, {"GET", "/v1/runs/run_example"}, {"GET", "/v1/input-profiles"}, {"POST", "/v1/jobs"}, {"POST", "/v1/previews/"}, {"POST", "/v1/jobs//runs"}, {"POST", "/v1/retained-column/1/previews"}, {"POST", "/v1/previews?extra=1"}, {"POST", "/v1/jobs/a%2Fb/runs"}, {"DELETE", "/v1/previews"}} {
		_, _, err := retainedRunnerTransport(row.method, row.path, admission, false)
		retainedD2ExpectCode(t, err, "capability-unsupported")
		ordinary, header, err := retainedRunnerTransport(row.method, row.path, nil, false)
		if err != nil || ordinary != row.path || header != "" {
			t.Fatal("unselected ordinary route changed")
		}
	}
	if selected, err := readRetainedRunnerSelection("GET", "/v1/status", nil, false); err != nil || selected != nil {
		t.Fatal("ordinary read unexpectedly selected")
	}
	_, err := readRetainedRunnerSelection("GET", "/v1/status", http.Header{retainedSelectorHeaderName: {retainedD2HeaderVector}}, false)
	retainedD2ExpectCode(t, err, "capability-unsupported")
	_, err = readRetainedRunnerSelection("GET", retainedRunnerPrefix+"runs/run_example", http.Header{retainedSelectorHeaderName: {retainedD2HeaderVector}}, false)
	retainedD2ExpectCode(t, err, "capability-unsupported")
	// Existing global constants stay independently owned, with their exact old
	// values. No selected helper changes the ordinary 2MiB request read/write.
	if dialectDocumentLimit != 128<<10 || runnerAnswerLimit != 16<<20 || runnerExportLimit <= 8<<20 {
		t.Fatal("ordinary global caps changed")
	}
}

func TestRetainedD2HeaderRefusalsBeforeRead(t *testing.T) {
	path := retainedRunnerPrefix + "inputs/next"
	for name, header := range map[string]http.Header{
		"missing":           {},
		"duplicate":         {retainedSelectorHeaderName: {retainedD2HeaderVector, retainedD2HeaderVector}},
		"case-duplicate":    {retainedSelectorHeaderName: {retainedD2HeaderVector}, "jpack-conversion-contract": {retainedD2HeaderVector}},
		"padded":            {retainedSelectorHeaderName: {retainedD2HeaderVector + "="}},
		"newline":           {retainedSelectorHeaderName: {retainedD2HeaderVector + "\n"}},
		"empty":             {retainedSelectorHeaderName: {""}},
		"over-bound":        {retainedSelectorHeaderName: {strings.Repeat("x", 4097)}},
		"standard-alphabet": {retainedSelectorHeaderName: {"++++////"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := readRetainedRunnerSelection("POST", path, header, false)
			retainedD2ExpectCode(t, err, "input-malformed")
		})
	}
	for name, raw := range map[string]string{
		"tiny-missing-role": strings.Replace(retainedD2SelectorJSON, `,"preparation"`, "", 1),
		"wrong-pin":         strings.Replace(retainedD2SelectorJSON, "0.3.0-rc.2", "0.3.0-rc.1", 1),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := readRetainedRunnerSelection("POST", path, http.Header{retainedSelectorHeaderName: {base64.RawURLEncoding.EncodeToString([]byte(raw))}}, false)
			retainedD2ExpectCode(t, err, "capability-unsupported")
		})
	}
	// A selected generated operation cannot reuse an ordinary negotiation with a
	// producer-carrier role present but its required grammar checks skipped.
	selection := retainedD2Selection(t, true)
	ordinary, err := negotiateRetainedRoles(selection, selection.Roles, false, retainedD2Reports(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = retainedRunnerTransport("POST", "/v1/inputs/next", ordinary, true)
	retainedD2ExpectCode(t, err, "capability-unsupported")
	generated := retainedD2Admission(t, true)
	selected, header, err := retainedRunnerTransport("POST", "/v1/inputs/next", generated, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readRetainedRunnerSelection("POST", selected, http.Header{retainedSelectorHeaderName: {header}}, true); err != nil {
		t.Fatal(err)
	}
}

type retainedD2CountingReader struct {
	reader       io.Reader
	calls, bytes int
}

func (r *retainedD2CountingReader) Read(p []byte) (int, error) {
	r.calls++
	n, err := r.reader.Read(p)
	r.bytes += n
	return n, err
}
func TestRetainedD2CompleteArtifactLowerLimitAndOneOver(t *testing.T) {
	selection := retainedD2Selection(t, false)
	reports := retainedD2Reports(t)
	reports.Runner[0].Limits["requestBodyBytes"] = 64
	admission, err := negotiateRetainedRoles(selection, selection.Roles, false, reports, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Valid JSON bytes exercise the actual negotiated byte guard; this is not a
	// maximum production/request-envelope witness or a source acquisition test.
	atLimit := append([]byte(`[]`), bytes.Repeat([]byte(" "), 62)...)
	if err := checkRetainedArtifact(admission, "requestBodyBytes", atLimit); err != nil {
		t.Fatal(err)
	}
	reader := &retainedD2CountingReader{reader: bytes.NewReader(atLimit)}
	got, err := readRetainedArtifact(reader, admission, "requestBodyBytes")
	if err != nil || !bytes.Equal(got, atLimit) {
		t.Fatalf("exact effective limit refused: %v", err)
	}
	over := append(append([]byte{}, atLimit...), ' ')
	retainedD2ExpectCode(t, checkRetainedArtifact(admission, "requestBodyBytes", over), "input-over-bound")
	reader = &retainedD2CountingReader{reader: strings.NewReader(string(over) + strings.Repeat(" ", 1000))}
	got, err = readRetainedArtifact(reader, admission, "requestBodyBytes")
	retainedD2ExpectCode(t, err, "input-over-bound")
	if got != nil || reader.bytes != 65 {
		t.Fatalf("reader clipped or read beyond limit+1: %d bytes", reader.bytes)
	}
	for name, admitted := range map[string]*retainedAdmission{"nil": nil, "unknown-role": {selection: selection, limits: map[string]map[string]int64{"execute": {"requestBodyBytes": 100}}}, "unsafe-cap": {selection: selection, limits: map[string]map[string]int64{"preview": {"requestBodyBytes": 9007199254740991}}}} {
		t.Run(name, func(t *testing.T) {
			reader := &retainedD2CountingReader{reader: bytes.NewReader([]byte(`{}`))}
			_, err := readRetainedArtifact(reader, admitted, "requestBodyBytes")
			if err == nil || reader.calls != 0 {
				t.Fatal("unadmitted limit reached the reader")
			}
		})
	}
	_, err = retainedArtifactLimit(admission, "arbitraryBytes")
	retainedD2ExpectCode(t, err, "capability-unsupported")
}

func TestRetainedD2RuntimeSelectionOperationScope(t *testing.T) {
	for _, operation := range []string{"contract", "canonicalize", "digest", "validate", "check"} {
		selectors, err := retainedRuntimeSelectors(operation)
		if err != nil || len(selectors) != 2 || selectors["draftContract"] != "retained-column/1@bcac15b7537ce21879201b5e5dfe795496225121" || selectors["draftProfile"] != "base" {
			t.Fatalf("Runtime operation %s: %#v %v", operation, selectors, err)
		}
	}
	for _, operation := range []string{"init", "schema", "reference", "evaluate", ""} {
		_, err := retainedRuntimeSelectors(operation)
		retainedD2ExpectCode(t, err, "capability-unsupported")
	}
}
