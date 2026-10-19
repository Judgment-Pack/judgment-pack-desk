package desk

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Opt-in read-only inventory of a local installation. No New/worker/store
// initialization is used, and no account credential contents are opened.
func TestDeskHomeLocalInventory(t *testing.T) {
	project := os.Getenv("JPS_HOME_INVENTORY_PROJECT")
	if project == "" {
		t.Skip("set JPS_HOME_INVENTORY_PROJECT for a read-only local inventory")
	}
	config := legacyConfigDir()
	root, err := OpenProjectRoot(project)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	settings, err := openPrivateDataRoot(config, false)
	if err != nil {
		t.Fatal(err)
	}
	defer settings.Close()
	s := &Server{projectDir: root.dir, project: root, root: root.own.root, configDir: config, assistant: &assistantStore{dir: config, root: settings}, cfg: Config{RunnerBin: os.Getenv("JPACK_RUNNER_HOME_TEST_BIN")}}
	raw, err := readHomeMetadata(filepath.Join(project, startupIdentityDir, startupIdentityName), startupIdentityLimit)
	if err != nil {
		t.Fatal(err)
	}
	if raw != nil {
		record, err := parseIdentity(raw)
		if err != nil {
			t.Fatal(err)
		}
		s.startup.state, s.startup.id = identityKept, record.ID
	}
	plan, err := s.planDeskHome(deskApplicationDataPath())
	if err != nil {
		t.Fatal(err)
	}
	blockers := append([]string{}, plan.Blockers...)
	plan.Blockers = nil
	if _, err := s.prepareHomeTransaction(plan); err != nil {
		blockers = append(blockers, err.Error())
	}
	plan.Blockers = blockers
	plan.Token = "[read-only inventory]"
	public, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	t.Log(string(public))
}
