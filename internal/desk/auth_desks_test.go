package desk

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func entryRequest(s *Server, path, header string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "http://127.0.0.1:8791"+path, nil)
	req.RemoteAddr = "127.0.0.1:12345"
	if header != "" {
		req.Header.Set("X-Jpack-Desk", header)
	}
	out := httptest.NewRecorder()
	s.ServeHTTP(out, req)
	return out
}
func TestEntryDirectoryIsPublicReadOnlyAndKeepsOtherGuards(t *testing.T) {
	cfg, registry := homeFixture(t)
	s, ts := startDesk(t, cfg)
	defer ts.Close()
	defer s.Close()
	// A registered, unopened desk is metadata, not permission to launch it.
	id := strings.Repeat("d", 32)
	folder := filepath.Join(filepath.Dir(cfg.DeskConfigDir), "research")
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	registry.Desks = append(registry.Desks, deskHomeEntry{ID: id, Name: "Research", Folder: "research", Kind: "desk", HistoryIdentity: "desk:" + id, StorageOwner: id, RunnerWorkspace: id})
	registry.Startup = id
	raw, _ := json.Marshal(registry)
	privateFixture(t, filepath.Join(cfg.DeskConfigDir, deskHomeRegistryName), string(raw))
	mark := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><path fill="#123456" d="M0 0h10v10z"/></svg>`
	branding, _ := json.Marshal(map[string]any{"deskConfigVersion": 1, "organization": map[string]any{"name": "Private configuration label", "mark": mark}})
	privateFixture(t, filepath.Join(folder, projectConfigName), string(branding))
	before, _ := os.ReadFile(filepath.Join(cfg.DeskConfigDir, deskHomeRegistryName))
	for _, selected := range []string{"", id, strings.Repeat("f", 32), "../bad"} {
		response := entryRequest(s, "/api/auth/desks", selected)
		if response.Code != 200 {
			t.Fatalf("%d %s", response.Code, response.Body.String())
		}
		if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatal(response.Header())
		}
		var result entryDirectory
		if json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Startup != id || len(result.Desks) != 2 {
			t.Fatal(response.Body.String())
		}
		for _, row := range result.Desks {
			if row.ID == id && (row.Name != "Research" || row.Mark == nil) {
				t.Fatal(row)
			}
			if row.Name == "Unveil" && row.ID != "" {
				t.Fatal("owner alias", row)
			}
		}
		for _, secret := range []string{cfg.ProjectDir, cfg.DeskConfigDir, "Private configuration label", "folder", "historyIdentity", "storageOwner", "managed", "location", cfg.Token} {
			if strings.Contains(response.Body.String(), secret) {
				t.Fatalf("public identity leaked %q", secret)
			}
		}
	}
	if len(s.desks) != 0 {
		t.Fatal("anonymous read opened a runtime", len(s.desks))
	}
	if _, err := os.Stat(filepath.Join(folder, deskManifest)); !os.IsNotExist(err) {
		t.Fatal("anonymous read wrote metadata", err)
	}
	after, _ := os.ReadFile(filepath.Join(cfg.DeskConfigDir, deskHomeRegistryName))
	if string(before) != string(after) {
		t.Fatal("read changed startup")
	}
	for _, path := range []string{"/api/desks", "/api/files", "/api/auth/settings", "/api/auth/desks/extra", "/api/auth/status"} {
		if out := entryRequest(s, path, id); out.Code != http.StatusUnauthorized {
			t.Fatalf("unguarded %s: %d", path, out.Code)
		}
	}
	// Live startup changes must be read from disk, never the startup snapshot.
	registry.Startup = registry.Desks[0].ID
	raw, _ = json.Marshal(registry)
	privateFixture(t, filepath.Join(cfg.DeskConfigDir, deskHomeRegistryName), string(raw))
	var result entryDirectory
	out := entryRequest(s, "/api/auth/desks", "")
	_ = json.Unmarshal(out.Body.Bytes(), &result)
	if result.Startup != "" {
		t.Fatal("startup owner must use empty alias", result)
	}
	for _, mutate := range []func(*http.Request){
		func(r *http.Request) { r.RemoteAddr = "192.0.2.1:44" },
		func(r *http.Request) { r.Header.Set("Origin", "http://evil.example") },
		func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") },
		func(r *http.Request) { r.Host = "evil.example:8791" },
	} {
		req := httptest.NewRequest("GET", "http://127.0.0.1:8791/api/auth/desks", nil)
		req.RemoteAddr = "127.0.0.1:3"
		mutate(req)
		out := httptest.NewRecorder()
		s.ServeHTTP(out, req)
		if out.Code != 403 {
			t.Fatal(out.Code, out.Body.String())
		}
	}
}
func TestEntryDirectoryLegacyReadsManifestsWithoutOpeningDesks(t *testing.T) {
	s, ts, _ := assistantServer(t)
	defer ts.Close()
	row := createTestDesk(t, ts, "Review")
	child := s.desks[row.ID]
	delete(s.desks, row.ID)
	defer child.Close()
	out := entryRequest(s, "/api/auth/desks", strings.Repeat("e", 32))
	var result entryDirectory
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &result) != nil || len(result.Desks) != 2 || result.Startup != "" {
		t.Fatal(out.Code, out.Body.String())
	}
	if len(s.desks) != 0 {
		t.Fatal("opened a saved desk")
	}
}
func TestEntryMarkAllowsOnlyBoundedSelfContainedImages(t *testing.T) {
	for _, bad := range []string{`<svg><script>alert(1)</script></svg>`, `<svg><image href="https://secret.example"/></svg>`, `<svg><path fill="url(https://secret.example)"/></svg>`, `<svg><path fill="u\72l(https://secret.example)"/></svg>`, `<svg><foreignObject/></svg>`, `<!DOCTYPE svg [<!ENTITY a SYSTEM "file:///secret">]><svg>&a;</svg>`, `data:image/svg+xml,%3Csvg%3E%3Cscript/%3E%3C/svg%3E`, `data:image/png;base64,bad`, "https://example.com/logo.png", "<svg>" + strings.Repeat(" ", entryMarkLimit) + "</svg>"} {
		if approvedEntryMark(bad) != nil {
			t.Fatalf("accepted unsafe mark %q", bad)
		}
	}
	for _, good := range []string{`<svg xmlns="http://www.w3.org/2000/svg"><path fill="url(#a)" d="M0 0z"/></svg>`, `data:image/svg+xml,%3Csvg%3E%3Cpath%20d=%22M0%200z%22/%3E%3C/svg%3E`} {
		if approvedEntryMark(good) == nil {
			t.Fatal("refused image", good)
		}
	}
}
