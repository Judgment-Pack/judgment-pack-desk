package exchange

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
)

func entryKindForm(e Preset, members map[string]json.RawMessage) error {
	if e.Kind == "preset" {
		for _, key := range []string{"pack", "artifact", "fixtures"} {
			if _, ok := members[key]; ok {
				return errors.New("a preset has no pack, artifact identity or fixtures")
			}
		}
		return nil
	}
	if e.Pack == nil || e.Fixtures == nil || len(e.Fixtures) < 1 || len(e.Fixtures) > 64 {
		return errors.New("an input dialect requires a pack reference and bounded fixtures")
	}
	u, err := url.Parse(e.Pack.ID)
	if err != nil || u.Scheme == "" || len(e.Pack.ID) > 2048 {
		return errors.New("invalid pack identity")
	}
	if (e.Pack.Versions == "") == (e.Pack.Digests == nil) || len(e.Pack.Versions) > 64 || len(e.Pack.Digests) > 64 {
		return errors.New("a pack reference requires versions or digests")
	}
	for _, d := range e.Pack.Digests {
		if !digestPattern.MatchString(d) {
			return errors.New("invalid pack digest")
		}
	}
	if e.Pack.Digests != nil && len(e.Pack.Digests) == 0 {
		return errors.New("empty pack digests")
	}
	for name, value := range e.Requires {
		switch name {
		case "platform":
			if !regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`).MatchString(value) {
				return errors.New("invalid platform")
			}
		case "inputProfile", "connectionProfile":
			if !digestPattern.MatchString(value) {
				return errors.New("invalid required profile digest")
			}
		default:
			return errors.New("unknown installation requirement")
		}
	}
	// Verification records are publisher claims, not execution instructions.
	for _, raw := range e.Verification {
		var v struct {
			Date    string   `json:"date"`
			Runner  string   `json:"runner"`
			Runtime string   `json:"runtime"`
			Readers []string `json:"readers"`
			Result  string   `json:"result"`
		}
		if strict(raw, &v) != nil || !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`).MatchString(v.Date) || !tagPattern.MatchString(v.Runner) || !tagPattern.MatchString(v.Runtime) || v.Result != "agree" || len(v.Readers) < 1 || len(v.Readers) > 8 {
			return errors.New("invalid verification record")
		}
		for _, reader := range v.Readers {
			if len(reader) > 256 || !regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+/[A-Za-z0-9_./-]+@[a-f0-9]{40}$`).MatchString(reader) {
				return errors.New("invalid verifier locator")
			}
		}
	}
	return nil
}
func checkInputReference(e Preset, raw []byte) error {
	var d struct {
		Version       json.RawMessage `json:"version"`
		FormatVersion string          `json:"formatVersion"`
		ID            string          `json:"id"`
	}
	if json.Unmarshal(raw, &d) != nil {
		return errors.New("invalid dialect")
	}
	if (e.Dialect.Version == 0) == (e.Dialect.FormatVersion == "") {
		return errors.New("dialect-reference-form")
	}
	if e.Dialect.FormatVersion != "" {
		if e.Dialect.FormatVersion != "2.0.0" && e.Dialect.FormatVersion != "3.0.0" {
			return errors.New("dialect-reference-form")
		}
		if d.FormatVersion != e.Dialect.FormatVersion {
			return errors.New("dialect-format-mismatch")
		}
		if e.Artifact == nil {
			return errors.New("artifact-required")
		}
		var version string
		if json.Unmarshal(d.Version, &version) != nil || e.Artifact.ID != d.ID || e.Artifact.Version != version {
			return errors.New("artifact-mismatch")
		}
	} else {
		if e.Dialect.Version < 2 {
			return errors.New("dialect-reference-form")
		}
		if d.FormatVersion != "" || string(d.Version) != fmt.Sprint(e.Dialect.Version) {
			return errors.New("dialect-format-mismatch")
		}
		if e.Artifact != nil {
			return errors.New("artifact-forbidden")
		}
	}
	return nil
}
func replayFixtures(ctx context.Context, files map[string]string, row Row, entry Preset, replay Replay) (map[string]json.RawMessage, error) {
	if len(row.Fixtures) != len(entry.Fixtures) {
		return nil, errors.New("manifest fixtures differ from the entry")
	}
	reports := map[string]json.RawMessage{}
	for _, fixture := range entry.Fixtures {
		if fixture.Path != "fixtures/"+fixture.Name || !digestPattern.MatchString(fixture.PreparationDigest) || reports[fixture.Name] != nil {
			return nil, errors.New("invalid fixture reference")
		}
		found := false
		for _, f := range row.Fixtures {
			found = found || f.Name == fixture.Name
		}
		if !found {
			return nil, errors.New("undeclared fixture")
		}
		input := ReplayInput{Dialect: []byte(files["dialect.json"]), Case: []byte(files[fixture.Path+"/case.json"]), Profiles: []byte(files[fixture.Path+"/profiles.json"]), Preparation: []byte(files[fixture.Path+"/preparation.json"]), ExpectedDigest: fixture.PreparationDigest}
		if raw, ok := files[fixture.Path+"/sources.json"]; ok {
			input.Acquisitions = []byte(raw)
		}
		if raw, ok := files[fixture.Path+"/binding.json"]; ok {
			input.Binding = []byte(raw)
		}
		if Digest(input.Preparation) != input.ExpectedDigest {
			return nil, errors.New("fixture preparation digest mismatch")
		}
		raw, err := replay(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("Fixture %s: %w", fixture.Name, err)
		}
		var report struct {
			Result  string `json:"result"`
			Digest  string `json:"digest"`
			Binding string `json:"binding"`
		}
		if json.Unmarshal(raw, &report) != nil || report.Result != "reading" || report.Digest != input.ExpectedDigest || report.Binding != "not-checked" {
			return nil, fmt.Errorf("Fixture %s did not replay to the expected preparation", fixture.Name)
		}
		reports[fixture.Name] = raw
	}
	return reports, nil
}

// Artifact pairs are immutable within one release and against retained imports.
func CheckArtifactPairs(candidates []Candidate) error {
	seen := map[string]string{}
	for _, c := range candidates {
		if c.Entry.Artifact == nil {
			continue
		}
		key := c.Entry.Artifact.ID + "\x00" + c.Entry.Artifact.Version
		if old, ok := seen[key]; ok && old != c.Digest {
			return errors.New("artifact-content-changed")
		}
		seen[key] = c.Digest
	}
	return nil
}
