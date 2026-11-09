package desk

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func TestStorageCensusCoveragePartition(t *testing.T) {
	rows := []struct {
		name    string
		parent  storageCensusControlParent
		file    string
		covered bool
	}{
		{"q-lock", storageCensusQParent, "lock", true},
		{"q-checkpoint-a", storageCensusQParent, "checkpoint.a", true},
		{"q-checkpoint-b", storageCensusQParent, "checkpoint.b", true},
		{"q-tickets-a", storageCensusQParent, "tickets.a", true},
		{"q-tickets-b", storageCensusQParent, "tickets.b", true},
		{"q-manifests", storageCensusQParent, "manifests", true},
		{"config-home-lease", storageCensusConfigParent, ".home-use.lock", true},
		{"config-bootstrap", storageCensusConfigParent, ".storage-bootstrap.lock", true},
		{"config-use", storageCensusConfigParent, ".storage-use.lock", true},
		{"config-claim", storageCensusConfigParent, "storage-installation.claim.json", true},
		{"config-record", storageCensusConfigParent, "storage-installation.json", true},
		{"config-intents", storageCensusConfigParent, "storage-host-intents", true},
		{"config-move-intent-bounded", storageCensusConfigParent, projectMoveName, true},
		{"config-ready-is-actual", storageCensusConfigParent, "storage-installation.ready", false},
		{"config-catalog-a-is-actual", storageCensusConfigParent, "storage-owner.a", false},
		{"config-catalog-b-is-actual", storageCensusConfigParent, "storage-owner.b", false},
		{"domain-name-alone-is-not-covered", storageCensusConfigParent, ".data.lock", false},
		{"work-name-alone-is-not-covered", storageCensusConfigParent, ".data-work.lock", false},
		{"config-cannot-cover-q-name", storageCensusConfigParent, "checkpoint.a", false},
		{"q-cannot-cover-config-name", storageCensusQParent, "storage-host-intents", false},
		{"nested-q-name", storageCensusQParent, "nested/manifests", false},
		{"nested-config-name", storageCensusConfigParent, "nested/storage-host-intents", false},
		{"unknown-control", storageCensusConfigParent, "unknown-control", false},
		{"uncovered-parent", storageCensusUncovered, "storage-host-intents", false},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			if got := row.parent.covers(row.file); got != row.covered {
				t.Fatal("literal per-parent partition differs")
			}
		})
	}
}

func storageCoverageCensusTotal(c storagebudget.Census) (v storagebudget.Vector) {
	for _, r := range c.Roots {
		for _, u := range r.Usage {
			v.Bytes += u.Bytes
			v.Files += u.Files
			v.Directories += u.Directories
		}
	}
	return v
}
func storageCoverageSizedFile(t *testing.T, path string, size int64) {
	t.Helper()
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	storageTestMust(t, e)
	e = f.Truncate(size)
	ce := f.Close()
	storageTestMust(t, errors.Join(e, ce))
}
func TestStorageCensusCoverageActualCharges(t *testing.T) {
	for _, name := range []string{"unchanged-complete-installation", "catalog-two-pages-at-config", "wrong-parent-control-names", "nested-control-names", "unknown-and-unselected-work-lock", "child-root-cannot-borrow-provision"} {
		t.Run(name, func(t *testing.T) {
			o, configRoot, config := storageWriterFixture(t, false)
			want := storagebudget.Vector{Bytes: (64 + 4) << 20, Files: 32, Directories: 5}
			switch name {
			case "catalog-two-pages-at-config":
				storageCoverageSizedFile(t, filepath.Join(config, "storage-owner.a"), 8<<20)
				storageCoverageSizedFile(t, filepath.Join(config, "storage-owner.b"), 8<<20)
				want.Bytes += 16 << 20
				want.Files += 2
			case "wrong-parent-control-names":
				storageTestMust(t, os.WriteFile(filepath.Join(config, "checkpoint.a"), []byte("cfg"), 0600))
				storageTestMust(t, os.WriteFile(filepath.Join(config, storageAnchorName, storageIntentName), []byte("qfil"), 0600))
				want.Bytes += 7
				want.Files += 2
			case "nested-control-names":
				storageTestMust(t, os.Mkdir(filepath.Join(config, "nested"), 0700))
				for file, data := range map[string]string{storageIntentName: "12345", storageReadyName: "x", "storage-owner.a": "1234567"} {
					storageTestMust(t, os.WriteFile(filepath.Join(config, "nested", file), []byte(data), 0600))
				}
				want.Bytes += 13
				want.Files += 3
				want.Directories++
			case "unknown-and-unselected-work-lock":
				storageTestMust(t, os.WriteFile(filepath.Join(config, "unknown-control"), []byte("abc"), 0600))
				storageTestMust(t, os.WriteFile(filepath.Join(config, ".data-work.lock"), nil, 0600))
				want.Bytes += 3
				want.Files += 2
			case "child-root-cannot-borrow-provision":
				storageTestMust(t, os.Mkdir(filepath.Join(config, "history"), 0700))
				storageTestMust(t, os.WriteFile(filepath.Join(config, "history", ".history.lock"), nil, 0600))
				storageTestMust(t, os.WriteFile(filepath.Join(config, "history", "manifests"), []byte("abc"), 0600))
				_, e := o.childRoot(t.Context(), configRoot, "history", storagebudget.History, nil, ".history.lock")
				storageTestMust(t, e)
				want.Bytes += 3
				want.Files += 2
				want.Directories++
			}
			c, e := o.measureCensus(t.Context(), o.orderedRoots())
			storageTestMust(t, e)
			if got := storageCoverageCensusTotal(c); got != want {
				t.Fatalf("actual charges differ: got%+v want%+v", got, want)
			}
			for _, r := range c.Roots {
				if r.ID == configRoot.row.ID && name == "catalog-two-pages-at-config" && r.Usage[storagebudget.Control] != (storagebudget.Vector{Bytes: 20 << 20, Files: 18, Directories: 4}) {
					t.Fatal("catalog charge borrowed Q provision")
				}
				if r.ID != configRoot.row.ID && name == "catalog-two-pages-at-config" && r.Usage[storagebudget.Control] != (storagebudget.Vector{Bytes: 64 << 20, Files: 16, Directories: 1}) {
					t.Fatal("Q provision changed for config catalogs")
				}
			}
		})
	}
}

