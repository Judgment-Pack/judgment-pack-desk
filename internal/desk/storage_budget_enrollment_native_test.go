package desk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"syscall"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Handwritten data for the pure comparator only. It is never installed and
// cannot issue an original qualification member or enable production effects.
func enrollmentNativeRecordVector(t *testing.T) storageEnrollmentNativeRecord {
	t.Helper()
	r := storageEnrollmentNativeRecord{Version: 1, Facts: storageEnrollmentNativeFacts{System: storageEnrollmentNativeSystem{GOOS: "linux", GOARCH: "amd64", Toolchain: "go1.26.10", KernelRelease: "test-kernel", KernelVersion: "test-build"}}}
	for i := range r.Facts.Parents {
		r.Facts.Parents[i] = storageEnrollmentNativeParentFacts{Backend: 1, Type: 0xef53, Flags: 4096, BlockSize: 4096, FragmentSize: 4096, ProjectFlags: 0}
	}
	paths := []string{"internal/storagebudget/io.go", "internal/storagebudget/coordinator.go", "internal/desk/storage_budget_enrollment_epoch.go", "internal/desk/storage_budget_enrollment_native.go", "internal/desk/storage_budget_enrollment_qualification.go", "internal/desk/storage_budget_enrollment_native_linux_amd64.go", "internal/desk/storage_budget_enrollment_capture.go", "internal/desk/storage_budget_enrollment_reservation.go", "internal/desk/storage_budget_enrollment_initial.go"}
	sort.Strings(paths)
	for i, path := range paths {
		var d storagebudget.Digest
		d[0] = byte(10 + i)
		r.Sources = append(r.Sources, storageEnrollmentNativeSourcePin{Path: path, Digest: d})
	}
	for i := range r.Predicates {
		artifact := func(body string) storageEnrollmentNativeArtifact {
			b := []byte(body)
			return storageEnrollmentNativeArtifact{Digest: sha256.Sum256(b), Body: b}
		}
		r.Predicates[i].Derivation = artifact("handwritten pure-comparison derivation fixture")
		r.Predicates[i].NativeResults = artifact("handwritten pure-comparison native fixture, not an executed report")
		r.Predicates[i].BoundarySet = artifact("handwritten pure-comparison finite boundary fixture")
	}
	b, err := json.Marshal(r.Sources)
	storageTestMust(t, err)
	r.Implementation = storageEnrollmentPeakHash("desk-enrollment-native-implementation-v1", b)
	return r
}

func TestStorageEnrollmentNativeRecordComparisons(t *testing.T) {
	r := enrollmentNativeRecordVector(t)
	if !storageEnrollmentNativeRecordValid(r) {
		t.Fatal("complete typed comparison vector refused")
	}
	b, err := json.Marshal([]storageEnrollmentNativeRecord{r})
	storageTestMust(t, err)
	got, err := storageEnrollmentNativeRecords(string(b))
	storageTestMust(t, err)
	if !reflect.DeepEqual(got, []storageEnrollmentNativeRecord{r}) {
		t.Fatal("record fields lost")
	}
	for bit := 0; bit < 5; bit++ {
		for part := 0; part < 3; part++ {
			bad := r
			switch part {
			case 0:
				bad.Predicates[bit].Derivation = storageEnrollmentNativeArtifact{}
			case 1:
				bad.Predicates[bit].NativeResults = storageEnrollmentNativeArtifact{}
			case 2:
				bad.Predicates[bit].BoundarySet = storageEnrollmentNativeArtifact{}
			}
			if storageEnrollmentNativeRecordValid(bad) {
				t.Fatal("missing predicate evidence accepted", bit, part)
			}
			bad = r
			switch part {
			case 0:
				bad.Predicates[bit].Derivation.Body = nil
			case 1:
				bad.Predicates[bit].NativeResults.Body = nil
			case 2:
				bad.Predicates[bit].BoundarySet.Body = nil
			}
			if storageEnrollmentNativeRecordValid(bad) {
				t.Fatal("bare predicate digest accepted", bit, part)
			}
		}
	}
	for _, raw := range []string{"", "null", "{}", "[{}]", string(b) + "[]", strings.Replace(string(b), `"Version":1`, `"Version":1,"Version":1`, 1), strings.Replace(string(b), `"Version":1`, `"Unknown":1,"Version":1`, 1), strings.Repeat(" ", 1048577)} {
		if _, err := storageEnrollmentNativeRecords(raw); err == nil {
			t.Fatal("malformed record accepted")
		}
	}
	for _, mutate := range []func(*storageEnrollmentNativeRecord){
		func(r *storageEnrollmentNativeRecord) { r.Version = 2 },
		func(r *storageEnrollmentNativeRecord) { r.Implementation[0] ^= 1 },
		func(r *storageEnrollmentNativeRecord) { r.Facts.System.GOOS = "darwin" },
		func(r *storageEnrollmentNativeRecord) { r.Facts.System.GOARCH = "arm64" },
		func(r *storageEnrollmentNativeRecord) { r.Facts.System.Toolchain = "" },
		func(r *storageEnrollmentNativeRecord) { r.Facts.System.KernelRelease = "" },
		func(r *storageEnrollmentNativeRecord) { r.Facts.System.KernelVersion = "" },
		func(r *storageEnrollmentNativeRecord) { r.Facts.Parents[0].Type = 0 },
		func(r *storageEnrollmentNativeRecord) { r.Facts.Parents[1].Backend = 2 },
		func(r *storageEnrollmentNativeRecord) { r.Facts.Parents[0].BlockSize = 0 },
		func(r *storageEnrollmentNativeRecord) { r.Sources = nil },
	} {
		bad := r
		mutate(&bad)
		if storageEnrollmentNativeRecordValid(bad) {
			t.Fatal("record mutation accepted")
		}
	}
	_, err = storageEnrollmentNativeRecords(storageEnrollmentInstalledNativeRecords)
	storageTestMust(t, err)
	// Release-owned records are parsed independently of these pure vectors.
}

func TestStorageEnrollmentNativeWriteResultVectors(t *testing.T) {
	for _, size := range []int{1, 4096, 65536} {
		if err := storageEnrollmentNativeWriteResult(size, size, nil); err != nil {
			t.Fatal("exact one-shot write refused", err)
		}
		for _, n := range []int{0, size - 1} {
			if err := storageEnrollmentNativeWriteResult(n, size, nil); !errors.Is(err, io.ErrShortWrite) {
				t.Fatal("zero/short accepted", n, size, err)
			}
			if err := storageEnrollmentNativeWriteResult(n, size, syscall.ENOSPC); !errors.Is(err, io.ErrShortWrite) || !errors.Is(err, syscall.ENOSPC) {
				t.Fatal("short ENOSPC lost", err)
			}
		}
		for _, failure := range []error{syscall.EINTR, syscall.EIO, syscall.ENOSPC} {
			if err := storageEnrollmentNativeWriteResult(size, size, failure); !errors.Is(err, failure) {
				t.Fatal("native failure lost", err)
			}
		}
	}
	for _, row := range [][2]int{{-1, 1}, {2, 1}, {0, 0}, {65537, 65537}} {
		if err := storageEnrollmentNativeWriteResult(row[0], row[1], nil); !errors.Is(err, storagebudget.ErrBounds) {
			t.Fatal("unbounded result accepted", err)
		}
	}
}

