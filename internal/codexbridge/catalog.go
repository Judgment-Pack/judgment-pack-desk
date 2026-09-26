package codexbridge

import (
	"context"
	_ "embed"
	"encoding/json"
)

// Codex takes a model's tool mode, sub-agent version and experimental tools
// from the model catalog before it reads the feature flags in config.toml, and
// a signed-in client fetches that catalog from the account. Desk therefore
// supplies its own through model_catalog_json: the pinned release's bundled
// catalog with the hidden models dropped and those three fields cleared, as
// scripts/codex-model-catalog.py derives it. The profile writes the file and
// refuses a changed copy, and every launched process must list exactly these
// models.
//
//go:embed model-catalog.json
var modelCatalog []byte

const (
	catalogFilename = "model-catalog.json"
	// The release file the catalog was derived from; both move with the pin.
	catalogRelease        = "rust-v0.157.1"
	catalogUpstreamSHA256 = "0178d235c589a31abd6ed0ea1e870935dc5819240eb0e813e178d3ebedf534f4"
)

type catalogModel struct {
	Slug                       string          `json:"slug"`
	Visibility                 string          `json:"visibility"`
	SupportedInAPI             bool            `json:"supported_in_api"`
	ToolMode                   json.RawMessage `json:"tool_mode"`
	MultiAgentVersion          json.RawMessage `json:"multi_agent_version"`
	ExperimentalSupportedTools json.RawMessage `json:"experimental_supported_tools"`
}

func catalogModels() ([]catalogModel, error) {
	var catalog struct {
		Models []catalogModel `json:"models"`
	}
	if err := json.Unmarshal(modelCatalog, &catalog); err != nil {
		return nil, err
	}
	return catalog.Models, nil
}

// catalogSlugs is the exact set a launched process must list, hidden included.
var catalogSlugs = func() map[string]bool {
	models, err := catalogModels()
	if err != nil || len(models) == 0 {
		panic("codexbridge: the embedded model catalog is not a catalog")
	}
	slugs := map[string]bool{}
	for _, model := range models {
		slugs[model.Slug] = true
	}
	return slugs
}()

// catalogListed refuses a process that lists anything but Desk's catalog: the
// supplied catalog was not applied, or the release's own models are offered.
func catalogListed(ctx context.Context, c *client) error {
	var response struct {
		Data []struct {
			Model string `json:"model"`
		} `json:"data"`
		Next *string `json:"nextCursor"`
	}
	if err := c.call(ctx, "model/list", map[string]any{"limit": 100, "includeHidden": true}, &response); err != nil {
		return err
	}
	if (response.Next != nil && *response.Next != "") || len(response.Data) != len(catalogSlugs) {
		return ErrUnavailable
	}
	seen := map[string]bool{}
	for _, row := range response.Data {
		if !catalogSlugs[row.Model] || seen[row.Model] {
			return ErrUnavailable
		}
		seen[row.Model] = true
	}
	return nil
}
