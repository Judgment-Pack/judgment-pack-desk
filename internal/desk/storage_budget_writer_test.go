package desk

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

type storageSyntheticFilesystem struct{}

func (storageSyntheticFilesystem) observeStorageFilesystem(ctx context.Context, root *os.Root) (storageFilesystemSample, error) {
	if e := ctx.Err(); e != nil {
		return storageFilesystemSample{}, e
	}
	if _, e := root.Stat("."); e != nil {
		return storageFilesystemSample{}, e
	}
	return storageSyntheticSample(), nil
}
func storageWriterFixture(t *testing.T, census bool) (*storageBudgetOwner, *storageRootBinding, string) {
	t.Helper()
	ref, config := storageTestInstallation(t)
	storageTestMust(t, os.WriteFile(filepath.Join(config, ".data.lock"), nil, 0600))
	o, e := newStorageBudgetOwner(ref, storageSyntheticFilesystem{}, storageBudgetOptions{HeadroomBytes: 16 << 20, HeadroomInodes: 100})
	storageTestMust(t, e)
	t.Cleanup(func() { storageTestMust(t, o.Close()) })
	r, e := o.configRoot(t.Context(), ".data.lock")
	storageTestMust(t, e)
	_, e = o.anchorRoot(t.Context())
	storageTestMust(t, e)
	if census {
		storageTestMust(t, o.InitialCensus(t.Context()))
	}
	return o, r, config
}
func storageWriterPlan(r *storageRootBinding) storageSingleFilePlan {
	return storageSingleFilePlan{Root: r, Target: "value.json", Stage: ".value-stage", Workflow: "original-fixture", Operation: "save-1", ExpectedAbsent: true, Bytes: []byte("original exact new bytes"), ObjectLimit: 8 << 20}
}
func storageWriterRecords(t *testing.T, o *storageBudgetOwner) []storageIntentRecord {
	t.Helper()
	var out []storageIntentRecord
	storageTestMust(t, o.withControl(t.Context(), func(ctx context.Context, table *storageIntentTable) error {
		for _, s := range table.slots {
			if s.record.Phase != storageIntentEmpty {
				out = append(out, s.record)
			}
		}
		return nil
	}))
	return out
}
func TestStorageCheckedWriterExactNewReplaceAndZero(t *testing.T) {
	o, r, config := storageWriterFixture(t, true)
	plan := storageWriterPlan(r)
	for i, data := range [][]byte{plan.Bytes, []byte("replacement"), {}} {
		plan.Bytes = data
		plan.Operation = string(rune('a' + i))
		a, e := o.admitSingleFile(t.Context(), plan)
		storageTestMust(t, e)
		if _, e = os.Stat(filepath.Join(config, plan.Stage)); !errors.Is(e, os.ErrNotExist) {
			t.Fatal("stage created during admission")
		}
		records := storageWriterRecords(t, o)
		latest := records[len(records)-1]
		if latest.Phase != storageIntentStaging || latest.Token != a.token || latest.PolicyRevision == 0 {
			t.Fatal("exact advanced ticket not durable")
		}
		status, e := o.ref.cell.q.InspectHostState(t.Context())
		storageTestMust(t, e)
		if status.Slots[a.token.Slot].Ticket.PolicyRevision != latest.PolicyRevision {
			t.Fatal("racy policy revision")
		}
		storageTestMust(t, a.Write(t.Context()))
		storageTestMust(t, a.Close())
		got, e := os.ReadFile(filepath.Join(config, plan.Target))
		storageTestMust(t, e)
		if !bytes.Equal(got, data) {
			t.Fatal("exact bytes changed")
		}
		if _, e = os.Stat(filepath.Join(config, plan.Stage)); !errors.Is(e, os.ErrNotExist) {
			t.Fatal("stage retained on success")
		}
		latest = storageWriterRecords(t, o)[len(records)-1]
		status, e = o.ref.cell.q.InspectHostState(t.Context())
		storageTestMust(t, e)
		if latest.Phase != storageIntentRetired || !storageIntentRetiredProof(latest, status) {
			t.Fatal("retired before actual incorporation")
		}
		snapshot, e := o.ref.cell.q.Snapshot(t.Context())
		storageTestMust(t, e)
		if len(snapshot.Tickets) != 0 || snapshot.Installation[storagebudget.Control].Bytes != storagebudget.MetadataBudget+storageHostBudget+uint64(len(data)) {
			t.Fatal("promise not replaced by exact final length")
		}
		plan.ExpectedAbsent = false
		plan.Expected = append([]byte(nil), data...)
	}
}
func TestStorageCheckedWriterUsesOwnerObjectLimit(t *testing.T) {
	o, r, config := storageWriterFixture(t, true)
	p := storageWriterPlan(r)
	p.Bytes = bytes.Repeat([]byte{7}, (2<<20)+1)
	a, e := o.admitSingleFile(t.Context(), p)
	storageTestMust(t, e)
	storageTestMust(t, a.Write(t.Context()))
	storageTestMust(t, a.Close())
	i, e := os.Stat(filepath.Join(config, p.Target))
	storageTestMust(t, e)
	if i.Size() != int64(len(p.Bytes)) {
		t.Fatal("recovery-only limit leaked into shared writer")
	}
	p.Operation = "too-large"
	p.Target = "other"
	p.Stage = ".other-stage"
	p.ObjectLimit = uint64(len(p.Bytes) - 1)
	before := storageTestTree(t, config)
	if _, e = o.admitSingleFile(t.Context(), p); e == nil {
		t.Fatal("original object limit ignored")
	}
	if !reflect.DeepEqual(before, storageTestTree(t, config)) {
		t.Fatal("oversize changed tree")
	}
}
func TestStorageCheckedWriterCopiedWrongStalePermit(t *testing.T) {
	for _, kind := range []string{"copied", "target", "stage", "token", "journal-generation", "closed", "Q-generation"} {
		t.Run(kind, func(t *testing.T) {
			o, r, config := storageWriterFixture(t, true)
			p := storageWriterPlan(r)
			a, e := o.admitSingleFile(t.Context(), p)
			storageTestMust(t, e)
			defer a.Close()
			use := a
			switch kind {
			case "copied":
				copy := storageSingleFileAdmission{self: a.self, owner: a.owner, plan: a.plan, use: a.use, domain: a.domain, slot: a.slot, generation: a.generation, operationID: a.operationID, token: a.token}
				use = &copy
			case "target":
				a.plan.Target = "other"
			case "stage":
				a.plan.Stage = ".other"
			case "token":
				a.token.ID[1] = 1
			case "journal-generation":
				a.generation++
			case "closed":
				storageTestMust(t, a.Close())
			case "Q-generation":
				_, e = o.ref.cell.q.Advance(t.Context(), a.token, o.ref.cell.binding, storagebudget.Uncertain, storageIntentTestDigest(92))
				storageTestMust(t, e)
			}
			before := storageTestTree(t, config)
			if e = use.Write(t.Context()); e == nil {
				t.Fatal("stale/copied authority accepted")
			}
			if !reflect.DeepEqual(before, storageTestTree(t, config)) {
				t.Fatal("refusal performed a syscall effect")
			}
		})
	}
}
func TestStorageCheckedWriterCollisionAndCASRetainPromise(t *testing.T) {
	for _, kind := range []string{"pre-collision", "post-collision", "cas"} {
		t.Run(kind, func(t *testing.T) {
			o, r, config := storageWriterFixture(t, true)
			p := storageWriterPlan(r)
			if kind == "pre-collision" {
				storageTestMust(t, os.WriteFile(filepath.Join(config, p.Stage), []byte("collision"), 0600))
				before := storageTestTree(t, config)
				if _, e := o.admitSingleFile(t.Context(), p); e == nil {
					t.Fatal("collision admitted")
				}
				if !reflect.DeepEqual(before, storageTestTree(t, config)) {
					t.Fatal("collision chose alternate name")
				}
				return
			}
			a, e := o.admitSingleFile(t.Context(), p)
			storageTestMust(t, e)
			defer a.Close()
			name := p.Stage
			if kind == "cas" {
				name = p.Target
			}
			storageTestMust(t, os.WriteFile(filepath.Join(config, name), []byte("concurrent"), 0600))
			before := storageTestTree(t, config)
			if e = a.Write(t.Context()); e == nil {
				t.Fatal("collision/CAS bypassed")
			}
			if !reflect.DeepEqual(before, storageTestTree(t, config)) {
				t.Fatal("collision/CAS performed effect")
			}
			snapshot, e := o.ref.cell.q.Snapshot(t.Context())
			storageTestMust(t, e)
			if len(snapshot.Tickets) != 1 || snapshot.Tickets[0].State != storagebudget.Staging {
				t.Fatal("uncertain collision refunded")
			}
		})
	}
}
func TestStorageCheckedWriterMaintenanceFenceInterleavings(t *testing.T) {
	for _, kind := range []string{"before-admission", "after-admission"} {
		t.Run(kind, func(t *testing.T) {
			o, r, config := storageWriterFixture(t, true)
			p := storageWriterPlan(r)
			var a *storageSingleFileAdmission
			var e error
			if kind == "after-admission" {
				a, e = o.admitSingleFile(t.Context(), p)
				storageTestMust(t, e)
				defer a.Close()
			}
			c, e := o.ref.cell.q.ConfigurationSnapshot(t.Context())
			storageTestMust(t, e)
			epoch, e := o.ref.cell.q.BeginMaintenance(t.Context(), c.Revision)
			storageTestMust(t, e)
			if kind == "before-admission" {
				before := storageTestTree(t, config)
				if _, e = o.admitSingleFile(t.Context(), p); e == nil {
					t.Fatal("pre-existing fence admitted")
				}
				if !reflect.DeepEqual(before, storageTestTree(t, config)) {
					t.Fatal("pre-existing fence caused growth")
				}
				return
			}
			if use, e := o.maintenanceUse(); e == nil {
				use.Close()
				t.Fatal("SH lifetime did not exclude census/Repair gate")
			}
			storageTestMust(t, a.Write(t.Context()))
			storageTestMust(t, a.Close())
			status, e := o.ref.cell.q.InspectHostState(t.Context())
			storageTestMust(t, e)
			if status.Epoch != epoch || !status.Maintenance {
				t.Fatal("writer cleared fence")
			}
			if storageWriterRecords(t, o)[0].AdmissionEpoch >= epoch {
				t.Fatal("test did not cross actual admission epoch")
			}
		})
	}
}
func TestStorageCheckedWriterUnknownReserveAdvanceAndOrphan(t *testing.T) {
	for _, point := range []struct {
		name, phase string
		want        storageIntentPhase
		state       storagebudget.State
	}{{"reserve", "before", storageIntentPrepared, storagebudget.Empty}, {"reserve", "after", storageIntentPrepared, storagebudget.Reserved}, {"advance", "before", storageIntentReserved, storagebudget.Reserved}, {"advance", "after", storageIntentReserved, storagebudget.Staging}} {
		t.Run(point.name+"-"+point.phase, func(t *testing.T) {
			o, r, config := storageWriterFixture(t, true)
			p := storageWriterPlan(r)
			o.fault = &storageBudgetFault{Name: point.name, Phase: point.phase}
			if _, e := o.admitSingleFile(t.Context(), p); e == nil {
				t.Fatal("fault ignored")
			}
			record := storageWriterRecords(t, o)[0]
			if record.Phase != point.want {
				t.Fatal("lost durable crash boundary")
			}
			s, e := o.ref.cell.q.InspectHostState(t.Context())
			storageTestMust(t, e)
			if s.Slots[0].Ticket.State != point.state {
				t.Fatal("unexpected Q outcome")
			}
			before := storageTestTree(t, config)
			if _, e = o.admitSingleFile(t.Context(), p); e == nil {
				t.Fatal("uncertain operation replayed")
			}
			if !reflect.DeepEqual(before, storageTestTree(t, config)) {
				t.Fatal("retry minted a second admission")
			}
			if point.phase == "after" && point.name == "reserve" {
				f, e := o.ref.cell.original.config.OpenFile(storageIntentName, os.O_RDWR, 0)
				storageTestMust(t, e)
				_, e = f.WriteAt(make([]byte, storageIntentBytes), 0)
				storageTestMust(t, e)
				storageTestMust(t, f.Sync())
				storageTestMust(t, f.Close())
				p.Operation = "different"
				before = storageTestTree(t, config)
				if _, e = o.admitSingleFile(t.Context(), p); e == nil {
					t.Fatal("zero banks hid orphan ticket")
				}
				if !reflect.DeepEqual(before, storageTestTree(t, config)) {
					t.Fatal("orphan mapped into new operation")
				}
			}
		})
	}
}

