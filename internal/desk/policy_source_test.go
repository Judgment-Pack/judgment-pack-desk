package desk

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPolicyImportRetainsOriginalAcrossEditRecoveryAndRestart(t *testing.T) {
	s, token := policyTestServer(t)
	raw := []byte("# Access\r\nCafé 中文\r\nKeep exact bytes.\r\n")
	request := policyEditRequest{OperationID: policyOp(t), Title: "Access", Narrative: "Editable interpretation", ArtifactVersion: "1.0.0", Import: &policyImport{Name: "Access.md", MediaType: "text/markdown", Data: raw}}
	committed := policyMutation(t, policyCall(s, token, "POST", "/api/policies", request))
	created := committed.Policy
	source := created.Document.Candidate.Source
	if source == nil || source.Name != request.Import.Name || source.MediaType != request.Import.MediaType || source.Origin != "user-upload" || source.Content != artifactReference(raw) || created.Document.ReviewState != "unreviewed" {
		t.Fatal("import lost exact source or claimed approval", created)
	}
	// A lost acknowledgement is recovered read-only with the original source;
	// an explicit exact replay returns the same immutable outcome.
	recovered := policyMutation(t, policyCall(s, token, "GET", "/api/policy-operations/"+request.OperationID, nil))
	replay := policyMutation(t, policyCall(s, token, "POST", "/api/policies", request))
	if recovered.Delivery != "recovered" || replay.Delivery != "recovered" || replay.Policy.Revision != created.Revision || !samePolicySource(recovered.Policy.Document.Candidate.Source, source) {
		t.Fatal("source recovery diverged")
	}
	changedRequest := request
	changedImport := *request.Import
	changedImport.Name = "renamed.md"
	changedRequest.Import = &changedImport
	policyError(t, policyCall(s, token, "POST", "/api/policies", changedRequest), 409, "idempotency-conflict")
	approved := policyApprove(t, s, token, created)
	published := policyMutation(t, policyCall(s, token, "POST", "/api/policies/"+created.Document.ID+"/publish", policyPublishBody(t, approved))).Policy
	edit := policyEditBody(t, published)
	edit.Narrative = "Edited narrative, original unchanged"
	edited := policyMutation(t, policyCall(s, token, "PUT", "/api/policies/"+created.Document.ID, edit)).Policy
	if edited.Document.ReviewState != "unreviewed" || edited.Document.Candidate.Digest == created.Document.Candidate.Digest || !samePolicySource(edited.Document.Candidate.Source, source) || edited.Document.Publications[0].Content != published.Document.Candidate.Content {
		t.Fatal("edit changed source or approval", edited)
	}
	// Chat deletion owns neither retained sources nor Policy records.
	chats := draftCall(s, "GET", "/api/conversations", nil, "")
	var chatReply conversationReply
	_ = json.Unmarshal(chats.Body.Bytes(), &chatReply)
	removed := draftCall(s, "PUT", "/api/conversations", map[string]any{"version": 1, "chats": []any{}}, chatReply.SHA256)
	if removed.Code != 200 {
		t.Fatal(removed.Body.String())
	}
	config := s.cfg
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	token = policyTestSession(t, reopened)
	current := policyRead(t, reopened, token, created.Document.ID)
	if current.Revision != edited.Revision || current.Document.Candidate.Narrative != edit.Narrative || !samePolicySource(current.Document.Candidate.Source, source) {
		t.Fatal("restart lost import", current)
	}
	response := policyCall(reopened, token, "GET", "/api/policies/"+created.Document.ID+"/source", nil)
	if response.Code != 200 || !bytes.Equal(response.Body.Bytes(), raw) || !strings.Contains(response.Header().Get("Content-Disposition"), "Access.md") || response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("original download changed", response.Code, response.Body.String())
	}
	policyError(t, policyCall(reopened, "", "GET", "/api/policies/"+created.Document.ID+"/source", nil), 401, CodeUnauthorized)
}

func TestPolicyImportRefusesUnsupportedAndForgedSources(t *testing.T) {
	s, token := policyTestServer(t)
	for _, source := range []policyImport{
		{Name: "../policy.md", MediaType: "text/markdown", Data: []byte("text")},
		{Name: `C:\policy.txt`, MediaType: "text/plain", Data: []byte("text")},
		{Name: "policy\n.md", MediaType: "text/markdown", Data: []byte("text")},
		{Name: "policy.pdf", MediaType: "text/plain", Data: []byte("text")},
		{Name: "policy.md", MediaType: "application/octet-stream", Data: []byte("text")},
		{Name: "policy.txt", MediaType: "text/plain", Data: []byte{0xff}},
		{Name: "policy.txt", MediaType: "text/plain", Data: []byte("binary\x00data")},
		{Name: "policy.txt", MediaType: "text/plain", Data: bytes.Repeat([]byte("x"), policySourceByteLimit+1)},
	} {
		t.Run(source.Name+source.MediaType, func(t *testing.T) {
			policyError(t, policyCall(s, token, "POST", "/api/policies", policyEditRequest{OperationID: policyOp(t), Import: &source}), 400, "invalid-policy-source")
		})
	}
	forged := map[string]any{"operationId": policyOp(t), "import": map[string]any{"name": "policy.md", "mediaType": "text/markdown", "data": "dGV4dA==", "origin": "verified-provider", "sha256": strings.Repeat("0", 64)}}
	policyError(t, policyCall(s, token, "POST", "/api/policies", forged), 400, CodeBadRequest)
	policyError(t, policyCall(s, "", "POST", "/api/policies", policyEditRequest{OperationID: policyOp(t), Import: &policyImport{Name: "policy.md", MediaType: "text/markdown", Data: []byte("text")}}), 401, CodeUnauthorized)
	list := policyCall(s, token, "GET", "/api/policies", nil)
	if !strings.Contains(list.Body.String(), `"items":[]`) {
		t.Fatal("refused import left a policy", list.Body.String())
	}
	plain := policyCreate(t, s, token)
	edit := policyEditBody(t, plain)
	edit.Import = &policyImport{Name: "policy.md", MediaType: "text/markdown", Data: []byte("text")}
	policyError(t, policyCall(s, token, "PUT", "/api/policies/"+plain.Document.ID, edit), 400, CodeBadRequest)
	policyError(t, policyCall(s, token, "GET", "/api/policies/"+plain.Document.ID+"/source", nil), 404, CodeNotFound)
	origin := httptest.NewRequest("GET", "/api/policies/"+plain.Document.ID+"/source", nil)
	origin.Header.Set("Authorization", "Bearer "+token)
	origin.Header.Set("Origin", "https://evil.example")
	response := httptest.NewRecorder()
	s.ServeHTTP(response, origin)
	if response.Code != 403 {
		t.Fatal("source bypassed origin guard", response.Code)
	}
}

