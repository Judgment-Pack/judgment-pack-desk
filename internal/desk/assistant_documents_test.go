package desk

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

// Hand-authored attachment-v1 vector. The fixture serializer below is independent
// of Desk/Gateway code: these vectors contain ASCII member names and integers,
// whose sorted Go JSON encoding (without HTML escaping) is their SPEC canon.
// Signatures use the published Gateway receipt/3 and seal/2 domain prefixes.
func assistantDocumentFixtureJSON(t *testing.T, v any) []byte {
	t.Helper()
	var out bytes.Buffer
	e := json.NewEncoder(&out)
	e.SetEscapeHTML(false)
	if err := e.Encode(v); err != nil {
		t.Fatal(err)
	}
	// Convert struct fields into sorted objects too; retain exact integer tokens.
	var sorted any
	decoder := json.NewDecoder(bytes.NewReader(out.Bytes()))
	decoder.UseNumber()
	if err := decoder.Decode(&sorted); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := e.Encode(sorted); err != nil {
		t.Fatal(err)
	}
	return bytes.TrimSuffix(out.Bytes(), []byte("\n"))
}
func assistantDocumentHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func assistantDocumentProgram(t *testing.T) string {
	t.Helper()
	program := os.Getenv("JPS_TEST_GATEWAY")
	if program == "" && os.Getenv("JPS_TEST_GATEWAY_BUNDLE") != "" {
		program = filepath.Join(os.Getenv("JPS_TEST_GATEWAY_BUNDLE"), executableName("gateway"))
	}
	if program == "" {
		t.Skip("set JPS_TEST_GATEWAY to the pinned Gateway verifier; offline only")
	}
	return program
}
func assistantDocumentFixture(t *testing.T, kind, shape string, change func(map[string]any)) ([]byte, inputResearchPin, assistantDocumentReference) {
	t.Helper()
	raw, err := os.ReadFile("testdata/assistant-document-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&record) != nil {
		t.Fatal("fixture")
	}
	object := attachmentObject{Version: 1, Original: attachmentOriginal{Name: "report.txt", MediaType: "text/plain", Bytes: "b3JpZ2luYWwgYnl0ZXM=", SHA256: "sha256:52c3935626c104b2cbc9031291a1c4d56614c38f52072a361d658a58a9c48698"}}
	key := ed25519.NewKeyFromSeed(make([]byte, 32))
	public := key.Public().(ed25519.PublicKey)
	keyID := assistantDocumentHash(public)[:32]
	pin := inputResearchPin{Authority: "gateway:test", Public: hex.EncodeToString(public)}
	proof := &attachmentProof{Session: "document-fixture", Source: "documents", Authority: pin.Authority, PublicKey: pin.Public}
	object.Proof = proof
	grant := strings.Repeat("a", 64)
	d := record["document"].(map[string]any)
	v := record["provenance"].(map[string]any)
	if kind != "inline" {
		record["original"] = map[string]any{"retention": "inline", "encoding": "base64", "bytes": object.Original.Bytes}
	}
	var args any = map[string]any{"document": object.Original, "options": map[string]string{"ocr": "auto"}}
	switch kind {
	case "drive":
		proof.Source = "drive"
		proof.Drive = &struct {
			Grant  string `json:"grant"`
			FileID string `json:"fileId"`
		}{grant, "file_123"}
		args = proof.Drive
		d["version"] = "7"
		v["source"] = map[string]any{"kind": "google-drive", "fileId": "file_123", "version": "7", "mediaType": "text/plain"}
	case "gmail":
		proof.Source = "gmail"
		proof.Gmail = &struct {
			Grant     string `json:"grant"`
			MessageID string `json:"messageId"`
		}{grant, "abc123"}
		args = proof.Gmail
		d["version"] = "7"
		v["source"] = map[string]any{"kind": "gmail", "messageId": "abc123", "threadId": "abc456", "version": "7", "format": "text-export-v1"}
	case "notion", "obsidian":
		id, url := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "https://www.notion.so/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		if kind == "obsidian" {
			id = "folder/note.md"
			url = "obsidian://open?vault=fixture&file=folder%2Fnote"
		}
		proof.Source = kind
		proof.Connected = &struct {
			Grant      string `json:"grant"`
			ResourceID string `json:"resourceId"`
		}{grant, id}
		args = proof.Connected
		d["version"] = d["id"]
		v["source"] = map[string]any{"kind": "connected-source", "provider": kind, "resourceId": id, "url": url, "version": d["id"], "format": "text-snapshot-v1"}
	case "resource":
		proof.Source = "fixture-source"
		proof.Resource = &struct {
			Grant      string `json:"grant"`
			ResourceID string `json:"resourceId"`
		}{grant, "file-1"}
		args = proof.Resource
		d["version"] = d["id"]
		v["source"] = map[string]any{"kind": "connection-resource", "provider": proof.Source, "resourceId": "file-1", "url": "https://example.com/file", "version": d["id"], "format": "retained-file-v1"}
	case "web", "web-pdf":
		proof.Source = "web"
		proof.Web = &struct {
			URL  string `json:"url"`
			Site string `json:"site,omitempty"`
		}{"https://example.com/report", "https://example.com/"}
		args = proof.Web
		d["version"] = d["id"]
		v["source"] = map[string]any{"kind": "web", "url": proof.Web.URL, "requestedUrl": proof.Web.URL, "responseDigest": d["id"], "version": d["id"], "format": "static-text-v1", "mediaType": "text/html"}
		if kind == "web-pdf" {
			object.Original.MediaType = "application/pdf"
			d["mediaType"] = "application/pdf"
			d["detectedMediaType"] = "application/pdf"
			v["processor"] = "adapter-document/pdf/1"
			source := v["source"].(map[string]any)
			source["format"] = "original-v1"
			source["mediaType"] = "application/pdf"
			content := record["content"].(map[string]any)
			content["extraction"] = "text-layer"
			for _, p := range content["pages"].([]any) {
				p.(map[string]any)["extraction"] = "text-layer"
			}
		}
	}
	if change != nil {
		change(record)
	}
	result := assistantDocumentFixtureJSON(t, record)
	salt := make([]byte, 32)
	committed := append(append(salt, []byte("args:")...), assistantDocumentFixtureJSON(t, args)...)
	digest := "sha256:" + assistantDocumentHash(result)
	receipt := map[string]any{"receiptVersion": "3", "kind": "acquisition", "sessionId": proof.Session, "callIndex": 0, "prevSignature": nil, "source": proof.Source, "authority": pin.Authority, "keyId": keyID, "resultDigest": digest, "argumentsCommitment": "sha256:" + assistantDocumentHash(committed), "caller": nil, "servedAt": "2026-10-20T00:00:00Z", "acquisition": map[string]any{"shape": shape, "endpoint": nil, "adapter": map[string]any{"name": "fixture", "version": "1", "digest": "sha256:" + strings.Repeat("a", 64)}, "observedAt": "2026-10-20T00:00:00Z", "schema": nil, "snapshot": nil, "statement": nil, "peerIdentity": nil, "upstreamToken": nil}}
	receipt["signature"] = hex.EncodeToString(ed25519.Sign(key, append([]byte("judgment-pack-gateway/receipt/3:"), assistantDocumentFixtureJSON(t, receipt)...)))
	seal := map[string]any{"sessionId": proof.Session, "finalCount": 1, "sealedAt": "2026-10-20T00:00:00Z", "keyId": keyID}
	seal["signature"] = hex.EncodeToString(ed25519.Sign(key, append([]byte("judgment-pack-gateway/seal/2:"), assistantDocumentFixtureJSON(t, seal)...)))
	proof.Response = string(assistantDocumentFixtureJSON(t, map[string]any{"receipt": receipt, "result": json.RawMessage(result), "salts": map[string]string{"args": strings.Repeat("0", 64)}}))
	proof.Registry = string(assistantDocumentFixtureJSON(t, seal)) + "\n"
	return assistantDocumentFixtureJSON(t, object), pin, assistantDocumentReference{ID: testAttachmentID, Digest: digest, Pages: []int{1}}
}

