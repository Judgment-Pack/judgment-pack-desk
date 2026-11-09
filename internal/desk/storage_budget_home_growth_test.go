package desk

import (
	"encoding/json"
	"strings"
	"testing"
)

// **A desk added to a Desk home after the first start is a registration
// difference, warned about, never read only.** The saved catalog holds the
// desks of the first start. A desk created later starts writable, the home
// stays writable while it runs, and after a restart both start writable and
// status lists the added desk's folder as not registered. Any other change to
// the registry (a desk changed or gone, another owner) is still custody.
func TestADeskAddedToADeskHomeIsWarnedAboutNotReadOnly(t *testing.T) {
	requireStorageLine(t)
	cfg, registry := homeFixture(t)
	s, ts := startDesk(t, cfg)
	added := createTestDesk(t, ts, "Added later")
	child := s.desks[added.ID]
	if child == nil || child.storageReadOnly || s.storageReadOnly {
		t.Fatalf("a desk added while Desk runs was read only: %v", child)
	}
	storageTestMust(t, s.storageConstruction.sources.currentProjects(t.Context()))
	if code, data := deskCall(t, ts, "PUT", "/api/file", added.ID, `{"path":"notes.md","content":"added","baseSha256":"","createParents":true}`, true); code != 200 && code != 201 {
		t.Fatalf("the added desk could not write: %d %s", code, data)
	}
	ts.Close()
	storageTestMust(t, s.Close())
	again, ats := startDesk(t, cfg)
	t.Cleanup(func() { ats.Close(); again.Close() })
	if again.storageReadOnly {
		t.Fatalf("the start after a desk was added was read only: %v", again.storageProblem)
	}
	reopened := again.desks[added.ID]
	if reopened == nil || reopened.storageReadOnly {
		t.Fatal("the added desk did not start writable after a restart")
	}
	code, data := deskCall(t, ats, "GET", "/api/storage/status", "", "", true)
	var status storageStatusResponse
	if code != 200 || json.Unmarshal(data, &status) != nil || status.ReadOnly || status.Registration == nil {
		t.Fatalf("status did not warn about the added desk: %d %s", code, data)
	}
	listed := false
	for _, folder := range status.Registration.Folders {
		listed = listed || strings.HasSuffix(folder, added.Folder[strings.LastIndex(added.Folder, "/")+1:])
	}
	if !listed {
		t.Fatalf("the added desk's folder was not listed: %v", status.Registration.Folders)
	}
	// Growth only: a changed or removed desk, or another owner, is custody.
	grown := registry
	grown.Desks = append(append([]deskHomeEntry{}, registry.Desks...), deskHomeEntry{ID: added.ID, Folder: "added"})
	if !storageHomeRegistryGrew(registry, grown) {
		t.Fatal("an added desk was not growth")
	}
	changed := grown
	changed.Desks = append([]deskHomeEntry{}, grown.Desks...)
	changed.Desks[0].Folder = "elsewhere"
	removed := registry
	removed.Desks = nil
	owner := grown
	owner.Owner = strings.Repeat("f", 64)
	for name, now := range map[string]deskHomeRegistry{"changed": changed, "removed": removed, "owner": owner} {
		if storageHomeRegistryGrew(registry, now) {
			t.Fatalf("a %s registry was taken for growth", name)
		}
	}
}
