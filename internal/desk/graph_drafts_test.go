package desk

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func graphDraftFixture(origin string) graphDraft {
	return graphDraft{DraftID: "draft-one", OriginChatID: origin, ID: "flow", Path: "flow.graph.json", Content: `{"formatVersion":"1","nodes":{},"edges":[]}`, CreatedAt: "2026-10-18T12:00:00Z"}
}
func readGraphDraftFixture(t *testing.T, s *Server, token string) (conversationReply, graphDraftDocument) {
	t.Helper()
	reply := policyWorkspaceRead(t, s, token, "/api/draft-graphs")
	var doc graphDraftDocument
	if json.Unmarshal(reply.Content, &doc) != nil {
		t.Fatal("invalid graph record")
	}
	return reply, doc
}
func TestGraphDraftsKeepExactPrivateBasisAfterChatDeletion(t *testing.T) {
	s, token := policyTestServer(t)
	p := policyCreate(t, s, token)
	source := policyAuthoringRead(t, s, token, p, "revisionId="+p.Document.Candidate.RevisionID)
	initial := graphDraftFixture("chat-one")
	legacy := initial
	legacy.OriginChatID = ""
	// GET is read-only; new independent stores require a real current session.
	empty, _ := readGraphDraftFixture(t, s, token)
	if empty.SHA256 != "absent" {
		t.Fatal("GET created a graph store")
	}
	for _, method := range []string{"GET", "PUT"} {
		if w := policyWorkspaceCall(s, testToken, method, "/api/draft-graphs", "absent", graphDraftDocument{1, []graphDraft{}, []string{}}); w.Code != 401 {
			t.Fatal("launch admitted private graphs", method, w.Code, w.Body.String())
		}
	}
	chats := map[string]any{"version": 1, "chats": []any{map[string]any{"id": "chat-one", "policySources": []policyAuthoringSource{source}, "graphDrafts": []graphDraft{legacy}}}}
	policyWorkspaceSave(t, s, token, "/api/conversations", chats)
	before, doc := readGraphDraftFixture(t, s, token)
	if len(doc.Drafts) != 1 || doc.Drafts[0].OriginChatID != "chat-one" || len(doc.Drafts[0].PolicySources) != 1 || doc.Drafts[0].PolicySources[0] != source {
		t.Fatal("lost independent exact basis", string(before.Content))
	}
	doc.Drafts[0].Content = `{"formatVersion":"1","nodes":{"edited":{"pack":"saved"}},"edges":[]}`
	changed := policyWorkspaceCall(s, token, "PUT", "/api/draft-graphs", before.SHA256, doc)
	if changed.Code != 200 {
		t.Fatal(changed.Code, changed.Body.String())
	}
	// The old chat copy is history: changing unrelated chat data never overwrites
	// or blocks the independently edited graph.
	chats["chats"].([]any)[0].(map[string]any)["composer"] = "Unrelated follow-up"
	policyWorkspaceSave(t, s, token, "/api/conversations", chats)
	policyWorkspaceSave(t, s, token, "/api/conversations", map[string]any{"version": 1, "chats": []any{}})
	_, retained := readGraphDraftFixture(t, s, token)
	if !sameDraftValue(retained, doc) {
		t.Fatal("chat save/delete replaced independent graph")
	}
	// Existing retained provenance remains private and readable even if the
	// original ledger is unavailable; it is never reclassified as public data.
	if err := os.Remove(filepath.Join(s.projectDir, policyDirectory, "ledger.json")); err != nil {
		t.Fatal(err)
	}
	readGraphDraftFixture(t, s, token)
	if w := policyCall(s, testToken, "GET", "/api/storage/backup", nil); w.Code != 401 {
		t.Fatal("launch exported private graphs", w.Code)
	}
	if w := policyCall(s, token, "GET", "/api/storage/backup", nil); w.Code != 200 {
		t.Fatal("authenticated historical export", w.Code, w.Body.String())
	}
	s.sessions.revoke(token)
	if w := policyCall(s, token, "GET", "/api/draft-graphs", nil); w.Code != 401 {
		t.Fatal("revoked session read graph", w.Code)
	}
}
func TestGraphDraftsScopedIdentityCASAndImmutableSources(t *testing.T) {
	s, token := policyTestServer(t)
	p := policyCreate(t, s, token)
	source := policyAuthoringRead(t, s, token, p, "revisionId="+p.Document.Candidate.RevisionID)
	one, two := graphDraftFixture("chat-one"), graphDraftFixture("chat-two")
	one.PolicySources = []policyAuthoringSource{source}
	doc := graphDraftDocument{1, []graphDraft{one, two}, []string{}}
	saved := policyWorkspaceSave(t, s, token, "/api/draft-graphs", doc)
	if _, err := os.Stat(filepath.Join(s.projectDir, one.Path)); !os.IsNotExist(err) {
		t.Fatal("retaining a private graph wrote a project file")
	}
	bad := doc
	bad.Drafts = append([]graphDraft{}, doc.Drafts...)
	bad.Drafts[0].PolicySources = nil
	if w := policyWorkspaceCall(s, token, "PUT", "/api/draft-graphs", saved.SHA256, bad); w.Code != 403 {
		t.Fatal("basis removed", w.Code, w.Body.String())
	}
	forged := graphDraftFixture("new-chat")
	forged.PolicySources = []policyAuthoringSource{source}
	forged.PolicySources[0].Narrative = "forged"
	bad.Drafts = []graphDraft{one, two, forged}
	if w := policyWorkspaceCall(s, token, "PUT", "/api/draft-graphs", saved.SHA256, bad); w.Code != 400 {
		t.Fatal("forged source admitted", w.Code, w.Body.String())
	}
	// Equal draftId values in separate conversations have separate custody.
	doc.Drafts = []graphDraft{two}
	doc.Deleted = []string{graphDraftIdentity(one)}
	accepted := policyWorkspaceCall(s, token, "PUT", "/api/draft-graphs", saved.SHA256, doc)
	if accepted.Code != 200 {
		t.Fatal(accepted.Code, accepted.Body.String())
	}
	if w := policyWorkspaceCall(s, token, "PUT", "/api/draft-graphs", saved.SHA256, doc); w.Code != 409 {
		t.Fatal("stale CAS accepted", w.Code)
	}
	latest, _ := readGraphDraftFixture(t, s, token)
	doc.Drafts = append(doc.Drafts, one)
	if w := policyWorkspaceCall(s, token, "PUT", "/api/draft-graphs", latest.SHA256, doc); w.Code != 400 && w.Code != 409 {
		t.Fatal("deleted origin resurrected", w.Code)
	}
}
func TestGraphDraftMigrationFailureKeepsOriginalConversation(t *testing.T) {
	s, token := policyTestServer(t)
	p := policyCreate(t, s, token)
	source := policyAuthoringRead(t, s, token, p, "revisionId="+p.Document.Candidate.RevisionID)
	// Install a legacy conversation directly to exercise the first write after
	// upgrade. Reads must never migrate it behind privateQuery.
	policyWorkspaceSave(t, s, token, "/api/conversations", map[string]any{"version": 1, "chats": []any{}})
	store, err := s.openChatData()
	if err != nil {
		t.Fatal(err)
	}
	defer store.root.Close()
	legacy := graphDraftFixture("")
	raw, _ := json.Marshal(map[string]any{"version": 1, "chats": []any{map[string]any{"id": "legacy-chat", "policySources": []policyAuthoringSource{source}, "graphDrafts": []graphDraft{legacy}}}})
	file, err := store.root.OpenFile(s.conversationName(), os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write(raw)
	_ = file.Close()
	old := policyWorkspaceRead(t, s, token, "/api/conversations")
	empty, _ := readGraphDraftFixture(t, s, token)
	if empty.SHA256 != "absent" {
		t.Fatal("legacy GET migrated storage")
	}
	if err = os.Remove(filepath.Join(s.projectDir, policyDirectory, "ledger.json")); err != nil {
		t.Fatal(err)
	}
	w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", old.SHA256, map[string]any{"version": 1, "chats": []any{}})
	if w.Code == 200 {
		t.Fatal("deleted chat despite failed exact-source migration")
	}
	if kept := policyWorkspaceRead(t, s, token, "/api/conversations"); kept.SHA256 != old.SHA256 {
		t.Fatal("failed migration erased legacy chat")
	}
}
func TestGraphDraftSharedRecordParity(t *testing.T) {
	raw, err := os.ReadFile("testdata/graph-draft-records.json")
	if err != nil {
		t.Fatal(err)
	}
	var tests []struct {
		Name     string          `json:"name"`
		Valid    bool            `json:"valid"`
		Document json.RawMessage `json:"document"`
	}
	if json.Unmarshal(raw, &tests) != nil {
		t.Fatal("invalid fixture")
	}
	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			if err := validateGraphDrafts(test.Document); (err == nil) != test.Valid {
				t.Fatal("graph record parity", err)
			}
		})
	}
	if !historyRequestPath("/api/draft-graphs") || !chatStorageFile("draft-graphs-"+strings.Repeat("a", 64)+".json") {
		t.Fatal("graph store omitted from private move/backup")
	}
}