func TestAssistantDocumentSupportedSources(t *testing.T) {
	program := assistantDocumentProgram(t)
	for _, tc := range []struct{ kind, shape string }{{"inline", "command"}, {"drive", "http"}, {"gmail", "http"}, {"notion", "mcp"}, {"obsidian", "command"}, {"resource", "http"}, {"resource", "command"}, {"resource", "mcp"}, {"web", "http"}, {"web-pdf", "http"}} {
		t.Run(tc.kind+"-"+tc.shape, func(t *testing.T) {
			raw, pin, ref := assistantDocumentFixture(t, tc.kind, tc.shape, nil)
			original := bytes.Clone(raw)
			if dir := os.Getenv("JPS_DOCUMENT_REFERENCE_FIXTURES"); dir != "" {
				// Export independent fixture INPUTS only, never observed verifier answers.
				input := map[string]any{"object": json.RawMessage(raw), "pin": pin, "reference": ref}
				if err := os.WriteFile(filepath.Join(dir, tc.kind+"-"+tc.shape+".json"), assistantDocumentFixtureJSON(t, input), 0600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := verifyAssistantDocument(t.Context(), program, raw, pin, ref)
			if err != nil || got.Name != "report.txt" || got.Partial || len(got.Pages) != 1 || got.Pages[0].Text != "Selected first page." || got.Pages[0].Page != 1 || got.Pages[0].Citation != "attachment:"+testAttachmentID+"/"+ref.Digest+"/page/1" {
				t.Fatalf("supported acquisition: %+v %v", got, err)
			}
			projected := got.contextDocument()
			if projected.ID != "attachment-"+testAttachmentID || projected.Kind != "artifact" || projected.Revision != ref.Digest || strings.Contains(projected.Value.Text(), "Private second") || !strings.Contains(projected.Value.Text(), got.Pages[0].Citation) {
				t.Fatal("unsafe context projection")
			}
			session, err := runcontext.ContextualSession(t.Context(), runcontext.Session{Purpose: "chat", Prompt: "Read selected document", Model: &runcontext.ModelSession{Family: "gemini", Model: "fixture"}, Context: &runcontext.Input{Documents: []runcontext.Document{projected}}})
			if err != nil {
				t.Fatal("canonical context", err)
			}
			snapshot, err := session.Context.Snapshot("")
			session.Context.Close()
			snapshotRaw, _ := json.Marshal(snapshot)
			if err != nil || bytes.Contains(snapshotRaw, []byte("Private second")) || !bytes.Contains(snapshotRaw, []byte("Selected first page.")) {
				t.Fatal("server context did not retain only selected text", err)
			}
			if !bytes.Equal(raw, original) || strings.Contains(got.Pages[0].Text, "Private second") {
				t.Fatal("original altered or unselected page leaked")
			}
		})
	}
}
func TestAssistantDocumentTamperingAndSelection(t *testing.T) {
	program := assistantDocumentProgram(t)
	valid, pin, ref := assistantDocumentFixture(t, "web", "http", nil)
	for _, tc := range []struct {
		name   string
		change func(*attachmentObject, *assistantDocumentReference, *inputResearchPin)
	}{
		{"result", func(o *attachmentObject, _ *assistantDocumentReference, _ *inputResearchPin) {
			o.Proof.Response = strings.Replace(o.Proof.Response, "Selected first page.", "Injected first page.", 1)
		}},
		{"receipt", func(o *attachmentObject, _ *assistantDocumentReference, _ *inputResearchPin) {
			o.Proof.Response = strings.Replace(o.Proof.Response, `"servedAt":"2026-10-20`, `"servedAt":"2026-10-19`, 1)
		}},
		{"registry", func(o *attachmentObject, _ *assistantDocumentReference, _ *inputResearchPin) {
			o.Proof.Registry = strings.Replace(o.Proof.Registry, `"sealedAt":"2026-10-20`, `"sealedAt":"2026-10-19`, 1)
		}},
		{"unsealed", func(o *attachmentObject, _ *assistantDocumentReference, _ *inputResearchPin) { o.Proof.Registry = "" }},
		{"commitment", func(o *attachmentObject, _ *assistantDocumentReference, _ *inputResearchPin) {
			o.Proof.Web.URL = "https://example.com/other"
		}},
		{"argument-only", func(o *attachmentObject, _ *assistantDocumentReference, _ *inputResearchPin) {
			o.Proof.Web.Site = "https://example.com/scope/"
		}},
		{"original", func(o *attachmentObject, _ *assistantDocumentReference, _ *inputResearchPin) {
			o.Original.Bytes = "b3RoZXI="
			o.Original.SHA256 = "sha256:" + assistantDocumentHash([]byte("other"))
		}},
		{"pin", func(_ *attachmentObject, _ *assistantDocumentReference, p *inputResearchPin) {
			p.Public = strings.Repeat("a", 64)
		}},
		{"authority", func(_ *attachmentObject, _ *assistantDocumentReference, p *inputResearchPin) {
			p.Authority = "gateway:other"
		}},
		{"digest", func(_ *attachmentObject, r *assistantDocumentReference, _ *inputResearchPin) {
			r.Digest = "sha256:" + strings.Repeat("a", 64)
		}},
		{"missing page", func(_ *attachmentObject, r *assistantDocumentReference, _ *inputResearchPin) { r.Pages = []int{3} }},
		{"no pages", func(_ *attachmentObject, r *assistantDocumentReference, _ *inputResearchPin) { r.Pages = nil }},
		{"duplicate pages", func(_ *attachmentObject, r *assistantDocumentReference, _ *inputResearchPin) { r.Pages = []int{1, 1} }},
		{"foreign ID", func(_ *attachmentObject, r *assistantDocumentReference, _ *inputResearchPin) { r.ID = "../outside" }},
		{"encoding", func(o *attachmentObject, _ *assistantDocumentReference, _ *inputResearchPin) {
			o.Original.Bytes += "\n"
		}},
		{"mixed source", func(o *attachmentObject, _ *assistantDocumentReference, _ *inputResearchPin) {
			o.Proof.Resource = &struct {
				Grant      string `json:"grant"`
				ResourceID string `json:"resourceId"`
			}{strings.Repeat("a", 64), "other"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var object attachmentObject
			_ = json.Unmarshal(valid, &object)
			r, p := ref, pin
			tc.change(&object, &r, &p)
			if _, err := verifyAssistantDocument(t.Context(), program, assistantDocumentFixtureJSON(t, object), p, r); err == nil {
				t.Fatal("tampered source accepted")
			}
		})
	}
	for _, raw := range [][]byte{
		[]byte(strings.Replace(string(valid), `"version":1`, `"version":1,"version":1`, 1)),
		[]byte(strings.Replace(string(valid), `"version":1`, `"Version":1`, 1)),
		[]byte(strings.Replace(string(valid), `"version":1`, `"version":1.0`, 1)),
		[]byte(strings.Replace(string(valid), `"name":"report.txt"`, `"name":"\ud800"`, 1)),
		append([]byte{0xff}, valid...), bytes.Repeat([]byte("x"), maxAttachmentObjectBytes+1),
	} {
		if _, err := verifyAssistantDocument(t.Context(), program, raw, pin, ref); err == nil {
			t.Fatal("ambiguous encoding accepted")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := verifyAssistantDocument(ctx, program, valid, pin, ref); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
}

func TestAssistantDocumentSignedContractAndPartialConsent(t *testing.T) {
	program := assistantDocumentProgram(t)
	partial := func(r map[string]any) {
		c := r["content"].(map[string]any)
		c["truncated"] = true
		r["processing"].(map[string]any)["status"] = "partial"
	}
	raw, pin, ref := assistantDocumentFixture(t, "inline", "command", partial)
	if _, err := verifyAssistantDocument(t.Context(), program, raw, pin, ref); err == nil {
		t.Fatal("partial without consent accepted")
	}
	ref.AllowPartial = true
	if got, err := verifyAssistantDocument(t.Context(), program, raw, pin, ref); err != nil || !got.Partial {
		t.Fatal("partial consent lost", err)
	}
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
	}{
		{"page chars", func(r map[string]any) { r["content"].(map[string]any)["chars"] = 41 }},
		{"page status", func(r map[string]any) {
			r["content"].(map[string]any)["pages"].([]any)[0].(map[string]any)["status"] = "failed"
		}},
		{"page bounds", func(r map[string]any) { r["processing"].(map[string]any)["bounds"].(map[string]any)["maxPages"] = 1 }},
		{"text bounds", func(r map[string]any) {
			r["processing"].(map[string]any)["bounds"].(map[string]any)["maxTextBytes"] = 1
		}},
		{"original name", func(r map[string]any) { r["document"].(map[string]any)["name"] = "different.txt" }},
		{"original size", func(r map[string]any) { r["document"].(map[string]any)["size"] = 15 }},
		{"source provenance", func(r map[string]any) {
			r["provenance"].(map[string]any)["source"].(map[string]any)["requestedUrl"] = "https://example.com/other"
		}},
		{"source original", func(r map[string]any) { r["original"].(map[string]any)["bytes"] = "b3RoZXI=" }},
		{"missing unmapped", func(r map[string]any) {
			delete(r["content"].(map[string]any)["pages"].([]any)[0].(map[string]any), "unmapped")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, pin, ref := assistantDocumentFixture(t, "web", "http", tc.change)
			ref.AllowPartial = true
			if _, err := verifyAssistantDocument(t.Context(), program, raw, pin, ref); err == nil {
				t.Fatal("signed inconsistent contract accepted")
			}
		})
	}
	// A correctly signed partial page with unmapped glyphs is never readable.
	raw, pin, ref = assistantDocumentFixture(t, "web-pdf", "http", func(r map[string]any) {
		c := r["content"].(map[string]any)
		p := c["pages"].([]any)[0].(map[string]any)
		p["text"] = "Selected first page�"
		p["chars"] = 20
		p["unmapped"] = 1
	})
	ref.AllowPartial = true
	if _, err := verifyAssistantDocument(t.Context(), program, raw, pin, ref); err == nil {
		t.Fatal("unmapped page accepted")
	}
	ref.Pages = []int{2}
	if got, err := verifyAssistantDocument(t.Context(), program, raw, pin, ref); err != nil || got.Pages[0].Text != "Private second page." || !got.Partial {
		t.Fatal("readable selection in partial source refused", err)
	}
}

func TestAssistantDocumentUnreadablePagesAndOCR(t *testing.T) {
	program := assistantDocumentProgram(t)
	for _, status := range []string{"no-text", "needs-ocr", "failed"} {
		t.Run(status, func(t *testing.T) {
			raw, pin, ref := assistantDocumentFixture(t, "web-pdf", "http", func(r map[string]any) {
				c := r["content"].(map[string]any)
				page := c["pages"].([]any)[0].(map[string]any)
				page["status"] = status
				page["text"] = ""
				page["chars"] = 0
				c["chars"] = 20
				if status != "no-text" {
					page["extraction"] = "none"
					p := r["processing"].(map[string]any)
					p["status"] = "partial"
					code := "ocr-not-run"
					var number any = nil
					if status == "failed" {
						code = "pdf-page-failed"
						number = 1
					}
					p["errors"] = []any{map[string]any{"code": code, "message": "Fixture unavailable page", "page": number}}
				}
			})
			ref.AllowPartial = true
			if _, err := verifyAssistantDocument(t.Context(), program, raw, pin, ref); err == nil {
				t.Fatal("unreadable page accepted")
			}
			ref.Pages = []int{2}
			if got, err := verifyAssistantDocument(t.Context(), program, raw, pin, ref); err != nil || got.Pages[0].Text != "Private second page." {
				t.Fatal("readable page refused", err)
			}
		})
	}
	raw, pin, ref := assistantDocumentFixture(t, "web-pdf", "http", func(r map[string]any) {
		c := r["content"].(map[string]any)
		c["extraction"] = "ocr"
		for _, item := range c["pages"].([]any) {
			item.(map[string]any)["extraction"] = "ocr"
		}
		r["provenance"].(map[string]any)["ocr"] = map[string]any{"program": "ocr-tesseract", "digest": "sha256:" + strings.Repeat("b", 64), "pages": []int{1, 2}}
	})
	if got, err := verifyAssistantDocument(t.Context(), program, raw, pin, ref); err != nil || got.Pages[0].Text != "Selected first page." {
		t.Fatal("signed OCR page refused", err)
	}
}

func assistantDocumentRequest(token string) *http.Request {
	r := httptest.NewRequest("POST", "/api/assistant-runs", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	return r
}
func assistantDocumentSavedSource(t *testing.T, s *Server, token string, start assistantRunStart, ref assistantDocumentReference) assistantRunSource {
	t.Helper()
	saved := policyWorkspaceRead(t, s, token, "/api/conversations")
	var doc conversationDocument
	_ = json.Unmarshal(saved.Content, &doc)
	var chat map[string]any
	_ = json.Unmarshal(doc.Chats[0], &chat)
	chat["documents"] = []any{map[string]any{"id": "file-one", "name": "report.txt", "text": "saved text is never evidence", "document": ref}}
	req := assistantDocumentRequest(token)
	raw := assistantDocumentFixtureJSON(t, map[string]any{"version": 1, "chats": []any{chat}})
	var body any
	_ = json.Unmarshal(raw, &body)
	w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, body)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	saved = policyWorkspaceRead(t, s, token, "/api/conversations")
	_ = json.Unmarshal(saved.Content, &doc)
	start.Revision = digestOf(doc.Chats[0])
	identity, _, err := s.assistantRunIdentity(req)
	if err != nil {
		t.Fatal(err)
	}
	source, err := s.readAssistantRunSource(req, start, identity, s.assistantRuns().project)
	if err != nil {
		t.Fatal(err)
	}
	return source
}
func TestAssistantDocumentAuthenticatedRootAndRecheck(t *testing.T) {
	program := assistantDocumentProgram(t)
	bundle := os.Getenv("JPS_TEST_DOCUMENT_BUNDLE")
	if bundle == "" {
		bundle = os.Getenv("JPS_TEST_GATEWAY_BUNDLE")
	}
	if bundle == "" {
		t.Skip("set JPS_TEST_DOCUMENT_BUNDLE to the pinned Gateway bundle")
	}
	if filepath.Join(bundle, executableName("gateway")) != program {
		t.Fatal("tests must share the same pinned executable")
	}
	s, token, start, _ := assistantProtocolFixture(t, assistantProtocolComplete)
	s.cfg.LocalGatewayBundle = bundle
	raw, pin, ref := assistantDocumentFixture(t, "web", "http", nil)
	writeDeskConfig(t, s, strings.Replace(researchDeskFile("http://127.0.0.1:9"), testSignerPublic, pin.Public, 1))
	source := assistantDocumentSavedSource(t, s, token, start, ref)
	store, err := s.openChatData()
	if err != nil {
		t.Fatal(err)
	}
	defer store.root.Close()
	conversation, err := resolveConversationName(store.root, s.conversationIdentity())
	if err != nil {
		t.Fatal(err)
	}
	name := attachmentProjectPrefix(conversation) + ref.ID + ".json"
	// A matching packet in another project's namespace cannot satisfy this read.
	if err = writePrivateData(store.root, "attachment-"+strings.Repeat("a", 64)+"-"+ref.ID+".json", raw); err != nil {
		t.Fatal(err)
	}
	if _, err = s.readAssistantDocument(assistantDocumentRequest(token), source, ref); err == nil {
		t.Fatal("foreign attachment accepted")
	}
	if err = writePrivateData(store.root, name, raw); err != nil {
		t.Fatal(err)
	}
	held, err := s.readAssistantDocument(assistantDocumentRequest(token), source, ref)
	if err != nil {
		t.Fatal("authenticated source", err)
	}
	if err = s.recheckAssistantDocument(assistantDocumentRequest(token), held); err != nil {
		t.Fatal(err)
	}
	activeSource := source
	activeSource.Binding.Unit = "model-requests"
	activeSource.Binding.ExecutionKey = "host-assigned-run-key"
	if _, err = s.readAssistantDocument(assistantDocumentRequest(token), activeSource, ref); err != nil {
		t.Fatal("admitted execution source refused", err)
	}
	projected := held.contextDocument()
	if strings.Contains(projected.Value.Text(), "Private second page.") || strings.Contains(projected.Value.Text(), "saved text is never evidence") || !strings.Contains(projected.Value.Text(), "Selected first page.") {
		t.Fatal("rooted context projection exposed unauthorized text")
	}

	if _, err = s.readAssistantDocument(assistantDocumentRequest(""), source, ref); err == nil {
		t.Fatal("unauthenticated reader accepted")
	}
	if _, err = s.readAssistantDocument(assistantDocumentRequest(policyTestSession(t, s)), source, ref); err == nil {
		t.Fatal("different authenticated session accepted")
	}
	alteredRequest := source
	alteredRequest.Request = []byte(`{"text":"different saved request"}`)
	if _, err = s.readAssistantDocument(assistantDocumentRequest(token), alteredRequest, ref); err == nil {
		t.Fatal("substituted saved request accepted")
	}
	other := ref
	other.Pages = []int{2}
	if _, err = s.readAssistantDocument(assistantDocumentRequest(token), source, other); err == nil {
		t.Fatal("unsaved selection accepted")
	}
	if err = writePrivateData(store.root, name, append(raw, ' ')); err != nil {
		t.Fatal(err)
	}
	if err = s.recheckAssistantDocument(assistantDocumentRequest(token), held); err == nil {
		t.Fatal("changed attachment accepted")
	}
	if err = writePrivateData(store.root, name, raw); err != nil {
		t.Fatal(err)
	}
	writeDeskConfig(t, s, strings.Replace(researchDeskFile("http://127.0.0.1:9"), "gateway:test", "gateway:changed", 1))
	if err = s.recheckAssistantDocument(assistantDocumentRequest(token), held); err == nil {
		t.Fatal("changed trust accepted")
	}
	writeDeskConfig(t, s, strings.Replace(researchDeskFile("http://127.0.0.1:9"), testSignerPublic, pin.Public, 1))
	writeDeskConfig(t, s, strings.Replace(researchDeskFile("http://127.0.0.1:9"), testSignerPublic, strings.Repeat("a", 64), 1))
	if err = s.recheckAssistantDocument(assistantDocumentRequest(token), held); err == nil {
		t.Fatal("changed signer accepted")
	}
	writeDeskConfig(t, s, strings.Replace(researchDeskFile("http://127.0.0.1:9"), testSignerPublic, pin.Public, 1))
	_ = assistantDocumentSavedSource(t, s, token, source.Start, other)
	if err = s.recheckAssistantDocument(assistantDocumentRequest(token), held); err == nil {
		t.Fatal("changed saved owner accepted")
	}
}

// The subprocess is this explicitly supplied test binary; PATH supplies no
// installed helper or application. It never contacts a network or model.
func TestAssistantDocumentCommandHelper(t *testing.T) {
	switch os.Getenv("JPS_DOCUMENT_COMMAND_HELPER") {
	case "large":
		_, _ = os.Stdout.Write(bytes.Repeat([]byte("x"), 1024))
		os.Exit(0)
	case "wait":
		time.Sleep(time.Minute)
		os.Exit(0)
	default:
		t.Skip("subprocess fixture only")
	}
}
func TestAssistantDocumentCommandBoundsAndCancellation(t *testing.T) {
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("JPS_DOCUMENT_COMMAND_HELPER", "large")
	if _, err = assistantDocumentCommand(t.Context(), program, nil, 4, "-test.run=^TestAssistantDocumentCommandHelper$"); err == nil {
		t.Fatal("oversized subprocess output accepted")
	}
	t.Setenv("JPS_DOCUMENT_COMMAND_HELPER", "wait")
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err = assistantDocumentCommand(ctx, program, nil, 1024, "-test.run=^TestAssistantDocumentCommandHelper$"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("in-flight subprocess cancellation lost", err)
	}
}
