package desk

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"time"

	exchangeCatalog "github.com/Judgment-Pack/judgment-pack-desk/internal/exchange"
)

// Replay uses only the installed Runner executable. It has no Runtime, store,
// signing key, installation profiles or gateway configuration. Fixture profiles
// are verification material, never new installation authority.
func (s *Server) exchangeReplay(ctx context.Context, f exchangeCatalog.ReplayInput) (json.RawMessage, error) {
	if !filepath.IsAbs(s.cfg.RunnerBin) {
		return nil, errors.New("Offline preparation verification is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	invoke := func(args ...string) ([]byte, int, error) {
		out := &cappedBuffer{limit: 8 << 20}
		cmd := exec.CommandContext(ctx, s.cfg.RunnerBin, append([]string{"verify-preparation"}, args...)...)
		cmd.Env = []string{"LANG=C", "LC_ALL=C"}
		cmd.Stdout = out
		cmd.Stderr = io.Discard
		cmd.WaitDelay = time.Second
		err := cmd.Run()
		code := 0
		if err != nil {
			var failed *exec.ExitError
			if !errors.As(err, &failed) {
				return nil, 3, errors.New("Runner could not verify this preparation")
			}
			code = failed.ExitCode()
		}
		if out.exceeded() || code < 0 || code > 2 {
			return nil, 3, errors.New("Runner could not verify this preparation")
		}
		return out.Bytes(), code, nil
	}
	raw, code, err := invoke("--capabilities")
	if err != nil {
		return nil, err
	}
	var caps struct {
		Version          int
		Command, Instant string
		DialectVersions  []int
		FormatVersions   []string
		Report           struct {
			Version    int
			Binding    []string
			ExitStatus struct{ Reading, Refusal, Finding, CouldNotRun int }
		}
	}
	if code != 0 || json.Unmarshal(raw, &caps) != nil || caps.Version != 1 || caps.Command != "verify-preparation" || caps.Instant != "preparation.verifiedAt" || caps.Report.Version != 1 || !slices.Equal(caps.Report.Binding, []string{"not-checked"}) || caps.Report.ExitStatus.Reading != 0 || caps.Report.ExitStatus.Refusal != 1 || caps.Report.ExitStatus.Finding != 2 || caps.Report.ExitStatus.CouldNotRun != 3 {
		return nil, errors.New("Runner does not support the required offline verification contract")
	}
	var dialect struct {
		Version       json.RawMessage
		FormatVersion string
	}
	if json.Unmarshal(f.Dialect, &dialect) != nil {
		return nil, errors.New("Invalid fixture dialect")
	}
	if dialect.FormatVersion != "" {
		if !slices.Contains(caps.FormatVersions, dialect.FormatVersion) {
			return nil, errors.New("Runner cannot verify this dialect format")
		}
	} else {
		var version int
		if json.Unmarshal(dialect.Version, &version) != nil || !slices.Contains(caps.DialectVersions, version) {
			return nil, errors.New("Runner cannot verify this dialect format")
		}
	}

	dir, err := os.MkdirTemp("", "jpack-preparation-")
	if err != nil {
		return nil, errors.New("Could not stage offline verification")
	}
	defer os.RemoveAll(dir)
	args := []string{"--preparation-digest", f.ExpectedDigest}
	for _, input := range []struct {
		name string
		raw  []byte
	}{
		{"dialect", f.Dialect}, {"case", f.Case}, {"profiles", f.Profiles}, {"preparation", f.Preparation}, {"sources", f.Acquisitions}, {"binding", f.Binding},
	} {
		if len(input.raw) == 0 && (input.name == "binding" || input.name == "sources") {
			continue
		}
		path := filepath.Join(dir, input.name+".json")
		if err := os.WriteFile(path, input.raw, 0600); err != nil {
			return nil, errors.New("Could not stage offline verification")
		}
		args = append(args, "--"+input.name, path)
	}
	raw, code, err = invoke(args...)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, preparationReplayFailure(raw, code)
	}
	return raw, nil
}

// Only protocol codes cross the process boundary. Reject mixed, extended, or
// exit-inconsistent reports instead of displaying subprocess text as a finding.
func preparationReplayFailure(raw []byte, code int) error {
	invalid := errors.New("Invalid preparation verification report")
	var report map[string]string
	if json.Unmarshal(raw, &report) != nil || len(report) != 2 {
		return invalid
	}
	kind, known := "refusal", []string{"input-over-bound", "input-malformed", "kind-unread"}
	if code == 2 {
		kind = "finding"
		known = []string{"profile-mismatch", "parameter-invalid", "read-failed", "acquisition-missing", "acquisition-unexpected", "acquisition-invalid", "acquisition-stale", "acquisition-sessions", "calculation-invalid", "admission-refused", "preparation-differs"}
	} else if code != 1 {
		return invalid
	}
	if report["result"] != kind || !slices.Contains(known, report[kind]) {
		return invalid
	}
	return errors.New("Offline preparation check: " + report[kind])
}
