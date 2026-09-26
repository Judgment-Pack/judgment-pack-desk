package desk

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testsRequest(t *testing.T, ts *httptest.Server, method, digest, body string, auth bool) (int, conversationReply) {
	t.Helper()
	req, _ := http.NewRequest(method, ts.URL+"/api/pack-tests", strings.NewReader(body))
	if auth {
		bearer(req)
	}
	if digest != "" {
		req.Header.Set("If-Match", digest)
	}
	response, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	var reply conversationReply
	if response.StatusCode == 200 {
		if err = json.Unmarshal(data, &reply); err != nil {
			t.Fatal(err)
		}
	}
	return response.StatusCode, reply
}
func TestPackTestsIndependentCustodyAndBackup(t *testing.T) {
	s, ts, _ := assistantServer(t)
	if code, _ := testsRequest(t, ts, "GET", "", "", false); code != 401 {
		t.Fatalf("unauthenticated read: %d", code)
	}
	code, initial := testsRequest(t, ts, "GET", "", "", true)
	if code != 200 || initial.SHA256 != "absent" {
		t.Fatalf("initial: %d", code)
	}
	data := `{"version":1,"suites":{"pack-one":{"revision":1,"cases":[],"runs":[],"recovered":[]}}}`
	if code, _ := testsRequest(t, ts, "PUT", "", data, true); code != 400 {
		t.Fatalf("missing precondition: %d", code)
	}
	code, saved := testsRequest(t, ts, "PUT", "absent", data, true)
	if code != 200 || saved.SHA256 != digestOf([]byte(data)) {
		t.Fatalf("save: %d", code)
	}
	if code, _ := testsRequest(t, ts, "PUT", "absent", data, true); code != 409 {
		t.Fatalf("stale: %d", code)
	}
	if code, _ := testsRequest(t, ts, "PUT", saved.SHA256, `{"version":1,"chats":[]}`, true); code != 400 {
		t.Fatalf("wrong document: %d", code)
	}
	if code, _ := chatRequest(t, ts, "PUT", "absent", `{"version":1,"chats":[]}`, true, ""); code != 200 {
		t.Fatal(code)
	}
	_, remaining := testsRequest(t, ts, "GET", "", "", true)
	if string(remaining.Content) != data {
		t.Fatal("chat write changed tests")
	}
	before := storageGet(t, ts)
	if before.ProjectBytes < int64(len(data)) {
		t.Fatal("test bytes omitted from storage usage")
	}
	archive := backupBytes(t, ts)
	config := s.cfg
	config.DeskConfigDir = t.TempDir()
	restored, rts := startDesk(t, config)
	defer restored.Close()
	defer rts.Close()
	empty := storageGet(t, rts)
	target := filepath.Join(t.TempDir(), "restored")
	if code, body := restoreBackup(t, rts, moveChatData{Path: target, Revision: empty.Revision}, archive); code != 200 {
		t.Fatalf("restore: %d %s", code, body)
	}
	code, reply := testsRequest(t, rts, "GET", "", "", true)
	if code != 200 || string(reply.Content) != data {
		t.Fatal("tests not restored", code)
	}
	info, err := os.Stat(filepath.Join(target, strings.Replace(s.conversationName(), "conversations-", "pack-tests-", 1)))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("test custody", err)
	}
}
