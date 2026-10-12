package desk

// Each Jobs run's signer, held to the list of public keys Desk keeps for its
// Runner (issue #345; ADR-0010, sections 4 and 5, as amended).
//
// # What Runner records
//
// Runner v0.8.0 fixes the key it signs with at start: the key the boot line
// names as expected (`signingPublicKey`, which Desk takes from its list,
// runner_key.go), and which the seed at the path must have. It holds each
// completed run's record signature to that key, by the check `verify-run
// --public-key` makes, and records the result on the run as `signing`:
// `{"status":"signed","publicKey":<the expected key>}`, or
// `{"status":"failed","reason":<one of six fixed sentences>}`. A run recorded
// without a key, or before Runner kept the member, has none. The run routes
// and the run lists serve it; no verification export carries it.
//
// # What Desk does with it
//
// `GET /api/audit/jobs-signing` reads one page of Runner's completed runs
// (`GET /v1/runs?state=completed`, 50 at most, newest first, with Runner's
// cursor) and, for each, passes Runner's member on as Runner served it, with
// how it holds against the list Desk keeps for this Runner (`holdSigning`):
//
//   - **listed**: signed, under a key the list names;
//   - **unlisted**: signed, under a key the list does not name, which the
//     page says in Desk's one sentence of its own;
//   - **failed**: Runner recorded a failed signing, which the page names in
//     Runner's own words;
//   - **none**: Runner recorded no member, which is never taken for signed;
//   - **not-held**: signed, and the list could not be read now, so the key
//     was held to nothing: never taken for listed, or for unlisted;
//   - **other**: a member in neither of Runner's two forms, shown as served.
//
// The list is read as a Runner's key is (`readKeys`): a regular file, not a
// link, the user's, writable by nobody else, the file opened the file
// inspected, read whole within its bound and in the one spelling Desk writes.
// A list that is not there is said as such; one that could not be read now
// is said as that, and never as no list.
//
// `GET /api/audit/jobs-verify-run?run=<id>` checks one run's export, on
// request: Desk asks Runner for the run's export at version 5, keeps it
// whole within Runner's own bound, writes it to a private copy in the
// hand-over folder, and runs `jpack-runner verify-run --file <copy>
// --profiles <this desk's input profiles> --release-digest <the export's own
// releaseDigest> --public-key <the list's key in force> --require-signed`
// in the project. verify-run requires the profiles and the release digest;
// the digest is the export's own, so its release check binds nothing here,
// and the page shows the command Desk ran. Desk then holds both copies to the
// bytes it wrote, and answers verify-run's exit status, its standard output
// verbatim and its standard error with no path in it. Desk says nothing of
// its own about what verify-run found.
//
// # What it does not establish
//
// Runner's `signing` member is Runner's own record, kept by the operator. It
// says what Runner's check found when the run completed, under the key it
// expected; Desk's list says which keys Desk keeps. Neither binds anything
// against the operator, who holds the key and the list (ADR-0010, section 7).
// Rotating Runner's key (#238) is not here: this version of Desk keeps one key
// for each Runner, and checks every run's export under it.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"slices"
	"time"
)

// How a run's `signing` holds against the list (`runSigning.Held`).
const (
	signingListed   = "listed"
	signingUnlisted = "unlisted"
	signingFailed   = "failed"
	signingNone     = "none"
	signingNotHeld  = "not-held"
	signingOther    = "other"
)

// What Desk found of the list of public keys for this desk's Runner
// (`runnerList.State`).
const (
	runnerListRead   = "read"
	runnerListNone   = "none"
	runnerListUnread = "unread"
)

// What the run routes answer, besides a page of runs and a check.
const (
	jobsSigningRuns    = "runs"
	jobsRunChecked     = "checked"
	jobsExportRefused  = "export-refused"
	jobsRunExportName  = "jobs-run-export.json"
	jobsRunProfileName = "jobs-run-profiles.json"
	// runnerVerifyTimeout bounds one verify-run, which runs no Runtime here.
	runnerVerifyTimeout = 60 * time.Second
	// runnerVerifyLimit is the most of each of verify-run's streams Desk
	// keeps: its report is one line of JSON, its sentence one line.
	runnerVerifyLimit = 64 << 10
)

