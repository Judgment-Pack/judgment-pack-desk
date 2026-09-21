package desk

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"
)

const connectionCatalogLimit = 32 << 10

// These are protocol identifiers, not executable names or authorization URLs.
// The browser additionally intersects them with the handlers it implements.
type connectionDescriptor struct {
	ID            string   `json:"id"`
	Auth          string   `json:"auth"`
	Registration  string   `json:"registration"`
	Selection     string   `json:"selection"`
	QueryRequired *bool    `json:"queryRequired"`
	Operations    []string `json:"operations"`
}
type connectionCatalog struct {
	Version   int                    `json:"version"`
	Providers []connectionDescriptor `json:"providers"`
}

var catalogIdentifier = regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`)
var errConnectionCatalog = errors.New("gateway connection catalog unavailable")

func readConnectionCatalog(ctx context.Context, bundle string) (json.RawMessage, error) {
	if verifyGatewayBundle(bundle) != nil {
		return nil, errConnectionCatalog
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(bundle, executableName("gateway-connections")), "--catalog")
	return runConnectionCatalog(cmd)
}

func runConnectionCatalog(cmd *exec.Cmd) (json.RawMessage, error) {
	cmd.Stdin = nil
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	pipe, err := cmd.StdoutPipe()
	if err != nil || cmd.Start() != nil {
		return nil, errConnectionCatalog
	}
	raw, err := io.ReadAll(io.LimitReader(pipe, connectionCatalogLimit+1))
	if err != nil || len(raw) > connectionCatalogLimit {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, errConnectionCatalog
	}
	if cmd.Wait() != nil {
		return nil, errConnectionCatalog
	}
	var catalog connectionCatalog
	if decodeDataJSON(raw, &catalog) != nil || catalog.Version != 1 || catalog.Providers == nil || len(catalog.Providers) > 32 {
		return nil, errConnectionCatalog
	}
	seen := map[string]bool{}
	for _, provider := range catalog.Providers {
		if provider.QueryRequired == nil || seen[provider.ID] || !catalogIdentifier.MatchString(provider.ID) || !catalogIdentifier.MatchString(provider.Auth) || !catalogIdentifier.MatchString(provider.Registration) || !catalogIdentifier.MatchString(provider.Selection) || len(provider.Operations) == 0 || len(provider.Operations) > 16 {
			return nil, errConnectionCatalog
		}
		seen[provider.ID] = true
		operations := map[string]bool{}
		for _, operation := range provider.Operations {
			if operations[operation] || !catalogIdentifier.MatchString(operation) {
				return nil, errConnectionCatalog
			}
			operations[operation] = true
		}
	}
	return json.Marshal(catalog)
}
