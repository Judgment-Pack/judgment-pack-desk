package desk

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// storage_budget_transfer_intent.go — distinct closed transfer role.
// This is a closed whole-transfer record, not a general effect journal. A
// published DataIntent covers every immutable ordinal. Restart never dispatches
// any data ordinal: missing returned observations retain the entire promise and
// require original-owner reconciliation. In particular zero result rows after
// DataIntent do not prove that their effects were absent.
const (
	storageTransferWorkflow         = "storage-whole-transfer-v1"
	storageTransferBankA            = "storage-transfer.a"
	storageTransferBankB            = "storage-transfer.b"
	storageTransferPage             = 4096
	storageTransferBase             = 65536
	storageTransferDescendantSlots  = 8192
	storageTransferDescendantBytes  = 17
	storageTransferHeader           = storageTransferBase + storageTransferDescendantSlots*storageTransferDescendantBytes
	storageTransferOutputBytes      = 288
	storageTransferBankBytes        = storageTransferHeader + maxStorageFiles*storageTransferOutputBytes
	storageTransferGroupOffset      = 8192
	storageTransferGroupBytes       = 896
	storageTransferOldPointerOffset = 24576
	storageTransferNewPointerOffset = 40960
	storageTransferPointerBytes     = 16384
	storageTransferGroupEnd         = storageTransferGroupOffset + storagebudget.MaxComponents*storageTransferGroupBytes
	storageTransferControlGroups    = 23
)

type storageTransferPhase byte

const (
	storageTransferPrepared storageTransferPhase = iota + 1
	storageTransferReserved
	storageTransferDataIntent
	storageTransferPointerIntent
	storageTransferCompletionIntent
	storageTransferObserved
	storageTransferSettled
	storageTransferRetired
	// Appended without renumbering the existing unpublished wire phases.
	storageTransferRejected
)

type storageTransferMode byte

const (
	storageTransferMove storageTransferMode = iota + 1
	storageTransferRestore
	storageTransferExport
)

// The native basis is original, not inferred from a new child's pathname.
// Each receipt retains its actual object/incarnation; the other native fields
// must equal Basis before compression. Parent i is the original existing parent
// when i==0, otherwise the exact previously observed descendant i-1.
type storageTransferDescendant struct {
	Object, Incarnation uint64
	State               byte
}
type storageTransferGroup struct {
	Component                        storagebudget.Component
	RootIdentity, FilesystemIdentity storagebudget.Digest
	Locator                          string
	Native                           storageOwnerPersistentObject
	Membership                       storagebudget.Digest
	Flags                            byte
}
type storageTransferOutput struct {
	Name   string
	Bytes  uint64
	SHA256 storagebudget.Digest
	Native storageOwnerPersistentObject
	State  byte // 0 unknown; 1 entered/unknown; 2 closed/read back; 3 removed/parent completed; 4 not entered
	Group  byte
}

// The 47-byte cleanup closure occupies existing reserved metadata. It is only
// comparison data; the original executor must consume the complete frozen
// ordinal board and join actual Q settlement and complete census before issuing
// it. A restart cannot reconstruct this closure from zero result rows.
type storageTransferCleanup struct {
	Outputs, Directories                      uint16
	OutputsRemoved, OutputsNotEntered         uint16
	DirectoriesRemoved, DirectoriesNotEntered uint16
	Stage, Marker                             byte // 3 removed with parent sync and checked Close; 4 not entered
	Closure                                   byte // exact value 7: all ordinals, unchanged old pointer, complete census
	Effects                                   storagebudget.Digest
}

func storageTransferCleanupCodec(c *storageIntentCodec, v *storageTransferCleanup, read bool) {
	for _, n := range []*uint16{&v.Outputs, &v.Directories, &v.OutputsRemoved, &v.OutputsNotEntered, &v.DirectoriesRemoved, &v.DirectoriesNotEntered} {
		if read {
			*n = uint16(c.get(2))
		} else {
			c.put(uint64(*n), 2)
		}
	}
	if read {
		v.Stage = byte(c.get(1))
		v.Marker = byte(c.get(1))
		v.Closure = byte(c.get(1))
	} else {
		c.put(uint64(v.Stage), 1)
		c.put(uint64(v.Marker), 1)
		c.put(uint64(v.Closure), 1)
	}
	c.digest(&v.Effects, read)
}
func (v storageTransferCleanup) complete() bool {
	return v.Outputs <= maxStorageFiles && v.Directories < storageTransferDescendantSlots && uint32(v.OutputsRemoved)+uint32(v.OutputsNotEntered) == uint32(v.Outputs) && uint32(v.DirectoriesRemoved)+uint32(v.DirectoriesNotEntered) == uint32(v.Directories) && (v.Stage == 3 || v.Stage == 4) && (v.Marker == 3 || v.Marker == 4) && v.Closure == 7 && v.Effects != (storagebudget.Digest{})
}

type storageTransferRecord struct {
	Generation                                 uint64
	Phase                                      storageTransferPhase
	Mode                                       storageTransferMode
	Operation, Installation                    storagebudget.ID
	Owner, QBinding                            storagebudget.Digest
	Epoch, PolicyRevision                      uint64
	HostSlot                                   uint16
	HostGeneration                             uint64 // exact original Prepared projection generation
	Catalog, Plan, Body, Previous              storagebudget.Digest
	Token, InitialToken                        storagebudget.Token
	PointerState                               byte // 0 old; 1 attempted (possibly landed); 2 positively observed new; never decreases
	OldAbsent                                  bool
	OldPointerNative                           storageOwnerPersistentObject // immutable actual original pointer; zero only for absence/export
	Cleanup                                    storageTransferCleanup
	TargetExisting                             storagebudget.Vector // positive pre-effect marker/root census; physical already charged
	TargetEmpty                                bool                 // original positive bounded inventory, not absence inferred from errors
	Changes                                    []storagebudget.Change
	Tail                                       storageCreationTail // exact existing finite enrollment/selection escrow
	Settings, Source, Parent, Basis            storageOwnerPersistentObject
	Requester, TargetBefore, MarkerBefore      storageOwnerPersistentObject
	RequesterWorkspace                         storagebudget.ID
	RequesterGeneration                        uint64
	InputBytes, ArchiveBytes, MarkerBytes      uint64
	InputDigest, ArchiveDigest, MarkerDigest   storagebudget.Digest
	ArchiveNative, MarkerNative, PointerNative storageOwnerPersistentObject
	ArchiveState, MarkerState                  byte
	CreatedAt                                  string
	OldPointer, NewPointer                     []byte
	ExistingPrefix                             uint16 // first missing component offset in the exact NewPointer Path
	Descendants                                []storageTransferDescendant
	Groups                                     []storageTransferGroup
	Outputs                                    []storageTransferOutput
}

func storageTransferNative(c *storageIntentCodec, p *storageOwnerPersistentObject, read bool) {
	if read {
		p.Backend = uint8(c.get(1))
		c.take(p.VolumeUUID[:])
	} else {
		c.put(uint64(p.Backend), 1)
		c.raw(p.VolumeUUID[:])
	}
	c.number(&p.Object, read)
	c.number(&p.Incarnation, read)
	if read {
		p.IncarnationKind = uint8(c.get(1))
		p.ObjectKind = uint8(c.get(1))
	} else {
		c.put(uint64(p.IncarnationKind), 1)
		c.put(uint64(p.ObjectKind), 1)
	}
}

func storageTransferRecordShape(r storageTransferRecord) error {
	if r.Generation == 0 || r.Phase < storageTransferPrepared || r.Phase > storageTransferRejected || r.Mode < storageTransferMove || r.Mode > storageTransferExport ||
		r.Operation == (storagebudget.ID{}) || r.Installation == (storagebudget.ID{}) || r.Owner == (storagebudget.Digest{}) || r.QBinding == (storagebudget.Digest{}) || r.Owner != r.QBinding || r.Epoch == 0 || r.PolicyRevision == 0 || r.HostSlot >= storagebudget.Slots || r.HostGeneration == 0 || r.Catalog == (storagebudget.Digest{}) ||
		len(r.Groups) < 1 || len(r.Groups) > storagebudget.MaxComponents || len(r.Changes) != len(r.Groups) || len(r.Outputs) > maxStorageFiles || len(r.Descendants) >= storageTransferDescendantSlots ||
		len(r.OldPointer) > storageTransferPointerBytes || len(r.NewPointer) > storageTransferPointerBytes || r.PointerState > 2 || r.PointerState == 2 && !r.PointerNative.valid(2) || r.PointerState != 2 && r.PointerNative != (storageOwnerPersistentObject{}) || r.OldAbsent && len(r.OldPointer) != 0 ||
		r.Mode == storageTransferExport && (len(r.NewPointer) != 0 || len(r.Descendants) != 0 || r.PointerState != 0) || r.Mode != storageTransferExport && len(r.NewPointer) == 0 || len(r.CreatedAt) > 64 || r.ArchiveState > 4 || r.MarkerState > 4 {
		return storagebudget.ErrBounds
	}
	if r.Mode == storageTransferExport {
		if r.OldPointerNative != (storageOwnerPersistentObject{}) || r.Cleanup != (storageTransferCleanup{}) {
			return storagebudget.ErrBounds
		}
		if !reflect.DeepEqual(r.Tail, storageCreationTail{}) {
			return storagebudget.ErrBounds
		}
	} else if r.Tail.validate() != nil || r.Tail.Installation != r.Installation || r.Tail.QBinding != r.QBinding || r.Tail.Project != r.Requester || r.Tail.Bridge != r.Operation {
		return storagebudget.ErrAuthority
	}
	if r.Mode != storageTransferExport && (r.OldAbsent && r.OldPointerNative != (storageOwnerPersistentObject{}) || !r.OldAbsent && !r.OldPointerNative.valid(2)) {
		return storagebudget.ErrBounds
	}
	if r.Cleanup != (storageTransferCleanup{}) && (r.Phase < storageTransferObserved || r.Phase > storageTransferRetired || r.PointerState != 0 || !r.Cleanup.complete() || int(r.Cleanup.Outputs) != len(r.Outputs) || int(r.Cleanup.Directories) != len(r.Descendants)) {
		return storagebudget.ErrBounds
	}
	preAdmission := r.Phase == storageTransferPrepared || r.Phase == storageTransferRejected
	if preAdmission && (r.Token != (storagebudget.Token{}) || r.InitialToken != (storagebudget.Token{})) || !preAdmission && (r.Token.ID == (storagebudget.ID{}) || r.Token.Sequence == 0 || r.Token.Generation == 0 || r.Token.Slot >= storagebudget.Slots) {
		return storagebudget.ErrBounds
	}
	if !preAdmission && (r.InitialToken.ID != r.Token.ID || r.InitialToken.Slot != r.Token.Slot || r.InitialToken.Sequence != r.Token.Sequence || r.InitialToken.Generation == 0 || r.InitialToken.Generation > r.Token.Generation) {
		return storagebudget.ErrBounds
	}
	if !preAdmission {
		advances := uint64(1)
		if r.Phase == storageTransferReserved {
			advances = 0
		}
		if r.Phase >= storageTransferSettled {
			advances = 2
		}
		if r.InitialToken.Generation > ^uint64(0)-advances || r.Token.Generation != r.InitialToken.Generation+advances {
			return storagebudget.ErrBounds
		}
	}
	if !r.Requester.valid(1) || r.RequesterWorkspace == (storagebudget.ID{}) || r.RequesterGeneration == 0 || !r.Settings.valid(1) || !r.Source.valid(1) || !r.Parent.valid(1) || !r.Basis.valid(1) {
		return storagebudget.ErrAuthority
	}
	if r.Mode == storageTransferExport && (r.ArchiveBytes == 0 || r.ArchiveBytes > maxBackupArchive || r.ArchiveDigest == (storagebudget.Digest{}) || r.CreatedAt == "") || r.Mode != storageTransferExport && (r.ArchiveBytes != 0 || r.ArchiveDigest != (storagebudget.Digest{})) || r.Mode == storageTransferRestore && (r.InputBytes == 0 || r.InputBytes > maxBackupArchive || r.InputDigest == (storagebudget.Digest{})) || r.Mode == storageTransferMove && (r.InputBytes != 0 || r.InputDigest != (storagebudget.Digest{})) || r.MarkerBytes > 4096 {
		return storagebudget.ErrBounds
	}
	if r.Mode == storageTransferExport && r.TargetExisting != (storagebudget.Vector{}) {
		return storagebudget.ErrBounds
	}
	if r.Mode != storageTransferExport {
		if r.TargetBefore == (storageOwnerPersistentObject{}) {
			if r.TargetExisting != (storagebudget.Vector{}) {
				return storagebudget.ErrBounds
			}
		} else if r.TargetExisting.Directories != 1 || r.MarkerBytes == 0 && (r.TargetExisting.Bytes < 1 || r.TargetExisting.Bytes > 4096 || r.TargetExisting.Files != 1) || r.MarkerBytes > 0 && (r.TargetExisting.Bytes != 0 || r.TargetExisting.Files != 0) {
			return storagebudget.ErrBounds
		}
		if !r.TargetEmpty || r.TargetBefore.valid(1) && r.TargetBefore == r.Source {
			return storagebudget.ErrAuthority
		}
		if r.MarkerBytes == 0 {
			if !r.TargetBefore.valid(1) || !r.MarkerBefore.valid(2) {
				return storagebudget.ErrAuthority
			}
		} else if r.MarkerDigest == (storagebudget.Digest{}) || r.MarkerBefore != (storageOwnerPersistentObject{}) {
			return storagebudget.ErrBounds
		}
		var location dataLocation
		if decodeDataJSON(r.NewPointer, &location) != nil || location.Version != 1 || location.Path == "" || filepath.Clean(location.Path) != location.Path || strings.ContainsAny(location.Path, "\x00\r\n") || !filepath.IsAbs(location.Path) && !filepath.IsLocal(location.Path) || int(r.ExistingPrefix) > len(location.Path) {
			return storagebudget.ErrBounds
		}
		prefix := int(r.ExistingPrefix)
		if len(r.Descendants) == 0 {
			if prefix != len(location.Path) {
				return storagebudget.ErrBounds
			}
		} else if prefix == len(location.Path) || prefix > 0 && location.Path[prefix-1] != filepath.Separator || len(strings.Split(strings.TrimPrefix(location.Path[prefix:], string(filepath.Separator)), string(filepath.Separator))) != len(r.Descendants) {
			return storagebudget.ErrBounds
		}
	}
	for i, g := range r.Groups {
		if len(g.Locator) > 256 || g.RootIdentity == (storagebudget.Digest{}) || g.FilesystemIdentity == (storagebudget.Digest{}) || g.Membership == (storagebudget.Digest{}) ||
			g.Component.Root == (storagebudget.ID{}) || g.Component.Filesystem == (storagebudget.ID{}) || g.Component.RootGeneration == 0 || g.Component.FilesystemGeneration == 0 ||
			g.Component.Owner == (storagebudget.Digest{}) || g.Component.Workspace != (storagebudget.ID{}) || g.Component.WorkspaceGeneration != 0 || g.Component.Category >= storagebudget.Categories || g.Component.CapacityCount < 1 || g.Component.CapacityCount > storagebudget.MaxCapacityRefs || g.Flags > storageTransferSymmetricDifferenceReferences || !g.Native.valid(1) {
			return storagebudget.ErrBounds
		}
		if i > 0 {
			prior := r.Groups[i-1].Component
			n := bytes.Compare(prior.Root[:], g.Component.Root[:])
			if n > 0 || n == 0 && (prior.Category > g.Component.Category || prior.Category == g.Component.Category && r.Groups[i-1].Flags >= g.Flags) {
				return storagebudget.ErrBounds
			}
		}
	}
	for i, o := range r.Outputs {
		if !chatStorageFile(o.Name) || len(o.Name) > 192 || o.Bytes > 1<<60 || o.SHA256 == (storagebudget.Digest{}) || int(o.Group) >= len(r.Groups) || o.State > 4 || i > 0 && r.Outputs[i-1].Name >= o.Name {
			return storagebudget.ErrBounds
		}
		if (o.State < 2 || o.State == 4) && o.Native != (storageOwnerPersistentObject{}) || (o.State == 2 || o.State == 3) && !o.Native.valid(2) {
			return storagebudget.ErrBounds
		}
		if (r.Phase < storageTransferDataIntent || r.Phase == storageTransferRejected) && o.State != 0 {
			return storagebudget.ErrBounds
		}
	}
	for i, d := range r.Descendants {
		if d.State > 3 || (d.State == 0 || d.State == 3) && (d.Object != 0 || d.Incarnation != 0) || d.State == 2 && d.Object == 0 || (d.State == 1 || d.State == 2) && i > 0 && r.Descendants[i-1].State != 2 || (r.Phase < storageTransferDataIntent || r.Phase == storageTransferRejected) && d.State != 0 {
			return storagebudget.ErrBounds
		}
	}
	// A rejected page never carries an effect/result/token. Its label is only
	// comparison data: original same-epoch no-ticket proof remains mandatory.
	if r.Phase == storageTransferRejected {
		if r.PointerState != 0 || r.PointerNative != (storageOwnerPersistentObject{}) || r.ArchiveNative != (storageOwnerPersistentObject{}) || r.MarkerNative != (storageOwnerPersistentObject{}) || r.ArchiveState != 0 || r.MarkerState != 0 {
			return storagebudget.ErrBounds
		}
		for _, change := range r.Changes {
			if change != (storagebudget.Change{}) {
				return storagebudget.ErrBounds
			}
		}
	}
	if r.Phase >= storageTransferSettled && r.Phase <= storageTransferRetired {
		if r.PointerState == 1 {
			return storagebudget.ErrUnknown
		}
		for _, o := range r.Outputs {
			if o.State < 2 {
				return storagebudget.ErrUnknown
			}
		}
		for _, d := range r.Descendants {
			if d.State < 2 {
				return storagebudget.ErrUnknown
			}
		}
		if r.MarkerBytes > 0 && r.MarkerState < 2 || (r.ArchiveBytes > 0 || r.InputBytes > 0) && r.ArchiveState < 2 {
			return storagebudget.ErrUnknown
		}
	}
	return nil
}

func storageTransferBodyCodec(b []byte, r *storageTransferRecord, read bool) bool {
	if len(b) != storageTransferBankBytes {
		return false
	}
	c := storageIntentCodec{b: b, at: storageTransferPage}
	storageTransferNative(&c, &r.Settings, read)
	storageTransferNative(&c, &r.Source, read)
	storageTransferNative(&c, &r.Parent, read)
	storageTransferNative(&c, &r.Basis, read)
	storageTransferNative(&c, &r.Requester, read)
	storageTransferNative(&c, &r.TargetBefore, read)
	storageTransferNative(&c, &r.MarkerBefore, read)
	c.id(&r.RequesterWorkspace, read)
	c.number(&r.RequesterGeneration, read)
	c.number(&r.InputBytes, read)
	c.number(&r.ArchiveBytes, read)
	c.number(&r.MarkerBytes, read)
	c.digest(&r.InputDigest, read)
	c.digest(&r.ArchiveDigest, read)
	c.digest(&r.MarkerDigest, read)
	storageTransferNative(&c, &r.ArchiveNative, read)
	storageTransferNative(&c, &r.MarkerNative, read)
	c.text(&r.CreatedAt, 64, read)
	c.vector(&r.TargetExisting, read)
	c.boolean(&r.TargetEmpty, read)
	storageTransferNative(&c, &r.PointerNative, read)
	// Original pointer native35 and cleanup47 consume 82 previously-zero bytes
	// before GroupOffset8192. All remaining metadata padding stays closed.
	storageTransferNative(&c, &r.OldPointerNative, read)
	storageTransferCleanupCodec(&c, &r.Cleanup, read)
	if read {
		r.ArchiveState = byte(c.get(1))
		r.MarkerState = byte(c.get(1))
	} else {
		c.put(uint64(r.ArchiveState), 1)
		c.put(uint64(r.MarkerState), 1)
	}
	if read {
		r.ExistingPrefix = uint16(c.get(2))
		ng, no, nd := int(c.get(1)), int(c.get(2)), int(c.get(2))
		old, next := int(c.get(2)), int(c.get(2))
		if ng > storagebudget.MaxComponents || no > maxStorageFiles || nd >= storageTransferDescendantSlots || old > storageTransferPointerBytes || next > storageTransferPointerBytes {
			return false
		}
		r.Groups = make([]storageTransferGroup, ng)
		r.Outputs = make([]storageTransferOutput, no)
		r.Descendants = make([]storageTransferDescendant, nd)
		r.OldPointer = append([]byte{}, b[storageTransferOldPointerOffset:storageTransferOldPointerOffset+old]...)
		r.NewPointer = append([]byte{}, b[storageTransferNewPointerOffset:storageTransferNewPointerOffset+next]...)
		if !storageAllZero(b[storageTransferOldPointerOffset+old:storageTransferNewPointerOffset]) || !storageAllZero(b[storageTransferNewPointerOffset+next:57344]) {
			return false
		}
	} else {
		c.put(uint64(r.ExistingPrefix), 2)
		c.put(uint64(len(r.Groups)), 1)
		c.put(uint64(len(r.Outputs)), 2)
		c.put(uint64(len(r.Descendants)), 2)
		c.put(uint64(len(r.OldPointer)), 2)
		c.put(uint64(len(r.NewPointer)), 2)
		copy(b[storageTransferOldPointerOffset:storageTransferNewPointerOffset], r.OldPointer)
		copy(b[storageTransferNewPointerOffset:57344], r.NewPointer)
	}
	c.skip(storageTransferGroupOffset-c.at, read)
	for i := 0; i < storagebudget.MaxComponents; i++ {
		end := c.at + storageTransferGroupBytes
		if i < len(r.Groups) {
			g := &r.Groups[i]
			c.component(&g.Component, read)
			c.digest(&g.RootIdentity, read)
			c.digest(&g.FilesystemIdentity, read)
			c.text(&g.Locator, 256, read)
			storageTransferNative(&c, &g.Native, read)
			c.digest(&g.Membership, read)
			if read {
				g.Flags = byte(c.get(1))
			} else {
				c.put(uint64(g.Flags), 1)
			}
		}
		c.skip(end-c.at, read)
	}
	c.skip(storageTransferOldPointerOffset-c.at, read)
	// Pointer slots were encoded above. The remaining base padding is closed.
	c.at = 57344
	if r.Mode == storageTransferExport {
		c.skip(storageCreationTailBytes, read)
	} else {
		if !storageCreationTailCodec(b[c.at:c.at+storageCreationTailBytes], &r.Tail, read) {
			return false
		}
		c.at += storageCreationTailBytes
	}
	c.skip(storageTransferBase-c.at, read)
	for i := 0; i < storageTransferDescendantSlots; i++ {
		if i < len(r.Descendants) {
			d := &r.Descendants[i]
			c.number(&d.Object, read)
			c.number(&d.Incarnation, read)
			if read {
				d.State = byte(c.get(1))
			} else {
				c.put(uint64(d.State), 1)
			}
		} else {
			c.skip(storageTransferDescendantBytes, read)
		}
	}
	for i := 0; i < maxStorageFiles; i++ {
		end := c.at + storageTransferOutputBytes
		if i < len(r.Outputs) {
			o := &r.Outputs[i]
			c.text(&o.Name, 192, read)
			c.number(&o.Bytes, read)
			c.digest(&o.SHA256, read)
			storageTransferNative(&c, &o.Native, read)
			if read {
				o.State = byte(c.get(1))
				o.Group = byte(c.get(1))
			} else {
				c.put(uint64(o.State), 1)
				c.put(uint64(o.Group), 1)
			}
		}
		c.skip(end-c.at, read)
	}
	return !c.bad && c.at == len(b)
}

func storageTransferHeaderCodec(b []byte, r *storageTransferRecord, read bool) bool {
	if len(b) < storageTransferPage {
		return false
	}
	c := storageIntentCodec{b: b[:storageTransferPage], at: 128}
	c.number(&r.Generation, read)
	if read {
		r.Phase = storageTransferPhase(c.get(1))
		r.Mode = storageTransferMode(c.get(1))
	} else {
		c.put(uint64(r.Phase), 1)
		c.put(uint64(r.Mode), 1)
	}
	c.id(&r.Operation, read)
	c.id(&r.Installation, read)
	c.digest(&r.Owner, read)
	c.digest(&r.QBinding, read)
	c.number(&r.Epoch, read)
	c.number(&r.PolicyRevision, read)
	if read {
		r.HostSlot = uint16(c.get(2))
	} else {
		c.put(uint64(r.HostSlot), 2)
	}
	c.number(&r.HostGeneration, read)
	c.digest(&r.Catalog, read)
	c.digest(&r.Plan, read)
	c.digest(&r.Body, read)
	c.digest(&r.Previous, read)
	c.token(&r.Token, read)
	c.token(&r.InitialToken, read)
	if read {
		r.PointerState = byte(c.get(1))
	} else {
		c.put(uint64(r.PointerState), 1)
	}
	c.boolean(&r.OldAbsent, read)
	if read {
		n := int(c.get(1))
		if n > storagebudget.MaxComponents {
			return false
		}
		r.Changes = make([]storagebudget.Change, n)
	} else {
		c.put(uint64(len(r.Changes)), 1)
	}
	for i := 0; i < storagebudget.MaxComponents; i++ {
		if i < len(r.Changes) {
			c.change(&r.Changes[i], read)
		} else {
			c.skip(96, read)
		}
	}
	c.skip(storageTransferPage-c.at, read)
	return !c.bad && c.at == storageTransferPage
}

// Immutable plan binding includes exact pointer bytes, every complete physical
// group/reference-set digest and every output name/size/digest, including empty
// files. Caller slices never alias the canonical comparison image.
func storageTransferPlanDigest(r storageTransferRecord) (storagebudget.Digest, error) {
	if err := storageTransferRecordShape(r); err != nil {
		return storagebudget.Digest{}, err
	}
	r.Cleanup = storageTransferCleanup{}
	r.ArchiveNative = storageOwnerPersistentObject{}
	r.MarkerNative = storageOwnerPersistentObject{}
	r.ArchiveState = 0
	r.MarkerState = 0
	r.Generation = 1
	r.Phase = storageTransferPrepared
	r.Token = storagebudget.Token{}
	r.InitialToken = storagebudget.Token{}
	r.PointerState = 0
	r.PointerNative = storageOwnerPersistentObject{}
	r.Plan = storagebudget.Digest{}
	r.Body = storagebudget.Digest{}
	r.Changes = make([]storagebudget.Change, len(r.Groups))
	r.Outputs = append([]storageTransferOutput{}, r.Outputs...)
	r.Descendants = append([]storageTransferDescendant{}, r.Descendants...)
	for i := range r.Outputs {
		r.Outputs[i].Native = storageOwnerPersistentObject{}
		r.Outputs[i].State = 0
	}
	for i := range r.Descendants {
		r.Descendants[i] = storageTransferDescendant{}
	}
	b := make([]byte, storageTransferBankBytes)
	if !storageTransferBodyCodec(b, &r, false) || !storageTransferHeaderCodec(b, &r, false) {
		return storagebudget.Digest{}, storagebudget.ErrBounds
	}
	return storageEnrollmentPeakHash("desk-storage-whole-transfer-plan-v1", b), nil
}

func storageTransferEncode(r storageTransferRecord) ([]byte, error) {
	if err := storageTransferRecordShape(r); err != nil {
		return nil, err
	}
	plan, err := storageTransferPlanDigest(r)
	if err != nil || r.Plan != (storagebudget.Digest{}) && r.Plan != plan {
		return nil, errors.Join(storagebudget.ErrAuthority, err)
	}
	r.Plan = plan
	b := make([]byte, storageTransferBankBytes)
	copy(b[:8], []byte("DSKHTR1\x00"))
	binary.LittleEndian.PutUint32(b[8:12], 1)
	binary.LittleEndian.PutUint32(b[12:16], storageTransferBankBytes)
	if !storageTransferBodyCodec(b, &r, false) {
		return nil, storagebudget.ErrBounds
	}
	r.Body = sha256.Sum256(b[storageTransferPage:])
	if !storageTransferHeaderCodec(b, &r, false) {
		return nil, storagebudget.ErrBounds
	}
	d := sha256.Sum256(b[:storageTransferPage])
	copy(b[96:128], d[:])
	return b, nil
}

func storageTransferDecode(b []byte) (storageTransferRecord, error) {
	var r storageTransferRecord
	if len(b) != storageTransferBankBytes || string(b[:8]) != "DSKHTR1\x00" || binary.LittleEndian.Uint32(b[8:12]) != 1 || binary.LittleEndian.Uint32(b[12:16]) != storageTransferBankBytes || !storageAllZero(b[16:96]) {
		return r, storagebudget.ErrBounds
	}
	header := append([]byte{}, b[:storageTransferPage]...)
	var expected storagebudget.Digest
	copy(expected[:], header[96:128])
	clear(header[96:128])
	if sha256.Sum256(header) != expected || !storageTransferHeaderCodec(b, &r, true) || !storageTransferBodyCodec(b, &r, true) || sha256.Sum256(b[storageTransferPage:]) != r.Body {
		return storageTransferRecord{}, storagebudget.ErrUnknown
	}
	if err := storageTransferRecordShape(r); err != nil {
		return storageTransferRecord{}, err
	}
	plan, err := storageTransferPlanDigest(r)
	if err != nil || plan != r.Plan {
		return storageTransferRecord{}, errors.Join(storagebudget.ErrAuthority, err)
	}
	return r, nil
}

func storageTransferSortedOutputs(plan chatTransferPlan, group func(string) (byte, error)) ([]storageTransferOutput, error) {
	if len(plan.Files) > maxStorageFiles {
		return nil, storagebudget.ErrBounds
	}
	out := make([]storageTransferOutput, 0, len(plan.Files))
	for _, f := range plan.Files {
		if f.Bytes < 0 || len(f.SHA256) != 64 {
			return nil, storagebudget.ErrBounds
		}
		d, err := storageTransferParseDigest(f.SHA256)
		if err != nil {
			return nil, err
		}
		g, err := group(f.Name)
		if err != nil {
			return nil, err
		}
		out = append(out, storageTransferOutput{Name: f.Name, Bytes: uint64(f.Bytes), SHA256: d, Group: g})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	for i := 1; i < len(out); i++ {
		if out[i-1].Name == out[i].Name {
			return nil, storagebudget.ErrBounds
		}
	}
	return out, nil
}

func storageTransferParseDigest(s string) (storagebudget.Digest, error) {
	var d storagebudget.Digest
	if len(s) != 64 {
		return d, storagebudget.ErrBounds
	}
	for i := range d {
		var n byte
		for j := 0; j < 2; j++ {
			c := s[i*2+j]
			if c >= '0' && c <= '9' {
				n = n*16 + c - '0'
			} else if c >= 'a' && c <= 'f' {
				n = n*16 + c - 'a' + 10
			} else {
				return storagebudget.Digest{}, storagebudget.ErrBounds
			}
		}
		d[i] = n
	}
	return d, nil
}

// A new operation is never inferred from an earlier operation's path. Bank
// reuse additionally requires original Q application and census/selection
// proofs; this comparison deliberately supplies no reuse authority.
func storageTransferTransition(old, next storageTransferRecord) bool {
	if old.Generation == ^uint64(0) || next.Generation != old.Generation+1 || old.Operation != next.Operation || old.Plan != next.Plan || old.Plan == (storagebudget.Digest{}) || old.PointerState > next.PointerState {
		return false
	}
	a, ae := storageTransferPlanDigest(old)
	b, be := storageTransferPlanDigest(next)
	if ae != nil || be != nil || a != old.Plan || b != old.Plan {
		return false
	}
	allowed := false
	switch old.Phase {
	case storageTransferPrepared:
		allowed = next.Phase == storageTransferReserved || next.Phase == storageTransferRejected
	case storageTransferReserved:
		allowed = next.Phase == storageTransferDataIntent
	case storageTransferDataIntent:
		allowed = next.Phase == storageTransferPointerIntent || next.Phase == storageTransferCompletionIntent
	case storageTransferPointerIntent:
		allowed = next.Phase == storageTransferCompletionIntent
	case storageTransferCompletionIntent:
		allowed = next.Phase == storageTransferObserved
	case storageTransferObserved:
		allowed = next.Phase == storageTransferSettled
	case storageTransferSettled:
		allowed = next.Phase == storageTransferRetired
	}
	if !allowed {
		return false
	}
	// Only publication of the complete original returned observations can
	// change the body. Header publications cannot rewrite a result or names.
	if next.Phase != storageTransferObserved && old.Body != next.Body {
		return false
	}
	if old.Phase != storageTransferPrepared && next.Token.ID != old.Token.ID || old.Phase != storageTransferPrepared && (next.Token.Slot != old.Token.Slot || next.Token.Sequence != old.Token.Sequence) {
		return false
	}
	if next.InitialToken != old.InitialToken && old.Phase != storageTransferPrepared {
		return false
	}
	if old.Phase != storageTransferPrepared {
		want := old.Token
		if next.Phase == storageTransferDataIntent || next.Phase == storageTransferSettled {
			if want.Generation == ^uint64(0) {
				return false
			}
			want.Generation++
		}
		if next.Token != want {
			return false
		}
	}
	return true
}

type storageTransferPair struct {
	Banks        [2]storageTransferRecord
	Selected     int
	RecoveryOnly bool
}

// Both banks are strict operands. There is no fallback from an unknown peer,
// even when the other page says Retired. A known initial/reuse/result-body cut
// is inspectable comparison data, never automatic resume or bank replacement.
func storageTransferDecodePair(raw [2][]byte) (storageTransferPair, error) {
	var p storageTransferPair
	var zero [2]bool
	for i, b := range raw {
		if len(b) != storageTransferBankBytes {
			return p, storagebudget.ErrBounds
		}
		zero[i] = storageAllZero(b)
		if !zero[i] {
			r, err := storageTransferDecode(b)
			if err != nil {
				return storageTransferPair{}, err
			}
			p.Banks[i] = r
		}
	}
	if zero[0] && zero[1] {
		return p, storagebudget.ErrUnknown
	}
	if zero[0] || zero[1] {
		if zero[0] {
			p.Selected = 1
		}
		if p.Banks[p.Selected].Phase != storageTransferPrepared || p.Banks[p.Selected].Previous != (storagebudget.Digest{}) {
			return storageTransferPair{}, storagebudget.ErrUnknown
		}
		p.RecoveryOnly = true
		return p, nil
	}
	a, b := p.Banks[0], p.Banks[1]
	if a.Operation != b.Operation {
		old, newer := 0, 1
		if a.Phase == storageTransferPrepared {
			old, newer = 1, 0
		}
		if (p.Banks[old].Phase != storageTransferRetired && p.Banks[old].Phase != storageTransferRejected) || p.Banks[newer].Phase != storageTransferPrepared || p.Banks[newer].Generation != 1 || p.Banks[newer].Previous != sha256.Sum256(raw[old]) || p.Banks[old].Installation != p.Banks[newer].Installation || p.Banks[old].QBinding != p.Banks[newer].QBinding {
			return storageTransferPair{}, storagebudget.ErrUnknown
		}
		p.Selected, p.RecoveryOnly = newer, true
		return p, nil
	}
	if a.Plan != b.Plan {
		return storageTransferPair{}, storagebudget.ErrAuthority
	}
	if a.Generation == b.Generation {
		if !bytes.Equal(raw[0], raw[1]) {
			return storageTransferPair{}, storagebudget.ErrUnknown
		}
		return p, nil
	}
	low, high := 0, 1
	if a.Generation > b.Generation {
		low, high = 1, 0
	}
	if !storageTransferTransition(p.Banks[low], p.Banks[high]) {
		return storageTransferPair{}, storagebudget.ErrUnknown
	}
	p.Selected = high
	p.RecoveryOnly = p.Banks[low].Body != p.Banks[high].Body
	return p, nil
}

// This persistence leaf supplies no admission. The original operation must
// first consume its exact predeclared control group and retain this named
// native witness. Both publication shapes are closed: complete Prepared/Observed
// banks, or a single header of the same immutable body. No create/truncate,
// fallback barrier, native write retry or ownership discovery occurs here.
// The selected peer supplies the predecessor. In an alternating publication,
// the overwritten bank is two phases behind; treating it as the predecessor
// would refuse the second header and leave the admitted operation unfinished.
// This is a closed byte comparison, never reuse/admission authority.
func storageTransferPersistenceBefore(before [2][]byte, bank int, next []byte, headerOnly bool) error {
	if bank < 0 || bank > 1 || len(before[0]) != storageTransferBankBytes || len(before[1]) != storageTransferBankBytes {
		return storagebudget.ErrBounds
	}
	r, err := storageTransferDecode(next)
	if err != nil {
		return err
	}
	if storageAllZero(before[0]) && storageAllZero(before[1]) {
		if headerOnly || r.Phase != storageTransferPrepared || r.Generation != 1 || r.Previous != (storagebudget.Digest{}) {
			return storagebudget.ErrStale
		}
		return nil
	}
	pair, err := storageTransferDecodePair(before)
	if err != nil {
		return err
	}
	old := pair.Banks[pair.Selected]
	// The second full publication makes the same Prepared/Observed image a
	// strict mirrored pair. No changed result or header can use this branch.
	if !headerOnly && (r.Phase == storageTransferPrepared || r.Phase == storageTransferObserved) && bytes.Equal(next, before[pair.Selected]) && !bytes.Equal(next, before[bank]) {
		return nil
	}
	if pair.RecoveryOnly || bank == pair.Selected {
		return storagebudget.ErrStale
	}
	if r.Operation != old.Operation {
		if headerOnly || r.Phase != storageTransferPrepared || (old.Phase != storageTransferRetired && old.Phase != storageTransferRejected) || r.Generation != 1 || r.Previous != sha256.Sum256(before[pair.Selected]) || r.Installation != old.Installation || r.QBinding != old.QBinding {
			return storagebudget.ErrStale
		}
		return nil
	}
	if !storageTransferTransition(old, r) {
		return storagebudget.ErrStale
	}
	if headerOnly {
		if !bytes.Equal(before[bank][storageTransferPage:], next[storageTransferPage:]) {
			return storagebudget.ErrStale
		}
	} else if r.Phase != storageTransferObserved {
		return storagebudget.ErrAuthority
	}
	return nil
}

func storageTransferPersist(ctx context.Context, n storageNamedFile, natives [2]storageOwnerPersistentObject, beforePair [2][]byte, bank int, next []byte, headerOnly bool) (err error) {
	if ctx == nil || bank < 0 || bank > 1 || n.name != []string{storageTransferBankA, storageTransferBankB}[bank] || n.size != storageTransferBankBytes || !natives[0].valid(2) || !natives[1].valid(2) || natives[0] == natives[1] {
		return storagebudget.ErrAuthority
	}
	native := natives[bank]
	if err = storageTransferPersistenceBefore(beforePair, bank, next, headerOnly); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = n.current(); err != nil {
		return err
	}
	actual, e := storageOwnerPersistentFile(ctx, n)
	if e != nil || actual != native {
		return errors.Join(errStorageCustody, e)
	}
	// Both exact original peers are read and checked before each overwrite.
	// Ten maximum publications bound these reads to20 complete bank images;
	// target readback adds four banks plus six pages. No unknown peer fallback.
	for index, name := range []string{storageTransferBankA, storageTransferBankB} {
		peer, e := storageOpenFile(n.root, name, storageTransferBankBytes)
		if e != nil {
			return e
		}
		actual, e := storageOwnerPersistentFile(ctx, peer)
		observed := make([]byte, storageTransferBankBytes)
		count, re := peer.file.ReadAt(observed, 0)
		e = errors.Join(e, re, peer.current(), peer.file.Close())
		if e != nil || actual != natives[index] || count != len(observed) || !bytes.Equal(observed, beforePair[index]) {
			return errors.Join(storagebudget.ErrStale, e)
		}
	}
	write, e := storageOpenFile(n.root, n.name, n.size)
	if e != nil {
		return e
	}
	closed := false
	defer func() {
		if !closed {
			err = errors.Join(err, write.file.Close())
		}
	}()
	if !os.SameFile(write.identity, n.identity) {
		return errStorageCustody
	}
	length := storageTransferBankBytes
	if headerOnly {
		length = storageTransferPage
	}
	check := make([]byte, length)
	for offset := 0; offset < length; {
		end := min(length, offset+65536)
		if err = storageEnrollmentNativeWrite(ctx, write.file, next[offset:end], int64(offset)); err != nil {
			return errors.Join(storagebudget.ErrUnknown, err)
		}
		offset = end
	}
	if err = storageEnrollmentNativeSync(ctx, write.file); err != nil {
		return errors.Join(storagebudget.ErrUnknown, err)
	}
	closed = true
	if err = write.file.Close(); err != nil {
		return errors.Join(storagebudget.ErrUnknown, err)
	}
	read, e := storageOpenFile(n.root, n.name, n.size)
	if e != nil {
		return errors.Join(storagebudget.ErrUnknown, e)
	}
	if !os.SameFile(read.identity, n.identity) {
		return errors.Join(errStorageCustody, read.file.Close())
	}
	count, e := read.file.ReadAt(check, 0)
	closeErr := read.file.Close()
	if e != nil || count != length || closeErr != nil || !bytes.Equal(check, next[:length]) {
		return errors.Join(storagebudget.ErrUnknown, e, closeErr, func() error {
			if count != length {
				return io.ErrUnexpectedEOF
			}
			return nil
		}())
	}
	return errors.Join(n.current(), ctx.Err())
}

// storage_budget_transfer_admission.go — distinct closed transfer role.
// A group selects a closed, complete retained reference set. None allows a
// caller-provided subset. Data copied out of a shared source stays attributed
// independently to every original source reference; physical bytes are counted
// once. Fixed installation control has no workspace attribution.
const (
	storageTransferNoReferences byte = iota
	storageTransferRootReferences
	storageTransferSourceReferences
	storageTransferUnionReferences
	storageTransferIntersectionReferences
	storageTransferRootOnlyReferences
	storageTransferSourceOnlyReferences
	storageTransferSymmetricDifferenceReferences
)

func storageTransferReferences(r storageTransferRecord, catalog storageOwnerCatalogV2) (*storagebudget.WorkspaceFanout, error) {
	if storageTransferRecordShape(r) != nil || catalog.Installation != r.Installation || catalog.QBinding != r.QBinding {
		return nil, storagebudget.ErrAuthority
	}
	digest, err := storageTransferCatalogDigest(catalog)
	if err != nil || digest != r.Catalog {
		return nil, storagebudget.ErrAuthority
	}
	workspace := map[storagebudget.ID]storagebudget.RegisteredWorkspace{}
	for _, w := range catalog.Workspaces {
		if _, ok := workspace[w.Definition.ID]; ok {
			return nil, storagebudget.ErrBounds
		}
		workspace[w.Definition.ID] = w.Definition
	}
	if workspace[r.RequesterWorkspace].Generation != r.RequesterGeneration {
		return nil, storagebudget.ErrAuthority
	}
	var source *storageOwnerRoot
	for i := range catalog.Roots {
		row := &catalog.Roots[i]
		if int(row.DirectoryObject) >= len(catalog.Objects) {
			return nil, storagebudget.ErrBounds
		}
		if catalog.Objects[row.DirectoryObject].Persistent == r.Source && !storageTransferCatalogView(*row) {
			if source != nil {
				return nil, storagebudget.ErrAuthority
			}
			source = row
		}
	}
	refs := make([][]storagebudget.ID, len(r.Groups))
	all := map[storagebudget.ID]bool{}
	type expandedKey struct {
		root, workspace storagebudget.ID
		category        byte
	}
	seen := map[expandedKey]bool{}
	for i, g := range r.Groups {
		var root *storageOwnerRoot
		for j := range catalog.Roots {
			row := &catalog.Roots[j]
			if row.Definition.ID == g.Component.Root {
				if root != nil {
					return nil, storagebudget.ErrAuthority
				}
				root = row
			}
		}
		if root == nil || root.Definition.Generation != g.Component.RootGeneration || root.Definition.Owner != g.Component.Owner || root.Definition.Identity != g.RootIdentity || root.Definition.Filesystem != g.Component.Filesystem || root.Definition.CapacityBinding != g.Component.CapacityBinding || root.Definition.CapacityCount != g.Component.CapacityCount || root.Definition.Locator != g.Locator || catalog.Objects[root.DirectoryObject].Persistent != g.Native {
			return nil, storagebudget.ErrAuthority
		}
		filesystemFound := false
		for _, f := range catalog.Filesystems {
			if f.Definition.ID == g.Component.Filesystem {
				if filesystemFound || f.Definition.Generation != g.Component.FilesystemGeneration || f.Definition.Identity != g.FilesystemIdentity {
					return nil, storagebudget.ErrAuthority
				}
				filesystemFound = true
			}
		}
		if !filesystemFound {
			return nil, storagebudget.ErrAuthority
		}
		for j := 0; j < storagebudget.MaxCapacityRefs; j++ {
			want := root.Definition.CapacityRefs[j]
			got := g.Component.Capacity[j]
			if j < int(root.Definition.CapacityCount) {
				if got.Constraint != want.Constraint || got.Generation != want.Generation {
					return nil, storagebudget.ErrAuthority
				}
			} else if got != (storagebudget.CapacityPromise{}) {
				return nil, storagebudget.ErrBounds
			}
		}
		set := map[storagebudget.ID]bool{}
		rootSet, sourceSet := map[storagebudget.ID]bool{}, map[storagebudget.ID]bool{}
		for _, id := range root.References {
			rootSet[id] = true
		}
		if source != nil {
			for _, id := range source.References {
				sourceSet[id] = true
			}
		}
		if g.Flags != storageTransferNoReferences && g.Flags != storageTransferRootReferences && source == nil {
			return nil, storagebudget.ErrAuthority
		}
		for id := range rootSet {
			if g.Flags == storageTransferRootReferences || g.Flags == storageTransferUnionReferences || g.Flags == storageTransferIntersectionReferences && sourceSet[id] || (g.Flags == storageTransferRootOnlyReferences || g.Flags == storageTransferSymmetricDifferenceReferences) && !sourceSet[id] {
				set[id] = true
			}
		}
		for id := range sourceSet {
			if g.Flags == storageTransferSourceReferences || g.Flags == storageTransferUnionReferences || (g.Flags == storageTransferSourceOnlyReferences || g.Flags == storageTransferSymmetricDifferenceReferences) && !rootSet[id] {
				set[id] = true
			}
		}
		for id := range set {
			if id == (storagebudget.ID{}) || workspace[id].Generation == 0 {
				return nil, storagebudget.ErrAuthority
			}
			refs[i] = append(refs[i], id)
			all[id] = true
		}
		sort.Slice(refs[i], func(a, b int) bool { return bytes.Compare(refs[i][a][:], refs[i][b][:]) < 0 })
		keys := refs[i]
		if len(keys) == 0 {
			keys = []storagebudget.ID{{}}
		}
		for _, id := range keys {
			key := expandedKey{g.Component.Root, id, g.Component.Category}
			if seen[key] {
				return nil, storagebudget.ErrBounds
			}
			seen[key] = true
		}
		if storageTransferMembershipDigest(r.Catalog, g.Component, refs[i], workspace) != g.Membership {
			return nil, storagebudget.ErrAuthority
		}
		if len(refs[i]) == 0 && (g.Component.Attribution != (storagebudget.Vector{}) || g.Component.SettlementAttribution != (storagebudget.Vector{})) {
			return nil, storagebudget.ErrAuthority
		}
	}
	if len(all) > storagebudget.MaxWorkspaces {
		return nil, storagebudget.ErrBounds
	}
	ids := make([]storagebudget.ID, 0, len(all))
	for id := range all {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return bytes.Compare(ids[i][:], ids[j][:]) < 0 })
	out := &storagebudget.WorkspaceFanout{Workspaces: make([]storagebudget.WorkspaceReference, len(ids)), Memberships: make([]storagebudget.WorkspaceMembership, len(r.Groups))}
	index := map[storagebudget.ID]int{}
	for i, id := range ids {
		index[id] = i
		out.Workspaces[i] = storagebudget.WorkspaceReference{ID: id, Generation: workspace[id].Generation}
	}
	for i := range refs {
		for _, id := range refs[i] {
			j := index[id]
			out.Memberships[i][j/8] |= 1 << uint(j%8)
		}
	}
	return out, nil
}

func storageTransferMembershipDigest(catalog storagebudget.Digest, component storagebudget.Component, ids []storagebudget.ID, workspace map[storagebudget.ID]storagebudget.RegisteredWorkspace) storagebudget.Digest {
	h := sha256.New()
	h.Write([]byte("desk-storage-whole-transfer-reference-set-v1\x00"))
	h.Write(catalog[:])
	h.Write(component.Root[:])
	h.Write([]byte{component.Category})
	var n [8]byte
	binary.LittleEndian.PutUint64(n[:], component.RootGeneration)
	h.Write(n[:])
	for _, id := range ids {
		h.Write(id[:])
		binary.LittleEndian.PutUint64(n[:], workspace[id].Generation)
		h.Write(n[:])
	}
	var d storagebudget.Digest
	copy(d[:], h.Sum(nil))
	return d
}

// storageTransferOperation binds the original Q request to the complete immutable
// host plan in a closed 106-byte operation name.
func storageTransferOperation(operation storagebudget.ID, plan storagebudget.Digest) string {
	return "transfer:" + hex.EncodeToString(operation[:]) + ":" + hex.EncodeToString(plan[:])
}

func storageTransferOriginalRequest(r storageTransferRecord, catalog storageOwnerCatalogV2) (storagebudget.Request, error) {
	if r.Plan == (storagebudget.Digest{}) {
		return storagebudget.Request{}, storagebudget.ErrAuthority
	}
	plan, err := storageTransferPlanDigest(r)
	if err != nil || plan != r.Plan {
		return storagebudget.Request{}, errors.Join(storagebudget.ErrAuthority, err)
	}
	f, err := storageTransferReferences(r, catalog)
	if err != nil {
		return storagebudget.Request{}, err
	}
	request := storagebudget.Request{Workflow: storageTransferWorkflow, Operation: storageTransferOperation(r.Operation, r.Plan), Owner: r.Owner, Fanout: f, Components: make([]storagebudget.Component, len(r.Groups))}
	for i, g := range r.Groups {
		request.Components[i] = g.Component
	}
	return request, nil
}

// Immutable metadata controls are published only four times per operation,
// regardless of the number of creation/output ordinals. Six page publications
// carry Reserve, DataIntent, pointer/serving intent, CompletionIntent, Settled
// and Retired. A final observed body is mirrored before Settle; a partial body
// never permits replay or a refund. Publication groups have no retries.
//
// The Q schedule includes the possible pre-Settle checkpoint required to apply
// prior terminal deltas, in addition to the final retirement checkpoint. Source
// reads and readback bytes are bounded separately; they are not free effects.
func storageTransferControlPlan() []storageEnrollmentPeakGroup {
	out := make([]storageEnrollmentPeakGroup, 0, storageTransferControlGroups)
	for i := 0; i < 4; i++ {
		metadata := uint64(16)
		if i < 2 {
			metadata = 28
		}
		out = append(out, storageEnrollmentPeakGroup{ID: uint16(len(out) + 1), Parent: 1, Bytes: storageTransferBankBytes, MetadataOps: metadata})
	}
	for i := 0; i < 6; i++ {
		out = append(out, storageEnrollmentPeakGroup{ID: uint16(len(out) + 1), Parent: 1, Bytes: storageTransferPage, MetadataOps: 16})
	}
	out = append(out, storageEnrollmentPeakGroup{ID: 11, Parent: 2, Bytes: storagebudget.ManifestBytes, MetadataOps: 28})
	for i := 0; i < 3; i++ {
		out = append(out, storageEnrollmentPeakGroup{ID: uint16(12 + i), Parent: 2, Bytes: storagebudget.TicketBytes, MetadataOps: 28})
	}
	for i := 0; i < 2; i++ {
		out = append(out, storageEnrollmentPeakGroup{ID: uint16(15 + i), Parent: 2, Bytes: storagebudget.CheckpointBytes, MetadataOps: 28})
	}
	for i := 0; i < 7; i++ {
		out = append(out, storageEnrollmentPeakGroup{ID: uint16(len(out) + 1), Parent: 1, Bytes: storageIntentPage, MetadataOps: 28})
	}
	return out
}

func storageTransferControlPeaks() ([2]storagebudget.CapacityVector, error) {
	var out [2]storagebudget.CapacityVector
	for _, g := range storageTransferControlPlan() {
		cost, ok := storageEnrollmentPeakEstimate(g)
		if !ok || g.Parent < 1 || g.Parent > 2 {
			return out, storagebudget.ErrBounds
		}
		old := &out[g.Parent-1]
		if old.Bytes > ^uint64(0)-cost.Bytes || old.Inodes > ^uint64(0)-cost.Inodes {
			return out, storagebudget.ErrBounds
		}
		old.Bytes += cost.Bytes
		old.Inodes += cost.Inodes
	}
	return out, nil
}

// Each call returns one detached finite window. It cannot add a parent or
// consume another reservation. The caller seals the complete original parent
// vector before Reserve; future descendants must already have a matching
// operation-owned earlier creation receipt before their window is observed.
func storageTransferParentWindow(total, ordinal int) (first, last int, err error) {
	if total < 0 || total > storageTransferDescendantSlots-1+maxStorageFiles+storagebudget.MaxComponents || ordinal < 0 || ordinal >= (total+storageCapacityPassParents-1)/storageCapacityPassParents {
		return 0, 0, storagebudget.ErrBounds
	}
	first = ordinal * storageCapacityPassParents
	last = min(total, first+storageCapacityPassParents)
	return
}

func storageTransferCatalogDigest(catalog storageOwnerCatalogV2) (storagebudget.Digest, error) {
	raw, err := json.Marshal(catalog)
	if err != nil {
		return storagebudget.Digest{}, err
	}
	return storageEnrollmentPeakHash("desk-storage-whole-transfer-original-catalog-v1", raw), nil
}

// A transfer pass borrows the original epoch; it never registers a second
// owner/use retain or reacquires a domain below private. Its samples reuse the
// existing observer's native view and global policy derivation. It is not a
// generic storageCapacityPass authority and cannot escape into ordinary APIs.
type storageTransferCapacityPass struct {
	self               *storageTransferCapacityPass
	operation          *chatTransferOperation
	completion         *chatTransferCompletion
	started, completed time.Time
	ctx                context.Context
	cancel             context.CancelFunc
	roots              []*storageRootBinding
	parents            []storageTransferObservedParent
	data               storageCapacityPass // sample/derive data only; never generic Close/current
	closed             bool
}

type storageTransferObservedParent struct {
	binding *storageRootBinding
	root    *os.Root
	native  storageOwnerPersistentObject
	facts   storageEnrollmentNativeParentFacts
	// -1 is an original existing parent. Otherwise this exact earlier creation
	// ordinal must have returned its independently observed object/incarnation.
	descendant int
}

func (op *chatTransferOperation) beginOriginalTransferCapacityPass(ctx context.Context, parents []storageTransferObservedParent, completion *chatTransferCompletion) (out *storageTransferCapacityPass, err error) {
	if op == nil || op.self != op || ctx == nil || len(parents) > storageCapacityPassParents {
		return nil, storagebudget.ErrBounds
	}
	if completion == nil {
		if err = op.currentBeforeEffect(ctx); err != nil {
			return nil, err
		}
	} else {
		if completion.operation != op {
			return nil, storagebudget.ErrAuthority
		}
		if err = completion.current(ctx); err != nil {
			return nil, err
		}
	}
	p := &storageTransferCapacityPass{operation: op, completion: completion, started: time.Now(), parents: append([]storageTransferObservedParent{}, parents...), roots: append([]*storageRootBinding{}, op.preparation.epoch.originalRoots...)}
	p.self = p
	p.ctx, p.cancel = context.WithCancel(ctx)
	queryCtx, cancelQuery := context.WithTimeout(p.ctx, 250*time.Millisecond)
	defer cancelQuery()
	op.mu.Lock()
	if op.observation != nil || op.entered || op.finished {
		op.mu.Unlock()
		p.cancel()
		return nil, storagebudget.ErrBusy
	}
	op.observation = p
	op.mu.Unlock()
	defer func() {
		if err != nil {
			err = errors.Join(err, p.Close())
		}
	}()
	o := op.preparation.registry.owner
	a, ok := o.observer.(*storageAdvisoryObserver)
	if !ok || a.owner != o || len(p.roots) == 0 || len(p.roots) > storageCapacityPassRoots || len(a.constraints) > storageCapacityPassConstraints+1 {
		return nil, storagebudget.ErrAuthority
	}
	p.data = storageCapacityPass{owner: o, samples: map[*os.Root]storageFilesystemSample{}, maxAge: a.maxAge}
	for _, root := range p.roots {
		if err = root.currentForOriginalTransfer(queryCtx, op); err != nil {
			return nil, err
		}
		sample, e := p.query(queryCtx, a, root, root.root)
		if e != nil {
			return nil, e
		}
		p.data.samples[root.root] = sample
	}
	for _, parent := range p.parents {
		if parent.binding == nil || parent.binding.owner != o || o.roots[parent.binding.row.ID] != parent.binding || parent.root == nil || !parent.native.valid(1) {
			return nil, storagebudget.ErrAuthority
		}
		if parent.descendant < 0 {
			original := parent.root == parent.binding.root || op.path != nil && parent.root == op.path.root && parent.native == op.path.native
			if !original {
				return nil, storagebudget.ErrAuthority
			}
		} else {
			if parent.descendant >= len(op.record.Descendants) {
				return nil, storagebudget.ErrAuthority
			}
			receipt := op.returned.Descendants[parent.descendant]
			native := op.record.Basis
			native.Object, native.Incarnation = receipt.Object, receipt.Incarnation
			if receipt.State != 2 || parent.native != native {
				return nil, storagebudget.ErrAuthority
			}
		}
		native, e := storageOwnerPersistentRoot(queryCtx, parent.root)
		if e != nil || native != parent.native {
			return nil, errors.Join(errStorageCustody, e)
		}
		held, e := parent.root.Open(".")
		if e != nil {
			return nil, e
		}
		facts, e := storageEnrollmentNativeParentObservation(queryCtx, held)
		e = errors.Join(e, held.Close())
		if e != nil || facts != parent.facts {
			return nil, errors.Join(errStorageCustody, e)
		}
		sample, e := p.query(queryCtx, a, parent.binding, parent.root)
		if e != nil {
			return nil, e
		}
		p.data.samples[parent.root] = sample
	}
	if err = p.data.derive(a); err != nil {
		return nil, err
	}
	p.completed = time.Now()
	if err = errors.Join(p.current(), queryCtx.Err()); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *storageTransferCapacityPass) query(ctx context.Context, a *storageAdvisoryObserver, r *storageRootBinding, root *os.Root) (out storageFilesystemSample, err error) {
	original, ok := a.roots[r.row.ID]
	if !ok || storageOwnerRootValue(original.Definition) != r.row || !reflect.DeepEqual(original.References, r.references) {
		return out, storagebudget.ErrAuthority
	}
	filesystem := a.filesystems[original.Definition.Filesystem]
	out = storageFilesystemSample{Filesystem: filesystem.Definition, RootGeneration: original.Definition.Generation, CapacityCount: original.Definition.CapacityCount, CapacityRefs: original.Definition.CapacityRefs}
	for i := 0; i < int(out.CapacityCount); i++ {
		row, ok := a.constraints[out.CapacityRefs[i].Constraint]
		if !ok {
			return out, storagebudget.ErrUnknown
		}
		out.Constraints = append(out.Constraints, row.Definition)
	}
	held, err := root.Open(".")
	if err != nil {
		return out, err
	}
	native, err := storageAdvisoryNativeView(ctx, held, filesystem, original.Membership, a.constraints)
	err = errors.Join(err, held.Close())
	if err != nil {
		return out, err
	}
	out.PhysicalStatus, out.NativeAvailable, out.NativeTotalBytes, out.Observations = native.status, native.available, native.totalBytes, native.exact
	if err = p.operation.preparation.registry.owner.validateStorageSample(out); err != nil {
		return out, err
	}
	return out, nil
}

func (p *storageTransferCapacityPass) current() error {
	if p == nil || p.self != p || p.closed || p.operation == nil || p.completed.IsZero() {
		return storagebudget.ErrAuthority
	}
	if err := p.ctx.Err(); err != nil {
		return err
	}
	if time.Since(p.completed) < 0 || time.Since(p.completed) > p.data.maxAge {
		return storagebudget.ErrUnknown
	}
	if p.completion != nil {
		return p.completion.current(p.ctx)
	}
	return p.operation.currentBeforeEffect(p.ctx)
}
func (p *storageTransferCapacityPass) observations() ([]storagebudget.CapacityObservation, error) {
	if err := p.current(); err != nil {
		return nil, err
	}
	rows := map[storagebudget.ID]storagebudget.CapacityObservation{}
	for _, sample := range p.data.samples {
		for _, observation := range sample.Observations {
			old, ok := rows[observation.Constraint]
			if ok {
				if old.Generation != observation.Generation || old.KnownResources != observation.KnownResources {
					return nil, storagebudget.ErrStale
				}
				observation.Available.Bytes = min(old.Available.Bytes, observation.Available.Bytes)
				observation.Available.Inodes = min(old.Available.Inodes, observation.Available.Inodes)
				if old.At.Before(observation.At) {
					observation.At = old.At
				}
			}
			rows[observation.Constraint] = observation
		}
	}
	out := make([]storagebudget.CapacityObservation, 0, len(rows))
	for _, row := range rows {
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return bytes.Compare(out[i].Constraint[:], out[j].Constraint[:]) < 0 })
	return out, nil
}
func (p *storageTransferCapacityPass) Close() error {
	if p == nil || p.self != p {
		return storagebudget.ErrAuthority
	}
	if p.closed {
		return nil
	}
	p.closed = true
	p.cancel()
	op := p.operation
	op.mu.Lock()
	defer op.mu.Unlock()
	if op.observation != p {
		return storagebudget.ErrAuthority
	}
	op.observation = nil
	return nil
}

// Charge rows are constructed from the complete closed effect/control board.
// Physical/capacity quantities are summed once. Attribution uses only exact
// root/source reference algebra; equal per-workspace quantities form one group.
// This avoids both duplicated physical charge and uniform-union overcharging.
type storageTransferCharge struct {
	root                                                     storagebudget.ID
	category, references                                     byte
	physical, settlement, attribution, settlementAttribution storagebudget.Vector
	capacity                                                 storagebudget.CapacityVector
}
type storageTransferAttributionAmount struct{ ordinary, settlement storagebudget.Vector }

func storageTransferSelectedReferences(root, source []storagebudget.ID, selector byte) ([]storagebudget.ID, error) {
	if selector > storageTransferSymmetricDifferenceReferences {
		return nil, storagebudget.ErrBounds
	}
	a, b := map[storagebudget.ID]bool{}, map[storagebudget.ID]bool{}
	for _, id := range root {
		a[id] = true
	}
	for _, id := range source {
		b[id] = true
	}
	selected := map[storagebudget.ID]bool{}
	for id := range a {
		if selector == storageTransferRootReferences || selector == storageTransferUnionReferences || selector == storageTransferIntersectionReferences && b[id] || (selector == storageTransferRootOnlyReferences || selector == storageTransferSymmetricDifferenceReferences) && !b[id] {
			selected[id] = true
		}
	}
	for id := range b {
		if selector == storageTransferSourceReferences || selector == storageTransferUnionReferences || (selector == storageTransferSourceOnlyReferences || selector == storageTransferSymmetricDifferenceReferences) && !a[id] {
			selected[id] = true
		}
	}
	out := make([]storagebudget.ID, 0, len(selected))
	for id := range selected {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return bytes.Compare(out[i][:], out[j][:]) < 0 })
	return out, nil
}

func storageTransferBuildGroups(catalog storageOwnerCatalogV2, sourceNative storageOwnerPersistentObject, charges []storageTransferCharge) ([]storageTransferGroup, error) {
	if len(charges) > maxStorageFiles+storageTransferDescendantSlots-1+64 {
		return nil, storagebudget.ErrBounds
	}
	roots := map[storagebudget.ID]storageOwnerRoot{}
	filesystems := map[storagebudget.ID]storageOwnerFilesystem{}
	workspaces := map[storagebudget.ID]storagebudget.RegisteredWorkspace{}
	var source *storageOwnerRoot
	for i, root := range catalog.Roots {
		if int(root.DirectoryObject) >= len(catalog.Objects) {
			return nil, storagebudget.ErrBounds
		}
		roots[root.Definition.ID] = root
		if catalog.Objects[root.DirectoryObject].Persistent == sourceNative && !storageTransferCatalogView(root) {
			if source != nil {
				return nil, storagebudget.ErrAuthority
			}
			source = &catalog.Roots[i]
		}
	}
	if source == nil {
		return nil, storagebudget.ErrAuthority
	}
	for _, f := range catalog.Filesystems {
		filesystems[f.Definition.ID] = f
	}
	for _, w := range catalog.Workspaces {
		workspaces[w.Definition.ID] = w.Definition
	}
	type key struct {
		root     storagebudget.ID
		category byte
	}
	type amounts struct {
		physical, settlement storagebudget.Vector
		capacity             storagebudget.CapacityVector
		selectors            [8]storageTransferAttributionAmount
	}
	sums := map[key]*amounts{}
	for _, charge := range charges {
		root, ok := roots[charge.root]
		if !ok || charge.category >= storagebudget.Categories || charge.references > storageTransferSymmetricDifferenceReferences || charge.references == storageTransferNoReferences && (charge.attribution != (storagebudget.Vector{}) || charge.settlementAttribution != (storagebudget.Vector{})) {
			return nil, storagebudget.ErrAuthority
		}
		_ = root
		k := key{charge.root, charge.category}
		sum := sums[k]
		if sum == nil {
			sum = &amounts{}
			sums[k] = sum
		}
		var err error
		sum.physical, err = storageVectorAdd(sum.physical, charge.physical)
		if err != nil {
			return nil, err
		}
		sum.settlement, err = storageVectorAdd(sum.settlement, charge.settlement)
		if err != nil {
			return nil, err
		}
		if charge.capacity.Bytes > 1<<60 || charge.capacity.Inodes > 1<<40 || sum.capacity.Bytes > 1<<60-charge.capacity.Bytes || sum.capacity.Inodes > 1<<40-charge.capacity.Inodes {
			return nil, storagebudget.ErrBounds
		}
		sum.capacity.Bytes += charge.capacity.Bytes
		sum.capacity.Inodes += charge.capacity.Inodes
		selected := &sum.selectors[charge.references]
		selected.ordinary, err = storageVectorAdd(selected.ordinary, charge.attribution)
		if err != nil {
			return nil, err
		}
		selected.settlement, err = storageVectorAdd(selected.settlement, charge.settlementAttribution)
		if err != nil {
			return nil, err
		}
	}
	catalogDigest, err := storageTransferCatalogDigest(catalog)
	if err != nil {
		return nil, err
	}
	out := []storageTransferGroup{}
	for k, sum := range sums {
		root := roots[k.root]
		filesystem, ok := filesystems[root.Definition.Filesystem]
		if !ok {
			return nil, storagebudget.ErrAuthority
		}
		quantities := map[storagebudget.ID]storageTransferAttributionAmount{}
		for selector, amount := range sum.selectors {
			if amount == (storageTransferAttributionAmount{}) {
				continue
			}
			ids, err := storageTransferSelectedReferences(root.References, source.References, byte(selector))
			if err != nil {
				return nil, err
			}
			if len(ids) == 0 {
				continue
			}
			for _, id := range ids {
				if workspaces[id].Generation == 0 {
					return nil, storagebudget.ErrAuthority
				}
				value := quantities[id]
				value.ordinary, err = storageVectorAdd(value.ordinary, amount.ordinary)
				if err != nil {
					return nil, err
				}
				value.settlement, err = storageVectorAdd(value.settlement, amount.settlement)
				if err != nil {
					return nil, err
				}
				quantities[id] = value
			}
		}
		partitions := map[storageTransferAttributionAmount][]storagebudget.ID{}
		for id, amount := range quantities {
			partitions[amount] = append(partitions[amount], id)
		}
		groupStart := len(out)
		for amount, ids := range partitions {
			sort.Slice(ids, func(i, j int) bool { return bytes.Compare(ids[i][:], ids[j][:]) < 0 })
			selector := byte(255)
			for candidate := byte(1); candidate <= storageTransferSymmetricDifferenceReferences; candidate++ {
				selected, e := storageTransferSelectedReferences(root.References, source.References, candidate)
				if e != nil {
					return nil, e
				}
				if reflect.DeepEqual(ids, selected) {
					selector = candidate
					break
				}
			}
			if selector == 255 {
				return nil, storagebudget.ErrBounds
			}
			component := storagebudget.Component{Filesystem: filesystem.Definition.ID, FilesystemGeneration: filesystem.Definition.Generation, Root: root.Definition.ID, RootGeneration: root.Definition.Generation, Owner: root.Definition.Owner, Category: k.category, Attribution: amount.ordinary, SettlementAttribution: amount.settlement, CapacityCount: root.Definition.CapacityCount, CapacityBinding: root.Definition.CapacityBinding}
			for i := 0; i < int(component.CapacityCount); i++ {
				ref := root.Definition.CapacityRefs[i]
				component.Capacity[i] = storagebudget.CapacityPromise{Constraint: ref.Constraint, Generation: ref.Generation}
			}
			out = append(out, storageTransferGroup{Component: component, RootIdentity: root.Definition.Identity, FilesystemIdentity: filesystem.Definition.Identity, Locator: root.Definition.Locator, Native: catalog.Objects[root.DirectoryObject].Persistent, Membership: storageTransferMembershipDigest(catalogDigest, component, ids, workspaces), Flags: selector})
		}
		if groupStart == len(out) {
			if sum.physical == (storagebudget.Vector{}) && sum.settlement == (storagebudget.Vector{}) {
				if sum.capacity != (storagebudget.CapacityVector{}) {
					return nil, storagebudget.ErrBounds
				}
				continue
			}
			component := storagebudget.Component{Filesystem: filesystem.Definition.ID, FilesystemGeneration: filesystem.Definition.Generation, Root: root.Definition.ID, RootGeneration: root.Definition.Generation, Owner: root.Definition.Owner, Category: k.category, CapacityCount: root.Definition.CapacityCount, CapacityBinding: root.Definition.CapacityBinding}
			for i := 0; i < int(component.CapacityCount); i++ {
				ref := root.Definition.CapacityRefs[i]
				component.Capacity[i] = storagebudget.CapacityPromise{Constraint: ref.Constraint, Generation: ref.Generation}
			}
			out = append(out, storageTransferGroup{Component: component, RootIdentity: root.Definition.Identity, FilesystemIdentity: filesystem.Definition.Identity, Locator: root.Definition.Locator, Native: catalog.Objects[root.DirectoryObject].Persistent, Membership: storageTransferMembershipDigest(catalogDigest, component, []storagebudget.ID{}, workspaces), Flags: storageTransferNoReferences})
		}
		// Assign physical/capacity once to the lowest canonical selector. Other
		// components retain their exact independent attribution and zero capacity.
		selected := groupStart
		for i := groupStart + 1; i < len(out); i++ {
			if out[i].Flags < out[selected].Flags {
				selected = i
			}
		}
		physical := &out[selected].Component
		physical.Physical, physical.SettlementPhysical = sum.physical, sum.settlement
		if sum.physical == (storagebudget.Vector{}) && sum.settlement == (storagebudget.Vector{}) && sum.capacity != (storagebudget.CapacityVector{}) {
			return nil, storagebudget.ErrBounds
		}
		for i := 0; i < int(physical.CapacityCount); i++ {
			axis := catalogConstraintResources(catalog, physical.Capacity[i].Constraint)
			if axis == 0 {
				return nil, storagebudget.ErrAuthority
			}
			if axis&storagebudget.CapacityBytes != 0 {
				physical.Capacity[i].Peak.Bytes = sum.capacity.Bytes
			}
			if axis&storagebudget.CapacityInodes != 0 {
				physical.Capacity[i].Peak.Inodes = sum.capacity.Inodes
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if n := bytes.Compare(a.Component.Root[:], b.Component.Root[:]); n != 0 {
			return n < 0
		}
		if a.Component.Category != b.Component.Category {
			return a.Component.Category < b.Component.Category
		}
		return a.Flags < b.Flags
	})
	if len(out) == 0 || len(out) > storagebudget.MaxComponents {
		return nil, storagebudget.ErrBounds
	}
	return out, nil
}
func catalogConstraintResources(catalog storageOwnerCatalogV2, id storagebudget.ID) uint8 {
	for _, row := range catalog.Constraints {
		if row.Definition.ID == id {
			return row.Definition.Resources
		}
	}
	return 0
}

// storage_budget_transfer_selection.go — distinct closed transfer role.
// Normalization binds one observed fact independently of later host headers.
// It does not turn a decoded record into authority or retry a Q result.
// Generation/previous-page below are comparison placeholders only: Evidence
// explicitly normalizes them. They never create or select a host page.
func storageTransferSettlementEvidence(r storageTransferRecord, catalog storageOwnerCatalogV2) (storagebudget.Digest, error) {
	if r.Phase < storageTransferObserved || r.Phase > storageTransferRetired {
		return storagebudget.Digest{}, storagebudget.ErrAuthority
	}
	r.Phase = storageTransferObserved
	r.Token = r.InitialToken
	r.Token.Generation++
	core, err := storageTransferProjectIntent(r, catalog, r.HostSlot, max(uint64(2), r.HostGeneration), storagebudget.Digest{1}, storageIntentSettling)
	if err != nil {
		return storagebudget.Digest{}, err
	}
	return core.Evidence, nil
}

func storageTransferFinalTarget(r storageTransferRecord) (storageOwnerPersistentObject, error) {
	if len(r.Descendants) == 0 {
		if !r.TargetBefore.valid(1) {
			return storageOwnerPersistentObject{}, storagebudget.ErrUnknown
		}
		return r.TargetBefore, nil
	}
	d := r.Descendants[len(r.Descendants)-1]
	if d.State != 2 || d.Object == 0 {
		return storageOwnerPersistentObject{}, storagebudget.ErrUnknown
	}
	n := r.Basis
	n.Object = d.Object
	n.Incarnation = d.Incarnation
	if !n.valid(1) {
		return storageOwnerPersistentObject{}, storagebudget.ErrUnknown
	}
	return n, nil
}

func storageTransferResolvedLocation(raw []byte, settingsPath string) (dataLocation, error) {
	var l dataLocation
	if len(raw) == 0 || len(raw) > storageTransferPointerBytes || decodeDataJSON(raw, &l) != nil || l.Version != 1 || l.Path == "" {
		return l, storagebudget.ErrBounds
	}
	for _, value := range []*string{&l.Path, &l.Previous} {
		if *value == "" {
			continue
		}
		if !filepath.IsAbs(*value) {
			if !filepath.IsLocal(*value) {
				return dataLocation{}, storagebudget.ErrBounds
			}
			*value = filepath.Join(settingsPath, *value)
		}
		if filepath.Clean(*value) != *value {
			return dataLocation{}, storagebudget.ErrBounds
		}
	}
	return l, nil
}

// Compare only one closed old-to-new data selection. The source remains a full
// retained row and all prior definitions/references survive. Native custody and
// exact raw pointer readback belong to the original live issuer, separately.
func storageTransferSelectionRelation(r storageTransferRecord, before, after storageOwnerCatalogV2) error {
	if storageTransferRecordShape(r) != nil || r.Mode == storageTransferExport || r.Phase != storageTransferRetired || r.PointerState != 2 || r.Installation != before.Installation || r.Installation != after.Installation || r.QBinding != before.QBinding || r.QBinding != after.QBinding || before.Policy != after.Policy || !storageTransferCatalogRetains(r, before, after) {
		return storagebudget.ErrAuthority
	}
	original, err := storageTransferCatalogDigest(before)
	if err != nil || original != r.Catalog {
		return errors.Join(storagebudget.ErrAuthority, err)
	}
	target, err := storageTransferFinalTarget(r)
	if err != nil {
		return err
	}
	settingsPath, err := storageTransferNativePath(before, r.Settings)
	if err != nil {
		return err
	}
	next, err := storageTransferResolvedLocation(r.NewPointer, settingsPath)
	if err != nil {
		return err
	}
	old := dataLocation{}
	if !r.OldAbsent {
		old, err = storageTransferResolvedLocation(r.OldPointer, settingsPath)
		if err != nil {
			return err
		}
	}
	return storageTransferSelectionMetadata(r, target, before, after, old, next)
}

func storageTransferNativePath(c storageOwnerCatalogV2, native storageOwnerPersistentObject) (string, error) {
	path := ""
	for _, root := range c.Roots {
		if int(root.DirectoryObject) >= len(c.Objects) {
			return "", storagebudget.ErrBounds
		}
		if c.Objects[root.DirectoryObject].Persistent == native && !storageTransferCatalogView(root) {
			if path != "" {
				return "", storagebudget.ErrAuthority
			}
			path = string(root.Source.Path)
		}
	}
	if path == "" {
		return "", storagebudget.ErrAuthority
	}
	return path, nil
}

// Detached catalog relation only. The caller must separately prove original
// native execution, retirement/applied Q and retained P origin. Resolved paths
// are compared directly: re-encoding expanded contained paths is not an
// original pointer byte image and cannot impose another JSON byte limit.
func storageTransferSelectionMetadata(r storageTransferRecord, target storageOwnerPersistentObject, before, after storageOwnerCatalogV2, old, next dataLocation) error {
	if r.Mode == storageTransferExport || r.Installation != before.Installation || r.Installation != after.Installation || r.QBinding != before.QBinding || r.QBinding != after.QBinding || before.Policy != after.Policy || !storageTransferCatalogRetains(r, before, after) {
		return storagebudget.ErrAuthority
	}
	a, b := before.OriginalOwner, after.OriginalOwner
	if a.Kind == 2 {
		if !storageEnrollmentOriginalRegistryEqual(before, after) {
			return storagebudget.ErrAuthority
		}
		a.RegistryControl = 0
		b.RegistryControl = 0
	}
	if !reflect.DeepEqual(a, b) {
		return storagebudget.ErrAuthority
	}
	for _, old := range before.Profiles {
		found := false
		for _, next := range after.Profiles {
			if old.ID == next.ID {
				if old != next {
					return storagebudget.ErrAuthority
				}
				found = true
			}
		}
		if !found {
			return storagebudget.ErrAuthority
		}
	}
	settingsPath := ""
	sourcePath := ""
	for _, root := range before.Roots {
		if int(root.DirectoryObject) >= len(before.Objects) {
			return storagebudget.ErrBounds
		}
		native := before.Objects[root.DirectoryObject].Persistent
		if native == r.Settings {
			if settingsPath != "" && settingsPath != string(root.Source.Path) {
				return storagebudget.ErrAuthority
			}
			settingsPath = string(root.Source.Path)
		}
		if native == r.Source {
			if sourcePath != "" && sourcePath != string(root.Source.Path) {
				return storagebudget.ErrAuthority
			}
			sourcePath = string(root.Source.Path)
		}
	}
	if settingsPath == "" || sourcePath == "" {
		return storagebudget.ErrAuthority
	}
	if next.Version != 1 || next.Previous != sourcePath || !filepath.IsAbs(next.Path) || filepath.Clean(next.Path) != next.Path {
		return storagebudget.ErrAuthority
	}
	oldIndex, newIndex := -1, -1
	for i, control := range before.OwnerControls {
		if int(control.ParentObject) >= len(before.Objects) {
			return storagebudget.ErrBounds
		}
		if before.Objects[control.ParentObject].Persistent == r.Settings && string(control.Name) == dataLocationName {
			if oldIndex >= 0 {
				return storagebudget.ErrBounds
			}
			oldIndex = i
		}
	}
	for i, control := range after.OwnerControls {
		if int(control.ParentObject) >= len(after.Objects) {
			return storagebudget.ErrBounds
		}
		if after.Objects[control.ParentObject].Persistent == r.Settings && string(control.Name) == dataLocationName {
			if newIndex >= 0 {
				return storagebudget.ErrBounds
			}
			newIndex = i
		}
	}
	if newIndex < 0 || (oldIndex < 0) != r.OldAbsent || len(after.OwnerControls) != len(before.OwnerControls)+func() int {
		if oldIndex < 0 {
			return 1
		}
		return 0
	}() {
		return storagebudget.ErrAuthority
	}
	selected := after.OwnerControls[newIndex]
	if selected.Role != 2 || selected.MaxBytes != 16384 || selected.Location.Version != 1 || string(selected.Location.Path) != next.Path || string(selected.Location.Previous) != next.Previous || selected.Location.Selected != target || selected.Location.PreviousSelected != r.Source {
		return storagebudget.ErrAuthority
	}
	if oldIndex >= 0 {
		prior := before.OwnerControls[oldIndex]
		if old.Version != 1 || prior.Role != selected.Role || prior.MaxBytes != selected.MaxBytes || prior.OwnerKind != selected.OwnerKind || !bytes.Equal(prior.OwnerKey, selected.OwnerKey) || string(prior.Location.Path) != old.Path || string(prior.Location.Previous) != old.Previous || prior.Location.Selected != r.Source {
			return storagebudget.ErrAuthority
		}
	}
	// There is exactly one mutable control. Every unrelated complete relation is
	// still byte-exact, including original replaceable-selection observations.
	for i, control := range before.OwnerControls {
		if i == oldIndex {
			continue
		}
		found := false
		for _, candidate := range after.OwnerControls {
			if before.Objects[control.ParentObject].Persistent == after.Objects[candidate.ParentObject].Persistent && bytes.Equal(control.Name, candidate.Name) {
				a, e := before.controlDigest(control)
				b, f := after.controlDigest(candidate)
				if e != nil || f != nil || a != b || control.RelationDigest != candidate.RelationDigest || control.MaxBytes != candidate.MaxBytes {
					return storagebudget.ErrAuthority
				}
				found = true
			}
		}
		if !found {
			return storagebudget.ErrAuthority
		}
	}
	for _, prior := range before.Roots {
		found := false
		for _, candidate := range after.Roots {
			if prior.Definition.ID != candidate.Definition.ID {
				continue
			}
			a, e := before.sourceDigest(prior)
			b, f := after.sourceDigest(candidate)
			if e != nil || f != nil || a != b || prior.Definition != candidate.Definition || prior.Role != candidate.Role || prior.Category != candidate.Category || !storageTransferRootReferencesRetained(r, before, prior, candidate) {
				return storagebudget.ErrAuthority
			}
			found = true
		}
		if !found {
			return storagebudget.ErrAuthority
		}
	}
	found := false
	for _, root := range after.Roots {
		if int(root.DirectoryObject) >= len(after.Objects) {
			return storagebudget.ErrBounds
		}
		if storageTransferControlRoot(after, root, uint32(newIndex)) && (root.Source.ControlKind == 0 || root.Source.ControlSelection == 1) && string(root.Source.Path) == next.Path && after.Objects[root.DirectoryObject].Persistent == target {
			if found {
				return storagebudget.ErrBounds
			}
			found = true
		}
	}
	if !found {
		return storagebudget.ErrAuthority
	}
	return nil
}

// This separate builder does not relax the ordinary append-only constructor.
// A version3 projection is emitted only after the exact transfer relation was
// compared; old readers reject its explicit version before expanding rows.
func storageEnrollmentTransferBeforeProjection(before, after storageOwnerCatalogV2, raw []byte, r storageTransferRecord) (storageEnrollmentPeakProjection, error) {
	if err := storageTransferSelectionRelation(r, before, after); err != nil {
		return storageEnrollmentPeakProjection{}, err
	}
	return storageTransferHistoricalProjection(before, after, raw)
}

// This builder reconstructs historical comparison material only. Native
// original endpoint/Q/custody validation remains at the P issuer and reader.
func storageTransferHistoricalProjection(before, after storageOwnerCatalogV2, raw []byte) (storageEnrollmentPeakProjection, error) {
	var out storageEnrollmentPeakProjection
	if len(raw) != storageOwnerCatalogBytes || before.Installation != after.Installation || before.QBinding != after.QBinding || before.Policy != after.Policy {
		return out, storagebudget.ErrAuthority
	}
	out = storageEnrollmentPeakProjection{Kind: 0, Catalog: before, SavedOuter: append([]byte{}, raw[:64]...), TransferPrior: &storageEnrollmentTransferPriorControl{}}
	if string(raw[:8]) == "DSKONP1\x00" {
		out.Kind = 1
		out.OriginalRef = 65535
		out.SavedOuter = append([]byte{}, raw[:8320]...)
	}
	old, next := storageEnrollmentPeakDictionaries(before), storageEnrollmentPeakDictionaries(after)
	for k := range old {
		out.Select[k] = []uint16{}
		for i := 0; i < old[k].Len(); i++ {
			found := -1
			for j := 0; j < next[k].Len(); j++ {
				match := false
				switch k {
				case 0:
					match = reflect.DeepEqual(before.objectMaterial(uint32(i)), after.objectMaterial(uint32(j)))
				case 1:
					a, b := before.OwnerControls[i], after.OwnerControls[j]
					match = before.Objects[a.ParentObject].Persistent == after.Objects[b.ParentObject].Persistent && bytes.Equal(a.Name, b.Name) && a.Role == b.Role && a.OwnerKind == b.OwnerKind && bytes.Equal(a.OwnerKey, b.OwnerKey) && a.MaxBytes == b.MaxBytes
					if match && a.RelationDigest != b.RelationDigest {
						if a.Role != 2 || string(a.Name) != dataLocationName {
							return out, storagebudget.ErrAuthority
						}
						location := storageEnrollmentTransferPriorLocation{Control: uint16(i), Selected: 65535, Previous: 65535}
						for index, root := range before.Roots {
							if !storageTransferControlRoot(before, root, uint32(i)) {
								continue
							}
							native := before.Objects[root.DirectoryObject].Persistent
							if bytes.Equal(root.Source.Path, a.Location.Path) && native == a.Location.Selected {
								if location.Selected != 65535 {
									return out, storagebudget.ErrBounds
								}
								location.Selected = uint16(index)
							}
							if len(a.Location.Previous) > 0 && bytes.Equal(root.Source.Path, a.Location.Previous) && native == a.Location.PreviousSelected {
								if location.Previous != 65535 {
									return out, storagebudget.ErrBounds
								}
								location.Previous = uint16(index)
							}
						}
						if location.Selected == 65535 || len(a.Location.Previous) > 0 && location.Previous == 65535 {
							return out, storagebudget.ErrBounds
						}
						out.TransferPrior.Controls = append(out.TransferPrior.Controls, location)
					}
				case 2:
					a, b := before.Roots[i], after.Roots[j]
					match = a.Definition.ID == b.Definition.ID
					if match {
						if a.Definition != b.Definition || a.Role != b.Role || a.Category != b.Category {
							return out, storagebudget.ErrAuthority
						}
						ad, e := before.sourceDigest(a)
						bd, f := after.sourceDigest(b)
						if e != nil || f != nil || ad != bd {
							return out, storagebudget.ErrAuthority
						}
					}
				default:
					match = reflect.DeepEqual(old[k].Index(i).Interface(), next[k].Index(j).Interface())
				}
				if match {
					if found >= 0 {
						return out, storagebudget.ErrBounds
					}
					found = j
				}
			}
			if found < 0 {
				return out, storagebudget.ErrStale
			}
			out.Select[k] = append(out.Select[k], uint16(found))
		}
	}
	// Current canonical reference edges are (root dictionary order, sorted refs).
	// Existing transfer membership is union-only; removed/foreign refs refuse.
	oldRoots := map[storagebudget.ID]storageOwnerRoot{}
	for _, row := range before.Roots {
		oldRoots[row.Definition.ID] = row
	}
	edge := 0
	retained := map[storagebudget.ID]int{}
	for _, row := range after.Roots {
		prior, projected := oldRoots[row.Definition.ID]
		for _, id := range row.References {
			if edge >= 65536 {
				return out, storagebudget.ErrBounds
			}
			if projected {
				for _, oldID := range prior.References {
					if oldID == id {
						out.TransferPrior.References[edge/8] |= 1 << uint(edge%8)
						retained[row.Definition.ID]++
					}
				}
			}
			edge++
		}
	}
	for ordinal, row := range before.Roots {
		if retained[row.Definition.ID] != len(row.References) {
			return out, storagebudget.ErrAuthority
		}
		if storageTransferCatalogView(row) {
			if row.Source.ControlSelection == 4 {
				out.TransferPrior.Inactive[ordinal/8] |= 1 << uint(ordinal%8)
			} else if row.Source.ControlSelection != 0 {
				return out, storagebudget.ErrBounds
			}
		}
	}
	return out, nil
}
func storageTransferProjectionDefinition(c storageOwnerCatalogV2, kind int, index uint16) (storagebudget.ID, bool) {
	switch kind {
	case 0:
		if int(index) < len(c.Roots) {
			return c.Roots[index].Definition.ID, true
		}
	case 1:
		if int(index) < len(c.Workspaces) {
			return c.Workspaces[index].Definition.ID, true
		}
	case 2:
		if int(index) < len(c.Filesystems) {
			return c.Filesystems[index].Definition.ID, true
		}
	case 3:
		if int(index) < len(c.Constraints) {
			return c.Constraints[index].Definition.ID, true
		}
	}
	return storagebudget.ID{}, false
}
func storageTransferProjectionDefinitionIndex(c storageOwnerCatalogV2, kind int, id storagebudget.ID) (uint16, bool) {
	size := 0
	switch kind {
	case 0:
		size = len(c.Roots)
	case 1:
		size = len(c.Workspaces)
	case 2:
		size = len(c.Filesystems)
	case 3:
		size = len(c.Constraints)
	default:
		return 0, false
	}
	found := -1
	for i := 0; i < size; i++ {
		candidate, ok := storageTransferProjectionDefinition(c, kind, uint16(i))
		if ok && candidate == id {
			if found >= 0 {
				return 0, false
			}
			found = i
		}
	}
	return uint16(found), found >= 0
}

// Applied tuples are compared independently of host phase text. The live
// issuer additionally calls Q.ValidateAppliedRequest against this same retained
// catalog/request, which reconstructs its manifest without reading retired
// manifest storage. No uncertain or merely replayed terminal permits cutover.
func storageTransferApplied(r storageTransferRecord, catalog storageOwnerCatalogV2, state storagebudget.HostState) bool {
	if storageTransferRecordShape(r) != nil || r.Phase != storageTransferRetired || int(r.Token.Slot) >= len(state.Slots) || state.Maintenance {
		return false
	}
	core, err := storageTransferProjectIntent(r, catalog, r.HostSlot, max(uint64(2), r.HostGeneration), storagebudget.Digest{1}, storageIntentRetired)
	if err != nil {
		return false
	}
	f, err := storageTransferCatalogIntentFanout(core, catalog)
	return err == nil && storageIntentFanoutAppliedOperand(core, r.Installation, r.QBinding, state.Slots[r.Token.Slot], f)
}

func storageTransferRootReferencesRetained(r storageTransferRecord, catalog storageOwnerCatalogV2, old, next storageOwnerRoot) bool {
	if reflect.DeepEqual(old.References, next.References) {
		return true
	}
	if !r.TargetEmpty || !r.TargetBefore.valid(1) || int(old.DirectoryObject) >= len(catalog.Objects) || catalog.Objects[old.DirectoryObject].Persistent != r.TargetBefore {
		return false
	}
	set := map[storagebudget.ID]bool{}
	for _, id := range old.References {
		set[id] = true
	}
	found := false
	for _, root := range catalog.Roots {
		if int(root.DirectoryObject) >= len(catalog.Objects) {
			return false
		}
		if !storageTransferCatalogView(root) && catalog.Objects[root.DirectoryObject].Persistent == r.Source {
			if found {
				return false
			}
			found = true
			for _, id := range root.References {
				set[id] = true
			}
		}
	}
	if !found {
		return false
	}
	ids := make([]storagebudget.ID, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return bytes.Compare(ids[i][:], ids[j][:]) < 0 })
	return reflect.DeepEqual(ids, next.References)
}
func storageTransferCatalogRetains(r storageTransferRecord, before, after storageOwnerCatalogV2) bool {
	copyAfter := after
	copyAfter.Roots = append([]storageOwnerRoot{}, after.Roots...)
	for i, next := range copyAfter.Roots {
		for _, old := range before.Roots {
			if old.Definition.ID != next.Definition.ID {
				continue
			}
			if !storageTransferRootReferencesRetained(r, before, old, next) {
				return false
			}
			copyAfter.Roots[i].References = old.References
		}
	}
	return storageOwnerCatalogRetains(before, copyAfter)
}

// HINT9 is a closed projection of ONE whole-transfer operation into the
// existing host slot, not another reservation. The full banks own the live
// ordinal/result inspection. P retains this complete projection and its
// original Q view after those banks are legitimately reused.
const storageTransferIntentBytes = 2052 // exactly the two old path slots
const storageTransferTailMeaningfulBytes = 1316
const storageTransferIntentMeaningfulBytes = 1866 // old1751 + old-pointer68 + cleanup47; reserved186

type storageIntentTransfer struct {
	Mode                                 storageTransferMode
	Source                               storageOwnerPersistentObject
	Catalog                              storagebudget.Digest
	Initial                              storagebudget.Token
	Membership                           storagebudget.Digest
	Selectors                            [storagebudget.MaxComponents]byte
	Observed, Selection                  storagebudget.Digest
	Settings, Selected, Marker, Location storageOwnerPersistentObject
	LocationPayload                      storagebudget.Digest
	Existing                             storagebudget.Vector
	PointerState                         byte
	Empty                                bool
	Tail                                 storageCreationTail
	Workspace                            storagebudget.ID
	WorkspaceGeneration                  uint64
	// Compact suffix at byte1751: digest32/native35/absence1/cleanup47.
	// Positive non-export retains original pointer identity but zero cleanup;
	// rejected retains immutable old-pointer facts; export suffix is zero. Remaining186 bytes are zero.
	OldPointerDigest storagebudget.Digest
	OldPointerNative storageOwnerPersistentObject
	OldAbsent        bool
	Cleanup          storageTransferCleanup
}

func storageTransferIntentCodec(c *storageIntentCodec, t *storageIntentTransfer, read bool) bool {
	start := c.at
	if read {
		t.Mode = storageTransferMode(c.get(1))
	} else {
		c.put(uint64(t.Mode), 1)
	}
	storageTransferNative(c, &t.Source, read)
	c.digest(&t.Catalog, read)
	c.token(&t.Initial, read)
	c.digest(&t.Membership, read)
	if read {
		c.take(t.Selectors[:])
	} else {
		c.raw(t.Selectors[:])
	}
	c.digest(&t.Observed, read)
	c.digest(&t.Selection, read)
	for _, n := range []*storageOwnerPersistentObject{&t.Settings, &t.Selected, &t.Marker, &t.Location} {
		storageTransferNative(c, n, read)
	}
	c.digest(&t.LocationPayload, read)
	c.vector(&t.Existing, read)
	if read {
		t.PointerState = byte(c.get(1))
	} else {
		c.put(uint64(t.PointerState), 1)
	}
	c.boolean(&t.Empty, read)
	full := make([]byte, storageCreationTailBytes)
	if read {
		c.take(full[:storageTransferTailMeaningfulBytes])
		if t.Mode == storageTransferExport {
			if !storageAllZero(full) {
				return false
			}
			t.Tail = storageCreationTail{}
		} else if !storageCreationTailCodec(full, &t.Tail, true) || t.Tail.validate() != nil {
			return false
		}
	} else {
		if t.Mode != storageTransferExport {
			if t.Tail.validate() != nil || !storageCreationTailCodec(full, &t.Tail, false) {
				return false
			}
		} else if !reflect.DeepEqual(t.Tail, storageCreationTail{}) {
			return false
		}
		// The full legacy tail's entire omitted suffix must be canonical zero.
		if !storageAllZero(full[storageTransferTailMeaningfulBytes:]) {
			return false
		}
		c.raw(full[:storageTransferTailMeaningfulBytes])
	}
	c.id(&t.Workspace, read)
	c.number(&t.WorkspaceGeneration, read)
	c.digest(&t.OldPointerDigest, read)
	storageTransferNative(c, &t.OldPointerNative, read)
	c.boolean(&t.OldAbsent, read)
	storageTransferCleanupCodec(c, &t.Cleanup, read)
	if c.at-start != storageTransferIntentMeaningfulBytes {
		return false
	}
	c.skip(storageTransferIntentBytes-storageTransferIntentMeaningfulBytes, read)
	return !c.bad
}

func storageTransferIntentSelection(r storageIntentRecord) storagebudget.Digest {
	if r.Transfer == nil {
		return storagebudget.Digest{}
	}
	t := r.Transfer
	// A detached commitment only. Original applied Q, native bank custody,
	// actual census and the exact source/control relation remain separate joins.
	v := struct {
		Plan                                         storagebudget.Digest
		Mode                                         storageTransferMode
		Source, Settings, Selected, Marker, Location storageOwnerPersistentObject
		Payload                                      storagebudget.Digest
		Existing                                     storagebudget.Vector
		Pointer                                      byte
		Empty                                        bool
		Workspace                                    storagebudget.ID
		Generation                                   uint64
		OldPointerDigest                             storagebudget.Digest
		OldPointerNative                             storageOwnerPersistentObject
		OldAbsent                                    bool
		Cleanup                                      storageTransferCleanup
	}{r.CASBinding, t.Mode, t.Source, t.Settings, t.Selected, t.Marker, t.Location, t.LocationPayload, t.Existing, t.PointerState, t.Empty, t.Workspace, t.WorkspaceGeneration, t.OldPointerDigest, t.OldPointerNative, t.OldAbsent, t.Cleanup}
	raw, err := json.Marshal(v)
	if err != nil {
		return storagebudget.Digest{}
	}
	return storageEnrollmentPeakHash("desk-storage-transfer-selection-observation-v1", raw)
}

func storageTransferIntentEvidence(r storageIntentRecord) storagebudget.Digest {
	if r.Transfer == nil {
		return storagebudget.Digest{}
	}
	r.Generation = 0
	r.Phase = 0
	r.PreviousPageDigest = storagebudget.Digest{}
	r.RetirementEpoch = 0
	r.Evidence = storagebudget.Digest{}
	r.Token = r.Transfer.Initial
	r.Token.Generation++
	r.PreviousToken = r.Transfer.Initial
	raw := make([]byte, storageIntentPage)
	if !storageIntentBodyCodec(raw, &r, false) {
		return storagebudget.Digest{}
	}
	return storageEnrollmentPeakHash("desk-storage-transfer-observed-settlement-v1", raw[128:r.encodedLength()])
}

func storageTransferIntentValid(r storageIntentRecord) error {
	t := r.Transfer
	if t == nil || r.Fanout != nil || t.Mode < storageTransferMove || t.Mode > storageTransferExport || !t.Source.valid(1) || !t.Settings.valid(1) || t.Catalog == (storagebudget.Digest{}) || t.Membership == (storagebudget.Digest{}) || t.Workspace == (storagebudget.ID{}) || t.WorkspaceGeneration == 0 || t.PointerState > 2 || !storageIntentVector(t.Existing) {
		return storagebudget.ErrUnknown
	}
	if r.Phase < storageIntentPrepared || r.Phase > storageIntentRejected || r.Slot >= storagebudget.Slots || r.Generation == 0 || r.OperationID == (storagebudget.ID{}) || r.CASBinding == (storagebudget.Digest{}) || r.AdmissionEpoch == 0 || r.NewLength > 1<<60 || r.Target != "" || r.Stage != "" || r.Workflow != storageTransferWorkflow || r.Operation != storageTransferOperation(r.OperationID, r.CASBinding) || r.OldExists || r.OldIdentity != (storageDirectoryIdentity{}) || r.OldLength != 0 || r.OldLinkCount != 0 || r.StageExists || r.StageIdentity != (storageDirectoryIdentity{}) {
		return storagebudget.ErrUnknown
	}
	if r.Generation == 1 && (r.Phase != storageIntentPrepared || r.PreviousPageDigest != (storagebudget.Digest{})) || r.Generation > 1 && r.PreviousPageDigest == (storagebudget.Digest{}) {
		return storagebudget.ErrUnknown
	}
	n := len(r.Components)
	if n < 1 || n > storagebudget.MaxComponents || len(r.RootIdentities) != n || len(r.FilesystemIdentities) != n || len(r.Locators) != n || len(r.Changes) != n {
		return storagebudget.ErrUnknown
	}
	for i, c := range r.Components {
		if c.Root == (storagebudget.ID{}) || c.Owner == (storagebudget.Digest{}) || c.Filesystem == (storagebudget.ID{}) || c.RootGeneration == 0 || c.FilesystemGeneration == 0 || c.Workspace != (storagebudget.ID{}) || c.WorkspaceGeneration != 0 || c.Category >= storagebudget.Categories || r.RootIdentities[i] == (storagebudget.Digest{}) || r.FilesystemIdentities[i] == (storagebudget.Digest{}) || !storageIntentText(r.Locators[i], 256, false) || c.CapacityCount < 1 || c.CapacityCount > storagebudget.MaxCapacityRefs || c.CapacityBinding == (storagebudget.Digest{}) || t.Selectors[i] > storageTransferSymmetricDifferenceReferences {
			return storagebudget.ErrUnknown
		}
		p, e := storageVectorAdd(c.Physical, c.SettlementPhysical)
		a, f := storageVectorAdd(c.Attribution, c.SettlementAttribution)
		if e != nil || f != nil || !storageIntentVector(p) || !storageIntentVector(a) || p == (storagebudget.Vector{}) && a == (storagebudget.Vector{}) || t.Selectors[i] == 0 && a != (storagebudget.Vector{}) {
			return storagebudget.ErrUnknown
		}
		for j, v := range c.Capacity {
			if j >= int(c.CapacityCount) {
				if v != (storagebudget.CapacityPromise{}) {
					return storagebudget.ErrUnknown
				}
				continue
			}
			if v.Constraint == (storagebudget.ID{}) || v.Generation == 0 || v.Peak.Bytes > 1<<60 || v.Peak.Inodes > 1<<40 || j > 0 && bytes.Compare(c.Capacity[j-1].Constraint[:], v.Constraint[:]) >= 0 || p == (storagebudget.Vector{}) && v.Peak != (storagebudget.CapacityVector{}) {
				return storagebudget.ErrUnknown
			}
		}
		ch := r.Changes[i]
		for _, v := range []storagebudget.Vector{ch.PhysicalAdd, ch.PhysicalRemove, ch.AttributionAdd, ch.AttributionRemove} {
			if !storageIntentVector(v) {
				return storagebudget.ErrUnknown
			}
		}
		if !storageVectorLE(ch.PhysicalAdd, p) || !storageVectorLE(ch.AttributionAdd, a) || (r.Phase < storageIntentSettling || r.Phase == storageIntentRejected) && ch != (storagebudget.Change{}) {
			return storagebudget.ErrUnknown
		}
		if i > 0 {
			old := r.Components[i-1]
			cmp := bytes.Compare(old.Root[:], c.Root[:])
			if cmp > 0 || cmp == 0 && (old.Category > c.Category || old.Category == c.Category && t.Selectors[i-1] >= t.Selectors[i]) {
				return storagebudget.ErrUnknown
			}
		}
	}
	for i := n; i < len(t.Selectors); i++ {
		if t.Selectors[i] != 0 {
			return storagebudget.ErrUnknown
		}
	}
	if t.Mode == storageTransferExport {
		if !reflect.DeepEqual(t.Tail, storageCreationTail{}) || t.Selected != (storageOwnerPersistentObject{}) || t.Marker != (storageOwnerPersistentObject{}) || t.Location != (storageOwnerPersistentObject{}) || t.LocationPayload != (storagebudget.Digest{}) || t.PointerState != 0 || t.Empty || t.Existing != (storagebudget.Vector{}) {
			return storagebudget.ErrUnknown
		}
	} else if t.Tail.validate() != nil || t.Tail.Bridge != r.OperationID || !t.Empty {
		return storagebudget.ErrUnknown
	}
	prepared := r.Phase == storageIntentPrepared || r.Phase == storageIntentRejected
	if prepared {
		if r.Token != (storagebudget.Token{}) || r.PreviousToken != (storagebudget.Token{}) || t.Initial != (storagebudget.Token{}) || r.PolicyRevision != 0 {
			return storagebudget.ErrUnknown
		}
	} else {
		delta := uint64(1)
		if r.Phase == storageIntentReserved {
			delta = 0
		}
		if r.Phase >= storageIntentSettled {
			delta = 2
		}
		if !storageIntentToken(t.Initial) || t.Initial.Generation > ^uint64(0)-delta || r.Token.Slot != t.Initial.Slot || r.Token.ID != t.Initial.ID || r.Token.Sequence != t.Initial.Sequence || r.Token.Generation != t.Initial.Generation+delta || r.PolicyRevision == 0 {
			return storagebudget.ErrUnknown
		}
		previous := storagebudget.Token{}
		if delta > 0 {
			previous = t.Initial
			previous.Generation += delta - 1
		}
		if r.PreviousToken != previous {
			return storagebudget.ErrUnknown
		}
	}
	if t.Mode == storageTransferExport {
		if t.OldPointerDigest != (storagebudget.Digest{}) || t.OldPointerNative != (storageOwnerPersistentObject{}) || t.OldAbsent || t.Cleanup != (storageTransferCleanup{}) {
			return storagebudget.ErrUnknown
		}
	} else {
		if t.OldAbsent {
			if t.OldPointerDigest != (storagebudget.Digest{}) || t.OldPointerNative != (storageOwnerPersistentObject{}) {
				return storagebudget.ErrUnknown
			}
		} else if t.OldPointerDigest == (storagebudget.Digest{}) || !t.OldPointerNative.valid(2) {
			return storagebudget.ErrUnknown
		}
		if t.Cleanup != (storageTransferCleanup{}) && (r.Outcome != storageIntentAborted || !t.Cleanup.complete()) {
			return storagebudget.ErrUnknown
		}
	}
	known := r.Phase >= storageIntentSettling && r.Phase <= storageIntentRetired
	if known {
		if r.Outcome != storageIntentPublished && r.Outcome != storageIntentAborted || t.PointerState == 1 || t.Observed == (storagebudget.Digest{}) || t.Selection != storageTransferIntentSelection(r) || r.Evidence != storageTransferIntentEvidence(r) {
			return storagebudget.ErrUnknown
		}
		if t.Mode != storageTransferExport && r.Outcome == storageIntentPublished && (t.PointerState != 2 || !t.Selected.valid(1) || !t.Marker.valid(2) || !t.Location.valid(2) || t.LocationPayload == (storagebudget.Digest{})) {
			return storagebudget.ErrUnknown
		}
	} else if r.Outcome != storageIntentUnobserved && r.Phase != storageIntentRejected || r.Evidence != (storagebudget.Digest{}) || t.Selection != (storagebudget.Digest{}) || t.Observed != (storagebudget.Digest{}) || t.Selected != (storageOwnerPersistentObject{}) || t.Marker != (storageOwnerPersistentObject{}) || t.Location != (storageOwnerPersistentObject{}) || t.LocationPayload != (storagebudget.Digest{}) || t.PointerState != 0 {
		return storagebudget.ErrUnknown
	}
	if known && t.Mode != storageTransferExport && r.Outcome == storageIntentAborted && !t.Cleanup.complete() {
		return storagebudget.ErrUnknown
	}
	if r.Phase == storageIntentRejected {
		if r.Outcome != storageIntentNotAdmitted || r.Rejection < storageIntentQuota || r.Rejection > storageIntentBusy {
			return storagebudget.ErrUnknown
		}
	} else if r.Rejection != 0 {
		return storagebudget.ErrUnknown
	}
	if r.Phase == storageIntentRetired {
		if r.RetirementEpoch < r.AdmissionEpoch {
			return storagebudget.ErrUnknown
		}
	} else if r.RetirementEpoch != 0 {
		return storagebudget.ErrUnknown
	}
	return nil
}

func storageTransferIntentTransition(old, next storageIntentRecord, page []byte) bool {
	if old.Generation == ^uint64(0) || next.Generation != old.Generation+1 || next.Slot != old.Slot || next.PreviousPageDigest != sha256.Sum256(page) {
		return false
	}
	if old.reusable() {
		return next.Phase == storageIntentPrepared && next.OperationID != old.OperationID
	}
	if old.Transfer == nil || next.Transfer == nil || !reflect.DeepEqual(storageIntentImmutable(old), storageIntentImmutable(next)) || old.Phase != storageIntentPrepared && old.Transfer.Initial != next.Transfer.Initial || old.PolicyRevision != 0 && old.PolicyRevision != next.PolicyRevision {
		return false
	}
	same := next.Token == old.Token && next.PreviousToken == old.PreviousToken
	switch old.Phase {
	case storageIntentPrepared:
		return next.Phase == storageIntentReserved || next.Phase == storageIntentRejected
	case storageIntentReserved:
		return next.Phase == storageIntentStaging && next.PreviousToken == old.Token && storageIntentNextToken(old.Token, next.Token)
	case storageIntentStaging:
		return next.Phase == storageIntentPublishing && same
	case storageIntentPublishing:
		return next.Phase == storageIntentSettling && same
	case storageIntentSettling:
		return next.Phase == storageIntentSettled && next.PreviousToken == old.Token && storageIntentNextToken(old.Token, next.Token) && next.Evidence == old.Evidence && reflect.DeepEqual(next.Changes, old.Changes) && reflect.DeepEqual(next.Transfer, old.Transfer)
	case storageIntentSettled:
		return next.Phase == storageIntentRetired && same && next.Evidence == old.Evidence && reflect.DeepEqual(next.Changes, old.Changes) && reflect.DeepEqual(next.Transfer, old.Transfer)
	}
	return false
}

func storageTransferIntentMembership(catalog storagebudget.Digest, f *storagebudget.WorkspaceFanout) storagebudget.Digest {
	if f == nil {
		return storagebudget.Digest{}
	}
	raw, err := json.Marshal(struct {
		Catalog storagebudget.Digest
		Fanout  storagebudget.WorkspaceFanout
	}{catalog, *f})
	if err != nil {
		return storagebudget.Digest{}
	}
	return storageEnrollmentPeakHash("desk-storage-whole-transfer-original-reference-map-v1", raw)
}

func storageTransferCatalogIntentFanout(r storageIntentRecord, catalog storageOwnerCatalogV2) (*storagebudget.WorkspaceFanout, error) {
	if storageTransferIntentValid(r) != nil {
		return nil, storagebudget.ErrAuthority
	}
	t := r.Transfer
	digest, err := storageTransferCatalogDigest(catalog)
	if err != nil || digest != t.Catalog {
		return nil, storagebudget.ErrAuthority
	}
	roots := map[storagebudget.ID]storageOwnerRoot{}
	workspaces := map[storagebudget.ID]storagebudget.RegisteredWorkspace{}
	source := []storagebudget.ID{}
	found := false
	for _, row := range catalog.Roots {
		if int(row.DirectoryObject) >= len(catalog.Objects) || roots[row.Definition.ID].Definition.ID != (storagebudget.ID{}) {
			return nil, storagebudget.ErrBounds
		}
		roots[row.Definition.ID] = row
		if !storageTransferCatalogView(row) && catalog.Objects[row.DirectoryObject].Persistent == t.Source {
			if found {
				return nil, storagebudget.ErrAuthority
			}
			found = true
			source = append([]storagebudget.ID{}, row.References...)
		}
	}
	for _, row := range catalog.Workspaces {
		v := row.Definition
		if v.ID == (storagebudget.ID{}) || v.Generation == 0 || workspaces[v.ID].ID != (storagebudget.ID{}) {
			return nil, storagebudget.ErrBounds
		}
		workspaces[v.ID] = v
	}
	if !found || workspaces[t.Workspace].Generation != t.WorkspaceGeneration {
		return nil, storagebudget.ErrAuthority
	}
	sets := make([][]storagebudget.ID, len(r.Components))
	union := map[storagebudget.ID]bool{}
	seen := map[string]bool{}
	for i, component := range r.Components {
		row, ok := roots[component.Root]
		if !ok {
			return nil, storagebudget.ErrAuthority
		}
		d := row.Definition
		if d.Identity != r.RootIdentities[i] || d.Owner != component.Owner || d.Generation != component.RootGeneration || d.Filesystem != component.Filesystem || d.Locator != r.Locators[i] || d.CapacityBinding != component.CapacityBinding || d.CapacityCount != component.CapacityCount {
			return nil, storagebudget.ErrAuthority
		}
		for j := 0; j < int(d.CapacityCount); j++ {
			if d.CapacityRefs[j].Constraint != component.Capacity[j].Constraint || d.CapacityRefs[j].Generation != component.Capacity[j].Generation {
				return nil, storagebudget.ErrAuthority
			}
		}
		fsFound := false
		for _, fs := range catalog.Filesystems {
			if fs.Definition.ID == component.Filesystem && fs.Definition.Identity == r.FilesystemIdentities[i] && fs.Definition.Generation == component.FilesystemGeneration {
				fsFound = true
			}
		}
		if !fsFound {
			return nil, storagebudget.ErrAuthority
		}
		refs, e := storageTransferSelectedReferences(row.References, source, t.Selectors[i])
		if e != nil {
			return nil, e
		}
		sets[i] = refs
		expanded := append([]storagebudget.ID{}, refs...)
		if len(expanded) == 0 {
			expanded = []storagebudget.ID{{}}
		}
		for _, id := range expanded {
			key := string(component.Root[:]) + string([]byte{component.Category}) + string(id[:])
			if seen[key] {
				return nil, storagebudget.ErrAuthority
			}
			seen[key] = true
			if id != (storagebudget.ID{}) {
				if workspaces[id].Generation == 0 {
					return nil, storagebudget.ErrAuthority
				}
				union[id] = true
			}
		}
	}
	if len(union) > storagebudget.MaxWorkspaces {
		return nil, storagebudget.ErrBounds
	}
	ids := make([]storagebudget.ID, 0, len(union))
	for id := range union {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return bytes.Compare(ids[i][:], ids[j][:]) < 0 })
	f := &storagebudget.WorkspaceFanout{Workspaces: make([]storagebudget.WorkspaceReference, 0, len(ids)), Memberships: make([]storagebudget.WorkspaceMembership, len(sets))}
	indices := map[storagebudget.ID]int{}
	for i, id := range ids {
		indices[id] = i
		f.Workspaces = append(f.Workspaces, storagebudget.WorkspaceReference{ID: id, Generation: workspaces[id].Generation})
	}
	for i, refs := range sets {
		for _, id := range refs {
			index := indices[id]
			f.Memberships[i][index/8] |= 1 << uint(index%8)
		}
	}
	if storageTransferIntentMembership(t.Catalog, f) != t.Membership {
		return nil, storagebudget.ErrAuthority
	}
	return f, nil
}

// Prefer the retained original P projection after a legitimate cutover/reuse.
// A current catalog's expanded target union cannot reconstruct the old B set.
// The endpoint is installed only by the existing native/Q reconciliation, and
// its exact host page plus original view remains the independent proof.
func (o *storageBudgetOwner) originalTransferIntentFanout(r storageIntentRecord) (*storagebudget.WorkspaceFanout, error) {
	if o == nil || !o.valid() || r.Transfer == nil {
		return nil, storagebudget.ErrAuthority
	}
	o.ref.cell.mu.Lock()
	endpoint := o.ref.cell.enrollmentEndpoint
	o.ref.cell.mu.Unlock()
	if endpoint != nil {
		raw, err := storageEncodeIntent(r, o.ref.cell.record.Installation, o.ref.cell.binding)
		if err != nil {
			return nil, err
		}
		for _, w := range endpoint.Witnesses {
			if !w.Selected || !bytes.Equal(w.Page, raw) || int(w.Origin) >= len(endpoint.Board.Origins) {
				continue
			}
			origin := endpoint.Board.Origins[w.Origin]
			if int(origin.Projection) >= len(endpoint.Projections) {
				return nil, storagebudget.ErrAuthority
			}
			return storageTransferCatalogIntentFanout(r, endpoint.Projections[origin.Projection].Catalog)
		}
	}
	observer, ok := o.observer.(*storageAdvisoryObserver)
	if !ok || observer == nil {
		return nil, storagebudget.ErrAuthority
	}
	return storageTransferCatalogIntentFanout(r, observer.catalog)
}

// Structural discharge is joined to the retained original P origin and Q view;
// neither a Tail hash nor a reusable phase can supply those operands. Every
// old transfer witness carries its full canonical Tail, even after later P
// successors overwrite both full transfer banks.
func storageTransferTailBound(r storageIntentRecord, e storageEnrollmentPeakEvidence) bool {
	if storageTransferIntentValid(r) != nil || r.Phase != storageIntentRetired || !e.Scope.capacityEstimate() {
		return false
	}
	t := r.Transfer
	if t.Mode == storageTransferExport {
		return true
	} // no enrollment tail was issued
	tail := t.Tail
	if tail.Installation != e.Installation || tail.QBinding != e.QBinding || e.Phase > 2 || tail.Config != e.Scope.target(9) || tail.Anchor != e.Scope.target(10) || e.Scope.final(0) > tail.MaximumExtent || e.Scope.final(1) > tail.MaximumExtent || tail.Bridge == e.Bridge && tail.Project != e.Project {
		return false
	}
	page, err := storageEncodeIntent(r, tail.Installation, tail.QBinding)
	if err != nil {
		return false
	}
	digest := sha256.Sum256(page)
	covered := false
	for _, w := range e.Witnesses {
		if !w.Selected || w.Core.Transfer == nil || w.Digest != digest || !bytes.Equal(w.Page, page) || int(w.Origin) >= len(e.Board.Origins) {
			continue
		}
		origin := e.Board.Origins[w.Origin]
		if origin.Bridge != tail.Bridge || origin.Epoch != r.RetirementEpoch {
			return false
		}
		for _, coverage := range e.Board.Coverage {
			if coverage.Slot == r.Slot && coverage.Page == digest && coverage.Operation == r.OperationID && coverage.Generation == r.Generation && coverage.Epoch == e.AfterEpoch && coverage.Bridge == e.Bridge {
				covered = true
			}
		}
	}
	if !covered {
		return false
	}
	// Positive selection requires this exact retained target and marker. An
	// aborted transfer needs a separately proven cleanup/census relation; it is
	// not discharged by interpreting missing objects or unknown Close as empty.
	if r.Outcome == storageIntentAborted {
		if t.PointerState != 0 || !t.Cleanup.complete() {
			return false
		}
		// Coverage above joins this exact retained page to the actual original P/Q
		// origin. The old pointer remains material after full-bank reuse.
		found := false
		for _, control := range e.Inner.Catalog.OwnerControls {
			if int(control.ParentObject) >= len(e.Inner.Catalog.Objects) {
				return false
			}
			if e.Inner.Catalog.Objects[control.ParentObject].Persistent != t.Settings || string(control.Name) != dataLocationName {
				continue
			}
			if found || t.OldAbsent || control.Role != 2 || control.Location.Selected != t.Source {
				return false
			}
			found = true
		}
		// The catalog stores resolved location metadata, not its replaceable raw
		// inode or JSON image. Those exact operands are material in this retained
		// compact witness and were checked by its original live cleanup issuer.
		return t.OldAbsent && !found || !t.OldAbsent && found && t.OldPointerNative.valid(2) && t.OldPointerDigest != (storagebudget.Digest{})
	}
	if r.Outcome != storageIntentPublished || t.PointerState != 2 {
		return false
	}
	rootFound, markerFound := false, false
	for _, root := range e.Inner.Catalog.Roots {
		if int(root.DirectoryObject) >= len(e.Inner.Catalog.Objects) {
			return false
		}
		if root.Role == 4 && e.Inner.Catalog.Objects[root.DirectoryObject].Persistent == t.Selected {
			if rootFound {
				return false
			}
			rootFound = true
			for _, object := range e.Inner.Catalog.Objects {
				if object.Role == 23 && object.Parent == root.DirectoryObject && object.Persistent == t.Marker && string(object.Name) == dataMarkerName {
					markerFound = true
				}
			}
		}
	}
	return rootFound && markerFound
}

func storageTransferIntentBound(r storageIntentRecord, installation storagebudget.ID, binding storagebudget.Digest) bool {
	if r.Transfer == nil {
		return true
	}
	if r.Transfer.Mode != storageTransferExport && (r.Transfer.Tail.Installation != installation || r.Transfer.Tail.QBinding != binding) {
		return false
	}
	for _, component := range r.Components {
		if component.Owner != binding {
			return false
		}
	}
	return true
}

// Produce comparison data only after the actual caller has selected and held
// its original bank/slot. Selection is not inferred from r or this return value.
func storageTransferProjectIntent(r storageTransferRecord, catalog storageOwnerCatalogV2, slot uint16, generation uint64, previous storagebudget.Digest, phase storageIntentPhase) (storageIntentRecord, error) {
	var out storageIntentRecord
	if storageTransferRecordShape(r) != nil || slot != r.HostSlot || generation < r.HostGeneration || generation == 0 {
		return out, storagebudget.ErrBounds
	}
	f, err := storageTransferReferences(r, catalog)
	if err != nil {
		return out, err
	}
	out = storageIntentRecord{Slot: slot, Generation: generation, Phase: phase, OperationID: r.Operation, PreviousPageDigest: previous, CASBinding: r.Plan, AdmissionEpoch: r.Epoch, Workflow: storageTransferWorkflow, Operation: storageTransferOperation(r.Operation, r.Plan), Changes: append([]storagebudget.Change{}, r.Changes...), Transfer: &storageIntentTransfer{Mode: r.Mode, Source: r.Source, Catalog: r.Catalog, Initial: r.InitialToken, Settings: r.Settings, Existing: r.TargetExisting, Empty: r.TargetEmpty, Tail: r.Tail, Workspace: r.RequesterWorkspace, WorkspaceGeneration: r.RequesterGeneration}}
	if r.Mode != storageTransferExport {
		out.Transfer.OldAbsent = r.OldAbsent
		out.Transfer.OldPointerNative = r.OldPointerNative
		if !r.OldAbsent {
			out.Transfer.OldPointerDigest = sha256.Sum256(r.OldPointer)
		}
	}
	out.Transfer.Tail.Capacity = append([][80]byte(nil), r.Tail.Capacity...)
	out.Transfer.Membership = storageTransferIntentMembership(r.Catalog, f)
	for i, g := range r.Groups {
		out.Components = append(out.Components, g.Component)
		out.RootIdentities = append(out.RootIdentities, g.RootIdentity)
		out.FilesystemIdentities = append(out.FilesystemIdentities, g.FilesystemIdentity)
		out.Locators = append(out.Locators, g.Locator)
		out.Transfer.Selectors[i] = g.Flags
	}
	for _, output := range r.Outputs {
		if output.Bytes > 1<<60-out.NewLength {
			return storageIntentRecord{}, storagebudget.ErrBounds
		}
		out.NewLength += output.Bytes
	}
	for _, v := range []uint64{r.InputBytes, r.ArchiveBytes, r.MarkerBytes} {
		if v > 1<<60-out.NewLength {
			return storageIntentRecord{}, storagebudget.ErrBounds
		}
		out.NewLength += v
	}
	if phase != storageIntentPrepared && phase != storageIntentRejected {
		out.PolicyRevision = r.PolicyRevision
		out.Token = r.Token
		if phase != storageIntentReserved {
			out.PreviousToken = r.InitialToken
			if phase >= storageIntentSettled {
				out.PreviousToken.Generation++
			}
		}
	}
	if phase >= storageIntentSettling && phase <= storageIntentRetired {
		observed := r
		observed.Generation = 1
		observed.Phase = storageTransferObserved
		observed.Token = r.InitialToken
		observed.Token.Generation++
		raw, e := storageTransferEncode(observed)
		if e != nil {
			return storageIntentRecord{}, e
		}
		out.Transfer.Observed = sha256.Sum256(raw)
		out.Transfer.PointerState = r.PointerState
		out.Transfer.Cleanup = r.Cleanup
		out.Outcome = storageIntentAborted
		if r.Mode == storageTransferExport {
			out.Outcome = storageIntentPublished
		} else {
			if r.PointerState == 2 {
				out.Outcome = storageIntentPublished
			}
			target, e := storageTransferFinalTarget(r)
			if e == nil {
				out.Transfer.Selected = target
			}
			out.Transfer.Marker = r.MarkerNative
			if r.MarkerBytes == 0 {
				out.Transfer.Marker = r.MarkerBefore
			}
			out.Transfer.Location = r.PointerNative
			if r.PointerState == 2 {
				out.Transfer.LocationPayload = sha256.Sum256(r.NewPointer)
			}
		}
		out.Transfer.Selection = storageTransferIntentSelection(out)
		out.Evidence = storageTransferIntentEvidence(out)
	}
	if phase == storageIntentRetired {
		out.RetirementEpoch = r.Epoch
	}
	if err = storageTransferIntentValid(out); err != nil {
		return storageIntentRecord{}, err
	}
	return out, nil
}

// For an uncertain or nonretired projection, the exact original complete bank
// is still mandatory. A hash match cannot turn missing/torn originals into a
// completed result. Historical completed P witnesses use their separate full
// origin/applied/census closure and do not call this comparison after reuse.
func storageTransferIntentOriginalBank(r storageIntentRecord, bank storageTransferRecord, catalog storageOwnerCatalogV2) bool {
	if r.Transfer == nil || bank.HostSlot != r.Slot || bank.HostGeneration > r.Generation || bank.Operation != r.OperationID || bank.Plan != r.CASBinding || bank.Catalog != r.Transfer.Catalog || bank.InitialToken != r.Transfer.Initial || bank.Installation != (catalog.Installation) || bank.QBinding != catalog.QBinding {
		return false
	}
	phase := storageIntentPrepared
	switch bank.Phase {
	case storageTransferReserved:
		phase = storageIntentReserved
	case storageTransferDataIntent:
		phase = storageIntentStaging
	case storageTransferPointerIntent, storageTransferCompletionIntent:
		phase = storageIntentPublishing
	case storageTransferObserved:
		phase = storageIntentSettling
	case storageTransferSettled:
		phase = storageIntentSettled
	case storageTransferRetired:
		phase = storageIntentRetired
	case storageTransferRejected:
		phase = storageIntentRejected
	}
	var expected storageIntentRecord
	var err error
	if phase == storageIntentRejected {
		expected, err = storageTransferRejectedProjection(bank, catalog, r.Slot, r.Generation, r.PreviousPageDigest, r.Rejection)
	} else {
		expected, err = storageTransferProjectIntent(bank, catalog, r.Slot, r.Generation, r.PreviousPageDigest, phase)
	}
	return err == nil && reflect.DeepEqual(expected, r)
}

// The private P catalog is a distinct, already versioned data language. Only
// its path fields receive the existing request's16KiB bound. Legacy V2/V3
// catalog scanners, bounds and source identities retain their4096-byte rules.
func storageEnrollmentCatalogPath(c storageOwnerCatalogV2, path []byte) bool {
	if c.Version != 1 {
		return storageOwnerAbsolute(path)
	}
	return storageIntentText(string(path), storageTransferPointerBytes, false) && filepath.IsAbs(string(path)) && filepath.Clean(string(path)) == string(path)
}
func storageEnrollmentSourcePath(d *storageEnrollmentDefaultSource) bool {
	if d == nil {
		return false
	}
	if d.Workflow != 4 && d.Workflow != 5 && d.Workflow != 6 {
		return storageOwnerAbsolute(d.SelectedPath)
	}
	return storageEnrollmentCatalogPath(storageOwnerCatalogV2{Version: 1}, d.SelectedPath)
}
func storageEnrollmentPeakCatalogBounded(c storageOwnerCatalogV2) bool {
	if c.Version != 1 {
		return c.bounded()
	}
	if len(c.Objects)+len(c.OwnerControls) > 8192 || len(c.Roots) > 4096 || len(c.Workspaces) > 1024 || len(c.Filesystems) > 128 || len(c.Constraints) > 512 || len(c.Profiles) > 64 || len(c.OriginalOwner.Entries) > 1024 || len(c.BeforeDefinitionIDs.Roots) > 4096 || len(c.BeforeDefinitionIDs.Workspaces) > 1024 || len(c.BeforeDefinitionIDs.Filesystems) > 128 || len(c.BeforeDefinitionIDs.Constraints) > 512 {
		return false
	}
	total, refs := 0, 0
	add := func(b []byte, limit int, count bool) bool {
		if len(b) > limit {
			return false
		}
		if count {
			total += len(b)
		}
		return total <= 4<<20
	}
	s := c.OriginalOwner
	if len(s.HomeOwner) > 64 || !add(s.ConfigPath, 4096, true) || !add(s.HomePath, 4096, true) {
		return false
	}
	for _, h := range s.Entries {
		if len(h.DeskID) > 32 || len(h.StorageOwner) > 64 || len(h.RunnerWorkspace) > 64 || !add(h.Folder, 120, true) || !add(h.HistoryIdentity, 4096, true) {
			return false
		}
	}
	for _, o := range c.Objects {
		if !add(o.Name, 1024, true) {
			return false
		}
	}
	for _, v := range c.OwnerControls {
		if !add(v.Name, 1024, true) || !add(v.OwnerKey, 4096, false) || !add(v.Location.Path, storageTransferPointerBytes, true) || !add(v.Location.Previous, storageTransferPointerBytes, true) {
			return false
		}
	}
	for _, r := range c.Roots {
		if len(r.Definition.Locator) > 256 || len(r.References) > 1024 || len(r.Membership.ExistingObjects) > 8192 || !add(r.Source.OwnerKey, 4096, false) || !add(r.Source.Path, storageTransferPointerBytes, true) || !add(r.Source.Relative, 65599, true) {
			return false
		}
		refs += len(r.References)
		if refs > 65536 {
			return false
		}
	}
	for _, w := range c.Workspaces {
		if !add(w.OwnerKey, 256, false) || !add(w.HistoryIdentity, 4096, true) {
			return false
		}
	}
	return true
}

func storageEnrollmentPeakRootRole(c storageOwnerCatalogV2, r storageOwnerRoot) error {
	if len(r.Source.Path) <= 4096 {
		return c.validateRootRole(r)
	}
	// All non-History long paths remain outside this correction. Config/home,
	// project identities and provider relations retain their original bounds.
	s := r.Source
	if c.Version != 1 || r.Role != 4 || s.Kind != 4 || r.Category != storagebudget.History || int(r.DirectoryObject) >= len(c.Objects) || !storageEnrollmentCatalogPath(c, s.Path) || !storageOwnerRelative(s.Relative) || !storageOwnerIdentityString(string(s.OwnerKey)) || s.ControlKind > 2 {
		return storagebudget.ErrBounds
	}
	o := c.Objects[r.DirectoryObject]
	if o.Role != 20 && o.Role != 21 || r.DomainObject == storageOwnerNoObject || int(r.DomainObject) >= len(c.Objects) || c.Objects[r.DomainObject].Role != 18 || r.WorkObject != storageOwnerNoObject && (int(r.WorkObject) >= len(c.Objects) || c.Objects[r.WorkObject].Role != 19) {
		return storagebudget.ErrBounds
	}
	if s.ControlKind == 0 {
		if s.ControlIndex != storageOwnerNoControl || s.ControlSelection != 0 {
			return storagebudget.ErrBounds
		}
		return nil
	}
	if s.ControlKind != 2 || int(s.ControlIndex) >= len(c.OwnerControls) || s.ControlSelection < 1 || s.ControlSelection > 3 {
		return storagebudget.ErrBounds
	}
	control := c.OwnerControls[s.ControlIndex]
	if control.Role != 2 || !bytes.Equal(control.OwnerKey, s.OwnerKey) {
		return storagebudget.ErrBounds
	}
	current := bytes.Equal(s.Path, control.Location.Path) && o.Persistent == control.Location.Selected
	previous := len(control.Location.Previous) > 0 && bytes.Equal(s.Path, control.Location.Previous) && o.Persistent == control.Location.PreviousSelected
	if s.ControlSelection == 1 && !current || s.ControlSelection == 2 && !previous {
		return storagebudget.ErrBounds
	}
	if s.ControlSelection == 3 {
		old := false
		for _, id := range c.BeforeDefinitionIDs.Roots {
			old = old || id == r.Definition.ID
		}
		if !old || current || previous {
			return storagebudget.ErrBounds
		}
	}
	return nil
}

func storageEnrollmentAncestorSourceIdentity(source storageEnrollmentPeakSource, raw []byte) storagebudget.Digest {
	domain := "desk-default-history-ancestor-scope-e1"
	if source.Default != nil && source.Default.Workflow == 4 {
		domain = "desk-chat-transfer-ancestor-scope-v1"
	}
	return storageEnrollmentPeakHash(domain, raw)
}

func storageTransferCapturedRootJoins(c storageOwnerCatalogV2, r storageOwnerRoot) error {
	if c.Version != 1 || r.Role != 4 || r.Category != storagebudget.History || r.Source.Kind != 11 || r.Source.ParentRoot != (storagebudget.ID{}) || r.Source.ControlKind != 0 || r.Source.ControlIndex != storageOwnerNoControl || r.Source.ControlSelection != 0 || r.WorkObject != storageOwnerNoObject || int(r.DirectoryObject) >= len(c.Objects) || r.DomainObject == storageOwnerNoObject || int(r.DomainObject) >= len(c.Objects) || c.Objects[r.DomainObject].Role != 18 || (c.Objects[r.DirectoryObject].Role != 20 && c.Objects[r.DirectoryObject].Role != 21) || c.Objects[r.DirectoryObject].Parent != storageOwnerNoObject {
		return storagebudget.ErrBounds
	}
	source, err := storageEnrollmentPeakDecodeSource(r.Source.Relative)
	if err != nil || source.Default == nil || source.Default.Workflow != 6 || source.Default.OwnSelf != 1 || !storageEnrollmentPeakSourceJoins(source, c, r) || !bytes.Equal(r.Source.Path, source.Default.SelectedPath) || c.Objects[r.DirectoryObject].Persistent != source.Default.Selected || r.Definition.Identity != storageEnrollmentPeakHash("desk-chat-transfer-captured-history-v1", r.Source.Relative) {
		return storagebudget.ErrBounds
	}
	owner := storageTransferSourceStorageOwner(source, c)
	if owner == "" || !bytes.Equal(r.Source.OwnerKey, []byte(owner)) {
		return storagebudget.ErrBounds
	}
	if source.Default.Marker != (storageOwnerPersistentObject{}) {
		found := false
		for _, object := range c.Objects {
			if object.Role == 23 && object.Parent == r.DirectoryObject && object.Persistent == source.Default.Marker && string(object.Name) == dataMarkerName && object.FixedLength > 0 && object.FixedLength <= 4096 {
				found = true
			}
		}
		if !found {
			return storagebudget.ErrBounds
		}
	}
	return nil
}

func storageTransferSourceStorageOwner(source storageEnrollmentPeakSource, c storageOwnerCatalogV2) string {
	if source.Default == nil {
		return ""
	}
	if source.Default.Branch != storageCreationContained {
		return c.OriginalOwner.HomeOwner
	}
	if c.OriginalOwner.Kind == 1 {
		return string(source.OwnerKey)
	}
	owner := ""
	for _, entry := range c.OriginalOwner.Entries {
		if entry.RunnerWorkspace == string(source.OwnerKey) && bytes.Equal(entry.HistoryIdentity, source.HistoryIdentity) {
			if owner != "" {
				return ""
			}
			owner = entry.StorageOwner
		}
	}
	return owner
}

func storageTransferFinalActionProof(e storageEnrollmentPeakEvidence) bool {
	source := e.Board.Source
	if source.Default == nil || source.Default.Workflow != 5 || !e.Scope.capacityEstimate() || !storageEnrollmentDefaultHistorySourceJoins(source, e.Inner.Catalog) {
		return false
	}
	d := source.Default
	var qualification storagebudget.Digest
	copy(qualification[:], e.Scope[408:440])
	for _, w := range e.Witnesses {
		if !w.Selected || w.Core.Transfer == nil || w.Digest != d.CreationPage || w.Core.Phase != storageIntentRetired {
			continue
		}
		t := w.Core.Transfer
		if w.Core.Outcome != storageIntentPublished || t.Mode == storageTransferExport || t.Tail.Bridge != e.Bridge || t.Tail.Project != e.Project || t.Tail.Config != e.Scope.target(9) || t.Tail.Anchor != e.Scope.target(10) || t.Tail.Qualification != qualification || t.Settings != d.Settings || t.Source != d.SourceNative || t.Selected != d.Selected || t.Marker != d.Marker || t.Location != d.Location || t.LocationPayload != d.LocationPayload || t.PointerState != 2 || !storageTransferTailBound(w.Core, e) {
			return false
		}
		beforeFound := false
		for _, projection := range e.Projections {
			if storageEnrollmentPeakProjectionFullDigest(projection) != e.Before {
				continue
			}
			digest, err := storageTransferCatalogDigest(projection.Catalog)
			if err != nil || digest != t.Catalog {
				return false
			}
			beforeFound = true
		}
		return beforeFound
	}
	return false
}

// The whole-transfer producer uses only these original physical parents. A
// missing destination is charged to its already enrolled, held ancestor until
// the post-settlement census installs the exact descendant relation. It never
// invents a Q root for a pathname or admits a separate ticket for each row.
//
// Complete group bound, including aliases: an existing target has History<=2
// (new union material and existing E only for A\\B), Memory1, Attachments1 and
// Control1. Settings Control1, fixed config Control1, fixed Q Control1 and the
// one restore/archive Transient1 give <=9. Target and settings are distinct.
// A missing target uses source A for all four categories at its original
// ancestor; intermediate directories use that same Control selector. Only its
// settings Control charge can have a different reference set B. If those two
// parents alias, the combined quantity has at most three nonzero atoms
// B\\A,A-intersection-B,A\\B, versus two separate groups: at most one additional
// group. Including config/Q Control and config Transient gives <=9 again.
// No-reference fixed control charges add no attribution atom when parents
// alias. Export has only config/Q Control and config Transient, hence <=3.
// These are producer bounds, not a waiver of BuildGroups' final <=16 check.
func storageTransferChargeBoard(r storageTransferRecord, catalog storageOwnerCatalogV2) ([]storageTransferGroup, error) {
	if r.Mode < storageTransferMove || r.Mode > storageTransferExport || r.InputBytes > maxBackupArchive || r.ArchiveBytes > maxBackupArchive || len(r.Outputs) > maxStorageFiles || len(r.Descendants) >= storageTransferDescendantSlots || !r.Settings.valid(1) || !r.Source.valid(1) || !r.Parent.valid(1) {
		return nil, storagebudget.ErrBounds
	}
	roots := map[storageOwnerPersistentObject]storageOwnerRoot{}
	var config, q storageOwnerRoot
	for _, root := range catalog.Roots {
		if int(root.DirectoryObject) >= len(catalog.Objects) {
			return nil, storagebudget.ErrBounds
		}
		native := catalog.Objects[root.DirectoryObject].Persistent
		if storageTransferCatalogView(root) {
			continue
		}
		if _, duplicate := roots[native]; duplicate {
			return nil, storagebudget.ErrAuthority
		}
		roots[native] = root
		if root.Role == 1 {
			if config.Definition.ID != (storagebudget.ID{}) {
				return nil, storagebudget.ErrAuthority
			}
			config = root
		}
		if root.Role == 2 {
			if q.Definition.ID != (storagebudget.ID{}) {
				return nil, storagebudget.ErrAuthority
			}
			q = root
		}
	}
	if config.Definition.ID == (storagebudget.ID{}) || q.Definition.ID == (storagebudget.ID{}) || config.Definition.ID == q.Definition.ID {
		return nil, storagebudget.ErrAuthority
	}
	settings, ok := roots[r.Settings]
	if !ok {
		return nil, storagebudget.ErrAuthority
	}
	source, ok := roots[r.Source]
	if !ok || len(source.References) == 0 {
		return nil, storagebudget.ErrAuthority
	}
	charges := []storageTransferCharge{}
	add := func(root storageOwnerRoot, category, selector byte, physical, attribution, settlement storagebudget.Vector, metadata uint64) error {
		capacity, err := storageAdvisoryEstimate(physical.Bytes+settlement.Bytes, physical.Files+settlement.Files, physical.Directories+settlement.Directories, metadata)
		if err != nil {
			return err
		}
		charges = append(charges, storageTransferCharge{root: root.Definition.ID, category: category, references: selector, physical: physical, attribution: attribution, settlement: settlement, capacity: capacity})
		return nil
	}
	peaks, err := storageTransferControlPeaks()
	if err != nil {
		return nil, err
	}
	for index, root := range []storageOwnerRoot{config, q} {
		bytes := uint64(0)
		for _, group := range storageTransferControlPlan() {
			if int(group.Parent) == index+1 {
				bytes += group.Bytes
			}
		}
		charges = append(charges, storageTransferCharge{root: root.Definition.ID, category: storagebudget.Control, references: storageTransferNoReferences, settlement: storagebudget.Vector{Bytes: bytes}, capacity: peaks[index]})
	}
	// The retained RAM upload is not a filesystem effect. Its one admitted input
	// stage, or the mutually exclusive export archive, is a transient file under
	// the original config parent. Removal credit requires actual completion.
	temporary := uint64(0)
	if r.Mode == storageTransferRestore {
		temporary = r.InputBytes
	}
	if r.Mode == storageTransferExport {
		temporary = r.ArchiveBytes
	}
	if temporary != 0 {
		v := storagebudget.Vector{Bytes: temporary, Files: 1}
		if err = add(config, storagebudget.Transient, storageTransferSourceReferences, v, v, storagebudget.Vector{}, 28); err != nil {
			return nil, err
		}
	}
	if r.Mode == storageTransferExport {
		return storageTransferBuildGroups(catalog, r.Source, charges)
	}
	if !r.TargetEmpty || len(r.NewPointer) == 0 || len(r.NewPointer) > storageTransferPointerBytes || len(r.OldPointer) > storageTransferPointerBytes {
		return nil, storagebudget.ErrAuthority
	}
	parent, ok := roots[r.Parent]
	// A missing destination belongs to its exact active namespace view, even
	// when its nonowning anchor aliases an ordinary config/history root.
	viewFound := false
	for _, candidate := range catalog.Roots {
		if !storageTransferCatalogView(candidate) || candidate.Source.ControlSelection == 4 || catalog.Objects[candidate.DirectoryObject].Persistent != r.Parent {
			continue
		}
		scope, e := storageEnrollmentPeakDecodeSource(candidate.Source.Relative)
		if e != nil {
			return nil, e
		}
		var wire dataLocation
		if decodeDataJSON(r.NewPointer, &wire) != nil {
			return nil, storagebudget.ErrBounds
		}
		targetPath := wire.Path
		if !filepath.IsAbs(targetPath) {
			settingsPath, e := storageTransferNativePath(catalog, r.Settings)
			if e != nil {
				return nil, e
			}
			targetPath = filepath.Join(settingsPath, targetPath)
		}
		next, e := storageHistoryNextComponent(string(scope.Default.SelectedPath), targetPath)
		if e != nil || next != string(scope.Default.Next) {
			continue
		}
		if !reflect.DeepEqual(candidate.References, source.References) || viewFound {
			return nil, storagebudget.ErrAuthority
		}
		parent = candidate
		ok = true
		viewFound = true
	}
	if !ok {
		return nil, storagebudget.ErrAuthority
	}
	target := parent
	selector := byte(storageTransferSourceReferences)
	if r.TargetBefore != (storageOwnerPersistentObject{}) {
		target, ok = roots[r.TargetBefore]
		if !ok || target.Role != 4 || target.Definition.ID == settings.Definition.ID || len(r.Descendants) != 0 {
			return nil, storagebudget.ErrAuthority
		}
		selector = storageTransferUnionReferences
	} else if len(r.Descendants) == 0 {
		return nil, storagebudget.ErrBounds
	}
	for _, output := range r.Outputs {
		if !chatStorageFile(output.Name) || output.Bytes > maxMoveBytes {
			return nil, storagebudget.ErrBounds
		}
		category := byte(storagebudget.History)
		if memoryFileName.MatchString(output.Name) {
			category = storagebudget.Memory
		}
		if attachmentFileName.MatchString(output.Name) {
			category = storagebudget.Attachments
		}
		if assistantDialectReadRecoveryFileName.MatchString(output.Name) {
			category = storagebudget.Control
		}
		v := storagebudget.Vector{Bytes: output.Bytes, Files: 1}
		if err = add(target, category, selector, v, v, storagebudget.Vector{}, 28); err != nil {
			return nil, err
		}
	}
	if r.MarkerBytes != 0 {
		v := storagebudget.Vector{Bytes: r.MarkerBytes, Files: 1}
		if err = add(target, storagebudget.History, selector, v, v, storagebudget.Vector{}, 28); err != nil {
			return nil, err
		}
	}
	for index := range r.Descendants {
		category := byte(storagebudget.Control)
		if index == len(r.Descendants)-1 {
			category = storagebudget.History
		}
		v := storagebudget.Vector{Directories: 1}
		if err = add(parent, category, storageTransferSourceReferences, v, v, storagebudget.Vector{}, 16); err != nil {
			return nil, err
		}
	}
	if r.TargetBefore != (storageOwnerPersistentObject{}) && r.TargetExisting != (storagebudget.Vector{}) {
		if r.TargetExisting.Directories != 1 || r.TargetExisting.Files > 1 || r.TargetExisting.Bytes > 4096 {
			return nil, storagebudget.ErrBounds
		}
		charges = append(charges, storageTransferCharge{root: target.Definition.ID, category: storagebudget.History, references: storageTransferSourceOnlyReferences, attribution: r.TargetExisting})
	}
	pointer := storagebudget.Vector{Bytes: uint64(len(r.NewPointer)), Files: 1}
	pointerAttribution := pointer
	pointerSelector := byte(storageTransferRootReferences)
	if settings.Role == 1 {
		pointerAttribution = storagebudget.Vector{}
		pointerSelector = storageTransferNoReferences
	}
	if err = add(settings, storagebudget.Control, pointerSelector, pointer, pointerAttribution, storagebudget.Vector{}, 28); err != nil {
		return nil, err
	}
	groups, err := storageTransferBuildGroups(catalog, r.Source, charges)
	if err != nil {
		return nil, err
	}
	if len(groups) > 9 {
		return nil, storagebudget.ErrAuthority
	}
	return groups, nil
}

func storageTransferStageName(r storageTransferRecord) (string, error) {
	if r.Operation == (storagebudget.ID{}) || r.Mode != storageTransferRestore && r.Mode != storageTransferExport {
		return "", storagebudget.ErrAuthority
	}
	return ".data-transfer-" + hex.EncodeToString(r.Operation[:]), nil
}

// Read both original named banks. Even a correctly checksummed selected bank
// cannot excuse an unknown peer. This leaf does not recover or execute an
// operation, and it is never called from inside a Q callback.
func storageTransferLoadOriginalBanks(ctx context.Context, c *storageInstallation) (pair storageTransferPair, raw [2][]byte, err error) {
	if ctx == nil || c == nil || c.ownerRecord == nil {
		return pair, raw, storagebudget.ErrAuthority
	}
	for index, name := range []string{storageTransferBankA, storageTransferBankB} {
		if err = ctx.Err(); err != nil {
			return pair, raw, err
		}
		file, e := storageOpenOriginalControl(c, name)
		if e != nil {
			return pair, raw, e
		}
		raw[index] = make([]byte, storageTransferBankBytes)
		for offset := 0; offset < len(raw[index]); offset += 65536 {
			last := min(offset+65536, len(raw[index]))
			n, re := file.file.ReadAt(raw[index][offset:last], int64(offset))
			if re != nil || n != last-offset {
				e = errors.Join(storagebudget.ErrUnknown, re, io.ErrUnexpectedEOF)
				break
			}
		}
		if e == nil {
			e = file.current()
		}
		e = errors.Join(e, file.file.Close())
		if e != nil {
			return pair, raw, e
		}
	}
	pair, err = storageTransferDecodePair(raw)
	if err != nil {
		return pair, raw, err
	}
	for _, bank := range pair.Banks {
		if bank.Operation != (storagebudget.ID{}) && (bank.Installation != c.ownerRecord.Installation || bank.QBinding != c.binding) {
			return storageTransferPair{}, raw, storagebudget.ErrAuthority
		}
	}
	return pair, raw, ctx.Err()
}

func (o *storageBudgetOwner) validateOriginalTransferBank(ctx context.Context, r storageIntentRecord) error {
	if r.Transfer == nil || r.Phase == storageIntentRetired || r.Phase == storageIntentRejected {
		return nil
	}
	observer, ok := o.observer.(*storageAdvisoryObserver)
	if !ok || observer == nil {
		return storagebudget.ErrAuthority
	}
	pair, _, err := storageTransferLoadOriginalBanks(ctx, o.ref.cell)
	if err != nil {
		return errors.Join(errStorageOwnerRecovery, err)
	}
	if pair.RecoveryOnly || !storageTransferIntentOriginalBank(r, pair.Banks[pair.Selected], observer.catalog) {
		return errStorageOwnerRecovery
	}
	return nil
}

// A captured historical destination has an immutable source relation rather
// than a mutable selection discriminator. Only this transfer-specific reader
// may use it to reconstruct an old data-location relation. The original
// settings native and storage owner remain exact; matching a pathname alone
// is insufficient. Generic catalog and control readers are unchanged.
func storageTransferControlRoot(c storageOwnerCatalogV2, r storageOwnerRoot, index uint32) bool {
	if r.Role != 4 || int(index) >= len(c.OwnerControls) || int(r.DirectoryObject) >= len(c.Objects) {
		return false
	}
	if r.Source.ControlKind == 2 {
		return r.Source.ControlIndex == index
	}
	if r.Source.Kind != 11 || r.Source.ControlKind != 0 || r.Source.ControlIndex != storageOwnerNoControl || storageTransferCapturedRootJoins(c, r) != nil {
		return false
	}
	source, err := storageEnrollmentPeakDecodeSource(r.Source.Relative)
	control := c.OwnerControls[index]
	return err == nil && source.Default != nil && int(control.ParentObject) < len(c.Objects) && source.Default.Settings == c.Objects[control.ParentObject].Persistent && control.Role == 2 && string(control.Name) == dataLocationName && bytes.Equal(r.Source.OwnerKey, control.OwnerKey)
}

// This is the transfer-only catalog comparison used while constructing and
// decoding P. It proves neither execution nor emptiness. Complete P validation
// additionally requires the selected retired HINT9 witness, original applied Q
// view/manifest, exact before projection and native-bound owner reconciliation.
func storageTransferSourceSelection(before, after storageOwnerCatalogV2, source storageEnrollmentPeakSource) bool {
	d := source.Default
	if d == nil || d.Workflow != 5 || !storageEnrollmentDefaultSourceNamespace(source) || !storageEnrollmentDefaultHistorySourceJoins(source, after) {
		return false
	}
	selected := -1
	oldIndex := -1
	for i, control := range after.OwnerControls {
		if int(control.ParentObject) >= len(after.Objects) {
			return false
		}
		if after.Objects[control.ParentObject].Persistent == d.Settings && string(control.Name) == dataLocationName {
			if selected >= 0 {
				return false
			}
			selected = i
		}
	}
	for i, control := range before.OwnerControls {
		if int(control.ParentObject) >= len(before.Objects) {
			return false
		}
		if before.Objects[control.ParentObject].Persistent == d.Settings && string(control.Name) == dataLocationName {
			if oldIndex >= 0 {
				return false
			}
			oldIndex = i
		}
	}
	if selected < 0 {
		return false
	}
	nextControl := after.OwnerControls[selected]
	owner := storageTransferSourceStorageOwner(source, after)
	if owner == "" || nextControl.OwnerKind != 2 || string(nextControl.OwnerKey) != owner || nextControl.Location.Selected != d.Selected || nextControl.Location.PreviousSelected == d.Selected || !nextControl.Location.PreviousSelected.valid(1) {
		return false
	}
	r := storageTransferRecord{Mode: storageTransferMove, Installation: source.Installation, QBinding: source.QBinding, Settings: d.Settings, Source: nextControl.Location.PreviousSelected, TargetEmpty: true, OldAbsent: oldIndex < 0}
	for _, root := range before.Roots {
		if int(root.DirectoryObject) >= len(before.Objects) {
			return false
		}
		if !storageTransferCatalogView(root) && before.Objects[root.DirectoryObject].Persistent == d.Selected {
			if r.TargetBefore != (storageOwnerPersistentObject{}) {
				return false
			}
			r.TargetBefore = d.Selected
		}
	}
	next := dataLocation{Version: int(nextControl.Location.Version), Path: string(nextControl.Location.Path), Previous: string(nextControl.Location.Previous)}
	old := dataLocation{}
	if oldIndex >= 0 {
		control := before.OwnerControls[oldIndex]
		old = dataLocation{Version: int(control.Location.Version), Path: string(control.Location.Path), Previous: string(control.Location.Previous)}
	}
	return storageTransferSelectionMetadata(r, d.Selected, before, after, old, next) == nil
}

// Pure closed comparison only. An original issuer must separately establish
// the exact definite Reserve error, same epoch and no matching Q ticket.
func storageTransferRejectedProjection(r storageTransferRecord, catalog storageOwnerCatalogV2, slot uint16, generation uint64, previous storagebudget.Digest, reason storageIntentRejection) (storageIntentRecord, error) {
	if r.Phase != storageTransferRejected || reason < storageIntentQuota || reason > storageIntentBusy {
		return storageIntentRecord{}, storagebudget.ErrAuthority
	}
	out, err := storageTransferProjectIntent(r, catalog, slot, generation, previous, storageIntentPrepared)
	if err != nil {
		return storageIntentRecord{}, err
	}
	out.Phase = storageIntentRejected
	out.Outcome = storageIntentNotAdmitted
	out.Rejection = reason
	if err = storageTransferIntentValid(out); err != nil {
		return storageIntentRecord{}, err
	}
	return out, nil
}

// Same-epoch no-ticket is complete metadata, never absence inferred from an
// inspection error or an empty result. Compare operation identity even if a
// corrupted/mismatched component list would evade the ordinary exact matcher.
func storageTransferNoTicket(r storageTransferRecord, status storagebudget.HostState) bool {
	if r.Operation == (storagebudget.ID{}) || r.Plan == (storagebudget.Digest{}) || r.Epoch == 0 || status.Epoch != r.Epoch || status.Maintenance {
		return false
	}
	operation := storageTransferOperation(r.Operation, r.Plan)
	for _, row := range status.Slots {
		ticket := row.Ticket
		if ticket.State != storagebudget.Empty && ticket.Workflow == storageTransferWorkflow && ticket.Operation == operation && ticket.Owner == r.QBinding {
			return false
		}
	}
	return true
}

// All host saves, including those while Closing, are projections of the exact
// current whole bank. Completion cannot use this to Reserve, change a plan or
// mint a host operation. Rejected additionally requires this invocation's exact
// actual Reserve return and the Q inspection it subsequently performed.
func (o *storageBudgetOwner) requireOriginalTransferProjection(ctx context.Context, op *chatTransferOperation, r storageIntentRecord) error {
	if o == nil || op == nil || op.self != op || op.preparation == nil || op.preparation.registry == nil || op.preparation.registry.owner != o || op.preparation.self != op.preparation || op.preparation.operation != op || op.preparation.epoch == nil || op.preparation.epoch.self != op.preparation.epoch || op.preparation.epoch.transfer != op.preparation || r.Transfer == nil || op.controlOriginal == nil || op.controlOriginal != op.preparation.epoch.original {
		return storagebudget.ErrAuthority
	}
	digest, err := storageTransferCatalogDigest(op.catalog)
	if err != nil || digest != op.record.Catalog {
		return errors.Join(storagebudget.ErrAuthority, err)
	}
	if r.Slot != op.record.HostSlot || r.Generation != op.host.record.Generation+1 || r.PreviousPageDigest != func() storagebudget.Digest {
		if op.host.record.Phase == storageIntentEmpty {
			return storagebudget.Digest{}
		}
		return sha256.Sum256(op.host.page)
	}() {
		return storagebudget.ErrStale
	}
	pair, err := storageTransferDecodePair(op.fullBanks)
	if err != nil || pair.RecoveryOnly || !reflect.DeepEqual(pair.Banks[pair.Selected], op.record) {
		return errors.Join(storagebudget.ErrAuthority, err)
	}
	if r.Phase == storageIntentRejected {
		if !op.reserveReturned || storageDefinitelyRejected(op.reserveError) != r.Rejection || op.rejectedProof == nil || !storageTransferNoTicket(op.record, *op.rejectedProof) {
			return storagebudget.ErrAuthority
		}
	}
	if !storageTransferIntentOriginalBank(r, op.record, op.catalog) {
		return storagebudget.ErrAuthority
	}
	return op.originalReadCurrent(ctx)
}

func (op *chatTransferOperation) takeOriginalTransferControl(ctx context.Context, ids []uint16, completion *chatTransferCompletion) error {
	if op == nil || op.self != op || len(ids) == 0 || op.controlOriginal == nil || op.preparation == nil || op.preparation.epoch == nil || op.controlOriginal != op.preparation.epoch.original {
		return storagebudget.ErrAuthority
	}
	if completion == nil {
		if err := op.currentBeforeEffect(ctx); err != nil {
			return err
		}
	} else {
		if completion.operation != op {
			return storagebudget.ErrAuthority
		}
		if err := completion.current(ctx); err != nil {
			return err
		}
	}
	op.mu.Lock()
	defer op.mu.Unlock()
	for _, id := range ids {
		if id < 1 || id > storageTransferControlGroups || op.controlConsumed[id-1] {
			return storagebudget.ErrAuthority
		}
	}
	for i, id := range ids {
		for _, prior := range ids[:i] {
			if prior == id {
				return storagebudget.ErrAuthority
			}
		}
	}
	for _, id := range ids {
		op.controlConsumed[id-1] = true
		if completion != nil {
			completion.consumed[id-1] = true
		}
	}
	return nil
}

func (op *chatTransferOperation) publishOriginalTransferHost(ctx context.Context, phase storageIntentPhase, reason storageIntentRejection, group uint16, completion *chatTransferCompletion) (err error) {
	if err = op.takeOriginalTransferControl(ctx, []uint16{group}, completion); err != nil {
		return err
	}
	r := op.record
	generation := op.host.record.Generation + 1
	previous := storagebudget.Digest{}
	if op.host.record.Phase != storageIntentEmpty {
		previous = sha256.Sum256(op.host.page)
	}
	var projected storageIntentRecord
	if phase == storageIntentRejected {
		projected, err = storageTransferRejectedProjection(r, op.catalog, r.HostSlot, generation, previous, reason)
	} else {
		projected, err = storageTransferProjectIntent(r, op.catalog, r.HostSlot, generation, previous, phase)
	}
	if err != nil {
		return err
	}
	if err = op.enterOriginalTransferCall(ctx, completion); err != nil {
		return err
	}
	defer func() { err = op.leaveOriginalTransferCall(err) }()
	return op.preparation.registry.owner.withControl(ctx, func(controlCtx context.Context, table *storageIntentTable) error {
		if !reflect.DeepEqual(table.slots[r.HostSlot], op.host) {
			return storagebudget.ErrStale
		}
		if err := table.saveWithOriginalTransfer(controlCtx, projected, op); err != nil {
			return err
		}
		op.host = table.slots[r.HostSlot]
		op.controlOriginal.Hosts[r.HostSlot][op.host.bank] = append([]byte{}, op.host.page...)
		return nil
	})
}

// Read-only control I/O is explicit too: one initial pair, two complete
// before peers for each of ten writes, then four full/six header readbacks.
// Q/host own fixed reads remain in their existing bounded leaf schedules.
func storageTransferBankReadBound() uint64 {
	return uint64((2+2*10+4)*storageTransferBankBytes + 6*storageTransferPage)
}

func (op *chatTransferOperation) publishOriginalTransferBank(ctx context.Context, r storageTransferRecord, bank int, group uint16, headerOnly bool, completion *chatTransferCompletion) (err error) {
	if op == nil || op.self != op || op.preparation == nil || bank < 0 || bank > 1 || op.controlOriginal == nil {
		return storagebudget.ErrAuthority
	}
	raw, err := storageTransferEncode(r)
	if err != nil {
		return err
	}
	detached, err := storageTransferDecode(raw)
	if err != nil {
		return err
	}
	if err = storageTransferPersistenceBefore(op.fullBanks, bank, raw, headerOnly); err != nil {
		return err
	}
	if err = op.takeOriginalTransferControl(ctx, []uint16{group}, completion); err != nil {
		return err
	}
	if err = op.enterOriginalTransferCall(ctx, completion); err != nil {
		return err
	}
	defer func() { err = op.leaveOriginalTransferCall(err) }()
	return op.preparation.registry.owner.withControl(ctx, func(controlCtx context.Context, _ *storageIntentTable) (err error) {
		name := []string{storageTransferBankA, storageTransferBankB}[bank]
		original, err := storageOpenOriginalControl(op.preparation.epoch.c.ref.cell, name)
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, original.file.Close()) }()
		// From this point the original preadmitted control allowance may have been
		// consumed even without a Q ticket. Uncertainty cannot restore Open.
		op.preparation.admitted = true
		if err = storageTransferPersist(controlCtx, original, op.bankNative, op.fullBanks, bank, raw, headerOnly); err != nil {
			return err
		}
		op.fullBanks[bank] = raw
		op.record = detached
		return nil
	})
}

// Complete initial-room admission occurs before either Prepared bank write.
// The existing Tail reserves its own finite successor controls independently;
// this check includes every transfer component's physical/capacity operands.
func storageTransferInitialCapacityRoom(r storageTransferRecord, catalog storageOwnerCatalogV2, observations []storagebudget.CapacityObservation) error {
	required := map[storagebudget.ID]storagebudget.CapacityVector{}
	for _, group := range r.Groups {
		for i := 0; i < int(group.Component.CapacityCount); i++ {
			promise := group.Component.Capacity[i]
			sum := required[promise.Constraint]
			if sum.Bytes > 1<<60-promise.Peak.Bytes || sum.Inodes > 1<<40-promise.Peak.Inodes {
				return storagebudget.ErrBounds
			}
			sum.Bytes += promise.Peak.Bytes
			sum.Inodes += promise.Peak.Inodes
			required[promise.Constraint] = sum
		}
	}
	found := map[storagebudget.ID]bool{}
	for _, observation := range observations {
		need, requiredHere := required[observation.Constraint]
		if !requiredHere {
			continue
		}
		if found[observation.Constraint] {
			return storagebudget.ErrBounds
		}
		found[observation.Constraint] = true
		matched := false
		for _, row := range catalog.Constraints {
			constraint := row.Definition
			if constraint.ID != observation.Constraint {
				continue
			}
			if matched || constraint.Generation != observation.Generation || observation.KnownResources != constraint.Resources {
				return storagebudget.ErrStale
			}
			matched = true
			if constraint.Resources&storagebudget.CapacityBytes != 0 && (observation.Available.Bytes < constraint.Headroom.Bytes || observation.Available.Bytes-constraint.Headroom.Bytes < need.Bytes) || constraint.Resources&storagebudget.CapacityInodes != 0 && (observation.Available.Inodes < constraint.Headroom.Inodes || observation.Available.Inodes-constraint.Headroom.Inodes < need.Inodes) {
				return storagebudget.ErrHeadroom
			}
		}
		if !matched {
			return storagebudget.ErrAuthority
		}
	}
	if len(found) != len(required) {
		return storagebudget.ErrUnknown
	}
	return nil
}

// Native original bank reads are separate from their closed decoding. Both
// zero is admissible only for this first-use original no-outstanding-Q capture;
// an unknown peer is never treated as a fresh operation.
func (op *chatTransferOperation) captureOriginalTransferBanks(ctx context.Context) (err error) {
	if op == nil || op.self != op || op.preparation == nil || op.controlOriginal == nil || op.fullBanks[0] != nil || op.fullBanks[1] != nil {
		return storagebudget.ErrAuthority
	}
	for i, name := range []string{storageTransferBankA, storageTransferBankB} {
		file, e := storageOpenOriginalControl(op.preparation.epoch.c.ref.cell, name)
		if e != nil {
			return e
		}
		native, e := storageOwnerPersistentFile(ctx, file)
		raw := make([]byte, storageTransferBankBytes)
		n, re := file.file.ReadAt(raw, 0)
		e = errors.Join(e, re, file.current(), file.file.Close())
		if e != nil || n != len(raw) || !native.valid(2) {
			return errors.Join(storagebudget.ErrUnknown, e)
		}
		op.fullBanks[i], op.bankNative[i] = raw, native
	}
	if op.bankNative[0] == op.bankNative[1] {
		return storagebudget.ErrAuthority
	}
	if storageAllZero(op.fullBanks[0]) && storageAllZero(op.fullBanks[1]) {
		return nil
	}
	pair, err := storageTransferDecodePair(op.fullBanks)
	if err != nil || pair.RecoveryOnly {
		return errors.Join(errStorageEnrollmentUnresolved, err)
	}
	prior := pair.Banks[pair.Selected]
	if prior.Phase != storageTransferRetired && prior.Phase != storageTransferRejected {
		return errStorageEnrollmentUnresolved
	}
	// The exact selected original HINT9 remains mandatory here. P's retained
	// original witness may supply it after legitimate host-slot reuse.
	type candidate struct {
		core storageIntentRecord
		page []byte
	}
	var candidates []candidate
	for slot, pages := range op.controlOriginal.Hosts {
		selected, e := storageSelectCreationHost(pages[0], pages[1], slot, prior.Installation, prior.QBinding)
		if e != nil {
			return e
		}
		if selected.Slot.record.OperationID == prior.Operation {
			candidates = append(candidates, candidate{selected.Slot.record, selected.Slot.page})
		}
	}
	p := op.preparation.epoch
	if p.previousEvidence != nil {
		for _, w := range p.previousEvidence.Witnesses {
			if w.Selected && w.Core.OperationID == prior.Operation {
				candidates = append(candidates, candidate{w.Core, w.Page})
			}
		}
	}
	proven := false
	status := storageHostState(p.initialInspection.View)
	for _, candidate := range candidates {
		host := candidate.core
		if host.Transfer == nil || host.CASBinding != prior.Plan || host.Transfer.Catalog != prior.Catalog || host.Transfer.Initial != prior.InitialToken {
			continue
		}
		original := op.catalog
		originalFound := false
		if digest, e := storageTransferCatalogDigest(original); e == nil && digest == prior.Catalog {
			originalFound = true
		}
		if p.previousEvidence != nil {
			for _, w := range p.previousEvidence.Witnesses {
				if !w.Selected || !bytes.Equal(w.Page, candidate.page) || !reflect.DeepEqual(w.Core, host) || w.Digest != sha256.Sum256(w.Page) || int(w.Origin) >= len(p.previousEvidence.Board.Origins) {
					continue
				}
				origin := p.previousEvidence.Board.Origins[w.Origin]
				if int(origin.Projection) >= len(p.previousEvidence.Projections) {
					return storagebudget.ErrAuthority
				}
				c := p.previousEvidence.Projections[origin.Projection].Catalog
				digest, e := storageTransferCatalogDigest(c)
				if e != nil || digest != prior.Catalog {
					return errStorageEnrollmentUnresolved
				}
				original = c
				originalFound = true
			}
		}
		if !originalFound || !storageTransferIntentOriginalBank(host, prior, original) {
			continue
		}
		if prior.Phase == storageTransferRejected {
			dependency, e := storageTransferNamespaceDependency(host, original)
			if e != nil {
				return errors.Join(errStorageEnrollmentUnresolved, e)
			}
			if host.Phase == storageIntentRejected && (dependency == nil && storageTransferNoTicket(prior, status) || dependency != nil && storageTransferCompactNoTicket(host, status, false) && storageTransferRejectedRetirementCompleted(host, original, p.previousEvidence)) {
				proven = true
			}
		} else if host.Phase == storageIntentRetired && storageIntentRetiredProof(host, status) && p.c.owner.validateAppliedIntentFanout(ctx, host, status) == nil && (prior.Mode == storageTransferExport || p.previousEvidence != nil && storageTransferTailBound(host, *p.previousEvidence)) {
			proven = true
		}
	}
	if !proven {
		return errStorageEnrollmentUnresolved
	}
	op.record.Previous = sha256.Sum256(op.fullBanks[pair.Selected])
	return nil
}

// This query is consumed once for each Q phase. A returned token is checked
// against the complete immutable request/manifest, never just its slot or ID.
func (op *chatTransferOperation) originalTransferTicket(ctx context.Context, token storagebudget.Token, state storagebudget.State, index int) (out storagebudget.Ticket, err error) {
	if op == nil || op.self != op || index < 0 || index >= len(op.ticketReads) {
		return out, storagebudget.ErrAuthority
	}
	op.mu.Lock()
	if op.ticketReads[index] {
		op.mu.Unlock()
		return out, storagebudget.ErrAuthority
	}
	op.ticketReads[index] = true
	completion := op.completion
	if !op.completing {
		completion = nil
	}
	op.mu.Unlock()
	if err = op.enterOriginalTransferCall(ctx, completion); err != nil {
		return out, err
	}
	defer func() { err = op.leaveOriginalTransferCall(err) }()
	status, err := op.preparation.registry.owner.ref.cell.q.InspectHostState(ctx)
	if err != nil {
		return out, err
	}
	if status.Epoch != op.record.Epoch || status.Maintenance || int(token.Slot) >= len(status.Slots) {
		return out, errStorageEnrollmentUnresolved
	}
	ticket := status.Slots[token.Slot].Ticket
	if ticket.Token != token || ticket.State != state || ticket.Owner != op.record.QBinding || ticket.Workflow != op.request.Workflow || ticket.Operation != op.request.Operation || ticket.PolicyRevision != op.record.PolicyRevision || !reflect.DeepEqual(ticket.Components, op.request.Components) || !reflect.DeepEqual(ticket.Fanout, op.request.Fanout) {
		return out, errStorageEnrollmentUnresolved
	}
	manifest, err := op.preparation.registry.owner.ref.cell.q.Manifest(ctx, token, op.record.QBinding)
	if err != nil || !reflect.DeepEqual(manifest, op.request) {
		return out, errors.Join(errStorageEnrollmentUnresolved, err)
	}
	return ticket, nil
}

// The original lexical planner fills only workload bytes/names. This issuer
// replaces all authority, epoch, membership, ticket and control operands from
// the held owner; serialized metadata cannot select a requester or reservation.
func (op *chatTransferOperation) sealOriginalTransferPlan(ctx context.Context) (observations []storagebudget.CapacityObservation, err error) {
	if op == nil || op.self != op || op.preparation == nil || op.preparation.epoch == nil || op.controlOriginal != nil || op.request.Operation != "" || op.preparation.admitted {
		return nil, storagebudget.ErrAuthority
	}
	if err = op.currentBeforeEffect(ctx); err != nil {
		return nil, err
	}
	p := op.preparation.epoch
	o := p.c.owner
	if p.original == nil || p.initialInspection == nil || p.qualified == nil || p.creation != nil || p.scope != nil || op.record.Mode != op.preparation.mode {
		return nil, storagebudget.ErrAuthority
	}
	observer, ok := o.observer.(*storageAdvisoryObserver)
	if !ok || observer.owner != o {
		return nil, storagebudget.ErrAuthority
	}
	catalogRaw, err := json.Marshal(observer.catalog)
	if err != nil {
		return nil, err
	}
	if err = storageEnrollmentPeakCatalogJSON(catalogRaw, &op.catalog); err != nil {
		return nil, err
	}
	settings, selection, err := p.c.originalHistorySettings(ctx, op.preparation.server)
	if err != nil {
		return nil, err
	}
	op.settings = settings.root
	r := op.record
	r.Generation = 1
	r.Phase = storageTransferPrepared
	r.Operation, err = storagebudget.NewID()
	if err != nil {
		return nil, err
	}
	r.Installation = p.c.ref.cell.ownerRecord.Installation
	r.Owner = p.c.ref.cell.binding
	r.QBinding = r.Owner
	r.Epoch = p.initialInspection.View.Configuration.Epoch
	r.PolicyRevision = p.initialInspection.View.Configuration.Revision
	r.Settings = settings.source.persistent
	r.Requester = p.original.Project
	r.RequesterWorkspace = storagebudget.ID{}
	r.RequesterGeneration = 0
	r.Token = storagebudget.Token{}
	r.InitialToken = storagebudget.Token{}
	r.PointerState = 0
	r.PointerNative = storageOwnerPersistentObject{}
	r.ArchiveState = 0
	r.MarkerState = 0
	r.ArchiveNative = storageOwnerPersistentObject{}
	r.MarkerNative = storageOwnerPersistentObject{}
	r.Plan = storagebudget.Digest{}
	r.Body = storagebudget.Digest{}
	r.Previous = storagebudget.Digest{}
	r.Catalog, err = storageTransferCatalogDigest(op.catalog)
	if err != nil {
		return nil, err
	}
	key, err := op.preparation.accountingKey(ctx)
	if err != nil {
		return nil, err
	}
	for _, workspace := range op.catalog.Workspaces {
		if string(workspace.OwnerKey) == key && string(workspace.HistoryIdentity) == op.preparation.server.conversationIdentity() {
			if r.RequesterWorkspace != (storagebudget.ID{}) {
				return nil, storagebudget.ErrAuthority
			}
			r.RequesterWorkspace = workspace.Definition.ID
			r.RequesterGeneration = workspace.Definition.Generation
		}
	}
	if r.RequesterWorkspace == (storagebudget.ID{}) {
		return nil, storagebudget.ErrAuthority
	}
	sourceFound := false
	for _, root := range p.originalRoots {
		if root.source != nil && root.source.path == selection.location.Path && root.source.persistent == r.Source && !storageTransferNamespaceBinding(root) {
			if sourceFound {
				return nil, storagebudget.ErrAuthority
			}
			sourceFound = true
		}
	}
	if !sourceFound {
		return nil, storagebudget.ErrAuthority
	}
	r.OldPointer, err = readPrivateData(settings.root, dataLocationName, storageTransferPointerBytes)
	r.OldAbsent = errors.Is(err, os.ErrNotExist)
	if r.OldAbsent {
		r.OldPointer = nil
		err = nil
	}
	if err != nil {
		return nil, err
	}
	r.OldPointerNative = storageOwnerPersistentObject{}
	r.Cleanup = storageTransferCleanup{}
	if r.Mode != storageTransferExport && !r.OldAbsent {
		named, e := storageOpenFile(settings.root, dataLocationName, int64(len(r.OldPointer)))
		if e != nil {
			return nil, e
		}
		r.OldPointerNative, e = storageOwnerPersistentFile(ctx, named)
		if e == nil {
			e = named.current()
		}
		if e != nil {
			return nil, errors.Join(e, named.file.Close())
		}
		op.oldPointer = &named
	}
	if selection.explicit == r.OldAbsent {
		return nil, errStorageCustody
	}
	if !r.OldAbsent {
		old, e := storageTransferResolvedLocation(r.OldPointer, selection.path)
		if e != nil || old != selection.location {
			return nil, errors.Join(errStorageCustody, e)
		}
	}
	op.record = r
	if r.Mode != storageTransferExport {
		if op.path == nil || op.path.self != op.path || op.path.closed {
			return nil, storagebudget.ErrAuthority
		}
		next, e := storageTransferResolvedLocation(r.NewPointer, selection.path)
		if e != nil || next.Path != op.path.path || next.Previous != selection.location.Path {
			return nil, errors.Join(storagebudget.ErrAuthority, e)
		}
		r.Parent = op.path.native
		r.Basis = op.path.native
		var wire dataLocation
		if decodeDataJSON(r.NewPointer, &wire) != nil {
			return nil, storagebudget.ErrBounds
		}
		r.TargetExisting = storagebudget.Vector{}
		r.MarkerBefore = storageOwnerPersistentObject{}
		r.MarkerBytes = 0
		r.MarkerDigest = storagebudget.Digest{}
		r.Descendants = nil
		if op.path.missing == len(op.path.names) {
			r.TargetBefore = op.path.native
			r.ExistingPrefix = uint16(len(wire.Path))
			r.TargetExisting.Directories = 1
			entries, e := storageEntries(op.path.root)
			if e != nil {
				return nil, e
			}
			for _, entry := range entries {
				if entry.Name() != dataMarkerName {
					return nil, withCode(CodeBadRequest, errors.New("the destination already has data; choose an empty folder"))
				}
			}
			if len(entries) != 0 {
				raw, e := readPrivateData(op.path.root, dataMarkerName, 4096)
				if e != nil {
					return nil, e
				}
				var marker dataMarker
				if decodeDataJSON(raw, &marker) != nil || marker.Version != 1 || marker.Owner != op.preparation.server.storageOwner() {
					return nil, errStorageCustody
				}
				named, e := storageOpenFile(op.path.root, dataMarkerName, int64(len(raw)))
				if e != nil {
					return nil, e
				}
				r.MarkerBefore, e = storageOwnerPersistentFile(ctx, named)
				e = errors.Join(e, named.current(), named.file.Close())
				if e != nil {
					return nil, e
				}
				r.TargetExisting.Bytes = uint64(len(raw))
				r.TargetExisting.Files = 1
			}
		} else {
			r.TargetBefore = storageOwnerPersistentObject{}
			suffix := strings.Join(op.path.names[op.path.missing:], string(filepath.Separator))
			prefix := len(wire.Path) - len(suffix)
			if prefix < 0 || !strings.HasSuffix(wire.Path, suffix) || prefix > 0 && wire.Path[prefix-1] != filepath.Separator {
				return nil, storagebudget.ErrBounds
			}
			r.ExistingPrefix = uint16(prefix)
			r.Descendants = make([]storageTransferDescendant, len(op.path.names)-op.path.missing)
		}
		r.TargetEmpty = true
		if r.MarkerBefore == (storageOwnerPersistentObject{}) {
			marker, e := json.Marshal(dataMarker{Version: 1, Owner: op.preparation.server.storageOwner()})
			if e != nil {
				return nil, e
			}
			r.MarkerBytes = uint64(len(marker))
			r.MarkerDigest = sha256.Sum256(marker)
		}
		r.Tail, err = p.originalCreationTail(ctx, r.Operation)
		if err != nil {
			return nil, err
		}
	} else {
		r.Tail = storageCreationTail{}
		r.Parent = p.c.ref.cell.ownerRecord.Config
		r.Basis = r.Parent
	}
	// Select one existing reusable original host slot. No nonterminal operation
	// is ignored in another slot, even if this request could otherwise fit.
	err = o.withControl(ctx, func(controlCtx context.Context, table *storageIntentTable) error {
		status, e := o.ref.cell.q.InspectHostState(controlCtx)
		if e != nil {
			return e
		}
		if status.Epoch != r.Epoch || status.Maintenance {
			return errStorageEnrollmentUnresolved
		}
		if e = o.validateIntentMappings(controlCtx, table, status); e != nil {
			return e
		}
		chosen := -1
		for i, slot := range table.slots {
			if slot.record.Phase != storageIntentEmpty && !slot.record.reusable() {
				return errStorageEnrollmentUnresolved
			}
			if chosen < 0 {
				chosen = i
			}
		}
		if chosen < 0 {
			return storagebudget.ErrBounds
		}
		op.host = table.slots[chosen]
		r.HostSlot = uint16(chosen)
		r.HostGeneration = op.host.record.Generation + 1
		if r.HostGeneration == 0 {
			return storagebudget.ErrBounds
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	op.controlOriginal = p.original
	op.record = r
	if err = op.captureOriginalTransferBanks(ctx); err != nil {
		return nil, err
	}
	r.Previous = op.record.Previous
	r.Groups, err = storageTransferChargeBoard(r, op.catalog)
	if err != nil {
		return nil, err
	}
	for i := range r.Outputs {
		output := &r.Outputs[i]
		output.State = 0
		output.Native = storageOwnerPersistentObject{}
		category := byte(storagebudget.History)
		if memoryFileName.MatchString(output.Name) {
			category = storagebudget.Memory
		}
		if attachmentFileName.MatchString(output.Name) {
			category = storagebudget.Attachments
		}
		if assistantDialectReadRecoveryFileName.MatchString(output.Name) {
			category = storagebudget.Control
		}
		found := false
		target := r.Parent
		if r.TargetBefore.valid(1) {
			target = r.TargetBefore
		}
		for index, group := range r.Groups {
			if group.Native == target && group.Component.Category == category && group.Flags != storageTransferNoReferences {
				if found {
					return nil, storagebudget.ErrAuthority
				}
				output.Group = byte(index)
				found = true
			}
		}
		if !found {
			return nil, storagebudget.ErrAuthority
		}
	}
	r.Changes = make([]storagebudget.Change, len(r.Groups))
	r.Plan, err = storageTransferPlanDigest(r)
	if err != nil {
		return nil, err
	}
	raw, err := storageTransferEncode(r)
	if err != nil {
		return nil, err
	}
	op.record, err = storageTransferDecode(raw)
	if err != nil {
		return nil, err
	}
	op.request, err = storageTransferOriginalRequest(op.record, op.catalog)
	if err != nil {
		return nil, err
	}
	if err = op.freezeOriginalTransferOrdinals(); err != nil {
		return nil, err
	}
	pass, err := op.beginOriginalTransferCapacityPass(ctx, nil, nil)
	if err != nil {
		return nil, err
	}
	observations, err = pass.observations()
	op.capacityAt = pass.completed
	err = errors.Join(err, pass.Close())
	if err != nil {
		return nil, err
	}
	if r.Mode != storageTransferExport {
		observations, err = storageCreationCapacityRoom(r.Tail, observations)
		if err != nil {
			return nil, err
		}
		anchor := storageEnrollmentPeakQHeldAnchor{Installation: storageEnrollmentPeakQID(r.Installation), Binding: storageEnrollmentPeakQDigest(r.QBinding)}
		q, e := storageEnrollmentPeakQDecodeCheckpoint(p.initialInspection.Checkpoints[p.initialInspection.CheckpointBank], anchor)
		if e != nil {
			return nil, e
		}
		if err = storageCreationLogicalRoom(r.Tail, op.request.Components, q); err != nil {
			return nil, err
		}
	}
	if err = storageTransferInitialCapacityRoom(r, op.catalog, observations); err != nil {
		return nil, err
	}
	if err = op.currentBeforeEffect(ctx); err != nil {
		return nil, err
	}
	op.tailAdmitted = r.Mode != storageTransferExport
	return observations, nil
}

// One complete reservation. The owner-selected pre-control grant is the same
// frozen plan/observation operand, established before either Prepared write.
// No caller can substitute a component set, observations or a second ticket.
func (op *chatTransferOperation) reserveWhole(ctx context.Context) (err error) {
	observations, err := op.sealOriginalTransferPlan(ctx)
	if err != nil {
		return err
	}
	comparison, err := op.preparation.registry.owner.captureOriginalReserve(ctx, op.request, nil, op.record.Operation, nil)
	if err != nil {
		return err
	}
	first := 0
	if !storageAllZero(op.fullBanks[0]) || !storageAllZero(op.fullBanks[1]) {
		pair, e := storageTransferDecodePair(op.fullBanks)
		if e != nil || pair.RecoveryOnly {
			return errors.Join(errStorageEnrollmentUnresolved, e)
		}
		first = 1 - pair.Selected
	}
	prepared := op.record
	if err = op.publishOriginalTransferBank(ctx, prepared, first, 1, false, nil); err != nil {
		return err
	}
	if err = op.publishOriginalTransferBank(ctx, prepared, 1-first, 2, false, nil); err != nil {
		return err
	}
	if err = op.publishOriginalTransferHost(ctx, storageIntentPrepared, storageIntentNoRejection, 17, nil); err != nil {
		return err
	}
	if err = op.takeOriginalTransferControl(ctx, []uint16{11, 12}, nil); err != nil {
		return err
	}
	if err = op.enterOriginalTransferCall(ctx, nil); err != nil {
		return err
	}
	token, reserveErr := op.preparation.registry.owner.ref.cell.q.ReserveCompared(ctx, op.request, observations, comparison)
	err = op.leaveOriginalTransferCall(reserveErr)
	// Only this actual call sets these fields; a decoded Rejected page, a caller
	// error argument, context value or copied completion cannot fabricate them.
	op.reserveReturned, op.reserveError = true, reserveErr
	if err != nil {
		if ctx.Err() != nil || storageDefinitelyRejected(reserveErr) == storageIntentNoRejection {
			return errors.Join(err, ctx.Err())
		}
		if e := op.enterOriginalTransferCall(ctx, nil); e != nil {
			return errors.Join(err, e)
		}
		checked, e := op.preparation.registry.owner.ref.cell.q.InspectHostState(ctx)
		e = op.leaveOriginalTransferCall(e)
		if e != nil || !storageTransferNoTicket(op.record, checked) {
			return errors.Join(err, errStorageEnrollmentUnresolved, e)
		}
		// Retain the actual complete detached same-load inspection. It grants only
		// this original Prepared->Rejected publication, never data or fresh capacity.
		op.rejectedProof = &checked
		rejected := op.record
		rejected.Generation++
		rejected.Phase = storageTransferRejected
		pair, e := storageTransferDecodePair(op.fullBanks)
		if e != nil {
			return errors.Join(err, e)
		}
		if e = op.publishOriginalTransferBank(ctx, rejected, 1-pair.Selected, 5, true, nil); e != nil {
			return errors.Join(err, e)
		}
		if e = op.publishOriginalTransferHost(ctx, storageIntentRejected, storageDefinitelyRejected(reserveErr), 18, nil); e != nil {
			return errors.Join(err, e)
		}
		dependency, e := storageTransferNamespaceDependency(op.host.record, op.catalog)
		if e != nil {
			return errors.Join(err, e)
		}
		op.namespaceDependency = dependency
		if dependency == nil {
			op.preparation.completed = true
			return err
		}
		op.mu.Lock()
		p := op.preparation
		p.mu.Lock()
		if op.completion != nil || op.completing || op.finished || p.revoked || p.finishing || p.finished || p.ctx.Err() != nil {
			p.mu.Unlock()
			op.mu.Unlock()
			return errors.Join(err, errStorageEnrollmentUnresolved)
		}
		completion := &chatTransferCompletion{operation: op, generation: p.epoch.generation, plan: op.record.Plan, initial: storagebudget.Token{}}
		completion.self = completion
		completion.consumed = op.controlConsumed
		op.completion = completion
		p.mu.Unlock()
		op.mu.Unlock()
		return err
	}
	ticket, err := op.originalTransferTicket(ctx, token, storagebudget.Reserved, 0)
	if err != nil {
		return err
	}
	if token.Generation > ^uint64(0)-2 {
		return storagebudget.ErrBounds
	}
	reserved := op.record
	reserved.Generation++
	reserved.Phase = storageTransferReserved
	reserved.Token = token
	reserved.InitialToken = token
	if ticket.PolicyRevision != reserved.PolicyRevision {
		return errStorageEnrollmentUnresolved
	}
	pair, err := storageTransferDecodePair(op.fullBanks)
	if err != nil {
		return err
	}
	if err = op.publishOriginalTransferBank(ctx, reserved, 1-pair.Selected, 5, true, nil); err != nil {
		return err
	}
	if err = op.publishOriginalTransferHost(ctx, storageIntentReserved, storageIntentNoRejection, 18, nil); err != nil {
		return err
	}
	op.mu.Lock()
	p := op.preparation
	p.mu.Lock()
	if op.completion != nil || op.completing || op.finished || p.revoked || p.finishing || p.finished || p.ctx.Err() != nil {
		p.mu.Unlock()
		op.mu.Unlock()
		return os.ErrClosed
	}
	completion := &chatTransferCompletion{operation: op, generation: p.epoch.generation, plan: op.record.Plan, initial: token}
	completion.self = completion
	completion.consumed = op.controlConsumed
	op.completion = completion
	p.mu.Unlock()
	op.mu.Unlock()
	if err = op.takeOriginalTransferControl(ctx, []uint16{13}, nil); err != nil {
		return err
	}
	if err = op.enterOriginalTransferCall(ctx, nil); err != nil {
		return err
	}
	advanced, e := p.registry.owner.ref.cell.q.Advance(ctx, token, op.record.QBinding, storagebudget.Staging, op.record.Plan)
	err = op.leaveOriginalTransferCall(e)
	if err != nil {
		return err
	}
	ticket, err = op.originalTransferTicket(ctx, advanced, storagebudget.Staging, 1)
	if err != nil {
		return err
	}
	if advanced.ID != token.ID || advanced.Slot != token.Slot || advanced.Sequence != token.Sequence || advanced.Generation != token.Generation+1 || ticket.Evidence != op.record.Plan {
		return errStorageEnrollmentUnresolved
	}
	data := op.record
	data.Generation++
	data.Phase = storageTransferDataIntent
	data.Token = advanced
	pair, err = storageTransferDecodePair(op.fullBanks)
	if err != nil {
		return err
	}
	if err = op.publishOriginalTransferBank(ctx, data, 1-pair.Selected, 6, true, nil); err != nil {
		return err
	}
	return op.publishOriginalTransferHost(ctx, storageIntentStaging, storageIntentNoRejection, 19, nil)
}

// Historical native reattachment is a read of the selected original catalog,
// not adoption by a matching path/marker. The complete held-name chain and
// stored persistent directory/marker/domain identities must all agree. This
// path also preserves captures older than the pointer's one Previous field.
func (x *storageOriginalSources) exactCapturedTransferRoot(ctx context.Context, catalog storageOwnerCatalogV2, row storageOwnerRoot) (_ *storageOriginalSourceRoot, err error) {
	if x == nil || x.construction == nil || ctx == nil || !x.originalOwnerEqual(catalog) || int(row.DirectoryObject) >= len(catalog.Objects) || len(x.roots) >= storagebudget.MaxRoots {
		return nil, storagebudget.ErrAuthority
	}
	captured := row.Role == 4 && row.Source.Kind == 11
	ancestor := row.Role == 8 && row.Source.Kind == 9
	historical := row.Role == 4 && row.Source.Kind == 4 && row.Source.ControlKind == 2 && row.Source.ControlSelection == 3
	if !captured && !ancestor && !historical {
		return nil, errStorageCustody
	}
	var scope *storageEnrollmentPeakSource
	if captured || ancestor {
		source, e := storageEnrollmentPeakDecodeSource(row.Source.Relative)
		if e != nil || source.Default == nil || !storageEnrollmentPeakSourceJoins(source, catalog, row) {
			return nil, errors.Join(storagebudget.ErrAuthority, e)
		}
		if captured {
			if e = storageTransferCapturedRootJoins(catalog, row); e != nil {
				return nil, e
			}
		} else if source.Default.Workflow != 4 || source.Default.OwnSelf != 0 || row.Definition.Identity != storageEnrollmentAncestorSourceIdentity(source, row.Source.Relative) {
			return nil, storagebudget.ErrAuthority
		}
		scope = &source
	} else if e := storageEnrollmentPeakRootRole(catalog, row); e != nil {
		return nil, e
	}
	native := catalog.Objects[row.DirectoryObject].Persistent
	if !native.valid(1) {
		return nil, storagebudget.ErrAuthority
	}
	for _, prior := range x.roots {
		if prior.persistent != native {
			continue
		}
		priorView := prior.role == 8 && prior.scope != nil && prior.scope.Default != nil && prior.scope.Default.Workflow == 4 && prior.scope.Default.OwnSelf == 0
		if ancestor {
			if priorView {
				if bytes.Equal(prior.scope.Default.Next, scope.Default.Next) {
					if reflect.DeepEqual(*prior.scope, *scope) && reflect.DeepEqual(prior.workspaces, row.References) {
						return prior, prior.current(ctx, x.construction)
					}
					// Same-Next rows require the exact bounded append-only retirement chain;
					// active duplicate/cohort substitution is refused by that relation.
					if _, e := storageTransferNamespaceRelations(catalog); e != nil {
						return nil, e
					}
					oldRetained := false
					oldRaw, e := storageEnrollmentPeakEncodeSource(*prior.scope)
					if e != nil {
						return nil, e
					}
					for _, candidate := range catalog.Roots {
						if storageTransferCatalogView(candidate) && bytes.Equal(candidate.Source.Relative, oldRaw) && (candidate.Source.ControlSelection == 4 || row.Source.ControlSelection == 4) {
							oldRetained = true
						}
					}
					if !oldRetained {
						return nil, errStorageCustody
					}
					continue
				}
				continue
			}
			if prior.role == 8 && (prior.scope == nil || prior.scope.Default == nil || prior.scope.Default.OwnSelf != 1) {
				return nil, errStorageCustody
			}
			continue // an owning directory and its bounded views have separate custody
		}
		if priorView {
			continue
		}
		return nil, errStorageCustody
	}
	// Source ownership is independently joined to an original held settings
	// parent. A different owner's row cannot borrow this installation's lock.
	settings := storageOwnerPersistentObject{}
	if scope != nil {
		settings = scope.Default.Settings
	} else {
		if int(row.Source.ControlIndex) >= len(catalog.OwnerControls) {
			return nil, storagebudget.ErrBounds
		}
		control := catalog.OwnerControls[row.Source.ControlIndex]
		if control.Role != 2 || string(control.Name) != dataLocationName || !bytes.Equal(control.OwnerKey, row.Source.OwnerKey) || int(control.ParentObject) >= len(catalog.Objects) {
			return nil, storagebudget.ErrAuthority
		}
		settings = catalog.Objects[control.ParentObject].Persistent
	}
	settingsFound := false
	for _, selection := range x.selections {
		actual, e := storageOwnerPersistentRoot(ctx, selection.settings)
		if e != nil {
			return nil, e
		}
		if actual == settings {
			if settingsFound {
				return nil, storagebudget.ErrAuthority
			}
			settingsFound = true
		}
	}
	if !settingsFound {
		return nil, storagebudget.ErrAuthority
	}
	if !ancestor {
		if row.DomainObject == storageOwnerNoObject || int(row.DomainObject) >= len(catalog.Objects) {
			return nil, storagebudget.ErrAuthority
		}
		domain := catalog.Objects[row.DomainObject]
		config, e := storageOwnerPersistentRoot(ctx, x.construction.original.config)
		if e != nil || domain.Role != 18 || string(domain.Name) != ".data.lock" || int(domain.Parent) >= len(catalog.Objects) || catalog.Objects[domain.Parent].Persistent != config {
			return nil, errors.Join(storagebudget.ErrAuthority, e)
		}
	}
	path := string(row.Source.Path)
	parts, e := storageTransferPathParts(path)
	if ancestor && path == "/" {
		parts = []string{}
		e = nil
	}
	if e != nil {
		return nil, e
	}
	base, e := os.OpenRoot(string(filepath.Separator))
	if e != nil {
		return nil, e
	}
	roots := []*os.Root{base}
	edges := []storageNamedRoot{}
	parent := base
	var marker storageNamedFile
	accepted := false
	defer func() {
		if !accepted {
			if marker.file != nil {
				err = errors.Join(err, marker.file.Close())
			}
			for i := len(roots) - 1; i >= 0; i-- {
				err = errors.Join(err, roots[i].Close())
			}
		}
	}()
	for i, name := range parts {
		if e = ctx.Err(); e != nil {
			return nil, e
		}
		edge, e := storagePinChild(parent, name, !ancestor && i == len(parts)-1)
		if e != nil {
			return nil, e
		}
		edges = append(edges, edge)
		roots = append(roots, edge.root)
		parent = edge.root
	}
	actual, e := storageOwnerPersistentRoot(ctx, parent)
	if e != nil || actual != native {
		return nil, errors.Join(errStorageCustody, e)
	}
	markerNative := storageOwnerPersistentObject{}
	markerLength := uint64(0)
	if !ancestor {
		for _, object := range catalog.Objects {
			if object.Role == 23 && object.Parent == row.DirectoryObject && string(object.Name) == dataMarkerName {
				if markerNative != (storageOwnerPersistentObject{}) {
					return nil, storagebudget.ErrAuthority
				}
				markerNative = object.Persistent
				markerLength = object.FixedLength
			}
		}
		if scope != nil && scope.Default.Marker != (storageOwnerPersistentObject{}) && markerNative != scope.Default.Marker {
			return nil, storagebudget.ErrAuthority
		}
		if markerNative != (storageOwnerPersistentObject{}) {
			if markerLength < 1 || markerLength > 4096 {
				return nil, storagebudget.ErrBounds
			}
			marker, e = storageOpenFile(parent, dataMarkerName, int64(markerLength))
			if e != nil {
				return nil, e
			}
			actual, e := storageOwnerPersistentFile(ctx, marker)
			if e != nil || actual != markerNative {
				return nil, errors.Join(errStorageCustody, e)
			}
			raw := make([]byte, markerLength)
			n, e := marker.file.ReadAt(raw, 0)
			if e != nil || n != len(raw) {
				return nil, errors.Join(storagebudget.ErrUnknown, e)
			}
			var value dataMarker
			if decodeDataJSON(raw, &value) != nil || value.Version != 1 || value.Owner != string(row.Source.OwnerKey) {
				return nil, errStorageCustody
			}
			marker.immutable = raw
		} else {
			if _, e = parent.Lstat(dataMarkerName); !errors.Is(e, os.ErrNotExist) {
				return nil, errors.Join(errStorageCustody, e)
			}
		}
	}
	if scope != nil {
		if _, e = storageTransferVerifyChainAt(ctx, *scope, base, edges); e != nil {
			return nil, e
		}
	}
	source := &storageOriginalSourceRoot{root: parent, witnesses: edges, path: path, key: string(row.Source.OwnerKey), role: row.Role, category: row.Category, persistent: native, workspaces: append([]storagebudget.ID{}, row.References...), scope: scope}
	if ancestor {
		source.relative = string(scope.Default.Next)
	}
	if marker.file != nil {
		source.marker = &marker
	}
	if e = source.current(ctx, x.construction); e != nil {
		return nil, e
	}
	for _, edge := range edges {
		if e = edge.current(); e != nil {
			return nil, e
		}
	}
	x.roots = append(x.roots, source)
	x.openedRoots = append(x.openedRoots, roots...)
	if marker.file != nil {
		x.openedFiles = append(x.openedFiles, marker.file)
	}
	accepted = true
	return source, nil
}

// The index contains only bounded catalog-derived views. No second ownership
// registry is introduced, and an OwnSelf0 anchor is never an owning root.
func storageTransferNamespaceBinding(r *storageRootBinding) bool {
	return r != nil && r.source != nil && r.source.role == 8 && r.source.scope != nil && r.source.scope.Default != nil && r.source.scope.Default.Workflow == 4 && r.source.scope.Default.OwnSelf == 0 && r.restrictedNamespace == string(r.source.scope.Default.Next) && storageEnrollmentDefaultSourceNamespace(*r.source.scope)
}
func (w *storageCensusWalk) transferNamespaceClaim(anchor storageDirectoryIdentity, name string) *storageRootBinding {
	if w == nil || w.namespaces == nil {
		return nil
	}
	return w.namespaces[anchor][name]
}

// The transfer-only walk retains the same entry/time/native observation limits
// and every live named edge. Its explicit stack covers at most8191 captured
// descendants/16KiB of relative names; unrelated roots keep generic64/1024.
// A nested nonowning anchor never removes itself or unclaimed siblings from
// the enclosing owner: only its exact Next child is carved at that anchor.
func (w *storageCensusWalk) transferDirectory(r *storageRootBinding, first storageNamedRoot, relative string, count *int, usage *[storagebudget.Categories]storagebudget.Vector) (err error) {
	if !storageTransferNamespaceBinding(r) || first.root == nil || count == nil {
		return storagebudget.ErrAuthority
	}
	type frame struct {
		edge         storageNamedRoot
		relative     string
		before       os.FileInfo
		entries      []os.DirEntry
		next         int
		identity     storageDirectoryIdentity
		genericDepth int
		genericName  string
		extended     bool
	}
	captured := func(edge storageNamedRoot) bool {
		parent, e := storageFileIdentityFromRoot(edge.parent)
		if e != nil {
			return false
		}
		child, e := storageFileIdentityFromRoot(edge.root)
		if e != nil {
			return false
		}
		return w.transferEdges[storageTransferCensusEdge{parent: parent, child: child, name: edge.name}]
	}
	firstCaptured := captured(first)
	stack := []frame{{edge: first, relative: relative, genericDepth: 1, genericName: relative, extended: firstCaptured}}
	// The caller owns first. Every deeper edge is joined and closed exactly once
	// on both successful return and failed observation; no stack unwind recursion.
	defer func() {
		for i := len(stack) - 1; i > 0; i-- {
			err = errors.Join(err, stack[i].edge.current(), stack[i].edge.root.Close())
		}
	}()
	for len(stack) > 0 {
		at := len(stack) - 1
		f := &stack[at]
		if err = w.ctx.Err(); err != nil {
			return err
		}
		if len(stack) >= storageTransferDescendantSlots || len(f.relative) > storageTransferPointerBytes || !f.extended && (f.genericDepth > storageCensusDepth || len(f.genericName) > storageCensusName) {
			return storagebudget.ErrBounds
		}
		if w.exclusive != nil {
			if err = w.exclusive.permit(w.ctx); err != nil {
				return err
			}
		}
		if f.before == nil {
			f.before, err = f.edge.root.Stat(".")
			if err != nil || !storagePrivate(f.before, true) {
				return errors.Join(errStorageCustody, err)
			}
			f.identity, err = storageFileIdentity(f.before)
			if err != nil {
				return err
			}
			if w.directories[f.identity] {
				return storagebudget.ErrUnknown
			}
			w.directories[f.identity] = true
			if err = w.add(r, f.relative, f.identity, 0, true, usage); err != nil {
				return err
			}
			// The entire parent chain stays live in stack; each edge is rechecked on
			// exit. The observation borrows this exact parent and its immediate edge.
			checkChain := func() error {
				for ordinal := 0; ordinal < (len(stack)+31)/32; ordinal++ {
					first, last, e := storageTransferParentWindow(len(stack), ordinal)
					if e != nil {
						return e
					}
					for i := first; i < last; i++ {
						if e = stack[i].edge.current(); e != nil {
							return e
						}
					}
				}
				return nil
			}
			if err = checkChain(); err != nil {
				return err
			}
			if err = w.sampleDirectory(r, f.edge.root, []storageNamedRoot{f.edge}); err != nil {
				return err
			}
			if err = checkChain(); err != nil {
				return err
			}
			file, e := f.edge.root.Open(".")
			if e != nil {
				return e
			}
			f.entries, e = file.ReadDir(storageCensusEntries - w.entries + 1)
			closeErr := file.Close()
			if e != nil && !errors.Is(e, io.EOF) || closeErr != nil {
				return errors.Join(e, closeErr)
			}
			if len(f.entries) > storageCensusEntries-w.entries {
				return storagebudget.ErrBounds
			}
			w.entries += len(f.entries)
			sort.Slice(f.entries, func(i, j int) bool { return f.entries[i].Name() < f.entries[j].Name() })
		}
		if f.next == len(f.entries) {
			after, e := f.edge.root.Stat(".")
			if e != nil || !os.SameFile(f.before, after) || after.Mode() != f.before.Mode() || !after.ModTime().Equal(f.before.ModTime()) {
				return errors.Join(storagebudget.ErrUnknown, e)
			}
			w.observation.observe(r, f.relative, f.before, after)
			e = errors.Join(f.edge.current(), r.current(w.ctx))
			if at > 0 {
				e = errors.Join(e, f.edge.root.Close())
			}
			stack = stack[:at]
			if e != nil {
				return e
			}
			continue
		}
		entry := f.entries[f.next]
		f.next++
		name := entry.Name()
		childName := f.relative + "/" + name
		if len(childName) > storageTransferPointerBytes {
			return storagebudget.ErrBounds
		}
		info, e := f.edge.root.Lstat(name)
		if e != nil {
			return e
		}
		id, e := storageFileIdentity(info)
		if e != nil {
			return e
		}
		if info.IsDir() {
			if view := w.transferNamespaceClaim(f.identity, name); view != nil && view != r {
				if e = view.current(w.ctx); e != nil {
					return e
				}
				continue
			}
			if owner := w.roots[id]; owner != nil && owner != r {
				if e = owner.current(w.ctx); e != nil {
					return e
				}
				continue
			}
			child, e := storagePinChild(f.edge.root, name, true)
			if e != nil {
				return e
			}
			extended := f.extended && captured(child)
			genericDepth := f.genericDepth + 1
			genericName := f.genericName + "/" + name
			if f.extended && !extended {
				genericDepth = 1
				genericName = name
			}
			stack = append(stack, frame{edge: child, relative: childName, genericDepth: genericDepth, genericName: genericName, extended: extended})
			continue
		}
		*count++
		if *count > maxStorageFiles {
			return storagebudget.ErrBounds
		}
		if !storageCensusPrivateFile(info) {
			return storagebudget.ErrUnknown
		}
		covered, e := r.censusCovered(w.ctx, childName, info)
		if e != nil {
			return e
		}
		if !covered {
			if e = w.add(r, childName, id, uint64(info.Size()), false, usage); e != nil {
				return e
			}
		}
		after, e := f.edge.root.Lstat(name)
		if e != nil || !os.SameFile(info, after) || after.Size() != info.Size() || after.Mode() != info.Mode() || !after.ModTime().Equal(info.ModTime()) {
			return errors.Join(storagebudget.ErrUnknown, e)
		}
		w.observation.observe(r, childName, info, after)
	}
	return r.current(w.ctx)
}

func storageTransferCatalogView(r storageOwnerRoot) bool {
	if r.Role != 8 || r.Source.Kind != 9 {
		return false
	}
	source, err := storageEnrollmentPeakDecodeSource(r.Source.Relative)
	return err == nil && source.Default != nil && source.Default.Workflow == 4 && source.Default.OwnSelf == 0
}

// Chain commitments use no new catalog object/root rows. This temporary census
// index is derived only from validated sources and their live retained edges.
// It is never persisted or consulted as an independent source of ownership.
type storageTransferCensusEdge struct {
	parent, child storageDirectoryIdentity
	name          string
}

func storageTransferChainDigest(source storageEnrollmentPeakSource, anchor storageOwnerPersistentObject, names []string, natives []storageOwnerPersistentObject) (storagebudget.Digest, error) {
	d := source.Default
	if d == nil || d.Workflow != 4 && d.Workflow != 5 && d.Workflow != 6 || len(names) != len(natives) || len(names) >= storageTransferDescendantSlots || int(d.ChainCount) != len(names) || !anchor.valid(1) || len(natives) > 0 && natives[len(natives)-1] != d.Selected || len(natives) == 0 && anchor != d.Selected {
		return storagebudget.Digest{}, storagebudget.ErrAuthority
	}
	parts, err := storageTransferSourceChainParts(d)
	if err != nil || !reflect.DeepEqual(parts, names) {
		return storagebudget.Digest{}, errors.Join(storagebudget.ErrAuthority, err)
	}
	copyDefault := *d
	copyDefault.ChainDigest = storagebudget.Digest{}
	source.Default = &copyDefault
	source.CapacityBinding = storagebudget.Digest{} // mapping subsequently binds the completed chain digest; no circular preimage
	raw, err := json.Marshal(source)
	if err != nil {
		return storagebudget.Digest{}, err
	}
	h := sha256.New()
	h.Write([]byte("desk-transfer-complete-held-chain-v1\x00"))
	var length [4]byte
	binary.LittleEndian.PutUint32(length[:], uint32(len(raw)))
	h.Write(length[:])
	h.Write(raw)
	h.Write(storageEnrollmentPeakNativeBytes(anchor))
	for i, name := range names {
		if !natives[i].valid(1) {
			return storagebudget.Digest{}, storagebudget.ErrAuthority
		}
		binary.LittleEndian.PutUint32(length[:], uint32(len(name)))
		h.Write(length[:])
		h.Write([]byte(name))
		h.Write(storageEnrollmentPeakNativeBytes(natives[i]))
	}
	var out storagebudget.Digest
	copy(out[:], h.Sum(nil))
	return out, nil
}
func storageTransferVerifyChain(ctx context.Context, source storageEnrollmentPeakSource, edges []storageNamedRoot) ([]storageTransferCensusEdge, error) {
	if len(edges) == 0 {
		return nil, storagebudget.ErrAuthority
	}
	return storageTransferVerifyChainAt(ctx, source, edges[0].parent, edges)
}
func storageTransferVerifyChainAt(ctx context.Context, source storageEnrollmentPeakSource, base *os.Root, edges []storageNamedRoot) ([]storageTransferCensusEdge, error) {
	if ctx == nil || source.Default == nil || int(source.Default.ChainCount) != len(edges) || len(edges) >= storageTransferDescendantSlots || source.Default.ChainDigest == (storagebudget.Digest{}) {
		return nil, storagebudget.ErrAuthority
	}
	parts, err := storageTransferSourceChainParts(source.Default)
	if err != nil || len(parts) != len(edges) {
		return nil, errors.Join(storagebudget.ErrAuthority, err)
	}
	if base == nil {
		return nil, storagebudget.ErrAuthority
	}
	anchor, err := storageOwnerPersistentRoot(ctx, base)
	if err != nil {
		return nil, err
	}
	natives := make([]storageOwnerPersistentObject, len(edges))
	out := make([]storageTransferCensusEdge, len(edges))
	parent := base
	check := func(first, last int) error {
		for i := first; i < last; i++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			if edges[i].parent == nil || edges[i].root == nil || edges[i].name != parts[i] {
				return storagebudget.ErrAuthority
			}
			if err := edges[i].current(); err != nil {
				return err
			}
		}
		return nil
	}
	for ordinal := 0; ordinal < (len(edges)+31)/32; ordinal++ {
		first, last, e := storageTransferParentWindow(len(edges), ordinal)
		if e != nil {
			return nil, e
		}
		if e = check(first, last); e != nil {
			return nil, e
		}
		for i := first; i < last; i++ {
			edge := edges[i]
			if edge.parent != parent {
				return nil, storagebudget.ErrAuthority
			}
			natives[i], e = storageOwnerPersistentRoot(ctx, edge.root)
			if e != nil {
				return nil, e
			}
			pi, e := storageFileIdentityFromRoot(edge.parent)
			if e != nil {
				return nil, e
			}
			ci, e := storageFileIdentityFromRoot(edge.root)
			if e != nil {
				return nil, e
			}
			out[i] = storageTransferCensusEdge{parent: pi, child: ci, name: edge.name}
			parent = edge.root
		}
		if e = check(first, last); e != nil {
			return nil, e
		}
	}
	digest, err := storageTransferChainDigest(source, anchor, parts, natives)
	if err != nil || digest != source.Default.ChainDigest {
		return nil, errors.Join(errStorageCustody, err)
	}
	// A later window cannot change an earlier edge before the full commitment is
	// accepted. Verify the complete live chain again in the same finite windows.
	for ordinal := 0; ordinal < (len(edges)+31)/32; ordinal++ {
		first, last, e := storageTransferParentWindow(len(edges), ordinal)
		if e != nil {
			return nil, e
		}
		if e = check(first, last); e != nil {
			return nil, e
		}
	}
	return out, nil
}
func storageFileIdentityFromRoot(root *os.Root) (storageDirectoryIdentity, error) {
	info, err := root.Stat(".")
	if err != nil {
		return storageDirectoryIdentity{}, err
	}
	return storageFileIdentity(info)
}
func (w *storageCensusWalk) captureOriginalTransferChains(roots []*storageRootBinding) error {
	if w == nil || w.ctx == nil {
		return storagebudget.ErrAuthority
	}
	w.transferEdges = map[storageTransferCensusEdge]bool{}
	for _, root := range roots {
		if root == nil || root.source == nil || root.source.scope == nil || root.source.scope.Default == nil {
			continue
		}
		source := root.source.scope
		if source.Default.Workflow != 4 && source.Default.Workflow != 5 && source.Default.Workflow != 6 {
			continue
		}
		if !storageEnrollmentDefaultSourceNamespace(*source) {
			return storagebudget.ErrAuthority
		}
		if err := root.current(w.ctx); err != nil {
			return err
		}
		edges, err := storageTransferVerifyChainAt(w.ctx, *source, func() *os.Root {
			if len(root.source.witnesses) == 0 {
				return root.source.root
			}
			return root.source.witnesses[0].parent
		}(), root.source.witnesses)
		if err != nil {
			return err
		}
		for _, edge := range edges {
			w.transferEdges[edge] = true
		}
	}
	return nil
}

// Retired-empty is a catalog consequence of an exclusive proven successor,
// never an accounting override. The prior row keeps its exact source bytes,
// definition, native identity and Q registration; only scalar4 marks its view
// after the source-bound transition. Each successor retains the exact
// prior root ID in CreationPage; LocationPayload remains canonically zero.
// Completed before lineage authenticates old retirement after physical bank reuse.
func storageTransferNamespaceID(id storagebudget.ID) storagebudget.Digest {
	return storageEnrollmentPeakHash("desk-transfer-prior-namespace-id-v1", id[:])
}
func storageTransferNamespaceRelations(c storageOwnerCatalogV2) (map[storagebudget.ID]storagebudget.ID, error) {
	type view struct {
		row    storageOwnerRoot
		source storageEnrollmentPeakSource
	}
	views := map[storagebudget.ID]view{}
	keys := map[storageOwnerPersistentObject]map[string]storagebudget.ID{}
	for _, row := range c.Roots {
		if !storageTransferCatalogView(row) {
			continue
		}
		source, err := storageEnrollmentPeakDecodeSource(row.Source.Relative)
		if err != nil || row.Source.ControlSelection != 0 && row.Source.ControlSelection != 4 || int(row.DirectoryObject) >= len(c.Objects) {
			return nil, storagebudget.ErrBounds
		}
		views[row.Definition.ID] = view{row, source}
		if row.Source.ControlSelection == 4 {
			continue
		}
		native := c.Objects[row.DirectoryObject].Persistent
		if keys[native] == nil {
			keys[native] = map[string]storagebudget.ID{}
		}
		name := string(source.Default.Next)
		if keys[native][name] != (storagebudget.ID{}) {
			return nil, storagebudget.ErrBounds
		}
		keys[native][name] = row.Definition.ID
	}
	links := map[storagebudget.ID]storagebudget.ID{}
	claimed := map[storagebudget.ID]bool{}
	for id, v := range views {
		d := v.source.Default
		if d.CreationPage == (storagebudget.Digest{}) {
			if d.LocationPayload != (storagebudget.Digest{}) {
				return nil, storagebudget.ErrBounds
			}
			continue
		}
		priorID := storagebudget.ID{}
		for candidate := range views {
			if storageTransferNamespaceID(candidate) == d.CreationPage {
				if priorID != (storagebudget.ID{}) {
					return nil, storagebudget.ErrBounds
				}
				priorID = candidate
			}
		}
		old, ok := views[priorID]
		if !ok || priorID == id || claimed[priorID] || old.row.Source.ControlSelection != 4 || d.LocationPayload != (storagebudget.Digest{}) || old.source.Default.Selected != d.Selected || !bytes.Equal(old.source.Default.SelectedPath, d.SelectedPath) || !bytes.Equal(old.source.Default.Next, d.Next) {
			return nil, storagebudget.ErrAuthority
		}
		links[id] = priorID
		claimed[priorID] = true
	}
	for id := range views {
		seen := map[storagebudget.ID]bool{}
		for next := id; next != (storagebudget.ID{}); next = links[next] {
			if seen[next] {
				return nil, storagebudget.ErrAuthority
			}
			seen[next] = true
		}
	}
	return links, nil
}

func storageTransferCompactNoTicket(core storageIntentRecord, state storagebudget.HostState, sameEpoch bool) bool {
	if core.Transfer == nil || core.OperationID == (storagebudget.ID{}) || core.CASBinding == (storagebudget.Digest{}) || core.AdmissionEpoch == 0 || state.Maintenance || sameEpoch && state.Epoch != core.AdmissionEpoch {
		return false
	}
	for _, slot := range state.Slots {
		ticket := slot.Ticket
		if ticket.State != storagebudget.Empty && ticket.Workflow == storageTransferWorkflow && ticket.Operation == core.Operation && len(core.Components) > 0 && ticket.Owner == core.Components[0].Owner {
			return false
		}
	}
	return true
}
func storageTransferNamespaceBefore(e storageEnrollmentPeakEvidence) (*storageOwnerCatalogV2, bool) {
	var before *storageOwnerCatalogV2
	for i := range e.Projections {
		if storageEnrollmentPeakProjectionFullDigest(e.Projections[i]) == e.Before {
			if before != nil {
				return nil, false
			}
			before = &e.Projections[i].Catalog
		}
	}
	return before, before != nil
}
func storageTransferNamespaceProof(e storageEnrollmentPeakEvidence) bool {
	if _, err := storageTransferNamespaceRelations(e.Inner.Catalog); err != nil {
		return false
	}
	retired := false
	for _, row := range e.Inner.Catalog.Roots {
		retired = retired || storageTransferCatalogView(row) && row.Source.ControlSelection == 4
	}
	if !retired {
		return true
	}
	before, ok := storageTransferNamespaceBefore(e)
	if !ok || e.Checkpoint.Q == nil || int(e.Checkpoint.View) >= len(e.Views) {
		return false
	}
	current := storageEnrollmentPeakHostState(e.Views[e.Checkpoint.View])
	changed := 0
	for _, row := range e.Inner.Catalog.Roots {
		if !storageTransferCatalogView(row) || row.Source.ControlSelection != 4 {
			continue
		}
		old, exists := storageTransferCatalogRoot(*before, row.Definition.ID)
		if !exists || !storageTransferCatalogView(old) || old.Definition != row.Definition || !bytes.Equal(old.Source.Relative, row.Source.Relative) || !reflect.DeepEqual(old.References, row.References) {
			return false
		}
		checkpointZero, censusZero := false, false
		for _, root := range e.Checkpoint.Q.Roots {
			if root.ID == storageEnrollmentPeakQID(row.Definition.ID) {
				if !root.Known || root.Usage != ([storageEnrollmentPeakQCategories]storageEnrollmentPeakQVector{}) {
					return false
				}
				checkpointZero = true
			}
		}
		for _, root := range e.Census.Roots {
			if root.ID == storageEnrollmentPeakQID(row.Definition.ID) {
				if root.Usage != ([storageEnrollmentPeakQCategories]storageEnrollmentPeakQVector{}) {
					return false
				}
				censusZero = true
			}
		}
		if !checkpointZero || !censusZero {
			return false
		}
		for _, slot := range current.Slots {
			if !storageUnretired(slot) {
				continue
			}
			for _, component := range slot.Ticket.Components {
				if component.Root == row.Definition.ID {
					return false
				}
			}
		}
		if old.Source.ControlSelection == 4 {
			continue
		} // exact completed-before inheritance
		if old.Source.ControlSelection != 0 {
			return false
		}
		changed++
		source, err := storageEnrollmentPeakDecodeSource(old.Source.Relative)
		if err != nil {
			return false
		}
		witnessFound := false
		for _, w := range e.Witnesses {
			core := w.Core
			if !w.Selected || core.Transfer == nil || core.Phase != storageIntentRejected && !(core.Phase == storageIntentRetired && core.Outcome == storageIntentAborted && core.Transfer.Cleanup.complete() && core.Transfer.PointerState == 0) || core.Transfer.Tail.Bridge != e.Bridge || int(w.Origin) >= len(e.Board.Origins) || w.Digest != sha256.Sum256(w.Page) {
				continue
			}
			origin := e.Board.Origins[w.Origin]
			if origin.Epoch != core.AdmissionEpoch || int(origin.View) >= len(e.Views) || int(origin.Projection) >= len(e.Projections) {
				return false
			}
			original := e.Projections[origin.Projection].Catalog
			dependency, e0 := storageTransferNamespaceDependency(core, original)
			if e0 != nil || dependency == nil || !reflect.DeepEqual(*dependency, source) || !storageTransferNamespaceActionApplied(core, original, storageEnrollmentPeakHostState(e.Views[origin.View])) || core.Transfer.Settings != source.Default.Settings || core.Transfer.Source != source.Default.SourceNative {
				return false
			}
			originalRow, exists := storageTransferCatalogRoot(original, row.Definition.ID)
			if !exists || !reflect.DeepEqual(originalRow, old) {
				return false
			}
			if witnessFound {
				return false
			}
			witnessFound = true
		}
		if !witnessFound {
			return false
		}
	}
	if changed > 1 {
		return false
	}
	// Retired rows never revive, even when their same-Next chain is extended.
	for _, old := range before.Roots {
		if storageTransferCatalogView(old) && old.Source.ControlSelection == 4 {
			next, exists := storageTransferCatalogRoot(e.Inner.Catalog, old.Definition.ID)
			if !exists || next.Source.ControlSelection != 4 {
				return false
			}
		}
	}
	return true
}

// Closed data board: one mutually-exclusive input/archive stage; every mkdir;
// the optional marker; every sorted output (including zero bytes); one pointer
// publication or one synchronous export serving entry. No ordinal can replay.
type storageTransferOrdinal struct {
	kind  byte
	index int
}

const (
	storageTransferStageOrdinal     byte = 1
	storageTransferDirectoryOrdinal byte = 2
	storageTransferMarkerOrdinal    byte = 3
	storageTransferOutputOrdinal    byte = 4
	storageTransferPointerOrdinal   byte = 5
	storageTransferServingOrdinal   byte = 6
)

func (op *chatTransferOperation) freezeOriginalTransferOrdinals() error {
	if op == nil || op.self != op || op.record.Phase != storageTransferPrepared || op.board != nil || op.results != nil || op.next != 0 || op.preparation.admitted {
		return storagebudget.ErrAuthority
	}
	r := op.record
	board := []storageTransferOrdinal{}
	if r.Mode == storageTransferRestore || r.Mode == storageTransferExport {
		board = append(board, storageTransferOrdinal{kind: storageTransferStageOrdinal})
	}
	for i := range r.Descendants {
		board = append(board, storageTransferOrdinal{kind: storageTransferDirectoryOrdinal, index: i})
	}
	if r.MarkerBytes > 0 {
		board = append(board, storageTransferOrdinal{kind: storageTransferMarkerOrdinal})
	}
	for i := range r.Outputs {
		board = append(board, storageTransferOrdinal{kind: storageTransferOutputOrdinal, index: i})
	}
	last := byte(storageTransferPointerOrdinal)
	if r.Mode == storageTransferExport {
		last = storageTransferServingOrdinal
	}
	board = append(board, storageTransferOrdinal{kind: last})
	if len(board) > storageTransferDescendantSlots+maxStorageFiles+3 {
		return storagebudget.ErrBounds
	}
	raw, err := storageTransferEncode(r)
	if err != nil {
		return err
	}
	op.returned, err = storageTransferDecode(raw)
	if err != nil {
		return err
	}
	op.board = board
	op.results = make([]byte, len(board))
	op.capacityChecks = make([]bool, len(board))
	op.effectFiles = map[int]storageNamedFile{}
	return nil
}
func (op *chatTransferOperation) enterOriginalTransferOrdinal(ctx context.Context, kind byte, index int) (int, error) {
	if op == nil || op.self != op || op.record.Phase != storageTransferDataIntent && !(kind == storageTransferPointerOrdinal && op.record.Phase == storageTransferPointerIntent) {
		return 0, storagebudget.ErrAuthority
	}
	if err := op.refreshOriginalTransferCapacity(ctx, kind, index); err != nil {
		return 0, err
	}
	if err := op.enterOriginalTransferCall(ctx, nil); err != nil {
		return 0, err
	}
	op.mu.Lock()
	ordinal := op.next
	if ordinal >= len(op.board) || ordinal >= len(op.results) || op.board[ordinal] != (storageTransferOrdinal{kind: kind, index: index}) || op.results[ordinal] != 0 {
		op.mu.Unlock()
		return 0, op.leaveOriginalTransferCall(storagebudget.ErrAuthority)
	}
	op.next++
	op.results[ordinal] = 1
	op.mu.Unlock()
	return ordinal, nil
}
func (op *chatTransferOperation) leaveOriginalTransferOrdinal(ordinal int, err error) error {
	op.mu.Lock()
	if ordinal < 0 || ordinal >= len(op.results) || op.results[ordinal] != 1 {
		err = errors.Join(err, storagebudget.ErrAuthority)
	} else if err == nil {
		op.results[ordinal] = 2
	}
	op.mu.Unlock()
	return op.leaveOriginalTransferCall(err)
}
func storageTransferSyncDirectory(ctx context.Context, root *os.Root) (err error) {
	if err = ctx.Err(); err != nil {
		return err
	}
	file, err := root.Open(".")
	if err != nil {
		return err
	}
	err = storageEnrollmentNativeSync(ctx, file)
	return errors.Join(err, file.Close())
}
func storageTransferVerifyFile(ctx context.Context, n storageNamedFile, size uint64, digest storagebudget.Digest) error {
	if size > 1<<60 || digest == (storagebudget.Digest{}) || n.file == nil || n.size != int64(size) {
		return storagebudget.ErrAuthority
	}
	h := sha256.New()
	buffer := make([]byte, 65536)
	for offset := uint64(0); offset < size; {
		if err := ctx.Err(); err != nil {
			return err
		}
		last := min(uint64(len(buffer)), size-offset)
		got, err := n.file.ReadAt(buffer[:last], int64(offset))
		if err != nil || got != int(last) {
			return errors.Join(storagebudget.ErrUnknown, err, io.ErrUnexpectedEOF)
		}
		h.Write(buffer[:last])
		offset += last
	}
	var extra [1]byte
	got, err := n.file.ReadAt(extra[:], int64(size))
	if got != 0 || !errors.Is(err, io.EOF) {
		return errors.Join(storagebudget.ErrUnknown, err)
	}
	var actual storagebudget.Digest
	copy(actual[:], h.Sum(nil))
	if actual != digest {
		return errStorageCustody
	}
	return n.current()
}
func (op *chatTransferOperation) originalTransferTarget() (*os.Root, error) {
	if op == nil || op.path == nil || op.path.self != op.path {
		return nil, storagebudget.ErrAuthority
	}
	if len(op.created) != len(op.returned.Descendants) {
		return nil, storagebudget.ErrUnknown
	}
	if len(op.created) == 0 {
		return op.path.root, nil
	}
	for _, edge := range op.created {
		if err := edge.current(); err != nil {
			return nil, err
		}
	}
	return op.created[len(op.created)-1].root, nil
}
func (op *chatTransferOperation) createOriginalTransferDirectories(ctx context.Context) (err error) {
	if op == nil || op.path == nil {
		return storagebudget.ErrAuthority
	}
	parent := op.path.root
	for index := range op.returned.Descendants {
		ordinal, e := op.enterOriginalTransferOrdinal(ctx, storageTransferDirectoryOrdinal, index)
		if e != nil {
			return e
		}
		e = func() error {
			if len(op.created) != index || op.path.missing+index >= len(op.path.names) {
				return storagebudget.ErrAuthority
			}
			name := op.path.names[op.path.missing+index]
			if _, e := parent.Lstat(name); !errors.Is(e, os.ErrNotExist) {
				return errors.Join(errStorageCustody, e)
			}
			op.returned.Descendants[index].State = 1
			if e := parent.Mkdir(name, 0700); e != nil {
				return e
			}
			edge, e := storagePinChild(parent, name, true)
			if e != nil {
				return e
			}
			op.created = append(op.created, edge)
			native, e := storageOwnerPersistentRoot(ctx, edge.root)
			if e != nil {
				return e
			}
			basis := op.record.Basis
			basis.Object = native.Object
			basis.Incarnation = native.Incarnation
			if basis != native {
				return errStorageCustody
			}
			if e = storageTransferSyncDirectory(ctx, edge.root); e != nil {
				return e
			}
			if e = storageTransferSyncDirectory(ctx, parent); e != nil {
				return e
			}
			if e = edge.current(); e != nil {
				return e
			}
			op.returned.Descendants[index] = storageTransferDescendant{Object: native.Object, Incarnation: native.Incarnation, State: 2}
			parent = edge.root
			return nil
		}()
		if e = op.leaveOriginalTransferOrdinal(ordinal, e); e != nil {
			return e
		}
		if testOriginalChatTransferNativeCut != nil {
			if e = testOriginalChatTransferNativeCut(storageTransferDirectoryReturnedCut); e != nil {
				return e
			}
		}
	}
	return nil
}

// This leaf accepts bytes only for the exact next immutable planned object.
// Names, parents, lengths and digests are selected internally. No general write
// permit or arbitrary native description crosses the boundary.
func (op *chatTransferOperation) writeOriginalTransferBytes(ctx context.Context, kind byte, index int, data []byte) (err error) {
	ordinal, err := op.enterOriginalTransferOrdinal(ctx, kind, index)
	if err != nil {
		return err
	}
	defer func() { err = op.leaveOriginalTransferOrdinal(ordinal, err) }()
	var parent *os.Root
	var name string
	var size uint64
	var digest storagebudget.Digest
	switch kind {
	case storageTransferOutputOrdinal:
		if index < 0 || index >= len(op.returned.Outputs) {
			return storagebudget.ErrAuthority
		}
		output := &op.returned.Outputs[index]
		name, size, digest = output.Name, output.Bytes, output.SHA256
		output.State = 1
		parent, err = op.originalTransferTarget()
	case storageTransferMarkerOrdinal:
		if index != 0 || op.record.MarkerBytes == 0 {
			return storagebudget.ErrAuthority
		}
		parent, err = op.originalTransferTarget()
		name, size, digest = dataMarkerName, op.record.MarkerBytes, op.record.MarkerDigest
		op.returned.MarkerState = 1
	case storageTransferStageOrdinal:
		if index != 0 || op.record.Mode != storageTransferRestore {
			return storagebudget.ErrAuthority
		}
		parent = op.preparation.epoch.c.original.config
		name, err = storageTransferStageName(op.record)
		size, digest = op.record.InputBytes, op.record.InputDigest
		op.returned.ArchiveState = 1
	default:
		return storagebudget.ErrAuthority
	}
	if err != nil {
		return err
	}
	if uint64(len(data)) != size || sha256.Sum256(data) != digest {
		return errStorageCustody
	}
	file, err := parent.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	writerClosed := false
	defer func() {
		if !writerClosed {
			writerClosed = true
			err = errors.Join(err, file.Close())
			if receipt, ok := op.effectFiles[ordinal]; ok {
				receipt.file = nil
				op.effectFiles[ordinal] = receipt
			}
		}
	}()
	info, err := file.Stat()
	if err != nil || !storagePrivate(info, false) {
		return errors.Join(errStorageCustody, err)
	}
	named := storageNamedFile{root: parent, name: name, file: file, identity: info, size: 0}
	native, err := storageOwnerPersistentFile(ctx, named)
	if err != nil {
		return err
	}
	op.effectFiles[ordinal] = named
	for offset := 0; offset < len(data); offset += 65536 {
		last := min(offset+65536, len(data))
		if err = storageEnrollmentNativeWrite(ctx, file, data[offset:last], int64(offset)); err != nil {
			return err
		}
	}
	if err = storageEnrollmentNativeSync(ctx, file); err != nil {
		return err
	}
	writerClosed = true
	err = file.Close()
	receipt := op.effectFiles[ordinal]
	receipt.file = nil
	op.effectFiles[ordinal] = receipt
	if err != nil {
		return err
	}
	readback, err := storageOpenFile(parent, name, int64(size))
	if err != nil {
		return err
	}
	actual, e := storageOwnerPersistentFile(ctx, readback)
	if e == nil && actual != native {
		e = errStorageCustody
	}
	if e == nil {
		e = storageTransferVerifyFile(ctx, readback, size, digest)
	}
	e = errors.Join(e, readback.file.Close())
	if e != nil {
		return e
	}
	if err = storageTransferSyncDirectory(ctx, parent); err != nil {
		return err
	}
	// Retain the immutable receipt, never the now-closed writer as a borrower.
	named.size = int64(size)
	named.file = nil
	op.effectFiles[ordinal] = named
	switch kind {
	case storageTransferOutputOrdinal:
		op.returned.Outputs[index].Native = native
		op.returned.Outputs[index].State = 2
	case storageTransferMarkerOrdinal:
		op.returned.MarkerNative = native
		op.returned.MarkerState = 2
	case storageTransferStageOrdinal:
		op.returned.ArchiveNative = native
		op.returned.ArchiveState = 2
		stage, e := storageOpenFile(parent, name, int64(size))
		if e != nil {
			return e
		}
		actual, e := storageOwnerPersistentFile(ctx, stage)
		if e != nil || actual != native {
			return errors.Join(errStorageCustody, e, stage.file.Close())
		}
		op.stage = &stage
	}
	return nil
}

func (w *storageCensusWalk) sampleOriginalTransferDirectory(r *storageRootBinding, root *os.Root, witnesses []storageNamedRoot) (err error) {
	op := w.transfer
	if op == nil || op.self != op || op.preparation.registry.owner != w.owner || root == nil {
		return storagebudget.ErrAuthority
	}
	if err = op.originalReadCurrent(w.ctx); err != nil {
		return err
	}
	found := false
	for _, original := range op.preparation.epoch.originalRoots {
		if original == r {
			found = true
		}
	}
	if !found {
		return storagebudget.ErrAuthority
	}
	if root != r.root {
		if len(witnesses) == 0 || witnesses[len(witnesses)-1].root != root {
			return storagebudget.ErrAuthority
		}
	}
	for _, edge := range witnesses {
		if err = edge.current(); err != nil {
			return err
		}
	}
	observer, ok := w.owner.observer.(*storageAdvisoryObserver)
	if !ok {
		return storagebudget.ErrAuthority
	}
	// This is the census's bounded read-only observation. It supplies neither a
	// Reserve operand nor a new parent/root to the operation's frozen board.
	pass := storageTransferCapacityPass{operation: op}
	if _, err = pass.query(w.ctx, observer, r, root); err != nil {
		return err
	}
	for _, edge := range witnesses {
		if err = edge.current(); err != nil {
			return err
		}
	}
	return op.originalReadCurrent(w.ctx)
}

func (op *chatTransferOperation) publishOriginalTransferPointer(ctx context.Context) (err error) {
	if op == nil || op.self != op || op.record.Mode == storageTransferExport || op.settings == nil || op.returned.PointerState != 0 || op.record.Phase != storageTransferDataIntent {
		return storagebudget.ErrAuthority
	}
	for _, output := range op.returned.Outputs {
		if output.State != 2 {
			return storagebudget.ErrUnknown
		}
	}
	if op.record.MarkerBytes > 0 && op.returned.MarkerState != 2 {
		return storagebudget.ErrUnknown
	}
	target, err := op.originalTransferTarget()
	if err != nil {
		return err
	}
	if err = validateStoredBindings(target); err != nil {
		return err
	}
	if testBeforeDataMoveCommit != nil {
		if err = testBeforeDataMoveCommit(); err != nil {
			return err
		}
	}
	intent := op.record
	intent.Generation++
	intent.Phase = storageTransferPointerIntent
	pair, err := storageTransferDecodePair(op.fullBanks)
	if err != nil {
		return err
	}
	if err = op.publishOriginalTransferBank(ctx, intent, 1-pair.Selected, 7, true, nil); err != nil {
		return err
	}
	if err = op.publishOriginalTransferHost(ctx, storageIntentPublishing, storageIntentNoRejection, 20, nil); err != nil {
		return err
	}
	ordinal, err := op.enterOriginalTransferOrdinal(ctx, storageTransferPointerOrdinal, 0)
	if err != nil {
		return err
	}
	defer func() { err = op.leaveOriginalTransferOrdinal(ordinal, err) }()
	stage := ".data-transfer-" + storageHexID(op.record.Operation) + "-pointer"
	file, err := op.settings.OpenFile(stage, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			closed = true
			err = errors.Join(err, file.Close())
		}
	}()
	for offset := 0; offset < len(op.record.NewPointer); offset += 65536 {
		last := min(offset+65536, len(op.record.NewPointer))
		if err = storageEnrollmentNativeWrite(ctx, file, op.record.NewPointer[offset:last], int64(offset)); err != nil {
			return err
		}
	}
	if err = storageEnrollmentNativeSync(ctx, file); err != nil {
		return err
	}
	closed = true
	if err = file.Close(); err != nil {
		return err
	}
	verified, err := storageOpenFile(op.settings, stage, int64(len(op.record.NewPointer)))
	if err != nil {
		return err
	}
	native, e := storageOwnerPersistentFile(ctx, verified)
	if e == nil {
		e = storageTransferVerifyFile(ctx, verified, uint64(len(op.record.NewPointer)), sha256.Sum256(op.record.NewPointer))
	}
	e = errors.Join(e, verified.file.Close())
	if e != nil {
		return e
	}
	// Last pre-Rename observation consumes the exact old raw pointer image. The
	// transition never substitutes canonicalized JSON or a remembered revision.
	raw, e := readPrivateData(op.settings, dataLocationName, storageTransferPointerBytes)
	if op.record.OldAbsent {
		if !errors.Is(e, os.ErrNotExist) {
			return errors.Join(errStorageCustody, e)
		}
	} else if e != nil || !bytes.Equal(raw, op.record.OldPointer) {
		return errors.Join(errStorageCustody, e)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	op.returned.PointerState = 1 // immediately before actual Rename; never decreases
	if err = op.settings.Rename(stage, dataLocationName); err != nil {
		return err
	}
	if err = storageTransferSyncDirectory(ctx, op.settings); err != nil {
		return err
	}
	named, err := storageOpenFile(op.settings, dataLocationName, int64(len(op.record.NewPointer)))
	if err != nil {
		return err
	}
	actual, e := storageOwnerPersistentFile(ctx, named)
	if e == nil && actual != native {
		e = errStorageCustody
	}
	if e == nil {
		e = storageTransferVerifyFile(ctx, named, uint64(len(op.record.NewPointer)), sha256.Sum256(op.record.NewPointer))
	}
	e = errors.Join(e, named.file.Close())
	if e != nil {
		return e
	}
	op.returned.PointerNative = native
	op.returned.PointerState = 2
	return nil
}
func (op *chatTransferOperation) closeOriginalTransferData() (err error) {
	if op == nil || op.self != op {
		return storagebudget.ErrAuthority
	}
	err = errors.Join(err, op.closeOriginalTransferStage())
	if op.oldPointer != nil && !op.oldPointerClosed {
		op.oldPointerClosed = true
		op.oldPointerErr = errors.Join(op.oldPointerErr, op.oldPointer.file.Close())
		op.oldPointer.file = nil
		err = errors.Join(err, op.oldPointerErr)
	}
	for i := len(op.created) - 1; i >= 0; i-- {
		err = errors.Join(err, op.created[i].current(), op.created[i].root.Close())
	}
	op.created = nil
	if op.path != nil {
		err = errors.Join(err, op.path.Close())
		op.path = nil
	}
	op.input = nil
	return err
}

func (op *chatTransferOperation) removeOriginalTransferFile(ctx context.Context, parent *os.Root, name string, size uint64, native storageOwnerPersistentObject, completion *chatTransferCompletion) (err error) {
	if !native.valid(2) || parent == nil || size > 1<<60 {
		return storagebudget.ErrAuthority
	}
	if err = op.enterOriginalTransferCall(ctx, completion); err != nil {
		return err
	}
	defer func() { err = op.leaveOriginalTransferCall(err) }()
	held, err := storageOpenFile(parent, name, int64(size))
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			closed = true
			err = errors.Join(err, held.file.Close())
		}
	}()
	actual, err := storageOwnerPersistentFile(ctx, held)
	if err != nil || actual != native {
		return errors.Join(errStorageCustody, err)
	}
	if err = parent.Remove(name); err != nil {
		return err
	}
	info, err := held.file.Stat()
	if err != nil {
		return err
	}
	links, known := linkCount(info)
	if !known || links != 0 || info.Size() != int64(size) {
		return storagebudget.ErrUnknown
	}
	closed = true
	if err = held.file.Close(); err != nil {
		return err
	}
	if err = storageTransferSyncDirectory(ctx, parent); err != nil {
		return err
	}
	if _, err = parent.Lstat(name); !errors.Is(err, os.ErrNotExist) {
		return errors.Join(errStorageCustody, err)
	}
	return nil
}
func (op *chatTransferOperation) closeOriginalTransferPointerProof(ctx context.Context, completion *chatTransferCompletion) (err error) {
	if op.oldPointerClosed {
		return errors.Join(storagebudget.ErrAuthority, op.oldPointerErr)
	}
	if err = op.enterOriginalTransferCall(ctx, completion); err != nil {
		return err
	}
	defer func() { err = op.leaveOriginalTransferCall(err) }()
	if op.record.OldAbsent {
		if op.oldPointer != nil {
			return storagebudget.ErrAuthority
		}
		if op.returned.PointerState == 0 {
			if _, e := op.settings.Lstat(dataLocationName); !errors.Is(e, os.ErrNotExist) {
				return errors.Join(errStorageCustody, e)
			}
		}
		op.oldPointerClosed = true
		return nil
	}
	original := op.oldPointer
	if original == nil || original.file == nil {
		return storagebudget.ErrAuthority
	}
	info, err := original.file.Stat()
	if err == nil && (!os.SameFile(info, original.identity) || info.Size() != int64(len(op.record.OldPointer))) {
		err = errStorageCustody
	}
	if err == nil {
		links, known := linkCount(info)
		if op.returned.PointerState == 2 {
			if !known || links != 0 {
				err = storagebudget.ErrUnknown
			}
		} else if op.returned.PointerState == 0 {
			err = original.current()
			if err == nil {
				raw := make([]byte, len(op.record.OldPointer))
				n, e := original.file.ReadAt(raw, 0)
				if e != nil || n != len(raw) || !bytes.Equal(raw, op.record.OldPointer) {
					err = errors.Join(errStorageCustody, e)
				}
			}
		} else {
			err = storagebudget.ErrUnknown
		}
	}
	op.oldPointerClosed = true
	op.oldPointerErr = errors.Join(err, original.file.Close())
	original.file = nil
	return op.oldPointerErr
}
func (op *chatTransferOperation) cleanupOriginalTransfer(ctx context.Context, completion *chatTransferCompletion) (err error) {
	if op.returned.PointerState == 1 {
		return storagebudget.ErrUnknown
	}
	for _, result := range op.results {
		if result == 1 {
			return storagebudget.ErrUnknown
		}
	}
	// Close the last actual stage reader/serving borrower before unlink credit.
	if e := op.closeOriginalTransferStage(); e != nil {
		return e
	}
	positive := op.returned.PointerState == 2
	target := (*os.Root)(nil)
	if op.record.Mode != storageTransferExport && !positive && len(op.created) == len(op.returned.Descendants) {
		target, err = op.originalTransferTarget()
		if err != nil {
			return err
		}
	}
	summary := storageTransferCleanup{Outputs: uint16(len(op.returned.Outputs)), Directories: uint16(len(op.returned.Descendants)), Stage: 4, Marker: 4, Closure: 7}
	for ordinal, step := range op.board {
		result := op.results[ordinal]
		if result == 0 {
			op.results[ordinal] = 4
			switch step.kind {
			case storageTransferOutputOrdinal:
				op.returned.Outputs[step.index].State = 4
				summary.OutputsNotEntered++
			case storageTransferDirectoryOrdinal:
				op.returned.Descendants[step.index] = storageTransferDescendant{State: 3}
				summary.DirectoriesNotEntered++
			case storageTransferStageOrdinal:
				op.returned.ArchiveState = 4
			case storageTransferMarkerOrdinal:
				op.returned.MarkerState = 4
			}
			continue
		}
		switch step.kind {
		case storageTransferStageOrdinal:
			name, e := storageTransferStageName(op.record)
			if e != nil {
				return e
			}
			size := op.record.InputBytes
			if op.record.Mode == storageTransferExport {
				size = op.record.ArchiveBytes
			}
			if e = op.removeOriginalTransferFile(ctx, op.preparation.epoch.c.original.config, name, size, op.returned.ArchiveNative, completion); e != nil {
				return e
			}
			op.returned.ArchiveState = 3
			summary.Stage = 3
		case storageTransferOutputOrdinal:
			if positive {
				continue
			}
			output := &op.returned.Outputs[step.index]
			if target == nil || output.State != 2 {
				return storagebudget.ErrUnknown
			}
			if e := op.removeOriginalTransferFile(ctx, target, output.Name, output.Bytes, output.Native, completion); e != nil {
				return e
			}
			output.State = 3
			summary.OutputsRemoved++
		case storageTransferMarkerOrdinal:
			if positive {
				continue
			}
			if target == nil || op.returned.MarkerState != 2 {
				return storagebudget.ErrUnknown
			}
			if e := op.removeOriginalTransferFile(ctx, target, dataMarkerName, op.record.MarkerBytes, op.returned.MarkerNative, completion); e != nil {
				return e
			}
			op.returned.MarkerState = 3
			summary.Marker = 3
		}
	}
	if positive || op.record.Mode == storageTransferExport {
		return nil
	}
	// Reverse completion preserves every still-needed parent description. Each
	// created directory has one actual Close, one Remove and one parent Sync.
	for index := len(op.created) - 1; index >= 0; index-- {
		if err = op.enterOriginalTransferCall(ctx, completion); err != nil {
			return err
		}
		edge := op.created[index]
		e := func() error {
			if e := edge.current(); e != nil {
				return e
			}
			file, e := edge.root.Open(".")
			if e != nil {
				return e
			}
			native, e := storageAdvisoryPersistentHeld(ctx, file)
			receipt := op.returned.Descendants[index]
			expected := op.record.Basis
			expected.Object, expected.Incarnation = receipt.Object, receipt.Incarnation
			if e != nil || native != expected {
				return errors.Join(errStorageCustody, e, file.Close())
			}
			// Pop only after Close returns, including its error, so final cleanup never
			// closes this same description twice or invents a missing Close fact.
			closeErr := edge.root.Close()
			op.created = op.created[:index]
			if closeErr != nil {
				return errors.Join(closeErr, file.Close())
			}
			if e = edge.parent.Remove(edge.name); e != nil {
				return errors.Join(e, file.Close())
			}
			info, e := file.Stat()
			if e != nil {
				return errors.Join(e, file.Close())
			}
			links, known := linkCount(info)
			if !known || links != 0 {
				return errors.Join(storagebudget.ErrUnknown, file.Close())
			}
			if e = file.Close(); e != nil {
				return e
			}
			if e = storageTransferSyncDirectory(ctx, edge.parent); e != nil {
				return e
			}
			if _, e = edge.parent.Lstat(edge.name); !errors.Is(e, os.ErrNotExist) {
				return errors.Join(errStorageCustody, e)
			}
			op.returned.Descendants[index] = storageTransferDescendant{State: 3}
			summary.DirectoriesRemoved++
			return nil
		}()
		if e = op.leaveOriginalTransferCall(e); e != nil {
			return e
		}
	}
	// The fixed summary is finalized only after the complete actual census below.
	op.returned.Cleanup = summary
	return nil
}
func (op *chatTransferOperation) originalTransferChanges() (out []storagebudget.Change, err error) {
	r := op.returned
	out = make([]storagebudget.Change, len(op.record.Groups))
	if r.Mode == storageTransferExport {
		if !storageTransferReturnedBoardKnown(op.board, op.results) || r.ArchiveState != 3 && r.ArchiveState != 4 {
			return nil, storagebudget.ErrUnknown
		}
		return out, nil
	}
	if r.PointerState == 0 {
		if !r.Cleanup.complete() || !storageTransferReturnedBoardKnown(op.board, op.results) {
			return nil, storagebudget.ErrUnknown
		}
		for ordinal, step := range op.board {
			if step.kind == storageTransferPointerOrdinal && op.results[ordinal] != 4 {
				return nil, storagebudget.ErrUnknown
			}
		}
		for _, output := range r.Outputs {
			if output.State != 3 && output.State != 4 {
				return nil, storagebudget.ErrUnknown
			}
		}
		for _, dir := range r.Descendants {
			if dir.State != 3 {
				return nil, storagebudget.ErrUnknown
			}
		}
		if r.MarkerBytes > 0 && r.MarkerState != 3 && r.MarkerState != 4 || r.InputBytes > 0 && r.ArchiveState != 3 && r.ArchiveState != 4 {
			return nil, storagebudget.ErrUnknown
		}
		return out, nil
	}
	if r.PointerState != 2 || !op.oldPointerClosed || op.oldPointerErr != nil {
		return nil, storagebudget.ErrUnknown
	}
	for _, output := range r.Outputs {
		if output.State != 2 {
			return nil, storagebudget.ErrUnknown
		}
	}
	for _, dir := range r.Descendants {
		if dir.State != 2 {
			return nil, storagebudget.ErrUnknown
		}
	}
	if r.MarkerBytes > 0 && r.MarkerState != 2 || r.InputBytes > 0 && r.ArchiveState != 3 {
		return nil, storagebudget.ErrUnknown
	}
	var settings *storageOwnerRoot
	var source *storageOwnerRoot
	for i := range op.catalog.Roots {
		row := &op.catalog.Roots[i]
		if storageTransferCatalogView(*row) {
			continue
		}
		native := op.catalog.Objects[row.DirectoryObject].Persistent
		if native == r.Settings {
			settings = row
		}
		if native == r.Source {
			source = row
		}
	}
	if settings == nil || source == nil {
		return nil, storagebudget.ErrAuthority
	}
	removeAssigned := false
	for i, g := range op.record.Groups {
		if g.Component.Category == storagebudget.Transient {
			continue
		}
		out[i].PhysicalAdd = g.Component.Physical
		out[i].AttributionAdd = g.Component.Attribution
		if r.OldAbsent || g.Native != r.Settings || g.Component.Category != storagebudget.Control {
			continue
		}
		old := storagebudget.Vector{Bytes: uint64(len(r.OldPointer)), Files: 1}
		if !removeAssigned {
			out[i].PhysicalRemove = old
			removeAssigned = true
		}
		if settings.Role == 1 {
			continue
		}
		references, e := storageTransferSelectedReferences(settings.References, source.References, g.Flags)
		if e != nil {
			return nil, e
		}
		if len(references) > 0 {
			for _, id := range references {
				found := false
				for _, oldID := range settings.References {
					found = found || id == oldID
				}
				if !found {
					return nil, storagebudget.ErrAuthority
				}
			}
			out[i].AttributionRemove = old
		}
	}
	if !r.OldAbsent && !removeAssigned {
		return nil, storagebudget.ErrAuthority
	}
	return out, nil
}

// This once-only continuation is called by the owning lexical invocation after
// every data/serving borrower actually returns. Live normal completion keeps
// the original caller/Close cancellation. Already failed/cancelled callbacks
// get the existing two-second cleanup dispatch window; neither path bounds
// actual native completion or mandatory owned Close.
func (op *chatTransferOperation) completeObserved(callbackErr error) (err error) {
	if op == nil || op.self != op {
		return storagebudget.ErrAuthority
	}
	op.mu.Lock()
	if op.completing || op.finished || op.entered {
		op.mu.Unlock()
		return storagebudget.ErrAuthority
	}
	completion := op.completion
	if completion == nil {
		op.mu.Unlock()
		if op.preparation.admitted && !op.preparation.completed {
			return errStorageEnrollmentUnresolved
		}
		return nil
	}
	op.completing = true
	op.mu.Unlock()
	defer func() { completion.finished = true }()
	// Choose once. Cancellation after live selection stops new dispatch; it
	// never renews this consumed continuation with a detached cleanup attempt.
	ctx := op.preparation.ctx
	if callbackErr != nil || ctx.Err() != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.WithoutCancel(op.preparation.request.Context()), 2*time.Second)
		defer cancel()
	}
	op.completionContext = ctx
	if err = completion.current(ctx); err != nil {
		return err
	}
	if op.record.Phase == storageTransferRejected {
		return op.completeOriginalTransferNamespaceRetirement(ctx)
	}
	if op.record.Phase != storageTransferDataIntent && op.record.Phase != storageTransferPointerIntent {
		return errStorageEnrollmentUnresolved
	}
	if err = op.cleanupOriginalTransfer(ctx, completion); err != nil {
		return errors.Join(errStorageEnrollmentUnresolved, err)
	}
	if op.record.Mode != storageTransferExport {
		if err = op.closeOriginalTransferPointerProof(ctx, completion); err != nil {
			return errors.Join(errStorageEnrollmentUnresolved, err)
		}
	}
	census, err := op.preparation.registry.owner.measureCensusAllTransfer(ctx, op.preparation.epoch.originalRoots, nil, nil, nil, op)
	if err != nil {
		return errors.Join(errStorageEnrollmentUnresolved, err)
	}
	if op.record.Mode != storageTransferExport && op.returned.PointerState == 0 {
		summary := op.returned.Cleanup
		raw, e := json.Marshal(struct {
			Plan          storagebudget.Digest
			Results       []byte
			Outputs       []storageTransferOutput
			Directories   []storageTransferDescendant
			Marker, Stage byte
			Census        storagebudget.Digest
		}{op.record.Plan, op.results, op.returned.Outputs, op.returned.Descendants, op.returned.MarkerState, op.returned.ArchiveState, census.Evidence})
		if e != nil {
			return e
		}
		summary.Effects = storageEnrollmentPeakHash("desk-transfer-complete-cleanup-census-v1", raw)
		if !summary.complete() {
			return storagebudget.ErrUnknown
		}
		op.returned.Cleanup = summary
	}
	changes, err := op.originalTransferChanges()
	if err != nil {
		return err
	}
	if op.record.Phase == storageTransferDataIntent {
		pointer := op.record
		pointer.Generation++
		pointer.Phase = storageTransferPointerIntent
		pair, e := storageTransferDecodePair(op.fullBanks)
		if e != nil {
			return e
		}
		if err = op.publishOriginalTransferBank(ctx, pointer, 1-pair.Selected, 7, true, completion); err != nil {
			return err
		}
		if err = op.publishOriginalTransferHost(ctx, storageIntentPublishing, storageIntentNoRejection, 20, completion); err != nil {
			return err
		}
	}
	intent := op.record
	intent.Generation++
	intent.Phase = storageTransferCompletionIntent
	pair, err := storageTransferDecodePair(op.fullBanks)
	if err != nil {
		return err
	}
	if err = op.publishOriginalTransferBank(ctx, intent, 1-pair.Selected, 8, true, completion); err != nil {
		return err
	}
	observed := op.returned
	observed.Generation = op.record.Generation + 1
	observed.Phase = storageTransferObserved
	observed.Token = op.record.Token
	observed.InitialToken = op.record.InitialToken
	observed.Plan = op.record.Plan
	observed.Changes = changes
	pair, err = storageTransferDecodePair(op.fullBanks)
	if err != nil {
		return err
	}
	bank := 1 - pair.Selected
	if err = op.publishOriginalTransferBank(ctx, observed, bank, 3, false, completion); err != nil {
		return err
	}
	if err = op.publishOriginalTransferBank(ctx, op.record, 1-bank, 4, false, completion); err != nil {
		return err
	}
	if err = op.publishOriginalTransferHost(ctx, storageIntentSettling, storageIntentNoRejection, 21, completion); err != nil {
		return err
	}
	evidence, err := storageTransferSettlementEvidence(op.record, op.catalog)
	if err != nil {
		return err
	}
	if err = op.takeOriginalTransferControl(ctx, []uint16{14, 15}, completion); err != nil {
		return err
	}
	if err = op.enterOriginalTransferCall(ctx, completion); err != nil {
		return err
	}
	settled, e := op.preparation.registry.owner.ref.cell.q.Settle(ctx, storagebudget.SettlementFact{Token: op.record.Token, Owner: op.record.QBinding, Evidence: evidence, Changes: changes})
	err = op.leaveOriginalTransferCall(e)
	if err != nil {
		return errors.Join(errStorageEnrollmentUnresolved, err)
	}
	ticket, err := op.originalTransferTicket(ctx, settled, storagebudget.Terminal, 2)
	if err != nil {
		return err
	}
	if ticket.Evidence != evidence || !reflect.DeepEqual(ticket.Changes, changes) || settled.Generation != op.record.Token.Generation+1 {
		return errStorageEnrollmentUnresolved
	}
	final := op.record
	final.Generation++
	final.Phase = storageTransferSettled
	final.Token = settled
	pair, err = storageTransferDecodePair(op.fullBanks)
	if err != nil {
		return err
	}
	if err = op.publishOriginalTransferBank(ctx, final, 1-pair.Selected, 9, true, completion); err != nil {
		return err
	}
	if err = op.publishOriginalTransferHost(ctx, storageIntentSettled, storageIntentNoRejection, 22, completion); err != nil {
		return err
	}
	if err = op.takeOriginalTransferControl(ctx, []uint16{16}, completion); err != nil {
		return err
	}
	if err = op.enterOriginalTransferCall(ctx, completion); err != nil {
		return err
	}
	e = op.preparation.registry.owner.ref.cell.q.Checkpoint(ctx)
	err = op.leaveOriginalTransferCall(e)
	if err != nil {
		return err
	}
	if err = op.enterOriginalTransferCall(ctx, completion); err != nil {
		return err
	}
	state, e := op.preparation.registry.owner.ref.cell.q.InspectHostState(ctx)
	if e == nil {
		e = op.preparation.registry.owner.ref.cell.q.ValidateAppliedRequest(ctx, op.record.Epoch, state.Slots[settled.Slot], op.record.InitialToken, op.request)
	}
	err = op.leaveOriginalTransferCall(e)
	if err != nil {
		return err
	}
	retired := op.record
	retired.Generation++
	retired.Phase = storageTransferRetired
	if !storageTransferApplied(retired, op.catalog, state) {
		return errStorageEnrollmentUnresolved
	}
	op.appliedProof = &state
	pair, err = storageTransferDecodePair(op.fullBanks)
	if err != nil {
		return err
	}
	if err = op.publishOriginalTransferBank(ctx, retired, 1-pair.Selected, 10, true, completion); err != nil {
		return err
	}
	if err = op.publishOriginalTransferHost(ctx, storageIntentRetired, storageIntentNoRejection, 23, completion); err != nil {
		return err
	}
	// Non-export Tail discharge/selection still requires the exact successor P
	// installation; this assignment is deferred to that installer.
	if op.record.Mode == storageTransferExport {
		op.preparation.completed = true
		return nil
	}
	if op.record.PointerState == 2 {
		return op.completeOriginalTransferSelection(ctx)
	}
	dependency, e := storageTransferNamespaceDependency(op.host.record, op.catalog)
	if e != nil {
		return e
	}
	op.namespaceDependency = dependency
	if dependency != nil {
		return op.completeOriginalTransferNamespaceRetirement(ctx)
	}
	return op.completeOriginalTransferOwningAbort(ctx)
}

// These nil-by-default test cuts observe only the two synchronous export
// boundaries. They grant no operation/file/response authority and never replace
// qualification, the real serving entry, or the real held-description Close.
const (
	storageTransferExportReadyCut byte = iota + 1
	storageTransferExportArchiveClosedCut
)

var testOriginalChatTransferExportCut func(byte) error

// Nil-by-default native regression observations carry no operation, descriptor,
// qualification or authority. Directory receipt/leave has completed before the
// first cut; the refresh caller always closes its held pass after the second.
const (
	storageTransferDirectoryReturnedCut byte = iota + 1
	storageTransferCapacityObservedCut
)

var testOriginalChatTransferNativeCut func(byte) error

func (op *chatTransferOperation) closeOriginalTransferStage() error {
	if op.stage == nil {
		return nil
	}
	stage := op.stage
	op.stage = nil
	err := stage.file.Close()
	if op.record.Mode == storageTransferExport && testOriginalChatTransferExportCut != nil {
		err = errors.Join(err, testOriginalChatTransferExportCut(storageTransferExportArchiveClosedCut))
	}
	return err
}

// Canonical export alone seals this response binding; completion has no HTTP
// authority. Every nested response/archive call is joined before serving ends.
type chatTransferExportResponse struct {
	self              *chatTransferExportResponse
	operation         *chatTransferOperation
	request           *http.Request
	writer            http.ResponseWriter
	started, returned bool
	err               error
}
type storageTransferArchiveWriter struct {
	ctx          context.Context
	file         *os.File
	digest       hash.Hash
	bytes, limit uint64
}

func (w *storageTransferArchiveWriter) Write(data []byte) (int, error) {
	if w == nil || w.file == nil || w.digest == nil || w.bytes > w.limit || uint64(len(data)) > w.limit-w.bytes {
		return 0, storagebudget.ErrBounds
	}
	written := 0
	for written < len(data) {
		last := min(written+65536, len(data))
		if err := storageEnrollmentNativeWrite(w.ctx, w.file, data[written:last], int64(w.bytes)); err != nil {
			return written, err
		}
		w.digest.Write(data[written:last])
		w.bytes += uint64(last - written)
		written = last
	}
	return written, nil
}
func (s *Server) authorizeOriginalTransferExport(r *http.Request, name string, data []byte) error {
	if assistantSourceFileName.MatchString(name) {
		return s.authorizeAssistantSourceStorage(r)
	}
	if assistantRunFileName.MatchString(name) || assistantDialectReadRecoveryFileName.MatchString(name) {
		return s.authorizeAssistantRunStorage(r)
	}
	if privateWorkspaceStorageFile(name) {
		return s.authorizePrivateWorkspace(r)
	}
	if conversationFileName.MatchString(name) {
		return s.authorizePolicyConversations(r, data)
	}
	if draftPackFileName.MatchString(name) {
		return s.authorizePolicyWorkspace(r, data, true)
	}
	return nil
}
func (op *chatTransferOperation) captureOriginalTransferSource(ctx context.Context) error {
	if op == nil || op.self != op || op.source != nil || op.preparation.admitted {
		return storagebudget.ErrAuthority
	}
	settings, selection, err := op.preparation.epoch.c.originalHistorySettings(ctx, op.preparation.server)
	if err != nil {
		return err
	}
	var original *storageRootBinding
	for _, root := range op.preparation.epoch.originalRoots {
		if storageTransferNamespaceBinding(root) || root.source == nil || root.source.path != selection.location.Path {
			continue
		}
		if original != nil {
			return storagebudget.ErrAuthority
		}
		original = root
	}
	if original == nil {
		return storagebudget.ErrAuthority
	}
	if err = original.currentForOriginalTransfer(ctx, op); err != nil {
		return err
	}
	raw, err := readPrivateData(settings.root, dataLocationName, storageTransferPointerBytes)
	if errors.Is(err, os.ErrNotExist) && !selection.explicit {
		raw = nil
		err = nil
	}
	if err != nil {
		return err
	}
	op.source = &chatDataStore{root: original.root, location: selection.location, revision: digestOf(raw), legacy: original.configControl}
	op.settings = settings.root
	op.record.Settings = settings.source.persistent
	op.record.OldPointer = append([]byte{}, raw...)
	op.record.OldAbsent = !selection.explicit
	op.record.Mode = op.preparation.mode
	op.record.Source = original.source.persistent
	return nil
}
func (op *chatTransferOperation) writeOriginalTransferArchive(ctx context.Context) (err error) {
	if op.source == nil || op.record.Mode != storageTransferExport || op.response == nil || op.response.self != op.response || op.response.operation != op {
		return storagebudget.ErrAuthority
	}
	ordinal, err := op.enterOriginalTransferOrdinal(ctx, storageTransferStageOrdinal, 0)
	if err != nil {
		return err
	}
	defer func() { err = op.leaveOriginalTransferOrdinal(ordinal, err) }()
	op.returned.ArchiveState = 1
	parent := op.preparation.epoch.c.original.config
	name, err := storageTransferStageName(op.record)
	if err != nil {
		return err
	}
	file, err := parent.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			closed = true
			err = errors.Join(err, file.Close())
		}
	}()
	info, err := file.Stat()
	if err != nil || !storagePrivate(info, false) {
		return errors.Join(errStorageCustody, err)
	}
	named := storageNamedFile{root: parent, name: name, file: file, identity: info, size: 0}
	native, err := storageOwnerPersistentFile(ctx, named)
	if err != nil {
		return err
	}
	writer := storageTransferArchiveWriter{ctx: ctx, file: file, digest: sha256.New(), limit: op.record.ArchiveBytes}
	err = writeChatBackup(ctx, op.source, &writer, op.record.CreatedAt, func(name string, data []byte) error {
		return op.preparation.server.authorizeOriginalTransferExport(op.preparation.request, name, data)
	})
	if err != nil {
		return err
	}
	var digest storagebudget.Digest
	copy(digest[:], writer.digest.Sum(nil))
	if writer.bytes != op.record.ArchiveBytes || digest != op.record.ArchiveDigest {
		return errStorageCustody
	}
	if err = storageEnrollmentNativeSync(ctx, file); err != nil {
		return err
	}
	closed = true
	if err = file.Close(); err != nil {
		return err
	}
	readback, err := storageOpenFile(parent, name, int64(op.record.ArchiveBytes))
	if err != nil {
		return err
	}
	actual, e := storageOwnerPersistentFile(ctx, readback)
	if e == nil && actual != native {
		e = errStorageCustody
	}
	if e == nil {
		e = storageTransferVerifyFile(ctx, readback, op.record.ArchiveBytes, op.record.ArchiveDigest)
	}
	e = errors.Join(e, readback.file.Close())
	if e != nil {
		return e
	}
	if err = storageTransferSyncDirectory(ctx, parent); err != nil {
		return err
	}
	stage, err := storageOpenFile(parent, name, int64(op.record.ArchiveBytes))
	if err != nil {
		return err
	}
	actual, e = storageOwnerPersistentFile(ctx, stage)
	if e != nil || actual != native {
		return errors.Join(errStorageCustody, e, stage.file.Close())
	}
	op.stage = &stage
	op.returned.ArchiveNative = native
	op.returned.ArchiveState = 2
	return nil
}

type chatTransferExportWriter struct{ binding *chatTransferExportResponse }

func (w *chatTransferExportWriter) Header() http.Header {
	if err := w.binding.operation.preparation.ctx.Err(); err != nil {
		w.binding.err = errors.Join(w.binding.err, err)
		return make(http.Header)
	}
	return w.binding.writer.Header()
}
func (w *chatTransferExportWriter) WriteHeader(status int) {
	if err := w.binding.operation.preparation.ctx.Err(); err != nil {
		w.binding.err = errors.Join(w.binding.err, err)
		return
	}
	w.binding.started = true
	w.binding.writer.WriteHeader(status)
}
func (w *chatTransferExportWriter) Write(data []byte) (int, error) {
	if err := w.binding.operation.preparation.ctx.Err(); err != nil {
		w.binding.err = errors.Join(w.binding.err, err)
		return 0, err
	}
	w.binding.started = true
	n, err := w.binding.writer.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	w.binding.err = errors.Join(w.binding.err, err)
	return n, err
}

type chatTransferExportArchive struct {
	operation *chatTransferOperation
	file      *os.File
	err       error
}

func (a *chatTransferExportArchive) Read(data []byte) (int, error) {
	if err := a.operation.preparation.ctx.Err(); err != nil {
		a.err = errors.Join(a.err, err)
		return 0, err
	}
	n, err := a.file.Read(data)
	if err != nil && !errors.Is(err, io.EOF) {
		a.err = errors.Join(a.err, err)
	}
	return n, err
}
func (a *chatTransferExportArchive) Seek(offset int64, whence int) (int64, error) {
	if err := a.operation.preparation.ctx.Err(); err != nil {
		a.err = errors.Join(a.err, err)
		return 0, err
	}
	at, err := a.file.Seek(offset, whence)
	a.err = errors.Join(a.err, err)
	return at, err
}
func serveOriginalChatTransferExport(op *chatTransferOperation) (err error) {
	if op == nil || op.self != op || op.record.Mode != storageTransferExport || op.stage == nil || op.stage.file == nil || op.returned.ArchiveState != 2 {
		return storagebudget.ErrAuthority
	}
	binding := op.response
	if binding == nil || binding.self != binding || binding.operation != op || binding.request != op.preparation.request || binding.writer == nil || binding.returned || binding.started {
		return storagebudget.ErrAuthority
	}
	ordinal, err := op.enterOriginalTransferOrdinal(op.preparation.ctx, storageTransferServingOrdinal, 0)
	if err != nil {
		return err
	}
	defer func() {
		binding.returned = true
		err = errors.Join(err, binding.err, op.leaveOriginalTransferOrdinal(ordinal, nil))
	}()
	if err = op.stage.current(); err != nil {
		return err
	}
	writer := &chatTransferExportWriter{binding: binding}
	archive := &chatTransferExportArchive{operation: op, file: op.stage.file}
	writer.Header().Set("Content-Type", "application/zip")
	writer.Header().Set("Content-Disposition", `attachment; filename="jpack-chat-backup.zip"`)
	http.ServeContent(writer, binding.request, "jpack-chat-backup.zip", time.Time{}, archive)
	return errors.Join(binding.err, archive.err)
}
func (s *Server) withOriginalChatExport(w http.ResponseWriter, r *http.Request) (started bool, err error) {
	return s.withOriginalChatExportCompared(w, r, nil)
}

func (s *Server) withOriginalChatExportCompared(w http.ResponseWriter, r *http.Request, retentionRevision *string) (started bool, err error) {
	p, err := s.prepareOriginalChatTransfer(r, storageTransferExport)
	if err != nil {
		return false, err
	}
	var binding *chatTransferExportResponse
	err = s.withOriginalChatTransfer(p, func(op *chatTransferOperation) error {
		binding = &chatTransferExportResponse{operation: op, request: r, writer: w}
		binding.self = binding
		op.response = binding
		if e := op.captureOriginalTransferSource(p.ctx); e != nil {
			return e
		}
		if retentionRevision != nil {
			if e := s.checkOriginalRetentionExport(r, op.source, *retentionRevision, op.preparation.epoch.c.owner); e != nil {
				return e
			}
		}
		op.record.CreatedAt = time.Now().UTC().Format(time.RFC3339)
		digest := sha256.New()
		count := &backupArchiveWriter{ctx: p.ctx, out: digest, limit: maxBackupArchive}
		if e := writeChatBackup(p.ctx, op.source, count, op.record.CreatedAt, func(name string, data []byte) error { return s.authorizeOriginalTransferExport(r, name, data) }); e != nil {
			return e
		}
		op.record.ArchiveBytes = uint64(count.bytes)
		copy(op.record.ArchiveDigest[:], digest.Sum(nil))
		if e := op.reserveWhole(p.ctx); e != nil {
			return e
		}
		if e := op.writeOriginalTransferArchive(p.ctx); e != nil {
			return e
		}
		if testOriginalChatTransferExportCut != nil {
			if e := testOriginalChatTransferExportCut(storageTransferExportReadyCut); e != nil {
				return e
			}
		}
		return serveOriginalChatTransferExport(op)
	})
	if binding != nil {
		started = binding.started
		binding.writer = nil
		binding.request = nil
	}
	return started, err
}

func storageTransferReturnedBoardKnown(board []storageTransferOrdinal, results []byte) bool {
	if len(board) == 0 || len(board) != len(results) {
		return false
	}
	for _, result := range results {
		if result != 2 && result != 4 {
			return false
		}
	}
	return true
}

func storageTransferSourceChainParts(d *storageEnrollmentDefaultSource) ([]string, error) {
	if d != nil && d.Workflow == 4 && d.OwnSelf == 0 && string(d.SelectedPath) == "/" && storageOwnerCatalogNameComponent(d.Next) && len(d.Next) <= 255 {
		return []string{}, nil
	}
	if d == nil {
		return nil, storagebudget.ErrAuthority
	}
	return storageTransferPathParts(string(d.SelectedPath))
}

func storageTransferNamespaceTerminal(c storageOwnerCatalogV2, id storagebudget.ID) (storageOwnerRoot, error) {
	links, err := storageTransferNamespaceRelations(c)
	if err != nil {
		return storageOwnerRoot{}, err
	}
	children := map[storagebudget.ID]storagebudget.ID{}
	for successor, prior := range links {
		if children[prior] != (storagebudget.ID{}) {
			return storageOwnerRoot{}, storagebudget.ErrAuthority
		}
		children[prior] = successor
	}
	for i := 0; i <= len(c.Roots); i++ {
		next := children[id]
		if next == (storagebudget.ID{}) {
			row, ok := storageTransferCatalogRoot(c, id)
			if !ok {
				return row, storagebudget.ErrAuthority
			}
			return row, nil
		}
		id = next
	}
	return storageOwnerRoot{}, storagebudget.ErrAuthority
}
func storageTransferInactiveNative(ctx context.Context, r *storageRootBinding, c storageOwnerCatalogV2) error {
	row, ok := storageTransferCatalogRoot(c, r.row.ID)
	if !ok || !storageTransferCatalogView(row) || row.Source.ControlSelection != 4 || row.Definition != storageRegisteredRoot(r.row) || r.source == nil || r.source.scope == nil || !bytes.Equal(row.Source.Relative, func() []byte { raw, _ := storageEnrollmentPeakEncodeSource(*r.source.scope); return raw }()) {
		return storagebudget.ErrAuthority
	}
	terminal, err := storageTransferNamespaceTerminal(c, row.Definition.ID)
	if err != nil {
		return err
	}
	if terminal.Source.ControlSelection == 4 {
		if _, err = r.root.Lstat(r.restrictedNamespace); !errors.Is(err, os.ErrNotExist) {
			return errors.Join(errStorageCustody, err)
		}
	} else if terminal.Source.ControlSelection != 0 || !storageTransferCatalogView(terminal) {
		return storagebudget.ErrAuthority
	}
	return ctx.Err()
}
func (r *storageRootBinding) transferInactiveCurrent(ctx context.Context) error {
	if r == nil || !r.transferInactive || !storageTransferNamespaceBinding(r) {
		return storagebudget.ErrAuthority
	}
	c := r.owner.ref.cell.startup
	if c != nil {
		if op := c.originalTransferOperation(); op != nil {
			p := op.preparation.epoch
			if p.initialPacket != nil {
				row, ok := storageTransferCatalogRoot(p.initialPacket.Inner.Catalog, r.row.ID)
				if ok && row.Source.ControlSelection == 4 {
					return storageTransferInactiveNative(ctx, r, p.initialPacket.Inner.Catalog)
				}
			}
		}
	}
	if c != nil {
		o := r.owner
		o.mu.Lock()
		g := &o.admission
		g.mu.Lock()
		p := g.enrollment
		valid := p != nil && p.self == p && p.c == c && p.startup && p.sourceHoldsComplete && p.scope != nil && p.initialPacket != nil
		g.mu.Unlock()
		o.mu.Unlock()
		if valid && p.initialPacket.Scope == *p.scope && storageTransferNamespaceProof(*p.initialPacket) {
			return storageTransferInactiveNative(ctx, r, p.initialPacket.Inner.Catalog)
		}
	}
	cell := r.owner.ref.cell
	cell.mu.Lock()
	endpoint := cell.enrollmentEndpoint
	catalog := cell.ownerCatalog
	cell.mu.Unlock()
	if endpoint == nil || !storageTransferNamespaceProof(*endpoint) || !reflect.DeepEqual(endpoint.Inner.Catalog, catalog) {
		return storagebudget.ErrAuthority
	}
	return storageTransferInactiveNative(ctx, r, catalog)
}
func (w *storageCensusWalk) originalTransferNamespaceRetired(r *storageRootBinding) (bool, error) {
	if !storageTransferNamespaceBinding(r) {
		return false, nil
	}
	catalog := storageOwnerCatalogV2{}
	if w.enrollment != nil && w.enrollment.candidateObserver != nil {
		catalog = w.enrollment.candidateObserver.catalog
	} else if observer, ok := w.owner.observer.(*storageAdvisoryObserver); ok {
		catalog = observer.catalog
	} else {
		return false, storagebudget.ErrAuthority
	}
	row, ok := storageTransferCatalogRoot(catalog, r.row.ID)
	if !ok {
		return false, storagebudget.ErrAuthority
	}
	if row.Source.ControlSelection != 4 {
		if r.transferInactive {
			return false, storagebudget.ErrAuthority
		}
		return false, nil
	}
	if !r.transferInactive {
		return false, storagebudget.ErrAuthority
	}
	if err := storageTransferInactiveNative(w.ctx, r, catalog); err != nil {
		return false, err
	}
	if w.enrollment != nil && w.enrollment.transfer != nil {
		op := w.enrollment.transfer.operation
		if op != nil && op.self == op && op.retiredNamespace == r.row.ID && bytes.Equal(op.retiredNamespaceSource, row.Source.Relative) {
			return true, op.originalReadCurrent(w.ctx)
		}
	}
	return true, r.transferInactiveCurrent(w.ctx)
}

func (op *chatTransferOperation) issueOriginalTransferNamespaceRetirement(ctx context.Context, old storageOwnerRoot) (storagebudget.Digest, error) {
	if op == nil || op.self != op || op.record.Phase != storageTransferRejected && op.record.Phase != storageTransferRetired || !op.completing || op.completion == nil || op.namespaceDependency == nil || op.retiredNamespace != (storagebudget.ID{}) || !storageTransferCatalogView(old) || old.Source.ControlSelection != 0 {
		return storagebudget.Digest{}, storagebudget.ErrAuthority
	}
	proof := op.rejectedProof
	if op.record.Phase == storageTransferRejected {
		if proof == nil || !op.reserveReturned || storageDefinitelyRejected(op.reserveError) == storageIntentNoRejection || !storageTransferNoTicket(op.record, *proof) {
			return storagebudget.Digest{}, storagebudget.ErrAuthority
		}
	} else {
		proof = op.appliedProof
		if proof == nil || op.record.PointerState != 0 || !op.record.Cleanup.complete() || !storageTransferApplied(op.record, op.catalog, *proof) {
			return storagebudget.Digest{}, storagebudget.ErrAuthority
		}
	}
	p := op.preparation.epoch
	scope, err := storageEnrollmentPeakDecodeSource(old.Source.Relative)
	if err != nil || !reflect.DeepEqual(scope, *op.namespaceDependency) || scope.Default.Settings != op.record.Settings || scope.Default.SourceNative != op.record.Source {
		return storagebudget.Digest{}, errors.Join(storagebudget.ErrAuthority, err)
	}
	dependency, err := storageTransferNamespaceDependency(op.host.record, op.catalog)
	if err != nil || dependency == nil || !reflect.DeepEqual(*dependency, scope) {
		return storagebudget.Digest{}, errors.Join(storagebudget.ErrAuthority, err)
	}
	var binding *storageRootBinding
	for _, root := range p.originalRoots {
		if root.row.ID == old.Definition.ID {
			binding = root
		}
	}
	if binding == nil || binding.transferInactive || !storageTransferNamespaceBinding(binding) {
		return storagebudget.Digest{}, storagebudget.ErrAuthority
	}
	if err = op.completion.current(ctx); err != nil {
		return storagebudget.Digest{}, err
	}
	if _, err = binding.root.Lstat(binding.restrictedNamespace); !errors.Is(err, os.ErrNotExist) {
		return storagebudget.Digest{}, errors.Join(errStorageCustody, err)
	}
	for _, slot := range proof.Slots {
		if !storageUnretired(slot) {
			continue
		}
		for _, component := range slot.Ticket.Components {
			if component.Root == old.Definition.ID {
				return storagebudget.Digest{}, storagebudget.ErrUnknown
			}
		}
	}
	census, err := p.c.owner.measureCensusAllTransfer(ctx, p.originalRoots, nil, nil, nil, op)
	if err != nil {
		return storagebudget.Digest{}, err
	}
	zero := false
	for _, root := range census.Roots {
		if root.ID == old.Definition.ID {
			if root.Usage != ([storagebudget.Categories]storagebudget.Vector{}) {
				return storagebudget.Digest{}, storagebudget.ErrUnknown
			}
			zero = true
		}
	}
	if !zero {
		return storagebudget.Digest{}, storagebudget.ErrUnknown
	}
	if _, err = binding.root.Lstat(binding.restrictedNamespace); !errors.Is(err, os.ErrNotExist) {
		return storagebudget.Digest{}, errors.Join(errStorageCustody, err)
	}
	if err = op.completion.current(ctx); err != nil {
		return storagebudget.Digest{}, err
	}
	digest := sha256.Sum256(op.host.page)
	op.retiredNamespace = old.Definition.ID
	op.retiredNamespacePage = digest
	op.retiredNamespaceSource = append([]byte{}, old.Source.Relative...)
	return digest, nil
}

func (op *chatTransferOperation) originalTransferCatalogBase(ctx context.Context, bridge storagebudget.ID) (storageOwnerCatalogV2, error) {
	p := op.preparation.epoch
	var out storageOwnerCatalogV2
	if p == nil || p.previousEvidence == nil || p.initialInspection == nil || p.original == nil || p.scope != nil || bridge == (storagebudget.ID{}) {
		return out, storagebudget.ErrAuthority
	}
	if err := op.originalReadCurrent(ctx); err != nil {
		return out, err
	}
	before := p.previousEvidence.Inner.Catalog
	raw, err := json.Marshal(before)
	if err != nil {
		return out, err
	}
	if err = storageEnrollmentPeakCatalogJSON(raw, &out); err != nil {
		return out, err
	}
	if out.Sequence == ^uint64(0) || p.initialInspection.View.Configuration.Revision == ^uint64(0) {
		return out, storagebudget.ErrBounds
	}
	out.Version = 1
	out.Sequence++
	out.Phase = "prepared"
	out.Transition = bridge
	out.ExpectedQRevision = p.initialInspection.View.Configuration.Revision + 1
	bank := 0
	if storageEnrollmentPeakU64(p.original.Catalogs[1][16:24]) > storageEnrollmentPeakU64(p.original.Catalogs[0][16:24]) {
		bank = 1
	}
	copy(out.PreviousCatalogDigest[:], p.original.Catalogs[bank][32:64])
	out.BeforeDefinitionIDs = storageOwnerDefinitionIDs{Roots: []storagebudget.ID{}, Workspaces: []storagebudget.ID{}, Filesystems: []storagebudget.ID{}, Constraints: []storagebudget.ID{}}
	for _, r := range before.Roots {
		out.BeforeDefinitionIDs.Roots = append(out.BeforeDefinitionIDs.Roots, r.Definition.ID)
	}
	for _, r := range before.Workspaces {
		out.BeforeDefinitionIDs.Workspaces = append(out.BeforeDefinitionIDs.Workspaces, r.Definition.ID)
	}
	for _, r := range before.Filesystems {
		out.BeforeDefinitionIDs.Filesystems = append(out.BeforeDefinitionIDs.Filesystems, r.Definition.ID)
	}
	for _, r := range before.Constraints {
		out.BeforeDefinitionIDs.Constraints = append(out.BeforeDefinitionIDs.Constraints, r.Definition.ID)
	}
	return out, nil
}
func storageTransferSealCatalog(catalog *storageOwnerCatalogV2) error {
	if catalog == nil {
		return storagebudget.ErrAuthority
	}
	if err := storageEnrollmentSortObjects(catalog); err != nil {
		return err
	}
	sort.Slice(catalog.Roots, func(i, j int) bool {
		return bytes.Compare(catalog.Roots[i].Definition.ID[:], catalog.Roots[j].Definition.ID[:]) < 0
	})
	sort.Slice(catalog.Filesystems, func(i, j int) bool {
		return bytes.Compare(catalog.Filesystems[i].Definition.ID[:], catalog.Filesystems[j].Definition.ID[:]) < 0
	})
	before, after, err := catalog.definitions()
	if err != nil {
		return err
	}
	catalog.BeforeDefinitionsDigest, err = storageOwnerDigest("desk-storage-owner-definitions-v2", before)
	if err != nil {
		return err
	}
	catalog.AfterDefinitionsDigest, err = storageOwnerDigest("desk-storage-owner-definitions-v2", after)
	return err
}
func (op *chatTransferOperation) appendOriginalTransferRoot(ctx context.Context, catalog *storageOwnerCatalogV2, native storageOwnerPersistentObject, path, next string, own bool, marker storageOwnerPersistentObject, markerBytes uint64, bridge storagebudget.ID, prior *storageOwnerRoot) (storageEnrollmentPeakSource, error) {
	var source storageEnrollmentPeakSource
	p := op.preparation.epoch
	if catalog == nil || op.path == nil || !native.valid(1) || op.record.Source == (storageOwnerPersistentObject{}) || own && next != "" || !own && !storageOwnerCatalogNameComponent([]byte(next)) {
		return source, storagebudget.ErrAuthority
	}
	settings, _, err := p.c.originalHistorySettings(ctx, op.preparation.server)
	if err != nil {
		return source, err
	}
	var selected storageOwnerRoot
	found := false
	for _, row := range p.previousEvidence.Inner.Catalog.Roots {
		if storageTransferCatalogView(row) {
			continue
		}
		if p.previousEvidence.Inner.Catalog.Objects[row.DirectoryObject].Persistent == op.record.Source {
			if found {
				return source, storagebudget.ErrAuthority
			}
			selected = row
			found = true
		}
	}
	if !found {
		return source, storagebudget.ErrAuthority
	}
	key, err := op.preparation.accountingKey(ctx)
	if err != nil {
		return source, err
	}
	var workspace storageOwnerWorkspace
	for _, w := range catalog.Workspaces {
		if string(w.OwnerKey) == key && string(w.HistoryIdentity) == op.preparation.server.conversationIdentity() {
			if workspace.Definition.ID != (storagebudget.ID{}) {
				return source, storagebudget.ErrAuthority
			}
			workspace = w
		}
	}
	if workspace.Definition.ID == (storagebudget.ID{}) {
		return source, storagebudget.ErrAuthority
	}
	profile, err := storageHostInstalledProfile(native.Backend)
	if err != nil {
		return source, err
	}
	profileFound := false
	for _, old := range catalog.Profiles {
		profileFound = profileFound || old == profile
	}
	if !profileFound {
		return source, storagebudget.ErrUnsupported
	}
	advisory := storageOwnerAdvisoryConstraint(catalog.Installation).Definition
	var filesystem storageOwnerFilesystem
	for _, old := range catalog.Filesystems {
		if old.Backend == native.Backend && old.VolumeUUID == native.VolumeUUID {
			if filesystem.Definition.ID != (storagebudget.ID{}) {
				return source, storagebudget.ErrAuthority
			}
			filesystem = old
		}
	}
	if filesystem.Definition.ID == (storagebudget.ID{}) {
		identity, e := storageOwnerDigest("desk-storage-original-volume-v3", native.VolumeUUID)
		if e != nil {
			return source, e
		}
		definition := storagebudget.Filesystem{ID: storageEnrollmentProjectDefinitionID("filesystem", catalog.Installation, bridge, native), Identity: identity, Generation: 1, InodeModel: storagebudget.InodesFinite, CapabilityEvidence: profile.InodeEvidence, ByteConstraint: advisory.ID, InodeConstraint: advisory.ID}
		if native.Backend == 2 {
			definition.InodeModel = storagebudget.InodesDynamic
			definition.InodeConstraint = storagebudget.ID{}
		}
		filesystem = storageOwnerFilesystem{Definition: definition, Backend: native.Backend, VolumeUUID: native.VolumeUUID, PersistentProfile: profile.ID, InodeProfile: profile.ID}
		catalog.Filesystems = append(catalog.Filesystems, filesystem)
	}
	index := storageOwnerNoObject
	for i, object := range catalog.Objects {
		if object.Persistent == native {
			if index != storageOwnerNoObject {
				return source, storagebudget.ErrAuthority
			}
			index = uint32(i)
		}
	}
	if index == storageOwnerNoObject {
		index = uint32(len(catalog.Objects))
		catalog.Objects = append(catalog.Objects, storageOwnerObject{Role: 20, Source: 3, Parent: storageOwnerNoObject, Name: []byte{}, Persistent: native, OwnerClass: 2, ModeClass: 1})
	}
	role, kind, category := byte(8), byte(9), byte(storagebudget.Control)
	idKind, namespace := "transfer-scope", "transfer-base"
	workflow := byte(4)
	ownSelf := byte(0)
	domain := storageOwnerNoObject
	if own {
		role, kind, category = 4, 11, storagebudget.History
		idKind, namespace = "transfer-root", "transfer-root"
		workflow = 6
		ownSelf = 1
		for i, object := range catalog.Objects {
			if object.Role == 18 && int(object.Parent) < len(catalog.Objects) && catalog.Objects[object.Parent].Persistent == p.c.ref.cell.ownerRecord.Config && string(object.Name) == ".data.lock" {
				if domain != storageOwnerNoObject {
					return source, storagebudget.ErrAuthority
				}
				domain = uint32(i)
			}
		}
		if domain == storageOwnerNoObject {
			return source, storagebudget.ErrAuthority
		}
		if marker.valid(2) {
			catalog.Objects = append(catalog.Objects, storageOwnerObject{Role: 23, Source: 3, Parent: index, Name: []byte(dataMarkerName), Persistent: marker, OwnerClass: 2, ModeClass: 2, FixedLength: markerBytes})
		}
	}
	id := storageEnrollmentProjectDefinitionID(idKind, catalog.Installation, bridge, native)
	row := storageOwnerRoot{Definition: storagebudget.RegisteredRoot{ID: id, Owner: catalog.QBinding, Filesystem: filesystem.Definition.ID, Generation: 1, Locator: idKind + ":" + storageHexID(id), CapacityCount: 1, CapacityRefs: [4]storagebudget.CapacityRef{{Constraint: advisory.ID, Generation: advisory.Generation}}}, Role: role, Category: category, References: append([]storagebudget.ID{}, selected.References...), Source: storageOwnerRootSource{Kind: kind, OwnerKey: []byte(op.preparation.server.storageOwner()), Path: []byte(path), Relative: []byte{}, ControlIndex: storageOwnerNoControl}, DirectoryObject: index, DomainObject: domain, WorkObject: storageOwnerNoObject, Membership: storageOwnerMembership{Backend: profile.Backend, ProfileID: profile.ID, ProfileVersion: profile.Version, ProfileEvidence: profile.Evidence, Pool: filesystem.Definition.ID, VolumeUUID: native.VolumeUUID, Creation: 1, ExistingObjects: []uint32{}}}
	if !own {
		row.Source.OwnerKey = append([]byte{}, workspace.OwnerKey...)
	}
	if len(row.References) == 1 {
		row.Definition.Workspace = row.References[0]
	}
	if profile.Backend == 1 {
		row.Membership.User = storageOwnerSubject{State: 4, Kind: 1}
		row.Membership.Group = storageOwnerSubject{State: 4, Kind: 2}
		row.Membership.Project = storageOwnerSubject{State: 4, Kind: 3}
	}
	branch := byte(storageCreationConfigured)
	if op.preparation.server.containedDeskData() {
		branch = storageCreationContained
	} else if path == op.preparation.server.defaultChatDataPath() {
		branch = storageCreationPersonal
	}
	source = storageEnrollmentPeakSource{Installation: catalog.Installation, QBinding: catalog.QBinding, Project: p.original.Project, Workspace: workspace.Definition.ID, WorkspaceGeneration: workspace.Definition.Generation, OwnerKey: append([]byte{}, workspace.OwnerKey...), HistoryIdentity: append([]byte{}, workspace.HistoryIdentity...), Filesystem: filesystem.Definition.ID, FilesystemIdentity: filesystem.Definition.Identity, FilesystemGeneration: filesystem.Definition.Generation, CapacityCount: row.Definition.CapacityCount, CapacityRefs: row.Definition.CapacityRefs, Generation: 1, Namespace: []byte(namespace), Default: &storageEnrollmentDefaultSource{Workflow: workflow, Branch: branch, Settings: settings.source.persistent, Selected: native, SelectedPath: []byte(path), OwnSelf: ownSelf, Next: []byte(next), Marker: marker, SourceNative: op.record.Source}}
	if prior != nil {
		if prior.Source.ControlSelection != 4 {
			return source, storagebudget.ErrAuthority
		}
		source.Default.CreationPage = storageTransferNamespaceID(prior.Definition.ID)
		source.Default.LocationPayload = storagebudget.Digest{}
	}
	source.HomeSource, err = storageEnrollmentHomeSourceDigest(source, *catalog)
	if err != nil {
		return source, err
	}
	source.CapacityBinding, err = storageEnrollmentPeakSourceMapping(source, *catalog, row)
	if err != nil {
		return source, err
	}
	edges := append(append([]storageNamedRoot{}, op.path.edges...), op.created...)
	parts, err := storageTransferSourceChainParts(source.Default)
	if err != nil || len(edges) != len(parts) {
		return source, errors.Join(storagebudget.ErrAuthority, err)
	}
	source.Default.ChainCount = uint16(len(edges))
	natives := make([]storageOwnerPersistentObject, len(edges))
	for ordinal := 0; ordinal < (len(edges)+31)/32; ordinal++ {
		first, last, e := storageTransferParentWindow(len(edges), ordinal)
		if e != nil {
			return source, e
		}
		for i := first; i < last; i++ {
			if e = edges[i].current(); e != nil {
				return source, e
			}
			natives[i], e = storageOwnerPersistentRoot(ctx, edges[i].root)
			if e != nil {
				return source, e
			}
		}
	}
	anchor, err := storageOwnerPersistentRoot(ctx, op.path.base)
	if err != nil {
		return source, err
	}
	source.Default.ChainDigest, err = storageTransferChainDigest(source, anchor, parts, natives)
	if err != nil {
		return source, err
	}
	source.CapacityBinding, err = storageEnrollmentPeakSourceMapping(source, *catalog, row)
	if err != nil {
		return source, err
	}
	if _, err = storageTransferVerifyChainAt(ctx, source, op.path.base, edges); err != nil {
		return source, err
	}
	row.Source.Relative, err = storageEnrollmentPeakEncodeSource(source)
	if err != nil {
		return source, err
	}
	if own {
		row.Definition.Identity = storageEnrollmentPeakHash("desk-chat-transfer-captured-history-v1", row.Source.Relative)
	} else {
		row.Definition.Identity = storageEnrollmentAncestorSourceIdentity(source, row.Source.Relative)
	}
	constraints := []storagebudget.CapacityConstraint{}
	for _, constraint := range catalog.Constraints {
		constraints = append(constraints, constraint.Definition)
	}
	row.Definition.CapacityBinding, err = storagebudget.CapacityBindingFor(storageOwnerRootValue(row.Definition), filesystem.Definition, constraints)
	if err != nil {
		return source, err
	}
	catalog.Roots = append(catalog.Roots, row)
	return source, storageTransferSealCatalog(catalog)
}

func storageTransferNamespaceCatalogRetains(before, after storageOwnerCatalogV2, source storageEnrollmentPeakSource) bool {
	if source.Default == nil || source.Default.Workflow != 4 || !storageEnrollmentDefaultSourceNamespace(source) {
		return false
	}
	if _, err := storageTransferNamespaceRelations(after); err != nil {
		return false
	}
	copy := after
	copy.Roots = append([]storageOwnerRoot{}, after.Roots...)
	changed := 0
	raw, err := storageEnrollmentPeakEncodeSource(source)
	if err != nil {
		return false
	}
	for i, next := range copy.Roots {
		old, exists := storageTransferCatalogRoot(before, next.Definition.ID)
		if !exists {
			continue
		}
		if old.Source.ControlSelection == next.Source.ControlSelection {
			continue
		}
		if !storageTransferCatalogView(old) || !storageTransferCatalogView(next) || old.Source.ControlSelection != 0 || next.Source.ControlSelection != 4 || !bytes.Equal(old.Source.Relative, raw) || !bytes.Equal(old.Source.Relative, next.Source.Relative) {
			return false
		}
		copy.Roots[i].Source.ControlSelection = 0
		changed++
	}
	if changed > 1 {
		return false
	}
	return storageEnrollmentRetainsOriginalWithDefault(before, copy, nil)
}
func storageTransferRetentionRegistration(board storageEnrollmentPeakBoard, catalog storageOwnerCatalogV2) bool {
	if board.SourceKind != 10 || board.Source.Default == nil || board.Source.Default.Workflow != 4 || !storageEnrollmentDefaultSourceNamespace(board.Source) {
		return false
	}
	for _, list := range board.Registration {
		if len(list) != 0 {
			return false
		}
	}
	raw, err := storageEnrollmentPeakEncodeSource(board.Source)
	if err != nil {
		return false
	}
	count := 0
	for _, row := range catalog.Roots {
		if storageTransferCatalogView(row) && row.Source.ControlSelection == 4 && bytes.Equal(row.Source.Relative, raw) {
			count++
		}
	}
	return count == 1
}
func storageTransferRetentionProof(e storageEnrollmentPeakEvidence) bool {
	if !storageTransferRetentionRegistration(e.Board, e.Inner.Catalog) || !storageTransferNamespaceProof(e) {
		return false
	}
	before, ok := storageTransferNamespaceBefore(e)
	if !ok {
		return false
	}
	if len(before.Roots) != len(e.Inner.Catalog.Roots) || len(before.Objects) != len(e.Inner.Catalog.Objects) || len(before.OwnerControls) != len(e.Inner.Catalog.OwnerControls) || len(before.Workspaces) != len(e.Inner.Catalog.Workspaces) || len(before.Filesystems) != len(e.Inner.Catalog.Filesystems) || len(before.Constraints) != len(e.Inner.Catalog.Constraints) || before.AfterDefinitionsDigest != e.Inner.Catalog.AfterDefinitionsDigest || !storageTransferNamespaceCatalogRetains(*before, e.Inner.Catalog, e.Board.Source) {
		return false
	}
	changed := 0
	for _, old := range before.Roots {
		next, exists := storageTransferCatalogRoot(e.Inner.Catalog, old.Definition.ID)
		if !exists {
			return false
		}
		if old.Source.ControlSelection != next.Source.ControlSelection {
			changed++
		}
	}
	return changed == 1
}

func (op *chatTransferOperation) consumeOriginalTransferTail(ctx context.Context, scope storageEnrollmentPeakScope) error {
	if op == nil || op.self != op || !op.tailAdmitted || op.tailConsumed || op.record.Mode == storageTransferExport || op.record.Phase != storageTransferRejected && op.record.Phase != storageTransferRetired || scope.requireCapacityEstimate() != nil {
		return storagebudget.ErrAuthority
	}
	if err := op.originalReadCurrent(ctx); err != nil {
		return err
	}
	tail := op.record.Tail
	if tail.validate() != nil || op.preparation.epoch.original != op.controlOriginal || op.controlOriginal.Project != tail.Project || storagebudget.ID([16]byte(scope[24:40])) != tail.Bridge || scope.target(9) != tail.Config || scope.target(10) != tail.Anchor || scope.final(0) > tail.MaximumExtent || scope.final(1) > tail.MaximumExtent {
		return storagebudget.ErrAuthority
	}
	for i, native := range tail.Targets {
		descriptor := scope[1360+i*32 : 1360+(i+1)*32]
		if scope.target(byte(i+1)) != native || op.controlOriginal.Targets[i] != native || storageEnrollmentPeakU64(descriptor[4:12]) != tail.Before[i] || op.controlOriginal.Extents[i] != tail.Before[i] {
			return storagebudget.ErrAuthority
		}
	}
	for i, raw := range op.controlOriginal.Catalogs {
		if sha256.Sum256(raw) != tail.Catalogs[i] {
			return storagebudget.ErrAuthority
		}
	}
	var peaks [2]storagebudget.CapacityVector
	for _, group := range storageEnrollmentPeakPlan(scope) {
		value, ok := storageEnrollmentPeakEstimate(group)
		if !ok {
			return storagebudget.ErrBounds
		}
		next, err := storageIdentityAddCapacity(peaks[group.Parent-1], value)
		if err != nil {
			return err
		}
		peaks[group.Parent-1] = next
	}
	for i, peak := range peaks {
		if peak.Bytes > tail.ParentPeaks[i].Bytes || peak.Inodes > tail.ParentPeaks[i].Inodes {
			return storagebudget.ErrBounds
		}
	}
	logical := storagebudget.Vector{Bytes: storageEnrollmentPeakU64(scope[360:368]), Files: storageEnrollmentPeakU64(scope[368:376]), Directories: storageEnrollmentPeakU64(scope[376:384])}
	if !storageVectorLE(logical, tail.Logical) {
		return storagebudget.ErrBounds
	}
	if op.record.Phase == storageTransferRejected {
		if !op.reserveReturned || storageDefinitelyRejected(op.reserveError) == storageIntentNoRejection || op.rejectedProof == nil || !storageTransferNoTicket(op.record, *op.rejectedProof) {
			return storagebudget.ErrAuthority
		}
	} else if op.appliedProof == nil || !storageTransferApplied(op.record, op.catalog, *op.appliedProof) {
		return storagebudget.ErrAuthority
	}
	op.tailConsumed = true
	return nil
}

func storageTransferCatalogRoot(c storageOwnerCatalogV2, id storagebudget.ID) (storageOwnerRoot, bool) {
	var out storageOwnerRoot
	found := false
	for _, row := range c.Roots {
		if row.Definition.ID == id {
			if found {
				return storageOwnerRoot{}, false
			}
			out = row
			found = true
		}
	}
	return out, found
}

// Install only the image reconciled by this original invocation's exact P/Q
// completion. Every replacement is an alias of a lock owner captured in the
// one initial sorted EX union. No lock is released or reacquired at this swap.
func (p *storageEnrollmentEpoch) installOriginalTransferBindings(ctx context.Context) error {
	if p == nil || p.self != p || p.transfer == nil || p.transfer.operation == nil || p.candidate == nil || p.candidate.installed || p.candidateObserver == nil || p.completedEvidence == nil || p.factualCensus == nil || p.planCursor != len(p.enrollmentPassPlan()) {
		return storagebudget.ErrAuthority
	}
	op := p.transfer.operation
	image := p.candidate
	after := p.completedEvidence.Inner.Catalog
	if op.self != op || op.preparation != p.transfer || !reflect.DeepEqual(after, p.candidateObserver.catalog) {
		return storagebudget.ErrAuthority
	}
	if err := op.originalReadCurrent(ctx); err != nil {
		return err
	}
	before := p.previousEvidence
	if before == nil {
		return storagebudget.ErrAuthority
	}
	source := p.completedEvidence.Board.Source
	if !storageEnrollmentRetainsOriginalWithDefault(before.Inner.Catalog, after, &source) {
		return errStorageCustody
	}
	if source.Default != nil && source.Default.Workflow == 5 {
		if op.record.Phase != storageTransferRetired || op.record.PointerState != 2 || op.appliedProof == nil || !storageTransferApplied(op.record, op.catalog, *op.appliedProof) || storageTransferSelectionRelation(op.record, op.catalog, after) != nil || !storageTransferTailBound(op.host.record, *p.completedEvidence) {
			return storagebudget.ErrAuthority
		}
	}
	roots := make([]*storageRootBinding, 0, len(image.roots))
	for _, root := range image.roots {
		roots = append(roots, root)
	}
	sort.Slice(roots, func(i, j int) bool { return bytes.Compare(roots[i].row.ID[:], roots[j].row.ID[:]) < 0 })
	holds := make([]*storageDomainHold, len(roots))
	domains := map[storageDirectoryIdentity]*storageDomainHold{}
	for id, owner := range p.use.domains {
		domains[id] = owner
	}
	for i, root := range roots {
		row, ok := storageTransferCatalogRoot(after, root.row.ID)
		if !ok || storageRegisteredRoot(root.row) != row.Definition || !reflect.DeepEqual(root.references, row.References) || root.transferInactive != (storageTransferCatalogView(row) && row.Source.ControlSelection == 4) || root.source == nil {
			return errStorageCustody
		}
		if err := root.source.currentForOriginalTransfer(ctx, op); err != nil {
			return err
		}
		directory, err := root.source.directoryIdentity()
		if err != nil || directory != root.directory || root.root != root.source.root {
			return errors.Join(errStorageCustody, err)
		}
		alias := &storageDomainHold{owner: p.c.owner, root: root, lifetime: p.use}
		alias.self = alias
		if !root.qControl {
			if root.domain.file == nil {
				return errStorageCustody
			}
			id, err := storageFileIdentity(root.domain.identity)
			if err != nil {
				return err
			}
			owner := domains[id]
			if owner == nil {
				// The only additional domain was already acquired before any enrollment
				// effect; its actual FD remains owned by the original epoch finalizer.
				if op.path == nil || op.path.domainOwner == nil || op.path.domainOwner != p.transferExtraDomain {
					return errStorageCustody
				}
				held, e := p.transferExtraDomain.Stat()
				if e != nil || !os.SameFile(held, root.domain.identity) {
					return errors.Join(errStorageCustody, e)
				}
				owner = &storageDomainHold{owner: p.c.owner, root: root, lifetime: p.use, file: p.transferExtraDomain}
				owner.self = owner
				domains[id] = owner
			}
			if owner.self != owner || owner.closed || owner.owner != p.c.owner || owner.lifetime != p.use || owner.file == nil || owner.borrowed != nil {
				return storagebudget.ErrAuthority
			}
			held, e := owner.file.Stat()
			if e != nil || !os.SameFile(held, root.domain.identity) {
				return errors.Join(errStorageCustody, e)
			}
			alias.borrowed = owner
		}
		holds[i] = alias
	}
	// Dictionary material is prepared before taking state locks. A successful
	// selection changes only the one original setting; unrelated selections and
	// the original source identities remain byte-for-byte retained.
	x := p.c.sources
	selections := append([]storageOriginalHistorySelection{}, x.selections...)
	if source.Default != nil && source.Default.Workflow == 5 {
		found := -1
		for i, selection := range selections {
			if selection.settings == op.settings {
				if found >= 0 {
					return storagebudget.ErrAuthority
				}
				found = i
			}
		}
		if found < 0 {
			return storagebudget.ErrAuthority
		}
		next, err := storageTransferResolvedLocation(op.record.NewPointer, selections[found].path)
		if err != nil {
			return err
		}
		raw, err := readPrivateData(op.settings, dataLocationName, storageTransferPointerBytes)
		if err != nil || !bytes.Equal(raw, op.record.NewPointer) {
			return errors.Join(errStorageCustody, err)
		}
		selections[found].location = next
		selections[found].explicit = true
		selections[found].pending = nil
		selections[found].pointer = nil
		selections[found].pointerNative = op.record.PointerNative
	}
	config := storageOwnerNoObject
	for i, object := range after.Objects {
		if object.Role == 1 && object.Persistent == p.c.ref.cell.ownerRecord.Config {
			if config != storageOwnerNoObject {
				return errStorageCustody
			}
			config = uint32(i)
		}
	}
	if config == storageOwnerNoObject {
		return errStorageCustody
	}
	if err := op.originalReadCurrent(ctx); err != nil {
		return err
	}
	o := p.c.owner
	o.mu.Lock()
	g := &o.admission
	g.mu.Lock()
	p.c.mu.Lock()
	p.mu.Lock()
	valid := !o.closed && !p.c.closed && p.running && !p.finishing && g.enrollment == p && g.generation == p.generation && (g.phase == storageAdmissionEnrollment || g.phase == storageAdmissionClosing) && o.active == 1 && g.shared == 0 && g.prefixes == 0 && g.preparation == nil && g.exclusiveCensus == nil
	for id, old := range o.roots {
		next := image.roots[id]
		if next != old && (next == nil || storageRegisteredRoot(next.row) != storageRegisteredRoot(old.row) || next.source != old.source && image.sourceAliases[old.source] != next.source || next.root != old.root || next.directory != old.directory || !reflect.DeepEqual(next.domain, old.domain) || next.category != old.category) {
			valid = false
		}
	}
	for id, old := range o.filesystems {
		if image.filesystems[id] != old {
			valid = false
		}
	}
	for id, old := range o.constraints {
		if image.constraints[id] != old {
			valid = false
		}
	}
	for id, old := range o.workspaces {
		next, ok := image.workspaces[id]
		if !ok || next.ID != old.ID || next.Generation != old.Generation || next.Limits != old.Limits {
			valid = false
		}
	}
	if !valid {
		p.mu.Unlock()
		p.c.mu.Unlock()
		g.mu.Unlock()
		o.mu.Unlock()
		return errStorageCustody
	}
	for id, old := range o.workspaces {
		image.workspaces[id] = old
	}
	oldHolds := p.holds
	o.roots, o.filesystems, o.constraints, o.workspaces = image.roots, image.filesystems, image.constraints, image.workspaces
	o.openedFiles = append(o.openedFiles, image.openedFiles...)
	image.openedFiles = nil
	image.installed = true
	p.originalRoots, p.holds, p.use.domains = roots, holds, domains
	for i, source := range x.roots {
		if alias := image.sourceAliases[source]; alias != nil {
			x.roots[i] = alias
		}
	}
	x.owner, x.objects, x.controls, x.configObject, x.selections = after.OriginalOwner, after.Objects, after.OwnerControls, config, selections
	p.mu.Unlock()
	p.c.mu.Unlock()
	g.mu.Unlock()
	o.mu.Unlock()
	// Only descriptor-free aliases are retired. Real lock-owning descriptions
	// remain in transferDomainOwners/private/project/transferExtraDomain.
	for _, hold := range oldHolds {
		if hold != nil && hold.file == nil {
			if err := hold.Close(); err != nil {
				return err
			}
		}
	}
	return nil
}

// The dependency is a property of the rejected operation's exact original
// catalog, never of a later active/retired label or the next operation's Tail.
// Failure to reconstruct that catalog is unresolved, not proof of no dependency.
func storageTransferNamespaceDependency(core storageIntentRecord, original storageOwnerCatalogV2) (*storageEnrollmentPeakSource, error) {
	if core.Transfer == nil || (core.Phase != storageIntentRejected || core.Outcome != storageIntentNotAdmitted) && (core.Phase != storageIntentRetired || core.Outcome != storageIntentAborted || !core.Transfer.Cleanup.complete() || core.Transfer.PointerState != 0) || core.OperationID == (storagebudget.ID{}) || original.Installation == (storagebudget.ID{}) || original.QBinding == (storagebudget.Digest{}) {
		return nil, storagebudget.ErrAuthority
	}
	digest, err := storageTransferCatalogDigest(original)
	if err != nil || digest != core.Transfer.Catalog {
		return nil, errors.Join(storagebudget.ErrAuthority, err)
	}
	var dependency *storageEnrollmentPeakSource
	var dependencyID storagebudget.ID
	for i, component := range core.Components {
		row, ok := storageTransferCatalogRoot(original, component.Root)
		if !ok || i >= len(core.RootIdentities) || row.Definition.Identity != core.RootIdentities[i] || row.Definition.Owner != component.Owner || row.Definition.Generation != component.RootGeneration || row.Definition.Filesystem != component.Filesystem {
			return nil, storagebudget.ErrAuthority
		}
		if !storageTransferCatalogView(row) {
			continue
		}
		source, e := storageEnrollmentPeakDecodeSource(row.Source.Relative)
		if e != nil || row.Source.ControlSelection != 0 || source.Default == nil || core.Transfer.Mode == storageTransferExport || source.Default.Settings != core.Transfer.Settings || source.Default.SourceNative != core.Transfer.Source || !core.Transfer.Empty || core.Transfer.Existing != (storagebudget.Vector{}) {
			return nil, storagebudget.ErrAuthority
		}
		if dependency != nil && dependencyID != row.Definition.ID {
			return nil, storagebudget.ErrAuthority
		}
		dependency = &source
		dependencyID = row.Definition.ID
	}
	if len(core.Components) == 0 {
		return nil, storagebudget.ErrAuthority
	}
	return dependency, nil
}
func storageTransferRetainedRejected(core storageIntentRecord, page []byte, original storageOwnerCatalogV2, e *storageEnrollmentPeakEvidence) bool {
	if e == nil || len(page) != storageIntentPage {
		return false
	}
	digest := sha256.Sum256(page)
	for _, w := range e.Witnesses {
		if !w.Selected || w.Digest != digest || !bytes.Equal(w.Page, page) || !reflect.DeepEqual(w.Core, core) || int(w.Origin) >= len(e.Board.Origins) {
			continue
		}
		origin := e.Board.Origins[w.Origin]
		if origin.Epoch != core.AdmissionEpoch || int(origin.View) >= len(e.Views) || int(origin.Projection) >= len(e.Projections) || !storageTransferCompactNoTicket(core, storageEnrollmentPeakHostState(e.Views[origin.View]), true) {
			return false
		}
		before, err := storageTransferCatalogDigest(e.Projections[origin.Projection].Catalog)
		want, e0 := storageTransferCatalogDigest(original)
		if err != nil || e0 != nil || before != want || before != core.Transfer.Catalog {
			return false
		}
		for _, coverage := range e.Board.Coverage {
			if coverage.Slot == w.Slot && coverage.Generation == w.Generation && coverage.Operation == w.Operation && coverage.Page == digest && coverage.Epoch == e.CoveredEpoch && coverage.Bridge == e.Bridge {
				return true
			}
		}
	}
	return false
}

// Refresh comparison bytes only after actual whole-operation retirement or
// definite rejection. Catalog/Ready/host originals cannot change in this step;
// the retained original Q query supplies exact applied/no-ticket observations.
func (op *chatTransferOperation) refreshOriginalTransferQ(ctx context.Context) error {
	p := op.preparation.epoch
	if p.original == nil || p.initialInspection == nil || p.scope != nil || p.initialPacket != nil || op.controlOriginal != p.original || op.completion == nil || !op.completing {
		return storagebudget.ErrAuthority
	}
	if err := op.completion.current(ctx); err != nil {
		return err
	}
	for target := byte(5); target <= 6; target++ {
		named := p.targets[target-1].named
		native, err := storageOwnerPersistentFile(ctx, named)
		if err != nil || native != p.original.Targets[target-1] {
			return errors.Join(errStorageCustody, err)
		}
		raw := make([]byte, storageOwnerCatalogBytes)
		n, e := named.file.ReadAt(raw, 0)
		if e != nil || n != len(raw) || !bytes.Equal(raw, p.original.Catalogs[target-5]) {
			return errors.Join(errStorageCustody, e)
		}
		if e = named.current(); e != nil {
			return e
		}
	}
	ready, err := p.readOriginalEnrollmentControl(ctx, storageReadyName, 32)
	if err != nil || sha256.Sum256(ready) != p.original.Ready {
		return errors.Join(errStorageCustody, err)
	}
	hosts, err := p.readOriginalEnrollmentControl(ctx, storageIntentName, storageIntentBytes)
	if err != nil {
		return err
	}
	for slot, pages := range p.original.Hosts {
		for bank, page := range pages {
			start := (slot*2 + bank) * storageIntentPage
			if !bytes.Equal(hosts[start:start+storageIntentPage], page) {
				return errStorageCustody
			}
		}
	}
	if err = op.enterOriginalTransferCall(ctx, op.completion); err != nil {
		return err
	}
	q, e := p.c.ref.cell.q.InspectEnrollment(ctx)
	err = op.leaveOriginalTransferCall(e)
	if err != nil {
		return err
	}
	state := storageHostState(q.View)
	if q.View.Configuration.Maintenance || state.Epoch != op.record.Epoch {
		return errStorageEnrollmentUnresolved
	}
	if op.record.Phase == storageTransferRejected {
		if op.rejectedProof == nil || !op.reserveReturned || storageDefinitelyRejected(op.reserveError) == storageIntentNoRejection || !storageTransferNoTicket(op.record, state) {
			return storagebudget.ErrAuthority
		}
	} else if op.record.Phase != storageTransferRetired || op.appliedProof == nil || !storageTransferApplied(op.record, op.catalog, state) {
		return storagebudget.ErrAuthority
	}
	for bank := 0; bank < 2; bank++ {
		raw := make([]byte, storagebudget.CheckpointBytes)
		named := p.targets[6+bank].named
		n, e := named.file.ReadAt(raw, 0)
		if e != nil || n != len(raw) || !bytes.Equal(raw, q.Checkpoints[bank]) {
			return errors.Join(errStorageCustody, e)
		}
		if e = named.current(); e != nil {
			return e
		}
	}
	p.original.Checkpoints, p.original.Tickets = q.Checkpoints, q.Tickets
	p.initialInspection = &q
	return op.completion.current(ctx)
}
func (op *chatTransferOperation) completeOriginalTransferNamespaceRetirement(ctx context.Context) error {
	if op.namespaceDependency == nil || op.record.Phase != storageTransferRejected && op.record.Phase != storageTransferRetired || op.record.Mode == storageTransferExport || op.tailConsumed || op.tailCompleted || op.completion == nil {
		return storagebudget.ErrAuthority
	}
	if err := op.refreshOriginalTransferQ(ctx); err != nil {
		return err
	}
	catalog, err := op.originalTransferCatalogBase(ctx, op.record.Tail.Bridge)
	if err != nil {
		return err
	}
	raw, err := storageEnrollmentPeakEncodeSource(*op.namespaceDependency)
	if err != nil {
		return err
	}
	found := false
	for i, row := range catalog.Roots {
		if storageTransferCatalogView(row) && row.Source.ControlSelection == 0 && bytes.Equal(row.Source.Relative, raw) {
			if found {
				return storagebudget.ErrAuthority
			}
			if _, err = op.issueOriginalTransferNamespaceRetirement(ctx, row); err != nil {
				return err
			}
			catalog.Roots[i].Source.ControlSelection = 4
			found = true
		}
	}
	if !found {
		return storagebudget.ErrAuthority
	}
	if err = storageTransferSealCatalog(&catalog); err != nil {
		return err
	}
	// No registration or source mutation: this one mutually exclusive P arm
	// retains the actual original Rejected pair/no-ticket origin under its Tail.
	if err = op.preparation.epoch.enrollHistoryAncestorCatalog(ctx, catalog, *op.namespaceDependency); err != nil {
		return err
	}
	p := op.preparation.epoch
	if !p.reconciled || p.completedEvidence == nil || !storageTransferRetentionProof(*p.completedEvidence) {
		return errStorageEnrollmentUnresolved
	}
	op.tailCompleted = true
	op.preparation.completed = true
	return nil
}

// The initial destination enrollment is complete before the whole data Plan
// freezes. Rebase its existing comparison state under the same EX owners;
// closing temporary P descriptions never releases any domain lock.
func (op *chatTransferOperation) resetOriginalTransferEnrollment(ctx context.Context) error {
	p := op.preparation.epoch
	if op.preparation.admitted || op.destinationEnrolled || !p.reconciled || p.completedEvidence == nil || p.candidate == nil || !p.candidate.installed || p.entered || p.err != nil {
		return storagebudget.ErrAuthority
	}
	if err := op.currentBeforeEffect(ctx); err != nil {
		return err
	}
	var err error
	for i := range p.targets {
		if p.targets[i].named.file != nil {
			err = errors.Join(err, p.targets[i].named.file.Close())
			p.targets[i].named.file = nil
		}
	}
	for i := range p.parents {
		if p.parents[i] != nil {
			err = errors.Join(err, p.parents[i].Close())
			p.parents[i] = nil
		}
	}
	if err != nil {
		return err
	}
	p.targets = [8]storageEnrollmentNativeTarget{}
	p.scope = nil
	p.reservation = storageEnrollmentReservation{}
	p.planCursor = 0
	p.initialFence = nil
	p.fenced = false
	p.evidence = nil
	p.reconciled = false
	p.candidate = nil
	p.candidateObserver = nil
	p.factualCensus = nil
	p.completedEvidence = nil
	p.initialInspection = nil
	p.initialPacket = nil
	p.previousEvidence = nil
	p.anchoredStaged = nil
	p.anchoredReference = nil
	p.original = nil
	p.passGroups = nil
	p.identityLaunch = nil
	if err = p.captureExistingOriginal(ctx); err != nil {
		return err
	}
	op.destinationEnrolled = true
	return op.currentBeforeEffect(ctx)
}
func (op *chatTransferOperation) enrollOriginalTransferDestination(ctx context.Context) error {
	if op == nil || op.self != op || op.source == nil || op.path == nil || op.destinationEnrolled || op.preparation.admitted || op.record.Mode == storageTransferExport {
		return storagebudget.ErrAuthority
	}
	p := op.preparation.epoch
	if p.previousEvidence == nil || p.original == nil || p.scope != nil {
		return storagebudget.ErrAuthority
	}
	if err := op.currentBeforeEffect(ctx); err != nil {
		return err
	}
	before := p.previousEvidence.Inner.Catalog
	var sourceRoot storageOwnerRoot
	sourceFound := false
	for _, row := range before.Roots {
		if !storageTransferCatalogView(row) && before.Objects[row.DirectoryObject].Persistent == op.record.Source {
			if sourceFound {
				return storagebudget.ErrAuthority
			}
			sourceRoot = row
			sourceFound = true
		}
	}
	if !sourceFound {
		return storagebudget.ErrAuthority
	}
	existing := op.path.missing == len(op.path.names)
	next := ""
	path := op.path.path
	if !existing {
		next = op.path.names[op.path.missing]
		path = "/" + strings.Join(op.path.names[:op.path.missing], "/")
	}
	var prior *storageOwnerRoot
	for i, row := range before.Roots {
		if int(row.DirectoryObject) >= len(before.Objects) {
			return storagebudget.ErrBounds
		}
		native := before.Objects[row.DirectoryObject].Persistent
		if native != op.path.native {
			continue
		}
		if existing && !storageTransferCatalogView(row) {
			if row.Role != 4 || string(row.Source.Path) != op.path.path {
				return storagebudget.ErrAuthority
			}
			op.destinationEnrolled = true
			return op.currentBeforeEffect(ctx)
		}
		if !existing && storageTransferCatalogView(row) {
			source, err := storageEnrollmentPeakDecodeSource(row.Source.Relative)
			if err != nil {
				return err
			}
			if string(source.Default.Next) != next {
				continue
			}
			if row.Source.ControlSelection == 0 {
				if source.Default.Settings == op.record.Settings && source.Default.SourceNative == op.record.Source && reflect.DeepEqual(row.References, sourceRoot.References) {
					op.destinationEnrolled = true
					return op.currentBeforeEffect(ctx)
				}
				return errStorageEnrollmentUnresolved
			}
			if row.Source.ControlSelection != 4 || !storageTransferNamespaceProof(*p.previousEvidence) {
				return storagebudget.ErrAuthority
			}
			terminal, e := storageTransferNamespaceTerminal(before, row.Definition.ID)
			if e != nil {
				return e
			}
			if terminal.Definition.ID != row.Definition.ID {
				continue
			}
			copy := before.Roots[i]
			if prior != nil {
				return storagebudget.ErrAuthority
			}
			prior = &copy
		}
	}
	marker := storageOwnerPersistentObject{}
	markerBytes := uint64(0)
	if existing {
		info, err := op.path.root.Stat(".")
		if err != nil || !storagePrivate(info, true) {
			return errors.Join(errStorageCustody, err)
		}
		entries, err := storageEntries(op.path.root)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.Name() != dataMarkerName {
				return withCode(CodeBadRequest, errors.New("the destination already has data; choose an empty folder"))
			}
		}
		if len(entries) > 0 {
			raw, err := readPrivateData(op.path.root, dataMarkerName, 4096)
			if err != nil {
				return err
			}
			var m dataMarker
			if decodeDataJSON(raw, &m) != nil || m.Version != 1 || m.Owner != op.preparation.server.storageOwner() {
				return errStorageCustody
			}
			named, err := storageOpenFile(op.path.root, dataMarkerName, int64(len(raw)))
			if err != nil {
				return err
			}
			marker, err = storageOwnerPersistentFile(ctx, named)
			err = errors.Join(err, named.current(), named.file.Close())
			if err != nil {
				return err
			}
			markerBytes = uint64(len(raw))
		}
	}
	bridge, err := storagebudget.NewID()
	if err != nil {
		return err
	}
	catalog, err := op.originalTransferCatalogBase(ctx, bridge)
	if err != nil {
		return err
	}
	source, err := op.appendOriginalTransferRoot(ctx, &catalog, op.path.native, path, next, existing, marker, markerBytes, bridge, prior)
	if err != nil {
		return err
	}
	if err = p.enrollHistoryAncestorCatalog(ctx, catalog, source); err != nil {
		return err
	}
	return op.resetOriginalTransferEnrollment(ctx)
}

func (s *Server) originalTransferRequestPath(request moveChatData) error {
	if !validMoveRequest(request) {
		return withCode(CodeBadRequest, errors.New("choose an absolute folder and reload storage settings"))
	}
	if _, err := storageTransferPathParts(request.Path); err != nil {
		return err
	}
	if err := s.policyMachineTarget(request.Path); err != nil {
		return err
	}
	if s.containedDeskData() && !pathContains(filepath.Join(s.projectDir, ".desk-private"), request.Path) {
		return withCode(CodeBadRequest, errors.New("choose a private data folder inside this desk"))
	}
	return nil
}
func (op *chatTransferOperation) planOriginalTransferWorkload(ctx context.Context, request moveChatData, plan chatTransferPlan) error {
	if op.source == nil || op.path == nil || op.path.path != request.Path || op.preparation.admitted || len(plan.Files) > maxStorageFiles {
		return storagebudget.ErrAuthority
	}
	s := op.preparation.server
	store := op.source
	if request.Revision != store.revision {
		return withCode(CodeStale, errors.New("the location changed; reload storage settings before moving data"))
	}
	if request.Path == store.location.Path || pathContains(request.Path, store.location.Path) || pathContains(store.location.Path, request.Path) || !s.containedDeskData() && pathContains(s.projectDir, request.Path) || pathContains(request.Path, s.projectDir) || request.Path == s.configDir {
		if !(store.legacy && request.Path == s.defaultChatDataPath() && request.Path != s.configDir && !pathContains(s.projectDir, request.Path)) {
			return withCode(CodeBadRequest, errors.New("choose a separate private folder outside the project and current data folder"))
		}
	}
	if err := s.authorizeStoredPolicyWorkspace(op.preparation.request, store.root); err != nil {
		return err
	}
	if err := validateStoredBindings(store.root); err != nil {
		return err
	}
	op.record.NewPointer = s.encodeDataLocation(dataLocation{Version: 1, Path: request.Path, Previous: store.location.Path})
	op.record.Outputs = make([]storageTransferOutput, len(plan.Files))
	for i, item := range plan.Files {
		if item.Bytes < 0 || !chatStorageFile(item.Name) {
			return storagebudget.ErrBounds
		}
		digest, err := hex.DecodeString(item.SHA256)
		if err != nil || len(digest) != 32 {
			return storagebudget.ErrBounds
		}
		op.record.Outputs[i] = storageTransferOutput{Name: item.Name, Bytes: uint64(item.Bytes)}
		copy(op.record.Outputs[i].SHA256[:], digest)
	}
	sort.Slice(op.record.Outputs, func(i, j int) bool { return op.record.Outputs[i].Name < op.record.Outputs[j].Name })
	for i := 1; i < len(op.record.Outputs); i++ {
		if op.record.Outputs[i-1].Name == op.record.Outputs[i].Name {
			return storagebudget.ErrBounds
		}
	}
	return op.currentBeforeEffect(ctx)
}
func (op *chatTransferOperation) writeOriginalTransferTarget(ctx context.Context) error {
	if err := op.createOriginalTransferDirectories(ctx); err != nil {
		return err
	}
	if op.record.MarkerBytes > 0 {
		data, err := json.Marshal(dataMarker{Version: 1, Owner: op.preparation.server.storageOwner()})
		if err != nil {
			return err
		}
		if err = op.writeOriginalTransferBytes(ctx, storageTransferMarkerOrdinal, 0, data); err != nil {
			return err
		}
	}
	kind := chatTransferMove
	var archive *zip.Reader
	var entries map[string]*zip.File
	var manifest map[string]backupEntry
	if op.record.Mode == storageTransferRestore {
		kind = chatTransferRestore
		if op.stage == nil || op.stage.file == nil {
			return storagebudget.ErrAuthority
		}
		decoded, m, err := validateChatBackupReader(ctx, op.stage.file, int64(op.record.InputBytes))
		if err != nil {
			return err
		}
		archive = decoded
		entries = map[string]*zip.File{}
		manifest = map[string]backupEntry{}
		for _, entry := range archive.File {
			entries[entry.Name] = entry
		}
		for _, entry := range m.Files {
			manifest[entry.Name] = entry
		}
	}
	for i, output := range op.record.Outputs {
		if err := op.currentBeforeEffect(ctx); err != nil {
			return err
		}
		var raw []byte
		var err error
		if kind == chatTransferMove {
			raw, err = readPrivateData(op.source.root, output.Name, storageFileLimit(output.Name))
		} else {
			entry := entries[output.Name]
			expected, exists := manifest[output.Name]
			if entry == nil || !exists {
				return storagebudget.ErrAuthority
			}
			raw, err = readBackupFile(entry, storageFileLimit(output.Name))
			if err == nil && (int64(len(raw)) != expected.Bytes || digestOf(raw) != expected.SHA256) {
				err = errStorageCustody
			}
			if err == nil {
				err = op.preparation.server.authorizeOriginalTransferRestore(op.preparation.request, output.Name, raw)
			}
		}
		if err != nil {
			return err
		}
		if err = validateStorageFile(output.Name, raw); err != nil {
			return err
		}
		data, err := transformChatTransfer(output.Name, raw, kind)
		if err != nil {
			return err
		}
		if err = op.writeOriginalTransferBytes(ctx, storageTransferOutputOrdinal, i, data); err != nil {
			return err
		}
	}
	target, err := op.originalTransferTarget()
	if err != nil {
		return err
	}
	if err = validateStoredBindings(target); err != nil {
		return err
	}
	return op.publishOriginalTransferPointer(ctx)
}
func (s *Server) withOriginalChatMove(r *http.Request, request moveChatData) (status storageStatus, err error) {
	request.Path = filepath.Clean(request.Path)
	if err = s.originalTransferRequestPath(request); err != nil {
		return status, err
	}
	if err = s.ensureDefaultHistoryEnrollment(r.Context()); err != nil {
		return status, err
	}
	p, err := s.prepareOriginalChatTransfer(r, storageTransferMove)
	if err != nil {
		return status, err
	}
	p.targetPath = request.Path
	err = s.withOriginalChatTransfer(p, func(op *chatTransferOperation) error {
		if e := op.captureOriginalTransferSource(p.ctx); e != nil {
			return e
		}
		plan, e := planChatData(p.ctx, op.source)
		if e != nil {
			return e
		}
		if e = op.planOriginalTransferWorkload(p.ctx, request, plan); e != nil {
			return e
		}
		if e = op.enrollOriginalTransferDestination(p.ctx); e != nil {
			return e
		}
		if e = op.reserveWhole(p.ctx); e != nil {
			return e
		}
		if e = op.writeOriginalTransferTarget(p.ctx); e != nil {
			return e
		}
		target, e := op.originalTransferTarget()
		if e != nil {
			return e
		}
		// Response metadata is detached from the captured source/selection wrapper.
		observed := chatDataStore{root: target, location: dataLocation{Version: 1, Path: request.Path, Previous: op.source.location.Path}, revision: digestOf(op.record.NewPointer)}
		status, e = s.describeStorage(&observed)
		status.Managed = s.containedDeskData()
		return e
	})
	return status, err
}
func (s *Server) authorizeOriginalTransferRestore(r *http.Request, name string, data []byte) error {
	if assistantSourceFileName.MatchString(name) {
		return s.authorizeAssistantSourceStorage(r)
	}
	if assistantRunFileName.MatchString(name) || assistantDialectReadRecoveryFileName.MatchString(name) {
		return s.authorizeAssistantRunStorage(r)
	}
	if privateWorkspaceStorageFile(name) {
		if err := s.authorizePrivateWorkspace(r); err != nil {
			return err
		}
		if graphDraftFileName.MatchString(name) {
			return s.admitGraphDraftUpdate(r, []byte(`{"version":1,"drafts":[],"deleted":[]}`), data)
		}
		return nil
	}
	drafts := draftPackFileName.MatchString(name)
	if !drafts && !conversationFileName.MatchString(name) {
		return nil
	}
	if err := s.authorizePolicyWorkspace(r, data, drafts); err != nil {
		return err
	}
	empty := []byte(`{"version":1,"chats":[]}`)
	if drafts {
		empty = []byte(`{"version":1,"drafts":[]}`)
	}
	return s.admitPolicyWorkspaceUpdate(r, empty, data, drafts)
}

type chatTransferBodyReader struct {
	preparation *chatTransferPreparation
	reader      io.Reader
}

func (b chatTransferBodyReader) Read(buffer []byte) (int, error) {
	if b.preparation == nil || b.preparation.self != b.preparation || b.reader == nil {
		return 0, storagebudget.ErrAuthority
	}
	if err := b.preparation.current(); err != nil {
		return 0, err
	}
	n, err := b.reader.Read(buffer)
	if canceled := b.preparation.ctx.Err(); canceled != nil {
		return n, errors.Join(err, canceled)
	}
	return n, err
}

// Network retention precedes work/writes/private acquisition. One registered
// process permit owns the one bounded RAM archive, with checked part/Body Close.
func (s *Server) readOriginalChatRestore(p *chatTransferPreparation) (request moveChatData, input []byte, plan chatTransferPlan, err error) {
	if p == nil || p.self != p || p.server != s || p.mode != storageTransferRestore || p.operation != nil || p.epoch != nil || !p.permit {
		return request, nil, plan, storagebudget.ErrAuthority
	}
	if e := p.attachRestoreBody(); e != nil {
		return request, nil, plan, e
	}
	reader, e := p.request.MultipartReader()
	if e != nil {
		return request, nil, plan, e
	}
	part, e := reader.NextPart()
	if e != nil {
		return request, nil, plan, e
	}
	if part.FormName() != "settings" {
		return request, nil, plan, errors.Join(withCode(CodeBadRequest, errors.New("restore settings must precede the backup")), part.Close())
	}
	data, e := readBounded(chatTransferBodyReader{p, part}, 16<<10)
	e = errors.Join(e, part.Close())
	if e != nil || decodeDataJSON(data, &request) != nil || !validMoveRequest(request) {
		return request, nil, plan, errors.Join(withCode(CodeBadRequest, errors.New("choose an absolute destination and reload storage settings")), e)
	}
	request.Path = filepath.Clean(request.Path)
	if e = s.originalTransferRequestPath(request); e != nil {
		return request, nil, plan, e
	}
	part, e = reader.NextPart()
	if e != nil {
		return request, nil, plan, e
	}
	if part.FormName() != "backup" {
		return request, nil, plan, errors.Join(withCode(CodeBadRequest, errors.New("a chat backup is required")), part.Close())
	}
	input, e = readBounded(chatTransferBodyReader{p, part}, maxBackupArchive)
	e = errors.Join(e, part.Close())
	if e != nil {
		return request, nil, plan, e
	}
	extra, e := reader.NextPart()
	if extra != nil {
		return request, nil, plan, errors.Join(withCode(CodeBadRequest, errors.New("unexpected data after the backup")), e, extra.Close())
	}
	if e != io.EOF {
		return request, nil, plan, errors.Join(withCode(CodeBadRequest, errors.New("unexpected data after the backup")), e)
	}
	if e = p.closeBody(); e != nil {
		return request, nil, plan, e
	}
	plan, e = planChatBackup(p.ctx, bytes.NewReader(input), int64(len(input)), func(name string, data []byte) error { return s.authorizeOriginalTransferRestore(p.request, name, data) })
	if e != nil {
		return request, nil, plan, e
	}
	return request, input, plan, p.current()
}
func (s *Server) withOriginalChatRestore(w http.ResponseWriter, r *http.Request) (status storageStatus, err error) {
	if err = s.ensureDefaultHistoryEnrollment(r.Context()); err != nil {
		return status, err
	}
	p, err := s.prepareOriginalChatTransfer(r, storageTransferRestore)
	if err != nil {
		return status, err
	}
	owned := false
	defer func() {
		if !owned {
			err = p.finish(err)
		}
	}()
	r.Body = http.MaxBytesReader(w, r.Body, maxBackupArchive+(32<<10))
	request, input, plan, err := s.readOriginalChatRestore(p)
	if err != nil {
		return status, err
	}
	p.targetPath = request.Path
	owned = true
	err = s.withOriginalChatTransfer(p, func(op *chatTransferOperation) error {
		op.input = input
		op.record.InputBytes = uint64(len(input))
		op.record.InputDigest = sha256.Sum256(input)
		if e := op.captureOriginalTransferSource(p.ctx); e != nil {
			return e
		}
		if e := op.planOriginalTransferWorkload(p.ctx, request, plan); e != nil {
			return e
		}
		if e := op.enrollOriginalTransferDestination(p.ctx); e != nil {
			return e
		}
		if e := op.reserveWhole(p.ctx); e != nil {
			return e
		}
		if e := op.writeOriginalTransferBytes(p.ctx, storageTransferStageOrdinal, 0, op.input); e != nil {
			return e
		}
		if e := op.writeOriginalTransferTarget(p.ctx); e != nil {
			return e
		}
		target, e := op.originalTransferTarget()
		if e != nil {
			return e
		}
		observed := chatDataStore{root: target, location: dataLocation{Version: 1, Path: request.Path, Previous: op.source.location.Path}, revision: digestOf(op.record.NewPointer)}
		status, e = s.describeStorage(&observed)
		status.Managed = s.containedDeskData()
		return e
	})
	input = nil
	return status, err
}

func storageTransferProjectionChanged(before, after storageOwnerCatalogV2) bool {
	for _, old := range before.Roots {
		for _, next := range after.Roots {
			if old.Definition.ID == next.Definition.ID && (!reflect.DeepEqual(old.References, next.References) || old.Source.ControlSelection != next.Source.ControlSelection) {
				return true
			}
		}
	}
	for _, old := range before.OwnerControls {
		for _, next := range after.OwnerControls {
			if int(old.ParentObject) < len(before.Objects) && int(next.ParentObject) < len(after.Objects) && before.Objects[old.ParentObject].Persistent == after.Objects[next.ParentObject].Persistent && bytes.Equal(old.Name, next.Name) && old.RelationDigest != next.RelationDigest {
				return true
			}
		}
	}
	return false
}

func storageTransferRejectedRetirementCompleted(core storageIntentRecord, original storageOwnerCatalogV2, e *storageEnrollmentPeakEvidence) bool {
	dependency, err := storageTransferNamespaceDependency(core, original)
	if err != nil || dependency == nil || e == nil || e.Phase != 2 || !storageTransferNamespaceProof(*e) {
		return false
	}
	raw, err := storageEnrollmentPeakEncodeSource(*dependency)
	if err != nil {
		return false
	}
	for _, old := range original.Roots {
		if !storageTransferCatalogView(old) || !bytes.Equal(old.Source.Relative, raw) {
			continue
		}
		next, exists := storageTransferCatalogRoot(e.Inner.Catalog, old.Definition.ID)
		if exists && next.Source.ControlSelection == 4 && next.Definition == old.Definition && bytes.Equal(next.Source.Relative, old.Source.Relative) && reflect.DeepEqual(next.References, old.References) {
			return true
		}
	}
	return false
}

// Prefix validation consumes the actual original host/Q comparison tuple.
// Completed coverage is produced later by the same fixed P schedule; it cannot
// be required circularly before that schedule is admitted.
func storageTransferPrefixActionProof(source storageEnrollmentPeakSource, before, after storageOwnerCatalogV2, state storagebudget.HostState, hosts [64][2][]byte) bool {
	d := source.Default
	if d == nil || d.Workflow != 4 && d.Workflow != 5 && d.Workflow != 6 {
		return false
	}
	if d.Workflow == 6 {
		if !storageTransferNoDefinitionChange(before, after) {
			return storageEnrollmentDefaultSourceNamespace(source)
		}
		board := storageEnrollmentPeakBoard{SourceKind: 11, Source: source}
		row, ok := storageTransferOwningCompletionRegistration(board, after)
		if !ok {
			return false
		}
		matches := 0
		for slot, pages := range hosts {
			choice, err := storageSelectCreationHost(pages[0], pages[1], slot, before.Installation, before.QBinding)
			if err != nil {
				return false
			}
			core := choice.Slot.record
			if core.Transfer == nil || core.Transfer.Tail.Bridge != after.Transition || core.Transfer.Settings != d.Settings || core.Transfer.Source != d.SourceNative {
				continue
			}
			matches++
			if !storageTransferOwningAbortCore(core, source, before, row, state) {
				return false
			}
		}
		return matches == 1
	}
	if !storageEnrollmentRetainsOriginalWithDefault(before, after, &source) {
		return false
	}
	if d.Workflow == 4 {
		changed := storagebudget.ID{}
		for _, old := range before.Roots {
			next, exists := storageTransferCatalogRoot(after, old.Definition.ID)
			if !exists {
				return false
			}
			if old.Source.ControlSelection != next.Source.ControlSelection {
				if changed != (storagebudget.ID{}) || old.Source.ControlSelection != 0 || next.Source.ControlSelection != 4 {
					return false
				}
				changed = old.Definition.ID
			}
		}
		for _, row := range after.Roots {
			if !storageTransferCatalogView(row) || row.Source.ControlSelection != 4 {
				continue
			}
			for _, slot := range state.Slots {
				if !storageUnretired(slot) {
					continue
				}
				for _, component := range slot.Ticket.Components {
					if component.Root == row.Definition.ID {
						return false
					}
				}
			}
		}
		if changed == (storagebudget.ID{}) {
			return true
		}
		old, exists := storageTransferCatalogRoot(before, changed)
		if !exists {
			return false
		}
		raw, err := storageEnrollmentPeakEncodeSource(source)
		if err != nil || !bytes.Equal(raw, old.Source.Relative) {
			return false
		}
		for slot, pages := range hosts {
			choice, err := storageSelectCreationHost(pages[0], pages[1], slot, before.Installation, before.QBinding)
			if err != nil {
				return false
			}
			core := choice.Slot.record
			if core.Transfer == nil || core.Phase != storageIntentRejected && !(core.Phase == storageIntentRetired && core.Outcome == storageIntentAborted) || core.Transfer.Tail.Bridge != after.Transition {
				continue
			}
			dependency, e := storageTransferNamespaceDependency(core, before)
			if e == nil && dependency != nil && reflect.DeepEqual(*dependency, source) && storageTransferNamespaceActionApplied(core, before, state) {
				return true
			}
		}
		return false
	}
	if !storageTransferSourceSelection(before, after, source) {
		return false
	}
	for slot, pages := range hosts {
		choice, err := storageSelectCreationHost(pages[0], pages[1], slot, before.Installation, before.QBinding)
		if err != nil {
			return false
		}
		core := choice.Slot.record
		t := core.Transfer
		if t == nil || core.Phase != storageIntentRetired || core.Outcome != storageIntentPublished || t.Tail.Bridge != after.Transition || t.Settings != d.Settings || t.Source != d.SourceNative || t.Selected != d.Selected || t.Marker != d.Marker || t.Location != d.Location || t.LocationPayload != d.LocationPayload || sha256.Sum256(choice.Slot.page) != d.CreationPage || t.PointerState != 2 {
			continue
		}
		fanout, e := storageTransferCatalogIntentFanout(core, before)
		if e == nil && int(core.Token.Slot) < len(state.Slots) && storageIntentFanoutAppliedOperand(core, before.Installation, before.QBinding, state.Slots[core.Token.Slot], fanout) {
			return true
		}
	}
	return false
}

func storageTransferNamespaceActionApplied(core storageIntentRecord, original storageOwnerCatalogV2, state storagebudget.HostState) bool {
	if core.Transfer == nil {
		return false
	}
	if core.Phase == storageIntentRejected {
		return storageTransferCompactNoTicket(core, state, true)
	}
	if core.Phase != storageIntentRetired || core.Outcome != storageIntentAborted || core.Transfer.PointerState != 0 || !core.Transfer.Cleanup.complete() || int(core.Token.Slot) >= len(state.Slots) {
		return false
	}
	fanout, err := storageTransferCatalogIntentFanout(core, original)
	return err == nil && storageIntentFanoutAppliedOperand(core, original.Installation, original.QBinding, state.Slots[core.Token.Slot], fanout)
}

func storageTransferProjectionSelected(indices []uint16, index uint16) bool {
	for _, selected := range indices {
		if selected == index {
			return true
		}
	}
	return false
}

// Board-only source operands retain the original selected source and actual
// owning destination. Existing root source bytes and definitions never change.
func (op *chatTransferOperation) originalTransferOwningScope(ctx context.Context, catalog storageOwnerCatalogV2, row storageOwnerRoot, workflow byte) (storageEnrollmentPeakSource, error) {
	var source storageEnrollmentPeakSource
	p := op.preparation.epoch
	if row.Role != 4 || row.Source.Kind != 4 && row.Source.Kind != 11 || int(row.DirectoryObject) >= len(catalog.Objects) || workflow != 5 && workflow != 6 || op.path == nil {
		return source, storagebudget.ErrAuthority
	}
	focal := op.record.RequesterWorkspace
	if workflow == 6 {
		focal = row.Definition.Workspace
		if focal == (storagebudget.ID{}) && len(row.References) > 0 {
			focal = row.References[0]
		}
	}
	var workspace storageOwnerWorkspace
	var fs storageOwnerFilesystem
	for _, w := range catalog.Workspaces {
		if w.Definition.ID == focal {
			workspace = w
		}
	}
	for _, f := range catalog.Filesystems {
		if f.Definition.ID == row.Definition.Filesystem {
			fs = f
		}
	}
	if workspace.Definition.ID == (storagebudget.ID{}) || fs.Definition.ID == (storagebudget.ID{}) {
		return source, storagebudget.ErrAuthority
	}
	member := false
	for _, id := range row.References {
		member = member || id == focal
	}
	if !member {
		return source, storagebudget.ErrAuthority
	}
	branch := byte(storageCreationConfigured)
	if op.preparation.server.containedDeskData() {
		branch = storageCreationContained
	} else if string(row.Source.Path) == op.preparation.server.defaultChatDataPath() {
		branch = storageCreationPersonal
	}
	source = storageEnrollmentPeakSource{Installation: catalog.Installation, QBinding: catalog.QBinding, Project: p.original.Project, Workspace: focal, WorkspaceGeneration: workspace.Definition.Generation, OwnerKey: append([]byte{}, workspace.OwnerKey...), HistoryIdentity: append([]byte{}, workspace.HistoryIdentity...), Filesystem: fs.Definition.ID, FilesystemIdentity: fs.Definition.Identity, FilesystemGeneration: fs.Definition.Generation, CapacityCount: row.Definition.CapacityCount, CapacityRefs: row.Definition.CapacityRefs, Generation: row.Definition.Generation, Namespace: []byte("transfer-root"), Default: &storageEnrollmentDefaultSource{Workflow: workflow, Branch: branch, Settings: op.record.Settings, SourceNative: op.record.Source, Selected: catalog.Objects[row.DirectoryObject].Persistent, SelectedPath: append([]byte{}, row.Source.Path...), OwnSelf: 1, Next: []byte{}}}
	if workflow == 5 {
		source.Namespace = []byte("transfer-data")
		source.Default.OwnSelf = 0
		source.Default.Marker = op.record.MarkerNative
		if op.record.MarkerBefore.valid(2) {
			source.Default.Marker = op.record.MarkerBefore
		}
		source.Default.Location = op.record.PointerNative
		source.Default.LocationPayload = sha256.Sum256(op.record.NewPointer)
		source.Default.CreationPage = sha256.Sum256(op.host.page)
	}
	var err error
	source.HomeSource, err = storageEnrollmentHomeSourceDigest(source, catalog)
	if err != nil {
		return source, err
	}
	edges := append([]storageNamedRoot{}, op.path.edges...)
	if workflow == 5 {
		edges = append(edges, op.created...)
	}
	names, err := storageTransferSourceChainParts(source.Default)
	if err != nil || len(names) != len(edges) {
		return source, errors.Join(storagebudget.ErrAuthority, err)
	}
	source.Default.ChainCount = uint16(len(edges))
	natives := make([]storageOwnerPersistentObject, len(edges))
	for window := 0; window < (len(edges)+31)/32; window++ {
		first, last, e := storageTransferParentWindow(len(edges), window)
		if e != nil {
			return source, e
		}
		for i := first; i < last; i++ {
			if e = edges[i].current(); e != nil {
				return source, e
			}
			natives[i], e = storageOwnerPersistentRoot(ctx, edges[i].root)
			if e != nil {
				return source, e
			}
		}
	}
	anchor, err := storageOwnerPersistentRoot(ctx, op.path.base)
	if err != nil {
		return source, err
	}
	source.Default.ChainDigest, err = storageTransferChainDigest(source, anchor, names, natives)
	if err != nil {
		return source, err
	}
	source.CapacityBinding, err = storageEnrollmentPeakSourceMapping(source, catalog, row)
	if err != nil {
		return source, err
	}
	if _, err = storageTransferVerifyChainAt(ctx, source, op.path.base, edges); err != nil {
		return source, err
	}
	return source, nil
}
func (op *chatTransferOperation) completeOriginalTransferSelection(ctx context.Context) error {
	if op.record.Phase != storageTransferRetired || op.record.PointerState != 2 || op.record.Mode == storageTransferExport || op.appliedProof == nil || !storageTransferApplied(op.record, op.catalog, *op.appliedProof) || op.tailConsumed || op.tailCompleted {
		return storagebudget.ErrAuthority
	}
	if err := op.refreshOriginalTransferQ(ctx); err != nil {
		return err
	}
	catalog, err := op.originalTransferCatalogBase(ctx, op.record.Tail.Bridge)
	if err != nil {
		return err
	}
	target, err := storageTransferFinalTarget(op.record)
	if err != nil {
		return err
	}
	location, err := storageTransferResolvedLocation(op.record.NewPointer, func() string { path, _ := storageTransferNativePath(op.catalog, op.record.Settings); return path }())
	if err != nil {
		return err
	}
	var targetID storagebudget.ID
	for _, row := range catalog.Roots {
		if !storageTransferCatalogView(row) && catalog.Objects[row.DirectoryObject].Persistent == target {
			if targetID != (storagebudget.ID{}) {
				return storagebudget.ErrAuthority
			}
			targetID = row.Definition.ID
		}
	}
	if targetID == (storagebudget.ID{}) {
		marker := op.record.MarkerNative
		if op.record.MarkerBefore.valid(2) {
			marker = op.record.MarkerBefore
		}
		if _, err = op.appendOriginalTransferRoot(ctx, &catalog, target, location.Path, "", true, marker, func() uint64 {
			if op.record.MarkerBefore.valid(2) {
				return op.record.TargetExisting.Bytes
			}
			return op.record.MarkerBytes
		}(), op.record.Tail.Bridge, nil); err != nil {
			return err
		}
		for _, row := range catalog.Roots {
			if !storageTransferCatalogView(row) && catalog.Objects[row.DirectoryObject].Persistent == target {
				targetID = row.Definition.ID
			}
		}
	}
	if targetID == (storagebudget.ID{}) {
		return storagebudget.ErrAuthority
	}
	// Existing E keeps its original immutable definition/source and receives only
	// the exact B union A charge membership proved by the original whole Plan.
	sourceRefs := []storagebudget.ID{}
	sourceFound := false
	for _, row := range op.catalog.Roots {
		if !storageTransferCatalogView(row) && op.catalog.Objects[row.DirectoryObject].Persistent == op.record.Source {
			if sourceFound {
				return storagebudget.ErrAuthority
			}
			sourceRefs = append(sourceRefs, row.References...)
			sourceFound = true
		}
	}
	if !sourceFound {
		return storagebudget.ErrAuthority
	}
	for i, row := range catalog.Roots {
		if row.Definition.ID != targetID {
			continue
		}
		set := map[storagebudget.ID]bool{}
		for _, id := range row.References {
			set[id] = true
		}
		for _, id := range sourceRefs {
			set[id] = true
		}
		refs := []storagebudget.ID{}
		for id := range set {
			refs = append(refs, id)
		}
		sort.Slice(refs, func(i, j int) bool { return bytes.Compare(refs[i][:], refs[j][:]) < 0 })
		catalog.Roots[i].References = refs
		marker := op.record.MarkerNative
		if op.record.MarkerBefore.valid(2) {
			marker = op.record.MarkerBefore
		}
		found := false
		for _, object := range catalog.Objects {
			if object.Role == 23 && object.Parent == row.DirectoryObject && string(object.Name) == dataMarkerName {
				if object.Persistent != marker {
					return errStorageCustody
				}
				found = true
			}
		}
		if !found {
			catalog.Objects = append(catalog.Objects, storageOwnerObject{Role: 23, Source: 3, Parent: row.DirectoryObject, Name: []byte(dataMarkerName), Persistent: marker, OwnerClass: 2, ModeClass: 2, FixedLength: op.record.MarkerBytes})
		}
	}
	settingsIndex := storageOwnerNoObject
	for i, object := range catalog.Objects {
		if object.Persistent == op.record.Settings {
			if settingsIndex != storageOwnerNoObject {
				return storagebudget.ErrAuthority
			}
			settingsIndex = uint32(i)
		}
	}
	if settingsIndex == storageOwnerNoObject {
		return storagebudget.ErrAuthority
	}
	index := -1
	for i, control := range catalog.OwnerControls {
		if control.ParentObject == settingsIndex && string(control.Name) == dataLocationName {
			if index >= 0 {
				return storagebudget.ErrAuthority
			}
			index = i
		}
	}
	control := storageOwnerControl{Role: 2, ParentObject: settingsIndex, Name: []byte(dataLocationName), OwnerKind: 2, OwnerKey: []byte(op.preparation.server.storageOwner()), Location: storageOwnerLocation{Version: 1, Path: []byte(location.Path), Previous: []byte(location.Previous), Selected: target, PreviousSelected: op.record.Source}, MaxBytes: 16384}
	control.RelationDigest, err = catalog.controlDigest(control)
	if err != nil {
		return err
	}
	if index < 0 {
		catalog.OwnerControls = append(catalog.OwnerControls, control)
		index = len(catalog.OwnerControls) - 1
	} else {
		old := catalog.OwnerControls[index]
		if old.Role != control.Role || old.OwnerKind != control.OwnerKind || !bytes.Equal(old.OwnerKey, control.OwnerKey) || old.MaxBytes != control.MaxBytes {
			return storagebudget.ErrAuthority
		}
		catalog.OwnerControls[index] = control
	}
	for i, row := range catalog.Roots {
		if row.Source.ControlKind != 2 || row.Source.ControlIndex != uint32(index) {
			continue
		}
		catalog.Roots[i].Source.ControlSelection = 3
		if bytes.Equal(row.Source.Path, control.Location.Path) && catalog.Objects[row.DirectoryObject].Persistent == target {
			catalog.Roots[i].Source.ControlSelection = 1
		}
		if bytes.Equal(row.Source.Path, control.Location.Previous) && catalog.Objects[row.DirectoryObject].Persistent == op.record.Source {
			catalog.Roots[i].Source.ControlSelection = 2
		}
	}
	if err = storageEnrollmentSortControls(&catalog); err != nil {
		return err
	}
	if err = storageTransferSealCatalog(&catalog); err != nil {
		return err
	}
	row, ok := storageTransferCatalogRoot(catalog, targetID)
	if !ok {
		return storagebudget.ErrAuthority
	}
	scope, err := op.originalTransferOwningScope(ctx, catalog, row, 5)
	if err != nil {
		return err
	}
	if err = storageTransferSelectionRelation(op.record, op.catalog, catalog); err != nil {
		return err
	}
	if err = op.preparation.epoch.enrollHistoryAncestorCatalog(ctx, catalog, scope); err != nil {
		return err
	}
	p := op.preparation.epoch
	if !p.reconciled || p.completedEvidence == nil || !storageTransferTailBound(op.host.record, *p.completedEvidence) {
		return errStorageEnrollmentUnresolved
	}
	op.tailCompleted = true
	op.preparation.completed = true
	return nil
}

func storageTransferNoDefinitionChange(before, after storageOwnerCatalogV2) bool {
	return before.Installation == after.Installation && before.QBinding == after.QBinding && before.Policy == after.Policy && before.AfterDefinitionsDigest == after.AfterDefinitionsDigest && reflect.DeepEqual(before.OriginalOwner, after.OriginalOwner) && reflect.DeepEqual(before.Objects, after.Objects) && reflect.DeepEqual(before.OwnerControls, after.OwnerControls) && reflect.DeepEqual(before.Roots, after.Roots) && reflect.DeepEqual(before.Workspaces, after.Workspaces) && reflect.DeepEqual(before.Filesystems, after.Filesystems) && reflect.DeepEqual(before.Constraints, after.Constraints) && reflect.DeepEqual(before.Profiles, after.Profiles)
}
func storageTransferOwningCompletionRegistration(board storageEnrollmentPeakBoard, catalog storageOwnerCatalogV2) (storageOwnerRoot, bool) {
	var out storageOwnerRoot
	if board.SourceKind != 11 || board.Source.Default == nil || board.Source.Default.Workflow != 6 || !storageEnrollmentDefaultSourceNamespace(board.Source) {
		return out, false
	}
	for _, list := range board.Registration {
		if len(list) != 0 {
			return out, false
		}
	}
	d := board.Source.Default
	found := false
	for _, row := range catalog.Roots {
		if row.Role != 4 || row.Source.Kind != 4 && row.Source.Kind != 11 || int(row.DirectoryObject) >= len(catalog.Objects) || catalog.Objects[row.DirectoryObject].Persistent != d.Selected || !bytes.Equal(row.Source.Path, d.SelectedPath) {
			continue
		}
		if found || !storageEnrollmentPeakSourceJoins(board.Source, catalog, row) {
			return storageOwnerRoot{}, false
		}
		out = row
		found = true
	}
	return out, found
}
func storageTransferOwningAbortCore(core storageIntentRecord, source storageEnrollmentPeakSource, before storageOwnerCatalogV2, row storageOwnerRoot, state storagebudget.HostState) bool {
	d, t := source.Default, core.Transfer
	if d == nil || t == nil || core.Phase != storageIntentRetired || core.Outcome != storageIntentAborted || t.Mode == storageTransferExport || t.PointerState != 0 || !t.Cleanup.complete() || t.Settings != d.Settings || t.Source != d.SourceNative || t.Selected != d.Selected || t.Tail.Project != source.Project || !t.Empty {
		return false
	}
	digest, err := storageTransferCatalogDigest(before)
	if err != nil || digest != t.Catalog {
		return false
	}
	original, ok := storageTransferCatalogRoot(before, row.Definition.ID)
	if !ok || !reflect.DeepEqual(original, row) {
		return false
	}
	bound := false
	for i, component := range core.Components {
		if component.Root == row.Definition.ID {
			if component.RootGeneration != row.Definition.Generation || component.Owner != row.Definition.Owner || i >= len(core.RootIdentities) || core.RootIdentities[i] != row.Definition.Identity {
				return false
			}
			bound = true
		}
	}
	if !bound || !storageTransferNamespaceActionApplied(core, before, state) {
		return false
	}
	found := false
	for _, control := range before.OwnerControls {
		if int(control.ParentObject) >= len(before.Objects) {
			return false
		}
		if before.Objects[control.ParentObject].Persistent != d.Settings || string(control.Name) != dataLocationName {
			continue
		}
		if found || t.OldAbsent || control.Role != 2 || control.Location.Selected != d.SourceNative {
			return false
		}
		found = true
	}
	return t.OldAbsent && !found || !t.OldAbsent && found && t.OldPointerNative.valid(2) && t.OldPointerDigest != (storagebudget.Digest{})
}
func storageTransferOwningAbortProof(e storageEnrollmentPeakEvidence) bool {
	row, ok := storageTransferOwningCompletionRegistration(e.Board, e.Inner.Catalog)
	if !ok {
		return false
	}
	before, ok := storageTransferNamespaceBefore(e)
	if !ok || !storageTransferNoDefinitionChange(*before, e.Inner.Catalog) {
		return false
	}
	matches := 0
	for _, w := range e.Witnesses {
		core := w.Core
		if !w.Selected || core.Transfer == nil || core.Transfer.Tail.Bridge != e.Bridge || core.Transfer.Settings != e.Board.Source.Default.Settings || core.Transfer.Source != e.Board.Source.Default.SourceNative {
			continue
		}
		matches++
		if core.Phase != storageIntentRetired || core.Outcome != storageIntentAborted || w.Digest != sha256.Sum256(w.Page) || int(w.Origin) >= len(e.Board.Origins) {
			return false
		}
		origin := e.Board.Origins[w.Origin]
		if origin.Bridge != core.Transfer.Tail.Bridge || origin.Epoch != core.RetirementEpoch || int(origin.View) >= len(e.Views) || int(origin.Projection) >= len(e.Projections) || !storageTransferOwningAbortCore(core, e.Board.Source, e.Projections[origin.Projection].Catalog, row, storageEnrollmentPeakHostState(e.Views[origin.View])) || !storageTransferTailBound(core, e) {
			return false
		}
	}
	return matches == 1
}
func (op *chatTransferOperation) completeOriginalTransferOwningAbort(ctx context.Context) error {
	if op.record.Phase != storageTransferRetired || op.record.PointerState != 0 || !op.record.Cleanup.complete() || !op.record.TargetBefore.valid(1) || op.appliedProof == nil || !storageTransferApplied(op.record, op.catalog, *op.appliedProof) || op.tailConsumed || op.tailCompleted {
		return storagebudget.ErrAuthority
	}
	if err := op.refreshOriginalTransferQ(ctx); err != nil {
		return err
	}
	catalog, err := op.originalTransferCatalogBase(ctx, op.record.Tail.Bridge)
	if err != nil {
		return err
	}
	if err = storageTransferSealCatalog(&catalog); err != nil {
		return err
	}
	var row storageOwnerRoot
	found := false
	for _, candidate := range catalog.Roots {
		if candidate.Role == 4 && catalog.Objects[candidate.DirectoryObject].Persistent == op.record.TargetBefore {
			if found {
				return storagebudget.ErrAuthority
			}
			row = candidate
			found = true
		}
	}
	if !found {
		return storagebudget.ErrAuthority
	}
	scope, err := op.originalTransferOwningScope(ctx, catalog, row, 6)
	if err != nil {
		return err
	}
	if !storageTransferNoDefinitionChange(op.catalog, catalog) {
		return storagebudget.ErrAuthority
	}
	if err = op.preparation.epoch.enrollHistoryAncestorCatalog(ctx, catalog, scope); err != nil {
		return err
	}
	p := op.preparation.epoch
	if !p.reconciled || p.completedEvidence == nil || !storageTransferOwningAbortProof(*p.completedEvidence) {
		return errStorageEnrollmentUnresolved
	}
	op.tailCompleted = true
	op.preparation.completed = true
	return nil
}

// Resolve the descendant's original attribution from the frozen destination
// namespace, not from every planned component sharing its native anchor.
// This only selects an already-held binding; the pass still checks its live
// native facts and each independently returned descendant receipt.
func (op *chatTransferOperation) originalTransferDescendantBinding() (*storageRootBinding, error) {
	if op == nil || op.self != op || op.preparation == nil || op.preparation.epoch == nil || op.path == nil || op.path.native != op.record.Parent || op.path.missing < 0 || op.path.missing >= len(op.path.names) || op.record.TargetBefore != (storageOwnerPersistentObject{}) {
		return nil, storagebudget.ErrAuthority
	}
	if _, err := storageTransferReferences(op.record, op.catalog); err != nil {
		return nil, err
	}
	// Distinct namespace views can share the anchor; two owning rows cannot.
	owning := false
	for _, row := range op.catalog.Roots {
		if !storageTransferCatalogView(row) && op.catalog.Objects[row.DirectoryObject].Persistent == op.record.Parent {
			if owning {
				return nil, storagebudget.ErrAuthority
			}
			owning = true
		}
	}
	var binding *storageRootBinding
	for _, group := range op.record.Groups {
		if group.Native != op.record.Parent {
			continue
		}
		row, ok := storageTransferCatalogRoot(op.catalog, group.Component.Root)
		if !ok || !storageTransferCatalogView(row) || row.Source.ControlSelection != 0 {
			continue
		}
		scope, err := storageEnrollmentPeakDecodeSource(row.Source.Relative)
		if err != nil {
			return nil, err
		}
		d := scope.Default
		next, err := storageHistoryNextComponent(string(d.SelectedPath), op.path.path)
		if err != nil || d.Selected != op.record.Parent || d.Settings != op.record.Settings || d.SourceNative != op.record.Source || next != string(d.Next) || next != op.path.names[op.path.missing] {
			continue
		}
		for _, root := range op.preparation.epoch.originalRoots {
			if root.row.ID != row.Definition.ID {
				continue
			}
			if root.transferInactive || !storageTransferNamespaceBinding(root) || storageRegisteredRoot(root.row) != row.Definition || root.source.persistent != op.record.Parent || !reflect.DeepEqual(root.references, row.References) || !reflect.DeepEqual(*root.source.scope, scope) || binding != nil && binding != root {
				return nil, storagebudget.ErrAuthority
			}
			binding = root
		}
	}
	if binding == nil {
		return nil, storagebudget.ErrAuthority
	}
	return binding, nil
}

// One optional fresh pass is frozen for each data/serving ordinal. Consuming
// that slot never grants capacity or permits a retry; the original one-ticket
// promise remains held. Every pass uses the same original roots and at most32
// exact previously returned descendant parents, under the original lifetime.
func (op *chatTransferOperation) refreshOriginalTransferCapacity(ctx context.Context, kind byte, index int) error {
	if op == nil || op.self != op || op.completing || op.next >= len(op.board) || len(op.capacityChecks) != len(op.board) || op.board[op.next] != (storageTransferOrdinal{kind: kind, index: index}) || op.capacityChecks[op.next] {
		return storagebudget.ErrAuthority
	}
	op.capacityChecks[op.next] = true
	age := time.Since(op.capacityAt)
	if !op.capacityAt.IsZero() && age >= 0 && age <= time.Duration(op.catalog.Policy.ObservationMaxAgeNanos) {
		return op.currentBeforeEffect(ctx)
	}
	parents := []storageTransferObservedParent{}
	if op.path != nil && len(op.created) > 0 && (kind == storageTransferDirectoryOrdinal || kind == storageTransferOutputOrdinal || kind == storageTransferMarkerOrdinal || kind == storageTransferPointerOrdinal) {
		binding, err := op.originalTransferDescendantBinding()
		if err != nil {
			return err
		}
		first := max(0, len(op.created)-storageCapacityPassParents)
		for window := 0; window < (len(op.created)+31)/32; window++ {
			a, b, err := storageTransferParentWindow(len(op.created), window)
			if err != nil {
				return err
			}
			for i := a; i < b; i++ {
				if err = op.created[i].current(); err != nil {
					return err
				}
			}
		}
		for i := first; i < len(op.created); i++ {
			receipt := op.returned.Descendants[i]
			native := op.record.Basis
			native.Object, native.Incarnation = receipt.Object, receipt.Incarnation
			if receipt.State != 2 {
				return storagebudget.ErrAuthority
			}
			parents = append(parents, storageTransferObservedParent{binding: binding, root: op.created[i].root, native: native, facts: op.path.facts, descendant: i})
		}
	}
	pass, err := op.beginOriginalTransferCapacityPass(ctx, parents, nil)
	if err != nil {
		return err
	}
	observations, err := pass.observations()
	at := pass.completed
	if testOriginalChatTransferNativeCut != nil {
		err = errors.Join(err, testOriginalChatTransferNativeCut(storageTransferCapacityObservedCut))
	}
	err = errors.Join(err, pass.Close())
	if err != nil {
		return err
	}
	// The original complete peak is already promised. Refreshing confirms the
	// frozen native membership/resources and configured headroom, never Reserve.
	for _, observation := range observations {
		found := false
		for _, constraint := range op.catalog.Constraints {
			if constraint.Definition.ID != observation.Constraint {
				continue
			}
			found = true
			d := constraint.Definition
			if d.Generation != observation.Generation || d.Resources != observation.KnownResources || d.Resources&storagebudget.CapacityBytes != 0 && observation.Available.Bytes < d.Headroom.Bytes || d.Resources&storagebudget.CapacityInodes != 0 && observation.Available.Inodes < d.Headroom.Inodes {
				return storagebudget.ErrHeadroom
			}
		}
		if !found {
			return storagebudget.ErrAuthority
		}
	}
	op.capacityAt = at
	return op.currentBeforeEffect(ctx)
}
