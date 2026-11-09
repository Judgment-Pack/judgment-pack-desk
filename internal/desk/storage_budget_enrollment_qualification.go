package desk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"runtime"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

var errStorageEnrollmentQualification = errors.New("storage enrollment native profile is not qualified")

// This immutable, release-owned input is deliberately separate from advisory
// profile hashes. No complete five-predicate native record has been qualified
// yet. A release may install records only after the exact source/build/profile
// and attached evidence are verified by the existing release review/checks.
// There is no runtime configuration, registration API or caller-supplied record.
// Empty installation data is a remaining activation gate, not feature success.
type storageEnrollmentNativeSourcePin struct {
	Path   string
	Digest storagebudget.Digest
}

type storageEnrollmentNativePredicateEvidence struct {
	// Complete release evidence bodies, not bare nonzero assertions. Native
	// results corroborate the exact source-derived safety subjects; they cannot
	// establish a universal native allocation or retention ceiling.
	Derivation    storageEnrollmentNativeArtifact
	NativeResults storageEnrollmentNativeArtifact
	BoundarySet   storageEnrollmentNativeArtifact
}

type storageEnrollmentNativeArtifact struct {
	Digest storagebudget.Digest
	Body   []byte
}

func (a storageEnrollmentNativeArtifact) valid() bool {
	return len(a.Body) > 0 && len(a.Body) <= 65536 && sha256.Sum256(a.Body) == a.Digest
}

type storageEnrollmentNativeSystem struct {
	GOOS, GOARCH, Toolchain, KernelRelease, KernelVersion string
}

type storageEnrollmentNativeParentFacts struct {
	Backend                 uint8
	Type, Flags             uint64
	BlockSize, FragmentSize int64
	ProjectFlags            uint32
}

type storageEnrollmentNativeFacts struct {
	System  storageEnrollmentNativeSystem
	Parents [2]storageEnrollmentNativeParentFacts
}

type storageEnrollmentNativeRecord struct {
	Version        uint16
	CapacityPolicy uint8 `json:",omitempty"`
	Facts          storageEnrollmentNativeFacts
	Sources        []storageEnrollmentNativeSourcePin
	Implementation storagebudget.Digest
	// Five entries in accepted bit order. No boolean or aggregate hash may
	// substitute for an entry or its independently verified source/results.
	// Record3 keeps mask31 and all five independently reviewed subjects:
	// 0 old-reader logical/state/death compatibility and fresh headroom entry;
	// 1 exact original O_EXCL/native capture and custody;
	// 2 checked successful arm/reference durability before Q, without fallback;
	// 3 completed application I/O and exclusion before another bounded pass;
	// 4 actual errors/shorts/ENOSPC preserve valid originals, debt and no replay.
	// Native allocation/metadata/snapshot retention is observed uncertainty,
	// never a universal <=P theorem under this distinct policy identity.
	Predicates [5]storageEnrollmentNativePredicateEvidence
	Creation   []storageEnrollmentNativeCreationProfile `json:",omitempty"`
}

// Optional immutable feature evidence in the same release-owned record. Nil
// preserves old JSON exactly and cannot qualify directory/marker/location
// effects. An old strict reader refuses a record carrying this new field.
// Actual original parent observations must match each ordered parent fact.
type storageEnrollmentNativeCreationProfile struct {
	Kind, Branch byte
	Parents      []storageEnrollmentNativeParentFacts
	Predicates   [5]storageEnrollmentNativePredicateEvidence
}

func storageEnrollmentCreationProfileValid(p storageEnrollmentNativeCreationProfile) bool {
	return storageEnrollmentCreationProfileValidVersion(p, 2)
}

