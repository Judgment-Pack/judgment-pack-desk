package desk

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Handwritten fixtures exercise policy/pass behavior. They certify no native
// platform or startup route; actual native qualification remains separate.
func storageAdvisoryPassFixture(samples ...storageFilesystemSample) (*storageCapacityPass, *storageAdvisoryObserver) {
	o := &storageBudgetOwner{}
	a := &storageAdvisoryObserver{owner: o, catalog: storageOwnerCatalogV2{Installation: storageIntentTestID(1)}}
	p := &storageCapacityPass{owner: o, started: time.Now(), maxAge: 5 * time.Second, samples: map[*os.Root]storageFilesystemSample{}}
	for _, s := range samples {
		p.samples[new(os.Root)] = s
	}
	return p, a
}
func storageAdvisoryScope(bytes uint64, finite bool, inodes uint64) storageFilesystemSample {
	status := storageAdvisoryStatus(time.Now())
	status.Bytes = storagePhysicalCounterMeasured
	status.Inodes = storagePhysicalCounterDynamic
	if finite {
		status.Inodes = storagePhysicalCounterMeasured
	}
	return storageFilesystemSample{PhysicalStatus: status, NativeTotalBytes: 1 << 30, NativeAvailable: storagebudget.CapacityVector{Bytes: bytes, Inodes: inodes}}
}
func TestStorageAdvisoryPassResiduals(t *testing.T) {
	first := storageAdvisoryScope(storageAdvisoryHeadroomMinBytes+(100<<20), false, 0)
	second := storageAdvisoryScope(storageAdvisoryHeadroomMinBytes+(1000<<20), true, 2000)
	first.PhysicalStatus.Oldest = time.Now().Add(-time.Second)
	p, a := storageAdvisoryPassFixture(first, second)
	storageTestMust(t, p.derive(a))
	for _, s := range p.samples {
		if len(s.Observations) != 1 {
			t.Fatal("missing shared policy row")
		}
		obs := s.Observations[0]
		if obs.Available != (storagebudget.CapacityVector{Bytes: 100 << 20, Inodes: 976}) || !obs.At.Equal(first.PhysicalStatus.Oldest) {
			t.Fatalf("wrong minimum or oldest query: %+v", obs)
		}
		if s.PhysicalStatus.UserQuota != storagePhysicalDiscoveryUnknown || s.PhysicalStatus.APFSContainer != storagePhysicalDiscoveryUnknown {
			t.Fatal("unknown discovery relabelled")
		}
	}
}
func TestStorageAdvisoryPassDynamicCapAndExhaustion(t *testing.T) {
	for _, finite := range []bool{false, true} {
		s := storageAdvisoryScope(storageAdvisoryHeadroomMinBytes, finite, 1024)
		p, a := storageAdvisoryPassFixture(s)
		storageTestMust(t, p.derive(a))
		for _, got := range p.samples {
			want := storageAdvisoryInodePolicyCap
			if finite {
				want = 0
			}
			if got.Observations[0].Available != (storagebudget.CapacityVector{Inodes: want}) {
				t.Fatal("exhaustion/cap changed")
			}
		}
	}
	low := storageAdvisoryScope(storageAdvisoryHeadroomMinBytes-1, false, 0)
	p, a := storageAdvisoryPassFixture(low)
	if !errors.Is(p.derive(a), storagebudget.ErrHeadroom) {
		t.Fatal("lowered headroom")
	}
}
func TestStorageAdvisoryPassUnknownAndStaleRefuse(t *testing.T) {
	for _, which := range []string{"bytes", "inodes", "stale", "future"} {
		t.Run(which, func(t *testing.T) {
			s := storageAdvisoryScope(1<<30, true, 2000)
			switch which {
			case "bytes":
				s.PhysicalStatus.Bytes = storagePhysicalCounterUnknown
			case "inodes":
				s.PhysicalStatus.Inodes = storagePhysicalCounterUnknown
			case "stale":
				s.PhysicalStatus.Oldest = time.Now().Add(-6 * time.Second)
			case "future":
				s.PhysicalStatus.Oldest = time.Now().Add(time.Hour)
			}
			p, a := storageAdvisoryPassFixture(s)
			if p.derive(a) == nil {
				t.Fatal("unknown/stale counters admitted")
			}
		})
	}
}
func TestStorageCapacityPassCompleteFrozenRegistry(t *testing.T) {
	o, r, _ := storageWriterFixture(t, false)
	p, e := o.beginStorageCapacityPass(t.Context(), nil)
	storageTestMust(t, e)
	if len(p.roots) != len(o.roots) || len(p.samples) != len(o.roots) {
		t.Fatal("partial enrolled set")
	}
	if _, e = o.addWorkspace("late", 1, storagebudget.Limits{}); !errors.Is(e, storagebudget.ErrBusy) {
		t.Fatal("registry changed during pass", e)
	}
	if u, e := o.maintenanceUse(); !errors.Is(e, storagebudget.ErrBusy) {
		if u != nil {
			u.Close()
		}
		t.Fatal("EX maintenance overlapped SH", e)
	}
	if _, e = o.beginStorageCapacityPass(t.Context(), []storageCapacityEffectParent{{binding: r, root: new(os.Root)}}); !errors.Is(e, storagebudget.ErrAuthority) {
		t.Fatal("unsealed parent accepted", e)
	}
	storageTestMust(t, p.Close())
	if !errors.Is(p.current(t.Context()), os.ErrClosed) {
		t.Fatal("closed pass reused")
	}
	storageTestMust(t, p.Close())
}
func TestStorageCapacityPassExactParentCustody(t *testing.T) {
	o, r, config := storageWriterFixture(t, false)
	storageTestMust(t, os.Mkdir(filepath.Join(config, "child"), 0700))
	child, e := storagePinChild(r.root, "child", true)
	storageTestMust(t, e)
	defer child.root.Close()
	p, e := o.beginStorageCapacityPass(t.Context(), []storageCapacityEffectParent{{binding: r, root: child.root, witnesses: []storageNamedRoot{child}}})
	storageTestMust(t, e)
	defer p.Close()
	if len(p.samples) != len(o.roots)+1 {
		t.Fatal("effect parent omitted")
	}
	storageTestMust(t, os.Rename(filepath.Join(config, "child"), filepath.Join(config, "held-child")))
	storageTestMust(t, os.Mkdir(filepath.Join(config, "child"), 0700))
	if p.current(t.Context()) == nil {
		t.Fatal("numeric cache authorized replaced parent")
	}
}
func TestStorageCapacityPassBoundsAndDeadline(t *testing.T) {
	o, _, _ := storageWriterFixture(t, false)
	if _, e := o.beginStorageCapacityPass(t.Context(), make([]storageCapacityEffectParent, 33)); !errors.Is(e, storagebudget.ErrBounds) {
		t.Fatal("effect bound", e)
	}
	saved := o.roots
	o.roots = map[storagebudget.ID]*storageRootBinding{}
	for i := 0; i < 4097; i++ {
		id := storagebudget.ID{}
		id[0] = byte(i)
		id[1] = byte(i >> 8)
		o.roots[id] = nil
	}
	_, e := o.beginStorageCapacityPass(t.Context(), nil)
	o.roots = saved
	if !errors.Is(e, storagebudget.ErrBounds) {
		t.Fatal("root bound", e)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, e = o.beginStorageCapacityPass(ctx, nil); !errors.Is(e, context.Canceled) {
		t.Fatal("cancelled pass", e)
	}
	p := &storageCapacityPass{started: time.Now().Add(-251 * time.Millisecond), maxAge: 5 * time.Second}
	if !errors.Is(p.checkTime(t.Context(), true), storagebudget.ErrStale) {
		t.Fatal("late query admitted")
	}
	p.started = time.Now().Add(-6 * time.Second)
	p.completed = p.started.Add(time.Millisecond)
	if !errors.Is(p.checkTime(t.Context(), false), storagebudget.ErrStale) {
		t.Fatal("stale cached pass admitted")
	}
}

type storagePassCallbackObserver func(context.Context, *os.Root) (storageFilesystemSample, error)

var _ storageFilesystemObserver = storagePassCallbackObserver(nil)

func (f storagePassCallbackObserver) observeStorageFilesystem(ctx context.Context, root *os.Root) (storageFilesystemSample, error) {
	return f(ctx, root)
}

func (storagePassCallbackObserver) storageCapacityEnvelope(ctx context.Context, root *os.Root, d storageCapacityDemand) ([storagebudget.MaxCapacityRefs]storagebudget.CapacityVector, error) {
	return (storageSyntheticFilesystem{}).storageCapacityEnvelope(ctx, root, d)
}

func TestStorageCapacityPassFailureReleasesOriginalCustody(t *testing.T) {
	for _, which := range []string{"root_bound", "constraint_bound", "root_identity", "empty_registry", "unsealed_parent", "use_busy", "counter_unknown", "counter_unknown_with_live_pass", "cancel_before", "cancel_during_query", "deadline_before"} {
		t.Run(which, func(t *testing.T) {
			o, r, _ := storageWriterFixture(t, false)
			before, err := o.ref.cell.q.InspectHost(t.Context())
			storageTestMust(t, err)
			o.mu.Lock()
			activeBefore := o.active
			o.mu.Unlock()
			o.ref.cell.mu.Lock()
			refsBefore := o.ref.cell.refs
			o.ref.cell.mu.Unlock()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var effects []storageCapacityEffectParent
			var queries int
			want := storagebudget.ErrBounds
			restore := func() {}
			t.Cleanup(func() { restore() })
			switch which {
			case "root_bound":
				saved := o.roots
				o.roots = map[storagebudget.ID]*storageRootBinding{}
				for i := 0; i < storageCapacityPassRoots+1; i++ {
					id := storagebudget.ID{byte(i), byte(i >> 8)}
					o.roots[id] = nil
				}
				restore = func() { o.roots = saved }
			case "constraint_bound":
				saved := o.constraints
				o.constraints = map[storagebudget.ID]storagebudget.CapacityConstraint{}
				for i := 0; i < storageCapacityPassConstraints+2; i++ {
					id := storagebudget.ID{byte(i), byte(i >> 8)}
					o.constraints[id] = storagebudget.CapacityConstraint{}
				}
				restore = func() { o.constraints = saved }
			case "root_identity":
				saved := o.roots
				o.roots = map[storagebudget.ID]*storageRootBinding{r.row.ID: nil}
				restore = func() { o.roots = saved }
				want = storagebudget.ErrAuthority
			case "empty_registry":
				saved := o.roots
				o.roots = map[storagebudget.ID]*storageRootBinding{}
				restore = func() { o.roots = saved }
				want = storagebudget.ErrUnknown
			case "unsealed_parent":
				effects = []storageCapacityEffectParent{{binding: r, root: new(os.Root)}}
				want = storagebudget.ErrAuthority
			case "use_busy":
				held, err := o.maintenanceUse()
				storageTestMust(t, err)
				restore = func() { storageTestMust(t, held.Close()) }
				want = storagebudget.ErrBusy
			case "counter_unknown":
				o.observer = storagePassFailureObserver{failure: storagebudget.ErrUnknown}
				want = storagebudget.ErrUnknown
			case "counter_unknown_with_live_pass":
				held, err := o.beginStorageCapacityPass(t.Context(), nil)
				storageTestMust(t, err)
				o.observer = storagePassFailureObserver{failure: storagebudget.ErrUnknown}
				restore = func() {
					storageTestMust(t, held.current(t.Context()))
					storageTestMust(t, held.Close())
				}
				want = storagebudget.ErrUnknown
			case "cancel_before":
				cancel()
				want = context.Canceled
			case "cancel_during_query":
				o.observer = storagePassCallbackObserver(func(context.Context, *os.Root) (storageFilesystemSample, error) {
					queries++
					cancel()
					return storageSyntheticSample(), nil
				})
				want = context.Canceled
			case "deadline_before":
				var expire context.CancelFunc
				ctx, expire = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer expire()
				want = context.DeadlineExceeded
			}
			p, err := o.beginStorageCapacityPass(ctx, effects)
			restore()
			restore = func() {}
			o.observer = storageSyntheticFilesystem{}
			if p != nil || !errors.Is(err, want) {
				t.Fatalf("failed pass = %v, %v; want nil, %v", p, err, want)
			}
			if want != storagebudget.ErrAuthority && errors.Is(err, storagebudget.ErrAuthority) {
				t.Fatal("cleanup lost original pass binding", err)
			}
			if which == "cancel_during_query" && queries != 1 {
				t.Fatal("cancellation did not occur inside acquired pass", queries)
			}
			assertReleased := func() {
				t.Helper()
				o.mu.Lock()
				active := o.active
				o.mu.Unlock()
				o.ref.cell.mu.Lock()
				refs := o.ref.cell.refs
				o.ref.cell.mu.Unlock()
				if active != activeBefore || refs != refsBefore {
					t.Fatalf("retained authority leaked: active %d -> %d, refs %d -> %d", activeBefore, active, refsBefore, refs)
				}
				u, err := o.maintenanceUse()
				storageTestMust(t, err)
				storageTestMust(t, u.Close())
			}
			assertReleased()
			after, err := o.ref.cell.q.InspectHost(t.Context())
			storageTestMust(t, err)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("failed observation changed original Q definitions or obligations")
			}
			p, err = o.beginStorageCapacityPass(t.Context(), nil)
			storageTestMust(t, err)
			storageTestMust(t, p.current(t.Context()))
			storageTestMust(t, p.Close())
			storageTestMust(t, p.Close())
			assertReleased()
		})
	}
}