func enrollmentDenseVector() []byte {
	b := make([]byte, 32+56*2048)
	binary.LittleEndian.PutUint64(b[8:16], 8388608)
	binary.LittleEndian.PutUint32(b[20:24], 2)
	binary.LittleEndian.PutUint32(b[24:28], 2048)
	binary.LittleEndian.PutUint64(b[40:48], 4096)
	binary.LittleEndian.PutUint64(b[48:56], 4194304)
	binary.LittleEndian.PutUint64(b[88:96], 4194304)
	binary.LittleEndian.PutUint64(b[96:104], 16777216)
	binary.LittleEndian.PutUint64(b[104:112], 4194304)
	binary.LittleEndian.PutUint32(b[128:132], 1)
	return b
}

func TestStorageEnrollmentNativeDenseVectors(t *testing.T) {
	b := enrollmentDenseVector()
	storageTestMust(t, storageEnrollmentNativeDenseRecord(b, 8388608))
	for _, offset := range []int{0, 8, 16, 20, 24, 28, 32, 40, 48, 56, 72, 76, 88, 96, 104, 112, 128, 132} {
		bad := append([]byte{}, b...)
		bad[offset] ^= 1
		if err := storageEnrollmentNativeDenseRecord(bad, 8388608); err == nil {
			t.Fatal("dense mapping mutation accepted", offset)
		}
	}
	for _, flags := range []uint32{0, 2, 4, 8, 128, 256, 512, 1024, 2048, 4096, 8192, 0x80000000} {
		bad := append([]byte{}, b...)
		binary.LittleEndian.PutUint32(bad[128:132], flags)
		if err := storageEnrollmentNativeDenseRecord(bad, 8388608); err == nil {
			t.Fatal("non-initialized/shared/unknown mapping accepted", flags)
		}
	}
	if err := storageEnrollmentNativeDenseRecord(b[:len(b)-1], 8388608); err == nil {
		t.Fatal("short mapping accepted")
	}
	if err := storageEnrollmentNativeDenseRecord(b, 4194304); err == nil {
		t.Fatal("wrong original extent accepted")
	}
}

func TestStorageEnrollmentEpochOriginalExclusionAndNoEffectExit(t *testing.T) {
	s := ownerActualNew(t, ownerActualConfig(t))
	c, o := s.storageConstruction, s.storageConstruction.owner
	before, err := c.ref.cell.q.InspectHost(t.Context())
	storageTestMust(t, err)
	p, err := c.beginEnrollmentEpoch(t.Context())
	storageTestMust(t, err)
	if o.active != 1 || c.home.exclusive {
		t.Fatal("phase changed home or lost owner retain")
	}
	if _, err := o.beginAdmission(t.Context()); !errors.Is(err, storagebudget.ErrBusy) {
		t.Fatal("ordinary admission entered enrollment", err)
	}
	if err := o.retainCapacityPassOutsideExclusive(); !errors.Is(err, storagebudget.ErrAuthority) {
		t.Fatal("observer entered enrollment", err)
	}
	if _, err := c.beginEnrollmentEpoch(t.Context()); !errors.Is(err, storagebudget.ErrBusy) {
		t.Fatal("second phase entered", err)
	}
	storageTestMust(t, p.acquire(t.Context()))
	if len(p.holds) != len(o.orderedRoots()) || p.use == nil || p.project == nil || p.work == nil {
		t.Fatal("complete original exclusion missing")
	}
	if err := p.qualifyOriginalNative(t.Context()); !errors.Is(err, errStorageEnrollmentQualification) {
		t.Fatal("unqualified installed data minted authority", err)
	}
	if p.qualified != nil || p.scope != nil {
		t.Fatal("refusal created authority or scope")
	}
	storageTestMust(t, p.finish(nil))
	if !o.admissionOpen() || o.active != 0 || p.running || p.use != nil || p.project != nil || p.work != nil || len(p.holds) != 0 {
		t.Fatal("no-effect attempt did not drain/reopen")
	}
	after, err := c.ref.cell.q.InspectHost(t.Context())
	storageTestMust(t, err)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("no-effect enrollment changed Q")
	}
	ordinary, err := o.beginAdmission(t.Context())
	storageTestMust(t, err)
	storageTestMust(t, ordinary.Close())
}

func TestStorageEnrollmentEpochCancellationRetainsOriginalHolds(t *testing.T) {
	s := ownerActualNew(t, ownerActualConfig(t))
	c, o := s.storageConstruction, s.storageConstruction.owner
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	p, err := c.beginEnrollmentEpoch(ctx)
	storageTestMust(t, err)
	storageTestMust(t, p.acquire(ctx))
	entered, release := make(chan struct{}), make(chan struct{})
	c.enrollmentFault = &storageEnrollmentFault{point: "blocked-original-invocation", entered: entered, release: release}
	result := make(chan error, 1)
	go func() {
		err := p.boundary("blocked-original-invocation")
		err = errors.Join(err, p.permit(ctx))
		result <- p.finish(err)
	}()
	<-entered
	cancel()
	if err := p.Close(); !errors.Is(err, storagebudget.ErrBusy) {
		t.Fatal("Close claimed a running invocation drained", err)
	}
	if o.active != 1 || p.use == nil || p.project == nil || p.work == nil {
		t.Fatal("cancel released original exclusion")
	}
	if _, err := p.project.Stat(); err != nil {
		t.Fatal("original project descriptor closed before drain", err)
	}
	if _, err := o.beginAdmission(t.Context()); !errors.Is(err, storagebudget.ErrBusy) {
		t.Fatal("admission raced canceled owner", err)
	}
	close(release)
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
	<-p.done
	if o.active != 0 || !o.admissionOpen() {
		t.Fatal("no-effect canceled invocation did not finish original cleanup")
	}
}

func TestStorageEnrollmentEpochBusyOriginalProjectPreservesQ(t *testing.T) {
	s := ownerActualNew(t, ownerActualConfig(t))
	c := s.storageConstruction
	f, err := s.root.Open(".")
	storageTestMust(t, err)
	defer f.Close()
	storageTestMust(t, lockProjectFile(f))
	before, err := c.ref.cell.q.InspectHost(t.Context())
	storageTestMust(t, err)
	p, err := c.beginEnrollmentEpoch(t.Context())
	storageTestMust(t, err)
	err = p.acquire(t.Context())
	if !errors.Is(err, storagebudget.ErrBusy) {
		t.Fatal("original project contention ignored", err)
	}
	if finish := p.finish(err); !errors.Is(finish, storagebudget.ErrBusy) {
		t.Fatal("contention error lost", finish)
	}
	if !c.owner.admissionOpen() || c.owner.active != 0 {
		t.Fatal("busy no-effect entry retained local phase")
	}
	after, err := c.ref.cell.q.InspectHost(t.Context())
	storageTestMust(t, err)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("busy entry mutated Q")
	}
}

func TestStorageEnrollmentStartupRoleRetainsOriginalAuthority(t *testing.T) {
	cfg := ownerActualConfig(t)
	_, c, closeConstruction := ownerCrashConstruction(t, cfg)
	defer closeConstruction()
	storageTestMust(t, c.loadOrEnrollOwner(t.Context()))
	original := c.mutation
	if original == nil || !c.home.exclusive || c.owner.active != 1 {
		t.Fatal("original startup role missing")
	}
	before, err := c.ref.cell.q.InspectHost(t.Context())
	storageTestMust(t, err)
	p, err := c.beginStartupEnrollment(t.Context())
	storageTestMust(t, err)
	if c.mutation != original || c.owner.active != 2 || !c.home.exclusive || !p.startup {
		t.Fatal("startup enrollment replaced original authority or home lease")
	}
	if err = original.current(t.Context(), true); err == nil {
		t.Fatal("startup mutation entered selected enrollment")
	}
	if _, err = c.owner.beginAdmission(t.Context()); !errors.Is(err, storagebudget.ErrBusy) {
		t.Fatal("ordinary mutation entered startup enrollment", err)
	}
	storageTestMust(t, p.acquire(t.Context()))
	if p.use == nil || p.project == nil || p.work == nil || !p.sourceHoldsComplete {
		t.Fatal("startup entry lost original exclusion")
	}
	// No Scope/native qualification/effect is issued. Actual held descriptors
	// drain, then precisely the same retained startup authority becomes usable.
	storageTestMust(t, p.finish(nil))
	if c.mutation != original || c.owner.active != 1 || !c.home.exclusive || p.running || p.use != nil || p.project != nil || p.work != nil {
		t.Fatal("startup no-effect drain replaced or released original lifetime")
	}
	storageTestMust(t, original.current(t.Context(), true))
	after, err := c.ref.cell.q.InspectHost(t.Context())
	storageTestMust(t, err)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("startup role selection changed Q")
	}
}

