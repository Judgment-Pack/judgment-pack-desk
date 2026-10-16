// Package exchange reads immutable catalog releases. It never executes entry
// code, binds credentials, acquires sources, or treats imports as run approval.
package exchange

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/jsonscalar"
	"io"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

const assertion = "The author asserts that these rules derive these facts and this evidence-availability from records of that shape, or these arguments from that disposition, and asserts nothing about any record's truth."

var idPattern = regexp.MustCompile(`^[a-z0-9]+(\.[a-z0-9-]+)+$`)
var digestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var tagPattern = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
var commitPattern = regexp.MustCompile(`^[a-f0-9]{40}([a-f0-9]{24})?$`)
var scans = []*regexp.Regexp{
	regexp.MustCompile(`[Bb]earer[ \t]+[A-Za-z0-9._~+/=-]{20,}`), regexp.MustCompile(`AKIA[0-9A-Z]{16}`), regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{30,}`), regexp.MustCompile(`sk-[A-Za-z0-9_-]{20,}`), regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`), regexp.MustCompile(`/home/|/tmp/`),
}

// Tree is a pinned commit's blobs, never checked-out files or fetched assets.
// Implementations enforce the requested byte bound before returning any bytes.
type Tree interface {
	Files(context.Context) ([]string, error)
	Read(context.Context, string, int) ([]byte, error)
}
type Check func(context.Context, []byte) (document, digest string, err error)
type Fixture struct {
	Name  string   `json:"name"`
	Files []string `json:"files"`
}
type Row struct {
	ID            string    `json:"id"`
	Kind          string    `json:"kind"`
	Path          string    `json:"path"`
	EntryDigest   string    `json:"entryDigest"`
	DialectDigest string    `json:"dialectDigest,omitempty"`
	OutputDigest  string    `json:"outputDigest,omitempty"`
	Fixtures      []Fixture `json:"fixtures"`
}
type Document struct {
	Path          string `json:"path"`
	Version       int    `json:"dialectVersion,omitempty"`
	FormatVersion string `json:"formatVersion,omitempty"`
	Digest        string `json:"digest"`
}
type PackReference struct {
	ID       string   `json:"id"`
	Versions string   `json:"versions,omitempty"`
	Digests  []string `json:"digests,omitempty"`
}
type ArtifactIdentity struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}
type EntryFixture struct {
	Name              string `json:"name"`
	Path              string `json:"path"`
	PreparationDigest string `json:"preparationDigest"`
}
type ReplayInput struct {
	Dialect, Case, Acquisitions, Profiles, Preparation, Binding []byte
	ExpectedDigest                                              string
}
type Replay func(context.Context, ReplayInput) (json.RawMessage, error)
type Preset struct {
	Pack          *PackReference    `json:"pack,omitempty"`
	Artifact      *ArtifactIdentity `json:"artifact,omitempty"`
	Fixtures      []EntryFixture    `json:"fixtures,omitempty"`
	EntryVersion  int               `json:"entryVersion"`
	Kind          string            `json:"kind"`
	ID            string            `json:"id"`
	Title         string            `json:"title"`
	Description   string            `json:"description,omitempty"`
	Direction     string            `json:"direction"`
	Dialect       Document          `json:"dialect"`
	Requires      map[string]string `json:"requires"`
	SourceKinds   []string          `json:"sourceKinds"`
	Licence       string            `json:"licence"`
	Author        string            `json:"author"`
	Assertion     string            `json:"assertion"`
	Verification  []json.RawMessage `json:"verification"`
	Compatibility map[string]string `json:"compatibility,omitempty"`
	Upgrade       map[string]string `json:"upgrade,omitempty"`
}
type Candidate struct {
	Row            Row                        `json:"row"`
	Entry          Preset                     `json:"entry"`
	Files          map[string]string          `json:"files"`
	Document       string                     `json:"document"`
	Digest         string                     `json:"digest"`
	FixtureReports map[string]json.RawMessage `json:"fixtureReports,omitempty"`
}
type Unavailable struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}
type Release struct {
	History     []Pin         `json:"history,omitempty"`
	Location    string        `json:"location"`
	Tag         string        `json:"tag"`
	Commit      string        `json:"commit"`
	Manifest    string        `json:"manifest"`
	Candidates  []Candidate   `json:"candidates"`
	Unavailable []Unavailable `json:"unavailable"`
}