type storagePassFailureObserver struct {
	storageSyntheticFilesystem
	failure error
}

func (f storagePassFailureObserver) observeStorageFilesystem(context.Context, *os.Root) (storageFilesystemSample, error) {
	return storageFilesystemSample{}, f.failure
}
func TestStorageCapacityPassKnownCounterRefusalPreservesDefinitions(t *testing.T) {
	o, _, _ := storageWriterFixture(t, true)
	before, e := o.ref.cell.q.InspectHost(t.Context())
	storageTestMust(t, e)
	o.observer = storagePassFailureObserver{failure: storagebudget.ErrUnknown}
	if _, e = o.beginStorageCapacityPass(t.Context(), nil); !errors.Is(e, storagebudget.ErrUnknown) {
		t.Fatal("known unreadable row admitted", e)
	}
	after, e := o.ref.cell.q.InspectHost(t.Context())
	storageTestMust(t, e)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("counter error edited original Q")
	}
}
func TestStorageCapacityPassPositiveDiscoveryFencesWithoutRebind(t *testing.T) {
	o, _, _ := storageWriterFixture(t, true)
	r := o.orderedRoots()[0]
	for _, bound := range o.roots {
		if bound.configControl {
			r = bound
		}
	}
	permit, e := o.admitSingleFile(t.Context(), storageWriterPlan(r))
	storageTestMust(t, e)
	defer permit.Close()
	before, e := o.ref.cell.q.InspectHost(t.Context())
	storageTestMust(t, e)
	o.observer = storagePassFailureObserver{failure: errStorageOwnerTransitionRequired}
	if _, e = o.beginStorageCapacityPass(storageMutationContext(t.Context(), permit.authority), nil); !errors.Is(e, errStorageOwnerTransitionRequired) {
		t.Fatal("discovery did not fence", e)
	}
	after, e := o.ref.cell.q.InspectHost(t.Context())
	storageTestMust(t, e)
	if !after.Configuration.Maintenance || !reflect.DeepEqual(before.Roots, after.Roots) || !reflect.DeepEqual(before.Configuration.Filesystems, after.Configuration.Filesystems) || !reflect.DeepEqual(before.Configuration.CapacityConstraints, after.Configuration.CapacityConstraints) || !reflect.DeepEqual(before.Slots, after.Slots) {
		t.Fatal("fence changed immutable definitions or ticket evidence")
	}
	if !errors.Is(o.InitialCensus(t.Context()), errStorageOwnerTransitionRequired) {
		t.Fatal("ordinary census cleared transition fence")
	}
}

