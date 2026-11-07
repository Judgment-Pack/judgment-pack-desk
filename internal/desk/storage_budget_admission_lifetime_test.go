package desk

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Fixtures that intentionally exercise a leaf mutation obtain real admission;
// read-only control helpers keep their original unprivileged context.
func storageTestMutationContext(t *testing.T, o *storageBudgetOwner, ctx context.Context) context.Context {
	t.Helper()
	a, err := o.beginAdmission(ctx)
	storageTestMust(t, err)
	t.Cleanup(func() { storageTestMust(t, a.Close()) })
	return storageMutationContext(ctx, a)
}

func storageAssertAdmissionCount(t *testing.T, o *storageBudgetOwner, want uint64) {
	t.Helper()
	o.admission.mu.Lock()
	count, entries := o.admission.shared, len(o.admission.leases)
	o.admission.mu.Unlock()
	if count != want || uint64(entries) != want {
		t.Fatalf("retained admission count/map = %d/%d, want %d", count, entries, want)
	}
}

func TestStorageAdmissionBootstrapTransferAndHandoff(t *testing.T) {
	s, c, _ := ownerCrashConstruction(t, ownerActualConfig(t))
	bootstrap := c.bootstrap
	storageTestMust(t, bootstrap.current(t.Context()))
	storageTestMust(t, c.loadOrEnrollOwner(t.Context()))
	if err := bootstrap.current(t.Context()); !errors.Is(err, storagebudget.ErrAuthority) {
		t.Fatal("consumed unpublished proof remained usable", err)
	}
	if err := c.installAdmissionOwner(t.Context(), c.owner); !errors.Is(err, storagebudget.ErrAuthority) {
		t.Fatal("second transfer succeeded", err)
	}
	if a, err := c.owner.beginAdmission(t.Context()); a != nil || !errors.Is(err, storagebudget.ErrBusy) {
		t.Fatal("ordinary caller entered blocked startup", err)
	}
	// Diagnostic table inspection remains available without save authority.
	storageTestMust(t, c.owner.withControl(t.Context(), func(context.Context, *storageIntentTable) error { return nil }))
	e, err := c.forServer(s)
	storageTestMust(t, err)
	s.storageEnrollment = e
	t.Cleanup(func() { storageTestMust(t, e.Close()) })
	originalFD := c.home.lease
	storageTestMust(t, s.prepareStorageAdmissionHandoff(t.Context()))
	h := c.handoff
	if err := c.mutation.current(t.Context(), true); !errors.Is(err, storagebudget.ErrAuthority) {
		t.Fatal("startup mutation survived transfer to read-only handoff", err)
	}
	if err := c.owner.requireMutation(storageMutationContext(t.Context(), h)); !errors.Is(err, storagebudget.ErrAuthority) {
		t.Fatal("handoff became a write capability", err)
	}
	if a, err := c.owner.beginAdmission(t.Context()); a != nil || !errors.Is(err, storagebudget.ErrBusy) {
		t.Fatal("ordinary caller entered blocked handoff", err)
	}
	storageTestMust(t, s.finishInfrastructureStartup(t.Context()))
	if err := c.openAdmissionHandoff(t.Context(), h); !errors.Is(err, storagebudget.ErrAuthority) {
		t.Fatal("ticket opened before actual Q/policy/pass validation", err)
	}
	if c.home.lease != originalFD || c.home.exclusive {
		t.Fatal("handoff replaced original home description or retained EX")
	}
	storageTestMust(t, s.revalidateStorageBudgetStartup(t.Context()))
	if err := c.openAdmissionHandoff(t.Context(), h); !errors.Is(err, storagebudget.ErrAuthority) {
		t.Fatal("consumed ticket reopened admission", err)
	}
	a, err := c.owner.beginAdmission(t.Context())
	storageTestMust(t, err)
	storageTestMust(t, a.Close())
	storageAssertAdmissionCount(t, c.owner, 0)
}

