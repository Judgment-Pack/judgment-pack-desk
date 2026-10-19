package desk

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func definitionReply(t *testing.T, ts *httptest.Server, subject string) conversationReply {
	t.Helper()
	req, _ := http.NewRequest("GET", ts.URL+"/api/briefs?subject="+url.QueryEscape(subject), nil)
	pageBearer(privateWorkspaceTestSession(t, ts))(req)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	var reply conversationReply
	if err = json.NewDecoder(resp.Body).Decode(&reply); err != nil {
		t.Fatal(err)
	}
	return reply
}
func beginDefinition(t *testing.T, ts *httptest.Server, subject string, snapshot json.RawMessage) BriefCommand {
	t.Helper()
	command := BriefCommand{Action: "begin", Subject: subject, Snapshot: snapshot, Model: "fixture"}
	code, reply := briefRequest(t, ts, "POST", command, true)
	if code != 200 {
		t.Fatal("begin", code)
	}
	var store BriefStore
	_ = json.Unmarshal(reply.Content, &store)
	command.Token = store.Subjects[subject].Pending.Token
	return command
}
func TestDefinitionBriefCustodyAndStaleInputs(t *testing.T) {
	s, ts, _ := assistantServer(t)
	writeProjectFile(t, s.projectDir, "jpack.json", `{"packs":{"one":{"path":"one.pack.json"}},"graphs":{"flow":{"path":"flow.graph.json"}}}`)
	writeProjectFile(t, s.projectDir, "one.pack.json", `{"title":"One","version":"1","evidenceRequirements":[{"id":"policy","required":true}]}`)
	writeProjectFile(t, s.projectDir, "flow.graph.json", `{"nodes":{"first":{"pack":"one"}},"edges":[],"result":"first"}`)
	initial := definitionReply(t, ts, "graph:flow")
	if len(initial.Snapshot) == 0 || initial.SnapshotIssue != "" {
		t.Fatal(initial.SnapshotIssue)
	}
	var frozen definitionBrief
	_ = json.Unmarshal(initial.Snapshot, &frozen)
	if frozen.Kind != "graph" || !strings.Contains(string(initial.Snapshot), "policy") {
		t.Fatal("missing graph dependency")
	}
	command := beginDefinition(t, ts, "graph:flow", initial.Snapshot)
	command.Action = "complete"
	command.Text = BriefText{"Context", "Findings", "Unknown", "Review"}
	if code, _ := briefRequest(t, ts, "POST", command, true); code != 200 {
		t.Fatal(code)
	}
	reopened := definitionReply(t, ts, "graph:flow")
	var store BriefStore
	_ = json.Unmarshal(reopened.Content, &store)
	if len(store.Subjects["graph:flow"].Revisions) != 1 || !sameBriefJSON(reopened.Snapshot, initial.Snapshot) {
		t.Fatal("brief did not survive reopen")
	}
	writeProjectFile(t, s.projectDir, "one.pack.json", `{"title":"One","version":"2"}`)
	changed := definitionReply(t, ts, "graph:flow")
	if sameBriefJSON(changed.Snapshot, initial.Snapshot) {
		t.Fatal("dependency change did not change basis")
	}
	command.Action = "begin"
	command.ExpectedRevision = 1
	if code, _ := briefRequest(t, ts, "POST", command, true); code != 409 {
		t.Fatal("stale snapshot accepted", code)
	}
	command.Snapshot = changed.Snapshot
	if code, _ := briefRequest(t, ts, "POST", command, true); code != 200 {
		t.Fatal("updated basis refused", code)
	}
	// Missing source never erases saved history or turns the old summary current.
	writeProjectFile(t, s.projectDir, "jpack.json", `{"packs":{},"graphs":{}}`)
	unavailable := definitionReply(t, ts, "graph:flow")
	if unavailable.SnapshotIssue == "" || len(unavailable.Snapshot) > 0 {
		t.Fatal("missing declaration accepted")
	}
	_ = json.Unmarshal(unavailable.Content, &store)
	if len(store.Subjects["graph:flow"].Revisions) != 1 {
		t.Fatal("lost history")
	}
}
func TestDefinitionBriefRejectsForgedTestsAndUnsafePaths(t *testing.T) {
	s, ts, _ := assistantServer(t)
	outside := t.TempDir()
	writeProjectFile(t, outside, "outside.json", `{"private":"outside project"}`)
	relative, err := filepath.Rel(s.projectDir, filepath.Join(outside, "outside.json"))
	if err != nil {
		t.Fatal(err)
	}
	config, _ := json.Marshal(map[string]any{"packs": map[string]any{"one": map[string]string{"path": "one.pack.json"}, "outside": map[string]string{"path": relative}}})
	writeProjectFile(t, s.projectDir, "jpack.json", string(config))
	pack := `{"title":"One","version":"1"}`
	writeProjectFile(t, s.projectDir, "one.pack.json", pack)
	cases := `[{"id":"one","name":"Saved","row":{"facts":{}}}]`
	tests := `{"version":1,"suites":{"one":{"revision":1,"cases":` + cases + `,"runs":[{"id":"old","packText":"{}","cases":` + cases + `,"report":{"status":"passed"}}]},"unrelated":{"cases":[{"private":"secret"}]}}}`
	if code, _ := testsRequest(t, ts, "PUT", "absent", tests, true); code != 200 {
		t.Fatal(code)
	}
	before := definitionReply(t, ts, "pack:one")
	if strings.Contains(string(before.Snapshot), "secret") || !strings.Contains(string(before.Snapshot), `"matchesDefinition":false`) {
		t.Fatal(string(before.Snapshot))
	}
	forged := json.RawMessage(strings.Replace(string(before.Snapshot), `"matchesDefinition":false`, `"matchesDefinition":true`, 1))
	if code, _ := briefRequest(t, ts, "POST", BriefCommand{Action: "begin", Subject: "pack:one", Snapshot: forged, Model: "fixture"}, true); code != 409 {
		t.Fatal("forged status accepted", code)
	}
	if got := definitionReply(t, ts, "pack:outside"); got.SnapshotIssue == "" {
		t.Fatal("outside path accepted")
	}
	if got := definitionReply(t, ts, "pack:one"); !sameBriefJSON(got.Snapshot, before.Snapshot) {
		t.Fatal("read mutated tests or definition")
	}
	writeProjectFile(t, s.projectDir, "one.pack.json", `null`)
	if got := definitionReply(t, ts, "pack:one"); got.SnapshotIssue == "" {
		t.Fatal("non-object definition accepted")
	}
}
func TestDefinitionDraftBriefsAreSeparateAndUseSavedBytes(t *testing.T) {
	s, ts, _ := assistantServer(t)
	writeProjectFile(t, s.projectDir, "jpack.json", `{"packs":{}}`)
	drafts := `{"version":1,"drafts":[{"id":"draft-one","title":"Draft one","checkpoint":{"state":{"candidates":[{"text":"{\"title\":\"First\"}","revision":1},{"text":"{\"title\":\"Latest\"}","revision":2}],"unknowns":["Unresolved"],"cases":[]}}}],"deleted":[]}`
	if code, _ := draftRequest(t, ts, "PUT", "absent", drafts, true); code != 200 {
		t.Fatal(code)
	}
	pack := definitionReply(t, ts, "pack-draft:draft-one")
	if pack.SnapshotIssue != "" || !strings.Contains(string(pack.Snapshot), "Latest") || strings.Contains(string(pack.Snapshot), "First") {
		t.Fatal(string(pack.Snapshot), pack.SnapshotIssue)
	}
	beginDefinition(t, ts, "pack-draft:draft-one", pack.Snapshot)
	chats := `{"version":1,"chats":[{"id":"chat","graphDrafts":[{"draftId":"draft-one","id":"graph-one","path":"flow.graph.json","createdAt":"2026-10-18T12:00:00Z","content":"{\"nodes\":{},\"edges\":[]}"}],"checkpoint":{"private":"do not include"}}]}`
	var conversation map[string]any
	if err := json.Unmarshal([]byte(chats), &conversation); err != nil {
		t.Fatal(err)
	}
	if w := policyWorkspaceCall(s, privateWorkspaceTestSession(t, ts), "PUT", "/api/conversations", "absent", conversation); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	graph := definitionReply(t, ts, "graph-draft:draft-one")
	if graph.SnapshotIssue != "" || strings.Contains(string(graph.Snapshot), "do not include") {
		t.Fatal(graph.SnapshotIssue)
	}
	beginDefinition(t, ts, "graph-draft:draft-one", graph.Snapshot)
	missing := definitionReply(t, ts, "pack-draft:deleted")
	if missing.SnapshotIssue == "" {
		t.Fatal("missing draft accepted")
	}
}
