package desk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDeskHomeConfirmWithNamedDesksAndRealRunner(t *testing.T) {
	runtime, runner := os.Getenv("JPACK_BIN"), os.Getenv("JPACK_RUNNER_HOME_TEST_BIN")
	if runtime == "" || runner == "" {
		t.Skip("requires Runtime and relocation Runner")
	}
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	cfg := Config{ProjectDir: t.TempDir(), DeskConfigDir: t.TempDir(), JpackBin: runtime, RunnerBin: runner, Token: testToken, LocalAccess: true, HomeMoveShutdown: func() {}}
	privateFixture(t, filepath.Join(cfg.ProjectDir, projectConfigName), `{"deskConfigVersion":1}`)
	s, ts := startDesk(t, cfg)
	createTestDesk(t, ts, "First")
	createTestDesk(t, ts, "Second")
	request := projectMoveRequest{Destination: filepath.Join(t.TempDir(), "jpack-desk")}
	// Storage is set up: the API refuses in words (errDeskHomeMoveAfterStorage).
	// The planner and the queue below it keep their coverage.
	raw, _ := json.Marshal(request)
	if code, body := deskCall(t, ts, "POST", "/api/desk-home/preview", "", string(raw), true); code != 409 || refusalOf(body) != errDeskHomeMoveAfterStorage.Error() {
		t.Fatal(code, string(body))
	}
	plan, err := s.planDeskHome(request.Destination)
	if err != nil || len(plan.Blockers) > 0 {
		t.Fatal(err, plan.Blockers)
	}
	request.Token = plan.Token
	if err = s.queueHomeMove(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	ts.Close()
	s.Close()
	if err := s.FinishDeskHomeMove(); err != nil {
		t.Fatal(err)
	}
}

func TestDeskHomeExpiredConfirmationDoesNotQueue(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	cfg := Config{ProjectDir: t.TempDir(), DeskConfigDir: t.TempDir(), Token: testToken, JpackBin: homeRuntime(t), HomeMoveShutdown: func() {}}
	s, ts := startDesk(t, cfg)
	t.Cleanup(func() { ts.Close(); s.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.queueHomeMove(ctx, projectMoveRequest{Destination: filepath.Join(t.TempDir(), "jpack-desk")}); err == nil {
		t.Fatal("expired confirmation accepted")
	}
	if s.homeMove.queued.Load() {
		t.Fatal("move queued")
	}
	if _, err := os.Stat(homeJournalPath(s.configDir)); !os.IsNotExist(err) {
		t.Fatal("expired request wrote journal", err)
	}
	// A duplicate request must not wait behind a stuck first request.
	s.homeMove.mu.Lock()
	defer s.homeMove.mu.Unlock()
	result := make(chan error, 1)
	go func() { result <- s.queueHomeMove(context.Background(), projectMoveRequest{}) }()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("concurrent confirmation accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("concurrent confirmation blocked")
	}
}

// Exercise the preview's and the confirmation's work checks against a live
// Runner contract. Storage is set up, so the HTTP preview and confirmation
// refuse in words first (errDeskHomeMoveAfterStorage); the checks keep their
// coverage below them, through the preview's own check of each Desk and the
// queue. The selected Desk differs from the one with the active trigger.
func TestDeskHomePreviewReportsActiveTriggerAndConfirmationRechecks(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	cfg := Config{ProjectDir: t.TempDir(), DeskConfigDir: t.TempDir(), Token: testToken, JpackBin: homeRuntime(t), HomeMoveShutdown: func() {}}
	privateFixture(t, filepath.Join(cfg.ProjectDir, projectConfigName), `{"deskConfigVersion":1}`)
	s, ts := startDesk(t, cfg)
	t.Cleanup(func() { ts.Close(); s.Close() })
	selected := createTestDesk(t, ts, "Research")
	named := createTestDesk(t, ts, "Sales demo")
	const jobID = "job_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	var paused atomic.Bool
	var unknown atomic.Bool
	var writes atomic.Int32
	companion := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			writes.Add(1)
			http.Error(w, "read-only check", 405)
			return
		}
		switch r.URL.Path {
		case "/v1/jobs":
			writeJSON(w, 200, map[string]any{"items": []any{map[string]string{"id": jobID, "name": "Evidence readiness"}}, "next": 0})
		case "/v1/jobs/" + jobID + "/triggers":
			trigger := map[string]any{"config": map[string]string{"name": "Evidence refresh"}}
			if !unknown.Load() {
				trigger["paused"] = paused.Load()
			}
			writeJSON(w, 200, map[string]any{"items": []any{trigger}})
		default:
			writeJSON(w, 200, map[string]any{"items": []any{}, "next": 0})
		}
	}))
	defer companion.Close()
	child := s.desks[named.ID]
	child.jobs = &jobsCompanion{url: companion.URL, token: "test-private", done: make(chan struct{}), stop: make(chan struct{})}
	t.Cleanup(func() { child.jobs.closed = true })
	request := projectMoveRequest{Destination: filepath.Join(t.TempDir(), "jpack-desk")}
	for _, path := range []string{"/api/desk-home/preview", "/api/desk-home/move"} {
		raw, _ := json.Marshal(request)
		if code, body := deskCall(t, ts, "POST", path, selected.ID, string(raw), true); code != 409 || refusalOf(body) != errDeskHomeMoveAfterStorage.Error() {
			t.Fatal(path, code, string(body))
		}
	}
	// The preview's check of each Desk's work, as its handler makes it.
	preview := func() deskHomePlan {
		t.Helper()
		plan, err := s.planDeskHome(request.Destination)
		if err != nil {
			t.Fatal(err)
		}
		for _, server := range s.homeServers() {
			if err := server.homeWorkBlocker(t.Context()); err != nil {
				plan.Blockers = append(plan.Blockers, err.Error())
			}
		}
		return plan
	}
	plan := preview()
	want := `Sales demo: Pause "Evidence refresh" in Jobs → Evidence readiness → Triggers`
	if len(plan.Blockers) != 1 || !strings.Contains(plan.Blockers[0], want) {
		t.Fatal(plan.Blockers)
	}
	paused.Store(true)
	plan = preview()
	if len(plan.Blockers) != 0 {
		t.Fatal(plan.Blockers)
	}
	request.Token = plan.Token
	// An enabled trigger after review must still prevent journaling and shutdown.
	paused.Store(false)
	if err := s.queueHomeMove(t.Context(), request); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatal(err)
	}
	if s.homeMove.queued.Load() {
		t.Fatal("blocked move queued")
	}
	if _, err := os.Stat(homeJournalPath(s.configDir)); !os.IsNotExist(err) {
		t.Fatal("blocked move wrote journal", err)
	}
	if _, err := os.Stat(request.Destination); !os.IsNotExist(err) {
		t.Fatal("preview or blocked confirmation moved files", err)
	}
	// A missing state cannot be treated as paused or presented as a known active trigger.
	unknown.Store(true)
	plan = preview()
	if len(plan.Blockers) != 1 || plan.Blockers[0] != "Sales demo: Jobs triggers could not be verified." {
		t.Fatal(plan.Blockers)
	}
	if writes.Load() != 0 {
		t.Fatal("preview or confirmation changed a trigger")
	}
}
