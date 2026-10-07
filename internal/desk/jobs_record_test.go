//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package desk

// Runner's chain of runs, handed over and checked (ADR-0010, sections 4 and
// 5; delivery rows 8b and 12). A stand-in Runner, an httptest server, serves
// the chain each test sets; the stand-in runtime answers `--trail` from files
// of its own, as runtime 0.27.1 answers it, and keeps what it read through
// `--trail`, so a test sees which copy the runtime was given. The last tests
// drive the real runtime, and the real Runner, and skip without them.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Two identities of Runner's chain of runs: the chain's, and the one it has
// after its store was replaced.
const (
	jobsTrail      = "abe5e3a494c36af588768ce5b0305497"
	jobsMovedTrail = "68e77cefed3b0f20750d9b61ffceec38"
)

// jobsVerified is what runtime 0.27.1 printed, measured, for `audit verify
// --trail .desk-private/handover/jobs-chain.jsonl --format json --expect
// <held>` over a chain of three records, with checkpoints held through the
// second.
const jobsVerified = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.27.1"},"command":"audit verify","trailPath":".desk-private/handover/jobs-chain.jsonl","snapshotBetweenWrites":true,"status":"valid","scope":"checkpoint","lines":3,"bytes":2854,"trail":"abe5e3a494c36af588768ce5b0305497","head":{"checkpointVersion":"1","recordDigest":"sha256:4c492e3c9838c70721a9aa540f069184129f6ac96aac397db5b8957b257fa96d","sequence":3,"trail":"abe5e3a494c36af588768ce5b0305497"},"coverage":{"legacyPrefix":0,"chained":3,"unchained":0,"uncovered":0,"damaged":0,"signed":{"status":"not-checked","detail":"no public key was supplied"},"signedRecords":0,"unsignedRecords":0,"checkpointed":{"status":"through","through":2},"witnessed":2,"unwitnessed":1,"stamped":{"status":"not-checked","detail":"no time-stamping roots were supplied"}},"segments":[{"firstLine":1,"lastLine":3}],"segmentsTotal":1,"discontinuities":[],"discontinuitiesTotal":0,"held":{"supplied":2,"matched":2,"failed":0,"latest":{"checkpointVersion":"1","recordDigest":"sha256:06cef9d78969d822062b8b71415537660b504f555312757bc83a5c4b9b307a54","sequence":2,"trail":"abe5e3a494c36af588768ce5b0305497"},"status":"matched"},"findings":[],"findingsTotal":0,"establishes":["The chained lines are consistent with one another: no line before the last was edited, inserted, deleted or moved without breaking a link, and the lines before the first chained line are the block its previous commits to.","Lines 1 to 2 are the lines that existed when the checkpoint was made, if the checkpoint was held independently of the trail's operator."],"doesNotEstablish":["Lines after 2, the checkpoint's sequence, are not authenticated by the chain: only a later checkpoint covering them, held independently of the operator, shows they are the ones first written.","That the trail is complete after line 2: lines removed from its end since the checkpoint was made are not missed.","That the holder kept every checkpoint handed to it, or that none later than those supplied exists: the coverage reaches only the checkpoints supplied here.","That any record's at is true: it is the operator's clock.","Who wrote any record: no public key was supplied, so no signature was checked.","When any checkpoint existed: no time-stamping roots were supplied, so no stamp was checked.","Whether any evaluation was refused at the gate, rehearsed, or failed before a disposition: the trail records decisions, not attempts, so its silence is not evidence that none were (ADR-0048)."]}`

// jobsCheckpointOf is the checkpoint of the chain of runs' entry at sequence
// in trail, as the stand-in prints it: of a record digest other than the
// desk trail's checkpointOf gives for the same identity and sequence, so that
// the two chains' bytes differ even where their identities do not.
func jobsCheckpointOf(trail string, sequence int) string {
	return fmt.Sprintf(`{"sequence":%d, "trail":"%s","recordDigest":"sha256:%064x","checkpointVersion":"1"}`, sequence, trail, sequence+0x5000) + "\n"
}

// jobsChainOf is the checkpoints of the entries at sequences in trail.
func jobsChainOf(trail string, sequences ...int) string {
	var out strings.Builder
	for _, sequence := range sequences {
		out.WriteString(jobsCheckpointOf(trail, sequence))
	}
	return out.String()
}

// runEntries is a chain of runs as Runner serves it: n entries, each a line
// of compact JSON ended by a newline, with bytes that are not UTF-8 in the
// last, so that a copy that decoded and encoded them again would differ.
func runEntries(trail string, n int) []byte {
	var out bytes.Buffer
	for sequence := 1; sequence <= n; sequence++ {
		fmt.Fprintf(&out, `{"entryVersion":"1","trail":"%s","sequence":%d,"previous":"sha256:%064x","kind":"run","run":"run_%032x","auditDigest":"sha256:%064x"}`, trail, sequence, sequence-1, sequence, sequence)
		if sequence == n {
			out.WriteString(" \xff\xfe")
		}
		out.WriteString("\n")
	}
	return out.Bytes()
}

// withJobsTrail adds to the stand-in at bin an answer for every command given
// `--trail`, ahead of all the others: it keeps the bytes of the file named in
// `<calls>.jobs.read` (or says it was no file there); its `audit checkpoint`
// answers from `<calls>.jobs.lines`, `.jobs.head` and `.jobs.since` as
// checkpointCase answers from its own; and its `audit verify` prints
// `<calls>.jobs.verify` and exits with `<calls>.jobs.verify.exit`.
func withJobsTrail(t *testing.T, bin, calls string) {
	t.Helper()
	script, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	jobs := calls + ".jobs"
	block := "trail=; prev=; for arg do if [ \"$prev\" = --trail ]; then trail=$arg; fi; prev=$arg; done\n" +
		"if [ -n \"$trail\" ]; then\n" +
		"  if [ -f \"$trail\" ] && [ -r \"$trail\" ]; then cat \"$trail\" > '" + jobs + ".read'; else printf 'NOT READ\\n' > '" + jobs + ".read'; fi\n" +
		"  case \"$1 $2\" in\n" +
		checkpointCase(jobs) +
		"'audit verify')\n" +
		"  while IFS= read -r line || [ -n \"$line\" ]; do printf '%s\\n' \"$line\"; done < '" + jobs + ".verify'\n" +
		"  IFS= read -r code < '" + jobs + ".verify.exit'; exit \"$code\"\n" +
		"  ;;\n" +
		"  esac\n" +
		"  exit 64\n" +
		"fi\n"
	script = bytes.Replace(script, []byte("case \"$1 $2\" in\n"), []byte(block+"case \"$1 $2\" in\n"), 1)
	if err := os.WriteFile(bin, script, 0o755); err != nil {
		t.Fatal(err)
	}
}

// jobsRig is a handoverRig whose desk has a Runner: a stand-in that serves
// what serve sets, and records each request it was sent.
type jobsRig struct {
	*handoverRig
	runner *httptest.Server
	mu     sync.Mutex
	handle http.HandlerFunc
	asked  []string
}