func TestStorageEnrollmentCreationQualificationDoesNotFollowControlOnlyRecord(t *testing.T) {
	record := enrollmentNativeRecordVector(t)
	if len(record.Creation) != 0 {
		t.Fatal("old record fabricated creation evidence")
	}
	profile := storageEnrollmentNativeCreationProfile{Kind: storageCreationHistory, Branch: storageCreationPersonal, Parents: append([]storageEnrollmentNativeParentFacts{}, record.Facts.Parents[:]...), Predicates: record.Predicates}
	if !storageEnrollmentCreationProfileMatches(profile, storageCreationHistory, storageCreationPersonal, record.Facts.Parents[:]) {
		t.Fatal("complete pure comparison operands refused")
	}
	record.Creation = []storageEnrollmentNativeCreationProfile{profile}
	// Handwritten source-closure operands for the pure feature comparator.
	// The release must still bind actual source/build identities and evidence.
	paths := []string{"internal/storagebudget/types.go", "internal/storagebudget/codec.go", "internal/desk/storage_budget_enrollment_creation.go", "internal/desk/storage_budget_enrollment_creation_bridge.go", "internal/desk/storage_budget_enrollment_demand.go", "internal/desk/storage_budget_enrollment_ancestry.go", "internal/desk/storage_budget_enrollment_packet.go", "internal/desk/storage_budget_enrollment_plan_scope.go", "internal/desk/storage_budget_enrollment_project_plan.go", "internal/desk/storage_budget_enrollment_peak_source.go", "internal/desk/storage_budget_enrollment_peak_board.go", "internal/desk/storage_budget_enrollment_peak_joins.go", "internal/desk/storage_budget_enrollment_peak_evidence.go", "internal/desk/storage_budget_enrollment_peak_compare.go", "internal/desk/storage_budget_enrollment_peak_peer.go", "internal/desk/storage_budget_enrollment_recovery.go", "internal/desk/storage_budget_enrollment_census.go", "internal/desk/storage_budget_enrollment_scope_sources.go", "internal/desk/storage_budget_enrollment_bindings.go", "internal/desk/storage_budget_enrollment_coverage.go", "internal/desk/storage_budget_sources.go", "internal/desk/storage_budget_census.go", "internal/desk/storage_budget_roots.go", "internal/desk/storage_budget_owner_bindings.go", "internal/desk/storage_budget_owner_lifecycle.go", "internal/desk/storage_budget_owner_catalog_capacity.go", "internal/desk/storage_budget_admission_lifetime.go", "internal/desk/storage_budget_anchor.go", "internal/desk/storage_budget_native.go", "internal/desk/storage_budget_native_linux.go", "internal/desk/storage_budget_native_object.go", "internal/desk/storage_budget_native_ext4_linux_amd64.go", "internal/desk/private_data.go", "internal/desk/handover.go", "internal/desk/review.go", "internal/desk/upgrade.go", "internal/desk/project_lock.go", "internal/desk/dialect_draft_coordination.go", "internal/desk/assistant_dialect_artifact_owners.go", "internal/desk/assistant_dialect_artifact_project_move_recovery.go", "internal/desk/project_move.go", "internal/desk/storage.go", "internal/desk/storage_backup.go", "internal/desk/conversations.go"}
	seen := map[string]bool{}
	for _, pin := range record.Sources {
		seen[pin.Path] = true
	}
	for i, path := range paths {
		if !seen[path] {
			record.Sources = append(record.Sources, storageEnrollmentNativeSourcePin{Path: path, Digest: storagebudget.Digest{byte(i + 1), 200}})
		}
	}
	sort.Slice(record.Sources, func(i, j int) bool { return record.Sources[i].Path < record.Sources[j].Path })
	pins, err := json.Marshal(record.Sources)
	storageTestMust(t, err)
	record.Implementation = storageEnrollmentPeakHash("desk-enrollment-native-implementation-v1", pins)
	if !storageEnrollmentNativeRecordValid(record) {
		t.Fatal("explicit pure creation-profile vector refused")
	}
	raw, err := json.Marshal([]storageEnrollmentNativeRecord{record})
	storageTestMust(t, err)
	decoded, err := storageEnrollmentNativeRecords(string(raw))
	storageTestMust(t, err)
	if !reflect.DeepEqual(decoded[0], record) {
		t.Fatal("lost explicit creation profile")
	}
	for _, mutate := range []func(*storageEnrollmentNativeCreationProfile){func(p *storageEnrollmentNativeCreationProfile) { p.Kind = 0 }, func(p *storageEnrollmentNativeCreationProfile) { p.Branch = 0 }, func(p *storageEnrollmentNativeCreationProfile) { p.Parents = nil }, func(p *storageEnrollmentNativeCreationProfile) { p.Parents = append(p.Parents, p.Parents[0]) }, func(p *storageEnrollmentNativeCreationProfile) { p.Parents[0].Backend = 2 }} {
		bad := profile
		bad.Parents = append([]storageEnrollmentNativeParentFacts{}, profile.Parents...)
		mutate(&bad)
		if storageEnrollmentCreationProfileValid(bad) {
			t.Fatal("widened creation profile accepted")
		}
	}
	for bit := 0; bit < 5; bit++ {
		for part := 0; part < 3; part++ {
			bad := profile
			switch part {
			case 0:
				bad.Predicates[bit].Derivation = storageEnrollmentNativeArtifact{}
			case 1:
				bad.Predicates[bit].NativeResults = storageEnrollmentNativeArtifact{}
			case 2:
				bad.Predicates[bit].BoundarySet = storageEnrollmentNativeArtifact{}
			}
			if storageEnrollmentCreationProfileValid(bad) {
				t.Fatal("missing feature-specific native predicate", bit, part)
			}
		}
	}
	actual := append([]storageEnrollmentNativeParentFacts{}, profile.Parents...)
	actual[0].ProjectFlags++
	if storageEnrollmentCreationProfileMatches(profile, profile.Kind, profile.Branch, actual) || storageEnrollmentCreationProfileMatches(profile, storageCreationPrivate, profile.Branch, profile.Parents) {
		t.Fatal("profile wildcarded actual parent facts or action")
	}
	// No test record enters the immutable installed input or enables effects.
	_, err = storageEnrollmentNativeRecords(storageEnrollmentInstalledNativeRecords)
	storageTestMust(t, err)
	// This fixture never reaches the original issuer or installed input.
}
func TestStorageEnrollmentCreationFixedDenseBoundaries(t *testing.T) {
	for _, length := range []uint64{1048576, 2097152, 4194304, 8388608, 33554432} {
		count := 2048
		if length == 33554432 {
			count = 8192
		}
		raw := make([]byte, 32+56*count)
		binary.LittleEndian.PutUint64(raw[8:16], length)
		binary.LittleEndian.PutUint32(raw[20:24], 1)
		binary.LittleEndian.PutUint32(raw[24:28], uint32(count))
		binary.LittleEndian.PutUint64(raw[40:48], 4096)
		binary.LittleEndian.PutUint64(raw[48:56], length)
		binary.LittleEndian.PutUint32(raw[72:76], 1)
		storageTestMust(t, storageEnrollmentNativeDenseRecord(raw, length))
		binary.LittleEndian.PutUint64(raw[48:56], length-4096)
		if storageEnrollmentNativeDenseRecord(raw, length) == nil {
			t.Fatal("accepted sparse fixed control", length)
		}
	}
	for _, length := range []uint64{0, 65536, 524288, 16777216, 67108864} {
		if storageEnrollmentDenseCount(length) != 0 {
			t.Fatal("unselected control extent", length)
		}
	}
}

