//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package desk

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

type storageOwnedInfo struct {
	size int64
	mode os.FileMode
	stat any
}

func (i storageOwnedInfo) Name() string       { return "fixture" }
func (i storageOwnedInfo) Size() int64        { return i.size }
func (i storageOwnedInfo) Mode() os.FileMode  { return i.mode }
func (i storageOwnedInfo) ModTime() time.Time { return time.Unix(100, 0) }
func (i storageOwnedInfo) IsDir() bool        { return i.mode.IsDir() }
func (i storageOwnedInfo) Sys() any           { return i.stat }
func storageOwnedInfoOf(inode uint64, size, blocks int64, directory bool) os.FileInfo {
	mode := os.FileMode(0600)
	if directory {
		mode = 0700 | os.ModeDir
	}
	stat := &syscall.Stat_t{Blocks: blocks}
	// Dev/Ino platform widths differ. Reflection here constructs handwritten
	// metadata only; it never derives expected results from the collector.
	field := reflect.ValueOf(stat).Elem().FieldByName("Dev")
	if field.Kind() >= reflect.Int && field.Kind() <= reflect.Int64 {
		field.SetInt(1)
	} else {
		field.SetUint(1)
	}
	reflect.ValueOf(stat).Elem().FieldByName("Ino").SetUint(inode)
	return storageOwnedInfo{size, mode, stat}
}
func storageOwnedPureOwner() (*storageBudgetOwner, *storageRootBinding) {
	fsID, w1, w2, rID := storagebudget.ID{1}, storagebudget.ID{2}, storagebudget.ID{3}, storagebudget.ID{4}
	o := &storageBudgetOwner{roots: map[storagebudget.ID]*storageRootBinding{}, filesystems: map[storagebudget.ID]storagebudget.Filesystem{fsID: {ID: fsID, Generation: 1, Identity: storagebudget.Digest{1}}}, workspaces: map[storagebudget.ID]storagebudget.Workspace{w1: {ID: w1, Generation: 1}, w2: {ID: w2, Generation: 1}}}
	o.self = o
	r := &storageRootBinding{owner: o, row: storagebudget.Root{ID: rID, Generation: 1, Filesystem: fsID}, category: storagebudget.History, references: []storagebudget.ID{w1}}
	r.self = r
	o.roots[rID] = r
	return o, r
}
func storageOwnedComplete(o *storageBudgetOwner, c *storageOwnedCapture) {
	c.complete()
	c.value.Epoch = 1
	o.finishOwnedMeasurement(c, context.Background(), nil)
}

