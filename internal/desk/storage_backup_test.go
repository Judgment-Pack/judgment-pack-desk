package desk

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func backupBytes(t *testing.T, ts *httptest.Server) []byte {
	t.Helper()
	req, _ := http.NewRequest("GET", ts.URL+"/api/storage/backup", nil)
	bearer(req)
	response, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 {
		t.Fatalf("backup: %d %s", response.StatusCode, data)
	}
	if response.Header.Get("Content-Type") != "application/zip" {
		t.Fatal("not a ZIP download")
	}
	return data
}
func restoreBackup(t *testing.T, ts *httptest.Server, request moveChatData, backup []byte) (int, string) {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	settings, _ := json.Marshal(request)
	form.WriteField("settings", string(settings))
	part, _ := form.CreateFormFile("backup", "backup.zip")
	part.Write(backup)
	form.Close()
	req, _ := http.NewRequest("POST", ts.URL+"/api/storage/restore", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	bearer(req)
	response, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	return response.StatusCode, string(data)
}
func makeBackup(t *testing.T, manifest backupManifest, extraNames ...string) []byte {
	t.Helper()
	var body bytes.Buffer
	archive := zip.NewWriter(&body)
	for _, entry := range manifest.Files {
		file, err := archive.Create(entry.Name)
		if err != nil {
			t.Fatal(err)
		}
		file.Write([]byte(storageChat))
	}
	for _, name := range extraNames {
		file, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		file.Write([]byte("unexpected"))
	}
	file, _ := archive.Create("manifest.json")
	data, _ := json.Marshal(manifest)
	file.Write(data)
	archive.Close()
	return body.Bytes()
}
func TestStorageBackupRestoreCleanProfile(t *testing.T) {
	original, ots, _ := assistantServer(t)
	privateFixture(t, filepath.Join(original.configDir, original.conversationName()), storageChat)
	privateFixture(t, filepath.Join(original.configDir, "secrets", "sensitive"), "secret must stay here")
	before := storageGet(t, ots)
	if !before.Legacy {
		t.Fatal("test must exercise credential exclusion from legacy root")
	}
	backup := backupBytes(t, ots)
	if bytes.Contains(backup, []byte("secret must stay here")) {
		t.Fatal("backup leaked credential")
	}
	target := filepath.Join(t.TempDir(), "restored")
	config := original.cfg
	config.DeskConfigDir = t.TempDir()
	restored, rts := startDesk(t, config)
	defer restored.Close()
	defer rts.Close()
	empty := storageGet(t, rts)
	code, body := restoreBackup(t, rts, moveChatData{Path: target, Revision: empty.Revision}, backup)
	if code != 200 {
		t.Fatalf("restore: %d %s", code, body)
	}
	if code, reply := chatRequest(t, rts, "GET", "", "", true, ""); code != 200 || string(reply.Content) != storageChat {
		t.Fatal("restored history differs", code, string(reply.Content))
	}
	mustBytes(t, filepath.Join(target, original.conversationName()), storageChat)
	if info, err := os.Stat(filepath.Join(target, original.conversationName())); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("restored permissions", err)
	}
	if status := storageGet(t, rts); status.PreviousPath != empty.Path {
		t.Fatal("original store not retained")
	}
	if _, err := os.Stat(filepath.Join(target, "secrets")); !os.IsNotExist(err) {
		t.Fatal("restored secrets")
	}
}
func TestStorageBackupRejectsCorruptionAndNeverActivates(t *testing.T) {
	for _, kind := range []string{"version", "checksum", "size", "traversal", "duplicate", "extra", "count", "too-many", "directory-bomb", "huge-entry", "bad-zip", "stale"} {
		t.Run(kind, func(t *testing.T) {
			s, ts, _ := assistantServer(t)
			chatRequest(t, ts, "PUT", "absent", storageChat, true, "")
			before := storageGet(t, ts)
			entry := backupEntry{Name: s.conversationName(), Bytes: int64(len(storageChat)), SHA256: digestOf([]byte(storageChat))}
			manifest := backupManifest{Version: 1, Files: []backupEntry{entry}}
			extra := []string{}
			switch kind {
			case "version":
				manifest.Version = 2
			case "checksum":
				manifest.Files[0].SHA256 = strings.Repeat("0", 64)
			case "size":
				manifest.Files[0].Bytes++
			case "traversal":
				manifest.Files[0].Name = "../outside"
			case "duplicate":
				extra = append(extra, entry.Name)
			case "extra":
				extra = append(extra, "secrets/assistant")
			case "huge-entry":
				manifest.Files[0].Bytes = maxConversationBytes + 1
			}
			backup := makeBackup(t, manifest, extra...)
			switch kind {
			case "count":
				binary.LittleEndian.PutUint16(backup[len(backup)-12:len(backup)-10], 3)
			case "too-many":
				binary.LittleEndian.PutUint16(backup[len(backup)-12:len(backup)-10], 65535)
			case "directory-bomb":
				binary.LittleEndian.PutUint32(backup[len(backup)-10:len(backup)-6], 128<<20)
			case "bad-zip":
				backup = []byte("not a backup")
			}
			revision := before.Revision
			if kind == "stale" {
				revision = "stale"
			}
			target := filepath.Join(t.TempDir(), "target")
			code, body := restoreBackup(t, ts, moveChatData{Path: target, Revision: revision}, backup)
			if code == 200 {
				t.Fatalf("%s accepted: %s", kind, body)
			}
			if current := storageGet(t, ts); current.Path != before.Path {
				t.Fatal("bad restore activated")
			}
			mustBytes(t, filepath.Join(before.Path, s.conversationName()), storageChat)
			if _, err := os.Stat(filepath.Join(target, s.conversationName())); !os.IsNotExist(err) {
				t.Fatal("failed restore left copied history", err)
			}
		})
	}
}
func TestStorageTransferLockAndGuards(t *testing.T) {
	s, ts, _ := assistantServer(t)
	for _, route := range []struct{ method, path string }{{"GET", "/api/storage/backup"}, {"POST", "/api/storage/restore"}} {
		for _, origin := range []string{"", "https://foreign.example"} {
			req, _ := http.NewRequest(route.method, ts.URL+route.path, nil)
			if origin != "" {
				bearer(req)
				req.Header.Set("Origin", origin)
			}
			response, err := ts.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			want := 401
			if origin != "" {
				want = 403
			}
			if response.StatusCode != want {
				t.Fatal(route.path, response.StatusCode)
			}
		}
	}
	lock, err := s.privateDataFileLock(".data-transfer.lock", true)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	for _, route := range []struct{ method, path string }{{"GET", "/api/storage/backup"}, {"POST", "/api/storage/restore"}} {
		req, _ := http.NewRequest(route.method, ts.URL+route.path, nil)
		bearer(req)
		response, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 409 {
			t.Fatal("overlapping data transfer accepted", route.path, response.StatusCode)
		}
	}
}
