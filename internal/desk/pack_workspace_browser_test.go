package desk

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// A disposable project exercises the real saved-pack routes and suite recovery.
// Test design is opened but never submitted to a provider.
func TestPackWorkspaceBrowser(t *testing.T) {
	runPackWorkspaceBrowser(t, "JPS_PACK_WORKSPACE_BROWSER")
}

func TestPackProposalBrowser(t *testing.T) {
	runPackWorkspaceBrowser(t, "JPS_PACK_PROPOSAL_BROWSER")
}

func runPackWorkspaceBrowser(t *testing.T, scriptEnv string) {
	t.Helper()
	script, runtime := os.Getenv(scriptEnv), os.Getenv("JPS_TEST_RUNTIME")
	if script == "" || runtime == "" {
		t.Skipf("set %s and JPS_TEST_RUNTIME after building web/dist", scriptEnv)
	}
	project := t.TempDir()
	mustWrite := func(path string, data []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(project, path), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	pack, err := os.ReadFile("../../web/src/packs/__fixtures__/minimal.pack.json")
	if err != nil {
		t.Fatal(err)
	}
	var packDocument map[string]any
	if err := json.Unmarshal(pack, &packDocument); err != nil {
		t.Fatal(err)
	}
	// Static advisories inspect even an unreachable comparison; existing outcome tests stay unchanged.
	packDocument["rules"].([]any)[0].(map[string]any)["when"] = map[string]any{"op": "any", "conditions": []any{map[string]any{"op": "literal", "value": true}, map[string]any{"op": "fact", "path": "/count", "operator": "greater-than", "value": "0"}}}
	pack, _ = json.Marshal(packDocument)
	mustWrite("example.pack.json", pack)
	mustWrite("jpack.json", []byte(`{"configVersion":"6","packs":{"example":{"path":"example.pack.json","expectedVersion":"0.1.0","matrix":"example.matrix.json"}}}`))
	mustWrite("example.matrix.json", []byte(`{"matrixVersion":"3","cases":[{"id":"saved-case","facts":{},"expectedDisposition":{"kind":"outcome","outcomeId":"proceed","reasons":[],"handoff":{"state":"none"}}}]}`))
	record, _ := json.Marshal(map[string]any{"researchRecordVersion": "1", "authoringSources": map[string]any{"version": 1, "documents": []any{}, "files": []any{map[string]any{"name": "Reviewed requirements.txt", "text": "A retained source from the authoring draft."}}, "retrievals": []any{}}})
	mustWrite("example.research.json", record)
	s, ts := startDesk(t, Config{Static: os.DirFS("../../web/dist"), ProjectDir: project, DeskConfigDir: t.TempDir(), Token: testToken, JpackBin: runtime, CodexBin: "off"})
	defer ts.Close()
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", script, ts.URL, testToken)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("browser: %s %v", out, err)
	} else {
		t.Log(string(out))
	}
}

func TestDialectsRefreshBrowser(t *testing.T) {
	runPackWorkspaceBrowser(t, "JPS_DIALECTS_REFRESH_BROWSER")
}

func TestCitationPreviewBrowser(t *testing.T) {
	runPackWorkspaceBrowser(t, "JPS_CITATION_PREVIEW_BROWSER")
}

func TestPageLoadingBrowser(t *testing.T) {
	runPackWorkspaceBrowser(t, "JPS_PAGE_LOADING_BROWSER")
}
