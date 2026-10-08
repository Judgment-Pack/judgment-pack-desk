package desk

// The archive rule (the maintainer's decision of 2026-10-08, after the third
// pass of the ADR-0010 line audit, issue #292): no path in Desk removes a
// seed, a next seed or a list of public keys on its own; each moves it to
// Desk's archive of keys with a journal line, and the owner removes it on
// their word. The helpers here read what a signing folder holds at its live
// names and in its archive, holding every archived file to its journal line.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// liveIn is what a signing folder holds at its live names: every name but
// its archive.
func liveIn(t *testing.T, dir string) []string {
	t.Helper()
	var names []string
	for _, name := range namesIn(t, dir) {
		if name != archiveDirName {
			names = append(names, name)
		}
	}
	return names
}

// archivedIn is what the archive of identity id in the signing folder dir
// holds: each file's kind and the rule its journal line names, "kind rule",
// in the order archived. Every file must have its line, with a sentence that
// names no path; anything else fails the test.
func archivedIn(t *testing.T, dir, id string) []string {
	t.Helper()
	folder := filepath.Join(dir, archiveDirName, id)
	lines := map[string]archiveLine{}
	if data, err := os.ReadFile(filepath.Join(folder, archiveJournalName)); err == nil {
		for _, raw := range strings.SplitAfter(string(data), "\n") {
			var line archiveLine
			if raw == "" || json.Unmarshal([]byte(raw), &line) != nil || string(line.line()) != raw {
				continue
			}
			if line.Event == "archived" {
				lines[line.File] = line
			}
		}
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	type entry struct {
		at   time.Time
		name string
		says string
	}
	var entries []entry
	for _, name := range namesIn(t, folder) {
		if name == archiveJournalName {
			continue
		}
		parts := archiveFileForm.FindStringSubmatch(name)
		if parts == nil {
			t.Errorf("the archive of %s holds %s, a name Desk does not write", id, name)
			continue
		}
		line, ok := lines[name]
		switch {
		case !ok:
			t.Errorf("the archive of %s holds %s with no journal line", id, name)
		case strings.TrimSpace(line.Why) == "" || line.Rule == "":
			t.Errorf("the journal line of %s says no rule or no sentence: %+v", name, line)
		case strings.Contains(line.Why, string(filepath.Separator)+"tmp"+string(filepath.Separator)) || strings.Contains(line.Why, dir):
			t.Errorf("the journal line of %s names a path: %s", name, line.Why)
		}
		at, _ := time.Parse(archiveTimeLayout, parts[3])
		entries = append(entries, entry{at, name, parts[4] + " " + line.Rule})
	}
	slices.SortStableFunc(entries, func(a, b entry) int { return a.at.Compare(b.at) })
	var kinds []string
	for _, e := range entries {
		kinds = append(kinds, e.says)
	}
	return kinds
}

// kindsArchived is "kind rule" for each of kinds, in that order, the rule
// for all.
func kindsArchived(rule string, kinds ...string) []string {
	var out []string
	for _, kind := range kinds {
		out = append(out, kind+" "+rule)
	}
	return out
}

// archivedFile is the path of the last file of kind archived under id in the
// signing folder dir, or "" where there is none.
func archivedFile(t *testing.T, dir, id, kind string) string {
	t.Helper()
	folder := filepath.Join(dir, archiveDirName, id)
	found, last := "", time.Time{}
	for _, name := range namesIn(t, folder) {
		parts := archiveFileForm.FindStringSubmatch(name)
		if parts == nil || parts[4] != kind {
			continue
		}
		if at, _ := time.Parse(archiveTimeLayout, parts[3]); found == "" || at.After(last) {
			found, last = filepath.Join(folder, name), at
		}
	}
	return found
}
