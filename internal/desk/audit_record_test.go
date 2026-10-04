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
	"unicode"
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
	// auditUnknownVerify is what a runtime with an audit group and no verify
	// would say, in the same parser's words.
	auditUnknownVerify = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.0.0"},"command":"jpack","status":"error","diagnostics":[{"code":"JPS-INVOCATION-ARGUMENTS","message":"unknown command \"verify\" for \"jpack audit\""}]}`
	// auditNoTrailYet is 0.26.0's answer, exit 4, where no record has been
	// written yet.
	auditNoTrailYet = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.26.0"},"command":"audit verify","status":"error","diagnostics":[{"code":"JPS-AUDIT-TRAIL-READ","codeStability":"provisional","layer":"operation","severity":"error","instancePath":"","message":"The project's trail /project/.desk-private/audit/evaluations.jsonl does not exist yet: no record has been written."}]}`
)

// auditMixedReport is what the published runtime 0.26.0 printed over a trail
// the published 0.25.0 and 0.26.0 wrote in turn: two unchained runs, a chained
// one, an unchained one, a chained one, and an unchained one. So the first two
// lines are the legacy prefix, one unchained line is committed to by a later
// chained one, and the last is covered by nothing.
const auditMixedReport = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.26.0"},"command":"audit verify","trailPath":"/project/.desk-private/audit/evaluations.jsonl","snapshotBetweenWrites":true,"status":"valid","scope":"one-supplied-chain","lines":6,"bytes":5146,"trail":"c962ef5fa560f62c67bd4c1c29e011e3","head":{"checkpointVersion":"1","recordDigest":"sha256:3a8839bbe26f8c56088623eb7b290572cd5a3941fb97456005c86106bb2d23ee","sequence":5,"trail":"c962ef5fa560f62c67bd4c1c29e011e3"},"coverage":{"legacyPrefix":2,"chained":2,"unchained":1,"uncovered":1,"damaged":0,"signed":{"status":"not-checked","detail":"no public key was supplied"},"signedRecords":0,"unsignedRecords":0,"checkpointed":{"status":"not-supplied"},"witnessed":0,"unwitnessed":2,"stamped":{"status":"not-checked","detail":"no time-stamping roots were supplied"}},"segments":[{"firstLine":1,"lastLine":6}],"segmentsTotal":1,"discontinuities":[],"discontinuitiesTotal":0,"findings":[],"findingsTotal":0,"establishes":["The chained lines are consistent with one another: no line before the last was edited, inserted, deleted or moved without breaking a link, and the lines before the first chained line are the block its previous commits to."],"doesNotEstablish":["The last line, and any lines rewritten from some point on with their links recomputed, are not authenticated by the chain: only a checkpoint covering them, held independently of the operator, shows they are the ones first written.","That the trail is complete: a trail cut short is as consistent as the whole one, and nothing here says which decisions were never written to it.","That any record's at is true: it is the operator's clock.","Who wrote any record: no public key was supplied, so no signature was checked.","When any checkpoint existed: no time-stamping roots were supplied, so no stamp was checked."]}`

// What the published runtime 0.26.0 printed, measured, when it refused to
// verify: a configuration whose audit.chain is not a boolean (exit 1), one of
// a configVersion it does not read (exit 2), and an argument it could not read
// (exit 3, for its root command). And an extra argument and an unknown flag,
// each in its command parser's words (exit 3): neither says the command is
// absent.
const (
	auditBadChain   = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.26.0"},"command":"audit verify","status":"error","diagnostics":[{"code":"JPS-PROJECT-CONFIG-SCHEMA","codeStability":"provisional","layer":"operation","severity":"error","instancePath":"","message":"The project configuration jpack.json does not satisfy the jpack.json schema: jsonschema validation failed with 'urn:judgmentpack:runtime:jpack-config:6#' - at '/audit/chain': got string, want boolean - at '': 'allOf' failed - at '/configVersion': value must be '6'"}]}`
	auditBadVersion = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.26.0"},"command":"audit verify","status":"unsupported","diagnostics":[{"code":"JPS-PROJECT-CONFIG-VERSION","codeStability":"provisional","layer":"operation","severity":"error","instancePath":"","message":"The project configuration jpack.json declares configVersion \"99\", which this runtime does not support. It accepts: 1, 2, 3, 4, 5, 6. This configuration comes from a newer toolchain: upgrade the runtime. Do not edit the declaration to an older version — that discards what this configuration declares."}]}`
	auditBadInteger = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.26.0"},"command":"jpack","status":"error","diagnostics":[{"code":"JPS-INVOCATION-ARGUMENTS","codeStability":"provisional","layer":"operation","severity":"error","instancePath":"","message":"invalid argument \"bad\" for \"--require-signed-through\" flag: strconv.ParseInt: parsing \"bad\": invalid syntax"}]}`
	auditExtraArg   = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.26.0"},"command":"jpack","status":"error","diagnostics":[{"code":"JPS-INVOCATION-ARGUMENTS","codeStability":"provisional","layer":"operation","severity":"error","instancePath":"","message":"unknown command \"extra\" for \"jpack audit verify\""}]}`
	auditWrongFlag  = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.26.0"},"command":"jpack","status":"error","diagnostics":[{"code":"JPS-INVOCATION-ARGUMENTS","codeStability":"provisional","layer":"operation","severity":"error","instancePath":"","message":"unknown flag: --wrong"}]}`
)

