package desk

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func briefRequest(t *testing.T, ts *httptest.Server, method string, c any, auth bool) (int, conversationReply) {
	t.Helper()
	b, _ := json.Marshal(c)
	req, _ := http.NewRequest(method, ts.URL+"/api/briefs", strings.NewReader(string(b)))
	if auth {
		bearer(req)
	}
	resp, e := ts.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	var reply conversationReply
	if resp.StatusCode == 200 {
		if e = json.NewDecoder(resp.Body).Decode(&reply); e != nil {
			t.Fatal(e)
		}
	}
	return resp.StatusCode, reply
}
func TestCaseBriefCustodyAndBackup(t *testing.T) {
	s, ts, _ := assistantServer(t)
	if code, _ := briefRequest(t, ts, "GET", nil, false); code != 401 {
		t.Fatal(code)
	}
	code, initial := briefRequest(t, ts, "GET", nil, true)
	if code != 200 || initial.SHA256 != "absent" {
		t.Fatal(code)
	}
	cases := `{"version":1,"suites":{"pack-one":{"revision":1,"cases":[{"id":"one","name":"Sample","revision":1,"row":{"facts":{}}}],"runs":[],"recovered":[]}}}`
	if code, _ = testsRequest(t, ts, "PUT", "absent", cases, true); code != 200 {
		t.Fatal(code)
	}
	c := BriefCommand{Action: "begin", Subject: "case:pack-one:one", Owner: "pack-one", CaseID: "one", Model: "fixture", Snapshot: json.RawMessage(`{"kind":"case","pack":"{}","record":{"case":{"id":"one","name":"Sample","revision":1,"row":{"facts":{}}}}}`)}
	code, begun := briefRequest(t, ts, "POST", c, true)
	if code != 200 {
		t.Fatal(code)
	}
	if code, _ = briefRequest(t, ts, "POST", c, true); code != 409 {
		t.Fatal("concurrent begin", code)
	}
	var d BriefStore
	_ = json.Unmarshal(begun.Content, &d)
	c.Action = "complete"
	c.Token = d.Subjects[c.Subject].Pending.Token
	c.Text = BriefText{"Context.", "Findings.", "Unknown.", "Review."}
	code, saved := briefRequest(t, ts, "POST", c, true)
	if code != 200 {
		t.Fatal(code)
	}
	code, retry := briefRequest(t, ts, "POST", c, true)
	if code != 200 || retry.SHA256 != saved.SHA256 {
		t.Fatal("retry changed revision")
	}
	if code, _ = briefRequest(t, ts, "PUT", d, true); code != 405 && code != 404 {
		t.Fatal("history is directly writable", code)
	}
	c.Action = "begin"
	c.ExpectedRevision = 1
	c.Snapshot = json.RawMessage(`{"kind":"case","pack":"{}","record":{"case":{"id":"one","revision":2}}}`)
	if code, _ = briefRequest(t, ts, "POST", c, true); code != 409 {
		t.Fatal("unsaved case accepted", code)
	}
	if before := storageGet(t, ts); before.ProjectBytes < int64(len(saved.Content)) {
		t.Fatal("brief omitted from storage count")
	}
	archive := backupBytes(t, ts)
	cfg := s.cfg
	cfg.DeskConfigDir = t.TempDir()
	restored, rts := startDesk(t, cfg)
	defer restored.Close()
	defer rts.Close()
	empty := storageGet(t, rts)
	if code, b := restoreBackup(t, rts, moveChatData{Path: filepath.Join(t.TempDir(), "restored"), Revision: empty.Revision}, archive); code != 200 {
		t.Fatal(code, b)
	}
	code, reply := briefRequest(t, rts, "GET", nil, true)
	if code != 200 || reply.SHA256 != saved.SHA256 {
		t.Fatal("brief lost on restore", code)
	}
}