func storageEnrollmentCreationProfileValidVersion(p storageEnrollmentNativeCreationProfile, version uint16) bool {
	if p.Kind < storageCreationPrivate || p.Kind > storageCreationAncestor || p.Branch < storageCreationConfigured || p.Branch > storageCreationPersonal || p.Kind == storageCreationPrivate && p.Branch != storageCreationContained || p.Kind == storageCreationAncestor && p.Branch != storageCreationPersonal || len(p.Parents) < 1 || len(p.Parents) > 2 {
		return false
	}
	for _, facts := range p.Parents {
		if !storageEnrollmentNativeParentFactsValid(facts, func() string {
			if facts.Backend == 2 {
				return "darwin"
			}
			return "linux"
		}(), version) {
			return false
		}
	}
	for _, evidence := range p.Predicates {
		if !evidence.Derivation.valid() || !evidence.NativeResults.valid() || !evidence.BoundarySet.valid() {
			return false
		}
	}
	return true
}

// One member of the original enrollment invocation; no independent retain,
// registration, recovered handle, syscall callback or mutable "qualified" bit.
type storageEnrollmentQualification struct {
	self       *storageEnrollmentQualification
	phase      *storageEnrollmentEpoch
	generation uint64
	record     storageEnrollmentNativeRecord
	// Exact live observations from the original qualifying invocation. Release
	// applicability does not replace this immutable currentness comparison.
	capturedFacts storageEnrollmentNativeFacts
	digest        storagebudget.Digest
}