func TestStorageCapacityPassFiniteGroupBudget(t *testing.T) {
	p := &storageCapacityPass{}
	for i := 0; i < 32; i++ {
		storageTestMust(t, p.claimGroup(storageCapacityDemand{MetadataOps: 1}))
	}
	if !errors.Is(p.claimGroup(storageCapacityDemand{Physical: storagebudget.Vector{Bytes: 1}}), storagebudget.ErrBounds) {
		t.Fatal("unbounded group retry")
	}
	storageTestMust(t, p.claimGroup(storageCapacityDemand{}))
	p.closed = true
	if !errors.Is(p.claimGroup(storageCapacityDemand{MetadataOps: 1}), os.ErrClosed) {
		t.Fatal("closed pass group")
	}
}
func TestStorageCapacityPassRetainsOriginalUntilClose(t *testing.T) {
	o, _, _ := storageWriterFixture(t, false)
	p, e := o.beginStorageCapacityPass(t.Context(), nil)
	storageTestMust(t, e)
	storageTestMust(t, o.Close())
	storageTestMust(t, p.current(t.Context()))
	if !errors.Is(o.retain(), os.ErrClosed) {
		t.Fatal("closing owner accepted another lifetime")
	}
	storageTestMust(t, p.Close())
	if !errors.Is(p.current(t.Context()), os.ErrClosed) {
		t.Fatal("original lifetime survived Close")
	}
}

