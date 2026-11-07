package desk

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Outcomes use handwritten expected E/R arithmetic. P is intentionally cut
// before any new application token or C receipt can explain its boundary.
func TestStoragePolicyNativeQBoundary(t *testing.T) {
	for _, bank := range []byte{0, 1} {
		for _, tc := range []struct {
			name, phase     string
			epoch, revision uint64
		}{{"commit", "active", 3, 16}, {"fenced-abort", "aborted", 3, 15}, {"unfenced-abort", "aborted-unfenced", 2, 13}} {
			t.Run(fmt.Sprintf("bank%d/%s", bank, tc.name), func(t *testing.T) {
				// Match the independently authored project/private graph used by
				// policyNativePlan; the generic advisory graph is not that owner.
				original, catalog, f, _ := policyNativeFixture(t, bank)
				prepared := original[1-bank]
				c, err := storageOwnerV6PackedDecode(prepared, f.raw, f.record, f.claim)
				storageTestMust(t, err)
				inverse, err := storagebudget.DecodePolicyTransition(c.PolicyTransition)
				storageTestMust(t, err)
				if inverse.Original.Epoch != 2 || inverse.Original.Revision != 13 {
					t.Fatal("handwritten initial Q vector changed")
				}
				c.Sequence++
				c.Phase = tc.phase
				copy(c.PreviousCatalogDigest[:], prepared[32:64])
				if tc.phase != "active" {
					c.EffectivePolicy = storageOwnerV6HostPolicy(inverse.Before)
				}
				final, err := storageOwnerV6PackedEncode(c, f.raw, f.record, f.claim)
				storageTestMust(t, err)
				actual := [2][]byte{prepared, prepared}
				actual[bank] = final
				lineage := policyNativePlan(t)
				native, err := storageOwnerV6NativeAppend(actual, bank, f.raw, catalog, lineage, storageCompositionPlan, lineage.Order[0][0], f.record, f.claim)
				storageTestMust(t, err)
				for _, raw := range native {
					v, e := storageOwnerV6NativeDecode(raw, f.record, f.claim)
					storageTestMust(t, e)
					if v.Boundary != (storagePolicyQBoundary{tc.epoch, tc.revision}) || len(v.Stage.Lineage.Rows.LineageCompletions) != 0 || !bytes.Equal(v.Stage.Core, func() []byte {
						x := make([]byte, storageOwnerCatalogBytes)
						source, _, e := storageOwnerDecodePackedSource(c.Source)
						storageTestMust(t, e)
						copy(x, source.Core)
						return x
					}()) {
						t.Fatal("P lost policy boundary, rewrote E1 or invented C")
					}
				}
				v, err := storageOwnerV6NativeDecode(native[bank], f.record, f.claim)
				storageTestMust(t, err)
				prefix, err := storageOwnerV6NativeStagePrefix(v.Stage, v.NativeBase, v.Boundary)
				storageTestMust(t, err)
				if len(prefix) != 256 || binary.LittleEndian.Uint16(prefix[:2]) != 3 || binary.LittleEndian.Uint64(prefix[233:241]) != tc.epoch || binary.LittleEndian.Uint64(prefix[241:249]) != tc.revision || !storageAllZero(prefix[249:]) {
					t.Fatal("fixed independent boundary layout changed")
				}
				projection, err := storagePolicyBeforeNativeProjection(catalog, catalog, native, bank, f.raw, f.record, f.claim)
				storageTestMust(t, err)
				if !storagePolicyProjectionQBoundary(projection, tc.epoch, tc.revision) || storagePolicyProjectionQBoundary(projection, tc.epoch+1, tc.revision+2) || storagePolicyProjectionQBoundary(projection, tc.epoch, tc.revision-1) {
					t.Fatal("historical native boundary used old E1/current outer epoch")
				}
				restored, err := storagePolicyNativeProjectionPair(projection)
				storageTestMust(t, err)
				if !bytes.Equal(restored[0], native[0]) || !bytes.Equal(restored[1], native[1]) {
					t.Fatal("per-bank boundary prefix not retained exactly")
				}
			})
		}
	}
}

func TestStoragePolicyNativeBoundaryWireRefusals(t *testing.T) {
	actual, catalog, f, _ := policyNativeFixture(t, 1)
	l := policyNativePlan(t)
	pair, err := storageOwnerV6NativeAppend(actual, 1, f.raw, catalog, l, storageCompositionPlan, l.Order[0][0], f.record, f.claim)
	storageTestMust(t, err)
	v, err := storageOwnerV6NativeDecode(pair[1], f.record, f.claim)
	storageTestMust(t, err)
	prefix, err := storageOwnerV6NativeStagePrefix(v.Stage, v.NativeBase, v.Boundary)
	storageTestMust(t, err)
	for _, tc := range []struct {
		name string
		at   int
		zero bool
	}{{"old-prefix-version", 0, false}, {"zero-epoch", 233, true}, {"zero-revision", 241, true}, {"reserved", 249, false}} {
		t.Run(tc.name, func(t *testing.T) {
			bad := append([]byte{}, prefix...)
			if tc.zero {
				clear(bad[tc.at : tc.at+8])
			} else if tc.at == 0 {
				binary.LittleEndian.PutUint16(bad[:2], 2)
			} else {
				bad[tc.at] = 1
			}
			if _, _, _, e := storageOwnerV6NativeReadStagePrefix(bad, v.Stage.Sequence, v.Stage.Core, v.Stage.Lineage); e == nil {
				t.Fatal("invalid/old boundary became current native data")
			}
		})
	}
	for _, tc := range []struct {
		name string
		at   int
	}{{"epoch", 233}, {"minimum-revision", 241}} {
		t.Run(tc.name, func(t *testing.T) {
			bad := append([]byte{}, pair[1]...)
			bad[64+520+tc.at] ^= 1
			compositionTestSeal(bad)
			if _, _, e := storageOwnerV6Payload(bad); e != nil {
				t.Fatal("mutation lost outer framing", e)
			}
			if _, e := storageOwnerV6NativeReadPair(pair[0], bad, nil, nil, f.record, f.claim); e == nil {
				t.Fatal("resealed unequal native boundary accepted")
			}
		})
	}
}