// Exercise the exact finish-then-fresh-startup error ownership pattern with
// actual original startup roles and the real successor entry. No Completed
// packet or native qualification is fabricated; this isolates the finalizer
// contract which previously discarded the fresh child's distinct error.
func TestStorageEnrollmentSuccessorFailureSurvivesFinishedOuterEpoch(t *testing.T) {
	cfg := ownerActualConfig(t)
	_, c, closeConstruction := ownerCrashConstruction(t, cfg)
	defer closeConstruction()
	storageTestMust(t, c.loadOrEnrollOwner(t.Context()))
	original := c.mutation
	before, err := c.ref.cell.q.InspectHost(t.Context())
	storageTestMust(t, err)
	hosts, err := os.ReadFile(filepath.Join(cfg.DeskConfigDir, storageIntentName))
	storageTestMust(t, err)
	failed := errors.New("handwritten fresh-successor failure after original finish")
	entered := make(chan struct{})
	c.enrollmentFault = &storageEnrollmentFault{point: "retired-successor-startup-before-acquire", entered: entered, err: failed}
	var old *storageEnrollmentEpoch
	returned := func() (err error) {
		old, err = c.beginStartupEnrollment(t.Context())
		if err != nil {
			return err
		}
		defer func() { err = old.finish(err) }()
		if err = old.acquire(t.Context()); err != nil {
			return err
		}
		if err = old.finish(nil); err != nil {
			return err
		}
		if old.running || c.mutation != original || c.owner.active != 1 {
			return errors.New("original startup epoch failed to restore its owner")
		}
		return c.resumeRetiredCreationStartup(t.Context(), storageEnrollmentPeakEvidence{}, storageCreationIntent{}, nil)
	}()
	select {
	case <-entered:
	default:
		t.Fatal("real fresh successor entry did not reach the failure seam")
	}
	if !errors.Is(returned, failed) {
		t.Fatal("outer finalization discarded fresh successor error", returned)
	}
	if old == nil || old.running || old.err != nil || old.use != nil || old.project != nil || old.work != nil || len(old.holds) != 0 || c.owner.active != 1 || c.mutation != original || !c.home.exclusive {
		t.Fatal("failure changed original cleanup/retain ownership")
	}
	storageTestMust(t, original.current(t.Context(), true))
	if _, err = c.owner.beginAdmission(t.Context()); !errors.Is(err, storagebudget.ErrBusy) {
		t.Fatal("fresh startup failure exposed ordinary admission", err)
	}
	after, err := c.ref.cell.q.InspectHost(t.Context())
	storageTestMust(t, err)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("fresh failure mutated Q")
	}
	current, err := os.ReadFile(filepath.Join(cfg.DeskConfigDir, storageIntentName))
	storageTestMust(t, err)
	if !bytes.Equal(hosts, current) {
		t.Fatal("fresh failure rewrote host history")
	}
	cleanup := errors.New("handwritten already-retained cleanup failure")
	finished := &storageEnrollmentEpoch{err: cleanup}
	finished.self = finished
	joined := finished.finish(failed)
	if !errors.Is(joined, cleanup) || !errors.Is(joined, failed) {
		t.Fatal("repeat finalization discarded mandatory cleanup or later failure")
	}
}

func TestStorageEnrollmentNativeV2PrivateClosedCohort(t *testing.T) {
	r := enrollmentNativeRecordVector(t)
	fact := r.Facts.Parents[0]
	profile := storageEnrollmentNativeCreationProfile{Kind: storageCreationPrivate, Branch: storageCreationContained, Parents: []storageEnrollmentNativeParentFacts{fact}, Predicates: r.Predicates}
	for _, roles := range [][]byte{{1}, {2}, {2, 3}} {
		actual := []storageEnrollmentNativeParentFacts{fact}
		if len(roles) == 2 {
			actual = append(actual, fact)
		}
		if !storageEnrollmentCreationProfileMatchesVersion(2, profile, 1, 2, roles, actual) {
			t.Fatal("required private arity/role refused", roles)
		}
		if len(roles) == 2 && storageEnrollmentCreationProfileMatchesVersion(1, profile, 1, 2, roles, actual) {
			t.Fatal("v1 reused as cohort proof")
		}
		for i := range actual {
			bad := append([]storageEnrollmentNativeParentFacts{}, actual...)
			bad[i].Flags++
			if storageEnrollmentCreationProfileMatchesVersion(2, profile, 1, 2, roles, bad) {
				t.Fatal("discarded distinct actual fact", roles, i)
			}
		}
	}
	for _, roles := range [][]byte{nil, {3}, {0}, {1, 2}, {3, 2}, {2, 2}, {2, 3, 3}} {
		actual := make([]storageEnrollmentNativeParentFacts, len(roles))
		for i := range actual {
			actual[i] = fact
		}
		if storageEnrollmentCreationProfileMatchesVersion(2, profile, 1, 2, roles, actual) {
			t.Fatal("unknown role/order/arity admitted", roles)
		}
	}
	for bit := 0; bit < 5; bit++ {
		for part := 0; part < 3; part++ {
			bad := profile
			switch part {
			case 0:
				bad.Predicates[bit].Derivation = storageEnrollmentNativeArtifact{}
			case 1:
				bad.Predicates[bit].NativeResults = storageEnrollmentNativeArtifact{}
			case 2:
				bad.Predicates[bit].BoundarySet = storageEnrollmentNativeArtifact{}
			}
			if storageEnrollmentCreationProfileMatchesVersion(2, bad, 1, 2, []byte{2, 3}, []storageEnrollmentNativeParentFacts{fact, fact}) {
				t.Fatal("predicate body omitted", bit, part)
			}
		}
	}
	// This pure comparison cannot issue a qualification member.
}