// auditedConfig is a project that keeps a trail, as a desk Desk made does.
const auditedConfig = `{"configVersion":"5","audit":{"dir":".desk-private/audit"},"packs":{}}` + "\n"

// auditRig is a stand-in runtime for the panel, and the file that steers its
// `audit verify`.
type auditRig struct {
	bin, calls, answer string
}

// deskAnswerName is the file a desk's folder can hold for the stand-in's
// `audit verify` to answer with there instead: so an answer says which
// directory the command ran in.
const deskAnswerName = "stand-in-verify.json"

// newAuditRig writes the stand-in. Its `packs schema` names versions; its
// `audit verify` prints what `answers` prepared and exits with its code, or,
// where the directory it runs in holds deskAnswerName, that file and its
// code. Every run appends its arguments and environment as
// `writeStandInRuntime` says.
func newAuditRig(t *testing.T, versions string) *auditRig {
	t.Helper()
	return newAuditRigSaying(t, reading(versions))
}

// newAuditRigSaying is newAuditRig with schema as its `packs schema`, a shell
// fragment.
func newAuditRigSaying(t *testing.T, schema string) *auditRig {
	t.Helper()
	dir := t.TempDir()
	rig := &auditRig{bin: filepath.Join(dir, "jpack"), answer: filepath.Join(dir, "verify.json")}
	rig.calls = writeStandInRuntime(t, rig.bin, schema, lockingAs(wantGatedConfig))
	script, err := os.ReadFile(rig.bin)
	if err != nil {
		t.Fatal(err)
	}
	verify := "'audit verify')\n  answer='" + rig.answer + "'\n  if [ -e ./" + deskAnswerName + " ]; then answer=./" + deskAnswerName + "; fi\n" +
		"  while IFS= read -r line || [ -n \"$line\" ]; do printf '%s\\n' \"$line\"; done < \"$answer\"\n" +
		"  IFS= read -r code < \"$answer.exit\"\n  exit \"$code\"\n  ;;\n"
	script = bytes.Replace(script, []byte("'packs lock')\n"), []byte(verify+"'packs lock')\n"), 1)
	if err := os.WriteFile(rig.bin, script, 0o755); err != nil {
		t.Fatal(err)
	}
	return rig
}

// answers sets what the stand-in's `audit verify` prints, and its exit.
func (rig *auditRig) answers(t *testing.T, code int, body string) {
	t.Helper()
	answerAt(t, rig.answer, code, body)
}