func storageEnrollmentNativeRecordValid(r storageEnrollmentNativeRecord) bool {
	maximumSources := 64
	if r.Version == 3 {
		maximumSources = 98
	}
	if r.Version != 1 && r.Version != 2 && r.Version != 3 || r.Version == 3 && r.CapacityPolicy != storageEnrollmentCapacityEstimatePolicy || r.Version != 3 && r.CapacityPolicy != 0 || len(r.Sources) == 0 || len(r.Sources) > maximumSources || r.Implementation == (storagebudget.Digest{}) {
		return false
	}
	s := r.Facts.System
	if !storageEnrollmentNativeSystemABI(s, r.Version) {
		return false
	}
	for _, p := range r.Facts.Parents {
		if !storageEnrollmentNativeParentFactsValid(p, s.GOOS, r.Version) {
			return false
		}
	}
	for _, e := range r.Predicates {
		if !e.Derivation.valid() || !e.NativeResults.valid() || !e.BoundarySet.valid() {
			return false
		}
	}
	if len(r.Creation) > 5 {
		return false
	}
	for i, profile := range r.Creation {
		for _, facts := range profile.Parents {
			if !storageEnrollmentNativeParentFactsValid(facts, s.GOOS, r.Version) {
				return false
			}
		}
		if !storageEnrollmentCreationProfileValidVersion(profile, r.Version) || r.Version >= 2 && profile.Kind == storageCreationPrivate && len(profile.Parents) != 1 || i > 0 && (r.Creation[i-1].Kind > profile.Kind || r.Creation[i-1].Kind == profile.Kind && r.Creation[i-1].Branch >= profile.Branch) {
			return false
		}
	}
	// Source manifest content is checked here, and its correspondence to the
	// actual build is a required release check. A running executable cannot
	// establish its own source identity by hashing a self-authored string.
	required := map[string]bool{
		"internal/storagebudget/io.go":                             false,
		"internal/storagebudget/coordinator.go":                    false,
		"internal/desk/storage_budget_enrollment_epoch.go":         false,
		"internal/desk/storage_budget_enrollment_native.go":        false,
		"internal/desk/storage_budget_enrollment_qualification.go": false,

		"internal/desk/storage_budget_enrollment_capture.go":     false,
		"internal/desk/storage_budget_enrollment_reservation.go": false,
		"internal/desk/storage_budget_enrollment_initial.go":     false,
	}
	if len(r.Creation) > 0 || r.Version >= 2 {
		for _, path := range []string{"internal/storagebudget/types.go", "internal/storagebudget/codec.go", "internal/desk/storage_budget_enrollment_creation.go", "internal/desk/storage_budget_enrollment_creation_bridge.go", "internal/desk/storage_budget_enrollment_demand.go", "internal/desk/storage_budget_enrollment_ancestry.go", "internal/desk/storage_budget_enrollment_packet.go", "internal/desk/storage_budget_enrollment_plan_scope.go", "internal/desk/storage_budget_enrollment_project_plan.go", "internal/desk/storage_budget_enrollment_peak_source.go", "internal/desk/storage_budget_enrollment_peak_board.go", "internal/desk/storage_budget_enrollment_peak_joins.go", "internal/desk/storage_budget_enrollment_peak_evidence.go", "internal/desk/storage_budget_enrollment_peak_compare.go", "internal/desk/storage_budget_enrollment_peak_peer.go", "internal/desk/storage_budget_enrollment_recovery.go", "internal/desk/storage_budget_enrollment_census.go", "internal/desk/storage_budget_enrollment_scope_sources.go", "internal/desk/storage_budget_enrollment_bindings.go", "internal/desk/storage_budget_enrollment_coverage.go", "internal/desk/storage_budget_sources.go", "internal/desk/storage_budget_census.go", "internal/desk/storage_budget_roots.go", "internal/desk/storage_budget_owner_bindings.go", "internal/desk/storage_budget_owner_lifecycle.go", "internal/desk/storage_budget_owner_catalog_capacity.go", "internal/desk/storage_budget_admission_lifetime.go", "internal/desk/storage_budget_anchor.go", "internal/desk/storage_budget_native.go", "internal/desk/storage_budget_native_linux.go", "internal/desk/storage_budget_native_object.go", "internal/desk/storage_budget_native_ext4_linux_amd64.go", "internal/desk/private_data.go", "internal/desk/handover.go", "internal/desk/review.go", "internal/desk/upgrade.go", "internal/desk/project_lock.go", "internal/desk/dialect_draft_coordination.go", "internal/desk/assistant_dialect_artifact_owners.go", "internal/desk/assistant_dialect_artifact_project_move_recovery.go", "internal/desk/project_move.go", "internal/desk/storage.go", "internal/desk/storage_backup.go", "internal/desk/conversations.go"} {
			required[path] = false
		}
	}
	if r.Version >= 2 {
		for _, path := range []string{"internal/desk/storage_budget_owner_catalog_v5.go", "internal/desk/storage_budget_owner_catalog_v5_validate.go", "internal/desk/storage_budget_owner_catalog_v5_codec.go", "internal/desk/storage_budget_owner_catalog_v5_effects.go", "internal/desk/storage_budget_owner_catalog_v5_relations.go", "internal/desk/storage_budget_owner_catalog_v5_capacity.go", "internal/desk/storage_budget_owner_catalog_v5_chain.go", "internal/desk/storage_budget_identity_intent.go", "internal/desk/storage_budget_identity_intent_material.go", "internal/desk/server.go", "internal/desk/startup_identity.go"} {
			required[path] = false
		}
	}
	platformSource := "internal/desk/storage_budget_enrollment_native_linux_amd64.go"
	if s.GOOS == "darwin" {
		platformSource = "internal/desk/storage_budget_enrollment_native_darwin.go"
	}
	required[platformSource] = false
	if r.Version == 3 {
		// These exact files implement the whole-transfer native/control boundary.
		// Record3 also pins pure decode operands and the actual arm/Q dispatch.
		// The complete unions are 93 Linux pins and 98 Darwin pins.
		for _, path := range []string{"internal/desk/storage_budget_transfer_intent.go", "internal/desk/storage_transfer_lifetime.go", "internal/desk/storage_budget_owner_catalog_json.go", "internal/desk/storage_budget_enrollment_peak_projection.go", "internal/desk/storage_budget_infrastructure.go", "internal/desk/storage_budget_owner_catalog_objects.go", "internal/desk/storage_budget_owner_store.go", "internal/desk/storage_budget_startup.go"} {
			required[path] = false
		}
		for _, path := range []string{"internal/desk/ai_memory.go", "internal/desk/attachments.go", "internal/storagebudget/fanout.go", "internal/storagebudget/accounting.go", "internal/storagebudget/recovery.go", "internal/storagebudget/host_state.go", "internal/storagebudget/enrollment_inspection.go", "internal/desk/storage_budget_enrollment_peak_q.go", "internal/desk/storage_budget_enrollment_peak_data.go", "internal/desk/storage_budget_enrollment_q_fanout.go", "internal/desk/storage_budget_writer.go", "internal/desk/storage_budget_single_file_fanout.go", "internal/desk/storage_budget_owner_catalog_v4_material.go"} {
			required[path] = false
		}
		required["internal/desk/storage_budget_enrollment_operands.go"] = false
		required["internal/desk/storage_budget_enrollment_q.go"] = false
		required["internal/storagebudget/sync_other.go"] = false
		if s.GOOS == "darwin" {
			required["internal/storagebudget/sync_darwin.go"] = false
		}
		for _, path := range []string{"internal/desk/storage_budget_enrollment_peak_scope.go", "internal/desk/storage_budget_enrollment_peak_peer.go", "internal/desk/storage_budget_enrollment_peak_evidence.go", "internal/desk/storage_budget_enrollment_peak_joins.go", "internal/desk/storage_budget_enrollment_peak_catalog.go", "internal/desk/storage_budget_enrollment_peak_dispatch.go", "internal/desk/storage_budget_enrollment_peak_wire.go", "internal/desk/storage_budget_enrollment_peak_compare.go", "internal/desk/storage_budget_intent.go"} {
			required[path] = false
		}
		if s.GOOS == "darwin" {
			for _, path := range []string{"internal/desk/storage_budget_native_darwin.go", "internal/desk/storage_budget_native_darwin_cgo.go", "internal/desk/storage_budget_native_darwin_records.go", "internal/desk/storage_budget_observer_darwin.go"} {
				required[path] = false
			}
		}
	}
	for i, p := range r.Sources {
		if p.Path == "" || len(p.Path) > 4096 || !storageOwnerRelative([]byte(p.Path)) || p.Digest == (storagebudget.Digest{}) || i > 0 && r.Sources[i-1].Path >= p.Path {
			return false
		}
		if _, ok := required[p.Path]; ok {
			required[p.Path] = true
		}
	}
	for _, present := range required {
		if !present {
			return false
		}
	}
	raw, err := json.Marshal(r.Sources)
	return err == nil && storageEnrollmentPeakHash(storageEnrollmentNativeImplementationDomain(r.Version), raw) == r.Implementation
}

