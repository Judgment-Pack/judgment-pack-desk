package desk

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func deskCall(t *testing.T, ts *httptest.Server, method, path, id, body string, auth bool) (int, []byte) {
	t.Helper()
	r, err := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if auth {
		r.Header.Set("Authorization", "Bearer "+testToken)
	}
	if id != "" {
		r.Header.Set("X-Jpack-Desk", id)
	}
	r.Header.Set("Content-Type", "application/json")
	response, err := ts.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, data
}
func createTestDesk(t *testing.T, ts *httptest.Server, name string) deskRecord {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"name": name})
	status, data := deskCall(t, ts, "POST", "/api/desks", "", string(body), true)
	var row deskRecord
	if status != 201 || json.Unmarshal(data, &row) != nil {
		t.Fatalf("create: %d %s", status, data)
	}
	return row
}
func TestNamedDesksKeepFilesAndArtifactsSeparate(t *testing.T) {
	s, ts, _ := assistantServer(t)
	a := createTestDesk(t, ts, "Policy review")
	b := createTestDesk(t, ts, "Vendor review")
	if a.ID == b.ID || !a.Managed || a.Name != "Policy review" {
		t.Fatal(a, b)
	}
	for _, row := range []deskRecord{a, b} {
		status, data := deskCall(t, ts, "GET", "/api/files", row.ID, "", true)
		if status != 200 || !bytes.Contains(data, []byte(row.Folder)) || bytes.Contains(data, []byte(".desk-private"+string(filepath.Separator)+"desk.json")) {
			t.Fatalf("files: %d %s", status, data)
		}
		status, data = deskCall(t, ts, "GET", "/api/file?path=.desk-private%2Fdesk.json", row.ID, "", true)
		if status == 200 {
			t.Fatal("private metadata exposed", string(data))
		}
	}
	status, data := deskCall(t, ts, "PUT", "/api/file", a.ID, `{"path":"packs/a.json","content":"{}","baseSha256":"","createParents":true}`, true)
	if status != 200 && status != 201 {
		t.Fatalf("write %d %s", status, data)
	}
	status, _ = deskCall(t, ts, "GET", "/api/file?path=packs%2Fa.json", b.ID, "", true)
	if status != 404 {
		t.Fatalf("other desk sees pack: %d", status)
	}
	child := s.desks[a.ID]
	store, err := child.openChatData()
	if err != nil {
		t.Fatal(err)
	}
	if !pathContains(a.Folder, store.location.Path) {
		t.Fatal("external chat data", store.location.Path)
	}
	if err = writePrivateData(store.root, child.conversationName(), []byte(`{"version":1,"chats":[]}`)); err != nil {
		t.Fatal(err)
	}
	store.root.Close()
	if _, err = os.Stat(filepath.Join(a.Folder, ".desk-private", "data", child.conversationName())); err != nil {
		t.Fatal(err)
	}
	child.cfg.RunnerBin = "/test/runner"
	child.initJobs()
	if child.jobs.dir != filepath.Join(a.Folder, ".desk-private", "jobs") || child.jobs.workspace != a.ID {
		t.Fatal("wrong runner scope", child.jobs)
	}
	status, data = deskCall(t, ts, "GET", "/api/desks", a.ID, "", true)
	var directory struct {
		Current deskRecord
		Desks   []deskRecord
	}
	_ = json.Unmarshal(data, &directory)
	if status != 200 || directory.Current.ID != a.ID || len(directory.Desks) != 3 {
		t.Fatalf("directory: %d %s", status, data)
	}
	status, _ = deskCall(t, ts, "POST", "/api/storage/move", a.ID, `{"path":"/tmp/outside","revision":"test"}`, true)
	if status != 400 {
		t.Fatalf("managed data should stay in desk: %d", status)
	}
}
func TestNamedDeskGuardsAndRestart(t *testing.T) {
	s, ts, _ := assistantServer(t)
	for _, body := range []string{`{"name":""}`, `{"name":" x "}`, `{"name":"x","path":"/tmp/escape"}`, `{"name":"bad\nname"}`} {
		status, _ := deskCall(t, ts, "POST", "/api/desks", "", body, true)
		if status != 400 {
			t.Fatalf("accepted invalid create: %d %s", status, body)
		}
	}
	status, _ := deskCall(t, ts, "POST", "/api/desks", "", `{"name":"Secret"}`, false)
	if status != 401 {
		t.Fatal(status)
	}
	row := createTestDesk(t, ts, "Research")
	for _, id := range []string{"../outside", strings.Repeat("a", 32)} {
		status, _ = deskCall(t, ts, "GET", "/api/files", id, "", true)
		if status != 400 && status != 404 {
			t.Fatal("fell back to default desk", status)
		}
	}
	status, _ = deskCall(t, ts, "GET", "/api/files", row.ID, "", false)
	if status != 401 {
		t.Fatal(status)
	}
	cfg := s.cfg
	cfg.Root = nil
	cfg.ProjectDir = s.projectDir
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if len(restarted.desks) != 1 || restarted.desks[row.ID] == nil {
		t.Fatal("saved desk not resumed")
	}
	if got, err := restarted.desks[row.ID].deskRecord(); err != nil || got.Name != row.Name {
		t.Fatal(got, err)
	}
}
func TestNamedDeskRegistryRejectsSymlinks(t *testing.T) {
	s, ts, _ := assistantServer(t)
	id := strings.Repeat("b", 32)
	root, err := s.desksRoot()
	if err != nil {
		t.Fatal(err)
	}
	root.Close()
	if err = os.Symlink(t.TempDir(), filepath.Join(s.configDir, "desks", id)); err != nil {
		t.Skip(err)
	}
	status, _ := deskCall(t, ts, "GET", "/api/files", id, "", true)
	if status != 404 {
		t.Fatal("symlink opened", status)
	}
}

func TestNamedDeskMovesWithItsConversationData(t *testing.T) {
	s, ts, _ := assistantServer(t)
	row := createTestDesk(t, ts, "Portable")
	child := s.desks[row.ID]
	store, err := child.openChatData()
	if err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"version":1,"chats":[{"id":"preserved"}]}`)
	if err = writePrivateData(store.root, child.conversationName(), original); err != nil {
		t.Fatal(err)
	}
	store.root.Close()
	cfg := s.cfg
	cfg.Root = nil
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(t.TempDir(), "renamed-desk")
	if err = os.Rename(row.Folder, moved); err != nil {
		t.Fatal(err)
	}
	cfg.ProjectDir = moved
	opened, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	if opened.cfg.deskID != row.ID {
		t.Fatal("direct launch did not adopt desk identity")
	}
	current, err := opened.deskRecord()
	if err != nil || current.Name != "Portable" || current.ID != "" {
		t.Fatal(current, err)
	}
	store, err = opened.openChatData()
	if err != nil {
		t.Fatal(err)
	}
	defer store.root.Close()
	data, err := readPrivateData(store.root, opened.conversationName(), 4096)
	if err != nil || !bytes.Equal(data, original) {
		t.Fatalf("history lost after move: %s %v", data, err)
	}
	if !pathContains(moved, store.location.Path) {
		t.Fatal("data escaped moved desk", store.location.Path)
	}
}
