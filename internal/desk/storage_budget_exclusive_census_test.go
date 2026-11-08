package desk

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// A codec-valid, source-bound definite rejection fixture. It issues no Q
// operation or native effect and never claims a measured grant/receipt.
func exclusiveSeedRejected(t *testing.T, o *storageBudgetOwner, count int) {
	t.Helper()
	var root *storageRootBinding
	for _, r := range o.roots {
		if r.configControl {
			root = r
		}
	}
	if root == nil {
		t.Fatal("missing config root")
	}
	v, err := o.ref.cell.q.InspectHost(t.Context())
	storageTestMust(t, err)
	a, err := o.beginAdmission(t.Context())
	storageTestMust(t, err)
	defer func() { storageTestMust(t, a.Close()) }()
	ctx := storageMutationContext(t.Context(), a)
	for i := 0; i < count; i++ {
		component := storageOwnerComponent(root, storagebudget.ID{}, storagebudget.Control, storagebudget.Vector{Bytes: 1, Files: 1}, storagebudget.Vector{}, storagebudget.Vector{})
		for j := 0; j < int(component.CapacityCount); j++ {
			component.Capacity[j].Peak = storagebudget.CapacityVector{Bytes: 1 << 20, Inodes: 8}
		}
		r := storageIntentRecord{Slot: uint16(i), Generation: 1, Phase: storageIntentPrepared, OperationID: storageIntentTestID(byte(i + 1)), CASBinding: storageIntentTestDigest(byte(i + 1)), AdmissionEpoch: v.Configuration.Epoch, NewLength: 1, Target: fmt.Sprintf("rejected-%d", i), Stage: fmt.Sprintf(".rejected-stage-%d", i), Workflow: "rejected-fixture", Operation: fmt.Sprintf("operation-%d", i), Components: []storagebudget.Component{component}, Changes: []storagebudget.Change{{}}, RootIdentities: []storagebudget.Digest{root.row.Identity}, FilesystemIdentities: []storagebudget.Digest{o.filesystems[component.Filesystem].Identity}, Locators: []string{root.row.Locator}}
		storageTestMust(t, o.withControl(ctx, func(ctx context.Context, table *storageIntentTable) error {
			if err := table.save(ctx, r); err != nil {
				return err
			}
			rejected := storageNextIntent(table.slots[i], storageIntentRejected)
			rejected.Outcome = storageIntentNotAdmitted
			rejected.Rejection = storageIntentQuota
			return table.save(ctx, rejected)
		}))
	}
}

func TestStorageExclusiveCensusRejectedHealthyRestarts(t *testing.T) {
	requireStorageLine(t)
	for _, count := range []int{1, 64} {
		t.Run(fmt.Sprintf("rejected-%d", count), func(t *testing.T) {
			cfg := ownerActualConfig(t)
			first := ownerActualNew(t, cfg)
			cfg.ProjectDir = first.projectDir
			exclusiveSeedRejected(t, first.storageConstruction.owner, count)
			before, err := first.storageConstruction.ref.cell.q.InspectHost(t.Context())
			storageTestMust(t, err)
			journal, err := os.ReadFile(filepath.Join(cfg.DeskConfigDir, storageIntentName))
			storageTestMust(t, err)
			storageTestMust(t, first.Close())
			for restart := 0; restart < 2; restart++ {
				next := ownerActualNew(t, cfg)
				c := next.storageConstruction
				after, err := c.ref.cell.q.InspectHost(t.Context())
				storageTestMust(t, err)
				if after.Configuration.Epoch != before.Configuration.Epoch || after.Configuration.Maintenance || c.grants["anchor/maintenance"] || !c.grants["anchor/census"] {
					t.Fatal("healthy startup advanced epoch or skipped census")
				}
				if c.exclusiveCensus == nil || c.exclusiveCensus.stage != storageExclusiveCompleted || c.owner.active != 0 || c.startupUse != nil || c.startupWork != nil || len(c.startupHolds) != 0 {
					t.Fatal("startup did not drain exact phase/outer holds")
				}
				for _, root := range after.Roots {
					if root.Generation == 0 {
						t.Fatal("lost original definition")
					}
				}
				got, err := os.ReadFile(filepath.Join(cfg.DeskConfigDir, storageIntentName))
				storageTestMust(t, err)
				if sha256.Sum256(got) != sha256.Sum256(journal) {
					t.Fatal("census rewrote reusable journal")
				}
				if len(storageWriterRecords(t, c.owner)) != count {
					t.Fatal("full table or original history was lost")
				}
				storageTestMust(t, next.Close())
			}
		})
	}
}