// Pure data validation only. It cannot mint an original qualification member.
func storageEnrollmentNativeRecords(raw string) ([]storageEnrollmentNativeRecord, error) {
	if len(raw) > 1<<20 {
		return nil, storagebudget.ErrBounds
	}
	d := json.NewDecoder(bytes.NewBufferString(raw))
	d.DisallowUnknownFields()
	var records []storageEnrollmentNativeRecord
	if err := d.Decode(&records); err != nil || records == nil || len(records) > 16 {
		return nil, errStorageEnrollmentQualification
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, errStorageEnrollmentQualification
	}
	canonical, err := json.Marshal(records)
	if err != nil || !bytes.Equal([]byte(raw), canonical) {
		return nil, errStorageEnrollmentQualification
	}
	for i, r := range records {
		if !storageEnrollmentNativeRecordValid(r) {
			return nil, errStorageEnrollmentQualification
		}
		for _, old := range records[:i] {
			// Kernel strings are Record3 provenance, so records differing only in
			// those strings overlap. Refuse ambiguity rather than selecting the
			// first report for the same supported ABI/parent cohort.
			if old.Version == 3 && r.Version == 3 && storageEnrollmentNativeRecordApplies(old, r.Facts) || old.Facts == r.Facts && (old.Version == r.Version || old.Version < 3 && r.Version < 3) {
				return nil, errStorageEnrollmentQualification
			}
		}
	}
	return records, nil
}

