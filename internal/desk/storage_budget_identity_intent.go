package desk

// Identity J3/J5 comparison formats. The original producer/issuer separately
// retains custody and admission; decoding these facts grants neither.
import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"reflect"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

const (
	storageIdentityIntentVersion        = 3
	storageIdentityIntentExtension      = 15791
	storageIdentityIntentExtensionBytes = 384
	storageIdentityIntentEncoded        = 16175
	storageIdentityIntentBody           = 16047
	storageIdentityIntentCreate         = 1
	storageIdentityIntentClear          = 2
	storageIdentityIntentStamp          = 3
	storageIdentityIntentInitialCAS     = 4
	storageIdentityMoveEncoded          = storageIdentityIntentEncoded + 200 // same fixed16KiB page, closed subformat3 only
	storageIdentityMoveBody             = storageIdentityIntentBody + 200
	storageIdentityIntentMove           = 5 // explicit J5 subformat3, not a legacy action
)

type storageIdentityNativePhase uint8

const (
	storageIdentityNativeNone        storageIdentityNativePhase = iota // 0: Prepared/Reserved/Staging/Rejected
	storageIdentityAllocatedObserved                                   // 1: Staging
	storageIdentityPayloadReady                                        // 2: Staging
	storageIdentityLinkedObserved                                      // 3: Publishing
	storageIdentityTargetDurable                                       // 4: Publishing/Settling/Settled/Retired; ledger stage-removed
	storageIdentityAbortPrepared                                       // 5: Staging; allocated original only
	storageIdentityAbortObserved                                       // 6: Staging/Settling/Settled/Retired
	storageIdentityEntered                                             // 7: persisted before the one CAS/stamp rename
)
const (
	storageIdentityParentPresent uint16 = 1 << iota
	storageIdentityAllocatedPresent
	storageIdentityLinkedPresent
	storageIdentityTargetPresent
	storageIdentityExpectedOldPresent // reserved for another action; forbidden for create
)
const (
	storageIdentityPayloadWritten uint32 = 1 << iota
	storageIdentityPayloadChmod
	storageIdentityPayloadSynced
	storageIdentityPayloadClosed
	storageIdentityPayloadReadback
	storageIdentityPayloadParentSynced
	storageIdentityPayloadParentClosed
	storageIdentityLinkedParentSynced
	storageIdentityLinkedParentClosed
	storageIdentityStageRemoved
	storageIdentityTargetSynced
	storageIdentityTargetClosed
	storageIdentityTargetReadback
	storageIdentityFinalParentSynced
	storageIdentityFinalParentClosed
	storageIdentityAbortRemoved
	storageIdentityAbortZeroLinks
	storageIdentityAbortClosed
	storageIdentityAbortParentSynced
	storageIdentityAbortParentClosed
	storageIdentityAbortAbsent
)
const (
	storageIdentityPayloadFacts   uint32 = (1 << 7) - 1
	storageIdentityPublishedFacts uint32 = (1 << 15) - 1
	storageIdentityAbortFacts     uint32 = ((1 << 21) - 1) &^ ((1 << 15) - 1)
)

type storageIdentityIntent struct {
	MoveTails                                                             storageMoveTailJournal
	Core                                                                  storageIntentRecord
	Action                                                                uint8
	NativePhase                                                           storageIdentityNativePhase
	Presence                                                              uint16
	PlanDigest, PayloadDigest                                             storagebudget.Digest
	Parent, AllocatedStage, LinkedStage, Target, ExpectedOld              storageOwnerPersistentObject
	AllocatedLinks, LinkedStageLinks, LinkedTargetLinks, FinalTargetLinks uint64
	Facts                                                                 uint32
}