func TestStoragePolicyCompletedBoundaryRefusals(t *testing.T) {
	c, source, _, f, _ := policyPackedFixture(t)
	t0, err := storagebudget.DecodePolicyTransition(c.PolicyTransition)
	storageTestMust(t, err)
	_, defs, err := source.definitions()
	storageTestMust(t, err)
	state := storagePolicyOriginal{Catalog: source, Evidence: epDecode(t, f), Effective: defs, Boundary: storagePolicyQBoundary{3, 16}}
	q := storagebudget.EnrollmentInspection{View: storagebudget.HostInspection{Configuration: storagebudget.Configuration{Epoch: 3, Revision: 16, Policy: t0.Before, Filesystems: append([]storagebudget.Filesystem{}, defs.Filesystems...), CapacityConstraints: append([]storagebudget.CapacityConstraint{}, defs.Constraints...)}, Roots: append([]storagebudget.RegisteredRoot{}, defs.Roots...), Workspaces: append([]storagebudget.RegisteredWorkspace{}, defs.Workspaces...)}}
	storageTestMust(t, storagePolicyCompletedQ(state, q))
	for _, tc := range []struct {
		name   string
		change func(*storagebudget.EnrollmentInspection)
	}{
		{"unexplained-epoch", func(v *storagebudget.EnrollmentInspection) { v.View.Configuration.Epoch++ }},
		{"below-revision-floor", func(v *storagebudget.EnrollmentInspection) { v.View.Configuration.Revision-- }},
		{"maintenance", func(v *storagebudget.EnrollmentInspection) { v.View.Configuration.Maintenance = true }},
		{"unapplied-terminal", func(v *storagebudget.EnrollmentInspection) {
			v.View.Slots[0].Ticket.State = storagebudget.Terminal
			v.View.Slots[0].Ticket.Token = storagebudget.Token{ID: storagebudget.ID{9}, Slot: 0, Generation: 1, Sequence: 1}
		}},
		{"different-effective-policy", func(v *storagebudget.EnrollmentInspection) { v.View.Configuration.Policy.Workspace.Total.Bytes-- }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := q
			tc.change(&v)
			if storagePolicyCompletedQ(state, v) == nil {
				t.Fatal("invalid current boundary accepted")
			}
		})
	}
}

func storagePolicyHostRequireRecord(t *testing.T) {
	t.Helper()
	records, err := storageEnrollmentNativeRecords(storageEnrollmentInstalledNativeRecords)
	storageTestMust(t, err)
	if len(records) == 0 {
		t.Skip("requires genuine current-source Record3; no native behavior is synthesized")
	}
}

