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

func TestStorageHostIntentV2LegacyInactivePreservedBeforeFence(t *testing.T) {
	for _, bank := range []int{0, 1} {
		t.Run(string(rune('a'+bank)), func(t *testing.T) {
			o, r, config := storageWriterFixture(t, false)
			journal := filepath.Join(config, storageIntentName)
			f, e := os.OpenFile(journal, os.O_WRONLY, 0)
			storageTestMust(t, e)
			_, e = f.WriteAt([]byte("!"), 0)
			storageTestMust(t, e) // earlier unknown cannot hide legacy
			_, e = f.WriteAt([]byte("DSKHINT1"), int64((63*2+bank)*storageIntentPage))
			storageTestMust(t, e)
			storageTestMust(t, f.Close())
			before, e := o.ref.cell.q.InspectHost(t.Context())
			storageTestMust(t, e)
			bytesBefore := storageTestTree(t, config)
			if e = o.InitialCensus(t.Context()); !errors.Is(e, storagebudget.ErrUnsupported) {
				t.Fatal("inactive legacy census journal not refused", e)
			}
			// A writer reaches the same guard after an original, fully known census.
			if _, e = o.admitSingleFile(t.Context(), storageWriterPlan(r)); !errors.Is(e, storagebudget.ErrUnsupported) {
				t.Fatal("legacy writer journal admitted")
			}
			after, e := o.ref.cell.q.InspectHost(t.Context())
			storageTestMust(t, e)
			if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(bytesBefore, storageTestTree(t, config)) {
				t.Fatal("legacy refusal wrote the fence or journal")
			}
		})
	}
}
func TestStorageHostIntentV2ImmutableCapacity(t *testing.T) {
	old := storageIntentTestPrepared()
	page, e := storageEncodeIntent(old, storageIntentTestID(70), storageIntentTestDigest(71))
	storageTestMust(t, e)
	for _, field := range []string{"count", "id", "generation", "bytes", "inodes", "binding"} {
		t.Run(field, func(t *testing.T) {
			slot := storageIntentSlot{record: old, page: page}
			next := storageNextIntent(slot, storageIntentReserved)
			next.Token = storagebudget.Token{Slot: 0, ID: storageIntentTestID(80), Sequence: 1, Generation: 1}
			next.PolicyRevision = 1
			c := &next.Components[0]
			switch field {
			case "count":
				c.CapacityCount--
			case "id":
				c.Capacity[0].Constraint = storageIntentTestID(79)
			case "generation":
				c.Capacity[0].Generation++
			case "bytes":
				c.Capacity[0].Peak.Bytes--
			case "inodes":
				c.Capacity[0].Peak.Inodes--
			case "binding":
				c.CapacityBinding = storageIntentTestDigest(99)
			}
			if storageIntentTransition(old, next, page) {
				t.Fatal("intent changed immutable capacity", field)
			}
		})
	}
}

// Explicitly synthetic root-specific graph. Container 89 is shared, volume90
// and volume93 are independent restrictions; this is no native backend proof.
type storageGraphFilesystem struct {
	roots    map[storageDirectoryIdentity]storageFilesystemSample
	fallback storageFilesystemSample
}

