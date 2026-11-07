package desk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

const (
	storageIntentPage    = 16384
	storageIntentBody    = 15663
	storageIntentEncoded = 128 + storageIntentBody
	storageIntentVersion = 2
)

type storageIntentPhase uint8

const (
	storageIntentEmpty storageIntentPhase = iota
	storageIntentPrepared
	storageIntentReserved
	storageIntentStaging
	storageIntentPublishing
	storageIntentSettling
	storageIntentSettled
	storageIntentRetired
	storageIntentRejected
)

type storageIntentOutcome uint8

const (
	storageIntentUnobserved storageIntentOutcome = iota
	storageIntentPublished
	storageIntentAborted
	storageIntentNotAdmitted
)

type storageIntentRejection uint8

const (
	storageIntentNoRejection storageIntentRejection = iota
	storageIntentQuota
	storageIntentHeadroom
	storageIntentBusy
)

// This closed metadata format never stores payload bytes or payload-derived
// digests. OperationID is a random immutable nonce, not a restored capability.
// Sixteen full v2 components fit without narrowing any B1 string/vector bound.
type storageIntentRecord struct {
	Slot                                            uint16
	Generation                                      uint64
	Phase                                           storageIntentPhase
	OperationID                                     storagebudget.ID
	PreviousPageDigest, CASBinding                  storagebudget.Digest
	AdmissionEpoch, PolicyRevision, RetirementEpoch uint64
	OldExists                                       bool
	OldIdentity                                     storageDirectoryIdentity
	OldLength, OldLinkCount                         uint64
	StageExists                                     bool
	StageIdentity                                   storageDirectoryIdentity
	NewLength                                       uint64
	Token, PreviousToken                            storagebudget.Token
	Outcome                                         storageIntentOutcome
	Rejection                                       storageIntentRejection
	Evidence                                        storagebudget.Digest
	Components                                      []storagebudget.Component
	Fanout                                          *storageIntentFanout   `json:",omitempty"`
	Transfer                                        *storageIntentTransfer `json:",omitempty"`
	RootIdentities, FilesystemIdentities            []storagebudget.Digest
	Changes                                         []storagebudget.Change
	Target, Stage, Workflow, Operation              string
	Locators                                        []string
}

func (r storageIntentRecord) encodedLength() int {
	if r.Fanout != nil {
		return storageIntentEncoded + 65
	}
	return storageIntentEncoded
}

