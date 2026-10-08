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

// localPlan is what Desk launches its local gateway with: the gateway's
// source arguments, and whether the plan gave the document sources the
// document-processing envelope. The gateway reads its settings when it is
// asked for the plan, and Desk asks once, when it starts the local gateway,
// so the plan is fixed for that gateway's life.
type localPlan struct {
	args               []string
	documentProcessing bool
}

// The envelopes a plan may give a source, in seconds: every source 60; the
// web-search source launched with --long-search 130 (gateway v0.10.0, #219);
// a document source launched with --document-processing 150, the processing
// deadline of at most 120 s, the adapter's stop at 140 s and its report
// (gateway v0.10.0, #218).
const (
	localSourceSeconds     = 60
	localSearchSeconds     = 130
	localProcessingSeconds = 150
)

// documentProcessingSource reports whether a source of the plan may read a
// PDF with the operator's OCR processor, and so may be launched with
// --document-processing. Gateway v0.10.0 gives exactly these four the flag.
func documentProcessingSource(id string) bool {
	switch id {
	case "documents", "drive", "web", "aws-s3":
		return true
	}
	return false
}

// Only the verified installed companion defines adapter launches. The browser,
// project and catalog cannot supply executable names or command arguments.
//
// The companion reads the document-processing settings and the search
// connections under JPACK_CONNECTIONS_DIR to decide the plan, so it is given
// this desk's connections directory, the one its companions keep: without
// it the plan is always the one with neither.
func localGatewaySourceArgs(ctx context.Context, bundle, connectionsDir string) (localPlan, error) {
	if err := verifyGatewayBundle(bundle); err != nil {
		return localPlan{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(bundle, executableName("gateway-connections")), "--local-plan")
	// The last value of a name is the one a program sees: Desk's directory,
	// never one Desk's own environment happens to carry.
	cmd.Env = append(os.Environ(), "JPACK_CONNECTIONS_DIR="+connectionsDir)
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return localPlan{}, err
	}
	if err = cmd.Start(); err != nil {
		return localPlan{}, err
	}
	defer pipe.Close()
	stop := context.AfterFunc(ctx, func() { _ = pipe.Close() })
	defer stop()
	raw, err := io.ReadAll(io.LimitReader(pipe, (32<<10)+1))
	if err != nil || len(raw) > 32<<10 {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return localPlan{}, errors.New("invalid local source plan")
	}
	if cmd.Wait() != nil || ctx.Err() != nil {
		return localPlan{}, errors.New("local source plan unavailable")
	}
	manifestFile, err := os.Open(filepath.Join(bundle, "gateway-bundle.json"))
	if err != nil {
		return localPlan{}, err
	}
	defer manifestFile.Close()
	manifestRaw, err := readBounded(manifestFile, 8192)
	if err != nil {
		return localPlan{}, err
	}
	var manifest struct {
		Files map[string]string `json:"files"`
	}
	if json.Unmarshal(manifestRaw, &manifest) != nil {
		return localPlan{}, errors.New("invalid local bundle")
	}
	return decodeLocalPlan(raw, manifest.Files)
}

// decodeLocalSourcePlan is decodeLocalPlan's arguments alone.
func decodeLocalSourcePlan(raw []byte, files map[string]string) ([]string, error) {
	plan, err := decodeLocalPlan(raw, files)
	if err != nil {
		return nil, err
	}
	return plan.args, nil
}

func decodeLocalPlan(raw []byte, files map[string]string) (localPlan, error) {
	invalid := errors.New("unsupported local source plan")
	if _, ok := catalogMembers(raw, "version", "sources"); !ok {
		return localPlan{}, invalid
	}
	var rows struct {
		Sources []json.RawMessage `json:"sources"`
	}
	if json.Unmarshal(raw, &rows) != nil {
		return localPlan{}, invalid
	}
	for _, row := range rows.Sources {
		if _, ok := catalogMembers(row, "id", "executable", "args", "shape", "timeout", "connections"); !ok {
			return localPlan{}, invalid
		}
	}
	var plan localSourcePlan
	if decodeDataJSON(raw, &plan) != nil || plan.Version != 1 || len(plan.Sources) == 0 || len(plan.Sources) > 64 {
		return localPlan{}, invalid
	}
	seen := map[string]bool{}
	args := []string{}
	// How many document sources the plan has, and how many it launches with
	// --document-processing: the gateway gives the flag to all or to none.
	documentSources, processing := 0, 0
	for _, source := range plan.Sources {
		// A source is given more than 60 seconds only as the gateway's plan
		// gives it, by the flag that says why: --long-search on web-search,
		// --document-processing (with the connections directory, where the
		// settings are) on a document source. Either flag anywhere else, or
		// twice, or a longer envelope without its flag, refuses the plan whole.
		longSearch, documentProcessing := 0, 0
		for _, arg := range source.Args {
			switch arg {
			case "--long-search":
				longSearch++
			case "--document-processing":
				documentProcessing++
			}
		}
		maximum := localSourceSeconds
		if longSearch > 1 || documentProcessing > 1 {
			return localPlan{}, invalid
		}
		if longSearch == 1 {
			if source.ID != "web-search" {
				return localPlan{}, invalid
			}
			maximum = localSearchSeconds
		}
		if documentProcessingSource(source.ID) {
			documentSources++
		}
		if documentProcessing == 1 {
			if !documentProcessingSource(source.ID) || !source.Connections {
				return localPlan{}, invalid
			}
			maximum = localProcessingSeconds
			processing++
		}
		if !catalogIdentifier.MatchString(source.ID) || seen[source.ID] || !catalogIdentifier.MatchString(source.Executable) || !strings.HasPrefix(source.Executable, "adapter-") || files[executableName(source.Executable)] == "" || len(source.Args) > 16 || source.Timeout < 1 || source.Timeout > maximum {
			return localPlan{}, invalid
		}
		if source.Shape != "command" && source.Shape != "http" && source.Shape != "mcp" {
			return localPlan{}, invalid
		}
		seen[source.ID] = true
		words := []string{executableName(source.Executable)}
		for _, arg := range source.Args {
			if len(arg) == 0 || len(arg) > 256 || strings.ContainsAny(arg, "\\\"'`)($;") || strings.IndexFunc(arg, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
				return localPlan{}, invalid
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
	if !seen["documents"] || processing != 0 && processing != documentSources {
		return localPlan{}, invalid
	}
	return localPlan{args: args, documentProcessing: processing > 0}, nil
}
