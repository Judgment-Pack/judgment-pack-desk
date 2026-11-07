package desk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Declaration order is the complete publication-evidence JSON operand. It is
// marshaled by storageOwnerDigest without tags/maps/omitempty/normalization.
// Paths are absent from the native observations. Locators bind the unchanged
// accounting mapping only; neither they nor decoded tuples create custody.
type storageIdentityPublicationMaterialV1 struct {
	Version              uint32
	PlanDigest           storagebudget.Digest
	Action               uint8
	OperationID          storagebudget.ID
	CASBinding           storagebudget.Digest
	AdmissionEpoch       uint64
	PolicyRevision       uint64
	StagingToken         storagebudget.Token
	Components           []storagebudget.Component
	RootIdentities       []storagebudget.Digest
	FilesystemIdentities []storagebudget.Digest
	Locators             []string
	Parent               storageOwnerPersistentObject
	AllocatedStage       storageOwnerPersistentObject
	LinkedStage          storageOwnerPersistentObject
	Target               storageOwnerPersistentObject
	ExpectedOld          storageOwnerPersistentObject
	Presence             uint16
	AllocatedLinks       uint64
	LinkedStageLinks     uint64
	LinkedTargetLinks    uint64
	FinalTargetLinks     uint64
	PayloadDigest        storagebudget.Digest
	PayloadLength        uint64
	Facts                uint32
	Outcome              storageIntentOutcome
}

func storageIdentityPublicationMaterial(r storageIdentityIntent) storageIdentityPublicationMaterialV1 {
	c := r.Core
	token := c.Token
	if c.Phase == storageIntentSettled || c.Phase == storageIntentRetired {
		token = c.PreviousToken
	}
	outcome := storageIntentPublished
	if r.NativePhase == storageIdentityAbortObserved {
		outcome = storageIntentAborted
	}
	version := uint32(1)
	if r.Action != storageIdentityIntentCreate {
		version = 2
	}
	return storageIdentityPublicationMaterialV1{
		Version: version, PlanDigest: r.PlanDigest, Action: r.Action, OperationID: c.OperationID, CASBinding: c.CASBinding, AdmissionEpoch: c.AdmissionEpoch, PolicyRevision: c.PolicyRevision, StagingToken: token,
		Components: c.Components, RootIdentities: c.RootIdentities, FilesystemIdentities: c.FilesystemIdentities, Locators: c.Locators,
		Parent: r.Parent, AllocatedStage: r.AllocatedStage, LinkedStage: r.LinkedStage, Target: r.Target, ExpectedOld: r.ExpectedOld, Presence: r.Presence,
		AllocatedLinks: r.AllocatedLinks, LinkedStageLinks: r.LinkedStageLinks, LinkedTargetLinks: r.LinkedTargetLinks, FinalTargetLinks: r.FinalTargetLinks,
		PayloadDigest: r.PayloadDigest, PayloadLength: c.NewLength, Facts: r.Facts, Outcome: outcome,
	}
}
func storageIdentityPublicationDigest(r storageIdentityIntent) (storagebudget.Digest, error) {
	if r.validate() != nil || r.NativePhase != storageIdentityTargetDurable && r.NativePhase != storageIdentityAbortObserved {
		return storagebudget.Digest{}, storagebudget.ErrBounds
	}
	domain := "desk-storage-identity-publication-v1"
	if r.Action != storageIdentityIntentCreate {
		domain = "desk-storage-identity-action-publication-v2"
	}
	return storageOwnerDigest(domain, storageIdentityPublicationMaterial(r))
}

// Explicit D6 v3 extension. All 16384 supplied, validated selected-page bytes
// (including populated checksum, native extension, reserved zeros and previous
// digest) are hashed unchanged, with no domain prefix. This does not select a
// bank or attest that any original native/Q fact actually happened.
func storageIdentityIntentDigest(raw []byte, slot int, installation storagebudget.ID, binding storagebudget.Digest, expected storageIdentityIntent) (storagebudget.Digest, error) {
	got, err := storageDecodeIdentityIntent(raw, slot, installation, binding)
	if err != nil || got.Core.Phase != storageIntentRetired || got.Core.Outcome != storageIntentPublished || got.NativePhase != storageIdentityTargetDurable || !reflect.DeepEqual(got, expected) {
		return storagebudget.Digest{}, storagebudget.ErrBounds
	}
	return sha256.Sum256(raw), nil
}

// D8 remains raw SHA256 of exactly authorized canonical identity bytes including
// terminal LF. A digest comparison supplies no publication or actual P custody.
func storageIdentityIntentPayloadOperand(r storageIdentityIntent, raw []byte) (storagebudget.Digest, error) {
	if r.validate() != nil || len(raw) == 0 || len(raw) > 512 || uint64(len(raw)) != r.Core.NewLength {
		return storagebudget.Digest{}, storagebudget.ErrBounds
	}
	if _, err := parseIdentity(raw); err != nil {
		return storagebudget.Digest{}, storagebudget.ErrBounds
	}
	d := sha256.Sum256(raw)
	if d != r.PayloadDigest {
		return storagebudget.Digest{}, storagebudget.ErrBounds
	}
	return d, nil
}

// These are detached exact metadata comparisons, never Q inspection/custody or
// permission to reissue Reserve. Prepared Reserve uncertainty is deliberately
// not classified as admitted by this function. Rejection uses independent
// original epoch/no-matching-ticket evidence; no rejection ticket is invented.
func storageIdentityIntentTicketMapping(r storageIdentityIntent, t storagebudget.Ticket, binding storagebudget.Digest) bool {
	c := r.Core
	if r.validate() != nil || binding == (storagebudget.Digest{}) || !storageIntentMatchingTicket(c, t, binding) || c.PolicyRevision != t.PolicyRevision {
		return false
	}
	switch c.Phase {
	case storageIntentReserved:
		return t.State == storagebudget.Reserved && t.Token == c.Token || t.State == storagebudget.Staging && storageIntentNextToken(c.Token, t.Token)
	case storageIntentStaging, storageIntentPublishing:
		return t.State == storagebudget.Staging && t.Token == c.Token
	case storageIntentSettling:
		return t.State == storagebudget.Staging && t.Token == c.Token || t.State == storagebudget.Terminal && storageIntentNextToken(c.Token, t.Token) && t.Evidence == c.Evidence && reflect.DeepEqual(t.Changes, c.Changes)
	case storageIntentSettled, storageIntentRetired:
		return t.State == storagebudget.Terminal && t.Token == c.Token && t.Evidence == c.Evidence && reflect.DeepEqual(t.Changes, c.Changes)
	}
	return false
}
func storageIdentityIntentExactRetired(r storageIdentityIntent, status storagebudget.HostState, binding storagebudget.Digest) bool {
	c := r.Core
	if c.Phase != storageIntentRetired || !storageIntentToken(c.Token) || status.Epoch != c.RetirementEpoch {
		return false
	}
	s := status.Slots[c.Token.Slot]
	return storageIdentityIntentTicketMapping(r, s.Ticket, binding) && s.PersistedApplied == c.Token && s.PersistedNextSequence > c.Token.Sequence
}

type storageIdentityIntentRecoveryCut uint8

const (
	storageIdentityRecoveryUnknown storageIdentityIntentRecoveryCut = iota
	storageIdentityRecoveryNeedsStageProof
	storageIdentityRecoveryNeedsPublicationProof
	storageIdentityRecoveryNeedsAbortCompletion
)

// This only classifies requirements, never a successful recovery. Presence never proves a matching
// native object. In particular durable allocation + absent name is unknown:
// parent persistence may have lost the name, or an unobserved removal happened.
// Positive native continuation still requires a separate qualified validator.
func storageIdentityIntentRecoveryBoundary(r storageIdentityIntent, stageNamePresent bool) storageIdentityIntentRecoveryCut {
	if r.validate() != nil {
		return storageIdentityRecoveryUnknown
	}
	switch r.NativePhase {
	case storageIdentityAllocatedObserved, storageIdentityPayloadReady, storageIdentityAbortPrepared:
		if stageNamePresent {
			return storageIdentityRecoveryNeedsStageProof
		}
	case storageIdentityLinkedObserved, storageIdentityTargetDurable:
		return storageIdentityRecoveryNeedsPublicationProof
	case storageIdentityAbortObserved:
		if !stageNamePresent {
			return storageIdentityRecoveryNeedsAbortCompletion
		}
	}
	return storageIdentityRecoveryUnknown
}

// The launch accumulator belongs to the original construction, across its
// sequential epochs. Its operands are obligations, not reusable permits. A
// failed/unknown group remains spent and charged until this launch is closed.
// Catalog keys deliberately do not include PlanID: duplicate/reopen are shared
// once TOTAL, as are the existing bounded P/C/S/F catalog operands.
type storageIdentityLaunchKey struct {
	Plan            storagebudget.ID
	Action, Attempt byte
	Group           uint16
	Catalog         string
	Parent          storageOwnerPersistentObject
	Target          string
	NativeTarget    storageOwnerPersistentObject
}
type storageIdentityLaunchGroup struct {
	key                                 storageIdentityLaunchKey
	parent                              *os.Root
	bytes, files, directories, metadata uint64
	spent                               bool
}
type storageIdentityLaunchApplication struct {
	// Fixed comparison operands retained only on this existing live board.
	// They neither reconstruct a plan nor grant permission to repeat effects.
	originalPlan      storagebudget.Digest
	initial           storageCompositionInitial
	plan              storagebudget.ID
	action            byte
	components        []storagebudget.Component
	settled, complete bool // factual Q incorporation and durable completion, original vectors stay immutable
}
type storageIdentityEnrollmentBoard struct {
	bridge   storagebudget.ID
	peaks    [2]storagebudget.CapacityVector
	parents  [2]*os.Root
	natives  [2]storageOwnerPersistentObject
	logical  storagebudget.Vector
	complete bool
	scope    *storageEnrollmentPeakScope // exact joined scope, not a replacement for its retained tail
}
type storageIdentityLaunch struct {
	catalogsKnown bool
	catalogs      [2]storagebudget.Digest
	applications  []storageIdentityLaunchApplication
	enrollments   []storageIdentityEnrollmentBoard
	self          *storageIdentityLaunch
	construction  *storageBudgetConstruction
	groups        []storageIdentityLaunchGroup
	components    []storagebudget.Component
	peak          storagebudget.CapacityVector
	debt          storagebudget.Vector
	failed        error
}

func storageIdentityAddCapacity(a, b storagebudget.CapacityVector) (storagebudget.CapacityVector, error) {
	if a.Bytes > 1<<60 || b.Bytes > 1<<60-a.Bytes || a.Inodes > 1<<40 || b.Inodes > 1<<40-a.Inodes {
		return storagebudget.CapacityVector{}, storagebudget.ErrBounds
	}
	return storagebudget.CapacityVector{Bytes: a.Bytes + b.Bytes, Inodes: a.Inodes + b.Inodes}, nil
}
func storageIdentityAddVector(a, b storagebudget.Vector) (storagebudget.Vector, error) {
	if a.Bytes > 1<<60 || b.Bytes > 1<<60-a.Bytes || a.Files > 1<<40 || b.Files > 1<<40-a.Files || a.Directories > 1<<40 || b.Directories > 1<<40-a.Directories {
		return storagebudget.Vector{}, storagebudget.ErrBounds
	}
	return storagebudget.Vector{Bytes: a.Bytes + b.Bytes, Files: a.Files + b.Files, Directories: a.Directories + b.Directories}, nil
}

// Merge one application's complete original mappings. Only identical mappings
// alias; a shared root with a distinct referencing workspace remains distinct.
// A retained mapping cannot be dropped merely because its ordinary peak is zero.
// All four logical vectors and every capacity promise are added independently.
func storageIdentityComponentUnion(retained, fresh []storagebudget.Component) ([]storagebudget.Component, error) {
	out := make([]storagebudget.Component, 0, storagebudget.MaxComponents)
	for part, rows := range [][]storagebudget.Component{retained, fresh} {
		for _, c := range rows {
			if c.Root == (storagebudget.ID{}) || c.RootGeneration == 0 || c.Filesystem == (storagebudget.ID{}) || c.FilesystemGeneration == 0 || c.Owner == (storagebudget.Digest{}) || c.Category >= storagebudget.Categories || c.CapacityCount == 0 || int(c.CapacityCount) > storagebudget.MaxCapacityRefs || c.CapacityBinding == (storagebudget.Digest{}) || c.Workspace == (storagebudget.ID{}) && c.WorkspaceGeneration != 0 || c.Workspace != (storagebudget.ID{}) && c.WorkspaceGeneration == 0 {
				return nil, storagebudget.ErrAuthority
			}
			if part == 0 && c.SettlementPhysical == (storagebudget.Vector{}) && c.SettlementAttribution == (storagebudget.Vector{}) {
				return nil, storagebudget.ErrUnknown
			}
			physical, err := storageIdentityAddVector(c.Physical, c.SettlementPhysical)
			if err != nil {
				return nil, err
			}
			if physical == (storagebudget.Vector{}) {
				for _, promise := range c.Capacity {
					if promise.Peak != (storagebudget.CapacityVector{}) {
						return nil, storagebudget.ErrUnknown
					}
				}
			}
			for i := range c.Capacity {
				v := c.Capacity[i]
				if i >= int(c.CapacityCount) {
					if v != (storagebudget.CapacityPromise{}) {
						return nil, storagebudget.ErrBounds
					}
				} else if v.Constraint == (storagebudget.ID{}) || v.Generation == 0 || i > 0 && bytes.Compare(c.Capacity[i-1].Constraint[:], v.Constraint[:]) >= 0 || v.Peak.Bytes > 1<<60 || v.Peak.Inodes > 1<<40 {
					return nil, storagebudget.ErrBounds
				}
			}
			index := -1
			for i, old := range out {
				if old.Root == c.Root && old.Workspace == c.Workspace && old.Category == c.Category {
					a, b := old, c
					a.Physical, a.Attribution, a.SettlementPhysical, a.SettlementAttribution = storagebudget.Vector{}, storagebudget.Vector{}, storagebudget.Vector{}, storagebudget.Vector{}
					b.Physical, b.Attribution, b.SettlementPhysical, b.SettlementAttribution = storagebudget.Vector{}, storagebudget.Vector{}, storagebudget.Vector{}, storagebudget.Vector{}
					for j := range a.Capacity {
						a.Capacity[j].Peak, b.Capacity[j].Peak = storagebudget.CapacityVector{}, storagebudget.CapacityVector{}
					}
					if a != b {
						return nil, storagebudget.ErrAuthority
					}
					index = i
					break
				}
			}
			if index < 0 {
				if len(out) == storagebudget.MaxComponents {
					return nil, storagebudget.ErrBounds
				}
				out = append(out, c)
				continue
			}
			old := &out[index]
			for _, pair := range [][2]*storagebudget.Vector{{&old.Physical, &c.Physical}, {&old.Attribution, &c.Attribution}, {&old.SettlementPhysical, &c.SettlementPhysical}, {&old.SettlementAttribution, &c.SettlementAttribution}} {
				sum, err := storageIdentityAddVector(*pair[0], *pair[1])
				if err != nil {
					return nil, err
				}
				*pair[0] = sum
			}
			for i := 0; i < int(c.CapacityCount); i++ {
				sum, err := storageIdentityAddCapacity(old.Capacity[i].Peak, c.Capacity[i].Peak)
				if err != nil {
					return nil, err
				}
				old.Capacity[i].Peak = sum
			}
		}
	}
	var settlement storagebudget.Vector
	for _, c := range out {
		for _, v := range []storagebudget.Vector{c.Physical, c.Attribution, c.SettlementPhysical, c.SettlementAttribution} {
			if _, err := storageIdentityAddVector(storagebudget.Vector{}, v); err != nil {
				return nil, err
			}
		}
		var err error
		settlement, err = storageIdentityAddVector(settlement, c.SettlementPhysical)
		if err != nil {
			return nil, err
		}
	}
	if settlement.Bytes >= 134217728 {
		return nil, storagebudget.ErrBounds
	}
	return out, nil
}

// Original finite operands only. The caller supplies the already constructed
// complete application component board; this method validates its original
// definitions/custody and debt before installing any launch state. A DTO alone
// cannot cause a group to be taken or any namespace/Q effect.
// Comparison-only input from readOriginalPolicy in this same held epoch.
// Both exact physical images must be the captured original pair before its
// retained lineage can satisfy J receipts. No source is inferred from core or
// equal hashes, and a recovery-only pair needs the original prepared relation.
func storageIdentityLaunchOriginalLineages(actual [2][]byte, state storagePolicyOriginal, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageCompositionLineage, storageCompositionLineage, error) {
	empty := storageCompositionEmptyLineage()
	if !storagePolicyActualPair(actual) || state.Selected > 1 ||
		!bytes.Equal(actual[0], state.Actual[0]) || !bytes.Equal(actual[1], state.Actual[1]) {
		return empty, empty, errStorageCustody
	}
	if state.RecoveryOnly {
		pair, err := storageOwnerV6NativeReadPair(actual[0], actual[1], &state.Catalog, state.Evidence.Raw, record, claim)
		if err != nil || !storagePolicyNativePreparedLineage(pair) || pair.Selected != state.Selected || !reflect.DeepEqual(pair.Banks[pair.Selected].Stage.Lineage, state.Lineage) {
			return empty, empty, errors.Join(errStorageEnrollmentUnresolved, err)
		}
		return state.Lineage, pair.Banks[1-pair.Selected].Stage.Lineage, nil
	}
	return state.Lineage, state.Lineage, nil
}

func (p *storageEnrollmentEpoch) prepareIdentityLaunch(ctx context.Context, plan storageOwnerV5LineagePlan, initial storageCompositionInitial, applications map[byte][]storagebudget.Component) error {
	if p == nil || p.self != p || p.original == nil || p.initialInspection == nil || p.qualified == nil || p.identityLaunch != nil && p.identityLaunch != p.c.identityLaunch || p.scope != nil || p.creation != nil || !p.identityPrefixes {
		return storagebudget.ErrAuthority
	}
	if p.identityPlan == nil || !reflect.DeepEqual(plan, *p.identityPlan) || initial != p.identityInitial || !reflect.DeepEqual(applications, p.identityApplications) {
		return storagebudget.ErrAuthority
	}
	if err := p.held(ctx); err != nil {
		return err
	}
	if p.qualified.record.Version < 2 {
		return errStorageEnrollmentQualification
	}
	record, claim := *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim
	catalogsBefore := p.original.Catalogs
	if p.compositionEntry != nil {
		catalogsBefore = p.compositionEntry.Original
	}
	cores, _, err := p.identityCorePair(catalogsBefore, record, claim)
	if err != nil {
		return err
	}
	var graph storageOwnerCatalogV2
	if string(cores[0][:8]) == "DSKOWN3\x00" {
		pair, e := storageOwnerDecodeCatalogPairV3(cores[0], cores[1], record, claim)
		if e != nil {
			return e
		}
		graph = pair.High
	} else {
		if p.previousEvidence == nil {
			return storagebudget.ErrAuthority
		}
		graph = p.previousEvidence.Inner.Catalog
	}
	lineage := storageCompositionEmptyLineage()
	var nativeDurable *storageCompositionLineage
	if storagePolicyActualPair(catalogsBefore) {
		state, e := p.c.owner.readOriginalPolicy(ctx)
		if e != nil {
			return e
		}
		current, durable, e := storageIdentityLaunchOriginalLineages(catalogsBefore, state, record, claim)
		if e != nil {
			return e
		}
		lineage, nativeDurable = current, &durable
	} else if string(catalogsBefore[0][:8]) == "DSKONL1\x00" {
		pair, e := storageCompositionReadPair(catalogsBefore[0], catalogsBefore[1], record, claim)
		if e != nil || pair.RecoveryOnly && !storageIdentityPreparedLineage(pair) {
			return storagebudget.ErrBounds
		}
		lineage = pair.Banks[pair.Selected].Lineage
	}
	// Adding the original P comparison never edits the retained native/legacy
	// source or the independently selected durable predecessor lineage.
	lineage.Rows.LineagePlans = append([]storageOwnerV5LineagePlan{}, lineage.Rows.LineagePlans...)
	lineage.Order[0] = append([]storagebudget.ID{}, lineage.Order[0]...)
	lineage.Initial = append([]storageCompositionInitial{}, lineage.Initial...)
	retained := false
	for i, id := range lineage.Order[0] {
		if id == plan.PlanID {
			if retained || lineage.Initial[i] != initial {
				return storagebudget.ErrAuthority
			}
			retained = true
		}
	}
	if retained {
		same := false
		for _, old := range lineage.Rows.LineagePlans {
			same = same || reflect.DeepEqual(old, plan)
		}
		if !same {
			return storagebudget.ErrAuthority
		}
	} else {
		lineage.Rows.LineagePlans = append(lineage.Rows.LineagePlans, plan)
		lineage.Order[0] = append(lineage.Order[0], plan.PlanID)
		lineage.Initial = append(lineage.Initial, initial)
	}
	sort.Slice(lineage.Rows.LineagePlans, func(i, j int) bool {
		return bytes.Compare(lineage.Rows.LineagePlans[i].PlanID[:], lineage.Rows.LineagePlans[j].PlanID[:]) < 0
	})
	if err = lineage.validateGraph(graph, record); err != nil {
		return err
	}
	if initial.Mode == 2 {
		if _, err = storageCompositionInitialBinding(plan, initial); err != nil {
			return err
		}
	}
	// A fresh action cannot strand an unrelated original J or omit its debt.
	// Known pending identity continuations are joined by their original member
	// before this fresh-action admission, never recast as an empty slot.
	for slot, pages := range p.original.Hosts {
		choice, e := storageSelectCreationHost(pages[0], pages[1], slot, record.Installation, record.QBinding)
		if e != nil {
			return e
		}
		pending := p.identity != nil && p.identity.recovering && p.identity.self == p.identity && choice.Identity != nil && bytes.Equal(choice.Slot.page, p.identity.selected.page) && reflect.DeepEqual(*choice.Identity, p.identity.record)
		if !choice.Slot.record.reusable() && !pending {
			return errStorageEnrollmentUnresolved
		}
		for _, identity := range []*storageIdentityIntent{choice.Identity, choice.PredecessorIdentity} {
			if identity == nil {
				continue
			}
			if pending && identity == choice.Identity {
				continue
			}
			if e = p.identityComponentMappings(ctx, identity.Core, identity.Core.Components[0].Root); e != nil {
				return e
			}
			raw, e := storageEncodeIdentityIntent(*identity, record.Installation, record.QBinding)
			if e != nil || !p.c.owner.identityReceiptCurrent(ctx, *identity, raw, lineage, storageHostState(p.initialInspection.View)) {
				return errors.Join(errStorageEnrollmentUnresolved, e)
			}
		}
		if choice.Creation != nil && !p.c.owner.creationTailCurrent(ctx, *choice.Creation, choice.Slot.page, storageHostState(p.initialInspection.View)) {
			return errStorageEnrollmentUnresolved
		}
	}
	for _, slot := range p.initialInspection.View.Slots {
		if !storageUnretired(slot) {
			continue
		}
		if p.identity == nil || slot.Ticket.Token != p.identity.admitted || !storageIntentMatchingTicket(p.identity.record.Core, slot.Ticket, record.QBinding) {
			return errStorageEnrollmentUnresolved
		}
	}
	compact, err := storageCompositionCompactCore(cores[0])
	if err != nil {
		return err
	}
	room, err := lineage.closureSize()
	if err != nil || 64+storageCompositionPrefixBytes+len(compact)+room > storageOwnerCatalogBytes {
		return storagebudget.ErrBounds
	}
	if plan.ProjectObject != p.original.Project || plan.ConfigObject != record.Config {
		return errStorageCustody
	}
	var private *storageRootBinding
	for _, r := range p.originalRoots {
		if r.row.ID != plan.PrivateRoot.ID {
			continue
		}
		native, e := storageOwnerPersistentRoot(ctx, r.root)
		if e != nil || native != plan.PrivateObject || private != nil {
			return errors.Join(errStorageCustody, e)
		}
		private = r
	}
	if private == nil {
		return errStorageCustody
	}
	actual, info, found, err := readIdentity(private.root)
	if err != nil {
		return err
	}
	var completed *storageOwnerV5LineageCompletion
	for i := range lineage.Rows.LineageCompletions {
		row := &lineage.Rows.LineageCompletions[i]
		if row.PlanID == plan.PlanID {
			completed = row
		}
	}
	if p.identity != nil && p.identity.recovering {
		if e := p.identity.recoveryNativeCurrent(ctx); e != nil {
			return e
		}
	} else if p.identityDone {
		if complete, e := p.identityCompletedCurrent(ctx); e != nil || !complete {
			return errors.Join(errStorageCustody, e)
		}
	} else if completed != nil {
		if !found || !bytes.Equal(actual.line(), plan.IdentityInitial) {
			return errStorageCustody
		}
		n, e := storageOpenFile(private.root, startupIdentityName, info.Size())
		if e != nil {
			return e
		}
		native, e := storageOwnerPersistentFile(ctx, n)
		e = errors.Join(e, n.file.Close())
		if e != nil || native != completed.Initial.PublishedObject {
			return errors.Join(errStorageCustody, e)
		}
	} else if initial.Mode == 1 {
		if found {
			return errStorageCustody
		}
	} else {
		if !found || !bytes.Equal(actual.line(), plan.IdentityInitial) || actual.Path != p.originalProjectPath() || actual.Folder == "" || actual.Folder != p.originalFolderKey() {
			return errStorageCustody
		}
		n, e := storageOpenFile(private.root, startupIdentityName, info.Size())
		if e != nil {
			return e
		}
		native, e := storageOwnerPersistentFile(ctx, n)
		e = errors.Join(e, n.file.Close())
		if e != nil || native != initial.ExpectedOld {
			return errors.Join(errStorageCustody, e)
		}
	}
	actions, err := storageIdentityRequiredActions(plan, initial)
	if err != nil || len(applications) != len(actions) {
		return storagebudget.ErrBounds
	}
	var union []storagebudget.Component
	for _, action := range actions {
		part, e := storageIdentityComponentUnion(nil, applications[action])
		if e != nil || len(part) == 0 {
			return errors.Join(storagebudget.ErrBounds, e)
		}
		core := storageIntentRecord{Components: part}
		for _, c := range part {
			r := p.c.owner.roots[c.Root]
			if r == nil {
				return errStorageCustody
			}
			core.RootIdentities = append(core.RootIdentities, r.row.Identity)
			core.FilesystemIdentities = append(core.FilesystemIdentities, p.c.owner.filesystems[c.Filesystem].Identity)
			core.Locators = append(core.Locators, r.row.Locator)
		}
		leader := plan.PrivateRoot.ID
		if action == storageIdentityIntentStamp {
			leader = part[0].Root
		}
		if e = p.identityComponentMappings(ctx, core, leader); e != nil {
			return e
		}
		union = append(union, part...)
	}
	for _, c := range union {
		if c.SettlementPhysical == (storagebudget.Vector{}) && c.SettlementAttribution == (storagebudget.Vector{}) {
			return storagebudget.ErrUnknown
		}
	}
	for _, component := range union {
		r := p.c.owner.roots[component.Root]
		if r == nil || component.Owner != r.row.Owner || component.RootGeneration != r.row.Generation || component.Filesystem != r.row.Filesystem || component.FilesystemGeneration != p.c.owner.filesystems[r.row.Filesystem].Generation || component.Category != r.category || component.CapacityCount != r.row.CapacityCount || component.CapacityBinding != r.row.CapacityBinding {
			return errStorageCustody
		}
		if (r.configControl || r.qControl) && component.Workspace != (storagebudget.ID{}) || !r.configControl && !r.qControl && len(r.references) > 0 && component.Workspace == (storagebudget.ID{}) {
			return errStorageCustody
		}
		if component.Workspace != (storagebudget.ID{}) {
			w, ok := p.c.owner.workspaces[component.Workspace]
			if !ok || w.Generation != component.WorkspaceGeneration {
				return errStorageCustody
			}
			linked := false
			for _, id := range r.references {
				linked = linked || id == component.Workspace
			}
			if !linked {
				return errStorageCustody
			}
		}
		for i := 0; i < int(component.CapacityCount); i++ {
			if component.Capacity[i].Constraint != r.row.CapacityRefs[i].Constraint || component.Capacity[i].Generation != r.row.CapacityRefs[i].Generation {
				return errStorageCustody
			}
		}
	}
	for _, component := range union {
		r := p.c.owner.roots[component.Root]
		refs := r.references
		if (r.configControl || r.qControl) && component.Category == storagebudget.Control {
			refs = nil
		}
		if len(refs) == 0 {
			refs = []storagebudget.ID{{}}
		}
		for _, ref := range refs {
			found := false
			for _, peer := range union {
				found = found || peer.Root == component.Root && peer.Category == component.Category && peer.Workspace == ref
			}
			if !found {
				return errStorageCustody
			}
		}
	}
	launch := p.c.identityLaunch
	if launch == nil {
		launch = &storageIdentityLaunch{construction: p.c}
		launch.self = launch
	}
	if launch.self != launch || launch.construction != p.c || launch.failed != nil {
		return errors.Join(storagebudget.ErrAuthority, launch.failed)
	}
	for _, g := range launch.groups {
		if g.key.Plan == plan.PlanID {
			// A completed original may be requested again after ordinary work.
			// Reuse only its exact original, fully discharged applications;
			// never append/reset a group, promise, spend or settlement here.
			if !p.identityDone || p.identity != nil || !launch.completedOriginalPlan(plan, initial, applications) {
				return storagebudget.ErrBounds
			}
			if err = p.checkIdentityLaunch(ctx, launch); err != nil {
				return err
			}
			p.identityLaunch = launch
			return p.held(ctx)
		}
	}
	// Build on a detached copy: a refused shape never partially installs a board.
	next := *launch
	next.groups = append([]storageIdentityLaunchGroup{}, launch.groups...)
	next.components = append(append([]storagebudget.Component{}, launch.components...), union...)
	next.applications = append([]storageIdentityLaunchApplication{}, launch.applications...)
	durableLineage := lineage
	if nativeDurable != nil {
		durableLineage = *nativeDurable
	} else if string(catalogsBefore[0][:8]) == "DSKONL1\x00" {
		pair, e := storageCompositionReadPair(catalogsBefore[0], catalogsBefore[1], record, claim)
		if e != nil {
			return e
		}
		if storageIdentityPreparedLineage(pair) {
			durableLineage = pair.Banks[1-pair.Selected].Lineage
		}
	}
	for _, action := range actions {
		complete := false
		if action == storageIdentityIntentCreate || action == storageIdentityIntentInitialCAS {
			for _, c := range durableLineage.Rows.LineageCompletions {
				complete = complete || c.PlanID == plan.PlanID
			}
		}
		if action == storageIdentityIntentStamp {
			for _, row := range durableLineage.Rows.LineageStampCompletions {
				complete = complete || row.PlanID == plan.PlanID
			}
		}
		if action == storageIdentityIntentClear {
			for _, f := range durableLineage.Rows.LineageContinuations {
				complete = complete || f.PlanID == plan.PlanID
			}
		}
		application, e := p.originalIdentityApplication(ctx, plan.PlanID, action, applications[action], complete)
		if e != nil {
			return e
		}
		application.originalPlan, e = storageOwnerV5PlanDigest(plan)
		if e != nil {
			return e
		}
		application.initial = initial
		next.applications = append(next.applications, application)
	}
	add := func(g storageIdentityLaunchGroup) error {
		for _, old := range next.groups {
			if old.key == g.key {
				return storagebudget.ErrBounds
			}
		}
		v, e := storageAdvisoryEstimate(g.bytes, g.files, g.directories, g.metadata)
		if e != nil {
			return e
		}
		next.peak, e = storageIdentityAddCapacity(next.peak, v)
		if e != nil {
			return e
		}
		next.groups = append(next.groups, g)
		return nil
	}
	// Preserve the exact shared keys. Entry adds four keys; it borrows the one
	// accepted duplication pair and does not mint a second entry-duplicate key.
	identity, err := parseIdentity(plan.IdentityInitial)
	if err != nil {
		return err
	}
	accepted := storageOwnerV4CatalogEffects()
	stamp := storageOwnerV5ProposedStampEffects()
	catalogs := append([]storageOwnerV4CatalogEffect{}, accepted[:]...)
	if identity.From != "" {
		catalogs = append(catalogs, stamp[:]...)
	}
	for _, key := range []string{"composition-entry/prepared/publish", "composition-entry/prepared/persist", "composition-entry/active/publish", "composition-entry/active/persist"} {
		catalogs = append(catalogs, storageOwnerV4CatalogEffect{Key: key, Bytes: 8388608, Metadata: 4, Parent: 1, MaxPerLaunch: 1})
	}
	for _, g := range catalogs {
		if identity.From == "" && strings.HasPrefix(g.Key, "lin4/from-") {
			continue
		}
		already := false
		for _, old := range next.groups {
			already = already || old.key.Catalog == g.Key
		}
		if already {
			continue
		}
		if err = add(storageIdentityLaunchGroup{key: storageIdentityLaunchKey{Catalog: g.Key, Parent: record.Config, Target: "storage-owner"}, parent: p.c.original.config, bytes: g.Bytes, files: g.Files, directories: g.Directories, metadata: g.Metadata}); err != nil {
			return err
		}
	}
	for _, action := range actions {
		for attempt := byte(1); attempt <= 2; attempt++ {
			groups, e := storageIdentityFiniteGroups(action, false, attempt == 2)
			if e != nil {
				return e
			}
			for _, g := range groups {
				parent, native, target := p.c.original.config, record.Config, "storage-intent"
				if g.Parent == 2 {
					parent = p.c.anchor
					native, e = storageOwnerPersistentRoot(ctx, parent)
					target = "Q"
				}
				if g.Parent == 3 {
					parent, native, target = private.root, plan.PrivateObject, startupIdentityName
				}
				if g.Parent == 4 {
					parent = p.c.original.config
					native = record.Config
					target = "stamping"
					if p.identityStamp != nil {
						if e = p.identityStamp.current(); e != nil {
							return e
						}
						parent = p.identityStamp.root
						native, e = storageOwnerPersistentRoot(ctx, parent)
					}
				}
				if e != nil {
					return e
				}
				if err = add(storageIdentityLaunchGroup{key: storageIdentityLaunchKey{Plan: plan.PlanID, Action: action, Attempt: attempt, Group: g.ID, Parent: native, Target: target}, parent: parent, bytes: g.Bytes, files: g.Files, directories: g.Directories, metadata: g.Metadata}); err != nil {
					return err
				}
			}
		}
	}
	for _, component := range union {
		next.debt, err = storageIdentityAddVector(next.debt, component.SettlementPhysical)
		if err != nil {
			return err
		}
	}
	if err = p.checkIdentityLaunch(ctx, &next); err != nil {
		return err
	}
	*launch = next
	launch.self = launch
	p.c.identityLaunch = launch
	p.identityLaunch = launch
	return p.held(ctx)
}

