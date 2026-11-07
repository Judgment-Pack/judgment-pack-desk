package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func moveRequest(t *testing.T, ts *httptest.Server, action string, request projectMoveRequest) (int, []byte) {
	t.Helper()
	raw, _ := json.Marshal(request)
	req, _ := http.NewRequest("POST", ts.URL+"/api/project-location/"+action, bytes.NewReader(raw))
	bearer(req)
	response, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	return response.StatusCode, data
}
func movePreview(t *testing.T, ts *httptest.Server, destination string) projectMoveRequest {
	t.Helper()
	code, data := moveRequest(t, ts, "preview", projectMoveRequest{Destination: destination})
	if code != 200 {
		t.Fatalf("preview %d %s", code, data)
	}
	var preview projectMovePreview
	if err := json.Unmarshal(data, &preview); err != nil {
		t.Fatal(err)
	}
	return projectMoveRequest{Destination: destination, Token: preview.Token}
}
func stampMoveIdentity(t *testing.T, s *Server) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(s.projectDir, startupIdentityDir), 0700); err != nil {
		t.Fatal(err)
	}
	privateFixture(t, filepath.Join(s.projectDir, startupIdentityDir, startupIdentityName), identityLine(strings.Repeat("c", 64), s.projectDir, ""))
}

// These follow a project folder moved outside Desk. Storage registers a project
// at its folder, and v0.14.0 refuses a start there in words instead.
const projectFollowedOutsideDesk = "v0.14.0 refuses a project moved outside Desk in words (TestProjectMovedOutsideDeskIsRefusedInWords); following the folder by its identity is Desk #698"

func TestRecognizedMoveLinksAllHistoryBeforeIdentity(t *testing.T) {
	t.Skip(projectFollowedOutsideDesk)
	s, ts, _ := assistantServer(t)
	stampMoveIdentity(t, s)
	if code, _ := chatRequest(t, ts, "PUT", "absent", storageChat, true, ""); code != 200 {
		t.Fatal(code)
	}
	storageChat := savedConversationBytes(t, s, ts)
	location := storageGet(t, ts)
	source := s.conversationName()
	siblings := map[string]string{draftPackName(source): `{"version":1,"drafts":[]}`, strings.Replace(source, "conversations-", "briefs-", 1): `{"version":1,"entries":[]}`, attachmentProjectPrefix(source) + "00000000-0000-0000-0000-000000000000.json": `{"retained":"unchanged"}`}
	for name, raw := range siblings {
		privateFixture(t, filepath.Join(location.Path, name), raw)
	}
	previous := s.projectDir
	destination := previous + "-moved"
	if err := os.Rename(previous, destination); err != nil {
		t.Fatal(err)
	}
	again, ats, _ := startedAt(t, s, destination)
	if again.locationStatus().Problem != "" {
		t.Fatal(again.locationStatus())
	}
	if code, reply := chatRequest(t, ats, "GET", "", "", true, ""); code != 200 || string(reply.Content) != storageChat {
		t.Fatalf("history %d %s", code, reply.Content)
	}
	private, err := again.openIdentityFolder()
	if err != nil {
		t.Fatal(err)
	}
	record, _, _, err := readIdentity(private)
	private.Close()
	if err != nil || record.Path != destination {
		t.Fatal(record, err)
	}
	for name, raw := range siblings {
		mustBytes(t, filepath.Join(location.Path, name), raw)
	}
	// A second manual move follows the same original namespace.
	third := destination + "-again"
	if err := os.Rename(destination, third); err != nil {
		t.Fatal(err)
	}
	last, lts, _ := startedAt(t, again, third)
	if code, reply := chatRequest(t, lts, "GET", "", "", true, ""); code != 200 || string(reply.Content) != storageChat {
		t.Fatalf("second history %d %s", code, reply.Content)
	}
	if last.locationStatus().Problem != "" {
		t.Fatal(last.locationStatus())
	}
}
func TestMovedHistoryWithoutChatsAndConflicts(t *testing.T) {
	t.Skip(projectFollowedOutsideDesk)
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "draft only", true: "destination draft conflict"}[conflict], func(t *testing.T) {
			s, ts, _ := assistantServer(t)
			stampMoveIdentity(t, s)
			location := storageGet(t, ts)
			old := s.projectDir
			destination := old + "-moved"
			source := s.conversationName()
			privateFixture(t, filepath.Join(location.Path, draftPackName(source)), `{"version":1,"drafts":[]}`)
			if conflict {
				privateFixture(t, filepath.Join(location.Path, "briefs-"+digestOf([]byte(destination))+".json"), `{"version":1,"entries":[]}`)
			}
			if err := os.Rename(old, destination); err != nil {
				t.Fatal(err)
			}
			again, ats, _ := startedAt(t, s, destination)
			private, err := again.openIdentityFolder()
			if err != nil {
				t.Fatal(err)
			}
			record, _, _, err := readIdentity(private)
			private.Close()
			if err != nil {
				t.Fatal(err)
			}
			if conflict {
				if record.Path != old || again.locationStatus().Problem != historyMoveConflict {
					t.Fatal("old path lost", record, again.locationStatus())
				}
				if code, _ := chatRequest(t, ats, "PUT", "absent", storageChat, true, ""); code != 409 {
					t.Fatal("created fresh history", code)
				}
			} else {
				if record.Path != destination || again.locationStatus().Problem != "" {
					t.Fatal(record, again.locationStatus())
				}
				store, err := again.openChatData()
				if err != nil {
					t.Fatal(err)
				}
				defer store.root.Close()
				name, err := resolveConversationName(store.root, destination)
				if err != nil || name != source {
					t.Fatal(name, err)
				}
			}
		})
	}
}

