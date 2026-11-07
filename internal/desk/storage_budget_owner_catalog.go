package desk

// This file contains detached comparison data only. No method opens a path,
// constructs an owner, qualifies a profile, or grants write/recovery authority.
import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"math"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

const (
	storageOwnerCatalogBytes        = 8 << 20
	storageOwnerNoObject     uint32 = math.MaxUint32
	storageOwnerNoControl    uint32 = math.MaxUint32
)

var storageOwnerCatalogMagic = [8]byte{'D', 'S', 'K', 'O', 'W', 'N', '2', 0}
var storageOwnerCatalogMagicV3 = [8]byte{'D', 'S', 'K', 'O', 'W', 'N', '3', 0}

type storageOwnerCatalogV2 struct {
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
}

// V3 retains the bounded field shapes; its advisory semantics require an
// explicit V3 codec. A V2 decoder never accepts or reinterprets this payload.
type storageOwnerCatalogV3 storageOwnerCatalogV2

type storageOwnerPolicy struct {
	Installation           storagebudget.Limits
	Workspace              storagebudget.Limits
	ObservationMaxAgeNanos uint64
}
type storageOwnerDefinitionIDs struct {
	Roots       []storagebudget.ID
	Workspaces  []storagebudget.ID
	Filesystems []storagebudget.ID
	Constraints []storagebudget.ID
}
type storageOwnerSource struct {
	Kind            uint8
	ConfigPath      []byte
	HomeOwner       string
	HomePath        []byte
	HomeObject      storageOwnerPersistentObject
	RegistryControl uint32
	Entries         []storageOwnerHomeRelation
}
type storageOwnerHomeRelation struct {
	DeskID          string
	Folder          []byte
	Kind            uint8
	HistoryIdentity []byte
	StorageOwner    string
	RunnerWorkspace string
}
type storageOwnerObject struct {
	Role        uint8
	Source      uint8
	Parent      uint32
	Name        []byte
	Persistent  storageOwnerPersistentObject
	OwnerClass  uint8
	ModeClass   uint8
	FixedLength uint64
}
type storageOwnerControl struct {
	Role           uint8
	ParentObject   uint32
	Name           []byte
	OwnerKind      uint8
	OwnerKey       []byte
	Location       storageOwnerLocation
	RelationDigest storagebudget.Digest
	MaxBytes       uint32
}
type storageOwnerLocation struct {
	Version          uint32
	Path             []byte
	Previous         []byte
	Selected         storageOwnerPersistentObject
	PreviousSelected storageOwnerPersistentObject
}
type storageOwnerRoot struct {
	Definition      storagebudget.RegisteredRoot
	Role            uint8
	Category        uint8
	References      []storagebudget.ID
	Source          storageOwnerRootSource
	DirectoryObject uint32
	DomainObject    uint32
	WorkObject      uint32
	Membership      storageOwnerMembership
}
type storageOwnerRootSource struct {
	Kind             uint8
	OwnerKey         []byte
	Path             []byte
	ParentRoot       storagebudget.ID
	Relative         []byte
	ControlKind      uint8
	ControlIndex     uint32
	ControlSelection uint8
}
type storageOwnerMembership struct {
	Backend         uint8
	ProfileID       uint16
	ProfileVersion  uint16
	ProfileEvidence storagebudget.Digest
	Pool            storagebudget.ID
	VolumeUUID      [16]byte
	ContainerUUID   [16]byte
	User            storageOwnerSubject
	Group           storageOwnerSubject
	Project         storageOwnerSubject
	Creation        uint8
	ExistingObjects []uint32
}
type storageOwnerSubject struct {
	State        uint8
	Kind         uint8
	Subject      uint32
	NamespaceMap storagebudget.Digest
	Constraint   storagebudget.ID
}
type storageOwnerWorkspace struct {
	Definition      storagebudget.RegisteredWorkspace
	OwnerKind       uint8
	OwnerKey        []byte
	HistoryIdentity []byte
}
type storageOwnerFilesystem struct {
	Definition        storagebudget.Filesystem
	Backend           uint8
	VolumeUUID        [16]byte
	ContainerUUID     [16]byte
	PersistentProfile uint16
	InodeProfile      uint16
}
type storageOwnerConstraint struct {
	Definition    storagebudget.CapacityConstraint
	Kind          uint8
	Pool          storagebudget.ID
	VolumeUUID    [16]byte
	ContainerUUID [16]byte
	Subject       storageOwnerSubject
}
type storageOwnerProfile struct {
	ID                 uint16
	Version            uint16
	Backend            uint8
	Evidence           storagebudget.Digest
	PersistentEvidence storagebudget.Digest
	MappingEvidence    storagebudget.Digest
	QuotaEvidence      storagebudget.Digest
	InodeEvidence      storagebudget.Digest
	CounterEvidence    storagebudget.Digest
	AllowanceEvidence  storagebudget.Digest
}

