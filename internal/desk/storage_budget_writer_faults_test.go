package desk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func TestStorageCheckedWriterFaultsRetainOriginalOutcome(t *testing.T) {
	points := []struct {
		name, phase string
		host        storageIntentPhase
		state       storagebudget.State
		target      bool
	}{
		{"stage", "before-create", storageIntentStaging, storagebudget.Staging, false}, {"stage", "after-create", storageIntentStaging, storagebudget.Staging, false}, {"stage", "short-write", storageIntentStaging, storagebudget.Staging, false}, {"stage", "after-write", storageIntentStaging, storagebudget.Staging, false}, {"stage", "after-chmod", storageIntentStaging, storagebudget.Staging, false}, {"stage", "after-sync", storageIntentStaging, storagebudget.Staging, false}, {"stage", "after-close", storageIntentStaging, storagebudget.Staging, false},
		{"publish", "before-rename", storageIntentPublishing, storagebudget.Staging, false}, {"publish", "after-rename", storageIntentPublishing, storagebudget.Staging, true}, {"publish", "before-directory-sync", storageIntentPublishing, storagebudget.Staging, true}, {"publish", "after-directory-sync", storageIntentPublishing, storagebudget.Staging, true}, {"publish", "after-directory-close", storageIntentPublishing, storagebudget.Staging, true}, {"publish", "after-directory", storageIntentPublishing, storagebudget.Staging, true}, {"publish", "after-readback", storageIntentPublishing, storagebudget.Staging, true}, {"settle", "before", storageIntentSettling, storagebudget.Staging, true}, {"settle", "after", storageIntentSettling, storagebudget.Terminal, true}, {"checkpoint", "before", storageIntentSettled, storagebudget.Terminal, true}, {"checkpoint", "after", storageIntentSettled, storagebudget.Terminal, true},
	}
	for _, point := range points {
		t.Run(point.name+"-"+point.phase, func(t *testing.T) {
			o, r, config := storageWriterFixture(t, true)
			p := storageWriterPlan(r)
			a, e := o.admitSingleFile(t.Context(), p)
			storageTestMust(t, e)
			o.fault = &storageBudgetFault{Name: point.name, Phase: point.phase}
			if e = a.Write(t.Context()); e == nil {
				t.Fatal("fault ignored")
			}
			storageTestMust(t, a.Close())
			records := storageWriterRecords(t, o)
			if len(records) != 1 || records[0].Phase != point.host {
				t.Fatalf("wrong retained phase: %+v", records)
			}
			s, e := o.ref.cell.q.InspectHostState(t.Context())
			storageTestMust(t, e)
			if s.Slots[0].Ticket.State != point.state {
				t.Fatal("Q charge/outcome changed")
			}
			b, e := os.ReadFile(filepath.Join(config, p.Target))
			if point.target {
				storageTestMust(t, e)
				if !bytes.Equal(b, p.Bytes) {
					t.Fatal("published bytes changed")
				}
			} else if !errors.Is(e, os.ErrNotExist) {
				t.Fatal("effect before publish")
			}
			before := storageTestTree(t, config)
			if _, e = o.admitSingleFile(t.Context(), p); e == nil {
				t.Fatal("uncertain operation replayed")
			}
			if !reflect.DeepEqual(before, storageTestTree(t, config)) {
				t.Fatal("retry grew or rewrote state")
			}
		})
	}
}
func TestStorageHostIntentPersistenceBoundaries(t *testing.T) {
	for _, phase := range []string{"before-write", "short-write", "after-write", "after-sync", "after-close", "after-readback"} {
		t.Run(phase, func(t *testing.T) {
			o, r, config := storageWriterFixture(t, true)
			p := storageWriterPlan(r)
			o.fault = &storageBudgetFault{Name: "intent", Phase: phase}
			if _, e := o.admitSingleFile(t.Context(), p); e == nil {
				t.Fatal("intent fault ignored")
			}
			if _, e := os.Stat(filepath.Join(config, p.Stage)); !errors.Is(e, os.ErrNotExist) {
				t.Fatal("stage before durable advanced intent")
			}
			s, e := o.ref.cell.q.InspectHostState(t.Context())
			storageTestMust(t, e)
			if s.Slots[0].Ticket.State != storagebudget.Empty {
				t.Fatal("Reserve ran before Prepared durability result")
			}
			if phase == "short-write" {
				before := storageTestTree(t, config)
				if _, e = o.admitSingleFile(t.Context(), p); e == nil {
					t.Fatal("torn bank adopted/recovered")
				}
				if !reflect.DeepEqual(before, storageTestTree(t, config)) {
					t.Fatal("torn bank caused retry growth")
				}
			}
		})
	}
}
func TestStorageHostControlAndUseGateOriginalWitness(t *testing.T) {
	for _, name := range []string{storageBootstrapName, storageUseName} {
		t.Run(name, func(t *testing.T) {
			o, r, config := storageWriterFixture(t, true)
			p := storageWriterPlan(r)
			original := filepath.Join(config, name)
			saved := original + ".saved"
			storageTestMust(t, os.Rename(original, saved))
			storageTestMust(t, os.WriteFile(original, nil, 0600))
			before := storageTestTree(t, config)
			if _, e := o.admitSingleFile(t.Context(), p); e == nil {
				t.Fatal("fresh descriptor replaced original gate")
			}
			if !reflect.DeepEqual(before, storageTestTree(t, config)) {
				t.Fatal("gate refusal grew state")
			}
			storageTestMust(t, os.Remove(original))
			storageTestMust(t, os.Rename(saved, original))
		})
	}
	o, r, config := storageWriterFixture(t, true)
	gate, e := storageOpenOriginalControl(o.ref.cell, storageBootstrapName)
	storageTestMust(t, e)
	defer gate.file.Close()
	storageTestMust(t, lockPrivateData(gate.file, true))
	before := storageTestTree(t, config)
	if _, e = o.admitSingleFile(t.Context(), storageWriterPlan(r)); e == nil {
		t.Fatal("busy control waited or bypassed")
	}
	if !reflect.DeepEqual(before, storageTestTree(t, config)) {
		t.Fatal("busy control changed state")
	}
}