func TestStoragePolicyHostPrepareAndStickyRestart(t *testing.T) {
	storagePolicyHostRequireRecord(t)
	for _, kind := range []string{"project", "managed-desk"} {
		t.Run(kind, func(t *testing.T) {
			cfg := ownerActualConfig(t)
			if kind == "managed-desk" {
				var registry deskHomeRegistry
				cfg, registry = homeFixture(t)
				cfg.CodexBin = "off"
				cfg.DevMode = true
				cfg.Port = testPort
				entry := &registry.Desks[0]
				entry.Kind = "desk"
				entry.HistoryIdentity = "desk:" + entry.ID
				entry.StorageOwner = entry.ID
				entry.RunnerWorkspace = entry.ID
				raw, e := json.Marshal(registry)
				storageTestMust(t, e)
				privateFixture(t, filepath.Join(cfg.DeskConfigDir, deskHomeRegistryName), string(raw))
			}
			s := ownerActualNew(t, cfg)
			before := ownerReviewDurableState(t, s.storageConstruction.original.config)
			if _, e := s.readStoragePolicySnapshot(t.Context()); e == nil {
				t.Fatal("unprepared first-use became writable")
			}
			if !reflect.DeepEqual(before, ownerReviewDurableState(t, s.storageConstruction.original.config)) {
				t.Fatal("read-only snapshot changed durable state")
			}
			storageTestMust(t, s.prepareStoragePolicy(t.Context()))
			first, e := s.readStoragePolicySnapshot(t.Context())
			storageTestMust(t, e)
			immutable := append([]storageOwnerWorkspace{}, s.storageConstruction.ref.cell.ownerCatalog.Workspaces...)
			var root *storageRootBinding
			for _, r := range s.storageConstruction.owner.roots {
				if r.configControl {
					root = r
				}
			}
			if root == nil {
				t.Fatal("original config binding absent")
			}
			var tokens [2]storagebudget.Token
			for i := 0; i < 2; i++ {
				plan := storageWriterPlan(root)
				plan.Target = fmt.Sprintf("policy-sticky-%d.json", i)
				plan.Stage = fmt.Sprintf(".policy-sticky-%d-stage", i)
				plan.Operation = fmt.Sprintf("policy-sticky-%d", i)
				a, e := s.storageConstruction.owner.admitSingleFile(t.Context(), plan)
				storageTestMust(t, e)
				tokens[i] = a.token
				storageTestMust(t, a.Write(t.Context()))
				storageTestMust(t, a.Close())
			}
			if tokens[0].Slot != tokens[1].Slot || tokens[1].Sequence <= tokens[0].Sequence {
				t.Fatal("actual ordinary Q-slot reuse precondition missing")
			}
			first, e = s.readStoragePolicySnapshot(t.Context())
			storageTestMust(t, e)

			// All snapshot coordinates and values must be the held original's;
			// a matching revision cannot lend authority to a foreign/stale row.
			for _, mutate := range []func(*storagePolicySnapshot){
				func(v *storagePolicySnapshot) { v.Installation[0] ^= 1 },
				func(v *storagePolicySnapshot) { v.Workspace.ID[0] ^= 1 },
				func(v *storagePolicySnapshot) { v.Workspace.Generation++ },
				func(v *storagePolicySnapshot) { v.Workspace.Limits.Total.Bytes-- },
				func(v *storagePolicySnapshot) { v.Policy.Workspace.Total.Bytes-- },
				func(v *storagePolicySnapshot) { v.Sequence++ },
				func(v *storagePolicySnapshot) { v.Epoch++ },
			} {
				bad := first
				mutate(&bad)
				limit := first.Policy.Workspace
				limit.Total.Bytes--
				if _, e = s.updateStoragePolicy(t.Context(), storagePolicyEdit{Expected: bad, Default: &limit}); !errors.Is(e, storagebudget.ErrAuthority) && !errors.Is(e, storagebudget.ErrStale) {
					t.Fatal("foreign/stale snapshot was not definitely refused", e)
				}
			}
			nextDefault := first.Policy.Workspace
			nextDefault.Total.Bytes--
			second, e := s.updateStoragePolicy(t.Context(), storagePolicyEdit{Expected: first, Default: &nextDefault})
			storageTestMust(t, e)
			if second.Workspace.Limits != first.Workspace.Limits || second.Policy.Workspace != nextDefault {
				t.Fatal("future-only default overwrote explicit sticky workspace")
			}
			copyOnce := second.Policy.Workspace
			third, e := s.updateStoragePolicy(t.Context(), storagePolicyEdit{Expected: second, Workspace: &copyOnce})
			storageTestMust(t, e)
			if third.Workspace.Limits != copyOnce {
				t.Fatal("explicit one-time default copy not installed")
			}
			if _, e = s.updateStoragePolicy(t.Context(), storagePolicyEdit{Expected: second, Workspace: &copyOnce}); e == nil {
				t.Fatal("stale policy snapshot was silently rebased")
			}
			if !reflect.DeepEqual(immutable, s.storageConstruction.ref.cell.ownerCatalog.Workspaces) {
				t.Fatal("policy edit mutated immutable admission baselines")
			}
			// Existing charged files survive a reduction below usage. Only a new
			// growth request is refused; policy itself is not a usage eraser.
			charges, e := s.storageConstruction.ref.cell.q.Snapshot(t.Context())
			storageTestMust(t, e)
			charged := false
			for _, row := range charges.Workspaces {
				if row.ID == third.Workspace.ID && row.Known {
					var bytes uint64
					for _, v := range row.Usage {
						bytes += v.Bytes
					}
					charged = bytes > 1
				}
			}
			if !charged {
				t.Fatal("actual over-limit charged-workspace precondition absent")
			}
			low := third.Workspace.Limits
			low.Total.Bytes = 1
			third, e = s.updateStoragePolicy(t.Context(), storagePolicyEdit{Expected: third, Workspace: &low})
			storageTestMust(t, e)
			retained, e := s.storageConstruction.ref.cell.q.Snapshot(t.Context())
			storageTestMust(t, e)
			if !reflect.DeepEqual(charges.Installation, retained.Installation) || !reflect.DeepEqual(charges.Promised, retained.Promised) || !reflect.DeepEqual(charges.Tickets, retained.Tickets) || !reflect.DeepEqual(charges.Roots, retained.Roots) || len(charges.Workspaces) != len(retained.Workspaces) {
				t.Fatal("policy reduction changed existing charges/promises/tickets")
			}
			for i, row := range charges.Workspaces {
				if row.ID != retained.Workspaces[i].ID || row.Generation != retained.Workspaces[i].Generation || row.Known != retained.Workspaces[i].Known || row.Usage != retained.Workspaces[i].Usage {
					t.Fatal("policy reduction erased an existing workspace charge")
				}
			}
			// Limits are warnings (storageLimitsAdvisory): new growth under the
			// reduced workspace limit is admitted, not refused.
			plan := storageWriterPlan(root)
			plan.Target, plan.Stage, plan.Operation = "policy-over-limit.json", ".policy-over-limit-stage", "policy-over-limit"
			a, e := s.storageConstruction.owner.admitSingleFile(t.Context(), plan)
			if e != nil || a == nil {
				t.Fatal("new growth under a reduced, advisory workspace limit was refused", e)
			}
			storageTestMust(t, a.Write(t.Context()))
			storageTestMust(t, a.Close())
			if _, e = root.root.Stat(plan.Target); e != nil {
				t.Fatal("growth admitted over an advisory limit was not written", e)
			}
			storageTestMust(t, s.Close())
			next := ownerActualNew(t, cfg)
			got, e := next.readStoragePolicySnapshot(t.Context())
			storageTestMust(t, e)
			if got.Policy != third.Policy || got.Workspace != third.Workspace || got.Epoch != third.Epoch || !reflect.DeepEqual(immutable, next.storageConstruction.ref.cell.ownerCatalog.Workspaces) {
				t.Fatal("restart lost current policy or sticky/source identity")
			}
		})
	}
}