func storageOwnerCatalogFrame(payload []byte, sequence uint64) ([]byte, error) {
	return storageOwnerCatalogFrameVersion(payload, sequence, 2)
}
func storageOwnerCatalogFrameVersion(payload []byte, sequence uint64, version uint32) ([]byte, error) {
	if version != 2 && version != 3 {
		return nil, storagebudget.ErrBounds
	}
	if sequence == 0 || len(payload) > storageOwnerCatalogBytes-storageOwnerHeaderBytes {
		return nil, storagebudget.ErrBounds
	}
	out := make([]byte, storageOwnerCatalogBytes)
	magic := storageOwnerCatalogMagic
	if version == 3 {
		magic = storageOwnerCatalogMagicV3
	}
	copy(out, magic[:])
	binary.LittleEndian.PutUint32(out[8:12], version)
	binary.LittleEndian.PutUint32(out[12:16], 64)
	binary.LittleEndian.PutUint64(out[16:24], sequence)
	binary.LittleEndian.PutUint32(out[24:28], uint32(len(payload)))
	copy(out[64:], payload)
	h := sha256.New()
	h.Write(out[:32])
	h.Write(payload)
	copy(out[32:64], h.Sum(nil))
	return out, nil
}
func storageOwnerCatalogPayload(raw []byte) ([]byte, uint64, error) {
	return storageOwnerCatalogPayloadVersion(raw, 2)
}
func storageOwnerCatalogPayloadVersion(raw []byte, version uint32) ([]byte, uint64, error) {
	magic := storageOwnerCatalogMagic
	if version == 3 {
		magic = storageOwnerCatalogMagicV3
	} else if version != 2 {
		return nil, 0, storagebudget.ErrBounds
	}
	if len(raw) != storageOwnerCatalogBytes || !bytes.Equal(raw[:8], magic[:]) || binary.LittleEndian.Uint32(raw[8:12]) != version || binary.LittleEndian.Uint32(raw[12:16]) != 64 || binary.LittleEndian.Uint32(raw[28:32]) != 0 {
		return nil, 0, storagebudget.ErrBounds
	}
	n := binary.LittleEndian.Uint32(raw[24:28])
	seq := binary.LittleEndian.Uint64(raw[16:24])
	if seq == 0 || n > storageOwnerCatalogBytes-64 {
		return nil, 0, storagebudget.ErrBounds
	}
	end := 64 + int(n)
	if !storageAllZero(raw[end:]) {
		return nil, 0, storagebudget.ErrBounds
	}
	h := sha256.New()
	h.Write(raw[:32])
	h.Write(raw[64:end])
	if !bytes.Equal(h.Sum(nil), raw[32:64]) {
		return nil, 0, storagebudget.ErrBounds
	}
	return raw[64:end], seq, nil
}

