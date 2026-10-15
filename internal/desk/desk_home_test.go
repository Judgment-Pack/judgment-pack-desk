package desk

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func homeFixture(t *testing.T) (Config, deskHomeRegistry) {
	t.Helper()
	base := t.TempDir()
	if err := os.Chmod(base, 0700); err != nil {
		t.Fatal(err)
	}
	config, project := filepath.Join(base, deskHomePlatform), filepath.Join(base, "unveil")
	for _, path := range []string{config, project, filepath.Join(project, ".desk-private", "data")} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	id, owner := strings.Repeat("a", 32), strings.Repeat("b", 64)
	value := deskHomeRegistry{Version: 1, Owner: owner, Startup: id, Desks: []deskHomeEntry{{ID: id, Name: "Unveil", Folder: "unveil", Kind: "project", HistoryIdentity: "/original/unveil", StorageOwner: owner, RunnerWorkspace: strings.Repeat("c", 64)}}}
	raw, _ := json.Marshal(value)
	privateFixture(t, filepath.Join(config, deskHomeRegistryName), string(raw))
	privateFixture(t, filepath.Join(project, projectConfigName), `{"deskConfigVersion":1}`)
	privateFixture(t, filepath.Join(project, ".desk-private", dataLocationName), `{"version":1,"path":"data"}`)
	marker, _ := json.Marshal(dataMarker{Version: 1, Owner: owner})
	privateFixture(t, filepath.Join(project, ".desk-private", "data", dataMarkerName), string(marker))
	return Config{ProjectDir: project, DeskConfigDir: config, Token: testToken, JpackBin: os.Getenv("JPACK_BIN"), LocalAccess: true}, value
}

func TestDeskHomeRetainsHistoryAndRunnerIdentityAfterMove(t *testing.T) {
	cfg, registry := homeFixture(t)
	s, ts := startDesk(t, cfg)
	if _, err := s.moveSnapshot(filepath.Join(t.TempDir(), "detached")); err == nil {
		t.Fatal("legacy endpoint could detach a registered project")
	}
	if s.installationOwner() != registry.Owner || s.conversationIdentity() != registry.Desks[0].HistoryIdentity {
		t.Fatal("retained identity changed")
	}
	if code, _ := chatRequest(t, ts, "PUT", "absent", storageChat, true, ""); code != 200 {
		t.Fatal(code)
	}
	store, err := s.openChatData()
	if err != nil {
		t.Fatal(err)
	}
	path := store.location.Path
	store.root.Close()
	if path != filepath.Join(cfg.ProjectDir, ".desk-private", "data") {
		t.Fatal(path)
	}
	ts.Close()
	s.Close()
	oldHome := filepath.Dir(cfg.DeskConfigDir)
	newHome := filepath.Join(t.TempDir(), "jpack-desk")
	if err = os.Rename(oldHome, newHome); err != nil {
		t.Fatal(err)
	}
	cfg.ProjectDir = filepath.Join(newHome, "unveil")
	cfg.DeskConfigDir = filepath.Join(newHome, deskHomePlatform)
	again, ats := startDesk(t, cfg)
	if again.installationOwner() != registry.Owner || again.conversationIdentity() != registry.Desks[0].HistoryIdentity {
		t.Fatal("move changed retained identity")
	}
	if code, reply := chatRequest(t, ats, "GET", "", "", true, ""); code != 200 || string(reply.Content) != storageChat {
		t.Fatal("chat detached", code, reply)
	}
	if again.homeEntry().RunnerWorkspace != registry.Desks[0].RunnerWorkspace {
		t.Fatal("Runner namespace changed")
	}
}
func TestDeskHomeCreatesFriendlyIsolatedFolders(t *testing.T) {
	cfg, _ := homeFixture(t)
	s, ts := startDesk(t, cfg)
	first := createTestDesk(t, ts, "Research desk")
	second := createTestDesk(t, ts, "Research desk")
	for _, row := range []deskRecord{first, second} {
		if filepath.Dir(row.Folder) != filepath.Dir(cfg.DeskConfigDir) || !strings.HasPrefix(filepath.Base(row.Folder), "research-desk-") {
			t.Fatal(row)
		}
		if _, ok := s.desks[row.ID]; !ok {
			t.Fatal("not published")
		}
	}
	if first.Folder == second.Folder || first.ID == second.ID {
		t.Fatal("name collision")
	}
	ts.Close()
	s.Close()
	again, _ := startDesk(t, cfg)
	if len(again.desks) != 2 {
		t.Fatal("registry did not resume", len(again.desks))
	}
	for _, child := range again.desks {
		if shared, reason := child.identityShared(); shared {
			t.Fatal("registered Desk became a copy", reason)
		}
	}
}
func TestDeskHomeRejectsInvalidRegistryAndPointer(t *testing.T) {
	cfg, valid := homeFixture(t)
	for _, change := range []func(*deskHomeRegistry){
		func(v *deskHomeRegistry) { v.Desks[0].Folder = "../escape" },
		func(v *deskHomeRegistry) { v.Desks[0].Folder = "_platform" },
		func(v *deskHomeRegistry) { v.Desks = append(v.Desks, v.Desks[0]) },
		func(v *deskHomeRegistry) { v.Startup = strings.Repeat("d", 32) },
		func(v *deskHomeRegistry) { v.Desks[0].Kind = "desk" },
	} {
		raw, _ := json.Marshal(valid)
		var value deskHomeRegistry
		json.Unmarshal(raw, &value)
		change(&value)
		if validateHomeRegistry(value) == nil {
			t.Fatal("accepted invalid registry", value)
		}
	}
	t.Setenv("JPACK_DESK_HOME", filepath.Dir(cfg.DeskConfigDir))
	if path, err := resolveHomeConfig(""); err != nil || path != cfg.DeskConfigDir {
		t.Fatal(path, err)
	}
	t.Setenv("JPACK_DESK_HOME", filepath.Join(t.TempDir(), "missing"))
	if _, err := resolveHomeConfig(""); err == nil {
		t.Fatal("silently fell back to old config")
	}
	if path, err := resolveHomeConfig(cfg.DeskConfigDir); err != nil || path != cfg.DeskConfigDir {
		t.Fatal("explicit fixture lost precedence", path, err)
	}
}

