package desk

// The decision-record panel (ADR-0010, sections 4, 6 and 8). A stand-in
// runtime, by absolute path, answers `packs schema` and `audit verify` from
// files each test prepares, so these run where no runtime is installed. Its
// reports are runtime 0.26.0's own, measured, with the trail's path
// shortened. The last test drives the real runtime and skips without one.

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// withAuditVersions is what a runtime with the audit commands names as
// `supportedConfigVersions`: 0.26.0 reads "1" to "6".
const withAuditVersions = `["1","2","3","4","5","6"]`

// What runtime 0.26.0 printed for `audit verify --config jpack.json --format
// json` over a trail of three deciding runs; after a torn last line was
// appended (exit 1); and after `audit repair` and one more run.
const (
	auditValidReport     = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.26.0"},"command":"audit verify","trailPath":"/project/.desk-private/audit/evaluations.jsonl","snapshotBetweenWrites":true,"status":"valid","scope":"one-supplied-chain","lines":3,"bytes":2856,"trail":"9a5ef41d74e7d7e003c8a34cff056351","head":{"checkpointVersion":"1","recordDigest":"sha256:a3c5701c62be479d03ebdd9b08a56d1195a272f203dd84053ae24f1daa8319cf","sequence":3,"trail":"9a5ef41d74e7d7e003c8a34cff056351"},"coverage":{"legacyPrefix":0,"chained":3,"unchained":0,"uncovered":0,"damaged":0,"signed":{"status":"not-checked","detail":"no public key was supplied"},"signedRecords":0,"unsignedRecords":0,"checkpointed":{"status":"not-supplied"},"witnessed":0,"unwitnessed":3,"stamped":{"status":"not-checked","detail":"no time-stamping roots were supplied"}},"segments":[{"firstLine":1,"lastLine":3}],"segmentsTotal":1,"discontinuities":[],"discontinuitiesTotal":0,"findings":[],"findingsTotal":0,"establishes":["The chained lines are consistent with one another: no line before the last was edited, inserted, deleted or moved without breaking a link, and the lines before the first chained line are the block its previous commits to."],"doesNotEstablish":["The last line, and any lines rewritten from some point on with their links recomputed, are not authenticated by the chain: only a checkpoint covering them, held independently of the operator, shows they are the ones first written.","That the trail is complete: a trail cut short is as consistent as the whole one, and nothing here says which decisions were never written to it.","That any record's at is true: it is the operator's clock.","Who wrote any record: no public key was supplied, so no signature was checked.","When any checkpoint existed: no time-stamping roots were supplied, so no stamp was checked."]}`
	auditInvalidReport   = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.26.0"},"command":"audit verify","trailPath":"/project/.desk-private/audit/evaluations.jsonl","snapshotBetweenWrites":true,"status":"invalid","scope":"one-supplied-chain","lines":3,"bytes":2864,"trail":"9a5ef41d74e7d7e003c8a34cff056351","head":{"checkpointVersion":"1","recordDigest":"sha256:a3c5701c62be479d03ebdd9b08a56d1195a272f203dd84053ae24f1daa8319cf","sequence":3,"trail":"9a5ef41d74e7d7e003c8a34cff056351"},"coverage":{"legacyPrefix":0,"chained":3,"unchained":0,"uncovered":0,"damaged":0,"signed":{"status":"not-checked","detail":"no public key was supplied"},"signedRecords":0,"unsignedRecords":0,"checkpointed":{"status":"not-supplied"},"witnessed":0,"unwitnessed":3,"stamped":{"status":"not-checked","detail":"no time-stamping roots were supplied"}},"segments":[{"firstLine":1,"lastLine":3}],"segmentsTotal":1,"discontinuities":[],"discontinuitiesTotal":0,"findings":[{"name":"incomplete-last-line","line":4,"detail":"the trail ends in 8 bytes with no newline: a write that did not complete"}],"findingsTotal":1,"establishes":[],"doesNotEstablish":["The last line, and any lines rewritten from some point on with their links recomputed, are not authenticated by the chain: only a checkpoint covering them, held independently of the operator, shows they are the ones first written.","That the trail is complete: a trail cut short is as consistent as the whole one, and nothing here says which decisions were never written to it.","That any record's at is true: it is the operator's clock.","Who wrote any record: no public key was supplied, so no signature was checked.","When any checkpoint existed: no time-stamping roots were supplied, so no stamp was checked."]}`
	auditSegmentedReport = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.26.0"},"command":"audit verify","trailPath":"/project/.desk-private/audit/evaluations.jsonl","snapshotBetweenWrites":true,"status":"segmented","scope":"one-supplied-chain","lines":6,"bytes":4369,"trail":"9a5ef41d74e7d7e003c8a34cff056351","head":{"checkpointVersion":"1","recordDigest":"sha256:f630f3571686f217871788345972f5621ba674410b8b646213ec9881149b517f","sequence":6,"trail":"9a5ef41d74e7d7e003c8a34cff056351"},"coverage":{"legacyPrefix":0,"chained":5,"unchained":0,"uncovered":0,"damaged":1,"signed":{"status":"not-checked","detail":"no public key was supplied"},"signedRecords":0,"unsignedRecords":0,"checkpointed":{"status":"not-supplied"},"witnessed":0,"unwitnessed":5,"stamped":{"status":"not-checked","detail":"no time-stamping roots were supplied"}},"segments":[{"firstLine":1,"lastLine":3},{"firstLine":5,"lastLine":6}],"segmentsTotal":2,"discontinuities":[{"line":5,"reason":"incomplete-last-line","damagedLine":4,"bytes":8,"digest":"sha256:952cdc0f85ab10d18a1bdccfeb6c3991e59ab424dc2ce916544aac44f3d8b45e"}],"discontinuitiesTotal":1,"findings":[],"findingsTotal":0,"establishes":["The chained lines are consistent with one another: no line before the last was edited, inserted, deleted or moved without breaking a link, and the lines before the first chained line are the block its previous commits to."],"doesNotEstablish":["The last line, and any lines rewritten from some point on with their links recomputed, are not authenticated by the chain: only a checkpoint covering them, held independently of the operator, shows they are the ones first written.","That the trail is complete: a trail cut short is as consistent as the whole one, and nothing here says which decisions were never written to it.","That the history is intact across a discontinuity: a repair keeps the damaged line in place and links over it, so what the damaged line held is not part of any segment.","That any record's at is true: it is the operator's clock.","Who wrote any record: no public key was supplied, so no signature was checked.","When any checkpoint existed: no time-stamping roots were supplied, so no stamp was checked."]}`
)

