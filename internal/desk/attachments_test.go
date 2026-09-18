package desk

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAttachmentUploadLeavesRoomForChatAndBackup(t *testing.T) {
	s, ts, _ := assistantServer(t)
	enableTestDocuments(t, s)
	store, err := s.openChatData()
	if err != nil {
		t.Fatal(err)
	}
	defer store.root.Close()
	entries, err := storageEntries(store.root)
	if err != nil {
		t.Fatal(err)
	}
	for i := len(entries); i < maxStorageFiles-4; i++ {
		if err := writePrivateData(store.root, fmt.Sprintf("conversations-%064x.json", i), []byte(`{"version":1,"chats":[]}`)); err != nil {
			t.Fatal(err)
		}
	}
	status, _, _ := documentRequest(t, ts, "PUT", testAttachmentID, "absent", documentBody(t, "private bytes"))
	if status != 413 {
		t.Fatalf("full store accepted upload: %d", status)
	}
	entries, err = storageEntries(store.root)
	if err != nil || len(entries) != maxStorageFiles-4 {
		t.Fatalf("store became unreadable: %d %v", len(entries), err)
	}
	archive, err := os.CreateTemp(t.TempDir(), "backup-")
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	if err := buildChatBackup(context.Background(), store, archive); err != nil {
		t.Fatal(err)
	}
}

const testAttachmentID = "12345678-1234-1234-1234-123456789abc"

func documentRequest(t *testing.T, ts *httptest.Server, method, id, digest, body string) (int, string, []byte) {
	t.Helper()
	req, _ := http.NewRequest(method, ts.URL+"/api/attachments/"+id, strings.NewReader(body))
	bearer(req)
	req.Header.Set("If-Match", digest)
	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	var reply struct {
		SHA256 string `json:"sha256"`
	}
	_ = json.Unmarshal(data, &reply)
	return res.StatusCode, reply.SHA256, data
}
func documentBody(t *testing.T, original string) string {
	t.Helper()
	data, err := json.Marshal(attachmentObject{Version: 1, Original: attachmentOriginal{Name: "policy.pdf", MediaType: "application/pdf", Bytes: base64.StdEncoding.EncodeToString([]byte(original)), SHA256: "sha256:" + digestOf([]byte(original))}})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
func TestAttachmentCustodyRetentionAndBackup(t *testing.T) {
	s, ts, log := assistantServer(t)
	enableTestDocuments(t, s)
	body := documentBody(t, "%PDF-1.4 original private bytes")
	status, digest, _ := documentRequest(t, ts, "PUT", testAttachmentID, "absent", body)
	if status != 200 || digest != digestOf([]byte(body)) {
		t.Fatalf("save %d %s", status, digest)
	}
	status, _, read := documentRequest(t, ts, "GET", testAttachmentID, "", "")
	if status != 200 || string(read) != body {
		t.Fatalf("read %d", status)
	}
	for _, bad := range []struct{ match, body string }{{"absent", body}, {"", body}, {digest, documentBody(t, "another original")}} {
		status, _, _ = documentRequest(t, ts, "PUT", testAttachmentID, bad.match, bad.body)
		if status != 409 {
			t.Fatalf("mutable original: %d", status)
		}
	}
	store, err := s.openChatData()
	if err != nil {
		t.Fatal(err)
	}
	defer store.root.Close()
	entries, err := storageEntries(store.root)
	if err != nil {
		t.Fatal(err)
	}
	var filename string
	for _, entry := range entries {
		if attachmentFileName.MatchString(entry.Name()) {
			filename = entry.Name()
		}
	}
	if filename == "" {
		t.Fatal("no attachment retained")
	}
	usage, err := s.describeStorage(store)
	if err != nil || usage.ProjectBytes != int64(len(body)) {
		t.Fatalf("project storage omitted document bytes: %d %v", usage.ProjectBytes, err)
	}
	if _, err := os.Stat(filepath.Join(s.projectDir, filename)); !os.IsNotExist(err) {
		t.Fatal("original in project")
	}
	archive, err := os.CreateTemp(t.TempDir(), "backup-")
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	if err = buildChatBackup(context.Background(), store, archive); err != nil {
		t.Fatal(err)
	}
	_, manifest, err := validateChatBackup(archive)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, file := range manifest.Files {
		if file.Name == filename {
			found = true
		}
	}
	if !found {
		t.Fatal("original omitted from backup")
	}
	if strings.Contains(log.String(), "private bytes") {
		t.Fatal("original leaked in log")
	}
	status, _ = chatRequest(t, ts, "GET", "", "", true, "")
	if status != 200 {
		t.Fatal("chat storage broke")
	}
}
func TestAttachmentIsolationAndInvalidBytes(t *testing.T) {
	shared := t.TempDir()
	first, a, _ := assistantServerIn(t, shared)
	enableTestDocuments(t, first)
	_, b, _ := assistantServerIn(t, shared)
	body := documentBody(t, "original")
	status, _, _ := documentRequest(t, a, "PUT", testAttachmentID, "absent", body)
	if status != 200 {
		t.Fatal(status)
	}
	status, _, _ = documentRequest(t, b, "GET", testAttachmentID, "", "")
	if status != 404 {
		t.Fatal("cross-project read", status)
	}
	for _, bad := range []string{strings.Replace(body, "sha256:", "sha256:0", 1), strings.Replace(body, "b3JpZ2luYWw=", "not base64", 1), `{"version":1}`, `null`} {
		status, _, _ = documentRequest(t, a, "PUT", testAttachmentID, "absent", bad)
		if status != 400 {
			t.Fatalf("invalid accepted: %d", status)
		}
	}
	req, _ := http.NewRequest("GET", a.URL+"/api/attachments/"+testAttachmentID, nil)
	res, err := a.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatal(res.StatusCode)
	}
	req, _ = http.NewRequest("PUT", a.URL+"/api/attachments/"+testAttachmentID, bytes.NewBufferString(body))
	bearer(req)
	req.Header.Set("Origin", "https://foreign.example")
	res, err = a.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatal(res.StatusCode)
	}
}

