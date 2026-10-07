package desk

// Runner's chain of runs, handed over and checked (ADR-0010, sections 4 and
// 5, as amended for issue #216; delivery rows 8b and 12).
//
// # Desk's private copy of the chain
//
// Runner keeps the installation's chain of runs, one entry for each completed
// run, chained by the runtime's rules, and serves it whole at `GET
// /v1/run-chain` (Runner v0.5.0 and later). The runtime reads such a file as a
// trail with `--trail <file>`. Desk takes a copy of it for each use, and runs
// the runtime over that copy alone:
//
//   - **Whole, or not at all.** Read through the companion, as `proxyJobs`
//     reads it (its URL and token), and held whole within runChainLimit before
//     anything is written. A transfer that ends early, an answer other than
//     200, and an answer past the bound are each an error, never a shorter
//     copy.
//   - **Replaced on each use.** Written with `writePrivateData`, owner-only,
//     to `.desk-private/handover/jobs-chain.jsonl`, in place of the copy
//     before. It is taken under handoverMu, which the request holds until the
//     runtime has read the copy, so that what the runtime reads is the copy
//     this request took.
//   - **No Runner, or Runner not running:** nothing is copied, and Desk says
//     which, in plain words.
//
// The hand-over of the chain's checkpoints to holders is handover.go's, for
// either chain (`handoverChain`).
//
// # The Jobs record
//
// `GET /api/audit/jobs-verify` takes a fresh copy and runs `jpack audit
// verify --trail .desk-private/handover/jobs-chain.jsonl --format json`, with
// `--expect` for each holder's file of the chain's checkpoints for the
// identity the copy has now (`heldOf`). It runs when the owner asks, and again
// after a hand-over of the chain is confirmed: never on a timer. Its answer is
// the decision record's (`auditAnswer`, read by `readAuditVerification`, with
// no path in it), with the copy's line count and Runner's key beside it.
//
// **No `--public-key`.** Runner's key (runner_key.go) is given to each run's
// runtime, which signs that run's record in the run's own attempt. The
// signature travels with the run's export (version 5, `run.auditSignatures`),
// and `jpack-runner verify-run` checks it there. The chain of runs has no
// signature sidecar, and the runtime would look for one only beside the copy
// (runtime 0.27.1, `openTrailFiles`), where none is: a key passed here would
// check nothing. So none is passed, the key is shown as `GET /api/runner-key`
// reports it, and the page says where each run's signature is checked.
//
// # What it establishes
//
// What Desk's own copies show, and nothing to anyone who does not trust this
// installation (ADR-0010, section 7). Without a held checkpoint, the chain
// shows nothing against the operator, who keeps it; with one held
// independently, the entries up to it are the ones that existed when it was
// handed over (Runner, `docs/MAPPING-V2.md`, "What the chain establishes").

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"
)

const (
	// jobsChainName is Desk's private copy of Runner's chain of runs, in the
	// hand-over folder; jobsChainCopy is its path in the project, where the
	// runtime's commands run.
	jobsChainName = "jobs-chain.jsonl"
	jobsChainCopy = handoverDir + "/" + jobsChainName
	// runChainTimeout bounds one read of the chain from Runner, as proxyJobs
	// bounds its reads.
	runChainTimeout = 40 * time.Second
)

// What the Jobs record and the hand-over can say of the chain, besides the
// decision record's states.
const (
	// jobsStateNoRunner: this desk has no Runner.
	jobsStateNoRunner = "no-runner"
	// jobsStateNotRunning: Runner did not start, or did not answer its
	// handshake, so its chain could not be read.
	jobsStateNotRunning = "not-running"
	// jobsStateEmpty: the copy holds no chained run.
	jobsStateEmpty = "empty"
	// jobsStateChain: the copy's identity and last sequence are known.
	jobsStateChain = "chain"
	// jobsStateUnread: the chain could not be copied whole, or the runtime
	// gave no checkpoint of the copy; Problem or Diagnostics say why.
	jobsStateUnread = "unread"
)

