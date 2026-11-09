package desk

// v0.13.2's Desk home moves, as these tests read them before storage refused
// home moves (8d1055cb): with the storage line off, the moves still work.

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDeskHomeAPIRequiresReviewAndStopsBeforeMoveStorageLineOff(t *testing.T) {
	if storageLineOn() {
		t.Skip("v0.13.2 behaviour, with the storage line off; TestDeskHomeAPIRequiresReviewAndStopsBeforeMove covers it on")
	}
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	shutdown := make(chan struct{}, 1)
	cfg := Config{ProjectDir: t.TempDir(), DeskConfigDir: t.TempDir(), Token: testToken, JpackBin: os.Getenv("JPACK_BIN"), HomeMoveShutdown: func() { shutdown <- struct{}{} }}
	privateFixture(t, filepath.Join(cfg.ProjectDir, projectConfigName), `{"deskConfigVersion":1}`)
	s, ts := startDesk(t, cfg)
	destination := filepath.Join(t.TempDir(), "jpack-desk")
	request, _ := json.Marshal(projectMoveRequest{Destination: destination})
	if code, _ := deskCall(t, ts, "POST", "/api/desk-home/preview", "", string(request), false); code != 401 {
		t.Fatal("unauthenticated preview", code)
	}
	code, raw := deskCall(t, ts, "POST", "/api/desk-home/preview", "", string(request), true)
	var plan deskHomePlan
	if code != 200 || json.Unmarshal(raw, &plan) != nil || len(plan.Blockers) > 0 {
		t.Fatal(code, string(raw))
	}
	if code, _ = deskCall(t, ts, "POST", "/api/desk-home/move", "", string(request), true); code != 409 {
		t.Fatal("accepted without review", code)
	}
	if s.homeMove.queued.Load() {
		t.Fatal("refusal scheduled a move")
	}
	request, _ = json.Marshal(projectMoveRequest{Destination: destination, Token: plan.Token})
	code, raw = deskCall(t, ts, "POST", "/api/desk-home/move", "", string(request), true)
	if code != 200 {
		t.Fatal(code, string(raw))
	}
	if _, err := os.Stat(cfg.ProjectDir); err != nil {
		t.Fatal("moved before shutdown", err)
	}
	if code, _ := deskCall(t, ts, "PUT", "/api/conversations", "", storageChat, true); code != 409 {
		t.Fatal("queued move allowed new writes", code)
	}
	if err := s.FinishDeskHomeMove(); err == nil {
		t.Fatal("moved while live process lease was held")
	}
	select {
	case <-shutdown:
	case <-time.After(3 * time.Second):
		t.Fatal("no shutdown request")
	}
	ts.Close()
	s.Close()
	if err := s.FinishDeskHomeMove(); err != nil {
		t.Fatal(err)
	}
	tx, err := readHomeTransaction(homeJournalPath(cfg.DeskConfigDir))
	if err != nil || tx == nil || !tx.Complete {
		t.Fatal("migration not complete", err)
	}
}

func TestDeskHomeMigrationPreservesHistoryCredentialsAndNamedDeskStorageLineOff(t *testing.T) {
	if storageLineOn() {
		t.Skip("v0.13.2 behaviour, with the storage line off; TestDeskHomeMigrationPreservesHistoryCredentialsAndNamedDesk covers it on")
	}
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	s, ts, _ := assistantServer(t)
	if code, _ := chatRequest(t, ts, "PUT", "absent", storageChat, true, ""); code != 200 {
		t.Fatal(code)
	}
	storageChat := savedConversationBytes(t, s, ts)
	named := createTestDesk(t, ts, "Research · Demo")
	privateFixture(t, filepath.Join(s.configDir, "secrets", "migration-fixture"), "opaque credential bytes\n")
	privateFixture(t, filepath.Join(named.Folder, ".desk-private", "retained-proof.json"), "{\"signed\":\"opaque exact bytes\"}\n")
	destination := filepath.Join(t.TempDir(), "jpack-desk")
	plan, err := s.planDeskHome(destination)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Blockers) != 0 {
		t.Fatal(plan.Blockers)
	}
	if _, err = os.Stat(destination); !os.IsNotExist(err) {
		t.Fatal("preview created destination", err)
	}
	tx, err := s.prepareHomeTransaction(plan)
	if err != nil {
		t.Fatal(err)
	}
	cfg := s.cfg
	ts.Close()
	s.Close()
	journal := homeJournalPath(tx.Config)
	if err = saveHomeTransaction(journal, tx, true); err != nil {
		t.Fatal(err)
	}
	// Stop after a parent rename. Recovery must infer that completed step from
	// its retained folder identity, then continue the nested relocations.
	testHomeMigrationStep = func(step string) error {
		if step == "rename-0" {
			return errors.New("simulated crash")
		}
		return nil
	}
	t.Cleanup(func() { testHomeMigrationStep = nil })
	if err = applyHomeTransaction(journal, &tx); err == nil {
		t.Fatal("crash hook ignored")
	}
	testHomeMigrationStep = nil
	if err = applyHomeTransaction(journal, &tx); err != nil {
		t.Fatal(err)
	}
	if !tx.Complete {
		t.Fatal("not committed")
	}
	var startup deskHomeEntry
	var movedNamed deskHomeEntry
	for _, entry := range plan.Registry.Desks {
		if entry.Kind == "project" {
			startup = entry
		} else if entry.ID == named.ID {
			movedNamed = entry
		}
	}
	cfg.ProjectDir = filepath.Join(destination, startup.Folder)
	cfg.Root = nil
	cfg.DeskConfigDir = filepath.Join(destination, deskHomePlatform)
	if chosen, err := ResolveProjectDir("", cfg.DeskConfigDir); err != nil || chosen != cfg.ProjectDir {
		t.Fatal("saved startup cannot open", chosen, err)
	}
	again, ats := startDesk(t, cfg)
	t.Cleanup(func() { ats.Close(); again.Close() })
	if code, reply := chatRequest(t, ats, "GET", "", "", true, ""); code != 200 || string(reply.Content) != storageChat {
		t.Fatal("history lost", code, reply)
	}
	if _, ok := again.desks[named.ID]; !ok {
		t.Fatal("named Desk was not resumed")
	}
	if raw, err := os.ReadFile(filepath.Join(cfg.DeskConfigDir, "secrets", "migration-fixture")); err != nil || string(raw) != "opaque credential bytes\n" {
		t.Fatal("credential changed", err)
	}
	if raw, err := os.ReadFile(filepath.Join(destination, movedNamed.Folder, ".desk-private", "retained-proof.json")); err != nil || string(raw) != "{\"signed\":\"opaque exact bytes\"}\n" {
		t.Fatal("retained evidence changed", err)
	}
	if shared, reason := again.identityShared(); shared {
		t.Fatal("move became an identity conflict", reason)
	}
}