func Digest(raw []byte) string     { return fmt.Sprintf("sha256:%x", sha256.Sum256(raw)) }
func bounded(s string, n int) bool { return len(s) > 0 && utf8.RuneCountInString(s) <= n }

// encoding/json intentionally accepts case aliases and repairs lone UTF-16
// surrogates. Catalog metadata has exact closed keys and Unicode scalar strings.
func exactMembers(raw json.RawMessage, typ reflect.Type) error {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == reflect.TypeFor[json.RawMessage]() {
		return nil
	}
	switch typ.Kind() {
	case reflect.Struct:
		var members map[string]json.RawMessage
		if err := json.Unmarshal(raw, &members); err != nil {
			return err
		}
		allowed := map[string]reflect.Type{}
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			key := strings.Split(field.Tag.Get("json"), ",")[0]
			if key != "" && key != "-" {
				allowed[key] = field.Type
			}
		}
		for key, value := range members {
			field, ok := allowed[key]
			if !ok {
				return fmt.Errorf("unknown metadata member %q", key)
			}
			if err := exactMembers(value, field); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		var values []json.RawMessage
		if err := json.Unmarshal(raw, &values); err != nil {
			return err
		}
		for _, value := range values {
			if err := exactMembers(value, typ.Elem()); err != nil {
				return err
			}
		}
	case reflect.Map:
		var values map[string]json.RawMessage
		if err := json.Unmarshal(raw, &values); err != nil {
			return err
		}
		for _, value := range values {
			if err := exactMembers(value, typ.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}
func strict(raw []byte, into any) error {
	if err := jsonscalar.ValidateEscapes(raw); err != nil {
		return err
	}

	if !utf8.Valid(raw) {
		return errors.New("JSON must be UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 64 {
			return errors.New("JSON exceeds the nesting limit")
		}
		t, e := d.Token()
		if e != nil {
			return e
		}
		if t == nil {
			return errors.New("null is not allowed in catalog metadata")
		}
		switch t {
		case json.Delim('{'):
			seen := map[string]bool{}
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return e
				}
				key, ok := k.(string)
				if !ok || seen[key] {
					return errors.New("duplicate or invalid member")
				}
				seen[key] = true
				if e = walk(depth + 1); e != nil {
					return e
				}
			}
			_, e = d.Token()
			return e
		case json.Delim('['):
			for d.More() {
				if e = walk(depth + 1); e != nil {
					return e
				}
			}
			_, e = d.Token()
			return e
		}
		return nil
	}
	if e := walk(0); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return errors.New("expected one JSON document")
	}
	if err := exactMembers(raw, reflect.TypeOf(into)); err != nil {
		return err
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	d.UseNumber()
	return d.Decode(into)
}
func CompareTags(a, b string) (int, error) {
	if len(a) > 256 || len(b) > 256 || !tagPattern.MatchString(a) || !tagPattern.MatchString(b) {
		return 0, errors.New("choose release tags in vMAJOR.MINOR.PATCH form")
	}
	x, y := strings.Split(a[1:], "."), strings.Split(b[1:], ".")
	for i := range x {
		if len(x[i]) < len(y[i]) {
			return -1, nil
		}
		if len(x[i]) > len(y[i]) {
			return 1, nil
		}
		if n := strings.Compare(x[i], y[i]); n != 0 {
			return n, nil
		}
	}
	return 0, nil
}
func validRange(s string) bool {
	if len(s) > 64 {
		return false
	}
	if _, e := CompareTags(s, s); e == nil {
		return true
	}
	parts := strings.Split(s, " ")
	if len(parts) != 2 || !strings.HasPrefix(parts[0], ">=") || !strings.HasPrefix(parts[1], "<") {
		return false
	}
	n, e := CompareTags(parts[0][2:], parts[1][1:])
	return e == nil && n < 0
}

// Review verifies the entire manifest and directory correspondence before
// offering any preset. Other entry kinds remain explicitly unavailable until
// their fixture replay and output contracts have a supported consumer.
func Review(ctx context.Context, tree Tree, location, tag, commit string, check Check, replays ...Replay) (Release, error) {
	out := Release{Location: location, Tag: tag, Commit: commit, Candidates: []Candidate{}, Unavailable: []Unavailable{}}
	if _, e := CompareTags(tag, tag); e != nil {
		return out, e
	}
	if !commitPattern.MatchString(commit) {
		return out, errors.New("release tag must resolve to a commit")
	}
	manifest, e := tree.Read(ctx, "manifest.json", 1<<20)
	if e != nil {
		return out, e
	}
	if len(manifest) > 1<<20 {
		return out, errors.New("manifest exceeds 1 MiB")
	}
	out.Manifest = string(manifest)
	var rows []Row
	if e = strict(manifest, &rows); e != nil || rows == nil || len(rows) > 1024 {
		return out, errors.New("invalid or oversized catalog manifest")
	}
	var rowMembers []map[string]json.RawMessage
	_ = json.Unmarshal(manifest, &rowMembers)
	files, e := tree.Files(ctx)
	if e != nil {
		return out, e
	}
	sets := map[string][]string{}
	for _, file := range files {
		if !strings.HasPrefix(file, "entries/") {
			continue
		}
		parts := strings.Split(file, "/")
		if len(parts) < 4 {
			return out, errors.New("unexpected file under entries")
		}
		path := strings.Join(parts[:3], "/")
		sets[path] = append(sets[path], file)
	}
	previous := ""
	declared := map[string]bool{}
	for rowIndex, row := range rows {
		parts := strings.SplitN(row.ID, ".", 2)
		if !idPattern.MatchString(row.ID) || len(row.ID) > 128 || row.ID <= previous || row.Path != "entries/"+strings.Join(parts, "/") || !digestPattern.MatchString(row.EntryDigest) || row.Fixtures == nil || len(row.Fixtures) > 64 || row.Kind != "preset" && row.Kind != "dialect" || row.DialectDigest == "" && row.OutputDigest == "" {
			return out, errors.New("invalid catalog manifest row")
		}
		_, hasDialect := rowMembers[rowIndex]["dialectDigest"]
		_, hasOutput := rowMembers[rowIndex]["outputDigest"]
		if hasDialect && !digestPattern.MatchString(row.DialectDigest) || hasOutput && !digestPattern.MatchString(row.OutputDigest) {
			return out, errors.New("invalid manifest digest")
		}
		if row.Kind == "preset" && (row.DialectDigest == "" || hasOutput || len(row.Fixtures) != 0) {
			return out, errors.New("invalid preset manifest row")
		}
		names := map[string]bool{}
		for _, fixture := range row.Fixtures {
			if !regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`).MatchString(fixture.Name) || len(fixture.Name) > 64 || names[fixture.Name] || len(fixture.Files) < 3 || len(fixture.Files) > 5 {
				return out, errors.New("invalid manifest fixture")
			}
			names[fixture.Name] = true
			members := map[string]bool{}
			for _, name := range fixture.Files {
				if !slices.Contains([]string{"case.json", "profiles.json", "preparation.json", "sources.json", "binding.json"}, name) || members[name] {
					return out, errors.New("invalid fixture file list")
				}
				members[name] = true
			}
			for _, name := range []string{"case.json", "profiles.json", "preparation.json"} {
				if !members[name] {
					return out, errors.New("incomplete fixture file list")
				}
			}
		}
		previous = row.ID
		declared[row.Path] = true
		if len(sets[row.Path]) == 0 {
			return out, errors.New("manifest entry directory is missing")
		}
	}
	if len(sets) != len(declared) {
		return out, errors.New("manifest does not enumerate every entry directory")
	}
	identities := make([]Candidate, 0, len(rows))
	for _, row := range rows {
		metadata, err := tree.Read(ctx, row.Path+"/entry.json", 64<<10)
		if err != nil {
			return out, err
		}
		identity, err := catalogIdentity(ctx, tree, row, metadata, check, true)
		if err != nil {
			return out, err
		}
		identities = append(identities, identity)
	}
	totalBytes := 0
	for _, row := range rows {
		if row.Kind != "preset" && (len(replays) == 0 || replays[0] == nil) {
			out.Unavailable = append(out.Unavailable, Unavailable{row.ID, "This entry requires fixture replay or output validation, which this importer does not yet support."})
			continue
		}
		candidate, e := reviewPreset(ctx, tree, row, sets[row.Path], check, replays...)
		if e != nil {
			out.Unavailable = append(out.Unavailable, Unavailable{row.ID, e.Error()})
			continue
		}
		for _, raw := range candidate.Files {
			totalBytes += len(raw)
		}
		if totalBytes > 16<<20 {
			return out, errors.New("catalog import exceeds the retained-file limit")
		}
		out.Candidates = append(out.Candidates, candidate)
	}
	err := checkCatalogHistory(ctx, tree, &out, check, identities)
	return out, err
}
func reviewPreset(ctx context.Context, tree Tree, row Row, files []string, check Check, replays ...Replay) (Candidate, error) {
	c := Candidate{Row: row, Files: map[string]string{}}
	want := []string{row.Path + "/README.md", row.Path + "/dialect.json", row.Path + "/entry.json"}
	if row.Kind == "dialect" {
		for _, f := range row.Fixtures {
			for _, name := range f.Files {
				want = append(want, row.Path+"/fixtures/"+f.Name+"/"+name)
			}
		}
	}
	slices.Sort(files)
	slices.Sort(want)
	if !slices.Equal(files, want) {
		return c, errors.New("entry files do not match its declared manifest")
	}
	retainedBytes := 0
	for _, path := range want {
		name := strings.TrimPrefix(path, row.Path+"/")
		limit := 64 << 10
		if name == "dialect.json" {
			limit = 128 << 10
		}
		if strings.HasPrefix(name, "fixtures/") {
			limit = 2 << 20
			if strings.HasSuffix(name, "/preparation.json") {
				limit = 8 << 20
			}
			if strings.HasSuffix(name, "/profiles.json") || strings.HasSuffix(name, "/binding.json") {
				limit = 64 << 10
			}
		}
		raw, e := tree.Read(ctx, row.Path+"/"+name, limit)
		if e != nil {
			return c, e
		}
		if len(raw) > limit || !utf8.Valid(raw) {
			return c, errors.New("entry file is oversized or not UTF-8")
		}
		for _, pattern := range scans {
			if pattern.Match(raw) {
				return c, errors.New("entry contains a recognized credential or local path")
			}
		}
		retainedBytes += len(raw)
		if retainedBytes > 16<<20 {
			return c, errors.New("entry exceeds the retained-file limit")
		}
		c.Files[name] = string(raw)
	}
	if Digest([]byte(c.Files["entry.json"])) != row.EntryDigest || Digest([]byte(c.Files["dialect.json"])) != row.DialectDigest {
		return c, errors.New("entry file digest mismatch")
	}
	// Entry and document versions are separate contracts. Read the exact
	// selector before interpreting closed metadata; old retained imports are
	// not re-imported or rewritten through this current-catalog reader.
	var selector map[string]json.RawMessage
	if e := strict([]byte(c.Files["entry.json"]), &selector); e != nil {
		return c, errors.New("invalid preset metadata")
	}
	if string(selector["entryVersion"]) != "3" {
		return c, errors.New("entry-version-unread: this importer requires Exchange entry format 3")
	}
	var entry Preset
	if e := strict([]byte(c.Files["entry.json"]), &entry); e != nil {
		return c, errors.New("invalid preset metadata")
	}
	var entryMembers map[string]json.RawMessage
	_ = json.Unmarshal([]byte(c.Files["entry.json"]), &entryMembers)
	if _, present := entryMembers["description"]; present && !bounded(entry.Description, 4096) {
		return c, errors.New("invalid preset description")
	}
	if entry.EntryVersion != 3 || entry.Kind != row.Kind || entry.ID != row.ID || entry.Direction != "input" || !bounded(entry.Title, 200) || entry.Description != "" && !bounded(entry.Description, 4096) || entry.Dialect.Path != "dialect.json" || (row.Kind == "preset" && (entry.Dialect.Version != 3 || entry.Dialect.FormatVersion != "")) || !digestPattern.MatchString(entry.Dialect.Digest) || !bounded(entry.Author, 100) || entry.Assertion != assertion || entry.Verification == nil || len(entry.Verification) > 32 || (row.Kind == "preset" && len(entry.Verification) != 0) || entry.Requires == nil || len(entry.SourceKinds) == 0 || len(entry.SourceKinds) > 16 || len(entry.Licence) > 64 || !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+-]*$`).MatchString(entry.Licence) {
		return c, errors.New("invalid preset metadata")
	}
	if row.Kind == "preset" && (len(entry.Requires) > 1 || len(entry.Requires) == 1 && (entry.Requires["platform"] == "" || len(entry.Requires["platform"]) > 64 || !regexp.MustCompile(`^[a-z][a-z0-9-]*$`).MatchString(entry.Requires["platform"]))) {
		return c, errors.New("a preset cannot carry installation profiles")
	}
	if entry.Compatibility != nil && (len(entry.Compatibility) != 2 || !validRange(entry.Compatibility["desk"]) || !validRange(entry.Compatibility["runner"])) {
		return c, errors.New("invalid compatibility statement")
	}
	if entry.Upgrade != nil {
		if len(entry.Upgrade) < 1 || len(entry.Upgrade) > 32 {
			return c, errors.New("invalid upgrade notes")
		}
		for digest, note := range entry.Upgrade {
			if !digestPattern.MatchString(digest) || !bounded(note, 2048) {
				return c, errors.New("invalid upgrade note")
			}
		}
		// Historical references are checked after every current entry is read.
	}
	if err := entryKindForm(entry, entryMembers); err != nil {
		return c, err
	}
	// Runtime validates the actual document; this consumer additionally holds
	// the preset-specific unbound-only and manifest source-kind constraints.
	var doc struct {
		Version       json.RawMessage              `json:"version"`
		FormatVersion string                       `json:"formatVersion"`
		ID            string                       `json:"id"`
		Sources       []map[string]json.RawMessage `json:"sources"`
	}
	if json.Unmarshal([]byte(c.Files["dialect.json"]), &doc) != nil || (row.Kind == "preset" && string(doc.Version) != "3") {
		return c, errors.New("a preset requires a version-3 dialect")
	}
	kinds := map[string]bool{}
	for _, source := range doc.Sources {
		var kind string
		if json.Unmarshal(source["kind"], &kind) != nil {
			return c, errors.New("invalid source kind")
		}
		kinds[kind] = true
		if row.Kind == "preset" && (kind == "operation" || kind == "research") {
			for _, member := range []string{"profile", "profileDigest", "webProfiles"} {
				if _, present := source[member]; present {
					return c, errors.New("preset sources must be unbound")
				}
			}
		}
	}
	if len(kinds) != len(entry.SourceKinds) {
		return c, errors.New("sourceKinds does not match the dialect")
	}
	seen := map[string]bool{}
	for _, kind := range entry.SourceKinds {
		if seen[kind] || !kinds[kind] {
			return c, errors.New("sourceKinds does not match the dialect")
		}
		seen[kind] = true
	}
	if check == nil {
		return c, errors.New("Runtime document checks are unavailable")
	}
	document, digest, e := check(ctx, []byte(c.Files["dialect.json"]))
	if e != nil {
		return c, e
	}
	if digest != entry.Dialect.Digest || Digest([]byte(document)) != digest {
		return c, errors.New("canonical dialect digest mismatch")
	}
	if row.Kind == "dialect" {
		if err := checkInputReference(entry, []byte(c.Files["dialect.json"])); err != nil {
			return c, err
		}
		if len(replays) == 0 || replays[0] == nil {
			return c, errors.New("Offline preparation verification is unavailable")
		}
		reports, err := replayFixtures(ctx, c.Files, row, entry, replays[0])
		if err != nil {
			return c, err
		}
		c.FixtureReports = reports
	}
	c.Entry = entry
	c.Document = document
	c.Digest = digest
	return c, nil
}
