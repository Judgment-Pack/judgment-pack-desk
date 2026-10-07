//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package desk

// What follows a record, in records or in lines (line audit of ADR-0010,
// finding 7). The records since a hand-over and the records pending a stamp
// are counted from what the runtime's report says of the trail's chained
// records, never by taking one line's number from another's: after a repair,
// a line the repair names as damaged is not a record. Where the report does
// not say, the count is of lines, and is given as one. The rule alone, then
// the decision record with a stand-in runtime, then the published runtime
// end to end, which skips without one.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// **The rule.** After no record, every chained record. After the record the
// held checkpoints reach, the runtime's own count of the records none
// witnesses. Where the trail holds no damaged and no unchained line, every
// line after it through the last chained one, which leaves out the
// unchained lines after the last chained line. Anywhere else, the lines, and
// said as lines; and nothing below none.
func TestWhatFollowsARecordIsCountedFromTheReport(t *testing.T) {
	// Three records, a torn fourth line and a repair: the report of runtime
	// 0.27.1 over that trail, with the first three checkpoints held.
	repaired := auditCoverage{Chained: 4, Damaged: 1, Checkpointed: auditCoverageState{Status: "through", Through: 3}, Witnessed: 3, Unwitnessed: 1}
	for _, c := range []struct {
		name     string
		lines    int64
		coverage auditCoverage
		through  int64
		count    int64
		records  bool
	}{
		{"after no record, every chained record", 5, repaired, 0, 4, true},
		{"after the record the held checkpoints reach, the runtime's count", 5, repaired, 3, 1, true},
		{"after another record of a repaired trail, the lines", 5, repaired, 2, 3, false},
		{"after a record of a trail with no held checkpoint, repaired, the lines", 5, auditCoverage{Chained: 4, Damaged: 1, Checkpointed: auditCoverageState{Status: "not-supplied"}, Unwitnessed: 4}, 3, 2, false},
		{"a trail with an unchained line, the lines", 6, auditCoverage{LegacyPrefix: 1, Chained: 4, Unchained: 1, Checkpointed: auditCoverageState{Status: "not-supplied"}, Unwitnessed: 4}, 2, 4, false},
		{"a trail of chained lines alone, the records", 6, auditCoverage{LegacyPrefix: 1, Chained: 5, Checkpointed: auditCoverageState{Status: "not-supplied"}, Unwitnessed: 5}, 2, 4, true},
		{"unchained lines after the last chained one are not counted", 7, auditCoverage{Chained: 5, Uncovered: 2, Checkpointed: auditCoverageState{Status: "not-supplied"}, Unwitnessed: 5}, 3, 2, true},
		{"a record past the last chained one, none", 3, auditCoverage{Chained: 3, Checkpointed: auditCoverageState{Status: "not-supplied"}, Unwitnessed: 3}, 5, 0, true},
	} {
		count, records := chainedAfter(&auditReport{Lines: c.lines, Coverage: c.coverage}, c.through)
		if count != c.count || records != c.records {
			t.Errorf("%s: %d, records %v; want %d, records %v", c.name, count, records, c.count, c.records)
		}
	}
}

// repairedReport is what runtime 0.27.1 reports over a trail of three
// records, a torn fourth line and its repair (the discontinuity at line 5),
// with the first three checkpoints held: four chained records, one of them
// unwitnessed, and a damaged line. Its identity is trail.
func repairedReport(t *testing.T, trail string) string {
	t.Helper()
	return edited(t, auditValidReport, func(report map[string]any) {
		report["status"], report["lines"], report["trail"] = "segmented", 5, trail
		coverage := member(report, "coverage")
		coverage["chained"], coverage["damaged"], coverage["witnessed"], coverage["unwitnessed"] = 4, 1, 3, 1
		coverage["checkpointed"] = map[string]any{"status": "through", "through": 3}
		report["segments"] = []any{map[string]any{"firstLine": 1, "lastLine": 3}, map[string]any{"firstLine": 5, "lastLine": 5}}
		report["segmentsTotal"] = 2
		report["discontinuities"] = []any{map[string]any{"line": 5, "reason": incompleteLastLine, "damagedLine": 4, "bytes": len(tornBytes), "digest": "sha256:" + strings.Repeat("ab", 32)}}
		report["discontinuitiesTotal"] = 1
	})
}