func TestStoragePolicyHostNativePBeforeFirstReserveRestart(t *testing.T) {
	storagePolicyHostRequireRecord(t)
	cfg := ownerActualConfig(t)
	s, err := New(cfg)
	storageTestMust(t, err)
	t.Cleanup(func() {
		if s != nil {
			storageTestMust(t, s.Close())
		}
	})
	if s.storageReadOnly || s.storageEnrollment == nil {
		t.Fatal("actual startup not ready", s.storageProblem)
	}
	storageTestMust(t, s.prepareStoragePolicy(t.Context()))
	// Genuine original local/private admission establishes the parent without
	// publishing an identity P or fabricating a project/source/private identity.
	storageTestMust(t, s.storageConstruction.completeOriginalLocalCreation(t.Context()))
	snapshot, err := s.readStoragePolicySnapshot(t.Context())
	storageTestMust(t, err)
	limits := snapshot.Policy.Workspace
	limits.Total.Bytes--
	edited, err := s.updateStoragePolicy(t.Context(), storagePolicyEdit{Expected: snapshot, Default: &limits})
	storageTestMust(t, err)
	c := s.storageConstruction
	before, err := c.ref.cell.q.InspectAction(t.Context())
	storageTestMust(t, err)
	cut := errors.New("handwritten actual P before first Reserve cut")
	entered := make(chan struct{})
	c.enrollmentFault = &storageEnrollmentFault{point: "identity-catalog/lin4/plan-active/persist", entered: entered, err: cut}
	if err = c.completeOriginalIdentity(t.Context()); !errors.Is(err, cut) {
		t.Fatal("actual identity did not publish P before first Reserve", err)
	}
	select {
	case <-entered:
	default:
		t.Fatal("required original P cut not reached")
	}
	a, err := os.ReadFile(filepath.Join(cfg.DeskConfigDir, "storage-owner.a"))
	storageTestMust(t, err)
	b, err := os.ReadFile(filepath.Join(cfg.DeskConfigDir, "storage-owner.b"))
	storageTestMust(t, err)
	pair, err := storageOwnerV6NativeReadPair(a, b, nil, nil, *c.ref.cell.ownerRecord, c.ref.cell.ownerClaim)
	storageTestMust(t, err)
	p := pair.Banks[pair.Selected]
	if pair.RecoveryOnly || p.Stage.Transition != storageCompositionPlan || len(p.Stage.Lineage.Rows.LineagePlans) != 1 || len(p.Stage.Lineage.Rows.LineageCompletions) != 0 || p.Boundary != (storagePolicyQBoundary{edited.Epoch, edited.Revision}) {
		t.Fatal("actual P lost tuple or invented C")
	}
	initial := append([]byte{}, p.Stage.Lineage.Rows.LineagePlans[0].IdentityInitial...)
	planID := p.Stage.Lineage.Rows.LineagePlans[0].PlanID
	after, err := c.ref.cell.q.InspectAction(t.Context())
	storageTestMust(t, err)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("P cut changed Q before first application admission")
	}
	closeErr := s.Close()
	s = nil
	if closeErr != nil && !errors.Is(closeErr, cut) {
		t.Fatal("original failed owner did not report its checked Close", closeErr)
	}
	s, err = New(cfg)
	storageTestMust(t, err)
	if s.storageReadOnly || s.storageEnrollment == nil {
		t.Fatal("actual P restart did not recover original first admission", s.storageProblem)
	}
	state, err := s.storageConstruction.owner.readOriginalPolicy(t.Context())
	storageTestMust(t, err)
	if !state.Boundary.valid() || state.Boundary.Epoch != edited.Epoch || state.Boundary.MinimumRevision != edited.Revision {
		t.Fatal("restart synthesized epoch from old E1 or current Q")
	}
	found := false
	for _, row := range state.Lineage.Rows.LineageCompletions {
		if row.PlanID == planID {
			found = true
		}
	}
	if !found || len(state.Lineage.Rows.LineagePlans) != 1 || !bytes.Equal(state.Lineage.Rows.LineagePlans[0].IdentityInitial, initial) {
		t.Fatal("restart redrew P or failed actual Reserve/native/C completion")
	}
	// Policy -> actual P/first Reserve/C -> a further policy commit, followed
	// by an unfenced abort, retains the independent current tuple and P bytes.
	current, err := s.readStoragePolicySnapshot(t.Context())
	storageTestMust(t, err)
	nextDefault := current.Policy.Workspace
	nextDefault.Total.Bytes--
	current, err = s.updateStoragePolicy(t.Context(), storagePolicyEdit{Expected: current, Default: &nextDefault})
	storageTestMust(t, err)
	aborted := errors.New("handwritten native-source next edit no-fence cut")
	s.storageConstruction.enrollmentFault = &storageEnrollmentFault{point: "policy-begin-before", err: aborted}
	nextDefault.Total.Bytes--
	if _, err = s.updateStoragePolicy(t.Context(), storagePolicyEdit{Expected: current, Default: &nextDefault}); !errors.Is(err, aborted) {
		t.Fatal("native-source second edit did not preserve no-fence failure", err)
	}
	closeErr = s.Close()
	s = nil
	if closeErr != nil && !errors.Is(closeErr, aborted) {
		t.Fatal("native-source aborted owner Close changed cause", closeErr)
	}
	s, err = New(cfg)
	storageTestMust(t, err)
	state, err = s.storageConstruction.owner.readOriginalPolicy(t.Context())
	storageTestMust(t, err)
	if state.Packed == nil || state.Packed.Phase != "aborted-unfenced" || state.Boundary != (storagePolicyQBoundary{current.Epoch, current.Revision}) || len(state.Lineage.Rows.LineagePlans) != 1 || !bytes.Equal(state.Lineage.Rows.LineagePlans[0].IdentityInitial, initial) {
		t.Fatal("native-source abort/restart lost original current tuple or plan")
	}
	current, err = s.readStoragePolicySnapshot(t.Context())
	storageTestMust(t, err)
	_, err = s.updateStoragePolicy(t.Context(), storagePolicyEdit{Expected: current, Default: &nextDefault})
	storageTestMust(t, err)
}