func TestStorageAdvisoryExt4ProjectSubjects(t *testing.T) {
	object := storageNativeLinuxObjectRaw{UID: 77, GID: 88}
	for _, tc := range []struct {
		name           string
		project, flags uint32
		use            storageAdvisoryObjectUse
		want           [3]uint32
	}{
		{"child_inherits_nonzero", 37, storageAdvisoryExt4ProjectInherit, storageAdvisoryProspectiveChild, [3]uint32{1001, 1002, 37}},
		{"child_without_inherit_uses_default", 37, 0, storageAdvisoryProspectiveChild, [3]uint32{1001, 1002, 0}},
		{"child_inherits_default", 0, storageAdvisoryExt4ProjectInherit, storageAdvisoryProspectiveChild, [3]uint32{1001, 1002, 0}},
		{"child_without_inherit_default", 0, 0, storageAdvisoryProspectiveChild, [3]uint32{1001, 1002, 0}},
		{"existing_object_keeps_noninherited_project", 37, 0, storageAdvisoryExistingObject, [3]uint32{77, 88, 37}},
		{"existing_object_with_inherit_keeps_own_subjects", 37, storageAdvisoryExt4ProjectInherit, storageAdvisoryExistingObject, [3]uint32{77, 88, 37}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ids, err := storageAdvisoryExt4QuotaSubjects(object, storageNativeExt4Raw{ParentProjectID: tc.project, ParentXFlags: tc.flags}, 1001, 1002, tc.use)
			storageTestMust(t, err)
			if ids != tc.want {
				t.Fatalf("selected actual quota subjects %v, want %v", ids, tc.want)
			}
		})
	}
	t.Run("child_setgid", func(t *testing.T) {
		object.Mode = 02000
		ids, err := storageAdvisoryExt4QuotaSubjects(object, storageNativeExt4Raw{ParentProjectID: 37}, 1001, 1002, storageAdvisoryProspectiveChild)
		storageTestMust(t, err)
		if ids != ([3]uint32{1001, 88, 0}) {
			t.Fatal("setgid changed project inheritance", ids)
		}
	})
	t.Run("ambiguous_effect_kind", func(t *testing.T) {
		if _, err := storageAdvisoryExt4QuotaSubjects(object, storageNativeExt4Raw{}, 1001, 1002, 0); !errors.Is(err, storagebudget.ErrUnknown) {
			t.Fatal("ambiguous effect interpreted as child/default", err)
		}
	})
}