func newJobsRig(t *testing.T, ids ...string) *jobsRig {
	t.Helper()
	rig := &jobsRig{handoverRig: newHandoverRig(t, ids...)}
	withJobsTrail(t, rig.bin, rig.calls)
	rig.jobsAnswers(t, 0, jobsVerified)
	rig.runner = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rig.mu.Lock()
		handle := rig.handle
		rig.asked = append(rig.asked, r.Method+" "+r.URL.RequestURI()+" "+r.Header.Get("Authorization"))
		rig.mu.Unlock()
		handle(w, r)
	}))
	// The server's own note of an aborted answer is not this test's.
	rig.runner.Config.ErrorLog = log.New(io.Discard, "", 0)
	rig.runner.Start()
	t.Cleanup(rig.runner.Close)
	rig.s.jobs = &jobsCompanion{url: rig.runner.URL, token: "test-private", done: make(chan struct{}), stop: make(chan struct{})}
	// The stand-in companion has no process to stop.
	t.Cleanup(func() { rig.s.jobs.closed = true })
	rig.serve(t, runEntries(jobsTrail, 3), jobsChainOf(jobsTrail, 1, 2, 3))
	return rig
}

// serve has the stand-in Runner serve chain, and the stand-in runtime give
// lines as the checkpoints of its copy.
func (r *jobsRig) serve(t *testing.T, chain []byte, lines string) {
	t.Helper()
	r.serveWith(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/jsonl")
		w.Write(chain)
	})
	if err := os.WriteFile(r.calls+".jobs.lines", []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
}

// serveWith has the stand-in Runner answer with handle.
func (r *jobsRig) serveWith(handle http.HandlerFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handle = handle
}

// runnerAsked is what the stand-in Runner was sent since the last call.
func (r *jobsRig) runnerAsked() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	asked := r.asked
	r.asked = nil
	return asked
}

// jobsAnswers sets what the stand-in's `audit verify --trail` prints, and
// its exit.
func (r *jobsRig) jobsAnswers(t *testing.T, code int, body string) {
	t.Helper()
	answerAt(t, r.calls+".jobs.verify", code, body)
}

// jobsRead is what the stand-in runtime last read through `--trail`.
func (r *jobsRig) jobsRead(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(r.calls + ".jobs.read")
	if err != nil {
		return ""
	}
	return string(data)
}

// copyPath is Desk's copy of the chain, in the project.
func (r *jobsRig) copyPath() string { return r.handoverPath(jobsChainName) }

// readJobs asks a desk for its Jobs record.
func readJobs(t *testing.T, ts *httptest.Server, desk string) (int, jobsRecordAnswer, []byte) {
	t.Helper()
	status, data := reviewCall(t, ts, "GET", "/api/audit/jobs-verify", desk, nil, bearer)
	var answer jobsRecordAnswer
	if status == http.StatusOK && json.Unmarshal(data, &answer) != nil {
		t.Fatalf("the Jobs record answered %d %s", status, data)
	}
	return status, answer, data
}

// jobsDownload is a download of the chain of runs' checkpoints for holder.
func (r *jobsRig) jobsDownload(t *testing.T, holder string) (int, http.Header, []byte) {
	t.Helper()
	return r.download(t, "holder="+holder+"&chain=jobs")
}

// jobsDownloaded is a download of the chain's checkpoints that must answer
// them.
func (r *jobsRig) jobsDownloaded(t *testing.T, holder string) (http.Header, []byte) {
	t.Helper()
	status, header, data := r.jobsDownload(t, holder)
	if status != http.StatusOK {
		t.Fatalf("the download of the chain's checkpoints answered %d %s", status, data)
	}
	return header, data
}

// jobsConfirmation is the body that confirms, for the chain of runs, the
// download whose headers are header.
func jobsConfirmation(t *testing.T, header http.Header) map[string]any {
	t.Helper()
	body := confirmation(t, header)
	body["chain"] = "jobs"
	return body
}

// jobsConfirmed confirms the chain's download whose headers are header, and
// fails the test unless it is recorded.
func (r *jobsRig) jobsConfirmed(t *testing.T, holder string, header http.Header) holderAnswer {
	t.Helper()
	status, data := r.confirm(t, holder, jobsConfirmation(t, header))
	var answer holderAnswer
	if status != http.StatusOK || json.Unmarshal(data, &answer) != nil {
		t.Fatalf("the confirmation of the chain's checkpoints answered %d %s", status, data)
	}
	return answer
}

// jobsState is the holders' answer's word on the chain, or a zero one.
func jobsState(answer holdersAnswer) jobsChainState {
	if answer.Jobs == nil {
		return jobsChainState{}
	}
	return *answer.Jobs
}

const (
	jobsHeadCall   = "audit checkpoint --trail .desk-private/handover/jobs-chain.jsonl --format json"
	jobsVerifyCall = "audit verify --trail .desk-private/handover/jobs-chain.jsonl --format json"
)

func jobsSinceCall(since int) string {
	return "audit checkpoint --trail .desk-private/handover/jobs-chain.jsonl --since " + strconv.Itoa(since) + " --limit 300"
}

// withoutEnv is calls with the stand-in's note of JPACK_CONFIG taken off.
func withoutEnv(calls []string) []string {
	out := make([]string, len(calls))
	for i, call := range calls {
		out[i] = strings.TrimSuffix(call, " [JPACK_CONFIG=unset]")
	}
	return out
}