// This is a handwritten source contract vector, with fake digests and labeled
// bodies. It exercises parsing only and is never an installed native record.
func enrollmentNativeEstimateRecordVector(t *testing.T, goos string) storageEnrollmentNativeRecord {
	t.Helper()
	r := enrollmentNativeRecordVector(t)
	r.Version, r.CapacityPolicy = 3, 2
	paths := strings.Fields(`
internal/desk/storage_budget_transfer_intent.go
internal/desk/storage_transfer_lifetime.go
internal/desk/storage_budget_owner_catalog_json.go
internal/desk/storage_budget_enrollment_peak_projection.go
internal/desk/storage_budget_infrastructure.go
internal/desk/storage_budget_owner_catalog_objects.go
internal/desk/storage_budget_owner_store.go
internal/desk/storage_budget_startup.go
internal/desk/ai_memory.go
internal/desk/attachments.go
internal/storagebudget/fanout.go
internal/storagebudget/accounting.go
internal/storagebudget/recovery.go
internal/storagebudget/host_state.go
internal/storagebudget/enrollment_inspection.go
internal/desk/storage_budget_enrollment_peak_q.go
internal/desk/storage_budget_enrollment_peak_data.go
internal/desk/storage_budget_enrollment_q_fanout.go
internal/desk/storage_budget_enrollment_operands.go
internal/desk/storage_budget_enrollment_q.go
internal/desk/storage_budget_writer.go
internal/desk/storage_budget_single_file_fanout.go
internal/desk/storage_budget_owner_catalog_v4_material.go
internal/storagebudget/types.go
internal/storagebudget/codec.go
internal/storagebudget/sync_other.go
internal/desk/storage_budget_enrollment_creation.go
internal/desk/storage_budget_enrollment_creation_bridge.go
internal/desk/storage_budget_enrollment_demand.go
internal/desk/storage_budget_enrollment_ancestry.go
internal/desk/storage_budget_enrollment_packet.go
internal/desk/storage_budget_enrollment_plan_scope.go
internal/desk/storage_budget_enrollment_project_plan.go
internal/desk/storage_budget_enrollment_peak_source.go
internal/desk/storage_budget_enrollment_peak_board.go
internal/desk/storage_budget_enrollment_peak_joins.go
internal/desk/storage_budget_enrollment_peak_evidence.go
internal/desk/storage_budget_enrollment_peak_compare.go
internal/desk/storage_budget_enrollment_peak_peer.go
internal/desk/storage_budget_enrollment_recovery.go
internal/desk/storage_budget_enrollment_census.go
internal/desk/storage_budget_enrollment_scope_sources.go
internal/desk/storage_budget_enrollment_bindings.go
internal/desk/storage_budget_enrollment_coverage.go
internal/desk/storage_budget_sources.go
internal/desk/storage_budget_census.go
internal/desk/storage_budget_roots.go
internal/desk/storage_budget_owner_bindings.go
internal/desk/storage_budget_owner_lifecycle.go
internal/desk/storage_budget_owner_catalog_capacity.go
internal/desk/storage_budget_admission_lifetime.go
internal/desk/storage_budget_anchor.go
internal/desk/storage_budget_native.go
internal/desk/storage_budget_native_linux.go
internal/desk/storage_budget_native_object.go
internal/desk/storage_budget_native_ext4_linux_amd64.go
internal/desk/private_data.go
internal/desk/handover.go
internal/desk/review.go
internal/desk/upgrade.go
internal/desk/project_lock.go
internal/desk/dialect_draft_coordination.go
internal/desk/assistant_dialect_artifact_owners.go
internal/desk/assistant_dialect_artifact_project_move_recovery.go
internal/desk/project_move.go
internal/desk/storage.go
internal/desk/storage_backup.go
internal/desk/conversations.go
internal/desk/storage_budget_owner_catalog_v5.go
internal/desk/storage_budget_owner_catalog_v5_validate.go
internal/desk/storage_budget_owner_catalog_v5_codec.go
internal/desk/storage_budget_owner_catalog_v5_effects.go
internal/desk/storage_budget_owner_catalog_v5_relations.go
internal/desk/storage_budget_owner_catalog_v5_capacity.go
internal/desk/storage_budget_owner_catalog_v5_chain.go
internal/desk/storage_budget_identity_intent.go
internal/desk/storage_budget_identity_intent_material.go
internal/desk/server.go
internal/desk/startup_identity.go
internal/desk/storage_budget_enrollment_peak_scope.go
internal/desk/storage_budget_enrollment_peak_catalog.go
internal/desk/storage_budget_enrollment_peak_dispatch.go
internal/desk/storage_budget_enrollment_peak_wire.go
internal/desk/storage_budget_intent.go
`)
	if goos == "darwin" {
		r.Facts.System.GOOS = "darwin"
		r.Facts.System.GOARCH = "arm64"
		for i := range r.Facts.Parents {
			r.Facts.Parents[i] = storageEnrollmentNativeParentFacts{Backend: 2, Type: 26, BlockSize: 4096}
		}
		for i, pin := range r.Sources {
			if pin.Path == "internal/desk/storage_budget_enrollment_native_linux_amd64.go" {
				r.Sources[i].Path = "internal/desk/storage_budget_enrollment_native_darwin.go"
			}
		}
		paths = append(paths, "internal/storagebudget/sync_darwin.go", "internal/desk/storage_budget_native_darwin.go", "internal/desk/storage_budget_native_darwin_cgo.go", "internal/desk/storage_budget_native_darwin_records.go", "internal/desk/storage_budget_observer_darwin.go")
	}
	for _, path := range paths {
		r.Sources = append(r.Sources, storageEnrollmentNativeSourcePin{Path: path, Digest: storagebudget.Digest{17}})
	}
	sort.Slice(r.Sources, func(i, j int) bool { return r.Sources[i].Path < r.Sources[j].Path })
	pins, err := json.Marshal(r.Sources)
	storageTestMust(t, err)
	r.Implementation = epHash("desk-enrollment-native-implementation-capacity-estimate-v1", pins)
	return r
}

func TestStorageEnrollmentNativeEstimateRecordVersionAndBounds(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		r := enrollmentNativeEstimateRecordVector(t, goos)
		wantCount := 93
		if goos == "darwin" {
			wantCount = 98
		}
		if len(r.Sources) != wantCount || !storageEnrollmentNativeRecordValid(r) {
			t.Fatal("complete handwritten source contract refused", goos, len(r.Sources))
		}
		raw, err := json.Marshal([]storageEnrollmentNativeRecord{r})
		storageTestMust(t, err)
		got, err := storageEnrollmentNativeRecords(string(raw))
		storageTestMust(t, err)
		if !reflect.DeepEqual(got, []storageEnrollmentNativeRecord{r}) {
			t.Fatal("new record identity lost")
		}
		for i := range r.Sources {
			bad := r
			bad.Sources = append([]storageEnrollmentNativeSourcePin{}, r.Sources[:i]...)
			bad.Sources = append(bad.Sources, r.Sources[i+1:]...)
			pins, err := json.Marshal(bad.Sources)
			storageTestMust(t, err)
			bad.Implementation = epHash("desk-enrollment-native-implementation-capacity-estimate-v1", pins)
			if storageEnrollmentNativeRecordValid(bad) {
				t.Fatal("missing source pin accepted", r.Sources[i].Path)
			}
			encoded, err := json.Marshal([]storageEnrollmentNativeRecord{bad})
			storageTestMust(t, err)
			if _, err := storageEnrollmentNativeRecords(string(encoded)); err == nil {
				t.Fatal("canonical record omitted required source pin", r.Sources[i].Path)
			}
		}
		if goos == "darwin" {
			amd64 := r
			amd64.Facts.System.GOARCH = "amd64"
			if !storageEnrollmentNativeRecordValid(amd64) {
				t.Fatal("Darwin amd64 shares the supported observation ABI, with separate exact facts")
			}
		}
		for _, version := range []uint16{1, 2} {
			bad := r
			bad.Version = version
			if storageEnrollmentNativeRecordValid(bad) {
				t.Fatal("estimate evidence relabeled as legacy record")
			}
		}
		for _, n := range []int{98, 99} {
			bad := r
			bad.Sources = append([]storageEnrollmentNativeSourcePin{}, r.Sources...)
			for len(bad.Sources) < n {
				bad.Sources = append(bad.Sources, storageEnrollmentNativeSourcePin{Path: fmt.Sprintf("extra/pin-%02d.go", len(bad.Sources)), Digest: storagebudget.Digest{29}})
			}
			sort.Slice(bad.Sources, func(i, j int) bool { return bad.Sources[i].Path < bad.Sources[j].Path })
			pins, err := json.Marshal(bad.Sources)
			storageTestMust(t, err)
			bad.Implementation = epHash("desk-enrollment-native-implementation-capacity-estimate-v1", pins)
			if storageEnrollmentNativeRecordValid(bad) != (n == 98) {
				t.Fatal("Record3 source boundary", n)
			}
			encoded, err := json.Marshal([]storageEnrollmentNativeRecord{bad})
			storageTestMust(t, err)
			_, err = storageEnrollmentNativeRecords(string(encoded))
			if (err == nil) != (n == 98) {
				t.Fatal("canonical Record3 source boundary", n, err)
			}
		}
		// Individually bounded artifact bodies can still exceed the original
		// aggregate input limit. The canonical whole record must refuse too.
		large := r
		body := bytes.Repeat([]byte{42}, 65536)
		artifact := storageEnrollmentNativeArtifact{Body: body, Digest: sha256.Sum256(body)}
		for i := range large.Predicates {
			large.Predicates[i] = storageEnrollmentNativePredicateEvidence{Derivation: artifact, NativeResults: artifact, BoundarySet: artifact}
		}
		tooLarge, err := json.Marshal([]storageEnrollmentNativeRecord{large})
		storageTestMust(t, err)
		if len(tooLarge) <= 1<<20 {
			t.Fatal("handwritten aggregate boundary did not exceed fixed input limit")
		}
		if _, err := storageEnrollmentNativeRecords(string(tooLarge)); !errors.Is(err, storagebudget.ErrBounds) {
			t.Fatal("canonical oversized record bypassed aggregate limit", err)
		}
		if _, err := storageEnrollmentNativeRecords(strings.Repeat(" ", (1<<20)+1)); !errors.Is(err, storagebudget.ErrBounds) {
			t.Fatal("whole-record input limit changed", err)
		}
		// Artifact integrity alone cannot replace any of the five subjects.
		for i := range r.Predicates {
			bad := r
			bad.Predicates[i].BoundarySet = storageEnrollmentNativeArtifact{}
			if storageEnrollmentNativeRecordValid(bad) {
				t.Fatal("estimate policy dropped native subject", i)
			}
		}
	}
	// Syntactic validity is separate from the original qualification issuer.
}