func TestStorageOwnedMeasurementMetadata(t *testing.T) {
	t.Run("objects-aliases-workspace-overlap", func(t *testing.T) {
		o, r := storageOwnedPureOwner()
		c := o.beginOwnedMeasurement(o.orderedRoots(), o.workspaces, o.filesystems)
		dir, file := storageOwnedInfoOf(10, 0, 8, true), storageOwnedInfoOf(11, 7, 0, false)
		c.observe(r, "", dir, dir)
		c.observe(r, "one", file, file)
		c.observe(r, "alias", file, file)
		if c.value.Shared != (storagebudget.Vector{}) {
			t.Fatal("same-workspace aliases became shared")
		}
		alias := *r
		alias.references = []storagebudget.ID{{3}, {3}, {2}}
		c.observe(&alias, "same-object", file, file)
		c.observe(&alias, "again", file, file)
		separate := storageOwnedInfoOf(12, 7, 2, false)
		c.observe(r, "equal-bytes-separate-object", separate, separate)
		if c.value.Logical != (storagebudget.Vector{Bytes: 14, Files: 2, Directories: 1}) || c.value.Shared != (storagebudget.Vector{Bytes: 7, Files: 1}) || c.value.Allocation != (storagebudget.Vector{Bytes: 5120, Files: 2, Directories: 1}) {
			t.Fatal("object/overlap/allocation totals", c.value)
		}
		if len(c.seen) != 3 || !c.value.AllocationKnown {
			t.Fatal("sparse zero lost or aliases not deduplicated")
		}
	})
	for _, tc := range []struct {
		name          string
		before, after os.FileInfo
	}{
		{"unsupported", storageOwnedInfo{7, 0600, struct{}{}}, storageOwnedInfo{7, 0600, struct{}{}}},
		{"negative-blocks", storageOwnedInfoOf(1, 7, -1, false), storageOwnedInfoOf(1, 7, -1, false)},
		{"multiply-overflow", storageOwnedInfoOf(1, 7, 1<<62, false), storageOwnedInfoOf(1, 7, 1<<62, false)},
		{"changed-blocks", storageOwnedInfoOf(1, 7, 1, false), storageOwnedInfoOf(1, 7, 2, false)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, r := storageOwnedPureOwner()
			c := o.beginOwnedMeasurement(o.orderedRoots(), o.workspaces, o.filesystems)
			if tc.name == "unsupported" {
				if _, ok := storageReportedAllocation(tc.before); ok {
					t.Fatal("unsupported allocation is known")
				}
				return
			}
			c.observe(r, "one", tc.before, tc.after)
			if c.value.AllocationKnown || !c.value.LogicalKnown || c.value.Logical.Bytes != 7 || c.value.Filesystems[r.row.Filesystem].AllocationKnown {
				t.Fatal("unknown allocation contaminated logical value", c.value)
			}
		})
	}
	t.Run("checked-add-and-diagnostic-overflow", func(t *testing.T) {
		if _, ok := storageOwnedAdd(storagebudget.Vector{Bytes: ^uint64(0)}, storagebudget.Vector{Bytes: 1}); ok {
			t.Fatal("wrapped addition")
		}
		o, r := storageOwnedPureOwner()
		c := o.beginOwnedMeasurement(o.orderedRoots(), o.workspaces, o.filesystems)
		c.value.Allocation.Bytes = ^uint64(0)
		info := storageOwnedInfoOf(1, 7, 1, false)
		c.observe(r, "one", info, info)
		if c.value.AllocationKnown || !c.value.LogicalKnown {
			t.Fatal("allocation overflow became a census error or known value")
		}
	})
	t.Run("filesystem-correspondence", func(t *testing.T) {
		o, r := storageOwnedPureOwner()
		other := storagebudget.ID{5}
		o.filesystems[other] = storagebudget.Filesystem{ID: other, Generation: 1, Identity: storagebudget.Digest{9}}
		c := o.beginOwnedMeasurement(o.orderedRoots(), o.workspaces, o.filesystems)
		info := storageOwnedInfoOf(1, 7, 1, false)
		c.observe(r, "one", info, info)
		copy := *r
		copy.row.Filesystem = other
		c.observe(&copy, "foreign", info, info)
		if c.value.LogicalKnown || c.value.AllocationKnown || c.value.SharedKnown {
			t.Fatal("ambiguous object attributed as measured")
		}
	})
	t.Run("bounded-additional-root-self-objects", func(t *testing.T) {
		if storageOwnedObjectLimit != storageCensusEntries+2*storagebudget.MaxRoots {
			t.Fatal("self objects omitted from bound")
		}
		o, r := storageOwnedPureOwner()
		c := o.beginOwnedMeasurement(o.orderedRoots(), o.workspaces, o.filesystems)
		for i := 0; i < storageOwnedObjectLimit; i++ {
			c.seen[storageDirectoryIdentity{1, uint64(i + 1)}] = &storageOwnedObject{}
		}
		info := storageOwnedInfoOf(uint64(storageOwnedObjectLimit+1), 1, 0, false)
		c.observe(r, "extra", info, info)
		if len(c.seen) != storageOwnedObjectLimit || c.value.LogicalKnown || c.value.AllocationKnown {
			t.Fatal("collector grew beyond finite object bound")
		}
	})
}

