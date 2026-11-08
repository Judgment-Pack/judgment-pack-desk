package desk

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// These handwritten observations establish the generic invocation's custody and
// lifetime contracts. They are not evidence of concrete/native admission.
type storageGenericCensusObserver struct {
	visit func(context.Context, *os.Root) error
}

func (a *storageGenericCensusObserver) observeStorageFilesystem(ctx context.Context, root *os.Root) (storageFilesystemSample, error) {
	if a.visit != nil {
		if err := a.visit(ctx, root); err != nil {
			return storageFilesystemSample{}, err
		}
	}
	return (storageSyntheticFilesystem{}).observeStorageFilesystem(ctx, root)
}
func (*storageGenericCensusObserver) storageCapacityEnvelope(ctx context.Context, root *os.Root, d storageCapacityDemand) ([storagebudget.MaxCapacityRefs]storagebudget.CapacityVector, error) {
	return storageSyntheticEnvelope(ctx, root, d)
}
func storageGenericCensusCurrent(o *storageBudgetOwner) *storageInitialCensusAuthority {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.currentCensus
}

func TestStorageGenericCensusOriginalBorrowAndNestedChain(t *testing.T) {
	o, config, path := storageWriterFixture(t, false)
	storageTestMust(t, os.MkdirAll(filepath.Join(path, "retained", "nested"), 0700))
	raw := []byte("exact generic retained bytes")
	storageTestMust(t, os.WriteFile(filepath.Join(path, "retained", "nested", "original"), raw, 0600))
	first, err := storagePinChild(config.root, "retained", true)
	storageTestMust(t, err)
	defer first.root.Close()
	second, err := storagePinChild(first.root, "nested", true)
	storageTestMust(t, err)
	defer second.root.Close()
	var captured *storageInitialCensusAuthority
	checked := false
	a := &storageGenericCensusObserver{}
	a.visit = func(ctx context.Context, _ *os.Root) error {
		if checked {
			return nil
		}
		checked = true // nested pass queries the same observer
		captured = storageGenericCensusCurrent(o)
		if captured == nil {
			t.Fatal("generic observation did not borrow the actual invocation")
		}
		storageTestMust(t, captured.current(ctx, o))
		storageAssertAdmissionCount(t, o, 1)
		if storageMutationIn(ctx, o) != captured.mutation {
			t.Fatal("generic census observation lost its one inherited admission")
		}
		effects := []storageCapacityEffectParent{{binding: config, root: second.root, witnesses: []storageNamedRoot{first, second}}}
		p, err := o.beginStorageCapacityPassWithCensus(ctx, effects, captured)
		storageTestMust(t, err)
		defer p.Close()
		if p.use != nil || p.census != captured || len(p.samples) != len(o.roots)+1 {
			t.Fatal("generic pass acquired fresh prefix custody or omitted a root/child")
		}
		_, err = o.rootStorageSample(p.context(ctx), config, second.root)
		storageTestMust(t, err)
		storageTestMust(t, p.Close())
		if use, err := o.ref.acquire(); use != nil || !errors.Is(err, storagebudget.ErrBusy) {
			if use != nil {
				_ = use.Close()
			}
			t.Fatal("pass close released the invocation's EX use", err)
		}
		return nil
	}
	o.observer = a
	storageTestMust(t, o.InitialCensus(t.Context()))
	storageAssertAdmissionCount(t, o, 0)
	if !checked || captured == nil || storageGenericCensusCurrent(o) != nil || !errors.Is(captured.current(t.Context(), o), storagebudget.ErrAuthority) {
		t.Fatal("invocation did not revoke original census authority")
	}
	view, err := o.ref.cell.q.Snapshot(t.Context())
	storageTestMust(t, err)
	if view.Maintenance || len(view.Roots) != len(o.roots) {
		t.Fatal("generic nested census did not publish its complete root set")
	}
	for _, r := range view.Roots {
		if !r.Known {
			t.Fatal("generic census published an unknown root")
		}
	}
	got, err := os.ReadFile(filepath.Join(path, "retained", "nested", "original"))
	storageTestMust(t, err)
	if !bytes.Equal(got, raw) {
		t.Fatal("generic census changed retained bytes")
	}
}

