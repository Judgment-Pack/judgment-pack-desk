//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package desk

// Stamping (ADR-0010, section 3; question 5; delivery row 7). A stand-in
// runtime, by absolute path, answers `audit checkpoint` from a file of
// checkpoint lines, as the hand-over's tests have it, `audit verify` as the
// decision record's tests have it, and `audit stamp` from a file each test
// prepares; its answers are runtime 0.27.1's own, measured, with the trail's
// path shortened and, where a stand-in's checkpoints are named, their
// identity and digest. The scheduler's clock and its wakes are the tests':
// nothing here waits for time to pass. The last tests drive the published
// runtime against a stand-in authority that issues tokens it accepts
// (stamping_authority_test.go), and skip without one.

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// What runtime 0.27.1 printed for `audit stamp --config jpack.json --tsa
// <address> --timeout 15s --format json`, measured: a stamp (exit 0); a
// checkpoint stamped already (exit 0); a listener that never answers (exit 4,
// after the timeout); no authority (exit 3); a trail that fails a check (exit
// 1); and a project with no trail yet (exit 4).
const (
	stampUnreachable  = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.27.1"},"command":"audit stamp","status":"error","diagnostics":[{"code":"JPS-AUDIT-STAMP-UNREACHABLE","codeStability":"provisional","layer":"operation","severity":"error","instancePath":"","message":"the time-stamping authority could not be asked, or did not answer: the request was not answered. Nothing was written, and the trail and the decisions in it are as they were; asking again stamps the same checkpoint."}]}`
	stampNoAuthority  = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.27.1"},"command":"audit stamp","status":"error","diagnostics":[{"code":"JPS-AUDIT-STAMP-NO-AUTHORITY","codeStability":"provisional","layer":"operation","severity":"error","instancePath":"","message":"No time-stamping authority is named: the audit member has no timestampAuthority and --tsa is not given."}]}`
	stampRefusedTrail = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.27.1"},"command":"audit stamp","status":"error","diagnostics":[{"code":"JPS-AUDIT-STAMP-REFUSED","codeStability":"provisional","layer":"operation","severity":"error","instancePath":"","message":"The trail fails 1 check(s), the first incomplete-last-line at line 3, so its checkpoint is not stamped; jpack audit verify lists them."}]}`
	stampNoTrailYet   = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.27.1"},"command":"audit stamp","status":"error","diagnostics":[{"code":"JPS-AUDIT-TRAIL-READ","codeStability":"provisional","layer":"operation","severity":"error","instancePath":"","message":"The project's trail /project/.desk-private/audit/evaluations.jsonl does not exist yet: no record has been written."}]}`
)

// stampedAnswer is 0.27.1's answer for a stamp of the stand-in's checkpoint
// at sequence in trail; alreadyStamped its answer for one stamped already.
func stampedAnswer(trail string, sequence int) string {
	return fmt.Sprintf(`{"outputVersion":"2","tool":{"name":"jpack","version":"0.27.1"},"command":"audit stamp","status":"stamped","trailPath":"/project/.desk-private/audit/evaluations.jsonl","checkpoint":{"checkpointVersion":"1","recordDigest":"sha256:%064x","sequence":%d,"trail":"%s"},"stampedAt":"2026-10-07T13:29:07Z","existedBy":"2026-10-07T13:29:07Z","policy":"1.3.6.1.4.1.99999.1"}`, sequence, sequence, trail)
}

// standInDigest is the record digest the stand-in's checkpoint at sequence
// names, in its answers to `audit checkpoint` and `audit stamp` alike.
func standInDigest(sequence int) string {
	return fmt.Sprintf("sha256:%064x", sequence)
}

func alreadyStamped(trail string, sequence int) string {
	return fmt.Sprintf(`{"outputVersion":"2","tool":{"name":"jpack","version":"0.27.1"},"command":"audit stamp","status":"already-stamped","trailPath":"/project/.desk-private/audit/evaluations.jsonl","checkpoint":{"checkpointVersion":"1","recordDigest":"sha256:%064x","sequence":%d,"trail":"%s"}}`, sequence, sequence, trail)
}

// stampsReport is what 0.27.1 printed for `audit verify --tsa-roots <file>`
// over a trail of three records whose first two a trusted stamp covers,
// measured, with the trail's path shortened and its identity the stand-in's.
const stampsReport = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.27.1"},"command":"audit verify","trailPath":"/project/.desk-private/audit/evaluations.jsonl","snapshotBetweenWrites":true,"status":"valid","scope":"one-supplied-chain","lines":3,"bytes":3533,"trail":"` + handoverTrail + `","head":{"checkpointVersion":"1","recordDigest":"sha256:607b98c9595baaa5154fc08e58f3b452084a6fdcf06fe71e49a00fa06556d3e3","sequence":3,"trail":"` + handoverTrail + `"},"coverage":{"legacyPrefix":0,"chained":3,"unchained":0,"uncovered":0,"damaged":0,"signed":{"status":"not-checked","detail":"no public key was supplied"},"signedRecords":0,"unsignedRecords":0,"checkpointed":{"status":"not-supplied"},"witnessed":0,"unwitnessed":3,"stamped":{"status":"through","through":2}},"segments":[{"firstLine":1,"lastLine":3}],"segmentsTotal":1,"discontinuities":[],"discontinuitiesTotal":0,"stamps":{"lines":2,"unreadable":0,"trusted":2,"revocationChecked":0,"revocationNotChecked":2,"coveredBy":"2026-10-07T13:29:22Z","lag":{"records":2,"maxSeconds":14.607222481,"maxSequence":2,"minSeconds":10.92565753,"minSequence":1,"atAfterStamp":false,"atUnreadable":0}},"findings":[],"findingsTotal":0,"establishes":["The chained lines are consistent with one another: no line before the last was edited, inserted, deleted or moved without breaking a link, and the lines before the first chained line are the block its previous commits to.","Lines 1 to 2 existed by 2026-10-07T13:29:22Z, as a time-stamping authority under a root supplied attests: the time it states, plus the accuracy it states."],"doesNotEstablish":["That any record's at is true: it is the operator's clock.","That lines after 2 existed by any time: no trusted stamp covers them.","When any record was made: a stamp shows its checkpoint existed by the stamp's time, not how long before, so a record's at stays the operator's word; the lag between each record's at and the first stamp covering it is reported, and judging it is the reader's."]}`

// stampCall is the stamp the scheduler and "Stamp now" run, with the
// authority tests set.
const (
	testAuthorityAddress = "https://tsa.example/stamp"
	stampCall            = "audit stamp --config jpack.json --tsa " + testAuthorityAddress + " --timeout 15s --format json [JPACK_CONFIG=unset]"
	stampHeadCall        = "audit checkpoint --config jpack.json --format json [JPACK_CONFIG=unset]"
)

// withStamp adds `audit stamp` to the stand-in at rig.bin: it prints
// `<calls>.stamp` and exits with `<calls>.stamp.exit`, where they exist
// (`stampsWith`), and otherwise exits 64.
func withStamp(t *testing.T, rig *auditRig) {
	t.Helper()
	script, err := os.ReadFile(rig.bin)
	if err != nil {
		t.Fatal(err)
	}
	// A stamp, or a checkpoint stamped already, leaves a line in the stamps
	// file naming its checkpoint, as the runtime's does: the scheduler skips
	// a head only where the stamps file holds one (second review of #317).
	stamp := "'audit stamp')\n" +
		"  if [ -e '" + rig.calls + ".stamp' ]; then\n" +
		"    while IFS= read -r line || [ -n \"$line\" ]; do printf '%s\\n' \"$line\"; done < '" + rig.calls + ".stamp'\n" +
		"    IFS= read -r code < '" + rig.calls + ".stamp.exit'\n" +
		"    cp=$(sed -n 's/.*\"checkpoint\":\\({[^}]*}\\).*/\\1/p' < '" + rig.calls + ".stamp')\n" +
		"    if [ \"$code\" = 0 ] && [ -n \"$cp\" ]; then mkdir -p .desk-private/audit; printf '{\"stampVersion\":\"1\",\"checkpoint\":%s,\"token\":\"AA==\"}\\n' \"$cp\" >> .desk-private/audit/stamps.jsonl; fi\n" +
		"    exit \"$code\"\n" +
		"  fi\n  exit 64\n  ;;\n"
	script = bytes.Replace(script, []byte("'packs lock')\n"), []byte(stamp+"'packs lock')\n"), 1)
	if err := os.WriteFile(rig.bin, script, 0o755); err != nil {
		t.Fatal(err)
	}
}

// stampsFileHolds appends to the project's stamps file a line naming the
// stand-in's checkpoint at sequence in trail, as a stamp of it leaves.
func (r *stampRig) stampsFileHolds(t *testing.T, trail string, sequence int) {
	t.Helper()
	path := filepath.Join(r.project, ".desk-private", "audit", "stamps.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString(`{"stampVersion":"1","checkpoint":` + strings.TrimSuffix(checkpointOf(trail, sequence), "\n") + `,"token":"AA=="}` + "\n"); err != nil {
		t.Fatal(err)
	}
}

// stampsWith sets what the stand-in's `audit stamp` prints, and its exit.
func (rig *auditRig) stampsWith(t *testing.T, code int, body string) {
	t.Helper()
	answerAt(t, rig.calls+".stamp", code, body)
}

// chainIs sets the stand-in's checkpoints: those of the records 1 to n of
// trail.
func (rig *auditRig) chainIs(t *testing.T, trail string, n int) {
	t.Helper()
	if err := os.WriteFile(rig.calls+".lines", []byte(chainOf(trail, upTo(n)...)), 0o600); err != nil {
		t.Fatal(err)
	}
}

// stampTime is the scheduler's clock in a test.
type stampTime struct {
	mu  sync.Mutex
	now time.Time
}

func (c *stampTime) read() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *stampTime) set(at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = at
}

// stampWaker wakes the scheduler of one desk started while it is set, and
// hears each wake acted on.
type stampWaker struct {
	clock *stampTime
	wake  chan time.Time
	woke  chan struct{}
}

// fixStamping fixes the scheduler's clock at handoverNow, and the wakes of
// the desk named only (the first desk started, where only is empty) to the
// test's own, for the servers started from now on. Every other desk's
// scheduler is never woken.
func fixStamping(t *testing.T, only ...string) *stampWaker {
	t.Helper()
	w := &stampWaker{clock: &stampTime{now: time.Unix(handoverNow, 0)}, wake: make(chan time.Time), woke: make(chan struct{}, 64)}
	wasClock, wasWake, wasWoke := stampClock, newStampWake, testHookStampWoke
	stampClock = w.clock.read
	var mu sync.Mutex
	wired := false
	newStampWake = func(name string) (<-chan time.Time, func()) {
		mu.Lock()
		defer mu.Unlock()
		if wired || len(only) > 0 && !slices.Contains(only, name) {
			return nil, func() {}
		}
		wired = true
		return w.wake, func() {}
	}
	testHookStampWoke = func() { w.woke <- struct{}{} }
	t.Cleanup(func() { stampClock, newStampWake, testHookStampWoke = wasClock, wasWake, wasWoke })
	return w
}

// send wakes the scheduler with the clock at handoverNow and after, and
// waits until it has acted; false where it did not wake, or did not act,
// within the bound.
func (w *stampWaker) send(after time.Duration) bool {
	at := time.Unix(handoverNow, 0).Add(after)
	w.clock.set(at)
	select {
	case w.wake <- at:
	case <-time.After(10 * time.Second):
		return false
	}
	select {
	case <-w.woke:
		return true
	case <-time.After(30 * time.Second):
		return false
	}
}

// at is send, and fails the test where the scheduler did not act.
func (w *stampWaker) at(t *testing.T, after time.Duration) {
	t.Helper()
	if !w.send(after) {
		t.Fatal("the scheduler did not wake and act")
	}
}

// stampRig is a startup desk over a project that keeps a trail, with the
// stand-in behind it and a test authority's root.
type stampRig struct {
	*auditRig
	s       *Server
	ts      *httptest.Server
	project string
	config  string
	logged  *lockedWriter
	roots   string
	crl     []byte
}

