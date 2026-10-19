//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package desk

// Each Jobs run's signer, held to the list of public keys Desk keeps for its
// Runner (issue #345; jobs_signing.go), and the key Runner is told to expect
// on its boot line. A stand-in Runner, an httptest server, serves the runs
// and the export each test sets, and a stand-in jpack-runner, by absolute
// path, answers verify-run; the last test drives the pinned Runner and
// runtime, and skips without them.

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
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

// Two keys a list can name, and a third it never does.
const (
	signerA = "882a7f2be72a4b0c0a03b590300c72e8ed3fab24355a6950f9e6399814c67350"
	signerB = "3d4017c3e843895a92b70aa74d1b7ebc9c982ccf2ec4968cc0cd55f12af4660c"
	signerC = "fc51cd8e6218a1a38da47ed00230f0580816ed13ba3303ac5deb911548908025"
)

// The six reasons Runner v0.8.0 gives for a failed signing (openapi.json,
// `Run.signing.reason`), word for word.
var runnerSigningReasons = []string{
	"The record's signature sidecar could not be read.",
	"The record's signature sidecar names a key that is not the expected one.",
	"The record's signature sidecar does not verify under the expected key.",
	"The Runtime signed nothing: the key at the signing key's path is not the expected key.",
	"The Runtime signed nothing, and the key at the signing key's path could not be read as a key.",
	"The record carries no signature.",
}

// **The boot line names the list's key as the one to expect** (issue #345).
// Where Desk names Runner its key, the boot line carries `signingPublicKey`,
// the one key of the list Desk keeps, beside `signingKey`; where Runner
// refuses the key, it is started again with neither.
func TestTheBootLineNamesTheListsKeyAsTheOneToExpect(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "jpack")
	writeStandInRuntime(t, bin, reading(allConfigVersions), lockingAs(wantGatedConfig))
	for _, refuse := range []bool{false, true} {
		runner, received := recordingRunner(t, refuse)
		s, _, _ := runnerDesk(t, bin, runner, t.TempDir())
		started(t, s.jobs)
		boots := bootLines(t, received)
		list := readFile(t, filepath.Join(runnerFolderOf(s), filepath.Base(s.jobs.dir)+".keys.jsonl"))
		if list != wantKeyLine(standInPublicKey, standInKeyID, 0) {
			t.Fatalf("the list is %q", list)
		}
		if bootKey(t, boots[0]) == "" || string(boots[0]["signingPublicKey"]) != `"`+standInPublicKey+`"` {
			t.Errorf("refuse %v: the first boot line is %q, want the key and the list's public key", refuse, readFile(t, received))
		}
		if refuse {
			if _, named := boots[1]["signingPublicKey"]; len(boots) != 2 || bootKey(t, boots[1]) != "" || named {
				t.Errorf("a Runner that refused its key was started again with %q", readFile(t, received))
			}
		} else if len(boots) != 1 {
			t.Errorf("a Runner that took its key was started %d times", len(boots))
		}
	}
}

// **A run's signing, held to the list**, for every form Runner gives and some
// it does not: signed under a key the list names, or one it does not, or with
// no list kept; failed, with each of Runner's six reasons and with none
// other; no member; and, where the list could not be read now, a signed run
// held to nothing, never called listed or unlisted.
func TestARunsSigningIsHeldToTheList(t *testing.T) {
	read := runnerList{State: runnerListRead, Keys: []string{signerA, signerB}, Current: signerB}
	none := runnerList{State: runnerListNone}
	unread := runnerList{State: runnerListUnread, Problem: "it could not be inspected"}
	signed := func(key string) json.RawMessage {
		return json.RawMessage(`{"status":"signed","publicKey":"` + key + `"}`)
	}
	cases := []struct {
		raw  json.RawMessage
		list runnerList
		want string
	}{
		{signed(signerA), read, signingListed},
		{signed(signerB), read, signingListed},
		{signed(signerC), read, signingUnlisted},
		{signed(signerA), none, signingNoList},
		{signed(signerC), none, signingNoList},
		{signed(signerA), unread, signingNotHeld},
		{signed(signerC), unread, signingNotHeld},
		{nil, read, signingNone},
		{nil, unread, signingNone},
		{json.RawMessage(`null`), read, signingOther},
		{json.RawMessage(`{"status":"signed"}`), read, signingOther},
		{json.RawMessage(`{"status":"signed","publicKey":"` + strings.ToUpper(signerA) + `"}`), read, signingOther},
		{json.RawMessage(`{"status":"signed","publicKey":"` + signerA + `","reason":"x"}`), read, signingOther},
		{json.RawMessage(`{"status":"signed","publicKey":"` + signerA + `","by":"x"}`), read, signingOther},
		{json.RawMessage(`{"status":"failed"}`), read, signingOther},
		{json.RawMessage(`{"status":"failed","reason":""}`), read, signingOther},
		{json.RawMessage(`{"status":"failed","reason":"The record carries no signature"}`), read, signingOther},
		{json.RawMessage(`{"status":"failed","reason":"/home/owner/key.seed could not be read."}`), read, signingOther},
		{json.RawMessage(`{"status":"failed","reason":"x","publicKey":"` + signerA + `"}`), read, signingOther},
		{json.RawMessage(`{"status":"pending"}`), read, signingOther},
	}
	for _, reason := range runnerSigningReasons {
		raw, _ := json.Marshal(map[string]string{"status": "failed", "reason": reason})
		for _, list := range []runnerList{read, none, unread} {
			cases = append(cases, struct {
				raw  json.RawMessage
				list runnerList
				want string
			}{raw, list, signingFailed})
		}
	}
	for _, c := range cases {
		if got := holdSigning(c.raw, c.list); got != c.want {
			t.Errorf("%s under a list %s: %s, want %s", c.raw, c.list.State, got, c.want)
		}
	}
}