func (g storageGraphFilesystem) observeStorageFilesystem(ctx context.Context, r *os.Root) (storageFilesystemSample, error) {
	if e := ctx.Err(); e != nil {
		return storageFilesystemSample{}, e
	}
	id, e := storageDirectoryID(r)
	if e != nil {
		return storageFilesystemSample{}, e
	}
	s, ok := g.roots[id]
	if !ok {
		s = g.fallback
	}
	s.Observations = append([]storagebudget.CapacityObservation(nil), s.Observations...)
	return s, nil
}
func (g storageGraphFilesystem) storageCapacityEnvelope(ctx context.Context, r *os.Root, d storageCapacityDemand) ([storagebudget.MaxCapacityRefs]storagebudget.CapacityVector, error) {
	var out [storagebudget.MaxCapacityRefs]storagebudget.CapacityVector
	s, e := g.observeStorageFilesystem(ctx, r)
	if e != nil {
		return out, e
	}
	base, e := storageSyntheticEnvelope(ctx, r, d)
	if e != nil {
		return out, e
	}
	for i, row := range s.Constraints {
		if row.Resources&storagebudget.CapacityBytes != 0 {
			out[i].Bytes = base[0].Bytes
		}
		if row.Resources&storagebudget.CapacityInodes != 0 {
			out[i].Inodes = base[0].Inodes
		}
	}
	return out, nil
}
func storageGraphSample(volume byte) storageFilesystemSample {
	s := storageSyntheticSample()
	pool := storagebudget.CapacityConstraint{ID: storageIntentTestID(89), Identity: storageIntentTestDigest(89), Generation: 7, Resources: storagebudget.CapacityBytes, Headroom: storagebudget.CapacityVector{Bytes: 16 << 20}}
	quota := s.Constraints[0]
	quota.ID = storageIntentTestID(volume)
	quota.Identity = storageIntentTestDigest(volume)
	s.Filesystem.ID = storageIntentTestID(volume + 1)
	s.Filesystem.Identity = storageIntentTestDigest(volume + 1)
	s.Filesystem.Generation = 5
	s.Filesystem.ByteConstraint = pool.ID
	s.Filesystem.InodeConstraint = quota.ID
	s.CapacityCount = 2
	s.CapacityRefs = [storagebudget.MaxCapacityRefs]storagebudget.CapacityRef{{Constraint: pool.ID, Generation: 7}, {Constraint: quota.ID, Generation: 1}}
	s.Constraints = []storagebudget.CapacityConstraint{pool, quota}
	s.Observations = []storagebudget.CapacityObservation{
		{Constraint: pool.ID, Generation: 7, KnownResources: storagebudget.CapacityBytes, Available: storagebudget.CapacityVector{Bytes: 1 << 40}, At: time.Now()},
		{Constraint: quota.ID, Generation: 1, KnownResources: quota.Resources, Available: storagebudget.CapacityVector{Bytes: 1 << 40, Inodes: 1 << 30}, At: time.Now()},
	}
	return s
}
func TestStorageWriterV2OverlapAndLiveObservationUnion(t *testing.T) {
	ref, config := storageTestInstallation(t)
	storageTestMust(t, os.WriteFile(filepath.Join(config, ".data.lock"), nil, 0600))
	graph := storageGraphFilesystem{roots: map[storageDirectoryIdentity]storageFilesystemSample{}, fallback: storageGraphSample(90)}
	other, e := newStorageBudgetOwner(ref, graph, storageBudgetOptions{HeadroomBytes: 16 << 20, HeadroomInodes: 100})
	storageTestMust(t, e)
	defer other.Close()
	root, e := other.configRoot(t.Context(), ".data.lock")
	storageTestMust(t, e)
	_, e = other.anchorRoot(t.Context())
	storageTestMust(t, e)
	storageTestMust(t, os.Mkdir(filepath.Join(config, "second"), 0700))
	storageTestMust(t, os.WriteFile(filepath.Join(config, "second", ".second.lock"), nil, 0600))
	held, e := os.OpenRoot(filepath.Join(config, "second"))
	storageTestMust(t, e)
	id, e := storageDirectoryID(held)
	storageTestMust(t, e)
	storageTestMust(t, held.Close())
	graph.roots[id] = storageGraphSample(93)
	child, e := other.childRoot(t.Context(), root, "second", storagebudget.History, nil, ".second.lock")
	storageTestMust(t, e)
	storageTestMust(t, other.InitialCensus(t.Context()))
	first, e := other.admitSingleFile(t.Context(), storageWriterPlan(root))
	storageTestMust(t, e)
	defer first.Close()
	candidate, e := other.writerComponents(t.Context(), storageWriterPlan(child), child.root)
	storageTestMust(t, e)
	obs, e := other.writerObservations(t.Context(), candidate)
	storageTestMust(t, e)
	if len(obs) != 3 || obs[0].Constraint != storageIntentTestID(89) || obs[1].Constraint != storageIntentTestID(90) || obs[2].Constraint != storageIntentTestID(93) {
		t.Fatal("candidate plus live union lost a shared/independent constraint")
	}
	if !obs[0].At.Equal(graph.fallback.Observations[0].At) {
		t.Fatal("shared counter lost oldest corroborating time")
	}
	status, e := other.ref.cell.q.InspectHost(t.Context())
	storageTestMust(t, e)
	ticket := status.Slots[first.token.Slot].Ticket
	if ticket.Components[0].Filesystem == candidate[0].Filesystem || ticket.Components[0].Capacity[0].Constraint != candidate[0].Capacity[0].Constraint || ticket.Components[0].Capacity[1].Constraint == candidate[0].Capacity[1].Constraint {
		t.Fatal("fixture did not establish distinct filesystems/shared pool/independent quotas")
	}
	broken := graph.roots[id]
	broken.Observations = broken.Observations[:1]
	graph.roots[id] = broken
	if _, e = other.writerObservations(t.Context(), candidate); e == nil {
		t.Fatal("missing domain observation admitted partial constraints")
	}
}
func TestStorageV2UnknownDynamicAndFiniteQuota(t *testing.T) {
	o, r, _ := storageWriterFixture(t, false)
	for _, kind := range []string{"unknown", "dynamic-primary", "finite-quota-missing", "over-four", "generation-zero"} {
		t.Run(kind, func(t *testing.T) {
			s := storageSyntheticSample()
			switch kind {
			case "unknown":
				s.Filesystem.InodeModel = storagebudget.InodesUnknown
			case "dynamic-primary":
				s.Filesystem.InodeModel = storagebudget.InodesDynamic
			case "finite-quota-missing":
				s.CapacityCount = 0
				s.CapacityRefs = [storagebudget.MaxCapacityRefs]storagebudget.CapacityRef{}
				s.Constraints = nil
			case "over-four":
				s.CapacityCount = 5
			case "generation-zero":
				s.RootGeneration = 0
			}
			if e := o.validateStorageSample(s); e == nil {
				t.Fatal("unknown/incomplete domain accepted", kind)
			}
		})
	}
	dynamic := storageSyntheticSample()
	dynamic.Filesystem.InodeModel = storagebudget.InodesDynamic
	dynamic.Filesystem.InodeConstraint = storagebudget.ID{}
	// Dynamic removes only a nonexistent independent pool; retained actual finite
	// volume object quota still appears as an Inodes resource and observation.
	storageTestMust(t, o.validateStorageSample(dynamic))
	o.observer = storageFixedFilesystem{dynamic}
	if _, e := o.rootStorageSample(t.Context(), r, r.root); e == nil {
		t.Fatal("a changed capability inherited an enrolled binding")
	}
}

