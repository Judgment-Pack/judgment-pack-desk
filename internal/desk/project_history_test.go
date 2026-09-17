package desk

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func historyRequest(t *testing.T, ts *httptest.Server, action string, request projectHistoryRequest) (int, projectHistoryPreview, string) {
	t.Helper()
	data, _ := json.Marshal(request)
	req, _ := http.NewRequest("POST", ts.URL+"/api/storage/project-history/"+action, bytes.NewReader(data))
	bearer(req)
	response, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ = io.ReadAll(response.Body)
	var preview projectHistoryPreview
	if response.StatusCode == 200 {
		if err = json.Unmarshal(data, &preview); err != nil {
			t.Fatal(err)
		}
	}
	return response.StatusCode, preview, string(data)
}
func TestProjectHistoryRelocationSurvivesMoveAndBackup(t *testing.T) {
	config := t.TempDir()
	old, ots, _ := assistantServerIn(t, config)
	moved, mts, _ := assistantServerIn(t, config)
	if code, _ := chatRequest(t, ots, "PUT", "absent", storageChat, true, ""); code != 200 {
		t.Fatal(code)
	}
	request := projectHistoryRequest{PreviousProject: old.projectDir}
	code, preview, body := historyRequest(t, mts, "preview", request)
	if code != 200 || preview.ChatCount != 1 {
		t.Fatal(code, body)
	}
	if code, reply := chatRequest(t, mts, "GET", "", "", true, ""); code != 200 || reply.SHA256 != "absent" {
		t.Fatal("preview modified project association", code)
	}
	request.SourceRevision = preview.SourceRevision
	request.BindingsRevision = preview.BindingsRevision
	if code, _, body = historyRequest(t, mts, "relink", request); code != 200 {
		t.Fatal(code, body)
	}
	if code, reply := chatRequest(t, mts, "GET", "", "", true, ""); code != 200 || string(reply.Content) != storageChat || reply.Project != moved.projectDir {
		t.Fatal("linked history did not open", code)
	}
	next := `{"version":1,"chats":[{"id":"newer"}]}`
	if code, _ := chatRequest(t, mts, "PUT", preview.SourceRevision, next, true, ""); code != 200 {
		t.Fatal(code)
	}
	if code, reply := chatRequest(t, ots, "GET", "", "", true, ""); code != 200 || string(reply.Content) != next {
		t.Fatal("old and new locations did not share linked history", code)
	}
	before := storageGet(t, mts)
	target := filepath.Join(t.TempDir(), "moved-data")
	if code, _, body := storageRequest(t, mts, "/api/storage/move", moveChatData{Path: target, Revision: before.Revision}); code != 200 {
		t.Fatal(code, body)
	}
	if code, reply := chatRequest(t, mts, "GET", "", "", true, ""); code != 200 || string(reply.Content) != next {
		t.Fatal("data move lost binding", code)
	}
	backup := backupBytes(t, mts)
	cfg := moved.cfg
	cfg.DeskConfigDir = t.TempDir()
	clean, cts := startDesk(t, cfg)
	defer clean.Close()
	defer cts.Close()
	empty := storageGet(t, cts)
	if code, body := restoreBackup(t, cts, moveChatData{Path: filepath.Join(t.TempDir(), "restored"), Revision: empty.Revision}, backup); code != 200 {
		t.Fatal(code, body)
	}
	if code, reply := chatRequest(t, cts, "GET", "", "", true, ""); code != 200 || string(reply.Content) != next {
		t.Fatal("backup restore lost binding", code)
	}
}
func TestProjectHistoryNeverMergesOrUsesStalePreview(t *testing.T) {
	config := t.TempDir()
	old, ots, _ := assistantServerIn(t, config)
	current, cts, _ := assistantServerIn(t, config)
	chatRequest(t, ots, "PUT", "absent", storageChat, true, "")
	request := projectHistoryRequest{PreviousProject: old.projectDir}
	code, preview, body := historyRequest(t, cts, "preview", request)
	if code != 200 {
		t.Fatal(code, body)
	}
	request.SourceRevision = preview.SourceRevision
	request.BindingsRevision = preview.BindingsRevision
	chatRequest(t, ots, "PUT", preview.SourceRevision, `{"version":1,"chats":[]}`, true, "")
	if code, _, _ := historyRequest(t, cts, "relink", request); code != 409 {
		t.Fatal("stale preview accepted", code)
	}
	chatRequest(t, cts, "PUT", "absent", storageChat, true, "")
	if code, _, _ := historyRequest(t, cts, "preview", projectHistoryRequest{PreviousProject: old.projectDir}); code != 409 {
		t.Fatal("existing history allowed a merge", code)
	}
	location := storageGet(t, cts)
	mustBytes(t, filepath.Join(location.Path, current.conversationName()), storageChat)
}
func TestMissingLinkedHistoryCannotBecomeEmpty(t *testing.T) {
	s, ts, _ := assistantServer(t)
	where := storageGet(t, ts)
	bindings := projectBindings{Version: 1, Projects: map[string]string{s.projectDir: "conversations-" + digestOf([]byte("gone")) + ".json"}}
	data, _ := json.Marshal(bindings)
	privateFixture(t, filepath.Join(where.Path, projectBindingsName), string(data))
	for _, method := range []string{"GET", "PUT"} {
		if code, _ := chatRequest(t, ts, method, "absent", storageChat, true, ""); code == 200 {
			t.Fatal("missing linked record accepted", method)
		}
	}
	if _, err := os.Stat(filepath.Join(where.Path, s.conversationName())); !os.IsNotExist(err) {
		t.Fatal("missing link recreated history")
	}
}

func TestBrokenProjectBindingStillAllowsRestore(t *testing.T) {
	_, ts, _ := assistantServer(t)
	chatRequest(t, ts, "PUT", "absent", storageChat, true, "")
	backup := backupBytes(t, ts)
	before := storageGet(t, ts)
	privateFixture(t, filepath.Join(before.Path, projectBindingsName), `{"version":9,"projects":{}}`)
	broken := storageGet(t, ts)
	if broken.Problem == "" {
		t.Fatal("broken binding not reported")
	}
	code, body := restoreBackup(t, ts, moveChatData{Path: filepath.Join(t.TempDir(), "restored"), Revision: broken.Revision}, backup)
	if code != 200 {
		t.Fatal("broken binding prevented recovery", code, body)
	}
	if code, reply := chatRequest(t, ts, "GET", "", "", true, ""); code != 200 || string(reply.Content) != storageChat {
		t.Fatal("recovery failed", code)
	}
}
