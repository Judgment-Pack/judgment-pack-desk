package desk

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func chatRequest(t *testing.T, ts *httptest.Server, method, digest, body string, auth bool, origin string) (int, conversationReply) {
	t.Helper()
	req, _ := http.NewRequest(method, ts.URL+"/api/conversations", strings.NewReader(body))
	if auth {
		bearer(req)
	}
	if digest != "" {
		req.Header.Set("If-Match", digest)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	response, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var reply conversationReply
	data, _ := io.ReadAll(response.Body)
	if response.StatusCode == 200 {
		if err := json.Unmarshal(data, &reply); err != nil {
			t.Fatal(err)
		}
	}
	return response.StatusCode, reply
}
func TestConversationHistoryCustodyAndCompare(t *testing.T) {
	s, ts, logged := assistantServer(t)
	status, initial := chatRequest(t, ts, "GET", "", "", true, "")
	if status != 200 || initial.SHA256 != "absent" || initial.Project != s.projectDir {
		t.Fatalf("bad initial reply: %d %+v", status, initial)
	}
	data := `{"version":1,"chats":[{"id":"chat-one","composer":"private transcript"}]}`
	status, landed := chatRequest(t, ts, "PUT", initial.SHA256, data, true, "")
	if status != 200 || landed.SHA256 != digestOf([]byte(data)) || string(landed.Content) != data {
		t.Fatalf("write: %d %+v", status, landed)
	}
	info, err := os.Stat(filepath.Join(s.defaultChatDataPath(), s.conversationName()))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	status, read := chatRequest(t, ts, "GET", "", "", true, "")
	if status != 200 || !bytes.Equal(read.Content, landed.Content) {
		t.Fatalf("read: %d %+v", status, read)
	}
	status, _ = chatRequest(t, ts, "PUT", "absent", `{"version":1,"chats":[]}`, true, "")
	if status != http.StatusConflict {
		t.Fatalf("stale write accepted: %d", status)
	}
	status, _ = chatRequest(t, ts, "PUT", "", data, true, "")
	if status != http.StatusBadRequest {
		t.Fatalf("missing precondition: %d", status)
	}
	status, _ = chatRequest(t, ts, "PUT", landed.SHA256, `{"version":1,"chats":[]}`, true, "")
	if status != 200 {
		t.Fatal(status)
	}
	if strings.Contains(logged.String(), "private transcript") {
		t.Fatal("transcript leaked to log")
	}
	if _, err = os.Stat(filepath.Join(s.projectDir, s.conversationName())); !os.IsNotExist(err) {
		t.Fatal("history appeared in project")
	}
}
func TestConversationHistoryGuardsAndShape(t *testing.T) {
	_, ts, _ := assistantServer(t)
	for _, method := range []string{"GET", "PUT"} {
		status, _ := chatRequest(t, ts, method, "absent", `{"version":1,"chats":[]}`, false, "")
		if status != 401 {
			t.Fatalf("%s unauthorized: %d", method, status)
		}
		status, _ = chatRequest(t, ts, method, "absent", `{"version":1,"chats":[]}`, true, "https://foreign.example")
		if status != 403 {
			t.Fatalf("%s origin: %d", method, status)
		}
	}
	for _, body := range []string{`null`, `[]`, `{"version":2,"chats":[]}`, `{"version":1}`, `{"version":1,"chats":[]} {}`, `{"version":1,"chats":["` + string([]byte{0xff}) + `"]}`} {
		status, _ := chatRequest(t, ts, "PUT", "absent", body, true, "")
		if status != 400 {
			t.Fatalf("invalid shape: %d", status)
		}
	}
	status, _ := chatRequest(t, ts, "PUT", "absent", strings.Repeat(" ", maxConversationBytes+1), true, "")
	if status != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize accepted: %d", status)
	}
}
func TestConversationHistoryProjectIsolation(t *testing.T) {
	shared := t.TempDir()
	first, a, _ := assistantServerIn(t, shared)
	second, b, _ := assistantServerIn(t, shared)
	if first.conversationName() == second.conversationName() {
		t.Fatal("different roots shared a record")
	}
	status, _ := chatRequest(t, a, "PUT", "absent", `{"version":1,"chats":[{"id":"only-first"}]}`, true, "")
	if status != 200 {
		t.Fatal(status)
	}
	status, reply := chatRequest(t, b, "GET", "", "", true, "")
	if status != 200 || reply.SHA256 != "absent" {
		t.Fatal("history crossed project roots")
	}
}
func TestConversationHistoryRejectsUnsafeFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions and links")
	}
	for _, kind := range []string{"symlink", "public", "directory"} {
		t.Run(kind, func(t *testing.T) {
			s, ts, _ := assistantServer(t)
			name := filepath.Join(s.configDir, s.conversationName())
			switch kind {
			case "symlink":
				target := filepath.Join(t.TempDir(), "target")
				if err := os.WriteFile(target, []byte(`{"version":1,"chats":[]}`), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, name); err != nil {
					t.Fatal(err)
				}
			case "public":
				if err := os.WriteFile(name, []byte(`{"version":1,"chats":[]}`), 0644); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(name, 0700); err != nil {
					t.Fatal(err)
				}
			}
			for _, method := range []string{"GET", "PUT"} {
				status, _ := chatRequest(t, ts, method, "absent", `{"version":1,"chats":[]}`, true, "")
				if status != 403 {
					t.Fatalf("unsafe %s via %s accepted: %d", kind, method, status)
				}
			}
		})
	}
}

func TestConversationReplyDoesNotExpandHTMLCharacters(t *testing.T) {
	body := `{"version":1,"chats":[{"composer":"` + strings.Repeat("<&>", 10000) + `"}]}`
	w := httptest.NewRecorder()
	writeConversationReply(w, conversationReply{Project: "/project", SHA256: "digest", Content: json.RawMessage(body)})
	if w.Body.Len() > len(body)+256 {
		t.Fatalf("response expanded: %d bytes", w.Body.Len())
	}
	var reply conversationReply
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reply.Content, []byte(body)) {
		t.Fatal("checkpoint changed during transport")
	}
}