func TestStoragePolicyHostInterruptedGroups(t *testing.T) {
	storagePolicyHostRequireRecord(t)
	for _, tc := range []struct {
		name, point, phase string
		committed          bool
	}{{"prepared", "policy-prepared-sync", "aborted-unfenced", false}, {"begin", "policy-apply-before", "aborted", false}, {"apply", "policy-final-sync", "active", true}, {"final", "policy-finish-before", "active", true}, {"before-fence", "policy-begin-before", "aborted-unfenced", false}} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := ownerActualConfig(t)
			s, err := New(cfg)
			storageTestMust(t, err)
			t.Cleanup(func() {
				if s != nil {
					storageTestMust(t, s.Close())
				}
			})
			storageTestMust(t, s.prepareStoragePolicy(t.Context()))
			first, err := s.readStoragePolicySnapshot(t.Context())
			storageTestMust(t, err)
			limits := first.Policy.Workspace
			limits.Total.Bytes--
			cut := errors.New("handwritten original policy group interruption")
			entered := make(chan struct{})
			s.storageConstruction.enrollmentFault = &storageEnrollmentFault{point: tc.point, entered: entered, err: cut}
			if _, err = s.updateStoragePolicy(t.Context(), storagePolicyEdit{Expected: first, Default: &limits}); !errors.Is(err, cut) {
				t.Fatal("known consumed group cut not preserved", err)
			}
			select {
			case <-entered:
			default:
				t.Fatal("selected original group not reached")
			}
			closeErr := s.Close()
			s = nil
			if closeErr != nil && !errors.Is(closeErr, cut) {
				t.Fatal("failed original Close changed error", closeErr)
			}
			s, err = New(cfg)
			storageTestMust(t, err)
			if s.storageReadOnly {
				t.Fatal("original remaining groups did not reconcile", s.storageProblem)
			}
			got, err := s.readStoragePolicySnapshot(t.Context())
			storageTestMust(t, err)
			if tc.committed && got.Policy.Workspace != limits || !tc.committed && got.Policy != first.Policy || got.Workspace != first.Workspace {
				t.Fatal("restart chose wrong original policy outcome")
			}
			state, err := s.storageConstruction.owner.readOriginalPolicy(t.Context())
			storageTestMust(t, err)
			if state.Packed == nil || state.Packed.Phase != tc.phase {
				t.Fatal("fenced/unfenced distinction was lost")
			}
			epoch, floor := first.Epoch, first.Revision
			if tc.phase == "active" {
				epoch++
				floor += 3
			} else if tc.phase == "aborted" {
				epoch++
				floor += 2
			}
			if state.Boundary != (storagePolicyQBoundary{epoch, floor}) || got.Epoch != epoch {
				t.Fatal("wrong independently derived outcome tuple")
			}
			// A further edit must compare this recovered source; it cannot inherit a
			// stale original revision or erase the selected abort distinction.
			again := got.Policy.Workspace
			again.Total.Bytes--
			next, err := s.updateStoragePolicy(t.Context(), storagePolicyEdit{Expected: got, Default: &again})
			storageTestMust(t, err)
			if next.Epoch != epoch+1 || next.Policy.Workspace != again {
				t.Fatal("next edit did not follow exact recovered current boundary")
			}
		})
	}
}

