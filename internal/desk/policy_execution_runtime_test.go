package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type realManualFixture struct {
	t        *testing.T
	f        *policyMachineFixture
	s        *Server
	token    string
	job      string
	adoption policyMutationReply
	policy   policyReply
}

func manualRuntimeBinaries(t *testing.T) (string, string) {
	t.Helper()
	bin, runtime := os.Getenv("JPACK_MANUAL_RUNNER_TEST_BIN"), os.Getenv("JPACK_BIN")
	if bin == "" || runtime == "" {
		t.Skip("required manual integration profile: set JPACK_MANUAL_RUNNER_TEST_BIN and JPACK_BIN")
	}
	if !filepath.IsAbs(bin) || !filepath.IsAbs(runtime) {
		t.Fatal("real integration binaries must be absolute")
	}
	if !policyManualAdmissionAvailable || probeRunnerAdmission(bin) != nil {
		t.Fatal("supplied real candidate must expose the complete matching manual profile; no skip or probe shim")
	}
	return bin, runtime
}
func newRealManualFixture(t *testing.T, bin, runtime string) *realManualFixture {
	t.Helper()
	ctx := context.Background()
	f := newPolicyMachineFixture(t)
	f.s.cfg.JpackBin = runtime
	// Actual Runtime authoring checks use the SAME executable as the later job.
	p := policyApprove(t, f.s, f.token, f.policy)
	p = policyMutation(t, policyCall(f.s, f.token, "POST", "/api/policies/"+p.Document.ID+"/publish", policyPublishBody(t, p))).Policy
	pack := coveragePublishFixture(t, f.s, []byte(reviewPack))
	service, actor, saved, material := coverageMaterialFixture(t, f.s, f.token, p, pack)
	basis, err := json.MarshalIndent(adoptionRuntimePolicy(material), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	edit := coverageSaveBody(t, p, pack)
	edit.CoverageID = saved.Document.ID
	edit.BaseRevision = saved.Revision
	edit.Statement.Basis = string(basis)
	changed, err := service.coverageCommand(ctx, p.Document.ID, actor, edit)
	if err != nil {
		t.Fatal(err)
	}
	coverage, err := service.coverageCommand(ctx, p.Document.ID, actor, coverageReviewBody(t, *changed.Coverage))
	if err != nil {
		t.Fatal("actual Runtime coverage approval", err)
	}
	c := coverage.Coverage
	q := policyAdoptionRequest{Action: "prepare", OperationID: policyOp(t), Pack: &pack, Coverages: []policyAdoptionSelection{{PolicyID: p.Document.ID, CoverageID: c.Document.ID, Revision: c.Revision, CandidateID: c.Document.Candidate.ID, ReviewID: c.Document.CurrentReviewID}}}
	prepared, err := service.adoptionCommand(ctx, actor, q)
	if err != nil {
		t.Fatal("actual Runtime adoption preparation", err)
	}
	adopted, err := service.adoptionCommand(ctx, actor, adoptionReviewRequest(t, prepared))
	if err != nil {
		t.Fatal("actual Runtime adoption approval", err)
	}
	lifecycleDo(t, service, p, lifecycleBody(t, service, p, "activate"))
	f.command(t, "enroll")
	f.command(t, "enroll-executor")
	// Match the public offline CLI's resolved configuration rather than relying
	// on a fixture-only alternate location or portable Runner identity discovery.
	config, err := ResolveDeskConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	f.s.cfg.DeskConfigDir = config
	f.s.cfg.RunnerBin = bin
	starter, err := f.configured(t)
	if err != nil {
		t.Fatal(err)
	}
	seed := &realManualFixture{t: t, f: f, s: starter, token: policyTestSession(t, starter)}
	if w := seed.call("GET", "status", nil, ""); w.Code != 200 {
		t.Fatal("ownership-only bootstrap", w.Code, w.Body.String())
	}
	matrix := `{"matrixVersion":"3","cases":[{"id":"synthetic","facts":{},"evidenceAvailability":{},"expectedDisposition":{"kind":"outcome","outcomeId":"accept","reasons":[],"handoff":{"state":"none"}}}]}`
	raw, _ := json.Marshal(map[string]any{"pack": reviewPack, "matrix": matrix, "input": map[string]any{"facts": map[string]any{}}})
	w := seed.call("POST", "previews", raw, "")
	var release struct{ ID, Tests string }
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &release) != nil || release.ID == "" || release.Tests != "passed" {
		t.Fatal("actual Runtime preview/matrix", w.Code, w.Body.String())
	}
	raw, _ = json.Marshal(map[string]any{"name": "Manual exact release", "releaseId": release.ID, "reviewed": true})
	w = seed.call("POST", "jobs", raw, "")
	var job struct{ ID string }
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &job) != nil || job.ID == "" {
		t.Fatal("real job create", w.Code, w.Body.String())
	}
	starter.Close()
	f.options.RunnerBin = bin
	if status, err := PolicyAuthorityLocal(ctx, "enable-execution", f.options, f.s.projectDir); err != nil || status.ExecutionState != "enabled" {
		t.Fatal("actual explicit store preparation/enablement", status, err)
	}
	s, err := f.configured(t)
	if err != nil {
		t.Fatal("actual enabled restart", err)
	}
	result := &realManualFixture{t, f, s, policyTestSession(t, s), job.ID, adopted, p}
	raw, _ = json.Marshal(map[string]any{"operationId": policyOp(t), "adoptionId": adopted.Adoption.Document.ID, "reviewId": adopted.Adoption.Document.CurrentReviewID})
	w = result.call("POST", "jobs/"+job.ID+"/policy-registration", raw, "")
	if w.Code != 200 {
		t.Fatal("exact owner-export/adoption registration", w.Code, w.Body.String())
	}
	return result
}
func (f *realManualFixture) call(method, tail string, raw []byte, key string) *httptest.ResponseRecorder {
	f.t.Helper()
	r := httptest.NewRequest(method, "/api/operations/"+tail, bytes.NewReader(raw))
	r.Header.Set("Authorization", "Bearer "+f.token)
	r.Header.Set("Content-Type", "application/json")
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	f.s.ServeHTTP(w, r)
	return w
}
func (f *realManualFixture) submit(key string) (string, *httptest.ResponseRecorder) {
	f.t.Helper()
	w := f.call("POST", "jobs/"+f.job+"/runs", []byte(`{"facts":{ "flag":false,"zero":0,"nothing":null,"large":9007199254740993.00 }}`), key)
	var result struct{ ID string }
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	return result.ID, w
}
func (f *realManualFixture) wait(id string) []byte {
	f.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		w := f.call("GET", "runs/"+id, nil, "")
		var run struct {
			State string
			Audit json.RawMessage
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &run) != nil {
			f.t.Fatal(w.Code, w.Body.String())
		}
		if run.State == "completed" || run.State == "failed" || run.State == "interrupted" {
			return w.Body.Bytes()
		}
		if time.Now().After(deadline) {
			f.t.Fatal("actual run did not settle", w.Body.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}
func (f *realManualFixture) withdraw() {
	f.t.Helper()
	service, _ := coverageServiceFixture(f.s, f.token)
	p, err := service.get(context.Background(), f.policy.Document.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	q := lifecycleBody(f.t, service, p, "withdraw")
	q.Rationale = "Explicit isolated fixture withdrawal"
	lifecycleDo(f.t, service, p, q)
}
func (f *realManualFixture) assertNoAudit(id string) {
	f.t.Helper()
	path := filepath.Join(f.s.jobs.dir, "attempts", id, "audit", "evaluations.jsonl")
	if raw, err := os.ReadFile(path); err == nil && len(bytes.TrimSpace(raw)) != 0 || err != nil && !errors.Is(err, os.ErrNotExist) {
		f.t.Fatal("blocked/uncertain run left operational audit", err)
	}
}

func (f *realManualFixture) pinnedRuntimeForRun(id string) string {
	f.t.Helper()
	w := f.call("GET", "runs/"+id, nil, "")
	var run struct {
		JobID, ReleaseID string
		Revision         int
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &run) != nil || run.JobID != f.job {
		f.t.Fatal("cannot resolve the actual run's retained release", w.Code, w.Body.String())
	}
	core, enabled, err := f.s.executionCore()
	if err != nil || !enabled {
		f.t.Fatal("cannot resolve the registered Runtime", err)
	}
	var runtimeDigest string
	err = core.repository.View(context.Background(), func(l policyLedger) error {
		registered, ok := l.Execution.Resources[executionResourceID(core.identity, run.JobID)]
		if !ok || registered.Content.Resource.ReleaseID != run.ReleaseID || registered.Content.Resource.JobRevision != strconv.Itoa(run.Revision) {
			return errors.New("actual run does not match the immutable registered release")
		}
		runtimeDigest = registered.Content.RuntimeDigest
		return nil
	})
	if err != nil || !policyExecutionDigest.MatchString(runtimeDigest) {
		f.t.Fatal("cannot resolve the exact retained Runtime digest", err)
	}
	return filepath.Join(f.s.jobs.dir, "runtimes", strings.TrimPrefix(runtimeDigest, "sha256:"))
}

// Proxy only the real inherited connection. Its write is invoked AFTER the core
// has durably committed, so withholding the original fresh reply fixes the
// commit/withdraw/spawn ordering without changing Runner or the Runtime.
type heldManualReply struct {
	net.Conn
	method         string
	ready          chan struct{}
	release        chan struct{}
	drop           bool
	once           sync.Once
	authorityInfos []os.FileInfo
}

func (h *heldManualReply) Write(raw []byte) (int, error) {
	var envelope struct {
		Method, Status string
		Body           struct{ Delivery, Decision string }
	}
	_ = json.Unmarshal(raw, &envelope)
	if envelope.Method == h.method && envelope.Status == "ok" && envelope.Body.Delivery == "committed" {
		h.once.Do(func() { close(h.ready) })
		select {
		case <-h.release:
		case <-time.After(4 * time.Second):
			return 0, errors.New("fixture reply release timeout")
		}
		if h.drop {
			h.Conn.Close()
			return 0, io.ErrClosedPipe
		}
	}
	return h.Conn.Write(raw)
}
func installHeldManualReply(t *testing.T, s *Server, method string, drop bool) *heldManualReply {
	t.Helper()
	proxy := &heldManualReply{method: method, ready: make(chan struct{}), release: make(chan struct{}), drop: drop}
	s.jobs.authoritySocketForTest = func() (net.Conn, *os.File, error) {
		host, child, err := jobsAuthoritySocket()
		if err == nil {
			duplicate, e := host.(*net.UnixConn).File()
			if e != nil {
				host.Close()
				child.Close()
				return nil, nil, e
			}
			hostInfo, e := duplicate.Stat()
			duplicate.Close()
			if e != nil {
				host.Close()
				child.Close()
				return nil, nil, e
			}
			childInfo, e := child.Stat()
			if e != nil {
				host.Close()
				child.Close()
				return nil, nil, e
			}
			proxy.authorityInfos = []os.FileInfo{hostInfo, childInfo}
			proxy.Conn = host
			host = proxy
		}
		return host, child, err
	}
	t.Cleanup(func() {
		select {
		case <-proxy.release:
		default:
			close(proxy.release)
		}
	})
	return proxy
}
func TestPolicyManualRealDeskRunnerRuntime(t *testing.T) {
	bin, runtime := manualRuntimeBinaries(t)
	t.Run("registered-exact-input-and-withdrawal-first", func(t *testing.T) {
		f := newRealManualFixture(t, bin, runtime)
		// Token rotation keeps the same stable installation-admin logical identity.
		f.token = policyTestSession(t, f.s)
		id, w := f.submit("original-manual-key")
		if w.Code != 202 || id == "" {
			t.Fatal(w.Code, w.Body.String())
		}
		result := f.wait(id)
		if !bytes.Contains(result, []byte(`"state":"completed"`)) || !bytes.Contains(result, []byte(`"audit"`)) {
			t.Fatal("actual Runtime failed", string(result))
		}
		facts, err := os.ReadFile(filepath.Join(f.s.jobs.dir, "attempts", id, "facts.json"))
		if err != nil || string(facts) != `{ "flag":false,"zero":0,"nothing":null,"large":9007199254740993.00 }` {
			t.Fatal("actual Runtime facts changed", string(facts), err)
		}
		if w := f.call("POST", "jobs/"+f.job+"/runs", []byte(` {"facts":{ "flag":false,"zero":0,"nothing":null,"large":9007199254740993.00 }}`), "original-manual-key"); w.Code != 409 {
			t.Fatal("same key changed outer bytes accepted", w.Code, w.Body.String())
		}
		f.withdraw()
		blocked, w := f.submit("withdrawal-first")
		if w.Code != 202 && w.Code != 409 {
			t.Fatal(w.Code, w.Body.String())
		}
		if blocked != "" {
			f.wait(blocked)
			f.assertNoAudit(blocked)
		}
	})
	for _, scenario := range []struct {
		name, method string
		drop         bool
	}{{"commit-before-withdrawal", "commitStart", false}, {"lost-start-reply", "commitStart", true}, {"lost-reserve-reply", "reserve", true}} {
		t.Run(scenario.name, func(t *testing.T) {
			f := newRealManualFixture(t, bin, runtime)
			// Registration started the Runner. Restart explicitly to install the test
			// transport wrapper before any governed attempt, retaining the real store.
			f.s.Close()
			s, err := f.f.configured(t)
			if err != nil {
				t.Fatal(err)
			}
			f.s = s
			f.token = policyTestSession(t, s)
			held := installHeldManualReply(t, s, scenario.method, scenario.drop)
			id, w := f.submit(scenario.name)
			if w.Code != 202 || id == "" {
				t.Fatal(w.Code, w.Body.String())
			}
			select {
			case <-held.ready:
			case <-time.After(15 * time.Second):
				t.Fatal("fresh durable reply was not reached")
			}
			if !scenario.drop {
				f.withdraw()
			}
			f.assertNoAudit(id)
			var observer *os.File
			if !scenario.drop {
				path := filepath.Join(f.s.jobs.dir, "attempts", id, "audit")
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatal(err)
				}
				observer, err = os.OpenFile(filepath.Join(path, "evaluations.jsonl"), os.O_CREATE|os.O_RDWR, 0600)
				if err != nil {
					t.Fatal(err)
				}
				defer observer.Close()
				if err = lockPrivateData(observer, true); err != nil {
					t.Fatal(err)
				}
			}
			close(held.release)
			if !scenario.drop {
				observeManualRuntimeFD(t, f.pinnedRuntimeForRun(id), f.s.jobs.ownership, observer, held.authorityInfos)
				observer.Close()
				result := f.wait(id)
				if !bytes.Contains(result, []byte(`"state":"completed"`)) {
					t.Fatal("committed-before-withdrawal work did not complete", string(result))
				}
			} else {
				// Channel loss may hold Jobs before a status response. Close this actual
				// Runner, reopen its exact store, and observe history without operational replay.
				f.s.Close()
				s, err = f.f.configured(t)
				if err != nil {
					t.Fatal(err)
				}
				f.s = s
				f.token = policyTestSession(t, s)
				if w := f.call("GET", "status", nil, ""); w.Code != 200 {
					t.Fatal("fresh owned epoch failed", w.Code, w.Body.String())
				}
				f.wait(id)
				f.assertNoAudit(id)
				core, enabled, err := s.executionCore()
				if err != nil || !enabled {
					t.Fatal(err)
				}
				err = core.repository.View(context.Background(), func(l policyLedger) error {
					intent := executionIntentID(core.identity, f.job, scenario.name)
					a, ok := l.Execution.Attempts[intent]
					if !ok {
						t.Fatal("durable barrier missing")
					}
					if scenario.method == "commitStart" && a.Start == nil {
						t.Fatal("lost committed result missing")
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
				// Reads cannot queue another attempt and an original-key retry cannot launch.
				_, again := f.submit(scenario.name)
				if again.Code != 200 && again.Code != 202 && again.Code != 409 {
					t.Fatal("original history unavailable", again.Code, again.Body.String())
				}
				time.Sleep(100 * time.Millisecond)
				f.assertNoAudit(id)
			}
		})
	}
}

func TestPolicyManualActualOldDeskRegistryRollbackFence(t *testing.T) {
	old := os.Getenv("JPACK_MANUAL_OLD_DESK_TEST_BIN")
	if old == "" {
		t.Skip("set JPACK_MANUAL_OLD_DESK_TEST_BIN for actual ownership-era downgrade fixture")
	}
	if !filepath.IsAbs(old) {
		t.Fatal("old Desk binary must be absolute")
	}
	f, _ := admissionPrepareFixture(t, "committed")
	observe := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, old, "policy-authority", "status", "--root", f.options.Root, "--installation", f.options.InstallationID)
		out, said := &cappedBuffer{limit: 4096}, &cappedBuffer{limit: 4096}
		cmd.Stdout = out
		cmd.Stderr = said
		return cmd.Run()
	}
	if err := observe(); err != nil {
		t.Fatal("old Desk did not accept original ownership registry", err)
	}
	prior := machineRaw(t, f.options.Root, policyMachineRegistryName)
	if _, err := enableAdmissionFixture(t, f, nil); err != nil {
		t.Fatal(err)
	}
	machineWrite(t, f.options.Root, policyMachineRegistryName, prior)
	if err := observe(); err == nil {
		t.Fatal("actual old Desk accepted rollback through sticky fence")
	}
	if _, err := PolicyAuthorityLocal(context.Background(), "status", f.options, ""); err == nil {
		t.Fatal("current Desk accepted same rollback")
	}
}