func TestStorageCheckedWriterInspectedEpochFenceProof(t *testing.T) {
	for _, kind := range []string{"fence-before-Reserve", "fence-after-Reserve"} {
		t.Run(kind, func(t *testing.T) {
			o, r, _ := storageWriterFixture(t, true)
			use, e := o.ref.acquire()
			storageTestMust(t, e)
			defer use.Close()
			domain, e := o.domain(t.Context(), r, use)
			storageTestMust(t, e)
			defer domain.Close()
			inspected, e := o.ref.cell.q.InspectHostState(t.Context())
			storageTestMust(t, e)
			p := storageWriterPlan(r)
			components, e := o.writerComponents(t.Context(), p, p.Root.root)
			storageTestMust(t, e)
			observations, e := o.writerObservations(t.Context(), components)
			storageTestMust(t, e)
			request := storagebudget.Request{Workflow: "epoch-proof-test-only", Operation: "proof", Owner: o.ref.cell.binding, Components: components, Entries: []storagebudget.Entry{{Kind: storagebudget.ObjectEntry, Target: p.Target, Stage: p.Stage, Bytes: uint64(len(p.Bytes)), Files: 1}}}
			if kind == "fence-before-Reserve" {
				_, e = o.ref.cell.q.BeginMaintenance(t.Context(), inspected.Revision)
				storageTestMust(t, e)
				before, e := o.ref.cell.q.InspectHostState(t.Context())
				storageTestMust(t, e)
				if _, e = o.ref.cell.q.Reserve(t.Context(), request, observations); !errors.Is(e, storagebudget.ErrUnknown) {
					t.Fatalf("preceding fence did not refuse actual admit: %v", e)
				}
				after, e := o.ref.cell.q.InspectHostState(t.Context())
				storageTestMust(t, e)
				if !reflect.DeepEqual(before, after) {
					t.Fatal("pre-existing fence wrote ticket")
				}
			} else {
				// A policy checkpoint between the inspected epoch and Reserve must be
				// represented by the successful ticket's actual revision, not inspection.
				c, e := o.ref.cell.q.ConfigurationSnapshot(t.Context())
				storageTestMust(t, e)
				storageTestMust(t, o.ref.cell.q.UpdatePolicy(t.Context(), c.Revision, c.Policy))
				token, e := o.ref.cell.q.Reserve(t.Context(), request, observations)
				storageTestMust(t, e)
				admitted, e := o.ref.cell.q.InspectHostState(t.Context())
				storageTestMust(t, e)
				if admitted.Slots[token.Slot].Ticket.PolicyRevision != inspected.Revision+1 {
					t.Fatal("test did not distinguish successful actual policy revision")
				}
				epoch, e := o.ref.cell.q.BeginMaintenance(t.Context(), admitted.Revision)
				storageTestMust(t, e)
				if epoch <= inspected.Epoch {
					t.Fatal("test did not establish later fence")
				}
			}
			if maintenance, e := o.maintenanceUse(); e == nil {
				maintenance.Close()
				t.Fatal("held original SH allowed fence to clear through census/Repair lifetime")
			}
		})
	}
}