// What runtime 0.25.0 printed for `audit verify --config jpack.json --format
// json`, exit 3, measured: it has no audit command, so its root command
// answers for the first flag it does not know. Without the flags it says
// "unknown command".
const (
	auditUnknownFlag    = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.25.0"},"command":"jpack","status":"error","diagnostics":[{"code":"JPS-INVOCATION-ARGUMENTS","codeStability":"provisional","layer":"operation","severity":"error","instancePath":"","message":"unknown flag: --config"}]}`
	auditUnknownCommand = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.25.0"},"command":"jpack","status":"error","diagnostics":[{"code":"JPS-INVOCATION-ARGUMENTS","codeStability":"provisional","layer":"operation","severity":"error","instancePath":"","message":"unknown command \"audit\" for \"jpack\""}]}`
	// auditNoTrailYet is 0.26.0's answer, exit 4, where no record has been
	// written yet.
	auditNoTrailYet = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.26.0"},"command":"audit verify","status":"error","diagnostics":[{"code":"JPS-AUDIT-TRAIL-READ","codeStability":"provisional","layer":"operation","severity":"error","instancePath":"","message":"The project's trail /project/.desk-private/audit/evaluations.jsonl does not exist yet: no record has been written."}]}`
)

// auditedConfig is a project that keeps a trail, as a desk Desk made does.
const auditedConfig = `{"configVersion":"5","audit":{"dir":".desk-private/audit"},"packs":{}}` + "\n"