func TestStorageAdmissionHandoffFailureStaysBlocked(t *testing.T) {
	for _, cut := range []string{"cancellation", "live-pass", "restriction"} {
		t.Run(cut, func(t *testing.T) {
			s, c, _ := ownerCrashConstruction(t, ownerActualConfig(t))
			storageTestMust(t, c.loadOrEnrollOwner(t.Context()))
			e, err := c.forServer(s)
			storageTestMust(t, err)
			s.storageEnrollment = e
			t.Cleanup(func() { storageTestMust(t, e.Close()) })
			storageTestMust(t, s.prepareStorageAdmissionHandoff(t.Context()))
			storageTestMust(t, s.finishInfrastructureStartup(t.Context()))
			before, err := c.ref.cell.q.InspectHost(t.Context())
			storageTestMust(t, err)
			ctx := t.Context()
			if cut == "cancellation" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if cut == "live-pass" {
				p, err := c.owner.beginStorageCapacityPassForHandoff(ctx, c.handoff)
				storageTestMust(t, err)
				defer p.Close()
			}
			if cut == "restriction" {
				err := c.owner.fenceStorageTransition(storageMutationContext(ctx, c.handoff))
				if !errors.Is(err, errStorageOwnerTransitionRequired) || !errors.Is(err, storagebudget.ErrAuthority) {
					t.Fatal("read-only handoff did not refuse restriction fence", err)
				}
			}
			if err := s.revalidateStorageBudgetStartup(ctx); err == nil {
				t.Fatal("failed handoff opened admission")
			}
			if a, err := c.owner.beginAdmission(t.Context()); a != nil || err == nil {
				t.Fatal("failed ticket allowed ordinary entry", err)
			}
			if err := c.openAdmissionHandoff(t.Context(), c.handoff); err == nil {
				t.Fatal("failed ticket was retried")
			}
			after, err := c.ref.cell.q.InspectHost(t.Context())
			storageTestMust(t, err)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("failed read-only handoff changed Q")
			}
		})
	}
}

func TestStorageAdmissionMutationAuthorityAndReadOnlyControl(t *testing.T) {
	o, r, config := storageWriterFixture(t, true)
	other, _, _ := storageWriterFixture(t, true)
	before := storageTestTree(t, config)
	lease, err := o.beginAdmission(t.Context())
	storageTestMust(t, err)
	foreign, err := other.beginAdmission(t.Context())
	storageTestMust(t, err)
	defer foreign.Close()
	copied := *lease
	copied.self = &copied
	for _, a := range []*storageMutationAuthority{nil, foreign, &copied} {
		ctx := storageMutationContext(t.Context(), a)
		err := o.withControl(ctx, func(ctx context.Context, table *storageIntentTable) error {
			return table.save(ctx, storageIntentRecord{})
		})
		if !errors.Is(err, storagebudget.ErrAuthority) {
			t.Fatal("read-only/foreign/copied table became writable", err)
		}
	}
	storageTestMust(t, lease.Close())
	if err := o.requireMutation(storageMutationContext(t.Context(), lease)); !errors.Is(err, storagebudget.ErrAuthority) {
		t.Fatal("closed admission remained current", err)
	}
	if !reflect.DeepEqual(before, storageTestTree(t, config)) {
		t.Fatal("authority refusal changed original journal/Q/data")
	}
	// A correct inherited ordinary lease still follows the real complete path.
	a, err := o.admitSingleFile(t.Context(), storageWriterPlan(r))
	storageTestMust(t, err)
	storageAssertAdmissionCount(t, o, 1)
	storageTestMust(t, a.Write(t.Context()))
	storageTestMust(t, a.Close())
	storageAssertAdmissionCount(t, o, 0)
}