// Comparison only. The caller retains original epoch/native/Q authority,
// validates the current completed identity, and rechecks the full launch.
// No old data is reconstructed from this commitment and no board is mutated.
func (l *storageIdentityLaunch) completedOriginalPlan(plan storageOwnerV5LineagePlan, initial storageCompositionInitial, applications map[byte][]storagebudget.Component) bool {
	if l == nil || len(applications) == 0 || len(applications) > 3 {
		return false
	}
	digest, err := storageOwnerV5PlanDigest(plan)
	if err != nil {
		return false
	}
	seen := map[byte]bool{}
	for _, original := range l.applications {
		if original.plan != plan.PlanID {
			continue
		}
		// A separately admitted move shares P but is not one of P/C/S/F.
		// Its completed factual application remains on the same board intact.
		if original.action == storageIdentityIntentMove {
			if original.originalPlan != (storagebudget.Digest{}) || original.initial != (storageCompositionInitial{}) || !original.settled || !original.complete {
				return false
			}
			continue
		}
		components, exists := applications[original.action]
		if !exists || seen[original.action] || original.originalPlan != digest || original.initial != initial || !original.settled || !original.complete || !reflect.DeepEqual(original.components, components) {
			return false
		}
		seen[original.action] = true
	}
	return len(seen) == len(applications)
}

// Q snapshots are detached before native capacity observation. No Q gate or
// mutex is held during I/O. Full prior spend and the entire unresolved board
// are charged on EVERY actual parent view when sharing is not proved.
func (p *storageEnrollmentEpoch) checkIdentityLaunch(ctx context.Context, l *storageIdentityLaunch) error {
	if l == nil || l.construction != p.c {
		return storagebudget.ErrAuthority
	}
	if err := p.held(ctx); err != nil {
		return err
	}
	if err := p.qualified.current(ctx, p); err != nil {
		return err
	}
	status, err := p.c.ref.cell.q.StatusSnapshot(ctx)
	if err != nil || p.scope == nil && (p.initialInspection == nil || status.Maintenance || status.Epoch != p.initialInspection.View.Configuration.Epoch) {
		return errors.Join(errStorageEnrollmentUnresolved, err)
	}
	for _, row := range status.Roots {
		if !row.Known && p.scope == nil {
			return storagebudget.ErrUnknown
		}
	}
	for _, row := range status.Workspaces {
		if !row.Known && p.scope == nil {
			return storagebudget.ErrUnknown
		}
	}
	pending, err := p.identityPendingComponents(ctx, l, status)
	if err != nil {
		return err
	}
	installation := status.Installation
	for category := range installation {
		installation[category], err = storageIdentityAddVector(installation[category], status.Promises.Peak[category])
		if err != nil {
			return err
		}
		installation[category], err = storageIdentityAddVector(installation[category], status.Promises.Settlement[category])
		if err != nil {
			return err
		}
	}
	workspace := map[storagebudget.ID][storagebudget.Categories]storagebudget.Vector{}
	for _, w := range status.Workspaces {
		usage := w.Usage
		for category := range usage {
			usage[category], err = storageIdentityAddVector(usage[category], w.Promises.Peak[category])
			if err != nil {
				return err
			}
			usage[category], err = storageIdentityAddVector(usage[category], w.Promises.Settlement[category])
			if err != nil {
				return err
			}
		}
		workspace[w.ID] = usage
	}
	for _, component := range pending {
		demand, e := storageIdentityAddVector(component.Physical, component.SettlementPhysical)
		if e != nil {
			return e
		}
		installation[component.Category], err = storageIdentityAddVector(installation[component.Category], demand)
		if err != nil {
			return err
		}
		if component.Workspace != (storagebudget.ID{}) {
			attribution, e := storageIdentityAddVector(component.Attribution, component.SettlementAttribution)
			if e != nil {
				return e
			}
			usage, found := workspace[component.Workspace]
			if !found {
				return errStorageCustody
			}
			usage[component.Category], err = storageIdentityAddVector(usage[component.Category], attribution)
			if err != nil {
				return err
			}
			workspace[component.Workspace] = usage
		}
	}
	within := func(usage [storagebudget.Categories]storagebudget.Vector, limits storagebudget.Limits) bool {
		var total storagebudget.Vector
		for category, v := range usage {
			limit := limits.Category[category]
			if v.Bytes > limit.Bytes || v.Files > limit.Files || v.Directories > limit.Directories {
				return false
			}
			var e error
			total, e = storageIdentityAddVector(total, v)
			if e != nil {
				return false
			}
		}
		return total.Bytes <= limits.Total.Bytes && total.Files <= limits.Total.Files && total.Directories <= limits.Total.Directories
	}
	for _, board := range l.enrollments {
		if board.complete {
			continue
		}
		installation[storagebudget.Control], err = storageIdentityAddVector(installation[storagebudget.Control], board.logical)
		if err != nil {
			return err
		}
	}
	owedMove, err := p.originalMoveOwed(ctx)
	if err != nil {
		return err
	}
	if !owedMove {
		if !within(installation, status.Policy.Installation) {
			return storagebudget.ErrLimit
		}
		for _, w := range status.Workspaces {
			if !within(workspace[w.ID], w.Limits) {
				return storagebudget.ErrLimit
			}
		}
	}
	spent, err := storageIdentityAddCapacity(p.c.spent, p.c.server.storageInfrastructure.spent)
	if err != nil {
		return err
	}
	whole, err := storageIdentityAddCapacity(spent, l.peak)
	if err != nil {
		return err
	}
	// Existing Q promises coexist with, and cannot stand in for, launch debt.
	for _, promise := range status.CapacityPromised {
		whole, err = storageIdentityAddCapacity(whole, promise.Peak)
		if err != nil {
			return err
		}
	}
	for _, component := range pending {
		for i := 0; i < int(component.CapacityCount); i++ {
			whole, err = storageIdentityAddCapacity(whole, component.Capacity[i].Peak)
			if err != nil {
				return err
			}
		}
	}
	seen := map[*os.Root]storageOwnerPersistentObject{}
	views := append([]storageIdentityLaunchGroup{}, l.groups...)
	for _, board := range l.enrollments {
		for i, parent := range board.parents {
			views = append(views, storageIdentityLaunchGroup{parent: parent, key: storageIdentityLaunchKey{Parent: board.natives[i]}})
		}
	}
	for _, g := range views {
		if native, ok := seen[g.parent]; ok {
			if native != g.key.Parent {
				return errStorageCustody
			}
			continue
		}
		seen[g.parent] = g.key.Parent
		if g.parent == nil {
			return storagebudget.ErrAuthority
		}
		f, e := g.parent.Open(".")
		if e != nil {
			return e
		}
		native, e := storageAdvisoryPersistentHeld(ctx, f)
		if e == nil && native != g.key.Parent {
			e = errStorageCustody
		}
		var facts storageAdvisoryNativeFacts
		if e == nil {
			facts, e = storageAdvisoryReadHeld(ctx, f)
		}
		if e == nil {
			e = (&storageInfrastructureStart{}).checkAvailability(facts, whole)
		}
		e = errors.Join(e, f.Close())
		if e != nil {
			return e
		}
	}
	constraints := status.CapacityConstraints
	if p.scope != nil {
		constraints = nil
		for _, row := range status.CapacityConstraints {
			if old, ok := p.c.owner.constraints[row.ID]; ok {
				if old != row {
					return errStorageCustody
				}
				constraints = append(constraints, row)
			}
		}
	}
	ids := make([]storagebudget.ID, 0, len(constraints))
	for _, row := range constraints {
		ids = append(ids, row.ID)
	}
	observations, err := p.originalEnrollmentObservations(ctx, ids)
	if err != nil {
		return err
	}
	for _, row := range constraints {
		var observation *storagebudget.CapacityObservation
		for i := range observations {
			if observations[i].Constraint == row.ID {
				observation = &observations[i]
			}
		}
		if observation == nil || observation.Generation != row.Generation || observation.KnownResources != row.Resources || observation.At.IsZero() || time.Since(observation.At) < 0 || time.Since(observation.At) > status.Policy.ObservationMaxAge {
			return storagebudget.ErrUnknown
		}
		if row.Resources&storagebudget.CapacityBytes != 0 && (observation.Available.Bytes < row.Headroom.Bytes || whole.Bytes > observation.Available.Bytes-row.Headroom.Bytes) || row.Resources&storagebudget.CapacityInodes != 0 && (observation.Available.Inodes < row.Headroom.Inodes || whole.Inodes > observation.Available.Inodes-row.Headroom.Inodes) {
			return storagebudget.ErrHeadroom
		}
	}
	return p.held(ctx)
}

