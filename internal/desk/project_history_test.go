package desk

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

// **Another startup project's history is not linked in: the installation is
// sealed to its first project** (v0.14.0). Relinking history recorded under
// another project folder of the same installation, without a Desk home, needs
// a Desk started on that second folder, and the first-project seal refuses
// it at startup, in words, before any history is read, previewed or linked.
// The first project's history is untouched. These were the relink and stale
// preview tests, which started both projects' Desks on one installation.
func TestProjectHistoryOfAnotherStartupProjectIsRefusedBySeal(t *testing.T) {
	requireStorageLine(t)
	config := t.TempDir()
	old, ots, _ := assistantServerIn(t, config)
	if code, _ := chatRequest(t, ots, "PUT", "absent", storageChat, true, ""); code != 200 {
		t.Fatal(code)
	}
	saved := savedConversationBytes(t, old, ots)
	location := storageGet(t, ots)
	ots.Close()
	assistantRunMust(t, old.Close())
	moved, err := New(Config{ProjectDir: t.TempDir(), DeskConfigDir: config, JpackBin: standInRuntime(t), Token: testToken, Port: 8791, Logger: log.New(io.Discard, "", 0)})
	if err == nil {
		_ = moved.Close()
		t.Fatalf("another startup project started (read-only=%v, problem=%v)", moved.storageReadOnly, moved.storageProblem)
	}
	if !errors.Is(err, errDeskAnotherStartupProject) || !strings.Contains(err.Error(), "first started on") {
		t.Fatalf("another startup project was refused without saying why: %v", err)
	}
	mustBytes(t, filepath.Join(location.Path, old.conversationName()), saved)
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
	s, ts, _ := assistantServer(t)
	chatRequest(t, ts, "PUT", "absent", storageChat, true, "")
	storageChat := savedConversationBytes(t, s, ts)
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