func TestStorageExclusiveCensusRetiredHistoryControls(t *testing.T) {
	requireStorageLine(t)
	cfg := ownerActualConfig(t)
	first := ownerActualNew(t, cfg)
	cfg.ProjectDir = first.projectDir
	o := first.storageConstruction.owner
	var root *storageRootBinding
	for _, r := range o.roots {
		if r.configControl {
			root = r
		}
	}
	for i := 0; i < 2; i++ {
		plan := storageWriterPlan(root)
		plan.Target = fmt.Sprintf("retired-%d.json", i)
		plan.Stage = fmt.Sprintf(".retired-stage-%d", i)
		plan.Operation = fmt.Sprintf("retired-%d", i)
		a, err := o.admitSingleFile(t.Context(), plan)
		storageTestMust(t, err)
		storageTestMust(t, a.Write(t.Context()))
		storageTestMust(t, a.Close())
	}
	records := storageWriterRecords(t, o)
	view, err := o.ref.cell.q.InspectHostState(t.Context())
	storageTestMust(t, err)
	if len(records) != 2 || records[0].Token.Slot != records[1].Token.Slot || records[0].Token == view.Slots[records[0].Token.Slot].PersistedApplied || records[1].Token != view.Slots[records[1].Token.Slot].PersistedApplied {
		t.Fatal("fixture did not distinguish later history from exact retired")
	}
	storageTestMust(t, first.Close())
	next := ownerActualNew(t, cfg)
	after, err := next.storageConstruction.ref.cell.q.InspectHostState(t.Context())
	storageTestMust(t, err)
	if after.Epoch != view.Epoch || after.Maintenance {
		t.Fatal("retired history self-invalidated")
	}
	for _, record := range storageWriterRecords(t, next.storageConstruction.owner) {
		if !storageIntentRetiredProof(record, after) {
			t.Fatal("retired mapping proof changed")
		}
	}
}

func TestStorageExclusiveCensusUnknownMaintenanceRefused(t *testing.T) {
	requireStorageLine(t)
	cfg := ownerActualConfig(t)
	first := ownerActualNew(t, cfg)
	cfg.ProjectDir = first.projectDir
	c := first.storageConstruction
	v, err := c.ref.cell.q.InspectHost(t.Context())
	storageTestMust(t, err)
	_, err = c.ref.cell.q.BeginMaintenance(t.Context(), v.Configuration.Revision)
	storageTestMust(t, err)
	storageTestMust(t, first.Close())
	next, err := New(cfg)
	storageTestMust(t, err)
	defer next.Close()
	if !next.storageReadOnly {
		t.Fatal("unknown maintenance cause cleared by startup")
	}
}