// The sentences these routes say in their own words. The page lists them, so
// that it can show each in the owner's language.
const (
	noRunnerWords         = "This desk has no Runner, so it has no chain of runs to hand over."
	runnerNotRunningWords = "Runner is not running on this desk now, so Desk could not read its chain of runs."
	noRunChainedWords     = "No run is chained yet, so there is nothing to hand over."
	noRunnerProjectWords  = "This desk holds no project folder for Desk to run the runtime in."
)

// errNoRunner is a desk with no Runner.
var errNoRunner = errors.New("this desk has no Runner")

// runnerStopped is a Runner that is not running: the companion could not
// start it, or reach it.
type runnerStopped struct{ err error }

func (e runnerStopped) Error() string { return e.err.Error() }
func (e runnerStopped) Unwrap() error { return e.err }

// runChainUnread is a chain that did not arrive whole from Runner.
type runChainUnread struct{ err error }

func (e runChainUnread) Error() string { return e.err.Error() }
func (e runChainUnread) Unwrap() error { return e.err }

// testHookJobsChainCopied runs once Desk has written its copy of the chain,
// before the runtime reads it, and is nil outside tests.
var testHookJobsChainCopied func()

// fetchRunChain is Runner's chain of runs, whole: `GET /v1/run-chain` through
// the companion, with its token, no query, no proxy and no redirect; an
// answer of 200, read to its end within runChainLimit. Anything else is an
// error, and nothing is kept of it.
func (s *Server) fetchRunChain(ctx context.Context) ([]byte, error) {
	if s.jobs == nil {
		return nil, errNoRunner
	}
	endpoint, token, err := s.jobs.endpoint()
	if err != nil {
		return nil, runnerStopped{err}
	}
	ctx, cancel := context.WithTimeout(ctx, runChainTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/v1/"+runChainRoute, http.NoBody)
	if err != nil {
		return nil, runChainUnread{err}
	}
	request.Header.Set("Authorization", "Bearer "+token)
	client := http.Client{Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		s.log.Printf("desk: the runner's chain of runs was not read: %v", err)
		return nil, runChainUnread{errors.New("the runner did not answer")}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, runChainUnread{fmt.Errorf("the runner answered %d, not its chain of runs", response.StatusCode)}
	}
	// **Held whole before anything is written**, as proxyJobs holds it: a
	// transfer Runner aborts, or a connection that closes short of the body's
	// length or its last chunk, fails to read, and an answer past the bound is
	// refused, never cut to it.
	data, err := io.ReadAll(io.LimitReader(response.Body, int64(runChainLimit)+1))
	if err != nil {
		return nil, runChainUnread{errors.New("the runner's answer did not complete")}
	}
	if len(data) > runChainLimit {
		return nil, runChainUnread{fmt.Errorf("the runner's chain of runs is larger than the %d bytes Desk holds of it", runChainLimit)}
	}
	return data, nil
}

// takeJobsChain takes a fresh private copy of Runner's chain of runs
// (fetchRunChain) and writes it as jobsChainName in the hand-over folder, in
// place of the copy before; it answers the bytes copied. The caller holds
// handoverMu until the runtime has read the copy.
func (s *Server) takeJobsChain(ctx context.Context) ([]byte, error) {
	data, err := s.fetchRunChain(ctx)
	if err != nil {
		return nil, err
	}
	handover, err := s.openHandover(true)
	if err != nil {
		return nil, fmt.Errorf("Desk's hand-over folder could not be opened: %w", err)
	}
	defer handover.Close()
	if err := writePrivateData(handover, jobsChainName, data); err != nil {
		return nil, fmt.Errorf("Desk could not write its copy of the chain of runs: %w", err)
	}
	if testHookJobsChainCopied != nil {
		testHookJobsChainCopied()
	}
	return data, nil
}

// chainFailure is how a route that needed a copy of the chain refuses where it
// could not take one, with what it then did not do (after): no Runner, Runner
// not running, a chain that did not arrive whole, and Desk's own failure to
// write the copy, each in its own words.
func (s *Server) chainFailure(err error, after string) *lockFailure {
	var stopped runnerStopped
	var unread runChainUnread
	switch {
	case err == nil:
		return nil
	case errors.Is(err, errNoRunner):
		return &lockFailure{http.StatusConflict, CodeBadRequest, noRunnerWords}
	case errors.As(err, &stopped):
		s.log.Printf("desk: Runner is not running, so its chain of runs was not read: %v", err)
		return &lockFailure{http.StatusServiceUnavailable, CodeBadRequest, runnerNotRunningWords}
	case errors.As(err, &unread):
		s.log.Printf("desk: the runner's chain of runs did not arrive whole: %v", err)
		return &lockFailure{http.StatusBadGateway, CodeBadRequest, handoverWords("Desk could not take a copy of the runner's chain of runs, and "+after+": ", err)}
	}
	return &lockFailure{http.StatusInternalServerError, CodeInternal, handoverWords("Desk could not take a copy of the runner's chain of runs, and "+after+": ", err)}
}

// copyChain takes, for Runner's chain of runs, the fresh copy a hand-over
// reads, or refuses as chainFailure says; for the desk's own trail it does
// nothing. The caller holds handoverMu.
func (s *Server) copyChain(ctx context.Context, chain handoverChain, after string) *lockFailure {
	if !chain.jobs {
		return nil
	}
	_, err := s.takeJobsChain(ctx)
	return s.chainFailure(err, after)
}

// jobsChainState is Runner's chain of runs as the hand-over shows it beside
// each holder: none where this desk has no Runner, or Runner is not running;
// none chained yet; its identity and last sequence; or why Desk could not
// tell, in its words or the runtime's.
type jobsChainState struct {
	State       string              `json:"state"`
	Chain       *checkpointHead     `json:"chain,omitempty"`
	Diagnostics []runtimeDiagnostic `json:"diagnostics,omitempty"`
	Problem     string              `json:"problem,omitempty"`
}

// jobsChainNow is jobsChainState from a fresh copy of the chain, and `audit
// checkpoint --trail <copy> --format json` over it. The caller holds
// handoverMu. Nothing it says names a path.
func (s *Server) jobsChainNow(ctx context.Context, dir heldDir) *jobsChainState {
	_, err := s.takeJobsChain(ctx)
	var stopped runnerStopped
	switch {
	case errors.Is(err, errNoRunner):
		return &jobsChainState{State: jobsStateNoRunner}
	case errors.As(err, &stopped):
		s.log.Printf("desk: Runner is not running, so its chain of runs was not read: %v", err)
		return &jobsChainState{State: jobsStateNotRunning}
	case err != nil:
		s.log.Printf("desk: the runner's chain of runs was not copied for the hand-over: %v", err)
		return &jobsChainState{State: jobsStateUnread, Problem: s.withoutPaths(handoverWords("Desk could not take a copy of the runner's chain of runs: ", err))}
	}
	head, refusal, err := s.readCheckpointHead(ctx, dir, jobsChain)
	switch {
	case err != nil:
		s.log.Printf("desk: the chain of runs' last checkpoint could not be read: %v", err)
		return &jobsChainState{State: jobsStateUnread, Problem: s.withoutPaths(handoverWords("The chain of runs' last checkpoint could not be read: ", err))}
	case refusal != nil:
		said := make([]runtimeDiagnostic, 0, len(refusal))
		for _, diagnostic := range refusal {
			said = append(said, runtimeDiagnostic{Code: diagnostic.Code, Message: s.withoutPaths(diagnostic.Message)})
		}
		return &jobsChainState{State: jobsStateUnread, Diagnostics: said}
	case head == nil:
		return &jobsChainState{State: jobsStateEmpty}
	}
	return &jobsChainState{State: jobsStateChain, Chain: head}
}

// jobsRecordAnswer is what `GET /api/audit/jobs-verify` answers: the decision
// record's answer, of the copy of Runner's chain of runs, whose State may
// also be "no-runner" or "not-running"; Runner's key as `GET /api/runner-key`
// reports it; and the number of lines in the copy checked.
type jobsRecordAnswer struct {
	auditAnswer
	RunnerKey  *RunnerKeyStatus `json:"runnerKey,omitempty"`
	ChainLines *int64           `json:"chainLines,omitempty"`
}

// chainLines is the number of lines in data: each ended by a newline, and a
// last one with none.
func chainLines(data []byte) int64 {
	lines := int64(bytes.Count(data, []byte("\n")))
	if len(data) > 0 && data[len(data)-1] != '\n' {
		lines++
	}
	return lines
}

// handleJobsVerify answers `GET /api/audit/jobs-verify`. It runs only when
// asked: the page asks when the owner checks, and after a hand-over of the
// chain is confirmed.
func (s *Server) handleJobsVerify(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if s.jobs == nil {
		writeJSON(w, http.StatusOK, jobsRecordAnswer{auditAnswer: auditAnswer{State: jobsStateNoRunner}})
		return
	}
	dir, _, ok := s.projectRuntime()
	if !ok {
		writeJSONCoded(w, http.StatusConflict, CodeBadRequest, noRunnerProjectWords)
		return
	}
	answer, failure := s.jobsVerify(r.Context(), dir)
	if failure != nil {
		s.refuse(w, failure)
		return
	}
	shown := answer
	shown.auditAnswer = s.withoutPathsIn(answer.auditAnswer)
	if before, after := mustJSON(answer), mustJSON(shown); before != after {
		// The page is told no path; the owner's own log keeps them.
		s.log.Printf("desk: the Jobs record, as the runtime said it: %s", before)
	}
	writeJSON(w, http.StatusOK, shown)
}

// jobsVerify is the Jobs record's answer: `packs schema` alone where the
// runtime reads no "6"; otherwise a fresh copy of the chain, and the
// runtime's `audit verify --trail <copy> --format json` over it, with the
// checkpoints of the chain handed over to holders and nothing else held.
func (s *Server) jobsVerify(ctx context.Context, dir heldDir) (jobsRecordAnswer, *lockFailure) {
	schema, err := readRuntimeSchema(ctx, s.cfg.JpackBin, dir)
	if err != nil {
		return jobsRecordAnswer{}, &lockFailure{http.StatusInternalServerError, CodeInternal, handoverWords("The chain of runs could not be checked: ", err)}
	}
	older := jobsRecordAnswer{auditAnswer: auditAnswer{State: auditStateOlder, Runtime: schema.version, Floor: auditRuntimeFloor}}
	if !slices.Contains(schema.supported, auditConfigVersion) {
		return older, nil
	}
	s.handoverMu.Lock()
	defer s.handoverMu.Unlock()
	data, err := s.takeJobsChain(ctx)
	var stopped runnerStopped
	switch {
	case errors.As(err, &stopped):
		s.log.Printf("desk: Runner is not running, so its chain of runs was not checked: %v", err)
		return jobsRecordAnswer{auditAnswer: auditAnswer{State: jobsStateNotRunning}, RunnerKey: s.jobs.keyStatus()}, nil
	case err != nil:
		return jobsRecordAnswer{}, s.chainFailure(err, "nothing was checked")
	}
	expect := s.heldOf(ctx, dir, jobsChain)
	args := append(append([]string{"audit", "verify"}, jobsChain.source()...), "--format", "json")
	args = append(args, expect.args...)
	out, runErr := runRuntime(ctx, s.cfg.JpackBin, dir, args...)
	answer, err := readAuditVerification(out, runErr)
	if err != nil {
		return jobsRecordAnswer{}, &lockFailure{http.StatusInternalServerError, CodeInternal, handoverWords("The chain of runs could not be checked: ", err)}
	}
	if answer.State == auditStateOlder {
		return older, nil
	}
	answer.Runtime = schema.version
	answer.Expected = expect.count
	answer.ExpectUnread = expect.unread
	answer.HandoverProblem = expect.problem
	lines := chainLines(data)
	return jobsRecordAnswer{auditAnswer: answer, RunnerKey: s.jobs.keyStatus(), ChainLines: &lines}, nil
}
