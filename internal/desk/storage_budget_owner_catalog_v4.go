package desk

// Catalog v4 is detached comparison data. None of these values opens Q,
// resolves a path, constructs custody, selects a live owner or grants an effect.
// M1, M3 and CC-2 must be resolved before any producer/recovery activation.
import "github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"

const (
	storageOwnerV4Enrollment        uint8 = 1
	storageOwnerV4Plan              uint8 = 2
	storageOwnerV4Completion        uint8 = 3
	storageOwnerV4Continuation      uint8 = 4
	storageOwnerV4BridgePlan        uint8 = 5
	storageOwnerV4Moved             uint8 = 1
	storageOwnerV4BothPresent       uint8 = 2
	storageOwnerV4SourceAbsent      uint8 = 3
	storageOwnerV4Rows                    = 1024
	storageOwnerV4PlanBytes               = 4096
	storageOwnerV4CompletionBytes         = 2048
	storageOwnerV4ContinuationBytes       = 8192
)

// The original field order is retained. The new fields are appended; none is
// included in the existing definition/source/root identity material.
type storageOwnerV4Catalog struct {
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
	UpgradeOrigin           storageOwnerV4UpgradeOrigin
	LineagePlans            []storageOwnerV4LineagePlan
	LineageCompletions      []storageOwnerV4LineageCompletion
	LineageContinuations    []storageOwnerV4FromContinuation
}

type storageOwnerV4UpgradeOrigin struct {
	Version             uint32
	Sequence            uint64
	FrameDigest         storagebudget.Digest
	OldProjectionDigest storagebudget.Digest
}

type storageOwnerV4Workspace struct {
	ID         storagebudget.ID
	Generation uint64
	Digest     storagebudget.Digest
}
type storageOwnerV4PrivateRoot struct {
	ID           storagebudget.ID
	Generation   uint64
	Identity     storagebudget.Digest
	SourceDigest storagebudget.Digest
}
type storageOwnerV4LineagePlan struct {
	PlanID                 storagebudget.ID
	Installation           storagebudget.ID
	QBinding               storagebudget.Digest
	Workspace              storageOwnerV4Workspace
	PrivateRoot            storageOwnerV4PrivateRoot
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
}
type storageOwnerV4Receipt struct {
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
type storageOwnerV4LineageCompletion struct {
	PlanID     storagebudget.ID
	PlanDigest storagebudget.Digest
	Initial    storageOwnerV4Receipt
}
type storageOwnerV4FromContinuation struct {
	PlanID            storagebudget.ID
	PlanDigest        storagebudget.Digest
	StampDisposition  uint8
	StampSourceBefore storageOwnerPersistentObject
	StampTargetBefore storageOwnerPersistentObject
	StampSourceAfter  storageOwnerPersistentObject
	StampTargetAfter  storageOwnerPersistentObject
	StampReceipt      storageOwnerV4Receipt
	Rewrite           storageOwnerV4Receipt
}

func (c storageOwnerV4Catalog) old() storageOwnerCatalogV2 {
	return storageOwnerCatalogV2{Version: 3, Installation: c.Installation, QBinding: c.QBinding, Sequence: c.Sequence, Phase: c.Phase, Transition: c.Transition, PreviousCatalogDigest: c.PreviousCatalogDigest, BeforeDefinitionsDigest: c.BeforeDefinitionsDigest, AfterDefinitionsDigest: c.AfterDefinitionsDigest, ExpectedQRevision: c.ExpectedQRevision, Policy: c.Policy, BeforeDefinitionIDs: c.BeforeDefinitionIDs, OriginalOwner: c.OriginalOwner, Objects: c.Objects, OwnerControls: c.OwnerControls, Roots: c.Roots, Workspaces: c.Workspaces, Filesystems: c.Filesystems, Constraints: c.Constraints, Profiles: c.Profiles}
}
