package desk

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"time"
)

var researchConnectionRevision = regexp.MustCompile(`^[a-f0-9]{64}$`)

var errResearchSearchChanged = errors.New("The search connection changed. Review the Web search settings in Recipe and save the recipe before starting research.")
var errResearchSearchUnavailable = errors.New("Search settings could not be checked. Reload the search connection before starting research.")

// This reads local connection metadata only. It neither tests the provider nor
// refreshes the reviewed recipe implicitly. External gateways enforce their own
// connection revisions; never compare them with this computer's connections.
func (s *Server) validateResearchSearchConnection(ctx context.Context, recipe inputResearchRecipe) error {
	if recipe.Search == nil || recipe.Limits.Searches == 0 {
		return nil
	}
	gateway, err := s.configuredResearch()
	if err != nil {
		return err
	}
	if !gateway.managedLocal {
		return nil
	}
	companion := s.connectionCompanion("web-search", true)
	if companion == nil {
		return errResearchSearchUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	raw, err := companion.call(ctx, s.localGateway.bundle, platformPath(s.assistant.dir, "gateway-connections"), "status", json.RawMessage(`{}`), "web-search", false)
	if err != nil {
		return errResearchSearchUnavailable
	}
	return validateResearchSearchStatus(raw, *recipe.Search)
}

func validateResearchSearchStatus(raw json.RawMessage, choice inputResearchSearch) error {
	var status struct {
		Version     int    `json:"version"`
		Error       string `json:"error"`
		Connections []struct {
			ID       string `json:"id"`
			Revision string `json:"revision"`
		} `json:"connections"`
	}
	if len(raw) > 64<<10 || json.Unmarshal(raw, &status) != nil || status.Error != "" || status.Version != 1 || status.Connections == nil || len(status.Connections) > 32 {
		return errResearchSearchUnavailable
	}
	seen := map[string]bool{}
	matched := ""
	for _, connection := range status.Connections {
		if !researchSourceName.MatchString(connection.ID) || !researchConnectionRevision.MatchString(connection.Revision) || seen[connection.ID] {
			return errResearchSearchUnavailable
		}
		seen[connection.ID] = true
		if connection.ID == choice.Connection {
			matched = connection.Revision
		}
	}
	if matched == "" {
		return errors.New("The selected search connection is no longer available. Choose a Web search connection in Recipe and save the recipe before starting research.")
	}
	if matched != choice.Revision {
		return errResearchSearchChanged
	}
	return nil
}