// **The Jobs record is the runtime's check of a copy of the chain Runner
// served, with nothing held but the checkpoints handed over.** Desk asks
// Runner for `GET /v1/run-chain` with its token and no query, keeps the bytes
// as served, owner-only, in the hand-over folder, and runs exactly `audit
// verify --trail <copy> --format json`: no `--config`, no `--public-key`, no
// `--require-…`. The answer is the runtime's report, the copy's line count and
// Runner's key as Desk reports it.
func TestJobsRecordChecksACopyOfTheChainRunnerServed(t *testing.T) {
	rig := newJobsRig(t)
	rig.s.jobs.key = &runnerKey{s: rig.s, name: strings.Repeat("ab", 16), status: RunnerKeyStatus{State: runnerKeySigned, PublicKey: standInPublicKey, KeyID: standInKeyID}}
	chain := runEntries(jobsTrail, 3)
	rig.serve(t, chain, jobsChainOf(jobsTrail, 1, 2, 3))
	rig.ran(t)
	status, answer, data := readJobs(t, rig.ts, "")
	if status != http.StatusOK || answer.State != auditStateReport || answer.Report == nil {
		t.Fatalf("the Jobs record answered %d %s", status, data)
	}
	if calls := withoutEnv(rig.ran(t)); !slices.Equal(calls, []string{"packs schema --format json", jobsVerifyCall}) {
		t.Errorf("the Jobs record ran %q, want packs schema and then audit verify of the copy alone", calls)
	}
	if asked := rig.runnerAsked(); !slices.Equal(asked, []string{"GET /v1/run-chain Bearer test-private"}) {
		t.Errorf("Runner was asked %q", asked)
	}
	if got := readFile(t, rig.copyPath()); got != string(chain) || modeOf(rig.copyPath()) != "-rw-------" {
		t.Errorf("Desk's copy is %s %q, want the chain as served, owner-only", modeOf(rig.copyPath()), got)
	}
	if got := rig.jobsRead(t); got != string(chain) {
		t.Errorf("the runtime read %q, want the chain as served", got)
	}
	var want struct {
		Coverage         auditCoverage `json:"coverage"`
		Establishes      []string      `json:"establishes"`
		DoesNotEstablish []string      `json:"doesNotEstablish"`
	}
	if err := json.Unmarshal([]byte(jobsVerified), &want); err != nil {
		t.Fatal(err)
	}
	if answer.Report.Coverage != want.Coverage || !slices.Equal(answer.Report.Establishes, want.Establishes) || !slices.Equal(answer.Report.DoesNotEstablish, want.DoesNotEstablish) {
		t.Errorf("the report is %+v, want the runtime's own", answer.Report)
	}
	if answer.ChainLines == nil || *answer.ChainLines != 3 || answer.Runtime != "0.0.0-stand-in" {
		t.Errorf("the answer counts %v lines, runtime %q", answer.ChainLines, answer.Runtime)
	}
	if answer.RunnerKey == nil || *answer.RunnerKey != (RunnerKeyStatus{State: runnerKeySigned, PublicKey: standInPublicKey, KeyID: standInKeyID}) {
		t.Errorf("Runner's key is reported %+v", answer.RunnerKey)
	}
	if answer.Keys != nil || answer.Signing != nil || answer.Rotation != nil || answer.Files != nil || answer.Expected != 0 {
		t.Errorf("the Jobs record carries the decision record's keys or files: %s", data)
	}
	// A last line with no newline is a line too.
	rig.serve(t, []byte("a\nb\nc"), "")
	if _, answer, _ := readJobs(t, rig.ts, ""); answer.ChainLines == nil || *answer.ChainLines != 3 {
		t.Errorf("a copy of three lines, the last with no newline, is counted %v", answer.ChainLines)
	}
}

// **A chain that does not arrive whole is an error, never a report.** A
// transfer Runner aborts, one closed short of its length or before its last
// chunk, an answer other than 200, and one past Desk's bound each answer an
// error in Desk's words: the runtime checks nothing, nothing is handed over
// or recorded, and the hand-over says it could not read the chain.
func TestJobsRecordAChainThatDidNotArriveWholeIsAnError(t *testing.T) {
	rig := newJobsRig(t, holderA)
	rig.addHolder(t, "Auditor", "e-mail")
	header, _ := rig.jobsDownloaded(t, holderA)
	page := `{"sequence":1}` + "\n"
	raw := func(response string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			connection, buffered, err := http.NewResponseController(w).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			buffered.WriteString(response)
			buffered.Flush()
			connection.Close()
		}
	}
	line := append(bytes.Repeat([]byte("a"), 1024), '\n')
	past := append(bytes.Repeat(line, runChainLimit/len(line)), bytes.Repeat([]byte("b"), runChainLimit%len(line)+1)...)
	for _, tc := range []struct {
		name   string
		runner http.HandlerFunc
		status int
	}{
		{"aborted as Runner aborts it", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/jsonl")
			w.WriteHeader(200)
			w.Write([]byte(page))
			http.NewResponseController(w).Flush()
			panic(http.ErrAbortHandler)
		}, http.StatusBadGateway},
		{"closed short of its length", raw("HTTP/1.1 200 OK\r\nContent-Type: application/jsonl\r\nContent-Length: 1000\r\n\r\n" + page), http.StatusBadGateway},
		{"closed before its last chunk", raw("HTTP/1.1 200 OK\r\nContent-Type: application/jsonl\r\nTransfer-Encoding: chunked\r\n\r\n" + strconv.FormatInt(int64(len(page)), 16) + "\r\n" + page + "\r\n"), http.StatusBadGateway},
		{"a refusal", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(500)
			w.Write([]byte(`{"error":{"code":"store_error","message":"The run store could not be read."}}` + "\n"))
		}, http.StatusBadGateway},
		{"past Desk's bound", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/jsonl")
			w.Write(past)
		}, http.StatusBadGateway},
	} {
		rig.serveWith(tc.runner)
		before := rig.snapshot(t)
		rig.ran(t)
		status, answer, data := readJobs(t, rig.ts, "")
		if status != tc.status || answer.Report != nil || !strings.Contains(refusalOf(data), "Desk could not take a copy of the runner's chain of runs, and nothing was checked") {
			t.Errorf("%s: the Jobs record answered %d %.300s", tc.name, status, data)
		}
		if calls := rig.ran(t); slices.ContainsFunc(calls, func(call string) bool { return strings.HasPrefix(call, "audit ") }) {
			t.Errorf("%s: the runtime was run over a chain that did not arrive whole: %q", tc.name, calls)
		}
		if status, _, data := rig.jobsDownload(t, holderA); status != tc.status || bytes.Contains(data, []byte("checkpointVersion")) {
			t.Errorf("%s: the download answered %d %.300s", tc.name, status, data)
		}
		if status, data := rig.confirm(t, holderA, jobsConfirmation(t, header)); status != tc.status {
			t.Errorf("%s: the confirmation answered %d %.300s", tc.name, status, data)
		}
		if after := rig.snapshot(t); after != before {
			t.Errorf("%s: Desk's record changed:\n%s\nwas\n%s", tc.name, after, before)
		}
		if state := jobsState(rig.holders(t)); state.State != jobsStateUnread || !strings.Contains(state.Problem, "Desk could not take a copy of the runner's chain of runs") {
			t.Errorf("%s: the holders say of the chain %+v", tc.name, state)
		}
	}
}

// **A fresh copy each time.** Each check, each download and each
// confirmation asks Runner again and writes what it served in place of the
// copy before, and the runtime reads that copy: a chain that grew, and one
// that shrank, are each read as served.
func TestJobsRecordTakesAFreshCopyEachTime(t *testing.T) {
	rig := newJobsRig(t, holderA)
	rig.addHolder(t, "Auditor", "e-mail")
	for _, n := range []int{3, 5, 2} {
		chain := runEntries(jobsTrail, n)
		rig.serve(t, chain, jobsChainOf(jobsTrail, upTo(n)...))
		rig.runnerAsked()
		if _, answer, data := readJobs(t, rig.ts, ""); answer.ChainLines == nil || *answer.ChainLines != int64(n) {
			t.Errorf("a chain of %d: the Jobs record answered %s", n, data)
		}
		if got := readFile(t, rig.copyPath()); got != string(chain) {
			t.Errorf("a chain of %d: Desk's copy holds %d bytes, want the chain as served", n, len(got))
		}
		if got := rig.jobsRead(t); got != string(chain) {
			t.Errorf("a chain of %d: the runtime read %d bytes, want the copy just taken", n, len(got))
		}
		os.WriteFile(rig.calls+".jobs.read", nil, 0o600)
		header, _ := rig.jobsDownloaded(t, holderA)
		if got := rig.jobsRead(t); got != string(chain) {
			t.Errorf("a chain of %d: the download read %d bytes, want the copy just taken", n, len(got))
		}
		if asked := rig.runnerAsked(); len(asked) != 2 {
			t.Errorf("a chain of %d: Runner was asked %q, want once for each", n, asked)
		}
		if n == 2 {
			rig.jobsConfirmed(t, holderA, header)
			if asked := rig.runnerAsked(); len(asked) != 1 {
				t.Errorf("the confirmation asked Runner %q, want once", asked)
			}
		}
	}
}