func TestMovedHistoryWithoutMarkerValidatesPreviousRuns(t *testing.T) {
	t.Skip(projectFollowedOutsideDesk)
	for _, kind := range []string{"empty ledger", "malformed ledger", "wrong namespace"} {
		t.Run(kind, func(t *testing.T) {
			s, ts, _ := assistantServer(t)
			stampMoveIdentity(t, s)
			location := storageGet(t, ts)
			old := s.projectDir
			destination := old + "-moved"
			source := s.conversationName()
			namespace := digestOf([]byte(old))
			draft := `{"version":1,"drafts":[]}`
			privateFixture(t, filepath.Join(location.Path, draftPackName(source)), draft)
			raw := `{"version":1,"project":"` + namespace + `","runs":[]}`
			switch kind {
			case "malformed ledger":
				raw = `{"version":1,"project":"` + namespace + `","runs":[{}]}`
			case "wrong namespace":
				raw = `{"version":1,"project":"` + digestOf([]byte(destination)) + `","runs":[]}`
			}
			name := "assistant-runs-" + namespace + ".json"
			privateFixture(t, filepath.Join(location.Path, name), raw)
			before := dialectArtifactOwnerRead(t, filepath.Join(old, startupIdentityDir, startupIdentityName))
			if err := os.Rename(old, destination); err != nil {
				t.Fatal(err)
			}
			again, _, _ := startedAt(t, s, destination)
			if kind == "empty ledger" {
				if again.locationStatus().Problem != "" {
					t.Fatal(again.locationStatus())
				}
				store, err := again.openChatData()
				if err != nil {
					t.Fatal(err)
				}
				defer store.root.Close()
				if name, err := resolveConversationName(store.root, destination); err != nil || name != source {
					t.Fatal(name, err)
				}
			} else {
				if again.locationStatus().Problem == "" {
					t.Fatal("invalid previous ledger allowed history move")
				}
				mustBytes(t, filepath.Join(destination, startupIdentityDir, startupIdentityName), string(before))
			}
			mustBytes(t, filepath.Join(location.Path, name), raw)
			mustBytes(t, filepath.Join(location.Path, draftPackName(source)), draft)
		})
	}
}