// Joins require the immutable record and claim, but those detached inputs do
// not substitute for actual original-owner, installed-profile or native proof.
func storageOwnerDecodeCatalogV2(raw []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageOwnerCatalogV2, error) {
	var c storageOwnerCatalogV2
	b, seq, err := storageOwnerCatalogPayload(raw)
	if err != nil {
		return c, err
	}
	if err = storageOwnerCatalogJSON(b, &c); err != nil {
		return storageOwnerCatalogV2{}, err
	}
	if c.Sequence != seq {
		return storageOwnerCatalogV2{}, storagebudget.ErrBounds
	}
	if err = c.validate(record, claim); err != nil {
		return storageOwnerCatalogV2{}, err
	}
	return c, nil
}
func storageOwnerEncodeCatalogV2(c storageOwnerCatalogV2, record storageOwnerRecordV2, claim storageOwnerClaimV2) ([]byte, error) {
	if err := c.validate(record, claim); err != nil {
		return nil, err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	// Apply exactly the decoder's pre-allocation/list/null contract to authored
	// values too. Encoding never sorts, fills absent lists or repairs digests.
	if !storageOwnerCatalogPreflight(b) {
		return nil, storagebudget.ErrBounds
	}
	return storageOwnerCatalogFrame(b, c.Sequence)
}

func storageOwnerDecodeCatalogV3(raw []byte, record storageOwnerRecordV2, claim storageOwnerClaimV2) (storageOwnerCatalogV3, error) {
	var c storageOwnerCatalogV2
	b, seq, err := storageOwnerCatalogPayloadVersion(raw, 3)
	if err != nil {
		return storageOwnerCatalogV3{}, err
	}
	if err = storageOwnerCatalogJSON(b, &c); err != nil {
		return storageOwnerCatalogV3{}, err
	}
	if c.Sequence != seq {
		return storageOwnerCatalogV3{}, storagebudget.ErrBounds
	}
	if err = c.validateVersion(record, claim, 3); err != nil {
		return storageOwnerCatalogV3{}, err
	}
	return storageOwnerCatalogV3(c), nil
}
func storageOwnerEncodeCatalogV3(value storageOwnerCatalogV3, record storageOwnerRecordV2, claim storageOwnerClaimV2) ([]byte, error) {
	c := storageOwnerCatalogV2(value)
	if err := c.validateVersion(record, claim, 3); err != nil {
		return nil, err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	if !storageOwnerCatalogPreflight(b) {
		return nil, storagebudget.ErrBounds
	}
	return storageOwnerCatalogFrameVersion(b, c.Sequence, 3)
}

func storageOwnerDigest(domain string, material any) (storagebudget.Digest, error) {
	b, err := json.Marshal(material)
	if err != nil {
		return storagebudget.Digest{}, err
	}
	h := sha256.New()
	h.Write([]byte(domain))
	h.Write([]byte{0})
	h.Write(b)
	var out storagebudget.Digest
	copy(out[:], h.Sum(nil))
	return out, nil
}

type storageOwnerDefinitions struct {
	Policy      storageOwnerPolicy
	Roots       []storagebudget.RegisteredRoot
	Workspaces  []storagebudget.RegisteredWorkspace
	Filesystems []storagebudget.Filesystem
	Constraints []storagebudget.CapacityConstraint
}

// Both returned complete definitions own their slices. Before is an exact
// dependency-closed subset of unchanged After rows under the identical policy.
// Proving it was actually enrolled remains the live prior-Q/catalog join.
func (c storageOwnerCatalogV2) definitions() (storageOwnerDefinitions, storageOwnerDefinitions, error) {
	a := storageOwnerDefinitions{c.Policy, []storagebudget.RegisteredRoot{}, []storagebudget.RegisteredWorkspace{}, []storagebudget.Filesystem{}, []storagebudget.CapacityConstraint{}}
	b := storageOwnerDefinitions{c.Policy, []storagebudget.RegisteredRoot{}, []storagebudget.RegisteredWorkspace{}, []storagebudget.Filesystem{}, []storagebudget.CapacityConstraint{}}
	rm := map[storagebudget.ID]storagebudget.RegisteredRoot{}
	wm := map[storagebudget.ID]storagebudget.RegisteredWorkspace{}
	fm := map[storagebudget.ID]storagebudget.Filesystem{}
	cm := map[storagebudget.ID]storagebudget.CapacityConstraint{}
	for _, v := range c.Roots {
		a.Roots = append(a.Roots, v.Definition)
		rm[v.Definition.ID] = v.Definition
	}
	for _, v := range c.Workspaces {
		a.Workspaces = append(a.Workspaces, v.Definition)
		wm[v.Definition.ID] = v.Definition
	}
	for _, v := range c.Filesystems {
		a.Filesystems = append(a.Filesystems, v.Definition)
		fm[v.Definition.ID] = v.Definition
	}
	for _, v := range c.Constraints {
		a.Constraints = append(a.Constraints, v.Definition)
		cm[v.Definition.ID] = v.Definition
	}
	oldR, oldW, oldF, oldC := map[storagebudget.ID]bool{}, map[storagebudget.ID]bool{}, map[storagebudget.ID]bool{}, map[storagebudget.ID]bool{}
	ids := c.BeforeDefinitionIDs
	if !storageOwnerSortedIDs(ids.Roots) || !storageOwnerSortedIDs(ids.Workspaces) || !storageOwnerSortedIDs(ids.Filesystems) || !storageOwnerSortedIDs(ids.Constraints) {
		return b, a, storagebudget.ErrBounds
	}
	for _, id := range ids.Roots {
		v, ok := rm[id]
		if !ok {
			return b, a, storagebudget.ErrBounds
		}
		b.Roots = append(b.Roots, v)
		oldR[id] = true
	}
	for _, id := range ids.Workspaces {
		v, ok := wm[id]
		if !ok {
			return b, a, storagebudget.ErrBounds
		}
		b.Workspaces = append(b.Workspaces, v)
		oldW[id] = true
	}
	for _, id := range ids.Filesystems {
		v, ok := fm[id]
		if !ok {
			return b, a, storagebudget.ErrBounds
		}
		b.Filesystems = append(b.Filesystems, v)
		oldF[id] = true
	}
	for _, id := range ids.Constraints {
		v, ok := cm[id]
		if !ok {
			return b, a, storagebudget.ErrBounds
		}
		b.Constraints = append(b.Constraints, v)
		oldC[id] = true
	}
	for _, v := range c.Roots {
		if !oldR[v.Definition.ID] {
			continue
		}
		d := v.Definition
		if !oldF[d.Filesystem] || d.Workspace != (storagebudget.ID{}) && !oldW[d.Workspace] || v.Source.ParentRoot != (storagebudget.ID{}) && !oldR[v.Source.ParentRoot] {
			return b, a, storagebudget.ErrBounds
		}
		for _, ref := range v.References {
			if !oldW[ref] {
				return b, a, storagebudget.ErrBounds
			}
		}
		for i := 0; i < int(d.CapacityCount); i++ {
			if !oldC[d.CapacityRefs[i].Constraint] {
				return b, a, storagebudget.ErrBounds
			}
		}
	}
	for _, v := range b.Filesystems {
		if !oldC[v.ByteConstraint] || v.InodeConstraint != (storagebudget.ID{}) && !oldC[v.InodeConstraint] {
			return b, a, storagebudget.ErrBounds
		}
	}
	return b, a, nil
}

func storageOwnerSortedIDs(ids []storagebudget.ID) bool {
	for i, id := range ids {
		if id == (storagebudget.ID{}) || i > 0 && bytes.Compare(ids[i-1][:], id[:]) >= 0 {
			return false
		}
	}
	return true
}