// **`--expect` for each holder's file of the chain, and no other.** One
// holder was handed the desk's trail, one the chain of runs, and one the
// chain before its identity changed. The Jobs record passes the second's
// file alone; the decision record passes the first's alone; and each says so.
func TestJobsRecordPassesEachHoldersFileOfTheChainAlone(t *testing.T) {
	rig := newJobsRig(t, holderA, holderB, holderC)
	rig.chain(t, chainOf(handoverTrail, 1, 2, 3))
	rig.addHolder(t, "Desk auditor", "e-mail")
	rig.addHolder(t, "Jobs auditor", "ticket")
	rig.addHolder(t, "Earlier auditor", "folder")
	header, _ := rig.downloaded(t, holderA)
	rig.confirmed(t, holderA, header)
	rig.serve(t, runEntries(jobsMovedTrail, 2), jobsChainOf(jobsMovedTrail, 1, 2))
	header, _ = rig.jobsDownloaded(t, holderC)
	rig.jobsConfirmed(t, holderC, header)
	rig.serve(t, runEntries(jobsTrail, 3), jobsChainOf(jobsTrail, 1, 2, 3))
	header, _ = rig.jobsDownloaded(t, holderB)
	rig.jobsConfirmed(t, holderB, header)
	rig.ran(t)
	_, answer, data := readJobs(t, rig.ts, "")
	if answer.State != auditStateReport || answer.Expected != 1 || answer.ExpectUnread != nil || answer.HandoverProblem != "" {
		t.Errorf("the Jobs record answered %s", data)
	}
	want := jobsVerifyCall + " --expect .desk-private/handover/" + holderB + "/jobs-" + jobsTrail + ".jsonl"
	if calls := withoutEnv(rig.ran(t)); lastOf(calls) != want || !slices.Contains(calls, jobsHeadCall) {
		t.Errorf("the Jobs record ran %q, want the identity read first and then %q", calls, want)
	}
	if _, decision, _ := readAudit(t, rig.ts, ""); decision.Expected != 1 {
		t.Errorf("the decision record passed %d files", decision.Expected)
	}
	if calls := rig.ran(t); !strings.HasSuffix(lastOf(calls), "--format json --expect .desk-private/handover/"+holderA+"/"+handoverTrail+".jsonl [JPACK_CONFIG=unset]") {
		t.Errorf("the decision record ran %q, want the desk trail's file alone", lastOf(calls))
	}
}