func TestGraphDraftRestoreAdmitsExactOldPublicationWithCurrentSession(t *testing.T) {
	s, token := policyTestServer(t)
	approved := policyApprove(t, s, token, policyCreate(t, s, token))
	published := policyMutation(t, policyCall(s, token, "POST", "/api/policies/"+approved.Document.ID+"/publish", policyPublishBody(t, approved))).Policy
	source := policyAuthoringRead(t, s, token, published, "publicationId="+published.Document.Publications[0].ID)
	edit := policyEditBody(t, published)
	edit.Narrative = "A newer candidate does not replace the graph's reviewed publication."
	edit.ArtifactVersion = "2.0.0"
	policyMutation(t, policyCall(s, token, "PUT", "/api/policies/"+approved.Document.ID, edit))
	draft := graphDraftFixture("chat-one")
	draft.PolicySources = []policyAuthoringSource{source}
	policyWorkspaceSave(t, s, token, "/api/draft-graphs", graphDraftDocument{1, []graphDraft{draft}, []string{}})
	backup := policyCall(s, token, "GET", "/api/storage/backup", nil)
	if backup.Code != 200 {
		t.Fatal(backup.Code, backup.Body.String())
	}
	cfg := s.cfg
	cfg.DeskConfigDir = t.TempDir()
	restored, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restored.Close() })
	current := policyTestSession(t, restored)
	if w := policyRestoreCall(t, restored, testToken, filepath.Join(t.TempDir(), "unsigned"), backup.Body.Bytes()); w.Code != 401 {
		t.Fatal("launch restored private graph", w.Code, w.Body.String())
	}
	if w := policyRestoreCall(t, restored, current, filepath.Join(t.TempDir(), "forged"), policyForgedBackup(t, backup.Body.Bytes())); w.Code != 400 {
		t.Fatal("forged restore source admitted", w.Code, w.Body.String())
	}
	if w := policyRestoreCall(t, restored, current, filepath.Join(t.TempDir(), "restored"), backup.Body.Bytes()); w.Code != 200 {
		t.Fatal("exact restore", w.Code, w.Body.String())
	}
	_, doc := readGraphDraftFixture(t, restored, current)
	if len(doc.Drafts) != 1 || doc.Drafts[0].PolicySources[0] != source {
		t.Fatal("restore substituted source head")
	}
	restored.sessions.revoke(current)
	if w := policyCall(restored, current, "GET", "/api/draft-graphs", nil); w.Code != 401 {
		t.Fatal("revoked restored session read private graph", w.Code)
	}
}