func storageCensusPhaseFixture(t *testing.T, known bool) (*storageBudgetOwner, []*storageDomainHold, uint64, storagebudget.Census, string, context.Context) {
	t.Helper()
	o, _, config := storageWriterFixture(t, known)
	ctx := storageTestMutationContext(t, o, t.Context())
	home, e := storageOpenOriginalControl(o.ref.cell, ".home-use.lock")
	storageTestMust(t, e)
	storageTestMust(t, lockPrivateData(home.file, true))
	t.Cleanup(func() { storageTestMust(t, home.file.Close()) })
	storageTestMust(t, o.retain())
	t.Cleanup(func() { storageTestMust(t, o.drop()) })
	// Same original format guard and preflight used by the actual lifecycle.
	storageTestMust(t, o.withControl(t.Context(), func(context.Context, *storageIntentTable) error { return nil }))
	v, e := o.ref.cell.q.InspectHost(t.Context())
	storageTestMust(t, e)
	_, e = o.registrationMissingFrom(v)
	storageTestMust(t, e)
	epoch, e := o.ref.cell.q.BeginMaintenance(t.Context(), v.Configuration.Revision)
	storageTestMust(t, e)
	use, e := o.maintenanceUse()
	storageTestMust(t, e)
	t.Cleanup(func() { storageTestMust(t, use.Close()) })
	var holds []*storageDomainHold
	for _, r := range o.orderedRoots() {
		h, e := o.domain(t.Context(), r, use)
		storageTestMust(t, e)
		holds = append(holds, h)
		t.Cleanup(func() { storageTestMust(t, h.Close()) })
	}
	storageTestMust(t, o.withControl(t.Context(), func(ctx context.Context, table *storageIntentTable) error {
		_, _, e := o.inspectInitialCensusPhase(ctx, table, epoch, holds)
		return e
	}))
	c, e := o.measureCensus(t.Context(), o.orderedRoots())
	storageTestMust(t, e)
	return o, holds, epoch, c, config, ctx
}
func TestStorageCensusV2FourTableComparison(t *testing.T) {
	o, _, _ := storageWriterFixture(t, false)
	_, e := o.addWorkspace("four-table-original", 3, storagebudget.Limits{})
	storageTestMust(t, e)
	v, e := o.ref.cell.q.InspectHost(t.Context())
	storageTestMust(t, e)
	add, e := o.registrationMissingFrom(v)
	storageTestMust(t, e)
	if len(add.Roots) != 2 || len(add.Workspaces) != 1 || len(add.Filesystems) != 1 || len(add.CapacityConstraints) != 1 {
		t.Fatal("four-table initial missing set incomplete")
	}
	storageTestMust(t, o.InitialCensus(t.Context()))
	v, e = o.ref.cell.q.InspectHost(t.Context())
	storageTestMust(t, e)
	add, e = o.registrationMissingFrom(v)
	storageTestMust(t, e)
	if storageRegistrationCount(add) != 0 {
		t.Fatal("registered constraint table still missing")
	}
	for _, field := range []string{"count", "refs", "binding", "model", "evidence", "primary-byte", "primary-inode", "constraint-id", "constraint-identity", "constraint-generation", "constraint-resources", "constraint-headroom"} {
		t.Run(field, func(t *testing.T) {
			x := v
			x.Roots = append([]storagebudget.RegisteredRoot(nil), v.Roots...)
			x.Configuration.Filesystems = append([]storagebudget.Filesystem(nil), v.Configuration.Filesystems...)
			x.Configuration.CapacityConstraints = append([]storagebudget.CapacityConstraint(nil), v.Configuration.CapacityConstraints...)
			switch field {
			case "count":
				x.Roots[0].CapacityCount = 0
			case "refs":
				x.Roots[0].CapacityRefs[0].Generation++
			case "binding":
				x.Roots[0].CapacityBinding = storageIntentTestDigest(99)
			case "model":
				x.Configuration.Filesystems[0].InodeModel = storagebudget.InodesDynamic
			case "evidence":
				x.Configuration.Filesystems[0].CapabilityEvidence = storageIntentTestDigest(99)
			case "primary-byte":
				x.Configuration.Filesystems[0].ByteConstraint = storageIntentTestID(99)
			case "primary-inode":
				x.Configuration.Filesystems[0].InodeConstraint = storageIntentTestID(99)
			case "constraint-id":
				x.Configuration.CapacityConstraints[0].ID = storageIntentTestID(99)
			case "constraint-identity":
				x.Configuration.CapacityConstraints[0].Identity = storageIntentTestDigest(99)
			case "constraint-generation":
				x.Configuration.CapacityConstraints[0].Generation++
			case "constraint-resources":
				x.Configuration.CapacityConstraints[0].Resources = storagebudget.CapacityBytes
			case "constraint-headroom":
				x.Configuration.CapacityConstraints[0].Headroom.Bytes++
			}
			if _, e := o.registrationMissingFrom(x); e == nil {
				t.Fatal("v2 immutable definition mismatch accepted", field)
			}
		})
	}
}
func TestStorageCensusActualPhasesRetainOuterCustody(t *testing.T) {
	o, holds, epoch, c, config, ctx := storageCensusPhaseFixture(t, false)
	storageTestMust(t, o.registerInitialCensus(ctx, epoch, holds))
	registered, e := o.ref.cell.q.InspectHost(t.Context())
	storageTestMust(t, e)
	if !registered.Configuration.Maintenance || len(registered.Roots) != 2 || len(registered.Configuration.CapacityConstraints) != 1 || o.censusEpoch != 0 {
		t.Fatal("registration claimed known publication")
	}
	for _, row := range []struct {
		name string
		root *os.Root
	}{{".home-use.lock", o.ref.cell.original.config}, {storageUseName, o.ref.cell.original.config}, {".data.lock", o.ref.cell.original.config}} {
		f, e := row.root.OpenFile(row.name, os.O_RDWR, 0)
		storageTestMust(t, e)
		if e = lockPrivateData(f, true); e == nil {
			f.Close()
			t.Fatal("outer custody released at phase boundary", row.name)
		}
		storageTestMust(t, f.Close())
	}
	// The short bootstrap gate is available between real phases.
	gate, e := storageOpenOriginalControl(o.ref.cell, storageBootstrapName)
	storageTestMust(t, e)
	storageTestMust(t, lockPrivateData(gate.file, true))
	storageTestMust(t, gate.file.Close())
	before := storageTestTree(t, config)
	// The no-add registration path must not call Register or advance a revision.
	storageTestMust(t, o.registerInitialCensus(ctx, epoch, holds))
	after, e := o.ref.cell.q.InspectHost(t.Context())
	storageTestMust(t, e)
	if !reflect.DeepEqual(registered, after) || !reflect.DeepEqual(before, storageTestTree(t, config)) {
		t.Fatal("no-add registration wrote a checkpoint")
	}
	storageTestMust(t, o.publishInitialCensus(ctx, epoch, holds, c))
	published, e := o.ref.cell.q.Snapshot(t.Context())
	storageTestMust(t, e)
	if published.Maintenance || len(published.Roots) != 2 {
		t.Fatal("complete phase publication failed")
	}
	for _, r := range published.Roots {
		if !r.Known {
			t.Fatal("complete root remained unknown")
		}
	}
}
func TestStorageCensusActualPhaseFreshRefusals(t *testing.T) {
	for _, kind := range []string{"epoch", "definition", "missing-constraint", "new-constraint", "journal", "orphan", "replaced-control", "cancelled", "measurement", "missing-hold"} {
		t.Run(kind, func(t *testing.T) {
			o, holds, epoch, c, config, ctx := storageCensusPhaseFixture(t, false)
			storageTestMust(t, o.registerInitialCensus(ctx, epoch, holds))
			switch kind {
			case "epoch":
				v, e := o.ref.cell.q.InspectHost(ctx)
				storageTestMust(t, e)
				_, e = o.ref.cell.q.BeginMaintenance(ctx, v.Configuration.Revision)
				storageTestMust(t, e)
			case "definition":
				for id, row := range o.filesystems {
					row.CapabilityEvidence = storageIntentTestDigest(99)
					o.filesystems[id] = row
					break
				}
			case "missing-constraint":
				for id := range o.constraints {
					delete(o.constraints, id)
					break
				}
			case "new-constraint":
				row := storagebudget.CapacityConstraint{ID: storageIntentTestID(99), Identity: storageIntentTestDigest(99), Generation: 1, Resources: storagebudget.CapacityBytes}
				o.constraints[row.ID] = row
			case "journal":
				f, e := os.OpenFile(filepath.Join(config, storageIntentName), os.O_WRONLY, 0)
				storageTestMust(t, e)
				_, e = f.WriteAt([]byte("!"), int64(63*2*storageIntentPage))
				storageTestMust(t, e)
				storageTestMust(t, f.Close())
			case "orphan":
				// A newly prepared original journal relation is non-idle even without a Q
				// ticket; it must not be converted to a census cancellation/disposition.
				var root *storageRootBinding
				for _, r := range o.roots {
					if r.configControl {
						root = r
					}
				}
				p := storageWriterPlan(root)
				components, e := o.writerComponents(ctx, p, root.root)
				storageTestMust(t, e)
				record := storageIntentTestPrepared()
				record.Components = components
				record.Changes = make([]storagebudget.Change, len(components))
				record.RootIdentities = nil
				record.FilesystemIdentities = nil
				record.Locators = nil
				record.AdmissionEpoch = epoch
				for _, comp := range components {
					r := o.roots[comp.Root]
					record.RootIdentities = append(record.RootIdentities, r.row.Identity)
					record.FilesystemIdentities = append(record.FilesystemIdentities, o.filesystems[comp.Filesystem].Identity)
					record.Locators = append(record.Locators, r.row.Locator)
				}
				storageTestMust(t, o.withControl(ctx, func(ctx context.Context, table *storageIntentTable) error { return table.save(ctx, record) }))
			case "replaced-control":
				name := filepath.Join(config, storageBootstrapName)
				b, e := os.ReadFile(name)
				storageTestMust(t, e)
				storageTestMust(t, os.Rename(name, name+".original"))
				storageTestMust(t, os.WriteFile(name, b, 0600))
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "measurement":
				c.Roots = c.Roots[:1]
			case "missing-hold":
				holds = holds[:1]
			}
			before := storageTestTree(t, config)
			if e := o.publishInitialCensus(ctx, epoch, holds, c); e == nil {
				t.Fatal("fresh phase change did not refuse", kind)
			}
			if !reflect.DeepEqual(before, storageTestTree(t, config)) {
				t.Fatal("phase refusal partially published", kind)
			}
		})
	}
}
func TestStorageCensusInterruptedRegistrationNeverPublishes(t *testing.T) {
	for _, phase := range []string{"after-register", "before-publish"} {
		t.Run(phase, func(t *testing.T) {
			o, _, _ := storageWriterFixture(t, false)
			o.fault = &storageBudgetFault{Name: "census", Phase: phase}
			if e := o.InitialCensus(t.Context()); !errors.Is(e, errStorageOwnerRecovery) {
				t.Fatal("one-shot census interruption not reached", e)
			}
			s, e := o.ref.cell.q.Snapshot(t.Context())
			storageTestMust(t, e)
			if !s.Maintenance || len(s.Roots) != 2 || o.censusEpoch != 0 {
				t.Fatal("interrupted registration published or lost fence")
			}
			for _, r := range s.Roots {
				if r.Known {
					t.Fatal("partial registered root marked known")
				}
			}
		})
	}
}
func TestStorageCensusRealShortControlDeadline(t *testing.T) {
	o, _, _ := storageWriterFixture(t, false)
	storageTestMust(t, o.withControl(t.Context(), func(ctx context.Context, _ *storageIntentTable) error {
		deadline, ok := ctx.Deadline()
		if !ok || deadline.After(time.Now().Add(250*time.Millisecond)) {
			t.Fatal("real 250ms control child was enlarged or removed")
		}
		return nil
	}))
}

func TestStorageCensusSameFormatDomainRefusalFollowsFence(t *testing.T) {
	o, _, config := storageWriterFixture(t, false)
	before, e := o.ref.cell.q.InspectHost(t.Context())
	storageTestMust(t, e)
	storageTestMust(t, os.Remove(filepath.Join(config, ".data.lock")))
	if e = o.InitialCensus(t.Context()); e == nil {
		t.Fatal("missing domain census admitted")
	}
	after, e := o.ref.cell.q.InspectHost(t.Context())
	storageTestMust(t, e)
	if !after.Configuration.Maintenance || after.Configuration.Epoch != before.Configuration.Epoch+1 || after.Configuration.Revision != before.Configuration.Revision+1 || len(after.Roots) != 0 {
		t.Fatal("same-format domain refusal moved before fence or partially registered")
	}
}