func TestStorageAdvisoryExt4EnrolledProjectFlagRemoval(t *testing.T) {
	m := storageOwnerMembership{User: storageOwnerSubject{State: 4, Kind: 1}, Group: storageOwnerSubject{State: 4, Kind: 2}, Project: storageOwnerSubject{State: 3, Kind: 3, Subject: 37, Constraint: storageIntentTestID(37), NamespaceMap: storageIntentTestDigest(37)}}
	before := m
	inherited, err := storageAdvisoryExt4QuotaSubjects(storageNativeLinuxObjectRaw{}, storageNativeExt4Raw{ParentProjectID: 37, ParentXFlags: storageAdvisoryExt4ProjectInherit}, 1001, 1002, storageAdvisoryProspectiveChild)
	storageTestMust(t, err)
	storageTestMust(t, storageAdvisoryValidateQuotaSubjects(m, inherited))
	removed, err := storageAdvisoryExt4QuotaSubjects(storageNativeLinuxObjectRaw{}, storageNativeExt4Raw{ParentProjectID: 37}, 1001, 1002, storageAdvisoryProspectiveChild)
	storageTestMust(t, err)
	if removed[2] != 0 || !errors.Is(storageAdvisoryValidateQuotaSubjects(m, removed), errStorageOwnerTransitionRequired) {
		t.Fatal("removed inheritance retained old project row")
	}
	if !errors.Is(storageAdvisoryConfirmQuotaSubjects(m, inherited, removed), errStorageOwnerTransitionRequired) {
		t.Fatal("changed enrolled mapping survived final revalidation")
	}
	if !reflect.DeepEqual(before, m) {
		t.Fatal("mapping refusal edited membership")
	}
	storageAdvisoryTestConservedTransitionFence(t, storageAdvisoryValidateQuotaSubjects(m, removed))
	unknown := storageOwnerMembership{User: storageOwnerSubject{State: 4, Kind: 1}, Group: storageOwnerSubject{State: 4, Kind: 2}, Project: storageOwnerSubject{State: 4, Kind: 3}}
	if !errors.Is(storageAdvisoryConfirmQuotaSubjects(unknown, inherited, removed), storagebudget.ErrUnknown) {
		t.Fatal("unresolved mid-query mapping relabelled disabled or unlimited")
	}
}