func TestProjectMoveConfirmedAndRestarted(t *testing.T) {
	s, ts, _ := assistantServer(t)
	if code, _ := chatRequest(t, ts, "PUT", "absent", storageChat, true, ""); code != 200 {
		t.Fatal(code)
	}
	storageChat := savedConversationBytes(t, s, ts)
	previous := s.projectDir
	destination := filepath.Join(t.TempDir(), "new project")
	request := movePreview(t, ts, destination)
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatal("preview moved folder")
	}
	code, data := moveRequest(t, ts, "move", request)
	if code != 200 {
		t.Fatalf("move %d %s", code, data)
	}
	var status projectLocation
	json.Unmarshal(data, &status)
	if !status.RestartRequired || status.MovedTo != destination || status.Problem != "" {
		t.Fatalf("status %s", data)
	}
	if _, err := os.Stat(previous); !os.IsNotExist(err) {
		t.Fatal("source remains", err)
	}
	if code, _ := chatRequest(t, ts, "PUT", "absent", storageChat, true, ""); code != 409 {
		t.Fatal("writes not frozen", code)
	}
	selected, err := ResolveProjectDir("", s.configDir)
	if err != nil || selected != destination {
		t.Fatal(selected, err)
	}
	again, ats, _ := startedAt(t, s, destination)
	if again.projectHasMoved() {
		t.Fatal("new process is frozen")
	}
	if code, reply := chatRequest(t, ats, "GET", "", "", true, ""); code != 200 || string(reply.Content) != storageChat {
		t.Fatalf("lost history %d %s", code, reply.Content)
	}
}

// **A project with history moves after a restart.** The chat's first save
// enrolls the project's history; Desk restarts on the project and moves it from
// there. The start at the destination opens the same history writable.
func TestProjectMoveAfterRestartKeepsHistory(t *testing.T) {
	s, ts, _ := assistantServer(t)
	if code, _ := chatRequest(t, ts, "PUT", "absent", storageChat, true, ""); code != 200 {
		t.Fatal(code)
	}
	storageChat := savedConversationBytes(t, s, ts)
	again, ats, _ := startedAt(t, s, s.projectDir)
	if again.storageReadOnly {
		t.Fatalf("the restart was read-only: %v", again.storageProblem)
	}
	destination := filepath.Join(t.TempDir(), "new project")
	request := movePreview(t, ats, destination)
	if code, data := moveRequest(t, ats, "move", request); code != 200 {
		t.Fatalf("move after a restart %d %s", code, data)
	}
	moved, mts, _ := startedAt(t, again, destination)
	if moved.storageReadOnly || moved.locationStatus().Problem != "" {
		t.Fatalf("the moved project started read-only: %v %+v", moved.storageProblem, moved.locationStatus())
	}
	if code, reply := chatRequest(t, mts, "GET", "", "", true, ""); code != 200 || string(reply.Content) != storageChat {
		t.Fatalf("lost history %d %s", code, reply.Content)
	}
}

