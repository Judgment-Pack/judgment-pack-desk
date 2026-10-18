package desk

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func policyAuthoringRead(t *testing.T, s *Server, token string, p policyReply, selector string) policyAuthoringSource {
	t.Helper()
	w := policyCall(s, token, "GET", "/api/policies/"+p.Document.ID+"/authoring-source?"+selector, nil)
	var source policyAuthoringSource
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &source) != nil {
		t.Fatalf("source read: %d %s", w.Code, w.Body.String())
	}
	return source
}

func policyWorkspaceCall(s *Server, token, method, path, revision string, body any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, bytes.NewReader(raw))
	r.Header.Set("Authorization", "Bearer "+token)
	if revision != "" {
		r.Header.Set("If-Match", revision)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func policyWorkspaceRead(t *testing.T, s *Server, token, path string) conversationReply {
	t.Helper()
	w := policyWorkspaceCall(s, token, "GET", path, "", nil)
	var result conversationReply
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil {
		t.Fatalf("workspace read: %d %s", w.Code, w.Body.String())
	}
	return result
}

func policyWorkspaceBody(drafts bool, source policyAuthoringSource) map[string]any {
	entry := map[string]any{"id": "interpretation-one", "title": "Private interpretation", "policySources": []policyAuthoringSource{source}, "messages": []any{map[string]any{"role": "user", "content": "Retain the original Policy basis."}}, "checkpoint": map[string]any{"state": map[string]any{"status": "ready", "candidates": []any{map[string]any{"text": `{"title":"Private candidate"}`, "revision": 1}}, "unknowns": []any{}, "cases": []any{}}}}
	key := "chats"
	if drafts {
		key = "drafts"
	}
	return map[string]any{"version": 1, key: []any{entry}}
}

func policyWorkspaceSave(t *testing.T, s *Server, token, path string, body any) conversationReply {
	t.Helper()
	current := policyWorkspaceRead(t, s, token, path)
	w := policyWorkspaceCall(s, token, "PUT", path, current.SHA256, body)
	var saved conversationReply
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &saved) != nil {
		t.Fatalf("workspace save: %d %s", w.Code, w.Body.String())
	}
	return saved
}

