package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Required cross-repository profile for the ownership integration. An explicitly
// supplied incompatible Runner fails; the published legacy Runner is not used.
func TestJobsExecutorRealEnrolledDeskRunnerRuntime(t *testing.T) {
	bin, runtime := os.Getenv("JPACK_EXECUTOR_RUNNER_TEST_BIN"), os.Getenv("JPACK_BIN")
	if bin == "" || runtime == "" {
		t.Skip("set JPACK_EXECUTOR_RUNNER_TEST_BIN and JPACK_BIN for the required executor integration profile")
	}
	if !filepath.IsAbs(bin) || !filepath.IsAbs(runtime) {
		t.Fatal("integration binaries must be absolute")
	}
	f := newPolicyMachineFixture(t)
	f.command(t, "enroll")
	f.command(t, "enroll-executor")
	f.s.cfg.RunnerBin, f.s.cfg.JpackBin = bin, runtime
	s, err := f.configured(t)
	if err != nil {
		t.Fatal(err)
	}
	token := policyTestSession(t, s)
	call := func(method, path string, value any, key string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(value)
		r := httptest.NewRequest(method, "/api/operations/"+path, bytes.NewReader(raw))
		if value == nil {
			r = httptest.NewRequest(method, "/api/operations/"+path, nil)
		}
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	if w := call("GET", "status", nil, ""); w.Code != 200 {
		t.Fatal("enrolled companion unavailable", w.Code, w.Body.String())
	}
	pack := `{"specVersion":"0.2.0-draft","id":"https://example.invalid/judgment-packs/executor-fixture","version":"0.1.0","title":"Executor fixture","decision":{"intent":"Exercise isolated local ownership.","question":"Does this synthetic request proceed?"},"outcomes":[{"id":"proceed","label":"Proceed"},{"id":"hold","label":"Hold"}],"rules":[{"id":"always","description":"Proceed.","when":{"op":"literal","value":true},"outcome":"proceed","onUnknown":"escalate"}]}`
	matrix := `{"matrixVersion":"3","cases":[{"id":"synthetic","facts":{},"evidenceAvailability":{},"expectedDisposition":{"kind":"outcome","outcomeId":"proceed","reasons":[],"handoff":{"state":"none"}}}]}`
	w := call("POST", "previews", map[string]any{"pack": pack, "matrix": matrix, "input": map[string]any{"facts": map[string]any{}}}, "")
	var release struct{ ID, Tests string }
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &release) != nil || release.ID == "" || release.Tests != "passed" {
		t.Fatal("real Runtime preview/check failed", w.Code, w.Body.String())
	}
	w = call("POST", "jobs", map[string]any{"name": "Synthetic owned job", "releaseId": release.ID, "reviewed": true}, "")
	var job struct{ ID string }
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &job) != nil || job.ID == "" {
		t.Fatal("real job create failed", w.Code, w.Body.String())
	}
	w = call("POST", "jobs/"+job.ID+"/runs", map[string]any{"facts": map[string]any{}}, "executor-real-run")
	var run struct {
		ID, State string
		Audit     json.RawMessage
	}
	if w.Code != 202 || json.Unmarshal(w.Body.Bytes(), &run) != nil || run.ID == "" {
		t.Fatal("real run submit failed", w.Code, w.Body.String())
	}
	for deadline := time.Now().Add(20 * time.Second); run.State != "completed"; {
		if run.State == "failed" || run.State == "interrupted" || time.Now().After(deadline) {
			t.Fatal("real Runtime run did not complete", w.Body.String())
		}
		time.Sleep(20 * time.Millisecond)
		w = call("GET", "runs/"+run.ID, nil, "")
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &run) != nil {
			t.Fatal("real run read failed", w.Code, w.Body.String())
		}
	}
	if len(run.Audit) == 0 || string(run.Audit) == "null" {
		t.Fatal("completed real Runtime run lacked audit")
	}
	// A copied portable Runner store cannot create a second external owner.
	copied := t.TempDir()
	if err = os.CopyFS(copied, os.DirFS(s.jobs.dir)); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(copied, 0700); err != nil {
		t.Fatal(err)
	}
	boot := map[string]any{"dir": copied, "runtime": runtime, "workspace": s.jobs.workspace, "owner": s.jobs.owner, "token": strings.Repeat("a", 64), "requiredCapabilities": runnerRequiredOwnershipCapabilities(), "executorOwnership": s.jobs.ownership}
	raw, _ := json.Marshal(boot)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	contender := exec.CommandContext(ctx, bin)
	contender.Env = []string{"LANG=C", "LC_ALL=C"}
	contender.Stdin = bytes.NewReader(append(raw, '\n'))
	stdout, stderr := &cappedBuffer{limit: 4096}, &cappedBuffer{limit: 4096}
	contender.Stdout, contender.Stderr = stdout, stderr
	if err = contender.Run(); err == nil || ctx.Err() != nil || len(stdout.Bytes()) != 0 || !bytes.Contains(stderr.Bytes(), []byte("executor lease is already owned or locking is unavailable")) {
		t.Fatalf("copied Runner did not report external contention: %v; bounded diagnostic: %q", err, stderr.Bytes())
	}
	// An unexpected owned exit is held in this Desk. Full operator restart
	// reopens the retained binding and obtains actual ownership anew.
	s.jobs.cmd.Process.Kill()
	select {
	case <-s.jobs.done:
	case <-time.After(5 * time.Second):
		t.Fatal("killed Runner did not exit")
	}
	if w = call("GET", "status", nil, ""); w.Code != 503 {
		t.Fatal("owned exit restarted automatically", w.Code)
	}
	s.Close()
	s, err = f.configured(t)
	if err != nil {
		t.Fatal("explicit restart failed", err)
	}
	token = policyTestSession(t, s)
	if w = call("GET", "runs/"+run.ID, nil, ""); w.Code != 200 {
		t.Fatal("explicit restart lost owned store", w.Code, w.Body.String())
	}
}