func TestStoragePolicyHostCancellationDrain(t *testing.T) {
	storagePolicyHostRequireRecord(t)
	for _, kind := range []string{"caller-cancel", "server-close"} {
		t.Run(kind, func(t *testing.T) {
			cfg := ownerActualConfig(t)
			s, err := New(cfg)
			storageTestMust(t, err)
			t.Cleanup(func() {
				if s != nil {
					storageTestMust(t, s.Close())
				}
			})
			storageTestMust(t, s.prepareStoragePolicy(t.Context()))
			first, err := s.readStoragePolicySnapshot(t.Context())
			storageTestMust(t, err)
			entered, release := make(chan struct{}), make(chan struct{})
			s.storageConstruction.enrollmentFault = &storageEnrollmentFault{point: "policy-prepared-sync", entered: entered, release: release}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			limits := first.Policy.Workspace
			limits.Total.Bytes--
			result := make(chan error, 1)
			go func() {
				_, e := s.updateStoragePolicy(ctx, storagePolicyEdit{Expected: first, Default: &limits})
				result <- e
			}()
			<-entered
			if kind == "caller-cancel" {
				cancel()
				select {
				case <-result:
					t.Fatal("original invocation returned while native boundary still owned")
				default:
				}
				close(release)
				if err = <-result; !errors.Is(err, context.Canceled) {
					t.Fatal("caller cancellation lost original error", err)
				}
				closeErr := s.Close()
				s = nil
				if closeErr != nil && !errors.Is(closeErr, context.Canceled) {
					t.Fatal("checked canceled Close changed cause", closeErr)
				}
			} else {
				closed := make(chan error, 1)
				owner := s.storageConstruction.owner
				go func() { closed <- s.Close() }()
				<-owner.closingStarted
				select {
				case <-closed:
					t.Fatal("Server.Close returned before original native drain")
				default:
				}
				close(release)
				if err = <-result; err == nil {
					t.Fatal("Closing policy invocation claimed success")
				}
				closeErr := <-closed
				s = nil
				if closeErr != nil && !errors.Is(closeErr, context.Canceled) && !errors.Is(closeErr, os.ErrClosed) {
					t.Fatal("checked original Close changed cancellation cause", closeErr)
				}
			}
		})
	}
}