func TestStorageHostControlContextBudgetAndUnknownPersistence(t *testing.T) {
	for _, kind := range []string{"cancelled-entry", "Q-contention", "cancelled-after-Prepared"} {
		t.Run(kind, func(t *testing.T) {
			o, r, config := storageWriterFixture(t, true)
			p := storageWriterPlan(r)
			ctx, cancel := context.WithCancel(storageTestMutationContext(t, o, t.Context()))
			defer cancel()
			if kind == "cancelled-entry" {
				cancel()
				before := storageTestTree(t, config)
				called := false
				e := o.withControl(ctx, func(ctx context.Context, table *storageIntentTable) error { called = true; return nil })
				if e == nil || called || !reflect.DeepEqual(before, storageTestTree(t, config)) {
					t.Fatal("cancelled entry performed lane operation")
				}
				return
			}
			if kind == "Q-contention" {
				lock, e := o.ref.cell.anchor.OpenFile("lock", os.O_RDWR, 0)
				storageTestMust(t, e)
				defer lock.Close()
				storageTestMust(t, lockPrivateData(lock, true))
				before := storageTestTree(t, config)
				started := time.Now()
				e = o.withControl(ctx, func(ctx context.Context, table *storageIntentTable) error {
					deadline, ok := ctx.Deadline()
					if !ok || time.Until(deadline) > 250*time.Millisecond {
						t.Fatal("missing finite child budget")
					}
					_, e := o.ref.cell.q.InspectHostState(ctx)
					return e
				})
				if !errors.Is(e, storagebudget.ErrBusy) || time.Since(started) > 2*time.Second || !reflect.DeepEqual(before, storageTestTree(t, config)) {
					t.Fatal("Q contention escaped context budget or wrote")
				}
				return
			}
			components, e := o.writerComponents(t.Context(), p, p.Root.root)
			storageTestMust(t, e)
			e = o.withControl(ctx, func(ctx context.Context, table *storageIntentTable) error {
				status, e := o.ref.cell.q.InspectHostState(ctx)
				if e != nil {
					return e
				}
				r := storageIntentTestPrepared()
				r.OldExists = false
				r.OldIdentity = storageDirectoryIdentity{}
				r.OldLength = 0
				r.OldLinkCount = 0
				r.NewLength = uint64(len(p.Bytes))
				r.AdmissionEpoch = status.Epoch
				r.Target = p.Target
				r.Stage = p.Stage
				r.Workflow = p.Workflow
				r.Operation = p.Operation
				r.Components = components
				r.Changes = make([]storagebudget.Change, len(components))
				r.RootIdentities = nil
				r.FilesystemIdentities = nil
				r.Locators = nil
				for _, c := range components {
					root := o.roots[c.Root]
					r.RootIdentities = append(r.RootIdentities, root.row.Identity)
					r.FilesystemIdentities = append(r.FilesystemIdentities, o.filesystems[c.Filesystem].Identity)
					r.Locators = append(r.Locators, root.row.Locator)
				}
				if e = table.save(ctx, r); e != nil {
					return e
				}
				cancel() // after actual durable page/readback, before Q
				_, e = o.ref.cell.q.Reserve(ctx, storagebudget.Request{Workflow: storageHostWorkflow, Operation: storageIntentOperation(r), Owner: o.ref.cell.binding, Components: components}, nil)
				if e == nil {
					return errors.New("cancelled Q admitted")
				}
				if ctx.Err() == nil {
					return errors.New("cancellation was lost")
				}
				if storageDefinitelyRejected(errors.Join(e, ctx.Err())) != storageIntentNoRejection {
					return errors.New("context uncertainty classified as definite rejection")
				}
				return errors.Join(storagebudget.ErrUnknown, e)
			})
			if e == nil {
				t.Fatal("post-persistence expiry hidden")
			}
			records := storageWriterRecords(t, o)
			if len(records) != 1 || records[0].Phase != storageIntentPrepared {
				t.Fatal("expiry lost original durable obligation")
			}
			before := storageTestTree(t, config)
			if _, e = o.admitSingleFile(t.Context(), p); e == nil {
				t.Fatal("expired original operation retried")
			}
			if !reflect.DeepEqual(before, storageTestTree(t, config)) {
				t.Fatal("expiry grew/remapped operation")
			}
		})
	}
}