func (r storageIntentRecord) reusable() bool {
	return r.Phase == storageIntentEmpty || r.Phase == storageIntentRetired || r.Phase == storageIntentRejected
}
func storageIntentText(s string, max int, empty bool) bool {
	return utf8.ValidString(s) && len(s) <= max && (empty || s != "") && !strings.ContainsAny(s, "\x00\r\n\ufffd")
}
func storageIntentRelative(s string) bool {
	if !storageIntentText(s, 1024, false) || strings.ContainsAny(s, "\\") || strings.HasPrefix(s, "/") {
		return false
	}
	for _, p := range strings.Split(s, "/") {
		if p == "" || p == "." || p == ".." {
			return false
		}
	}
	return true
}
func storageIntentToken(t storagebudget.Token) bool {
	return t.Slot < storagebudget.Slots && t.ID != (storagebudget.ID{}) && t.Sequence > 0 && t.Generation > 0
}
func storageIntentNextToken(a, b storagebudget.Token) bool {
	return storageIntentToken(a) && storageIntentToken(b) && a.Slot == b.Slot && a.ID == b.ID && a.Sequence == b.Sequence && a.Generation != ^uint64(0) && b.Generation == a.Generation+1
}
func storageIntentVector(v storagebudget.Vector) bool {
	return v.Bytes <= 1<<60 && v.Files <= 1<<40 && v.Directories <= 1<<40
}
func (r storageIntentRecord) validate() error {
	if r.Transfer != nil {
		return storageTransferIntentValid(r)
	}
	if r.Fanout != nil {
		if r.Fanout.Binding == (storagebudget.Digest{}) || len(r.Components) < 1 || len(r.Components) > 2 {
			return storagebudget.ErrUnknown
		}
		initial := r.Fanout.Initial
		if r.Phase == storageIntentPrepared || r.Phase == storageIntentRejected {
			if initial != (storagebudget.Token{}) {
				return storagebudget.ErrUnknown
			}
		} else if !storageIntentToken(initial) || initial.Slot != r.Token.Slot || initial.ID != r.Token.ID || initial.Sequence != r.Token.Sequence || initial.Generation > r.Token.Generation || r.Phase == storageIntentReserved && initial != r.Token {
			return storagebudget.ErrUnknown
		}
	}

	if r.Phase < storageIntentPrepared || r.Phase > storageIntentRejected || r.Slot >= storagebudget.Slots || r.Generation == 0 || r.OperationID == (storagebudget.ID{}) || r.CASBinding == (storagebudget.Digest{}) || r.AdmissionEpoch == 0 || r.NewLength > 1<<60 || !storageIntentRelative(r.Target) || !storageIntentRelative(r.Stage) || r.Target == r.Stage || !storageIntentText(r.Workflow, 128, false) || !storageIntentText(r.Operation, 128, false) {
		return storagebudget.ErrUnknown
	}
	if r.Generation == 1 && (r.Phase != storageIntentPrepared || r.PreviousPageDigest != (storagebudget.Digest{})) || r.Generation > 1 && r.PreviousPageDigest == (storagebudget.Digest{}) {
		return storagebudget.ErrUnknown
	}
	n := len(r.Components)
	if n < 1 || n > storagebudget.MaxComponents || len(r.RootIdentities) != n || len(r.FilesystemIdentities) != n || len(r.Locators) != n || len(r.Changes) != n {
		return storagebudget.ErrUnknown
	}
	if r.OldExists {
		if r.OldIdentity == (storageDirectoryIdentity{}) || r.OldLinkCount != 1 || r.OldLength > 1<<60 {
			return storagebudget.ErrUnknown
		}
	} else if r.OldIdentity != (storageDirectoryIdentity{}) || r.OldLength != 0 || r.OldLinkCount != 0 {
		return storagebudget.ErrUnknown
	}
	if r.StageExists {
		if r.StageIdentity == (storageDirectoryIdentity{}) {
			return storagebudget.ErrUnknown
		}
	} else if r.StageIdentity != (storageDirectoryIdentity{}) {
		return storagebudget.ErrUnknown
	}
	for i, c := range r.Components {
		if c.Filesystem == (storagebudget.ID{}) || c.Root == (storagebudget.ID{}) || c.Owner == (storagebudget.Digest{}) || c.FilesystemGeneration == 0 || c.RootGeneration == 0 || c.Category >= storagebudget.Categories || r.RootIdentities[i] == (storagebudget.Digest{}) || r.FilesystemIdentities[i] == (storagebudget.Digest{}) || !storageIntentText(r.Locators[i], 256, false) || !storageIntentVector(c.Physical) || !storageIntentVector(c.Attribution) || !storageIntentVector(c.SettlementPhysical) || !storageIntentVector(c.SettlementAttribution) {
			return storagebudget.ErrUnknown
		}
		if c.CapacityCount < 1 || c.CapacityCount > storagebudget.MaxCapacityRefs || c.CapacityBinding == (storagebudget.Digest{}) {
			return storagebudget.ErrUnknown
		}
		for j, p := range c.Capacity {
			if j >= int(c.CapacityCount) {
				if p != (storagebudget.CapacityPromise{}) {
					return storagebudget.ErrUnknown
				}
				continue
			}
			if p.Constraint == (storagebudget.ID{}) || p.Generation == 0 || p.Peak.Bytes > 1<<60 || p.Peak.Inodes > 1<<40 || j > 0 && bytes.Compare(c.Capacity[j-1].Constraint[:], p.Constraint[:]) >= 0 {
				return storagebudget.ErrUnknown
			}
		}
		if c.Workspace == (storagebudget.ID{}) {
			if c.WorkspaceGeneration != 0 || r.Fanout == nil && (c.Attribution != (storagebudget.Vector{}) || c.SettlementAttribution != (storagebudget.Vector{})) {
				return storagebudget.ErrUnknown
			}
		} else if c.WorkspaceGeneration == 0 {
			return storagebudget.ErrUnknown
		}
		p, e := storageVectorAdd(c.Physical, c.SettlementPhysical)
		if e != nil {
			return storagebudget.ErrUnknown
		}
		if p == (storagebudget.Vector{}) {
			for _, cap := range c.Capacity {
				if cap.Peak != (storagebudget.CapacityVector{}) {
					return storagebudget.ErrUnknown
				}
			}
		}
		w, e := storageVectorAdd(c.Attribution, c.SettlementAttribution)
		if e != nil || p == (storagebudget.Vector{}) && w == (storagebudget.Vector{}) {
			return storagebudget.ErrUnknown
		}
		for j := 0; j < i; j++ {
			prior := r.Components[j]
			if prior.Root == c.Root && prior.Workspace == c.Workspace && prior.Category == c.Category {
				return storagebudget.ErrUnknown
			}
		}
		ch := r.Changes[i]
		if !storageIntentVector(ch.PhysicalAdd) || !storageIntentVector(ch.PhysicalRemove) || !storageIntentVector(ch.AttributionAdd) || !storageIntentVector(ch.AttributionRemove) {
			return storagebudget.ErrUnknown
		}
		if !storageVectorLE(ch.PhysicalAdd, p) || !storageVectorLE(ch.AttributionAdd, w) {
			return storagebudget.ErrUnknown
		}
		if r.Phase < storageIntentSettling || r.Phase == storageIntentRejected {
			if ch != (storagebudget.Change{}) {
				return storagebudget.ErrUnknown
			}
		}
	}
	if r.Phase < storageIntentPublishing && r.StageExists {
		return storagebudget.ErrUnknown
	}
	if r.Phase == storageIntentPrepared || r.Phase == storageIntentRejected {
		if r.Token != (storagebudget.Token{}) || r.PreviousToken != (storagebudget.Token{}) || r.PolicyRevision != 0 || r.StageExists || r.RetirementEpoch != 0 || r.Evidence != (storagebudget.Digest{}) {
			return storagebudget.ErrUnknown
		}
	} else {
		if !storageIntentToken(r.Token) || r.PolicyRevision == 0 {
			return storagebudget.ErrUnknown
		}
		if r.Phase == storageIntentReserved {
			if r.PreviousToken != (storagebudget.Token{}) || r.StageExists || r.Evidence != (storagebudget.Digest{}) {
				return storagebudget.ErrUnknown
			}
		} else if !storageIntentNextToken(r.PreviousToken, r.Token) {
			return storagebudget.ErrUnknown
		}
	}
	if r.Phase == storageIntentRejected {
		if r.Outcome != storageIntentNotAdmitted || r.Rejection < storageIntentQuota || r.Rejection > storageIntentBusy {
			return storagebudget.ErrUnknown
		}
	} else if r.Rejection != storageIntentNoRejection {
		return storagebudget.ErrUnknown
	}
	if r.Phase >= storageIntentSettling && r.Phase <= storageIntentRetired {
		if (r.Outcome != storageIntentPublished && r.Outcome != storageIntentAborted) || r.Evidence == (storagebudget.Digest{}) {
			return storagebudget.ErrUnknown
		}
	} else if r.Phase != storageIntentRejected && r.Outcome != storageIntentUnobserved {
		return storagebudget.ErrUnknown
	}
	if r.Phase >= storageIntentPublishing && r.Phase <= storageIntentRetired && r.Outcome != storageIntentAborted && !r.StageExists {
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

// The codec advances through fixed fields, never variable-size allocations.
type storageIntentCodec struct {
	b   []byte
	at  int
	bad bool
}

func (c *storageIntentCodec) raw(b []byte)  { copy(c.b[c.at:c.at+len(b)], b); c.at += len(b) }
func (c *storageIntentCodec) take(b []byte) { copy(b, c.b[c.at:c.at+len(b)]); c.at += len(b) }
func (c *storageIntentCodec) put(v uint64, n int) {
	for i := 0; i < n; i++ {
		c.b[c.at+i] = byte(v >> uint(i*8))
	}
	c.at += n
}
func (c *storageIntentCodec) get(n int) (v uint64) {
	for i := 0; i < n; i++ {
		v |= uint64(c.b[c.at+i]) << uint(i*8)
	}
	c.at += n
	return
}
func (c *storageIntentCodec) skip(n int, read bool) {
	if read && !storageAllZero(c.b[c.at:c.at+n]) {
		c.bad = true
	}
	c.at += n
}
func (c *storageIntentCodec) text(s *string, n int, read bool) {
	if read {
		l := int(c.get(2))
		if l > n {
			c.bad = true
			l = 0
		}
		*s = string(c.b[c.at : c.at+l])
		if !storageAllZero(c.b[c.at+l : c.at+n]) {
			c.bad = true
		}
		c.at += n
	} else {
		c.put(uint64(len(*s)), 2)
		copy(c.b[c.at:c.at+n], *s)
		c.at += n
	}
}
func (c *storageIntentCodec) number(v *uint64, read bool) {
	if read {
		*v = c.get(8)
	} else {
		c.put(*v, 8)
	}
}
func (c *storageIntentCodec) boolean(v *bool, read bool) {
	if read {
		n := c.get(1)
		if n > 1 {
			c.bad = true
		}
		*v = n == 1
	} else {
		n := uint64(0)
		if *v {
			n = 1
		}
		c.put(n, 1)
	}
}
func (c *storageIntentCodec) identity(v *storageDirectoryIdentity, read bool) {
	c.number(&v.Device, read)
	c.number(&v.Inode, read)
}
func (c *storageIntentCodec) token(v *storagebudget.Token, read bool) {
	if read {
		v.Slot = uint8(c.get(1))
		c.take(v.ID[:])
	} else {
		c.put(uint64(v.Slot), 1)
		c.raw(v.ID[:])
	}
	c.number(&v.Sequence, read)
	c.number(&v.Generation, read)
}
func (c *storageIntentCodec) vector(v *storagebudget.Vector, read bool) {
	c.number(&v.Bytes, read)
	c.number(&v.Files, read)
	c.number(&v.Directories, read)
}
func (c *storageIntentCodec) digest(v *storagebudget.Digest, read bool) {
	if read {
		c.take(v[:])
	} else {
		c.raw(v[:])
	}
}
func (c *storageIntentCodec) id(v *storagebudget.ID, read bool) {
	if read {
		c.take(v[:])
	} else {
		c.raw(v[:])
	}
}
func (c *storageIntentCodec) component(v *storagebudget.Component, read bool) {
	c.id(&v.Filesystem, read)
	c.number(&v.FilesystemGeneration, read)
	c.id(&v.Root, read)
	c.number(&v.RootGeneration, read)
	c.digest(&v.Owner, read)
	c.id(&v.Workspace, read)
	c.number(&v.WorkspaceGeneration, read)
	if read {
		v.Category = uint8(c.get(1))
	} else {
		c.put(uint64(v.Category), 1)
	}
	c.vector(&v.Physical, read)
	c.vector(&v.Attribution, read)
	c.vector(&v.SettlementPhysical, read)
	c.vector(&v.SettlementAttribution, read)
	if read {
		v.CapacityCount = uint8(c.get(1))
	} else {
		c.put(uint64(v.CapacityCount), 1)
	}
	for i := range v.Capacity {
		p := &v.Capacity[i]
		c.id(&p.Constraint, read)
		c.number(&p.Generation, read)
		c.number(&p.Peak.Bytes, read)
		c.number(&p.Peak.Inodes, read)
	}
	c.digest(&v.CapacityBinding, read)
	c.skip(6, read)
}
func (c *storageIntentCodec) change(v *storagebudget.Change, read bool) {
	c.vector(&v.PhysicalAdd, read)
	c.vector(&v.PhysicalRemove, read)
	c.vector(&v.AttributionAdd, read)
	c.vector(&v.AttributionRemove, read)
}

// storageZeroBlock is never written. Comparing a buffer against it checks
// every byte, as a byte loop would, using the runtime's vectorized equality.
var storageZeroBlock [64 << 10]byte

func storageAllZero(b []byte) bool {
	for len(b) > 0 {
		n := min(len(b), len(storageZeroBlock))
		if !bytes.Equal(b[:n], storageZeroBlock[:n]) {
			return false
		}
		b = b[n:]
	}
	return true
}
func storageIntentBodyCodec(b []byte, r *storageIntentRecord, read bool) bool {
	c := storageIntentCodec{b: b, at: 128}
	c.id(&r.OperationID, read)
	c.digest(&r.PreviousPageDigest, read)
	c.digest(&r.CASBinding, read)
	c.number(&r.AdmissionEpoch, read)
	c.number(&r.PolicyRevision, read)
	c.number(&r.RetirementEpoch, read)
	c.boolean(&r.OldExists, read)
	c.identity(&r.OldIdentity, read)
	c.number(&r.OldLength, read)
	c.number(&r.OldLinkCount, read)
	c.boolean(&r.StageExists, read)
	c.identity(&r.StageIdentity, read)
	c.number(&r.NewLength, read)
	c.token(&r.Token, read)
	c.token(&r.PreviousToken, read)
	if read {
		r.Outcome = storageIntentOutcome(c.get(1))
		r.Rejection = storageIntentRejection(c.get(1))
	} else {
		c.put(uint64(r.Outcome), 1)
		c.put(uint64(r.Rejection), 1)
	}
	c.digest(&r.Evidence, read)
	n := len(r.Components)
	if read {
		n = int(c.get(1))
		if n < 1 || n > storagebudget.MaxComponents {
			return false
		}
		r.Components = make([]storagebudget.Component, n)
		r.RootIdentities = make([]storagebudget.Digest, n)
		r.FilesystemIdentities = make([]storagebudget.Digest, n)
		r.Changes = make([]storagebudget.Change, n)
		r.Locators = make([]string, n)
	} else {
		c.put(uint64(n), 1)
	}
	for i := 0; i < storagebudget.MaxComponents; i++ {
		if i < n {
			c.component(&r.Components[i], read)
		} else {
			c.skip(400, read)
		}
	}
	for i := 0; i < storagebudget.MaxComponents; i++ {
		if i < n {
			c.digest(&r.RootIdentities[i], read)
		} else {
			c.skip(32, read)
		}
	}
	for i := 0; i < storagebudget.MaxComponents; i++ {
		if i < n {
			c.digest(&r.FilesystemIdentities[i], read)
		} else {
			c.skip(32, read)
		}
	}
	for i := 0; i < storagebudget.MaxComponents; i++ {
		if i < n {
			c.change(&r.Changes[i], read)
		} else {
			c.skip(96, read)
		}
	}
	if r.Transfer != nil {
		if !storageTransferIntentCodec(&c, r.Transfer, read) {
			return false
		}
	} else {
		c.text(&r.Target, 1024, read)
		c.text(&r.Stage, 1024, read)
	}
	c.text(&r.Workflow, 128, read)
	c.text(&r.Operation, 128, read)
	for i := 0; i < storagebudget.MaxComponents; i++ {
		if i < n {
			c.text(&r.Locators[i], 256, read)
		} else {
			c.skip(258, read)
		}
	}
	if r.Fanout != nil {
		c.digest(&r.Fanout.Binding, read)
		c.token(&r.Fanout.Initial, read)
	}
	return !c.bad && c.at == r.encodedLength()
}
func storageEncodeIntent(r storageIntentRecord, installation storagebudget.ID, binding storagebudget.Digest) ([]byte, error) {
	if r.validate() != nil || installation == (storagebudget.ID{}) || binding == (storagebudget.Digest{}) || !storageTransferIntentBound(r, installation, binding) {
		return nil, storagebudget.ErrUnknown
	}
	b := make([]byte, storageIntentPage)
	magic, version := "DSKHINT2", uint16(storageIntentVersion)
	if r.Transfer != nil {
		magic, version = "DSKHINT9", 9
	}
	if r.Fanout != nil {
		magic, version = "DSKHINT8", 8
	}
	copy(b[:8], magic)
	binary.LittleEndian.PutUint16(b[8:10], version)
	binary.LittleEndian.PutUint16(b[10:12], r.Slot)
	b[12] = byte(r.Phase)
	binary.LittleEndian.PutUint64(b[16:24], r.Generation)
	copy(b[24:40], installation[:])
	copy(b[40:72], binding[:])
	binary.LittleEndian.PutUint32(b[72:76], uint32(r.encodedLength()-128))
	if !storageIntentBodyCodec(b, &r, false) {
		return nil, storagebudget.ErrBounds
	}
	sum := sha256.Sum256(b)
	copy(b[76:108], sum[:])
	return b, nil
}
func storageDecodeIntent(b []byte, slot int, installation storagebudget.ID, binding storagebudget.Digest) (storageIntentRecord, error) {
	var r storageIntentRecord
	if len(b) == storageIntentPage && bytes.Equal(b[:8], []byte("DSKHINT8")) && binary.LittleEndian.Uint16(b[8:10]) == 8 {
		r.Fanout = &storageIntentFanout{}
	}
	if len(b) == storageIntentPage && bytes.Equal(b[:8], []byte("DSKHINT9")) && binary.LittleEndian.Uint16(b[8:10]) == 9 {
		r.Transfer = &storageIntentTransfer{}
	}
	magic, version := "DSKHINT2", uint16(storageIntentVersion)
	if r.Transfer != nil {
		magic, version = "DSKHINT9", 9
	}
	if r.Fanout != nil {
		magic, version = "DSKHINT8", 8
	}
	if len(b) != storageIntentPage || !bytes.Equal(b[:8], []byte(magic)) || binary.LittleEndian.Uint16(b[8:10]) != version || int(binary.LittleEndian.Uint16(b[10:12])) != slot || !storageAllZero(b[13:16]) || !bytes.Equal(b[24:40], installation[:]) || !bytes.Equal(b[40:72], binding[:]) || binary.LittleEndian.Uint32(b[72:76]) != uint32(r.encodedLength()-128) || !storageAllZero(b[108:128]) || !storageAllZero(b[r.encodedLength():]) {
		return r, storagebudget.ErrUnknown
	}
	seal := append([]byte(nil), b...)
	clear(seal[76:108])
	sum := sha256.Sum256(seal)
	if !bytes.Equal(sum[:], b[76:108]) {
		return r, storagebudget.ErrUnknown
	}
	r.Slot = uint16(slot)
	r.Generation = binary.LittleEndian.Uint64(b[16:24])
	r.Phase = storageIntentPhase(b[12])
	if !storageIntentBodyCodec(b, &r, true) || r.validate() != nil || !storageTransferIntentBound(r, installation, binding) {
		return storageIntentRecord{}, storagebudget.ErrUnknown
	}
	return r, nil
}
func storageIntentImmutable(r storageIntentRecord) storageIntentRecord {
	if r.Transfer != nil {
		copy := *r.Transfer
		copy.Initial = storagebudget.Token{}
		copy.Observed = storagebudget.Digest{}
		copy.Selection = storagebudget.Digest{}
		copy.Selected = storageOwnerPersistentObject{}
		copy.Marker = storageOwnerPersistentObject{}
		copy.Location = storageOwnerPersistentObject{}
		copy.LocationPayload = storagebudget.Digest{}
		copy.PointerState = 0
		copy.Cleanup = storageTransferCleanup{}
		r.Transfer = &copy
	}
	if r.Fanout != nil {
		f := *r.Fanout
		f.Initial = storagebudget.Token{}
		r.Fanout = &f
	}
	r.Generation = 0
	r.Phase = 0
	r.PreviousPageDigest = storagebudget.Digest{}
	r.PolicyRevision = 0
	r.RetirementEpoch = 0
	r.StageExists = false
	r.StageIdentity = storageDirectoryIdentity{}
	r.Token = storagebudget.Token{}
	r.PreviousToken = storagebudget.Token{}
	r.Outcome = 0
	r.Rejection = 0
	r.Evidence = storagebudget.Digest{}
	r.Changes = nil
	return r
}
func storageIntentTransition(old, next storageIntentRecord, oldPage []byte) bool {
	if old.Transfer != nil || next.Transfer != nil {
		return storageTransferIntentTransition(old, next, oldPage)
	}
	if old.Generation == ^uint64(0) || next.Generation != old.Generation+1 || next.Slot != old.Slot || next.PreviousPageDigest != sha256.Sum256(oldPage) {
		return false
	}
	if old.reusable() {
		return next.Phase == storageIntentPrepared && next.OperationID != old.OperationID
	}
	if !reflect.DeepEqual(storageIntentImmutable(old), storageIntentImmutable(next)) {
		return false
	}
	if old.Fanout != nil && old.Phase != storageIntentPrepared && (next.Fanout == nil || old.Fanout.Initial != next.Fanout.Initial) {
		return false
	}
	if old.PolicyRevision != 0 && old.PolicyRevision != next.PolicyRevision {
		return false
	}
	switch old.Phase {
	case storageIntentPrepared:
		return next.Phase == storageIntentReserved || next.Phase == storageIntentRejected
	case storageIntentReserved:
		return next.Phase == storageIntentStaging && next.PreviousToken == old.Token && storageIntentNextToken(old.Token, next.Token)
	case storageIntentStaging:
		return next.Phase == storageIntentPublishing && next.Token == old.Token && next.PreviousToken == old.PreviousToken
	case storageIntentPublishing:
		return next.Phase == storageIntentSettling && next.Token == old.Token && next.PreviousToken == old.PreviousToken && next.StageIdentity == old.StageIdentity && next.StageExists == old.StageExists
	case storageIntentSettling:
		return next.Phase == storageIntentSettled && next.PreviousToken == old.Token && storageIntentNextToken(old.Token, next.Token) && next.Evidence == old.Evidence && next.Outcome == old.Outcome && next.StageIdentity == old.StageIdentity && reflect.DeepEqual(next.Changes, old.Changes)
	case storageIntentSettled:
		return next.Phase == storageIntentRetired && next.Token == old.Token && next.PreviousToken == old.PreviousToken && next.Evidence == old.Evidence && next.Outcome == old.Outcome && next.StageIdentity == old.StageIdentity && reflect.DeepEqual(next.Changes, old.Changes)
	}
	return false
}

type storageIntentSlot struct {
	record storageIntentRecord
	page   []byte
	bank   int
}
type storageIntentTable struct {
	owner               *storageBudgetOwner
	identity            [storagebudget.Slots]*storageIdentityIntent
	slots               [storagebudget.Slots]storageIntentSlot
	creation            [storagebudget.Slots]*storageCreationIntent
	creationPredecessor [storagebudget.Slots]*storageCreationIntent
}

func storageSelectIntent(a, b []byte, slot int, installation storagebudget.ID, binding storagebudget.Digest) (storageIntentSlot, error) {
	if len(a) != storageIntentPage || len(b) != storageIntentPage {
		return storageIntentSlot{}, storagebudget.ErrUnknown
	}
	// A recognized prior format is never an absent/torn candidate, including
	// an inactive bank. Preserve both pages; no automatic migration or reset.
	if bytes.Equal(a[:8], []byte("DSKHINT1")) || bytes.Equal(b[:8], []byte("DSKHINT1")) {
		return storageIntentSlot{}, storagebudget.ErrUnsupported
	}
	az, bz := storageAllZero(a), storageAllZero(b)
	empty := storageIntentSlot{record: storageIntentRecord{Slot: uint16(slot)}, bank: 1, page: make([]byte, storageIntentPage)}
	if az && bz {
		return empty, nil
	}
	var ar, br storageIntentRecord
	var e error
	if !az {
		ar, e = storageDecodeIntent(a, slot, installation, binding)
		if e != nil {
			return empty, e
		}
	}
	if !bz {
		br, e = storageDecodeIntent(b, slot, installation, binding)
		if e != nil {
			return empty, e
		}
	}
	if az || bz {
		r, page, bank := ar, a, 0
		if az {
			r, page, bank = br, b, 1
		}
		if r.Generation != 1 || r.Phase != storageIntentPrepared || r.PreviousPageDigest != (storagebudget.Digest{}) {
			return empty, storagebudget.ErrUnknown
		}
		return storageIntentSlot{r, append([]byte(nil), page...), bank}, nil
	}
	if ar.Generation == br.Generation {
		if !bytes.Equal(a, b) {
			return empty, storagebudget.ErrUnknown
		}
		return storageIntentSlot{ar, append([]byte(nil), a...), 0}, nil
	}
	old, next, op, np, bank := ar, br, a, b, 1
	if ar.Generation > br.Generation {
		old, next, op, np, bank = br, ar, b, a, 0
	}
	if !storageIntentTransition(old, next, op) {
		return empty, storagebudget.ErrUnknown
	}
	return storageIntentSlot{next, append([]byte(nil), np...), bank}, nil
}

// The independent bootstrap description is a short, try-only control gate.
// Original open/bootstrap continue holding this same inode through completion.
// This transaction never acquires storage-use/domain locks, scans, or effects;
// callers already hold those lifetimes. Q is a leaf beneath this control gate.
// The 250 ms child context bounds the original coordination/new-Q-work window.
// Ordinary admission may record only a positively completed Reserve/Advance
// under a separate cancellable 250 ms metadata budget while retaining these same
// locks. It stops further Q work/permit issuance if coordination expired. Total
// residency may include that confirmation window; fixed-size Sync/Close calls
// cannot be preempted by a context deadline.
// The final move reconciliation runs under the original startup mutation and
// home EX, after the registered enrollment epoch has drained. Decode once with
// that original caller context; the short control window still bounds Q work.
// This value never leaves this call or becomes durable/reusable authority.
func (o *storageBudgetOwner) withStartupMoveControl(ctx context.Context, run func(context.Context, *storageIntentTable, *storagePolicyOriginal) error) error {
	if err := o.startupMoveControlCurrent(ctx); err != nil {
		return err
	}
	original, err := o.readOriginalPolicy(ctx)
	if err != nil {
		return err
	}
	if original.RecoveryOnly {
		return errStorageEnrollmentUnresolved
	}
	return o.withControlOriginal(ctx, &original, nil, func(ctx context.Context, table *storageIntentTable) error {
		return run(ctx, table, &original)
	})
}

// The registered move retains its original use EX, project/work/signing and
// domain descriptions until epoch finish. No finished epoch or startup ticket
// may substitute for this current original exclusion, including late replay.
func (o *storageBudgetOwner) withRegisteredMoveControl(ctx context.Context, move *storageMoveOperation, run func(context.Context, *storageIntentTable, *storagePolicyOriginal) error) error {
	if move == nil {
		return storagebudget.ErrAuthority
	}
	if err := o.originalControlExclusionCurrent(ctx, move); err != nil {
		return err
	}
	original, err := o.readOriginalPolicy(ctx)
	if err != nil {
		return err
	}
	if original.RecoveryOnly {
		return errStorageEnrollmentUnresolved
	}
	return o.withControlOriginal(ctx, &original, move, func(ctx context.Context, table *storageIntentTable) error {
		return run(ctx, table, &original)
	})
}

func (o *storageBudgetOwner) originalControlExclusionCurrent(ctx context.Context, move *storageMoveOperation) error {
	if move == nil {
		return o.startupMoveControlCurrent(ctx)
	}
	p := move.phase
	if !o.valid() || move.self != move || p == nil || p.self != p || p.move != move || p.c == nil || p.c.owner != o || p.c.ref == nil || p.c.ref.cell != o.ref.cell || move.lease == nil || move.lease.move != move || p.project != move.lease.file || p.use == nil || p.use.cell != o.ref.cell {
		return storagebudget.ErrAuthority
	}
	return p.held(ctx)
}

func (o *storageBudgetOwner) startupMoveControlCurrent(ctx context.Context) error {
	if !o.valid() || o.ref.cell.startup == nil {
		return storagebudget.ErrAuthority
	}
	c := o.ref.cell.startup
	c.mu.Lock()
	a, owner := c.mutation, c.owner
	c.mu.Unlock()
	if owner != o || a == nil || a.owner != o || a.kind != storageMutationStartup {
		return storagebudget.ErrAuthority
	}
	if err := a.current(ctx, true); err != nil {
		return err
	}
	return c.current(ctx, true)
}

// Only exact actual images revalidate a decoded snapshot. Both banks matter,
// including the unselected original peer. Hashes/sequence alone cannot do this.
func storageControlOriginalImages(original *storagePolicyOriginal, actual [2][]byte) bool {
	return original != nil && !original.RecoveryOnly && original.Selected <= 1 &&
		len(original.Actual[0]) == storageOwnerCatalogBytes && len(original.Actual[1]) == storageOwnerCatalogBytes &&
		len(actual[0]) == storageOwnerCatalogBytes && len(actual[1]) == storageOwnerCatalogBytes &&
		bytes.Equal(original.Actual[0], actual[0]) && bytes.Equal(original.Actual[1], actual[1])
}

// Called with original bootstrap EX held, before decoding J and again after a
// successful read-only callback. No Q call occurs under an owner/admission lock.
func (o *storageBudgetOwner) controlOriginalCurrent(ctx context.Context, original *storagePolicyOriginal, move *storageMoveOperation) error {
	if err := o.originalControlExclusionCurrent(ctx, move); err != nil {
		return err
	}
	var actual [2][]byte
	for i, name := range []string{"storage-owner.a", "storage-owner.b"} {
		file, err := storageOpenOriginalControl(o.ref.cell, name)
		if err != nil {
			return err
		}
		actual[i] = make([]byte, storageOwnerCatalogBytes)
		n, readErr := file.file.ReadAt(actual[i], 0)
		err = errors.Join(readErr, file.current(), file.file.Close())
		if err != nil || n != len(actual[i]) {
			return errors.Join(errStorageCustody, err)
		}
	}
	if !storageControlOriginalImages(original, actual) {
		return errStorageCustody
	}
	return o.originalControlExclusionCurrent(ctx, move)
}

func (o *storageBudgetOwner) withControl(ctx context.Context, run func(context.Context, *storageIntentTable) error) error {
	return o.withControlOriginal(ctx, nil, nil, run)
}

func (o *storageBudgetOwner) withControlOriginal(ctx context.Context, original *storagePolicyOriginal, move *storageMoveOperation, run func(context.Context, *storageIntentTable) error) (err error) {
	if !o.valid() || move != nil && original == nil {
		return storagebudget.ErrAuthority
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()
	if !o.controlMu.TryLock() {
		return storagebudget.ErrBusy
	}
	defer o.controlMu.Unlock()
	gate, e := storageOpenOriginalControl(o.ref.cell, storageBootstrapName)
	if e != nil {
		return e
	}
	defer func() { err = errors.Join(err, gate.file.Close()) }()
	if e = lockPrivateData(gate.file, true); e != nil {
		return storagebudget.ErrBusy
	}
	if e = gate.current(); e != nil {
		return e
	}
	if original != nil {
		if e = o.controlOriginalCurrent(ctx, original, move); e != nil {
			return e
		}
	}
	if e = o.withOriginalIntentTableOriginal(ctx, original, run); e != nil {
		return e
	}
	if original != nil {
		return o.controlOriginalCurrent(ctx, original, move)
	}
	return nil
}

// The caller owns the actual original bootstrap EX for this entire invocation.
// This preserves the exact decoder/receipt checks for both existing withControl
// and the registered policy epoch; it neither acquires authority nor locks Q.
func (o *storageBudgetOwner) withOriginalIntentTable(ctx context.Context, run func(context.Context, *storageIntentTable) error) error {
	return o.withOriginalIntentTableOriginal(ctx, nil, run)
}

func (o *storageBudgetOwner) withOriginalIntentTableOriginal(ctx context.Context, original *storagePolicyOriginal, run func(context.Context, *storageIntentTable) error) error {
	file, e := storageOpenOriginalControl(o.ref.cell, storageIntentName)
	if e != nil {
		return e
	}
	raw := make([]byte, storageIntentBytes)
	n, re := file.file.ReadAt(raw, 0)
	ce := file.file.Close()
	if re != nil || n != len(raw) || ce != nil {
		return errors.Join(storagebudget.ErrUnknown, re, ce)
	}
	// Inspect all fixed headers before parsing any selected record: a corrupt
	// earlier v2 slot cannot hide a recognized prior format in an inactive bank.
	for start := 0; start < len(raw); start += storageIntentPage {
		if bytes.Equal(raw[start:start+8], []byte("DSKHINT1")) {
			return storagebudget.ErrUnsupported
		}
	}
	table := &storageIntentTable{owner: o}
	var creationStatus *storagebudget.HostState
	for i := range table.slots {
		start := i * 2 * storageIntentPage
		a, b := raw[start:start+storageIntentPage], raw[start+storageIntentPage:start+2*storageIntentPage]
		if !storageCreationEstimatePage(a) && !storageCreationEstimatePage(b) && string(a[:8]) != "DSKHINT4" && string(b[:8]) != "DSKHINT4" && string(a[:8]) != "DSKHINT3" && string(b[:8]) != "DSKHINT3" && string(a[:8]) != "DSKHINT5" && string(b[:8]) != "DSKHINT5" {
			selected, e := storageSelectIntent(a, b, i, o.ref.cell.record.Installation, o.ref.cell.binding)
			if e != nil {
				return e
			}
			table.slots[i] = selected
			continue
		}
		selected, e := storageSelectCreationHost(a, b, i, o.ref.cell.record.Installation, o.ref.cell.binding)
		if e != nil {
			return e
		}
		if creationStatus == nil {
			status, e := o.ref.cell.q.InspectHostState(ctx)
			if e != nil {
				return e
			}
			creationStatus = &status
		}
		status := *creationStatus
		if selected.Identity != nil || selected.PredecessorIdentity != nil {
			var lineage storageCompositionLineage
			var e error
			if original == nil {
				lineage, e = o.identityLineage(ctx)
			} else {
				lineage, e = original.Lineage, ctx.Err()
			}
			if e != nil {
				return e
			}
			if selected.Identity != nil {
				if e = o.identityRecordMappings(ctx, selected.Identity.Core, selected.Identity.Core.Components[0].Root); e != nil {
					return e
				}
				if !o.identityReceiptCurrentOriginal(ctx, *selected.Identity, selected.Slot.page, lineage, status, original) {
					return errStorageEnrollmentUnresolved
				}
			}
			if selected.PredecessorIdentity != nil {
				if e = o.identityRecordMappings(ctx, selected.PredecessorIdentity.Core, selected.PredecessorIdentity.Core.Components[0].Root); e != nil {
					return e
				}
				prior, e := storageEncodeIdentityIntent(*selected.PredecessorIdentity, o.ref.cell.record.Installation, o.ref.cell.binding)
				if e != nil || !o.identityReceiptCurrentOriginal(ctx, *selected.PredecessorIdentity, prior, lineage, status, original) {
					return errors.Join(errStorageEnrollmentUnresolved, e)
				}
			}
		}
		if selected.Creation != nil && !o.creationTailCurrentOriginal(ctx, *selected.Creation, selected.Slot.page, status, original) {
			return errStorageEnrollmentUnresolved
		}
		if selected.PredecessorCreation != nil {
			previous, e := storageEncodeCreationIntent(*selected.PredecessorCreation, o.ref.cell.record.Installation, o.ref.cell.binding)
			if e != nil || !o.creationTailCurrentOriginal(ctx, *selected.PredecessorCreation, previous, status, original) {
				return errors.Join(errStorageEnrollmentUnresolved, e)
			}
		}
		table.slots[i] = selected.Slot
		table.identity[i] = selected.Identity
		table.creation[i] = selected.Creation
		table.creationPredecessor[i] = selected.PredecessorCreation
	}
	if e = o.fail("intent", "loaded"); e != nil {
		return e
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	return run(ctx, table)
}
func (t *storageIntentTable) save(ctx context.Context, r storageIntentRecord) error {
	return t.saveWithOriginalTransfer(ctx, r, nil)
}

// Every entry retains a concrete authority check. The transfer alternative
// accepts only its exact original projection and never widens requireMutation.
func (t *storageIntentTable) saveWithOriginalTransfer(ctx context.Context, r storageIntentRecord, op *chatTransferOperation) (err error) {
	o := t.owner
	if op == nil {
		if e := o.requireMutation(ctx); e != nil {
			return e
		}
	} else if e := o.requireOriginalTransferProjection(ctx, op, r); e != nil {
		return e
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	if int(r.Slot) >= len(t.slots) {
		return storagebudget.ErrBounds
	}
	old := t.slots[r.Slot]
	if old.record.Phase == storageIntentEmpty {
		if r.Generation != 1 || r.Phase != storageIntentPrepared || r.PreviousPageDigest != (storagebudget.Digest{}) {
			return storagebudget.ErrStale
		}
	} else if t.creation[r.Slot] != nil || t.identity[r.Slot] != nil {
		if !old.record.reusable() || r.Phase != storageIntentPrepared || old.record.Generation == ^uint64(0) || r.Generation != old.record.Generation+1 || r.OperationID == old.record.OperationID || r.PreviousPageDigest != sha256.Sum256(old.page) {
			return storagebudget.ErrStale
		}
	} else if !storageIntentTransition(old.record, r, old.page) {
		return storagebudget.ErrStale
	}
	raw, e := storageEncodeIntent(r, o.ref.cell.record.Installation, o.ref.cell.binding)
	if e != nil {
		return e
	}
	file, e := storageOpenOriginalControl(o.ref.cell, storageIntentName)
	if e != nil {
		return e
	}
	closed := false
	defer func() {
		if !closed {
			err = errors.Join(err, file.file.Close())
		}
	}()
	offset := int64((int(r.Slot)*2 + 1 - old.bank) * storageIntentPage)
	if e = o.fail("intent", "before-write"); e != nil {
		return e
	}
	write := raw
	if o.fault != nil && o.fault.Name == "intent" && o.fault.Phase == "short-write" {
		write = raw[:len(raw)/2]
		o.fault = nil
	}
	n, e := file.file.WriteAt(write, offset)
	if e != nil || n != len(raw) {
		return errors.Join(storagebudget.ErrUnknown, e, io.ErrShortWrite)
	}
	if e = o.fail("intent", "after-write"); e != nil {
		return e
	}
	if r.Transfer != nil {
		e = storageEnrollmentNativeSync(ctx, file.file)
	} else {
		e = file.file.Sync()
	}
	if e != nil {
		return e
	}
	if e = o.fail("intent", "after-sync"); e != nil {
		return e
	}
	ce := file.file.Close()
	closed = true
	if e = errors.Join(ce, o.fail("intent", "after-close")); e != nil {
		return e
	}
	read, e := storageOpenOriginalControl(o.ref.cell, storageIntentName)
	if e != nil {
		return e
	}
	back := make([]byte, len(raw))
	n, re := read.file.ReadAt(back, offset)
	ce = read.file.Close()
	if re != nil || n != len(back) || ce != nil || !bytes.Equal(raw, back) {
		return errors.Join(storagebudget.ErrUnknown, re, ce)
	}
	if e = o.fail("intent", "after-readback"); e != nil {
		return e
	}
	// Installation control identities/bytes stay original after readback.
	if op == nil {
		e = o.ref.cell.current()
	} else {
		e = op.originalReadCurrent(ctx)
	}
	if e != nil {
		return e
	}
	if e = ctx.Err(); e != nil {
		return errors.Join(storagebudget.ErrUnknown, e)
	}
	t.slots[r.Slot] = storageIntentSlot{r, raw, 1 - old.bank}
	t.creation[r.Slot] = nil
	t.identity[r.Slot] = nil
	t.creationPredecessor[r.Slot] = nil
	return nil
}
func storageNextIntent(old storageIntentSlot, phase storageIntentPhase) storageIntentRecord {
	r := old.record
	r.Generation++
	r.Phase = phase
	r.PreviousPageDigest = sha256.Sum256(old.page)
	r.Components = append([]storagebudget.Component(nil), r.Components...)
	r.Changes = append([]storagebudget.Change(nil), r.Changes...)
	return r
}
func storageIntentOperation(r storageIntentRecord) string {
	if r.Transfer != nil {
		return storageTransferOperation(r.OperationID, r.CASBinding)
	}
	if r.Fanout != nil {
		d := storageIntentFanoutPlan(r)
		return "single-fanout:" + storageHexID(r.OperationID) + ":" + fmt.Sprintf("%x", d[:])
	}

	return "single:" + storageHexID(r.OperationID)
}
func storageIntentRetiredProof(r storageIntentRecord, status storagebudget.HostState) bool {
	if r.Phase != storageIntentRetired || !storageIntentToken(r.Token) {
		return false
	}
	s := status.Slots[r.Token.Slot]
	a := s.PersistedApplied
	if a == r.Token {
		return s.PersistedNextSequence > r.Token.Sequence
	}
	return status.Epoch == r.RetirementEpoch && a.ID != (storagebudget.ID{}) && a.Sequence > r.Token.Sequence && a.Generation > 0 && s.PersistedNextSequence > a.Sequence
}
func storageIntentMatchingTicket(r storageIntentRecord, t storagebudget.Ticket, binding storagebudget.Digest) bool {
	return t.State != storagebudget.Empty && t.Workflow == storageIntentQWorkflow(r) && t.Operation == storageIntentOperation(r) && t.Owner == binding && reflect.DeepEqual(t.Components, r.Components) && (t.Fanout != nil) == (r.Fanout != nil || r.Transfer != nil)
}
func (o *storageBudgetOwner) validateIntentMappings(ctx context.Context, table *storageIntentTable, status storagebudget.HostState) error {
	return o.validateIntentMappingsOriginal(ctx, table, status, nil)
}

func (o *storageBudgetOwner) validateIntentMappingsOriginal(ctx context.Context, table *storageIntentTable, status storagebudget.HostState, original *storagePolicyOriginal) error {
	validateManifest := func(r storageIntentRecord, t storagebudget.Ticket) error {
		if identity := table.identity[r.Slot]; identity != nil {
			if !reflect.DeepEqual(identity.Core, r) {
				return errStorageOwnerRecovery
			}
			want, e := storageIdentityRequest(*identity, o.ref.cell.binding)
			if e != nil {
				return e
			}
			got, e := o.ref.cell.q.Manifest(ctx, t.Token, o.ref.cell.binding)
			if e != nil || !reflect.DeepEqual(got, want) {
				return errors.Join(errStorageOwnerRecovery, e)
			}
			return nil
		}
		if creation := table.creation[r.Slot]; creation != nil {
			if !reflect.DeepEqual(creation.Core, r) {
				return errStorageOwnerRecovery
			}
			want, e := storageCreationRequestBound(*creation, o.ref.cell.binding)
			if e != nil {
				return e
			}
			got, e := o.ref.cell.q.Manifest(ctx, t.Token, o.ref.cell.binding)
			if e != nil || !reflect.DeepEqual(got, want) {
				return errors.Join(errStorageOwnerRecovery, e)
			}
			return nil
		}
		return o.validateIntentManifest(ctx, r, t)
	}
	allZero := true
	for _, host := range table.slots {
		if host.record.Phase != storageIntentEmpty {
			allZero = false
			break
		}
	}
	if allZero {
		for _, s := range status.Slots {
			if s.Ticket.State != storagebudget.Empty && storageKnownIntentWorkflow(s.Ticket.Workflow) {
				return errStorageOwnerRecovery
			}
		}
	}
	// Legitimate retirement/reuse never zeros the host allocation. Even a fully
	// incorporated protocol ticket cannot explain losing all original history.

	// Even zero banks do not authorize replacing a lost operation mapping. A
	// protocol ticket not fully incorporated by its exact persisted tuple requires
	// an original journal relation. Fully incorporated tickets have no live charge
	// or write authority; their finite history may already have been evicted.
	for _, s := range status.Slots {
		t := s.Ticket
		if t.State == storagebudget.Empty || !storageUnretired(s) || !storageKnownIntentWorkflow(t.Workflow) {
			continue
		}
		matches := 0
		for _, host := range table.slots {
			r := host.record
			if r.Phase == storageIntentEmpty || r.Phase == storageIntentRejected {
				continue
			}
			if storageIntentMatchingTicket(r, t, o.ref.cell.binding) {
				matches++
			}
		}
		if matches != 1 {
			return errStorageOwnerRecovery
		}
	}
	for _, host := range table.slots {
		r := host.record
		if r.Phase == storageIntentEmpty {
			continue
		}
		if err := o.validateOriginalTransferBank(ctx, r); err != nil {
			return err
		}
		for i, c := range r.Components {
			root := o.roots[c.Root]
			if root == nil || root.row.Identity != r.RootIdentities[i] || root.row.Owner != c.Owner || root.row.Generation != c.RootGeneration || root.row.Filesystem != c.Filesystem || root.row.Locator != r.Locators[i] || o.filesystems[c.Filesystem].Identity != r.FilesystemIdentities[i] || !o.intentCapacityMatches(root, c) {
				return errStorageOwnerRecovery
			}
		}
		if creation := table.creation[r.Slot]; creation != nil && creation.Kind == storageCreationLocalPrivate && r.Phase != storageIntentRetired && r.Phase != storageIntentRejected && r.Phase != storageIntentPrepared {
			ticket := status.Slots[r.Token.Slot].Ticket
			if !storageCreationOriginalQMapping(*creation, ticket, o.ref.cell.binding) {
				return errStorageOwnerRecovery
			}
			if storageUnretired(status.Slots[r.Token.Slot]) {
				if e := validateManifest(r, ticket); e != nil {
					return e
				}
			}
			continue
		}
		if r.Fanout != nil || r.Transfer != nil {
			if _, err := o.originalIntentFanout(r); err != nil {
				return err
			}
		}
		if r.Phase == storageIntentRetired {
			if err := o.validateAppliedIntentFanout(ctx, r, status); err != nil {
				return err
			}
			if !storageIntentRetiredProof(r, status) && !o.coveredEnrollmentHost(host, status) && !o.policyCompletedHostOriginal(ctx, host, status, original) {
				return errStorageOwnerRecovery
			}
			continue
		}
		if r.Phase == storageIntentRejected {
			if status.Epoch != r.AdmissionEpoch && !o.coveredEnrollmentHost(host, status) && !o.policyCompletedHostOriginal(ctx, host, status, original) {
				return errStorageOwnerRecovery
			}
			for _, s := range status.Slots {
				if storageIntentMatchingTicket(r, s.Ticket, o.ref.cell.binding) {
					return errStorageOwnerRecovery
				}
			}
			continue
		}
		if r.Phase == storageIntentPrepared {
			for _, s := range status.Slots {
				if storageUnretired(s) && storageIntentMatchingTicket(r, s.Ticket, o.ref.cell.binding) {
					if s.Ticket.State != storagebudget.Reserved {
						return errStorageOwnerRecovery
					}
					if e := validateManifest(r, s.Ticket); e != nil {
						return e
					}
				}
			}
			continue
		} // Never adopts an uncertain Reserve.
		if !storageIntentToken(r.Token) {
			return errStorageOwnerRecovery
		}
		t := status.Slots[r.Token.Slot].Ticket
		if !storageIntentMatchingTicket(r, t, o.ref.cell.binding) {
			return errStorageOwnerRecovery
		}
		expectedState := storagebudget.Staging
		if r.Phase == storageIntentReserved {
			expectedState = storagebudget.Reserved
		}
		if r.Phase == storageIntentSettled {
			expectedState = storagebudget.Terminal
		}
		exact := t.Token == r.Token && t.State == expectedState
		if !exact && !(r.Phase == storageIntentReserved && t.State == storagebudget.Staging && storageIntentNextToken(r.Token, t.Token)) && !(r.Phase == storageIntentSettling && t.State == storagebudget.Terminal && storageIntentNextToken(r.Token, t.Token)) {
			return errStorageOwnerRecovery
		}
		if r.PolicyRevision != t.PolicyRevision {
			return errStorageOwnerRecovery
		}
		// Only a still-current ticket may have its manifest dereferenced. Retired
		// manifests can have been overwritten by the slot's next operation.
		if !storageUnretired(status.Slots[r.Token.Slot]) && (r.Fanout != nil || r.Transfer != nil) {
			if err := o.validateAppliedIntentFanout(ctx, r, status); err != nil {
				return err
			}
		}
		if storageUnretired(status.Slots[r.Token.Slot]) {
			if e := validateManifest(r, t); e != nil {
				return e
			}
		}
	}
	return nil
}

func (o *storageBudgetOwner) validateIntentManifest(ctx context.Context, r storageIntentRecord, t storagebudget.Ticket) error {
	request, err := o.ref.cell.q.Manifest(ctx, t.Token, o.ref.cell.binding)
	if err != nil {
		return err
	}
	expected, err := o.originalIntentRequest(r)
	if err != nil || !reflect.DeepEqual(request, expected) {
		return errors.Join(errStorageOwnerRecovery, err)
	}
	return nil
}

func (o *storageBudgetOwner) intentCapacityMatches(r *storageRootBinding, c storagebudget.Component) bool {
	fs, ok := o.filesystems[c.Filesystem]
	if !ok || fs.Generation != c.FilesystemGeneration || r.row.CapacityCount != c.CapacityCount || r.row.CapacityBinding != c.CapacityBinding {
		return false
	}
	if c.Workspace != (storagebudget.ID{}) {
		w, ok := o.workspaces[c.Workspace]
		if !ok || w.Generation != c.WorkspaceGeneration {
			return false
		}
	}
	p, e := storageVectorAdd(c.Physical, c.SettlementPhysical)
	if e != nil {
		return false
	}
	if p.Files > 1<<40-p.Directories {
		return false
	}
	inodes := p.Files + p.Directories
	for i, ref := range r.row.CapacityRefs {
		peak := c.Capacity[i]
		if i >= int(c.CapacityCount) {
			if peak != (storagebudget.CapacityPromise{}) {
				return false
			}
			continue
		}
		row, ok := o.constraints[ref.Constraint]
		if !ok || peak.Constraint != ref.Constraint || peak.Generation != ref.Generation || row.Generation != ref.Generation || peak.Peak.Bytes > 1<<60 || peak.Peak.Inodes > 1<<40 {
			return false
		}
		if p == (storagebudget.Vector{}) {
			if peak.Peak != (storagebudget.CapacityVector{}) {
				return false
			}
			continue
		}
		if row.Resources&storagebudget.CapacityBytes != 0 {
			if peak.Peak.Bytes < p.Bytes || inodes > 0 && peak.Peak.Bytes == 0 {
				return false
			}
		} else if peak.Peak.Bytes != 0 {
			return false
		}
		if row.Resources&storagebudget.CapacityInodes != 0 {
			if peak.Peak.Inodes < inodes {
				return false
			}
		} else if peak.Peak.Inodes != 0 {
			return false
		}
	}
	return true
}

// policyCompletedAt returns the actual completed V6 policy when Q is exactly at
// its outcome: the same full host state, the policy's effective definitions,
// its boundary epoch and no terminal left unapplied. Only that can explain a
// policy-only epoch advance; a legacy or recovery-only image cannot widen it.
func (o *storageBudgetOwner) policyCompletedAt(ctx context.Context, status storagebudget.HostState, original *storagePolicyOriginal) (storagePolicyOriginal, bool) {
	var state storagePolicyOriginal
	if original == nil {
		active, err := o.policyReserveClosureActive(ctx)
		if err != nil || !active {
			return state, false
		}
		state, err = o.readOriginalPolicy(ctx)
		if err != nil {
			return state, false
		}
	} else {
		// Same strictly decoded images were compared under bootstrap EX; retain
		// the original V6-only policy epoch exception, not a legacy widening.
		if ctx.Err() != nil || original.RecoveryOnly ||
			(!bytes.HasPrefix(original.Actual[0], storageOwnerV6Magic[:]) && !bytes.HasPrefix(original.Actual[1], storageOwnerV6Magic[:])) {
			return state, false
		}
		state = *original
	}
	q, err := o.ref.cell.q.InspectAction(ctx)
	if err != nil || !reflect.DeepEqual(storageHostState(q.View), status) {
		return state, false
	}
	if state.RecoveryOnly {
		o.admission.mu.Lock()
		p := o.admission.enrollment
		o.admission.mu.Unlock()
		if p == nil || p.originalPolicyCurrentQ(ctx, state, q) != nil {
			return state, false
		}
	} else if storagePolicyCompletedQ(state, q) != nil {
		return state, false
	}
	return state, true
}

// policyCompletedDefinitions reports whether Q's definitions in v are exactly
// the effective definitions of a completed original policy that Q is at, for
// a start whose catalogue predates the limits edited since enrollment.
func (o *storageBudgetOwner) policyCompletedDefinitions(ctx context.Context, v storagebudget.HostInspection) bool {
	if !o.valid() || v.Configuration.Maintenance {
		return false
	}
	q, err := o.ref.cell.q.InspectAction(ctx)
	if err != nil {
		return false
	}
	state, ok := o.policyCompletedAt(ctx, storageHostState(q.View), nil)
	return ok && !state.RecoveryOnly && reflect.DeepEqual(storageOwnerDefinitionSnapshot(v), state.Effective) && reflect.DeepEqual(storageHostState(v), storageHostState(q.View))
}

// policyExplainedCoverageEpoch is the epoch at which the enrollment endpoint
// covered its retained pages, when the advance from it to the current epoch is
// explained by completed policy edits alone. Each limit edit after enrollment
// moves Q to a new epoch without touching the enrollment: the completed policy
// is built on this exact endpoint evidence, and Q, in this same full host
// state, is at that policy's boundary epoch (policyCompletedAt). Anything else
// (no completed policy, another endpoint, another epoch or host state, an
// unapplied terminal) explains nothing and returns false.
func (o *storageBudgetOwner) policyExplainedCoverageEpoch(ctx context.Context, e storageEnrollmentPeakEvidence, status storagebudget.HostState, original *storagePolicyOriginal) (uint64, bool) {
	if !o.valid() || e.Phase != 2 || e.CoveredEpoch == 0 || e.CoveredEpoch >= status.Epoch {
		return 0, false
	}
	state, ok := o.policyCompletedAt(ctx, status, original)
	if !ok || state.RecoveryOnly || state.Evidence.Phase != 2 || !bytes.Equal(state.Evidence.Raw, e.Raw) || state.Evidence.CoveredEpoch != e.CoveredEpoch {
		return 0, false
	}
	return e.CoveredEpoch, true
}

// Only a strict actual completed V6 source can explain a policy-only epoch
// advance. This supplements the unchanged legacy epoch guard for an already
// durable Retired/Rejected page; it never promotes an unfinished operation.
// The existing caller still checks component ownership, manifests and tails.
func (o *storageBudgetOwner) policyCompletedHost(ctx context.Context, host storageIntentSlot, status storagebudget.HostState) bool {
	return o.policyCompletedHostOriginal(ctx, host, status, nil)
}

func (o *storageBudgetOwner) policyCompletedHostOriginal(ctx context.Context, host storageIntentSlot, status storagebudget.HostState, original *storagePolicyOriginal) bool {
	if !o.valid() || len(host.page) != storageIntentPage || host.record.Slot >= storagebudget.Slots || host.record.Phase != storageIntentRetired && host.record.Phase != storageIntentRejected {
		return false
	}
	state, ok := o.policyCompletedAt(ctx, status, original)
	if !ok {
		return false
	}
	r := host.record
	if r.AdmissionEpoch < state.Evidence.AfterEpoch || r.AdmissionEpoch > status.Epoch {
		return false
	}
	file, err := storageOpenOriginalControl(o.ref.cell, storageIntentName)
	if err != nil {
		return false
	}
	var pages [2][]byte
	for i := range pages {
		pages[i] = make([]byte, storageIntentPage)
		n, re := file.file.ReadAt(pages[i], int64((int(r.Slot)*2+i)*storageIntentPage))
		if re != nil || n != len(pages[i]) {
			_ = file.file.Close()
			return false
		}
	}
	if errors.Join(file.current(), file.file.Close()) != nil {
		return false
	}
	selection, err := storageSelectCreationHost(pages[0], pages[1], int(r.Slot), o.ref.cell.record.Installation, o.ref.cell.binding)
	if err != nil {
		return false
	}
	found := false
	for _, page := range pages {
		if bytes.Equal(page, host.page) {
			decoded, err := storageDecodeCreationHostPage(page, int(r.Slot), o.ref.cell.record.Installation, o.ref.cell.binding)
			if err != nil || !reflect.DeepEqual(decoded.Slot.record, r) {
				return false
			}
			found = true
		}
	}
	if !found || !bytes.Equal(selection.Slot.page, host.page) && (selection.Slot.record.Phase != storageIntentPrepared || selection.Slot.record.PreviousPageDigest != sha256.Sum256(host.page)) {
		return false
	}
	if r.Phase == storageIntentRejected {
		for _, s := range status.Slots {
			if storageIntentMatchingTicket(r, s.Ticket, o.ref.cell.binding) {
				return false
			}
		}
		return true
	}
	if !storageIntentToken(r.Token) || r.RetirementEpoch < r.AdmissionEpoch || r.RetirementEpoch > status.Epoch {
		return false
	}
	slot := status.Slots[r.Token.Slot]
	applied := slot.PersistedApplied
	return applied == r.Token && slot.PersistedNextSequence > r.Token.Sequence || applied.ID != (storagebudget.ID{}) && applied.Sequence > r.Token.Sequence && applied.Generation > 0 && slot.PersistedNextSequence > applied.Sequence
}