// The sentences these routes say in their own words. The page lists them,
// so that it can show each in the owner's language.
const (
	noRunnerRunsWords      = "This desk has no Runner, so it has no run to check."
	runnerNotRunningRuns   = "Runner is not running on this desk now, so Desk could not read its runs."
	runnerListNoneWords    = "Desk keeps no list of public keys for this desk's Runner, so it has no key to check a run's export under."
	runnerListUnreadPrefix = "Desk could not read its list of Runner's public keys now, so it checked nothing: "
	runCopyChangedWords    = "Desk's copy of the run's export changed while jpack-runner verify-run read it, so its answer is not shown."
)

var (
	runIDPattern         = regexp.MustCompile(`^run_[a-f0-9]{32}$`)
	releaseDigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// runnerList is the list of public keys Desk keeps for this desk's Runner,
// as the page is shown it: the keys in order and the one in force, the last;
// that there is none; or why it could not be read now, with no path.
type runnerList struct {
	State   string   `json:"state"`
	Keys    []string `json:"keys,omitempty"`
	Current string   `json:"current,omitempty"`
	Problem string   `json:"problem,omitempty"`
}

// readRunnerList reads the list of public keys Desk keeps for this desk's
// Runner, as a start reads it (`readKeys`), without taking the signing
// folder's lock: a list is written whole and put in place by a rename, and
// nothing here is decided under it. Nothing is made.
func (s *Server) readRunnerList() runnerList {
	name := s.jobs.workspace
	if !runnerKeyName.MatchString(name) {
		return runnerList{State: runnerListNone}
	}
	unread := func(err error) runnerList {
		return runnerList{State: runnerListUnread, Problem: s.runnerKeyWords(err.Error(), name, true)}
	}
	signing, err := s.assistant.openSigning(false)
	if errors.Is(err, errNoSigningDir) {
		return runnerList{State: runnerListNone}
	}
	if err != nil {
		return unread(err)
	}
	defer signing.Close()
	runner, err := signing.openRunnerDir()
	if errors.Is(err, fs.ErrNotExist) {
		return runnerList{State: runnerListNone}
	}
	if err != nil {
		return unread(err)
	}
	defer runner.Close()
	keys, found, err := runner.readKeys(name + keysSuffix)
	switch {
	case !found:
		return runnerList{State: runnerListNone}
	case err != nil:
		return unread(fmt.Errorf("its list of public keys was not read: %w", err))
	}
	list := runnerList{State: runnerListRead}
	for _, key := range keys {
		list.Keys = append(list.Keys, key.PublicKey)
	}
	list.Current = keys[len(keys)-1].PublicKey
	return list
}

// holdSigning is how a run's `signing` member, as Runner served it, holds
// against list. A member that is not there is none; a member in neither of
// Runner's forms is other.
func holdSigning(raw json.RawMessage, list runnerList) string {
	if raw == nil {
		return signingNone
	}
	var signing struct {
		Status    string  `json:"status"`
		PublicKey *string `json:"publicKey"`
		Reason    *string `json:"reason"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&signing) != nil || decoder.More() {
		return signingOther
	}
	switch {
	case signing.Status == "failed" && signing.PublicKey == nil && signing.Reason != nil && *signing.Reason != "":
		return signingFailed
	case signing.Status == "signed" && signing.Reason == nil && signing.PublicKey != nil && publicKeyForm.MatchString(*signing.PublicKey):
		if list.State == runnerListUnread {
			return signingNotHeld
		}
		if slices.Contains(list.Keys, *signing.PublicKey) {
			return signingListed
		}
		return signingUnlisted
	}
	return signingOther
}

// runSigning is one completed run as the page is shown it: Runner's member,
// as Runner served it, and how it holds against the list.
type runSigning struct {
	ID         string          `json:"id"`
	JobID      string          `json:"jobId"`
	JobName    string          `json:"jobName,omitempty"`
	FinishedAt string          `json:"finishedAt,omitempty"`
	Signing    json.RawMessage `json:"signing,omitempty"`
	Held       string          `json:"held"`
}

// jobsSigningAnswer is what `GET /api/audit/jobs-signing` answers: a page of
// Runner's completed runs, newest first, with Runner's cursor for the next
// (none on the last), and the list they were held to; or that this desk has
// no Runner, or that Runner is not running.
type jobsSigningAnswer struct {
	State string       `json:"state"`
	List  *runnerList  `json:"list,omitempty"`
	Runs  []runSigning `json:"runs,omitempty"`
	Next  int64        `json:"next,omitempty"`
}

// runnerAnswer is one GET of Runner's, through the companion: the route at
// tail with query, Runner's token, no proxy and no redirect, its status and
// its whole answer within limit. A Runner that is not running is
// runnerStopped; an answer that did not complete, or passed limit, is an
// error, never a shorter answer.
func (s *Server) runnerAnswer(ctx context.Context, tail string, query url.Values, limit int) (int, []byte, error) {
	endpoint, token, err := s.jobs.endpoint()
	if err != nil {
		return 0, nil, runnerStopped{err}
	}
	ctx, cancel := context.WithTimeout(ctx, runChainTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/v1/"+tail, http.NoBody)
	if err != nil {
		return 0, nil, err
	}
	request.URL.RawQuery = query.Encode()
	request.Header.Set("Authorization", "Bearer "+token)
	client := http.Client{Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		s.log.Printf("desk: the runner did not answer %s: %v", tail, err)
		return 0, nil, errors.New("the runner did not answer")
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, int64(limit)+1))
	if err != nil {
		return 0, nil, errors.New("the runner's answer did not complete")
	}
	if len(data) > limit {
		return 0, nil, fmt.Errorf("the runner's answer is larger than the %d bytes Desk reads of it", limit)
	}
	return response.StatusCode, data, nil
}

// handleJobsSigning answers `GET /api/audit/jobs-signing`, with at most one
// `after`, Runner's cursor, as the journal's is read (`journalCursor`). It
// runs only when the page asks.
func (s *Server) handleJobsSigning(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if s.jobs == nil {
		writeJSON(w, http.StatusOK, jobsSigningAnswer{State: jobsStateNoRunner})
		return
	}
	after, err := journalCursor(r.URL.RawQuery)
	if err != nil {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, "Ask for a page of runs with at most one after, a sequence in decimal digits, in a well-formed query.")
		return
	}
	answer, failure := s.jobsSigning(r.Context(), after)
	if failure != nil {
		s.refuse(w, failure)
		return
	}
	writeJSON(w, http.StatusOK, answer)
}

// jobsSigning is one page of Runner's completed runs, each held to the list.
func (s *Server) jobsSigning(ctx context.Context, after string) (jobsSigningAnswer, *lockFailure) {
	query := url.Values{"state": {"completed"}}
	if after != "" {
		query.Set("after", after)
	}
	status, data, err := s.runnerAnswer(ctx, "runs", query, runnerAnswerLimit)
	var stopped runnerStopped
	switch {
	case errors.As(err, &stopped):
		s.log.Printf("desk: Runner is not running, so its runs were not read: %v", err)
		return jobsSigningAnswer{State: jobsStateNotRunning}, nil
	case err != nil:
		return jobsSigningAnswer{}, &lockFailure{http.StatusBadGateway, CodeBadRequest, handoverWords("Desk could not read Runner's runs: ", err)}
	case status != http.StatusOK:
		return jobsSigningAnswer{}, &lockFailure{http.StatusBadGateway, CodeBadRequest, fmt.Sprintf("Desk could not read Runner's runs: the runner answered %d, not its runs.", status)}
	}
	var page struct {
		Items []struct {
			ID         string          `json:"id"`
			JobID      string          `json:"jobId"`
			JobName    string          `json:"jobName"`
			State      string          `json:"state"`
			FinishedAt string          `json:"finishedAt"`
			Signing    json.RawMessage `json:"signing"`
		} `json:"items"`
		Next *int64 `json:"next"`
	}
	if json.Unmarshal(data, &page) != nil || page.Items == nil || page.Next == nil || *page.Next < 0 {
		return jobsSigningAnswer{}, &lockFailure{http.StatusBadGateway, CodeBadRequest, "Desk could not read Runner's runs: the runner's answer is not a page of runs."}
	}
	list := s.readRunnerList()
	answer := jobsSigningAnswer{State: jobsSigningRuns, List: &list, Runs: []runSigning{}, Next: *page.Next}
	for _, item := range page.Items {
		if !runIDPattern.MatchString(item.ID) {
			return jobsSigningAnswer{}, &lockFailure{http.StatusBadGateway, CodeBadRequest, "Desk could not read Runner's runs: the runner's answer names a run by no id Runner gives."}
		}
		if item.State != "completed" {
			continue
		}
		answer.Runs = append(answer.Runs, runSigning{ID: item.ID, JobID: item.JobID, JobName: item.JobName, FinishedAt: item.FinishedAt, Signing: item.Signing, Held: holdSigning(item.Signing, list)})
	}
	return answer, nil
}

// runVerifyAnswer is what `GET /api/audit/jobs-verify-run` answers: what
// jpack-runner verify-run said of the run's export, checked under the list's
// key in force (Exit, Stdout verbatim, Stderr with no path); or Runner's own
// refusal to export the run (Status, Said).
type runVerifyAnswer struct {
	State         string `json:"state"`
	Run           string `json:"run"`
	PublicKey     string `json:"publicKey,omitempty"`
	ExportVersion int    `json:"exportVersion,omitempty"`
	Exit          *int   `json:"exit,omitempty"`
	Stdout        string `json:"stdout,omitempty"`
	Stderr        string `json:"stderr,omitempty"`
	Status        int    `json:"status,omitempty"`
	Said          string `json:"said,omitempty"`
}

// verifyRunRequest is the run a check asks for: exactly one `run`, Runner's
// id, in a well-formed query, and nothing else.
func verifyRunRequest(rawQuery string) (string, bool) {
	query, err := url.ParseQuery(rawQuery)
	run := query["run"]
	if err != nil || len(query) != 1 || len(run) != 1 || !runIDPattern.MatchString(run[0]) {
		return "", false
	}
	return run[0], true
}

// handleJobsVerifyRun answers `GET /api/audit/jobs-verify-run?run=<id>`. It
// runs only when the owner asks, for that run.
func (s *Server) handleJobsVerifyRun(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if s.jobs == nil {
		writeJSONCoded(w, http.StatusConflict, CodeBadRequest, noRunnerRunsWords)
		return
	}
	run, ok := verifyRunRequest(r.URL.RawQuery)
	if !ok {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, "Ask for one run, by Runner's id, in a well-formed query.")
		return
	}
	dir, _, ok := s.projectRuntime()
	if !ok {
		writeJSONCoded(w, http.StatusConflict, CodeBadRequest, noRunnerProjectWords)
		return
	}
	answer, failure := s.jobsVerifyRun(r.Context(), dir, run)
	if failure != nil {
		s.refuse(w, failure)
		return
	}
	writeJSON(w, http.StatusOK, answer)
}

// jobsVerifyRun checks run's export with jpack-runner verify-run under the
// list's key in force, as the package comment says.
func (s *Server) jobsVerifyRun(ctx context.Context, dir heldDir, run string) (runVerifyAnswer, *lockFailure) {
	list := s.readRunnerList()
	switch list.State {
	case runnerListNone:
		return runVerifyAnswer{}, &lockFailure{http.StatusConflict, CodeBadRequest, runnerListNoneWords}
	case runnerListUnread:
		return runVerifyAnswer{}, &lockFailure{http.StatusServiceUnavailable, CodeBadRequest, runnerListUnreadPrefix + list.Problem}
	}
	status, export, err := s.runnerAnswer(ctx, "runs/"+run+"/verification", url.Values{"version": {"5"}}, runnerExportLimit)
	var stopped runnerStopped
	switch {
	case errors.As(err, &stopped):
		s.log.Printf("desk: Runner is not running, so the run's export was not read: %v", err)
		return runVerifyAnswer{}, &lockFailure{http.StatusServiceUnavailable, CodeBadRequest, runnerNotRunningRuns}
	case err != nil:
		return runVerifyAnswer{}, &lockFailure{http.StatusBadGateway, CodeBadRequest, handoverWords("Desk could not read the run's export: ", err)}
	case status != http.StatusOK:
		// Runner's own refusal, in its words.
		return runVerifyAnswer{State: jobsExportRefused, Run: run, Status: status, Said: s.withoutPaths(string(export))}, nil
	}
	var head struct {
		Version       int    `json:"version"`
		ReleaseDigest string `json:"releaseDigest"`
	}
	if json.Unmarshal(export, &head) != nil || !releaseDigestPattern.MatchString(head.ReleaseDigest) {
		return runVerifyAnswer{}, &lockFailure{http.StatusBadGateway, CodeBadRequest, "Desk could not read the run's export: the runner's answer is not an export with a release digest."}
	}
	profiles := []byte(s.jobs.profiles)
	if len(profiles) == 0 {
		profiles = []byte("[]")
	}
	s.handoverMu.Lock()
	defer s.handoverMu.Unlock()
	handover, err := s.openHandover(true)
	if err != nil {
		return runVerifyAnswer{}, &lockFailure{http.StatusInternalServerError, CodeInternal, handoverWords("Desk's hand-over folder could not be opened: ", err)}
	}
	defer handover.Close()
	copies := []struct {
		name string
		data []byte
	}{{jobsRunExportName, export}, {jobsRunProfileName, profiles}}
	defer func() {
		for _, each := range copies {
			if err := handover.Remove(each.name); err != nil && !errors.Is(err, fs.ErrNotExist) {
				s.log.Printf("desk: Desk's copy %s was not removed after the check: %v", each.name, err)
			}
		}
	}()
	for _, each := range copies {
		if err := writePrivateData(handover, each.name, each.data); err != nil {
			return runVerifyAnswer{}, &lockFailure{http.StatusInternalServerError, CodeInternal, handoverWords("Desk could not write its copy of the run's export: ", err)}
		}
	}
	stdout, stderr, exit, err := runRunnerVerify(ctx, s.jobs.bin, dir,
		"verify-run", "--file", handoverDir+"/"+jobsRunExportName, "--profiles", handoverDir+"/"+jobsRunProfileName,
		"--release-digest", head.ReleaseDigest, "--public-key", list.Current, "--require-signed")
	if err != nil {
		return runVerifyAnswer{}, &lockFailure{http.StatusInternalServerError, CodeInternal, handoverWords("jpack-runner verify-run did not answer: ", err)}
	}
	// **Held to what Desk wrote.** What verify-run read is the copy Desk
	// wrote only where both are still those bytes.
	for _, each := range copies {
		if read, err := readPrivateData(handover, each.name, len(each.data)); err != nil || !bytes.Equal(read, each.data) {
			return runVerifyAnswer{}, &lockFailure{http.StatusConflict, CodeStale, runCopyChangedWords}
		}
	}
	return runVerifyAnswer{State: jobsRunChecked, Run: run, PublicKey: list.Current, ExportVersion: head.Version, Exit: &exit, Stdout: string(stdout), Stderr: s.withoutPaths(string(stderr))}, nil
}

// testHookRunnerVerified runs once verify-run has answered, before Desk holds
// its copies to what it wrote, and is nil outside tests.
var testHookRunnerVerified func()

// runRunnerVerify runs Runner's bin with args in dir, the project Desk holds,
// as the runtime's commands run there (`runtimeCommandAt`), with Runner's
// own environment (LANG and LC_ALL only), and answers both of its streams
// and its exit status. A command that could not run, did not finish within
// runnerVerifyTimeout or wrote more than runnerVerifyLimit to either stream
// is an error.
func runRunnerVerify(ctx context.Context, bin string, dir heldDir, args ...string) (stdout, stderr []byte, exit int, err error) {
	ctx, cancel := context.WithTimeout(ctx, runnerVerifyTimeout)
	defer cancel()
	cmd, err := runtimeCommandAt(ctx, bin, dir, args...)
	if err != nil {
		return nil, nil, 0, err
	}
	cmd.Env = []string{"LANG=C", "LC_ALL=C"}
	out, said := &cappedBuffer{limit: runnerVerifyLimit}, &cappedBuffer{limit: runnerVerifyLimit}
	cmd.Stdout, cmd.Stderr = out, said
	cmd.WaitDelay = time.Second
	if err := aimRuntimeAt(cmd, dir); err != nil {
		return nil, nil, 0, err
	}
	runErr := cmd.Run()
	if testHookRunnerVerified != nil {
		testHookRunnerVerified()
	}
	var exited *exec.ExitError
	switch {
	case ctx.Err() != nil:
		return nil, nil, 0, fmt.Errorf("it did not finish within %s", runnerVerifyTimeout)
	case out.exceeded() || said.exceeded():
		return nil, nil, 0, fmt.Errorf("it wrote more than %d bytes", runnerVerifyLimit)
	case runErr == nil:
		return out.Bytes(), said.Bytes(), 0, nil
	case errors.As(runErr, &exited) && exited.ExitCode() > 0:
		return out.Bytes(), said.Bytes(), exited.ExitCode(), nil
	}
	return nil, nil, 0, runErr
}