func TestDeskHomeWholeMoveKeepsNewDesksAndRecoversAfterPointerStorageLineOff(t *testing.T) {
	if storageLineOn() {
		t.Skip("v0.13.2 behaviour, with the storage line off; TestDeskHomeWholeMoveKeepsNewDesksAndRecoversAfterPointer covers it on")
	}
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	cfg, _ := homeFixture(t)
	s, ts := startDesk(t, cfg)
	named := createTestDesk(t, ts, "New research")
	destination := filepath.Join(t.TempDir(), "jpack-desk")
	plan, err := s.planDeskHome(destination)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Registry.Desks) != 2 {
		t.Fatal("cached registry omitted newly created Desk")
	}
	tx, err := s.prepareHomeTransaction(plan)
	if err != nil {
		t.Fatal(err)
	}
	ts.Close()
	s.Close()
	journal := homeJournalPath(tx.Config)
	if err = saveHomeTransaction(journal, tx, true); err != nil {
		t.Fatal(err)
	}
	testHomeMigrationStep = func(step string) error {
		if step == "write-"+strconv.Itoa(len(tx.Writes)-1) {
			return errors.New("stopped after pointer")
		}
		return nil
	}
	t.Cleanup(func() { testHomeMigrationStep = nil })
	if err = finishHomeTransaction(journal); err == nil {
		t.Fatal("missing simulated crash")
	}
	testHomeMigrationStep = nil
	if err = finishHomeTransaction(journal); err != nil {
		t.Fatal("recovery failed", err)
	}
	cfg.ProjectDir = filepath.Join(destination, "unveil")
	cfg.DeskConfigDir = filepath.Join(destination, deskHomePlatform)
	again, ats := startDesk(t, cfg)
	t.Cleanup(func() { ats.Close(); again.Close() })
	if again.desks[named.ID] == nil {
		t.Fatal("new Desk lost after move")
	}
}

// Exercise the HTTP preview and confirmation against a live Runner contract.
// The selected Desk differs from the one with the active trigger.
func TestDeskHomePreviewReportsActiveTriggerAndConfirmationRechecksStorageLineOff(t *testing.T) {
	if storageLineOn() {
		t.Skip("v0.13.2 behaviour, with the storage line off; TestDeskHomePreviewReportsActiveTriggerAndConfirmationRechecks covers it on")
	}
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
	preview := func() deskHomePlan {
		t.Helper()
		raw, _ := json.Marshal(request)
		code, body := deskCall(t, ts, "POST", "/api/desk-home/preview", selected.ID, string(raw), true)
		var plan deskHomePlan
		if code != 200 || json.Unmarshal(body, &plan) != nil {
			t.Fatal(code, string(body))
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
	raw, _ := json.Marshal(request)
	code, body := deskCall(t, ts, "POST", "/api/desk-home/move", selected.ID, string(raw), true)
	var failure struct {
		Error string `json:"error"`
	}
	if code != 409 || json.Unmarshal(body, &failure) != nil || !strings.Contains(failure.Error, want) {
		t.Fatal(code, string(body))
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