// **Since each hand-over, from the report.** Holder A was handed the
// checkpoints through record 3, holder C through record 2, and the trail was
// then repaired: the report's held checkpoints reach record 3. The decision
// record says one record since A's, the runtime's count, and three lines
// since C's, as lines. A report of another trail than the one whose
// checkpoints were passed says nothing since either; and the holders' answer
// gives the lines since, never records.
func TestTheRecordsSinceAHandOverAreTheReports(t *testing.T) {
	rig := newHandoverRig(t, holderA, holderC)
	rig.addHolder(t, "Auditor", "e-mail")
	rig.addHolder(t, "Regulator", "portal")
	rig.chain(t, chainOf(handoverTrail, 1, 2))
	header, _ := rig.downloaded(t, holderC)
	rig.confirmed(t, holderC, header)
	rig.chain(t, chainOf(handoverTrail, 1, 2, 3))
	header, _ = rig.downloaded(t, holderA)
	rig.confirmed(t, holderA, header)
	rig.chain(t, chainOf(handoverTrail, 1, 2, 3, 5))
	rig.answers(t, 0, repairedReport(t, handoverTrail))
	_, answer, refusal := readAudit(t, rig.ts, "")
	one, three := int64(1), int64(3)
	want := []handedSince{{Holder: holderA, Trail: handoverTrail, Through: 3, Records: &one}, {Holder: holderC, Trail: handoverTrail, Through: 2, Lines: &three}}
	if answer.Expected != 2 || !sameSince(answer.Since, want) {
		t.Errorf("the decision record answered %+v %q, want since %+v", answer, refusal, want)
	}
	if listed := rig.holders(t); since(holderAt(listed, 0).Trails[handoverTrail]) != 2 || since(holderAt(listed, 1).Trails[handoverTrail]) != 3 {
		t.Errorf("the holders answered %+v", listed)
	}
	rig.answers(t, 0, repairedReport(t, movedTrail))
	if _, answer, _ := readAudit(t, rig.ts, ""); answer.Expected != 2 || answer.Since != nil {
		t.Errorf("with a report of another trail the decision record answered %+v", answer.Since)
	}
}

// sameSince is whether two lists of counts since a hand-over say the same.
func sameSince(got, want []handedSince) bool {
	same := func(a, b *int64) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
	return slices.EqualFunc(got, want, func(a, b handedSince) bool {
		return a.Holder == b.Holder && a.Trail == b.Trail && a.Through == b.Through && same(a.Records, b.Records) && same(a.Lines, b.Lines)
	})
}

// **Pending a stamp, from the report.** Over the repaired trail stamped
// through record 3: one record pending, the runtime's count, where the held
// checkpoints reach that record; and two lines, as lines, where no held
// checkpoint is passed.
func TestTheRecordsPendingAStampAfterARepairAreTheReports(t *testing.T) {
	r := newStampRig(t)
	r.set(t, r.proposal(nil))
	stamped := edited(t, repairedReport(t, handoverTrail), func(report map[string]any) {
		member(report, "coverage")["stamped"] = map[string]any{"status": "through", "through": 3}
		report["stamps"] = map[string]any{"lines": 1, "unreadable": 0, "trusted": 1, "revocationChecked": 0, "revocationNotChecked": 1, "coveredBy": "2026-10-07T13:29:22Z",
			"lag": map[string]any{"records": 3, "maxSeconds": 9.5, "maxSequence": 1, "minSeconds": 2.5, "minSequence": 3, "atAfterStamp": false, "atUnreadable": 0}}
	})
	r.answers(t, 0, stamped)
	if got := stampingOf(t, r.ts, ""); got.Pending == nil || *got.Pending != 1 || got.PendingLines != nil {
		t.Errorf("with the held checkpoints reaching the record stamped the decision record says %+v", got)
	}
	r.answers(t, 0, edited(t, stamped, func(report map[string]any) {
		coverage := member(report, "coverage")
		coverage["checkpointed"], coverage["witnessed"], coverage["unwitnessed"] = map[string]any{"status": "not-supplied"}, 0, 4
	}))
	if got := stampingOf(t, r.ts, ""); got.Pending != nil || got.PendingLines == nil || *got.PendingLines != 2 {
		t.Errorf("with no held checkpoint the decision record says %+v", got)
	}
}

