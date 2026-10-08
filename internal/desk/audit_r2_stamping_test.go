package desk

// The second ADR-0010 line audit's stamping findings (issues #312 and #313):
// "the same checkpoint" is one rule, by trail, sequence and record digest,
// for the scheduler and for the decision record's word on the last run; and
// a run that did not stamp leaves no checkpoint known. A stand-in runtime,
// by absolute path, answers each command; the test that drives the published
// runtime skips without one.

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
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
// earlier record, by the runtime's checkpoint of the record there now. The
// auditor's scenario: record 3 rewritten in the same trail, and stamped from
// outside Desk, keeps the run's checkpoint unchecked.
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
		r := &auditReport{Trail: handoverTrail, Coverage: auditCoverage{Stamped: auditCoverageState{Status: "through", Through: through}}}
		if head != nil {
			r.head = checkpointLine{trail: head.Identity, sequence: head.Sequence, digest: head.Digest}
		}
		return r
	}
	run := func(sequence int64, digest string) auditStamping {
		return auditStamping{Passed: true, Last: &stampRun{Status: stampStamped, Trail: handoverTrail, Sequence: sequence, Digest: digest}}
	}
	head3 := &checkpointHead{Identity: handoverTrail, Sequence: 3, Digest: standInDigest(3)}
	at2 := &checkpointHead{Identity: handoverTrail, Sequence: 2, Digest: standInDigest(2)}
	for _, tc := range []struct {
		name   string
		view   auditStamping
		report *auditReport
		at     *checkpointHead
		atErr  error
		want   *stampChecked
		asked  bool
	}{
		{"the run named the head, and the head is its record", run(3, standInDigest(3)), report(3, head3), nil, nil, &stampChecked{Checked: true}, false},
		{"the auditor's scenario: the head at that sequence is another record", run(3, rewrittenDigest(3)), report(3, head3), nil, nil, &stampChecked{Reason: lastRunRewritten}, false},
		{"an earlier record, still the run's", run(2, standInDigest(2)), report(3, head3), at2, nil, &stampChecked{Checked: true}, true},
		{"an earlier record, rewritten since", run(2, rewrittenDigest(2)), report(3, head3), at2, nil, &stampChecked{Reason: lastRunRewritten}, true},
		{"an earlier record no longer chained", run(2, standInDigest(2)), report(3, head3), nil, errCheckpointsChanged, &stampChecked{Reason: lastRunGone}, true},
		{"an earlier record the runtime could not be asked of", run(2, standInDigest(2)), report(3, head3), nil, errors.New("exit status 4"), &stampChecked{Reason: lastRunUnasked}, true},
		{"stamps that reach an earlier record", run(3, standInDigest(3)), report(2, head3), nil, nil, &stampChecked{Reason: fmt.Sprintf(lastRunBelow, 2)}, false},
		{"another trail's stamps", run(3, standInDigest(3)), &auditReport{Trail: movedTrail, Coverage: auditCoverage{Stamped: auditCoverageState{Status: "through", Through: 9}}}, nil, nil, &stampChecked{Reason: lastRunOtherTrail}, false},
		{"a report naming no trail", run(3, standInDigest(3)), &auditReport{Coverage: auditCoverage{Stamped: auditCoverageState{Status: "through", Through: 9}}}, nil, nil, &stampChecked{Reason: lastRunOtherTrail}, false},
		{"no stamp", run(3, standInDigest(3)), &auditReport{Trail: handoverTrail, Coverage: auditCoverage{Stamped: auditCoverageState{Status: "none"}}}, nil, nil, &stampChecked{Reason: lastRunNoStamp}, false},
		{"stamps not checked", run(3, standInDigest(3)), &auditReport{Trail: handoverTrail, Coverage: auditCoverage{Stamped: auditCoverageState{Status: "not-checked"}}}, nil, nil, &stampChecked{Reason: lastRunNotChecked}, false},
		{"no roots passed", auditStamping{Last: run(3, standInDigest(3)).Last}, report(3, head3), nil, nil, &stampChecked{Reason: lastRunNoRoots}, false},
		{"no report, roots passed", run(3, standInDigest(3)), nil, nil, nil, &stampChecked{Reason: lastRunNoTrail}, false},
		{"no report, no roots", auditStamping{Last: run(3, standInDigest(3)).Last}, nil, nil, nil, &stampChecked{Reason: lastRunNoTrail}, false},
		{"a run that named no checkpoint", auditStamping{Passed: true, Last: &stampRun{Status: stampRefused}}, report(3, head3), nil, nil, nil, false},
		{"no run", auditStamping{Passed: true}, report(3, head3), nil, nil, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			asked := false
			got := lastRunChecked(tc.view, tc.report, func(trail string, sequence int64) (*checkpointHead, error) {
				asked = true
				if trail != handoverTrail || sequence != tc.view.Last.Sequence {
					t.Errorf("the runtime was asked for record %d of %s", sequence, trail)
				}
				return tc.at, tc.atErr
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

// **The decision record says whether the last run's checkpoint is one a
// stamp the runtime checked reaches, and why not** (issue #312), through the
// stand-in. With the run's checkpoint the report's head, the head's digest
// decides, and nothing more is asked; with an earlier one, Desk asks the
// runtime for the checkpoint of the record there now, and that decides. The
// auditor's scenario: the same trail and sequence holding another record,
// with the report's stamps reaching it, keeps the run unchecked. The report
// itself is the runtime's, unchanged.
func TestTheDecisionRecordHoldsTheLastRunToItsCheckpoint(t *testing.T) {
	fixStamping(t)
	r := newStampRig(t)
	r.set(t, r.proposal(nil))
	r.chainIs(t, handoverTrail, 3)
	r.answers(t, 0, headAt(strings.Replace(stampsReport, `"stamped":{"status":"through","through":2}`, `"stamped":{"status":"through","through":3}`, 1), 3))
	since := "audit checkpoint --config jpack.json --since 1 --limit 300 [JPACK_CONFIG=unset]"
	for _, tc := range []struct {
		name     string
		sequence int64
		digest   string
		lines    string
		want     stampChecked
		asked    bool
	}{
		{"the head, its record", 3, standInDigest(3), chainOf(handoverTrail, 1, 2, 3), stampChecked{Checked: true}, false},
		{"the head, another record now", 3, rewrittenDigest(3), chainOf(handoverTrail, 1, 2, 3), stampChecked{Reason: lastRunRewritten}, false},
		{"an earlier record, its record", 2, standInDigest(2), chainOf(handoverTrail, 1, 2, 3), stampChecked{Checked: true}, true},
		{"an earlier record, another record now", 2, standInDigest(2),
			chainOf(handoverTrail, 1) + strings.Replace(checkpointOf(handoverTrail, 2), standInDigest(2), rewrittenDigest(2), 1) + chainOf(handoverTrail, 3), stampChecked{Reason: lastRunRewritten}, true},
		{"an earlier record no longer chained", 2, standInDigest(2), chainOf(handoverTrail, 1, 3), stampChecked{Reason: lastRunGone}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(r.calls+".lines", []byte(tc.lines), 0o600); err != nil {
				t.Fatal(err)
			}
			r.s.stamping.record(stampRun{At: handoverNow, Status: stampStamped, Trail: handoverTrail, Sequence: tc.sequence, Digest: tc.digest})
			r.ran(t)
			status, answer, data := readAudit(t, r.ts, "")
			if status != http.StatusOK || answer.Stamping == nil || answer.Stamping.LastChecked == nil || *answer.Stamping.LastChecked != tc.want {
				t.Fatalf("the decision record answered %d %+v %s", status, answer.Stamping, data)
			}
			if answer.Report == nil || answer.Report.Status != "valid" || answer.Report.Coverage.Stamped.Through != 3 {
				t.Errorf("the report is not the runtime's: %+v", answer.Report)
			}
			if asked := slices.Contains(r.ran(t), since); asked != tc.asked {
				t.Errorf("the runtime was asked for the checkpoint at record 2: %v, want %v", asked, tc.asked)
			}
		})
	}
	t.Run("an earlier record the runtime does not answer for", func(t *testing.T) {
		if err := os.WriteFile(r.calls+".since", []byte("exit 4\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Remove(r.calls + ".since") })
		r.s.stamping.record(stampRun{At: handoverNow, Status: stampStamped, Trail: handoverTrail, Sequence: 2, Digest: standInDigest(2)})
		if got := stampingOf(t, r.ts, ""); got.LastChecked == nil || *got.LastChecked != (stampChecked{Reason: lastRunUnasked}) {
			t.Errorf("the decision record says %+v", got.LastChecked)
		}
		if !strings.Contains(r.log(), "could not be asked of the runtime") {
			t.Errorf("the log does not say why: %s", r.log())
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
// checked reaches; with a fourth record, still so, asked of the runtime for
// record 3. The trail is then put back to its first two records and a third
// written again, stamped from outside Desk: the runtime checks that stamp
// and reaches record 3, and the run's checkpoint stays the authority's
// answer, since record 3 is another record now. "Stamp now" finds it stamped
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
