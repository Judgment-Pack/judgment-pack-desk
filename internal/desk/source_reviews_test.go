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

func sourceReviewsRequest(t *testing.T, ts *httptest.Server, method, digest, body string, auth bool) (int, conversationReply) {
	t.Helper()
	req, _ := http.NewRequest(method, ts.URL+"/api/source-reviews", strings.NewReader(body))
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
func TestSourceReviewsIndependentCustodyAndBackup(t *testing.T) {
	s, ts, _ := assistantServer(t)
	if code, _ := sourceReviewsRequest(t, ts, "GET", "", "", false); code != 401 {
		t.Fatalf("unauthenticated read: %d", code)
	}
	code, initial := sourceReviewsRequest(t, ts, "GET", "", "", true)
	if code != 200 || initial.SHA256 != "absent" {
		t.Fatalf("initial: %d", code)
	}
	data := `{"version":1,"reviews":[{"id":"11111111-1111-1111-1111-111111111111","name":"Policy","before":{"id":"22222222-2222-2222-2222-222222222222","digest":"sha256:` + strings.Repeat("a", 64) + `","pages":[1],"allowPartial":false},"after":{"id":"33333333-3333-3333-3333-333333333333","digest":"sha256:` + strings.Repeat("b", 64) + `","pages":[1],"allowPartial":false},"checkedAt":"2026-09-24T12:00:00Z"}]}`
	if code, _ := sourceReviewsRequest(t, ts, "PUT", "", data, true); code != 400 {
		t.Fatalf("missing precondition: %d", code)
	}
	code, saved := sourceReviewsRequest(t, ts, "PUT", "absent", data, true)
	if code != 200 || saved.SHA256 != digestOf([]byte(data)) {
		t.Fatalf("save: %d", code)
	}
	if code, _ := sourceReviewsRequest(t, ts, "PUT", "absent", data, true); code != 409 {
		t.Fatalf("stale: %d", code)
	}
	if code, _ := sourceReviewsRequest(t, ts, "PUT", saved.SHA256, `{"version":1,"chats":[]}`, true); code != 400 {
		t.Fatalf("wrong document: %d", code)
	}
	if code, _ := chatRequest(t, ts, "PUT", "absent", `{"version":1,"chats":[]}`, true, ""); code != 200 {
		t.Fatal(code)
	}
	_, remaining := sourceReviewsRequest(t, ts, "GET", "", "", true)
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
	code, reply := sourceReviewsRequest(t, rts, "GET", "", "", true)
	if code != 200 || string(reply.Content) != data {
		t.Fatal("tests not restored", code)
	}
	info, err := os.Stat(filepath.Join(target, strings.Replace(s.conversationName(), "conversations-", "source-reviews-", 1)))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("test custody", err)
	}
}
