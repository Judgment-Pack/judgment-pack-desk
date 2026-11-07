package desk

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func TestStorageEnrollmentInitialCaptureOriginalState(t *testing.T) {
	s := ownerActualNew(t, ownerActualConfig(t))
	c := s.storageConstruction
	before, err := c.ref.cell.q.InspectEnrollment(t.Context())
	storageTestMust(t, err)
	p, err := c.beginEnrollmentEpoch(t.Context())
	storageTestMust(t, err)
	if err = p.captureInitialOriginal(t.Context()); !errors.Is(err, storagebudget.ErrAuthority) {
		t.Fatal("capture ran before complete original holds", err)
	}
	storageTestMust(t, p.acquire(t.Context()))
	storageTestMust(t, p.captureInitialOriginal(t.Context()))
	if p.original == nil || p.scope != nil || p.reservation.phase != nil || p.qualified != nil {
		t.Fatal("read-only capture issued a scope or native capability")
	}
	if !reflect.DeepEqual(p.original.Checkpoints, before.Checkpoints) || !reflect.DeepEqual(p.original.Tickets, before.Tickets) {
		t.Fatal("capture omitted or changed the actual raw Q banks")
	}
	ready, err := os.ReadFile(filepath.Join(s.configDir, storageReadyName))
	storageTestMust(t, err)
	if p.original.Ready != sha256.Sum256(ready) {
		t.Fatal("ready digest did not bind complete actual original bytes")
	}
	for bank, name := range []string{"storage-owner.a", "storage-owner.b"} {
		raw, err := os.ReadFile(filepath.Join(s.configDir, name))
		storageTestMust(t, err)
		if !bytes.Equal(p.original.Catalogs[bank], raw) {
			t.Fatal("actual catalog peer lost")
		}
	}
	hosts, err := os.ReadFile(filepath.Join(s.configDir, storageIntentName))
	storageTestMust(t, err)
	for slot, pages := range p.original.Hosts {
		for bank, page := range pages {
			start := (slot*2 + bank) * 16384
			if !bytes.Equal(page, hosts[start:start+16384]) {
				t.Fatal("actual host page lost", slot, bank)
			}
		}
	}
	for i := 0; i < 4; i++ {
		if p.original.Targets[i] != (storageOwnerPersistentObject{}) || p.original.Extents[i] != 0 {
			t.Fatal("capture fabricated a new object's native identity")
		}
	}
	for i := 4; i < 8; i++ {
		if p.original.Targets[i] != p.targets[i].native || !p.targets[i].native.valid(2) || p.targets[i].named.file == nil {
			t.Fatal("actual original target custody missing", i)
		}
	}
	if err = p.captureInitialOriginal(t.Context()); !errors.Is(err, storagebudget.ErrAuthority) {
		t.Fatal("capture overwrote an existing invocation snapshot", err)
	}
	storageTestMust(t, p.finish(nil))
	after, err := c.ref.cell.q.InspectEnrollment(t.Context())
	storageTestMust(t, err)
	if !reflect.DeepEqual(before, after) || !c.owner.admissionOpen() {
		t.Fatal("read-only capture changed Q or failed to restore ordinary admission")
	}
}

func TestStorageEnrollmentInitialCapturePreservesPartialNamespace(t *testing.T) {
	s := ownerActualNew(t, ownerActualConfig(t))
	c := s.storageConstruction
	const name = "storage-enrollment.b"
	const retained = "unresolved original capture fixture"
	privateFixture(t, filepath.Join(s.configDir, name), retained)
	before, err := c.ref.cell.q.InspectEnrollment(t.Context())
	storageTestMust(t, err)
	p, err := c.beginEnrollmentEpoch(t.Context())
	storageTestMust(t, err)
	storageTestMust(t, p.acquire(t.Context()))
	err = p.captureInitialOriginal(t.Context())
	if !errors.Is(err, errStorageEnrollmentUnresolved) || p.original != nil || p.scope != nil {
		t.Fatal("partial namespace was adopted as a new original attempt", err)
	}
	if finish := p.finish(err); !errors.Is(finish, errStorageEnrollmentUnresolved) {
		t.Fatal("partial namespace refusal lost", finish)
	}
	raw, err := os.ReadFile(filepath.Join(s.configDir, name))
	storageTestMust(t, err)
	if string(raw) != retained {
		t.Fatal("uncertain native object changed")
	}
	for _, absent := range []string{"storage-enrollment.a", "storage-enrollment-step.a", "storage-enrollment-step.b"} {
		if _, err = os.Lstat(filepath.Join(s.configDir, absent)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("capture created another namespace entry", absent, err)
		}
	}
	after, err := c.ref.cell.q.InspectEnrollment(t.Context())
	storageTestMust(t, err)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("namespace refusal changed Q")
	}
}