func exclusiveAwait(t *testing.T, entered <-chan struct{}, done <-chan error) {
	t.Helper()
	select {
	case <-entered:
		return
	case err := <-done:
		t.Fatal("startup stopped before owned boundary", err)
	case <-time.After(10 * time.Second):
		t.Fatal("owned boundary not reached")
	}
}
func TestStorageExclusiveCensusCloseRetainsActualDrain(t *testing.T) {
	requireStorageLine(t)
	for _, point := range []string{"capture", "walk-read-dir", "walk-dir-close", "child-root-close", "allowance-native-read", "allowance-parent-close", "post-spend", "control-return", "helper-drained"} {
		t.Run(point, func(t *testing.T) {
			cfg := ownerActualConfig(t)
			storageTestMust(t, os.MkdirAll(filepath.Join(cfg.DeskConfigDir, "retained", "nested"), 0700))
			first := ownerActualNew(t, cfg)
			cfg.ProjectDir = first.projectDir
			storageTestMust(t, first.Close())
			_, c, _ := ownerCrashConstructionCloseCheck(t, cfg, func(err error) {
				if !errors.Is(err, storagebudget.ErrBusy) && !errors.Is(err, os.ErrClosed) {
					t.Error(err)
				}
			})
			entered, release := make(chan struct{}), make(chan struct{})
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
			}()
			c.exclusiveFault = &storageExclusiveCensusFault{point: point, entered: entered, release: release}
			loadDone := make(chan error, 1)
			go func() { loadDone <- c.loadOrEnrollOwner(t.Context()) }()
			exclusiveAwait(t, entered, loadDone)
			p := c.exclusiveCensus
			o := c.owner
			exclusiveAssertOriginalEX(t, c)
			if measured, _ := o.ownedMeasurement(); measured != nil {
				t.Fatal("held original invocation published measurement before finalization")
			}
			o.mu.Lock()
			active := o.active
			o.mu.Unlock()
			want := uint64(2)
			if point == "allowance-native-read" || point == "allowance-parent-close" || point == "post-spend" || point == "control-return" {
				want = 3
			}
			if active != want {
				t.Fatalf("active=%d want=%d before terminal close", active, want)
			}
			if err := c.releaseStartupHolds(); !errors.Is(err, storagebudget.ErrBusy) || c.startupUse == nil || c.startupWork == nil {
				t.Fatal("blocked invocation released outer custody", err)
			}
			if pass, err := o.beginStorageCapacityPass(t.Context(), nil); pass != nil || err == nil {
				t.Fatal("ordinary observer entered exclusive phase", err)
			}
			if pass, err := o.beginStorageCapacityPassWithStartup(t.Context(), nil, c); pass != nil || err == nil {
				t.Fatal("old startup observer entered installed exclusive phase", err)
			}
			if err := o.requireMutation(storageMutationContext(t.Context(), c.mutation)); err == nil {
				t.Fatal("generic startup mutation entered exclusive phase")
			}
			closeDone := make(chan error, 1)
			go func() { closeDone <- c.Close() }()
			select {
			case <-o.closingStarted:
			case <-time.After(time.Second):
				t.Fatal("Close did not revoke promptly")
			}
			if err := p.permit(t.Context()); err == nil {
				t.Fatal("retained pin resurrected authority")
			}
			select {
			case err := <-closeDone:
				t.Fatal("terminal cleanup overtook owned drain", err)
			default:
			}
			o.mu.Lock()
			still := o.active
			closed := o.closed
			o.mu.Unlock()
			if still != want || closed {
				t.Fatal("terminal close dropped executing helper/invocation")
			}
			exclusiveAssertOriginalEX(t, c)
			close(release)
			select {
			case err := <-loadDone:
				if err == nil {
					t.Fatal("closed invocation reported success")
				}
			case <-time.After(10 * time.Second):
				t.Fatal("invocation failed to drain")
			}
			var closeErr error
			select {
			case closeErr = <-closeDone:
			case <-time.After(10 * time.Second):
				t.Fatal("terminal continuation deadlocked")
			}
			if closeErr == nil {
				t.Fatal("first terminal Close/cancellation error lost")
			}
			o.mu.Lock()
			active = o.active
			closed = o.closed
			o.mu.Unlock()
			if active != 0 || !closed || c.startupUse != nil || c.startupWork != nil || len(c.startupHolds) != 0 {
				t.Fatal("actual final drain leaked custody")
			}
			if again := c.Close(); again == nil || again.Error() != closeErr.Error() {
				t.Fatal("repeated Close changed final outcome", again)
			}
		})
	}
}

func TestStorageExclusiveCensusFailedSpentGroupFreshRestart(t *testing.T) {
	requireStorageLine(t)
	cfg := ownerActualConfig(t)
	first := ownerActualNew(t, cfg)
	cfg.ProjectDir = first.projectDir
	exclusiveSeedRejected(t, first.storageConstruction.owner, 1)
	storageTestMust(t, first.Close())
	_, c, close := ownerCrashConstruction(t, cfg)
	c.fault = &storageBootstrapFault{Name: "anchor/census", Phase: "before-effect"}
	if err := c.loadOrEnrollOwner(t.Context()); err == nil {
		t.Fatal("spent group fault not reached")
	}
	if !c.grants["anchor/census"] || c.grants["anchor/maintenance"] || c.owner.active != 0 {
		t.Fatal("uncertain group refunded or leaked lifetime")
	}
	close()
	next := ownerActualNew(t, cfg)
	if next.storageConstruction.owner.censusEpoch == 0 {
		t.Fatal("fresh original healthy restart disabled")
	}
}