func TestStorageHostIntentEachPhaseConservesTicket(t *testing.T) {
	for _, row := range []struct {
		name      string
		remaining int
		admission bool
		host      storageIntentPhase
		state     storagebudget.State
	}{{"Reserved", 1, true, storageIntentReserved, storagebudget.Reserved}, {"Staging", 2, true, storageIntentStaging, storagebudget.Staging}, {"Publishing", 0, false, storageIntentPublishing, storagebudget.Staging}, {"Settling", 1, false, storageIntentSettling, storagebudget.Staging}, {"Settled", 2, false, storageIntentSettled, storagebudget.Terminal}, {"Retired", 3, false, storageIntentRetired, storagebudget.Terminal}} {
		t.Run(row.name, func(t *testing.T) {
			o, r, config := storageWriterFixture(t, true)
			p := storageWriterPlan(r)
			var a *storageSingleFileAdmission
			var e error
			if row.admission {
				o.fault = &storageBudgetFault{Name: "intent", Phase: "after-write", Remaining: row.remaining}
				a, e = o.admitSingleFile(t.Context(), p)
			} else {
				a, e = o.admitSingleFile(t.Context(), p)
				storageTestMust(t, e)
				o.fault = &storageBudgetFault{Name: "intent", Phase: "after-write", Remaining: row.remaining}
				e = a.Write(t.Context())
			}
			if e == nil {
				t.Fatal("journal boundary fault ignored")
			}
			if a != nil {
				storageTestMust(t, a.Close())
			}
			records := storageWriterRecords(t, o)
			if records[0].Phase != row.host {
				t.Fatal("latest possible durable relation lost")
			}
			s, e := o.ref.cell.q.InspectHostState(t.Context())
			storageTestMust(t, e)
			if s.Slots[0].Ticket.State != row.state {
				t.Fatal("ticket promise/outcome changed")
			}
			if row.admission {
				if _, e = os.Stat(filepath.Join(config, p.Stage)); !errors.Is(e, os.ErrNotExist) {
					t.Fatal("stage escaped failed advanced-token persistence")
				}
			}
			before := storageTestTree(t, config)
			if _, e = o.admitSingleFile(t.Context(), p); e == nil {
				t.Fatal("unknown original operation replayed")
			}
			if !reflect.DeepEqual(before, storageTestTree(t, config)) {
				t.Fatal("fault retry rewrote original relation")
			}
		})
	}
}