func TestStorageOwnedMeasurementPublication(t *testing.T) {
	t.Run("newer-failed-attempt-keeps-prior-stale", func(t *testing.T) {
		o, r := storageOwnedPureOwner()
		first := o.beginOwnedMeasurement(o.orderedRoots(), o.workspaces, o.filesystems)
		i := storageOwnedInfoOf(1, 7, 0, false)
		first.observe(r, "one", i, i)
		storageOwnedComplete(o, first)
		old, failed := o.ownedMeasurement()
		if old == nil || failed {
			t.Fatal("original success not published")
		}
		later := o.beginOwnedMeasurement(o.orderedRoots(), o.workspaces, o.filesystems)
		o.finishOwnedMeasurement(later, t.Context(), errors.New("actual walk failed"))
		got, failed := o.ownedMeasurement()
		if got != old || !failed || got.Completed != old.Completed {
			t.Fatal("failure replaced or freshened prior observation")
		}
		storageOwnedComplete(o, first)
		got, failed = o.ownedMeasurement()
		if got != old || !failed {
			t.Fatal("late earlier success overwrote newer attempt")
		}
	})
	for _, name := range []string{"outer-close", "caller-cancel", "owner-closing", "scope-generation", "scope-references", "uncollected"} {
		t.Run(name, func(t *testing.T) {
			o, r := storageOwnedPureOwner()
			c := o.beginOwnedMeasurement(o.orderedRoots(), o.workspaces, o.filesystems)
			info := storageOwnedInfoOf(1, 7, 0, false)
			c.observe(r, "one", info, info)
			c.complete()
			c.value.Epoch = 1
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var err error
			switch name {
			case "outer-close":
				err = errors.New("checked close failed")
			case "caller-cancel":
				cancel()
			case "owner-closing":
				o.closing = true
			case "scope-generation":
				r.row.Generation++
			case "scope-references":
				r.references = []storagebudget.ID{{3}}
			case "uncollected":
				c.collected = false
			}
			o.finishOwnedMeasurement(c, ctx, err)
			if o.measurement.last != nil {
				t.Fatal("invalid completion published measurement", name)
			}
		})
	}
	for _, name := range []string{"success", "release-startup-holds-error", "inner-error", "caller-cancel"} {
		t.Run("startup-"+name, func(t *testing.T) {
			o, _ := storageOwnedPureOwner()
			c := o.beginOwnedMeasurement(o.orderedRoots(), o.workspaces, o.filesystems)
			c.complete()
			c.value.Epoch = 1
			startup := &storageBudgetConstruction{owner: o}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var inner, outer error
			if name == "inner-error" {
				inner = errors.New("phase finish/drop failed")
			}
			if name == "release-startup-holds-error" {
				outer = errors.New("original release failed")
			}
			o.stageStartupMeasurement(c, startup, ctx, inner)
			if o.measurement.last != nil {
				t.Fatal("inner success published before outer release")
			}
			if name == "caller-cancel" {
				cancel()
			}
			o.finishStartupMeasurement(startup, ctx, outer)
			if (o.measurement.last != nil) != (name == "success") || o.measurement.startup != nil {
				t.Fatal("outer result not respected", name)
			}
		})
	}
	t.Run("restart-does-not-recover-observation-time", func(t *testing.T) {
		o, _ := storageOwnedPureOwner()
		c := o.beginOwnedMeasurement(o.orderedRoots(), o.workspaces, o.filesystems)
		storageOwnedComplete(o, c)
		restarted, _ := storageOwnedPureOwner()
		if got, _ := restarted.ownedMeasurement(); got != nil {
			t.Fatal("decoded state minted a time")
		}
	})
}

