package desk

// The second ADR-0010 line audit's stamping findings (issues #312 and #313):
// "the same checkpoint" is one rule, by trail, sequence and record digest,
// for the scheduler and for the decision record's word on the last run; and
// a run that did not stamp leaves no checkpoint known. A stand-in runtime,
// by absolute path, answers each command; the test that drives the published
// runtime skips without one.

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// **"The same checkpoint" is one rule** (issue #312). One trail, one
// sequence and one record digest, each known: another of any of the three,
// or a digest not known, is another checkpoint. The decision record's word
// on the last run holds it to that rule: where it named the report's head,
// by the head's digest, asking the runtime nothing; where it named an
// earlier record, by the runtime's one verification with the run's own
// checkpoint (issue #324). The auditor's scenario: record 3 rewritten in the
// same trail, and stamped from outside Desk, keeps the run's checkpoint
// unchecked.
func TestTheSameCheckpointIsOneRule(t *testing.T) {
	known := &checkpointHead{Identity: handoverTrail, Sequence: 3, Digest: standInDigest(3)}
	for name, tc := range map[string]struct {
		other *checkpointHead
		same  bool
	}{
		"the same":              {&checkpointHead{Identity: handoverTrail, Sequence: 3, Digest: standInDigest(3)}, true},
		"another trail":         {&checkpointHead{Identity: movedTrail, Sequence: 3, Digest: standInDigest(3)}, false},
		"another sequence":      {&checkpointHead{Identity: handoverTrail, Sequence: 4, Digest: standInDigest(3)}, false},
		"another record":        {&checkpointHead{Identity: handoverTrail, Sequence: 3, Digest: rewrittenDigest(3)}, false},
		"no digest known":       {&checkpointHead{Identity: handoverTrail, Sequence: 3}, false},
		"nothing to hold it to": {nil, false},
	} {
		if got := sameCheckpoint(known, tc.other); got != tc.same {
			t.Errorf("%s: same=%v", name, got)
		}
	}
	if sameCheckpoint(&checkpointHead{Identity: handoverTrail, Sequence: 3}, &checkpointHead{Identity: handoverTrail, Sequence: 3}) {
		t.Error("a checkpoint known without its digest is taken for one")
	}

	report := func(through int64, head *checkpointHead) *auditReport {
		r := &auditReport{Status: "valid", Trail: handoverTrail, Coverage: auditCoverage{Stamped: auditCoverageState{Status: "through", Through: through}}}
		if head != nil {
			r.head = checkpointLine{trail: head.Identity, sequence: head.Sequence, digest: head.Digest}
		}
		return r
	}
	run := func(sequence int64, digest string) auditStamping {
		return auditStamping{Passed: true, Last: &stampRun{Status: stampStamped, Trail: handoverTrail, Sequence: sequence, Digest: digest}}
	}
	head3 := &checkpointHead{Identity: handoverTrail, Sequence: 3, Digest: standInDigest(3)}
	for _, tc := range []struct {
		name      string
		view      auditStamping
		report    *auditReport
		confirmed bool
		why       string
		want      *stampChecked
		asked     bool
	}{
		{"the run named the head, and the head is its record", run(3, standInDigest(3)), report(3, head3), false, "", &stampChecked{Checked: true}, false},
		{"the auditor's scenario: the head at that sequence is another record", run(3, rewrittenDigest(3)), report(3, head3), false, "", &stampChecked{Reason: lastRunRewritten}, false},
		{"an earlier record the runtime confirms", run(2, standInDigest(2)), report(3, head3), true, "", &stampChecked{Checked: true}, true},
		{"an earlier record the runtime finds invalid with it", run(2, rewrittenDigest(2)), report(3, head3), false, lastRunExpectInvalid, &stampChecked{Reason: lastRunExpectInvalid}, true},
		{"an earlier record the runtime did not check", run(2, standInDigest(2)), report(3, head3), false, lastRunExpectUnchecked, &stampChecked{Reason: lastRunExpectUnchecked}, true},
		{"an earlier record no checked stamp reaches with it", run(2, standInDigest(2)), report(3, head3), false, lastRunExpectUncovered, &stampChecked{Reason: lastRunExpectUncovered}, true},
		{"an earlier record, a report with no head", run(2, standInDigest(2)), report(3, nil), false, "", &stampChecked{Reason: lastRunUnasked}, false},
		{"an earlier record, a report that is not valid", run(2, standInDigest(2)), func() *auditReport { r := report(3, head3); r.Status = "segmented"; return r }(), true, "", &stampChecked{Reason: lastRunExpectInvalid}, false},
		{"stamps that reach an earlier record", run(3, standInDigest(3)), report(2, head3), false, "", &stampChecked{Reason: fmt.Sprintf(lastRunBelow, 2)}, false},
		{"another trail's stamps", run(3, standInDigest(3)), &auditReport{Trail: movedTrail, Coverage: auditCoverage{Stamped: auditCoverageState{Status: "through", Through: 9}}}, false, "", &stampChecked{Reason: lastRunOtherTrail}, false},
		{"a report naming no trail", run(3, standInDigest(3)), &auditReport{Coverage: auditCoverage{Stamped: auditCoverageState{Status: "through", Through: 9}}}, false, "", &stampChecked{Reason: lastRunOtherTrail}, false},
		{"no stamp", run(3, standInDigest(3)), &auditReport{Trail: handoverTrail, Coverage: auditCoverage{Stamped: auditCoverageState{Status: "none"}}}, false, "", &stampChecked{Reason: lastRunNoStamp}, false},
		{"stamps not checked", run(3, standInDigest(3)), &auditReport{Trail: handoverTrail, Coverage: auditCoverage{Stamped: auditCoverageState{Status: "not-checked"}}}, false, "", &stampChecked{Reason: lastRunNotChecked}, false},
		{"no roots passed", auditStamping{Last: run(3, standInDigest(3)).Last}, report(3, head3), false, "", &stampChecked{Reason: lastRunNoRoots}, false},
		{"no report, roots passed", run(3, standInDigest(3)), nil, false, "", &stampChecked{Reason: lastRunNoTrail}, false},
		{"no report, no roots", auditStamping{Last: run(3, standInDigest(3)).Last}, nil, false, "", &stampChecked{Reason: lastRunNoTrail}, false},
		{"a run that named no checkpoint", auditStamping{Passed: true, Last: &stampRun{Status: stampRefused}}, report(3, head3), false, "", nil, false},
		{"no run", auditStamping{Passed: true}, report(3, head3), false, "", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			asked := false
			got := lastRunChecked(tc.view, tc.report, func(run *stampRun, head checkpointHead) (bool, string) {
				asked = true
				if run != tc.view.Last || head != *head3 {
					t.Errorf("the runtime was asked of %+v under %+v", run, head)
				}
				return tc.confirmed, tc.why
			})
			if (got == nil) != (tc.want == nil) || got != nil && *got != *tc.want {
				t.Errorf("the word on the last run is %+v, want %+v", got, tc.want)
			}
			if asked != tc.asked {
				t.Errorf("the runtime was asked: %v, want %v", asked, tc.asked)
			}
		})
	}
}