func (p *storageEnrollmentEpoch) qualificationFacts(ctx context.Context) (storageEnrollmentNativeFacts, error) {
	var facts storageEnrollmentNativeFacts
	if err := p.held(ctx); err != nil {
		return facts, err
	}
	system, err := storageEnrollmentNativeSystemFacts(ctx)
	if err != nil {
		return facts, err
	}
	system.GOOS, system.GOARCH, system.Toolchain = runtime.GOOS, runtime.GOARCH, runtime.Version()
	facts.System = system
	for i, parent := range []*os.Root{p.c.original.config, p.c.anchor} {
		f, e := parent.Open(".")
		if e != nil {
			return storageEnrollmentNativeFacts{}, e
		}
		facts.Parents[i], e = storageEnrollmentNativeParentObservation(ctx, f)
		e = errors.Join(e, f.Close())
		if e != nil {
			return storageEnrollmentNativeFacts{}, e
		}
	}
	if err = p.held(ctx); err != nil {
		return storageEnrollmentNativeFacts{}, err
	}
	return facts, nil
}

func (p *storageEnrollmentEpoch) qualifyOriginalNative(ctx context.Context) error {
	if p == nil || p.self != p || p.qualified != nil {
		return storagebudget.ErrAuthority
	}
	if err := p.held(ctx); err != nil {
		return err
	}
	records, err := storageEnrollmentNativeRecords(storageEnrollmentInstalledNativeRecords)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return errStorageEnrollmentQualification
	}
	facts, err := p.qualificationFacts(ctx)
	if err != nil {
		return err
	}
	for _, r := range records {
		// Old records remain inspectable, but cannot qualify revised semantics.
		if !storageEnrollmentNativeRecordApplies(r, facts) {
			continue
		}
		// Read-only original length/custody checks. No density, reclamation or
		// allocation ceiling is inferred for this versioned estimate policy.
		for _, name := range []string{"storage-owner.a", "storage-owner.b", "checkpoint.a", "checkpoint.b"} {
			var n storageNamedFile
			if name == "checkpoint.a" || name == "checkpoint.b" {
				n, err = p.openEnrollmentCheckpoint(ctx, name)
			} else {
				n, err = storageOpenOriginalControl(p.c.ref.cell, name)
			}
			if err != nil {
				return err
			}
			_, err = n.file.Stat()
			err = errors.Join(err, n.current(), n.file.Close())
			if err != nil {
				return err
			}
		}
		raw, e := json.Marshal(r)
		if e != nil {
			return e
		}
		q := &storageEnrollmentQualification{phase: p, generation: p.generation, record: r, capturedFacts: facts, digest: storageEnrollmentPeakHash(storageEnrollmentNativeProfileDomain(r.Version), raw)}
		q.self = q
		if e = p.held(ctx); e != nil {
			return e
		}
		p.qualified = q
		return nil
	}
	return errStorageEnrollmentQualification
}

func (q *storageEnrollmentQualification) current(ctx context.Context, p *storageEnrollmentEpoch) error {
	if p == nil || p.self != p || q == nil || q.self != q || q.phase != p || p.qualified != q || q.generation != p.generation {
		return storagebudget.ErrAuthority
	}
	if q.record.Version != 3 || q.record.CapacityPolicy != storageEnrollmentCapacityEstimatePolicy {
		return errStorageEnrollmentPeakMigration
	}
	facts, err := p.qualificationFacts(ctx)
	if err != nil || !storageEnrollmentNativeInvocationMatches(q.record, q.capturedFacts, facts) {
		return errors.Join(errStorageEnrollmentQualification, err)
	}
	return nil
}

