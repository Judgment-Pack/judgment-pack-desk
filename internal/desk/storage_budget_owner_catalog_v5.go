package desk

// Catalog v5 is detached comparison data. None of these values opens Q,
// resolves a path, constructs custody, selects a live owner or grants an effect.
// M1, M3 and CC-2 must be resolved before any producer/recovery activation.
import (
	"bytes"
	"encoding/json"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
	"math"
	"reflect"
	"sort"
	"time"
)

const (
	storageOwnerV5Enrollment        uint8 = 1
	storageOwnerV5Plan              uint8 = 2
	storageOwnerV5Completion        uint8 = 3
	storageOwnerV5Continuation      uint8 = 4
	storageOwnerV5StampCompletion   uint8 = 6
	storageOwnerV5Moved             uint8 = 1
	storageOwnerV5BothPresent       uint8 = 2
	storageOwnerV5SourceAbsent      uint8 = 3
	storageOwnerV5Rows                    = 1024
	storageOwnerV5PlanBytes               = 4096
	storageOwnerV5CompletionBytes         = 2048
	storageOwnerV5ContinuationBytes       = 8192
	storageOwnerV5StampBytes              = 8192
)

// The original field order is retained. The new fields are appended; none is
// included in the existing definition/source/root identity material.
type storageOwnerV5Catalog struct {
	Version                 uint32
	Installation            storagebudget.ID
	QBinding                storagebudget.Digest
	Sequence                uint64
	Phase                   string
	Transition              storagebudget.ID
	PreviousCatalogDigest   storagebudget.Digest
	BeforeDefinitionsDigest storagebudget.Digest
	AfterDefinitionsDigest  storagebudget.Digest
	ExpectedQRevision       uint64
	Policy                  storageOwnerPolicy
	BeforeDefinitionIDs     storageOwnerDefinitionIDs
	OriginalOwner           storageOwnerSource
	Objects                 []storageOwnerObject
	OwnerControls           []storageOwnerControl
	Roots                   []storageOwnerRoot
	Workspaces              []storageOwnerWorkspace
	Filesystems             []storageOwnerFilesystem
	Constraints             []storageOwnerConstraint
	Profiles                []storageOwnerProfile
	TransitionKind          uint8
	TransitionPlan          storagebudget.ID
	UpgradeOrigin           storageOwnerV5UpgradeOrigin
	LineagePlans            []storageOwnerV5LineagePlan
	LineageCompletions      []storageOwnerV5LineageCompletion
	LineageContinuations    []storageOwnerV5FromContinuation
	LineageStampCompletions []storageOwnerV5StampCompletionRow
}

type storageOwnerV5UpgradeOrigin struct {
	Version             uint32
	Sequence            uint64
	FrameDigest         storagebudget.Digest
	OldProjectionDigest storagebudget.Digest
}