// answerAt writes an answer for the stand-in at path, and its exit.
func answerAt(t *testing.T, path string, code int, body string) {
	t.Helper()
	if os.WriteFile(path, []byte(body), 0o600) != nil || os.WriteFile(path+".exit", []byte(strconv.Itoa(code)+"\n"), 0o600) != nil {
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

// refusalOf is a refusal's sentence.
func refusalOf(data []byte) string {
	var body struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(data, &body)
	return body.Error
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

// edited is a report the runtime printed, with change applied to it as JSON.
func edited(t *testing.T, report string, change func(map[string]any)) string {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(report), &value); err != nil {
		t.Fatal(err)
	}
	change(value)
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// member is the object at a path of members in a decoded report.
func member(value map[string]any, path ...string) map[string]any {
	for _, name := range path {
		value = value[name].(map[string]any)
	}
	return value
}

// **What the runtime does not document is an error, said as one.** A report
// whose status disagrees with its exit; one missing any member a report has,
// down to each coverage count, each protection's status and each item of its
// lists, or holding a value no report has; one whose lists are longer than
// their totals, or whose findings disagree with its status; an answer from
// another command; an error without words, or one that exits 0; output that
// is not JSON; and output past `runRuntime`'s bound are each refused, never
// shown as a report.
func TestAnAnswerTheRuntimeDoesNotDocumentIsAnError(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	ts, rig, _ := auditDesk(t, withAuditVersions, auditedConfig)
	without := func(report string, path ...string) string {
		return edited(t, report, func(value map[string]any) {
			delete(member(value, path[:len(path)-1]...), path[len(path)-1])
		})
	}
	set := func(report string, to any, path ...string) string {
		return edited(t, report, func(value map[string]any) {
			member(value, path[:len(path)-1]...)[path[len(path)-1]] = to
		})
	}
	item := func(report, list string, change func(map[string]any)) string {
		return edited(t, report, func(value map[string]any) {
			change(value[list].([]any)[0].(map[string]any))
		})
	}
	padded := strings.Replace(auditValidReport, `"trailPath":"`, `"trailPath":"`+strings.Repeat("x", runtimeAnswerLimit), 1)
	cases := []struct {
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
		{"an error with no diagnostics", 4, `{"command":"audit verify","status":"error","diagnostics":[]}`, "did not answer as documented"},
		{"a diagnostic with no words", 4, `{"command":"audit verify","status":"error","diagnostics":[{"code":"JPS-AUDIT-TRAIL-READ","message":""}]}`, "did not answer as documented"},
		{"a diagnostic with no code", 4, `{"command":"audit verify","status":"error","diagnostics":[{"message":"The trail could not be read."}]}`, "did not answer as documented"},
		{"an error that exits 0", 0, auditNoTrailYet, "did not answer as documented"},
		{"a report past runRuntime's bound", 0, padded, "larger than 65536 bytes"},
		{"findings under a valid status", 0, set(auditValidReport, 1, "findingsTotal"), "did not answer as documented"},
		{"no findings under an invalid status", 1, set(set(auditInvalidReport, []any{}, "findings"), 0, "findingsTotal"), "did not answer as documented"},
		{"more findings than their total", 1, edited(t, auditInvalidReport, func(value map[string]any) {
			findings := value["findings"].([]any)
			value["findings"] = append(findings, findings[0])
		}), "did not answer as documented"},
		{"more segments than their total", 0, set(auditSegmentedReport, 1, "segmentsTotal"), "did not answer as documented"},
		{"more discontinuities than their total", 0, set(auditSegmentedReport, 0, "discontinuitiesTotal"), "did not answer as documented"},
		{"a negative count", 0, set(auditValidReport, -1, "coverage", "uncovered"), "did not answer as documented"},
		{"a count that is not a number", 0, set(auditValidReport, "3", "lines"), "did not answer as documented"},
		{"signed through no sequence", 0, set(auditValidReport, map[string]any{"status": "through"}, "coverage", "signed"), "did not answer as documented"},
		{"a protection with no status", 0, set(auditValidReport, map[string]any{"detail": "no public key was supplied"}, "coverage", "signed"), "did not answer as documented"},
		{"a segment with no first line", 0, item(auditSegmentedReport, "segments", func(v map[string]any) { delete(v, "firstLine") }), "did not answer as documented"},
		{"a discontinuity with no digest", 0, item(auditSegmentedReport, "discontinuities", func(v map[string]any) { delete(v, "digest") }), "did not answer as documented"},
		{"a discontinuity with no damaged line", 0, item(auditSegmentedReport, "discontinuities", func(v map[string]any) { delete(v, "damagedLine") }), "did not answer as documented"},
		{"a finding with no name", 1, item(auditInvalidReport, "findings", func(v map[string]any) { delete(v, "name") }), "did not answer as documented"},
		{"a finding with no line", 1, item(auditInvalidReport, "findings", func(v map[string]any) { delete(v, "line") }), "did not answer as documented"},
		{"a finding with no detail", 1, item(auditInvalidReport, "findings", func(v map[string]any) { delete(v, "detail") }), "did not answer as documented"},
	}
	for _, name := range []string{"coverage", "segments", "discontinuities", "findings", "establishes", "doesNotEstablish", "snapshotBetweenWrites", "lines", "bytes", "segmentsTotal", "discontinuitiesTotal", "findingsTotal"} {
		cases = append(cases, struct {
			name string
			code int
			body string
			says string
		}{"a report with no " + name, 0, without(auditValidReport, name), "did not answer as documented"})
	}
	for _, name := range []string{"legacyPrefix", "chained", "unchained", "uncovered", "damaged", "signed", "signedRecords", "unsignedRecords", "checkpointed", "witnessed", "unwitnessed", "stamped"} {
		cases = append(cases, struct {
			name string
			code int
			body string
			says string
		}{"a report with no coverage." + name, 0, without(auditValidReport, "coverage", name), "did not answer as documented"})
	}
	// The invalid report the reviewer named: its findings would be hidden
	// behind a total of nothing.
	cases = append(cases, struct {
		name string
		code int
		body string
		says string
	}{"an invalid report with no findingsTotal", 1, without(auditInvalidReport, "findingsTotal"), "did not answer as documented"})
	for _, tc := range cases {
		rig.answers(t, tc.code, tc.body)
		status, answer, refusal := readAudit(t, ts, "")
		if status != http.StatusInternalServerError || !strings.HasPrefix(refusal, "The decision record could not be checked: ") || !strings.Contains(refusal, tc.says) || answer.Report != nil {
			t.Errorf("%s: answered %d %+v %q, want an error saying %q", tc.name, status, answer, refusal, tc.says)
		}
	}
	// Each accepted unchanged, so it is the change that is refused.
	for _, tc := range []struct {
		code int
		body string
	}{{0, auditValidReport}, {1, auditInvalidReport}, {0, auditSegmentedReport}, {0, auditMixedReport}} {
		rig.answers(t, tc.code, tc.body)
		reportOf(t, ts, "")
	}
}

// **A refusal the runtime explains is shown in its words, whatever its
// exit.** The published 0.26.0's own refusals, measured: a configuration it
// refuses exits 1 with JPS-PROJECT-CONFIG-SCHEMA, one of a configVersion it
// does not read exits 2, an argument it cannot read exits 3 for its root
// command, a trail with no record yet exits 4. None of them is a report, and
// none says the runtime has no audit commands.
func TestARefusalTheRuntimeExplainsIsShownInItsWords(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	ts, rig, _ := auditDesk(t, withAuditVersions, auditedConfig)
	twoSaid := `{"command":"jpack","status":"error","diagnostics":[{"code":"JPS-INVOCATION-ARGUMENTS","message":"unknown flag: --config"},{"code":"JPS-INVOCATION-ARGUMENTS","message":"unknown flag: --format"}]}`
	otherCode := strings.Replace(auditUnknownFlag, "JPS-INVOCATION-ARGUMENTS", "JPS-INVOCATION-FORMAT", 1)
	for _, tc := range []struct {
		name string
		code int
		body string
	}{
		{"an audit.chain that is not a boolean, exit 1", 1, auditBadChain},
		{"a configVersion it does not read, exit 2", 2, auditBadVersion},
		{"an argument it cannot read, exit 3", 3, auditBadInteger},
		{"an extra argument, exit 3", 3, auditExtraArg},
		{"an unknown flag that is not --config, exit 3", 3, auditWrongFlag},
		{"a trail with no record yet, exit 4", 4, auditNoTrailYet},
		{"the parser's absence words beside others, exit 3", 3, twoSaid},
		{"the absence words under another code, exit 3", 3, otherCode},
		{"the absence words with exit 1", 1, auditUnknownFlag},
		{"the absence words with exit 2", 2, auditUnknownFlag},
	} {
		rig.answers(t, tc.code, tc.body)
		var want struct {
			Diagnostics []runtimeDiagnostic `json:"diagnostics"`
		}
		if err := json.Unmarshal([]byte(tc.body), &want); err != nil {
			t.Fatal(err)
		}
		if tc.body == auditNoTrailYet {
			// The runtime names the trail by its path; the panel says which file.
			want.Diagnostics[0].Message = "The project's trail …/evaluations.jsonl does not exist yet: no record has been written."
		}
		status, answer, refusal := readAudit(t, ts, "")
		if status != http.StatusOK || answer.State != auditStateUnverified || !slices.Equal(answer.Diagnostics, want.Diagnostics) || answer.Report != nil || answer.Runtime != "0.0.0-stand-in" {
			t.Errorf("%s: answered %d %+v %q, want the runtime's own words", tc.name, status, answer, refusal)
		}
	}
}

// **No answer says where the owner keeps their files.** The runtime names
// paths in its sentences: the trail it could not open, resolved from an
// absolute audit.dir through the folder Desk entered, or a file a finding is
// about. The panel passes on every sentence, in a refusal and in a report,
// with each path from a root replaced, and keeps a JSON pointer, which is
// not one.
func TestThePanelQuotesNoPath(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	ts, rig, project := auditDesk(t, withAuditVersions, auditedConfig)
	secret := "/private/SECRET-AUDIT-PATH"
	refused := `{"outputVersion":"2","tool":{"name":"jpack","version":"0.26.0"},"command":"audit verify","status":"error","diagnostics":[` +
		`{"code":"JPS-AUDIT-TRAIL-READ","message":"The project's trail /proc/self/fd/3` + secret + `/evaluations.jsonl could not be opened: open /proc/self/fd/3` + secret + `/evaluations.jsonl: no such file or directory."},` +
		`{"code":"JPS-AUDIT-TRAIL-READ","message":"The project is at ` + project + `, and its key at \"` + secret + `/key.seed\"."},` +
		`{"code":"JPS-PROJECT-CONFIG-SCHEMA","message":"at '/audit/chain': got string, want boolean"}]}`
	rig.answers(t, 4, refused)
	status, data := reviewCall(t, ts, "GET", "/api/audit/verify", "", nil, bearer)
	for _, leaked := range []string{"SECRET-AUDIT-PATH", "/proc/self", project} {
		if strings.Contains(string(data), leaked) {
			t.Errorf("the refusal says %q: %s", leaked, data)
		}
	}
	var answer auditAnswer
	if status != http.StatusOK || json.Unmarshal(data, &answer) != nil || len(answer.Diagnostics) != 3 {
		t.Fatalf("the refusal answered %d %s", status, data)
	}
	for i, want := range []string{
		"The project's trail …/evaluations.jsonl could not be opened: open …/evaluations.jsonl: no such file or directory.",
		`The project is at the project's folder, and its key at "…".`,
		"at '/audit/chain': got string, want boolean",
	} {
		if answer.Diagnostics[i].Message != want {
			t.Errorf("diagnostic %d says %q, want %q", i, answer.Diagnostics[i].Message, want)
		}
	}

	// And in a report: a finding's detail, a reason, a coverage detail and
	// the runtime's sentences.
	report := strings.Replace(auditInvalidReport, `"detail":"the trail ends in 8 bytes with no newline: a write that did not complete"`, `"detail":"`+secret+`/evaluations.jsonl ends in 8 bytes"`, 1)
	report = strings.Replace(report, `"detail":"no public key was supplied"`, `"detail":"no key at `+secret+`/key.seed"`, 1)
	report = strings.Replace(report, `"establishes":[]`, `"establishes":["The lines in `+project+`/.desk-private/audit are consistent."]`, 1)
	report = strings.Replace(report, `"doesNotEstablish":["`, `"doesNotEstablish":["Nothing about `+secret+`/stamps.jsonl. `, 1)
	report = strings.Replace(report, `"checkpointed":{"status":"not-supplied"}`, `"checkpointed":{"status":"not-supplied","detail":"none held at `+secret+`"}`, 1)
	report = strings.Replace(report, `"stamped":{"status":"not-checked","detail":"no time-stamping roots were supplied"}`, `"stamped":{"status":"not-checked","detail":"no roots at `+secret+`/roots.pem; see /elsewhere/My SECRET-AUDIT-PATH copy/stamps.jsonl"}`, 1)
	report = strings.Replace(report, `"discontinuities":[],"discontinuitiesTotal":0`, `"discontinuities":[{"line":2,"reason":"repaired at `+secret+`","damagedLine":1,"bytes":8,"digest":"sha256:952cdc0f85ab10d18a1bdccfeb6c3991e59ab424dc2ce916544aac44f3d8b45e"}],"discontinuitiesTotal":1`, 1)
	if strings.Count(report, secret) != 6 {
		t.Fatal("the report fixture does not hold a path in every field the panel passes on")
	}
	rig.answers(t, 1, report)
	status, data = reviewCall(t, ts, "GET", "/api/audit/verify", "", nil, bearer)
	for _, leaked := range []string{"SECRET-AUDIT-PATH", project} {
		if strings.Contains(string(data), leaked) {
			t.Errorf("the report says %q: %s", leaked, data)
		}
	}
	answer = auditAnswer{}
	if status != http.StatusOK || json.Unmarshal(data, &answer) != nil || answer.Report == nil {
		t.Fatalf("the report answered %d %s", status, data)
	}
	if got := answer.Report.Findings[0].Detail; got != "…/evaluations.jsonl ends in 8 bytes" {
		t.Errorf("the finding says %q", got)
	}
	if got := answer.Report.Coverage.Signed.Detail; got != "no key at …" {
		t.Errorf("the signatures' detail says %q", got)
	}
	if got := answer.Report.Establishes; len(got) != 1 || got[0] != "The lines in … are consistent." {
		t.Errorf("the report establishes %q", got)
	}
	if got := answer.Report.DoesNotEstablish; len(got) == 0 || !strings.HasPrefix(got[0], "Nothing about …/stamps.jsonl. ") {
		t.Errorf("the report does not establish %q", got)
	}
	if got := answer.Report.Coverage.Checkpointed.Detail + " | " + answer.Report.Coverage.Stamped.Detail; got != "none held at … | no roots at …; see …/stamps.jsonl" {
		t.Errorf("the coverage details say %q", got)
	}
	if got := answer.Report.Discontinuities; len(got) != 1 || got[0].Reason != "repaired at …" {
		t.Errorf("the discontinuity says %+v", got)
	}
}

// **The audit directory is replaced whole, however the runtime joins it.**
// The runtime names its trail as the folder it ran in joined with audit.dir:
// an absolute one is joined on, and one that climbs out is resolved to where
// it is. A space or a parenthesis in it must not end the replacement, and the
// project's folder before it must not be replaced first and leave the rest.
func TestTheAuditDirectoryIsNeverQuoted(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	outside := filepath.Join(t.TempDir(), "Top SECRET (audit) dir")
	for _, tc := range []struct{ name, dir string }{
		{"absolute, with a space and parentheses", outside},
		{"climbing out of the project", "../Top SECRET dir"},
		{"inside, with a space", "Top SECRET dir"},
		{"absolute, with a tab", filepath.Join(filepath.Dir(outside), "Top\t SECRET dir")},
		{"absolute, with a newline", filepath.Join(filepath.Dir(outside), "Top\n SECRET dir")},
		{"absolute, with a zero-width space", filepath.Join(filepath.Dir(outside), "Top\u200b SECRET dir")},
		// A line or paragraph separator, beside a colon, a semicolon or a
		// double quote, which a path to a runtime file may not cross: only
		// the audit directory's displayed span can take these whole.
		{"inside, with a line separator and a colon", "Top\u2028 SECRET-AUDIT-PATH:TAIL"},
		{"absolute, with a paragraph separator and a semicolon", filepath.Join(filepath.Dir(outside), "Top\u2029 SECRET;TAIL")},
		{"absolute, with a line separator and a double quote", filepath.Join(filepath.Dir(outside), "Top\u2028 SECRET\"TAIL")},
	} {
		config := `{"configVersion":"5","audit":{"dir":` + strconv.Quote(tc.dir) + `},"packs":{}}` + "\n"
		ts, rig, project := auditDesk(t, withAuditVersions, config)
		// As the runtime prints them (runtimePrints, not the code under test).
		joined := runtimePrints(filepath.Join(project, tc.dir, "evaluations.jsonl"))
		proc := runtimePrints(filepath.Join("/proc/self/fd/3", tc.dir, "evaluations.jsonl"))
		said := []map[string]string{
			{"code": "JPS-AUDIT-TRAIL-READ", "message": "The project's trail " + joined + " could not be opened as one regular file inside the project."},
			{"code": "JPS-AUDIT-TRAIL-READ", "message": "The project's trail " + proc + " does not exist yet: no record has been written."},
		}
		want := []string{
			"The project's trail …/evaluations.jsonl could not be opened as one regular file inside the project.",
			"The project's trail …/evaluations.jsonl does not exist yet: no record has been written.",
		}
		if filepath.IsAbs(tc.dir) {
			// An absolute audit.dir named on its own, not joined to anything.
			said = append(said, map[string]string{"code": "JPS-AUDIT-TRAIL-READ", "message": "The audit directory " + runtimePrints(tc.dir) + " is not inside the project."})
			want = append(want, "The audit directory … is not inside the project.")
		}
		body, _ := json.Marshal(map[string]any{"command": "audit verify", "status": "error", "diagnostics": said})
		rig.answers(t, 4, string(body))
		status, data := reviewCall(t, ts, "GET", "/api/audit/verify", "", nil, bearer)
		for _, leaked := range []string{"SECRET", "TAIL", "(audit)", project, "/proc/self"} {
			if strings.Contains(string(data), leaked) {
				t.Errorf("%s: the panel says %q: %s", tc.name, leaked, data)
			}
		}
		var answer auditAnswer
		if status != http.StatusOK || json.Unmarshal(data, &answer) != nil || len(answer.Diagnostics) != len(want) {
			t.Fatalf("%s: the panel answered %d %s", tc.name, status, data)
		}
		for i := range want {
			if answer.Diagnostics[i].Message != want[i] {
				t.Errorf("%s: diagnostic %d says %q, want %q", tc.name, i, answer.Diagnostics[i].Message, want[i])
			}
		}
	}
}

// **The owner's log keeps the runtime's words whole.** Where the panel
// replaced a path, Desk's log has the answer as the runtime gave it; where
// there was nothing to replace, it says nothing.
func TestTheRuntimesOwnWordsStayInDesksLog(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	rig := newAuditRig(t, withAuditVersions)
	s, ts, logged := gatesServer(t, rig.bin)
	writeProject(t, s.projectDir, map[string]string{"jpack.json": auditedConfig})
	rig.answers(t, 4, `{"command":"audit verify","status":"error","diagnostics":[{"code":"JPS-AUDIT-TRAIL-READ","message":"The project's trail /private/SECRET-AUDIT-PATH/evaluations.jsonl could not be opened."}]}`)
	if status, data := reviewCall(t, ts, "GET", "/api/audit/verify", "", nil, bearer); status != http.StatusOK || strings.Contains(string(data), "SECRET-AUDIT-PATH") {
		t.Fatalf("the panel answered %d %s", status, data)
	}
	if !strings.Contains(logged.String(), "desk: the decision record, as the runtime said it:") || !strings.Contains(logged.String(), "/private/SECRET-AUDIT-PATH/evaluations.jsonl") {
		t.Errorf("the log does not keep the runtime's words: %q", logged.String())
	}
	logged.Reset()
	rig.answers(t, 2, auditBadVersion)
	if status, _ := reviewCall(t, ts, "GET", "/api/audit/verify", "", nil, bearer); status != http.StatusOK {
		t.Fatalf("the panel answered %d", status)
	}
	if strings.Contains(logged.String(), "as the runtime said it") {
		t.Errorf("an answer with no path was logged: %q", logged.String())
	}
}

// runtimePrints is the runtime's own display rule, copied here from runtime
// 0.26.0's `internal/display/sanitize.go` as the tests' oracle, so that a test
// of displayedPath never takes its expectation from displayedPath.
func runtimePrints(value string) string {
	var output strings.Builder
	for _, char := range value {
		if unicode.IsControl(char) || unicode.Is(unicode.Cf, char) || unicode.Is(unicode.Zl, char) || unicode.Is(unicode.Zp, char) {
			output.WriteRune('?')
			continue
		}
		output.WriteRune(char)
	}
	return output.String()
}

// **The runtime's display rule, category by category** (runtime 0.26.0,
// `internal/display/sanitize.go`): a control, format, line-separator or
// paragraph-separator character is printed as "?", and nothing else is.
func TestAPathIsTakenAsTheRuntimePrintsIt(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"a\tb", "a?b"},          // Cc
		{"a\x07b", "a?b"},        // Cc
		{"a\u200bb", "a?b"},      // Cf
		{"a\u202eb", "a?b"},      // Cf
		{"a\u2028b", "a?b"},      // Zl
		{"a\u2029b", "a?b"},      // Zp
		{"a\u00a0b", "a\u00a0b"}, // Zs, left
		{"aéb", "aéb"},
		{"a\ue000b", "a\ue000b"}, // private use, left
	} {
		if got := displayedPath(tc.in); got != tc.want {
			t.Errorf("%q is taken as %q, want %q", tc.in, got, tc.want)
		}
	}
}

// What a message keeps and what it loses: a path from a root, on Unix or
// Windows, wherever it stands in a sentence, keeps only a runtime file's name;
// a JSON pointer, a URL, a relative path and a fraction are left as they are.
func TestAMessageNamesNoAbsolutePath(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"/a/b/evaluations.jsonl", "…/evaluations.jsonl"},
		{"open /a/b/c: no such file", "open …: no such file"},
		{"trail /a/stamps.jsonl.", "trail …/stamps.jsonl."},
		{`key "/home/owner/key.seed" refused`, `key "…" refused`},
		{"(/x/jpack.json)", "(…/jpack.json)"},
		{"dir=/srv/x", "dir=…"},
		{"[/srv/x]", "[…]"},
		{`at C:\Users\owner\signatures.jsonl`, "at …/signatures.jsonl"},
		{"at D:/data/x", "at …"},
		{"at '/audit/chain': got string", "at '/audit/chain': got string"},
		{"see https://example.org/a/b", "see https://example.org/a/b"},
		{".desk-private/audit/evaluations.jsonl", ".desk-private/audit/evaluations.jsonl"},
		{"line 3/4 of 7", "line 3/4 of 7"},
		{"/", "…"},
		{"The project's trail /x/Top? SECRET dir/evaluations.jsonl could not be opened.", "The project's trail …/evaluations.jsonl could not be opened."},
		{"open /a/b: no such file in trail /x/y z/stamps.jsonl.", "open …: no such file in trail …/stamps.jsonl."},
		{`at C:\My Files\signatures.jsonl, then`, "at …/signatures.jsonl, then"},
		{"/a b/jpack.json.bak is not read", "…/jpack.json.bak is not read"},
	} {
		if got := withoutAbsolutePaths(tc.in); got != tc.want {
			t.Errorf("%q became %q, want %q", tc.in, got, tc.want)
		}
	}
}