// Real outer finish with an already-drained epoch: these are finalization
// boundary vectors, not qualification or a replacement for an application move.
func TestStorageOwnedMeasurementTransferOuterFinish(t *testing.T) {
	for _, name := range []string{"success", "drop-error", "registry-currentness-error"} {
		t.Run(name, func(t *testing.T) {
			o, _, _ := storageWriterFixture(t, false)
			capture := o.beginOwnedMeasurement(o.orderedRoots(), o.workspaces, o.filesystems)
			capture.complete()
			capture.value.Epoch = 1
			epoch := &storageEnrollmentEpoch{measurement: capture, measurementCaller: t.Context(), reconciled: true}
			epoch.self = epoch
			registry := &chatTransferLifetimes{owner: o}
			p := &chatTransferPreparation{registry: registry, server: &Server{}, epoch: epoch, cancel: func() {}, done: make(chan struct{}), retained: true}
			p.self = p
			registry.current = p
			if name != "drop-error" {
				storageTestMust(t, o.retain())
			}
			if name == "registry-currentness-error" {
				registry.current = &chatTransferPreparation{}
			}
			err := p.finish(nil)
			if (err == nil) != (name == "success") {
				t.Fatal("original finalizer error changed", err)
			}
			value, _ := o.ownedMeasurement()
			if (value != nil) != (name == "success") {
				t.Fatal("published before later drop/currentness result", name)
			}
		})
	}
}

func TestStorageOwnedMeasurementInitialCensus(t *testing.T) {
	o, root, path := storageWriterFixture(t, false)
	storageTestMust(t, os.WriteFile(filepath.Join(path, "empty"), nil, 0600))
	storageTestMust(t, os.WriteFile(filepath.Join(path, "real"), []byte("seven!!"), 0600))
	storageTestMust(t, os.Link(filepath.Join(path, "real"), filepath.Join(path, "hardlink")))
	storageTestMust(t, os.WriteFile(filepath.Join(path, storageReadyName), o.ref.cell.binding[:], 0600))
	storageTestMust(t, o.InitialCensus(t.Context()))
	got, failed := o.ownedMeasurement()
	if got == nil || failed || !got.LogicalKnown || !got.AllocationKnown || got.Completed.Before(got.Started) {
		t.Fatal("successful original census did not publish actual metadata", got, failed)
	}
	// Independent bounded fixture census, including covered controls and both
	// owning root directories. No expected value is copied from Q charges.
	var expected storagebudget.Vector
	seen := map[storageDirectoryIdentity]bool{}
	err := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		stat := info.Sys().(*syscall.Stat_t)
		id := storageDirectoryIdentity{uint64(stat.Dev), uint64(stat.Ino)}
		if seen[id] {
			return nil
		}
		seen[id] = true
		if info.IsDir() {
			expected.Directories++
		} else {
			expected.Files++
			expected.Bytes += uint64(info.Size())
		}
		return nil
	})
	storageTestMust(t, err)
	if got.Logical != expected || got.Logical.Directories != 2 {
		t.Fatal("actual covered/root metadata omitted or duplicated", got.Logical, expected)
	}
	q, err := o.ref.cell.q.Snapshot(t.Context())
	storageTestMust(t, err)
	if got.Logical == storageCensusTotal(q) {
		t.Fatal("returned provisioned charges as measurement")
	}
	if got.Filesystems[root.row.Filesystem].Logical != expected {
		t.Fatal("filesystem measurement lost objects")
	}
	priorTime := got.Completed
	storageTestMust(t, os.Symlink("absent", filepath.Join(path, "bad")))
	if err = o.InitialCensus(t.Context()); err == nil {
		t.Fatal("invalid census succeeded")
	}
	retained, failed := o.ownedMeasurement()
	if retained != got || !failed || retained.Completed != priorTime {
		t.Fatal("failed real census replaced successful observation")
	}
}