func (r storageIdentityIntent) validate() error {
	if r.Action != storageIdentityIntentMove && r.MoveTails != (storageMoveTailJournal{}) {
		return storagebudget.ErrUnknown
	}
	if r.Action != storageIdentityIntentCreate {
		return r.validateAction()
	}
	c := r.Core
	if storageIdentityIntentCoreValidate(c) != nil || r.Action != storageIdentityIntentCreate || r.PlanDigest == (storagebudget.Digest{}) || r.PayloadDigest == (storagebudget.Digest{}) || c.Workflow != "desk-signing-lineage-v1" || c.Operation != "p:"+hex.EncodeToString(r.PlanDigest[:])+":create" || c.NewLength == 0 || c.NewLength > 512 || c.OldExists || c.OldIdentity != (storageDirectoryIdentity{}) || c.OldLength != 0 || c.OldLinkCount != 0 || c.StageIdentity != (storageDirectoryIdentity{}) || r.ExpectedOld != (storageOwnerPersistentObject{}) || !r.Parent.valid(1) {
		return storagebudget.ErrUnknown
	}
	flags := storageIdentityParentPresent
	allocated := r.NativePhase != storageIdentityNativeNone
	linked := r.NativePhase == storageIdentityLinkedObserved || r.NativePhase == storageIdentityTargetDurable
	if allocated {
		flags |= storageIdentityAllocatedPresent
	}
	if linked {
		flags |= storageIdentityLinkedPresent | storageIdentityTargetPresent
	}
	if r.Presence != flags || c.StageExists != allocated {
		return storagebudget.ErrUnknown
	}
	if allocated {
		if !r.AllocatedStage.valid(2) || !storageIdentitySameVolume(r.Parent, r.AllocatedStage) || r.AllocatedLinks != 1 {
			return storagebudget.ErrUnknown
		}
	} else if r.AllocatedStage != (storageOwnerPersistentObject{}) || r.AllocatedLinks != 0 {
		return storagebudget.ErrUnknown
	}
	if linked {
		// Separately recorded observations are deliberately not equated by object or
		// incarnation. A qualified live/restart native predicate must prove relation.
		if !r.LinkedStage.valid(2) || !r.Target.valid(2) || !storageIdentitySameVolume(r.Parent, r.LinkedStage) || !storageIdentitySameVolume(r.Parent, r.Target) || r.LinkedStageLinks != 2 || r.LinkedTargetLinks != 2 {
			return storagebudget.ErrUnknown
		}
	} else if r.LinkedStage != (storageOwnerPersistentObject{}) || r.Target != (storageOwnerPersistentObject{}) || r.LinkedStageLinks != 0 || r.LinkedTargetLinks != 0 {
		return storagebudget.ErrUnknown
	}
	expectedPhase := storageIntentStaging
	switch r.NativePhase {
	case storageIdentityNativeNone:
		if c.Phase != storageIntentPrepared && c.Phase != storageIntentReserved && c.Phase != storageIntentStaging && c.Phase != storageIntentRejected || r.Facts != 0 {
			return storagebudget.ErrUnknown
		}
		expectedPhase = c.Phase
	case storageIdentityAllocatedObserved:
		if r.Facts != 0 {
			return storagebudget.ErrUnknown
		}
	case storageIdentityPayloadReady:
		if r.Facts != storageIdentityPayloadFacts {
			return storagebudget.ErrUnknown
		}
	case storageIdentityLinkedObserved:
		expectedPhase = storageIntentPublishing
		if r.Facts != storageIdentityPayloadFacts {
			return storagebudget.ErrUnknown
		}
	case storageIdentityTargetDurable:
		expectedPhase = storageIntentPublishing
		if r.Facts != storageIdentityPublishedFacts || r.FinalTargetLinks != 1 {
			return storagebudget.ErrUnknown
		}
	case storageIdentityAbortPrepared:
		if r.Facts != 0 && r.Facts != storageIdentityPayloadFacts {
			return storagebudget.ErrUnknown
		}
	case storageIdentityAbortObserved:
		if r.Facts != storageIdentityAbortFacts && r.Facts != storageIdentityAbortFacts|storageIdentityPayloadFacts {
			return storagebudget.ErrUnknown
		}
	default:
		return storagebudget.ErrUnknown
	}
	if r.NativePhase != storageIdentityTargetDurable && r.FinalTargetLinks != 0 {
		return storagebudget.ErrUnknown
	}
	terminal := c.Phase >= storageIntentSettling && c.Phase <= storageIntentRetired
	if terminal {
		if r.NativePhase != storageIdentityTargetDurable && r.NativePhase != storageIdentityAbortObserved || r.NativePhase == storageIdentityTargetDurable && c.Outcome != storageIntentPublished || r.NativePhase == storageIdentityAbortObserved && c.Outcome != storageIntentAborted {
			return storagebudget.ErrUnknown
		}
		expected, err := storageIdentitySettlementChanges(r)
		if err != nil || !reflect.DeepEqual(c.Changes, expected) {
			return storagebudget.ErrUnknown
		}

		d, err := storageOwnerDigest("desk-storage-identity-publication-v1", storageIdentityPublicationMaterial(r))
		if err != nil || d != c.Evidence {
			return storagebudget.ErrUnknown
		}
	} else if c.Phase != expectedPhase || c.Evidence != (storagebudget.Digest{}) {
		return storagebudget.ErrUnknown
	}
	return nil
}
func storageIdentitySameVolume(a, b storageOwnerPersistentObject) bool {
	return a.Backend == b.Backend && a.VolumeUUID == b.VolumeUUID
}