func (p *storageEnrollmentEpoch) takeIdentityCatalog(ctx context.Context, key string, bank byte) error {
	if bank > 1 {
		return storagebudget.ErrBounds
	}
	target := p.targets[4+bank]
	if target.named.file == nil || target.native != p.original.Targets[4+bank] {
		return errStorageCustody
	}
	if p.move != nil && p.identity != nil && p.identity.record.Action == storageIdentityIntentMove {
		groups := map[string]uint16{"move/prepared/publish": 50, "move/prepared/persist": 51, "move/active/publish": 52, "move/active/persist": 53}
		if group, ok := groups[key]; ok {
			return p.identity.take(ctx, group)
		}
	}
	l := p.identityLaunch
	if l == nil || l.self != l || l != p.c.identityLaunch || l.failed != nil {
		return storagebudget.ErrAuthority
	}
	if err := p.checkIdentityLaunch(ctx, l); err != nil {
		return err
	}
	for i := range l.groups {
		g := &l.groups[i]
		if g.key.Catalog != key {
			continue
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		if !p.running || p.revoked || p.entered || p.finishing || g.spent {
			return storagebudget.ErrAuthority
		}
		if g.key.NativeTarget != (storageOwnerPersistentObject{}) && (g.key.NativeTarget != target.native || g.key.Target != target.named.name) {
			return errStorageCustody
		}
		g.key.Target = target.named.name
		g.key.NativeTarget = target.native
		g.spent = true // before the FIRST syscall/API in the complete group
		return nil
	}
	return storagebudget.ErrAuthority
}

// This is only the codec-local half of the reviewed shared-settlement join.
// Every independent promise/debt vector remains unchanged. Actual reference
// completeness and current original registration are checked by the original
// producer below, never inferred here from workspace IDs or promise amounts.
func storageIdentityPeerMapping(c storageIntentRecord, index int) (bool, error) {
	if index <= 0 || index >= len(c.Components) || len(c.RootIdentities) != len(c.Components) || len(c.FilesystemIdentities) != len(c.Components) || len(c.Locators) != len(c.Components) {
		return false, storagebudget.ErrBounds
	}
	leader, peer := c.Components[0], c.Components[index]
	if peer.Root != leader.Root || peer.Category != leader.Category {
		return false, nil
	}
	if leader.Workspace == (storagebudget.ID{}) || peer.Workspace == (storagebudget.ID{}) || peer.Workspace == leader.Workspace || peer.WorkspaceGeneration == 0 {
		return false, storagebudget.ErrUnknown
	}
	projection := leader
	projection.Workspace, projection.WorkspaceGeneration = peer.Workspace, peer.WorkspaceGeneration
	projection.Physical, projection.Attribution = peer.Physical, peer.Attribution
	projection.SettlementPhysical, projection.SettlementAttribution = peer.SettlementPhysical, peer.SettlementAttribution
	for j := range projection.Capacity {
		projection.Capacity[j].Peak = peer.Capacity[j].Peak
	}
	if projection != peer || c.RootIdentities[0] != c.RootIdentities[index] || c.FilesystemIdentities[0] != c.FilesystemIdentities[index] || c.Locators[0] != c.Locators[index] {
		return false, storagebudget.ErrUnknown
	}
	for i := 0; i < index; i++ {
		if c.Components[i].Root == peer.Root && c.Components[i].Workspace == peer.Workspace && c.Components[i].Category == peer.Category {
			return false, storagebudget.ErrUnknown
		}
	}
	return true, nil
}
func storageIdentitySettlementChanges(r storageIdentityIntent) ([]storagebudget.Change, error) {
	c := r.Core
	if len(c.Components) == 0 || len(c.Components) > storagebudget.MaxComponents || len(c.RootIdentities) != len(c.Components) || len(c.FilesystemIdentities) != len(c.Components) || len(c.Locators) != len(c.Components) {
		return nil, storagebudget.ErrBounds
	}
	out := make([]storagebudget.Change, len(c.Components))
	add, remove := storagebudget.Vector{}, storagebudget.Vector{}
	if c.Outcome == storageIntentPublished && r.Action != storageIdentityIntentStamp {
		add = storagebudget.Vector{Bytes: c.NewLength, Files: 1}
		if r.Action == storageIdentityIntentClear || r.Action == storageIdentityIntentInitialCAS || r.Action == storageIdentityIntentMove {
			remove = storagebudget.Vector{Bytes: c.OldLength, Files: 1}
		}
		out[0].PhysicalAdd, out[0].PhysicalRemove = add, remove
		if c.Components[0].Workspace != (storagebudget.ID{}) {
			out[0].AttributionAdd, out[0].AttributionRemove = add, remove
		}
	}
	for i := 1; i < len(c.Components); i++ {
		peer, err := storageIdentityPeerMapping(c, i)
		if err != nil {
			return nil, err
		}
		if peer {
			out[i].AttributionAdd, out[i].AttributionRemove = add, remove
		}
	}
	if r.Action == storageIdentityIntentMove && c.Outcome == storageIntentPublished {
		if !r.MoveTails.valid(c) {
			return nil, storagebudget.ErrUnknown
		}
		for tail := 0; tail < int(r.MoveTails.Count); tail++ {
			leader := int(r.MoveTails.Component[tail])
			if leader >= len(c.Components) {
				return nil, storagebudget.ErrBounds
			}
			mapping := c.Components[leader]
			add := storagebudget.Vector{Bytes: r.MoveTails.AfterBytes[tail], Files: 1}
			remove := storagebudget.Vector{}
			if r.MoveTails.BeforeExists[tail] {
				remove = storagebudget.Vector{Bytes: r.MoveTails.BeforeBytes[tail], Files: 1}
			}
			for i, component := range c.Components {
				if component.Root != mapping.Root || component.Category != mapping.Category {
					continue
				}
				var e error
				if i == leader {
					out[i].PhysicalAdd, e = storageIdentityAddVector(out[i].PhysicalAdd, add)
					if e != nil {
						return nil, e
					}
					out[i].PhysicalRemove, e = storageIdentityAddVector(out[i].PhysicalRemove, remove)
					if e != nil {
						return nil, e
					}
				}
				if component.Workspace != (storagebudget.ID{}) {
					out[i].AttributionAdd, e = storageIdentityAddVector(out[i].AttributionAdd, add)
					if e != nil {
						return nil, e
					}
					out[i].AttributionRemove, e = storageIdentityAddVector(out[i].AttributionRemove, remove)
					if e != nil {
						return nil, e
					}
				}
			}
		}
	}
	return out, nil
}

// Compare EVERY exact mapping and the complete original reference set. This
// is deliberately separate from the pure projection. The graph is an actual
// strict-decoded retained core; held roots are the original epoch's bindings.
// Journal/manifest equality freezes independent debt before any action effect.
func (p *storageEnrollmentEpoch) identityComponentMappings(ctx context.Context, c storageIntentRecord, private storagebudget.ID) error {
	if err := p.held(ctx); err != nil {
		return err
	}
	if err := p.c.owner.identityRecordMappings(ctx, c, private); err != nil {
		return err
	}
	return p.held(ctx)
}
func (o *storageBudgetOwner) identityRecordMappings(ctx context.Context, c storageIntentRecord, private storagebudget.ID) error {
	if !o.valid() {
		return storagebudget.ErrAuthority
	}
	if len(c.Components) == 0 || len(c.Components) > storagebudget.MaxComponents || len(c.RootIdentities) != len(c.Components) || len(c.FilesystemIdentities) != len(c.Components) || len(c.Locators) != len(c.Components) || c.Components[0].Root != private || c.Components[0].Category != storagebudget.Control {
		return storagebudget.ErrAuthority
	}
	for _, component := range c.Components {
		r := o.roots[component.Root]
		if r == nil {
			return errStorageCustody
		}
		refs := r.references
		if r.configControl || r.qControl {
			refs = nil
		}
		if len(refs) == 0 {
			refs = []storagebudget.ID{{}}
		}
		seen := map[storagebudget.ID]bool{}
		for i, row := range c.Components {
			if row.Root != component.Root || row.Category != component.Category {
				continue
			}
			fs, ok := o.filesystems[row.Filesystem]
			if !ok || row.Category != r.category || row.RootGeneration != r.row.Generation || row.Owner != r.row.Owner || row.Filesystem != r.row.Filesystem || row.FilesystemGeneration != fs.Generation || c.RootIdentities[i] != r.row.Identity || c.FilesystemIdentities[i] != fs.Identity || c.Locators[i] != r.row.Locator || !o.intentCapacityMatches(r, row) || seen[row.Workspace] {
				return errStorageCustody
			}
			member := false
			for _, id := range refs {
				member = member || id == row.Workspace
			}
			if !member {
				return errStorageCustody
			}
			if row.Workspace != (storagebudget.ID{}) {
				w, ok := o.workspaces[row.Workspace]
				if !ok || w.Generation != row.WorkspaceGeneration {
					return errStorageCustody
				}
			} else if row.WorkspaceGeneration != 0 {
				return errStorageCustody
			}
			seen[row.Workspace] = true
		}
		if len(seen) != len(refs) {
			return errStorageCustody
		}
		if err := r.current(ctx); err != nil {
			return err
		}
	}
	for i := 1; i < len(c.Components); i++ {
		if _, err := storageIdentityPeerMapping(c, i); err != nil {
			return err
		}
	}
	return nil
}

func storageIdentityRequiredActions(plan storageOwnerV5LineagePlan, initial storageCompositionInitial) ([]byte, error) {
	identity, err := parseIdentity(plan.IdentityInitial)
	if err != nil {
		return nil, err
	}
	action := byte(storageIdentityIntentCreate)
	if initial.Mode == 2 {
		action = storageIdentityIntentInitialCAS
	} else if initial.Mode != 1 || initial.ExpectedOld != (storageOwnerPersistentObject{}) {
		return nil, storagebudget.ErrBounds
	}
	out := []byte{action}
	if identity.From != "" {
		out = append(out, storageIdentityIntentStamp, storageIdentityIntentClear)
	}
	return out, nil
}
func (p *storageEnrollmentEpoch) identityGraph() (storageOwnerCatalogV2, storageCompositionLineage, error) {
	var graph storageOwnerCatalogV2
	lineage := storageCompositionEmptyLineage()
	if p == nil || p.original == nil {
		return graph, lineage, storagebudget.ErrAuthority
	}
	record, claim := *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim
	catalogs := p.original.Catalogs
	if p.compositionEntry != nil && p.compositionCut < 4 {
		catalogs = p.compositionEntry.Original
	}
	cores, _, err := p.identityCorePair(catalogs, record, claim)
	if err != nil {
		return graph, lineage, err
	}
	if string(cores[0][:8]) == "DSKOWN3\x00" {
		pair, e := storageOwnerDecodeCatalogPairV3(cores[0], cores[1], record, claim)
		if e != nil {
			return graph, lineage, e
		}
		graph = pair.High
	} else {
		if p.previousEvidence == nil {
			return graph, lineage, errStorageEnrollmentUnresolved
		}
		graph = p.previousEvidence.Inner.Catalog
	}
	if storagePolicyActualPair(catalogs) {
		p.c.ref.cell.mu.Lock()
		endpoint := p.c.ref.cell.enrollmentEndpoint
		p.c.ref.cell.mu.Unlock()
		if endpoint == nil {
			return graph, lineage, errStorageEnrollmentUnresolved
		}
		selected := byte(0)
		if storageEnrollmentPeakU64(catalogs[1][16:24]) > storageEnrollmentPeakU64(catalogs[0][16:24]) {
			selected = 1
		}
		v, e := storageOwnerV6NativeReadEndpoint(catalogs[selected], endpoint.Raw, record, claim)
		if e != nil {
			return graph, lineage, e
		}
		lineage = v.Stage.Lineage
	} else if string(catalogs[0][:8]) == "DSKONL1\x00" {
		pair, e := storageCompositionReadPair(catalogs[0], catalogs[1], record, claim)
		if e != nil || pair.RecoveryOnly && !storageIdentityPreparedLineage(pair) {
			return graph, lineage, storagebudget.ErrUnknown
		}
		lineage = pair.Banks[pair.Selected].Lineage
	}
	return graph, lineage, lineage.validateGraph(graph, record)
}

// Legitimate identity choice under the original project/signing prefixes. No
// identity ID, legacy From, accounting/history key or ExpectedOld is supplied
// by a caller. Existing P is retained verbatim; a restart never redraws it.
func (p *storageEnrollmentEpoch) captureOriginalIdentityPlan(ctx context.Context) error {
	if p == nil || p.identityPlan != nil || !p.identityPrefixes || p.original == nil {
		return storagebudget.ErrAuthority
	}
	if err := p.held(ctx); err != nil {
		return err
	}
	graph, lineage, err := p.identityGraph()
	if err != nil {
		return err
	}
	var private *storageRootBinding
	var row *storageOwnerRoot
	for _, r := range p.originalRoots {
		if r.source == nil || r.category != storagebudget.Control || r.source.path != filepath.Join(p.originalProjectPath(), startupIdentityDir) || r.source.role != 3 && r.source.role != 6 {
			continue
		}
		if private != nil {
			return errStorageCustody
		}
		private = r
		for i := range graph.Roots {
			if graph.Roots[i].Definition.ID == r.row.ID {
				row = &graph.Roots[i]
			}
		}
	}
	if private == nil || row == nil || row.Source.Kind != 3 && row.Source.Kind != 6 && row.Source.Kind != 8 {
		return errStorageOwnerTransitionRequired
	}
	native, err := storageOwnerPersistentRoot(ctx, private.root)
	if err != nil || int(row.DirectoryObject) >= len(graph.Objects) || graph.Objects[row.DirectoryObject].Persistent != native {
		return errors.Join(errStorageCustody, err)
	}
	for index, id := range lineage.Order[0] {
		for _, old := range lineage.Rows.LineagePlans {
			if old.PlanID != id || old.PrivateRoot.ID != row.Definition.ID {
				continue
			}
			if p.identityPlan != nil || old.PrivateObject != native || old.ProjectObject != p.original.Project {
				return errStorageCustody
			}
			copy := old
			copy.IdentityInitial = append([]byte{}, old.IdentityInitial...)
			copy.IdentityFromCleared = append([]byte{}, old.IdentityFromCleared...)
			copy.HistoryIdentity = append([]byte{}, old.HistoryIdentity...)
			copy.AccountingOwnerKey = append([]byte{}, old.AccountingOwnerKey...)
			p.identityPlan = &copy
			p.identityInitial = lineage.Initial[index]
			p.identityPrivate = private
		}
	}
	if p.identityPlan == nil {
		if p.startupProject != nil {
			return errStorageEnrollmentUnresolved
		}
		record, info, found, e := readIdentity(private.root)
		if e != nil {
			return e
		}
		initial := storageCompositionInitial{Mode: 1}
		if found {
			if record.Path != p.originalProjectPath() || record.Folder == "" || record.Folder != p.originalFolderKey() {
				return errStorageCustody
			}
			data, held, before, e := storageReadOriginalFile(private.root, startupIdentityName, 512)
			if e != nil {
				return e
			}
			p.identityOriginal = held
			if !os.SameFile(info, before) || !bytes.Equal(data, record.line()) {
				return errStorageCustody
			}
			initial.Mode = 2
			initial.ExpectedOld, e = storageAdvisoryPersistentHeld(ctx, held)
			if e != nil {
				return e
			}
		} else {
			if p.startupProject != nil {
				return errStorageEnrollmentUnresolved
			}
			record, e = p.originalServer().chooseStartupIdentity(p.signing)
			if e != nil {
				return e
			}
		}
		var workspace *storageOwnerWorkspace
		for i := range graph.Workspaces {
			w := &graph.Workspaces[i]
			if w.OwnerKind != 3 {
				continue
			}
			linked := false
			for _, ref := range row.References {
				linked = linked || ref == w.Definition.ID
			}
			if !linked {
				continue
			}
			focal := false
			if graph.OriginalOwner.Kind == 1 {
				focal = bytes.Equal(w.OwnerKey, row.Source.OwnerKey) && (string(w.OwnerKey) == record.ID || string(w.OwnerKey) == digestOf([]byte(p.originalProjectPath())))
			} else {
				for _, entry := range graph.OriginalOwner.Entries {
					focal = focal || entry.Kind == 1 && filepath.Join(string(graph.OriginalOwner.HomePath), string(entry.Folder)) == p.originalProjectPath() && entry.RunnerWorkspace == string(w.OwnerKey) && bytes.Equal(entry.HistoryIdentity, w.HistoryIdentity)
				}
			}
			if focal {
				if workspace != nil {
					return errStorageCustody
				}
				workspace = w
			}
		}
		if workspace == nil {
			return errStorageCustody
		}
		id, e := storagebudget.NewID()
		if e != nil {
			return e
		}
		wd, e := storageOwnerV4WorkspaceDigest(*workspace)
		if e != nil {
			return e
		}
		sd, e := storageCompositionPrivateSourceDigest(graph, *row)
		if e != nil {
			return e
		}
		var provider *storageOwnerRoot
		for i := range graph.Roots {
			r := &graph.Roots[i]
			if r.Role != 5 {
				continue
			}
			for _, ref := range r.References {
				if ref == workspace.Definition.ID {
					if provider != nil {
						return errStorageCustody
					}
					provider = r
				}
			}
		}
		pd, e := storageOwnerV4ProviderDigest(graph, *workspace, provider)
		if e != nil {
			return e
		}
		plan := storageOwnerV5LineagePlan{PlanID: id, Installation: graph.Installation, QBinding: graph.QBinding, Workspace: storageOwnerV5Workspace{ID: workspace.Definition.ID, Generation: workspace.Definition.Generation, Digest: wd}, PrivateRoot: storageOwnerV5PrivateRoot{ID: row.Definition.ID, Generation: row.Definition.Generation, Identity: row.Definition.Identity, SourceDigest: sd}, ProjectObject: p.original.Project, PrivateObject: native, ConfigObject: p.c.ref.cell.ownerRecord.Config, ProviderRelationDigest: pd, HistoryIdentity: append([]byte{}, workspace.HistoryIdentity...), AccountingOwnerKey: append([]byte{}, workspace.OwnerKey...), IdentityInitial: record.line(), IdentityFromCleared: []byte{}}
		if record.From != "" {
			record.From = ""
			plan.IdentityFromCleared = record.line()
		}
		plan.CompletionRoom, plan.StampRoom, plan.ContinuationRoom, e = storageOwnerV5Rooms(plan)
		if e != nil {
			return e
		}
		candidate := storageCompositionEmptyLineage()
		candidate.Rows.LineagePlans = []storageOwnerV5LineagePlan{plan}
		candidate.Order[0] = []storagebudget.ID{plan.PlanID}
		candidate.Initial = []storageCompositionInitial{initial}
		if e = candidate.validateGraph(graph, *p.c.ref.cell.ownerRecord); e != nil {
			return e
		}
		p.identityPlan, p.identityInitial, p.identityPrivate = &plan, initial, private
	}
	identity, err := parseIdentity(p.identityPlan.IdentityInitial)
	if err != nil {
		return err
	}
	if identity.From != "" {
		if _, e := p.c.original.config.Lstat(stampingDirName); e == nil {
			stamp, e := storagePinChild(p.c.original.config, stampingDirName, true)
			if e != nil {
				return e
			}
			p.identityStamp = &stamp
		} else if !errors.Is(e, os.ErrNotExist) {
			return e
		}
	}
	actions, err := storageIdentityRequiredActions(*p.identityPlan, p.identityInitial)
	if err != nil {
		return err
	}
	p.identityApplications = map[byte][]storagebudget.Component{}
	for _, action := range actions {
		components, e := p.identityApplicationComponents(ctx, action)
		if e != nil {
			return e
		}
		p.identityApplications[action] = components
	}
	return p.held(ctx)
}

// Existing finite action+one receipt+optional Q completion+shared duplicate and
// reopen operands, distributed to their actual original parent mappings. This
// is an application request board, not another ticket or native allowance.
func (p *storageEnrollmentEpoch) identityApplicationComponents(ctx context.Context, action byte) ([]storagebudget.Component, error) {
	if p.identityPlan == nil || p.identityPrivate == nil {
		return nil, storagebudget.ErrAuthority
	}
	if err := p.held(ctx); err != nil {
		return nil, err
	}
	var control, anchor *storageRootBinding
	for _, r := range p.originalRoots {
		if r.configControl {
			if control != nil {
				return nil, errStorageCustody
			}
			control = r
		}
		if r.qControl {
			if anchor != nil {
				return nil, errStorageCustody
			}
			anchor = r
		}
	}
	if control == nil || anchor == nil {
		return nil, errStorageCustody
	}
	groups, err := storageIdentityFiniteGroups(action, false, true)
	if err != nil {
		return nil, err
	}
	var debt [3]storagebudget.Vector
	var peak [3]storagebudget.CapacityVector
	add := func(parent int, b, f, d, m uint64) error {
		var e error
		debt[parent], e = storageIdentityAddVector(debt[parent], storagebudget.Vector{Bytes: b, Files: f, Directories: d})
		if e != nil {
			return e
		}
		v, e := storageAdvisoryEstimate(b, f, d, m)
		if e != nil {
			return e
		}
		peak[parent], e = storageIdentityAddCapacity(peak[parent], v)
		return e
	}
	for _, g := range groups {
		parent := int(g.Parent) - 1
		if g.Parent == 4 {
			parent = 0
		}
		if err = add(parent, g.Bytes, g.Files, g.Directories, g.Metadata); err != nil {
			return nil, err
		}
	}
	for i := 0; i < 7; i++ {
		if err = add(0, 8388608, 0, 0, 4); err != nil {
			return nil, err
		}
	}
	main := p.identityPrivate
	physical := storagebudget.Vector{Bytes: uint64(len(p.identityPlan.IdentityInitial)), Files: 1}
	if action == storageIdentityIntentClear {
		physical.Bytes = uint64(len(p.identityPlan.IdentityFromCleared))
	}
	if action == storageIdentityIntentStamp {
		main = control
		physical = storagebudget.Vector{}
	}
	var out []storagebudget.Component
	refs := main.references
	if main.configControl || main.qControl {
		refs = nil
	}
	if len(refs) == 0 {
		refs = []storagebudget.ID{{}}
	}
	for i, ref := range refs {
		ordinary, settlement, attribution, settlementAttribution := physical, debt[2], physical, debt[2]
		if action == storageIdentityIntentStamp {
			settlement = debt[0]
			settlementAttribution = storagebudget.Vector{}
		}
		if ref == (storagebudget.ID{}) {
			attribution, settlementAttribution = storagebudget.Vector{}, storagebudget.Vector{}
		}
		if i > 0 {
			ordinary, settlement = storagebudget.Vector{}, storagebudget.Vector{}
		}
		c := storageOwnerComponent(main, ref, storagebudget.Control, ordinary, attribution, settlement)
		c.SettlementAttribution = settlementAttribution
		if i == 0 {
			v := peak[2]
			if action == storageIdentityIntentStamp {
				v = peak[0]
			}
			for j := 0; j < int(c.CapacityCount); j++ {
				c.Capacity[j].Peak = v
			}
		}
		out = append(out, c)
	}
	if action != storageIdentityIntentStamp {
		c := storageOwnerComponent(control, storagebudget.ID{}, storagebudget.Control, storagebudget.Vector{}, storagebudget.Vector{}, debt[0])
		for j := 0; j < int(c.CapacityCount); j++ {
			c.Capacity[j].Peak = peak[0]
		}
		out = append(out, c)
	}
	c := storageOwnerComponent(anchor, storagebudget.ID{}, storagebudget.Control, storagebudget.Vector{}, storagebudget.Vector{}, debt[1])
	for j := 0; j < int(c.CapacityCount); j++ {
		c.Capacity[j].Peak = peak[1]
	}
	out = append(out, c)
	return storageIdentityComponentUnion(nil, out)
}

// Only the original epoch may construct a live action. Decoded J/P/manifest
// operands remain comparisons; none can replace these retained descriptions.
type storageIdentityLive struct {
	self                  *storageIdentityLive
	phase                 *storageEnrollmentEpoch
	record                storageIdentityIntent
	request               storagebudget.Request
	selected              storageIntentSlot
	host                  storageNamedFile
	parent                *os.Root
	parentFile            *os.File // borrowed original domain description
	payload               []byte
	old                   *os.File // retained original, borrowed from epoch for initial CAS
	oldInfo               os.FileInfo
	allocated             *os.File
	allocatedInfo         os.FileInfo
	observations          []storagebudget.CapacityObservation
	admitted              storagebudget.Token
	attempt               byte
	hostWrites            uint16
	started, finished     bool
	recovering            bool
	hostPersisted         bool
	qPersisted            bool
	ownedParent           bool
	source                *storageNamedRoot
	sourceFile            *os.File
	stageName, targetName string
}

func (m *storageIdentityLive) Close() error {
	if m == nil {
		return nil
	}
	if m.self != m {
		return storagebudget.ErrAuthority
	}
	var err error
	if m.allocated != nil {
		f := m.allocated
		m.allocated = nil
		err = errors.Join(err, f.Close())
	}
	if m.sourceFile != nil {
		f := m.sourceFile
		m.sourceFile = nil
		err = errors.Join(err, f.Close())
	}
	if m.source != nil {
		r := m.source
		m.source = nil
		err = errors.Join(err, r.root.Close())
	}
	if m.ownedParent && m.parentFile != nil {
		f := m.parentFile
		m.parentFile = nil
		err = errors.Join(err, f.Close())
	}
	return err
}
func (m *storageIdentityLive) current(ctx context.Context) error {
	if m == nil || m.self != m || m.phase == nil || m.phase.identity != m || m.phase.identityPlan == nil || m.phase.identityLaunch == nil || m.phase.identityLaunch.failed != nil {
		return storagebudget.ErrAuthority
	}
	p := m.phase
	if err := p.held(ctx); err != nil {
		return err
	}
	if err := p.qualified.current(ctx, p); err != nil {
		return err
	}
	digest, err := storageOwnerV5PlanDigest(*p.identityPlan)
	if err != nil || digest != m.record.PlanDigest {
		return errors.Join(storagebudget.ErrAuthority, err)
	}
	if err = m.record.validate(); err != nil {
		return err
	}
	if err = m.hostCurrent(); err != nil {
		return err
	}
	if err = p.identityComponentMappings(ctx, m.record.Core, m.record.Core.Components[0].Root); err != nil {
		return err
	}
	native, err := storageAdvisoryPersistentHeld(ctx, m.parentFile)
	if err != nil || native != m.record.Parent {
		return errors.Join(errStorageCustody, err)
	}
	named, e := m.parent.Stat(".")
	held, e2 := m.parentFile.Stat()
	if e != nil || e2 != nil || !os.SameFile(named, held) {
		return errors.Join(errStorageCustody, e, e2)
	}
	if m.record.Action == storageIdentityIntentStamp {
		if p.identityStamp == nil || p.identityStamp.root != m.parent {
			return storagebudget.ErrAuthority
		}
		if err = p.identityStamp.current(); err != nil {
			return err
		}
		facts, e := storageEnrollmentNativeParentObservation(ctx, m.parentFile)
		if e != nil || facts != p.qualified.record.Facts.Parents[0] {
			return errors.Join(errStorageEnrollmentQualification, e)
		}
		return p.qualified.current(ctx, p)
	}
	return p.qualified.privateEffectParent(ctx, p, storageCreationPrivate, m.parentFile)
}
func (p *storageEnrollmentEpoch) identityPendingComponents(ctx context.Context, l *storageIdentityLaunch, status storagebudget.StatusSnapshot) ([]storagebudget.Component, error) {
	// Reserve transfers precisely one original application's promises to Q.
	// All other apps and all consumed/unknown finite groups remain charged.
	var alias *storageIdentityLaunchApplication
	m := p.identity
	if m != nil && m.self == m && m.admitted != (storagebudget.Token{}) {
		host, err := p.c.ref.cell.q.InspectHostState(ctx)
		if err != nil {
			return nil, err
		}
		if host.Epoch != status.Epoch || host.Revision != status.Revision || host.Maintenance {
			return nil, errStorageEnrollmentUnresolved
		}
		ticket := host.Slots[m.admitted.Slot].Ticket
		if ticket.State != storagebudget.Terminal {
			if ticket.Token != m.admitted || ticket.Owner != p.c.ref.cell.binding || ticket.Workflow != m.request.Workflow || ticket.Operation != m.request.Operation || !reflect.DeepEqual(ticket.Components, m.request.Components) {
				return nil, errStorageEnrollmentUnresolved
			}
			actual, err := p.c.ref.cell.q.Manifest(ctx, m.admitted, p.c.ref.cell.binding)
			if err != nil || !reflect.DeepEqual(actual, m.request) {
				return nil, errors.Join(errStorageEnrollmentUnresolved, err)
			}
			for i := range l.applications {
				a := &l.applications[i]
				if a.plan == p.identityPlan.PlanID && a.action == m.record.Action {
					if alias != nil || !reflect.DeepEqual(a.components, m.request.Components) {
						return nil, storagebudget.ErrAuthority
					}
					alias = a
				}
			}
			if alias == nil {
				return nil, storagebudget.ErrAuthority
			}
		}
	}
	if creation := p.creation; creation != nil && creation.self == creation && creation.admitted != (storagebudget.Token{}) {
		host, e := p.c.ref.cell.q.InspectHostState(ctx)
		if e != nil {
			return nil, e
		}
		if host.Epoch != status.Epoch || host.Revision != status.Revision || host.Maintenance != status.Maintenance {
			return nil, errStorageEnrollmentUnresolved
		}
		ticket := host.Slots[creation.admitted.Slot].Ticket
		if ticket.State != storagebudget.Terminal {
			if ticket.Token != creation.admitted || !storageCreationTicketMatchesBound(creation.record, ticket, creation.request.Owner) {
				return nil, errStorageEnrollmentUnresolved
			}
			actual, e := p.c.ref.cell.q.Manifest(ctx, creation.admitted, creation.request.Owner)
			if e != nil || !reflect.DeepEqual(actual, creation.request) {
				return nil, errors.Join(errStorageEnrollmentUnresolved, e)
			}
			for i := range l.applications {
				a := &l.applications[i]
				if a.plan == creation.record.Core.OperationID && a.action == creation.launchAction() {
					if alias != nil || !reflect.DeepEqual(a.components, creation.request.Components) {
						return nil, storagebudget.ErrAuthority
					}
					alias = a
				}
			}
			if alias == nil {
				return nil, storagebudget.ErrAuthority
			}
		}
	}
	var out []storagebudget.Component
	for i := range l.applications {
		a := &l.applications[i]
		if a != alias && !a.complete {
			for _, original := range a.components {
				component := original
				if a.settled {
					component.Physical = storagebudget.Vector{}
					component.Attribution = storagebudget.Vector{}
				}
				out = append(out, component)
			}
		}
	}
	return out, nil
}
func (m *storageIdentityLive) take(ctx context.Context, group uint16) error {
	if err := m.current(ctx); err != nil {
		return err
	}
	p, l := m.phase, m.phase.identityLaunch
	if err := p.checkIdentityLaunch(ctx, l); err != nil {
		return err
	}
	for i := range l.groups {
		g := &l.groups[i]
		planKey := p.identityPlan.PlanID
		if m.record.Action == storageIdentityIntentMove {
			if p.move == nil {
				return storagebudget.ErrAuthority
			}
			planKey = p.move.request.Operation
		}
		if g.key.Plan == planKey && g.key.Action == m.record.Action && g.key.Attempt == m.attempt && g.key.Group == group {
			if g.spent {
				return storagebudget.ErrAuthority
			}
			g.spent = true
			return nil
		}
	}
	return storagebudget.ErrAuthority
}
func (m *storageIdentityLive) enter(ctx context.Context) error {
	if err := m.current(ctx); err != nil {
		return err
	}
	p := m.phase
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.running || p.entered || p.revoked || p.finishing || p.err != nil {
		return storagebudget.ErrAuthority
	}
	p.entered = true
	m.started = true
	return nil
}
func (m *storageIdentityLive) next(phase storageIntentPhase) storageIdentityIntent {
	r := m.record
	r.Core.Generation = m.selected.record.Generation + 1
	r.Core.PreviousPageDigest = sha256.Sum256(m.selected.page)
	r.Core.Phase = phase
	r.Core.Changes = append([]storagebudget.Change{}, r.Core.Changes...)
	return r
}
func (m *storageIdentityLive) hostCurrent() error {
	actual := make([]byte, storageIntentBytes)
	n, err := m.host.file.ReadAt(actual, 0)
	if err != nil || n != len(actual) {
		return errors.Join(errStorageCustody, err)
	}
	for slot, pages := range m.phase.original.Hosts {
		for bank, page := range pages {
			at := (slot*2 + bank) * storageIntentPage
			if !bytes.Equal(actual[at:at+storageIntentPage], page) {
				return errStorageCustody
			}
		}
	}
	return m.host.current()
}
func (m *storageIdentityLive) publish(ctx context.Context, r storageIdentityIntent) (err error) {
	if m == nil || r.validate() != nil {
		return storagebudget.ErrAuthority
	}
	old := m.selected
	if old.record.Phase == storageIntentEmpty {
		if r.Core.Generation != 1 || r.Core.Phase != storageIntentPrepared || r.Core.PreviousPageDigest != (storagebudget.Digest{}) {
			return storagebudget.ErrStale
		}
	} else if old.record.reusable() && r.Core.OperationID != old.record.OperationID {
		if r.Core.Generation != old.record.Generation+1 || r.Core.Phase != storageIntentPrepared || r.Core.PreviousPageDigest != sha256.Sum256(old.page) {
			return storagebudget.ErrStale
		}
	} else {
		prior, e := storageDecodeIdentityIntent(old.page, int(r.Core.Slot), m.phase.c.ref.cell.ownerRecord.Installation, m.phase.c.ref.cell.binding)
		if e != nil || !storageIdentityIntentTransition(prior, r, old.page) {
			return errors.Join(storagebudget.ErrStale, e)
		}
	}
	raw, err := storageEncodeIdentityIntent(r, m.phase.c.ref.cell.ownerRecord.Installation, m.phase.c.ref.cell.binding)
	if err != nil {
		return err
	}
	if err = m.hostCurrent(); err != nil {
		return err
	}
	if err = m.take(ctx, storageIdentityHostGroup(m.record.Action, m.hostWrites+1)); err != nil {
		return err
	}
	m.hostWrites++
	if err = m.enter(ctx); err != nil {
		return err
	}
	defer func() {
		err = m.phase.leaveNative(err)
		if err != nil {
			m.phase.identityLaunch.failed = errors.Join(m.phase.identityLaunch.failed, err)
		}
	}()
	write, err := storageOpenFile(m.host.root, m.host.name, m.host.size)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			err = errors.Join(err, write.file.Close())
		}
	}()
	if !os.SameFile(write.identity, m.host.identity) {
		return errStorageCustody
	}
	at := int64((int(r.Core.Slot)*2 + 1 - old.bank) * storageIntentPage)
	if err = m.phase.boundary("identity-host-write"); err != nil {
		return err
	}
	if err = m.phase.permit(ctx); err != nil {
		return err
	}
	if err = storageEnrollmentNativeWrite(ctx, write.file, raw, at); err != nil {
		return err
	}
	if err = m.phase.boundary("identity-host-sync"); err != nil {
		return err
	}
	if err = m.phase.permit(ctx); err != nil {
		return err
	}
	if err = write.file.Sync(); err != nil {
		return err
	}
	closed = true
	if err = errors.Join(write.file.Close(), m.phase.boundary("identity-host-close")); err != nil {
		return err
	}
	read, err := storageOpenFile(m.host.root, m.host.name, m.host.size)
	if err != nil {
		return err
	}
	back := make([]byte, storageIntentPage)
	n, e := read.file.ReadAt(back, at)
	err = errors.Join(e, read.current(), read.file.Close(), m.phase.boundary("identity-host-readback"))
	if err != nil || n != len(back) || !bytes.Equal(back, raw) {
		return errors.Join(errStorageCustody, err)
	}
	m.record = r
	m.selected = storageIntentSlot{record: r.Core, page: raw, bank: 1 - old.bank}
	m.phase.original.Hosts[r.Core.Slot][m.selected.bank] = append([]byte{}, raw...)
	point := ""
	switch {
	case r.Core.Phase == storageIntentRetired:
		point = "identity-retired-before-receipt"
	case r.NativePhase == storageIdentityAllocatedObserved:
		point = "identity-allocated-original-saved"
	case r.NativePhase == storageIdentityPayloadReady:
		point = "identity-payload-original-saved"
	case r.NativePhase == storageIdentityLinkedObserved:
		point = "identity-target-original-observed"
	case r.NativePhase == storageIdentityTargetDurable && r.Core.Phase == storageIntentPublishing:
		point = "identity-target-original-durable"
	}
	if point != "" {
		if err = m.phase.boundary(point); err != nil {
			return err
		}
	}
	return m.phase.permit(ctx)
}
func storageIdentityRequest(r storageIdentityIntent, binding storagebudget.Digest) (storagebudget.Request, error) {
	if r.validate() != nil {
		return storagebudget.Request{}, storagebudget.ErrBounds
	}
	e := storagebudget.Entry{Kind: storagebudget.ObjectEntry, Component: 0, Target: r.Core.Target, Stage: r.Core.Stage, OldIdentity: r.Core.CASBinding, NewIdentity: r.Core.CASBinding, Bytes: r.Core.NewLength, Files: 1}
	if r.Action == storageIdentityIntentStamp {
		e.Files = 0
		e.Directories = 1
	}
	return storagebudget.Request{Workflow: storageHostWorkflow, Operation: storageIntentOperation(r.Core), Owner: binding, Components: append([]storagebudget.Component{}, r.Core.Components...), Entries: []storagebudget.Entry{e}}, nil
}
func (p *storageEnrollmentEpoch) issueOriginalIdentity(ctx context.Context, action byte) (out *storageIdentityLive, err error) {
	if p == nil || p.identityPlan == nil || p.identityLaunch == nil || p.identity != nil || p.identityPrivate == nil || p.compositionCut != 4 {
		return nil, storagebudget.ErrAuthority
	}
	if err = p.held(ctx); err != nil {
		return nil, err
	}
	plan := *p.identityPlan
	digest, err := storageOwnerV5PlanDigest(plan)
	if err != nil {
		return nil, err
	}
	components, ok := p.identityApplications[action]
	if !ok {
		return nil, storagebudget.ErrAuthority
	}
	// P must already be the exact retained active lineage row.
	_, lineage, err := p.identityGraph()
	if err != nil {
		return nil, err
	}
	found := false
	for _, row := range lineage.Rows.LineagePlans {
		if row.PlanID == plan.PlanID {
			found = reflect.DeepEqual(row, plan)
		}
	}
	if !found {
		return nil, storagebudget.ErrAuthority
	}
	m := &storageIdentityLive{phase: p, parent: p.identityPrivate.root, payload: append([]byte{}, plan.IdentityInitial...), attempt: 1}
	m.self = m
	defer func() {
		if out == nil {
			err = errors.Join(err, m.Close())
		}
	}()
	if action == storageIdentityIntentMove {
		if p.move == nil || p.move.phase != p {
			return nil, storagebudget.ErrAuthority
		}
		_, m.payload, err = p.move.request.operands(lineage, p.move.before)
		if err != nil {
			return nil, err
		}
	}
	if action == storageIdentityIntentClear {
		m.payload = append([]byte{}, plan.IdentityFromCleared...)
	}
	for _, hold := range p.holds {
		if hold.root == p.identityPrivate {
			h := hold
			for h.borrowed != nil {
				h = h.borrowed
			}
			m.parentFile = h.file
		}
	}
	if m.parentFile != nil {
		native, e := storageAdvisoryPersistentHeld(ctx, m.parentFile)
		if e != nil {
			return nil, e
		}
		if native != plan.PrivateObject {
			m.parentFile = nil
		}
	}
	if m.parentFile == nil {
		m.parentFile, err = m.parent.Open(".")
		if err != nil {
			return nil, err
		}
		m.ownedParent = true
	}
	if action == storageIdentityIntentStamp {
		if m.ownedParent {
			if err = m.parentFile.Close(); err != nil {
				return nil, err
			}
			m.parentFile = nil
			m.ownedParent = false
		}
		if p.identityStamp == nil {
			return nil, storagebudget.ErrAuthority
		}
		if err = p.identityStamp.current(); err != nil {
			return nil, err
		}
		m.parent = p.identityStamp.root
		m.parentFile, err = m.parent.Open(".")
		if err != nil {
			return nil, err
		}
		m.ownedParent = true
		m.payload = nil
	}
	if m.parentFile == nil {
		return nil, storagebudget.ErrAuthority
	}
	core := storageIntentRecord{Phase: storageIntentPrepared, AdmissionEpoch: p.initialInspection.View.Configuration.Epoch, NewLength: uint64(len(m.payload)), Components: append([]storagebudget.Component{}, components...), Changes: make([]storagebudget.Change, len(components)), Target: startupIdentityName, Workflow: "desk-signing-lineage-v1"}
	if action == storageIdentityIntentMove {
		core.OperationID = p.move.request.Operation
	} else {
		core.OperationID, err = storagebudget.NewID()
	}
	if err != nil {
		return nil, err
	}
	core.Stage = identityStagingPrefix + hex.EncodeToString(core.OperationID[:])
	m.targetName, m.stageName = core.Target, core.Stage
	var sourceNative storageOwnerPersistentObject
	if action == storageIdentityIntentStamp {
		identity, e := parseIdentity(plan.IdentityInitial)
		if e != nil || identity.From == "" {
			return nil, storagebudget.ErrAuthority
		}
		if identity.From == identity.ID {
			return nil, errStorageCustody
		}
		source, e := storagePinChild(m.parent, identity.From, true)
		if e != nil {
			return nil, e
		}
		m.source = &source
		m.sourceFile, e = source.root.Open(".")
		if e != nil {
			return nil, e
		}
		sourceNative, e = storageAdvisoryPersistentHeld(ctx, m.sourceFile)
		if e != nil {
			return nil, e
		}
		if _, e = m.parent.Lstat(identity.ID); !errors.Is(e, os.ErrNotExist) {
			return nil, errors.Join(errStorageCustody, e)
		}
		m.targetName, m.stageName = identity.ID, identity.From
		core.Target, core.Stage = filepath.Join(stampingDirName, identity.ID), filepath.Join(stampingDirName, identity.From)
	}
	for _, c := range components {
		r := p.c.owner.roots[c.Root]
		if r == nil {
			return nil, errStorageCustody
		}
		core.RootIdentities = append(core.RootIdentities, r.row.Identity)
		core.FilesystemIdentities = append(core.FilesystemIdentities, p.c.owner.filesystems[c.Filesystem].Identity)
		core.Locators = append(core.Locators, r.row.Locator)
	}
	core.CASBinding = digest
	name := "create"
	presence := uint16(storageIdentityParentPresent)
	expected := storageOwnerPersistentObject{}
	if action == storageIdentityIntentInitialCAS || action == storageIdentityIntentClear || action == storageIdentityIntentMove {
		name = "initial-cas"
		expected = p.identityInitial.ExpectedOld
		oldBody := plan.IdentityInitial
		if action == storageIdentityIntentMove {
			name = "move"
			expected = p.move.request.ExpectedOld
			oldBody, _, err = p.move.request.operands(lineage, p.move.before)
			if err != nil {
				return nil, err
			}
			core.CASBinding, err = p.move.request.digest()
			if err != nil {
				return nil, err
			}
		}
		if action == storageIdentityIntentClear {
			name = "clear"
			for _, c := range lineage.Rows.LineageCompletions {
				if c.PlanID == plan.PlanID {
					expected = c.Initial.PublishedObject
				}
			}
			stamped := false
			for _, s := range lineage.Rows.LineageStampCompletions {
				stamped = stamped || s.PlanID == plan.PlanID
			}
			if !stamped {
				return nil, storagebudget.ErrAuthority
			}
			if e := p.identityStampAfterCurrent(ctx, lineage); e != nil {
				return nil, e
			}
		}
		raw, held, info, e := storageReadOriginalFile(m.parent, startupIdentityName, 512)
		if e != nil {
			return nil, e
		}
		if p.identityOriginal != nil {
			if originalInfo, e := p.identityOriginal.Stat(); e != nil || !os.SameFile(info, originalInfo) {
				held.Close()
				return nil, errStorageCustody
			}
			held.Close()
			held = p.identityOriginal
		} else {
			p.identityOriginal = held
		}
		m.old, m.oldInfo = held, info
		native, e := storageAdvisoryPersistentHeld(ctx, held)
		if e != nil || native != expected || !bytes.Equal(raw, oldBody) {
			return nil, errors.Join(errStorageCustody, e)
		}
		core.OldExists = true
		core.OldLength = uint64(len(raw))
		core.OldLinkCount = 1
		core.OldIdentity, e = storageFileIdentity(info)
		if e != nil {
			return nil, e
		}
		presence |= storageIdentityExpectedOldPresent
		if action == storageIdentityIntentInitialCAS {
			core.CASBinding, err = storageCompositionInitialBinding(plan, p.identityInitial)
			if err != nil {
				return nil, err
			}
		}
	} else if action == storageIdentityIntentStamp {
		name = "stamp"
		completed := false
		for _, c := range lineage.Rows.LineageCompletions {
			completed = completed || c.PlanID == plan.PlanID
		}
		if !completed {
			return nil, storagebudget.ErrAuthority
		}
	} else if action != storageIdentityIntentCreate || p.identityInitial.Mode != 1 || p.identityInitial.ExpectedOld != (storageOwnerPersistentObject{}) {
		return nil, storagebudget.ErrAuthority
	} else if _, e := m.parent.Lstat(startupIdentityName); !errors.Is(e, os.ErrNotExist) {
		return nil, errors.Join(errStorageCustody, e)
	}
	core.Operation = "p:" + hex.EncodeToString(digest[:]) + ":" + name
	chosen := -1
	for slot, pages := range p.original.Hosts {
		selected, e := storageSelectCreationHost(pages[0], pages[1], slot, plan.Installation, plan.QBinding)
		if e != nil {
			return nil, e
		}
		if !selected.Slot.record.reusable() {
			return nil, errStorageEnrollmentUnresolved
		}
		if selected.Identity != nil {
			if e = p.identityComponentMappings(ctx, selected.Identity.Core, selected.Identity.Core.Components[0].Root); e != nil {
				return nil, e
			}
			if !p.c.owner.identityReceiptCurrent(ctx, *selected.Identity, selected.Slot.page, lineage, storageHostState(p.initialInspection.View)) {
				return nil, errStorageEnrollmentUnresolved
			}
		}
		if selected.PredecessorIdentity != nil {
			if e = p.identityComponentMappings(ctx, selected.PredecessorIdentity.Core, selected.PredecessorIdentity.Core.Components[0].Root); e != nil {
				return nil, e
			}
			prior, e := storageEncodeIdentityIntent(*selected.PredecessorIdentity, plan.Installation, plan.QBinding)
			if e != nil || !p.c.owner.identityReceiptCurrent(ctx, *selected.PredecessorIdentity, prior, lineage, storageHostState(p.initialInspection.View)) {
				return nil, errors.Join(errStorageEnrollmentUnresolved, e)
			}
		}
		if chosen < 0 && (selected.Creation == nil || p.c.owner.creationTailCurrent(ctx, *selected.Creation, selected.Slot.page, storageHostState(p.initialInspection.View))) {
			chosen = slot
			m.selected = selected.Slot
		}
	}
	if chosen < 0 {
		return nil, storagebudget.ErrBounds
	}
	core.Slot = uint16(chosen)
	core.Generation = 1
	if m.selected.record.Phase != storageIntentEmpty {
		if m.selected.record.Generation == ^uint64(0) {
			return nil, storagebudget.ErrBounds
		}
		core.Generation = m.selected.record.Generation + 1
		core.PreviousPageDigest = sha256.Sum256(m.selected.page)
	}
	m.record = storageIdentityIntent{Core: core, Action: action, PlanDigest: digest, PayloadDigest: sha256.Sum256(m.payload), Parent: plan.PrivateObject, ExpectedOld: expected, Presence: presence}
	if action == storageIdentityIntentMove {
		m.record.MoveTails, err = p.move.tailJournal(components)
		if err != nil {
			return nil, err
		}
		if !p.move.request.identityBound(m.record, lineage, p.move.before) {
			return nil, storagebudget.ErrAuthority
		}
	}
	if action == storageIdentityIntentStamp {
		m.record.Parent, err = storageAdvisoryPersistentHeld(ctx, m.parentFile)
		if err != nil {
			return nil, err
		}
		m.record.PayloadDigest = storagebudget.Digest{}
		m.record.AllocatedStage = sourceNative
		m.record.Presence = 3
	}
	for _, control := range p.c.original.files {
		if control.root == p.c.original.config && control.name == storageIntentName {
			m.host = control
		}
		if control.name == storageIntentName || control.root == p.c.anchor && (control.name == "tickets.a" || control.name == "tickets.b" || control.name == "manifests") {
			if err = control.current(); err != nil {
				return nil, err
			}
			if err = storageEnrollmentNativeDense(ctx, control.file, uint64(control.size)); err != nil {
				return nil, err
			}
		}
	}
	if m.host.file == nil {
		return nil, errStorageCustody
	}
	m.request, err = storageIdentityRequest(m.record, plan.QBinding)
	if err != nil {
		return nil, err
	}
	ids := map[storagebudget.ID]bool{}
	for _, c := range components {
		for i := 0; i < int(c.CapacityCount); i++ {
			ids[c.Capacity[i].Constraint] = true
		}
	}
	ordered := make([]storagebudget.ID, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Slice(ordered, func(i, j int) bool { return bytes.Compare(ordered[i][:], ordered[j][:]) < 0 })
	m.observations, err = p.originalEnrollmentObservations(ctx, ordered)
	if err != nil {
		return nil, err
	}
	p.identity = m
	if err = m.current(ctx); err != nil {
		p.identity = nil
		return nil, err
	}
	return m, nil
}

// A disk-applied terminal retires Manifest even while this exact ticket is
// current. Reconstruct its original immutable request commitment from J and
// compare the complete independently loaded terminal/applied/next tuple. Live
// or unapplied requests still require the ordinary strict Manifest lookup.
func storageOriginalActionRequest(ctx context.Context, q *storagebudget.Coordinator, core storageIntentRecord, request storagebudget.Request, status storagebudget.HostState, token storagebudget.Token) error {
	if int(token.Slot) >= len(status.Slots) || status.Epoch != core.AdmissionEpoch {
		return errStorageEnrollmentUnresolved
	}
	row := status.Slots[token.Slot]
	t := row.Ticket
	if t.Token != token || t.Owner != request.Owner || t.Workflow != request.Workflow || t.Operation != request.Operation || !reflect.DeepEqual(t.Components, request.Components) {
		return errStorageEnrollmentUnresolved
	}
	if t.State != storagebudget.Terminal || row.PersistedApplied != token {
		actual, err := q.Manifest(ctx, token, request.Owner)
		if err != nil || !reflect.DeepEqual(actual, request) {
			return errors.Join(errStorageEnrollmentUnresolved, err)
		}
		return nil
	}
	if core.Phase < storageIntentSettling || core.Phase > storageIntentRetired || t.PolicyRevision != core.PolicyRevision || t.Evidence != core.Evidence || !reflect.DeepEqual(t.Changes, core.Changes) {
		return errStorageEnrollmentUnresolved
	}
	staging := core.Token
	if core.Phase >= storageIntentSettled {
		staging = core.PreviousToken
	}
	if !storageIntentNextToken(staging, token) || staging.Generation <= 1 {
		return errStorageEnrollmentUnresolved
	}
	initial := staging
	initial.Generation-- // these original J3/J5 routes advance Reserved -> Staging exactly once
	return q.ValidateAppliedRequest(ctx, status.Epoch, row, initial, request)
}

func (m *storageIdentityLive) ticket(ctx context.Context, token storagebudget.Token, state storagebudget.State) (storagebudget.Ticket, error) {
	host, err := m.phase.c.ref.cell.q.InspectHostState(ctx)
	if err != nil {
		return storagebudget.Ticket{}, err
	}
	t := host.Slots[token.Slot].Ticket
	if host.Epoch != m.record.Core.AdmissionEpoch || t.Token != token || t.State != state || t.Owner != m.request.Owner || t.Workflow != m.request.Workflow || t.Operation != m.request.Operation || !reflect.DeepEqual(t.Components, m.request.Components) {
		return storagebudget.Ticket{}, errStorageEnrollmentUnresolved
	}
	if err = storageOriginalActionRequest(ctx, m.phase.c.ref.cell.q, m.record.Core, m.request, host, token); err != nil {
		return storagebudget.Ticket{}, err
	}
	return t, m.current(ctx)
}
func (m *storageIdentityLive) admit(ctx context.Context) (err error) {
	if m.record.Core.Phase != storageIntentPrepared {
		return storagebudget.ErrAuthority
	}
	if m.record.Action == storageIdentityIntentMove {
		op := m.phase.move
		if op == nil || op.phase != m.phase || !op.intentClaimed || op.admissionAttempted || m.hostWrites != 0 || m.admitted != (storagebudget.Token{}) {
			return storagebudget.ErrAuthority
		}
		op.admissionAttempted = true // no retry after any compare/Prepared/Q outcome
	} else if m.started {
		return storagebudget.ErrAuthority
	}
	var comparison storagebudget.ReserveComparison
	if m.record.Action == storageIdentityIntentMove {
		err = m.phase.c.owner.withRegisteredMoveControl(ctx, m.phase.move, func(ctx context.Context, table *storageIntentTable, original *storagePolicyOriginal) error {
			var e error
			comparison, e = m.phase.c.owner.captureOriginalReserveOriginal(ctx, m.request, table, m.record.Core.OperationID, &m.record, original)
			return e
		})
	} else {
		comparison, err = m.phase.c.owner.captureOriginalReserve(ctx, m.request, nil, m.record.Core.OperationID, &m.record)
	}
	if err != nil {
		return err
	}
	if err = m.publish(ctx, m.record); err != nil {
		return err
	}
	if err = m.take(ctx, 20); err != nil {
		return err
	}
	if err = m.take(ctx, 21); err != nil {
		return err
	}
	if err = m.enter(ctx); err != nil {
		return err
	}
	token, e := m.phase.c.ref.cell.q.ReserveCompared(ctx, m.request, m.observations, comparison)
	err = m.phase.leaveNative(e)
	if err != nil {
		return err
	}
	m.admitted = token
	t, err := m.ticket(ctx, token, storagebudget.Reserved)
	if err != nil {
		return err
	}
	r := m.next(storageIntentReserved)
	r.Core.Token = token
	r.Core.PolicyRevision = t.PolicyRevision
	if err = m.publish(ctx, r); err != nil {
		return err
	}
	if err = m.take(ctx, 22); err != nil {
		return err
	}
	if err = m.enter(ctx); err != nil {
		return err
	}
	advanced, e := m.phase.c.ref.cell.q.Advance(ctx, token, m.request.Owner, storagebudget.Staging, r.Core.CASBinding)
	err = m.phase.leaveNative(e)
	if err != nil {
		return err
	}
	m.admitted = advanced
	t, err = m.ticket(ctx, advanced, storagebudget.Staging)
	if err != nil || t.Evidence != r.Core.CASBinding {
		return errors.Join(errStorageEnrollmentUnresolved, err)
	}
	r = m.next(storageIntentStaging)
	r.Core.PreviousToken = token
	r.Core.Token = advanced
	return m.publish(ctx, r)
}

func (m *storageIdentityLive) native(ctx context.Context, group uint16, fn func() error) (err error) {
	if err = m.take(ctx, group); err != nil {
		return err
	}
	if err = m.enter(ctx); err != nil {
		return err
	}
	defer func() {
		err = m.phase.leaveNative(err)
		if err != nil {
			m.phase.identityLaunch.failed = errors.Join(m.phase.identityLaunch.failed, err)
		}
	}()
	if err = m.phase.boundary("identity-native"); err != nil {
		return err
	}
	if err = m.phase.permit(ctx); err != nil {
		return err
	}
	return fn()
}
func (m *storageIdentityLive) syncParent(ctx context.Context) (err error) {
	f, err := m.parent.Open(".")
	if err != nil {
		return err
	}
	native, e := storageAdvisoryPersistentHeld(ctx, f)
	if e == nil && native != m.record.Parent {
		e = errStorageCustody
	}
	if e == nil {
		e = m.phase.permit(ctx)
	}
	if e == nil {
		e = f.Sync()
	}
	return errors.Join(e, f.Close(), m.phase.boundary("identity-parent-close"))
}
func (m *storageIdentityLive) fileObservation(ctx context.Context, name string, links uint64, want storageOwnerPersistentObject, body bool) (storageOwnerPersistentObject, error) {
	info, err := m.parent.Lstat(name)
	if err != nil || !storageCensusPrivateFile(info) || m.allocated == nil || m.allocatedInfo == nil || !os.SameFile(info, m.allocatedInfo) {
		return storageOwnerPersistentObject{}, errors.Join(errStorageCustody, err)
	}
	count, known := linkCount(info)
	if !known || uint64(count) != links {
		return storageOwnerPersistentObject{}, errStorageCustody
	}
	held, err := m.allocated.Stat()
	if err != nil || !os.SameFile(held, info) {
		return storageOwnerPersistentObject{}, errors.Join(errStorageCustody, err)
	}
	// The ordinary fixed-control opener intentionally requires nlink1. This
	// exact original allocation observer additionally handles the J3 nlink2
	// interval, without weakening that opener or adopting a named inode.
	current, err := m.parent.OpenFile(name, os.O_RDONLY|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return storageOwnerPersistentObject{}, err
	}
	opened, e := current.Stat()
	if e == nil && (!os.SameFile(opened, info) || !os.SameFile(opened, m.allocatedInfo)) {
		e = errStorageCustody
	}
	var native storageOwnerPersistentObject
	if e == nil {
		native, e = storageAdvisoryPersistentHeld(ctx, current)
	}
	if e == nil && want != (storageOwnerPersistentObject{}) && native != want {
		e = errStorageCustody
	}
	if e == nil && body {
		raw := make([]byte, len(m.payload))
		n, readErr := current.ReadAt(raw, 0)
		if readErr != nil || n != len(raw) || uint64(info.Size()) != uint64(len(raw)) || !bytes.Equal(raw, m.payload) {
			e = errors.Join(errStorageCustody, readErr)
		}
	}
	after, check := m.parent.Lstat(name)
	if check == nil {
		count, known := linkCount(after)
		if !storageCensusPrivateFile(after) || after.Mode().Perm()&0600 != 0600 || !os.SameFile(after, info) || !known || uint64(count) != links || after.Size() != info.Size() {
			check = errStorageCustody
		}
	}
	e = errors.Join(e, check, current.Close())
	return native, e
}
func (m *storageIdentityLive) oldCurrent(ctx context.Context) error {
	if m.old == nil || m.oldInfo == nil {
		return storagebudget.ErrAuthority
	}
	info, err := m.parent.Lstat(m.targetName)
	held, e := m.old.Stat()
	if err != nil || e != nil || !os.SameFile(info, m.oldInfo) || !os.SameFile(held, info) {
		return errors.Join(errStorageCustody, err, e)
	}
	identity, e := storageFileIdentity(info)
	links, known := linkCount(info)
	native, e2 := storageAdvisoryPersistentHeld(ctx, m.old)
	if e != nil || e2 != nil || identity != m.record.Core.OldIdentity || native != m.record.ExpectedOld || !known || links != 1 || uint64(info.Size()) != m.record.Core.OldLength {
		return errors.Join(errStorageCustody, e, e2)
	}
	raw := make([]byte, m.record.Core.OldLength)
	n, e := m.old.ReadAt(raw, 0)
	oldBody := m.phase.identityPlan.IdentityInitial
	if m.record.Action == storageIdentityIntentMove {
		if m.phase.move == nil {
			return storagebudget.ErrAuthority
		}
		oldBody, _, e = m.phase.move.request.operands(m.phase.move.original.Lineage, m.phase.move.before)
		if e != nil {
			return e
		}
	}
	if e != nil || n != len(raw) || !bytes.Equal(raw, oldBody) {
		return errors.Join(errStorageCustody, e)
	}
	return m.current(ctx)
}
func (m *storageIdentityLive) publishIdentity(ctx context.Context) error {
	return m.publishIdentityTo(ctx, false)
}
func (m *storageIdentityLive) publishIdentityTo(ctx context.Context, stageOnly bool) error {
	if stageOnly && (m.record.Action != storageIdentityIntentMove || m.phase.move == nil) {
		return storagebudget.ErrAuthority
	}
	if m.record.Core.Phase != storageIntentStaging && m.record.Core.Phase != storageIntentPublishing {
		return storagebudget.ErrAuthority
	}
	var r storageIdentityIntent
	var err error
	renameOwed := false
	if m.record.NativePhase == storageIdentityNativeNone {
		if m.recovering {
			return errStorageEnrollmentUnresolved
		}
		if err := m.native(ctx, 30, func() error {
			if _, err := m.parent.Lstat(m.stageName); !errors.Is(err, os.ErrNotExist) {
				return errors.Join(errStorageCustody, err)
			}
			if m.record.Action == storageIdentityIntentCreate {
				if _, err := m.parent.Lstat(m.targetName); !errors.Is(err, os.ErrNotExist) {
					return errors.Join(errStorageCustody, err)
				}
			} else if err := m.oldCurrent(ctx); err != nil {
				return err
			}
			f, err := m.parent.OpenFile(m.stageName, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				return err
			}
			m.allocated = f
			m.allocatedInfo, err = f.Stat()
			if err != nil {
				return err
			}
			count, known := linkCount(m.allocatedInfo)
			if !known || count != 1 || !storageCensusPrivateFile(m.allocatedInfo) {
				return errStorageCustody
			}
			return nil
		}); err != nil {
			return err
		}
		native, e := storageAdvisoryPersistentHeld(ctx, m.allocated)
		err = e
		if err != nil {
			return err
		}
		r = m.next(storageIntentStaging)
		r.NativePhase = storageIdentityAllocatedObserved
		r.AllocatedStage = native
		r.AllocatedLinks = 1
		r.Presence |= storageIdentityAllocatedPresent
		r.Core.StageExists = true
		if err = m.publish(ctx, r); err != nil {
			return err
		}
	}
	if m.record.NativePhase == storageIdentityAllocatedObserved {
		if err = m.native(ctx, 31, func() (err error) {
			if _, err = m.fileObservation(ctx, m.stageName, 1, m.record.AllocatedStage, false); err != nil {
				return err
			}
			info, e := m.parent.Lstat(m.stageName)
			if e != nil || info.Size() < 0 || info.Size() > int64(len(m.payload)) {
				return errors.Join(errStorageCustody, e)
			}
			write, err := storageOpenFile(m.parent, m.stageName, info.Size())
			if err != nil {
				return err
			}
			closed := false
			defer func() {
				if !closed {
					err = errors.Join(err, write.file.Close())
				}
			}()
			if err = storageEnrollmentNativeWrite(ctx, write.file, m.payload, 0); err != nil {
				return err
			}
			if err = m.phase.permit(ctx); err != nil {
				return err
			}
			if err = write.file.Chmod(0600); err != nil {
				return err
			}
			if err = m.phase.permit(ctx); err != nil {
				return err
			}
			if err = write.file.Sync(); err != nil {
				return err
			}
			closed = true
			if err = errors.Join(write.file.Close(), m.phase.boundary("identity-payload-close")); err != nil {
				return err
			}
			if _, err = m.fileObservation(ctx, m.stageName, 1, m.record.AllocatedStage, true); err != nil {
				return err
			}
			return m.syncParent(ctx)
		}); err != nil {
			return err
		}
		r = m.next(storageIntentStaging)
		r.NativePhase = storageIdentityPayloadReady
		r.Facts = storageIdentityPayloadFacts
		if err = m.publish(ctx, r); err != nil {
			return err
		}
	}
	if stageOnly {
		if m.record.NativePhase != storageIdentityPayloadReady {
			return storagebudget.ErrAuthority
		}
		return m.current(ctx)
	}
	if m.record.NativePhase == storageIdentityPayloadReady && m.record.Action != storageIdentityIntentCreate {
		r = m.next(storageIntentStaging)
		r.NativePhase = storageIdentityEntered
		if err = m.publish(ctx, r); err != nil {
			return err
		}
		renameOwed = true
	}
	if m.record.NativePhase == storageIdentityPayloadReady || m.record.NativePhase == storageIdentityEntered {
		var stage, target storageOwnerPersistentObject
		if err = m.native(ctx, 32, func() (err error) {
			if m.recovering && m.record.NativePhase == storageIdentityEntered && !renameOwed {
				if _, e := m.parent.Lstat(m.stageName); !errors.Is(e, os.ErrNotExist) {
					return errors.Join(errStorageEnrollmentUnresolved, e)
				}
				target, err = m.fileObservation(ctx, m.targetName, 1, m.record.AllocatedStage, true)
				return err
			}
			if m.record.Action == storageIdentityIntentCreate && m.recovering {
				if _, e := m.parent.Lstat(m.targetName); e == nil {
					stage, err = m.fileObservation(ctx, m.stageName, 2, m.record.AllocatedStage, true)
					if err != nil {
						return err
					}
					target, err = m.fileObservation(ctx, m.targetName, 2, m.record.AllocatedStage, true)
					return err
				} else if !errors.Is(e, os.ErrNotExist) {
					return e
				}
			}
			if _, err = m.fileObservation(ctx, m.stageName, 1, m.record.AllocatedStage, true); err != nil {
				return err
			}
			if m.record.Action == storageIdentityIntentCreate {
				if _, e := m.parent.Lstat(m.targetName); !errors.Is(e, os.ErrNotExist) {
					return errors.Join(errStorageCustody, e)
				}
				if err = m.phase.permit(ctx); err != nil {
					return err
				}
				if err = m.parent.Link(m.stageName, m.targetName); err != nil {
					return err
				}
				stage, err = m.fileObservation(ctx, m.stageName, 2, m.record.AllocatedStage, true)
				if err != nil {
					return err
				}
				target, err = m.fileObservation(ctx, m.targetName, 2, m.record.AllocatedStage, true)
				return err
			}
			if err = m.oldCurrent(ctx); err != nil {
				return err
			}
			if err = m.phase.permit(ctx); err != nil {
				return err
			}
			if err = m.parent.Rename(m.stageName, m.targetName); err != nil {
				return err
			}
			if _, e := m.parent.Lstat(m.stageName); !errors.Is(e, os.ErrNotExist) {
				return errors.Join(errStorageCustody, e)
			}
			target, err = m.fileObservation(ctx, m.targetName, 1, m.record.AllocatedStage, true)
			if err == nil {
				err = m.phase.boundary("identity-rename-before-observed-page")
			}
			return err
		}); err != nil {
			return err
		}
		r = m.next(storageIntentPublishing)
		r.NativePhase = storageIdentityLinkedObserved
		r.Target = target
		r.Presence |= storageIdentityTargetPresent
		if m.record.Action == storageIdentityIntentCreate {
			r.LinkedStage = stage
			r.LinkedStageLinks = 2
			r.LinkedTargetLinks = 2
			r.Presence |= storageIdentityLinkedPresent
		} else {
			r.LinkedTargetLinks = 1
		}
		if err = m.publish(ctx, r); err != nil {
			return err
		}
	}
	if m.record.NativePhase != storageIdentityLinkedObserved {
		if m.record.NativePhase == storageIdentityTargetDurable {
			return m.recoveryTargetCurrent(ctx)
		}
		return storagebudget.ErrAuthority
	}
	if err = m.native(ctx, 33, func() error {
		if err := m.syncParent(ctx); err != nil {
			return err
		}
		if m.record.Action == storageIdentityIntentCreate {
			if _, e := m.parent.Lstat(m.stageName); e == nil {
				if _, err := m.fileObservation(ctx, m.stageName, 2, m.record.AllocatedStage, true); err != nil {
					return err
				}
				if err := m.phase.permit(ctx); err != nil {
					return err
				}
				if err := m.parent.Remove(m.stageName); err != nil {
					return err
				}
			} else if !m.recovering || !errors.Is(e, os.ErrNotExist) {
				return errors.Join(errStorageCustody, e)
			}
		}
		if _, err := m.parent.Lstat(m.stageName); !errors.Is(err, os.ErrNotExist) {
			return errors.Join(errStorageCustody, err)
		}
		return nil
	}); err != nil {
		return err
	}
	if err = m.native(ctx, 34, func() (err error) {
		if _, err = m.fileObservation(ctx, m.targetName, 1, m.record.Target, true); err != nil {
			return err
		}
		f, err := storageOpenFile(m.parent, m.targetName, int64(len(m.payload)))
		if err != nil {
			return err
		}
		if err = m.phase.permit(ctx); err == nil {
			err = f.file.Sync()
		}
		err = errors.Join(err, f.file.Close(), m.phase.boundary("identity-target-close"))
		if err != nil {
			return err
		}
		_, err = m.fileObservation(ctx, m.targetName, 1, m.record.Target, true)
		return err
	}); err != nil {
		return err
	}
	if err = m.native(ctx, 35, func() error {
		if err := m.syncParent(ctx); err != nil {
			return err
		}
		if m.old != nil {
			info, err := m.old.Stat()
			if err != nil {
				return err
			}
			links, known := linkCount(info)
			if !known || links != 0 {
				return errStorageCustody
			}
			old := m.old
			m.old = nil
			if m.phase.identityOriginal == old {
				m.phase.identityOriginal = nil
			}
			if err = old.Close(); err != nil {
				return err
			}
		}
		_, err := m.fileObservation(ctx, m.targetName, 1, m.record.Target, true)
		return err
	}); err != nil {
		return err
	}
	r = m.next(storageIntentPublishing)
	r.NativePhase = storageIdentityTargetDurable
	r.Facts = storageIdentityPublishedFacts
	r.FinalTargetLinks = 1
	return m.publish(ctx, r)
}
func (m *storageIdentityLive) settle(ctx context.Context) (err error) {
	if m.record.NativePhase != storageIdentityTargetDurable && m.record.NativePhase != storageIdentityAbortObserved {
		return storagebudget.ErrAuthority
	}
	if m.record.Core.Phase == storageIntentPublishing || m.record.Core.Phase == storageIntentStaging {
		r := m.next(storageIntentSettling)
		r.Core.Outcome = storageIntentPublished
		if r.NativePhase == storageIdentityAbortObserved {
			r.Core.Outcome = storageIntentAborted
		}
		r.Core.Changes, err = storageIdentitySettlementChanges(r)
		if err != nil {
			return err
		}
		domain := "desk-storage-identity-publication-v1"
		if r.Action != storageIdentityIntentCreate {
			domain = "desk-storage-identity-action-publication-v2"
		}
		r.Core.Evidence, err = storageOwnerDigest(domain, storageIdentityPublicationMaterial(r))
		if err != nil {
			return err
		}
		if err = m.publish(ctx, r); err != nil {
			return err
		}
	}
	if m.record.Core.Phase == storageIntentSettling {
		r := m.record
		if err = m.take(ctx, 23); err != nil {
			return err
		}
		if err = m.take(ctx, 24); err != nil {
			return err
		}
		if err = m.enter(ctx); err != nil {
			return err
		}
		// Q explicitly permits this exact factual persistence request to repeat;
		// no native operation or Reserve/Advance is repeated by this recovery.
		terminal, e := m.phase.c.ref.cell.q.Settle(ctx, storagebudget.SettlementFact{Token: r.Core.Token, Owner: m.request.Owner, Evidence: r.Core.Evidence, Changes: r.Core.Changes})
		err = m.phase.leaveNative(e)
		if err != nil {
			return err
		}
		m.admitted = terminal
		if err = m.phase.boundary("identity-terminal-before-settled"); err != nil {
			return err
		}
		ticket, e := m.ticket(ctx, terminal, storagebudget.Terminal)
		if e != nil || ticket.Evidence != r.Core.Evidence || !reflect.DeepEqual(ticket.Changes, r.Core.Changes) {
			return errors.Join(errStorageEnrollmentUnresolved, e)
		}
		m.phase.markApplication(m.phase.identityPlan.PlanID, m.record.Action, true, false)
		next := m.next(storageIntentSettled)
		next.Core.PreviousToken = r.Core.Token
		next.Core.Token = terminal
		if err = m.publish(ctx, next); err != nil {
			return err
		}
	}
	if m.record.Core.Phase == storageIntentSettled {
		r := m.record
		if err = m.take(ctx, 25); err != nil {
			return err
		}
		if err = m.enter(ctx); err != nil {
			return err
		}
		e := m.phase.c.ref.cell.q.Checkpoint(ctx)
		err = m.phase.leaveNative(e)
		if err != nil {
			return err
		}
		if err = m.phase.boundary("identity-checkpoint-before-retired"); err != nil {
			return err
		}
		host, e := m.phase.c.ref.cell.q.InspectHostState(ctx)
		if e != nil {
			return e
		}
		row := host.Slots[r.Core.Token.Slot]
		if host.Epoch != r.Core.AdmissionEpoch || row.PersistedApplied != r.Core.Token || row.PersistedNextSequence <= r.Core.Token.Sequence {
			return errStorageEnrollmentUnresolved
		}
		next := m.next(storageIntentRetired)
		next.Core.RetirementEpoch = host.Epoch
		if err = m.publish(ctx, next); err != nil {
			return err
		}
	}
	if m.record.Core.Phase != storageIntentRetired {
		return storagebudget.ErrAuthority
	}
	host, err := m.phase.c.ref.cell.q.InspectHostState(ctx)
	if err != nil || !storageIdentityIntentExactRetired(m.record, host, m.request.Owner) {
		return errors.Join(errStorageEnrollmentUnresolved, err)
	}
	m.phase.markApplication(m.phase.identityPlan.PlanID, m.record.Action, true, false)
	return m.current(ctx)
}
func (m *storageIdentityLive) execute(ctx context.Context) error {
	if m.recovering {
		if err := m.recoveryNativeCurrent(ctx); err != nil {
			return err
		}
		if err := m.persistOriginalAction(ctx); err != nil {
			return err
		}
		if err := m.persistOriginalQ(ctx); err != nil {
			return err
		}
	} else if err := m.admit(ctx); err != nil {
		return err
	}
	if m.record.NativePhase == storageIdentityAbortPrepared {
		if err := m.completeOriginalAbort(ctx); err != nil {
			return err
		}
	} else if m.record.NativePhase != storageIdentityAbortObserved && m.record.Core.Phase < storageIntentSettling {
		if m.record.Action == storageIdentityIntentStamp {
			if err := m.publishStamp(ctx); err != nil {
				return err
			}
		} else if err := m.publishIdentity(ctx); err != nil {
			return err
		}
	}
	return m.settle(ctx)
}

// These four writes use the unchanged permanent physical wrapper sequence.
// The supplied lineage is checked as exactly one append against both retained
// original banks; the frozen core and each prior row/initial operand survive.
func (p *storageEnrollmentEpoch) appendIdentityLineage(ctx context.Context, lineage storageCompositionLineage, transition byte, keys [4]string) error {
	if p.identityPlan == nil || p.identityLaunch == nil || p.compositionCut != 4 {
		return storagebudget.ErrAuthority
	}
	record, claim := *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim
	if storagePolicyActualPair(p.original.Catalogs) {
		graph, _, err := p.identityGraph()
		if err != nil || p.previousEvidence == nil {
			return errors.Join(errStorageEnrollmentUnresolved, err)
		}
		selected := byte(0)
		if storageEnrollmentPeakU64(p.original.Catalogs[1][16:24]) > storageEnrollmentPeakU64(p.original.Catalogs[0][16:24]) {
			selected = 1
		}
		raws, err := storageOwnerV6NativeAppend(p.original.Catalogs, selected, p.previousEvidence.Raw, graph, lineage, transition, p.identityPlan.PlanID, record, claim)
		if err != nil {
			return err
		}
		for i, group := range []struct {
			bank  byte
			write bool
		}{{1 - selected, true}, {1 - selected, false}, {selected, true}, {selected, false}} {
			if err = p.identityCatalogGroup(ctx, keys[i], group.bank, raws[group.bank], group.write); err != nil {
				return err
			}
		}
		pair, err := storageOwnerV6NativeReadPair(p.original.Catalogs[0], p.original.Catalogs[1], nil, nil, record, claim)
		if err != nil || pair.RecoveryOnly || !reflect.DeepEqual(pair.Banks[pair.Selected].Stage.Lineage, lineage) {
			return errors.Join(errStorageCustody, err)
		}
		p.identityCatalogsKnown()
		return p.held(ctx)
	}
	pair, err := storageCompositionReadPair(p.original.Catalogs[0], p.original.Catalogs[1], record, claim)
	if err != nil || pair.RecoveryOnly {
		return errors.Join(errStorageEnrollmentUnresolved, err)
	}
	graph, _, err := p.identityGraph()
	if err != nil {
		return err
	}
	if err = lineage.validateGraph(graph, record); err != nil {
		return err
	}
	selected := pair.Banks[pair.Selected]
	if selected.Sequence > ^uint64(0)-2 {
		return storagebudget.ErrBounds
	}
	rawLineage, err := lineage.encode()
	if err != nil {
		return err
	}
	prepared := selected
	prepared.Sequence++
	prepared.Publication = storageCompositionPrepared
	prepared.Transition = transition
	prepared.TransitionID = p.identityPlan.PlanID
	prepared.Previous, err = storageCompositionChecksumOf(selected.Raw)
	if err != nil {
		return err
	}
	prepared.Lineage = lineage
	prepared.LineageDigest = sha256.Sum256(rawLineage)
	prepared.Raw = nil
	if !storageCompositionLineageAppend(selected, prepared) {
		return storagebudget.ErrStale
	}
	preparedRaw, err := prepared.encode(record, claim)
	if err != nil {
		return err
	}
	prepared, err = storageCompositionDecode(preparedRaw, record, claim)
	if err != nil {
		return err
	}
	active := prepared
	active.Sequence++
	active.Publication = storageCompositionActive
	active.Previous, err = storageCompositionChecksumOf(preparedRaw)
	if err != nil {
		return err
	}
	if !storageCompositionFinalize(prepared, active) {
		return storagebudget.ErrStale
	}
	activeRaw, err := active.encode(record, claim)
	if err != nil {
		return err
	}
	bank := byte(1 - pair.Selected)
	if err = p.identityCatalogGroup(ctx, keys[0], bank, preparedRaw, true); err != nil {
		return err
	}
	if err = p.identityCatalogGroup(ctx, keys[1], bank, preparedRaw, false); err != nil {
		return err
	}
	if err = p.identityCatalogGroup(ctx, keys[2], byte(pair.Selected), activeRaw, true); err != nil {
		return err
	}
	if err = p.identityCatalogGroup(ctx, keys[3], byte(pair.Selected), activeRaw, false); err != nil {
		return err
	}
	pair, err = storageCompositionReadPair(p.original.Catalogs[0], p.original.Catalogs[1], record, claim)
	if err != nil || pair.RecoveryOnly || !reflect.DeepEqual(pair.Banks[pair.Selected].Lineage, lineage) {
		return errors.Join(errStorageCustody, err)
	}
	p.identityCatalogsKnown()
	return p.held(ctx)
}
func (p *storageEnrollmentEpoch) identityCatalogGroup(ctx context.Context, key string, bank byte, raw []byte, write bool) (err error) {
	if bank > 1 || len(raw) != storageOwnerCatalogBytes || p.identityPlan == nil {
		return storagebudget.ErrAuthority
	}
	for i, before := range p.original.Catalogs {
		if err = p.compositionCatalogCurrent(ctx, byte(i), before); err != nil {
			return err
		}
	}
	if err = p.takeIdentityCatalog(ctx, key, bank); err != nil {
		return err
	}
	p.mu.Lock()
	if !p.running || p.entered || p.revoked || p.finishing || p.err != nil {
		p.mu.Unlock()
		return storagebudget.ErrAuthority
	}
	p.entered = true
	p.compositionStarted = true
	p.mu.Unlock()
	defer func() {
		err = p.leaveNative(err)
		if err != nil {
			p.identityLaunch.failed = errors.Join(p.identityLaunch.failed, err)
		}
	}()
	target := p.targets[4+bank]
	f, err := storageOpenFile(target.named.root, target.named.name, storageOwnerCatalogBytes)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			err = errors.Join(err, f.file.Close())
		}
	}()
	native, e := storageOwnerPersistentFile(ctx, f)
	if e != nil || native != target.native || !os.SameFile(f.identity, target.named.identity) {
		return errors.Join(errStorageCustody, e)
	}
	if write {
		p.identityLaunch.catalogsKnown = false
		for at := 0; at < len(raw); at += 65536 {
			if err = p.boundary("identity-catalog-write"); err != nil {
				return err
			}
			if err = p.permit(ctx); err != nil {
				return err
			}
			if err = storageEnrollmentNativeWrite(ctx, f.file, raw[at:at+65536], int64(at)); err != nil {
				return err
			}
		}
	} else {
		if err = p.boundary("identity-catalog-sync"); err != nil {
			return err
		}
		if err = p.permit(ctx); err != nil {
			return err
		}
		if err = f.file.Sync(); err != nil {
			return err
		}
	}
	closed = true
	if err = errors.Join(f.file.Close(), p.boundary("identity-catalog-close")); err != nil {
		return err
	}
	if !write {
		parent, e := p.c.original.config.Open(".")
		if e != nil {
			return e
		}
		n, e := storageAdvisoryPersistentHeld(ctx, parent)
		if e == nil && n != p.identityPlan.ConfigObject {
			e = errStorageCustody
		}
		if e == nil {
			e = p.permit(ctx)
		}
		if e == nil {
			e = parent.Sync()
		}
		if err = errors.Join(e, parent.Close(), p.boundary("identity-catalog-parent-close")); err != nil {
			return err
		}
	}
	if err = p.compositionCatalogCurrent(ctx, bank, raw); err != nil {
		return err
	}
	p.original.Catalogs[bank] = append([]byte{}, raw...)
	if !write {
		if err = p.boundary("identity-catalog/" + key); err != nil {
			return err
		}
	}
	return p.held(ctx)
}
func (p *storageEnrollmentEpoch) persistIdentityPlan(ctx context.Context) error {
	_, lineage, err := p.identityGraph()
	if err != nil {
		return err
	}
	for _, row := range lineage.Rows.LineagePlans {
		if row.PlanID == p.identityPlan.PlanID {
			if !reflect.DeepEqual(row, *p.identityPlan) {
				return errStorageCustody
			}
			return nil
		}
	}
	lineage.Rows.LineagePlans = append(lineage.Rows.LineagePlans, *p.identityPlan)
	sort.Slice(lineage.Rows.LineagePlans, func(i, j int) bool {
		return bytes.Compare(lineage.Rows.LineagePlans[i].PlanID[:], lineage.Rows.LineagePlans[j].PlanID[:]) < 0
	})
	lineage.Order[0] = append(lineage.Order[0], p.identityPlan.PlanID)
	lineage.Initial = append(lineage.Initial, p.identityInitial)
	return p.appendIdentityLineage(ctx, lineage, storageCompositionPlan, [4]string{"lin4/plan-prepared/publish", "lin4/plan-prepared/persist", "lin4/plan-active/publish", "lin4/plan-active/persist"})
}
func (m *storageIdentityLive) receipt(ctx context.Context) (storageOwnerV5Receipt, error) {
	var out storageOwnerV5Receipt
	if err := m.current(ctx); err != nil {
		return out, err
	}
	p := m.phase
	host, err := p.c.ref.cell.q.InspectHostState(ctx)
	if err != nil || !storageIdentityIntentExactRetired(m.record, host, m.request.Owner) {
		return out, errors.Join(errStorageEnrollmentUnresolved, err)
	}
	if err = m.hostCurrent(); err != nil {
		return out, err
	}
	intent, err := storageIdentityIntentDigest(m.selected.page, int(m.record.Core.Slot), p.identityPlan.Installation, m.request.Owner, m.record)
	if err != nil {
		return out, err
	}
	ticket, err := storageOwnerV4TicketDigest(p.identityPlan.Installation, m.request.Owner, host.Slots[m.record.Core.Token.Slot].Ticket)
	if err != nil {
		return out, err
	}
	row := host.Slots[m.record.Core.Token.Slot]
	out = storageOwnerV5Receipt{OperationID: m.record.Core.OperationID, Terminal: m.record.Core.Token, PersistedApplied: row.PersistedApplied, PersistedNextSequence: row.PersistedNextSequence, RetirementEpoch: m.record.Core.RetirementEpoch, IntentDigest: intent, TicketDigest: ticket, PublishedObject: m.record.Target, PublishedBytesDigest: m.record.PayloadDigest, PublicationEvidence: m.record.Core.Evidence}
	return out, nil
}
func (m *storageIdentityLive) persistCompletion(ctx context.Context) error {
	if m.record.Action != storageIdentityIntentCreate && m.record.Action != storageIdentityIntentInitialCAS {
		return storagebudget.ErrAuthority
	}
	receipt, err := m.receipt(ctx)
	if err != nil {
		return err
	}
	p := m.phase
	_, lineage, err := p.identityGraph()
	if err != nil {
		return err
	}
	row := storageOwnerV5LineageCompletion{PlanID: p.identityPlan.PlanID, PlanDigest: m.record.PlanDigest, Initial: receipt}
	lineage.Rows.LineageCompletions = append(lineage.Rows.LineageCompletions, row)
	sort.Slice(lineage.Rows.LineageCompletions, func(i, j int) bool {
		return bytes.Compare(lineage.Rows.LineageCompletions[i].PlanID[:], lineage.Rows.LineageCompletions[j].PlanID[:]) < 0
	})
	lineage.Order[1] = append(lineage.Order[1], p.identityPlan.PlanID)
	if err = p.appendIdentityLineage(ctx, lineage, storageCompositionCompletion, [4]string{"lin4/completion-prepared/publish", "lin4/completion-prepared/persist", "lin4/completion-active/publish", "lin4/completion-active/persist"}); err != nil {
		return err
	}
	m.finished = true
	m.phase.markApplication(m.phase.identityPlan.PlanID, m.record.Action, true, true)
	return nil
}

