package exchange

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

type HistoricalTree struct {
	Tag, Commit string
	Tree        Tree
}
type HistoryTree interface {
	PriorReleases(context.Context, string) ([]HistoricalTree, error)
}

// Named artifact identities and upgrade notes are checked against earlier tags.
// Missing or excessive history is a refusal, never an assumed first release.
func checkCatalogHistory(ctx context.Context, tree Tree, out *Release, check Check, identities ...[]Candidate) error {
	current := out.Candidates
	if len(identities) != 0 {
		current = identities[0]
	}
	needed := false
	for _, c := range current {
		needed = needed || c.Entry.Artifact != nil || c.Entry.Upgrade != nil
	}
	if !needed {
		return CheckArtifactPairs(current)
	}
	history, ok := tree.(HistoryTree)
	if !ok {
		return errors.New("Catalog history is unavailable for named artifacts")
	}
	prior, err := history.PriorReleases(ctx, out.Tag)
	if err != nil {
		return err
	}
	if len(prior) > 32 {
		return errors.New("Catalog history exceeds 32 releases")
	}
	pairs := map[string]string{}
	entries := map[string]map[string]bool{}
	retained := 0
	for _, release := range prior {
		if n, e := CompareTags(release.Tag, out.Tag); e != nil || n >= 0 || !commitPattern.MatchString(release.Commit) {
			return errors.New("Invalid prior catalog release")
		}
		out.History = append(out.History, Pin{Location: out.Location, Tag: release.Tag, Commit: release.Commit})
		raw, e := release.Tree.Read(ctx, "manifest.json", 1<<20)
		if e != nil {
			return e
		}
		retained += len(raw)
		if retained > 32<<20 {
			return errors.New("Catalog history exceeds its byte limit")
		}
		var rows []Row
		if strict(raw, &rows) != nil || rows == nil || len(rows) > 1024 {
			return errors.New("Invalid historical manifest")
		}
		previous := ""
		for _, row := range rows {
			parts := strings.SplitN(row.ID, ".", 2)
			if !idPattern.MatchString(row.ID) || row.ID <= previous || row.Path != "entries/"+strings.Join(parts, "/") || !digestPattern.MatchString(row.EntryDigest) {
				return errors.New("Invalid historical entry")
			}
			previous = row.ID
			metadata, e := release.Tree.Read(ctx, row.Path+"/entry.json", 64<<10)
			if e != nil {
				return e
			}
			if Digest(metadata) != row.EntryDigest {
				return errors.New("Historical entry digest mismatch")
			}
			retained += len(metadata)
			if retained > 32<<20 {
				return errors.New("Catalog history exceeds its byte limit")
			}
			entry, e := catalogIdentity(ctx, release.Tree, row, metadata, check, false)
			if e != nil {
				return e
			}
			if entries[row.ID] == nil {
				entries[row.ID] = map[string]bool{}
			}
			entries[row.ID][row.EntryDigest] = true
			if entry.Entry.Artifact != nil {
				key := entry.Entry.Artifact.ID + "\x00" + entry.Entry.Artifact.Version
				if before, seen := pairs[key]; seen && before != entry.Digest {
					return errors.New("artifact-content-changed")
				}
				pairs[key] = entry.Digest
			}
		}
	}
	for _, c := range current {
		if c.Entry.Artifact != nil {
			key := c.Entry.Artifact.ID + "\x00" + c.Entry.Artifact.Version
			if before, seen := pairs[key]; seen && before != c.Digest {
				return errors.New("artifact-content-changed")
			}
			pairs[key] = c.Digest
		}
		for digest := range c.Entry.Upgrade {
			if !entries[c.Entry.ID][digest] {
				return errors.New("Upgrade note names no earlier entry digest for this entry")
			}
		}
	}
	return nil
}