func TestStorageEnrollmentInitialRoomHandwritten(t *testing.T) {
	// Reuse the independently handwritten codec material, never recorded
	// output. These values are comparison data and cannot install qualification.
	f := epEvidence(t, 0)
	s := f.scope
	s[321] = 0
	clear(s[1080:1220])
	q := *f.q[0]
	for i := range q.Roots {
		if q.Roots[i].ID != (storageEnrollmentPeakQID{}) {
			q.Roots[i].Known = true
		}
	}
	for i := range q.Workspaces {
		if q.Workspaces[i].ID != (storageEnrollmentPeakQID{}) {
			q.Workspaces[i].Known = true
		}
	}
	now := time.Unix(1800000000, 0)
	observations := make([]storagebudget.CapacityObservation, int(s[326]))
	for i := range observations {
		raw := s[440+i*80 : 440+(i+1)*80]
		copy(observations[i].Constraint[:], raw[:16])
		observations[i].Generation = storageEnrollmentPeakU64(raw[16:24])
		observations[i].At = now
		for _, row := range q.CapacityConstraints {
			if storagebudget.ID(row.ID) == observations[i].Constraint {
				observations[i].KnownResources = row.Resources
				if row.Resources&storagebudget.CapacityBytes != 0 {
					observations[i].Available.Bytes = row.Headroom.Bytes + storageEnrollmentPeakU64(raw[32:40])
				}
				if row.Resources&storagebudget.CapacityInodes != 0 {
					observations[i].Available.Inodes = row.Headroom.Inodes + storageEnrollmentPeakU64(raw[40:48])
				}
			}
		}
	}
	storageTestMust(t, storageEnrollmentInitialRoom(s, &q, observations, now))
	for _, mutation := range []string{"missing", "duplicate", "generation", "unknown", "stale", "future", "bytes", "inodes", "root-unknown", "workspace-unknown", "maintenance", "logical-limit"} {
		t.Run(mutation, func(t *testing.T) {
			badQ := q
			bad := append([]storagebudget.CapacityObservation{}, observations...)
			switch mutation {
			case "missing":
				bad = nil
			case "duplicate":
				bad = append(bad, bad[0])
			case "generation":
				bad[0].Generation++
			case "unknown":
				bad[0].KnownResources = 0
			case "stale":
				bad[0].At = now.Add(-q.Policy.ObservationMaxAge - time.Nanosecond)
			case "future":
				bad[0].At = now.Add(time.Nanosecond)
			case "bytes":
				bad[0].Available.Bytes--
			case "inodes":
				bad[0].Available.Inodes--
			case "root-unknown":
				badQ.Roots[0].Known = false
			case "workspace-unknown":
				badQ.Workspaces[0].Known = false
			case "maintenance":
				badQ.Maintenance = true
			case "logical-limit":
				badQ.Installation[storagebudget.Control] = badQ.Policy.Installation.Category[storagebudget.Control]
				badQ.Roots[0].Usage[storagebudget.Control] = badQ.Installation[storagebudget.Control]
			}
			if err := storageEnrollmentInitialRoom(s, &badQ, bad, now); err == nil {
				t.Fatal("inadmissible initial scope accepted")
			}
		})
	}
}