func TestStorageExclusiveCensusCancellationAndCleanupError(t *testing.T) {
	requireStorageLine(t)
	for _, kind := range []string{"canceled-after-spend", "owned-close-error"} {
		t.Run(kind, func(t *testing.T) {
			cfg := ownerActualConfig(t)
			first := ownerActualNew(t, cfg)
			cfg.ProjectDir = first.projectDir
			storageTestMust(t, first.Close())
			_, c, closeOriginal := ownerCrashConstruction(t, cfg)
			entered, release := make(chan struct{}), make(chan struct{})
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
			}()
			injected := errors.New("owned census cleanup result unknown")
			point := "post-spend"
			var faultErr error
			if kind == "owned-close-error" {
				point = "walk-dir-close"
				faultErr = injected
			}
			c.exclusiveFault = &storageExclusiveCensusFault{point: point, entered: entered, release: release, err: faultErr}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- c.loadOrEnrollOwner(ctx) }()
			exclusiveAwait(t, entered, done)
			if kind == "canceled-after-spend" {
				cancel()
			}
			close(release)
			select {
			case err := <-done:
				want := injected
				if kind == "canceled-after-spend" {
					want = context.Canceled
				}
				if !errors.Is(err, want) {
					t.Fatal("original failure/cancellation lost", err)
				}
				if kind == "owned-close-error" && !errors.Is(err, storagebudget.ErrUnknown) {
					t.Fatal("owned Close failure lost typed unknown refusal", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("failed invocation did not drain")
			}
			if c.owner.active != 0 || c.startupUse != nil || c.startupWork != nil || len(c.startupHolds) != 0 {
				t.Fatal("failed cleanup leaked custody")
			}
			if measured, _ := c.owner.ownedMeasurement(); measured != nil {
				t.Fatal("failed/canceled actual exclusive census published measurement")
			}
			c.owner.mu.Lock()
			pending := c.owner.measurement.startup
			c.owner.mu.Unlock()
			if pending != nil {
				t.Fatal("outer startup finalizer retained failed measurement")
			}
			if kind == "canceled-after-spend" && !c.grants["anchor/census"] {
				t.Fatal("spent group was refunded")
			}
			closeOriginal()
			next := ownerActualNew(t, cfg)
			if next.storageConstruction.owner.censusEpoch == 0 {
				t.Fatal("fresh invocation disabled after failure")
			}
		})
	}
}

func TestStorageExclusiveRetiredEpochPredicatesUnchanged(t *testing.T) {
	// Journal slot1, Q slot7: slot-number coincidence is never a proof.
	r := storageIntentRecord{Slot: 1, Phase: storageIntentRetired, AdmissionEpoch: 7, RetirementEpoch: 7, Token: storagebudget.Token{Slot: 7, ID: storageIntentTestID(1), Sequence: 1, Generation: 3}}
	state := storagebudget.HostState{Epoch: 7}
	state.Slots[7].PersistedApplied = storagebudget.Token{Slot: 7, ID: storageIntentTestID(2), Sequence: 2, Generation: 3}
	state.Slots[7].PersistedNextSequence = 3
	if !storageIntentRetiredProof(r, state) {
		t.Fatal("later durable history rejected at same epoch")
	}
	state.Epoch = 8
	if storageIntentRetiredProof(r, state) {
		t.Fatal("later-history epoch fence weakened")
	}
	state.Slots[7].PersistedApplied = r.Token
	state.Slots[7].PersistedNextSequence = 2
	if !storageIntentRetiredProof(r, state) {
		t.Fatal("exact persisted tuple incorrectly depends on epoch")
	}
	state.Slots[7].PersistedNextSequence = 1
	if storageIntentRetiredProof(r, state) {
		t.Fatal("next sequence omitted from exact proof")
	}
}

func TestStorageExclusiveCensusUncertainReserveNotAdopted(t *testing.T) {
	requireStorageLine(t)
	cfg := ownerActualConfig(t)
	first := ownerActualNew(t, cfg)
	cfg.ProjectDir = first.projectDir
	o := first.storageConstruction.owner
	var root *storageRootBinding
	for _, r := range o.roots {
		if r.configControl {
			root = r
		}
	}
	plan := storageWriterPlan(root)
	o.fault = &storageBudgetFault{Name: "reserve", Phase: "after"}
	if a, err := o.admitSingleFile(t.Context(), plan); a != nil || err == nil {
		t.Fatal("uncertain Reserve fixture did not stop")
	}
	records := storageWriterRecords(t, o)
	view, err := o.ref.cell.q.InspectHostState(t.Context())
	storageTestMust(t, err)
	if len(records) != 1 || records[0].Phase != storageIntentPrepared || view.Slots[0].Ticket.State != storagebudget.Reserved {
		t.Fatal("fixture lost Prepared/Reserved uncertainty")
	}
	paths := []string{storageIntentName, filepath.Join(storageAnchorName, "checkpoint.a"), filepath.Join(storageAnchorName, "checkpoint.b"), filepath.Join(storageAnchorName, "tickets.a"), filepath.Join(storageAnchorName, "tickets.b")}
	before := map[string][32]byte{}
	for _, name := range paths {
		raw, err := os.ReadFile(filepath.Join(cfg.DeskConfigDir, name))
		storageTestMust(t, err)
		before[name] = sha256.Sum256(raw)
	}
	storageTestMust(t, first.Close())
	next, err := New(cfg)
	storageTestMust(t, err)
	defer next.Close()
	if !next.storageReadOnly {
		t.Fatal("uncertain Reserve became healthy/action authority")
	}
	for _, name := range paths {
		raw, err := os.ReadFile(filepath.Join(cfg.DeskConfigDir, name))
		storageTestMust(t, err)
		if sha256.Sum256(raw) != before[name] {
			t.Fatal("uncertain Reserve adopted, reissued or cleared", name)
		}
	}
	if _, err = os.Stat(filepath.Join(cfg.DeskConfigDir, plan.Target)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("uncertain Reserve produced native effect", err)
	}
}