// A durable receipt binds the exact old page and immutable plan before a host
// slot can be reused. A later protocol slot cannot stand in for its D7 ticket.
func storageIdentityReceiptBound(r storageIdentityIntent, page []byte, lineage storageCompositionLineage, status storagebudget.HostState) bool {
	if r.validate() != nil || r.Core.Phase != storageIntentRetired || r.Core.RetirementEpoch > status.Epoch {
		return false
	}
	var plan *storageOwnerV5LineagePlan
	for i := range lineage.Rows.LineagePlans {
		p := &lineage.Rows.LineagePlans[i]
		d, e := storageOwnerV5PlanDigest(*p)
		if e == nil && d == r.PlanDigest {
			if plan != nil {
				return false
			}
			plan = p
		}
	}
	if plan == nil {
		return false
	}
	if r.Core.Outcome == storageIntentAborted {
		if r.NativePhase != storageIdentityAbortObserved || !storageIntentRetiredProof(r.Core, status) {
			return false
		}
		t := status.Slots[r.Core.Token.Slot].Ticket
		return t.Token != r.Core.Token || storageIdentityIntentTicketMapping(r, t, plan.QBinding)
	}
	if r.Core.Outcome != storageIntentPublished {
		return false
	}
	digest, err := storageIdentityIntentDigest(page, int(r.Core.Slot), plan.Installation, plan.QBinding, r)
	if err != nil {
		return false
	}
	var receipt storageOwnerV5Receipt
	switch r.Action {
	case storageIdentityIntentCreate, storageIdentityIntentInitialCAS:
		for _, row := range lineage.Rows.LineageCompletions {
			if row.PlanID == plan.PlanID && row.PlanDigest == r.PlanDigest {
				receipt = row.Initial
			}
		}
	case storageIdentityIntentStamp:
		for _, row := range lineage.Rows.LineageStampCompletions {
			if row.PlanID == plan.PlanID && row.PlanDigest == r.PlanDigest {
				receipt = row.StampReceipt
			}
		}
	case storageIdentityIntentClear:
		for _, row := range lineage.Rows.LineageContinuations {
			if row.PlanID == plan.PlanID && row.PlanDigest == r.PlanDigest {
				receipt = row.Rewrite
			}
		}
	default:
		return false
	}
	if receipt.OperationID != r.Core.OperationID || receipt.Terminal != r.Core.Token || receipt.PersistedApplied != r.Core.Token || receipt.PersistedNextSequence <= r.Core.Token.Sequence || receipt.RetirementEpoch != r.Core.RetirementEpoch || receipt.IntentDigest != digest || receipt.PublishedObject != r.Target || receipt.PublishedBytesDigest != r.PayloadDigest || receipt.PublicationEvidence != r.Core.Evidence || receipt.TicketDigest == (storagebudget.Digest{}) {
		return false
	}
	applied := status.Slots[r.Core.Token.Slot]
	if applied.PersistedApplied == r.Core.Token {
		if applied.PersistedNextSequence <= r.Core.Token.Sequence {
			return false
		}
	} else if applied.PersistedApplied.ID == (storagebudget.ID{}) || applied.PersistedApplied.Sequence <= r.Core.Token.Sequence || applied.PersistedApplied.Generation == 0 || applied.PersistedNextSequence <= applied.PersistedApplied.Sequence {
		return false
	}
	// While the exact ticket remains available, independently require exact D7.
	ticket := status.Slots[r.Core.Token.Slot].Ticket
	if ticket.Token == r.Core.Token {
		d, e := storageOwnerV4TicketDigest(plan.Installation, plan.QBinding, ticket)
		if e != nil || d != receipt.TicketDigest || !storageIdentityIntentTicketMapping(r, ticket, plan.QBinding) {
			return false
		}
	}
	return true
}
func (o *storageBudgetOwner) identityLineage(ctx context.Context) (storageCompositionLineage, error) {
	active, e := o.policyReserveClosureActive(ctx)
	if e != nil {
		return storageCompositionLineage{}, e
	}
	if active {
		return o.originalPolicyLineage(ctx)
	}
	var raw [2][]byte
	for i, name := range []string{"storage-owner.a", "storage-owner.b"} {
		f, err := storageOpenOriginalControl(o.ref.cell, name)
		if err != nil {
			return storageCompositionLineage{}, err
		}
		if f.size != storageOwnerCatalogBytes {
			f.file.Close()
			return storageCompositionLineage{}, errStorageCustody
		}
		raw[i] = make([]byte, storageOwnerCatalogBytes)
		n, e := f.file.ReadAt(raw[i], 0)
		e = errors.Join(e, f.current(), f.file.Close())
		if e != nil || n != len(raw[i]) {
			return storageCompositionLineage{}, errors.Join(errStorageCustody, e)
		}
	}
	pair, err := storageCompositionReadPair(raw[0], raw[1], *o.ref.cell.ownerRecord, o.ref.cell.ownerClaim)
	if err != nil || pair.RecoveryOnly {
		return storageCompositionLineage{}, errors.Join(errStorageEnrollmentUnresolved, err)
	}
	lineage := pair.Banks[pair.Selected].Lineage
	if err = lineage.validateGraph(o.ref.cell.ownerCatalog, *o.ref.cell.ownerRecord); err != nil {
		return storageCompositionLineage{}, err
	}
	return lineage, nil
}