func storageOwnedStatusFixture(p storageStatusProjection, at time.Time) *storageOwnedMeasurement {
	v := &storageOwnedMeasurement{Epoch: p.Epoch, Revision: p.Revision, BoundaryKnown: true, Started: at.Add(-time.Second), Completed: at, Logical: storagebudget.Vector{Bytes: 9007199254740993, Files: 2, Directories: 1}, Allocation: storagebudget.Vector{Files: 2, Directories: 1}, Shared: storagebudget.Vector{Bytes: 23, Files: 1}, LogicalKnown: true, AllocationKnown: true, SharedKnown: true, Roots: map[storagebudget.ID]storagebudget.RegisteredRoot{}, WorkspaceGenerations: map[storagebudget.ID]uint64{}, FilesystemGenerations: map[storagebudget.ID]uint64{}, Filesystems: map[storagebudget.ID]storageOwnedFilesystem{}}
	for _, row := range p.Roots {
		id, _ := storageStatusParseID(row.ID)
		fs, _ := storageStatusParseID(row.Filesystem)
		v.Roots[id] = storagebudget.RegisteredRoot{ID: id, Filesystem: fs, Generation: row.Generation}
	}
	for _, row := range p.Workspaces {
		id, _ := storageStatusParseID(row.ID)
		v.WorkspaceGenerations[id] = row.Generation
	}
	for _, row := range p.Filesystems {
		id, _ := storageStatusParseID(row.ID)
		v.FilesystemGenerations[id] = row.Generation
		v.Filesystems[id] = storageOwnedFilesystem{Logical: v.Logical, Allocation: v.Allocation, LogicalKnown: true, AllocationKnown: true}
	}
	return v
}
func TestStorageStatusMeasurement(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	for _, name := range []string{"fresh-refresh", "cache-hit-completed-write-unprovable", "failed-newer-census", "aged", "maintenance", "revision-changed", "epoch-changed", "active-same-revision", "uncertain-same-revision", "terminal-unapplied-same-revision", "unknown-boundary", "root-generation", "workspace-generation", "filesystem-generation"} {
		t.Run(name, func(t *testing.T) {
			p := storageStatusTestProjection()
			p.Active = 0
			p.Uncertain = 0
			p.Terminal = 0
			v := storageOwnedStatusFixture(p, now.Add(-time.Second))
			refresh, failed := true, false
			switch name {
			case "cache-hit-completed-write-unprovable":
				refresh = false
			case "failed-newer-census":
				failed = true
			case "aged":
				v.Completed = now.Add(-16 * time.Second)
			case "maintenance":
				p.Maintenance = true
			case "revision-changed":
				p.Revision--
			case "epoch-changed":
				p.Epoch--
			case "active-same-revision":
				p.Active = 1
			case "uncertain-same-revision":
				p.Uncertain = 1
			case "terminal-unapplied-same-revision":
				p.Terminal = 1
			case "unknown-boundary":
				v.BoundaryKnown = false
			case "root-generation":
				p.Roots[0].Generation++
			case "workspace-generation":
				p.Workspaces[0].Generation++
			case "filesystem-generation":
				p.Filesystems[0].Generation++
			}
			out := storageStatusTestPublic(t, p, "")
			p.applyOwnedMeasurement(out, v, failed, now, refresh, true)
			if name == "root-generation" || name == "workspace-generation" || name == "filesystem-generation" {
				if out.Measurement.At != nil || out.PhysicalAllocated.Value != nil {
					t.Fatal("foreign correspondence received old totals")
				}
				return
			}
			want := "stale"
			if name == "fresh-refresh" {
				want = "fresh"
			}
			if name == "maintenance" {
				want = "maintenance"
			}
			if out.Measurement.Freshness != want || *out.Measurement.At != storageStatusTime(v.Completed) || out.Measurement.Logical.Value.Bytes != "9007199254740993" || out.PhysicalAllocated.Value.Bytes != "0" || out.SharedAttribution.Value.Bytes != "23" || out.Reclaimable.EligibleLogical != nil || out.RetainedLogical.Value != nil {
				t.Fatal("measurement truth/freshness changed", out.Measurement, out.PhysicalAllocated)
			}
			out.PhysicalAllocated.Value.Bytes = "99"
			next := storageStatusTestPublic(t, p, "")
			p.applyOwnedMeasurement(next, v, failed, now.Add(time.Second), false, true)
			if next.PhysicalAllocated.Value.Bytes != "0" || *next.Measurement.At != storageStatusTime(v.Completed) || next.Measurement.Generation != storageStatusUInt(v.Epoch) {
				t.Fatal("response aliases observation or status refresh minted time")
			}
		})
	}
}