type storageOwnerV5Workspace struct {
	ID         storagebudget.ID
	Generation uint64
	Digest     storagebudget.Digest
}
type storageOwnerV5PrivateRoot struct {
	ID           storagebudget.ID
	Generation   uint64
	Identity     storagebudget.Digest
	SourceDigest storagebudget.Digest
}
type storageOwnerV5LineagePlan struct {
	PlanID                 storagebudget.ID
	Installation           storagebudget.ID
	QBinding               storagebudget.Digest
	Workspace              storageOwnerV5Workspace
	PrivateRoot            storageOwnerV5PrivateRoot
	ProjectObject          storageOwnerPersistentObject
	PrivateObject          storageOwnerPersistentObject
	ConfigObject           storageOwnerPersistentObject
	ProviderRelationDigest storagebudget.Digest
	HistoryIdentity        []byte
	AccountingOwnerKey     []byte
	IdentityInitial        []byte
	IdentityFromCleared    []byte
	CompletionRoom         uint32
	ContinuationRoom       uint32
	StampRoom              uint32
}
type storageOwnerV5Receipt struct {
	OperationID           storagebudget.ID
	Terminal              storagebudget.Token
	PersistedApplied      storagebudget.Token
	PersistedNextSequence uint64
	RetirementEpoch       uint64
	IntentDigest          storagebudget.Digest
	TicketDigest          storagebudget.Digest
	PublishedObject       storageOwnerPersistentObject
	PublishedBytesDigest  storagebudget.Digest
	PublicationEvidence   storagebudget.Digest
}
type storageOwnerV5LineageCompletion struct {
	PlanID     storagebudget.ID
	PlanDigest storagebudget.Digest
	Initial    storageOwnerV5Receipt
}
type storageOwnerV5FromContinuation struct {
	PlanID            storagebudget.ID
	PlanDigest        storagebudget.Digest
	StampDisposition  uint8
	StampSourceBefore storageOwnerPersistentObject
	StampTargetBefore storageOwnerPersistentObject
	StampSourceAfter  storageOwnerPersistentObject
	StampTargetAfter  storageOwnerPersistentObject
	StampReceipt      storageOwnerV5Receipt
	Rewrite           storageOwnerV5Receipt
}

// S's declaration order is its exact canonical digest/wire DTO. It excludes
// clear receipt, clocks, paths, annotations and its own digest.
type storageOwnerV5StampCompletionRow struct {
	PlanID            storagebudget.ID
	PlanDigest        storagebudget.Digest
	StampDisposition  uint8
	StampSourceBefore storageOwnerPersistentObject
	StampTargetBefore storageOwnerPersistentObject
	StampSourceAfter  storageOwnerPersistentObject
	StampTargetAfter  storageOwnerPersistentObject
	StampReceipt      storageOwnerV5Receipt
}

func (c storageOwnerV5Catalog) old() storageOwnerCatalogV2 {
	return storageOwnerCatalogV2{Version: 3, Installation: c.Installation, QBinding: c.QBinding, Sequence: c.Sequence, Phase: c.Phase, Transition: c.Transition, PreviousCatalogDigest: c.PreviousCatalogDigest, BeforeDefinitionsDigest: c.BeforeDefinitionsDigest, AfterDefinitionsDigest: c.AfterDefinitionsDigest, ExpectedQRevision: c.ExpectedQRevision, Policy: c.Policy, BeforeDefinitionIDs: c.BeforeDefinitionIDs, OriginalOwner: c.OriginalOwner, Objects: c.Objects, OwnerControls: c.OwnerControls, Roots: c.Roots, Workspaces: c.Workspaces, Filesystems: c.Filesystems, Constraints: c.Constraints, Profiles: c.Profiles}
}

// V6 retains a canonical complete V3/V4/V5 source payload. Policy edits never
// rewrite it: workspace admission Limits are source/provider/lineage material.
// Overrides are current sticky values, not history and not default inheritance.
// This detached core has no startup, serving, recovery or native grant wiring.
type storageOwnerV6Override struct {
	ID         storagebudget.ID
	Generation uint64
	Limits     storagebudget.Limits
}

const (
	storageOwnerV6PolicyTransition uint8 = 1
	storageOwnerV6SourceTransition uint8 = 2
)

type storageOwnerV6Catalog struct {
	Version               uint32
	Sequence              uint64
	TransitionKind        uint8
	Phase                 string // prepared, active, aborted (fenced), aborted-unfenced
	Transition            storagebudget.ID
	PreviousCatalogDigest storagebudget.Digest
	BeforeEffectiveDigest storagebudget.Digest
	AfterEffectiveDigest  storagebudget.Digest
	Source                json.RawMessage
	EffectivePolicy       storageOwnerPolicy
	Overrides             []storageOwnerV6Override
	PolicyTransition      []byte
}