func (m *storageIdentityLive) stampCurrent(ctx context.Context, name string) error {
	if m.source == nil || m.sourceFile == nil || m.phase.identityStamp == nil {
		return storagebudget.ErrAuthority
	}
	if err := m.phase.identityStamp.current(); err != nil {
		return err
	}
	named, err := m.parent.Lstat(name)
	held, e := m.sourceFile.Stat()
	if err != nil || e != nil || !storagePrivate(named, true) || !os.SameFile(named, m.source.identity) || !os.SameFile(named, held) {
		return errors.Join(errStorageCustody, err, e)
	}
	native, err := storageAdvisoryPersistentHeld(ctx, m.sourceFile)
	if err != nil || native != m.record.AllocatedStage {
		return errors.Join(errStorageCustody, err)
	}
	return m.current(ctx)
}
func (m *storageIdentityLive) publishStamp(ctx context.Context) error {
	if m.record.Action != storageIdentityIntentStamp || m.record.Core.Phase != storageIntentStaging && m.record.Core.Phase != storageIntentPublishing {
		return storagebudget.ErrAuthority
	}
	var r storageIdentityIntent
	renameOwed := false
	if m.record.NativePhase == storageIdentityNativeNone {
		r = m.next(storageIntentStaging)
		r.NativePhase = storageIdentityEntered
		if err := m.publish(ctx, r); err != nil {
			return err
		}
		renameOwed = true
	}
	if m.record.NativePhase == storageIdentityEntered {
		if err := m.native(ctx, 30, func() error {
			if m.recovering && !renameOwed {
				if _, e := m.parent.Lstat(m.stageName); !errors.Is(e, os.ErrNotExist) {
					return errors.Join(errStorageEnrollmentUnresolved, e)
				}
				return m.stampCurrent(ctx, m.targetName)
			}
			if err := m.stampCurrent(ctx, m.stageName); err != nil {
				return err
			}
			if _, err := m.parent.Lstat(m.targetName); !errors.Is(err, os.ErrNotExist) {
				return errors.Join(errStorageCustody, err)
			}
			if err := m.phase.permit(ctx); err != nil {
				return err
			}
			if err := m.parent.Rename(m.stageName, m.targetName); err != nil {
				return err
			}
			if _, err := m.parent.Lstat(m.stageName); !errors.Is(err, os.ErrNotExist) {
				return errors.Join(errStorageCustody, err)
			}
			return m.stampCurrent(ctx, m.targetName)
		}); err != nil {
			return err
		}
		r = m.next(storageIntentPublishing)
		r.NativePhase = storageIdentityLinkedObserved
		r.Target = r.AllocatedStage
		r.Presence = 11
		if err := m.publish(ctx, r); err != nil {
			return err
		}
	}
	if m.record.NativePhase == storageIdentityTargetDurable {
		return m.recoveryTargetCurrent(ctx)
	}
	if m.record.NativePhase != storageIdentityLinkedObserved {
		return storagebudget.ErrAuthority
	}
	if err := m.native(ctx, 31, func() error {
		if err := m.stampCurrent(ctx, m.targetName); err != nil {
			return err
		}
		f, err := m.source.root.Open(".")
		if err != nil {
			return err
		}
		n, e := storageAdvisoryPersistentHeld(ctx, f)
		if e == nil && n != m.record.Target {
			e = errStorageCustody
		}
		if e == nil {
			e = m.phase.permit(ctx)
		}
		if e == nil {
			e = f.Sync()
		}
		if err = errors.Join(e, f.Close(), m.phase.boundary("identity-stamp-target-close")); err != nil {
			return err
		}
		return m.stampCurrent(ctx, m.targetName)
	}); err != nil {
		return err
	}
	if err := m.native(ctx, 32, func() error {
		if _, err := m.parent.Lstat(m.stageName); !errors.Is(err, os.ErrNotExist) {
			return errors.Join(errStorageCustody, err)
		}
		return m.syncParent(ctx)
	}); err != nil {
		return err
	}
	r = m.next(storageIntentPublishing)
	r.NativePhase = storageIdentityTargetDurable
	r.Facts = 127
	return m.publish(ctx, r)
}
func (m *storageIdentityLive) captureRetiredOriginal(ctx context.Context) error {
	p := m.phase
	if err := m.current(ctx); err != nil {
		return err
	}
	if err := m.hostCurrent(); err != nil {
		return err
	}
	q, err := p.c.ref.cell.q.InspectEnrollment(ctx)
	if err != nil || q.View.Configuration.Maintenance || !storageIdentityIntentExactRetired(m.record, storageHostState(q.View), m.request.Owner) {
		return errors.Join(errStorageEnrollmentUnresolved, err)
	}
	_, after, err := storageOwnerActualDefinitions(q.View, p.c.ref.cell.ownerCatalog)
	if err != nil || !after {
		return errors.Join(errStorageCustody, err)
	}
	for bank := 0; bank < 2; bank++ {
		raw := make([]byte, storagebudget.CheckpointBytes)
		n, e := p.targets[6+bank].named.file.ReadAt(raw, 0)
		if e != nil || n != len(raw) || !bytes.Equal(raw, q.Checkpoints[bank]) {
			return errors.Join(errStorageCustody, e)
		}
		if e = p.targets[6+bank].named.current(); e != nil {
			return e
		}
	}
	p.original.Checkpoints = q.Checkpoints
	p.original.Tickets = q.Tickets
	p.initialInspection = &q
	return p.held(ctx)
}
func (p *storageEnrollmentEpoch) persistStampRow(ctx context.Context, row storageOwnerV5StampCompletionRow) error {
	if p.identityPlan == nil || row.PlanID != p.identityPlan.PlanID || !row.valid() {
		return storagebudget.ErrAuthority
	}
	if row.StampDisposition == storageOwnerV5Moved {
		m := p.identity
		if m == nil || m.record.Action != storageIdentityIntentStamp {
			return storagebudget.ErrAuthority
		}
		receipt, e := m.receipt(ctx)
		if e != nil || receipt != row.StampReceipt || m.record.AllocatedStage != row.StampSourceBefore || m.record.Target != row.StampTargetAfter || row.StampTargetBefore != (storageOwnerPersistentObject{}) || row.StampSourceAfter != (storageOwnerPersistentObject{}) {
			return errors.Join(errStorageCustody, e)
		}
		if e = m.stampCurrent(ctx, m.targetName); e != nil {
			return e
		}
		if _, e = m.parent.Lstat(m.stageName); !errors.Is(e, os.ErrNotExist) {
			return errors.Join(errStorageCustody, e)
		}
	} else {
		actual, moved, e := p.originalStampDisposition(ctx)
		if e != nil || moved || actual != row {
			return errors.Join(errStorageCustody, e)
		}
	}
	_, lineage, err := p.identityGraph()
	if err != nil {
		return err
	}
	lineage.Rows.LineageStampCompletions = append(lineage.Rows.LineageStampCompletions, row)
	sort.Slice(lineage.Rows.LineageStampCompletions, func(i, j int) bool {
		return bytes.Compare(lineage.Rows.LineageStampCompletions[i].PlanID[:], lineage.Rows.LineageStampCompletions[j].PlanID[:]) < 0
	})
	lineage.Order[2] = append(lineage.Order[2], p.identityPlan.PlanID)
	return p.appendIdentityLineage(ctx, lineage, storageCompositionStamp, [4]string{"m1-proposed/stamp-anchor/stamp-prepared/publish", "m1-proposed/stamp-anchor/stamp-prepared/persist", "m1-proposed/stamp-anchor/stamp-active/publish", "m1-proposed/stamp-anchor/stamp-active/persist"})
}
func (p *storageEnrollmentEpoch) originalStampDisposition(ctx context.Context) (storageOwnerV5StampCompletionRow, bool, error) {
	var row storageOwnerV5StampCompletionRow
	if p.identityPlan == nil {
		return row, false, storagebudget.ErrAuthority
	}
	if err := p.held(ctx); err != nil {
		return row, false, err
	}
	identity, err := parseIdentity(p.identityPlan.IdentityInitial)
	if err != nil || identity.From == "" || identity.From == identity.ID {
		return row, false, storagebudget.ErrAuthority
	}
	digest, err := storageOwnerV5PlanDigest(*p.identityPlan)
	if err != nil {
		return row, false, err
	}
	row.PlanID, row.PlanDigest = p.identityPlan.PlanID, digest
	observe := func(name string) (storageOwnerPersistentObject, error) {
		var zero storageOwnerPersistentObject
		if p.identityStamp == nil {
			if _, e := p.c.original.config.Lstat(stampingDirName); !errors.Is(e, os.ErrNotExist) {
				return zero, errors.Join(errStorageCustody, e)
			}
			return zero, nil
		}
		if e := p.identityStamp.current(); e != nil {
			return zero, e
		}
		if _, e := p.identityStamp.root.Lstat(name); errors.Is(e, os.ErrNotExist) {
			return zero, nil
		} else if e != nil {
			return zero, e
		}
		root, e := storagePinChild(p.identityStamp.root, name, true)
		if e != nil {
			return zero, e
		}
		native, e := storageOwnerPersistentRoot(ctx, root.root)
		e = errors.Join(e, root.current(), root.root.Close())
		return native, e
	}
	row.StampSourceBefore, err = observe(identity.From)
	if err != nil {
		return row, false, err
	}
	row.StampTargetBefore, err = observe(identity.ID)
	if err != nil {
		return row, false, err
	}
	if row.StampSourceBefore != (storageOwnerPersistentObject{}) && row.StampTargetBefore == (storageOwnerPersistentObject{}) {
		return row, true, nil
	}
	row.StampSourceAfter, row.StampTargetAfter = row.StampSourceBefore, row.StampTargetBefore
	row.StampDisposition = storageOwnerV5BothPresent
	if row.StampSourceBefore == (storageOwnerPersistentObject{}) {
		row.StampDisposition = storageOwnerV5SourceAbsent
	}
	return row, false, nil
}
func (m *storageIdentityLive) persistContinuation(ctx context.Context) error {
	if m.record.Action != storageIdentityIntentClear {
		return storagebudget.ErrAuthority
	}
	receipt, err := m.receipt(ctx)
	if err != nil {
		return err
	}
	p := m.phase
	_, lineage, err := p.identityGraph()
	if err != nil {
		return err
	}
	var stamp *storageOwnerV5StampCompletionRow
	for i := range lineage.Rows.LineageStampCompletions {
		s := &lineage.Rows.LineageStampCompletions[i]
		if s.PlanID == p.identityPlan.PlanID {
			stamp = s
		}
	}
	if stamp == nil {
		return storagebudget.ErrAuthority
	}
	s := *stamp
	row := storageOwnerV5FromContinuation{PlanID: s.PlanID, PlanDigest: s.PlanDigest, StampDisposition: s.StampDisposition, StampSourceBefore: s.StampSourceBefore, StampTargetBefore: s.StampTargetBefore, StampSourceAfter: s.StampSourceAfter, StampTargetAfter: s.StampTargetAfter, StampReceipt: s.StampReceipt, Rewrite: receipt}
	lineage.Rows.LineageContinuations = append(lineage.Rows.LineageContinuations, row)
	sort.Slice(lineage.Rows.LineageContinuations, func(i, j int) bool {
		return bytes.Compare(lineage.Rows.LineageContinuations[i].PlanID[:], lineage.Rows.LineageContinuations[j].PlanID[:]) < 0
	})
	lineage.Order[3] = append(lineage.Order[3], p.identityPlan.PlanID)
	if err = p.appendIdentityLineage(ctx, lineage, storageCompositionContinuation, [4]string{"lin4/from-prepared/publish", "lin4/from-prepared/persist", "lin4/from-active/publish", "lin4/from-active/persist"}); err != nil {
		return err
	}
	m.finished = true
	m.phase.markApplication(m.phase.identityPlan.PlanID, m.record.Action, true, true)
	return nil
}