// Actual read-only leaves on each published native target. An unsupported
// filesystem/ABI is explicit; this test does not manufacture qualification.
func TestStorageEnrollmentNativeEstimateObservationPrerequisites(t *testing.T) {
	if !(runtime.GOOS == "linux" && runtime.GOARCH == "amd64" || runtime.GOOS == "darwin" && (runtime.GOARCH == "arm64" || runtime.GOARCH == "amd64")) {
		t.Skip("enrollment observation target is not implemented")
	}
	system, err := storageEnrollmentNativeSystemFacts(t.Context())
	storageTestMust(t, err)
	t.Logf("actual native system facts: %+v", system)
	if system.KernelRelease == "" || system.KernelVersion == "" {
		t.Fatal("actual kernel identity unavailable")
	}
	root, err := os.OpenRoot(t.TempDir())
	storageTestMust(t, err)
	defer root.Close()
	parent, err := root.Open(".")
	storageTestMust(t, err)
	defer parent.Close()
	facts, err := storageEnrollmentNativeParentObservation(t.Context(), parent)
	t.Logf("actual native parent observation: %+v error=%v", facts, err)
	storageTestMust(t, err)
	if !storageEnrollmentNativeParentFactsValid(facts, runtime.GOOS, 3) {
		t.Fatal("actual parent facts do not match the supported observation prerequisite; no qualification established")
	}
	// This scratch observation calls no production qualifier or enrollment
	// issuer. Its prerequisites apply independently of installed release records.
}

// Scratch syscall evidence is separate from complete production-entry
// qualification. Parent execution pins exact source/build identities and logs.
func TestStorageEnrollmentNativeEstimateActualWriteAndCheckedSync(t *testing.T) {
	if !(runtime.GOOS == "linux" && runtime.GOARCH == "amd64" || runtime.GOOS == "darwin" && (runtime.GOARCH == "arm64" || runtime.GOARCH == "amd64")) {
		t.Skip("native enrollment syscall target is not implemented")
	}
	system, err := storageEnrollmentNativeSystemFacts(t.Context())
	storageTestMust(t, err)
	t.Logf("native syscall route: %s/%s toolchain=%s kernel=%q version=%q", runtime.GOOS, runtime.GOARCH, runtime.Version(), system.KernelRelease, system.KernelVersion)
	if info, ok := debug.ReadBuildInfo(); ok {
		t.Logf("actual test build: %s", info.String())
	} else {
		t.Log("test build info unavailable; parent exact build/source manifest is required")
	}
	root, err := os.OpenRoot(t.TempDir())
	storageTestMust(t, err)
	defer root.Close()
	for _, size := range []int{4096, 65536} {
		name := fmt.Sprintf("original-%d", size)
		file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		storageTestMust(t, err)
		body := bytes.Repeat([]byte{0x68, 0x05, 0x31, 0x02}, size/4)
		writeErr := storageEnrollmentNativeWrite(t.Context(), file, body, 0)
		var syncErr error
		if writeErr == nil {
			syncErr = storageEnrollmentNativeSync(t.Context(), file)
		}
		closeErr := file.Close()
		storageTestMust(t, writeErr)
		storageTestMust(t, closeErr)
		if syncErr != nil {
			t.Fatalf("actual %d-byte file full-sync prerequisite refused: %v; no qualification established", size, syncErr)
		}
		parent, err := root.Open(".")
		storageTestMust(t, err)
		parentErr := storageEnrollmentNativeSync(t.Context(), parent)
		storageTestMust(t, parent.Close())
		if parentErr != nil {
			t.Fatalf("actual original-parent full-sync prerequisite refused after %d-byte file success: %v; no qualification established", size, parentErr)
		}
		read, err := root.Open(name)
		storageTestMust(t, err)
		got, readErr := io.ReadAll(io.LimitReader(read, int64(size+1)))
		storageTestMust(t, read.Close())
		storageTestMust(t, readErr)
		if !bytes.Equal(body, got) {
			t.Fatal("actual fixed write/sync/close/readback differed", size)
		}
		t.Logf("observed actual %d-byte Write/checked-Sync/Close/parent-Sync/readback; this is not process-death or power-loss evidence", size)
	}
	// These scratch native leaves call no production qualifier or enrollment
	// issuer. Passing them does not establish whole-entry qualification.
}

