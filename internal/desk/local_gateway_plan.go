package desk

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type localSourcePlan struct {
	Version int `json:"version"`
	Sources []struct {
		ID          string   `json:"id"`
		Executable  string   `json:"executable"`
		Args        []string `json:"args"`
		Shape       string   `json:"shape"`
		Timeout     int      `json:"timeout"`
		Connections bool     `json:"connections"`
	} `json:"sources"`
}

// Only the verified installed companion defines adapter launches. The browser,
// project and catalog cannot supply executable names or command arguments.
func localGatewaySourceArgs(ctx context.Context, bundle string) ([]string, error) {
	if err := verifyGatewayBundle(bundle); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(bundle, executableName("gateway-connections")), "--local-plan")
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	defer pipe.Close()
	stop := context.AfterFunc(ctx, func() { _ = pipe.Close() })
	defer stop()
	raw, err := io.ReadAll(io.LimitReader(pipe, (32<<10)+1))
	if err != nil || len(raw) > 32<<10 {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, errors.New("invalid local source plan")
	}
	if cmd.Wait() != nil || ctx.Err() != nil {
		return nil, errors.New("local source plan unavailable")
	}
	manifestFile, err := os.Open(filepath.Join(bundle, "gateway-bundle.json"))
	if err != nil {
		return nil, err
	}
	defer manifestFile.Close()
	manifestRaw, err := readBounded(manifestFile, 8192)
	if err != nil {
		return nil, err
	}
	var manifest struct {
		Files map[string]string `json:"files"`
	}
	if json.Unmarshal(manifestRaw, &manifest) != nil {
		return nil, errors.New("invalid local bundle")
	}
	return decodeLocalSourcePlan(raw, manifest.Files)
}

func decodeLocalSourcePlan(raw []byte, files map[string]string) ([]string, error) {
	invalid := errors.New("unsupported local source plan")
	if _, ok := catalogMembers(raw, "version", "sources"); !ok {
		return nil, invalid
	}
	var rows struct {
		Sources []json.RawMessage `json:"sources"`
	}
	if json.Unmarshal(raw, &rows) != nil {
		return nil, invalid
	}
	for _, row := range rows.Sources {
		if _, ok := catalogMembers(row, "id", "executable", "args", "shape", "timeout", "connections"); !ok {
			return nil, invalid
		}
	}
	var plan localSourcePlan
	if decodeDataJSON(raw, &plan) != nil || plan.Version != 1 || len(plan.Sources) == 0 || len(plan.Sources) > 64 {
		return nil, invalid
	}
	seen := map[string]bool{}
	args := []string{}
	for _, source := range plan.Sources {
		if !catalogIdentifier.MatchString(source.ID) || seen[source.ID] || !catalogIdentifier.MatchString(source.Executable) || !strings.HasPrefix(source.Executable, "adapter-") || files[executableName(source.Executable)] == "" || len(source.Args) > 16 || source.Timeout < 1 || source.Timeout > 60 {
			return nil, invalid
		}
		if source.Shape != "command" && source.Shape != "http" && source.Shape != "mcp" {
			return nil, invalid
		}
		seen[source.ID] = true
		words := []string{executableName(source.Executable)}
		for _, arg := range source.Args {
			if len(arg) == 0 || len(arg) > 256 || strings.ContainsAny(arg, "\\\"'`)($;") || strings.IndexFunc(arg, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
				return nil, invalid
			}
			words = append(words, arg)
		}
		args = append(args, "--source", source.ID+"="+strings.Join(words, " "), "--source-timeout", source.ID+"="+strconv.Itoa(source.Timeout))
		if source.Shape != "command" {
			args = append(args, "--source-shape", source.ID+"="+source.Shape)
		}
		if source.Connections {
			args = append(args, "--source-env", source.ID+"=JPACK_CONNECTIONS_DIR")
		}
	}
	if !seen["documents"] {
		return nil, invalid
	}
	return args, nil
}