// Fetch history only into the disposable bare tree; never check out or execute
// catalog contents. Each opened tag goes through the existing object verifier.
func (t *GitTree) PriorReleases(ctx context.Context, tag string) ([]HistoricalTree, error) {
	var raw []byte
	var err error
	if t.remote != "" {
		raw, err = t.git(ctx, 32<<10, "ls-remote", "--refs", "--tags", t.remote)
	} else {
		raw, err = t.git(ctx, 32<<10, "for-each-ref", "--format=%(objectname) %(refname)", "refs/tags/")
	}
	if err != nil {
		return nil, err
	}
	tags := []string{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || !strings.HasPrefix(fields[1], "refs/tags/") {
			continue
		}
		name := strings.TrimPrefix(fields[1], "refs/tags/")
		if !tagPattern.MatchString(name) {
			continue
		}
		order, e := CompareTags(name, tag)
		if e != nil {
			return nil, e
		}
		if order < 0 {
			tags = append(tags, name)
		}
	}
	if len(tags) > 32 {
		return nil, errors.New("Catalog history exceeds 32 releases")
	}
	slices.SortFunc(tags, func(a, b string) int { n, _ := CompareTags(a, b); return n })
	result := []HistoricalTree{}
	for _, name := range tags {
		if t.remote != "" {
			if _, err = t.git(ctx, 4096, "fetch", "--quiet", "--depth=1", "--no-tags", "--no-recurse-submodules", t.remote, "refs/tags/"+name+":refs/tags/"+name); err != nil {
				return nil, err
			}
		}
		prior, close, e := OpenRelease(ctx, t.directory, name)
		if e != nil {
			return nil, e
		}
		close() // Local trees have no disposable directory of their own.
		result = append(result, HistoricalTree{name, prior.Commit, prior})
	}
	return result, nil
}

// Publication identity is checked before availability. A failing or unsupported
// fixture cannot hide a conflicting pair in another row of the same release.
func catalogIdentity(ctx context.Context, tree Tree, row Row, metadata []byte, check Check, current bool) (Candidate, error) {
	c := Candidate{Row: row}
	if Digest(metadata) != row.EntryDigest {
		return c, errors.New("artifact-history-invalid: entry digest mismatch")
	}
	var members map[string]json.RawMessage
	if strict(metadata, &members) != nil || members == nil {
		return c, errors.New("artifact-history-invalid: malformed entry")
	}
	if string(members["entryVersion"]) != "3" {
		return c, nil
	}
	if v, ok := members["upgrade"]; ok {
		if strict(v, &c.Entry.Upgrade) != nil {
			return c, errors.New("invalid upgrade notes")
		}
	}
	c.Entry.ID = row.ID
	var reference map[string]json.RawMessage
	if v, ok := members["dialect"]; ok {
		if strict(v, &reference) != nil {
			return c, errors.New("artifact-history-invalid: malformed reference")
		}
	}
	if _, named := reference["formatVersion"]; !named {
		if _, declares := members["artifact"]; current && declares {
			return c, errors.New("artifact-forbidden")
		}
		return c, nil
	}
	if strict(members["dialect"], &c.Entry.Dialect) != nil || strict(members["artifact"], &c.Entry.Artifact) != nil || c.Entry.Artifact == nil || c.Entry.Dialect.Path != "dialect.json" || !digestPattern.MatchString(c.Entry.Dialect.Digest) {
		return c, errors.New("artifact-history-invalid: malformed named reference")
	}
	if _, mixed := reference["dialectVersion"]; mixed {
		return c, errors.New("dialect-reference-form")
	}
	raw, err := tree.Read(ctx, row.Path+"/dialect.json", 128<<10)
	if err != nil {
		return c, err
	}
	if Digest(raw) != row.DialectDigest {
		return c, errors.New("artifact-history-invalid: dialect digest mismatch")
	}
	if err = checkInputReference(c.Entry, raw); err != nil {
		return c, err
	}
	if check == nil {
		return c, errors.New("Runtime document checks are unavailable")
	}
	c.Document, c.Digest, err = check(ctx, raw)
	if err != nil {
		return c, err
	}
	if c.Digest != c.Entry.Dialect.Digest {
		return c, errors.New("artifact-history-invalid: canonical digest mismatch")
	}
	return c, nil
}