// **A holder's file of the chain that cannot be read now, or is not what Desk
// recorded, is passed over and named**, and the check runs without it.
func TestJobsRecordNamesAHeldFileItCannotRead(t *testing.T) {
	rig := newJobsRig(t, holderA, holderB)
	rig.addHolder(t, "Auditor", "e-mail")
	rig.addHolder(t, "Counterparty", "ticket")
	for _, id := range []string{holderA, holderB} {
		header, _ := rig.jobsDownloaded(t, id)
		rig.jobsConfirmed(t, id, header)
	}
	held := rig.handoverPath(holderA, "jobs-"+jobsTrail+".jsonl")
	whole := readFile(t, held)
	onlyB := " --expect .desk-private/handover/" + holderB + "/jobs-" + jobsTrail + ".jsonl"
	for _, tc := range []struct {
		name   string
		break_ func()
	}{
		{"readable by others", func() { os.Chmod(held, 0o640) }},
		{"not to be opened", func() { os.Chmod(held, 0o200) }},
		{"cut short", func() { os.WriteFile(held, []byte(whole[:strings.Index(whole, "\n")+1]), 0o600) }},
		{"gone", func() { os.Remove(held) }},
	} {
		tc.break_()
		rig.ran(t)
		_, answer, data := readJobs(t, rig.ts, "")
		if answer.State != auditStateReport || answer.Expected != 1 || !slices.Equal(answer.ExpectUnread, []string{"Auditor"}) {
			t.Errorf("%s: the Jobs record answered %s", tc.name, data)
		}
		if calls := withoutEnv(rig.ran(t)); lastOf(calls) != jobsVerifyCall+onlyB {
			t.Errorf("%s: the Jobs record ran %q, want B's file alone", tc.name, lastOf(calls))
		}
		os.Remove(held)
		if err := os.WriteFile(held, []byte(whole), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, answer, _ := readJobs(t, rig.ts, ""); answer.Expected != 2 || answer.ExpectUnread != nil {
		t.Errorf("mended, the Jobs record answered %+v", answer)
	}
}

// **The chain of runs has a cursor of its own, apart from the desk's
// trail's**, even where the two have one identity. Its download is named for
// the chain, starts after the holder's cursor for the chain, and is
// confirmed into the record's `jobs` and the file `jobs-<identity>.jsonl`,
// never into the desk trail's; nothing new is 204, a chain shorter than what
// was handed over is 409, and a stale confirmation is 409 "stale", each as
// for the desk's trail.
func TestJobsHandOverKeepsACursorApartFromTheDeskTrails(t *testing.T) {
	rig := newJobsRig(t, holderA)
	rig.chain(t, chainOf(handoverTrail, 1, 2, 3))
	rig.serve(t, runEntries(handoverTrail, 5), jobsChainOf(handoverTrail, upTo(5)...))
	rig.addHolder(t, "Auditor", "e-mail")
	header, desk := rig.downloaded(t, holderA)
	rig.confirmed(t, holderA, header)
	recordBefore := readFile(t, rig.handoverPath(holderA, handoverRecordName))
	rig.checkpointCalls(t)
	header, jobs := rig.jobsDownloaded(t, holderA)
	if string(jobs) != jobsChainOf(handoverTrail, upTo(5)...) || header.Get(checkpointsFromHeader) != "0" || header.Get(checkpointsThroughHeader) != "5" ||
		header.Get("Content-Disposition") != `attachment; filename="jobs-checkpoints-`+handoverTrail+`-1-5.jsonl"` || header.Get(checkpointsDigestHeader) != digestOfString(string(jobs)) {
		t.Fatalf("the chain's download answered %v\n%s", header, jobs)
	}
	if calls := rig.checkpointCalls(t); !slices.Equal(calls, []string{jobsHeadCall, jobsSinceCall(0)}) {
		t.Errorf("the chain's download ran %q", calls)
	}
	// A confirmation of the desk trail's download as the chain's is stale:
	// other bytes.
	stale := confirmation(t, http.Header{checkpointsTrailHeader: {handoverTrail}, checkpointsFromHeader: {"0"}, checkpointsThroughHeader: {"3"}, checkpointsDigestHeader: {digestOfString(string(desk))}})
	stale["chain"] = "jobs"
	if status, data := rig.confirm(t, holderA, stale); status != http.StatusConflict || !bytes.Contains(data, []byte(`"reason":"stale"`)) {
		t.Errorf("the desk trail's file confirmed as the chain's answered %d %s", status, data)
	}
	shown := rig.jobsConfirmed(t, holderA, header)
	if shown.Jobs[handoverTrail].Through != 5 || shown.Trails[handoverTrail].Through != 3 {
		t.Errorf("the confirmation answered %+v", shown)
	}
	var record handoverRecord
	if err := json.Unmarshal([]byte(readFile(t, rig.handoverPath(holderA, handoverRecordName))), &record); err != nil {
		t.Fatal(err)
	}
	if record.Trails[handoverTrail].Through != 3 || record.Jobs[handoverTrail] != (handedOver{From: 0, Through: 5, ConfirmedAt: handoverNow, Digest: digestOfString(string(jobs))}) {
		t.Errorf("the record is %+v", record)
	}
	if got := readFile(t, rig.handoverPath(holderA, handoverTrail+".jsonl")); got != string(desk) {
		t.Errorf("the desk trail's file holds\n%s", got)
	}
	if got := readFile(t, rig.handoverPath(holderA, "jobs-"+handoverTrail+".jsonl")); got != string(jobs) {
		t.Errorf("the chain's file holds\n%s", got)
	}
	if !strings.HasPrefix(recordBefore, `{"version":"1","trails":`) || strings.Contains(recordBefore, `"jobs"`) {
		t.Errorf("a record with no hand-over of the chain is written %s", recordBefore)
	}
	// Nothing new on either: 204, each.
	if status, header, data := rig.jobsDownload(t, holderA); status != http.StatusNoContent || len(data) != 0 || header.Get(checkpointsTrailHeader) != handoverTrail {
		t.Errorf("nothing new on the chain answered %d %s", status, data)
	}
	if status, _, data := rig.download(t, "holder="+holderA); status != http.StatusNoContent {
		t.Errorf("nothing new on the trail answered %d %s", status, data)
	}
	// The chain grows: its download starts after 5; the trail grows: its
	// download starts after 3.
	rig.serve(t, runEntries(handoverTrail, 6), jobsChainOf(handoverTrail, upTo(6)...))
	rig.chain(t, chainOf(handoverTrail, 1, 2, 3, 4))
	listed := rig.holders(t)
	if entry := holderAt(listed, 0); since(entry.Jobs[handoverTrail]) != 1 || since(entry.Trails[handoverTrail]) != 1 || jobsState(listed).Chain == nil || jobsState(listed).Chain.Sequence != 6 {
		t.Errorf("the holders answered %+v %+v", listed.Holders, listed.Jobs)
	}
	if header, data := rig.jobsDownloaded(t, holderA); header.Get(checkpointsFromHeader) != "5" || string(data) != jobsCheckpointOf(handoverTrail, 6) {
		t.Errorf("the chain's next download answered %v %s", header, data)
	}
	if header, data := rig.downloaded(t, holderA); header.Get(checkpointsFromHeader) != "3" || string(data) != checkpointOf(handoverTrail, 4) {
		t.Errorf("the trail's next download answered %v %s", header, data)
	}
	// The chain shorter than what was handed over: 409, and nothing served.
	rig.serve(t, runEntries(handoverTrail, 4), jobsChainOf(handoverTrail, upTo(4)...))
	if status, _, data := rig.jobsDownload(t, holderA); status != http.StatusConflict || !strings.Contains(refusalOf(data), "the trail is shorter than what was handed over") {
		t.Errorf("a chain shorter than what was handed over answered %d %s", status, data)
	}
	// A replay of the first confirmation, from a cursor no longer the holder's:
	// stale, and nothing changes.
	before := rig.snapshot(t)
	if status, data := rig.confirm(t, holderA, jobsConfirmation(t, header)); status != http.StatusConflict || !bytes.Contains(data, []byte(`"reason":"stale"`)) {
		t.Errorf("a replayed confirmation answered %d %s", status, data)
	}
	if rig.snapshot(t) != before {
		t.Error("a stale confirmation changed Desk's record")
	}
}

// **A chain moved to another identity starts each holder at 0 for it**, and
// the holder's record of the chain before is kept and said.
func TestJobsHandOverStartsAMovedChainAtZero(t *testing.T) {
	rig := newJobsRig(t, holderA)
	rig.addHolder(t, "Auditor", "e-mail")
	header, _ := rig.jobsDownloaded(t, holderA)
	rig.jobsConfirmed(t, holderA, header)
	rig.serve(t, runEntries(jobsMovedTrail, 2), jobsChainOf(jobsMovedTrail, 1, 2))
	listed := rig.holders(t)
	if entry := holderAt(listed, 0); !entry.OtherJobsChain || entry.OtherTrail || entry.Jobs[jobsTrail].Through != 3 || since(entry.Jobs[jobsTrail]) != -1 {
		t.Errorf("the holders answered %+v", entry)
	}
	if header, data := rig.jobsDownloaded(t, holderA); header.Get(checkpointsFromHeader) != "0" || header.Get(checkpointsTrailHeader) != jobsMovedTrail || string(data) != jobsChainOf(jobsMovedTrail, 1, 2) {
		t.Errorf("the moved chain's download answered %v %s", header, data)
	}
}

// **Where there is no Runner, or it is not running, the Jobs record and the
// hand-over say so and offer nothing**: no runtime is run for the chain,
// nothing is downloaded, and nothing is recorded.
func TestJobsRecordSaysWhereThereIsNoRunnerOrItIsNotRunning(t *testing.T) {
	rig := newJobsRig(t, holderA)
	rig.addHolder(t, "Auditor", "e-mail")
	header, _ := rig.jobsDownloaded(t, holderA)
	runner := rig.s.jobs
	for _, tc := range []struct {
		name   string
		jobs   *jobsCompanion
		state  string
		status int
		words  string
	}{
		{"no Runner", nil, jobsStateNoRunner, http.StatusConflict, noRunnerWords},
		{"Runner not running", &jobsCompanion{}, jobsStateNotRunning, http.StatusServiceUnavailable, runnerNotRunningWords},
	} {
		rig.s.jobs = tc.jobs
		before := rig.snapshot(t)
		rig.ran(t)
		status, answer, data := readJobs(t, rig.ts, "")
		if status != http.StatusOK || answer.State != tc.state || answer.Report != nil || answer.ChainLines != nil {
			t.Errorf("%s: the Jobs record answered %d %s", tc.name, status, data)
		}
		if calls := rig.ran(t); slices.ContainsFunc(calls, func(call string) bool { return strings.Contains(call, "--trail") }) {
			t.Errorf("%s: the runtime was run over the chain: %q", tc.name, calls)
		}
		if state := jobsState(rig.holders(t)); state.State != tc.state || state.Chain != nil {
			t.Errorf("%s: the holders say of the chain %+v", tc.name, state)
		}
		if status, _, data := rig.jobsDownload(t, holderA); status != tc.status || refusalOf(data) != tc.words {
			t.Errorf("%s: the download answered %d %s", tc.name, status, data)
		}
		if status, data := rig.confirm(t, holderA, jobsConfirmation(t, header)); status != tc.status || refusalOf(data) != tc.words {
			t.Errorf("%s: the confirmation answered %d %s", tc.name, status, data)
		}
		if rig.snapshot(t) != before {
			t.Errorf("%s: Desk's record changed", tc.name)
		}
		// The desk's own trail is handed over as before.
		if status, _, _ := rig.download(t, "holder="+holderA); status != http.StatusConflict && status != http.StatusOK {
			t.Errorf("%s: the trail's download answered %d", tc.name, status)
		}
	}
	rig.s.jobs = runner
}

// **The Jobs record runs only where the runtime has the command**: with a
// runtime that reads no "6", or whose `audit verify` the command parser says
// does not exist, it says the decision record's older-runtime sentence and
// asks Runner for nothing.
func TestJobsRecordWithAnOlderRuntime(t *testing.T) {
	for _, tc := range []struct {
		name, versions string
		absent         bool
	}{
		{"no 6", `["1","2","3","4","5"]`, false},
		{"no audit verify", withAuditVersions, true},
	} {
		rig := newJobsRig(t)
		if !tc.absent {
			script, _ := os.ReadFile(rig.bin)
			script = bytes.Replace(script, []byte(withAuditVersions), []byte(tc.versions), 1)
			os.WriteFile(rig.bin, script, 0o755)
		} else {
			rig.jobsAnswers(t, 3, auditUnknownCommand)
		}
		rig.runnerAsked()
		status, answer, data := readJobs(t, rig.ts, "")
		if status != http.StatusOK || answer.State != auditStateOlder || answer.Floor != auditRuntimeFloor || answer.Report != nil {
			t.Errorf("%s: the Jobs record answered %d %s", tc.name, status, data)
		}
		if asked := rig.runnerAsked(); !tc.absent && len(asked) != 0 {
			t.Errorf("%s: Runner was asked %q", tc.name, asked)
		}
	}
}

// **A download or a confirmation asks for the chain in one way only.**
// `chain=jobs`, once, beside the holder, or nothing; `"chain":"jobs"` in a
// confirmation, or nothing. Anything else is refused and runs nothing.
func TestJobsHandOverIsAskedForInOneWay(t *testing.T) {
	rig := newJobsRig(t, holderA)
	rig.addHolder(t, "Auditor", "e-mail")
	header, _ := rig.jobsDownloaded(t, holderA)
	rig.runnerAsked()
	for _, query := range []string{"holder=" + holderA + "&chain=trail", "holder=" + holderA + "&chain=jobs&chain=jobs", "holder=" + holderA + "&chain=", "holder=" + holderA + "&chain=JOBS", "holder=" + holderA + "&chain=jobs&x=1", "chain=jobs"} {
		if status, _, data := rig.download(t, query); status != http.StatusBadRequest {
			t.Errorf("%s: answered %d %s", query, status, data)
		}
	}
	for _, chain := range []any{"", "trail", "JOBS", 1, true} {
		body := jobsConfirmation(t, header)
		body["chain"] = chain
		if status, data := rig.confirm(t, holderA, body); status != http.StatusBadRequest {
			t.Errorf("a confirmation with chain %v answered %d %s", chain, status, data)
		}
	}
	if asked := rig.runnerAsked(); len(asked) != 0 {
		t.Errorf("refused requests asked Runner %q", asked)
	}
}

// **A copy the runtime cannot read is said, never taken for an empty
// chain.** The runtime's refusal to read the copy is shown in its words on
// the holders' answer, and a download is refused with them; the desk trail's
// "no trail yet" rule is not applied to the copy.
func TestJobsHandOverSaysACopyTheRuntimeCannotRead(t *testing.T) {
	rig := newJobsRig(t, holderA)
	rig.addHolder(t, "Auditor", "e-mail")
	trailRead := `{"outputVersion":"2","command":"audit checkpoint","status":"error","diagnostics":[{"code":"JPS-AUDIT-TRAIL-READ","message":"The trail .desk-private/handover/jobs-chain.jsonl could not be opened as one regular file."}]}`
	answerAt(t, rig.calls+".jobs.head", 4, trailRead)
	// The desk's own trail is not there: its rule would take that for none.
	listed := rig.holders(t)
	if state := jobsState(listed); state.State != jobsStateUnread || len(state.Diagnostics) != 1 || state.Diagnostics[0].Code != "JPS-AUDIT-TRAIL-READ" {
		t.Errorf("the holders say of the chain %+v", state)
	}
	if status, _, data := rig.jobsDownload(t, holderA); status != http.StatusConflict || !strings.Contains(refusalOf(data), "could not be opened as one regular file") {
		t.Errorf("the download answered %d %s", status, data)
	}
	// An empty chain: no run is chained yet.
	os.Remove(rig.calls + ".jobs.head")
	rig.serve(t, nil, "")
	if state := jobsState(rig.holders(t)); state.State != jobsStateEmpty {
		t.Errorf("an empty chain is said %+v", state)
	}
	if status, _, data := rig.jobsDownload(t, holderA); status != http.StatusConflict || refusalOf(data) != noRunChainedWords {
		t.Errorf("an empty chain's download answered %d %s", status, data)
	}
}

// **No path reaches the page.** The runtime's refusal names the project's
// folder: the Jobs record, the holders' answer and a download each say it
// with "the project's folder" in its place; Desk's log keeps it whole.
func TestJobsRecordQuotesNoPath(t *testing.T) {
	rig := newJobsRig(t, holderA)
	rig.addHolder(t, "Auditor", "e-mail")
	copyPath := filepath.Join(rig.project, ".desk-private", "handover", "jobs-chain.jsonl")
	refused := `{"outputVersion":"2","command":"audit verify","status":"error","diagnostics":[{"code":"JPS-AUDIT-TRAIL-READ","message":"The trail ` + copyPath + ` could not be opened as one regular file."}]}`
	rig.jobsAnswers(t, 4, refused)
	answerAt(t, rig.calls+".jobs.head", 4, strings.Replace(refused, "audit verify", "audit checkpoint", 1))
	status, answer, data := readJobs(t, rig.ts, "")
	if status != http.StatusOK || answer.State != auditStateUnverified || len(answer.Diagnostics) != 1 || answer.HandoverProblem == "" {
		t.Errorf("the Jobs record answered %d %s", status, data)
	}
	_, listed := reviewCall(t, rig.ts, "GET", "/api/audit/holders", "", nil, bearer)
	_, _, downloaded := rig.jobsDownload(t, holderA)
	for _, said := range [][]byte{data, listed, downloaded} {
		if bytes.Contains(said, []byte(rig.project)) || !bytes.Contains(said, []byte("jobs-chain.jsonl")) {
			t.Errorf("the answer says where the project is, or not which file: %s", said)
		}
	}
	if !strings.Contains(rig.logged.String(), copyPath) {
		t.Errorf("Desk's log does not keep the runtime's words whole: %s", rig.logged)
	}
}

/* With the runtime ----------------------------------------------------------- */

// chainedByTheRuntime is a trail the runtime at bin chained, of n deciding
// runs, in a project of its own: what a test serves as Runner's chain. It
// answers the project's folder and the trail's path.
func chainedByTheRuntime(t *testing.T, bin string, n int) (string, string) {
	t.Helper()
	lab := t.TempDir()
	writeProject(t, lab, map[string]string{
		"jpack.json":   `{"configVersion":"6","audit":{"dir":".desk-private/audit"},"packs":{"alpha":{"path":"packs/a.json"}}}` + "\n",
		"packs/a.json": reviewPack,
	})
	if err := os.MkdirAll(filepath.Join(lab, ".desk-private", "audit"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(lab, ".desk-private"), 0o700); err != nil {
		t.Fatal(err)
	}
	for range n {
		evaluateIn(t, bin, lab)
	}
	return lab, filepath.Join(lab, ".desk-private", "audit", "evaluations.jsonl")
}

// runtimeDesk is a startup desk with the runtime at bin, over a project that
// keeps a trail, whose Runner is a stand-in serving what the file at chain
// holds at each request.
func runtimeDesk(t *testing.T, bin, project, chain string) (*Server, *httptest.Server) {
	t.Helper()
	writeProject(t, project, map[string]string{"jpack.json": auditedConfig})
	runner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := os.ReadFile(chain)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/jsonl")
		w.Write(data)
	}))
	t.Cleanup(runner.Close)
	s, ts := startDesk(t, Config{ProjectDir: project, JpackBin: bin, Token: testToken, Logger: log.New(io.Discard, "", 0)})
	s.jobs = &jobsCompanion{url: runner.URL, token: "test-private", done: make(chan struct{}), stop: make(chan struct{})}
	t.Cleanup(func() { s.Close() })
	t.Cleanup(ts.Close)
	t.Cleanup(func() { s.jobs.closed = true })
	return s, ts
}