func TestStorageAdvisoryExt4DefaultProjectFiniteRefuses(t *testing.T) {
	ids, err := storageAdvisoryExt4QuotaSubjects(storageNativeLinuxObjectRaw{}, storageNativeExt4Raw{ParentProjectID: 37}, 1001, 1002, storageAdvisoryProspectiveChild)
	storageTestMust(t, err)
	raw := storageNativeQuotaDiskRaw{FilesystemType: 0xef53, Subject: storageNativeQuotaSubject{Kind: storageNativeQuotaProject, ID: ids[2]}, Version: 1, Flags: 2, BlockUnit: 512, HardBlocks: 8, UsedBlocks: 1, HardInodes: 32, UsedInodes: 1}
	if raw.Subject.ID != 0 || !errors.Is(storageAdvisoryLaunchQuotaRefusal(raw), storagebudget.ErrUnknown) {
		t.Fatal("finite default-project quota omitted or unlimited row invented")
	}
	existing, err := storageAdvisoryExt4QuotaSubjects(storageNativeLinuxObjectRaw{UID: 77, GID: 88}, storageNativeExt4Raw{ParentProjectID: 37}, 1001, 1002, storageAdvisoryExistingObject)
	storageTestMust(t, err)
	if existing != ([3]uint32{77, 88, 37}) {
		t.Fatal("existing metadata charged as default-project child", existing)
	}
}