func storageOwnerV6Policy(p storageOwnerPolicy) storagebudget.Policy {
	return storagebudget.Policy{Installation: p.Installation, Workspace: p.Workspace, ObservationMaxAge: time.Duration(p.ObservationMaxAgeNanos)}
}
func storageOwnerV6HostPolicy(p storagebudget.Policy) storageOwnerPolicy {
	return storageOwnerPolicy{Installation: p.Installation, Workspace: p.Workspace, ObservationMaxAgeNanos: uint64(p.ObservationMaxAge)}
}

// sourceDefinitions owns all returned definition slices. It has deliberately no
// effective-policy fallback: callers comparing actual Q must expand separately.
func (c storageOwnerV6Catalog) sourceDefinitions(r storageOwnerRecordV2, cl storageOwnerClaimV2) (storageOwnerDefinitions, error) {
	s, err := storageOwnerV6Source(c.Source, r, cl)
	if err != nil {
		return storageOwnerDefinitions{}, err
	}
	_, d, err := s.definitions()
	return d, err
}

func storageOwnerV6Expand(source storageOwnerDefinitions, policy storageOwnerPolicy, overrides []storageOwnerV6Override) (storageOwnerDefinitions, error) {
	if !policy.valid() || policy.ObservationMaxAgeNanos != source.Policy.ObservationMaxAgeNanos || overrides == nil || len(overrides) > storagebudget.MaxWorkspaces {
		return storageOwnerDefinitions{}, storagebudget.ErrBounds
	}
	d := storageOwnerDefinitions{policy, append([]storagebudget.RegisteredRoot{}, source.Roots...), append([]storagebudget.RegisteredWorkspace{}, source.Workspaces...), append([]storagebudget.Filesystem{}, source.Filesystems...), append([]storagebudget.CapacityConstraint{}, source.Constraints...)}
	for i, row := range overrides {
		if row.ID == (storagebudget.ID{}) || row.Generation == 0 || !storageOwnerLimitsValid(row.Limits) || i > 0 && bytes.Compare(overrides[i-1].ID[:], row.ID[:]) >= 0 {
			return storageOwnerDefinitions{}, storagebudget.ErrBounds
		}
		found := false
		for j, w := range source.Workspaces {
			if w.ID != row.ID {
				continue
			}
			if w.Generation != row.Generation || w.Limits == row.Limits {
				return storageOwnerDefinitions{}, storagebudget.ErrBounds
			}
			d.Workspaces[j].Limits = row.Limits
			found = true
			break
		}
		if !found {
			return storageOwnerDefinitions{}, storagebudget.ErrBounds
		}
	}
	return d, nil
}
func (c storageOwnerV6Catalog) effectiveDefinitions(r storageOwnerRecordV2, cl storageOwnerClaimV2) (storageOwnerDefinitions, error) {
	source, err := c.sourceDefinitions(r, cl)
	if err != nil {
		return storageOwnerDefinitions{}, err
	}
	return storageOwnerV6Expand(source, c.EffectivePolicy, c.Overrides)
}

// CanonicalSet replaces one current value. Returning to admission removes its
// sparse row; absence still denotes that explicit sticky baseline, not default.
func storageOwnerV6CanonicalSet(source storageOwnerDefinitions, rows []storageOwnerV6Override, id storagebudget.ID, generation uint64, limits storagebudget.Limits) ([]storageOwnerV6Override, error) {
	if _, err := storageOwnerV6Expand(source, source.Policy, rows); err != nil {
		return nil, err
	}
	var baseline storagebudget.Limits
	found := false
	for _, w := range source.Workspaces {
		if w.ID == id && w.Generation == generation {
			baseline = w.Limits
			found = true
			break
		}
	}
	if !found || !storageOwnerLimitsValid(limits) {
		return nil, storagebudget.ErrBounds
	}
	out := make([]storageOwnerV6Override, 0, len(rows)+1)
	for _, row := range rows {
		if row.ID != id {
			out = append(out, row)
		}
	}
	if limits != baseline {
		out = append(out, storageOwnerV6Override{id, generation, limits})
	}
	sort.Slice(out, func(i, j int) bool { return bytes.Compare(out[i].ID[:], out[j].ID[:]) < 0 })
	return out, nil
}