// No profile is inferred from advisory mapping hashes or matching filesystem
// names. The caller supplies actual held originals; this function grants no
// effect and does not retain a new owner/qualification object.
func (q *storageEnrollmentQualification) creationParents(ctx context.Context, p *storageEnrollmentEpoch, kind, branch byte, roles []byte, parents []*os.File) error {
	if err := q.current(ctx, p); err != nil {
		return err
	}
	if kind == storageCreationLocalPrivate {
		if q.record.Version < 2 {
			return errStorageEnrollmentQualification
		}
		kind = storageCreationPrivate
	}
	if len(parents) < 1 || len(parents) > 2 || len(roles) != len(parents) {
		return storagebudget.ErrBounds
	}
	actual := make([]storageEnrollmentNativeParentFacts, len(parents))
	for i, file := range parents {
		if file == nil {
			return storagebudget.ErrAuthority
		}
		facts, err := storageEnrollmentNativeParentObservation(ctx, file)
		if err != nil {
			return err
		}
		actual[i] = facts
	}
	for _, profile := range q.record.Creation {
		if storageEnrollmentCreationProfileMatchesVersion(q.record.Version, profile, kind, branch, roles, actual) {
			return q.current(ctx, p)
		}
	}
	return errStorageEnrollmentQualification
}

func storageEnrollmentCreationProfileMatches(profile storageEnrollmentNativeCreationProfile, kind, branch byte, actual []storageEnrollmentNativeParentFacts) bool {
	if !storageEnrollmentCreationProfileValid(profile) || profile.Kind != kind || profile.Branch != branch || len(profile.Parents) != len(actual) {
		return false
	}
	for i := range actual {
		if actual[i] != profile.Parents[i] {
			return false
		}
	}
	return true
}

// Version2 Private/Contained is one qualified common cohort. Each actual
// original fact must match; roles/order/arity and distinct native ownership are
// separate mandatory predicates. Version1 keeps its exact ordered matcher.
func storageEnrollmentCreationProfileMatchesVersion(version uint16, profile storageEnrollmentNativeCreationProfile, kind, branch byte, roles []byte, actual []storageEnrollmentNativeParentFacts) bool {
	if version == 1 {
		return storageEnrollmentCreationProfileMatches(profile, kind, branch, actual)
	}
	if version != 2 && version != 3 || !storageEnrollmentCreationProfileValidVersion(profile, version) || profile.Kind != kind || profile.Branch != branch || len(roles) != len(actual) {
		return false
	}
	if kind != storageCreationPrivate {
		if len(profile.Parents) != len(actual) {
			return false
		}
		for i := range actual {
			if actual[i] != profile.Parents[i] {
				return false
			}
		}
		return true
	}
	if branch != storageCreationContained || len(profile.Parents) != 1 || !(len(roles) == 1 && (roles[0] == 1 || roles[0] == 2) || len(roles) == 2 && roles[0] == 2 && roles[1] == 3) {
		return false
	}
	for _, facts := range actual {
		if facts != profile.Parents[0] {
			return false
		}
	}
	return true
}

func storageCreationOriginalRoles(record storageCreationIntent) []byte {
	roles := make([]byte, len(record.Parents))
	for i, parent := range record.Parents {
		roles[i] = parent.Role
	}
	return roles
}

// A newly captured private/draft directory is observed again before becoming
// any descendant's effect parent. Numeric matching alone grants no custody;
// the caller still owns the exact captured object and original parent graph.
func (q *storageEnrollmentQualification) privateEffectParent(ctx context.Context, p *storageEnrollmentEpoch, kind byte, file *os.File) error {
	if err := q.current(ctx, p); err != nil {
		return err
	}
	if q.record.Version < 2 || kind != storageCreationPrivate && kind != storageCreationLocalPrivate {
		return nil
	}
	facts, err := storageEnrollmentNativeParentObservation(ctx, file)
	if err != nil {
		return err
	}
	for _, profile := range q.record.Creation {
		if storageEnrollmentCreationProfileMatchesVersion(q.record.Version, profile, storageCreationPrivate, storageCreationContained, []byte{2}, []storageEnrollmentNativeParentFacts{facts}) {
			return q.current(ctx, p)
		}
	}
	return errStorageEnrollmentQualification
}