func TestStorageAdvisoryAPFSRestrictionRoutes(t *testing.T) {
	uuid := [16]byte{1}
	container := [16]byte{2}
	m := storageOwnerMembership{VolumeUUID: uuid, ContainerUUID: container}
	volume := storageOwnerConstraint{Kind: 6, VolumeUUID: uuid, ContainerUUID: container}
	containerRow := storageOwnerConstraint{Kind: 5, ContainerUUID: container}
	for _, tc := range []struct {
		name           string
		quota, reserve uint64
		enrolled       bool
		row            storageOwnerConstraint
		want           error
	}{
		{"launch_quota", 1, 0, false, storageOwnerConstraint{}, storagebudget.ErrUnknown},
		{"launch_reserve", 0, 1, false, storageOwnerConstraint{}, storagebudget.ErrUnknown},
		{"enrolled_unknown_to_quota", 1, 0, true, storageOwnerConstraint{}, errStorageOwnerTransitionRequired},
		{"enrolled_unknown_to_reserve", 0, 1, true, storageOwnerConstraint{}, errStorageOwnerTransitionRequired},
		{"enrolled_known_volume_counter_unavailable", 1, 0, true, volume, storagebudget.ErrUnknown},
		{"enrolled_known_volume_retained_without_counter", 0, 0, true, volume, storagebudget.ErrUnknown},
		{"enrolled_container_new_volume_restriction", 1, 0, true, containerRow, errStorageOwnerTransitionRequired},
		{"enrolled_known_container_counter_unavailable", 0, 0, true, containerRow, storagebudget.ErrUnknown},
		{"enrolled_unknown_without_positive", 0, 0, true, storageOwnerConstraint{}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := map[storagebudget.ID]storageOwnerConstraint{}
			if tc.row.Kind != 0 {
				rows[storageIntentTestID(1)] = tc.row
			}
			before := make(map[storagebudget.ID]storageOwnerConstraint)
			for id, row := range rows {
				before[id] = row
			}
			got := storageAdvisoryAPFSRestriction(storageNativeDarwinVolumeRaw{UUID: uuid, Quota: tc.quota, Reserve: tc.reserve}, m, rows, tc.enrolled)
			if got != tc.want {
				t.Fatalf("route %v, want %v", got, tc.want)
			}
			if !reflect.DeepEqual(before, rows) {
				t.Fatal("restriction classification rebound native rows")
			}
		})
	}
}

func TestStorageAdvisoryAPFSNewRestrictionFenceConserves(t *testing.T) {
	uuid := [16]byte{1}
	signal := storageAdvisoryAPFSRestriction(storageNativeDarwinVolumeRaw{UUID: uuid, Quota: 1}, storageOwnerMembership{VolumeUUID: uuid}, nil, true)
	storageAdvisoryTestConservedTransitionFence(t, signal)
}