func storageOwnerV6EffectiveDigest(d storageOwnerDefinitions) (storagebudget.Digest, error) {
	return storageOwnerDigest("desk-owner-effective-definitions-v6", d)
}

// validatePolicyRelation inverts the sole declared effective workspace change.
// Source material and every other effective row remain byte-for-byte values.
// aborted means a fenced Q1 abort; aborted-unfenced means no Q fence was
// taken. The original host must match the distinct expected Q epoch. Raw Q
// matching remains a separate original locked operation.
func (c storageOwnerV6Catalog) validatePolicyRelation(r storageOwnerRecordV2, cl storageOwnerClaimV2) error {
	if c.TransitionKind != storageOwnerV6PolicyTransition || c.Version != 6 || c.Sequence == 0 || c.Phase == "prepared" && c.Sequence == math.MaxUint64 || c.Transition == (storagebudget.ID{}) || c.PreviousCatalogDigest == (storagebudget.Digest{}) || c.Phase != "prepared" && c.Phase != "active" && c.Phase != "aborted" && c.Phase != "aborted-unfenced" {
		return storagebudget.ErrBounds
	}
	source, err := c.sourceDefinitions(r, cl)
	if err != nil {
		return err
	}
	return c.validatePolicyDefinitions(source, r)
}

// Shared pure delta validation after either strict legacy decoding or the
// separately context-bound packed source comparison. It grants no effect.
func (c storageOwnerV6Catalog) validatePolicyDefinitions(source storageOwnerDefinitions, r storageOwnerRecordV2) error {
	if c.TransitionKind != storageOwnerV6PolicyTransition || c.Version != 6 || c.Sequence == 0 || c.Phase == "prepared" && c.Sequence == math.MaxUint64 || c.Transition == (storagebudget.ID{}) || c.PreviousCatalogDigest == (storagebudget.Digest{}) || c.Phase != "prepared" && c.Phase != "active" && c.Phase != "aborted" && c.Phase != "aborted-unfenced" {
		return storagebudget.ErrBounds
	}
	p, err := storagebudget.DecodePolicyTransition(c.PolicyTransition)
	if err != nil {
		return err
	}
	if p.Original.Installation != r.Installation || p.Original.Binding != r.QBinding {
		return storagebudget.ErrBounds
	}
	current, err := storageOwnerV6Expand(source, c.EffectivePolicy, c.Overrides)
	if err != nil {
		return err
	}
	beforeRows := append([]storageOwnerV6Override{}, c.Overrides...)
	if c.Phase == "aborted" || c.Phase == "aborted-unfenced" {
		if storageOwnerV6Policy(current.Policy) != p.Before {
			return storagebudget.ErrBounds
		}
		if p.Workspace != (storagebudget.ID{}) {
			for _, w := range current.Workspaces {
				if w.ID == p.Workspace && (w.Generation != p.WorkspaceGeneration || w.Limits != p.BeforeLimits) {
					return storagebudget.ErrBounds
				}
			}
		}
	} else {
		if storageOwnerV6Policy(current.Policy) != p.After {
			return storagebudget.ErrBounds
		}
		if p.Workspace != (storagebudget.ID{}) {
			found := false
			for _, w := range current.Workspaces {
				if w.ID == p.Workspace {
					if w.Generation != p.WorkspaceGeneration || w.Limits != p.AfterLimits {
						return storagebudget.ErrBounds
					}
					found = true
					break
				}
			}
			if !found {
				return storagebudget.ErrBounds
			}
			beforeRows, err = storageOwnerV6CanonicalSet(source, beforeRows, p.Workspace, p.WorkspaceGeneration, p.BeforeLimits)
			if err != nil {
				return err
			}
		}
	}
	before, err := storageOwnerV6Expand(source, storageOwnerV6HostPolicy(p.Before), beforeRows)
	if err != nil {
		return err
	}
	afterRows := beforeRows
	if p.Workspace != (storagebudget.ID{}) {
		afterRows, err = storageOwnerV6CanonicalSet(source, beforeRows, p.Workspace, p.WorkspaceGeneration, p.AfterLimits)
		if err != nil {
			return err
		}
	}
	after, err := storageOwnerV6Expand(source, storageOwnerV6HostPolicy(p.After), afterRows)
	if err != nil {
		return err
	}
	bd, err := storageOwnerV6EffectiveDigest(before)
	if err != nil {
		return err
	}
	ad, err := storageOwnerV6EffectiveDigest(after)
	if err != nil {
		return err
	}
	if bd != c.BeforeEffectiveDigest || ad != c.AfterEffectiveDigest {
		return storagebudget.ErrBounds
	}
	return nil
}

