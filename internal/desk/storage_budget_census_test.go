package desk

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func storageCensusTotal(s storagebudget.Snapshot) (v storagebudget.Vector) {
	for _, x := range s.Installation {
		v.Bytes += x.Bytes
		v.Files += x.Files
		v.Directories += x.Directories
	}
	return
}
func TestStorageInitialCensusKnownFloorsUnknownControlAndEmptyNamespace(t *testing.T) {
	o, _, config := storageWriterFixture(t, false)
	storageTestMust(t, os.WriteFile(filepath.Join(config, "unknown-control"), []byte("unknown"), 0600))
	storageTestMust(t, os.WriteFile(filepath.Join(config, "empty"), nil, 0600))
	storageTestMust(t, os.Mkdir(filepath.Join(config, "empty-dir"), 0700))
	storageTestMust(t, o.InitialCensus(t.Context()))
	s, e := o.ref.cell.q.Snapshot(t.Context())
	storageTestMust(t, e)
	if s.Maintenance || len(s.Roots) != 2 || len(s.Workspaces) != 0 || storageCensusTotal(s) != (storagebudget.Vector{Bytes: storagebudget.MetadataBudget + storageHostBudget + 7, Files: 34, Directories: 6}) {
		t.Fatalf("control floor or unknown objects omitted/doubled: %+v", storageCensusTotal(s))
	}
	for _, r := range s.Roots {
		if !r.Known || r.Generation != 1 {
			t.Fatal("incomplete root marked known")
		}
	}
}
func TestStorageInitialCensusNestedAliasedSharedInodeAttribution(t *testing.T) {
	o, configRoot, config := storageWriterFixture(t, false)
	w1, e := o.addWorkspace("original-workspace-1", 1, storagebudget.Limits{})
	storageTestMust(t, e)
	w2, e := o.addWorkspace("original-workspace-2", 2, storagebudget.Limits{})
	storageTestMust(t, e)
	storageTestMust(t, os.Mkdir(filepath.Join(config, "history"), 0700))
	storageTestMust(t, os.WriteFile(filepath.Join(config, "history", ".history.lock"), nil, 0600))
	storageTestMust(t, os.WriteFile(filepath.Join(config, "history", "one"), []byte("same"), 0600))
	storageTestMust(t, os.Link(filepath.Join(config, "history", "one"), filepath.Join(config, "history", "alias")))
	storageTestMust(t, os.WriteFile(filepath.Join(config, "history", "equal-separate"), []byte("same"), 0600))
	root, e := o.childRoot(t.Context(), configRoot, "history", storagebudget.History, []storagebudget.ID{w1}, ".history.lock")
	storageTestMust(t, e)
	alias, e := o.childRoot(t.Context(), configRoot, "history", storagebudget.History, []storagebudget.ID{w1, w2}, ".history.lock")
	storageTestMust(t, e)
	if root != alias {
		t.Fatal("alias minted another physical owner")
	}
	storageTestMust(t, o.InitialCensus(t.Context()))
	s, e := o.ref.cell.q.Snapshot(t.Context())
	storageTestMust(t, e)
	if len(s.Roots) != 3 || storageCensusTotal(s) != (storagebudget.Vector{Bytes: storagebudget.MetadataBudget + storageHostBudget + 8, Files: 36, Directories: 6}) {
		t.Fatalf("nested/shared census wrong: %+v", storageCensusTotal(s))
	}
	for _, w := range s.Workspaces {
		if !w.Known || w.Usage[storagebudget.History] != (storagebudget.Vector{Bytes: 8, Files: 4, Directories: 1}) {
			t.Fatalf("workspace sharing counted incorrectly: %+v", w)
		}
	}
	p := storageWriterPlan(root)
	p.ExpectedAbsent = false
	p.Expected = []byte("same")
	p.Target = "one"
	if _, e = o.admitSingleFile(t.Context(), p); e == nil {
		t.Fatal("shared inode replacement claimed exact removal credit")
	}
}
func TestStorageInitialCensusRefusalsPreserveMaintenance(t *testing.T) {
	for _, kind := range []string{"symlink", "nonprivate", "missing-domain", "entry-bound", "nested-boundary-missing"} {
		t.Run(kind, func(t *testing.T) {
			o, r, config := storageWriterFixture(t, false)
			switch kind {
			case "symlink":
				storageTestMust(t, os.Symlink("missing", filepath.Join(config, "link")))
			case "nonprivate":
				storageTestMust(t, os.WriteFile(filepath.Join(config, "public"), nil, 0644))
			case "missing-domain":
				storageTestMust(t, os.Remove(filepath.Join(config, ".data.lock")))
			case "entry-bound":
				w, e := o.addWorkspace("finite-history", 1, storagebudget.Limits{})
				storageTestMust(t, e)
				storageTestMust(t, o.legacyHistory(r, []storagebudget.ID{w}))
				for i := 0; i < maxStorageFiles; i++ {
					storageTestMust(t, os.WriteFile(filepath.Join(config, "entry-"+storageHexID(storageIntentTestID(byte(i%255+1)))+"-"+string(rune(0x100+i))), nil, 0600))
				}
			case "nested-boundary-missing":
				storageTestMust(t, os.Mkdir(filepath.Join(config, "history"), 0700))
				storageTestMust(t, os.WriteFile(filepath.Join(config, "history", ".history.lock"), nil, 0600))
				_, e := o.childRoot(t.Context(), r, "history", storagebudget.History, nil, ".history.lock")
				storageTestMust(t, e)
				storageTestMust(t, os.Rename(filepath.Join(config, "history"), filepath.Join(config, "moved-history")))
			}
			if e := o.InitialCensus(t.Context()); e == nil {
				t.Fatal("unknown census published")
			}
			s, e := o.ref.cell.q.Snapshot(t.Context())
			storageTestMust(t, e)
			if !s.Maintenance {
				t.Fatal("failed census cleared fence")
			}
			for _, row := range s.Roots {
				if row.Known {
					t.Fatal("partial root became known")
				}
			}
		})
	}
}
func TestStorageInitialCensusNoUnretiredCancellation(t *testing.T) {
	o, r, config := storageWriterFixture(t, true)
	p := storageWriterPlan(r)
	a, e := o.admitSingleFile(t.Context(), p)
	storageTestMust(t, e)
	// Original home SH and use SH are held through the actual writer lifetime.
	// Initial-only census cannot wait/upgrade them; the Q ticket is unchanged.
	before, e := o.ref.cell.q.InspectHostState(t.Context())
	storageTestMust(t, e)
	tree := storageTestTree(t, config)
	if e = o.InitialCensus(t.Context()); e == nil {
		t.Fatal("live writer census admitted")
	}
	after, e := o.ref.cell.q.InspectHostState(t.Context())
	storageTestMust(t, e)
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(tree, storageTestTree(t, config)) {
		t.Fatal("busy initial census changed state")
	}
	storageTestMust(t, a.Close())
	if e = o.InitialCensus(t.Context()); e == nil {
		t.Fatal("unretired ticket cancelled by census")
	}
	s, e := o.ref.cell.q.Snapshot(t.Context())
	storageTestMust(t, e)
	if !s.Maintenance || len(s.Tickets) != 1 {
		t.Fatal("unretired charge/fence lost")
	}
}
func TestStorageOriginalRootRefusesCopiedUnsupportedAndMissingRegistration(t *testing.T) {
	o, r, config := storageWriterFixture(t, true)
	p := storageWriterPlan(r)
	copy := *r
	p.Root = &copy
	before := storageTestTree(t, config)
	if _, e := o.admitSingleFile(t.Context(), p); e == nil {
		t.Fatal("copied root authority accepted")
	}
	if !reflect.DeepEqual(before, storageTestTree(t, config)) {
		t.Fatal("copied root changed state")
	}
	if _, e := newStorageBudgetOwner(o.ref, nil, storageBudgetOptions{}); e == nil {
		t.Fatal("unqualified observer accepted")
	}
	// A freely opened external root and copied metadata do not become enrolled.
	external, e := os.OpenRoot(t.TempDir())
	storageTestMust(t, e)
	defer external.Close()
	fake := &storageRootBinding{owner: o, root: external, row: r.row}
	p.Root = fake
	if _, e = o.admitSingleFile(t.Context(), p); e == nil {
		t.Fatal("external path/root became original authority")
	}
	// A new original owner that omits an already registered child must retain an
	// unknown boundary rather than publish zero for it. No migration is inferred.
	o2, e := newStorageBudgetOwner(o.ref, storageSyntheticFilesystem{}, storageBudgetOptions{HeadroomBytes: 16 << 20, HeadroomInodes: 100})
	if !errors.Is(e, storagebudget.ErrAuthority) {
		if o2 != nil {
			o2.Close()
		}
		t.Fatal("borrowed child ref established a new machine owner")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	before = storageTestTree(t, config)
	if e = o.InitialCensus(ctx); e == nil {
		t.Fatal("cancelled census ran")
	}
	if !reflect.DeepEqual(before, storageTestTree(t, config)) {
		t.Fatal("cancelled census mutated state")
	}
}

func TestStorageOriginalReopenMissingRootAndReplacedControl(t *testing.T) {
	for _, kind := range []string{"missing-registered-root", "replaced-bootstrap-control"} {
		t.Run(kind, func(t *testing.T) {
			o, r, config := storageWriterFixture(t, false)
			storageTestMust(t, os.Mkdir(filepath.Join(config, "history"), 0700))
			storageTestMust(t, os.WriteFile(filepath.Join(config, "history", ".history.lock"), nil, 0600))
			storageTestMust(t, os.WriteFile(filepath.Join(config, "history", "original"), []byte("retained history"), 0600))
			_, e := o.childRoot(t.Context(), r, "history", storagebudget.History, nil, ".history.lock")
			storageTestMust(t, e)
			storageTestMust(t, o.InitialCensus(t.Context()))
			if kind == "missing-registered-root" {
				c, e := o.ref.cell.q.ConfigurationSnapshot(t.Context())
				storageTestMust(t, e)
				_, e = o.ref.cell.q.BeginMaintenance(t.Context(), c.Revision)
				storageTestMust(t, e)
			} else {
				original := filepath.Join(config, storageBootstrapName)
				storageTestMust(t, os.Rename(original, original+".saved"))
				storageTestMust(t, os.WriteFile(original, nil, 0600))
			}
			ref, e := openStorageInstallation(t.Context(), config, "synthetic-original-home")
			storageTestMust(t, e)
			defer ref.Close()
			other, e := newStorageBudgetOwner(ref, storageSyntheticFilesystem{}, storageBudgetOptions{HeadroomBytes: 16 << 20, HeadroomInodes: 100})
			storageTestMust(t, e)
			defer other.Close()
			root, e := other.configRoot(t.Context(), ".data.lock")
			storageTestMust(t, e)
			_, e = other.anchorRoot(t.Context())
			storageTestMust(t, e)
			beforeState, e := ref.cell.q.InspectHostState(t.Context())
			storageTestMust(t, e)
			before := storageTestTree(t, config)
			if kind == "missing-registered-root" {
				if e = other.InitialCensus(t.Context()); e == nil {
					t.Fatal("omitted root became zero census")
				}
			} else {
				if _, e = other.admitSingleFile(t.Context(), storageWriterPlan(root)); e == nil {
					t.Fatal("reopened replacement control inherited registered authority")
				}
			}
			after, e := ref.cell.q.InspectHostState(t.Context())
			storageTestMust(t, e)
			if !reflect.DeepEqual(beforeState, after) || !reflect.DeepEqual(before, storageTestTree(t, config)) {
				t.Fatal("refusal changed census/charge/fence or grew namespace")
			}
		})
	}
}

func TestStorageCheckedWriterCannotCrossNestedBoundaryOrReplaceDomain(t *testing.T) {
	o, r, config := storageWriterFixture(t, false)
	storageTestMust(t, os.Mkdir(filepath.Join(config, "history"), 0700))
	storageTestMust(t, os.WriteFile(filepath.Join(config, "history", ".history.lock"), nil, 0600))
	child, e := o.childRoot(t.Context(), r, "history", storagebudget.History, nil, ".history.lock")
	storageTestMust(t, e)
	storageTestMust(t, o.InitialCensus(t.Context()))
	for _, kind := range []string{"Q-boundary", "history-boundary", "original-domain"} {
		t.Run(kind, func(t *testing.T) {
			p := storageWriterPlan(r)
			switch kind {
			case "Q-boundary":
				p.Target = "storage-budget/new"
				p.Stage = "storage-budget/.stage"
			case "history-boundary":
				p.Target = "history/new"
				p.Stage = "history/.stage"
			case "original-domain":
				p.Root = child
				p.Target = ".history.lock"
				p.Stage = ".stage"
				p.ExpectedAbsent = false
				p.Expected = nil
			}
			before := storageTestTree(t, config)
			if _, e := o.admitSingleFile(t.Context(), p); e == nil {
				t.Fatal("parent root bypassed original nested/domain authority")
			}
			if !reflect.DeepEqual(before, storageTestTree(t, config)) {
				t.Fatal("boundary refusal grew/remapped state")
			}
		})
	}
}

func TestStorageRecoveryCanonicalSidecarControlAccounting(t *testing.T) {
	for _, kind := range []string{"history", "legacy-config"} {
		t.Run(kind, func(t *testing.T) {
			o, root, config := storageWriterFixture(t, false)
			w1, e := o.addWorkspace("original-sidecar-workspace-a", 1, storagebudget.Limits{})
			storageTestMust(t, e)
			w2, e := o.addWorkspace("original-sidecar-workspace-b", 2, storagebudget.Limits{})
			storageTestMust(t, e)
			references := []storagebudget.ID{w1, w2}
			directory := config
			if kind == "history" {
				directory = filepath.Join(config, "history")
				storageTestMust(t, os.Mkdir(directory, 0700))
				storageTestMust(t, os.WriteFile(filepath.Join(directory, ".history.lock"), nil, 0600))
				root, e = o.childRoot(t.Context(), root, "history", storagebudget.History, references, ".history.lock")
				storageTestMust(t, e)
			} else {
				storageTestMust(t, o.legacyHistory(root, references))
			}
			name := assistantDialectReadRecoveryName(strings.Repeat("a", 64), "arun_"+strings.Repeat("b", 32))
			if !assistantDialectReadRecoveryFileName.MatchString(name) {
				t.Fatal("producer did not produce canonical sidecar name")
			}
			// Metadata-only census neither parses nor authorizes recovery rows.
			// It counts every byte; consumer row validation remains unchanged.
			storageTestMust(t, os.WriteFile(filepath.Join(directory, name), []byte("valid-name"), 0600))
			malformed := "assistant-dialect-read-recovery-malformed.json"
			unrelated := "assistant-read-recovery-unrelated.json"
			storageTestMust(t, os.WriteFile(filepath.Join(directory, malformed), []byte("bad"), 0600))
			storageTestMust(t, os.WriteFile(filepath.Join(directory, unrelated), []byte("other"), 0600))
			storageTestMust(t, o.InitialCensus(t.Context()))
			snapshot, e := o.ref.cell.q.Snapshot(t.Context())
			storageTestMust(t, e)
			if len(snapshot.Workspaces) != 2 {
				t.Fatal("shared canonical sidecar workspaces omitted")
			}
			var row storagebudget.Root
			for _, candidate := range snapshot.Roots {
				if candidate.ID == root.row.ID {
					row = candidate
				}
			}
			if kind == "history" {
				if row.Usage[storagebudget.Control] != (storagebudget.Vector{Bytes: 10, Files: 1}) || row.Usage[storagebudget.History] != (storagebudget.Vector{Bytes: 8, Files: 3, Directories: 1}) {
					t.Fatal("canonical sidecar not Control or unknown names lost conservative History charge")
				}
			} else if row.Usage[storagebudget.Control] != (storagebudget.Vector{Bytes: storageHostBudget + 18, Files: 19, Directories: 4}) {
				t.Fatal("legacy canonical/unknown physical charge omitted or doubled")
			}
			for _, workspace := range snapshot.Workspaces {
				if workspace.Usage[storagebudget.Control] != (storagebudget.Vector{Bytes: 10, Files: 1}) {
					t.Fatal("canonical Control attribution omitted for a legacy/shared workspace")
				}
			}
			p := storageWriterPlan(root)
			p.Target, p.Stage = name, ".sidecar-stage"
			p.ExpectedAbsent, p.Expected = false, []byte("valid-name")
			a, e := o.admitSingleFile(t.Context(), p)
			storageTestMust(t, e)
			record := storageWriterRecords(t, o)[0]
			var physical storagebudget.Vector
			seen := map[storagebudget.ID]bool{}
			for _, component := range record.Components {
				if component.Category != storagebudget.Control {
					t.Fatal("admitted canonical sidecar category changed")
				}
				physical, e = storageVectorAdd(physical, component.Physical)
				storageTestMust(t, e)
				if component.Physical == (storagebudget.Vector{}) && component.SettlementPhysical == (storagebudget.Vector{}) {
					if component.CapacityCount != root.row.CapacityCount || component.CapacityBinding != root.row.CapacityBinding {
						t.Fatal("attribution-only component lost complete relation")
					}
					for _, peak := range component.Capacity {
						if peak.Peak != (storagebudget.CapacityVector{}) {
							t.Fatal("shared attribution multiplied physical peak")
						}
					}
				}
				if component.Workspace != (storagebudget.ID{}) {
					seen[component.Workspace] = true
					if component.Attribution != (storagebudget.Vector{Bytes: uint64(len(p.Bytes)), Files: 1}) {
						t.Fatal("admitted shared workspace Control attribution changed")
					}
				}
			}
			if physical != (storagebudget.Vector{Bytes: uint64(len(p.Bytes)), Files: 1}) || !seen[w1] || !seen[w2] || len(seen) != 2 {
				t.Fatal("shared sidecar was not physically once and attributed to each workspace")
			}
			storageTestMust(t, a.Close()) // no consumer activation or fake positive POST
			for _, alternative := range []string{malformed, unrelated, "nested/" + name} {
				category, refs := root.categoryOf(alternative)
				if kind == "history" && (category != storagebudget.History || len(refs) != 2) || kind == "legacy-config" && (category != storagebudget.Control || len(refs) != 0) {
					t.Fatal("canonical recognizer broadened unknown filename attribution")
				}
			}
		})
	}
}