func TestPolicySourceIdentityAndLegacyCandidateBytes(t *testing.T) {
	// Omitting source retains the exact previous schema-1 candidate bytes. No
	// ledger/schema migration rewrites schema-2 records or published identities.
	legacy := []byte(`{"schemaVersion":1,"title":"Legacy","narrative":"Original","artifactVersion":"1.0.0"}`)
	raw, _ := json.Marshal(policyCandidateContent{SchemaVersion: 1, Title: "Legacy", Narrative: "Original", ArtifactVersion: "1.0.0"})
	if !bytes.Equal(raw, legacy) {
		t.Fatal("no-source identity changed", string(raw))
	}
	source := policySource{Name: "one.md", MediaType: "text/markdown", Origin: "user-upload", Content: artifactReference([]byte("same source"))}
	candidate := policyCandidateContent{SchemaVersion: 1, Source: &source, Title: "Policy", Narrative: "Same narrative", ArtifactVersion: "1.0.0"}
	first, _ := json.Marshal(candidate)
	source.Name = "two.md"
	second, _ := json.Marshal(candidate)
	if digestOf(first) == digestOf(second) {
		t.Fatal("candidate identity omitted source name")
	}
	source.MediaType = "text/plain"
	third, _ := json.Marshal(candidate)
	if digestOf(second) == digestOf(third) {
		t.Fatal("candidate identity omitted media type")
	}
}

func TestPolicyImportRetentionFailuresLeaveNoReachableDraft(t *testing.T) {
	for _, failAt := range []int{1, 2, 3} {
		t.Run(map[int]string{1: "original", 2: "candidate", 3: "receipt"}[failAt], func(t *testing.T) {
			s, token := policyTestServer(t)
			s.retainedArtifacts = &policyFailingArtifacts{base: s.artifactRepository(), failAt: failAt}
			request := policyEditRequest{OperationID: policyOp(t), Title: "Import", Narrative: "Editable draft", ArtifactVersion: "1.0.0", Import: &policyImport{Name: "source.md", MediaType: "text/markdown", Data: []byte("Exact original\r\n中文")}}
			refusal := policyError(t, policyCall(s, token, "POST", "/api/policies", request), 503, "storage-unavailable")
			if refusal["outcome"] != "not-committed" {
				t.Fatal("retention failure stranded operation", refusal)
			}
			list := policyCall(s, token, "GET", "/api/policies", nil)
			if !strings.Contains(list.Body.String(), `"items":[]`) {
				t.Fatal("partial import exposed", list.Body.String())
			}
			policyError(t, policyCall(s, token, "GET", "/api/policy-operations/"+request.OperationID, nil), 404, CodeNotFound)
			// Only this explicit new write retries after the proven precommit refusal.
			request.OperationID = policyOp(t)
			saved := policyMutation(t, policyCall(s, token, "POST", "/api/policies", request)).Policy
			source := policyCall(s, token, "GET", "/api/policies/"+saved.Document.ID+"/source", nil)
			if source.Code != 200 || !bytes.Equal(source.Body.Bytes(), request.Import.Data) {
				t.Fatal("explicit retry lost original")
			}
		})
	}
}

func TestPolicyImportSupportedMaximumFitsExistingRequestBoundary(t *testing.T) {
	request := policyEditRequest{OperationID: strings.Repeat("x", 128), Title: strings.Repeat("\x01", 500), Narrative: strings.Repeat("\x01", policyNarrativeLimit), ArtifactVersion: strings.Repeat("\x01", policyArtifactVersionLimit), Import: &policyImport{Name: strings.Repeat("x", 252) + ".md", MediaType: "text/markdown", Data: bytes.Repeat([]byte("a"), policySourceByteLimit)}}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	parsed := policyEditRequest{}
	r := httptest.NewRequest("POST", "/api/policies", bytes.NewReader(raw))
	if err = inputRequest(r, &parsed); err != nil {
		t.Fatal("supported maximum exceeds shared request bound", len(raw), err)
	}
	if err = policyValidateCommand(policyCommand{Kind: "create", Edit: &parsed}, policyStoredRecord{}, nil); err != nil {
		t.Fatal("supported maximum refused", err)
	}
}