// **With the runtime: three records, retained checkpoints, a torn fourth
// line, a repair.** On a desk Desk made, with an authority whose root is the
// stand-in's, three deciding runs are recorded and stamped, and their
// checkpoints are handed over to a holder and confirmed. The trail's last
// line is then cut and repaired. The runtime's report: four chained records,
// one of them unwitnessed, one damaged line, stamped through record 3. The
// decision record agrees with it: one record since the hand-over and one
// record pending a stamp, not two. The holders' answer gives the two lines
// since, as lines. With the holder's file passed over, the report no longer
// says how many records follow record 3, and the pending count is two
// lines, given as lines.
func TestTheCountsAfterARepairAgreeWithTheRuntime(t *testing.T) {
	bin := requireBinary(t)
	t.Setenv("JPACK_CONFIG", "")
	const id = "a00000000000000000000000000000c7"
	fixStamping(t, id)
	fixDeskIDs(t, id)
	fixHandover(t, holderA)
	authority, err := newTestAuthority()
	if err != nil {
		t.Fatal(err)
	}
	tsa := httptest.NewServer(authority)
	t.Cleanup(tsa.Close)
	s, ts, _ := gatesServer(t, bin)
	row := createGatedDesk(t, ts)
	if row.ID != id {
		t.Fatalf("the desk was made as %s", row.ID)
	}
	if !slices.Contains(mustSchema(t, bin, row.Folder), "6") {
		t.Skip("this runtime has no audit commands")
	}
	config := strings.Replace(gatedConfigFor(t, s, row), `"packs":{}`, `"packs":{"alpha":{"path":"packs/a.json"}}`, 1)
	writeProject(t, row.Folder, map[string]string{"packs/a.json": reviewPack, "jpack.json": config})
	jpackIn(t, bin, row.Folder, "packs", "lock", "--config", "jpack.json", "--format", "json")
	for range 3 {
		if code, out := decidingRun(t, bin, row.Folder); code != 0 {
			t.Fatalf("a deciding run exited %d: %s", code, out)
		}
	}
	call := func(method, path string, body any) (int, []byte) {
		return reviewCall(t, ts, method, path, row.ID, body, bearer)
	}

	proposal := map[string]any{"authority": tsa.URL + "/", "roots": string(authority.rootPEM()), "policies": []string{"1.3.6.1.4.1.99999.1"}}
	if got := setOn(t, ts, row.ID, proposal, checkOn(t, ts, row.ID, proposal).Token); got.status != http.StatusOK {
		t.Fatalf("the settings were not kept: %d %s", got.status, got.data)
	}
	var run struct {
		Run stampRun `json:"run"`
	}
	if got := stampingCall(t, ts, "/api/audit/stamping/stamp", row.ID, map[string]any{}); got.status != http.StatusOK ||
		json.Unmarshal([]byte(got.data), &run) != nil || run.Run.Status != stampStamped || run.Run.Sequence != 3 {
		t.Fatalf("Stamp now answered %d %s", got.status, got.data)
	}

	if status, data := call("POST", "/api/audit/holders", map[string]string{"label": "Auditor", "channel": "e-mail"}); status != http.StatusCreated {
		t.Fatalf("adding a holder answered %d %s", status, data)
	}
	request, _ := http.NewRequest("GET", ts.URL+"/api/audit/checkpoints?holder="+holderA, nil)
	bearer(request)
	request.Header.Set("X-Jpack-Desk", row.ID)
	response, err := ts.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get(checkpointsThroughHeader) != "3" {
		t.Fatalf("the download answered %d %v", response.StatusCode, response.Header)
	}
	trail := response.Header.Get(checkpointsTrailHeader)
	if status, data := call("POST", "/api/audit/holders/"+holderA+"/confirm", confirmation(t, response.Header)); status != http.StatusOK {
		t.Fatalf("the confirmation answered %d %s", status, data)
	}

	cutTrail(t, row.Folder)
	_, torn, _ := readAudit(t, ts, row.ID)
	if torn.Repair == nil || torn.Repair.State != repairAvailable || torn.Repair.Line != 4 {
		t.Fatalf("after the cut the panel offers %+v", torn.Repair)
	}
	if got := repairOn(t, ts, row.ID, torn.Repair.Token); got.status != http.StatusOK || got.answer.Discontinuity.DamagedLine != 4 || got.answer.Discontinuity.Line != 5 {
		t.Fatalf("the repair answered %d %s", got.status, got.data)
	}
	lockFree(t, s.desks[row.ID])

	_, answer, refusal := readAudit(t, ts, row.ID)
	report := answer.Report
	if report == nil || report.Status != "segmented" || report.Trail != trail || report.Lines != 5 {
		t.Fatalf("after the repair the panel answered %+v %q", answer, refusal)
	}
	c := report.Coverage
	if c.Chained != 4 || c.Damaged != 1 || c.Witnessed != 3 || c.Unwitnessed != 1 ||
		c.Checkpointed != (auditCoverageState{Status: "through", Through: 3}) || c.Stamped != (auditCoverageState{Status: "through", Through: 3}) {
		t.Fatalf("the runtime's coverage is %+v", c)
	}
	one := int64(1)
	if !sameSince(answer.Since, []handedSince{{Holder: holderA, Trail: trail, Through: 3, Records: &one}}) {
		t.Errorf("since the hand-over the decision record says %+v, want the runtime's one unwitnessed record", answer.Since)
	}
	if answer.Stamping == nil || answer.Stamping.Pending == nil || *answer.Stamping.Pending != c.Unwitnessed || answer.Stamping.PendingLines != nil {
		t.Errorf("pending a stamp the decision record says %+v, want the runtime's one record", answer.Stamping)
	}
	status, data := call("GET", "/api/audit/holders", nil)
	var listed holdersAnswer
	if status != http.StatusOK || json.Unmarshal(data, &listed) != nil || listed.Trail == nil || listed.Trail.Sequence != 5 ||
		since(holderAt(listed, 0).Trails[trail]) != 2 || strings.Contains(string(data), `"unwitnessed"`) {
		t.Errorf("the holders answered %d %s", status, data)
	}

	// The holder's file cut short: passed over, and no held checkpoint
	// reaches the record stamped.
	held := filepath.Join(row.Folder, ".desk-private", "handover", holderA, trail+".jsonl")
	whole := readFile(t, held)
	if err := os.WriteFile(held, []byte(whole[:strings.Index(whole, "\n")+1]), 0o600); err != nil {
		t.Fatal(err)
	}
	_, answer, _ = readAudit(t, ts, row.ID)
	if answer.Report == nil || answer.Expected != 0 || answer.Since != nil || answer.Report.Coverage.Stamped.Through != 3 || answer.Stamping == nil ||
		answer.Stamping.Pending != nil || answer.Stamping.PendingLines == nil || *answer.Stamping.PendingLines != 2 {
		t.Errorf("with the holder's file passed over the decision record says %+v %+v", answer.Since, answer.Stamping)
	}
}