func TestDeskHomeOpeningNamedDeskKeepsOriginalProjectAvailable(t *testing.T) {
	cfg, registry := homeFixture(t)
	s, ts := startDesk(t, cfg)
	if code, _ := chatRequest(t, ts, "PUT", "absent", storageChat, true, ""); code != 200 {
		t.Fatal(code)
	}
	named := createTestDesk(t, ts, "Research")
	ts.Close()
	s.Close()
	cfg.ProjectDir = named.Folder
	again, _ := startDesk(t, cfg)
	original := again.desks[registry.Desks[0].ID]
	if original == nil {
		t.Fatal("original project disappeared when a named Desk became startup")
	}
	row, err := original.deskRecord()
	if err != nil || row.ID != registry.Desks[0].ID || row.Name != "Unveil" {
		t.Fatal(row, err)
	}
	if original.conversationIdentity() != registry.Desks[0].HistoryIdentity || original.signingKeyName() == named.ID {
		t.Fatal("original project adopted another Desk identity")
	}
	t.Setenv(runtimeConfigEnv, "/another/project/jpack.json")
	t.Setenv(runtimeSigningKeyEnv, "/another/key")
	t.Setenv(runtimeSigningPublicKeyEnv, "another-public-key")
	for _, value := range original.runtimeEnv() {
		if strings.HasPrefix(value, runtimeConfigEnv+"=") || strings.HasPrefix(value, runtimeSigningKeyEnv+"=") || strings.HasPrefix(value, runtimeSigningPublicKeyEnv+"=") {
			t.Fatal("child inherited another project's runtime override")
		}
	}
	if dir, named, ok := original.projectRuntime(); !ok || named != "" || dir.startup {
		t.Fatal("child runtime treated as process startup project")
	}
	for _, selector := range []string{registry.Desks[0].ID, strings.Repeat("e", 32)} {
		w := httptest.NewRecorder()
		original.handleAgentRun(w, httptest.NewRequest("GET", "http://localhost/api/agent/run?desk="+selector, nil))
		want := 400
		if selector == registry.Desks[0].ID {
			want = 401
		}
		if w.Code != want {
			t.Fatal("wrong agent routing", w.Code, w.Body.String())
		}
	}
}

func TestDeskHomeMovePreservesSelectedStartupDesk(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	cfg, _ := homeFixture(t)
	s, ts := startDesk(t, cfg)
	named := createTestDesk(t, ts, "Research")
	raw, _ := json.Marshal(map[string]any{"deskConfigVersion": 1, "project": map[string]any{"file": filepath.Join(named.Folder, projectConfigName)}})
	privateFixture(t, filepath.Join(cfg.DeskConfigDir, deskConfigName), string(raw))
	destination := filepath.Join(t.TempDir(), "jpack-desk")
	plan, err := s.planDeskHome(destination)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.prepareHomeTransaction(plan)
	if err != nil {
		t.Fatal(err)
	}
	ts.Close()
	s.Close()
	journal := homeJournalPath(tx.Config)
	if err = saveHomeTransaction(journal, tx, true); err != nil {
		t.Fatal(err)
	}
	if err = finishHomeTransaction(journal); err != nil {
		t.Fatal(err)
	}
	path, err := ResolveProjectDir("", filepath.Join(destination, deskHomePlatform))
	if err != nil || path != filepath.Join(destination, filepath.Base(named.Folder)) {
		t.Fatal("startup selection changed", path, err)
	}
}
