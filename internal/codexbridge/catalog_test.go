package codexbridge

import (
	"encoding/json"
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
		if string(model.ToolMode) != "null" || string(model.MultiAgentVersion) != "null" || string(model.ExperimentalSupportedTools) != "[]" {
			t.Errorf("model %q keeps tool metadata: tool_mode %s, multi_agent_version %s, experimental_supported_tools %s",
				model.Slug, model.ToolMode, model.MultiAgentVersion, model.ExperimentalSupportedTools)
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

// catalogRows is the model/list reply a faithful process gives for the catalog.
func catalogRows() []any {
	rows := []any{}
	for slug := range catalogSlugs {
		rows = append(rows, map[string]any{"model": slug, "displayName": slug, "hidden": false,
			"defaultReasoningEffort": "medium", "supportedReasoningEfforts": []any{map[string]string{"reasoningEffort": "medium"}}})
	}
	return rows
}