func TestStorageEnrollmentInitialScopeCaptureOnly(t *testing.T) {
	_, record, _ := ownerAdvisoryFixture(false)
	before := epScope(1, false, record, [2]uint64{}, [2]uint64{262144, 327680})
	targets := [4]storageOwnerPersistentObject{epNative(401, 2), epNative(402, 2), epNative(403, 2), epNative(404, 2)}
	got, err := storageEnrollmentCaptureScope(before, targets)
	storageTestMust(t, err)
	want := before
	want[321] = 15
	for i, target := range targets {
		epAtNative(want[1080+i*35:], target)
	}
	if got != want || !bytes.Equal(got[184:216], before[184:216]) {
		t.Fatal("capture changed immutable Scope policy or another binding")
	}
	for _, mutation := range []string{"absent", "duplicate", "directory", "existing-catalog-alias", "already-captured"} {
		t.Run(mutation, func(t *testing.T) {
			badBefore, badTargets := before, targets
			switch mutation {
			case "absent":
				badTargets[0] = storageOwnerPersistentObject{}
			case "duplicate":
				badTargets[1] = badTargets[0]
			case "directory":
				badTargets[0] = epNative(401, 1)
			case "existing-catalog-alias":
				badTargets[0] = record.CatalogA
			case "already-captured":
				badBefore = got
			}
			if _, err := storageEnrollmentCaptureScope(badBefore, badTargets); err == nil {
				t.Fatal("invalid native capture accepted")
			}
		})
	}
}

func TestStorageEnrollmentEstimateObservedOverheadDoesNotRefundOrRaiseLimits(t *testing.T) {
	f := epEvidence(t, 0)
	s := f.scope
	epSet16(s[:2], 3)
	s[2], s[321], s[1640], s[1704] = 2, 0, 1, 1
	clear(s[1080:1220])
	normal := s
	clear(normal[184:216])
	d := epHash("desk-enrollment-control-capacity-estimate-v1", normal[:])
	copy(s[184:216], d[:])
	q := *f.q[0]
	for i := range q.Roots {
		if q.Roots[i].ID != (storageEnrollmentPeakQID{}) {
			q.Roots[i].Known = true
		}
	}
	for i := range q.Workspaces {
		if q.Workspaces[i].ID != (storageEnrollmentPeakQID{}) {
			q.Workspaces[i].Known = true
		}
	}
	now := time.Unix(1800000000, 0)
	observations := make([]storagebudget.CapacityObservation, int(s[326]))
	for i := range observations {
		raw := s[440+i*80 : 440+(i+1)*80]
		copy(observations[i].Constraint[:], raw[:16])
		observations[i].Generation = storageEnrollmentPeakU64(raw[16:24])
		observations[i].At = now
		for _, row := range q.CapacityConstraints {
			if storagebudget.ID(row.ID) == observations[i].Constraint {
				observations[i].KnownResources = row.Resources
				if row.Resources&storagebudget.CapacityBytes != 0 {
					observations[i].Available.Bytes = row.Headroom.Bytes + storageEnrollmentPeakU64(raw[32:40])
				}
				if row.Resources&storagebudget.CapacityInodes != 0 {
					observations[i].Available.Inodes = row.Headroom.Inodes + storageEnrollmentPeakU64(raw[40:48])
				}
			}
		}
	}
	storageTestMust(t, storageEnrollmentInitialRoom(s, &q, observations, now))
	originalScope, originalPolicy := s, q.Policy
	reservation := storageEnrollmentReservation{scope: s}
	// Model native retention/allocation above the authored estimate, without
	// asserting a measured filesystem maximum. Keep the observed free value
	// exactly as supplied; the policy estimate cannot overwrite it.
	overhead := storageEnrollmentPeakU64(s[384:392]) + 1
	observed := append([]storagebudget.CapacityObservation{}, observations...)
	if overhead >= observed[0].Available.Bytes {
		observed[0].Available.Bytes = 0
	} else {
		observed[0].Available.Bytes -= overhead
	}
	beforeObserved := observed[0]
	if err := storageEnrollmentInitialRoom(s, &q, observed, now); !errors.Is(err, storagebudget.ErrHeadroom) {
		t.Fatal("truthful lower observation did not refuse", err)
	}
	if s != originalScope || q.Policy != originalPolicy || reservation.scope != originalScope || observed[0] != beforeObserved {
		t.Fatal("observed overhead changed estimate, limit, retained debt or actual observation")
	}
	// This room comparison has no Q dispatcher; uncertainty cannot be replayed
	// or declared settled from this observation. Actual Q-cut vectors remain
	// the separate existing exact-prefix tests.
}