// Follows compares a proposed Prepared with the actual complete current V6
// predecessor. A self-consistent delta alone never selects the prior catalog.
func storageOwnerV6Follows(previous, prepared storageOwnerV6Catalog, r storageOwnerRecordV2, cl storageOwnerClaimV2) error {
	if err := previous.validateV6(r, cl); err != nil {
		return err
	}
	if err := prepared.validateV6(r, cl); err != nil {
		return err
	}
	if prepared.TransitionKind == storageOwnerV6SourceTransition {
		return storageOwnerV6SourceFollows(previous, prepared, r, cl)
	}
	if previous.Phase != "active" && previous.Phase != "aborted" && previous.Phase != "aborted-unfenced" || prepared.Phase != "prepared" || previous.Sequence == math.MaxUint64 || prepared.Sequence != previous.Sequence+1 || prepared.Transition == previous.Transition || !bytes.Equal(previous.Source, prepared.Source) {
		return storagebudget.ErrBounds
	}
	d, err := storageOwnerV6Digest(previous)
	if err != nil {
		return err
	}
	old, err := previous.effectiveDefinitions(r, cl)
	if err != nil {
		return err
	}
	od, err := storageOwnerV6EffectiveDigest(old)
	if err != nil {
		return err
	}
	if prepared.PreviousCatalogDigest != d || prepared.BeforeEffectiveDigest != od {
		return storagebudget.ErrBounds
	}
	return nil
}

// Starts binds the first Prepared to an actual validated original source frame.
// Legacy frame decoding stays version-specific. This helper is detached and is
// not wired into ordinary startup: serving an unhandled V6 prefix remains refused.
func storageOwnerV6Starts(sourceFrame []byte, prepared storageOwnerV6Catalog, r storageOwnerRecordV2, cl storageOwnerClaimV2) error {
	if err := prepared.validateV6(r, cl); err != nil {
		return err
	}
	var raw []byte
	var seq uint64
	var err error
	switch {
	case len(sourceFrame) >= 8 && bytes.Equal(sourceFrame[:8], storageOwnerV5Magic[:]):
		raw, seq, err = storageOwnerV5Payload(sourceFrame)
	case len(sourceFrame) >= 8 && bytes.Equal(sourceFrame[:8], storageOwnerV4Magic[:]):
		raw, seq, err = storageOwnerV4Payload(sourceFrame)
	default:
		// The unchanged V3 decoder validates both its distinct frame and payload.
		v, e := storageOwnerDecodeCatalogV3(sourceFrame, r, cl)
		if e != nil {
			return e
		}
		old := storageOwnerCatalogV2(v)
		raw, err = json.Marshal(old)
		seq = old.Sequence
	}
	if err != nil {
		return err
	}
	source, err := storageOwnerV6Source(raw, r, cl)
	if err != nil {
		return err
	}
	if prepared.TransitionKind != storageOwnerV6PolicyTransition || source.Phase != "active" || prepared.Phase != "prepared" || seq == math.MaxUint64 || prepared.Sequence != seq+1 || prepared.Transition == source.Transition || !bytes.Equal(raw, prepared.Source) || !bytes.Equal(prepared.PreviousCatalogDigest[:], sourceFrame[32:64]) {
		return storagebudget.ErrBounds
	}
	_, old, err := source.definitions()
	if err != nil {
		return err
	}
	d, err := storageOwnerV6EffectiveDigest(old)
	if err != nil {
		return err
	}
	if d != prepared.BeforeEffectiveDigest {
		return storagebudget.ErrBounds
	}
	return nil
}