func TestStorageOwnedMeasurementSharedWireVectors(t *testing.T) {
	raw, err := os.ReadFile("../../web/src/admin/storageOwnedMeasurement.vectors.json")
	storageTestMust(t, err)
	var vectors []struct {
		Name  string          `json:"name"`
		Facet json.RawMessage `json:"facet"`
	}
	storageTestMust(t, json.Unmarshal(raw, &vectors))
	if len(vectors) != 3 {
		t.Fatal("handwritten vector count changed")
	}
	for _, row := range vectors {
		t.Run(row.Name, func(t *testing.T) {
			p := storageStatusTestProjection()
			out := storageStatusTestPublic(t, p, "")
			if row.Name != "not-yet-measured" {
				v := storageOwnedStatusFixture(p, time.Date(2026, 10, 31, 11, 59, 59, 0, time.UTC))
				v.Epoch = 1
				if row.Name == "allocation-unknown-logical-zero" {
					v.Logical = storagebudget.Vector{}
					v.Shared = storagebudget.Vector{}
					v.AllocationKnown = false
				}
				p.applyOwnedMeasurement(out, v, false, time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC), false, true)
			}
			encoded, e := json.Marshal(struct {
				Measurement any `json:"measurement"`
				Physical    any `json:"physicalAllocated"`
				Shared      any `json:"sharedAttribution"`
			}{out.Measurement, out.PhysicalAllocated, out.SharedAttribution})
			storageTestMust(t, e)
			var expected, actual any
			storageTestMust(t, json.Unmarshal(row.Facet, &expected))
			storageTestMust(t, json.Unmarshal(encoded, &actual))
			if !reflect.DeepEqual(expected, actual) {
				t.Fatalf("shared handwritten wire changed: expected%s actual%s", row.Facet, encoded)
			}
		})
	}
}

// Actual application identity/custody is required here. Parent must integrate
// fresh exact-source native/proof correspondence before selecting this group.
func TestStorageOwnedMeasurementQualifiedLifecycle(t *testing.T) {
	requireStorageLine(t)
	records, err := storageEnrollmentNativeRecords(storageEnrollmentInstalledNativeRecords)
	storageTestMust(t, err)
	if len(records) == 0 {
		t.Skip("requires genuine current-source installed Record3; missing input is not qualification")
	}
	cfg := ownerActualConfig(t)
	s := ownerActualNew(t, cfg)
	o := s.storageConstruction.owner
	first, failed := o.ownedMeasurement()
	if first == nil || failed || first.Completed.IsZero() {
		t.Fatal("actual startup did not finalize measurement")
	}
	before := first.Completed
	o.mu.Lock()
	attempt := o.measurement.attempt
	o.mu.Unlock()
	for i := 0; i < 2; i++ {
		status := s.readStorageStatus(t.Context())
		if status.Snapshot == nil || status.Snapshot.Measurement.At == nil || *status.Snapshot.Measurement.At != storageStatusTime(before) {
			t.Fatal("status read discarded or relabeled original measurement", status.Availability)
		}
	}
	o.mu.Lock()
	afterAttempt := o.measurement.attempt
	o.mu.Unlock()
	if afterAttempt != attempt {
		t.Fatal("status read initiated census")
	}
	storageTestMust(t, s.prepareStoragePolicy(t.Context()))
	prepared, _ := o.ownedMeasurement()
	o.mu.Lock()
	epoch := o.censusEpoch
	o.mu.Unlock()
	if prepared == nil || prepared.Epoch != epoch {
		t.Fatal("actual owning enrollment did not hand off complete observation")
	}
	storageTestMust(t, s.Close())
	if value, _ := o.ownedMeasurement(); value != nil {
		t.Fatal("closed owner retained displayable measurement")
	}
	restartedAt := time.Now()
	restarted := ownerActualNew(t, cfg)
	latest, _ := restarted.storageConstruction.owner.ownedMeasurement()
	if latest == nil || latest.Completed.Before(restartedAt) {
		t.Fatal("restart synthesized old measurement time instead of genuine census")
	}
}