// signingRig is a jobsRig whose Runner keeps its key under name, with a list
// of public keys Desk keeps for it, and a stand-in jpack-runner.
type signingRig struct {
	*jobsRig
	name, list string
}

func newSigningRig(t *testing.T) *signingRig {
	t.Helper()
	rig := &signingRig{jobsRig: newJobsRig(t), name: strings.Repeat("ab", 32)}
	rig.s.jobs.workspace = rig.name
	signing, runner, err := rig.s.assistant.openRunnerSigning()
	if err != nil {
		t.Fatal(err)
	}
	signing.Close()
	runner.Close()
	rig.list = filepath.Join(runnerFolderOf(rig.s), rig.name+".keys.jsonl")
	rig.keys(t, wantKeyLine(signerA, keyIDOf(signerA), 0))
	return rig
}

// keys writes the list of public keys Desk keeps for this Runner.
func (r *signingRig) keys(t *testing.T, lines string) {
	t.Helper()
	if err := os.WriteFile(r.list, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
}

// signings answers `GET /api/audit/jobs-signing` with query.
func (r *signingRig) signings(t *testing.T, query string) (int, jobsSigningAnswer, []byte) {
	t.Helper()
	status, data := reviewCall(t, r.ts, "GET", "/api/audit/jobs-signing"+query, "", nil, bearer)
	var answer jobsSigningAnswer
	if status == http.StatusOK {
		if err := json.Unmarshal(data, &answer); err != nil {
			t.Fatalf("the answer is not JSON: %s", data)
		}
	}
	return status, answer, data
}

// **Each completed run's signing, as Runner served it, held to the list.**
// Runner is asked for its completed runs with its cursor alone; each run's
// member reaches the page byte for byte, with how it holds against the list
// as read now: listed, unlisted, failed, none or other. A list that is not
// there holds no key; a list that could not be read now holds none either,
// and says why with no path, and its signed runs are held to nothing.
func TestJobsSigningReadsEachRunsSigningAsRunnerServesIt(t *testing.T) {
	rig := newSigningRig(t)
	members := map[string]string{
		"run_" + strings.Repeat("1", 32): `{"status":"signed","publicKey":"` + signerA + `"}`,
		"run_" + strings.Repeat("2", 32): `{"status":"signed","publicKey":"` + signerC + `"}`,
		"run_" + strings.Repeat("3", 32): `{"status":"failed","reason":"` + runnerSigningReasons[3] + `"}`,
		"run_" + strings.Repeat("4", 32): ``,
		"run_" + strings.Repeat("6", 32): `{"status":"signed"}`,
		"run_" + strings.Repeat("7", 32): `{"status":"failed","reason":"` + rig.s.configDir + `/secrets/signing/runner/key.seed could not be read."}`,
	}
	order := []string{"run_" + strings.Repeat("7", 32), "run_" + strings.Repeat("6", 32), "run_" + strings.Repeat("4", 32), "run_" + strings.Repeat("3", 32), "run_" + strings.Repeat("2", 32), "run_" + strings.Repeat("1", 32)}
	var page strings.Builder
	page.WriteString(`{"items":[{"id":"run_` + strings.Repeat("5", 32) + `","jobId":"job_` + strings.Repeat("0", 32) + `","jobName":"Queued","state":"queued"}`)
	for _, id := range order {
		page.WriteString(`,{"id":"` + id + `","jobId":"job_` + strings.Repeat("0", 32) + `","jobName":"Named","state":"completed","finishedAt":"2026-10-11T12:00:00Z"`)
		if members[id] != "" {
			page.WriteString(`,"signing":` + members[id])
		}
		page.WriteString("}")
	}
	page.WriteString(`],"next":7}`)
	rig.serveWith(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/runs" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(page.String()))
	})
	rig.runnerAsked()
	held := func(answer jobsSigningAnswer) map[string]string {
		got := map[string]string{}
		for _, run := range answer.Runs {
			got[run.ID] = run.Held
			if run.ID == order[0] {
				// A string that names a path reaches the page without it.
				if strings.Contains(string(run.Signing), rig.s.configDir) || !strings.Contains(string(run.Signing), `"status":"failed"`) || !json.Valid(run.Signing) {
					t.Errorf("%s's signing reached the page as %s", run.ID, run.Signing)
				}
				continue
			}
			if string(run.Signing) != members[run.ID] {
				t.Errorf("%s's signing reached the page as %s, want %s as Runner served it", run.ID, run.Signing, members[run.ID])
			}
		}
		return got
	}
	status, answer, data := rig.signings(t, "")
	want := map[string]string{order[5]: signingListed, order[4]: signingUnlisted, order[3]: signingFailed, order[2]: signingNone, order[1]: signingOther, order[0]: signingOther}
	if status != http.StatusOK || answer.State != "runs" || answer.Next != 7 || answer.List == nil || answer.List.State != runnerListRead ||
		!slices.Equal(answer.List.Keys, []string{signerA}) || answer.List.Current != signerA || len(answer.Runs) != 6 {
		t.Fatalf("the page of runs answered %d %s", status, data)
	}
	if got := held(answer); !mapsEqual(got, want) {
		t.Errorf("held %v, want %v", got, want)
	}
	if asked := rig.runnerAsked(); len(asked) != 1 || asked[0] != "GET /v1/runs?state=completed Bearer test-private" {
		t.Errorf("Runner was asked %q", asked)
	}
	if status, _, _ := rig.signings(t, "?after=7&q=x"); status != http.StatusOK {
		t.Errorf("the next page answered %d", status)
	}
	if asked := rig.runnerAsked(); len(asked) != 1 || asked[0] != "GET /v1/runs?after=7&state=completed Bearer test-private" {
		t.Errorf("Runner was asked %q for the next page", asked)
	}
	for _, query := range []string{"?after=", "?after=1&after=2", "?after=x", "?after=%zz"} {
		if status, _, data := rig.signings(t, query); status != http.StatusBadRequest {
			t.Errorf("%s answered %d %s", query, status, data)
		}
	}
	if asked := rig.runnerAsked(); len(asked) != 0 {
		t.Errorf("a refused cursor reached Runner: %q", asked)
	}

	// A list that names B and then A: the run signed under A is still listed.
	rig.keys(t, wantKeyLine(signerA, keyIDOf(signerA), 0)+`{"publicKey":"`+signerB+`","keyId":"`+keyIDOf(signerB)+`","at":4}`+"\n")
	if _, answer, data := rig.signings(t, ""); answer.List == nil || answer.List.Current != signerB || held(answer)[order[5]] != signingListed {
		t.Errorf("a run signed under the list's earlier key: %s", data)
	}

	// No list: no signed run is said to be under a key a list does not name.
	if err := os.Remove(rig.list); err != nil {
		t.Fatal(err)
	}
	_, answer, data = rig.signings(t, "")
	want[order[5]], want[order[4]] = signingNoList, signingNoList
	if answer.List == nil || answer.List.State != runnerListNone || !mapsEqual(held(answer), want) {
		t.Errorf("with no list: %s", data)
	}

	// A list that could not be read now: held to nothing, and said.
	rig.keys(t, wantKeyLine(signerA, keyIDOf(signerA), 0))
	if err := os.Chmod(rig.list, 0o620); err != nil {
		t.Fatal(err)
	}
	_, answer, data = rig.signings(t, "")
	want[order[5]], want[order[4]] = signingNotHeld, signingNotHeld
	if answer.List == nil || answer.List.State != runnerListUnread || !strings.Contains(answer.List.Problem, "its group or other users can write it") || !mapsEqual(held(answer), want) {
		t.Errorf("with a list that could not be read: %s", data)
	}
	if strings.Contains(string(data), rig.s.configDir) || strings.Contains(string(data), "/tmp") {
		t.Errorf("the answer names a path: %s", data)
	}

	// No Runner.
	rig.s.jobs.closed = true
	jobs := rig.s.jobs
	rig.s.jobs = nil
	if status, answer, data := rig.signings(t, ""); status != http.StatusOK || answer.State != jobsStateNoRunner || answer.Runs != nil {
		t.Errorf("with no Runner: %d %s", status, data)
	}
	rig.s.jobs = jobs
}