func TestStorageCensusCoverageOriginalParentAndDomain(t *testing.T) {
	for _, name := range []string{"retained-original-domain", "retained-custom-domain", "string-without-witness", "other-parent-domain-witness", "config-cannot-borrow-q-floor", "q-cannot-borrow-config-floor", "two-parent-flags-refuse"} {
		t.Run(name, func(t *testing.T) {
			o, r, config := storageWriterFixture(t, false)
			if name == "retained-custom-domain" {
				// This is a separate original fixture, not mutation of an enrolled
				// domain. Its original constructor retains the exact named file.
				ref, dir := storageTestInstallation(t)
				storageTestMust(t, os.WriteFile(filepath.Join(dir, ".original-domain"), nil, 0600))
				other, e := newStorageBudgetOwner(ref, storageSyntheticFilesystem{}, storageBudgetOptions{HeadroomBytes: 16 << 20, HeadroomInodes: 100})
				storageTestMust(t, e)
				t.Cleanup(func() { storageTestMust(t, other.Close()) })
				r, e = other.configRoot(t.Context(), ".original-domain")
				storageTestMust(t, e)
				info, e := r.root.Lstat(".original-domain")
				storageTestMust(t, e)
				covered, e := r.censusCovered(t.Context(), ".original-domain", info)
				storageTestMust(t, e)
				if !covered {
					t.Fatal("actual selected literal domain lost coverage")
				}
				return
			}
			beforeName, beforeDomain, beforeConfig, beforeQ := r.domainName, r.domain, r.configControl, r.qControl
			defer func() {
				r.domainName, r.domain, r.configControl, r.qControl = beforeName, beforeDomain, beforeConfig, beforeQ
			}()
			switch name {
			case "retained-original-domain":
				info, e := r.root.Lstat(".data.lock")
				storageTestMust(t, e)
				covered, e := r.censusCovered(t.Context(), ".data.lock", info)
				storageTestMust(t, e)
				if !covered {
					t.Fatal("original retained domain lost coverage")
				}
				return
			case "string-without-witness":
				storageTestMust(t, os.WriteFile(filepath.Join(config, ".invented-domain"), nil, 0600))
				r.domainName = ".invented-domain"
				r.domain = storageNamedFile{}
				info, e := r.root.Lstat(r.domainName)
				storageTestMust(t, e)
				covered, e := r.censusCovered(t.Context(), r.domainName, info)
				storageTestMust(t, e)
				if covered {
					t.Fatal("string conferred coverage")
				}
				return
			case "other-parent-domain-witness":
				root, e := os.OpenRoot(t.TempDir())
				storageTestMust(t, e)
				defer root.Close()
				storageTestMust(t, root.WriteFile(".data.lock", nil, 0600))
				n, e := storageOpenFile(root, ".data.lock", 0)
				storageTestMust(t, e)
				defer n.file.Close()
				r.domain = n
				info, e := r.root.Lstat(".data.lock")
				storageTestMust(t, e)
				covered, e := r.censusCovered(t.Context(), ".data.lock", info)
				if e == nil || covered {
					t.Fatal("foreign parent witness conferred coverage")
				}
				return
			case "config-cannot-borrow-q-floor":
				r.configControl = false
				r.qControl = true
			case "q-cannot-borrow-config-floor":
				for _, candidate := range o.orderedRoots() {
					if candidate.qControl {
						q := *candidate
						q.qControl = false
						q.configControl = true
						if _, e := q.censusControlParent(); e == nil {
							t.Fatal("Q borrowed config provision")
						}
						return
					}
				}
				t.Fatal("fixture missing Q root")
			case "two-parent-flags-refuse":
				r.configControl = true
				r.qControl = true
			}
			c, e := o.measureCensus(t.Context(), o.orderedRoots())
			if e == nil || !reflect.DeepEqual(c, storagebudget.Census{}) {
				t.Fatal("wrong parent provision accepted/nonzero failed census")
			}
		})
	}
}