func TestStorageEnrollmentLegacyNativeSourceBoundsUnchanged(t *testing.T) {
	for _, version := range []uint16{1, 2} {
		for _, count := range []int{64, 65} {
			r := enrollmentNativeRecordVector(t)
			r.Version = version
			if version == 2 {
				// Handwritten legacy source union. Record3-only dependencies must
				// neither become legacy requirements nor enlarge the legacy cap.
				paths := strings.Fields(`
internal/storagebudget/types.go
internal/storagebudget/codec.go
internal/desk/storage_budget_enrollment_creation.go
internal/desk/storage_budget_enrollment_creation_bridge.go
internal/desk/storage_budget_enrollment_demand.go
internal/desk/storage_budget_enrollment_ancestry.go
internal/desk/storage_budget_enrollment_packet.go
internal/desk/storage_budget_enrollment_plan_scope.go
internal/desk/storage_budget_enrollment_project_plan.go
internal/desk/storage_budget_enrollment_peak_source.go
internal/desk/storage_budget_enrollment_peak_board.go
internal/desk/storage_budget_enrollment_peak_joins.go
internal/desk/storage_budget_enrollment_peak_evidence.go
internal/desk/storage_budget_enrollment_peak_compare.go
internal/desk/storage_budget_enrollment_peak_peer.go
internal/desk/storage_budget_enrollment_recovery.go
internal/desk/storage_budget_enrollment_census.go
internal/desk/storage_budget_enrollment_scope_sources.go
internal/desk/storage_budget_enrollment_bindings.go
internal/desk/storage_budget_enrollment_coverage.go
internal/desk/storage_budget_sources.go
internal/desk/storage_budget_census.go
internal/desk/storage_budget_roots.go
internal/desk/storage_budget_owner_bindings.go
internal/desk/storage_budget_owner_lifecycle.go
internal/desk/storage_budget_owner_catalog_capacity.go
internal/desk/storage_budget_admission_lifetime.go
internal/desk/storage_budget_anchor.go
internal/desk/storage_budget_native.go
internal/desk/storage_budget_native_linux.go
internal/desk/storage_budget_native_object.go
internal/desk/storage_budget_native_ext4_linux_amd64.go
internal/desk/private_data.go
internal/desk/handover.go
internal/desk/review.go
internal/desk/upgrade.go
internal/desk/project_lock.go
internal/desk/dialect_draft_coordination.go
internal/desk/assistant_dialect_artifact_owners.go
internal/desk/assistant_dialect_artifact_project_move_recovery.go
internal/desk/project_move.go
internal/desk/storage.go
internal/desk/storage_backup.go
internal/desk/conversations.go
internal/desk/storage_budget_owner_catalog_v5.go
internal/desk/storage_budget_owner_catalog_v5_validate.go
internal/desk/storage_budget_owner_catalog_v5_codec.go
internal/desk/storage_budget_owner_catalog_v5_effects.go
internal/desk/storage_budget_owner_catalog_v5_relations.go
internal/desk/storage_budget_owner_catalog_v5_capacity.go
internal/desk/storage_budget_owner_catalog_v5_chain.go
internal/desk/storage_budget_identity_intent.go
internal/desk/storage_budget_identity_intent_material.go
internal/desk/server.go
internal/desk/startup_identity.go
`)
				for _, path := range paths {
					r.Sources = append(r.Sources, storageEnrollmentNativeSourcePin{Path: path, Digest: storagebudget.Digest{31}})
				}
				if len(r.Sources) != 64 {
					t.Fatal("handwritten legacy Record2 union changed", len(r.Sources))
				}
			}
			for len(r.Sources) < count {
				r.Sources = append(r.Sources, storageEnrollmentNativeSourcePin{Path: fmt.Sprintf("extra/legacy-%02d.go", len(r.Sources)), Digest: storagebudget.Digest{31}})
			}
			sort.Slice(r.Sources, func(i, j int) bool { return r.Sources[i].Path < r.Sources[j].Path })
			pins, err := json.Marshal(r.Sources)
			storageTestMust(t, err)
			r.Implementation = epHash("desk-enrollment-native-implementation-v1", pins)
			if storageEnrollmentNativeRecordValid(r) != (count == 64) {
				t.Fatal("legacy source-list bound changed", version, count)
			}
			encoded, err := json.Marshal([]storageEnrollmentNativeRecord{r})
			storageTestMust(t, err)
			if _, err := storageEnrollmentNativeRecords(string(encoded)); (err == nil) != (count == 64) {
				t.Fatal("canonical legacy source-list bound changed", version, count, err)
			}
		}
	}
}

// Handwritten data comparisons only. No record is installed, and these tests
// create no original qualification member or native execution permission.
func TestStorageEnrollmentRecord3PublicABIApplicability(t *testing.T) {
	for _, target := range []struct{ goos, arch string }{{"linux", "amd64"}, {"darwin", "arm64"}, {"darwin", "amd64"}} {
		t.Run(target.goos+"/"+target.arch, func(t *testing.T) {
			r := enrollmentNativeEstimateRecordVector(t, target.goos)
			r.Facts.System.GOARCH = target.arch
			// Ordered original parents may have different exact mount facts.
			r.Facts.Parents[1].Flags ^= 1
			if !storageEnrollmentNativeRecordValid(r) {
				t.Fatal("handwritten ABI/profile vector invalid")
			}
			actual := r.Facts
			actual.System.KernelRelease = "different-supported-kernel-release"
			actual.System.KernelVersion = "different-supported-kernel-build"
			if !storageEnrollmentNativeRecordApplies(r, actual) {
				t.Fatal("kernel provenance incorrectly became exact build permission")
			}
			if !storageEnrollmentNativeInvocationMatches(r, actual, actual) {
				t.Fatal("exact actual invocation capture refused")
			}
			mutations := []struct {
				name   string
				change func(*storageEnrollmentNativeFacts)
			}{
				{"os", func(f *storageEnrollmentNativeFacts) { f.System.GOOS = "freebsd" }},
				{"arch", func(f *storageEnrollmentNativeFacts) { f.System.GOARCH = "386" }},
				{"other-supported-arch", func(f *storageEnrollmentNativeFacts) {
					if target.arch == "amd64" {
						f.System.GOARCH = "arm64"
					} else {
						f.System.GOARCH = "amd64"
					}
				}},
				{"toolchain", func(f *storageEnrollmentNativeFacts) { f.System.Toolchain = "go1.26.11" }},
				{"empty-release", func(f *storageEnrollmentNativeFacts) { f.System.KernelRelease = "" }},
				{"oversized-release", func(f *storageEnrollmentNativeFacts) { f.System.KernelRelease = strings.Repeat("k", 257) }},
				{"empty-version", func(f *storageEnrollmentNativeFacts) { f.System.KernelVersion = "" }},
				{"oversized-version", func(f *storageEnrollmentNativeFacts) { f.System.KernelVersion = strings.Repeat("k", 257) }},
				{"newline-version", func(f *storageEnrollmentNativeFacts) { f.System.KernelVersion = "kernel\nversion" }},
				{"ordered-parents", func(f *storageEnrollmentNativeFacts) { f.Parents[0], f.Parents[1] = f.Parents[1], f.Parents[0] }},
			}
			for parent := 0; parent < 2; parent++ {
				for _, axis := range []string{"backend", "type", "flags", "block", "fragment", "project"} {
					mutations = append(mutations, struct {
						name   string
						change func(*storageEnrollmentNativeFacts)
					}{
						fmt.Sprintf("parent%d-%s", parent, axis),
						func(f *storageEnrollmentNativeFacts) {
							p := &f.Parents[parent]
							switch axis {
							case "backend":
								p.Backend ^= 1
							case "type":
								p.Type ^= 1
							case "flags":
								p.Flags ^= 1 << 63
							case "block":
								p.BlockSize++
							case "fragment":
								p.FragmentSize++
							case "project":
								p.ProjectFlags ^= 1 << 31
							}
						},
					})
				}
			}
			for _, mutation := range mutations {
				t.Run(mutation.name, func(t *testing.T) {
					bad := actual
					mutation.change(&bad)
					if storageEnrollmentNativeRecordApplies(r, bad) || storageEnrollmentNativeInvocationMatches(r, actual, bad) {
						t.Fatal("ABI, provenance bound or exact parent cohort/currentness was broadened")
					}
				})
			}
			for _, version := range []uint16{1, 2} {
				legacy := r
				legacy.Version, legacy.CapacityPolicy = version, 0
				if storageEnrollmentNativeRecordApplies(legacy, actual) {
					t.Fatal("legacy record acquired unpublished Record3 applicability")
				}
			}
		})
	}
}