// Called within one original post-census enrollment lifetime. This is the
// actual producer→physical lineage→J/Q/native→receipt sequence, not a DTO API.
func (p *storageEnrollmentEpoch) runOriginalIdentity(ctx context.Context) error {
	if err := p.captureOriginalIdentityPlan(ctx); err != nil {
		return err
	}
	if p.qualified == nil {
		if err := p.qualifyOriginalNative(ctx); err != nil {
			return err
		}
	}
	local, err := p.joinOriginalLocalCreation(ctx)
	if err != nil {
		return err
	}
	if local {
		m := p.creation
		if err = m.execute(ctx); err != nil {
			return err
		}
		if err = m.Close(); err != nil {
			return err
		}
		p.creation = nil
	}
	joined, err := p.joinOriginalIdentityAction(ctx)
	if err != nil {
		return err
	}
	if !joined {
		if complete, e := p.identityCompletedCurrent(ctx); e != nil {
			return e
		} else {
			p.identityDone = complete
		}
	}
	if err := p.prepareIdentityLaunch(ctx, *p.identityPlan, p.identityInitial, p.identityApplications); err != nil {
		return err
	}
	if p.compositionEntry == nil && p.compositionCut != 4 {
		if err := p.planCompositionEntry(ctx); err != nil {
			return err
		}
	}
	if p.compositionCut < 4 {
		if err := p.runCompositionEntry(ctx); err != nil {
			return err
		}
		p.identityCatalogsKnown()
	}
	if joined {
		if err := p.identity.execute(ctx); err != nil {
			return err
		}
	}
	if err := p.completeIdentityPhysical(ctx); err != nil {
		return err
	}
	if joined {
		m := p.identity
		if err := p.persistOriginalIdentityReceipt(ctx, m); err != nil {
			return err
		}
		if err := m.Close(); err != nil {
			return err
		}
		p.identity = nil
	}
	if p.identityDone {
		return p.held(ctx)
	}
	if err := p.persistIdentityPlan(ctx); err != nil {
		return err
	}
	_, lineage, err := p.identityGraph()
	if err != nil {
		return err
	}
	completed, stamped, continued := false, false, false
	for _, c := range lineage.Rows.LineageCompletions {
		completed = completed || c.PlanID == p.identityPlan.PlanID
	}
	for _, s := range lineage.Rows.LineageStampCompletions {
		stamped = stamped || s.PlanID == p.identityPlan.PlanID
	}
	for _, f := range lineage.Rows.LineageContinuations {
		continued = continued || f.PlanID == p.identityPlan.PlanID
	}
	closeMember := func(m *storageIdentityLive) error {
		if err := m.Close(); err != nil {
			return err
		}
		p.identity = nil
		return nil
	}
	if !completed {
		action := byte(storageIdentityIntentCreate)
		if p.identityInitial.Mode == 2 {
			action = storageIdentityIntentInitialCAS
		}
		m, e := p.issueOriginalIdentity(ctx, action)
		if e != nil {
			return e
		}
		if e = m.execute(ctx); e != nil {
			return e
		}
		if e = m.persistCompletion(ctx); e != nil {
			return e
		}
		if e = m.captureRetiredOriginal(ctx); e != nil {
			return e
		}
		if e = closeMember(m); e != nil {
			return e
		}
	}
	identity, err := parseIdentity(p.identityPlan.IdentityInitial)
	if err != nil {
		return err
	}
	if identity.From == "" {
		p.identityDone = true
		return p.held(ctx)
	}
	if !stamped {
		row, moved, e := p.originalStampDisposition(ctx)
		if e != nil {
			return e
		}
		if moved {
			m, e := p.issueOriginalIdentity(ctx, storageIdentityIntentStamp)
			if e != nil {
				return e
			}
			if e = m.execute(ctx); e != nil {
				return e
			}
			receipt, e := m.receipt(ctx)
			if e != nil {
				return e
			}
			row.StampDisposition = storageOwnerV5Moved
			row.StampSourceAfter = storageOwnerPersistentObject{}
			row.StampTargetAfter = m.record.Target
			row.StampReceipt = receipt
			if e = p.persistStampRow(ctx, row); e != nil {
				return e
			}
			m.finished = true
			p.markApplication(p.identityPlan.PlanID, m.record.Action, true, true)
			if e = m.captureRetiredOriginal(ctx); e != nil {
				return e
			}
			if e = closeMember(m); e != nil {
				return e
			}
		} else if e = p.persistStampRow(ctx, row); e != nil {
			return e
		}
	}
	if !continued {
		m, e := p.issueOriginalIdentity(ctx, storageIdentityIntentClear)
		if e != nil {
			return e
		}
		if e = m.execute(ctx); e != nil {
			return e
		}
		if e = m.persistContinuation(ctx); e != nil {
			return e
		}
		if e = m.captureRetiredOriginal(ctx); e != nil {
			return e
		}
		if e = closeMember(m); e != nil {
			return e
		}
	}
	p.identityDone = true
	return p.held(ctx)
}