// Publication validates the one final catalog write. Committed keeps the new
// effective values; aborted restores exactly the declared old effective values.
// It does not decide whether Q0/Q1 permits abort or whether Q2 permits commit.
func storageOwnerV6Publication(prepared, final storageOwnerV6Catalog, r storageOwnerRecordV2, cl storageOwnerClaimV2) error {
	if err := prepared.validateV6(r, cl); err != nil {
		return err
	}
	if err := final.validateV6(r, cl); err != nil {
		return err
	}
	if prepared.TransitionKind == storageOwnerV6SourceTransition {
		return storageOwnerV6SourcePublication(prepared, final, r, cl)
	}
	if prepared.Phase != "prepared" || final.Phase != "active" && final.Phase != "aborted" && final.Phase != "aborted-unfenced" || prepared.Sequence == math.MaxUint64 {
		return storagebudget.ErrBounds
	}
	expected := prepared
	expected.Sequence++
	expected.Phase = final.Phase
	d, err := storageOwnerV6Digest(prepared)
	if err != nil {
		return err
	}
	expected.PreviousCatalogDigest = d
	if final.Phase == "aborted" || final.Phase == "aborted-unfenced" {
		p, err := storagebudget.DecodePolicyTransition(prepared.PolicyTransition)
		if err != nil {
			return err
		}
		expected.EffectivePolicy = storageOwnerV6HostPolicy(p.Before)
		expected.Overrides = append([]storageOwnerV6Override{}, prepared.Overrides...)
		if p.Workspace != (storagebudget.ID{}) {
			source, err := prepared.sourceDefinitions(r, cl)
			if err != nil {
				return err
			}
			expected.Overrides, err = storageOwnerV6CanonicalSet(source, expected.Overrides, p.Workspace, p.WorkspaceGeneration, p.BeforeLimits)
			if err != nil {
				return err
			}
		}
	}
	a, err := json.Marshal(expected)
	if err != nil {
		return err
	}
	b, err := json.Marshal(final)
	if err != nil {
		return err
	}
	if !bytes.Equal(a, b) {
		return storagebudget.ErrBounds
	}
	return nil
}

func (c storageOwnerV6Catalog) validateV6(r storageOwnerRecordV2, cl storageOwnerClaimV2) error {
	switch c.TransitionKind {
	case storageOwnerV6PolicyTransition:
		return c.validatePolicyRelation(r, cl)
	case storageOwnerV6SourceTransition:
		return c.validateSourceRelation(r, cl)
	default:
		return storagebudget.ErrBounds
	}
}

// Source transitions carry no policy inverse at all. Immutable source Policy
// remains its original admission material; only the separate current projection
// supplies actual-Q policy and sticky workspace values.
func (c storageOwnerV6Catalog) validateSourceRelation(r storageOwnerRecordV2, cl storageOwnerClaimV2) error {
	if c.Version != 6 || c.TransitionKind != storageOwnerV6SourceTransition || c.Sequence == 0 || c.Phase == "prepared" && c.Sequence == math.MaxUint64 || c.Phase != "prepared" && c.Phase != "active" || c.Transition == (storagebudget.ID{}) || c.PreviousCatalogDigest == (storagebudget.Digest{}) || c.PolicyTransition == nil || len(c.PolicyTransition) != 0 {
		return storagebudget.ErrBounds
	}
	source, err := storageOwnerV6Source(c.Source, r, cl)
	if err != nil {
		return err
	}
	if source.Phase != c.Phase || source.Transition != c.Transition {
		return storagebudget.ErrBounds
	}
	before, after, err := source.definitions()
	if err != nil {
		return err
	}
	return c.validateSourceDefinitions(before, after)
}