// **With a runtime that has no audit commands, one sentence and nothing
// more** (ADR-0010, section 6). A runtime that does not read "6" is asked
// nothing past `packs schema`. One that reads it, and whose command parser
// still says, alone and exit 3, that it has no `audit verify` (in any of its
// three words for it, 0.25.0's two among them), is read the same way.
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
	for _, said := range []string{auditUnknownFlag, auditUnknownCommand, auditUnknownVerify} {
		rig.answers(t, 3, said)
		if status, answer, refusal := readAudit(t, ts, ""); status != http.StatusOK || !sameAuditAnswer(answer, older) {
			t.Errorf("a runtime with no audit verify (%s) answered %d %+v %q", said, status, answer, refusal)
		}
		if calls := rig.ran(t); !slices.Equal(calls, []string{schemaCall, verifyCall}) {
			t.Errorf("the panel ran %q", calls)
		}
	}
}

// **A `packs schema` that fails is an error, never an older runtime.** The
// panel does not say a runtime lacks the audit commands because it could not
// ask, and runs nothing past the failed question.
func TestAFailedSchemaIsAnErrorAndNotAnOlderRuntime(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	for _, schema := range []string{
		"  printf 'not json\\n'\n  exit 5",
		"  printf '%s\\n' '{\"command\":\"packs schema\",\"status\":\"valid\"}'",
		"  exit 0",
	} {
		rig := newAuditRigSaying(t, schema)
		rig.answers(t, 0, auditValidReport)
		project := t.TempDir()
		writeProject(t, project, map[string]string{"jpack.json": auditedConfig})
		s, ts := startDesk(t, Config{ProjectDir: project, JpackBin: rig.bin, Token: testToken, Logger: log.New(io.Discard, "", 0)})
		status, answer, refusal := readAudit(t, ts, "")
		if status != http.StatusInternalServerError || !strings.Contains(refusal, "configuration versions it reads") || answer.State != "" {
			t.Errorf("schema %q: answered %d %+v %q, want an error", schema, status, answer, refusal)
		}
		if calls := rig.ran(t); !slices.Equal(calls, []string{schemaCall}) {
			t.Errorf("schema %q: the panel ran %q, want packs schema alone", schema, calls)
		}
		s.Close()
		ts.Close()
	}
}