func storageOwnedRowsProjection(t *testing.T, o *storageBudgetOwner, c *storageOwnedCapture) *storageStatusSnapshot {
	t.Helper()
	p := storageStatusTestProjection()
	p.Roots = nil
	p.Filesystems = nil
	template := p.Workspaces[0]
	p.Workspaces = nil
	for _, root := range o.orderedRoots() {
		p.Roots = append(p.Roots, storageStatusCachedRoot{ID: storageHexID(root.row.ID), Filesystem: storageHexID(root.row.Filesystem), Generation: root.row.Generation, Known: true})
	}
	for id, row := range o.workspaces {
		member := template
		member.ID = storageHexID(id)
		member.Generation = row.Generation
		p.Workspaces = append(p.Workspaces, member)
	}
	for id, row := range o.filesystems {
		p.Filesystems = append(p.Filesystems, storageStatusCachedFilesystem{ID: storageHexID(id), Generation: row.Generation, Constraints: []string{p.Constraints[0].ID}})
	}
	c.complete()
	c.value.Epoch = 1
	out := storageStatusTestPublic(t, p, "")
	p.applyOwnedMeasurement(out, &c.value, false, time.Now(), false, false)
	return out
}
func TestStorageOwnedMeasurementUnknownRows(t *testing.T) {
	for _, kind := range []string{"object-limit", "unsupported-object", "unknown-identity", "missing-after", "cross-filesystem-object", "allocation-only", "changed-allocation"} {
		t.Run(kind, func(t *testing.T) {
			o, r := storageOwnedPureOwner()
			priorFS, currentFS, goodFS := r.row.Filesystem, storagebudget.ID{5}, storagebudget.ID{6}
			o.filesystems[currentFS] = storagebudget.Filesystem{ID: currentFS, Generation: 1, Identity: storagebudget.Digest{5}}
			o.filesystems[goodFS] = storagebudget.Filesystem{ID: goodFS, Generation: 1, Identity: storagebudget.Digest{6}}
			other := *r
			other.row.ID = storagebudget.ID{7}
			other.row.Filesystem = currentFS
			o.roots[other.row.ID] = &other
			good := *r
			good.row.ID = storagebudget.ID{8}
			good.row.Filesystem = goodFS
			o.roots[good.row.ID] = &good
			c := o.beginOwnedMeasurement(o.orderedRoots(), o.workspaces, o.filesystems)
			goodInfo := storageOwnedInfoOf(uint64(storageOwnedObjectLimit+100), 9, 2, false)
			c.observe(&good, "independently-valid", goodInfo, goodInfo)
			info := storageOwnedInfoOf(uint64(storageOwnedObjectLimit+1), 7, 1, false)
			switch kind {
			case "object-limit":
				for i := uint64(1); len(c.seen) < storageOwnedObjectLimit; i++ {
					c.seen[storageDirectoryIdentity{1, i}] = &storageOwnedObject{}
				}
			case "unsupported-object":
				info = storageOwnedInfo{size: 7, mode: os.ModeNamedPipe | 0600, stat: info.Sys()}
			case "unknown-identity":
				info = storageOwnedInfo{size: 7, mode: 0600, stat: struct{}{}}
			case "allocation-only":
				info = storageOwnedInfoOf(10, 7, -1, false)
			}
			if kind == "missing-after" {
				c.observe(r, "affected", info, nil)
			} else {
				c.observe(r, "affected", info, info)
			}
			if kind == "cross-filesystem-object" {
				c.observe(&other, "ambiguous-alias", info, info)
			}
			if kind == "changed-allocation" {
				c.observe(r, "changed-alias", info, storageOwnedInfoOf(uint64(storageOwnedObjectLimit+1), 7, 2, false))
			}
			out := storageOwnedRowsProjection(t, o, c)
			rows := map[string]storageStatusFilesystem{}
			for _, row := range out.Filesystems {
				rows[row.ID] = row
			}
			affected := rows[storageHexID(priorFS)]
			logicalValid := kind == "allocation-only" || kind == "changed-allocation"
			if (affected.MeasuredLogical != nil) != logicalValid || affected.PhysicalAllocated != nil {
				t.Fatal("affected filesystem leaked zero/partial known value", kind, affected)
			}
			if logicalValid && affected.MeasuredLogical.Bytes != "7" {
				t.Fatal("allocation uncertainty erased valid logical bytes")
			}
			if kind == "cross-filesystem-object" {
				row := rows[storageHexID(currentFS)]
				if row.MeasuredLogical != nil || row.PhysicalAllocated != nil {
					t.Fatal("prior/current correspondence ambiguity did not invalidate both rows")
				}
			}
			valid := rows[storageHexID(goodFS)]
			if valid.MeasuredLogical == nil || valid.MeasuredLogical.Bytes != "9" || valid.PhysicalAllocated == nil || valid.PhysicalAllocated.Bytes != "1024" {
				t.Fatal("unrelated valid filesystem was discarded", valid)
			}
			if out.PhysicalAllocated.Value != nil {
				t.Fatal("aggregate allocation remained known")
			}
		})
	}
}