func (c storageOwnerV6Catalog) validateSourceDefinitions(before, after storageOwnerDefinitions) error {
	var err error
	retained := map[storagebudget.ID]bool{}
	for _, w := range before.Workspaces {
		retained[w.ID] = true
	}
	for _, w := range after.Workspaces {
		if !retained[w.ID] && w.Limits != c.EffectivePolicy.Workspace {
			return storagebudget.ErrBounds
		}
	}
	for _, row := range c.Overrides {
		if !retained[row.ID] {
			return storagebudget.ErrBounds
		}
	}
	before, err = storageOwnerV6Expand(before, c.EffectivePolicy, c.Overrides)
	if err != nil {
		return err
	}
	after, err = storageOwnerV6Expand(after, c.EffectivePolicy, c.Overrides)
	if err != nil {
		return err
	}
	bd, err := storageOwnerV6EffectiveDigest(before)
	if err != nil {
		return err
	}
	ad, err := storageOwnerV6EffectiveDigest(after)
	if err != nil {
		return err
	}
	if bd != c.BeforeEffectiveDigest || ad != c.AfterEffectiveDigest {
		return storagebudget.ErrBounds
	}
	return nil
}

// SourceFollows retains all exact effective overrides while delegating every
// immutable enrollment/lineage join to the existing version-specific relation.
func storageOwnerV6SourceFollows(previous, prepared storageOwnerV6Catalog, r storageOwnerRecordV2, cl storageOwnerClaimV2) error {
	if previous.Phase != "active" && previous.Phase != "aborted" && previous.Phase != "aborted-unfenced" || prepared.Phase != "prepared" || previous.Sequence == math.MaxUint64 || prepared.Sequence != previous.Sequence+1 || previous.Transition == prepared.Transition || previous.EffectivePolicy != prepared.EffectivePolicy || !reflect.DeepEqual(previous.Overrides, prepared.Overrides) {
		return storagebudget.ErrBounds
	}
	d, err := storageOwnerV6Digest(previous)
	if err != nil {
		return err
	}
	if prepared.PreviousCatalogDigest != d {
		return storagebudget.ErrBounds
	}
	old, err := previous.effectiveDefinitions(r, cl)
	if err != nil {
		return err
	}
	od, err := storageOwnerV6EffectiveDigest(old)
	if err != nil {
		return err
	}
	if od != prepared.BeforeEffectiveDigest {
		return storagebudget.ErrBounds
	}
	return storageOwnerV6SourceAdjacent(previous.Source, prepared.Source, r, cl)
}

func storageOwnerV6SourcePublication(prepared, final storageOwnerV6Catalog, r storageOwnerRecordV2, cl storageOwnerClaimV2) error {
	if prepared.Phase != "prepared" || final.Phase != "active" || prepared.Sequence == math.MaxUint64 || final.Sequence != prepared.Sequence+1 {
		return storagebudget.ErrBounds
	}
	expected := prepared
	expected.Sequence++
	expected.Phase = "active"
	d, err := storageOwnerV6Digest(prepared)
	if err != nil {
		return err
	}
	expected.PreviousCatalogDigest = d
	expected.Source = final.Source
	a, err := json.Marshal(expected)
	if err != nil {
		return err
	}
	b, err := json.Marshal(final)
	if err != nil {
		return err
	}
	if !bytes.Equal(a, b) {
		return storagebudget.ErrBounds
	}
	return storageOwnerV6SourceAdjacent(prepared.Source, final.Source, r, cl)
}