func TestStorageHostIntentFiniteHistoryEvictionAndLostAllHistory(t *testing.T) {
	o, r, config := storageWriterFixture(t, true)
	p := storageWriterPlan(r)
	for _, name := range []string{"first", "second"} {
		p.Target = name
		p.Stage = "." + name
		p.Operation = name
		a, e := o.admitSingleFile(t.Context(), p)
		storageTestMust(t, e)
		storageTestMust(t, a.Write(t.Context()))
		storageTestMust(t, a.Close())
	}
	status, e := o.ref.cell.q.InspectHostState(t.Context())
	storageTestMust(t, e)
	before := storageTestTree(t, config)
	storageTestMust(t, o.withControl(t.Context(), func(ctx context.Context, table *storageIntentTable) error {
		// Hand-authored detached metadata models legal finite history eviction: a
		// retired host slot reused for a known rejected new operation. It grants no
		// write permit and changes neither the actual journal nor Q. Its predecessor
		// relation is still checked with the actual closed codec/selector.
		old := table.slots[1]
		prepared := old.record
		prepared.Generation++
		prepared.Phase = storageIntentPrepared
		prepared.OperationID = storageIntentTestID(100)
		prepared.PreviousPageDigest = sha256.Sum256(old.page)
		metadata := storageMetadataDigest("desk-storage-single-file-cas-v1", prepared.OldIdentity, r.directory)
		prepared.CASBinding = sha256.Sum256(append(append(append([]byte(nil), metadata[:]...), prepared.OperationID[:]...), []byte(prepared.Target+"\x00"+prepared.Stage)...))
		prepared.Token = storagebudget.Token{}
		prepared.PreviousToken = storagebudget.Token{}
		prepared.PolicyRevision = 0
		prepared.RetirementEpoch = 0
		prepared.StageExists = false
		prepared.StageIdentity = storageDirectoryIdentity{}
		prepared.Outcome = storageIntentUnobserved
		prepared.Evidence = storagebudget.Digest{}
		prepared.Changes = make([]storagebudget.Change, len(prepared.Components))
		page, e := storageEncodeIntent(prepared, o.ref.cell.record.Installation, o.ref.cell.binding)
		if e != nil {
			return e
		}
		rejected := storageNextIntent(storageIntentSlot{prepared, page, 0}, storageIntentRejected)
		rejected.Outcome = storageIntentNotAdmitted
		rejected.Rejection = storageIntentQuota
		b, e := storageEncodeIntent(rejected, o.ref.cell.record.Installation, o.ref.cell.binding)
		if e != nil {
			return e
		}
		selected, e := storageSelectIntent(page, b, 1, o.ref.cell.record.Installation, o.ref.cell.binding)
		if e != nil {
			return e
		}
		table.slots[1] = selected
		if e = o.validateIntentMappings(ctx, table, status); e != nil {
			return e
		}
		for i := range table.slots {
			table.slots[i] = storageIntentSlot{record: storageIntentRecord{Slot: uint16(i)}}
		}
		if e = o.validateIntentMappings(ctx, table, status); !errors.Is(e, errStorageOwnerRecovery) {
			return errors.New("lost-all-history image accepted incorporated terminal")
		}
		return nil
	}))
	if !reflect.DeepEqual(before, storageTestTree(t, config)) {
		t.Fatal("detached history check wrote original state")
	}
	f, e := o.ref.cell.original.config.OpenFile(storageIntentName, os.O_RDWR, 0)
	storageTestMust(t, e)
	_, e = f.WriteAt(make([]byte, storageIntentBytes), 0)
	storageTestMust(t, e)
	storageTestMust(t, f.Sync())
	storageTestMust(t, f.Close())
	p.Target = "third"
	p.Stage = ".third"
	p.Operation = "third"
	before = storageTestTree(t, config)
	if _, e = o.admitSingleFile(t.Context(), p); !errors.Is(e, errStorageOwnerRecovery) {
		t.Fatalf("lost original history renewed authority: %v", e)
	}
	if !reflect.DeepEqual(before, storageTestTree(t, config)) {
		t.Fatal("lost history grew a new mapping/effect")
	}
}