// These domains preserve all legacy digests. A version change never relabels
// existing source/evidence bytes or installs them as qualified native evidence.
func storageEnrollmentNativeImplementationDomain(version uint16) string {
	if version == 3 {
		return "desk-enrollment-native-implementation-capacity-estimate-v1"
	}
	return "desk-enrollment-native-implementation-v1"
}

func storageEnrollmentNativeProfileDomain(version uint16) string {
	if version == 3 {
		return "desk-enrollment-native-profile-capacity-estimate-v1"
	}
	return "desk-enrollment-native-profile-v1"
}

func storageEnrollmentNativeParentFactsValid(p storageEnrollmentNativeParentFacts, goos string, version uint16) bool {
	if goos == "linux" {
		return p.Backend == 1 && p.Type == 0xef53 && p.BlockSize == 4096 && p.FragmentSize == 4096
	}
	return version == 3 && goos == "darwin" && p.Backend == 2 && p.Type == 26 && p.BlockSize > 0 && p.BlockSize <= 1<<20 && p.FragmentSize == 0 && p.ProjectFlags == 0
}

// The supported native ABI is selected by OS/architecture, not a kernel build
// string. Linux uses the amd64 public syscall/UAPI leaves; Darwin uses the
// pinned libSystem calls and SDK-asserted layouts on amd64/arm64. Unsupported
// requests, returned masks and checked barriers still fail at their actual
// leaves. This predicate neither qualifies native behavior nor issues custody.
func storageEnrollmentNativeSystemABI(s storageEnrollmentNativeSystem, version uint16) bool {
	if !storageOwnerText(s.GOOS, 16, false) || !storageOwnerText(s.GOARCH, 16, false) || !storageOwnerText(s.Toolchain, 64, false) || !storageOwnerText(s.KernelRelease, 256, false) || !storageOwnerText(s.KernelVersion, 256, false) {
		return false
	}
	return s.GOOS == "linux" && s.GOARCH == "amd64" || version == 3 && s.GOOS == "darwin" && (s.GOARCH == "arm64" || s.GOARCH == "amd64")
}

// Pure applicability comparison for unpublished Record3. Full kernel strings
// remain bounded provenance in the complete hashed record; no guessed kernel
// range or wildcard parent feature is introduced. Exact toolchain, all parent
// geometry/flags, ordered roles and separate live native custody remain needed.
func storageEnrollmentNativeRecordApplies(r storageEnrollmentNativeRecord, actual storageEnrollmentNativeFacts) bool {
	if r.Version != 3 || r.CapacityPolicy != storageEnrollmentCapacityEstimatePolicy || !storageEnrollmentNativeSystemABI(r.Facts.System, 3) || !storageEnrollmentNativeSystemABI(actual.System, 3) {
		return false
	}
	if r.Facts.System.GOOS != actual.System.GOOS || r.Facts.System.GOARCH != actual.System.GOARCH || r.Facts.System.Toolchain != actual.System.Toolchain || r.Facts.Parents != actual.Parents {
		return false
	}
	for _, parent := range actual.Parents {
		if !storageEnrollmentNativeParentFactsValid(parent, actual.System.GOOS, 3) {
			return false
		}
	}
	return true
}

// A different supported kernel report can apply to the release record, but
// every observation in one original invocation must match its exact capture.
func storageEnrollmentNativeInvocationMatches(r storageEnrollmentNativeRecord, captured, current storageEnrollmentNativeFacts) bool {
	return captured == current && storageEnrollmentNativeRecordApplies(r, captured)
}
