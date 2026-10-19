package desk

import (
	"encoding/json"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultProjectMoveReviewsWithoutCreatingAndKeepsHistory(t *testing.T) {
	dataHome := filepath.Join(t.TempDir(), "missing", "data")
	t.Setenv("XDG_DATA_HOME", dataHome)
	s, ts, _ := assistantServer(t)
	if code, _ := chatRequest(t, ts, "PUT", "absent", storageChat, true, ""); code != 200 {
		t.Fatal(code)
	}
	managed := createTestDesk(t, ts, "Research stays here")
	code, raw := deskCall(t, ts, "GET", "/api/project-location", managed.ID, "", true)
	var status projectLocation
	if code != 200 || json.Unmarshal(raw, &status) != nil {
		t.Fatal(code, string(raw))
	}
	destination := filepath.Join(dataHome, deskDirName, "projects", filepath.Base(s.projectDir))
	if status.Path != s.projectDir || status.DefaultDestination != destination {
		t.Fatal(status)
	}
	code, raw = deskCall(t, ts, "GET", "/api/desk-config", managed.ID, "", true)
	var current struct{ Project struct{ Dir string } }
	if code != 200 || json.Unmarshal(raw, &current) != nil || current.Project.Dir != managed.Folder {
		t.Fatal("current Desk location", code, string(raw))
	}
	code, raw = moveRequest(t, ts, "preview", projectMoveRequest{Destination: destination})
	var preview projectMovePreview
	if code != 200 || json.Unmarshal(raw, &preview) != nil || !preview.CreateParent {
		t.Fatal(code, string(raw))
	}
	if _, err := os.Stat(dataHome); !os.IsNotExist(err) {
		t.Fatal("review created default folders", err)
	}
	request := projectMoveRequest{Destination: destination, Token: preview.Token}
	if code, raw = moveRequest(t, ts, "move", request); code != 200 {
		t.Fatal(code, string(raw))
	}
	if _, err := os.Stat(s.projectDir); !os.IsNotExist(err) {
		t.Fatal("source was not moved", err)
	}
	if _, err := os.Stat(managed.Folder); err != nil {
		t.Fatal("managed Desk moved", err)
	}
	if info, err := os.Stat(filepath.Dir(destination)); err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("default parent must be private", info, err)
	}
	again, ats, _ := startedAt(t, s, destination)
	if again.locationStatus().Problem != "" {
		t.Fatal(again.locationStatus())
	}
	if code, reply := chatRequest(t, ats, "GET", "", "", true, ""); code != 200 || string(reply.Content) != storageChat {
		t.Fatal("history did not follow move", code, reply)
	}
}

func TestDefaultProjectMoveRefusesChangedParentsAndOccupiedDestinations(t *testing.T) {
	for _, change := range []string{"parent appeared", "occupied", "symlink", "unsafe permissions"} {
		t.Run(change, func(t *testing.T) {
			dataHome := t.TempDir()
			t.Setenv("XDG_DATA_HOME", dataHome)
			s, ts, _ := assistantServer(t)
			destination := s.defaultProjectDestination()
			request := movePreview(t, ts, destination)
			parent := filepath.Dir(destination)
			switch change {
			case "parent appeared":
				if err := os.MkdirAll(parent, 0700); err != nil {
					t.Fatal(err)
				}
			case "occupied":
				if err := os.MkdirAll(destination, 0700); err != nil {
					t.Fatal(err)
				}
				privateFixture(t, filepath.Join(destination, "keep"), "untouched")
			case "symlink":
				if err := os.Symlink(t.TempDir(), filepath.Join(dataHome, deskDirName)); err != nil {
					t.Fatal(err)
				}
			case "unsafe permissions":
				if err := os.Chmod(dataHome, 0777); err != nil {
					t.Fatal(err)
				}
			}
			if code, raw := moveRequest(t, ts, "move", request); code != 409 {
				t.Fatal("changed destination accepted", code, string(raw))
			}
			if _, err := os.Stat(s.projectDir); err != nil {
				t.Fatal(err)
			}
			if change == "occupied" {
				mustBytes(t, filepath.Join(destination, "keep"), "untouched")
			}
		})
	}
}

func TestDefaultProjectMoveCustomParentStillRequired(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	s, ts, _ := assistantServer(t)
	destination := filepath.Join(t.TempDir(), "missing", "custom")
	if code, raw := moveRequest(t, ts, "preview", projectMoveRequest{Destination: destination}); code != 409 {
		t.Fatal(code, string(raw))
	}
	if _, err := os.Stat(filepath.Dir(destination)); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.projectDir); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultProjectMoveParentRaceAfterSnapshot(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	s, _, _ := assistantServer(t)
	preview, err := s.moveSnapshot(s.defaultProjectDestination())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(preview.Destination), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = prepareProjectDestination(preview); err == nil {
		t.Fatal("raced parent reused")
	}
	if _, err = os.Stat(s.projectDir); err != nil {
		t.Fatal(err)
	}
}

func TestChatDataDefaultUsesHistoryAndPreservesSavedPointer(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	application := filepath.Join(dataHome, deskDirName)
	if err := os.MkdirAll(filepath.Join(application, "releases"), 0700); err != nil {
		t.Fatal(err)
	}
	privateFixture(t, filepath.Join(application, "releases", "keep"), "installed release")
	// Exercise production path resolution instead of startDesk's config override.
	ts := httptest.NewUnstartedServer(nil)
	s, err := New(Config{ProjectDir: t.TempDir(), Token: testToken, Port: ts.Listener.Addr().(*net.TCPAddr).Port})
	if err != nil {
		ts.Close()
		t.Fatal(err)
	}
	ts.Config.Handler = s
	ts.Start()
	defer s.Close()
	defer ts.Close()
	// An existing legacy pointer remains authoritative after the default changes.
	location, _ := json.Marshal(dataLocation{Version: 1, Path: s.configDir})
	privateFixture(t, filepath.Join(s.configDir, dataLocationName), string(location))
	privateFixture(t, filepath.Join(s.configDir, s.conversationName()), storageChat)
	status := storageGet(t, ts)
	if status.Path != s.configDir || status.RecommendedPath != filepath.Join(application, "history") {
		t.Fatal(status)
	}
	code, moved, raw := storageRequest(t, ts, "/api/storage/move", moveChatData{Path: status.RecommendedPath, Revision: status.Revision})
	if code != 200 || moved.Path != status.RecommendedPath {
		t.Fatal(code, raw)
	}
	mustBytes(t, filepath.Join(application, "releases", "keep"), "installed release")
	if code, reply := chatRequest(t, ts, "GET", "", "", true, ""); code != 200 || string(reply.Content) != storageChat {
		t.Fatal(code, reply)
	}
	// A fresh installation also defaults to the dedicated child path.
	fresh := &Server{projectDir: t.TempDir()}
	if fresh.defaultChatDataPath() != filepath.Join(application, "history") {
		t.Fatal(fresh.defaultChatDataPath())
	}
}