// auditRig is a stand-in runtime for the panel, and the file that steers its
// `audit verify`.
type auditRig struct {
	bin, calls, answer string
}

// newAuditRig writes the stand-in. Its `packs schema` names versions; its
// `audit verify` prints what `answers` prepared and exits with its code. Every
// run appends its arguments and environment as `writeStandInRuntime` says.
func newAuditRig(t *testing.T, versions string) *auditRig {
	t.Helper()
	dir := t.TempDir()
	rig := &auditRig{bin: filepath.Join(dir, "jpack"), answer: filepath.Join(dir, "verify.json")}
	rig.calls = writeStandInRuntime(t, rig.bin, reading(versions), lockingAs(wantGatedConfig))
	script, err := os.ReadFile(rig.bin)
	if err != nil {
		t.Fatal(err)
	}
	verify := "'audit verify')\n  while IFS= read -r line || [ -n \"$line\" ]; do printf '%s\\n' \"$line\"; done < '" + rig.answer + "'\n" +
		"  IFS= read -r code < '" + rig.answer + ".exit'\n  exit \"$code\"\n  ;;\n"
	script = bytes.Replace(script, []byte("'packs lock')\n"), []byte(verify+"'packs lock')\n"), 1)
	if err := os.WriteFile(rig.bin, script, 0o755); err != nil {
		t.Fatal(err)
	}
	return rig
}

// answers sets what the stand-in's `audit verify` prints, and its exit.
func (rig *auditRig) answers(t *testing.T, code int, body string) {
	t.Helper()
	if os.WriteFile(rig.answer, []byte(body), 0o600) != nil || os.WriteFile(rig.answer+".exit", []byte(strconv.Itoa(code)+"\n"), 0o600) != nil {
		t.Fatal("could not prepare the stand-in's answer")
	}
}

