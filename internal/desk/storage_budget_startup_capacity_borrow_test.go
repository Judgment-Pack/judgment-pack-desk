package desk

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func TestStorageOwnerStartupCapacityOriginalBorrow(t *testing.T) {
	cfg := ownerActualConfig(t)
	if err := os.MkdirAll(filepath.Join(cfg.DeskConfigDir, "retained", "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	raw := []byte("retained original nested bytes")
	path := filepath.Join(cfg.DeskConfigDir, "retained", "nested", "original.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	s := ownerActualNew(t, cfg)
	c := s.storageConstruction
	if !c.owner.admissionOpen() || c.mutation.current(t.Context(), true) == nil || c.handoff.current(t.Context(), false) == nil {
		t.Fatal("successful actual startup failed to consume exclusive authorities")
	}
	if c.startupUse != nil || c.startupWork != nil || len(c.startupHolds) != 0 {
		t.Fatal("completed startup retained census EX holds")
	}
	view, err := c.ref.cell.q.Snapshot(t.Context())
	if err != nil || view.Maintenance {
		t.Fatal("nested census did not complete", err)
	}
	for _, r := range view.Roots {
		if !r.Known {
			t.Fatal("startup enrolled an unmeasured original root")
		}
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatal("startup changed retained bytes", err)
	}
	if p, err := c.owner.beginStorageCapacityPassWithStartup(t.Context(), nil, c); p != nil || !errors.Is(err, storagebudget.ErrAuthority) {
		t.Fatal("finished startup lent released custody", err)
	}
}

func TestStorageOwnerStartupCapacityBorrowAdmissionAndLifetime(t *testing.T) {
	cfg := ownerActualConfig(t)
	if err := os.MkdirAll(filepath.Join(cfg.DeskConfigDir, "retained"), 0700); err != nil {
		t.Fatal(err)
	}
	_, c, _ := ownerCrashConstruction(t, cfg)
	if err := c.loadOrEnrollOwner(t.Context()); err != nil {
		t.Fatal(err)
	}
	// The original construction still owns home EX. Establish the next actual
	// maintenance fence and original holds; never manufacture a lifetime token.
	v, err := c.ref.cell.q.InspectHost(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.ref.cell.q.BeginMaintenance(t.Context(), v.Configuration.Revision); err != nil {
		t.Fatal(err)
	}
	if err := c.acquireStartupHolds(t.Context(), c.ref.cell.ownerCatalog); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := c.releaseStartupHolds(); err != nil {
			t.Error(err)
		}
	}()
	var config *storageRootBinding
	for _, r := range c.owner.orderedRoots() {
		if r.configControl {
			config = r
		}
	}
	if config == nil {
		t.Fatal("missing original config binding")
	}
	child, err := storagePinChild(config.root, "retained", true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := child.root.Close(); err != nil {
			t.Error(err)
		}
	}()
	withoutChain := []storageCapacityEffectParent{{binding: config, root: child.root}}
	if p, err := c.owner.beginStorageCapacityPassWithStartup(t.Context(), withoutChain, c); p != nil || !errors.Is(err, storagebudget.ErrAuthority) {
		t.Fatal("unwitnessed census child admitted", err)
	}
	// Retain every ordinary field, but give the counterfeit its own zero mutex
	// and self pointer. Exact startup pointer sealing must still reject it.
	counterfeit := &storageBudgetConstruction{
		server: c.server, project: c.project, assistant: c.assistant,
		home: c.home, providers: c.providers, selected: c.selected,
		original: c.original, anchor: c.anchor, ref: c.ref, owner: c.owner,
		sources: c.sources, closed: c.closed, closing: c.closing,
		grants: c.grants, spent: c.spent, fault: c.fault,
		startupUse: c.startupUse, startupWork: c.startupWork,
		startupHolds: c.startupHolds, startupCensusEpoch: c.startupCensusEpoch,
	}
	counterfeit.self = counterfeit
	if p, err := c.owner.beginStorageCapacityPassWithStartup(t.Context(), nil, counterfeit); p != nil || !errors.Is(err, storagebudget.ErrAuthority) {
		t.Fatal("counterfeit construction lent original custody", err)
	}
	effects := []storageCapacityEffectParent{{binding: config, root: child.root, witnesses: []storageNamedRoot{child}}}
	p, err := c.owner.beginStorageCapacityPassWithStartup(t.Context(), effects, c)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := p.Close(); err != nil {
			t.Error(err)
		}
	}()
	storageAssertAdmissionCount(t, c.owner, 0) // borrow startup, never re-enter ordinary
	if p.mutation != c.mutation {
		t.Fatal("startup pass did not inherit exact transferred session")
	}
	if _, err := c.owner.rootStorageSample(p.context(t.Context()), config, child.root); err != nil {
		t.Fatal("exact original child observation refused", err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	// Closing a borrowed pass cannot end the outer EX lifetime.
	if use, err := c.ref.acquire(); use != nil || !errors.Is(err, storagebudget.ErrBusy) {
		if use != nil {
			_ = use.Close()
		}
		t.Fatal("pass close released original EX", err)
	}
	late, err := c.owner.beginStorageCapacityPassWithStartup(t.Context(), effects, c)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := late.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := c.releaseStartupHolds(); err != nil {
		t.Fatal(err)
	}
	if err := late.current(t.Context()); !errors.Is(err, storagebudget.ErrAuthority) {
		t.Fatal("pass outlived original borrowed holds", err)
	}
	use, err := c.ref.acquire()
	if err != nil {
		t.Fatal("released startup leaked EX", err)
	}
	if err := use.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestStorageOwnerStartupCapacityFailureCleanup(t *testing.T) {
	cfg := ownerActualConfig(t)
	_, c, _ := ownerCrashConstruction(t, cfg)
	c.fault = &storageBootstrapFault{Name: "anchor/census", Phase: "before-effect"}
	if err := c.loadOrEnrollOwner(t.Context()); err == nil {
		t.Fatal("census publication fault was lost")
	}
	if !c.grants["anchor/census"] {
		t.Fatal("failure happened before census measurement completed")
	}
	if c.startupUse != nil || c.startupWork != nil || len(c.startupHolds) != 0 || c.owner.active != 0 {
		t.Fatal("failed census leaked borrowed pass or startup holds")
	}
	view, err := c.ref.cell.q.InspectHost(t.Context())
	if err != nil || !view.Configuration.Maintenance {
		t.Fatal("failed census withdrew maintenance fence", err)
	}
	for _, slot := range view.Slots {
		if slot.Ticket.State != storagebudget.Empty {
			t.Fatal("failed observation manufactured an operation")
		}
	}
	use, err := c.ref.acquire()
	if err != nil {
		t.Fatal("failed census leaked original EX use hold", err)
	}
	if err := use.Close(); err != nil {
		t.Fatal(err)
	}
}