func TestGraphDraftSnapshotsRefuseNestedPrivateProvenance(t *testing.T) {
	s, token := policyTestServer(t)
	p := policyCreate(t, s, token)
	source := policyAuthoringRead(t, s, token, p, "revisionId="+p.Document.Candidate.RevisionID)
	for _, field := range []string{"policySources", "PolicySources", "originChatId", "OriginChatId"} {
		var nested any = []policyAuthoringSource{source}
		if strings.EqualFold(field, "originChatId") {
			nested = "forged-owner"
		}
		draft := map[string]any{"draftId": "draft-one", "id": "flow", "path": "flow.graph.json", "content": "{}", "createdAt": "2026-10-18T12:00:00Z", field: nested}
		body := map[string]any{"version": 1, "chats": []any{map[string]any{"id": "chat-one", "graphDrafts": []any{draft}}}}
		for _, credential := range []string{testToken, token} {
			if w := policyWorkspaceCall(s, credential, "PUT", "/api/conversations", "absent", body); w.Code != 400 {
				t.Fatal("nested provenance admitted", field, w.Code, w.Body.String())
			}
		}
	}
	if got := policyWorkspaceRead(t, s, token, "/api/conversations"); got.SHA256 != "absent" {
		t.Fatal("refused snapshot wrote private history")
	}
}