// **Every count is the runtime's, none dropped.** A trail two runtimes wrote
// in turn, the published 0.25.0 unchained and 0.26.0 chained, has a legacy
// prefix, an unchained line a later chained one commits to, and an uncovered
// last line; the panel shows each count as the runtime gave it.
func TestEveryCoverageCountIsTheRuntimes(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	ts, rig, _ := auditDesk(t, withAuditVersions, auditedConfig)
	rig.answers(t, 0, auditMixedReport)
	report := reportOf(t, ts, "")
	want := auditCoverage{LegacyPrefix: 2, Chained: 2, Unchained: 1, Uncovered: 1, Damaged: 0,
		Signed: auditCoverageState{Status: "not-checked", Detail: "no public key was supplied"}, Checkpointed: auditCoverageState{Status: "not-supplied"},
		Witnessed: 0, Unwitnessed: 2, Stamped: auditCoverageState{Status: "not-checked", Detail: "no time-stamping roots were supplied"}}
	if report.Coverage != want || report.Lines != 6 || report.Status != "valid" {
		t.Errorf("the mixed trail is shown as %+v", report)
	}
	held := edited(t, auditValidReport, func(value map[string]any) {
		coverage := member(value, "coverage")
		coverage["signed"] = map[string]any{"status": "through", "through": 3}
		coverage["signedRecords"], coverage["unsignedRecords"] = 3, 0
		coverage["checkpointed"] = map[string]any{"status": "through", "through": 2}
		coverage["witnessed"], coverage["unwitnessed"] = 2, 1
		coverage["stamped"] = map[string]any{"status": "none"}
	})
	rig.answers(t, 0, held)
	report = reportOf(t, ts, "")
	if c := report.Coverage; c.Signed != (auditCoverageState{Status: "through", Through: 3}) || c.SignedRecords != 3 || c.Checkpointed != (auditCoverageState{Status: "through", Through: 2}) ||
		c.Witnessed != 2 || c.Unwitnessed != 1 || c.Stamped != (auditCoverageState{Status: "none"}) {
		t.Errorf("held inputs' reach is shown as %+v", c)
	}
}