// **A project moved outside Desk is refused in words.** Storage registered
// the project at its folder when its history was enrolled. Renamed by hand,
// the start found the same folder at another path and refused custody, so
// Desk ran read-only behind a generic pause. It now does not start, says the
// project was moved outside Desk, and names both folders, moving it back and
// Move…. Moved back, the project starts with its history.
func TestProjectMovedOutsideDeskIsRefusedInWords(t *testing.T) {
	for _, identity := range []bool{false, true} {
		t.Run(map[bool]string{false: "history", true: "history and identity"}[identity], func(t *testing.T) {
			s, ts, _ := assistantServer(t)
			if code, _ := chatRequest(t, ts, "PUT", "absent", storageChat, true, ""); code != 200 {
				t.Fatal(code)
			}
			if identity {
				// The producer's private folder and identity: the lineage
				// then names the project's accounting key and history path.
				storageTestMust(t, s.ensureDialectDraftEnrollment(t.Context()))
				if _, err := os.Stat(filepath.Join(s.projectDir, startupIdentityDir, startupIdentityName)); err != nil {
					t.Fatal(err)
				}
			}
			storageChat := savedConversationBytes(t, s, ts)
			previous := s.projectDir
			destination := previous + "-renamed"
			storageTestMust(t, s.Close())
			storageTestMust(t, os.Rename(previous, destination))
			cfg := s.cfg
			cfg.Root, cfg.ProjectDir = nil, destination
			refused, err := New(cfg)
			if refused != nil {
				refused.Close()
			}
			if !errors.Is(err, errDeskProjectMovedOutside) || refused != nil {
				t.Fatalf("a project moved outside Desk started: %v", err)
			}
			for _, words := range []string{"moved outside Desk", previous, destination, "Move the folder back", "Move…"} {
				if !strings.Contains(err.Error(), words) {
					t.Fatalf("the refusal does not say %q: %v", words, err)
				}
			}
			storageTestMust(t, os.Rename(destination, previous))
			again, ats, _ := startedAt(t, s, previous)
			if again.storageReadOnly {
				t.Fatalf("the project moved back started read-only: %v", again.storageProblem)
			}
			if code, reply := chatRequest(t, ats, "GET", "", "", true, ""); code != 200 || string(reply.Content) != storageChat {
				t.Fatalf("history after moving back %d %s", code, reply.Content)
			}
		})
	}
}
func TestProjectMoveNeverReplacesDestinationOrStaleConfig(t *testing.T) {
	for _, kind := range []string{"occupied", "stale", "history", "busy", "tool"} {
		t.Run(kind, func(t *testing.T) {
			s, ts, _ := assistantServer(t)
			destination := filepath.Join(t.TempDir(), "new")
			request := movePreview(t, ts, destination)
			switch kind {
			case "occupied":
				if err := os.Mkdir(destination, 0700); err != nil {
					t.Fatal(err)
				}
				privateFixture(t, filepath.Join(destination, "keep"), "keep")
			case "stale":
				privateFixture(t, filepath.Join(s.configDir, deskConfigName), `{"project":{"file":null}}`)
			case "history":
				store, err := s.openChatData()
				if err != nil {
					t.Fatal(err)
				}
				defer store.root.Close()
				if err = claimPrivateData(store.root, "pack-tests-"+digestOf([]byte(destination))+".json", []byte(`{"version":1,"entries":[]}`)); err != nil {
					t.Fatal(err)
				}
			case "busy":
				s.location.use.RLock()
				defer s.location.use.RUnlock()
			case "tool":
				c := &conn{requests: map[string]bool{"1": true}}
				s.mu.Lock()
				s.conns[c] = struct{}{}
				s.mu.Unlock()
				defer func() { s.mu.Lock(); delete(s.conns, c); s.mu.Unlock() }()
			}
			if code, data := moveRequest(t, ts, "move", request); code != 409 {
				t.Fatalf("accepted %d %s", code, data)
			}
			if _, err := os.Stat(s.projectDir); err != nil {
				t.Fatal(err)
			}
			if kind == "occupied" {
				mustBytes(t, filepath.Join(destination, "keep"), "keep")
			}
		})
	}
}

