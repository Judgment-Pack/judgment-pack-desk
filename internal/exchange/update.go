package exchange

import (
	"errors"
	"slices"
)

type Pin struct {
	Location string `json:"location"`
	Tag      string `json:"tag"`
	Commit   string `json:"commit"`
}

// CheckReleasePins refuses moved tags and downgrades across all retained tags,
// not just the currently selected entry. Comparing decimal components uses no
// machine integer or floating-point arithmetic.
func CheckReleasePins(next Release, pins []Pin) error {
	for _, pin := range pins {
		if pin.Location != next.Location {
			continue
		}
		order, e := CompareTags(next.Tag, pin.Tag)
		if e != nil {
			return e
		}
		if next.Tag == pin.Tag && next.Commit != pin.Commit {
			return errors.New("this release tag moved from its retained commit")
		}
		if order < 0 {
			return errors.New("a lower catalog release cannot replace the retained release")
		}
	}
	return nil
}

type Change struct {
	Withdrawn     bool              `json:"withdrawn"`
	Files         []string          `json:"files"`
	UpgradeNote   string            `json:"upgradeNote,omitempty"`
	Compatibility map[string]string `json:"compatibility,omitempty"`
}

func Changes(old Candidate, next Release) Change {
	for _, candidate := range next.Candidates {
		if candidate.Row.ID != old.Row.ID {
			continue
		}
		change := Change{Files: []string{}, UpgradeNote: candidate.Entry.Upgrade[old.Row.EntryDigest], Compatibility: candidate.Entry.Compatibility}
		names := map[string]bool{}
		for name := range old.Files {
			names[name] = true
		}
		for name := range candidate.Files {
			names[name] = true
		}
		for name := range names {
			if a, aok := old.Files[name]; a != candidate.Files[name] || aok != hasFile(candidate.Files, name) {
				change.Files = append(change.Files, name)
			}
		}
		slices.Sort(change.Files)
		return change
	}
	// Malformed/unsupported entries are not withdrawals. They cannot replace the
	// retained entry; the UI must show their refusal, preserving the import.
	for _, unavailable := range next.Unavailable {
		if unavailable.ID == old.Row.ID {
			return Change{Files: []string{"unavailable"}}
		}
	}
	return Change{Withdrawn: true, Files: []string{}}
}
func hasFile(files map[string]string, name string) bool { _, ok := files[name]; return ok }