func TestGraphDraftFirstMigrationKeepsIncomingEditOrDeletedPrior(t *testing.T) {
	for _, deleting := range []bool{false, true} {
		t.Run(map[bool]string{false: "edited", true: "deleted"}[deleting], func(t *testing.T) {
			s, token := policyTestServer(t)
			policyWorkspaceSave(t, s, token, "/api/conversations", map[string]any{"version": 1, "chats": []any{}})
			prior := graphDraftFixture("")
			body := map[string]any{"version": 1, "chats": []any{map[string]any{"id": "legacy-chat", "graphDrafts": []graphDraft{prior}}}}
			raw, _ := json.Marshal(body)
			store, err := s.openChatData()
			if err != nil {
				t.Fatal(err)
			}
			defer store.root.Close()
			file, err := store.root.OpenFile(s.conversationName(), os.O_WRONLY|os.O_TRUNC, 0600)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = file.Write(raw)
			_ = file.Close()
			expected := prior.Content
			if deleting {
				body["chats"] = []any{}
			} else {
				edited := prior
				edited.Content = `{"nodes":{"new":{"pack":"saved"}},"edges":[]}`
				body["chats"].([]any)[0].(map[string]any)["graphDrafts"] = []graphDraft{edited}
				expected = edited.Content
			}
			policyWorkspaceSave(t, s, token, "/api/conversations", body)
			_, doc := readGraphDraftFixture(t, s, token)
			if len(doc.Drafts) != 1 || doc.Drafts[0].Content != expected {
				t.Fatal("first migration lost intended snapshot", deleting, doc)
			}
		})
	}
}

func TestGraphDraftPendingSaveFreezesCandidateAndReconcilesExactTarget(t *testing.T) {
	s, token := policyTestServer(t)
	draft := graphDraftFixture("chat-one")
	draft.WritePending = true
	draft.Description = "Reviewed description"
	doc := graphDraftDocument{1, []graphDraft{draft}, []string{}}
	saved := policyWorkspaceSave(t, s, token, "/api/draft-graphs", doc)
	for _, change := range []func(*graphDraft){func(d *graphDraft) { d.Content = "{}" }, func(d *graphDraft) { d.Description = "other" }, func(d *graphDraft) { d.WritePending = false }} {
		changed := draft
		change(&changed)
		if w := policyWorkspaceCall(s, token, "PUT", "/api/draft-graphs", saved.SHA256, graphDraftDocument{1, []graphDraft{changed}, []string{}}); w.Code != 403 {
			t.Fatal("pending candidate changed", w.Code, w.Body.String())
		}
	}
	if w := policyWorkspaceCall(s, token, "PUT", "/api/draft-graphs", saved.SHA256, graphDraftDocument{1, []graphDraft{}, []string{graphDraftIdentity(draft)}}); w.Code != 403 {
		t.Fatal("pending history deleted", w.Code)
	}
	finalized := draft
	finalized.WritePending = false
	finalized.Saved = true
	target := graphDraftDocument{1, []graphDraft{finalized}, []string{}}
	if w := policyWorkspaceCall(s, token, "PUT", "/api/draft-graphs", saved.SHA256, target); w.Code != 409 {
		t.Fatal("missing graph finalized", w.Code, w.Body.String())
	}
	writeProjectFile(t, s.projectDir, draft.Path, draft.Content)
	writeProjectFile(t, s.projectDir, "jpack.json", `{"configVersion":"2","graphs":{"flow":{"path":"flow.graph.json","description":"changed"}}}`)
	if w := policyWorkspaceCall(s, token, "PUT", "/api/draft-graphs", saved.SHA256, target); w.Code != 409 {
		t.Fatal("changed registration finalized", w.Code, w.Body.String())
	}
	writeProjectFile(t, s.projectDir, "jpack.json", `{"configVersion":"2","graphs":{"flow":{"path":"flow.graph.json","description":"Reviewed description"}}}`)
	if w := policyWorkspaceCall(s, token, "PUT", "/api/draft-graphs", saved.SHA256, target); w.Code != 200 {
		t.Fatal("exact graph finalization", w.Code, w.Body.String())
	}
}