func newStampRig(t *testing.T) *stampRig {
	t.Helper()
	t.Setenv("JPACK_CONFIG", "")
	rig := newAuditRig(t, withAuditVersions)
	withCheckpoints(t, rig.bin, rig.calls)
	withStamp(t, rig)
	rig.answers(t, 0, auditValidReport)
	project := t.TempDir()
	writeProject(t, project, map[string]string{"jpack.json": auditedConfig})
	config := t.TempDir()
	logged := &lockedWriter{w: &bytes.Buffer{}}
	s, ts := startDesk(t, Config{ProjectDir: project, JpackBin: rig.bin, Token: testToken, DeskConfigDir: config, Logger: log.New(logged, "", 0)})
	t.Cleanup(func() { s.Close() })
	t.Cleanup(ts.Close)
	authority, err := newTestAuthority()
	if err != nil {
		t.Fatal(err)
	}
	crl, err := authority.crl(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return &stampRig{auditRig: rig, s: s, ts: ts, project: project, config: config, logged: logged, roots: string(authority.rootPEM()), crl: crl}
}

// lockedWriter is a log a scheduler's goroutine and the test both use.
type lockedWriter struct {
	mu sync.Mutex
	w  *bytes.Buffer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func (l *lockedWriter) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.String()
}

func (r *stampRig) log() string {
	return r.logged.String()
}

// proposal is the settings a test proposes: the test authority's address and
// root, and what changes.
func (r *stampRig) proposal(change map[string]any) map[string]any {
	proposal := map[string]any{"authority": testAuthorityAddress, "roots": r.roots}
	for name, value := range change {
		if value == nil {
			delete(proposal, name)
		} else {
			proposal[name] = value
		}
	}
	return proposal
}

// stampingAnswered is what a stamping route answered: its status, the body,
// and a refusal's sentence and code.
type stampingAnswered struct {
	status      int
	data        string
	error, code string
}

func stampingCall(t *testing.T, ts *httptest.Server, path, desk string, body any) stampingAnswered {
	t.Helper()
	status, data := reviewCall(t, ts, "POST", path, desk, body, bearer)
	var refusal struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	_ = json.Unmarshal(data, &refusal)
	return stampingAnswered{status: status, data: string(data), error: refusal.Error, code: refusal.Code}
}

// checkOn is a check of proposal on desk, which the test requires to hold.
func checkOn(t *testing.T, ts *httptest.Server, desk string, proposal map[string]any) stampingChecked {
	t.Helper()
	got := stampingCall(t, ts, "/api/audit/stamping/check", desk, proposal)
	var checked stampingChecked
	if got.status != http.StatusOK || json.Unmarshal([]byte(got.data), &checked) != nil || len(checked.Token) != stampingTokenLength {
		t.Fatalf("the check answered %d %s", got.status, got.data)
	}
	return checked
}

// setOn is a confirmation of proposal with token on desk.
func setOn(t *testing.T, ts *httptest.Server, desk string, proposal map[string]any, token string) stampingAnswered {
	t.Helper()
	body := map[string]any{"token": token}
	for name, value := range proposal {
		body[name] = value
	}
	return stampingCall(t, ts, "/api/audit/stamping", desk, body)
}

// set checks proposal and confirms it, and fails the test unless it is kept.
func (r *stampRig) set(t *testing.T, proposal map[string]any) {
	t.Helper()
	if got := setOn(t, r.ts, "", proposal, checkOn(t, r.ts, "", proposal).Token); got.status != http.StatusOK {
		t.Fatalf("the settings were not kept: %d %s", got.status, got.data)
	}
}

// stamping is the decision record's word on stamping, which the test
// requires it to give.
func stampingOf(t *testing.T, ts *httptest.Server, desk string) auditStamping {
	t.Helper()
	status, answer, refusal := readAudit(t, ts, desk)
	if status != http.StatusOK || answer.Stamping == nil {
		t.Fatalf("the decision record answered %d %+v %q, with no word on stamping", status, answer, refusal)
	}
	return *answer.Stamping
}

// folder is the stamping folder Desk keeps for the startup desk: under the
// hex SHA-256 of the project's resolved path, worked out here by hand.
func (r *stampRig) folder(t *testing.T) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(r.project)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(resolved))
	return filepath.Join(r.config, "stamping", hex.EncodeToString(sum[:]))
}

// fileDigest is the hexadecimal SHA-256 of data.
func fileDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// stampsRun is how many `audit stamp` calls the stand-in ran in calls.
func stampsRun(calls []string) int {
	n := 0
	for _, call := range calls {
		if strings.HasPrefix(call, "audit stamp ") {
			n++
		}
	}
	return n
}

// tsaArguments is the `--tsa-…` arguments of the `audit verify` among calls.
var tsaArgument = regexp.MustCompile(`--tsa-(?:roots|policy|crls) [^ ]+`)

func verifyTSA(t *testing.T, calls []string) []string {
	t.Helper()
	for _, call := range calls {
		if strings.HasPrefix(call, "audit verify ") {
			return tsaArgument.FindAllString(call, -1)
		}
	}
	t.Fatalf("no audit verify among %q", calls)
	return nil
}

// stampHold holds each stamp run at the moment before the runtime is asked,
// until it is freed: started hears each run arrive there.
type stampHold struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

// holdStampRuns holds the stamp runs of the servers of this test where the
// stamp is about to be asked for, with the settings read again and held
// (testHookStampRun). The test frees them in its cleanup, which runs before
// the server it made before this closes, so that a test that stops early
// never leaves a run held and a Close waiting for it.
func holdStampRuns(t *testing.T) *stampHold {
	t.Helper()
	return holdAt(t, &testHookStampRun)
}

// holdAt is holdStampRuns at the hook given; its cleanup also clears the
// hook, after the server closes.
func holdAt(t *testing.T, hook *func()) *stampHold {
	t.Helper()
	h := &stampHold{started: make(chan struct{}, 8), release: make(chan struct{})}
	*hook = func() { h.started <- struct{}{}; <-h.release }
	t.Cleanup(h.free)
	return h
}

// free lets every held run, and every later one, go on.
func (h *stampHold) free() { h.once.Do(func() { close(h.release) }) }

// arrived waits for a run to arrive at the hold, and fails the test where
// none does within the bound.
func (h *stampHold) arrived(t *testing.T) {
	t.Helper()
	select {
	case <-h.started:
	case <-time.After(10 * time.Second):
		t.Fatal("no stamp run started")
	}
}

/* The settings --------------------------------------------------------------- */