func TestStorageEnrollmentRecord3InvocationKeepsExactCapturedFacts(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		r := enrollmentNativeEstimateRecordVector(t, goos)
		captured := r.Facts
		captured.System.KernelRelease = "actual-original-release"
		captured.System.KernelVersion = "actual-original-version"
		if !storageEnrollmentNativeRecordApplies(r, captured) || !storageEnrollmentNativeInvocationMatches(r, captured, captured) {
			t.Fatal("release applicability confused with live invocation capture")
		}
		// Even the record's original report cannot replace this invocation's
		// different actual capture after qualification.
		if storageEnrollmentNativeInvocationMatches(r, captured, r.Facts) {
			t.Fatal("release report replaced original invocation facts")
		}
		for _, change := range []func(*storageEnrollmentNativeFacts){
			func(f *storageEnrollmentNativeFacts) { f.System.KernelRelease = "another-supported-release" },
			func(f *storageEnrollmentNativeFacts) { f.System.KernelVersion = "another-supported-version" },
		} {
			current := captured
			change(&current)
			if !storageEnrollmentNativeRecordApplies(r, current) {
				t.Fatal("handwritten current report must still belong to the release ABI cohort")
			}
			if storageEnrollmentNativeInvocationMatches(r, captured, current) {
				t.Fatal("applicable report bypassed exact in-invocation currentness")
			}
		}
		if storageEnrollmentNativeInvocationMatches(r, storageEnrollmentNativeFacts{}, storageEnrollmentNativeFacts{}) {
			t.Fatal("missing original capture was treated as current")
		}
		before := captured
		for parent := range captured.Parents {
			current := captured
			current.Parents[parent].Flags ^= 1
			if storageEnrollmentNativeInvocationMatches(r, captured, current) || captured != before {
				t.Fatal("actual parent drift accepted or original capture mutated")
			}
		}
	}
}

func TestStorageEnrollmentRecord3RejectsOverlappingKernelReports(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		r := enrollmentNativeEstimateRecordVector(t, goos)
		second := r
		second.Facts.System.KernelRelease = "another-kernel-report"
		second.Facts.System.KernelVersion = "another-build-report"
		firstBody, err := json.Marshal(r)
		storageTestMust(t, err)
		secondBody, err := json.Marshal(second)
		storageTestMust(t, err)
		if bytes.Equal(firstBody, secondBody) || epHash(storageEnrollmentNativeProfileDomain(3), firstBody) == epHash(storageEnrollmentNativeProfileDomain(3), secondBody) {
			t.Fatal("complete record stopped binding kernel evidence provenance")
		}
		encoded, err := json.Marshal([]storageEnrollmentNativeRecord{r, second})
		storageTestMust(t, err)
		if _, err := storageEnrollmentNativeRecords(string(encoded)); !errors.Is(err, errStorageEnrollmentQualification) {
			t.Fatal("two reports ambiguously qualified one ABI/parent cohort", err)
		}
		// A different exact parent cohort remains distinct, without dropping
		// any raw flag from its evidence or actual per-invocation comparison.
		second.Facts.Parents[1].Flags ^= 1
		encoded, err = json.Marshal([]storageEnrollmentNativeRecord{r, second})
		storageTestMust(t, err)
		decoded, err := storageEnrollmentNativeRecords(string(encoded))
		storageTestMust(t, err)
		if len(decoded) != 2 || !reflect.DeepEqual(decoded[0], r) || !reflect.DeepEqual(decoded[1], second) {
			t.Fatal("distinct complete exact cohorts lost")
		}
	}
	legacy := enrollmentNativeRecordVector(t)
	encoded, err := json.Marshal([]storageEnrollmentNativeRecord{legacy, legacy})
	storageTestMust(t, err)
	if _, err := storageEnrollmentNativeRecords(string(encoded)); !errors.Is(err, errStorageEnrollmentQualification) {
		t.Fatal("legacy duplicate refusal changed", err)
	}
}

// These are actual safe native failures. ENOSPC/short/EINTR rows above are
// modeled results; no test fills a filesystem or presents injection as errno.
func TestStorageEnrollmentNativeActualErrnoPreservesOriginal(t *testing.T) {
	if !(runtime.GOOS == "linux" && runtime.GOARCH == "amd64" || runtime.GOOS == "darwin" && (runtime.GOARCH == "arm64" || runtime.GOARCH == "amd64")) {
		t.Skip("outside the published native matrix")
	}
	root, err := os.OpenRoot(t.TempDir())
	storageTestMust(t, err)
	defer root.Close()
	f, err := root.OpenFile("original", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	storageTestMust(t, err)
	defer f.Close()
	original := bytes.Repeat([]byte{0x68, 0x05, 0x19, 0x31}, 1024)
	storageTestMust(t, storageEnrollmentNativeWrite(t.Context(), f, original, 0))
	storageTestMust(t, storageEnrollmentNativeSync(t.Context(), f))
	before, err := f.Stat()
	storageTestMust(t, err)
	collision, err := root.OpenFile("original", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if collision != nil {
		collision.Close()
		t.Fatal("O_EXCL replaced an existing original")
	}
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("actual O_EXCL collision: %v", err)
	}
	readOnly, err := root.Open("original")
	storageTestMust(t, err)
	defer readOnly.Close()
	if err := storageEnrollmentNativeWrite(t.Context(), readOnly, []byte{0xff}, 0); !errors.Is(err, syscall.EBADF) {
		t.Fatalf("actual readonly pwrite must return EBADF: %v", err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := storageEnrollmentNativeWrite(cancelled, f, []byte{0xfe}, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled native call: %v", err)
	}
	after, err := f.Stat()
	storageTestMust(t, err)
	got := make([]byte, len(original))
	_, err = f.ReadAt(got, 0)
	storageTestMust(t, err)
	if !os.SameFile(before, after) || after.Size() != int64(len(original)) || !bytes.Equal(got, original) {
		t.Fatal("failed native operation changed original object or bytes")
	}
	t.Log("actual O_EXCL EEXIST and readonly pwrite EBADF; cancellation is a pre-call refusal; no ENOSPC or power-loss claim")
}

func TestStorageTransferRecord3CompleteSourceUnionRemainsBounded(t *testing.T) {
	// Pure handwritten comparison records only. Every source in the complete
	// mandatory union is explicit, including pure operands and actual arm/Q
	// dispatch. These records are never installed and certify no executable.
	for _, goos := range []string{"linux", "darwin"} {
		r := enrollmentNativeEstimateRecordVector(t, goos)
		want := 93
		if goos == "darwin" {
			want = 98
		}
		if len(r.Sources) != want || !storageEnrollmentNativeRecordValid(r) {
			t.Fatal("complete mandatory union/cap changed", goos, len(r.Sources))
		}
		for _, path := range []string{"internal/desk/storage_budget_enrollment_native.go", "internal/desk/storage_backup.go", "internal/desk/storage_budget_roots.go", "internal/desk/storage_budget_enrollment_operands.go", "internal/desk/storage_budget_enrollment_q.go"} {
			bad := r
			bad.Sources = append([]storageEnrollmentNativeSourcePin{}, r.Sources...)
			found := false
			for i, pin := range bad.Sources {
				if pin.Path == path {
					bad.Sources = append(bad.Sources[:i], bad.Sources[i+1:]...)
					found = true
					break
				}
			}
			if !found {
				t.Fatal("mandatory dependency source missing", path)
			}
			raw, err := json.Marshal(bad.Sources)
			if err != nil {
				t.Fatal(err)
			}
			bad.Implementation = storageEnrollmentPeakHash(storageEnrollmentNativeImplementationDomain(3), raw)
			if storageEnrollmentNativeRecordValid(bad) {
				t.Fatal("missing complete dependency accepted", goos, path)
			}
		}
	}
}