// runCheckpoint is a stamp run's checkpoint line, as the runtime prints it in
// its answer: what an older run's own verification is given (issue #324).
func runCheckpoint(trail string, sequence int64, digest string) string {
	return fmt.Sprintf(`{"checkpointVersion":"1","recordDigest":"%s","sequence":%d,"trail":"%s"}`, digest, sequence, trail)
}

// standInRun is a stamp run of the stand-in's checkpoint at sequence, with
// the record digest given, as `audit stamp` answered it.
func standInRun(sequence int64, digest string) stampRun {
	return stampRun{At: handoverNow, Status: stampStamped, Trail: handoverTrail, Sequence: sequence, Digest: digest, checkpoint: []byte(runCheckpoint(handoverTrail, sequence, digest))}
}

// What the stand-in's `audit verify` answers with an older run's own
// checkpoint, in runtime 0.27.1's form: the checkpoint held and a checked
// stamp reaching it (valid); the record there another now (invalid,
// checkpoint-record-mismatch); and a refusal (exit 4).
var (
	expectConfirmed = headAt(strings.NewReplacer(`"stamped":{"status":"through","through":2}`, `"stamped":{"status":"through","through":3}`,
		`"checkpointed":{"status":"not-supplied"},"witnessed":0,"unwitnessed":3`, `"checkpointed":{"status":"through","through":2},"witnessed":2,"unwitnessed":1`).Replace(stampsReport), 3)
	expectMismatch = strings.NewReplacer(`"status":"valid"`, `"status":"invalid"`, `"stamped":{"status":"through","through":2}`, `"stamped":{"status":"through","through":3}`,
		`"checkpointed":{"status":"not-supplied"}`, `"checkpointed":{"status":"failed"}`,
		`"findings":[],"findingsTotal":0`, `"findings":[{"name":"checkpoint-record-mismatch","line":2,"detail":"the record at sequence 2 is not the one the checkpoint names"}],"findingsTotal":1`).Replace(headAt(stampsReport, 3))
	expectRefused = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.27.1"},"command":"audit verify","status":"error","diagnostics":[{"code":"JPS-AUDIT-TRAIL-READ","codeStability":"provisional","layer":"operation","severity":"error","instancePath":"","message":"The project's trail could not be read."}]}`
)

// expectRuns is how many of calls ran `audit verify` with an older run's own
// checkpoint, and how many read checkpoints in batches.
func expectRuns(calls []string) (verified, batched int) {
	for _, call := range calls {
		switch {
		case strings.HasPrefix(call, "audit verify ") && strings.Contains(call, "/.expect-"):
			verified++
		case strings.HasPrefix(call, "audit checkpoint ") && strings.Contains(call, "--since"):
			batched++
		}
	}
	return verified, batched
}

