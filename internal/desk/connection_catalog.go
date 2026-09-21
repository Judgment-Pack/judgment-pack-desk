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
type sourceDescriptor struct {
	ID         string   `json:"id"`
	Input      string   `json:"input"`
	MediaTypes []string `json:"mediaTypes"`
	MaxBytes   int      `json:"maxBytes"`
}
type connectionCatalog struct {
	Sources   []sourceDescriptor     `json:"sources"`
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
	return runConnectionCatalog(ctx, cmd)
}

func runConnectionCatalog(ctx context.Context, cmd *exec.Cmd) (json.RawMessage, error) {
	cmd.Stdin = nil
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	pipe, err := cmd.StdoutPipe()
	if err != nil || cmd.Start() != nil {
		return nil, errConnectionCatalog
	}
	// Closing our read end also bounds cancellation if a descendant ever
	// inherits stdout. Killing just the direct child would not close that FD.
	stop := context.AfterFunc(ctx, func() { _ = pipe.Close() })
	defer stop()
	defer pipe.Close()
	raw, err := io.ReadAll(io.LimitReader(pipe, connectionCatalogLimit+1))
	if err != nil || len(raw) > connectionCatalogLimit {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, errConnectionCatalog
	}
	if cmd.Wait() != nil || ctx.Err() != nil {
		return nil, errConnectionCatalog
	}
	// encoding/json otherwise folds duplicate and case-insensitive member
	// names. Discovery has one spelling and one value for every protocol field.
	members, ok := catalogMembers(raw, "version", "providers", "sources")
	if !ok {
		return nil, errConnectionCatalog
	}
	for _, member := range members {
		if member.name == "sources" {
			var rows []json.RawMessage
			if json.Unmarshal(member.raw, &rows) != nil || len(rows) > 32 {
				return nil, errConnectionCatalog
			}
			for _, row := range rows {
				if _, ok := catalogMembers(row, "id", "input", "mediaTypes", "maxBytes"); !ok {
					return nil, errConnectionCatalog
				}
			}
		}
		if member.name == "providers" {
			var rows []json.RawMessage
			if json.Unmarshal(member.raw, &rows) != nil || len(rows) > 32 {
				return nil, errConnectionCatalog
			}
			for _, row := range rows {
				if _, ok := catalogMembers(row, "id", "auth", "registration", "selection", "queryRequired", "operations"); !ok {
					return nil, errConnectionCatalog
				}
			}
		}
	}
	var catalog connectionCatalog
	if decodeDataJSON(raw, &catalog) != nil || catalog.Version != 2 || catalog.Sources == nil || len(catalog.Sources) > 32 || catalog.Providers == nil || len(catalog.Providers) > 32 {
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
	seenSources := map[string]bool{}
	for _, source := range catalog.Sources {
		if seenSources[source.ID] || !catalogIdentifier.MatchString(source.ID) || !catalogIdentifier.MatchString(source.Input) || source.MaxBytes < 1 || source.MaxBytes > 16<<20 || len(source.MediaTypes) == 0 || len(source.MediaTypes) > 8 {
			return nil, errConnectionCatalog
		}
		seenSources[source.ID] = true
		types := map[string]bool{}
		for _, media := range source.MediaTypes {
			if types[media] || !regexp.MustCompile(`^[a-z][a-z0-9.+-]{0,63}/[a-z][a-z0-9.+-]{0,63}$`).MatchString(media) {
				return nil, errConnectionCatalog
			}
			types[media] = true
		}
	}
	return json.Marshal(catalog)
}

func catalogMembers(raw []byte, allowed ...string) ([]deskMember, bool) {
	members, duplicate, err := topLevelMembers(raw)
	if err != nil || duplicate != "" || len(members) != len(allowed) {
		return nil, false
	}
	for _, member := range members {
		found := false
		for _, name := range allowed {
			found = found || member.name == name
		}
		if !found {
			return nil, false
		}
	}
	return members, true
}