// The chat's first save enrolls the project's history, so every cut below is a
// move of a project with history. Each must actually be reached: a move that
// fails earlier also answers 409. A stop before the move's Pending row leaves
// an unknown prefix: the restart stays read-only and keeps the intent as it
// was. Every later stop is finished or rolled back by the restart.
func TestProjectMoveCrashRecovery(t *testing.T) {
	for _, step := range []string{"intent-claimed", "admitted-before-stage", "before-rename", "after-rename", "terminal-before-current"} {
		t.Run(step, func(t *testing.T) {
			s, ts, _ := assistantServer(t)
			chatRequest(t, ts, "PUT", "absent", storageChat, true, "")
			storageChat := savedConversationBytes(t, s, ts)
			destination := filepath.Join(t.TempDir(), "new")
			request := movePreview(t, ts, destination)
			reached := false
			testProjectMoveStep = func(at string) error {
				if at == step {
					reached = true
					return errors.New("simulated stop")
				}
				return nil
			}
			defer func() { testProjectMoveStep = nil }()
			code, data := moveRequest(t, ts, "move", request)
			testProjectMoveStep = nil
			if !reached {
				t.Fatalf("the move stopped before %s: %d %s", step, code, data)
			}
			next := s.projectDir
			if step == "after-rename" || step == "terminal-before-current" {
				if code != 200 {
					t.Fatal(code, string(data))
				}
				next = destination
				selected, err := ResolveProjectDir("", s.configDir)
				if err != nil || selected != next {
					t.Fatal(selected, err)
				}
			} else if code != 409 {
				t.Fatal(code, string(data))
			}
			if step == "intent-claimed" || step == "admitted-before-stage" {
				intent, err := os.ReadFile(filepath.Join(s.configDir, projectMoveName))
				if err != nil {
					t.Fatal(err)
				}
				again, _, _ := startedAt(t, s, next)
				if !again.storageReadOnly || !errors.Is(again.storageProblem, errStorageEnrollmentUnresolved) {
					t.Fatal("an unknown move prefix became writable", again.storageProblem)
				}
				mustBytes(t, filepath.Join(s.configDir, projectMoveName), string(intent))
				return
			}
			again, ats, _ := startedAt(t, s, next)
			if again.locationStatus().Problem != "" {
				t.Fatal(again.locationStatus())
			}
			if _, err := os.Stat(filepath.Join(s.configDir, projectMoveName)); !os.IsNotExist(err) {
				t.Fatal("intent not cleared", err)
			}
			if code, reply := chatRequest(t, ats, "GET", "", "", true, ""); code != 200 || string(reply.Content) != storageChat {
				t.Fatal("history lost", code)
			}
		})
	}
}