// Each native tuple is packed into exactly 35 bytes: u8 backend, UUID[16],
// u64 object, u64 incarnation, u8 incarnation-kind, u8 object-kind. No ABI padding.
func storageIdentityIntentTuple(c *storageIntentCodec, p *storageOwnerPersistentObject, read bool) {
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
func storageIdentityIntentExtensionCodec(b []byte, r *storageIdentityIntent, read bool) bool {
	c := storageIntentCodec{b: b, at: storageIdentityIntentExtension}
	version := uint64(1)
	if len(b) == storageIntentPage && string(b[:8]) == "DSKHINT5" {
		version = 2
		if b[13] == 3 {
			version = 3
		}
	}
	if read {
		if c.get(2) != version {
			c.bad = true
		}
		r.Action = uint8(c.get(1))
		r.NativePhase = storageIdentityNativePhase(c.get(1))
		r.Presence = uint16(c.get(2))
	} else {
		c.put(version, 2)
		c.put(uint64(r.Action), 1)
		c.put(uint64(r.NativePhase), 1)
		c.put(uint64(r.Presence), 2)
	}
	c.skip(2, read)
	c.digest(&r.PlanDigest, read)
	c.digest(&r.PayloadDigest, read)
	for _, p := range []*storageOwnerPersistentObject{&r.Parent, &r.AllocatedStage, &r.LinkedStage, &r.Target, &r.ExpectedOld} {
		storageIdentityIntentTuple(&c, p, read)
	}
	c.number(&r.AllocatedLinks, read)
	c.number(&r.LinkedStageLinks, read)
	c.number(&r.LinkedTargetLinks, read)
	c.number(&r.FinalTargetLinks, read)
	if read {
		r.Facts = uint32(c.get(4))
	} else {
		c.put(uint64(r.Facts), 4)
	}
	if version == 3 {
		j := &r.MoveTails
		c.digest(&j.Plan, read)
		if read {
			j.Count = byte(c.get(1))
			j.Allocated = byte(c.get(1))
			j.Entered = byte(c.get(1))
			j.Done = byte(c.get(1))
		} else {
			c.put(uint64(j.Count), 1)
			c.put(uint64(j.Allocated), 1)
			c.put(uint64(j.Entered), 1)
			c.put(uint64(j.Done), 1)
		}
		for i := 0; i < storageMoveTailMaximum; i++ {
			if read {
				j.Component[i] = byte(c.get(1))
			} else {
				c.put(uint64(j.Component[i]), 1)
			}
			c.number(&j.BeforeBytes[i], read)
			c.number(&j.AfterBytes[i], read)
			if read {
				v := c.get(1)
				if v > 1 {
					c.bad = true
				}
				j.BeforeExists[i] = v == 1
			} else {
				v := uint64(0)
				if j.BeforeExists[i] {
					v = 1
				}
				c.put(v, 1)
			}
			storageIdentityIntentTuple(&c, &j.Staged[i], read)
		}
		return !c.bad && c.at == storageIdentityMoveEncoded
	}
	c.skip(101, read)
	return !c.bad && c.at == storageIdentityIntentEncoded
}
func storageEncodeIdentityIntent(r storageIdentityIntent, installation storagebudget.ID, binding storagebudget.Digest) ([]byte, error) {
	if r.validate() != nil || installation == (storagebudget.ID{}) || binding == (storagebudget.Digest{}) {
		return nil, storagebudget.ErrUnknown
	}
	b := make([]byte, storageIntentPage)
	copy(b[:8], "DSKHINT3")
	binary.LittleEndian.PutUint16(b[8:10], storageIdentityIntentVersion)
	if r.Action != storageIdentityIntentCreate {
		copy(b[:8], "DSKHINT5")
		binary.LittleEndian.PutUint16(b[8:10], 5)
		b[13] = 1
		if r.Action == storageIdentityIntentMove {
			b[13] = 3
		}
	}
	binary.LittleEndian.PutUint16(b[10:12], r.Core.Slot)
	b[12] = byte(r.Core.Phase)
	binary.LittleEndian.PutUint64(b[16:24], r.Core.Generation)
	copy(b[24:40], installation[:])
	copy(b[40:72], binding[:])
	body := uint32(storageIdentityIntentBody)
	if r.Action == storageIdentityIntentMove {
		body = storageIdentityMoveBody
	}
	binary.LittleEndian.PutUint32(b[72:76], body)
	if !storageIntentBodyCodec(b, &r.Core, false) || !storageIdentityIntentExtensionCodec(b, &r, false) {
		return nil, storagebudget.ErrBounds
	}
	sum := sha256.Sum256(b)
	copy(b[76:108], sum[:])
	return b, nil
}
func storageDecodeIdentityIntent(b []byte, slot int, installation storagebudget.ID, binding storagebudget.Digest) (storageIdentityIntent, error) {
	var r storageIdentityIntent
	encoded, body := storageIdentityIntentEncoded, uint32(storageIdentityIntentBody)
	if len(b) == storageIntentPage && string(b[:8]) == "DSKHINT5" && b[13] == 3 {
		encoded, body = storageIdentityMoveEncoded, storageIdentityMoveBody
	}
	if len(b) != storageIntentPage || slot < 0 || slot >= storagebudget.Slots || installation == (storagebudget.ID{}) || binding == (storagebudget.Digest{}) || !(string(b[:8]) == "DSKHINT3" && binary.LittleEndian.Uint16(b[8:10]) == 3 && storageAllZero(b[13:16]) || string(b[:8]) == "DSKHINT5" && binary.LittleEndian.Uint16(b[8:10]) == 5 && (b[13] == 1 || b[13] == 3) && storageAllZero(b[14:16])) || int(binary.LittleEndian.Uint16(b[10:12])) != slot || !bytes.Equal(b[24:40], installation[:]) || !bytes.Equal(b[40:72], binding[:]) || binary.LittleEndian.Uint32(b[72:76]) != body || !storageAllZero(b[108:128]) || !storageAllZero(b[encoded:]) {
		return r, storagebudget.ErrUnknown
	}
	seal := append([]byte(nil), b...)
	clear(seal[76:108])
	sum := sha256.Sum256(seal)
	if !bytes.Equal(sum[:], b[76:108]) {
		return r, storagebudget.ErrUnknown
	}
	r.Core.Slot = uint16(slot)
	r.Core.Generation = binary.LittleEndian.Uint64(b[16:24])
	r.Core.Phase = storageIntentPhase(b[12])
	if !storageIntentBodyCodec(b, &r.Core, true) || !storageIdentityIntentExtensionCodec(b, &r, true) || r.validate() != nil {
		return storageIdentityIntent{}, storagebudget.ErrUnknown
	}
	if (string(b[:8]) == "DSKHINT3") != (r.Action == storageIdentityIntentCreate) || (b[13] == 3) != (r.Action == storageIdentityIntentMove) {
		return storageIdentityIntent{}, storagebudget.ErrUnknown
	}
	return r, nil
}

// V3 repeats the closed common core bounds without changing the v2 validator.
// Native facts replace the v2 device/inode StageIdentity and early-stage rule.
func storageIdentityIntentCoreValidate(r storageIntentRecord) error {
	return storageMonotoneIntentCoreValidate(r, false)
}

// Equal target/stage is a closed direct-creation encoding, selected only by
// the separate monotone creation validator. J2/J3 retain their old refusal.
func storageMonotoneIntentCoreValidate(r storageIntentRecord, direct bool) error {
	// Ordinary file fanout has its own closed plan/authority. Identity and
	// creation extensions retain their original flat body and exact bounds.
	if r.Fanout != nil {
		return storagebudget.ErrUnsupported
	}
	if r.Phase < storageIntentPrepared || r.Phase > storageIntentRejected || r.Slot >= storagebudget.Slots || r.Generation == 0 || r.OperationID == (storagebudget.ID{}) || r.CASBinding == (storagebudget.Digest{}) || r.AdmissionEpoch == 0 || r.NewLength > 1<<60 || !storageIntentRelative(r.Target) || !storageIntentRelative(r.Stage) || (r.Target == r.Stage) != direct || !storageIntentText(r.Workflow, 128, false) || !storageIntentText(r.Operation, 128, false) {
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
			if c.WorkspaceGeneration != 0 || c.Attribution != (storagebudget.Vector{}) || c.SettlementAttribution != (storagebudget.Vector{}) {
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
	if r.Phase == storageIntentRetired {
		if r.RetirementEpoch < r.AdmissionEpoch {
			return storagebudget.ErrUnknown
		}
	} else if r.RetirementEpoch != 0 {
		return storagebudget.ErrUnknown
	}
	return nil
}

func storageIdentityIntentTransition(old, next storageIdentityIntent, oldPage []byte) bool {
	if old.Action != storageIdentityIntentCreate || next.Action != storageIdentityIntentCreate {
		return storageIdentityActionTransition(old, next, oldPage)
	}
	// Even a direct detached caller must supply the exact old validated page,
	// not an unrelated byte string chosen only to satisfy the hash link.
	if len(oldPage) != storageIntentPage {
		return false
	}
	var installation storagebudget.ID
	var binding storagebudget.Digest
	copy(installation[:], oldPage[24:40])
	copy(binding[:], oldPage[40:72])
	got, err := storageDecodeIdentityIntent(oldPage, int(old.Core.Slot), installation, binding)
	if err != nil || !reflect.DeepEqual(got, old) {
		return false
	}
	if old.validate() != nil || next.validate() != nil || len(oldPage) != storageIntentPage || old.Core.Generation == ^uint64(0) || next.Core.Generation != old.Core.Generation+1 || next.Core.Slot != old.Core.Slot || next.Core.PreviousPageDigest != sha256.Sum256(oldPage) {
		return false
	}
	if old.Core.reusable() {
		return next.Core.Phase == storageIntentPrepared && next.Core.OperationID != old.Core.OperationID
	}
	if !reflect.DeepEqual(storageIntentImmutable(old.Core), storageIntentImmutable(next.Core)) || old.Action != next.Action || old.PlanDigest != next.PlanDigest || old.PayloadDigest != next.PayloadDigest || old.Parent != next.Parent || old.ExpectedOld != next.ExpectedOld || old.Core.PolicyRevision != 0 && old.Core.PolicyRevision != next.Core.PolicyRevision || old.Facts&^next.Facts != 0 || old.Presence&^next.Presence != 0 {
		return false
	}
	if old.Presence&storageIdentityAllocatedPresent != 0 && (old.AllocatedStage != next.AllocatedStage || old.AllocatedLinks != next.AllocatedLinks) || old.Presence&storageIdentityLinkedPresent != 0 && (old.LinkedStage != next.LinkedStage || old.Target != next.Target || old.LinkedStageLinks != next.LinkedStageLinks || old.LinkedTargetLinks != next.LinkedTargetLinks) {
		return false
	}
	sameTokens := next.Core.Token == old.Core.Token && next.Core.PreviousToken == old.Core.PreviousToken
	switch old.Core.Phase {
	case storageIntentPrepared:
		return next.Core.Phase == storageIntentReserved || next.Core.Phase == storageIntentRejected
	case storageIntentReserved:
		return next.Core.Phase == storageIntentStaging && next.NativePhase == storageIdentityNativeNone && next.Core.PreviousToken == old.Core.Token && storageIntentNextToken(old.Core.Token, next.Core.Token)
	case storageIntentStaging:
		if !sameTokens {
			return false
		}
		if old.NativePhase == storageIdentityAbortObserved {
			return next.Core.Phase == storageIntentSettling && next.NativePhase == old.NativePhase && next.Facts == old.Facts
		}
		if next.Core.Phase == storageIntentPublishing {
			return old.NativePhase == storageIdentityPayloadReady && next.NativePhase == storageIdentityLinkedObserved
		}
		if next.Core.Phase != storageIntentStaging {
			return false
		}
		switch old.NativePhase {
		case storageIdentityNativeNone:
			return next.NativePhase == storageIdentityAllocatedObserved
		case storageIdentityAllocatedObserved:
			return next.NativePhase == storageIdentityPayloadReady || next.NativePhase == storageIdentityAbortPrepared
		case storageIdentityPayloadReady:
			return next.NativePhase == storageIdentityAbortPrepared
		case storageIdentityAbortPrepared:
			return next.NativePhase == storageIdentityAbortObserved && next.Facts&storageIdentityPayloadFacts == old.Facts&storageIdentityPayloadFacts
		}
	case storageIntentPublishing:
		if !sameTokens {
			return false
		}
		return old.NativePhase == storageIdentityLinkedObserved && next.Core.Phase == storageIntentPublishing && next.NativePhase == storageIdentityTargetDurable || old.NativePhase == storageIdentityTargetDurable && next.Core.Phase == storageIntentSettling && next.NativePhase == old.NativePhase && next.Facts == old.Facts
	case storageIntentSettling:
		return next.Core.Phase == storageIntentSettled && next.NativePhase == old.NativePhase && next.Facts == old.Facts && next.Core.PreviousToken == old.Core.Token && storageIntentNextToken(old.Core.Token, next.Core.Token) && next.Core.Evidence == old.Core.Evidence && next.Core.Outcome == old.Core.Outcome && reflect.DeepEqual(next.Core.Changes, old.Core.Changes)
	case storageIntentSettled:
		return next.Core.Phase == storageIntentRetired && next.NativePhase == old.NativePhase && next.Facts == old.Facts && sameTokens && next.Core.Evidence == old.Core.Evidence && next.Core.Outcome == old.Core.Outcome && reflect.DeepEqual(next.Core.Changes, old.Core.Changes)
	}
	return false
}

type storageIdentityIntentBankKind uint8

const (
	storageIdentityIntentBanksEmpty storageIdentityIntentBankKind = iota
	storageIdentityIntentBanksV2
	storageIdentityIntentBanksV3
)

type storageIdentityIntentSelection struct {
	Kind   storageIdentityIntentBankKind
	Legacy storageIntentSlot
	Record storageIdentityIntent
	Page   []byte
	Bank   int
}

// Detached only. Mixed v2/v3 is recognized unsupported, never an automatic
// upgrade/reuse edge. A malformed/torn nonzero peer refuses without fallback.
func storageSelectIdentityIntent(a, b []byte, slot int, installation storagebudget.ID, binding storagebudget.Digest) (storageIdentityIntentSelection, error) {
	var out storageIdentityIntentSelection
	if len(a) != storageIntentPage || len(b) != storageIntentPage || slot < 0 || slot >= storagebudget.Slots || installation == (storagebudget.ID{}) || binding == (storagebudget.Digest{}) {
		return out, storagebudget.ErrUnknown
	}
	az, bz := storageAllZero(a), storageAllZero(b)
	if az && bz {
		out.Bank = 1
		out.Page = make([]byte, storageIntentPage)
		return out, nil
	}
	v2, v3 := false, false
	for _, raw := range [][]byte{a, b} {
		switch string(raw[:8]) {
		case "DSKHINT1":
			return out, storagebudget.ErrUnsupported
		case "DSKHINT2", "DSKHINT8", "DSKHINT9":
			v2 = true
		case "DSKHINT3":
			v3 = true
		default:
			if !storageAllZero(raw) {
				return out, storagebudget.ErrUnknown
			}
		}
	}
	if v2 && v3 {
		return out, storagebudget.ErrUnsupported
	}
	if v2 {
		s, err := storageSelectIntent(a, b, slot, installation, binding)
		if err != nil {
			return out, err
		}
		out.Kind = storageIdentityIntentBanksV2
		out.Legacy = s
		return out, nil
	}
	var ar, br storageIdentityIntent
	var err error
	if !az {
		ar, err = storageDecodeIdentityIntent(a, slot, installation, binding)
		if err != nil {
			return out, err
		}
	}
	if !bz {
		br, err = storageDecodeIdentityIntent(b, slot, installation, binding)
		if err != nil {
			return out, err
		}
	}
	out.Kind = storageIdentityIntentBanksV3
	if az || bz {
		r, page, bank := ar, a, 0
		if az {
			r, page, bank = br, b, 1
		}
		if r.Core.Generation != 1 || r.Core.Phase != storageIntentPrepared || r.Core.PreviousPageDigest != (storagebudget.Digest{}) {
			return storageIdentityIntentSelection{}, storagebudget.ErrUnknown
		}
		out.Record = r
		out.Page = append([]byte(nil), page...)
		out.Bank = bank
		return out, nil
	}
	if ar.Core.Generation == br.Core.Generation {
		if !bytes.Equal(a, b) {
			return storageIdentityIntentSelection{}, storagebudget.ErrUnknown
		}
		out.Record = ar
		out.Page = append([]byte(nil), a...)
		return out, nil
	}
	old, next, op, np, bank := ar, br, a, b, 1
	if ar.Core.Generation > br.Core.Generation {
		old, next, op, np, bank = br, ar, b, a, 0
	}
	if !storageIdentityIntentTransition(old, next, op) {
		return storageIdentityIntentSelection{}, storagebudget.ErrUnknown
	}
	out.Record = next
	out.Page = append([]byte(nil), np...)
	out.Bank = bank
	return out, nil
}

// J5 has closed action-specific native matrices. These stored comparisons do
// not establish that a rename occurred or authorize repeating an Entered call.
func (r storageIdentityIntent) validateAction() error {
	c := r.Core
	if r.Action == storageIdentityIntentMove && !r.MoveTails.valid(c) {
		return storagebudget.ErrUnknown
	}
	stamp := r.Action == storageIdentityIntentStamp
	if r.Action < storageIdentityIntentClear || r.Action > storageIdentityIntentMove || storageIdentityIntentCoreValidate(c) != nil || r.PlanDigest == (storagebudget.Digest{}) || !r.Parent.valid(1) || c.StageIdentity != (storageDirectoryIdentity{}) || c.Workflow != "desk-signing-lineage-v1" {
		return storagebudget.ErrUnknown
	}
	action := "clear"
	if stamp {
		action = "stamp"
	} else if r.Action == storageIdentityIntentInitialCAS {
		action = "initial-cas"
	} else if r.Action == storageIdentityIntentMove {
		action = "move"
	}
	if c.Operation != "p:"+hex.EncodeToString(r.PlanDigest[:])+":"+action {
		return storagebudget.ErrUnknown
	}
	terminal := c.Phase >= storageIntentSettling && c.Phase <= storageIntentRetired
	expectedPhase := storageIntentStaging
	if stamp {
		if c.OldExists || c.OldIdentity != (storageDirectoryIdentity{}) || c.OldLength != 0 || c.OldLinkCount != 0 || c.NewLength != 0 || c.StageExists || r.PayloadDigest != (storagebudget.Digest{}) || r.ExpectedOld != (storageOwnerPersistentObject{}) || !r.AllocatedStage.valid(1) || !storageIdentitySameVolume(r.Parent, r.AllocatedStage) || r.LinkedStage != (storageOwnerPersistentObject{}) || r.AllocatedLinks != 0 || r.LinkedStageLinks != 0 || r.LinkedTargetLinks != 0 || r.FinalTargetLinks != 0 {
			return storagebudget.ErrUnknown
		}
		presence := uint16(3)
		switch r.NativePhase {
		case storageIdentityNativeNone:
			if c.Phase != storageIntentPrepared && c.Phase != storageIntentReserved && c.Phase != storageIntentStaging && c.Phase != storageIntentRejected || r.Facts != 0 {
				return storagebudget.ErrUnknown
			}
			expectedPhase = c.Phase
		case storageIdentityEntered:
			if r.Facts != 0 {
				return storagebudget.ErrUnknown
			}
		case storageIdentityLinkedObserved:
			expectedPhase, presence = storageIntentPublishing, 11
			if r.Facts != 0 {
				return storagebudget.ErrUnknown
			}
		case storageIdentityTargetDurable:
			expectedPhase, presence = storageIntentPublishing, 11
			if r.Facts != 127 {
				return storagebudget.ErrUnknown
			}
		default:
			return storagebudget.ErrUnknown
		}
		if r.Presence != presence || presence == 11 && r.Target != r.AllocatedStage || presence == 3 && r.Target != (storageOwnerPersistentObject{}) {
			return storagebudget.ErrUnknown
		}
	} else {
		if !c.OldExists || c.OldIdentity == (storageDirectoryIdentity{}) || c.OldLength == 0 || c.OldLength > 512 || c.OldLinkCount != 1 || c.NewLength == 0 || c.NewLength > 512 || r.PayloadDigest == (storagebudget.Digest{}) || !r.ExpectedOld.valid(2) || !storageIdentitySameVolume(r.Parent, r.ExpectedOld) || r.LinkedStage != (storageOwnerPersistentObject{}) || r.LinkedStageLinks != 0 {
			return storagebudget.ErrUnknown
		}
		allocated := r.NativePhase != storageIdentityNativeNone
		published := r.NativePhase == storageIdentityLinkedObserved || r.NativePhase == storageIdentityTargetDurable
		presence := uint16(17)
		if allocated {
			presence |= 2
		}
		if published {
			presence |= 8
		}
		if r.Presence != presence || c.StageExists != allocated || allocated && (!r.AllocatedStage.valid(2) || !storageIdentitySameVolume(r.Parent, r.AllocatedStage) || r.AllocatedLinks != 1 || r.AllocatedStage == r.ExpectedOld) || !allocated && (r.AllocatedStage != (storageOwnerPersistentObject{}) || r.AllocatedLinks != 0) {
			return storagebudget.ErrUnknown
		}
		if published {
			if r.Target != r.AllocatedStage || r.LinkedTargetLinks != 1 {
				return storagebudget.ErrUnknown
			}
		} else if r.Target != (storageOwnerPersistentObject{}) || r.LinkedTargetLinks != 0 {
			return storagebudget.ErrUnknown
		}
		switch r.NativePhase {
		case storageIdentityNativeNone:
			if c.Phase != storageIntentPrepared && c.Phase != storageIntentReserved && c.Phase != storageIntentStaging && c.Phase != storageIntentRejected || r.Facts != 0 {
				return storagebudget.ErrUnknown
			}
			expectedPhase = c.Phase
		case storageIdentityAllocatedObserved:
			if r.Facts != 0 {
				return storagebudget.ErrUnknown
			}
		case storageIdentityPayloadReady, storageIdentityEntered:
			if r.Facts != 127 {
				return storagebudget.ErrUnknown
			}
		case storageIdentityLinkedObserved:
			expectedPhase = storageIntentPublishing
			if r.Facts != 127 {
				return storagebudget.ErrUnknown
			}
		case storageIdentityTargetDurable:
			expectedPhase = storageIntentPublishing
			if r.Facts != 32767 || r.FinalTargetLinks != 1 {
				return storagebudget.ErrUnknown
			}
		case storageIdentityAbortPrepared:
			if r.Facts != 0 && r.Facts != 127 {
				return storagebudget.ErrUnknown
			}
		case storageIdentityAbortObserved:
			if r.Facts != storageIdentityAbortFacts && r.Facts != storageIdentityAbortFacts|127 {
				return storagebudget.ErrUnknown
			}
		default:
			return storagebudget.ErrUnknown
		}
		if r.NativePhase != storageIdentityTargetDurable && r.FinalTargetLinks != 0 {
			return storagebudget.ErrUnknown
		}
	}
	if terminal {
		if r.NativePhase != storageIdentityTargetDurable && (stamp || r.NativePhase != storageIdentityAbortObserved) || r.NativePhase == storageIdentityTargetDurable && c.Outcome != storageIntentPublished || r.NativePhase == storageIdentityAbortObserved && c.Outcome != storageIntentAborted {
			return storagebudget.ErrUnknown
		}
		expected, err := storageIdentitySettlementChanges(r)
		if err != nil || !reflect.DeepEqual(c.Changes, expected) {
			return storagebudget.ErrUnknown
		}

		d, err := storageOwnerDigest("desk-storage-identity-action-publication-v2", storageIdentityPublicationMaterial(r))
		if err != nil || d != c.Evidence {
			return storagebudget.ErrUnknown
		}
	} else if c.Phase != expectedPhase || c.Evidence != (storagebudget.Digest{}) {
		return storagebudget.ErrUnknown
	}
	return nil
}

func storageIdentityActionTransition(old, next storageIdentityIntent, oldPage []byte) bool {
	if len(oldPage) != storageIntentPage || old.Action == storageIdentityIntentCreate || next.Action != old.Action {
		return false
	}
	var installation storagebudget.ID
	var binding storagebudget.Digest
	copy(installation[:], oldPage[24:40])
	copy(binding[:], oldPage[40:72])
	decoded, err := storageDecodeIdentityIntent(oldPage, int(old.Core.Slot), installation, binding)
	if err != nil || !reflect.DeepEqual(decoded, old) || old.validate() != nil || next.validate() != nil || old.Core.Generation == ^uint64(0) || next.Core.Generation != old.Core.Generation+1 || next.Core.Slot != old.Core.Slot || next.Core.PreviousPageDigest != sha256.Sum256(oldPage) {
		return false
	}
	if old.Core.reusable() {
		return next.Core.Phase == storageIntentPrepared && next.Core.OperationID != old.Core.OperationID
	}
	if !storageMoveTailJournalTransition(old.MoveTails, next.MoveTails) {
		return false
	}
	if !reflect.DeepEqual(storageIntentImmutable(old.Core), storageIntentImmutable(next.Core)) || old.PlanDigest != next.PlanDigest || old.PayloadDigest != next.PayloadDigest || old.Parent != next.Parent || old.ExpectedOld != next.ExpectedOld || old.Core.PolicyRevision != 0 && next.Core.PolicyRevision != old.Core.PolicyRevision || old.Facts&^next.Facts != 0 || old.Presence&^next.Presence != 0 {
		return false
	}
	if old.Presence&2 != 0 && (old.AllocatedStage != next.AllocatedStage || old.AllocatedLinks != next.AllocatedLinks) || old.Presence&8 != 0 && (old.Target != next.Target || old.LinkedTargetLinks != next.LinkedTargetLinks) {
		return false
	}
	same := next.Core.Token == old.Core.Token && next.Core.PreviousToken == old.Core.PreviousToken
	switch old.Core.Phase {
	case storageIntentPrepared:
		return next.Core.Phase == storageIntentReserved || next.Core.Phase == storageIntentRejected
	case storageIntentReserved:
		return next.Core.Phase == storageIntentStaging && next.NativePhase == storageIdentityNativeNone && next.Core.PreviousToken == old.Core.Token && storageIntentNextToken(old.Core.Token, next.Core.Token)
	case storageIntentStaging:
		if old.Action == storageIdentityIntentMove && old.NativePhase == storageIdentityPayloadReady && next.NativePhase == old.NativePhase && next.Core.Phase == old.Core.Phase && old.MoveTails != next.MoveTails {
			return same && old.Facts == next.Facts && old.Core.Evidence == next.Core.Evidence && old.Core.Outcome == next.Core.Outcome && reflect.DeepEqual(old.Core.Changes, next.Core.Changes)
		}
		if !same {
			return false
		}
		if old.NativePhase == storageIdentityAbortObserved {
			return next.Core.Phase == storageIntentSettling && next.NativePhase == old.NativePhase && next.Facts == old.Facts
		}
		if old.NativePhase == storageIdentityEntered {
			return next.Core.Phase == storageIntentPublishing && next.NativePhase == storageIdentityLinkedObserved
		}
		if next.Core.Phase != storageIntentStaging {
			return false
		}
		if old.Action == storageIdentityIntentStamp {
			return old.NativePhase == storageIdentityNativeNone && next.NativePhase == storageIdentityEntered
		}
		switch old.NativePhase {
		case storageIdentityNativeNone:
			return next.NativePhase == storageIdentityAllocatedObserved
		case storageIdentityAllocatedObserved:
			return next.NativePhase == storageIdentityPayloadReady || next.NativePhase == storageIdentityAbortPrepared
		case storageIdentityPayloadReady:
			return next.NativePhase == storageIdentityEntered || next.NativePhase == storageIdentityAbortPrepared
		case storageIdentityAbortPrepared:
			return next.NativePhase == storageIdentityAbortObserved && next.Facts&127 == old.Facts&127
		}
	case storageIntentPublishing:
		if old.Action == storageIdentityIntentMove && old.NativePhase == storageIdentityTargetDurable && next.NativePhase == old.NativePhase && next.Core.Phase == old.Core.Phase && old.MoveTails != next.MoveTails {
			return same && old.Facts == next.Facts && old.Core.Evidence == next.Core.Evidence && old.Core.Outcome == next.Core.Outcome && reflect.DeepEqual(old.Core.Changes, next.Core.Changes)
		}
		return same && (old.NativePhase == storageIdentityLinkedObserved && next.Core.Phase == storageIntentPublishing && next.NativePhase == storageIdentityTargetDurable || old.NativePhase == storageIdentityTargetDurable && next.Core.Phase == storageIntentSettling && next.NativePhase == old.NativePhase && next.Facts == old.Facts)
	case storageIntentSettling:
		return next.Core.Phase == storageIntentSettled && next.NativePhase == old.NativePhase && next.Facts == old.Facts && next.Core.PreviousToken == old.Core.Token && storageIntentNextToken(old.Core.Token, next.Core.Token) && next.Core.Evidence == old.Core.Evidence && next.Core.Outcome == old.Core.Outcome && reflect.DeepEqual(next.Core.Changes, old.Core.Changes)
	case storageIntentSettled:
		return next.Core.Phase == storageIntentRetired && next.NativePhase == old.NativePhase && next.Facts == old.Facts && same && next.Core.Evidence == old.Core.Evidence && next.Core.Outcome == old.Core.Outcome && reflect.DeepEqual(next.Core.Changes, old.Core.Changes)
	}
	return false
}

// Closed action operands in the original launch accumulator. These are not
// grants: every live consumer must bind its exact P/action/attempt/parent and
// target, retain originals, and consume the whole API group before its effect.
// Local is a separate in-memory selector, never a sixth wire/native kind.
type storageIdentityFiniteGroup struct {
	ID                                  uint16
	Parent                              byte // 1 original config, 2 original Q, 3 original private, 4 original signing
	Bytes, Files, Directories, Metadata uint64
}

func storageIdentityFiniteGroups(action byte, local, recovery bool) ([]storageIdentityFiniteGroup, error) {
	if local && action != 0 || !local && (action < 1 || action > storageIdentityIntentMove) {
		return nil, storagebudget.ErrBounds
	}
	hosts, hostMetadata, qMetadata := 13, uint64(4), uint64(4)
	if action == storageIdentityIntentClear || action == storageIdentityIntentInitialCAS || action == storageIdentityIntentMove {
		hosts = 14
	}
	if action == storageIdentityIntentMove {
		hosts += 3 * storageMoveTailMaximum
	}
	if action == storageIdentityIntentStamp {
		hosts = 10
	}
	if local {
		hostMetadata, qMetadata = 16, 28
	}
	out := make([]storageIdentityFiniteGroup, 0, 27)
	for i := 0; i < hosts; i++ {
		out = append(out, storageIdentityFiniteGroup{ID: storageIdentityHostGroup(action, uint16(i+1)), Parent: 1, Bytes: 16384, Metadata: hostMetadata})
	}
	for i, size := range []uint64{524288, 16384, 16384, 4194304, 16384, 4194304} {
		out = append(out, storageIdentityFiniteGroup{ID: uint16(20 + i), Parent: 2, Bytes: size, Metadata: qMetadata})
	}
	if action == storageIdentityIntentStamp {
		for i := 0; i < 3; i++ {
			out = append(out, storageIdentityFiniteGroup{ID: uint16(30 + i), Parent: 4, Metadata: 16})
		}
	} else if local {
		for i, row := range [][3]uint64{{0, 0, 1}, {0, 0, 0}, {2, 1, 0}, {2, 0, 0}, {0, 1, 0}, {0, 0, 0}} {
			out = append(out, storageIdentityFiniteGroup{ID: uint16(30 + i), Parent: 3, Bytes: row[0], Files: row[1], Directories: row[2], Metadata: 28})
		}
	} else {
		for i, row := range [][2]uint64{{0, 1}, {512, 0}, {512, 1}, {0, 0}, {512, 0}, {0, 0}} {
			out = append(out, storageIdentityFiniteGroup{ID: uint16(30 + i), Parent: 3, Bytes: row[0], Files: row[1], Metadata: 16})
		}
	}
	if recovery {
		out = append(out, storageIdentityFiniteGroup{ID: 40, Parent: 2, Bytes: 44040192, Metadata: 16})
	}
	return out, nil
}

func storageIdentityFiniteTotals(groups []storageIdentityFiniteGroup) (uint64, storagebudget.CapacityVector, error) {
	var raw uint64
	var peak storagebudget.CapacityVector
	seen := map[uint16]bool{}
	for _, g := range groups {
		if seen[g.ID] || g.Parent < 1 || g.Parent > 4 {
			return 0, storagebudget.CapacityVector{}, storagebudget.ErrBounds
		}
		seen[g.ID] = true
		v, err := storageAdvisoryEstimate(g.Bytes, g.Files, g.Directories, g.Metadata)
		if err != nil || g.Bytes > 1<<60-raw || v.Bytes > 1<<60-peak.Bytes || v.Inodes > 1<<40-peak.Inodes {
			return 0, storagebudget.CapacityVector{}, storagebudget.ErrBounds
		}
		raw += g.Bytes
		peak.Bytes += v.Bytes
		peak.Inodes += v.Inodes
	}
	return raw, peak, nil
}

// Move's additional J prefixes must not collide with original Q20..25,
// native30..34, recovery40 or closed catalog/tail groups. Old IDs stay exact.
func storageIdentityHostGroup(action byte, n uint16) uint16 {
	if action == storageIdentityIntentMove && n >= 15 {
		return 100 + n
	}
	return n
}
