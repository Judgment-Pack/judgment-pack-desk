package desk

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// requireStorageLine skips a test that exercises the storage line unless the
// process runs it (JPACK_DESK_STORAGE_LINE=on, the storage lane).
func requireStorageLine(t testing.TB) {
	t.Helper()
	if os.Getenv(storageLineEnv) != "on" {
		t.Skip("the storage line is off; this runs in the storage lane (" + storageLineEnv + "=on)")
	}
}

// storageLineOn says whether this process runs the storage line, for a test
// whose v0.13.2 part runs either way and whose storage part runs only on.
func storageLineOn() bool { return os.Getenv(storageLineEnv) == "on" }

// Off, Desk keeps v0.13.2's storage: no enrollment, census or admission, chat
// history where v0.13.2 keeps it, no storage-line files, and a second Desk on
// the same installation starts beside the first.
func TestStorageLineOffKeepsV0132Storage(t *testing.T) {
	t.Setenv(storageLineEnv, "")
	config := t.TempDir()
	s, ts, _ := assistantServerIn(t, config)
	if s.storageLine || s.storageEnrollment != nil || s.storageConstruction != nil || s.storageInfrastructure != nil || s.storageReadOnly {
		t.Fatalf("storage line ran while off: line=%v enrollment=%v readOnly=%v problem=%v", s.storageLine, s.storageEnrollment != nil, s.storageReadOnly, s.storageProblem)
	}
	status, initial := chatRequest(t, ts, "GET", "", "", true, "")
	if status != 200 {
		t.Fatal(status)
	}
	data := `{"version":1,"chats":[{"id":"chat-one","composer":"kept"}]}`
	if status, _ = chatRequest(t, ts, "PUT", initial.SHA256, data, true, ""); status != 200 {
		t.Fatal("save refused", status)
	}
	if _, err := os.Stat(filepath.Join(s.defaultChatDataPath(), s.conversationName())); err != nil {
		t.Fatal("history is not where v0.13.2 keeps it", err)
	}
	for _, name := range []string{storageRecordName, storageClaimName, storageReadyName, storageAnchorName} {
		if _, err := os.Lstat(filepath.Join(config, name)); !os.IsNotExist(err) {
			t.Fatalf("storage-line file %s made while off: %v", name, err)
		}
	}
	// The page reads the line as off, and the line's own routes refuse.
	code, raw := deskCall(t, ts, "GET", "/api/storage/status", "", "", true)
	var answer map[string]any
	if code != 200 || json.Unmarshal(raw, &answer) != nil || answer["line"] != "off" || answer["version"] != float64(1) || len(answer) != 3 {
		t.Fatalf("storage status with the line off: %d %s", code, raw)
	}
	for _, path := range []string{"/api/storage/policy", "/api/storage/retention"} {
		if code, raw = deskCall(t, ts, "GET", path, "", "", true); code != 404 {
			t.Fatalf("%s with the line off: %d %s", path, code, raw)
		}
	}
	other, ts2, _ := assistantServerIn(t, config)
	if other.storageReadOnly {
		t.Fatal("second Desk read only", other.storageProblem)
	}
	if status, _ = chatRequest(t, ts2, "GET", "", "", true, ""); status != 200 {
		t.Fatal("second Desk refused", status)
	}
}

// A development installation the candidate enrolled still opens with the
// storage line off, and its chats read and save.
func TestStorageLineOnThenOffOpensChats(t *testing.T) {
	config, project := t.TempDir(), t.TempDir()
	t.Setenv(storageLineEnv, "on")
	s, ts, _ := assistantServerAt(t, project, config)
	if !s.storageLine || s.storageEnrollment == nil {
		t.Skip("storage line did not enroll here", s.storageProblem)
	}
	_, initial := chatRequest(t, ts, "GET", "", "", true, "")
	data := `{"version":1,"chats":[{"id":"chat-one","composer":"enrolled"}]}`
	status, landed := chatRequest(t, ts, "PUT", initial.SHA256, data, true, "")
	if status != 200 {
		t.Fatal("enrolled save refused", status)
	}
	ts.Close()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv(storageLineEnv, "")
	off, ts2, _ := assistantServerAt(t, project, config)
	if off.storageLine || off.storageReadOnly {
		t.Fatal("off start", off.storageProblem)
	}
	status, read := chatRequest(t, ts2, "GET", "", "", true, "")
	if status != 200 || string(read.Content) != string(landed.Content) {
		t.Fatalf("enrolled history not read with the line off: %d %q", status, read.Content)
	}
	if status, _ = chatRequest(t, ts2, "PUT", read.SHA256, `{"version":1,"chats":[]}`, true, ""); status != 200 {
		t.Fatal("save refused with the line off", status)
	}
}