func TestConfirmedMoveOrCopyKeepsHistorySeparate(t *testing.T) {
	t.Skip(projectFollowedOutsideDesk)
	for _, choice := range []string{identityMoved, identityCopy} {
		t.Run(choice, func(t *testing.T) {
			s, ts, _ := assistantServer(t)
			stampMoveIdentity(t, s)
			chatRequest(t, ts, "PUT", "absent", storageChat, true, "")
			storageChat := savedConversationBytes(t, s, ts)
			previous := s.projectDir
			destination := previous + "-new"
			record := identityRecord{ID: strings.Repeat("c", 64), Path: previous}
			privateFixture(t, filepath.Join(previous, startupIdentityDir, startupIdentityName), string(record.line()))
			if err := os.Rename(previous, destination); err != nil {
				t.Fatal(err)
			}
			again, ats, _ := startedAt(t, s, destination)
			offer := offerOf(t, again)
			token := offer.Moved
			if choice == identityCopy {
				token = offer.Copy
			}
			if failure := again.resolveIdentity(context.Background(), choice, token); failure != nil {
				t.Fatal(failure)
			}
			code, reply := chatRequest(t, ats, "GET", "", "", true, "")
			if code != 200 {
				t.Fatal(code)
			}
			if choice == identityMoved && string(reply.Content) != storageChat {
				t.Fatal("move lost chats")
			}
			if choice == identityCopy && reply.SHA256 != "absent" {
				t.Fatal("copy inherited chats")
			}
		})
	}
}
func TestProjectMoveDestinationAppearsAfterReview(t *testing.T) {
	s, ts, _ := assistantServer(t)
	destination := filepath.Join(t.TempDir(), "new")
	request := movePreview(t, ts, destination)
	testProjectMoveStep = func(at string) error {
		if at == "before-rename" {
			return os.Mkdir(destination, 0700)
		}
		return nil
	}
	defer func() { testProjectMoveStep = nil }()
	if code, data := moveRequest(t, ts, "move", request); code != 409 {
		t.Fatal(code, string(data))
	}
	if _, err := os.Stat(s.projectDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(destination); err != nil {
		t.Fatal(err)
	}
}
func TestProjectMoveRecoveryWillNotOverwriteLaterSettings(t *testing.T) {
	s, ts, _ := assistantServer(t)
	destination := filepath.Join(t.TempDir(), "new")
	request := movePreview(t, ts, destination)
	testProjectMoveStep = func(at string) error {
		if at == "after-rename" {
			return errors.New("stop")
		}
		return nil
	}
	defer func() { testProjectMoveStep = nil }()
	moveRequest(t, ts, "move", request)
	testProjectMoveStep = nil
	replacement := `{"project":{"file":null}}`
	privateFixture(t, filepath.Join(s.configDir, deskConfigName), replacement)
	if _, err := OpenProject("", s.configDir); err == nil {
		t.Fatal("ignored later startup edit")
	}
	settingsFile := filepath.Join(s.configDir, deskConfigName)
	again, ats, _ := startedAt(t, s, destination)
	// The conflict is worded and names the file; the start stays read only
	// (the move's recovery is unresolved) and never overwrites the later edit.
	status := again.locationStatus()
	if !again.storageReadOnly || !strings.Contains(status.Problem, settingsFile) || !strings.Contains(status.Problem, "edited after the project was moved") || !strings.Contains(status.Problem, "Finish the move with the moved settings") {
		t.Fatalf("unreported settings conflict: %v %q", again.storageReadOnly, status.Problem)
	}
	conflict := status.SettingsConflict
	if conflict == nil || !conflict.CanFinish || conflict.Finished || conflict.File != settingsFile || len(conflict.Later) != 1 || !strings.Contains(conflict.Later[0], "the startup project") || !strings.Contains(status.Problem, conflict.Later[0]) {
		t.Fatalf("the conflict did not offer its one choice and the later edit: %+v", conflict)
	}
	mustBytes(t, settingsFile, replacement)
	// The location card answers while read only; the choice needs the exact
	// conflict this start reported.
	if code, data := deskCall(t, ats, "GET", "/api/project-location", "", "", true); code != 200 || !strings.Contains(string(data), `"canFinish":true`) {
		t.Fatalf("the read-only location card %d %s", code, data)
	}
	if code, data := deskCall(t, ats, "POST", "/api/project-location/finish-move", "", `{"digest":"`+strings.Repeat("0", 64)+`"}`, true); code != 409 {
		t.Fatalf("a stale choice was taken %d %s", code, data)
	}
	mustBytes(t, settingsFile, replacement)
	// Another edit after the start reported the conflict is not removed unseen.
	again2 := `{"project":{"file":null},"assistant":{}}`
	privateFixture(t, settingsFile, again2)
	if code, data := deskCall(t, ats, "POST", "/api/project-location/finish-move", "", `{"digest":"`+conflict.Digest+`"}`, true); code != 409 {
		t.Fatalf("a settings file changed since the start was removed %d %s", code, data)
	}
	mustBytes(t, settingsFile, again2)
	privateFixture(t, settingsFile, replacement)
	code, data := deskCall(t, ats, "POST", "/api/project-location/finish-move", "", `{"digest":"`+conflict.Digest+`"}`, true)
	var finished projectLocation
	if code != 200 || json.Unmarshal(data, &finished) != nil || !finished.RestartRequired || finished.SettingsConflict == nil || !finished.SettingsConflict.Finished || !strings.Contains(finished.Problem, "make this later edit again: "+conflict.Later[0]) {
		t.Fatalf("finish the move with the moved settings %d %s", code, data)
	}
	if code, _ := deskCall(t, ats, "POST", "/api/project-location/finish-move", "", `{"digest":"`+conflict.Digest+`"}`, true); code != 409 {
		t.Fatal("the choice was taken twice")
	}
	// The next start's recovery writes the move's committed settings and
	// finishes the move: writable, no problem, the project pointing at the
	// moved folder, and the recovery record gone.
	done, _, _ := startedAt(t, again, destination)
	if done.storageReadOnly || done.locationStatus().Problem != "" || done.locationStatus().SettingsConflict != nil {
		t.Fatalf("the move did not finish after the choice: %v %q", done.storageProblem, done.locationStatus().Problem)
	}
	present, current, err := done.readDeskFile()
	if err != nil || !present || decodeDeskFile(current).ProjectFile != filepath.Join(destination, projectConfigName) {
		t.Fatalf("the moved settings were not written: %v %s", err, current)
	}
	if _, found, err := readProjectMove(done.assistant.root); err != nil || found {
		t.Fatalf("the move's recovery record remained: %v %v", found, err)
	}
}
func TestProjectLocationIsAuthenticatedInstallationScope(t *testing.T) {
	s, ts, _ := assistantServer(t)
	for _, authorized := range []bool{false, true} {
		req, _ := http.NewRequest("GET", ts.URL+"/api/project-location", nil)
		req.Header.Set("X-Jpack-Desk", strings.Repeat("f", 32))
		if authorized {
			bearer(req)
		}
		res, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if authorized {
			var status projectLocation
			json.Unmarshal(data, &status)
			if res.StatusCode != 200 || status.Path != s.projectDir {
				t.Fatal(res.StatusCode, string(data))
			}
		} else if res.StatusCode != 401 {
			t.Fatal("unauthorized", res.StatusCode)
		}
	}
}

func TestProjectMoveEmptyRunner(t *testing.T) {
	runner := os.Getenv("JPACK_RUNNER_TEST_BIN")
	runtimeBin := os.Getenv("JPACK_BIN")
	if runner == "" || runtimeBin == "" {
		t.Skip("requires the local Runner and Runtime test binaries")
	}
	s, ts := startDesk(t, Config{ProjectDir: t.TempDir(), DeskConfigDir: t.TempDir(), Token: testToken, RunnerBin: runner, JpackBin: runtimeBin})
	defer s.Close()
	defer ts.Close()
	request := movePreview(t, ts, filepath.Join(t.TempDir(), "moved"))
	if code, data := moveRequest(t, ts, "move", request); code != 200 {
		t.Fatal(code, string(data))
	}
}
func TestProjectMoveTracksOnlyRuntimeRequests(t *testing.T) {
	s, _, _ := assistantServer(t)
	c := &conn{}
	if !s.beginProjectRequest(c, []byte(`{"jsonrpc":"2.0","method":"tools/call","id":1}`)) {
		t.Fatal("request refused")
	}
	if len(c.requests) != 1 {
		t.Fatal("request not tracked")
	}
	c.finishProjectRequest([]byte(`{"jsonrpc":"2.0","method":"notifications/progress","params":{}}`))
	if len(c.requests) != 1 {
		t.Fatal("notification released work")
	}
	c.finishProjectRequest([]byte(`{"jsonrpc":"2.0","result":{},"id":1}`))
	if len(c.requests) != 0 {
		t.Fatal("response did not finish work")
	}
	if !s.beginProjectRequest(c, []byte(`{"jsonrpc":"2.0","id":2,"result":{}}`)) || len(c.requests) != 0 {
		t.Fatal("client answer tracked as a new runtime request")
	}
	s.location.use.Lock()
	defer s.location.use.Unlock()
	if s.beginProjectRequest(c, []byte(`{"jsonrpc":"2.0","method":"tools/call","id":3}`)) {
		t.Fatal("work began during move")
	}
}