// These checks use independently opened descriptions of the original inode.
// A nonblocking SH attempt must be busy while the original EX description is
// still alive. Reading active/closed flags alone cannot establish that custody.
func exclusiveAssertHeldLock(t *testing.T, original *os.File, root *os.Root, name string) {
	t.Helper()
	if original == nil {
		t.Fatal("missing original EX description", name)
	}
	held, err := original.Stat()
	storageTestMust(t, err)
	probe, err := root.OpenFile(name, os.O_RDONLY|openNoFollow|openNonBlocking, 0)
	storageTestMust(t, err)
	fresh, statErr := probe.Stat()
	if statErr != nil || !os.SameFile(held, fresh) {
		storageTestMust(t, errors.Join(statErr, probe.Close()))
		t.Fatal("probe did not reopen original EX inode", name)
	}
	lockErr := lockPrivateData(probe, false)
	closeErr := probe.Close()
	if !lockHeld(lockErr) {
		t.Fatal("independent SH entered original EX custody", name, lockErr)
	}
	storageTestMust(t, closeErr)
}

func exclusiveAssertOriginalEX(t *testing.T, c *storageBudgetConstruction) {
	t.Helper()
	check := func(original *os.File, root *os.Root, name string) { exclusiveAssertHeldLock(t, original, root, name) }
	if c.startupUse == nil || c.startupWork == nil {
		t.Fatal("outer startup custody disappeared")
	}
	check(c.home.lease, c.original.config, ".home-use.lock")
	check(c.startupUse.gate, c.original.config, storageUseName)
	check(c.startupWork.file, c.original.config, ".data-work.lock")
	domains := 0
	for _, h := range c.startupHolds {
		if h.file == nil {
			continue
		}
		name := h.root.domainName
		if h.root.domainDirectory {
			name = "."
		}
		check(h.file, h.root.domain.root, name)
		domains++
	}
	if domains == 0 {
		t.Fatal("fixture had no independently held root domain")
	}
}