// **The decision record says whether the last run's checkpoint is one a
// stamp the runtime checked reaches, and why not** (issue #312), through the
// stand-in. With the run's checkpoint the report's head, the head's digest
// decides, and nothing more is asked; with an earlier one, one runtime
// verification decides, given the run's own checkpoint line exactly as the
// runtime printed it (issue #324), and no checkpoint is read in batches. The
// auditor's scenario: the same trail and sequence holding another record,
// with the report's stamps reaching it, keeps the run unchecked. The report
// itself is the runtime's, unchanged.
func TestTheDecisionRecordHoldsTheLastRunToItsCheckpoint(t *testing.T) {
	fixStamping(t)
	r := newStampRig(t)
	r.set(t, r.proposal(nil))
	r.chainIs(t, handoverTrail, 3)
	r.answers(t, 0, headAt(strings.Replace(stampsReport, `"stamped":{"status":"through","through":2}`, `"stamped":{"status":"through","through":3}`, 1), 3))
	for _, tc := range []struct {
		name     string
		sequence int64
		digest   string
		code     int
		expect   string
		want     stampChecked
		asked    bool
	}{
		{"the head, its record", 3, standInDigest(3), 0, expectConfirmed, stampChecked{Checked: true}, false},
		{"the head, another record now", 3, rewrittenDigest(3), 0, expectConfirmed, stampChecked{Reason: lastRunRewritten}, false},
		{"an earlier record the runtime confirms", 2, standInDigest(2), 0, expectConfirmed, stampChecked{Checked: true}, true},
		{"an earlier record, another record now", 2, rewrittenDigest(2), 1, expectMismatch, stampChecked{Reason: lastRunExpectInvalid}, true},
		{"an earlier record the runtime does not check", 2, standInDigest(2), 4, expectRefused, stampChecked{Reason: lastRunExpectUnchecked}, true},
		{"an earlier record with no checked stamp reaching it", 2, standInDigest(2), 0, strings.Replace(expectConfirmed, `"stamped":{"status":"through","through":3}`, `"stamped":{"status":"through","through":1}`, 1), stampChecked{Reason: lastRunExpectUncovered}, true},
		{"an earlier record the held checkpoint does not reach", 2, standInDigest(2), 0, strings.Replace(expectConfirmed, `"checkpointed":{"status":"through","through":2}`, `"checkpointed":{"status":"through","through":1}`, 1), stampChecked{Reason: lastRunExpectUncovered}, true},
		// Review round 1 of #327, finding 6: the runtime confirms the run's
		// checkpoint in a history whose head is not the one the panel shows,
		// as where the trail was put back between the two questions.
		{"an earlier record confirmed in another history", 2, standInDigest(2), 0, strings.Replace(expectConfirmed, standInDigest(3), rewrittenDigest(3), 1), stampChecked{Reason: lastRunExpectMoved}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r.answersExpect(t, tc.code, tc.expect)
			// Each case is another trail behind one stand-in report, whose
			// head stays: what was confirmed for that head is let go.
			r.s.stamping.forgetConfirmed()
			run := standInRun(tc.sequence, tc.digest)
			r.s.stamping.record(run)
			r.ran(t)
			os.Remove(r.calls + ".expected")
			status, answer, data := readAudit(t, r.ts, "")
			if status != http.StatusOK || answer.Stamping == nil || answer.Stamping.LastChecked == nil || *answer.Stamping.LastChecked != tc.want {
				t.Fatalf("the decision record answered %d %+v %s", status, answer.Stamping, data)
			}
			if answer.Report == nil || answer.Report.Status != "valid" || answer.Report.Coverage.Stamped.Through != 3 {
				t.Errorf("the report is not the runtime's: %+v", answer.Report)
			}
			verified, batched := expectRuns(r.ran(t))
			if verified != map[bool]int{true: 1, false: 0}[tc.asked] || batched != 0 {
				t.Errorf("the runtime verified with the run's checkpoint %d times and read checkpoints in batches %d times, want %v and none", verified, batched, tc.asked)
			}
			if tc.asked {
				// **Exact bytes**: the run's checkpoint line as the runtime
				// printed it, and a newline.
				if got := readFile(t, r.calls+".expected"); got != string(run.checkpoint)+"\n" {
					t.Errorf("the runtime was given %q as the run's checkpoint", got)
				}
			}
			if leftovers, _ := filepath.Glob(filepath.Join(r.folder(t), ".expect-*")); len(leftovers) != 0 {
				t.Errorf("the run's checkpoint file was left: %v", leftovers)
			}
		})
	}
	t.Run("a run whose checkpoint line is not the runtime's", func(t *testing.T) {
		r.s.stamping.forgetConfirmed()
		run := standInRun(2, standInDigest(2))
		run.checkpoint = []byte(runCheckpoint(handoverTrail, 2, rewrittenDigest(2)))
		r.s.stamping.record(run)
		r.ran(t)
		if got := stampingOf(t, r.ts, ""); got.LastChecked == nil || *got.LastChecked != (stampChecked{Reason: lastRunUnasked}) {
			t.Errorf("the decision record says %+v", got.LastChecked)
		}
		if verified, _ := expectRuns(r.ran(t)); verified != 0 {
			t.Errorf("the runtime was given a line that is not the run's checkpoint")
		}
	})
}

// **A run that did not stamp leaves no checkpoint known** (issue #313, the
// auditor's scenario). The scheduler stamps record 5, and the next wake, the
// head where it was, asks nothing. The trail is then put back without its
// stamps, and a stamp asked for fails, the authority unreachable: the next
// scheduled wake asks the runtime again, which stamps.
func TestAFailedStampRunLeavesNoCheckpointKnown(t *testing.T) {
	w := fixStamping(t)
	r := newStampRig(t)
	r.set(t, r.proposal(nil))
	r.chainIs(t, handoverTrail, 5)
	r.stampsWith(t, 0, stampedAnswer(handoverTrail, 5))
	r.ran(t)
	stamps := []string{schemaCall, stampHeadCall, stampCall}
	asks := []string{schemaCall, stampHeadCall}
	w.at(t, time.Minute)
	if calls := r.ran(t); !slices.Equal(calls, stamps) {
		t.Fatalf("the first wake ran %q", calls)
	}
	w.at(t, time.Hour+time.Minute)
	if calls := r.ran(t); !slices.Equal(calls, asks) {
		t.Fatalf("with the head stamped a wake ran %q", calls)
	}
	r.stampsWith(t, 4, stampUnreachable)
	got := stampingCall(t, r.ts, "/api/audit/stamping/stamp", "", map[string]any{})
	var answered struct {
		Run stampRun `json:"run"`
	}
	if got.status != http.StatusOK || json.Unmarshal([]byte(got.data), &answered) != nil || answered.Run.Status != stampRefused {
		t.Fatalf("Stamp now answered %d %s", got.status, got.data)
	}
	r.stampsWith(t, 0, stampedAnswer(handoverTrail, 5))
	r.ran(t)
	w.at(t, 2*time.Hour+time.Minute)
	if calls := r.ran(t); !slices.Equal(calls, stamps) {
		t.Errorf("after a stamp that failed, the head where it was, a wake ran %q", calls)
	}
}

