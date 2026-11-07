package desk

import (
	"net/http"
	"strings"
	"testing"
)

// **A named desk outside a Desk home works as in v0.13.2.** It is a child of
// the installation: it starts writable, with its Runner configured, saves and
// reads its own chats and files, and its storage status answers. It never
// borrows the installation from a folder the desks folder does not name.
func TestANamedDeskOutsideADeskHomeHasStorage(t *testing.T) {
	s, ts, _ := assistantServer(t)
	named := createTestDesk(t, ts, "Named")
	child := s.desks[named.ID]
	if child == nil || child.storageReadOnly {
		t.Fatalf("the named desk started read only: %v", child.storageProblem)
	}
	put := func(body string) int {
		r, _ := http.NewRequest("PUT", ts.URL+"/api/conversations", strings.NewReader(body))
		bearer(r)
		r.Header.Set("X-Jpack-Desk", named.ID)
		r.Header.Set("If-Match", "absent")
		response, err := ts.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		return response.StatusCode
	}
	if code := put(`{"version":1,"chats":[{"id":"named-chat"}]}`); code != 200 {
		t.Fatalf("the named desk could not save a chat: %d", code)
	}
	if code, data := deskCall(t, ts, "GET", "/api/conversations", named.ID, "", true); code != 200 || !strings.Contains(string(data), "named-chat") {
		t.Fatalf("the named desk lost its chat: %d %s", code, data)
	}
	if code, data := deskCall(t, ts, "GET", "/api/conversations", "", "", true); code != 200 || strings.Contains(string(data), "named-chat") {
		t.Fatalf("the named desk's chat reached the project: %d %s", code, data)
	}
	if code, data := deskCall(t, ts, "PUT", "/api/file", named.ID, `{"path":"notes.md","content":"named","baseSha256":"","createParents":true}`, true); code != 200 && code != 201 {
		t.Fatalf("the named desk could not write a file: %d %s", code, data)
	}
	if code, data := deskCall(t, ts, "GET", "/api/storage/status", named.ID, "", true); code != 200 {
		t.Fatalf("the named desk's storage status: %d %s", code, data)
	}
	// Only the named desk's own registered folder borrows the installation.
	c := s.storageConstruction
	saved := child.cfg.deskID
	child.cfg.deskID = strings.Repeat("e", 32)
	if err := c.namedDeskChildCurrent(child); err == nil {
		t.Fatal("a desk ID the desks folder does not name borrowed the installation")
	}
	child.cfg.deskID = saved
	if err := c.namedDeskChildCurrent(child); err != nil {
		t.Fatalf("the named desk's own folder: %v", err)
	}
	other := createTestDesk(t, ts, "Other")
	child.cfg.deskID = other.ID
	if err := c.namedDeskChildCurrent(child); err == nil {
		t.Fatal("another desk's folder was accepted for this desk")
	}
	child.cfg.deskID = saved
	// Where the installation itself is read only, a named desk is too.
	s.desksMu.Lock()
	storageTestMust(t, child.Close())
	delete(s.desks, named.ID)
	s.storageReadOnly, s.storageProblem = true, errStorageCustody
	reopened, err := s.openDeskLocked(named.ID)
	s.desksMu.Unlock()
	if err != nil || !reopened.storageReadOnly {
		t.Fatalf("a named desk of a read-only installation started writable: %v", err)
	}
}