// Each step is distinct real ancestry work, never a retry of an unknown
// operation. The bounded chain must establish both actual path2 and path3,
// including interrupted continuations; missing coverage fails this group.
func TestStoragePolicyHostPostPolicyR1Restart(t *testing.T) {
	storagePolicyHostRequireRecord(t)
	cfg := ownerActualConfig(t)
	cfg.JpackBin = standInRuntime(t)
	base := t.TempDir()
	configHome, parent := filepath.Join(base, "configuration"), filepath.Join(base, "public-original-parent")
	storageTestMust(t, os.Mkdir(configHome, 0700))
	storageTestMust(t, os.Mkdir(parent, 0755))
	storageTestMust(t, os.Chmod(parent, 0755))
	missing := parent
	for i := 0; i < 12; i++ {
		missing = filepath.Join(missing, fmt.Sprintf("missing-%d", i))
	}
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_DATA_HOME", missing)
	cfg.DeskConfigDir = ""
	s, err := New(cfg)
	storageTestMust(t, err)
	t.Cleanup(func() {
		if s != nil {
			storageTestMust(t, s.Close())
		}
	})
	ready := func(t *testing.T) *storageBudgetConstruction {
		t.Helper()
		if s.storageReadOnly || s.storageConstruction == nil || s.storageConstruction.owner == nil || s.storageEnrollment == nil {
			t.Fatal("actual original host lost startup readiness", s.storageProblem)
		}
		return s.storageConstruction
	}
	c := ready(t)
	storageTestMust(t, s.prepareStoragePolicy(t.Context()))
	// Complete genuine project/private identity first, so restart cannot add
	// unrelated first-use identity work while this test compares R1 bytes.
	storageTestMust(t, c.completeOriginalLocalCreation(t.Context()))
	storageTestMust(t, c.completeOriginalIdentity(t.Context()))
	parentInfo, err := os.Stat(parent)
	storageTestMust(t, err)
	seen, interrupted := map[byte]bool{}, map[byte]bool{}
	steps := []struct {
		name, point string
		qstage      int
	}{
		{"step0-complete", "", 3},
		{"step1-fence", "successor-two-fence-family2", 0},
		{"step2-prepared", "successor-first-prepared-family2", 0},
		{"step3-q1", "successor-q1-family2", 1},
		{"step4-q2", "successor-q2-family2", 2},
		{"step5-q3", "successor-q3-family2", 3},
	}
	for _, step := range steps {
		if !t.Run(step.name, func(t *testing.T) {
			c = ready(t)
			snapshot, e := s.readStoragePolicySnapshot(t.Context())
			storageTestMust(t, e)
			nextDefault := snapshot.Policy.Workspace
			nextDefault.Total.Bytes--
			policy, e := s.updateStoragePolicy(t.Context(), storagePolicyEdit{Expected: snapshot, Default: &nextDefault})
			storageTestMust(t, e)
			prior := c.ref.cell.enrollmentEndpoint
			if prior == nil {
				t.Fatal("genuine prior E1 absent")
			}
			entered := make(chan struct{})
			cut := errors.New("handwritten original post-policy R1 cut")
			if step.point != "" {
				c.enrollmentFault = &storageEnrollmentFault{point: step.point, entered: entered, err: cut}
			}
			complete, callErr := c.enrollNextHistoryAncestor(t.Context(), s)
			if complete {
				t.Fatal("bounded missing ancestry unexpectedly exhausted")
			}
			if step.point == "" {
				storageTestMust(t, callErr)
			} else {
				if !errors.Is(callErr, cut) {
					t.Fatal("post-policy original R1 missed exact cut", callErr)
				}
				select {
				case <-entered:
				default:
					t.Fatal("original publication/Q cut not reached")
				}
			}
			read := func() (storageOwnerV6NativePair, [2][]byte, storageEnrollmentPeakEnvelope) {
				t.Helper()
				c := ready(t)
				var raw [2][]byte
				for i, name := range []string{"storage-owner.a", "storage-owner.b"} {
					var e error
					raw[i], e = os.ReadFile(filepath.Join(s.configDir, name))
					storageTestMust(t, e)
				}
				pair, e := storageOwnerV6NativeReadPair(raw[0], raw[1], &prior.Inner.Catalog, prior.Raw, *c.ref.cell.ownerRecord, c.ref.cell.ownerClaim)
				storageTestMust(t, e)
				outer, e := storageEnrollmentPeakDecodeEnvelopeNative(pair.Banks[pair.Selected].Stage.Core, *c.ref.cell.ownerRecord, c.ref.cell.ownerClaim, true, true, true)
				storageTestMust(t, e)
				return pair, raw, outer
			}
			// A failed epoch is intentionally not ordinary-ready; bounded reads
			// below need original descriptors, not a fresh mutation permission.
			var physical [2][]byte
			for i, name := range []string{"storage-owner.a", "storage-owner.b"} {
				physical[i], e = os.ReadFile(filepath.Join(s.configDir, name))
				storageTestMust(t, e)
			}
			pair, e := storageOwnerV6NativeReadPair(physical[0], physical[1], &prior.Inner.Catalog, prior.Raw, *c.ref.cell.ownerRecord, c.ref.cell.ownerClaim)
			storageTestMust(t, e)
			outer, e := storageEnrollmentPeakDecodeEnvelopeNative(pair.Banks[pair.Selected].Stage.Core, *c.ref.cell.ownerRecord, c.ref.cell.ownerClaim, true, true, true)
			storageTestMust(t, e)
			path := outer.Scope.path()
			if path != 2 && path != 3 {
				t.Fatal("actual R1 selected another path", path)
			}
			seen[path] = true
			if step.point != "" {
				interrupted[path] = true
			}
			if outer.Snapshot != nil {
				if outer.Snapshot.Encoding != 6 {
					t.Fatal("real Version3 Fence lost native Snapshot6")
				}
				if _, e = storageEnrollmentPeakEncodeEnvelope(outer, *c.ref.cell.ownerRecord, c.ref.cell.ownerClaim); !errors.Is(e, storagebudget.ErrBounds) {
					t.Fatal("legacy encoder accepted native Fence", e)
				}
				if _, e = storageEnrollmentPeakDecodeEnvelope(pair.Banks[pair.Selected].Stage.Core, *c.ref.cell.ownerRecord, c.ref.cell.ownerClaim, true); !errors.Is(e, storagebudget.ErrBounds) {
					t.Fatal("legacy reader accepted native Fence", e)
				}
			}
			if outer.Marker == nil || outer.Marker.Phase != 3 {
				if pair.Banks[pair.Selected].Boundary != (storagePolicyQBoundary{policy.Epoch, policy.Revision}) {
					t.Fatal("pending native cut rebound before genuine completion")
				}
			}
			qCut, e := c.ref.cell.q.InspectAction(t.Context())
			storageTestMust(t, e)
			if qCut.View.Configuration.Epoch != policy.Epoch+uint64(min(step.qstage, 1)) || qCut.View.Configuration.Maintenance != (step.qstage == 1 || step.qstage == 2) {
				t.Fatal("actual Q interruption stage differs from the original selected suffix")
			}
			hosts, e := os.ReadFile(filepath.Join(s.configDir, storageIntentName))
			storageTestMust(t, e)
			if step.point != "" {
				closeErr := s.Close()
				s = nil
				if closeErr != nil && !errors.Is(closeErr, cut) {
					t.Fatal("checked interrupted Close changed cause", closeErr)
				}
				s, e = New(cfg)
				storageTestMust(t, e)
				c = ready(t)
			}
			completed := c.ref.cell.enrollmentEndpoint
			if completed == nil || completed.Version != 3 || completed.Phase != 2 || completed.Scope != outer.Scope || completed.BeforeEpoch != policy.Epoch || completed.BeforeRevision < policy.Revision || completed.AfterEpoch != policy.Epoch+1 || completed.AfterRevision != completed.BeforeRevision+3 || !storageEnrollmentPeakRetainsOrigins(*completed, *prior) {
				t.Fatal("full genuine Version3 completion changed original Q0/Q3, scope or origins")
			}
			finalPair, finalRaw, final := read()
			if finalPair.RecoveryOnly || !finalPair.Duplicated || !bytes.Equal(finalRaw[0], finalRaw[1]) || final.Marker == nil || final.Marker.Phase != 3 || storageEnrollmentPeakJoinMarker(final, *completed, completed.Ready) != nil {
				t.Fatal("R1 final selector/duplicate lost actual whole bytes or full evidence join")
			}
			state, e := c.owner.readOriginalPolicy(t.Context())
			storageTestMust(t, e)
			want := storagePolicyQBoundary{policy.Epoch + 1, completed.BeforeRevision + 3}
			if state.Boundary != want || state.Policy.Workspace != nextDefault {
				t.Fatal("completed native boundary/effective default mismatch")
			}
			found := false
			for _, projection := range completed.Projections {
				if storageEnrollmentPeakProjectionFullDigest(projection) == completed.Before {
					found = storagePolicyProjectionQBoundary(projection, policy.Epoch, policy.Revision)
				}
			}
			if !found {
				t.Fatal("historical before retained a substituted current boundary")
			}
			qAfter, e := c.ref.cell.q.InspectAction(t.Context())
			storageTestMust(t, e)
			storageTestMust(t, storagePolicyCompletedQ(state, qAfter))
			for _, bad := range []struct{ epoch, revision uint64 }{{policy.Epoch, want.MinimumRevision}, {want.Epoch, want.MinimumRevision - 1}} {
				changed := qAfter
				changed.View.Configuration.Epoch, changed.View.Configuration.Revision = bad.epoch, bad.revision
				if storagePolicyCompletedQ(state, changed) == nil {
					t.Fatal("full new-Q join accepted wrong epoch/revision")
				}
			}
			if !bytes.Equal(qCut.Tickets[0], qAfter.Tickets[0]) || !bytes.Equal(qCut.Tickets[1], qAfter.Tickets[1]) {
				t.Fatal("R1 recovery replayed an application ticket")
			}
			currentHosts, e := os.ReadFile(filepath.Join(s.configDir, storageIntentName))
			storageTestMust(t, e)
			if !bytes.Equal(hosts, currentHosts) {
				t.Fatal("R1 recovery rewrote original creation/identity J")
			}
			parentAfter, e := os.Stat(parent)
			storageTestMust(t, e)
			if !os.SameFile(parentInfo, parentAfter) {
				t.Fatal("R1 exchanged the original public ancestor")
			}
			// A separate completed startup must retain the same endpoint, whole
			// native duplicates and current Q without dispatching the suffix again.
			storageTestMust(t, s.Close())
			s = nil
			s, e = New(cfg)
			storageTestMust(t, e)
			c = ready(t)
			if !bytes.Equal(c.ref.cell.enrollmentEndpoint.Raw, completed.Raw) {
				t.Fatal("completed restart selected another endpoint")
			}
			_, restartedRaw, _ := read()
			qRestarted, e := c.ref.cell.q.InspectAction(t.Context())
			storageTestMust(t, e)
			if !bytes.Equal(restartedRaw[0], finalRaw[0]) || !bytes.Equal(restartedRaw[1], finalRaw[1]) || !reflect.DeepEqual(qAfter, qRestarted) {
				t.Fatal("completed restart replayed native publication/Q")
			}
		}) {
			return
		}
	}
	if !seen[2] || !seen[3] || !interrupted[2] || !interrupted[3] {
		t.Fatal("bounded real post-policy fixture must execute and recover both R1 paths", seen, interrupted)
	}
}

// The former positive fixture combined an advisory workspace with a project
// identity plan. Keep that exact mismatched custody relation as a refusal.
func TestStoragePolicyNativeQBoundaryForeignPlanRefusal(t *testing.T) {
	prepared, catalog, praw, f, _ := policyPackedFixture(t)
	final := prepared
	final.Sequence++
	final.Phase = "active"
	copy(final.PreviousCatalogDigest[:], praw[32:64])
	active, err := storageOwnerV6PackedEncode(final, f.raw, f.record, f.claim)
	storageTestMust(t, err)
	plan := policyNativePlan(t)
	if plan.validateGraph(catalog, f.record) == nil {
		t.Fatal("foreign project/advisory graph precondition disappeared")
	}
	if _, err = storageOwnerV6NativeAppend([2][]byte{praw, active}, 1, f.raw, catalog, plan, storageCompositionPlan, plan.Order[0][0], f.record, f.claim); !errors.Is(err, storagebudget.ErrBounds) {
		t.Fatal("foreign project plan acquired advisory workspace ownership", err)
	}
}
