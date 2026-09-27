package codexbridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every model Desk offers must carry no tool-mode metadata: Codex reads that
// before the disabled features, so a single entry would reopen the boundary.
func TestTheModelCatalogIsClosed(t *testing.T) {
	models, err := catalogModels()
	if err != nil || len(models) == 0 {
		t.Fatalf("embedded catalog: %d models, %v", len(models), err)
	}
	seen := map[string]bool{}
	for _, model := range models {
		// A signed-out process lists only API-supported models, so the launch
		// check's exact match needs every listed model to be one.
		if model.Slug == "" || seen[model.Slug] || model.Visibility != "list" || !model.SupportedInAPI {
			t.Errorf("model %q: visibility %q, api %v, duplicate %v", model.Slug, model.Visibility, model.SupportedInAPI, seen[model.Slug])
		}
		seen[model.Slug] = true
		if string(model.ToolMode) != "null" || string(model.MultiAgentVersion) != "null" || string(model.ExperimentalSupportedTools) != "[]" ||
			string(model.ApplyPatchToolType) != "null" || model.SupportsSearchTool {
			t.Errorf("model %q keeps tool metadata: tool_mode %s, multi_agent_version %s, experimental_supported_tools %s, apply_patch_tool_type %s, supports_search_tool %v",
				model.Slug, model.ToolMode, model.MultiAgentVersion, model.ExperimentalSupportedTools, model.ApplyPatchToolType, model.SupportsSearchTool)
		}
		// The token-budget switch registers the context tools; the experimental
		// context flag can enable that extension for a signed-in account.
		budget := model.Messages.TokenBudget
		if model.SupportsExperimentalContext || (budget != nil && (budget.Enabled || budget.UseHistoryNotesExtension)) {
			t.Errorf("model %q keeps a context-tool switch: supports_experimental_context %v, token_budget %+v",
				model.Slug, model.SupportsExperimentalContext, budget)
		}
	}
	if len(catalogSlugs) != len(models) {
		t.Fatalf("catalog slugs %d, models %d", len(catalogSlugs), len(models))
	}
	var shape map[string]json.RawMessage
	if json.Unmarshal(modelCatalog, &shape) != nil || len(shape) != 1 || shape["models"] == nil {
		t.Fatalf("catalog shape: %d top-level members", len(shape))
	}
}

// The catalog is derived from the pinned release's file, so its release and
// the derivation script's constants must move with the pin.
func TestTheCatalogFollowsThePin(t *testing.T) {
	version := strings.TrimPrefix(Version, "codex-cli ")
	if catalogRelease != "rust-v"+version || runtimeFilename != "codex-"+version || !strings.Contains(linuxAMD64Runtime.URL, "/"+catalogRelease+"/") {
		t.Fatalf("pin %s, catalog release %s, runtime %s from %s", version, catalogRelease, runtimeFilename, linuxAMD64Runtime.URL)
	}
	script, err := os.ReadFile(filepath.Join("..", "..", "scripts", "codex-model-catalog.py"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"RELEASE = '" + catalogRelease + "'", "UPSTREAM_SHA256 = '" + catalogUpstreamSHA256 + "'"} {
		if !strings.Contains(string(script), line+"\n") {
			t.Errorf("derivation script lacks %s", line)
		}
	}
}

// catalogRows is the model/list reply a faithful process gives for the catalog.
func catalogRows() []any {
	rows := []any{}
	for slug := range catalogSlugs {
		rows = append(rows, map[string]any{"model": slug, "displayName": slug, "hidden": false,
			"defaultReasoningEffort": "medium", "supportedReasoningEfforts": []any{map[string]string{"reasoningEffort": "medium"}}})
	}
	return rows
}