func TestStorageAdmissionFenceRequiresExistingAuthority(t *testing.T) {
	for _, mode := range []string{"missing", "closed", "ordinary"} {
		t.Run(mode, func(t *testing.T) {
			o, _, config := storageWriterFixture(t, true)
			before, err := o.ref.cell.q.InspectHost(t.Context())
			storageTestMust(t, err)
			tree := storageTestTree(t, config)
			ctx := t.Context()
			var a *storageMutationAuthority
			if mode != "missing" {
				a, err = o.beginAdmission(ctx)
				storageTestMust(t, err)
				defer a.Close()
				ctx = storageMutationContext(ctx, a)
				if mode == "closed" {
					storageTestMust(t, a.Close())
				}
			}
			err = o.fenceStorageTransition(ctx)
			if !errors.Is(err, errStorageOwnerTransitionRequired) {
				t.Fatal("restriction did not remain visible", err)
			}
			after, err := o.ref.cell.q.InspectHost(t.Context())
			storageTestMust(t, err)
			if mode == "ordinary" {
				if !after.Configuration.Maintenance || after.Configuration.Revision <= before.Configuration.Revision {
					t.Fatal("valid inherited ordinary authority failed existing fence")
				}
				storageAssertAdmissionCount(t, o, 1) // no recursive ordinary entry
				if err := o.requireMutation(ctx); !errors.Is(err, errStorageOwnerTransitionRequired) {
					t.Fatal("fence authority also permitted general mutation after invalidity", err)
				}
			} else if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(tree, storageTestTree(t, config)) {
				t.Fatal("missing/expired authority wrote Q or journal")
			}
			if !o.transitionRequired {
				t.Fatal("missing authority hid positive invalidity")
			}
		})
	}
}

func TestStorageAdmissionConstructionCloseJoinsOutsideMutex(t *testing.T) {
	s := ownerActualNew(t, ownerActualConfig(t))
	c, o := s.storageConstruction, s.storageConstruction.owner
	if s.recoveryStorage != nil {
		storageTestMust(t, s.recoveryStorage.Close())
	}
	a, err := o.beginAdmission(t.Context())
	storageTestMust(t, err)
	defer a.Close()
	done, second := make(chan error, 1), make(chan error, 1)
	go func() { done <- c.Close() }()
	select {
	case <-o.closingStarted:
	case <-time.After(time.Second):
		t.Fatal("construction did not stop owner admission")
	}
	if !c.mu.TryLock() {
		t.Fatal("construction waits for operation drain while holding cleanup mutex")
	}
	c.mu.Unlock()
	if !o.admission.mu.TryLock() {
		t.Fatal("construction waits while holding admission mutex")
	}
	o.admission.mu.Unlock()
	go func() { second <- c.Close() }()
	select {
	case err := <-done:
		t.Fatal("construction discarded original descriptors before operation drain", err)
	default:
	}
	if _, err := o.ref.cell.original.config.Stat("."); err != nil {
		t.Fatal("active lease lost its original descriptor", err)
	}
	storageTestMust(t, a.Close())
	for _, ch := range []chan error{done, second} {
		select {
		case err := <-ch:
			storageTestMust(t, err)
		case <-time.After(time.Second):
			t.Fatal("construction close did not join after final drain")
		}
	}
	if !errors.Is(a.current(t.Context(), true), storagebudget.ErrAuthority) && !errors.Is(a.current(t.Context(), true), os.ErrClosed) {
		t.Fatal("terminal construction retained mutation authority")
	}
}

func TestStorageAdmissionBoundsAndCancelledEntryPreserveState(t *testing.T) {
	o, _, config := storageWriterFixture(t, true)
	before := storageTestTree(t, config)
	o.mu.Lock()
	active := o.active
	o.mu.Unlock()
	o.admission.mu.Lock()
	o.admission.shared = ^uint64(0) // simulate count exhaustion before entry
	o.admission.mu.Unlock()
	if a, err := o.beginAdmission(t.Context()); a != nil || !errors.Is(err, storagebudget.ErrBounds) {
		t.Fatal("shared count overflow admitted", err)
	}
	o.admission.mu.Lock()
	o.admission.shared = 0
	originalGeneration := o.admission.generation
	o.admission.generation = 0
	o.admission.mu.Unlock()
	if a, err := o.beginAdmission(t.Context()); a != nil || !errors.Is(err, storagebudget.ErrBounds) {
		t.Fatal("zero generation admitted", err)
	}
	o.admission.mu.Lock()
	o.admission.generation = originalGeneration
	o.admission.mu.Unlock()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if a, err := o.beginAdmission(ctx); a != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled entry acquired lifetime", err)
	}
	o.mu.Lock()
	afterActive := o.active
	o.mu.Unlock()
	storageAssertAdmissionCount(t, o, 0)
	if active != afterActive || !reflect.DeepEqual(before, storageTestTree(t, config)) {
		t.Fatal("refused entry leaked retains or changed original state")
	}
}