func TestStorageGenericCensusRefusesCounterfeitAndIncompleteAuthority(t *testing.T) {
	o, config, path := storageWriterFixture(t, false)
	other, _, _ := storageWriterFixture(t, false)
	storageTestMust(t, os.MkdirAll(filepath.Join(path, "retained", "nested"), 0700))
	first, err := storagePinChild(config.root, "retained", true)
	storageTestMust(t, err)
	defer first.root.Close()
	second, err := storagePinChild(first.root, "nested", true)
	storageTestMust(t, err)
	defer second.root.Close()
	checked := false
	a := &storageGenericCensusObserver{}
	a.visit = func(ctx context.Context, _ *os.Root) error {
		if checked {
			return nil
		}
		checked = true
		seal := storageGenericCensusCurrent(o)
		if seal == nil {
			t.Fatal("missing actual census token")
		}
		before, err := o.ref.cell.q.InspectHost(ctx)
		storageTestMust(t, err)
		refuse := func(owner *storageBudgetOwner, effects []storageCapacityEffectParent, token *storageInitialCensusAuthority) {
			t.Helper()
			p, err := owner.beginStorageCapacityPassWithCensus(ctx, effects, token)
			if p != nil {
				_ = p.Close()
				t.Fatal("counterfeit/incomplete authority opened a pass")
			}
			if !errors.Is(err, storagebudget.ErrAuthority) {
				t.Fatal("counterfeit/incomplete authority was not refused", err)
			}
		}
		refuse(o, nil, nil)
		// Copy every non-mutex field and supply a fresh self seal. The actual
		// owner pointer, not a caller-assembled tuple, must remain decisive.
		copy := &storageInitialCensusAuthority{active: true, owner: seal.owner, mutation: seal.mutation, ref: seal.ref, cell: seal.cell, home: seal.home, use: seal.use, roots: seal.roots, holds: seal.holds, epoch: seal.epoch}
		copy.self = seal
		refuse(o, nil, copy)
		copy.self = copy
		refuse(o, nil, copy)
		refuse(other, nil, seal)
		refuse(o, []storageCapacityEffectParent{{binding: config, root: second.root}}, seal)
		refuse(o, []storageCapacityEffectParent{{binding: config, root: second.root, witnesses: []storageNamedRoot{second}}}, seal)
		refuse(o, []storageCapacityEffectParent{{binding: config, root: second.root, witnesses: []storageNamedRoot{second, first}}}, seal)
		// Pointer replacement cannot leave a previously issued token current.
		o.mu.Lock()
		o.currentCensus = nil
		o.mu.Unlock()
		refuse(o, nil, seal)
		o.mu.Lock()
		o.currentCensus = seal
		o.mu.Unlock()
		p, err := o.beginStorageCapacityPassWithCensus(ctx, nil, seal)
		storageTestMust(t, err)
		defer p.Close()
		storageTestMust(t, p.Close())
		if !errors.Is(p.current(ctx), os.ErrClosed) {
			t.Fatal("closed generic pass reused")
		}
		after, err := o.ref.cell.q.InspectHost(ctx)
		storageTestMust(t, err)
		if !reflect.DeepEqual(before, after) {
			t.Fatal("authority refusals changed Q tuples")
		}
		return nil
	}
	o.observer = a
	storageTestMust(t, o.InitialCensus(t.Context()))
	if !checked {
		t.Fatal("generic authority tests did not execute")
	}
}

func TestStorageGenericCensusBorrowRefusesChangedOriginalCustody(t *testing.T) {
	for _, kind := range []string{"home", "closed-use", "closed-domain", "epoch"} {
		t.Run(kind, func(t *testing.T) {
			o, _, path := storageWriterFixture(t, false)
			checked := false
			stop := errors.New("handwritten custody change")
			o.observer = &storageGenericCensusObserver{visit: func(ctx context.Context, _ *os.Root) error {
				if checked {
					return nil
				}
				checked = true
				seal := storageGenericCensusCurrent(o)
				if seal == nil {
					t.Fatal("missing actual census authority")
				}
				p, err := o.beginStorageCapacityPassWithCensus(ctx, nil, seal)
				storageTestMust(t, err)
				defer p.Close()
				before, err := o.ref.cell.q.InspectHost(ctx)
				storageTestMust(t, err)
				switch kind {
				case "home":
					name := filepath.Join(path, ".home-use.lock")
					storageTestMust(t, os.Rename(name, name+".original"))
					storageTestMust(t, os.WriteFile(name, nil, 0600))
				case "closed-use":
					storageTestMust(t, seal.use.Close())
				case "closed-domain":
					closed := false
					for _, h := range seal.holds {
						if !h.root.qControl && h.borrowed == nil {
							storageTestMust(t, h.Close())
							closed = true
							break
						}
					}
					if !closed {
						t.Fatal("fixture omitted an original domain EX")
					}
				case "epoch":
					_, err := o.ref.cell.q.BeginMaintenance(ctx, before.Configuration.Revision)
					storageTestMust(t, err)
				}
				if err := p.current(ctx); err == nil {
					t.Fatal("numeric pass survived changed original custody")
				}
				if late, err := o.beginStorageCapacityPassWithCensus(ctx, nil, seal); late != nil || err == nil {
					if late != nil {
						_ = late.Close()
					}
					t.Fatal("changed original custody lent a new pass", err)
				}
				after, err := o.ref.cell.q.InspectHost(ctx)
				storageTestMust(t, err)
				if !reflect.DeepEqual(before.Slots, after.Slots) || !reflect.DeepEqual(before.Roots, after.Roots) || !after.Configuration.Maintenance {
					t.Fatal("custody refusal changed operation/root tuples or withdrew maintenance")
				}
				return stop
			}}
			if err := o.InitialCensus(t.Context()); !errors.Is(err, stop) || !checked {
				t.Fatal("changed original custody was not exercised", err)
			}
			if storageGenericCensusCurrent(o) != nil || o.active != 0 {
				t.Fatal("changed-custody failure leaked the invocation or pass retain")
			}
		})
	}
}