// ran is what the stand-in ran since the last call, then forgets it.
func (rig *auditRig) ran(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(rig.calls)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	os.Remove(rig.calls)
	os.Remove(rig.calls + ".env")
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

// auditDesk is a startup desk over a project with config as its jpack.json,
// and the stand-in behind it.
func auditDesk(t *testing.T, versions, config string) (*httptest.Server, *auditRig, string) {
	t.Helper()
	rig := newAuditRig(t, versions)
	project := t.TempDir()
	if config != "" {
		writeProject(t, project, map[string]string{"jpack.json": config})
	}
	s, ts := startDesk(t, Config{ProjectDir: project, JpackBin: rig.bin, Token: testToken, Logger: log.New(io.Discard, "", 0)})
	t.Cleanup(func() { s.Close() })
	t.Cleanup(ts.Close)
	return ts, rig, project
}

// readAudit asks a desk for its decision record.
func readAudit(t *testing.T, ts *httptest.Server, desk string) (int, auditAnswer, string) {
	t.Helper()
	status, data := reviewCall(t, ts, "GET", "/api/audit/verify", desk, nil, bearer)
	var answer auditAnswer
	if status == http.StatusOK && json.Unmarshal(data, &answer) != nil {
		t.Fatalf("the decision record answered %d %s", status, data)
	}
	var refusal struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(data, &refusal)
	return status, answer, refusal.Error
}

// reportOf is the panel's report on desk, or a failure that says what came
// instead.
func reportOf(t *testing.T, ts *httptest.Server, desk string) *auditReport {
	t.Helper()
	status, answer, refusal := readAudit(t, ts, desk)
	if status != http.StatusOK || answer.State != auditStateReport || answer.Report == nil {
		t.Fatalf("the decision record answered %d %+v %q, want a report", status, answer, refusal)
	}
	return answer.Report
}

const (
	schemaCall = "packs schema --format json [JPACK_CONFIG=unset]"
	verifyCall = "audit verify --config jpack.json --format json [JPACK_CONFIG=unset]"
)

// **The runtime's own audit verify, with no held input.** On the startup desk
// and on a desk Desk made, the panel asks `packs schema`, then runs exactly
// `audit verify --config jpack.json --format json`: no key, no checkpoint, no
// stamping roots, and no `--require-…` flag. What it shows is the runtime's
// report: its status, coverage, segments and sentences, as printed.
func TestTheDecisionRecordRunsAuditVerifyWithNoHeldInput(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	ts, rig, _ := auditDesk(t, withAuditVersions, auditedConfig)
	rig.answers(t, 0, auditValidReport)
	report := reportOf(t, ts, "")
	if calls := rig.ran(t); !slices.Equal(calls, []string{schemaCall, verifyCall}) {
		t.Errorf("the panel ran %q, want packs schema and then audit verify with no held input", calls)
	}
	var want struct {
		Establishes      []string      `json:"establishes"`
		DoesNotEstablish []string      `json:"doesNotEstablish"`
		Coverage         auditCoverage `json:"coverage"`
	}
	if err := json.Unmarshal([]byte(auditValidReport), &want); err != nil {
		t.Fatal(err)
	}
	if report.Status != "valid" || report.Lines != 3 || report.Bytes != 2856 || !report.SnapshotBetweenWrites ||
		report.Coverage != want.Coverage || report.Coverage.Chained != 3 || report.Coverage.Unwitnessed != 3 ||
		report.Coverage.Signed != (auditCoverageState{Status: "not-checked", Detail: "no public key was supplied"}) ||
		report.Coverage.Checkpointed.Status != "not-supplied" || report.Coverage.Stamped.Status != "not-checked" ||
		!slices.Equal(report.Segments, []auditSegment{{1, 3}}) || report.SegmentsTotal != 1 ||
		len(report.Discontinuities) != 0 || len(report.Findings) != 0 || report.FindingsTotal != 0 {
		t.Errorf("the panel shows %+v", report)
	}
	if !slices.Equal(report.Establishes, want.Establishes) || !slices.Equal(report.DoesNotEstablish, want.DoesNotEstablish) || len(report.DoesNotEstablish) != 5 {
		t.Errorf("the runtime's sentences were not passed through as written: %q / %q", report.Establishes, report.DoesNotEstablish)
	}

	// A desk Desk made: its own project, the same command.
	_, ts2, _ := gatesServer(t, rig.bin)
	row := createGatedDesk(t, ts2)
	rig.ran(t)
	rig.answers(t, 1, auditInvalidReport)
	if report := reportOf(t, ts2, row.ID); report.Status != "invalid" {
		t.Errorf("the made desk's panel shows %+v", report)
	}
	if calls := rig.ran(t); !slices.Equal(calls, []string{schemaCall, verifyCall}) {
		t.Errorf("the made desk's panel ran %q", calls)
	}
}

// **Exit 1 is a failed check, and its report is read.** The torn trail's
// finding is shown by name, with the runtime's own detail; a segmented trail
// shows its segments and the discontinuity between them.
func TestAFailedCheckIsReadAsAReport(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	ts, rig, _ := auditDesk(t, withAuditVersions, auditedConfig)
	rig.answers(t, 1, auditInvalidReport)
	report := reportOf(t, ts, "")
	want := []auditFinding{{Name: "incomplete-last-line", Line: 4, Detail: "the trail ends in 8 bytes with no newline: a write that did not complete"}}
	if report.Status != "invalid" || !slices.Equal(report.Findings, want) || report.FindingsTotal != 1 || len(report.Establishes) != 0 || len(report.DoesNotEstablish) != 5 {
		t.Errorf("the failed check was shown as %+v", report)
	}

	rig.answers(t, 0, auditSegmentedReport)
	report = reportOf(t, ts, "")
	if report.Status != "segmented" || !slices.Equal(report.Segments, []auditSegment{{1, 3}, {5, 6}}) || report.SegmentsTotal != 2 ||
		!slices.Equal(report.Discontinuities, []auditDiscontinuity{{Line: 5, Reason: "incomplete-last-line", DamagedLine: 4, Bytes: 8, Digest: "sha256:952cdc0f85ab10d18a1bdccfeb6c3991e59ab424dc2ce916544aac44f3d8b45e"}}) ||
		report.DiscontinuitiesTotal != 1 || report.Coverage.Damaged != 1 || report.Coverage.Chained != 5 {
		t.Errorf("the segmented trail was shown as %+v", report)
	}
}

// **What the runtime does not document is an error, said as one.** A report
// whose status disagrees with its exit, one missing a member a report has, an
// answer from another command, an error without diagnostics, output that is
// not JSON, and output past `runRuntime`'s bound are each refused, never shown
// as a report. A refusal the runtime explains in its diagnostics is shown as
// the runtime's.
func TestAnAnswerTheRuntimeDoesNotDocumentIsAnError(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	ts, rig, _ := auditDesk(t, withAuditVersions, auditedConfig)
	without := func(member string) string {
		var report map[string]any
		if err := json.Unmarshal([]byte(auditValidReport), &report); err != nil {
			t.Fatal(err)
		}
		delete(report, member)
		data, _ := json.Marshal(report)
		return string(data)
	}
	padded := strings.Replace(auditValidReport, `"trailPath":"`, `"trailPath":"`+strings.Repeat("x", runtimeAnswerLimit), 1)
	for _, tc := range []struct {
		name string
		code int
		body string
		says string
	}{
		{"not JSON", 0, "consistent: 3 line(s)", "did not answer as documented"},
		{"nothing at all, exit 1", 1, "", "did not answer as documented"},
		{"a valid report with exit 1", 1, auditValidReport, "did not answer as documented"},
		{"an invalid report with exit 0", 0, auditInvalidReport, "did not answer as documented"},
		{"a segmented report with exit 1", 1, auditSegmentedReport, "did not answer as documented"},
		{"another command's answer", 0, strings.Replace(auditValidReport, `"command":"audit verify"`, `"command":"packs verify"`, 1), "did not answer as documented"},
		{"a report with no coverage", 0, without("coverage"), "did not answer as documented"},
		{"a report with no segments", 0, without("segments"), "did not answer as documented"},
		{"a report with no discontinuities", 0, without("discontinuities"), "did not answer as documented"},
		{"a report with no findings", 0, without("findings"), "did not answer as documented"},
		{"a report with no establishes", 0, without("establishes"), "did not answer as documented"},
		{"a report with no doesNotEstablish", 0, without("doesNotEstablish"), "did not answer as documented"},
		{"a report with no snapshot", 0, without("snapshotBetweenWrites"), "did not answer as documented"},
		{"an error with no diagnostics", 4, `{"command":"audit verify","status":"error","diagnostics":[]}`, "did not answer as documented"},
		{"an error that exits 0", 0, auditNoTrailYet, "did not answer as documented"},
		{"an error that exits 1", 1, auditNoTrailYet, "did not answer as documented"},
		{"another command's refusal, exit 2", 2, auditUnknownFlag, "did not answer as documented"},
		{"a report past runRuntime's bound", 0, padded, "larger than 65536 bytes"},
	} {
		rig.answers(t, tc.code, tc.body)
		status, answer, refusal := readAudit(t, ts, "")
		if status != http.StatusInternalServerError || !strings.HasPrefix(refusal, "The decision record could not be checked: ") || !strings.Contains(refusal, tc.says) || answer.Report != nil {
			t.Errorf("%s: answered %d %+v %q, want an error saying %q", tc.name, status, answer, refusal, tc.says)
		}
	}

	rig.answers(t, 4, auditNoTrailYet)
	status, answer, refusal := readAudit(t, ts, "")
	want := []runtimeDiagnostic{{Code: "JPS-AUDIT-TRAIL-READ", Message: "The project's trail /project/.desk-private/audit/evaluations.jsonl does not exist yet: no record has been written."}}
	if status != http.StatusOK || answer.State != auditStateUnverified || !slices.Equal(answer.Diagnostics, want) || answer.Report != nil || answer.Runtime != "0.0.0-stand-in" {
		t.Errorf("the runtime's refusal answered %d %+v %q, want its diagnostics", status, answer, refusal)
	}
}

// **With a runtime that has no audit commands, one sentence and nothing
// more** (ADR-0010, section 6). A runtime that does not read "6" is asked
// nothing past `packs schema`. One that reads it, and still answers for its
// root command that it knows no `audit verify`, is read the same way, in
// either of runtime 0.25.0's words. An `audit verify` that refuses on its own
// account is not mistaken for one.
func TestTheDecisionRecordSaysWhenTheRuntimeHasNoAuditCommands(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	older := auditAnswer{State: auditStateOlder, Runtime: "0.0.0-stand-in", Floor: "0.26.0"}
	ts, rig, _ := auditDesk(t, allConfigVersions, auditedConfig)
	rig.answers(t, 0, auditValidReport)
	if status, answer, refusal := readAudit(t, ts, ""); status != http.StatusOK || !sameAuditAnswer(answer, older) {
		t.Errorf("a runtime that reads no 6 answered %d %+v %q", status, answer, refusal)
	}
	if calls := rig.ran(t); !slices.Equal(calls, []string{schemaCall}) {
		t.Errorf("with a runtime that reads no 6 the panel ran %q, want packs schema alone", calls)
	}

	ts, rig, _ = auditDesk(t, withAuditVersions, auditedConfig)
	for _, said := range []string{auditUnknownFlag, auditUnknownCommand} {
		rig.answers(t, 3, said)
		if status, answer, refusal := readAudit(t, ts, ""); status != http.StatusOK || !sameAuditAnswer(answer, older) {
			t.Errorf("a runtime with no audit verify (%s) answered %d %+v %q", said, status, answer, refusal)
		}
		if calls := rig.ran(t); !slices.Equal(calls, []string{schemaCall, verifyCall}) {
			t.Errorf("the panel ran %q", calls)
		}
	}

	notDeclared := `{"outputVersion":"2","tool":{"name":"jpack","version":"0.26.0"},"command":"audit verify","status":"error","diagnostics":[{"code":"JPS-AUDIT-NOT-DECLARED","message":"This project's jpack.json declares no audit directory, so it keeps no trail; pass --trail <file> to read a trail file."}]}`
	rig.answers(t, 3, notDeclared)
	if status, answer, _ := readAudit(t, ts, ""); status != http.StatusOK || answer.State != auditStateUnverified || len(answer.Diagnostics) != 1 || answer.Diagnostics[0].Code != "JPS-AUDIT-NOT-DECLARED" {
		t.Errorf("audit verify's own refusal, exit 3, answered %d %+v", status, answer)
	}
}

func sameAuditAnswer(a, b auditAnswer) bool {
	return a.State == b.State && a.Runtime == b.Runtime && a.Floor == b.Floor && a.Report == nil && b.Report == nil && len(a.Diagnostics) == 0 && len(b.Diagnostics) == 0
}

// **Unavailable where the review is** (ADR-0010, section 8). On the startup
// desk under a JPACK_CONFIG that names another project's configuration, the
// panel refuses, says why, and runs nothing; naming this project's own, it
// runs. A desk Desk made never reads JPACK_CONFIG, and runs without it.
func TestTheDecisionRecordIsUnavailableWhereTheReviewIs(t *testing.T) {
	ts, rig, project := auditDesk(t, withAuditVersions, auditedConfig)
	elsewhere := t.TempDir()
	writeProject(t, elsewhere, map[string]string{"jpack.json": auditedConfig})
	rig.answers(t, 0, auditValidReport)

	named := filepath.Join(elsewhere, "jpack.json")
	t.Setenv("JPACK_CONFIG", named)
	status, answer, refusal := readAudit(t, ts, "")
	want := "This project's runtime reads " + named + ", which JPACK_CONFIG names, and not this project's jpack.json, so Desk does not check its decision record here."
	if status != http.StatusConflict || refusal != want || answer.State != "" {
		t.Errorf("under another project's JPACK_CONFIG the panel answered %d %+v %q", status, answer, refusal)
	}
	if calls := rig.ran(t); len(calls) != 0 {
		t.Errorf("a refused panel ran %q", calls)
	}

	t.Setenv("JPACK_CONFIG", filepath.Join(project, "jpack.json"))
	reportOf(t, ts, "")
	if calls := rig.ran(t); !slices.Equal(calls, []string{schemaCall, verifyCall}) {
		t.Errorf("under this project's own JPACK_CONFIG the panel ran %q", calls)
	}

	t.Setenv("JPACK_CONFIG", named)
	_, ts2, _ := gatesServer(t, rig.bin)
	row := createGatedDesk(t, ts2)
	rig.ran(t)
	reportOf(t, ts2, row.ID)
	if calls := rig.ran(t); !slices.Equal(calls, []string{schemaCall, verifyCall}) {
		t.Errorf("a made desk under another project's JPACK_CONFIG ran %q, want both without it", calls)
	}
}

// **A project that keeps no trail: said, and nothing run.** A jpack.json
// with no audit member, or one with no directory in it, keeps no trail, and
// the panel asks the runtime nothing, not even `packs schema`. A jpack.json
// Desk cannot read, or read as a configuration, is an error.
func TestAProjectThatKeepsNoTrailRunsNothing(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	for _, config := range []string{
		`{"configVersion":"5","packs":{}}`,
		`{"configVersion":"5","audit":{},"packs":{}}`,
		`{"configVersion":"5","audit":{"dir":""},"packs":{}}`,
		`{"configVersion":"5","audit":null,"packs":{}}`,
	} {
		ts, rig, _ := auditDesk(t, withAuditVersions, config)
		rig.answers(t, 0, auditValidReport)
		if status, answer, refusal := readAudit(t, ts, ""); status != http.StatusOK || !sameAuditAnswer(answer, auditAnswer{State: auditStateNoTrail}) {
			t.Errorf("%s: answered %d %+v %q, want no trail", config, status, answer, refusal)
		}
		if calls := rig.ran(t); len(calls) != 0 {
			t.Errorf("%s: a project with no trail had the panel run %q", config, calls)
		}
	}
	for _, tc := range []struct{ config, says string }{
		{"", "jpack.json could not be read"},
		{`{"configVersion":"5","audit":{"dir":5}}`, "jpack.json is not a configuration Desk can read"},
		{`{"configVersion":"5",`, "jpack.json is not a configuration Desk can read"},
	} {
		ts, rig, _ := auditDesk(t, withAuditVersions, tc.config)
		rig.answers(t, 0, auditValidReport)
		if status, _, refusal := readAudit(t, ts, ""); status != http.StatusInternalServerError || !strings.Contains(refusal, tc.says) {
			t.Errorf("%q: answered %d %q, want an error saying %q", tc.config, status, refusal, tc.says)
		}
		if calls := rig.ran(t); len(calls) != 0 {
			t.Errorf("%q: the panel ran %q", tc.config, calls)
		}
	}
}

// **The panel's commands keep an inherited JPACK_SIGNING_KEY on the startup
// desk only** (ADR-0010, sections 1 and 8), as every other command Desk runs
// does: on a desk Desk made, `packs schema` and `audit verify` run without it,
// and with the rest of Desk's environment.
func TestThePanelsCommandsKeepTheSigningKeyOnlyOnTheStartupDesk(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	key := filepath.Join(t.TempDir(), "owner-signing.seed")
	t.Setenv("JPACK_SIGNING_KEY", key)
	t.Setenv("DESK_TEST_INHERITED", "kept")
	rig := newAuditRig(t, withAuditVersions)
	s, ts, _ := gatesServer(t, rig.bin)
	writeProject(t, s.projectDir, map[string]string{"jpack.json": auditedConfig})
	row := createGatedDesk(t, ts)
	rig.answers(t, 0, auditValidReport)
	for _, desk := range []struct{ id, env string }{
		{"", "[JPACK_SIGNING_KEY=" + key + "] [DESK_TEST_INHERITED=kept]"},
		{row.ID, "[JPACK_SIGNING_KEY=unset] [DESK_TEST_INHERITED=kept]"},
	} {
		os.Remove(rig.calls + ".env")
		reportOf(t, ts, desk.id)
		want := []string{"packs schema --format json " + desk.env, "audit verify --config jpack.json --format json " + desk.env}
		if seen := envSeen(t, rig.calls); !slices.Equal(seen, want) {
			t.Errorf("desk %q: the panel ran %q, want %q", desk.id, seen, want)
		}
	}
}

// The real runtime, end to end on a desk Desk made. With a runtime that reads
// "6": before any record, its refusal; after a deciding run, a chained
// trail's report; after a torn last line, exit 1 read as a report. With an
// older runtime: the one sentence, and its version; and its own answer to
// `audit verify`, had the panel asked, is read as the same absence.
func TestTheDecisionRecordWithTheRuntime(t *testing.T) {
	bin := requireBinary(t)
	t.Setenv("JPACK_CONFIG", "")
	s, ts, _ := gatesServer(t, bin)
	row := createGatedDesk(t, ts)
	var schema struct {
		Tool struct {
			Version string `json:"version"`
		} `json:"tool"`
		Supported []string `json:"supportedConfigVersions"`
	}
	if err := json.Unmarshal(jpackIn(t, bin, row.Folder, "packs", "schema", "--format", "json"), &schema); err != nil {
		t.Fatal(err)
	}
	status, answer, refusal := readAudit(t, ts, row.ID)
	if !slices.Contains(schema.Supported, "6") {
		if status != http.StatusOK || !sameAuditAnswer(answer, auditAnswer{State: auditStateOlder, Runtime: schema.Tool.Version, Floor: "0.26.0"}) {
			t.Errorf("runtime %s answered %d %+v %q", schema.Tool.Version, status, answer, refusal)
		}
		dir, refused := s.desks[row.ID].auditRuntime()
		if refused != "" {
			t.Fatal(refused)
		}
		out, err := runRuntime(t.Context(), bin, dir, "audit", "verify", "--config", "jpack.json", "--format", "json")
		if read, readErr := readAuditVerification(out, err); readErr != nil || read.State != auditStateOlder {
			t.Errorf("runtime %s's own answer to audit verify was read as %+v, %v (%s)", schema.Tool.Version, read, readErr, out)
		}
		return
	}
	if status != http.StatusOK || answer.State != auditStateUnverified || len(answer.Diagnostics) != 1 || answer.Diagnostics[0].Code != "JPS-AUDIT-TRAIL-READ" || answer.Runtime != schema.Tool.Version {
		t.Fatalf("before any record the panel answered %d %+v %q", status, answer, refusal)
	}

	config := strings.Replace(gatedConfigFor(t, row.ConfigVersion), `"packs":{}`, `"packs":{"alpha":{"path":"packs/a.json"}}`, 1)
	writeProject(t, row.Folder, map[string]string{"packs/a.json": reviewPack, "jpack.json": config})
	jpackIn(t, bin, row.Folder, "packs", "lock", "--config", "jpack.json", "--format", "json")
	facts := filepath.Join(t.TempDir(), "facts.json")
	if err := os.WriteFile(facts, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	jpackIn(t, bin, row.Folder, "experimental", "evaluate", "--config", "jpack.json", "--pack-id", "alpha", "--facts", facts, "--format", "json")
	report := reportOf(t, ts, row.ID)
	if report.Status != "valid" || report.Lines != 1 || report.Coverage.Chained != 1 || report.Coverage.Signed.Status != "not-checked" || len(report.Establishes) == 0 || len(report.DoesNotEstablish) == 0 {
		t.Errorf("after one deciding run the panel shows %+v", report)
	}

	trail, err := os.OpenFile(filepath.Join(row.Folder, ".desk-private", "audit", "evaluations.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	trail.WriteString(`{"torn":`)
	trail.Close()
	report = reportOf(t, ts, row.ID)
	if report.Status != "invalid" || len(report.Findings) != 1 || report.Findings[0].Name != "incomplete-last-line" {
		t.Errorf("after a torn line the panel shows %+v", report)
	}
}