func TestStorageOwnedMeasurementActivityContinuity(t *testing.T) {
	for _, name := range []string{"idle-original", "active-mutation", "completed-no-Q-write", "crossed-refresh", "generation-changed", "preparation", "prefix", "enrollment", "census", "counter-saturated"} {
		t.Run(name, func(t *testing.T) {
			o, _, _ := storageWriterFixture(t, false)
			c := o.beginOwnedMeasurement(o.orderedRoots(), o.workspaces, o.filesystems)
			before := o.ownedMeasurementActivity()
			var authority *storageMutationAuthority

			defer func() {
				o.admission.preparation = nil
				o.admission.prefixes = 0
				o.admission.enrollment = nil
				o.admission.exclusiveCensus = nil
				if authority != nil {
					storageTestMust(t, authority.Close())
				}
			}()
			switch name {
			case "active-mutation", "completed-no-Q-write", "crossed-refresh":
				var e error
				authority, e = o.beginAdmission(t.Context())
				storageTestMust(t, e)
				if name != "active-mutation" {
					storageTestMust(t, authority.Close())
				}
				if name == "completed-no-Q-write" {
					before = o.ownedMeasurementActivity()
				}
			case "generation-changed":
				o.admission.generation++
			case "preparation":
				o.admission.preparation = &storageIdentityPreparation{}
			case "prefix":
				o.admission.prefixes = 1
			case "enrollment":
				o.admission.enrollment = &storageEnrollmentEpoch{}
			case "census":
				o.admission.exclusiveCensus = &storageExclusiveStartupCensus{}
			case "counter-saturated":
				o.admission.measurementActivity = ^uint64(0)
				c.value.OrdinaryActivity = ^uint64(0)
				before = o.ownedMeasurementActivity()
			}
			after := o.ownedMeasurementActivity()
			if c.value.activityContinuous(before, after) != (name == "idle-original") {
				t.Fatal("unproved activity reported continuous", name, before, after)
			}
			if authority != nil {
				storageTestMust(t, authority.Close())
			}
			// Restore only test markers before fixture cleanup; no effect was issued.
			o.admission.preparation = nil
			o.admission.prefixes = 0
			o.admission.enrollment = nil
			o.admission.exclusiveCensus = nil
		})
	}
}