func TestStorageCensusReadyLexicalCoverage(t *testing.T) {
	for _, name := range []string{"valid-exact-once", "absent-provision-reserved", "short", "long", "wrong-content", "directory", "symlink", "hardlink", "nonprivate", "replacement-during-hold", "content-change-during-hold", "charge-error", "equivalent-between-holds"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			storageTestMust(t, os.Chmod(dir, 0700))
			root, e := os.OpenRoot(dir)
			storageTestMust(t, e)
			defer root.Close()
			binding := storagebudget.Digest{1, 2, 3}
			if name == "absent-provision-reserved" {
				if _, e := root.Lstat(storageReadyName); !errors.Is(e, os.ErrNotExist) {
					t.Fatal("Ready unexpectedly present")
				}
				if v := storageCensusConfigParent.provision(false); v != (storagebudget.Vector{Bytes: 4 << 20, Files: 16, Directories: 4}) {
					t.Fatal("absent bootstrap Ready lost reserved provision")
				}
				return
			}
			data := append([]byte{}, binding[:]...)
			switch name {
			case "short":
				data = data[:31]
			case "long":
				data = append(data, 0)
			case "wrong-content":
				data[0] ^= 1
			}
			if name == "directory" {
				storageTestMust(t, root.Mkdir(storageReadyName, 0700))
			} else if name == "symlink" {
				storageTestMust(t, root.WriteFile("target", data, 0600))
				storageTestMust(t, os.Symlink("target", filepath.Join(dir, storageReadyName)))
			} else {
				storageTestMust(t, root.WriteFile(storageReadyName, data, 0600))
			}
			if name == "hardlink" {
				storageTestMust(t, os.Link(filepath.Join(dir, storageReadyName), filepath.Join(dir, "alias")))
			}
			if name == "nonprivate" {
				storageTestMust(t, root.Chmod(storageReadyName, 0644))
			}
			before, e := root.Lstat(storageReadyName)
			storageTestMust(t, e)
			calls := 0
			sentinel := errors.New("handwritten census charge failure")
			charge := func(storageDirectoryIdentity) error {
				calls++
				switch name {
				case "replacement-during-hold":
					storageTestMust(t, root.WriteFile("replacement", binding[:], 0600))
					storageTestMust(t, root.Rename("replacement", storageReadyName))
				case "content-change-during-hold":
					storageTestMust(t, root.WriteFile(storageReadyName, make([]byte, 32), 0600))
				case "charge-error":
					return sentinel
				}
				return nil
			}
			e = storageCensusReady(root, binding, before, charge)
			if name == "valid-exact-once" || name == "equivalent-between-holds" {
				storageTestMust(t, e)
				if calls != 1 {
					t.Fatal("actual Ready namespace not charged once")
				}
				got, e := storageVectorAdd(storageCensusConfigParent.provision(true), storagebudget.Vector{Bytes: 32, Files: 1})
				storageTestMust(t, e)
				if got != (storagebudget.Vector{Bytes: 4 << 20, Files: 16, Directories: 4}) {
					t.Fatal("actual Ready doubled existing provision")
				}
				if name == "equivalent-between-holds" {
					storageTestMust(t, root.WriteFile("replacement", binding[:], 0600))
					storageTestMust(t, root.Rename("replacement", storageReadyName))
					next, e := root.Lstat(storageReadyName)
					storageTestMust(t, e)
					if os.SameFile(before, next) {
						t.Fatal("fixture did not replace inode")
					}
					storageTestMust(t, storageCensusReady(root, binding, next, func(storageDirectoryIdentity) error { calls++; return nil }))
					if calls != 2 {
						t.Fatal("fresh lexical hold did not measure current object once")
					}
				}
			} else {
				if e == nil {
					t.Fatal("malformed/changed Ready accepted")
				}
				if name == "charge-error" && !errors.Is(e, sentinel) {
					t.Fatal("charge error lost")
				}
				if name != "replacement-during-hold" && name != "content-change-during-hold" && name != "charge-error" && calls != 0 {
					t.Fatal("unsafe Ready was charged before validation")
				}
			}
		})
	}
}