// **Kept by Desk, outside the project, under the desk's name, and held to
// their record.** No authority by default: the decision record passes no
// `--tsa-…` and says so. Once set, on the owner's confirmation, the settings
// are in Desk's configuration folder, `stamping/<SHA-256 of the project's
// path>/`, both folders 0700, every file 0600: the settings file, naming the
// authority, the interval, the policies and the SHA-256 of the roots and of
// the revocation list, and the roots and the list by those digests, as
// given. Nothing in the project changes, and jpack.json names no authority.
// The decision record shows the settings and passes them. A roots file
// changed since, a settings file open to others or not of Desk's shape, and
// a folder open to others are each "unread": nothing of them is passed, and
// the page is told why; a settings file that can be read at all can still be
// removed.
func TestStampingSettingsAreKeptByDeskOutsideTheProject(t *testing.T) {
	fixStamping(t)
	r := newStampRig(t)
	if got := stampingOf(t, r.ts, ""); got.State != stampingStateNone || got.Passed || got.RemoveToken != "" || got.Settings != nil {
		t.Errorf("with no authority set the decision record says %+v", got)
	}
	if args := verifyTSA(t, r.ran(t)); args != nil {
		t.Errorf("with no authority set the check was given %q", args)
	}
	crl := base64.StdEncoding.EncodeToString(r.crl)
	r.set(t, r.proposal(map[string]any{"intervalMinutes": 30, "policies": []string{"1.3.6.1.4.1.99999.1"}, "crls": []string{crl}}))

	folder := r.folder(t)
	for _, dir := range []string{filepath.Dir(folder), folder} {
		if info, err := os.Lstat(dir); err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Errorf("%s is %v, %v; want a folder, 0700", filepath.Base(dir), info, err)
		}
	}
	rootsName, crlName := "roots-"+fileDigest([]byte(r.roots))+".pem", "crl-"+fileDigest(r.crl)+".crl"
	want := map[string]string{rootsName: r.roots, crlName: string(r.crl)}
	for name, content := range want {
		if info, err := os.Lstat(filepath.Join(folder, name)); err != nil || info.Mode().Perm() != 0o600 || readFile(t, filepath.Join(folder, name)) != content {
			t.Errorf("%s is %v, %v, or not the bytes given", name, info, err)
		}
	}
	var file map[string]any
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(folder, "settings.json"))), &file); err != nil {
		t.Fatal(err)
	}
	if file["authority"] != testAuthorityAddress || file["intervalMinutes"] != float64(30) || file["roots"] != "sha256:"+fileDigest([]byte(r.roots)) ||
		fmt.Sprint(file["policies"]) != "[1.3.6.1.4.1.99999.1]" || fmt.Sprint(file["crls"]) != "[sha256:"+fileDigest(r.crl)+"]" || file["setAt"] != float64(handoverNow) {
		t.Errorf("the settings file is %v", file)
	}
	if info, err := os.Lstat(filepath.Join(folder, "settings.json")); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the settings file is %v, %v", info, err)
	}
	if got := readFile(t, filepath.Join(r.project, "jpack.json")); got != auditedConfig {
		t.Errorf("jpack.json changed: %s", got)
	}
	if entries, _ := os.ReadDir(r.project); len(entries) != 1 {
		t.Errorf("the project holds %v", entries)
	}

	got := stampingOf(t, r.ts, "")
	if got.State != stampingStateSet || !got.Passed || len(got.RemoveToken) != stampingTokenLength || got.Settings == nil ||
		got.Settings.Authority != testAuthorityAddress || got.Settings.IntervalMinutes != 30 || !slices.Equal(got.Settings.Policies, []string{"1.3.6.1.4.1.99999.1"}) ||
		len(got.Settings.Roots) != 1 || got.Settings.Roots[0].Subject != "CN=test time-stamping root" ||
		len(got.Settings.CRLs) != 1 || got.Settings.CRLs[0] != (stampingList{SHA256: "sha256:" + fileDigest(r.crl), Lists: 1}) {
		t.Errorf("the decision record says %+v", got)
	}
	wantArgs := []string{"--tsa-roots " + filepath.Join(folder, rootsName), "--tsa-policy 1.3.6.1.4.1.99999.1", "--tsa-crls " + filepath.Join(folder, crlName)}
	if args := verifyTSA(t, r.ran(t)); !slices.Equal(args, wantArgs) {
		t.Errorf("the check was given %q, want %q", args, wantArgs)
	}

	unread := func(t *testing.T, removable bool) {
		t.Helper()
		got := stampingOf(t, r.ts, "")
		if got.State != stampingStateUnread || got.Passed || got.Settings != nil || !strings.HasPrefix(got.Problem, "Desk could not read the stamping settings it keeps for this desk, so it uses none of them now: ") ||
			(got.RemoveToken != "") != removable {
			t.Errorf("the decision record says %+v", got)
		}
		if args := verifyTSA(t, r.ran(t)); args != nil {
			t.Errorf("settings Desk could not read gave the check %q", args)
		}
		if strings.Contains(mustJSON(got), r.config) {
			t.Errorf("the decision record names Desk's configuration folder: %s", mustJSON(got))
		}
	}
	rootsPath, settingsPath := filepath.Join(folder, rootsName), filepath.Join(folder, "settings.json")
	other, _ := newTestAuthority()
	if err := os.WriteFile(rootsPath, other.rootPEM(), 0o600); err != nil {
		t.Fatal(err)
	}
	unread(t, true)
	if err := os.WriteFile(rootsPath, []byte(r.roots), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := stampingOf(t, r.ts, ""); got.State != stampingStateSet {
		t.Fatalf("with the roots put back the decision record says %+v", got)
	}
	r.ran(t)
	settings := readFile(t, settingsPath)
	if err := os.Chmod(settingsPath, 0o644); err != nil {
		t.Fatal(err)
	}
	unread(t, false)
	if err := os.Chmod(settingsPath, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, []byte(strings.Replace(settings, `{"version"`, `{"extra":1,"version"`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	unread(t, true)
	// Roots the settings name by their very digest, holding no certificate
	// (review round 1): held to what the runtime reads, not to the digest
	// alone.
	noCertificate := []byte("not a certificate\n")
	written := filepath.Join(folder, "roots-"+fileDigest(noCertificate)+".pem")
	if err := os.WriteFile(written, noCertificate, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, []byte(strings.Replace(settings, "sha256:"+fileDigest([]byte(r.roots)), "sha256:"+fileDigest(noCertificate), 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	unread(t, true)
	if err := os.Remove(written); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	unread(t, false)
	if err := os.Chmod(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := stampingOf(t, r.ts, ""); got.State != stampingStateSet || !got.Passed {
		t.Errorf("with everything put back the decision record says %+v", got)
	}
}

// **A desk Desk made keeps its settings under its id**, apart from the
// startup desk's.
func TestEachDeskKeepsItsOwnStampingSettings(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	rig := newAuditRig(t, withAuditVersions)
	withCheckpoints(t, rig.bin, rig.calls)
	withStamp(t, rig)
	rig.answers(t, 0, auditValidReport)
	s, ts, _ := gatesServer(t, rig.bin)
	row := createSignedDesk(t, s, ts, rig.calls, "a0000000000000000000000000000007")
	authority, _ := newTestAuthority()
	proposal := map[string]any{"authority": testAuthorityAddress, "roots": string(authority.rootPEM())}
	if got := setOn(t, ts, row.ID, proposal, checkOn(t, ts, row.ID, proposal).Token); got.status != http.StatusOK {
		t.Fatalf("the made desk's settings were not kept: %d %s", got.status, got.data)
	}
	if _, err := os.Stat(filepath.Join(s.configDir, "stamping", row.ID, "settings.json")); err != nil {
		t.Errorf("the made desk's settings are not under its id: %v", err)
	}
	if got := stampingOf(t, ts, row.ID); got.State != stampingStateSet {
		t.Errorf("the made desk says %+v", got)
	}
	// The startup desk keeps none: the stamping folder holds the made
	// desk's alone.
	if entries, err := os.ReadDir(filepath.Join(s.configDir, "stamping")); err != nil || len(entries) != 1 || entries[0].Name() != row.ID {
		t.Errorf("the stamping folder holds %v, %v", entries, err)
	}
}

// **The confirmation is bound to the proposal, to this desk, to the
// settings as they were read, and to one attempt.** The same token again is
// refused as used; a token given for other settings, for another desk, for
// a proposal changed since, or before the settings changed, is refused as
// stale; a removal's token does not set, nor a setting's remove; and a token
// that is not one is refused before anything is read. Each refusal changes
// nothing.
func TestAStampingTokenIsBoundToWhatWasShown(t *testing.T) {
	r := newStampRig(t)
	proposal := r.proposal(nil)
	token := checkOn(t, r.ts, "", proposal).Token
	if got := setOn(t, r.ts, "", proposal, token); got.status != http.StatusOK {
		t.Fatalf("the settings were not kept: %d %s", got.status, got.data)
	}
	settingsPath := filepath.Join(r.folder(t), "settings.json")
	kept := readFile(t, settingsPath)
	stale := func(t *testing.T, got stampingAnswered, words string) {
		t.Helper()
		if got.status != http.StatusConflict || got.code != CodeStale || got.error != words || readFile(t, settingsPath) != kept {
			t.Errorf("answered %d %s; the settings are %s", got.status, got.data, readFile(t, settingsPath))
		}
	}
	// Replayed.
	stale(t, setOn(t, r.ts, "", proposal, token), stampingUsedWords)
	// For a proposal changed since.
	changed := r.proposal(map[string]any{"intervalMinutes": 90})
	stale(t, setOn(t, r.ts, "", changed, checkOn(t, r.ts, "", proposal).Token), stampingStaleWords)
	// Before the settings changed.
	early := checkOn(t, r.ts, "", changed).Token
	r.set(t, r.proposal(map[string]any{"intervalMinutes": 45}))
	kept = readFile(t, settingsPath)
	stale(t, setOn(t, r.ts, "", changed, early), stampingStaleWords)
	// Of another purpose: a removal's token over the same settings.
	removal := stampingOf(t, r.ts, "").RemoveToken
	stale(t, setOn(t, r.ts, "", proposal, removal), stampingStaleWords)
	// The right MAC of another purpose, for the same proposal.
	plan, _ := stampingProposal{Authority: testAuthorityAddress, Roots: r.roots}.plan()
	sum := sha256.Sum256([]byte(kept))
	purpose := r.s.stampingToken(stampingRemovePurpose, strings.Repeat("0", stampingNonceLength), plan.digest(), "sha256:"+hex.EncodeToString(sum[:]))
	stale(t, setOn(t, r.ts, "", proposal, purpose), stampingStaleWords)
	// The setting's own MAC, as it would be, keeps it: the check above is
	// the MAC's alone.
	same := r.s.stampingToken(stampingSetPurpose, strings.Repeat("1", stampingNonceLength), plan.digest(), "sha256:"+hex.EncodeToString(sum[:]))
	if got := setOn(t, r.ts, "", proposal, same); got.status != http.StatusOK {
		t.Errorf("a token of the setting's own MAC answered %d %s", got.status, got.data)
	}
	kept = readFile(t, settingsPath)
	// Another nonce before the same MAC.
	forged := strings.Repeat("e", stampingNonceLength) + checkOn(t, r.ts, "", proposal).Token[stampingNonceLength:]
	stale(t, setOn(t, r.ts, "", proposal, forged), stampingStaleWords)
	// The MAC another key makes over this desk's own proposal and settings.
	other := &Server{cfg: Config{deskID: r.s.cfg.deskID}, projectDir: r.s.projectDir}
	stale(t, setOn(t, r.ts, "", proposal, other.stampingToken(stampingSetPurpose, strings.Repeat("2", stampingNonceLength), plan.digest(), "sha256:"+fileDigest([]byte(kept)))), stampingStaleWords)
	// Not a token.
	for _, wrong := range []string{"", "ab", strings.Repeat("a", stampingTokenLength+2)} {
		if got := setOn(t, r.ts, "", proposal, wrong); got.status != http.StatusBadRequest || got.error != stampingConfirmWords {
			t.Errorf("token %q answered %d %s", wrong, got.status, got.data)
		}
	}

	// Another desk of the same Desk: its token is not this desk's, nor this
	// desk's its own.
	t.Setenv("JPACK_CONFIG", "")
	s2, ts2, _ := gatesServer(t, r.bin)
	row := createSignedDesk(t, s2, ts2, r.calls, "a0000000000000000000000000000008")
	theirs := checkOn(t, ts2, row.ID, proposal).Token
	stale(t, setOn(t, r.ts, "", proposal, theirs), stampingStaleWords)
	ours := checkOn(t, r.ts, "", proposal).Token
	if got := setOn(t, ts2, row.ID, proposal, ours); got.status != http.StatusConflict || got.code != CodeStale {
		t.Errorf("this desk's token on another answered %d %s", got.status, got.data)
	}
}

// **Removing the authority, on a token over the settings as shown.** A
// removal's token given before the settings changed is stale; the fresh one
// removes the settings file and the files it named, and keeps the trail's
// stamps file as it is; it is spent; and the decision record then says no
// authority is set, and passes nothing.
func TestRemovingTheAuthorityKeepsTheStamps(t *testing.T) {
	r := newStampRig(t)
	r.set(t, r.proposal(map[string]any{"crls": []string{base64.StdEncoding.EncodeToString(r.crl)}}))
	stamps := filepath.Join(r.project, ".desk-private", "audit", "stamps.jsonl")
	writeProject(t, r.project, map[string]string{".desk-private/audit/stamps.jsonl": "{\"kept\":true}\n"})
	early := stampingOf(t, r.ts, "").RemoveToken
	r.set(t, r.proposal(map[string]any{"intervalMinutes": 120}))
	if got := stampingCall(t, r.ts, "/api/audit/stamping/remove", "", map[string]string{"token": early}); got.status != http.StatusConflict || got.error != stampingStaleWords {
		t.Errorf("a removal's token from before a change answered %d %s", got.status, got.data)
	}
	token := stampingOf(t, r.ts, "").RemoveToken
	got := stampingCall(t, r.ts, "/api/audit/stamping/remove", "", map[string]string{"token": token})
	if got.status != http.StatusOK {
		t.Fatalf("the removal answered %d %s", got.status, got.data)
	}
	if entries, err := os.ReadDir(r.folder(t)); err != nil || len(entries) != 0 {
		t.Errorf("the stamping folder holds %v, %v", entries, err)
	}
	if readFile(t, stamps) != "{\"kept\":true}\n" {
		t.Error("the trail's stamps file changed")
	}
	if again := stampingCall(t, r.ts, "/api/audit/stamping/remove", "", map[string]string{"token": token}); again.status != http.StatusConflict || again.error != stampingUsedWords {
		t.Errorf("the removal's token again answered %d %s", again.status, again.data)
	}
	r.ran(t)
	if got := stampingOf(t, r.ts, ""); got.State != stampingStateNone || got.RemoveToken != "" {
		t.Errorf("after the removal the decision record says %+v", got)
	}
	if args := verifyTSA(t, r.ran(t)); args != nil {
		t.Errorf("after the removal the check was given %q", args)
	}
	if got := stampingCall(t, r.ts, "/api/audit/stamping/remove", "", map[string]string{"token": "ab"}); got.status != http.StatusBadRequest || got.error != stampingRemoveWords {
		t.Errorf("a removal with no token answered %d %s", got.status, got.data)
	}
}

// **A proposal is held to every bound, on both sides of each.** The
// authority: http or https, a host, no user, password, fragment, space or
// control character, at most 2048 bytes. The interval: 5 to 1440 minutes,
// 60 where none is given. The roots and each revocation list: at most
// 262144 bytes, which the reader reads, at least one certificate or list.
// At most 8 policies and 4 lists, each once. Each refusal says which rule,
// and keeps nothing.
func TestAStampingProposalIsHeldToItsBounds(t *testing.T) {
	r := newStampRig(t)
	pad := func(n int, tail string) string { return strings.Repeat("\n", n-len(tail)) + tail }
	crlPEM := string(r.crl)
	b64 := func(data string) string { return base64.StdEncoding.EncodeToString([]byte(data)) }
	authority, _ := newTestAuthority()
	otherCRL, _ := authority.crl(time.Now())
	host := "https://tsa.example/"
	long := host + strings.Repeat("a", 2048-len(host))
	oid64 := strings.Repeat("1.", 31) + "11"
	for _, c := range []struct {
		name   string
		change map[string]any
		words  string
	}{
		{"an ftp address", map[string]any{"authority": "ftp://tsa.example/"}, stampingAuthorityWords},
		{"no host", map[string]any{"authority": "https:///stamp"}, stampingAuthorityWords},
		{"a user", map[string]any{"authority": "https://user:secret@tsa.example/"}, stampingAuthorityWords},
		{"a fragment", map[string]any{"authority": "https://tsa.example/#part"}, stampingAuthorityWords},
		{"a space", map[string]any{"authority": "https://tsa.example/a b"}, stampingAuthorityWords},
		{"a control character", map[string]any{"authority": "https://tsa.example/\u0007"}, stampingAuthorityWords},
		{"a right-to-left override", map[string]any{"authority": "https://tsa.example/\u202e"}, stampingAuthorityWords},
		{"a zero-width space", map[string]any{"authority": "https://tsa.\u200bexample/"}, stampingAuthorityWords},
		{"an address past its bound", map[string]any{"authority": long + "a"}, stampingAuthorityWords},
		{"no address", map[string]any{"authority": nil}, stampingAuthorityWords},
		{"an interval of 4 minutes", map[string]any{"intervalMinutes": 4}, stampingIntervalWords},
		{"an interval of 1441 minutes", map[string]any{"intervalMinutes": 1441}, stampingIntervalWords},
		{"no roots", map[string]any{"roots": nil}, stampingRootsWords},
		{"roots of white space alone", map[string]any{"roots": "\n \n"}, stampingRootsWords},
		{"roots with a private key", map[string]any{"roots": r.roots + privateKeyPEM(t)}, stampingRootsWords},
		{"roots with text outside the blocks", map[string]any{"roots": "# the authority's root\n" + r.roots}, stampingRootsWords},
		{"roots with a block's headers", map[string]any{"roots": withHeaders(t, r.roots)}, stampingRootsWords},
		{"roots that are not PEM", map[string]any{"roots": "not a certificate"}, stampingRootsWords},
		{"roots past their bound", map[string]any{"roots": pad(stampingBoundBytes+1, r.roots)}, stampingRootsWords},
		{"a broken certificate", map[string]any{"roots": "-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n"}, stampingRootsWords},
		{"nine policies", map[string]any{"policies": []string{"1.1", "1.2", "1.3", "1.4", "1.5", "1.6", "1.7", "1.8", "1.9"}}, stampingPoliciesWords},
		{"one arc", map[string]any{"policies": []string{"1"}}, stampingPoliciesWords},
		{"a leading zero", map[string]any{"policies": []string{"1.02"}}, stampingPoliciesWords},
		{"a sign", map[string]any{"policies": []string{"1.+2"}}, stampingPoliciesWords},
		{"a policy twice", map[string]any{"policies": []string{"1.2", "1.2"}}, stampingPoliciesWords},
		{"a policy past its bound", map[string]any{"policies": []string{oid64 + "1"}}, stampingPoliciesWords},
		{"five lists", map[string]any{"crls": []string{b64(crlPEM), b64(string(otherCRL)), b64(string(derOf(t, r.crl))), b64(string(otherCRLOf(t))), b64(string(otherCRLOf(t)))}}, stampingCRLsWords},
		{"a list twice", map[string]any{"crls": []string{b64(crlPEM), b64(crlPEM)}}, stampingCRLsWords},
		{"a list that is not one", map[string]any{"crls": []string{b64("not a list")}}, stampingCRLsWords},
		{"a list with text outside the blocks", map[string]any{"crls": []string{b64(crlPEM + "# kept beside it\n")}}, stampingCRLsWords},
		{"a list not in base64", map[string]any{"crls": []string{"%%%"}}, stampingCRLsWords},
		{"a list past its bound", map[string]any{"crls": []string{b64(pad(stampingBoundBytes+1, crlPEM))}}, stampingCRLsWords},
	} {
		got := stampingCall(t, r.ts, "/api/audit/stamping/check", "", r.proposal(c.change))
		if got.status != http.StatusBadRequest || got.error != c.words {
			t.Errorf("%s: answered %d %s", c.name, got.status, got.data)
		}
	}
	for _, c := range []struct {
		name     string
		change   map[string]any
		interval int64
	}{
		{"an address at its bound", map[string]any{"authority": long}, 60},
		{"an http address", map[string]any{"authority": "http://127.0.0.1:9/"}, 60},
		{"an interval of 5 minutes", map[string]any{"intervalMinutes": 5}, 5},
		{"an interval of 1440 minutes", map[string]any{"intervalMinutes": 1440}, 1440},
		{"roots at their bound", map[string]any{"roots": pad(stampingBoundBytes, r.roots)}, 60},
		{"eight policies, one at its bound", map[string]any{"policies": []string{"1.1", "1.2", "1.3", "1.4", "1.5", "1.6", "1.7", oid64}}, 60},
		{"four lists, one at its bound, one DER", map[string]any{"crls": []string{b64(pad(stampingBoundBytes, crlPEM)), b64(string(otherCRL)), b64(string(derOf(t, r.crl))), b64(string(otherCRLOf(t)))}}, 60},
	} {
		proposal := r.proposal(c.change)
		checked := checkOn(t, r.ts, "", proposal)
		if checked.Shown.IntervalMinutes != c.interval {
			t.Errorf("%s: shown %+v", c.name, checked.Shown)
		}
		if got := setOn(t, r.ts, "", proposal, checked.Token); got.status != http.StatusOK {
			t.Errorf("%s: kept? %d %s", c.name, got.status, got.data)
		} else if got := stampingOf(t, r.ts, ""); got.State != stampingStateSet {
			t.Errorf("%s: read back as %+v", c.name, got)
		}
	}
	// The body: one byte past its bound refused before it is read.
	body := `{"authority":"` + testAuthorityAddress + `","roots":"` + strings.Repeat("a", 2<<20) + `"}`
	request, _ := http.NewRequest("POST", r.ts.URL+"/api/audit/stamping/check", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	bearer(request)
	response, err := r.ts.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest || refusalOf(data) != stampingRequestWords {
		t.Errorf("a body past its bound answered %d %s", response.StatusCode, data)
	}
	// A member besides those asked for; not JSON; cross-site.
	if got := stampingCall(t, r.ts, "/api/audit/stamping/check", "", r.proposal(map[string]any{"timestampAuthority": "x"})); got.status != http.StatusBadRequest || got.error != stampingRequestWords {
		t.Errorf("an unknown member answered %d %s", got.status, got.data)
	}
	status, _ := reviewCall(t, r.ts, "POST", "/api/audit/stamping/check", "", nil, bearer)
	if status != http.StatusUnsupportedMediaType {
		t.Errorf("a check with no JSON answered %d", status)
	}
	status, _ = reviewCall(t, r.ts, "POST", "/api/audit/stamping/check", "", r.proposal(nil), bearer, func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") })
	if status != http.StatusUnsupportedMediaType {
		t.Errorf("a check sent as text answered %d", status)
	}
	status, _ = reviewCall(t, r.ts, "POST", "/api/audit/stamping/check", "", r.proposal(nil), bearer, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") })
	if status != http.StatusForbidden {
		t.Errorf("a cross-site check answered %d", status)
	}
	status, _ = reviewCall(t, r.ts, "POST", "/api/audit/stamping/check", "", r.proposal(nil))
	if status != http.StatusUnauthorized {
		t.Errorf("a check with no bearer answered %d", status)
	}
}

// privateKeyPEM is a private key, PEM: what must never be kept beside the
// roots.
func privateKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}))
}

// withHeaders is the first certificate of roots, PEM, with a header.
func withHeaders(t *testing.T, roots string) string {
	t.Helper()
	block, _ := pem.Decode([]byte(roots))
	if block == nil {
		t.Fatal("no PEM block")
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: block.Type, Headers: map[string]string{"Comment": "kept beside it"}, Bytes: block.Bytes}))
}

// stampingBoundBytes is the most of the roots, and of one list, Desk keeps
// and reads: the test's own number.
const stampingBoundBytes = 262144

// derOf is the DER of the first list in a PEM file of lists.
func derOf(t *testing.T, data []byte) []byte {
	t.Helper()
	lists, ok := readCRLs(data)
	if !ok {
		t.Fatal("not a list")
	}
	return lists[0].Raw
}

// otherCRLOf is a list of a third authority.
func otherCRLOf(t *testing.T) []byte {
	t.Helper()
	authority, _ := newTestAuthority()
	crl, err := authority.crl(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return crl
}

// **The largest settings the writer accepts fit the bound they are read
// with**, newline and all.
func TestTheLargestStampingSettingsFitTheirBound(t *testing.T) {
	policies := make([]string, stampingMaxPolicies)
	for i := range policies {
		policies[i] = strings.Repeat("1.", 30) + fmt.Sprintf("1%d.1", i)
	}
	plan := stampingPlan{authority: "https://" + strings.Repeat("a", stampingAuthorityLimit-8), interval: stampingMaxInterval, roots: []byte("r"), policies: policies}
	for i := range stampingMaxCRLs {
		plan.crls = append(plan.crls, []byte{byte(i)})
	}
	data, err := json.Marshal(plan.file(1<<53 - 1))
	if err != nil || len(data)+1 > 4096 {
		t.Errorf("the largest settings are %d bytes (%v), past the 4 KiB they are held under", len(data)+1, err)
	}
	if !checkStampingFile(plan.file(0)) {
		t.Error("the largest settings are not settings Desk reads")
	}
}

/* The scheduler -------------------------------------------------------------- */

// **At the interval, and only where the head has moved.** The first wake
// after the authority is set stamps; a wake before the interval runs
// nothing; at the interval, a head that has not moved past the checkpoint
// stamped asks the runtime where the trail ends, and nothing more; a head
// that has moved is stamped at the next interval, not before. A check with
// roots that shows the trail stamped through its head makes the next
// attempt ask nothing more; a trail moved aside, of another identity, is
// stamped; a change of settings makes the next wake attempt; and with no
// authority, nothing runs at all.
func TestTheSchedulerStampsAtTheIntervalOnlyWhereTheHeadMoved(t *testing.T) {
	w := fixStamping(t)
	r := newStampRig(t)
	r.chainIs(t, handoverTrail, 3)
	r.stampsWith(t, 0, stampedAnswer(handoverTrail, 3))
	w.at(t, 0)
	if calls := r.ran(t); calls != nil {
		t.Errorf("with no authority set a wake ran %q", calls)
	}
	r.set(t, r.proposal(nil))
	r.ran(t)

	w.at(t, time.Minute)
	if calls := r.ran(t); !slices.Equal(calls, []string{schemaCall, stampHeadCall, stampCall}) {
		t.Errorf("the first wake ran %q", calls)
	}
	last := stampingOf(t, r.ts, "").Last
	if last == nil || last.At != handoverNow+60 || last.Requested ||
		!sameRun(*last, stampRun{Status: stampStamped, Trail: handoverTrail, Sequence: 3, Digest: standInDigest(3), StampedAt: "2026-10-07T13:29:07Z", ExistedBy: "2026-10-07T13:29:07Z", Policy: "1.3.6.1.4.1.99999.1"}) {
		t.Errorf("the last run is %+v", last)
	}
	r.ran(t)
	w.at(t, time.Hour+59*time.Second)
	if calls := r.ran(t); calls != nil {
		t.Errorf("a wake before the interval ran %q", calls)
	}
	w.at(t, time.Hour+time.Minute)
	if calls := r.ran(t); !slices.Equal(calls, []string{schemaCall, stampHeadCall}) {
		t.Errorf("at the interval, with the head where it was, a wake ran %q", calls)
	}
	r.chainIs(t, handoverTrail, 4)
	r.stampsWith(t, 0, stampedAnswer(handoverTrail, 4))
	w.at(t, 2*time.Hour)
	if calls := r.ran(t); calls != nil {
		t.Errorf("a wake before the next interval ran %q", calls)
	}
	w.at(t, 2*time.Hour+time.Minute)
	if calls := r.ran(t); !slices.Equal(calls, []string{schemaCall, stampHeadCall, stampCall}) {
		t.Errorf("at the interval, with the head moved, a wake ran %q", calls)
	}

	// A check with roots that shows the trail stamped through record 6, its
	// head, whose record digest the report names; the stamps file holds that
	// stamp, as the report says.
	r.chainIs(t, handoverTrail, 6)
	r.stampsFileHolds(t, handoverTrail, 6)
	r.answers(t, 0, headAt(strings.Replace(strings.Replace(stampsReport, `"stamped":{"status":"through","through":2}`, `"stamped":{"status":"through","through":6}`, 1), `"lines":3,`, `"lines":6,`, 1), 6))
	if got := stampingOf(t, r.ts, ""); got.Pending == nil || *got.Pending != 0 {
		t.Errorf("the check says %+v", got)
	}
	r.ran(t)
	w.at(t, 3*time.Hour+time.Minute)
	if calls := r.ran(t); !slices.Equal(calls, []string{schemaCall, stampHeadCall}) {
		t.Errorf("with the head stamped, as the check showed, a wake ran %q", calls)
	}
	// Moved aside: another identity, at a lower sequence.
	r.chainIs(t, movedTrail, 1)
	r.stampsWith(t, 0, stampedAnswer(movedTrail, 1))
	w.at(t, 4*time.Hour+time.Minute)
	if calls := r.ran(t); !slices.Equal(calls, []string{schemaCall, stampHeadCall, stampCall}) {
		t.Errorf("with the trail moved aside a wake ran %q", calls)
	}
	// A change of settings: the next wake attempts, at once.
	r.set(t, r.proposal(map[string]any{"intervalMinutes": 30}))
	r.ran(t)
	w.at(t, 4*time.Hour+2*time.Minute)
	if calls := r.ran(t); !slices.Equal(calls, []string{schemaCall, stampHeadCall}) {
		t.Errorf("after a change of settings a wake ran %q", calls)
	}
	w.at(t, 4*time.Hour+31*time.Minute)
	if calls := r.ran(t); calls != nil {
		t.Errorf("a wake 29 minutes later ran %q", calls)
	}
	w.at(t, 4*time.Hour+32*time.Minute)
	if calls := r.ran(t); !slices.Equal(calls, []string{schemaCall, stampHeadCall}) {
		t.Errorf("at the new interval a wake ran %q", calls)
	}
	// No chained record: nothing to stamp, and nothing said.
	if err := os.WriteFile(r.calls+".lines", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	before := stampingOf(t, r.ts, "").Last
	r.ran(t)
	w.at(t, 6*time.Hour)
	if calls := r.ran(t); !slices.Equal(calls, []string{schemaCall, stampHeadCall}) {
		t.Errorf("with no chained record a wake ran %q", calls)
	}
	if after := stampingOf(t, r.ts, "").Last; after == nil || after.At != before.At || !sameRun(*after, *before) {
		t.Errorf("with no chained record the last run became %+v", after)
	}
}

// headAt is report, stampsReport's shape, with its head the stand-in's
// checkpoint at sequence: the record digest the report names for it.
func headAt(report string, sequence int) string {
	return strings.Replace(report, `"recordDigest":"sha256:607b98c9595baaa5154fc08e58f3b452084a6fdcf06fe71e49a00fa06556d3e3","sequence":3`,
		`"recordDigest":"`+standInDigest(sequence)+`","sequence":`+strconv.Itoa(sequence), 1)
}

// rewrittenAt is the stand-in's checkpoints of records 1 to n of trail, the
// record at n another than the one stampedAnswer and chainIs name there: a
// trail restored to an earlier point and written since, with its identity.
func (rig *auditRig) rewrittenAt(t *testing.T, trail string, n int) {
	t.Helper()
	last := strings.Replace(checkpointOf(trail, n), standInDigest(n), rewrittenDigest(n), 1)
	if err := os.WriteFile(rig.calls+".lines", []byte(chainOf(trail, upTo(n-1)...)+last), 0o600); err != nil {
		t.Fatal(err)
	}
}

// rewrittenDigest is the record digest of that other record at sequence.
func rewrittenDigest(sequence int) string {
	return fmt.Sprintf("sha256:%064x", 0x1000+sequence)
}

// **A trail restored to an earlier point is stamped again** (line audit,
// finding 5). The scheduler holds the head to the checkpoint it last knew
// stamped, by trail, sequence and record digest, and stamps wherever it is
// not that checkpoint. The trail stamped at record 5, then restored to an
// earlier point and repaired, ends at record 4: below the checkpoint
// stamped, and stamped. Another trail, moved in, ends at record 4 too, with
// a checkpoint of the same digest: stamped. Restored and written since, it
// ends at record 4 again, another record of the same trail: stamped. And
// where the head is the checkpoint the last run named, nothing is asked.
func TestTheSchedulerStampsATrailRestoredToAnEarlierPointAgain(t *testing.T) {
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
	// Restored to an earlier point and repaired: the same identity, its last
	// record below the one stamped.
	r.chainIs(t, handoverTrail, 4)
	r.stampsWith(t, 0, stampedAnswer(handoverTrail, 4))
	w.at(t, time.Hour+time.Minute)
	if calls := r.ran(t); !slices.Equal(calls, stamps) {
		t.Errorf("with the head below the checkpoint stamped a wake ran %q", calls)
	}
	w.at(t, 2*time.Hour+time.Minute)
	if calls := r.ran(t); !slices.Equal(calls, asks) {
		t.Errorf("with the head the checkpoint stamped a wake ran %q", calls)
	}
	// Another trail at the very sequence, whose checkpoint names the same
	// digest the stand-in gives every record 4: only its identity differs.
	r.chainIs(t, movedTrail, 4)
	r.stampsWith(t, 0, stampedAnswer(movedTrail, 4))
	w.at(t, 3*time.Hour+time.Minute)
	if calls := r.ran(t); !slices.Equal(calls, stamps) {
		t.Errorf("with another trail at the sequence stamped a wake ran %q", calls)
	}
	// Another record at the sequence stamped, in the same trail: only its
	// digest differs.
	r.rewrittenAt(t, movedTrail, 4)
	r.stampsWith(t, 0, strings.Replace(stampedAnswer(movedTrail, 4), standInDigest(4), rewrittenDigest(4), 1))
	w.at(t, 4*time.Hour+time.Minute)
	if calls := r.ran(t); !slices.Equal(calls, stamps) {
		t.Errorf("with another record at the sequence stamped a wake ran %q", calls)
	}
	if last := stampingOf(t, r.ts, "").Last; last == nil || last.Trail != movedTrail || last.Digest != rewrittenDigest(4) {
		t.Errorf("the last run is %+v", last)
	}
	r.ran(t)
	w.at(t, 5*time.Hour+time.Minute)
	if calls := r.ran(t); !slices.Equal(calls, asks) {
		t.Errorf("with the head the checkpoint just stamped a wake ran %q", calls)
	}
}

// **A check with roots replaces what the scheduler knows, and never only
// advances it** (line audit, finding 5). After a stamp of record 5, a check
// whose stamps the runtime accepts reach only record 2 makes the next wake
// ask the runtime again, which answers that the checkpoint is stamped
// already: nothing is asked of the authority, and the wake after asks
// nothing. A check that finds no stamp covering a record leaves nothing
// known: the next wake asks again. A check whose stamps reach record 5, which
// is not the report's head, knows no record digest for it: another record at
// record 5 is stamped (second review of #294). The decision record's report
// names the trail's identity to the page.
func TestACheckWithRootsReplacesWhatTheSchedulerKnows(t *testing.T) {
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
	lower := strings.Replace(strings.Replace(stampsReport, `"lines":3,`, `"lines":5,`, 1), `"chained":3,`, `"chained":5,`, 1)
	r.answers(t, 0, lower)
	status, answer, _ := readAudit(t, r.ts, "")
	if status != http.StatusOK || answer.Report == nil || answer.Report.Trail != handoverTrail || answer.Report.Coverage.Stamped.Through != 2 {
		t.Fatalf("the decision record answered %d %+v", status, answer.Report)
	}
	r.ran(t)
	r.stampsWith(t, 0, alreadyStamped(handoverTrail, 5))
	w.at(t, 2*time.Hour+time.Minute)
	if calls := r.ran(t); !slices.Equal(calls, stamps) {
		t.Errorf("after a check whose stamps reach a lower record a wake ran %q", calls)
	}
	w.at(t, 3*time.Hour+time.Minute)
	if calls := r.ran(t); !slices.Equal(calls, asks) {
		t.Errorf("after the runtime said the head is stamped already a wake ran %q", calls)
	}
	none := strings.Replace(strings.Replace(lower, `"stamped":{"status":"through","through":2}`, `"stamped":{"status":"none"}`, 1), `"coveredBy":"2026-10-07T13:29:22Z",`, ``, 1)
	r.answers(t, 0, strings.Replace(none, `"trusted":2,"revocationChecked":0,"revocationNotChecked":2`, `"trusted":0,"revocationChecked":0,"revocationNotChecked":0`, 1))
	if got := stampingOf(t, r.ts, ""); !got.Passed {
		t.Fatalf("the check was not given the roots: %+v", got)
	}
	r.ran(t)
	w.at(t, 4*time.Hour+time.Minute)
	if calls := r.ran(t); !slices.Equal(calls, stamps) {
		t.Errorf("after a check that finds no stamp a wake ran %q", calls)
	}
	r.answers(t, 0, strings.Replace(lower, `"stamped":{"status":"through","through":2}`, `"stamped":{"status":"through","through":5}`, 1))
	if got := stampingOf(t, r.ts, ""); !got.Passed {
		t.Fatalf("the check was not given the roots: %+v", got)
	}
	r.rewrittenAt(t, handoverTrail, 5)
	r.stampsWith(t, 0, strings.Replace(stampedAnswer(handoverTrail, 5), standInDigest(5), rewrittenDigest(5), 1))
	r.ran(t)
	w.at(t, 5*time.Hour+time.Minute)
	if calls := r.ran(t); !slices.Equal(calls, stamps) {
		t.Errorf("with another record at the sequence a check found stamped, with no digest known, a wake ran %q", calls)
	}
}

// **A check keeps the record digest of the head it found stamped** (second
// review of #294). The scheduler stamps record 5; a check with roots finds
// the trail stamped through record 5, its head, and the report names that
// record's digest: the next wake asks nothing more. The trail is then
// restored to an earlier point and written since, so another record sits at
// record 5: never stamped, and the next wake stamps it.
func TestACheckKeepsTheDigestOfTheHeadItFindsStamped(t *testing.T) {
	w := fixStamping(t)
	r := newStampRig(t)
	r.set(t, r.proposal(nil))
	r.chainIs(t, handoverTrail, 5)
	r.stampsWith(t, 0, stampedAnswer(handoverTrail, 5))
	r.ran(t)
	stamps := []string{schemaCall, stampHeadCall, stampCall}
	w.at(t, time.Minute)
	if calls := r.ran(t); !slices.Equal(calls, stamps) {
		t.Fatalf("the first wake ran %q", calls)
	}
	lower := strings.Replace(strings.Replace(stampsReport, `"lines":3,`, `"lines":5,`, 1), `"chained":3,`, `"chained":5,`, 1)
	r.answers(t, 0, headAt(strings.Replace(lower, `"stamped":{"status":"through","through":2}`, `"stamped":{"status":"through","through":5}`, 1), 5))
	if got := stampingOf(t, r.ts, ""); !got.Passed {
		t.Fatalf("the check was not given the roots: %+v", got)
	}
	r.ran(t)
	w.at(t, time.Hour+time.Minute)
	if calls := r.ran(t); !slices.Equal(calls, []string{schemaCall, stampHeadCall}) {
		t.Errorf("with the head the record a check found stamped a wake ran %q", calls)
	}
	r.rewrittenAt(t, handoverTrail, 5)
	r.stampsWith(t, 0, strings.Replace(stampedAnswer(handoverTrail, 5), standInDigest(5), rewrittenDigest(5), 1))
	r.ran(t)
	w.at(t, 2*time.Hour+time.Minute)
	if calls := r.ran(t); !slices.Equal(calls, stamps) {
		t.Errorf("with another record at the sequence a check found stamped a wake ran %q", calls)
	}
}

// **Each failure in the runtime's words, and tried again only at the next
// interval.** An authority that does not answer, none named, and a trail
// that fails a check: each the runtime's refusal, its code and message as it
// said them; a wake before the interval asks nothing, and the one at the
// interval asks once. A refusal naming the trail's path reaches the page with
// no path, and the log keeps it. An answer the runtime does not document, and
// a run that does not finish, are said in Desk's words.
func TestEachStampFailureIsShownInTheRuntimesWordsAndTriedAtTheNextInterval(t *testing.T) {
	w := fixStamping(t)
	r := newStampRig(t)
	r.chainIs(t, handoverTrail, 2)
	r.set(t, r.proposal(nil))
	elapsed := time.Duration(0)
	for _, answer := range []struct {
		code int
		body string
	}{{4, stampUnreachable}, {3, stampNoAuthority}, {1, stampRefusedTrail}} {
		r.stampsWith(t, answer.code, answer.body)
		var want struct {
			Diagnostics []runtimeDiagnostic `json:"diagnostics"`
		}
		if err := json.Unmarshal([]byte(answer.body), &want); err != nil {
			t.Fatal(err)
		}
		elapsed += time.Hour
		w.at(t, elapsed)
		if n := stampsRun(r.ran(t)); n != 1 {
			t.Errorf("%s: the wake ran %d stamps", want.Diagnostics[0].Code, n)
		}
		last := stampingOf(t, r.ts, "").Last
		if last == nil || last.Status != stampRefused || !slices.Equal(last.Diagnostics, want.Diagnostics) || last.At != handoverNow+int64(elapsed/time.Second) {
			t.Errorf("%s: the last run is %+v", want.Diagnostics[0].Code, last)
		}
		r.ran(t)
		w.at(t, elapsed+59*time.Minute)
		if calls := r.ran(t); calls != nil {
			t.Errorf("%s: a wake before the interval ran %q", want.Diagnostics[0].Code, calls)
		}
	}
	// No trail yet: a refusal naming the trail's path.
	r.stampsWith(t, 4, stampNoTrailYet)
	elapsed += time.Hour
	w.at(t, elapsed)
	status, data := reviewCall(t, r.ts, "GET", "/api/audit/verify", "", nil, bearer)
	var answer auditAnswer
	if status != http.StatusOK || json.Unmarshal(data, &answer) != nil || answer.Stamping == nil || answer.Stamping.Last == nil ||
		!slices.Equal(answer.Stamping.Last.Diagnostics, []runtimeDiagnostic{{"JPS-AUDIT-TRAIL-READ", "The project's trail …/evaluations.jsonl does not exist yet: no record has been written."}}) {
		t.Errorf("the decision record answered %d %s", status, data)
	}
	if strings.Contains(string(data), "/project") || !strings.Contains(r.log(), "/project/.desk-private/audit/evaluations.jsonl") {
		t.Errorf("the page was told the path, or the log was not: %s", data)
	}
	// Not documented: Desk's words.
	for _, body := range []string{
		`{"outputVersion":"2","command":"audit stamp","status":"stamped"}`,
		strings.Replace(stampedAnswer(handoverTrail, 2), `"outputVersion":"2"`, `"outputVersion":"3"`, 1),
		"not JSON",
	} {
		r.stampsWith(t, 0, body)
		elapsed += time.Hour
		w.at(t, elapsed)
		if last := stampingOf(t, r.ts, "").Last; last == nil || last.Status != stampProblem || last.Problem != stampingUndocumented {
			t.Errorf("%s: the last run is %+v", body, last)
		}
	}
	// A run that does not finish within runRuntime's bound.
	was := runtimeCommandTimeout
	runtimeCommandTimeout = 2 * time.Second
	t.Cleanup(func() { runtimeCommandTimeout = was })
	stampingRunsAs(t, r.auditRig, "  exec sleep 30")
	elapsed += time.Hour
	w.at(t, elapsed)
	runtimeCommandTimeout = was
	if last := stampingOf(t, r.ts, "").Last; last == nil || last.Status != stampProblem || !strings.HasPrefix(last.Problem, "The stamp run did not finish: ") {
		t.Errorf("a run that did not finish is %+v", last)
	}
}

// stampingRunsAs makes the stand-in's `audit stamp` run fragment in place of
// printing an answer.
func stampingRunsAs(t *testing.T, rig *auditRig, fragment string) {
	t.Helper()
	script, err := os.ReadFile(rig.bin)
	if err != nil {
		t.Fatal(err)
	}
	script = bytes.Replace(script, []byte("'audit stamp')\n"), []byte("'audit stamp')\n"+fragment+"\n"), 1)
	if err := os.WriteFile(rig.bin, script, 0o755); err != nil {
		t.Fatal(err)
	}
}

// **One run at a time per desk.** While "Stamp now" runs, a second is
// refused in plain words, a wake that finds a stamp due runs nothing, and
// the decision record says a run is in progress; once it ends, one stamp was
// asked for, and the turn is free.
func TestOneStampRunAtATimePerDesk(t *testing.T) {
	t.Cleanup(func() { testHookStampRun = nil })
	w := fixStamping(t)
	r := newStampRig(t)
	r.chainIs(t, handoverTrail, 2)
	r.stampsWith(t, 0, stampedAnswer(handoverTrail, 2))
	r.set(t, r.proposal(nil))
	hold := holdStampRuns(t)
	r.ran(t)
	first := make(chan stampingAnswered, 1)
	go func() { first <- stampingCall(t, r.ts, "/api/audit/stamping/stamp", "", map[string]any{}) }()
	hold.arrived(t)
	second := make(chan stampingAnswered, 1)
	go func() { second <- stampingCall(t, r.ts, "/api/audit/stamping/stamp", "", map[string]any{}) }()
	select {
	case got := <-second:
		if got.status != http.StatusConflict || got.error != stampingBusyWords {
			t.Errorf("a second Stamp now answered %d %s", got.status, got.data)
		}
	case <-time.After(10 * time.Second):
		t.Error("a second Stamp now waited for the first")
		hold.free()
		<-second
		<-first
		t.FailNow()
	}
	woke := make(chan bool, 1)
	go func() { woke <- w.send(2 * time.Hour) }()
	select {
	case acted := <-woke:
		if !acted {
			t.Error("the scheduler did not act on its wake")
		}
	case <-time.After(10 * time.Second):
		t.Error("a wake waited for the run in progress")
		hold.free()
		<-first
		<-woke
		t.FailNow()
	}
	if got := stampingOf(t, r.ts, ""); !got.Running {
		t.Errorf("during a run the decision record says %+v", got)
	}
	hold.free()
	got := <-first
	var run struct {
		Run stampRun `json:"run"`
	}
	if got.status != http.StatusOK || json.Unmarshal([]byte(got.data), &run) != nil || run.Run.Status != stampStamped || !run.Run.Requested || run.Run.Sequence != 2 {
		t.Errorf("Stamp now answered %d %s", got.status, got.data)
	}
	calls := r.ran(t)
	if n := stampsRun(calls); n != 1 || slices.Contains(calls, stampHeadCall) {
		t.Errorf("with one run in progress the stand-in ran %q", calls)
	}
	if !r.s.stamping.turn.TryLock() {
		t.Fatal("the run's turn is still held")
	}
	r.s.stamping.turn.Unlock()
	if got := stampingOf(t, r.ts, ""); got.Running || got.Last == nil || !got.Last.Requested {
		t.Errorf("after the run the decision record says %+v", got)
	}
}

// **"Stamp now" asks the runtime once, with the authority set, or says why
// not.** It runs the stamp whatever the head (a checkpoint stamped already
// costs nothing); with no authority it runs nothing; and it is a JSON POST
// of the owner's, never a cross-site one.
func TestStampNowIsTheOwnersRequest(t *testing.T) {
	fixStamping(t)
	r := newStampRig(t)
	r.chainIs(t, handoverTrail, 2)
	r.stampsWith(t, 0, alreadyStamped(handoverTrail, 2))
	if got := stampingCall(t, r.ts, "/api/audit/stamping/stamp", "", map[string]any{}); got.status != http.StatusConflict || got.error != stampingNoneWords {
		t.Errorf("with no authority Stamp now answered %d %s", got.status, got.data)
	}
	if n := stampsRun(r.ran(t)); n != 0 {
		t.Errorf("with no authority Stamp now ran %d stamps", n)
	}
	r.set(t, r.proposal(nil))
	r.ran(t)
	got := stampingCall(t, r.ts, "/api/audit/stamping/stamp", "", map[string]any{})
	if got.status != http.StatusOK || !strings.Contains(got.data, `"status":"already-stamped"`) {
		t.Errorf("Stamp now answered %d %s", got.status, got.data)
	}
	if calls := r.ran(t); !slices.Equal(calls, []string{schemaCall, stampCall}) {
		t.Errorf("Stamp now ran %q, want packs schema and the stamp alone", calls)
	}
	// The runtime's refusal, naming the trail's path: passed on with none,
	// and kept whole in the log.
	r.stampsWith(t, 4, stampNoTrailYet)
	got = stampingCall(t, r.ts, "/api/audit/stamping/stamp", "", map[string]any{})
	if got.status != http.StatusOK || strings.Contains(got.data, "/project") || !strings.Contains(got.data, "The project's trail …/evaluations.jsonl does not exist yet") ||
		!strings.Contains(r.log(), "/project/.desk-private/audit/evaluations.jsonl") {
		t.Errorf("Stamp now answered %d %s", got.status, got.data)
	}
	r.ran(t)
	if got := stampingCall(t, r.ts, "/api/audit/stamping/stamp", "", map[string]any{"now": true}); got.status != http.StatusBadRequest || got.error != stampingStartWords {
		t.Errorf("a request with a member answered %d %s", got.status, got.data)
	}
	status, _ := reviewCall(t, r.ts, "POST", "/api/audit/stamping/stamp", "", map[string]any{}, bearer, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") })
	if status != http.StatusForbidden {
		t.Errorf("a cross-site Stamp now answered %d", status)
	}
	if n := stampsRun(r.ran(t)); n != 0 {
		t.Errorf("requests that are not the owner's ran %d stamps", n)
	}
}

// **The scheduler stops with the server, and a stop never kills a stamp
// mid-write.** A Close while a stamp runs waits for it to end: the run is
// recorded, and Close returns only then. After it the loop is gone, and a
// "Stamp now" starts nothing.
func TestTheSchedulerStopsWithTheServer(t *testing.T) {
	t.Cleanup(func() { testHookStampRun = nil })
	w := fixStamping(t)
	r := newStampRig(t)
	r.chainIs(t, handoverTrail, 2)
	r.stampsWith(t, 0, stampedAnswer(handoverTrail, 2))
	r.set(t, r.proposal(nil))
	hold := holdStampRuns(t)
	r.ran(t)
	woke := make(chan bool, 1)
	go func() { woke <- w.send(time.Minute) }()
	hold.arrived(t)
	closed := make(chan struct{})
	go func() { r.s.Close(); close(closed) }()
	// A Close that does not wait returns at once; one that waits is still
	// waiting a second later.
	select {
	case <-closed:
		t.Fatal("Close returned while a stamp was running")
	case <-time.After(time.Second):
	}
	hold.free()
	if !<-woke {
		t.Error("the scheduler did not act on its wake")
	}
	<-closed
	if n := stampsRun(r.ran(t)); n != 1 {
		t.Errorf("the stamp in progress at Close ran %d times", n)
	}
	if last, _ := r.s.stamping.report(); last == nil || last.Status != stampStamped {
		t.Errorf("the stamp in progress at Close was recorded as %+v", last)
	}
	select {
	case <-r.s.stamping.done:
	default:
		t.Error("the scheduler's loop outlived Close")
	}
	select {
	case w.wake <- time.Now():
		t.Error("a wake after Close was taken")
	default:
	}
	if got := stampingCall(t, r.ts, "/api/audit/stamping/stamp", "", map[string]any{}); got.status == http.StatusOK || stampsRun(r.ran(t)) != 0 {
		t.Errorf("Stamp now after Close answered %d %s", got.status, got.data)
	}
}

// stampsTo is how many `audit stamp` calls among calls name the authority
// at address.
func stampsTo(calls []string, address string) int {
	n := 0
	for _, call := range calls {
		if strings.HasPrefix(call, "audit stamp ") && strings.Contains(call, " --tsa "+address+" ") {
			n++
		}
	}
	return n
}

// **The settings stamped with are the settings in force** (review round 1).
// A change to another authority, or a removal, confirmed while a run makes
// its checks, is answered at once, and the run then asks for no stamp at all,
// on the owner's request or the scheduler's: never to the authority replaced.
// The next request stamps with the authority now set.
func TestAChangeOrRemovalWhileARunChecksIsHonoured(t *testing.T) {
	t.Cleanup(func() { testHookStampChecked = nil })
	w := fixStamping(t)
	r := newStampRig(t)
	r.chainIs(t, handoverTrail, 2)
	r.stampsWith(t, 0, stampedAnswer(handoverTrail, 2))
	r.set(t, r.proposal(nil))
	hold := holdAt(t, &testHookStampChecked)
	r.ran(t)
	first := make(chan stampingAnswered, 1)
	go func() { first <- stampingCall(t, r.ts, "/api/audit/stamping/stamp", "", map[string]any{}) }()
	hold.arrived(t)
	other := "https://other.example/stamp"
	changed := r.proposal(map[string]any{"authority": other})
	if got := setOn(t, r.ts, "", changed, checkOn(t, r.ts, "", changed).Token); got.status != http.StatusOK {
		t.Fatalf("a change while the run checked answered %d %s", got.status, got.data)
	}
	hold.free()
	got := <-first
	var run struct {
		Run stampRun `json:"run"`
	}
	if got.status != http.StatusOK || json.Unmarshal([]byte(got.data), &run) != nil || run.Run.Status != stampProblem || run.Run.Problem != stampingMovedWords {
		t.Errorf("Stamp now across a change answered %d %s", got.status, got.data)
	}
	if n := stampsRun(r.ran(t)); n != 0 {
		t.Errorf("across a change the run asked for %d stamps", n)
	}
	if got := stampingCall(t, r.ts, "/api/audit/stamping/stamp", "", map[string]any{}); got.status != http.StatusOK {
		t.Fatalf("Stamp now answered %d %s", got.status, got.data)
	}
	if calls := r.ran(t); stampsTo(calls, other) != 1 || stampsTo(calls, testAuthorityAddress) != 0 {
		t.Errorf("after the change Stamp now ran %q", calls)
	}

	// The scheduler's run, across a removal, with a record added since.
	r.chainIs(t, handoverTrail, 3)
	hold2 := holdAt(t, &testHookStampChecked)
	woke := make(chan bool, 1)
	go func() { woke <- w.send(3 * time.Hour) }()
	hold2.arrived(t)
	token := stampingOf(t, r.ts, "").RemoveToken
	if got := stampingCall(t, r.ts, "/api/audit/stamping/remove", "", map[string]string{"token": token}); got.status != http.StatusOK {
		t.Fatalf("a removal while the run checked answered %d %s", got.status, got.data)
	}
	hold2.free()
	if !<-woke {
		t.Fatal("the scheduler did not act on its wake")
	}
	if n := stampsRun(r.ran(t)); n != 0 {
		t.Errorf("across a removal the scheduler asked for %d stamps", n)
	}
	if last := stampingOf(t, r.ts, ""); last.Last == nil || last.Last.Problem != stampingMovedWords || last.Last.Requested {
		t.Errorf("across a removal the last run is %+v", last.Last)
	}
}

// **A change or a removal confirmed while the stamp is being asked for waits
// for it** (review round 1). The settings are held from the moment they are
// read again until the runtime answers: a removal, and a change, posted then
// are not answered while the stamp runs, and are answered once it is over;
// the stamp in progress goes to the authority it read; and after the removal
// no run starts.
func TestAChangeOrRemovalWaitsForTheStampInProgress(t *testing.T) {
	t.Cleanup(func() { testHookStampRun = nil })
	w := fixStamping(t)
	r := newStampRig(t)
	r.chainIs(t, handoverTrail, 2)
	r.stampsWith(t, 0, stampedAnswer(handoverTrail, 2))
	r.set(t, r.proposal(nil))
	for _, c := range []struct {
		name  string
		write func() stampingAnswered
	}{
		{"a change", func() stampingAnswered {
			changed := r.proposal(map[string]any{"intervalMinutes": 30})
			return setOn(t, r.ts, "", changed, checkOn(t, r.ts, "", changed).Token)
		}},
		{"a removal", func() stampingAnswered {
			return stampingCall(t, r.ts, "/api/audit/stamping/remove", "", map[string]string{"token": stampingOf(t, r.ts, "").RemoveToken})
		}},
	} {
		hold := holdStampRuns(t)
		r.ran(t)
		first := make(chan stampingAnswered, 1)
		go func() { first <- stampingCall(t, r.ts, "/api/audit/stamping/stamp", "", map[string]any{}) }()
		hold.arrived(t)
		// The token is taken before the run holds the settings; the write
		// that confirms it waits.
		written := make(chan stampingAnswered, 1)
		go func() { written <- c.write() }()
		var answer stampingAnswered
		select {
		case answer = <-written:
			t.Errorf("%s was answered while the stamp ran: %d %s", c.name, answer.status, answer.data)
		case <-time.After(time.Second):
			hold.free()
			answer = <-written
		}
		hold.free()
		if got := <-first; got.status != http.StatusOK || !strings.Contains(got.data, `"status":"stamped"`) {
			t.Errorf("%s: the stamp in progress answered %d %s", c.name, got.status, got.data)
		}
		if answer.status != http.StatusOK {
			t.Errorf("%s answered %d %s after the stamp", c.name, answer.status, answer.data)
		}
		if n := stampsTo(r.ran(t), testAuthorityAddress); n != 1 {
			t.Errorf("%s: the stamp in progress asked the authority it read %d times", c.name, n)
		}
	}
	w.at(t, 6*time.Hour)
	if calls := r.ran(t); calls != nil {
		t.Errorf("after the removal a wake ran %q", calls)
	}
}

// **Close waits for a "Stamp now" in progress** (review round 1): the loop is
// idle, and Close returns only once the owner's run is over.
func TestCloseWaitsForAStampNowInProgress(t *testing.T) {
	t.Cleanup(func() { testHookStampRun = nil })
	fixStamping(t)
	r := newStampRig(t)
	r.chainIs(t, handoverTrail, 2)
	r.stampsWith(t, 0, stampedAnswer(handoverTrail, 2))
	r.set(t, r.proposal(nil))
	hold := holdStampRuns(t)
	r.ran(t)
	first := make(chan stampingAnswered, 1)
	go func() { first <- stampingCall(t, r.ts, "/api/audit/stamping/stamp", "", map[string]any{}) }()
	hold.arrived(t)
	closed := make(chan struct{})
	go func() { r.s.Close(); close(closed) }()
	select {
	case <-closed:
		t.Error("Close returned while a Stamp now was running")
	case <-time.After(time.Second):
	}
	hold.free()
	if got := <-first; got.status != http.StatusOK || !strings.Contains(got.data, `"status":"stamped"`) {
		t.Errorf("the Stamp now in progress at Close answered %d %s", got.status, got.data)
	}
	<-closed
	if n := stampsRun(r.ran(t)); n != 1 {
		t.Errorf("the Stamp now in progress at Close ran %d stamps", n)
	}
}

// **The answers `audit stamp` documents, and nothing else.**
func TestAStampAnswerIsReadAsDocumented(t *testing.T) {
	exit := func(code int) error {
		if code == 0 {
			return nil
		}
		return exitError(t, code)
	}
	for _, c := range []struct {
		name string
		code int
		body string
		want stampRun
	}{
		{"a stamp", 0, stampedAnswer(handoverTrail, 3), stampRun{Status: stampStamped, Trail: handoverTrail, Sequence: 3, Digest: standInDigest(3), StampedAt: "2026-10-07T13:29:07Z", ExistedBy: "2026-10-07T13:29:07Z", Policy: "1.3.6.1.4.1.99999.1"}},
		{"stamped already", 0, alreadyStamped(handoverTrail, 3), stampRun{Status: stampAlready, Trail: handoverTrail, Sequence: 3, Digest: standInDigest(3)}},
		{"unreachable", 4, stampUnreachable, stampRun{Status: stampRefused, Diagnostics: []runtimeDiagnostic{{"JPS-AUDIT-STAMP-UNREACHABLE", "the time-stamping authority could not be asked, or did not answer: the request was not answered. Nothing was written, and the trail and the decisions in it are as they were; asking again stamps the same checkpoint."}}}},
		{"a stamp with a failed exit", 1, stampedAnswer(handoverTrail, 3), stampRun{Status: stampProblem, Problem: stampingUndocumented}},
		{"a refusal with exit 0", 0, stampUnreachable, stampRun{Status: stampProblem, Problem: stampingUndocumented}},
		{"a refusal with no words", 4, `{"outputVersion":"2","command":"audit stamp","status":"error","diagnostics":[]}`, stampRun{Status: stampProblem, Problem: stampingUndocumented}},
		{"another command", 0, strings.Replace(stampedAnswer(handoverTrail, 3), `"audit stamp"`, `"audit verify"`, 1), stampRun{Status: stampProblem, Problem: stampingUndocumented}},
		{"another status", 0, strings.Replace(stampedAnswer(handoverTrail, 3), `"stamped"`, `"done"`, 1), stampRun{Status: stampProblem, Problem: stampingUndocumented}},
		{"another outputVersion", 0, strings.Replace(stampedAnswer(handoverTrail, 3), `"outputVersion":"2"`, `"outputVersion":"1"`, 1), stampRun{Status: stampProblem, Problem: stampingUndocumented}},
		{"no checkpoint", 0, strings.Replace(stampedAnswer(handoverTrail, 3), `"checkpoint"`, `"other"`, 1), stampRun{Status: stampProblem, Problem: stampingUndocumented}},
		{"a checkpoint of no shape", 0, strings.Replace(stampedAnswer(handoverTrail, 3), `"checkpointVersion":"1",`, ``, 1), stampRun{Status: stampProblem, Problem: stampingUndocumented}},
		{"a stamp with no time", 0, strings.Replace(stampedAnswer(handoverTrail, 3), `"existedBy"`, `"other"`, 1), stampRun{Status: stampProblem, Problem: stampingUndocumented}},
		{"a stamp with a time of no form", 0, strings.Replace(stampedAnswer(handoverTrail, 3), `"stampedAt":"2026-10-07T13:29:07Z"`, `"stampedAt":"yesterday"`, 1), stampRun{Status: stampProblem, Problem: stampingUndocumented}},
		{"a stamp with no policy", 0, strings.Replace(stampedAnswer(handoverTrail, 3), `"policy":"1.3.6.1.4.1.99999.1"`, `"policy":"any"`, 1), stampRun{Status: stampProblem, Problem: stampingUndocumented}},
		{"stamped already, with a time", 0, strings.Replace(alreadyStamped(handoverTrail, 3), `}}`, `},"existedBy":"2026-10-07T13:29:07Z"}`, 1), stampRun{Status: stampProblem, Problem: stampingUndocumented}},
		{"not JSON", 0, "stamped", stampRun{Status: stampProblem, Problem: stampingUndocumented}},
	} {
		got := readStamped([]byte(c.body), exit(c.code))
		if !sameRun(got, c.want) {
			t.Errorf("%s: read as %+v, want %+v", c.name, got, c.want)
		}
	}
	if got := readStamped(nil, os.ErrDeadlineExceeded); got.Status != stampProblem || !strings.HasPrefix(got.Problem, "The stamp run did not finish: ") {
		t.Errorf("a run that did not finish is read as %+v", got)
	}
}

func sameRun(a, b stampRun) bool {
	return a.Status == b.Status && a.Trail == b.Trail && a.Sequence == b.Sequence && a.Digest == b.Digest && a.StampedAt == b.StampedAt && a.ExistedBy == b.ExistedBy &&
		a.Policy == b.Policy && a.Problem == b.Problem && slices.Equal(a.Diagnostics, b.Diagnostics)
}

// exitError is the error a command that exited with code gives.
func exitError(t *testing.T, code int) error {
	t.Helper()
	err := exec.Command("sh", "-c", fmt.Sprintf("exit %d", code)).Run()
	if err == nil {
		t.Fatal("no exit error")
	}
	return err
}

/* Verification with roots ----------------------------------------------------- */

// **The stamps as the runtime read them, and the records pending.** With the
// roots passed, the report's stamps are its own: the stamps file's lines,
// those that hold, how many had revocation checked, the time the stamped
// records existed by, and the lag; and the records after the one stamped
// through are pending. Where no stamp covers a record, every record is
// pending. Without roots passed, nothing is pending, whatever the report
// says. A stamps member that is not the runtime's shape is not a report.
func TestTheStampsAndThePendingRecordsAreTheRuntimes(t *testing.T) {
	r := newStampRig(t)
	r.answers(t, 0, stampsReport)
	if got := stampingOf(t, r.ts, ""); got.Pending != nil {
		t.Errorf("with no roots passed the decision record says %+v", got)
	}
	r.set(t, r.proposal(nil))
	status, answer, _ := readAudit(t, r.ts, "")
	want := &auditStamps{Lines: 2, Trusted: 2, RevocationNotChecked: 2, CoveredBy: "2026-10-07T13:29:22Z",
		Lag: auditStampLag{Records: 2, MaxSeconds: 14.607222481, MaxSequence: 2, MinSeconds: 10.92565753, MinSequence: 1}}
	if status != http.StatusOK || answer.Report == nil || answer.Report.Stamps == nil || *answer.Report.Stamps != *want ||
		answer.Report.Coverage.Stamped != (auditCoverageState{Status: "through", Through: 2}) {
		t.Errorf("the decision record answered %d %+v", status, answer.Report)
	}
	if answer.Stamping == nil || answer.Stamping.Pending == nil || *answer.Stamping.Pending != 1 {
		t.Errorf("the decision record says %+v pending", answer.Stamping)
	}
	none := strings.Replace(strings.Replace(stampsReport, `"stamped":{"status":"through","through":2}`, `"stamped":{"status":"none"}`, 1), `"coveredBy":"2026-10-07T13:29:22Z",`, ``, 1)
	r.answers(t, 0, strings.Replace(none, `"trusted":2,"revocationChecked":0,"revocationNotChecked":2`, `"trusted":0,"revocationChecked":0,"revocationNotChecked":0`, 1))
	if got := stampingOf(t, r.ts, ""); got.Pending == nil || *got.Pending != 3 {
		t.Errorf("with no stamp covering a record the decision record says %+v", got)
	}
	for name, change := range map[string]func(map[string]any){
		"no lag":                      func(report map[string]any) { delete(member(report, "stamps"), "lag") },
		"a lag with no records":       func(report map[string]any) { delete(member(report, "stamps", "lag"), "records") },
		"a lag with no longest":       func(report map[string]any) { delete(member(report, "stamps", "lag"), "maxSeconds") },
		"a lag with no record":        func(report map[string]any) { member(report, "stamps", "lag")["maxSequence"] = 0 },
		"no time stamped through":     func(report map[string]any) { delete(member(report, "stamps"), "coveredBy") },
		"a time of no form":           func(report map[string]any) { member(report, "stamps")["coveredBy"] = "yesterday" },
		"a negative count":            func(report map[string]any) { member(report, "stamps")["unreadable"] = -1 },
		"revocation counts that miss": func(report map[string]any) { member(report, "stamps")["revocationChecked"] = 1 },
		"no trusted count":            func(report map[string]any) { delete(member(report, "stamps"), "trusted") },
	} {
		r.answers(t, 0, edited(t, stampsReport, change))
		if status, _, refusal := readAudit(t, r.ts, ""); status != http.StatusInternalServerError || !strings.Contains(refusal, "did not answer as documented") {
			t.Errorf("%s: the decision record answered %d %q", name, status, refusal)
		}
	}
}

// **Held to the file read: a roots file whose path names another file is not
// passed.** Where, between Desk's reading of the settings and the check, the
// roots' path comes to name another file, even one of the same bytes, the
// check is given no `--tsa-…`, and the page is told why.
func TestRootsWhosePathNamesAnotherFileAreNotPassed(t *testing.T) {
	r := newStampRig(t)
	r.set(t, r.proposal(nil))
	roots := filepath.Join(r.folder(t), "roots-"+fileDigest([]byte(r.roots))+".pem")
	testHookStampingRead = func() {
		if err := os.Rename(roots, roots+".was"); err != nil {
			t.Error(err)
		}
		if err := os.WriteFile(roots, []byte(r.roots), 0o600); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { testHookStampingRead = nil })
	r.ran(t)
	got := stampingOf(t, r.ts, "")
	if got.Passed || got.PassProblem != "Desk could not hand the runtime the roots it keeps for this desk, so no stamp was checked: the path of a file Desk keeps does not name the file it read." {
		t.Errorf("the decision record says %+v", got)
	}
	if args := verifyTSA(t, r.ran(t)); args != nil {
		t.Errorf("the check was given %q", args)
	}
}

/* With the runtime ------------------------------------------------------------ */

// **With the runtime and a stand-in authority: stamping, end to end.** On a
// desk Desk made, with two deciding runs, the owner sets an authority whose
// root is the stand-in's, with its policy. The decision record passes them:
// no stamp covers a record, and both are pending. "Stamp now": the authority
// stamps the checkpoint at record 2, and the runtime keeps the token. The
// record then shows the trail stamped through record 2, the stamp the runtime
// accepted, the lag for both records, and nothing pending. A third deciding
// run is pending; the scheduler, at the interval, stamps it, and the record
// shows the trail stamped through record 3; at the next interval, with the
// head where it was, the authority is not asked again. Then the authority is
// one that never answers: a fourth deciding run, and "Stamp now" is the
// runtime's JPS-AUDIT-STAMP-UNREACHABLE, in its words, after the runtime's
// own timeout, and the stamps file is as it was.
func TestStampingWithTheRuntime(t *testing.T) {
	bin := requireBinary(t)
	t.Setenv("JPACK_CONFIG", "")
	const id = "a00000000000000000000000000000a1"
	w := fixStamping(t, id)
	fixDeskIDs(t, id)
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
	for range 2 {
		if code, out := decidingRun(t, bin, row.Folder); code != 0 {
			t.Fatalf("a deciding run exited %d: %s", code, out)
		}
	}
	proposal := map[string]any{"authority": tsa.URL + "/", "roots": string(authority.rootPEM()), "policies": []string{"1.3.6.1.4.1.99999.1"}}
	if got := setOn(t, ts, row.ID, proposal, checkOn(t, ts, row.ID, proposal).Token); got.status != http.StatusOK {
		t.Fatalf("the settings were not kept: %d %s", got.status, got.data)
	}
	if got := readFile(t, filepath.Join(row.Folder, "jpack.json")); got != config {
		t.Errorf("jpack.json changed: %s", got)
	}
	_, answer, _ := readAudit(t, ts, row.ID)
	if answer.Report == nil || answer.Report.Coverage.Stamped.Status != "none" || answer.Stamping == nil || !answer.Stamping.Passed ||
		answer.Stamping.Pending == nil || *answer.Stamping.Pending != 2 {
		t.Fatalf("before a stamp the decision record says %+v %+v", answer.Report, answer.Stamping)
	}

	got := stampingCall(t, ts, "/api/audit/stamping/stamp", row.ID, map[string]any{})
	var run struct {
		Run stampRun `json:"run"`
	}
	if got.status != http.StatusOK || json.Unmarshal([]byte(got.data), &run) != nil || run.Run.Status != stampStamped || run.Run.Sequence != 2 ||
		run.Run.Policy != "1.3.6.1.4.1.99999.1" || authority.served() != 1 || strings.Contains(got.data, row.Folder) {
		t.Fatalf("Stamp now answered %d %s, the authority served %d", got.status, got.data, authority.served())
	}
	stampsPath := filepath.Join(row.Folder, ".desk-private", "audit", "stamps.jsonl")
	if lines := strings.Count(readFile(t, stampsPath), "\n"); lines != 1 {
		t.Errorf("the stamps file holds %d lines", lines)
	}
	_, answer, _ = readAudit(t, ts, row.ID)
	report := answer.Report
	if report == nil || report.Coverage.Stamped != (auditCoverageState{Status: "through", Through: 2}) || report.Stamps == nil ||
		report.Stamps.Trusted != 1 || report.Stamps.Lag.Records != 2 || report.Stamps.CoveredBy == "" || answer.Stamping == nil ||
		answer.Stamping.Pending == nil || *answer.Stamping.Pending != 0 {
		t.Fatalf("after the stamp the decision record says %+v %+v", report, answer.Stamping)
	}
	if !slices.ContainsFunc(report.Establishes, func(sentence string) bool { return strings.HasPrefix(sentence, "Lines 1 to 2 existed by ") }) {
		t.Errorf("the runtime's sentence on the stamp is not passed on: %q", report.Establishes)
	}

	if code, out := decidingRun(t, bin, row.Folder); code != 0 {
		t.Fatalf("a deciding run exited %d: %s", code, out)
	}
	if _, answer, _ = readAudit(t, ts, row.ID); answer.Stamping == nil || answer.Stamping.Pending == nil || *answer.Stamping.Pending != 1 {
		t.Fatalf("after a third run the decision record says %+v", answer.Stamping)
	}
	w.at(t, time.Hour)
	if authority.served() != 2 {
		t.Errorf("at the interval, with the head moved, the authority served %d", authority.served())
	}
	if _, answer, _ = readAudit(t, ts, row.ID); answer.Report == nil || answer.Report.Coverage.Stamped != (auditCoverageState{Status: "through", Through: 3}) {
		t.Fatalf("after the scheduler's stamp the decision record says %+v", answer.Report)
	}
	w.at(t, 2*time.Hour)
	if authority.served() != 2 {
		t.Errorf("with the head where it was, the authority served %d", authority.served())
	}

	// An authority that never answers.
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
	if got := setOn(t, ts, row.ID, proposal, checkOn(t, ts, row.ID, proposal).Token); got.status != http.StatusOK {
		t.Fatalf("the settings were not kept: %d %s", got.status, got.data)
	}
	if code, out := decidingRun(t, bin, row.Folder); code != 0 {
		t.Fatalf("a deciding run exited %d: %s", code, out)
	}
	kept := readFile(t, stampsPath)
	got = stampingCall(t, ts, "/api/audit/stamping/stamp", row.ID, map[string]any{})
	if got.status != http.StatusOK || json.Unmarshal([]byte(got.data), &run) != nil || run.Run.Status != stampRefused ||
		!slices.Equal(run.Run.Diagnostics, []runtimeDiagnostic{{"JPS-AUDIT-STAMP-UNREACHABLE", "the time-stamping authority could not be asked, or did not answer: the request was not answered. Nothing was written, and the trail and the decisions in it are as they were; asking again stamps the same checkpoint."}}) {
		t.Errorf("Stamp now against a listener that never answers answered %d %s", got.status, got.data)
	}
	if readFile(t, stampsPath) != kept {
		t.Error("the stamps file changed")
	}
}

// **With the runtime: no path.** The project Desk was started on is in a
// folder whose name holds a space, a tab and U+2028, and Desk's
// configuration folder too. Before any record, "Stamp now" is the runtime's
// refusal naming the trail by its path, and Desk passes it on with none; the
// decision record, which gives the runtime the roots by their path in that
// configuration folder, names neither folder; and the log keeps the path.
func TestStampingQuotesNoPathWithTheRuntime(t *testing.T) {
	bin := requireBinary(t)
	t.Setenv("JPACK_CONFIG", "")
	t.Setenv(runtimeSigningKeyEnv, "")
	project := filepath.Join(t.TempDir(), "Top SECRET\t(desk)  PATH")
	configDir := filepath.Join(t.TempDir(), "Desk SECRET\t  CONFIG")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(mustSchema(t, bin, project), "6") {
		t.Skip("this runtime has no audit commands")
	}
	writeProject(t, project, map[string]string{"packs/a.json": reviewPack, "jpack.json": `{"configVersion":"6","audit":{"dir":".desk-private/audit"},"packs":{"alpha":{"path":"packs/a.json"}}}` + "\n"})
	jpackIn(t, bin, project, "packs", "lock", "--config", "jpack.json", "--format", "json")
	logged := &lockedWriter{w: &bytes.Buffer{}}
	s, ts := startDesk(t, Config{ProjectDir: project, JpackBin: bin, Token: testToken, DeskConfigDir: configDir, Logger: log.New(logged, "", 0)})
	t.Cleanup(func() { s.Close() })
	t.Cleanup(ts.Close)
	authority, _ := newTestAuthority()
	proposal := map[string]any{"authority": "http://127.0.0.1:9/", "roots": string(authority.rootPEM())}
	if got := setOn(t, ts, "", proposal, checkOn(t, ts, "", proposal).Token); got.status != http.StatusOK {
		t.Fatalf("the settings were not kept: %d %s", got.status, got.data)
	}
	leaks := func(t *testing.T, data string) {
		t.Helper()
		for _, leaked := range []string{"SECRET", "PATH", "CONFIG", "(desk)", "/proc/self", project, configDir, "trailPath"} {
			if strings.Contains(data, leaked) {
				t.Errorf("Desk says %q: %s", leaked, data)
			}
		}
	}
	got := stampingCall(t, ts, "/api/audit/stamping/stamp", "", map[string]any{})
	var run struct {
		Run stampRun `json:"run"`
	}
	if got.status != http.StatusOK || json.Unmarshal([]byte(got.data), &run) != nil || run.Run.Status != stampRefused ||
		len(run.Run.Diagnostics) != 1 || run.Run.Diagnostics[0].Code != "JPS-AUDIT-TRAIL-READ" || !strings.Contains(run.Run.Diagnostics[0].Message, "…/evaluations.jsonl") {
		t.Errorf("Stamp now before any record answered %d %s", got.status, got.data)
	}
	leaks(t, got.data)
	// The runtime names the trail through the folder it runs in; the log
	// keeps its words whole.
	if !strings.Contains(logged.String(), "/.desk-private/audit/evaluations.jsonl does not exist yet") {
		t.Errorf("the log does not keep the runtime's words whole: %s", logged.String())
	}
	status, data := reviewCall(t, ts, "GET", "/api/audit/verify", "", nil, bearer)
	if status != http.StatusOK || !strings.Contains(string(data), `"passed":true`) {
		t.Errorf("the decision record answered %d %s", status, data)
	}
	leaks(t, string(data))
	if code, out := decidingRun(t, bin, project); code != 0 {
		t.Fatalf("a deciding run exited %d: %s", code, out)
	}
	got = stampingCall(t, ts, "/api/audit/stamping/stamp", "", map[string]any{})
	if got.status != http.StatusOK || json.Unmarshal([]byte(got.data), &run) != nil || run.Run.Status != stampRefused || run.Run.Diagnostics[0].Code != "JPS-AUDIT-STAMP-UNREACHABLE" {
		t.Errorf("Stamp now against a refused connection answered %d %s", got.status, got.data)
	}
	leaks(t, got.data)
	status, data = reviewCall(t, ts, "GET", "/api/audit/verify", "", nil, bearer)
	if status != http.StatusOK {
		t.Errorf("the decision record answered %d %s", status, data)
	}
	leaks(t, string(data))
	if _, err := fs.Stat(os.DirFS(configDir), "stamping"); err != nil {
		t.Errorf("Desk's configuration folder holds no stamping folder: %v", err)
	}
}