type storageFixedFilesystem struct{ sample storageFilesystemSample }

func (f storageFixedFilesystem) observeStorageFilesystem(ctx context.Context, root *os.Root) (storageFilesystemSample, error) {
	if e := ctx.Err(); e != nil {
		return storageFilesystemSample{}, e
	}
	return f.sample, nil
}
func TestStorageCheckedWriterZeroByteNamespaceHeadroom(t *testing.T) {
	o, r, config := storageWriterFixture(t, true)
	p := storageWriterPlan(r)
	p.Bytes = nil
	sample := storageSyntheticSample()
	sample.Observations[0].Available.Inodes = 100
	o.observer = storageFixedFilesystem{sample}
	if _, e := o.admitSingleFile(t.Context(), p); !errors.Is(e, storagebudget.ErrHeadroom) {
		t.Fatalf("zero-byte file did not consume namespace headroom: %v", e)
	}
	record := storageWriterRecords(t, o)[0]
	if record.Phase != storageIntentRejected || record.Rejection != storageIntentHeadroom {
		t.Fatal("known no-ticket rejection not recorded")
	}
	if _, e := os.Stat(filepath.Join(config, p.Stage)); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("zero-byte stage created before admission")
	}
}

func TestStorageCheckedWriterAdmittedOriginalInodeCAS(t *testing.T) {
	for _, boundary := range []string{"before-stage", "before-publication"} {
		t.Run(boundary, func(t *testing.T) {
			o, root, config := storageWriterFixture(t, false)
			p := storageWriterPlan(root)
			p.ExpectedAbsent, p.Expected = false, []byte("same original bytes")
			storageTestMust(t, os.WriteFile(filepath.Join(config, p.Target), p.Expected, 0600))
			storageTestMust(t, o.InitialCensus(t.Context()))
			a, e := o.admitSingleFile(t.Context(), p)
			storageTestMust(t, e)
			defer a.Close()
			admitted := storageWriterRecords(t, o)[0]
			if admitted.OldIdentity == (storageDirectoryIdentity{}) || a.oldTarget == nil {
				t.Fatal("original admitted inode not retained")
			}
			if boundary == "before-publication" {
				// Stop the original attempt before rename. Test the exact private
				// pre-publication CAS function separately; never resume/replay Write.
				o.fault = &storageBudgetFault{Name: "publish", Phase: "before-rename"}
				if e = a.Write(t.Context()); e == nil {
					t.Fatal("publication boundary fault ignored")
				}
			}
			alias := filepath.Join(config, "retained-original")
			storageTestMust(t, os.Rename(filepath.Join(config, p.Target), alias))
			storageTestMust(t, os.WriteFile(filepath.Join(config, p.Target), p.Expected, 0600))
			original, e := os.Stat(alias)
			storageTestMust(t, e)
			substituted, e := os.Stat(filepath.Join(config, p.Target))
			storageTestMust(t, e)
			if os.SameFile(original, substituted) || original.Size() != substituted.Size() {
				t.Fatal("fixture did not retain a distinct same-size original inode")
			}
			before := storageTestTree(t, config)
			beforeRecord := storageWriterRecords(t, o)[0]
			beforeQ, e := o.ref.cell.q.InspectHostState(t.Context())
			storageTestMust(t, e)
			if boundary == "before-stage" {
				e = a.Write(t.Context())
			} else {
				_, e = a.admittedBaseline(t.Context(), admitted)
			}
			if !errors.Is(e, storagebudget.ErrStale) {
				t.Fatalf("same-byte inode substituted for admitted original: %v", e)
			}
			afterQ, e := o.ref.cell.q.InspectHostState(t.Context())
			storageTestMust(t, e)
			afterRecord := storageWriterRecords(t, o)[0]
			if !reflect.DeepEqual(before, storageTestTree(t, config)) || !reflect.DeepEqual(beforeQ, afterQ) || !reflect.DeepEqual(beforeRecord, afterRecord) || afterRecord.OldIdentity != admitted.OldIdentity || afterQ.Slots[a.token.Slot].Ticket.State != storagebudget.Staging {
				t.Fatal("inode refusal changed original mapping/namespace/charge or credited old removal")
			}
			if boundary == "before-stage" {
				if _, e = os.Stat(filepath.Join(config, p.Stage)); !errors.Is(e, os.ErrNotExist) {
					t.Fatal("same-byte substituted inode caused stage growth")
				}
			} else if info, e := os.Stat(filepath.Join(config, p.Stage)); e != nil || info.Size() != int64(len(p.Bytes)) || afterRecord.Phase != storageIntentPublishing {
				t.Fatal("pre-publication refusal lost conservative completed stage obligation")
			}
		})
	}
}