// Portable protocol fixture: it exercises the production classification signal
// through the production pass/fence join, without claiming native syscall proof.
func storageAdvisoryTestConservedTransitionFence(t *testing.T, signal error) {
	t.Helper()
	if signal != errStorageOwnerTransitionRequired {
		t.Fatal("new restriction lost transition signal", signal)
	}
	o, r, _ := storageWriterFixture(t, true)
	permit, err := o.admitSingleFile(t.Context(), storageWriterPlan(r))
	storageTestMust(t, err)
	defer permit.Close()
	before, err := o.ref.cell.q.InspectHost(t.Context())
	storageTestMust(t, err)
	o.observer = storagePassFailureObserver{failure: signal}
	if _, err = o.beginStorageCapacityPass(storageMutationContext(t.Context(), permit.authority), nil); !errors.Is(err, errStorageOwnerTransitionRequired) {
		t.Fatal("transition did not reach original-owner fence", err)
	}
	after, err := o.ref.cell.q.InspectHost(t.Context())
	storageTestMust(t, err)
	if !after.Configuration.Maintenance || !reflect.DeepEqual(before.Roots, after.Roots) || !reflect.DeepEqual(before.Workspaces, after.Workspaces) || !reflect.DeepEqual(before.Configuration.Filesystems, after.Configuration.Filesystems) || !reflect.DeepEqual(before.Configuration.CapacityConstraints, after.Configuration.CapacityConstraints) || !reflect.DeepEqual(before.Slots, after.Slots) {
		t.Fatal("fence changed immutable definitions, bindings or nonterminal obligations")
	}
	if !errors.Is(o.InitialCensus(t.Context()), errStorageOwnerTransitionRequired) {
		t.Fatal("ordinary census cleared transition fence")
	}
}

func TestStorageAdvisoryExistingLeaseScopeJoins(t *testing.T) {
	uuid := [16]byte{1}
	parent := storageNativeLinuxObjectRaw{MountID: 12, MountKind: storageNativeMountOrdinary, DeviceMajor: 8, DeviceMinor: 1}
	t.Run("linux_same_held_scope", func(t *testing.T) { storageTestMust(t, storageAdvisoryLinuxTargetJoin(parent, parent, uuid, uuid)) })
	for _, name := range []string{"linux_other_volume", "linux_other_mount", "linux_other_mount_kind", "linux_other_device", "linux_missing_mount", "linux_ambiguous_mount_kind"} {
		t.Run(name, func(t *testing.T) {
			p, target := parent, parent
			targetUUID := uuid
			switch name {
			case "linux_other_volume":
				targetUUID = [16]byte{2}
			case "linux_other_mount":
				target.MountID++
			case "linux_other_mount_kind":
				target.MountKind = storageNativeMountUnique
			case "linux_other_device":
				target.DeviceMinor++
			case "linux_missing_mount":
				p.MountID = 0
				target.MountID = 0
			case "linux_ambiguous_mount_kind":
				p.MountKind = 0
				target.MountKind = 0
			}
			if storageAdvisoryLinuxTargetJoin(p, target, uuid, targetUUID) == nil {
				t.Fatal("target quota query crossed an unproved scope")
			}
		})
	}
	darwinParent := storageNativeDarwinTypedObjectRaw{Kind: storageNativeObjectDirectory, RealDevice: 1, RealFSID: [2]int32{1, 2}}
	darwinTarget := storageNativeDarwinTypedObjectRaw{Kind: storageNativeObjectRegular, RealDevice: 1, RealFSID: [2]int32{1, 2}}
	t.Run("darwin_same_held_scope", func(t *testing.T) { storageTestMust(t, storageAdvisoryDarwinTargetJoin(darwinParent, darwinTarget)) })
	for _, name := range []string{"darwin_other_scope", "darwin_other_device", "darwin_missing_scope", "darwin_wrong_target_kind", "darwin_wrong_parent_kind"} {
		t.Run(name, func(t *testing.T) {
			p, target := darwinParent, darwinTarget
			switch name {
			case "darwin_other_scope":
				target.RealFSID[1]++
			case "darwin_other_device":
				target.RealDevice++
			case "darwin_missing_scope":
				p.RealFSID = [2]int32{}
				target.RealFSID = [2]int32{}
			case "darwin_wrong_target_kind":
				target.Kind = storageNativeObjectDirectory
			case "darwin_wrong_parent_kind":
				p.Kind = storageNativeObjectRegular
			}
			if storageAdvisoryDarwinTargetJoin(p, target) == nil {
				t.Fatal("target quota query crossed an unproved scope or object kind")
			}
		})
	}
}