func TestStorageExclusiveCensusPriorObserverRefusedBeforeCurrentness(t *testing.T) {
	requireStorageLine(t)
	for _, point := range []string{"before-exclusive-selection", "before-phase", "allowance-parent-close"} {
		t.Run(point, func(t *testing.T) {
			cfg := ownerActualConfig(t)
			first := ownerActualNew(t, cfg)
			cfg.ProjectDir = first.projectDir
			storageTestMust(t, first.Close())
			_, c, _ := ownerCrashConstruction(t, cfg)
			entered, release := make(chan struct{}), make(chan struct{})
			forbidden := make(chan struct{})
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
			}()
			c.exclusiveFault = &storageExclusiveCensusFault{point: point, entered: entered, release: release, next: &storageExclusiveCensusFault{point: "capacity-before-current", entered: forbidden}}
			loadDone := make(chan error, 1)
			go func() { loadDone <- c.loadOrEnrollOwner(t.Context()) }()
			exclusiveAwait(t, entered, loadDone)
			c.mu.Lock()
			eligible := c.exclusiveCensusEligible
			c.mu.Unlock()
			preSelection := point == "before-exclusive-selection"
			if preSelection {
				if eligible || c.startupUse != nil || c.startupWork != nil || len(c.startupHolds) != 0 {
					t.Fatal("fixture crossed eligibility/hold publication boundary")
				}
			} else if !eligible {
				t.Fatal("healthy route did not seal eligibility before holds publication")
			}
			checkCustody := func() {
				if preSelection {
					exclusiveAssertHeldLock(t, c.home.lease, c.original.config, ".home-use.lock")
				} else {
					exclusiveAssertOriginalEX(t, c)
				}
			}
			o := c.owner
			o.mu.Lock()
			active := o.active
			o.mu.Unlock()
			want := uint64(1)
			if point == "allowance-parent-close" {
				want = 3
			}
			if active != want {
				t.Fatal("unexpected original retain count", active, want)
			}
			checkCustody()
			if pass, err := o.beginStorageCapacityPassWithStartup(t.Context(), nil, c); pass != nil || !errors.Is(err, storagebudget.ErrAuthority) {
				t.Fatal("old startup route reached native currentness", err)
			}
			// Carrying the original Startup mutation through the ordinary overload
			// cannot evade the same eligibility gate.
			if pass, err := o.beginStorageCapacityPass(storageMutationContext(t.Context(), c.mutation), nil); pass != nil || !errors.Is(err, storagebudget.ErrAuthority) {
				t.Fatal("context-only startup observer evaded exclusion", err)
			}
			select {
			case <-forbidden:
				t.Fatal("refused observer entered pre-currentness/native boundary")
			default:
			}
			o.mu.Lock()
			after := o.active
			o.mu.Unlock()
			if after != want {
				t.Fatal("refusal added/dropped an original retain", after, want)
			}
			checkCustody()
			// The final case is already paused with an actual allowance parent
			// descriptor owned pending Close; no old native observer survives beside it.
			close(release)
			select {
			case err := <-loadDone:
				storageTestMust(t, err)
			case <-time.After(10 * time.Second):
				t.Fatal("required healthy exclusive borrower path failed to drain")
			}
			if c.exclusiveCensus == nil || c.exclusiveCensus.stage != storageExclusiveCompleted || c.startupUse != nil || c.startupWork != nil || len(c.startupHolds) != 0 {
				t.Fatal("healthy route disabled or outer cleanup changed")
			}
			o.mu.Lock()
			after = o.active
			o.mu.Unlock()
			if after != 1 {
				t.Fatal("original startup retain changed after healthy census", after)
			}
		})
	}
}

func TestStorageExclusiveCensusGenericObserverCurrentnessFailurePreserved(t *testing.T) {
	// Fresh initial enrollment still uses the original Maintenance route; that
	// same unfinished original construction may borrow its real EX holds.
	cfg := ownerActualConfig(t)
	_, c, _ := ownerCrashConstruction(t, cfg)
	storageTestMust(t, c.loadOrEnrollOwner(t.Context()))
	v, err := c.ref.cell.q.InspectHost(t.Context())
	storageTestMust(t, err)
	_, err = c.ref.cell.q.BeginMaintenance(t.Context(), v.Configuration.Revision)
	storageTestMust(t, err)
	storageTestMust(t, c.acquireStartupHolds(t.Context(), c.ref.cell.ownerCatalog))
	defer func() { storageTestMust(t, c.releaseStartupHolds()) }()
	entered, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	c.exclusiveFault = &storageExclusiveCensusFault{point: "capacity-before-current", entered: entered, release: release}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		pass, err := c.owner.beginStorageCapacityPassWithStartup(ctx, nil, c)
		if pass != nil {
			err = errors.Join(err, pass.Close())
		}
		done <- err
	}()
	exclusiveAwait(t, entered, done)
	c.owner.mu.Lock()
	active := c.owner.active
	c.owner.mu.Unlock()
	if active != 2 {
		t.Fatal("original generic pass retain missing before currentness", active)
	}
	exclusiveAssertOriginalEX(t, c)
	cancel()
	close(release)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("original currentness cancellation lost", err)
		}
	case <-time.After(time.Second):
		t.Fatal("failed generic currentness did not drain")
	}
	c.owner.mu.Lock()
	active = c.owner.active
	c.owner.mu.Unlock()
	if active != 1 {
		t.Fatal("generic failure duplicated/leaked original retain", active)
	}
	exclusiveAssertOriginalEX(t, c)
	// Currentness failure does not permanently disable the original generic
	// Maintenance observer; its own subsequent pass still borrows these holds.
	pass, err := c.owner.beginStorageCapacityPassWithStartup(t.Context(), nil, c)
	storageTestMust(t, err)
	storageTestMust(t, pass.Close())
	exclusiveAssertOriginalEX(t, c)
}