func (p *storageEnrollmentEpoch) identityStampAfterCurrent(ctx context.Context, lineage storageCompositionLineage) error {
	var stamp *storageOwnerV5StampCompletionRow
	for i := range lineage.Rows.LineageStampCompletions {
		s := &lineage.Rows.LineageStampCompletions[i]
		if s.PlanID == p.identityPlan.PlanID {
			stamp = s
		}
	}
	if stamp == nil {
		return storagebudget.ErrAuthority
	}
	actual, _, err := p.originalStampDisposition(ctx)
	if err != nil {
		return err
	}
	if actual.StampSourceBefore != stamp.StampSourceAfter || actual.StampTargetBefore != stamp.StampTargetAfter {
		return errStorageCustody
	}
	return p.held(ctx)
}
func (p *storageEnrollmentEpoch) identityCompletedCurrent(ctx context.Context) (bool, error) {
	_, lineage, err := p.identityGraph()
	if err != nil {
		return false, err
	}
	pair, e := storageCompositionReadPair(p.original.Catalogs[0], p.original.Catalogs[1], *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
	if e == nil && storageIdentityPreparedLineage(pair) {
		return false, nil
	}
	var completion *storageOwnerV5LineageCompletion
	for i := range lineage.Rows.LineageCompletions {
		c := &lineage.Rows.LineageCompletions[i]
		if c.PlanID == p.identityPlan.PlanID {
			completion = c
		}
	}
	if completion == nil {
		return false, nil
	}
	published, body := completion.Initial.PublishedObject, p.identityPlan.IdentityInitial
	identity, err := parseIdentity(body)
	if err != nil {
		return false, err
	}
	if identity.From != "" {
		var continuation *storageOwnerV5FromContinuation
		for i := range lineage.Rows.LineageContinuations {
			f := &lineage.Rows.LineageContinuations[i]
			if f.PlanID == p.identityPlan.PlanID {
				continuation = f
			}
		}
		if continuation == nil {
			return false, nil
		}
		if err = p.identityStampAfterCurrent(ctx, lineage); err != nil {
			return false, err
		}
		published, body = continuation.Rewrite.PublishedObject, p.identityPlan.IdentityFromCleared
	}
	// The immutable completion remains origin evidence. A separately admitted
	// current move may replace only this exact P's current native/body tuple.
	// Re-read the actual selected storage pair and identity under this epoch;
	// a cached path alias, matching owner or prior InitialCAS token is insufficient.
	p.c.mu.Lock()
	moved := p.c.moveHistory
	p.c.mu.Unlock()
	if moved {
		state, moveErr := p.c.owner.readOriginalPolicy(ctx)
		if moveErr != nil {
			return false, moveErr
		}
		for _, row := range state.Moves {
			if row.PlanID != p.identityPlan.PlanID {
				continue
			}
			joined, e := p.c.readMoveBootstrap(ctx, state)
			if e != nil {
				return false, e
			}
			if !reflect.DeepEqual(joined.Plan, *p.identityPlan) || joined.Pending || joined.Current == nil || joined.Current.Current == nil {
				return false, errStorageEnrollmentUnresolved
			}
			published, body = joined.Current.Current.Receipt.PublishedObject, joined.Current.Current.Body
		}
	}
	raw, held, _, err := storageReadOriginalFile(p.identityPrivate.root, startupIdentityName, 512)
	if err != nil {
		return false, err
	}
	native, e := storageAdvisoryPersistentHeld(ctx, held)
	e = errors.Join(e, held.Close())
	if e != nil || native != published || !bytes.Equal(raw, body) {
		return false, errors.Join(errStorageCustody, e)
	}
	_, after, e := storageOwnerActualDefinitions(p.initialInspection.View, p.c.ref.cell.ownerCatalog)
	if e != nil || !after || p.initialInspection.View.Configuration.Maintenance {
		return false, errors.Join(errStorageEnrollmentUnresolved, e)
	}
	return true, p.held(ctx)
}

func (p *storageEnrollmentEpoch) captureIdentityOriginal(ctx context.Context) error {
	if !p.identityPrefixes {
		return storagebudget.ErrAuthority
	}
	if err := p.captureOriginalControls(ctx); err != nil {
		return err
	}
	// Recover the exact founding comparison before interpreting either bare
	// core. This performs no write and retains the actual mixed physical pair.
	if err := p.planCompositionEntry(ctx); err != nil {
		return err
	}
	catalogs := p.original.Catalogs
	if p.compositionEntry != nil && p.compositionCut < 4 {
		catalogs = p.compositionEntry.Original
	}
	cores, _, err := p.identityCorePair(catalogs, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
	if err != nil {
		return err
	}
	if string(cores[0][:8]) == "DSKOWN3\x00" {
		pair, e := storageOwnerDecodeCatalogPairV3(cores[0], cores[1], *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
		if e != nil || pair.High.Phase != "active" {
			return errors.Join(errStorageEnrollmentUnresolved, e)
		}
		for _, name := range []string{"storage-enrollment.a", "storage-enrollment.b", "storage-enrollment-step.a", "storage-enrollment-step.b"} {
			if _, e = p.c.original.config.Lstat(name); !errors.Is(e, os.ErrNotExist) {
				return errors.Join(errStorageEnrollmentUnresolved, e)
			}
		}
		return p.held(ctx)
	}
	return p.captureExistingOriginalMetadata(ctx, nil)
}
func (c *storageBudgetConstruction) completeOriginalIdentity(ctx context.Context) error {
	return c.completeOriginalIdentityFor(ctx, c.server)
}
func (c *storageBudgetConstruction) completeOriginalIdentityFor(ctx context.Context, requester *Server) (err error) {
	if c == nil || c.self != c || c.owner == nil || c.home == nil {
		return storagebudget.ErrAuthority
	}
	// The existing startup role retains its home description. All original
	// startup use/work/domains must drain before the prescribed prefix order.
	if err = c.releaseStartupHolds(); err != nil {
		return err
	}
	var p *storageEnrollmentEpoch
	if c.home.exclusive {
		p, err = c.beginStartupEnrollment(ctx)
	} else {
		p, err = c.beginEnrollmentEpoch(ctx)
	}
	if err != nil {
		return err
	}
	defer func() { err = p.finish(err) }()
	if p.startup && requester == c.server {
		err = p.selectStartupOriginalRequester(ctx)
	} else {
		err = p.selectOriginalRequester(ctx, requester)
	}
	if err != nil {
		return err
	}
	if err = p.acquireIdentity(ctx); err != nil {
		return err
	}
	if err = p.captureIdentityOriginal(ctx); err != nil {
		return err
	}
	if err = p.runOriginalIdentity(ctx); err != nil {
		return err
	}
	// Early recovery may have selected another retained home project. Only the
	// actual primary's completion discharges its post-census automatic demand.
	if p.startup && (p.startupProject == nil && p.requester == c.server || p.startupProject != nil && os.SameFile(p.startupProject.named.identity, c.project.info)) {
		c.startupIdentityCompleted = true
	}
	return nil
}

func storageIdentityPreparedLineage(pair storageCompositionPair) bool {
	high := pair.Banks[pair.Selected]
	return !pair.Duplicated && pair.RecoveryOnly && high.Publication == storageCompositionPrepared && high.Transition >= storageCompositionPlan && high.Transition <= storageCompositionContinuation && storageCompositionLineageAppend(pair.Banks[1-pair.Selected], high)
}
func (p *storageEnrollmentEpoch) identityCorePair(raw [2][]byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) ([2][]byte, int, error) {
	if len(raw[0]) != storageOwnerCatalogBytes || len(raw[1]) != storageOwnerCatalogBytes {
		return [2][]byte{}, 0, storagebudget.ErrBounds
	}
	if p.identityPrefixes && (string(raw[0][:8]) == "DSKONL1\x00" || string(raw[1][:8]) == "DSKONL1\x00") {
		pair, err := storageCompositionReadPair(raw[0], raw[1], record, claim)
		if err == nil && storageIdentityPreparedLineage(pair) {
			return [2][]byte{pair.Banks[0].Core, pair.Banks[1].Core}, int(pair.Selected), nil
		}
	}
	return p.originalCorePair(raw, record, claim)
}
func (m *storageIdentityLive) recoveryTargetCurrent(ctx context.Context) error {
	if m.record.NativePhase != storageIdentityLinkedObserved && m.record.NativePhase != storageIdentityTargetDurable && m.record.NativePhase != storageIdentityEntered {
		return storagebudget.ErrAuthority
	}
	if _, err := m.parent.Lstat(m.stageName); !errors.Is(err, os.ErrNotExist) {
		return errors.Join(errStorageCustody, err)
	}
	if m.record.Action == storageIdentityIntentStamp {
		return m.stampCurrent(ctx, m.targetName)
	}
	want := m.record.Target
	if want == (storageOwnerPersistentObject{}) {
		want = m.record.AllocatedStage
	}
	_, err := m.fileObservation(ctx, m.targetName, 1, want, true)
	return err
}
func (m *storageIdentityLive) recoveryNativeCurrent(ctx context.Context) error {
	p := m.phase
	if !m.recovering || m.self != m || p.identity != m || p.identityPlan == nil || p.qualified == nil {
		return storagebudget.ErrAuthority
	}
	if err := p.held(ctx); err != nil {
		return err
	}
	if err := p.qualified.current(ctx, p); err != nil {
		return err
	}
	if err := p.identityComponentMappings(ctx, m.record.Core, m.record.Core.Components[0].Root); err != nil {
		return err
	}
	if err := m.hostCurrent(); err != nil {
		return err
	}
	native, err := storageAdvisoryPersistentHeld(ctx, m.parentFile)
	if err != nil || native != m.record.Parent {
		return errors.Join(errStorageCustody, err)
	}
	if m.record.Action == storageIdentityIntentStamp {
		name := m.stageName
		if m.record.NativePhase == storageIdentityEntered || m.record.NativePhase == storageIdentityLinkedObserved || m.record.NativePhase == storageIdentityTargetDurable {
			name = m.targetName
			if _, e := m.parent.Lstat(m.stageName); !errors.Is(e, os.ErrNotExist) {
				return errors.Join(errStorageEnrollmentUnresolved, e)
			}
		}
		if m.source == nil || m.sourceFile == nil {
			return storagebudget.ErrAuthority
		}
		info, e := m.parent.Lstat(name)
		held, e2 := m.sourceFile.Stat()
		n, e3 := storageAdvisoryPersistentHeld(ctx, m.sourceFile)
		if e != nil || e2 != nil || e3 != nil || !os.SameFile(info, held) || !os.SameFile(info, m.source.identity) || n != m.record.AllocatedStage {
			return errors.Join(errStorageCustody, e, e2, e3)
		}
		return nil
	}
	switch m.record.NativePhase {
	case storageIdentityAllocatedObserved, storageIdentityPayloadReady, storageIdentityAbortPrepared:
		links := uint64(1)
		if m.record.NativePhase == storageIdentityPayloadReady && m.record.Action == storageIdentityIntentCreate {
			if _, e := m.parent.Lstat(m.targetName); e == nil {
				links = 2
			} else if !errors.Is(e, os.ErrNotExist) {
				return e
			}
		}
		if _, err = m.fileObservation(ctx, m.stageName, links, m.record.AllocatedStage, m.record.NativePhase == storageIdentityPayloadReady || m.record.Facts == storageIdentityPayloadFacts); err != nil {
			return err
		}
		if m.record.Action == storageIdentityIntentCreate {
			if links == 1 {
				if _, e := m.parent.Lstat(m.targetName); !errors.Is(e, os.ErrNotExist) {
					return errors.Join(errStorageCustody, e)
				}
			}
		} else {
			return m.oldCurrentForRecovery(ctx)
		}
	case storageIdentityEntered, storageIdentityLinkedObserved, storageIdentityTargetDurable:
		if m.record.Action == storageIdentityIntentCreate && m.record.NativePhase == storageIdentityLinkedObserved {
			if _, e := m.parent.Lstat(m.stageName); e == nil {
				if _, e = m.fileObservation(ctx, m.stageName, 2, m.record.AllocatedStage, true); e != nil {
					return e
				}
				_, e = m.fileObservation(ctx, m.targetName, 2, m.record.Target, true)
				return e
			} else if !errors.Is(e, os.ErrNotExist) {
				return e
			}
		}
		if _, e := m.parent.Lstat(m.stageName); !errors.Is(e, os.ErrNotExist) {
			return errors.Join(errStorageEnrollmentUnresolved, e)
		}
		want := m.record.Target
		if want == (storageOwnerPersistentObject{}) {
			want = m.record.AllocatedStage
		}
		_, err = m.fileObservation(ctx, m.targetName, 1, want, true)
		return err
	case storageIdentityAbortObserved:
		if _, e := m.parent.Lstat(m.stageName); !errors.Is(e, os.ErrNotExist) {
			return errors.Join(errStorageCustody, e)
		}
		if m.record.Action == storageIdentityIntentCreate {
			if _, e := m.parent.Lstat(m.targetName); !errors.Is(e, os.ErrNotExist) {
				return errors.Join(errStorageCustody, e)
			}
		} else {
			return m.oldCurrentForRecovery(ctx)
		}
	default:
		return errStorageEnrollmentUnresolved
	}
	return nil
}
func (m *storageIdentityLive) oldCurrentForRecovery(ctx context.Context) error {
	if m.old == nil {
		return storagebudget.ErrAuthority
	}
	info, e := m.parent.Lstat(m.targetName)
	held, e2 := m.old.Stat()
	native, e3 := storageAdvisoryPersistentHeld(ctx, m.old)
	if e != nil || e2 != nil || e3 != nil || !os.SameFile(info, held) || !os.SameFile(info, m.oldInfo) || native != m.record.ExpectedOld {
		return errors.Join(errStorageCustody, e, e2, e3)
	}
	links, known := linkCount(info)
	id, e := storageFileIdentity(info)
	if !known || links != 1 || e != nil || id != m.record.Core.OldIdentity || uint64(info.Size()) != m.record.Core.OldLength {
		return errors.Join(errStorageCustody, e)
	}
	raw := make([]byte, m.record.Core.OldLength)
	n, e := m.old.ReadAt(raw, 0)
	if e != nil || n != len(raw) || !bytes.Equal(raw, m.phase.identityPlan.IdentityInitial) {
		return errors.Join(errStorageCustody, e)
	}
	return nil
}

// Re-establish only the native member already named by an original retained
// page. This requires qualified incarnation continuity plus the complete P,
// current registration/reference graph, original request and exact Q token.
// A filename/body match without the original persistent native is refused.
func (p *storageEnrollmentEpoch) joinOriginalIdentityAction(ctx context.Context) (joined bool, err error) {
	if p.identity != nil || p.identityPlan == nil || p.qualified == nil {
		return false, storagebudget.ErrAuthority
	}
	plan := *p.identityPlan
	digest, err := storageOwnerV5PlanDigest(plan)
	if err != nil {
		return false, err
	}
	_, lineage, err := p.identityGraph()
	if err != nil {
		return false, err
	}
	pair, e0 := storageCompositionReadPair(p.original.Catalogs[0], p.original.Catalogs[1], *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
	if e0 == nil && storageIdentityPreparedLineage(pair) {
		lineage = pair.Banks[1-pair.Selected].Lineage
	}
	var selected *storageCreationHostSelection
	for slot, pages := range p.original.Hosts {
		choice, e := storageSelectCreationHost(pages[0], pages[1], slot, plan.Installation, plan.QBinding)
		if e != nil {
			return false, e
		}
		if choice.Identity == nil {
			if !choice.Slot.record.reusable() {
				return false, errStorageEnrollmentUnresolved
			}
			continue
		}
		r := choice.Identity
		if p.c.owner.identityReceiptCurrent(ctx, *r, choice.Slot.page, lineage, storageHostState(p.initialInspection.View)) {
			continue
		}
		if r.PlanDigest != digest || selected != nil {
			return false, errStorageEnrollmentUnresolved
		}
		copy := choice
		selected = &copy
	}
	if selected == nil {
		return false, nil
	}
	p.identityRecoveryPending = true
	r := *selected.Identity
	if r.Core.Phase == storageIntentPrepared || r.Core.Phase == storageIntentReserved || r.Core.Phase == storageIntentRejected || r.NativePhase == storageIdentityNativeNone && r.Action != storageIdentityIntentStamp {
		return false, errStorageEnrollmentUnresolved
	}
	if r.Core.AdmissionEpoch != p.initialInspection.View.Configuration.Epoch || r.Core.Target != startupIdentityName && r.Action != storageIdentityIntentStamp {
		return false, errStorageCustody
	}
	application, e := storageIdentityComponentUnion(r.Core.Components, nil)
	if e != nil || !reflect.DeepEqual(application, r.Core.Components) {
		return false, errors.Join(errStorageCustody, e)
	}
	if e = p.identityComponentMappings(ctx, r.Core, r.Core.Components[0].Root); e != nil {
		return false, e
	}
	m := &storageIdentityLive{phase: p, record: r, selected: selected.Slot, parent: p.identityPrivate.root, payload: append([]byte{}, plan.IdentityInitial...), attempt: 2, recovering: true, started: true, stageName: r.Core.Stage, targetName: r.Core.Target}
	m.self = m
	defer func() {
		if !joined {
			if p.identity == m {
				p.identity = nil
			}
			err = errors.Join(err, m.Close())
		}
	}()
	if r.Action == storageIdentityIntentMove {
		if p.move == nil || p.move.phase != p || !p.move.request.identityBound(r, lineage, p.move.before) {
			return false, errStorageCustody
		}
		_, m.payload, err = p.move.request.operands(lineage, p.move.before)
		if err != nil {
			return false, err
		}
	}
	if r.Action == storageIdentityIntentClear {
		m.payload = append([]byte{}, plan.IdentityFromCleared...)
		if e = p.identityStampAfterCurrent(ctx, lineage); e != nil {
			return false, e
		}
	}
	expectedBinding := digest
	if r.Action == storageIdentityIntentMove {
		expectedBinding, err = p.move.request.digest()
		if err != nil {
			return false, err
		}
	}
	if r.Action == storageIdentityIntentInitialCAS {
		expectedBinding, e = storageCompositionInitialBinding(plan, p.identityInitial)
		if e != nil || r.ExpectedOld != p.identityInitial.ExpectedOld {
			return false, errors.Join(errStorageCustody, e)
		}
	}
	if r.Action == storageIdentityIntentClear {
		expected := storageOwnerPersistentObject{}
		for _, row := range lineage.Rows.LineageCompletions {
			if row.PlanID == plan.PlanID {
				expected = row.Initial.PublishedObject
			}
		}
		if expected == (storageOwnerPersistentObject{}) || r.ExpectedOld != expected {
			return false, errStorageCustody
		}
	}
	if r.Core.CASBinding != expectedBinding {
		return false, errStorageCustody
	}
	if r.Action == storageIdentityIntentStamp {
		identity, e := parseIdentity(plan.IdentityInitial)
		if e != nil || identity.From == "" || p.identityStamp == nil {
			return false, errors.Join(errStorageCustody, e)
		}
		if r.Core.Target != filepath.Join(stampingDirName, identity.ID) || r.Core.Stage != filepath.Join(stampingDirName, identity.From) {
			return false, errStorageCustody
		}
		m.parent = p.identityStamp.root
		m.stageName, m.targetName = identity.From, identity.ID
		m.payload = nil
		name := m.stageName
		if r.NativePhase == storageIdentityEntered || r.NativePhase == storageIdentityLinkedObserved || r.NativePhase == storageIdentityTargetDurable {
			name = m.targetName
		}
		source, e := storagePinChild(m.parent, name, true)
		if e != nil {
			return false, e
		}
		m.source = &source
		m.sourceFile, e = source.root.Open(".")
		if e != nil {
			return false, e
		}
	} else {
		if r.Core.Target != startupIdentityName || r.Core.Stage != identityStagingPrefix+hex.EncodeToString(r.Core.OperationID[:]) || r.Core.NewLength != uint64(len(m.payload)) || r.PayloadDigest != sha256.Sum256(m.payload) || r.Parent != plan.PrivateObject {
			return false, errStorageCustody
		}
		if r.NativePhase != storageIdentityAbortObserved {
			name := m.stageName
			if r.NativePhase == storageIdentityEntered || r.NativePhase == storageIdentityTargetDurable {
				name = m.targetName
			} else if r.NativePhase == storageIdentityLinkedObserved {
				if _, e := m.parent.Lstat(name); errors.Is(e, os.ErrNotExist) {
					name = m.targetName
				} else if e != nil {
					return false, e
				}
			}
			f, e := m.parent.OpenFile(name, os.O_RDONLY|openNoFollow|openNonBlocking, 0)
			if e != nil {
				return false, e
			}
			m.allocated = f
			m.allocatedInfo, e = f.Stat()
			if e != nil {
				return false, e
			}
			native, e := storageAdvisoryPersistentHeld(ctx, f)
			if e != nil || native != r.AllocatedStage || !storageCensusPrivateFile(m.allocatedInfo) {
				return false, errors.Join(errStorageCustody, e)
			}
		}
		if r.Action != storageIdentityIntentCreate && (r.NativePhase == storageIdentityAllocatedObserved || r.NativePhase == storageIdentityPayloadReady || r.NativePhase == storageIdentityAbortPrepared || r.NativePhase == storageIdentityAbortObserved) {
			body, held, info, e := storageReadOriginalFile(m.parent, m.targetName, 512)
			if e != nil {
				return false, e
			}
			p.identityOriginal = held
			m.old, m.oldInfo = held, info
			oldBody := plan.IdentityInitial
			if r.Action == storageIdentityIntentMove {
				oldBody, _, err = p.move.request.operands(lineage, p.move.before)
				if err != nil {
					return false, err
				}
			}
			if !bytes.Equal(body, oldBody) {
				return false, errStorageCustody
			}
		}
	}
	m.parentFile, err = m.parent.Open(".")
	if err != nil {
		return false, err
	}
	m.ownedParent = true
	for _, f := range p.c.original.files {
		if f.root == p.c.original.config && f.name == storageIntentName {
			m.host = f
		}
	}
	if m.host.file == nil {
		return false, errStorageCustody
	}
	m.request, err = storageIdentityRequest(r, plan.QBinding)
	if err != nil {
		return false, err
	}
	status := storageHostState(p.initialInspection.View)
	if int(r.Core.Token.Slot) >= len(status.Slots) {
		return false, storagebudget.ErrBounds
	}
	ticket := status.Slots[r.Core.Token.Slot].Ticket
	if !storageIdentityIntentTicketMapping(r, ticket, plan.QBinding) {
		return false, errStorageEnrollmentUnresolved
	}
	if r.Core.Phase == storageIntentRetired && !storageIdentityIntentExactRetired(r, status, plan.QBinding) {
		return false, errStorageEnrollmentUnresolved
	}
	if e = storageOriginalActionRequest(ctx, p.c.ref.cell.q, r.Core, m.request, status, ticket.Token); e != nil {
		return false, e
	}
	m.admitted = ticket.Token
	p.identity = m
	if err = m.recoveryNativeCurrent(ctx); err != nil {
		return false, err
	}
	p.identityApplications[r.Action] = append([]storagebudget.Component{}, r.Core.Components...)
	return true, nil
}
func (m *storageIdentityLive) persistOriginalAction(ctx context.Context) (err error) {
	if !m.recovering || m.hostPersisted {
		return storagebudget.ErrAuthority
	}
	if err = m.take(ctx, 1); err != nil {
		return err
	}
	m.hostWrites = 1
	if err = m.enter(ctx); err != nil {
		return err
	}
	defer func() {
		err = m.phase.leaveNative(err)
		if err != nil {
			m.phase.identityLaunch.failed = errors.Join(m.phase.identityLaunch.failed, err)
		}
	}()
	if err = m.hostCurrent(); err != nil {
		return err
	}
	f, err := storageOpenFile(m.host.root, m.host.name, m.host.size)
	if err != nil {
		return err
	}
	if !os.SameFile(f.identity, m.host.identity) {
		return errors.Join(errStorageCustody, f.file.Close())
	}
	if err = m.phase.permit(ctx); err == nil {
		err = f.file.Sync()
	}
	err = errors.Join(err, f.file.Close(), m.phase.boundary("identity-recovery-host-close"))
	if err != nil {
		return err
	}
	if err = m.hostCurrent(); err != nil {
		return err
	}
	m.hostPersisted = true
	return nil
}
func (m *storageIdentityLive) persistOriginalQ(ctx context.Context) (err error) {
	if !m.recovering || m.qPersisted {
		return storagebudget.ErrAuthority
	}
	before, err := m.phase.c.ref.cell.q.InspectAction(ctx)
	if err != nil {
		return err
	}
	if !storageIdentityIntentTicketMapping(m.record, before.View.Slots[m.admitted.Slot].Ticket, m.request.Owner) {
		return errStorageEnrollmentUnresolved
	}
	if err = m.take(ctx, 40); err != nil {
		return err
	}
	if err = m.enter(ctx); err != nil {
		return err
	}
	defer func() {
		err = m.phase.leaveNative(err)
		if err != nil {
			m.phase.identityLaunch.failed = errors.Join(m.phase.identityLaunch.failed, err)
		}
	}()
	parent, err := m.phase.c.anchor.Open(".")
	if err != nil {
		return err
	}
	for _, name := range []string{"lock", "checkpoint.a", "checkpoint.b", "tickets.a", "tickets.b", "manifests"} {
		f, e := m.phase.c.openOriginalAnchorControl(ctx, name)
		if e != nil {
			err = e
			break
		}
		if e = m.phase.permit(ctx); e == nil {
			e = f.file.Sync()
		}
		e = errors.Join(e, f.current(), f.file.Close(), m.phase.boundary("identity-recovery-q-close"))
		if e != nil {
			err = e
			break
		}
	}
	if err == nil {
		err = m.phase.permit(ctx)
	}
	if err == nil {
		err = parent.Sync()
	}
	err = errors.Join(err, parent.Close(), m.phase.boundary("identity-recovery-q-parent-close"))
	if err != nil {
		return err
	}
	after, err := m.phase.c.ref.cell.q.InspectAction(ctx)
	if err != nil || !reflect.DeepEqual(before, after) {
		return errors.Join(errStorageCustody, err)
	}
	m.qPersisted = true
	return nil
}

func (m *storageIdentityLive) completeOriginalAbort(ctx context.Context) error {
	if !m.recovering || m.record.NativePhase != storageIdentityAbortPrepared || m.record.Core.Phase != storageIntentStaging || m.allocated == nil {
		return storagebudget.ErrAuthority
	}
	facts := m.record.Facts
	if err := m.native(ctx, 33, func() error {
		if _, err := m.fileObservation(ctx, m.stageName, 1, m.record.AllocatedStage, facts == storageIdentityPayloadFacts); err != nil {
			return err
		}
		if err := m.phase.permit(ctx); err != nil {
			return err
		}
		if err := m.parent.Remove(m.stageName); err != nil {
			return err
		}
		info, err := m.allocated.Stat()
		if err != nil {
			return err
		}
		links, known := linkCount(info)
		if !known || links != 0 {
			return errStorageCustody
		}
		f := m.allocated
		m.allocated = nil
		if err = f.Close(); err != nil {
			return err
		}
		if _, err = m.parent.Lstat(m.stageName); !errors.Is(err, os.ErrNotExist) {
			return errors.Join(errStorageCustody, err)
		}
		return m.syncParent(ctx)
	}); err != nil {
		return err
	}
	r := m.next(storageIntentStaging)
	r.NativePhase = storageIdentityAbortObserved
	r.Facts = facts | storageIdentityAbortFacts
	return m.publish(ctx, r)
}

func storageIdentityCatalogKeys(transition byte) ([4]string, error) {
	prefix := "lin4/plan"
	switch transition {
	case storageCompositionPlan:
	case storageCompositionCompletion:
		prefix = "lin4/completion"
	case storageCompositionStamp:
		prefix = "m1-proposed/stamp-anchor/stamp"
	case storageCompositionContinuation:
		prefix = "lin4/from"
	default:
		return [4]string{}, storagebudget.ErrBounds
	}
	return [4]string{prefix + "-prepared/publish", prefix + "-prepared/persist", prefix + "-active/publish", prefix + "-active/persist"}, nil
}
func (p *storageEnrollmentEpoch) identityCatalogsKnown() {
	p.identityLaunch.catalogsKnown = true
	for i, raw := range p.original.Catalogs {
		p.identityLaunch.catalogs[i] = sha256.Sum256(raw)
	}
}
func (p *storageEnrollmentEpoch) completeIdentityPhysical(ctx context.Context) error {
	l := p.identityLaunch
	if l == nil || l.failed != nil || p.identityPlan == nil {
		return storagebudget.ErrAuthority
	}
	same := l.catalogsKnown
	for i, raw := range p.original.Catalogs {
		same = same && l.catalogs[i] == sha256.Sum256(raw)
	}
	if same {
		return p.held(ctx)
	}
	record, claim := *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim
	if storagePolicyActualPair(p.original.Catalogs) {
		state, err := p.c.owner.readOriginalPolicy(ctx)
		if err != nil {
			return err
		}
		if !bytes.Equal(state.Actual[0], p.original.Catalogs[0]) || !bytes.Equal(state.Actual[1], p.original.Catalogs[1]) {
			return errStorageCustody
		}
		if state.Packed != nil {
			if err = storagePolicyCompletedQ(state, *p.initialInspection); err != nil {
				return err
			}
			if err = p.identityCatalogGroup(ctx, "lin4/reopen-base/persist", state.Selected, state.Actual[state.Selected], false); err != nil {
				return err
			}
		} else {
			pair, err := storageOwnerV6NativeReadPair(state.Actual[0], state.Actual[1], &state.Catalog, state.Evidence.Raw, record, claim)
			if err != nil {
				return err
			}
			high := pair.Banks[pair.Selected]
			if storagePolicyNativePreparedLineage(pair) {
				if err = p.validateIdentityPhysicalRow(ctx, high.Stage); err != nil {
					return err
				}
				keys, err := storageIdentityCatalogKeys(high.Stage.Transition)
				if err != nil {
					return err
				}
				if err = p.takeIdentityCatalog(ctx, keys[0], pair.Selected); err != nil {
					return err
				}
				if err = p.identityCatalogGroup(ctx, keys[1], pair.Selected, high.Stage.Raw, false); err != nil {
					return err
				}
				active := high
				active.Stage.Sequence++
				active.Stage.Publication = storageCompositionActive
				active.Stage.Previous, err = storageCompositionChecksumOf(high.Stage.Raw)
				if err != nil {
					return err
				}
				raw, err := active.encode(record, claim)
				if err != nil {
					return err
				}
				if err = p.identityCatalogGroup(ctx, keys[2], 1-pair.Selected, raw, true); err != nil {
					return err
				}
				if err = p.identityCatalogGroup(ctx, keys[3], 1-pair.Selected, raw, false); err != nil {
					return err
				}
			} else {
				if pair.RecoveryOnly {
					return errStorageEnrollmentUnresolved
				}
				if err = p.identityCatalogGroup(ctx, "lin4/reopen-base/persist", pair.Selected, high.Stage.Raw, false); err != nil {
					return err
				}
			}
		}
		p.identityCatalogsKnown()
		return p.held(ctx)
	}
	pair, err := storageCompositionReadPair(p.original.Catalogs[0], p.original.Catalogs[1], record, claim)
	if err != nil {
		return err
	}
	high := pair.Banks[pair.Selected]
	if storageIdentityPreparedLineage(pair) {
		if high.TransitionID != p.identityPlan.PlanID {
			return errStorageCustody
		}
		keys, e := storageIdentityCatalogKeys(high.Transition)
		if e != nil {
			return e
		}
		// Prepared's complete original image is present. Its write is historical
		// spend; only persistence and the still-owed Active publication continue.
		if e = p.validateIdentityPhysicalRow(ctx, high); e != nil {
			return e
		}
		if e = p.takeIdentityCatalog(ctx, keys[0], byte(pair.Selected)); e != nil {
			return e
		}
		if e = p.identityCatalogGroup(ctx, keys[1], byte(pair.Selected), high.Raw, false); e != nil {
			return e
		}
		active := high
		if active.Sequence == ^uint64(0) {
			return storagebudget.ErrBounds
		}
		active.Sequence++
		active.Publication = storageCompositionActive
		active.Previous, e = storageCompositionChecksumOf(high.Raw)
		if e != nil {
			return e
		}
		raw, e := active.encode(record, claim)
		if e != nil || !storageCompositionFinalize(high, active) {
			return errors.Join(errStorageCustody, e)
		}
		bank := byte(1 - pair.Selected)
		if e = p.identityCatalogGroup(ctx, keys[2], bank, raw, true); e != nil {
			return e
		}
		if e = p.identityCatalogGroup(ctx, keys[3], bank, raw, false); e != nil {
			return e
		}
	} else {
		if pair.RecoveryOnly {
			return errStorageEnrollmentUnresolved
		}
		// A selected Active may have died before Sync/Close. This unchanged shared
		// once-total key finishes the exact original bytes; it is no rewrite.
		if err = p.identityCatalogGroup(ctx, "lin4/reopen-base/persist", byte(pair.Selected), high.Raw, false); err != nil {
			return err
		}
	}
	p.identityCatalogsKnown()
	return p.held(ctx)
}
func (p *storageEnrollmentEpoch) validateIdentityPhysicalRow(ctx context.Context, bank storageCompositionBank) error {
	if bank.TransitionID != p.identityPlan.PlanID {
		return storagebudget.ErrAuthority
	}
	if bank.Transition == storageCompositionPlan {
		return bank.Lineage.validateGraph(p.c.ref.cell.ownerCatalog, *p.c.ref.cell.ownerRecord)
	}
	if bank.Transition == storageCompositionStamp {
		for _, s := range bank.Lineage.Rows.LineageStampCompletions {
			if s.PlanID == p.identityPlan.PlanID && s.StampDisposition != storageOwnerV5Moved {
				actual, moved, e := p.originalStampDisposition(ctx)
				if e != nil || moved || actual != s {
					return errors.Join(errStorageCustody, e)
				}
				return nil
			}
		}
	}
	m := p.identity
	if m == nil || m.record.Core.Phase != storageIntentRetired || m.record.Core.Outcome != storageIntentPublished {
		return errStorageEnrollmentUnresolved
	}
	receipt, err := m.receipt(ctx)
	if err != nil {
		return err
	}
	switch bank.Transition {
	case storageCompositionCompletion:
		if m.record.Action != storageIdentityIntentCreate && m.record.Action != storageIdentityIntentInitialCAS {
			return storagebudget.ErrAuthority
		}
		for _, c := range bank.Lineage.Rows.LineageCompletions {
			if c.PlanID == p.identityPlan.PlanID && c.Initial == receipt {
				return nil
			}
		}
	case storageCompositionStamp:
		if m.record.Action != storageIdentityIntentStamp {
			return storagebudget.ErrAuthority
		}
		for _, s := range bank.Lineage.Rows.LineageStampCompletions {
			if s.PlanID == p.identityPlan.PlanID && s.StampReceipt == receipt && s.StampSourceBefore == m.record.AllocatedStage && s.StampTargetBefore == (storageOwnerPersistentObject{}) && s.StampSourceAfter == (storageOwnerPersistentObject{}) && s.StampTargetAfter == m.record.Target {
				return nil
			}
		}
	case storageCompositionContinuation:
		if m.record.Action != storageIdentityIntentClear {
			return storagebudget.ErrAuthority
		}
		for _, f := range bank.Lineage.Rows.LineageContinuations {
			if f.PlanID == p.identityPlan.PlanID && f.Rewrite == receipt {
				return nil
			}
		}
	}
	return errStorageCustody
}
func (p *storageEnrollmentEpoch) persistOriginalIdentityReceipt(ctx context.Context, m *storageIdentityLive) error {
	if m == nil || p.identity != m || m.record.Core.Phase != storageIntentRetired {
		return storagebudget.ErrAuthority
	}
	if m.record.Core.Outcome == storageIntentAborted {
		if err := m.recoveryNativeCurrent(ctx); err != nil {
			return err
		}
		m.finished = true
		p.markApplication(p.identityPlan.PlanID, m.record.Action, true, true)
		return m.captureRetiredOriginal(ctx)
	}
	_, lineage, err := p.identityGraph()
	if err != nil {
		return err
	}
	status, err := p.c.ref.cell.q.InspectHostState(ctx)
	if err != nil {
		return err
	}
	if p.c.owner.identityReceiptCurrent(ctx, m.record, m.selected.page, lineage, status) {
		// A pending physical receipt has just been completed; actual same-token Q
		// inspection still supplies initial factual capture, never a newer slot.
		if _, err = m.receipt(ctx); err != nil {
			return err
		}
		m.finished = true
		p.markApplication(p.identityPlan.PlanID, m.record.Action, true, true)
		return m.captureRetiredOriginal(ctx)
	}
	switch m.record.Action {
	case storageIdentityIntentCreate, storageIdentityIntentInitialCAS:
		err = m.persistCompletion(ctx)
	case storageIdentityIntentStamp:
		receipt, e := m.receipt(ctx)
		if e != nil {
			return e
		}
		row := storageOwnerV5StampCompletionRow{PlanID: p.identityPlan.PlanID, PlanDigest: m.record.PlanDigest, StampDisposition: storageOwnerV5Moved, StampSourceBefore: m.record.AllocatedStage, StampTargetAfter: m.record.Target, StampReceipt: receipt}
		err = p.persistStampRow(ctx, row)
		if err == nil {
			m.finished = true
			p.markApplication(p.identityPlan.PlanID, m.record.Action, true, true)
		}
	case storageIdentityIntentClear:
		err = m.persistContinuation(ctx)
	default:
		return storagebudget.ErrBounds
	}
	if err != nil {
		return err
	}
	return m.captureRetiredOriginal(ctx)
}

// Creation/local actions and exact parent/child enrollment obligations share
// the construction's original accumulator. These are finite resource boards;
// neither a board nor a decoded page constructs a live member or native grant.
func (m *storageCreationLive) launchAction() byte {
	if m.record.Kind == storageCreationLocalPrivate {
		return 0
	}
	return 128 + m.record.Kind
}
func storageIdentityLaunchAddGroup(l *storageIdentityLaunch, g storageIdentityLaunchGroup) error {
	for _, old := range l.groups {
		if old.key == g.key {
			return storagebudget.ErrBounds
		}
	}
	v, err := storageAdvisoryEstimate(g.bytes, g.files, g.directories, g.metadata)
	if err != nil {
		return err
	}
	l.peak, err = storageIdentityAddCapacity(l.peak, v)
	if err != nil {
		return err
	}
	l.groups = append(l.groups, g)
	return nil
}
func (p *storageEnrollmentEpoch) prepareCreationLaunch(ctx context.Context, m *storageCreationLive) error {
	if p.creation != m || m.reservation == nil || m.started && !m.recovering || p.scope != nil {
		return storagebudget.ErrAuthority
	}
	launch := p.c.identityLaunch
	if launch == nil {
		launch = &storageIdentityLaunch{construction: p.c}
		launch.self = launch
	}
	if launch.self != launch || launch.construction != p.c || launch.failed != nil {
		return storagebudget.ErrAuthority
	}
	next := *launch
	next.groups = append([]storageIdentityLaunchGroup{}, launch.groups...)
	next.applications = append([]storageIdentityLaunchApplication{}, launch.applications...)
	next.enrollments = append([]storageIdentityEnrollmentBoard{}, launch.enrollments...)
	for _, a := range next.applications {
		if a.plan == m.record.Core.OperationID {
			return storagebudget.ErrAuthority
		}
	}
	next.applications = append(next.applications, storageIdentityLaunchApplication{plan: m.record.Core.OperationID, action: m.launchAction(), components: append([]storagebudget.Component{}, m.record.Core.Components...), settled: m.recovering && p.initialInspection.View.Slots[m.admitted.Slot].Ticket.State == storagebudget.Terminal})
	groups, err := storageIdentityFiniteGroups(0, true, m.recovering)
	if err != nil {
		return err
	}
	if m.record.Kind != storageCreationLocalPrivate {
		// J4 retains its unchanged fifteen-page completion ceiling and its separate
		// full enrollment tail. The Settle prior-checkpoint ceiling also coexists.
		groups = groups[:len(groups)-6]
		groups = append(groups, storageIdentityFiniteGroup{ID: 14, Parent: 1, Bytes: 16384, Metadata: 16}, storageIdentityFiniteGroup{ID: 15, Parent: 1, Bytes: 16384, Metadata: 16})
		for i, e := range m.record.Effects {
			f, d := uint64(1), uint64(0)
			if e.Kind == 1 {
				f, d = 0, 1
			}
			groups = append(groups, storageIdentityFiniteGroup{ID: uint16(30 + 2*i), Parent: 3, Bytes: e.Bytes, Files: f, Directories: d, Metadata: 28}, storageIdentityFiniteGroup{ID: uint16(31 + 2*i), Parent: 3, Bytes: e.Bytes, Metadata: 28})
		}
		tail := m.record.Tail
		peaks := tail.ParentPeaks
		for _, g := range storageCreationControlPlan() {
			v, ok := storageEnrollmentPeakEstimate(g)
			if !ok || peaks[g.Parent-1].Bytes < v.Bytes || peaks[g.Parent-1].Inodes < v.Inodes {
				return storagebudget.ErrBounds
			}
			peaks[g.Parent-1].Bytes -= v.Bytes
			peaks[g.Parent-1].Inodes -= v.Inodes
		}
		for _, old := range next.enrollments {
			if old.bridge == tail.Bridge {
				return storagebudget.ErrAuthority
			}
		}
		next.enrollments = append(next.enrollments, storageIdentityEnrollmentBoard{bridge: tail.Bridge, peaks: peaks, parents: [2]*os.Root{p.c.original.config, p.c.anchor}, natives: [2]storageOwnerPersistentObject{tail.Config, tail.Anchor}, logical: tail.Logical})
		for _, v := range peaks {
			next.peak, err = storageIdentityAddCapacity(next.peak, v)
			if err != nil {
				return err
			}
		}
	}
	for _, g := range groups {
		parent, native, target := p.c.original.config, p.c.ref.cell.ownerRecord.Config, "storage-intent"
		if g.Parent == 2 {
			parent, native, target = p.c.anchor, p.c.ref.cell.ownerRecord.Anchor, "Q"
		}
		if g.Parent == 3 {
			parent, native, target = m.parents[0].root, m.record.Parents[0].Native, m.record.SelectedPath
		}
		if err = storageIdentityLaunchAddGroup(&next, storageIdentityLaunchGroup{key: storageIdentityLaunchKey{Plan: m.record.Core.OperationID, Action: m.launchAction(), Attempt: m.launchAttempt(), Group: g.ID, Parent: native, Target: target}, parent: parent, bytes: g.Bytes, files: g.Files, directories: g.Directories, metadata: g.Metadata}); err != nil {
			return err
		}
	}
	if err = p.checkIdentityLaunch(ctx, &next); err != nil {
		return err
	}
	*launch = next
	launch.self = launch
	p.c.identityLaunch = launch
	p.identityLaunch = launch
	return nil
}
func (m *storageCreationLive) takeLaunch(ctx context.Context, id uint16) error {
	p := m.phase
	l := p.identityLaunch
	if l == nil || l != p.c.identityLaunch || l.self != l || l.failed != nil {
		return storagebudget.ErrAuthority
	}
	if err := p.checkIdentityLaunch(ctx, l); err != nil {
		return err
	}
	for i := range l.groups {
		g := &l.groups[i]
		if g.key.Plan == m.record.Core.OperationID && g.key.Action == m.launchAction() && g.key.Attempt == m.launchAttempt() && g.key.Group == id {
			if g.spent {
				return storagebudget.ErrAuthority
			}
			g.spent = true
			return nil
		}
	}
	return storagebudget.ErrAuthority
}
func (m *storageCreationLive) effectLaunchGroup(index int, persist bool) uint16 {
	at := index
	if m.record.Kind == storageCreationLocalPrivate {
		switch m.record.Effects[index].Name {
		case "dialect-drafts":
			at = 0
		case ".gitignore":
			at = 1
		case "workflow.lock":
			at = 2
		default:
			return 0
		}
	}
	id := uint16(30 + 2*at)
	if persist {
		id++
	}
	return id
}

func (p *storageEnrollmentEpoch) admitScopeLaunch(ctx context.Context, s storageEnrollmentPeakScope) error {
	if p != nil && p.transfer != nil && p.transfer.operation != nil && p.transfer.operation.tailAdmitted {
		return p.transfer.operation.consumeOriginalTransferTail(ctx, s)
	}
	if p.qualified == nil || p.original == nil || p.scope != nil && *p.scope != s {
		return storagebudget.ErrAuthority
	}
	launch := p.c.identityLaunch
	if launch == nil {
		launch = &storageIdentityLaunch{construction: p.c}
		launch.self = launch
	}
	if launch.self != launch || launch.construction != p.c || launch.failed != nil {
		return storagebudget.ErrAuthority
	}
	next := *launch
	next.enrollments = append([]storageIdentityEnrollmentBoard{}, launch.enrollments...)
	var bridge storagebudget.ID
	copy(bridge[:], s[24:40])
	var peaks [2]storagebudget.CapacityVector
	for _, g := range storageEnrollmentPeakPlan(s) {
		v, ok := storageEnrollmentPeakEstimate(g)
		if !ok {
			return storagebudget.ErrBounds
		}
		var e error
		peaks[g.Parent-1], e = storageIdentityAddCapacity(peaks[g.Parent-1], v)
		if e != nil {
			return e
		}
	}
	logical := storagebudget.Vector{Bytes: storageEnrollmentPeakU64(s[360:368]), Files: storageEnrollmentPeakU64(s[368:376]), Directories: storageEnrollmentPeakU64(s[376:384])}
	found := false
	for i := range next.enrollments {
		old := &next.enrollments[i]
		if old.bridge != bridge {
			continue
		}
		if found || old.scope != nil && *old.scope != s {
			return errStorageCustody
		}
		found = true
		for j, v := range peaks {
			if v.Bytes > old.peaks[j].Bytes || v.Inodes > old.peaks[j].Inodes {
				return storagebudget.ErrBounds
			}
		}
		if logical.Bytes > old.logical.Bytes || logical.Files > old.logical.Files || logical.Directories > old.logical.Directories {
			return storagebudget.ErrBounds
		}
		copy := s
		old.scope = &copy
	}
	if !found {
		copy := s
		next.enrollments = append(next.enrollments, storageIdentityEnrollmentBoard{bridge: bridge, peaks: peaks, logical: logical, scope: &copy, parents: [2]*os.Root{p.c.original.config, p.c.anchor}, natives: [2]storageOwnerPersistentObject{p.c.ref.cell.ownerRecord.Config, p.c.ref.cell.ownerRecord.Anchor}})
		for _, v := range peaks {
			var e error
			next.peak, e = storageIdentityAddCapacity(next.peak, v)
			if e != nil {
				return e
			}
		}
	}
	if err := p.checkIdentityLaunch(ctx, &next); err != nil {
		return err
	}
	*launch = next
	launch.self = launch
	p.c.identityLaunch = launch
	p.identityLaunch = launch
	return nil
}

func (p *storageEnrollmentEpoch) markApplication(plan storagebudget.ID, action byte, settled, complete bool) {
	if p.identityLaunch == nil {
		return
	}
	for i := range p.identityLaunch.applications {
		a := &p.identityLaunch.applications[i]
		if a.plan == plan && a.action == action {
			a.settled = a.settled || settled
			a.complete = a.complete || complete
		}
	}
}

func (p *storageEnrollmentEpoch) completeScopeLaunch(e storageEnrollmentPeakEvidence) {
	if p.identityLaunch == nil {
		return
	}
	for i := range p.identityLaunch.enrollments {
		board := &p.identityLaunch.enrollments[i]
		if board.bridge == e.Bridge && board.scope != nil && *board.scope == e.Scope {
			board.complete = true
		}
	}
	if p.creation != nil && p.creation.record.Kind != storageCreationLocalPrivate && storageCreationTailIncluded(p.creation.record, e) {
		p.markApplication(p.creation.record.Core.OperationID, p.creation.launchAction(), true, true)
	}
}

func (m *storageCreationLive) launchAttempt() byte {
	if m.recovering {
		return 2
	}
	return 1
}

// Only a selected local J5 with a captured current effect or all durable
// effects can recover. A latest Owed effect is uncertain and never reissued.
func (p *storageEnrollmentEpoch) joinOriginalLocalCreation(ctx context.Context) (joined bool, err error) {
	if p.creation != nil || p.qualified == nil || p.qualified.record.Version < 2 {
		return false, storagebudget.ErrAuthority
	}
	var choice *storageCreationHostSelection
	pendingIdentity := false
	installation, binding := p.c.ref.cell.ownerRecord.Installation, p.c.ref.cell.binding
	_, lineage, e := p.identityGraph()
	if e != nil {
		return false, e
	}
	for slot, pages := range p.original.Hosts {
		selectedPendingIdentity := false
		selected, e := storageSelectCreationHost(pages[0], pages[1], slot, installation, binding)
		if e != nil {
			return false, e
		}
		for _, identity := range []*storageIdentityIntent{selected.Identity, selected.PredecessorIdentity} {
			if identity == nil {
				continue
			}
			page, e := storageEncodeIdentityIntent(*identity, installation, binding)
			if e != nil {
				return false, e
			}
			if !p.c.owner.identityReceiptCurrent(ctx, *identity, page, lineage, storageHostState(p.initialInspection.View)) {
				if identity != selected.Identity || p.identityPlan == nil {
					return false, errStorageEnrollmentUnresolved
				}
				digest, e := storageOwnerV5PlanDigest(*p.identityPlan)
				if e != nil || digest != identity.PlanDigest {
					return false, errors.Join(errStorageEnrollmentUnresolved, e)
				}
				pendingIdentity, selectedPendingIdentity = true, true
			}
		}
		if selected.Creation == nil {
			if selectedPendingIdentity {
				continue
			}
			if !selected.Slot.record.reusable() {
				return false, errStorageEnrollmentUnresolved
			}
			continue
		}
		if selected.Creation.Kind != storageCreationLocalPrivate {
			if !p.c.owner.creationTailCurrent(ctx, *selected.Creation, selected.Slot.page, storageHostState(p.initialInspection.View)) {
				return false, errStorageEnrollmentUnresolved
			}
			continue
		}
		if p.c.owner.creationTailCurrent(ctx, *selected.Creation, selected.Slot.page, storageHostState(p.initialInspection.View)) {
			continue
		}
		if choice != nil {
			return false, errStorageEnrollmentUnresolved
		}
		copy := selected
		choice = &copy
	}
	if choice == nil {
		return false, nil
	}
	if pendingIdentity {
		return false, errStorageEnrollmentUnresolved
	}
	r := *choice.Creation
	if r.Core.Phase < storageIntentStaging || r.Core.Phase == storageIntentRejected || r.Core.AdmissionEpoch != p.initialInspection.View.Configuration.Epoch || r.SelectedPath != dialectDraftStoragePath(p.originalProjectPath()) {
		return false, errStorageEnrollmentUnresolved
	}
	captured, allDurable := false, true
	for _, effect := range r.Effects {
		captured = captured || effect.State == storageCreationCaptured
		allDurable = allDurable && effect.State == storageCreationDurable
	}
	if !captured && !allDurable {
		return false, errStorageEnrollmentUnresolved
	}
	requester := p.originalServer()
	if p.startupProject != nil {
		requester = nil
	}
	m := &storageCreationLive{phase: p, requester: requester, record: r, selected: choice.Slot, recovering: true, started: true}
	m.self = m
	defer func() {
		if !joined {
			if p.creation == m {
				p.creation = nil
			}
			err = errors.Join(err, m.Close())
		}
	}()
	if err = p.bindCreationParents(ctx, m); err != nil {
		return false, err
	}
	if err = m.validateOriginalSelection(ctx); err != nil {
		return false, err
	}
	for i, effect := range r.Effects {
		if effect.Kind == 2 {
			if effect.Name == ".gitignore" {
				m.payloads[i] = []byte("*\n")
			} else {
				m.payloads[i] = []byte{}
			}
		}
		if effect.State == storageCreationOwed {
			continue
		}
		var parent *os.Root
		if int(effect.Parent) < len(m.parents) {
			parent = m.parents[effect.Parent].root
		} else {
			parent = m.objects[int(effect.Parent)-len(m.parents)].root
		}
		if parent == nil {
			return false, errStorageCustody
		}
		object := &m.objects[i]
		object.parent, object.name = parent, effect.Name
		if effect.Kind == 1 {
			edge, e := storagePinChild(parent, effect.Name, true)
			if e != nil {
				return false, e
			}
			object.root, object.identity = edge.root, edge.identity
			object.lease, e = storageCreatorAcquire(edge.root)
			if e != nil {
				return false, e
			}
			object.file = object.lease.file
			object.borrow, e = object.lease.borrow(ctx, edge.root)
			if e != nil {
				return false, e
			}
		} else {
			object.file, err = parent.OpenFile(effect.Name, os.O_RDONLY|openNoFollow|openNonBlocking, 0)
			if err != nil {
				return false, err
			}
			object.identity, err = object.file.Stat()
			if err != nil {
				return false, err
			}
		}
		object.native, err = storageAdvisoryPersistentHeld(ctx, object.file)
		if err != nil || object.native != effect.Native {
			return false, errors.Join(errStorageCustody, err)
		}
		m.effectCalls[i][0] = true
		m.effectCalls[i][1] = effect.State == storageCreationDurable
	}
	for _, control := range p.c.original.files {
		if control.root == p.c.original.config && control.name == storageIntentName {
			m.host = control
		}
	}
	if m.host.file == nil {
		return false, errStorageCustody
	}
	m.request, err = storageCreationRequestBound(r, binding)
	if err != nil {
		return false, err
	}
	ticket := p.initialInspection.View.Slots[r.Core.Token.Slot].Ticket
	if !storageCreationTicketMatchesBound(r, ticket, binding) || ticket.PolicyRevision != r.Core.PolicyRevision {
		return false, errStorageEnrollmentUnresolved
	}
	exact := ticket.Token == r.Core.Token
	if r.Core.Phase == storageIntentStaging || r.Core.Phase == storageIntentPublishing {
		exact = exact && ticket.State == storagebudget.Staging
	} else if r.Core.Phase == storageIntentSettling {
		exact = exact && ticket.State == storagebudget.Staging || ticket.State == storagebudget.Terminal && storageIntentNextToken(r.Core.Token, ticket.Token) && ticket.Evidence == r.Core.Evidence && reflect.DeepEqual(ticket.Changes, r.Core.Changes)
	} else {
		exact = exact && ticket.State == storagebudget.Terminal && ticket.Evidence == r.Core.Evidence && reflect.DeepEqual(ticket.Changes, r.Core.Changes)
	}
	if !exact || r.Core.Phase == storageIntentRetired && !storageIntentRetiredProof(r.Core, storageHostState(p.initialInspection.View)) {
		return false, errStorageEnrollmentUnresolved
	}
	if e = storageOriginalActionRequest(ctx, p.c.ref.cell.q, r.Core, m.request, storageHostState(p.initialInspection.View), ticket.Token); e != nil {
		return false, e
	}
	m.admitted = ticket.Token
	m.qCalls[0], m.qCalls[1] = true, true
	for _, slot := range p.initialInspection.View.Slots {
		if storageUnretired(slot) && slot.Ticket.Token != m.admitted {
			return false, errStorageEnrollmentUnresolved
		}
	}
	for _, control := range p.c.original.files {
		if control.name != storageIntentName && !(control.root == p.c.anchor && (control.name == "tickets.a" || control.name == "tickets.b" || control.name == "manifests")) {
			continue
		}
		if e = control.current(); e != nil {
			return false, e
		}
		if e = storageEnrollmentNativeDense(ctx, control.file, uint64(control.size)); e != nil {
			return false, e
		}
	}
	if r.Core.Phase >= storageIntentSettled {
		m.qCalls[2] = true
	}
	if r.Core.Phase == storageIntentRetired {
		m.qCalls[3] = true
	}
	m.reservation = &storageCreationLiveReservation{member: m, qualification: p.qualified.digest, originalCheckpoint: sha256.Sum256(p.initialInspection.Checkpoints[p.initialInspection.CheckpointBank])}
	p.creation = m
	if err = m.current(ctx); err != nil {
		return false, err
	}
	if err = m.validatePayloadsAndDemand(); err != nil {
		return false, err
	}
	if err = p.prepareCreationLaunch(ctx, m); err != nil {
		return false, err
	}
	for i := range p.identityLaunch.groups {
		g := &p.identityLaunch.groups[i]
		if g.key.Plan != r.Core.OperationID || g.key.Action != m.launchAction() || g.key.Attempt != 2 {
			continue
		}
		if g.key.Group >= 20 && g.key.Group <= 22 {
			g.spent = true
		}
		if r.Core.Phase >= storageIntentSettled && g.key.Group >= 23 && g.key.Group <= 24 {
			g.spent = true
		}
		if r.Core.Phase == storageIntentRetired && g.key.Group == 25 {
			g.spent = true
		}
		for at, effect := range r.Effects {
			if effect.State >= storageCreationCaptured && g.key.Group == m.effectLaunchGroup(at, false) || effect.State == storageCreationDurable && g.key.Group == m.effectLaunchGroup(at, true) {
				g.spent = true
			}
		}
	}
	return true, nil
}
func (m *storageCreationLive) persistOriginalLocal(ctx context.Context) (err error) {
	if !m.recovering || m.record.Kind != storageCreationLocalPrivate {
		return storagebudget.ErrAuthority
	}
	if err = m.takeLaunch(ctx, 1); err != nil {
		return err
	}
	m.hostWrites = 1
	if err = m.enter(ctx); err != nil {
		return err
	}
	err = func() error {
		raw := make([]byte, storageIntentBytes)
		n, e := m.host.file.ReadAt(raw, 0)
		if e != nil || n != len(raw) {
			return errors.Join(errStorageCustody, e)
		}
		for slot, pages := range m.phase.original.Hosts {
			for bank, page := range pages {
				at := (slot*2 + bank) * storageIntentPage
				if !bytes.Equal(raw[at:at+storageIntentPage], page) {
					return errStorageCustody
				}
			}
		}
		f, e := storageOpenFile(m.host.root, m.host.name, m.host.size)
		if e != nil {
			return e
		}
		if !os.SameFile(f.identity, m.host.identity) {
			return errors.Join(errStorageCustody, f.file.Close())
		}
		e = m.phase.permit(ctx)
		if e == nil {
			e = f.file.Sync()
		}
		return errors.Join(e, f.current(), f.file.Close())
	}()
	err = m.phase.leaveNative(err)
	if err != nil {
		return err
	}
	before, err := m.phase.c.ref.cell.q.InspectAction(ctx)
	if err != nil {
		return err
	}
	if err = m.takeLaunch(ctx, 40); err != nil {
		return err
	}
	if err = m.enter(ctx); err != nil {
		return err
	}
	err = func() (err error) {
		parent, err := m.phase.c.anchor.Open(".")
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, parent.Close()) }()
		for _, name := range []string{"lock", "checkpoint.a", "checkpoint.b", "tickets.a", "tickets.b", "manifests"} {
			f, e := m.phase.c.openOriginalAnchorControl(ctx, name)
			if e != nil {
				return e
			}
			e = m.phase.permit(ctx)
			if e == nil {
				e = f.file.Sync()
			}
			if e = errors.Join(e, f.current(), f.file.Close()); e != nil {
				return e
			}
		}
		if err = m.phase.permit(ctx); err != nil {
			return err
		}
		return parent.Sync()
	}()
	err = m.phase.leaveNative(err)
	if err != nil {
		return err
	}
	after, err := m.phase.c.ref.cell.q.InspectAction(ctx)
	if err != nil || !reflect.DeepEqual(before, after) {
		return errors.Join(errStorageCustody, err)
	}
	return m.current(ctx)
}

func (c *storageBudgetConstruction) completeOriginalLocalCreation(ctx context.Context) (err error) {
	if err = c.releaseStartupHolds(); err != nil {
		return err
	}
	var p *storageEnrollmentEpoch
	if c.home.exclusive {
		p, err = c.beginStartupEnrollment(ctx)
	} else {
		p, err = c.beginEnrollmentEpoch(ctx)
	}
	if err != nil {
		return err
	}
	defer func() { err = p.finish(err) }()
	if p.startup {
		if err = p.selectStartupOriginalRequester(ctx); err != nil {
			return err
		}
	}
	if err = p.acquireIdentity(ctx); err != nil {
		return err
	}
	c.ref.cell.mu.Lock()
	existing := c.ref.cell.enrollmentEndpoint != nil
	c.ref.cell.mu.Unlock()
	if existing {
		err = p.captureExistingOriginal(ctx)
	} else {
		err = p.captureInitialOriginal(ctx)
	}
	if err != nil {
		return err
	}
	if err = p.qualifyOriginalNative(ctx); err != nil {
		return err
	}
	joined, err := p.joinOriginalLocalCreation(ctx)
	if err != nil {
		return err
	}
	if !joined {
		return nil
	}
	return p.creation.execute(ctx)
}

func (p *storageEnrollmentEpoch) originalAccountingKey(ctx context.Context) (string, error) {
	s := p.originalServer()
	if entry := p.originalHomeEntry(); entry != nil {
		return entry.RunnerWorkspace, nil
	}
	_, lineage, err := p.identityGraph()
	if err != nil {
		return "", err
	}
	history, err := p.originalHistoryIdentity(ctx)
	if err != nil {
		return "", err
	}
	key := ""
	for _, plan := range lineage.Rows.LineagePlans {
		if plan.ProjectObject != p.original.Project {
			continue
		}
		if key != "" || plan.ConfigObject != p.c.ref.cell.ownerRecord.Config || string(plan.HistoryIdentity) != history {
			return "", errStorageCustody
		}
		key = string(plan.AccountingOwnerKey)
	}
	if key != "" {
		return key, nil
	}
	return s.inspectStorageStartupKey()
}

// Initialize pending projection from the exact original joined terminal, whose
// replay is already incorporated in Q's loaded usage. Completion still depends
// solely on the durable lineage receipt; independent debt/peaks remain exact.
func (p *storageEnrollmentEpoch) originalIdentityApplication(ctx context.Context, plan storagebudget.ID, action byte, components []storagebudget.Component, complete bool) (storageIdentityLaunchApplication, error) {
	a := storageIdentityLaunchApplication{plan: plan, action: action, components: append([]storagebudget.Component{}, components...), settled: complete, complete: complete}
	m := p.identity
	if complete || m == nil || !m.recovering || m.self != m || m.phase != p || m.record.Action != action {
		return a, nil
	}
	if p.identityPlan == nil || p.identityPlan.PlanID != plan || !reflect.DeepEqual(m.request.Components, components) || p.initialInspection == nil {
		return a, storagebudget.ErrAuthority
	}
	status := storageHostState(p.initialInspection.View)
	t := status.Slots[m.admitted.Slot].Ticket
	if t.State != storagebudget.Terminal {
		return a, nil
	}
	if t.Token != m.admitted || !storageIdentityIntentTicketMapping(m.record, t, m.request.Owner) {
		return a, errStorageEnrollmentUnresolved
	}
	if err := storageOriginalActionRequest(ctx, p.c.ref.cell.q, m.record.Core, m.request, status, t.Token); err != nil {
		return a, err
	}
	a.settled = true
	return a, nil
}

// Classify original pending work under startup's retained home exclusion, before
// selecting a project prefix. These are comparison operands, never a requester
// Server or a new native/registration capability.
func (c *storageBudgetConstruction) pendingStartupOriginal(ctx context.Context) (plan *storageOwnerV5LineagePlan, local *storageCreationIntent, err error) {
	if c == nil || c.self != c || c.owner == nil || c.ref == nil || c.sources == nil || !c.home.exclusive {
		return nil, nil, storagebudget.ErrAuthority
	}
	if err = c.current(ctx, true); err != nil {
		return nil, nil, err
	}
	record, claim := *c.ref.cell.ownerRecord, c.ref.cell.ownerClaim
	var catalogs [2][]byte
	for i, name := range []string{"storage-owner.a", "storage-owner.b"} {
		raw, held, e := storageOwnerReadFixed(c.original.config, name, storageOwnerCatalogBytes)
		if e != nil {
			return nil, nil, e
		}
		e = errors.Join(held.current(), held.file.Close())
		if e != nil {
			return nil, nil, e
		}
		catalogs[i] = raw
	}
	lineage, durable := storageCompositionEmptyLineage(), storageCompositionEmptyLineage()
	founding := false
	if storagePolicyActualPair(catalogs) {
		state, e := c.owner.readOriginalPolicy(ctx)
		if e != nil {
			return nil, nil, e
		}
		lineage, durable = state.Lineage, state.Lineage
		if state.RecoveryOnly {
			pair, e := storageOwnerV6NativeReadPair(catalogs[0], catalogs[1], &state.Catalog, state.Evidence.Raw, record, claim)
			if e != nil || !storagePolicyNativePreparedLineage(pair) {
				return nil, nil, errors.Join(errStorageEnrollmentUnresolved, e)
			}
			durable = pair.Banks[1-pair.Selected].Stage.Lineage
		}
	} else if string(catalogs[0][:8]) == "DSKONL1\x00" || string(catalogs[1][:8]) == "DSKONL1\x00" {
		pair, e := storageCompositionReadPair(catalogs[0], catalogs[1], record, claim)
		if c.startupFirstBase && (e != nil || pair.Banks[pair.Selected].Transition == storageCompositionBase) {
			// Both identical Prepared wrappers are a successful strict pair read,
			// but still belong to the original first-base route, not P/J lineage.
			// Recheck the retained physical/core images and complete Q/input
			// commitment before selecting only that route's original primary.
			for i := range catalogs {
				if !bytes.Equal(catalogs[i], c.startupPhysical[i]) {
					return nil, nil, errStorageEnrollmentUnresolved
				}
			}
			if e = c.validateStartupCompositionQ(ctx, record, claim); e != nil {
				return nil, nil, e
			}
			founding = true
		} else {
			if e != nil {
				return nil, nil, e
			}
			if pair.Banks[pair.Selected].Transition == storageCompositionBase || pair.RecoveryOnly && !storageIdentityPreparedLineage(pair) {
				return nil, nil, errStorageEnrollmentUnresolved
			}
			lineage = pair.Banks[pair.Selected].Lineage
			durable = lineage
			if storageIdentityPreparedLineage(pair) {
				durable = pair.Banks[1-pair.Selected].Lineage
			}
			if e = lineage.validateGraph(c.ref.cell.ownerCatalog, record); e != nil {
				return nil, nil, e
			}
		}
	}
	for _, row := range lineage.Rows.LineagePlans {
		complete, stamp, continuation := false, false, false
		for _, r := range durable.Rows.LineageCompletions {
			complete = complete || r.PlanID == row.PlanID
		}
		identity, e := parseIdentity(row.IdentityInitial)
		if e != nil {
			return nil, nil, e
		}
		if identity.From != "" {
			for _, r := range durable.Rows.LineageStampCompletions {
				stamp = stamp || r.PlanID == row.PlanID
			}
			for _, r := range durable.Rows.LineageContinuations {
				continuation = continuation || r.PlanID == row.PlanID
			}
			complete = complete && stamp && continuation
		}
		if !complete {
			if plan != nil {
				return nil, nil, errStorageEnrollmentUnresolved
			}
			copy := row
			plan = &copy
		}
	}
	status, e := c.ref.cell.q.InspectHostState(ctx)
	if e != nil {
		return nil, nil, e
	}
	raw, held, e := storageOwnerReadFixed(c.original.config, storageIntentName, storageIntentBytes)
	if e != nil {
		return nil, nil, e
	}
	defer func() {
		err = errors.Join(err, held.current(), held.file.Close())
		if err != nil {
			plan, local = nil, nil
		}
	}()
	for slot := 0; slot < storagebudget.Slots; slot++ {
		start := slot * 2 * storageIntentPage
		choice, e := storageSelectCreationHost(raw[start:start+storageIntentPage], raw[start+storageIntentPage:start+2*storageIntentPage], slot, record.Installation, record.QBinding)
		if e != nil {
			return nil, nil, e
		}
		if founding && (choice.Identity != nil || !choice.Slot.record.reusable() || choice.Creation != nil && !c.owner.creationTailCurrent(ctx, *choice.Creation, choice.Slot.page, status)) {
			return nil, nil, errStorageEnrollmentUnresolved
		}
		if choice.Identity != nil && !c.owner.identityReceiptCurrent(ctx, *choice.Identity, choice.Slot.page, durable, status) {
			digest := choice.Identity.PlanDigest
			matched := false
			for _, row := range lineage.Rows.LineagePlans {
				d, e := storageOwnerV5PlanDigest(row)
				if e != nil {
					return nil, nil, e
				}
				if d != digest {
					continue
				}
				if matched || plan != nil && plan.PlanID != row.PlanID {
					return nil, nil, errStorageEnrollmentUnresolved
				}
				copy := row
				plan, matched = &copy, true
			}
			if !matched {
				return nil, nil, errStorageEnrollmentUnresolved
			}
		}
		if choice.Creation != nil && choice.Creation.Kind == storageCreationLocalPrivate && !c.owner.creationTailCurrent(ctx, *choice.Creation, choice.Slot.page, status) {
			if local != nil {
				return nil, nil, errStorageEnrollmentUnresolved
			}
			copy := *choice.Creation
			local = &copy
		}
	}
	if plan != nil && local != nil {
		return nil, nil, errStorageEnrollmentUnresolved
	}
	return plan, local, c.current(ctx, true)
}

func (p *storageEnrollmentEpoch) selectStartupOriginalRequester(ctx context.Context) error {
	if p == nil || p.self != p || !p.startup || p.requester != nil || p.startupProject != nil || p.use != nil || p.project != nil {
		return storagebudget.ErrAuthority
	}
	plan, local, err := p.c.pendingStartupOriginal(ctx)
	if err != nil {
		return err
	}
	if plan == nil && local == nil || p.c.server.home == nil {
		return p.selectOriginalRequester(ctx, p.c.server)
	}
	if err = p.c.sources.currentProjects(ctx); err != nil {
		return err
	}
	graph := p.c.ref.cell.ownerCatalog
	var private *storageRootBinding
	if plan != nil {
		private = p.c.owner.roots[plan.PrivateRoot.ID]
		if private == nil || private.row.Generation != plan.PrivateRoot.Generation || private.row.Identity != plan.PrivateRoot.Identity || private.source == nil || private.source.persistent != plan.PrivateObject || plan.ConfigObject != p.c.ref.cell.ownerRecord.Config {
			return errStorageCustody
		}
	} else {
		if len(local.Parents) == 0 || local.Parents[0].Role != 2 {
			return errStorageCustody
		}
		if err = p.c.owner.validateCreationReferencedComponents(*local); err != nil {
			return err
		}
		for _, parent := range local.Parents {
			if parent.Role != 2 && parent.Role != 3 || int(parent.Component) >= len(local.Core.Components) {
				return errStorageCustody
			}
			component := local.Core.Components[parent.Component]
			binding := p.c.owner.roots[component.Root]
			if binding == nil || binding.source == nil || binding.row.Generation != component.RootGeneration || binding.row.Identity != local.Core.RootIdentities[parent.Component] {
				return errStorageCustody
			}
			if parent.Role == 2 && parent.Native != binding.source.persistent {
				return errStorageCustody
			}
			if private != nil && private != binding {
				return errStorageCustody
			}
			private = binding
		}
	}
	if private == nil || private.source == nil || private.category != storagebudget.Control || private.source.role != 3 && private.source.role != 6 || len(private.source.witnesses) == 0 {
		return errStorageCustody
	}
	edge := private.source.witnesses[len(private.source.witnesses)-1]
	if edge.name != startupIdentityDir || edge.root != private.root {
		return errStorageCustody
	}
	if err = private.current(ctx); err != nil {
		return err
	}
	for _, original := range p.c.sources.projects {
		if original.entry.Kind != "project" {
			continue
		}
		// The exact retained private witness must descend from this held original
		// registry project. Neither a path nor matching identity bytes suffices.
		a, e := edge.parent.Stat(".")
		b, e2 := original.named.root.Stat(".")
		if e != nil || e2 != nil {
			return errors.Join(errStorageCustody, e, e2)
		}
		if !os.SameFile(a, b) {
			continue
		}
		native, e := storageOwnerPersistentRoot(ctx, original.named.root)
		if e != nil {
			return e
		}
		path := filepath.Join(p.c.server.home.Path, original.entry.Folder)
		if private.source.path != filepath.Join(path, startupIdentityDir) || private.source.key != original.entry.ID {
			return errStorageCustody
		}
		if plan != nil && (plan.ProjectObject != native || string(plan.AccountingOwnerKey) != original.entry.RunnerWorkspace || string(plan.HistoryIdentity) != original.entry.HistoryIdentity) {
			return errStorageCustody
		}
		if local != nil && local.SelectedPath != dialectDraftStoragePath(path) {
			return errStorageCustody
		}
		matched := false
		for _, workspace := range graph.Workspaces {
			if string(workspace.OwnerKey) != original.entry.RunnerWorkspace || string(workspace.HistoryIdentity) != original.entry.HistoryIdentity || workspace.OwnerKind != 3 {
				continue
			}
			if matched || plan != nil && (plan.Workspace.ID != workspace.Definition.ID || plan.Workspace.Generation != workspace.Definition.Generation) {
				return errStorageCustody
			}
			referenced := false
			for _, ref := range private.references {
				referenced = referenced || ref == workspace.Definition.ID
			}
			if !referenced {
				return errStorageCustody
			}
			matched = true
		}
		if !matched || p.startupProject != nil {
			return errStorageCustody
		}
		copy := original
		p.startupProject = &copy
	}
	if p.startupProject == nil {
		return errStorageCustody
	}
	return p.startupProjectCurrent(ctx)
}

// The legacy receipt predicate stays unchanged. An already durable aborted
// identity page may additionally cross an explained policy-only epoch, but only
// after the original owner proves its actual selected page and current full Q.
func (o *storageBudgetOwner) identityReceiptCurrent(ctx context.Context, r storageIdentityIntent, page []byte, lineage storageCompositionLineage, status storagebudget.HostState) bool {
	return o.identityReceiptCurrentOriginal(ctx, r, page, lineage, status, nil)
}

// original is invocation-local comparison data supplied only after the control
// gate validates both actual images. Mappings and current Q checks remain at
// each original call site; this does not grant a permit or reconstruct a page.
func (o *storageBudgetOwner) identityReceiptCurrentOriginal(ctx context.Context, r storageIdentityIntent, page []byte, lineage storageCompositionLineage, status storagebudget.HostState, original *storagePolicyOriginal) bool {
	if original != nil && ctx.Err() != nil {
		return false
	}
	if r.Action == storageIdentityIntentMove && r.Core.Outcome == storageIntentPublished {
		if original == nil {
			state, err := o.readOriginalPolicy(ctx)
			if err != nil || state.RecoveryOnly {
				return false
			}
			return storageMoveReceiptBound(r, page, lineage, state.Moves, status)
		}
		return !original.RecoveryOnly && storageMoveReceiptBound(r, page, lineage, original.Moves, status)
	}
	if storageIdentityReceiptBound(r, page, lineage, status) {
		return true
	}
	if r.validate() != nil || r.Core.Phase != storageIntentRetired || r.Core.Outcome != storageIntentAborted || r.NativePhase != storageIdentityAbortObserved || r.Core.RetirementEpoch > status.Epoch || !o.policyCompletedHostOriginal(ctx, storageIntentSlot{record: r.Core, page: page}, status, original) {
		return false
	}
	var plan *storageOwnerV5LineagePlan
	for i := range lineage.Rows.LineagePlans {
		p := &lineage.Rows.LineagePlans[i]
		d, e := storageOwnerV5PlanDigest(*p)
		if e == nil && d == r.PlanDigest {
			if plan != nil {
				return false
			}
			plan = p
		}
	}
	if plan == nil {
		return false
	}
	t := status.Slots[r.Core.Token.Slot].Ticket
	return t.Token != r.Core.Token || storageIdentityIntentTicketMapping(r, t, plan.QBinding)
}