// Test-only qualified facts. These fixed IDs, generations, inode model and
// 4096-byte metadata allowance are synthetic; they qualify no real filesystem.
func storageSyntheticSample() storageFilesystemSample {
	constraint := storagebudget.CapacityConstraint{ID: storageIntentTestID(90), Identity: storageIntentTestDigest(90), Generation: 1, Resources: storagebudget.CapacityBytes | storagebudget.CapacityInodes, Headroom: storagebudget.CapacityVector{Bytes: 16 << 20, Inodes: 100}}
	return storageFilesystemSample{
		Filesystem:     storagebudget.Filesystem{ID: storageIntentTestID(91), Identity: storageIntentTestDigest(91), Generation: 1, InodeModel: storagebudget.InodesFinite, CapabilityEvidence: storageIntentTestDigest(92), ByteConstraint: constraint.ID, InodeConstraint: constraint.ID},
		RootGeneration: 1, CapacityCount: 1, CapacityRefs: [storagebudget.MaxCapacityRefs]storagebudget.CapacityRef{{Constraint: constraint.ID, Generation: 1}}, Constraints: []storagebudget.CapacityConstraint{constraint},
		Observations: []storagebudget.CapacityObservation{{Constraint: constraint.ID, Generation: 1, KnownResources: constraint.Resources, Available: storagebudget.CapacityVector{Bytes: 1 << 40, Inodes: 1 << 30}, At: time.Now()}},
	}
}
func storageSyntheticEnvelope(ctx context.Context, root *os.Root, d storageCapacityDemand) ([storagebudget.MaxCapacityRefs]storagebudget.CapacityVector, error) {
	var out [storagebudget.MaxCapacityRefs]storagebudget.CapacityVector
	if e := ctx.Err(); e != nil {
		return out, e
	}
	if _, e := root.Stat("."); e != nil {
		return out, e
	}
	p, e := storageVectorAdd(d.Physical, d.Settlement)
	if e != nil {
		return out, e
	}
	if p.Files > 1<<40-p.Directories {
		return out, storagebudget.ErrBounds
	}
	n := p.Files + p.Directories
	// Finite synthetic fixture arithmetic, authored independently of production.
	if n > (1<<60-p.Bytes)/4096 || p.Bytes+n*4096 > 1<<60-4096 {
		return out, storagebudget.ErrBounds
	}
	out[0] = storagebudget.CapacityVector{Bytes: p.Bytes + n*4096 + 4096, Inodes: n}
	return out, nil
}
func (storageSyntheticFilesystem) storageCapacityEnvelope(ctx context.Context, root *os.Root, d storageCapacityDemand) ([storagebudget.MaxCapacityRefs]storagebudget.CapacityVector, error) {
	return storageSyntheticEnvelope(ctx, root, d)
}
func (storageFixedFilesystem) storageCapacityEnvelope(ctx context.Context, root *os.Root, d storageCapacityDemand) ([storagebudget.MaxCapacityRefs]storagebudget.CapacityVector, error) {
	return storageSyntheticEnvelope(ctx, root, d)
}