func TestStorageCheckedWriterOriginalCloseBeforeRemovalCredit(t *testing.T) {
	for _, outcome := range []string{"known-close", "unknown-close"} {
		t.Run(outcome, func(t *testing.T) {
			o, root, config := storageWriterFixture(t, false)
			p := storageWriterPlan(root)
			p.ExpectedAbsent, p.Expected = false, []byte("original allocation")
			storageTestMust(t, os.WriteFile(filepath.Join(config, p.Target), p.Expected, 0600))
			storageTestMust(t, o.InitialCensus(t.Context()))
			a, e := o.admitSingleFile(t.Context(), p)
			storageTestMust(t, e)
			defer a.Close()
			original := a.oldTarget
			admitted := storageWriterRecords(t, o)[0]
			// The private removal guard must refuse while the original still
			// has its namespace link, without closing that original witness.
			if e = a.closeRemovedOriginal(admitted); !errors.Is(e, storagebudget.ErrUnknown) || a.oldTarget != original {
				t.Fatal("original with a remaining link was closed or credited")
			}
			if _, e = original.Stat(); e != nil {
				t.Fatal("remaining-link refusal closed the original descriptor")
			}
			before, e := o.ref.cell.q.Snapshot(t.Context())
			storageTestMust(t, e)
			if outcome == "unknown-close" {
				// The seam reports an unknown outcome after the actual Close.
				// It does not simulate an OS-qualified physical close failure.
				o.fault = &storageBudgetFault{Name: "original", Phase: "close"}
			}
			e = a.Write(t.Context())
			if outcome == "known-close" {
				storageTestMust(t, e)
			} else if !errors.Is(e, storagebudget.ErrUnknown) {
				t.Fatalf("unknown original Close outcome qualified removal: %v", e)
			}
			if a.oldTarget != nil {
				t.Fatal("old descriptor escaped its actual close")
			}
			if _, e = original.Stat(); !errors.Is(e, os.ErrClosed) {
				t.Fatal("original descriptor was not actually closed")
			}
			after, e := o.ref.cell.q.Snapshot(t.Context())
			storageTestMust(t, e)
			if outcome == "known-close" {
				if len(after.Tickets) != 0 || after.Installation[storagebudget.Control].Bytes != storagebudget.MetadataBudget+storageHostBudget+uint64(len(p.Bytes)) {
					t.Fatal("known original close did not settle exact replacement")
				}
			} else if !reflect.DeepEqual(before.Installation, after.Installation) || !reflect.DeepEqual(before.Promised, after.Promised) || len(after.Tickets) != 1 || after.Tickets[0].State != storagebudget.Staging || storageWriterRecords(t, o)[0].Phase != storageIntentPublishing {
				t.Fatal("uncertain original Close credited old removal or lost full obligation")
			}
		})
	}
}