// **With the runtime, end to end.** A chain the runtime chained, of three
// records, served as Runner's: the download is the runtime's own `audit
// checkpoint --trail --since 0` of it, byte for byte; once confirmed, the
// Jobs record's `audit verify --trail` witnesses every record through the one
// confirmed; one more record is unwitnessed; and the next download is its
// line alone.
func TestJobsRecordWithTheRuntime(t *testing.T) {
	bin := requireBinary(t)
	t.Setenv("JPACK_CONFIG", "")
	fixHandover(t, holderA)
	lab, trail := chainedByTheRuntime(t, bin, 3)
	_, ts := runtimeDesk(t, bin, t.TempDir(), trail)
	if status, data := reviewCall(t, ts, "POST", "/api/audit/holders", "", map[string]string{"label": "Auditor", "channel": "e-mail"}, bearer); status != http.StatusCreated {
		if status == http.StatusConflict && strings.Contains(refusalOf(data), "has no audit commands") {
			t.Skip("this runtime has no audit commands")
		}
		t.Fatalf("adding a holder answered %d %s", status, data)
	}
	get := func(query string) (int, http.Header, []byte) {
		request, _ := http.NewRequest("GET", ts.URL+"/api/audit/checkpoints?"+query, nil)
		bearer(request)
		response, err := ts.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, _ := io.ReadAll(response.Body)
		return response.StatusCode, response.Header, data
	}
	status, header, first := get("holder=" + holderA + "&chain=jobs")
	if want := jpackIn(t, bin, lab, "audit", "checkpoint", "--trail", trail, "--since", "0", "--limit", "300"); status != http.StatusOK || !bytes.Equal(first, want) || bytes.Count(first, []byte("\n")) != 3 {
		t.Fatalf("the download answered %d\n%s\nwant the runtime's own\n%s", status, first, want)
	}
	if header.Get(checkpointsThroughHeader) != "3" || !strings.HasPrefix(header.Get("Content-Disposition"), `attachment; filename="jobs-checkpoints-`) {
		t.Errorf("the download's headers are %v", header)
	}
	if status, data := reviewCall(t, ts, "POST", "/api/audit/holders/"+holderA+"/confirm", "", jobsConfirmation(t, header), bearer); status != http.StatusOK {
		t.Fatalf("the confirmation answered %d %s", status, data)
	}
	_, answer, data := readJobs(t, ts, "")
	if answer.State != auditStateReport || answer.Report == nil || answer.Report.Status != "valid" || answer.Expected != 1 ||
		answer.Report.Coverage.Witnessed != 3 || answer.Report.Coverage.Unwitnessed != 0 ||
		answer.Report.Coverage.Checkpointed != (auditCoverageState{Status: "through", Through: 3}) || answer.ChainLines == nil || *answer.ChainLines != 3 {
		t.Fatalf("after the hand-over the Jobs record answered %s", data)
	}
	if answer.Report.Coverage.Signed.Status != "not-checked" {
		t.Errorf("the Jobs record checked signatures: %+v", answer.Report.Coverage.Signed)
	}
	evaluateIn(t, bin, lab)
	if _, answer, data := readJobs(t, ts, ""); answer.Report == nil || answer.Report.Coverage.Witnessed != 3 || answer.Report.Coverage.Unwitnessed != 1 {
		t.Errorf("after one more record the Jobs record answered %s", data)
	}
	status, header, second := get("holder=" + holderA + "&chain=jobs")
	if status != http.StatusOK || bytes.Count(second, []byte("\n")) != 1 || !bytes.Contains(second, []byte(`"sequence":4,`)) || header.Get(checkpointsFromHeader) != "3" {
		t.Fatalf("the second download answered %d %v\n%s", status, header, second)
	}
}