// mapsEqual is whether a and b hold the same pairs.
func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if other, found := b[key]; !found || other != value {
			return false
		}
	}
	return true
}

// standInVerifier is a stand-in jpack-runner at an absolute path: it writes
// its arguments, one a line, and the folder it ran in to record, prints
// stdout and stderr, and exits with code. Shell builtins only: Desk runs it
// with LANG and LC_ALL alone.
func standInVerifier(t *testing.T, record, stdout, stderr string, code int) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "jpack-runner")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + record + "'\npwd >> '" + record + "'\n" +
		"printf '%s\\n' '" + stdout + "'\nprintf '%s\\n' '" + stderr + "' >&2\nexit " + string(rune('0'+code)) + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return bin
}

// **verify-run on one run's export, on request, its answer as it gave it.**
// Desk asks Runner for the run and its export at version 5, writes the export
// and this desk's input profiles to private copies of this check's own, and
// runs `jpack-runner verify-run --file <copy> --profiles <copy>
// --release-digest <the export's own> --public-key <key> --require-signed` in
// the project, the key the one the run's signing names where the list names
// it, and the list's key in force otherwise. It answers the exit status and
// standard output verbatim, and standard error with no path; the copies are
// gone after. Runner's refusal to export is its own answer. No list, a list
// that could not be read, a copy changed while verify-run read it and a query
// that names no one run are each refused.
func TestJobsVerifyRunShowsWhatVerifyRunSaid(t *testing.T) {
	rig := newSigningRig(t)
	run := "run_" + strings.Repeat("1", 32)
	job := "job_" + strings.Repeat("2", 32)
	digest := "sha256:" + strings.Repeat("ab", 32)
	export := []byte(`{"version":5,"releaseDigest":"` + digest + `","run":{"id":"` + run + `","jobId":"` + job + `","auditSignatures":"eA=="},"note":"a&b"}` + "\n")
	exportStatus := http.StatusOK
	signing := `{"status":"signed","publicKey":"` + signerA + `"}`
	rig.serveWith(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/runs/"+run && r.URL.RawQuery == "" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":"` + run + `","jobId":"` + job + `","state":"completed","signing":` + signing + `}`))
			return
		}
		if r.URL.Path != "/v1/runs/"+run+"/verification" || r.URL.RawQuery != "version=5" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(exportStatus)
		if exportStatus != http.StatusOK {
			w.Write([]byte(`{"error":"not_verified_mapping","message":"this run has no lineage"}`))
			return
		}
		w.Write(export)
	})
	record := filepath.Join(t.TempDir(), "argv")
	report := `{"exactBytes":"matches-record","exportVersion":5,"retainedDisposition":"matches-audit","signature":{"status":"unsigned"},"status":"unsigned"}`
	said := "runner: the run is unsigned under " + rig.project + "/secret.seed"
	rig.s.jobs.bin = standInVerifier(t, record, report, said, 1)
	// copyOf is the path of this check's copy whose name begins with prefix.
	copyOf := func(prefix string) string {
		t.Helper()
		found, _ := filepath.Glob(filepath.Join(rig.project, ".desk-private", "handover", prefix+"*"))
		if len(found) != 1 {
			t.Fatalf("Desk's copies named %s: %q", prefix, found)
		}
		return found[0]
	}
	var seen []byte
	var names []string
	testHookRunnerVerified = func() {
		seen, _ = os.ReadFile(copyOf(jobsRunExportName))
		names = []string{filepath.Base(copyOf(jobsRunExportName)), filepath.Base(copyOf(jobsRunProfileName))}
	}
	t.Cleanup(func() { testHookRunnerVerified = nil })
	verify := func(query string) (int, runVerifyAnswer, []byte) {
		t.Helper()
		status, data := reviewCall(t, rig.ts, "GET", "/api/audit/jobs-verify-run"+query, "", nil, bearer)
		var answer runVerifyAnswer
		if status == http.StatusOK {
			if err := json.Unmarshal(data, &answer); err != nil {
				t.Fatalf("the answer is not JSON: %s", data)
			}
		}
		return status, answer, data
	}

	status, answer, data := verify("?run=" + run)
	if status != http.StatusOK || answer.State != jobsRunChecked || answer.Run != run || answer.PublicKey != signerA || answer.Under != underListed || answer.ExportVersion != 5 ||
		answer.Exit == nil || *answer.Exit != 1 || answer.Stdout != report+"\n" || answer.Stderr != "runner: the run is unsigned under the project's folder/secret.seed\n" {
		t.Fatalf("the check answered %d %s", status, data)
	}
	argv := strings.Split(strings.TrimSuffix(readFile(t, record), "\n"), "\n")
	wantArgv := []string{"verify-run", "--file", ".desk-private/handover/" + names[0], "--profiles", ".desk-private/handover/" + names[1], "--release-digest", digest, "--public-key", signerA, "--require-signed"}
	if len(argv) != len(wantArgv)+1 || !slices.Equal(argv[:len(wantArgv)], wantArgv) {
		t.Errorf("verify-run was run with %q, want %q", argv, wantArgv)
	}
	if where, _ := filepath.EvalSymlinks(argv[len(argv)-1]); where != mustEval(t, rig.project) {
		t.Errorf("verify-run ran in %s, not the project", argv[len(argv)-1])
	}
	if !bytes.Equal(seen, export) {
		t.Errorf("verify-run read %q, not the export Runner served", seen)
	}
	if !strings.HasPrefix(names[0], jobsRunExportName) || !strings.HasPrefix(names[1], jobsRunProfileName) || names[0] == jobsRunExportName {
		t.Errorf("Desk's copies were named %q", names)
	}
	if left, _ := filepath.Glob(filepath.Join(rig.project, ".desk-private", "handover", "jobs-run-*")); len(left) != 0 {
		t.Errorf("Desk's copies are still there: %q", left)
	}
	if strings.Contains(string(data), rig.project) {
		t.Errorf("the answer names the project's path: %s", data)
	}

	// With a list of two keys, A and then B: a run signed under A, which the
	// list names, is checked under A, on the list's word; a run signed under a
	// key the list does not name, a failed signing and a run with none, under
	// B, the key in force. The installation's profiles go as they are.
	rig.keys(t, wantKeyLine(signerA, keyIDOf(signerA), 0)+`{"publicKey":"`+signerB+`","keyId":"`+keyIDOf(signerB)+`","at":4}`+"\n")
	rig.s.jobs.profiles = json.RawMessage(`[{"id":"p"}]`)
	var profiles []byte
	testHookRunnerVerified = func() {
		profiles, _ = os.ReadFile(copyOf(jobsRunProfileName))
	}
	if _, answer, data := verify("?run=" + run); answer.PublicKey != signerA || answer.Under != underListed || !strings.Contains(readFile(t, record), "--public-key\n"+signerA+"\n") || string(profiles) != `[{"id":"p"}]` {
		t.Errorf("a run signed under the list's earlier key: %s, profiles %s", data, profiles)
	}
	for _, member := range []string{`{"status":"signed","publicKey":"` + signerC + `"}`, `{"status":"failed","reason":"` + runnerSigningReasons[3] + `"}`, `null`} {
		signing = member
		if _, answer, data := verify("?run=" + run); answer.PublicKey != signerB || answer.Under != underInForce || !strings.Contains(readFile(t, record), "--public-key\n"+signerB+"\n") {
			t.Errorf("a run whose signing is %s: %s", member, data)
		}
	}
	signing = `{"status":"signed","publicKey":"` + signerA + `"}`

	// A copy changed while verify-run read it.
	testHookRunnerVerified = func() {
		os.WriteFile(copyOf(jobsRunExportName), []byte("{}"), 0o600)
	}
	if status, _, data := verify("?run=" + run); status != http.StatusConflict || !strings.Contains(string(data), runCopyChangedWords) {
		t.Errorf("a copy changed while read answered %d %s", status, data)
	}
	testHookRunnerVerified = nil

	// Runner's refusal to export the run, in its words.
	exportStatus = http.StatusUnprocessableEntity
	os.Remove(record)
	if status, answer, data := verify("?run=" + run); status != http.StatusOK || answer.State != jobsExportRefused || answer.Status != 422 ||
		answer.Said != `{"error":"not_verified_mapping","message":"this run has no lineage"}` || answer.Exit != nil {
		t.Errorf("Runner's refusal answered %d %s", status, data)
	}
	if _, err := os.Lstat(record); !os.IsNotExist(err) {
		t.Errorf("verify-run ran on a refused export")
	}
	exportStatus = http.StatusOK

	// No list, and a list that could not be read now: nothing is checked.
	rig.keys(t, wantKeyLine(signerA, keyIDOf(signerA), 0))
	if err := os.Chmod(rig.list, 0o620); err != nil {
		t.Fatal(err)
	}
	if status, _, data := verify("?run=" + run); status != http.StatusServiceUnavailable || !strings.Contains(string(data), "Desk could not read its list of Runner's public keys now, so it checked nothing: ") || strings.Contains(string(data), rig.s.configDir) {
		t.Errorf("a list that could not be read answered %d %s", status, data)
	}
	if err := os.Remove(rig.list); err != nil {
		t.Fatal(err)
	}
	if status, _, data := verify("?run=" + run); status != http.StatusConflict || !strings.Contains(string(data), runnerListNoneWords) {
		t.Errorf("no list answered %d %s", status, data)
	}
	if _, err := os.Lstat(record); !os.IsNotExist(err) {
		t.Errorf("verify-run ran without a list")
	}
	for _, query := range []string{"", "?run=run_1", "?run=" + run + "&run=" + run, "?run=" + run + "&x=1", "?run=%zz"} {
		if status, _, data := verify(query); status != http.StatusBadRequest {
			t.Errorf("%q answered %d %s", query, status, data)
		}
	}
}

// The request identity is separate from cryptographic validity: another run can
// legitimately share the same listed key and still must not be labeled this run.
func TestJobsVerifyRunBindsRequestedRunAndJob(t *testing.T) {
	requested := "run_" + strings.Repeat("1", 32)
	otherRun := "run_" + strings.Repeat("2", 32)
	job := "job_" + strings.Repeat("3", 32)
	otherJob := "job_" + strings.Repeat("4", 32)
	digest := "sha256:" + strings.Repeat("ab", 32)
	for _, step := range []struct {
		name, recordedRun, recordedJob, exportRun, exportJob string
		version, exit                                        int
		unsigned, valid                                      bool
	}{
		{"another same-key run returned for the request", otherRun, job, otherRun, job, 5, 0, false, false},
		{"another same-key export returned for the run", requested, job, otherRun, job, 5, 0, false, false},
		{"export belongs to another job", requested, job, requested, otherJob, 5, 0, false, false},
		{"missing recorded job", requested, "", requested, "", 5, 0, false, false},
		{"missing export run", requested, job, "", job, 5, 0, false, false},
		{"same-key matching version 5", requested, job, requested, job, 5, 0, false, true},
		{"matching retained unsigned version 4", requested, job, requested, job, 4, 1, true, true},
	} {
		t.Run(step.name, func(t *testing.T) {
			rig := newSigningRig(t)
			var signing any = map[string]string{"status": "signed", "publicKey": signerA}
			if step.unsigned {
				signing = nil
			}
			record, _ := json.Marshal(map[string]any{"id": step.recordedRun, "jobId": step.recordedJob, "state": "completed", "signing": signing})
			export, _ := json.Marshal(map[string]any{"version": step.version, "releaseDigest": digest, "run": map[string]any{"id": step.exportRun, "jobId": step.exportJob}})
			rig.serveWith(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/v1/runs/" + requested:
					w.Write(record)
				case "/v1/runs/" + requested + "/verification":
					w.Write(export)
				default:
					http.NotFound(w, r)
				}
			})
			invoked := filepath.Join(t.TempDir(), "verifier-arguments")
			rig.s.jobs.bin = standInVerifier(t, invoked, "exact verifier output", "exact verifier diagnostic", step.exit)
			status, data := reviewCall(t, rig.ts, "GET", "/api/audit/jobs-verify-run?run="+requested, "", nil, bearer)
			var answer runVerifyAnswer
			if err := json.Unmarshal(data, &answer); err != nil {
				t.Fatal(err)
			}
			if !step.valid {
				if status != http.StatusBadGateway || answer.State == jobsRunChecked || answer.Run != "" || answer.Exit != nil || answer.Stdout != "" {
					t.Errorf("unbound response was attributed to the request: status %d, state %q, run %q", status, answer.State, answer.Run)
				}
				if _, err := os.Stat(invoked); !os.IsNotExist(err) {
					t.Error("verifier invoked for unbound identities")
				}
				copies, _ := filepath.Glob(filepath.Join(rig.project, ".desk-private", "handover", jobsRunExportName+"*"))
				if len(copies) != 0 {
					t.Error("unbound export was retained as a verification copy")
				}
				return
			}
			under := underListed
			if step.unsigned {
				under = underInForce
			}
			if status != http.StatusOK || answer.State != jobsRunChecked || answer.Run != requested || answer.ExportVersion != step.version || answer.PublicKey != signerA || answer.Under != under || answer.Exit == nil || *answer.Exit != step.exit || answer.Stdout != "exact verifier output\n" {
				t.Errorf("matching retained response refused or changed: status %d, %+v", status, answer)
			}
			if _, err := os.Stat(invoked); err != nil {
				t.Error("matching response never reached the verifier")
			}
		})
	}
}

// mustEval is path with its links resolved.
func mustEval(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

/* With the pinned Runner --------------------------------------------------- */

// **Each run's signer held to the list, end to end** (issue #345), with the
// pinned Runner (v0.8.0) and runtime (v0.30.0):
//   - a run signed under the list's key shows Runner's `signed`, listed, and
//     verify-run on request passes it, its answer the binary's own;
//   - with the seed replaced at its path while Runner runs, the next run
//     completes, its signing failed in Runner's words, named; verify-run
//     with --require-signed refuses its export, its answer the binary's own;
//   - with a seed substituted between Desk's custody check and Runner's
//     start, Runner refuses the key the boot line says to expect, and is
//     started again without it;
//   - with the seed and the list replaced by another key, the first run's
//     key is one the list does not name, and is named.
func TestJobsRealCompanionHoldsEachRunsSignerToTheList(t *testing.T) {
	bin, runtime := os.Getenv("JPACK_RUNNER_TEST_BIN"), os.Getenv("JPACK_BIN")
	if bin == "" || runtime == "" {
		t.Skip("set JPACK_RUNNER_TEST_BIN and JPACK_BIN for companion integration")
	}
	t.Setenv("JPACK_CONFIG", "")
	t.Setenv("JPACK_SIGNING_KEY", "")
	t.Setenv("JPACK_SIGNING_PUBLIC_KEY", "")
	run := func(name string, args ...string) (string, string, error) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		cmd := exec.Command(name, args...)
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		return stdout.String(), stderr.String(), err
	}
	// generate has the runtime make a seed in a private folder of its own,
	// and answers its bytes and its public key and keyId.
	generate := func() ([]byte, string, string) {
		t.Helper()
		dir := t.TempDir()
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		seed := filepath.Join(dir, "key.seed")
		out, said, err := run(runtime, "audit", "key", "generate", seed, "--format", "json")
		var made struct{ PublicKey, KeyID string }
		if err != nil || json.Unmarshal([]byte(out), &made) != nil || made.PublicKey == "" {
			t.Fatalf("the runtime made no key: %v %s %s", err, out, said)
		}
		data, err := os.ReadFile(seed)
		if err != nil {
			t.Fatal(err)
		}
		return data, made.PublicKey, made.KeyID
	}
	// put writes data at path through a new name in its folder, renamed into
	// place: one name, the owner's alone.
	put := func(path string, data []byte) {
		t.Helper()
		staged := path + ".staged"
		if err := os.WriteFile(staged, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(staged, path); err != nil {
			t.Fatal(err)
		}
	}
	config, project := t.TempDir(), t.TempDir()
	if err := os.Chmod(config, 0o700); err != nil {
		t.Fatal(err)
	}
	pack, _ := json.Marshal(`{"specVersion":"0.2.0-draft","id":"https://example.invalid/judgment-packs/scored","version":"0.1.0","title":"A scored pack","decision":{"intent":"Decide the one thing this pack decides.","question":"Does this request proceed?"},"outcomes":[{"id":"proceed","label":"Proceed"},{"id":"hold","label":"Hold"}],"rules":[{"id":"seven","description":"Proceed at seven.","when":{"op":"fact","path":"/score","operator":"equals","value":7},"outcome":"proceed","onUnknown":"escalate"}]}`)
	input := `{"source":{"mapping":{"version":2,"case":{"facts":[{"target":"/score","source":"/score"}],"evidence":[]},"sources":[]},"case":{"score":7}}}`
	var ts *httptest.Server
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
	start := func() *Server {
		t.Helper()
		var s *Server
		s, ts = startDesk(t, Config{RunnerBin: bin, JpackBin: runtime, RunnerAllowUntestedReleases: true, ProjectDir: project, DeskConfigDir: config, Token: testToken})
		if status, data := call("GET", "operations/status", ""); status != 200 {
			t.Fatalf("Runner did not start: %d %s", status, data)
		}
		return s
	}
	runnerKey := func() RunnerKeyStatus {
		t.Helper()
		status, data := call("GET", "runner-key", "")
		var answer struct{ RunnerKey *RunnerKeyStatus }
		if status != 200 || json.Unmarshal(data, &answer) != nil || answer.RunnerKey == nil {
			t.Fatalf("runner-key: %d %s", status, data)
		}
		return *answer.RunnerKey
	}
	var job string
	completed := func(key string) string {
		t.Helper()
		if job == "" {
			status, data := call("POST", "operations/previews", `{"pack":`+string(pack)+`,"input":`+input+`}`)
			var release struct{ ID string }
			if status != 201 || json.Unmarshal(data, &release) != nil || release.ID == "" {
				t.Fatalf("preview: %d %s", status, data)
			}
			status, data = call("POST", "operations/jobs", `{"name":"Held","releaseId":"`+release.ID+`","reviewed":true}`)
			var made struct{ ID string }
			if status != 201 || json.Unmarshal(data, &made) != nil || made.ID == "" {
				t.Fatalf("job: %d %s", status, data)
			}
			job = made.ID
		}
		status, data := call("POST", "operations/jobs/"+job+"/runs", input, "Idempotency-Key", key)
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
		return run.ID
	}
	signings := func() (jobsSigningAnswer, map[string]runSigning) {
		t.Helper()
		status, data := call("GET", "audit/jobs-signing", "")
		var answer jobsSigningAnswer
		if status != 200 || json.Unmarshal(data, &answer) != nil || answer.State != "runs" || answer.List == nil {
			t.Fatalf("jobs-signing: %d %s", status, data)
		}
		byID := map[string]runSigning{}
		for _, run := range answer.Runs {
			byID[run.ID] = run
		}
		return answer, byID
	}
	profiles := filepath.Join(t.TempDir(), "profiles.json")
	if err := os.WriteFile(profiles, []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	// checked is Desk's check of run's export, and what the binary itself
	// says of the same export with the same arguments.
	checked := func(id, publicKey string) (runVerifyAnswer, string, string, int) {
		t.Helper()
		status, data := call("GET", "audit/jobs-verify-run?run="+id, "")
		t.Logf("Desk's check of %s: %s", id, data)
		var answer runVerifyAnswer
		if status != 200 || json.Unmarshal(data, &answer) != nil || answer.State != jobsRunChecked || answer.Exit == nil {
			t.Fatalf("jobs-verify-run: %d %s", status, data)
		}
		status, export := call("GET", "operations/runs/"+id+"/verification?version=5", "")
		var head struct{ ReleaseDigest string }
		if status != 200 || json.Unmarshal(export, &head) != nil {
			t.Fatalf("export: %d %.300s", status, export)
		}
		file := filepath.Join(t.TempDir(), "run.json")
		if err := os.WriteFile(file, export, 0o600); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, err := run(bin, "verify-run", "--file", file, "--profiles", profiles, "--release-digest", head.ReleaseDigest, "--public-key", publicKey, "--require-signed")
		var exited *exec.ExitError
		switch {
		case err == nil:
			return answer, stdout, stderr, 0
		case errors.As(err, &exited):
			return answer, stdout, stderr, exited.ExitCode()
		}
		t.Fatal(err)
		return answer, stdout, stderr, 0
	}

	// **Signed under the list's key.**
	s := start()
	key := runnerKey()
	folder := runnerFolderOf(s)
	state := filepath.Base(s.jobs.dir)
	seed, list := filepath.Join(folder, state+".seed"), filepath.Join(folder, state+".keys.jsonl")
	if key.State != "signed" || readFile(t, list) != wantKeyLine(key.PublicKey, key.KeyID, 0) {
		t.Fatalf("Runner's key: %+v, list %q", key, readFile(t, list))
	}
	first := completed("first")
	answer, byID := signings()
	if answer.List.State != runnerListRead || answer.List.Current != key.PublicKey || byID[first].Held != signingListed ||
		string(byID[first].Signing) != `{"status":"signed","publicKey":"`+key.PublicKey+`"}` {
		t.Fatalf("the first run: %+v %+v", answer.List, byID[first])
	}
	verified, stdout, stderr, code := checked(first, key.PublicKey)
	if *verified.Exit != 0 || code != 0 || verified.Stdout != stdout || verified.Stderr != stderr || !strings.Contains(stdout, `"signature":{`) || verified.PublicKey != key.PublicKey || verified.Under != underListed {
		t.Fatalf("verify-run on the first run: Desk %+v\nthe binary %d %s %s", verified, code, stdout, stderr)
	}

	// **The seed replaced at its path while Runner runs.**
	original, err := os.ReadFile(seed)
	if err != nil {
		t.Fatal(err)
	}
	other, otherKey, _ := generate()
	put(seed, other)
	second := completed("second")
	_, byID = signings()
	want := `{"status":"failed","reason":"The Runtime signed nothing: the key at the signing key's path is not the expected key."}`
	if byID[second].Held != signingFailed || string(byID[second].Signing) != want || byID[first].Held != signingListed {
		t.Fatalf("the run after the seed was replaced: %+v", byID[second])
	}
	if status, data := call("GET", "operations/runs/"+second, ""); status != 200 || !bytes.Contains(data, []byte(`"signing":`+want)) {
		t.Errorf("Runner's own route says otherwise: %d %.400s", status, data)
	}
	verified, stdout, stderr, code = checked(second, key.PublicKey)
	if *verified.Exit == 0 || *verified.Exit != code || verified.Stdout != stdout || verified.Stderr != stderr || !strings.Contains(stdout, "unsigned") || verified.PublicKey != key.PublicKey || verified.Under != underInForce {
		t.Fatalf("verify-run on the second run: Desk %+v\nthe binary %d %s %s", verified, code, stdout, stderr)
	}
	ts.Close()
	s.Close()

	// **A seed substituted between Desk's check and Runner's start.**
	put(seed, original)
	var once sync.Once
	testHookKeyBetween = func(at string) {
		if at == "runner: decided" {
			once.Do(func() { put(seed, other) })
		}
	}
	s = start()
	testHookKeyBetween = nil
	if key := runnerKey(); key.State != "unsigned" || key.Reason != "runner-refused" || key.Detail != "the signing key at its path is not the expected key" {
		t.Fatalf("a seed substituted before Runner's start: %+v (the substitute is %s)", key, otherKey)
	}
	ts.Close()
	s.Close()

	// **The seed and the list replaced together by another key.**
	third, thirdKey, thirdID := generate()
	put(seed, third)
	put(list, []byte(wantKeyLine(thirdKey, thirdID, 0)))
	s = start()
	defer s.Close()
	defer ts.Close()
	if key := runnerKey(); key.State != "signed" || key.PublicKey != thirdKey {
		t.Fatalf("Runner's key after the replacement: %+v", key)
	}
	last := completed("third")
	answer, byID = signings()
	if answer.List.Current != thirdKey || byID[last].Held != signingListed || byID[first].Held != signingUnlisted || byID[second].Held != signingFailed ||
		string(byID[first].Signing) != `{"status":"signed","publicKey":"`+key.PublicKey+`"}` {
		t.Fatalf("after the replacement: %+v", byID)
	}
}