func TestDocumentConfigurationConditionalWrite(t *testing.T) {
	s, ts, _ := assistantServer(t)
	original := `{"deskConfigVersion":1,"user":{"displayName":"Keep this"}}`
	writeDeskConfig(t, s, original)
	value := `{"research":{"documents":{"source":"documents"}},"ifMatch":"` + digestOf([]byte(original)) + `"}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/desk-config", strings.NewReader(value))
	bearer(req)
	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		data, _ := io.ReadAll(res.Body)
		t.Fatalf("save %d %s", res.StatusCode, data)
	}
	_, landed, err := s.readDeskFile()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(landed, []byte(`"user": {"displayName":"Keep this"}`)) {
		t.Fatal("unrelated configuration changed")
	}
	decoded := decodeDeskFile(landed)
	if decoded.refused() || decoded.Research.documents.maxRequestBytes != 32<<20 {
		t.Fatalf("bad document defaults: %+v", decoded.Problems)
	}
	req, _ = http.NewRequest("PUT", ts.URL+"/api/desk-config", strings.NewReader(value))
	bearer(req)
	res, err = ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 409 {
		t.Fatal("stale write accepted", res.StatusCode)
	}
}

func TestAttachmentBackupRestoresOriginalAndProofAndRefusesSymlinks(t *testing.T) {
	s, ts, _ := assistantServer(t)
	enableTestDocuments(t, s)
	body := documentBody(t, "retained source")
	status, digest, _ := documentRequest(t, ts, "PUT", testAttachmentID, "absent", body)
	if status != 200 {
		t.Fatal(status)
	}
	var object attachmentObject
	_ = json.Unmarshal([]byte(body), &object)
	object.Proof = &attachmentProof{Session: "test", Source: "documents", Authority: "gateway:test", PublicKey: strings.Repeat("ab", 32), Response: `{"result":{},"receipt":{}}`, Registry: "test registry bytes"}
	data, _ := json.Marshal(object)
	status, digest, _ = documentRequest(t, ts, "PUT", testAttachmentID, digest, string(data))
	if status != 200 {
		t.Fatal(status)
	}
	status, _, _ = documentRequest(t, ts, "PUT", testAttachmentID, digest, string(data))
	if status != 409 {
		t.Fatal("completed proof replaced", status)
	}
	store, err := s.openChatData()
	if err != nil {
		t.Fatal(err)
	}
	defer store.root.Close()
	archive, err := os.CreateTemp(t.TempDir(), "backup-")
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	if err = buildChatBackup(context.Background(), store, archive); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	if err = os.Chmod(dest, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	target := &chatDataStore{root: root}
	copied := []string{}
	if err = copyChatBackup(context.Background(), archive, target, &copied); err != nil {
		t.Fatal(err)
	}
	var name string
	for _, item := range copied {
		if attachmentFileName.MatchString(item) {
			name = item
		}
	}
	restored, err := readPrivateData(root, name, maxAttachmentObjectBytes)
	if err != nil || !bytes.Equal(restored, data) {
		t.Fatal("backup lost document", err)
	}
	if err = store.root.Remove(name); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(filepath.Join(dest, name), filepath.Join(store.location.Path, name)); err != nil {
		t.Skip("symlinks unavailable", err)
	}
	status, _, _ = documentRequest(t, ts, "GET", testAttachmentID, "", "")
	if status == 200 {
		t.Fatal("symlink attachment read")
	}
}

func enableTestDocuments(t *testing.T, s *Server) {
	t.Helper()
	var config map[string]any
	_ = json.Unmarshal([]byte(researchDeskFile("http://127.0.0.1:8787")), &config)
	config["research"].(map[string]any)["documents"] = map[string]any{"source": "documents"}
	data, _ := json.Marshal(config)
	writeDeskConfig(t, s, string(data))
}
func TestAttachmentPersonalUploadLimit(t *testing.T) {
	s, ts, _ := assistantServer(t)
	body := documentBody(t, "too many bytes")
	status, _, _ := documentRequest(t, ts, "PUT", testAttachmentID, "absent", body)
	if status != 409 {
		t.Fatal("upload before configuration", status)
	}
	enableTestDocuments(t, s)
	_, data, _ := s.readDeskFile()
	var config map[string]any
	_ = json.Unmarshal(data, &config)
	config["research"].(map[string]any)["documents"].(map[string]any)["maxFileBytes"] = 1
	data, _ = json.Marshal(config)
	writeDeskConfig(t, s, string(data))
	status, _, _ = documentRequest(t, ts, "PUT", testAttachmentID, "absent", body)
	if status != 413 {
		t.Fatal("configured original limit not enforced", status)
	}
}