// **Each desk's command runs in that desk's own folder.** The stand-in answers
// from a file in the directory it runs in, so the startup desk and a desk Desk
// made each see the answer their own folder holds.
func TestEachDeskIsCheckedInItsOwnFolder(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	rig := newAuditRig(t, withAuditVersions)
	rig.answers(t, 4, auditNoTrailYet)
	s, ts, _ := gatesServer(t, rig.bin)
	writeProject(t, s.projectDir, map[string]string{"jpack.json": auditedConfig})
	answerAt(t, filepath.Join(s.projectDir, deskAnswerName), 0, auditValidReport)
	row := createGatedDesk(t, ts)
	answerAt(t, filepath.Join(row.Folder, deskAnswerName), 0, auditMixedReport)
	other := createGatedDesk(t, ts)
	answerAt(t, filepath.Join(other.Folder, deskAnswerName), 1, auditInvalidReport)
	for _, desk := range []struct {
		id    string
		lines int64
		state string
	}{{"", 3, "valid"}, {row.ID, 6, "valid"}, {other.ID, 3, "invalid"}, {row.ID, 6, "valid"}, {"", 3, "valid"}} {
		if report := reportOf(t, ts, desk.id); report.Lines != desk.lines || report.Status != desk.state {
			t.Errorf("desk %q was checked as %d lines, %s; want its own folder's %d, %s", desk.id, report.Lines, report.Status, desk.lines, desk.state)
		}
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
	status, data := reviewCall(t, ts, "GET", "/api/audit/verify", "", nil, bearer)
	want := "This project's runtime reads the configuration that JPACK_CONFIG names where Desk was started, and not this project's jpack.json, so Desk does not check its decision record here."
	if status != http.StatusConflict || refusalOf(data) != want {
		t.Errorf("under another project's JPACK_CONFIG the panel answered %d %s", status, data)
	}
	// The setting is named, and its value is not: neither the path nor the
	// folder it is in.
	if bytes.Contains(data, []byte(elsewhere)) || bytes.Contains(data, []byte(filepath.Base(elsewhere))) {
		t.Errorf("the panel's refusal discloses the configured path: %s", data)
	}
	// Nor does an error: a runtime that has gone from where it was named is
	// said by its file name, not its folder.
	t.Setenv("JPACK_CONFIG", "")
	gone := t.TempDir()
	missing := filepath.Join(gone, "jpack")
	_, ts3 := startDesk(t, Config{ProjectDir: project, JpackBin: missing, Token: testToken, Logger: log.New(io.Discard, "", 0)})
	status, data = reviewCall(t, ts3, "GET", "/api/audit/verify", "", nil, bearer)
	if status != http.StatusInternalServerError || !strings.HasPrefix(refusalOf(data), "The decision record could not be checked: ") || bytes.Contains(data, []byte(gone)) || bytes.Contains(data, []byte(project)) {
		t.Errorf("with its runtime gone the panel answered %d %s", status, data)
	}
	t.Setenv("JPACK_CONFIG", named)
	// The review's own refusal keeps its wording, which quotes it.
	if status, data := reviewCall(t, ts, "GET", "/api/review", "", nil, bearer); status != http.StatusConflict || !bytes.Contains(data, []byte(named)) {
		t.Errorf("the review's refusal changed: %d %s", status, data)
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

	// An argument it cannot read is its refusal, for its root command, exit
	// 3: shown in its words, and never taken for an older runtime.
	dir, refused := s.desks[row.ID].auditRuntime()
	if refused != "" {
		t.Fatal(refused)
	}
	out, err := runRuntime(t.Context(), bin, dir, "audit", "verify", "--config", "jpack.json", "--format", "json", "--require-signed-through", "bad")
	if read, readErr := readAuditVerification(out, err); readErr != nil || read.State != auditStateUnverified || len(read.Diagnostics) != 1 || read.Diagnostics[0].Code != "JPS-INVOCATION-ARGUMENTS" {
		t.Errorf("runtime %s's answer to an argument it cannot read was read as %+v, %v (%s)", schema.Tool.Version, read, readErr, out)
	}
	// A configuration it refuses, exit 1: its refusal, in its words.
	writeProject(t, row.Folder, map[string]string{"jpack.json": `{"configVersion":"6","audit":{"dir":".desk-private/audit","chain":"yes"},"packs":{}}` + "\n"})
	status, answer, refusal = readAudit(t, ts, row.ID)
	if status != http.StatusOK || answer.State != auditStateUnverified || len(answer.Diagnostics) == 0 || answer.Diagnostics[0].Code != "JPS-PROJECT-CONFIG-SCHEMA" {
		t.Errorf("with a configuration the runtime refuses the panel answered %d %+v %q", status, answer, refusal)
	}
}

// **With the runtime: an absolute audit.dir is not repeated.** The runtime
// resolves it, and says the trail it could not open by that path. The panel
// says which file, and not where.
func TestThePanelQuotesNoPathWithTheRuntime(t *testing.T) {
	bin := requireBinary(t)
	t.Setenv("JPACK_CONFIG", "")
	outside := t.TempDir()
	for _, dir := range []string{
		filepath.Join(outside, "SECRET-AUDIT-PATH"),
		filepath.Join(outside, "Top SECRET (audit) dir"),
		"../Top SECRET dir",
		"Top SECRET dir",
		filepath.Join(outside, "Top\t SECRET dir"),
		filepath.Join(outside, "Top\u200b SECRET dir"),
		"Top\u2028 SECRET-AUDIT-PATH:TAIL",
		filepath.Join(outside, "Top\u2029 SECRET;TAIL"),
	} {
		project := t.TempDir()
		writeProject(t, project, map[string]string{"jpack.json": `{"configVersion":"5","audit":{"dir":` + strconv.Quote(dir) + `},"packs":{}}` + "\n"})
		s, ts := startDesk(t, Config{ProjectDir: project, JpackBin: bin, Token: testToken, Logger: log.New(io.Discard, "", 0)})
		status, data := reviewCall(t, ts, "GET", "/api/audit/verify", "", nil, bearer)
		ts.Close()
		s.Close()
		var answer auditAnswer
		if status != http.StatusOK || json.Unmarshal(data, &answer) != nil {
			t.Fatalf("%q: the panel answered %d %s", dir, status, data)
		}
		if answer.State == auditStateOlder {
			t.Skip("this runtime has no audit commands")
		}
		for _, leaked := range []string{"SECRET", "TAIL", "(audit)", "/proc/self", project, outside} {
			if strings.Contains(string(data), leaked) {
				t.Errorf("%q: the panel says %q: %s", dir, leaked, data)
			}
		}
		if answer.State != auditStateUnverified || len(answer.Diagnostics) == 0 {
			t.Errorf("%q: the panel answered %+v, want the runtime's refusal", dir, answer)
		}
	}
}