// **With the runtime: no path reaches the page.** In a project whose path
// holds a space, a tab and a line separator, a copy the runtime cannot open:
// its refusal, measured on 0.27.1, names the copy by the path Desk gave it,
// relative to the project, and the Jobs record, the holders' answer and a
// download each say which file, and not where.
func TestJobsRecordQuotesNoPathWithTheRuntime(t *testing.T) {
	bin := requireBinary(t)
	t.Setenv("JPACK_CONFIG", "")
	fixHandover(t, holderA)
	project := filepath.Join(t.TempDir(), "Top SECRET\tproject TAIL")
	if err := os.MkdirAll(filepath.Join(project, ".desk-private", "audit"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, trail := chainedByTheRuntime(t, bin, 1)
	s, ts := runtimeDesk(t, bin, project, trail)
	_ = s
	if status, data := reviewCall(t, ts, "POST", "/api/audit/holders", "", map[string]string{"label": "Auditor", "channel": "e-mail"}, bearer); status != http.StatusCreated {
		if status == http.StatusConflict && strings.Contains(refusalOf(data), "has no audit commands") {
			t.Skip("this runtime has no audit commands")
		}
		t.Fatalf("adding a holder answered %d %s", status, data)
	}
	copyPath := filepath.Join(project, ".desk-private", "handover", jobsChainName)
	testHookJobsChainCopied = func() { os.Chmod(copyPath, 0o200) }
	t.Cleanup(func() { testHookJobsChainCopied = nil })
	status, answer, data := readJobs(t, ts, "")
	if status != http.StatusOK || answer.State != auditStateUnverified || len(answer.Diagnostics) == 0 || answer.Diagnostics[0].Code != "JPS-AUDIT-TRAIL-READ" {
		t.Errorf("the Jobs record answered %d %s", status, data)
	}
	_, listed := reviewCall(t, ts, "GET", "/api/audit/holders", "", nil, bearer)
	request, _ := http.NewRequest("GET", ts.URL+"/api/audit/checkpoints?holder="+holderA+"&chain=jobs", nil)
	bearer(request)
	response, err := ts.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	downloaded, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Errorf("the download answered %d %s", response.StatusCode, downloaded)
	}
	for _, said := range [][]byte{data, listed, downloaded} {
		for _, leaked := range []string{"SECRET", "TAIL", project, "/tmp", "/proc"} {
			if strings.Contains(string(said), leaked) {
				t.Errorf("the answer says %q: %s", leaked, said)
			}
		}
		if !bytes.Contains(said, []byte("jobs-chain.jsonl")) {
			t.Errorf("the answer does not say which file: %s", said)
		}
	}
}

/* With the pinned Runner --------------------------------------------------- */

// **Runner's chain, handed over and checked, end to end**, with the pinned
// Runner and runtime. A desk's Runner completes one mapped run; the chain of
// runs it serves holds that run's entry; its checkpoint is handed over to a
// holder and confirmed; and the Jobs record's `audit verify --trail` over a
// fresh copy is valid, with the one entry witnessed. Desk's copy is the chain
// the Runs page downloads, byte for byte.
func TestJobsRealCompanionHandsTheChainOverAndChecksIt(t *testing.T) {
	bin, runtime := os.Getenv("JPACK_RUNNER_TEST_BIN"), os.Getenv("JPACK_BIN")
	if bin == "" || runtime == "" {
		t.Skip("set JPACK_RUNNER_TEST_BIN and JPACK_BIN for companion integration")
	}
	t.Setenv("JPACK_CONFIG", "")
	t.Setenv("JPACK_SIGNING_KEY", "")
	fixHandover(t, holderA)
	config, project := t.TempDir(), t.TempDir()
	if err := os.Chmod(config, 0o700); err != nil {
		t.Fatal(err)
	}
	writeProject(t, project, map[string]string{"jpack.json": auditedConfig})
	s, ts := startDesk(t, Config{RunnerBin: bin, JpackBin: runtime, RunnerAllowUntestedReleases: true, ProjectDir: project, DeskConfigDir: config, Token: testToken})
	defer ts.Close()
	defer s.Close()
	call := func(method, path, body string, header ...string) (int, []byte) {
		t.Helper()
		r, err := http.NewRequest(method, ts.URL+"/api/"+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Authorization", "Bearer "+testToken)
		r.Header.Set("Content-Type", "application/json")
		for i := 0; i+1 < len(header); i += 2 {
			r.Header.Set(header[i], header[i+1])
		}
		response, err := ts.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, data
	}
	pack, _ := json.Marshal(`{"specVersion":"0.2.0-draft","id":"https://example.invalid/judgment-packs/scored","version":"0.1.0","title":"A scored pack","decision":{"intent":"Decide the one thing this pack decides.","question":"Does this request proceed?"},"outcomes":[{"id":"proceed","label":"Proceed"},{"id":"hold","label":"Hold"}],"rules":[{"id":"seven","description":"Proceed at seven.","when":{"op":"fact","path":"/score","operator":"equals","value":7},"outcome":"proceed","onUnknown":"escalate"}]}`)
	input := `{"source":{"mapping":{"version":2,"case":{"facts":[{"target":"/score","source":"/score"}],"evidence":[]},"sources":[]},"case":{"score":7}}}`
	status, data := call("POST", "operations/previews", `{"pack":`+string(pack)+`,"input":`+input+`}`)
	var release struct{ ID string }
	if status != 201 || json.Unmarshal(data, &release) != nil || release.ID == "" {
		t.Fatalf("preview: %d %s", status, data)
	}
	status, data = call("POST", "operations/jobs", `{"name":"Chained","releaseId":"`+release.ID+`","reviewed":true}`)
	var job struct{ ID string }
	if status != 201 || json.Unmarshal(data, &job) != nil || job.ID == "" {
		t.Fatalf("job: %d %s", status, data)
	}
	status, data = call("POST", "operations/jobs/"+job.ID+"/runs", input, "Idempotency-Key", "chained")
	var run struct{ ID, State string }
	if status != 202 || json.Unmarshal(data, &run) != nil || run.ID == "" {
		t.Fatalf("run: %d %s", status, data)
	}
	for deadline := time.Now().Add(60 * time.Second); run.State != "completed"; {
		if run.State == "failed" || run.State == "interrupted" || time.Now().After(deadline) {
			t.Fatalf("the run did not complete: %s", data)
		}
		time.Sleep(200 * time.Millisecond)
		if status, data = call("GET", "operations/runs/"+run.ID, ""); status != 200 || json.Unmarshal(data, &run) != nil {
			t.Fatalf("run: %d %s", status, data)
		}
	}
	if status, data := call("POST", "audit/holders", `{"label":"Auditor","channel":"e-mail"}`); status != http.StatusCreated {
		t.Fatalf("adding a holder answered %d %s", status, data)
	}
	request, _ := http.NewRequest("GET", ts.URL+"/api/audit/checkpoints?holder="+holderA+"&chain=jobs", nil)
	bearer(request)
	response, err := ts.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	checkpoints, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || bytes.Count(checkpoints, []byte("\n")) != 1 || response.Header.Get(checkpointsThroughHeader) != "1" {
		t.Fatalf("the download answered %d %v\n%s", response.StatusCode, response.Header, checkpoints)
	}
	confirm, _ := json.Marshal(jobsConfirmation(t, response.Header))
	if status, data := call("POST", "audit/holders/"+holderA+"/confirm", string(confirm)); status != http.StatusOK {
		t.Fatalf("the confirmation answered %d %s", status, data)
	}
	status, data = call("GET", "audit/jobs-verify", "")
	var answer jobsRecordAnswer
	if status != http.StatusOK || json.Unmarshal(data, &answer) != nil || answer.State != auditStateReport || answer.Report == nil ||
		answer.Report.Status != "valid" || answer.Report.Coverage.Witnessed != 1 || answer.Report.Coverage.Unwitnessed != 0 || answer.Expected != 1 ||
		answer.ChainLines == nil || *answer.ChainLines != 1 || answer.RunnerKey == nil {
		t.Fatalf("the Jobs record answered %d %s", status, data)
	}
	status, chain := call("GET", "operations/run-chain", "")
	if copied := readFile(t, filepath.Join(project, ".desk-private", "handover", jobsChainName)); status != 200 || copied != string(chain) {
		t.Errorf("Desk's copy is not the chain the Runs page downloads: %d\n%s\n%s", status, copied, chain)
	}
	if out, err := exec.Command(runtime, "audit", "verify", "--trail", filepath.Join(project, ".desk-private", "handover", jobsChainName), "--expect", filepath.Join(project, ".desk-private", "handover", holderA, "jobs-"+response.Header.Get(checkpointsTrailHeader)+".jsonl"), "--format", "json").Output(); err != nil || !bytes.Contains(out, []byte(`"witnessed":1`)) {
		t.Errorf("a holder's own check of the copy with the file handed over: %v %s", err, out)
	}
}