func TestStorageGenericCensusNamedChildReplacementRefusesPublication(t *testing.T) {
	o, config, path := storageWriterFixture(t, false)
	storageTestMust(t, os.MkdirAll(filepath.Join(path, "retained", "nested"), 0700))
	raw := []byte("original child bytes")
	storageTestMust(t, os.WriteFile(filepath.Join(path, "retained", "nested", "original"), raw, 0600))
	first, err := storagePinChild(config.root, "retained", true)
	storageTestMust(t, err)
	defer first.root.Close()
	second, err := storagePinChild(first.root, "nested", true)
	storageTestMust(t, err)
	defer second.root.Close()
	id, err := storageDirectoryID(second.root)
	storageTestMust(t, err)
	before, err := o.ref.cell.q.InspectHost(t.Context())
	storageTestMust(t, err)
	replaced := false
	o.observer = &storageGenericCensusObserver{visit: func(_ context.Context, root *os.Root) error {
		got, err := storageDirectoryID(root)
		if err != nil {
			return err
		}
		if !replaced && got == id {
			replaced = true
			if err := os.Rename(filepath.Join(path, "retained", "nested"), filepath.Join(path, "retained", "old-nested")); err != nil {
				return err
			}
			return os.Mkdir(filepath.Join(path, "retained", "nested"), 0700)
		}
		return nil
	}}
	if err := o.InitialCensus(t.Context()); err == nil || !replaced {
		t.Fatal("replaced exact named child was accepted or not exercised", err)
	}
	after, err := o.ref.cell.q.InspectHost(t.Context())
	storageTestMust(t, err)
	if !after.Configuration.Maintenance || !reflect.DeepEqual(before.Slots, after.Slots) || len(after.Roots) != len(before.Roots) || storageGenericCensusCurrent(o) != nil || o.active != 0 {
		t.Fatal("failed child observation changed operation tuples/published roots or leaked authority")
	}
	got, err := os.ReadFile(filepath.Join(path, "retained", "old-nested", "original"))
	storageTestMust(t, err)
	if !bytes.Equal(got, raw) {
		t.Fatal("refused observation changed original child bytes")
	}
}

func TestStorageGenericCensusConcurrentExitAndLatePassCleanup(t *testing.T) {
	requireStorageLine(t)
	o, config, _ := storageWriterFixture(t, false)
	entered := make(chan *storageInitialCensusAuthority, 1)
	resume := make(chan struct{})
	var unblock sync.Once
	t.Cleanup(func() { unblock.Do(func() { close(resume) }) })
	var first atomic.Bool
	stop := errors.New("handwritten observation failure")
	o.observer = &storageGenericCensusObserver{visit: func(_ context.Context, _ *os.Root) error {
		if first.CompareAndSwap(false, true) {
			entered <- storageGenericCensusCurrent(o)
			<-resume
			return stop
		}
		return nil
	}}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	result := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		result <- o.InitialCensus(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		unblock.Do(func() { close(resume) })
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("generic invocation did not finish cleanup")
		}
	})
	var seal *storageInitialCensusAuthority
	select {
	case seal = <-entered:
	case err := <-result:
		t.Fatal("generic invocation failed before observation", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if seal == nil {
		t.Fatal("actual generic invocation did not lend its token")
	}
	late, err := o.beginStorageCapacityPassWithCensus(t.Context(), nil, seal)
	storageTestMust(t, err)
	defer late.Close()
	checked := make(chan error, 1)
	go func() {
		for i := 0; i < 16; i++ {
			_ = late.current(ctx)
		}
		<-done
		checked <- late.current(ctx)
	}()
	unblock.Do(func() { close(resume) })
	if err = <-result; !errors.Is(err, stop) {
		t.Fatal("initial observation failure lost", err)
	}
	if err = <-checked; !errors.Is(err, storagebudget.ErrAuthority) {
		t.Fatal("late pass accepted observation after invocation exit", err)
	}
	if _, err = storagePassFrom(late.context(t.Context()), o); !errors.Is(err, storagebudget.ErrAuthority) {
		t.Fatal("revoked pass remained available to the concrete observer", err)
	}
	if storageGenericCensusCurrent(o) != nil || o.active != 1 {
		t.Fatal("invocation leaked authority or dropped the pass retain")
	}
	use, err := o.ref.acquire()
	storageTestMust(t, err)
	storageTestMust(t, use.Close())
	view, err := o.ref.cell.q.InspectHost(t.Context())
	storageTestMust(t, err)
	if !view.Configuration.Maintenance {
		t.Fatal("failed census cleared maintenance")
	}
	for _, slot := range view.Slots {
		if slot.Ticket.State != storagebudget.Empty {
			t.Fatal("failed census manufactured an operation")
		}
	}
	storageTestMust(t, o.Close())
	if _, err := config.domain.file.Stat(); err != nil {
		t.Fatal("owner close discarded original descriptors before pass close", err)
	}
	storageTestMust(t, late.Close())
	if _, err := config.domain.file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("last pass retain leaked the owner's original descriptor", err)
	}
}