// chainedTrail is n chained records of trail, each line as a deciding run's
// is chained to the line before (the auditor's probe form), the record at
// each sequence in rewritten named for its run as given: its lines, and the
// checkpoint of its last, in the runtime's canonical form.
func chainedTrail(trail string, runs ...string) (string, string) {
	var data strings.Builder
	last := []byte{}
	for i, run := range runs {
		last = []byte(fmt.Sprintf(`{"trail":"%s","sequence":%d,"previous":"%s","at":"2026-10-07T00:00:00Z","run":"%s"}`, trail, i+1, sha256Digest(last), run))
		data.Write(last)
		data.WriteByte('\n')
	}
	return data.String(), fmt.Sprintf(`{"checkpointVersion":"1","recordDigest":"%s","sequence":%d,"trail":"%s"}`, sha256Digest(last), len(runs), trail)
}

// stampLine is a stamps file's line for checkpoint, its token made by
// authority, as a stamp made outside Desk would be.
func stampLine(t *testing.T, authority *testAuthority, checkpoint string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(checkpoint))
	token, err := authority.token(sum[:], nil)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf(`{"stampVersion":"1","checkpoint":%s,"token":"%s"}`, checkpoint, base64.StdEncoding.EncodeToString(token)) + "\n"
}

// **With the runtime: the last run's checkpoint held to its record, and a
// failed run that leaves nothing known** (issues #312 and #313, the
// auditor's scenarios, with the published runtime; skipped without one). A
// project whose path holds a space, a tab and U+2028 keeps a trail of three
// records. "Stamp now" stamps record 3 through a test authority, and the
// decision record says the run's checkpoint is one a stamp the runtime
// checked reaches; with a fourth record, still so, by the runtime's one
// verification with the run's checkpoint (issue #324). With records 3 and 4
// written again, and stamped from outside Desk at record 4, that verification
// refuses the run's checkpoint. The trail is then put back to its first two
// records and a third written again, stamped from outside Desk: the runtime
// checks that stamp and reaches record 3, and the run's checkpoint stays the
// authority's answer, since record 3 is another record now. "Stamp now" finds it stamped
// already; the stamps are then put back without it, and a stamp asked for
// fails, the authority silent: the next scheduled wake asks the runtime, and
// the authority stamps. No answer names a path.
func TestTheLastRunsCheckpointWithTheRuntime(t *testing.T) {
	bin := requireBinary(t)
	t.Setenv("JPACK_CONFIG", "")
	t.Setenv("JPACK_SIGNING_KEY", "")
	w := fixStamping(t)
	authority, err := newTestAuthority()
	if err != nil {
		t.Fatal(err)
	}
	tsa := httptest.NewServer(authority)
	t.Cleanup(tsa.Close)
	project := filepath.Join(t.TempDir(), "Top SECRET\tproject TAIL")
	trail := strings.Repeat("c", 32)
	lines, checkpoint3 := chainedTrail(trail, "first", "second", "third")
	writeProject(t, project, map[string]string{"jpack.json": auditedConfig, ".desk-private/audit/evaluations.jsonl": lines})
	if err := os.Chmod(filepath.Join(project, ".desk-private"), 0o700); err != nil {
		t.Fatal(err)
	}
	s, ts := startDesk(t, Config{ProjectDir: project, JpackBin: bin, Token: testToken, DeskConfigDir: filepath.Join(t.TempDir(), "Top SECRET\tconfig TAIL"), Logger: log.New(io.Discard, "", 0)})
	t.Cleanup(func() { s.Close() })
	t.Cleanup(ts.Close)
	if !slices.Contains(mustSchema(t, bin, project), "6") {
		t.Skip("this runtime has no audit commands")
	}
	proposal := map[string]any{"authority": tsa.URL + "/", "roots": string(authority.rootPEM())}
	setAuthority := func(t *testing.T) {
		t.Helper()
		if got := setOn(t, ts, "", proposal, checkOn(t, ts, "", proposal).Token); got.status != http.StatusOK {
			t.Fatalf("the settings were not kept: %d %s", got.status, got.data)
		}
	}
	setAuthority(t)
	stampNow := func(t *testing.T, want string) stampRun {
		t.Helper()
		got := stampingCall(t, ts, "/api/audit/stamping/stamp", "", map[string]any{})
		var answered struct {
			Run stampRun `json:"run"`
		}
		if got.status != http.StatusOK || json.Unmarshal([]byte(got.data), &answered) != nil || answered.Run.Status != want || strings.Contains(got.data, "SECRET") {
			t.Fatalf("Stamp now answered %d %s, want %s", got.status, got.data, want)
		}
		return answered.Run
	}
	lastChecked := func(t *testing.T) stampChecked {
		t.Helper()
		status, answer, data := readAudit(t, ts, "")
		if status != http.StatusOK || answer.Stamping == nil || answer.Stamping.LastChecked == nil || strings.Contains(data, "SECRET") {
			t.Fatalf("the decision record answered %d %+v %s", status, answer.Stamping, data)
		}
		return *answer.Stamping.LastChecked
	}

	run := stampNow(t, stampStamped)
	if run.Sequence != 3 || run.Trail != trail || authority.served() != 1 {
		t.Fatalf("Stamp now stamped %+v, the authority served %d", run, authority.served())
	}
	if got := lastChecked(t); got != (stampChecked{Checked: true}) {
		t.Errorf("with the run's checkpoint the head, the decision record says %+v", got)
	}
	evaluations := filepath.Join(project, ".desk-private", "audit", "evaluations.jsonl")
	four, _ := chainedTrail(trail, "first", "second", "third", "fourth")
	if err := os.WriteFile(evaluations, []byte(four), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := lastChecked(t); got != (stampChecked{Checked: true}) {
		t.Errorf("with the run's checkpoint an earlier record, the decision record says %+v", got)
	}
	// Records 3 and 4 written again, stamped from outside Desk at record 4:
	// the run named an earlier record, another one now.
	againFour, againCheckpoint4 := chainedTrail(trail, "first", "second", "third again", "fourth again")
	if err := os.WriteFile(evaluations, []byte(againFour), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".desk-private", "audit", "stamps.jsonl"), []byte(stampLine(t, authority, againCheckpoint4)), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := lastChecked(t); got != (stampChecked{Reason: lastRunExpectInvalid}) {
		t.Errorf("with the run's earlier record written again, the decision record says %+v", got)
	}

	// Put back to an earlier point, written since, and stamped from outside.
	rewritten, rewrittenCheckpoint := chainedTrail(trail, "first", "second", "third, written again")
	if rewrittenCheckpoint == checkpoint3 {
		t.Fatal("the rewritten record is the same record")
	}
	stamps := filepath.Join(project, ".desk-private", "audit", "stamps.jsonl")
	if err := os.WriteFile(evaluations, []byte(rewritten), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stamps, []byte(stampLine(t, authority, rewrittenCheckpoint)), 0o600); err != nil {
		t.Fatal(err)
	}
	status, answer, _ := readAudit(t, ts, "")
	if status != http.StatusOK || answer.Report == nil || answer.Report.Coverage.Stamped != (auditCoverageState{Status: "through", Through: 3}) {
		t.Fatalf("the runtime does not check the stamp made outside Desk: %+v", answer.Report)
	}
	if answer.Stamping == nil || answer.Stamping.LastChecked == nil || *answer.Stamping.LastChecked != (stampChecked{Reason: lastRunRewritten}) {
		t.Errorf("with record 3 another record now, the decision record says %+v", answer.Stamping)
	}

	// Stamped already; the stamps put back without it; a stamp that fails.
	stampNow(t, stampAlready)
	if err := os.Remove(stamps); err != nil {
		t.Fatal(err)
	}
	silent, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var held sync.Mutex
	var conns []net.Conn
	t.Cleanup(func() {
		silent.Close()
		held.Lock()
		defer held.Unlock()
		for _, conn := range conns {
			conn.Close()
		}
	})
	go func() {
		for {
			conn, err := silent.Accept()
			if err != nil {
				return
			}
			held.Lock()
			conns = append(conns, conn)
			held.Unlock()
		}
	}()
	was := stampTimeout
	stampTimeout = "2s"
	t.Cleanup(func() { stampTimeout = was })
	proposal["authority"] = "http://" + silent.Addr().String() + "/"
	setAuthority(t)
	stampNow(t, stampRefused)
	proposal["authority"] = tsa.URL + "/"
	setAuthority(t)
	served := authority.served()
	w.at(t, time.Hour)
	if authority.served() != served+1 {
		t.Errorf("after a stamp that failed, the scheduled wake had the authority serve %d, want %d", authority.served(), served+1)
	}
	if lines := strings.Count(readFile(t, stamps), "\n"); lines != 1 {
		t.Errorf("the stamps file holds %d lines", lines)
	}
}

// **An older run is checked by one runtime verification, over one trail**
// (issue #324, the third line audit's finding 6, and the second review of
// #317's swap; with the published runtime, skipped without one). Two
// histories, each the auditor's: a rewritten trail of 301 records whose head
// a stamp made outside Desk covers, with the run naming the original first
// record, which two batches of checkpoints, one from each history, once
// called checked; and a trail whose records 3 and 4 were written again, with
// a wrapper that puts the original back immediately before the runtime is
// asked about the run's checkpoint. Either way the runtime's one answer,
// given the run's checkpoint, is not "valid" with a stamp reaching it, the
// decision record does not say checked, and no checkpoint is read in
// batches.
func TestAnOlderRunIsCheckedByOneVerificationWithTheRuntime(t *testing.T) {
	bin := requireBinary(t)
	t.Setenv("JPACK_CONFIG", "")
	t.Setenv("JPACK_SIGNING_KEY", "")
	fixStamping(t)
	authority, err := newTestAuthority()
	if err != nil {
		t.Fatal(err)
	}
	trail := strings.Repeat("c", 32)
	type history struct {
		name                      string
		runCheckpoint             string
		now, putBack, stampedHead string
		through                   int64
	}
	runs := make([]string, 301)
	for i := range runs {
		runs[i] = "record"
	}
	original301, _ := chainedTrail(trail, runs...)
	_, first := chainedTrail(trail, runs[:1]...)
	runs[0] = "rewritten first"
	rewritten301, head301 := chainedTrail(trail, runs...)
	original4, _ := chainedTrail(trail, "first", "second", "third", "fourth")
	_, third := chainedTrail(trail, "first", "second", "third")
	rewritten4, head4 := chainedTrail(trail, "first", "second", "third again", "fourth again")
	for _, h := range []history{
		{"two histories across a batch's bound", first, rewritten301, original301, head301, 301},
		{"the original put back before the runtime is asked", third, rewritten4, original4, head4, 4},
	} {
		t.Run(h.name, func(t *testing.T) {
			named, _ := readCheckpointLine([]byte(h.runCheckpoint))
			project := filepath.Join(t.TempDir(), "project")
			writeProject(t, project, map[string]string{"jpack.json": auditedConfig, ".desk-private/audit/evaluations.jsonl": h.now,
				".desk-private/audit/stamps.jsonl": stampLine(t, authority, h.stampedHead)})
			if err := os.Chmod(filepath.Join(project, ".desk-private"), 0o700); err != nil {
				t.Fatal(err)
			}
			evaluations := filepath.Join(project, ".desk-private", "audit", "evaluations.jsonl")
			scratch := t.TempDir()
			armed, putBack, wrapper, calls := filepath.Join(scratch, "armed"), filepath.Join(scratch, "original.jsonl"), filepath.Join(scratch, "jpack"), filepath.Join(scratch, "calls")
			writeBare(t, putBack, h.putBack)
			// Every call is logged; the one given the run's checkpoint puts the
			// original trail back first, where armed.
			writeBare(t, wrapper, "#!/bin/sh\nprintf '%s\\n' \"$*\" >> "+shellQuote(calls)+"\ncase \"$*\" in\n*/.expect-*) if [ -e "+shellQuote(armed)+" ]; then cp "+shellQuote(putBack)+" "+shellQuote(evaluations)+"; rm "+shellQuote(armed)+"; fi;;\nesac\nexec "+shellQuote(bin)+" \"$@\"\n")
			if err := os.Chmod(wrapper, 0o700); err != nil {
				t.Fatal(err)
			}
			s, ts := startDesk(t, Config{ProjectDir: project, JpackBin: wrapper, Token: testToken, DeskConfigDir: filepath.Join(t.TempDir(), "config"), Logger: log.New(io.Discard, "", 0)})
			t.Cleanup(func() { s.Close() })
			t.Cleanup(ts.Close)
			if !slices.Contains(mustSchema(t, bin, project), "6") {
				t.Skip("this runtime has no audit commands")
			}
			proposal := map[string]any{"authority": "http://127.0.0.1:9/", "roots": string(authority.rootPEM())}
			if got := setOn(t, ts, "", proposal, checkOn(t, ts, "", proposal).Token); got.status != http.StatusOK {
				t.Fatalf("the settings were not kept: %d %s", got.status, got.data)
			}
			s.stamping.record(stampRun{At: handoverNow, Status: stampStamped, Trail: trail, Sequence: named.sequence, Digest: named.digest, checkpoint: []byte(h.runCheckpoint)})
			writeBare(t, armed, "")
			os.Remove(calls)
			status, answer, data := readAudit(t, ts, "")
			if status != http.StatusOK || answer.Report == nil || answer.Report.Coverage.Stamped != (auditCoverageState{Status: "through", Through: h.through}) {
				t.Fatalf("the check did not see the stamp at the head: %d %s", status, data)
			}
			if answer.Stamping == nil || answer.Stamping.LastChecked == nil || answer.Stamping.LastChecked.Checked {
				t.Errorf("the decision record says the run's checkpoint is checked: %+v", answer.Stamping)
			}
			// The runtime's own word, given the run's checkpoint, over the
			// trail as it is now.
			expect := filepath.Join(scratch, "expect.jsonl")
			writeBare(t, expect, h.runCheckpoint+"\n")
			roots := filepath.Join(scratch, "roots.pem")
			writeBare(t, roots, string(authority.rootPEM()))
			out, _ := exec.Command(bin, "audit", "verify", "--trail", evaluations, "--tsa-roots", roots, "--expect", expect, "--format", "json").CombinedOutput()
			if confirmed, _ := readAuditVerification(out, nil); confirmed.Report != nil && confirmed.Report.Status == "valid" {
				t.Errorf("the runtime, given the run's checkpoint, answers valid: %s", out)
			}
			logged := readFile(t, calls)
			if strings.Contains(logged, "--since") || !strings.Contains(logged, "/.expect-") {
				t.Errorf("the runtime was asked:\n%s", logged)
			}
		})
	}
}

// **A check read before a failed run does not put back what the run cleared**
// (second review of #317, finding 2, the reviewer's held check). A check with
// roots finds the head stamped and knows its digest. A second check is held
// after the runtime has answered it; meanwhile a stamp asked for fails, which
// leaves nothing known. The held check then completes: it does not put its
// digest back, and the next scheduled wake asks the runtime.
func TestACheckReadBeforeAFailedRunDoesNotRestoreIt(t *testing.T) {
	w := fixStamping(t)
	r := newStampRig(t)
	r.set(t, r.proposal(nil))
	r.chainIs(t, handoverTrail, 3)
	r.answers(t, 0, headAt(strings.Replace(stampsReport, `"stamped":{"status":"through","through":2}`, `"stamped":{"status":"through","through":3}`, 1), 3))
	// The stand-in, wrapped: armed, its audit verify answers, says so, and
	// holds its answer until let go.
	scratch := t.TempDir()
	armed, answered, letGo := filepath.Join(scratch, "armed"), filepath.Join(scratch, "answered"), filepath.Join(scratch, "go")
	if err := os.Rename(r.bin, r.bin+".stand-in"); err != nil {
		t.Fatal(err)
	}
	writeBare(t, r.bin, "#!/bin/sh\nif [ \"$1 $2\" = 'audit verify' ] && [ -e "+shellQuote(armed)+" ]; then rm "+shellQuote(armed)+"; out=$("+shellQuote(r.bin+".stand-in")+" \"$@\"); code=$?; : > "+shellQuote(answered)+"; while [ ! -e "+shellQuote(letGo)+" ]; do sleep 0.05; done; printf '%s\\n' \"$out\"; exit $code; fi\nexec "+shellQuote(r.bin+".stand-in")+" \"$@\"\n")
	if err := os.Chmod(r.bin, 0o700); err != nil {
		t.Fatal(err)
	}
	stampingOf(t, r.ts, "")
	if known := r.s.stamping.stamped; known == nil || known.Digest != standInDigest(3) {
		t.Fatalf("the first check knows %+v", known)
	}
	writeBare(t, armed, "")
	held := make(chan struct{})
	go func() {
		defer close(held)
		reviewCall(t, r.ts, "GET", "/api/audit/verify", "", nil, bearer)
	}()
	for i := 0; ; i++ {
		if _, err := os.Stat(answered); err == nil {
			break
		}
		if i > 400 {
			t.Fatal("the held check never ran")
		}
		time.Sleep(25 * time.Millisecond)
	}
	r.stampsWith(t, 4, stampUnreachable)
	if got := stampingCall(t, r.ts, "/api/audit/stamping/stamp", "", map[string]any{}); got.status != http.StatusOK || !strings.Contains(got.data, `"status":"refused"`) {
		t.Fatalf("Stamp now answered %d %s", got.status, got.data)
	}
	writeBare(t, letGo, "")
	<-held
	r.s.stamping.mu.Lock()
	known := r.s.stamping.stamped
	r.s.stamping.mu.Unlock()
	if known != nil && known.Digest != "" {
		t.Errorf("the held check put back %+v after the failed run", known)
	}
	r.stampsWith(t, 0, stampedAnswer(handoverTrail, 3))
	r.ran(t)
	w.at(t, time.Hour)
	if calls := r.ran(t); !slices.Equal(calls, []string{schemaCall, stampHeadCall, stampCall}) {
		t.Errorf("after the held check, the scheduled wake ran %q", calls)
	}
}

// **The scheduler skips a head only where the stamps file holds it, by its
// content** (second review of #317, finding 3). The scheduler stamps record
// 5, and the next wake, the stamps file holding that stamp, asks nothing. The
// stamps file is then removed, with no run that failed and no check: the
// next wake asks the runtime, which stamps. A stamps file holding a line for
// another record at that sequence, or one whose token is not base64, holds
// no stamp of the head either.
func TestTheSchedulerSkipsOnlyWhereTheStampsFileHoldsTheHead(t *testing.T) {
	w := fixStamping(t)
	r := newStampRig(t)
	r.set(t, r.proposal(nil))
	r.chainIs(t, handoverTrail, 5)
	r.stampsWith(t, 0, stampedAnswer(handoverTrail, 5))
	r.ran(t)
	stamps := []string{schemaCall, stampHeadCall, stampCall}
	asks := []string{schemaCall, stampHeadCall}
	w.at(t, time.Minute)
	if calls := r.ran(t); !slices.Equal(calls, stamps) {
		t.Fatalf("the first wake ran %q", calls)
	}
	w.at(t, time.Hour+time.Minute)
	if calls := r.ran(t); !slices.Equal(calls, asks) {
		t.Fatalf("with the head stamped, and the stamps file holding it, a wake ran %q", calls)
	}
	file := filepath.Join(r.project, ".desk-private", "audit", "stamps.jsonl")
	line := `{"stampVersion":"1","checkpoint":` + strings.TrimSuffix(checkpointOf(handoverTrail, 5), "\n") + `,"token":"AA=="}` + "\n"
	for i, then := range []struct {
		name  string
		write func()
	}{
		{"removed", func() {
			if err := os.Remove(file); err != nil {
				t.Fatal(err)
			}
		}},
		{"holding another record at the sequence", func() {
			writeBare(t, file, strings.Replace(line, standInDigest(5), rewrittenDigest(5), 1))
		}},
		{"holding a token that is not base64", func() { writeBare(t, file, strings.Replace(line, `"AA=="`, `"not base64"`, 1)) }},
	} {
		then.write()
		w.at(t, time.Duration(2*i+2)*time.Hour+time.Minute)
		if calls := r.ran(t); !slices.Equal(calls, stamps) {
			t.Errorf("with the stamps file %s, a wake ran %q", then.name, calls)
		}
		w.at(t, time.Duration(2*i+3)*time.Hour+time.Minute)
		if calls := r.ran(t); !slices.Equal(calls, asks) {
			t.Errorf("after the stamp, the stamps file %s, a wake ran %q", then.name, calls)
		}
	}
}

// **The runtime's word on an earlier record is kept for the head it was
// given under, and forgotten by every verification that does not confirm it**
// (issue #324; second review of #317, finding 6). The last run named record 2
// of three: a second load with the same head asks the runtime nothing more,
// and says the same; with records 2 and 3 written again, and so another
// head, the runtime is asked again, and refuses the run's checkpoint; and
// with the first head back, it is asked again, since the refusal forgot what
// was kept.
func TestTheEarlierRecordsAnswerIsKeptForItsHead(t *testing.T) {
	fixStamping(t)
	r := newStampRig(t)
	r.set(t, r.proposal(nil))
	r.chainIs(t, handoverTrail, 3)
	report := headAt(strings.Replace(stampsReport, `"stamped":{"status":"through","through":2}`, `"stamped":{"status":"through","through":3}`, 1), 3)
	r.answers(t, 0, report)
	r.answersExpect(t, 0, expectConfirmed)
	r.s.stamping.record(standInRun(2, standInDigest(2)))
	asked := func(t *testing.T) int {
		t.Helper()
		verified, batched := expectRuns(r.ran(t))
		if batched != 0 {
			t.Errorf("checkpoints were read in batches %d times", batched)
		}
		return verified
	}
	r.ran(t)
	for load := range 2 {
		if got := stampingOf(t, r.ts, ""); got.LastChecked == nil || *got.LastChecked != (stampChecked{Checked: true}) {
			t.Errorf("load %d says %+v", load+1, got.LastChecked)
		}
	}
	if n := asked(t); n != 1 {
		t.Errorf("two loads with one head asked the runtime %d times", n)
	}
	r.answers(t, 0, strings.Replace(report, standInDigest(3), rewrittenDigest(3), 1))
	r.answersExpect(t, 1, expectMismatch)
	if got := stampingOf(t, r.ts, ""); got.LastChecked == nil || *got.LastChecked != (stampChecked{Reason: lastRunExpectInvalid}) {
		t.Errorf("with another head, the decision record says %+v", got.LastChecked)
	}
	if n := asked(t); n != 1 {
		t.Errorf("with another head the runtime was asked %d times", n)
	}
	r.answers(t, 0, report)
	r.answersExpect(t, 0, expectConfirmed)
	if got := stampingOf(t, r.ts, ""); got.LastChecked == nil || *got.LastChecked != (stampChecked{Checked: true}) {
		t.Errorf("with the first head back, the decision record says %+v", got.LastChecked)
	}
	if n := asked(t); n != 1 {
		t.Errorf("with the first head back after a refusal, the runtime was asked %d times, want once: a refusal forgets what was kept", n)
	}
	// The decision record's own verification failing, under the same head,
	// forgets it too: the runtime is asked again once it passes.
	r.answers(t, 1, strings.NewReplacer(`"status":"valid"`, `"status":"invalid"`, `"findings":[],"findingsTotal":0`, `"findings":[{"name":"chain-link-mismatch","line":2,"detail":"a link does not hold"}],"findingsTotal":1`).Replace(report))
	if got := stampingOf(t, r.ts, ""); got.LastChecked == nil || *got.LastChecked != (stampChecked{Reason: lastRunExpectInvalid}) {
		t.Errorf("under a report that is not valid, the decision record says %+v", got.LastChecked)
	}
	if n := asked(t); n != 0 {
		t.Errorf("under a report that is not valid, the runtime was asked %d times", n)
	}
	r.answers(t, 0, report)
	stampingOf(t, r.ts, "")
	if n := asked(t); n != 1 {
		t.Errorf("after a verification that failed, the runtime was asked %d times, want once: a failure forgets what was kept", n)
	}
	// **A confirmation in another history is kept for no head** (review
	// round 1 of #327, finding 6): asked again on the next load.
	r.answersExpect(t, 0, strings.Replace(expectConfirmed, standInDigest(3), rewrittenDigest(3), 1))
	r.s.stamping.forgetConfirmed()
	for load := range 2 {
		if got := stampingOf(t, r.ts, ""); got.LastChecked == nil || *got.LastChecked != (stampChecked{Reason: lastRunExpectMoved}) {
			t.Errorf("load %d under another history says %+v", load+1, got.LastChecked)
		}
	}
	if n := asked(t); n != 2 {
		t.Errorf("two loads under another history asked the runtime %d times, want twice", n)
	}
}

// **An older run's confirmation is kept only under the head it verified**
// (review round 1 of #327, finding 6, the reviewer's scenario, with the
// published runtime; skipped without one). The panel's report shows a
// rewritten history; the runtime, asked about the run's checkpoint, reads the
// original one, which holds it with a stamp reaching it. That answer is of
// another history than the panel shows: not checked, and kept for no head.
// With the rewritten history in place, the runtime refuses the checkpoint.
func TestAnOlderRunsConfirmationIsKeptOnlyUnderTheHeadItVerified(t *testing.T) {
	bin := requireBinary(t)
	t.Setenv("JPACK_CONFIG", "")
	t.Setenv("JPACK_SIGNING_KEY", "")
	s, _ := bareServer(t, filepath.Join(t.TempDir(), "project"), filepath.Join(t.TempDir(), "config"), "")
	s.cfg.JpackBin, s.stamping = bin, &stampScheduler{}
	authority, err := newTestAuthority()
	if err != nil {
		t.Fatal(err)
	}
	folder, err := s.openStamping(true)
	if err != nil {
		t.Fatal(err)
	}
	folder.Close()
	project, why := s.auditRuntime()
	if why != "" {
		t.Fatal(why)
	}
	original, originalHead := chainedTrail(fixtureTrail, "original first", "second", "third")
	_, runCheckpoint := chainedTrail(fixtureTrail, "original first")
	rewritten, rewrittenHead := chainedTrail(fixtureTrail, "rewritten first", "second", "third")
	panelHead, _ := readCheckpointLine([]byte(rewrittenHead))
	named, _ := readCheckpointLine([]byte(runCheckpoint))
	run := &stampRun{Status: stampStamped, Trail: named.trail, Sequence: named.sequence, Digest: named.digest, checkpoint: []byte(runCheckpoint)}
	head := checkpointHead{Identity: panelHead.trail, Sequence: panelHead.sequence, Digest: panelHead.digest}
	roots := filepath.Join(t.TempDir(), "roots.pem")
	writeBare(t, roots, string(authority.rootPEM()))
	args := []string{"--tsa-roots", roots}
	audit := filepath.Join(s.projectDir, ".desk-private", "audit")
	writeBare(t, filepath.Join(audit, "evaluations.jsonl"), original)
	writeBare(t, filepath.Join(audit, "stamps.jsonl"), stampLine(t, authority, originalHead))
	if checked, why := s.expectRunCheckpoint(context.Background(), project, run, head, args); checked || why != lastRunExpectMoved {
		t.Errorf("under another history the run is checked %v: %s", checked, why)
	}
	if s.stamping.confirmedAt(checkpointHead{Identity: run.Trail, Sequence: run.Sequence, Digest: run.Digest}, head) {
		t.Error("a confirmation in another history was kept for the panel's head")
	}
	writeBare(t, filepath.Join(audit, "evaluations.jsonl"), rewritten)
	writeBare(t, filepath.Join(audit, "stamps.jsonl"), stampLine(t, authority, rewrittenHead))
	if checked, why := s.expectRunCheckpoint(context.Background(), project, run, head, args); checked || why != lastRunExpectInvalid {
		t.Errorf("under the rewritten history the run is checked %v: %s", checked, why)
	}
}
