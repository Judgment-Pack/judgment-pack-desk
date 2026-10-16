package desk

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func publicationCall(t *testing.T, s *Server, id string, req dialectPublicationRequest) (int, []byte) {
	t.Helper()
	raw, _ := json.Marshal(req)
	r := httptest.NewRequest("POST", "/api/input-preparations/"+id+"/publications", strings.NewReader(string(raw)))
	r.Header.Set("Authorization", "Bearer "+testToken)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w.Code, w.Body.Bytes()
}
func checkAndPublishDialect(t *testing.T, s *Server, saved inputPreparationReply, variant, version string) dialectPublicationAnswer {
	t.Helper()
	req := dialectPublicationRequest{Action: "check", Revision: saved.SHA256, Variant: variant, Version: version}
	code, raw := publicationCall(t, s, saved.Document.ID, req)
	var checked dialectPublicationAnswer
	if code != 200 || json.Unmarshal(raw, &checked) != nil {
		t.Fatalf("check %d %s", code, raw)
	}
	req.Action = "publish"
	req.CheckDigest = checked.Digest
	req.Reviewed = true
	code, raw = publicationCall(t, s, saved.Document.ID, req)
	if code != 200 || json.Unmarshal(raw, &checked) != nil || !checked.Saved {
		t.Fatalf("publish %d %s", code, raw)
	}
	return checked
}
func TestDialectPublicationIdentityAndRecovery(t *testing.T) {
	requireBinary(t)
	s, _ := newTestServer(t, false)
	ctx := context.Background()
	doc := repositoryDocument(91)
	doc.Recipe.Qualification = qualificationFixture()
	saved, err := s.preparationRepository().CompareAndSwap(ctx, doc, "")
	if err != nil {
		t.Fatal(err)
	}
	original := saved.SHA256
	p := checkAndPublishDialect(t, s, saved, "asserted", "1.0.0")
	if p.Digest != "sha256:"+digestOf([]byte(p.Document)) || !strings.Contains(p.Document, `"formatVersion":"2.0.0"`) || strings.Contains(p.Document, `"provider"`) {
		t.Fatal(p)
	}
	after, err := s.preparationRepository().Get(ctx, doc.ID)
	if err != nil || after.SHA256 != original {
		t.Fatal("publication mutated recipe", err)
	}
	// A completed retry reconciles from retained bytes even with Runtime down.
	bin := s.cfg.JpackBin
	s.cfg.JpackBin = "/no-runtime"
	req := dialectPublicationRequest{Action: "publish", Revision: saved.SHA256, Variant: "asserted", Version: "1.0.0", Reviewed: true, CheckDigest: p.Digest}
	code, raw := publicationCall(t, s, doc.ID, req)
	if code != 200 || !strings.Contains(string(raw), p.Publication.ID) {
		t.Fatalf("retry %d %s", code, raw)
	}
	s.cfg.JpackBin = bin
	// Unrelated record edits don't create another immutable release.
	doc.Records[0].Values["count"] = "456"
	saved, err = s.preparationRepository().CompareAndSwap(ctx, doc, saved.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	same := checkAndPublishDialect(t, s, saved, "asserted", "1.0.0")
	if same.Publication.ID != p.Publication.ID {
		t.Fatal("record update republished unchanged artifact")
	}
	// Changed local conversion/recipe requires a fresh review, not a silent latest lookup.
	doc.Recipe.Intent = "Changed research task"
	saved, err = s.preparationRepository().CompareAndSwap(ctx, doc, saved.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.publishedExecutable(ctx, doc, "asserted"); err == nil {
		t.Fatal("changed recipe executed without review")
	}
	req.Revision = original
	code, _ = publicationCall(t, s, doc.ID, dialectPublicationRequest{Action: "check", Revision: original, Variant: "asserted", Version: "1.0.1"})
	if code != 409 {
		t.Fatal("stale review accepted", code)
	}
	p2 := checkAndPublishDialect(t, s, saved, "asserted", "1.0.1")
	if p2.Digest == p.Digest {
		t.Fatal("version missing from digest")
	}
	code, raw = publicationCall(t, s, doc.ID, dialectPublicationRequest{Action: "check", Revision: saved.SHA256, Variant: "asserted", Version: "0.9.0"})
	if code != 409 {
		t.Fatalf("backwards check %d %s", code, raw)
	}
	// Published content cannot be changed while reusing the identity and version.
	doc.Recipe.Qualification.Mapping.Case.Facts[0].Target = "/company/region"
	doc.Recipe.Qualification.Pack = strings.ReplaceAll(doc.Recipe.Qualification.Pack, "/company/country", "/company/region")
	doc.Recipe.Qualification.Digest = sha256Digest([]byte(doc.Recipe.Qualification.Pack))
	saved, err = s.preparationRepository().CompareAndSwap(ctx, doc, saved.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	code, raw = publicationCall(t, s, doc.ID, dialectPublicationRequest{Action: "check", Revision: saved.SHA256, Variant: "asserted", Version: "1.0.1"})
	if code != 409 {
		t.Fatalf("identity reused %d %s", code, raw)
	}
}
func TestDialectPublicationGeneratedOrigin(t *testing.T) {
	requireBinary(t)
	s, _ := newTestServer(t, false)
	doc := repositoryDocument(92)
	doc.Recipe.Qualification = qualificationFixture()
	saved, err := s.preparationRepository().CompareAndSwap(context.Background(), doc, "")
	if err != nil {
		t.Fatal(err)
	}
	p := checkAndPublishDialect(t, s, saved, "generated", "1.0.0")
	var m map[string]any
	if json.Unmarshal([]byte(p.Document), &m) != nil {
		t.Fatal(p.Document)
	}
	if m["formatVersion"] != "3.0.0" || m["admits"] == nil || m["sources"] == nil || m["provider"] != nil {
		t.Fatal("generated provenance lost", p.Document)
	}
	if len(m["case"].(map[string]any)["facts"].([]any)) != 0 {
		t.Fatal("generated values became case assertions")
	}
	req := dialectPublicationRequest{Action: "publish", Revision: saved.SHA256, Variant: "generated", Version: "1.0.1"}
	code, _ := publicationCall(t, s, doc.ID, req)
	if code != 400 {
		t.Fatal("unchecked publish", code)
	}
	req.Reviewed = true
	req.CheckDigest = "sha256:wrong"
	code, _ = publicationCall(t, s, doc.ID, req)
	if code != 409 {
		t.Fatal("changed digest published", code)
	}
}
func TestPublishedReleaseChecksCanonicalCommitment(t *testing.T) {
	// JSON transport escaping changes framing, never the authoritative digest.
	mapping := json.RawMessage(`{"formatVersion":"2.0.0","id":"urn:test:a&b","version":"1.0.0","case":{"facts":[],"evidence":[]}}`)
	digest := "sha256:" + digestOf(mapping)
	raw, _ := json.Marshal(map[string]any{"source": map[string]any{"mapping": mapping, "mappingDigest": digest}})
	pin := &struct{ ID, Version, Digest string }{"urn:test:a&b", "1.0.0", digest}
	if err := verifyPublishedRelease(raw, mapping, pin); err != nil {
		t.Fatal(err)
	}
	pin.Digest = "sha256:wrong"
	if verifyPublishedRelease(raw, mapping, pin) == nil {
		t.Fatal("wrong published pin accepted")
	}
	pin.Digest = digest
	if verifyPublishedRelease(raw, json.RawMessage(`{}`), pin) == nil {
		t.Fatal("wrong executable mapping accepted")
	}
	imported, _ := json.Marshal(map[string]any{"source": map[string]any{"mapping": mapping, "mappingDigest": "sha256:bound", "import": map[string]any{"dialect": mapping, "binding": map[string]any{"dialect": digest}}}})
	if err := verifyPublishedRelease(imported, mapping, pin); err != nil {
		t.Fatal("bound digest confused with publication", err)
	}
}

func TestDialectPublicationHistoryPagesAndVariants(t *testing.T) {
	requireBinary(t)
	s, _ := newTestServer(t, false)
	doc := repositoryDocument(93)
	doc.Recipe.Qualification = qualificationFixture()
	saved, err := s.preparationRepository().CompareAndSwap(context.Background(), doc, "")
	if err != nil {
		t.Fatal(err)
	}
	first := checkAndPublishDialect(t, s, saved, "asserted", "1.0.0")
	checkAndPublishDialect(t, s, saved, "generated", "1.0.0")
	latest := checkAndPublishDialect(t, s, saved, "asserted", "1.0.1")
	for _, tc := range []struct{ query, id string }{{"?variant=asserted", latest.Publication.ID}, {"?variant=asserted&before=3", first.Publication.ID}, {"?variant=generated", ""}} {
		r := httptest.NewRequest("GET", "/api/input-preparations/"+doc.ID+"/publications"+tc.query, nil)
		r.Header.Set("Authorization", "Bearer "+testToken)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		var reply struct {
			Items []dialectPublicationAnswer `json:"items"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &reply) != nil || len(reply.Items) == 0 {
			t.Fatalf("history %d %s", w.Code, w.Body.String())
		}
		if tc.id != "" && reply.Items[0].Publication.ID != tc.id {
			t.Fatal("wrong history page", tc.query)
		}
		if tc.id == "" && (len(reply.Items) != 1 || reply.Items[0].Publication.Dialect.Variant != "generated") {
			t.Fatal("variant crossed")
		}
	}
}