func policyRestoreCall(t *testing.T, s *Server, token, destination string, backup []byte) *httptest.ResponseRecorder {
	t.Helper()
	status := policyCall(s, token, "GET", "/api/storage", nil)
	var storage storageStatus
	if status.Code != 200 || json.Unmarshal(status.Body.Bytes(), &storage) != nil {
		t.Fatalf("storage status: %d %s", status.Code, status.Body.String())
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	settings, _ := json.Marshal(moveChatData{Path: destination, Revision: storage.Revision})
	if err := form.WriteField("settings", string(settings)); err != nil {
		t.Fatal(err)
	}
	part, err := form.CreateFormFile("backup", "backup.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(backup); err != nil {
		t.Fatal(err)
	}
	if err = form.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/storage/restore", &body)
	r.Header.Set("Content-Type", form.FormDataContentType())
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func policyForgedBackup(t *testing.T, raw []byte) []byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	writer := zip.NewWriter(&out)
	manifest := backupManifest{Version: 1, Files: []backupEntry{}}
	for _, file := range reader.File {
		if file.Name == "manifest.json" {
			continue
		}
		entry, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(entry)
		entry.Close()
		if err != nil {
			t.Fatal(err)
		}
		if conversationFileName.MatchString(file.Name) || draftPackFileName.MatchString(file.Name) || graphDraftFileName.MatchString(file.Name) {
			var body map[string]any
			if json.Unmarshal(data, &body) != nil {
				t.Fatal("invalid fixture history")
			}
			key := "chats"
			if draftPackFileName.MatchString(file.Name) || graphDraftFileName.MatchString(file.Name) {
				key = "drafts"
			}
			body[key].([]any)[0].(map[string]any)["policySources"].([]any)[0].(map[string]any)["narrative"] = "Forged retained Policy narrative."
			data, _ = json.Marshal(body)
		}
		item, err := writer.Create(file.Name)
		if err != nil {
			t.Fatal(err)
		}
		item.Write(data)
		manifest.Files = append(manifest.Files, backupEntry{Name: file.Name, Bytes: int64(len(data)), SHA256: digestOf(data)})
	}
	item, _ := writer.Create("manifest.json")
	data, _ := json.Marshal(manifest)
	item.Write(data)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestPolicyAuthoringSourcePinsOlderPublicationAfterEdits(t *testing.T) {
	s, token := policyTestServer(t)
	p := policyApprove(t, s, token, policyCreate(t, s, token))
	published := policyMutation(t, policyCall(s, token, "POST", "/api/policies/"+p.Document.ID+"/publish", policyPublishBody(t, p))).Policy
	publication := published.Document.Publications[0]
	edit := policyEditBody(t, published)
	edit.Title, edit.Narrative, edit.ArtifactVersion = "New policy title", "New policy text must never substitute the selected source.", "2.0.0"
	latest := policyMutation(t, policyCall(s, token, "PUT", "/api/policies/"+p.Document.ID, edit)).Policy
	ledgerPath := filepath.Join(s.projectDir, policyDirectory, "ledger.json")
	before, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	source := policyAuthoringRead(t, s, token, p, "publicationId="+publication.ID)
	old := p.Document.Candidate
	if source.Version != 1 || source.PolicyID != p.Document.ID || source.RevisionID != old.RevisionID || source.CandidateDigest != old.Digest || source.Content != old.Content || source.Title != old.Title || source.ArtifactVersion != old.ArtifactVersion || source.Narrative != old.Narrative || source.PublicationID != publication.ID || source.Narrative == latest.Document.Candidate.Narrative {
		t.Fatal("publication source was substituted", source)
	}
	revision := policyAuthoringRead(t, s, token, p, "revisionId="+old.RevisionID)
	source.PublicationID = ""
	if source != revision {
		t.Fatal("manual revision and publication resolved different retained content")
	}
	after, _ := os.ReadFile(ledgerPath)
	if !bytes.Equal(before, after) {
		t.Fatal("preparation mutated Policy storage")
	}
	policyWorkspaceSave(t, s, token, "/api/conversations", policyWorkspaceBody(false, revision))
}

func TestPolicyAuthoringSourceSelectorsAndSession(t *testing.T) {
	s, token := policyTestServer(t)
	p := policyCreate(t, s, token)
	base := "/api/policies/" + p.Document.ID + "/authoring-source"
	revision := p.Document.Candidate.RevisionID
	for _, query := range []string{"", "?revisionId=", "?revisionId=" + revision + "&publicationId=publication_" + strings.Repeat("a", 48), "?revisionId=" + revision + "&revisionId=" + revision, "?revisionId=" + revision + "&other=x", "?publicationId=" + revision, "?revisionId=bad", "?revisionId=" + revision + ";other=x"} {
		policyError(t, policyCall(s, token, "GET", base+query, nil), 400, CodeBadRequest)
	}
	policyError(t, policyCall(s, token, "GET", base+"?publicationId=publication_"+strings.Repeat("a", 48), nil), 404, CodeNotFound)
	policyError(t, policyCall(s, testToken, "GET", base+"?revisionId="+revision, nil), 401, CodeUnauthorized)
	if err := os.Remove(filepath.Join(s.projectDir, retainedArtifactDirectory, p.Document.Candidate.Content.SHA256)); err != nil {
		t.Fatal(err)
	}
	failure := policyError(t, policyCall(s, token, "GET", base+"?revisionId="+revision, nil), 503, "storage-unavailable")
	if failure["outcome"] == "unknown" {
		t.Fatal("read-only preparation claimed an uncertain write")
	}
	s.sessions.revoke(token)
	policyError(t, policyCall(s, token, "GET", base+"?revisionId="+revision, nil), 401, CodeUnauthorized)
}

func TestPolicyAuthoringSourceSharedFixtureBindsCandidateEnvelope(t *testing.T) {
	raw, err := os.ReadFile("testdata/policy-authoring-source.json")
	if err != nil {
		t.Fatal(err)
	}
	var source policyAuthoringSource
	if err = decodeDataJSON(raw, &source); err != nil {
		t.Fatal(err)
	}
	candidate, err := json.Marshal(policyCandidateContent{SchemaVersion: 1, Title: source.Title, Narrative: source.Narrative, ArtifactVersion: source.ArtifactVersion})
	if err != nil || artifactReference(candidate) != source.Content || digestOf(candidate) != source.CandidateDigest || source.Content.Bytes == len(source.Narrative) {
		t.Fatal("fixture confuses retained candidate content with narrative bytes", string(candidate), source)
	}
	for _, drafts := range []bool{false, true} {
		body, _ := json.Marshal(policyWorkspaceBody(drafts, source))
		if _, err := policyWorkspaceEntries(body, drafts); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPolicyAuthoringSourceRejectsJSONAliasSubstitution(t *testing.T) {
	raw, err := os.ReadFile("testdata/policy-authoring-source.json")
	if err != nil {
		t.Fatal(err)
	}
	var source policyAuthoringSource
	if json.Unmarshal(raw, &source) != nil {
		t.Fatal("invalid source fixture")
	}
	encoded, _ := json.Marshal(source)
	for _, drafts := range []bool{false, true} {
		key, alias := "chats", "Chats"
		validate := validateConversations
		if drafts {
			key, alias, validate = "drafts", "Drafts", validateDraftPacks
		}
		entry := `{"id":"private","policySources":[` + string(encoded) + `]}`
		for _, body := range []string{
			`{"version":1,"` + key + `":[` + entry + `],"` + alias + `":[]}`,
			`{"version":1,"` + key + `":[` + strings.Replace(entry, `"policySources":`, `"PolicySources":`, 1) + `]}`,
			`{"version":1,"` + key + `":[` + strings.Replace(entry, `"id":"private",`, `"id":"private","PolicySources":[],`, 1) + `]}`,
			`{"version":1,"` + key + `":[` + strings.Replace(entry, `"narrative":`, `"Narrative":"replacement","narrative":`, 1) + `]}`,
			`{"version":1,"` + key + `":[` + strings.Replace(entry, `"bytes":121`, `"bytes":121,"Bytes":121`, 1) + `]}`,
			`{"version":1,"` + key + `":[` + strings.Replace(entry, `"candidateDigest":`, `"publicationId":null,"candidateDigest":`, 1) + `]}`,
		} {
			if validate([]byte(body)) == nil {
				t.Fatal("JSON alias could substitute or hide private material", body)
			}
		}
	}
}

func TestPolicyAuthoringSourceWorkspaceRejectsFalseBasis(t *testing.T) {
	s, token := policyTestServer(t)
	p := policyApprove(t, s, token, policyCreate(t, s, token))
	p = policyMutation(t, policyCall(s, token, "POST", "/api/policies/"+p.Document.ID+"/publish", policyPublishBody(t, p))).Policy
	source := policyAuthoringRead(t, s, token, p, "publicationId="+p.Document.Publications[0].ID)
	other := policyCreate(t, s, token)
	for _, drafts := range []bool{false, true} {
		path := "/api/conversations"
		if drafts {
			path = "/api/draft-packs"
		}
		for _, change := range []struct {
			name string
			edit func(*policyAuthoringSource)
		}{{"narrative", func(s *policyAuthoringSource) { s.Narrative += " substituted" }}, {"title", func(s *policyAuthoringSource) { s.Title = "False title" }}, {"version", func(s *policyAuthoringSource) { s.ArtifactVersion = "2.0.0" }}, {"content size", func(s *policyAuthoringSource) { s.Content.Bytes++ }}, {"wrong policy", func(s *policyAuthoringSource) { s.PolicyID = other.Document.ID }}, {"wrong publication", func(s *policyAuthoringSource) { s.PublicationID = "publication_" + strings.Repeat("a", 48) }}, {"wrong revision", func(s *policyAuthoringSource) { s.RevisionID = other.Document.Candidate.RevisionID }}} {
			t.Run(path+"/"+change.name, func(t *testing.T) {
				bad := source
				change.edit(&bad)
				w := policyWorkspaceCall(s, token, "PUT", path, "absent", policyWorkspaceBody(drafts, bad))
				if w.Code != 400 && w.Code != 404 {
					t.Fatal("false basis admitted", w.Code, w.Body.String())
				}
				if got := policyWorkspaceRead(t, s, token, path); got.SHA256 != "absent" {
					t.Fatal("refused basis was persisted")
				}
			})
		}
		w := policyWorkspaceCall(s, testToken, "PUT", path, "absent", policyWorkspaceBody(drafts, source))
		if w.Code != 401 {
			t.Fatal("launch credential introduced source", w.Code, w.Body.String())
		}
	}
}

func TestPolicyAuthoringSourceDerivativeHistoryCustody(t *testing.T) {
	s, token := policyTestServer(t)
	p := policyCreate(t, s, token)
	source := policyAuthoringRead(t, s, token, p, "revisionId="+p.Document.Candidate.RevisionID)
	for _, drafts := range []bool{false, true} {
		path, key := "/api/conversations", "chats"
		if drafts {
			path, key = "/api/draft-packs", "drafts"
		}
		body := policyWorkspaceBody(drafts, source)
		saved := policyWorkspaceSave(t, s, token, path, body)
		for _, mutation := range []string{"remove source", "change source"} {
			entry := body[key].([]any)[0].(map[string]any)
			if mutation == "remove source" {
				delete(entry, "policySources")
			} else {
				changed := source
				changed.Narrative += " altered"
				entry["policySources"] = []policyAuthoringSource{changed}
			}
			w := policyWorkspaceCall(s, token, "PUT", path, saved.SHA256, body)
			if w.Code != 403 {
				t.Fatal("retained source was declassified", path, mutation, w.Code, w.Body.String())
			}
		}
		if got := policyWorkspaceRead(t, s, token, path); got.SHA256 != saved.SHA256 {
			t.Fatal("refused edit changed retained history")
		}
	}
	for _, path := range []string{"/api/conversations", "/api/draft-packs", "/api/storage/backup", "/api/briefs?subject=pack-draft:interpretation-one"} {
		w := policyCall(s, testToken, "GET", path, nil)
		if w.Code != 401 || strings.Contains(w.Body.String(), source.Narrative) {
			t.Fatal("launch credential read private derivative", path, w.Code, w.Body.String())
		}
	}
	// The independent draft survives conversation deletion and remains private.
	policyWorkspaceSave(t, s, token, "/api/conversations", map[string]any{"version": 1, "chats": []any{}})
	if !bytes.Contains(policyWorkspaceRead(t, s, token, "/api/draft-packs").Content, []byte(source.Narrative[:20])) {
		t.Fatal("chat deletion lost independent draft provenance")
	}
	// Current content availability must not erase authorized retained history or
	// force its old exact source to be replaced by the latest candidate.
	if err := os.Remove(filepath.Join(s.projectDir, policyDirectory, "ledger.json")); err != nil {
		t.Fatal(err)
	}
	draft := policyWorkspaceRead(t, s, token, "/api/draft-packs")
	var retained any
	if json.Unmarshal(draft.Content, &retained) != nil {
		t.Fatal("invalid retained draft")
	}
	policyWorkspaceSave(t, s, token, "/api/draft-packs", retained)
	if w := policyCall(s, token, "GET", "/api/storage/backup", nil); w.Code != 200 {
		t.Fatal("availability blocked historical export", w.Code, w.Body.String())
	}
	s.sessions.revoke(token)
	for _, path := range []string{"/api/conversations", "/api/draft-packs", "/api/storage/backup"} {
		if w := policyCall(s, token, "GET", path, nil); w.Code != 401 {
			t.Fatal("revoked session read derivative", path, w.Code)
		}
	}
}

func TestPolicyAuthoringSourceRestoreRequiresExactAvailableBasis(t *testing.T) {
	s, token := policyTestServer(t)
	p := policyCreate(t, s, token)
	source := policyAuthoringRead(t, s, token, p, "revisionId="+p.Document.Candidate.RevisionID)
	policyWorkspaceSave(t, s, token, "/api/conversations", policyWorkspaceBody(false, source))
	policyWorkspaceSave(t, s, token, "/api/draft-packs", policyWorkspaceBody(true, source))
	export := policyCall(s, token, "GET", "/api/storage/backup", nil)
	if export.Code != 200 {
		t.Fatal(export.Code, export.Body.String())
	}
	// Restore into an empty private store to exercise admission of incoming
	// material, independently of the existing protected store's access check.
	config := s.cfg
	config.DeskConfigDir = t.TempDir()
	restored, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restored.Close() })
	s, token = restored, policyTestSession(t, restored)
	target := filepath.Join(t.TempDir(), "restored")
	if w := policyRestoreCall(t, s, testToken, target, export.Body.Bytes()); w.Code != 401 {
		t.Fatal("launch credential restored private material", w.Code, w.Body.String())
	}
	if w := policyRestoreCall(t, s, token, target, export.Body.Bytes()); w.Code != 200 {
		t.Fatal("same-install restore", w.Code, w.Body.String())
	}
	for _, path := range []string{"/api/conversations", "/api/draft-packs"} {
		if !bytes.Contains(policyWorkspaceRead(t, s, token, path).Content, []byte(source.RevisionID)) {
			t.Fatal("restore lost exact source", path)
		}
		if w := policyCall(s, testToken, "GET", path, nil); w.Code != 401 {
			t.Fatal("restore removed private association", path, w.Code)
		}
	}
	forgedTarget := filepath.Join(t.TempDir(), "forged")
	if w := policyRestoreCall(t, s, token, forgedTarget, policyForgedBackup(t, export.Body.Bytes())); w.Code != 400 {
		t.Fatal("forged backup basis admitted", w.Code, w.Body.String())
	}
	for _, name := range []string{s.conversationName(), draftPackName(s.conversationName())} {
		if _, err := os.Stat(filepath.Join(forgedTarget, name)); !os.IsNotExist(err) {
			t.Fatal("forged backup left copied private content", err)
		}
	}
	ledgerPath := filepath.Join(s.projectDir, policyDirectory, "ledger.json")
	if err := os.Remove(ledgerPath); err != nil {
		t.Fatal(err)
	}
	before := policyWorkspaceRead(t, s, token, "/api/draft-packs")
	refusedTarget := filepath.Join(t.TempDir(), "unavailable")
	w := policyRestoreCall(t, s, token, refusedTarget, export.Body.Bytes())
	if w.Code != 404 {
		t.Fatal("unresolved imported source admitted", w.Code, w.Body.String())
	}
	if after := policyWorkspaceRead(t, s, token, "/api/draft-packs"); after.SHA256 != before.SHA256 {
		t.Fatal("failed restore changed current history")
	}
	if _, err := os.Stat(filepath.Join(refusedTarget, draftPackName(s.conversationName()))); !os.IsNotExist(err) {
		t.Fatal("failed restore retained copied draft", err)
	}
}

func TestPolicyAuthoringSourceValidationBoundsAndNonPolicyHistory(t *testing.T) {
	s, token := policyTestServer(t)
	p := policyCreate(t, s, token)
	source := policyAuthoringRead(t, s, token, p, "revisionId="+p.Document.Candidate.RevisionID)
	for _, drafts := range []bool{false, true} {
		key := "chats"
		validate := validateConversations
		if drafts {
			key, validate = "drafts", validateDraftPacks
		}
		for _, sources := range []any{nil, "text", []policyAuthoringSource{source, source}, append(make([]policyAuthoringSource, 8), source)} {
			body := policyWorkspaceBody(drafts, source)
			body[key].([]any)[0].(map[string]any)["policySources"] = sources
			raw, _ := json.Marshal(body)
			if validate(raw) == nil {
				t.Fatal("malformed or duplicate sources admitted", drafts, sources)
			}
		}
		tooLarge := source
		tooLarge.Narrative = strings.Repeat("x", policyNarrativeLimit+1)
		raw, _ := json.Marshal(policyWorkspaceBody(drafts, tooLarge))
		if validate(raw) == nil {
			t.Fatal("oversized private narrative admitted")
		}
	}
	for _, path := range []string{"/api/conversations", "/api/draft-packs"} {
		key := "chats"
		if strings.HasSuffix(path, "draft-packs") {
			key = "drafts"
		}
		policyWorkspaceSave(t, s, testToken, path, map[string]any{"version": 1, key: []any{map[string]any{"id": "ordinary", "title": "Ordinary history"}}})
	}
	backup := policyCall(s, testToken, "GET", "/api/storage/backup", nil)
	if backup.Code != 200 {
		t.Fatal("non-Policy backup changed", backup.Code, backup.Body.String())
	}
	if w := policyRestoreCall(t, s, testToken, filepath.Join(t.TempDir(), "ordinary"), backup.Body.Bytes()); w.Code != 200 {
		t.Fatal("non-Policy restore changed", w.Code, w.Body.String())
	}
}

func TestPolicyAuthoringSourceInterpretationProposalScopeAndRetention(t *testing.T) {
	s, token := policyTestServer(t)
	p := policyCreate(t, s, token)
	proposal := policyInterpretationProposal{Version: 1, ID: "proposal-one", PolicyID: p.Document.ID, RevisionID: p.Document.Candidate.RevisionID, CreatedAt: "2026-10-18T12:00:00Z"}
	entry := map[string]any{"id": "policy-chat", "policy": map[string]string{"id": p.Document.ID}, "policyInterpretations": []policyInterpretationProposal{proposal}, "checkpoint": map[string]any{"retained": "Private Policy draft reasoning."}}
	body := map[string]any{"version": 1, "chats": []any{entry}}
	policyWorkspaceSave(t, s, token, "/api/conversations", body)
	proposal.OpenedChatID = "interpretation-one"
	entry["policyInterpretations"] = []policyInterpretationProposal{proposal}
	policyWorkspaceSave(t, s, token, "/api/conversations", body)
	for _, change := range []struct {
		name string
		edit func(*policyInterpretationProposal)
	}{{"wrong owner", func(p *policyInterpretationProposal) { p.PolicyID = "policy_" + strings.Repeat("a", 48) }}, {"wrong revision kind", func(p *policyInterpretationProposal) { p.RevisionID = "publication_" + strings.Repeat("b", 48) }}, {"wrong publication kind", func(p *policyInterpretationProposal) { p.PublicationID = p.RevisionID }}, {"missing timestamp", func(p *policyInterpretationProposal) { p.CreatedAt = "" }}} {
		t.Run(change.name, func(t *testing.T) {
			bad := proposal
			change.edit(&bad)
			entry["policyInterpretations"] = []policyInterpretationProposal{bad}
			raw, _ := json.Marshal(body)
			if validateConversations(raw) == nil {
				t.Fatal("invalid proposal scope admitted")
			}
		})
	}
	entry["policyInterpretations"] = []policyInterpretationProposal{proposal}
	raw, _ := json.Marshal(map[string]any{"version": 1, "drafts": []any{entry}})
	if validateDraftPacks(raw) == nil {
		t.Fatal("Policy chat handoff admitted on independent draft")
	}
	// A caller cannot remove even the original Policy chat's association while
	// leaving its checkpoint behind for launch-secret access.
	delete(entry, "policyInterpretations")
	delete(entry, "policy")
	current := policyWorkspaceRead(t, s, token, "/api/conversations")
	w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", current.SHA256, body)
	if w.Code != 403 {
		t.Fatal("Policy chat association stripped", w.Code, w.Body.String())
	}
	if w = policyCall(s, testToken, "GET", "/api/conversations", nil); w.Code != 401 {
		t.Fatal("stripped history exposed checkpoint", w.Code, w.Body.String())
	}
}

func TestPolicyAuthoringSourceBusyAdmissionPreservesWorkspace(t *testing.T) {
	s, token := policyTestServer(t)
	p := policyCreate(t, s, token)
	source := policyAuthoringRead(t, s, token, p, "revisionId="+p.Document.Candidate.RevisionID)
	policyWorkspaceRead(t, s, token, "/api/draft-packs")
	root, err := s.openPolicies(false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	lock, err := root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err = lockPrivateData(lock, true); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	raw, _ := json.Marshal(policyWorkspaceBody(true, source))
	r := httptest.NewRequest("PUT", "/api/draft-packs", bytes.NewReader(raw)).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("If-Match", "absent")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	policyError(t, w, 503, "storage-busy")
	if w.Header().Get("Retry-After") != "1" {
		t.Fatal("busy admission omitted bounded retry guidance")
	}
	if saved := policyWorkspaceRead(t, s, token, "/api/draft-packs"); saved.SHA256 != "absent" {
		t.Fatal("busy source admission changed private draft storage")
	}
}
